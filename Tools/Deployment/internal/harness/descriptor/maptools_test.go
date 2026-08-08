package descriptor_test

// Tests for tool mapping lookup semantics: one-to-many, many-to-one, and unmapped.
//
// Coverage:
//   - One generic tool mapping to several harness tools produces a ToolResolution with all
//     harness tools listed and outcome ToolMapped.
//   - Several generic tools each mapping to the same single harness tool produce separate
//     ToolResolutions, each with outcome ToolMapped and the same HarnessTools list.
//   - A generic tool with no mapping entry in the descriptor produces a ToolResolution with
//     outcome ToolUnmapped — the unmapped case is a distinct outcome, not an empty result.
//   - MapTools returns exactly one ToolResolution per entry in ToolRequest.Generic.
//   - The order of Resolutions matches the order of ToolRequest.Generic (not mapping order).
//   - When all tools are mapped, MapTools returns no error.
//   - An empty Generic list produces an empty Resolutions slice, not an error.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// mappingDescriptorYAML is the inline YAML for a descriptor used by all MapTools tests.
// It uses the multi-destination schema (destinations:) introduced in Stage 2.
// It contains:
//   - file_write → DestMain ["write/createFile", "write/editFile"]  (one-to-many via single destination)
//   - file_search → DestMain ["search/textSearch"]                  (one half of many-to-one pair)
//   - content_search → DestMain ["search/textSearch"]               (other half of many-to-one pair)
//   - file_read → DestMain ["read/readFile"]                        (one-to-one, for baseline)
//   - user_interaction → destinations: []                           (explicitly unsupported)
//   - "terminal" is absent from mappings (unmapped)
const mappingDescriptorYAML = `schema_version: "1"
id: "mapping-test-harness"
display_name: "Mapping Test Harness"
tools:
  shape: list
  universe:
    - name: "read/readFile"
      unused: deny
      by_convention: false
    - name: "write/createFile"
      unused: deny
      by_convention: false
    - name: "write/editFile"
      unused: deny
      by_convention: false
    - name: "search/textSearch"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      destinations:
        - to: main
          names:
            - "read/readFile"
    - generic: "file_write"
      destinations:
        - to: main
          names:
            - "write/createFile"
            - "write/editFile"
    - generic: "file_search"
      destinations:
        - to: main
          names:
            - "search/textSearch"
    - generic: "content_search"
      destinations:
        - to: main
          names:
            - "search/textSearch"
    - generic: "user_interaction"
      destinations: []
frontmatter:
  tools_key: "tools"
`

func loadMappingDescriptor(t *testing.T) *domain.HarnessDescriptor {
	t.Helper()
	d, err := descriptor.Parse([]byte(mappingDescriptorYAML), "inline:mapping-test-harness")
	if err != nil {
		t.Fatalf("Parse mapping descriptor: %v", err)
	}
	if d == nil {
		t.Fatal("Parse returned nil descriptor without error")
	}
	return d
}

// --- One-to-many: one generic tool maps to several harness tools ---

func TestMapTools_OneToMany_OutcomeIsMapped(t *testing.T) {
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_write"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools returned unexpected error: %v", err)
	}
	if len(result.Resolutions) == 0 {
		t.Fatal("expected at least one resolution")
	}
	if result.Resolutions[0].Outcome != domain.ToolMapped {
		t.Errorf("file_write outcome: want %q, got %q", domain.ToolMapped, result.Resolutions[0].Outcome)
	}
}

func TestMapTools_OneToMany_AllHarnessToolsPresent(t *testing.T) {
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_write"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if res.Generic != "file_write" {
		t.Errorf("resolution Generic: want %q, got %q", "file_write", res.Generic)
	}
	// file_write maps to two harness tools via a DestMain destination.
	// HarnessTools is the flattened union of all Destinations' Names.
	wantTools := []string{"write/createFile", "write/editFile"}
	if len(res.HarnessTools) != len(wantTools) {
		t.Fatalf("HarnessTools count: want %d, got %d: %v", len(wantTools), len(res.HarnessTools), res.HarnessTools)
	}
	for i, want := range wantTools {
		if res.HarnessTools[i] != want {
			t.Errorf("HarnessTools[%d]: want %q, got %q", i, want, res.HarnessTools[i])
		}
	}
	// Destinations must also be populated: one DestMain destination carrying both names.
	if len(res.Destinations) != 1 {
		t.Errorf("Destinations count: want 1, got %d", len(res.Destinations))
	}
}

func TestMapTools_OneToMany_ResultHasOneResolutionForOneInput(t *testing.T) {
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_write"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	// Exactly one generic tool in → exactly one ToolResolution out.
	if len(result.Resolutions) != 1 {
		t.Errorf("expected 1 resolution for 1 input generic tool, got %d", len(result.Resolutions))
	}
}

// --- Many-to-one: several generic tools each map to the same single harness tool ---

func TestMapTools_ManyToOne_EachGenericHasSeparateResolution(t *testing.T) {
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_search", "content_search"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	// Two generic tools → two resolutions, one per generic tool.
	if len(result.Resolutions) != 2 {
		t.Fatalf("expected 2 resolutions for 2 generic tools, got %d: %v", len(result.Resolutions), result.Resolutions)
	}
}

func TestMapTools_ManyToOne_BothOutcomesAreMapped(t *testing.T) {
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_search", "content_search"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	for _, res := range result.Resolutions {
		if res.Outcome != domain.ToolMapped {
			t.Errorf("generic %q: outcome want %q, got %q", res.Generic, domain.ToolMapped, res.Outcome)
		}
	}
}

func TestMapTools_ManyToOne_BothMapToSameHarnessTool(t *testing.T) {
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_search", "content_search"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 2 {
		t.Fatalf("expected 2 resolutions, got %d", len(result.Resolutions))
	}
	// Both file_search and content_search map to ["search/textSearch"] in the fixture.
	for _, res := range result.Resolutions {
		if len(res.HarnessTools) != 1 {
			t.Errorf("generic %q: expected 1 harness tool, got %d: %v", res.Generic, len(res.HarnessTools), res.HarnessTools)
			continue
		}
		if res.HarnessTools[0] != "search/textSearch" {
			t.Errorf("generic %q: expected harness tool %q, got %q", res.Generic, "search/textSearch", res.HarnessTools[0])
		}
	}
}

func TestMapTools_ManyToOne_GenericNamesPreservedInResolutions(t *testing.T) {
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_search", "content_search"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 2 {
		t.Fatalf("expected 2 resolutions, got %d", len(result.Resolutions))
	}
	// The Generic field of each resolution must reflect the original generic tool name.
	if result.Resolutions[0].Generic != "file_search" {
		t.Errorf("Resolutions[0].Generic: want %q, got %q", "file_search", result.Resolutions[0].Generic)
	}
	if result.Resolutions[1].Generic != "content_search" {
		t.Errorf("Resolutions[1].Generic: want %q, got %q", "content_search", result.Resolutions[1].Generic)
	}
}

// --- Unmapped: a generic tool with no mapping entry ---

func TestMapTools_UnmappedGeneric_OutcomeIsToolUnmapped(t *testing.T) {
	// "terminal" is not in the mapping descriptor's Mappings list.
	// The outcome must be ToolUnmapped — a distinct value, not ToolMapped with empty tools.
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"terminal"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if res.Outcome != domain.ToolUnmapped {
		t.Errorf("unmapped generic tool outcome: want %q, got %q", domain.ToolUnmapped, res.Outcome)
	}
}

func TestMapTools_UnmappedGeneric_HarnessToolsIsEmpty(t *testing.T) {
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"terminal"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	if len(result.Resolutions[0].HarnessTools) != 0 {
		t.Errorf("unmapped generic tool must have empty HarnessTools, got: %v", result.Resolutions[0].HarnessTools)
	}
}

func TestMapTools_UnmappedIsDistinctFromMappedWithNoTools(t *testing.T) {
	// ToolUnmapped (no mapping entry) must be a different outcome from ToolMapped with
	// an empty HarnessTools list (which represents an explicitly unsupported tool).
	// This test verifies the constants differ.
	if domain.ToolUnmapped == domain.ToolMapped {
		t.Error("ToolUnmapped and ToolMapped must be distinct outcome values")
	}
}

// --- Resolution count and ordering ---

func TestMapTools_ResolutionCountMatchesGenericCount(t *testing.T) {
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "file_write", "file_search", "content_search", "terminal"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != len(req.Generic) {
		t.Errorf("resolution count: want %d (one per generic tool), got %d",
			len(req.Generic), len(result.Resolutions))
	}
}

func TestMapTools_ResolutionOrderMatchesGenericOrder(t *testing.T) {
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"content_search", "file_read", "file_write"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 3 {
		t.Fatalf("expected 3 resolutions, got %d", len(result.Resolutions))
	}
	// Resolutions must follow the same order as req.Generic.
	for i, generic := range req.Generic {
		if result.Resolutions[i].Generic != generic {
			t.Errorf("Resolutions[%d].Generic: want %q, got %q", i, generic, result.Resolutions[i].Generic)
		}
	}
}

func TestMapTools_EmptyGenericList_ReturnsEmptyResolutions(t *testing.T) {
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools with empty Generic list returned unexpected error: %v", err)
	}
	if len(result.Resolutions) != 0 {
		t.Errorf("empty Generic list must produce empty Resolutions, got %d", len(result.Resolutions))
	}
}

// --- ToolResult.Fields: the primary frontmatter output ---

func TestMapTools_Fields_NonEmptyForMappedTools(t *testing.T) {
	// Fields is the primary output of MapTools: the rendered frontmatter value ready for
	// placement. An implementation that correctly populates Resolutions but leaves Fields
	// empty or nil fails this test.
	d := loadMappingDescriptor(t)
	// Use a request with only mapped tools so the Fields output is unambiguous.
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Fields) == 0 {
		t.Fatal("expected Fields to be non-empty; Fields is the primary frontmatter output of MapTools")
	}
}

func TestMapTools_Fields_ContainsToolsKey(t *testing.T) {
	// The descriptor declares frontmatter.tools_key: "tools". MapTools must return a Fields
	// entry with Key == "tools" so the caller can write it into frontmatter without guessing.
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	var found bool
	for _, f := range result.Fields {
		if f.Key == "tools" {
			found = true
			break
		}
	}
	if !found {
		keys := make([]string, len(result.Fields))
		for i, f := range result.Fields {
			keys[i] = f.Key
		}
		t.Errorf("expected a FrontmatterField with Key=%q in Fields; got keys: %v", "tools", keys)
	}
}

func TestMapTools_Fields_ToolsValueContainsMappedHarnessTool(t *testing.T) {
	// The "tools" field value must list the harness tool names that the mapped generic tools
	// resolve to. For file_read → ["read/readFile"], the value must contain "read/readFile".
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}

	var toolsField *domain.FrontmatterField
	for i := range result.Fields {
		if result.Fields[i].Key == "tools" {
			toolsField = &result.Fields[i]
			break
		}
	}
	if toolsField == nil {
		t.Fatal("no FrontmatterField with Key=\"tools\" in Fields")
	}
	if toolsField.Value.Kind != domain.KindList {
		t.Fatalf("tools field value kind: want %q, got %q", domain.KindList, toolsField.Value.Kind)
	}
	var containsReadFile bool
	for _, item := range toolsField.Value.Items {
		if item.Kind == domain.KindScalar && item.Scalar == "read/readFile" {
			containsReadFile = true
			break
		}
	}
	if !containsReadFile {
		t.Errorf("tools field value should contain %q; got items: %v", "read/readFile", toolsField.Value.Items)
	}
}

// --- Explicitly unsupported tool: mapping entry with empty HarnessTools ---

func TestMapTools_ExplicitlyUnsupported_OutcomeIsMappedWithEmptyHarnessTools(t *testing.T) {
	// user_interaction has a mapping entry with destinations: [] in the descriptor.
	// This is the "explicitly unsupported" case: the descriptor author acknowledged the tool
	// and declared that this harness does not support it.
	// The outcome must be ToolMapped (the tool was found in the mapping table), not ToolUnmapped.
	// HarnessTools must be empty (len == 0), distinguishing it from a tool that maps to real tools.
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if res.Outcome != domain.ToolMapped {
		t.Errorf("explicitly-unsupported tool outcome: want %q, got %q; an empty HarnessTools slice in a mapping entry means ToolMapped with no harness tools, not ToolUnmapped", domain.ToolMapped, res.Outcome)
	}
	if len(res.HarnessTools) != 0 {
		t.Errorf("explicitly-unsupported tool HarnessTools: want empty, got %v", res.HarnessTools)
	}
}

func TestMapTools_ExplicitlyUnsupported_IsDistinctFromUnmappedAtRuntime(t *testing.T) {
	// This is the runtime complement to TestMapTools_UnmappedIsDistinctFromMappedWithNoTools.
	// Call MapTools with both user_interaction (explicitly unsupported, in mappings with [])
	// and terminal (absent from mappings entirely) and verify their outcomes differ.
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction", "terminal"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 2 {
		t.Fatalf("expected 2 resolutions, got %d", len(result.Resolutions))
	}
	// user_interaction is explicitly unsupported: ToolMapped with empty HarnessTools.
	explicitRes := result.Resolutions[0]
	if explicitRes.Generic != "user_interaction" {
		t.Fatalf("Resolutions[0].Generic: want %q, got %q", "user_interaction", explicitRes.Generic)
	}
	if explicitRes.Outcome != domain.ToolMapped {
		t.Errorf("user_interaction (explicitly unsupported): want outcome %q, got %q", domain.ToolMapped, explicitRes.Outcome)
	}
	// terminal has no mapping entry: ToolUnmapped.
	unmappedRes := result.Resolutions[1]
	if unmappedRes.Generic != "terminal" {
		t.Fatalf("Resolutions[1].Generic: want %q, got %q", "terminal", unmappedRes.Generic)
	}
	if unmappedRes.Outcome != domain.ToolUnmapped {
		t.Errorf("terminal (unmapped): want outcome %q, got %q", domain.ToolUnmapped, unmappedRes.Outcome)
	}
	// The two outcomes must differ.
	if explicitRes.Outcome == unmappedRes.Outcome {
		t.Errorf("explicitly-unsupported and unmapped must have different outcomes; both got %q", explicitRes.Outcome)
	}
}

// --- ToolCustom: user-supplied MCP server name ---

func TestMapTools_Custom_OutcomeIsToolCustom(t *testing.T) {
	// When a generic tool has no mapping entry but CustomNames provides a server name,
	// the outcome must be ToolCustom. This path is exercised when a user sets up their
	// own MCP tool that is not in the harness's standard vocabulary.
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-mcp-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	if result.Resolutions[0].Outcome != domain.ToolCustom {
		t.Errorf("custom-named tool outcome: want %q, got %q", domain.ToolCustom, result.Resolutions[0].Outcome)
	}
}

func TestMapTools_Custom_HarnessToolsContainsCustomName(t *testing.T) {
	// The custom MCP server name must appear in HarnessTools so the rendering path can
	// include it in the output without needing to re-inspect CustomNames.
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-mcp-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if len(res.HarnessTools) == 0 {
		t.Fatal("custom tool must have non-empty HarnessTools")
	}
	// The custom name (possibly formatted with CustomToolTemplate) must be present.
	// The descriptor's custom_tool_template is empty, so the name is used as-is.
	var found bool
	for _, ht := range res.HarnessTools {
		if ht == "my-mcp-server" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("custom name %q not found in HarnessTools: %v", "my-mcp-server", res.HarnessTools)
	}
}

func TestMapTools_Custom_DestinationsContainsSingleDestMain(t *testing.T) {
	// For a ToolCustom outcome (no mapping, CustomNames supplies a name), the design contract
	// requires MapTools to produce a single destination of kind DestMain carrying the templated
	// name — preserving the pre-Destinations behaviour of putting the name into the main tools
	// field. This test is RED until MapTools populates Destinations for the ToolCustom path.
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-mcp-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if res.Outcome != domain.ToolCustom {
		t.Fatalf("expected ToolCustom outcome, got %q", res.Outcome)
	}
	// The design contract: "For a ToolCustom outcome, produce a single destination of kind
	// DestMain carrying the templated name."
	if len(res.Destinations) != 1 {
		t.Fatalf("ToolCustom resolution Destinations: want exactly 1 (a single DestMain), got %d; "+
			"MapTools must populate Destinations for the ToolCustom path", len(res.Destinations))
	}
	dest := res.Destinations[0]
	if dest.Kind != domain.DestMain {
		t.Errorf("ToolCustom Destinations[0].Kind: want %q, got %q; "+
			"the custom name must go to the main tools destination", domain.DestMain, dest.Kind)
	}
	// The destination must carry the (possibly templated) custom name in its Names slice.
	if len(dest.Names) == 0 {
		t.Fatal("ToolCustom Destinations[0].Names is empty; must contain the custom harness tool name")
	}
	var found bool
	for _, n := range dest.Names {
		if n == "my-mcp-server" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ToolCustom Destinations[0].Names does not contain %q: %v", "my-mcp-server", dest.Names)
	}
}

// --- ToolSkipped / ToolUnmapped: no destinations produced ---

func TestMapTools_Skipped_DestinationsIsEmpty(t *testing.T) {
	// The design contract states: "For ToolSkipped and ToolUnmapped, produce no destinations."
	// An empty (nil or zero-length) Destinations slice is the correct representation; any
	// non-empty Destinations on a skipped tool would be spurious.
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey:     "test-agent",
		Generic:      []string{"file_read"},
		SkippedTools: map[string]bool{"file_read": true},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if res.Outcome != domain.ToolSkipped {
		t.Fatalf("expected ToolSkipped outcome, got %q", res.Outcome)
	}
	if len(res.Destinations) != 0 {
		t.Errorf("ToolSkipped resolution Destinations: want empty, got %d destinations: %+v; "+
			"skipped tools must produce no destinations", len(res.Destinations), res.Destinations)
	}
}

func TestMapTools_Unmapped_DestinationsIsEmpty(t *testing.T) {
	// The design contract states: "For ToolSkipped and ToolUnmapped, produce no destinations."
	// "terminal" is absent from the mapping descriptor's Mappings list → ToolUnmapped.
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"terminal"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if res.Outcome != domain.ToolUnmapped {
		t.Fatalf("expected ToolUnmapped outcome, got %q", res.Outcome)
	}
	if len(res.Destinations) != 0 {
		t.Errorf("ToolUnmapped resolution Destinations: want empty, got %d destinations: %+v; "+
			"unmapped tools must produce no destinations", len(res.Destinations), res.Destinations)
	}
}

// --- ToolSkipped: user declined to name an unmapped tool ---

func TestMapTools_Skipped_OutcomeIsToolSkipped(t *testing.T) {
	// When a tool is listed in req.SkippedTools, its outcome must be ToolSkipped regardless
	// of whether it has a mapping entry. This path is exercised when the user explicitly
	// declines to configure a tool.
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey:     "test-agent",
		Generic:      []string{"file_read"},
		SkippedTools: map[string]bool{"file_read": true},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	if result.Resolutions[0].Outcome != domain.ToolSkipped {
		t.Errorf("skipped tool outcome: want %q, got %q", domain.ToolSkipped, result.Resolutions[0].Outcome)
	}
}

func TestMapTools_Skipped_GenericNamePreserved(t *testing.T) {
	// The Generic field of the resolution must still name the original tool even when skipped.
	d := loadMappingDescriptor(t)
	req := domain.ToolRequest{
		AgentKey:     "test-agent",
		Generic:      []string{"file_read"},
		SkippedTools: map[string]bool{"file_read": true},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	if result.Resolutions[0].Generic != "file_read" {
		t.Errorf("skipped tool resolution Generic: want %q, got %q", "file_read", result.Resolutions[0].Generic)
	}
}

// --- Mixed request ---

func TestMapTools_MixedRequest_EachToolHasCorrectOutcome(t *testing.T) {
	d := loadMappingDescriptor(t)
	// Mix of mapped (file_read), one-to-many (file_write), and unmapped (terminal).
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "file_write", "terminal"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 3 {
		t.Fatalf("expected 3 resolutions, got %d", len(result.Resolutions))
	}

	// file_read → mapped to one tool
	if result.Resolutions[0].Outcome != domain.ToolMapped {
		t.Errorf("file_read: want outcome %q, got %q", domain.ToolMapped, result.Resolutions[0].Outcome)
	}
	if len(result.Resolutions[0].HarnessTools) != 1 {
		t.Errorf("file_read: want 1 harness tool, got %d", len(result.Resolutions[0].HarnessTools))
	}

	// file_write → mapped to two tools (one-to-many)
	if result.Resolutions[1].Outcome != domain.ToolMapped {
		t.Errorf("file_write: want outcome %q, got %q", domain.ToolMapped, result.Resolutions[1].Outcome)
	}
	if len(result.Resolutions[1].HarnessTools) != 2 {
		t.Errorf("file_write: want 2 harness tools, got %d", len(result.Resolutions[1].HarnessTools))
	}

	// terminal → unmapped
	if result.Resolutions[2].Outcome != domain.ToolUnmapped {
		t.Errorf("terminal: want outcome %q, got %q", domain.ToolUnmapped, result.Resolutions[2].Outcome)
	}
}

// --- Permission shape: MapTools produces KindMapping Fields, not KindList ---
//
// Regression coverage for the bug where buildToolFields ignored d.Tools.Shape for
// shape:permission descriptors and always emitted a flat KindList. The fix split
// buildToolFields into buildListToolFields (for ShapeList) and buildPermissionToolFields
// (for ShapePermission), which emits a KindMapping field with one pair per Universe tool.
//
// The fixture is testdata/descriptors/valid-permission-shape.yaml. It declares:
//   - Universe: "read" (unused: deny), "write" (unused: deny), "execute" (unused: deny)
//   - Mappings: file_read → ["read"], file_write → ["write"]
//   - frontmatter.tools_key: "permission"
//
// When MapTools is called with Generic: ["file_read", "file_write"]:
//   - "read"    → allow (resolved via file_read mapping)
//   - "write"   → allow (resolved via file_write mapping)
//   - "execute" → deny  (not resolved; falls back to HarnessTool.Unused)

func loadPermissionShapeDescriptor(t *testing.T) *domain.HarnessDescriptor {
	t.Helper()
	return loadFixture(t, "valid-permission-shape.yaml")
}

func TestMapTools_PermissionShape_FieldsValueIsKindMapping(t *testing.T) {
	// The core regression: a permission-shape descriptor must produce a KindMapping Fields
	// value. An implementation that ignores d.Tools.Shape would produce KindList here.
	d := loadPermissionShapeDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "file_write"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Fields) == 0 {
		t.Fatal("expected Fields to be non-empty for permission-shape descriptor")
	}
	var permField *domain.FrontmatterField
	for i := range result.Fields {
		if result.Fields[i].Key == "permission" {
			permField = &result.Fields[i]
			break
		}
	}
	if permField == nil {
		keys := make([]string, len(result.Fields))
		for i, f := range result.Fields {
			keys[i] = f.Key
		}
		t.Fatalf("expected a FrontmatterField with Key=%q; got keys: %v", "permission", keys)
	}
	if permField.Value.Kind != domain.KindMapping {
		t.Errorf("permission field value kind: want %q, got %q; permission-shape descriptor must emit a KindMapping, not KindList", domain.KindMapping, permField.Value.Kind)
	}
}

func TestMapTools_PermissionShape_FieldsContainsPermissionKey(t *testing.T) {
	// The frontmatter.tools_key in the fixture is "permission". MapTools must emit a
	// FrontmatterField with that key so the caller can place it without guessing the key name.
	d := loadPermissionShapeDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "file_write"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	var found bool
	for _, f := range result.Fields {
		if f.Key == "permission" {
			found = true
			break
		}
	}
	if !found {
		keys := make([]string, len(result.Fields))
		for i, f := range result.Fields {
			keys[i] = f.Key
		}
		t.Errorf("expected FrontmatterField with Key=%q in Fields; got keys: %v", "permission", keys)
	}
}

func TestMapTools_PermissionShape_PairsCountMatchesUniverse(t *testing.T) {
	// Every tool in the Universe must appear in the Pairs output exactly once, regardless of
	// which generic tools are in the request. The fixture Universe has three tools.
	d := loadPermissionShapeDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "file_write"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	var permField *domain.FrontmatterField
	for i := range result.Fields {
		if result.Fields[i].Key == "permission" {
			permField = &result.Fields[i]
			break
		}
	}
	if permField == nil {
		t.Fatal("no FrontmatterField with Key=\"permission\" in Fields")
	}
	// The fixture Universe: "read", "write", "execute" — three entries.
	wantPairs := 3
	if len(permField.Value.Pairs) != wantPairs {
		t.Errorf("Pairs count: want %d (one per Universe tool), got %d: %v",
			wantPairs, len(permField.Value.Pairs), permField.Value.Pairs)
	}
}

func TestMapTools_PermissionShape_MappedToolGetsAllowDisposition(t *testing.T) {
	// A Universe tool that appears in the resolved set (via a mapping) must have disposition
	// "allow". file_read maps to "read", so the "read" pair must be "allow".
	d := loadPermissionShapeDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "file_write"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permissionPairs(t, result)

	for _, p := range pairs {
		if p.Key == "read" {
			if p.Value.Kind != domain.KindScalar {
				t.Errorf("\"read\" pair value kind: want KindScalar, got %q", p.Value.Kind)
			}
			if p.Value.Scalar != string(domain.Allow) {
				t.Errorf("\"read\" pair disposition: want %q (resolved via file_read mapping), got %q",
					domain.Allow, p.Value.Scalar)
			}
			return
		}
	}
	t.Errorf("\"read\" key not found in Pairs; got pairs: %v", pairs)
}

func TestMapTools_PermissionShape_UnmappedToolGetsUnusedDisposition(t *testing.T) {
	// A Universe tool that is NOT in the resolved set must receive its HarnessTool.Unused
	// disposition. "execute" has no mapping entry and unused: deny in the fixture, so its
	// pair value must be "deny".
	d := loadPermissionShapeDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "file_write"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permissionPairs(t, result)

	for _, p := range pairs {
		if p.Key == "execute" {
			if p.Value.Kind != domain.KindScalar {
				t.Errorf("\"execute\" pair value kind: want KindScalar, got %q", p.Value.Kind)
			}
			if p.Value.Scalar != string(domain.Deny) {
				t.Errorf("\"execute\" pair disposition: want %q (HarnessTool.Unused from fixture), got %q",
					domain.Deny, p.Value.Scalar)
			}
			return
		}
	}
	t.Errorf("\"execute\" key not found in Pairs; got pairs: %v", pairs)
}

func TestMapTools_PermissionShape_PairsAreInUniverseOrder(t *testing.T) {
	// Pairs must follow the Universe declaration order so output is deterministic.
	// Fixture Universe order: "read" (0), "write" (1), "execute" (2).
	d := loadPermissionShapeDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "file_write"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	pairs := permissionPairs(t, result)

	if len(pairs) < 3 {
		t.Fatalf("expected at least 3 pairs, got %d", len(pairs))
	}
	wantOrder := []string{"read", "write", "execute"}
	for i, want := range wantOrder {
		if pairs[i].Key != want {
			t.Errorf("Pairs[%d].Key: want %q (Universe order), got %q", i, want, pairs[i].Key)
		}
	}
}

func TestMapTools_PermissionShape_AllUniverseToolsAppearsEvenWithEmptyGenericList(t *testing.T) {
	// Even when Generic is empty, all Universe tools must appear in the KindMapping output
	// with their Unused disposition. The permission shape always emits the full tool matrix.
	d := loadPermissionShapeDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools with empty Generic list: %v", err)
	}
	pairs := permissionPairs(t, result)

	// All three Universe tools must still appear with "deny" (their Unused disposition).
	wantKeys := []string{"read", "write", "execute"}
	pairIndex := make(map[string]string, len(pairs))
	for _, p := range pairs {
		pairIndex[p.Key] = p.Value.Scalar
	}
	for _, key := range wantKeys {
		disposition, ok := pairIndex[key]
		if !ok {
			t.Errorf("Universe tool %q missing from Pairs with empty Generic list", key)
			continue
		}
		if disposition != string(domain.Deny) {
			t.Errorf("Universe tool %q disposition with no resolved tools: want %q, got %q",
				key, domain.Deny, disposition)
		}
	}
}

// --- by_convention: Universe tools emitted for every agent regardless of generic tools ---

// byConventionDescriptorYAML declares a list-shape harness with one by-convention tool
// ("search/listDirectory") and one normally mapped tool ("read/readFile"). The by-convention
// tool must appear in Fields output for every agent, even when the agent's generic tools
// list contains no mapping that resolves to it.
const byConventionDescriptorYAML = `schema_version: "1"
id: "by-convention-harness"
display_name: "By Convention Harness"
tools:
  shape: list
  universe:
    - name: "search/listDirectory"
      unused: deny
      by_convention: true
    - name: "read/readFile"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      destinations:
        - to: main
          names:
            - "read/readFile"
frontmatter:
  tools_key: "tools"
`

func loadByConventionDescriptor(t *testing.T) *domain.HarnessDescriptor {
	t.Helper()
	d, err := descriptor.Parse([]byte(byConventionDescriptorYAML), "inline:by-convention-harness")
	if err != nil {
		t.Fatalf("Parse by-convention descriptor: %v", err)
	}
	if d == nil {
		t.Fatal("Parse returned nil descriptor without error")
	}
	return d
}

// TestMapTools_ByConvention_AppearsInFieldsWithNoMatchingGenericTool asserts that a
// Universe tool marked by_convention: true appears in the Fields tools list even when
// none of the agent's generic tools resolve to it. By-convention tools are unconditionally
// emitted; the harness uses them for tools that must appear in every agent output (e.g.
// "search/listDirectory" in the VS Code GHCP harness).
//
// This test will be RED until buildListToolFields is updated to include by_convention tools
// unconditionally alongside the tools resolved from the generic tool list.
func TestMapTools_ByConvention_AppearsInFieldsWithNoMatchingGenericTool(t *testing.T) {
	d := loadByConventionDescriptor(t)
	// file_read maps to "read/readFile" only; no generic tool maps to "search/listDirectory".
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	var toolsField *domain.FrontmatterField
	for i := range result.Fields {
		if result.Fields[i].Key == "tools" {
			toolsField = &result.Fields[i]
			break
		}
	}
	if toolsField == nil {
		keys := make([]string, len(result.Fields))
		for i, f := range result.Fields {
			keys[i] = f.Key
		}
		t.Fatalf("tools field absent from Fields; got keys: %v", keys)
	}
	if toolsField.Value.Kind != domain.KindList {
		t.Fatalf("tools field value kind: want %q, got %q", domain.KindList, toolsField.Value.Kind)
	}
	var found bool
	for _, item := range toolsField.Value.Items {
		if item.Scalar == "search/listDirectory" {
			found = true
			break
		}
	}
	if !found {
		scalars := make([]string, len(toolsField.Value.Items))
		for i, item := range toolsField.Value.Items {
			scalars[i] = item.Scalar
		}
		t.Errorf("by-convention tool %q absent from tools field; by_convention: true tools must appear "+
			"in every agent's output regardless of generic tool mappings; got: %v",
			"search/listDirectory", scalars)
	}
}

// permissionPairs is a test helper that extracts the Pairs slice from the "permission" field
// in result.Fields. It fails the test immediately if the field or its KindMapping value is absent.
func permissionPairs(t *testing.T, result domain.ToolResult) []domain.FieldPair {
	t.Helper()
	for i := range result.Fields {
		if result.Fields[i].Key == "permission" {
			f := &result.Fields[i]
			if f.Value.Kind != domain.KindMapping {
				t.Fatalf("\"permission\" field value kind: want %q, got %q", domain.KindMapping, f.Value.Kind)
			}
			return f.Value.Pairs
		}
	}
	t.Fatalf("no FrontmatterField with Key=\"permission\" found in Fields (len=%d)", len(result.Fields))
	return nil
}

// =============================================================================
// ExtractToolEntries tests (T3.1)
//
// Coverage:
//   - Permission-shaped harness: only entries with disposition "allow" are returned;
//     entries with deny or other dispositions are excluded.
//   - Permission-shaped harness: by-convention tools are excluded even when their
//     disposition is "allow", because the forward direction emits them unconditionally.
//   - List-shaped harness: every scalar item is returned (no disposition filter).
//   - List-shaped harness: by-convention tools are excluded regardless.
//   - A zero FieldValue (absent tools key) returns an empty slice without error.
//   - A descriptor with an empty ToolsKey returns an empty slice.
//   - Duplicate entries in a list-shaped value are deduplicated first-seen.
//   - Document order of the input is preserved in the returned slice.
// =============================================================================

// permExtractDescriptorYAML declares a permission-shaped harness with:
//   - "read/readFile"  — allow disposition, not by-convention (expected to be returned)
//   - "write/createFile" — allow disposition, not by-convention (expected to be returned)
//   - "list"           — allow disposition, by_convention: true  (expected to be excluded)
//   - "execute"        — deny disposition, not by-convention    (expected to be excluded)
const permExtractDescriptorYAML = `schema_version: "1"
id: "perm-extract-harness"
display_name: "Permission Extraction Test Harness"
tools:
  shape: permission
  universe:
    - name: "read/readFile"
      unused: deny
      by_convention: false
    - name: "write/createFile"
      unused: deny
      by_convention: false
    - name: "list"
      unused: allow
      by_convention: true
    - name: "execute"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      destinations:
        - to: main
          names:
            - "read/readFile"
    - generic: "file_write"
      destinations:
        - to: main
          names:
            - "write/createFile"
frontmatter:
  tools_key: "permission"
`

// listExtractDescriptorYAML declares a list-shaped harness with:
//   - "read/readFile"         — not by-convention (expected to be returned)
//   - "write/editFile"        — not by-convention (expected to be returned)
//   - "search/listDirectory"  — by_convention: true (expected to be excluded)
const listExtractDescriptorYAML = `schema_version: "1"
id: "list-extract-harness"
display_name: "List Extraction Test Harness"
tools:
  shape: list
  universe:
    - name: "read/readFile"
      unused: deny
      by_convention: false
    - name: "write/editFile"
      unused: deny
      by_convention: false
    - name: "search/listDirectory"
      unused: deny
      by_convention: true
  mappings:
    - generic: "file_read"
      destinations:
        - to: main
          names:
            - "read/readFile"
    - generic: "file_write"
      destinations:
        - to: main
          names:
            - "write/editFile"
frontmatter:
  tools_key: "tools"
`

// noToolsKeyDescriptorYAML declares a descriptor with no tools_key, so ExtractToolEntries
// must return an empty slice without error.
const noToolsKeyDescriptorYAML = `schema_version: "1"
id: "no-tools-key-harness"
display_name: "No Tools Key Harness"
tools:
  shape: list
  universe:
    - name: "read/readFile"
      unused: deny
      by_convention: false
frontmatter:
  tools_key: ""
`

func loadPermExtractDescriptor(t *testing.T) *domain.HarnessDescriptor {
	t.Helper()
	d, err := descriptor.Parse([]byte(permExtractDescriptorYAML), "inline:perm-extract-harness")
	if err != nil {
		t.Fatalf("Parse perm-extract descriptor: %v", err)
	}
	return d
}

func loadListExtractDescriptor(t *testing.T) *domain.HarnessDescriptor {
	t.Helper()
	d, err := descriptor.Parse([]byte(listExtractDescriptorYAML), "inline:list-extract-harness")
	if err != nil {
		t.Fatalf("Parse list-extract descriptor: %v", err)
	}
	return d
}

// buildPermValue constructs a KindMapping FieldValue representing a deployed agent's
// permission block. Each pair is (toolName, disposition) where disposition is "allow"
// or "deny".
func buildPermValue(pairs ...struct{ name, disposition string }) domain.FieldValue {
	fp := make([]domain.FieldPair, len(pairs))
	for i, p := range pairs {
		fp[i] = domain.FieldPair{
			Key:   p.name,
			Value: domain.ScalarValue(p.disposition, domain.QuotePlain),
		}
	}
	return domain.MappingValue(fp)
}

// buildListValue constructs a KindList FieldValue representing a deployed agent's tools list.
func buildListValue(names ...string) domain.FieldValue {
	items := make([]domain.FieldValue, len(names))
	for i, n := range names {
		items[i] = domain.ScalarValue(n, domain.QuotePlain)
	}
	return domain.ListValue(items, domain.ListBlock)
}

// --- Permission shape: only allow entries returned ---

func TestExtractToolEntries_PermissionShape_AllowEntriesReturned(t *testing.T) {
	// Allow entries that are not by-convention must appear in the returned slice.
	d := loadPermExtractDescriptor(t)
	toolsValue := buildPermValue(
		struct{ name, disposition string }{"read/readFile", "allow"},
		struct{ name, disposition string }{"write/createFile", "allow"},
		struct{ name, disposition string }{"list", "allow"},
		struct{ name, disposition string }{"execute", "deny"},
	)

	got := descriptor.ExtractToolEntries(d, toolsValue)

	// "read/readFile" and "write/createFile" are allow and not by-convention.
	for _, want := range []string{"read/readFile", "write/createFile"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ExtractToolEntries: allow entry %q absent from result %v", want, got)
		}
	}
}

func TestExtractToolEntries_PermissionShape_DenyEntriesExcluded(t *testing.T) {
	// Deny entries represent tools the agent does not use; they must not appear.
	d := loadPermExtractDescriptor(t)
	toolsValue := buildPermValue(
		struct{ name, disposition string }{"read/readFile", "allow"},
		struct{ name, disposition string }{"execute", "deny"},
	)

	got := descriptor.ExtractToolEntries(d, toolsValue)

	for _, g := range got {
		if g == "execute" {
			t.Errorf("ExtractToolEntries: deny entry %q must not appear in result, but got %v", "execute", got)
		}
	}
}

func TestExtractToolEntries_PermissionShape_ByConventionExcluded(t *testing.T) {
	// By-convention entries carry allow disposition unconditionally in the forward direction;
	// their presence in a deployed file carries no information about the agent's generic tools.
	// They must be excluded regardless of disposition.
	d := loadPermExtractDescriptor(t)
	toolsValue := buildPermValue(
		struct{ name, disposition string }{"read/readFile", "allow"},
		struct{ name, disposition string }{"list", "allow"}, // list is by_convention: true
	)

	got := descriptor.ExtractToolEntries(d, toolsValue)

	for _, g := range got {
		if g == "list" {
			t.Errorf("ExtractToolEntries: by-convention entry %q must not appear in result, but got %v", "list", got)
		}
	}
}

func TestExtractToolEntries_PermissionShape_ExactResultCount(t *testing.T) {
	// With one allow+not-by-convention, one allow+by-convention, and one deny: exactly
	// one entry should be returned.
	d := loadPermExtractDescriptor(t)
	toolsValue := buildPermValue(
		struct{ name, disposition string }{"read/readFile", "allow"},
		struct{ name, disposition string }{"list", "allow"},
		struct{ name, disposition string }{"execute", "deny"},
	)

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) != 1 {
		t.Errorf("ExtractToolEntries: want 1 entry (only read/readFile), got %d: %v", len(got), got)
	}
	if len(got) == 1 && got[0] != "read/readFile" {
		t.Errorf("ExtractToolEntries: want [read/readFile], got %v", got)
	}
}

// --- List shape: every entry returned (except by-convention) ---

func TestExtractToolEntries_ListShape_NonByConventionEntriesReturned(t *testing.T) {
	// A list-shaped harness contributes every listed entry that is not by-convention.
	d := loadListExtractDescriptor(t)
	toolsValue := buildListValue("read/readFile", "write/editFile", "search/listDirectory")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	for _, want := range []string{"read/readFile", "write/editFile"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ExtractToolEntries list shape: entry %q absent from result %v", want, got)
		}
	}
}

func TestExtractToolEntries_ListShape_ByConventionExcluded(t *testing.T) {
	// search/listDirectory is by_convention: true in the list-extract descriptor.
	// It must be excluded even though it appears in the list.
	d := loadListExtractDescriptor(t)
	toolsValue := buildListValue("read/readFile", "search/listDirectory", "write/editFile")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	for _, g := range got {
		if g == "search/listDirectory" {
			t.Errorf("ExtractToolEntries list shape: by-convention entry %q must not appear in result %v",
				"search/listDirectory", got)
		}
	}
}

func TestExtractToolEntries_ListShape_ExactResultCount(t *testing.T) {
	// With two non-by-convention entries and one by-convention, exactly two entries returned.
	d := loadListExtractDescriptor(t)
	toolsValue := buildListValue("read/readFile", "write/editFile", "search/listDirectory")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) != 2 {
		t.Errorf("ExtractToolEntries list shape: want 2 entries, got %d: %v", len(got), got)
	}
}

// --- Absent or zero FieldValue ---

func TestExtractToolEntries_ZeroFieldValue_ReturnsEmpty(t *testing.T) {
	// A zero FieldValue represents an absent tools key. No entries can be extracted.
	d := loadListExtractDescriptor(t)
	var toolsValue domain.FieldValue // zero value

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) != 0 {
		t.Errorf("ExtractToolEntries with zero FieldValue: want empty, got %v", got)
	}
}

func TestExtractToolEntries_ZeroFieldValue_ReturnsNonNilSlice(t *testing.T) {
	// A nil return would be acceptable (design says "empty slice — never an error"),
	// but callers should be able to range safely; len(nil) == 0 so nil is fine here.
	// This test only verifies the empty guarantee (not nil vs empty distinction).
	d := loadListExtractDescriptor(t)
	var toolsValue domain.FieldValue

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if got != nil && len(got) != 0 {
		t.Errorf("ExtractToolEntries with zero FieldValue: want empty, got %v", got)
	}
}

func TestExtractToolEntries_EmptyToolsKey_ReturnsEmpty(t *testing.T) {
	// A descriptor with an empty ToolsKey declares no tools field; no entries can exist.
	d, err := descriptor.Parse([]byte(noToolsKeyDescriptorYAML), "inline:no-tools-key-harness")
	if err != nil {
		t.Fatalf("Parse no-tools-key descriptor: %v", err)
	}
	toolsValue := buildListValue("read/readFile")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) != 0 {
		t.Errorf("ExtractToolEntries with empty ToolsKey: want empty, got %v", got)
	}
}

// --- Document order and deduplication ---

func TestExtractToolEntries_ListShape_DocumentOrderMaintained(t *testing.T) {
	// The returned slice must preserve the document order of the input list, not sort
	// or reorder the entries. This pins the "extraction order" guarantee that the service
	// layer relies on for stable question ordering.
	d := loadListExtractDescriptor(t)
	toolsValue := buildListValue("write/editFile", "read/readFile") // write before read

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) != 2 {
		t.Fatalf("ExtractToolEntries: want 2 entries, got %d: %v", len(got), got)
	}
	if got[0] != "write/editFile" {
		t.Errorf("ExtractToolEntries: [0] want %q (document order), got %q", "write/editFile", got[0])
	}
	if got[1] != "read/readFile" {
		t.Errorf("ExtractToolEntries: [1] want %q (document order), got %q", "read/readFile", got[1])
	}
}

func TestExtractToolEntries_ListShape_DeduplicatedFirstSeen(t *testing.T) {
	// Duplicate entries in the input are collapsed to the first occurrence. The deduplication
	// must be first-seen so the output order is deterministic.
	d := loadListExtractDescriptor(t)
	toolsValue := buildListValue("read/readFile", "write/editFile", "read/readFile")

	got := descriptor.ExtractToolEntries(d, toolsValue)

	if len(got) != 2 {
		t.Errorf("ExtractToolEntries deduplication: want 2 entries (first-seen), got %d: %v", len(got), got)
	}
	if len(got) >= 1 && got[0] != "read/readFile" {
		t.Errorf("ExtractToolEntries deduplication: got[0] want %q, got %q", "read/readFile", got[0])
	}
	if len(got) >= 2 && got[1] != "write/editFile" {
		t.Errorf("ExtractToolEntries deduplication: got[1] want %q, got %q", "write/editFile", got[1])
	}
}

// =============================================================================
// ReverseMapTool and ReverseMapTools tests (T3.2)
//
// Coverage:
//   - A harness tool named by exactly one mapping resolves to that mapping's generic name.
//   - Many-to-one: multiple generics mapping to the same harness name → first mapping in
//     declaration order wins (AD-5), so the result is deterministic.
//   - One-to-many: one generic maps to multiple harness names → each harness name resolves
//     to the same generic independently (the result is the same generic for both).
//   - An unmapped harness entry returns ok=false and an empty generic name.
//   - A harness with nil or empty Mappings returns ok=false for every input.
//   - ReverseMapTools returns one resolution per input entry in the same order.
//   - ReverseMapTools sets Outcome=ToolMapped for resolved entries and ToolUnmapped otherwise.
//   - ReverseMapTools preserves the Harness field in every resolution.
//   - ReverseMapTools with an empty input returns an empty (or nil) slice.
//   - ReverseMapTools with a descriptor carrying no mapping data → all ToolUnmapped.
// =============================================================================

// noMappingsDescriptorYAML declares a descriptor with no mappings at all.
// ReverseMapTool must return ok=false for any harness name against this descriptor.
const noMappingsDescriptorYAML = `schema_version: "1"
id: "no-mappings-harness"
display_name: "No Mappings Harness"
tools:
  shape: list
  universe:
    - name: "read/readFile"
      unused: deny
      by_convention: false
frontmatter:
  tools_key: "tools"
`

func loadNoMappingsDescriptor(t *testing.T) *domain.HarnessDescriptor {
	t.Helper()
	d, err := descriptor.Parse([]byte(noMappingsDescriptorYAML), "inline:no-mappings-harness")
	if err != nil {
		t.Fatalf("Parse no-mappings descriptor: %v", err)
	}
	return d
}

// --- ReverseMapTool: basic resolution ---

func TestReverseMapTool_MappedEntry_OkIsTrue(t *testing.T) {
	// A harness tool named in exactly one mapping must be resolved; ok must be true.
	d := loadMappingDescriptor(t) // file_read → read/readFile
	_, ok := descriptor.ReverseMapTool(d, "read/readFile")
	if !ok {
		t.Error("ReverseMapTool: ok must be true for a mapped harness entry; got false")
	}
}

func TestReverseMapTool_MappedEntry_ReturnsCorrectGenericName(t *testing.T) {
	// file_read maps to read/readFile in the mapping descriptor. The reverse must return
	// "file_read" when given "read/readFile".
	d := loadMappingDescriptor(t)
	got, ok := descriptor.ReverseMapTool(d, "read/readFile")
	if !ok {
		t.Fatalf("ReverseMapTool(read/readFile): ok=false; expected a mapped result")
	}
	if got != "file_read" {
		t.Errorf("ReverseMapTool(read/readFile): want %q, got %q", "file_read", got)
	}
}

// --- ReverseMapTool: unmapped entry ---

func TestReverseMapTool_UnmappedEntry_OkIsFalse(t *testing.T) {
	// An entry that appears in no mapping must return ok=false.
	// "read/readFile" does not appear in the no-mappings descriptor.
	d := loadNoMappingsDescriptor(t)
	_, ok := descriptor.ReverseMapTool(d, "read/readFile")
	if ok {
		t.Error("ReverseMapTool with no mappings: ok must be false; got true")
	}
}

func TestReverseMapTool_UnmappedEntry_GenericIsEmpty(t *testing.T) {
	// An unmapped entry must return an empty generic name — never an invented name.
	d := loadNoMappingsDescriptor(t)
	got, _ := descriptor.ReverseMapTool(d, "read/readFile")
	if got != "" {
		t.Errorf("ReverseMapTool unmapped: generic must be empty, got %q", got)
	}
}

func TestReverseMapTool_EntryAbsentFromMappings_OkIsFalse(t *testing.T) {
	// "terminal" appears in the mapping descriptor's Universe but not in any mapping.
	// It is genuinely unmapped: ok must be false.
	d := loadMappingDescriptor(t)
	_, ok := descriptor.ReverseMapTool(d, "terminal")
	if ok {
		t.Error("ReverseMapTool(terminal): ok must be false for a harness tool absent from all mappings")
	}
}

// --- ReverseMapTool: many-to-one (AD-5 — first mapping wins) ---

func TestReverseMapTool_ManyToOne_FirstMappingDeclarationOrderWins(t *testing.T) {
	// file_search and content_search both map to search/textSearch in the mapping descriptor,
	// with file_search declared first. ReverseMapTool must resolve search/textSearch to
	// "file_search" (the first-declared mapping), not "content_search".
	d := loadMappingDescriptor(t)
	got, ok := descriptor.ReverseMapTool(d, "search/textSearch")
	if !ok {
		t.Fatal("ReverseMapTool(search/textSearch): ok=false; expected mapped result")
	}
	if got != "file_search" {
		t.Errorf("ReverseMapTool many-to-one: want %q (first declaration), got %q", "file_search", got)
	}
}

func TestReverseMapTool_ManyToOne_ResultIsDeterministic(t *testing.T) {
	// Repeated calls with the same input must produce the same result.
	d := loadMappingDescriptor(t)
	got1, ok1 := descriptor.ReverseMapTool(d, "search/textSearch")
	got2, ok2 := descriptor.ReverseMapTool(d, "search/textSearch")
	if ok1 != ok2 || got1 != got2 {
		t.Errorf("ReverseMapTool many-to-one is non-deterministic: call1=(%q,%v) call2=(%q,%v)",
			got1, ok1, got2, ok2)
	}
}

// --- ReverseMapTool: one-to-many (file_write → write/createFile and write/editFile) ---

func TestReverseMapTool_OneToMany_FirstHarnessToolResolvesToGeneric(t *testing.T) {
	// file_write maps to both write/createFile and write/editFile. Each harness tool must
	// independently resolve to "file_write".
	d := loadMappingDescriptor(t)
	got, ok := descriptor.ReverseMapTool(d, "write/createFile")
	if !ok {
		t.Fatal("ReverseMapTool(write/createFile): ok=false; expected mapped result")
	}
	if got != "file_write" {
		t.Errorf("ReverseMapTool(write/createFile): want %q, got %q", "file_write", got)
	}
}

func TestReverseMapTool_OneToMany_SecondHarnessToolResolvesToSameGeneric(t *testing.T) {
	d := loadMappingDescriptor(t)
	got, ok := descriptor.ReverseMapTool(d, "write/editFile")
	if !ok {
		t.Fatal("ReverseMapTool(write/editFile): ok=false; expected mapped result")
	}
	if got != "file_write" {
		t.Errorf("ReverseMapTool(write/editFile): want %q, got %q", "file_write", got)
	}
}

// --- ReverseMapTool: no mapping data ---

func TestReverseMapTool_NoMappingData_OkIsFalse(t *testing.T) {
	// A descriptor with no mappings cannot resolve any harness tool; ok must always be false.
	d := loadNoMappingsDescriptor(t)
	_, ok := descriptor.ReverseMapTool(d, "read/readFile")
	if ok {
		t.Error("ReverseMapTool with empty mappings: ok must be false; got true")
	}
}

// --- ReverseMapTools: sequence, ordering, and outcome fields ---

func TestReverseMapTools_EmptyInput_ReturnsEmptyOrNilSlice(t *testing.T) {
	// An empty input slice must produce an empty (or nil) result; never a non-empty one.
	d := loadMappingDescriptor(t)
	got := descriptor.ReverseMapTools(d, []string{})
	if len(got) != 0 {
		t.Errorf("ReverseMapTools with empty input: want empty, got %d resolutions: %v", len(got), got)
	}
}

func TestReverseMapTools_CountMatchesInputCount(t *testing.T) {
	// ReverseMapTools returns exactly one resolution per input entry, matching
	// the contract stated in the design: "one ReverseToolResolution per input entry".
	d := loadMappingDescriptor(t)
	inputs := []string{"read/readFile", "search/textSearch", "terminal"}

	got := descriptor.ReverseMapTools(d, inputs)

	if len(got) != len(inputs) {
		t.Errorf("ReverseMapTools: want %d resolutions (one per input), got %d: %v",
			len(inputs), len(got), got)
	}
}

func TestReverseMapTools_PreservesInputOrder(t *testing.T) {
	// The resolution at index i must correspond to inputs[i]. Input order is preserved so
	// the service layer can prompt in a stable, predictable sequence.
	d := loadMappingDescriptor(t)
	inputs := []string{"read/readFile", "search/textSearch", "terminal"}

	got := descriptor.ReverseMapTools(d, inputs)

	if len(got) != 3 {
		t.Fatalf("ReverseMapTools: want 3 resolutions, got %d", len(got))
	}
	for i, input := range inputs {
		if got[i].Harness != input {
			t.Errorf("ReverseMapTools: got[%d].Harness = %q, want %q (input order not preserved)",
				i, got[i].Harness, input)
		}
	}
}

func TestReverseMapTools_MappedEntry_OutcomeIsToolMapped(t *testing.T) {
	// A harness tool that resolves to a generic name must have Outcome = ToolMapped.
	d := loadMappingDescriptor(t)
	got := descriptor.ReverseMapTools(d, []string{"read/readFile"})
	if len(got) != 1 {
		t.Fatalf("ReverseMapTools: want 1 resolution, got %d", len(got))
	}
	if got[0].Outcome != domain.ToolMapped {
		t.Errorf("ReverseMapTools mapped entry Outcome: want %q, got %q", domain.ToolMapped, got[0].Outcome)
	}
}

func TestReverseMapTools_MappedEntry_GenericFieldIsSet(t *testing.T) {
	// The Generic field of a resolved entry must hold the generic tool name.
	d := loadMappingDescriptor(t)
	got := descriptor.ReverseMapTools(d, []string{"read/readFile"})
	if len(got) != 1 {
		t.Fatalf("ReverseMapTools: want 1 resolution, got %d", len(got))
	}
	if got[0].Generic != "file_read" {
		t.Errorf("ReverseMapTools mapped entry Generic: want %q, got %q", "file_read", got[0].Generic)
	}
}

func TestReverseMapTools_UnmappedEntry_OutcomeIsToolUnmapped(t *testing.T) {
	// An entry absent from all mappings must have Outcome = ToolUnmapped.
	d := loadMappingDescriptor(t)
	got := descriptor.ReverseMapTools(d, []string{"terminal"})
	if len(got) != 1 {
		t.Fatalf("ReverseMapTools: want 1 resolution, got %d", len(got))
	}
	if got[0].Outcome != domain.ToolUnmapped {
		t.Errorf("ReverseMapTools unmapped entry Outcome: want %q, got %q", domain.ToolUnmapped, got[0].Outcome)
	}
}

func TestReverseMapTools_UnmappedEntry_GenericIsEmpty(t *testing.T) {
	// The Generic field of an unmapped entry must be empty — no invented name.
	d := loadMappingDescriptor(t)
	got := descriptor.ReverseMapTools(d, []string{"terminal"})
	if len(got) != 1 {
		t.Fatalf("ReverseMapTools: want 1 resolution, got %d", len(got))
	}
	if got[0].Generic != "" {
		t.Errorf("ReverseMapTools unmapped entry Generic: want empty, got %q", got[0].Generic)
	}
}

func TestReverseMapTools_HarnessFieldMatchesInput(t *testing.T) {
	// The Harness field of every resolution must equal the corresponding input entry.
	d := loadMappingDescriptor(t)
	inputs := []string{"read/readFile", "terminal"}
	got := descriptor.ReverseMapTools(d, inputs)
	if len(got) != 2 {
		t.Fatalf("ReverseMapTools: want 2 resolutions, got %d", len(got))
	}
	for i, input := range inputs {
		if got[i].Harness != input {
			t.Errorf("ReverseMapTools: got[%d].Harness = %q, want %q", i, got[i].Harness, input)
		}
	}
}

func TestReverseMapTools_MixedInput_CorrectOutcomesPerEntry(t *testing.T) {
	// A mixed input containing both mapped and unmapped entries must produce the correct
	// outcome for each: mapped entries get ToolMapped, unmapped get ToolUnmapped.
	d := loadMappingDescriptor(t)
	// read/readFile: mapped (file_read); terminal: unmapped.
	got := descriptor.ReverseMapTools(d, []string{"read/readFile", "terminal"})
	if len(got) != 2 {
		t.Fatalf("ReverseMapTools: want 2 resolutions, got %d", len(got))
	}
	if got[0].Outcome != domain.ToolMapped {
		t.Errorf("ReverseMapTools mixed: got[0] (read/readFile) Outcome want %q, got %q",
			domain.ToolMapped, got[0].Outcome)
	}
	if got[1].Outcome != domain.ToolUnmapped {
		t.Errorf("ReverseMapTools mixed: got[1] (terminal) Outcome want %q, got %q",
			domain.ToolUnmapped, got[1].Outcome)
	}
}

func TestReverseMapTools_NoMappingData_AllOutcomesAreToolUnmapped(t *testing.T) {
	// A harness declaring no mapping data cannot resolve any tool. Every entry must be
	// ToolUnmapped — the implementation must not guess or return ok=true for any entry.
	d := loadNoMappingsDescriptor(t)
	inputs := []string{"read/readFile", "write/editFile"}
	got := descriptor.ReverseMapTools(d, inputs)
	if len(got) != len(inputs) {
		t.Fatalf("ReverseMapTools no-mapping-data: want %d resolutions, got %d", len(inputs), len(got))
	}
	for i, res := range got {
		if res.Outcome != domain.ToolUnmapped {
			t.Errorf("ReverseMapTools no-mapping-data: got[%d] (%q) Outcome want %q, got %q",
				i, inputs[i], domain.ToolUnmapped, res.Outcome)
		}
		if res.Generic != "" {
			t.Errorf("ReverseMapTools no-mapping-data: got[%d] (%q) Generic must be empty, got %q",
				i, inputs[i], res.Generic)
		}
	}
}
