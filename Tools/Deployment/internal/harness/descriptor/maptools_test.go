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
// It contains:
//   - file_write → ["write/createFile", "write/editFile"]  (one-to-many)
//   - file_search → ["search/textSearch"]                  (one half of many-to-one pair)
//   - content_search → ["search/textSearch"]               (other half of many-to-one pair)
//   - file_read → ["read/readFile"]                        (one-to-one, for baseline)
//   - user_interaction → []                                (explicitly unsupported, empty harness_tools)
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
      harness_tools:
        - "read/readFile"
    - generic: "file_write"
      harness_tools:
        - "write/createFile"
        - "write/editFile"
    - generic: "file_search"
      harness_tools:
        - "search/textSearch"
    - generic: "content_search"
      harness_tools:
        - "search/textSearch"
    - generic: "user_interaction"
      harness_tools: []
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
	// file_write maps to two harness tools in the fixture.
	wantTools := []string{"write/createFile", "write/editFile"}
	if len(res.HarnessTools) != len(wantTools) {
		t.Fatalf("HarnessTools count: want %d, got %d: %v", len(wantTools), len(res.HarnessTools), res.HarnessTools)
	}
	for i, want := range wantTools {
		if res.HarnessTools[i] != want {
			t.Errorf("HarnessTools[%d]: want %q, got %q", i, want, res.HarnessTools[i])
		}
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
	// user_interaction has a mapping entry with harness_tools: [] in the descriptor.
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
      harness_tools:
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
