package docformat_test

// Tests for RenderOpenTagLine and RenderCloseTagLine — the canonical tag-line rendering
// functions — and their agreement with the output produced by AppendRegion.
//
// All tests are in the TDD RED phase. The stub implementations in new_api_stub.go cause
// every test to panic at runtime ("not implemented"), which is the expected TDD RED state.
// Tests must compile but must not pass without the real implementation.
//
// Design note (from ContractsDesign.md):
//   RenderOpenTagLine and RenderCloseTagLine share their implementation with the internal
//   serialiser used by AppendRegion, so appended and externally rendered tags cannot drift
//   apart. A single table asserts both paths agree.
//
// Specification:
//
//   func RenderOpenTagLine(kind NodeKind, name string, version string) ([]byte, error)
//     Returns a canonical opening tag line terminated by a single "\n".
//     kind maps to the type attribute.
//     A compound name (Prefix:id) is split at the first colon: tag name + name attribute.
//     An empty version omits the version attribute.
//     Returns ErrInvalidRegionName when name is not a valid tag name.
//     Returns ErrInvalidRegionKind when kind is not one of the four node kinds.
//     Returns ErrInvalidVersion for a malformed version value.
//
//   func RenderCloseTagLine(name string) ([]byte, error)
//     Returns the canonical closing tag line terminated by a single "\n".
//     Only the prefix of a compound name appears in the tag.
//     Returns ErrInvalidRegionName when name is not a valid tag name.
//
// Coverage (RenderOpenTagLine — happy path):
//   - NodeSection, simple name, no version → <Name type="core">\n
//   - NodeDeployed, simple name, no version → <Name type="managed">\n
//   - NodeInjection, simple name, no version → <Name type="project">\n
//   - NodeCustom, simple name, no version → <Name type="custom">\n
//   - NodeSection, simple name, with version → <Name type="core" version="...">\n
//   - NodeSection, compound name "Prefix:id", no version → <Prefix type="core" name="id">\n
//   - NodeSection, compound name "Prefix:id", with version → <Prefix type="core" name="id" version="...">\n
//   - Output is terminated by exactly one "\n" (never "\r\n").
//   - Canonical attribute order: type, name (when present), version (when present).
//
// Coverage (RenderCloseTagLine — happy path):
//   - Simple name → </Name>\n
//   - Compound name "Prefix:id" → </Prefix>\n (only the prefix).
//   - Output is terminated by exactly one "\n".
//
// Coverage (RenderOpenTagLine — error cases):
//   - Empty name → ErrInvalidRegionName.
//   - Name starting with a digit → ErrInvalidRegionName.
//   - Invalid NodeKind value → ErrInvalidRegionKind.
//   - Version with a double-quote character → ErrInvalidVersion.
//   - Version with a single-quote character → ErrInvalidVersion.
//   - Version with leading/trailing whitespace → ErrInvalidVersion.
//   - Version with a newline → ErrInvalidVersion.
//   - Version with a carriage return → ErrInvalidVersion.
//
// Coverage (RenderCloseTagLine — error cases):
//   - Empty name → ErrInvalidRegionName.
//   - Name starting with a digit → ErrInvalidRegionName.
//
// Coverage (agreement with AppendRegion):
//   - RenderOpenTagLine(NodeInjection, name, "") agrees with what AppendRegion emits.
//   - RenderOpenTagLine(NodeCustom, name, "") agrees with what AppendRegion emits.
//   - RenderCloseTagLine(name) agrees with what AppendRegion emits for close tags.
//   - Compound name agreement: RenderOpenTagLine(NodeInjection, "Workflow:quick-fix", "")
//     agrees with AppendRegion(NodeInjection, "Workflow:quick-fix", nil).

import (
	"bytes"
	"errors"
	"testing"

	"mosaic-common/docformat"
)

// ---------------------------------------------------------------------------
// Table tests: RenderOpenTagLine happy path
// ---------------------------------------------------------------------------

type renderOpenTagTest struct {
	kind    docformat.NodeKind
	name    string
	version string
	want    string // expected output, including "\n"
}

var renderOpenTagCases = []renderOpenTagTest{
	// Each of the four kinds maps to the correct type attribute value.
	{docformat.NodeSection, "Identity", "", "<Identity type=\"core\">\n"},
	{docformat.NodeDeployed, "CommunicationProtocol", "", "<CommunicationProtocol type=\"managed\">\n"},
	{docformat.NodeInjection, "IdentityExtension", "", "<IdentityExtension type=\"project\">\n"},
	{docformat.NodeCustom, "ProjectNotes", "", "<ProjectNotes type=\"custom\">\n"},

	// Version attribute is added after type (and after name if compound).
	{docformat.NodeSection, "Identity", "3.0", "<Identity type=\"core\" version=\"3.0\">\n"},
	{docformat.NodeDeployed, "CommunicationProtocol", "1.10", "<CommunicationProtocol type=\"managed\" version=\"1.10\">\n"},

	// Compound name: prefix goes in the tag name, id goes in name attribute.
	{docformat.NodeSection, "Workflow:quick-fix", "", "<Workflow type=\"core\" name=\"quick-fix\">\n"},
	{docformat.NodeDeployed, "Workflow:quick-fix", "", "<Workflow type=\"managed\" name=\"quick-fix\">\n"},
	{docformat.NodeInjection, "Workflow:quick-fix", "", "<Workflow type=\"project\" name=\"quick-fix\">\n"},
	{docformat.NodeCustom, "Workflow:quick-fix", "", "<Workflow type=\"custom\" name=\"quick-fix\">\n"},

	// Compound name with version: canonical order is type, name, version.
	{docformat.NodeSection, "Workflow:quick-fix", "3.0", "<Workflow type=\"core\" name=\"quick-fix\" version=\"3.0\">\n"},

	// Names with hyphens (valid tag names).
	{docformat.NodeDeployed, "my-harness", "", "<my-harness type=\"managed\">\n"},

	// Multi-segment compound name: everything after the first colon is the name attribute.
	{docformat.NodeSection, "InfrastructureAgent:build-runner", "1.0.0", "<InfrastructureAgent type=\"core\" name=\"build-runner\" version=\"1.0.0\">\n"},

}

func TestRenderOpenTagLine_HappyPath_TableDriven(t *testing.T) {
	for _, tc := range renderOpenTagCases {
		tc := tc
		t.Run(string(tc.kind)+"/"+tc.name+"/version="+tc.version, func(t *testing.T) {
			got, err := docformat.RenderOpenTagLine(tc.kind, tc.name, tc.version)
			if err != nil {
				t.Fatalf("RenderOpenTagLine(%q, %q, %q): unexpected error: %v", tc.kind, tc.name, tc.version, err)
			}
			if string(got) != tc.want {
				t.Errorf("RenderOpenTagLine(%q, %q, %q):\n  want %q\n   got %q", tc.kind, tc.name, tc.version, tc.want, got)
			}
		})
	}
}

func TestRenderOpenTagLine_Output_TerminatedByLFNotCRLF(t *testing.T) {
	// Output must always be terminated by exactly "\n", never "\r\n".
	got, err := docformat.RenderOpenTagLine(docformat.NodeSection, "Identity", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.HasSuffix(got, []byte("\n")) {
		t.Errorf("output not LF-terminated: %q", got)
	}
	if bytes.HasSuffix(got, []byte("\r\n")) {
		t.Errorf("output is CRLF-terminated (want LF only): %q", got)
	}
}

// ---------------------------------------------------------------------------
// Table tests: RenderCloseTagLine happy path
// ---------------------------------------------------------------------------

type renderCloseTagTest struct {
	name string
	want string // expected output, including "\n"
}

var renderCloseTagCases = []renderCloseTagTest{
	// Simple names.
	{"Identity", "</Identity>\n"},
	{"CommunicationProtocol", "</CommunicationProtocol>\n"},
	{"IdentityExtension", "</IdentityExtension>\n"},
	{"ProjectNotes", "</ProjectNotes>\n"},

	// Compound name: only the prefix (before the first colon) appears in the close tag.
	{"Workflow:quick-fix", "</Workflow>\n"},
	{"InfrastructureAgent:build-runner", "</InfrastructureAgent>\n"},
	{"Workflow:variant:extra", "</Workflow>\n"},

	// Hyphenated name.
	{"my-harness", "</my-harness>\n"},
}

func TestRenderCloseTagLine_HappyPath_TableDriven(t *testing.T) {
	for _, tc := range renderCloseTagCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := docformat.RenderCloseTagLine(tc.name)
			if err != nil {
				t.Fatalf("RenderCloseTagLine(%q): unexpected error: %v", tc.name, err)
			}
			if string(got) != tc.want {
				t.Errorf("RenderCloseTagLine(%q):\n  want %q\n   got %q", tc.name, tc.want, got)
			}
		})
	}
}

func TestRenderCloseTagLine_Output_TerminatedByLFNotCRLF(t *testing.T) {
	got, err := docformat.RenderCloseTagLine("Identity")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.HasSuffix(got, []byte("\n")) {
		t.Errorf("output not LF-terminated: %q", got)
	}
	if bytes.HasSuffix(got, []byte("\r\n")) {
		t.Errorf("output is CRLF-terminated (want LF only): %q", got)
	}
}

// ---------------------------------------------------------------------------
// Error cases: RenderOpenTagLine
// ---------------------------------------------------------------------------

func TestRenderOpenTagLine_EmptyName_ReturnsErrInvalidRegionName(t *testing.T) {
	_, err := docformat.RenderOpenTagLine(docformat.NodeSection, "", "")
	if !errors.Is(err, docformat.ErrInvalidRegionName) {
		t.Errorf("RenderOpenTagLine with empty name: want ErrInvalidRegionName, got %v", err)
	}
}

func TestRenderOpenTagLine_NameStartingWithDigit_ReturnsErrInvalidRegionName(t *testing.T) {
	// Tag names must start with a letter: [A-Za-z][A-Za-z0-9-]*
	_, err := docformat.RenderOpenTagLine(docformat.NodeSection, "1Section", "")
	if !errors.Is(err, docformat.ErrInvalidRegionName) {
		t.Errorf("RenderOpenTagLine with digit-leading name: want ErrInvalidRegionName, got %v", err)
	}
}

func TestRenderOpenTagLine_NameWithSpaces_ReturnsErrInvalidRegionName(t *testing.T) {
	_, err := docformat.RenderOpenTagLine(docformat.NodeSection, "My Section", "")
	if !errors.Is(err, docformat.ErrInvalidRegionName) {
		t.Errorf("RenderOpenTagLine with name containing spaces: want ErrInvalidRegionName, got %v", err)
	}
}

func TestRenderOpenTagLine_InvalidKind_ReturnsErrInvalidRegionKind(t *testing.T) {
	// A NodeKind value that is not one of the four defined constants must be rejected.
	_, err := docformat.RenderOpenTagLine(docformat.NodeKind("invalid-kind"), "Identity", "")
	if !errors.Is(err, docformat.ErrInvalidRegionKind) {
		t.Errorf("RenderOpenTagLine with invalid kind: want ErrInvalidRegionKind, got %v", err)
	}
}

func TestRenderOpenTagLine_EmptyKind_ReturnsErrInvalidRegionKind(t *testing.T) {
	_, err := docformat.RenderOpenTagLine(docformat.NodeKind(""), "Identity", "")
	if !errors.Is(err, docformat.ErrInvalidRegionKind) {
		t.Errorf("RenderOpenTagLine with empty kind: want ErrInvalidRegionKind, got %v", err)
	}
}

func TestRenderOpenTagLine_VersionWithDoubleQuote_ReturnsErrInvalidVersion(t *testing.T) {
	_, err := docformat.RenderOpenTagLine(docformat.NodeSection, "Identity", "1\"10")
	if !errors.Is(err, docformat.ErrInvalidVersion) {
		t.Errorf("RenderOpenTagLine with double-quote in version: want ErrInvalidVersion, got %v", err)
	}
}

func TestRenderOpenTagLine_VersionWithLeadingSpace_ReturnsErrInvalidVersion(t *testing.T) {
	_, err := docformat.RenderOpenTagLine(docformat.NodeSection, "Identity", " 1.10")
	if !errors.Is(err, docformat.ErrInvalidVersion) {
		t.Errorf("RenderOpenTagLine with leading-space version: want ErrInvalidVersion, got %v", err)
	}
}

func TestRenderOpenTagLine_VersionWithTrailingSpace_ReturnsErrInvalidVersion(t *testing.T) {
	_, err := docformat.RenderOpenTagLine(docformat.NodeSection, "Identity", "1.10 ")
	if !errors.Is(err, docformat.ErrInvalidVersion) {
		t.Errorf("RenderOpenTagLine with trailing-space version: want ErrInvalidVersion, got %v", err)
	}
}

func TestRenderOpenTagLine_VersionWithNewline_ReturnsErrInvalidVersion(t *testing.T) {
	_, err := docformat.RenderOpenTagLine(docformat.NodeSection, "Identity", "1\n10")
	if !errors.Is(err, docformat.ErrInvalidVersion) {
		t.Errorf("RenderOpenTagLine with newline in version: want ErrInvalidVersion, got %v", err)
	}
}

func TestRenderOpenTagLine_VersionContainsSingleQuote_ReturnsErrInvalidVersion(t *testing.T) {
	// A single-quote character in the version value must be rejected: it would break
	// the attribute quoting. This error must be symmetric with SetVersion's behaviour.
	_, err := docformat.RenderOpenTagLine(docformat.NodeSection, "Identity", "1'10")
	if !errors.Is(err, docformat.ErrInvalidVersion) {
		t.Errorf("RenderOpenTagLine with single-quote in version: want ErrInvalidVersion, got %v", err)
	}
}

func TestRenderOpenTagLine_VersionContainsCR_ReturnsErrInvalidVersion(t *testing.T) {
	// A carriage return is a line terminator character and must be rejected.
	// This error must be symmetric with SetVersion's behaviour.
	_, err := docformat.RenderOpenTagLine(docformat.NodeSection, "Identity", "1\r10")
	if !errors.Is(err, docformat.ErrInvalidVersion) {
		t.Errorf("RenderOpenTagLine with CR in version: want ErrInvalidVersion, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Error cases: RenderCloseTagLine
// ---------------------------------------------------------------------------

func TestRenderCloseTagLine_EmptyName_ReturnsErrInvalidRegionName(t *testing.T) {
	_, err := docformat.RenderCloseTagLine("")
	if !errors.Is(err, docformat.ErrInvalidRegionName) {
		t.Errorf("RenderCloseTagLine with empty name: want ErrInvalidRegionName, got %v", err)
	}
}

func TestRenderCloseTagLine_NameStartingWithDigit_ReturnsErrInvalidRegionName(t *testing.T) {
	// Tag names must start with a letter: [A-Za-z][A-Za-z0-9-]*
	// This must be symmetric with RenderOpenTagLine's digit-leading name rejection.
	_, err := docformat.RenderCloseTagLine("1Section")
	if !errors.Is(err, docformat.ErrInvalidRegionName) {
		t.Errorf("RenderCloseTagLine with digit-leading name: want ErrInvalidRegionName, got %v", err)
	}
}

func TestRenderCloseTagLine_NameWithSpaces_ReturnsErrInvalidRegionName(t *testing.T) {
	_, err := docformat.RenderCloseTagLine("My Section")
	if !errors.Is(err, docformat.ErrInvalidRegionName) {
		t.Errorf("RenderCloseTagLine with name containing spaces: want ErrInvalidRegionName, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Agreement tests: RenderOpenTagLine and RenderCloseTagLine must agree with AppendRegion
//
// AppendRegion only accepts NodeInjection and NodeCustom kinds. The agreement test
// for these two kinds demonstrates that the external rendering API and the internal
// serialiser used by AppendRegion are driven from the same code path.
// ---------------------------------------------------------------------------

func TestRenderOpenTagLine_AgreesWithAppendRegion_InjectionNode(t *testing.T) {
	// RenderOpenTagLine(NodeInjection, "IdentityExtension", "") must produce the same
	// opening tag line that AppendRegion appends when creating an injection node.
	rendered, err := docformat.RenderOpenTagLine(docformat.NodeInjection, "IdentityExtension", "")
	if err != nil {
		t.Fatalf("RenderOpenTagLine: unexpected error: %v", err)
	}

	// Create a document with a section, then append an injection inside it.
	doc := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n")
	sectionNode, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}
	_, appendErr := sectionNode.AppendRegion(docformat.NodeInjection, "IdentityExtension", nil)
	if appendErr != nil {
		t.Fatalf("AppendRegion(NodeInjection, \"IdentityExtension\", nil): %v", appendErr)
	}

	output := doc.Bytes()
	// The appended opening tag line must appear in the document.
	if !bytes.Contains(output, rendered) {
		t.Errorf("AppendRegion output does not contain the tag line produced by RenderOpenTagLine.\nRenderOpenTagLine produced: %q\nDocument bytes:\n%s", rendered, output)
	}
}

func TestRenderOpenTagLine_AgreesWithAppendRegion_CustomNode(t *testing.T) {
	// RenderOpenTagLine(NodeCustom, "ProjectNotes", "") must produce the same opening tag
	// line that AppendRegion appends when creating a custom node.
	rendered, err := docformat.RenderOpenTagLine(docformat.NodeCustom, "ProjectNotes", "")
	if err != nil {
		t.Fatalf("RenderOpenTagLine: unexpected error: %v", err)
	}

	doc := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n")
	_, appendErr := doc.Body().AppendRegion(docformat.NodeCustom, "ProjectNotes", nil)
	if appendErr != nil {
		t.Fatalf("AppendRegion(NodeCustom, \"ProjectNotes\", nil): %v", appendErr)
	}

	output := doc.Bytes()
	if !bytes.Contains(output, rendered) {
		t.Errorf("AppendRegion output does not contain the tag line produced by RenderOpenTagLine.\nRenderOpenTagLine produced: %q\nDocument bytes:\n%s", rendered, output)
	}
}

func TestRenderCloseTagLine_AgreesWithAppendRegion_InjectionNode(t *testing.T) {
	// RenderCloseTagLine("IdentityExtension") must produce the same closing tag line that
	// AppendRegion emits when creating an injection node.
	rendered, err := docformat.RenderCloseTagLine("IdentityExtension")
	if err != nil {
		t.Fatalf("RenderCloseTagLine: unexpected error: %v", err)
	}

	doc := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n")
	sectionNode, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}
	_, appendErr := sectionNode.AppendRegion(docformat.NodeInjection, "IdentityExtension", nil)
	if appendErr != nil {
		t.Fatalf("AppendRegion: %v", appendErr)
	}

	output := doc.Bytes()
	if !bytes.Contains(output, rendered) {
		t.Errorf("AppendRegion output does not contain the close tag line produced by RenderCloseTagLine.\nRenderCloseTagLine produced: %q\nDocument bytes:\n%s", rendered, output)
	}
}

func TestRenderOpenTagLine_AgreesWithAppendRegion_CompoundInjectionName(t *testing.T) {
	// RenderOpenTagLine(NodeInjection, "Workflow:quick-fix", "") must agree with
	// AppendRegion(NodeInjection, "Workflow:quick-fix", nil).
	rendered, err := docformat.RenderOpenTagLine(docformat.NodeInjection, "Workflow:quick-fix", "")
	if err != nil {
		t.Fatalf("RenderOpenTagLine: unexpected error: %v", err)
	}

	doc := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n")
	sectionNode, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}
	_, appendErr := sectionNode.AppendRegion(docformat.NodeInjection, "Workflow:quick-fix", nil)
	if appendErr != nil {
		t.Fatalf("AppendRegion(NodeInjection, \"Workflow:quick-fix\", nil): %v", appendErr)
	}

	output := doc.Bytes()
	if !bytes.Contains(output, rendered) {
		t.Errorf("AppendRegion compound-name output does not contain the tag line produced by RenderOpenTagLine.\nRenderOpenTagLine produced: %q\nDocument bytes:\n%s", rendered, output)
	}
}

func TestRenderCloseTagLine_AgreesWithAppendRegion_CompoundName(t *testing.T) {
	// RenderCloseTagLine("Workflow:quick-fix") → </Workflow>\n
	// AppendRegion(NodeInjection, "Workflow:quick-fix", nil) must emit </Workflow> as the close tag.
	rendered, err := docformat.RenderCloseTagLine("Workflow:quick-fix")
	if err != nil {
		t.Fatalf("RenderCloseTagLine: unexpected error: %v", err)
	}

	doc := parseInlineDoc(t, "<Identity type=\"core\">\n</Identity>\n")
	sectionNode, ok := doc.Body().Section("Identity")
	if !ok {
		t.Fatal("Section(\"Identity\") not found")
	}
	_, appendErr := sectionNode.AppendRegion(docformat.NodeInjection, "Workflow:quick-fix", nil)
	if appendErr != nil {
		t.Fatalf("AppendRegion: %v", appendErr)
	}

	output := doc.Bytes()
	if !bytes.Contains(output, rendered) {
		t.Errorf("AppendRegion compound-name close tag does not match RenderCloseTagLine.\nRenderCloseTagLine produced: %q\nDocument bytes:\n%s", rendered, output)
	}
}

// ---------------------------------------------------------------------------
// Cross-check: RenderOpenTagLine and Parse round-trip
//
// A tag line produced by RenderOpenTagLine must be accepted by the parser as a boundary.
// This verifies that the canonical emitted form satisfies the lenient parse rules.
// ---------------------------------------------------------------------------

func TestRenderOpenTagLine_ProducedTagLine_AcceptedByParser(t *testing.T) {
	// Build an inline document using the tag line from RenderOpenTagLine, then parse it
	// and verify the node is found under the expected addressing API.
	openTag, err := docformat.RenderOpenTagLine(docformat.NodeInjection, "IdentityExtension", "2.1")
	if err != nil {
		t.Fatalf("RenderOpenTagLine: unexpected error: %v", err)
	}
	closeTag, err := docformat.RenderCloseTagLine("IdentityExtension")
	if err != nil {
		t.Fatalf("RenderCloseTagLine: unexpected error: %v", err)
	}

	// Build a full inline doc: a section wrapping the injection.
	src := "<Identity type=\"core\">\n" + string(openTag) + "Content.\n" + string(closeTag) + "</Identity>\n"
	doc := parseInlineDoc(t, src)

	node, ok := doc.Body().Injection("IdentityExtension")
	if !ok {
		t.Fatal("Injection(\"IdentityExtension\") not found after parsing a tag line from RenderOpenTagLine; the canonical emitted form must be accepted by the parser")
	}

	if node.Version() != "2.1" {
		t.Errorf("Version() after round-trip: want %q, got %q", "2.1", node.Version())
	}
}

func TestRenderOpenTagLine_CompoundName_ProducedTagLine_AcceptedByParser(t *testing.T) {
	// A compound-name tag line from RenderOpenTagLine must be accepted by the parser.
	openTag, err := docformat.RenderOpenTagLine(docformat.NodeSection, "Workflow:quick-fix", "3.0")
	if err != nil {
		t.Fatalf("RenderOpenTagLine: unexpected error: %v", err)
	}
	closeTag, err := docformat.RenderCloseTagLine("Workflow:quick-fix")
	if err != nil {
		t.Fatalf("RenderCloseTagLine: unexpected error: %v", err)
	}

	src := string(openTag) + "Content.\n" + string(closeTag)
	doc := parseInlineDoc(t, src)

	node, ok := doc.Body().Section("Workflow:quick-fix")
	if !ok {
		t.Fatal("Section(\"Workflow:quick-fix\") not found after parsing a compound-name tag line from RenderOpenTagLine")
	}

	if node.Name() != "Workflow:quick-fix" {
		t.Errorf("Name(): want %q, got %q", "Workflow:quick-fix", node.Name())
	}
	if node.Version() != "3.0" {
		t.Errorf("Version(): want %q, got %q", "3.0", node.Version())
	}
}
