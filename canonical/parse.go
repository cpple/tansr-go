package canonical

import (
	"bytes"
	"strconv"
	"unicode/utf8"
)

// Decode is the normalising decoder (RFC-UAPI-1 §4.3 `decodeCanonical`): strict lexical rules
// (duplicate decoded keys, `-0` / `1e3` / `1.0` / `01`, invalid UTF-8, lone surrogates, depth / node /
// raw-byte limits) but any legal JSON whitespace, key order and equivalent escapes are accepted.
// The result re-encoded with [Encode] is the canonical byte form.
//
// Result types: map[string]any, []any, string, int64, bool, nil.
func Decode(input []byte, opts Options) (any, error) {
	return parse(input, opts, false)
}

// ParseStrict is the strict decoder (RFC-UAPI-1 §4.3 `parseStrict`): same rules as [Decode] and, in
// addition, the input bytes must already equal their canonical form. Otherwise it fails with
// CodeNotCanonical carrying the first violation's Reason, Path and byte Offset. Lexical errors take
// precedence over not_canonical.
//
// ParseStrict(x) succeeds ⇔ Decode(x) succeeds ∧ x == Encode(Decode(x)).
func ParseStrict(input []byte, opts Options) (any, error) {
	return parse(input, opts, true)
}

func parse(input []byte, opts Options, strict bool) (any, error) {
	lim, oerr := opts.normalize()
	if oerr != nil {
		return nil, oerr
	}
	if len(input) > lim.maxBytes {
		return nil, newError(CodeBytesExceeded, "input exceeds MaxBytes "+strconv.Itoa(lim.maxBytes), "", 0)
	}
	if !utf8.Valid(input) {
		return nil, newError(CodeInvalidUTF8, "input is not valid UTF-8", "", firstInvalidUTF8(input))
	}
	p := &parser{b: input, strict: strict, lim: lim}
	return p.document()
}

// firstInvalidUTF8 returns the byte offset of the first invalid UTF-8 sequence (input known invalid).
func firstInvalidUTF8(b []byte) int {
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size <= 1 {
			return i
		}
		i += size
	}
	return 0
}

type violation struct {
	reason Reason
	path   string
	at     int
}

type parser struct {
	b         []byte
	at        int
	strict    bool
	lim       limits
	nodes     int
	violation *violation
}

func isWhitespace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func (p *parser) fail(code Code, message, path string, at int) *Error {
	return newError(code, message, path, at)
}

func (p *parser) note(reason Reason, path string, at int) {
	if p.strict && p.violation == nil {
		p.violation = &violation{reason: reason, path: path, at: at}
	}
}

func (p *parser) document() (any, error) {
	value, err := p.value(0, "")
	if err != nil {
		return nil, err
	}
	p.whitespace("")
	if p.at != len(p.b) {
		return nil, p.fail(CodeTrailingData, "unexpected bytes after the root value", "", p.at)
	}
	if p.violation != nil {
		v := p.violation
		return nil, &Error{Code: CodeNotCanonical, Message: "input is valid but not canonical (" + string(v.reason) + ")", Path: v.path, Offset: v.at, Reason: v.reason}
	}
	return value, nil
}

func (p *parser) whitespace(path string) {
	start := p.at
	for p.at < len(p.b) && isWhitespace(p.b[p.at]) {
		p.at++
	}
	if p.at != start {
		p.note(ReasonWhitespace, path, start)
	}
}

func (p *parser) value(depth int, path string) (any, *Error) {
	p.whitespace(path)
	if depth > p.lim.maxDepth {
		return nil, p.fail(CodeDepthExceeded, "nesting depth exceeds MaxDepth "+strconv.Itoa(p.lim.maxDepth), path, p.at)
	}
	p.nodes++
	if p.nodes > p.lim.maxNodes {
		return nil, p.fail(CodeNodesExceeded, "node count exceeds MaxNodes "+strconv.Itoa(p.lim.maxNodes), path, p.at)
	}
	if p.at >= len(p.b) {
		return nil, p.fail(CodeUnexpectedEnd, "expected a value", path, p.at)
	}
	switch c := p.b[p.at]; {
	case c == '"':
		return p.string(path, false)
	case c == '{':
		return p.object(depth, path)
	case c == '[':
		return p.array(depth, path)
	case c == 't':
		return p.literal("true", true, path)
	case c == 'f':
		return p.literal("false", false, path)
	case c == 'n':
		return p.literal("null", nil, path)
	case isDigit(c):
		return p.number(path)
	case c == '-' || c == '+' || c == '.':
		return nil, p.fail(CodeInvalidNumber, "number token must be 0 or [1-9][0-9]* (no sign, fraction or exponent)", path, p.at)
	default:
		return nil, p.fail(CodeUnexpectedToken, "unexpected character at value position", path, p.at)
	}
}

func (p *parser) literal(token string, result any, path string) (any, *Error) {
	rest := p.b[p.at:]
	if bytes.HasPrefix(rest, []byte(token)) {
		p.at += len(token)
		return result, nil
	}
	if bytes.HasPrefix([]byte(token), rest) {
		return nil, p.fail(CodeUnexpectedEnd, "truncated literal "+token, path, p.at)
	}
	return nil, p.fail(CodeUnexpectedToken, "invalid literal (expected "+token+")", path, p.at)
}

func (p *parser) number(path string) (any, *Error) {
	start := p.at
	if p.b[p.at] == '0' {
		p.at++
	} else {
		for p.at < len(p.b) && isDigit(p.b[p.at]) {
			p.at++
		}
	}
	if p.at < len(p.b) {
		next := p.b[p.at]
		if !(next == ',' || next == ']' || next == '}' || isWhitespace(next)) {
			return nil, p.fail(CodeInvalidNumber, "number token must be 0 or [1-9][0-9]* (no leading zero, fraction or exponent)", path, start)
		}
	}
	token := p.b[start:p.at]
	if len(token) > 16 {
		return nil, p.fail(CodeUnsafeInteger, "integer exceeds 2^53-1", path, start)
	}
	u, err := strconv.ParseUint(string(token), 10, 64)
	if err != nil || u > MaxSafeInteger {
		return nil, p.fail(CodeUnsafeInteger, "integer exceeds 2^53-1", path, start)
	}
	return int64(u), nil
}

// string parses the token at p.at (an opening quote). keyMode makes canonical escape violations
// register under the member pointer of the decoded key rather than the container path.
func (p *parser) string(path string, keyMode bool) (string, *Error) {
	start := p.at
	end, err := p.scanString(path, start)
	if err != nil {
		return "", err
	}
	token := p.b[start:end]
	p.at = end
	decoded, hasEscape, err := decodeStringToken(token, path, start)
	if err != nil {
		return "", err
	}
	if p.strict && p.violation == nil && hasEscape {
		escapePath := path
		if keyMode {
			escapePath = childPointer(path, decoded)
		}
		p.escapes(token, start, escapePath)
	}
	return decoded, nil
}

// scanString walks one string token and returns the index just past the closing quote, reporting the
// first lexical problem (unescaped control character / invalid escape / truncation) in input order.
func (p *parser) scanString(path string, start int) (int, *Error) {
	b := p.b
	i := start + 1
	for i < len(b) {
		c := b[i]
		switch {
		case c == '"':
			return i + 1, nil
		case c < 0x20:
			return 0, p.fail(CodeInvalidString, "unescaped control character in string", path, i)
		case c == '\\':
			if i+1 >= len(b) {
				return 0, p.fail(CodeUnexpectedEnd, "input ends inside an escape sequence", path, i)
			}
			switch b[i+1] {
			case 'u':
				if i+6 > len(b) {
					return 0, p.fail(CodeUnexpectedEnd, "input ends inside a \\u escape", path, i)
				}
				for k := i + 2; k < i+6; k++ {
					if !isHex(b[k]) {
						return 0, p.fail(CodeInvalidString, "\\u escape requires four hexadecimal digits", path, i)
					}
				}
				i += 6
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				i += 2
			default:
				return 0, p.fail(CodeInvalidString, "invalid escape sequence", path, i)
			}
		default:
			i++
		}
	}
	return 0, p.fail(CodeUnexpectedEnd, "unterminated string", path, start)
}

func hex4(b []byte) rune {
	var r rune
	for _, c := range b {
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c-'a') + 10
		default:
			r |= rune(c-'A') + 10
		}
	}
	return r
}

// decodeStringToken decodes a lexically valid token (quotes included). Unpaired surrogates fail with
// lone_surrogate at the token start, as in the reference implementation.
func decodeStringToken(token []byte, path string, start int) (string, bool, *Error) {
	body := token[1 : len(token)-1]
	if bytes.IndexByte(body, '\\') < 0 {
		return string(body), false, nil
	}
	out := make([]byte, 0, len(body))
	for i := 0; i < len(body); {
		c := body[i]
		if c != '\\' {
			out = append(out, c)
			i++
			continue
		}
		switch body[i+1] {
		case '"':
			out = append(out, '"')
		case '\\':
			out = append(out, '\\')
		case '/':
			out = append(out, '/')
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'u':
			r := hex4(body[i+2 : i+6])
			switch {
			case r >= 0xd800 && r <= 0xdbff:
				if i+12 <= len(body) && body[i+6] == '\\' && body[i+7] == 'u' {
					low := hex4(body[i+8 : i+12])
					if low >= 0xdc00 && low <= 0xdfff {
						out = utf8.AppendRune(out, 0x10000+((r-0xd800)<<10)+(low-0xdc00))
						i += 12
						continue
					}
				}
				return "", false, newError(CodeLoneSurrogate, "string contains an unpaired surrogate", path, start)
			case r >= 0xdc00 && r <= 0xdfff:
				return "", false, newError(CodeLoneSurrogate, "string contains an unpaired surrogate", path, start)
			default:
				out = utf8.AppendRune(out, r)
			}
			i += 6
			continue
		}
		i += 2
	}
	return string(out), true, nil
}

// escapes registers the first non-canonical escape in a lexically valid token: only `\"` `\\` `\b` `\t`
// `\n` `\f` `\r` and lowercase `\u00xx` for the remaining control characters are canonical.
func (p *parser) escapes(token []byte, start int, path string) {
	for i := 1; i < len(token)-1; i++ {
		if token[i] != '\\' {
			continue
		}
		switch token[i+1] {
		case 'u':
			hex := token[i+2 : i+6]
			code := hex4(hex)
			if bytes.ContainsAny(hex, "ABCDEF") || code >= 0x20 || code == 0x08 || code == 0x09 || code == 0x0a || code == 0x0c || code == 0x0d {
				p.note(ReasonEscape, path, start+i)
				return
			}
			i += 5
		case '/':
			p.note(ReasonEscape, path, start+i)
			return
		default:
			i++
		}
	}
}

func (p *parser) expect(c byte, message, path string) *Error {
	if p.at >= len(p.b) {
		return p.fail(CodeUnexpectedEnd, message, path, p.at)
	}
	if p.b[p.at] != c {
		return p.fail(CodeUnexpectedToken, message, path, p.at)
	}
	p.at++
	return nil
}

func (p *parser) object(depth int, path string) (any, *Error) {
	p.at++
	result := map[string]any{}
	p.whitespace(path)
	if p.at >= len(p.b) {
		return nil, p.fail(CodeUnexpectedEnd, "unterminated object", path, p.at)
	}
	if p.b[p.at] == '}' {
		p.at++
		return result, nil
	}
	var previous string
	hasPrevious := false
	for {
		p.whitespace(path)
		if p.at >= len(p.b) {
			return nil, p.fail(CodeUnexpectedEnd, "unterminated object", path, p.at)
		}
		if p.b[p.at] != '"' {
			return nil, p.fail(CodeUnexpectedToken, "expected a string key", path, p.at)
		}
		keyStart := p.at
		key, err := p.string(path, true)
		if err != nil {
			return nil, err
		}
		member := childPointer(path, key)
		if !IsValidKey(key) {
			return nil, p.fail(CodeInvalidKey, "keys must be nonempty printable ASCII (0x21-0x7e)", member, keyStart)
		}
		if _, dup := result[key]; dup {
			return nil, p.fail(CodeDuplicateKey, "duplicate key after escape decoding", member, keyStart)
		}
		if hasPrevious && key < previous {
			p.note(ReasonKeyOrder, member, keyStart)
		}
		previous, hasPrevious = key, true
		p.whitespace(path)
		if err := p.expect(':', "expected \":\" after key", member); err != nil {
			return nil, err
		}
		value, err := p.value(depth+1, member)
		if err != nil {
			return nil, err
		}
		result[key] = value
		p.whitespace(path)
		if p.at >= len(p.b) {
			return nil, p.fail(CodeUnexpectedEnd, "unterminated object", path, p.at)
		}
		separator := p.b[p.at]
		p.at++
		if separator == '}' {
			return result, nil
		}
		if separator != ',' {
			return nil, p.fail(CodeUnexpectedToken, "expected \",\" or \"}\"", path, p.at-1)
		}
	}
}

func (p *parser) array(depth int, path string) (any, *Error) {
	p.at++
	result := []any{}
	p.whitespace(path)
	if p.at >= len(p.b) {
		return nil, p.fail(CodeUnexpectedEnd, "unterminated array", path, p.at)
	}
	if p.b[p.at] == ']' {
		p.at++
		return result, nil
	}
	for index := 0; ; index++ {
		value, err := p.value(depth+1, indexPointer(path, index))
		if err != nil {
			return nil, err
		}
		result = append(result, value)
		p.whitespace(path)
		if p.at >= len(p.b) {
			return nil, p.fail(CodeUnexpectedEnd, "unterminated array", path, p.at)
		}
		separator := p.b[p.at]
		p.at++
		if separator == ']' {
			return result, nil
		}
		if separator != ',' {
			return nil, p.fail(CodeUnexpectedToken, "expected \",\" or \"]\"", path, p.at-1)
		}
	}
}
