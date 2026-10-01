package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// DomainRetryActionMap maps each family's own retryAction word to the unified action (RFC-UAPI-1 §2.3).
// Unregistered words map to none.
var DomainRetryActionMap = map[string]string{
	"none":               "none",
	"same-request":       "same-request",
	"backoff":            "same-request",
	"query-status":       "query-status",
	"reconcile":          "query-status",
	"rebind":             "rebind",
	"refresh":            "refresh",
	"refresh-projection": "refresh",
	"discover":           "rediscover",
	"rediscover":         "rediscover",
}

// resultUnknownCodes have an unknown side effect: only query-status / rebind are ever permitted and a
// replay with a fresh key is forbidden (manual §16.6 item 3).
var resultUnknownCodes = map[string]bool{"result_unknown": true, "commit_unknown": true}

// RetryAdvice normalises the retry guidance of an error.
type RetryAdvice struct {
	// Action is the unified action (RetryActions vocabulary).
	Action string
	// Stated is true when the server explicitly gave an action (/v2 family envelopes have none → false;
	// never inferred as replayable).
	Stated bool
	// DomainRetryAction is the family's own word ("" when absent).
	DomainRetryAction string
	// RetryAfter is the wait requested by the server (HasRetryAfter false when none).
	RetryAfter    time.Duration
	HasRetryAfter bool
	// Replayable: Action == same-request ∧ stated ∧ not an unknown-side-effect code.
	Replayable bool
	// ClosureID is the current closure id reported with rediscover (412 closure_stale); "" otherwise.
	ClosureID string
	// Source is unified, domain or none.
	Source string
}

// Advice derives RetryAdvice from any error. query-status / rebind / refresh / rediscover are advisory
// only: the caller performs them; this package never does.
func Advice(err error) RetryAdvice {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		unknown := resultUnknownCodes[apiErr.Code] || resultUnknownCodes[apiErr.DomainCode()]
		action := apiErr.RetryAction
		if unknown && action == "same-request" {
			action = "query-status"
		}
		domainAction := apiErr.DomainRetryAction()
		if domainAction == "" {
			domainAction = apiErr.RetryAction
		}
		advice := RetryAdvice{Action: action, Stated: true, DomainRetryAction: domainAction, RetryAfter: apiErr.RetryAfter, HasRetryAfter: apiErr.HasRetryAfter, Replayable: action == "same-request", Source: "unified"}
		if action == "rediscover" {
			advice.ClosureID = apiErr.ClosureID()
		}
		return advice
	}
	var domainErr *DomainError
	if errors.As(err, &domainErr) {
		stated := domainErr.RetryAction != ""
		mapped := "none"
		if stated {
			if m, ok := DomainRetryActionMap[domainErr.RetryAction]; ok {
				mapped = m
			}
		}
		if resultUnknownCodes[domainErr.Code] && mapped == "same-request" {
			mapped = "query-status"
		}
		return RetryAdvice{Action: mapped, Stated: stated, DomainRetryAction: domainErr.RetryAction, RetryAfter: domainErr.RetryAfter, HasRetryAfter: domainErr.HasRetryAfter, Replayable: stated && mapped == "same-request", Source: "domain"}
	}
	return RetryAdvice{Action: "none", Source: "none"}
}

// SameRequest describes the original Call verbatim (same operation, same options, same
// IdempotencyKey / body requestId).
type SameRequest struct {
	Operation string
	Options   CallOptions
}

// IdempotencyKeyOf returns the key the request carried: Idempotency-Key first, then the body's
// top-level requestId, then request.requestId (the family control-envelope form); "" when none.
func IdempotencyKeyOf(req SameRequest) string {
	if req.Options.IdempotencyKey != "" {
		return req.Options.IdempotencyKey
	}
	switch body := req.Options.Body.(type) {
	case nil, []byte:
		return ""
	case map[string]any:
		if id, _ := body["requestId"].(string); id != "" {
			return id
		}
		if nested, ok := body["request"].(map[string]any); ok {
			id, _ := nested["requestId"].(string)
			return id
		}
		return ""
	case json.RawMessage:
		return requestIDOfJSON(body)
	default:
		data, err := json.Marshal(body)
		if err != nil {
			return ""
		}
		return requestIDOfJSON(data)
	}
}

func requestIDOfJSON(data []byte) string {
	var probe struct {
		RequestID string `json:"requestId"`
		Request   struct {
			RequestID string `json:"requestId"`
		} `json:"request"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return ""
	}
	if probe.RequestID != "" {
		return probe.RequestID
	}
	return probe.Request.RequestID
}

// RetryOptions tune RetrySameRequest.
type RetryOptions struct {
	// MaxWait bounds a single wait (default 30 s). A server value above it fails with
	// CodeRetryAfterExceedsBudget instead of hanging.
	MaxWait time.Duration
	// Sleep is injectable for tests (default: timer honouring ctx).
	Sleep func(ctx context.Context, d time.Duration) error
}

const defaultMaxWait = 30 * time.Second

func defaultSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// RetrySameRequest replays the original request exactly once: only when the server stated
// same-request, the request carries the same idempotency key and after honouring RetryAfter.
// Any other action fails with CodeNotRetryable (the caller follows Advice instead); a missing key
// fails with CodeRetryKeyMissing.
func RetrySameRequest(ctx context.Context, client *Client, cause error, req SameRequest, opts RetryOptions) (*Result, error) {
	advice := Advice(cause)
	if !advice.Replayable {
		detail := "server advised " + advice.Action
		if !advice.Stated {
			detail += " (not stated)"
		}
		return nil, newClientError(CodeNotRetryable, detail+"; same-request replay is not permitted")
	}
	if IdempotencyKeyOf(req) == "" {
		return nil, newClientError(CodeRetryKeyMissing, "same-request replay requires the original Idempotency-Key or body.requestId")
	}
	maxWait := opts.MaxWait
	if maxWait == 0 {
		maxWait = defaultMaxWait
	}
	if maxWait < 0 {
		return nil, newClientError(CodeInvalidOptions, "MaxWait must not be negative")
	}
	if advice.HasRetryAfter && advice.RetryAfter > maxWait {
		return nil, newClientError(CodeRetryAfterExceedsBudget, "server asked to wait "+advice.RetryAfter.String()+", budget is "+maxWait.String())
	}
	if advice.HasRetryAfter && advice.RetryAfter > 0 {
		sleep := opts.Sleep
		if sleep == nil {
			sleep = defaultSleep
		}
		if err := sleep(ctx, advice.RetryAfter); err != nil {
			return nil, wrapClientError(CodeAborted, "", err)
		}
	}
	if ctx.Err() != nil {
		return nil, wrapClientError(CodeAborted, "", ctx.Err())
	}
	return client.Call(ctx, req.Operation, req.Options)
}
