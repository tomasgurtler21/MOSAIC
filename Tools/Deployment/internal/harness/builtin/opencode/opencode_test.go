package opencode_test

// Tests for the OpenCode built-in harness module.
//
// OpenCode is the most structurally distinct harness in the system: it replaces
// the generic tools list with a nested allow/deny permission mapping, adds a
// "mode" field, drops "name", and reorders keys. These tests drive the module
// through every distinctive behaviour and the shared HarnessModule contract.
//
// Coverage:
//
//   Golden file tests (representative agent set):
//   - A transform of the generic contracts-review agent (skill, no terminal) produces
//     a permission block with skill:allow, bash:deny, and every other used tool allowed.
//   - A transform of the generic test-runner agent (all 7 generic tools) produces a
//     permission block with 15 entries in universe order, all mapped tools allowed.
//   - A transform of the generic planner-tdd-soft agent (skill + terminal) produces
//     both skill:allow and bash:allow. The committed OpenCode reference file incorrectly
//     shows bash:deny; the golden corrects this because the generic source includes
//     terminal in its tool list.
//   - A transform of the generic orchestrator ({tool-permissions} placeholder) produces
//     a permission block driven by the descriptor's placeholder_expansion, with task:allow
//     and mode:primary (orchestrators are primary agents, not subagents).
//
//   Permission block generation:
//   - All tools the agent uses map to allow; all other universe tools are explicitly deny.
//   - The by_convention "list" tool is always allow regardless of the agent's generic tools.
//   - Permission pairs appear in stable universe order for every agent.
//   - A skill-using agent has skill:allow; an agent without skill has skill:deny.
//   - A terminal-using agent has bash:allow; an agent without terminal has bash:deny.
//   - A subagent-using agent (orchestrator) has task:allow.
//
//   Frontmatter field removal, addition, and key order:
//   - The "name" field is absent from the deployed output (dropped).
//   - "mode: subagent" is added to every regular agent output.
//   - The orchestrator receives "mode: primary" instead of "mode: subagent".
//   - Output key order is: id, version, transform_version, injections_version,
//     description, mode, model, permission.
//   - Generic-only keys (recommended_tier, tier_rationale, required_skills) are dropped.
//
//   Model format and passthrough:
//   - A model id following the "provider/model-id" convention is emitted verbatim.
//   - A custom model id not following the convention is emitted exactly as given,
//     with no reformatting toward the "provider/model-id" format.
//
//   Deployment paths and hook registration:
//   - Project-scoped agent path is ".opencode/agents/<key>.md"
//   - Project-scoped skill path is ".opencode/skills/<key>/SKILL.md" (key subdirectory)
//   - Project-scoped hook path is ".opencode/plugins/<filename>" (plugins/, not hooks/)
//   - Hooks require no registration steps (HookPlan.Registration is empty)
//   - Any non-project scope returns domain.ErrUnsupportedScope (via the shared contracttest universal invariant)
//
//   Harness-level injections:
//   - HarnessConstraints is filled with the parallel tool calls and working directory note.
//   - LanguagePatterns is declared but empty.
//   - injections_version is "1.3.1" per the OpenCode descriptor.
//   - Project-class injections (IdentityExtension, ProtocolExtension, etc.) are not filled.
//
//   Shared contract:
//   - The module passes contracttest.Run with identical universal invariant results.

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/opencode"
	"mosaic-deploy/internal/harness/contracttest"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/transform"
)

// updateGolden regenerates the golden files from current engine output when -update is passed.
// Run: go test ./internal/harness/builtin/opencode/... -run TestGoldenFile -update
var updateGolden = flag.Bool("update", false, "regenerate golden files from current engine output")

// testModel is the ModelSelection used in all OpenCode golden file tests. Using the
// provider/model-id convention that OpenCode expects, with a fixed value that makes
// golden file content deterministic and reviewable.
var testModel = domain.ModelSelection{
	ModelID: "github-copilot/claude-sonnet-4-6",
	Origin:  domain.OriginHarnessList,
}

// opencodeHarnessConstraints is the expected content of the HarnessConstraints injection
// for OpenCode: the parallel tool calls instruction and the working directory note.
// Both constraints are present because OpenCode subagents receive them; the GHCP CLI
// omits the working directory note since it uses a different resolution model.
const opencodeHarnessConstraints = "" +
	"- **Parallel Tool Calls:** Issue multiple independent tool calls in a single response whenever possible. Sequential tool calls are only permitted when a later call depends on the result of an earlier one. This minimises inference API calls to improve speed and reduce cost.\n" +
	"- **Working Directory vs Workspace Root:** File tool paths resolve relative to the **working directory**, not the workspace root. Orchestration is always at working directory."

// repoRoot returns the absolute path to the repository root, navigating up from this
// package's directory. The test is skipped if the root cannot be located.
func repoRoot(t *testing.T) string {
	t.Helper()
	// Package is at Tools/Deployment/internal/harness/builtin/opencode/
	// Navigate six levels up to reach the repository root.
	rel := filepath.Join("..", "..", "..", "..", "..", "..")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return abs
}

// goldenDir returns the path to the opencode golden file directory.
func goldenDir(t *testing.T) string {
	t.Helper()
	// testdata/golden/opencode/ is at Tools/Deployment/testdata/golden/opencode/
	rel := filepath.Join("..", "..", "..", "..", "testdata", "golden", "opencode")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve golden dir: %v", err)
	}
	return abs
}

// newModule constructs the OpenCode module against the real repository root, failing the
// test immediately if construction fails.
func newModule(t *testing.T) domain.HarnessModule {
	t.Helper()
	mod, err := opencode.New(registry.BuiltinOptions{MosaicRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("opencode.New(): %v", err)
	}
	return mod
}

// applyAndCompare applies the harness transform to a generic source file and compares the
// output byte-for-byte with the golden file at goldenPath. When -update is passed, the
// golden file is regenerated instead.
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

// loadProtocol loads the protocol content from the repository root for use in transform requests.
// All agents in Agents/Generic/ carry a [[DEPLOYED:CommunicationProtocol]] region, so Protocol
// must be populated for every transform.Apply call that processes those source files.
func loadProtocol(t *testing.T, root string) domain.ProtocolContent {
	t.Helper()
	content, err := catalog.FileProtocolLoader{}.LoadProtocol(root)
	if err != nil {
		t.Fatalf("load protocol from %s: %v", root, err)
	}
	return content
}

// permissionPairs builds the complete 15-entry permission block for the given set of allowed
// tools. Universe order is: read, write, edit, glob, grep, list (by_convention: always allow),
// bash, patch, webfetch, question, lsp, task, todowrite, todoread, skill.
// The by_convention "list" tool is always allow regardless of the allowed set.
func permissionPairs(allowed map[string]bool) []domain.FieldPair {
	universe := []string{
		"read", "write", "edit", "glob", "grep", "list",
		"bash", "patch", "webfetch", "question", "lsp",
		"task", "todowrite", "todoread", "skill",
	}
	byConvention := map[string]bool{"list": true}

	pairs := make([]domain.FieldPair, len(universe))
	for i, tool := range universe {
		disposition := "deny"
		if allowed[tool] || byConvention[tool] {
			disposition = "allow"
		}
		pairs[i] = domain.FieldPair{
			Key:   tool,
			Value: domain.ScalarValue(disposition, domain.QuotePlain),
		}
	}
	return pairs
}

// ---------------------------------------------------------------------------
// Golden file tests (T12.1)
// ---------------------------------------------------------------------------

// TestGoldenFile_OpenCode_ContractsReviewAgent tests a "tool-light" agent: contracts-review
// uses [skill, file_read, file_write, file_edit, file_search, content_search, user_interaction]
// but no terminal. The output must produce a permission block with skill:allow, bash:deny,
// and every other mapped tool allowed. "name" is absent from the output.
//
// The golden file is fresh-authored from the committed reference
// Agents/OpenCode/CodebaseAgnostic/Agents/contracts-review.md with the following corrections:
//   - Permission pair order follows descriptor universe order (read, write, edit, glob, grep,
//     list, bash, patch, webfetch, question, lsp, task, todowrite, todoread, skill) rather than
//     the order in the committed file, which was produced by an LLM process.
//   - Model changed to testModel "github-copilot/claude-sonnet-4-6" (committed uses opus).
//   - Project injections cleared (create mode).
func TestGoldenFile_OpenCode_ContractsReviewAgent(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)

	srcPath := filepath.Join(root, "Agents", "Generic", "Agents", "Validation", "contracts-review.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic contracts-review agent not found at %s: %v", srcPath, err)
	}

	req := transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "contracts-review",
		Module:   mod,
		Model:    testModel,
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: protocol,
	}

	goldenPath := filepath.Join(goldenDir(t), "contracts-review.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// TestGoldenFile_OpenCode_TestRunnerAgent tests the "tool-heavy" canonical reference agent:
// test-runner uses all 7 generic tools [file_read, file_write, file_edit, file_search,
// content_search, terminal, user_interaction]. The output permission block must list all
// 15 universe tools in stable order with every mapped tool allowed and the rest denied.
//
// Agents/OpenCode/CodebaseAgnostic/Agents/test-runner.md is the canonical reference for
// OpenCode frontmatter field order, mode, and the complete permission block format.
//
// Deliberate deviations from the committed reference:
//   - Model changed to testModel "github-copilot/claude-sonnet-4-6".
//   - Project injections cleared (create mode).
func TestGoldenFile_OpenCode_TestRunnerAgent(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)

	srcPath := filepath.Join(root, "Agents", "Generic", "Agents", "Execution", "test-runner.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic test-runner agent not found at %s: %v", srcPath, err)
	}

	req := transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "test-runner",
		Module:   mod,
		Model:    testModel,
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: protocol,
	}

	goldenPath := filepath.Join(goldenDir(t), "test-runner.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// TestGoldenFile_OpenCode_PlannerTDDSoftAgent tests an agent that uses both the skill tool
// and terminal: planner-tdd-soft uses [skill, file_read, file_write, file_edit, file_search,
// content_search, terminal, user_interaction]. Both skill:allow and bash:allow must appear.
//
// Deliberate deviations from the committed reference
// Agents/OpenCode/CodebaseAgnostic/Agents/planner-tdd-soft.md:
//   - The committed file shows bash:deny, which is incorrect. The generic source declares
//     "terminal" in its tool list; terminal maps to bash, so bash must be allow. This golden
//     corrects that error.
//   - Model changed to testModel "github-copilot/claude-sonnet-4-6".
//   - Project injections cleared (create mode).
func TestGoldenFile_OpenCode_PlannerTDDSoftAgent(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)

	srcPath := filepath.Join(root, "Agents", "Generic", "Agents", "Planning", "planner-tdd-soft.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic planner-tdd-soft agent not found at %s: %v", srcPath, err)
	}

	req := transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "planner-tdd-soft",
		Module:   mod,
		Model:    testModel,
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: protocol,
	}

	goldenPath := filepath.Join(goldenDir(t), "planner-tdd-soft.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// TestGoldenFile_OpenCode_Orchestrator tests the orchestrator, which uses {tool-permissions}
// as its tools placeholder. The placeholder expands to the descriptor's placeholder_expansion
// set: read, write, edit, glob, grep, bash, question, task. The list tool is additionally
// always allow (by_convention). The orchestrator receives mode:primary (not mode:subagent).
//
// Deliberate deviations from the committed reference
// Agents/OpenCode/CodebaseAgnostic/orchestrator.md:
//   - The committed file shows task appearing before patch in the permission block, which
//     violates the stable universe order (patch comes before task). This golden file uses
//     the canonical universe order from the descriptor.
//   - Model changed to testModel "github-copilot/claude-sonnet-4-6".
//   - Project injections cleared (create mode).
func TestGoldenFile_OpenCode_Orchestrator(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)

	srcPath := filepath.Join(root, "Agents", "Generic", "Orchestrator", "orchestrator.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic orchestrator not found at %s: %v", srcPath, err)
	}

	req := transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "orchestrator",
		Module:   mod,
		Model:    testModel,
		Scope:    domain.ScopeProject,
		Role:     domain.RoleOrchestrator,
		Protocol: protocol,
	}

	goldenPath := filepath.Join(goldenDir(t), "orchestrator.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// ---------------------------------------------------------------------------
// Permission block generation tests (T12.2)
// ---------------------------------------------------------------------------

// TestPermission_AllMappedToolsAllowed verifies that every tool the agent uses maps to
// allow and every unused tool is explicitly deny.
func TestPermission_AllMappedToolsAllowed(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-runner",
		Generic:  []string{"file_read", "file_write", "file_edit", "file_search", "content_search", "terminal", "user_interaction"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	if len(result.Fields) == 0 {
		t.Fatal("Tools returned no fields")
	}
	permField := result.Fields[0]
	if permField.Key != "permission" {
		t.Fatalf("Fields[0].Key = %q, want %q", permField.Key, "permission")
	}
	if permField.Value.Kind != domain.KindMapping {
		t.Fatalf("permission field Kind = %q, want KindMapping", permField.Value.Kind)
	}

	dispositions := make(map[string]string)
	for _, pair := range permField.Value.Pairs {
		dispositions[pair.Key] = pair.Value.Scalar
	}

	wantAllow := []string{"read", "write", "edit", "glob", "grep", "list", "bash", "question"}
	for _, tool := range wantAllow {
		if dispositions[tool] != "allow" {
			t.Errorf("permission[%q] = %q, want %q", tool, dispositions[tool], "allow")
		}
	}

	wantDeny := []string{"patch", "webfetch", "lsp", "task", "todowrite", "todoread", "skill"}
	for _, tool := range wantDeny {
		if dispositions[tool] != "deny" {
			t.Errorf("permission[%q] = %q, want %q", tool, dispositions[tool], "deny")
		}
	}
}

// TestPermission_ByConvention_ListAlwaysAllow verifies that the "list" tool is always
// allow in the permission block regardless of which generic tools the agent declares.
// list is a by_convention:true tool — it is emitted for every agent.
func TestPermission_ByConvention_ListAlwaysAllow(t *testing.T) {
	mod := newModule(t)

	// An agent that uses only file_read — list is not in the generic tools.
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "minimal-agent",
		Generic:  []string{"file_read"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	if len(result.Fields) == 0 {
		t.Fatal("Tools returned no fields")
	}
	permField := result.Fields[0]

	var listDisposition string
	for _, pair := range permField.Value.Pairs {
		if pair.Key == "list" {
			listDisposition = pair.Value.Scalar
			break
		}
	}
	if listDisposition != "allow" {
		t.Errorf("permission[\"list\"] = %q, want %q; list is by_convention and must always be allow", listDisposition, "allow")
	}
}

// TestPermission_StableKeyOrder verifies that permission pairs are always emitted in
// universe order: read, write, edit, glob, grep, list, bash, patch, webfetch, question,
// lsp, task, todowrite, todoread, skill. The order must be identical for all agents.
func TestPermission_StableKeyOrder(t *testing.T) {
	mod := newModule(t)

	wantOrder := []string{
		"read", "write", "edit", "glob", "grep", "list",
		"bash", "patch", "webfetch", "question", "lsp",
		"task", "todowrite", "todoread", "skill",
	}

	agents := []struct {
		name    string
		generic []string
	}{
		{"test-runner", []string{"file_read", "file_write", "file_edit", "file_search", "content_search", "terminal", "user_interaction"}},
		{"contracts-review", []string{"skill", "file_read", "file_write", "file_edit", "file_search", "content_search", "user_interaction"}},
		{"minimal", []string{"file_read"}},
	}

	for _, agent := range agents {
		result, err := mod.Tools(domain.ToolRequest{
			AgentKey: agent.name,
			Generic:  agent.generic,
		})
		if err != nil {
			t.Fatalf("Tools(%s): %v", agent.name, err)
		}
		if len(result.Fields) == 0 {
			t.Fatalf("Tools(%s): no fields returned", agent.name)
		}
		pairs := result.Fields[0].Value.Pairs
		if len(pairs) != len(wantOrder) {
			t.Errorf("Tools(%s): permission block has %d pairs, want %d", agent.name, len(pairs), len(wantOrder))
			continue
		}
		for i, pair := range pairs {
			if pair.Key != wantOrder[i] {
				t.Errorf("Tools(%s): permission pair[%d].Key = %q, want %q", agent.name, i, pair.Key, wantOrder[i])
			}
		}
	}
}

// TestPermission_SkillAllowedWhenDeclared verifies that the "skill" tool is allow when
// the agent's generic tool list includes "skill".
func TestPermission_SkillAllowedWhenDeclared(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "contracts-review",
		Generic:  []string{"skill", "file_read"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	for _, pair := range result.Fields[0].Value.Pairs {
		if pair.Key == "skill" {
			if pair.Value.Scalar != "allow" {
				t.Errorf("permission[\"skill\"] = %q, want %q when agent declares the skill generic tool", pair.Value.Scalar, "allow")
			}
			return
		}
	}
	t.Error("permission[\"skill\"] not found in output")
}

// TestPermission_SkillDeniedWhenNotUsed verifies that the "skill" tool is deny when
// the agent does not include "skill" in its generic tool list.
func TestPermission_SkillDeniedWhenNotUsed(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-runner",
		Generic:  []string{"file_read", "terminal"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	for _, pair := range result.Fields[0].Value.Pairs {
		if pair.Key == "skill" {
			if pair.Value.Scalar != "deny" {
				t.Errorf("permission[\"skill\"] = %q, want %q when agent does not declare the skill generic tool", pair.Value.Scalar, "deny")
			}
			return
		}
	}
	t.Error("permission[\"skill\"] not found in output")
}

// TestPermission_BashAllowedWhenTerminalDeclared verifies that bash is allow when
// the agent declares the "terminal" generic tool.
func TestPermission_BashAllowedWhenTerminalDeclared(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "planner-tdd-soft",
		Generic:  []string{"file_read", "terminal"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	for _, pair := range result.Fields[0].Value.Pairs {
		if pair.Key == "bash" {
			if pair.Value.Scalar != "allow" {
				t.Errorf("permission[\"bash\"] = %q, want %q when agent declares the terminal generic tool", pair.Value.Scalar, "allow")
			}
			return
		}
	}
	t.Error("permission[\"bash\"] not found in output")
}

// TestPermission_BashDeniedWhenTerminalNotDeclared verifies that bash is deny when
// the agent does not use the "terminal" generic tool.
func TestPermission_BashDeniedWhenTerminalNotDeclared(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "contracts-review",
		Generic:  []string{"skill", "file_read"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	for _, pair := range result.Fields[0].Value.Pairs {
		if pair.Key == "bash" {
			if pair.Value.Scalar != "deny" {
				t.Errorf("permission[\"bash\"] = %q, want %q when agent does not declare terminal", pair.Value.Scalar, "deny")
			}
			return
		}
	}
	t.Error("permission[\"bash\"] not found in output")
}

// TestPermission_TaskAllowedForOrchestrator verifies that the task permission is allow
// when the agent uses the "subagent" generic tool (as the orchestrator does).
func TestPermission_TaskAllowedForOrchestrator(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "orchestrator",
		Generic:  []string{"subagent", "file_read"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	for _, pair := range result.Fields[0].Value.Pairs {
		if pair.Key == "task" {
			if pair.Value.Scalar != "allow" {
				t.Errorf("permission[\"task\"] = %q, want %q when agent uses subagent generic tool", pair.Value.Scalar, "allow")
			}
			return
		}
	}
	t.Error("permission[\"task\"] not found in output")
}

// TestPermission_AllToolsExplicitlyListed verifies that every universe tool appears exactly
// once in every permission block. OpenCode's critical constraint is that unlisted tools
// default to allow, so all unused tools must be explicitly deny.
func TestPermission_AllToolsExplicitlyListed(t *testing.T) {
	mod := newModule(t)

	universeSize := 15 // read, write, edit, glob, grep, list, bash, patch, webfetch, question, lsp, task, todowrite, todoread, skill

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "minimal-agent",
		Generic:  []string{"file_read"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	if len(result.Fields) == 0 {
		t.Fatal("no fields returned")
	}
	pairs := result.Fields[0].Value.Pairs
	if len(pairs) != universeSize {
		t.Errorf("permission block has %d entries, want %d (all %d universe tools must be listed)", len(pairs), universeSize, universeSize)
	}

	seen := make(map[string]int)
	for _, pair := range pairs {
		seen[pair.Key]++
	}
	for tool, count := range seen {
		if count > 1 {
			t.Errorf("tool %q appears %d times in permission block; want exactly 1", tool, count)
		}
	}
}

// ---------------------------------------------------------------------------
// Frontmatter field removal, addition, and key order tests (T12.3)
// ---------------------------------------------------------------------------

// TestFrontmatter_NameFieldDropped verifies that the "name" field is absent from the
// deployed output. OpenCode identifies agents by filename, not by a name field.
func TestFrontmatter_NameFieldDropped(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)

	srcPath := filepath.Join(root, "Agents", "Generic", "Agents", "Execution", "test-runner.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic test-runner agent not found: %v", err)
	}

	result, err := transform.Apply(transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "test-runner",
		Module:   mod,
		Model:    testModel,
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: protocol,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	_, ok := doc.Frontmatter().Get("name")
	if ok {
		t.Error("\"name\" field is present in output frontmatter; OpenCode must drop it (agents are identified by filename)")
	}
}

// TestFrontmatter_ModeSubagentAdded verifies that "mode: subagent" is present in the
// deployed output for a regular (non-orchestrator) agent.
func TestFrontmatter_ModeSubagentAdded(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)

	srcPath := filepath.Join(root, "Agents", "Generic", "Agents", "Execution", "test-runner.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic test-runner agent not found: %v", err)
	}

	result, err := transform.Apply(transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "test-runner",
		Module:   mod,
		Model:    testModel,
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: protocol,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	modeVal, ok := doc.Frontmatter().Get("mode")
	if !ok {
		t.Fatal("\"mode\" field not found in output frontmatter; must be added by OpenCode harness")
	}
	if modeVal.Scalar != "subagent" {
		t.Errorf("mode = %q, want %q for a regular agent", modeVal.Scalar, "subagent")
	}
}

// TestFrontmatter_OrchestratorGetsPrimaryMode verifies that the orchestrator receives
// "mode: primary" instead of "mode: subagent". The orchestrator is a primary agent that
// users interact with directly; subagents are invoked by other agents via the task tool.
func TestFrontmatter_OrchestratorGetsPrimaryMode(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)

	srcPath := filepath.Join(root, "Agents", "Generic", "Orchestrator", "orchestrator.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic orchestrator not found: %v", err)
	}

	result, err := transform.Apply(transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "orchestrator",
		Module:   mod,
		Model:    testModel,
		Scope:    domain.ScopeProject,
		Role:     domain.RoleOrchestrator,
		Protocol: protocol,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	modeVal, ok := doc.Frontmatter().Get("mode")
	if !ok {
		t.Fatal("\"mode\" field not found in orchestrator output frontmatter")
	}
	if modeVal.Scalar != "primary" {
		t.Errorf("orchestrator mode = %q, want %q; orchestrators are primary agents, not subagents", modeVal.Scalar, "primary")
	}
}

// TestFrontmatter_GenericOnlyKeysDropped verifies that the three generic-only frontmatter
// keys (recommended_tier, tier_rationale, required_skills) are absent from deployed output.
func TestFrontmatter_GenericOnlyKeysDropped(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)

	srcPath := filepath.Join(root, "Agents", "Generic", "Agents", "Execution", "test-runner.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic test-runner agent not found: %v", err)
	}

	result, err := transform.Apply(transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "test-runner",
		Module:   mod,
		Model:    testModel,
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: protocol,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	fm := doc.Frontmatter()
	genericOnly := []string{"recommended_tier", "tier_rationale", "required_skills"}
	for _, key := range genericOnly {
		_, ok := fm.Get(key)
		if ok {
			t.Errorf("generic-only key %q is present in output; must be dropped", key)
		}
	}
}

// TestFrontmatter_KeyOrderMatchesHarnessConvention verifies that the deployed frontmatter
// follows the OpenCode key order: id, version, transform_version, injections_version,
// description, mode, model, permission.
func TestFrontmatter_KeyOrderMatchesHarnessConvention(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)

	srcPath := filepath.Join(root, "Agents", "Generic", "Agents", "Execution", "test-runner.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic test-runner agent not found: %v", err)
	}

	result, err := transform.Apply(transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "test-runner",
		Module:   mod,
		Model:    testModel,
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: protocol,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	keys := doc.Frontmatter().Keys()

	wantOrder := []string{"id", "version", "transform_version", "injections_version", "description", "mode", "model", "permission"}
	if len(keys) < len(wantOrder) {
		t.Fatalf("frontmatter has %d keys, need at least %d", len(keys), len(wantOrder))
	}

	// Verify the mandated keys appear in the required relative order.
	pos := make(map[string]int)
	for i, k := range keys {
		pos[k] = i
	}
	for i := 1; i < len(wantOrder); i++ {
		prev, curr := wantOrder[i-1], wantOrder[i]
		prevPos, prevOK := pos[prev]
		currPos, currOK := pos[curr]
		if !prevOK || !currOK {
			continue // key may be absent from this specific source; golden files cover full order
		}
		if prevPos >= currPos {
			t.Errorf("key order violation: %q (pos %d) must come before %q (pos %d)", prev, prevPos, curr, currPos)
		}
	}
}

// ---------------------------------------------------------------------------
// Model format and passthrough tests (T12.4)
// ---------------------------------------------------------------------------

// TestModelFormat_ProviderModelIDIsEmittedVerbatim verifies that a model id following the
// "provider/model-id" convention is written exactly as given to the "model" frontmatter key.
func TestModelFormat_ProviderModelIDIsEmittedVerbatim(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)

	srcPath := filepath.Join(root, "Agents", "Generic", "Agents", "Execution", "test-runner.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic test-runner agent not found: %v", err)
	}

	wantModelID := "github-copilot/claude-opus-4.6"
	result, err := transform.Apply(transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "test-runner",
		Module:   mod,
		Model:    domain.ModelSelection{ModelID: wantModelID, Origin: domain.OriginHarnessList},
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: protocol,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	modelVal, ok := doc.Frontmatter().Get("model")
	if !ok {
		t.Fatal("model field not found in output frontmatter")
	}
	if modelVal.Scalar != wantModelID {
		t.Errorf("model = %q, want %q", modelVal.Scalar, wantModelID)
	}
}

// TestModelFormat_CustomIDPassthrough verifies that a model id that does not follow the
// "provider/model-id" convention is emitted exactly as the user entered it. The OpenCode
// harness declares the provider/model-id format as a display hint only; no validation or
// reformatting is applied to the model field value.
func TestModelFormat_CustomIDPassthrough(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)

	srcPath := filepath.Join(root, "Agents", "Generic", "Agents", "Execution", "test-runner.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic test-runner agent not found: %v", err)
	}

	// A model id that does not follow the "provider/model-id" convention.
	customModelID := "my-custom-provider/my-special-model-v42"
	result, err := transform.Apply(transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "test-runner",
		Module:   mod,
		Model:    domain.ModelSelection{ModelID: customModelID, Origin: domain.OriginCustom},
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: protocol,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	modelVal, ok := doc.Frontmatter().Get("model")
	if !ok {
		t.Fatal("model field not found in output frontmatter")
	}
	if modelVal.Scalar != customModelID {
		t.Errorf("model = %q, want %q (custom model id must be emitted verbatim, not reformatted)", modelVal.Scalar, customModelID)
	}
}

// TestModelFormat_NonConventionalIDNeverRewritten verifies that a model id that has no
// provider prefix (no "/" separator) is emitted as-is, never reformatted.
func TestModelFormat_NonConventionalIDNeverRewritten(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)

	srcPath := filepath.Join(root, "Agents", "Generic", "Agents", "Execution", "test-runner.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic test-runner agent not found: %v", err)
	}

	// A model id with no provider prefix — user entered it without the convention.
	bareModelID := "claude-opus-4.6"
	result, err := transform.Apply(transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "test-runner",
		Module:   mod,
		Model:    domain.ModelSelection{ModelID: bareModelID, Origin: domain.OriginCustom},
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol: protocol,
	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	doc, err := docformat.Parse(result.Output)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}
	modelVal, ok := doc.Frontmatter().Get("model")
	if !ok {
		t.Fatal("model field not found in output frontmatter")
	}
	if modelVal.Scalar != bareModelID {
		t.Errorf("model = %q, want %q (model id without provider prefix must not be reformatted)", modelVal.Scalar, bareModelID)
	}
}

// ---------------------------------------------------------------------------
// Deployment path and hook registration tests (T12.5)
// ---------------------------------------------------------------------------

// TestTargetPath_OpenCode_AgentProjectScope verifies that an agent deployed to project
// scope lands at ".opencode/agents/<key>.md".
func TestTargetPath_OpenCode_AgentProjectScope(t *testing.T) {
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
	want := ".opencode/agents/test-runner.md"
	if path != want {
		t.Errorf("TargetPath = %q, want %q", path, want)
	}
}

// TestTargetPath_OpenCode_SkillProjectScope verifies that a skill is deployed under a
// key-named subdirectory: ".opencode/skills/<key>/SKILL.md". The key subdirectory is
// required because all skill entry files are named SKILL.md.
func TestTargetPath_OpenCode_SkillProjectScope(t *testing.T) {
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
	want := ".opencode/skills/lean-tdd/SKILL.md"
	if path != want {
		t.Errorf("TargetPath = %q, want %q", path, want)
	}
}

// TestTargetPath_OpenCode_HookProjectScope verifies that hooks deploy to
// ".opencode/plugins/<filename>", not ".opencode/hooks/". OpenCode uses the
// "plugins/" directory for hooks; presence alone activates them.
func TestTargetPath_OpenCode_HookProjectScope(t *testing.T) {
	mod := newModule(t)
	path, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:     domain.ArtifactHook,
		Key:      "subagent-logger",
		FileName: "subagent-logger.ts",
		Scope:    domain.ScopeProject,
		GOOS:     "linux",
	})
	if err != nil {
		t.Fatalf("TargetPath: %v", err)
	}
	want := ".opencode/plugins/subagent-logger.ts"
	if path != want {
		t.Errorf("TargetPath = %q, want %q", path, want)
	}
}

// TestHookPlan_NoRegistrationSteps verifies that the OpenCode HookPlan has no registration
// steps. OpenCode activates plugins by presence in the plugins directory alone; no
// configuration file modification or editor setting is required.
func TestHookPlan_NoRegistrationSteps(t *testing.T) {
	mod := newModule(t)

	bundle := domain.HookBundle{
		Key:     "subagent-logger",
		Version: "1.0.0",
		Variants: map[string]domain.HookVariant{
			"opencode": {
				HarnessID: "opencode",
				Supported: true,
				Files: []domain.HookFile{
					{SourcePath: "/some/path/subagent-logger.ts", TargetName: "subagent-logger.ts"},
				},
				Registration: nil, // no registration steps for OpenCode
			},
		},
	}

	plan, err := mod.HookPlan(domain.HookPlanRequest{
		Bundle: bundle,
		Scope:  domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("HookPlan: %v", err)
	}
	if !plan.Supported {
		t.Fatalf("HookPlan.Supported = false, want true; OpenCode supports hooks")
	}
	if len(plan.Registration) != 0 {
		t.Errorf("HookPlan.Registration has %d steps, want 0; OpenCode hooks require no registration (presence alone activates them)", len(plan.Registration))
	}
}

// TestHookPlan_TargetDirIsPluginsDir verifies that the HookPlan deploys to the plugins
// directory (.opencode/plugins/), not a hooks directory.
func TestHookPlan_TargetDirIsPluginsDir(t *testing.T) {
	mod := newModule(t)

	bundle := domain.HookBundle{
		Key:     "subagent-logger",
		Version: "1.0.0",
		Variants: map[string]domain.HookVariant{
			"opencode": {
				HarnessID: "opencode",
				Supported: true,
				Files:     []domain.HookFile{{SourcePath: "/path/file.ts", TargetName: "file.ts"}},
			},
		},
	}

	plan, err := mod.HookPlan(domain.HookPlanRequest{
		Bundle: bundle,
		Scope:  domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("HookPlan: %v", err)
	}
	want := ".opencode/plugins"
	if plan.TargetDir != want {
		t.Errorf("HookPlan.TargetDir = %q, want %q", plan.TargetDir, want)
	}
}

// ---------------------------------------------------------------------------
// Harness-level injection tests (T12.6)
// ---------------------------------------------------------------------------

// TestInjection_OpenCode_HarnessConstraintsFilled verifies that HarnessConstraints is
// filled with both the parallel tool calls instruction and the working directory note.
// OpenCode adds both constraints; GHCP CLI adds only the parallel tool calls instruction.
func TestInjection_OpenCode_HarnessConstraintsFilled(t *testing.T) {
	mod := newModule(t)
	content, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints"})
	if !ok {
		t.Fatal("Injection(\"HarnessConstraints\") returned ok=false; OpenCode must fill this injection")
	}
	if content != opencodeHarnessConstraints {
		t.Errorf("Injection(\"HarnessConstraints\"):\n  got:  %q\n  want: %q", content, opencodeHarnessConstraints)
	}
}

// TestInjection_OpenCode_LanguagePatternsIsEmpty verifies that LanguagePatterns is
// declared but empty (no language-specific pattern guidance for OpenCode agents).
func TestInjection_OpenCode_LanguagePatternsIsEmpty(t *testing.T) {
	mod := newModule(t)
	content, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns"})
	if !ok {
		t.Fatal("Injection(\"LanguagePatterns\") returned ok=false; OpenCode must declare this injection")
	}
	if content != "" {
		t.Errorf("Injection(\"LanguagePatterns\") = %q; want empty string", content)
	}
}

// TestInjection_OpenCode_ProjectInjectionsNotFilled verifies that project-class injections
// are not filled by the OpenCode harness (they are preserved on update, empty on create).
func TestInjection_OpenCode_ProjectInjectionsNotFilled(t *testing.T) {
	mod := newModule(t)
	projectInjections := []string{
		"IdentityExtension",
		"ProtocolExtension",
		"CodebaseContext",
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

// TestInjection_OpenCode_InjectionsVersion verifies that the descriptor carries
// injections_version "1.3.1". This version stamps every deployed agent and signals
// that agent content must be refreshed when the harness injections change.
func TestInjection_OpenCode_InjectionsVersion(t *testing.T) {
	mod := newModule(t)
	d := mod.Descriptor()
	if d == nil {
		t.Fatal("Descriptor() returned nil")
	}
	want := "1.3.1"
	if d.InjectionsVersion != want {
		t.Errorf("Descriptor().InjectionsVersion = %q, want %q", d.InjectionsVersion, want)
	}
}

// TestInjection_OpenCode_ToolUniverseFromDescriptor verifies that the tool universe and
// default dispositions come from the descriptor data, not from Go constants. This is tested
// by confirming that the descriptor is non-nil, carries a non-empty universe with the
// permission shape, and that each universe entry has an Unused disposition.
func TestInjection_OpenCode_ToolUniverseFromDescriptor(t *testing.T) {
	mod := newModule(t)
	d := mod.Descriptor()
	if d == nil {
		t.Fatal("Descriptor() returned nil")
	}
	if d.Tools.Shape != domain.ShapePermission {
		t.Errorf("Descriptor().Tools.Shape = %q, want %q; OpenCode uses permission shape", d.Tools.Shape, domain.ShapePermission)
	}
	if len(d.Tools.Universe) == 0 {
		t.Fatal("Descriptor().Tools.Universe is empty; must declare the full tool universe")
	}
	for i, tool := range d.Tools.Universe {
		if tool.Unused == "" {
			t.Errorf("Universe[%d] (%q): Unused disposition is empty; every tool must declare allow or deny", i, tool.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Shared contract test (T12.7)
// ---------------------------------------------------------------------------

// TestContract_OpenCode runs the shared HarnessModule contract suite against the OpenCode
// module. This ensures OpenCode satisfies every invariant that all provision tiers must
// exhibit identically.
func TestContract_OpenCode(t *testing.T) {
	mod := newModule(t) // RED: fails here until implementation is complete (I12.2)

	// testRunnerAllowed is the full permission mapping for the test-runner tool set.
	// test-runner uses [file_read, file_write, file_edit, file_search, content_search,
	// terminal, user_interaction], which map to [read, write, edit, glob, grep, bash,
	// question]. The "list" tool is by_convention (always allow).
	testRunnerAllowed := map[string]bool{
		"read": true, "write": true, "edit": true, "glob": true,
		"grep": true, "bash": true, "question": true,
	}

	// contractsReviewAllowed is the full permission mapping for contracts-review.
	// contracts-review uses [skill, file_read, file_write, file_edit, file_search,
	// content_search, user_interaction], mapping to [skill, read, write, edit, glob,
	// grep, question]. No terminal → bash:deny.
	contractsReviewAllowed := map[string]bool{
		"read": true, "write": true, "edit": true, "glob": true,
		"grep": true, "question": true, "skill": true,
	}

	// orchestratorPlaceholderAllowed reflects the placeholder_expansion for OpenCode:
	// read, write, edit, glob, grep, bash, question, task. Plus list (by_convention).
	orchestratorPlaceholderAllowed := map[string]bool{
		"read": true, "write": true, "edit": true, "glob": true,
		"grep": true, "bash": true, "question": true, "task": true,
	}

	contracttest.Run(t, mod, contracttest.Fixtures{
		ToolCases: []contracttest.ToolCase{
			{
				// permission_shape_all_seven_generic_tools: verifies that the permission block
				// contains all 15 universe tools in stable order with correct allow/deny dispositions.
				// This is the canonical behaviour that most distinguishes OpenCode from the list-shape
				// harnesses (Claude Code, GHCP CLI): every unused tool is explicitly deny.
				Name: "permission_shape_all_seven_generic_tools",
				Request: domain.ToolRequest{
					AgentKey: "test-runner",
					Generic:  []string{"file_read", "file_write", "file_edit", "file_search", "content_search", "terminal", "user_interaction"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key:   "permission",
						Value: domain.MappingValue(permissionPairs(testRunnerAllowed)),
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "file_read", Outcome: domain.ToolMapped, HarnessTools: []string{"read"}},
					{Generic: "file_write", Outcome: domain.ToolMapped, HarnessTools: []string{"write"}},
					{Generic: "file_edit", Outcome: domain.ToolMapped, HarnessTools: []string{"edit"}},
					{Generic: "file_search", Outcome: domain.ToolMapped, HarnessTools: []string{"glob"}},
					{Generic: "content_search", Outcome: domain.ToolMapped, HarnessTools: []string{"grep"}},
					{Generic: "terminal", Outcome: domain.ToolMapped, HarnessTools: []string{"bash"}},
					{Generic: "user_interaction", Outcome: domain.ToolMapped, HarnessTools: []string{"question"}},
				},
			},
			{
				// skill_agent_has_skill_allow_and_no_bash: verifies many things about the
				// contracts-review agent transform. skill:allow because contracts-review uses
				// the skill generic tool; bash:deny because it does not use terminal.
				Name: "skill_agent_has_skill_allow_and_no_bash",
				Request: domain.ToolRequest{
					AgentKey: "contracts-review",
					Generic:  []string{"skill", "file_read", "file_write", "file_edit", "file_search", "content_search", "user_interaction"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key:   "permission",
						Value: domain.MappingValue(permissionPairs(contractsReviewAllowed)),
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "skill", Outcome: domain.ToolMapped, HarnessTools: []string{"skill"}},
					{Generic: "file_read", Outcome: domain.ToolMapped, HarnessTools: []string{"read"}},
					{Generic: "file_write", Outcome: domain.ToolMapped, HarnessTools: []string{"write"}},
					{Generic: "file_edit", Outcome: domain.ToolMapped, HarnessTools: []string{"edit"}},
					{Generic: "file_search", Outcome: domain.ToolMapped, HarnessTools: []string{"glob"}},
					{Generic: "content_search", Outcome: domain.ToolMapped, HarnessTools: []string{"grep"}},
					{Generic: "user_interaction", Outcome: domain.ToolMapped, HarnessTools: []string{"question"}},
				},
			},
			{
				// subagent_maps_to_task: verifies that the "subagent" generic tool maps to
				// task:allow. This is the mechanism by which the orchestrator can invoke subagents.
				Name: "subagent_maps_to_task",
				Request: domain.ToolRequest{
					AgentKey: "orchestrator",
					Generic:  []string{"subagent"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key: "permission",
						Value: domain.MappingValue(permissionPairs(map[string]bool{
							"task": true,
						})),
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "subagent", Outcome: domain.ToolMapped, HarnessTools: []string{"task"}},
				},
			},
			{
				// placeholder_expansion_for_orchestrator: verifies that the {tool-permissions}
				// placeholder expands to the descriptor's placeholder_expansion set with a full
				// 15-entry permission block. The orchestrator needs read, write, edit, glob, grep,
				// bash, question, and task allowed.
				Name: "placeholder_expansion_for_orchestrator",
				Request: domain.ToolRequest{
					AgentKey:    "orchestrator",
					Placeholder: "{tool-permissions}",
				},
				Fields: []domain.FrontmatterField{
					{
						Key:   "permission",
						Value: domain.MappingValue(permissionPairs(orchestratorPlaceholderAllowed)),
					},
				},
				Resolutions: nil, // placeholder produces no per-generic resolutions
			},
		},

		FrontmatterCases: []contracttest.FrontmatterCase{
			{
				// drops_name_adds_mode_subagent_applies_key_order: the key differences from
				// other harnesses are (a) name is dropped (not just generic-only keys),
				// (b) mode:subagent is added (a new harness-specific field), and (c) the key
				// order places description before mode and permission after model.
				// Model and version stamps are NOT in Set — transform applies them exclusively.
				Name: "drops_name_adds_mode_subagent_applies_key_order",
				Request: domain.FrontmatterRequest{
					Kind:     domain.ArtifactAgent,
					AgentKey: "test-runner",
					Model: domain.ModelSelection{
						ModelID: "github-copilot/claude-sonnet-4-6",
						Origin:  domain.OriginHarnessList,
					},
					Versions: domain.VersionStamps{
						TransformVersion:  "3.0.0",
						InjectionsVersion: "1.3.1",
					},
				},
				Expected: domain.FrontmatterPlan{
					Set: []domain.FrontmatterField{
						{Key: "mode", Value: domain.ScalarValue("subagent", domain.QuotePlain)},
					},
					Remove:   []string{"name", "recommended_tier", "tier_rationale", "required_skills", "tools"},
					KeyOrder: []string{"id", "version", "transform_version", "injections_version", "description", "mode", "model", "permission"},
				},
			},
			{
				// orchestrator_gets_mode_primary: the orchestrator is a primary agent in OpenCode
				// (user-facing, not invoked by other agents), so its mode must be "primary" rather
				// than "subagent". The module detects the orchestrator via AgentKey == "orchestrator".
				Name: "orchestrator_gets_mode_primary",
				Request: domain.FrontmatterRequest{
					Kind:     domain.ArtifactAgent,
					AgentKey: "orchestrator",
					Model: domain.ModelSelection{
						ModelID: "github-copilot/claude-sonnet-4-6",
						Origin:  domain.OriginHarnessList,
					},
					Versions: domain.VersionStamps{
						TransformVersion:  "3.0.0",
						InjectionsVersion: "1.3.1",
					},
				},
				Expected: domain.FrontmatterPlan{
					Set: []domain.FrontmatterField{
						{Key: "mode", Value: domain.ScalarValue("primary", domain.QuotePlain)},
					},
					Remove:   []string{"name", "recommended_tier", "tier_rationale", "required_skills", "tools"},
					KeyOrder: []string{"id", "version", "transform_version", "injections_version", "description", "mode", "model", "permission"},
				},
			},
		},

		InjectionCases: map[string]string{
			"HarnessConstraints": opencodeHarnessConstraints,
			"LanguagePatterns":   "",
		},

		NotFilled: []string{
			"IdentityExtension",
			"ProtocolExtension",
			"CodebaseContext",
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
				Expected: ".opencode/agents/test-runner.md",
			},
			{
				Name:     "skill_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactSkill, Key: "lean-tdd", FileName: "SKILL.md", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".opencode/skills/lean-tdd/SKILL.md",
			},
			{
				Name:     "hook_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactHook, Key: "subagent-logger", FileName: "subagent-logger.ts", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".opencode/plugins/subagent-logger.ts",
			},
			{
				Name:    "unsupported_kind_returns_sentinel",
				Request: domain.TargetPathRequest{Kind: domain.ArtifactKind("workflow"), Key: "x", Scope: domain.ScopeProject, GOOS: "linux"},
				Err:     domain.ErrArtifactUnsupported,
			},
		},

		HookPlanCases: []contracttest.HookPlanCase{
			{
				// hook_with_opencode_variant: verifies that a bundle that has an "opencode"
				// variant is supported and deploys to the plugins directory with no registration.
				Name: "hook_with_opencode_variant",
				Request: domain.HookPlanRequest{
					Bundle: domain.HookBundle{
						Key:     "subagent-logger",
						Version: "1.0.0",
						Variants: map[string]domain.HookVariant{
							"opencode": {
								HarnessID: "opencode",
								Supported: true,
								Files: []domain.HookFile{
									{SourcePath: "/path/subagent-logger.ts", TargetName: "subagent-logger.ts"},
								},
							},
						},
					},
					Scope: domain.ScopeProject,
				},
				Supported: true,
				TargetDir: ".opencode/plugins",
				FileNames: []string{"subagent-logger.ts"},
				StepIDs:   []string{}, // no registration steps
			},
		},
	})
}
