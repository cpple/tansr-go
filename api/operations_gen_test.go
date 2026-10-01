package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpple/tansr-go/internal/manifestgen"
)

var update = flag.Bool("update", false, "rewrite operations_gen.go from contract/api-manifest.json")

// TestOperationsGenerated is the generator's -check gate: operations_gen.go must equal the rendering
// of the vendored manifest byte for byte, and the embedded source hash must match the file.
func TestOperationsGenerated(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "contract", "api-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := manifestgen.Render(raw)
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		if err := os.WriteFile("operations_gen.go", want, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("operations_gen.go rewritten")
	}
	got, err := os.ReadFile("operations_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("operations_gen.go is stale: run `go run ./internal/gen/manifest2go` (or `go test ./api -run TestOperationsGenerated -update`)")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ManifestSourceSHA256 {
		t.Fatalf("ManifestSourceSHA256 %s differs from manifest file %s", ManifestSourceSHA256, hex.EncodeToString(sum[:]))
	}
	if len(operations) != 80 {
		t.Fatalf("expected 80 operations, got %d", len(operations))
	}
	if len(ClosureOperationNames()) != 76 {
		t.Fatalf("expected 76 closure operations, got %d", len(ClosureOperationNames()))
	}
}
