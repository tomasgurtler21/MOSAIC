package ghcpcli_test

// Tests for the GitHub Copilot CLI built-in harness module.
//
// Coverage:
//   Golden file tests (tool-light, tool-heavy, skill-using, orchestrator agents):
//   - A transform of the generic contracts-review agent produces a flow-style single-quoted
//     tool list ['skill', 'read', 'edit', 'search', 'ask_user'] with user-invocable: false.
//   - A transform of the generic test-runner agent produces ['read', 'edit', 'search', 'execute',
//     'ask_user'] demonstrating many-to-one aliasing (file_write + file_edit → edit;
//     file_search + content_search → search).
//   - A transform of the generic planner-tdd-soft agent includes skill and execute, showing
//     that skill maps to the 'skill' harness tool and terminal maps to 'execute'.
//   - A transform of the generic orchestrator produces the placeholder expansion as a
//     flow-style list without 'skill' (orchestrators do not use skills).
//
//   Many-to-one aliasing:
//   - file_write and file_edit both map to 'edit'; only one 'edit' entry appears in output.
//   - file_search and content_search both map to 'search'; only one 'search' appears.
//   - Resolutions report both generic tools as ToolMapped with HarnessTools: ["edit"/"search"].
//
//   Deployment path resolution:
//   - Project-scoped agent path is ".github/agents/<key>.agent.md"
//   - Project-scoped skill path is ".github/skills/<key>/SKILL.md" (key subdirectory prevents collisions)
//   - Hook artifact kind resolves via descriptor to ".github/hooks/<filename>"
//   - Any non-project scope returns domain.ErrUnsupportedScope (via the shared contracttest universal invariant)
//
//   Hook support:
//   - HookPlan returns Supported: true for any bundle (including an empty bundle with no variants).
//   - HookPlan.Reason is empty when Supported is true.
//   - HookPlan.TargetDir is ".github/hooks" when Supported is true.
//   - HookPlan.Files contains the ghcp-cli variant files when the bundle carries a "ghcp-cli" variant.
//   - TargetPath for ArtifactHook resolves via descriptor; returns a path under ".github/hooks/".
//
//   Harness-level injections:
//   - HarnessConstraints is filled with the parallel tool calls instruction text.
//   - LanguagePatterns is not filled — it is a project-authored injection name, not declared
//     by this harness.
//   - injections_version is "1.2.0" per the GHCP CLI descriptor.
//   - Project-class injections are not filled by the harness.
//
//   Role-conditional user-invocable (TDD RED - implementation pending):
//   - user-invocable is false for subagent role.
//   - user-invocable is true for orchestrator role.
//   - user-invocable is true for utility role.
//   - user-invocable is true for standalone role.
//   - user-invocable is absent from Set when no role is provided (empty/zero value).
//
//   Shared contract:
//   - Both modules pass contracttest.Run with identical universal invariant results.

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/ghcpcli"
	"mosaic-deploy/internal/harness/contracttest"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/transform"
)

// updateGolden regenerates the golden files from current engine output when -update is passed.
// Run: go test ./internal/harness/builtin/ghcpcli/... -run TestGoldenFile -update
var updateGolden = flag.Bool("update", false, "regenerate golden files from current engine output")

// testModel is the ModelSelection used in all GHCP CLI golden file tests.
var testModel = domain.ModelSelection{
	ModelID: "claude-sonnet-4-6",
	Origin:  domain.OriginHarnessList,
}

// ghcpHarnessConstraints is the expected content of the HarnessConstraints injection for GHCP CLI.
const ghcpHarnessConstraints = "**Parallel Tool Calls:** Issue multiple independent tool calls in a single response whenever possible. Sequential tool calls are only permitted when a later call depends on the result of an earlier one. This minimises inference API calls to improve speed and reduce cost."

func repoRoot(t *testing.T) string {
	t.Helper()
	// Package is at Tools/Deployment/internal/harness/builtin/ghcpcli/
	rel := filepath.Join("..", "..", "..", "..", "..", "..")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return abs
}

func goldenDir(t *testing.T) string {
	t.Helper()
	rel := filepath.Join("..", "..", "..", "..", "testdata", "golden", "ghcp-cli")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve golden dir: %v", err)
	}
	return abs
}

// agentFixturesDir returns the path to the frozen agent source fixtures used by golden file
// tests. These fixtures are small, stable files that exercise specific transform behaviours
// (tool-light, tool-heavy, skill-using, placeholder-expanding) without coupling the tests to
// the live Catalog/ tree. Editing a live catalog agent leaves these tests unaffected.
func agentFixturesDir(t *testing.T) string {
	t.Helper()
	// testdata/agent-fixtures/ is at Tools/Deployment/testdata/agent-fixtures/
	rel := filepath.Join("..", "..", "..", "..", "testdata", "agent-fixtures")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve agent fixtures dir: %v", err)
	}
	return abs
}

// frozenCatalogRoot returns the absolute path to the frozen Catalog fixture tree. This tree
// is a snapshot of the protocol source document and all HarnessInjections files at the time
// the golden files were last regenerated. Golden-file tests must use this root instead of the
// live repo root so they remain immune to Catalog evolution (version bumps, prose edits, etc.).
func frozenCatalogRoot(t *testing.T) string {
	t.Helper()
	// testdata/frozen-catalog/ is at Tools/Deployment/testdata/frozen-catalog/
	rel := filepath.Join("..", "..", "..", "..", "testdata", "frozen-catalog")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve frozen catalog root: %v", err)
	}
	return abs
}

func newModule(t *testing.T) domain.HarnessModule {
	t.Helper()
	mod, err := ghcpcli.New(registry.BuiltinOptions{MosaicRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("ghcpcli.New(): %v", err)
	}
	return mod
}

// newModuleFromFrozen constructs the GHCP CLI module against the frozen Catalog fixture root.
// Golden-file tests use this instead of newModule so that changes to the live HarnessInjections
// files in Catalog/ do not affect the committed golden files.
func newModuleFromFrozen(t *testing.T) domain.HarnessModule {
	t.Helper()
	mod, err := ghcpcli.New(registry.BuiltinOptions{MosaicRoot: frozenCatalogRoot(t)})
	if err != nil {
		t.Fatalf("ghcpcli.New() from frozen catalog: %v", err)
	}
	return mod
}

// loadProtocol loads the protocol content from the repository root for use in transform requests.
// All agents in Catalog/Subagents/ carry a <CommunicationProtocol type="managed"> region, so Protocol
// must be populated for every transform.Apply call that processes those source files.
func loadProtocol(t *testing.T, root string) domain.ProtocolContent {
	t.Helper()
	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)
	if err != nil {
		t.Fatalf("load protocol from %s: %v", root, err)
	}
	return content
}

func applyAndCompare(t *testing.T, mod domain.HarnessModule, req transform.Request, goldenPath string) {
	t.Helper()
	result, err := transform.Apply(req)
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, result.Output, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		t.Logf("updated golden file: %s (%d bytes)", goldenPath, len(result.Output))
		return
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v\n(run go test -update to generate it)", goldenPath, err)
	}

	if !bytes.Equal(result.Output, golden) {
		first := firstDiff(result.Output, golden)
		t.Errorf("output does not match golden file %s\n"+
			"output: %d bytes, golden: %d bytes, first difference at byte %d\n\n"+
			"--- output (first 800 bytes) ---\n%s\n\n--- golden (first 800 bytes) ---\n%s",
			goldenPath,
			len(result.Output), len(golden), first,
			truncate(result.Output, 800),
			truncate(golden, 800),
		)
	}
}

func firstDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func truncate(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}

// ---------------------------------------------------------------------------
// Golden file tests
// ---------------------------------------------------------------------------

// TestGoldenFile_GHCP_ContractsReviewAgent tests a "tool-light" agent: contracts-review uses
// [skill, file_read, file_write, file_edit, file_search, content_search, user_interaction].
// The output should have: tools: ['skill', 'read', 'edit', 'search', 'ask_user']
// Aliasing: file_write + file_edit → edit (one entry); file_search + content_search → search.
// The file uses the .agent.md extension and adds user-invocable: false.
//
// Deliberate deviations from Agents/GHCP CLI/CodebaseAgnostic/Agents/contracts-review.agent.md:
// the committed file shows "tools: [skill, read, edit, search, ask_user]" (no quotes on items),
// whereas the canonical GHCP CLI format uses single-quoted flow style: ['skill', 'read', ...].
// The golden file uses the correct single-quoted flow style.
func TestGoldenFile_GHCP_ContractsReviewAgent(t *testing.T) {
	mod := newModuleFromFrozen(t)
	protocol := loadProtocol(t, frozenCatalogRoot(t))


	srcPath := filepath.Join(agentFixturesDir(t), "contracts-review.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("frozen fixture not found at %s: %v", srcPath, err)
	}

	req := transform.Request{
		Source:     src,
		Kind:       domain.ArtifactAgent,
		Key:        "contracts-review",
		Module:     mod,
		Model:      testModel,
		Scope:      domain.ScopeProject,
		Role:       domain.RoleWorker,
		Protocol:   protocol,

	}

	goldenPath := filepath.Join(goldenDir(t), "contracts-review.agent.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// TestGoldenFile_GHCP_TestRunnerAgent tests the tool-heavy case: test-runner uses all seven
// generic tools [file_read, file_write, file_edit, file_search, content_search, terminal,
// user_interaction]. The output demonstrates the canonical GHCP CLI many-to-one aliasing:
// tools: ['read', 'edit', 'search', 'execute', 'ask_user']
// (file_write + file_edit → edit; file_search + content_search → search; no duplication)
func TestGoldenFile_GHCP_TestRunnerAgent(t *testing.T) {
	mod := newModuleFromFrozen(t)
	protocol := loadProtocol(t, frozenCatalogRoot(t))


	srcPath := filepath.Join(agentFixturesDir(t), "test-runner.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("frozen fixture not found at %s: %v", srcPath, err)
	}

	req := transform.Request{
		Source:     src,
		Kind:       domain.ArtifactAgent,
		Key:        "test-runner",
		Module:     mod,
		Model:      testModel,
		Scope:      domain.ScopeProject,
		Role:       domain.RoleWorker,
		Protocol:   protocol,

	}

	goldenPath := filepath.Join(goldenDir(t), "test-runner.agent.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// TestGoldenFile_GHCP_PlannerTDDSoftAgent tests the skill-and-tool combination:
// planner-tdd-soft uses [skill, file_read, file_write, file_edit, file_search, content_search,
// terminal, user_interaction]. The output should include 'skill' (maps to skill harness tool)
// and 'execute' (maps from terminal):
// tools: ['skill', 'read', 'edit', 'search', 'execute', 'ask_user']
func TestGoldenFile_GHCP_PlannerTDDSoftAgent(t *testing.T) {
	mod := newModuleFromFrozen(t)
	protocol := loadProtocol(t, frozenCatalogRoot(t))


	srcPath := filepath.Join(agentFixturesDir(t), "planner-tdd-soft.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("frozen fixture not found at %s: %v", srcPath, err)
	}

	req := transform.Request{
		Source:     src,
		Kind:       domain.ArtifactAgent,
		Key:        "planner-tdd-soft",
		Module:     mod,
		Model:      testModel,
		Scope:      domain.ScopeProject,
		Role:       domain.RoleWorker,
		Protocol:   protocol,

	}

	goldenPath := filepath.Join(goldenDir(t), "planner-tdd-soft.agent.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// TestGoldenFile_GHCP_Orchestrator tests the {tool-permissions} placeholder expansion.
// The orchestrator uses {tool-permissions} as its tools value. The placeholder expands to
// the GHCP CLI placeholder_expansion: ['read', 'edit', 'search', 'execute', 'ask_user', 'agent'].
// (skill is excluded from the placeholder expansion because orchestrators do not use skills)
//
// Deliberate deviation from Agents/GHCP CLI/CodebaseAgnostic/orchestrator.agent.md:
// the committed file shows tools: ['read', 'edit', 'ask_user', 'agent'], missing 'search'
// and 'execute'. This appears to be an error in the committed file produced by the rough
// LLM-based process. The correct expansion should include all placeholder_expansion tools.
func TestGoldenFile_GHCP_Orchestrator(t *testing.T) {
	mod := newModuleFromFrozen(t)
	protocol := loadProtocol(t, frozenCatalogRoot(t))


	srcPath := filepath.Join(agentFixturesDir(t), "orchestrator.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("frozen fixture not found at %s: %v", srcPath, err)
	}

	req := transform.Request{
		Source:     src,
		Kind:       domain.ArtifactAgent,
		Key:        "orchestrator",
		Module:     mod,
		Model:      testModel,
		Scope:      domain.ScopeProject,
		Role:       domain.RoleOrchestrator,
		Protocol:   protocol,

	}

	goldenPath := filepath.Join(goldenDir(t), "orchestrator.agent.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// ---------------------------------------------------------------------------
// Many-to-one aliasing tests
// ---------------------------------------------------------------------------

// TestToolAliasing_FileWriteAndEditCollapse verifies that file_write and file_edit both map
// to the 'edit' harness tool and that exactly one 'edit' entry appears in the output.
func TestToolAliasing_FileWriteAndEditCollapse(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-aliasing",
		Generic:  []string{"file_write", "file_edit"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	// Both resolutions should map to "edit".
	for i, res := range result.Resolutions {
		if res.Outcome != domain.ToolMapped {
			t.Errorf("Resolutions[%d].Outcome = %q, want ToolMapped", i, res.Outcome)
		}
		if len(res.HarnessTools) != 1 || res.HarnessTools[0] != "edit" {
			t.Errorf("Resolutions[%d].HarnessTools = %v, want [\"edit\"]", i, res.HarnessTools)
		}
	}

	// The rendered tools field should contain exactly one "edit" item.
	editCount := 0
	for _, field := range result.Fields {
		if field.Key == "tools" && field.Value.Kind == domain.KindList {
			for _, item := range field.Value.Items {
				if item.Scalar == "edit" {
					editCount++
				}
			}
		}
	}
	if editCount != 1 {
		t.Errorf("'edit' appears %d times in the tools field; want exactly 1 (many-to-one deduplication)", editCount)
	}
}

// TestToolAliasing_FileSearchAndContentSearchCollapse verifies that file_search and
// content_search both map to 'search' and appear only once in the output.
func TestToolAliasing_FileSearchAndContentSearchCollapse(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-aliasing",
		Generic:  []string{"file_search", "content_search"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	for i, res := range result.Resolutions {
		if res.Outcome != domain.ToolMapped {
			t.Errorf("Resolutions[%d].Outcome = %q, want ToolMapped", i, res.Outcome)
		}
		if len(res.HarnessTools) != 1 || res.HarnessTools[0] != "search" {
			t.Errorf("Resolutions[%d].HarnessTools = %v, want [\"search\"]", i, res.HarnessTools)
		}
	}

	searchCount := 0
	for _, field := range result.Fields {
		if field.Key == "tools" && field.Value.Kind == domain.KindList {
			for _, item := range field.Value.Items {
				if item.Scalar == "search" {
					searchCount++
				}
			}
		}
	}
	if searchCount != 1 {
		t.Errorf("'search' appears %d times in the tools field; want exactly 1", searchCount)
	}
}

// TestToolAliasing_FullTestRunnerSet verifies the complete many-to-one aliasing for the
// test-runner tool set (7 generic tools → 5 unique harness tools).
func TestToolAliasing_FullTestRunnerSet(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-runner",
		Generic:  []string{"file_read", "file_write", "file_edit", "file_search", "content_search", "terminal", "user_interaction"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	// Verify resolution count.
	if len(result.Resolutions) != 7 {
		t.Errorf("Resolutions count = %d, want 7", len(result.Resolutions))
	}

	// Verify no harness tool appears more than once in the rendered output.
	seen := make(map[string]int)
	for _, field := range result.Fields {
		if field.Key == "tools" && field.Value.Kind == domain.KindList {
			for _, item := range field.Value.Items {
				seen[item.Scalar]++
			}
		}
	}
	for tool, count := range seen {
		if count > 1 {
			t.Errorf("harness tool %q appears %d times in output; want at most 1 (deduplication required)", tool, count)
		}
	}

	// Verify expected harness tool set.
	wantTools := map[string]bool{"read": true, "edit": true, "search": true, "execute": true, "ask_user": true}
	for tool := range wantTools {
		if seen[tool] == 0 {
			t.Errorf("expected harness tool %q not found in output", tool)
		}
	}
}

// TestToolFormat_FlowStyleSingleQuoted verifies that GHCP CLI emits tools as a flow-style
// YAML sequence with single-quoted items: tools: ['read', 'edit'].
func TestToolFormat_FlowStyleSingleQuoted(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "format-check",
		Generic:  []string{"file_read", "terminal"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	var toolsField *domain.FrontmatterField
	for i, f := range result.Fields {
		if f.Key == "tools" {
			toolsField = &result.Fields[i]
			break
		}
	}
	if toolsField == nil {
		t.Fatal("tools field not found in result.Fields")
	}
	if toolsField.Value.Kind != domain.KindList {
		t.Fatalf("tools field Kind = %q, want KindList", toolsField.Value.Kind)
	}
	if toolsField.Value.List != domain.ListFlow {
		t.Errorf("tools field List style = %q, want ListFlow (GHCP CLI uses flow-style [...])", toolsField.Value.List)
	}
	for i, item := range toolsField.Value.Items {
		if item.Quote != domain.QuoteSingle {
			t.Errorf("tools field Items[%d].Quote = %q, want QuoteSingle (GHCP CLI uses 'single-quoted' items)", i, item.Quote)
		}
	}
}

// ---------------------------------------------------------------------------
// Deployment path resolution tests
// ---------------------------------------------------------------------------

// TestTargetPath_GHCP_AgentExtension verifies that GHCP CLI agents use the .agent.md extension.
func TestTargetPath_GHCP_AgentExtension(t *testing.T) {
	mod := newModule(t)
	path, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:  domain.ArtifactAgent,
		Key:   "test-runner",
		Scope: domain.ScopeProject,
		GOOS:  "linux",
	})
	if err != nil {
		t.Fatalf("TargetPath: %v", err)
	}
	want := ".github/agents/test-runner.agent.md"
	if path != want {
		t.Errorf("TargetPath = %q, want %q", path, want)
	}
}

// TestTargetPath_GHCP_SkillProjectScope verifies that a GHCP CLI skill is deployed under a
// key-named subdirectory: ".github/skills/<key>/SKILL.md". The key subdirectory is required
// because all skill entry files are named SKILL.md by convention.
func TestTargetPath_GHCP_SkillProjectScope(t *testing.T) {
	mod := newModule(t)
	path, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:     domain.ArtifactSkill,
		Key:      "lean-tdd",
		FileName: "SKILL.md",
		Scope:    domain.ScopeProject,
		GOOS:     "linux",
	})
	if err != nil {
		t.Fatalf("TargetPath: %v", err)
	}
	want := ".github/skills/lean-tdd/SKILL.md"
	if path != want {
		t.Errorf("TargetPath = %q, want %q", path, want)
	}
}

// TestTargetPath_GHCP_HookResolvesViaDescriptor verifies that requesting a hook target path
// succeeds and returns a path under ".github/hooks/" (resolves via the descriptor's hooks.project setting).
func TestTargetPath_GHCP_HookResolvesViaDescriptor(t *testing.T) {
	mod := newModule(t)
	got, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:     domain.ArtifactHook,
		Key:      "subagent-logger",
		FileName: "hook.sh",
		Scope:    domain.ScopeProject,
		GOOS:     "linux",
	})
	if err != nil {
		t.Fatalf("TargetPath for ArtifactHook returned error %v; want a resolved path under .github/hooks/", err)
	}
	want := ".github/hooks/hook.sh"
	if got != want {
		t.Errorf("TargetPath for ArtifactHook = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Hook support tests
// ---------------------------------------------------------------------------

// TestHookPlan_GHCP_Supported verifies that HookPlan returns Supported: true for GHCP CLI,
// reflecting that the descriptor now declares hooks as supported with a "ghcp-cli" variant key.
func TestHookPlan_GHCP_Supported(t *testing.T) {
	mod := newModule(t)

	plan, err := mod.HookPlan(domain.HookPlanRequest{})
	if err != nil {
		t.Fatalf("HookPlan: %v", err)
	}
	if !plan.Supported {
		t.Error("HookPlan.Supported = false; GHCP CLI descriptor declares hooks supported and HookPlan must return Supported: true")
	}
}

// TestHookPlan_GHCP_ReasonEmptyWhenSupported verifies that HookPlan.Reason is empty when
// Supported is true. A non-empty reason is only meaningful for an unsupported harness.
func TestHookPlan_GHCP_ReasonEmptyWhenSupported(t *testing.T) {
	mod := newModule(t)

	plan, err := mod.HookPlan(domain.HookPlanRequest{})
	if err != nil {
		t.Fatalf("HookPlan: %v", err)
	}
	if plan.Reason != "" {
		t.Errorf("HookPlan.Reason = %q; must be empty when Supported is true", plan.Reason)
	}
}

// TestHookPlan_GHCP_NoFilesWhenBundleEmpty verifies that when no bundle variant is provided
// (empty bundle), HookPlan still returns Supported: true but Files is empty because there
// is no "ghcp-cli" variant in the bundle to pull files from.
func TestHookPlan_GHCP_NoFilesWhenBundleEmpty(t *testing.T) {
	mod := newModule(t)

	plan, err := mod.HookPlan(domain.HookPlanRequest{})
	if err != nil {
		t.Fatalf("HookPlan: %v", err)
	}
	if !plan.Supported {
		t.Fatal("HookPlan.Supported = false; want true even when bundle has no variant")
	}
	if len(plan.Files) > 0 {
		t.Errorf("HookPlan.Files has %d entries when bundle has no ghcp-cli variant; want empty", len(plan.Files))
	}
}

// TestHookPlan_GHCP_TargetDir verifies that HookPlan returns TargetDir ".github/hooks" when
// supported, matching the hooks.project value declared in the ghcp-cli.yaml descriptor.
func TestHookPlan_GHCP_TargetDir(t *testing.T) {
	mod := newModule(t)

	plan, err := mod.HookPlan(domain.HookPlanRequest{})
	if err != nil {
		t.Fatalf("HookPlan: %v", err)
	}
	want := ".github/hooks"
	if plan.TargetDir != want {
		t.Errorf("HookPlan.TargetDir = %q, want %q", plan.TargetDir, want)
	}
}

// TestHookPlan_GHCP_WithBundleVariant verifies that HookPlan returns the files and registration
// steps from the bundle's "ghcp-cli" variant when that variant is present. This mirrors the
// claude-code HookPlan test pattern and confirms the descriptor-driven variant lookup is wired
// correctly for the GHCP CLI harness.
func TestHookPlan_GHCP_WithBundleVariant(t *testing.T) {
	mod := newModule(t)

	bundle := domain.HookBundle{
		Key: "mosaic-logger",
		Variants: map[string]domain.HookVariant{
			"ghcp-cli": {
				HarnessID: "ghcp-cli",
				Supported: true,
				Files: []domain.HookFile{
					{SourcePath: "/tmp/src/mosaic_logger.py", TargetName: "mosaic_logger.py"},
					{SourcePath: "/tmp/src/mosaic_logger_core.py", TargetName: "mosaic_logger_core.py"},
				},
				Registration: []domain.RegistrationStep{
					{ID: "settings-fragment", TargetPath: ".github/hooks/mosaic-logger.json", Performable: true},
				},
			},
		},
	}

	plan, err := mod.HookPlan(domain.HookPlanRequest{Bundle: bundle})
	if err != nil {
		t.Fatalf("HookPlan with bundle variant: %v", err)
	}
	if !plan.Supported {
		t.Fatalf("HookPlan.Supported = false; want true when ghcp-cli variant is present in bundle")
	}
	if plan.TargetDir != ".github/hooks" {
		t.Errorf("HookPlan.TargetDir = %q, want %q", plan.TargetDir, ".github/hooks")
	}
	wantFileNames := []string{"mosaic_logger.py", "mosaic_logger_core.py"}
	if len(plan.Files) != len(wantFileNames) {
		t.Fatalf("HookPlan.Files has %d entries; want %d (matching bundle variant file count)", len(plan.Files), len(wantFileNames))
	}
	for i, want := range wantFileNames {
		if plan.Files[i].TargetName != want {
			t.Errorf("HookPlan.Files[%d].TargetName = %q, want %q", i, plan.Files[i].TargetName, want)
		}
	}
	if len(plan.Registration) != 1 {
		t.Fatalf("HookPlan.Registration has %d entries; want 1", len(plan.Registration))
	}
	if plan.Registration[0].ID != "settings-fragment" {
		t.Errorf("HookPlan.Registration[0].ID = %q, want %q", plan.Registration[0].ID, "settings-fragment")
	}
}

// ---------------------------------------------------------------------------
// Harness-level injection tests
// ---------------------------------------------------------------------------

// TestInjection_GHCP_HarnessConstraintsFilled verifies that GHCP CLI provides the parallel
// tool calls instruction in its HarnessConstraints injection.
func TestInjection_GHCP_HarnessConstraintsFilled(t *testing.T) {
	mod := newModule(t)
	content, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints"})
	if !ok {
		t.Fatal("Injection(\"HarnessConstraints\") returned ok=false; GHCP CLI must fill this injection")
	}
	if content != ghcpHarnessConstraints {
		t.Errorf("Injection(\"HarnessConstraints\"):\n  got:  %q\n  want: %q", content, ghcpHarnessConstraints)
	}
}

// TestInjection_GHCP_ProjectInjectionsNotFilled verifies that project-class injections are
// not filled by the GHCP CLI harness. LanguagePatterns is included here: it is a
// project-authored injection name, not a harness-declared one, so GHCP CLI must not fill it.
func TestInjection_GHCP_ProjectInjectionsNotFilled(t *testing.T) {
	mod := newModule(t)
	projectInjections := []string{
		"IdentityExtension",
		"ProtocolExtension",
		"CodebaseContext",
		"LanguagePatterns",
		"OutputArtifactTemplate",
		"CustomConstraints",
		"ErrorHandlingExtension",
		"ContextLimits",
	}
	for _, name := range projectInjections {
		_, ok := mod.Injection(domain.InjectionRequest{Name: name})
		if ok {
			t.Errorf("Injection(%q) returned ok=true; project-class injections must not be filled by the harness", name)
		}
	}
}

// TestInjection_GHCP_InjectionsVersion verifies injections_version "1.2.0" for GHCP CLI.
func TestInjection_GHCP_InjectionsVersion(t *testing.T) {
	mod := newModule(t)
	d := mod.Descriptor()
	if d == nil {
		t.Fatal("Descriptor() returned nil")
	}
	want := "1.2.0"
	if d.InjectionsVersion != want {
		t.Errorf("Descriptor().InjectionsVersion = %q, want %q", d.InjectionsVersion, want)
	}
}

// ---------------------------------------------------------------------------
// Role-conditional mechanism test
// ---------------------------------------------------------------------------

// TestGhcpCli_UserInvocable_MechanismIsRoleConditional verifies that the GHCP CLI descriptor
// implements user-invocable via the role-conditional schema (RoleConditionalAdd), not via a
// static Add entry. This is the RED-phase signal for the subagent false outcome: the current
// descriptor has a static value: false for all agents, so RoleConditionalAdd will be empty
// before implementation.
//
// RED: currently fails because ghcp-cli.yaml uses a static value: false entry. After
// implementation (I2.6), the descriptor uses value_by_role and RoleConditionalAdd is non-empty.
func TestGhcpCli_UserInvocable_MechanismIsRoleConditional(t *testing.T) {
	mod := newModule(t)
	desc := mod.Descriptor()
	if desc == nil {
		t.Fatal("Descriptor() returned nil")
	}

	// The descriptor must use value_by_role for user-invocable, which is loaded into
	// RoleConditionalAdd, not the static Add list.
	if len(desc.Frontmatter.RoleConditionalAdd) == 0 {
		t.Error("Frontmatter.RoleConditionalAdd is empty: user-invocable must be declared " +
			"with value_by_role in ghcp-cli.yaml, not as a static value entry; " +
			"the static value: false entry must be replaced by a role-conditional entry")
	}

	// The static Add list must not contain user-invocable (it must be role-conditional only).
	for _, f := range desc.Frontmatter.Add {
		if f.Key == "user-invocable" {
			t.Errorf("Frontmatter.Add contains a static %q entry; after implementation, "+
				"user-invocable must be in RoleConditionalAdd (value_by_role), not in Add (static value)", f.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// Shared contract test
// ---------------------------------------------------------------------------

// TestContract_GHCP runs the shared HarnessModule contract suite against the GHCP CLI module.
func TestContract_GHCP(t *testing.T) {
	mod := newModule(t) // RED: fails here until implementation is complete

	contracttest.Run(t, mod, contracttest.Fixtures{
		ToolCases: []contracttest.ToolCase{
			{
				// many_to_one_aliasing_write_and_edit: verifies many-to-one deduplication AND the
				// flow-style single-quoted list format that distinguishes GHCP CLI from Claude Code.
				// file_write and file_edit both resolve to "edit"; the rendered tools field contains
				// exactly one 'edit' item in a ['edit'] flow-style single-quoted sequence.
				Name: "many_to_one_aliasing_write_and_edit",
				Request: domain.ToolRequest{
					AgentKey: "aliasing-test",
					Generic:  []string{"file_write", "file_edit"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key: "tools",
						Value: domain.FieldValue{
							Kind: domain.KindList,
							List: domain.ListFlow,
							Items: []domain.FieldValue{
								{Kind: domain.KindScalar, Scalar: "edit", Quote: domain.QuoteSingle},
							},
						},
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "file_write", Outcome: domain.ToolMapped, HarnessTools: []string{"edit"}},
					{Generic: "file_edit", Outcome: domain.ToolMapped, HarnessTools: []string{"edit"}},
				},
			},
			{
				// many_to_one_aliasing_search: verifies file_search + content_search → one 'search'.
				Name: "many_to_one_aliasing_search",
				Request: domain.ToolRequest{
					AgentKey: "aliasing-test",
					Generic:  []string{"file_search", "content_search"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key: "tools",
						Value: domain.FieldValue{
							Kind: domain.KindList,
							List: domain.ListFlow,
							Items: []domain.FieldValue{
								{Kind: domain.KindScalar, Scalar: "search", Quote: domain.QuoteSingle},
							},
						},
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "file_search", Outcome: domain.ToolMapped, HarnessTools: []string{"search"}},
					{Generic: "content_search", Outcome: domain.ToolMapped, HarnessTools: []string{"search"}},
				},
			},
			{
				Name: "skill_maps_to_skill",
				Request: domain.ToolRequest{
					AgentKey: "skill-test",
					Generic:  []string{"skill"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key: "tools",
						Value: domain.FieldValue{
							Kind: domain.KindList,
							List: domain.ListFlow,
							Items: []domain.FieldValue{
								{Kind: domain.KindScalar, Scalar: "skill", Quote: domain.QuoteSingle},
							},
						},
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "skill", Outcome: domain.ToolMapped, HarnessTools: []string{"skill"}},
				},
			},
			{
				Name: "subagent_maps_to_agent",
				Request: domain.ToolRequest{
					AgentKey: "orchestrator",
					Generic:  []string{"subagent"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key: "tools",
						Value: domain.FieldValue{
							Kind: domain.KindList,
							List: domain.ListFlow,
							Items: []domain.FieldValue{
								{Kind: domain.KindScalar, Scalar: "agent", Quote: domain.QuoteSingle},
							},
						},
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "subagent", Outcome: domain.ToolMapped, HarnessTools: []string{"agent"}},
				},
			},
		},

		FrontmatterCases: []contracttest.FrontmatterCase{
			{
				// user_invocable_false_for_subagent: GHCP CLI adds "user-invocable: false" for
				// subagent-role agents. The three generic-only keys are removed, and the canonical
				// key order is applied. Model and version stamps are NOT in Set — they are applied
				// exclusively by the transform's Steps 3 and 4.
				//
				// Note: This case is a REGRESSION GUARD for preserved subagent behavior. It does
				// not fail in TDD RED phase because the current static descriptor already produces
				// user-invocable: false for all agents including subagent. After implementation,
				// the mechanism changes (role-conditional resolution), but the outcome (false for
				// subagent) is identical. The companion test
				// TestGhcpCli_UserInvocable_MechanismIsRoleConditional asserts the implementation
				// mechanism and provides the RED-phase signal for this outcome.
				Name: "user_invocable_false_for_subagent",
				Request: domain.FrontmatterRequest{
					Kind:     domain.ArtifactAgent,
					AgentKey: "test-agent",
					Role:     domain.RoleSubagent,
					Model: domain.ModelSelection{
						ModelID: "claude-sonnet-4-6",
						Origin:  domain.OriginHarnessList,
					},
					Versions: domain.VersionStamps{
						HarnessVersion:  "3.0.0",
						InjectionsVersion: "1.2.0",
					},
				},
				Expected: domain.FrontmatterPlan{
					Set: []domain.FrontmatterField{
						{Key: "user-invocable", Value: domain.ScalarValue("false", domain.QuotePlain)},
					},
					Remove:   []string{"recommended_tier", "tier_rationale", "required_skills"},
					KeyOrder: []string{"mosaic_id", "version", "mosaic_transform_version", "mosaic_injections_version", "name", "description", "model", "tools", "user-invocable"},
				},
			},
			{
				// user_invocable_true_for_orchestrator: GHCP CLI sets "user-invocable: true" for
				// orchestrator-role agents, enabling the agent to be invoked by users. The role-
				// conditional schema resolves the orchestrator role to true.
				// RED: currently fails because user-invocable is a static false in the descriptor.
				Name: "user_invocable_true_for_orchestrator",
				Request: domain.FrontmatterRequest{
					Kind:     domain.ArtifactAgent,
					AgentKey: "test-agent",
					Role:     domain.RoleOrchestrator,
					Model: domain.ModelSelection{
						ModelID: "claude-sonnet-4-6",
						Origin:  domain.OriginHarnessList,
					},
					Versions: domain.VersionStamps{
						HarnessVersion:  "3.0.0",
						InjectionsVersion: "1.2.0",
					},
				},
				Expected: domain.FrontmatterPlan{
					Set: []domain.FrontmatterField{
						{Key: "user-invocable", Value: domain.ScalarValue("true", domain.QuotePlain)},
					},
					Remove:   []string{"recommended_tier", "tier_rationale", "required_skills"},
					KeyOrder: []string{"mosaic_id", "version", "mosaic_transform_version", "mosaic_injections_version", "name", "description", "model", "tools", "user-invocable"},
				},
			},
			{
				// user_invocable_true_for_utility: GHCP CLI sets "user-invocable: true" for utility-
				// role agents so they can be invoked by users outside of orchestrated workflows.
				// RED: currently fails because user-invocable is a static false in the descriptor.
				Name: "user_invocable_true_for_utility",
				Request: domain.FrontmatterRequest{
					Kind:     domain.ArtifactAgent,
					AgentKey: "test-agent",
					Role:     domain.RoleUtility,
					Model: domain.ModelSelection{
						ModelID: "claude-sonnet-4-6",
						Origin:  domain.OriginHarnessList,
					},
					Versions: domain.VersionStamps{
						HarnessVersion:  "3.0.0",
						InjectionsVersion: "1.2.0",
					},
				},
				Expected: domain.FrontmatterPlan{
					Set: []domain.FrontmatterField{
						{Key: "user-invocable", Value: domain.ScalarValue("true", domain.QuotePlain)},
					},
					Remove:   []string{"recommended_tier", "tier_rationale", "required_skills"},
					KeyOrder: []string{"mosaic_id", "version", "mosaic_transform_version", "mosaic_injections_version", "name", "description", "model", "tools", "user-invocable"},
				},
			},
			{
				// user_invocable_true_for_standalone: GHCP CLI sets "user-invocable: true" for
				// standalone-role agents deployed outside any workflow.
				// RED: currently fails because user-invocable is a static false in the descriptor.
				Name: "user_invocable_true_for_standalone",
				Request: domain.FrontmatterRequest{
					Kind:     domain.ArtifactAgent,
					AgentKey: "test-agent",
					Role:     domain.RoleStandalone,
					Model: domain.ModelSelection{
						ModelID: "claude-sonnet-4-6",
						Origin:  domain.OriginHarnessList,
					},
					Versions: domain.VersionStamps{
						HarnessVersion:  "3.0.0",
						InjectionsVersion: "1.2.0",
					},
				},
				Expected: domain.FrontmatterPlan{
					Set: []domain.FrontmatterField{
						{Key: "user-invocable", Value: domain.ScalarValue("true", domain.QuotePlain)},
					},
					Remove:   []string{"recommended_tier", "tier_rationale", "required_skills"},
					KeyOrder: []string{"mosaic_id", "version", "mosaic_transform_version", "mosaic_injections_version", "name", "description", "model", "tools", "user-invocable"},
				},
			},
			{
				// user_invocable_absent_for_empty_role: when no role is set (zero value), the
				// role-conditional user-invocable field is omitted entirely from the Set. The
				// Remove list and KeyOrder remain unchanged.
				// RED: currently fails because the static descriptor always adds user-invocable: false.
				Name: "user_invocable_absent_for_empty_role",
				Request: domain.FrontmatterRequest{
					Kind:     domain.ArtifactAgent,
					AgentKey: "test-agent",
					Model: domain.ModelSelection{
						ModelID: "claude-sonnet-4-6",
						Origin:  domain.OriginHarnessList,
					},
					Versions: domain.VersionStamps{
						HarnessVersion:  "3.0.0",
						InjectionsVersion: "1.2.0",
					},
				},
				Expected: domain.FrontmatterPlan{
					Set:      []domain.FrontmatterField{},
					Remove:   []string{"recommended_tier", "tier_rationale", "required_skills"},
					KeyOrder: []string{"mosaic_id", "version", "mosaic_transform_version", "mosaic_injections_version", "name", "description", "model", "tools", "user-invocable"},
				},
			},
		},

		InjectionCases: map[string]string{
			"HarnessConstraints": ghcpHarnessConstraints,
		},

		NotFilled: []string{
			"IdentityExtension",
			"ProtocolExtension",
			"CodebaseContext",
			"LanguagePatterns",
			"OutputArtifactTemplate",
			"CustomConstraints",
			"ErrorHandlingExtension",
			"ContextLimits",
			"AvailableWorkflows",
		},

		TargetPathCases: []contracttest.TargetPathCase{
			{
				Name:     "agent_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactAgent, Key: "test-runner", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".github/agents/test-runner.agent.md",
			},
			{
				Name:     "skill_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactSkill, Key: "lean-tdd", FileName: "SKILL.md", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".github/skills/lean-tdd/SKILL.md",
			},
			{
				Name:     "hook_resolves_via_descriptor",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactHook, Key: "any-hook", FileName: "hook.sh", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".github/hooks/hook.sh",
			},
		},

		HookPlanCases: []contracttest.HookPlanCase{
			{
				Name:      "hooks_supported",
				Request:   domain.HookPlanRequest{},
				Supported: true,
				TargetDir: ".github/hooks",
			},
		},
	})
}

// ---------------------------------------------------------------------------
// T3.3 — Regression: ghcp-cli routes custom tools to main tools field (unchanged)
// ---------------------------------------------------------------------------

// TestGhcpCli_CustomToolDestinations_DescriptorDeclaresNone verifies that the ghcp-cli
// descriptor does not declare a custom_tool_destination field. Only Claude Code declares
// that field; all other built-in harnesses must leave it unset so their custom tool
// routing remains unchanged.
func TestGhcpCli_CustomToolDestinations_DescriptorDeclaresNone(t *testing.T) {
	mod := newModule(t)
	desc := mod.Descriptor()

	if len(desc.Tools.CustomToolDestinations) != 0 {
		t.Errorf("ghcp-cli descriptor declares CustomToolDestinations (count=%d); "+
			"only claude-code.yaml declares custom_tool_destination; "+
			"all other built-in descriptors must leave the field unset; "+
			"dests: %v", len(desc.Tools.CustomToolDestinations), desc.Tools.CustomToolDestinations)
	}
}

// TestGhcpCli_CustomTool_RoutesToMainToolsNotMcpServers verifies that when an agent
// declares a custom (MCP-style) tool and the ghcp-cli descriptor has no
// custom_tool_destination, the tool resolves to a DestMain destination and no mcpServers
// field appears in the output. This is the regression guard ensuring ghcp-cli's custom
// tool handling is byte-identical to pre-Stage-3 behaviour.
func TestGhcpCli_CustomTool_RoutesToMainToolsNotMcpServers(t *testing.T) {
	mod := newModule(t)
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

	// No mcpServers field must appear: ghcp-cli has no custom_tool_destination declaration.
	for _, f := range result.Fields {
		if f.Key == "mcpServers" {
			t.Errorf("mcpServers field present in ghcp-cli Tools() output; "+
				"ghcp-cli must not route custom tools to a separate mcpServers field — "+
				"only Claude Code declares custom_tool_destination; "+
				"fields: %v", result.Fields)
		}
	}

	// The ToolCustom resolution must use DestMain (the historical fallback).
	if len(result.Resolutions) == 1 && result.Resolutions[0].Outcome == domain.ToolCustom {
		dests := result.Resolutions[0].Destinations
		if len(dests) != 1 || dests[0].Kind != domain.DestMain {
			t.Errorf("custom tool resolution destinations: want single DestMain, got %v; "+
				"without custom_tool_destination, custom tools must route to the main tools field (DestMain)",
				dests)
		}
	}

	// The custom tool name must appear in the main tools list field.
	toolsKey := desc.Frontmatter.ToolsKey
	var foundInMainTools bool
	for _, f := range result.Fields {
		if f.Key == toolsKey && f.Value.Kind == domain.KindList {
			for _, item := range f.Value.Items {
				if item.Scalar == "human-in-the-loop" {
					foundInMainTools = true
				}
			}
		}
	}
	if !foundInMainTools {
		t.Errorf("custom tool human-in-the-loop not found in main tools list field %q; "+
			"without custom_tool_destination, custom tools must appear in the main tools list",
			toolsKey)
	}
}
