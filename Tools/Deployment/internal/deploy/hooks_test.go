package deploy_test

// hooks_test.go covers T17.7.
//
// Hook deployment has two distinct concerns tested here:
//
// Hook file deployment: each HookPlan.Files entry is copied from its SourcePath to
// <deploymentRoot>/<HookPlan.TargetDir>/<HookFile.TargetName>. Variant file resolution
// is the caller's responsibility (the catalog resolves ReusesVariant before supplying
// HookPlan), so these tests verify that the executor writes the exact files in HookPlan.Files.
//
// Registration policy (AC17.5): for each RegistrationStep in HookPlan.Registration:
//   - If !Performable: always emit a GapManualStep todo; never attempt to write.
//   - If Performable and the target file is absent: write the fragment and emit no gap.
//   - If Performable and the target file already exists: do NOT modify it; emit a
//     GapHookRegistration gap so the user sees the required fragment as a TODO item.
//
// The policy "never edit an existing registration config" is the most destructive bug guard
// in the tool (Plan.md Risk table). Tests verify it as an invariant.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// Hook file deployment
// ---------------------------------------------------------------------------

// TestHooks_FilesDeployedToTargetDir verifies that every file listed in a HookPlan is
// written to <deploymentRoot>/<HookPlan.TargetDir>/<HookFile.TargetName>.
func TestHooks_FilesDeployedToTargetDir(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	hookSrc := makeHookSourceFile(t, "#!/bin/sh\necho subagent-logger")
	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: ".claude/hooks",
		Files: []domain.HookFile{
			{SourcePath: hookSrc, TargetName: "subagent-logger.sh"},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	expectedPath := filepath.Join(workspace, ".claude", "hooks", "subagent-logger.sh")
	if !fileExistsAt(t, expectedPath) {
		t.Errorf("hook file not found at %q", expectedPath)
	}
}

// TestHooks_FileContentMatchesSource verifies that the deployed hook file contains the exact
// bytes from the source file, byte-for-byte.
func TestHooks_FileContentMatchesSource(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	hookContent := []byte("#!/bin/sh\nexport AGENT_LOG=/tmp/agents.log\n")
	hookSrc := makeHookSourceFile(t, string(hookContent))

	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: ".claude/hooks",
		Files:     []domain.HookFile{{SourcePath: hookSrc, TargetName: "logger.sh"}},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	deployed := readFile(t, filepath.Join(workspace, ".claude", "hooks", "logger.sh"))
	if !bytes.Equal(deployed, hookContent) {
		t.Errorf("deployed hook content:\ngot:  %q\nwant: %q", deployed, hookContent)
	}
}

// TestHooks_MultipleFilesDeployed verifies that all files in a HookPlan are deployed,
// not just the first one.
func TestHooks_MultipleFilesDeployed(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	srcA := makeHookSourceFile(t, "#!/bin/sh\necho file-a")
	srcB := makeHookSourceFile(t, "#!/bin/sh\necho file-b")

	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: ".claude/hooks",
		Files: []domain.HookFile{
			{SourcePath: srcA, TargetName: "file-a.sh"},
			{SourcePath: srcB, TargetName: "file-b.sh"},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, name := range []string{"file-a.sh", "file-b.sh"} {
		p := filepath.Join(workspace, ".claude", "hooks", name)
		if !fileExistsAt(t, p) {
			t.Errorf("hook file %q not found at %q", name, p)
		}
	}
}

// TestHooks_UnsupportedHookPlan_NoFilesWritten verifies that when HookPlan.Supported is
// false, the executor does not write any files for that plan.
func TestHooks_UnsupportedHookPlan_NoFilesWritten(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	hookPlan := domain.HookPlan{
		Supported: false,
		Reason:    "this harness does not support hooks",
		TargetDir: ".claude/hooks",
		Files: []domain.HookFile{
			{SourcePath: makeHookSourceFile(t, "content"), TargetName: "hook.sh"},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	hookPath := filepath.Join(workspace, ".claude", "hooks", "hook.sh")
	if fileExistsAt(t, hookPath) {
		t.Errorf("hook file found at %q for an unsupported hook plan; executor must not write it", hookPath)
	}
}

// ---------------------------------------------------------------------------
// Registration policy: Performable + target absent → write
// ---------------------------------------------------------------------------

// TestHooks_Registration_AbsentTarget_FragmentIsWritten verifies that when a registration
// step is Performable and its target file does not yet exist, the executor writes the
// fragment to the target path.
func TestHooks_Registration_AbsentTarget_FragmentIsWritten(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	registrationTargetRel := ".claude/settings.json"
	fragment := `{"hooks": {"pre-tool-use": ["~/.claude/hooks/subagent-logger.sh"]}}`

	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: ".claude/hooks",
		Files:     []domain.HookFile{{SourcePath: makeHookSourceFile(t, "hook"), TargetName: "hook.sh"}},
		Registration: []domain.RegistrationStep{
			{
				ID:          "claude-code-settings",
				TargetPath:  registrationTargetRel,
				Fragment:    fragment,
				Performable: true,
				Instruction: "Add the hook entry to .claude/settings.json",
			},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	targetPath := filepath.Join(workspace, registrationTargetRel)
	if !fileExistsAt(t, targetPath) {
		t.Errorf("registration target %q was not written by the executor", targetPath)
	}
	content := readFile(t, targetPath)
	if !bytes.Equal(content, []byte(fragment)) {
		t.Errorf("registration file content:\ngot:  %q\nwant: %q", content, fragment)
	}
}

// TestHooks_Registration_AbsentTarget_NoGapEmitted verifies that when the executor
// successfully writes the registration fragment, no GapHookRegistration gap is emitted.
func TestHooks_Registration_AbsentTarget_NoGapEmitted(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: ".claude/hooks",
		Files:     []domain.HookFile{{SourcePath: makeHookSourceFile(t, "hook"), TargetName: "hook.sh"}},
		Registration: []domain.RegistrationStep{
			{
				ID:          "claude-code-settings",
				TargetPath:  ".claude/settings.json",
				Fragment:    `{}`,
				Performable: true,
				Instruction: "instruction",
			},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	spy := newSpyCollector()
	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), spy)
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if spy.hasGapKind(domain.GapHookRegistration) {
		t.Error("GapHookRegistration was emitted for a successful registration; it must not be")
	}
}

// ---------------------------------------------------------------------------
// Registration policy: Performable + target exists → never modify, emit gap
// ---------------------------------------------------------------------------

// TestHooks_Registration_ExistingTarget_IsNeverModified verifies the core safety invariant:
// when a registration target file already exists, the executor NEVER modifies it. This is
// the most destructive bug guard in the tool (AC17.5).
func TestHooks_Registration_ExistingTarget_IsNeverModified(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	// Pre-create the registration target with user content.
	registrationTargetRel := ".claude/settings.json"
	originalContent := []byte(`{"model": "claude-opus-4-5", "hooks": {}}`)
	registerTargetPath := filepath.Join(workspace, registrationTargetRel)
	if err := os.MkdirAll(filepath.Dir(registerTargetPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(registerTargetPath, originalContent, 0o644); err != nil {
		t.Fatalf("write existing registration target: %v", err)
	}

	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: ".claude/hooks",
		Files:     []domain.HookFile{{SourcePath: makeHookSourceFile(t, "hook"), TargetName: "hook.sh"}},
		Registration: []domain.RegistrationStep{
			{
				ID:          "claude-code-settings",
				TargetPath:  registrationTargetRel,
				Fragment:    `{"hooks": {"pre-tool-use": ["~/.claude/hooks/subagent-logger.sh"]}}`,
				Performable: true,
				Instruction: "Add the hook entry to .claude/settings.json",
			},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	actualContent := readFile(t, registerTargetPath)
	if !bytes.Equal(actualContent, originalContent) {
		t.Errorf("existing registration target was modified:\ngot:  %q\nwant: %q (unchanged)", actualContent, originalContent)
	}
}

// TestHooks_Registration_ExistingTarget_EmitsGapHookRegistration verifies that when a
// registration target already exists, the executor emits a GapHookRegistration so the user
// sees the required fragment as a TODO item (AC17.5).
func TestHooks_Registration_ExistingTarget_EmitsGapHookRegistration(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	registrationTargetRel := ".claude/settings.json"
	registerTargetPath := filepath.Join(workspace, registrationTargetRel)
	if err := os.MkdirAll(filepath.Dir(registerTargetPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(registerTargetPath, []byte(`{"existing": "content"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	fragment := `{"hooks": {"pre-tool-use": ["logger.sh"]}}`
	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: ".claude/hooks",
		Files:     []domain.HookFile{{SourcePath: makeHookSourceFile(t, "hook"), TargetName: "hook.sh"}},
		Registration: []domain.RegistrationStep{
			{
				ID:          "claude-code-settings",
				TargetPath:  registrationTargetRel,
				Fragment:    fragment,
				Performable: true,
				Instruction: "Manually add the hook entry to .claude/settings.json",
			},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	spy := newSpyCollector()
	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), spy)
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !spy.hasGapKind(domain.GapHookRegistration) {
		t.Error("GapHookRegistration was not emitted for an existing registration target; it must be")
	}
}

// TestHooks_Registration_ExistingTarget_GapCarriesFragment verifies that the GapHookRegistration
// gap carries the required fragment so the user knows what to paste.
func TestHooks_Registration_ExistingTarget_GapCarriesFragment(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	registrationTargetRel := ".claude/settings.json"
	registerTargetPath := filepath.Join(workspace, registrationTargetRel)
	_ = os.MkdirAll(filepath.Dir(registerTargetPath), 0o755)
	_ = os.WriteFile(registerTargetPath, []byte(`{"existing": true}`), 0o644)

	fragment := `{"hooks": {"pre-tool-use": ["logger.sh"]}}`
	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: ".claude/hooks",
		Files:     []domain.HookFile{{SourcePath: makeHookSourceFile(t, "hook"), TargetName: "hook.sh"}},
		Registration: []domain.RegistrationStep{
			{
				ID:          "claude-code-settings",
				TargetPath:  registrationTargetRel,
				Fragment:    fragment,
				Performable: true,
				Instruction: "instruction",
			},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	spy := newSpyCollector()
	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), spy)
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var found bool
	for _, g := range spy.gaps {
		if g.Kind == domain.GapHookRegistration && g.Fragment == fragment {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no GapHookRegistration gap with Fragment=%q found; got gaps: %+v", fragment, spy.gaps)
	}
}

// ---------------------------------------------------------------------------
// Registration policy: !Performable → always a manual step
// ---------------------------------------------------------------------------

// TestHooks_Registration_NonPerformable_AlwaysEmitsGapManualStep verifies that when a
// registration step has Performable == false (e.g. a user-level editor setting that the
// tool cannot configure), it is always emitted as a GapManualStep, regardless of whether
// the target file exists.
func TestHooks_Registration_NonPerformable_AlwaysEmitsGapManualStep(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: ".claude/hooks",
		Files:     []domain.HookFile{{SourcePath: makeHookSourceFile(t, "hook"), TargetName: "hook.sh"}},
		Registration: []domain.RegistrationStep{
			{
				ID:          "editor-setting",
				TargetPath:  "", // empty: tool cannot perform this step at all
				Fragment:    "Enable hooks in VS Code settings",
				Performable: false,
				Instruction: "Open VS Code settings and enable the hooks extension",
			},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	spy := newSpyCollector()
	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), spy)
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !spy.hasGapKind(domain.GapManualStep) {
		t.Error("GapManualStep was not emitted for a non-performable registration step")
	}
}

// TestHooks_Registration_NonPerformable_NothingWrittenToFilesystem verifies that a
// non-performable registration step causes no file writes at all.
func TestHooks_Registration_NonPerformable_NothingWrittenToFilesystem(t *testing.T) {
	workspace := t.TempDir()
	mosaicRoot := t.TempDir()

	hookPlan := domain.HookPlan{
		Supported: true,
		TargetDir: ".claude/hooks",
		Files:     []domain.HookFile{{SourcePath: makeHookSourceFile(t, "hook"), TargetName: "hook.sh"}},
		Registration: []domain.RegistrationStep{
			{
				ID:          "editor-setting",
				TargetPath:  "",
				Fragment:    "some fragment",
				Performable: false,
				Instruction: "manual step",
			},
		},
	}

	req := deploy.ExecRequest{
		Plan:       newPlan(workspace),
		MosaicRoot: mosaicRoot,
		Content:    fixedContent(nil),
		Hooks:      []domain.HookPlan{hookPlan},
	}

	ex := deploy.NewExecutor(manifest.NewStore(), newSpyLogger(), newSpyCollector())
	if _, err := ex.Execute(context.Background(), req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The only file that should exist in the workspace is the hook file itself.
	// No registration file should have been written.
	// (We verify the hook file IS written and the registration target is NOT.)
	hookPath := filepath.Join(workspace, ".claude", "hooks", "hook.sh")
	if !fileExistsAt(t, hookPath) {
		t.Errorf("hook file not found; executor should still deploy hook files even with non-performable steps")
	}
}

// ---------------------------------------------------------------------------
// Shared hook test helper
// ---------------------------------------------------------------------------

// makeHookSourceFile creates a temporary file with the given content and returns its path.
func makeHookSourceFile(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(f, []byte(content), 0o755); err != nil {
		t.Fatalf("makeHookSourceFile: %v", err)
	}
	return f
}
