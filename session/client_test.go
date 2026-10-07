package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tansrai/tansr-go/api"
)

func testClient(t *testing.T, handler http.HandlerFunc, options ...func(*api.Options)) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	opts := api.Options{BaseURL: server.URL, Token: "fixture", SessionFamily: "sdk1", EventEnvelope: true}
	for _, change := range options {
		change(&opts)
	}
	transport, err := api.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(transport)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func response(w http.ResponseWriter, domain string, status int, body any) {
	w.Header().Set(api.HeaderContract, api.Contract)
	w.Header().Set(api.HeaderManifestRevision, strconv.Itoa(api.ManifestRevision))
	w.Header().Set(api.HeaderDomain, domain)
	w.Header().Set(api.HeaderSchemaHash, "none")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func discovery(w http.ResponseWriter) {
	response(w, "session", 200, map[string]any{"protocol": "sdk2-ext-v1", "contracts": []any{
		map[string]any{"contract": "sdk1", "availability": "legacy-complete"},
		map[string]any{"contract": "sdk2-offload-v1", "availability": "source-required"},
	}})
}

const closureID = "fc9a7b7a38e67ef3bcf0165be04290102628d1fa344b9e4775edad987b609399"

func closure(w http.ResponseWriter, state string) {
	domains, operations := map[string]any{}, map[string]string{}
	for _, d := range api.Domains {
		if d != "discovery" {
			domains[d] = map[string]any{"installed": true, "revision": nil}
		}
	}
	for _, name := range api.ClosureOperationNames() {
		operations[name] = state
	}
	w.Header().Set(api.HeaderClosureID, closureID)
	response(w, "discovery", 200, map[string]any{"contract": api.Contract, "closureId": closureID, "authorizationRevision": nil, "domains": domains, "operations": operations})
}

func testPath(operation string, params map[string]string) string {
	op, _ := api.Lookup(operation)
	path, err := api.InstantiatePath(op, params)
	if err != nil {
		panic(err)
	}
	return path
}

func ref(c *Client) *Session { return &Session{client: c, created: Created{SessionID: "s1"}} }

func TestSessionRequiresExplicitNegotiation(t *testing.T) {
	for _, opts := range []api.Options{
		{BaseURL: "http://localhost", Token: "fixture", EventEnvelope: true},
		{BaseURL: "http://localhost", Token: "fixture", SessionFamily: "sdk1"},
	} {
		transport, err := api.New(opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := New(transport); err == nil {
			t.Fatal("implicit family/envelope accepted")
		}
	}
}

func TestCreateResumeAndTokenRenewal(t *testing.T) {
	var tokenCalls, creates atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(api.HeaderSessionFamily) != "sdk1" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer token-") {
			t.Error("missing explicit family or renewable token")
		}
		if r.Method == "GET" {
			if r.URL.Path != testPath(api.OpSessionCapabilities, nil) || r.URL.Query().Get("protocol") != "sdk2-ext-v1" {
				t.Errorf("wrong discovery %s", r.URL)
			}
			discovery(w)
			return
		}
		creates.Add(1)
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		status, resumed := 201, false
		if _, ok := body["resume"]; ok {
			status = 200 // Existing live session attaches without a rebuild.
		}
		if _, exists := body["IdempotencyKey"]; exists {
			t.Error("transport-only option leaked into body")
		}
		response(w, "session", status, Created{SessionID: "s1", Resumed: resumed, LastSeq: 4})
	}, func(opts *api.Options) {
		opts.TokenFunc = func(context.Context) (string, error) { return fmt.Sprintf("token-%d", tokenCalls.Add(1)), nil }
	})
	s, err := c.Create(context.Background(), CreateOptions{Model: "chosen", WriteOptions: WriteOptions{IdempotencyKey: "create-key"}})
	if err != nil || s.ID() != "s1" || s.Created().LastSeq != 4 {
		t.Fatalf("create: %v %v", s, err)
	}
	s, err = c.Resume(context.Background(), "s1")
	if err != nil || s.Created().Resumed || s.ID() != "s1" {
		t.Fatalf("live resume: %v %v", s, err)
	}
	if creates.Load() != 2 || tokenCalls.Load() != 4 {
		t.Fatalf("creates=%d token reads=%d", creates.Load(), tokenCalls.Load())
	}
}

func TestOffloadRequiresRetainedIdentity(t *testing.T) {
	var requests atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method == "GET" {
			discovery(w)
			return
		}
		response(w, "session", 201, map[string]any{"sessionId": "s1", "resumed": false, "lastSeq": 0, "contract": "sdk2-offload-v1", "availability": "source-required"})
	}, func(opts *api.Options) { opts.SessionFamily = "sdk2-offload-v1" })
	if _, err := c.Create(context.Background(), CreateOptions{}); err == nil || requests.Load() != 0 {
		t.Fatal("offload create missing requestId performed a request")
	}
	if _, err := c.Create(context.Background(), CreateOptions{RequestID: "retained-create"}); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatal("unexpected implicit retry")
	}
}

func TestMissingSessionIDCannotBecomeSuccessfulCreate(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			discovery(w)
			return
		}
		response(w, "session", 201, map[string]any{"id": "wrong-field", "resumed": false, "lastSeq": 0})
	})
	if _, err := c.Create(context.Background(), CreateOptions{}); !errors.Is(err, &api.ClientError{Code: api.CodeInvalidResponse}) {
		t.Fatalf("got %v", err)
	}
}

func TestWriteRefreshesClosureAndNeverReplaysUnknown(t *testing.T) {
	var writes, discoveries atomic.Int32
	var disabled atomic.Bool
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			discoveries.Add(1)
			state := api.StateEnabled
			if disabled.Load() {
				state = api.StateDisabled
			}
			closure(w, state)
			return
		}
		writes.Add(1)
		if r.Header.Get(api.HeaderClosureID) != closureID || r.Header.Get("Idempotency-Key") != "stable" {
			t.Error("write omitted closure or idempotency key")
		}
		response(w, "session", 503, map[string]any{"contract": api.Contract, "traceId": "trace-1", "requestId": "stable", "code": "result_unknown", "status": 503, "retryAction": "query-status", "message": "unknown"})
	})
	s := ref(c)
	if _, err := s.Send(context.Background(), "hello", WriteOptions{IdempotencyKey: "stable"}); !errors.Is(err, &api.APIError{Code: api.CodeResultUnknown}) {
		t.Fatalf("unknown effect lost: %v", err)
	}
	disabled.Store(true)
	var capability *CapabilityError
	if _, err := s.Send(context.Background(), "hello", WriteOptions{}); !errors.As(err, &capability) || capability.State != api.StateDisabled {
		t.Fatalf("stale closure accepted: %v", err)
	}
	if writes.Load() != 1 || discoveries.Load() != 2 {
		t.Fatalf("writes=%d discoveries=%d", writes.Load(), discoveries.Load())
	}
}

func TestPermissionQuestionAndInputWire(t *testing.T) {
	var writes atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			closure(w, api.StateEnabled)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch writes.Add(1) {
		case 1:
			if body["digest"] != "digest-original" || body["verdict"] != "deny" || !strings.HasSuffix(r.URL.Path, "/ticket-1") {
				t.Errorf("wrong permission receipt %v", body)
			}
		case 2:
			answers, ok := body["answers"].([]any)
			if !ok || len(answers) != 1 || answers[0].(map[string]any)["questionId"] != "question-1" {
				t.Errorf("wrong question receipt %v", body)
			}
		case 3:
			if body["inputId"] != "input-1" || body["ack"] != "memory" || body["target"].(map[string]any)["turnId"] != "turn-1" {
				t.Errorf("wrong steering receipt %v", body)
			}
			response(w, "session", 202, map[string]any{"outcome": "accepted", "receipt": map[string]any{"state": "queued"}})
			return
		}
		response(w, "session", 200, Accepted{Accepted: true})
	})
	s := ref(c)
	if _, err := s.Permission(context.Background(), "ticket-1", "digest-original", "deny", WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Answer(context.Background(), "ticket-2", []Answer{{QuestionID: "question-1", SelectedOptionIDs: []string{}, FreeText: "answer"}}, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitInput(context.Background(), Input{InputID: "input-1", Target: InputTarget{"epoch", "turn-1"}, Content: InputContent{Text: "new instruction"}, Ack: "memory"}, WriteOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestCloseDoesNotRequireLiveClosure(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.Header.Get(api.HeaderClosureID) != "" {
			t.Error("close requested a live closure or added an unsupported header")
		}
		response(w, "session", 202, Accepted{SessionID: "s1", Accepted: true})
	})
	if _, err := ref(c).Close(context.Background(), WriteOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryCountOnly(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != testPath(api.OpSessionHistoryRead, map[string]string{"id": "s1"}) || r.URL.Query().Get("limit") != "0" || r.URL.Query().Get("offset") != "0" {
			t.Errorf("count-only request changed: %s %s", r.Method, r.URL)
		}
		response(w, "session", 200, map[string]any{"sessionId": "s1", "lastSeq": 20, "messages": []any{}, "total": 100, "offset": 0})
	})
	raw, err := ref(c).History(context.Background(), 0, 0)
	if err != nil || !strings.Contains(string(raw), `"total":100`) {
		t.Fatalf("count-only history rejected: %s %v", raw, err)
	}
}

func sseHeader(w http.ResponseWriter) {
	w.Header().Set(api.HeaderContract, api.Contract)
	w.Header().Set(api.HeaderManifestRevision, strconv.Itoa(api.ManifestRevision))
	w.Header().Set(api.HeaderDomain, "session")
	w.Header().Set(api.HeaderSchemaHash, "none")
	w.Header().Set(api.HeaderEventEnvelope, api.Contract)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
}

func frame(w io.Writer, id, kind, status string, raw any) {
	var eventID, terminal any
	if id != "" {
		_, _ = fmt.Fprintln(w, "id: "+id)
		eventID = id
	}
	if status != "" {
		terminal = status
	}
	body, _ := json.Marshal(map[string]any{"contract": api.Contract, "eventId": eventID, "domain": "session", "type": kind,
		"cursorSet":      map[string]any{"eventCursor": eventID, "archiveCoverage": nil, "outputWatermark": nil, "materialConsumed": nil, "ackReceipt": nil},
		"terminalStatus": terminal, "raw": raw})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
}

func TestTurnOutcomesAndEOF(t *testing.T) {
	for _, test := range []struct {
		kind, status, outcome string
		recoverable           *bool
	}{
		{"turn.completed", "completed", OutcomeCompleted, nil},
		{"turn.aborted", "aborted", OutcomeAborted, nil},
		{"session.ended", "completed", OutcomeSessionEnded, nil},
		{"turn.error", "aborted", OutcomeAborted, boolPointer(false)},
		{"turn.error", "", "", boolPointer(true)},
		{"future.event", "completed", "", nil},
	} {
		t.Run(test.kind+test.status, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				sseHeader(w)
				raw := map[string]any{"type": test.kind, "sessionId": "s1", "seq": 5, "ts": 1, "turnId": "turn-1"}
				if test.recoverable != nil {
					raw["recoverable"] = *test.recoverable
				}
				frame(w, "5", test.kind, test.status, raw)
			})
			stream, err := ref(c).Events(context.Background(), "4")
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			event, err := stream.Next()
			if err != nil {
				t.Fatal(err)
			}
			outcome, terminal := event.TurnOutcome()
			if terminal != (test.outcome != "") || outcome.Status != test.outcome {
				t.Errorf("outcome=%+v terminal=%v", outcome, terminal)
			}
			if _, err := stream.Next(); !errors.Is(err, io.EOF) || stream.LastEventID() != "5" {
				t.Fatalf("EOF=%v cursor=%s", err, stream.LastEventID())
			}
		})
	}
}

func boolPointer(value bool) *bool { return &value }

func TestReplayGapAndDuplicateHandling(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		sseHeader(w)
		if r.Header.Get("Last-Event-ID") != "4" {
			t.Error("event resume cursor omitted")
		}
		frame(w, "4", "turn.completed", "completed", map[string]any{"type": "turn.completed", "sessionId": "s1", "seq": 4})
		frame(w, "", "server.replay.gap", "", map[string]any{"type": "server.replay.gap", "sessionId": "s1", "reason": "ahead_of_log", "requestedAfterSeq": 4, "droppedEvents": 0})
		frame(w, "1", "server.permission.request", "", map[string]any{"ts": 12.5, "requestId": "ticket", "digest": "digest"})
	})
	stream, err := ref(c).Events(context.Background(), "4")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	event, err := stream.Next()
	if err != nil || event.Type != "server.replay.gap" || stream.LastEventID() != "" {
		t.Fatalf("gap did not reset old event cursor: %+v %v", event, err)
	}
	event, err = stream.Next()
	if err != nil || event.Type != "server.permission.request" || stream.LastEventID() != "1" {
		t.Fatalf("new epoch control discarded: %+v %v", event, err)
	}
}

func TestForeignSessionEventRejected(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		sseHeader(w)
		frame(w, "1", "turn.completed", "completed", map[string]any{"type": "turn.completed", "sessionId": "other", "seq": 1})
	})
	stream, err := ref(c).Events(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); !errors.Is(err, &api.ClientError{Code: api.CodeInvalidResponse}) {
		t.Fatalf("foreign event accepted: %v", err)
	}
}

func TestStreamContextCancellationInterruptsRead(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		sseHeader(w)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := ref(c).Events(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	result := make(chan error, 1)
	go func() { _, err := stream.Next(); result <- err }()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel cause lost: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not release blocked observation")
	}
}
