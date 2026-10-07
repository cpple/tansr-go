package demoutil

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenFileRenewalDoesNotCacheOldCredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user-token")
	t.Setenv("TANSR_TOKEN_FILE", path)
	t.Setenv("TANSR_TOKEN", "old-env-token")
	for _, value := range []string{"first-user-token", "renewed-user-token"} {
		if err := os.WriteFile(path, []byte(value+"\r\n"), 0600); err != nil {
			t.Fatal(err)
		}
		actual, err := token(context.Background())
		if err != nil || actual != value {
			t.Fatalf("unexpected credential result: %v", err)
		}
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 8193)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := token(context.Background()); err == nil {
		t.Fatal("unbounded token accepted")
	}
}

func TestDisplayRemovesTerminalControls(t *testing.T) {
	text := Text("answer\x1b[2J\x00\nnext\tline")
	if strings.ContainsAny(text, "\x1b\x00") || !strings.Contains(text, "\nnext\tline") {
		t.Fatal("unsafe rendering")
	}
}
