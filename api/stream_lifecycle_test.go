package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpple/tansr-go/sse"
)

func TestInjectedClientCannotRedirectAuthenticatedRequests(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/sessions", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	var policyCalls atomic.Int32
	injected := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		policyCalls.Add(1)
		return nil
	}}
	c, err := New(Options{BaseURL: origin.URL, HTTPClient: injected, Token: "synthetic-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Call(context.Background(), OpSessionCreate, CallOptions{Body: map[string]any{}})
	if !errors.Is(err, errRedirect) {
		t.Fatalf("create followed redirect: %v", err)
	}
	_, err = c.Events(context.Background(), OpSessionEventsObserve, EventsOptions{Params: map[string]string{"id": "synthetic"}})
	if !errors.Is(err, errRedirect) {
		t.Fatalf("events followed redirect: %v", err)
	}
	if redirected.Load() != 0 || policyCalls.Load() != 0 {
		t.Fatal("redirect reached caller policy or foreign server")
	}
	// The SDK must not mutate a client shared with unrelated HTTP users.
	resp, err := injected.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if policyCalls.Load() != 1 || redirected.Load() != 1 || c.http.Timeout != injected.Timeout {
		t.Fatal("injected client was changed or its transport options were lost")
	}
}

type countedBody struct {
	io.ReadCloser
	closes atomic.Int32
}

func (b *countedBody) Close() error {
	b.closes.Add(1)
	return b.ReadCloser.Close()
}

func testStream(body *countedBody, negotiated bool) *Stream {
	ctx, cancel := context.WithCancel(context.Background())
	return &Stream{body: body, reader: sse.NewReader(body, sse.Options{StrictEOF: true}), ctx: ctx, cancel: cancel, negotiated: negotiated}
}

func TestStreamReleasesBodyOnEOFAndInvalidFrames(t *testing.T) {
	for _, tc := range []struct {
		name, wire string
		negotiated bool
		want       ClientErrorCode
	}{
		{name: "EOF"},
		{name: "truncated", wire: "data: incomplete", want: CodeInvalidResponse},
		{name: "UTF8", wire: "data: \xff\n\n", want: CodeInvalidResponse},
		{name: "envelope", wire: "data: {}\n\n", negotiated: true, want: CodeInvalidEnvelope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &countedBody{ReadCloser: io.NopCloser(strings.NewReader(tc.wire))}
			s := testStream(body, tc.negotiated)
			_, err := s.Next()
			if tc.want == "" {
				if !errors.Is(err, io.EOF) {
					t.Fatalf("want EOF, got %v", err)
				}
			} else {
				var ce *ClientError
				if !errors.As(err, &ce) || ce.Code != tc.want {
					t.Fatalf("want %s, got %v", tc.want, err)
				}
			}
			if body.closes.Load() != 1 || s.ctx.Err() == nil {
				t.Fatal("finished stream retained its body or context")
			}
			_ = s.Close()
			_, err = s.Next()
			if body.closes.Load() != 1 || !errors.Is(err, io.EOF) {
				t.Fatal("stream cleanup was not idempotent")
			}
		})
	}
}

func TestStreamConcurrentReadCursorAndClose(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	body := &countedBody{ReadCloser: r}
	s := testStream(body, false)
	readDone := make(chan error, 1)
	go func() {
		for {
			if _, err := s.Next(); err != nil {
				readDone <- err
				return
			}
		}
	}()
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for i := 0; i < 100; i++ {
			_ = s.LastEventID()
		}
	}()
	for i := 0; i < 20; i++ {
		if _, err := io.WriteString(w, "id: cursor\ndata: test\n\n"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 10; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); _ = s.Close() }()
	}
	workers.Wait()
	select {
	case err := <-readDone:
		var ce *ClientError
		if !errors.Is(err, io.EOF) && (!errors.As(err, &ce) || ce.Code != CodeAborted) {
			t.Fatalf("close result: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close failed to unblock Next")
	}
	if body.closes.Load() != 1 || s.LastEventID() != "cursor" {
		t.Fatal("duplicate close or lost stream cursor")
	}
}
