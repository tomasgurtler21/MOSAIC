package docformat_test

// Tests for Document.Clone().
//
// Coverage:
//   - A cloned document produces bytes identical to the original at the moment of cloning.
//   - Mutating the clone's frontmatter leaves the original document's bytes unchanged.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
	domain "mosaic-common/mosaic"
)

func TestDocument_Clone_BytesEqualOriginalAtCreation(t *testing.T) {
	// The clone must be a byte-for-byte copy of the original at the moment Clone is called.
	src := []byte("---\nname: test-agent\nversion: 1.0.0\n---\n\nBody text.\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	clone := doc.Clone()

	if !bytes.Equal(doc.Bytes(), clone.Bytes()) {
		t.Errorf("clone.Bytes() differs from original at creation time:\n  original: %q\n  clone:    %q",
			doc.Bytes(), clone.Bytes())
	}
}

func TestDocument_Clone_MutatingClone_DoesNotAffectOriginal(t *testing.T) {
	// Modifying the clone's frontmatter must not change the original document's bytes.
	// This verifies that Clone performs a deep copy and the two documents do not share
	// any mutable state (e.g. the same underlying frontmatter slice).
	src := []byte("---\nname: original-name\nversion: 1.0.0\n---\n\nBody text.\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	originalBytes := doc.Bytes()

	clone := doc.Clone()
	if err := clone.Frontmatter().Set("name", domain.ScalarValue("mutated-name", domain.QuotePlain)); err != nil {
		t.Fatalf("Set on clone: %v", err)
	}

	// Verify the clone actually changed (so a trivially-wrong Clone cannot pass this test by
	// returning a clone that rejects all mutations).
	if bytes.Equal(originalBytes, clone.Bytes()) {
		t.Error("clone.Bytes() is unchanged after Set — the mutation had no effect on the clone")
	}

	// Verify the original is untouched.
	if !bytes.Equal(originalBytes, doc.Bytes()) {
		t.Errorf("original document changed after mutating clone:\n  before: %q\n  after:  %q",
			originalBytes, doc.Bytes())
	}
}
