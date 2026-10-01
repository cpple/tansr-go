package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func errorsAs(err error, target **ClientError) bool { return errors.As(err, target) }

func mustOp(t *testing.T, name string) *Operation {
	t.Helper()
	op, ok := Lookup(name)
	if !ok {
		t.Fatalf("unknown operation %s", name)
	}
	return op
}

func TestInstantiatePath(t *testing.T) {
	op := mustOp(t, OpSessionToolResult)
	path, err := InstantiatePath(op, map[string]string{"id": "s 1?ä", "targetId": "t~1"})
	if err != nil || path != "/api/sessions/s%201%3F%C3%A4/tool-results/t~1" {
		t.Fatalf("path %q err %v", path, err)
	}
	bad := []map[string]string{
		{"id": "s"},                            // missing targetId
		{"id": "", "targetId": "t"},            // empty
		{"id": "s", "targetId": "t", "x": "1"}, // unknown
		{"id": "..", "targetId": "t"},
		{"id": "a/b", "targetId": "t"},
		{"id": "a\\b", "targetId": "t"},
		{"id": "a\x00", "targetId": "t"},
	}
	for _, params := range bad {
		_, err := InstantiatePath(op, params)
		var cerr *ClientError
		if !errors.As(err, &cerr) || cerr.Code != CodeInvalidParams {
			t.Fatalf("%v: expected invalid_params, got %v", params, err)
		}
	}
	// sdk2-wire golden path ids: encodeURIComponent parity
	raw, err := os.ReadFile(filepath.Join("..", "contract", "sdk2-wire-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		PathIDs []struct {
			ID      string `json:"id"`
			Encoded string `json:"segment"`
		} `json:"pathIds"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.PathIDs) == 0 {
		t.Fatal("no pathIds in sdk2-wire-v1 golden")
	}
	for _, p := range wire.PathIDs {
		if got := EncodeURIComponent(p.ID); got != p.Encoded {
			t.Errorf("EncodeURIComponent(%q) = %q, want %q", p.ID, got, p.Encoded)
		}
	}
}

func TestBuildQuery(t *testing.T) {
	// whitelist order + revision default injection
	op := mustOp(t, OpArchiveRecordsRead)
	q, err := BuildQuery(op, map[string]string{"limit": "10", "historyEpoch": "3"})
	if err != nil || q != "?protocol=sdk2-ext-v1&historyEpoch=3&limit=10" {
		t.Fatalf("query %q err %v", q, err)
	}
	q, err = BuildQuery(op, map[string]string{"protocol": "custom"})
	if err != nil || q != "?protocol=custom" {
		t.Fatalf("explicit value must win: %q %v", q, err)
	}
	_, err = BuildQuery(op, map[string]string{"foo": "1"})
	var cerr *ClientError
	if !errors.As(err, &cerr) || cerr.Code != CodeInvalidQuery {
		t.Fatalf("expected invalid_query, got %v", err)
	}
	// session.capabilities speaks sdk2-ext-v1 although registered under the session family
	q, _ = BuildQuery(mustOp(t, OpSessionCapabilities), nil)
	if q != "?protocol=sdk2-ext-v1" {
		t.Fatalf("session.capabilities default: %q", q)
	}
	// terminal families inject contract
	q, _ = BuildQuery(mustOp(t, OpTerminalConfigurationRead), nil)
	if q != "?contract=terminal-services-v1" {
		t.Fatalf("terminal default: %q", q)
	}
	// facade operations inject nothing and accept no keys
	q, _ = BuildQuery(mustOp(t, OpDiscoveryManifest), nil)
	if q != "" {
		t.Fatalf("facade query: %q", q)
	}
	// cache defaults
	q, _ = BuildQuery(mustOp(t, OpCacheBindingGet), nil)
	if q != "?protocol=sdk2-cache-v1" {
		t.Fatalf("cache default: %q", q)
	}
	// value escaping
	q, _ = BuildQuery(mustOp(t, OpSessionGet), map[string]string{"include": "a b&c"})
	if q != "?include=a+b%26c" {
		t.Fatalf("escaping: %q", q)
	}
}

func TestOperationTableInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, op := range Operations() {
		if seen[op.Name] {
			t.Fatalf("duplicate %s", op.Name)
		}
		seen[op.Name] = true
		if op.Path[:5] != "/api/" {
			t.Fatalf("%s: path %s is not under /api/", op.Name, op.Path)
		}
		if op.SSE != (op.Kind == "stream") {
			t.Fatalf("%s: sse/kind mismatch", op.Name)
		}
		if (op.Domain == "discovery") != op.IsFacade() {
			t.Fatalf("%s: facade/domain mismatch", op.Name)
		}
		if op.IsFacade() && op.UsesCanonicalBody() {
			t.Fatalf("%s: facade operations use plain JSON", op.Name)
		}
	}
	if !mustOp(t, OpArchiveAckCommit).UsesCanonicalBody() || mustOp(t, OpSessionMessageSend).UsesCanonicalBody() {
		t.Fatal("canonical body families")
	}
}
