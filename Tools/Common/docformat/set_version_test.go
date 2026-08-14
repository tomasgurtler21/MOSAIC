package docformat_test

// Tests for Node.SetVersion — setting, replacing, and removing the version attribute
// on a region's opening tag.
//
// All tests are in the TDD RED phase. The stub implementation in new_api_stub.go causes
// every test to panic at runtime ("not implemented"), which is the expected TDD RED state.
// Tests must compile but must not pass without the real implementation.
//
// Specification (from ContractsDesign.md):
//
//   SetVersion(version string) error
//
//   Sets, replaces, or removes the version attribute on the region's opening tag.
//   An empty version removes the attribute. Only the opening tag line is re-rendered
//   in canonical form; every other byte of the document is untouched.
//   Returns ErrVersionNotSettable when the node has no opening tag.
//   Returns ErrInvalidVersion when version contains a quote character, a line
//   terminator, or leading/trailing whitespace.
//
// Coverage (happy path):
//   - Set version on a node with no prior version attribute.
//   - Replace an existing version attribute with a new value.
//   - Remove version by passing "".
//   - Re-set version to the same value as already present (idempotent write).
//
// Coverage (canonical form):
//   - Canonical attribute order: type, then name (if compound), then version.
//   - Tag line is re-rendered; existing whitespace/quote style is normalised.
//   - Only the opening tag line changes; all other bytes are preserved byte-for-byte.
//   - Line terminator of the original opening tag line is preserved.
//
// Coverage (sentinel errors):
//   - ErrInvalidVersion returned for version containing a double-quote character.
//   - ErrInvalidVersion returned for version containing a single-quote character.
//   - ErrInvalidVersion returned for version containing a newline character.
//   - ErrInvalidVersion returned for version with leading whitespace.
//   - ErrInvalidVersion returned for version with trailing whitespace.
//   - ErrVersionNotSettable is not reachable via the public API: unmatched close tags
//     are literal text (not nodes), so a *Node with no opening tag cannot be obtained
//     from Parse. This is documented and exercised by asserting that a document with
//     an unmatched close tag produces zero boundary nodes.
//
// Coverage (terminator preservation):
//   - CRLF-terminated opening tag line remains CRLF-terminated after SetVersion.
//   - LF-terminated opening tag line remains LF-terminated after SetVersion.

import (
	"bytes"
	"errors"
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// Happy-path: set, replace, remove
// ---------------------------------------------------------------------------

func TestSetVersion_OnNodeWithNoVersion_SetsVersion(t *testing.T) {
	// SetVersion on a node that has no prior version attribute must add the attribute.
	// After the call, Version() must return the new value.
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetVersion("1.10"); err != nil {
		t.Fatalf("SetVersion(\"1.10\"): unexpected error: %v", err)
	}

	if got := node.Version(); got != "1.10" {
		t.Errorf("Version() after SetVersion(\"1.10\"): want %q, got %q", "1.10", got)
	}
}

func TestSetVersion_OnNodeWithExistingVersion_ReplacesVersion(t *testing.T) {
	// SetVersion on a node that already has a version attribute must replace it.
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\" version=\"1.10\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetVersion("2.0"); err != nil {
		t.Fatalf("SetVersion(\"2.0\"): unexpected error: %v", err)
	}

	if got := node.Version(); got != "2.0" {
		t.Errorf("Version() after replacing: want %q, got %q", "2.0", got)
	}
}

func TestSetVersion_EmptyString_RemovesVersionAttribute(t *testing.T) {
	// SetVersion("") must remove the version attribute entirely.
	// After removal, Version() must return "".
	// The output must not contain version="" or version= in any form.
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\" version=\"1.10\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetVersion(""); err != nil {
		t.Fatalf("SetVersion(\"\"): unexpected error: %v", err)
	}

	if got := node.Version(); got != "" {
		t.Errorf("Version() after removal: want \"\", got %q", got)
	}

	// Verify the attribute is gone from the raw bytes too.
	if bytes.Contains(doc.Bytes(), []byte("version=")) {
		t.Error("doc.Bytes() still contains \"version=\" after SetVersion(\"\")")
	}
}

func TestSetVersion_SameValueAsAlreadyPresent_IsIdempotent(t *testing.T) {
	// SetVersion with the value already present must succeed and produce the same output.
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\" version=\"1.10\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetVersion("1.10"); err != nil {
		t.Fatalf("SetVersion(\"1.10\") (idempotent): unexpected error: %v", err)
	}

	if got := node.Version(); got != "1.10" {
		t.Errorf("Version() after idempotent write: want %q, got %q", "1.10", got)
	}
}

// ---------------------------------------------------------------------------
// Canonical form: only the tag line changes
// ---------------------------------------------------------------------------

func TestSetVersion_CanonicalForm_OnlyOpeningTagLineChanges(t *testing.T) {
	// SetVersion must not change any byte outside the opening tag line.
	// The body text and the closing tag must be preserved byte-for-byte.
	const bodyText = "Body text that must not change.\n"
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\n"+bodyText+"</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetVersion("1.10"); err != nil {
		t.Fatalf("SetVersion(\"1.10\"): unexpected error: %v", err)
	}

	output := doc.Bytes()
	if !bytes.Contains(output, []byte(bodyText)) {
		t.Errorf("body text %q not found in output after SetVersion", bodyText)
	}
	if !bytes.Contains(output, []byte("</CommunicationProtocol>")) {
		t.Error("closing tag </CommunicationProtocol> not found in output after SetVersion")
	}
}

func TestSetVersion_CanonicalForm_AttributeOrderIsTypeNameVersion(t *testing.T) {
	// After SetVersion, the canonical attribute order is: type, then name (if compound),
	// then version. This is verified by inspecting the opening tag line in the output.
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetVersion("1.10"); err != nil {
		t.Fatalf("SetVersion: unexpected error: %v", err)
	}

	output := doc.Bytes()
	// The canonical tag line must contain: type="managed" before version="1.10"
	typeIdx := bytes.Index(output, []byte(`type="managed"`))
	versionIdx := bytes.Index(output, []byte(`version="1.10"`))
	if typeIdx < 0 {
		t.Error("canonical tag line does not contain type=\"managed\"")
	}
	if versionIdx < 0 {
		t.Error("canonical tag line does not contain version=\"1.10\"")
	}
	if typeIdx >= 0 && versionIdx >= 0 && typeIdx > versionIdx {
		t.Errorf("type attribute (offset %d) must appear before version attribute (offset %d) in canonical output", typeIdx, versionIdx)
	}
}

func TestSetVersion_CanonicalForm_CompoundName_AttributeOrderIsTypeNameVersion(t *testing.T) {
	// For a compound-name node <Workflow type="core" name="my-workflow">,
	// canonical attribute order after SetVersion is: type, name, version.
	doc := parsedBoundaryFixture(t, "compound-section.md")
	node, ok := doc.Body().Section("Workflow:my-workflow")
	if !ok {
		t.Fatal("Section(\"Workflow:my-workflow\") not found")
	}

	if err := node.SetVersion("1.0"); err != nil {
		t.Fatalf("SetVersion: unexpected error: %v", err)
	}

	output := doc.Bytes()
	typeIdx := bytes.Index(output, []byte(`type="core"`))
	nameIdx := bytes.Index(output, []byte(`name="my-workflow"`))
	versionIdx := bytes.Index(output, []byte(`version="1.0"`))
	if typeIdx < 0 {
		t.Error("canonical tag line does not contain type=\"core\"")
	}
	if nameIdx < 0 {
		t.Error("canonical tag line does not contain name=\"my-workflow\"")
	}
	if versionIdx < 0 {
		t.Error("canonical tag line does not contain version=\"1.0\"")
	}
	if typeIdx >= 0 && nameIdx >= 0 && typeIdx > nameIdx {
		t.Error("type must appear before name in canonical attribute order")
	}
	if nameIdx >= 0 && versionIdx >= 0 && nameIdx > versionIdx {
		t.Error("name must appear before version in canonical attribute order")
	}
}

func TestSetVersion_CanonicalForm_NonCanonicalInputIsNormalised(t *testing.T) {
	// Input tags with non-canonical attribute order or single quotes are normalised on write.
	// Non-canonical input: version comes before type, single quotes used.
	// After SetVersion, the output must be canonical (type before version, double quotes).
	doc := parseInlineDoc(t, "<CommunicationProtocol version='1.10' type='managed'>\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found — lenient parse may not have accepted non-canonical input")
	}

	if err := node.SetVersion("2.0"); err != nil {
		t.Fatalf("SetVersion: unexpected error: %v", err)
	}

	output := doc.Bytes()
	// After normalisation: type must precede version, double quotes used
	typeIdx := bytes.Index(output, []byte(`type="managed"`))
	versionIdx := bytes.Index(output, []byte(`version="2.0"`))
	if typeIdx < 0 {
		t.Error("normalised output missing type=\"managed\"")
	}
	if versionIdx < 0 {
		t.Error("normalised output missing version=\"2.0\"")
	}
	if bytes.Contains(output, []byte("version='")) {
		t.Error("normalised output must use double quotes, not single quotes")
	}
}

// ---------------------------------------------------------------------------
// Line terminator preservation
// ---------------------------------------------------------------------------

func TestSetVersion_LineTerminatorPreservation_LFTerminatorPreserved(t *testing.T) {
	// If the opening tag line is LF-terminated, SetVersion must preserve that terminator.
	// (LF is the standard; this is the baseline case.)
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetVersion("1.10"); err != nil {
		t.Fatalf("SetVersion: unexpected error: %v", err)
	}

	output := doc.Bytes()
	// Must contain the canonical open tag followed immediately by LF (not CRLF).
	tag := []byte("<CommunicationProtocol type=\"managed\" version=\"1.10\">\n")
	if !bytes.Contains(output, tag) {
		t.Errorf("output does not contain LF-terminated tag line; output:\n%s", output)
	}
}

func TestSetVersion_LineTerminatorPreservation_CRLFTerminatorPreserved(t *testing.T) {
	// If the opening tag line is CRLF-terminated, SetVersion must preserve CRLF.
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\r\nContent.\r\n</CommunicationProtocol>\r\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetVersion("1.10"); err != nil {
		t.Fatalf("SetVersion: unexpected error: %v", err)
	}

	output := doc.Bytes()
	// Must contain the canonical open tag followed immediately by CRLF.
	tag := []byte("<CommunicationProtocol type=\"managed\" version=\"1.10\">\r\n")
	if !bytes.Contains(output, tag) {
		t.Errorf("output does not contain CRLF-terminated tag line; output:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// Sentinel errors: ErrInvalidVersion
// ---------------------------------------------------------------------------

func TestSetVersion_VersionContainsDoubleQuote_ReturnsErrInvalidVersion(t *testing.T) {
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	err := node.SetVersion("1\"10")
	if !errors.Is(err, docformat.ErrInvalidVersion) {
		t.Errorf("SetVersion with double-quote: want ErrInvalidVersion, got %v", err)
	}
}

func TestSetVersion_VersionContainsSingleQuote_ReturnsErrInvalidVersion(t *testing.T) {
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	err := node.SetVersion("1'10")
	if !errors.Is(err, docformat.ErrInvalidVersion) {
		t.Errorf("SetVersion with single-quote: want ErrInvalidVersion, got %v", err)
	}
}

func TestSetVersion_VersionContainsNewline_ReturnsErrInvalidVersion(t *testing.T) {
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	err := node.SetVersion("1\n10")
	if !errors.Is(err, docformat.ErrInvalidVersion) {
		t.Errorf("SetVersion with newline: want ErrInvalidVersion, got %v", err)
	}
}

func TestSetVersion_VersionHasLeadingWhitespace_ReturnsErrInvalidVersion(t *testing.T) {
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	err := node.SetVersion(" 1.10")
	if !errors.Is(err, docformat.ErrInvalidVersion) {
		t.Errorf("SetVersion with leading space: want ErrInvalidVersion, got %v", err)
	}
}

func TestSetVersion_VersionHasTrailingWhitespace_ReturnsErrInvalidVersion(t *testing.T) {
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	err := node.SetVersion("1.10 ")
	if !errors.Is(err, docformat.ErrInvalidVersion) {
		t.Errorf("SetVersion with trailing space: want ErrInvalidVersion, got %v", err)
	}
}

func TestSetVersion_VersionContainsCR_ReturnsErrInvalidVersion(t *testing.T) {
	// Carriage return is a line terminator character and must be rejected.
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	err := node.SetVersion("1\r10")
	if !errors.Is(err, docformat.ErrInvalidVersion) {
		t.Errorf("SetVersion with CR: want ErrInvalidVersion, got %v", err)
	}
}

func TestSetVersion_ValidVersion_DoesNotReturnErrInvalidVersion(t *testing.T) {
	// A valid version string (alphanumeric with dots and dashes) must not return an error,
	// and Version() must subsequently return the value that was set.
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetVersion("1.10-beta"); err != nil {
		t.Errorf("SetVersion(\"1.10-beta\"): unexpected error: %v", err)
	}
	if got := node.Version(); got != "1.10-beta" {
		t.Errorf("Version() after SetVersion(\"1.10-beta\"): want %q, got %q", "1.10-beta", got)
	}
}

// ---------------------------------------------------------------------------
// Sentinel errors: ErrVersionNotSettable
// ---------------------------------------------------------------------------

func TestSetVersion_ErrVersionNotSettable_UnreachableViaPublicAPI(t *testing.T) {
	// ErrVersionNotSettable is returned by SetVersion when the node has no opening tag
	// (n.openTag is empty). Such nodes do not arise through the current public parser API:
	// an unmatched close tag is treated as literal text per the leniency contract, so
	// callers cannot obtain a *Node with no opening tag from Parse.
	//
	// This test exercises the leniency contract directly: a document containing only
	// an unmatched close tag (malformed/unmatched-close.md) must produce zero section
	// nodes. Since no *Node can be obtained, ErrVersionNotSettable cannot be triggered.
	//
	// The implementation must still guard against an empty openTag and return
	// ErrVersionNotSettable if such a node ever arises (e.g. through future internal
	// construction paths or synthetic nodes created outside the parser).
	doc := parsedBoundaryFixture(t, "malformed/unmatched-close.md")

	// An unmatched close tag must produce zero boundary nodes.
	if sections := doc.Body().Sections(); len(sections) != 0 {
		t.Errorf("expected 0 sections in malformed/unmatched-close.md, got %d; "+
			"unmatched close tags must be literal text, not nodes", len(sections))
	}
	if injections := doc.Body().Injections(); len(injections) != 0 {
		t.Errorf("expected 0 injections in malformed/unmatched-close.md, got %d", len(injections))
	}
}

func TestSetVersion_UnclosedNodeWithOpenTag_DoesNotReturnErrVersionNotSettable(t *testing.T) {
	// A node from a malformed document (opened but never closed) still has an opening tag
	// and SetVersion should work. This test uses the unbalanced-open fixture, which has
	// a section that opens but never closes.
	//
	// Note: ErrVersionNotSettable is for nodes that have NO opening tag at all (e.g.,
	// synthetic orphan nodes). An unclosed node has an opening tag and SetVersion must
	// work on it. This test verifies the function does NOT return ErrVersionNotSettable
	// for an unclosed (but open-tag-bearing) node, and that it returns no error at all.
	doc := parsedBoundaryFixture(t, "malformed/unbalanced-open.md")
	sections := doc.Body().Sections()
	if len(sections) == 0 {
		t.Fatal("no sections found in unbalanced-open.md — fix or recreate the fixture")
	}

	node := sections[0]
	err := node.SetVersion("1.0")
	if errors.Is(err, docformat.ErrVersionNotSettable) {
		t.Error("SetVersion on an unclosed (but open-tag-bearing) node must not return ErrVersionNotSettable")
	}
	if err != nil {
		t.Errorf("SetVersion on an unclosed (but open-tag-bearing) node: unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Diff assertions: the diff is minimal
// ---------------------------------------------------------------------------

func TestSetVersion_DiffIsMinimal_OnlyOpenTagLineChanged(t *testing.T) {
	// SetVersion must produce a minimal diff. Only the opening tag line may change.
	// This is verified by comparing before and after: all lines except the opening tag
	// must be identical.
	const before = "<CommunicationProtocol type=\"managed\">\nFirst line.\nSecond line.\n</CommunicationProtocol>\nTrailing content.\n"
	doc := parseInlineDoc(t, before)
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetVersion("1.10"); err != nil {
		t.Fatalf("SetVersion: unexpected error: %v", err)
	}

	after := doc.Bytes()

	// All expected content except the opening tag must be present unchanged.
	for _, line := range []string{"First line.\n", "Second line.\n", "</CommunicationProtocol>\n", "Trailing content.\n"} {
		if !bytes.Contains(after, []byte(line)) {
			t.Errorf("expected unchanged line %q not found in output", line)
		}
	}

	// The original (version-less) opening tag must no longer be present.
	if bytes.Contains(after, []byte("<CommunicationProtocol type=\"managed\">\n")) {
		t.Error("original opening tag (without version) still present after SetVersion")
	}

	// The new canonical opening tag must be present.
	if !bytes.Contains(after, []byte("<CommunicationProtocol type=\"managed\" version=\"1.10\">\n")) {
		t.Errorf("new canonical opening tag not found in output; output:\n%s", after)
	}
}
