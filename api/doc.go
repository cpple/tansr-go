// Package api is the unified /api client of tansr Serve (RFC-UAPI-1, contract unified-v1).
//
// Fact source: contract/api-manifest.json (tansr-cli packages/server/contract/api-manifest.json).
// operations_gen.go is generated from it by internal/gen/manifest2go; no path, method, query key or
// domain is written by hand.
//
// The seven disciplines of the SDK manual §16.6 are enforced here rather than left to callers:
//
//  1. No silent downgrade: a response without tansr-contract: unified-v1 is *ContractUnavailableError
//     (errors.Is(err, ErrContractUnavailable)); the client never retries with /v2 or /v3 prefixes.
//     A requested event envelope that the server does not echo is ErrEnvelopeNotNegotiated.
//  2. No URLs from responses: every request path is instantiated from the generated template
//     (InstantiatePath); discovery bodies carry no navigation fields and are rejected if they do.
//  3. No replay of unknown side effects: RetrySameRequest replays only when the server stated
//     same-request with the same idempotency key; result_unknown / commit_unknown map to query-status.
//  4. Identity is not in the body: authentication is the Authorization header; envelopes and
//     headers with identity fields fail validation.
//  5. SSE EOF / HTTP 202 are not completion: Stream.Next returns io.EOF as a transport fact only;
//     only EventEnvelope.TerminalStatus decides completion; Result.Status 202 means accepted.
//  6. The five cursors are not interchangeable: EventCursor, ArchiveCoverage, OutputWatermark,
//     MaterialConsumed and AckReceipt are distinct types; nothing here synthesises or advances them.
//  7. No hand-written legacy prefixes: the only path material is the manifest table.
//
// Errors (plan D19): *APIError carries the unified code (ErrorCode, 19 values) and RetryAction as its
// primary fields; the family's own code is secondary, in Detail.DomainCode. errors.Is against
// &APIError{Code: …} matches on the unified code only. *DomainError remains for unwrapped family
// envelopes (archive-sync-v1 today); *ClientError is local; *ContractUnavailableError is "not a
// unified-v1 Serve".
//
// Request heads (manifest r7): CallOptions.IdempotencyKey, IfMatch (from Meta.ETag of a prior read;
// only operations with Operation.AcceptsIfMatch) and Deadline (RFC 3339 UTC; expired → refused, never
// extended). Serve answers 409 conflict / idempotency_key_reused, 412 precondition_failed / refresh /
// if_match_stale and 408 invalid_request / deadline_exceeded; none of them is retried automatically.
package api
