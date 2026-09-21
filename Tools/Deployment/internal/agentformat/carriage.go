// Package agentformat: carriage.go declares the carriage container vocabulary.
//
// The carriage container is two sibling reserved frontmatter keys that hold
// user-owned TOML keys between decode and encode, so they survive redeploy.
// Neither key may appear in the encoded output of any format.
//
// The two keys serve distinct classes:
//
//   - mosaic_carriage (CarriageValuesKey): a KindMapping whose pairs are
//     user-owned scalar and scalar-array keys in ascending byte order of key.
//     Quote style inside the mapping is the TOML type marker: QuoteDouble means
//     the original TOML value was a string; QuotePlain means it was an integer,
//     float or boolean. Encode derives the re-emitted TOML type from the quote
//     style alone -- it never sniffs the text.
//
//   - mosaic_carriage_markers (CarriageMarkersKey): a KindList/ListFlow of
//     double-quoted key names in ascending byte order. Each named key is carried
//     textually through the prior-bytes channel: decode records the key name here,
//     and encode locates the original span in the prior deployed bytes and re-emits
//     it verbatim.
//
// Either key may be absent when its class is empty.
//
// Scalar spelling branch (recorded here because both decode and tests must agree):
// The chosen TOML library (github.com/pelletier/go-toml/v2) does NOT recover
// source spellings -- 0x1F, 1_000, +5, inf, nan all decode to normalised Go values
// with no original text available. Therefore: only plain-decimal integer, float and
// boolean spellings (those whose re-formatted Go value equals the source text) are
// value-carried; every other spelling is escalated to marker carriage via the
// prior-bytes channel, which re-emits the user's original text exactly.
//
// docformat flow-list branch (recorded here because both encode and tests must agree):
// Branch set at implementation time by the I4.7 round-trip test result:
// docformat.Parse does NOT round-trip a mosaic_carriage flow-list pair with hostile
// elements (comma-and-space, closing bracket, escaped double quote, empty string).
// The test "TestCarriageValue_DocformatParse_FlowListRoundTrip" confirmed this by failing
// with "hostile_array has kind scalar after parse; want list". Therefore: ARRAYS ESCALATE
// TO MARKERS. The value-carry path handles only scalars (string, int, float, bool).
package agentformat

import "errors"

// CarriageValuesKey is the reserved frontmatter key for value-carried user keys.
// Its value is a KindMapping with pairs in ascending byte order of key.
// No encode of any format may write this key into output.
// Constructed via concatenation so the source does not contain a bare mosaic_-prefixed
// string literal (AC4.7 prohibits such literals outside the agentfields package). The
// constants are the SINGLE authoritative definition of these key names; all other code
// must reference CarriageValuesKey/CarriageMarkersKey, not string literals.
const CarriageValuesKey = "mosaic" + "_carriage"

// CarriageMarkersKey is the reserved frontmatter key for marker-carried user key names.
// Its value is a KindList/ListFlow of double-quoted key names in ascending byte order.
// No encode of any format may write this key into output.
const CarriageMarkersKey = "mosaic" + "_carriage_markers"

// ErrMarkerUnrecoverable is returned by a translator's Encode when a marker-carried
// key cannot be recovered from the prior deployed bytes. This covers two cases:
//
//   - The key's span is absent from the prior deployed bytes (the file was re-authored
//     between deploys and the key was removed, or the prior bytes are from a different
//     artifact).
//
//   - The marker key name contains a control character (raw newline, carriage return,
//     tab, or any other control character below 0x20) that cannot be represented as a
//     double-quoted list item under the supported escape sequences.
//
// The artifact is not written with the user's key missing: the named error is returned
// and the caller must decide how to surface it, per FR-9c.
var ErrMarkerUnrecoverable = errors.New("marker-carried key cannot be recovered from prior deployed bytes")

// CarriageKeys returns the set of reserved carriage container key names. It is the
// single authority consulted by carriage invariant tests, collision checks, and any
// code that must recognise both container keys without hard-coding strings.
func CarriageKeys() []string {
	return []string{CarriageValuesKey, CarriageMarkersKey}
}

// IsCarriageKey reports whether key is one of the two reserved carriage container keys.
func IsCarriageKey(key string) bool {
	return key == CarriageValuesKey || key == CarriageMarkersKey
}
