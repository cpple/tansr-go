package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpple/tansr-go/api"
	"github.com/cpple/tansr-go/session"
)

func testEvent(t *testing.T, kind, status string, seq int64, raw string) session.Event {
	t.Helper()
	cursor := strconv.FormatInt(seq, 10)
	var terminal any
	if status != "" {
		terminal = status
	}
	encoded, err := json.Marshal(map[string]any{"contract": "unified-v1", "domain": "session", "type": kind,
		"eventId": cursor, "terminalStatus": terminal, "raw": json.RawMessage(raw),
		"cursorSet": map[string]any{"eventCursor": cursor, "archiveCoverage": nil, "outputWatermark": nil, "materialConsumed": nil, "ackReceipt": nil}})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := api.ParseEventEnvelope(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return session.Event{Type: kind, Raw: json.RawMessage(raw), Envelope: envelope, TerminalStatus: status}
}

func TestChatTurnResultIsNotStreamEnd(t *testing.T) {
	for _, tc := range []struct {
		name, kind, status, raw string
		eof                     bool
		want                    string
	}{
		{name: "completed", kind: "turn.completed", status: "completed", raw: `{"turnId":"turn-1"}`},
		{name: "aborted", kind: "turn.aborted", status: "aborted", raw: `{"reason":"aborted_user"}`, want: "aborted"},
		{name: "failed", kind: "turn.error", status: "aborted", raw: `{"recoverable":false}`, want: "aborted"},
		{name: "session-ended", kind: "session.ended", status: "completed", raw: `{}`, want: "session-ended"},
		{name: "EOF", eof: true, want: "EOF is not completion"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			calls, interrupts := 0, 0
			hooks := chatHooks{send: func(context.Context, string) (int64, error) { calls++; return 0, nil }, interrupt: func(context.Context) error { interrupts++; return nil }}
			events := make(chan eventResult, 1)
			if tc.eof {
				events <- eventResult{err: io.EOF}
			} else {
				events <- eventResult{event: testEvent(t, tc.kind, tc.status, 1, tc.raw)}
			}
			err := chatLoop(context.Background(), hooks, nil, events, &out, "hello", false, 0)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("result %v; output %s", err, &out)
			}
			if calls != 1 {
				t.Fatalf("sent %d messages", calls)
			}
			if tc.eof && interrupts != 1 {
				t.Fatalf("uncertain stream left active turn: %d", interrupts)
			}
			if tc.want != "" && strings.Contains(out.String(), "[turn completed]") {
				t.Fatal("failed turn rendered successful")
			}
		})
	}
}

func TestChatIgnoresHistoricalCompletion(t *testing.T) {
	events := make(chan eventResult, 3)
	events <- eventResult{event: testEvent(t, "turn.completed", "completed", 4, `{}`)}
	events <- eventResult{event: testEvent(t, "msg.text.delta", "", 6, `{"text":"CURRENT"}`)}
	events <- eventResult{event: testEvent(t, "turn.completed", "completed", 7, `{}`)}
	var out bytes.Buffer
	hooks := chatHooks{send: func(context.Context, string) (int64, error) { return 5, nil }, interrupt: func(context.Context) error { return nil }}
	if err := chatLoop(context.Background(), hooks, nil, events, &out, "next", false, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "CURRENT") || strings.Count(out.String(), "[turn completed]") != 1 {
		t.Fatal(out.String())
	}
}

func TestChatDoesNotReplayUnknownAcceptance(t *testing.T) {
	unknown := &api.APIError{Code: api.CodeResultUnknown}
	calls := 0
	hooks := chatHooks{send: func(context.Context, string) (int64, error) { calls++; return 0, unknown }}
	err := chatLoop(context.Background(), hooks, nil, nil, io.Discard, "hello", false, 0)
	if !errors.Is(err, unknown) || calls != 1 {
		t.Fatalf("error %v calls %d", err, calls)
	}
}

func TestChatPermissionRequiresManualDecisionAndOriginalDigest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	events := make(chan eventResult)
	lines := make(chan inputLine)
	result := make(chan error, 1)
	var manual atomic.Bool
	calls := 0
	hooks := chatHooks{send: func(context.Context, string) (int64, error) { return 0, nil }, interrupt: func(context.Context) error { return nil },
		permission: func(_ context.Context, ticket, digest, verdict string) error {
			calls++
			if !manual.Load() || ticket != "p-1" || digest != "original-digest" || verdict != "allow" {
				return fmt.Errorf("wrong approval: %s %s %s", ticket, digest, verdict)
			}
			return nil
		}}
	go func() { result <- chatLoop(ctx, hooks, lines, events, io.Discard, "hello", false, 0) }()
	events <- eventResult{event: testEvent(t, "server.permission.request", "", 1, `{"requestId":"p-1","digest":"original-digest","name":"DemoOrderStatus","ts":1}`)}
	manual.Store(true)
	lines <- inputLine{text: "/allow p-1"}
	lines <- inputLine{text: "/allow p-1"} // resolved ticket cannot approve twice
	events <- eventResult{event: testEvent(t, "turn.completed", "completed", 2, `{}`)}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("approval calls %d", calls)
	}
}

func TestChatCancellationInterruptsButDoesNotClaimCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	hooks := chatHooks{interrupt: func(ctx context.Context) error { calls++; return ctx.Err() }}
	err := chatLoop(ctx, hooks, nil, nil, io.Discard, "", true, 0)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("%v interrupt=%d", err, calls)
	}
}

func TestChatClosedPermissionAndMalformedEvent(t *testing.T) {
	permissions := map[string]permissionTicket{}
	questions := map[string]bool{}
	request := session.Event{Type: "server.permission.request", Raw: json.RawMessage(`{"requestId":"p","digest":"d","ts":1}`)}
	if err := displayEvent(request, io.Discard, permissions, questions); err != nil {
		t.Fatal(err)
	}
	closed := session.Event{Type: "server.permission.closed", Raw: json.RawMessage(`{"requestId":"p","ts":2}`)}
	if err := displayEvent(closed, io.Discard, permissions, questions); err != nil {
		t.Fatal(err)
	}
	if len(permissions) != 0 {
		t.Fatal("closed ticket remained actionable")
	}
	request.Raw = json.RawMessage(`{"requestId":"p"}`)
	if err := displayEvent(request, io.Discard, permissions, questions); err == nil {
		t.Fatal("missing digest accepted")
	}
}

func TestChatInteractiveTwoTurnsAndQuestionAnswer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	lines := make(chan inputLine)
	events := make(chan eventResult)
	done := make(chan error, 1)
	var out bytes.Buffer
	messages := []string{}
	answers := 0
	hooks := chatHooks{
		send: func(_ context.Context, text string) (int64, error) {
			messages = append(messages, text)
			if len(messages) == 1 {
				return 0, nil
			}
			return 2, nil
		},
		interrupt: func(context.Context) error { return nil },
		answer: func(_ context.Context, ticket string, values []session.Answer) error {
			answers++
			if ticket != "q-ticket" || len(values) != 1 || values[0].QuestionID != "q1" || values[0].FreeText != "sample" || values[0].SelectedOptionIDs == nil {
				return errors.New("wrong question answer")
			}
			return nil
		},
	}
	go func() { done <- chatLoop(ctx, hooks, lines, events, &out, "", false, 0) }()
	lines <- inputLine{text: "first"}
	events <- eventResult{event: testEvent(t, "server.question.request", "", 1, `{"requestId":"q-ticket","questions":[{"id":"q1","prompt":"Select data","options":[]}],"ts":1}`)}
	lines <- inputLine{text: `/answers q-ticket [{"questionId":"q1","selectedOptionIds":[],"freeText":"sample"}]`}
	events <- eventResult{event: testEvent(t, "turn.completed", "completed", 2, `{}`)}
	lines <- inputLine{text: "second"}
	events <- eventResult{event: testEvent(t, "turn.completed", "completed", 3, `{}`)}
	lines <- inputLine{text: "/quit"}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if strings.Join(messages, ",") != "first,second" || answers != 1 || strings.Count(out.String(), "[turn completed]") != 2 {
		t.Fatalf("messages=%v answers=%d output=%s", messages, answers, &out)
	}
}
