package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tansrai/tansr-go/api"
	"github.com/tansrai/tansr-go/executor"
	"github.com/tansrai/tansr-go/internal/wire"
	"github.com/tansrai/tansr-go/memorypublication"
	"github.com/tansrai/tansr-go/session"
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
