package transform_test

// destination_reorder_test.go tests that DestField destination frontmatter fields survive
// the applyFrontmatter drop step and key-order step without being removed or misplaced (T4.6).
//
// Background:
//   applyFrontmatter processes fields in this order:
//     Step 1: remove fields named in the descriptor's static drop list
//     Step 2: add fields from the descriptor's static add list
//     Step 5: set tool fields (ToolResult.Fields — produced by MapTools for DestField destinations)
//     Step 7: reorder fields using the descriptor's key_order list
//
//   DestField destination fields are set in step 5, AFTER the drop step (step 1). Because
//   the drop list is static (compiled into the descriptor), and because destination field
//   names are user-configured at runtime, no static drop list can name a destination field.
//   Destination fields therefore must survive the drop step.
//
//   Reorder (step 7): fields not named in key_order are placed AFTER the listed fields in
//   their original relative order. Destination fields (unlisted in key_order) must appear
//   after the listed keys, in the order emitted by MapTools.
//
// These tests will be GREEN after the descriptor package Stage 3 work (which is already
// done). They are written here as regression specifications: any future change to
// applyFrontmatter must not break destination field survival.

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Inline descriptors for drop/reorder tests (T4.6)
// ---------------------------------------------------------------------------

// destWithDropListDescriptorYAML has a descriptor that drops "legacy_tools" from the
// frontmatter. The "mcp_servers" DestField destination must NOT be dropped, because it
// is not in the static drop list — only statically declared fields can be dropped.
const destWithDropListDescriptorYAML = `schema_version: "1"
id: "dest-drop-harness"
display_name: "Destination Drop Harness"
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
          names:
            - "user-feedback"
frontmatter:
  tools_key: "tools"
  drop:
    - "legacy_tools"
    - "deprecated_field"
`

// destWithKeyOrderDescriptorYAML has a descriptor with key_order listing id, version,
// description, tools. The "mcp_servers" DestField destination is NOT listed in key_order,
// so it must appear AFTER all listed keys.
const destWithKeyOrderDescriptorYAML = `schema_version: "1"
id: "dest-reorder-harness"
display_name: "Destination Reorder Harness"
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
          names:
            - "user-feedback"
frontmatter:
  tools_key: "tools"
  key_order:
    - "id"
    - "version"
    - "description"
    - "tools"
`

// destBeforeListedKeyDescriptorYAML has mcp_servers as an add field (so it appears in the
// source) and also as a DestField destination, with key_order placing tools before the
// unlisted fields. This tests that the destination field written in step 5 is placed
// correctly by the step 7 reorder even when the same key name was present in the source.
const destTwoFieldsWithKeyOrderYAML = `schema_version: "1"
id: "dest-two-fields-order-harness"
display_name: "Two Fields Order Harness"
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
  key_order:
    - "id"
    - "version"
    - "description"
    - "tools"
`

// ---------------------------------------------------------------------------
// Source with legacy fields to exercise the drop list
// ---------------------------------------------------------------------------

// sourceWithLegacyField is an agent source that includes a "legacy_tools" field.
// After the drop step, "legacy_tools" must be absent. After the tool field step,
// "mcp_servers" (from DestField) must be present.
const sourceWithLegacyField = `---
id: 200
version: 1.0.0
description: Drop list survival test agent
legacy_tools: [OldTool]
deprecated_field: old_value
tools: [user_interaction]
---
Body.
`

// sourceForReorderTest is an agent source for the reorder survival test.
const sourceForReorderTest = `---
id: 201
version: 1.0.0
description: Reorder test agent
tools: [user_interaction]
---
Body.
`

// sourceForTwoFieldsReorderTest is an agent source for the two-fields reorder test.
const sourceForTwoFieldsReorderTest = `---
id: 202
version: 1.0.0
description: Two-field reorder test agent
tools: [user_interaction, code_execution]
---
Body.
`

// ---------------------------------------------------------------------------
// T4.6 — Destination fields survive drop and reorder
// ---------------------------------------------------------------------------

// TestDestinationField_SurvivesDropList verifies that DestField destination fields are
// NOT removed by the descriptor's static drop list. The drop list is compiled into the
// descriptor and can only name statically known field keys; user-configured destination
// field names are unknown at descriptor authoring time and cannot appear in the drop list.
//
// The test drops "legacy_tools" and "deprecated_field" (static) and asserts that
// "mcp_servers" (user-configured DestField) survives.
func TestDestinationField_SurvivesDropList(t *testing.T) {
	mod := newDescriptorModule(t, destWithDropListDescriptorYAML, "inline:dest-drop")
	req := transform.Request{
		Source: []byte(sourceWithLegacyField),
		Kind:   domain.ArtifactAgent,
		Key:    "dest-drop-test",
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

	// Dropped fields must be absent.
	if _, ok := doc.Frontmatter().Get("legacy_tools"); ok {
		t.Error("legacy_tools present in output; the drop list must remove it")
	}
	if _, ok := doc.Frontmatter().Get("deprecated_field"); ok {
		t.Error("deprecated_field present in output; the drop list must remove it")
	}

	// DestField destination must survive.
	v, ok := doc.Frontmatter().Get("mcp_servers")
	if !ok {
		t.Fatalf("mcp_servers absent from output frontmatter; DestField destination must survive the drop step "+
			"because user-configured destination field names cannot appear in the descriptor's static drop list; "+
			"output keys: %v", doc.Frontmatter().Keys())
	}
	if len(v.Items) == 0 {
		t.Fatal("mcp_servers field is empty; DestField destination must emit its declared names")
	}
}

// TestDestinationField_AppearsAfterKeyOrderedFields verifies that DestField destination
// fields, which are NOT listed in the descriptor's key_order, are placed AFTER all fields
// that ARE listed in key_order. This is the standard behaviour for unlisted fields: they
// are appended after listed fields in their original relative order.
func TestDestinationField_AppearsAfterKeyOrderedFields(t *testing.T) {
	mod := newDescriptorModule(t, destWithKeyOrderDescriptorYAML, "inline:dest-reorder")
	req := transform.Request{
		Source: []byte(sourceForReorderTest),
		Kind:   domain.ArtifactAgent,
		Key:    "dest-reorder-test",
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

	keys := doc.Frontmatter().Keys()

	// mcp_servers must be present.
	mcpIdx := indexOf(keys, "mcp_servers")
	if mcpIdx < 0 {
		t.Fatalf("mcp_servers absent from output frontmatter; DestField destination must be present; keys: %v", keys)
	}

	// tools is the last key_order entry. mcp_servers (unlisted) must appear after it.
	toolsIdx := indexOf(keys, "tools")
	if toolsIdx < 0 {
		t.Fatalf("tools field absent from output; keys: %v", keys)
	}
	if mcpIdx < toolsIdx {
		t.Errorf("mcp_servers (index %d) appears before tools (index %d); "+
			"unlisted fields must be appended after all key_order-listed fields; keys: %v",
			mcpIdx, toolsIdx, keys)
	}
}

// TestDestinationField_TwoUnlistedFields_RelativeOrderFollowsEmission verifies that when
// two DestField destinations emit two unlisted fields, both fields appear after the
// key_order-listed fields, in the order they were emitted by MapTools (user_interaction
// first, code_execution second).
func TestDestinationField_TwoUnlistedFields_RelativeOrderFollowsEmission(t *testing.T) {
	mod := newDescriptorModule(t, destTwoFieldsWithKeyOrderYAML, "inline:dest-two-fields-order")
	req := transform.Request{
		Source: []byte(sourceForTwoFieldsReorderTest),
		Kind:   domain.ArtifactAgent,
		Key:    "dest-two-fields-order-test",
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

	keys := doc.Frontmatter().Keys()

	toolsIdx := indexOf(keys, "tools")
	mcpIdx := indexOf(keys, "mcp_servers")
	dockerIdx := indexOf(keys, "docker_images")

	if toolsIdx < 0 {
		t.Fatalf("tools absent from output; keys: %v", keys)
	}
	if mcpIdx < 0 {
		t.Fatalf("mcp_servers absent from output; user_interaction DestField destination must produce it; keys: %v", keys)
	}
	if dockerIdx < 0 {
		t.Fatalf("docker_images absent from output; code_execution DestField destination must produce it; keys: %v", keys)
	}

	// Both unlisted fields must appear after the last listed key (tools).
	if mcpIdx <= toolsIdx {
		t.Errorf("mcp_servers (index %d) does not appear after tools (index %d); "+
			"unlisted fields must be placed after key_order-listed fields; keys: %v",
			mcpIdx, toolsIdx, keys)
	}
	if dockerIdx <= toolsIdx {
		t.Errorf("docker_images (index %d) does not appear after tools (index %d); "+
			"unlisted fields must be placed after key_order-listed fields; keys: %v",
			dockerIdx, toolsIdx, keys)
	}

	// mcp_servers (from user_interaction, first in mappings) must appear before
	// docker_images (from code_execution, second in mappings).
	if mcpIdx >= dockerIdx {
		t.Errorf("mcp_servers (index %d) does not appear before docker_images (index %d); "+
			"unlisted DestField fields must appear in first-seen emission order (mapping declaration order); "+
			"keys: %v", mcpIdx, dockerIdx, keys)
	}
}

// ---------------------------------------------------------------------------
// Local helper
// ---------------------------------------------------------------------------

// indexOf returns the first index of target in slice, or -1 if not found.
func indexOf(slice []string, target string) int {
	for i, s := range slice {
		if s == target {
			return i
		}
	}
	return -1
}
