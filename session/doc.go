// Package session provides the conversation API above the manifest-driven api
// transport. Serve owns the agent loop, context, permissions and accounting.
//
// Construct an api.Client with an explicit SessionFamily and EventEnvelope=true,
// then call New. Use Create without an initial prompt, open Events, and Send to
// avoid racing the first output. Attach only reads a live or stored reference;
// Resume asks Serve to reopen it. Neither silently creates a replacement session.
//
// A 202 receipt is acceptance only. A closed event stream, session.ended or an
// interrupted request does not prove a turn completed. Persist LastEventID only
// for event resumption; it is never archive coverage or a material ACK. Streams
// must be closed. Closing observation does not interrupt the remote turn: call
// Interrupt explicitly when that is the user's intent.
//
// The package never retries a write automatically. Keep the same IdempotencyKey
// when reconciling an unknown result, and use status APIs before resubmitting
// side effects. Per-request short-lived token renewal remains in api.TokenFunc.
package session
