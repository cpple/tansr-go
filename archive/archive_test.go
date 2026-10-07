package archive

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/tansrai/tansr-go/api"
	"github.com/tansrai/tansr-go/canonical"
)

func fixture(t *testing.T) (Binding, Status, Page, map[string][]byte) {
	t.Helper()
	format := "split-receipts-v1"
	published := "1"
	limits := Limits{ControlBytes: 262144, RecordBytes: 262144, PageRecords: 128, PageBytes: 1048576, AttachmentBytes: 33554432, ChunkBytes: 1024, MaterialConcurrent: 2, MaterialQueue: 16, MaterialCandidates: 32, MaterialBytes: 1048576, MaterialDeadlineMs: 30000, PendingRecords: 4096, PendingBytes: 67108864, InflightReserveBytes: 1048576, OfflineMs: 1000, EventRetentionMs: 1000, EventRetentionFrames: 1, EventRetentionBytes: 1024, TerminalReceiptRetentionMs: 60000, EpochLifetimeMs: 60000, MaterialChunkBytes: 65536}
	target := Target{"opaque-session 中文", Generations{"h-1", "0", "9007199254740993"}, strings.Repeat("0", 64)}
	b := Binding{Protocol: Protocol, BindingID: "binding-1", Scope: Scope{"app-1", "user-1", "1"}, Target: target, Revision: "1", State: "active", SourceID: "source-1", AcceptedCapabilities: []string{"archive-transfer-v1", "context-materials-v1"}, RejectedCapabilities: []RejectedCapability{}, Availability: "legacy-complete", OperationEpoch: &Epoch{"epoch-1", "2030-01-01T00:00:00.000Z", "2030-01-01T00:01:00.000Z", "active"}, Limits: limits, ArchiveAckFormat: &format}
	body := []byte(`{"role":"user","text":"合成-secret-body-标签"}`)
	ref := ArtifactRef{"artifact-1", b.SourceID, len(body), digest(body), "application/json"}
	r := Record{RecordID: "record-1", Sequence: "1", Target: target, TurnID: "turn-1", RecordKind: "turn", TurnState: "completed", PredecessorDigest: strings.Repeat("0", 64), RecordDigest: strings.Repeat("0", 64), Payload: ref, Attachments: []ArtifactRef{}, PayloadDigest: domainDigest("tansr.sdk2.payload.v1", body)}
	r.RecordDigest = recordHash(t, r)
	s := Status{Protocol: Protocol, BindingID: b.BindingID, Revision: b.Revision, Generations: target.Generations, SourceID: b.SourceID, SourceGeneration: "source-generation-1", PublishedThroughSequence: &published, PendingBytes: len(body), PendingRecords: 1, SessionPersistence: "unchanged", State: "active"}
	p := Page{Protocol: Protocol, BindingID: b.BindingID, Generations: target.Generations, Records: []Record{r}, NextAfterSequence: &published, Complete: true, PublishedThroughSequence: &published}
	for name, value := range map[string]any{"BindingView": b, "ArchiveStatus": s, "ArchivePage": p} {
		if err := validate(name, value); err != nil {
			t.Fatal(name, err)
		}
	}
	return b, s, p, map[string][]byte{ref.ArtifactID: body}
}
func recordHash(t *testing.T, r Record) string {
	t.Helper()
	data, err := canon(r)
	if err != nil {
		t.Fatal(err)
	}
	value, err := canonical.Decode(data, canonical.Options{MaxBytes: 2 << 20})
	if err != nil {
		t.Fatal(err)
	}
	m := value.(map[string]any)
	delete(m, "recordDigest")
	data, err = canon(m)
	if err != nil {
		t.Fatal(err)
	}
	return domainDigest("tansr.sdk2.record.v1", data)
}
func receiptFor(t *testing.T, id Identity, ack Ack) MutationReceipt {
	t.Helper()
	data, _ := canon(ack)
	value, err := canonical.Decode(data, canonical.Options{MaxBytes: 2 << 20})
	if err != nil {
		t.Fatal(err)
	}
	m := value.(map[string]any)
	delete(m, "request")
	data, err = canon(map[string]any{"scope": []string{id.ApplicationScopeID, id.EndUserID}, "operation": "archive-ack", "semantic": m})
	if err != nil {
		t.Fatal(err)
	}
	return MutationReceipt{Protocol: Protocol, Request: ack.Request, BindingID: ack.BindingID, Operation: "archive-ack", SemanticDigest: domainDigest("tansr.sdk2.operation.v1", data), State: "completed", Revision: strconv.FormatInt(seq(ack.ExpectedRevision)+1, 10), OutcomeRef: "receipt-1"}
}
func archiveTempDir(t *testing.T) string {
	t.Helper()
	// macOS may expose TMPDIR through /var -> /private/var. Resolve only this
	// test-owned directory; the store still rejects symlink parent paths.
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return directory
}
func openFixture(t *testing.T, b Binding, status Status) (*FileStore, StoreOptions) {
	t.Helper()
	id, err := IdentityFrom(b, status)
	if err != nil {
		t.Fatal(err)
	}
	options := StoreOptions{Path: filepath.Join(archiveTempDir(t), "archive.bin"), Key: bytes.Repeat([]byte{7}, 32), Identity: id, CheckAccess: func(got Identity) error {
		if got != id {
			return ErrIntegrity
		}
		return nil
	}}
	store, err := OpenFileStore(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, options
}
func TestDurablePageBeforeAckAndReopen(t *testing.T) {
	b, s, p, objects := fixture(t)
	store, options := openFixture(t, b, s)
	ack, err := store.Receive(b, s, p, objects, RequestIdentity{"request-1", "epoch-1"})
	if err != nil {
		t.Fatal(err)
	}
	coverage, err := store.Coverage()
	if err != nil || coverage != nil {
		t.Fatalf("local write falsely became acknowledged: %v %v", coverage, err)
	}
	stored, err := os.ReadFile(options.Path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte("secret-body")) || bytes.Contains(stored, []byte("user-1")) {
		t.Fatal("archive contains plaintext")
	}
	if _, err = OpenFileStore(options); err == nil {
		t.Fatal("second process handle opened live store")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFileStore(options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	pending, err := reopened.Pending()
	if err != nil || pending == nil || !equal(*pending, ack) {
		t.Fatalf("pending ACK lost: %v", err)
	}
	bad := receiptFor(t, options.Identity, ack)
	bad.State = "accepted"
	if err = reopened.Confirm(bad); !errors.Is(err, ErrReceipt) {
		t.Fatalf("accepted was treated complete: %v", err)
	}
	bad = receiptFor(t, options.Identity, ack)
	bad.SemanticDigest = strings.Repeat("f", 64)
	if err = reopened.Confirm(bad); !errors.Is(err, ErrReceipt) {
		t.Fatal("wrong semantic digest accepted")
	}
	good := receiptFor(t, options.Identity, ack)
	if err = reopened.Confirm(good); err != nil {
		t.Fatal(err)
	}
	if err = reopened.Confirm(good); err != nil {
		t.Fatal("idempotent confirm", err)
	}
	coverage, err = reopened.Coverage()
	if err != nil || coverage == nil || *coverage != ack.Coverage {
		t.Fatal("completed receipt not persisted", err)
	}
	body, err := reopened.Body(p.Records[0].Payload)
	if err != nil || !bytes.Equal(body, objects["artifact-1"]) {
		t.Fatal("body changed", err)
	}
	body[0] = 'x'
	again, _ := reopened.Body(p.Records[0].Payload)
	if bytes.Equal(body, again) {
		t.Fatal("body aliases store memory")
	}
}
func TestArchiveRejectsIntegrityFailuresBeforeSave(t *testing.T) {
	for _, kind := range []string{"body", "record-digest", "chain", "source", "sequence", "extra-object", "cross-user", "payload-digest"} {
		t.Run(kind, func(t *testing.T) {
			b, s, p, objects := fixture(t)
			store, _ := openFixture(t, b, s)
			switch kind {
			case "body":
				objects["artifact-1"][0] = 'x'
			case "record-digest":
				p.Records[0].RecordDigest = strings.Repeat("f", 64)
			case "chain":
				p.Records[0].PredecessorDigest = strings.Repeat("f", 64)
				p.Records[0].RecordDigest = recordHash(t, p.Records[0])
			case "source":
				p.Records[0].Payload.SourceID = "other-source"
				p.Records[0].RecordDigest = recordHash(t, p.Records[0])
			case "sequence":
				p.Records[0].Sequence = "2"
				p.Records[0].RecordDigest = recordHash(t, p.Records[0])
			case "extra-object":
				objects["unexpected"] = []byte("x")
			case "cross-user":
				b.Scope.EndUserID = "other-user"
			case "payload-digest":
				p.Records[0].PayloadDigest = strings.Repeat("f", 64)
				p.Records[0].RecordDigest = recordHash(t, p.Records[0])
			}
			if _, err := store.Receive(b, s, p, objects, RequestIdentity{"request-1", "epoch-1"}); err == nil {
				t.Fatal("invalid batch accepted")
			}
			head, err := store.Head()
			if err != nil || head != nil {
				t.Fatal("rejected batch changed head", err)
			}
		})
	}
}
func TestArchiveWrongKeyIdentityAndRevocation(t *testing.T) {
	b, s, _, _ := fixture(t)
	store, options := openFixture(t, b, s)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	wrong := options
	wrong.Key = bytes.Repeat([]byte{8}, 32)
	if _, err := OpenFileStore(wrong); err == nil {
		t.Fatal("wrong key accepted")
	}
	wrong = options
	wrong.Identity.EndUserID = "other-user"
	wrong.CheckAccess = func(Identity) error { return nil }
	if _, err := OpenFileStore(wrong); err == nil {
		t.Fatal("wrong user accepted")
	}
	allowed := true
	options.CheckAccess = func(Identity) error {
		if !allowed {
			return errors.New("revoked")
		}
		return nil
	}
	store, err := OpenFileStore(options)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	allowed = false
	if _, err = store.ReadRecords("1", 1); err == nil {
		t.Fatal("revoked read allowed")
	}
	if _, err = store.Pending(); err == nil {
		t.Fatal("revoked reconciliation allowed")
	}
}

type fakeArchive struct {
	t                 *testing.T
	mu                sync.Mutex
	b                 Binding
	s                 Status
	p                 Page
	objects           map[string][]byte
	id                Identity
	dropFirst         bool
	acks              []Ack
	recordReads       int
	chunkCalls        int
	badChunk          bool
	uploaded          int
	materialSubmitted int
}

func fakeArchiveServer(t *testing.T) (*fakeArchive, *Client) {
	t.Helper()
	b, s, p, objects := fixture(t)
	id, _ := IdentityFrom(b, s)
	f := &fakeArchive{t: t, b: b, s: s, p: p, objects: objects, id: id}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	client, err := api.New(api.Options{BaseURL: server.URL, Token: "synthetic-token"})
	if err != nil {
		t.Fatal(err)
	}
	return f, NewClient(client)
}
func (f *fakeArchive) send(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(api.HeaderContract, api.Contract)
	w.Header().Set(api.HeaderManifestRevision, strconv.Itoa(api.ManifestRevision))
	w.Header().Set(api.HeaderSchemaHash, "sha256:"+api.ManifestSchemaHash)
	w.Header().Set(api.HeaderDomain, "archive")
	data, err := canon(body)
	if err != nil {
		f.t.Error(err)
		http.Error(w, "encode", 500)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
func (f *fakeArchive) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		f.t.Errorf("legacy route %s", r.URL.Path)
		http.NotFound(w, r)
		return
	}
	path := r.URL.Path
	switch {
	case path == "/api/capabilities/archive":
		f.send(w, 200, Capabilities{Protocol: Protocol, Availability: "legacy-complete", Capabilities: []string{"archive-transfer-v1", "context-materials-v1"}, Limits: f.b.Limits, OperationEpoch: *f.b.OperationEpoch, ArchiveAckFormats: []string{"split-receipts-v1"}})
	case strings.HasSuffix(path, "/binding-target"):
		f.send(w, 200, BindingTarget{Protocol: Protocol, Target: f.b.Target, Revision: "0"})
	case path == "/api/archive/bindings" && r.Method == http.MethodPost:
		var input BindingCreateRequest
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &input); err != nil {
			f.t.Error(err)
		}
		if err := validate("BindingCreateRequest", input); err != nil {
			f.t.Error(err)
		}
		if input.Request.OperationEpoch != f.b.OperationEpoch.ID || input.ExpectedRevision != "0" || input.Archive.SourceID != f.b.SourceID || input.Target != f.b.Target || r.Header.Get(api.HeaderIdempotencyKey) != input.Request.RequestID {
			f.t.Error("create changed negotiated identity")
		}
		f.send(w, 201, f.b)
	case strings.HasSuffix(path, "/archive/status"):
		f.send(w, 200, f.s)
	case strings.HasSuffix(path, "/archive/records"):
		f.recordReads++
		p := f.p
		if r.URL.Query().Get("afterSequence") == "1" {
			p.Records = []Record{}
		}
		f.send(w, 200, p)
	case strings.Contains(path, "/archive/artifacts/"):
		f.chunkCalls++
		ref := f.p.Records[0].Payload
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		maximum, _ := strconv.Atoi(r.URL.Query().Get("maxBytes"))
		body := f.objects[ref.ArtifactID]
		part := body[offset:min(offset+maximum, len(body))]
		value := ArtifactChunk{Protocol: Protocol, BindingID: f.b.BindingID, ArtifactID: ref.ArtifactID, SourceID: ref.SourceID, Generations: f.b.Target.Generations, Offset: offset, Bytes: len(part), TotalBytes: len(body), SHA256: ref.SHA256, ChunkSHA256: digest(part), Base64: base64.StdEncoding.EncodeToString(part)}
		if f.badChunk {
			value.ChunkSHA256 = strings.Repeat("f", 64)
		}
		f.send(w, 200, value)
	case strings.HasSuffix(path, "/archive/acks"):
		data, _ := io.ReadAll(r.Body)
		if _, err := canonical.ParseStrict(data, canonical.Options{MaxBytes: 2 << 20}); err != nil {
			f.t.Error(err)
		}
		var ack Ack
		if err := json.Unmarshal(data, &ack); err != nil {
			f.t.Error(err)
		}
		f.acks = append(f.acks, ack)
		if r.Header.Get(api.HeaderIdempotencyKey) != ack.Request.RequestID || r.Header.Get(api.HeaderIfMatch) != strconv.Quote(ack.ExpectedRevision) {
			f.t.Error("missing stable three headers")
		}
		if f.dropFirst {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				f.t.Error(err)
			} else {
				_ = conn.Close()
			}
			return
		}
		receipt := receiptFor(f.t, f.id, ack)
		f.s.AcknowledgedCoverage = &ack.Coverage
		f.s.Revision = receipt.Revision
		f.b.Revision = receipt.Revision
		f.send(w, 200, receipt)
	case strings.HasSuffix(path, "/chunks"):
		var input MaterialUploadChunk
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &input); err != nil {
			f.t.Error(err)
		}
		f.uploaded++
		ref := f.p.Records[0].Payload
		f.send(w, 200, MaterialUploadStatus{Protocol: Protocol, BindingID: f.b.BindingID, MaterialRequestID: input.MaterialRequestID, UploadID: "upload-1", Artifact: ref, State: "committed", ChunkBytes: input.Bytes, ReceivedOffsets: []int{0}, ReceivedBytes: ref.Bytes, RemainingTTLMS: 1000})
	case strings.HasSuffix(path, "/material-responses"):
		var input MaterialResponse
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &input); err != nil {
			f.t.Error(err)
		}
		f.materialSubmitted++
		f.send(w, 202, MaterialReceipt{Protocol: Protocol, BindingID: f.b.BindingID, MaterialRequestID: input.MaterialRequestID, State: "received", Revision: "1", AcceptedRecordIDs: []string{"record-1"}})
	case strings.HasSuffix(path, "/materials/material-1"):
		f.send(w, 200, MaterialReceipt{Protocol: Protocol, BindingID: f.b.BindingID, MaterialRequestID: "material-1", State: "core-consumed", Revision: "2", AcceptedRecordIDs: []string{"record-1"}})
	case strings.HasSuffix(path, "/"+f.b.BindingID):
		f.send(w, 200, f.b)
	default:
		f.t.Errorf("unexpected route %s", path)
		http.NotFound(w, r)
	}
}
func TestSyncLostAckRecoversOriginalAfterRestart(t *testing.T) {
	f, client := fakeArchiveServer(t)
	f.dropFirst = true
	store, options := openFixture(t, f.b, f.s)
	if _, err := SyncOnce(context.Background(), client, store, "request-original"); err == nil {
		t.Fatal("lost ACK response treated success")
	}
	f.mu.Lock()
	f.dropFirst = false
	f.mu.Unlock()
	if pending, _ := store.Pending(); pending == nil {
		t.Fatal("lost response discarded durable ACK")
	}
	if coverage, _ := store.Coverage(); coverage != nil {
		t.Fatal("lost response advanced coverage")
	}
	_ = store.Close()
	reopened, err := OpenFileStore(options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	result, err := SyncOnce(context.Background(), client, reopened, "must-not-replace-original")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Recovered || result.Receipt == nil {
		t.Fatal("did not reconcile")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.acks) < 2 || !equal(f.acks[0], f.acks[len(f.acks)-1]) || f.recordReads != 1 || f.chunkCalls != 1 {
		t.Fatalf("recovery changed identity or re-downloaded: acks=%d records=%d chunks=%d", len(f.acks), f.recordReads, f.chunkCalls)
	}
}
func TestSyncRejectsBadChunkWithoutAck(t *testing.T) {
	f, client := fakeArchiveServer(t)
	f.badChunk = true
	store, _ := openFixture(t, f.b, f.s)
	if _, err := SyncOnce(context.Background(), client, store, "request-1"); !errors.Is(err, ErrIntegrity) {
		t.Fatal(err)
	}
	if len(f.acks) != 0 {
		t.Fatal("corrupt data acknowledged")
	}
}
func TestMaterialResponseIsNotCoreConsumption(t *testing.T) {
	f, client := fakeArchiveServer(t)
	store, _ := openFixture(t, f.b, f.s)
	if _, err := SyncOnce(context.Background(), client, store, "request-1"); err != nil {
		t.Fatal(err)
	}
	r := f.p.Records[0]
	request := MaterialRequest{Protocol: Protocol, BindingID: f.b.BindingID, MaterialRequestID: "material-1", Target: f.b.Target, SourceID: f.b.SourceID, SourceGeneration: f.s.SourceGeneration, RequestedRecords: []MaterialRecord{{r.RecordID, r.RecordDigest, r.Payload, r.Attachments}}, Purpose: "context-recall", MaxBytes: 65536, RemainingTTLMS: 10000, ChunkBytes: r.Payload.Bytes}
	result, err := RespondMaterials(context.Background(), client, store, request, RequestIdentity{"material-response-1", "epoch-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.State != "received" || result.Response.Request.RequestID != "material-response-1" {
		t.Fatal("upload confused with consumption")
	}
	status, err := client.MaterialStatus(context.Background(), f.b.BindingID, "material-1")
	if err != nil || status.State != "core-consumed" {
		t.Fatal("explicit status failed", err)
	}
	request.RequestedRecords[0].Digest = strings.Repeat("f", 64)
	if _, err = RespondMaterials(context.Background(), client, store, request, RequestIdentity{"material-response-2", "epoch-1"}); !errors.Is(err, ErrIntegrity) {
		t.Fatal("changed digest accepted", err)
	}
	if f.uploaded != 1 || f.materialSubmitted != 1 {
		t.Fatal("invalid material caused network mutation")
	}
}
func TestConcurrentStoreReadersAndIdempotentConfirm(t *testing.T) {
	b, s, p, objects := fixture(t)
	store, options := openFixture(t, b, s)
	ack, err := store.Receive(b, s, p, objects, RequestIdentity{"request-1", "epoch-1"})
	if err != nil {
		t.Fatal(err)
	}
	receipt := receiptFor(t, options.Identity, ack)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5 {
				if _, err := store.Body(p.Records[0].Payload); err != nil {
					t.Error(err)
				}
				if err := store.Confirm(receipt); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}

func TestSecondPagePreservesChainAndCapacity(t *testing.T) {
	b, status, page, objects := fixture(t)
	store, options := openFixture(t, b, status)
	ack, err := store.Receive(b, status, page, objects, RequestIdentity{"ack-1", "epoch-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Confirm(receiptFor(t, options.Identity, ack)); err != nil {
		t.Fatal(err)
	}
	b.Revision = "2"
	status.Revision = "2"
	status.AcknowledgedCoverage = &ack.Coverage
	two := "2"
	status.PublishedThroughSequence = &two
	record := clone(page.Records[0])
	record.RecordID = "record-2"
	record.Sequence = "2"
	record.PredecessorDigest = ack.Coverage.HeadDigest
	record.RecordDigest = recordHash(t, record)
	page.Records = []Record{record}
	page.NextAfterSequence = &two
	page.PublishedThroughSequence = &two
	ack2, err := store.Receive(b, status, page, objects, RequestIdentity{"ack-2", "epoch-1"})
	if err != nil {
		t.Fatal(err)
	}
	if ack2.Coverage.FromSequence != "2" || ack2.Coverage.ThroughSequence != "2" {
		t.Fatal("new page coverage was conflated with prior page")
	}
	if _, err = store.Receive(b, status, page, objects, RequestIdentity{"changed", "epoch-1"}); !errors.Is(err, ErrPendingAck) {
		t.Fatal("pending replaced", err)
	}
	if err = store.Confirm(receiptFor(t, options.Identity, ack2)); err != nil {
		t.Fatal(err)
	}
	records, err := store.ReadRecords("1", 128)
	if err != nil || len(records) != 2 {
		t.Fatal("prior history lost", err)
	}
	_ = store.Close()
	reopened, err := OpenFileStore(options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err = reopened.ReadRecords("1", 128)
	if err != nil || len(records) != 2 {
		t.Fatal("history did not survive reopen", err)
	}
}
func TestStoreTamperAndCapacityFailClosed(t *testing.T) {
	b, status, page, objects := fixture(t)
	store, options := openFixture(t, b, status)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(options.Path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err = os.WriteFile(options.Path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = OpenFileStore(options); !errors.Is(err, ErrIntegrity) {
		t.Fatal("tampered encrypted file accepted", err)
	}
	options.Path = filepath.Join(archiveTempDir(t), "small.bin")
	options.Limits = StoreLimits{1, 1, 1024, 1024}
	small, err := OpenFileStore(options)
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close()
	// Make the referenced artifact exceed the configured batch before any write.
	body := bytes.Repeat([]byte{'x'}, 2048)
	page.Records[0].Payload.Bytes = len(body)
	page.Records[0].Payload.SHA256 = digest(body)
	page.Records[0].PayloadDigest = domainDigest("tansr.sdk2.payload.v1", body)
	page.Records[0].RecordDigest = recordHash(t, page.Records[0])
	objects["artifact-1"] = body
	if _, err = small.Receive(b, status, page, objects, RequestIdentity{"ack-1", "epoch-1"}); !errors.Is(err, ErrCapacity) {
		t.Fatal("capacity not enforced", err)
	}
	if pending, _ := small.Pending(); pending != nil {
		t.Fatal("over-capacity batch prepared ACK")
	}
}
func TestPublicationRaceIsRetryableBeforeAnyCommit(t *testing.T) {
	b, status, page, objects := fixture(t)
	store, _ := openFixture(t, b, status)
	status.PublishedThroughSequence = nil
	if _, err := store.Receive(b, status, page, objects, RequestIdentity{"ack-1", "epoch-1"}); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatal("publication race not classified", err)
	}
	if pending, _ := store.Pending(); pending != nil {
		t.Fatal("publication race wrote pending ACK")
	}
}

func TestCreateUsesNegotiatedArchiveContract(t *testing.T) {
	f, client := fakeArchiveServer(t)
	binding, err := client.Create(context.Background(), f.b.Target.SessionID, f.b.SourceID, "create-request-1")
	if err != nil {
		t.Fatal(err)
	}
	if binding.BindingID != f.b.BindingID || binding.ArchiveAckFormat == nil || *binding.ArchiveAckFormat != "split-receipts-v1" {
		t.Fatal("binding not negotiated")
	}
}
