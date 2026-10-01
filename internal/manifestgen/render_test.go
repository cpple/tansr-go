package manifestgen

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "write api/operations_gen.go from contract/api-manifest.json")

func TestRender(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "contract", "api-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := Render(raw)
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, want := range []string{"package api", "const ManifestRevision = 6", `Path: "/api/sessions/:id/tool-results/:targetId", Params: []string{"id", "targetId"}`} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered source lacks %q", want)
		}
	}
	if !regexp.MustCompile(`OpDiscoveryManifest\s+= "discovery\.manifest"`).MatchString(text) {
		t.Fatal("rendered source lacks the OpDiscoveryManifest constant")
	}
	if strings.Contains(text, "/v2/") || strings.Contains(text, "/v3/") {
		t.Fatal("rendered source must not contain legacy prefixes")
	}
	if *update {
		out := filepath.Join("..", "..", "api", "operations_gen.go")
		if err := os.WriteFile(out, source, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", out)
	}
}

func TestConstName(t *testing.T) {
	cases := map[string]string{
		"discovery.manifest":               "OpDiscoveryManifest",
		"session.message.send":             "OpSessionMessageSend",
		"terminal.executor.events.observe": "OpTerminalExecutorEventsObserve",
		"cache.core.diagnostics":           "OpCacheCoreDiagnostics",
	}
	for in, want := range cases {
		if got := ConstName(in); got != want {
			t.Errorf("ConstName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := strings.Join(TemplateParams("/api/archive/bindings/:id/materials/:targetId/uploads/:uploadId/chunks"), ","); got != "id,targetId,uploadId" {
		t.Fatalf("TemplateParams: %s", got)
	}
}

func TestRenderRejectsInconsistentManifest(t *testing.T) {
	bad := `{"format":"tansr-api-manifest-v1","contract":"unified-v1","revision":1,"schemaHash":"x","operations":[{"name":"a.b","domain":"session","family":"f","method":"GET","apiPath":"/v2/x","aliases":[],"kind":"read","sse":false,"query":[]}]}`
	if _, err := Render([]byte(bad)); err == nil {
		t.Fatal("legacy apiPath must be rejected")
	}
	bad = `{"format":"tansr-api-manifest-v1","contract":"unified-v1","revision":1,"schemaHash":"x","operations":[{"name":"a.b","domain":"session","family":"f","method":"GET","apiPath":"/api/x","aliases":[],"kind":"stream","sse":false,"query":[]}]}`
	if _, err := Render([]byte(bad)); err == nil {
		t.Fatal("kind/sse mismatch must be rejected")
	}
	if _, err := Render([]byte(`{"format":"other"}`)); err == nil {
		t.Fatal("unknown format must be rejected")
	}
}
