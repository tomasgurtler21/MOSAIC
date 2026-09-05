package claudecode_test

// Tests for the Claude Code built-in harness module.
//
// Coverage:
//   Golden file tests (tool-light, tool-heavy, skill-using, orchestrator agents):
//   - A transform of the generic contracts-review agent (tool-light: 7 generic tools (skill maps to empty, 6 harness tools emitted), no terminal)
//     produces frontmatter with the correct comma-separated tool scalar and drops generic-only fields.
//   - A transform of the generic test-runner agent (tool-heavy: all 7 generic tools including terminal)
//     produces frontmatter listing all 7 Claude Code tools in universe order.
//   - A transform of the generic planner-tdd-soft agent (skill + terminal) produces frontmatter
//     with all tools except skill (which maps to empty/unsupported) and includes Bash for terminal.
//   - A transform of the generic orchestrator ({tool-permissions} placeholder) produces the
//     full placeholder expansion as a comma-separated tool scalar.
//
//   Deployment path resolution:
//   - Project-scoped agent path is ".claude/agents/<key>.md"
//   - Project-scoped skill path is ".claude/skills/<key>/SKILL.md" (key subdirectory prevents collisions)
//   - Project-scoped hook path is ".claude/hooks/<filename>"
//   - Any non-project scope returns domain.ErrUnsupportedScope (via the shared contracttest universal invariant)
//
//   Harness-level injections:
//   - HarnessConstraints injection is filled with empty content (Claude Code has no constraint)
//   - LanguagePatterns is not filled — it is a project-authored injection name, not declared
//     by this harness
//   - injections_version is stamped from the descriptor's injections_version field
//   - Project-class injections (IdentityExtension, ProtocolExtension, etc.) are not filled
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
	"mosaic-deploy/internal/harness/builtin/claudecode"
	"mosaic-deploy/internal/harness/contracttest"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/transform"
)

// updateGolden regenerates the golden files from current engine output when -update is passed.
// Run: go test ./internal/harness/builtin/claudecode/... -run TestGoldenFile -update
var updateGolden = flag.Bool("update", false, "regenerate golden files from current engine output")

// testModel is the ModelSelection used in all Claude Code golden file tests. Using a fixed,
// known model ID makes the golden file content deterministic and reviewable.
var testModel = domain.ModelSelection{
	ModelID: "claude-sonnet-4-6",
	Origin:  domain.OriginHarnessList,
}

// repoRoot returns the absolute path to the repository root, navigating up from this
// package's directory. The test is skipped if the root cannot be located.
func repoRoot(t *testing.T) string {
	t.Helper()
	// Package is at Tools/Deployment/internal/harness/builtin/claudecode/
	// Navigate six levels up to reach the repository root.
	rel := filepath.Join("..", "..", "..", "..", "..", "..")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return abs
}

// goldenDir returns the path to the claude-code golden file directory relative to the package.
func goldenDir(t *testing.T) string {
	t.Helper()
	// testdata/golden/claude-code/ is at Tools/Deployment/testdata/golden/claude-code/
	rel := filepath.Join("..", "..", "..", "..", "testdata", "golden", "claude-code")
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

// newModule constructs the Claude Code module against the real repository root, failing the
// test immediately if construction fails.
func newModule(t *testing.T) domain.HarnessModule {
	t.Helper()
	mod, err := claudecode.New(registry.BuiltinOptions{MosaicRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("claudecode.New(): %v", err)
	}
	return mod
}

// newModuleFromFrozen constructs the Claude Code module against the frozen Catalog fixture root.
// Golden-file tests use this instead of newModule so that changes to the live HarnessInjections
// files in Catalog/ do not affect the committed golden files.
func newModuleFromFrozen(t *testing.T) domain.HarnessModule {
	t.Helper()
	mod, err := claudecode.New(registry.BuiltinOptions{MosaicRoot: frozenCatalogRoot(t)})
	if err != nil {
		t.Fatalf("claudecode.New() from frozen catalog: %v", err)
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


// applyAndCompare applies the harness transform to a generic source file and compares the
// output byte-for-byte with the golden file at goldenPath. When -update is passed, the golden
// file is regenerated instead.
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

// TestGoldenFile_ClaudeCode_ContractsReviewAgent tests a "tool-light" agent: contracts-review
// uses [skill, file_read, file_write, file_edit, file_search, content_search, user_interaction]
// but no terminal. The output should list Read, Write, Edit, Glob, Grep, AskUserQuestion
// (skill maps to empty/unsupported and is omitted).
//
// Deliberate deviation from committed Agents/Claude Code/CodebaseAgnostic/Agents/contracts-review.md:
// the committed file was produced by an LLM process and may contain project injection content;
// the golden file uses create-mode with all project injections cleared.
func TestGoldenFile_ClaudeCode_ContractsReviewAgent(t *testing.T) {
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

	goldenPath := filepath.Join(goldenDir(t), "contracts-review.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// TestGoldenFile_ClaudeCode_TestRunnerAgent tests a "tool-heavy" agent: test-runner uses all
// seven generic tools [file_read, file_write, file_edit, file_search, content_search, terminal,
// user_interaction]. The output should list all eight Claude Code tools: Read, Write, Edit,
// Bash, Glob, Grep, AskUserQuestion (in universe order, comma-separated).
//
// Deliberate deviation from committed Agents/Claude Code/CodebaseAgnostic/Agents/test-runner.md:
// the committed file uses "opus 4.6" as model; this golden uses the fixed test model
// "claude-sonnet-4-6". Project injections are cleared (create mode).
func TestGoldenFile_ClaudeCode_TestRunnerAgent(t *testing.T) {
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

	goldenPath := filepath.Join(goldenDir(t), "test-runner.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// TestGoldenFile_ClaudeCode_PlannerTDDSoftAgent tests an agent that uses the skill generic tool
// in addition to a full tool set: planner-tdd-soft uses [skill, file_read, file_write, file_edit,
// file_search, content_search, terminal, user_interaction]. The output should list Read, Write,
// Edit, Bash, Glob, Grep, AskUserQuestion — skill maps to empty/unsupported and is omitted;
// terminal maps to Bash and must appear.
//
// Deliberate deviation from committed Agents/Claude Code/CodebaseAgnostic/Agents/planner-tdd-soft.md:
// the committed file is missing Bash (the terminal tool mapping is absent), which is an error in
// the committed file. This golden file corrects the deviation and records it here.
func TestGoldenFile_ClaudeCode_PlannerTDDSoftAgent(t *testing.T) {
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

	goldenPath := filepath.Join(goldenDir(t), "planner-tdd-soft.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// TestGoldenFile_ClaudeCode_Orchestrator tests the orchestrator, which uses {tool-permissions}
// as a tools placeholder. The placeholder expands to the full Claude Code universe:
// Read, Write, Edit, Bash, Glob, Grep, Task, AskUserQuestion (comma-separated scalar).
//
// Deliberate deviation from committed Agents/Claude Code/CodebaseAgnostic/orchestrator.md:
// the committed file uses "opus 4.6" as model. This golden uses "claude-sonnet-4-6". Project
// injections are cleared (create mode).
func TestGoldenFile_ClaudeCode_Orchestrator(t *testing.T) {
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

	goldenPath := filepath.Join(goldenDir(t), "orchestrator.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// ---------------------------------------------------------------------------
// Deployment path resolution tests
// ---------------------------------------------------------------------------

// TestTargetPath_ClaudeCode_AgentProjectScope verifies that an agent deployed to project scope
// lands at ".claude/agents/<key>.md".
func TestTargetPath_ClaudeCode_AgentProjectScope(t *testing.T) {
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
	want := ".claude/agents/test-runner.md"
	if path != want {
		t.Errorf("TargetPath = %q, want %q", path, want)
	}
}

// TestTargetPath_ClaudeCode_SkillProjectScope verifies that a skill is deployed under a
// key-named subdirectory: ".claude/skills/<key>/SKILL.md". The key subdirectory is required
// because all skill entry files are named SKILL.md by convention; without it, every deployed
// skill would overwrite the same path.
func TestTargetPath_ClaudeCode_SkillProjectScope(t *testing.T) {
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
	want := ".claude/skills/lean-tdd/SKILL.md"
	if path != want {
		t.Errorf("TargetPath = %q, want %q", path, want)
	}
}

// TestTargetPath_ClaudeCode_HookProjectScope verifies that hooks are deployed to
// ".claude/hooks/<filename>" when Claude Code hook support is declared.
func TestTargetPath_ClaudeCode_HookProjectScope(t *testing.T) {
	mod := newModule(t)
	path, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:     domain.ArtifactHook,
		Key:      "subagent-logger",
		FileName: "subagent-logger.sh",
		Scope:    domain.ScopeProject,
		GOOS:     "linux",
	})
	if err != nil {
		t.Fatalf("TargetPath: %v", err)
	}
	want := ".claude/hooks/subagent-logger.sh"
	if path != want {
		t.Errorf("TargetPath = %q, want %q", path, want)
	}
}

// ---------------------------------------------------------------------------
// Harness-level injection tests
// ---------------------------------------------------------------------------

// TestInjection_ClaudeCode_HarnessConstraintsIsEmpty verifies that Claude Code declares
// HarnessConstraints with empty content (no constraint injection).
func TestInjection_ClaudeCode_HarnessConstraintsIsEmpty(t *testing.T) {
	mod := newModule(t)
	content, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: ""})
	if !ok {
		t.Fatal("Injection(\"HarnessConstraints\") returned ok=false; Claude Code must declare this injection")
	}
	if content != "" {
		t.Errorf("Injection(\"HarnessConstraints\") = %q; want empty string (Claude Code has no constraint injection)", content)
	}
}

// TestInjection_ClaudeCode_ProjectInjectionsNotFilled verifies that project-class injections
// are not filled by the Claude Code harness (they remain empty on create). LanguagePatterns
// is included here: it is a project-authored injection name, not a harness-declared one, so
// Claude Code must not fill it.
func TestInjection_ClaudeCode_ProjectInjectionsNotFilled(t *testing.T) {
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
		_, ok := mod.Injection(domain.InjectionRequest{Name: name, AgentKey: ""})
		if ok {
			t.Errorf("Injection(%q) returned ok=true; project-class injections must not be filled by the harness", name)
		}
	}
}

// TestInjection_ClaudeCode_InjectionsVersion verifies that the module's descriptor carries
// the correct injections_version "1.1.0" for Claude Code.
func TestInjection_ClaudeCode_InjectionsVersion(t *testing.T) {
	mod := newModule(t)
	d := mod.Descriptor()
	if d == nil {
		t.Fatal("Descriptor() returned nil")
	}
	want := "1.1.0"
	if d.InjectionsVersion != want {
		t.Errorf("Descriptor().InjectionsVersion = %q, want %q", d.InjectionsVersion, want)
	}
}

// ---------------------------------------------------------------------------
// Shared contract test
// ---------------------------------------------------------------------------

// TestContract_ClaudeCode runs the shared HarnessModule contract suite against the Claude Code
// module. This ensures Claude Code satisfies every invariant that all provision tiers must
// exhibit identically.
func TestContract_ClaudeCode(t *testing.T) {
	mod := newModule(t) // RED: fails here until implementation is complete

	contracttest.Run(t, mod, contracttest.Fixtures{
		ToolCases: []contracttest.ToolCase{
			{
				// all_seven_generic_tools: verifies the full mapping and the comma-separated scalar
				// format that distinguishes Claude Code from GHCP CLI. The tools field must be a
				// plain (unquoted) scalar containing harness tool names in universe order, separated
				// by ", ". Universe order from the descriptor: Read, Write, Edit, Bash, Glob, Grep,
				// Task, AskUserQuestion. Task is absent here because subagent is not in the request.
				Name: "all_seven_generic_tools",
				Request: domain.ToolRequest{
					AgentKey: "test-runner",
					Generic:  []string{"file_read", "file_write", "file_edit", "file_search", "content_search", "terminal", "user_interaction"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key: "tools",
						Value: domain.FieldValue{
							Kind:   domain.KindScalar,
							Scalar: "Read, Write, Edit, Bash, Glob, Grep, AskUserQuestion",
							Quote:  domain.QuotePlain,
						},
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "file_read", Outcome: domain.ToolMapped, HarnessTools: []string{"Read"}},
					{Generic: "file_write", Outcome: domain.ToolMapped, HarnessTools: []string{"Write"}},
					{Generic: "file_edit", Outcome: domain.ToolMapped, HarnessTools: []string{"Edit"}},
					{Generic: "file_search", Outcome: domain.ToolMapped, HarnessTools: []string{"Glob"}},
					{Generic: "content_search", Outcome: domain.ToolMapped, HarnessTools: []string{"Grep"}},
					{Generic: "terminal", Outcome: domain.ToolMapped, HarnessTools: []string{"Bash"}},
					{Generic: "user_interaction", Outcome: domain.ToolMapped, HarnessTools: []string{"AskUserQuestion"}},
				},
			},
			{
				Name: "skill_tool_is_unsupported",
				Request: domain.ToolRequest{
					AgentKey: "planner-tdd-soft",
					Generic:  []string{"skill"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key: "tools",
						Value: domain.FieldValue{
							Kind:   domain.KindScalar,
							Scalar: "",
							Quote:  domain.QuotePlain,
						},
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "skill", Outcome: domain.ToolMapped, HarnessTools: []string{}},
				},
			},
			{
				Name: "subagent_maps_to_task",
				Request: domain.ToolRequest{
					AgentKey: "orchestrator",
					Generic:  []string{"subagent"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key: "tools",
						Value: domain.FieldValue{
							Kind:   domain.KindScalar,
							Scalar: "Task, TaskStop",
							Quote:  domain.QuotePlain,
						},
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "subagent", Outcome: domain.ToolMapped, HarnessTools: []string{"Task", "TaskStop"}},
				},
			},
		},

		FrontmatterCases: []contracttest.FrontmatterCase{
			{
				// drops_generic_only_keys_and_applies_key_order: verifies that Claude Code drops the
				// three generic-only frontmatter keys (recommended_tier, tier_rationale, required_skills)
				// and applies its canonical key order from the descriptor. Claude Code has no static
				// descriptor Add fields, so Set is nil — model and version stamps are applied
				// exclusively by the transform's Steps 3 and 4, not by the module. Including them
				// here would cause duplicate FieldChange entries in transform.Report.Fields.
				Name: "drops_generic_only_keys_and_applies_key_order",
				Request: domain.FrontmatterRequest{
					Kind:     domain.ArtifactAgent,
					AgentKey: "test-agent",
					Model: domain.ModelSelection{
						ModelID: "claude-sonnet-4-6",
						Origin:  domain.OriginHarnessList,
					},
					Versions: domain.VersionStamps{
						HarnessVersion:  "3.0.0",
						InjectionsVersion: "1.1.0",
					},
				},
				Expected: domain.FrontmatterPlan{
					Set:      nil,
					Remove:   []string{"recommended_tier", "tier_rationale", "required_skills"},
					KeyOrder: []string{"mosaic_id", "version", "mosaic_transform_version", "mosaic_injections_version", "name", "description", "model", "tools"},
				},
			},
		},

		InjectionCases: map[string]string{
			"HarnessConstraints": "",
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
				Expected: ".claude/agents/test-runner.md",
			},
			{
				Name:     "skill_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactSkill, Key: "lean-tdd", FileName: "SKILL.md", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".claude/skills/lean-tdd/SKILL.md",
			},
			{
				Name:     "hook_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactHook, Key: "subagent-logger", FileName: "hook.sh", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".claude/hooks/hook.sh",
			},
			{
				Name:    "unsupported_kind_returns_sentinel",
				Request: domain.TargetPathRequest{Kind: domain.ArtifactKind("workflow"), Key: "x", Scope: domain.ScopeProject, GOOS: "linux"},
				Err:     domain.ErrArtifactUnsupported,
			},
		},
	})
}
