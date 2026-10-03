// Command go-chat is the smallest end-to-end walk over the unified /api contract with the Go SDK:
// discover → create a session → read its capability closure → send one message → read the event
// stream until a terminal event → close the session. Every request goes through package api by
// operation name; this file contains no URL path.
//
//	go run ./examples/go-chat -base http://127.0.0.1:8787 -token "$TANSR_TOKEN" -message "hello"
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cpple/tansr-go/api"
)

func main() {
	base := flag.String("base", "http://127.0.0.1:8787", "Serve origin (http(s)://host[:port])")
	token := flag.String("token", os.Getenv("TANSR_TOKEN"), "bearer token (or TANSR_TOKEN)")
	message := flag.String("message", "hello", "message text to send")
	timeout := flag.Duration("timeout", 60*time.Second, "overall deadline")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := run(ctx, *base, *token, *message); err != nil {
		fmt.Fprintln(os.Stderr, "go-chat:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, base, token, message string) error {
	client, err := api.New(api.Options{BaseURL: base, Token: token, EventEnvelope: true})
	if err != nil {
		return err
	}

	// 1. Discover: the manifest revision is observed, never used to switch behaviour.
	manifest, err := client.Manifest(ctx)
	if err != nil {
		return describe(err)
	}
	fmt.Printf("manifest revision %d (locked %d), %d operations\n", manifest.Body.Revision, api.ManifestRevision, len(manifest.Body.Operations))
	caps, err := client.Capabilities(ctx)
	if err != nil {
		return describe(err)
	}
	if !caps.Body.Domains.Session.Installed {
		return errors.New("this deployment has no session domain installed")
	}

	// 2. Create a session (agent-session-v1, plain JSON body).
	created, err := client.Call(ctx, api.OpSessionCreate, api.CallOptions{Body: map[string]any{}})
	if err != nil {
		return describe(err)
	}
	var session struct {
		ID string `json:"id"`
	}
	if err := created.Decode(&session); err != nil || session.ID == "" {
		return fmt.Errorf("session.create returned no id: %v", err)
	}
	fmt.Printf("session %s created (http %d)\n", session.ID, created.Status)
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := client.Call(closeCtx, api.OpSessionClose, api.CallOptions{Params: map[string]string{"id": session.ID}}); err != nil {
			fmt.Fprintln(os.Stderr, "session.close:", describe(err))
		}
	}()

	// 3. Read the capability closure and check the fence before writing.
	closure, err := client.SessionCapabilities(ctx, session.ID)
	if err != nil {
		return describe(err)
	}
	if state := closure.Closure.Operations[api.OpSessionMessageSend]; state != api.StateEnabled {
		return fmt.Errorf("%s is %s for this session", api.OpSessionMessageSend, state)
	}

	// 4. Open the event stream first (negotiated envelope), then send the message with the closure id.
	stream, err := client.Events(ctx, api.OpSessionEventsObserve, api.EventsOptions{Params: map[string]string{"id": session.ID}})
	if err != nil {
		return describe(err)
	}
	defer stream.Close()
	accepted, err := client.Call(ctx, api.OpSessionMessageSend, api.CallOptions{
		Params:         map[string]string{"id": session.ID},
		Body:           map[string]any{"text": message},
		ClosureID:      closure.ClosureID,
		IdempotencyKey: fmt.Sprintf("go-chat-%d", time.Now().UnixNano()),
	})
	if err != nil {
		return describe(err)
	}
	fmt.Printf("message accepted (http %d) — acceptance is not completion\n", accepted.Status)

	// 5. Read frames until a terminal event; EOF alone is not completion.
	for {
		frame, err := stream.Next()
		if err == io.EOF {
			return errors.New("stream ended without a terminal event (EOF ≠ completion)")
		}
		if err != nil {
			return describe(err)
		}
		env := frame.Envelope
		eventType := "<unknown>"
		if env.Type != nil {
			eventType = *env.Type
		}
		fmt.Printf("event %s domain=%s cursor=%s raw=%s\n", eventType, env.Domain, cursorText(env), truncate(string(env.Raw), 120))
		if env.IsTerminal() {
			fmt.Printf("terminal status: %s\n", *env.TerminalStatus)
			return nil
		}
	}
}

func cursorText(env *api.EventEnvelope) string {
	if text, ok := env.CursorSet.EventCursor.Text(); ok {
		return text
	}
	return "-"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// describe renders the error surface without echoing tokens or bodies. The unified code decides the
// branch (plan D19: one `switch code` across every tansr SDK); the family code is only shown as detail.
func describe(err error) error {
	var apiErr *api.APIError
	var domainErr *api.DomainError
	var clientErr *api.ClientError
	switch {
	case errors.Is(err, api.ErrContractUnavailable):
		return fmt.Errorf("unified contract unavailable — this is not a unified-v1 Serve; the SDK does not fall back to legacy paths: %w", err)
	case errors.Is(err, api.ErrEnvelopeNotNegotiated):
		return fmt.Errorf("server did not echo the event envelope negotiation: %w", err)
	case errors.As(err, &apiErr):
		advice := api.Advice(apiErr)
		hint := ""
		switch apiErr.Code {
		case api.CodeCapabilityUnavailable:
			hint = " — not installed or outside the closure on this deployment; nothing to fall back to"
		case api.CodePreconditionFailed:
			hint = " — re-read (closure / resource revision) before writing again"
		case api.CodeResultUnknown:
			hint = " — side effect unknown: query the receipt, never replay with a new key"
		}
		detail := ""
		if apiErr.Detail.Present() {
			detail = fmt.Sprintf(" [reason=%q domainCode=%q]", apiErr.Detail.Reason, apiErr.Detail.DomainCode)
		}
		return fmt.Errorf("%w (retryAction %s, replayable %v)%s%s", apiErr, advice.Action, advice.Replayable, detail, hint)
	case errors.As(err, &domainErr):
		return fmt.Errorf("%w (unwrapped family envelope, retryAction %q → %s)", domainErr, domainErr.RetryAction, api.Advice(domainErr).Action)
	case errors.As(err, &clientErr):
		return fmt.Errorf("local: %w", clientErr)
	}
	return err
}
