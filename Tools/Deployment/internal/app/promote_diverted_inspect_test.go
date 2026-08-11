package app

// promote_diverted_inspect_test.go covers the Stage 2 extension of inspectPromoteSource:
// when the harness descriptor declares DestField destinations, the entries stored in those
// fields must be extracted and returned in promoteSourceFacts.DivertedTools.
//
// Tests are in package app (not package app_test) to access the unexported
// inspectPromoteSource function and promoteSourceFacts type directly.
//
// These tests are the TDD RED phase for I2.3. They fail because the current
// inspectPromoteSource does not populate DivertedTools.
//
// Verified behaviours:
//
//   - A deployed source carrying a diverted field whose descriptor declares FormatListBlock
//     produces non-empty DivertedTools in promoteSourceFacts.
//   - The DivertedTools entries match the tool names present in the diverted field value.
//   - DivertedTools are in extraction order (document order, first-seen across all diverted fields).
//   - DivertedTools from multiple diverted fields are concatenated in field-declaration order.
//   - A descriptor with no DestField destinations produces an empty DivertedTools slice.
//   - The main tools key entries remain in HarnessTools and are independent of DivertedTools.
//   - By-convention tools are excluded from DivertedTools (AD-14).
//   - DivertedTools are deduplicated first-seen across all diverted fields combined.

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// divertedDescriptorForInspect returns a HarnessDescriptor that declares "tools" as
// ToolsKey and "mcpServers" as a DestField destination with FormatListBlock.
// The universe includes "by-conv-tool" with ByConvention=true for AD-14 tests.
func divertedDescriptorForInspect() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		ID: "test-harness",
		Frontmatter: domain.FrontmatterSpec{
			ModelKey: "model",
			ToolsKey: "tools",
		},
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Universe: []domain.HarnessTool{
				{Name: "read-file", Unused: domain.Deny, ByConvention: false},
				{Name: "by-conv-tool", Unused: domain.Deny, ByConvention: true},
			},
			Mappings: []domain.ToolMapping{
				{
					Generic: "file_read",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"read-file"}},
					},
				},
				{
					Generic: "mcp_skill",
					Destinations: []domain.ToolDestination{
						{
							Kind:   domain.DestField,
							Field:  "mcpServers",
							Format: domain.FormatListBlock,
							Names:  []string{"mcp-skill-server"},
						},
					},
				},
			},
		},
	}
}

// noDestFieldDescriptorForInspect returns a descriptor with only DestMain destinations.
func noDestFieldDescriptorForInspect() *domain.HarnessDescriptor {
	return &domain.HarnessDescriptor{
		ID: "no-destfield-harness",
		Frontmatter: domain.FrontmatterSpec{
			ModelKey: "model",
			ToolsKey: "tools",
		},
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Universe: []domain.HarnessTool{
				{Name: "read-file", Unused: domain.Deny, ByConvention: false},
			},
			Mappings: []domain.ToolMapping{
				{
					Generic: "file_read",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestMain, Names: []string{"read-file"}},
					},
				},
			},
		},
	}
}

// buildDivertedSource returns source bytes for a deployed agent that carries both a
// main tools list and a diverted mcpServers field. The file satisfies both eligibility
// signals (transform_version + [[SECTION:...]] tag).
func buildDivertedSource(mainTools []string, mcpServerNames []string) []byte {
	toolsLine := "tools: [" + joinTools(mainTools) + "]\n"

	mcpBlock := "mcpServers:\n"
	for _, n := range mcpServerNames {
		mcpBlock += "  - " + n + "\n"
	}

	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"model: github-copilot/claude-sonnet-4-6\n" +
		"id: \"5\"\n" +
		"version: \"1.0.0\"\n" +
		"name: diverted-test-agent\n" +
		"role: subagent\n" +
		toolsLine +
		mcpBlock +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"Test agent content.\n" +
		"[[/SECTION:Identity]]\n")
}

// joinTools produces a comma-separated list for a YAML flow sequence.
func joinTools(names []string) string {
	if len(names) == 0 {
		return ""
	}
	result := names[0]
	for _, n := range names[1:] {
		result += ", " + n
	}
	return result
}

// verifyParses confirms that src parses as a valid docformat document. Used as a guard
// in tests that construct source bytes manually.
func verifyParses(t *testing.T, src []byte) {
	t.Helper()
	if _, err := docformat.Parse(src); err != nil {
		t.Fatalf("test source bytes do not parse: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestInspectPromoteSource_DivertedField_PopulatesDivertedTools verifies that when
// the descriptor declares a DestField destination and the source carries that field,
// inspectPromoteSource populates DivertedTools with the extracted entries.
func TestInspectPromoteSource_DivertedField_PopulatesDivertedTools(t *testing.T) {
	d := divertedDescriptorForInspect() // declares "mcpServers" as DestField/FormatListBlock
	src := buildDivertedSource([]string{"read-file"}, []string{"mcp-skill-server", "mcp-server-extra"})
	verifyParses(t, src)

	facts, err := inspectPromoteSource(src, d)
	if err != nil {
		t.Fatalf("inspectPromoteSource: %v", err)
	}

	if len(facts.DivertedTools) == 0 {
		t.Error("inspectPromoteSource with a diverted mcpServers field returned empty DivertedTools; " +
			"entries in DestField destinations must be extracted and placed in DivertedTools")
	}
}

// TestInspectPromoteSource_DivertedField_EntriesMatchDeclaredContent verifies that the
// DivertedTools entries match the tool names stored in the diverted field.
func TestInspectPromoteSource_DivertedField_EntriesMatchDeclaredContent(t *testing.T) {
	d := divertedDescriptorForInspect()
	src := buildDivertedSource([]string{"read-file"}, []string{"mcp-skill-server", "mcp-server-extra"})

	facts, err := inspectPromoteSource(src, d)
	if err != nil {
		t.Fatalf("inspectPromoteSource: %v", err)
	}

	wantDiverted := []string{"mcp-skill-server", "mcp-server-extra"}
	if len(facts.DivertedTools) != len(wantDiverted) {
		t.Fatalf("DivertedTools: want %d entries %v, got %d: %v",
			len(wantDiverted), wantDiverted, len(facts.DivertedTools), facts.DivertedTools)
	}
	for i, want := range wantDiverted {
		if facts.DivertedTools[i] != want {
			t.Errorf("DivertedTools[%d] = %q, want %q", i, facts.DivertedTools[i], want)
		}
	}
}

// TestInspectPromoteSource_NoDestField_DivertedToolsEmpty verifies that a descriptor
// with no DestField destinations produces an empty DivertedTools slice.
func TestInspectPromoteSource_NoDestField_DivertedToolsEmpty(t *testing.T) {
	d := noDestFieldDescriptorForInspect()
	src := buildDivertedSource([]string{"read-file"}, nil)

	facts, err := inspectPromoteSource(src, d)
	if err != nil {
		t.Fatalf("inspectPromoteSource: %v", err)
	}

	if len(facts.DivertedTools) != 0 {
		t.Errorf("inspectPromoteSource with no DestField destinations returned non-empty DivertedTools %v; "+
			"only descriptors that declare DestField destinations produce DivertedTools",
			facts.DivertedTools)
	}
}

// TestInspectPromoteSource_DivertedField_IndependentFromMainTools verifies that
// DivertedTools and HarnessTools are populated independently: entries in the main tools
// key appear only in HarnessTools; entries in diverted fields appear only in DivertedTools.
func TestInspectPromoteSource_DivertedField_IndependentFromMainTools(t *testing.T) {
	d := divertedDescriptorForInspect()
	src := buildDivertedSource([]string{"read-file"}, []string{"mcp-skill-server"})

	facts, err := inspectPromoteSource(src, d)
	if err != nil {
		t.Fatalf("inspectPromoteSource: %v", err)
	}

	// "read-file" must be in HarnessTools, not DivertedTools.
	for _, e := range facts.DivertedTools {
		if e == "read-file" {
			t.Errorf("main tools key entry %q appeared in DivertedTools; main tools must stay in HarnessTools only", "read-file")
		}
	}
	// "mcp-skill-server" must be in DivertedTools, not HarnessTools.
	for _, e := range facts.HarnessTools {
		if e == "mcp-skill-server" {
			t.Errorf("diverted entry %q appeared in HarnessTools; diverted tools must be in DivertedTools only", "mcp-skill-server")
		}
	}
}

// TestInspectPromoteSource_DivertedField_DocumentOrder verifies that DivertedTools
// entries are returned in document order (the order they appear in the YAML value).
func TestInspectPromoteSource_DivertedField_DocumentOrder(t *testing.T) {
	d := divertedDescriptorForInspect()
	// Declare the servers in a specific order.
	src := buildDivertedSource(nil, []string{"server-z", "server-a", "server-m"})

	facts, err := inspectPromoteSource(src, d)
	if err != nil {
		t.Fatalf("inspectPromoteSource: %v", err)
	}

	if len(facts.DivertedTools) != 3 {
		t.Fatalf("DivertedTools: want 3 entries, got %d: %v", len(facts.DivertedTools), facts.DivertedTools)
	}
	wantOrder := []string{"server-z", "server-a", "server-m"}
	for i, want := range wantOrder {
		if facts.DivertedTools[i] != want {
			t.Errorf("DivertedTools[%d] = %q, want %q (document order must be preserved)",
				i, facts.DivertedTools[i], want)
		}
	}
}

// TestInspectPromoteSource_DivertedField_ByConventionExcluded verifies that
// by-convention tools (ByConvention=true in the descriptor) are excluded from
// DivertedTools, matching the ExtractToolEntries behaviour for the main tools key (AD-14).
func TestInspectPromoteSource_DivertedField_ByConventionExcluded(t *testing.T) {
	d := divertedDescriptorForInspect() // universe includes "by-conv-tool" with ByConvention=true
	// Include a by-convention tool name in the diverted field value.
	src := buildDivertedSource(nil, []string{"mcp-skill-server", "by-conv-tool"})

	facts, err := inspectPromoteSource(src, d)
	if err != nil {
		t.Fatalf("inspectPromoteSource: %v", err)
	}

	for _, e := range facts.DivertedTools {
		if e == "by-conv-tool" {
			t.Errorf("by-convention tool %q must be excluded from DivertedTools (AD-14); got entries: %v",
				"by-conv-tool", facts.DivertedTools)
		}
	}
}

// TestInspectPromoteSource_DivertedField_Deduplication verifies that duplicate entries
// across the diverted field(s) are deduplicated first-seen.
func TestInspectPromoteSource_DivertedField_Deduplication(t *testing.T) {
	d := divertedDescriptorForInspect()
	src := buildDivertedSource(nil, []string{"mcp-skill-server", "mcp-skill-server"}) // duplicate

	facts, err := inspectPromoteSource(src, d)
	if err != nil {
		t.Fatalf("inspectPromoteSource: %v", err)
	}

	count := 0
	for _, e := range facts.DivertedTools {
		if e == "mcp-skill-server" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("DivertedTools contains %q %d times; duplicate entries must be deduplicated first-seen — got: %v",
			"mcp-skill-server", count, facts.DivertedTools)
	}
}

// TestInspectPromoteSource_MultipleDestFields_AllExtracted verifies that when the
// descriptor declares two DestField destinations and both are present in the source,
// entries from both fields appear in DivertedTools, with the first-declared field's
// entries coming first.
func TestInspectPromoteSource_MultipleDestFields_AllExtracted(t *testing.T) {
	// Build a descriptor with two DestField destinations.
	d := &domain.HarnessDescriptor{
		ID: "multi-destfield-harness",
		Frontmatter: domain.FrontmatterSpec{
			ModelKey: "model",
			ToolsKey: "tools",
		},
		Tools: domain.ToolSpec{
			Shape: domain.ShapeList,
			Universe: []domain.HarnessTool{
				{Name: "read-file", Unused: domain.Deny, ByConvention: false},
			},
			Mappings: []domain.ToolMapping{
				{
					Generic: "tool_alpha",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "fieldOne", Format: domain.FormatListBlock, Names: []string{"alpha-server"}},
					},
				},
				{
					Generic: "tool_beta",
					Destinations: []domain.ToolDestination{
						{Kind: domain.DestField, Field: "fieldTwo", Format: domain.FormatListBlock, Names: []string{"beta-server"}},
					},
				},
			},
		},
	}

	// Build a source that carries both diverted fields.
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"id: \"5\"\n" +
		"version: \"1.0.0\"\n" +
		"name: multi-diverted-agent\n" +
		"role: subagent\n" +
		"fieldOne:\n" +
		"  - alpha-server\n" +
		"fieldTwo:\n" +
		"  - beta-server\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"Content.\n" +
		"[[/SECTION:Identity]]\n")

	facts, err := inspectPromoteSource(src, d)
	if err != nil {
		t.Fatalf("inspectPromoteSource: %v", err)
	}

	// Both diverted entries must appear in DivertedTools.
	wantEntries := map[string]bool{"alpha-server": true, "beta-server": true}
	found := make(map[string]bool)
	for _, e := range facts.DivertedTools {
		found[e] = true
	}

	for want := range wantEntries {
		if !found[want] {
			t.Errorf("DivertedTools is missing entry %q from the second DestField destination; "+
				"all diverted fields must be collected into DivertedTools; got: %v",
				want, facts.DivertedTools)
		}
	}
}
