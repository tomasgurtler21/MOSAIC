package docformat_test

// Tests for the new Node read API: Version(), Closed(), and compound-name Name() reassembly.
//
// All tests are in the TDD RED phase. The stub implementations in new_api_stub.go cause
// every test to panic at runtime ("not implemented"), which is the expected TDD RED state.
// Tests must compile but must not pass without the real implementation.
//
// Coverage (Version()):
//   - A node whose opening tag carries version="1.10" returns "1.10" from Version().
//   - A node whose opening tag has no version attribute returns "" from Version().
//   - A node whose opening tag has version="" (empty value) returns "" from Version().
//   - Version() does not return an error and returns "" for an absent version: an absent version is a legitimate state.
//
// Coverage (Closed()):
//   - A node with a matching close tag reports Closed() == true.
//   - A node with no close tag (unclosed, from unbalanced-open.md) reports Closed() == false.
//   - Closed() is the supported way to detect an unclosed region; callers must not construct
//     a close-tag string from Name() and search Node.Bytes().
//
// Coverage (Name() — compound-name reassembly):
//   - A section with only a tag name (no name attribute) returns the bare tag name from Name().
//   - A section with name="quick-fix" and tag name "Workflow" returns "Workflow:quick-fix".
//   - Body.Section("Workflow:quick-fix") finds the node addressable by the reassembled name.
//   - A deployed region with name="quick-fix" and tag name "Workflow" reassembles to
//     "Workflow:quick-fix" via Name().
//   - After reassembly, strings.HasPrefix(node.Name(), "Workflow:") returns true.
//
// Coverage (Version() after SetVersion()):
//   - After SetVersion("2.0"), Version() returns "2.0".
//   - After SetVersion(""), Version() returns "" (attribute removed).

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Version() — reading the version attribute
// ---------------------------------------------------------------------------

func TestNodeVersion_OpenTagWithVersion_ReturnsVersionString(t *testing.T) {
	// A node whose opening tag carries version="1.10" must return "1.10" from Version().
	// Parse an inline document with a versioned tag: <CommunicationProtocol type="managed" version="1.10">
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\" version=\"1.10\">\nProtocol content.\n</CommunicationProtocol>\n")

	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	got := node.Version()
	if got != "1.10" {
		t.Errorf("Version(): want %q, got %q", "1.10", got)
	}
}

func TestNodeVersion_OpenTagWithoutVersionAttribute_ReturnsEmptyString(t *testing.T) {
	// A node whose opening tag has no version attribute must return "" from Version().
	// This is a legitimate state — version is optional on all tags.
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\nContent.\n</CommunicationProtocol>\n")

	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	got := node.Version()
	if got != "" {
		t.Errorf("Version() on tag without version attribute: want \"\", got %q", got)
	}
}

func TestNodeVersion_OpenTagWithEmptyVersionValue_ReturnsEmptyString(t *testing.T) {
	// A node with version="" (empty attribute value) must return "" from Version().
	// This is consistent with the absent-attribute case — both mean "no version".
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\" version=\"\">\nContent.\n</CommunicationProtocol>\n")

	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	got := node.Version()
	if got != "" {
		t.Errorf("Version() on tag with version=\"\": want \"\", got %q", got)
	}
}

func TestNodeVersion_SectionNode_WithVersion_ReturnsVersionString(t *testing.T) {
	// Version() works for any node kind, not just deployed nodes.
	doc := parseInlineDoc(t, "<Identity type=\"core\" version=\"3.0\">\nContent.\n</Identity>\n")

	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	got := node.Version()
	if got != "3.0" {
		t.Errorf("Version() on section node: want %q, got %q", "3.0", got)
	}
}

func TestNodeVersion_InjectionNode_WithVersion_ReturnsVersionString(t *testing.T) {
	// Version() works for injection nodes.
	doc := parseInlineDoc(t, "<Identity type=\"core\">\n<IdentityExtension type=\"project\" version=\"2.1\">\nContent.\n</IdentityExtension>\n</Identity>\n")

	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	got := node.Version()
	if got != "2.1" {
		t.Errorf("Version() on injection node: want %q, got %q", "2.1", got)
	}
}

func TestNodeVersion_AbsentVersion_IsLegitimate_ReturnsEmptyString(t *testing.T) {
	// Version() returns string, not (string, error). An absent version is a legitimate
	// state, not a failure. This test verifies both that the function compiles with the
	// correct (error-free) signature and that the return value is "" for a tag with
	// no version attribute.
	doc := parseInlineDoc(t, "<Identity type=\"core\">\nContent.\n</Identity>\n")
	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	got := node.Version()
	if got != "" {
		t.Errorf("Version() on a node with no version attribute: want \"\", got %q", got)
	}
}

func TestNodeVersion_AfterSetVersion_ReturnsNewVersion(t *testing.T) {
	// After SetVersion("2.0"), Version() must return "2.0".
	// This tests the read accessor in conjunction with the mutator.
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\" version=\"1.10\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetVersion("2.0"); err != nil {
		t.Fatalf("SetVersion(\"2.0\"): %v", err)
	}

	got := node.Version()
	if got != "2.0" {
		t.Errorf("Version() after SetVersion(\"2.0\"): want %q, got %q", "2.0", got)
	}
}

func TestNodeVersion_AfterSetVersionEmpty_ReturnsEmptyString(t *testing.T) {
	// After SetVersion(""), Version() must return "" (attribute removed).
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\" version=\"1.10\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetVersion(""); err != nil {
		t.Fatalf("SetVersion(\"\"): %v", err)
	}

	got := node.Version()
	if got != "" {
		t.Errorf("Version() after SetVersion(\"\"): want \"\", got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Closed() — detecting whether a region has a matching close tag
// ---------------------------------------------------------------------------

func TestNodeClosed_NodeWithCloseTag_ReturnsTrue(t *testing.T) {
	// A node with a matching </Name> close tag must report Closed() == true.
	doc := parseInlineDoc(t, "<Identity type=\"core\">\nContent.\n</Identity>\n")
	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	if !node.Closed() {
		t.Error("Closed(): want true for a node with a matching close tag, got false")
	}
}

func TestNodeClosed_NodeWithoutCloseTag_ReturnsFalse(t *testing.T) {
	// A node with no close tag (unclosed region) must report Closed() == false.
	// Uses the unbalanced-open fixture, which has an unclosed section.
	doc := parsedBoundaryFixture(t, "malformed/unbalanced-open.md")

	// Find any section in the document — it should be unclosed.
	sections := doc.Body().Sections()
	if len(sections) == 0 {
		t.Fatal("no sections found in unbalanced-open.md — fix or recreate the fixture")
	}

	unclosed := sections[0] // the unclosed section
	if unclosed.Closed() {
		t.Error("Closed(): want false for an unclosed region, got true")
	}
}

func TestNodeClosed_EmptyNode_WithMatchingClose_ReturnsTrue(t *testing.T) {
	// An empty region (no content between open and close tags) with a close tag is closed.
	doc := parsedBoundaryFixture(t, "empty-injection.md")
	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found")
	}

	if !node.Closed() {
		t.Error("Closed(): want true for an empty injection with a matching close tag, got false")
	}
}

func TestNodeClosed_IsTheSupportedWayToDetectUnclosedRegion(t *testing.T) {
	// Closed() is the ONLY supported way to detect an unclosed region.
	// Callers must not construct a close-tag string from Name() and search Bytes().
	// This test verifies Closed() compiles and works for a closed region so that
	// callers don't have a reason to use the unsupported approach.
	doc := parseInlineDoc(t, "<CommunicationProtocol type=\"managed\">\nContent.\n</CommunicationProtocol>\n")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	// Must return true, and callers must use this, not string construction.
	if !node.Closed() {
		t.Error("Closed(): want true for a properly closed deployed region")
	}
}

// ---------------------------------------------------------------------------
// Name() — compound-name reassembly from tag name + name attribute
// ---------------------------------------------------------------------------

func TestNodeName_SimpleSection_ReturnsBareTagName(t *testing.T) {
	// A section with only a tag name (no name attribute) must return the bare tag name.
	// For <Identity type="core">, Name() returns "Identity".
	doc := parseInlineDoc(t, "<Identity type=\"core\">\nContent.\n</Identity>\n")
	node, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}

	if node.Name() != "Identity" {
		t.Errorf("Name(): want %q, got %q", "Identity", node.Name())
	}
}

func TestNodeName_CompoundSection_ReassemblesTagNamePlusNameAttr(t *testing.T) {
	// For <Workflow type="core" name="my-workflow">, Name() must return "Workflow:my-workflow".
	// This is the load-bearing compatibility guarantee.
	doc := parsedBoundaryFixture(t, "compound-section.md")

	node, ok := doc.Body().Section("Workflow:my-workflow")
	if !ok {
		t.Fatal("Section(\"Workflow:my-workflow\") not found")
	}

	got := node.Name()
	if got != "Workflow:my-workflow" {
		t.Errorf("Name(): want %q, got %q", "Workflow:my-workflow", got)
	}
}

func TestNodeName_CompoundSection_AddressableByReassembledName(t *testing.T) {
	// Body.Section("Workflow:my-workflow") must find the node addressable by the
	// reassembled name. This verifies the addressing API and Name() are consistent.
	doc := parsedBoundaryFixture(t, "compound-section.md")

	node, ok := doc.Body().Section("Workflow:my-workflow")

	if !ok {
		t.Fatal("Section(\"Workflow:my-workflow\") returned false; compound names must be addressable via the reassembled form")
	}
	if node == nil {
		t.Fatal("Section returned nil node with ok==true")
	}
}

func TestNodeName_CompoundDeployed_ReassemblesTagNamePlusNameAttr(t *testing.T) {
	// For <Workflow type="managed" name="quick-fix">, Name() must return "Workflow:quick-fix".
	doc := parsedBoundaryFixture(t, "deployed-compound-name.md")

	node, ok := doc.Body().Deployed("Workflow:quick-fix")
	if !ok {
		t.Fatal("Deployed(\"Workflow:quick-fix\") not found")
	}

	got := node.Name()
	if got != "Workflow:quick-fix" {
		t.Errorf("Name(): want %q, got %q", "Workflow:quick-fix", got)
	}
}

func TestNodeName_CompoundName_HasPrefixConsumerCompatibility(t *testing.T) {
	// Downstream consumers use strings.HasPrefix(node.Name(), "Workflow:") to detect
	// workflow regions. Name() must preserve this compatibility contract.
	doc := parsedBoundaryFixture(t, "compound-section.md")
	node, ok := doc.Body().Section("Workflow:my-workflow")
	if !ok {
		t.Fatal("Section(\"Workflow:my-workflow\") not found")
	}

	if !strings.HasPrefix(node.Name(), "Workflow:") {
		t.Errorf("strings.HasPrefix(node.Name(), \"Workflow:\") returned false; got Name()=%q; consumer compatibility requires the Prefix: prefix", node.Name())
	}
}

func TestNodeName_CompoundCustom_ReassemblesTagNamePlusNameAttr(t *testing.T) {
	// For <Workflow type="custom" name="quick-fix">, Name() must return "Workflow:quick-fix".
	doc := parsedBoundaryFixture(t, "custom-compound-name.md")

	node, ok := doc.Body().Custom("Workflow:quick-fix")
	if !ok {
		t.Fatal("Custom(\"Workflow:quick-fix\") not found")
	}

	got := node.Name()
	if got != "Workflow:quick-fix" {
		t.Errorf("Name(): want %q, got %q", "Workflow:quick-fix", got)
	}
}
