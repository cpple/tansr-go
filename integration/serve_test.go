package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tansrai/tansr-go/api"
	"github.com/tansrai/tansr-go/archive"
	"github.com/tansrai/tansr-go/executor"
	"github.com/tansrai/tansr-go/memorypublication"
	"github.com/tansrai/tansr-go/session"
)

// 这些测试只有显式指定 CLI 工作区或同源便携宿主时运行，普通 Go 消费者不安装 Node。
type fixture struct {
	PublicationIdentity   memorypublication.Identity `json:"publicationIdentity"`
	BaseURL               string                     `json:"baseURL"`
	Token                 string                     `json:"token"`
	ApplicationScopeID    string                     `json:"applicationScopeId"`
	EndUserID             string                     `json:"endUserId"`
	AuthorizationRevision string                     `json:"authorizationRevision"`
	DefinitionDigest      string                     `json:"definitionDigest"`
	Declaration           json.RawMessage            `json:"declaration"`
	ManifestRevision      int                        `json:"manifestRevision"`
}

// loseAckResponse forwards the real request and drains its real success response,
// then models a connection loss. It never fabricates a Serve response or receipt.
type loseAckResponse struct {
	lost      atomic.Bool
	operation string
}

func (l *loseAckResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	operation := l.operation
	if operation == "" {
		operation = api.OpArchiveAckCommit
	}
	ack, _ := api.Lookup(operation)
	prefix, suffix, _ := strings.Cut(ack.Path, ":id")
	if request.Method == ack.Method && strings.HasPrefix(request.URL.Path, prefix) && strings.HasSuffix(request.URL.Path, suffix) && response.StatusCode == http.StatusOK && l.lost.CompareAndSwap(false, true) {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		return nil, fmt.Errorf("synthetic acknowledgement response loss after Serve commit")
	}
	return response, nil
}

func physicalTempDir(t *testing.T) string {
	t.Helper()
	// Canonicalize only a directory created by this test, so macOS TMPDIR's
	// /var symlink does not violate Go/Serve archive stores' physical-path policy.
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

func startFixture(t *testing.T, mode string) fixture {
	t.Helper()
	cliRoot := os.Getenv("TANSR_GO_SERVE_CLI_ROOT")
	portable := os.Getenv("TANSR_GO_SERVE_FIXTURE")
	if cliRoot == "" && portable == "" {
		t.Skip("真实 Serve 集成需要 TANSR_GO_SERVE_CLI_ROOT 或同源 TANSR_GO_SERVE_FIXTURE")
	}
	var err error
	node := os.Getenv("TANSR_GO_SERVE_NODE")
	if node == "" {
		node = "node"
	}
	ctx, cancel := context.WithCancel(context.Background())
	moduleURL := func(parts ...string) string {
		p := filepath.ToSlash(filepath.Join(parts...))
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		return (&url.URL{Scheme: "file", Path: p}).String()
	}
	var cmd *exec.Cmd
	if portable != "" {
		portable, err = filepath.Abs(portable)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		info, statErr := os.Stat(portable)
		if statErr != nil || !info.Mode().IsRegular() {
			cancel()
			t.Fatalf("portable Serve fixture must be a regular file: %v", statErr)
		}
		// Portable output embeds the original manifest and statically bundles the
		// same source imports. A fresh working directory prevents accidental
		// fallback to an installed CLI tree or its node_modules.
		directory := physicalTempDir(t)
		cmd = exec.CommandContext(ctx, node, portable, ".", directory, mode)
		cmd.Dir = directory
	} else {
		cliRoot, err = filepath.Abs(cliRoot)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		script, err := filepath.Abs("serve-fixture.mjs")
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		cmd = exec.CommandContext(ctx, node, "--import", moduleURL(cliRoot, "scripts", "inject-globals.mjs"),
			"--import", moduleURL(cliRoot, "node_modules", "tsx", "dist", "loader.mjs"), script, cliRoot, physicalTempDir(t), mode)
		cmd.Dir = cliRoot
	}
	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	ready := make(chan fixture, 1)
	scanDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "TANSR_GO_FIXTURE ") {
				var f fixture
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "TANSR_GO_FIXTURE ")), &f); err != nil {
					scanDone <- err
					return
				}
				ready <- f
			}
		}
		scanDone <- scanner.Err()
	}()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	t.Cleanup(func() {
		_, _ = io.WriteString(stdin, "stop\n")
		_ = stdin.Close()
		select {
		case err := <-waited:
			if err != nil {
				t.Errorf("Serve fixture exit: %v\n%s", err, stderr.String())
			}
		case <-time.After(15 * time.Second):
			cancel()
			<-waited
			t.Errorf("Serve fixture did not stop gracefully\n%s", stderr.String())
		}
		cancel()
	})
	select {
	case f := <-ready:
		if f.ManifestRevision != api.ManifestRevision {
			t.Fatalf("Serve manifest %d != frozen SDK %d", f.ManifestRevision, api.ManifestRevision)
		}
		return f
	case err := <-scanDone:
		t.Fatalf("Serve fixture exited before ready: %v", err)
	case <-time.After(60 * time.Second):
		t.Fatal("Serve fixture startup timed out")
	}
	return fixture{}
}

func clientFor(t *testing.T, f fixture) *api.Client {
	return clientForFamily(t, f, "sdk1")
}

func clientForFamily(t *testing.T, f fixture, family string) *api.Client {
	t.Helper()
	c, err := api.New(api.Options{BaseURL: f.BaseURL, Token: f.Token, SessionFamily: family, EventEnvelope: true})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRealServeExecutorClient(t *testing.T) {
	f := startFixture(t, "execution")
	c := clientFor(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	sessions, err := session.New(c)
	if err != nil {
		t.Fatal(err)
	}
	s, err := sessions.Create(ctx, session.CreateOptions{ClientTools: []json.RawMessage{f.Declaration}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background(), session.WriteOptions{})
	x, err := executor.NewClient(c, executor.Scope{ApplicationScopeID: f.ApplicationScopeID, EndUserID: f.EndUserID, AuthorizationRevision: f.AuthorizationRevision})
	if err != nil {
		t.Fatal(err)
	}
	platform := executor.CurrentPlatform()
	workspace := executor.Workspace{WorkspaceID: "go-business-workspace", Revision: "1"}
	journal, err := executor.NewFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	var executions atomic.Int32
	runner, err := executor.NewRunner(executor.RunnerOptions{Client: x, Journal: journal,
		Registration: executor.Registration{Protocol: executor.Protocol, ExecutorID: "go-executor", Platform: platform,
			Workspaces: []executor.Workspace{workspace}, Operations: []string{"tool.invoke"}, Tools: []executor.ToolDefinition{{Name: "BusinessLookup", DefinitionDigest: f.DefinitionDigest}}},
		Authorize: func(_ context.Context, operation executor.Operation) error {
			if operation.SessionID != s.ID() || operation.Scope != x.Scope() || operation.Binding.Target.WorkspaceID != workspace.WorkspaceID {
				return fmt.Errorf("unexpected local execution identity")
			}
			return nil
		},
		Tools: map[string]executor.Tool{"BusinessLookup": {DefinitionDigest: f.DefinitionDigest, Handle: func(context.Context, map[string]any) (any, error) {
			executions.Add(1)
			return map[string]any{"status": "ok", "content": []any{map[string]any{"t": "text", "text": "go-terminal-fact"}}}, nil
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := runner.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	connection, err = x.Heartbeat(ctx, connection)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := s.Capabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	caps, err := x.Initialize(ctx, s.ID(), platform, []string{"BusinessLookup"}, closure.ClosureID)
	if err != nil {
		t.Fatal(err)
	}
	closure, err = s.Capabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	caps, err = x.Bind(ctx, s.ID(), connection, workspace, caps.CapabilityRevision, closure.ClosureID)
	if err != nil || caps.Binding == nil {
		t.Fatalf("bind: %+v %v", caps, err)
	}
	stream, err := s.Events(ctx, fmt.Sprint(s.Created().LastSeq))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err = s.Send(ctx, "GO-TOOL", session.WriteOptions{IdempotencyKey: "go-executor-message"}); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 1)
	go func() {
		for {
			event, err := stream.Next()
			if err != nil {
				results <- err
				return
			}
			if event.Type == "server.permission.request" {
				var permission struct {
					RequestID string `json:"requestId"`
					Digest    string `json:"digest"`
				}
				if err = json.Unmarshal(event.Raw, &permission); err != nil {
					results <- err
					return
				}
				if _, err = s.Permission(ctx, permission.RequestID, permission.Digest, "allow", session.WriteOptions{IdempotencyKey: "go-executor-approval"}); err != nil {
					results <- err
					return
				}
			}
			if outcome, done := event.TurnOutcome(); done {
				if outcome.Status != session.OutcomeCompleted {
					results <- fmt.Errorf("tool turn: %+v", outcome)
				} else {
					results <- nil
				}
				return
			}
		}
	}()
	var operation executor.Operation
	for operation.OperationID == "" {
		batch, err := x.Poll(ctx, connection)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Operations) > 1 {
			t.Fatalf("unexpected operations: %+v", batch)
		}
		if len(batch.Operations) == 1 {
			operation = batch.Operations[0]
			break
		}
		select {
		case err := <-results:
			t.Fatalf("turn ended without executor operation: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	if operation.ToolName != "BusinessLookup" || operation.Request.Operation != "tool.invoke" {
		t.Fatalf("wrong device dispatch: %+v", operation)
	}
	receipt, err := runner.Execute(ctx, operation)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := runner.Execute(ctx, operation)
	if err != nil || executions.Load() != 1 || replayed.OperationID != receipt.OperationID || replayed.Digest != receipt.Digest {
		t.Fatalf("local durable replay: executions=%d receipt=%+v err=%v", executions.Load(), replayed, err)
	}
	for i := 0; i < 2; i++ {
		status, err := x.Submit(ctx, operation, receipt)
		if err != nil || status.Status != "completed" {
			t.Fatalf("receipt %d: %+v %v", i, status, err)
		}
	}
	select {
	case err := <-results:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	history, err := s.History(ctx, 0, 20)
	if err != nil || !bytes.Contains(history, []byte("go-tool-complete")) {
		t.Fatalf("tool history: %s %v", history, err)
	}
}

func TestRealServeArchiveClient(t *testing.T) {
	for _, family := range []string{"sdk1", "sdk2-offload-v1"} {
		t.Run(family, func(t *testing.T) {
			mode := "archive"
			if family == "sdk2-offload-v1" {
				mode = "archive-offload"
			}
			f := startFixture(t, mode)
			loss := &loseAckResponse{}
			c, err := api.New(api.Options{BaseURL: f.BaseURL, Token: f.Token, SessionFamily: family,
				EventEnvelope: true, HTTPClient: &http.Client{Transport: loss}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			sessions, err := session.New(c)
			if err != nil {
				t.Fatal(err)
			}
			create := session.CreateOptions{}
			if family == "sdk2-offload-v1" {
				create.RequestID = "go-archive-create"
			}
			s, err := sessions.Create(ctx, create)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close(context.Background(), session.WriteOptions{})
			stream, err := s.Events(ctx, fmt.Sprint(s.Created().LastSeq))
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			a := archive.NewClient(c)
			target, err := a.BindingTarget(ctx, s.ID())
			if err != nil || target.BindingID == nil {
				t.Fatalf("archive binding target: %+v %v", target, err)
			}
			binding, err := a.Binding(ctx, *target.BindingID)
			if err != nil {
				t.Fatal(err)
			}
			status, err := a.Status(ctx, binding.BindingID)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := archive.IdentityFrom(binding, status)
			if err != nil {
				t.Fatal(err)
			}
			key := make([]byte, 32)
			if _, err = rand.Read(key); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(physicalTempDir(t), "terminal.archive")
			options := archive.StoreOptions{Path: path, Key: key, Identity: identity, CheckAccess: func(got archive.Identity) error {
				if got != identity || got.ApplicationScopeID != f.ApplicationScopeID || got.EndUserID != f.EndUserID {
					return fmt.Errorf("unexpected archive identity")
				}
				return nil
			}}
			store, err := archive.OpenFileStore(options)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			if _, err = s.Send(ctx, "GO-ARCHIVE", session.WriteOptions{IdempotencyKey: "go-archive-message"}); err != nil {
				t.Fatal(err)
			}
			for {
				event, err := stream.Next()
				if err != nil {
					t.Fatal(err)
				}
				if outcome, done := event.TurnOutcome(); done {
					if outcome.Status != session.OutcomeCompleted {
						t.Fatalf("archive turn: %+v", outcome)
					}
					break
				}
			}
			// turn.completed is delivered before archive finish-run persists its
			// control revision. Fence this loss-only test on public session idle;
			// the separate rebase test deliberately changes the revision after save.
			waitArchiveIdle(t, ctx, s)
			_, err = archive.SyncOnce(ctx, a, store, "go-archive-sync")
			if err == nil || !loss.lost.Load() {
				t.Fatalf("ack response loss not observed: %v", err)
			}
			pending, err := store.Pending()
			if err != nil || pending == nil {
				t.Fatalf("original ACK not durable: %+v %v", pending, err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = archive.OpenFileStore(options)
			if err != nil {
				t.Fatal(err)
			}
			synced, err := archive.SyncOnce(ctx, a, store, "must-not-replace-original-ack")
			if err != nil {
				t.Fatal(err)
			}
			if !synced.Recovered || synced.Receipt == nil || synced.Receipt.Request != pending.Request {
				t.Fatalf("original acknowledgement not recovered: %+v", synced)
			}
			coverage, err := store.Coverage()
			if err != nil || coverage == nil {
				t.Fatalf("coverage: %+v %v", coverage, err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = archive.OpenFileStore(options)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := store.Coverage()
			if err != nil || restored == nil || *restored != *coverage {
				t.Fatalf("restored coverage: %+v %v", restored, err)
			}
			records, err := store.ReadRecords("1", 128)
			if err != nil || len(records) == 0 {
				t.Fatalf("restored records: %d %v", len(records), err)
			}
			found := false
			for _, record := range records {
				body, err := store.Body(record.Payload)
				if err != nil {
					t.Fatal(err)
				}
				found = found || bytes.Contains(body, []byte("go-archive-answer"))
			}
			if !found {
				t.Fatal("terminal archive did not retain synthetic assistant answer")
			}
			status, err = a.Status(ctx, binding.BindingID)
			if err != nil || status.AcknowledgedCoverage == nil || *status.AcknowledgedCoverage != *coverage {
				t.Fatalf("Serve acknowledged coverage: %+v %v", status, err)
			}
			resumed, err := sessions.Resume(ctx, s.ID())
			if err != nil || resumed.ID() != s.ID() {
				t.Fatalf("explicit %s resume: %v", family, err)
			}
		})
	}
}

func waitArchiveIdle(t *testing.T, ctx context.Context, s *session.Session) {
	t.Helper()
	for {
		meta, err := s.Meta(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if meta.Status == "idle" {
			return
		}
		if meta.Status != "running" {
			t.Fatalf("session ended before archive commit: %s", meta.Status)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// A completed second turn deterministically advances the original binding's
// revision after the first page was durably received. No fixture edits or sleeps
// stand in for the real control CAS, idle lease, rebase mapping or receipt.
func TestRealServeArchiveRebase(t *testing.T) {
	for _, family := range []string{"sdk1", "sdk2-offload-v1"} {
		t.Run(family, func(t *testing.T) {
			mode := "archive"
			if family == "sdk2-offload-v1" {
				mode = "archive-offload"
			}
			f := startFixture(t, mode)
			loss := &loseAckResponse{operation: api.OpArchiveAckRebase}
			transport, err := api.New(api.Options{BaseURL: f.BaseURL, Token: f.Token, SessionFamily: family, EventEnvelope: true, HTTPClient: &http.Client{Transport: loss}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			sessions, err := session.New(transport)
			if err != nil {
				t.Fatal(err)
			}
			create := session.CreateOptions{}
			if family == "sdk2-offload-v1" {
				create.RequestID = "rebase-session-create"
			}
			s, err := sessions.Create(ctx, create)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close(context.Background(), session.WriteOptions{})
			stream, err := s.Events(ctx, fmt.Sprint(s.Created().LastSeq))
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			turn := func(requestID string) {
				t.Helper()
				if _, err := s.Send(ctx, "GO-ARCHIVE", session.WriteOptions{IdempotencyKey: requestID}); err != nil {
					t.Fatal(err)
				}
				for {
					event, err := stream.Next()
					if err != nil {
						t.Fatal(err)
					}
					if outcome, done := event.TurnOutcome(); done {
						if outcome.Status != session.OutcomeCompleted {
							t.Fatalf("archive turn failed: %+v", outcome)
						}
						break
					}
				}
				waitArchiveIdle(t, ctx, s)
			}
			turn("rebase-first-turn")
			a := archive.NewClient(transport)
			target, err := a.BindingTarget(ctx, s.ID())
			if err != nil || target.BindingID == nil {
				t.Fatal("binding target", err)
			}
			binding, err := a.Binding(ctx, *target.BindingID)
			if err != nil {
				t.Fatal(err)
			}
			status, err := a.Status(ctx, binding.BindingID)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := archive.IdentityFrom(binding, status)
			if err != nil {
				t.Fatal(err)
			}
			page, err := a.ReadRecords(ctx, binding, nil)
			if err != nil || len(page.Records) == 0 {
				t.Fatal("first page", err)
			}
			objects := map[string][]byte{}
			for _, record := range page.Records {
				for _, ref := range append([]archive.ArtifactRef{record.Payload}, record.Attachments...) {
					if _, ok := objects[ref.ArtifactID]; ok {
						continue
					}
					body, err := a.ReadArtifact(ctx, binding, ref)
					if err != nil {
						t.Fatal(err)
					}
					objects[ref.ArtifactID] = body
				}
			}
			key := make([]byte, 32)
			if _, err = rand.Read(key); err != nil {
				t.Fatal(err)
			}
			options := archive.StoreOptions{Path: filepath.Join(physicalTempDir(t), "recovery.archive"), Key: key, Identity: identity, CheckAccess: func(got archive.Identity) error {
				if got != identity {
					return archive.ErrIntegrity
				}
				return nil
			}}
			store, err := archive.OpenFileStore(options)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			previous, err := store.Receive(binding, status, page, objects, archive.RequestIdentity{RequestID: "stale-original-ack", OperationEpoch: binding.OperationEpoch.ID})
			if err != nil {
				t.Fatal(err)
			}
			turn("rebase-second-turn")
			current, err := a.Binding(ctx, binding.BindingID)
			if err != nil || current.Revision == previous.ExpectedRevision {
				t.Fatal("real run did not advance binding", err)
			}
			if _, err = archive.SyncOnce(ctx, a, store, "must-not-replace-original"); err == nil {
				t.Fatal("stale original ACK unexpectedly accepted")
			} else {
				var apiErr *api.APIError
				if !errors.As(err, &apiErr) || apiErr.Code != api.CodePreconditionFailed || apiErr.Detail.Reason != api.ReasonIfMatchStale {
					t.Fatalf("not the expected revision conflict: %v", err)
				}
			}
			if _, err = archive.RecoverPending(ctx, a, store, "rebase-fixed-request"); err == nil || !loss.lost.Load() {
				t.Fatalf("real committed rebase response was not lost: %v", err)
			}
			intent, err := store.PendingRebase()
			if err != nil || intent == nil || intent.Request.RequestID != "rebase-fixed-request" {
				t.Fatal("recovery intent not durable", err)
			}
			if coverage, _ := store.Coverage(); coverage != nil {
				t.Fatal("lost rebase response advanced coverage")
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = archive.OpenFileStore(options)
			if err != nil {
				t.Fatal(err)
			}
			recovered, err := archive.SyncOnce(ctx, a, store, "must-not-replace-recovery")
			if err != nil {
				t.Fatal(err)
			}
			if !recovered.Recovered || recovered.Receipt == nil || recovered.Receipt.Request != intent.Request {
				t.Fatalf("recovered wrong operation: %+v", recovered)
			}
			if pending, _ := store.Pending(); pending != nil {
				t.Fatal("old ACK remained pending")
			}
			if pending, _ := store.PendingRebase(); pending != nil {
				t.Fatal("recovery remained pending")
			}
			coverage, err := store.Coverage()
			if err != nil || coverage == nil || *coverage != previous.Coverage {
				t.Fatal("wrong local coverage", err)
			}
			status, err = a.Status(ctx, binding.BindingID)
			if err != nil || status.AcknowledgedCoverage == nil || *status.AcknowledgedCoverage != *coverage {
				t.Fatal("server coverage disagrees", err)
			}
			for _, record := range page.Records {
				body, err := store.Body(record.Payload)
				if err != nil || !bytes.Equal(body, objects[record.Payload.ArtifactID]) {
					t.Fatal("recovery altered original bytes", err)
				}
			}
			t.Logf("original ACK revision=%s rejected after run revision=%s; recovery=%s receipt revision=%s; preserved coverage=%s", previous.ExpectedRevision, current.Revision, intent.Request.RequestID, recovered.Receipt.Revision, coverage.ThroughSequence)
		})
	}
}

func TestRealServeSessionWire(t *testing.T) {
	f := startFixture(t, "session")
	c := clientFor(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	created, err := c.Call(ctx, api.OpSessionCreate, api.CallOptions{Body: map[string]any{"tools": []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	var made struct {
		SessionID string `json:"sessionId"`
		LastSeq   int64  `json:"lastSeq"`
	}
	if err := created.Decode(&made); err != nil || made.SessionID == "" {
		t.Fatalf("invalid create: %s (%v)", created.Body, err)
	}
	p := map[string]string{"id": made.SessionID}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if _, err := c.Call(closeCtx, api.OpSessionClose, api.CallOptions{Params: p}); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	stream, err := c.Events(ctx, api.OpSessionEventsObserve, api.EventsOptions{Params: p, LastEventID: fmt.Sprint(made.LastSeq)})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	closure, err := c.SessionCapabilities(ctx, made.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	sent, err := c.Call(ctx, api.OpSessionMessageSend, api.CallOptions{Params: p, Body: map[string]any{"prompt": "GO-SESSION"},
		ClosureID: closure.ClosureID, IdempotencyKey: "go-integration-message"})
	if err != nil || sent.Status != 202 {
		t.Fatalf("send: %+v %v", sent, err)
	}
	var text strings.Builder
	for {
		frame, err := stream.Next()
		if err != nil {
			t.Fatalf("missing completed turn: %v", err)
		}
		env := frame.Envelope
		if env.Type != nil && *env.Type == "msg.text.delta" {
			var raw struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(env.Raw, &raw); err != nil {
				t.Fatal(err)
			}
			text.WriteString(raw.Text)
		}
		if env.IsTerminal() {
			if env.Type == nil || *env.Type != "turn.completed" || *env.TerminalStatus != api.TerminalCompleted {
				t.Fatalf("not a completed turn: %s", frame.Data)
			}
			break
		}
	}
	if text.String() != "go-real-serve-answer" {
		t.Fatalf("text=%q", text.String())
	}
	if stream.LastEventID() == "" {
		t.Fatal("no replay cursor")
	}
}

func TestRealServeSessionClient(t *testing.T) {
	f := startFixture(t, "session")
	c, err := session.New(clientFor(t, f))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := c.Create(ctx, session.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := s.Close(closeCtx, session.WriteOptions{}); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	attached, err := c.Resume(ctx, s.ID())
	if err != nil || attached.ID() != s.ID() {
		t.Fatalf("resume live: %v", err)
	}
	stream, err := s.Events(ctx, fmt.Sprint(s.Created().LastSeq))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := s.Send(ctx, "GO-HIGH-LEVEL", session.WriteOptions{IdempotencyKey: "go-session-client"}); err != nil {
		t.Fatal(err)
	}
	for {
		event, err := stream.Next()
		if err != nil {
			t.Fatal(err)
		}
		if outcome, done := event.TurnOutcome(); done {
			if outcome.Status != session.OutcomeCompleted {
				t.Fatalf("outcome: %+v", outcome)
			}
			break
		}
	}
	history, err := s.History(ctx, 0, 20)
	if err != nil || !bytes.Contains(history, []byte("go-real-serve-answer")) {
		t.Fatalf("history: %s %v", history, err)
	}
	last := stream.LastEventID()
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	reconnected, err := s.Events(ctx, last)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	if _, err := s.Send(ctx, "GO-BLOCK", session.WriteOptions{IdempotencyKey: "go-session-block"}); err != nil {
		t.Fatal(err)
	}
	interrupted := false
	for {
		event, err := reconnected.Next()
		if err != nil {
			t.Fatal(err)
		}
		if event.Type == "msg.text.delta" && !interrupted {
			if _, err := s.Interrupt(ctx, session.WriteOptions{IdempotencyKey: "go-session-interrupt"}); err != nil {
				t.Fatal(err)
			}
			interrupted = true
		}
		if outcome, done := event.TurnOutcome(); done {
			if !interrupted || outcome.Status != session.OutcomeAborted {
				t.Fatalf("interrupt outcome: %+v", outcome)
			}
			break
		}
	}
}
