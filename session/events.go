package session

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/tansrai/tansr-go/api"
)

// Event retains unknown additive kernel/control fields and the exact unified
// envelope. Raw for server.* controls is the original payload, not a fabricated
// kernel event. Inspect Type before decoding it.
type Event struct {
	Type           string
	Raw            json.RawMessage
	Envelope       *api.EventEnvelope
	TerminalStatus string
}

type Outcome struct {
	Status string
	TurnID string
	Reason string
}

const (
	OutcomeCompleted    = "completed"
	OutcomeAborted      = "aborted"
	OutcomeSessionEnded = "session-ended"
	OutcomeUnknown      = "unknown"
)

// TurnOutcome distinguishes a completed turn from an ended observation stream.
// A session-ended outcome means a caller waiting for a turn has no evidence of
// success. Unknown event types are exposed, never interpreted as turn completion.
func (e Event) TurnOutcome() (Outcome, bool) {
	var raw struct {
		TurnID      string `json:"turnId"`
		Reason      string `json:"reason"`
		Recoverable *bool  `json:"recoverable"`
	}
	if json.Unmarshal(e.Raw, &raw) != nil {
		return Outcome{}, false
	}
	out := Outcome{TurnID: raw.TurnID, Reason: raw.Reason}
	switch {
	case e.Type == "turn.completed" && e.TerminalStatus == api.TerminalCompleted:
		out.Status = OutcomeCompleted
	case e.Type == "turn.aborted" && e.TerminalStatus == api.TerminalAborted:
		out.Status = OutcomeAborted
	case e.Type == "turn.error" && raw.Recoverable != nil && !*raw.Recoverable && e.TerminalStatus == api.TerminalAborted:
		out.Status = OutcomeAborted
	case e.Type == "session.ended" && e.TerminalStatus == api.TerminalCompleted:
		out.Status = OutcomeSessionEnded
	case strings.HasPrefix(e.Type, "turn.") && e.TerminalStatus == api.TerminalUnknown:
		out.Status = OutcomeUnknown
	default:
		return Outcome{}, false
	}
	return out, true
}

// EventStream permits one Next consumer and concurrent Close/LastEventID calls.
// It never reconnects, resends a message or interrupts Serve automatically.
type EventStream struct {
	stream    *api.Stream
	sessionID string
	nextMu    sync.Mutex
	cursorMu  sync.RWMutex
	lastID    string
	lastSeq   int64
}

func (s *Session) Events(ctx context.Context, lastEventID string) (*EventStream, error) {
	sequence := int64(-1)
	if lastEventID != "" {
		var err error
		sequence, err = parseSequence(lastEventID)
		if err != nil {
			return nil, &api.ClientError{Code: api.CodeInvalidLastEventID, Detail: "session cursor must be a nonnegative safe decimal integer"}
		}
	}
	stream, err := s.client.transport.Events(ctx, api.OpSessionEventsObserve, api.EventsOptions{Params: s.params(), LastEventID: lastEventID})
	if err != nil {
		return nil, err
	}
	if stream.Meta().Domain != "session" {
		_ = stream.Close()
		return nil, invalidResponse("session stream belongs to another domain")
	}
	return &EventStream{stream: stream, sessionID: s.ID(), lastID: lastEventID, lastSeq: sequence}, nil
}

func (s *EventStream) Close() error { return s.stream.Close() }

func (s *EventStream) LastEventID() string {
	s.cursorMu.RLock()
	defer s.cursorMu.RUnlock()
	return s.lastID
}

func (s *EventStream) Next() (Event, error) {
	s.nextMu.Lock()
	defer s.nextMu.Unlock()
	for {
		frame, err := s.stream.Next()
		if err != nil {
			return Event{}, err
		}
		event, sequence, gap, err := s.decode(frame)
		if err != nil {
			_ = s.stream.Close()
			return Event{}, err
		}
		if gap {
			var raw struct {
				Reason string `json:"reason"`
			}
			_ = json.Unmarshal(event.Raw, &raw)
			if raw.Reason == "ahead_of_log" {
				s.lastSeq = -1
				s.cursorMu.Lock()
				s.lastID = ""
				s.cursorMu.Unlock()
			}
			// A gap is exposed for host recovery, never silently patched using
			// history or acknowledged as archive coverage.
			return event, nil
		}
		if sequence <= s.lastSeq {
			continue
		}
		s.lastSeq = sequence
		s.cursorMu.Lock()
		s.lastID = frame.ID
		s.cursorMu.Unlock()
		return event, nil
	}
}

func (s *EventStream) decode(frame *api.Frame) (Event, int64, bool, error) {
	envelope := frame.Envelope
	if envelope == nil || envelope.Domain != "session" {
		return Event{}, 0, false, invalidResponse("session event has no matching unified envelope")
	}
	event := Event{Raw: envelope.Raw, Envelope: envelope}
	if envelope.Type != nil {
		event.Type = *envelope.Type
	}
	if envelope.TerminalStatus != nil {
		event.TerminalStatus = *envelope.TerminalStatus
	}
	var raw struct {
		Type      string   `json:"type"`
		SessionID string   `json:"sessionId"`
		Sequence  *int64   `json:"seq"`
		Timestamp *float64 `json:"ts"`
		Reason    string   `json:"reason"`
	}
	if json.Unmarshal(event.Raw, &raw) != nil {
		return Event{}, 0, false, invalidResponse("event payload field types are invalid")
	}
	if event.Type == "server.replay.gap" {
		if frame.HasID || envelope.EventID != nil || !envelope.CursorSet.EventCursor.IsNull() || raw.SessionID != s.sessionID || raw.Type != event.Type {
			return Event{}, 0, false, invalidResponse("replay gap changed identity or cursor")
		}
		return event, -1, true, nil
	}
	if !frame.HasID || envelope.EventID == nil || *envelope.EventID != frame.ID {
		return Event{}, 0, false, invalidResponse("event id differs from its SSE frame")
	}
	cursor, ok := envelope.CursorSet.EventCursor.Text()
	if !ok || cursor != frame.ID {
		return Event{}, 0, false, invalidResponse("event cursor differs from its frame identity")
	}
	sequence, err := parseSequence(frame.ID)
	if err != nil {
		return Event{}, 0, false, invalidResponse("session sequence is not a safe decimal integer")
	}
	if strings.HasPrefix(event.Type, "server.") {
		if raw.Timestamp == nil {
			return Event{}, 0, false, invalidResponse("control event has no timestamp")
		}
	} else if raw.Type != event.Type || raw.SessionID != s.sessionID || raw.Sequence == nil || *raw.Sequence != sequence {
		return Event{}, 0, false, invalidResponse("kernel event identity differs from its stream")
	}
	return event, sequence, false, nil
}

func parseSequence(value string) (int64, error) {
	if value == "" || len(value) > 1 && value[0] == '0' {
		return 0, invalidResponse("invalid sequence")
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, invalidResponse("invalid sequence")
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || !safeCount(n) {
		return 0, invalidResponse("unsafe sequence")
	}
	return n, nil
}
