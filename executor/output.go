package executor

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/tansrai/tansr-go/api"
	"github.com/tansrai/tansr-go/canonical"
	"github.com/tansrai/tansr-go/internal/wire"
)

const outputContract = "terminal-services-v1"

var (
	// ErrOutputGap means Serve cannot establish the original output watermark.
	// It is not permission to restart the operation or resend from zero.
	ErrOutputGap = errors.New("executor: output watermark unavailable")
	// ErrOutputIntegrity rejects a response for another operation or an impossible ACK.
	ErrOutputIntegrity = errors.New("executor: output integrity mismatch")
	// ErrOutputUnknown means the original batch or seal has not been acknowledged.
	ErrOutputUnknown = errors.New("executor: output acknowledgement unknown")
	// ErrOutputClosed rejects new capture after Finish has fixed the seal.
	ErrOutputClosed = errors.New("executor: output capture already finished")
)

// TerminalSessionReference identifies an existing session, not an authorization grant.
type TerminalSessionReference struct {
	SessionContract string `json:"sessionContract"`
	SessionID       string `json:"sessionId"`
}

type OutputOperationReference struct {
	OperationID   string `json:"operationId"`
	RequestDigest string `json:"requestDigest"`
}

// OutputLimits must come from the negotiated terminal binding. The writer does
// not increase any limit, and validates them against the frozen terminal schema.
type OutputLimits struct {
	MaxControlBytes  int `json:"maxControlBytes"`
	MaxBlockBytes    int `json:"maxBlockBytes"`
	MaxBatchBytes    int `json:"maxBatchBytes"`
	MaxPendingBytes  int `json:"maxPendingBytes"`
	MaxRetainedBytes int `json:"maxRetainedBytes"`
}

type OutputBlock struct {
	Seq           string `json:"seq"`
	ByteOffset    string `json:"byteOffset"`
	Channel       string `json:"channel"`
	Encoding      string `json:"encoding"`
	ByteLength    int    `json:"byteLength"`
	PayloadDigest string `json:"payloadDigest"`
	Base64        string `json:"base64"`
}

type OutputSeal struct {
	LastSeq       *string `json:"lastSeq"`
	TotalBytes    string  `json:"totalBytes"`
	PayloadDigest string  `json:"payloadDigest"`
	Truncated     bool    `json:"truncated"`
}

type OutputStatus struct {
	Contract        string                   `json:"contract"`
	Operation       OutputOperationReference `json:"operation"`
	State           string                   `json:"state"`
	AcceptedThrough *string                  `json:"acceptedThrough"`
	DurableThrough  *string                  `json:"durableThrough"`
	RetainedFrom    *string                  `json:"retainedFrom"`
	NextByteOffset  *string                  `json:"nextByteOffset"`
	Seal            *OutputSeal              `json:"seal"`
}

type outputBatch struct {
	Contract     string                   `json:"contract"`
	Session      TerminalSessionReference `json:"session"`
	Operation    OutputOperationReference `json:"operation"`
	ExecutorID   string                   `json:"executorId"`
	ConnectionID string                   `json:"connectionId"`
	Blocks       []OutputBlock            `json:"blocks"`
	Seal         *OutputSeal              `json:"seal"`
}

type OutputOptions struct {
	Session      TerminalSessionReference
	Operation    OutputOperationReference
	ExecutorID   string
	ConnectionID string
	Limits       OutputLimits
	// Encoding defaults to binary. Bytes split across chunks are never decoded
	// or repaired; hosts may opt into utf-8 only when they know the source encoding.
	Encoding string
}

type OutputSnapshot struct {
	PendingBytes  int
	PendingBlocks int
	CapturedBytes string
	DroppedBytes  string
	Truncated     bool
	Sealed        bool
	Failed        bool
}

type pendingOutput struct {
	block OutputBlock
	cost  int
}

// OutputWriter uploads observation bytes for one already-dispatched operation.
// The host must first negotiate execution-stream-v1 and validate its terminal
// scope/execution binding. IDs and limits passed here do not establish authority;
// Serve still authorizes every request. The writer never starts or restarts tools.
//
// Capture and the two io.Writers are safe for concurrent stdout/stderr pipes.
// Upload is asynchronous, with an encoded-body bound including in-flight blocks.
// When full, capture truncates the continuous prefix and keeps draining the pipes.
// Finish is the completion barrier; successful pipe writes alone do not mean ACK.
type OutputWriter struct {
	mu          sync.Mutex
	client      *api.Client
	opts        OutputOptions
	ctx         context.Context
	cancel      context.CancelFunc
	hash        hash.Hash
	pending     []pendingOutput
	pendingSize int
	next        int64
	offset      int64
	ack         int64
	ackOffset   int64
	sent        int64
	dropped     uint64
	truncated   bool
	seal        *OutputSeal
	status      *OutputStatus
	sealed      bool
	err         error
	running     bool
	reconciling bool
	changed     chan struct{}
}

func NewOutputWriter(ctx context.Context, client *api.Client, opts OutputOptions) (*OutputWriter, error) {
	if ctx == nil || client == nil {
		return nil, errors.New("executor: output context and client are required")
	}
	for _, check := range []struct {
		name  string
		value any
	}{{"SessionReference", opts.Session}, {"OperationReference", opts.Operation}, {"Id", opts.ExecutorID}, {"Id", opts.ConnectionID}, {"Limits", opts.Limits}} {
		if err := wire.Validate(outputContract, check.name, check.value); err != nil {
			return nil, err
		}
	}
	if opts.Encoding == "" {
		opts.Encoding = "binary"
	}
	if opts.Encoding != "binary" && opts.Encoding != "utf-8" {
		return nil, errors.New("executor: output encoding must be binary or utf-8")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	return &OutputWriter{client: client, opts: opts, ctx: ctx, cancel: cancel,
		hash: sha256.New(), ack: -1, sent: -1, changed: make(chan struct{})}, nil
}

func (w *OutputWriter) notifyLocked() {
	close(w.changed)
	w.changed = make(chan struct{})
}

func (w *OutputWriter) dropLocked(n int) {
	if n == 0 {
		return
	}
	w.truncated = true
	// Saturate only the diagnostic counter; protocol sequence/offset never wrap.
	if uint64(n) > math.MaxUint64-w.dropped {
		w.dropped = math.MaxUint64
	} else {
		w.dropped += uint64(n)
	}
}

// Capture retains a bounded prefix and returns the number of captured bytes.
// A short count with nil error means truncation; subsequent capture keeps draining
// without extending that prefix. Use Finish to observe upload errors and seal ACK.
func (w *OutputWriter) Capture(channel string, p []byte) (int, error) {
	if channel != "stdout" && channel != "stderr" {
		return 0, errors.New("executor: output channel must be stdout or stderr")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.seal != nil {
		return 0, ErrOutputClosed
	}
	if w.truncated || w.err != nil || w.ctx.Err() != nil {
		w.dropLocked(len(p))
		return 0, nil
	}
	maximum := min(w.opts.Limits.MaxBlockBytes, w.opts.Limits.MaxBatchBytes)
	accepted := 0
	for accepted < len(p) {
		n := min(maximum, len(p)-accepted)
		if w.next == math.MaxInt64 || int64(n) > math.MaxInt64-w.offset {
			w.dropLocked(len(p) - accepted)
			break
		}
		part := p[accepted : accepted+n]
		digest := sha256.Sum256(part)
		block := OutputBlock{Seq: strconv.FormatInt(w.next, 10), ByteOffset: strconv.FormatInt(w.offset, 10),
			Channel: channel, Encoding: w.opts.Encoding, ByteLength: n,
			PayloadDigest: hex.EncodeToString(digest[:]), Base64: base64.StdEncoding.EncodeToString(part)}
		encoded, err := canonical.Encode(block, canonical.Options{MaxBytes: w.opts.Limits.MaxControlBytes})
		if err != nil {
			w.err = err
			w.dropLocked(len(p) - accepted)
			w.notifyLocked()
			return accepted, err
		}
		if len(encoded) > w.opts.Limits.MaxPendingBytes-w.pendingSize {
			w.dropLocked(len(p) - accepted)
			break
		}
		_, _ = w.hash.Write(part)
		w.pending = append(w.pending, pendingOutput{block: block, cost: len(encoded)})
		w.pendingSize += len(encoded)
		w.next++
		w.offset += int64(n)
		accepted += n
	}
	w.startLocked()
	return accepted, nil
}

type outputPipe struct {
	writer  *OutputWriter
	channel string
}

func (p outputPipe) Write(data []byte) (int, error) {
	_, err := p.writer.Capture(p.channel, data)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

// Stdout drains stdout even when the bounded upload prefix has been truncated.
// Finish, not Write, reports the remote completion and truncation state.
func (w *OutputWriter) Stdout() io.Writer { return outputPipe{w, "stdout"} }

// Stderr shares stdout's sequence and byte-offset space in capture order.
func (w *OutputWriter) Stderr() io.Writer { return outputPipe{w, "stderr"} }

func (w *OutputWriter) Snapshot() OutputSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return OutputSnapshot{PendingBytes: w.pendingSize, PendingBlocks: len(w.pending),
		CapturedBytes: strconv.FormatInt(w.offset, 10), DroppedBytes: strconv.FormatUint(w.dropped, 10),
		Truncated: w.truncated, Sealed: w.sealed, Failed: w.err != nil || w.ctx.Err() != nil}
}

// Abort stops this writer's uploads without inventing an ACK or deleting pending
// bytes. A new writer must never be used to replay the same operation from zero.
func (w *OutputWriter) Abort() { w.cancel() }

func (w *OutputWriter) startLocked() {
	if w.running || w.reconciling || w.err != nil || w.sealed || w.ctx.Err() != nil || len(w.pending) == 0 && w.seal == nil {
		return
	}
	w.running = true
	go func() {
		err := w.pump()
		w.mu.Lock()
		defer w.mu.Unlock()
		w.running = false
		if err != nil {
			w.err = err
		}
		w.notifyLocked()
		// Capture may append after pump observes an empty queue but before it
		// clears running. Start again only when actual work remains.
		w.startLocked()
	}()
}

func (w *OutputWriter) batchLocked(blocks []OutputBlock, seal *OutputSeal) outputBatch {
	return outputBatch{Contract: outputContract, Session: w.opts.Session, Operation: w.opts.Operation,
		ExecutorID: w.opts.ExecutorID, ConnectionID: w.opts.ConnectionID, Blocks: blocks, Seal: seal}
}

func (w *OutputWriter) nextBatch() (outputBatch, int64, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return outputBatch{}, 0, false, w.err
	}
	if w.sealed || len(w.pending) == 0 && w.seal == nil {
		return outputBatch{}, 0, false, nil
	}
	blocks := make([]OutputBlock, 0, min(32, len(w.pending)))
	bytes := 0
	for _, pending := range w.pending {
		if len(blocks) == 32 || pending.block.ByteLength > w.opts.Limits.MaxBatchBytes-bytes {
			break
		}
		candidate := append(blocks, pending.block)
		if _, err := canonical.Encode(w.batchLocked(candidate, nil), canonical.Options{MaxBytes: w.opts.Limits.MaxControlBytes}); err != nil {
			break
		}
		blocks = candidate
		bytes += pending.block.ByteLength
	}
	if len(w.pending) > 0 && len(blocks) == 0 {
		return outputBatch{}, 0, false, errors.New("executor: output block cannot fit negotiated control limit")
	}
	var seal *OutputSeal
	if len(blocks) == 0 {
		seal = w.seal
	} else {
		w.sent, _ = strconv.ParseInt(blocks[len(blocks)-1].Seq, 10, 64)
	}
	body := w.batchLocked(blocks, seal)
	if _, err := canonical.Encode(body, canonical.Options{MaxBytes: w.opts.Limits.MaxControlBytes}); err != nil {
		return outputBatch{}, 0, false, err
	}
	return body, w.sent, true, nil
}

func (w *OutputWriter) pump() error {
	for {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		body, expected, exists, err := w.nextBatch()
		if err != nil || !exists {
			return err
		}
		if err := wire.Validate(outputContract, "OutputBatchRequest", body); err != nil {
			return err
		}
		var last error
		for attempt := 0; attempt < 2; attempt++ {
			ctx, cancel := context.WithTimeout(w.ctx, 30*time.Second)
			result, err := w.client.Call(ctx, api.OpTerminalOutputBatch, api.CallOptions{
				Params: map[string]string{"id": w.opts.ExecutorID}, Body: body,
				MaxResponseBytes: int64(w.opts.Limits.MaxControlBytes),
			})
			if err == nil {
				var status OutputStatus
				status, err = decodeOutputStatus(result, w.opts.Limits.MaxControlBytes)
				if err == nil {
					err = w.accept(status)
				}
				if err != nil {
					cancel()
					return err
				}
				if w.acknowledged(expected, body.Seal != nil) {
					cancel()
					last = nil
					break
				}
				err = ErrOutputUnknown
			}
			last = err
			if ctx.Err() != nil || outputAuthorizationError(err) {
				cancel()
				return err
			}
			// A missing POST response is not proof of non-acceptance. Query only
			// this operation, then at most replay the identical original batch.
			_, err = w.queryStatus(ctx)
			cancel()
			if err != nil {
				return err
			}
			if w.acknowledged(expected, body.Seal != nil) {
				last = nil
				break
			}
		}
		if last != nil {
			return last
		}
	}
}

func outputAuthorizationError(err error) bool {
	var apiErr *api.APIError
	return errors.As(err, &apiErr) && (apiErr.Code == api.CodeUnauthorized || apiErr.Code == api.CodeForbidden)
}

func (w *OutputWriter) acknowledged(expected int64, seal bool) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ack >= expected && (!seal || w.sealed)
}

func (w *OutputWriter) queryStatus(ctx context.Context) (OutputStatus, error) {
	result, err := w.client.Call(ctx, api.OpTerminalOutputStatus, api.CallOptions{
		Params: map[string]string{"id": w.opts.Session.SessionID},
		Query: map[string]string{"contract": outputContract, "sessionContract": w.opts.Session.SessionContract,
			"operationId": w.opts.Operation.OperationID, "requestDigest": w.opts.Operation.RequestDigest},
		MaxResponseBytes: int64(w.opts.Limits.MaxControlBytes),
	})
	if err != nil {
		return OutputStatus{}, err
	}
	status, err := decodeOutputStatus(result, w.opts.Limits.MaxControlBytes)
	if err == nil {
		err = w.accept(status)
	}
	return status, err
}

func (w *OutputWriter) accept(status OutputStatus) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if status.Operation != w.opts.Operation {
		return ErrOutputIntegrity
	}
	if status.State == "unavailable" || status.State == "gap" || status.NextByteOffset == nil {
		return ErrOutputGap
	}
	ack := outputSequence(status.AcceptedThrough)
	if ack < w.ack || ack > w.sent {
		return ErrOutputIntegrity
	}
	offset := w.ackOffset
	if ack != w.ack {
		found := false
		for _, item := range w.pending {
			seq, _ := strconv.ParseInt(item.block.Seq, 10, 64)
			if seq == ack {
				start, _ := strconv.ParseInt(item.block.ByteOffset, 10, 64)
				offset = start + int64(item.block.ByteLength)
				found = true
				break
			}
		}
		if !found {
			return ErrOutputIntegrity
		}
	}
	if strconv.FormatInt(offset, 10) != *status.NextByteOffset || status.Seal != nil && !outputSealEqual(status.Seal, w.seal) {
		return ErrOutputIntegrity
	}
	removed := 0
	for _, item := range w.pending {
		seq, _ := strconv.ParseInt(item.block.Seq, 10, 64)
		if seq > ack {
			break
		}
		w.pendingSize -= item.cost
		w.pending[removed] = pendingOutput{}
		removed++
	}
	w.pending = w.pending[removed:]
	if len(w.pending) == 0 {
		w.pending = nil
	}
	w.ack, w.ackOffset = ack, offset
	copy := cloneOutputStatus(status)
	w.status = &copy
	w.sealed = status.Seal != nil
	w.notifyLocked()
	return nil
}

// Reconcile waits for any current upload and checks the original watermark. It
// resumes only the remaining original blocks; no tool execution is retried.
func (w *OutputWriter) Reconcile(ctx context.Context) (OutputStatus, error) {
	if ctx == nil {
		return OutputStatus{}, errors.New("executor: output reconcile context is required")
	}
	for {
		w.mu.Lock()
		if !w.running && !w.reconciling {
			w.reconciling = true
			w.mu.Unlock()
			break
		}
		changed := w.changed
		w.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return OutputStatus{}, ctx.Err()
		case <-w.ctx.Done():
			return OutputStatus{}, w.ctx.Err()
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	stop := context.AfterFunc(w.ctx, cancel)
	status, err := w.queryStatus(requestCtx)
	stop()
	cancel()
	w.mu.Lock()
	w.reconciling = false
	if err == nil {
		w.err = nil
	}
	w.notifyLocked()
	w.startLocked()
	w.mu.Unlock()
	return status, err
}

// Finish fixes the byte-prefix seal and waits for its ACK. An empty output still
// sends and acknowledges an explicit empty seal. Cancellation aborts in-flight
// requests and retains unresolved bytes; it never reports completion.
func (w *OutputWriter) Finish(ctx context.Context, truncated bool) (OutputStatus, error) {
	if ctx == nil {
		return OutputStatus{}, errors.New("executor: output finish context is required")
	}
	w.mu.Lock()
	if w.seal == nil {
		w.truncated = w.truncated || truncated
		var last *string
		if w.next > 0 {
			value := strconv.FormatInt(w.next-1, 10)
			last = &value
		}
		w.seal = &OutputSeal{LastSeq: last, TotalBytes: strconv.FormatInt(w.offset, 10),
			PayloadDigest: hex.EncodeToString(w.hash.Sum(nil)), Truncated: w.truncated}
	}
	for {
		if err := ctx.Err(); err != nil {
			w.mu.Unlock()
			w.cancel()
			return OutputStatus{}, err
		}
		if err := w.ctx.Err(); err != nil {
			w.mu.Unlock()
			return OutputStatus{}, err
		}
		if w.err != nil {
			err := w.err
			w.mu.Unlock()
			return OutputStatus{}, err
		}
		if w.sealed && w.status != nil {
			status := cloneOutputStatus(*w.status)
			w.mu.Unlock()
			return status, nil
		}
		w.startLocked()
		changed := w.changed
		w.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			w.cancel()
			return OutputStatus{}, ctx.Err()
		case <-w.ctx.Done():
			return OutputStatus{}, w.ctx.Err()
		}
		w.mu.Lock()
	}
}

func decodeOutputStatus(result *api.Result, maximum int) (OutputStatus, error) {
	if result.Status != 200 || result.ContentType != "application/json" {
		return OutputStatus{}, errors.New("executor: output status requires JSON HTTP 200")
	}
	if _, err := canonical.ParseStrict(result.Body, canonical.Options{MaxBytes: maximum}); err != nil {
		return OutputStatus{}, err
	}
	if _, err := wire.Decode(outputContract, "OutputStatus", result.Body); err != nil {
		return OutputStatus{}, err
	}
	var status OutputStatus
	if err := json.Unmarshal(result.Body, &status); err != nil {
		return OutputStatus{}, err
	}
	if err := validateOutputStatus(status); err != nil {
		return OutputStatus{}, err
	}
	return status, nil
}

func outputSequence(value *string) int64 {
	if value == nil {
		return -1
	}
	n, _ := strconv.ParseInt(*value, 10, 64) // Caller validated Sequence first.
	return n
}

func validateOutputStatus(s OutputStatus) error {
	ack, durable, retained, offset := outputSequence(s.AcceptedThrough), outputSequence(s.DurableThrough), outputSequence(s.RetainedFrom), outputSequence(s.NextByteOffset)
	if durable > ack || retained > ack {
		return ErrOutputIntegrity
	}
	if offset == -1 {
		if s.State != "unavailable" || ack != -1 || durable != -1 || retained != -1 || s.Seal != nil {
			return ErrOutputIntegrity
		}
	} else if ack == -1 && offset != 0 {
		return ErrOutputIntegrity
	}
	if ack != -1 && (offset <= ack || offset == -1) {
		return ErrOutputIntegrity
	}
	if s.Seal != nil {
		if outputSequence(s.Seal.LastSeq) != ack || s.NextByteOffset == nil || s.Seal.TotalBytes != *s.NextByteOffset {
			return ErrOutputIntegrity
		}
		if ack == -1 && (s.Seal.TotalBytes != "0" || s.Seal.PayloadDigest != fmt.Sprintf("%x", sha256.Sum256(nil))) {
			return ErrOutputIntegrity
		}
	}
	switch s.State {
	case "complete":
		if s.Seal == nil || s.Seal.Truncated {
			return ErrOutputIntegrity
		}
	case "truncated":
		if s.Seal == nil || !s.Seal.Truncated {
			return ErrOutputIntegrity
		}
	case "available":
		if ack != -1 || s.Seal != nil {
			return ErrOutputIntegrity
		}
	case "receiving":
		if ack == -1 || s.Seal != nil {
			return ErrOutputIntegrity
		}
	case "gap":
		if ack == -1 {
			return ErrOutputIntegrity
		}
	}
	return nil
}

func outputSealEqual(a, b *OutputSeal) bool {
	return a != nil && b != nil && outputSequence(a.LastSeq) == outputSequence(b.LastSeq) &&
		a.TotalBytes == b.TotalBytes && a.PayloadDigest == b.PayloadDigest && a.Truncated == b.Truncated
}

func cloneOutputStatus(value OutputStatus) OutputStatus {
	cloneString := func(value *string) *string {
		if value == nil {
			return nil
		}
		copy := *value
		return &copy
	}
	value.AcceptedThrough = cloneString(value.AcceptedThrough)
	value.DurableThrough = cloneString(value.DurableThrough)
	value.RetainedFrom = cloneString(value.RetainedFrom)
	value.NextByteOffset = cloneString(value.NextByteOffset)
	if value.Seal != nil {
		copy := *value.Seal
		copy.LastSeq = cloneString(copy.LastSeq)
		value.Seal = &copy
	}
	return value
}
