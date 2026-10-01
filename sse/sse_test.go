package sse

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func readAll(t *testing.T, input string, opts Options) ([]Event, error) {
	t.Helper()
	r := NewReader(strings.NewReader(input), opts)
	var out []Event
	for {
		ev, err := r.Next()
		if err != nil {
			if err == io.EOF {
				return out, nil
			}
			return out, err
		}
		out = append(out, *ev)
	}
}

func TestMultiLineDataAndComments(t *testing.T) {
	input := ": keep-alive\n" +
		"id: 7\nevent: msg\ndata: {\"a\":\n" +
		"data:  1}\n" +
		"data\n\n" +
		":another comment\n\n" +
		"data: second\r\n\r\n" +
		"data: cr-only\r\r"
	events, err := readAll(t, input, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	if events[0].ID != "7" || !events[0].HasID || events[0].Event != "msg" || events[0].Data != "{\"a\":\n 1}\n" {
		t.Fatalf("first frame: %+v", events[0])
	}
	if events[1].HasID || events[1].Event != "" || events[1].Data != "second" {
		t.Fatalf("second frame: %+v", events[1])
	}
	if events[2].Data != "cr-only" {
		t.Fatalf("third frame: %+v", events[2])
	}
}

func TestLastEventID(t *testing.T) {
	// id-only frames (id: z) are not dispatched but still move LastEventID; an id with U+0000 is ignored;
	// a bare `id` line resets it.
	r := NewReader(strings.NewReader("id: a\ndata: 1\n\ndata: 2\n\nid: z\n\ndata: 2z\n\nid: b\u0000c\ndata: 3\n\nid\ndata: 4\n\n"), Options{})
	var ids []string
	for {
		ev, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.LastEventID()+"|"+ev.Data)
	}
	want := []string{"a|1", "a|2", "z|2z", "z|3", "|4"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("last-event-id tracking: %v", ids)
	}
}

// TestNoDataFramesNotDispatched: bare event: / retry: / id: frames are swallowed (WHATWG; Node SseParser
// parity) while `data:` with an empty value is a dispatched empty frame.
func TestNoDataFramesNotDispatched(t *testing.T) {
	events, err := readAll(t, "event: ping\n\nretry: 3000\n\nid: 9\n\nretry: 3000\ndata: \n\nevent: x\ndata: y\n\n", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Data != "" || events[0].HasID || events[0].Event != "" || events[1].Event != "x" || events[1].Data != "y" {
		t.Fatalf("expected the empty-data frame and the x frame only, got %+v", events)
	}
	r := NewReader(strings.NewReader("event: only\n\n"), Options{StrictEOF: true})
	if _, err := r.Next(); err != io.EOF {
		t.Fatalf("event-only stream must end cleanly without a frame, got %v", err)
	}
	r = NewReader(strings.NewReader("id: 7"), Options{})
	if _, err := r.Next(); err != io.EOF || r.LastEventID() != "7" {
		t.Fatalf("lenient EOF after an id-only frame: %v %q", err, r.LastEventID())
	}
}

func TestStrictEOF(t *testing.T) {
	if _, err := readAll(t, "data: partial\n", Options{StrictEOF: true}); !errors.Is(err, ErrTruncated) {
		t.Fatalf("expected ErrTruncated, got %v", err)
	}
	events, err := readAll(t, "data: partial", Options{})
	if err != nil || len(events) != 1 || events[0].Data != "partial" {
		t.Fatalf("lenient EOF: %v %+v", err, events)
	}
	events, err = readAll(t, "data: a\n\n", Options{StrictEOF: true})
	if err != nil || len(events) != 1 {
		t.Fatalf("clean EOF after frame: %v %+v", err, events)
	}
	events, err = readAll(t, ": only a comment\n", Options{StrictEOF: true})
	if err != nil || len(events) != 0 {
		t.Fatalf("comment-only stream must end cleanly: %v %+v", err, events)
	}
}

func TestFrameLimitAndUTF8(t *testing.T) {
	if _, err := readAll(t, "data: "+strings.Repeat("x", 100)+"\n\n", Options{MaxFrameBytes: 64}); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
	if _, err := readAll(t, "data: \xff\xfe\n\n", Options{}); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("expected ErrInvalidUTF8, got %v", err)
	}
	events, err := readAll(t, "event: ping\ndata\n\n", Options{})
	if err != nil || len(events) != 1 || events[0].Event != "ping" || events[0].Data != "" {
		t.Fatalf("event frame with a bare data line: %v %+v", err, events)
	}
}
