package executor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func journalOptions() JournalEncryption {
	return JournalEncryption{Key: bytes.Repeat([]byte{7}, 32), ApplicationScopeID: "app", EndUserID: "user", ExecutorID: "device", CheckAccess: func() error { return nil }}
}
func TestEncryptedJournalReopenAndSensitiveReceipt(t *testing.T) {
	directory := t.TempDir()
	opts := journalOptions()
	j, e := NewEncryptedFileJournal(directory, opts)
	if e != nil {
		t.Fatal(e)
	}
	op := testOperation(t)
	claim, e := j.Claim(context.Background(), op)
	if e != nil || !claim.Claimed {
		t.Fatal(claim, e)
	}
	receipt := receiptFor(op, "completed", "", &Resource{Operation: "tool.invoke", Args: map[string]any{"resultJson": `{"status":"ok","content":[{"t":"text","text":"sensitive-publication-body"}]}`}})
	if e = j.Complete(context.Background(), op, receipt); e != nil {
		t.Fatal(e)
	}
	j.Close()
	j, e = NewEncryptedFileJournal(directory, opts)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	claim, e = j.Claim(context.Background(), op)
	if e != nil || claim.Receipt == nil || !equal(*claim.Receipt, receipt) {
		t.Fatal(claim, e)
	}
	paths, _ := filepath.Glob(filepath.Join(directory, "*"))
	for _, p := range paths {
		raw, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		if bytes.Contains(raw, []byte("sensitive-publication-body")) {
			t.Fatal("plaintext receipt")
		}
	}
	if _, e = NewFileJournal(directory); e == nil {
		t.Fatal("downgrade opened")
	}
	wrong := opts
	wrong.Key = bytes.Repeat([]byte{8}, 32)
	if _, e = NewEncryptedFileJournal(directory, wrong); e == nil {
		t.Fatal("wrong key")
	}
	wrong = opts
	wrong.EndUserID = "other"
	if _, e = NewEncryptedFileJournal(directory, wrong); e == nil {
		t.Fatal("wrong identity")
	}
	key, _ := journalKey(op)
	raw, e := os.ReadFile(filepath.Join(directory, key+".receipt"))
	if e != nil {
		t.Fatal(e)
	}
	raw[len(raw)-1] ^= 1
	os.WriteFile(filepath.Join(directory, key+".receipt"), raw, 0600)
	if _, e = j.Claim(context.Background(), op); !errors.Is(e, ErrOutcomeUnknown) {
		t.Fatal("tamper", e)
	}
}
func TestEncryptedJournalRejectsRevocationAndLegacyMigration(t *testing.T) {
	directory := t.TempDir()
	legacy, e := NewFileJournal(directory)
	if e != nil {
		t.Fatal(e)
	}
	legacy.Close()
	if _, e = NewEncryptedFileJournal(directory, journalOptions()); e == nil {
		t.Fatal("implicit migration")
	}
	opts := journalOptions()
	allowed := true
	opts.CheckAccess = func() error {
		if !allowed {
			return errors.New("revoked")
		}
		return nil
	}
	j, e := NewEncryptedFileJournal(t.TempDir(), opts)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	op := testOperation(t)
	if _, e = j.Claim(context.Background(), op); e != nil {
		t.Fatal(e)
	}
	allowed = false
	if _, e = j.Claim(context.Background(), op); e == nil {
		t.Fatal("revoked claim")
	}
	if e = j.Complete(context.Background(), op, receiptFor(op, "unknown", "execution_outcome_unknown", nil)); e == nil {
		t.Fatal("revoked complete")
	}
}

type publicationTestHost struct{ calls int }

func (h *publicationTestHost) RequiresEncryptedJournal() bool { return true }
func (h *publicationTestHost) Execute(_ context.Context, op Operation, args map[string]any) (any, error) {
	h.calls++
	if op.ToolName != "MemoryPublication" || args["action"] != "head" {
		return nil, ErrInvalid
	}
	return testResult(), nil
}
func TestRunnerReservedPublicationEncryptedJournalAndReplay(t *testing.T) {
	opts := journalOptions()
	j, e := NewEncryptedFileJournal(t.TempDir(), opts)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	h := &publicationTestHost{}
	registration := testRegistration()
	registration.Tools = []ToolDefinition{{MemoryPublicationToolName, MemoryPublicationDefinitionDigest}}
	options := RunnerOptions{Client: testClient(t, "http://127.0.0.1:1"), Registration: registration, Journal: j, MemoryPublication: h, Authorize: func(context.Context, Operation) error { return nil }}
	runner, e := NewRunner(options)
	if e != nil {
		t.Fatal(e)
	}
	connection := testConnection()
	runner.connection = &connection
	op := testOperation(t)
	op.ToolName = "MemoryPublication"
	op.Request.Args = map[string]any{"name": MemoryPublicationToolName, "definitionDigest": MemoryPublicationDefinitionDigest, "argsJson": `{"contract":"terminal-services-v1","action":"head","sourceId":"source","sourceGeneration":"1","domainKey":"domain"}`}
	setDigest(t, &op)
	receipt, e := runner.Execute(context.Background(), op)
	if e != nil || receipt.Status != "completed" {
		t.Fatal(receipt, e)
	}
	replayed, e := runner.Execute(context.Background(), op)
	if e != nil || !equal(receipt, replayed) || h.calls != 1 {
		t.Fatal(replayed, e, h.calls)
	}
	plaintext := newJournal(t)
	options.Journal = plaintext
	if _, e = NewRunner(options); !errors.Is(e, ErrUnsupported) {
		t.Fatal("plaintext accepted", e)
	}
	options.Journal = j
	options.MemoryPublication = nil
	options.Tools = map[string]Tool{MemoryPublicationToolName: {DefinitionDigest: MemoryPublicationDefinitionDigest, Handle: func(context.Context, map[string]any) (any, error) { return nil, nil }}}
	if _, e = NewRunner(options); !errors.Is(e, ErrUnsupported) {
		t.Fatal("business handler installed", e)
	}
	op.ToolName = "Lookup"
	setDigest(t, &op)
	if _, e = runner.Execute(context.Background(), op); e == nil {
		t.Fatal("borrowed permission")
	}
}

func TestMissingJournalMarkerNeverDowngradesEncryptedRecords(t *testing.T) {
	directory := t.TempDir()
	j, e := NewEncryptedFileJournal(directory, journalOptions())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = j.Claim(context.Background(), testOperation(t)); e != nil {
		t.Fatal(e)
	}
	j.Close()
	if e = os.Remove(filepath.Join(directory, ".journal-mode")); e != nil {
		t.Fatal(e)
	}
	if _, e = NewFileJournal(directory); e == nil {
		t.Fatal("missing marker downgraded encrypted journal")
	}
	if _, e = os.Stat(filepath.Join(directory, ".journal-mode")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("missing identity rebuilt")
	}
}
