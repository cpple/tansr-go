package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpple/tansr-go/api"
	"github.com/cpple/tansr-go/canonical"
)

func testScope() Scope {
	return Scope{ApplicationScopeID: "app", EndUserID: "user", AuthorizationRevision: "1"}
}
func testConnection() Connection {
	return Connection{Protocol: Protocol, ExecutorID: "device", ConnectionID: "connection", ConnectionRevision: "1", ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), HeartbeatAfterMS: 1000}
}
func testOperation(t *testing.T) Operation {
	t.Helper()
	op := Operation{Protocol: Protocol, OperationID: "op", SessionID: "session", Scope: testScope(), Binding: Binding{BindingID: "binding", Revision: "1", Target: Target{ExecutorID: "device", ConnectionID: "connection", ConnectionRevision: "1", WorkspaceID: "workspace", WorkspaceRevision: "1"}}, ToolName: "Lookup", Request: Resource{Operation: "tool.invoke", Args: map[string]any{"name": "Lookup", "definitionDigest": strings.Repeat("a", 64), "argsJson": "{\"value\":-1.5}"}}, ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	setDigest(t, &op)
	return op
}
func setDigest(t *testing.T, op *Operation) {
	t.Helper()
	var err error
	op.Digest, err = OperationDigest(*op)
	if err != nil {
		t.Fatal(err)
	}
}
func testRegistration() Registration {
	return Registration{Protocol: Protocol, ExecutorID: "device", Platform: CurrentPlatform(), Workspaces: []Workspace{{WorkspaceID: "workspace", Revision: "1"}}, Operations: []string{"tool.invoke"}, Tools: []ToolDefinition{{Name: "Lookup", DefinitionDigest: strings.Repeat("a", 64)}}}
}
func testResult() any {
	return map[string]any{"status": "ok", "content": []any{map[string]any{"t": "text", "text": "client-only-fact"}}}
}
func testClient(t *testing.T, base string) *Client {
	t.Helper()
	raw, err := api.New(api.Options{BaseURL: base, Token: "synthetic-token"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(raw, testScope())
	if err != nil {
		t.Fatal(err)
	}
	return client
}
func testRunner(t *testing.T, client *Client, journal Journal, handle func(context.Context, map[string]any) (any, error)) *Runner {
	t.Helper()
	runner, err := NewRunner(RunnerOptions{Client: client, Registration: testRegistration(), Journal: journal, Tools: map[string]Tool{"Lookup": {DefinitionDigest: strings.Repeat("a", 64), Handle: handle}}, Authorize: func(_ context.Context, op Operation) error {
		if op.SessionID != "session" || op.Binding.BindingID != "binding" {
			return ErrConflict
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	connection := testConnection()
	runner.connection = &connection
	return runner
}
func newJournal(t *testing.T) *FileJournal {
	t.Helper()
	journal, err := NewFileJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	return journal
}

func TestRunnerDurableReplayAndConflictingDigest(t *testing.T) {
	op := testOperation(t)
	calls := 0
	journal := newJournal(t)
	runner := testRunner(t, testClient(t, "http://127.0.0.1:1"), journal, func(_ context.Context, args map[string]any) (any, error) {
		calls++
		if args["value"] != json.Number("-1.5") {
			t.Fatalf("business numbers changed: %v", args)
		}
		return testResult(), nil
	})
	first, err := runner.Execute(context.Background(), op)
	if err != nil || first.Status != "completed" {
		t.Fatalf("first: %+v %v", first, err)
	}
	second, err := runner.Execute(context.Background(), op)
	if err != nil || !equal(first, second) || calls != 1 {
		t.Fatalf("replayed: calls=%d err=%v", calls, err)
	}
	op.Request.Args["argsJson"] = "{}"
	setDigest(t, &op)
	if _, err = runner.Execute(context.Background(), op); !errors.Is(err, ErrConflict) || calls != 1 {
		t.Fatalf("conflict: %v %d", err, calls)
	}
}
func TestRunnerPendingClaimNeverExecutes(t *testing.T) {
	op := testOperation(t)
	directory := t.TempDir()
	journal, err := NewFileJournal(directory)
	if err != nil {
		t.Fatal(err)
	}
	if claim, err := journal.Claim(context.Background(), op); err != nil || !claim.Claimed {
		t.Fatal(claim, err)
	}
	_ = journal.Close()
	journal, err = NewFileJournal(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	calls := 0
	runner := testRunner(t, testClient(t, "http://127.0.0.1:1"), journal, func(context.Context, map[string]any) (any, error) { calls++; return testResult(), nil })
	receipt, err := runner.Execute(context.Background(), op)
	if err != nil || receipt.Status != "unknown" || calls != 0 {
		t.Fatalf("pending reexecuted: %+v %v %d", receipt, err, calls)
	}
	claim, err := journal.Claim(context.Background(), op)
	if err != nil || claim.Receipt == nil || claim.Receipt.Status != "unknown" {
		t.Fatal(claim, err)
	}
}
func TestRunnerRejectsBeforeEffect(t *testing.T) {
	for _, scenario := range []string{"digest", "scope", "generation", "workspace", "unregistered", "expired", "authorization-mutation"} {
		t.Run(scenario, func(t *testing.T) {
			op := testOperation(t)
			calls := 0
			journal := newJournal(t)
			runner := testRunner(t, testClient(t, "http://127.0.0.1:1"), journal, func(context.Context, map[string]any) (any, error) { calls++; return testResult(), nil })
			switch scenario {
			case "digest":
				op.Digest = strings.Repeat("f", 64)
			case "scope":
				op.Scope.EndUserID = "other"
				setDigest(t, &op)
			case "generation":
				op.Binding.Target.ConnectionRevision = "2"
				setDigest(t, &op)
			case "workspace":
				op.Binding.Target.WorkspaceRevision = "2"
				setDigest(t, &op)
			case "unregistered":
				op.Request.Args["definitionDigest"] = strings.Repeat("b", 64)
				setDigest(t, &op)
			case "expired":
				op.ExpiresAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
				setDigest(t, &op)
			case "authorization-mutation":
				runner.options.Authorize = func(_ context.Context, copy Operation) error {
					copy.Request.Args["argsJson"] = "{\"hostile\":true}"
					return ErrConflict
				}
			}
			receipt, err := runner.Execute(context.Background(), op)
			if calls != 0 {
				t.Fatal("handler ran")
			}
			if scenario == "expired" {
				if err != nil || receipt.Status != "failed" {
					t.Fatal(receipt, err)
				}
			} else if err == nil {
				t.Fatal("missing rejection")
			}
		})
	}
}
func TestRunnerUnknownVersusExplicitNoEffect(t *testing.T) {
	for _, scenario := range []string{"error", "panic", "invalid-result", "rejected", "cancelled-after-effect"} {
		t.Run(scenario, func(t *testing.T) {
			op := testOperation(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			journal := newJournal(t)
			runner := testRunner(t, testClient(t, "http://127.0.0.1:1"), journal, func(context.Context, map[string]any) (any, error) {
				calls++
				switch scenario {
				case "error":
					return nil, io.ErrUnexpectedEOF
				case "panic":
					panic("never expose host panic")
				case "invalid-result":
					return map[string]any{"status": "invented"}, nil
				case "rejected":
					return nil, &Rejected{Code: "EACCES"}
				case "cancelled-after-effect":
					cancel()
					return testResult(), nil
				}
				panic("unreachable")
			})
			receipt, err := runner.Execute(ctx, op)
			expected := "unknown"
			if scenario == "rejected" {
				expected = "failed"
			}
			if scenario == "cancelled-after-effect" {
				expected = "completed"
			}
			if err != nil || receipt.Status != expected || calls != 1 {
				t.Fatal(receipt, err, calls)
			}
			claim, err := journal.Claim(context.Background(), op)
			if err != nil || claim.Receipt == nil || claim.Receipt.Status != expected {
				t.Fatal(claim, err)
			}
		})
	}
}
func TestJournalConcurrentClaimsAndCorruption(t *testing.T) {
	op := testOperation(t)
	dir := t.TempDir()
	a, err := NewFileJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := NewFileJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			journal := a
			if i%2 == 0 {
				journal = b
			}
			claim, err := journal.Claim(context.Background(), op)
			if err == nil && claim.Claimed {
				count.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal("not exclusive:", count.Load())
	}
	key, err := journalKey(op)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, key+".receipt"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Claim(context.Background(), op); !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatal(err)
	}
}
func TestBusinessJSONAndDefinitionDigest(t *testing.T) {
	for _, text := range []string{`{"n":-1.25,"nested":{"名称":true}}`, `{"s":"\ud83d\ude00"}`, `{"n":1e3}`} {
		if _, err := toolObject(text); err != nil {
			t.Fatal(text, err)
		}
	}
	for _, text := range []string{`{"s":"\ud800"}`, `{"s":"\udc00"}`, `{"x":1,"x":2}`, `{} {}`, `[]`, `{"n":1e9999}`, "{\"x\":\"\xff\"}", strings.Repeat(" ", 32769) + "{}"} {
		if _, err := toolObject(text); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	decl := map[string]any{"name": "DemoOrderStatus", "description": "Read the status of sample order DEMO-001; this is demonstration data.", "parameters": map[string]any{"orderId": map[string]any{"type": "string", "description": "Sample order ID: DEMO-001"}}, "readOnly": true}
	digest, err := DefinitionDigest(decl)
	// Cross-runtime evidence: original Node kernel/journal/hash.ts canonicalStringify, not a Go
	// reimplementation used as the expected value (2026-10-07 frozen source).
	if err != nil || digest != "fb0ae6b3fd7dbfca35be5f54694fa5d70bf9587234d5d559188fefaa86da65fd" {
		t.Fatal(digest, err)
	}
	indexed := map[string]any{"name": "Indexes", "description": "<>& 中文 \u2028 \u2029", "parameters": map[string]any{"10": map[string]any{"type": "string"}, "2": map[string]any{"type": "number"}, "01": map[string]any{"type": "boolean"}}}
	if digest, err := DefinitionDigest(indexed); err != nil || digest != "05536eedfce5c077e587e5a2e2fc48e77f075cea6c3cb8f3139095f1e7af6491" {
		t.Fatal(digest, err)
	}
	decl["parameters"] = map[string]any{"__proto__": map[string]any{"type": "string"}}
	if _, err = DefinitionDigest(decl); err == nil {
		t.Fatal("prototype key accepted")
	}
	data, err := legacyJSON(map[string]any{"10": int64(10), "2": int64(2), "01": int64(1)})
	if err != nil || string(data) != `{"2":2,"10":10,"01":1}` {
		t.Fatal(string(data), err)
	}
}

func executorHeaders(w http.ResponseWriter) {
	w.Header().Set(api.HeaderContract, api.Contract)
	w.Header().Set(api.HeaderManifestRevision, "7")
	w.Header().Set(api.HeaderSchemaHash, "none")
	w.Header().Set(api.HeaderDomain, "execution")
	w.Header().Set("Content-Type", "application/json")
}
func executorResponse(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	executorHeaders(w)
	data, err := canonical.Encode(value, canonical.Options{MaxBytes: controlBytes})
	if err != nil {
		t.Fatal(err)
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
func operationPath(t *testing.T, name string, params map[string]string) string {
	t.Helper()
	op, ok := api.Lookup(name)
	if !ok {
		t.Fatal(name)
	}
	path, err := api.InstantiatePath(op, params)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunnerLostReceiptReconcilesWithoutExecution(t *testing.T) {
	op := testOperation(t)
	var mu sync.Mutex
	var stored *Receipt
	poll := operationPath(t, api.OpExecutorOperationsPoll, map[string]string{"id": "device"})
	submit := operationPath(t, api.OpExecutorReceiptSubmit, map[string]string{"id": "device"})
	status := operationPath(t, api.OpExecutionStatus, map[string]string{"id": "session", "targetId": "op"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case poll:
			executorResponse(t, w, 200, Batch{Protocol: Protocol, ExecutorID: "device", ConnectionID: "connection", Operations: []Operation{op}})
		case submit:
			data, _ := io.ReadAll(req.Body)
			if _, err := canonical.ParseStrict(data, canonical.Options{MaxBytes: controlBytes}); err != nil {
				t.Error(err)
			}
			var receipt Receipt
			_ = json.Unmarshal(data, &receipt)
			mu.Lock()
			stored = &receipt
			mu.Unlock()
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close()
			}
		case status:
			mu.Lock()
			receipt := stored
			mu.Unlock()
			state := "pending"
			if receipt != nil {
				state = receipt.Status
			}
			executorResponse(t, w, 200, Status{Protocol: Protocol, Operation: op, Status: state, Receipt: receipt})
		default:
			t.Error("unexpected request", req.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	calls := 0
	runner := testRunner(t, testClient(t, server.URL), newJournal(t), func(context.Context, map[string]any) (any, error) { calls++; return testResult(), nil })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runner.options.OnReceipt = func(context.Context, Operation, Receipt) error { cancel(); return nil }
	err := runner.Run(ctx)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("lost receipt: %v calls=%d", err, calls)
	}
}
func TestRunnerLeaseFailureCancelsActiveHandler(t *testing.T) {
	op := testOperation(t)
	poll := operationPath(t, api.OpExecutorOperationsPoll, map[string]string{"id": "device"})
	heartbeat := operationPath(t, api.OpExecutorHeartbeat, map[string]string{"id": "device"})
	status := operationPath(t, api.OpExecutionStatus, map[string]string{"id": "session", "targetId": "op"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case poll:
			executorResponse(t, w, 200, Batch{Protocol: Protocol, ExecutorID: "device", ConnectionID: "connection", Operations: []Operation{op}})
		case status:
			executorResponse(t, w, 200, Status{Protocol: Protocol, Operation: op, Status: "pending", Receipt: nil})
		case heartbeat:
			conn := testConnection()
			conn.ConnectionID = "other"
			executorResponse(t, w, 200, conn)
		default:
			executorHeaders(w)
			w.WriteHeader(503)
		}
	}))
	defer server.Close()
	var calls atomic.Int32
	journal := newJournal(t)
	runner := testRunner(t, testClient(t, server.URL), journal, func(ctx context.Context, _ map[string]any) (any, error) {
		calls.Add(1)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runner.Run(ctx); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	claim, err := journal.Claim(context.Background(), op)
	if err != nil || claim.Receipt == nil || claim.Receipt.Status != "unknown" {
		t.Fatal(claim, err)
	}
}

func TestClientRejectsTamperedBatchAndMissingFields(t *testing.T) {
	op := testOperation(t)
	for _, scenario := range []string{"scope", "digest", "duplicates", "missing", "whitespace", "extra"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				item := clone(op)
				batch := Batch{Protocol: Protocol, ExecutorID: "device", ConnectionID: "connection", Operations: []Operation{item}}
				switch scenario {
				case "scope":
					batch.Operations[0].Scope.EndUserID = "other"
					setDigest(t, &batch.Operations[0])
				case "digest":
					batch.Operations[0].Digest = strings.Repeat("0", 64)
				case "duplicates":
					batch.Operations = append(batch.Operations, item)
				}
				data, _ := canonical.Encode(batch, canonical.Options{MaxBytes: controlBytes})
				if scenario == "missing" {
					var value map[string]any
					_ = json.Unmarshal(data, &value)
					delete(value, "operations")
					data, _ = canonical.Encode(value, canonical.Options{MaxBytes: controlBytes})
				}
				if scenario == "extra" {
					data = append([]byte(`{"extra":true,`), data[1:]...)
				}
				if scenario == "whitespace" {
					data = append(data, '\n')
				}
				executorHeaders(w)
				_, _ = w.Write(data)
			}))
			defer server.Close()
			if _, err := testClient(t, server.URL).Poll(context.Background(), testConnection()); err == nil {
				t.Fatal("accepted tampering")
			}
		})
	}
}

func TestConcurrentPublicExecuteIsCancelledWhenRunnerLeaseFails(t *testing.T) {
	op := testOperation(t)
	poll := operationPath(t, api.OpExecutorOperationsPoll, map[string]string{"id": "device"})
	heartbeat := operationPath(t, api.OpExecutorHeartbeat, map[string]string{"id": "device"})
	status := operationPath(t, api.OpExecutionStatus, map[string]string{"id": "session", "targetId": "op"})
	polled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case poll:
			select {
			case polled <- struct{}{}:
			default:
			}
			executorResponse(t, w, 200, Batch{Protocol: Protocol, ExecutorID: "device", ConnectionID: "connection", Operations: []Operation{}})
		case heartbeat:
			connection := testConnection()
			connection.ConnectionID = "changed"
			executorResponse(t, w, 200, connection)
		case status:
			executorResponse(t, w, 200, Status{Protocol: Protocol, Operation: op, Status: "pending", Receipt: nil})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	runner := testRunner(t, testClient(t, server.URL), newJournal(t), func(ctx context.Context, _ map[string]any) (any, error) { <-ctx.Done(); return nil, ctx.Err() })
	runCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runResult := make(chan error, 1)
	go func() { runResult <- runner.Run(runCtx) }()
	select {
	case <-polled:
	case <-runCtx.Done():
		t.Fatal(runCtx.Err())
	}
	executeResult := make(chan error, 1)
	go func() {
		receipt, err := runner.Execute(context.Background(), op)
		if err == nil && receipt.Status != "unknown" {
			err = ErrConflict
		}
		executeResult <- err
	}()
	select {
	case err := <-runResult:
		if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	case <-runCtx.Done():
		t.Fatal(runCtx.Err())
	}
	select {
	case err := <-executeResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("public Execute continued after lease failure")
	}
}

func TestStatusHookChecksFactsAndCancelsForgedResponse(t *testing.T) {
	for _, forged := range []bool{false, true} {
		t.Run(strconv.FormatBool(forged), func(t *testing.T) {
			op := testOperation(t)
			poll := operationPath(t, api.OpExecutorOperationsPoll, map[string]string{"id": "device"})
			submit := operationPath(t, api.OpExecutorReceiptSubmit, map[string]string{"id": "device"})
			var controllerReads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case poll:
					executorResponse(t, w, 200, Batch{Protocol: Protocol, ExecutorID: "device", ConnectionID: "connection", Operations: []Operation{op}})
				case submit:
					var receipt Receipt
					_ = json.NewDecoder(req.Body).Decode(&receipt)
					executorResponse(t, w, 200, Status{Protocol: Protocol, Operation: op, Status: receipt.Status, Receipt: &receipt})
				default:
					controllerReads.Add(1)
					w.WriteHeader(403)
				}
			}))
			defer server.Close()
			runner := testRunner(t, testClient(t, server.URL), newJournal(t), func(ctx context.Context, _ map[string]any) (any, error) { <-ctx.Done(); return nil, ctx.Err() })
			var callbacks atomic.Int32
			runner.options.Status = func(ctx context.Context, operation Operation) (Status, error) {
				callbacks.Add(1)
				if forged {
					operation.SessionID = "foreign-session"
					setDigest(t, &operation)
				}
				return Status{Protocol: Protocol, Operation: operation, Status: "unknown", Receipt: nil}, nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			runner.options.OnReceipt = func(context.Context, Operation, Receipt) error { cancel(); return nil }
			err := runner.Run(ctx)
			if forged && !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
			if !forged && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if callbacks.Load() < 1 || controllerReads.Load() != 0 {
				t.Fatal("callback did not replace controller route", callbacks.Load(), controllerReads.Load())
			}
		})
	}
}

func TestExecutorOnlyStatusUsesRestrictedOperation(t *testing.T) {
	op := testOperation(t)
	connection := testConnection()
	session := TerminalSessionReference{SessionContract: "sdk1", SessionID: op.SessionID}
	expectedPath := operationPath(t, api.OpTerminalExecutionState, map[string]string{"id": connection.ExecutorID, "targetId": op.OperationID})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != expectedPath || req.URL.Query().Get("sessionId") != op.SessionID || req.URL.Query().Get("requestDigest") != op.Digest || req.URL.Query().Get("connectionId") != connection.ConnectionID || req.URL.Query().Get("sessionContract") != "sdk1" {
			t.Error("wrong restricted query", req.URL)
		}
		executorHeaders(w)
		w.Header().Set(api.HeaderDomain, "terminal")
		data, err := canonical.Encode(map[string]any{"contract": "terminal-services-v1", "session": session, "execution": Status{Protocol: Protocol, Operation: op, Status: "pending", Receipt: nil}}, canonical.Options{MaxBytes: controlBytes})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	state, err := testClient(t, server.URL).ExecutorStatus(context.Background(), session, connection, op)
	if err != nil || state.Status != "pending" {
		t.Fatal(state, err)
	}
}
