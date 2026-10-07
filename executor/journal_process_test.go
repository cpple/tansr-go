package executor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real child processes write the journal, exit normally, and reopen it in a
// second process. A durable claim without receipt models an interrupted host's
// uncertain outcome; it is deliberately not a test of forced power-loss recovery.
func TestFileJournalAcrossProcesses(t *testing.T) {
	const modeKey = "TANSR_GO_JOURNAL_PROCESS_TEST_MODE"
	const directoryKey = "TANSR_GO_JOURNAL_PROCESS_TEST_DIRECTORY"
	if mode := os.Getenv(modeKey); mode != "" {
		directory := os.Getenv(directoryKey)
		data, err := os.ReadFile(filepath.Join(directory, "operation.json"))
		if err != nil {
			t.Fatal(err)
		}
		var operation Operation
		if err = json.Unmarshal(data, &operation); err != nil {
			t.Fatal(err)
		}
		journal, err := NewFileJournal(filepath.Join(directory, "journal"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := journal.Close(); err != nil {
				t.Error(err)
			}
		}()
		calls := 0
		effect := func() error {
			calls++
			// O_EXCL makes a second effect observable across process boundaries;
			// an in-memory counter alone could silently reset on every restart.
			file, err := os.OpenFile(filepath.Join(directory, "side-effect"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			_, writeErr := file.WriteString("one synthetic effect\n")
			if writeErr == nil {
				writeErr = file.Sync()
			}
			closeErr := file.Close()
			if writeErr != nil {
				return writeErr
			}
			return closeErr
		}
		if mode == "write-inflight" {
			claim, err := journal.Claim(context.Background(), operation)
			if err != nil || !claim.Claimed || claim.Receipt != nil {
				t.Fatalf("initial claim failed: %+v %v", claim, err)
			}
			if err = effect(); err != nil {
				t.Fatal(err)
			}
			// Intentionally leave the original claim without a receipt. This
			// process still closes handles and exits through the normal test path.
			return
		}
		runner := testRunner(t, testClient(t, "http://127.0.0.1:1"), journal, func(context.Context, map[string]any) (any, error) {
			if err := effect(); err != nil {
				return nil, err
			}
			return testResult(), nil
		})
		receipt, err := runner.Execute(context.Background(), operation)
		if err != nil {
			t.Fatal(err)
		}
		switch mode {
		case "write-completed":
			if calls != 1 || receipt.Status != "completed" {
				t.Fatalf("initial effect did not complete once: calls=%d %+v", calls, receipt)
			}
			encoded, err := json.Marshal(receipt)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(directory, "original-receipt.json"), encoded, 0600); err != nil {
				t.Fatal(err)
			}
		case "read-completed":
			encoded, err := os.ReadFile(filepath.Join(directory, "original-receipt.json"))
			if err != nil {
				t.Fatal(err)
			}
			var original Receipt
			if err = json.Unmarshal(encoded, &original); err != nil {
				t.Fatal(err)
			}
			if calls != 0 || receipt.Status != "completed" || !equal(receipt, original) {
				t.Fatalf("completed operation reran or receipt changed: calls=%d %+v", calls, receipt)
			}
		case "read-inflight":
			if calls != 0 || receipt.Status != "unknown" || receipt.ErrorCode == nil || *receipt.ErrorCode != "execution_outcome_unknown" {
				t.Fatalf("uncertain effect was redone or became completed: calls=%d %+v", calls, receipt)
			}
		default:
			t.Fatalf("unknown subprocess mode %q", mode)
		}
		if strings.HasPrefix(mode, "read-") {
			replayed, err := runner.Execute(context.Background(), operation)
			if err != nil || calls != 0 || !equal(receipt, replayed) {
				t.Fatalf("second replay changed the durable outcome: calls=%d %+v %v", calls, replayed, err)
			}
		}
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"completed", "inflight"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			operation := testOperation(t)
			operation.ExpiresAt = time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
			setDigest(t, &operation)
			encoded, err := json.Marshal(operation)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(directory, "operation.json"), encoded, 0600); err != nil {
				t.Fatal(err)
			}
			for _, mode := range []string{"write-" + scenario, "read-" + scenario} {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				command := exec.CommandContext(ctx, executable, "-test.run=^TestFileJournalAcrossProcesses$", "-test.v")
				command.Env = append(os.Environ(), modeKey+"="+mode, directoryKey+"="+directory)
				output, err := command.CombinedOutput()
				cancel()
				if err != nil {
					t.Fatalf("journal child %s: %v\n%s", mode, err, output)
				}
				t.Logf("journal child mode=%s pid=%d exited successfully", mode, command.Process.Pid)
			}
			effect, err := os.ReadFile(filepath.Join(directory, "side-effect"))
			if err != nil || string(effect) != "one synthetic effect\n" {
				t.Fatalf("unexpected cross-process effect count: %q %v", effect, err)
			}
			journal, err := NewFileJournal(filepath.Join(directory, "journal"))
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			claim, err := journal.Claim(context.Background(), operation)
			expectedStatus := "completed"
			if scenario == "inflight" {
				expectedStatus = "unknown"
			}
			if err != nil || claim.Claimed || claim.Receipt == nil || claim.Receipt.Status != expectedStatus {
				t.Fatalf("child outcome was not durable: %+v %v", claim, err)
			}
		})
	}
}
