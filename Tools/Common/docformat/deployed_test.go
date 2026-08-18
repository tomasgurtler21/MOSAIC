package docformat_test

// Tests for the DEPLOYED boundary marker: lexing, addressing, enumeration, and mutation.
//
// Coverage:
//
//   Lexing:
//   - <Name type="managed"> open tag is recognised; the resulting node has Kind NodeDeployed.
//   - </Name> close tag correctly closes the deployed node.
//   - Compound names expressed via the name attribute (e.g. name="quick-fix") are accepted.
//   - Tag names containing hyphens (e.g. <my-harness type="managed">) are accepted.
//   - Tags missing the type attribute are not recognised as boundary tags.
//   - Tags with type="deployed" (misspelling of "managed") are not recognised.
//   - Tags with type="MANAGED" (wrong case) are not recognised.
//   - An unmatched </Name> close tag (no open on the stack) is treated as literal text.
//
//   Addressing and enumeration:
//   - Body.Deployed(name) returns the deployed node by exact name.
//   - Body.Deployed(name) returns false for an absent name.
//   - Body.Deployed(name) finds a deployed node nested inside a section.
//   - Body.DeployedRegions() returns all deployed nodes in document order.
//   - Body.DeployedRegions() returns an empty slice when the document has no deployed nodes.
//   - A deployed node nested inside a section has that section as its Parent.
//   - A top-level deployed node has nil Parent.
//   - Body.Injections() does NOT include deployed nodes.
//   - Body.DeployedRegions() does NOT include injection nodes.
//   - Body.Regions() returns injections and deployed nodes interleaved in document order.
//
//   Content mutation and round-trip:
//   - Node.Content returns inner bytes of a deployed node, excluding the boundary tag lines.
//   - Node.SetContent replaces a deployed node's content; Content returns the new bytes.
//   - Node.SetContent causes IsEmpty to return false for non-blank content.
//   - Node.Clear empties a deployed node's content; IsEmpty returns true afterwards.
//   - Node.Clear leaves the boundary tags intact; Node.Bytes still contains them.
//   - Node.Bytes includes the opening tag, content, and closing tag.
//   - Node.Bytes after SetContent contains the new content and both boundary tags.
//   - Unmutated parse/serialise round-trip of a document mixing all three marker kinds is byte-identical.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// Lexing: open and close forms
// ---------------------------------------------------------------------------

func TestBoundaryLexer_Deployed_OpenTag_RecognisedAsDeployedNode(t *testing.T) {
	// <CommunicationProtocol type="managed"> must be recognised as an open boundary tag and
	// produce a deployed node retrievable via Body.Deployed.
	doc := parsedBoundaryFixture(t, "empty-deployed.md")

	node, ok := doc.Body().Deployed("CommunicationProtocol")

	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") returned false for a document that contains <CommunicationProtocol type=\"managed\">")
	}
	if node == nil {
		t.Fatal("Deployed(\"CommunicationProtocol\") returned nil node with ok == true")
	}
}

func TestBoundaryLexer_Deployed_NodeKind_IsDeployed(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-deployed.md")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if node.Kind() != docformat.NodeDeployed {
		t.Errorf("Kind: want NodeDeployed, got %q", node.Kind())
	}
}

func TestBoundaryLexer_Deployed_NodeName_MatchesTagName(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-deployed.md")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if node.Name() != "CommunicationProtocol" {
		t.Errorf("Name: want %q, got %q", "CommunicationProtocol", node.Name())
	}
}

func TestBoundaryLexer_Deployed_CloseTag_CorrectlyClosesNode(t *testing.T) {
	// A deployed node with content between its open and close tags must expose that content
	// via Content(). This verifies the close tag is recognised and terminates the node.
	doc := parsedBoundaryFixture(t, "filled-deployed.md")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	content := node.Content()
	if len(bytes.TrimSpace(content)) == 0 {
		t.Error("deployed node content is empty; the close tag may not have been recognised, leaving the node unclosed and swallowing the content")
	}
}

// ---------------------------------------------------------------------------
// Lexing: name variants (compound via name attribute, hyphens)
// ---------------------------------------------------------------------------

func TestBoundaryLexer_Deployed_CompoundName_WithColons_Accepted(t *testing.T) {
	// Compound names use the name attribute: <Workflow type="managed" name="quick-fix">.
	// Body.Deployed must find such a node using the reassembled name "Workflow:quick-fix".
	doc := parsedBoundaryFixture(t, "deployed-compound-name.md")

	node, ok := doc.Body().Deployed("Workflow:quick-fix")

	if !ok {
		t.Fatal("Deployed(\"Workflow:quick-fix\") returned false; deployed compound names must be accepted")
	}
	if node.Name() != "Workflow:quick-fix" {
		t.Errorf("Name: want %q, got %q", "Workflow:quick-fix", node.Name())
	}
}

func TestBoundaryLexer_Deployed_HyphenName_Accepted(t *testing.T) {
	// Tag names containing hyphens (<my-harness type="managed">) must be accepted.
	doc := parsedBoundaryFixture(t, "deployed-hyphen-name.md")

	node, ok := doc.Body().Deployed("my-harness")

	if !ok {
		t.Fatal("Deployed(\"my-harness\") returned false; tag names containing hyphens must be accepted")
	}
	if node.Name() != "my-harness" {
		t.Errorf("Name: want %q, got %q", "my-harness", node.Name())
	}
}

// ---------------------------------------------------------------------------
// Lexing: rejected / near-miss forms (new XML syntax)
// ---------------------------------------------------------------------------

func TestBoundaryLexer_Deployed_NoTypeAttribute_NotRecognisedAsBoundaryTag(t *testing.T) {
	// <CommunicationProtocol> with no type attribute must be treated as literal text.
	// The inertness rule requires the type attribute with a recognised value.
	src := []byte("<CommunicationProtocol>\n</CommunicationProtocol>\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if nodes := doc.Body().DeployedRegions(); len(nodes) != 0 {
		t.Errorf("tag without type attribute must not produce a deployed node; got %d node(s)", len(nodes))
	}
	// Round-trip: the tag-like lines must be preserved as literal text.
	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("tag without type attribute must round-trip as literal text:\n  want: %q\n  got:  %q", src, got)
	}
}

func TestBoundaryLexer_Deployed_WrongTypeValue_NotRecognised(t *testing.T) {
	// <CommunicationProtocol type="deployed"> — type value "deployed" is not a recognised
	// MOSAIC kind (the correct value is "managed"). Must be treated as literal text.
	src := []byte("<CommunicationProtocol type=\"deployed\">\n</CommunicationProtocol>\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if nodes := doc.Body().DeployedRegions(); len(nodes) != 0 {
		t.Errorf("type=\"deployed\" (misspelling of \"managed\") must not produce a deployed node; got %d node(s)", len(nodes))
	}
}

func TestBoundaryLexer_Deployed_UppercaseTypeValue_NotRecognised(t *testing.T) {
	// <CommunicationProtocol type="MANAGED"> — type value matching is case-sensitive;
	// only lowercase "managed" is the canonical recognised form.
	src := []byte("<CommunicationProtocol type=\"MANAGED\">\n</CommunicationProtocol>\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if nodes := doc.Body().DeployedRegions(); len(nodes) != 0 {
		t.Errorf("type=\"MANAGED\" (wrong case) must not produce a deployed node; got %d node(s)", len(nodes))
	}
}

func TestBoundaryLexer_Deployed_UnmatchedCloseTag_TreatedAsLiteralText(t *testing.T) {
	// </CommunicationProtocol> encountered when no matching open tag is on the stack must be
	// treated as literal text and must not produce a deployed node.
	src := []byte("</CommunicationProtocol>\nSome other content.\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if nodes := doc.Body().DeployedRegions(); len(nodes) != 0 {
		t.Errorf("unmatched </CommunicationProtocol> must not produce a deployed node; got %d node(s)", len(nodes))
	}
	// The unmatched close tag must be preserved verbatim as literal text (round-trip).
	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("unmatched close tag must round-trip as literal text:\n  want: %q\n  got:  %q", src, got)
	}
}

// ---------------------------------------------------------------------------
// Addressing and enumeration
// ---------------------------------------------------------------------------

func TestBody_Deployed_AbsentName_ReturnsFalse(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-deployed.md")

	_, ok := doc.Body().Deployed("NonExistent")

	if ok {
		t.Error("Deployed(\"NonExistent\") returned true for a name that does not appear in the document")
	}
}

func TestBody_Deployed_FindsNodeAtAnyNestingDepth(t *testing.T) {
	// ProtocolConstraints is nested inside <Constraints type="core">; Deployed() must find it
	// regardless of nesting depth.
	doc := parsedBoundaryFixture(t, "deployed-in-section.md")

	node, ok := doc.Body().Deployed("ProtocolConstraints")

	if !ok {
		t.Fatal("Deployed(\"ProtocolConstraints\") returned false for a deployed node nested inside a section")
	}
	if node.Name() != "ProtocolConstraints" {
		t.Errorf("Name: want %q, got %q", "ProtocolConstraints", node.Name())
	}
}

func TestBody_Deployed_NestedNode_ParentIsContainingSection(t *testing.T) {
	// A deployed node nested inside a section must have that section as its Parent.
	doc := parsedBoundaryFixture(t, "deployed-in-section.md")

	deployed, ok := doc.Body().Deployed("ProtocolConstraints")
	if !ok {
		t.Fatal("Deployed(\"ProtocolConstraints\") not found")
	}
	section, ok := doc.Body().Section("Constraints")
	if !ok {
		t.Fatal("Section(\"Constraints\") not found")
	}

	parent := deployed.Parent()
	if parent == nil {
		t.Fatal("nested deployed node must have a non-nil Parent()")
	}
	if parent != section {
		t.Error("nested deployed node Parent is not the enclosing section node")
	}
}

func TestBody_Deployed_TopLevelNode_HasNilParent(t *testing.T) {
	// A top-level deployed node (not nested in any section) must have nil Parent.
	doc := parsedBoundaryFixture(t, "empty-deployed.md")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if node.Parent() != nil {
		t.Error("top-level deployed node must have nil Parent()")
	}
}

func TestBody_DeployedRegions_ReturnsAllDeployedNodesInDocumentOrder(t *testing.T) {
	// mixed-markers.md has two deployed regions in document order:
	//   1. CommunicationProtocol (top level)
	//   2. ProtocolConstraints (nested inside <Constraints type="core">)
	doc := parsedBoundaryFixture(t, "mixed-markers.md")

	regions := doc.Body().DeployedRegions()

	if len(regions) != 2 {
		t.Fatalf("expected 2 deployed regions, got %d", len(regions))
	}
	if regions[0].Name() != "CommunicationProtocol" {
		t.Errorf("regions[0].Name: want %q, got %q", "CommunicationProtocol", regions[0].Name())
	}
	if regions[1].Name() != "ProtocolConstraints" {
		t.Errorf("regions[1].Name: want %q, got %q", "ProtocolConstraints", regions[1].Name())
	}
}

func TestBody_DeployedRegions_EmptyWhenNoDeployedNodes(t *testing.T) {
	// A document containing only SECTION and INJECTION markers must yield an empty slice
	// from DeployedRegions().
	doc := parsedBoundaryFixture(t, "multiple-sections.md")

	regions := doc.Body().DeployedRegions()

	if len(regions) != 0 {
		t.Errorf("expected 0 deployed regions for a document with only sections and injections, got %d", len(regions))
	}
}

// ---------------------------------------------------------------------------
// Disjointness: Injections() vs DeployedRegions()
// ---------------------------------------------------------------------------

func TestBody_Injections_DoesNotReturnDeployedNodes(t *testing.T) {
	// Body.Injections() must return only NodeInjection nodes. NodeDeployed nodes must never
	// appear in the injection enumeration, even when both kinds coexist in the document.
	doc := parsedBoundaryFixture(t, "mixed-markers.md")

	injections := doc.Body().Injections()

	for _, n := range injections {
		if n.Kind() != docformat.NodeInjection {
			t.Errorf("Injections() returned a node of kind %q; only NodeInjection expected", n.Kind())
		}
	}

	names := make(map[string]bool, len(injections))
	for _, n := range injections {
		names[n.Name()] = true
	}
	if names["CommunicationProtocol"] {
		t.Error("Injections() must not include the deployed node \"CommunicationProtocol\"")
	}
	if names["ProtocolConstraints"] {
		t.Error("Injections() must not include the deployed node \"ProtocolConstraints\"")
	}
}

func TestBody_DeployedRegions_DoesNotReturnInjectionNodes(t *testing.T) {
	// Body.DeployedRegions() must return only NodeDeployed nodes. NodeInjection nodes must
	// never appear in the deployed enumeration, even when both kinds coexist in the document.
	doc := parsedBoundaryFixture(t, "mixed-markers.md")

	deployedRegions := doc.Body().DeployedRegions()

	for _, n := range deployedRegions {
		if n.Kind() != docformat.NodeDeployed {
			t.Errorf("DeployedRegions() returned a node of kind %q; only NodeDeployed expected", n.Kind())
		}
	}

	names := make(map[string]bool, len(deployedRegions))
	for _, n := range deployedRegions {
		names[n.Name()] = true
	}
	if names["IdentityExtension"] {
		t.Error("DeployedRegions() must not include the injection node \"IdentityExtension\"")
	}
	if names["CodebaseContext"] {
		t.Error("DeployedRegions() must not include the injection node \"CodebaseContext\"")
	}
}

func TestBody_Regions_ReturnsInjectionAndDeployedNodesInterleavedInDocumentOrder(t *testing.T) {
	// Body.Regions() must enumerate every injection and every deployed region at any nesting
	// depth, interleaved in document order.
	//
	// mixed-markers.md layout (document order):
	//   <IdentityExtension type="project">   — inside <Identity type="core">
	//   <CommunicationProtocol type="managed"> — top level
	//   <ProtocolConstraints type="managed">   — inside <Constraints type="core">
	//   <CodebaseContext type="project">     — inside <Constraints type="core">
	doc := parsedBoundaryFixture(t, "mixed-markers.md")

	regions := doc.Body().Regions()

	if len(regions) != 4 {
		t.Fatalf("expected 4 regions (2 injections + 2 deployed), got %d", len(regions))
	}

	type want struct {
		name string
		kind docformat.NodeKind
	}
	expected := []want{
		{"IdentityExtension", docformat.NodeInjection},
		{"CommunicationProtocol", docformat.NodeDeployed},
		{"ProtocolConstraints", docformat.NodeDeployed},
		{"CodebaseContext", docformat.NodeInjection},
	}
	for i, e := range expected {
		if regions[i].Name() != e.name {
			t.Errorf("regions[%d].Name: want %q, got %q", i, e.name, regions[i].Name())
		}
		if regions[i].Kind() != e.kind {
			t.Errorf("regions[%d].Kind: want %q, got %q", i, e.kind, regions[i].Kind())
		}
	}
}

// ---------------------------------------------------------------------------
// Content mutation
// ---------------------------------------------------------------------------

func TestMutation_Deployed_Content_ExcludesBoundaryTagLines(t *testing.T) {
	// Node.Content must return the inner bytes between the boundary tags, not the tags themselves.
	doc := parsedBoundaryFixture(t, "filled-deployed.md")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	content := node.Content()

	if bytes.Contains(content, []byte(`<CommunicationProtocol type="managed">`)) {
		t.Error("Node.Content must not contain the opening boundary tag")
	}
	if bytes.Contains(content, []byte("</CommunicationProtocol>")) {
		t.Error("Node.Content must not contain the closing boundary tag")
	}
	if !bytes.Contains(content, []byte("Deployed content line one.")) {
		t.Error("Node.Content must contain the inner body text")
	}
}

func TestMutation_Deployed_SetContent_ContentAvailableViaContent(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-deployed.md")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	newContent := []byte("Generated protocol content.\n")
	if err := node.SetContent(newContent); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	got := node.Content()
	if !bytes.Equal(got, newContent) {
		t.Errorf("Content after SetContent: want %q, got %q", newContent, got)
	}
}

func TestMutation_Deployed_SetContent_IsEmptyFalseAfterNonBlankContent(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-deployed.md")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetContent([]byte("Generated content.\n")); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	if node.IsEmpty() {
		t.Error("IsEmpty must return false after SetContent with non-blank content")
	}
}

func TestMutation_Deployed_SetContent_BytesContainsBoundaryTagsAndNewContent(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-deployed.md")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.SetContent([]byte("New deployed content.\n")); err != nil {
		t.Fatalf("SetContent: %v", err)
	}

	b := node.Bytes()
	if !bytes.Contains(b, []byte(`<CommunicationProtocol type="managed">`)) {
		t.Error("Node.Bytes after SetContent must contain the opening boundary tag")
	}
	if !bytes.Contains(b, []byte("</CommunicationProtocol>")) {
		t.Error("Node.Bytes after SetContent must contain the closing boundary tag")
	}
	if !bytes.Contains(b, []byte("New deployed content.")) {
		t.Error("Node.Bytes after SetContent must contain the new content")
	}
}

func TestMutation_Deployed_Clear_IsEmptyTrue(t *testing.T) {
	doc := parsedBoundaryFixture(t, "filled-deployed.md")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}
	if node.IsEmpty() {
		t.Fatal("filled-deployed.md fixture has no content — cannot test Clear; update the fixture to include non-blank deployed content")
	}

	if err := node.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	if !node.IsEmpty() {
		t.Errorf("IsEmpty: want true after Clear, got false (content: %q)", node.Content())
	}
}

func TestMutation_Deployed_Clear_BoundaryTagsRemainInNodeBytes(t *testing.T) {
	doc := parsedBoundaryFixture(t, "filled-deployed.md")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	if err := node.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	b := node.Bytes()
	if !bytes.Contains(b, []byte(`<CommunicationProtocol type="managed">`)) {
		t.Error("Node.Bytes after Clear must still contain the opening boundary tag")
	}
	if !bytes.Contains(b, []byte("</CommunicationProtocol>")) {
		t.Error("Node.Bytes after Clear must still contain the closing boundary tag")
	}
}

func TestMutation_Deployed_Bytes_IncludesBoundaryTagLinesAndContent(t *testing.T) {
	doc := parsedBoundaryFixture(t, "filled-deployed.md")
	node, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found")
	}

	b := node.Bytes()

	if !bytes.Contains(b, []byte(`<CommunicationProtocol type="managed">`)) {
		t.Error("Node.Bytes must include the opening boundary tag")
	}
	if !bytes.Contains(b, []byte("</CommunicationProtocol>")) {
		t.Error("Node.Bytes must include the closing boundary tag")
	}
	if !bytes.Contains(b, []byte("Deployed content line one.")) {
		t.Error("Node.Bytes must include the inner content")
	}
}

// ---------------------------------------------------------------------------
// Round-trip: mixed-marker document
// ---------------------------------------------------------------------------

func TestRoundTrip_MixedMarkers_UnmutatedParseSerialise_ByteIdentical(t *testing.T) {
	// An unmutated parse/serialise cycle of a document mixing SECTION, INJECTION, and
	// DEPLOYED markers must reproduce the source bytes exactly.
	src := boundaryFixtureBytes(t, "mixed-markers.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse mixed-markers.md: %v", err)
	}

	// Trigger body parsing by enumerating the managed regions.
	_ = doc.Body().Regions()

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("mixed-marker document round-trip produced different bytes (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}
