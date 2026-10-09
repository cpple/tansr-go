// Command go-memory runs a dedicated encrypted publication host for an existing
// Serve session. Memory extraction and selection remain entirely in Serve.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"

	"github.com/tansrai/tansr-go/api"
	"github.com/tansrai/tansr-go/canonical"
	"github.com/tansrai/tansr-go/examples/internal/demoutil"
	"github.com/tansrai/tansr-go/executor"
	"github.com/tansrai/tansr-go/internal/wire"
	"github.com/tansrai/tansr-go/memorypublication"
)

type options struct{ base, session, executor, workspace, file, journal, mode, access, identity, bindingRequest string }

func flags(f *flag.FlagSet, o *options) {
	f.StringVar(&o.base, "base", "http://127.0.0.1:8787", "Serve origin")
	f.StringVar(&o.session, "session", "", "existing session selected by trusted controller (required)")
	f.StringVar(&o.executor, "executor", "", "executor identity authorized by Serve (required)")
	f.StringVar(&o.workspace, "workspace", "go-memory", "logical workspace, not a filesystem path")
	f.StringVar(&o.file, "file", "", "private encrypted publication file (required)")
	f.StringVar(&o.journal, "journal", "", "private encrypted execution journal directory (required)")
	f.StringVar(&o.mode, "mode", "reopen", "create once or reopen the same medium")
	f.StringVar(&o.access, "access-file", "", "host-controlled live authorization Scope JSON (required)")
	f.StringVar(&o.identity, "identity-file", "", "host-provided publication Identity JSON (required)")
	f.StringVar(&o.bindingRequest, "binding-request", "", "original terminal binding request ID (required; retain on lost reply)")
}
func main() {
	var o options
	flags(flag.CommandLine, &o)
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if e := run(ctx, o); e != nil && !errors.Is(e, context.Canceled) {
		fmt.Fprintln(os.Stderr, "go-memory:", demoutil.Text(demoutil.Describe(e).Error()))
		os.Exit(1)
	}
}
func readJSON(path string, value any) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 8193))
	if e != nil || len(raw) > 8192 {
		return errors.New("bounded host configuration required")
	}
	if _, e = canonical.Decode(raw, canonical.Options{MaxBytes: 8192}); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(value)
}
func run(ctx context.Context, o options) error {
	if o.session == "" || o.executor == "" || o.file == "" || o.journal == "" || o.access == "" || o.identity == "" || o.bindingRequest == "" {
		return errors.New("session, executor, file, journal, access-file, identity-file and binding-request are required")
	}
	key, e := hex.DecodeString(os.Getenv("TANSR_MEMORY_KEY"))
	if e != nil || len(key) != 32 {
		return errors.New("TANSR_MEMORY_KEY must be 64 hex characters from the host key store")
	}
	defer clear(key)
	var identity memorypublication.Identity
	if e = readJSON(o.identity, &identity); e != nil {
		return e
	}
	current := func() (executor.Scope, error) {
		var s executor.Scope
		if e := ctx.Err(); e != nil {
			return s, e
		}
		if e := readJSON(o.access, &s); e != nil {
			return s, e
		}
		if e := wire.Validate("sdk2-ext-v1", "Scope", s); e != nil {
			return s, e
		}
		return s, nil
	}
	scope, e := current()
	if e != nil {
		return e
	}
	check := func() error {
		s, e := current()
		if e != nil {
			return e
		}
		if s != scope {
			return errors.New("current principal or authorization changed; retain original files and restart after host reconciliation")
		}
		return nil
	}
	path, e := filepath.Abs(o.file)
	if e != nil {
		return e
	}
	journalPath, e := filepath.Abs(o.journal)
	if e != nil {
		return e
	}
	// The caller creates a private directory/ACL. Missing recovery data are never rebuilt.
	if o.mode == "reopen" {
		if _, e = os.Stat(filepath.Join(journalPath, ".journal-mode")); e != nil {
			return e
		}
	}
	store, e := memorypublication.OpenFileStore(memorypublication.Options{Path: path, Mode: o.mode, Key: key, Identity: identity, CurrentScope: current})
	if e != nil {
		return e
	}
	defer store.Close()
	journal, e := executor.NewEncryptedFileJournal(journalPath, executor.JournalEncryption{Key: key, ApplicationScopeID: scope.ApplicationScopeID, EndUserID: scope.EndUserID, ExecutorID: o.executor, CheckAccess: check})
	if e != nil {
		return e
	}
	defer journal.Close()
	host, e := memorypublication.NewHost(store, true)
	if e != nil {
		return e
	}
	client, e := demoutil.Client(o.base)
	if e != nil {
		return e
	}
	execution, e := executor.NewClient(client, scope)
	if e != nil {
		return e
	}
	workspace := executor.Workspace{WorkspaceID: o.workspace, Revision: "1"}
	registration := executor.Registration{Protocol: executor.Protocol, ExecutorID: o.executor, Platform: executor.CurrentPlatform(), Workspaces: []executor.Workspace{workspace}, Operations: []string{"tool.invoke"}, Tools: []executor.ToolDefinition{{Name: executor.MemoryPublicationToolName, DefinitionDigest: executor.MemoryPublicationDefinitionDigest}}}
	var expected *executor.Binding
	runner, e := executor.NewRunner(executor.RunnerOptions{Client: execution, Registration: registration, Journal: journal, MemoryPublication: host, Authorize: func(c context.Context, op executor.Operation) error {
		if e := c.Err(); e != nil {
			return e
		}
		if e := check(); e != nil {
			return e
		}
		if expected == nil || op.Scope != scope || op.SessionID != o.session || op.ToolName != "MemoryPublication" || !reflect.DeepEqual(op.Binding, *expected) {
			return executor.ErrConflict
		}
		return nil
	}})
	if e != nil {
		return e
	}
	connection, e := runner.Connect(ctx)
	if e != nil {
		return e
	}
	closure, e := client.SessionCapabilities(ctx, o.session)
	if e != nil {
		return e
	}
	initialized, e := execution.Initialize(ctx, o.session, registration.Platform, nil, closure.ClosureID)
	if e != nil {
		return e
	}
	closure, e = client.SessionCapabilities(ctx, o.session)
	if e != nil {
		return e
	}
	bound, e := execution.Bind(ctx, o.session, connection, workspace, initialized.CapabilityRevision, closure.ClosureID)
	if e != nil {
		return e
	}
	expected = bound.Binding
	if expected == nil {
		return errors.New("execution binding missing")
	}
	ref := map[string]any{"sessionContract": "sdk1", "sessionId": o.session}
	body := map[string]any{"contract": memorypublication.Contract, "requestId": o.bindingRequest, "session": ref, "executionBinding": expected, "required": []string{"memory-lifecycle-v1"}, "optional": []string{}}
	if e = wire.Validate(memorypublication.Contract, "BindingRequest", body); e != nil {
		return e
	}
	result, e := client.Call(ctx, api.OpTerminalBindingCreate, api.CallOptions{Body: body})
	if e != nil {
		return e
	}
	decoded, e := wire.Decode(memorypublication.Contract, "BindingResponse", result.Body)
	if e != nil {
		return e
	}
	response := decoded.(map[string]any)
	raw, _ := canonical.Encode(expected, canonical.Options{})
	got, _ := canonical.Encode(response["executionBinding"], canonical.Options{})
	scopeBytes, _ := canonical.Encode(scope, canonical.Options{})
	returnedScope, _ := canonical.Encode(response["scope"], canonical.Options{})
	refBytes, _ := canonical.Encode(ref, canonical.Options{})
	returnedRef, _ := canonical.Encode(response["session"], canonical.Options{})
	if response["requestId"] != o.bindingRequest || string(raw) != string(got) || string(scopeBytes) != string(returnedScope) || string(refBytes) != string(returnedRef) {
		return executor.ErrConflict
	}
	accepted := false
	for _, f := range response["accepted"].([]any) {
		if f == "memory-lifecycle-v1" {
			accepted = true
		}
	}
	if !accepted {
		return errors.New("memory-lifecycle-v1 unavailable")
	}
	fmt.Println("ready: encrypted publication and execution journal; preserve files, identity, key and original transfer IDs on failure")
	return runner.Run(ctx)
}
