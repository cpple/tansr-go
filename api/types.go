package api

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Types in this file have the same shape as the definitions of doc/rfc/unified-v1.schema.json. They
// are populated only after Validate accepted the decoded value, so field invariants (enums, patterns,
// presence) hold whenever a value came through the client.

// Manifest is GET /api/manifest (schema Manifest). Runtime is nil for the repository artifact.
type Manifest struct {
	Format       string                 `json:"format"`
	Contract     string                 `json:"contract"`
	Revision     int                    `json:"revision"`
	SchemaHash   string                 `json:"schemaHash"`
	Families     []ManifestFamily       `json:"families"`
	Operations   []ManifestOperation    `json:"operations"`
	Capabilities []ManifestCapability   `json:"capabilities"`
	PlatformAPI  []PlatformAPIReference `json:"platformApi"`
	Runtime      *ManifestRuntime       `json:"runtime,omitempty"`
}

// ManifestRuntime is the runtime-only section of GET /api/manifest.
type ManifestRuntime struct {
	Installed InstalledDomains `json:"installed"`
}

// InstalledDomains lists the domains assembled by this deployment (schema InstalledDomains).
type InstalledDomains struct {
	// Session is the preferred session family; nil = no session plane installed.
	Session             *string `json:"session"`
	Execution           bool    `json:"execution"`
	Archive             bool    `json:"archive"`
	ArchiveSync         bool    `json:"archive-sync"`
	Cache               bool    `json:"cache"`
	Terminal            bool    `json:"terminal"`
	TerminalObservation bool    `json:"terminal-observation"`
	TerminalProfile     bool    `json:"terminal-profile"`
}

// ManifestFamily is one contract-family registration (schema ManifestFamily).
type ManifestFamily struct {
	ID                      string            `json:"id"`
	Domains                 []string          `json:"domains"`
	Status                  string            `json:"status"`
	RFC                     string            `json:"rfc"`
	Source                  string            `json:"source"`
	SHA256                  string            `json:"sha256"`
	Golden                  map[string]string `json:"golden"`
	Generated               []string          `json:"generated"`
	LegacyEntries           []string          `json:"legacyEntries"`
	APIEntries              []string          `json:"apiEntries"`
	Clients                 []ClientLock      `json:"clients"`
	SessionManifestRevision *int              `json:"sessionManifestRevision,omitempty"`
	SessionManifestSHA256   *string           `json:"sessionManifestSha256,omitempty"`
}

// ClientLock is one client registration of a family (schema ClientLock).
type ClientLock struct {
	Client   string  `json:"client"`
	Repo     string  `json:"repo"`
	File     *string `json:"file,omitempty"`
	Lock     string  `json:"lock"`
	Observed string  `json:"observed"`
	Drift    bool    `json:"drift"`
}

// ManifestOperation is one catalogue entry as published by the server (schema ManifestOperation).
type ManifestOperation struct {
	Name              string   `json:"name"`
	Domain            string   `json:"domain"`
	Family            *string  `json:"family"`
	Method            string   `json:"method"`
	APIPath           string   `json:"apiPath"`
	Aliases           []string `json:"aliases"`
	LegacyPath        *string  `json:"legacyPath"`
	LegacyOffloadPath *string  `json:"legacyOffloadPath"`
	Kind              string   `json:"kind"`
	SSE               bool     `json:"sse"`
	Request           *string  `json:"request"`
	Response          *string  `json:"response"`
	Query             []string `json:"query"`
	Notes             *string  `json:"notes"`
}

// ManifestCapability is one derived capability (schema ManifestCapability). Value is a LimitRange
// object for kind limit and a JSON string otherwise; it is kept raw.
type ManifestCapability struct {
	ID     string          `json:"id"`
	Domain string          `json:"domain"`
	Family string          `json:"family"`
	Kind   string          `json:"kind"`
	Source string          `json:"source"`
	Value  json.RawMessage `json:"value"`
}

// LimitRange is the value of a kind=limit capability.
type LimitRange struct {
	Min     int64  `json:"min"`
	Max     int64  `json:"max"`
	Default *int64 `json:"default"`
}

// Limit decodes Value as a LimitRange (kind limit only).
func (c ManifestCapability) Limit() (LimitRange, error) {
	var r LimitRange
	if c.Kind != "limit" {
		return r, errors.New("capability is not a limit")
	}
	return r, json.Unmarshal(c.Value, &r)
}

// PlatformAPIReference points at a tansr-api capability family (schema PlatformApiReference).
type PlatformAPIReference struct {
	ID       string   `json:"id"`
	Doc      string   `json:"doc"`
	Host     string   `json:"host"`
	Entries  []string `json:"entries"`
	Vendored *string  `json:"vendored"`
}

// Capabilities is GET /api/capabilities (schema Capabilities). It carries no URL navigation fields:
// the client always uses the manifest-generated paths (RFC-SDK2-1 §10.5).
type Capabilities struct {
	Contract         string            `json:"contract"`
	ManifestRevision int               `json:"manifestRevision"`
	SchemaHash       string            `json:"schemaHash"`
	Domains          CapabilityDomains `json:"domains"`
}

// CapabilityDomains is the eight-domain deployment view.
type CapabilityDomains struct {
	Session             SessionDomainCapability `json:"session"`
	Execution           DomainCapability        `json:"execution"`
	Archive             DomainCapability        `json:"archive"`
	ArchiveSync         DomainCapability        `json:"archive-sync"`
	Cache               DomainCapability        `json:"cache"`
	Terminal            DomainCapability        `json:"terminal"`
	TerminalObservation DomainCapability        `json:"terminal-observation"`
	TerminalProfile     DomainCapability        `json:"terminal-profile"`
}

// DomainCapability is the deployment-level view of one domain (schema DomainCapability).
// Installed means "assembled here", not "you may use it".
type DomainCapability struct {
	Installed  bool   `json:"installed"`
	Contract   string `json:"contract"`
	Status     string `json:"status"`
	SchemaHash string `json:"schemaHash"`
}

// SessionDomainCapability adds the session family view (schema SessionDomainCapability).
type SessionDomainCapability struct {
	Installed  bool          `json:"installed"`
	Family     SessionFamily `json:"family"`
	Contract   string        `json:"contract"`
	Status     string        `json:"status"`
	SchemaHash string        `json:"schemaHash"`
}

// SessionFamily is {preferred, available[]} of the session domain.
type SessionFamily struct {
	// Preferred is the default family when tansr-session-family is not sent; nil when none.
	Preferred *string  `json:"preferred"`
	Available []string `json:"available"`
}

// CapabilityClosure is GET /api/sessions/:id/capabilities (schema CapabilityClosure).
type CapabilityClosure struct {
	Contract string `json:"contract"`
	// ClosureID = sha256("tansr.unified.closure.v1" ‖ 0x00 ‖ canonical({authorizationRevision, domains, operations})).
	ClosureID string `json:"closureId"`
	// AuthorizationRevision is a Sequence string or nil.
	AuthorizationRevision *string `json:"authorizationRevision"`
	// Domains maps the eight closure domains to their state.
	Domains map[string]ClosureDomainState `json:"domains"`
	// Operations maps all 76 closure operation names to enabled | disabled | unavailable.
	Operations map[string]string `json:"operations"`
}

// ClosureDomainState is one domain's fence state (schema ClosureDomainState).
type ClosureDomainState struct {
	Installed bool `json:"installed"`
	// Revision is the domain's self-reported generation or nil.
	Revision *string `json:"revision"`
}

// Operation states (schema OperationState).
const (
	StateEnabled     = "enabled"
	StateDisabled    = "disabled"
	StateUnavailable = "unavailable"
)

// ErrorEnvelope is the wire form of FacadeError / UnifiedError.
type ErrorEnvelope struct {
	Contract     string         `json:"contract"`
	TraceID      string         `json:"traceId"`
	RequestID    *string        `json:"requestId"`
	Code         string         `json:"code"`
	Status       int            `json:"status"`
	RetryAction  string         `json:"retryAction"`
	RetryAfterMs *int64         `json:"retryAfterMs,omitempty"`
	Message      string         `json:"message"`
	Detail       map[string]any `json:"detail,omitempty"`
}

// --- cursors -------------------------------------------------------------------------------------

// cursor is the shared storage of the five cursor positions. Each position is a distinct named type
// so that they cannot be assigned to one another (manual §16.6 item 6); a position advances only
// when its own contract condition is met — never on SSE EOF, HTTP 202 or connection success.
type cursor struct {
	raw json.RawMessage
}

func (c cursor) isNull() bool { return len(c.raw) == 0 || bytes.Equal(c.raw, []byte("null")) }

func (c cursor) text() (string, bool) {
	var s string
	if c.isNull() || json.Unmarshal(c.raw, &s) != nil {
		return "", false
	}
	return s, true
}

func (c cursor) rawJSON() json.RawMessage {
	if c.isNull() {
		return json.RawMessage("null")
	}
	return append(json.RawMessage(nil), c.raw...)
}

func (c *cursor) unmarshal(data []byte) error {
	c.raw = append(json.RawMessage(nil), data...)
	return nil
}

func (c cursor) marshal() ([]byte, error) { return c.rawJSON(), nil }

// EventCursor is the event-stream position (= SSE id; Last-Event-ID resumption only).
type EventCursor struct{ cursor }

// ArchiveCoverage is the archive ACK coverage (today a Coverage object on the wire).
type ArchiveCoverage struct{ cursor }

// OutputWatermark is the terminal output watermark (afterSeq).
type OutputWatermark struct{ cursor }

// MaterialConsumed marks material consumed by the core (≠ received).
type MaterialConsumed struct{ cursor }

// AckReceipt is the ACK receipt (always null in event streams today).
type AckReceipt struct{ cursor }

// IsNull reports whether the position is absent.
func (c EventCursor) IsNull() bool { return c.isNull() }

// Text returns the string form when the cursor is an opaque string.
func (c EventCursor) Text() (string, bool) { return c.text() }

// Raw returns the raw JSON of the position ("null" when absent).
func (c EventCursor) Raw() json.RawMessage { return c.rawJSON() }

// UnmarshalJSON keeps the wire bytes.
func (c *EventCursor) UnmarshalJSON(b []byte) error { return c.unmarshal(b) }

// MarshalJSON writes the wire bytes back.
func (c EventCursor) MarshalJSON() ([]byte, error) { return c.marshal() }

// IsNull reports whether the position is absent.
func (c ArchiveCoverage) IsNull() bool { return c.isNull() }

// Text returns the string form when the cursor is an opaque string.
func (c ArchiveCoverage) Text() (string, bool) { return c.text() }

// Raw returns the raw JSON of the position ("null" when absent).
func (c ArchiveCoverage) Raw() json.RawMessage { return c.rawJSON() }

// UnmarshalJSON keeps the wire bytes.
func (c *ArchiveCoverage) UnmarshalJSON(b []byte) error { return c.unmarshal(b) }

// MarshalJSON writes the wire bytes back.
func (c ArchiveCoverage) MarshalJSON() ([]byte, error) { return c.marshal() }

// IsNull reports whether the position is absent.
func (c OutputWatermark) IsNull() bool { return c.isNull() }

// Text returns the string form when the cursor is an opaque string.
func (c OutputWatermark) Text() (string, bool) { return c.text() }

// Raw returns the raw JSON of the position ("null" when absent).
func (c OutputWatermark) Raw() json.RawMessage { return c.rawJSON() }

// UnmarshalJSON keeps the wire bytes.
func (c *OutputWatermark) UnmarshalJSON(b []byte) error { return c.unmarshal(b) }

// MarshalJSON writes the wire bytes back.
func (c OutputWatermark) MarshalJSON() ([]byte, error) { return c.marshal() }

// IsNull reports whether the position is absent.
func (c MaterialConsumed) IsNull() bool { return c.isNull() }

// Text returns the string form when the cursor is an opaque string.
func (c MaterialConsumed) Text() (string, bool) { return c.text() }

// Raw returns the raw JSON of the position ("null" when absent).
func (c MaterialConsumed) Raw() json.RawMessage { return c.rawJSON() }

// UnmarshalJSON keeps the wire bytes.
func (c *MaterialConsumed) UnmarshalJSON(b []byte) error { return c.unmarshal(b) }

// MarshalJSON writes the wire bytes back.
func (c MaterialConsumed) MarshalJSON() ([]byte, error) { return c.marshal() }

// IsNull reports whether the position is absent.
func (c AckReceipt) IsNull() bool { return c.isNull() }

// Text returns the string form when the cursor is an opaque string.
func (c AckReceipt) Text() (string, bool) { return c.text() }

// Raw returns the raw JSON of the position ("null" when absent).
func (c AckReceipt) Raw() json.RawMessage { return c.rawJSON() }

// UnmarshalJSON keeps the wire bytes.
func (c *AckReceipt) UnmarshalJSON(b []byte) error { return c.unmarshal(b) }

// MarshalJSON writes the wire bytes back.
func (c AckReceipt) MarshalJSON() ([]byte, error) { return c.marshal() }

// CursorSet is the five separated positions (schema CursorSet).
type CursorSet struct {
	EventCursor      EventCursor      `json:"eventCursor"`
	ArchiveCoverage  ArchiveCoverage  `json:"archiveCoverage"`
	OutputWatermark  OutputWatermark  `json:"outputWatermark"`
	MaterialConsumed MaterialConsumed `json:"materialConsumed"`
	AckReceipt       AckReceipt       `json:"ackReceipt"`
}

// Terminal statuses (schema TerminalStatus; nil = non-terminal event).
const (
	TerminalAccepted  = "accepted"
	TerminalCompleted = "completed"
	TerminalAborted   = "aborted"
	TerminalUnknown   = "unknown"
)

// EventEnvelope is the unified event envelope (RFC-UAPI-1 §1.5; plan D18 seven-key wire form
// {contract, eventId, domain, type, cursorSet, terminalStatus, raw}). Transitional forms are
// tolerated on decode: the schema's nine-key form (seq, payload) and today's six-key server form
// (no eventId); absent positions read as nil.
type EventEnvelope struct {
	Contract string `json:"contract"`
	// EventID is the schema Id; nil when the wire did not carry it (stream position is in
	// CursorSet.EventCursor).
	EventID *string `json:"eventId"`
	// Seq is transitional (schema nine-key form); nil on the D18 wire.
	Seq *string `json:"seq,omitempty"`
	// Domain is the accepting domain (= tansr-domain).
	Domain string `json:"domain"`
	// Type is the event type; nil when the server could not classify the event (raw carries it).
	Type      *string   `json:"type"`
	CursorSet CursorSet `json:"cursorSet"`
	// TerminalStatus is nil for non-terminal events, else accepted | completed | aborted | unknown.
	// accepted only means accepted; unknown must be handled like result_unknown (query, never replay).
	TerminalStatus *string `json:"terminalStatus"`
	// Payload is transitional (schema nine-key form); nil on the D18 wire. Use PayloadOrRaw.
	Payload json.RawMessage `json:"payload,omitempty"`
	// Raw is the original data: event (any JSON value, including unknown types). Never validated.
	Raw json.RawMessage `json:"raw"`
}

// IsTerminal reports completion / abortion / unknown outcome. Only terminalStatus decides this — an
// SSE EOF, a 202 or a closed connection never does (manual §16.6 item 5).
func (e *EventEnvelope) IsTerminal() bool {
	return e.TerminalStatus != nil && *e.TerminalStatus != TerminalAccepted
}

// PayloadOrRaw returns payload when present (transitional form) and raw otherwise.
func (e *EventEnvelope) PayloadOrRaw() json.RawMessage {
	if len(e.Payload) > 0 && !bytes.Equal(e.Payload, []byte("null")) {
		return e.Payload
	}
	return e.Raw
}

// ParseEventEnvelope validates and decodes one frame data payload. Any malformed control envelope
// yields *ClientError with CodeInvalidEnvelope wrapping the *ValidationError; raw / payload are never
// inspected beyond their JSON type.
func ParseEventEnvelope(data []byte) (*EventEnvelope, error) {
	value, err := DecodeJSON(data)
	if err != nil {
		return nil, wrapClientError(CodeInvalidEnvelope, "frame data is not JSON", err)
	}
	if err := Validate(DefEventEnvelope, value); err != nil {
		return nil, wrapClientError(CodeInvalidEnvelope, err.Error(), err)
	}
	var envelope EventEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, wrapClientError(CodeInvalidEnvelope, "envelope does not decode", err)
	}
	if envelope.Raw == nil {
		envelope.Raw = json.RawMessage("null")
	}
	return &envelope, nil
}
