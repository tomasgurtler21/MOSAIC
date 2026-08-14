package catalog_test

// protocol_loader_test.go verifies the FileProtocolLoader contract:
//
// T5.1 — Protocol source extraction:
//   - Both role blocks (Subagent and Orchestrator) are extracted from a well-formed document.
//   - Each block's inner content is returned; the block's own boundary tag lines are excluded.
//   - The frontmatter `version` scalar is returned as the source version.
//   - Text outside the two deployable blocks never appears in the returned content.
//
// T5.2 — Failure modes (each failure produces a distinct, identifiable error):
//   - Missing file                    → ErrProtocolSourceMissing
//   - Unparseable document            → ErrProtocolSourceUnparseable
//   - Absent Subagent block           → ErrProtocolBlockMissing
//   - Absent Orchestrator block       → ErrProtocolBlockMissing
//   - Empty Subagent block            → ErrProtocolBlockEmpty
//   - Empty Orchestrator block        → ErrProtocolBlockEmpty
//   - Whitespace-only block           → ErrProtocolBlockEmpty (same treatment as truly empty)
//   - Missing frontmatter version     → ErrProtocolVersionMissing
//   - No error return carries partial content (loader is all-or-nothing)

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// ---------------------------------------------------------------------------
// Shared test helpers
// ---------------------------------------------------------------------------

// makeTempMosaicRoot creates a minimal MOSAIC repository root in a temp directory.
// It writes both marker files that ResolveRoot / isMosaicRoot require. The caller
// is responsible for cleaning up via t.Cleanup (provided automatically by t.TempDir).
func makeTempMosaicRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// Write the single marker file that isMosaicRoot checks (target layout).
	mustMkdir(t, root, "Catalog")
	mustWriteFile(t, root, filepath.Join("Catalog", "SourceFilesFormat.md"), []byte("# Source Files Format\n"))
	return root
}

// writeProtocolDoc writes the given content to the canonical protocol source path inside root.
func writeProtocolDoc(t *testing.T, root string, content []byte) {
	t.Helper()
	mustMkdir(t, root, "Development", "Designs")
	mustWriteFile(t, root, catalog.ProtocolSourceRelPath, content)
}

// mustMkdir creates a directory (and all parents) under root. Fails the test on error.
func mustMkdir(t *testing.T, root string, parts ...string) {
	t.Helper()
	dir := filepath.Join(append([]string{root}, parts...)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mustMkdir %q: %v", dir, err)
	}
}

// mustWriteFile writes content to the file at <root>/<relPath>. The parent directory
// must already exist.
func mustWriteFile(t *testing.T, root, relPath string, content []byte) {
	t.Helper()
	p := filepath.Join(root, relPath)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("mustWriteFile %q: %v", p, err)
	}
}

// wellFormedProtocolDoc is a minimal but structurally valid protocol source document used
// across the T5.1 success tests. It includes:
//   - A frontmatter version scalar ("1.9")
//   - Outer maintainer text that must NOT appear in any returned block
//   - The Subagent block with distinct inner content
//   - The Orchestrator block with distinct inner content
//
// The string "OUTER_MAINTAINER_TEXT" is the excluded marker: the T5.1 assertion that
// nothing outside the two blocks reaches the caller uses this token.
const wellFormedProtocolDoc = `---
version: "1.9"
---

OUTER_MAINTAINER_TEXT: this prose is maintainer documentation and must never reach a deployed agent.

## 1. Canonical Sections

<CommunicationProtocol type="core" name="Subagent">
## Communication Protocol

You operate under the subagent protocol. This is the deployable subagent block.
</CommunicationProtocol>

Between-blocks text that must also be excluded from the returned payload.

<CommunicationProtocol type="core" name="Orchestrator">
## Communication Protocol

You operate under the orchestrator protocol. This is the deployable orchestrator block.
</CommunicationProtocol>

Trailing maintainer text that must not appear in any returned block.
`

// ---------------------------------------------------------------------------
// T5.1 — Protocol source extraction from a well-formed document
// ---------------------------------------------------------------------------

// TestProtocolLoader_WellFormedDoc_ReturnsNoError verifies that LoadProtocol returns no error
// when the protocol source document is well-formed.
func TestProtocolLoader_WellFormedDoc_ReturnsNoError(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeProtocolDoc(t, root, []byte(wellFormedProtocolDoc))

	_, err := catalog.FileProtocolLoader{}.LoadProtocol(root)

	if err != nil {
		t.Errorf("LoadProtocol returned unexpected error for a well-formed document: %v", err)
	}
}

// TestProtocolLoader_WellFormedDoc_ReturnsSourceVersion verifies that the frontmatter
// `version` scalar from the protocol document is returned verbatim as ProtocolContent.Version.
func TestProtocolLoader_WellFormedDoc_ReturnsSourceVersion(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeProtocolDoc(t, root, []byte(wellFormedProtocolDoc))

	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)
	if err != nil {
		t.Fatalf("LoadProtocol error: %v", err)
	}

	if content.Version != "1.9" {
		t.Errorf("ProtocolContent.Version = %q, want %q", content.Version, "1.9")
	}
}

// TestProtocolLoader_WellFormedDoc_BothBlocksPresent verifies that ProtocolContent.Blocks
// contains entries for both ProtocolSubagent and ProtocolOrchestrator variants.
func TestProtocolLoader_WellFormedDoc_BothBlocksPresent(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeProtocolDoc(t, root, []byte(wellFormedProtocolDoc))

	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)
	if err != nil {
		t.Fatalf("LoadProtocol error: %v", err)
	}

	if len(content.Blocks) == 0 {
		t.Fatal("ProtocolContent.Blocks is empty; both role blocks must be present")
	}
}

// TestProtocolLoader_WellFormedDoc_SubagentBlockContainsExpectedContent verifies that the
// subagent block's inner content contains the distinctive subagent text from the fixture.
func TestProtocolLoader_WellFormedDoc_SubagentBlockContainsExpectedContent(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeProtocolDoc(t, root, []byte(wellFormedProtocolDoc))

	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)
	if err != nil {
		t.Fatalf("LoadProtocol error: %v", err)
	}

	// The subagent block fixture contains this distinctive phrase.
	const wantPhrase = "subagent protocol"

	subagentBlock, ok := content.Blocks["subagent"]
	if !ok {
		t.Fatal("ProtocolContent.Blocks[\"subagent\"] is absent; expected a subagent block")
	}
	if !bytes.Contains(subagentBlock, []byte(wantPhrase)) {
		t.Errorf("subagent block does not contain expected phrase %q; got:\n%s", wantPhrase, subagentBlock)
	}
}

// TestProtocolLoader_WellFormedDoc_OrchestratorBlockContainsExpectedContent verifies that the
// orchestrator block's inner content contains the distinctive orchestrator text from the fixture.
func TestProtocolLoader_WellFormedDoc_OrchestratorBlockContainsExpectedContent(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeProtocolDoc(t, root, []byte(wellFormedProtocolDoc))

	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)
	if err != nil {
		t.Fatalf("LoadProtocol error: %v", err)
	}

	const wantPhrase = "orchestrator protocol"

	orchestratorBlock, ok := content.Blocks["orchestrator"]
	if !ok {
		t.Fatal("ProtocolContent.Blocks[\"orchestrator\"] is absent; expected an orchestrator block")
	}
	if !bytes.Contains(orchestratorBlock, []byte(wantPhrase)) {
		t.Errorf("orchestrator block does not contain expected phrase %q; got:\n%s", wantPhrase, orchestratorBlock)
	}
}

// TestProtocolLoader_WellFormedDoc_OuterTextNotInSubagentBlock verifies that text that appears
// outside both deployable blocks in the source document does not reach the returned subagent
// block. The excluded marker "OUTER_MAINTAINER_TEXT" is present in the fixture's surrounding
// documentation prose and must not leak into the deployed payload.
func TestProtocolLoader_WellFormedDoc_OuterTextNotInSubagentBlock(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeProtocolDoc(t, root, []byte(wellFormedProtocolDoc))

	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)
	if err != nil {
		t.Fatalf("LoadProtocol error: %v", err)
	}

	const excludedMarker = "OUTER_MAINTAINER_TEXT"

	subagentBlock, ok := content.Blocks["subagent"]
	if !ok {
		t.Fatal("subagent block absent; cannot check exclusion")
	}
	if bytes.Contains(subagentBlock, []byte(excludedMarker)) {
		t.Errorf("subagent block contains the outer-documentation marker %q; "+
			"only inner block content must be returned, not surrounding maintainer prose", excludedMarker)
	}
}

// TestProtocolLoader_WellFormedDoc_OuterTextNotInOrchestratorBlock verifies that outer
// documentation text does not appear in the orchestrator block payload.
func TestProtocolLoader_WellFormedDoc_OuterTextNotInOrchestratorBlock(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeProtocolDoc(t, root, []byte(wellFormedProtocolDoc))

	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)
	if err != nil {
		t.Fatalf("LoadProtocol error: %v", err)
	}

	const excludedMarker = "OUTER_MAINTAINER_TEXT"

	orchestratorBlock, ok := content.Blocks["orchestrator"]
	if !ok {
		t.Fatal("orchestrator block absent; cannot check exclusion")
	}
	if bytes.Contains(orchestratorBlock, []byte(excludedMarker)) {
		t.Errorf("orchestrator block contains the outer-documentation marker %q; "+
			"only inner block content must be returned, not surrounding maintainer prose", excludedMarker)
	}
}

// TestProtocolLoader_WellFormedDoc_SubagentBlockDoesNotContainBoundaryTags verifies that
// the returned subagent block does not include its own boundary tag lines. The payload is
// the inner content only — the tag lines belong to the source document structure, not to
// the deployed content.
func TestProtocolLoader_WellFormedDoc_SubagentBlockDoesNotContainBoundaryTags(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeProtocolDoc(t, root, []byte(wellFormedProtocolDoc))

	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)
	if err != nil {
		t.Fatalf("LoadProtocol error: %v", err)
	}

	subagentBlock, ok := content.Blocks["subagent"]
	if !ok {
		t.Fatal("subagent block absent; cannot check for boundary tags")
	}
	if bytes.Contains(subagentBlock, []byte(`<CommunicationProtocol type="core" name="Subagent">`)) {
		t.Error("subagent block payload contains its own opening boundary tag; " +
			"only inner content must be returned, not the tag lines themselves")
	}
	if bytes.Contains(subagentBlock, []byte("</CommunicationProtocol>")) {
		t.Error("subagent block payload contains its own closing boundary tag")
	}
}

// TestProtocolLoader_WellFormedDoc_BlocksAreNonEmpty verifies that both returned blocks
// contain non-whitespace content. An all-whitespace block would cause silent deployment of
// an empty protocol section, which the contract explicitly forbids.
func TestProtocolLoader_WellFormedDoc_BlocksAreNonEmpty(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeProtocolDoc(t, root, []byte(wellFormedProtocolDoc))

	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)
	if err != nil {
		t.Fatalf("LoadProtocol error: %v", err)
	}

	for variant, block := range content.Blocks {
		if len(bytes.TrimSpace(block)) == 0 {
			t.Errorf("Blocks[%q] has only whitespace content; a successful load must return non-empty blocks", variant)
		}
	}
}

// ---------------------------------------------------------------------------
// T5.2 — Failure modes
// ---------------------------------------------------------------------------

// TestProtocolLoader_MissingFile_ReturnsErrProtocolSourceMissing verifies that when the
// protocol source file does not exist at the expected path, LoadProtocol returns an error
// wrapping ErrProtocolSourceMissing. The error must also identify the file path so that the
// caller can report which file was absent (AC5.4: error identifying the file and the problem).
func TestProtocolLoader_MissingFile_ReturnsErrProtocolSourceMissing(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// Deliberately do NOT write the protocol document.

	_, err := catalog.FileProtocolLoader{}.LoadProtocol(root)

	if err == nil {
		t.Fatal("LoadProtocol returned nil error for a missing protocol file; expected ErrProtocolSourceMissing")
	}
	if !errors.Is(err, catalog.ErrProtocolSourceMissing) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrProtocolSourceMissing", err)
	}
	// The error must name the offending file path so operators can locate and fix the problem.
	if !strings.Contains(err.Error(), catalog.ProtocolSourceRelPath) {
		t.Errorf("error %q does not mention the protocol file path %q; "+
			"errors must identify the file and the problem (AC5.4)", err.Error(), catalog.ProtocolSourceRelPath)
	}
}

// TestProtocolLoader_UnparseableDocument_ReturnsErrProtocolSourceUnparseable verifies that
// when the protocol file exists but cannot be parsed by docformat.Parse (e.g. malformed YAML
// frontmatter), LoadProtocol returns an error wrapping ErrProtocolSourceUnparseable. The
// error must also identify the file path (AC5.4).
func TestProtocolLoader_UnparseableDocument_ReturnsErrProtocolSourceUnparseable(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// A frontmatter block with a duplicate key causes docformat.Parse to fail.
	unparseable := []byte("---\nversion: \"1.9\"\nversion: \"duplicate\"\n---\nBody text.\n")
	writeProtocolDoc(t, root, unparseable)

	_, err := catalog.FileProtocolLoader{}.LoadProtocol(root)

	if err == nil {
		t.Fatal("LoadProtocol returned nil error for an unparseable document; expected ErrProtocolSourceUnparseable")
	}
	if !errors.Is(err, catalog.ErrProtocolSourceUnparseable) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrProtocolSourceUnparseable", err)
	}
	// The error must name the offending file path so operators can locate and fix the problem.
	if !strings.Contains(err.Error(), catalog.ProtocolSourceRelPath) {
		t.Errorf("error %q does not mention the protocol file path %q; "+
			"errors must identify the file and the problem (AC5.4)", err.Error(), catalog.ProtocolSourceRelPath)
	}
}

// TestProtocolLoader_MissingSubagentBlock_ReturnsErrProtocolBlockMissing verifies that
// when the Subagent block is absent from the protocol document, LoadProtocol returns an
// error wrapping ErrProtocolBlockMissing. The error must also identify the file path (AC5.4).
func TestProtocolLoader_MissingSubagentBlock_ReturnsErrProtocolBlockMissing(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// Document with only the Orchestrator block; Subagent block is absent.
	docMissingSubagent := []byte(`---
version: "1.9"
---

<CommunicationProtocol type="core" name="Orchestrator">
Orchestrator content.
</CommunicationProtocol>
`)
	writeProtocolDoc(t, root, docMissingSubagent)

	_, err := catalog.FileProtocolLoader{}.LoadProtocol(root)

	if err == nil {
		t.Fatal("LoadProtocol returned nil error when Subagent block is absent; expected ErrProtocolBlockMissing")
	}
	if !errors.Is(err, catalog.ErrProtocolBlockMissing) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrProtocolBlockMissing", err)
	}
	// The error must name the offending file path so operators can locate and fix the problem.
	if !strings.Contains(err.Error(), catalog.ProtocolSourceRelPath) {
		t.Errorf("error %q does not mention the protocol file path %q; "+
			"errors must identify the file and the problem (AC5.4)", err.Error(), catalog.ProtocolSourceRelPath)
	}
}

// TestProtocolLoader_MissingOrchestratorBlock_ReturnsErrProtocolBlockMissing verifies that
// when the Orchestrator block is absent from the protocol document, LoadProtocol returns an
// error wrapping ErrProtocolBlockMissing.
func TestProtocolLoader_MissingOrchestratorBlock_ReturnsErrProtocolBlockMissing(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// Document with only the Subagent block; Orchestrator block is absent.
	docMissingOrchestrator := []byte(`---
version: "1.9"
---

<CommunicationProtocol type="core" name="Subagent">
Subagent content.
</CommunicationProtocol>
`)
	writeProtocolDoc(t, root, docMissingOrchestrator)

	_, err := catalog.FileProtocolLoader{}.LoadProtocol(root)

	if err == nil {
		t.Fatal("LoadProtocol returned nil error when Orchestrator block is absent; expected ErrProtocolBlockMissing")
	}
	if !errors.Is(err, catalog.ErrProtocolBlockMissing) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrProtocolBlockMissing", err)
	}
}

// TestProtocolLoader_EmptySubagentBlock_ReturnsErrProtocolBlockEmpty verifies that when the
// Subagent block is present but has no inner content (open tag immediately followed by close
// tag), LoadProtocol returns an error wrapping ErrProtocolBlockEmpty. The error must also
// identify the file path (AC5.4).
func TestProtocolLoader_EmptySubagentBlock_ReturnsErrProtocolBlockEmpty(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// Document where the Subagent block has no content between its tags.
	docEmptySubagent := []byte(`---
version: "1.9"
---

<CommunicationProtocol type="core" name="Subagent">
</CommunicationProtocol>

<CommunicationProtocol type="core" name="Orchestrator">
Orchestrator content.
</CommunicationProtocol>
`)
	writeProtocolDoc(t, root, docEmptySubagent)

	_, err := catalog.FileProtocolLoader{}.LoadProtocol(root)

	if err == nil {
		t.Fatal("LoadProtocol returned nil error for an empty Subagent block; expected ErrProtocolBlockEmpty")
	}
	if !errors.Is(err, catalog.ErrProtocolBlockEmpty) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrProtocolBlockEmpty", err)
	}
	// The error must name the offending file path so operators can locate and fix the problem.
	if !strings.Contains(err.Error(), catalog.ProtocolSourceRelPath) {
		t.Errorf("error %q does not mention the protocol file path %q; "+
			"errors must identify the file and the problem (AC5.4)", err.Error(), catalog.ProtocolSourceRelPath)
	}
}

// TestProtocolLoader_EmptyOrchestratorBlock_ReturnsErrProtocolBlockEmpty verifies that when
// the Orchestrator block is present but empty, LoadProtocol returns ErrProtocolBlockEmpty.
func TestProtocolLoader_EmptyOrchestratorBlock_ReturnsErrProtocolBlockEmpty(t *testing.T) {
	root := makeTempMosaicRoot(t)
	docEmptyOrchestrator := []byte(`---
version: "1.9"
---

<CommunicationProtocol type="core" name="Subagent">
Subagent content.
</CommunicationProtocol>

<CommunicationProtocol type="core" name="Orchestrator">
</CommunicationProtocol>
`)
	writeProtocolDoc(t, root, docEmptyOrchestrator)

	_, err := catalog.FileProtocolLoader{}.LoadProtocol(root)

	if err == nil {
		t.Fatal("LoadProtocol returned nil error for an empty Orchestrator block; expected ErrProtocolBlockEmpty")
	}
	if !errors.Is(err, catalog.ErrProtocolBlockEmpty) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrProtocolBlockEmpty", err)
	}
}

// TestProtocolLoader_WhitespaceOnlySubagentBlock_ReturnsErrProtocolBlockEmpty verifies that
// a block containing only whitespace is treated identically to a completely empty block.
// Whitespace-only content would produce an empty protocol section in the deployed agent.
func TestProtocolLoader_WhitespaceOnlySubagentBlock_ReturnsErrProtocolBlockEmpty(t *testing.T) {
	root := makeTempMosaicRoot(t)
	docWhitespaceOnly := []byte("---\nversion: \"1.9\"\n---\n\n" +
		"<CommunicationProtocol type=\"core\" name=\"Subagent\">\n   \n\t\n\n</CommunicationProtocol>\n\n" +
		"<CommunicationProtocol type=\"core\" name=\"Orchestrator\">\nOrchestrator content.\n</CommunicationProtocol>\n")
	writeProtocolDoc(t, root, docWhitespaceOnly)

	_, err := catalog.FileProtocolLoader{}.LoadProtocol(root)

	if err == nil {
		t.Fatal("LoadProtocol returned nil error for a whitespace-only Subagent block; "+
			"whitespace-only content must be treated as empty (ErrProtocolBlockEmpty)", )
	}
	if !errors.Is(err, catalog.ErrProtocolBlockEmpty) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrProtocolBlockEmpty", err)
	}
}

// TestProtocolLoader_WhitespaceOnlyOrchestratorBlock_ReturnsErrProtocolBlockEmpty verifies that
// a whitespace-only Orchestrator block is treated identically to a completely empty Orchestrator
// block. This is the symmetric counterpart to TestProtocolLoader_WhitespaceOnlySubagentBlock_*.
// Both roles share the same emptiness check, and both must be enforced.
func TestProtocolLoader_WhitespaceOnlyOrchestratorBlock_ReturnsErrProtocolBlockEmpty(t *testing.T) {
	root := makeTempMosaicRoot(t)
	docWhitespaceOrchestrator := []byte("---\nversion: \"1.9\"\n---\n\n" +
		"<CommunicationProtocol type=\"core\" name=\"Subagent\">\nSubagent content.\n</CommunicationProtocol>\n\n" +
		"<CommunicationProtocol type=\"core\" name=\"Orchestrator\">\n   \n\t\n\n</CommunicationProtocol>\n")
	writeProtocolDoc(t, root, docWhitespaceOrchestrator)

	_, err := catalog.FileProtocolLoader{}.LoadProtocol(root)

	if err == nil {
		t.Fatal("LoadProtocol returned nil error for a whitespace-only Orchestrator block; " +
			"whitespace-only content must be treated as empty (ErrProtocolBlockEmpty)")
	}
	if !errors.Is(err, catalog.ErrProtocolBlockEmpty) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrProtocolBlockEmpty", err)
	}
	if !strings.Contains(err.Error(), catalog.ProtocolSourceRelPath) {
		t.Errorf("error %q does not mention the protocol file path %q; "+
			"errors must identify the file and the problem (AC5.4)", err.Error(), catalog.ProtocolSourceRelPath)
	}
}

// TestProtocolLoader_MissingFrontmatterVersion_ReturnsErrProtocolVersionMissing verifies that
// when the protocol document's frontmatter carries no `version` key, LoadProtocol returns an
// error wrapping ErrProtocolVersionMissing. A versionless protocol defeats staleness detection.
// The error must also identify the file path (AC5.4).
func TestProtocolLoader_MissingFrontmatterVersion_ReturnsErrProtocolVersionMissing(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// Both blocks present and non-empty, but `version` is absent from frontmatter.
	docNoVersion := []byte(`---
name: "Communication Protocol"
---

<CommunicationProtocol type="core" name="Subagent">
Subagent content.
</CommunicationProtocol>

<CommunicationProtocol type="core" name="Orchestrator">
Orchestrator content.
</CommunicationProtocol>
`)
	writeProtocolDoc(t, root, docNoVersion)

	_, err := catalog.FileProtocolLoader{}.LoadProtocol(root)

	if err == nil {
		t.Fatal("LoadProtocol returned nil error when frontmatter version is missing; expected ErrProtocolVersionMissing")
	}
	if !errors.Is(err, catalog.ErrProtocolVersionMissing) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrProtocolVersionMissing", err)
	}
	// The error must name the offending file path so operators can locate and fix the problem.
	if !strings.Contains(err.Error(), catalog.ProtocolSourceRelPath) {
		t.Errorf("error %q does not mention the protocol file path %q; "+
			"errors must identify the file and the problem (AC5.4)", err.Error(), catalog.ProtocolSourceRelPath)
	}
}

// TestProtocolLoader_FailureReturnsNoPartialContent verifies that a load failure returns a
// zero-value ProtocolContent. The loader is all-or-nothing: partial content must never
// reach the transform, because the transform would then silently deploy an incomplete
// protocol section for whichever role is missing.
//
// This test uses a document with a missing Orchestrator block; the expected error is
// ErrProtocolBlockMissing (not a generic "not implemented" placeholder). The error type
// check ensures the real implementation returns the correct sentinel while also verifying
// that the returned ProtocolContent is zero-valued on failure.
func TestProtocolLoader_FailureReturnsNoPartialContent(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// Missing Orchestrator block: one block is extractable but the overall load must fail.
	docMissingOrchestrator := []byte(`---
version: "1.9"
---

<CommunicationProtocol type="core" name="Subagent">
Subagent content.
</CommunicationProtocol>
`)
	writeProtocolDoc(t, root, docMissingOrchestrator)

	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)

	// The error must be ErrProtocolBlockMissing, not a generic placeholder.
	if err == nil {
		t.Fatal("expected ErrProtocolBlockMissing for a document missing the Orchestrator block; got nil")
	}
	if !errors.Is(err, catalog.ErrProtocolBlockMissing) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrProtocolBlockMissing", err)
	}
	// On any failure, the returned ProtocolContent must be the zero value.
	if content.Version != "" {
		t.Errorf("ProtocolContent.Version = %q on a failed load; want empty string (zero value)", content.Version)
	}
	if len(content.Blocks) != 0 {
		t.Errorf("ProtocolContent.Blocks has %d entries on a failed load; want zero (zero value)", len(content.Blocks))
	}
}

// TestProtocolLoader_RealProtocolDocument_CanBeLoaded verifies that the actual
// Development/Designs/CommunicationProtocol.md in the MOSAIC repository is loadable.
// This confirms that the implementation handles the real document's structure correctly
// and that the source document has not drifted out of the loader's expected format.
func TestProtocolLoader_RealProtocolDocument_CanBeLoaded(t *testing.T) {
	root, err := catalog.ResolveRoot(repoRoot())
	if err != nil {
		t.Fatalf("ResolveRoot: %v — cannot locate MOSAIC repository root for integration test", err)
	}

	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)

	if err != nil {
		t.Fatalf("LoadProtocol(real repository root): %v; "+
			"the real CommunicationProtocol.md must be loadable by the production loader", err)
	}
	if content.Version == "" {
		t.Error("ProtocolContent.Version is empty for the real protocol document; expected a non-empty version string")
	}
	if len(content.Blocks) == 0 {
		t.Error("ProtocolContent.Blocks is empty for the real protocol document; expected both role blocks")
	}
}
