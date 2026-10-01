// Package executor defines the interfaces of an executor host: a process that registers with Serve,
// heartbeats, polls the operations dispatched to it and submits receipts (sdk2-ext-v1 execution
// domain; manifest operations executor.register, executor.heartbeat, executor.operations.poll,
// executor.receipt.submit — all called through package api, never by literal path).
//
// Status: interfaces only (UAPI-01 skeleton). The loop implementation, operation typing and receipt
// vocabulary follow when the execution-domain wire types are ported. Disciplines that already bind
// implementations of these interfaces:
//
//   - a receipt is only submitted for an operation the executor actually performed; an operation whose
//     outcome is unknown is reported as such, never re-run under a fresh key (SDK manual §16.6 item 3);
//   - executor identity travels in headers set by package api, never in the body (item 4);
//   - a poll that returns no operations or ends is not a completion signal for anything (item 5).
package executor

import (
	"context"
	"encoding/json"
)

// Operation is one unit of work dispatched by Serve. Raw is the untouched wire object; typed views
// are added when the execution wire types are ported.
type Operation struct {
	// ID is the operation id used in the receipt.
	ID string
	// Kind is the operation kind (family vocabulary; unknown kinds must be rejected, not guessed).
	Kind string
	// Raw is the full wire object.
	Raw json.RawMessage
}

// Receipt reports the outcome of one Operation.
type Receipt struct {
	OperationID string
	// Status is the family receipt status word (e.g. completed, failed, unknown).
	Status string
	// Body is the receipt payload as the family defines it.
	Body json.RawMessage
}

// Handler performs operations. Returning an error means the operation was not performed; the runner
// reports it without inventing a receipt.
type Handler interface {
	Handle(ctx context.Context, op Operation) (Receipt, error)
}

// Runner drives the executor lifecycle (register → heartbeat → poll → Handle → receipt) until ctx
// ends. Implementations use package api for every request.
type Runner interface {
	Run(ctx context.Context, handler Handler) error
}
