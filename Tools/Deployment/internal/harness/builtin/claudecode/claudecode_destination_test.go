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
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/claudecode"
)

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
	mod, err := claudecode.New()
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
	mod, err := claudecode.New()
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
	mod, err := claudecode.New()
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
