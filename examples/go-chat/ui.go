package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/tansrai/tansr-go/examples/internal/demoutil"
	"github.com/tansrai/tansr-go/session"
)

type eventResult struct {
	event session.Event
	err   error
}
type inputLine struct {
	text string
	err  error
}
type chatHooks struct {
	send       func(context.Context, string) (int64, error)
	interrupt  func(context.Context) error
	permission func(context.Context, string, string, string) error
	answer     func(context.Context, string, []session.Answer) error
}

func readLines(ctx context.Context, reader io.Reader) <-chan inputLine {
	lines := make(chan inputLine)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 4096), 262144)
		for scanner.Scan() {
			select {
			case lines <- inputLine{text: scanner.Text()}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case lines <- inputLine{err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return lines
}

type permissionTicket struct{ requestID, digest string }

func chatLoop(ctx context.Context, hooks chatHooks, lines <-chan inputLine, events <-chan eventResult, out io.Writer, message string, active bool, floor int64) error {
	permissions := make(map[string]permissionTicket)
	questions := make(map[string]bool)
	oneShot := message != ""
	defer func() {
		if active {
			stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := hooks.interrupt(stop); err != nil {
				fmt.Fprintln(out, "interrupt failed; check session state:", demoutil.Text(demoutil.Describe(err).Error()))
			}
		}
	}()
	if message != "" {
		var err error
		floor, err = hooks.send(ctx, message)
		if err != nil {
			return err
		} // No blind retry after unknown acceptance.
		active = true
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				lines = nil
				if !active {
					return nil
				}
				continue
			}
			if line.err != nil {
				return line.err
			}
			text := strings.TrimSpace(line.text)
			if text == "" {
				continue
			}
			parts := strings.SplitN(text, " ", 3)
			switch parts[0] {
			case "/quit":
				return nil
			case "/cancel":
				if active {
					if err := hooks.interrupt(ctx); err != nil {
						return err
					}
					fmt.Fprintln(out, "interrupt accepted; waiting for a terminal event")
				}
			case "/allow", "/deny":
				if len(parts) != 2 {
					fmt.Fprintln(out, "usage: /allow <ticket> or /deny <ticket>")
					continue
				}
				ticket, ok := permissions[parts[1]]
				if !ok {
					fmt.Fprintln(out, "permission ticket is absent or already closed")
					continue
				}
				if err := hooks.permission(ctx, ticket.requestID, ticket.digest, strings.TrimPrefix(parts[0], "/")); err != nil {
					return err
				}
				delete(permissions, ticket.requestID)
			case "/answers":
				var answers []session.Answer
				if len(parts) != 3 || !questions[parts[1]] || json.Unmarshal([]byte(parts[2]), &answers) != nil || len(answers) == 0 {
					fmt.Fprintln(out, `usage: /answers <ticket> [{"questionId":"q1","selectedOptionIds":[],"freeText":"your answer"}]`)
					continue
				}
				if err := hooks.answer(ctx, parts[1], answers); err != nil {
					return err
				}
				delete(questions, parts[1])
			default:
				if strings.HasPrefix(text, "/") {
					fmt.Fprintln(out, "unknown command")
					continue
				}
				if active {
					fmt.Fprintln(out, "turn is running; wait or /cancel before a new message")
					continue
				}
				var err error
				floor, err = hooks.send(ctx, text)
				if err != nil {
					return err
				}
				active = true
			}
		case received, ok := <-events:
			if !ok || received.err == io.EOF {
				return errors.New("event stream ended without a turn result; EOF is not completion; resume the same session")
			}
			if received.err != nil {
				return received.err
			}
			event := received.event
			if event.Type == "server.replay.gap" {
				return errors.New("event replay has a gap; inspect history before resuming; no message was replayed")
			}
			if err := displayEvent(event, out, permissions, questions); err != nil {
				return err
			}
			if outcome, terminal := event.TurnOutcome(); terminal && active && eventSequence(event) > floor {
				active = false
				clear(permissions)
				clear(questions)
				if outcome.Status != session.OutcomeCompleted {
					return fmt.Errorf("turn did not complete: %s", outcome.Status)
				}
				fmt.Fprintln(out, "\n[turn completed]")
				if oneShot || lines == nil {
					return nil
				}
			}
		}
	}
}

func eventSequence(event session.Event) int64 {
	if event.Envelope != nil {
		if cursor, ok := event.Envelope.CursorSet.EventCursor.Text(); ok {
			value, err := strconv.ParseInt(cursor, 10, 64)
			if err == nil {
				return value
			}
		}
	}
	return -1
}

func displayEvent(event session.Event, out io.Writer, permissions map[string]permissionTicket, questions map[string]bool) error {
	var body struct {
		Text      string          `json:"text"`
		Name      string          `json:"name"`
		RequestID string          `json:"requestId"`
		Digest    string          `json:"digest"`
		Summary   string          `json:"summary"`
		Questions json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal(event.Raw, &body); err != nil {
		return errors.New("cannot display malformed session event")
	}
	switch event.Type {
	case "msg.text.delta":
		fmt.Fprint(out, demoutil.Text(body.Text))
	case "server.permission.request":
		p := body
		if p.RequestID == "" || p.Digest == "" {
			return errors.New("permission event has no ticket or digest")
		}
		permissions[p.RequestID] = permissionTicket{p.RequestID, p.Digest}
		fmt.Fprintf(out, "\n[permission %s] %s %s\n/allow %s or /deny %s (Serve checks expiry)\n", demoutil.Text(p.RequestID), demoutil.Text(p.Name), demoutil.Text(p.Summary), demoutil.Text(p.RequestID), demoutil.Text(p.RequestID))
	case "server.permission.closed":
		delete(permissions, body.RequestID)
	case "server.question.request":
		if body.RequestID == "" || len(body.Questions) == 0 {
			return errors.New("question event has no ticket or questions")
		}
		questions[body.RequestID] = true
		fmt.Fprintf(out, "\n[question %s] %s\nUse /answers <ticket> <JSON array>\n", demoutil.Text(body.RequestID), demoutil.Text(string(body.Questions)))
	case "server.question.closed":
		delete(questions, body.RequestID)
	case "tool.started", "tool.completed", "tool.failed":
		fmt.Fprintf(out, "\n[%s] %s\n", demoutil.Text(event.Type), demoutil.Text(body.Name))
	case "server.tool.request":
		return errors.New("legacy inline tool requested; use an explicitly bound go-tools executor")
	default:
		if !strings.HasPrefix(event.Type, "msg.") {
			fmt.Fprintf(out, "\n[%s]\n", demoutil.Text(event.Type))
		}
	}
	return nil
}
