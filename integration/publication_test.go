package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tansrai/tansr-go/api"
	"github.com/tansrai/tansr-go/canonical"
	"github.com/tansrai/tansr-go/executor"
	"github.com/tansrai/tansr-go/internal/wire"
	"github.com/tansrai/tansr-go/memorypublication"
	"github.com/tansrai/tansr-go/session"
	"github.com/tansrai/tansr-go/terminalpersistence"
)

func TestRealServeEncryptedMemoryPublication(t *testing.T) {
	f := startFixture(t, "publication")
	client := clientFor(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	sessions, e := session.New(client)
	if e != nil {
		t.Fatal(e)
	}
	conversation, e := sessions.Create(ctx, session.CreateOptions{Tools: []string{"SearchMemory"}})
	if e != nil {
		t.Fatal(e)
	}
	scope := executor.Scope{ApplicationScopeID: f.ApplicationScopeID, EndUserID: f.EndUserID, AuthorizationRevision: f.AuthorizationRevision}
	execution, e := executor.NewClient(client, scope)
	if e != nil {
		t.Fatal(e)
	}
	root := physicalTempDir(t)
	key := bytes.Repeat([]byte{31}, 32)
	options := memorypublication.Options{Path: filepath.Join(root, "memory.bin"), Mode: "create", Key: key, Identity: f.PublicationIdentity, CurrentScope: func() (executor.Scope, error) { return scope, nil }}
	store, e := memorypublication.OpenFileStore(options)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	host, e := memorypublication.NewHost(store, true)
	if e != nil {
		t.Fatal(e)
	}
	journal, e := executor.NewEncryptedFileJournal(filepath.Join(root, "journal"), executor.JournalEncryption{Key: key, ApplicationScopeID: scope.ApplicationScopeID, EndUserID: scope.EndUserID, ExecutorID: "go-executor", CheckAccess: func() error { return nil }})
	if e != nil {
		t.Fatal(e)
	}
	defer journal.Close()
	workspace := executor.Workspace{WorkspaceID: "go-memory", Revision: "1"}
	platform := executor.CurrentPlatform()
	var expected *executor.Binding
	var calls atomic.Int32
	runner, e := executor.NewRunner(executor.RunnerOptions{Client: execution, Journal: journal, MemoryPublication: host, Registration: executor.Registration{Protocol: executor.Protocol, ExecutorID: "go-executor", Platform: platform, Workspaces: []executor.Workspace{workspace}, Operations: []string{"tool.invoke"}, Tools: []executor.ToolDefinition{{Name: executor.MemoryPublicationToolName, DefinitionDigest: executor.MemoryPublicationDefinitionDigest}}}, Authorize: func(_ context.Context, op executor.Operation) error {
		if expected == nil || op.ToolName != "MemoryPublication" || op.SessionID != conversation.ID() || op.Scope != scope || !reflect.DeepEqual(op.Binding, *expected) {
			return executor.ErrConflict
		}
		return nil
	}, OnReceipt: func(_ context.Context, _ executor.Operation, receipt executor.Receipt) error {
		if receipt.Status != "completed" {
			return fmt.Errorf("publication receipt: %s", receipt.Status)
		}
		calls.Add(1)
		return nil
	}})
	if e != nil {
		t.Fatal(e)
	}
	connection, e := runner.Connect(ctx)
	if e != nil {
		t.Fatal(e)
	}
	closure, e := conversation.Capabilities(ctx)
	if e != nil {
		t.Fatal(e)
	}
	initialized, e := execution.Initialize(ctx, conversation.ID(), platform, nil, closure.ClosureID)
	if e != nil {
		t.Fatal(e)
	}
	closure, e = conversation.Capabilities(ctx)
	if e != nil {
		t.Fatal(e)
	}
	bound, e := execution.Bind(ctx, conversation.ID(), connection, workspace, initialized.CapabilityRevision, closure.ClosureID)
	if e != nil {
		t.Fatal(e)
	}
	expected = bound.Binding
	ref := map[string]any{"sessionContract": "sdk1", "sessionId": conversation.ID()}
	binding := map[string]any{"contract": memorypublication.Contract, "requestId": "go-memory-binding", "session": ref, "executionBinding": expected, "required": []string{"memory-lifecycle-v1"}, "optional": []string{}}
	if e = wire.Validate(memorypublication.Contract, "BindingRequest", binding); e != nil {
		t.Fatal(e)
	}
	result, e := client.Call(ctx, api.OpTerminalBindingCreate, api.CallOptions{Body: binding})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = wire.Decode(memorypublication.Contract, "BindingResponse", result.Body); e != nil {
		t.Fatal(e)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- runner.Run(runCtx) }()
	drained := false
	defer func() {
		stop()
		if drained {
			return
		}
		select {
		case e := <-done:
			if e != nil && runCtx.Err() == nil {
				t.Error(e)
			}
		case <-time.After(10 * time.Second):
			t.Error("runner did not drain")
		}
	}()
	result, e = client.Call(ctx, api.OpTerminalMemoryRead, api.CallOptions{Params: map[string]string{"id": conversation.ID()}, Query: map[string]string{"contract": memorypublication.Contract, "sessionContract": "sdk1"}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = wire.Decode(memorypublication.Contract, "MemoryStateResponse", result.Body); e != nil {
		t.Fatal(e)
	}
	owner := memorypublication.Owner{Scope: scope, SessionID: conversation.ID(), Binding: *expected}
	head := memorypublication.Request{"contract": memorypublication.Contract, "action": "head", "sourceId": f.PublicationIdentity.SourceID, "sourceGeneration": f.PublicationIdentity.SourceGeneration, "domainKey": f.PublicationIdentity.DomainKey}
	response, e := store.Execute(ctx, head, owner)
	if e != nil {
		t.Fatal(e)
	}
	if response["publication"] == nil || calls.Load() < 4 {
		t.Fatalf("memory API must actually publish device bytes: %v calls=%d", response, calls.Load())
	}
	// Stop execution before cold reopening the original medium. No body or credentials are logged.
	stop()
	if e := <-done; e != nil && !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	drained = true
	store.Close()
	options.Mode = "reopen"
	store, e = memorypublication.OpenFileStore(options)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	after, e := store.Execute(ctx, head, owner)
	if e != nil || !reflect.DeepEqual(after, response) {
		t.Fatal("cold reopen changed publication", e)
	}
	raw, e := os.ReadFile(options.Path)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, []byte(f.PublicationIdentity.DomainKey)) {
		t.Fatal("plaintext metadata")
	}
	t.Logf("real Serve publication receipts=%d, cold reopened encrypted medium; no model request", calls.Load())
}

// persistenceTrace observes actual Host calls; it does not generate operations.
type persistenceTrace struct {
	mu            sync.Mutex
	operations    []executor.Operation
	outputs       []any
	target        string
	lost, queried bool
}
type tracedPersistenceHost struct {
	executor.TerminalPersistenceHost
	trace *persistenceTrace
}

func (h tracedPersistenceHost) Execute(ctx context.Context, op executor.Operation, args map[string]any) (any, error) {
	out, err := h.TerminalPersistenceHost.Execute(ctx, op, args)
	h.trace.mu.Lock()
	defer h.trace.mu.Unlock()
	h.trace.operations = append(h.trace.operations, op)
	h.trace.outputs = append(h.trace.outputs, out)
	if err == nil && args["action"] == "commit" && h.trace.target == "" {
		h.trace.target = op.OperationID
	}
	return out, err
}
func (x *persistenceTrace) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	submit, _ := api.Lookup(api.OpExecutorReceiptSubmit)
	prefix, suffix, _ := strings.Cut(submit.Path, ":id")
	if req.Method == submit.Method && strings.HasPrefix(req.URL.Path, prefix) && strings.HasSuffix(req.URL.Path, suffix) && req.GetBody != nil && response.StatusCode == 200 && !x.lost {
		body, e := req.GetBody()
		if e != nil {
			response.Body.Close()
			return nil, e
		}
		var receipt executor.Receipt
		e = json.NewDecoder(body).Decode(&receipt)
		body.Close()
		if e != nil {
			response.Body.Close()
			return nil, e
		}
		if receipt.OperationID == x.target && receipt.Status == "completed" {
			_, e = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if e != nil {
				return nil, e
			}
			x.lost = true
			return nil, errors.New("synthetic loss after real commit receipt accepted")
		}
	}
	status, _ := api.Lookup(api.OpExecutionStatus)
	if x.lost && req.Method == status.Method && strings.HasSuffix(req.URL.Path, "/"+x.target) && response.StatusCode == 200 {
		x.queried = true
	}
	return response, nil
}
func (x *persistenceTrace) snapshot() ([]executor.Operation, []any, bool, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return append([]executor.Operation(nil), x.operations...), append([]any(nil), x.outputs...), x.lost, x.queried
}
func persistenceJSON(t *testing.T, value any) string {
	t.Helper()
	b, e := canonical.Encode(value, canonical.Options{MaxBytes: 32768})
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func persistenceArgs(t *testing.T, op executor.Operation) map[string]any {
	t.Helper()
	var args map[string]any
	if e := json.Unmarshal([]byte(op.Request.Args["argsJson"].(string)), &args); e != nil {
		t.Fatal(e)
	}
	return args
}

// This is one real /api consumer. Trusted fixture controls explicitly consume and
// archive the original committed receipt; this is not automatic model consumption.
func TestRealServeTerminalPersistencePermanentKeysAndReopen(t *testing.T) {
	f := startFixture(t, "persistence")
	if f.Profile != terminalpersistence.Contract {
		t.Fatalf("wrong negotiated fixture profile: %s", f.Profile)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	trace := new(persistenceTrace)
	client, e := api.New(api.Options{BaseURL: f.BaseURL, Token: f.Token, SessionFamily: "sdk1", EventEnvelope: true, HTTPClient: &http.Client{Transport: trace}})
	if e != nil {
		t.Fatal(e)
	}
	sessions, e := session.New(client)
	if e != nil {
		t.Fatal(e)
	}
	scope := executor.Scope{ApplicationScopeID: f.ApplicationScopeID, EndUserID: f.EndUserID, AuthorizationRevision: f.AuthorizationRevision}
	execution, e := executor.NewClient(client, scope)
	if e != nil {
		t.Fatal(e)
	}
	root := physicalTempDir(t)
	options := terminalpersistence.Options{Path: filepath.Join(root, "persistence.bin"), Mode: "create", Key: bytes.Repeat([]byte{41}, 32), Identity: f.PublicationIdentity, CurrentScope: func() (executor.Scope, error) { return scope, nil }}
	journalPath := filepath.Join(root, "journal")
	journalKey := executor.JournalEncryption{Key: bytes.Repeat([]byte{42}, 32), ApplicationScopeID: scope.ApplicationScopeID, EndUserID: scope.EndUserID, ExecutorID: "go-executor", CheckAccess: func() error { return nil }}
	store, e := terminalpersistence.OpenFileStore(options)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	journal, e := executor.NewEncryptedFileJournal(journalPath, journalKey)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { journal.Close() })
	type active struct {
		conversation *session.Session
		owner        terminalpersistence.Owner
		runner       *executor.Runner
		stop         func()
	}
	start := func() active {
		conversation, err := sessions.Create(ctx, session.CreateOptions{Tools: []string{"SearchMemory"}})
		if err != nil {
			t.Fatal(err)
		}
		host, err := terminalpersistence.NewHost(store, true)
		if err != nil {
			t.Fatal(err)
		}
		workspace := executor.Workspace{WorkspaceID: "go-persistence", Revision: "1"}
		platform := executor.CurrentPlatform()
		var binding *executor.Binding
		runner, err := executor.NewRunner(executor.RunnerOptions{Client: execution, Journal: journal, TerminalPersistence: tracedPersistenceHost{host, trace}, PollInterval: 10 * time.Millisecond,
			Registration: executor.Registration{Protocol: executor.Protocol, ExecutorID: "go-executor", Platform: platform, Workspaces: []executor.Workspace{workspace}, Operations: []string{"tool.invoke"}, Tools: []executor.ToolDefinition{{Name: executor.TerminalPersistenceToolName, DefinitionDigest: executor.TerminalPersistenceDefinitionDigest}}},
			Authorize: func(_ context.Context, op executor.Operation) error {
				if binding == nil || op.Scope != scope || op.SessionID != conversation.ID() || op.ToolName != "MemoryPublication" || !reflect.DeepEqual(op.Binding, *binding) {
					return executor.ErrConflict
				}
				return nil
			},
			OnReceipt: func(_ context.Context, _ executor.Operation, r executor.Receipt) error {
				if r.Status != "completed" {
					return fmt.Errorf("unexpected persistence receipt: %s", r.Status)
				}
				return nil
			}})
		if err != nil {
			t.Fatal(err)
		}
		connection, err := runner.Connect(ctx)
		if err != nil {
			t.Fatal(err)
		}
		closure, err := conversation.Capabilities(ctx)
		if err != nil {
			t.Fatal(err)
		}
		initialized, err := execution.Initialize(ctx, conversation.ID(), platform, nil, closure.ClosureID)
		if err != nil {
			t.Fatal(err)
		}
		closure, err = conversation.Capabilities(ctx)
		if err != nil {
			t.Fatal(err)
		}
		bound, err := execution.Bind(ctx, conversation.ID(), connection, workspace, initialized.CapabilityRevision, closure.ClosureID)
		if err != nil {
			t.Fatal(err)
		}
		binding = bound.Binding
		ref := map[string]any{"sessionContract": "sdk1", "sessionId": conversation.ID()}
		result, err := client.Call(ctx, api.OpTerminalBindingCreate, api.CallOptions{Body: map[string]any{"contract": memorypublication.Contract, "requestId": "persistence-binding-" + conversation.ID(), "session": ref, "executionBinding": binding, "required": []string{"memory-lifecycle-v1"}, "optional": []string{}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = wire.Decode(memorypublication.Contract, "BindingResponse", result.Body); err != nil {
			t.Fatal(err)
		}
		runCtx, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- runner.Run(runCtx) }()
		var once sync.Once
		drain := func() {
			once.Do(func() {
				stop()
				select {
				case err := <-done:
					if err != nil && !errors.Is(err, context.Canceled) {
						t.Error(err)
					}
				case <-time.After(10 * time.Second):
					t.Error("persistence runner did not drain")
				}
			})
		}
		t.Cleanup(drain)
		return active{conversation, terminalpersistence.Owner{Scope: scope, SessionID: conversation.ID(), Binding: *binding}, runner, drain}
	}
	decode := func(def string, raw []byte) map[string]any {
		if _, err := wire.Decode(memorypublication.Contract, def, raw); err != nil {
			t.Fatal(err)
		}
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	read := func(a active) map[string]any {
		result, err := client.Call(ctx, api.OpTerminalMemoryRead, api.CallOptions{Params: map[string]string{"id": a.conversation.ID()}, Query: map[string]string{"contract": memorypublication.Contract, "sessionContract": "sdk1"}})
		if err != nil {
			t.Fatal(err)
		}
		return decode("MemoryStateResponse", result.Body)["memory"].(map[string]any)
	}
	queryReceipt := func(a active) map[string]any {
		result, err := client.Call(ctx, api.OpTerminalMemoryReceipt, api.CallOptions{Params: map[string]string{"id": a.conversation.ID(), "targetId": "go-persistence-operation"}, Query: map[string]string{"contract": memorypublication.Contract, "sessionContract": "sdk1", "requestId": "go-persistence-request"}})
		if err != nil {
			t.Fatal(err)
		}
		value := decode("MemoryReceiptResponse", result.Body)["receipt"]
		if value == nil {
			t.Fatal("original archived receipt missing")
		}
		return value.(map[string]any)
	}
	first := start()
	state := read(first)
	if e = f.control(ctx, map[string]any{"command": "settle-publication", "sessionId": first.conversation.ID()}, nil); e != nil {
		t.Fatal(e)
	}
	command := map[string]any{"contract": memorypublication.Contract, "session": map[string]any{"sessionContract": "sdk1", "sessionId": first.conversation.ID()}, "requestId": "go-persistence-request", "operationId": "go-persistence-operation", "sourceId": f.PublicationIdentity.SourceID, "sourceGeneration": f.PublicationIdentity.SourceGeneration, "expectedRevision": state["revision"], "command": map[string]any{"kind": "pin", "text": "synthetic Go persistence fact"}}
	result, e := client.Call(ctx, api.OpTerminalMemoryCommand, api.CallOptions{Params: map[string]string{"id": first.conversation.ID()}, Body: command})
	if e != nil {
		t.Fatal(e)
	}
	original := decode("MemoryReceiptResponse", result.Body)["receipt"].(map[string]any)
	if original["status"] != "committed" || original["durable"] != true || original["consumed"] != false {
		t.Fatalf("original pin receipt: %v", original)
	}
	if e = f.control(ctx, map[string]any{"command": "settle-publication", "sessionId": first.conversation.ID()}, nil); e != nil {
		t.Fatal(e)
	}
	archiveControl := map[string]any{"command": "archive-receipts", "sessionId": first.conversation.ID(), "operationIds": []string{"go-persistence-operation"}, "limit": 1}
	if e = f.control(ctx, archiveControl, nil); e == nil {
		t.Fatal("unconsumed receipt archived without explicit trusted consumption")
	}
	archiveControl["consume"] = true
	var archiveResult struct {
		Archived           struct{ Archived, Hot int }
		ExplicitlyConsumed bool
	}
	if e = f.control(ctx, archiveControl, &archiveResult); e != nil {
		t.Fatal(e)
	}
	if !archiveResult.ExplicitlyConsumed || archiveResult.Archived.Archived != 1 || archiveResult.Archived.Hot != 0 {
		t.Fatalf("archive facts: %+v", archiveResult)
	}
	archived := queryReceipt(first)
	original["consumed"] = true
	if persistenceJSON(t, archived) != persistenceJSON(t, original) {
		t.Fatal("archiving changed original receipt")
	}
	// Replaying the original command hits the permanent primary key; changing only
	// operationId must hit the permanent secondary key and conflict without writing.
	assertKeys := func(a active) {
		command["session"] = map[string]any{"sessionContract": "sdk1", "sessionId": a.conversation.ID()}
		before, _, _, _ := trace.snapshot()
		result, err := client.Call(ctx, api.OpTerminalMemoryCommand, api.CallOptions{Params: map[string]string{"id": a.conversation.ID()}, Body: command})
		if err != nil {
			t.Fatal(err)
		}
		if persistenceJSON(t, decode("MemoryReceiptResponse", result.Body)["receipt"]) != persistenceJSON(t, archived) {
			t.Fatal("original command replay changed receipt")
		}
		command["operationId"] = "different-operation"
		_, err = client.Call(ctx, api.OpTerminalMemoryCommand, api.CallOptions{Params: map[string]string{"id": a.conversation.ID()}, Body: command})
		command["operationId"] = "go-persistence-operation"
		var apiError *api.APIError
		if !errors.As(err, &apiError) || apiError.Detail.DomainCode != "request_conflict" {
			t.Fatalf("secondary key must conflict: %v", err)
		}
		after, outputs, _, _ := trace.snapshot()
		hits := map[string]bool{}
		for i := len(before); i < len(after); i++ {
			args := persistenceArgs(t, after[i])
			switch args["action"] {
			case "begin", "put", "commit":
				t.Fatal("permanent replay attempted another write")
			case "lookup":
				var envelope struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				}
				raw, _ := json.Marshal(outputs[i])
				if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope.Content) != 1 {
					t.Fatal("missing real lookup response", err)
				}
				var response map[string]any
				if err := json.Unmarshal([]byte(envelope.Content[0].Text), &response); err != nil {
					t.Fatal(err)
				}
				if response["entry"] != nil {
					hits[args["key"].(map[string]any)["kind"].(string)] = true
				}
			}
		}
		if !hits["primary"] || !hits["secondary"] {
			t.Fatalf("missing real permanent dual-key lookup hits: %v", hits)
		}
	}
	assertKeys(first)
	if e = f.control(ctx, map[string]any{"command": "settle-publication", "sessionId": first.conversation.ID()}, nil); e != nil {
		t.Fatal(e)
	}
	ops, _, lost, queried := trace.snapshot()
	if !lost || !queried {
		t.Fatalf("real accepted commit response loss not reconciled: lost=%v query=%v", lost, queried)
	}
	var commitOp executor.Operation
	for _, op := range ops {
		if op.OperationID == trace.target {
			commitOp = op
		}
	}
	if commitOp.OperationID == "" {
		t.Fatal("original lost commit operation missing")
	}
	status, e := execution.Status(ctx, first.conversation.ID(), commitOp.OperationID)
	if e != nil || status.Receipt == nil || status.Receipt.Status != "completed" || !reflect.DeepEqual(status.Operation, commitOp) {
		t.Fatal("original HTTP execution status changed", e)
	}
	args := persistenceArgs(t, commitOp)
	args["action"] = "query"
	beforeTicket, e := store.Execute(ctx, terminalpersistence.Request(args), first.owner)
	if e != nil {
		t.Fatal(e)
	}
	if beforeTicket["transfer"].(map[string]any)["status"] != "committed" {
		t.Fatal("original committed transfer lost")
	}
	if _, e = first.conversation.Close(ctx, session.WriteOptions{}); e != nil {
		t.Fatal(e)
	}
	if e = f.control(ctx, map[string]any{"command": "set-publication-mode", "mode": "reopen"}, nil); e != nil {
		t.Fatal(e)
	}
	first.stop()
	if e = store.Close(); e != nil {
		t.Fatal(e)
	}
	if e = journal.Close(); e != nil {
		t.Fatal(e)
	}
	options.Mode = "reopen"
	options.AuthorizeRecovery = func(identity terminalpersistence.Identity, _ string, old, current terminalpersistence.Owner) bool {
		return identity == f.PublicationIdentity && reflect.DeepEqual(old, first.owner) && current.Scope == scope
	}
	store, e = terminalpersistence.OpenFileStore(options)
	if e != nil {
		t.Fatal(e)
	}
	journal, e = executor.NewEncryptedFileJournal(journalPath, journalKey)
	if e != nil {
		t.Fatal(e)
	}
	claim, e := journal.Claim(ctx, commitOp)
	if e != nil || claim.Claimed || claim.Receipt == nil || !reflect.DeepEqual(*claim.Receipt, *status.Receipt) {
		t.Fatal("cold journal lost original receipt", e)
	}
	second := start()
	read(second)
	if e = f.control(ctx, map[string]any{"command": "settle-publication", "sessionId": second.conversation.ID()}, nil); e != nil {
		t.Fatal(e)
	}
	afterTicket, e := store.Execute(ctx, terminalpersistence.Request(args), second.owner)
	if e != nil || persistenceJSON(t, afterTicket) != persistenceJSON(t, beforeTicket) {
		t.Fatal("new owner changed original transfer result", e)
	}
	if persistenceJSON(t, queryReceipt(second)) != persistenceJSON(t, archived) {
		t.Fatal("cold source changed permanent receipt")
	}
	assertKeys(second)
	var facts struct {
		ModelExchanges int
		Operations     []struct{ OperationID, Digest, Tool, Action, ReceiptStatus string }
	}
	if e = f.control(ctx, map[string]any{"command": "facts"}, &facts); e != nil {
		t.Fatal(e)
	}
	if facts.ModelExchanges != 0 {
		t.Fatal("storage-only consumer dispatched a model request")
	}
	found := 0
	for _, op := range facts.Operations {
		if op.OperationID == commitOp.OperationID {
			found++
			if op.Digest != commitOp.Digest || op.Tool != executor.TerminalPersistenceToolName || op.ReceiptStatus != "completed" {
				t.Fatal("real spool original fact changed")
			}
		}
	}
	if found != 1 {
		t.Fatal("real spool original operation is not unique")
	}
	if _, e = second.conversation.Close(ctx, session.WriteOptions{}); e != nil {
		t.Fatal(e)
	}
	if e = f.control(ctx, map[string]any{"command": "set-publication-mode", "mode": "reopen"}, nil); e != nil {
		t.Fatal(e)
	}
	second.stop()
	digest := sha256.Sum256([]byte(persistenceJSON(t, beforeTicket)))
	t.Logf("real Serve V1: permanent primary+secondary hits before/after cold reopen; original commit operation=%s digest=%s transfer-result-sha256=%x; accepted response loss queried original; modelExchanges=0", commitOp.OperationID, commitOp.Digest, digest)
}
