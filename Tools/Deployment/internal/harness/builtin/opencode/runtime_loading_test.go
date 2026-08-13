package opencode_test

// runtime_loading_test.go: Tests for the run-time harness content loading path for the
// OpenCode built-in harness module.
//
// These tests provide per-package RED-phase coverage for opencode, mirroring the pattern
// established in claudecode/runtime_loading_test.go. They verify that once I6.4 changes
// opencode.New to accept registry.BuiltinOptions, the module reads content from
// <MosaicRoot>/Agents/OpenCode/ at construction time rather than from embedded bytes.
//
// Test coverage:
//
//   RepoContentDir constant:
//   - opencode.RepoContentDir equals "Catalog/Agents/OpenCode".
//
//   Run-time loading from declared directory (T6.2):
//   - An opencode module constructed against a temporary root reads HarnessInjections.md
//     and HarnessInjectionsOrchestrator.md from <MosaicRoot>/Agents/OpenCode/.
//   - The injected content comes from those on-disk files, not from embedded bytes.
//
//   Failure modes (T6.3):
//   - A missing HarnessInjections.md causes module construction to return a non-nil error.
//   - A missing HarnessInjectionsOrchestrator.md causes module construction to return a non-nil error.
//   - An unparseable HarnessInjections.md causes module construction to return a non-nil error.
//   - An unparseable HarnessInjectionsOrchestrator.md causes module construction to return a non-nil error.
//   - Construction errors name the missing or unparseable file.
//
//   No-rebuild property (T6.4):
//   - Constructing a module, modifying a content file, and constructing again produces different content.
//
//   Orchestrator/subagent content merging preserved (T6.5):
//   - Shared-only: subagent receives it.
//   - Orchestrator-only: subagent receives nothing (ok=false).
//   - Both non-empty: orchestrator receives sharedContent + "\n\n" + orchContent.
//   - Undeclared in both: ok=false.
//
// RED STATE:
//   All tests that call opencode.NewWithOptsForTesting fail at construction time because
//   New(registry.BuiltinOptions) is not yet implemented. See I6.2, I6.3, I6.4.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/opencode"
	"mosaic-deploy/internal/harness/registry"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// writeOpenCodeContentFile writes src to dir/filename, failing the test on any error.
func writeOpenCodeContentFile(t *testing.T, dir, filename, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(src), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
}

// makeTempRootForOpenCode creates a MosaicRoot temp directory and the OpenCode content
// directory <root>/Agents/OpenCode/. Returns the root path and the content directory path.
func makeTempRootForOpenCode(t *testing.T) (root string, contentDir string) {
	t.Helper()
	root = t.TempDir()
	contentDir = filepath.Join(root, opencode.RepoContentDir)
	if err := os.MkdirAll(contentDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", contentDir, err)
	}
	return root, contentDir
}

// newOpenCodeModuleFromOpts constructs an opencode module against a temporary root and fails
// the test if construction fails.
//
// RED: Fails via NewWithOptsForTesting returning an error until I6.4 implements New(opts).
func newOpenCodeModuleFromOpts(t *testing.T, root string) domain.HarnessModule {
	t.Helper()
	mod, err := opencode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err != nil {
		t.Fatalf("opencode.New(opts): %v", err) // RED: fails until I6.4 implemented
	}
	return mod
}

// ---------------------------------------------------------------------------
// Shared fixtures for content files
// ---------------------------------------------------------------------------

const openCodeSharedInjections = `---
version: "1.0.0"
---
[[DEPLOYED:LanguagePatterns]]
Use idiomatic Go.
[[/DEPLOYED:LanguagePatterns]]

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
`

const openCodeOrchestratorInjections = `---
version: "1.0.0"
---
[[DEPLOYED:HarnessConstraints]]
Orchestrator-specific constraint content.
[[/DEPLOYED:HarnessConstraints]]
`

// ---------------------------------------------------------------------------
// RepoContentDir constant
// ---------------------------------------------------------------------------

// TestRepoContentDir_OpenCode_IsCorrect verifies that opencode.RepoContentDir is the correct
// repository directory relative to the MOSAIC root.
func TestRepoContentDir_OpenCode_IsCorrect(t *testing.T) {
	const want = "Catalog/Agents/OpenCode"
	if opencode.RepoContentDir != want {
		t.Errorf("opencode.RepoContentDir = %q, want %q", opencode.RepoContentDir, want)
	}
}

// ---------------------------------------------------------------------------
// Run-time loading from declared directory
// ---------------------------------------------------------------------------

// TestOpenCode_ReadsDeclaredDirectory verifies that constructing an opencode module with a
// MosaicRoot reads content from <MosaicRoot>/<RepoContentDir>/.
func TestOpenCode_ReadsDeclaredDirectory(t *testing.T) {
	root, contentDir := makeTempRootForOpenCode(t)
	writeOpenCodeContentFile(t, contentDir, "HarnessInjections.md", openCodeSharedInjections)
	writeOpenCodeContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md", openCodeOrchestratorInjections)

	mod := newOpenCodeModuleFromOpts(t, root)

	content, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if !ok {
		t.Fatal("Injection(LanguagePatterns) returned ok=false; should be declared in HarnessInjections.md")
	}
	if !strings.Contains(content, "Use idiomatic Go.") {
		t.Errorf("LanguagePatterns content does not match on-disk file; got: %q", content)
	}
}

// TestOpenCode_ContentFromDiskNotFromEmbed verifies that the module reads content from the
// disk path, not from any embedded bytes.
func TestOpenCode_ContentFromDiskNotFromEmbed(t *testing.T) {
	uniqueContent := "OPENCODE_RUNTIME_LOADING_UNIQUE_SENTINEL_XYZ_9847612"
	root, contentDir := makeTempRootForOpenCode(t)
	writeOpenCodeContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:LanguagePatterns]]\n"+uniqueContent+"\n[[/DEPLOYED:LanguagePatterns]]\n\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeOpenCodeContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newOpenCodeModuleFromOpts(t, root)

	content, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if !ok {
		t.Fatal("Injection(LanguagePatterns) returned ok=false")
	}
	if !strings.Contains(content, uniqueContent) {
		t.Errorf("module did not return on-disk content; sentinel %q not found in: %q", uniqueContent, content)
	}
}

// ---------------------------------------------------------------------------
// Failure modes
// ---------------------------------------------------------------------------

// TestOpenCode_MissingSharedFile_ConstructionFails verifies that constructing an opencode
// module when HarnessInjections.md is absent returns a non-nil error.
func TestOpenCode_MissingSharedFile_ConstructionFails(t *testing.T) {
	root, contentDir := makeTempRootForOpenCode(t)
	// Only write orchestrator file; shared file is missing.
	writeOpenCodeContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md", openCodeOrchestratorInjections)

	_, err := opencode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err == nil {
		t.Error("expected non-nil error when HarnessInjections.md is missing, got nil")
	}
}

// TestOpenCode_MissingSharedFile_ErrorNamesFile verifies that the construction error names
// the missing HarnessInjections.md so the user can diagnose the problem.
func TestOpenCode_MissingSharedFile_ErrorNamesFile(t *testing.T) {
	root, contentDir := makeTempRootForOpenCode(t)
	writeOpenCodeContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md", openCodeOrchestratorInjections)

	_, err := opencode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err == nil {
		t.Fatal("expected non-nil error when HarnessInjections.md is missing")
	}
	if !strings.Contains(err.Error(), "HarnessInjections") {
		t.Errorf("error does not mention the missing file name; got: %q", err.Error())
	}
}

// TestOpenCode_MissingOrchestratorFile_ConstructionFails verifies that constructing an
// opencode module when HarnessInjectionsOrchestrator.md is absent returns a non-nil error.
func TestOpenCode_MissingOrchestratorFile_ConstructionFails(t *testing.T) {
	root, contentDir := makeTempRootForOpenCode(t)
	writeOpenCodeContentFile(t, contentDir, "HarnessInjections.md", openCodeSharedInjections)
	// Orchestrator file is missing.

	_, err := opencode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err == nil {
		t.Error("expected non-nil error when HarnessInjectionsOrchestrator.md is missing, got nil")
	}
}

// TestOpenCode_MissingOrchestratorFile_ErrorNamesFile verifies that the construction error
// names the missing HarnessInjectionsOrchestrator.md file.
func TestOpenCode_MissingOrchestratorFile_ErrorNamesFile(t *testing.T) {
	root, contentDir := makeTempRootForOpenCode(t)
	writeOpenCodeContentFile(t, contentDir, "HarnessInjections.md", openCodeSharedInjections)

	_, err := opencode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err == nil {
		t.Fatal("expected non-nil error when HarnessInjectionsOrchestrator.md is missing")
	}
	if !strings.Contains(err.Error(), "HarnessInjectionsOrchestrator") {
		t.Errorf("error does not mention the missing file name; got: %q", err.Error())
	}
}

// TestOpenCode_UnparseableSharedFile_ConstructionFails verifies that an unparseable
// HarnessInjections.md causes module construction to fail.
func TestOpenCode_UnparseableSharedFile_ConstructionFails(t *testing.T) {
	root, contentDir := makeTempRootForOpenCode(t)
	writeOpenCodeContentFile(t, contentDir, "HarnessInjections.md", "---\nversion: \"1.0\"\nunclosed-frontmatter\n")
	writeOpenCodeContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md", openCodeOrchestratorInjections)

	_, err := opencode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err == nil {
		t.Error("expected non-nil error for unparseable HarnessInjections.md, got nil")
	}
}

// TestOpenCode_UnparseableOrchestratorFile_ConstructionFails verifies that an unparseable
// HarnessInjectionsOrchestrator.md causes module construction to fail.
func TestOpenCode_UnparseableOrchestratorFile_ConstructionFails(t *testing.T) {
	root, contentDir := makeTempRootForOpenCode(t)
	writeOpenCodeContentFile(t, contentDir, "HarnessInjections.md", openCodeSharedInjections)
	writeOpenCodeContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md", "---\nversion: \"1.0\"\nunclosed-frontmatter\n")

	_, err := opencode.NewWithOptsForTesting(t, registry.BuiltinOptions{MosaicRoot: root})
	if err == nil {
		t.Error("expected non-nil error for unparseable HarnessInjectionsOrchestrator.md, got nil")
	}
}

// ---------------------------------------------------------------------------
// No-rebuild property
// ---------------------------------------------------------------------------

// TestOpenCode_NoRebuild_ChangingSharedFileChangesInjectedContent verifies the no-rebuild
// property: editing HarnessInjections.md and constructing the module again changes the
// injected content without recompiling the binary.
func TestOpenCode_NoRebuild_ChangingSharedFileChangesInjectedContent(t *testing.T) {
	root, contentDir := makeTempRootForOpenCode(t)

	const firstContent = "OPENCODE_FIRST_VERSION_LANGUAGE_PATTERN_SENTINEL"
	writeOpenCodeContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:LanguagePatterns]]\n"+firstContent+"\n[[/DEPLOYED:LanguagePatterns]]\n\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeOpenCodeContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod1 := newOpenCodeModuleFromOpts(t, root)
	content1, ok1 := mod1.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if !ok1 {
		t.Fatal("module1: Injection(LanguagePatterns) returned ok=false")
	}
	if !strings.Contains(content1, firstContent) {
		t.Fatalf("module1: expected sentinel %q in content; got: %q", firstContent, content1)
	}

	// Edit the file to simulate an in-place update without rebuild.
	const secondContent = "OPENCODE_SECOND_VERSION_LANGUAGE_PATTERN_SENTINEL"
	writeOpenCodeContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:LanguagePatterns]]\n"+secondContent+"\n[[/DEPLOYED:LanguagePatterns]]\n\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	// Construct a new module from the same root — no rebuild needed.
	mod2 := newOpenCodeModuleFromOpts(t, root)
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

// ---------------------------------------------------------------------------
// Orchestrator/subagent content merging preserved
// ---------------------------------------------------------------------------

// TestOpenCode_Merging_SharedOnly_SubagentReceivesSharedContent verifies that when only
// shared content is declared, the subagent receives it.
func TestOpenCode_Merging_SharedOnly_SubagentReceivesSharedContent(t *testing.T) {
	root, contentDir := makeTempRootForOpenCode(t)
	writeOpenCodeContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:LanguagePatterns]]\nShared language patterns.\n[[/DEPLOYED:LanguagePatterns]]\n\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeOpenCodeContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newOpenCodeModuleFromOpts(t, root)

	c, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if !ok {
		t.Fatal("subagent: Injection(LanguagePatterns) returned ok=false; must be declared")
	}
	if !strings.Contains(c, "Shared language patterns.") {
		t.Errorf("subagent LanguagePatterns content incorrect; got: %q", c)
	}
}

// TestOpenCode_Merging_OrchestratorOnly_SubagentReceivesNothing verifies that
// orchestrator-only content does not reach the subagent (ok=false).
func TestOpenCode_Merging_OrchestratorOnly_SubagentReceivesNothing(t *testing.T) {
	root, contentDir := makeTempRootForOpenCode(t)
	writeOpenCodeContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeOpenCodeContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\nOrch constraint.\n[[/DEPLOYED:HarnessConstraints]]\n\n[[DEPLOYED:LanguagePatterns]]\nOrchestrator-only patterns.\n[[/DEPLOYED:LanguagePatterns]]\n")

	mod := newOpenCodeModuleFromOpts(t, root)

	_, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if ok {
		t.Error("subagent must not receive orchestrator-only content (LanguagePatterns absent from shared file)")
	}
}

// TestOpenCode_Merging_BothNonEmpty_OrchestratorReceivesMergedWithSeparator verifies that
// when both shared and orchestrator-only content are non-empty, the orchestrator receives
// sharedContent + "\n\n" + orchContent.
func TestOpenCode_Merging_BothNonEmpty_OrchestratorReceivesMergedWithSeparator(t *testing.T) {
	root, contentDir := makeTempRootForOpenCode(t)
	const sharedPart = "Shared constraint line."
	const orchPart = "Orchestrator-only constraint line."
	writeOpenCodeContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:HarnessConstraints]]\n"+sharedPart+"\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeOpenCodeContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n"+orchPart+"\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newOpenCodeModuleFromOpts(t, root)

	c, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "orchestrator"})
	if !ok {
		t.Fatal("orchestrator: Injection(HarnessConstraints) returned ok=false")
	}
	want := sharedPart + "\n\n" + orchPart
	if c != want {
		t.Errorf("orchestrator HarnessConstraints merge format incorrect\ngot:  %q\nwant: %q", c, want)
	}
}

// TestOpenCode_Merging_UndeclaredInBoth_OkFalse verifies that a name not declared in either
// file returns ok=false for both subagent and orchestrator.
func TestOpenCode_Merging_UndeclaredInBoth_OkFalse(t *testing.T) {
	root, contentDir := makeTempRootForOpenCode(t)
	writeOpenCodeContentFile(t, contentDir, "HarnessInjections.md",
		"[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")
	writeOpenCodeContentFile(t, contentDir, "HarnessInjectionsOrchestrator.md",
		"---\nversion: \"1.0.0\"\n---\n[[DEPLOYED:HarnessConstraints]]\n[[/DEPLOYED:HarnessConstraints]]\n")

	mod := newOpenCodeModuleFromOpts(t, root)

	_, okSubagent := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "subagent"})
	if okSubagent {
		t.Error("subagent: Injection(LanguagePatterns) returned ok=true; name is undeclared in both files, must return ok=false")
	}
	_, okOrch := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "orchestrator"})
	if okOrch {
		t.Error("orchestrator: Injection(LanguagePatterns) returned ok=true; name is undeclared in both files, must return ok=false")
	}
}
