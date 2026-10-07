package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONTransportLimits(t *testing.T) {
	for _, invalid := range []string{
		"{\"text\":\"\xff\"}",
		`{"value":1e999}`,
		strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65),
		"[" + strings.Repeat("0,", 200000) + "0]",
	} {
		if _, err := DecodeJSON([]byte(invalid)); err == nil {
			t.Fatal("transport accepted invalid Unicode, unbounded JSON or an infinite number")
		}
	}
	value, err := DecodeJSON([]byte(`{ "signed": -1.25, "exponent": 1e2, "large": 9007199254740993, "text": "中文" }`))
	if err != nil {
		t.Fatalf("ordinary JSON was incorrectly restricted to control JSON: %v", err)
	}
	if value.(map[string]any)["large"].(json.Number).String() != "9007199254740993" {
		t.Fatal("transport rounded an opaque JSON number")
	}
	if _, err := DecodeJSON([]byte(strings.Repeat("[", 64) + "0" + strings.Repeat("]", 64))); err != nil {
		t.Fatalf("valid boundary rejected: %v", err)
	}
}
