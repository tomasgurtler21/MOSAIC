package docformat_test

// Tests for body lexing and parsing (T4.1).
//
// Coverage:
//   - Parsing a document body with a single [[SECTION:Name]] block returns a Section node.
//   - The section node reports Kind == NodeSection and the correct Name.
//   - A section not present in the body returns false from Body.Section.
//   - Parsing a body with [[INJECTION:Name]] nested inside a section returns an Injection node.
//   - The injection node reports Kind == NodeInjection and the correct Name.
//   - Compound section names (e.g. "Workflow:my-workflow") are parsed as a single opaque name.
//   - Body.Sections returns all top-level sections in document order.
//   - Body.Injections returns all injections at any nesting depth in document order.
//   - Nested injection nodes have the enclosing section as their Parent.
//   - Top-level section nodes have nil Parent.
//   - Children of a section include its directly nested injections.
//   - Node.Content returns the inner bytes of a node, excluding the two boundary tag lines.
//   - Legacy [INJECTION: name] comment markers are treated as ordinary text, not boundary tags.
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
		t.Fatal("Section(\"Identity\") returned false for a document that contains [[SECTION:Identity]]")
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
		t.Fatal("Injection(\"IdentityExtension\") returned false for a document that contains [[INJECTION:IdentityExtension]]")
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
	if bytes.Contains(content, []byte("[[SECTION:Identity]]")) {
		t.Error("Node.Content must not contain the opening boundary tag")
	}
	// The content must not contain the closing tag line.
	if bytes.Contains(content, []byte("[[/SECTION:Identity]]")) {
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
		t.Fatal("Section(\"Workflow:my-workflow\") returned false; compound section names must be parsed as a single opaque name")
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
		t.Error("Body.Injection returned a node for IdentityExtension even though the document only has a legacy marker, not a real [[INJECTION:IdentityExtension]] tag")
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

	if !bytes.Contains(b, []byte("[[SECTION:Identity]]")) {
		t.Error("Node.Bytes must include the opening boundary tag")
	}
	if !bytes.Contains(b, []byte("[[/SECTION:Identity]]")) {
		t.Error("Node.Bytes must include the closing boundary tag")
	}
	if !bytes.Contains(b, []byte("Section content.")) {
		t.Error("Node.Bytes must include the inner content")
	}
}
