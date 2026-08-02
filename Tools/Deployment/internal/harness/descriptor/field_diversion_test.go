package descriptor_test

// field_diversion_test.go tests two advanced MapTools behaviors at the descriptor layer:
//
// Field diversion: a generic tool whose ToolMapping declares a DestField destination
// must route its names to a separate FrontmatterField rather than appearing in the main tools
// value. MapTools must produce a FrontmatterField for each DestField destination key it
// encounters, and must NOT include the diverted harness tool names in the main tools list.
//
// Custom tool template: when ToolSpec.CustomToolTemplate is non-empty, a
// user-supplied MCP server name is formatted through the template before being stored
// in HarnessTools. The formatted name must appear in the Fields output for list-shape
// harnesses; the raw unformatted name must not.
//
// These tests use the multi-destination destinations: schema. They are RED until the
// descriptor loader (I2.2) and field builder (I2.3) are updated.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/descriptor"
)

// --- Fixtures ---

// fieldDiversionDescriptorYAML declares a list-shape harness with one standard mapping
// (file_read → DestMain ["read-file"]) and one field-destination mapping
// (skill → DestField "mcp_servers" ["mcp-skill-tool"]).
// Uses the multi-destination destinations: schema.
const fieldDiversionDescriptorYAML = `schema_version: "1"
id: "field-diversion-harness"
display_name: "Field Diversion Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      destinations:
        - to: main
          names:
            - "read-file"
    - generic: "skill"
      destinations:
        - to: field
          field: mcp_servers
          names:
            - "mcp-skill-tool"
frontmatter:
  tools_key: "tools"
`

func loadFieldDiversionDescriptor(t *testing.T) *domain.HarnessDescriptor {
	t.Helper()
	d, err := descriptor.Parse([]byte(fieldDiversionDescriptorYAML), "inline:field-diversion-harness")
	if err != nil {
		t.Fatalf("Parse field-diversion descriptor: %v", err)
	}
	if d == nil {
		t.Fatal("Parse returned nil descriptor without error")
	}
	return d
}

// customTemplateDescriptorYAML declares a harness whose custom_tool_template wraps any
// user-supplied MCP server name with the prefix "mcp:". A custom name "my-server" must
// therefore produce "mcp:my-server" in the harness tool output.
// Uses the multi-destination destinations: schema.
const customTemplateDescriptorYAML = `schema_version: "1"
id: "custom-template-harness"
display_name: "Custom Template Harness"
tools:
  shape: list
  universe:
    - name: "read-file"
      unused: deny
      by_convention: false
  mappings:
    - generic: "file_read"
      destinations:
        - to: main
          names:
            - "read-file"
  custom_tool_template: "mcp:%s"
frontmatter:
  tools_key: "tools"
`

func loadCustomTemplateDescriptor(t *testing.T) *domain.HarnessDescriptor {
	t.Helper()
	d, err := descriptor.Parse([]byte(customTemplateDescriptorYAML), "inline:custom-template-harness")
	if err != nil {
		t.Fatalf("Parse custom-template descriptor: %v", err)
	}
	if d == nil {
		t.Fatal("Parse returned nil descriptor without error")
	}
	return d
}

// --- Field diversion: DestField destination routes names to a separate frontmatter key ---

// TestMapTools_FieldDiversion_DiversionFieldPresentInFields asserts that MapTools
// returns a FrontmatterField whose Key matches the DestField destination's declared field
// name. Without this entry, the transform stage has no field to write the diverted tool
// into, and the tool is silently lost.
//
// This test is RED until buildListToolFields is updated to produce a separate
// FrontmatterField for each DestField destination.
func TestMapTools_FieldDiversion_DiversionFieldPresentInFields(t *testing.T) {
	d := loadFieldDiversionDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "skill"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	var found bool
	for _, f := range result.Fields {
		if f.Key == "mcp_servers" {
			found = true
			break
		}
	}
	if !found {
		keys := make([]string, len(result.Fields))
		for i, f := range result.Fields {
			keys[i] = f.Key
		}
		t.Errorf("expected a FrontmatterField with Key=%q for the field-diverted tool; got Keys: %v — "+
			"MapTools must produce a separate field entry for each diversion destination declared in the descriptor", "mcp_servers", keys)
	}
}

// TestMapTools_FieldDiversion_DiversionFieldContainsDiversionHarnessTool asserts
// that the separate diversion field carries the harness tool name declared in the
// mapping, not an empty value or the generic name.
//
// This test will be RED until the diversion field is produced.
func TestMapTools_FieldDiversion_DiversionFieldContainsDiversionHarnessTool(t *testing.T) {
	d := loadFieldDiversionDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"skill"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	var divField *domain.FrontmatterField
	for i := range result.Fields {
		if result.Fields[i].Key == "mcp_servers" {
			divField = &result.Fields[i]
			break
		}
	}
	if divField == nil {
		keys := make([]string, len(result.Fields))
		for i, f := range result.Fields {
			keys[i] = f.Key
		}
		t.Fatalf("mcp_servers field not found in Fields; got Keys: %v", keys)
	}
	if divField.Value.Kind != domain.KindList {
		t.Fatalf("mcp_servers field value kind: want %q, got %q", domain.KindList, divField.Value.Kind)
	}
	var found bool
	for _, item := range divField.Value.Items {
		if item.Scalar == "mcp-skill-tool" {
			found = true
			break
		}
	}
	if !found {
		scalars := make([]string, len(divField.Value.Items))
		for i, item := range divField.Value.Items {
			scalars[i] = item.Scalar
		}
		t.Errorf("harness tool %q not found in mcp_servers field; got: %v", "mcp-skill-tool", scalars)
	}
}

// TestMapTools_FieldDiversion_DiversionToolAbsentFromMainToolsField asserts that a
// field-diverted generic tool does NOT appear in the main tools field value. Allowing
// it into both fields would produce duplicate tool declarations across frontmatter keys.
func TestMapTools_FieldDiversion_DiversionToolAbsentFromMainToolsField(t *testing.T) {
	d := loadFieldDiversionDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "skill"},
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
		// No main tools field at all — the diverted tool cannot be in it.
		return
	}
	if toolsField.Value.Kind != domain.KindList {
		return
	}
	for _, item := range toolsField.Value.Items {
		if item.Scalar == "mcp-skill-tool" {
			t.Errorf("diverted harness tool %q found in main tools field; a field-diverted tool must not appear in the main tools value", "mcp-skill-tool")
		}
	}
}

// TestMapTools_FieldDiversion_ResolutionDestinationMatchesMappingDeclaration asserts that the
// ToolResolution for a field-destination tool carries a Destinations entry whose Field matches
// the descriptor's mapping declaration. Consumers of the resolution must be able to identify
// the destination key without re-reading the descriptor.
func TestMapTools_FieldDiversion_ResolutionDestinationMatchesMappingDeclaration(t *testing.T) {
	d := loadFieldDiversionDescriptor(t)
	req := domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"skill"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if len(res.Destinations) == 0 {
		t.Fatal("ToolResolution.Destinations must be non-empty for a field-destination mapping; " +
			"the resolution must record all destinations the generic tool resolved to")
	}
	// The skill mapping declares one DestField destination targeting "mcp_servers".
	dest := res.Destinations[0]
	if dest.Kind != domain.DestField {
		t.Errorf("Destinations[0].Kind: want %q, got %q", domain.DestField, dest.Kind)
	}
	if dest.Field != "mcp_servers" {
		t.Errorf("Destinations[0].Field: want %q (matching the mapping declaration), got %q", "mcp_servers", dest.Field)
	}
}

// --- Custom tool template: ToolSpec.CustomToolTemplate formats user-supplied names ---

// TestMapTools_CustomTemplate_NameIsFormattedThroughTemplate asserts that when
// CustomToolTemplate is non-empty, a user-supplied MCP server name is substituted into
// the template before being stored in HarnessTools. The raw user-supplied name must NOT
// appear verbatim; only the formatted form may appear.
func TestMapTools_CustomTemplate_NameIsFormattedThroughTemplate(t *testing.T) {
	d := loadCustomTemplateDescriptor(t)
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
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
		t.Fatalf("outcome: want %q, got %q", domain.ToolCustom, res.Outcome)
	}
	const want = "mcp:my-server"
	var found bool
	for _, ht := range res.HarnessTools {
		if ht == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("formatted tool name %q not found in HarnessTools; custom_tool_template must be applied; got: %v", want, res.HarnessTools)
	}
}

// TestMapTools_CustomTemplate_RawNameAbsentWhenTemplateApplied asserts that the
// raw user-supplied name does not appear in HarnessTools when a template is applied.
// Both the raw name and the formatted name appearing would indicate the template was
// applied in addition to, not instead of, the original name.
func TestMapTools_CustomTemplate_RawNameAbsentWhenTemplateApplied(t *testing.T) {
	d := loadCustomTemplateDescriptor(t)
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	if len(result.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(result.Resolutions))
	}
	for _, ht := range result.Resolutions[0].HarnessTools {
		if ht == "my-server" {
			t.Errorf("raw user-supplied name %q found in HarnessTools when custom_tool_template %q is set; only the formatted form %q must appear",
				"my-server", "mcp:%s", "mcp:my-server")
		}
	}
}

// TestMapTools_CustomTool_AppearsInToolsField asserts that a custom-named tool
// (ToolCustom outcome) appears in the Fields output for a list-shape harness. Without
// this the transform stage writes a tools list that omits the MCP server name entirely,
// making the deployment invalid for harnesses where every tool must be listed.
//
// This test will be RED until buildListToolFields handles ToolCustom resolutions in
// addition to ToolMapped resolutions.
func TestMapTools_CustomTool_AppearsInToolsField(t *testing.T) {
	// Use the custom-template descriptor but supply the custom name without relying on
	// template expansion so the assertion is purely about Field inclusion, not formatting.
	d := loadCustomTemplateDescriptor(t)
	req := domain.ToolRequest{
		AgentKey:    "test-agent",
		Generic:     []string{"terminal"},
		CustomNames: map[string]string{"terminal": "my-server"},
	}

	result, err := descriptor.MapTools(d, req)

	if err != nil {
		t.Fatalf("MapTools: %v", err)
	}
	// The formatted name is "mcp:my-server" per the template.
	wantName := "mcp:my-server"

	var toolsField *domain.FrontmatterField
	for i := range result.Fields {
		if result.Fields[i].Key == "tools" {
			toolsField = &result.Fields[i]
			break
		}
	}
	if toolsField == nil {
		t.Fatal("tools field absent from Fields; custom tools must appear in the main tools field for list-shape harnesses")
	}
	if toolsField.Value.Kind != domain.KindList {
		t.Fatalf("tools field value kind: want %q, got %q", domain.KindList, toolsField.Value.Kind)
	}
	var found bool
	for _, item := range toolsField.Value.Items {
		if item.Scalar == wantName {
			found = true
			break
		}
	}
	if !found {
		scalars := make([]string, len(toolsField.Value.Items))
		for i, item := range toolsField.Value.Items {
			scalars[i] = item.Scalar
		}
		t.Errorf("formatted custom tool name %q not found in tools field; ToolCustom resolutions must appear in the list output; got: %v", wantName, scalars)
	}
}
