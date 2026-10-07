package archive

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const storeFormat = "tansr-go-archive-v1"
const recoveryStoreFormat = "tansr-go-archive-v2"

var storeMagic = []byte("Tansr-Go-Archive/1\n")

// StoreLimits are logical bounds, not a filesystem quota. FileStore rewrites one
// bounded encrypted snapshot per transaction; use an application store for huge archives.
type StoreLimits struct {
	MaxRecords     int `json:"maxRecords"`
	MaxArtifacts   int `json:"maxArtifacts"`
	MaxStoredBytes int `json:"maxStoredBytes"`
	MaxBatchBytes  int `json:"maxBatchBytes"`
}
type StoreOptions struct {
	// Path must be an absolute host-configured filename in an existing private directory.
	Path string
	// Key is exactly 32 bytes, supplied by the host key store, never saved in the archive.
	Key      []byte
	Identity Identity
	Limits   StoreLimits
	// CheckAccess is mandatory and runs before and after reads, and before commits.
	// It must reject logout, revoked access or a change of current application/user.
	// Do not call back into this store or authorize using data read from its file.
	CheckAccess func(Identity) error
}

// Store lets applications supply a database or another durable archive medium.
// Receive MUST atomically save the verified page, all bytes and the original ACK
// before returning. Confirm MUST durably verify the original completed receipt.
// Pending, Head and Coverage have distinct meanings; implementations must not
// replace a pending identity or derive access authority from stored metadata.
// Identity and StorageLimits are immutable for the store's lifetime.
type Store interface {
	Identity() Identity
	StorageLimits() StoreLimits
	CheckAccess() error
	Head() (*Head, error)
	Coverage() (*Coverage, error)
	Pending() (*Ack, error)
	Receive(Binding, Status, Page, map[string][]byte, RequestIdentity) (Ack, error)
	Confirm(MutationReceipt) error
	ReadRecordsByID([]string) ([]Record, error)
	Body(ArtifactRef) ([]byte, error)
}
type savedArtifact struct {
	Ref  ArtifactRef `json:"ref"`
	Body []byte      `json:"body"`
}
type storeState struct {
	Format      string                   `json:"format"`
	Identity    Identity                 `json:"identity"`
	Limits      StoreLimits              `json:"limits"`
	Records     []Record                 `json:"records"`
	Artifacts   map[string]savedArtifact `json:"artifacts"`
	Pending     *Ack                     `json:"pending"`
	Coverage    *Coverage                `json:"coverage"`
	LastReceipt *MutationReceipt         `json:"lastReceipt"`
	Rebases     []rebaseEntry            `json:"rebases,omitempty"`
}

// FileStore serializes access in this process and takes an OS file lock across
// processes. Crashing releases that lock, so reopening can recover pending ACKs.
type FileStore struct {
	mu          sync.Mutex
	authMu      sync.Mutex
	closed      bool
	uncertain   bool
	path        string
	root        *os.Root
	lockFile    *os.File
	cipher      cipher.AEAD
	identity    Identity
	limits      StoreLimits
	checkAccess func(Identity) error
	state       storeState
}

func defaultLimits(l StoreLimits) (StoreLimits, error) {
	if l == (StoreLimits{}) {
		l = StoreLimits{4096, 16384, 64 << 20, 8 << 20}
	}
	if l.MaxRecords < 1 || l.MaxRecords > 1000000 || l.MaxArtifacts < 1 || l.MaxArtifacts > 1000000 || l.MaxStoredBytes < 1 || l.MaxStoredBytes > 64<<20 || l.MaxBatchBytes < 1 || l.MaxBatchBytes > l.MaxStoredBytes {
		return l, ErrCapacity
	}
	return l, nil
}
func validateIdentity(i Identity) error {
	for _, v := range []string{i.ApplicationScopeID, i.BindingID, i.SourceID, i.SourceGeneration} {
		if err := validate("Id", v); err != nil {
			return err
		}
	}
	for _, v := range []string{i.EndUserID, i.SessionID} {
		if err := validate("LegacyId", v); err != nil {
			return err
		}
	}
	return validate("Generations", i.Generations)
}

// OpenFileStore creates an empty encrypted store or verifies and reopens the
// existing store with the same identity, limits and key. It never deletes or resets
// an unreadable store and never treats an old backup as current server authority.
func OpenFileStore(options StoreOptions) (store *FileStore, err error) {
	if !filepath.IsAbs(options.Path) || filepath.Base(options.Path) == "." || len(options.Key) != 32 || options.CheckAccess == nil {
		return nil, errors.New("archive: absolute Path, 32-byte Key and CheckAccess are required")
	}
	if err = validateIdentity(options.Identity); err != nil {
		return nil, err
	}
	limits, err := defaultLimits(options.Limits)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(filepath.Clean(options.Path))
	real, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return nil, err
	}
	if !samePath(parent, real) {
		return nil, errors.New("archive: parent directory must not contain symlinks")
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, err
	}
	store = &FileStore{path: filepath.Join(parent, filepath.Base(options.Path)), root: root, identity: options.Identity, limits: limits, checkAccess: options.CheckAccess}
	opened := store
	defer func() {
		if err != nil {
			opened.closeFiles()
			store = nil
		}
	}()
	if err = store.authorize(); err != nil {
		return nil, err
	}
	name := filepath.Base(store.path)
	if err = rejectLink(root, name+".lock"); err != nil {
		return nil, err
	}
	store.lockFile, err = root.OpenFile(name+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockExclusive(store.lockFile); err != nil {
		return nil, fmt.Errorf("archive: store is already in use: %w", err)
	}
	if err = rejectLink(root, name); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(options.Key)
	if err != nil {
		return nil, err
	}
	store.cipher, err = cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	store.state = storeState{Format: storeFormat, Identity: store.identity, Limits: limits, Records: []Record{}, Artifacts: map[string]savedArtifact{}}
	f, err := root.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		err = store.save(store.state)
		return store, err
	}
	if err != nil {
		return nil, err
	}
	blob, readErr := io.ReadAll(io.LimitReader(f, int64(2*limits.MaxStoredBytes+(4<<20))+1))
	closeErr := f.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(blob) > 2*limits.MaxStoredBytes+(4<<20) || !bytes.HasPrefix(blob, storeMagic) || len(blob) < len(storeMagic)+store.cipher.NonceSize()+store.cipher.Overhead() {
		return nil, ErrIntegrity
	}
	sealed := blob[len(storeMagic):]
	plain, err := store.cipher.Open(nil, sealed[:store.cipher.NonceSize()], sealed[store.cipher.NonceSize():], storeMagic)
	if err != nil {
		return nil, ErrIntegrity
	}
	defer clear(plain)
	decoder := json.NewDecoder(bytes.NewReader(plain))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&store.state); err != nil {
		return nil, ErrIntegrity
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, ErrIntegrity
	}
	if err = store.validateState(); err != nil {
		return nil, err
	}
	if err = store.authorize(); err != nil {
		return nil, err
	}
	return store, nil
}
func samePath(a, b string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
func rejectLink(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("archive: store and lock must be regular files")
	}
	return nil
}
func (s *FileStore) authorize() error {
	s.authMu.Lock()
	defer s.authMu.Unlock()
	return s.checkAccess(s.identity)
}
func (s *FileStore) enter() error {
	if s == nil {
		return ErrClosed
	}
	if err := s.authorize(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	if s.uncertain {
		s.mu.Unlock()
		return errors.New("archive: uncertain durable write; close and reopen the store")
	}
	return nil
}

// CheckAccess revalidates the live host authorization and open state.
func (s *FileStore) CheckAccess() error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.mu.Unlock()
	return s.authorize()
}
func (s *FileStore) save(state storeState) error {
	if err := s.authorize(); err != nil {
		return err
	}
	plain, err := json.Marshal(state)
	if err != nil {
		return err
	}
	defer clear(plain)
	if len(plain) > 2*s.limits.MaxStoredBytes+(4<<20) {
		return ErrCapacity
	}
	nonce := make([]byte, s.cipher.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	blob := append(append([]byte{}, storeMagic...), nonce...)
	blob = s.cipher.Seal(blob, nonce, plain, storeMagic)
	suffix := make([]byte, 12)
	if _, err = rand.Read(suffix); err != nil {
		return err
	}
	temp := fmt.Sprintf(".%s.%x.tmp", filepath.Base(s.path), suffix)
	f, err := s.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer s.root.Remove(temp)
	n, writeErr := f.Write(blob)
	if writeErr == nil && n != len(blob) {
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
	if err = s.authorize(); err != nil {
		return err
	}
	if err = rejectLink(s.root, filepath.Base(s.path)); err != nil {
		return err
	}
	if err = replaceDurable(s.root, s.path, temp); err != nil {
		s.uncertain = true
		return err
	}
	return nil
}
func (s *FileStore) closeFiles() error {
	var err error
	if s.lockFile != nil {
		err = s.lockFile.Close()
		s.lockFile = nil
	}
	if s.root != nil {
		err = errors.Join(err, s.root.Close())
		s.root = nil
	}
	return err
}
func (s *FileStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.cipher = nil
	for id, artifact := range s.state.Artifacts {
		clear(artifact.Body)
		delete(s.state.Artifacts, id)
	}
	s.state.Records = nil
	s.state.Pending = nil
	s.state.Coverage = nil
	s.state.LastReceipt = nil
	s.state.Rebases = nil
	return s.closeFiles()
}
func clone[T any](v T) T {
	raw, _ := json.Marshal(v)
	var copy T
	_ = json.Unmarshal(raw, &copy)
	return copy
}
func (s *FileStore) Identity() Identity         { return s.identity }
func (s *FileStore) StorageLimits() StoreLimits { return s.limits }
func (s *FileStore) Head() (*Head, error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	if err := s.authorize(); err != nil {
		return nil, err
	}
	return stateHead(s.state), nil
}
func stateHead(state storeState) *Head {
	if len(state.Records) == 0 {
		return nil
	}
	r := state.Records[len(state.Records)-1]
	return &Head{r.Sequence, r.RecordDigest}
}

// Coverage reports only the coverage with a verified completed server receipt.
func (s *FileStore) Coverage() (*Coverage, error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	if err := s.authorize(); err != nil {
		return nil, err
	}
	return clone(s.state.Coverage), nil
}
func (s *FileStore) Pending() (*Ack, error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	if err := s.authorize(); err != nil {
		return nil, err
	}
	return clone(s.state.Pending), nil
}

// ReadRecords returns at most limit verified records, starting at fromSequence (inclusive).
func (s *FileStore) ReadRecords(fromSequence string, limit int) ([]Record, error) {
	if validate("RecordSequence", fromSequence) != nil || limit < 1 || limit > 128 {
		return nil, ErrIntegrity
	}
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	out := []Record{}
	for _, r := range s.state.Records {
		if seq(r.Sequence) >= seq(fromSequence) {
			out = append(out, r)
			if len(out) == limit {
				break
			}
		}
	}
	if err := s.authorize(); err != nil {
		return nil, err
	}
	return clone(out), nil
}

// ReadRecordsByID returns every requested record, in request order. A missing
// record is an error, never an incomplete material success.
func (s *FileStore) ReadRecordsByID(ids []string) ([]Record, error) {
	if len(ids) < 1 || len(ids) > 128 {
		return nil, ErrCapacity
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		if validate("Id", id) != nil || wanted[id] {
			return nil, ErrIntegrity
		}
		wanted[id] = true
	}
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	found := map[string]Record{}
	for _, r := range s.state.Records {
		if wanted[r.RecordID] {
			found[r.RecordID] = r
		}
	}
	if len(found) != len(ids) {
		return nil, errors.New("archive: requested record is unavailable")
	}
	out := make([]Record, 0, len(ids))
	for _, id := range ids {
		out = append(out, found[id])
	}
	if err := s.authorize(); err != nil {
		return nil, err
	}
	return clone(out), nil
}
func (s *FileStore) Body(ref ArtifactRef) ([]byte, error) {
	if err := validate("ArtifactRef", ref); err != nil {
		return nil, err
	}
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.mu.Unlock()
	stored, ok := s.state.Artifacts[ref.ArtifactID]
	if !ok || stored.Ref != ref || ref.SourceID != s.identity.SourceID || len(stored.Body) != ref.Bytes || digest(stored.Body) != ref.SHA256 {
		return nil, ErrIntegrity
	}
	if err := s.authorize(); err != nil {
		return nil, err
	}
	return append([]byte{}, stored.Body...), nil
}
func logicalBytes(state storeState) (int, error) {
	total := 0
	for _, r := range state.Records {
		raw, err := canon(r)
		if err != nil {
			return 0, err
		}
		total += len(raw)
	}
	for _, a := range state.Artifacts {
		total += len(a.Body)
	}
	for _, row := range state.Rebases {
		raw, err := canon(row)
		if err != nil {
			return 0, err
		}
		total += len(raw)
		if row.Result == nil && row.OriginalReceipt == nil {
			// Reserve the frozen maximum result before transmitting a rebase.
			total += rebaseResponseBytes
		}
	}
	return total, nil
}
func (s *FileStore) validateState() error {
	state := s.state
	if state.Format != storeFormat && state.Format != recoveryStoreFormat || state.Identity != s.identity || state.Limits != s.limits || state.Records == nil || state.Artifacts == nil || len(state.Records) > s.limits.MaxRecords || len(state.Artifacts) > s.limits.MaxArtifacts {
		return ErrIntegrity
	}
	total, err := logicalBytes(state)
	if err != nil {
		return err
	}
	if total > s.limits.MaxStoredBytes {
		return ErrCapacity
	}
	prev := strings.Repeat("0", 64)
	seen := map[string]bool{}
	refs := map[string]ArtifactRef{}
	for index, r := range state.Records {
		if seq(r.Sequence) != int64(index+1) || r.PredecessorDigest != prev || seen[r.RecordID] || r.Target.SessionID != s.identity.SessionID || r.Target.Generations != s.identity.Generations {
			return ErrIntegrity
		}
		if err := verifyRecord(r, 262144); err != nil {
			return err
		}
		seen[r.RecordID] = true
		prev = r.RecordDigest
		for _, ref := range append([]ArtifactRef{r.Payload}, r.Attachments...) {
			if ref.SourceID != s.identity.SourceID {
				return ErrIntegrity
			}
			if prior, ok := refs[ref.ArtifactID]; ok && prior != ref {
				return ErrIntegrity
			}
			refs[ref.ArtifactID] = ref
			body, ok := state.Artifacts[ref.ArtifactID]
			if !ok || body.Ref != ref || len(body.Body) != ref.Bytes || digest(body.Body) != ref.SHA256 {
				return ErrIntegrity
			}
		}
		if domainDigest("tansr.sdk2.payload.v1", state.Artifacts[r.Payload.ArtifactID].Body) != r.PayloadDigest {
			return ErrIntegrity
		}
	}
	if len(refs) != len(state.Artifacts) {
		return ErrIntegrity
	}
	if state.Coverage != nil {
		if !validCoverage(*state.Coverage) || seq(state.Coverage.ThroughSequence) > int64(len(state.Records)) || state.Records[seq(state.Coverage.ThroughSequence)-1].RecordDigest != state.Coverage.HeadDigest || state.LastReceipt == nil {
			return ErrIntegrity
		}
	} else if state.LastReceipt != nil {
		return ErrIntegrity
	}
	if state.Pending != nil {
		ack := *state.Pending
		if validate("ArchiveAckRequest", ack) != nil || ack.BindingID != s.identity.BindingID || ack.SourceID != s.identity.SourceID || ack.SourceGeneration != s.identity.SourceGeneration || ack.Generations != s.identity.Generations || !validCoverage(ack.Coverage) || seq(ack.Coverage.ThroughSequence) != int64(len(state.Records)) || ack.Coverage.HeadDigest != prev {
			return ErrIntegrity
		}
		from := int64(1)
		if state.Coverage != nil {
			from = seq(state.Coverage.ThroughSequence) + 1
		}
		if seq(ack.Coverage.FromSequence) != from {
			return ErrIntegrity
		}
	} else if len(state.Records) > 0 && (state.Coverage == nil || seq(state.Coverage.ThroughSequence) != int64(len(state.Records))) {
		return ErrIntegrity
	}
	return s.validateRebases()
}

// Receive verifies and durably commits one page and all its referenced bytes.
// The returned ACK is prepared, not yet accepted by Serve.
func (s *FileStore) Receive(binding Binding, status Status, page Page, artifacts map[string][]byte, request RequestIdentity) (Ack, error) {
	var zero Ack
	if err := s.enter(); err != nil {
		return zero, err
	}
	defer s.mu.Unlock()
	if s.state.Pending != nil {
		return zero, ErrPendingAck
	}
	id, err := IdentityFrom(binding, status)
	if err != nil {
		return zero, err
	}
	if id != s.identity || binding.OperationEpoch == nil || binding.OperationEpoch.ID != request.OperationEpoch || !has(binding.AcceptedCapabilities, "archive-transfer-v1") || binding.ArchiveAckFormat == nil || *binding.ArchiveAckFormat != "split-receipts-v1" {
		return zero, ErrIntegrity
	}
	if !sameString(status.PublishedThroughSequence, page.PublishedThroughSequence) {
		return zero, ErrSnapshotChanged
	}
	if err = validate("RequestIdentity", request); err != nil {
		return zero, err
	}
	if reservedRebaseIdentity(s.state, request) {
		return zero, ErrReceipt
	}
	head := stateHead(s.state)
	var after *string
	if head != nil {
		after = &head.Sequence
	}
	if head == nil && status.AcknowledgedCoverage != nil || head != nil && (status.AcknowledgedCoverage == nil || status.AcknowledgedCoverage.ThroughSequence != head.Sequence || status.AcknowledgedCoverage.HeadDigest != head.RecordDigest) {
		return zero, ErrIntegrity
	}
	if err = verifyPage(binding, after, page); err != nil {
		return zero, err
	}
	if len(page.Records) == 0 {
		return zero, ErrIntegrity
	}
	predecessor := strings.Repeat("0", 64)
	if head != nil {
		predecessor = head.RecordDigest
	}
	if page.Records[0].PredecessorDigest != predecessor {
		return zero, ErrIntegrity
	}
	refs := map[string]ArtifactRef{}
	payloads := []ArtifactReceipt{}
	attachments := []ArtifactReceipt{}
	pseen, aseen := map[string]bool{}, map[string]bool{}
	batchBytes := 0
	for _, r := range page.Records {
		raw, _ := canon(r)
		if len(raw) > s.limits.MaxBatchBytes-batchBytes {
			return zero, ErrCapacity
		}
		batchBytes += len(raw)
		for index, ref := range append([]ArtifactRef{r.Payload}, r.Attachments...) {
			if _, ok := refs[ref.ArtifactID]; !ok {
				if ref.Bytes > s.limits.MaxBatchBytes-batchBytes {
					return zero, ErrCapacity
				}
				batchBytes += ref.Bytes
			}
			refs[ref.ArtifactID] = ref
			if index == 0 && !pseen[ref.ArtifactID] {
				payloads = append(payloads, ArtifactReceipt{ref.ArtifactID, ref.SHA256, "durably-stored"})
				pseen[ref.ArtifactID] = true
			}
			if index > 0 && !aseen[ref.ArtifactID] {
				attachments = append(attachments, ArtifactReceipt{ref.ArtifactID, ref.SHA256, "durably-stored"})
				aseen[ref.ArtifactID] = true
			}
		}
	}
	if batchBytes > s.limits.MaxBatchBytes || len(refs) != len(artifacts) {
		return zero, ErrCapacity
	}
	next := clone(s.state)
	seen := map[string]bool{}
	for _, r := range next.Records {
		seen[r.RecordID] = true
	}
	for _, r := range page.Records {
		if seen[r.RecordID] {
			return zero, ErrIntegrity
		}
		seen[r.RecordID] = true
	}
	for id, ref := range refs {
		body, ok := artifacts[id]
		if !ok || len(body) != ref.Bytes || digest(body) != ref.SHA256 {
			return zero, ErrIntegrity
		}
		if prior, ok := next.Artifacts[id]; ok && prior.Ref != ref {
			return zero, ErrIntegrity
		}
		next.Artifacts[id] = savedArtifact{ref, append([]byte{}, body...)}
	}
	for _, r := range page.Records {
		if domainDigest("tansr.sdk2.payload.v1", next.Artifacts[r.Payload.ArtifactID].Body) != r.PayloadDigest {
			return zero, ErrIntegrity
		}
	}
	last := page.Records[len(page.Records)-1]
	ack := Ack{Protocol: Protocol, Request: request, BindingID: s.identity.BindingID, ExpectedRevision: binding.Revision, Generations: s.identity.Generations, SourceID: s.identity.SourceID, SourceGeneration: s.identity.SourceGeneration, Coverage: Coverage{page.Records[0].Sequence, last.Sequence, last.RecordDigest}, Attachments: attachments, AckFormat: "split-receipts-v1", Payloads: payloads}
	if err = validate("ArchiveAckRequest", ack); err != nil {
		return zero, err
	}
	raw, err := canon(ack)
	if err != nil {
		return zero, err
	}
	if len(raw) > binding.Limits.ControlBytes {
		return zero, ErrCapacity
	}
	next.Records = append(next.Records, clone(page.Records)...)
	next.Pending = &ack
	total, err := logicalBytes(next)
	if err != nil {
		return zero, err
	}
	if len(next.Records) > s.limits.MaxRecords || len(next.Artifacts) > s.limits.MaxArtifacts || total > s.limits.MaxStoredBytes {
		return zero, ErrCapacity
	}
	if err = s.save(next); err != nil {
		return zero, err
	}
	s.state = next
	return clone(ack), nil
}

// Confirm advances accepted coverage only for the original completed ACK and
// its scope-framed semantic digest. A save failure leaves the ACK pending.
func (s *FileStore) Confirm(receipt MutationReceipt) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.mu.Unlock()
	if s.state.Pending == nil {
		if s.state.LastReceipt != nil && equal(*s.state.LastReceipt, receipt) {
			return nil
		}
		return ErrReceipt
	}
	if err := verifyReceipt(s.identity, *s.state.Pending, receipt); err != nil {
		return err
	}
	next := clone(s.state)
	coverage := next.Pending.Coverage
	next.Coverage = &coverage
	next.Pending = nil
	copy := clone(receipt)
	next.LastReceipt = &copy
	for index := range next.Rebases {
		row := &next.Rebases[index]
		if row.Result == nil && row.OriginalReceipt == nil && equal(row.Intent.Previous, *s.state.Pending) {
			row.OriginalReceipt = &copy
		}
	}
	if err := s.save(next); err != nil {
		return err
	}
	s.state = next
	return nil
}
