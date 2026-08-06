package catalog_test

// bundle_loader_test.go verifies the FileBundleLoader contract.
//
// Coverage (T6.1 — Bundle loading):
//
//   Well-formed bundle:
//     - LoadBundle returns no error for a structurally valid bundle document.
//     - The returned BundleContent carries the `bundle_version` scalar, verbatim.
//     - All five declared blocks are returned in Blocks, one per declaration.
//     - Each block's Target, AppliesTo, SpecifiedIn, and Name fields are populated
//       from the declaration entry.
//     - Each block's Content is the section's inner text, excluding the block's own
//       boundary tag lines and surrounding maintainer prose.
//     - All five blocks have non-empty, non-whitespace Content.
//
//   Failure modes (each returns a distinct, identifiable sentinel; loader is all-or-nothing):
//     - Missing file                          → ErrBundleSourceMissing
//     - Unparseable document                  → ErrBundleSourceUnparseable
//     - Missing block section in body         → ErrBundleBlockMissing
//     - Empty block section in body           → ErrBundleBlockEmpty
//     - Whitespace-only block section         → ErrBundleBlockEmpty
//     - Missing `bundle_version` in frontmatter → ErrBundleVersionMissing
//     - No `blocks` list in frontmatter       → ErrBundleBlocksMissing
//     - Declaration missing required field    → ErrBundleBlockDeclInvalid
//     - Every error names BundleSourceRelPath
//     - No partial content is returned on error (zero-value BundleContent)
//
//   Real bundle document:
//     - The real Agents/Generic/DeployedSections.md can be loaded successfully.

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"mosaic-deploy/internal/catalog"
)

// ---------------------------------------------------------------------------
// Shared test helpers
// ---------------------------------------------------------------------------

// writeBundleDoc writes the given content to the canonical bundle source path inside root.
func writeBundleDoc(t *testing.T, root string, content []byte) {
	t.Helper()
	mustMkdir(t, root, "Agents", "Generic")
	mustWriteFile(t, root, catalog.BundleSourceRelPath, content)
}

// ---------------------------------------------------------------------------
// Well-formed bundle fixture
// ---------------------------------------------------------------------------

// wellFormedBundleDoc is a minimal but structurally valid bundle document used across
// the T6.1 success tests. It includes:
//   - A frontmatter `bundle_version` scalar
//   - A `blocks` list with all five canonical block declarations
//   - Outer maintainer prose that must NOT appear in any returned block payload
//   - Five body sections with distinct inner content, one per declaration
//
// The string "OUTER_BUNDLE_TEXT" is the exclusion marker: it appears in the surrounding
// maintainer documentation but must never reach a caller through the returned Content bytes.
const wellFormedBundleDoc = `---
bundle_version: "1.0.0"
blocks:
  - name: "AuthorityHierarchy:Subagent"
    target: AuthorityHierarchy
    applies_to: subagent
    specified_in: Development/Designs/AgentTemplateArchitecture.md
  - name: "ClosingProcedure:Subagent"
    target: ClosingProcedure
    applies_to: subagent
    specified_in: Development/Designs/AgentTemplateArchitecture.md
  - name: "ProtocolConstraints:Subagent"
    target: ProtocolConstraints
    applies_to: subagent
    specified_in: Development/Designs/AgentTemplateArchitecture.md
  - name: "ErrorHandlingCommon:Subagent"
    target: ErrorHandlingCommon
    applies_to: subagent
    specified_in: Development/Designs/AgentTemplateArchitecture.md
  - name: "ExecutionPhilosophyCommon:Subagent"
    target: ExecutionPhilosophyCommon
    applies_to: subagent
    specified_in: Development/Designs/AgentTemplateArchitecture.md
---

OUTER_BUNDLE_TEXT: this is maintainer documentation and must never reach a deployed agent.

[[SECTION:AuthorityHierarchy:Subagent]]
### Authority Hierarchy

Authority hierarchy content for subagents.
[[/SECTION:AuthorityHierarchy:Subagent]]

[[SECTION:ClosingProcedure:Subagent]]
### Closing Procedure

Closing procedure content for subagents.
[[/SECTION:ClosingProcedure:Subagent]]

[[SECTION:ProtocolConstraints:Subagent]]
### Protocol Constraints

Protocol constraints content for subagents.
[[/SECTION:ProtocolConstraints:Subagent]]

[[SECTION:ErrorHandlingCommon:Subagent]]
### Error Handling Common

Error handling common content for subagents.
[[/SECTION:ErrorHandlingCommon:Subagent]]

[[SECTION:ExecutionPhilosophyCommon:Subagent]]
### Execution Philosophy Common

Execution philosophy common content for subagents.
[[/SECTION:ExecutionPhilosophyCommon:Subagent]]

Trailing maintainer text that must not appear in any returned block.
`

// ---------------------------------------------------------------------------
// T6.1 — Well-formed bundle: successful extraction
// ---------------------------------------------------------------------------

// TestBundleLoader_WellFormedDoc_ReturnsNoError verifies that LoadBundle returns no error
// for a structurally valid bundle document with all required fields and all declared sections.
func TestBundleLoader_WellFormedDoc_ReturnsNoError(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeBundleDoc(t, root, []byte(wellFormedBundleDoc))

	_, err := catalog.FileBundleLoader{}.LoadBundle(root)

	if err != nil {
		t.Errorf("LoadBundle returned unexpected error for a well-formed bundle: %v", err)
	}
}

// TestBundleLoader_WellFormedDoc_ReturnsBundleVersion verifies that the frontmatter
// `bundle_version` scalar from the bundle document is returned verbatim as BundleContent.Version.
func TestBundleLoader_WellFormedDoc_ReturnsBundleVersion(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeBundleDoc(t, root, []byte(wellFormedBundleDoc))

	content, err := catalog.FileBundleLoader{}.LoadBundle(root)
	if err != nil {
		t.Fatalf("LoadBundle error: %v", err)
	}

	if content.Version != "1.0.0" {
		t.Errorf("BundleContent.Version = %q, want %q", content.Version, "1.0.0")
	}
}

// TestBundleLoader_WellFormedDoc_ReturnsFiveBlocks verifies that BundleContent.Blocks
// contains exactly five entries — one per declaration in the fixture's `blocks` list.
func TestBundleLoader_WellFormedDoc_ReturnsFiveBlocks(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeBundleDoc(t, root, []byte(wellFormedBundleDoc))

	content, err := catalog.FileBundleLoader{}.LoadBundle(root)
	if err != nil {
		t.Fatalf("LoadBundle error: %v", err)
	}

	if len(content.Blocks) != 5 {
		t.Errorf("BundleContent.Blocks length = %d, want 5; all five declared blocks must be returned",
			len(content.Blocks))
	}
}

// TestBundleLoader_WellFormedDoc_AuthorityHierarchyBlockFields verifies that the
// AuthorityHierarchy:Subagent block carries the correct Target, AppliesTo, Name,
// and SpecifiedIn fields from the declaration.
func TestBundleLoader_WellFormedDoc_AuthorityHierarchyBlockFields(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeBundleDoc(t, root, []byte(wellFormedBundleDoc))

	content, err := catalog.FileBundleLoader{}.LoadBundle(root)
	if err != nil {
		t.Fatalf("LoadBundle error: %v", err)
	}

	var found bool
	for _, blk := range content.Blocks {
		if blk.Name == "AuthorityHierarchy:Subagent" {
			found = true
			if blk.Target != "AuthorityHierarchy" {
				t.Errorf("AuthorityHierarchy block Target = %q, want %q", blk.Target, "AuthorityHierarchy")
			}
			if blk.AppliesTo != "subagent" {
				t.Errorf("AuthorityHierarchy block AppliesTo = %q, want %q", blk.AppliesTo, "subagent")
			}
			if blk.SpecifiedIn == "" {
				t.Error("AuthorityHierarchy block SpecifiedIn is empty; declaration value must be carried")
			}
		}
	}
	if !found {
		t.Error("BundleContent.Blocks does not contain a block with Name \"AuthorityHierarchy:Subagent\"")
	}
}

// TestBundleLoader_WellFormedDoc_AllBlocksHaveNonEmptyContent verifies that every returned
// block carries non-whitespace Content. An all-whitespace block would cause silent deployment
// of an empty region, which the contract explicitly forbids.
func TestBundleLoader_WellFormedDoc_AllBlocksHaveNonEmptyContent(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeBundleDoc(t, root, []byte(wellFormedBundleDoc))

	content, err := catalog.FileBundleLoader{}.LoadBundle(root)
	if err != nil {
		t.Fatalf("LoadBundle error: %v", err)
	}

	for _, blk := range content.Blocks {
		if len(bytes.TrimSpace(blk.Content)) == 0 {
			t.Errorf("block %q has empty or whitespace-only Content; a successful load must return non-empty block content",
				blk.Name)
		}
	}
}

// TestBundleLoader_WellFormedDoc_AllFiveTargetsPresent verifies that each of the five
// canonical target names appears in at least one returned block.
func TestBundleLoader_WellFormedDoc_AllFiveTargetsPresent(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeBundleDoc(t, root, []byte(wellFormedBundleDoc))

	content, err := catalog.FileBundleLoader{}.LoadBundle(root)
	if err != nil {
		t.Fatalf("LoadBundle error: %v", err)
	}

	wantTargets := []string{
		"AuthorityHierarchy",
		"ClosingProcedure",
		"ProtocolConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}
	targetSet := make(map[string]bool, len(content.Blocks))
	for _, blk := range content.Blocks {
		targetSet[blk.Target] = true
	}
	for _, target := range wantTargets {
		if !targetSet[target] {
			t.Errorf("BundleContent.Blocks has no block with Target %q; all five canonical targets must be present",
				target)
		}
	}
}

// TestBundleLoader_WellFormedDoc_ContentExcludesOwnBoundaryTags verifies that the returned
// block content does not include the block's own [[SECTION:]] boundary tag lines. The payload
// is the inner content only — the tag lines belong to the source document structure, not to
// the deployed agent.
func TestBundleLoader_WellFormedDoc_ContentExcludesOwnBoundaryTags(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeBundleDoc(t, root, []byte(wellFormedBundleDoc))

	content, err := catalog.FileBundleLoader{}.LoadBundle(root)
	if err != nil {
		t.Fatalf("LoadBundle error: %v", err)
	}

	for _, blk := range content.Blocks {
		if bytes.Contains(blk.Content, []byte("[[SECTION:"+blk.Name+"]]")) {
			t.Errorf("block %q Content contains its own opening boundary tag; "+
				"only inner content must be returned, not the tag lines", blk.Name)
		}
		if bytes.Contains(blk.Content, []byte("[[/SECTION:"+blk.Name+"]]")) {
			t.Errorf("block %q Content contains its own closing boundary tag; "+
				"only inner content must be returned, not the tag lines", blk.Name)
		}
	}
}

// TestBundleLoader_WellFormedDoc_ContentExcludesOuterMaintainerText verifies that text
// appearing outside the section blocks in the bundle document does not leak into any
// returned block's Content.
func TestBundleLoader_WellFormedDoc_ContentExcludesOuterMaintainerText(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeBundleDoc(t, root, []byte(wellFormedBundleDoc))

	content, err := catalog.FileBundleLoader{}.LoadBundle(root)
	if err != nil {
		t.Fatalf("LoadBundle error: %v", err)
	}

	const excludedMarker = "OUTER_BUNDLE_TEXT"
	for _, blk := range content.Blocks {
		if bytes.Contains(blk.Content, []byte(excludedMarker)) {
			t.Errorf("block %q Content contains outer-documentation marker %q; "+
				"only inner block content must be returned, not surrounding maintainer prose", blk.Name, excludedMarker)
		}
	}
}

// TestBundleLoader_WellFormedDoc_BlockForMatchesByTargetAndRole verifies that
// BundleContent.BlockFor finds blocks by Target and role — not by compound Name.
// This is the compound-name hazard: the block's Name is "AuthorityHierarchy:Subagent"
// while the region name is just "AuthorityHierarchy".
func TestBundleLoader_WellFormedDoc_BlockForMatchesByTargetAndRole(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeBundleDoc(t, root, []byte(wellFormedBundleDoc))

	content, err := catalog.FileBundleLoader{}.LoadBundle(root)
	if err != nil {
		t.Fatalf("LoadBundle error: %v", err)
	}

	// BlockFor("AuthorityHierarchy", RoleSubagent) must find the block whose Target is
	// "AuthorityHierarchy" and AppliesTo is "subagent", NOT the block whose Name happens to
	// start with "AuthorityHierarchy". This validates that matching is on the declared fields.
	block, ok := content.BlockFor("AuthorityHierarchy", "subagent")
	if !ok {
		t.Fatal("BundleContent.BlockFor(\"AuthorityHierarchy\", \"subagent\") returned ok = false; " +
			"the block must be found by Target and AppliesTo, not by compound Name")
	}
	if len(bytes.TrimSpace(block)) == 0 {
		t.Error("BlockFor returned an empty block; a successful match must return non-empty content")
	}
}

// TestBundleLoader_WellFormedDoc_BlockForMismatchedRoleReturnsFalse verifies that
// BlockFor returns ok = false when the role does not match any block's AppliesTo.
// An orchestrator role has no matching block in the fixture (all blocks apply to subagent).
func TestBundleLoader_WellFormedDoc_BlockForMismatchedRoleReturnsFalse(t *testing.T) {
	root := makeTempMosaicRoot(t)
	writeBundleDoc(t, root, []byte(wellFormedBundleDoc))

	content, err := catalog.FileBundleLoader{}.LoadBundle(root)
	if err != nil {
		t.Fatalf("LoadBundle error: %v", err)
	}

	_, ok := content.BlockFor("AuthorityHierarchy", "orchestrator")
	if ok {
		t.Error("BundleContent.BlockFor(\"AuthorityHierarchy\", \"orchestrator\") returned ok = true; " +
			"no block in the fixture applies to orchestrator, so ok must be false")
	}
}

// ---------------------------------------------------------------------------
// T6.1 — Failure modes
// ---------------------------------------------------------------------------

// TestBundleLoader_MissingFile_ReturnsErrBundleSourceMissing verifies that when the bundle
// source file does not exist at the expected path, LoadBundle returns an error wrapping
// ErrBundleSourceMissing. The error must also name the bundle file path.
func TestBundleLoader_MissingFile_ReturnsErrBundleSourceMissing(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// Deliberately do NOT write the bundle document.

	_, err := catalog.FileBundleLoader{}.LoadBundle(root)

	if err == nil {
		t.Fatal("LoadBundle returned nil error for a missing bundle file; expected ErrBundleSourceMissing")
	}
	if !errors.Is(err, catalog.ErrBundleSourceMissing) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrBundleSourceMissing", err)
	}
	if !strings.Contains(err.Error(), catalog.BundleSourceRelPath) {
		t.Errorf("error %q does not mention the bundle file path %q; "+
			"errors must identify the file and the problem", err.Error(), catalog.BundleSourceRelPath)
	}
}

// TestBundleLoader_UnparseableDocument_ReturnsErrBundleSourceUnparseable verifies that
// when the bundle file exists but cannot be parsed by docformat.Parse (e.g. malformed YAML
// frontmatter with a duplicate key), LoadBundle returns an error wrapping
// ErrBundleSourceUnparseable. The error must also name the bundle file path.
func TestBundleLoader_UnparseableDocument_ReturnsErrBundleSourceUnparseable(t *testing.T) {
	root := makeTempMosaicRoot(t)
	unparseable := []byte("---\nbundle_version: \"1.0.0\"\nbundle_version: \"duplicate\"\n---\nBody text.\n")
	writeBundleDoc(t, root, unparseable)

	_, err := catalog.FileBundleLoader{}.LoadBundle(root)

	if err == nil {
		t.Fatal("LoadBundle returned nil error for an unparseable document; expected ErrBundleSourceUnparseable")
	}
	if !errors.Is(err, catalog.ErrBundleSourceUnparseable) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrBundleSourceUnparseable", err)
	}
	if !strings.Contains(err.Error(), catalog.BundleSourceRelPath) {
		t.Errorf("error %q does not mention the bundle file path %q",
			err.Error(), catalog.BundleSourceRelPath)
	}
}

// TestBundleLoader_MissingBundleVersion_ReturnsErrBundleVersionMissing verifies that when
// the bundle document's frontmatter carries no `bundle_version` key, LoadBundle returns an
// error wrapping ErrBundleVersionMissing. A versionless bundle defeats staleness detection.
func TestBundleLoader_MissingBundleVersion_ReturnsErrBundleVersionMissing(t *testing.T) {
	root := makeTempMosaicRoot(t)
	docNoVersion := []byte(`---
blocks:
  - name: "AuthorityHierarchy:Subagent"
    target: AuthorityHierarchy
    applies_to: subagent
    specified_in: Development/Designs/AgentTemplateArchitecture.md
---

[[SECTION:AuthorityHierarchy:Subagent]]
Authority hierarchy content.
[[/SECTION:AuthorityHierarchy:Subagent]]
`)
	writeBundleDoc(t, root, docNoVersion)

	_, err := catalog.FileBundleLoader{}.LoadBundle(root)

	if err == nil {
		t.Fatal("LoadBundle returned nil error when bundle_version is absent; expected ErrBundleVersionMissing")
	}
	if !errors.Is(err, catalog.ErrBundleVersionMissing) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrBundleVersionMissing", err)
	}
	if !strings.Contains(err.Error(), catalog.BundleSourceRelPath) {
		t.Errorf("error %q does not mention the bundle file path %q",
			err.Error(), catalog.BundleSourceRelPath)
	}
}

// TestBundleLoader_NoBlocksInFrontmatter_ReturnsErrBundleBlocksMissing verifies that when
// the bundle document's frontmatter contains no `blocks` list (or an empty one), LoadBundle
// returns an error wrapping ErrBundleBlocksMissing.
func TestBundleLoader_NoBlocksInFrontmatter_ReturnsErrBundleBlocksMissing(t *testing.T) {
	root := makeTempMosaicRoot(t)
	docNoBlocks := []byte(`---
bundle_version: "1.0.0"
---

Some body text with no blocks declared.
`)
	writeBundleDoc(t, root, docNoBlocks)

	_, err := catalog.FileBundleLoader{}.LoadBundle(root)

	if err == nil {
		t.Fatal("LoadBundle returned nil error when no blocks are declared; expected ErrBundleBlocksMissing")
	}
	if !errors.Is(err, catalog.ErrBundleBlocksMissing) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrBundleBlocksMissing", err)
	}
}

// TestBundleLoader_BlockDeclMissingTarget_ReturnsErrBundleBlockDeclInvalid verifies that
// when a block declaration in the `blocks` list is missing the `target` field, LoadBundle
// returns an error wrapping ErrBundleBlockDeclInvalid.
func TestBundleLoader_BlockDeclMissingTarget_ReturnsErrBundleBlockDeclInvalid(t *testing.T) {
	root := makeTempMosaicRoot(t)
	docMissingTarget := []byte(`---
bundle_version: "1.0.0"
blocks:
  - name: "AuthorityHierarchy:Subagent"
    applies_to: subagent
    specified_in: Development/Designs/AgentTemplateArchitecture.md
---

[[SECTION:AuthorityHierarchy:Subagent]]
Content.
[[/SECTION:AuthorityHierarchy:Subagent]]
`)
	writeBundleDoc(t, root, docMissingTarget)

	_, err := catalog.FileBundleLoader{}.LoadBundle(root)

	if err == nil {
		t.Fatal("LoadBundle returned nil error when block declaration is missing `target`; expected ErrBundleBlockDeclInvalid")
	}
	if !errors.Is(err, catalog.ErrBundleBlockDeclInvalid) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrBundleBlockDeclInvalid", err)
	}
}

// TestBundleLoader_MissingSectionInBody_ReturnsErrBundleBlockMissing verifies that when a
// declaration's `name` does not match any section in the bundle document body, LoadBundle
// returns an error wrapping ErrBundleBlockMissing.
func TestBundleLoader_MissingSectionInBody_ReturnsErrBundleBlockMissing(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// A valid declaration that names a section not present in the body.
	docMissingSection := []byte(`---
bundle_version: "1.0.0"
blocks:
  - name: "AuthorityHierarchy:Subagent"
    target: AuthorityHierarchy
    applies_to: subagent
    specified_in: Development/Designs/AgentTemplateArchitecture.md
---

Body text but no [[SECTION:AuthorityHierarchy:Subagent]] region.
`)
	writeBundleDoc(t, root, docMissingSection)

	_, err := catalog.FileBundleLoader{}.LoadBundle(root)

	if err == nil {
		t.Fatal("LoadBundle returned nil error when declared section is absent from body; expected ErrBundleBlockMissing")
	}
	if !errors.Is(err, catalog.ErrBundleBlockMissing) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrBundleBlockMissing", err)
	}
	if !strings.Contains(err.Error(), catalog.BundleSourceRelPath) {
		t.Errorf("error %q does not mention the bundle file path %q",
			err.Error(), catalog.BundleSourceRelPath)
	}
}

// TestBundleLoader_EmptyBlockSection_ReturnsErrBundleBlockEmpty verifies that when a
// declared section is present in the body but has no inner content (open tag immediately
// followed by close tag), LoadBundle returns an error wrapping ErrBundleBlockEmpty.
// Deploying an empty block would ship an agent with a region that appears complete and
// instructs nothing.
func TestBundleLoader_EmptyBlockSection_ReturnsErrBundleBlockEmpty(t *testing.T) {
	root := makeTempMosaicRoot(t)
	docEmptySection := []byte(`---
bundle_version: "1.0.0"
blocks:
  - name: "AuthorityHierarchy:Subagent"
    target: AuthorityHierarchy
    applies_to: subagent
    specified_in: Development/Designs/AgentTemplateArchitecture.md
---

[[SECTION:AuthorityHierarchy:Subagent]]
[[/SECTION:AuthorityHierarchy:Subagent]]
`)
	writeBundleDoc(t, root, docEmptySection)

	_, err := catalog.FileBundleLoader{}.LoadBundle(root)

	if err == nil {
		t.Fatal("LoadBundle returned nil error for an empty block section; expected ErrBundleBlockEmpty")
	}
	if !errors.Is(err, catalog.ErrBundleBlockEmpty) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrBundleBlockEmpty", err)
	}
	if !strings.Contains(err.Error(), catalog.BundleSourceRelPath) {
		t.Errorf("error %q does not mention the bundle file path %q",
			err.Error(), catalog.BundleSourceRelPath)
	}
}

// TestBundleLoader_WhitespaceOnlyBlockSection_ReturnsErrBundleBlockEmpty verifies that a
// section containing only whitespace is treated identically to a completely empty section.
// Whitespace-only content would produce an invisible deployed region.
func TestBundleLoader_WhitespaceOnlyBlockSection_ReturnsErrBundleBlockEmpty(t *testing.T) {
	root := makeTempMosaicRoot(t)
	docWhitespace := []byte("---\nbundle_version: \"1.0.0\"\nblocks:\n  - name: \"AuthorityHierarchy:Subagent\"\n    target: AuthorityHierarchy\n    applies_to: subagent\n    specified_in: Development/Designs/AgentTemplateArchitecture.md\n---\n\n[[SECTION:AuthorityHierarchy:Subagent]]\n   \n\t\n\n[[/SECTION:AuthorityHierarchy:Subagent]]\n")
	writeBundleDoc(t, root, docWhitespace)

	_, err := catalog.FileBundleLoader{}.LoadBundle(root)

	if err == nil {
		t.Fatal("LoadBundle returned nil error for a whitespace-only section; " +
			"whitespace-only content must be treated as empty (ErrBundleBlockEmpty)")
	}
	if !errors.Is(err, catalog.ErrBundleBlockEmpty) {
		t.Errorf("error = %v; want an error wrapping catalog.ErrBundleBlockEmpty", err)
	}
}

// TestBundleLoader_FailureReturnsNoPartialContent verifies that a load failure returns a
// zero-value BundleContent. The loader is all-or-nothing: partial content must never
// reach the transform, because the transform would then silently deploy an incomplete bundle.
func TestBundleLoader_FailureReturnsNoPartialContent(t *testing.T) {
	root := makeTempMosaicRoot(t)
	// Use a missing-section document to trigger ErrBundleBlockMissing.
	docMissingSection := []byte(`---
bundle_version: "1.0.0"
blocks:
  - name: "AuthorityHierarchy:Subagent"
    target: AuthorityHierarchy
    applies_to: subagent
    specified_in: Development/Designs/AgentTemplateArchitecture.md
---

No matching section in the body.
`)
	writeBundleDoc(t, root, docMissingSection)

	content, err := catalog.FileBundleLoader{}.LoadBundle(root)

	if err == nil {
		t.Fatal("expected error for a document with a missing section; got nil")
	}
	// On any failure, the returned BundleContent must be the zero value.
	if content.Version != "" {
		t.Errorf("BundleContent.Version = %q on a failed load; want empty string (zero value)", content.Version)
	}
	if len(content.Blocks) != 0 {
		t.Errorf("BundleContent.Blocks has %d entries on a failed load; want zero (zero value)", len(content.Blocks))
	}
}

// ---------------------------------------------------------------------------
// T6.1 — Integration: real bundle document
// ---------------------------------------------------------------------------

// TestBundleLoader_RealBundleDocument_CanBeLoaded verifies that the actual
// Agents/Generic/DeployedSections.md in the MOSAIC repository is loadable. This confirms
// that the real document has not drifted out of the loader's expected format.
func TestBundleLoader_RealBundleDocument_CanBeLoaded(t *testing.T) {
	root, err := catalog.ResolveRoot(".")
	if err != nil {
		t.Fatalf("ResolveRoot: %v — cannot locate MOSAIC repository root", err)
	}

	content, err := catalog.FileBundleLoader{}.LoadBundle(root)

	if err != nil {
		t.Fatalf("LoadBundle(real repository root): %v; "+
			"the real DeployedSections.md must be loadable by the production loader", err)
	}
	if content.Version == "" {
		t.Error("BundleContent.Version is empty for the real bundle document; expected a non-empty version string")
	}
	if len(content.Blocks) == 0 {
		t.Error("BundleContent.Blocks is empty for the real bundle document; expected all five blocks")
	}
}

// TestBundleLoader_RealBundleDocument_CarriesAllFiveBlocks verifies that the real bundle
// document contains all five canonical target names.
func TestBundleLoader_RealBundleDocument_CarriesAllFiveBlocks(t *testing.T) {
	root, err := catalog.ResolveRoot(".")
	if err != nil {
		t.Fatalf("ResolveRoot: %v", err)
	}

	content, err := catalog.FileBundleLoader{}.LoadBundle(root)
	if err != nil {
		t.Fatalf("LoadBundle: %v", err)
	}

	wantTargets := []string{
		"AuthorityHierarchy",
		"ClosingProcedure",
		"ProtocolConstraints",
		"ErrorHandlingCommon",
		"ExecutionPhilosophyCommon",
	}
	targetSet := make(map[string]bool, len(content.Blocks))
	for _, blk := range content.Blocks {
		targetSet[blk.Target] = true
	}
	for _, target := range wantTargets {
		if !targetSet[target] {
			t.Errorf("real bundle document is missing a block with Target %q; all five canonical blocks must be present",
				target)
		}
	}
}
