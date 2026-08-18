package docformat_test

// vocabulary_stage4_test.go covers the Stage 4 vocabulary additions and amendments:
//
// Coverage (CanonicalManagedBlocks — separate registry for nested managed blocks):
//   - CanonicalManagedBlocks contains exactly one entry.
//   - CanonicalManagedBlocks contains "Workflow".
//   - CanonicalManagedBlocks is disjoint from CanonicalDeployed.
//   - CanonicalDeployed still contains exactly 9 names (registry blast radius is zero).
//
// Coverage (ManagedBlockParent — parent requirement map for managed blocks):
//   - ManagedBlockParent is not nil.
//   - ManagedBlockParent maps "Workflow" to "AvailableWorkflows".
//
// Coverage (IsManagedBlockName):
//   - Bare form "Workflow" reports true.
//   - Compound form "Workflow:brownfield-tdd" reports true.
//   - Compound form "Workflow:quick-fix" reports true.
//   - A name in CanonicalDeployed (e.g. "AvailableWorkflows") reports false — the two
//     registries are disjoint by design.
//   - An unrecognised name ("SomeUnknown") reports false.
//   - Empty string reports false.
//   - Leading-colon form (":x") reports false — prefix is empty string.
//
// Coverage (RetypeOpenTagLine):
//   - An opening tag with type="core" is retyped to type="managed" (NodeDeployed).
//   - The name and version attributes are preserved byte-for-byte.
//   - The block body lines after the first are NOT affected (only line is in scope).
//   - Attribute order is preserved when type is not the first attribute.
//   - Single-quote quoting style is preserved.
//   - A CRLF line terminator is preserved.
//   - An LF line terminator is preserved.
//   - A line without a terminator is handled without error.
//   - A closing tag returns (nil, false).
//   - A plain-text line returns (nil, false).
//   - A tag with no type attribute returns (nil, false).
//
// Coverage (ExpectedMarker — amended for managed block names):
//   - "Workflow" (bare managed block name) returns (NodeDeployed, true).
//   - "Workflow:brownfield-tdd" (compound managed block name) returns (NodeDeployed, true).
//   - "Workflow:quick-fix" (compound managed block name) returns (NodeDeployed, true).
//   - Existing CanonicalDeployed names are unaffected (regression guard).
//
// Coverage (ClassifyRegion — amended for managed block names):
//   - NodeDeployed + "Workflow" returns (InjectionWorkflow, nil).
//   - NodeDeployed + "Workflow:brownfield-tdd" returns (InjectionWorkflow, nil).
//   - NodeDeployed + "Workflow:quick-fix" returns (InjectionWorkflow, nil).
//   - NodeInjection + "Workflow" returns (InjectionProject, nil) — injection names are open.
//   - NodeInjection + "Workflow:brownfield-tdd" returns (InjectionProject, nil).
//   - Existing NodeDeployed + CanonicalDeployed behaviours are unaffected (regression guard).

import (
	"errors"
	"testing"

	"mosaic-common/docformat"
	domain "mosaic-common/mosaic"
)

// ---------------------------------------------------------------------------
// CanonicalManagedBlocks registry
// ---------------------------------------------------------------------------

func TestCanonicalManagedBlocks_ContainsExactlyOneEntry(t *testing.T) {
	got := docformat.CanonicalManagedBlocks
	if len(got) != 1 {
		t.Fatalf("CanonicalManagedBlocks length: want 1, got %d: %v", len(got), got)
	}
}

func TestCanonicalManagedBlocks_ContainsWorkflow(t *testing.T) {
	for _, n := range docformat.CanonicalManagedBlocks {
		if n == "Workflow" {
			return
		}
	}
	t.Errorf("CanonicalManagedBlocks does not contain %q; got: %v", "Workflow", docformat.CanonicalManagedBlocks)
}

func TestCanonicalManagedBlocks_DisjointFromCanonicalDeployed(t *testing.T) {
	deployed := make(map[string]bool, len(docformat.CanonicalDeployed))
	for _, n := range docformat.CanonicalDeployed {
		deployed[n] = true
	}
	for _, n := range docformat.CanonicalManagedBlocks {
		if deployed[n] {
			t.Errorf("name %q appears in both CanonicalManagedBlocks and CanonicalDeployed — the registries must be disjoint", n)
		}
	}
}

func TestCanonicalDeployed_Stage4_StillContainsNineNames(t *testing.T) {
	// Adding CanonicalManagedBlocks must not change CanonicalDeployed. The closed nine-name
	// set must remain exactly nine so that RefreshScope.Regions(), bundle-target validation,
	// and generator sweep tests remain unaffected.
	got := docformat.CanonicalDeployed
	if len(got) != 9 {
		t.Fatalf("CanonicalDeployed length: want 9 (unchanged by Stage 4), got %d: %v", len(got), got)
	}
}

// ---------------------------------------------------------------------------
// ManagedBlockParent map
// ---------------------------------------------------------------------------

func TestManagedBlockParent_IsNotNil(t *testing.T) {
	if docformat.ManagedBlockParent == nil {
		t.Fatal("ManagedBlockParent is nil — must be populated before first use")
	}
}

func TestManagedBlockParent_Workflow_MapsToAvailableWorkflows(t *testing.T) {
	got := docformat.ManagedBlockParent
	if got == nil {
		t.Fatal("ManagedBlockParent is nil")
	}
	parent, ok := got["Workflow"]
	if !ok {
		t.Fatalf("ManagedBlockParent has no entry for %q", "Workflow")
	}
	if parent != "AvailableWorkflows" {
		t.Errorf("ManagedBlockParent[%q]: want %q, got %q", "Workflow", "AvailableWorkflows", parent)
	}
}

// ---------------------------------------------------------------------------
// IsManagedBlockName
// ---------------------------------------------------------------------------

func TestIsManagedBlockName_BareWorkflow_ReturnsTrue(t *testing.T) {
	if !docformat.IsManagedBlockName("Workflow") {
		t.Error("IsManagedBlockName(\"Workflow\"): want true, got false")
	}
}

func TestIsManagedBlockName_CompoundWorkflow_BrownfieldTDD_ReturnsTrue(t *testing.T) {
	if !docformat.IsManagedBlockName("Workflow:brownfield-tdd") {
		t.Error("IsManagedBlockName(\"Workflow:brownfield-tdd\"): want true, got false")
	}
}

func TestIsManagedBlockName_CompoundWorkflow_QuickFix_ReturnsTrue(t *testing.T) {
	if !docformat.IsManagedBlockName("Workflow:quick-fix") {
		t.Error("IsManagedBlockName(\"Workflow:quick-fix\"): want true, got false")
	}
}

func TestIsManagedBlockName_CanonicalDeployedName_ReturnsFalse(t *testing.T) {
	// CanonicalDeployed and CanonicalManagedBlocks must be disjoint. No member of
	// CanonicalDeployed should report true through IsManagedBlockName.
	for _, name := range docformat.CanonicalDeployed {
		if docformat.IsManagedBlockName(name) {
			t.Errorf("IsManagedBlockName(%q): want false — CanonicalDeployed members must not be managed block names", name)
		}
	}
}

func TestIsManagedBlockName_AvailableWorkflows_ReturnsFalse(t *testing.T) {
	// AvailableWorkflows is the parent managed region, not a nested block name.
	if docformat.IsManagedBlockName("AvailableWorkflows") {
		t.Error("IsManagedBlockName(\"AvailableWorkflows\"): want false — it is a CanonicalDeployed top-level region, not a nested block")
	}
}

func TestIsManagedBlockName_UnrecognisedName_ReturnsFalse(t *testing.T) {
	if docformat.IsManagedBlockName("SomeCompletelyUnrecognisedName") {
		t.Error("IsManagedBlockName(\"SomeCompletelyUnrecognisedName\"): want false")
	}
}

func TestIsManagedBlockName_EmptyString_ReturnsFalse(t *testing.T) {
	if docformat.IsManagedBlockName("") {
		t.Error("IsManagedBlockName(\"\"): want false")
	}
}

func TestIsManagedBlockName_LeadingColon_ReturnsFalse(t *testing.T) {
	// ":x" has an empty prefix — the empty string is not in CanonicalManagedBlocks.
	if docformat.IsManagedBlockName(":x") {
		t.Error("IsManagedBlockName(\":x\"): want false — leading-colon name has empty prefix")
	}
}

// ---------------------------------------------------------------------------
// RetypeOpenTagLine
// ---------------------------------------------------------------------------

func TestRetypeOpenTagLine_CoreToManaged_LF(t *testing.T) {
	line := []byte("<Workflow type=\"core\" name=\"brownfield-tdd\" version=\"3.7\">\n")
	want := []byte("<Workflow type=\"managed\" name=\"brownfield-tdd\" version=\"3.7\">\n")
	got, ok := docformat.RetypeOpenTagLine(line, docformat.NodeDeployed)
	if !ok {
		t.Fatal("RetypeOpenTagLine returned ok=false; want ok=true for a valid opening tag")
	}
	if string(got) != string(want) {
		t.Errorf("RetypeOpenTagLine output mismatch:\n want: %q\n  got: %q", want, got)
	}
}

func TestRetypeOpenTagLine_AttributeOrderPreserved_TypeNotFirst(t *testing.T) {
	// When type is not the first attribute, attribute order must be preserved.
	line := []byte("<Workflow name='x' type='core'>\r\n")
	want := []byte("<Workflow name='x' type='managed'>\r\n")
	got, ok := docformat.RetypeOpenTagLine(line, docformat.NodeDeployed)
	if !ok {
		t.Fatal("RetypeOpenTagLine returned ok=false for valid opening tag with type not first")
	}
	if string(got) != string(want) {
		t.Errorf("RetypeOpenTagLine: attribute order or quoting not preserved:\n want: %q\n  got: %q", want, got)
	}
}

func TestRetypeOpenTagLine_SingleQuoteStyle_Preserved(t *testing.T) {
	// Single-quote quoting style must be preserved — not normalised to double-quotes.
	line := []byte("<Workflow name='x' type='core'>\n")
	got, ok := docformat.RetypeOpenTagLine(line, docformat.NodeDeployed)
	if !ok {
		t.Fatal("RetypeOpenTagLine returned ok=false for valid opening tag with single quotes")
	}
	// The output must still use single quotes for the type value.
	if len(got) == 0 || got[0] == 0 {
		t.Fatal("RetypeOpenTagLine returned empty output")
	}
	// Verify single-quote style is preserved: type='managed' not type="managed"
	want := []byte("<Workflow name='x' type='managed'>\n")
	if string(got) != string(want) {
		t.Errorf("RetypeOpenTagLine: single-quote style not preserved:\n want: %q\n  got: %q", want, got)
	}
}

func TestRetypeOpenTagLine_CRLFTerminator_Preserved(t *testing.T) {
	line := []byte("<Workflow type=\"core\" name=\"quick-fix\" version=\"3.0\">\r\n")
	got, ok := docformat.RetypeOpenTagLine(line, docformat.NodeDeployed)
	if !ok {
		t.Fatal("RetypeOpenTagLine returned ok=false for CRLF-terminated tag")
	}
	if len(got) < 2 || got[len(got)-2] != '\r' || got[len(got)-1] != '\n' {
		t.Errorf("RetypeOpenTagLine: CRLF terminator not preserved; got: %q", got)
	}
}

func TestRetypeOpenTagLine_LFTerminator_Preserved(t *testing.T) {
	line := []byte("<Workflow type=\"core\" name=\"quick-fix\" version=\"3.0\">\n")
	got, ok := docformat.RetypeOpenTagLine(line, docformat.NodeDeployed)
	if !ok {
		t.Fatal("RetypeOpenTagLine returned ok=false for LF-terminated tag")
	}
	if len(got) == 0 || got[len(got)-1] != '\n' {
		t.Errorf("RetypeOpenTagLine: LF terminator not preserved; got: %q", got)
	}
	// Must NOT be CRLF.
	if len(got) >= 2 && got[len(got)-2] == '\r' {
		t.Errorf("RetypeOpenTagLine: LF-only input got CRLF output; got: %q", got)
	}
}

func TestRetypeOpenTagLine_NoTerminator_OpeningTag_ReturnedWithoutTerminator(t *testing.T) {
	// A line with no trailing newline or carriage return must be handled without error.
	// The implementation contract states that the line terminator may be LF, CRLF, or absent.
	line := []byte("<Workflow type=\"core\" name=\"x\" version=\"1.0\">")
	got, ok := docformat.RetypeOpenTagLine(line, docformat.NodeDeployed)
	if !ok {
		t.Fatal("RetypeOpenTagLine returned ok=false for a valid opening tag with no line terminator; want ok=true")
	}
	if got == nil {
		t.Fatal("RetypeOpenTagLine returned nil bytes for a valid opening tag with no line terminator")
	}
	for _, b := range got {
		if b == '\n' || b == '\r' {
			t.Errorf("RetypeOpenTagLine added a line terminator to input that had none; got: %q", got)
			break
		}
	}
}

func TestRetypeOpenTagLine_ClosingTag_ReturnsNilFalse(t *testing.T) {
	line := []byte("</Workflow>\n")
	got, ok := docformat.RetypeOpenTagLine(line, docformat.NodeDeployed)
	if ok {
		t.Error("RetypeOpenTagLine returned ok=true for a closing tag; want ok=false")
	}
	if got != nil {
		t.Errorf("RetypeOpenTagLine returned non-nil bytes for a closing tag; got: %q", got)
	}
}

func TestRetypeOpenTagLine_PlainTextLine_ReturnsNilFalse(t *testing.T) {
	line := []byte("Some prose line that is not a tag.\n")
	got, ok := docformat.RetypeOpenTagLine(line, docformat.NodeDeployed)
	if ok {
		t.Error("RetypeOpenTagLine returned ok=true for a plain text line; want ok=false")
	}
	if got != nil {
		t.Errorf("RetypeOpenTagLine returned non-nil bytes for a plain text line; got: %q", got)
	}
}

func TestRetypeOpenTagLine_TagWithNoTypeAttribute_ReturnsNilFalse(t *testing.T) {
	// A boundary-shaped tag with no type attribute cannot be retyped.
	line := []byte("<Workflow name=\"quick-fix\">\n")
	got, ok := docformat.RetypeOpenTagLine(line, docformat.NodeDeployed)
	if ok {
		t.Error("RetypeOpenTagLine returned ok=true for a tag with no type attribute; want ok=false")
	}
	if got != nil {
		t.Errorf("RetypeOpenTagLine returned non-nil bytes for a tag with no type attribute; got: %q", got)
	}
}

func TestRetypeOpenTagLine_VersionAttributePreserved(t *testing.T) {
	line := []byte("<Workflow type=\"core\" name=\"greenfield-tdd\" version=\"3.3\">\n")
	got, ok := docformat.RetypeOpenTagLine(line, docformat.NodeDeployed)
	if !ok {
		t.Fatal("RetypeOpenTagLine returned ok=false for valid tag")
	}
	// version attribute value must be preserved
	if string(got) != "<Workflow type=\"managed\" name=\"greenfield-tdd\" version=\"3.3\">\n" {
		t.Errorf("RetypeOpenTagLine: version not preserved; got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// ExpectedMarker — amended for managed block names
// ---------------------------------------------------------------------------

func TestExpectedMarker_BareWorkflow_ReturnsNodeDeployedAndKnownTrue(t *testing.T) {
	// After Stage 4, bare "Workflow" is a recognised managed block name.
	kind, known := docformat.ExpectedMarker("Workflow")
	if !known {
		t.Error("ExpectedMarker(\"Workflow\"): known=false, want true — bare managed block names must be recognised")
	}
	if kind != docformat.NodeDeployed {
		t.Errorf("ExpectedMarker(\"Workflow\"): kind=%q, want NodeDeployed", kind)
	}
}

func TestExpectedMarker_CompoundWorkflow_BrownfieldTDD_ReturnsNodeDeployedAndKnownTrue(t *testing.T) {
	// Compound managed block names must also be recognised.
	kind, known := docformat.ExpectedMarker("Workflow:brownfield-tdd")
	if !known {
		t.Error("ExpectedMarker(\"Workflow:brownfield-tdd\"): known=false, want true")
	}
	if kind != docformat.NodeDeployed {
		t.Errorf("ExpectedMarker(\"Workflow:brownfield-tdd\"): kind=%q, want NodeDeployed", kind)
	}
}

func TestExpectedMarker_CompoundWorkflow_QuickFix_ReturnsNodeDeployedAndKnownTrue(t *testing.T) {
	kind, known := docformat.ExpectedMarker("Workflow:quick-fix")
	if !known {
		t.Error("ExpectedMarker(\"Workflow:quick-fix\"): known=false, want true")
	}
	if kind != docformat.NodeDeployed {
		t.Errorf("ExpectedMarker(\"Workflow:quick-fix\"): kind=%q, want NodeDeployed", kind)
	}
}

func TestExpectedMarker_Stage4_AllCanonicalDeployed_Unaffected(t *testing.T) {
	// Stage 4 must not change the existing nine-name CanonicalDeployed behaviour.
	for _, name := range docformat.CanonicalDeployed {
		kind, known := docformat.ExpectedMarker(name)
		if !known {
			t.Errorf("ExpectedMarker(%q): known=false (regression — was true before Stage 4)", name)
		}
		if kind != docformat.NodeDeployed {
			t.Errorf("ExpectedMarker(%q): kind=%q (regression — was NodeDeployed before Stage 4)", name, kind)
		}
	}
}

func TestExpectedMarker_Stage4_UnrecognisedName_StillReturnsFalse(t *testing.T) {
	_, known := docformat.ExpectedMarker("SomeCompletelyUnknownBoundaryName")
	if known {
		t.Error("ExpectedMarker(\"SomeCompletelyUnknownBoundaryName\"): known=true, want false")
	}
}

// ---------------------------------------------------------------------------
// ClassifyRegion — amended for managed block names
// ---------------------------------------------------------------------------

func TestClassifyRegion_BareWorkflow_UnderDeployed_ReturnsWorkflowClass(t *testing.T) {
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "Workflow")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"Workflow\"): unexpected error: %v", err)
	}
	if class != domain.InjectionWorkflow {
		t.Errorf("ClassifyRegion(NodeDeployed, \"Workflow\"): want InjectionWorkflow, got %q", class)
	}
}

func TestClassifyRegion_CompoundWorkflow_BrownfieldTDD_UnderDeployed_ReturnsWorkflowClass(t *testing.T) {
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "Workflow:brownfield-tdd")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"Workflow:brownfield-tdd\"): unexpected error: %v", err)
	}
	if class != domain.InjectionWorkflow {
		t.Errorf("ClassifyRegion(NodeDeployed, \"Workflow:brownfield-tdd\"): want InjectionWorkflow, got %q", class)
	}
}

func TestClassifyRegion_CompoundWorkflow_QuickFix_UnderDeployed_ReturnsWorkflowClass(t *testing.T) {
	class, err := docformat.ClassifyRegion(docformat.NodeDeployed, "Workflow:quick-fix")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeDeployed, \"Workflow:quick-fix\"): unexpected error: %v", err)
	}
	if class != domain.InjectionWorkflow {
		t.Errorf("ClassifyRegion(NodeDeployed, \"Workflow:quick-fix\"): want InjectionWorkflow, got %q", class)
	}
}

func TestClassifyRegion_BareWorkflow_UnderInjection_ReturnsProjectClass(t *testing.T) {
	// Injection names are open. "Workflow" under type="project" is not a marker error.
	class, err := docformat.ClassifyRegion(docformat.NodeInjection, "Workflow")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeInjection, \"Workflow\"): unexpected error: %v", err)
	}
	if class != domain.InjectionProject {
		t.Errorf("ClassifyRegion(NodeInjection, \"Workflow\"): want InjectionProject, got %q", class)
	}
}

func TestClassifyRegion_CompoundWorkflow_UnderInjection_ReturnsProjectClass(t *testing.T) {
	// Compound injection names remain open.
	class, err := docformat.ClassifyRegion(docformat.NodeInjection, "Workflow:brownfield-tdd")
	if err != nil {
		t.Fatalf("ClassifyRegion(NodeInjection, \"Workflow:brownfield-tdd\"): unexpected error: %v", err)
	}
	if class != domain.InjectionProject {
		t.Errorf("ClassifyRegion(NodeInjection, \"Workflow:brownfield-tdd\"): want InjectionProject, got %q", class)
	}
}

func TestClassifyRegion_Stage4_UnknownDeployed_StillReturnsUnknownDeployedError(t *testing.T) {
	// An unrecognised type="managed" name that is neither CanonicalDeployed nor a managed
	// block name must still return ErrUnknownDeployedName. Stage 4 must not widen the set.
	_, err := docformat.ClassifyRegion(docformat.NodeDeployed, "SomeCompletelyUnknownBoundaryName")
	if err == nil {
		t.Fatal("ClassifyRegion(NodeDeployed, unknown): want error wrapping ErrUnknownDeployedName, got nil")
	}
	if !errors.Is(err, docformat.ErrUnknownDeployedName) {
		t.Errorf("ClassifyRegion(NodeDeployed, unknown): error does not wrap ErrUnknownDeployedName: %v", err)
	}
}

func TestClassifyRegion_Stage4_AllCanonicalDeployed_UnderDeployed_Unaffected(t *testing.T) {
	// Stage 4 must not change existing classification for CanonicalDeployed names.
	// Each must still return a nil error (regression guard for classification paths).
	for _, name := range docformat.CanonicalDeployed {
		_, err := docformat.ClassifyRegion(docformat.NodeDeployed, name)
		if err != nil {
			t.Errorf("ClassifyRegion(NodeDeployed, %q): unexpected error after Stage 4: %v", name, err)
		}
	}
}
