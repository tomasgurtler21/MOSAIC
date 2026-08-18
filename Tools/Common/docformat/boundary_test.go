package docformat_test

// Tests for body lexing and parsing.
//
// Coverage:
//   - Parsing a document body with a single <Name type="core"> block returns a Section node.
//   - The section node reports Kind == NodeSection and the correct Name.
//   - A section not present in the body returns false from Body.Section.
//   - Parsing a body with <Name type="project"> nested inside a section returns an Injection node.
//   - The injection node reports Kind == NodeInjection and the correct Name.
//   - Compound section names (e.g. "Workflow:my-workflow" via tag+name attr) are parsed and
//     reassembled into the Prefix:id form by Name().
//   - Body.Sections returns all top-level sections in document order.
//   - Body.Injections returns all injections at any nesting depth in document order.
//   - Nested injection nodes have the enclosing section as their Parent.
//   - Top-level section nodes have nil Parent.
//   - Children of a section include its directly nested injections.
//   - Node.Content returns the inner bytes of a node, excluding the two boundary tag lines.
//   - Legacy [INJECTION: name] comment markers are treated as ordinary text, not boundary tags.
//   - Old-style [[SECTION:Name]] double-bracket markers are treated as inert text, not boundaries.
//   - An injection appearing absent from the document returns false from Body.Injection.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"mosaic-common/docformat"
)

const boundaryTestdataDir = "../testdata/boundary"

// boundaryFixtureBytes reads a fixture file from the boundary testdata directory.
func boundaryFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(boundaryTestdataDir, name))
	if err != nil {
		t.Fatalf("read boundary fixture %s: %v", name, err)
	}
	return data
}

// parsedBoundaryFixture parses a boundary fixture and returns the Document.
func parsedBoundaryFixture(t *testing.T, name string) *docformat.Document {
	t.Helper()
	src := boundaryFixtureBytes(t, name)
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return doc
}

// --- Single-section documents ---

func TestBody_Section_SimpleSectionPresent_ReturnsNode(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")

	node, ok := doc.Body().Section("Identity")

	if !ok {
		t.Fatal("Section(\"Identity\") returned false for a document that contains <Identity type=\"core\">")
	}
	if node == nil {
		t.Fatal("Section(\"Identity\") returned a nil node with ok == true")
	}
}

func TestBody_Section_NodeKind_IsSection(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")
	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	if node.Kind() != docformat.NodeSection {
		t.Errorf("Kind: want NodeSection, got %q", node.Kind())
	}
}

func TestBody_Section_NodeName_MatchesTagName(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")
	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	if node.Name() != "Identity" {
		t.Errorf("Name: want %q, got %q", "Identity", node.Name())
	}
}

func TestBody_Section_AbsentName_ReturnsFalse(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")

	_, ok := doc.Body().Section("Capabilities")

	if ok {
		t.Error("Section(\"Capabilities\") returned true for a document that does not contain that section")
	}
}

func TestBody_Section_TopLevelNode_HasNilParent(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")
	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	if node.Parent() != nil {
		t.Error("top-level section node must have nil Parent()")
	}
}

// --- Nested injections ---

func TestBody_Injection_NestedInsideSection_ReturnsNode(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-injection.md")

	node, ok := doc.Body().Injection("IdentityExtension")

	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") returned false for a document that contains <IdentityExtension type=\"project\">")
	}
	if node == nil {
		t.Fatal("Injection(\"IdentityExtension\") returned nil node with ok == true")
	}
}

func TestBody_Injection_NodeKind_IsInjection(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	if node.Kind() != docformat.NodeInjection {
		t.Errorf("Kind: want NodeInjection, got %q", node.Kind())
	}
}

func TestBody_Injection_NodeName_MatchesTagName(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	if node.Name() != "IdentityExtension" {
		t.Errorf("Name: want %q, got %q", "IdentityExtension", node.Name())
	}
}

func TestBody_Injection_NestedNode_ParentIsContainingSection(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-injection.md")
	injection, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}
	section, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	parent := injection.Parent()
	if parent == nil {
		t.Fatal("nested injection node must have a non-nil Parent()")
	}
	if parent != section {
		t.Errorf("injection Parent is not the enclosing section node")
	}
}

func TestBody_Section_Children_IncludesNestedInjections(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-injection.md")
	section, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}
	injection, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	children := section.Children()
	if len(children) == 0 {
		t.Fatal("section Children() is empty; expected at least the nested IdentityExtension injection")
	}

	found := false
	for _, c := range children {
		if c == injection {
			found = true
			break
		}
	}
	if !found {
		t.Error("IdentityExtension injection node not found in section's Children()")
	}
}

// --- Content ---

func TestBody_Node_Content_ExcludesBoundaryTagLines(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")
	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	content := node.Content()

	// The content must not start with or contain the opening tag line.
	if bytes.Contains(content, []byte(`<Identity type="core">`)) {
		t.Error("Node.Content must not contain the opening boundary tag")
	}
	// The content must not contain the closing tag line.
	if bytes.Contains(content, []byte("</Identity>")) {
		t.Error("Node.Content must not contain the closing boundary tag")
	}
	// The inner prose must be present.
	if !bytes.Contains(content, []byte("Section content.")) {
		t.Error("Node.Content must contain the inner body text")
	}
}

// --- Compound section names ---

func TestBody_Section_CompoundName_ParsesCorrectly(t *testing.T) {
	doc := parsedBoundaryFixture(t, "compound-section.md")

	node, ok := doc.Body().Section("Workflow:my-workflow")

	if !ok {
		t.Fatal("Section(\"Workflow:my-workflow\") returned false; compound section names using the name attribute must reassemble to Prefix:id via Name()")
	}
	if node.Name() != "Workflow:my-workflow" {
		t.Errorf("Name: want %q, got %q", "Workflow:my-workflow", node.Name())
	}
}

// --- Multiple sections ---

func TestBody_Sections_ReturnsTopLevelSectionsInDocumentOrder(t *testing.T) {
	doc := parsedBoundaryFixture(t, "multiple-sections.md")

	sections := doc.Body().Sections()

	if len(sections) != 2 {
		t.Fatalf("expected 2 top-level sections, got %d", len(sections))
	}
	if sections[0].Name() != "Identity" {
		t.Errorf("sections[0].Name: want %q, got %q", "Identity", sections[0].Name())
	}
	if sections[1].Name() != "CommunicationProtocol" {
		t.Errorf("sections[1].Name: want %q, got %q", "CommunicationProtocol", sections[1].Name())
	}
}

func TestBody_Injections_ReturnsAllInjectionNodesInDocumentOrder(t *testing.T) {
	// The document has two sections, each with one injection.
	doc := parsedBoundaryFixture(t, "multiple-sections.md")

	injections := doc.Body().Injections()

	if len(injections) != 2 {
		t.Fatalf("expected 2 injections, got %d", len(injections))
	}
	if injections[0].Name() != "IdentityExtension" {
		t.Errorf("injections[0].Name: want %q, got %q", "IdentityExtension", injections[0].Name())
	}
	if injections[1].Name() != "ProtocolExtension" {
		t.Errorf("injections[1].Name: want %q, got %q", "ProtocolExtension", injections[1].Name())
	}
}

func TestBody_Injection_AbsentName_ReturnsFalse(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")

	_, ok := doc.Body().Injection("IdentityExtension")

	if ok {
		t.Error("Injection returned true for an injection that does not appear in the document")
	}
}

// --- Legacy comment markers ---

func TestBody_LegacyCommentMarkers_NotRecognisedAsBoundaryTags(t *testing.T) {
	// The legacy [INJECTION: identity_extension] marker (comment style, snake_case) must
	// not be recognised as an injection boundary. The section's content must include
	// the legacy marker line verbatim.
	doc := parsedBoundaryFixture(t, "legacy-markers.md")
	section, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	content := section.Content()

	// The legacy marker must appear as plain text in the section's content.
	if !bytes.Contains(content, []byte("[INJECTION: identity_extension]")) {
		t.Error("legacy [INJECTION: ...] marker should be present as plain text in section content")
	}
	// No injection node must be created for the legacy marker.
	_, ok = doc.Body().Injection("identity_extension")
	if ok {
		t.Error("Body.Injection returned a node for a legacy comment-style marker — these must not be parsed as boundary tags")
	}
	_, ok = doc.Body().Injection("IdentityExtension")
	if ok {
		t.Error("Body.Injection returned a node for IdentityExtension even though the document only has a legacy marker, not a real <IdentityExtension type=\"project\"> tag")
	}
}

func TestBody_OldStyleDoubleBracketMarkers_NotRecognisedAsBoundaryTags(t *testing.T) {
	// Old-style [[SECTION:Identity]] double-bracket markers are no longer the recognised
	// boundary syntax. They must be treated as inert text and must not produce nodes.
	src := []byte("[[SECTION:Identity]]\nContent.\n[[/SECTION:Identity]]\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if nodes := doc.Body().Sections(); len(nodes) != 0 {
		t.Errorf("[[SECTION:Identity]] (old syntax) must not produce a section node; got %d node(s)", len(nodes))
	}
	// The old-style markers must round-trip as inert text.
	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("old-style markers must round-trip as literal text:\n  want: %q\n  got:  %q", src, got)
	}
}

// --- Node.Bytes includes boundary tags ---

func TestBody_Node_Bytes_IncludesBoundaryTagLines(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")
	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	b := node.Bytes()

	if !bytes.Contains(b, []byte(`<Identity type="core">`)) {
		t.Error("Node.Bytes must include the opening boundary tag")
	}
	if !bytes.Contains(b, []byte("</Identity>")) {
		t.Error("Node.Bytes must include the closing boundary tag")
	}
	if !bytes.Contains(b, []byte("Section content.")) {
		t.Error("Node.Bytes must include the inner content")
	}
}

// --- Inertness: foreign tags are preserved as literal text ---

func TestBoundaryLexer_ForeignTag_NoTypeAttribute_TreatedAsLiteralText(t *testing.T) {
	// A tag-like line with no type attribute must not be recognised as a boundary tag.
	// The fixture contains <Foo> with no type — it must be inert.
	src := boundaryFixtureBytes(t, "foreign-tag-no-type.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse foreign-tag-no-type.md: %v", err)
	}

	// No section or injection nodes — the tag is inert.
	if nodes := doc.Body().Sections(); len(nodes) != 0 {
		t.Errorf("<Foo> with no type attribute must produce no section nodes; got %d", len(nodes))
	}
	// Round-trip: the file must be reproduced byte-for-byte.
	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("foreign-tag-no-type.md round-trip failed — inert tag must be preserved byte-for-byte")
		reportFirstDifference(t, src, got)
	}
}

func TestBoundaryLexer_ForeignTag_UnrecognisedTypeValue_TreatedAsLiteralText(t *testing.T) {
	// A tag with type="widget" (not core/managed/project/custom) must be inert.
	src := boundaryFixtureBytes(t, "foreign-tag-unknown-type.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse foreign-tag-unknown-type.md: %v", err)
	}

	if nodes := doc.Body().Sections(); len(nodes) != 0 {
		t.Errorf("<Foo type=\"widget\"> must produce no section nodes; got %d", len(nodes))
	}
	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("foreign-tag-unknown-type.md round-trip failed — inert tag must be preserved byte-for-byte")
		reportFirstDifference(t, src, got)
	}
}

func TestBoundaryLexer_ForeignTag_TrailingContentOnLine_TreatedAsLiteralText(t *testing.T) {
	// A tag with valid type but trailing content on the same line is not alone on its line
	// and must be inert.
	src := boundaryFixtureBytes(t, "foreign-tag-trailing-content.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse foreign-tag-trailing-content.md: %v", err)
	}

	if nodes := doc.Body().Sections(); len(nodes) != 0 {
		t.Errorf(`<Foo type="core"> trailing words must produce no section nodes; got %d`, len(nodes))
	}
	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("foreign-tag-trailing-content.md round-trip failed — inert tag must be preserved byte-for-byte")
		reportFirstDifference(t, src, got)
	}
}

func TestBoundaryLexer_ForeignTag_SelfClosing_TreatedAsLiteralText(t *testing.T) {
	// A self-closing tag (<Foo type="core"/>) is not a valid MOSAIC boundary form and must be inert.
	src := boundaryFixtureBytes(t, "foreign-tag-self-closing.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse foreign-tag-self-closing.md: %v", err)
	}

	if nodes := doc.Body().Sections(); len(nodes) != 0 {
		t.Errorf(`<Foo type="core"/> (self-closing) must produce no section nodes; got %d`, len(nodes))
	}
	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("foreign-tag-self-closing.md round-trip failed — inert tag must be preserved byte-for-byte")
		reportFirstDifference(t, src, got)
	}
}

func TestBoundaryLexer_ValidTagInsideFencedBlock_RecognisedAsBoundary(t *testing.T) {
	// The parser is line-oriented and code-fence-unaware. A valid MOSAIC boundary tag
	// that appears inside a fenced code block is still recognised as a boundary, because
	// the lexer classifies each line independently with no knowledge of fenced-code context.
	//
	// This is the documented design decision: a fenced tag line that satisfies the boundary
	// rule is still a boundary, as it is under the current (old-syntax) behaviour.
	src := boundaryFixtureBytes(t, "foreign-tag-inside-fenced-block.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse foreign-tag-inside-fenced-block.md: %v", err)
	}

	// The tag inside the fenced block must be recognised as a section boundary.
	_, ok := doc.Body().Section("Identity")
	if !ok {
		t.Error("Section(\"Identity\") returned false; a valid MOSAIC tag inside a fenced code block must be recognised as a boundary because the parser is code-fence-unaware")
	}

	// Round-trip must be byte-exact: the fence markers and surrounding text are preserved verbatim.
	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("foreign-tag-inside-fenced-block.md round-trip failed — surrounding content must be preserved byte-for-byte")
		reportFirstDifference(t, src, got)
	}
}

func TestBoundaryLexer_InlineTag_NoTypeAttribute_TreatedAsLiteralText(t *testing.T) {
	// Inline: a bare tag-like line with no type attribute is inert text and must round-trip.
	src := []byte("<SomeTag>\nContent.\n</SomeTag>\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if nodes := doc.Body().Sections(); len(nodes) != 0 {
		t.Errorf("<SomeTag> with no type attribute must produce no boundary node; got %d node(s)", len(nodes))
	}
	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("bare tag without type attribute must round-trip:\n  want: %q\n  got:  %q", src, got)
	}
}

func TestBoundaryLexer_UnmatchedCloseTag_TreatedAsLiteralText(t *testing.T) {
	// An unmatched close tag encountered when no matching open is on the stack must be
	// treated as literal text and must round-trip byte-for-byte.
	src := []byte("</Orphan>\nSome other content.\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if nodes := doc.Body().Sections(); len(nodes) != 0 {
		t.Errorf("unmatched </Orphan> must not produce a section node; got %d node(s)", len(nodes))
	}
	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("unmatched close tag must round-trip as literal text:\n  want: %q\n  got:  %q", src, got)
	}
}
