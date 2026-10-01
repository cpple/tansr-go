package api

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/cpple/tansr-go/sse"
)

// EventsOptions are the inputs of Events.
type EventsOptions struct {
	Params map[string]string
	Query  map[string]string
	// LastEventID → Last-Event-ID (event-stream resumption only; it advances no other cursor).
	LastEventID string
	// OnOpen is called once when the stream is established (200 text/event-stream, negotiation
	// verified). Connection success advances no cursor and signals no completion.
	OnOpen func(Meta)
}

// Frame is one delivered SSE frame. Envelope is set only when the client negotiated the unified
// envelope; otherwise Data carries the family's raw frame text unchanged.
type Frame struct {
	ID    string
	HasID bool
	Event string
	Data  string
	// Envelope is the parsed unified envelope (negotiated streams only).
	Envelope *EventEnvelope
}

// Stream is an open event stream. Next returns io.EOF when the server closed the stream cleanly;
// EOF is a transport fact, not a completion signal (manual §16.6 item 5).
type Stream struct {
	meta       Meta
	negotiated bool
	body       io.Closer
	reader     *sse.Reader
	cancel     context.CancelFunc
	ctx        context.Context
	done       bool
}

// Meta returns the unified headers of the stream response.
func (s *Stream) Meta() Meta { return s.meta }

// LastEventID returns the most recent id: seen (for Last-Event-ID resumption).
func (s *Stream) LastEventID() string { return s.reader.LastEventID() }

// Close releases the connection. It is safe to call more than once.
func (s *Stream) Close() error {
	s.done = true
	s.cancel()
	return s.body.Close()
}

// Next returns the next frame, io.EOF at a clean end of stream, or an error. In negotiated mode pure
// transport hint frames (no id, no event, empty data) are skipped and every other frame must decode
// as an EventEnvelope (malformed → *ClientError CodeInvalidEnvelope; the stream is then unusable).
func (s *Stream) Next() (*Frame, error) {
	if s.done {
		return nil, io.EOF
	}
	for {
		ev, err := s.reader.Next()
		if err != nil {
			s.done = true
			switch {
			case err == io.EOF:
				return nil, io.EOF
			case s.ctx.Err() != nil:
				return nil, wrapClientError(CodeAborted, "", s.ctx.Err())
			case errors.Is(err, sse.ErrFrameTooLarge):
				return nil, wrapClientError(CodePayloadTooLarge, "", err)
			case errors.Is(err, sse.ErrTruncated), errors.Is(err, sse.ErrInvalidUTF8):
				return nil, wrapClientError(CodeInvalidResponse, err.Error(), err)
			default:
				return nil, wrapClientError(CodeNetworkError, "", err)
			}
		}
		frame := &Frame{ID: ev.ID, HasID: ev.HasID, Event: ev.Event, Data: ev.Data}
		if !s.negotiated {
			return frame, nil
		}
		if !ev.HasID && ev.Event == "" && ev.Data == "" {
			continue
		}
		envelope, err := ParseEventEnvelope([]byte(ev.Data))
		if err != nil {
			s.done = true
			return nil, err
		}
		frame.Envelope = envelope
		return frame, nil
	}
}

// Events opens an SSE operation. With Options.EventEnvelope the request carries
// tansr-event-envelope: unified-v1 and the echo is required: a missing echo fails with
// ErrEnvelopeNotNegotiated instead of silently delivering raw frames; an echo without a request fails
// with CodeInvalidResponse.
func (c *Client) Events(ctx context.Context, operation string, opts EventsOptions) (*Stream, error) {
	op, err := c.operation(operation)
	if err != nil {
		return nil, err
	}
	if op.Kind != "stream" {
		return nil, newClientError(CodeInvalidOperation, operation+" is not an SSE stream; use Call()")
	}
	path, err := InstantiatePath(op, opts.Params)
	if err != nil {
		return nil, err
	}
	query, err := BuildQuery(op, opts.Query)
	if err != nil {
		return nil, err
	}
	headers, err := c.headers(ctx, "text/event-stream")
	if err != nil {
		return nil, err
	}
	if opts.LastEventID != "" {
		if !lastEventIDRule.MatchString(opts.LastEventID) {
			return nil, newClientError(CodeInvalidLastEventID, "Last-Event-ID must be 1–256 printable ASCII characters")
		}
		headers.Set("Last-Event-ID", opts.LastEventID)
	}
	if c.eventEnvelope {
		headers.Set(HeaderEventEnvelope, Contract)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(streamCtx, op.Method, c.baseURL+path+query, nil)
	if err != nil {
		cancel()
		return nil, wrapClientError(CodeInvalidOptions, "request could not be built", err)
	}
	req.Header = headers
	resp, err := c.send(streamCtx, req)
	if err != nil {
		cancel()
		return nil, err
	}
	meta, err := readUnifiedHeaders(resp)
	if err != nil {
		resp.Body.Close()
		cancel()
		return nil, err
	}
	c.observe(meta)
	if resp.StatusCode >= 400 {
		err := c.errorResponse(streamCtx, resp, meta, op)
		resp.Body.Close()
		cancel()
		return nil, err
	}
	ct := contentTypeOf(resp.Header)
	if resp.StatusCode != 200 || ct != "text/event-stream" {
		resp.Body.Close()
		cancel()
		return nil, newClientError(CodeInvalidResponse, "expected 200 text/event-stream, got "+http.StatusText(resp.StatusCode)+" "+ct)
	}
	if c.eventEnvelope && !meta.EventEnvelope {
		resp.Body.Close()
		cancel()
		return nil, newClientError(CodeEnvelopeNotNegotiated, "server did not echo "+HeaderEventEnvelope+": "+Contract)
	}
	if !c.eventEnvelope && meta.EventEnvelope {
		resp.Body.Close()
		cancel()
		return nil, newClientError(CodeInvalidResponse, "server echoed "+HeaderEventEnvelope+" without negotiation")
	}
	if opts.OnOpen != nil {
		opts.OnOpen(meta)
	}
	return &Stream{
		meta:       meta,
		negotiated: c.eventEnvelope,
		body:       resp.Body,
		reader:     sse.NewReader(resp.Body, sse.Options{MaxFrameBytes: c.maxFrame, StrictEOF: true}),
		cancel:     cancel,
		ctx:        streamCtx,
	}, nil
}
