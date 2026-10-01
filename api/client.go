package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cpple/tansr-go/canonical"
)

const (
	// DefaultMaxResponseBytes bounds JSON / byte response bodies (2 MiB).
	DefaultMaxResponseBytes = 2 * 1024 * 1024
	// DefaultMaxEventFrameBytes bounds one SSE frame (2 MiB).
	DefaultMaxEventFrameBytes = 2 * 1024 * 1024
)

var (
	idempotencyKeyRule = regexp.MustCompile(`^[\x21-\x7e]{1,128}$`)
	lastEventIDRule    = regexp.MustCompile(`^[\x20-\x7e]{1,256}$`)
	tokenRule          = regexp.MustCompile(`^[\x21-\x7e]+$`)
	domainCodeRule     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)
	domainRetryRule    = regexp.MustCompile(`^[a-z][a-z-]{0,63}$`)
	errRedirect        = errors.New("redirects are not followed")
)

// Options configures a Client.
type Options struct {
	// BaseURL is the Serve origin only: http(s)://host[:port] without path, query or credentials.
	BaseURL string
	// HTTPClient is injected for transport control (defaults to a client that refuses redirects).
	HTTPClient *http.Client
	// Token is the bearer token; TokenFunc is consulted per request instead when set. Short-lived
	// tickets are expected — do not embed long-term application secrets in terminals.
	Token     string
	TokenFunc func(ctx context.Context) (string, error)
	// Authorize may adjust the request headers after the token was applied. At least one of Token,
	// TokenFunc or Authorize is required.
	Authorize func(ctx context.Context, h http.Header) error
	// SessionFamily → request header tansr-session-family ("" = deployment default; sdk1 | sdk2-offload-v1).
	SessionFamily string
	// EventEnvelope requests the unified event envelope on Events(): tansr-event-envelope: unified-v1
	// is sent and the echo is verified (no echo → ErrEnvelopeNotNegotiated).
	EventEnvelope bool
	// MaxResponseBytes / MaxEventFrameBytes bound bodies and frames (0 = defaults; minimum 1024).
	MaxResponseBytes   int64
	MaxEventFrameBytes int
	// OnContract observes the unified headers of every /api response (diagnostics only; the client
	// never switches logic on it).
	OnContract func(Observation)
}

// Observation is the contract seen on the latest /api response.
type Observation struct {
	ManifestRevision int
	SchemaHash       string
	Domain           string
}

// Lock is the build-time locked contract (from the generated operation table).
type Lock struct {
	ManifestRevision int
	SchemaHash       string
}

// Locked returns the manifest revision / schemaHash the client was generated from.
func Locked() Lock { return Lock{ManifestRevision: ManifestRevision, SchemaHash: ManifestSchemaHash} }

// Client is the unified /api client. Every path is instantiated from the generated operation table;
// nothing is read from response bodies to build URLs.
type Client struct {
	baseURL       string
	http          *http.Client
	token         string
	tokenFunc     func(context.Context) (string, error)
	authorize     func(context.Context, http.Header) error
	sessionFamily string
	eventEnvelope bool
	maxResponse   int64
	maxFrame      int
	onContract    func(Observation)

	mu       sync.Mutex
	observed *Observation
}

// New validates the options and returns a Client.
func New(opts Options) (*Client, error) {
	origin, err := originOf(opts.BaseURL)
	if err != nil {
		return nil, err
	}
	if opts.Token == "" && opts.TokenFunc == nil && opts.Authorize == nil {
		return nil, newClientError(CodeInvalidOptions, "either Token, TokenFunc or Authorize is required")
	}
	if opts.Token != "" && !tokenRule.MatchString(opts.Token) {
		return nil, newClientError(CodeInvalidOptions, "Token must be a non-empty printable ASCII string")
	}
	switch opts.SessionFamily {
	case "", "sdk1", "sdk2-offload-v1":
	default:
		return nil, newClientError(CodeInvalidOptions, "SessionFamily must be sdk1 or sdk2-offload-v1")
	}
	if opts.MaxResponseBytes != 0 && opts.MaxResponseBytes < 1024 {
		return nil, newClientError(CodeInvalidOptions, "MaxResponseBytes must be ≥ 1024")
	}
	if opts.MaxEventFrameBytes != 0 && opts.MaxEventFrameBytes < 1024 {
		return nil, newClientError(CodeInvalidOptions, "MaxEventFrameBytes must be ≥ 1024")
	}
	c := &Client{
		baseURL:       origin,
		http:          opts.HTTPClient,
		token:         opts.Token,
		tokenFunc:     opts.TokenFunc,
		authorize:     opts.Authorize,
		sessionFamily: opts.SessionFamily,
		eventEnvelope: opts.EventEnvelope,
		maxResponse:   opts.MaxResponseBytes,
		maxFrame:      opts.MaxEventFrameBytes,
		onContract:    opts.OnContract,
	}
	if c.http == nil {
		c.http = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return errRedirect }}
	}
	if c.maxResponse == 0 {
		c.maxResponse = DefaultMaxResponseBytes
	}
	if c.maxFrame == 0 {
		c.maxFrame = DefaultMaxEventFrameBytes
	}
	return c, nil
}

func originOf(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", newClientError(CodeInvalidOptions, "BaseURL is not a URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil || u.Opaque != "" {
		return "", newClientError(CodeInvalidOptions, "BaseURL must be a bare http(s) origin")
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

// BaseURL returns the configured origin.
func (c *Client) BaseURL() string { return c.baseURL }

// SessionFamily returns the configured session family ("" = deployment default).
func (c *Client) SessionFamily() string { return c.sessionFamily }

// EventEnvelope reports whether Events() negotiates the unified envelope.
func (c *Client) EventEnvelope() bool { return c.eventEnvelope }

// Observed returns the contract seen on the most recent /api response.
func (c *Client) Observed() (Observation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.observed == nil {
		return Observation{}, false
	}
	return *c.observed, true
}

func (c *Client) observe(meta Meta) {
	obs := Observation{ManifestRevision: meta.ManifestRevision, SchemaHash: meta.SchemaHash, Domain: meta.Domain}
	c.mu.Lock()
	c.observed = &obs
	c.mu.Unlock()
	if c.onContract != nil {
		c.onContract(obs)
	}
}

func (c *Client) headers(ctx context.Context, accept string) (http.Header, error) {
	h := http.Header{}
	h.Set("Accept", accept)
	token := c.token
	if c.tokenFunc != nil {
		t, err := c.tokenFunc(ctx)
		if err != nil {
			return nil, wrapClientError(CodeInvalidOptions, "TokenFunc failed", err)
		}
		token = t
	}
	if token != "" {
		if !tokenRule.MatchString(token) {
			return nil, newClientError(CodeInvalidOptions, "token must be a non-empty printable ASCII string")
		}
		h.Set("Authorization", "Bearer "+token)
	}
	if c.sessionFamily != "" {
		h.Set(HeaderSessionFamily, c.sessionFamily)
	}
	if c.authorize != nil {
		if err := c.authorize(ctx, h); err != nil {
			return nil, wrapClientError(CodeInvalidOptions, "Authorize failed", err)
		}
	}
	return h, nil
}

// CallOptions are the per-call inputs of Call.
type CallOptions struct {
	// Params fill the path placeholders of the operation template.
	Params map[string]string
	// Query holds whitelisted query keys (manifest query[]); revision defaults are injected per family.
	Query map[string]string
	// Body: nil = no body; []byte = application/octet-stream; json.RawMessage = sent as is (checked
	// with canonical.ParseStrict for canonical families); any other value is encoded — canonical
	// JSON for strict families, encoding/json otherwise.
	Body any
	// ClosureID → tansr-closure-id (session-scoped write operations only; mismatch → 412 precondition_failed).
	ClosureID string
	// IdempotencyKey → Idempotency-Key (write operations only; printable ASCII 1–128).
	IdempotencyKey string
	// MaxResponseBytes overrides the client default for this call.
	MaxResponseBytes int64
}

// Result is the outcome of a successful Call. Status 202 means accepted, never completed (manual §16.6 item 5).
type Result struct {
	Status int
	// Body is the raw body (JSON or octet-stream bytes; empty for 204).
	Body        []byte
	ContentType string
	Meta        Meta
}

// Decode unmarshals a JSON body into v.
func (r *Result) Decode(v any) error {
	if r.ContentType != "application/json" {
		return newClientError(CodeInvalidResponse, "body is not application/json")
	}
	return json.Unmarshal(r.Body, v)
}

// Response pairs a validated typed body with its metadata.
type Response[T any] struct {
	Status int
	Body   T
	Meta   Meta
}

func (c *Client) operation(name string) (*Operation, error) {
	op, ok := Lookup(name)
	if !ok {
		return nil, newClientError(CodeInvalidOperation, "unknown operation "+strconv.Quote(name))
	}
	return op, nil
}

func encodeBody(op *Operation, body any, maxBytes int64) ([]byte, string, error) {
	switch b := body.(type) {
	case nil:
		return nil, "", nil
	case []byte:
		return append([]byte(nil), b...), "application/octet-stream", nil
	case json.RawMessage:
		if op.UsesCanonicalBody() {
			if _, err := canonical.ParseStrict(b, canonical.Options{MaxBytes: int(maxBytes)}); err != nil {
				return nil, "", wrapClientError(CodeInvalidBody, "raw body is not canonical JSON", err)
			}
		} else if !json.Valid(b) {
			return nil, "", newClientError(CodeInvalidBody, "raw body is not valid JSON")
		}
		return append([]byte(nil), b...), "application/json", nil
	}
	if op.UsesCanonicalBody() {
		encoded, err := canonical.Encode(body, canonical.Options{MaxBytes: int(maxBytes)})
		if err != nil {
			var cerr *canonical.Error
			detail := "canonical encoding rejected the body"
			if errors.As(err, &cerr) {
				detail += " (" + string(cerr.Code) + ")"
			}
			return nil, "", wrapClientError(CodeInvalidBody, detail, err)
		}
		return encoded, "application/json", nil
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, "", wrapClientError(CodeInvalidBody, "body is not serialisable", err)
	}
	return encoded, "application/json", nil
}

// Call performs one non-stream operation. Local validation failures return *ClientError before any
// request is sent; responses without the unified headers return *ContractUnavailableError; error
// envelopes return *APIError (unified) or *DomainError (unwrapped family envelope).
func (c *Client) Call(ctx context.Context, operation string, opts CallOptions) (*Result, error) {
	op, err := c.operation(operation)
	if err != nil {
		return nil, err
	}
	if op.Kind == "stream" {
		return nil, newClientError(CodeInvalidOperation, operation+" is an SSE stream; use Events()")
	}
	path, err := InstantiatePath(op, opts.Params)
	if err != nil {
		return nil, err
	}
	query, err := BuildQuery(op, opts.Query)
	if err != nil {
		return nil, err
	}
	maximum := c.maxResponse
	if opts.MaxResponseBytes != 0 {
		if opts.MaxResponseBytes < 1 {
			return nil, newClientError(CodeInvalidOptions, "MaxResponseBytes must be positive")
		}
		maximum = opts.MaxResponseBytes
	}
	headers, err := c.headers(ctx, "application/json, application/octet-stream")
	if err != nil {
		return nil, err
	}
	if opts.ClosureID != "" {
		if !hex64.MatchString(opts.ClosureID) {
			return nil, newClientError(CodeInvalidClosureID, "ClosureID must be 64 lowercase hex characters")
		}
		if op.Kind != "write" || !strings.HasPrefix(op.Path, "/api/sessions/:id/") {
			return nil, newClientError(CodeClosureIDNotApplicable, operation+": tansr-closure-id applies to session-scoped write operations only")
		}
		headers.Set(HeaderClosureID, opts.ClosureID)
	}
	if opts.IdempotencyKey != "" {
		if !idempotencyKeyRule.MatchString(opts.IdempotencyKey) {
			return nil, newClientError(CodeInvalidIdempotencyKey, "Idempotency-Key must be 1–128 printable ASCII characters")
		}
		if op.Kind != "write" {
			return nil, newClientError(CodeInvalidIdempotencyKey, operation+": Idempotency-Key applies to write operations only")
		}
		headers.Set("Idempotency-Key", opts.IdempotencyKey)
	}
	body, contentType, err := encodeBody(op, opts.Body, c.maxResponse)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		headers.Set("Content-Type", contentType)
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, op.Method, c.baseURL+path+query, reader)
	if err != nil {
		return nil, wrapClientError(CodeInvalidOptions, "request could not be built", err)
	}
	req.Header = headers
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	resp, err := c.send(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	meta, err := readUnifiedHeaders(resp)
	if err != nil {
		return nil, err
	}
	c.observe(meta)
	if resp.StatusCode >= 400 {
		return nil, c.errorResponse(ctx, resp, meta, op)
	}
	if resp.StatusCode == 204 {
		return &Result{Status: 204, Meta: meta}, nil
	}
	ct := contentTypeOf(resp.Header)
	if ct != "application/json" && ct != "application/octet-stream" {
		return nil, &ContractUnavailableError{Reason: ReasonNonJSONBody, Status: resp.StatusCode, ContentType: ct, Contract: meta.Contract}
	}
	data, err := readBody(ctx, resp, maximum)
	if err != nil {
		return nil, err
	}
	if ct == "application/json" && len(data) > 0 {
		if _, err := DecodeJSON(data); err != nil {
			return nil, &ContractUnavailableError{Reason: ReasonInvalidJSON, Status: resp.StatusCode, ContentType: ct, Contract: meta.Contract}
		}
	}
	return &Result{Status: resp.StatusCode, Body: data, ContentType: ct, Meta: meta}, nil
}

func (c *Client) send(ctx context.Context, req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, wrapClientError(CodeAborted, "", ctx.Err())
		}
		if errors.Is(err, errRedirect) {
			return nil, wrapClientError(CodeNetworkError, errRedirect.Error(), err)
		}
		return nil, wrapClientError(CodeNetworkError, "", err)
	}
	return resp, nil
}

func readBody(ctx context.Context, resp *http.Response, maximum int64) ([]byte, error) {
	if resp.ContentLength > maximum {
		return nil, newClientError(CodePayloadTooLarge, "")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maximum+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, wrapClientError(CodeAborted, "", ctx.Err())
		}
		return nil, wrapClientError(CodeNetworkError, "", err)
	}
	if int64(len(data)) > maximum {
		return nil, newClientError(CodePayloadTooLarge, "")
	}
	if resp.ContentLength >= 0 && resp.ContentLength != int64(len(data)) {
		return nil, newClientError(CodeInvalidResponse, "content-length mismatch")
	}
	return data, nil
}

// errorResponse decodes an error body: unified envelope → *APIError, unwrapped family envelope →
// *DomainError, anything else → *ContractUnavailableError. Unknown codes / actions are never guessed.
func (c *Client) errorResponse(ctx context.Context, resp *http.Response, meta Meta, op *Operation) error {
	ct := contentTypeOf(resp.Header)
	if ct != "application/json" {
		return &ContractUnavailableError{Reason: ReasonNonJSONBody, Status: resp.StatusCode, ContentType: ct, Contract: meta.Contract}
	}
	data, err := readBody(ctx, resp, c.maxResponse)
	if err != nil {
		return err
	}
	value, err := DecodeJSON(data)
	if err != nil {
		return &ContractUnavailableError{Reason: ReasonInvalidJSON, Status: resp.StatusCode, ContentType: ct, Contract: meta.Contract}
	}
	return decodeErrorBody(resp.StatusCode, meta, value, data, op, ct)
}

func decodeErrorBody(status int, meta Meta, value any, data []byte, op *Operation, contentType string) error {
	body, ok := objectOf(value)
	if !ok {
		return &ContractUnavailableError{Reason: ReasonInvalidErrorBody, Status: status, ContentType: contentType, Contract: meta.Contract}
	}
	if body["contract"] == Contract {
		if err := Validate(DefUnifiedError, value); err != nil {
			return wrapClientError(CodeInvalidResponse, "unified error envelope is malformed: "+err.Error(), err)
		}
		var envelope ErrorEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return wrapClientError(CodeInvalidResponse, "unified error envelope does not decode", err)
		}
		if envelope.Status != status {
			return newClientError(CodeInvalidResponse, "unified envelope status differs from http status")
		}
		apiErr := &APIError{
			Code: envelope.Code, Status: status, RetryAction: envelope.RetryAction, TraceID: envelope.TraceID,
			RequestID: envelope.RequestID, Message: envelope.Message, Detail: envelope.Detail, Meta: meta,
			RetryAfter: meta.RetryAfter, HasRetryAfter: meta.HasRetryAfter,
		}
		if envelope.RetryAfterMs != nil {
			apiErr.RetryAfter, apiErr.HasRetryAfter = time.Duration(*envelope.RetryAfterMs)*time.Millisecond, true
		}
		return apiErr
	}
	marker, _ := body["protocol"].(string)
	if marker == "" {
		marker, _ = body["contract"].(string)
	}
	family := op.Family
	if marker != "" && contains(Families, marker) {
		family = marker
	}
	if family == "" {
		return &ContractUnavailableError{Reason: ReasonInvalidErrorBody, Status: status, ContentType: contentType, Contract: meta.Contract}
	}
	derr := &DomainError{Family: family, Status: status, Body: body, Meta: meta, RetryAfter: meta.RetryAfter, HasRetryAfter: meta.HasRetryAfter}
	code, _ := body["code"].(string)
	if code == "" {
		if nested, ok := objectOf(body["error"]); ok {
			code, _ = nested["code"].(string)
		}
	}
	if domainCodeRule.MatchString(code) {
		derr.Code = code
	}
	if action, _ := body["retryAction"].(string); domainRetryRule.MatchString(action) {
		derr.RetryAction = action
	}
	if ms, ok := integerOf(body["retryAfterMs"]); ok && ms >= 0 {
		derr.RetryAfter, derr.HasRetryAfter = time.Duration(ms)*time.Millisecond, true
	}
	return derr
}

// Manifest performs discovery.manifest and validates the body against schema Manifest and the
// unified headers (revision and schemaHash must agree).
func (c *Client) Manifest(ctx context.Context) (*Response[Manifest], error) {
	res, err := c.Call(ctx, OpDiscoveryManifest, CallOptions{})
	if err != nil {
		return nil, err
	}
	value, err := DecodeJSON(res.Body)
	if err != nil {
		return nil, wrapClientError(CodeInvalidResponse, "manifest body is not JSON", err)
	}
	if err := Validate(DefManifest, value); err != nil {
		return nil, wrapClientError(CodeInvalidResponse, "manifest body is malformed: "+err.Error(), err)
	}
	var body Manifest
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return nil, wrapClientError(CodeInvalidResponse, "manifest body does not decode", err)
	}
	if body.Revision != res.Meta.ManifestRevision || "sha256:"+body.SchemaHash != res.Meta.SchemaHash || body.Runtime == nil {
		return nil, newClientError(CodeInvalidResponse, "manifest body does not match the unified headers")
	}
	return &Response[Manifest]{Status: res.Status, Body: body, Meta: res.Meta}, nil
}

// Capabilities performs discovery.capabilities and validates the body against schema Capabilities.
func (c *Client) Capabilities(ctx context.Context) (*Response[Capabilities], error) {
	res, err := c.Call(ctx, OpDiscoveryCapabilities, CallOptions{})
	if err != nil {
		return nil, err
	}
	value, err := DecodeJSON(res.Body)
	if err != nil {
		return nil, wrapClientError(CodeInvalidResponse, "capabilities body is not JSON", err)
	}
	if err := Validate(DefCapabilities, value); err != nil {
		return nil, wrapClientError(CodeInvalidResponse, "capabilities body is malformed: "+err.Error(), err)
	}
	var body Capabilities
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return nil, wrapClientError(CodeInvalidResponse, "capabilities body does not decode", err)
	}
	if body.ManifestRevision != res.Meta.ManifestRevision || body.SchemaHash != res.Meta.SchemaHash {
		return nil, newClientError(CodeInvalidResponse, "capabilities body does not match the unified headers")
	}
	return &Response[Capabilities]{Status: res.Status, Body: body, Meta: res.Meta}, nil
}

// ClosureResult is the session-level capability closure with the header closure id.
type ClosureResult struct {
	Closure CapabilityClosure
	// ClosureID is the tansr-closure-id header, verified equal to Closure.ClosureID.
	ClosureID string
	Meta      Meta
}

// SessionCapabilities performs discovery.session.capabilities for sessionID and verifies that the
// tansr-closure-id header equals the body closureId.
func (c *Client) SessionCapabilities(ctx context.Context, sessionID string) (*ClosureResult, error) {
	res, err := c.Call(ctx, OpDiscoverySessionCapabilities, CallOptions{Params: map[string]string{"id": sessionID}})
	if err != nil {
		return nil, err
	}
	value, err := DecodeJSON(res.Body)
	if err != nil {
		return nil, wrapClientError(CodeInvalidResponse, "closure body is not JSON", err)
	}
	if err := Validate(DefCapabilityClosure, value); err != nil {
		return nil, wrapClientError(CodeInvalidResponse, "capability closure body is malformed: "+err.Error(), err)
	}
	var body CapabilityClosure
	if err := json.Unmarshal(res.Body, &body); err != nil {
		return nil, wrapClientError(CodeInvalidResponse, "closure body does not decode", err)
	}
	if res.Meta.ClosureID == "" || res.Meta.ClosureID != body.ClosureID {
		return nil, newClientError(CodeInvalidResponse, "tansr-closure-id header differs from body closureId")
	}
	return &ClosureResult{Closure: body, ClosureID: res.Meta.ClosureID, Meta: res.Meta}, nil
}

// String identifies the client for diagnostics (origin and locked manifest revision; no token).
func (c *Client) String() string {
	return fmt.Sprintf("tansr api client %s (manifest revision %d)", c.baseURL, ManifestRevision)
}
