package docformat_test

// Tests for body addressing and empty/filled injection detection (T4.3).
//
// Coverage:
//   - Body.Section looks up a top-level section by name.
//   - Body.Injection searches at any nesting depth for an injection by name.
//   - Injection.IsEmpty returns true when the injection content is whitespace only.
//   - Injection.IsEmpty returns false when the injection contains non-whitespace content.
//   - Section names are matched exactly (case-sensitive).
//   - Injection names are matched exactly (case-sensitive).
//   - An injection nested inside a section is found by Body.Injection without knowing its parent.
//   - Body.Section with an absent name returns false.
//   - Body.Injection with an absent name returns false.
//   - Node.Bytes for an injection includes the opening and closing boundary tag lines.
//   - Content of a filled injection is accessible via Node.Content (the "lift" operation).
//   - A deployed agent with filled injections has non-empty injection nodes.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/docformat"
)

// --- Section lookup ---

func TestAddressing_Section_ByName_ReturnsCorrectNode(t *testing.T) {
	doc := parsedBoundaryFixture(t, "multiple-sections.md")

	node, ok := doc.Body().Section("CommunicationProtocol")

	if !ok {
		t.Fatal("Section(\"CommunicationProtocol\") returned false")
	}
	if node.Name() != "CommunicationProtocol" {
		t.Errorf("Name: want %q, got %q", "CommunicationProtocol", node.Name())
	}
}

func TestAddressing_Section_NameIsCaseSensitive(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")

	_, ok := doc.Body().Section("identity")

	if ok {
		t.Error("Section(\"identity\") returned true; section names must be matched case-sensitively")
	}
}

func TestAddressing_Section_AbsentName_ReturnsFalse(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")

	_, ok := doc.Body().Section("Constraints")

	if ok {
		t.Error("Section(\"Constraints\") returned true for a document that does not contain it")
	}
}

// --- Injection lookup at any depth ---

func TestAddressing_Injection_FindsNodeNestedInsideSection(t *testing.T) {
	// Body.Injection must find an injection that is a child of a section, without
	// the caller needing to know or specify the parent section name.
	doc := parsedBoundaryFixture(t, "empty-injection.md")

	node, ok := doc.Body().Injection("IdentityExtension")

	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") returned false; must search at any depth")
	}
	if node.Name() != "IdentityExtension" {
		t.Errorf("Name: want %q, got %q", "IdentityExtension", node.Name())
	}
}

func TestAddressing_Injection_NameIsCaseSensitive(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-injection.md")

	_, ok := doc.Body().Injection("identityextension")

	if ok {
		t.Error("Injection(\"identityextension\") returned true; injection names must be matched case-sensitively")
	}
}

func TestAddressing_Injection_AbsentName_ReturnsFalse(t *testing.T) {
	doc := parsedBoundaryFixture(t, "simple-section.md")

	_, ok := doc.Body().Injection("HarnessConstraints")

	if ok {
		t.Error("Injection(\"HarnessConstraints\") returned true for a document that does not contain it")
	}
}

func TestAddressing_Injection_SecondSectionInjection_Found(t *testing.T) {
	// Verify that injections nested in sections other than the first are also found.
	doc := parsedBoundaryFixture(t, "multiple-sections.md")

	node, ok := doc.Body().Injection("ProtocolExtension")

	if !ok {
		t.Fatal("Injection(\"ProtocolExtension\") not found; must search injections in all sections")
	}
	if node.Name() != "ProtocolExtension" {
		t.Errorf("Name: want %q, got %q", "ProtocolExtension", node.Name())
	}
}

// --- Empty/filled detection ---

func TestAddressing_Injection_IsEmpty_TrueForWhitespaceOnlyContent(t *testing.T) {
	// The empty-injection fixture has an [[INJECTION:IdentityExtension]] block with no
	// content between its tags (or only the newline that immediately follows the open tag).
	doc := parsedBoundaryFixture(t, "empty-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	if !node.IsEmpty() {
		t.Errorf("IsEmpty: want true for a whitespace-only injection, got false (content: %q)", node.Content())
	}
}

func TestAddressing_Injection_IsEmpty_FalseForNonBlankContent(t *testing.T) {
	// The filled-injection fixture has actual content between the injection tags.
	doc := parsedBoundaryFixture(t, "filled-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	if node.IsEmpty() {
		t.Error("IsEmpty: want false for an injection with non-whitespace content, got true")
	}
}

func TestAddressing_Injection_FilledContent_VisibleViaContent(t *testing.T) {
	// Node.Content must return the inner bytes of a filled injection so the update
	// pipeline can "lift" the content out and transfer it to a new deployed version.
	doc := parsedBoundaryFixture(t, "filled-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	content := node.Content()

	if !bytes.Contains(content, []byte("Filled injection content line one.")) {
		t.Error("Content must include the inner prose of a filled injection")
	}
	if bytes.Contains(content, []byte("[[INJECTION:IdentityExtension]]")) {
		t.Error("Content must not include the opening boundary tag")
	}
	if bytes.Contains(content, []byte("[[/INJECTION:IdentityExtension]]")) {
		t.Error("Content must not include the closing boundary tag")
	}
}

// --- Node.Bytes for injections ---

func TestAddressing_InjectionNode_Bytes_IncludesOpenAndCloseTags(t *testing.T) {
	doc := parsedBoundaryFixture(t, "filled-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	b := node.Bytes()

	if !bytes.Contains(b, []byte("[[INJECTION:IdentityExtension]]")) {
		t.Error("Node.Bytes must contain the opening injection boundary tag")
	}
	if !bytes.Contains(b, []byte("[[/INJECTION:IdentityExtension]]")) {
		t.Error("Node.Bytes must contain the closing injection boundary tag")
	}
}

// --- Compound section lookup ---

func TestAddressing_Section_CompoundName_Addressable(t *testing.T) {
	// A [[SECTION:Workflow:my-workflow]] block is addressable by its full compound name.
	doc := parsedBoundaryFixture(t, "compound-section.md")

	node, ok := doc.Body().Section("Workflow:my-workflow")

	if !ok {
		t.Fatal("Section(\"Workflow:my-workflow\") returned false; compound section names must be addressable")
	}
	if node.Name() != "Workflow:my-workflow" {
		t.Errorf("Name: want %q, got %q", "Workflow:my-workflow", node.Name())
	}
}

func TestAddressing_Section_CompoundName_ContentIsAccessible(t *testing.T) {
	doc := parsedBoundaryFixture(t, "compound-section.md")
	node, ok := doc.Body().Section("Workflow:my-workflow")
	if !ok {
		t.Fatal("Section(\"Workflow:my-workflow\") not found")
	}

	content := node.Content()

	if !bytes.Contains(content, []byte("Workflow section content.")) {
		t.Error("Content of compound-named section must include its inner prose")
	}
}

// --- Real-world deployed agent: addressing and filled detection ---

func TestAddressing_DeployedAgent_FilledInjection_IsNotEmpty(t *testing.T) {
	// The OpenCode example project agent has a filled [[INJECTION:IdentityExtension]]
	// block. After parsing, that injection must be non-empty, confirming that the body
	// parser correctly classifies filled vs empty injections in a real deployed file.
	fpath := filepath.Join(repoRoot(), "Agents", "OpenCode", "ExampleProject", "Agents", "test-runner.md")
	src, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found in deployed agent")
	}
	if node.IsEmpty() {
		t.Error("IsEmpty: deployed agent injection is filled but IsEmpty returned true")
	}
}

func TestAddressing_DeployedAgent_FilledInjection_ContentContainsPayload(t *testing.T) {
	// The Content of the filled injection must contain the agent-specific prose that
	// was injected during deployment, confirming the lift operation can retrieve it.
	fpath := filepath.Join(repoRoot(), "Agents", "OpenCode", "ExampleProject", "Agents", "test-runner.md")
	src, err := os.ReadFile(fpath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found in deployed agent")
	}
	content := node.Content()

	// The deployed test-runner has domain-specific step 6 injected (from the fixture we read).
	// Verify the content is non-empty and does not contain the boundary tags.
	if len(bytes.TrimSpace(content)) == 0 {
		t.Error("Content of filled deployed injection must not be blank")
	}
	if bytes.Contains(content, []byte("[[INJECTION:")) {
		t.Error("Content of injection must not include boundary tag syntax")
	}
}
