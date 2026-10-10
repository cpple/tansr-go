package executor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A surviving permanent receipt is evidence of execution even when its claim
// was lost. Reopening must not turn that damaged pair into permission to run IO.
func TestFileJournalOrphanReceiptNeverClaimsAgain(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		name := "plaintext"
		if encrypted {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			open := func() (*FileJournal, error) {
				if encrypted {
					return NewEncryptedFileJournal(directory, journalOptions())
				}
				return NewFileJournal(directory)
			}
			j, err := open()
			if err != nil {
				t.Fatal(err)
			}
			op := testOperation(t)
			if claim, err := j.Claim(context.Background(), op); err != nil || !claim.Claimed {
				t.Fatal(claim, err)
			}
			receipt := receiptFor(op, "unknown", "execution_outcome_unknown", nil)
			if err = j.Complete(context.Background(), op, receipt); err != nil {
				t.Fatal(err)
			}
			if err = j.Close(); err != nil {
				t.Fatal(err)
			}
			key, err := journalKey(op)
			if err != nil {
				t.Fatal(err)
			}
			claimPath := filepath.Join(directory, key+".claim")
			receiptPath := filepath.Join(directory, key+".receipt")
			original, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(claimPath); err != nil {
				t.Fatal(err)
			}
			j, err = open()
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			for attempt := 0; attempt < 2; attempt++ {
				claim, err := j.Claim(context.Background(), op)
				if !errors.Is(err, ErrOutcomeUnknown) || claim.Claimed || claim.Receipt != nil {
					t.Fatalf("orphan receipt authorized execution: claim=%+v err=%v", claim, err)
				}
				if _, err = os.Stat(claimPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("claim recreated", err)
				}
				actual, err := os.ReadFile(receiptPath)
				if err != nil || !bytes.Equal(original, actual) {
					t.Fatal("permanent receipt changed", err)
				}
			}
		})
	}
}

func TestEncryptedJournalRejectsDamagedAndForeignReceiptWithoutRewriting(t *testing.T) {
	for _, damage := range []string{"truncated", "tag", "foreign-aad", "plaintext", "unknown-version"} {
		t.Run(damage, func(t *testing.T) {
			directory := t.TempDir()
			j, err := NewEncryptedFileJournal(directory, journalOptions())
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			op := testOperation(t)
			if _, err = j.Claim(context.Background(), op); err != nil {
				t.Fatal(err)
			}
			if err = j.Complete(context.Background(), op, receiptFor(op, "unknown", "execution_outcome_unknown", nil)); err != nil {
				t.Fatal(err)
			}
			key, _ := journalKey(op)
			path := filepath.Join(directory, key+".receipt")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			bad := append([]byte{}, original...)
			switch damage {
			case "truncated":
				bad = bad[:len(bad)-7]
			case "tag":
				bad[len(bad)-1] ^= 1
			case "foreign-aad":
				bad, err = os.ReadFile(filepath.Join(directory, key+".claim"))
				if err != nil {
					t.Fatal(err)
				}
			case "plaintext":
				bad = []byte(`{"digest":"untrusted-sensitive-marker"}`)
			case "unknown-version":
				bad[len("Tansr-Go-Journal/")] = '9'
			}
			if err = os.WriteFile(path, bad, 0600); err != nil {
				t.Fatal(err)
			}
			claim, err := j.Claim(context.Background(), op)
			if !errors.Is(err, ErrOutcomeUnknown) || claim.Claimed || claim.Receipt != nil {
				t.Fatalf("damaged receipt delivered: %+v %v", claim, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, bad) {
				t.Fatal("damaged source overwritten", err)
			}
			if err = os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			claim, err = j.Claim(context.Background(), op)
			if err != nil || claim.Claimed || claim.Receipt == nil || claim.Receipt.Status != "unknown" {
				t.Fatal("original permanent unknown lost", err)
			}
		})
	}
}
