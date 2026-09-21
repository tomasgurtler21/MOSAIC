package agentformat_test

// registration_test.go hosts T2.7 and T2.8.
//
// T2.7: The set of format IDs in the formatid vocabulary equals the set registered in
// the translator registry. This closes the seam between the two packages so a format ID
// cannot exist in one without the other.
//
// T2.8: After importing only the wiring package (internal/agentformat/all) and formatid,
// both the Markdown and CodexTOML format IDs resolve through Lookup. The test must NOT
// import internal/agentformat/codextoml directly -- a direct import would satisfy the
// lookup from the test side and prove nothing about the wiring package.

import (
	"sort"
	"testing"

	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/agentformat/formatid"

	// Blank-import the wiring package. This is the sanctioned mechanism for tests
	// outside the composition root to obtain a resolvable CodexTOML translator.
	// Do NOT replace this with _ "mosaic-deploy/internal/agentformat/codextoml" --
	// a direct import of the implementation package would bypass the wiring package
	// entirely, and a broken wiring package would leave this test green.
	_ "mosaic-deploy/internal/agentformat/all"
)

// TestVocabularyEqualsRegistry verifies that the set of format IDs returned by
// formatid.All() is exactly the set returned by agentformat.RegisteredIDs(). A format
// ID existing in formatid but not registered (or registered but not in the vocabulary)
// would indicate a wiring gap.
func TestVocabularyEqualsRegistry(t *testing.T) {
	vocab := formatid.All()
	sort.Slice(vocab, func(i, j int) bool { return vocab[i] < vocab[j] })

	registered := agentformat.RegisteredIDs() // already sorted per its contract

	if len(vocab) != len(registered) {
		t.Errorf("vocabulary has %d IDs, registry has %d IDs; they must be equal sets\nvocabulary: %v\nregistered: %v",
			len(vocab), len(registered), vocab, registered)
		return
	}

	for i := range vocab {
		if vocab[i] != registered[i] {
			t.Errorf("vocabulary[%d]=%q != registered[%d]=%q; sets must be equal", i, vocab[i], i, registered[i])
		}
	}
}

// TestWiringPackage_MarkdownResolvable verifies that the Markdown format ID resolves
// through Lookup after only the wiring package is imported.
func TestWiringPackage_MarkdownResolvable(t *testing.T) {
	tr, err := agentformat.Lookup(formatid.Markdown)
	if err != nil {
		t.Fatalf("Lookup(Markdown) returned error %v; want non-nil translator", err)
	}
	if tr == nil {
		t.Fatal("Lookup(Markdown) returned nil translator with nil error")
	}
}

// TestWiringPackage_CodexTOMLResolvable verifies that the CodexTOML format ID resolves
// through Lookup after the wiring package is imported. This test proves that the wiring
// package (internal/agentformat/all) pulls in the CodexTOML translator.
//
// This test does NOT import internal/agentformat/codextoml directly. The blank import
// above is of agentformat/all. If the wiring package stopped importing codextoml, this
// test would fail. A direct import of codextoml here would bypass that check.
func TestWiringPackage_CodexTOMLResolvable(t *testing.T) {
	tr, err := agentformat.Lookup(formatid.CodexTOML)
	if err != nil {
		t.Fatalf("Lookup(CodexTOML) returned error %v; want non-nil translator", err)
	}
	if tr == nil {
		t.Fatal("Lookup(CodexTOML) returned nil translator with nil error")
	}
}

// TestWiringPackage_BothFormatIDsResolvable verifies in a single assertion that both
// the Markdown and CodexTOML format IDs resolve. Complementary to the two tests above;
// serves as a quick overall check.
func TestWiringPackage_BothFormatIDsResolvable(t *testing.T) {
	for _, id := range formatid.All() {
		id := id
		t.Run(string(id), func(t *testing.T) {
			tr, err := agentformat.Lookup(id)
			if err != nil {
				t.Fatalf("Lookup(%q) returned error %v; want non-nil translator", id, err)
			}
			if tr == nil {
				t.Fatalf("Lookup(%q) returned nil translator with nil error", id)
			}
		})
	}
}
