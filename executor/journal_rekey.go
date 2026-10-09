package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"

	"github.com/tansrai/tansr-go/canonical"
)

// JournalMigrationOptions keeps the original authenticated identity and requires
// a different 32-byte key. Zero limits default to 65536 records and 1 GiB total
// ciphertext. Limits reject oversized sources; they never evict durable facts.
type JournalMigrationOptions struct {
	Source    JournalEncryption
	TargetKey []byte
	MaxFiles  int
	MaxBytes  int64
}

// JournalMigrationError reports a retained encrypted staging directory or a
// completely published target requiring reconciliation. It never authorizes
// automatic cleanup, overwriting, or resuming execution in either copy.
type JournalMigrationError struct {
	StagingDirectory string
	TargetDirectory  string
	Published        bool
	Err              error
}

func (e *JournalMigrationError) Error() string {
	return fmt.Sprintf("executor: journal migration incomplete (published=%t): %v", e.Published, e.Err)
}
func (e *JournalMigrationError) Unwrap() error { return e.Err }

// RekeyEncryptedFileJournal explicitly copies an existing encrypted journal to
// an absent directory. All original claim keys and complete receipts are retained;
// pending and unknown outcomes never become permission to execute again.
// Stop and drain every writer first. This version's encrypted constructors,
// Claim and Complete are excluded by a cross-process lock during migration;
// older/noncooperating writers are outside that guarantee. After success the host
// must switch once, never write to both copies. Publication migration is separate.
// The first visible target is the complete verified snapshot. The source record
// bytes are never modified; opening an older source may add an empty lock file.
func RekeyEncryptedFileJournal(ctx context.Context, sourceDirectory, targetDirectory string, options JournalMigrationOptions) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if !filepath.IsAbs(sourceDirectory) || !filepath.IsAbs(targetDirectory) || len(options.TargetKey) != 32 || bytes.Equal(options.Source.Key, options.TargetKey) {
		return ErrInvalid
	}
	sourceDirectory, targetDirectory = filepath.Clean(sourceDirectory), filepath.Clean(targetDirectory)
	if options.MaxFiles == 0 {
		options.MaxFiles = 65536
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = 1 << 30
	}
	if options.MaxFiles < 1 || options.MaxBytes < 1 {
		return ErrInvalid
	}
	sourceInfo, err := migrationDirectory(sourceDirectory)
	if err != nil {
		return err
	}
	parent := filepath.Dir(targetDirectory)
	parentInfo, err := migrationDirectory(parent)
	if err != nil {
		return err
	}
	// Staging must never be part of the source being enumerated.
	relative, err := filepath.Rel(sourceDirectory, parent)
	if err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))) {
		return ErrInvalid
	}
	if err != nil && strings.EqualFold(filepath.VolumeName(sourceDirectory), filepath.VolumeName(parent)) {
		return err
	}
	if _, err = os.Lstat(targetDirectory); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	encryption, err := newJournalEncryption(options.Source)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(sourceDirectory)
	if err != nil {
		return err
	}
	source := &FileJournal{root: root, encryption: encryption}
	stage := ""
	published := false
	defer func() {
		err = errors.Join(err, source.Close())
		if err != nil && stage != "" {
			err = &JournalMigrationError{StagingDirectory: stage, TargetDirectory: targetDirectory, Published: published, Err: err}
		}
	}()
	// Do not initialize/rebuild a missing source marker, even in an empty directory.
	if err = source.verifyJournalMode(); err != nil {
		return err
	}
	if err = source.openMigrationLock(); err != nil {
		return err
	}
	if err = source.lockJournal(); err != nil {
		return err
	}
	defer source.unlockJournal()
	before, err := journalSnapshot(ctx, source, options)
	if err != nil {
		return err
	}
	targetOptions := options.Source
	targetOptions.Key = options.TargetKey
	stage, err = os.MkdirTemp(parent, ".tansr-journal-import-")
	if err != nil {
		return err
	}
	target, err := NewEncryptedFileJournal(stage, targetOptions)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, target.Close())
		}
	}()
	for _, entry := range before {
		if entry.Name == ".journal-mode" {
			continue
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		var raw json.RawMessage
		if err = source.read(entry.Name, &raw); err != nil {
			return err
		}
		value, decodeErr := canonical.ParseStrict(raw, canonical.Options{MaxBytes: controlBytes})
		clear(raw)
		if decodeErr != nil {
			return decodeErr
		}
		if err = target.create(entry.Name, value); err != nil {
			return err
		}
	}
	// Re-read all copied records before close and again through a fresh OS root.
	copied, err := journalSnapshot(ctx, target, options)
	if err != nil {
		return err
	}
	if !sameJournalPlaintext(before, copied) {
		return ErrConflict
	}
	err = target.Close()
	closed = true
	if err != nil {
		return err
	}
	target, err = NewEncryptedFileJournal(stage, targetOptions)
	if err != nil {
		return err
	}
	closed = false
	copied, err = journalSnapshot(ctx, target, options)
	if err != nil {
		return err
	}
	if !sameJournalPlaintext(before, copied) {
		return ErrConflict
	}
	err = target.Close()
	closed = true
	if err != nil {
		return err
	}
	after, err := journalSnapshot(ctx, source, options)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, after) {
		return ErrConflict
	}
	if err = sameMigrationDirectory(sourceDirectory, sourceInfo); err != nil {
		return err
	}
	if err = sameMigrationDirectory(parent, parentInfo); err != nil {
		return err
	}
	if err = options.Source.CheckAccess(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = publishJournalDirectory(stage, targetDirectory); err != nil {
		return err
	}
	published = true
	if runtime.GOOS != "windows" {
		d, openErr := os.Open(parent)
		if openErr != nil {
			return openErr
		}
		err = errors.Join(d.Sync(), d.Close())
		if err != nil {
			return err
		}
	}
	if err = sameMigrationDirectory(parent, parentInfo); err != nil {
		return err
	}
	if err = options.Source.CheckAccess(); err != nil {
		return err
	}
	return ctx.Err()
}

type journalSnapshotEntry struct {
	Name          string
	Plain, Cipher [32]byte
}

func journalSnapshot(ctx context.Context, journal *FileJournal, limits JournalMigrationOptions) ([]journalSnapshotEntry, error) {
	if err := journal.verifyJournalMode(); err != nil {
		return nil, err
	}
	dir, err := journal.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries := []journalSnapshotEntry{}
	claims := map[string]string{}
	type receiptWitness struct{ OperationID, ExecutorID, Digest string }
	receipts := map[string]receiptWitness{}
	var total int64
	for {
		names, readErr := dir.Readdirnames(128)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		for _, name := range names {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			if name == journalLockName {
				continue
			}
			if len(entries) >= limits.MaxFiles+1 {
				return nil, fmt.Errorf("%w: journal migration file limit", ErrInvalid)
			}
			info, statErr := journal.root.Lstat(name)
			if statErr != nil {
				return nil, statErr
			}
			if !info.Mode().IsRegular() || info.Size() > controlBytes+128 {
				return nil, ErrOutcomeUnknown
			}
			if info.Size() > limits.MaxBytes-total {
				return nil, fmt.Errorf("%w: journal migration byte limit", ErrInvalid)
			}
			total += info.Size()
			if name != ".journal-mode" {
				key, suffix, ok := strings.Cut(name, ".")
				if !ok || validate("Digest", key) != nil || (suffix != "claim" && suffix != "receipt") {
					return nil, ErrConflict
				}
			}
			var raw json.RawMessage
			if err = journal.read(name, &raw); err != nil {
				return nil, err
			}
			plainHash := sha256.Sum256(raw)
			if strings.HasSuffix(name, ".claim") {
				var claim map[string]any
				err = json.Unmarshal(raw, &claim)
				if err == nil && (len(claim) != 1 || validate("Digest", claim["digest"]) != nil) {
					err = ErrInvalid
				}
				if err == nil {
					claims[strings.TrimSuffix(name, ".claim")] = claim["digest"].(string)
				}
			} else if strings.HasSuffix(name, ".receipt") {
				var receipt Receipt
				err = json.Unmarshal(raw, &receipt)
				if err == nil {
					var value any
					value, err = canonical.ParseStrict(raw, canonical.Options{MaxBytes: controlBytes})
					if err == nil {
						err = validate("ExecutionReceiptRequest", value)
					}
				}
				if err == nil {
					receipts[strings.TrimSuffix(name, ".receipt")] = receiptWitness{receipt.OperationID, receipt.ExecutorID, receipt.Digest}
				}
			} else {
				var mode map[string]any
				err = json.Unmarshal(raw, &mode)
				if err == nil && (len(mode) != 1 || mode["format"] != "encrypted-v1") {
					err = ErrConflict
				}
			}
			clear(raw)
			if err != nil {
				return nil, err
			}
			f, openErr := journal.root.Open(name)
			if openErr != nil {
				return nil, openErr
			}
			hash := sha256.New()
			n, hashErr := io.Copy(hash, io.LimitReader(f, controlBytes+129))
			closeErr := f.Close()
			if hashErr != nil || closeErr != nil {
				return nil, errors.Join(hashErr, closeErr)
			}
			if n != info.Size() {
				return nil, ErrConflict
			}
			var cipherHash [32]byte
			copy(cipherHash[:], hash.Sum(nil))
			entries = append(entries, journalSnapshotEntry{Name: name, Plain: plainHash, Cipher: cipherHash})
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	for key, receipt := range receipts {
		identity := journal.encryption.identity
		if claims[key] != receipt.Digest || receipt.ExecutorID != identity.ExecutorID {
			return nil, ErrConflict
		}
		raw, encodeErr := canonical.Encode([]any{identity.ApplicationScopeID, identity.EndUserID, identity.ExecutorID, receipt.OperationID}, canonical.Options{MaxBytes: 8192})
		if encodeErr != nil || canonical.Hex(sha256.Sum256(raw)) != key {
			return nil, ErrConflict
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}
func sameJournalPlaintext(a, b []journalSnapshotEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Plain != b[i].Plain {
			return false
		}
	}
	return true
}
func migrationDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalid
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	same := real == filepath.Clean(path)
	if runtime.GOOS == "windows" {
		same = strings.EqualFold(real, filepath.Clean(path))
	}
	if !same {
		return nil, ErrInvalid
	}
	return info, nil
}
func sameMigrationDirectory(path string, before os.FileInfo) error {
	after, err := migrationDirectory(path)
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) {
		return ErrConflict
	}
	return nil
}
