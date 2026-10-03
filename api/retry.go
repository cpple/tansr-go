package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// DomainRetryActionMap maps each family's own retryAction word to the unified action (RFC-UAPI-1 §2.3).
// Unregistered words map to none.
var DomainRetryActionMap = map[string]RetryAction{
	"none":               ActionNone,
	"same-request":       ActionSameRequest,
	"backoff":            ActionSameRequest,
	"query-status":       ActionQueryStatus,
	"reconcile":          ActionQueryStatus,
	"rebind":             ActionRebind,
	"refresh":            ActionRefresh,
	"refresh-projection": ActionRefresh,
	"discover":           ActionRediscover,
	"rediscover":         ActionRediscover,
}

// resultUnknownCodes have an unknown side effect: only query-status / rebind are ever permitted and a
// replay with a fresh key is forbidden (manual §16.6 item 3).
var resultUnknownCodes = map[string]bool{"result_unknown": true, "commit_unknown": true}

// RetryAdvice normalises the retry guidance of an error.
type RetryAdvice struct {
	// Action is the unified action (RetryActions vocabulary).
	Action RetryAction
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
		unknown := resultUnknownCodes[string(apiErr.Code)] || resultUnknownCodes[apiErr.Detail.DomainCode]
		action := apiErr.RetryAction
		if unknown && action == ActionSameRequest {
			action = ActionQueryStatus
		}
		domainAction := apiErr.Detail.DomainRetryAction
		if domainAction == "" {
			domainAction = string(apiErr.RetryAction)
		}
		advice := RetryAdvice{Action: action, Stated: true, DomainRetryAction: domainAction, RetryAfter: apiErr.RetryAfter, HasRetryAfter: apiErr.HasRetryAfter, Replayable: action == ActionSameRequest, Source: "unified"}
		if action == ActionRediscover {
			advice.ClosureID = apiErr.ClosureID()
		}
		return advice
	}
	var domainErr *DomainError
	if errors.As(err, &domainErr) {
		stated := domainErr.RetryAction != ""
		mapped := ActionNone
		if stated {
			if m, ok := DomainRetryActionMap[domainErr.RetryAction]; ok {
				mapped = m
			}
		}
		if resultUnknownCodes[domainErr.Code] && mapped == ActionSameRequest {
			mapped = ActionQueryStatus
		}
		return RetryAdvice{Action: mapped, Stated: stated, DomainRetryAction: domainErr.RetryAction, RetryAfter: domainErr.RetryAfter, HasRetryAfter: domainErr.HasRetryAfter, Replayable: stated && mapped == ActionSameRequest, Source: "domain"}
	}
	return RetryAdvice{Action: ActionNone, Source: "none"}
}

// SameRequest describes the original Call verbatim (same operation, same options, same
// IdempotencyKey / body requestId).
type SameRequest struct {
	Operation string
	Options   CallOptions
}

// IdempotencyKeyOf returns the key the request carried: Idempotency-Key first, then the body position
// the operation's family declares as its requestId (manifest r7 requestIdPath, e.g. request.requestId
// for sdk2-ext-v1; agent-session-v1 has none and relies on the header); "" when none.
func IdempotencyKeyOf(req SameRequest) string {
	if req.Options.IdempotencyKey != "" {
		return req.Options.IdempotencyKey
	}
	op, ok := Lookup(req.Operation)
	if !ok || op.RequestIDPath() == nil {
		return ""
	}
	return stringAtPath(bodyAsGeneric(req.Options.Body), op.RequestIDPath())
}

// bodyAsGeneric views a CallOptions.Body as decoded JSON (nil for no body / octet-stream / unencodable).
func bodyAsGeneric(body any) any {
	switch b := body.(type) {
	case nil, []byte:
		return nil
	case map[string]any:
		return b
	case json.RawMessage:
		var decoded any
		if json.Unmarshal(b, &decoded) != nil {
			return nil
		}
		return decoded
	default:
		data, err := json.Marshal(body)
		if err != nil {
			return nil
		}
		var decoded any
		if json.Unmarshal(data, &decoded) != nil {
			return nil
		}
		return decoded
	}
}

// stringAtPath walks a KeyPath through decoded JSON and returns the string found ("" otherwise).
func stringAtPath(value any, path []string) string {
	for _, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value, ok = object[key]
		if !ok {
			return ""
		}
	}
	text, _ := value.(string)
	return text
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
		detail := "server advised " + string(advice.Action)
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
	if deadline := req.Options.Deadline; !deadline.IsZero() {
		// The deadline is the caller's, never the SDK's to extend: a replay that could not even start
		// before it fails here without sleeping (Node ./api retrySameRequest: now + wait >= deadline).
		wait := time.Duration(0)
		if advice.HasRetryAfter && advice.RetryAfter > 0 {
			wait = advice.RetryAfter
		}
		if !client.now().Add(wait).Before(deadline) {
			return nil, newClientError(CodeDeadlineExceeded, "replay would start at or after Deadline "+FormatDeadline(deadline)+"; the request was not replayed")
		}
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
