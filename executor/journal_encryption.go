package executor

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/tansrai/tansr-go/canonical"
)

// JournalEncryption supplies the same host-managed 32-byte key pattern as
// archive.StoreOptions. Identity is authenticated with every filename and body.
// Key rotation requires a new verified medium; this constructor never migrates.
type JournalEncryption struct {
	Key                                       []byte
	ApplicationScopeID, EndUserID, ExecutorID string
	CheckAccess                               func() error
}
type journalEncryption struct {
	aead     cipher.AEAD
	aad      []byte
	identity JournalEncryption
}

var encryptedJournalMagic = []byte("Tansr-Go-Journal/1\n")

// NewEncryptedFileJournal protects both claims and full receipts, including read
// results containing publication bytes. Existing plaintext journals are rejected.
func NewEncryptedFileJournal(directory string, options JournalEncryption) (*FileJournal, error) {
	enc, err := newJournalEncryption(options)
	if err != nil {
		return nil, err
	}
	return openFileJournal(directory, enc)
}
func newJournalEncryption(options JournalEncryption) (*journalEncryption, error) {
	if len(options.Key) != 32 || options.CheckAccess == nil || validate("Id", options.ApplicationScopeID) != nil || validate("LegacyId", options.EndUserID) != nil || validate("Id", options.ExecutorID) != nil {
		return nil, ErrInvalid
	}
	if e := options.CheckAccess(); e != nil {
		return nil, e
	}
	b, e := aes.NewCipher(options.Key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	aad, e := canonical.Encode([]any{options.ApplicationScopeID, options.EndUserID, options.ExecutorID}, canonical.Options{MaxBytes: 8192})
	if e != nil {
		return nil, e
	}
	options.Key = nil
	return &journalEncryption{aead: a, aad: aad, identity: options}, nil
}
func (j *FileJournal) EncryptedAtRest() bool { return j.encryption != nil }
func (j *FileJournal) protect(name string, data []byte) ([]byte, error) {
	if j.encryption == nil {
		return data, nil
	}
	if e := j.encryption.identity.CheckAccess(); e != nil {
		return nil, e
	}
	nonce := make([]byte, j.encryption.aead.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		return nil, e
	}
	out := append(append([]byte{}, encryptedJournalMagic...), nonce...)
	aad := append(append([]byte{}, j.encryption.aad...), []byte(name)...)
	return j.encryption.aead.Seal(out, nonce, data, aad), nil
}
func (j *FileJournal) unprotect(name string, data []byte) ([]byte, error) {
	if j.encryption == nil {
		if bytes.HasPrefix(data, encryptedJournalMagic) {
			return nil, ErrOutcomeUnknown
		}
		return data, nil
	}
	if e := j.encryption.identity.CheckAccess(); e != nil {
		return nil, e
	}
	a := j.encryption.aead
	if !bytes.HasPrefix(data, encryptedJournalMagic) || len(data) < len(encryptedJournalMagic)+a.NonceSize()+a.Overhead() {
		return nil, ErrOutcomeUnknown
	}
	data = data[len(encryptedJournalMagic):]
	aad := append(append([]byte{}, j.encryption.aad...), []byte(name)...)
	out, e := a.Open(nil, data[:a.NonceSize()], data[a.NonceSize():], aad)
	if e != nil {
		return nil, ErrOutcomeUnknown
	}
	return out, nil
}
func (j *FileJournal) verifyJournalMode() error {
	mode := "plaintext-v1"
	if j.encryption != nil {
		mode = "encrypted-v1"
	}
	var actual map[string]any
	e := j.read(".journal-mode", &actual)
	if e != nil {
		return e
	}
	if actual["format"] != mode {
		return ErrConflict
	}
	return nil
}
func (j *FileJournal) initializeJournalMode() error {
	if _, e := j.root.Stat(".journal-mode"); e == nil {
		return j.verifyJournalMode()
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if j.encryption != nil {
		d, e := j.root.Open(".")
		if e != nil {
			return e
		}
		names, e := d.Readdirnames(2)
		d.Close()
		if e != nil && !errors.Is(e, io.EOF) {
			return ErrConflict
		}
		for _, name := range names {
			if name != journalLockName {
				return ErrConflict
			}
		}
	}
	if j.encryption == nil {
		d, e := j.root.Open(".")
		if e != nil {
			return e
		}
		defer d.Close()
		for {
			entries, e := d.Readdirnames(128)
			if e != nil && !errors.Is(e, io.EOF) {
				return e
			}
			for _, name := range entries {
				if strings.HasSuffix(name, ".claim") || strings.HasSuffix(name, ".receipt") {
					var value map[string]any
					if e := j.read(name, &value); e != nil {
						return e
					}
				}
			}
			if errors.Is(e, io.EOF) {
				break
			}
		}
	}
	mode := "plaintext-v1"
	if j.encryption != nil {
		mode = "encrypted-v1"
	}
	if e := j.create(".journal-mode", map[string]any{"format": mode}); e != nil && !errors.Is(e, os.ErrExist) {
		return e
	}
	return j.verifyJournalMode()
}
func (j *FileJournal) checkJournal(op Operation) error {
	if e := j.verifyJournalMode(); e != nil {
		return e
	}
	if j.encryption != nil {
		o := j.encryption.identity
		if op.Scope.ApplicationScopeID != o.ApplicationScopeID || op.Scope.EndUserID != o.EndUserID || op.Binding.Target.ExecutorID != o.ExecutorID {
			return ErrConflict
		}
		return o.CheckAccess()
	}
	return nil
}
