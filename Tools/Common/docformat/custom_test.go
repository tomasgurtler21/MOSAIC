package docformat_test

// Tests for the CUSTOM boundary marker: lexing, body accessors, and round-trip fidelity.
//
// All tests in this file target behaviour that is not yet implemented. They are written
// in TDD RED phase: the parser does not yet recognise [[CUSTOM:]] tags and the body
// accessor stubs return nil / false. Every test is expected to fail until the matching
// implementation tasks are complete.
//
// Coverage (Parser):
//   - [[CUSTOM:Name]] open tag is recognised; the resulting node has Kind NodeCustom.
//   - [[/CUSTOM:Name]] close tag correctly closes the custom node.
//   - A custom region at body top level has nil Parent.
//   - A custom region nested inside a [[SECTION:]] has that section as its Parent.
//   - A custom region nested inside a [[DEPLOYED:]] region has the deployed node as Parent.
//   - Names containing colons (compound names such as "Workflow:quick-fix") are accepted.
//   - [[CUSTOM:]] with an empty name is not recognised as a boundary tag.
//
// Coverage (Body accessors):
//   - Body.CustomRegions() returns all [[CUSTOM:]] regions at any depth in document order.
//   - Body.CustomRegions() returns nil or empty for a document with no custom regions.
//   - Body.Custom(name) returns the first matching custom region at any depth.
//   - Body.Custom(name) returns false for an absent name.
//   - Body.Injections() does NOT include custom nodes.
//   - Body.UserRegions() returns injections and custom regions interleaved in document order,
//     excluding deployed nodes.
//   - Body.Regions() returns injections, deployed, and custom nodes interleaved in document order.
//
// Coverage (Round-trip):
//   - A document containing [[CUSTOM:]] regions at top level round-trips byte-identically
//     after the body is parsed via CustomRegions().
//   - A document containing [[CUSTOM:]] regions with non-blank content round-trips
//     byte-identically.
//   - A document containing all four marker kinds round-trips byte-identically.
//   - The three-marker document (SECTION, INJECTION, DEPLOYED) round-trips byte-identically
//     after the lexer change — the existing markers must not regress.

import (
	"bytes"
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// Parser: open and close forms
// ---------------------------------------------------------------------------

func TestBoundaryLexer_Custom_OpenTag_RecognisedAsCustomNode(t *testing.T) {
	// [[CUSTOM:CustomConstraints]] must be recognised as a boundary open tag and produce
	// a custom node retrievable via Body.Custom.
	doc := parsedBoundaryFixture(t, "empty-custom.md")

	node, ok := doc.Body().Custom("CustomConstraints")

	if !ok {
		t.Fatal("Custom(\"CustomConstraints\") returned false for a document that contains [[CUSTOM:CustomConstraints]]")
	}
	if node == nil {
		t.Fatal("Custom(\"CustomConstraints\") returned nil node with ok == true")
	}
}

func TestBoundaryLexer_Custom_NodeKind_IsCustom(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-custom.md")
	node, ok := doc.Body().Custom("CustomConstraints")
	if !ok {
		t.Fatal("Custom(\"CustomConstraints\") not found")
	}

	if node.Kind() != docformat.NodeCustom {
		t.Errorf("Kind: want NodeCustom, got %q", node.Kind())
	}
}

func TestBoundaryLexer_Custom_NodeName_MatchesTagName(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-custom.md")
	node, ok := doc.Body().Custom("CustomConstraints")
	if !ok {
		t.Fatal("Custom(\"CustomConstraints\") not found")
	}

	if node.Name() != "CustomConstraints" {
		t.Errorf("Name: want %q, got %q", "CustomConstraints", node.Name())
	}
}

func TestBoundaryLexer_Custom_CloseTag_CorrectlyClosesNode(t *testing.T) {
	// A custom node with content between its open and close tags must expose that content
	// via Content(). This verifies the close tag is recognised and terminates the node.
	doc := parsedBoundaryFixture(t, "filled-custom.md")
	node, ok := doc.Body().Custom("CustomConstraints")
	if !ok {
		t.Fatal("Custom(\"CustomConstraints\") not found in filled-custom.md")
	}

	content := node.Content()
	if len(bytes.TrimSpace(content)) == 0 {
		t.Error("custom node content is empty; the close tag may not have been recognised, leaving the node unclosed and swallowing all content")
	}
}

// ---------------------------------------------------------------------------
// Parser: nesting positions
// ---------------------------------------------------------------------------

func TestBoundaryLexer_Custom_AtBodyTopLevel_HasNilParent(t *testing.T) {
	// A custom region that appears at the top level of the body (not inside any section
	// or deployed region) must have a nil Parent.
	doc := parsedBoundaryFixture(t, "empty-custom.md")
	node, ok := doc.Body().Custom("CustomConstraints")
	if !ok {
		t.Fatal("Custom(\"CustomConstraints\") not found")
	}

	if node.Parent() != nil {
		t.Error("top-level custom node must have nil Parent()")
	}
}

func TestBoundaryLexer_Custom_NestedInsideSection_ParentIsSection(t *testing.T) {
	// custom-in-section.md has [[CUSTOM:CustomConstraints]] directly inside
	// [[SECTION:Constraints]]. The custom node's Parent must be the section node.
	doc := parsedBoundaryFixture(t, "custom-in-section.md")
	custom, ok := doc.Body().Custom("CustomConstraints")
	if !ok {
		t.Fatal("Custom(\"CustomConstraints\") not found in custom-in-section.md")
	}
	section, ok := doc.Body().Section("Constraints")
	if !ok {
		t.Fatal("Section(\"Constraints\") not found in custom-in-section.md")
	}

	parent := custom.Parent()
	if parent == nil {
		t.Fatal("custom node nested inside a section must have a non-nil Parent()")
	}
	if parent != section {
		t.Errorf("custom node Parent is not the enclosing section node; got %q", parent.Name())
	}
}

func TestBoundaryLexer_Custom_NestedInsideDeployed_ParentIsDeployedNode(t *testing.T) {
	// custom-in-deployed.md has [[CUSTOM:CustomConstraints]] directly inside
	// [[DEPLOYED:CommunicationProtocol]]. The custom node's Parent must be the deployed node.
	doc := parsedBoundaryFixture(t, "custom-in-deployed.md")
	custom, ok := doc.Body().Custom("CustomConstraints")
	if !ok {
		t.Fatal("Custom(\"CustomConstraints\") not found in custom-in-deployed.md")
	}
	deployed, ok := doc.Body().Deployed("CommunicationProtocol")
	if !ok {
		t.Fatal("Deployed(\"CommunicationProtocol\") not found in custom-in-deployed.md")
	}

	parent := custom.Parent()
	if parent == nil {
		t.Fatal("custom node nested inside a deployed region must have a non-nil Parent()")
	}
	if parent != deployed {
		t.Errorf("custom node Parent is not the enclosing deployed node; got %q", parent.Name())
	}
}

// ---------------------------------------------------------------------------
// Parser: name variants
// ---------------------------------------------------------------------------

func TestBoundaryLexer_Custom_CompoundName_WithColons_Accepted(t *testing.T) {
	// Names containing colons (e.g. "Workflow:quick-fix") must be treated as a single
	// opaque name, consistent with SECTION, INJECTION, and DEPLOYED markers.
	doc := parsedBoundaryFixture(t, "custom-compound-name.md")

	node, ok := doc.Body().Custom("Workflow:quick-fix")

	if !ok {
		t.Fatal("Custom(\"Workflow:quick-fix\") returned false; CUSTOM names containing colons must be accepted")
	}
	if node.Name() != "Workflow:quick-fix" {
		t.Errorf("Name: want %q, got %q", "Workflow:quick-fix", node.Name())
	}
}

// ---------------------------------------------------------------------------
// Parser: rejected / near-miss forms
// ---------------------------------------------------------------------------

func TestBoundaryLexer_Custom_EmptyName_NotRecognisedAsBoundaryTag(t *testing.T) {
	// [[CUSTOM:]] — empty name — must be treated as literal text, not as a boundary tag.
	// Consistent with the same behaviour for SECTION, INJECTION, and DEPLOYED.
	src := []byte("[[CUSTOM:]]\n")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if nodes := doc.Body().CustomRegions(); len(nodes) != 0 {
		t.Errorf("[[CUSTOM:]] with empty name must not produce a custom node; got %d node(s)", len(nodes))
	}
}

// ---------------------------------------------------------------------------
// Body accessors: CustomRegions enumeration
// ---------------------------------------------------------------------------

func TestBody_CustomRegions_ReturnsAllCustomNodesAtAnyDepth_InDocumentOrder(t *testing.T) {
	// mixed-markers-with-custom.md has two [[CUSTOM:]] regions in document order:
	//   1. HarnessCustom — nested inside [[DEPLOYED:CommunicationProtocol]]
	//   2. ProjectNotes  — at body top level
	doc := parsedBoundaryFixture(t, "mixed-markers-with-custom.md")

	customs := doc.Body().CustomRegions()

	if len(customs) != 2 {
		t.Fatalf("expected 2 custom regions, got %d", len(customs))
	}
	if customs[0].Name() != "HarnessCustom" {
		t.Errorf("customs[0].Name: want %q, got %q", "HarnessCustom", customs[0].Name())
	}
	if customs[1].Name() != "ProjectNotes" {
		t.Errorf("customs[1].Name: want %q, got %q", "ProjectNotes", customs[1].Name())
	}
}

func TestBody_CustomRegions_EmptyWhenNoCustomNodes(t *testing.T) {
	// A document containing only SECTION, INJECTION, and DEPLOYED markers must yield
	// an empty (or nil) slice from CustomRegions().
	doc := parsedBoundaryFixture(t, "mixed-markers.md")

	customs := doc.Body().CustomRegions()

	if len(customs) != 0 {
		t.Errorf("expected 0 custom regions for a document with no [[CUSTOM:]] markers, got %d", len(customs))
	}
}

// ---------------------------------------------------------------------------
// Body accessors: Custom(name) name lookup
// ---------------------------------------------------------------------------

func TestBody_Custom_AbsentName_ReturnsFalse(t *testing.T) {
	doc := parsedBoundaryFixture(t, "empty-custom.md")

	_, ok := doc.Body().Custom("NonExistent")

	if ok {
		t.Error("Custom(\"NonExistent\") returned true for a name that does not appear in the document")
	}
}

func TestBody_Custom_FindsAtAnyNestingDepth(t *testing.T) {
	// HarnessCustom is nested inside [[DEPLOYED:CommunicationProtocol]];
	// Custom() must find it regardless of nesting depth.
	doc := parsedBoundaryFixture(t, "mixed-markers-with-custom.md")

	node, ok := doc.Body().Custom("HarnessCustom")

	if !ok {
		t.Fatal("Custom(\"HarnessCustom\") returned false for a node nested inside a deployed region")
	}
	if node.Name() != "HarnessCustom" {
		t.Errorf("Name: want %q, got %q", "HarnessCustom", node.Name())
	}
}

// ---------------------------------------------------------------------------
// Body accessors: disjointness — Injections() must not include custom nodes
// ---------------------------------------------------------------------------

func TestBody_Injections_DoesNotReturnCustomNodes(t *testing.T) {
	// Body.Injections() must return only NodeInjection nodes. NodeCustom nodes must
	// never appear in the injection enumeration, even when both kinds coexist in the document.
	doc := parsedBoundaryFixture(t, "mixed-markers-with-custom.md")

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
	if names["HarnessCustom"] {
		t.Error("Injections() must not include the custom node \"HarnessCustom\"")
	}
	if names["ProjectNotes"] {
		t.Error("Injections() must not include the custom node \"ProjectNotes\"")
	}
}

// ---------------------------------------------------------------------------
// Body accessors: UserRegions (injections + custom, no deployed)
// ---------------------------------------------------------------------------

func TestBody_UserRegions_ReturnsInjectionsAndCustomInterleavedInDocumentOrder(t *testing.T) {
	// mixed-markers-with-custom.md layout (document order of user-owned regions):
	//   [[INJECTION:IdentityExtension]]  — inside [[SECTION:Identity]]
	//   [[CUSTOM:HarnessCustom]]         — inside [[DEPLOYED:CommunicationProtocol]]
	//   [[CUSTOM:ProjectNotes]]          — top level
	//
	// UserRegions must include both injections and custom regions, interleaved, and must
	// NOT include NodeDeployed nodes.
	doc := parsedBoundaryFixture(t, "mixed-markers-with-custom.md")

	userRegions := doc.Body().UserRegions()

	if len(userRegions) != 3 {
		t.Fatalf("expected 3 user-owned regions (1 injection + 2 custom), got %d", len(userRegions))
	}

	type want struct {
		name string
		kind docformat.NodeKind
	}
	expected := []want{
		{"IdentityExtension", docformat.NodeInjection},
		{"HarnessCustom", docformat.NodeCustom},
		{"ProjectNotes", docformat.NodeCustom},
	}
	for i, e := range expected {
		if userRegions[i].Name() != e.name {
			t.Errorf("userRegions[%d].Name: want %q, got %q", i, e.name, userRegions[i].Name())
		}
		if userRegions[i].Kind() != e.kind {
			t.Errorf("userRegions[%d].Kind: want %q, got %q", i, e.kind, userRegions[i].Kind())
		}
	}
}

func TestBody_UserRegions_DoesNotReturnDeployedNodes(t *testing.T) {
	// UserRegions must never include NodeDeployed nodes; those are tool-managed content.
	doc := parsedBoundaryFixture(t, "mixed-markers-with-custom.md")

	userRegions := doc.Body().UserRegions()

	for _, n := range userRegions {
		if n.Kind() == docformat.NodeDeployed {
			t.Errorf("UserRegions() returned NodeDeployed node %q; only injections and custom regions expected", n.Name())
		}
	}
}

// ---------------------------------------------------------------------------
// Body accessors: Regions() now includes custom nodes
// ---------------------------------------------------------------------------

func TestBody_Regions_IncludesCustomNodesInterleavedInDocumentOrder(t *testing.T) {
	// mixed-markers-with-custom.md layout (document order of all non-section nodes):
	//   [[INJECTION:IdentityExtension]]      — inside [[SECTION:Identity]]
	//   [[DEPLOYED:CommunicationProtocol]]   — top level
	//   [[CUSTOM:HarnessCustom]]             — inside [[DEPLOYED:CommunicationProtocol]]
	//   [[CUSTOM:ProjectNotes]]              — top level
	//
	// Regions() must interleave all three region kinds (injection, deployed, custom) in
	// document order, with a parent visited before its children.
	doc := parsedBoundaryFixture(t, "mixed-markers-with-custom.md")

	regions := doc.Body().Regions()

	if len(regions) != 4 {
		t.Fatalf("expected 4 regions (1 injection + 1 deployed + 2 custom), got %d", len(regions))
	}

	type want struct {
		name string
		kind docformat.NodeKind
	}
	expected := []want{
		{"IdentityExtension", docformat.NodeInjection},
		{"CommunicationProtocol", docformat.NodeDeployed},
		{"HarnessCustom", docformat.NodeCustom},
		{"ProjectNotes", docformat.NodeCustom},
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
// Round-trip: byte-exact serialisation for documents with custom regions
// ---------------------------------------------------------------------------

func TestRoundTrip_Custom_EmptyCustomRegion_ByteIdentical(t *testing.T) {
	// Parsing a document that contains an empty [[CUSTOM:]] region and then serialising it
	// must reproduce the source bytes exactly. The accessor call below forces body parsing;
	// if CustomRegions() returns the wrong count the test stops before the byte check to
	// make the failure mode unambiguous.
	src := boundaryFixtureBytes(t, "empty-custom.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse empty-custom.md: %v", err)
	}

	customs := doc.Body().CustomRegions()
	if len(customs) != 1 {
		t.Fatalf("expected 1 custom region after parsing empty-custom.md, got %d — body parse must recognise [[CUSTOM:]] markers before round-trip is meaningful", len(customs))
	}

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip of empty-custom.md produced different bytes (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

func TestRoundTrip_Custom_FilledCustomRegion_ByteIdentical(t *testing.T) {
	// A custom region with non-blank content must round-trip byte-identically.
	src := boundaryFixtureBytes(t, "filled-custom.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse filled-custom.md: %v", err)
	}

	customs := doc.Body().CustomRegions()
	if len(customs) != 1 {
		t.Fatalf("expected 1 custom region, got %d", len(customs))
	}

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip of filled-custom.md produced different bytes (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

func TestRoundTrip_Custom_CustomNestedInSection_ByteIdentical(t *testing.T) {
	src := boundaryFixtureBytes(t, "custom-in-section.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse custom-in-section.md: %v", err)
	}

	customs := doc.Body().CustomRegions()
	if len(customs) != 1 {
		t.Fatalf("expected 1 custom region, got %d", len(customs))
	}

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip of custom-in-section.md produced different bytes (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

func TestRoundTrip_Custom_CustomNestedInDeployed_ByteIdentical(t *testing.T) {
	src := boundaryFixtureBytes(t, "custom-in-deployed.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse custom-in-deployed.md: %v", err)
	}

	customs := doc.Body().CustomRegions()
	if len(customs) != 1 {
		t.Fatalf("expected 1 custom region, got %d", len(customs))
	}

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip of custom-in-deployed.md produced different bytes (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

func TestRoundTrip_Custom_AllFourMarkerKinds_ByteIdentical(t *testing.T) {
	// mixed-markers-with-custom.md contains SECTION, INJECTION, DEPLOYED, and CUSTOM markers.
	// All four must survive a parse/serialise cycle byte-identically.
	src := boundaryFixtureBytes(t, "mixed-markers-with-custom.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse mixed-markers-with-custom.md: %v", err)
	}

	customs := doc.Body().CustomRegions()
	if len(customs) != 2 {
		t.Fatalf("expected 2 custom regions in mixed-markers-with-custom.md, got %d", len(customs))
	}

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("round-trip of mixed-markers-with-custom.md produced different bytes (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: regression — three-marker documents must not be affected
// ---------------------------------------------------------------------------

func TestRoundTrip_ThreeMarkerDocument_NoRegressionFromLexerChange(t *testing.T) {
	// mixed-markers.md contains only SECTION, INJECTION, and DEPLOYED markers. Touching
	// the shared lexer switch to add CUSTOM support must not cause any regression in
	// parsing or serialisation of documents that use none of the new marker.
	src := boundaryFixtureBytes(t, "mixed-markers.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse mixed-markers.md: %v", err)
	}

	// Access every region kind, including the new custom accessor, to ensure none of them
	// disturbs the serialised output.
	_ = doc.Body().Regions()
	_ = doc.Body().Injections()
	_ = doc.Body().DeployedRegions()
	_ = doc.Body().CustomRegions() // must return empty without error

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("three-marker document round-trip produced different bytes after lexer change (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

func TestRoundTrip_InjectionOnlyDocument_NoRegressionFromLexerChange(t *testing.T) {
	// A document containing only SECTION and INJECTION markers must round-trip
	// byte-identically after the lexer change.
	src := boundaryFixtureBytes(t, "filled-injection.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse filled-injection.md: %v", err)
	}

	_ = doc.Body().Injections()
	_ = doc.Body().CustomRegions()

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("injection-only document round-trip produced different bytes after lexer change (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}

func TestRoundTrip_DeployedOnlyDocument_NoRegressionFromLexerChange(t *testing.T) {
	// A document containing only a DEPLOYED marker must round-trip byte-identically
	// after the lexer change.
	src := boundaryFixtureBytes(t, "filled-deployed.md")
	doc, err := docformat.Parse(src)
	if err != nil {
		t.Fatalf("Parse filled-deployed.md: %v", err)
	}

	_ = doc.Body().DeployedRegions()
	_ = doc.Body().CustomRegions()

	got := doc.Bytes()
	if !bytes.Equal(src, got) {
		t.Errorf("deployed-only document round-trip produced different bytes after lexer change (original=%d bytes, got=%d bytes)", len(src), len(got))
		reportFirstDifference(t, src, got)
	}
}
