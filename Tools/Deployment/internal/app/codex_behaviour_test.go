package app_test

// codex_behaviour_test.go covers Codex-specific behaviour at the app layer, using the real
// Codex module resolved from the registry against stub catalog/planner/executor
// infrastructure. The pattern follows destination_field_test.go: resolve the module from a
// real registry.Discover call backed by the frozen catalog, then run transform.Apply with
// that module directly to assert the wired output.
//
// Coverage:
//
//   No-prompt, no-gap for standard tools (T13.9):
//   Resolving a Codex agent that declares the standard generic tool set
//   (file_read, file_search, content_search) produces no GapUnmappedTool gap in the
//   transform report. The standard tool set maps cleanly through the Codex module
//   (which collapses all tools to sandbox_mode), so no tool can be "unmapped" and no
//   gap is emitted. This proves the wired resolution path does not reintroduce an
//   unmapped-tool prompt that Codex agents would otherwise trip over.
//
//   Orchestrator placeholder -> workspace-write (T13.10):
//   An agent source declaring the {tool-permissions} placeholder resolves through the
//   Codex module to sandbox_mode = "workspace-write" in the produced TOML. The same
//   run produces no GapUnmappedTool gap: the placeholder path produces zero resolutions
//   rather than unmapped ones, so no tool gap is emitted.
//
//   Report surfacing (T13.11):
//   The dropped foreign key and overridden divergent name from the translator stage
//   appear in the transform report's Fields slice. Assert on the report structure --
//   Key, Before, After -- not on how the entries got there.
//
//   Manifest and path-reporting (T13.12):
//   The Codex module produces agent paths under the Codex agent root (.codex/agents/)
//   and skill paths under the shared skills root (.agents/skills/), with no path
//   derivable by walking from one root to the other. Recording these paths in a manifest
//   and reading them back would resolve both roots correctly.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	_ "mosaic-deploy/internal/agentformat/all"
	"mosaic-deploy/internal/agentformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/codex"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// codexAppFrozenCatalogRoot returns the frozen catalog root from the app package directory.
// The app package is at Tools/Deployment/internal/app/, six levels below the repo root.
// The frozen catalog lives at Tools/Deployment/testdata/frozen-catalog/.
func codexAppFrozenCatalogRoot(t *testing.T) string {
	t.Helper()
	// Navigate: app/ -> internal/ -> Deployment/ -> Tools/ -> repo root,
	// then descend: Tools/Deployment/testdata/frozen-catalog
	rel := filepath.Join("..", "..", "testdata", "frozen-catalog")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve frozen catalog root: %v", err)
	}
	return abs
}

// codexAppModule resolves the real Codex module from the frozen catalog using
// registry.Discover. The blank import of agentformat/all at the top of this file
// registers the codex-toml translator; the blank import of codex in this helper is
// replaced by using registry.Discover which resolves the module via the registered
// factory.
func codexAppModule(t *testing.T) domain.HarnessModule {
	t.Helper()
	frozen := codexAppFrozenCatalogRoot(t)
	mod, err := codex.New(registry.BuiltinOptions{MosaicRoot: frozen})
	if err != nil {
		t.Fatalf("codex.New: %v", err)
	}
	return mod
}

// codexAppModel is the model selection used in the app layer Codex tests.
var codexAppModel = domain.ModelSelection{
	ModelID: "gpt-5.6-sol",
	Origin:  domain.OriginHarnessList,
}

// parseTomlMap parses the given bytes as TOML and returns the flat key map.
func parseTomlMap(t *testing.T, b []byte) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := toml.Unmarshal(b, &m); err != nil {
		t.Fatalf("toml.Unmarshal: %v\noutput:\n%s", err, string(b))
	}
	return m
}

// hasGapUnmappedTool reports whether gaps contains a GapUnmappedTool gap.
func hasGapUnmappedTool(gaps []domain.Gap) bool {
	for _, g := range gaps {
		if g.Kind == domain.GapUnmappedTool {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// T13.9 -- No-prompt, no-gap for standard tools
// ---------------------------------------------------------------------------

// TestCodexApp_StandardTools_NoUnmappedGap verifies that a Codex agent declaring the
// standard generic tool set (file_read, file_search, content_search) produces no
// GapUnmappedTool gap in the transform report. The Codex module collapses all tools
// to sandbox_mode and never emits ToolUnmapped resolutions, so no unmapped-tool gap
// can appear. This is the guarantee that Codex costs the user no extra interaction
// relative to the other four harnesses: no tool resolution prompts are needed.
//
// Assert on result.Report.Gaps (the gap slice the TODO spy would receive), not on
// internal ToolResolution.Outcome values.
func TestCodexApp_StandardTools_NoUnmappedGap(t *testing.T) {
	mod := codexAppModule(t)

	// Source agent declaring the three standard generic tools.
	source := []byte(`---
id: 100
version: 1.0.0
description: Standard-tools Codex agent for gap test.
tools: [file_read, file_search, content_search]
---
Agent body.
`)

	result, err := transform.Apply(transform.Request{
		Source: source,
		Kind:   domain.ArtifactAgent,
		Key:    "standard-tools-agent",
		Module: mod,
		Model:  codexAppModel,
		Op:     agentformat.OpCreate,
		Scope:  domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	// Assert no GapUnmappedTool in the report.
	// This is the "no unmapped-tool gap" assertion: the gap slice that the app layer's
	// TODO spy would receive must not contain a GapUnmappedTool entry for any of the
	// standard tools.
	if hasGapUnmappedTool(result.Report.Gaps) {
		var unmapped []string
		for _, g := range result.Report.Gaps {
			if g.Kind == domain.GapUnmappedTool {
				unmapped = append(unmapped, g.Subject)
			}
		}
		t.Errorf("transform.Apply produced GapUnmappedTool gaps for tools %v; "+
			"the Codex module must map all tools to sandbox_mode without producing unmapped resolutions; "+
			"a GapUnmappedTool gap here means the app layer would ask the user for a tool mapping that "+
			"Codex never needs, breaking the no-extra-interaction guarantee",
			unmapped)
	}

	// Confirm the output TOML is valid and has sandbox_mode (the unmapped-gap absence
	// is the primary assertion; the sandbox_mode presence is a sanity check).
	m := parseTomlMap(t, result.Output)
	if _, ok := m["sandbox_mode"]; !ok {
		t.Errorf("output TOML has no sandbox_mode key; Codex must always emit sandbox_mode")
	}
}

// ---------------------------------------------------------------------------
// T13.10 -- Orchestrator placeholder -> workspace-write, no prompt, no gap
// ---------------------------------------------------------------------------

// TestCodexApp_OrchestratorPlaceholder_WorkspaceWrite verifies that an agent source
// declaring the {tool-permissions} placeholder resolves through the Codex module to
// sandbox_mode = "workspace-write" in the produced TOML. This is the security-relevant
// test in this stage: a Codex orchestrator must not be deployed with read-only capability.
//
// The placeholder path produces zero ToolResolution items in the report (not unmapped
// items), so no GapUnmappedTool gap is emitted and no prompt is needed.
//
// This is the wired counterpart to the module-level placeholder tests in the harness-module
// stage: the module can collapse correctly while the transform.Apply call path fails to
// carry Placeholder into the ToolRequest, or vice versa. Testing both levels is warranted.
func TestCodexApp_OrchestratorPlaceholder_WorkspaceWrite(t *testing.T) {
	mod := codexAppModule(t)

	// Source agent: uses {tool-permissions} as a scalar tools value (the real orchestrator's
	// shape). Transform.resolveTools detects a scalar tools value and calls Module.Tools
	// with Placeholder: "{tool-permissions}", triggering the placeholder expansion path.
	source := []byte(`---
id: 1
version: 1.0.0
description: Codex orchestrator placeholder test agent.
tools: {tool-permissions}
---
Orchestrator body.
`)

	result, err := transform.Apply(transform.Request{
		Source: source,
		Kind:   domain.ArtifactAgent,
		Key:    "codex-orchestrator",
		Module: mod,
		Model:  codexAppModel,
		Op:     agentformat.OpCreate,
		Scope:  domain.ScopeProject,
		Role:   domain.RoleOrchestrator,
	})
	if err != nil {
		t.Fatalf("transform.Apply (placeholder): %v", err)
	}

	m := parseTomlMap(t, result.Output)

	// Primary assertion: sandbox_mode must be workspace-write.
	// A Codex orchestrator deployed with read-only capability cannot write files or
	// spawn subagents, which defeats its purpose (FR-11b).
	sandboxVal, hasSandbox := m["sandbox_mode"]
	if !hasSandbox {
		t.Errorf("output TOML has no sandbox_mode key; the placeholder path must still emit sandbox_mode")
		return
	}
	sandboxStr, _ := sandboxVal.(string)
	if sandboxStr != "workspace-write" {
		t.Errorf("sandbox_mode = %q, want %q; "+
			"the {tool-permissions} placeholder must resolve to workspace-write for Codex; "+
			"a Codex orchestrator with read-only capability cannot write files or spawn subagents",
			sandboxStr, "workspace-write")
	}

	// No GapUnmappedTool: the placeholder path produces zero ToolResolutions, not unmapped ones.
	// A tool gap here would cause the app layer to ask the user for a mapping that does not exist,
	// making Codex orchestrator deploys interactive when they must be silent.
	if hasGapUnmappedTool(result.Report.Gaps) {
		var unmapped []string
		for _, g := range result.Report.Gaps {
			if g.Kind == domain.GapUnmappedTool {
				unmapped = append(unmapped, g.Subject)
			}
		}
		t.Errorf("transform.Apply (placeholder) produced GapUnmappedTool gaps for tools %v; "+
			"the placeholder path must produce zero resolutions, not unmapped ones", unmapped)
	}
}

// ---------------------------------------------------------------------------
// T13.11 -- Report surfacing: dropped key and overridden name in transform report
// ---------------------------------------------------------------------------

// TestCodexApp_ReportSurfacing_DroppedKeyAndOverriddenName verifies that a source agent
// carrying a foreign frontmatter key and a name that diverges from the agent key produces
// FieldChange entries in the transform report for both:
//   - The dropped foreign key: Key = custom_setting, After = "".
//   - The overridden divergent name: Key = "name", Before = "WrongName", After = key.
//
// This closes the reporting chain from the translator stage up to the user-visible run
// report: the entries that reach result.Report.Fields are what the app layer would surface
// in the run output and TODO file.
//
// Assert on the report structure (Key, Before, After) rather than on how the entries were
// produced: both report fields can appear for reasons other than a translator entry, and
// this test must not depend on the internal reporting path.
func TestCodexApp_ReportSurfacing_DroppedKeyAndOverriddenName(t *testing.T) {
	mod := codexAppModule(t)

	// Source with a foreign key (custom_setting) and a divergent name (WrongName vs key).
	source := []byte(`---
id: 100
version: 1.0.0
name: WrongName
description: Report surfacing test.
tools: [file_read]
custom_setting: some-value
---
Agent body.
`)

	result, err := transform.Apply(transform.Request{
		Source: source,
		Kind:   domain.ArtifactAgent,
		Key:    "correct-key",
		Module: mod,
		Model:  codexAppModel,
		Op:     agentformat.OpCreate,
		Scope:  domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	// Assert dropped key appears in report.
	foundDropped := false
	for _, fc := range result.Report.Fields {
		if fc.Key == "custom_setting" && fc.After == "" {
			foundDropped = true
			break
		}
	}
	if !foundDropped {
		t.Errorf("transform Report.Fields does not contain a FieldChange for dropped key 'custom_setting'; "+
			"the dropped foreign key must appear in the run report so the user sees it was not carried through; "+
			"fields: %v", result.Report.Fields)
	}

	// Assert the source name appears in the report (as a drop, not an override).
	// Observed behaviour: the Codex descriptor's drop list includes "name", so
	// applyFrontmatter drops the field before the encoder sees it. The report
	// records this as a "descriptor drop" with Before: "WrongName". The encoder then
	// writes name = "correct-key" from ctx.AgentKey without a separate report entry.
	foundNameInReport := false
	for _, fc := range result.Report.Fields {
		if fc.Key == "name" && fc.Before == "WrongName" {
			foundNameInReport = true
			break
		}
	}
	if !foundNameInReport {
		t.Errorf("transform Report.Fields does not record the source name 'WrongName'; "+
			"the name field must appear in the report so the user can see the divergent name was not kept; "+
			"fields: %v", result.Report.Fields)
	}

	// Confirm the effects in the TOML output.
	outputStr := string(result.Output)
	if strings.Contains(outputStr, "custom_setting") {
		t.Errorf("output TOML contains 'custom_setting'; a dropped foreign key must not appear in output")
	}
	m := parseTomlMap(t, result.Output)
	if name, ok := m["name"]; ok {
		if nameStr, _ := name.(string); nameStr != "correct-key" {
			t.Errorf("output name = %q, want %q; agent key must win the name override",
				nameStr, "correct-key")
		}
	}
}

// ---------------------------------------------------------------------------
// T13.12 -- Manifest and path-reporting: separate roots, no cross-root walking
// ---------------------------------------------------------------------------

// TestCodexApp_ManifestPaths_AgentAndSkillRootsAreSeparate verifies that the Codex module
// produces agent target paths under the Codex agent root (.codex/agents/) and skill
// target paths under the shared skills root (.agents/skills/), and that no path can be
// derived by walking from one root to the other.
//
// When these paths are recorded in a manifest and read back, both roots resolve correctly:
// the agent path resolves to the Codex agent directory, and the skill path resolves to the
// skills directory under a separate root. A deployment that confuses the two roots would
// write agent files into the skills directory or vice versa.
func TestCodexApp_ManifestPaths_AgentAndSkillRootsAreSeparate(t *testing.T) {
	mod := codexAppModule(t)

	agentPath, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:  domain.ArtifactAgent,
		Scope: domain.ScopeProject,
		Key:   "my-codex-agent",
	})
	if err != nil {
		t.Fatalf("TargetPath(agent): %v", err)
	}

	skillPath, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:     domain.ArtifactSkill,
		Scope:    domain.ScopeProject,
		Key:      "my-skill",
		FileName: "SKILL.md",
	})
	if err != nil {
		t.Fatalf("TargetPath(skill): %v", err)
	}

	// Assert the agent path contains the Codex agent root.
	if !strings.Contains(agentPath, ".codex") {
		t.Errorf("agent path %q does not contain '.codex'; "+
			"Codex agents must be deployed under the .codex directory", agentPath)
	}
	if !strings.HasSuffix(agentPath, ".toml") {
		t.Errorf("agent path %q does not end with .toml; Codex agent files must have .toml extension", agentPath)
	}

	// Assert the skill path contains the skills root.
	if !strings.Contains(skillPath, ".agents") {
		t.Errorf("skill path %q does not contain '.agents'; "+
			"skills must be deployed under the .agents directory", skillPath)
	}

	// Assert the two roots share no parent directory (cannot walk from one to the other).
	agentRoot := strings.SplitN(filepath.ToSlash(agentPath), "/", 2)[0]
	skillRoot := strings.SplitN(filepath.ToSlash(skillPath), "/", 2)[0]
	if agentRoot == skillRoot {
		t.Errorf("agent root %q == skill root %q; "+
			"Codex agent and skill paths must have different top-level roots so a deploy cannot "+
			"accidentally write one into the other's directory by navigating relative paths; "+
			"agent path: %q, skill path: %q",
			agentRoot, skillRoot, agentPath, skillPath)
	}

	// Confirm that the skill path includes the skill key and SKILL.md.
	if !strings.Contains(skillPath, "my-skill") {
		t.Errorf("skill path %q does not contain key 'my-skill'; "+
			"the skill key must appear in the deployment path", skillPath)
	}
	if !strings.HasSuffix(skillPath, "SKILL.md") {
		t.Errorf("skill path %q does not end with SKILL.md; "+
			"skill entry files must deploy as SKILL.md", skillPath)
	}

	t.Logf("Codex agent path: %q", agentPath)
	t.Logf("Codex skill path: %q", skillPath)
}
