package api

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// ClientErrorCode enumerates local failures (options, placeholders, query keys, envelope negotiation,
// network, cancellation). No server wire code is added here; the vocabulary mirrors the Node client.
type ClientErrorCode string

const (
	CodeInvalidOptions          ClientErrorCode = "invalid_options"
	CodeInvalidOperation        ClientErrorCode = "invalid_operation"
	CodeInvalidParams           ClientErrorCode = "invalid_params"
	CodeInvalidQuery            ClientErrorCode = "invalid_query"
	CodeInvalidBody             ClientErrorCode = "invalid_body"
	CodeInvalidClosureID        ClientErrorCode = "invalid_closure_id"
	CodeClosureIDNotApplicable  ClientErrorCode = "closure_id_not_applicable"
	CodeInvalidIdempotencyKey   ClientErrorCode = "invalid_idempotency_key"
	CodeInvalidLastEventID      ClientErrorCode = "invalid_last_event_id"
	CodeAborted                 ClientErrorCode = "aborted"
	CodeNetworkError            ClientErrorCode = "network_error"
	CodeInvalidResponse         ClientErrorCode = "invalid_response"
	CodePayloadTooLarge         ClientErrorCode = "payload_too_large"
	CodeEnvelopeNotNegotiated   ClientErrorCode = "envelope_not_negotiated"
	CodeInvalidEnvelope         ClientErrorCode = "invalid_envelope"
	CodeNotRetryable            ClientErrorCode = "not_retryable"
	CodeRetryKeyMissing         ClientErrorCode = "retry_key_missing"
	CodeRetryAfterExceedsBudget ClientErrorCode = "retry_after_exceeds_budget"
)

// ClientError is a local failure. It never carries response bodies, configuration or tokens.
type ClientError struct {
	Code   ClientErrorCode
	Detail string
	// Err is the underlying cause (network error, context error, validation error), if any.
	Err error
}

func (e *ClientError) Error() string {
	if e.Detail == "" {
		return "api client: " + string(e.Code)
	}
	return "api client: " + string(e.Code) + " — " + e.Detail
}

// Unwrap exposes the cause for errors.Is / errors.As.
func (e *ClientError) Unwrap() error { return e.Err }

// Is lets errors.Is(err, ErrEnvelopeNotNegotiated) and friends match on the code.
func (e *ClientError) Is(target error) bool {
	t, ok := target.(*ClientError)
	return ok && t.Code == e.Code && t.Detail == "" && t.Err == nil
}

func newClientError(code ClientErrorCode, detail string) *ClientError {
	return &ClientError{Code: code, Detail: detail}
}

func wrapClientError(code ClientErrorCode, detail string, err error) *ClientError {
	return &ClientError{Code: code, Detail: detail, Err: err}
}

// ErrEnvelopeNotNegotiated is matched by errors.Is when Events() requested the unified envelope and
// the server did not echo tansr-event-envelope: unified-v1 (manual §16.6 item 1: no silent downgrade
// to raw frames).
var ErrEnvelopeNotNegotiated = &ClientError{Code: CodeEnvelopeNotNegotiated}

// ErrAborted is matched by errors.Is when the caller's context ended the request.
var ErrAborted = &ClientError{Code: CodeAborted}

// ContractUnavailableReason classifies why a response could not be read as unified-v1.
type ContractUnavailableReason string

const (
	// ReasonMissingContractHeader: no tansr-contract header (legacy serve, SDK1-only deployment, proxy page).
	ReasonMissingContractHeader ContractUnavailableReason = "missing_contract_header"
	// ReasonContractMismatch: tansr-contract present but not unified-v1.
	ReasonContractMismatch ContractUnavailableReason = "contract_mismatch"
	// ReasonInvalidContractHeaders: the other unified headers are absent or malformed.
	ReasonInvalidContractHeaders ContractUnavailableReason = "invalid_contract_headers"
	// ReasonNonJSONBody: body is not JSON (HTML, text, unknown content-type).
	ReasonNonJSONBody ContractUnavailableReason = "non_json_body"
	// ReasonInvalidJSON: declared JSON but unparsable.
	ReasonInvalidJSON ContractUnavailableReason = "invalid_json"
	// ReasonInvalidErrorBody: error response body is not a recognisable envelope object.
	ReasonInvalidErrorBody ContractUnavailableReason = "invalid_error_body"
)

// ErrContractUnavailable is the sentinel matched by errors.Is for every *ContractUnavailableError.
var ErrContractUnavailable = errors.New("unified contract unavailable")

// ContractUnavailableError reports that the unified contract could not be read from a response.
// The client never falls back to legacy prefixes or to another parser on this error (§16.6 item 1).
type ContractUnavailableError struct {
	Reason      ContractUnavailableReason
	Status      int
	ContentType string
	// Contract is the tansr-contract header value actually received ("" when absent).
	Contract string
}

func (e *ContractUnavailableError) Error() string {
	return fmt.Sprintf("unified contract unavailable: %s (http %d)", e.Reason, e.Status)
}

// Is makes errors.Is(err, ErrContractUnavailable) true.
func (e *ContractUnavailableError) Is(target error) bool { return target == ErrContractUnavailable }

// ErrorCodes is the 19-code unified vocabulary (schema UnifiedCode; RFC-UAPI-1 §2.2).
var ErrorCodes = []string{
	"invalid_request", "protocol_mismatch", "unauthorized", "forbidden", "not_found", "method_not_allowed",
	"gone", "conflict", "stale_generation", "gap", "capability_unavailable", "capacity_exceeded",
	"payload_too_large", "upstream_unavailable", "result_unknown", "rejected", "internal_error",
	"precondition_failed", "not_canonical",
}

// FacadeErrorCodes is the 8-code facade subset (schema FacadeCode).
var FacadeErrorCodes = []string{
	"unauthorized", "not_found", "method_not_allowed", "invalid_request", "capability_unavailable",
	"capacity_exceeded", "precondition_failed", "upstream_unavailable",
}

// RetryActions is the unified retryAction vocabulary (schema RetryAction; RFC-UAPI-1 §2.3).
var RetryActions = []string{"none", "same-request", "query-status", "rebind", "refresh", "rediscover"}

// DomainRetryActions is the union of the per-family retryAction words kept in detail.domainRetryAction.
var DomainRetryActions = []string{"none", "query-status", "rebind", "same-request", "refresh-projection", "backoff", "discover", "reconcile", "refresh"}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// APIError is a unified error envelope `{contract:'unified-v1', traceId, requestId, code, status,
// retryAction, retryAfterMs?, message, detail?}` (schema FacadeError / UnifiedError).
type APIError struct {
	Code   string
	Status int
	// RetryAction is the unified action stated by the server (RetryActions vocabulary).
	RetryAction string
	// TraceID echoes x-request-id (observability only; unrelated to the idempotency key).
	TraceID string
	// RequestID is the client idempotency key as seen by the server; nil for facade-owned errors.
	RequestID *string
	// Message is the server message (≤ 1024 characters).
	Message string
	// Detail is the open detail object (nil when absent). Known keys: domain, family, domainCode,
	// domainStatus, domainRetryAction, fallback, reason, header, closureId, operation, state.
	Detail map[string]any
	// RetryAfter is the wait requested by the server (body retryAfterMs first, then Retry-After header);
	// zero when neither was given (HasRetryAfter is then false).
	RetryAfter    time.Duration
	HasRetryAfter bool
	// Meta is the unified metadata of the response that carried the error.
	Meta Meta
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api %d %s: %s", e.Status, e.Code, e.Message)
}

// DomainCode returns detail.domainCode ("" when absent).
func (e *APIError) DomainCode() string { return detailString(e.Detail, "domainCode") }

// DomainRetryAction returns detail.domainRetryAction ("" when absent).
func (e *APIError) DomainRetryAction() string { return detailString(e.Detail, "domainRetryAction") }

// DomainStatus returns detail.domainStatus (0 when absent).
func (e *APIError) DomainStatus() int {
	v, ok := e.Detail["domainStatus"]
	if !ok {
		return 0
	}
	n, ok := integerOf(v)
	if !ok {
		return 0
	}
	return int(n)
}

// ClosureID returns the current closure id reported with fence errors (412 / 403 / 404
// outside_closure): detail.closureId first, then the tansr-closure-id header. "" when absent.
func (e *APIError) ClosureID() string {
	if v := detailString(e.Detail, "closureId"); hex64.MatchString(v) {
		return v
	}
	if hex64.MatchString(e.Meta.ClosureID) {
		return e.Meta.ClosureID
	}
	return ""
}

func detailString(detail map[string]any, key string) string {
	v, _ := detail[key].(string)
	return v
}

// DomainError is a family envelope passed through unwrapped by the server (today only archive-sync-v1
// has no code table). RetryAction is the family's own word ("" when the family has none).
type DomainError struct {
	Family      string
	Code        string
	Status      int
	RetryAction string
	// Body is the raw envelope object.
	Body          map[string]any
	RetryAfter    time.Duration
	HasRetryAfter bool
	Meta          Meta
}

func (e *DomainError) Error() string {
	code := e.Code
	if code == "" {
		code = "unknown_code"
	}
	return fmt.Sprintf("api %d %s %s", e.Status, e.Family, code)
}
