package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const defaultMaxBytes = 262144

func readContract(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "contract", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func decodeJSON(t *testing.T, data []byte, v any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
}

type crossVector struct {
	ID        string  `json:"id"`
	Category  string  `json:"category"`
	InputKind string  `json:"inputKind"`
	Input     *string `json:"input"`
	Generator *struct {
		Kind    string `json:"kind"`
		Element string `json:"element"`
		Count   int    `json:"count"`
		Tail    string `json:"tail"`
	} `json:"generator"`
	MaxBytes        int    `json:"maxBytes"`
	Expect          string `json:"expect"`
	CanonicalHex    string `json:"canonicalHex"`
	CanonicalSha256 string `json:"canonicalSha256"`
	Note            string `json:"note"`
}

func (v crossVector) bytes(t *testing.T) []byte {
	t.Helper()
	if v.Generator != nil {
		if v.Generator.Kind != "flat-array" {
			t.Fatalf("%s: unknown generator %q", v.ID, v.Generator.Kind)
		}
		var b strings.Builder
		b.WriteByte('[')
		for i := 0; i < v.Generator.Count; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(v.Generator.Element)
		}
		if v.Generator.Tail != "" {
			b.WriteByte(',')
			b.WriteString(v.Generator.Tail)
		}
		b.WriteByte(']')
		return []byte(b.String())
	}
	if v.Input == nil {
		t.Fatalf("%s: no input", v.ID)
	}
	switch v.InputKind {
	case "utf8-text":
		return []byte(*v.Input)
	case "bytes":
		raw, err := base64.StdEncoding.DecodeString(*v.Input)
		if err != nil {
			t.Fatalf("%s: base64: %v", v.ID, err)
		}
		return raw
	}
	t.Fatalf("%s: unknown inputKind %q", v.ID, v.InputKind)
	return nil
}

// TestCrossVectors runs the 127 shared vectors through both entry points: Decode decides accept /
// reject exactly like the reference implementation; the canonical bytes match; ParseStrict accepts
// precisely the inputs that already are canonical and otherwise reports not_canonical.
func TestCrossVectors(t *testing.T) {
	var fixture struct {
		Vectors []crossVector `json:"vectors"`
	}
	decodeJSON(t, readContract(t, "canonical-cross-vectors.json"), &fixture)
	if len(fixture.Vectors) != 127 {
		t.Fatalf("expected 127 vectors, got %d", len(fixture.Vectors))
	}
	for _, v := range fixture.Vectors {
		t.Run(v.ID, func(t *testing.T) {
			input := v.bytes(t)
			opts := Options{MaxBytes: defaultMaxBytes}
			if v.MaxBytes != 0 {
				opts.MaxBytes = v.MaxBytes
			}
			value, err := Decode(input, opts)
			strictValue, strictErr := ParseStrict(input, opts)
			if v.Expect == "reject" {
				if err == nil {
					t.Fatalf("Decode accepted a reject vector: %v", value)
				}
				var cerr *Error
				if !errors.As(err, &cerr) || cerr.Code == CodeNotCanonical {
					t.Fatalf("Decode error must be a lexical *Error, got %v", err)
				}
				if strictErr == nil {
					t.Fatalf("ParseStrict accepted a reject vector")
				}
				var serr *Error
				if !errors.As(strictErr, &serr) || serr.Code != cerr.Code {
					t.Fatalf("ParseStrict code %v differs from Decode code %v", strictErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Decode rejected an accept vector: %v", err)
			}
			encoded, err := Encode(value, opts)
			if err != nil {
				t.Fatalf("Encode(Decode(x)): %v", err)
			}
			if v.CanonicalHex != "" {
				if got := hex.EncodeToString(encoded); got != v.CanonicalHex {
					t.Fatalf("canonical bytes differ\n got %s\nwant %s", got, v.CanonicalHex)
				}
			} else {
				sum := sha256.Sum256(encoded)
				if got := hex.EncodeToString(sum[:]); got != v.CanonicalSha256 {
					t.Fatalf("canonical sha256 differs: got %s want %s", got, v.CanonicalSha256)
				}
			}
			if bytes.Equal(encoded, input) {
				if strictErr != nil {
					t.Fatalf("ParseStrict rejected canonical input: %v", strictErr)
				}
				again, err := Encode(strictValue, opts)
				if err != nil || !bytes.Equal(again, encoded) {
					t.Fatalf("ParseStrict value re-encodes differently: %v", err)
				}
			} else {
				var serr *Error
				if !errors.As(strictErr, &serr) || serr.Code != CodeNotCanonical {
					t.Fatalf("ParseStrict must report not_canonical for non-canonical input, got %v", strictErr)
				}
				if serr.Reason == "" || serr.Offset < 0 || serr.Offset > len(input) {
					t.Fatalf("not_canonical must carry reason and offset: %+v", serr)
				}
			}
		})
	}
}

// TestWireGolden locks the sdk2-wire-v1 metadata bytes: encode(value) == utf8, ParseStrict(utf8) round
// trips, every invalidMetadata sample is rejected, and the SSE golden data line is canonical.
func TestWireGolden(t *testing.T) {
	var fixture struct {
		Metadata []struct {
			ID    string `json:"id"`
			Value any    `json:"value"`
			UTF8  string `json:"utf8"`
		} `json:"metadata"`
		InvalidMetadata []string `json:"invalidMetadata"`
		SSE             struct {
			Frame any    `json:"frame"`
			UTF8  string `json:"utf8"`
		} `json:"sse"`
	}
	decodeJSON(t, readContract(t, "sdk2-wire-v1.json"), &fixture)
	opts := Options{MaxBytes: defaultMaxBytes}
	for _, m := range fixture.Metadata {
		encoded, err := Encode(m.Value, opts)
		if err != nil {
			t.Fatalf("%s: Encode: %v", m.ID, err)
		}
		if string(encoded) != m.UTF8 {
			t.Fatalf("%s: bytes differ\n got %s\nwant %s", m.ID, encoded, m.UTF8)
		}
		parsed, err := ParseStrict([]byte(m.UTF8), opts)
		if err != nil {
			t.Fatalf("%s: ParseStrict: %v", m.ID, err)
		}
		again, err := Encode(parsed, opts)
		if err != nil || string(again) != m.UTF8 {
			t.Fatalf("%s: round trip differs: %v %s", m.ID, err, again)
		}
	}
	for _, sample := range fixture.InvalidMetadata {
		if _, err := Decode([]byte(sample), opts); err == nil {
			t.Fatalf("Decode accepted invalid metadata %s", sample)
		}
		if _, err := ParseStrict([]byte(sample), opts); err == nil {
			t.Fatalf("ParseStrict accepted invalid metadata %s", sample)
		}
	}
	_, data, ok := strings.Cut(fixture.SSE.UTF8, "data: ")
	if !ok {
		t.Fatal("sse golden has no data line")
	}
	data = strings.TrimSuffix(data, "\n\n")
	if _, err := ParseStrict([]byte(data), opts); err != nil {
		t.Fatalf("sse data line is not canonical: %v", err)
	}
	frame, _ := fixture.SSE.Frame.(map[string]any)
	delete(frame, "protocol")
	frame["protocol"] = "sdk2-ext-v1"
	encoded, err := Encode(frame, opts)
	if err != nil || string(encoded) != data {
		t.Fatalf("sse frame encodes differently: %v\n got %s\nwant %s", err, encoded, data)
	}
}

// TestClosureDigest recomputes the capability-closure ids of the unified-v1 golden (closure.ts
// computeClosure products): closureId = SHA256("tansr.unified.closure.v1" ‖ 0x00 ‖ canonical({authorizationRevision, domains, operations})).
func TestClosureDigest(t *testing.T) {
	var golden struct {
		Vectors []struct {
			Name       string         `json:"name"`
			Definition string         `json:"definition"`
			Expect     string         `json:"expect"`
			Value      map[string]any `json:"value"`
		} `json:"vectors"`
	}
	decodeJSON(t, readContract(t, "unified-v1.golden.json"), &golden)
	checked := 0
	for _, v := range golden.Vectors {
		if v.Definition != "CapabilityClosure" || v.Expect != "valid" {
			continue
		}
		body := map[string]any{
			"authorizationRevision": v.Value["authorizationRevision"],
			"domains":               v.Value["domains"],
			"operations":            v.Value["operations"],
		}
		encoded, err := Encode(body, Options{MaxBytes: defaultMaxBytes})
		if err != nil {
			t.Fatalf("%s: Encode: %v", v.Name, err)
		}
		digest, err := DomainDigest(DomainClosure, encoded)
		if err != nil {
			t.Fatalf("%s: DomainDigest: %v", v.Name, err)
		}
		if got := Hex(digest); got != v.Value["closureId"] {
			t.Fatalf("%s: closureId %s, want %v", v.Name, got, v.Value["closureId"])
		}
		checked++
	}
	if checked < 2 {
		t.Fatalf("expected at least two closure vectors, checked %d", checked)
	}
	if _, err := DomainDigest("", []byte("x")); err == nil {
		t.Fatal("empty domain must be rejected")
	}
	if _, err := DomainDigest("a\x00b", []byte("x")); err == nil {
		t.Fatal("domain with U+0000 must be rejected")
	}
	if _, err := DomainDigest("d", nil); err == nil {
		t.Fatal("nil data must be rejected")
	}
}

func codeOf(t *testing.T, err error) *Error {
	t.Helper()
	var cerr *Error
	if !errors.As(err, &cerr) {
		t.Fatalf("expected *Error, got %v", err)
	}
	return cerr
}

func TestDecodeErrorLocations(t *testing.T) {
	opts := Options{MaxBytes: defaultMaxBytes}
	cases := []struct {
		input  string
		code   Code
		path   string
		offset int
	}{
		{`{"a":1,"a":2}`, CodeDuplicateKey, "/a", 7},
		{`{"a":{"b":[1,-1]}}`, CodeInvalidNumber, "/a/b/1", 13},
		{`[1e3]`, CodeInvalidNumber, "/0", 1},
		{`{"é":1}`, CodeInvalidKey, "/é", 1},
		{`{"a":1`, CodeUnexpectedEnd, "", 6},
		{`{} {}`, CodeTrailingData, "", 3},
		{`["\ud800"]`, CodeLoneSurrogate, "/0", 1},
		{`{"a/b":{"~":true}} `, CodeNotCanonical, "", 18},
		{"", CodeUnexpectedEnd, "", 0},
	}
	for _, c := range cases {
		_, err := ParseStrict([]byte(c.input), opts)
		cerr := codeOf(t, err)
		if cerr.Code != c.code || cerr.Path != c.path || cerr.Offset != c.offset {
			t.Errorf("%q: got %s %q %d, want %s %q %d", c.input, cerr.Code, cerr.Path, cerr.Offset, c.code, c.path, c.offset)
		}
	}
	// not_canonical reasons with member pointers (RFC 6901 escaping of ~ and /)
	_, err := ParseStrict([]byte(`{"a/b":1,"~":{"y":1,"x":2}}`), opts)
	cerr := codeOf(t, err)
	if cerr.Code != CodeNotCanonical || cerr.Reason != ReasonKeyOrder || cerr.Path != "/~0/x" || cerr.Offset != 20 {
		t.Fatalf("key_order location: %+v", cerr)
	}
	_, err = ParseStrict([]byte(`["\/"]`), opts)
	cerr = codeOf(t, err)
	if cerr.Code != CodeNotCanonical || cerr.Reason != ReasonEscape || cerr.Path != "/0" || cerr.Offset != 2 {
		t.Fatalf("escape location: %+v", cerr)
	}
	_, err = ParseStrict([]byte(`{"\u0061":1}`), opts)
	cerr = codeOf(t, err)
	if cerr.Reason != ReasonEscape || cerr.Path != "/a" {
		t.Fatalf("key escape location: %+v", cerr)
	}
	_, err = Decode([]byte(`[]`), Options{})
	if codeOf(t, err).Code != CodeInvalidOptions {
		t.Fatal("MaxBytes is required")
	}
	_, err = Decode([]byte(`[]`), Options{MaxBytes: 1, MaxDepth: 33})
	if codeOf(t, err).Code != CodeInvalidOptions {
		t.Fatal("MaxDepth may only tighten")
	}
	_, err = Decode([]byte(`[[0]]`), Options{MaxBytes: 16, MaxDepth: 1})
	if c := codeOf(t, err); c.Code != CodeDepthExceeded || c.Path != "/0/0" {
		t.Fatalf("tightened depth: %+v", c)
	}
	_, err = Decode([]byte(`[0,0]`), Options{MaxBytes: 16, MaxNodes: 2})
	if c := codeOf(t, err); c.Code != CodeNodesExceeded || c.Path != "/1" {
		t.Fatalf("tightened nodes: %+v", c)
	}
}

type sample struct {
	Name     string         `json:"name"`
	Count    int            `json:"count,omitempty"`
	Tags     []string       `json:"tags"`
	Meta     map[string]any `json:"meta,omitempty"`
	Ignored  string         `json:"-"`
	private  int            //nolint:unused
	Nested   *sample        `json:"nested,omitempty"`
	Embedded                //nolint:unused
}

type Embedded struct {
	Flag bool `json:"flag"`
}

func TestEncodeGoValues(t *testing.T) {
	opts := Options{MaxBytes: defaultMaxBytes}
	got, err := Encode(sample{Name: "n", Tags: nil, Ignored: "x", Nested: &sample{Name: "m", Count: 2, Tags: []string{"b", "a"}}, Embedded: Embedded{Flag: true}}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"flag":true,"name":"n","nested":{"count":2,"flag":false,"name":"m","tags":["b","a"]},"tags":null}`; string(got) != want {
		t.Fatalf("struct encoding\n got %s\nwant %s", got, want)
	}
	got, err = Encode(map[string]any{"b": json.Number("9007199254740991"), "a": float64(3), "c": uint8(7), "d": []any{nil, true}}, opts)
	if err != nil || string(got) != `{"a":3,"b":9007199254740991,"c":7,"d":[null,true]}` {
		t.Fatalf("mixed numbers: %v %s", err, got)
	}
	failures := []struct {
		value any
		code  Code
		path  string
	}{
		{map[string]any{"n": 1.5}, CodeInvalidNumber, "/n"},
		{map[string]any{"n": -1}, CodeInvalidNumber, "/n"},
		{[]any{float64(1 << 53)}, CodeUnsafeInteger, "/0"},
		{[]any{json.Number("1e3")}, CodeInvalidNumber, "/0"},
		{map[string]any{"": 1}, CodeInvalidKey, "/"},
		{map[string]any{"汉": 1}, CodeInvalidKey, "/汉"},
		{[]byte("x"), CodeUnsupportedValue, ""},
		{map[int]any{1: 1}, CodeUnsupportedValue, ""},
		{func() {}, CodeUnsupportedValue, ""},
		{"\xff", CodeInvalidUTF8, ""},
	}
	for _, f := range failures {
		_, err := Encode(f.value, opts)
		c := codeOf(t, err)
		if c.Code != f.code || c.Path != f.path || c.Offset != -1 {
			t.Errorf("%v: got %s %q %d, want %s %q -1", f.value, c.Code, c.Path, c.Offset, f.code, f.path)
		}
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if c := codeOf(t, mustErr(Encode(cyclic, opts))); c.Code != CodeCyclicReference || c.Path != "/self" {
		t.Fatalf("cycle: %+v", c)
	}
	loop := &sample{Name: "l"}
	loop.Nested = loop
	if c := codeOf(t, mustErr(Encode(loop, opts))); c.Code != CodeCyclicReference {
		t.Fatalf("pointer cycle: %+v", c)
	}
	if c := codeOf(t, mustErr(Encode(map[string]any{"a": "日本"}, Options{MaxBytes: 13}))); c.Code != CodeBytesExceeded {
		t.Fatalf("bytes budget: %+v", c)
	}
	if out, err := Encode(map[string]any{"a": "日本"}, Options{MaxBytes: 14}); err != nil || len(out) != 14 {
		t.Fatalf("bytes budget exact: %v %d", err, len(out))
	}
	deep := any(int64(0))
	for i := 0; i < 33; i++ {
		deep = []any{deep}
	}
	if c := codeOf(t, mustErr(Encode(deep, opts))); c.Code != CodeDepthExceeded {
		t.Fatalf("depth: %+v", c)
	}
}

func mustErr(_ []byte, err error) error { return err }
