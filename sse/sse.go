// Package sse reads text/event-stream frames (WHATWG Server-Sent Events) from an io.Reader.
//
// The reader is transport-only: it merges multi-line `data:` fields with "\n", tracks `id:` for
// Last-Event-ID resumption, skips comment lines (`:` prefix) and reports frames as they are
// terminated by an empty line (LF, CRLF or CR line ends). Frames without a data field are not
// dispatched (WHATWG dispatch rule; their id: still advances LastEventID). It attaches no meaning to
// frames: an SSE EOF is not a completion signal and the stream position is not a cursor of any other
// kind (SDK manual §16.6 items 5 and 6).
//
// The unified event envelope (api.EventEnvelope) is decoded by package api on top of this reader.
package sse

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"sync"
	"unicode/utf8"
)

// DefaultMaxFrameBytes is the frame size limit applied when Options.MaxFrameBytes is zero (2 MiB, same
// as the Node client).
const DefaultMaxFrameBytes = 2 * 1024 * 1024

var (
	// ErrFrameTooLarge is returned when a single frame exceeds Options.MaxFrameBytes.
	ErrFrameTooLarge = errors.New("sse: frame exceeds size limit")
	// ErrTruncated is returned in StrictEOF mode when the stream ends inside a frame (no terminating
	// empty line). Callers must not treat the partial frame as delivered.
	ErrTruncated = errors.New("sse: stream ended inside a frame")
	// ErrInvalidUTF8 is returned when a frame carries invalid UTF-8.
	ErrInvalidUTF8 = errors.New("sse: frame is not valid UTF-8")
)

// Event is one dispatched frame.
type Event struct {
	// ID is the `id:` field of this frame; HasID reports whether the field was present at all
	// (an empty id field resets the last event id, see WHATWG).
	ID    string
	HasID bool
	// Event is the `event:` field ("" when absent).
	Event string
	// Data is the `data:` lines joined with "\n" (trailing newline removed). It is "" when the frame
	// carried no data field.
	Data string
}

// Options configures a Reader.
type Options struct {
	// MaxFrameBytes bounds the bytes of one frame (all field lines). 0 selects DefaultMaxFrameBytes.
	MaxFrameBytes int
	// StrictEOF makes the reader return ErrTruncated instead of dispatching a frame that was not
	// terminated by an empty line before EOF.
	StrictEOF bool
}

// Reader parses frames from an io.Reader.
type Reader struct {
	src    *bufio.Reader
	opts   Options
	lastID string
	idMu   sync.RWMutex
	done   bool
	skipLF bool // a CR already ended the previous line; consume an optional following LF

	// current frame
	seen    bool
	hasData bool
	data    []byte
	event   string
	id      string
	hasID   bool
	size    int
}

// NewReader wraps r.
func NewReader(r io.Reader, opts Options) *Reader {
	if opts.MaxFrameBytes <= 0 {
		opts.MaxFrameBytes = DefaultMaxFrameBytes
	}
	return &Reader{src: bufio.NewReader(r), opts: opts}
}

// LastEventID returns the most recent `id:` value seen (the value to send as Last-Event-ID when
// resuming). It changes only through id fields; it is not advanced by EOF or by any other cursor.
func (r *Reader) LastEventID() string {
	r.idMu.RLock()
	defer r.idMu.RUnlock()
	return r.lastID
}

// Next returns the next dispatched frame. It returns io.EOF after the stream ended cleanly.
//
// As in WHATWG "dispatch the event" (and the Node client's SseParser), a frame that carried no data
// field at all — bare id:, event: or retry: lines — is not dispatched: its id: still updates
// LastEventID and the reader moves on to the next frame. A frame whose data field is present but empty
// (`data:`) is dispatched with Data == "".
func (r *Reader) Next() (*Event, error) {
	if r.done {
		return nil, io.EOF
	}
	for {
		line, eof, err := r.readLine()
		if err != nil {
			r.done = true
			return nil, err
		}
		if eof {
			r.done = true
			if !r.seen {
				return nil, io.EOF
			}
			if r.opts.StrictEOF {
				return nil, ErrTruncated
			}
			ev, err := r.dispatch()
			if err != nil || ev != nil {
				return ev, err
			}
			return nil, io.EOF
		}
		if len(line) == 0 {
			if !r.seen {
				continue
			}
			ev, err := r.dispatch()
			if err != nil || ev != nil {
				return ev, err
			}
			continue
		}
		if line[0] == ':' {
			continue
		}
		r.size += len(line)
		if r.size > r.opts.MaxFrameBytes {
			r.done = true
			return nil, ErrFrameTooLarge
		}
		field, value := splitField(line)
		switch string(field) {
		case "data":
			if r.hasData {
				r.data = append(r.data, '\n')
			}
			r.data = append(r.data, value...)
			r.hasData = true
			r.seen = true
		case "event":
			r.event = string(value)
			r.seen = true
		case "id":
			if bytes.IndexByte(value, 0) < 0 {
				r.id = string(value)
				r.hasID = true
				r.seen = true
			}
		case "retry":
			// transport hint only; this reader does not reconnect
			r.seen = true
		default:
			// unknown fields are ignored per WHATWG
		}
	}
}

// dispatch ends the current frame. The id: buffer becomes LastEventID; a frame without a data field
// yields (nil, nil) and is not delivered.
func (r *Reader) dispatch() (*Event, error) {
	ev := &Event{ID: r.id, HasID: r.hasID, Event: r.event, Data: string(r.data)}
	hasData := r.hasData
	r.seen, r.hasData, r.data, r.event, r.id, r.hasID, r.size = false, false, r.data[:0], "", "", false, 0
	if !utf8.ValidString(ev.Data) || !utf8.ValidString(ev.Event) || !utf8.ValidString(ev.ID) {
		r.done = true
		return nil, ErrInvalidUTF8
	}
	if ev.HasID {
		r.idMu.Lock()
		r.lastID = ev.ID
		r.idMu.Unlock()
	}
	if !hasData {
		return nil, nil
	}
	return ev, nil
}

// splitField splits "field: value" (a single leading space of the value is removed).
func splitField(line []byte) ([]byte, []byte) {
	i := bytes.IndexByte(line, ':')
	if i < 0 {
		return line, nil
	}
	value := line[i+1:]
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return line[:i], value
}

// readLine reads one line terminated by CRLF, LF or CR. eof is true when the source ended without a
// further line. A partial line at EOF is delivered first (eof=false) and then eof=true follows.
func (r *Reader) readLine() (line []byte, eof bool, err error) {
	var buf []byte
	for {
		b, rerr := r.src.ReadByte()
		if rerr != nil {
			if rerr == io.EOF {
				if len(buf) == 0 {
					return nil, true, nil
				}
				return buf, false, nil
			}
			return nil, false, rerr
		}
		if r.skipLF {
			r.skipLF = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\n':
			return buf, false, nil
		case '\r':
			// A CR is already a complete line ending. Looking ahead here blocks
			// delivery of a complete CR-delimited frame on a still-open stream.
			r.skipLF = true
			return buf, false, nil
		}
		buf = append(buf, b)
		if len(buf) > r.opts.MaxFrameBytes {
			return nil, false, ErrFrameTooLarge
		}
	}
}
