package main

import (
	"context"
	"strings"
	"testing"
)

func TestArchiveKeyAndInvalidOptionsFailBeforeNetwork(t *testing.T) {
	for _, value := range []string{"", "secret", strings.Repeat("x", 64), strings.Repeat("00", 31)} {
		t.Setenv("TANSR_ARCHIVE_KEY", value)
		if _, err := archiveKey(); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
	t.Setenv("TANSR_ARCHIVE_KEY", strings.Repeat("ab", 32))
	key, err := archiveKey()
	if err != nil || len(key) != 32 {
		t.Fatal(err)
	}
	for _, opts := range []options{{}, {path: "data", session: "s", binding: "b", maxPages: 1}, {path: "data", session: "s", maxPages: 0}} {
		if err := run(context.Background(), opts); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
}
