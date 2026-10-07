package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cpple/tansr-go/api"
)

func prepareRecoveryFixture(t *testing.T) (*FileStore, StoreOptions, Ack) {
	t.Helper()
	b, s, p, objects := fixture(t)
	store, options := openFixture(t, b, s)
	ack, err := store.Receive(b, s, p, objects, RequestIdentity{"original-ack", "epoch-1"})
	if err != nil {
		t.Fatal(err)
	}
	return store, options, ack
}
func rebaseReceiptFor(t *testing.T, id Identity, intent AckRebaseRequest) AckRebaseReceipt {
	t.Helper()
	next := clone(intent.Previous)
	next.Request = intent.Request
	next.ExpectedRevision = "9"
	return AckRebaseReceipt{Protocol: Protocol, BindingID: intent.BindingID, Previous: intent.Previous, Request: intent.Request, Next: next, Receipt: receiptFor(t, id, next)}
}
func TestRebaseStateMigrationAndReopen(t *testing.T) {
	store, options, ack := prepareRecoveryFixture(t)
	if store.state.Format != storeFormat {
		t.Fatal("receive silently migrated v1")
	}
	intent, err := store.PrepareRebase(RequestIdentity{"recovery-1", "epoch-1"})
	if err != nil {
		t.Fatal(err)
	}
	if store.state.Format != recoveryStoreFormat || !equal(intent.Previous, ack) {
		t.Fatal("migration replaced original ACK")
	}
	if _, err = store.PrepareRebase(RequestIdentity{"replacement-key", "epoch-1"}); !errors.Is(err, ErrPendingAck) {
		t.Fatal("replaced pending recovery", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenFileStore(options)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pending, err := store.PendingRebase()
	if err != nil || pending == nil || !equal(*pending, intent) {
		t.Fatal("durable recovery lost", err)
	}
	if original, _ := store.Pending(); original == nil || !equal(*original, ack) {
		t.Fatal("original was changed")
	}
	if coverage, _ := store.Coverage(); coverage != nil {
		t.Fatal("prepare falsely accepted coverage")
	}
	result := rebaseReceiptFor(t, options.Identity, intent)
	if err = store.ConfirmRebase(result); err != nil {
		t.Fatal(err)
	}
	if err = store.ConfirmRebase(result); err != nil {
		t.Fatal("same receipt not idempotent", err)
	}
	if err = store.Confirm(receiptFor(t, options.Identity, ack)); !errors.Is(err, ErrReceipt) {
		t.Fatal("fabricated old confirmation", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenFileStore(options)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if pending, _ := store.Pending(); pending != nil {
		t.Fatal("rebase did not clear pending")
	}
	if recovery, _ := store.PendingRebase(); recovery != nil {
		t.Fatal("rebase did not finish")
	}
	coverage, err := store.Coverage()
	if err != nil || coverage == nil || *coverage != ack.Coverage || len(store.state.Rebases) != 1 {
		t.Fatal("ledger/coverage not durable", err)
	}
	if !equal(store.state.Rebases[0].Intent.Previous, ack) {
		t.Fatal("original ACK absent from ledger")
	}
}

func TestRebaseRejectsInvalidResultAndIdentity(t *testing.T) {
	for _, kind := range []string{"same-key", "epoch", "previous", "coverage", "payload", "request", "equal-revision", "digest", "accepted", "foreign-scope", "reuse"} {
		t.Run(kind, func(t *testing.T) {
			store, options, ack := prepareRecoveryFixture(t)
			request := RequestIdentity{"recovery-1", "epoch-1"}
			if kind == "same-key" {
				request = ack.Request
			}
			if kind == "epoch" {
				request.OperationEpoch = "epoch-new"
			}
			intent, err := store.PrepareRebase(request)
			if kind == "same-key" || kind == "epoch" {
				if err == nil || store.state.Format != storeFormat {
					t.Fatal("invalid recovery prepared")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result := rebaseReceiptFor(t, options.Identity, intent)
			switch kind {
			case "previous":
				result.Previous.ExpectedRevision = "2"
			case "coverage":
				result.Next.Coverage.HeadDigest = strings.Repeat("a", 64)
			case "payload":
				result.Next.Payloads[0].SHA256 = strings.Repeat("f", 64)
			case "request":
				result.Next.Request.RequestID = "another"
			case "equal-revision":
				result.Next.ExpectedRevision = ack.ExpectedRevision
			case "digest":
				result.Receipt.SemanticDigest = strings.Repeat("0", 64)
			case "accepted":
				result.Receipt.State = "accepted"
			case "foreign-scope":
				other := options.Identity
				other.EndUserID = "other-user"
				result.Receipt = receiptFor(t, other, result.Next)
			case "reuse":
				if err = store.ConfirmRebase(result); err != nil {
					t.Fatal(err)
				}
				// A completed rebase identity may not become an original ACK for a new page.
				if !reservedRebaseIdentity(store.state, result.Next.Request) || !reservedRebaseIdentity(store.state, ack.Request) {
					t.Fatal("ledger identity not reserved")
				}
				return
			}
			if err = store.ConfirmRebase(result); err == nil {
				t.Fatal("invalid result accepted")
			}
			if current, _ := store.Pending(); current == nil || !equal(*current, ack) {
				t.Fatal("rejection lost original ACK")
			}
			if coverage, _ := store.Coverage(); coverage != nil {
				t.Fatal("rejection advanced coverage")
			}
		})
	}
}

func TestRebasePreparedOriginalReceiptWins(t *testing.T) {
	store, options, ack := prepareRecoveryFixture(t)
	intent, err := store.PrepareRebase(RequestIdentity{"recovery-1", "epoch-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Confirm(receiptFor(t, options.Identity, ack)); err != nil {
		t.Fatal(err)
	}
	if pending, _ := store.PendingRebase(); pending != nil {
		t.Fatal("original confirmation left recovery unresolved")
	}
	if err = store.ConfirmRebase(rebaseReceiptFor(t, options.Identity, intent)); !errors.Is(err, ErrReceipt) {
		t.Fatal("late different recovery accepted", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFileStore(options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.state.Rebases[0].OriginalReceipt == nil || reopened.state.Rebases[0].Result != nil {
		t.Fatal("original receipt ledger lost")
	}
}

func TestRebaseRejectsKnownCompletedOrdinaryAckIdentity(t *testing.T) {
	store, options, ack := prepareRecoveryFixture(t)
	receipt := receiptFor(t, options.Identity, ack)
	if err := store.Confirm(receipt); err != nil {
		t.Fatal(err)
	}
	b, status, page, objects := fixture(t)
	b.Revision, status.Revision = receipt.Revision, receipt.Revision
	status.AcknowledgedCoverage = &ack.Coverage
	sequence := "2"
	status.PublishedThroughSequence = &sequence
	page.PublishedThroughSequence, page.NextAfterSequence = &sequence, &sequence
	page.Records[0].Sequence, page.Records[0].RecordID = sequence, "record-2"
	page.Records[0].PredecessorDigest = ack.Coverage.HeadDigest
	page.Records[0].RecordDigest = recordHash(t, page.Records[0])
	pending, err := store.Receive(b, status, page, objects, RequestIdentity{"original-ack-2", "epoch-1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PrepareRebase(ack.Request); !errors.Is(err, ErrReceipt) {
		t.Fatal("known committed identity became recovery", err)
	}
	if store.state.Format != storeFormat || len(store.state.Rebases) != 0 {
		t.Fatal("rejected identity changed durable format")
	}
	if got, _ := store.Pending(); got == nil || !equal(*got, pending) {
		t.Fatal("rejected identity changed pending")
	}
}

type recoveryHTTP struct {
	t           *testing.T
	mu          sync.Mutex
	f           *fakeArchive
	store       *FileStore
	ack         Ack
	ackAccepted bool
	failure     string
	drop        bool
	intents     []AckRebaseRequest
	result      *AckRebaseReceipt
	ackCalls    int
}

func (f *recoveryHTTP) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	problem := func(status int, code, domain, reason string) {
		detail := map[string]any{"domainCode": domain, "domainStatus": status, "domainRetryAction": "none"}
		if reason != "" {
			detail["reason"] = reason
		}
		action := "none"
		if code == "precondition_failed" {
			action = "refresh"
		}
		f.f.send(w, status, map[string]any{"contract": "unified-v1", "traceId": "recovery-trace", "requestId": "recovery-request", "code": code, "status": status, "retryAction": action, "message": domain, "detail": detail})
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/archive/acks"):
		f.ackCalls++
		var ack Ack
		_ = json.NewDecoder(r.Body).Decode(&ack)
		if !equal(ack, f.ack) {
			f.t.Error("original ACK changed")
		}
		if f.ackAccepted {
			f.f.send(w, 200, receiptFor(f.t, f.f.id, ack))
			return
		}
		if f.failure == "forbidden" {
			problem(403, "forbidden", "forbidden", "")
			return
		}
		problem(412, "precondition_failed", "binding_conflict", "if_match_stale")
	case strings.HasSuffix(r.URL.Path, "/archive/ack-rebases"):
		var intent AckRebaseRequest
		_ = json.NewDecoder(r.Body).Decode(&intent)
		f.intents = append(f.intents, intent)
		stored, err := f.store.PendingRebase()
		if err != nil || stored == nil || !equal(*stored, intent) {
			f.t.Error("rebase sent before durable preparation", err)
		}
		if r.Header.Get(api.HeaderIdempotencyKey) != intent.Request.RequestID || r.Header.Get(api.HeaderIfMatch) != "" {
			f.t.Error("wrong recovery transport headers")
		}
		if f.failure == "busy" {
			problem(409, "conflict", "binding_conflict", "")
			return
		}
		if f.failure == "already-accepted" {
			problem(409, "conflict", "request_id_conflict", "")
			return
		}
		if f.result == nil {
			result := rebaseReceiptFor(f.t, f.f.id, intent)
			f.result = &result
		}
		if !equal(f.result.Previous, intent.Previous) || f.result.Request != intent.Request {
			f.t.Error("recovery key changed")
		}
		if f.drop {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				f.t.Error(err)
			} else {
				_ = conn.Close()
			}
			return
		}
		f.f.send(w, 200, *f.result)
	case strings.Contains(r.URL.Path, "/operations"):
		if r.URL.Query().Get("requestId") != f.ack.Request.RequestID {
			f.t.Error("queried replacement identity")
		}
		f.f.send(w, 200, receiptFor(f.t, f.f.id, f.ack))
	default:
		f.t.Error("unexpected recovery HTTP", r.URL.Path)
		http.NotFound(w, r)
	}
}
func recoveryServer(t *testing.T, store *FileStore, ack Ack) (*recoveryHTTP, *Client) {
	t.Helper()
	b, s, p, objects := fixture(t)
	f := &recoveryHTTP{t: t, store: store, ack: ack, f: &fakeArchive{t: t, b: b, s: s, p: p, objects: objects, id: store.Identity()}}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	c, err := api.New(api.Options{BaseURL: server.URL, Token: "synthetic-token"})
	if err != nil {
		t.Fatal(err)
	}
	return f, NewClient(c)
}
func TestRecoverPendingHTTPAndRestart(t *testing.T) {
	store, options, ack := prepareRecoveryFixture(t)
	f, client := recoveryServer(t, store, ack)
	f.drop = true
	if _, err := RecoverPending(context.Background(), client, store, "recovery-1"); err == nil {
		t.Fatal("lost response became success")
	}
	f.mu.Lock()
	f.drop = false
	f.mu.Unlock()
	if coverage, _ := store.Coverage(); coverage != nil {
		t.Fatal("lost response advanced coverage")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFileStore(options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	f.mu.Lock()
	f.store = reopened
	f.mu.Unlock()
	result, err := SyncOnce(context.Background(), client, reopened, "must-not-replace-recovery")
	if err != nil || !result.Recovered || result.Receipt == nil || result.Receipt.Request.RequestID != "recovery-1" {
		t.Fatal("original recovery not continued", result, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.intents) < 2 || f.ackCalls != 1 {
		t.Fatal("lost response changed recovery or repeated original ACK")
	}
	for _, intent := range f.intents {
		if !equal(intent, f.intents[0]) {
			t.Fatal("lost response changed recovery identity")
		}
	}
	if !bytes.Equal(reopened.state.Artifacts["artifact-1"].Body, f.f.objects["artifact-1"]) {
		t.Fatal("rebase altered bytes")
	}
}
func TestRecoverPendingGuardsAndOriginalResolution(t *testing.T) {
	for _, kind := range []string{"original-accepted", "forbidden", "busy", "capacity", "revoked", "already-accepted"} {
		t.Run(kind, func(t *testing.T) {
			store, options, ack := prepareRecoveryFixture(t)
			f, client := recoveryServer(t, store, ack)
			switch kind {
			case "original-accepted":
				f.ackAccepted = true
			case "forbidden", "busy", "already-accepted":
				f.failure = kind
			case "capacity":
				store.limits.MaxStoredBytes = 1024
			case "revoked":
				store.checkAccess = func(Identity) error { return errors.New("revoked") }
			}
			result, err := RecoverPending(context.Background(), client, store, "recovery-1")
			if kind == "original-accepted" || kind == "already-accepted" {
				if err != nil || result.Receipt == nil || result.Receipt.Request != ack.Request {
					t.Fatal("original acceptance not resolved", err)
				}
				if kind == "original-accepted" && store.state.Format != storeFormat {
					t.Fatal("original receipt needlessly migrated")
				}
				if kind == "already-accepted" {
					if err = store.Close(); err != nil {
						t.Fatal(err)
					}
					s, e := OpenFileStore(options)
					if e != nil {
						t.Fatal(e)
					}
					_ = s.Close()
				}
				return
			}
			if err == nil {
				t.Fatal("guard treated as success")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if kind == "busy" {
				if len(f.intents) != 1 {
					t.Fatal("busy path lost prepared attempt")
				}
				if p, _ := store.PendingRebase(); p == nil {
					t.Fatal("busy cleared intent")
				}
			} else if len(f.intents) != 0 || store.state.Format != storeFormat {
				t.Fatal("guard prepared or transmitted recovery")
			}
			if kind == "revoked" && f.ackCalls != 0 {
				t.Fatal("revoked store reached HTTP")
			}
		})
	}
}
