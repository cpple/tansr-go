package api

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type goldenVector struct {
	Name       string          `json:"name"`
	Definition string          `json:"definition"`
	Expect     string          `json:"expect"`
	Value      json.RawMessage `json:"value"`
	ValueFile  string          `json:"valueFile"`
	Base       string          `json:"base"`
	Patch      []struct {
		Op    string          `json:"op"`
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value"`
	} `json:"patch"`
	Note string `json:"note"`
}

type goldenFile struct {
	Vectors []goldenVector `json:"vectors"`
}

func loadGolden(t *testing.T) (*goldenFile, map[string]*goldenVector) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "contract", "unified-v1.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var golden goldenFile
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	byName := map[string]*goldenVector{}
	for i := range golden.Vectors {
		byName[golden.Vectors[i].Name] = &golden.Vectors[i]
	}
	return &golden, byName
}

func decodeGeneric(t *testing.T, raw []byte) any {
	t.Helper()
	value, err := DecodeJSON(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return value
}

// materialise resolves value / valueFile / base+patch into a generic tree.
func materialise(t *testing.T, v *goldenVector, byName map[string]*goldenVector) any {
	t.Helper()
	switch {
	case v.ValueFile != "":
		if v.ValueFile != "packages/server/contract/api-manifest.json" {
			t.Fatalf("%s: unknown valueFile %s", v.Name, v.ValueFile)
		}
		raw, err := os.ReadFile(filepath.Join("..", "contract", "api-manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		return decodeGeneric(t, raw)
	case v.Base != "":
		base, ok := byName[v.Base]
		if !ok {
			t.Fatalf("%s: unknown base %s", v.Name, v.Base)
		}
		value := materialise(t, base, byName)
		for _, p := range v.Patch {
			var patchValue any
			if p.Value != nil {
				patchValue = decodeGeneric(t, p.Value)
			}
			var err error
			value, err = applyPatch(value, p.Op, p.Path, patchValue)
			if err != nil {
				t.Fatalf("%s: patch %s %s: %v", v.Name, p.Op, p.Path, err)
			}
		}
		return value
	default:
		return decodeGeneric(t, v.Value)
	}
}

func splitPointer(pointer string) []string {
	if pointer == "" {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for i, part := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return parts
}

// applyPatch implements RFC 6902 add / replace / remove on the generic tree (copy on write).
func applyPatch(root any, op, pointer string, value any) (any, error) {
	parts := splitPointer(pointer)
	if len(parts) == 0 {
		if op == "remove" {
			return nil, nil
		}
		return value, nil
	}
	return patchNode(root, parts, op, value)
}

func patchNode(node any, parts []string, op string, value any) (any, error) {
	key := parts[0]
	switch container := node.(type) {
	case map[string]any:
		clone := make(map[string]any, len(container)+1)
		for k, v := range container {
			clone[k] = v
		}
		if len(parts) == 1 {
			switch op {
			case "add", "replace":
				if op == "replace" {
					if _, ok := clone[key]; !ok {
						return nil, errPath(key)
					}
				}
				clone[key] = value
			case "remove":
				if _, ok := clone[key]; !ok {
					return nil, errPath(key)
				}
				delete(clone, key)
			}
			return clone, nil
		}
		child, ok := clone[key]
		if !ok {
			return nil, errPath(key)
		}
		next, err := patchNode(child, parts[1:], op, value)
		if err != nil {
			return nil, err
		}
		clone[key] = next
		return clone, nil
	case []any:
		clone := append([]any(nil), container...)
		if len(parts) == 1 {
			if key == "-" && op == "add" {
				return append(clone, value), nil
			}
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i > len(clone) || (op != "add" && i == len(clone)) {
				return nil, errPath(key)
			}
			switch op {
			case "add":
				clone = append(clone[:i], append([]any{value}, clone[i:]...)...)
			case "replace":
				clone[i] = value
			case "remove":
				clone = append(clone[:i], clone[i+1:]...)
			}
			return clone, nil
		}
		i, err := strconv.Atoi(key)
		if err != nil || i < 0 || i >= len(clone) {
			return nil, errPath(key)
		}
		next, err := patchNode(clone[i], parts[1:], op, value)
		if err != nil {
			return nil, err
		}
		clone[i] = next
		return clone, nil
	}
	return nil, errPath(key)
}

type pathError string

func (e pathError) Error() string { return "path segment not found: " + string(e) }
func errPath(key string) error    { return pathError(key) }

// TestGoldenVectors validates every unified-v1 golden vector: positives must pass, negatives must fail;
// positives must additionally decode into the typed structs without unknown fields.
func TestGoldenVectors(t *testing.T) {
	golden, byName := loadGolden(t)
	counts := map[string]int{}
	for i := range golden.Vectors {
		v := &golden.Vectors[i]
		t.Run(v.Name, func(t *testing.T) {
			value := materialise(t, v, byName)
			err := Validate(v.Definition, value)
			switch v.Expect {
			case "valid":
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				assertTyped(t, v.Definition, value)
			case "invalid":
				if err == nil {
					t.Fatalf("expected invalid (%s)", v.Note)
				}
				var verr *ValidationError
				if !asValidation(err, &verr) || verr.Definition != v.Definition {
					t.Fatalf("expected *ValidationError for %s, got %T %v", v.Definition, err, err)
				}
			default:
				t.Fatalf("unknown expect %q", v.Expect)
			}
			counts[v.Expect]++
		})
	}
	if counts["valid"] < 30 || counts["invalid"] < 100 {
		t.Fatalf("unexpected vector counts: %v", counts)
	}
}

func asValidation(err error, target **ValidationError) bool {
	v, ok := err.(*ValidationError)
	if ok {
		*target = v
	}
	return ok
}

// assertTyped checks that the typed Go shapes cover every key of a valid vector (DisallowUnknownFields)
// and round-trip the value.
func assertTyped(t *testing.T, definition string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	strict := func(target any) {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(target); err != nil {
			t.Fatalf("typed decode of %s: %v", definition, err)
		}
	}
	switch definition {
	case DefManifest:
		var m Manifest
		strict(&m)
		if m.Revision < 1 || len(m.Operations) == 0 {
			t.Fatal("manifest decoded empty")
		}
	case DefCapabilities:
		var c Capabilities
		strict(&c)
		if c.Domains.Session.Contract != "agent-session-v1" {
			t.Fatal("capabilities decoded wrong")
		}
	case DefCapabilityClosure:
		var c CapabilityClosure
		strict(&c)
		if len(c.Operations) != 76 || len(c.Domains) != 8 {
			t.Fatalf("closure decoded %d operations / %d domains", len(c.Operations), len(c.Domains))
		}
	case DefFacadeError, DefUnifiedError:
		var e ErrorEnvelope
		strict(&e)
		if e.Code == "" || e.Status == 0 {
			t.Fatal("error envelope decoded empty")
		}
	case DefEventEnvelope:
		env, err := ParseEventEnvelope(raw)
		if err != nil {
			t.Fatalf("ParseEventEnvelope: %v", err)
		}
		if env.Domain == "" || env.Raw == nil {
			t.Fatal("event envelope decoded empty")
		}
		var e EventEnvelope
		strict(&e)
	case DefRequestHeaders, DefResponseHeaders:
		// header projections have no typed struct beyond Meta
	default:
		t.Fatalf("no typed assertion for %s", definition)
	}
}

// TestEventEnvelopeTransitionalForms: D18 seven-key wire (no seq / payload) and today's six-key
// server form (no eventId) decode; a generic cursor position or an identity key never does.
func TestEventEnvelopeTransitionalForms(t *testing.T) {
	seven := `{"contract":"unified-v1","eventId":"12","domain":"session","type":"msg.text.delta","cursorSet":{"eventCursor":"12","archiveCoverage":null,"outputWatermark":null,"materialConsumed":null,"ackReceipt":null},"terminalStatus":null,"raw":{"type":"msg.text.delta","text":"hi"}}`
	env, err := ParseEventEnvelope([]byte(seven))
	if err != nil {
		t.Fatal(err)
	}
	if env.EventID == nil || *env.EventID != "12" || env.Seq != nil || env.Payload != nil || env.IsTerminal() {
		t.Fatalf("seven-key form: %+v", env)
	}
	if cur, ok := env.CursorSet.EventCursor.Text(); !ok || cur != "12" || !env.CursorSet.AckReceipt.IsNull() {
		t.Fatalf("cursor positions: %+v", env.CursorSet)
	}
	if string(env.PayloadOrRaw()) != `{"type":"msg.text.delta","text":"hi"}` {
		t.Fatalf("PayloadOrRaw: %s", env.PayloadOrRaw())
	}
	six := `{"contract":"unified-v1","domain":"archive","type":null,"cursorSet":{"eventCursor":"c-1","archiveCoverage":{"fromSequence":"1","throughSequence":"9","headDigest":"x"},"outputWatermark":null,"materialConsumed":null,"ackReceipt":null},"terminalStatus":"completed","raw":{"unknown":true}}`
	env, err = ParseEventEnvelope([]byte(six))
	if err != nil {
		t.Fatal(err)
	}
	if env.EventID != nil || env.Type != nil || !env.IsTerminal() {
		t.Fatalf("six-key form: %+v", env)
	}
	if _, ok := env.CursorSet.ArchiveCoverage.Text(); ok || env.CursorSet.ArchiveCoverage.IsNull() {
		t.Fatal("coverage object must be neither text nor null")
	}
	for _, bad := range []string{
		`{"contract":"unified-v1","domain":"session","type":null,"cursorSet":{"eventCursor":null,"archiveCoverage":null,"outputWatermark":null,"materialConsumed":null,"ackReceipt":null,"cursor":"1"},"terminalStatus":null,"raw":null}`,
		`{"contract":"unified-v1","domain":"session","type":null,"cursorSet":{"eventCursor":null,"archiveCoverage":null,"outputWatermark":null,"materialConsumed":null,"ackReceipt":null},"terminalStatus":null,"raw":null,"sessionId":"s"}`,
		`{"contract":"unified-v1","domain":"session","type":null,"cursorSet":{"eventCursor":null,"archiveCoverage":null,"outputWatermark":null,"materialConsumed":null,"ackReceipt":null},"terminalStatus":null}`,
		`{"contract":"unified-v2","domain":"session","type":null,"cursorSet":{"eventCursor":null,"archiveCoverage":null,"outputWatermark":null,"materialConsumed":null,"ackReceipt":null},"terminalStatus":null,"raw":null}`,
		`[]`,
		`not json`,
	} {
		_, err := ParseEventEnvelope([]byte(bad))
		var cerr *ClientError
		if !errorsAs(err, &cerr) || cerr.Code != CodeInvalidEnvelope {
			t.Fatalf("expected invalid_envelope for %s, got %v", bad, err)
		}
	}
}
