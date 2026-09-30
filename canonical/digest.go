package canonical

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// Registered digest domains (RFC-UAPI-1 §4.2). Existing names never change; new names need an RFC entry.
const (
	// DomainClosure frames the capability-closure body: closureId = SHA256(UTF8(domain) ‖ 0x00 ‖ canonical(body)).
	DomainClosure = "tansr.unified.closure.v1"
)

// FrameDomain returns `UTF8(domain) ‖ 0x00 ‖ data` for callers that feed their own hash / HMAC.
// The domain must be a non-empty, valid UTF-8 string without U+0000.
func FrameDomain(domain string, data []byte) ([]byte, error) {
	if domain == "" || strings.IndexByte(domain, 0) >= 0 || !utf8.ValidString(domain) {
		return nil, optionsError("domain must be a nonempty well-formed string without U+0000")
	}
	if data == nil {
		return nil, &Error{Code: CodeInvalidInput, Message: "data must not be nil", Offset: -1}
	}
	framed := make([]byte, 0, len(domain)+1+len(data))
	framed = append(framed, domain...)
	framed = append(framed, 0)
	return append(framed, data...), nil
}

// DomainDigest computes SHA256(UTF8(domain) ‖ 0x00 ‖ data) (RFC-UAPI-1 §4.2).
func DomainDigest(domain string, data []byte) ([32]byte, error) {
	framed, err := FrameDomain(domain, data)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(framed), nil
}

// Hex renders a digest as 64 lowercase hex characters (schema `Digest`).
func Hex(digest [32]byte) string {
	return hex.EncodeToString(digest[:])
}
