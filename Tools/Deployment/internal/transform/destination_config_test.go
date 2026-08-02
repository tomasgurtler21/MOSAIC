package transform_test

// destination_config_test.go tests the transform pipeline for config-declared
// multi-destination tool mappings (T4.2).
//
// These tests verify that a single generic tool whose mapping carries BOTH a DestMain
// destination and a DestField destination simultaneously produces:
//   - The harness tool name(s) in the main tools frontmatter field (from DestMain)
//   - The declared name(s) in a separate, user-configured frontmatter field (from DestField)
//
// The DestField + format tests cover the three supported value formats:
//   - FormatListBlock (the default): emitted as a YAML block sequence
//   - FormatListFlow: emitted as a YAML flow sequence (inline bracket notation)
//   - FormatScalar: emitted as a comma-separated scalar string
//
// Note: descriptor.MapTools and buildListToolFields already implement multi-destination
// routing. These tests will be GREEN after the descriptor package Stage 3 work. They are
// written here as TDD specifications for the end-to-end observable behaviour: what must
// appear in the output document's frontmatter.

import (
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Inline descriptors for multi-destination mapping tests (T4.2)
// ---------------------------------------------------------------------------

// multiDestDescriptorYAML is a list-shape harness where "user_interaction" maps to BOTH
// a DestMain destination (adding AskUser to the main tools field) and a DestField
// destination (writing user-feedback to the mcp_servers frontmatter key with list-block
// format — the default when format is omitted).
//
// This simulates the effective mapping set after the ToolMappings hook (I4.1/I4.2) has
// merged user-local config onto the harness descriptor.
const multiDestDescriptorYAML = `schema_version: "1"
id: "multi-dest-harness"
display_name: "Multi-Destination Harness"
tools:
  shape: list
  universe:
    - name: "AskUser"
      unused: deny
      by_convention: false
    - name: "ReadFile"
      unused: deny
      by_convention: false
  mappings:
    - generic: "user_interaction"
      destinations:
        - to: main
          names:
            - "AskUser"
        - to: field
          field: "mcp_servers"
          names:
            - "user-feedback"
    - generic: "file_read"
      destinations:
        - to: main
          names:
            - "ReadFile"
frontmatter:
  tools_key: "tools"
`

// multiDestWithListFlowYAML is the same harness but with the DestField destination
// explicitly specifying list-flow format for the mcp_servers field.
const multiDestWithListFlowYAML = `schema_version: "1"
id: "multi-dest-flow-harness"
display_name: "Multi-Destination Flow Harness"
tools:
  shape: list
  universe:
    - name: "AskUser"
      unused: deny
      by_convention: false
  mappings:
    - generic: "user_interaction"
      destinations:
        - to: main
          names:
            - "AskUser"
        - to: field
          field: "mcp_servers"
          format: "list-flow"
          names:
            - "user-feedback"
            - "extra-server"
frontmatter:
  tools_key: "tools"
`

// multiDestWithScalarYAML is the same harness but with the DestField destination
// explicitly specifying scalar format (comma-separated string) for the mcp_servers field.
const multiDestWithScalarYAML = `schema_version: "1"
id: "multi-dest-scalar-harness"
display_name: "Multi-Destination Scalar Harness"
tools:
  shape: list
  universe:
    - name: "AskUser"
      unused: deny
      by_convention: false
  mappings:
    - generic: "user_interaction"
      destinations:
        - to: main
          names:
            - "AskUser"
        - to: field
          field: "mcp_servers"
          format: "scalar"
          separator: ", "
          names:
            - "user-feedback"
            - "extra-server"
frontmatter:
  tools_key: "tools"
`

// multiDestTwoFieldsYAML has two distinct DestField destinations for two separate
// generic tools, routing each to a different frontmatter key. This tests that multiple
// DestField destinations can coexist independently in the same output.
const multiDestTwoFieldsYAML = `schema_version: "1"
id: "multi-dest-two-fields-harness"
display_name: "Multi-Destination Two Fields Harness"
tools:
  shape: list
  universe:
    - name: "AskUser"
      unused: deny
      by_convention: false
    - name: "ExecuteCode"
      unused: deny
      by_convention: false
  mappings:
    - generic: "user_interaction"
      destinations:
        - to: main
          names:
            - "AskUser"
        - to: field
          field: "mcp_servers"
          names:
            - "user-feedback"
    - generic: "code_execution"
      destinations:
        - to: main
          names:
            - "ExecuteCode"
        - to: field
          field: "docker_images"
          names:
            - "code-runner"
frontmatter:
  tools_key: "tools"
`

// ---------------------------------------------------------------------------
// Source agent YAML skeletons
// ---------------------------------------------------------------------------

// userInteractionAndReadSource declares both user_interaction (multi-destination) and
// file_read (main-only) to test that multi-destination and single-destination tools
// coexist correctly in the same apply pass.
const userInteractionAndReadSource = `---
id: 100
version: 1.0.0
description: Multi-destination integration test agent
tools: [user_interaction, file_read]
---
Body.
`

// userInteractionOnlySource declares only user_interaction for focused tests of the
// multi-destination mapping without interference from a second tool.
const userInteractionOnlySource = `---
id: 101
version: 1.0.0
description: User interaction tool test agent
tools: [user_interaction]
---
Body.
`

// twoMultiDestSource declares both user_interaction and code_execution, each of which
// has multi-destination mappings (main + field), to test independent field emission.
const twoMultiDestSource = `---
id: 102
version: 1.0.0
description: Two multi-destination tools test agent
tools: [user_interaction, code_execution]
---
Body.
`

// ---------------------------------------------------------------------------
// T4.2 — Multi-destination mapping rendering at the transform level
// ---------------------------------------------------------------------------

// TestMultiDestMapping_MainDestinationPresentInMainToolsField verifies that when a generic
// tool's mapping includes a DestMain destination, the harness tool names from that
// destination appear in the main tools frontmatter field of the output document.
//
// This is the baseline assertion for multi-destination: the DestMain side must behave
// identically to a mapping that has only DestMain.
func TestMultiDestMapping_MainDestinationPresentInMainToolsField(t *testing.T) {
	mod := newDescriptorModule(t, multiDestDescriptorYAML, "inline:multi-dest")
	req := transform.Request{
		Source: []byte(userInteractionAndReadSource),
		Kind:   domain.ArtifactAgent,
		Key:    "multi-dest-main-test",
		Module: mod,
		Model:  domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	// AskUser must appear in the main tools field via the DestMain destination.
	items := listFieldScalars(t, doc, "tools")
	if items == nil {
		t.Fatal("tools field absent from output frontmatter; DestMain destination must contribute to the main tools field")
	}
	var found bool
	for _, item := range items {
		if item == "AskUser" {
			found = true
		}
	}
	if !found {
		t.Errorf("AskUser not found in tools field %v; DestMain destination must add the mapped harness tool name to the main tools field",
			items)
	}
}

// TestMultiDestMapping_FieldDestinationPresentAsSeparateFrontmatterField verifies that
// when a generic tool's mapping includes a DestField destination, the declared names from
// that destination appear in the named frontmatter key of the output document — separate
// from the main tools field.
//
// This is the primary assertion for multi-destination field emission.
func TestMultiDestMapping_FieldDestinationPresentAsSeparateFrontmatterField(t *testing.T) {
	mod := newDescriptorModule(t, multiDestDescriptorYAML, "inline:multi-dest")
	req := transform.Request{
		Source: []byte(userInteractionOnlySource),
		Kind:   domain.ArtifactAgent,
		Key:    "multi-dest-field-test",
		Module: mod,
		Model:  domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	// mcp_servers must appear as a separate field.
	v, ok := doc.Frontmatter().Get("mcp_servers")
	if !ok {
		t.Fatalf("mcp_servers field absent from output frontmatter; DestField destination must write its declared names to the named field; "+
			"frontmatter keys: %v", doc.Frontmatter().Keys())
	}
	if v.Kind != domain.KindList {
		t.Errorf("mcp_servers field value kind: want KindList (list-block is the default format), got %v", v.Kind)
	}
	if len(v.Items) == 0 {
		t.Fatal("mcp_servers field is empty; DestField destination must write the mapping's declared names")
	}
	var found bool
	for _, item := range v.Items {
		if item.Scalar == "user-feedback" {
			found = true
		}
	}
	if !found {
		t.Errorf("user-feedback not found in mcp_servers field; DestField destination must emit the names declared in the mapping; "+
			"got items: %v", v.Items)
	}
}

// TestMultiDestMapping_FieldDestinationNameDoesNotAppearInMainToolsField verifies that the
// names from a DestField destination are NOT added to the main tools field. A DestField
// destination routes exclusively to the named field; it must not duplicate into tools.
func TestMultiDestMapping_FieldDestinationNameDoesNotAppearInMainToolsField(t *testing.T) {
	mod := newDescriptorModule(t, multiDestDescriptorYAML, "inline:multi-dest")
	req := transform.Request{
		Source: []byte(userInteractionOnlySource),
		Kind:   domain.ArtifactAgent,
		Key:    "multi-dest-no-leak-test",
		Module: mod,
		Model:  domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	items := listFieldScalars(t, doc, "tools")
	for _, item := range items {
		if item == "user-feedback" {
			t.Errorf("DestField name %q found in main tools field; "+
				"DestField destination names must appear only in the named destination field, not in tools; "+
				"all tools items: %v", "user-feedback", items)
		}
	}
}

// TestMultiDestMapping_TwoDistinctFieldDestinations_BothFieldsPresent verifies that when
// two different generic tools each carry a DestField destination routing to different field
// names, both fields are independently populated in the output frontmatter.
func TestMultiDestMapping_TwoDistinctFieldDestinations_BothFieldsPresent(t *testing.T) {
	mod := newDescriptorModule(t, multiDestTwoFieldsYAML, "inline:multi-dest-two-fields")
	req := transform.Request{
		Source: []byte(twoMultiDestSource),
		Kind:   domain.ArtifactAgent,
		Key:    "two-dest-fields-test",
		Module: mod,
		Model:  domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	// Both field-destination keys must be present.
	mcpItems := listFieldScalars(t, doc, "mcp_servers")
	if mcpItems == nil {
		t.Errorf("mcp_servers field absent; user_interaction DestField destination must produce mcp_servers; "+
			"frontmatter keys: %v", doc.Frontmatter().Keys())
	}
	dockerItems := listFieldScalars(t, doc, "docker_images")
	if dockerItems == nil {
		t.Errorf("docker_images field absent; code_execution DestField destination must produce docker_images; "+
			"frontmatter keys: %v", doc.Frontmatter().Keys())
	}

	// Verify expected names in each field.
	if !containsItem(mcpItems, "user-feedback") {
		t.Errorf("user-feedback not found in mcp_servers: %v", mcpItems)
	}
	if !containsItem(dockerItems, "code-runner") {
		t.Errorf("code-runner not found in docker_images: %v", dockerItems)
	}
}

// TestMultiDestMapping_FieldDestinationListFlowFormat verifies that when the DestField
// destination specifies format: list-flow, the emitted frontmatter value uses inline
// (bracket) YAML notation rather than the default multi-line block sequence.
func TestMultiDestMapping_FieldDestinationListFlowFormat(t *testing.T) {
	mod := newDescriptorModule(t, multiDestWithListFlowYAML, "inline:multi-dest-flow")
	req := transform.Request{
		Source: []byte(userInteractionOnlySource),
		Kind:   domain.ArtifactAgent,
		Key:    "multi-dest-flow-test",
		Module: mod,
		Model:  domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The raw output bytes must use flow notation for mcp_servers.
	// Flow sequence renders as "[..., ...]" on a single line.
	outputStr := string(result.Output)
	if !strings.Contains(outputStr, "mcp_servers:") {
		t.Fatal("mcp_servers field absent from output; DestField destination must emit the named field")
	}
	// The field must appear somewhere in the frontmatter before the closing ---.
	frontmatterEnd := strings.Index(outputStr[3:], "---") // skip opening ---
	var frontmatter string
	if frontmatterEnd >= 0 {
		frontmatter = outputStr[3 : frontmatterEnd+3]
	} else {
		frontmatter = outputStr
	}
	mcpIdx := strings.Index(frontmatter, "mcp_servers:")
	if mcpIdx < 0 {
		t.Fatal("mcp_servers: not found in frontmatter")
	}
	mcpLine := frontmatter[mcpIdx:]
	// Flow notation uses "[" immediately after the key (on the same line or next).
	// Block notation uses "\n-" for each item. If the byte after mcp_servers: ... contains
	// "[" before any "\n- " pattern, it's flow.
	afterKey := mcpLine[len("mcp_servers:"):]
	blockIdx := strings.Index(afterKey, "\n-")
	flowIdx := strings.Index(afterKey, "[")
	if flowIdx < 0 || (blockIdx >= 0 && blockIdx < flowIdx) {
		t.Errorf("mcp_servers field does not use list-flow notation; "+
			"format: list-flow must produce a YAML flow sequence (inline brackets); "+
			"output around mcp_servers:\n%s", truncateStr(mcpLine, 200))
	}
}

// TestMultiDestMapping_FieldDestinationScalarFormat verifies that when the DestField
// destination specifies format: scalar, the emitted frontmatter value is a comma-separated
// scalar string rather than a YAML list.
func TestMultiDestMapping_FieldDestinationScalarFormat(t *testing.T) {
	mod := newDescriptorModule(t, multiDestWithScalarYAML, "inline:multi-dest-scalar")
	req := transform.Request{
		Source: []byte(userInteractionOnlySource),
		Kind:   domain.ArtifactAgent,
		Key:    "multi-dest-scalar-test",
		Module: mod,
		Model:  domain.ModelSelection{Origin: domain.OriginUnresolved},
		Scope:  domain.ScopeProject,
	}

	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("Parse output: %v", err)
	}

	v, ok := doc.Frontmatter().Get("mcp_servers")
	if !ok {
		t.Fatal("mcp_servers field absent; DestField destination must emit the named field")
	}
	if v.Kind != domain.KindScalar {
		t.Errorf("mcp_servers field value kind: want KindScalar (format: scalar must produce a scalar value), got %v", v.Kind)
	}
	// The scalar value must contain both names joined by the separator.
	if !strings.Contains(v.Scalar, "user-feedback") {
		t.Errorf("mcp_servers scalar value %q does not contain %q; all DestField names must appear in the scalar",
			v.Scalar, "user-feedback")
	}
	if !strings.Contains(v.Scalar, "extra-server") {
		t.Errorf("mcp_servers scalar value %q does not contain %q; all DestField names must appear in the scalar",
			v.Scalar, "extra-server")
	}
}

// ---------------------------------------------------------------------------
// Local helpers for this file
// ---------------------------------------------------------------------------

// containsItem reports whether items contains the target string.
func containsItem(items []string, target string) bool {
	for _, s := range items {
		if s == target {
			return true
		}
	}
	return false
}

// truncateStr returns the first n bytes of s for use in diagnostic messages.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
