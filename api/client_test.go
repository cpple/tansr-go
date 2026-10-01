package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpple/tansr-go/canonical"
)

// fakeServe is an httptest stand-in for tansr Serve built strictly from the unified-v1 golden bodies.
// It records every request so tests can assert what the client sent — and that no /v2 or /v3 path
// was ever attempted.
type fakeServe struct {
	t      *testing.T
	golden map[string]*goldenVector
	mu     sync.Mutex
	seen   []*http.Request
	acks   int
	// knobs
	dropContract   bool
	capabilitiesFn func(map[string]any)
	closureFn      func(map[string]any)
	echoEnvelope   bool
	closureID      string
}

func newFakeServe(t *testing.T) (*fakeServe, *httptest.Server) {
	_, byName := loadGolden(t)
	f := &fakeServe{t: t, golden: byName, echoEnvelope: true}
	closure := materialise(t, byName["closure-full-enabled"], byName).(map[string]any)
	f.closureID = closure["closureId"].(string)
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	return f, server
}

func (f *fakeServe) body(name string) map[string]any {
	return materialise(f.t, f.golden[name], f.golden).(map[string]any)
}

func (f *fakeServe) requests() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request(nil), f.seen...)
}

func (f *fakeServe) unified(w http.ResponseWriter, domain string) {
	h := w.Header()
	h.Set(HeaderContract, Contract)
	h.Set(HeaderManifestRevision, "4")
	h.Set(HeaderDomain, domain)
	if domain == "discovery" {
		h.Set(HeaderSchemaHash, "sha256:"+ManifestSchemaHash)
	} else {
		h.Set(HeaderSchemaHash, "none")
	}
	h.Set(HeaderTraceID, "trace-1")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeServe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.seen = append(f.seen, r.Clone(context.Background()))
	f.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/v2/") || strings.HasPrefix(r.URL.Path, "/v3/") {
		f.t.Errorf("client attempted legacy path %s", r.URL.Path)
		http.Error(w, "legacy", http.StatusNotFound)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		f.unified(w, "discovery")
		writeJSON(w, 401, f.body("facade-error-unauthorized"))
		return
	}
	if f.dropContract {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "<html>proxy</html>")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	switch {
	case r.URL.Path == "/api/manifest" && r.Method == "GET":
		f.unified(w, "discovery")
		writeJSON(w, 200, f.body("manifest-runtime-view"))
	case r.URL.Path == "/api/capabilities" && r.Method == "GET":
		f.unified(w, "discovery")
		body := f.body("capabilities-offload-deployment")
		if f.capabilitiesFn != nil {
			f.capabilitiesFn(body)
		}
		writeJSON(w, 200, body)
	case len(parts) == 4 && parts[1] == "sessions" && parts[3] == "capabilities" && r.Method == "GET":
		f.unified(w, "discovery")
		body := f.body("closure-full-enabled")
		if f.closureFn != nil {
			f.closureFn(body)
		}
		w.Header().Set(HeaderClosureID, f.closureID)
		writeJSON(w, 200, body)
	case r.URL.Path == "/api/sessions" && r.Method == "POST":
		f.unified(w, "session")
		writeJSON(w, 201, map[string]any{"id": "s-1", "status": "idle"})
	case len(parts) == 4 && parts[1] == "sessions" && parts[3] == "messages" && r.Method == "POST":
		f.unified(w, "session")
		if cid := r.Header.Get(HeaderClosureID); cid != "" && cid != f.closureID {
			w.Header().Set(HeaderClosureID, f.closureID)
			writeJSON(w, 412, f.body("facade-error-precondition-failed"))
			return
		}
		writeJSON(w, 202, map[string]any{"accepted": true, "turnId": "t-1"})
	case len(parts) == 4 && parts[1] == "sessions" && parts[3] == "history" && r.Method == "GET":
		// legacy-looking deployment: unified headers missing entirely
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"messages":[]}`)
	case len(parts) == 4 && parts[1] == "sessions" && parts[3] == "events" && r.Method == "GET":
		f.unified(w, "session")
		negotiated := r.Header.Get(HeaderEventEnvelope) == Contract
		if negotiated && f.echoEnvelope {
			w.Header().Set(HeaderEventEnvelope, Contract)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		if negotiated && f.echoEnvelope {
			first, _ := json.Marshal(f.body("event-envelope-session-text-delta"))
			last, _ := json.Marshal(f.body("event-envelope-terminal-completed"))
			_, _ = io.WriteString(w, ": keep-alive\n\nid: 12\nevent: msg.text.delta\ndata: "+string(first)+"\n\n")
			if r.Header.Get("Last-Event-ID") == "" {
				_, _ = io.WriteString(w, "id: 88\nevent: tool.completed\ndata: "+string(last)+"\n\n")
			}
			return
		}
		_, _ = io.WriteString(w, "id: 1\nevent: msg.text.delta\ndata: {\"type\":\"msg.text.delta\",\"text\":\"raw\"}\n\n")
	case len(parts) == 6 && parts[1] == "archive" && parts[5] == "acks" && r.Method == "POST":
		f.unified(w, "archive")
		raw, _ := io.ReadAll(r.Body)
		if _, err := canonical.ParseStrict(raw, canonical.Options{MaxBytes: 262144}); err != nil {
			writeJSON(w, 400, f.body("unified-error-not-canonical"))
			return
		}
		attempt := 0
		if parts[3] == "b-busy" {
			f.mu.Lock()
			f.acks++
			attempt = f.acks
			f.mu.Unlock()
		}
		if parts[3] == "b-busy" && attempt == 1 {
			w.Header().Set(HeaderRetryAfter, "1")
			body := f.body("facade-error-capacity-exceeded")
			writeJSON(w, 503, body)
			return
		}
		if parts[3] == "b-unknown" {
			writeJSON(w, 503, f.body("unified-error-cache-result-unknown"))
			return
		}
		writeJSON(w, 200, map[string]any{"protocol": "sdk2-ext-v1", "coverage": map[string]any{"throughSequence": "9"}})
	case len(parts) == 5 && parts[1] == "archive" && parts[4] == "events":
		// archive stream that ignores the envelope negotiation (no echo)
		f.unified(w, "archive")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "data: {}\n\n")
	default:
		f.unified(w, "discovery")
		writeJSON(w, 404, f.body("facade-error-not-found"))
	}
}

func newClient(t *testing.T, server *httptest.Server, envelope bool) *Client {
	t.Helper()
	c, err := New(Options{BaseURL: server.URL, Token: "t0ken", SessionFamily: "sdk2-offload-v1", EventEnvelope: envelope})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewOptions(t *testing.T) {
	for _, bad := range []Options{
		{BaseURL: "http://h/", Token: ""},
		{BaseURL: "http://h/path", Token: "t"},
		{BaseURL: "ftp://h", Token: "t"},
		{BaseURL: "http://u:p@h", Token: "t"},
		{BaseURL: "http://h", Token: "t", SessionFamily: "sdk3"},
		{BaseURL: "http://h", Token: "bad token"},
		{BaseURL: "http://h", Token: "t", MaxResponseBytes: 10},
	} {
		if _, err := New(bad); err == nil {
			t.Fatalf("expected invalid_options for %+v", bad)
		}
	}
	c, err := New(Options{BaseURL: "HTTP://Host:8080/", Authorize: func(context.Context, http.Header) error { return nil }})
	if err != nil || c.BaseURL() != "http://host:8080" {
		t.Fatalf("origin normalisation: %v %q", err, c.BaseURL())
	}
	if Locked().ManifestRevision != 4 {
		t.Fatal("locked revision")
	}
}

// Discipline 2 + 4 + 7: discovery walks the manifest → capabilities → closure using only local
// templates, sends identity in the Authorization header and never touches /v2 or /v3.
func TestDiscoveryFlow(t *testing.T) {
	f, server := newFakeServe(t)
	c := newClient(t, server, false)
	ctx := context.Background()

	manifest, err := c.Manifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Body.Revision != 4 || manifest.Body.Runtime == nil || manifest.Meta.Domain != "discovery" {
		t.Fatalf("manifest: %+v", manifest.Body.Revision)
	}
	caps, err := c.Capabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if caps.Body.Domains.Session.Contract != "agent-session-v1" || caps.Body.ManifestRevision != 4 {
		t.Fatalf("capabilities: %+v", caps.Body)
	}
	closure, err := c.SessionCapabilities(ctx, "s 1")
	if err != nil {
		t.Fatal(err)
	}
	if closure.ClosureID != f.closureID || closure.Closure.Operations[OpSessionMessageSend] != StateEnabled {
		t.Fatalf("closure: %+v", closure)
	}
	obs, ok := c.Observed()
	if !ok || obs.ManifestRevision != 4 {
		t.Fatalf("observed: %+v", obs)
	}
	var paths []string
	for _, r := range f.requests() {
		paths = append(paths, r.URL.RequestURI())
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer t0ken") || r.Header.Get(HeaderSessionFamily) != "sdk2-offload-v1" {
			t.Fatalf("request headers: %v", r.Header)
		}
	}
	if strings.Join(paths, " ") != "/api/manifest /api/capabilities /api/sessions/s%201/capabilities" {
		t.Fatalf("paths sent: %v", paths)
	}
}

// Discipline 2: navigation fields in discovery bodies are rejected, not followed.
func TestDiscoveryRejectsNavigationFields(t *testing.T) {
	f, server := newFakeServe(t)
	c := newClient(t, server, false)
	f.capabilitiesFn = func(body map[string]any) {
		body["domains"].(map[string]any)["archive"].(map[string]any)["entry"] = "/v3/sdk2"
	}
	_, err := c.Capabilities(context.Background())
	var cerr *ClientError
	if !errors.As(err, &cerr) || cerr.Code != CodeInvalidResponse {
		t.Fatalf("expected invalid_response, got %v", err)
	}
	// identity in the closure body is a contract violation too (discipline 4)
	f.closureFn = func(body map[string]any) { body["sessionId"] = "s-1" }
	_, err = c.SessionCapabilities(context.Background(), "s-1")
	if !errors.As(err, &cerr) || cerr.Code != CodeInvalidResponse {
		t.Fatalf("expected invalid_response for identity field, got %v", err)
	}
	// header / body closure id disagreement
	f.closureFn = func(body map[string]any) {
		body["closureId"] = strings.Repeat("a", 64)
	}
	_, err = c.SessionCapabilities(context.Background(), "s-1")
	if !errors.As(err, &cerr) || cerr.Code != CodeInvalidResponse {
		t.Fatalf("expected invalid_response for closure id mismatch, got %v", err)
	}
	for _, r := range f.requests() {
		if strings.Contains(r.URL.Path, "/v3/") {
			t.Fatal("navigation field was followed")
		}
	}
}

// Discipline 1: missing unified headers → ErrContractUnavailable, no legacy fallback.
func TestContractUnavailable(t *testing.T) {
	f, server := newFakeServe(t)
	c := newClient(t, server, false)
	_, err := c.Call(context.Background(), OpSessionHistoryRead, CallOptions{Params: map[string]string{"id": "s-1"}})
	var cu *ContractUnavailableError
	if !errors.Is(err, ErrContractUnavailable) || !errors.As(err, &cu) || cu.Reason != ReasonMissingContractHeader {
		t.Fatalf("expected missing_contract_header, got %v", err)
	}
	f.dropContract = true
	_, err = c.Manifest(context.Background())
	if !errors.As(err, &cu) || cu.Reason != ReasonMissingContractHeader || cu.ContentType != "text/html" {
		t.Fatalf("expected contract unavailable on html page, got %v", err)
	}
	if len(f.requests()) != 2 {
		t.Fatalf("client must not retry with another prefix; saw %d requests", len(f.requests()))
	}
}

// Disciplines 1, 5, 6: envelope negotiation is explicit; EOF is not completion; cursors stay apart.
func TestEventsNegotiated(t *testing.T) {
	f, server := newFakeServe(t)
	c := newClient(t, server, true)
	var opened *Meta
	stream, err := c.Events(context.Background(), OpSessionEventsObserve, EventsOptions{Params: map[string]string{"id": "s-1"}, OnOpen: func(m Meta) { opened = &m }})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if opened == nil || !opened.EventEnvelope {
		t.Fatal("OnOpen must see the echoed negotiation header")
	}
	first, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if first.Envelope == nil || first.Envelope.IsTerminal() || first.ID != "12" {
		t.Fatalf("first frame: %+v", first)
	}
	if cur, ok := first.Envelope.CursorSet.EventCursor.Text(); !ok || cur != "12" || !first.Envelope.CursorSet.OutputWatermark.IsNull() {
		t.Fatalf("cursor set: %+v", first.Envelope.CursorSet)
	}
	second, err := stream.Next()
	if err != nil {
		t.Fatal(err)
	}
	if !second.Envelope.IsTerminal() || *second.Envelope.TerminalStatus != TerminalCompleted {
		t.Fatalf("second frame: %+v", second.Envelope)
	}
	if wm, ok := second.Envelope.CursorSet.OutputWatermark.Text(); !ok || wm != "88" || !second.Envelope.CursorSet.EventCursor.IsNull() {
		t.Fatalf("terminal cursor set: %+v", second.Envelope.CursorSet)
	}
	if _, err := stream.Next(); err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
	if stream.LastEventID() != "88" {
		t.Fatalf("LastEventID follows id: fields only, got %q", stream.LastEventID())
	}
	// resumption sends Last-Event-ID and nothing else changes
	resumed, err := c.Events(context.Background(), OpSessionEventsObserve, EventsOptions{Params: map[string]string{"id": "s-1"}, LastEventID: stream.LastEventID()})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if _, err := resumed.Next(); err != nil {
		t.Fatal(err)
	}
	if _, err := resumed.Next(); err != io.EOF {
		t.Fatalf("resumed stream: %v", err)
	}
	reqs := f.requests()
	if reqs[1].Header.Get("Last-Event-ID") != "88" || reqs[1].Header.Get(HeaderEventEnvelope) != Contract || reqs[1].Header.Get("Accept") != "text/event-stream" {
		t.Fatalf("resume headers: %v", reqs[1].Header)
	}
	// server that does not echo → explicit failure, never raw frames
	_, err = c.Events(context.Background(), OpArchiveEventsObserve, EventsOptions{Params: map[string]string{"id": "b-1"}})
	if !errors.Is(err, ErrEnvelopeNotNegotiated) {
		t.Fatalf("expected ErrEnvelopeNotNegotiated, got %v", err)
	}
	// echo without request → contract inconsistency
	f.echoEnvelope = true
	raw := newClient(t, server, false)
	rawStream, err := raw.Events(context.Background(), OpSessionEventsObserve, EventsOptions{Params: map[string]string{"id": "s-1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer rawStream.Close()
	frame, err := rawStream.Next()
	if err != nil || frame.Envelope != nil || !strings.Contains(frame.Data, "raw") {
		t.Fatalf("raw frame: %+v %v", frame, err)
	}
	// Call on a stream operation and Events on a call operation are local errors
	var cerr *ClientError
	if _, err := c.Call(context.Background(), OpSessionEventsObserve, CallOptions{}); !errors.As(err, &cerr) || cerr.Code != CodeInvalidOperation {
		t.Fatalf("expected invalid_operation, got %v", err)
	}
	if _, err := c.Events(context.Background(), OpSessionGet, EventsOptions{}); !errors.As(err, &cerr) || cerr.Code != CodeInvalidOperation {
		t.Fatalf("expected invalid_operation, got %v", err)
	}
}

// Discipline 5 + closure preconditions: 202 is acceptance only; a stale closure id yields 412
// precondition_failed / rediscover with the current closure id.
func TestWriteWithClosure(t *testing.T) {
	f, server := newFakeServe(t)
	c := newClient(t, server, false)
	ctx := context.Background()
	created, err := c.Call(ctx, OpSessionCreate, CallOptions{Body: map[string]any{"model": "x"}})
	if err != nil || created.Status != 201 {
		t.Fatalf("create: %v %+v", err, created)
	}
	var session struct {
		ID string `json:"id"`
	}
	if err := created.Decode(&session); err != nil || session.ID != "s-1" {
		t.Fatalf("decode: %v", err)
	}
	res, err := c.Call(ctx, OpSessionMessageSend, CallOptions{Params: map[string]string{"id": session.ID}, Body: map[string]any{"text": "hi"}, ClosureID: f.closureID, IdempotencyKey: "msg-1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 202 {
		t.Fatalf("expected 202 accepted, got %d", res.Status)
	}
	stale := strings.Repeat("b", 64)
	_, err = c.Call(ctx, OpSessionMessageSend, CallOptions{Params: map[string]string{"id": session.ID}, Body: map[string]any{"text": "hi"}, ClosureID: stale})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "precondition_failed" || apiErr.Status != 412 || apiErr.RetryAction != "rediscover" {
		t.Fatalf("expected 412 precondition_failed, got %v", err)
	}
	if apiErr.DomainCode() != "closure_stale" || apiErr.ClosureID() == "" || apiErr.RequestID != nil || apiErr.TraceID == "" {
		t.Fatalf("precondition detail: %+v", apiErr)
	}
	advice := Advice(apiErr)
	if advice.Action != "rediscover" || advice.Replayable || advice.ClosureID != apiErr.ClosureID() {
		t.Fatalf("advice: %+v", advice)
	}
	// local guards
	var cerr *ClientError
	if _, err := c.Call(ctx, OpSessionGet, CallOptions{Params: map[string]string{"id": "s"}, ClosureID: f.closureID}); !errors.As(err, &cerr) || cerr.Code != CodeClosureIDNotApplicable {
		t.Fatalf("closure id on read: %v", err)
	}
	if _, err := c.Call(ctx, OpSessionMessageSend, CallOptions{Params: map[string]string{"id": "s"}, ClosureID: "abc"}); !errors.As(err, &cerr) || cerr.Code != CodeInvalidClosureID {
		t.Fatalf("bad closure id: %v", err)
	}
	if _, err := c.Call(ctx, OpSessionGet, CallOptions{Params: map[string]string{"id": "s"}, IdempotencyKey: "k"}); !errors.As(err, &cerr) || cerr.Code != CodeInvalidIdempotencyKey {
		t.Fatalf("idempotency key on read: %v", err)
	}
	if _, err := c.Call(ctx, OpSessionMessageSend, CallOptions{Params: map[string]string{"id": "s"}, IdempotencyKey: "has space"}); !errors.As(err, &cerr) || cerr.Code != CodeInvalidIdempotencyKey {
		t.Fatalf("bad idempotency key: %v", err)
	}
	if _, err := c.Call(ctx, "session.nope", CallOptions{}); !errors.As(err, &cerr) || cerr.Code != CodeInvalidOperation {
		t.Fatalf("unknown operation: %v", err)
	}
	// 404 facade error decodes as APIError
	if _, err := c.Call(ctx, OpCacheCapabilities, CallOptions{}); !errors.As(err, &apiErr) || apiErr.Code != "not_found" || apiErr.Status != 404 {
		t.Fatalf("404: %v", err)
	}
	reqs := f.requests()
	if reqs[1].Header.Get(HeaderClosureID) != f.closureID || reqs[1].Header.Get("Idempotency-Key") != "msg-1" || reqs[1].Header.Get("Content-Type") != "application/json" {
		t.Fatalf("write headers: %v", reqs[1].Header)
	}
}

// Discipline 3 + canonical bodies: strict families are canonical-encoded; same-request replays only
// when stated with the same key; unknown outcomes are never replayed.
func TestCanonicalBodiesAndRetry(t *testing.T) {
	f, server := newFakeServe(t)
	c := newClient(t, server, false)
	ctx := context.Background()
	body := map[string]any{"protocol": "sdk2-ext-v1", "request": map[string]any{"requestId": "ack-1"}, "ack": map[string]any{"throughSequence": "9"}}
	res, err := c.Call(ctx, OpArchiveAckCommit, CallOptions{Params: map[string]string{"id": "b-1"}, Body: body})
	if err != nil || res.Status != 200 {
		t.Fatalf("ack: %v", err)
	}
	// raw non-canonical bytes are refused locally for strict families
	var cerr *ClientError
	_, err = c.Call(ctx, OpArchiveAckCommit, CallOptions{Params: map[string]string{"id": "b-1"}, Body: json.RawMessage(`{"b":1, "a":2}`)})
	if !errors.As(err, &cerr) || cerr.Code != CodeInvalidBody {
		t.Fatalf("expected invalid_body, got %v", err)
	}
	_, err = c.Call(ctx, OpArchiveAckCommit, CallOptions{Params: map[string]string{"id": "b-1"}, Body: map[string]any{"n": 1.5}})
	if !errors.As(err, &cerr) || cerr.Code != CodeInvalidBody {
		t.Fatalf("expected invalid_body for fraction, got %v", err)
	}
	// 503 capacity_exceeded same-request + Retry-After → replay once with the same key
	slept := time.Duration(0)
	req := SameRequest{Operation: OpArchiveAckCommit, Options: CallOptions{Params: map[string]string{"id": "b-busy"}, Body: body}}
	_, err = c.Call(ctx, req.Operation, req.Options)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "capacity_exceeded" || apiErr.RetryAction != "same-request" || !apiErr.HasRetryAfter || apiErr.RetryAfter != time.Second {
		t.Fatalf("expected 503 same-request with retry-after, got %v", err)
	}
	if k := IdempotencyKeyOf(req); k != "ack-1" {
		t.Fatalf("body requestId as key: %q", k)
	}
	replayed, err := RetrySameRequest(ctx, c, apiErr, req, RetryOptions{Sleep: func(_ context.Context, d time.Duration) error { slept = d; return nil }})
	if err != nil || replayed.Status != 200 || slept != time.Second {
		t.Fatalf("replay: %v %v", err, slept)
	}
	// budget exceeded → no hang, no replay
	_, err = RetrySameRequest(ctx, c, apiErr, req, RetryOptions{MaxWait: 500 * time.Millisecond, Sleep: func(context.Context, time.Duration) error { t.Fatal("must not sleep"); return nil }})
	if !errors.As(err, &cerr) || cerr.Code != CodeRetryAfterExceedsBudget {
		t.Fatalf("expected retry_after_exceeds_budget, got %v", err)
	}
	// unknown outcome: server says query-status; never replayed
	_, err = c.Call(ctx, OpArchiveAckCommit, CallOptions{Params: map[string]string{"id": "b-unknown"}, Body: body})
	if !errors.As(err, &apiErr) || apiErr.Code != "result_unknown" {
		t.Fatalf("expected result_unknown, got %v", err)
	}
	if adv := Advice(apiErr); adv.Action != "query-status" || adv.Replayable || adv.Source != "unified" {
		t.Fatalf("advice: %+v", adv)
	}
	_, err = RetrySameRequest(ctx, c, apiErr, SameRequest{Operation: OpArchiveAckCommit, Options: CallOptions{Params: map[string]string{"id": "b-unknown"}, Body: body}}, RetryOptions{})
	if !errors.As(err, &cerr) || cerr.Code != CodeNotRetryable {
		t.Fatalf("expected not_retryable, got %v", err)
	}
	// stated same-request but no key → retry_key_missing
	keyless := &APIError{Code: "capacity_exceeded", Status: 503, RetryAction: "same-request"}
	_, err = RetrySameRequest(ctx, c, keyless, SameRequest{Operation: OpArchiveAckCommit, Options: CallOptions{Params: map[string]string{"id": "b-1"}, Body: map[string]any{"x": 1}}}, RetryOptions{})
	if !errors.As(err, &cerr) || cerr.Code != CodeRetryKeyMissing {
		t.Fatalf("expected retry_key_missing, got %v", err)
	}
	// domain envelope advice: backoff → same-request, /v2 family without action → not stated
	if adv := Advice(&DomainError{Family: "terminal-services-v1", Code: "request_limit", RetryAction: "backoff"}); adv.Action != "same-request" || !adv.Replayable {
		t.Fatalf("backoff mapping: %+v", adv)
	}
	if adv := Advice(&DomainError{Family: "agent-session-v1", Code: "session_ended"}); adv.Action != "none" || adv.Stated || adv.Replayable {
		t.Fatalf("unstated domain advice: %+v", adv)
	}
	if adv := Advice(errors.New("boom")); adv.Source != "none" || adv.Replayable {
		t.Fatalf("plain error advice: %+v", adv)
	}
	// the server saw canonical bytes for the strict family
	for _, r := range f.requests() {
		if strings.HasSuffix(r.URL.Path, "/acks") && r.URL.RawQuery != "" {
			t.Fatalf("ack must not carry a query: %s", r.URL.RawQuery)
		}
	}
	// not_canonical from the server maps to an APIError with retryAction none
	_, err = c.Call(ctx, OpArchiveAckCommit, CallOptions{Params: map[string]string{"id": "b-1"}, Body: json.RawMessage(`{"a":1}`)})
	if err != nil {
		t.Fatalf("canonical raw body must pass: %v", err)
	}
}

func TestErrorBodyDecoding(t *testing.T) {
	op := mustOp(t, OpArchiveSync)
	meta := Meta{Contract: Contract, Domain: "archive-sync"}
	// unwrapped family envelope → DomainError with the family's own retryAction
	body := []byte(`{"contract":"archive-sync-v1","error":{"code":"sync_conflict"},"retryAction":"reconcile","retryAfterMs":250}`)
	value, _ := DecodeJSON(body)
	err := decodeErrorBody(409, meta, value, body, op, "application/json")
	var derr *DomainError
	if !errors.As(err, &derr) || derr.Family != "archive-sync-v1" || derr.Code != "sync_conflict" || derr.RetryAction != "reconcile" || derr.RetryAfter != 250*time.Millisecond {
		t.Fatalf("domain error: %v", err)
	}
	if adv := Advice(derr); adv.Action != "query-status" || adv.Replayable {
		t.Fatalf("reconcile maps to query-status: %+v", adv)
	}
	// unified envelope with a code outside the vocabulary is invalid_response, not a guess
	body = []byte(`{"contract":"unified-v1","traceId":"t","requestId":null,"code":"brand_new","status":400,"retryAction":"none","message":"x"}`)
	value, _ = DecodeJSON(body)
	var cerr *ClientError
	if err := decodeErrorBody(400, meta, value, body, op, "application/json"); !errors.As(err, &cerr) || cerr.Code != CodeInvalidResponse {
		t.Fatalf("unknown code: %v", err)
	}
	// status disagreement
	body = []byte(`{"contract":"unified-v1","traceId":"t","requestId":null,"code":"not_found","status":404,"retryAction":"none","message":"x"}`)
	value, _ = DecodeJSON(body)
	if err := decodeErrorBody(410, meta, value, body, op, "application/json"); !errors.As(err, &cerr) || cerr.Code != CodeInvalidResponse {
		t.Fatalf("status mismatch: %v", err)
	}
	// facade operation with a non-unified body → contract unavailable
	facade := mustOp(t, OpDiscoveryManifest)
	value, _ = DecodeJSON([]byte(`{"error":"nope"}`))
	if err := decodeErrorBody(500, meta, value, nil, facade, "application/json"); !errors.Is(err, ErrContractUnavailable) {
		t.Fatalf("facade non-envelope: %v", err)
	}
	if err := decodeErrorBody(500, meta, "text", nil, op, "application/json"); !errors.Is(err, ErrContractUnavailable) {
		t.Fatalf("non-object: %v", err)
	}
}

func TestReadUnifiedHeaders(t *testing.T) {
	mk := func(h map[string]string) *http.Response {
		resp := &http.Response{StatusCode: 200, Header: http.Header{}}
		for k, v := range h {
			resp.Header.Set(k, v)
		}
		return resp
	}
	good := map[string]string{HeaderContract: Contract, HeaderManifestRevision: "4", HeaderDomain: "session", HeaderSchemaHash: "none", HeaderRetryAfter: "2.5", HeaderTraceID: "abc", HeaderClosureID: strings.Repeat("c", 64)}
	meta, err := readUnifiedHeaders(mk(good))
	if err != nil || meta.ManifestRevision != 4 || meta.RetryAfter != 2500*time.Millisecond || meta.TraceID != "abc" || meta.ClosureID == "" || meta.EventEnvelope {
		t.Fatalf("meta: %+v %v", meta, err)
	}
	for key, val := range map[string]string{HeaderContract: "unified-v2", HeaderManifestRevision: "0", HeaderDomain: "SDK2", HeaderSchemaHash: "sha1:x", HeaderClosureID: "short", HeaderEventEnvelope: "legacy"} {
		bad := map[string]string{}
		for k, v := range good {
			bad[k] = v
		}
		bad[key] = val
		_, err := readUnifiedHeaders(mk(bad))
		var cu *ContractUnavailableError
		if !errors.As(err, &cu) {
			t.Fatalf("%s=%s must fail", key, val)
		}
		if key == HeaderContract && cu.Reason != ReasonContractMismatch {
			t.Fatalf("contract mismatch reason: %v", cu.Reason)
		}
	}
	if d, ok := ParseRetryAfter("Wed, 21 Oct 2015 07:28:00 GMT", time.Date(2015, 10, 21, 7, 27, 30, 0, time.UTC)); !ok || d != 30*time.Second {
		t.Fatalf("http-date retry-after: %v %v", d, ok)
	}
	if _, ok := ParseRetryAfter("soon", time.Now()); ok {
		t.Fatal("unparsable retry-after must be absent")
	}
}

func TestContextCancellation(t *testing.T) {
	_, server := newFakeServe(t)
	c := newClient(t, server, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Manifest(ctx)
	if !errors.Is(err, ErrAborted) {
		t.Fatalf("expected aborted, got %v", err)
	}
}
