package app_test

// promote_diversion_e2e_test.go covers the Stage 2 end-to-end promote behaviours for
// diverted fields and unknown fields. Tests are in package app_test to exercise the
// Service.Promote method through its public interface, verifying that the service:
//
//   - recovers diverted tool entries into the generic tools list (AC2.2),
//   - strips the diverted field key from the output (AC2.2),
//   - auto-strips unmappable diverted values and reports them in StrippedFields with
//     StripReasonUnmappedDivertedValue, with no user prompt (AC2.3, FR-2),
//   - strips unknown frontmatter fields and reports them in StrippedFields with
//     StripReasonUnknownField (AC2.4, FR-3),
//   - resolves diverted fields per value: mappable values are recovered, unmappable
//     values are reported, and the field key is always dropped (AC2.7),
//   - populates PromoteResult.DivertedTools with the recovered diverted entries.
//
// These tests are the TDD RED phase for I2.3–I2.6. They compile successfully and fail
// because the service does not yet implement diverted-field recovery or unknown-field
// stripping.
//
// Test structure:
//   - Each test sets up a real (but minimal) service with a harness module whose
//     descriptor declares DestField destinations, writes a temporary source file, and
//     calls Service.Promote with all questions pre-answered.
//   - The interaction stub is configured to expect zero questions for the diverted-
//     field path (FR-2: no prompt for unmapped diverted values).
//
// Limitation: service-level promote tests require a filesystem (catalog discovery needs
// to write the output file). Tests use t.TempDir() for isolation.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Fixtures: harness modules that declare DestField destinations
// ---------------------------------------------------------------------------

// divertedHarnessModule is a stubHarnessModule whose descriptor declares "mcpServers"
// as a DestField/FormatListBlock destination for the "mcp_skill" generic tool.
// The mapping for "file_read" uses DestMain.
//
// Mappings:
//   - file_read → DestMain, ["read-file"]
//   - mcp_skill → DestField "mcpServers" FormatListBlock, ["mcp-skill-server"]
type divertedHarnessModule struct {
	ref  domain.HarnessRef
	desc domain.HarnessDescriptor
}

func newDivertedHarnessModule() *divertedHarnessModule {
	return &divertedHarnessModule{
		ref: domain.HarnessRef{
			ID:          "diverted-harness",
			DisplayName: "Diverted Harness",
			Tier:        domain.TierBuiltin,
			Usable:      true,
		},
		desc: domain.HarnessDescriptor{
			ID:          "diverted-harness",
			DisplayName: "Diverted Harness",
			Frontmatter: domain.FrontmatterSpec{
				ModelKey: "model",
				ToolsKey: "tools",
			},
			Tools: domain.ToolSpec{
				Shape: domain.ShapeList,
				Universe: []domain.HarnessTool{
					{Name: "read-file", Unused: domain.Deny, ByConvention: false},
					{Name: "mcp-skill-server", Unused: domain.Deny, ByConvention: false},
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
		},
	}
}

func (m *divertedHarnessModule) Ref() domain.HarnessRef                { return m.ref }
func (m *divertedHarnessModule) Descriptor() *domain.HarnessDescriptor { return &m.desc }
func (m *divertedHarnessModule) Tools(req domain.ToolRequest) (domain.ToolResult, error) {
	resolutions := make([]domain.ToolResolution, len(req.Generic))
	for i, g := range req.Generic {
		resolutions[i] = domain.ToolResolution{Generic: g, Outcome: domain.ToolMapped}
	}
	return domain.ToolResult{Resolutions: resolutions}, nil
}
func (m *divertedHarnessModule) Frontmatter(req domain.FrontmatterRequest) (domain.FrontmatterPlan, error) {
	return domain.FrontmatterPlan{}, nil
}
func (m *divertedHarnessModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	return req.Key + ".md", nil
}
func (m *divertedHarnessModule) Injection(_ domain.InjectionRequest) (string, bool) { return "", false }
func (m *divertedHarnessModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{Supported: false, Reason: "stub"}, nil
}
func (m *divertedHarnessModule) Close() error { return nil }

// ---------------------------------------------------------------------------
// Source file builders
// ---------------------------------------------------------------------------

// divertedSourceWithMappableTool builds a deployed agent source that carries:
//   - "tools:" with "read-file" (maps to generic "file_read")
//   - "mcpServers:" list with "mcp-skill-server" (maps to generic "mcp_skill")
//
// Both entries have reverse-map entries in the descriptor. The output must contain
// ["file_read", "mcp_skill"] in the generic tools list.
func divertedSourceWithMappableTool() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"model: github-copilot/claude-sonnet-4-6\n" +
		"id: \"10\"\n" +
		"version: \"1.0.0\"\n" +
		"name: diverted-agent\n" +
		"description: Agent with diverted tool entries\n" +
		"role: subagent\n" +
		"tools:\n" +
		"  - read-file\n" +
		"mcpServers:\n" +
		"  - mcp-skill-server\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are a test agent.\n" +
		"[[/SECTION:Identity]]\n")
}

// divertedSourceWithUnmappableTool builds a deployed agent source carrying
// "mcpServers:" with one entry that has no reverse-mapping in the descriptor.
func divertedSourceWithUnmappableTool() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"model: github-copilot/claude-sonnet-4-6\n" +
		"id: \"11\"\n" +
		"version: \"1.0.0\"\n" +
		"name: diverted-unmapped-agent\n" +
		"description: Agent with unmappable diverted entry\n" +
		"role: subagent\n" +
		"tools:\n" +
		"  - read-file\n" +
		"mcpServers:\n" +
		"  - unmapped-custom-server\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are a test agent.\n" +
		"[[/SECTION:Identity]]\n")
}

// divertedSourceWithMixedDivertedEntries builds a source with "mcpServers:" carrying
// both a mappable entry ("mcp-skill-server") and an unmappable entry
// ("unmapped-custom-server"). Used for the partial-resolution tests (T2.5).
func divertedSourceWithMixedDivertedEntries() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"model: github-copilot/claude-sonnet-4-6\n" +
		"id: \"12\"\n" +
		"version: \"1.0.0\"\n" +
		"name: mixed-diverted-agent\n" +
		"description: Agent with partial diversion mapping\n" +
		"role: subagent\n" +
		"tools:\n" +
		"  - read-file\n" +
		"mcpServers:\n" +
		"  - mcp-skill-server\n" +
		"  - unmapped-custom-server\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are a test agent.\n" +
		"[[/SECTION:Identity]]\n")
}

// sourceWithUnknownField builds a deployed agent source that carries a frontmatter
// field ("custom_unknown_field") not declared by the harness or by MOSAIC.
func sourceWithUnknownField() []byte {
	return []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"injections_version: \"1.0.0\"\n" +
		"model: github-copilot/claude-sonnet-4-6\n" +
		"id: \"13\"\n" +
		"version: \"1.0.0\"\n" +
		"name: unknown-field-agent\n" +
		"description: Agent with an unknown frontmatter field\n" +
		"role: subagent\n" +
		"tools:\n" +
		"  - read-file\n" +
		"custom_unknown_field: some-value\n" +
		"---\n" +
		"[[SECTION:Identity]]\n" +
		"You are a test agent.\n" +
		"[[/SECTION:Identity]]\n")
}

// ---------------------------------------------------------------------------
// Service construction helpers
// ---------------------------------------------------------------------------

// buildDivertedPromoteDeps builds a fully-wired app.Deps with a divertedHarnessModule
// as the sole harness, using the supplied mosaicRoot for catalog writes.
func buildDivertedPromoteDeps(t *testing.T, stub *interactiontest.Stub, mosaicRoot string) app.Deps {
	t.Helper()
	deps, _ := newBaseDeps(t, stub)
	deps.MosaicRoot = mosaicRoot

	mod := newDivertedHarnessModule()
	deps.Registry = &stubRegistry{
		list: []domain.HarnessRef{mod.Ref()},
		modules: map[string]domain.HarnessModule{
			mod.Ref().ID: mod,
		},
	}
	return deps
}

// writeDivertedSourceFile writes srcBytes to a temp file in dir and returns its path.
func writeDivertedSourceFile(t *testing.T, dir string, name string, srcBytes []byte) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, srcBytes, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// T2.4: Promote end-to-end with diverted fields
// ---------------------------------------------------------------------------

// TestPromote_DivertedEntryReverseMaps_AppearsInGenericTools verifies that a diverted
// field entry that has a reverse mapping appears in the promoted generic agent's tools
// list. This is the core AC2.2 assertion.
func TestPromote_DivertedEntryReverseMaps_AppearsInGenericTools(t *testing.T) {
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeDivertedSourceFile(t, t.TempDir(), "diverted-agent.md", divertedSourceWithMappableTool())

	// No questions should be asked for diverted entries that reverse-map.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, "TestCategory", "TestCategory").
		Build()
	deps := buildDivertedPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		HarnessID: "diverted-harness",
		Category:  "TestCategory",
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// The diverted tool "mcp-skill-server" reverse-maps to "mcp_skill". It must appear
	// in the promoted generic tools list.
	found := false
	for _, tool := range result.Tools {
		if tool == "mcp_skill" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("generic tool %q not found in result.Tools %v; "+
			"a diverted entry with a known reverse mapping must be recovered into the generic tools list (AC2.2)",
			"mcp_skill", result.Tools)
	}
}

// TestPromote_DivertedEntryReverseMaps_DivertedFieldAbsentFromOutput verifies that the
// harness-side diverted field key ("mcpServers") does not appear in the promoted generic
// agent file. It is a harness artifact and must always be dropped (AC2.2).
func TestPromote_DivertedEntryReverseMaps_DivertedFieldAbsentFromOutput(t *testing.T) {
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeDivertedSourceFile(t, t.TempDir(), "diverted-agent.md", divertedSourceWithMappableTool())

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, "TestCategory", "TestCategory").
		Build()
	deps := buildDivertedPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		HarnessID: "diverted-harness",
		Category:  "TestCategory",
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Read the generated file and verify "mcpServers" is absent from its frontmatter.
	if result.DestinationPath == "" {
		t.Fatal("PromoteResult.DestinationPath is empty; cannot verify output file contents")
	}
	outBytes, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("reading promoted output file: %v", readErr)
	}

	// Use a simple bytes check: "mcpServers:" appearing in the frontmatter would be a failure.
	if containsFrontmatterKey(outBytes, "mcpServers") {
		t.Errorf("promoted generic agent file contains frontmatter key %q; "+
			"diverted harness fields must always be dropped from the generic output (AC2.2)",
			"mcpServers")
	}
}

// TestPromote_DivertedEntryReverseMaps_PopulatesDivertedToolsInResult verifies that
// PromoteResult.DivertedTools contains the recovered diverted entries, allowing the
// user to see which entries were recovered from diverted fields (separate from the
// main tools key).
func TestPromote_DivertedEntryReverseMaps_PopulatesDivertedToolsInResult(t *testing.T) {
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeDivertedSourceFile(t, t.TempDir(), "diverted-agent.md", divertedSourceWithMappableTool())

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, "TestCategory", "TestCategory").
		Build()
	deps := buildDivertedPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		HarnessID: "diverted-harness",
		Category:  "TestCategory",
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	if len(result.DivertedTools) == 0 {
		t.Error("PromoteResult.DivertedTools is empty; recovered diverted entries must be reported " +
			"separately so the user can see the recovery happened")
	}
}

// TestPromote_UnmappableDivertedEntry_StrippedWithNoPrompt verifies that a diverted
// entry that cannot be reverse-mapped is stripped automatically without asking the
// user (FR-2) and appears in PromoteResult.StrippedFields with
// StripReasonUnmappedDivertedValue (AC2.3).
func TestPromote_UnmappableDivertedEntry_StrippedWithNoPrompt(t *testing.T) {
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeDivertedSourceFile(t, t.TempDir(), "unmapped-agent.md", divertedSourceWithUnmappableTool())

	// The interaction stub must NOT answer QPromoteCustomTool — the unmapped diverted
	// entry must be stripped automatically, never prompted (FR-2).
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, "TestCategory", "TestCategory").
		Build()
	deps := buildDivertedPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		HarnessID: "diverted-harness",
		Category:  "TestCategory",
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// At least one StrippedField must be present with the unmapped diverted value reported.
	var found bool
	for _, sf := range result.StrippedFields {
		if sf.Reason == app.StripReasonUnmappedDivertedValue {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PromoteResult.StrippedFields contains no entry with StripReasonUnmappedDivertedValue; "+
			"unmappable diverted entries must be reported as stripped automatically (AC2.3, FR-2); "+
			"got StrippedFields: %v", result.StrippedFields)
	}
}

// TestPromote_UnmappableDivertedEntry_ReportedWithValue verifies that the StrippedField
// entry for an unmapped diverted value carries the harness-side tool name so the user
// can re-add it by hand.
func TestPromote_UnmappableDivertedEntry_ReportedWithValue(t *testing.T) {
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeDivertedSourceFile(t, t.TempDir(), "unmapped-agent.md", divertedSourceWithUnmappableTool())

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, "TestCategory", "TestCategory").
		Build()
	deps := buildDivertedPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		HarnessID: "diverted-harness",
		Category:  "TestCategory",
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Find the stripped-field entry for the unmapped diverted value.
	var strippedEntry *app.StrippedField
	for i, sf := range result.StrippedFields {
		if sf.Reason == app.StripReasonUnmappedDivertedValue {
			strippedEntry = &result.StrippedFields[i]
			break
		}
	}
	if strippedEntry == nil {
		t.Fatalf("no StrippedField with StripReasonUnmappedDivertedValue found in %v", result.StrippedFields)
	}

	// The Values must include "unmapped-custom-server" so the user knows what was lost.
	found := false
	for _, v := range strippedEntry.Values {
		if v == "unmapped-custom-server" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("StrippedField.Values %v does not contain the unmapped harness name %q; "+
			"the stripped entry must carry the lost value so the user can recover it manually",
			strippedEntry.Values, "unmapped-custom-server")
	}
}

// TestPromote_UnknownField_StrippedAndReported verifies that a frontmatter field unknown
// to MOSAIC and to the harness is stripped from the promoted file and reported in
// PromoteResult.StrippedFields with StripReasonUnknownField (AC2.4, FR-3).
func TestPromote_UnknownField_StrippedAndReported(t *testing.T) {
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeDivertedSourceFile(t, t.TempDir(), "unknown-field-agent.md", sourceWithUnknownField())

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, "TestCategory", "TestCategory").
		Build()
	deps := buildDivertedPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		HarnessID: "diverted-harness",
		Category:  "TestCategory",
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Find the stripped entry for "custom_unknown_field".
	var unknownEntry *app.StrippedField
	for i, sf := range result.StrippedFields {
		if sf.Key == "custom_unknown_field" && sf.Reason == app.StripReasonUnknownField {
			unknownEntry = &result.StrippedFields[i]
			break
		}
	}
	if unknownEntry == nil {
		t.Errorf("no StrippedField entry for %q with StripReasonUnknownField found in StrippedFields %v; "+
			"fields unknown to MOSAIC and the harness must be stripped and reported (AC2.4, FR-3)",
			"custom_unknown_field", result.StrippedFields)
	}
}

// TestPromote_UnknownField_AbsentFromOutputFile verifies that the unknown field key is
// absent from the promoted generic agent's frontmatter.
func TestPromote_UnknownField_AbsentFromOutputFile(t *testing.T) {
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeDivertedSourceFile(t, t.TempDir(), "unknown-field-agent.md", sourceWithUnknownField())

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, "TestCategory", "TestCategory").
		Build()
	deps := buildDivertedPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		HarnessID: "diverted-harness",
		Category:  "TestCategory",
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	if result.DestinationPath == "" {
		t.Fatal("PromoteResult.DestinationPath is empty; cannot verify output file contents")
	}
	outBytes, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("reading promoted output: %v", readErr)
	}

	if containsFrontmatterKey(outBytes, "custom_unknown_field") {
		t.Errorf("promoted generic agent file contains unknown field %q; "+
			"fields unknown to MOSAIC and the harness must be stripped from the output (AC2.4, FR-3)",
			"custom_unknown_field")
	}
}

// ---------------------------------------------------------------------------
// T2.5: Partial-resolution case
// ---------------------------------------------------------------------------

// TestPromote_PartialDiversion_MappableValuesRecovered verifies that when a diverted
// field carries both a mappable and an unmappable entry, the mappable one is recovered
// into the generic tools list (AC2.7).
func TestPromote_PartialDiversion_MappableValuesRecovered(t *testing.T) {
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeDivertedSourceFile(t, t.TempDir(), "mixed-diverted.md", divertedSourceWithMixedDivertedEntries())

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, "TestCategory", "TestCategory").
		Build()
	deps := buildDivertedPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		HarnessID: "diverted-harness",
		Category:  "TestCategory",
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// "mcp-skill-server" reverse-maps to "mcp_skill" — must appear in tools.
	found := false
	for _, tool := range result.Tools {
		if tool == "mcp_skill" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("generic tool %q not found in result.Tools %v; "+
			"the mappable diverted entry must be recovered even when the same field carries unmappable entries (AC2.7)",
			"mcp_skill", result.Tools)
	}
}

// TestPromote_PartialDiversion_OnlyUnmappableValuesStripped verifies that when a
// diverted field carries both a mappable and an unmappable entry, only the unmappable
// one appears in StrippedFields.Values — the mappable one must not be in the strip report
// (AC2.7).
func TestPromote_PartialDiversion_OnlyUnmappableValuesStripped(t *testing.T) {
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeDivertedSourceFile(t, t.TempDir(), "mixed-diverted.md", divertedSourceWithMixedDivertedEntries())

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, "TestCategory", "TestCategory").
		Build()
	deps := buildDivertedPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		HarnessID: "diverted-harness",
		Category:  "TestCategory",
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	// Find the StrippedField for the unmapped diverted value.
	var strippedEntry *app.StrippedField
	for i, sf := range result.StrippedFields {
		if sf.Reason == app.StripReasonUnmappedDivertedValue {
			strippedEntry = &result.StrippedFields[i]
			break
		}
	}
	if strippedEntry == nil {
		t.Fatalf("no StrippedField with StripReasonUnmappedDivertedValue; "+
			"the unmappable diverted entry must be reported (AC2.7, AC2.3); got StrippedFields: %v",
			result.StrippedFields)
	}

	// The StrippedField must contain only the unmappable value.
	for _, v := range strippedEntry.Values {
		if v == "mcp-skill-server" {
			t.Errorf("StrippedField.Values contains the mappable entry %q; "+
				"resolution is per value — only unmappable values are stripped, not the whole field (AC2.7)",
				"mcp-skill-server")
		}
	}

	found := false
	for _, v := range strippedEntry.Values {
		if v == "unmapped-custom-server" {
			found = true
		}
	}
	if !found {
		t.Errorf("StrippedField.Values %v does not contain the unmappable entry %q; "+
			"only the unmappable values must appear in the strip report (AC2.7)",
			strippedEntry.Values, "unmapped-custom-server")
	}
}

// TestPromote_PartialDiversion_FieldKeyAlwaysDropped verifies that the diverted field
// key ("mcpServers") is absent from the promoted output even when some values are
// mappable — the field key is always a harness artifact and is never carried (AC2.7).
func TestPromote_PartialDiversion_FieldKeyAlwaysDropped(t *testing.T) {
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeDivertedSourceFile(t, t.TempDir(), "mixed-diverted.md", divertedSourceWithMixedDivertedEntries())

	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, "TestCategory", "TestCategory").
		Build()
	deps := buildDivertedPromoteDeps(t, stub, mosaicRoot)
	svc := app.New(deps)

	result, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		HarnessID: "diverted-harness",
		Category:  "TestCategory",
	})
	if err != nil {
		t.Fatalf("Promote: unexpected error: %v", err)
	}

	if result.DestinationPath == "" {
		t.Fatal("PromoteResult.DestinationPath is empty; cannot verify output file")
	}
	outBytes, readErr := os.ReadFile(result.DestinationPath)
	if readErr != nil {
		t.Fatalf("reading promoted output: %v", readErr)
	}

	if containsFrontmatterKey(outBytes, "mcpServers") {
		t.Errorf("promoted generic agent file contains diverted field key %q; "+
			"the field key must always be dropped regardless of whether its values reverse-mapped (AC2.7)",
			"mcpServers")
	}
}

// TestPromote_PartialDiversion_NoPromptForUnmappedDivertedValues verifies that the
// interaction stub is NOT asked QPromoteCustomTool for unmappable diverted entries —
// stripping is automatic with no user interaction (FR-2, AC2.3).
func TestPromote_PartialDiversion_NoPromptForUnmappedDivertedValues(t *testing.T) {
	mosaicRoot := writeMosaicRoot(t)
	srcPath := writeDivertedSourceFile(t, t.TempDir(), "mixed-diverted.md", divertedSourceWithMixedDivertedEntries())

	// Build a spy interaction that fails the test if QPromoteCustomTool is asked.
	spy := &noCustomToolPromptSpy{t: t}
	deps := buildDivertedPromoteDeps(t, interactiontest.NewBuilder().
		AnswerSelectOne(domain.QPromoteCategory, "TestCategory", "TestCategory").
		Build(), mosaicRoot)
	deps.Interaction = spy
	svc := app.New(deps)

	_, err := svc.Promote(context.Background(), app.PromoteRequest{
		FilePath:  srcPath,
		HarnessID: "diverted-harness",
		Category:  "TestCategory",
	})
	if err != nil {
		// A prompt-violation error would have been set by the spy.
		if errors.Is(err, errCustomToolPromptForbidden) {
			t.Fatalf("Service.Promote asked QPromoteCustomTool for an unmappable diverted entry; "+
				"FR-2 requires automatic stripping with no prompt for diverted-field values: %v", err)
		}
		t.Fatalf("Promote: unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// containsFrontmatterKey reports whether the bytes of a YAML-fronted document carry
// the given key at the frontmatter level. This is a simple byte scan; it looks for
// "\nkey:" or "^key:" patterns. It is intentionally conservative — a false positive is
// worse than a false negative in these tests.
func containsFrontmatterKey(b []byte, key string) bool {
	needle := []byte("\n" + key + ":")
	if len(b) >= len(key)+1 {
		// Handle key at the very start of the frontmatter block (after "---\n").
		prefix := []byte(key + ":")
		// Scan after the first "---\n".
		const marker = "---\n"
		idx := 0
		for i := 0; i < len(b)-len(marker)+1; i++ {
			if string(b[i:i+len(marker)]) == marker {
				idx = i + len(marker)
				break
			}
		}
		if idx > 0 && idx+len(prefix) <= len(b) {
			if string(b[idx:idx+len(prefix)]) == string(prefix) {
				return true
			}
		}
	}
	// Also check for the key appearing after any newline.
	for i := 0; i < len(b)-len(needle)+1; i++ {
		match := true
		for j, c := range needle {
			if b[i+j] != c {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// errCustomToolPromptForbidden is returned by noCustomToolPromptSpy when
// QPromoteCustomTool is asked — it signals FR-2 is violated.
var errCustomToolPromptForbidden = errors.New("QPromoteCustomTool was asked for a diverted entry; FR-2 forbids this")

// noCustomToolPromptSpy is an Interaction implementation that fails the test immediately
// if QPromoteCustomTool is ever asked. It delegates all other questions to a stub
// category-answering implementation.
type noCustomToolPromptSpy struct {
	t    *testing.T
	stub *interactiontest.Stub
}

func (s *noCustomToolPromptSpy) AskText(ctx context.Context, q domain.TextQuestion) (domain.TextAnswer, error) {
	if q.ID == domain.QPromoteCustomTool {
		s.t.Errorf("AskText called with QPromoteCustomTool; FR-2 forbids prompting for unmappable diverted entries")
		return domain.TextAnswer{}, errCustomToolPromptForbidden
	}
	if s.stub != nil {
		return s.stub.AskText(ctx, q)
	}
	return domain.TextAnswer{Status: domain.SkippedOne}, nil
}

func (s *noCustomToolPromptSpy) SelectOne(ctx context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	if q.ID == domain.QPromoteCategory {
		return domain.ChoiceAnswer{Status: domain.Answered, OptionID: "TestCategory"}, nil
	}
	if s.stub != nil {
		return s.stub.SelectOne(ctx, q)
	}
	return domain.ChoiceAnswer{Status: domain.SkippedOne}, nil
}

func (s *noCustomToolPromptSpy) SelectMany(ctx context.Context, q domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	if s.stub != nil {
		return s.stub.SelectMany(ctx, q)
	}
	return domain.MultiChoiceAnswer{Status: domain.SkippedOne}, nil
}

func (s *noCustomToolPromptSpy) Confirm(ctx context.Context, q domain.Question) (domain.ConfirmAnswer, error) {
	if s.stub != nil {
		return s.stub.Confirm(ctx, q)
	}
	return domain.ConfirmAnswer{Confirm: true}, nil
}

func (s *noCustomToolPromptSpy) Notify(ctx context.Context, n domain.Notice) {
	if s.stub != nil {
		s.stub.Notify(ctx, n)
	}
}

func (s *noCustomToolPromptSpy) Progress(ctx context.Context, e domain.ProgressEvent) {
	if s.stub != nil {
		s.stub.Progress(ctx, e)
	}
}

func (s *noCustomToolPromptSpy) Review(ctx context.Context, p domain.Plan) (domain.ConfirmAnswer, error) {
	if s.stub != nil {
		return s.stub.Review(ctx, p)
	}
	return domain.ConfirmAnswer{Confirm: true}, nil
}
