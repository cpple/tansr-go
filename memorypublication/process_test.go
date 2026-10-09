package memorypublication

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestFileStoreProcessLockAndRecovery(t *testing.T) {
	if directory := os.Getenv("TANSR_GO_PUBLICATION_TEST_DIR"); directory != "" {
		opts, o := fixture(t)
		opts.Path = filepath.Join(directory, "memory.bin")
		opts.Mode = "reopen"
		if os.Getenv("TANSR_GO_PUBLICATION_TEST_ACTION") == "blocked" {
			if s, e := OpenFileStore(opts); e == nil {
				s.Close()
				t.Fatal("cross process lock ignored")
			}
			return
		}
		s, e := OpenFileStore(opts)
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close()
		if got := execute(t, s, transferRequest("query", "original"), o); status(got) != "staging" {
			t.Fatal(got)
		}
		body := []byte("subprocess-secret")
		execute(t, s, chunk("original", body, 0), o)
		execute(t, s, transferRequest("commit", "original"), o)
		return
	}
	opts, o := fixture(t)
	s, e := OpenFileStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	body := []byte("subprocess-secret")
	execute(t, s, begin("original", body, nil), o)
	run := func(action string) {
		t.Helper()
		exe, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestFileStoreProcessLockAndRecovery$", "-test.v")
		cmd.Env = append(os.Environ(), "TANSR_GO_PUBLICATION_TEST_DIR="+filepath.Dir(opts.Path), "TANSR_GO_PUBLICATION_TEST_ACTION="+action)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("child: %v %s", e, out)
		}
	}
	run("blocked")
	s.Close()
	run("commit")
	opts.Mode = "reopen"
	s, e = OpenFileStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if got := execute(t, s, transferRequest("query", "original"), o); status(got) != "committed" {
		t.Fatal(got)
	}
	raw, _ := os.ReadFile(opts.Path)
	if bytes.Contains(raw, body) {
		t.Fatal("plaintext child write")
	}
}
func TestRekeyPreservesPendingAndTerminalFacts(t *testing.T) {
	opts, o := fixture(t)
	s, e := OpenFileStore(opts)
	if e != nil {
		t.Fatal(e)
	}
	body := []byte("old-key-memory")
	publish(t, s, "committed", body, nil, o)
	execute(t, s, begin("staged", body, hash(body)), o)
	s.Close()
	original, _ := os.ReadFile(opts.Path)
	target := filepath.Join(filepath.Dir(opts.Path), "rotated.bin")
	key := bytes.Repeat([]byte{9}, 32)
	s, e = Rekey(opts, target, key)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for id, want := range map[string]string{"committed": "committed", "staged": "staging"} {
		if got := status(execute(t, s, transferRequest("query", id), o)); got != want {
			t.Fatal(got, want)
		}
	}
	after, _ := os.ReadFile(opts.Path)
	if !bytes.Equal(original, after) {
		t.Fatal("source changed")
	}
	if _, e = Rekey(opts, target, key); e == nil {
		t.Fatal("target replaced")
	}
}
