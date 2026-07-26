package manifest_test

// Tests for the Hash function.
//
// Hash invariants verified here:
//
// Format:
//   - Return value starts with "sha256:".
//   - Hex digits are lowercase.
//   - Total string length is 71 characters (7 for "sha256:" + 64 hex digits).
//
// Stability:
//   - Two calls with identical input produce identical output (deterministic).
//   - Two distinct byte slices with the same content produce the same hash.
//
// Sensitivity:
//   - A single byte change produces a different hash.
//   - Appending one byte produces a different hash.
//   - CRLF vs LF produces different hashes (no line-ending normalisation, CD-11, AC14.3).
//   - Mixed CRLF/LF differs from uniform LF.
//
// Known value:
//   - SHA-256 of the empty byte slice equals the well-known constant "e3b0c44…".
//
// Edge cases:
//   - Empty input returns a valid "sha256:<hex>" string (not empty, not a panic).

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// Format
// ---------------------------------------------------------------------------

// TestHash_HasSha256Prefix verifies that the Hash return value starts with "sha256:".
// The algorithm prefix is part of the ContentHash contract (CD-11).
func TestHash_HasSha256Prefix(t *testing.T) {
	h := manifest.Hash([]byte("hello"))
	if !strings.HasPrefix(h, "sha256:") {
		t.Errorf("Hash: got %q; expected prefix \"sha256:\"", h)
	}
}

// TestHash_HexPartIsLowercase verifies that the hex digits in the hash are lowercase.
// CD-11 specifies lowercase hex so that hash values in manifest files are canonical.
func TestHash_HexPartIsLowercase(t *testing.T) {
	h := manifest.Hash([]byte("hello world"))
	hexPart := strings.TrimPrefix(h, "sha256:")
	for i, c := range hexPart {
		if c >= 'A' && c <= 'F' {
			t.Errorf("Hash hex part has uppercase character %q at position %d: full hash %q", c, i, h)
		}
	}
}

// TestHash_OutputLengthIsFixed verifies that Hash always returns a string of fixed length.
// SHA-256 produces 32 bytes → 64 hex chars; with the "sha256:" prefix, the total is 71.
func TestHash_OutputLengthIsFixed(t *testing.T) {
	const wantLen = 71 // len("sha256:") == 7; SHA-256 hex == 64
	inputs := [][]byte{
		{},
		{'a'},
		[]byte("hello world"),
		[]byte(strings.Repeat("x", 1000)),
	}
	for _, in := range inputs {
		h := manifest.Hash(in)
		if len(h) != wantLen {
			t.Errorf("Hash(%q): len = %d, want %d", in, len(h), wantLen)
		}
	}
}

// ---------------------------------------------------------------------------
// Stability and determinism
// ---------------------------------------------------------------------------

// TestHash_IsStableAcrossMultipleCalls verifies that calling Hash twice with the same
// input produces identical output. Hash is required to be deterministic.
func TestHash_IsStableAcrossMultipleCalls(t *testing.T) {
	content := []byte("the quick brown fox")
	first := manifest.Hash(content)
	second := manifest.Hash(content)
	if first != second {
		t.Errorf("Hash is not stable: first = %q, second = %q", first, second)
	}
}

// TestHash_IdenticalContent_ProducesIdenticalHashes verifies that two separate byte
// slices with the same content produce the same hash. Hash is a function of content,
// not of object identity.
func TestHash_IdenticalContent_ProducesIdenticalHashes(t *testing.T) {
	a := []byte("same content")
	b := []byte("same content")
	hA := manifest.Hash(a)
	hB := manifest.Hash(b)
	if hA != hB {
		t.Errorf("Hash of identical content differs:\n  first:  %q\n  second: %q", hA, hB)
	}
}

// ---------------------------------------------------------------------------
// Empty input
// ---------------------------------------------------------------------------

// TestHash_EmptyInput_ReturnsValidHash verifies that hashing an empty byte slice returns
// a valid "sha256:<hex>" string and does not panic or return an empty string.
func TestHash_EmptyInput_ReturnsValidHash(t *testing.T) {
	h := manifest.Hash([]byte{})
	if h == "" {
		t.Fatal("Hash of empty input returned an empty string; expected a valid hash")
	}
	if !strings.HasPrefix(h, "sha256:") {
		t.Errorf("Hash of empty input: got %q; expected prefix \"sha256:\"", h)
	}
}

// TestHash_KnownValue_EmptyString pins the SHA-256 implementation against the
// well-known hash of the empty byte slice. This test ensures the algorithm is SHA-256
// (not, for example, SHA-1 or MD5) and that the format is "sha256:<lowercase-hex>".
func TestHash_KnownValue_EmptyString(t *testing.T) {
	// The SHA-256 digest of zero bytes is a published constant.
	const want = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	got := manifest.Hash([]byte{})
	if got != want {
		t.Errorf("Hash(empty): got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Sensitivity to content changes
// ---------------------------------------------------------------------------

// TestHash_SingleByteChange_ProducesDifferentHash verifies that changing a single byte
// in the content produces a different hash. Any byte-level change must be detectable.
func TestHash_SingleByteChange_ProducesDifferentHash(t *testing.T) {
	original := []byte("hello world")
	modified := []byte("hello World") // uppercase W

	h1 := manifest.Hash(original)
	h2 := manifest.Hash(modified)

	if h1 == h2 {
		t.Errorf("Hash produced identical results for different content:\n  original: %q → %q\n  modified: %q → %q",
			original, h1, modified, h2)
	}
}

// TestHash_AppendedByte_ProducesDifferentHash verifies that appending one byte to the
// content produces a different hash.
func TestHash_AppendedByte_ProducesDifferentHash(t *testing.T) {
	original := []byte("file content")
	appended := []byte("file content\n")

	h1 := manifest.Hash(original)
	h2 := manifest.Hash(appended)

	if h1 == h2 {
		t.Errorf("Hash is identical after appending a byte; expected different hashes")
	}
}

// TestHash_LineEndingCRLF_vs_LF_ProducesDifferentHash verifies that a file with CRLF
// line endings hashes differently from the same text with LF line endings. A line-ending
// change is a real modification to a deployed file; Hash must never normalise line endings
// (CD-11, AC14.3).
func TestHash_LineEndingCRLF_vs_LF_ProducesDifferentHash(t *testing.T) {
	lf := []byte("line one\nline two\n")
	crlf := []byte("line one\r\nline two\r\n")

	hLF := manifest.Hash(lf)
	hCRLF := manifest.Hash(crlf)

	if hLF == hCRLF {
		t.Errorf("Hash produced identical results for LF and CRLF line endings; "+
			"line endings must not be normalised (CD-11, AC14.3):\n  LF:   %q\n  CRLF: %q",
			hLF, hCRLF)
	}
}

// TestHash_MixedLineEndings_ProducesDifferentHash verifies that mixed CRLF/LF in part of
// a file is distinguishable from uniform LF. Every byte must contribute to the hash.
func TestHash_MixedLineEndings_ProducesDifferentHash(t *testing.T) {
	uniform := []byte("first\nsecond\nthird\n")
	mixed := []byte("first\r\nsecond\nthird\n")

	h1 := manifest.Hash(uniform)
	h2 := manifest.Hash(mixed)

	if h1 == h2 {
		t.Errorf("Hash produced identical results for uniform-LF and mixed-CRLF/LF content; "+
			"every byte must contribute to the hash")
	}
}

// TestHash_DifferentLengths_ProducesDifferentHashes verifies that content of different
// lengths produces different hashes (property of SHA-256).
func TestHash_DifferentLengths_ProducesDifferentHashes(t *testing.T) {
	short := []byte("abc")
	long := []byte("abcd")

	h1 := manifest.Hash(short)
	h2 := manifest.Hash(long)

	if h1 == h2 {
		t.Errorf("Hash of %q and %q are identical; expected different hashes for different-length inputs",
			short, long)
	}
}
