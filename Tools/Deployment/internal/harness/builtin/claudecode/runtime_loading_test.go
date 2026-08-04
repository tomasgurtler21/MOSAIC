package claudecode_test

// runtime_loading_test.go: Tests for the run-time harness content loading path introduced
// in Stage 6. These tests specify that built-in modules read their harness content from the
// repository on disk at construction time, not from embedded bytes.
//
// Test coverage:
//
//   T6.1 — RepoContentDir constant (compile-time check):
//   - claudecode.RepoContentDir equals "Agents/Claude Code".
//   - ghcpcli.RepoContentDir, opencode.RepoContentDir, vscodeghcp.RepoContentDir are each
//     the correct path for their respective harness directories.
//
//   T6.2 — Run-time loading from declared directory:
//   - A claudecode module constructed against a temporary root reads HarnessInjections.md
//     and HarnessInjectionsOrchestrator.md from <MosaicRoot>/Agents/Claude Code/.
//   - The injected content comes from those on-disk files, not from embedded bytes.
//
//   T6.3 — Failure modes:
//   - A missing HarnessInjections.md causes module construction to return a non-nil error.
//   - A missing HarnessInjectionsOrchestrator.md causes module construction to return a non-nil error.
//   - An unparseable HarnessInjections.md causes module construction to return a non-nil error.
//   - The error names the missing or unparseable file.
//
//   T6.4 — No-rebuild property:
//   - Constructing a module against a temp root, modifying a content file, and constructing
//     again produces a module with different injected content — no rebuild required.
//
//   T6.5 — Orchestrator/subagent content merging preserved:
//   - Shared-only content: subagent receives it; orchestrator receives it.
//   - Orchestrator-only content: subagent receives nothing (ok=false); orchestrator receives it.
//   - Both non-empty: orchestrator receives sharedContent + "\n\n" + orchContent.
//   - Shared declared-empty, orch non-empty: orchestrator receives orch content only.
//   - Both declared-empty: subagent ok=true + ""; orchestrator ok=true + "".
//   - Undeclared in both: subagent ok=false; orchestrator ok=false.
//
// RED STATE:
//   All tests that call claudecode.NewWithOptsForTesting fail at construction time because
//   New(registry.BuiltinOptions) is not yet implemented. See I6.2, I6.3, I6.4.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/claudecode"
	"mosaic-deploy/internal/harness/builtin/ghcpcli"
	"mosaic-deploy/internal/harness/builtin/opencode"
	"mosaic-deploy/internal/harness/builtin/vscodeghcp"
	"mosaic-deploy/internal/harness/registry"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// writeHarnessContentFile writes src to dir/filename, failing the test on any error.
func writeHarnessContentFile(t *testing.T, dir, filename, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(src), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
}

// makeTempRoot creates a MosaicRoot temp directory and the Claude Code content directory
// <root>/Agents/Claude Code/. Returns the root path and the content directory path.
func makeTempRoot(t *testing.T) (root string, contentDir string) {
	t.Helper()
	root = t.TempDir()
	contentDir = filepath.Join(root, claudecode.RepoContentDir)
	if err := os.MkdirAll(contentDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", contentDir, err)
	}
	return root, contentDir
}

// newModuleFromOpts constructs a claudecode module against a temporary root and fails the
// test if construction fails.
//
// RED: Fails via NewWithOptsForTesting returning an error until I6.4 implements New(opts).
func newModuleFromOpts(t *testing.T, root string) domain.HarnessModule {
	t.Helper()
	mod, err := claudecode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err != nil {
		t.Fatalf("claudecode.New(opts): %v", err) // RED: fails until I6.4 implemented
	}
	return mod
}

// ---------------------------------------------------------------------------
// Shared fixtures for content files
// ---------------------------------------------------------------------------

// sharedOnlyHarnessInjections is a minimal HarnessInjections.md with [[DEPLOYED:]] markers.
const sharedOnlyHarnessInjections = `---
version: "1.0.0"
---
[[DEPLOYED:LanguagePatterns]]
Use idiomatic Go.
[[/DEPLOYED:LanguagePatterns]]

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
`

// orchestratorHarnessInjectionsOrchestrator is a minimal HarnessInjectionsOrchestrator.md.
const orchestratorHarnessInjectionsOrchestrator = `---
version: "1.0.0"
---
[[DEPLOYED:HarnessConstraints]]
Orchestrator-specific constraint content.
[[/DEPLOYED:HarnessConstraints]]
`

// ---------------------------------------------------------------------------
// T6.1: RepoContentDir constants
// ---------------------------------------------------------------------------

// TestRepoContentDir_ClaudeCode verifies that claudecode.RepoContentDir is the correct
// repository directory relative to the MOSAIC root.
func TestRepoContentDir_ClaudeCode(t *testing.T) {
	const want = "Agents/Claude Code"
	if claudecode.RepoContentDir != want {
		t.Errorf("claudecode.RepoContentDir = %q, want %q", claudecode.RepoContentDir, want)
	}
}

// TestRepoContentDir_GhcpCli verifies that ghcpcli.RepoContentDir is the correct path.
func TestRepoContentDir_GhcpCli(t *testing.T) {
	const want = "Agents/GHCP CLI"
	if ghcpcli.RepoContentDir != want {
		t.Errorf("ghcpcli.RepoContentDir = %q, want %q", ghcpcli.RepoContentDir, want)
	}
}

// TestRepoContentDir_OpenCode verifies that opencode.RepoContentDir is the correct path.
func TestRepoContentDir_OpenCode(t *testing.T) {
	const want = "Agents/OpenCode"
	if opencode.RepoContentDir != want {
		t.Errorf("opencode.RepoContentDir = %q, want %q", opencode.RepoContentDir, want)
	}
}

// TestRepoContentDir_VsCodeGhcp verifies that vscodeghcp.RepoContentDir is the correct path.
func TestRepoContentDir_VsCodeGhcp(t *testing.T) {
	const want = "Agents/VS code GHCP"
	if vscodeghcp.RepoContentDir != want {
		t.Errorf("vscodeghcp.RepoContentDir = %q, want %q", vscodeghcp.RepoContentDir, want)
	}
}

// TestRepoContentDir_AllHarnesses_AreDistinct verifies that no two built-in harnesses share
// a content directory.
func TestRepoContentDir_AllHarnesses_AreDistinct(t *testing.T) {
	dirs := map[string]string{
		"claude-code": claudecode.RepoContentDir,
		"ghcp-cli":    ghcpcli.RepoContentDir,
		"opencode":    opencode.RepoContentDir,
		"vscode-ghcp": vscodeghcp.RepoContentDir,
	}
	seen := make(map[string]string, len(dirs))
	for id, dir := range dirs {
		if existing, conflict := seen[dir]; conflict {
			t.Errorf("harness %q and %q share the same RepoContentDir %q; each must be distinct",
				id, existing, dir)
		}
		seen[dir] = id
	}
}

// ---------------------------------------------------------------------------
// T6.2: Run-time loading from declared directory
// ---------------------------------------------------------------------------

// TestRuntimeLoading_ReadsDeclaredDirectory verifies that constructing a claudecode module
// with a MosaicRoot reads content from <MosaicRoot>/<RepoContentDir>/.
func TestRuntimeLoading_ReadsDeclaredDirectory(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md", sharedOnlyHarnessInjections)
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md", orchestratorHarnessInjectionsOrchestrator)

	mod := newModuleFromOpts(t, root)

	content, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if !ok {
		t.Fatal("Injection(LanguagePatterns) returned ok=false; should be declared in HarnessInjections.md")
	}
	if !strings.Contains(content, "Use idiomatic Go.") {
		t.Errorf("LanguagePatterns content does not match on-disk file; got: %q", content)
	}
}

// TestRuntimeLoading_ContentFromDiskNotFromEmbed verifies that the module reads content from
// the disk path, not from any embedded bytes. It writes unique sentinel content to the temp
// directory that cannot be in any compiled embed.
func TestRuntimeLoading_ContentFromDiskNotFromEmbed(t *testing.T) {
	uniqueContent := "RUNTIME_LOADING_UNIQUE_SENTINEL_XYZ_9847612"
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md", "[[DEPLOYED:LanguagePatterns]]\n"+uniqueContent+"\n[[/DEPLOYED:LanguagePatterns]]\n\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md", "---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newModuleFromOpts(t, root)

	content, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if !ok {
		t.Fatal("Injection(LanguagePatterns) returned ok=false")
	}
	if !strings.Contains(content, uniqueContent) {
		t.Errorf("module did not return on-disk content; sentinel %q not found in: %q", uniqueContent, content)
	}
}

// ---------------------------------------------------------------------------
// T6.3: Failure modes
// ---------------------------------------------------------------------------

// TestFailureMode_MissingSharedFile_ConstructionFails verifies that constructing a claudecode
// module when HarnessInjections.md is absent returns a non-nil error.
func TestFailureMode_MissingSharedFile_ConstructionFails(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	// Only write orchestrator file; shared file is missing.
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md", orchestratorHarnessInjectionsOrchestrator)

	_, err := claudecode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err == nil {
		t.Error("expected non-nil error when HarnessInjections.md is missing, got nil")
	}
}

// TestFailureMode_MissingSharedFile_ErrorNamesFile verifies that the construction error
// names the missing HarnessInjections.md file so the user can diagnose the problem.
func TestFailureMode_MissingSharedFile_ErrorNamesFile(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md", orchestratorHarnessInjectionsOrchestrator)

	_, err := claudecode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err == nil {
		t.Fatal("expected non-nil error when HarnessInjections.md is missing")
	}
	if !strings.Contains(err.Error(), "HarnessInjections") {
		t.Errorf("error does not mention the missing file name; got: %q", err.Error())
	}
}

// TestFailureMode_MissingOrchestratorFile_ConstructionFails verifies that constructing a
// claudecode module when HarnessInjectionsOrchestrator.md is absent returns a non-nil error.
func TestFailureMode_MissingOrchestratorFile_ConstructionFails(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md", sharedOnlyHarnessInjections)
	// Orchestrator file is missing.

	_, err := claudecode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err == nil {
		t.Error("expected non-nil error when HarnessInjectionsOrchestrator.md is missing, got nil")
	}
}

// TestFailureMode_MissingOrchestratorFile_ErrorNamesFile verifies that the construction error
// names the missing HarnessInjectionsOrchestrator.md file.
func TestFailureMode_MissingOrchestratorFile_ErrorNamesFile(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md", sharedOnlyHarnessInjections)

	_, err := claudecode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err == nil {
		t.Fatal("expected non-nil error when HarnessInjectionsOrchestrator.md is missing")
	}
	if !strings.Contains(err.Error(), "HarnessInjectionsOrchestrator") {
		t.Errorf("error does not mention the missing file name; got: %q", err.Error())
	}
}

// TestFailureMode_UnparseableSharedFile_ConstructionFails verifies that an unparseable
// HarnessInjections.md causes module construction to fail.
func TestFailureMode_UnparseableSharedFile_ConstructionFails(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	// Unclosed frontmatter — docformat.Parse returns an error.
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md", "---\nversion: \"1.0\"\nunclosed-frontmatter\n")
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md", orchestratorHarnessInjectionsOrchestrator)

	_, err := claudecode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err == nil {
		t.Error("expected non-nil error for unparseable HarnessInjections.md, got nil")
	}
}

// TestFailureMode_UnparseableOrchestratorFile_ConstructionFails verifies that an unparseable
// HarnessInjectionsOrchestrator.md causes module construction to fail.
func TestFailureMode_UnparseableOrchestratorFile_ConstructionFails(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md", sharedOnlyHarnessInjections)
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md", "---\nversion: \"1.0\"\nunclosed-frontmatter\n")

	_, err := claudecode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err == nil {
		t.Error("expected non-nil error for unparseable HarnessInjectionsOrchestrator.md, got nil")
	}
}

// ---------------------------------------------------------------------------
// T6.4: No-rebuild property
// ---------------------------------------------------------------------------

// TestNoRebuild_ChangingSharedFileChangesInjectedContent verifies the no-rebuild property:
// editing HarnessInjections.md and constructing the module again changes the injected
// content without recompiling the binary.
func TestNoRebuild_ChangingSharedFileChangesInjectedContent(t *testing.T) {
	root, contentDir := makeTempRoot(t)

	const firstContent = "FIRST_VERSION_LANGUAGE_PATTERN_SENTINEL"
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:LanguagePatterns]]\n"+firstContent+"\n[[/DEPLOYED:LanguagePatterns]]\n\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod1 := newModuleFromOpts(t, root)
	content1, ok1 := mod1.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if !ok1 {
		t.Fatal("module1: Injection(LanguagePatterns) returned ok=false")
	}
	if !strings.Contains(content1, firstContent) {
		t.Fatalf("module1: expected sentinel %q in content; got: %q", firstContent, content1)
	}

	// Edit the file to simulate an in-place update without rebuild.
	const secondContent = "SECOND_VERSION_LANGUAGE_PATTERN_SENTINEL"
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:LanguagePatterns]]\n"+secondContent+"\n[[/DEPLOYED:LanguagePatterns]]\n\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	// Construct a new module from the same root — no rebuild needed.
	mod2 := newModuleFromOpts(t, root)
	content2, ok2 := mod2.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if !ok2 {
		t.Fatal("module2: Injection(LanguagePatterns) returned ok=false")
	}
	if !strings.Contains(content2, secondContent) {
		t.Errorf("module2: expected updated sentinel %q after file edit; got: %q", secondContent, content2)
	}
	if content1 == content2 {
		t.Errorf("content unchanged after file edit: both modules returned %q", content1)
	}
}

// TestNoRebuild_ChangingOrchestratorFileChangesInjectedContent verifies that editing
// HarnessInjectionsOrchestrator.md and constructing again changes the orchestrator content.
func TestNoRebuild_ChangingOrchestratorFileChangesInjectedContent(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md", sharedOnlyHarnessInjections)

	const firstOrchContent = "FIRST_ORCH_CONSTRAINT_SENTINEL"
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n"+firstOrchContent+"\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod1 := newModuleFromOpts(t, root)
	c1, _ := mod1.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "orchestrator"})

	const secondOrchContent = "SECOND_ORCH_CONSTRAINT_SENTINEL"
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"2.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n"+secondOrchContent+"\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod2 := newModuleFromOpts(t, root)
	c2, _ := mod2.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "orchestrator"})

	if c1 == c2 {
		t.Errorf("orchestrator HarnessConstraints unchanged after editing orchestrator file: both = %q", c1)
	}
	if !strings.Contains(c2, secondOrchContent) {
		t.Errorf("module2 orchestrator HarnessConstraints does not contain updated content %q; got: %q", secondOrchContent, c2)
	}
}

// ---------------------------------------------------------------------------
// T6.5: Orchestrator/subagent content merging preserved
// ---------------------------------------------------------------------------

// TestMerging_SharedOnly_SubagentReceivesSharedContent verifies that when only shared content
// is declared, the subagent receives it.
func TestMerging_SharedOnly_SubagentReceivesSharedContent(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:LanguagePatterns]]\nShared language patterns.\n[[/DEPLOYED:LanguagePatterns]]\n\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newModuleFromOpts(t, root)

	c, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if !ok {
		t.Fatal("subagent: Injection(LanguagePatterns) returned ok=false; must be declared")
	}
	if !strings.Contains(c, "Shared language patterns.") {
		t.Errorf("subagent LanguagePatterns content incorrect; got: %q", c)
	}
}

// TestMerging_SharedOnly_OrchestratorReceivesSharedContent verifies that when no orchestrator
// content exists for a name, the orchestrator receives the shared content unmodified.
func TestMerging_SharedOnly_OrchestratorReceivesSharedContent(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	// LanguagePatterns is in shared only; orchestrator file has no LanguagePatterns.
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:LanguagePatterns]]\nShared language patterns.\n[[/DEPLOYED:LanguagePatterns]]\n\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newModuleFromOpts(t, root)

	c, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "orchestrator"})
	if !ok {
		t.Fatal("orchestrator: Injection(LanguagePatterns) returned ok=false; shared content must propagate to orchestrator")
	}
	if !strings.Contains(c, "Shared language patterns.") {
		t.Errorf("orchestrator LanguagePatterns should be shared content unmodified; got: %q", c)
	}
}

// TestMerging_OrchestratorOnly_SubagentReceivesNothing verifies that orchestrator-only content
// (declared only in HarnessInjectionsOrchestrator.md) does NOT reach the subagent (ok=false).
func TestMerging_OrchestratorOnly_SubagentReceivesNothing(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	// Shared file has only HarnessConstraints; LanguagePatterns is absent from shared.
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	// Orchestrator file has LanguagePatterns.
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\nOrch constraint.\n[[/DEPLOYED:HarnessConstraints]]\n\n[[DEPLOYED:LanguagePatterns]]\nOrchestrator-only patterns.\n[[/DEPLOYED:LanguagePatterns]]\n")

	mod := newModuleFromOpts(t, root)

	_, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if ok {
		t.Error("subagent must not receive orchestrator-only content (LanguagePatterns absent from shared file)")
	}
}

// TestMerging_OrchestratorOnly_OrchestratorReceivesContent verifies that the orchestrator
// receives content declared only in HarnessInjectionsOrchestrator.md.
func TestMerging_OrchestratorOnly_OrchestratorReceivesContent(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\nOrchestrator-only constraint.\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newModuleFromOpts(t, root)

	c, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "orchestrator"})
	if !ok {
		t.Fatal("orchestrator: Injection(HarnessConstraints) returned ok=false; orchestrator-only content must reach orchestrator")
	}
	if !strings.Contains(c, "Orchestrator-only constraint.") {
		t.Errorf("orchestrator HarnessConstraints missing expected text; got: %q", c)
	}
}

// TestMerging_BothNonEmpty_OrchestratorReceivesMergedWithSeparator verifies that when both
// shared and orchestrator-only content are non-empty, the orchestrator receives
// sharedContent + "\n\n" + orchContent (the canonical merge format).
func TestMerging_BothNonEmpty_OrchestratorReceivesMergedWithSeparator(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	const sharedPart = "Shared constraint line."
	const orchPart = "Orchestrator-only constraint line."
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:HarnessConstraints]]\n"+sharedPart+"\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n"+orchPart+"\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newModuleFromOpts(t, root)

	c, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "orchestrator"})
	if !ok {
		t.Fatal("orchestrator: Injection(HarnessConstraints) returned ok=false")
	}
	want := sharedPart + "\n\n" + orchPart
	if c != want {
		t.Errorf("orchestrator HarnessConstraints merge format incorrect\ngot:  %q\nwant: %q", c, want)
	}
}

// TestMerging_BothDeclaredEmpty_SubagentOkTrueEmptyString verifies that when both shared
// and orchestrator content are declared-but-empty, the subagent receives ok=true + "".
func TestMerging_BothDeclaredEmpty_SubagentOkTrueEmptyString(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newModuleFromOpts(t, root)

	c, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "subagent"})
	if !ok {
		t.Fatal("subagent: Injection(HarnessConstraints) returned ok=false; declared-but-empty must return ok=true")
	}
	if c != "" {
		t.Errorf("subagent HarnessConstraints should be empty string (declared-but-empty); got: %q", c)
	}
}

// TestMerging_BothDeclaredEmpty_OrchestratorOkTrueEmptyString verifies that when both sources
// are declared-but-empty, the orchestrator receives ok=true + "".
func TestMerging_BothDeclaredEmpty_OrchestratorOkTrueEmptyString(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newModuleFromOpts(t, root)

	c, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "orchestrator"})
	if !ok {
		t.Fatal("orchestrator: Injection(HarnessConstraints) returned ok=false; declared-but-empty (both sources) must return ok=true")
	}
	if c != "" {
		t.Errorf("orchestrator HarnessConstraints should be empty string (both declared-but-empty); got: %q", c)
	}
}

// TestMerging_UndeclaredInBoth_SubagentOkFalse verifies that a name not declared in either
// file returns ok=false for the subagent.
func TestMerging_UndeclaredInBoth_SubagentOkFalse(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	// Neither file declares LanguagePatterns.
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newModuleFromOpts(t, root)

	_, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if ok {
		t.Error("subagent: Injection(LanguagePatterns) returned ok=true; name is undeclared in both files, must return ok=false")
	}
}

// TestMerging_UndeclaredInBoth_OrchestratorOkFalse verifies that a name not declared in
// either file returns ok=false for the orchestrator.
func TestMerging_UndeclaredInBoth_OrchestratorOkFalse(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newModuleFromOpts(t, root)

	_, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "orchestrator"})
	if ok {
		t.Error("orchestrator: Injection(LanguagePatterns) returned ok=true; name is undeclared in both files, must return ok=false")
	}
}

// TestMerging_SharedEmptyOrchNonEmpty_OrchestratorReceivesOrchContent verifies merge case:
// shared is declared-but-empty, orchestrator has non-empty content → orchestrator receives
// the orch content (not the empty shared content, not a merged pair).
func TestMerging_SharedEmptyOrchNonEmpty_OrchestratorReceivesOrchContent(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\nOrchestrator-specific content when shared is empty.\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newModuleFromOpts(t, root)

	c, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "orchestrator"})
	if !ok {
		t.Fatal("orchestrator: Injection(HarnessConstraints) returned ok=false")
	}
	if !strings.Contains(c, "Orchestrator-specific content when shared is empty.") {
		t.Errorf("orchestrator HarnessConstraints should be orch content (shared is empty); got: %q", c)
	}
}

// TestMerging_Deterministic verifies that two calls with equal InjectionRequest inputs
// return equal outputs (no randomness in the on-disk content source).
func TestMerging_Deterministic(t *testing.T) {
	root, contentDir := makeTempRoot(t)
	writeHarnessContentFile(t, contentDir, "HarnessInjections.md", sharedOnlyHarnessInjections)
	writeHarnessContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md", orchestratorHarnessInjectionsOrchestrator)

	mod := newModuleFromOpts(t, root)

	names := []string{"LanguagePatterns", "HarnessConstraints", "NotDeclared"}
	keys := []string{"subagent", "orchestrator", ""}
	for _, name := range names {
		for _, key := range keys {
			req := domain.InjectionRequest{Name: name, AgentKey: key}
			c1, ok1 := mod.Injection(req)
			c2, ok2 := mod.Injection(req)
			if ok1 != ok2 || c1 != c2 {
				t.Errorf("Injection(%q, %q) non-deterministic: first=(%q,%v), second=(%q,%v)",
					name, key, c1, ok1, c2, ok2)
			}
		}
	}
}
