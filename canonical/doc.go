// Package canonical implements the tansr canonical JSON rules of RFC-UAPI-1 §4
// (= SDK2-ext-v1-wire §5): the byte form used for control DTOs, record metadata,
// closure ids and every domain digest.
//
// Semantics are a port of the JavaScript reference implementation
// (tansr-cli scripts/api/draft-canonical/**, later packages/protocol/src/canonical/)
// and are locked by the shared cross-vector set (contract/canonical-cross-vectors.json)
// and the sdk2-wire-v1 metadata goldens (contract/sdk2-wire-v1.json).
//
// Rules (RFC-UAPI-1 §4.1):
//
//   - object keys are non-empty printable ASCII (0x21–0x7e), emitted in byte order, no whitespace;
//   - numbers are `0 | [1-9][0-9]*` and ≤ 2^53−1 (no sign, fraction, exponent, leading zero, -0, NaN, Infinity);
//   - strings are emitted scalar by scalar without Unicode normalisation: `"` `\` and the five short
//     escapes, other control characters as lowercase `\u00xx`, everything else (including `/`,
//     U+2028/2029 and all non-ASCII) as raw UTF-8; unpaired surrogates and invalid UTF-8 are rejected;
//   - duplicate keys (after escape decoding) are rejected at every level; `__proto__` is an ordinary key;
//   - depth ≤ 32 (ancestor containers, root = 0), nodes ≤ 100000 (scalars included, keys excluded),
//     the byte budget is supplied by the caller and applies to the raw input (decode) or the
//     accumulated output (encode).
//
// Two decode entry points share one scan (RFC-UAPI-1 §4.3 names in parentheses; Go uses the package
// qualifier instead of the Canonical suffix):
//
//   - [ParseStrict] (= parseStrict) accepts only input that already equals its canonical bytes; otherwise
//     it fails with code not_canonical and the first violation (whitespace / key_order / escape) —
//     server-side wire intake, digest recomputation, closureId verification;
//   - [Decode] (= decodeCanonical) accepts any lexically valid document under the same rules (whitespace,
//     unsorted keys, equivalent escapes) — client-side tolerant reads. `Encode(Decode(x))` yields the
//     canonical bytes; [Encode] is encodeCanonical and [DomainDigest] is domainDigest.
//
// Errors are *[Error] with a stable [Code] table shared with the JavaScript implementation, an RFC 6901
// JSON pointer [Error.Path] and, on the decode side, the UTF-8 byte [Error.Offset] of the offending input.
package canonical
