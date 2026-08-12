package docformat_test

// Tests for the region mutation API: Body.AppendRegion, Node.AppendRegion, and
// Body.InsertRegionAfter.
//
// All tests are in the TDD RED phase: these methods and their exported error variables do
// not yet exist. Tests are expected to fail to compile until the implementation is added
// (boundary.go or a companion file in the docformat package).
//
// The error variables expected by this test file are:
//   docformat.ErrInvalidRegionKind    — NodeSection or NodeDeployed passed as kind
//   docformat.ErrInvalidRegionName    — empty or syntactically invalid name
//   docformat.ErrDuplicateRegionName  — name already present anywhere in the document
//   docformat.ErrSiblingNotInDocument — InsertRegionAfter: sibling from a different body
//
// Coverage (Body.AppendRegion — happy path):
//   - Appending NodeCustom returns a non-nil node with the correct Kind and Name.
//   - The appended node is findable via Body.Custom immediately after append.
//   - The appended node's Content matches the supplied content bytes.
//   - Every pre-existing byte of the document is preserved (HasPrefix assertion).
//   - Appending NodeInjection is accepted (not restricted to custom).
//   - Re-parsing the serialised output finds the appended region with its content intact.
//   - A second round-trip of the serialised output is byte-identical (round-trip stability).
//
// Coverage (Body.AppendRegion — edge cases):
//   - Body without trailing newline: the opening tag is on its own line; pre-existing bytes unchanged.
//   - Two sequential appends: both regions present in the correct order.
//   - Content with no boundary tags appended verbatim.
//   - Content without a trailing newline: a newline is added before the closing tag.
//
// Coverage (Body.AppendRegion — error cases):
//   - NodeSection kind → ErrInvalidRegionKind.
//   - NodeDeployed kind → ErrInvalidRegionKind.
//   - Name already present in the document → ErrDuplicateRegionName.
//   - Empty name → ErrInvalidRegionName.
//
// Coverage (Node.AppendRegion — append inside a node):
//   - Appending NodeCustom inside a section returns a node with the correct Kind and Name.
//   - The new node is findable via Body.Custom at the document level.
//   - The returned node's Parent is the containing section.
//   - The section's Content includes the new region's boundary tags.
//   - Duplicate name (existing elsewhere in the document) → ErrDuplicateRegionName.
//
// Coverage (Body.InsertRegionAfter — insert immediately after a sibling):
//   - The inserted region appears immediately after the specified sibling in the output.
//   - The node following the sibling is pushed down and its bytes are preserved.
//   - Sibling from a different document → ErrSiblingNotInDocument.

import (
	"bytes"
	"errors"
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// Body.AppendRegion — happy path
// ---------------------------------------------------------------------------

func TestBodyAppendRegion_Custom_ReturnsNonNilNodeWithCorrectKindAndName(t *testing.T) {
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")

	node, err := doc.Body().AppendRegion(docformat.NodeCustom, "ProjectNotes", []byte("My notes.\n"))

	if err != nil {
		t.Fatalf("AppendRegion: unexpected error: %v", err)
	}
	if node == nil {
		t.Fatal("AppendRegion: returned nil node without error")
	}
	if node.Kind() != docformat.NodeCustom {
		t.Errorf("returned node Kind: want NodeCustom, got %q", node.Kind())
	}
	if node.Name() != "ProjectNotes" {
		t.Errorf("returned node Name: want %q, got %q", "ProjectNotes", node.Name())
	}
}

func TestBodyAppendRegion_Custom_FindableViaBodyCustom(t *testing.T) {
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")

	_, err := doc.Body().AppendRegion(docformat.NodeCustom, "ProjectNotes", []byte("My notes.\n"))
	if err != nil {
		t.Fatalf("AppendRegion: %v", err)
	}

	node, ok := doc.Body().Custom("ProjectNotes")
	if !ok {
		t.Fatal("Body.Custom(\"ProjectNotes\") returned false after AppendRegion")
	}
	if node == nil {
		t.Fatal("Body.Custom returned nil node with ok==true")
	}
}

func TestBodyAppendRegion_Custom_ContentMatchesSupplied(t *testing.T) {
	content := []byte("My project notes.\n")
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")

	node, err := doc.Body().AppendRegion(docformat.NodeCustom, "ProjectNotes", content)
	if err != nil {
		t.Fatalf("AppendRegion: %v", err)
	}

	got := node.Content()
	if !bytes.Equal(got, content) {
		t.Errorf("node Content: want %q, got %q", content, got)
	}
}

func TestBodyAppendRegion_Custom_PreExistingBytesUnchanged(t *testing.T) {
	// Every pre-existing byte must appear verbatim at the start of the serialised output.
	// Appending a region must not rewrite any preceding byte.
	original := "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n"
	doc := parseInlineDoc(t, original)
	originalBytes := []byte(original)

	_, err := doc.Body().AppendRegion(docformat.NodeCustom, "ProjectNotes", []byte("Notes.\n"))
	if err != nil {
		t.Fatalf("AppendRegion: %v", err)
	}

	newBytes := doc.Bytes()
	if !bytes.HasPrefix(newBytes, originalBytes) {
		t.Errorf("pre-existing bytes not preserved:\n  original (%d bytes): %q\n  new output (%d bytes): %q", len(originalBytes), originalBytes, len(newBytes), newBytes)
	}
}

func TestBodyAppendRegion_Injection_Accepted(t *testing.T) {
	// NodeInjection is a user-owned kind and must be accepted by AppendRegion.
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")

	node, err := doc.Body().AppendRegion(docformat.NodeInjection, "ExtraInjection", []byte("Injected.\n"))

	if err != nil {
		t.Fatalf("AppendRegion(NodeInjection): unexpected error: %v", err)
	}
	if node == nil {
		t.Fatal("AppendRegion(NodeInjection): returned nil node without error")
	}
	if node.Kind() != docformat.NodeInjection {
		t.Errorf("returned node Kind: want NodeInjection, got %q", node.Kind())
	}
}

func TestBodyAppendRegion_Custom_ReparseContainsRegionWithContent(t *testing.T) {
	// After appending a region, serialising and re-parsing must produce a tree where the
	// appended region is present with the correct content.
	content := []byte("Custom content.\n")
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")

	_, err := doc.Body().AppendRegion(docformat.NodeCustom, "ProjectNotes", content)
	if err != nil {
		t.Fatalf("AppendRegion: %v", err)
	}

	serialised := doc.Bytes()
	doc2, err := docformat.Parse(serialised)
	if err != nil {
		t.Fatalf("Parse re-serialised output: %v", err)
	}

	node2, ok := doc2.Body().Custom("ProjectNotes")
	if !ok {
		t.Fatal("after re-parse, Body.Custom(\"ProjectNotes\") returned false — appended region must survive re-parse")
	}
	if !bytes.Equal(node2.Content(), content) {
		t.Errorf("re-parsed node Content: want %q, got %q", content, node2.Content())
	}
}

func TestBodyAppendRegion_Custom_RoundTripStability(t *testing.T) {
	// A document with an appended region must serialise byte-identically on a second
	// round trip (append-then-parse round-trip stability).
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")
	_, err := doc.Body().AppendRegion(docformat.NodeCustom, "ProjectNotes", []byte("Notes.\n"))
	if err != nil {
		t.Fatalf("AppendRegion: %v", err)
	}

	output1 := doc.Bytes()

	doc2, err := docformat.Parse(output1)
	if err != nil {
		t.Fatalf("Parse first output: %v", err)
	}
	// Trigger body parsing on doc2 so the second serialisation exercises the parser.
	_ = doc2.Body().Sections()

	output2 := doc2.Bytes()

	if !bytes.Equal(output1, output2) {
		t.Errorf("round-trip stability: output1 != output2")
		reportFirstDifference(t, output1, output2)
	}
}

// ---------------------------------------------------------------------------
// Body.AppendRegion — edge cases
// ---------------------------------------------------------------------------

func TestBodyAppendRegion_NoTrailingNewline_OpeningTagOnItsOwnLine(t *testing.T) {
	// When the body does not end in a newline, the implementation must insert one before
	// the opening tag so the tag is not concatenated onto the last content line.
	// The pre-existing bytes must still appear verbatim at the start of the output.
	bodyWithoutNewline := "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]" // no trailing newline
	doc := parseInlineDoc(t, bodyWithoutNewline)

	_, err := doc.Body().AppendRegion(docformat.NodeCustom, "ProjectNotes", []byte("Notes.\n"))
	if err != nil {
		t.Fatalf("AppendRegion on body without trailing newline: %v", err)
	}

	newBytes := doc.Bytes()

	// The original bytes must appear verbatim at the start of the output.
	if !bytes.HasPrefix(newBytes, []byte(bodyWithoutNewline)) {
		t.Errorf("pre-existing bytes not preserved:\n  original: %q\n  output:   %q", bodyWithoutNewline, newBytes)
	}

	// The opening tag must be on its own line: a newline must immediately precede it.
	openTagOnOwnLine := []byte("\n[[CUSTOM:ProjectNotes]]")
	if !bytes.Contains(newBytes, openTagOnOwnLine) {
		t.Errorf("opening tag is not on its own line; expected %q in output:\n  %q", openTagOnOwnLine, newBytes)
	}
}

func TestBodyAppendRegion_MultipleSequentialAppends_BothRegionsPresentInOrder(t *testing.T) {
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")

	_, err := doc.Body().AppendRegion(docformat.NodeCustom, "FirstNotes", []byte("First.\n"))
	if err != nil {
		t.Fatalf("first AppendRegion: %v", err)
	}
	_, err = doc.Body().AppendRegion(docformat.NodeCustom, "SecondNotes", []byte("Second.\n"))
	if err != nil {
		t.Fatalf("second AppendRegion: %v", err)
	}

	customs := doc.Body().CustomRegions()
	if len(customs) != 2 {
		t.Fatalf("expected 2 custom regions after two appends, got %d", len(customs))
	}
	if customs[0].Name() != "FirstNotes" {
		t.Errorf("customs[0].Name: want %q, got %q", "FirstNotes", customs[0].Name())
	}
	if customs[1].Name() != "SecondNotes" {
		t.Errorf("customs[1].Name: want %q, got %q", "SecondNotes", customs[1].Name())
	}

	// FirstNotes must precede SecondNotes in the serialised output.
	output := doc.Bytes()
	firstIdx := bytes.Index(output, []byte("[[CUSTOM:FirstNotes]]"))
	secondIdx := bytes.Index(output, []byte("[[CUSTOM:SecondNotes]]"))
	if firstIdx < 0 {
		t.Error("[[CUSTOM:FirstNotes]] not found in output")
	}
	if secondIdx < 0 {
		t.Error("[[CUSTOM:SecondNotes]] not found in output")
	}
	if firstIdx >= 0 && secondIdx >= 0 && firstIdx > secondIdx {
		t.Error("FirstNotes must appear before SecondNotes in serialised output")
	}
}

func TestBodyAppendRegion_ContentWithNoBoundaryTags_AppendedVerbatim(t *testing.T) {
	// Content that contains no boundary tag syntax must be preserved exactly as supplied.
	content := []byte("Plain prose with no special markers.\nSecond line.\n")
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nSome content.\n[[/SECTION:Identity]]\n")

	node, err := doc.Body().AppendRegion(docformat.NodeCustom, "ProseNotes", content)
	if err != nil {
		t.Fatalf("AppendRegion: %v", err)
	}

	if !bytes.Equal(node.Content(), content) {
		t.Errorf("node Content: want %q, got %q", content, node.Content())
	}
}

func TestBodyAppendRegion_ContentWithoutTrailingNewline_ClosingTagOnItsOwnLine(t *testing.T) {
	// When the supplied content does not end in a newline, AppendRegion must add one
	// before the closing tag so the closing tag occupies its own line.
	contentWithoutNewline := []byte("Notes without trailing newline")
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")

	_, err := doc.Body().AppendRegion(docformat.NodeCustom, "ProjectNotes", contentWithoutNewline)
	if err != nil {
		t.Fatalf("AppendRegion: %v", err)
	}

	output := doc.Bytes()
	// The closing tag must be on its own line: a newline must immediately precede it.
	closeTagOnOwnLine := []byte("\n[[/CUSTOM:ProjectNotes]]")
	if !bytes.Contains(output, closeTagOnOwnLine) {
		t.Errorf("closing tag is not on its own line; expected %q in output:\n  %q", closeTagOnOwnLine, output)
	}
}

// ---------------------------------------------------------------------------
// Body.AppendRegion — error cases
// ---------------------------------------------------------------------------

func TestBodyAppendRegion_SectionKind_ReturnsErrInvalidRegionKind(t *testing.T) {
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")

	_, err := doc.Body().AppendRegion(docformat.NodeSection, "NewSection", []byte("Content.\n"))

	if err == nil {
		t.Fatal("AppendRegion(NodeSection): want ErrInvalidRegionKind, got nil")
	}
	if !errors.Is(err, docformat.ErrInvalidRegionKind) {
		t.Errorf("AppendRegion(NodeSection): want ErrInvalidRegionKind, got %v", err)
	}
}

func TestBodyAppendRegion_DeployedKind_ReturnsErrInvalidRegionKind(t *testing.T) {
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")

	_, err := doc.Body().AppendRegion(docformat.NodeDeployed, "ToolRegion", []byte("Content.\n"))

	if err == nil {
		t.Fatal("AppendRegion(NodeDeployed): want ErrInvalidRegionKind, got nil")
	}
	if !errors.Is(err, docformat.ErrInvalidRegionKind) {
		t.Errorf("AppendRegion(NodeDeployed): want ErrInvalidRegionKind, got %v", err)
	}
}

func TestBodyAppendRegion_DuplicateName_ReturnsErrDuplicateRegionName(t *testing.T) {
	// The custom region "CustomConstraints" already exists in the fixture.
	doc := parsedBoundaryFixture(t, "empty-custom.md")

	_, err := doc.Body().AppendRegion(docformat.NodeCustom, "CustomConstraints", []byte("Extra.\n"))

	if err == nil {
		t.Fatal("AppendRegion with duplicate name: want ErrDuplicateRegionName, got nil")
	}
	if !errors.Is(err, docformat.ErrDuplicateRegionName) {
		t.Errorf("AppendRegion with duplicate name: want ErrDuplicateRegionName, got %v", err)
	}
}

func TestBodyAppendRegion_EmptyName_ReturnsErrInvalidRegionName(t *testing.T) {
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")

	_, err := doc.Body().AppendRegion(docformat.NodeCustom, "", []byte("Content.\n"))

	if err == nil {
		t.Fatal("AppendRegion with empty name: want ErrInvalidRegionName, got nil")
	}
	if !errors.Is(err, docformat.ErrInvalidRegionName) {
		t.Errorf("AppendRegion with empty name: want ErrInvalidRegionName, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Node.AppendRegion — append a region inside a node
// ---------------------------------------------------------------------------

func TestNodeAppendRegion_Custom_InsideSection_ReturnsNodeWithCorrectKindAndName(t *testing.T) {
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")
	section, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	node, err := section.AppendRegion(docformat.NodeCustom, "SectionNotes", []byte("Notes.\n"))

	if err != nil {
		t.Fatalf("Node.AppendRegion: unexpected error: %v", err)
	}
	if node == nil {
		t.Fatal("Node.AppendRegion: returned nil node without error")
	}
	if node.Kind() != docformat.NodeCustom {
		t.Errorf("returned node Kind: want NodeCustom, got %q", node.Kind())
	}
	if node.Name() != "SectionNotes" {
		t.Errorf("returned node Name: want %q, got %q", "SectionNotes", node.Name())
	}
}

func TestNodeAppendRegion_Custom_FindableViaBodyCustom(t *testing.T) {
	// A node appended inside a section must be reachable at the document level via Body.Custom.
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")
	section, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	_, err := section.AppendRegion(docformat.NodeCustom, "SectionNotes", []byte("Notes.\n"))
	if err != nil {
		t.Fatalf("Node.AppendRegion: %v", err)
	}

	node, ok := doc.Body().Custom("SectionNotes")
	if !ok {
		t.Fatal("Body.Custom(\"SectionNotes\") returned false after Node.AppendRegion — the node must be in the live tree")
	}
	if node == nil {
		t.Fatal("Body.Custom returned nil node with ok==true")
	}
}

func TestNodeAppendRegion_Custom_ParentIsContainingSection(t *testing.T) {
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")
	section, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	node, err := section.AppendRegion(docformat.NodeCustom, "SectionNotes", []byte("Notes.\n"))
	if err != nil {
		t.Fatalf("Node.AppendRegion: %v", err)
	}

	parent := node.Parent()
	if parent == nil {
		t.Fatal("node appended inside a section must have a non-nil Parent()")
	}
	if parent != section {
		t.Errorf("node Parent is not the containing section; got %q", parent.Name())
	}
}

func TestNodeAppendRegion_Custom_SectionContentContainsBoundaryTags(t *testing.T) {
	// After appending a custom region inside a section, the section's Content must contain
	// both the opening and closing boundary tags of the new region.
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")
	section, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	_, err := section.AppendRegion(docformat.NodeCustom, "SectionNotes", []byte("Notes.\n"))
	if err != nil {
		t.Fatalf("Node.AppendRegion: %v", err)
	}

	sectionContent := section.Content()
	if !bytes.Contains(sectionContent, []byte("[[CUSTOM:SectionNotes]]")) {
		t.Error("section Content must contain the opening tag of the appended region")
	}
	if !bytes.Contains(sectionContent, []byte("[[/CUSTOM:SectionNotes]]")) {
		t.Error("section Content must contain the closing tag of the appended region")
	}
}

func TestNodeAppendRegion_DuplicateName_ReturnsErrDuplicateRegionName(t *testing.T) {
	// A name already present anywhere in the document must be rejected, even when the
	// duplicate is in a different node.
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n[[CUSTOM:Existing]]\nExisting.\n[[/CUSTOM:Existing]]\n")
	section, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	_, err := section.AppendRegion(docformat.NodeCustom, "Existing", []byte("Duplicate.\n"))

	if err == nil {
		t.Fatal("Node.AppendRegion with duplicate name: want ErrDuplicateRegionName, got nil")
	}
	if !errors.Is(err, docformat.ErrDuplicateRegionName) {
		t.Errorf("Node.AppendRegion with duplicate name: want ErrDuplicateRegionName, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Body.InsertRegionAfter — insert immediately after a sibling
// ---------------------------------------------------------------------------

func TestBodyInsertRegionAfter_InsertsAfterSpecifiedSibling_CorrectPosition(t *testing.T) {
	// Document: Identity → Constraints (two top-level sections).
	// Insert MiddleNotes after Identity → expected order: Identity → MiddleNotes → Constraints.
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n[[SECTION:Constraints]]\nConstraints.\n[[/SECTION:Constraints]]\n")
	identity, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	_, err := doc.Body().InsertRegionAfter(identity, docformat.NodeCustom, "MiddleNotes", []byte("Middle.\n"))
	if err != nil {
		t.Fatalf("InsertRegionAfter: unexpected error: %v", err)
	}

	output := doc.Bytes()
	identityEnd := bytes.Index(output, []byte("[[/SECTION:Identity]]"))
	middleStart := bytes.Index(output, []byte("[[CUSTOM:MiddleNotes]]"))
	constraintsStart := bytes.Index(output, []byte("[[SECTION:Constraints]]"))

	if identityEnd < 0 {
		t.Fatal("[[/SECTION:Identity]] not found in output")
	}
	if middleStart < 0 {
		t.Fatal("[[CUSTOM:MiddleNotes]] not found in output")
	}
	if constraintsStart < 0 {
		t.Fatal("[[SECTION:Constraints]] not found in output")
	}
	if !(identityEnd < middleStart && middleStart < constraintsStart) {
		t.Errorf("expected order Identity → MiddleNotes → Constraints; got byte positions: identity_end=%d, middle=%d, constraints=%d", identityEnd, middleStart, constraintsStart)
	}
}

func TestBodyInsertRegionAfter_FollowingNodeBytesPreserved(t *testing.T) {
	// After inserting MiddleNotes between Identity and Constraints, Constraints must still
	// be present with its original content intact (no byte corruption of the trailing node).
	doc := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n[[SECTION:Constraints]]\nConstraints content.\n[[/SECTION:Constraints]]\n")
	identity, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	_, err := doc.Body().InsertRegionAfter(identity, docformat.NodeCustom, "MiddleNotes", []byte("Middle.\n"))
	if err != nil {
		t.Fatalf("InsertRegionAfter: %v", err)
	}

	output := doc.Bytes()
	if !bytes.Contains(output, []byte("Constraints content.")) {
		t.Error("Constraints section content was lost or corrupted after InsertRegionAfter")
	}
	if !bytes.Contains(output, []byte("[[SECTION:Constraints]]")) {
		t.Error("[[SECTION:Constraints]] opening tag not found in output after InsertRegionAfter")
	}
	if !bytes.Contains(output, []byte("[[/SECTION:Constraints]]")) {
		t.Error("[[/SECTION:Constraints]] closing tag not found in output after InsertRegionAfter")
	}
}

func TestBodyInsertRegionAfter_SiblingNotInDocument_ReturnsErrSiblingNotInDocument(t *testing.T) {
	// A sibling node obtained from a different document must be rejected.
	doc1 := parseInlineDoc(t, "[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")
	doc2 := parseInlineDoc(t, "[[SECTION:Other]]\nContent.\n[[/SECTION:Other]]\n")

	foreignSibling, ok := doc2.Body().Section("Other")
	if !ok {
		t.Fatal("Section(\"Other\") not found in doc2")
	}

	_, err := doc1.Body().InsertRegionAfter(foreignSibling, docformat.NodeCustom, "Orphaned", []byte("Content.\n"))

	if err == nil {
		t.Fatal("InsertRegionAfter with foreign sibling: want ErrSiblingNotInDocument, got nil")
	}
	if !errors.Is(err, docformat.ErrSiblingNotInDocument) {
		t.Errorf("InsertRegionAfter with foreign sibling: want ErrSiblingNotInDocument, got %v", err)
	}
}
