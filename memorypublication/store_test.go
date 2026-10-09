package memorypublication

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tansrai/tansr-go/executor"
)

func fixture(t *testing.T) (Options, Owner) {
	t.Helper()
	owner := Owner{Scope: executor.Scope{ApplicationScopeID: "app", EndUserID: "user", AuthorizationRevision: "1"}, SessionID: "session", Binding: executor.Binding{BindingID: "binding", Revision: "1", Target: executor.Target{ExecutorID: "device", ConnectionID: "connection", ConnectionRevision: "1", WorkspaceID: "workspace", WorkspaceRevision: "1"}}}
	directory, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	opts := Options{Path: filepath.Join(directory, "memory.bin"), Mode: "create", Key: bytes.Repeat([]byte{7}, 32), Identity: Identity{"app", "user", "source", "1", "domain"}, CurrentScope: func() (executor.Scope, error) { return owner.Scope, nil }}
	return opts, owner
}
func request(action string) Request {
	return Request{"contract": Contract, "action": action, "sourceId": "source", "sourceGeneration": "1", "domainKey": "domain"}
}
func begin(id string, body []byte, expected any) Request {
	r := request("begin")
	r["transferId"] = id
	r["byteLength"] = len(body)
	r["sha256"] = hash(body)
	r["expectedEtag"] = expected
	return r
}
func chunk(id string, body []byte, off int) Request {
	r := request("chunk")
	r["transferId"] = id
	r["byteLength"] = len(body)
	r["base64"] = base64.StdEncoding.EncodeToString(body)
	r["offset"] = off
	r["payloadDigest"] = hash(body)
	return r
}
func transferRequest(action, id string) Request { r := request(action); r["transferId"] = id; return r }
func execute(t *testing.T, s *FileStore, r Request, o Owner) Response {
	t.Helper()
	v, e := s.Execute(context.Background(), r, o)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func publish(t *testing.T, s *FileStore, id string, b []byte, expected any, o Owner) Response {
	t.Helper()
	execute(t, s, begin(id, b, expected), o)
	execute(t, s, chunk(id, b, 0), o)
	return execute(t, s, transferRequest("commit", id), o)
}
func status(r Response) string { return r["transfer"].(map[string]any)["status"].(string) }
func TestDurableCASReplayReadAndCapacity(t *testing.T) {
	opts, o := fixture(t)
	opts.Limits = Limits{2, 4096, 32768}
	s, e := OpenFileStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = OpenFileStore(opts); e == nil {
		t.Fatal("second owner opened")
	}
	body := []byte("PST-go-sensitive-正文")
	execute(t, s, begin("first", body, nil), o)
	execute(t, s, begin("second", []byte("loser"), nil), o)
	execute(t, s, chunk("first", body, 0), o)
	execute(t, s, chunk("first", body, 0), o)
	if _, e = s.Execute(context.Background(), chunk("first", []byte("changed"), 0), o); e == nil {
		t.Fatal("different replay accepted")
	}
	committed := execute(t, s, transferRequest("commit", "first"), o)
	if status(committed) != "committed" {
		t.Fatal(committed)
	}
	execute(t, s, chunk("second", []byte("loser"), 0), o)
	if status(execute(t, s, transferRequest("commit", "second"), o)) != "conflict" {
		t.Fatal("CAS overwritten")
	}
	s.Close()
	opts.Mode = "reopen"
	s, e = OpenFileStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if got := execute(t, s, transferRequest("commit", "first"), o); !equal(got, committed) {
		t.Fatal("lost reply replay differs")
	}
	if status(execute(t, s, transferRequest("commit", "missing"), o)) != "unknown" {
		t.Fatal("unknown recreated")
	}
	if _, e = s.Execute(context.Background(), begin("third", body, hash(body)), o); !errors.Is(e, AdapterError("capacity_exceeded")) {
		t.Fatal("capacity", e)
	}
	head := execute(t, s, request("head"), o)
	if head["publication"].(map[string]any)["etag"] != hash(body) {
		t.Fatal(head)
	}
	r := request("read")
	r["etag"] = hash(body)
	r["offset"] = 0
	r["length"] = MaxChunk
	read := execute(t, s, r, o)
	if read["base64"] != base64.StdEncoding.EncodeToString(body) {
		t.Fatal("body changed")
	}
	r["etag"] = "stale"
	if _, e = s.Execute(context.Background(), r, o); !errors.Is(e, AdapterError("revision_conflict")) {
		t.Fatal(e)
	}
	c, e := s.Capacity()
	if e != nil || c.StoredTransfers != 2 || c.StagingBytes != 0 {
		t.Fatal(c, e)
	}
	raw, e := os.ReadFile(opts.Path)
	if e != nil {
		t.Fatal(e)
	}
	for _, probe := range [][]byte{body, []byte(base64.StdEncoding.EncodeToString(body)), []byte("session"), []byte("first")} {
		if bytes.Contains(raw, probe) {
			t.Fatal("plaintext in medium")
		}
	}
}
func TestOwnerFenceRevocationAndReadOnlyRecovery(t *testing.T) {
	opts, o := fixture(t)
	current := o.Scope
	revoked := false
	opts.CurrentScope = func() (executor.Scope, error) {
		if revoked {
			return current, errors.New("revoked")
		}
		return current, nil
	}
	opts.AuthorizeRecovery = func(_ Identity, _ string, original, now Owner) bool {
		return original.Binding.Target.ConnectionID == "connection" && now.Binding.Target.ConnectionID == "new"
	}
	s, e := OpenFileStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	body := []byte("memory")
	execute(t, s, begin("pending", body, nil), o)
	newer := o
	newer.Binding.Target.ConnectionID = "new"
	newer.Binding.Target.ConnectionRevision = "2"
	if status(execute(t, s, transferRequest("query", "pending"), newer)) != "staging" {
		t.Fatal("recovery query")
	}
	for _, r := range []Request{begin("pending", body, nil), chunk("pending", body, 0), transferRequest("commit", "pending")} {
		if _, e = s.Execute(context.Background(), r, newer); !errors.Is(e, AdapterError("request_conflict")) {
			t.Fatal("foreign owner wrote", e)
		}
	}
	current.AuthorizationRevision = "2"
	if _, e = s.Execute(context.Background(), request("head"), o); e == nil {
		t.Fatal("stale authorization")
	}
	current = o.Scope
	revoked = true
	if _, e = s.Execute(context.Background(), request("head"), o); e == nil {
		t.Fatal("revoked read")
	}
	revoked = false
	r := request("head")
	r["sourceGeneration"] = "2"
	if _, e = s.Execute(context.Background(), r, o); !errors.Is(e, AdapterError("stale_generation")) {
		t.Fatal(e)
	}
}
func TestWrongKeyIdentityCorruptionAndMissingRetainOriginal(t *testing.T) {
	opts, o := fixture(t)
	s, e := OpenFileStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	publish(t, s, "one", []byte("encrypted"), nil, o)
	s.Close()
	opts.Mode = "reopen"
	original, _ := os.ReadFile(opts.Path)
	wrong := opts
	wrong.Key = bytes.Repeat([]byte{8}, 32)
	if _, e = OpenFileStore(wrong); !errors.Is(e, ErrIntegrity) {
		t.Fatal("wrong key", e)
	}
	wrong = opts
	wrong.Identity.DomainKey = "other"
	if _, e = OpenFileStore(wrong); !errors.Is(e, ErrIntegrity) {
		t.Fatal("wrong AAD", e)
	}
	after, _ := os.ReadFile(opts.Path)
	if !bytes.Equal(after, original) {
		t.Fatal("original overwritten")
	}
	for _, raw := range [][]byte{original[:len(original)-1], append([]byte{}, original...)} {
		raw[len(raw)-1] ^= 1
		if e = os.WriteFile(opts.Path, raw, 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = OpenFileStore(opts); !errors.Is(e, ErrIntegrity) {
			t.Fatal("corrupt opened", e)
		}
		after, _ = os.ReadFile(opts.Path)
		if !bytes.Equal(after, raw) {
			t.Fatal("corrupt overwritten")
		}
	}
	if e = os.Remove(opts.Path); e != nil {
		t.Fatal(e)
	}
	if _, e = OpenFileStore(opts); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("missing recreated", e)
	}
}
func TestStagingReopenAndInvalidBody(t *testing.T) {
	opts, o := fixture(t)
	s, e := OpenFileStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	body := []byte("中文-buffer")
	execute(t, s, begin("one", body, nil), o)
	execute(t, s, chunk("one", body[:3], 0), o)
	s.Close()
	opts.Mode = "reopen"
	s, e = OpenFileStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if _, e = s.Execute(context.Background(), chunk("one", body[4:], 4), o); e == nil {
		t.Fatal("gap accepted")
	}
	execute(t, s, chunk("one", body[3:], 3), o)
	execute(t, s, transferRequest("commit", "one"), o)
	invalid := []byte{255}
	execute(t, s, begin("bad", invalid, hash(body)), o)
	execute(t, s, chunk("bad", invalid, 0), o)
	if _, e = s.Execute(context.Background(), transferRequest("commit", "bad"), o); !errors.Is(e, AdapterError("integrity_mismatch")) {
		t.Fatal(e)
	}
	r := request("head")
	r["unknown"] = true
	if _, e = s.Execute(context.Background(), r, o); e == nil {
		t.Fatal("extra wire field")
	}
}
func TestAuthFailureBeforeAndAfterPublicationCommit(t *testing.T) {
	for _, afterCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after"}[afterCommit], func(t *testing.T) {
			opts, o := fixture(t)
			calls, failAt := 0, 0
			opts.CurrentScope = func() (executor.Scope, error) {
				calls++
				if calls == failAt {
					return o.Scope, errors.New("revoked")
				}
				return o.Scope, nil
			}
			s, e := OpenFileStore(opts)
			if e != nil {
				t.Fatal(e)
			}
			body := []byte("body")
			execute(t, s, begin("one", body, nil), o)
			execute(t, s, chunk("one", body, 0), o)
			calls = 0
			failAt = 2
			if afterCommit {
				failAt = 3
			}
			_, e = s.Execute(context.Background(), transferRequest("commit", "one"), o)
			if e == nil {
				t.Fatal("auth failure accepted")
			}
			if afterCommit && !errors.Is(e, ErrUnknown) {
				t.Fatal(e)
			}
			s.Close()
			failAt = 0
			opts.Mode = "reopen"
			s, e = OpenFileStore(opts)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			got := status(execute(t, s, transferRequest("query", "one"), o))
			want := "staging"
			if afterCommit {
				want = "committed"
			}
			if got != want {
				t.Fatal(got, want)
			}
		})
	}
}
func TestHostUsesOriginalOperationAndRejectsPlaintextRequirement(t *testing.T) {
	opts, o := fixture(t)
	s, e := OpenFileStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	h, e := NewHost(s, true)
	if e != nil {
		t.Fatal(e)
	}
	r := request("head")
	raw, _ := json.Marshal(r)
	op := executor.Operation{Protocol: executor.Protocol, OperationID: "op", SessionID: o.SessionID, Scope: o.Scope, Binding: o.Binding, ToolName: "MemoryPublication", ExpiresAt: "2030-01-01T00:00:00Z", Request: executor.Resource{Operation: "tool.invoke", Args: map[string]any{"name": executor.MemoryPublicationToolName, "definitionDigest": executor.MemoryPublicationDefinitionDigest, "argsJson": string(raw)}}}
	op.Digest, e = executor.OperationDigest(op)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.Execute(context.Background(), op, r); e != nil {
		t.Fatal(e)
	}
	r["sourceId"] = "other"
	if _, e = h.Execute(context.Background(), op, r); e == nil {
		t.Fatal("swapped input")
	}
}

func TestCancellationAtCommitBoundaryPreservesStage(t *testing.T) {
	opts, o := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls, armed := 0, false
	opts.CurrentScope = func() (executor.Scope, error) {
		if armed {
			calls++
			if calls == 2 {
				cancel()
			}
		}
		return o.Scope, nil
	}
	s, e := OpenFileStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	body := []byte("cancel")
	execute(t, s, begin("one", body, nil), o)
	execute(t, s, chunk("one", body, 0), o)
	armed = true
	if _, e = s.Execute(ctx, transferRequest("commit", "one"), o); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	armed = false
	if got := status(execute(t, s, transferRequest("query", "one"), o)); got != "staging" {
		t.Fatal(got)
	}
}
