package api

import (
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Contract is the unified contract marker (tansr-contract header, body contract fields).
const Contract = "unified-v1"

// Header names (lowercase; HTTP header names are case-insensitive).
const (
	HeaderContract         = "tansr-contract"
	HeaderManifestRevision = "tansr-manifest-revision"
	HeaderDomain           = "tansr-domain"
	HeaderSchemaHash       = "tansr-schema-hash"
	HeaderClosureID        = "tansr-closure-id"
	HeaderEventEnvelope    = "tansr-event-envelope"
	HeaderSessionFamily    = "tansr-session-family"
	HeaderTraceID          = "x-request-id"
	HeaderIdempotencyKey   = "idempotency-key"
	HeaderIfMatch          = "if-match"
	HeaderETag             = "etag"
	HeaderDeadline         = "deadline"
	HeaderRetryAfter       = "retry-after"
	HeaderLastEventID      = "last-event-id"
)

var (
	// strongETagRule is the only ETag / If-Match form the manifest r7 three heads accept: the resource's
	// revision as a quoted decimal. Weak validators (W/"…") and wildcards (*) are not a tansr form.
	strongETagRule = regexp.MustCompile(`^"(0|[1-9][0-9]{0,18})"$`)
	bareRevision   = regexp.MustCompile(`^(0|[1-9][0-9]{0,18})$`)
)

// NormalizeIfMatch accepts the strong form `"<revision>"` or a bare decimal revision (quoted on the
// wire) and reports false for anything else. Mirrors Node @tansr/api-client `./api` normalizeIfMatch.
func NormalizeIfMatch(value string) (string, bool) {
	text := strings.TrimSpace(value)
	if strongETagRule.MatchString(text) {
		return text, true
	}
	if bareRevision.MatchString(text) {
		return `"` + text + `"`, true
	}
	return "", false
}

// FormatDeadline renders the `deadline` request header: RFC 3339 in UTC (the only form Serve accepts).
func FormatDeadline(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// Meta is the decoded projection of the unified response headers (schema ResponseHeaders).
type Meta struct {
	// Contract is always "unified-v1" once the headers were accepted.
	Contract string
	// ManifestRevision is tansr-manifest-revision. It is compared with ManifestRevision for
	// diagnostics only; the client never switches logic on it.
	ManifestRevision int
	// SchemaHash is tansr-schema-hash: "sha256:<hex>" or "none".
	SchemaHash string
	// Domain is tansr-domain (accepting domain, passed through verbatim).
	Domain string
	// ClosureID is tansr-closure-id ("" when absent).
	ClosureID string
	// EventEnvelope is true when the server echoed tansr-event-envelope: unified-v1.
	EventEnvelope bool
	// TraceID is the echoed x-request-id ("" when absent or malformed).
	TraceID string
	// RetryAfter is the decoded retry-after header; HasRetryAfter is false when absent/unparsable.
	RetryAfter    time.Duration
	HasRetryAfter bool
	// ETag is the strong validator `"<revision>"` of a versioned resource (manifest etagPath); "" when
	// absent or not in the strong form. Pass it back verbatim as CallOptions.IfMatch.
	ETag string
}

var (
	revisionPattern   = regexp.MustCompile(`^[1-9][0-9]{0,9}$`)
	domainPattern     = regexp.MustCompile(`^[a-z][a-z-]{0,63}$`)
	schemaHashPattern = regexp.MustCompile(`^(sha256:[0-9a-f]{64}|none)$`)
	traceIDPattern    = regexp.MustCompile(`^[\x21-\x7e]{1,256}$`)
	retryAfterSeconds = regexp.MustCompile(`^\d+(\.\d+)?$`)
)

// ParseRetryAfter decodes a retry-after header (seconds, fractional seconds or HTTP-date).
func ParseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, false
	}
	if retryAfterSeconds.MatchString(text) {
		seconds, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsInf(seconds, 0) || seconds > 1e9 {
			return 0, false
		}
		return time.Duration(math.Round(seconds*1000)) * time.Millisecond, true
	}
	at, err := http.ParseTime(text)
	if err != nil {
		return 0, false
	}
	wait := at.Sub(now)
	if wait < 0 {
		wait = 0
	}
	return wait, true
}

// readUnifiedHeaders decodes the four unified headers (+ closure / envelope / trace / retry-after).
// Missing or malformed headers yield *ContractUnavailableError; HTML pages, proxies and legacy serve
// all land here and are never mistaken for a legacy API.
func readUnifiedHeaders(resp *http.Response) (Meta, error) {
	contentType := contentTypeOf(resp.Header)
	contract := resp.Header.Get(HeaderContract)
	fail := func(reason ContractUnavailableReason) (Meta, error) {
		return Meta{}, &ContractUnavailableError{Reason: reason, Status: resp.StatusCode, ContentType: contentType, Contract: contract}
	}
	if len(resp.Header.Values(HeaderContract)) == 0 {
		return fail(ReasonMissingContractHeader)
	}
	if contract != Contract {
		return fail(ReasonContractMismatch)
	}
	revision := resp.Header.Get(HeaderManifestRevision)
	domain := resp.Header.Get(HeaderDomain)
	schemaHash := resp.Header.Get(HeaderSchemaHash)
	if !revisionPattern.MatchString(revision) || !domainPattern.MatchString(domain) || !schemaHashPattern.MatchString(schemaHash) {
		return fail(ReasonInvalidContractHeaders)
	}
	closureID := resp.Header.Get(HeaderClosureID)
	if closureID != "" && !hex64.MatchString(closureID) {
		return fail(ReasonInvalidContractHeaders)
	}
	echoed := resp.Header.Values(HeaderEventEnvelope)
	if len(echoed) > 0 && (len(echoed) != 1 || echoed[0] != Contract) {
		return fail(ReasonInvalidContractHeaders)
	}
	meta := Meta{Contract: Contract, SchemaHash: schemaHash, Domain: domain, ClosureID: closureID, EventEnvelope: len(echoed) == 1}
	meta.ManifestRevision, _ = strconv.Atoi(revision)
	if trace := resp.Header.Get(HeaderTraceID); traceIDPattern.MatchString(trace) {
		meta.TraceID = trace
	}
	meta.RetryAfter, meta.HasRetryAfter = ParseRetryAfter(resp.Header.Get(HeaderRetryAfter), time.Now())
	if etag := strings.TrimSpace(resp.Header.Get(HeaderETag)); strongETagRule.MatchString(etag) {
		meta.ETag = etag
	}
	return meta, nil
}

func contentTypeOf(h http.Header) string {
	value := h.Get("content-type")
	if i := strings.IndexByte(value, ';'); i >= 0 {
		value = value[:i]
	}
	return strings.ToLower(strings.TrimSpace(value))
}
