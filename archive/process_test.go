package archive

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A normal process exit, followed by a different OS process, must preserve both
// the encrypted body and the original unconfirmed ACK. This does not simulate
// abrupt power loss or establish a filesystem's power-loss durability guarantees.
func TestFileStoreAcrossProcesses(t *testing.T) {
	const modeKey = "TANSR_GO_ARCHIVE_PROCESS_TEST_MODE"
	const pathKey = "TANSR_GO_ARCHIVE_PROCESS_TEST_PATH"
	binding, status, page, objects := fixture(t)
	identity, err := IdentityFrom(binding, status)
	if err != nil {
		t.Fatal(err)
	}
	options := StoreOptions{
		Path: os.Getenv(pathKey), Key: bytes.Repeat([]byte{19}, 32), Identity: identity,
		CheckAccess: func(actual Identity) error {
			if actual != identity {
				return ErrIntegrity
			}
			return nil
		},
	}
	request := RequestIdentity{RequestID: "cross-process-original-ack", OperationEpoch: binding.OperationEpoch.ID}

	if mode := os.Getenv(modeKey); mode != "" {
		store, err := OpenFileStore(options)
		if mode == "locked" {
			if err == nil {
				_ = store.Close()
				t.Fatal("another process acquired the live archive lock")
			}
			if !strings.Contains(err.Error(), "store is already in use") {
				t.Fatalf("rejected for a reason other than lock ownership: %v", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := store.Close(); err != nil {
				t.Error(err)
			}
		}()
		switch mode {
		case "write":
			ack, err := store.Receive(binding, status, page, objects, request)
			if err != nil {
				t.Fatal(err)
			}
			if ack.Request != request {
				t.Fatal("prepared ACK changed identity")
			}
			if coverage, err := store.Coverage(); err != nil || coverage != nil {
				t.Fatalf("unconfirmed local write advanced coverage: %+v %v", coverage, err)
			}
		case "read-confirm":
			ack, err := store.Pending()
			if err != nil || ack == nil || ack.Request != request || ack.ExpectedRevision != binding.Revision {
				t.Fatalf("new process lost or replaced pending ACK: %+v %v", ack, err)
			}
			if coverage, err := store.Coverage(); err != nil || coverage != nil {
				t.Fatalf("pending ACK became accepted across restart: %+v %v", coverage, err)
			}
			body, err := store.Body(page.Records[0].Payload)
			if err != nil || !bytes.Equal(body, objects["artifact-1"]) {
				t.Fatalf("raw artifact bytes changed across process restart: %v", err)
			}
			if err = store.Confirm(receiptFor(t, identity, *ack)); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unknown subprocess mode %q", mode)
		}
		return
	}

	// macOS may expose TMPDIR through /var -> /private/var. Pass the canonical
	// test-owned directory, rather than relaxing the store's no-symlink contract.
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	options.Path = filepath.Join(directory, "archive.bin")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	run := func(mode string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, executable, "-test.run=^TestFileStoreAcrossProcesses$", "-test.v")
		command.Env = append(os.Environ(), modeKey+"="+mode, pathKey+"="+options.Path)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("archive child %s: %v\n%s", mode, err, output)
		}
		t.Logf("archive child mode=%s pid=%d exited successfully", mode, command.Process.Pid)
	}
	run("write")
	sealed, err := os.ReadFile(options.Path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, objects["artifact-1"]) || bytes.Contains(sealed, []byte(request.RequestID)) {
		t.Fatal("child persisted plaintext instead of an encrypted archive")
	}
	held, err := OpenFileStore(options)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	run("locked")
	if err = held.Close(); err != nil {
		t.Fatal(err)
	}
	run("read-confirm")
	final, err := OpenFileStore(options)
	if err != nil {
		t.Fatal(err)
	}
	defer final.Close()
	if pending, err := final.Pending(); err != nil || pending != nil {
		t.Fatalf("completed ACK remained pending: %+v %v", pending, err)
	}
	coverage, err := final.Coverage()
	if err != nil || coverage == nil || coverage.ThroughSequence != "1" || coverage.HeadDigest != page.Records[0].RecordDigest {
		t.Fatalf("child confirmation was not durable: %+v %v", coverage, err)
	}
	body, err := final.Body(page.Records[0].Payload)
	if err != nil || !bytes.Equal(body, objects["artifact-1"]) {
		t.Fatalf("confirmation changed archived body: %v", err)
	}
}
