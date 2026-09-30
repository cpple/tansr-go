package canonical

import (
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Encode writes v as canonical bytes (RFC-UAPI-1 §4.1 / SDK2-ext-v1-wire §5).
//
// Accepted Go values: nil, bool, string, every integer kind, float32 / float64 holding a non-negative
// integer ≤ 2^53−1, json.Number holding a canonical integer token, maps with string keys, slices and
// arrays (except []byte), structs (exported fields, encoding/json tag names, `-` and `omitempty` honoured,
// anonymous struct fields flattened), pointers and interfaces (nil → null). Nil maps and nil slices encode
// as null like encoding/json. Everything else — including []byte, json.RawMessage, channels, functions,
// complex numbers and non-string map keys — is rejected with unsupported_value.
//
// opts.MaxBytes bounds the accumulated output; MaxDepth / MaxNodes may only tighten the fixed limits.
func Encode(v any, opts Options) ([]byte, error) {
	lim, oerr := opts.normalize()
	if oerr != nil {
		return nil, oerr
	}
	w := &writer{lim: lim, ancestors: map[ancestorKey]struct{}{}}
	if err := w.write(reflect.ValueOf(v), 0, ""); err != nil {
		return nil, err
	}
	return w.buf, nil
}

type ancestorKey struct {
	ptr  uintptr
	len  int
	kind reflect.Kind
}

type writer struct {
	buf       []byte
	lim       limits
	nodes     int
	ancestors map[ancestorKey]struct{}
}

var (
	jsonNumberType     = reflect.TypeOf(json.Number(""))
	jsonRawMessageType = reflect.TypeOf(json.RawMessage(nil))
)

func (w *writer) fail(code Code, message, path string) *Error {
	return newError(code, message, path, -1)
}

func (w *writer) ascii(part, path string) *Error {
	if len(w.buf)+len(part) > w.lim.maxBytes {
		return w.fail(CodeBytesExceeded, "encoded bytes exceed MaxBytes "+strconv.Itoa(w.lim.maxBytes), path)
	}
	w.buf = append(w.buf, part...)
	return nil
}

func (w *writer) write(v reflect.Value, depth int, path string) *Error {
	// Unwrap interfaces and pointers before counting the node; nil at any level is null.
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			v = reflect.Value{}
			break
		}
		if v.Kind() == reflect.Pointer {
			key := ancestorKey{ptr: v.Pointer(), kind: reflect.Pointer}
			if _, cyclic := w.ancestors[key]; cyclic {
				return w.fail(CodeCyclicReference, "value contains itself", path)
			}
			w.ancestors[key] = struct{}{}
			defer delete(w.ancestors, key)
		}
		v = v.Elem()
	}
	if depth > w.lim.maxDepth {
		return w.fail(CodeDepthExceeded, "nesting depth exceeds MaxDepth "+strconv.Itoa(w.lim.maxDepth), path)
	}
	w.nodes++
	if w.nodes > w.lim.maxNodes {
		return w.fail(CodeNodesExceeded, "node count exceeds MaxNodes "+strconv.Itoa(w.lim.maxNodes), path)
	}
	if !v.IsValid() {
		return w.ascii("null", path)
	}
	if v.Type() == jsonNumberType {
		return w.numberToken(v.String(), path)
	}
	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			return w.ascii("true", path)
		}
		return w.ascii("false", path)
	case reflect.String:
		return w.string(v.String(), path)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return w.int64(v.Int(), path)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := v.Uint()
		if u > MaxSafeInteger {
			return w.fail(CodeUnsafeInteger, "integer exceeds 2^53-1", path)
		}
		return w.ascii(strconv.FormatUint(u, 10), path)
	case reflect.Float32, reflect.Float64:
		return w.float(v.Float(), path)
	case reflect.Slice:
		if v.Type() == jsonRawMessageType {
			return w.fail(CodeUnsupportedValue, "json.RawMessage is not representable; Decode it first", path)
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return w.fail(CodeUnsupportedValue, "[]byte is not representable (no base64 form in canonical JSON)", path)
		}
		if v.IsNil() {
			return w.ascii("null", path)
		}
		return w.array(v, depth, path)
	case reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return w.fail(CodeUnsupportedValue, "byte arrays are not representable", path)
		}
		return w.array(v, depth, path)
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return w.fail(CodeUnsupportedValue, "map keys must be strings", path)
		}
		if v.IsNil() {
			return w.ascii("null", path)
		}
		return w.mapObject(v, depth, path)
	case reflect.Struct:
		return w.structObject(v, depth, path)
	default:
		return w.fail(CodeUnsupportedValue, v.Kind().String()+" is not representable", path)
	}
}

func (w *writer) int64(i int64, path string) *Error {
	if i > MaxSafeInteger || i < -MaxSafeInteger {
		return w.fail(CodeUnsafeInteger, "integer exceeds 2^53-1", path)
	}
	if i < 0 {
		return w.fail(CodeInvalidNumber, "number must be nonnegative", path)
	}
	return w.ascii(strconv.FormatInt(i, 10), path)
}

func (w *writer) float(f float64, path string) *Error {
	if math.IsNaN(f) || math.IsInf(f, 0) || math.Trunc(f) != f {
		return w.fail(CodeInvalidNumber, "number must be an integer (no fraction, NaN or Infinity)", path)
	}
	if f > MaxSafeInteger || f < -MaxSafeInteger {
		return w.fail(CodeUnsafeInteger, "integer exceeds 2^53-1", path)
	}
	if f < 0 || math.Signbit(f) {
		return w.fail(CodeInvalidNumber, "number must be nonnegative (no -0)", path)
	}
	return w.ascii(strconv.FormatInt(int64(f), 10), path)
}

// numberToken accepts a pre-formatted token (json.Number) only if it already is canonical.
func (w *writer) numberToken(token, path string) *Error {
	if !isCanonicalNumberToken(token) {
		return w.fail(CodeInvalidNumber, "number token must be 0 or [1-9][0-9]* (no sign, fraction or exponent)", path)
	}
	if len(token) > 16 {
		return w.fail(CodeUnsafeInteger, "integer exceeds 2^53-1", path)
	}
	u, err := strconv.ParseUint(token, 10, 64)
	if err != nil || u > MaxSafeInteger {
		return w.fail(CodeUnsafeInteger, "integer exceeds 2^53-1", path)
	}
	return w.ascii(token, path)
}

func isCanonicalNumberToken(token string) bool {
	if token == "" {
		return false
	}
	if token == "0" {
		return true
	}
	if token[0] < '1' || token[0] > '9' {
		return false
	}
	for i := 1; i < len(token); i++ {
		if token[i] < '0' || token[i] > '9' {
			return false
		}
	}
	return true
}

func (w *writer) string(s, path string) *Error {
	if !utf8.ValidString(s) {
		return w.fail(CodeInvalidUTF8, "string is not valid UTF-8", path)
	}
	// Encoded length ≥ raw length + 2 quotes: reject early without allocating.
	if len(w.buf)+len(s)+2 > w.lim.maxBytes {
		return w.fail(CodeBytesExceeded, "encoded bytes exceed MaxBytes "+strconv.Itoa(w.lim.maxBytes), path)
	}
	w.buf = appendString(w.buf, s)
	if len(w.buf) > w.lim.maxBytes {
		return w.fail(CodeBytesExceeded, "encoded bytes exceed MaxBytes "+strconv.Itoa(w.lim.maxBytes), path)
	}
	return nil
}

const hexDigits = "0123456789abcdef"

// appendString appends the canonical JSON string form of s (s must be valid UTF-8).
func appendString(buf []byte, s string) []byte {
	buf = append(buf, '"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		buf = append(buf, s[start:i]...)
		switch c {
		case '"':
			buf = append(buf, '\\', '"')
		case '\\':
			buf = append(buf, '\\', '\\')
		case '\b':
			buf = append(buf, '\\', 'b')
		case '\t':
			buf = append(buf, '\\', 't')
		case '\n':
			buf = append(buf, '\\', 'n')
		case '\f':
			buf = append(buf, '\\', 'f')
		case '\r':
			buf = append(buf, '\\', 'r')
		default:
			buf = append(buf, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		}
		start = i + 1
	}
	buf = append(buf, s[start:]...)
	return append(buf, '"')
}

func (w *writer) enter(v reflect.Value, path string) (func(), *Error) {
	var key ancestorKey
	switch v.Kind() {
	case reflect.Map:
		key = ancestorKey{ptr: v.Pointer(), kind: reflect.Map}
	case reflect.Slice:
		if v.Len() == 0 {
			return func() {}, nil
		}
		key = ancestorKey{ptr: v.Pointer(), len: v.Len(), kind: reflect.Slice}
	default:
		return func() {}, nil
	}
	if _, cyclic := w.ancestors[key]; cyclic {
		return nil, w.fail(CodeCyclicReference, "value contains itself", path)
	}
	w.ancestors[key] = struct{}{}
	return func() { delete(w.ancestors, key) }, nil
}

func (w *writer) array(v reflect.Value, depth int, path string) *Error {
	length := v.Len()
	// Every element is at least one node: reject before iterating.
	if w.nodes+length > w.lim.maxNodes {
		return w.fail(CodeNodesExceeded, "node count exceeds MaxNodes "+strconv.Itoa(w.lim.maxNodes), path)
	}
	leave, err := w.enter(v, path)
	if err != nil {
		return err
	}
	defer leave()
	if err := w.ascii("[", path); err != nil {
		return err
	}
	for i := 0; i < length; i++ {
		if i > 0 {
			if err := w.ascii(",", path); err != nil {
				return err
			}
		}
		if err := w.write(v.Index(i), depth+1, indexPointer(path, i)); err != nil {
			return err
		}
	}
	return w.ascii("]", path)
}

type member struct {
	key   string
	value reflect.Value
}

func (w *writer) mapObject(v reflect.Value, depth int, path string) *Error {
	leave, err := w.enter(v, path)
	if err != nil {
		return err
	}
	defer leave()
	members := make([]member, 0, v.Len())
	iter := v.MapRange()
	for iter.Next() {
		members = append(members, member{key: iter.Key().String(), value: iter.Value()})
	}
	return w.object(members, depth, path)
}

func (w *writer) structObject(v reflect.Value, depth int, path string) *Error {
	members := make([]member, 0, v.NumField())
	seen := map[string]struct{}{}
	if err := w.collectFields(v, path, &members, seen); err != nil {
		return err
	}
	return w.object(members, depth, path)
}

func (w *writer) collectFields(v reflect.Value, path string, members *[]member, seen map[string]struct{}) *Error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		fv := v.Field(i)
		if field.Anonymous && name == "" {
			inner := fv
			for inner.Kind() == reflect.Pointer {
				if inner.IsNil() {
					inner = reflect.Value{}
					break
				}
				inner = inner.Elem()
			}
			if inner.IsValid() && inner.Kind() == reflect.Struct {
				if err := w.collectFields(inner, path, members, seen); err != nil {
					return err
				}
				continue
			}
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			name = field.Name
		}
		if hasOption(opts, "omitempty") && isEmptyValue(fv) {
			continue
		}
		if _, dup := seen[name]; dup {
			return w.fail(CodeInvalidProperty, "duplicate JSON field name \""+name+"\"", path)
		}
		seen[name] = struct{}{}
		*members = append(*members, member{key: name, value: fv})
	}
	return nil
}

func hasOption(opts, want string) bool {
	for opts != "" {
		var head string
		head, opts, _ = strings.Cut(opts, ",")
		if head == want {
			return true
		}
	}
	return false
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return v.IsNil()
	}
	return false
}

func (w *writer) object(members []member, depth int, path string) *Error {
	sort.Slice(members, func(i, j int) bool { return members[i].key < members[j].key })
	if err := w.ascii("{", path); err != nil {
		return err
	}
	for i, m := range members {
		memberPath := childPointer(path, m.key)
		if !IsValidKey(m.key) {
			return w.fail(CodeInvalidKey, "keys must be nonempty printable ASCII (0x21-0x7e)", memberPath)
		}
		if i > 0 {
			if err := w.ascii(",", path); err != nil {
				return err
			}
		}
		if err := w.ascii(string(appendString(nil, m.key))+":", memberPath); err != nil {
			return err
		}
		if err := w.write(m.value, depth+1, memberPath); err != nil {
			return err
		}
	}
	return w.ascii("}", path)
}
