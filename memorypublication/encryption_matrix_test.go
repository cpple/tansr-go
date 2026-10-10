package memorypublication

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/tansrai/tansr-go/executor"
)

func TestPublicationTemporarySnapshotsRemainEncrypted(t *testing.T) {
	opts, owner := fixture(t)
	body := []byte("PST-Go-A05-sensitive-publication-body")
	scans := 0
	opts.CurrentScope = func() (executor.Scope, error) {
		files, err := filepath.Glob(filepath.Join(filepath.Dir(opts.Path), ".memory.bin.*.tmp"))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range files {
			raw, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(raw, magic) {
				t.Fatal("temporary snapshot format")
			}
			for _, marker := range [][]byte{body, []byte(base64.StdEncoding.EncodeToString(body)), opts.Key, []byte("\"transfers\"")} {
				if bytes.Contains(raw, marker) {
					t.Fatal("plaintext in temporary publication snapshot")
				}
			}
			scans++
		}
		return owner.Scope, nil
	}
	store, err := OpenFileStore(opts)
	if err != nil {
		t.Fatal(err)
	}
	publish(t, store, "original", body, nil, owner)
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if scans < 4 {
		t.Fatalf("missing actual temp-file observations: %d", scans)
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(opts.Path), "*.tmp"))
	if err != nil || len(files) != 0 {
		t.Fatal("temporary snapshot not released", err)
	}
}
