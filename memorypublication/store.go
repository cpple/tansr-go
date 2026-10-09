package memorypublication

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tansrai/tansr-go/canonical"
	"github.com/tansrai/tansr-go/executor"
	"github.com/tansrai/tansr-go/internal/wire"
)

const format = "tansr-go-memory-publication-v1"

var magic = []byte("Tansr-Go-MemoryPublication/1\n")

type transfer struct {
	Request  Request `json:"request"`
	Owner    Owner   `json:"owner"`
	Status   string  `json:"status"`
	Received int     `json:"received"`
	Body     []byte  `json:"body"`
	Etag     *string `json:"etag"`
}
type state struct {
	Format    string              `json:"format"`
	Identity  Identity            `json:"identity"`
	Limits    Limits              `json:"limits"`
	Body      []byte              `json:"body"`
	Etag      *string             `json:"etag"`
	Transfers map[string]transfer `json:"transfers"`
}

// FileStore uses the same AES-GCM, host key, OS lock and sync/replace primitives
// as archive.FileStore. All publication, staging and permanent receipts are encrypted.
// A bounded encrypted snapshot is replaced per write; no physical power-loss SLA.
type FileStore struct {
	mu                sync.Mutex
	options           Options
	root              *os.Root
	lock              *os.File
	lockInfo          os.FileInfo
	fileInfo          os.FileInfo
	aead              cipher.AEAD
	aad               []byte
	state             state
	closed, uncertain bool
	commits           uint64
}

func hash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
func canonicalBytes(v any) ([]byte, error) {
	return canonical.Encode(v, canonical.Options{MaxBytes: 8 << 20})
}
func equal(a, b any) bool {
	x, e := canonicalBytes(a)
	y, f := canonicalBytes(b)
	return e == nil && f == nil && bytes.Equal(x, y)
}
func normalize(r Request) (Request, error) {
	raw, e := canonicalBytes(r)
	if e != nil {
		return nil, AdapterError("invalid_request")
	}
	v, e := wire.Decode(Contract, "MemoryPublicationRequest", raw)
	if e != nil {
		return nil, AdapterError("invalid_request")
	}
	return Request(v.(map[string]any)), nil
}
func number(r Request, k string) int { return int(r[k].(int64)) }
func validateOwner(o Owner) error {
	for k, v := range map[string]any{"Scope": o.Scope, "LegacyId": o.SessionID, "ExecutionBinding": o.Binding} {
		if e := wire.Validate("sdk2-ext-v1", k, v); e != nil {
			return AdapterError("invalid_request")
		}
	}
	return nil
}
func defaultLimits(l Limits) (Limits, error) {
	if l == (Limits{}) {
		l = Limits{4096, 2 * MaxBody, 32 << 20}
	}
	if l.MaxTransfers < 1 || l.MaxTransfers > 1048576 || l.MaxStagingBytes < 1 || l.MaxStagingBytes > 8*MaxBody || l.MaxFileBytes < 1024 || l.MaxFileBytes > 128<<20 {
		return l, AdapterError("capacity_exceeded")
	}
	return l, nil
}
func noLink(root *os.Root, name string) error {
	i, e := root.Lstat(name)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	if !i.Mode().IsRegular() {
		return ErrIntegrity
	}
	return nil
}
func (s *FileStore) current() (executor.Scope, error) {
	c, e := s.options.CurrentScope()
	if e != nil {
		return c, e
	}
	if wire.Validate("sdk2-ext-v1", "Scope", c) != nil || c.ApplicationScopeID != s.options.Identity.ApplicationScopeID || c.EndUserID != s.options.Identity.EndUserID {
		return c, AdapterError("request_conflict")
	}
	return c, nil
}
func (s *FileStore) fixed() error {
	parent := filepath.Dir(s.options.Path)
	real, e := filepath.EvalSymlinks(parent)
	if e != nil {
		return e
	}
	same := filepath.Clean(real) == filepath.Clean(parent)
	if filepath.Separator == '\\' {
		same = strings.EqualFold(real, parent)
	}
	if !same {
		return ErrIntegrity
	}
	i, e := os.Stat(parent)
	if e != nil {
		return e
	}
	f, e := s.root.Open(".")
	if e != nil {
		return e
	}
	j, e := f.Stat()
	f.Close()
	if e != nil || !os.SameFile(i, j) {
		return ErrIntegrity
	}
	for name, expected := range map[string]os.FileInfo{filepath.Base(s.options.Path): s.fileInfo, filepath.Base(s.options.Path) + ".lock": s.lockInfo} {
		if e = noLink(s.root, name); e != nil {
			return e
		}
		i, e = s.root.Stat(name)
		if expected == nil {
			if !errors.Is(e, os.ErrNotExist) {
				return ErrIntegrity
			}
		} else if e != nil || !os.SameFile(i, expected) {
			return ErrIntegrity
		}
	}
	return nil
}
func OpenFileStore(o Options) (*FileStore, error) { return openFileStore(o, nil) }
func openFileStore(o Options, initial *state) (_ *FileStore, err error) {
	if !filepath.IsAbs(o.Path) || len(o.Key) != 32 || o.CurrentScope == nil || (o.Mode != "create" && o.Mode != "reopen") {
		return nil, AdapterError("invalid_request")
	}
	o.Path = filepath.Clean(o.Path)
	o.Limits, err = defaultLimits(o.Limits)
	if err != nil {
		return nil, err
	}
	head := Request{"contract": Contract, "action": "head", "sourceId": o.Identity.SourceID, "sourceGeneration": o.Identity.SourceGeneration, "domainKey": o.Identity.DomainKey}
	if _, err = normalize(head); err != nil {
		return nil, err
	}
	parent := filepath.Dir(o.Path)
	real, e := filepath.EvalSymlinks(parent)
	if e != nil {
		return nil, e
	}
	same := real == parent
	if filepath.Separator == '\\' {
		same = strings.EqualFold(real, parent)
	}
	if !same {
		return nil, ErrIntegrity
	}
	root, e := os.OpenRoot(parent)
	if e != nil {
		return nil, e
	}
	s := &FileStore{options: o, root: root}
	s.options.Key = nil
	defer func() {
		if err != nil {
			s.closeFiles()
		}
	}()
	if _, err = s.current(); err != nil {
		return nil, err
	}
	name := filepath.Base(o.Path)
	if err = noLink(root, name+".lock"); err != nil {
		return nil, err
	}
	s.lock, err = root.OpenFile(name+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockExclusive(s.lock); err != nil {
		return nil, err
	}
	s.lockInfo, err = s.lock.Stat()
	if err != nil {
		return nil, err
	}
	if err = noLink(root, name); err != nil {
		return nil, err
	}
	s.fileInfo, err = root.Stat(name)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if o.Mode == "create" && exists {
		return nil, os.ErrExist
	}
	if o.Mode == "reopen" && !exists {
		return nil, os.ErrNotExist
	}
	block, e := aes.NewCipher(o.Key)
	if e != nil {
		return nil, e
	}
	s.aead, e = cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	id, _ := canonicalBytes(o.Identity)
	s.aad = append(append([]byte{}, magic...), id...)
	s.state = state{Format: format, Identity: o.Identity, Limits: o.Limits, Transfers: map[string]transfer{}}
	if initial != nil {
		raw, e := json.Marshal(initial)
		if e != nil {
			return nil, e
		}
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		e = d.Decode(&s.state)
		clear(raw)
		if e != nil {
			return nil, e
		}
		if e = s.validateState(); e != nil {
			return nil, e
		}
	}
	if !exists {
		c, e := s.current()
		if e != nil {
			return nil, e
		}
		if err = s.save(s.state, c, context.Background()); err != nil {
			return nil, err
		}
		return s, nil
	}
	f, e := root.Open(name)
	if e != nil {
		return nil, e
	}
	blob, e := io.ReadAll(io.LimitReader(f, int64(o.Limits.MaxFileBytes)+1))
	ce := f.Close()
	if e != nil {
		return nil, e
	}
	if ce != nil {
		return nil, ce
	}
	if len(blob) > o.Limits.MaxFileBytes || !bytes.HasPrefix(blob, magic) || len(blob) < len(magic)+s.aead.NonceSize()+s.aead.Overhead() {
		return nil, ErrIntegrity
	}
	sealed := blob[len(magic):]
	plain, e := s.aead.Open(nil, sealed[:s.aead.NonceSize()], sealed[s.aead.NonceSize():], s.aad)
	if e != nil {
		return nil, ErrIntegrity
	}
	defer clear(plain)
	d := json.NewDecoder(bytes.NewReader(plain))
	d.UseNumber()
	d.DisallowUnknownFields()
	if d.Decode(&s.state) != nil || d.Decode(new(any)) != io.EOF {
		return nil, ErrIntegrity
	}
	if err = s.validateState(); err != nil {
		return nil, err
	}
	if _, err = s.current(); err != nil {
		return nil, err
	}
	if err = s.fixed(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *FileStore) validateState() error {
	st := s.state
	if st.Format != format || st.Identity != s.options.Identity || st.Limits != s.options.Limits || st.Transfers == nil || len(st.Transfers) > st.Limits.MaxTransfers {
		return ErrIntegrity
	}
	if st.Etag == nil {
		if st.Body != nil {
			return ErrIntegrity
		}
	} else if len(st.Body) < 1 || len(st.Body) > MaxBody || !utf8.Valid(st.Body) || hash(st.Body) != *st.Etag {
		return ErrIntegrity
	}
	staging := 0
	for id, t := range st.Transfers {
		r, e := normalize(t.Request)
		if e != nil || r["action"] != "begin" || r["transferId"] != id || r["sourceId"] != st.Identity.SourceID || r["sourceGeneration"] != st.Identity.SourceGeneration || r["domainKey"] != st.Identity.DomainKey || validateOwner(t.Owner) != nil || t.Owner.Scope.ApplicationScopeID != st.Identity.ApplicationScopeID || t.Owner.Scope.EndUserID != st.Identity.EndUserID {
			return ErrIntegrity
		}
		t.Request = r
		n := number(r, "byteLength")
		if t.Received < 0 || t.Received > n {
			return ErrIntegrity
		}
		switch t.Status {
		case "staging":
			if len(t.Body) != n || t.Etag != nil {
				return ErrIntegrity
			}
			staging += n
		case "committed":
			if t.Body != nil || t.Etag == nil || *t.Etag != r["sha256"] || t.Received != n {
				return ErrIntegrity
			}
		case "conflict":
			if t.Body != nil || t.Etag != nil {
				return ErrIntegrity
			}
		default:
			return ErrIntegrity
		}
		s.state.Transfers[id] = t
	}
	if staging > st.Limits.MaxStagingBytes {
		return ErrIntegrity
	}
	return nil
}
func (s *FileStore) save(st state, before executor.Scope, ctx context.Context) error {
	raw, e := json.Marshal(st)
	if e != nil {
		return e
	}
	defer clear(raw)
	if len(raw)+len(magic)+s.aead.NonceSize()+s.aead.Overhead() > s.options.Limits.MaxFileBytes {
		return AdapterError("capacity_exceeded")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	blob := append(append([]byte{}, magic...), nonce...)
	blob = s.aead.Seal(blob, nonce, raw, s.aad)
	suffix := make([]byte, 12)
	if _, e = rand.Read(suffix); e != nil {
		return e
	}
	temp := fmt.Sprintf(".%s.%x.tmp", filepath.Base(s.options.Path), suffix)
	f, e := s.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer s.root.Remove(temp)
	n, e := f.Write(blob)
	if e == nil && n != len(blob) {
		e = io.ErrShortWrite
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	c, e := s.current()
	if e != nil {
		return e
	}
	if c != before {
		return AdapterError("request_conflict")
	}
	if e = s.fixed(); e != nil {
		return e
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = replaceDurable(s.root, s.options.Path, temp); e != nil {
		s.uncertain = true
		return errors.Join(ErrUnknown, e)
	}
	s.commits++
	s.fileInfo, e = s.root.Stat(filepath.Base(s.options.Path))
	if e != nil {
		s.uncertain = true
		return errors.Join(ErrUnknown, e)
	}
	c, e = s.current()
	if e != nil || c != before {
		s.uncertain = true
		return ErrUnknown
	}
	s.state = st
	return nil
}
func (s *FileStore) Identity() Identity         { return s.options.Identity }
func (s *FileStore) Capabilities() Capabilities { return Capabilities{true, true} }
func (s *FileStore) capacity() Capacity {
	n := 0
	for _, t := range s.state.Transfers {
		n += len(t.Body)
	}
	return Capacity{s.options.Limits, len(s.state.Transfers), n}
}
func (s *FileStore) Capacity() (Capacity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Capacity{}, ErrClosed
	}
	if s.uncertain {
		return Capacity{}, ErrUnknown
	}
	if _, e := s.current(); e != nil {
		return Capacity{}, e
	}
	if e := s.fixed(); e != nil {
		return Capacity{}, e
	}
	result := s.capacity()
	if _, e := s.current(); e != nil {
		return Capacity{}, e
	}
	return result, nil
}
func (s *FileStore) Execute(ctx context.Context, input Request, owner Owner) (Response, error) {
	ownerRaw, _ := json.Marshal(owner)
	owner = Owner{}
	if e := json.Unmarshal(ownerRaw, &owner); e != nil {
		return nil, AdapterError("invalid_request")
	}
	r, e := normalize(input)
	if e != nil {
		return nil, e
	}
	if e = validateOwner(owner); e != nil {
		return nil, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	if s.uncertain {
		return nil, ErrUnknown
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	scope, e := s.current()
	if e != nil {
		return nil, e
	}
	if scope != owner.Scope {
		return nil, AdapterError("request_conflict")
	}
	if e = s.fixed(); e != nil {
		return nil, e
	}
	id := s.options.Identity
	if r["sourceId"] != id.SourceID || r["sourceGeneration"] != id.SourceGeneration || r["domainKey"] != id.DomainKey {
		return nil, AdapterError("stale_generation")
	}
	commits := s.commits
	result, e := s.execute(ctx, r, owner, scope)
	if e != nil {
		return nil, e
	}
	if e = wire.Validate(Contract, "MemoryPublicationResponse", result); e != nil {
		return nil, ErrIntegrity
	}
	after, e := s.current()
	if s.commits != commits && (e != nil || after != scope) {
		s.uncertain = true
		return nil, ErrUnknown
	}
	if e != nil {
		return nil, e
	}
	if after != scope {
		return nil, AdapterError("request_conflict")
	}
	if e = s.fixed(); e != nil {
		if s.commits != commits {
			s.uncertain = true
			return nil, errors.Join(ErrUnknown, e)
		}
		return nil, e
	}
	return result, nil
}
func (s *FileStore) execute(ctx context.Context, r Request, owner Owner, scope executor.Scope) (Response, error) {
	action := r["action"].(string)
	out := Response{"contract": Contract, "action": action, "sourceId": r["sourceId"], "sourceGeneration": r["sourceGeneration"], "domainKey": r["domainKey"]}
	if action == "head" {
		out["publication"] = nil
		if s.state.Etag != nil {
			out["publication"] = map[string]any{"etag": *s.state.Etag, "byteLength": len(s.state.Body), "sha256": hash(s.state.Body)}
		}
		return out, nil
	}
	if action == "read" {
		if s.state.Etag == nil || r["etag"] != *s.state.Etag {
			return nil, AdapterError("revision_conflict")
		}
		offset, n := number(r, "offset"), number(r, "length")
		if offset > len(s.state.Body) {
			return nil, AdapterError("invalid_request")
		}
		end := min(offset+n, len(s.state.Body))
		b := s.state.Body[offset:end]
		out["etag"] = *s.state.Etag
		out["offset"] = offset
		out["byteLength"] = len(b)
		out["base64"] = base64.StdEncoding.EncodeToString(b)
		out["payloadDigest"] = hash(b)
		out["nextOffset"] = end
		out["complete"] = end == len(s.state.Body)
		return out, nil
	}
	tid := r["transferId"].(string)
	t, found := s.state.Transfers[tid]
	if found && !equal(t.Owner, owner) {
		if action != "query" || s.options.AuthorizeRecovery == nil || !s.options.AuthorizeRecovery(s.options.Identity, tid, t.Owner, owner) {
			return nil, AdapterError("request_conflict")
		}
	}
	next := s.state
	next.Transfers = make(map[string]transfer, len(s.state.Transfers)+1)
	for k, v := range s.state.Transfers {
		next.Transfers[k] = v
	}
	changed := false
	switch action {
	case "query":
	case "begin":
		if found {
			if !equal(t.Request, r) {
				return nil, AdapterError("request_conflict")
			}
		} else {
			c := s.capacity()
			if c.StoredTransfers >= c.Limits.MaxTransfers || c.StagingBytes+number(r, "byteLength") > c.Limits.MaxStagingBytes {
				return nil, AdapterError("capacity_exceeded")
			}
			t = transfer{Request: r, Owner: owner, Status: "staging", Body: make([]byte, number(r, "byteLength"))}
			changed = true
			found = true
		}
	case "chunk":
		if found {
			b, e := base64.StdEncoding.Strict().DecodeString(r["base64"].(string))
			if e != nil || len(b) != number(r, "byteLength") || base64.StdEncoding.EncodeToString(b) != r["base64"] || hash(b) != r["payloadDigest"] {
				return nil, AdapterError("integrity_mismatch")
			}
			off := number(r, "offset")
			if t.Status != "staging" || off+len(b) > len(t.Body) {
				return nil, AdapterError("request_conflict")
			}
			if off < t.Received {
				if off+len(b) > t.Received || !bytes.Equal(b, t.Body[off:off+len(b)]) {
					return nil, AdapterError("request_conflict")
				}
			} else {
				if off != t.Received {
					return nil, AdapterError("request_conflict")
				}
				t.Body = append([]byte{}, t.Body...)
				copy(t.Body[off:], b)
				t.Received += len(b)
				changed = true
			}
		}
	case "commit":
		if found && t.Status == "staging" {
			if t.Received != len(t.Body) || hash(t.Body) != t.Request["sha256"] || !utf8.Valid(t.Body) {
				return nil, AdapterError("integrity_mismatch")
			}
			var expected any
			if s.state.Etag != nil {
				expected = *s.state.Etag
			}
			if expected != t.Request["expectedEtag"] {
				t.Status = "conflict"
			} else {
				etag := hash(t.Body)
				next.Body = t.Body
				next.Etag = &etag
				t.Status = "committed"
				t.Etag = &etag
			}
			t.Body = nil
			changed = true
		}
	}
	if changed {
		next.Transfers[tid] = t
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if e := s.save(next, scope, ctx); e != nil {
			return nil, e
		}
	}
	receipt := map[string]any{"transferId": tid, "status": "unknown", "receivedBytes": nil, "etag": nil}
	if found {
		receipt["status"] = t.Status
		receipt["receivedBytes"] = t.Received
		if t.Etag != nil {
			receipt["etag"] = *t.Etag
		}
	}
	out["transfer"] = receipt
	return out, nil
}
func (s *FileStore) closeFiles() error {
	var e error
	if s.lock != nil {
		e = s.lock.Close()
	}
	if s.root != nil {
		e = errors.Join(e, s.root.Close())
	}
	return e
}
func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	clear(s.state.Body)
	for _, t := range s.state.Transfers {
		clear(t.Body)
	}
	s.state = state{}
	s.aead = nil
	return s.closeFiles()
}
