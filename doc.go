// Package tansr is the root of the tansr Go SDK (module github.com/tansrai/tansr-go).
//
// The module speaks the unified /api contract of tansr Serve (RFC-UAPI-1, contract unified-v1) and is
// organised as one package per concern:
//
//   - api        — the unified client: discovery (manifest / capabilities / session closure), Call,
//     Events (SSE + unified event envelope), error envelopes, retry advice;
//   - canonical  — RFC-UAPI-1 §4 canonical JSON (Encode / Decode / ParseStrict) and domain digests;
//   - sse        — text/event-stream frame reader;
//   - session    — conversation lifecycle, streaming turns and explicit approvals;
//   - executor   — authenticated business-tool hosts, durable execution receipts and output chunks;
//   - archive    — verified records, encrypted bounded storage, durable acknowledgements and materials.
//
// Facts come from the frozen artifacts under contract/ (see contract/LOCK.json);
// api/operations_gen.go is generated from the manifest. This client does not embed an agent kernel,
// enable arbitrary shell access or substitute local state for Serve authorization. The examples and
// README describe the implemented high-level subset separately from the complete operation catalog.
package tansr
