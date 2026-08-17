package descriptor_test

// Tests for loading a harness descriptor that declares custom_tool_destination.
//
// Coverage:
//   - A descriptor declaring custom_tool_destination with a to:field entry loads
//     successfully and the entry is reachable on domain.ToolSpec with kind, field, format,
//     and separator preserved.
//   - A descriptor omitting custom_tool_destination loads successfully and the slice is empty.
//   - A descriptor declaring custom_tool_destination: [] loads successfully and yields
//     no destinations (identical treatment to the field being absent).
//   - A descriptor declaring both custom_tool_template and custom_tool_destination loads
//     successfully and both fields are independently populated.
//   - A custom_tool_destination entry that declares a names key fails to parse, for both
//     a populated names list (names: ["x"]) and an empty names list (names: []).
//   - All four built-in harness descriptors and the shared fixture descriptor still load
//     and validate without error (regression guard).

import (
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// --- Inline YAML fixtures ---

// customToolDestPopulatedYAML declares a descriptor with one custom_tool_destination entry:
// to:field, field:mcpServers, format:list-block.
const customToolDestPopulatedYAML = `schema_version: "1"
id: "ctd-test"
display_name: "CTD Test"
tools:
  shape: list
  custom_tool_destination:
    - to: field
      field: mcpServers
      format: list-block
frontmatter:
  tools_key: "tools"
`

// customToolDestWithSeparatorYAML declares a descriptor with a scalar-format entry that
// carries a separator value, to verify the separator attribute round-trips correctly.
const customToolDestWithSeparatorYAML = `schema_version: "1"
id: "ctd-separator-test"
display_name: "CTD Separator Test"
tools:
  shape: list
  custom_tool_destination:
    - to: field
      field: mcpServers
      format: scalar
      separator: ", "
frontmatter:
  tools_key: "tools"
`

// customToolDestOmittedYAML declares a descriptor that has no custom_tool_destination key.
const customToolDestOmittedYAML = `schema_version: "1"
id: "ctd-omit-test"
display_name: "CTD Omit Test"
tools:
  shape: list
frontmatter:
  tools_key: "tools"
`

// customToolDestEmptyListYAML declares a descriptor with custom_tool_destination: [].
// The empty list must be treated identically to the field being absent.
const customToolDestEmptyListYAML = `schema_version: "1"
id: "ctd-empty-test"
display_name: "CTD Empty Test"
tools:
  shape: list
  custom_tool_destination: []
frontmatter:
  tools_key: "tools"
`

// customToolDestWithTemplateYAML declares a descriptor with both custom_tool_template and
// custom_tool_destination. The two fields are independent: the template controls name
// formatting, the destination list controls where that name is written.
const customToolDestWithTemplateYAML = `schema_version: "1"
id: "ctd-template-test"
display_name: "CTD Template Test"
tools:
  shape: list
  custom_tool_template: "mcp:%s"
  custom_tool_destination:
    - to: field
      field: mcpServers
      format: list-block
frontmatter:
  tools_key: "tools"
`

// customToolDestNamesPopulatedYAML declares a descriptor whose custom_tool_destination
// entry carries names: ["some-tool"]. The names key must be rejected at parse time.
const customToolDestNamesPopulatedYAML = `schema_version: "1"
id: "ctd-names-test"
display_name: "CTD Names Test"
tools:
  shape: list
  custom_tool_destination:
    - to: field
      field: mcpServers
      names:
        - "some-tool"
frontmatter:
  tools_key: "tools"
`

// customToolDestNamesEmptyYAML declares a descriptor whose custom_tool_destination entry
// carries names: []. An explicitly empty names list must also be rejected at parse time.
const customToolDestNamesEmptyYAML = `schema_version: "1"
id: "ctd-names-empty-test"
display_name: "CTD Names Empty Test"
tools:
  shape: list
  custom_tool_destination:
    - to: field
      field: mcpServers
      names: []
frontmatter:
  tools_key: "tools"
`

// customToolDestMultiEntryYAML declares a descriptor with two valid and distinct
// custom_tool_destination entries: one to:main and one to:field. This exercises the
// slice deep-copy path in mapWireToolSpec and guards against off-by-one errors when
// more than one entry is declared.
const customToolDestMultiEntryYAML = `schema_version: "1"
id: "ctd-multi-test"
display_name: "CTD Multi Test"
tools:
  shape: list
  custom_tool_destination:
    - to: main
    - to: field
      field: mcpServers
      format: list-block
frontmatter:
  tools_key: "tools"
`

// --- Helper ---

// parseCustomToolDest is a thin wrapper over descriptor.Parse for inline YAML literals
// in these tests.
func parseCustomToolDest(t *testing.T, src, origin string) (*domain.HarnessDescriptor, error) {
	t.Helper()
	return descriptor.Parse([]byte(src), origin)
}

// --- T1.1: Populated custom_tool_destination field ---

// TestParse_CustomToolDestination_PopulatedField_LoadsWithoutError asserts that a descriptor
// declaring a to:field custom_tool_destination entry parses without error. This is the
// baseline check — if parse fails, none of the attribute tests below can run.
func TestParse_CustomToolDestination_PopulatedField_LoadsWithoutError(t *testing.T) {
	_, err := parseCustomToolDest(t, customToolDestPopulatedYAML, "inline:ctd-test")
	if err != nil {
		t.Fatalf("Parse: unexpected error for descriptor with populated custom_tool_destination: %v", err)
	}
}

// TestParse_CustomToolDestination_PopulatedField_SliceHasOneEntry asserts that a single
// declared entry produces exactly one element in CustomToolDestinations.
func TestParse_CustomToolDestination_PopulatedField_SliceHasOneEntry(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestPopulatedYAML, "inline:ctd-test")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tools.CustomToolDestinations) != 1 {
		t.Fatalf("CustomToolDestinations: want 1 entry, got %d", len(d.Tools.CustomToolDestinations))
	}
}

// TestParse_CustomToolDestination_PopulatedField_KindPreserved asserts that to:field in YAML
// maps to domain.DestField on the loaded destination.
func TestParse_CustomToolDestination_PopulatedField_KindPreserved(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestPopulatedYAML, "inline:ctd-test")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tools.CustomToolDestinations) == 0 {
		t.Fatal("CustomToolDestinations: empty; cannot check Kind")
	}
	got := d.Tools.CustomToolDestinations[0].Kind
	if got != domain.DestField {
		t.Errorf("CustomToolDestinations[0].Kind: want %q, got %q", domain.DestField, got)
	}
}

// TestParse_CustomToolDestination_PopulatedField_FieldNamePreserved asserts that the field:
// YAML attribute is mapped onto the loaded destination's Field string.
func TestParse_CustomToolDestination_PopulatedField_FieldNamePreserved(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestPopulatedYAML, "inline:ctd-test")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tools.CustomToolDestinations) == 0 {
		t.Fatal("CustomToolDestinations: empty; cannot check Field")
	}
	got := d.Tools.CustomToolDestinations[0].Field
	if got != "mcpServers" {
		t.Errorf("CustomToolDestinations[0].Field: want %q, got %q", "mcpServers", got)
	}
}

// TestParse_CustomToolDestination_PopulatedField_FormatPreserved asserts that format:list-block
// in YAML maps to domain.FormatListBlock on the loaded destination.
func TestParse_CustomToolDestination_PopulatedField_FormatPreserved(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestPopulatedYAML, "inline:ctd-test")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tools.CustomToolDestinations) == 0 {
		t.Fatal("CustomToolDestinations: empty; cannot check Format")
	}
	got := d.Tools.CustomToolDestinations[0].Format
	if got != domain.FormatListBlock {
		t.Errorf("CustomToolDestinations[0].Format: want %q, got %q", domain.FormatListBlock, got)
	}
}

// TestParse_CustomToolDestination_PopulatedField_SeparatorPreserved asserts that the separator:
// YAML attribute is mapped onto the loaded destination's Separator string.
func TestParse_CustomToolDestination_PopulatedField_SeparatorPreserved(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestWithSeparatorYAML, "inline:ctd-separator-test")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tools.CustomToolDestinations) == 0 {
		t.Fatal("CustomToolDestinations: empty; cannot check Separator")
	}
	got := d.Tools.CustomToolDestinations[0].Separator
	if got != ", " {
		t.Errorf("CustomToolDestinations[0].Separator: want %q, got %q", ", ", got)
	}
}

// TestParse_CustomToolDestination_PopulatedField_NamesNotPopulatedAtLoadTime asserts that
// the Names field on a loaded custom_tool_destination entry contains no elements. Names are
// injected at resolution time by MapTools, not at load time; the wire type for this field
// has no Names member so the load path has no source for name data.
func TestParse_CustomToolDestination_PopulatedField_NamesNotPopulatedAtLoadTime(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestPopulatedYAML, "inline:ctd-test")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tools.CustomToolDestinations) == 0 {
		t.Fatal("CustomToolDestinations: empty; cannot check Names")
	}
	names := d.Tools.CustomToolDestinations[0].Names
	if len(names) != 0 {
		t.Errorf("CustomToolDestinations[0].Names: want empty (no names at load time), got %v", names)
	}
}

// --- T1.1: Field omitted ---

// TestParse_CustomToolDestination_FieldOmitted_LoadsWithoutError asserts that a descriptor
// that does not declare custom_tool_destination still loads without error.
func TestParse_CustomToolDestination_FieldOmitted_LoadsWithoutError(t *testing.T) {
	_, err := parseCustomToolDest(t, customToolDestOmittedYAML, "inline:ctd-omit")
	if err != nil {
		t.Fatalf("Parse: unexpected error for descriptor omitting custom_tool_destination: %v", err)
	}
}

// TestParse_CustomToolDestination_FieldOmitted_YieldsNoDestinations asserts that when the
// field is absent, CustomToolDestinations contains no entries.
func TestParse_CustomToolDestination_FieldOmitted_YieldsNoDestinations(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestOmittedYAML, "inline:ctd-omit")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tools.CustomToolDestinations) != 0 {
		t.Errorf("CustomToolDestinations: want empty for omitted field, got %d entries: %v",
			len(d.Tools.CustomToolDestinations), d.Tools.CustomToolDestinations)
	}
}

// --- T1.1: Empty list (custom_tool_destination: []) ---

// TestParse_CustomToolDestination_EmptyList_LoadsWithoutError asserts that declaring
// custom_tool_destination: [] is accepted without error, just like omitting the field.
func TestParse_CustomToolDestination_EmptyList_LoadsWithoutError(t *testing.T) {
	_, err := parseCustomToolDest(t, customToolDestEmptyListYAML, "inline:ctd-empty")
	if err != nil {
		t.Fatalf("Parse: unexpected error for descriptor with custom_tool_destination: []: %v", err)
	}
}

// TestParse_CustomToolDestination_EmptyList_YieldsNoDestinations asserts that
// custom_tool_destination: [] produces the same zero-length result as an absent field.
func TestParse_CustomToolDestination_EmptyList_YieldsNoDestinations(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestEmptyListYAML, "inline:ctd-empty")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tools.CustomToolDestinations) != 0 {
		t.Errorf("CustomToolDestinations: want empty for custom_tool_destination: [], got %d entries: %v",
			len(d.Tools.CustomToolDestinations), d.Tools.CustomToolDestinations)
	}
}

// --- T1.1: Coexistence with custom_tool_template ---

// TestParse_CustomToolDestination_WithCustomToolTemplate_LoadsWithoutError asserts that
// declaring both custom_tool_template and custom_tool_destination on the same descriptor
// succeeds. The two fields are independent.
func TestParse_CustomToolDestination_WithCustomToolTemplate_LoadsWithoutError(t *testing.T) {
	_, err := parseCustomToolDest(t, customToolDestWithTemplateYAML, "inline:ctd-template")
	if err != nil {
		t.Fatalf("Parse: unexpected error for descriptor with both custom_tool_template and custom_tool_destination: %v", err)
	}
}

// TestParse_CustomToolDestination_WithCustomToolTemplate_TemplateFieldPreserved asserts
// that the custom_tool_template value is unchanged when custom_tool_destination is also set.
func TestParse_CustomToolDestination_WithCustomToolTemplate_TemplateFieldPreserved(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestWithTemplateYAML, "inline:ctd-template")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if d.Tools.CustomToolTemplate != "mcp:%s" {
		t.Errorf("CustomToolTemplate: want %q, got %q — custom_tool_template must not be affected by custom_tool_destination",
			"mcp:%s", d.Tools.CustomToolTemplate)
	}
}

// TestParse_CustomToolDestination_WithCustomToolTemplate_DestinationFieldPreserved asserts
// that CustomToolDestinations is correctly loaded when custom_tool_template is also set.
func TestParse_CustomToolDestination_WithCustomToolTemplate_DestinationFieldPreserved(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestWithTemplateYAML, "inline:ctd-template")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tools.CustomToolDestinations) != 1 {
		t.Fatalf("CustomToolDestinations: want 1 entry, got %d — custom_tool_destination must be loaded independently of custom_tool_template",
			len(d.Tools.CustomToolDestinations))
	}
	dest := d.Tools.CustomToolDestinations[0]
	if dest.Kind != domain.DestField {
		t.Errorf("CustomToolDestinations[0].Kind: want %q, got %q", domain.DestField, dest.Kind)
	}
	if dest.Field != "mcpServers" {
		t.Errorf("CustomToolDestinations[0].Field: want %q, got %q", "mcpServers", dest.Field)
	}
}

// --- T1.1: Two-entry list ---

// TestParse_CustomToolDestination_TwoEntries_LoadsWithoutError asserts that a descriptor
// declaring two valid and distinct custom_tool_destination entries (one to:main and one
// to:field) parses without error.
func TestParse_CustomToolDestination_TwoEntries_LoadsWithoutError(t *testing.T) {
	_, err := parseCustomToolDest(t, customToolDestMultiEntryYAML, "inline:ctd-multi")
	if err != nil {
		t.Fatalf("Parse: unexpected error for descriptor with two custom_tool_destination entries: %v", err)
	}
}

// TestParse_CustomToolDestination_TwoEntries_SliceHasTwoElements asserts that a two-entry
// list produces exactly two elements in CustomToolDestinations. This exercises the
// slice deep-copy path in mapWireToolSpec.
func TestParse_CustomToolDestination_TwoEntries_SliceHasTwoElements(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestMultiEntryYAML, "inline:ctd-multi")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tools.CustomToolDestinations) != 2 {
		t.Fatalf("CustomToolDestinations: want 2 entries, got %d", len(d.Tools.CustomToolDestinations))
	}
}

// TestParse_CustomToolDestination_TwoEntries_FirstEntryIsMain asserts that the first entry
// in a two-entry list maps to kind DestMain.
func TestParse_CustomToolDestination_TwoEntries_FirstEntryIsMain(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestMultiEntryYAML, "inline:ctd-multi")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tools.CustomToolDestinations) < 1 {
		t.Fatal("CustomToolDestinations: fewer than 1 entry; cannot check first entry Kind")
	}
	if d.Tools.CustomToolDestinations[0].Kind != domain.DestMain {
		t.Errorf("CustomToolDestinations[0].Kind: want %q, got %q", domain.DestMain, d.Tools.CustomToolDestinations[0].Kind)
	}
}

// TestParse_CustomToolDestination_TwoEntries_SecondEntryIsField asserts that the second entry
// in a two-entry list maps to kind DestField with the expected field name and format.
func TestParse_CustomToolDestination_TwoEntries_SecondEntryIsField(t *testing.T) {
	d, err := parseCustomToolDest(t, customToolDestMultiEntryYAML, "inline:ctd-multi")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(d.Tools.CustomToolDestinations) < 2 {
		t.Fatal("CustomToolDestinations: fewer than 2 entries; cannot check second entry")
	}
	dest := d.Tools.CustomToolDestinations[1]
	if dest.Kind != domain.DestField {
		t.Errorf("CustomToolDestinations[1].Kind: want %q, got %q", domain.DestField, dest.Kind)
	}
	if dest.Field != "mcpServers" {
		t.Errorf("CustomToolDestinations[1].Field: want %q, got %q", "mcpServers", dest.Field)
	}
	if dest.Format != domain.FormatListBlock {
		t.Errorf("CustomToolDestinations[1].Format: want %q, got %q", domain.FormatListBlock, dest.Format)
	}
}

// --- T1.2: names key rejected at parse time ---

// TestParse_CustomToolDestination_NamesKeyPopulated_ParseFails asserts that a
// custom_tool_destination entry declaring names: ["some-tool"] fails to parse.
// The names key is prohibited — it is meaningless for a harness-level default that
// applies to every not-yet-resolved custom tool. The dedicated wire type for this field
// has no Names member, so DisallowUnknownField rejects the key at decode time.
//
// Structure: this test first verifies that the base fixture (without names:) parses
// successfully. In TDD RED state, that prerequisite fails because custom_tool_destination
// is not yet a known wire field, correctly placing the test in the RED phase. In GREEN
// state, the prerequisite passes and the test then verifies that adding names: causes the
// parse to fail — gating the specific implementation requirement that the dedicated wire
// type omits Names.
func TestParse_CustomToolDestination_NamesKeyPopulated_ParseFails(t *testing.T) {
	// Prerequisite: the base fixture (a valid entry without names:) must parse successfully.
	// If this fails, custom_tool_destination is not yet wired — the test is correctly RED.
	_, prereqErr := parseCustomToolDest(t, customToolDestPopulatedYAML, "inline:ctd-test")
	if prereqErr != nil {
		t.Fatalf("prerequisite: a custom_tool_destination entry without names: must parse "+
			"successfully once the wire field exists; got error: %v — "+
			"implement the custom_tool_destination wire field before this test can gate names: rejection",
			prereqErr)
	}

	_, err := parseCustomToolDest(t, customToolDestNamesPopulatedYAML, "inline:ctd-names")
	if err == nil {
		t.Fatal("Parse: expected error for custom_tool_destination entry with names: [\"some-tool\"], got nil; " +
			"the names key must be rejected at parse time for custom_tool_destination entries")
	}
}

// TestParse_CustomToolDestination_NamesKeyEmpty_ParseFails asserts that a
// custom_tool_destination entry declaring names: [] also fails to parse.
// Even an explicitly empty names list must be rejected — the distinction between absent
// and empty is deliberately not preserved at the domain layer for this field.
//
// Structure: mirrors TestParse_CustomToolDestination_NamesKeyPopulated_ParseFails. The
// prerequisite check on the base fixture ensures the test fails in RED (before the wire
// field exists) and passes only in GREEN with a correctly implemented dedicated wire type.
func TestParse_CustomToolDestination_NamesKeyEmpty_ParseFails(t *testing.T) {
	// Prerequisite: the base fixture (a valid entry without names:) must parse successfully.
	// If this fails, custom_tool_destination is not yet wired — the test is correctly RED.
	_, prereqErr := parseCustomToolDest(t, customToolDestPopulatedYAML, "inline:ctd-test")
	if prereqErr != nil {
		t.Fatalf("prerequisite: a custom_tool_destination entry without names: must parse "+
			"successfully once the wire field exists; got error: %v — "+
			"implement the custom_tool_destination wire field before this test can gate names: rejection",
			prereqErr)
	}

	_, err := parseCustomToolDest(t, customToolDestNamesEmptyYAML, "inline:ctd-names-empty")
	if err == nil {
		t.Fatal("Parse: expected error for custom_tool_destination entry with names: [], got nil; " +
			"an explicitly empty names key must also be rejected at parse time")
	}
}

// --- T1.4: Regression — existing descriptors load and validate without error ---

// builtinDescriptors lists the four embedded harness descriptor YAML files relative to
// the descriptor package directory (Tools/Deployment/internal/harness/descriptor/).
var builtinDescriptors = []struct {
	harness string
	relPath string
}{
	{"claude-code", filepath.Join("..", "builtin", "claudecode", "claude-code.yaml")},
	{"opencode", filepath.Join("..", "builtin", "opencode", "opencode.yaml")},
	{"ghcp-cli", filepath.Join("..", "builtin", "ghcpcli", "ghcp-cli.yaml")},
	{"vscode-ghcp", filepath.Join("..", "builtin", "vscodeghcp", "vscode-ghcp.yaml")},
}

// TestLoad_ExistingBuiltinDescriptors_StillLoadWithoutError is a regression guard
// asserting that adding the custom_tool_destination field to the wire type does not
// break loading of any of the four built-in harness descriptors.
func TestLoad_ExistingBuiltinDescriptors_StillLoadWithoutError(t *testing.T) {
	for _, tc := range builtinDescriptors {
		tc := tc
		t.Run(tc.harness, func(t *testing.T) {
			src, err := os.ReadFile(tc.relPath)
			if err != nil {
				t.Fatalf("read %s descriptor at %s: %v", tc.harness, tc.relPath, err)
			}
			d, err := descriptor.Parse(src, tc.relPath)
			if err != nil {
				t.Fatalf("Parse %s descriptor: unexpected error: %v", tc.harness, err)
			}
			if d == nil {
				t.Fatalf("Parse %s descriptor: returned nil without error", tc.harness)
			}
		})
	}
}

// TestLoad_SharedFixtureDescriptor_StillLoadsWithoutError is a regression guard
// asserting that the shared testdata fixture descriptor (valid-full.yaml) still loads
// and validates without error. This fixture is the reference descriptor used by
// load_test.go for field-by-field assertions.
func TestLoad_SharedFixtureDescriptor_StillLoadsWithoutError(t *testing.T) {
	fixturePath := filepath.Join(testdataDir, "valid-full.yaml")
	d, err := descriptor.Load(fixturePath)
	if err != nil {
		t.Fatalf("Load shared fixture descriptor (valid-full.yaml): unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("Load shared fixture descriptor: returned nil without error")
	}
}
