// Package archive defines the interfaces of an archive consumer: reading records and artifacts of an
// archive binding, acknowledging coverage and answering material requests (sdk2-ext-v1 archive
// domain; manifest operations archive.binding.*, archive.records.read, archive.artifact.read,
// archive.ack.commit, archive.ack.rebase, material.* — all called through package api).
//
// Status: interfaces only (UAPI-01 skeleton). Control DTOs of this domain are strict canonical JSON
// (package canonical); the typed record / coverage / ack wire shapes follow when the archive wire types
// are ported. Cursor discipline that already binds implementations (SDK manual §16.6 item 6):
//
//   - Coverage (archiveCoverage) advances only after an ACK the server accepted;
//   - MaterialConsumed advances only when the core reports consumption (≠ received / uploaded);
//   - AckReceipt is the server's receipt for an ACK, distinct from coverage;
//   - none of them is the event-stream cursor and none advances on SSE EOF or HTTP 202.
package archive

import (
	"context"
	"encoding/json"
)

// Coverage is the acknowledged range of an archive binding (wire form of the archiveCoverage cursor).
type Coverage struct {
	FromSequence    string
	ThroughSequence string
	HeadDigest      string
}

// Record is one archive record page entry. Metadata is canonical control JSON kept as bytes.
type Record struct {
	Sequence string
	Metadata json.RawMessage
	Payload  []byte
}

// Page is one archive.records.read result.
type Page struct {
	Records []Record
	// Coverage is the server-reported coverage at read time; it is informational and does not
	// advance the consumer's own coverage cursor.
	Coverage Coverage
	// More reports whether records beyond this page exist.
	More bool
}

// Reader reads records and artifacts of a binding.
type Reader interface {
	ReadRecords(ctx context.Context, bindingID string, afterSequence string, limit int) (Page, error)
	ReadArtifact(ctx context.Context, bindingID, artifactID string, offset, maxBytes int64) ([]byte, error)
}

// Acknowledger commits coverage. The returned receipt is the ackReceipt cursor value.
type Acknowledger interface {
	Commit(ctx context.Context, bindingID string, coverage Coverage, requestID string) (receipt json.RawMessage, err error)
}
