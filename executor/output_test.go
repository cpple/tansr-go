package executor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpple/tansr-go/api"
	"github.com/cpple/tansr-go/canonical"
	"github.com/cpple/tansr-go/internal/wire"
)

type outputRoundTrip func(*http.Request) (*http.Response, error)

func (fn outputRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func outputString(value string) *string { return &value }

func outputTestOptions() OutputOptions {
	return OutputOptions{Session: TerminalSessionReference{"sdk2-offload-v1", "session-1"},
		Operation: OutputOperationReference{"operation-1", strings.Repeat("a", 64)}, ExecutorID: "executor-1", ConnectionID: "connection-1",
		Limits: OutputLimits{262144, 16384, 65536, 2097152, 8388608}}
}

type outputPeer struct {
	t           *testing.T
	mu          sync.Mutex
	status      OutputStatus
	batches     []outputBatch
	rawBatches  []string
	captured    []byte
	blocks      []OutputBlock
	statusCalls int
	intercept   func(*http.Request, outputBatch, int) (*http.Response, error, bool)
}

func newOutputPeer(t *testing.T) *outputPeer {
	return &outputPeer{t: t, status: OutputStatus{Contract: outputContract, Operation: outputTestOptions().Operation,
		State: "available", NextByteOffset: outputString("0")}}
}

func (p *outputPeer) response(status int, value any) *http.Response {
	p.t.Helper()
	body, err := canonical.Encode(value, canonical.Options{MaxBytes: 262144})
	if err != nil {
		p.t.Errorf("encode fixture: %v", err)
		body = []byte("null")
	}
	h := make(http.Header)
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set(api.HeaderContract, api.Contract)
	h.Set(api.HeaderManifestRevision, strconv.Itoa(api.ManifestRevision))
	h.Set(api.HeaderDomain, "terminal")
	h.Set(api.HeaderSchemaHash, "none")
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}
}

func (p *outputPeer) client() *api.Client {
	p.t.Helper()
	client, err := api.New(api.Options{BaseURL: "http://output.test", Token: "synthetic-token", HTTPClient: &http.Client{Transport: outputRoundTrip(p.roundTrip)}})
	if err != nil {
		p.t.Fatal(err)
	}
	return client
}

func (p *outputPeer) roundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("Authorization") != "Bearer synthetic-token" {
		return nil, errors.New("missing synthetic auth")
	}
	if r.Method == "GET" {
		if r.URL.Path != "/api/terminal/sessions/session-1/tool-output-status" ||
			r.URL.Query().Get("contract") != outputContract || r.URL.Query().Get("sessionContract") != "sdk2-offload-v1" ||
			r.URL.Query().Get("operationId") != "operation-1" || r.URL.Query().Get("requestDigest") != strings.Repeat("a", 64) {
			return nil, errors.New("unexpected status route or identity")
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.statusCalls++
		return p.response(200, p.status), nil
	}
	if r.Method != "POST" || r.URL.Path != "/api/terminal/executors/executor-1/output-batches" {
		return nil, fmt.Errorf("unexpected output operation %s %s", r.Method, r.URL.Path)
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if _, err := canonical.ParseStrict(raw, canonical.Options{MaxBytes: 262144}); err != nil {
		return nil, err
	}
	if _, err := wire.Decode(outputContract, "OutputBatchRequest", raw); err != nil {
		return nil, err
	}
	var batch outputBatch
	if err := json.Unmarshal(raw, &batch); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.batches = append(p.batches, batch)
	p.rawBatches = append(p.rawBatches, string(raw))
	count := len(p.batches)
	p.mu.Unlock()
	if p.intercept != nil {
		if response, err, done := p.intercept(r, batch, count); done {
			return response, err
		}
	}
	return p.commit(batch)
}

func (p *outputPeer) commit(batch outputBatch) (*http.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if batch.Operation != p.status.Operation || batch.Session != outputTestOptions().Session || batch.ExecutorID != "executor-1" || batch.ConnectionID != "connection-1" {
		return nil, errors.New("wrong batch binding")
	}
	batchBytes := 0
	for _, block := range batch.Blocks {
		data, err := base64.StdEncoding.DecodeString(block.Base64)
		if err != nil || len(data) != block.ByteLength || fmt.Sprintf("%x", sha256.Sum256(data)) != block.PayloadDigest {
			return nil, errors.New("incorrect block integrity")
		}
		batchBytes += len(data)
		seq, _ := strconv.ParseInt(block.Seq, 10, 64)
		if seq <= outputSequence(p.status.AcceptedThrough) {
			if p.blocks[seq] != block {
				return nil, errors.New("non-identical replay")
			}
			continue
		}
		if seq != int64(len(p.blocks)) || block.ByteOffset != strconv.Itoa(len(p.captured)) {
			return nil, errors.New("noncontinuous output")
		}
		p.blocks = append(p.blocks, block)
		p.captured = append(p.captured, data...)
		p.status.State = "receiving"
		p.status.AcceptedThrough = outputString(block.Seq)
		p.status.RetainedFrom = outputString("0")
		p.status.NextByteOffset = outputString(strconv.Itoa(len(p.captured)))
	}
	if batchBytes > 65536 {
		return nil, errors.New("unbounded batch")
	}
	if batch.Seal != nil {
		if batch.Seal.TotalBytes != strconv.Itoa(len(p.captured)) || batch.Seal.PayloadDigest != fmt.Sprintf("%x", sha256.Sum256(p.captured)) ||
			outputSequence(batch.Seal.LastSeq) != outputSequence(p.status.AcceptedThrough) {
			return nil, errors.New("incorrect seal")
		}
		p.status.Seal = batch.Seal
		p.status.State = "complete"
		if batch.Seal.Truncated {
			p.status.State = "truncated"
		}
	}
	return p.response(200, p.status), nil
}

func outputWriterForTest(t *testing.T, peer *outputPeer, opts OutputOptions) *OutputWriter {
	t.Helper()
	writer, err := NewOutputWriter(context.Background(), peer.client(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(writer.Abort)
	return writer
}

func outputFinish(t *testing.T, writer *OutputWriter) (OutputStatus, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return writer.Finish(ctx, false)
}

func TestOutputWriterSharedSequencePreservesBytesAndSeal(t *testing.T) {
	peer := newOutputPeer(t)
	opts := outputTestOptions()
	opts.Limits.MaxBlockBytes = 2
	writer := outputWriterForTest(t, peer, opts)
	chinese := []byte("中")
	if n, err := writer.Stdout().Write(chinese[:1]); n != 1 || err != nil {
		t.Fatalf("stdout = %d, %v", n, err)
	}
	_, _ = writer.Stderr().Write([]byte("!"))
	_, _ = writer.Stdout().Write(chinese[1:])
	chinese[0] = 0 // Caller memory is not retained.
	status, err := outputFinish(t, writer)
	if err != nil || status.State != "complete" || status.DurableThrough != nil {
		t.Fatalf("finish = %+v, %v", status, err)
	}
	peer.mu.Lock()
	if len(peer.blocks) != 3 || !bytes.Equal(peer.captured, []byte{0xe4, '!', 0xb8, 0xad}) {
		t.Errorf("blocks/bytes = %v / %x", peer.blocks, peer.captured)
	}
	for i, block := range peer.blocks {
		if block.Seq != strconv.Itoa(i) || block.ByteOffset != strconv.Itoa(i) || block.Encoding != "binary" {
			t.Errorf("unexpected block %d: %+v", i, block)
		}
	}
	if last := peer.batches[len(peer.batches)-1]; len(last.Blocks) != 0 || last.Seal == nil {
		t.Error("seal was not sent independently")
	}
	peer.mu.Unlock()
	if snapshot := writer.Snapshot(); !snapshot.Sealed || snapshot.PendingBytes != 0 || snapshot.Failed {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	*status.Seal.LastSeq = "999" // Returned DTO cannot mutate the writer's stored ACK.
	again, err := outputFinish(t, writer)
	if err != nil || *again.Seal.LastSeq != "2" {
		t.Fatalf("mutable completion snapshot: %+v, %v", again, err)
	}
	if _, err := writer.Capture("stdout", []byte("late")); !errors.Is(err, ErrOutputClosed) {
		t.Fatalf("capture after seal = %v", err)
	}
}

func TestOutputWriterEmptySeal(t *testing.T) {
	peer := newOutputPeer(t)
	writer := outputWriterForTest(t, peer, outputTestOptions())
	status, err := outputFinish(t, writer)
	if err != nil || status.State != "complete" || status.Seal.LastSeq != nil || status.Seal.TotalBytes != "0" {
		t.Fatalf("empty finish = %+v, %v", status, err)
	}
}

func TestOutputWriterLostACKAndIdenticalReplay(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(fmt.Sprintf("accepted=%v", accepted), func(t *testing.T) {
			peer := newOutputPeer(t)
			peer.intercept = func(_ *http.Request, batch outputBatch, count int) (*http.Response, error, bool) {
				if count != 1 {
					return nil, nil, false
				}
				if accepted {
					response, err := peer.commit(batch)
					if err != nil {
						return nil, err, true
					}
					_ = response.Body.Close()
				}
				return nil, errors.New("synthetic ACK disconnect"), true
			}
			writer := outputWriterForTest(t, peer, outputTestOptions())
			_, _ = writer.Stdout().Write([]byte("once"))
			status, err := outputFinish(t, writer)
			if err != nil || status.State != "complete" {
				t.Fatalf("finish = %+v, %v", status, err)
			}
			peer.mu.Lock()
			defer peer.mu.Unlock()
			if peer.statusCalls != 1 || string(peer.captured) != "once" || len(peer.blocks) != 1 {
				t.Fatalf("replayed effect: queries=%d bytes=%q blocks=%d", peer.statusCalls, peer.captured, len(peer.blocks))
			}
			if !accepted && peer.rawBatches[0] != peer.rawBatches[1] {
				t.Error("replay changed original batch bytes")
			}
		})
	}
}

func TestOutputWriterBoundIncludesInFlightAndDrainsAfterTruncation(t *testing.T) {
	peer := newOutputPeer(t)
	started, release := make(chan struct{}), make(chan struct{})
	peer.intercept = func(r *http.Request, _ outputBatch, count int) (*http.Response, error, bool) {
		if count == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err(), true
			}
		}
		return nil, nil, false
	}
	opts := outputTestOptions()
	opts.Limits.MaxPendingBytes = 1024
	opts.Limits.MaxBlockBytes = 10
	writer := outputWriterForTest(t, peer, opts)
	_, _ = writer.Stdout().Write(bytes.Repeat([]byte{'a'}, 10))
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upload did not start")
	}
	for n := 0; n < 100; n++ {
		if n, err := writer.Stderr().Write(bytes.Repeat([]byte{'b'}, 10)); n != 10 || err != nil {
			t.Fatalf("pipe not drained: %d %v", n, err)
		}
	}
	full := writer.Snapshot()
	if full.PendingBytes > 1024 || !full.Truncated || full.DroppedBytes == "0" {
		t.Fatalf("unbounded queue: %+v", full)
	}
	close(release)
	status, err := outputFinish(t, writer)
	if err != nil || status.State != "truncated" || status.Seal.TotalBytes != full.CapturedBytes {
		t.Fatalf("truncated finish: %+v %v", status, err)
	}
}

func TestOutputWriterUnknownWatermarkRetainsPending(t *testing.T) {
	peer := newOutputPeer(t)
	peer.status.State = "unavailable"
	peer.status.NextByteOffset = nil
	peer.intercept = func(*http.Request, outputBatch, int) (*http.Response, error, bool) {
		return nil, errors.New("disconnected"), true
	}
	writer := outputWriterForTest(t, peer, outputTestOptions())
	_, _ = writer.Stderr().Write([]byte("retained"))
	_, err := outputFinish(t, writer)
	if !errors.Is(err, ErrOutputGap) || writer.Snapshot().PendingBytes == 0 || writer.Snapshot().Sealed {
		t.Fatalf("unknown watermark = %v, %+v", err, writer.Snapshot())
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if len(peer.batches) != 1 || peer.statusCalls != 1 {
		t.Fatalf("gap retried upload: %d %d", len(peer.batches), peer.statusCalls)
	}
}

func TestOutputWriterExplicitReconcileResumesOriginalPendingBytes(t *testing.T) {
	peer := newOutputPeer(t)
	peer.status.State = "unavailable"
	peer.status.NextByteOffset = nil
	peer.intercept = func(*http.Request, outputBatch, int) (*http.Response, error, bool) {
		return nil, errors.New("temporarily disconnected"), true
	}
	writer := outputWriterForTest(t, peer, outputTestOptions())
	_, _ = writer.Stdout().Write([]byte("original"))
	if _, err := outputFinish(t, writer); !errors.Is(err, ErrOutputGap) {
		t.Fatalf("expected unavailable output, got %v", err)
	}
	// The first pump has completed. Restore the peer's original zero watermark;
	// no new writer, operation, sequence or tool execution is introduced.
	peer.mu.Lock()
	peer.status.State, peer.status.NextByteOffset = "available", outputString("0")
	peer.intercept = nil
	peer.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := writer.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if status, err := outputFinish(t, writer); err != nil || status.State != "complete" {
		t.Fatalf("reconciled finish = %+v %v", status, err)
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if string(peer.captured) != "original" || len(peer.blocks) != 1 || peer.rawBatches[0] != peer.rawBatches[1] {
		t.Fatal("reconciliation changed or duplicated original output")
	}
}

func TestOutputWriterRetryBoundDoesNotInventCompletion(t *testing.T) {
	peer := newOutputPeer(t)
	peer.intercept = func(*http.Request, outputBatch, int) (*http.Response, error, bool) {
		return nil, errors.New("synthetic lost request"), true
	}
	writer := outputWriterForTest(t, peer, outputTestOptions())
	_, _ = writer.Stdout().Write([]byte("original"))
	if _, err := outputFinish(t, writer); err == nil {
		t.Fatal("unacknowledged prefix reported complete")
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if len(peer.batches) != 2 || peer.statusCalls != 2 || peer.rawBatches[0] != peer.rawBatches[1] {
		t.Fatalf("unexpected retries: posts=%d status=%d", len(peer.batches), peer.statusCalls)
	}
	if writer.Snapshot().PendingBytes == 0 || writer.Snapshot().Sealed {
		t.Fatal("unknown outcome discarded pending bytes")
	}
}

func TestOutputWriterRejectsForgedStatusBeforeDroppingBytes(t *testing.T) {
	for _, scenario := range []string{"other-operation", "future-ack", "wrong-offset", "false-durability", "noncanonical"} {
		t.Run(scenario, func(t *testing.T) {
			peer := newOutputPeer(t)
			peer.intercept = func(_ *http.Request, batch outputBatch, _ int) (*http.Response, error, bool) {
				status := OutputStatus{Contract: outputContract, Operation: batch.Operation, State: "receiving",
					AcceptedThrough: outputString("0"), NextByteOffset: outputString("1"), RetainedFrom: outputString("0")}
				switch scenario {
				case "other-operation":
					status.Operation.OperationID = "alien"
				case "future-ack":
					status.AcceptedThrough, status.NextByteOffset = outputString("1"), outputString("2")
				case "wrong-offset":
					status.NextByteOffset = outputString("2")
				case "false-durability":
					status.DurableThrough = outputString("1")
				}
				response := peer.response(200, status)
				if scenario == "noncanonical" {
					body, _ := io.ReadAll(response.Body)
					response.Body = io.NopCloser(bytes.NewReader(append(body, '\n')))
					response.ContentLength++
				}
				return response, nil, true
			}
			writer := outputWriterForTest(t, peer, outputTestOptions())
			_, _ = writer.Stdout().Write([]byte("x"))
			_, err := outputFinish(t, writer)
			if err == nil || writer.Snapshot().PendingBytes == 0 || writer.Snapshot().Sealed {
				t.Fatalf("forged ACK accepted: %v %+v", err, writer.Snapshot())
			}
		})
	}
}

func TestOutputWriterAuthorizationDoesNotReconcile(t *testing.T) {
	peer := newOutputPeer(t)
	peer.intercept = func(*http.Request, outputBatch, int) (*http.Response, error, bool) {
		return peer.response(401, map[string]any{"contract": api.Contract, "traceId": strings.Repeat("a", 32), "requestId": nil,
			"code": "unauthorized", "status": 401, "retryAction": "none", "message": "authentication required"}), nil, true
	}
	writer := outputWriterForTest(t, peer, outputTestOptions())
	_, _ = writer.Stdout().Write([]byte("x"))
	_, err := outputFinish(t, writer)
	var apiErr *api.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeUnauthorized {
		t.Fatalf("expected auth error: %v", err)
	}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.statusCalls != 0 || len(peer.batches) != 1 {
		t.Fatal("unauthorized writer attempted reconciliation/replay")
	}
}

func TestOutputWriterFinishCancellationStopsUpload(t *testing.T) {
	peer := newOutputPeer(t)
	started, cancelled := make(chan struct{}), make(chan struct{})
	peer.intercept = func(r *http.Request, _ outputBatch, _ int) (*http.Response, error, bool) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
		return nil, r.Context().Err(), true
	}
	writer := outputWriterForTest(t, peer, outputTestOptions())
	_, _ = writer.Stdout().Write([]byte("prefix"))
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upload did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := writer.Finish(ctx, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled finish = %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight upload was not cancelled")
	}
	if writer.Snapshot().Sealed || writer.Snapshot().PendingBytes == 0 {
		t.Fatal("cancellation invented an ACK")
	}
}

func TestOutputStatusCrossFieldConstraints(t *testing.T) {
	emptyDigest := sha256.Sum256(nil)
	for _, valid := range []OutputStatus{
		{Contract: outputContract, Operation: outputTestOptions().Operation, State: "available", NextByteOffset: outputString("0")},
		{Contract: outputContract, Operation: outputTestOptions().Operation, State: "unavailable"},
		{Contract: outputContract, Operation: outputTestOptions().Operation, State: "complete", NextByteOffset: outputString("0"),
			Seal: &OutputSeal{TotalBytes: "0", PayloadDigest: hex.EncodeToString(emptyDigest[:])}},
	} {
		if err := validateOutputStatus(valid); err != nil {
			t.Fatalf("valid status rejected: %+v %v", valid, err)
		}
	}
	for _, invalid := range []OutputStatus{
		{State: "receiving", NextByteOffset: outputString("0")},
		{State: "available", AcceptedThrough: outputString("0"), NextByteOffset: outputString("1")},
		{State: "complete", NextByteOffset: outputString("0")},
		{State: "truncated", NextByteOffset: outputString("0"), Seal: &OutputSeal{TotalBytes: "0", PayloadDigest: hex.EncodeToString(emptyDigest[:])}},
		{State: "receiving", AcceptedThrough: outputString("0"), NextByteOffset: outputString("1"), RetainedFrom: outputString("1")},
		{State: "unavailable", NextByteOffset: outputString("1")},
	} {
		if err := validateOutputStatus(invalid); err == nil {
			t.Fatalf("invalid status accepted: %+v", invalid)
		}
	}
}
