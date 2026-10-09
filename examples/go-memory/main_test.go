package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestReopenDefaultAndMissingInputs(t *testing.T) {
	var o options
	f := flag.NewFlagSet("go-memory", flag.ContinueOnError)
	flags(f, &o)
	if e := f.Parse(nil); e != nil {
		t.Fatal(e)
	}
	if o.mode != "reopen" {
		t.Fatal("unsafe create default")
	}
	if e := run(context.Background(), o); e == nil {
		t.Fatal("missing identity accepted")
	}
}
func TestHostConfigurationRejectsAmbiguousAndUnknownFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "scope.json")
	for _, raw := range []string{`{"v":"a","v":"b"}`, `{"unexpected":"x"}`} {
		if e := os.WriteFile(p, []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
		var v struct {
			V string `json:"v"`
		}
		if e := readJSON(p, &v); e == nil {
			t.Fatal("ambiguous host configuration")
		}
	}
}
