// Package executor connects explicitly registered client tools to Serve's frozen execution
// contract. Serve owns the agent loop and policy; the device still authorizes every operation.
//
// Client validates canonical wire objects, operation digests, scope, connection generations and
// receipts. Runner executes only registered business tool handlers; it never creates a shell or
// accesses the filesystem on behalf of a model. FileJournal claims operations durably before a
// handler starts. A pending claim after a crash becomes unknown, never permission to execute again.
//
// A Client is bound to one immutable authenticated scope. Token renewal may keep that scope, but an
// identity or authorization revision change requires stopping the runner and constructing a new
// client. An executor ID in a body identifies a resource; authentication remains in api headers.
package executor
