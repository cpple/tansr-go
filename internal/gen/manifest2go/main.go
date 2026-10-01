// Command manifest2go renders api/operations_gen.go from the vendored server manifest.
//
// Usage (from the module root):
//
//	go run ./internal/gen/manifest2go                  # regenerate api/operations_gen.go
//	go run ./internal/gen/manifest2go -check           # exit 1 when the committed file is stale
//	go run ./internal/gen/manifest2go -manifest <path> -out <path>
//
// `go test ./api` performs the same -check (TestOperationsGenerated) and `go test ./api -run
// TestOperationsGenerated -update` rewrites the file, for hosts that refuse to start freshly built
// executables.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/cpple/tansr-go/internal/manifestgen"
)

func main() {
	manifestPath := flag.String("manifest", "contract/api-manifest.json", "path of api-manifest.json (tansr-api-manifest-v1)")
	outPath := flag.String("out", "api/operations_gen.go", "path of the generated Go file")
	check := flag.Bool("check", false, "do not write; exit 1 when the generated file differs from -out")
	flag.Parse()

	raw, err := os.ReadFile(*manifestPath)
	if err != nil {
		fatal("read manifest: %v", err)
	}
	source, err := manifestgen.Render(raw)
	if err != nil {
		fatal("%v", err)
	}
	if *check {
		current, err := os.ReadFile(*outPath)
		if err != nil {
			fatal("read %s: %v", *outPath, err)
		}
		if !bytes.Equal(current, source) {
			fatal("%s is stale: regenerate with `go run ./internal/gen/manifest2go`", *outPath)
		}
		fmt.Printf("%s is up to date\n", *outPath)
		return
	}
	if err := os.WriteFile(*outPath, source, 0o644); err != nil {
		fatal("write %s: %v", *outPath, err)
	}
	fmt.Printf("wrote %s\n", *outPath)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "manifest2go: "+format+"\n", args...)
	os.Exit(1)
}
