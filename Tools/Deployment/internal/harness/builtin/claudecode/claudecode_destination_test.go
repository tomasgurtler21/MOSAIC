package claudecode_test

// claudecode_destination_test.go tests that the Claude Code module correctly handles
// DestField destinations alongside its scalar-conversion behaviour (T4.2 / I4.3).
//
// Background: claudecode.module.convertFieldsToScalar converts the main tools field
// (identified by Descriptor().Frontmatter.ToolsKey) from KindList to KindScalar, because
// Claude Code expects a comma-separated string. When a mapping also carries a DestField
// destination, descriptor.MapTools returns a second FrontmatterField for the named
// destination key (e.g., "mcp_servers"). That second field must NOT be converted to
// scalar; it must pass through convertFieldsToScalar unchanged.
//
// These tests simulate the effect of the registry ToolMappings hook (I4.1/I4.2) by
// directly mutating the module descriptor's Mappings slice to add a DestField destination
// alongside the existing DestMain destination for user_interaction. This is the only
// permitted mutation of a module descriptor after construction (per the design contract),
// matching exactly what Discover does when it installs the hook's return value.
//
// These tests will be GREEN after the descriptor Stage 3 work (which is already done);
// they are written here as TDD specifications for the I4.3 contract:
// "convertFieldsToScalar must only affect the field whose key equals ToolsKey".

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/claudecode"
	"mosaic-deploy/internal/harness/registry"
)

// fieldKeys returns the frontmatter field keys from a Fields slice, for use in error messages.
func fieldKeys(fields []domain.FrontmatterField) []string {
	keys := make([]string, len(fields))
	for i, f := range fields {
		keys[i] = f.Key
	}
	return keys
}

// ---------------------------------------------------------------------------
// T4.2 / I4.3 — convertFieldsToScalar only converts the main tools field
// ---------------------------------------------------------------------------

// TestClaudeCode_Tools_DestFieldNotConvertedToScalar verifies that when a mapping carries
// both a DestMain destination (contributing to the main tools field) and a DestField
// destination (writing a separate frontmatter key), the DestField field is emitted as a
// KindList and is NOT converted to the comma-separated KindScalar that Claude Code uses
// for its main tools field.
//
// This is the critical test for convertFieldsToScalar: it must be narrowly scoped to the
// key identified by Descriptor().Frontmatter.ToolsKey and must not touch any other field.
func TestClaudeCode_Tools_DestFieldNotConvertedToScalar(t *testing.T) {
	mod, err := claudecode.New(registry.BuiltinOptions{MosaicRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("claudecode.New(): %v", err)
	}
	desc := mod.Descriptor()

	// Simulate the registry ToolMappings hook: overlay the user_interaction mapping with
	// an additional DestField destination routing user-feedback to mcp_servers.
	// This replaces the mapping's destination set with the merged effective set.
	overlaid := false
	for i, m := range desc.Tools.Mappings {
		if m.Generic == "user_interaction" {
			desc.Tools.Mappings[i].Destinations = append(
				desc.Tools.Mappings[i].Destinations,
				domain.ToolDestination{
					Kind:   domain.DestField,
					Field:  "mcp_servers",
					Format: domain.FormatListBlock,
					Names:  []string{"user-feedback"},
				},
			)
			overlaid = true
			break
		}
	}
	if !overlaid {
		t.Fatal("user_interaction mapping not found in claude-code descriptor; test setup failed")
	}

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	})
	if err != nil {
		t.Fatalf("mod.Tools: %v", err)
	}

	// Locate the main tools field and the mcp_servers DestField field.
	toolsKey := desc.Frontmatter.ToolsKey
	var toolsField *domain.FrontmatterField
	var mcpField *domain.FrontmatterField
	for i := range result.Fields {
		switch result.Fields[i].Key {
		case toolsKey:
			toolsField = &result.Fields[i]
		case "mcp_servers":
			mcpField = &result.Fields[i]
		}
	}

	if toolsField == nil {
		t.Fatalf("main tools field %q absent from Tools() result; tools field: %+v", toolsKey, result.Fields)
	}
	// Claude Code always converts the main tools field to scalar.
	if toolsField.Value.Kind != domain.KindScalar {
		t.Errorf("main tools field %q value kind: want KindScalar (Claude Code scalar format), got %v; "+
			"convertFieldsToScalar must convert the main tools field",
			toolsKey, toolsField.Value.Kind)
	}
	if toolsField.Value.Scalar == "" {
		t.Error("main tools field scalar value is empty; AskUserQuestion must appear in the scalar")
	}

	if mcpField == nil {
		t.Fatalf("mcp_servers field absent from Tools() result; DestField destination must produce a separate frontmatter field; "+
			"fields: %+v", result.Fields)
	}
	// The DestField field must NOT be converted to scalar.
	if mcpField.Value.Kind != domain.KindList {
		t.Errorf("mcp_servers field value kind: want KindList (DestField must not be converted to scalar), got %v; "+
			"convertFieldsToScalar must only affect the field identified by ToolsKey (%q), "+
			"leaving other fields unchanged", mcpField.Value.Kind, toolsKey)
	}
	if len(mcpField.Value.Items) == 0 {
		t.Error("mcp_servers field items empty; user-feedback must appear in the DestField field")
	}
	var foundUserFeedback bool
	for _, item := range mcpField.Value.Items {
		if item.Scalar == "user-feedback" {
			foundUserFeedback = true
		}
	}
	if !foundUserFeedback {
		t.Errorf("user-feedback not found in mcp_servers field items %v", mcpField.Value.Items)
	}
}

// TestClaudeCode_Tools_MainToolsFieldConvertedToScalarWithDestField verifies that when a
// DestField destination is present, the main tools field is STILL converted to the
// comma-separated scalar format. Adding a DestField destination must not suppress the
// Claude Code-specific scalar conversion that applies to the main tools field.
func TestClaudeCode_Tools_MainToolsFieldConvertedToScalarWithDestField(t *testing.T) {
	mod, err := claudecode.New(registry.BuiltinOptions{MosaicRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("claudecode.New(): %v", err)
	}
	desc := mod.Descriptor()

	// Overlay user_interaction with a DestField destination.
	for i, m := range desc.Tools.Mappings {
		if m.Generic == "user_interaction" {
			desc.Tools.Mappings[i].Destinations = append(
				desc.Tools.Mappings[i].Destinations,
				domain.ToolDestination{
					Kind:   domain.DestField,
					Field:  "mcp_servers",
					Format: domain.FormatListBlock,
					Names:  []string{"user-feedback"},
				},
			)
			break
		}
	}

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	})
	if err != nil {
		t.Fatalf("mod.Tools: %v", err)
	}

	toolsKey := desc.Frontmatter.ToolsKey
	for _, f := range result.Fields {
		if f.Key == toolsKey {
			if f.Value.Kind != domain.KindScalar {
				t.Errorf("main tools field %q value kind: want KindScalar, got %v; "+
					"the presence of a DestField destination must not suppress Claude Code's "+
					"scalar conversion of the main tools field",
					toolsKey, f.Value.Kind)
			}
			return
		}
	}
	t.Fatalf("main tools field %q not found in result; fields: %+v", toolsKey, result.Fields)
}

// TestClaudeCode_Tools_MultipleDestFieldsAllPassThroughUntouched verifies that when a
// module has two DestField destinations (two separate generic tools each routing to a
// different field), both produced fields pass through convertFieldsToScalar as KindList
// values without being converted to scalar.
func TestClaudeCode_Tools_MultipleDestFieldsAllPassThroughUntouched(t *testing.T) {
	mod, err := claudecode.New(registry.BuiltinOptions{MosaicRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("claudecode.New(): %v", err)
	}
	desc := mod.Descriptor()

	// Replace the entire mappings slice with a set containing two generic tools, each
	// with a DestField destination routing to a different field name. This simulates
	// a user config that declares two harness-agnostic tool-to-field routes.
	desc.Tools.Mappings = []domain.ToolMapping{
		{
			Generic: "user_interaction",
			Destinations: []domain.ToolDestination{
				{Kind: domain.DestMain, Names: []string{"AskUserQuestion"}},
				{Kind: domain.DestField, Field: "mcp_servers", Format: domain.FormatListBlock, Names: []string{"user-feedback"}},
			},
		},
		{
			Generic: "skill",
			Destinations: []domain.ToolDestination{
				{Kind: domain.DestField, Field: "skill_servers", Format: domain.FormatListBlock, Names: []string{"mosaic-skills"}},
			},
		},
	}
	// Ensure skill exists in the universe to avoid ToolUnmapped for skill, or use
	// a generic tool that this descriptor supports. Since we replaced mappings, let's
	// only request user_interaction which has a DestMain and a DestField.
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_interaction"},
	})
	if err != nil {
		t.Fatalf("mod.Tools for user_interaction: %v", err)
	}

	toolsKey := desc.Frontmatter.ToolsKey
	for _, f := range result.Fields {
		if f.Key == toolsKey {
			continue // main tools field, expected to be scalar
		}
		// All other fields must be KindList (not converted).
		if f.Value.Kind == domain.KindScalar {
			t.Errorf("field %q was converted to scalar; convertFieldsToScalar must only convert the field "+
				"identified by ToolsKey (%q), all other fields must pass through unchanged; "+
				"got fields: %+v", f.Key, toolsKey, result.Fields)
		}
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Custom tools route to mcpServers with no config-declared tool_destinations
// ---------------------------------------------------------------------------

// TestClaudeCode_CustomTool_RoutesToMcpServersWithNoConfigEntry verifies that when no
// config-declared tool_destinations entry exists for a custom (MCP-style) tool, the tool
// routes to the mcpServers frontmatter field as a block list, not to the main tools value.
//
// This test loads the real Claude Code module (embedding claude-code.yaml). It fails (RED)
// until claude-code.yaml declares custom_tool_destination routing custom tools to the
// mcpServers field. Before that declaration, custom tools fall back to the main tools
// scalar, so the mcpServers assertion fails.
func TestClaudeCode_CustomTool_RoutesToMcpServersWithNoConfigEntry(t *testing.T) {
	mod, err := claudecode.New(registry.BuiltinOptions{MosaicRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("claudecode.New(): %v", err)
	}
	desc := mod.Descriptor()

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"user_feedback"},
		CustomNames: map[string]string{
			"user_feedback": "human-in-the-loop",
		},
	})
	if err != nil {
		t.Fatalf("mod.Tools: %v", err)
	}

	toolsKey := desc.Frontmatter.ToolsKey

	var mcpField *domain.FrontmatterField
	var mainField *domain.FrontmatterField
	for i := range result.Fields {
		switch result.Fields[i].Key {
		case "mcpServers":
			mcpField = &result.Fields[i]
		case toolsKey:
			mainField = &result.Fields[i]
		}
	}

	// mcpServers must be present as a KindList (block list).
	if mcpField == nil {
		t.Fatalf("mcpServers field absent from Tools() result; "+
			"with no config-declared tool_destinations entry, a custom tool must route to "+
			"mcpServers via the descriptor's custom_tool_destination declaration; "+
			"fields present: %v", fieldKeys(result.Fields))
	}
	if mcpField.Value.Kind != domain.KindList {
		t.Errorf("mcpServers field kind: want KindList (list-block), got %v; "+
			"the descriptor's custom_tool_destination declares format: list-block",
			mcpField.Value.Kind)
	}
	var foundInMCP bool
	for _, item := range mcpField.Value.Items {
		if item.Scalar == "human-in-the-loop" {
			foundInMCP = true
		}
	}
	if !foundInMCP {
		t.Errorf("custom tool name human-in-the-loop not found in mcpServers field items %v; "+
			"the resolved custom tool name must appear in the mcpServers block list",
			mcpField.Value.Items)
	}

	// The custom tool must NOT appear in the main tools comma-separated scalar.
	if mainField != nil && mainField.Value.Kind == domain.KindScalar {
		if strings.Contains(mainField.Value.Scalar, "human-in-the-loop") {
			t.Errorf("custom tool human-in-the-loop found in main tools scalar %q; "+
				"custom tools must route exclusively to mcpServers when the descriptor declares "+
				"custom_tool_destination and no config entry overrides the default",
				mainField.Value.Scalar)
		}
	}
}

// TestClaudeCode_CustomTool_NoConfigEntry_BothMappedAndCustomToolRequestedTogether
// verifies that when both a descriptor-mapped tool (file_read → Read) and a custom tool
// (user_feedback → human-in-the-loop) are requested together, the mapped tool appears in
// the main tools scalar and the custom tool appears in mcpServers — they do not interfere.
//
// This test fails (RED) until claude-code.yaml declares custom_tool_destination, because
// without that declaration the custom tool also falls to the main scalar, mixing with Read.
func TestClaudeCode_CustomTool_NoConfigEntry_BothMappedAndCustomToolRequestedTogether(t *testing.T) {
	mod, err := claudecode.New(registry.BuiltinOptions{MosaicRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("claudecode.New(): %v", err)
	}
	desc := mod.Descriptor()

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "user_feedback"},
		CustomNames: map[string]string{
			"user_feedback": "human-in-the-loop",
		},
	})
	if err != nil {
		t.Fatalf("mod.Tools: %v", err)
	}

	toolsKey := desc.Frontmatter.ToolsKey

	var mainField *domain.FrontmatterField
	var mcpField *domain.FrontmatterField
	for i := range result.Fields {
		switch result.Fields[i].Key {
		case toolsKey:
			mainField = &result.Fields[i]
		case "mcpServers":
			mcpField = &result.Fields[i]
		}
	}

	// mapped tool (Read) must appear in the main scalar.
	if mainField == nil {
		t.Fatalf("main tools field %q absent from result; fields: %v", toolsKey, fieldKeys(result.Fields))
	}
	if mainField.Value.Kind != domain.KindScalar {
		t.Errorf("main tools field kind: want KindScalar, got %v", mainField.Value.Kind)
	}
	if !strings.Contains(mainField.Value.Scalar, "Read") {
		t.Errorf("Read not found in main tools scalar %q; file_read must map to Read", mainField.Value.Scalar)
	}

	// custom tool must NOT appear in the main scalar.
	if strings.Contains(mainField.Value.Scalar, "human-in-the-loop") {
		t.Errorf("custom tool human-in-the-loop found in main tools scalar %q; "+
			"custom tools must route to mcpServers, not to the main tools value",
			mainField.Value.Scalar)
	}

	// custom tool must appear in mcpServers block list.
	if mcpField == nil {
		t.Fatalf("mcpServers field absent from result; "+
			"custom tools must route to mcpServers via the descriptor's custom_tool_destination; "+
			"fields: %v", fieldKeys(result.Fields))
	}
	var found bool
	for _, item := range mcpField.Value.Items {
		if item.Scalar == "human-in-the-loop" {
			found = true
		}
	}
	if !found {
		t.Errorf("human-in-the-loop not found in mcpServers field %v", mcpField.Value.Items)
	}
}

// ---------------------------------------------------------------------------
// T3.4 — Main tools scalar conversion leaves mcpServers field untouched
// ---------------------------------------------------------------------------

// TestClaudeCode_ScalarConversion_McpServersFieldSurvivesAsBlockList verifies that when
// the Claude Code module produces both a main tools field (comma-separated KindScalar)
// and an mcpServers field (block-list KindList), the convertFieldsToScalar conversion
// applies only to the main tools field. The mcpServers field passes through unchanged as
// a KindList because its key does not equal ToolsKey.
//
// This test loads the real module and fails (RED) until claude-code.yaml declares
// custom_tool_destination: without that declaration, no mcpServers field is produced by
// the descriptor's custom-tool routing, and the assertion that mcpServers is a KindList
// block list cannot pass.
func TestClaudeCode_ScalarConversion_McpServersFieldSurvivesAsBlockList(t *testing.T) {
	mod, err := claudecode.New(registry.BuiltinOptions{MosaicRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("claudecode.New(): %v", err)
	}
	desc := mod.Descriptor()

	// Request a descriptor-mapped tool (file_read) and a custom tool. After I3.1 the
	// custom tool routes to mcpServers via the descriptor's custom_tool_destination.
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-agent",
		Generic:  []string{"file_read", "user_feedback"},
		CustomNames: map[string]string{
			"user_feedback": "human-in-the-loop",
		},
	})
	if err != nil {
		t.Fatalf("mod.Tools: %v", err)
	}

	toolsKey := desc.Frontmatter.ToolsKey

	var mainField *domain.FrontmatterField
	var mcpField *domain.FrontmatterField
	for i := range result.Fields {
		switch result.Fields[i].Key {
		case toolsKey:
			mainField = &result.Fields[i]
		case "mcpServers":
			mcpField = &result.Fields[i]
		}
	}

	// Main tools field must be a comma-separated KindScalar (Claude Code's format).
	if mainField == nil {
		t.Fatalf("main tools field %q absent from result; fields: %v", toolsKey, fieldKeys(result.Fields))
	}
	if mainField.Value.Kind != domain.KindScalar {
		t.Errorf("main tools field %q kind: want KindScalar (comma-separated scalar), got %v; "+
			"convertFieldsToScalar must convert the main tools field to a comma-separated scalar",
			toolsKey, mainField.Value.Kind)
	}
	if !strings.Contains(mainField.Value.Scalar, "Read") {
		t.Errorf("main tools scalar %q does not contain Read; file_read must map to Read", mainField.Value.Scalar)
	}

	// mcpServers field must survive as an unmodified KindList block list, because its key
	// does not equal ToolsKey and convertFieldsToScalar only converts the ToolsKey field.
	if mcpField == nil {
		t.Fatalf("mcpServers field absent from result; "+
			"custom tools must route to mcpServers via the descriptor's custom_tool_destination; "+
			"fields: %v", fieldKeys(result.Fields))
	}
	if mcpField.Value.Kind != domain.KindList {
		t.Errorf("mcpServers field kind: want KindList (block list, not converted to scalar), got %v; "+
			"convertFieldsToScalar must only convert the field identified by ToolsKey (%q); "+
			"the mcpServers field must pass through unchanged as a block list",
			mcpField.Value.Kind, toolsKey)
	}
	var foundInMCP bool
	for _, item := range mcpField.Value.Items {
		if item.Scalar == "human-in-the-loop" {
			foundInMCP = true
		}
	}
	if !foundInMCP {
		t.Errorf("human-in-the-loop not found in mcpServers field items %v; "+
			"the custom tool name must appear in the mcpServers block list after routing",
			mcpField.Value.Items)
	}

	// Confirm the custom tool name did NOT also appear in the main tools scalar.
	if strings.Contains(mainField.Value.Scalar, "human-in-the-loop") {
		t.Errorf("custom tool human-in-the-loop found in main tools scalar %q; "+
			"it must appear only in mcpServers, not in the main tools value",
			mainField.Value.Scalar)
	}
}
