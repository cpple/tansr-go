package executor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func migrationFixture(t *testing.T) (string, string, JournalMigrationOptions, []Operation, map[string]Receipt) {
	t.Helper()
	parent := t.TempDir()
	source, target := filepath.Join(parent, "original"), filepath.Join(parent, "target")
	options := JournalMigrationOptions{Source: journalOptions(), TargetKey: bytes.Repeat([]byte{9}, 32)}
	j, err := NewEncryptedFileJournal(source, options.Source)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	operations := []Operation{}
	receipts := map[string]Receipt{}
	for _, status := range []string{"pending", "completed", "unknown", "failed"} {
		op := testOperation(t)
		op.OperationID = status
		setDigest(t, &op)
		if claim, err := j.Claim(context.Background(), op); err != nil || !claim.Claimed {
			t.Fatal(claim, err)
		}
		operations = append(operations, op)
		if status == "pending" {
			continue
		}
		var receipt Receipt
		if status == "completed" {
			receipt = receiptFor(op, status, "", &Resource{Operation: "tool.invoke", Args: map[string]any{"resultJson": `{"status":"ok","content":[{"t":"text","text":"sensitive-original-memory"}]}`}})
		} else {
			receipt = receiptFor(op, status, "execution_outcome_unknown", nil)
		}
		if err = j.Complete(context.Background(), op, receipt); err != nil {
			t.Fatal(err)
		}
		receipts[op.OperationID] = receipt
	}
	return source, target, options, operations, receipts
}
func journalBytes(t *testing.T, directory string) map[string]string {
	t.Helper()
	names, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(directory, name.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[name.Name()] = string(raw)
	}
	return out
}
func TestEncryptedJournalRekeyPreservesOriginalKeysAndOutcomes(t *testing.T) {
	source, target, options, operations, receipts := migrationFixture(t)
	original := journalBytes(t, source)
	if err := RekeyEncryptedFileJournal(context.Background(), source, target, options); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, journalBytes(t, source)) {
		t.Fatal("source bytes changed")
	}
	targetOptions := options.Source
	targetOptions.Key = options.TargetKey
	for n := 0; n < 2; n++ {
		j, err := NewEncryptedFileJournal(target, targetOptions)
		if err != nil {
			t.Fatal(err)
		}
		for _, op := range operations {
			result, err := j.Claim(context.Background(), op)
			if err != nil || result.Claimed {
				t.Fatal(result, err)
			}
			if op.OperationID == "pending" {
				if result.Receipt != nil {
					t.Fatal("pending became receipt")
				}
			} else if result.Receipt == nil || !equal(*result.Receipt, receipts[op.OperationID]) {
				t.Fatal("receipt changed", result)
			}
		}
		changed := operations[0]
		changed.Scope.AuthorizationRevision = "2"
		setDigest(t, &changed)
		if _, err = j.Claim(context.Background(), changed); !errors.Is(err, ErrConflict) {
			t.Fatal("new authorization replayed original key", err)
		}
		if err = j.Close(); err != nil {
			t.Fatal(err)
		}
	}
	j, err := NewEncryptedFileJournal(target, targetOptions)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	calls := 0
	runner := testRunner(t, testClient(t, "http://127.0.0.1:1"), j, func(context.Context, map[string]any) (any, error) { calls++; return testResult(), nil })
	receipt, err := runner.Execute(context.Background(), operations[0])
	if err != nil || calls != 0 || receipt.Status != "unknown" {
		t.Fatal(receipt, err, calls)
	}
	again, err := runner.Execute(context.Background(), operations[0])
	if err != nil || !equal(receipt, again) || calls != 0 {
		t.Fatal(again, err, calls)
	}
	for _, raw := range journalBytes(t, target) {
		if bytes.Contains([]byte(raw), []byte("sensitive-original-memory")) {
			t.Fatal("plaintext receipt")
		}
	}
	if _, err = NewEncryptedFileJournal(target, options.Source); err == nil {
		t.Fatal("old key opened target")
	}
	if _, err = NewFileJournal(target); err == nil {
		t.Fatal("downgraded target")
	}
}
func TestEncryptedJournalRekeyRefusesUnsafeInputs(t *testing.T) {
	cases := []string{"existing-empty-target", "same-key", "wrong-key", "wrong-user", "missing-marker", "corrupt-receipt", "orphan-receipt", "unknown-file", "file-limit", "byte-limit", "canceled", "revoked", "inside-source"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			source, target, options, operations, _ := migrationFixture(t)
			ctx := context.Background()
			switch name {
			case "existing-empty-target":
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			case "same-key":
				options.TargetKey = options.Source.Key
			case "wrong-key":
				options.Source.Key = bytes.Repeat([]byte{1}, 32)
			case "wrong-user":
				options.Source.EndUserID = "other"
			case "missing-marker":
				if err := os.Remove(filepath.Join(source, ".journal-mode")); err != nil {
					t.Fatal(err)
				}
			case "corrupt-receipt":
				key, _ := journalKey(operations[1])
				if err := os.WriteFile(filepath.Join(source, key+".receipt"), []byte("partial"), 0600); err != nil {
					t.Fatal(err)
				}
			case "orphan-receipt":
				key, _ := journalKey(operations[1])
				if err := os.Remove(filepath.Join(source, key+".claim")); err != nil {
					t.Fatal(err)
				}
			case "unknown-file":
				if err := os.WriteFile(filepath.Join(source, "unknown"), []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			case "file-limit":
				options.MaxFiles = 1
			case "byte-limit":
				options.MaxBytes = 1
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "revoked":
				options.Source.CheckAccess = func() error { return errors.New("revoked") }
			case "inside-source":
				target = filepath.Join(source, "target")
			}
			original := journalBytes(t, source)
			if err := RekeyEncryptedFileJournal(ctx, source, target, options); err == nil {
				t.Fatal("unsafe migration succeeded")
			}
			if !reflect.DeepEqual(original, journalBytes(t, source)) {
				t.Fatal("source changed")
			}
			if name == "existing-empty-target" {
				names, err := os.ReadDir(target)
				if err != nil || len(names) != 0 {
					t.Fatal("existing target changed", names, err)
				}
			} else if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Fatal("failed migration published", err)
			}
		})
	}
}
func TestEncryptedJournalMigrationRetainsEncryptedStageAndRejectsLateCollision(t *testing.T) {
	for _, name := range []string{"revoked", "canceled", "late-empty-directory"} {
		t.Run(name, func(t *testing.T) {
			source, target, options, _, _ := migrationFixture(t)
			original := journalBytes(t, source)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			triggered := false
			options.Source.CheckAccess = func() error {
				stages, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".tansr-journal-import-*"))
				if len(stages) > 0 && !triggered {
					triggered = true
					if _, err := os.Lstat(target); !os.IsNotExist(err) {
						t.Fatal("partial target visible", err)
					}
					switch name {
					case "revoked":
						return errors.New("revoked")
					case "canceled":
						cancel()
					case "late-empty-directory":
						return os.Mkdir(target, 0700)
					}
				}
				if name == "revoked" && triggered {
					return errors.New("revoked")
				}
				return nil
			}
			err := RekeyEncryptedFileJournal(ctx, source, target, options)
			var migration *JournalMigrationError
			if !triggered || !errors.As(err, &migration) || migration.Published || migration.StagingDirectory == "" {
				t.Fatal("missing recovery information", err)
			}
			if _, err := os.Stat(migration.StagingDirectory); err != nil {
				t.Fatal("stage lost", err)
			}
			if !reflect.DeepEqual(original, journalBytes(t, source)) {
				t.Fatal("original lost")
			}
			if name == "late-empty-directory" {
				names, err := os.ReadDir(target)
				if err != nil || len(names) != 0 {
					t.Fatal("collision replaced", names, err)
				}
			} else if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Fatal("partial target", err)
			}
		})
	}
}
func TestEncryptedJournalMigrationLockProcess(t *testing.T) {
	if source := os.Getenv("TANSR_GO_MIGRATION_LOCK_SOURCE"); source != "" {
		j, err := NewEncryptedFileJournal(source, journalOptions())
		if err == nil {
			j.Close()
			t.Fatal("migration lock allowed child open")
		}
		return
	}
	source, target, options, operations, _ := migrationFixture(t)
	writer, err := NewEncryptedFileJournal(source, options.Source)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	triggered := false
	options.Source.CheckAccess = func() error {
		stages, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".tansr-journal-import-*"))
		if len(stages) == 0 || triggered {
			return nil
		}
		triggered = true
		if _, err := writer.Claim(context.Background(), operations[0]); err == nil {
			t.Fatal("active writer claimed during migration")
		}
		if err := writer.Complete(context.Background(), operations[0], receiptFor(operations[0], "unknown", "execution_outcome_unknown", nil)); err == nil {
			t.Fatal("active writer completed during migration")
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, executable, "-test.run=^TestEncryptedJournalMigrationLockProcess$", "-test.v")
		command.Env = append(os.Environ(), "TANSR_GO_MIGRATION_LOCK_SOURCE="+source)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("child: %v %s", err, output)
		}
		return nil
	}
	if err = RekeyEncryptedFileJournal(context.Background(), source, target, options); err != nil || !triggered {
		t.Fatal(err, triggered)
	}
	// The caller still owns cutover. The migration does not silently retire/rewrite source facts.
	if result, err := writer.Claim(context.Background(), operations[0]); err != nil || result.Claimed || result.Receipt != nil {
		t.Fatal(result, err)
	}
}

func TestEncryptedJournalMigrationPublishedErrorRetainsCompleteTarget(t *testing.T) {
	source, target, options, operations, receipts := migrationFixture(t)
	original := journalBytes(t, source)
	options.Source.CheckAccess = func() error {
		if _, err := os.Stat(target); err == nil {
			return errors.New("revoked after publish")
		}
		return nil
	}
	err := RekeyEncryptedFileJournal(context.Background(), source, target, options)
	var failure *JournalMigrationError
	if !errors.As(err, &failure) || !failure.Published || failure.TargetDirectory != target {
		t.Fatal("missing published recovery state", err)
	}
	if !reflect.DeepEqual(original, journalBytes(t, source)) {
		t.Fatal("original changed")
	}
	next := journalOptions()
	next.Key = options.TargetKey
	j, err := NewEncryptedFileJournal(target, next)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	result, err := j.Claim(context.Background(), operations[1])
	if err != nil || result.Receipt == nil || !equal(*result.Receipt, receipts[operations[1].OperationID]) {
		t.Fatal("published snapshot incomplete", result, err)
	}
	if err = RekeyEncryptedFileJournal(context.Background(), source, target, options); err == nil {
		t.Fatal("published target overwritten")
	}
}

func TestEncryptedJournalMigrationUpgradesOnlyLegacyLockMetadata(t *testing.T) {
	source, target, options, _, _ := migrationFixture(t)
	if err := os.Remove(filepath.Join(source, journalLockName)); err != nil {
		t.Fatal(err)
	}
	original := journalBytes(t, source)
	if err := RekeyEncryptedFileJournal(context.Background(), source, target, options); err != nil {
		t.Fatal(err)
	}
	after := journalBytes(t, source)
	if after[journalLockName] != "" {
		t.Fatal("lock contains data")
	}
	delete(after, journalLockName)
	if !reflect.DeepEqual(original, after) {
		t.Fatal("legacy source records changed")
	}
}
