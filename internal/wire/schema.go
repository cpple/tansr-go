// Package wire validates control DTOs against the SDK's frozen JSON schemas.
// It is not a validator for ordinary tool arguments or business JSON. Those are
// opaque string/payload fields with their own contract and limits.
package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cpple/tansr-go/canonical"
	"github.com/cpple/tansr-go/contract"
)

// MaxBytes is an absolute control-DTO bound. Endpoint-specific limits (including
// raw request, artifact, and archive limits) must also be enforced by callers.
const MaxBytes = 8 << 20

const maxSchemaDepth = 128
const maxEvaluationSteps = 1_000_000

var errEvaluationLimit = errors.New("wire: schema evaluation limit exceeded")

// ValidationError identifies a rejected constraint without exposing body values.
type ValidationError struct {
	Schema     string
	Definition string
	Path       string
	Rule       string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("wire: %s#%s%s violates %s", e.Schema, e.Definition, e.Path, e.Rule)
}

// Validate accepts canonical-compatible Go values, including structs with JSON
// tags. It validates the same normalized representation that will be sent on the
// wire. Invalid UTF-8, unsafe integers, duplicate flattened struct keys, cycles,
// and non-control values are rejected before schema validation.
func Validate(schemaName, definition string, value any) error {
	raw, err := canonical.Encode(value, canonical.Options{MaxBytes: MaxBytes})
	if err != nil {
		return err
	}
	_, err = Decode(schemaName, definition, raw)
	return err
}

// Decode requires canonical control bytes, matching Serve's decodeControl. It
// rejects whitespace/ordering alternatives, duplicate keys, invalid UTF-8 or
// surrogates, non-control number syntax, and unsafe integers. The result uses
// maps, slices, int64, strings, bool, or nil, validated against a named frozen
// definition. Ordinary business JSON does not use this decoder.
func Decode(schemaName, definition string, raw []byte) (any, error) {
	doc, err := load(schemaName)
	if err != nil {
		return nil, err
	}
	n, ok := doc.definitions[definition]
	if !ok || definition == "" {
		return nil, fmt.Errorf("wire: unknown definition %q in %s", definition, schemaName)
	}
	value, err := canonical.ParseStrict(raw, canonical.Options{MaxBytes: MaxBytes})
	if err != nil {
		return nil, err
	}
	state := evaluation{schema: schemaName, definition: definition, doc: doc}
	if err := state.check(n, value, "", 0); err != nil {
		return nil, err
	}
	return value, nil
}

type document struct {
	definitions map[string]*node
	root        *node
}

type cachedDocument struct {
	doc *document
	err error
}

var documents sync.Map

func load(name string) (*document, error) {
	if cached, ok := documents.Load(name); ok {
		item := cached.(cachedDocument)
		return item.doc, item.err
	}
	raw, err := contract.ReadSchema(name)
	var doc *document
	if err == nil {
		doc, err = compile(raw)
	}
	// Cache only known schemas; untrusted names must not grow a process-wide map.
	if raw != nil {
		actual, _ := documents.LoadOrStore(name, cachedDocument{doc, err})
		item := actual.(cachedDocument)
		return item.doc, item.err
	}
	return nil, err
}

type bound struct {
	set   bool
	value int64
}

type node struct {
	boolean                            *bool
	ref                                string
	types                              []string
	properties                         map[string]*node
	required                           []string
	additional, propertyNames          *node
	items, contains                    *node
	tuple                              []*node
	tupleSet                           bool
	additionalItems                    *node
	allOf, anyOf, oneOf                []*node
	not, ifNode, thenNode, elseNode    *node
	constSet                           bool
	constant                           any
	enum                               []any
	minLength, maxLength               bound
	minItems, maxItems                 bound
	minProperties, maxProperties       bound
	minimum, maximum                   bound
	exclusiveMinimum, exclusiveMaximum bound
	multipleOf                         bound
	uniqueItems                        bool
	pattern                            *regexp.Regexp
	format                             string
}

func compile(raw []byte) (*document, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("wire: invalid frozen schema: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("wire: trailing frozen schema data")
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("wire: frozen schema root is not an object")
	}
	d := &document{definitions: map[string]*node{}}
	definitions, ok := m["definitions"].(map[string]any)
	if !ok {
		return nil, errors.New("wire: frozen schema has no definitions")
	}
	for name, value := range definitions {
		n, err := compileNode(value, 0)
		if err != nil {
			return nil, fmt.Errorf("wire: definition %s: %w", name, err)
		}
		d.definitions[name] = n
	}
	var err error
	d.root, err = compileNode(m, 0)
	if err != nil {
		return nil, err
	}
	for _, n := range d.definitions {
		if err := d.checkRefs(n); err != nil {
			return nil, err
		}
	}
	if err := d.checkRefs(d.root); err != nil {
		return nil, err
	}
	return d, nil
}

func compileNode(value any, depth int) (*node, error) {
	if depth > maxSchemaDepth {
		return nil, errors.New("wire: frozen schema nesting limit exceeded")
	}
	if b, ok := value.(bool); ok {
		return &node{boolean: &b}, nil
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("wire: schema node must be an object or boolean")
	}
	n := &node{}
	child := func(v any) (*node, error) { return compileNode(v, depth+1) }
	for key, value := range m {
		var err error
		switch key {
		case "$schema", "$id", "title", "description", "$comment", "default", "examples", "x-wire-limits":
			// Known annotations. x-wire-limits describes endpoint byte budgets;
			// each protocol client enforces those in addition to schema shape.
		case "definitions":
			if depth != 0 {
				err = errors.New("nested definitions are unsupported")
			} else if _, ok := value.(map[string]any); !ok {
				err = errors.New("definitions must be an object")
			}
		case "$ref":
			n.ref, ok = value.(string)
			if !ok || !strings.HasPrefix(n.ref, "#/definitions/") || strings.Contains(strings.TrimPrefix(n.ref, "#/definitions/"), "/") {
				err = errors.New("only local definition references are supported")
			}
		case "type":
			n.types, err = stringList(value, true)
			for _, typ := range n.types {
				switch typ {
				case "object", "array", "string", "integer", "number", "boolean", "null":
				default:
					err = fmt.Errorf("unsupported type %q", typ)
				}
			}
		case "properties":
			properties, valid := value.(map[string]any)
			if !valid {
				err = errors.New("properties must be an object")
				break
			}
			n.properties = make(map[string]*node, len(properties))
			for name, v := range properties {
				n.properties[name], err = child(v)
				if err != nil {
					break
				}
			}
		case "required":
			n.required, err = stringList(value, false)
		case "additionalProperties":
			n.additional, err = child(value)
		case "propertyNames":
			n.propertyNames, err = child(value)
		case "items":
			if values, tuple := value.([]any); tuple {
				n.tupleSet = true
				if len(values) > 0 {
					n.tuple, err = nodeList(values, depth+1)
				}
			} else {
				n.items, err = child(value)
			}
		case "additionalItems":
			n.additionalItems, err = child(value)
		case "contains":
			n.contains, err = child(value)
		case "allOf":
			n.allOf, err = nodeList(value, depth+1)
		case "anyOf":
			n.anyOf, err = nodeList(value, depth+1)
		case "oneOf":
			n.oneOf, err = nodeList(value, depth+1)
		case "not":
			n.not, err = child(value)
		case "if":
			n.ifNode, err = child(value)
		case "then":
			n.thenNode, err = child(value)
		case "else":
			n.elseNode, err = child(value)
		case "const":
			n.constSet = true
			n.constant, err = schemaConstant(value)
		case "enum":
			values, valid := value.([]any)
			if !valid || len(values) == 0 {
				err = errors.New("enum must be a nonempty array")
				break
			}
			for _, v := range values {
				var normal any
				normal, err = schemaConstant(v)
				if err != nil {
					break
				}
				n.enum = append(n.enum, normal)
			}
		case "minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf":
			var number int64
			number, err = schemaInteger(value)
			if err != nil {
				break
			}
			if (strings.HasPrefix(key, "min") || strings.HasPrefix(key, "max")) && key != "minimum" && key != "maximum" && number < 0 || key == "multipleOf" && number <= 0 {
				err = errors.New("invalid constraint bound")
				break
			}
			b := bound{true, number}
			switch key {
			case "minLength":
				n.minLength = b
			case "maxLength":
				n.maxLength = b
			case "minItems":
				n.minItems = b
			case "maxItems":
				n.maxItems = b
			case "minProperties":
				n.minProperties = b
			case "maxProperties":
				n.maxProperties = b
			case "minimum":
				n.minimum = b
			case "maximum":
				n.maximum = b
			case "exclusiveMinimum":
				n.exclusiveMinimum = b
			case "exclusiveMaximum":
				n.exclusiveMaximum = b
			case "multipleOf":
				n.multipleOf = b
			}
		case "uniqueItems":
			n.uniqueItems, ok = value.(bool)
			if !ok {
				err = errors.New("uniqueItems must be boolean")
			}
		case "pattern":
			pattern, valid := value.(string)
			if !valid {
				err = errors.New("pattern must be a string")
				break
			}
			n.pattern, err = compilePattern(pattern)
		case "format":
			n.format, ok = value.(string)
			if !ok || n.format != "date-time" {
				err = errors.New("unsupported schema format")
			}
		default:
			err = fmt.Errorf("unsupported schema keyword %q", key)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
	}
	return n, nil
}

func stringList(v any, allowString bool) ([]string, error) {
	if s, ok := v.(string); ok && allowString {
		return []string{s}, nil
	}
	values, ok := v.([]any)
	if !ok || allowString && len(values) == 0 {
		return nil, errors.New("expected string array (nonempty for type)")
	}
	list := make([]string, len(values))
	seen := map[string]bool{}
	for i, v := range values {
		s, valid := v.(string)
		if !valid || seen[s] {
			return nil, errors.New("expected unique string array")
		}
		list[i], seen[s] = s, true
	}
	return list, nil
}

func nodeList(v any, depth int) ([]*node, error) {
	values, ok := v.([]any)
	if !ok || len(values) == 0 {
		return nil, errors.New("expected nonempty schema array")
	}
	result := make([]*node, len(values))
	for i, v := range values {
		n, err := compileNode(v, depth)
		if err != nil {
			return nil, err
		}
		result[i] = n
	}
	return result, nil
}

func schemaInteger(v any) (int64, error) {
	value, ok := v.(json.Number)
	if !ok {
		return 0, errors.New("constraint is not a number")
	}
	i, err := value.Int64()
	if err != nil {
		return 0, errors.New("non-integer schema bound is unsupported")
	}
	return i, nil
}

func schemaConstant(v any) (any, error) {
	switch value := v.(type) {
	case json.Number:
		return value.Int64()
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			normal, err := schemaConstant(item)
			if err != nil {
				return nil, err
			}
			result[i] = normal
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			normal, err := schemaConstant(item)
			if err != nil {
				return nil, err
			}
			result[key] = normal
		}
		return result, nil
	default:
		return value, nil
	}
}

func compilePattern(pattern string) (*regexp.Regexp, error) {
	// The frozen ECMA patterns use no lookaround or backreferences. Convert the
	// Unicode escape notation used in terminal service path/control exclusions
	// and ECMAScript's Unicode whitespace set (RE2's \s is ASCII-only).
	const whitespace = `\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`
	var out strings.Builder
	inClass := false
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '[' {
			inClass = true
		} else if pattern[i] == ']' {
			inClass = false
		}
		if pattern[i] != '\\' || i+1 >= len(pattern) {
			out.WriteByte(pattern[i])
			continue
		}
		i++
		switch pattern[i] {
		case 'u':
			if i+4 >= len(pattern) {
				return nil, errors.New("incomplete Unicode regexp escape")
			}
			hex := pattern[i+1 : i+5]
			code, err := strconv.ParseUint(hex, 16, 16)
			if err != nil || code >= 0xd800 && code <= 0xdfff {
				return nil, errors.New("unsupported Unicode regexp escape")
			}
			out.WriteString(`\x{` + hex + `}`)
			i += 4
		case 's':
			if !inClass {
				out.WriteByte('[')
			}
			out.WriteString(whitespace)
			if !inClass {
				out.WriteByte(']')
			}
		case 'S':
			if inClass {
				return nil, errors.New("negated whitespace in a regexp class is unsupported")
			}
			out.WriteString(`[^` + whitespace + `]`)
		default:
			out.WriteByte('\\')
			out.WriteByte(pattern[i])
		}
	}
	return regexp.Compile(out.String())
}

func (d *document) checkRefs(n *node) error {
	if n.ref != "" {
		if _, ok := d.definitions[strings.TrimPrefix(n.ref, "#/definitions/")]; !ok {
			return fmt.Errorf("wire: unresolved schema reference %s", n.ref)
		}
	}
	for _, child := range n.properties {
		if err := d.checkRefs(child); err != nil {
			return err
		}
	}
	for _, group := range [][]*node{n.tuple, n.allOf, n.anyOf, n.oneOf} {
		for _, child := range group {
			if err := d.checkRefs(child); err != nil {
				return err
			}
		}
	}
	for _, child := range []*node{n.additional, n.propertyNames, n.items, n.contains, n.additionalItems, n.not, n.ifNode, n.thenNode, n.elseNode} {
		if child != nil {
			if err := d.checkRefs(child); err != nil {
				return err
			}
		}
	}
	return nil
}

type evaluation struct {
	schema, definition string
	doc                *document
	steps              int
}

func (s *evaluation) invalid(path, rule string) error {
	return &ValidationError{Schema: s.schema, Definition: s.definition, Path: path, Rule: rule}
}

func (s *evaluation) check(n *node, value any, path string, depth int) error {
	s.steps++
	if depth > maxSchemaDepth || s.steps > maxEvaluationSteps {
		return errEvaluationLimit
	}
	if n.boolean != nil {
		if !*n.boolean {
			return s.invalid(path, "false schema")
		}
		return nil
	}
	check := func(n *node, v any, p string) error { return s.check(n, v, p, depth+1) }
	if n.ref != "" {
		// Draft-07 $ref replaces its containing schema; sibling annotations do
		// not change referenced constraints.
		name := strings.TrimPrefix(n.ref, "#/definitions/")
		if err := check(s.doc.definitions[name], value, path); err != nil {
			return err
		}
		// Some older frozen definitions express decimal syntax, while the
		// shared Node validator also enforces the protocol's signed-64 range.
		if name == "Sequence" || name == "RecordSequence" {
			text, ok := value.(string)
			sequence, err := strconv.ParseUint(text, 10, 64)
			if !ok || err != nil || sequence > math.MaxInt64 {
				return s.invalid(path, "sequence range")
			}
		}
		return nil
	}
	if len(n.types) != 0 {
		match := false
		for _, typ := range n.types {
			match = match || matchesType(value, typ)
		}
		if !match {
			return s.invalid(path, "type")
		}
	}
	if n.constSet && !reflect.DeepEqual(value, n.constant) {
		return s.invalid(path, "const")
	}
	if n.enum != nil {
		match := false
		for _, candidate := range n.enum {
			if reflect.DeepEqual(value, candidate) {
				match = true
				break
			}
		}
		if !match {
			return s.invalid(path, "enum")
		}
	}
	for _, child := range n.allOf {
		if err := check(child, value, path); err != nil {
			return err
		}
	}
	if n.anyOf != nil {
		match := false
		for _, child := range n.anyOf {
			err := check(child, value, path)
			if err == nil {
				match = true
				break
			}
			if errors.Is(err, errEvaluationLimit) {
				return err
			}
		}
		if !match {
			return s.invalid(path, "anyOf")
		}
	}
	if n.oneOf != nil {
		matches := 0
		for _, child := range n.oneOf {
			err := check(child, value, path)
			if err == nil {
				matches++
			}
			if errors.Is(err, errEvaluationLimit) {
				return err
			}
		}
		if matches != 1 {
			return s.invalid(path, "oneOf")
		}
	}
	if n.not != nil {
		err := check(n.not, value, path)
		if err == nil {
			return s.invalid(path, "not")
		}
		if errors.Is(err, errEvaluationLimit) {
			return err
		}
	}
	if n.ifNode != nil {
		err := check(n.ifNode, value, path)
		if errors.Is(err, errEvaluationLimit) {
			return err
		}
		branch := n.elseNode
		if err == nil {
			branch = n.thenNode
		}
		if branch != nil {
			if err := check(branch, value, path); err != nil {
				return err
			}
		}
	}
	switch v := value.(type) {
	case map[string]any:
		if outside(int64(len(v)), n.minProperties, n.maxProperties) {
			return s.invalid(path, "property count")
		}
		for _, key := range n.required {
			if _, exists := v[key]; !exists {
				return s.invalid(pointer(path, key), "required")
			}
		}
		// Deterministic order also makes malformed-input diagnostics stable.
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if n.propertyNames != nil {
				if err := check(n.propertyNames, key, pointer(path, key)); err != nil {
					return err
				}
			}
			child, known := n.properties[key]
			if !known {
				child = n.additional
			}
			if child != nil {
				if err := check(child, v[key], pointer(path, key)); err != nil {
					return err
				}
			}
		}
	case []any:
		if outside(int64(len(v)), n.minItems, n.maxItems) {
			return s.invalid(path, "item count")
		}
		seen := map[string]bool{}
		contained := n.contains == nil
		for i, item := range v {
			p := pointer(path, strconv.Itoa(i))
			child := n.items
			if n.tupleSet {
				if i < len(n.tuple) {
					child = n.tuple[i]
				} else {
					child = n.additionalItems
				}
			}
			if child != nil {
				if err := check(child, item, p); err != nil {
					return err
				}
			}
			if n.uniqueItems {
				b, err := canonical.Encode(item, canonical.Options{MaxBytes: MaxBytes})
				if err != nil {
					return err
				}
				key := string(b)
				if seen[key] {
					return s.invalid(p, "uniqueItems")
				}
				seen[key] = true
			}
			if !contained {
				err := check(n.contains, item, p)
				if err == nil {
					contained = true
				}
				if errors.Is(err, errEvaluationLimit) {
					return err
				}
			}
		}
		if !contained {
			return s.invalid(path, "contains")
		}
	case string:
		if outside(int64(utf8.RuneCountInString(v)), n.minLength, n.maxLength) {
			return s.invalid(path, "string length")
		}
		if n.pattern != nil && !n.pattern.MatchString(v) {
			return s.invalid(path, "pattern")
		}
		if n.format == "date-time" && !validDateTime(v) {
			return s.invalid(path, "date-time")
		}
	case int64:
		if outside(v, n.minimum, n.maximum) {
			return s.invalid(path, "numeric bounds")
		}
		if n.exclusiveMinimum.set && v <= n.exclusiveMinimum.value || n.exclusiveMaximum.set && v >= n.exclusiveMaximum.value {
			return s.invalid(path, "exclusive numeric bounds")
		}
		if n.multipleOf.set && v%n.multipleOf.value != 0 {
			return s.invalid(path, "multipleOf")
		}
	}
	return nil
}

func matchesType(value any, typ string) bool {
	switch typ {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number", "integer":
		_, ok := value.(int64)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	}
	return false
}

func outside(v int64, min, max bound) bool {
	return min.set && v < min.value || max.set && v > max.value
}

func pointer(parent, key string) string {
	return parent + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

var dateTimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[Tt]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[Zz]|[+-]\d{2}:\d{2})$`)

func validDateTime(value string) bool {
	if !dateTimePattern.MatchString(value) {
		return false
	}
	normal := value[:10] + "T" + value[11:]
	if strings.HasSuffix(normal, "z") {
		normal = normal[:len(normal)-1] + "Z"
	}
	if !strings.HasSuffix(normal, "Z") {
		zone := normal[len(normal)-6:]
		hour, _ := strconv.Atoi(zone[1:3])
		minute, _ := strconv.Atoi(zone[4:6])
		if hour > 23 || minute > 59 {
			return false
		}
	}
	leap := normal[17:19] == "60"
	if leap {
		normal = normal[:17] + "59" + normal[19:]
	}
	// Match Serve's wireDateTime: it normalizes second 60 to 59 before parsing,
	// then advances the instant by one second. Validation only needs finiteness.
	_, err := time.Parse(time.RFC3339Nano, normal)
	return err == nil
}
