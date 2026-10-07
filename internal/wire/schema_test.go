package wire

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tansrai/tansr-go/canonical"
)

func TestFrozenSchemasCompile(t *testing.T) {
	names := []string{"archive-sync-v1", "sdk2-archive-recovery-v1", "sdk2-cache-core-v1", "sdk2-cache-v1", "sdk2-ext-v1", "terminal-observation-v1", "terminal-profile-v1", "terminal-services-v1", "terminal-shell-sandbox-v1", "unified-v1"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			doc, err := load(name)
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.definitions) == 0 {
				t.Fatal("no definitions compiled")
			}
		})
	}
}

func TestFrozenTerminalGoldenVectors(t *testing.T) {
	names := []string{"terminal-observation-v1", "terminal-profile-v1", "terminal-services-v1"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "contract", name+".golden.json"))
			if err != nil {
				t.Fatal(err)
			}
			type vector struct {
				ID         string          `json:"id"`
				Definition string          `json:"definition"`
				Value      json.RawMessage `json:"value"`
			}
			var vectors struct {
				Positive []vector `json:"positive"`
				Negative []vector `json:"negative"`
			}
			if err := json.Unmarshal(raw, &vectors); err != nil {
				t.Fatal(err)
			}
			if len(vectors.Positive) == 0 || len(vectors.Negative) == 0 {
				t.Fatal("golden vector set is empty")
			}
			for _, v := range vectors.Positive {
				t.Run("positive/"+v.ID, func(t *testing.T) {
					value, err := canonical.Decode(v.Value, canonical.Options{MaxBytes: MaxBytes})
					if err != nil {
						t.Fatal(err)
					}
					if err := Validate(name, v.Definition, value); err != nil {
						t.Fatal(err)
					}
				})
			}
			for _, v := range vectors.Negative {
				t.Run("negative/"+v.ID, func(t *testing.T) {
					value, err := canonical.Decode(v.Value, canonical.Options{MaxBytes: MaxBytes})
					if err != nil {
						return
					}
					if err := Validate(name, v.Definition, value); err == nil {
						t.Fatal("invalid golden value accepted")
					}
				})
			}
		})
	}
}

func TestDecodeControlSyntax(t *testing.T) {
	valid := `{"applicationScopeId":"app","authorizationRevision":"9007199254740993","endUserId":"用户"}`
	value, err := Decode("sdk2-ext-v1", "Scope", []byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if value.(map[string]any)["authorizationRevision"] != "9007199254740993" {
		t.Fatal("decimal string lost precision")
	}
	for name, raw := range map[string][]byte{
		"whitespace":        []byte(` {"applicationScopeId":"app","authorizationRevision":"0","endUserId":"user"}`),
		"unordered":         []byte(`{"endUserId":"user","applicationScopeId":"app","authorizationRevision":"0"}`),
		"duplicate":         []byte(`{"applicationScopeId":"app","endUserId":"user","authorizationRevision":"0","endUserId":"other"}`),
		"escaped duplicate": []byte(`{"applicationScopeId":"app","endUserId":"user","authorizationRevision":"0","end\u0055serId":"other"}`),
		"utf8":              append([]byte(`{"applicationScopeId":"app","endUserId":"`), append([]byte{0xff}, []byte(`","authorizationRevision":"0"}`)...)...),
		"surrogate":         []byte(`{"applicationScopeId":"app","endUserId":"\ud800","authorizationRevision":"0"}`),
		"extra authority":   []byte(`{"applicationScopeId":"app","endUserId":"user","authorizationRevision":"0","admin":true}`),
		"unsafe number":     []byte(`9007199254740992`),
		"exponent":          []byte(`1e3`),
		"negative":          []byte(`-1`),
		"fraction":          []byte(`1.5`),
		"null":              []byte(`null`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode("sdk2-ext-v1", "Scope", raw); err == nil {
				t.Fatal("invalid control data accepted")
			}
		})
	}
}

func TestValidateStructAndFrozenExecutor(t *testing.T) {
	type connection struct {
		Protocol     string `json:"protocol"`
		ExecutorID   string `json:"executorId"`
		ConnectionID string `json:"connectionId"`
		Revision     string `json:"connectionRevision"`
		ExpiresAt    string `json:"expiresAt"`
		Heartbeat    int    `json:"heartbeatAfterMs"`
	}
	v := connection{"sdk2-ext-v1", "executor", "connection", "9223372036854775807", "2026-10-07T12:30:00.123Z", 1000}
	if err := Validate("sdk2-ext-v1", "ExecutorConnection", v); err != nil {
		t.Fatal(err)
	}
	v.Heartbeat = 999
	if err := Validate("sdk2-ext-v1", "ExecutorConnection", v); err == nil {
		t.Fatal("heartbeat below schema bound accepted")
	}
	v.Heartbeat = 1000
	v.ExpiresAt = "2026-02-30T12:30:00Z"
	if err := Validate("sdk2-ext-v1", "ExecutorConnection", v); err == nil {
		t.Fatal("invalid date accepted")
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if err := Validate("sdk2-ext-v1", "Scope", cyclic); err == nil {
		t.Fatal("cyclic input accepted")
	}
}

func TestSchemaConstraintCombinations(t *testing.T) {
	doc, err := compile([]byte(`{"definitions":{"Value":{
		"type":"object","required":["kind","members"],"additionalProperties":false,
		"propertyNames":{"pattern":"^[a-z]+$"},"maxProperties":3,
		"properties":{"kind":{"enum":["single","multiple"]},"members":{"type":"array","uniqueItems":true,"minItems":1,"maxItems":3,"items":{"type":"integer","minimum":0,"maximum":10,"multipleOf":2}},"receipt":{"type":"string","minLength":1,"maxLength":2}},
		"allOf":[{"not":{"properties":{"members":{"contains":{"const":8}}}}}],
		"if":{"properties":{"kind":{"const":"single"}}},"then":{"properties":{"members":{"maxItems":1}}},"else":{"required":["receipt"]}
	}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		raw   string
		valid bool
	}{
		"valid one":            {`{"kind":"single","members":[2]}`, true},
		"valid unicode length": {`{"kind":"multiple","members":[2,4],"receipt":"😀好"}`, true},
		"duplicate":            {`{"kind":"multiple","members":[2,2],"receipt":"ok"}`, false},
		"contains not":         {`{"kind":"single","members":[8]}`, false},
		"if then":              {`{"kind":"single","members":[2,4]}`, false},
		"else required":        {`{"kind":"multiple","members":[2,4]}`, false},
		"multiple":             {`{"kind":"single","members":[3]}`, false},
		"max length":           {`{"kind":"multiple","members":[2],"receipt":"😀好呀"}`, false},
		"unknown key":          {`{"kind":"single","members":[2],"extra":null}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			value, err := canonical.Decode([]byte(tc.raw), canonical.Options{MaxBytes: 1024})
			if err != nil {
				t.Fatal(err)
			}
			state := evaluation{doc: doc}
			err = state.check(doc.definitions["Value"], value, "", 0)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}

func TestUnsupportedConstraintFailsClosed(t *testing.T) {
	for _, raw := range []string{
		`{"definitions":{"Value":{"type":"object","unevaluatedProperties":false}}}`,
		`{"definitions":{"Value":{"anyOf":[{"type":"string"},{"unknownConstraint":true}]}}}`,
		`{"definitions":{"Value":{"type":"string","format":"unchecked-format"}}}`,
		`{"definitions":{"Value":{"$ref":"https://remote.invalid/schema"}}}`,
		`{"definitions":{"Value":{"$ref":"#/definitions/Missing"}}}`,
		`{"definitions":{"Value":{"type":"string","pattern":"(?=a)a"}}}`,
	} {
		if _, err := compile([]byte(raw)); err == nil {
			t.Fatalf("unimplemented constraint accepted: %s", raw)
		}
	}
}

func TestRecursiveSchemaIsBounded(t *testing.T) {
	doc, err := compile([]byte(`{"definitions":{"Loop":{"$ref":"#/definitions/Loop"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	state := evaluation{doc: doc}
	if err = state.check(doc.definitions["Loop"], nil, "", 0); !errors.Is(err, errEvaluationLimit) {
		t.Fatalf("got %v", err)
	}
	doc, err = compile([]byte(`{"definitions":{"Loop":{"anyOf":[{"$ref":"#/definitions/Loop"},{"type":"null"}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	state = evaluation{doc: doc}
	if err = state.check(doc.definitions["Loop"], nil, "", 0); !errors.Is(err, errEvaluationLimit) {
		t.Fatalf("branch swallowed resource limit: %v", err)
	}
}

func TestInvalidSchemaNamesAndDefinitions(t *testing.T) {
	for _, name := range []string{"../sdk2-ext-v1", "sdk2-ext-v1.schema.json", "unknown", ""} {
		if _, err := Decode(name, "Id", []byte(`"ok"`)); err == nil {
			t.Fatal("invalid schema name accepted")
		}
	}
	if _, err := Decode("sdk2-ext-v1", "Missing", []byte(`"ok"`)); err == nil {
		t.Fatal("unknown definition accepted")
	}
	if _, err := Decode("sdk2-ext-v1", "Id", []byte(`"`+strings.Repeat("a", 129)+`"`)); err == nil {
		t.Fatal("oversize ID accepted")
	}
}

func TestFrozenRecoveryCheckpoint(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "contract", "sdk2-archive-recovery-v1.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	value, err := canonical.Decode(raw, canonical.Options{MaxBytes: MaxBytes})
	if err != nil {
		t.Fatal(err)
	}
	fixture := value.(map[string]any)
	checkpoint := fixture["receive"].(map[string]any)["checkpoint"].(map[string]any)
	for field, definition := range map[string]string{"binding": "BindingView", "status": "ArchiveStatus", "page": "ArchivePage"} {
		t.Run(definition, func(t *testing.T) {
			if err := Validate("sdk2-ext-v1", definition, checkpoint[field]); err != nil {
				t.Fatal(err)
			}
		})
	}
	for field, definition := range map[string]string{"request": "AckRebaseRequest", "response": "AckRebaseReceipt"} {
		t.Run(definition, func(t *testing.T) {
			if err := Validate("sdk2-archive-recovery-v1", definition, fixture[field]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFrozenBindingDisjointCapabilities(t *testing.T) {
	value := map[string]any{
		"protocol":             "sdk2-ext-v1",
		"request":              map[string]any{"requestId": "request", "operationEpoch": "epoch"},
		"target":               map[string]any{"sessionId": "session", "sourceSnapshotDigest": strings.Repeat("a", 64), "generations": map[string]any{"historyEpoch": "history", "deletionGeneration": "0", "projectionRevision": "0"}},
		"expectedRevision":     "0",
		"requiredCapabilities": []string{"archive-transfer-v1"},
		"optionalCapabilities": []string{"context-materials-v1"},
		"archive":              map[string]any{"strategy": "single-authorized-source", "sourceId": "source", "durability": "source-ack-with-durable-spool", "delivery": "required", "sessionAvailability": "legacy-complete", "ackFormat": "split-receipts-v1"},
	}
	if err := Validate("sdk2-ext-v1", "BindingCreateRequest", value); err != nil {
		t.Fatal(err)
	}
	value["optionalCapabilities"] = []string{"archive-transfer-v1"}
	if err := Validate("sdk2-ext-v1", "BindingCreateRequest", value); err == nil {
		t.Fatal("overlapping capabilities accepted")
	}
}

func TestDateTimeAndECMAPatternParity(t *testing.T) {
	for _, value := range []string{"2026-10-07T12:30:00Z", "2026-10-07t12:30:00z", "2026-10-07T12:30:60+01:30", "2024-02-29T23:59:59.123456789123Z"} {
		if !validDateTime(value) {
			t.Fatalf("valid time rejected: %s", value)
		}
	}
	for _, value := range []string{"2026-02-29T00:00:00Z", "2026-10-07T24:00:00Z", "2026-10-07T23:61:00Z", "2026-10-07T12:30:61Z", "2026-10-07T12:30:00+24:00", "2026-10-07T12:30:00+00:60", "2026-10-07T12:30:00Z\n"} {
		if validDateTime(value) {
			t.Fatalf("invalid time accepted: %s", value)
		}
	}
	p, err := compilePattern(`^[^\s\\]+$`)
	if err != nil {
		t.Fatal(err)
	}
	if !p.MatchString("sdk2#Id") {
		t.Fatal("legal schema reference rejected")
	}
	for _, value := range []string{"a b", "a\tb", "a\u00a0b", "a\u3000b", "a\ufeffb", "a\\b"} {
		if p.MatchString(value) {
			t.Fatalf("ECMA whitespace/path constraint bypass: %q", value)
		}
	}
	p, err = compilePattern(`^[^\u0000-\u001f\u007f]+$`)
	if err != nil {
		t.Fatal(err)
	}
	if p.MatchString("a\x00b") || p.MatchString("a\x7fb") || !p.MatchString("路径") {
		t.Fatal("Unicode escape conversion changed the pattern")
	}
}

func TestReferencedSequenceUsesInt64Range(t *testing.T) {
	doc, err := compile([]byte(`{"definitions":{"Sequence":{"type":"string","pattern":"^(0|[1-9][0-9]{0,18})$"},"Value":{"$ref":"#/definitions/Sequence"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	state := evaluation{doc: doc}
	if err := state.check(doc.definitions["Value"], "9223372036854775807", "", 0); err != nil {
		t.Fatal(err)
	}
	state = evaluation{doc: doc}
	if err := state.check(doc.definitions["Value"], "9223372036854775808", "", 0); err == nil {
		t.Fatal("unsigned sequence beyond protocol range accepted")
	}
}
