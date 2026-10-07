// Package archive implements the sdk2-ext-v1 archive domain over the generated
// unified API. Records and artifacts are verified and saved before an ACK can be
// sent. A lost response leaves the original ACK durable for retry; HTTP success,
// SSE cursors and local writes never imply core consumption.
//
// FileStore is an encrypted single-device store, not the Node SQLite format, a
// replica, a backup authority, or a context assembler. Applications retain key
// custody and provide a current-access check on every local operation.
package archive
