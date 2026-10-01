// Package tansr is the root of the tansr Go SDK (module github.com/cpple/tansr-go).
//
// The module speaks the unified /api contract of tansr Serve (RFC-UAPI-1, contract unified-v1) and is
// organised as one package per concern:
//
//   - api        — the unified client: discovery (manifest / capabilities / session closure), Call,
//     Events (SSE + unified event envelope), error envelopes, retry advice;
//   - canonical  — RFC-UAPI-1 §4 canonical JSON (Encode / Decode / ParseStrict) and domain digests;
//   - sse        — text/event-stream frame reader;
//   - executor   — interfaces for executor hosts (register / heartbeat / poll / receipt);
//   - archive    — interfaces for archive consumers (records, coverage, acknowledgements, materials).
//
// Status: skeleton (UAPI-01 stage four, lane U4-GO). Facts come from the vendored artifacts under
// contract/ (see contract/PROVENANCE.json); api/operations_gen.go is generated from the manifest and
// nothing about paths is written by hand. The SDK manual §16.6 disciplines are enforced in package api.
package tansr
