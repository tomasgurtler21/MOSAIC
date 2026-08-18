package manifest

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash returns the canonical content hash of content in the form "sha256:<lowercase-hex>".
//
// No normalisation is applied: content is hashed exactly as received. A file that differs
// from another only in line endings (e.g. CRLF vs LF) produces a different hash, because
// a line-ending change is a real modification to a deployed file (CD-11, AC14.3).
//
// The algorithm prefix "sha256:" makes future algorithm changes non-breaking: the manifest
// schema records the algorithm prefix next to the hex digest, so any reader can dispatch
// on the prefix rather than assuming SHA-256 forever.
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}
