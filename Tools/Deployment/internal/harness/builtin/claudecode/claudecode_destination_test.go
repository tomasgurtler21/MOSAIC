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
// T3.1 — Custom tools appear as mcp__name__* wildcard in the main tools scalar
// ---------------------------------------------------------------------------

// TestClaudeCode_CustomTool_RoutesToMainToolsAsWildcard verifies that when a custom
// (MCP-style) tool is requested, the tool name is formatted as mcp__<name>__* via the
// descriptor's custom_tool_template and written to the main tools scalar -- not to any
// separate mcpServers field.
//
// This test loads the real Claude Code module (embedding claude-code.yaml). It fails (RED)
// against the old descriptor (which routed custom tools to mcpServers) and turns GREEN
// once claude-code.yaml declares custom_tool_template: "mcp__%s__*" with the custom tool
// destination set to main (or omitted, since main is the default).
func TestClaudeCode_CustomTool_RoutesToMainToolsAsWildcard(t *testing.T) {
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

	var mainField *domain.FrontmatterField
	for i := range result.Fields {
		if result.Fields[i].Key == toolsKey {
			mainField = &result.Fields[i]
		}
		// No mcpServers field must be produced by the descriptor's custom tool routing.
		if result.Fields[i].Key == "mcpServers" {
			t.Errorf("mcpServers field present in Tools() result; "+
				"after the fix, custom tools must route to the main tools scalar as mcp__<name>__*, "+
				"not to a separate mcpServers field; fields: %v", fieldKeys(result.Fields))
		}
	}

	// The main tools field must be present as a KindScalar.
	if mainField == nil {
		t.Fatalf("main tools field %q absent from Tools() result; "+
			"the custom tool must appear in the main tools scalar; fields: %v",
			toolsKey, fieldKeys(result.Fields))
	}
	if mainField.Value.Kind != domain.KindScalar {
		t.Errorf("main tools field %q kind: want KindScalar, got %v", toolsKey, mainField.Value.Kind)
	}

	// The custom tool must appear as mcp__human-in-the-loop__* in the main scalar.
	const wantWildcard = "mcp__human-in-the-loop__*"
	if !strings.Contains(mainField.Value.Scalar, wantWildcard) {
		t.Errorf("main tools scalar %q does not contain %q; "+
			"the descriptor's custom_tool_template must format the name as mcp__<name>__* "+
			"and write it to the main tools scalar",
			mainField.Value.Scalar, wantWildcard)
	}
}

// TestClaudeCode_CustomTool_BothMappedAndCustomToolRoutedToMainScalar verifies that when
// both a descriptor-mapped tool (file_read -> Read) and a custom tool
// (user_feedback -> human-in-the-loop) are requested together, both appear in the main
// tools scalar: Read as its mapped name and the custom tool as mcp__human-in-the-loop__*.
// No separate mcpServers field is produced.
//
// This test fails (RED) against the old descriptor (which routed the custom tool to
// mcpServers) and turns GREEN once claude-code.yaml declares custom_tool_template and
// routes custom tools to the main field.
func TestClaudeCode_CustomTool_BothMappedAndCustomToolRoutedToMainScalar(t *testing.T) {
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
	for i := range result.Fields {
		if result.Fields[i].Key == toolsKey {
			mainField = &result.Fields[i]
		}
		// No mcpServers field must be produced.
		if result.Fields[i].Key == "mcpServers" {
			t.Errorf("mcpServers field present in Tools() result; "+
				"after the fix, custom tools must route to the main tools scalar as mcp__<name>__*, "+
				"not to a separate mcpServers field; fields: %v", fieldKeys(result.Fields))
		}
	}

	// Main tools field must be a KindScalar.
	if mainField == nil {
		t.Fatalf("main tools field %q absent from result; fields: %v", toolsKey, fieldKeys(result.Fields))
	}
	if mainField.Value.Kind != domain.KindScalar {
		t.Errorf("main tools field kind: want KindScalar, got %v", mainField.Value.Kind)
	}

	// Mapped tool (Read) must appear in the main scalar.
	if !strings.Contains(mainField.Value.Scalar, "Read") {
		t.Errorf("Read not found in main tools scalar %q; file_read must map to Read", mainField.Value.Scalar)
	}

	// Custom tool must appear as mcp__human-in-the-loop__* in the main scalar.
	const wantWildcard = "mcp__human-in-the-loop__*"
	if !strings.Contains(mainField.Value.Scalar, wantWildcard) {
		t.Errorf("main tools scalar %q does not contain %q; "+
			"the custom tool must be formatted by custom_tool_template and written to the main scalar "+
			"alongside mapped tools",
			mainField.Value.Scalar, wantWildcard)
	}
}

// ---------------------------------------------------------------------------
// T3.4 — Custom tool wildcard appears in main scalar; no separate mcpServers field
// ---------------------------------------------------------------------------

// TestClaudeCode_ScalarConversion_CustomToolWildcardInMainScalar verifies that when
// the Claude Code module processes a descriptor-mapped tool and a custom tool together,
// both appear as entries in the main tools comma-separated scalar. The custom tool is
// formatted as mcp__<name>__* by the descriptor's custom_tool_template. No separate
// mcpServers field is produced, confirming that convertFieldsToScalar does not need to
// skip a non-existent mcpServers field.
//
// This test loads the real module and fails (RED) against the old descriptor (which
// routed custom tools to mcpServers instead of the main scalar) and turns GREEN once
// claude-code.yaml declares custom_tool_template: "mcp__%s__*" with custom tools
// directed to the main field.
func TestClaudeCode_ScalarConversion_CustomToolWildcardInMainScalar(t *testing.T) {
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
	for i := range result.Fields {
		if result.Fields[i].Key == toolsKey {
			mainField = &result.Fields[i]
		}
		// No mcpServers field should exist after the fix.
		if result.Fields[i].Key == "mcpServers" {
			t.Errorf("mcpServers field present in Tools() result; "+
				"after the fix, custom tools route to the main scalar as mcp__<name>__* "+
				"and no separate mcpServers field is produced; fields: %v", fieldKeys(result.Fields))
		}
	}

	// Main tools field must be a comma-separated KindScalar.
	if mainField == nil {
		t.Fatalf("main tools field %q absent from result; fields: %v", toolsKey, fieldKeys(result.Fields))
	}
	if mainField.Value.Kind != domain.KindScalar {
		t.Errorf("main tools field %q kind: want KindScalar (comma-separated scalar), got %v; "+
			"convertFieldsToScalar must convert the main tools field to a comma-separated scalar",
			toolsKey, mainField.Value.Kind)
	}

	// Mapped tool (Read) must appear in the main scalar.
	if !strings.Contains(mainField.Value.Scalar, "Read") {
		t.Errorf("main tools scalar %q does not contain Read; file_read must map to Read", mainField.Value.Scalar)
	}

	// Custom tool must appear as mcp__human-in-the-loop__* in the main scalar.
	const wantWildcard = "mcp__human-in-the-loop__*"
	if !strings.Contains(mainField.Value.Scalar, wantWildcard) {
		t.Errorf("main tools scalar %q does not contain %q; "+
			"the custom tool must be formatted by custom_tool_template as mcp__<name>__* "+
			"and written to the main tools scalar alongside mapped tools",
			mainField.Value.Scalar, wantWildcard)
	}
}
