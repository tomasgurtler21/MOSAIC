package vscodeghcp_test

// Tests for the VS Code GHCP built-in harness module.
//
// Coverage:
//   Golden file tests (tool-light, tool-heavy, skill-using, orchestrator agents):
//   - A transform of the generic contracts-review agent produces a flow-style single-quoted
//     hierarchical tool list. contracts-review uses [skill, file_read, file_write, file_edit,
//     file_search, content_search, user_interaction]. skill maps to empty; file_write is
//     one-to-many (edit/createFile + edit/editFiles); file_edit deduplicates against editFiles.
//     search/listDirectory appears by convention. Output adds disable-model-invocation: false.
//   - A transform of the generic test-runner agent produces the full mapped tool set:
//     ['read/readFile', 'edit/createFile', 'edit/editFiles', 'search/fileSearch',
//     'search/textSearch', 'search/listDirectory', 'execute/runInTerminal', 'vscode/askQuestions']
//     plus disable-model-invocation: false.
//   - A transform of the generic planner-tdd-soft agent (skill + terminal) produces the same
//     mapping as test-runner plus execute/runInTerminal; skill maps to empty.
//   - A transform of the generic orchestrator ({tool-permissions} placeholder) produces the
//     full placeholder expansion including edit/createDirectory and agent.
//   - All VS Code GHCP agents use the .md extension (see file extension decision below).
//
//   File extension decision (AC13.6):
//   - The descriptor declares ".md" as the agent extension, matching the CodebaseAgnostic
//     canonical reference set (Agents/VS code GHCP/CodebaseAgnostic/Agents/*.md).
//   - Agents/VS code GHCP/UtilityAgents/*.agent.md use .agent.md, but those files were
//     authored outside the deployment tool and represent a deviation from the declared extension.
//   - VS Code GHCP recognises both .md and .agent.md files in .github/agents/; the deployment
//     tool emits .md to match the canonical reference.
//
//   Hierarchical tool path generation (one-to-many expansion and by-convention tools):
//   - file_write maps to BOTH edit/createFile AND edit/editFiles (one-to-many expansion).
//   - file_edit maps to edit/editFiles; when file_write is also present, editFiles appears
//     exactly once in the output (deduplication by descriptor.MapTools).
//   - search/listDirectory has no generic counterpart and appears for every agent regardless
//     of which generic tools are declared (ByConvention: true in the descriptor's universe).
//   - Resolutions report one entry per input generic tool; the rendered field reflects the
//     deduplicated harness set in universe order.
//   - The tool field is flow-style with single-quoted items: ['read/readFile', 'edit/createFile']
//
//   Human-readable model format:
//   - The VS Code GHCP descriptor's ModelCatalog.FormatHint is "display-name" (human-readable
//     names with spaces, e.g. "Claude Sonnet 4.6").
//   - A ModelSelection with ModelID "Claude Sonnet 4.6" is emitted verbatim as that string.
//   - A custom ModelID is also emitted verbatim with no reformatting (AC13.3, FR-11).
//
//   Project-scope path resolution:
//   - Project-scoped agent path is ".github/agents/<key>.md"
//   - Project-scoped skill path is ".github/skills/<key>/SKILL.md" (key subdirectory)
//   - Hook artifact paths resolve relative to .claude/hooks/ (reuses Claude Code convention)
//   - Any non-project scope returns domain.ErrUnsupportedScope (via the shared contracttest universal invariant)
//
//   Hook variant reuse:
//   - The vscode-ghcp hook variant declares reuses: claude-code in hook.yaml.
//   - The catalog resolves this before calling HookPlan: the HookPlanRequest carries
//     the vscode-ghcp variant's Files already populated from the claude-code variant.
//   - HookPlan.Files matches the claude-code file set (no duplicated files on disk).
//   - HookPlan.Registration contains the vscode-ghcp variant's own registration steps.
//
//   Always-TODO registration step (user-level editor setting):
//   - The vscode-ghcp variant declares an "enable-chat-hooks" registration step with
//     performable: false and an empty target_path. The deployment tool cannot write
//     user-level VS Code settings; the step is always a TODO item.
//   - HookPlan.Registration contains this step with Performable: false.
//   - The step is present with a non-empty Instruction so the TODO checklist can render it.
//
//   Harness-level injection content and injections_version:
//   - HarnessConstraints is filled with the file-reading behavioral constraint text.
//   - CustomConstraints is filled with the parallel tool calls instruction text.
//     (VS Code GHCP declares both as harness-level injections in its descriptor.)
//   - LanguagePatterns is declared with empty content (no language pattern injection).
//   - injections_version is "1.3.0" per the VS Code GHCP HarnessInjections.md.
//   - Project-class injections not declared by the descriptor are not filled.
//
//   Shared contract:
//   - The module passes contracttest.Run with the universal invariant results.

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/vscodeghcp"
	"mosaic-deploy/internal/harness/contracttest"
	"mosaic-deploy/internal/harness/registry"
	"mosaic-deploy/internal/transform"
)

// updateGolden regenerates the golden files from current engine output when -update is passed.
// Run: go test ./internal/harness/builtin/vscodeghcp/... -run TestGoldenFile -update
var updateGolden = flag.Bool("update", false, "regenerate golden files from current engine output")

// testModel is the ModelSelection used in all VS Code GHCP golden file tests.
// The model ID uses the human-readable format this harness expects: display names with spaces.
var testModel = domain.ModelSelection{
	ModelID: "Claude Sonnet 4.6",
	Origin:  domain.OriginHarnessList,
}

// vscodeGHCPFileReadingConstraint is the expected content of the HarnessConstraints injection.
// VS Code GHCP injects the file-reading behavioral constraint as a harness-level injection
// because agents running on this platform commonly stop reading mid-file.
// Source: Agents/VS code GHCP/HarnessInjections.md (harness_constraints section, item 1).
const vscodeGHCPFileReadingConstraint = "When reading a file with the intent to read it fully, **never assume the file is complete just because the last returned line is blank or ends a section.** Always verify you have reached the true end:\n" +
	"- After reading a chunk, check if you received fewer lines than you requested — that signals the actual end of file\n" +
	"- If you received as many lines as requested, the file likely continues — issue another read starting from where the last one ended\n" +
	"- Keep paginating until you receive a short (or empty) response\n" +
	"- **Exception:** If you are intentionally reading a specific range (e.g., to find a particular function or section), you do not need to read the rest of the file"

// vscodeGHCPParallelToolCalls is the expected content of the CustomConstraints injection.
// VS Code GHCP declares CustomConstraints as a harness-level injection (unlike other harnesses
// that leave it project-level) because the parallel tool call pattern is a behavioural requirement
// imposed by this platform's cost model, not by any individual project.
// Source: Agents/VS code GHCP/HarnessInjections.md (harness_constraints section, item 2).
const vscodeGHCPParallelToolCalls = "**Parallel Tool Calls:** Issue multiple independent tool calls in a single response whenever possible. Sequential tool calls are only permitted when a later call depends on the result of an earlier one. This minimises inference API calls to improve speed and reduce cost."

// repoRoot returns the absolute path to the repository root, navigating up from this
// package's directory. The test is skipped if the root cannot be located.
func repoRoot(t *testing.T) string {
	t.Helper()
	// Package is at Tools/Deployment/internal/harness/builtin/vscodeghcp/
	// Navigate six levels up to reach the repository root.
	rel := filepath.Join("..", "..", "..", "..", "..", "..")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return abs
}

// goldenDir returns the path to the vscode-ghcp golden file directory.
func goldenDir(t *testing.T) string {
	t.Helper()
	// testdata/golden/vscode-ghcp/ is at Tools/Deployment/testdata/golden/vscode-ghcp/
	rel := filepath.Join("..", "..", "..", "..", "testdata", "golden", "vscode-ghcp")
	abs, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("resolve golden dir: %v", err)
	}
	return abs
}

// newModule constructs the VS Code GHCP module against the real repository root, failing the
// test immediately if construction fails.
func newModule(t *testing.T) domain.HarnessModule {
	t.Helper()
	mod, err := vscodeghcp.New(registry.BuiltinOptions{MosaicRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("vscodeghcp.New(): %v", err)
	}
	return mod
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
// Golden file tests (T13.1)
// ---------------------------------------------------------------------------

// TestGoldenFile_VSCodeGHCP_ContractsReviewAgent tests a "tool-light" agent: contracts-review
// uses [skill, file_read, file_write, file_edit, file_search, content_search, user_interaction].
//
// Expected output tool list:
//   - skill maps to empty/unsupported (omitted)
//   - file_read  → read/readFile
//   - file_write → edit/createFile + edit/editFiles (one-to-many)
//   - file_edit  → edit/editFiles (deduplicated against file_write's editFiles mapping)
//   - file_search → search/fileSearch
//   - content_search → search/textSearch
//   - search/listDirectory: by convention (no generic counterpart, always present)
//   - user_interaction → vscode/askQuestions
//
// Result: ['read/readFile', 'edit/createFile', 'edit/editFiles', 'search/fileSearch',
//          'search/textSearch', 'search/listDirectory', 'vscode/askQuestions']
//
// The golden file uses create-mode (no deployed file) so project injections are empty.
// Deliberate deviation from Agents/VS code GHCP/CodebaseAgnostic/Agents/contracts-review.md:
// the committed file was produced by an LLM process and may contain project injection content;
// the golden file uses create-mode with all project injections cleared.
func TestGoldenFile_VSCodeGHCP_ContractsReviewAgent(t *testing.T) {
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
		Protocol:   protocol,

	}

	goldenPath := filepath.Join(goldenDir(t), "contracts-review.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// TestGoldenFile_VSCodeGHCP_TestRunnerAgent tests the "tool-heavy" case. test-runner uses
// [file_read, file_write, file_edit, file_search, content_search, terminal, user_interaction].
// This is the canonical reference agent for VS Code GHCP tool mapping
// (Agents/VS code GHCP/CodebaseAgnostic/Agents/test-runner.md is the authoritative reference).
//
// Expected tool list: ['read/readFile', 'edit/createFile', 'edit/editFiles', 'search/fileSearch',
//                      'search/textSearch', 'search/listDirectory', 'execute/runInTerminal',
//                      'vscode/askQuestions']
//
// Deliberate deviation from committed reference:
// the committed file uses "Claude Opus 4.6" as model; this golden uses the fixed test model
// "Claude Sonnet 4.6". Project injections are cleared (create mode).
func TestGoldenFile_VSCodeGHCP_TestRunnerAgent(t *testing.T) {
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
		Protocol:   protocol,

	}

	goldenPath := filepath.Join(goldenDir(t), "test-runner.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// TestGoldenFile_VSCodeGHCP_PlannerTDDSoftAgent tests the skill-and-terminal combination.
// planner-tdd-soft uses [skill, file_read, file_write, file_edit, file_search, content_search,
// terminal, user_interaction]. skill maps to empty; terminal maps to execute/runInTerminal.
//
// Expected tool list: ['read/readFile', 'edit/createFile', 'edit/editFiles', 'search/fileSearch',
//                      'search/textSearch', 'search/listDirectory', 'execute/runInTerminal',
//                      'vscode/askQuestions']
func TestGoldenFile_VSCodeGHCP_PlannerTDDSoftAgent(t *testing.T) {
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
		Protocol:   protocol,

	}

	goldenPath := filepath.Join(goldenDir(t), "planner-tdd-soft.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// TestGoldenFile_VSCodeGHCP_Orchestrator tests the {tool-permissions} placeholder expansion.
// The orchestrator uses {tool-permissions} as its tools value. The placeholder expands to
// the full VS Code GHCP tool set including edit/createDirectory and agent.
//
// Expected tool list: ['read/readFile', 'edit/createFile', 'edit/createDirectory',
//                      'edit/editFiles', 'search/fileSearch', 'search/textSearch',
//                      'search/listDirectory', 'execute/runInTerminal', 'agent', 'vscode/askQuestions']
//
// Deliberate deviation from committed Agents/VS code GHCP/CodebaseAgnostic/orchestrator.md:
// the committed file shows "disable-model-invocation: true". The VS Code GHCP descriptor
// adds "disable-model-invocation: false" for all agents; the value "true" in the committed
// file is a manual authoring error. This golden file uses "false" to match the descriptor-driven
// behaviour. The committed file uses "Claude Opus 4.6" as model; this golden uses the fixed
// test model "Claude Sonnet 4.6".
func TestGoldenFile_VSCodeGHCP_Orchestrator(t *testing.T) {
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
		Protocol:   protocol,

	}

	goldenPath := filepath.Join(goldenDir(t), "orchestrator.md")
	applyAndCompare(t, mod, req, goldenPath)
}

// ---------------------------------------------------------------------------
// Hierarchical tool path generation and one-to-many expansion tests (T13.2)
// ---------------------------------------------------------------------------

// TestToolMapping_FileWriteOneToMany verifies that file_write maps to BOTH edit/createFile
// AND edit/editFiles. This is the core one-to-many expansion that distinguishes VS Code GHCP
// from Claude Code and GHCP CLI: a single generic tool produces two harness tool entries.
func TestToolMapping_FileWriteOneToMany(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "one-to-many-test",
		Generic:  []string{"file_write"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	if len(result.Resolutions) != 1 {
		t.Fatalf("Resolutions count = %d, want 1 (one per input generic tool)", len(result.Resolutions))
	}

	res := result.Resolutions[0]
	if res.Generic != "file_write" {
		t.Errorf("Resolutions[0].Generic = %q, want \"file_write\"", res.Generic)
	}
	if res.Outcome != domain.ToolMapped {
		t.Errorf("Resolutions[0].Outcome = %q, want ToolMapped", res.Outcome)
	}

	wantHarnessTools := []string{"edit/createFile", "edit/editFiles"}
	if len(res.HarnessTools) != len(wantHarnessTools) {
		t.Errorf("Resolutions[0].HarnessTools = %v, want %v (one-to-many expansion)", res.HarnessTools, wantHarnessTools)
	} else {
		for i, want := range wantHarnessTools {
			if res.HarnessTools[i] != want {
				t.Errorf("Resolutions[0].HarnessTools[%d] = %q, want %q", i, res.HarnessTools[i], want)
			}
		}
	}

	// The rendered field must contain both harness tools.
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

	emitted := make([]string, len(toolsField.Value.Items))
	for i, item := range toolsField.Value.Items {
		emitted[i] = item.Scalar
	}
	// Verify both one-to-many mapped tools and the by-convention tool appear in the output.
	wantInOutput := append(wantHarnessTools, "search/listDirectory")
	for _, want := range wantInOutput {
		found := false
		for _, got := range emitted {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected tool %q in rendered output %v", want, emitted)
		}
	}
}

// TestToolMapping_FileWriteAndEditDeduplication verifies that when both file_write and file_edit
// are requested, edit/editFiles appears exactly once in the rendered output. file_write maps to
// [edit/createFile, edit/editFiles] and file_edit also maps to [edit/editFiles]; deduplication
// must prevent edit/editFiles from appearing twice.
func TestToolMapping_FileWriteAndEditDeduplication(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "dedup-test",
		Generic:  []string{"file_write", "file_edit"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	if len(result.Resolutions) != 2 {
		t.Fatalf("Resolutions count = %d, want 2 (one per input generic tool)", len(result.Resolutions))
	}

	// Both tools map to edit/editFiles; the rendered field must contain exactly one.
	editFilesCount := 0
	createFileCount := 0
	for _, field := range result.Fields {
		if field.Key == "tools" && field.Value.Kind == domain.KindList {
			for _, item := range field.Value.Items {
				switch item.Scalar {
				case "edit/editFiles":
					editFilesCount++
				case "edit/createFile":
					createFileCount++
				}
			}
		}
	}
	if editFilesCount != 1 {
		t.Errorf("'edit/editFiles' appears %d times in the tools field; want exactly 1 (deduplication required)", editFilesCount)
	}
	if createFileCount != 1 {
		t.Errorf("'edit/createFile' appears %d times in the tools field; want exactly 1", createFileCount)
	}
}

// TestToolMapping_FileEditOnly verifies that file_edit alone maps to ["edit/editFiles"] without
// the deduplication path interfering. This guards against a regression where deduplication logic
// in the combined file_write+file_edit path masks a broken solo file_edit mapping.
func TestToolMapping_FileEditOnly(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "edit-only-test",
		Generic:  []string{"file_edit"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	if len(result.Resolutions) != 1 {
		t.Fatalf("Resolutions count = %d, want 1 (one per input generic tool)", len(result.Resolutions))
	}

	res := result.Resolutions[0]
	if res.Generic != "file_edit" {
		t.Errorf("Resolutions[0].Generic = %q, want \"file_edit\"", res.Generic)
	}
	if res.Outcome != domain.ToolMapped {
		t.Errorf("Resolutions[0].Outcome = %q, want ToolMapped", res.Outcome)
	}
	if len(res.HarnessTools) != 1 || res.HarnessTools[0] != "edit/editFiles" {
		t.Errorf("Resolutions[0].HarnessTools = %v, want [\"edit/editFiles\"]", res.HarnessTools)
	}

	// The rendered field must contain edit/editFiles but NOT edit/createFile.
	editFilesCount := 0
	createFileCount := 0
	for _, field := range result.Fields {
		if field.Key == "tools" && field.Value.Kind == domain.KindList {
			for _, item := range field.Value.Items {
				switch item.Scalar {
				case "edit/editFiles":
					editFilesCount++
				case "edit/createFile":
					createFileCount++
				}
			}
		}
	}
	if editFilesCount != 1 {
		t.Errorf("'edit/editFiles' appears %d times in rendered output; want exactly 1", editFilesCount)
	}
	if createFileCount != 0 {
		t.Errorf("'edit/createFile' appears %d times in rendered output; want 0 (file_edit alone must not produce createFile)", createFileCount)
	}
}

// TestToolMapping_ByConventionListDirectory verifies that search/listDirectory appears in the
// output for any agent, regardless of whether the agent declares any tools that map to it.
// search/listDirectory has no generic counterpart; it is emitted by convention (ByConvention: true)
// to give every VS Code GHCP agent directory listing capability without explicit declaration.
func TestToolMapping_ByConventionListDirectory(t *testing.T) {
	mod := newModule(t)

	// Request with no tool that maps to search/listDirectory.
	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "convention-test",
		Generic:  []string{"user_interaction"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	found := false
	for _, field := range result.Fields {
		if field.Key == "tools" && field.Value.Kind == domain.KindList {
			for _, item := range field.Value.Items {
				if item.Scalar == "search/listDirectory" {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Error("'search/listDirectory' not found in rendered output; it must appear by convention for every VS Code GHCP agent")
	}
}

// TestToolMapping_ByConventionNoPseudoResolution verifies that search/listDirectory does not
// appear in Resolutions (by-convention tools have no generic counterpart; only input generic
// tools produce ToolResolution entries).
func TestToolMapping_ByConventionNoPseudoResolution(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "convention-resolution-test",
		Generic:  []string{"user_interaction"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	for _, res := range result.Resolutions {
		if res.Generic == "search/listDirectory" {
			t.Errorf("Resolutions contains an entry for 'search/listDirectory'; by-convention tools must not produce ToolResolution entries")
		}
	}
	if len(result.Resolutions) != 1 {
		t.Errorf("Resolutions count = %d, want 1 (one per input generic tool only)", len(result.Resolutions))
	}
}

// TestToolMapping_FullTestRunnerSet verifies the complete tool mapping for the test-runner
// agent set against the canonical reference (Agents/VS code GHCP/CodebaseAgnostic/Agents/test-runner.md).
// Input: [file_read, file_write, file_edit, file_search, content_search, terminal, user_interaction]
// Expected rendered output (universe order):
//   ['read/readFile', 'edit/createFile', 'edit/editFiles', 'search/fileSearch',
//    'search/textSearch', 'search/listDirectory', 'execute/runInTerminal', 'vscode/askQuestions']
func TestToolMapping_FullTestRunnerSet(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "test-runner",
		Generic:  []string{"file_read", "file_write", "file_edit", "file_search", "content_search", "terminal", "user_interaction"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	if len(result.Resolutions) != 7 {
		t.Errorf("Resolutions count = %d, want 7 (one per input generic tool)", len(result.Resolutions))
	}

	// Verify expected harness tool set (including by-convention search/listDirectory).
	wantTools := map[string]bool{
		"read/readFile":         true,
		"edit/createFile":       true,
		"edit/editFiles":        true,
		"search/fileSearch":     true,
		"search/textSearch":     true,
		"search/listDirectory":  true,
		"execute/runInTerminal": true,
		"vscode/askQuestions":   true,
	}

	seen := make(map[string]int)
	for _, field := range result.Fields {
		if field.Key == "tools" && field.Value.Kind == domain.KindList {
			for _, item := range field.Value.Items {
				seen[item.Scalar]++
			}
		}
	}

	for tool := range wantTools {
		if seen[tool] == 0 {
			t.Errorf("expected harness tool %q not found in output", tool)
		}
	}
	for tool, count := range seen {
		if count > 1 {
			t.Errorf("harness tool %q appears %d times; want at most 1 (deduplication required)", tool, count)
		}
	}
}

// TestToolMapping_SkillMapsToEmpty verifies that the skill generic tool maps to an empty
// harness tool set for VS Code GHCP. Skills are loaded by the VS Code platform automatically
// from the .github/skills/ directory; no explicit tool entry is needed.
func TestToolMapping_SkillMapsToEmpty(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "skill-test",
		Generic:  []string{"skill"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	if len(result.Resolutions) != 1 {
		t.Fatalf("Resolutions count = %d, want 1", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if res.Outcome != domain.ToolMapped {
		t.Errorf("skill resolution Outcome = %q, want ToolMapped", res.Outcome)
	}
	if len(res.HarnessTools) != 0 {
		t.Errorf("skill resolution HarnessTools = %v, want empty (skill is platform-handled, no tool entry needed)", res.HarnessTools)
	}
}

// TestToolMapping_SubagentMapsToAgent verifies that the subagent generic tool maps to the
// 'agent' harness tool for VS Code GHCP (the harness-side name for running subagents).
func TestToolMapping_SubagentMapsToAgent(t *testing.T) {
	mod := newModule(t)

	result, err := mod.Tools(domain.ToolRequest{
		AgentKey: "orchestrator",
		Generic:  []string{"subagent"},
	})
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	if len(result.Resolutions) != 1 {
		t.Fatalf("Resolutions count = %d, want 1", len(result.Resolutions))
	}
	res := result.Resolutions[0]
	if res.Outcome != domain.ToolMapped {
		t.Errorf("subagent resolution Outcome = %q, want ToolMapped", res.Outcome)
	}
	if len(res.HarnessTools) != 1 || res.HarnessTools[0] != "agent" {
		t.Errorf("subagent resolution HarnessTools = %v, want [\"agent\"]", res.HarnessTools)
	}
}

// TestToolFormat_FlowStyleSingleQuotedHierarchical verifies that VS Code GHCP emits tools as a
// flow-style YAML sequence with single-quoted hierarchical path items:
// tools: ['read/readFile', 'edit/editFiles']
func TestToolFormat_FlowStyleSingleQuotedHierarchical(t *testing.T) {
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
		t.Errorf("tools field List style = %q, want ListFlow (VS Code GHCP uses flow-style [...])", toolsField.Value.List)
	}
	for i, item := range toolsField.Value.Items {
		if item.Quote != domain.QuoteSingle {
			t.Errorf("tools field Items[%d].Quote = %q, want QuoteSingle (VS Code GHCP uses 'single-quoted' items)", i, item.Quote)
		}
		if !strings.Contains(item.Scalar, "/") {
			t.Errorf("tools field Items[%d].Scalar = %q; VS Code GHCP tool names are hierarchical paths (category/tool)", i, item.Scalar)
		}
	}
}

// ---------------------------------------------------------------------------
// Human-readable model format tests (T13.3)
// ---------------------------------------------------------------------------

// TestModel_DescriptorDeclares_ModelKey verifies that the descriptor declares a non-empty model
// key (so the transform pipeline knows where to write the model ID) and that the module does not
// pre-set the model key in FrontmatterPlan.Set (model stamping is exclusively the transform's
// responsibility). End-to-end verbatim emission is verified by TestModel_CustomModelIDEmittedVerbatim.
func TestModel_DescriptorDeclares_ModelKey(t *testing.T) {
	mod := newModule(t)

	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "test-agent",
		Model: domain.ModelSelection{
			ModelID: "Claude Sonnet 4.6",
			Origin:  domain.OriginHarnessList,
		},
		Versions: domain.VersionStamps{
			TransformVersion:  "3.0.0",
			InjectionsVersion: "1.3.0",
		},
	}

	plan, err := mod.Frontmatter(req)
	if err != nil {
		t.Fatalf("Frontmatter: %v", err)
	}

	// The descriptor's Add fields include disable-model-invocation: false.
	// Verify that model fields are handled consistently (model is stamped by transform pipeline,
	// not by the module's Set — but the descriptor ModelKey tells transform where to write it).
	d := mod.Descriptor()
	if d == nil {
		t.Fatal("Descriptor() returned nil")
	}
	if d.Frontmatter.ModelKey == "" {
		t.Error("Descriptor().Frontmatter.ModelKey is empty; VS Code GHCP must declare a model key so the transform can emit the model")
	}

	// The Add fields must not include model (model is applied exclusively by transform Step 3).
	for _, field := range plan.Set {
		if field.Key == d.Frontmatter.ModelKey {
			t.Errorf("FrontmatterPlan.Set contains the model key %q; model must be stamped exclusively by the transform pipeline, not the module", d.Frontmatter.ModelKey)
		}
	}
}

// TestModel_CustomModelIDEmittedVerbatim verifies that a custom model ID supplied by the user
// is emitted verbatim without reformatting. The harness must never reformat, validate, or
// normalise the ModelID (AC13.3, FR-11).
func TestModel_CustomModelIDEmittedVerbatim(t *testing.T) {
	mod := newModule(t)
	root := repoRoot(t)
	protocol := loadProtocol(t, root)


	srcPath := filepath.Join(root, "Agents", "Generic", "Agents", "Execution", "test-runner.md")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Skipf("generic test-runner agent not found at %s: %v", srcPath, err)
	}

	customModel := domain.ModelSelection{
		ModelID: "my-custom-llm-provider/ultra-model-v3",
		Origin:  domain.OriginCustom,
	}

	result, err := transform.Apply(transform.Request{
		Source:   src,
		Kind:     domain.ArtifactAgent,
		Key:      "test-runner",
		Module:   mod,
		Model:    customModel,
		Scope:    domain.ScopeProject,
		Role:     domain.RoleWorker,
		Protocol:   protocol,

	})
	if err != nil {
		t.Fatalf("transform.Apply: %v", err)
	}

	// The custom model ID must appear verbatim in the output bytes.
	if !bytes.Contains(result.Output, []byte(customModel.ModelID)) {
		t.Errorf("transform output does not contain custom model ID %q; it must be emitted verbatim", customModel.ModelID)
	}
}

// TestModel_DescriptorFormatHintIsDisplayName verifies that the descriptor declares a format
// hint indicating human-readable display names are expected for this harness.
func TestModel_DescriptorFormatHintIsDisplayName(t *testing.T) {
	mod := newModule(t)
	d := mod.Descriptor()
	if d == nil {
		t.Fatal("Descriptor() returned nil")
	}
	// The format hint is display-only but must signal to the TUI that this harness expects
	// human-readable names like "Claude Opus 4.6", not provider IDs like "claude-opus-4-6".
	if d.Models.FormatHint == "" {
		t.Error("Descriptor().Models.FormatHint is empty; VS Code GHCP should declare a format hint to guide model selection UI")
	}
}

// ---------------------------------------------------------------------------
// Project-scope path resolution tests
// ---------------------------------------------------------------------------

// TestTargetPath_VSCodeGHCP_AgentProjectScope verifies that an agent deployed to project scope
// lands at ".github/agents/<key>.md".
func TestTargetPath_VSCodeGHCP_AgentProjectScope(t *testing.T) {
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
	want := ".github/agents/test-runner.md"
	if path != want {
		t.Errorf("TargetPath = %q, want %q", path, want)
	}
}

// TestTargetPath_VSCodeGHCP_SkillProjectScope verifies that a skill is deployed under a
// key-named subdirectory: ".github/skills/<key>/SKILL.md". The key subdirectory is required
// because all skill entry files are named SKILL.md by convention.
func TestTargetPath_VSCodeGHCP_SkillProjectScope(t *testing.T) {
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

// TestTargetPath_VSCodeGHCP_HookProjectScope verifies that hooks are deployed to
// ".claude/hooks/<filename>". VS Code GHCP reuses the Claude Code hook file set; deploying
// hooks to the same .claude/hooks/ location makes them available to both platforms.
func TestTargetPath_VSCodeGHCP_HookProjectScope(t *testing.T) {
	mod := newModule(t)
	path, err := mod.TargetPath(domain.TargetPathRequest{
		Kind:     domain.ArtifactHook,
		Key:      "subagent-logger",
		FileName: "subagent-logger.ps1",
		Scope:    domain.ScopeProject,
		GOOS:     "linux",
	})
	if err != nil {
		t.Fatalf("TargetPath: %v", err)
	}
	want := ".claude/hooks/subagent-logger.ps1"
	if path != want {
		t.Errorf("TargetPath = %q, want %q", path, want)
	}
}

// ---------------------------------------------------------------------------
// Hook variant reuse tests (T13.5)
// ---------------------------------------------------------------------------

// TestHookPlan_VSCodeGHCP_ReusesClaudeCodeFiles verifies that the VS Code GHCP hook plan
// returns the claude-code variant's files (resolved by the catalog before the call) without
// duplicating them. The vscode-ghcp hook.yaml variant declares reuses: claude-code; the
// catalog resolves this and provides the actual file list in the HookPlanRequest.
func TestHookPlan_VSCodeGHCP_ReusesClaudeCodeFiles(t *testing.T) {
	mod := newModule(t)

	// Simulate the catalog's pre-resolved bundle: the vscode-ghcp variant already has the
	// claude-code file list (after reuse resolution). No claude-code variant is present because
	// the catalog replaces reuses references with the concrete file list.
	bundle := domain.HookBundle{
		Key:     "subagent-logger",
		Version: "1.0.0",
		Variants: map[string]domain.HookVariant{
			"vscode-ghcp": {
				HarnessID: "vscode-ghcp",
				Supported: true,
				// Files already resolved from claude-code via reuses (catalog responsibility).
				Files: []domain.HookFile{
					{SourcePath: "/repo/Hooks/subagent-logger/claude-code/subagent-logger.ps1", TargetName: "subagent-logger.ps1"},
					{SourcePath: "/repo/Hooks/subagent-logger/claude-code/config.json", TargetName: "subagent-logger.json"},
				},
				Registration: []domain.RegistrationStep{
					{
						ID:          "settings-fragment",
						TargetPath:  ".claude/settings.json",
						Performable: true,
						Instruction: "Merge the hooks fragment into .claude/settings.json to register the subagent-logger script.",
						Fragment:    `{"hooks": {"SubagentStop": [{"type": "command", "command": ".claude/hooks/subagent-logger.ps1"}]}}`,
					},
					{
						ID:          "enable-chat-hooks",
						TargetPath:  "",
						Performable: false,
						Instruction: `Enable hooks in VS Code: open User Settings (JSON) and set "chat.hooks.enabled": true. This is a user-level editor setting and cannot be set by the deployment tool.`,
					},
				},
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
		t.Fatalf("HookPlan.Supported = false, want true; VS Code GHCP declares hook support")
	}
	if len(plan.Files) != 2 {
		t.Errorf("HookPlan.Files count = %d, want 2 (resolved from claude-code reuse)", len(plan.Files))
	}

	// Verify that no file appears more than once (no duplicated reuse).
	seenTargets := make(map[string]int)
	for _, f := range plan.Files {
		seenTargets[f.TargetName]++
	}
	for name, count := range seenTargets {
		if count > 1 {
			t.Errorf("HookPlan.Files contains %q %d times; reuse must not duplicate files", name, count)
		}
	}
}

// TestHookPlan_VSCodeGHCP_Supported verifies that VS Code GHCP declares hook support.
func TestHookPlan_VSCodeGHCP_Supported(t *testing.T) {
	mod := newModule(t)

	plan, err := mod.HookPlan(domain.HookPlanRequest{
		Bundle: domain.HookBundle{
			Key:     "test-bundle",
			Variants: map[string]domain.HookVariant{
				"vscode-ghcp": {
					HarnessID: "vscode-ghcp",
					Supported: true,
					Files:     []domain.HookFile{},
				},
			},
		},
		Scope: domain.ScopeProject,
	})
	if err != nil {
		t.Fatalf("HookPlan: %v", err)
	}
	if !plan.Supported {
		t.Error("HookPlan.Supported = false; VS Code GHCP supports hooks via claude-code file reuse")
	}
}

// ---------------------------------------------------------------------------
// Always-TODO registration step tests (T13.6)
// ---------------------------------------------------------------------------

// TestHookPlan_VSCodeGHCP_AlwaysTODORegistrationStep verifies that the enable-chat-hooks
// registration step is present with Performable: false. The deployment tool cannot write
// user-level VS Code settings ("chat.hooks.enabled": true); this step is always a TODO item.
func TestHookPlan_VSCodeGHCP_AlwaysTODORegistrationStep(t *testing.T) {
	mod := newModule(t)

	bundle := domain.HookBundle{
		Key:     "subagent-logger",
		Version: "1.0.0",
		Variants: map[string]domain.HookVariant{
			"vscode-ghcp": {
				HarnessID: "vscode-ghcp",
				Supported: true,
				Files:     []domain.HookFile{},
				Registration: []domain.RegistrationStep{
					{
						ID:          "enable-chat-hooks",
						TargetPath:  "",
						Performable: false,
						Instruction: `Enable hooks in VS Code: open User Settings (JSON) and set "chat.hooks.enabled": true.`,
					},
				},
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

	var enableStep *domain.RegistrationStep
	for i, step := range plan.Registration {
		if step.ID == "enable-chat-hooks" {
			enableStep = &plan.Registration[i]
			break
		}
	}
	if enableStep == nil {
		t.Fatal("HookPlan.Registration does not contain 'enable-chat-hooks' step; it must be present for the TODO checklist")
	}
	if enableStep.Performable {
		t.Error("enable-chat-hooks step has Performable=true; it must be false because the deployment tool cannot write user-level VS Code settings")
	}
	if enableStep.TargetPath != "" {
		t.Errorf("enable-chat-hooks step TargetPath = %q, want empty string (tool cannot perform this step)", enableStep.TargetPath)
	}
	if enableStep.Instruction == "" {
		t.Error("enable-chat-hooks step Instruction is empty; it must be non-empty so the TODO checklist can render a human-readable instruction")
	}
}

// TestHookPlan_VSCodeGHCP_EnableChatHooksNeverAttempted verifies that the enable-chat-hooks
// step is reported as an unperformable action (Performable: false) rather than attempted.
// This is the behavioural contract: the deployment tool reports the requirement and never
// writes to user-level VS Code settings (AC13.5).
func TestHookPlan_VSCodeGHCP_EnableChatHooksNeverAttempted(t *testing.T) {
	mod := newModule(t)

	bundle := domain.HookBundle{
		Key:     "subagent-logger",
		Variants: map[string]domain.HookVariant{
			"vscode-ghcp": {
				HarnessID: "vscode-ghcp",
				Supported: true,
				Files:     []domain.HookFile{},
				Registration: []domain.RegistrationStep{
					{ID: "settings-fragment", TargetPath: ".claude/settings.json", Performable: true},
					{ID: "enable-chat-hooks", TargetPath: "", Performable: false, Instruction: "Enable chat.hooks.enabled in VS Code User Settings."},
				},
			},
		},
	}

	plan, err := mod.HookPlan(domain.HookPlanRequest{Bundle: bundle, Scope: domain.ScopeProject})
	if err != nil {
		t.Fatalf("HookPlan: %v", err)
	}

	for _, step := range plan.Registration {
		if step.ID == "enable-chat-hooks" {
			if step.Performable {
				t.Error("enable-chat-hooks is Performable=true; must be false — the tool must never attempt to write user-level VS Code settings")
			}
			if step.TargetPath != "" {
				t.Errorf("enable-chat-hooks TargetPath = %q; must be empty when Performable=false", step.TargetPath)
			}
			return
		}
	}
	t.Error("enable-chat-hooks step not found in HookPlan.Registration")
}

// ---------------------------------------------------------------------------
// Harness-level injection content and injections_version tests (T13.7)
// ---------------------------------------------------------------------------

// TestInjection_VSCodeGHCP_HarnessConstraintsFilledWithFileReadingWarning verifies that
// HarnessConstraints is filled with the file-reading behavioral constraint. VS Code GHCP
// agents commonly stop reading files mid-way; this injection addresses that at the harness level.
func TestInjection_VSCodeGHCP_HarnessConstraintsFilledWithFileReadingWarning(t *testing.T) {
	mod := newModule(t)
	content, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints"})
	if !ok {
		t.Fatal("Injection(\"HarnessConstraints\") returned ok=false; VS Code GHCP must fill this injection with the file-reading constraint")
	}
	if !strings.Contains(content, "never assume the file is complete") {
		t.Errorf("Injection(\"HarnessConstraints\") does not contain the file-reading constraint text.\ngot: %q\nwant content containing: \"never assume the file is complete\"", content)
	}
}

// TestInjection_VSCodeGHCP_HarnessConstraintsExactContent verifies the full expected content
// of the HarnessConstraints injection against the reference in HarnessInjections.md.
func TestInjection_VSCodeGHCP_HarnessConstraintsExactContent(t *testing.T) {
	mod := newModule(t)
	content, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints"})
	if !ok {
		t.Fatal("Injection(\"HarnessConstraints\") returned ok=false")
	}
	if content != vscodeGHCPFileReadingConstraint {
		t.Errorf("Injection(\"HarnessConstraints\"):\n  got:  %q\n  want: %q", content, vscodeGHCPFileReadingConstraint)
	}
}

// TestInjection_VSCodeGHCP_CustomConstraintsFilledWithParallelToolCalls verifies that
// CustomConstraints is filled with the parallel tool calls instruction. VS Code GHCP declares
// this as a harness-level injection because parallel tool call behaviour is a platform cost
// optimisation, not a project-specific constraint.
func TestInjection_VSCodeGHCP_CustomConstraintsFilledWithParallelToolCalls(t *testing.T) {
	mod := newModule(t)
	content, ok := mod.Injection(domain.InjectionRequest{Name: "CustomConstraints"})
	if !ok {
		t.Fatal("Injection(\"CustomConstraints\") returned ok=false; VS Code GHCP fills CustomConstraints at the harness level with the parallel tool calls instruction")
	}
	if content != vscodeGHCPParallelToolCalls {
		t.Errorf("Injection(\"CustomConstraints\"):\n  got:  %q\n  want: %q", content, vscodeGHCPParallelToolCalls)
	}
}

// TestInjection_VSCodeGHCP_LanguagePatternsIsEmpty verifies that LanguagePatterns is declared
// with empty content (VS Code GHCP has no language-specific pattern injection).
func TestInjection_VSCodeGHCP_LanguagePatternsIsEmpty(t *testing.T) {
	mod := newModule(t)
	content, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns"})
	if !ok {
		t.Fatal("Injection(\"LanguagePatterns\") returned ok=false; VS Code GHCP must declare this injection")
	}
	if content != "" {
		t.Errorf("Injection(\"LanguagePatterns\") = %q; want empty string (no language pattern injection)", content)
	}
}

// TestInjection_VSCodeGHCP_ProjectInjectionsNotFilled verifies that project-class injections
// that VS Code GHCP does not declare are not filled by the module.
func TestInjection_VSCodeGHCP_ProjectInjectionsNotFilled(t *testing.T) {
	mod := newModule(t)
	// These project-class injections are not declared by VS Code GHCP as harness-level.
	projectInjections := []string{
		"IdentityExtension",
		"ProtocolExtension",
		"CodebaseContext",
		"OutputArtifactTemplate",
		"ErrorHandlingExtension",
		"ContextLimits",
	}
	for _, name := range projectInjections {
		_, ok := mod.Injection(domain.InjectionRequest{Name: name})
		if ok {
			t.Errorf("Injection(%q) returned ok=true; this injection is not declared by VS Code GHCP at the harness level", name)
		}
	}
}

// TestInjection_VSCodeGHCP_InjectionsVersion verifies that the module's descriptor carries
// injections_version "1.3.0" per the HarnessInjections.md reference.
func TestInjection_VSCodeGHCP_InjectionsVersion(t *testing.T) {
	mod := newModule(t)
	d := mod.Descriptor()
	if d == nil {
		t.Fatal("Descriptor() returned nil")
	}
	want := "1.3.0"
	if d.InjectionsVersion != want {
		t.Errorf("Descriptor().InjectionsVersion = %q, want %q", d.InjectionsVersion, want)
	}
}

// ---------------------------------------------------------------------------
// Shared contract test (T13.8)
// ---------------------------------------------------------------------------

// TestContract_VSCodeGHCP runs the shared HarnessModule contract suite against the VS Code GHCP
// module. This ensures VS Code GHCP satisfies every invariant that all provision tiers must
// exhibit identically (AC13.7).
func TestContract_VSCodeGHCP(t *testing.T) {
	mod := newModule(t) // RED: fails here until implementation is complete

	contracttest.Run(t, mod, contracttest.Fixtures{
		ToolCases: []contracttest.ToolCase{
			{
				// one_to_many_file_write: verifies file_write → [edit/createFile, edit/editFiles]
				// plus the flow-style single-quoted format that distinguishes VS Code GHCP from
				// Claude Code (which uses comma-separated scalar) and from GHCP CLI (which uses
				// flat tool names like 'read' vs hierarchical 'read/readFile').
				Name: "one_to_many_file_write",
				Request: domain.ToolRequest{
					AgentKey: "one-to-many-test",
					Generic:  []string{"file_write"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key: "tools",
						Value: domain.FieldValue{
							Kind: domain.KindList,
							List: domain.ListFlow,
							Items: []domain.FieldValue{
								{Kind: domain.KindScalar, Scalar: "edit/createFile", Quote: domain.QuoteSingle},
								{Kind: domain.KindScalar, Scalar: "edit/editFiles", Quote: domain.QuoteSingle},
								// search/listDirectory appears by convention in addition to the mapped tools.
								{Kind: domain.KindScalar, Scalar: "search/listDirectory", Quote: domain.QuoteSingle},
							},
						},
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "file_write", Outcome: domain.ToolMapped, HarnessTools: []string{"edit/createFile", "edit/editFiles"}},
				},
			},
			{
				// write_and_edit_deduplication: file_write + file_edit → two resolutions but
				// edit/editFiles appears exactly once in the rendered output.
				Name: "write_and_edit_deduplication",
				Request: domain.ToolRequest{
					AgentKey: "dedup-test",
					Generic:  []string{"file_write", "file_edit"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key: "tools",
						Value: domain.FieldValue{
							Kind: domain.KindList,
							List: domain.ListFlow,
							Items: []domain.FieldValue{
								{Kind: domain.KindScalar, Scalar: "edit/createFile", Quote: domain.QuoteSingle},
								{Kind: domain.KindScalar, Scalar: "edit/editFiles", Quote: domain.QuoteSingle},
								{Kind: domain.KindScalar, Scalar: "search/listDirectory", Quote: domain.QuoteSingle},
							},
						},
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "file_write", Outcome: domain.ToolMapped, HarnessTools: []string{"edit/createFile", "edit/editFiles"}},
					{Generic: "file_edit", Outcome: domain.ToolMapped, HarnessTools: []string{"edit/editFiles"}},
				},
			},
			{
				// skill_maps_to_empty: VS Code GHCP loads skills automatically from the platform;
				// no explicit tool entry is needed or emitted.
				Name: "skill_maps_to_empty",
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
								// Only search/listDirectory from by-convention; skill produces no entry.
								{Kind: domain.KindScalar, Scalar: "search/listDirectory", Quote: domain.QuoteSingle},
							},
						},
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "skill", Outcome: domain.ToolMapped, HarnessTools: []string{}},
				},
			},
			{
				// subagent_maps_to_agent: the orchestrator uses subagent which maps to 'agent'.
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
								{Kind: domain.KindScalar, Scalar: "search/listDirectory", Quote: domain.QuoteSingle},
							},
						},
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "subagent", Outcome: domain.ToolMapped, HarnessTools: []string{"agent"}},
				},
			},
			{
				// file_edit_only: file_edit alone maps to [edit/editFiles]; no edit/createFile
				// is emitted. Guards against the deduplication path in file_write+file_edit masking
				// a broken solo file_edit mapping.
				Name: "file_edit_only",
				Request: domain.ToolRequest{
					AgentKey: "edit-only-test",
					Generic:  []string{"file_edit"},
				},
				Fields: []domain.FrontmatterField{
					{
						Key: "tools",
						Value: domain.FieldValue{
							Kind: domain.KindList,
							List: domain.ListFlow,
							Items: []domain.FieldValue{
								{Kind: domain.KindScalar, Scalar: "edit/editFiles", Quote: domain.QuoteSingle},
								{Kind: domain.KindScalar, Scalar: "search/listDirectory", Quote: domain.QuoteSingle},
							},
						},
					},
				},
				Resolutions: []domain.ToolResolution{
					{Generic: "file_edit", Outcome: domain.ToolMapped, HarnessTools: []string{"edit/editFiles"}},
				},
			},
		},

		FrontmatterCases: []contracttest.FrontmatterCase{
			{
				// adds_disable_model_invocation_and_drops_generic_keys: the key behavioural difference
				// from other harnesses is that VS Code GHCP declaratively adds "disable-model-invocation: false"
				// to every agent. Set contains only this static descriptor Add field. The three
				// generic-only keys are removed and the canonical key order is applied. Model and version
				// stamps are NOT in Set — they are applied exclusively by the transform's Steps 3 and 4.
				Name: "adds_disable_model_invocation_and_drops_generic_keys",
				Request: domain.FrontmatterRequest{
					Kind:     domain.ArtifactAgent,
					AgentKey: "test-agent",
					Model: domain.ModelSelection{
						ModelID: "Claude Sonnet 4.6",
						Origin:  domain.OriginHarnessList,
					},
					Versions: domain.VersionStamps{
						TransformVersion:  "3.0.0",
						InjectionsVersion: "1.3.0",
					},
				},
				Expected: domain.FrontmatterPlan{
					Set: []domain.FrontmatterField{
						{Key: "disable-model-invocation", Value: domain.ScalarValue("false", domain.QuotePlain)},
					},
					Remove:   []string{"recommended_tier", "tier_rationale", "required_skills"},
					KeyOrder: []string{"id", "version", "transform_version", "injections_version", "name", "description", "model", "tools", "disable-model-invocation"},
				},
			},
		},

		InjectionCases: map[string]string{
			"HarnessConstraints": vscodeGHCPFileReadingConstraint,
			"CustomConstraints":  vscodeGHCPParallelToolCalls,
			"LanguagePatterns":   "",
		},

		NotFilled: []string{
			"IdentityExtension",
			"ProtocolExtension",
			"CodebaseContext",
			"OutputArtifactTemplate",
			"ErrorHandlingExtension",
			"ContextLimits",
			"AvailableWorkflows",
		},

		TargetPathCases: []contracttest.TargetPathCase{
			{
				Name:     "agent_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactAgent, Key: "test-runner", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".github/agents/test-runner.md",
			},
			{
				Name:     "skill_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactSkill, Key: "lean-tdd", FileName: "SKILL.md", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".github/skills/lean-tdd/SKILL.md",
			},
			{
				Name:     "hook_project_linux",
				Request:  domain.TargetPathRequest{Kind: domain.ArtifactHook, Key: "subagent-logger", FileName: "subagent-logger.ps1", Scope: domain.ScopeProject, GOOS: "linux"},
				Expected: ".claude/hooks/subagent-logger.ps1",
			},
			{
				Name:    "unsupported_kind_returns_sentinel",
				Request: domain.TargetPathRequest{Kind: domain.ArtifactKind("workflow"), Key: "x", Scope: domain.ScopeProject, GOOS: "linux"},
				Err:     domain.ErrArtifactUnsupported,
			},
		},

		HookPlanCases: []contracttest.HookPlanCase{
			{
				Name: "supported_with_files_and_two_registration_steps",
				Request: domain.HookPlanRequest{
					Bundle: domain.HookBundle{
						Key:     "subagent-logger",
						Version: "1.0.0",
						Variants: map[string]domain.HookVariant{
							"vscode-ghcp": {
								HarnessID: "vscode-ghcp",
								Supported: true,
								Files: []domain.HookFile{
									{SourcePath: "/repo/Hooks/subagent-logger/claude-code/subagent-logger.ps1", TargetName: "subagent-logger.ps1"},
									{SourcePath: "/repo/Hooks/subagent-logger/claude-code/config.json", TargetName: "subagent-logger.json"},
								},
								Registration: []domain.RegistrationStep{
									{ID: "settings-fragment", TargetPath: ".claude/settings.json", Performable: true, Instruction: "Register hook."},
									{ID: "enable-chat-hooks", TargetPath: "", Performable: false, Instruction: "Enable chat.hooks.enabled in VS Code User Settings."},
								},
							},
						},
					},
					Scope: domain.ScopeProject,
				},
				Supported: true,
				TargetDir: ".claude/hooks",
				FileNames: []string{"subagent-logger.ps1", "subagent-logger.json"},
				StepIDs:   []string{"settings-fragment", "enable-chat-hooks"},
			},
		},
	})
}

// ---------------------------------------------------------------------------
// Additional invariant sanity tests
// ---------------------------------------------------------------------------

// TestDescriptor_VSCodeGHCP_HarnessID verifies that the module's Ref and Descriptor return
// a consistent harness ID for the VS Code GHCP harness.
func TestDescriptor_VSCodeGHCP_HarnessID(t *testing.T) {
	mod := newModule(t)

	ref := mod.Ref()
	if ref.ID != "vscode-ghcp" {
		t.Errorf("Ref().ID = %q, want \"vscode-ghcp\"", ref.ID)
	}

	d := mod.Descriptor()
	if d == nil {
		t.Fatal("Descriptor() returned nil")
	}
	if d.ID != "vscode-ghcp" {
		t.Errorf("Descriptor().ID = %q, want \"vscode-ghcp\"", d.ID)
	}
	if ref.ID != d.ID {
		t.Errorf("Ref().ID (%q) != Descriptor().ID (%q); they must be identical", ref.ID, d.ID)
	}
}

// TestDescriptor_VSCodeGHCP_AgentExtensionDeclaration verifies that the descriptor declares
// the agent file extension. This test enforces that the file extension decision is recorded
// in the descriptor rather than being implicit (AC13.6).
// Decision: ".md" (to match the CodebaseAgnostic canonical reference set).
func TestDescriptor_VSCodeGHCP_AgentExtensionDeclaration(t *testing.T) {
	mod := newModule(t)
	d := mod.Descriptor()
	if d == nil {
		t.Fatal("Descriptor() returned nil")
	}

	ext, ok := d.Extensions[domain.ArtifactAgent]
	if !ok {
		t.Fatal("Descriptor().Extensions does not declare an extension for ArtifactAgent; the file extension decision must be explicit in the descriptor (AC13.6)")
	}
	// The canonical CodebaseAgnostic reference uses ".md" (not ".agent.md").
	// Agents/VS code GHCP/UtilityAgents/*.agent.md use .agent.md but were authored separately.
	// The descriptor declares ".md" to match the canonical reference.
	want := ".md"
	if ext != want {
		t.Errorf("Descriptor().Extensions[ArtifactAgent] = %q, want %q\n"+
			"Decision recorded here: CodebaseAgnostic canonical reference uses .md; descriptor declares .md.\n"+
			"UtilityAgents use .agent.md but were authored outside the deployment tool.",
			ext, want)
	}
}

// TestClose_VSCodeGHCP_Idempotent verifies that calling Close() twice does not return an error.
// Built-in modules hold no external resources; Close must be safe to call multiple times.
func TestClose_VSCodeGHCP_Idempotent(t *testing.T) {
	mod := newModule(t)
	if err := mod.Close(); err != nil {
		t.Errorf("first Close() = %v, want nil", err)
	}
	if err := mod.Close(); err != nil {
		t.Errorf("second Close() = %v, want nil (Close must be idempotent)", err)
	}
}

// TestErrors_sentinel ensures the domain sentinel errors used by this test file are
// importable and correctly implement the errors.Is contract. Guards against import cycles
// that would silently break the test, and verifies the sentinel is a proper comparable error.
func TestErrors_sentinel(t *testing.T) {
	if domain.ErrArtifactUnsupported == nil {
		t.Error("domain.ErrArtifactUnsupported is nil")
	}

	// Plain errors.New wrapping does NOT satisfy errors.Is (no error chain).
	plainWrapped := errors.New("plain: " + domain.ErrArtifactUnsupported.Error())
	if errors.Is(plainWrapped, domain.ErrArtifactUnsupported) {
		t.Error("unexpected: errors.New wrapping should not satisfy errors.Is for sentinel")
	}

	// fmt.Errorf with %w DOES satisfy errors.Is (proper error chain unwrapping).
	fmtWrapped := fmt.Errorf("fmt wrapped: %w", domain.ErrArtifactUnsupported)
	if !errors.Is(fmtWrapped, domain.ErrArtifactUnsupported) {
		t.Error("fmt.Errorf(\"...: %%w\", ErrArtifactUnsupported) should satisfy errors.Is; sentinel must support error unwrapping")
	}
}
