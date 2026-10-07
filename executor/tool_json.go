package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/tansrai/tansr-go/canonical"
)

const toolBytes = 32768

// Business JSON is deliberately distinct from canonical control metadata: fractional/negative
// numbers and Unicode keys are allowed. The parser still bounds depth/nodes and rejects malformed
// Unicode, duplicate keys and trailing JSON instead of silently changing the host's arguments.
func toolObject(text string) (map[string]any, error) {
	if len(text) > toolBytes || !utf8.ValidString(text) || !validEscapes(text) {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	nodes := 0
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		nodes++
		if depth > 32 || nodes > 32768 {
			return nil, ErrInvalid
		}
		token, err := d.Token()
		if err != nil {
			return nil, ErrInvalid
		}
		switch v := token.(type) {
		case json.Delim:
			switch v {
			case '{':
				object := map[string]any{}
				for d.More() {
					keyToken, e := d.Token()
					if e != nil {
						return nil, ErrInvalid
					}
					key, ok := keyToken.(string)
					if !ok {
						return nil, ErrInvalid
					}
					if _, exists := object[key]; exists {
						return nil, ErrInvalid
					}
					child, e := read(depth + 1)
					if e != nil {
						return nil, e
					}
					object[key] = child
				}
				if end, e := d.Token(); e != nil || end != json.Delim('}') {
					return nil, ErrInvalid
				}
				return object, nil
			case '[':
				array := []any{}
				for d.More() {
					child, e := read(depth + 1)
					if e != nil {
						return nil, e
					}
					array = append(array, child)
				}
				if end, e := d.Token(); e != nil || end != json.Delim(']') {
					return nil, ErrInvalid
				}
				return array, nil
			default:
				return nil, ErrInvalid
			}
		case json.Number:
			n, e := strconv.ParseFloat(string(v), 64)
			if e != nil || math.IsInf(n, 0) || math.IsNaN(n) {
				return nil, ErrInvalid
			}
			return v, nil
		default:
			return token, nil
		}
	}
	value, err := read(0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, ErrInvalid
	}
	return object, nil
}
func validEscapes(text string) bool {
	// encoding/json replaces lone surrogate escapes with U+FFFD; detect them before decoding.
	inside := false
	for i := 0; i < len(text); i++ {
		if text[i] == '"' {
			inside = !inside
			continue
		}
		if !inside || text[i] != '\\' {
			continue
		}
		i++
		if i >= len(text) {
			return false
		}
		if text[i] != 'u' {
			continue
		}
		if i+4 >= len(text) {
			return false
		}
		code, err := strconv.ParseUint(text[i+1:i+5], 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code >= 0xd800 && code <= 0xdbff {
			if i+6 >= len(text) || text[i+1:i+3] != "\\u" {
				return false
			}
			low, err := strconv.ParseUint(text[i+3:i+7], 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return !inside
}
func verifyToolResult(text string) error {
	v, err := toolObject(text)
	if err != nil {
		return err
	}
	if v["status"] == "error" {
		message, ok := v["message"].(string)
		if !ok || len(message) == 0 || jsLength(message) > 4096 {
			return ErrInvalid
		}
		return nil
	}
	if v["status"] != "ok" {
		return ErrInvalid
	}
	if isError, ok := v["isError"]; ok {
		if _, valid := isError.(bool); !valid {
			return ErrInvalid
		}
	}
	content, ok := v["content"].([]any)
	if !ok || len(content) < 1 || len(content) > 64 {
		return ErrInvalid
	}
	for _, entry := range content {
		item, ok := entry.(map[string]any)
		if !ok {
			return ErrInvalid
		}
		if item["t"] == "text" {
			if _, ok = item["text"].(string); !ok {
				return ErrInvalid
			}
			continue
		}
		if item["t"] != "image" {
			return ErrInvalid
		}
		mime, ok := item["mime"].(string)
		if !ok || !(mime == "image/png" || mime == "image/jpeg" || mime == "image/webp" || mime == "image/gif") {
			return ErrInvalid
		}
		if _, ok = item["data"].(string); !ok {
			return ErrInvalid
		}
	}
	return nil
}

var toolNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)

// DefinitionDigest hashes the exact declaration passed to session creation. This helper accepts
// the canonical-compatible declaration subset: printable ASCII parameter keys and safe integer
// control fields. It does not insert defaults. The digest framing is the frozen SDK2 client-tool
// framing, whose legacy sorted JSON is identical for this subset; unsupported values fail locally.
func DefinitionDigest(declaration map[string]any) (string, error) {
	encoded, err := canonical.Encode(declaration, canonical.Options{MaxBytes: controlBytes})
	if err != nil {
		return "", err
	}
	normalized, err := canonical.Decode(encoded, canonical.Options{MaxBytes: controlBytes})
	if err != nil {
		return "", err
	}
	v := normalized.(map[string]any)
	for k := range v {
		switch k {
		case "name", "description", "parameters", "readOnly", "effects", "timeoutMs":
		default:
			return "", ErrInvalid
		}
	}
	name, ok := v["name"].(string)
	if !ok || !toolNamePattern.MatchString(name) {
		return "", ErrInvalid
	}
	description, ok := v["description"].(string)
	if !ok || description == "" || jsLength(description) > 2048 {
		return "", ErrInvalid
	}
	if r, ok := v["readOnly"]; ok {
		if _, valid := r.(bool); !valid {
			return "", ErrInvalid
		}
	}
	if t, ok := v["timeoutMs"]; ok {
		n, valid := t.(int64)
		if !valid || n < 1000 || n > 600000 {
			return "", ErrInvalid
		}
	}
	if effects, ok := v["effects"]; ok {
		list, valid := effects.([]any)
		if !valid || len(list) > 4 {
			return "", ErrInvalid
		}
		seen := map[string]bool{}
		for _, entry := range list {
			s, valid := entry.(string)
			if !valid || seen[s] || !(s == "irreversible" || s == "financial" || s == "external" || s == "affects-others") {
				return "", ErrInvalid
			}
			seen[s] = true
		}
	}
	if p, ok := v["parameters"]; ok {
		parameters, valid := p.(map[string]any)
		if !valid {
			return "", ErrInvalid
		}
		// The original endpoint has independent parameter bytes/depth limits.
		data, e := legacyJSON(parameters)
		if e != nil || len(data) > 32768 {
			return "", ErrInvalid
		}
		for _, spec := range parameters {
			if err := validateParameter(spec, 1); err != nil {
				return "", err
			}
		}
	}
	encoded, err = legacyJSON(v)
	if err != nil {
		return "", err
	}
	digest, err := canonical.DomainDigest("tansr.sdk2.client-tool.v1", encoded)
	if err != nil {
		return "", fmt.Errorf("%w: definition digest", err)
	}
	return canonical.Hex(digest), nil
}
func validateParameter(value any, depth int) error {
	if depth > 8 {
		return ErrInvalid
	}
	v, ok := value.(map[string]any)
	if !ok {
		return ErrInvalid
	}
	for k := range v {
		switch k {
		case "type", "description", "optional", "items", "properties":
		default:
			return ErrInvalid
		}
	}
	typeName, ok := v["type"].(string)
	if !ok || !(typeName == "string" || typeName == "number" || typeName == "boolean" || typeName == "array" || typeName == "object") {
		return ErrInvalid
	}
	if d, ok := v["description"]; ok {
		s, valid := d.(string)
		if !valid || jsLength(s) > 2048 {
			return ErrInvalid
		}
	}
	if optional, ok := v["optional"]; ok {
		if _, valid := optional.(bool); !valid {
			return ErrInvalid
		}
	}
	if items, ok := v["items"]; ok {
		if err := validateParameter(items, depth+1); err != nil {
			return err
		}
	}
	if p, ok := v["properties"]; ok {
		props, valid := p.(map[string]any)
		if !valid {
			return ErrInvalid
		}
		for _, spec := range props {
			if err := validateParameter(spec, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
func jsLength(value string) int { return len(utf16.Encode([]rune(value))) }

// JSON.stringify visits integer-index properties first, even after sortKeysDeep. This matters for
// legal parameter names "2" and "10" and is distinct from the new canonical control codec.
func legacyJSON(value any) ([]byte, error) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			if key == "__proto__" {
				return nil, ErrInvalid
			}
			keys = append(keys, key)
		}
		index := func(key string) (uint64, bool) {
			n, e := strconv.ParseUint(key, 10, 32)
			return n, e == nil && n < 4294967295 && strconv.FormatUint(n, 10) == key
		}
		sort.Slice(keys, func(i, j int) bool {
			a, ai := index(keys[i])
			b, bi := index(keys[j])
			if ai && bi {
				return a < b
			}
			if ai != bi {
				return ai
			}
			return keys[i] < keys[j]
		})
		var out bytes.Buffer
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			k, e := canonical.Encode(key, canonical.Options{MaxBytes: controlBytes})
			if e != nil {
				return nil, e
			}
			out.Write(k)
			out.WriteByte(':')
			b, e := legacyJSON(v[key])
			if e != nil {
				return nil, e
			}
			out.Write(b)
		}
		out.WriteByte('}')
		return out.Bytes(), nil
	case []any:
		var out bytes.Buffer
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			b, e := legacyJSON(item)
			if e != nil {
				return nil, e
			}
			out.Write(b)
		}
		out.WriteByte(']')
		return out.Bytes(), nil
	default:
		return canonical.Encode(value, canonical.Options{MaxBytes: controlBytes})
	}
}
