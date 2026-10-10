package executor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/tansrai/tansr-go/canonical"
)

// FileJournal stores immutable claims and receipts in a host-controlled private directory.
// O_EXCL is the cross-process claim primitive; fsync happens before returning a successful claim
// or receipt. An incomplete/corrupt file fails closed, and pending records are never age-evicted.
// NewFileJournal preserves plaintext compatibility. NewEncryptedFileJournal protects all record bodies.
// Both require a host-controlled directory and backups; neither is an OS sandbox.
// Unix also syncs the containing directory. Windows uses File.Sync but has no portable directory
// flush through the Go standard library; power-loss durability there requires a host-supplied
// transactional Journal. FileJournal guarantees process-restart replay, not a Windows power-loss SLA.
type FileJournal struct {
	root       *os.Root
	mu         sync.Mutex
	closed     bool
	encryption *journalEncryption
	lock       *os.File
}

func NewFileJournal(directory string) (*FileJournal, error) { return openFileJournal(directory, nil) }
func openFileJournal(directory string, encryption *journalEncryption) (*FileJournal, error) {
	if !filepath.IsAbs(directory) {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalid
	}
	if encryption != nil {
		real, e := filepath.EvalSymlinks(directory)
		if e != nil {
			return nil, e
		}
		same := filepath.Clean(directory) == real
		if runtime.GOOS == "windows" {
			same = strings.EqualFold(filepath.Clean(directory), real)
		}
		if !same {
			return nil, ErrInvalid
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	j := &FileJournal{root: root, encryption: encryption}
	if encryption != nil {
		if err = j.openMigrationLock(); err != nil {
			root.Close()
			return nil, err
		}
		if err = j.lockJournal(); err != nil {
			j.Close()
			return nil, err
		}
		defer j.unlockJournal()
	}
	if err = j.initializeJournalMode(); err != nil {
		j.Close()
		return nil, err
	}
	return j, nil
}
func (j *FileJournal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	var lockErr error
	if j.lock != nil {
		lockErr = j.lock.Close()
	}
	return errors.Join(j.root.Close(), lockErr)
}

type journalClaim struct {
	Digest string `json:"digest"`
}

func journalKey(op Operation) (string, error) {
	if err := validateOperation(op); err != nil {
		return "", err
	}
	// Authorization revisions and connection leases are not part of the key: changing either may
	// not authorize re-execution of an existing operation ID with a new request digest.
	b, err := canonical.Encode([]any{op.Scope.ApplicationScopeID, op.Scope.EndUserID, op.Binding.Target.ExecutorID, op.OperationID}, canonical.Options{MaxBytes: 8192})
	if err != nil {
		return "", err
	}
	return canonical.Hex(sha256.Sum256(b)), nil
}
func (j *FileJournal) read(name string, out any) error {
	info, err := j.root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > controlBytes+128 {
		return ErrOutcomeUnknown
	}
	f, err := j.root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, controlBytes+129))
	if err != nil {
		return err
	}
	data, err = j.unprotect(name, data)
	if err != nil {
		return err
	}
	defer clear(data)
	if len(data) > controlBytes {
		return ErrOutcomeUnknown
	}
	if _, err = canonical.ParseStrict(data, canonical.Options{MaxBytes: controlBytes}); err != nil {
		return ErrOutcomeUnknown
	}
	if err = json.Unmarshal(data, out); err != nil {
		return ErrOutcomeUnknown
	}
	if j.encryption != nil {
		return j.encryption.identity.CheckAccess()
	}
	return nil
}
func (j *FileJournal) create(name string, value any) error {
	data, err := canonical.Encode(value, canonical.Options{MaxBytes: controlBytes})
	if err != nil {
		return err
	}
	defer clear(data)
	data, err = j.protect(name, data)
	if err != nil {
		return err
	}
	f, err := j.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	// Never delete a partially written claim/receipt: it is an uncertain durable fact, not a retry
	// token. A later open reports unknown instead of performing the tool again.
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if j.encryption != nil {
		if err := j.encryption.identity.CheckAccess(); err != nil {
			return errors.Join(ErrOutcomeUnknown, err)
		}
	}
	if runtime.GOOS != "windows" {
		directory, err := j.root.Open(".")
		if err != nil {
			return err
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
	return nil
}
func (j *FileJournal) Claim(ctx context.Context, op Operation) (ClaimResult, error) {
	if err := ctx.Err(); err != nil {
		return ClaimResult{}, err
	}
	key, err := journalKey(op)
	if err != nil {
		return ClaimResult{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return ClaimResult{}, os.ErrClosed
	}
	if err = j.lockJournal(); err != nil {
		return ClaimResult{}, err
	}
	defer j.unlockJournal()
	if err = j.checkJournal(op); err != nil {
		return ClaimResult{}, err
	}
	// A surviving receipt is a permanent execution fact, even if the claim is
	// missing or damaged. Never recreate its claim and authorize the tool again.
	if _, statErr := j.root.Lstat(key + ".claim"); errors.Is(statErr, os.ErrNotExist) {
		if _, receiptErr := j.root.Lstat(key + ".receipt"); !errors.Is(receiptErr, os.ErrNotExist) {
			return ClaimResult{}, errors.Join(ErrOutcomeUnknown, receiptErr)
		}
	} else if statErr != nil {
		return ClaimResult{}, statErr
	}
	err = j.create(key+".claim", journalClaim{Digest: op.Digest})
	if err == nil {
		return ClaimResult{Claimed: true}, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return ClaimResult{}, err
	}
	var claim journalClaim
	if err = j.read(key+".claim", &claim); err != nil {
		return ClaimResult{}, err
	}
	if claim.Digest != op.Digest {
		return ClaimResult{}, ErrConflict
	}
	var receipt Receipt
	if err = j.read(key+".receipt", &receipt); errors.Is(err, os.ErrNotExist) {
		return ClaimResult{}, nil
	} else if err != nil {
		return ClaimResult{}, err
	}
	if err = validateReceipt(op, receipt); err != nil {
		return ClaimResult{}, err
	}
	return ClaimResult{Receipt: &receipt}, nil
}
func (j *FileJournal) Complete(ctx context.Context, op Operation, receipt Receipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := journalKey(op)
	if err != nil {
		return err
	}
	if err = validateReceipt(op, receipt); err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return os.ErrClosed
	}
	if err = j.lockJournal(); err != nil {
		return err
	}
	defer j.unlockJournal()
	if err = j.checkJournal(op); err != nil {
		return err
	}
	var claim journalClaim
	if err = j.read(key+".claim", &claim); err != nil {
		return err
	}
	if claim.Digest != op.Digest {
		return ErrConflict
	}
	err = j.create(key+".receipt", receipt)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	var previous Receipt
	if err = j.read(key+".receipt", &previous); err != nil {
		return err
	}
	if !equal(previous, receipt) {
		return ErrConflict
	}
	return nil
}
