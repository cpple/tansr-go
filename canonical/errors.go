package canonical

import (
	"fmt"
	"strconv"
	"strings"
)

// Code is the stable canonical error code table (RFC-UAPI-1 §4; identical to the JavaScript
// CANONICAL_ERROR_CODES). Codes marked "decode" / "encode" are only produced by that side.
type Code string

const (
	// CodeInvalidOptions — both sides: MaxBytes < 1, MaxDepth > 32, MaxNodes > 100000, invalid domain name.
	CodeInvalidOptions Code = "invalid_options"
	// CodeInvalidInput — digest side: nil input where bytes are required.
	CodeInvalidInput Code = "invalid_input"
	// CodeBytesExceeded — decode: raw input bytes > MaxBytes (checked before UTF-8 validation);
	// encode: accumulated output bytes > MaxBytes.
	CodeBytesExceeded Code = "bytes_exceeded"
	// CodeInvalidUTF8 — decode: invalid UTF-8 (overlong, truncated, CESU-8 surrogates, > U+10FFFF);
	// encode (Go only): a Go string that is not valid UTF-8 — the Go analogue of a lone surrogate.
	CodeInvalidUTF8 Code = "invalid_utf8"
	// CodeUnexpectedEnd — decode: input ends inside a value / string / literal / escape (empty input included).
	CodeUnexpectedEnd Code = "unexpected_end"
	// CodeUnexpectedToken — decode: structural error (bad start byte, missing ':' / ',', unquoted key,
	// uppercase literal, NaN / Infinity, BOM, non-JSON whitespace).
	CodeUnexpectedToken Code = "unexpected_token"
	// CodeTrailingData — decode: non-whitespace bytes after the root value.
	CodeTrailingData Code = "trailing_data"
	// CodeInvalidNumber — both sides: number token is not `0 | [1-9][0-9]*`; encode: fraction / negative / -0 / NaN / Inf.
	CodeInvalidNumber Code = "invalid_number"
	// CodeUnsafeInteger — both sides: integer magnitude exceeds 2^53−1.
	CodeUnsafeInteger Code = "unsafe_integer"
	// CodeInvalidString — decode: string lexical error (bad escape, short \u, unescaped control character).
	CodeInvalidString Code = "invalid_string"
	// CodeLoneSurrogate — decode: unpaired / reversed / high-followed-by-non-low surrogate escape.
	CodeLoneSurrogate Code = "lone_surrogate"
	// CodeInvalidKey — both sides: key is not non-empty printable ASCII (^[\x21-\x7e]+$).
	CodeInvalidKey Code = "invalid_key"
	// CodeDuplicateKey — decode: same key twice in one object after escape decoding.
	CodeDuplicateKey Code = "duplicate_key"
	// CodeDepthExceeded — both sides: ancestor container count > MaxDepth (root = 0).
	CodeDepthExceeded Code = "depth_exceeded"
	// CodeNodesExceeded — both sides: node count (root, containers, scalars; keys excluded) > MaxNodes.
	CodeNodesExceeded Code = "nodes_exceeded"
	// CodeUnsupportedValue — encode: Go value not representable (func, chan, complex, []byte, non-string map key…).
	CodeUnsupportedValue Code = "unsupported_value"
	// CodeInvalidProperty — encode: struct field shape not representable (duplicate JSON name after tag resolution).
	CodeInvalidProperty Code = "invalid_property"
	// CodeCyclicReference — encode: a map / slice / pointer appears in its own ancestor chain.
	CodeCyclicReference Code = "cyclic_reference"
	// CodeNotCanonical — ParseStrict only: input is a valid, normalisable document but its bytes differ
	// from its canonical bytes; Reason names the first violation.
	CodeNotCanonical Code = "not_canonical"
)

// Reason classifies the first canonical violation reported with CodeNotCanonical.
type Reason string

const (
	ReasonWhitespace Reason = "whitespace"
	ReasonKeyOrder   Reason = "key_order"
	ReasonEscape     Reason = "escape"
)

// Error is the unified canonical error `{ code, path, offset, message, reason }`.
//
// Path is an RFC 6901 JSON pointer ("" = root, "/a/0/b"); on the encode side it points at the offending
// Go value, on the decode side at the offending node or its container. Offset is the UTF-8 byte offset
// into the raw input on the decode side and -1 where not applicable. Reason is set only for not_canonical.
type Error struct {
	Code    Code
	Message string
	Path    string
	Offset  int
	Reason  Reason
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("canonical: ")
	b.WriteString(string(e.Code))
	if e.Reason != "" {
		b.WriteString(" (")
		b.WriteString(string(e.Reason))
		b.WriteString(")")
	}
	if e.Path != "" || e.Offset >= 0 {
		b.WriteString(" at ")
		if e.Path != "" {
			b.WriteString(e.Path)
		} else {
			b.WriteString("<root>")
		}
		if e.Offset >= 0 {
			b.WriteString(" byte ")
			b.WriteString(strconv.Itoa(e.Offset))
		}
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	return b.String()
}

func newError(code Code, message, path string, offset int) *Error {
	return &Error{Code: code, Message: message, Path: path, Offset: offset}
}

func optionsError(format string, args ...any) *Error {
	return &Error{Code: CodeInvalidOptions, Message: fmt.Sprintf(format, args...), Offset: -1}
}

// PointerSegment escapes one JSON pointer reference token (RFC 6901: `~` → `~0`, `/` → `~1`).
func PointerSegment(key string) string {
	if !strings.ContainsAny(key, "~/") {
		return key
	}
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

func childPointer(parent, key string) string {
	return parent + "/" + PointerSegment(key)
}

func indexPointer(parent string, index int) string {
	return parent + "/" + strconv.Itoa(index)
}
