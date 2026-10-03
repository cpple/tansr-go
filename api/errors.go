package api

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// ErrorCode is the unified 19-code vocabulary (schema UnifiedCode; RFC-UAPI-1 §2.2). Plan D19
// (0.14.0 batch): it is the primary field of every server error the SDK exposes — a `switch err.Code`
// written against this type has the same shape in Node, C#, Android, iOS and HarmonyOS. Family codes
// are preserved as a secondary, machine-readable position in Detail.DomainCode and never replace Code.
type ErrorCode string

// Unified codes. CodePayloadTooLarge is deliberately an untyped constant: the same word is also the
// local ClientErrorCode for an oversized response body, so one constant compares against both types.
const (
	CodeInvalidRequest        ErrorCode = "invalid_request"
	CodeProtocolMismatch      ErrorCode = "protocol_mismatch"
	CodeUnauthorized          ErrorCode = "unauthorized"
	CodeForbidden             ErrorCode = "forbidden"
	CodeNotFound              ErrorCode = "not_found"
	CodeMethodNotAllowed      ErrorCode = "method_not_allowed"
	CodeGone                  ErrorCode = "gone"
	CodeConflict              ErrorCode = "conflict"
	CodeStaleGeneration       ErrorCode = "stale_generation"
	CodeGap                   ErrorCode = "gap"
	CodeCapabilityUnavailable ErrorCode = "capability_unavailable"
	CodeCapacityExceeded      ErrorCode = "capacity_exceeded"
	CodePayloadTooLarge                 = "payload_too_large"
	CodeUpstreamUnavailable   ErrorCode = "upstream_unavailable"
	CodeResultUnknown         ErrorCode = "result_unknown"
	CodeRejected              ErrorCode = "rejected"
	CodeInternalError         ErrorCode = "internal_error"
	CodePreconditionFailed    ErrorCode = "precondition_failed"
	CodeNotCanonical          ErrorCode = "not_canonical"
)

// ErrorCodes is the 19-code unified vocabulary in schema order (schema UnifiedCode).
var ErrorCodes = []string{
	"invalid_request", "protocol_mismatch", "unauthorized", "forbidden", "not_found", "method_not_allowed",
	"gone", "conflict", "stale_generation", "gap", "capability_unavailable", "capacity_exceeded",
	"payload_too_large", "upstream_unavailable", "result_unknown", "rejected", "internal_error",
	"precondition_failed", "not_canonical",
}

// FacadeErrorCodes is the 8-code facade subset (schema FacadeCode). Request-header rejections of the
// /api three-header wiring are not facade codes: they carry requestId / detail.domainCode and validate
// as UnifiedError (revision 7).
var FacadeErrorCodes = []string{
	"unauthorized", "not_found", "method_not_allowed", "invalid_request", "capability_unavailable",
	"capacity_exceeded", "precondition_failed", "upstream_unavailable",
}

// RetryAction is the unified retryAction vocabulary (schema RetryAction; RFC-UAPI-1 §2.3). Only
// ActionSameRequest is ever executed by this package (RetrySameRequest); the others are advice.
type RetryAction string

// Unified retry actions.
const (
	ActionNone        RetryAction = "none"
	ActionSameRequest RetryAction = "same-request"
	ActionQueryStatus RetryAction = "query-status"
	ActionRebind      RetryAction = "rebind"
	ActionRefresh     RetryAction = "refresh"
	ActionRediscover  RetryAction = "rediscover"
)

// RetryActions is the unified retryAction vocabulary in schema order.
var RetryActions = []string{"none", "same-request", "query-status", "rebind", "refresh", "rediscover"}

// DomainRetryActions is the union of the per-family retryAction words kept in detail.domainRetryAction.
var DomainRetryActions = []string{"none", "query-status", "rebind", "same-request", "refresh-projection", "backoff", "discover", "reconcile", "refresh"}

// Request-header reasons (schema UnifiedErrorDetail.reason, revision 7; RFC-UAPI-1 §1.2, plan D27: no
// new codes — the unified code decides the handling, Detail.Reason only names the cause).
const (
	ReasonNotInstalled                = "not_installed"
	ReasonOutsideClosure              = "outside_closure"
	ReasonIdempotencyKeyInvalid       = "idempotency_key_invalid"
	ReasonIdempotencyKeyNotApplicable = "idempotency_key_not_applicable"
	ReasonIdempotencyKeyReused        = "idempotency_key_reused"
	ReasonIdempotencyKeyMismatch      = "idempotency_key_mismatch"
	ReasonReceiptNotRetained          = "receipt_not_retained"
	ReasonReceiptWindowFull           = "receipt_window_full"
	ReasonIfMatchInvalid              = "if_match_invalid"
	ReasonIfMatchNotApplicable        = "if_match_not_applicable"
	ReasonIfMatchBodyMismatch         = "if_match_body_mismatch"
	ReasonIfMatchStale                = "if_match_stale"
	ReasonDeadlineInvalid             = "deadline_invalid"
	ReasonDeadlineExceeded            = "deadline_exceeded"
	ReasonHeaderBodyLimit             = "header_body_limit"
	ReasonHeaderBodyNotCanonical      = "header_body_not_canonical"
	ReasonHeaderProcessingFailed      = "header_processing_failed"
)

// ClientErrorCode enumerates local failures (options, placeholders, query keys, request headers,
// envelope negotiation, network, cancellation). No server wire code is added here; the vocabulary
// mirrors the Node client (@tansr/api-client/api ApiClientErrorCode).
type ClientErrorCode string

const (
	CodeInvalidOptions         ClientErrorCode = "invalid_options"
	CodeInvalidOperation       ClientErrorCode = "invalid_operation"
	CodeInvalidParams          ClientErrorCode = "invalid_params"
	CodeInvalidQuery           ClientErrorCode = "invalid_query"
	CodeInvalidBody            ClientErrorCode = "invalid_body"
	CodeInvalidClosureID       ClientErrorCode = "invalid_closure_id"
	CodeClosureIDNotApplicable ClientErrorCode = "closure_id_not_applicable"
	CodeInvalidIdempotencyKey  ClientErrorCode = "invalid_idempotency_key"
	// CodeInvalidIfMatch: IfMatch is neither `"<revision>"` nor a bare decimal revision.
	CodeInvalidIfMatch ClientErrorCode = "invalid_if_match"
	// CodeIfMatchNotApplicable: IfMatch on a non-write operation or on one whose manifest entry has no
	// expectedRevision position.
	CodeIfMatchNotApplicable ClientErrorCode = "if_match_not_applicable"
	// CodeInvalidDeadline: Deadline is not a representable RFC 3339 instant.
	CodeInvalidDeadline ClientErrorCode = "invalid_deadline"
	// CodeDeadlineExceeded: the deadline had passed before sending (or before replaying) — the request
	// is not sent, not replayed and the deadline is never extended.
	CodeDeadlineExceeded        ClientErrorCode = "deadline_exceeded"
	CodeInvalidLastEventID      ClientErrorCode = "invalid_last_event_id"
	CodeAborted                 ClientErrorCode = "aborted"
	CodeNetworkError            ClientErrorCode = "network_error"
	CodeInvalidResponse         ClientErrorCode = "invalid_response"
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

// ErrDeadlineExceeded is matched by errors.Is when a Deadline had already passed before the request
// was sent or replayed.
var ErrDeadlineExceeded = &ClientError{Code: CodeDeadlineExceeded}

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

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ErrorDetail is the typed view of the open `detail` object of a unified error envelope (schema
// UnifiedErrorDetail ∪ FacadeErrorDetail). Every field is the zero value when the key is absent; Raw
// holds the complete object (nil when the envelope carried no detail) so that keys this version does
// not know remain readable. Under plan D19 the family position (DomainCode / DomainStatus /
// DomainRetryAction) is secondary: branch on APIError.Code first, read DomainCode only when a
// family-level distinction is needed.
type ErrorDetail struct {
	// Domain / Family identify the accepting domain and contract family of a wrapped family error.
	Domain string
	Family string
	// DomainCode is the family's own code (RFC-UAPI-1 §2.2), e.g. closure_stale, session_not_found,
	// revision_conflict, request_id_conflict.
	DomainCode string
	// DomainStatus is the family's wire status (0 when absent).
	DomainStatus int
	// DomainRetryAction is the family's own retry word (DomainRetryActions; "" when absent or null).
	DomainRetryAction string
	// Fallback is the cache family's fallback word (none | legacy-cold); it never enters the unified layer.
	Fallback string
	// Reason is the machine-readable cause: not_installed / outside_closure for fence and assembly,
	// the Reason* constants for request-header rejections.
	Reason string
	// Header names the request header that triggered the rejection (idempotency-key, if-match,
	// deadline, tansr-closure-id, …).
	Header string
	// LimitBytes is the body cap of a 413 header_body_limit rejection (0 when absent).
	LimitBytes int64
	// ClosureID / Operation / State are the fence positions of 412 / 403 / 404 outside_closure errors.
	ClosureID string
	Operation string
	State     string
	// Raw is the detail object as received (nil when absent).
	Raw map[string]any
}

// Present reports whether the envelope carried a detail object.
func (d ErrorDetail) Present() bool { return d.Raw != nil }

// detailFrom projects a validated detail object onto ErrorDetail.
func detailFrom(raw map[string]any) ErrorDetail {
	if raw == nil {
		return ErrorDetail{}
	}
	d := ErrorDetail{Raw: raw}
	d.Domain = detailString(raw, "domain")
	d.Family = detailString(raw, "family")
	d.DomainCode = detailString(raw, "domainCode")
	d.DomainRetryAction = detailString(raw, "domainRetryAction")
	d.Fallback = detailString(raw, "fallback")
	d.Reason = detailString(raw, "reason")
	d.Header = detailString(raw, "header")
	d.ClosureID = detailString(raw, "closureId")
	d.Operation = detailString(raw, "operation")
	d.State = detailString(raw, "state")
	if n, ok := integerOf(raw["domainStatus"]); ok {
		d.DomainStatus = int(n)
	}
	if n, ok := integerOf(raw["limitBytes"]); ok {
		d.LimitBytes = n
	}
	return d
}

func detailString(detail map[string]any, key string) string {
	v, _ := detail[key].(string)
	return v
}

// APIError is a unified error envelope `{contract:'unified-v1', traceId, requestId, code, status,
// retryAction, retryAfterMs?, message, detail?}` (schema FacadeError / UnifiedError). It is the single
// public shape of every server-side rejection that reached the client through the unified contract:
// facade-owned errors, wrapped family errors and request-header rejections alike (plan D19, unified
// code first). Code and RetryAction decide the handling; Detail keeps the family position.
type APIError struct {
	// Code is the unified code (ErrorCode, 19 words).
	Code   ErrorCode
	Status int
	// RetryAction is the unified action stated by the server.
	RetryAction RetryAction
	// TraceID echoes x-request-id (observability only; unrelated to the idempotency key).
	TraceID string
	// RequestID is the client idempotency key as seen by the server (Idempotency-Key or the body
	// requestId position); nil for facade-owned errors and families without a key position.
	RequestID *string
	// Message is the server message (≤ 1024 characters).
	Message string
	// Detail is the typed detail view (Detail.Present() false when the envelope had none).
	Detail ErrorDetail
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

// Is lets errors.Is(err, &APIError{Code: CodeNotFound}) match on the unified code alone: the target
// must carry only a Code (every other field zero), mirroring ClientError.Is.
func (e *APIError) Is(target error) bool {
	t, ok := target.(*APIError)
	if !ok || t.Code != e.Code || t.Status != 0 || t.RetryAction != "" || t.TraceID != "" || t.RequestID != nil || t.Message != "" {
		return false
	}
	return !t.Detail.Present() && !t.HasRetryAfter && t.Meta == (Meta{})
}

// ClosureID returns the current closure id reported with fence errors (412 / 403 / 404
// outside_closure): Detail.ClosureID first, then the tansr-closure-id header. "" when absent.
func (e *APIError) ClosureID() string {
	if hex64.MatchString(e.Detail.ClosureID) {
		return e.Detail.ClosureID
	}
	if hex64.MatchString(e.Meta.ClosureID) {
		return e.Meta.ClosureID
	}
	return ""
}

// DomainError is a family envelope passed through unwrapped by the server. It is the residual form
// outside the unified-code model: today only archive-sync-v1 (no code table in the error matrix) reaches
// the client this way; every other family is wrapped into APIError. RetryAction is the family's own
// word ("" when the family has none); Advice maps it onto the unified vocabulary.
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
