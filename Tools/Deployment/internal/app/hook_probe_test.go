package app

// hook_probe_test.go covers the layout-aware hook presence probe (probeHookPlanPresence), the
// path-keyed HookPlan index (hookPlansByTargetPath) and the post-pass (applyHookPresence).
//
// A hook bundle is present when at least one file of the harness's hook variant exists as a
// regular file at <workspace>/<HookPlan.TargetDir>/<HookFile.TargetName>. Nested target names
// (for example "lib/util.ts") resolve relative to TargetDir. A directory at
// <hooks dir>/<hook key> is not a presence signal: the executor never writes one.
//
// All tests use t.TempDir() workspaces and literal HookPlan values; nothing reads Catalog/.

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const probeHooksDir = ".claude/hooks"

// flatHookPlan returns a supported HookPlan whose files are written flat into dir.
func flatHookPlan(dir string, names ...string) domain.HookPlan {
	files := make([]domain.HookFile, 0, len(names))
	for _, n := range names {
		files = append(files, domain.HookFile{SourcePath: "/src/" + n, TargetName: n})
	}
	return domain.HookPlan{Supported: true, TargetDir: dir, Files: files}
}

// writeWorkspaceFile creates workspace-relative file rel (and its parent directories).
func writeWorkspaceFile(t *testing.T, workspace, rel string) {
	t.Helper()
	full := filepath.Join(workspace, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", rel, err)
	}
}

// ---------------------------------------------------------------------------
// probeHookPlanPresence - present layouts
// ---------------------------------------------------------------------------

// TestProbeHookPlanPresence_FlatLayout_Present verifies that a bundle whose files were written
// directly into the hooks directory (claude-code, vscode-ghcp, ghcp-cli layout) is present.
func TestProbeHookPlanPresence_FlatLayout_Present(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".claude/hooks/logger.sh")
	writeWorkspaceFile(t, ws, ".claude/hooks/logger.py")
	hp := flatHookPlan(probeHooksDir, "logger.sh", "logger.py")

	state := probeHookPlanPresence(ws, hp)

	if !state.Present {
		t.Error("Present = false for a bundle deployed flat into the hooks directory, want true")
	}
}

// TestProbeHookPlanPresence_NestedLibLayout_Present verifies that a bundle whose files include
// nested target names (opencode lib/<file>.ts) is present when the nested file is on disk.
func TestProbeHookPlanPresence_NestedLibLayout_Present(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".opencode/plugins/logger.ts")
	writeWorkspaceFile(t, ws, ".opencode/plugins/lib/util.ts")
	hp := flatHookPlan(".opencode/plugins", "logger.ts", "lib/util.ts")

	state := probeHookPlanPresence(ws, hp)

	if !state.Present {
		t.Error("Present = false for an opencode-style bundle with nested lib/ files, want true")
	}
}

// TestProbeHookPlanPresence_OnlyNestedFileOnDisk_Present verifies that the nested file alone
// is enough: nested target names are resolved relative to TargetDir, not skipped.
func TestProbeHookPlanPresence_OnlyNestedFileOnDisk_Present(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".opencode/plugins/lib/util.ts")
	hp := flatHookPlan(".opencode/plugins", "logger.ts", "lib/util.ts")

	state := probeHookPlanPresence(ws, hp)

	if !state.Present {
		t.Error("Present = false when only the nested lib/ variant file exists, want true")
	}
}

// TestProbeHookPlanPresence_SomeVariantFilesMissing_StillPresent verifies that one existing
// file out of several is sufficient for presence.
func TestProbeHookPlanPresence_SomeVariantFilesMissing_StillPresent(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".github/hooks/hooks.json")
	hp := flatHookPlan(".github/hooks", "hooks.json", "logger.sh", "logger.ps1")

	state := probeHookPlanPresence(ws, hp)

	if !state.Present {
		t.Error("Present = false when one of three variant files exists, want true")
	}
}

// ---------------------------------------------------------------------------
// probeHookPlanPresence - absent cases
// ---------------------------------------------------------------------------

// TestProbeHookPlanPresence_NoVariantFileOnDisk_Absent verifies that a supported plan whose
// files are all missing probes absent, even though the hooks directory exists.
func TestProbeHookPlanPresence_NoVariantFileOnDisk_Absent(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".claude/hooks/unrelated.sh")
	hp := flatHookPlan(probeHooksDir, "logger.sh", "logger.py")

	state := probeHookPlanPresence(ws, hp)

	if state.Present {
		t.Error("Present = true although none of the variant files exist, want false")
	}
}

// TestProbeHookPlanPresence_MissingWorkspaceDir_Absent verifies that a nonexistent hooks
// directory probes absent without error.
func TestProbeHookPlanPresence_MissingWorkspaceDir_Absent(t *testing.T) {
	ws := t.TempDir()
	hp := flatHookPlan(probeHooksDir, "logger.sh")

	state := probeHookPlanPresence(ws, hp)

	if state.Present {
		t.Error("Present = true for an empty workspace, want false")
	}
}

// TestProbeHookPlanPresence_UnsupportedPlan_Absent verifies that an unsupported HookPlan
// probes absent even when files matching its (stale) file list are on disk.
func TestProbeHookPlanPresence_UnsupportedPlan_Absent(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".claude/hooks/logger.sh")
	hp := flatHookPlan(probeHooksDir, "logger.sh")
	hp.Supported = false
	hp.Reason = "runtime-provisioned"

	state := probeHookPlanPresence(ws, hp)

	if state.Present {
		t.Error("Present = true for an unsupported HookPlan, want false")
	}
}

// TestProbeHookPlanPresence_SupportedPlanWithoutFiles_Absent verifies that a supported plan
// with no files (a bundle without a variant for the harness) probes absent.
func TestProbeHookPlanPresence_SupportedPlanWithoutFiles_Absent(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".claude/hooks/logger.sh")
	hp := domain.HookPlan{Supported: true, TargetDir: probeHooksDir}

	state := probeHookPlanPresence(ws, hp)

	if state.Present {
		t.Error("Present = true for a HookPlan with no files, want false")
	}
}

// TestProbeHookPlanPresence_EmptyTargetNameIgnored verifies that a file entry with an empty
// TargetName does not resolve to the (existing) hooks directory itself.
func TestProbeHookPlanPresence_EmptyTargetNameIgnored(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".claude/hooks/other.sh")
	hp := domain.HookPlan{
		Supported: true,
		TargetDir: probeHooksDir,
		Files:     []domain.HookFile{{SourcePath: "/src/x", TargetName: ""}},
	}

	state := probeHookPlanPresence(ws, hp)

	if state.Present {
		t.Error("Present = true for a file entry with an empty TargetName, want false")
	}
}

// TestProbeHookPlanPresence_DirectoryAtFilePath_Absent verifies that a directory sitting at
// a variant file's path is not a regular file and does not count as presence.
func TestProbeHookPlanPresence_DirectoryAtFilePath_Absent(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".claude", "hooks", "logger.sh"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	hp := flatHookPlan(probeHooksDir, "logger.sh")

	state := probeHookPlanPresence(ws, hp)

	if state.Present {
		t.Error("Present = true when the variant file path is a directory, want false")
	}
}

// TestProbeHookPlanPresence_StrayBundleDirectory_NotThePresenceSignal verifies the old
// directory-at-<hooks dir>/<key> layout is no longer a presence signal: a populated
// <hooks dir>/<key> directory does not make a bundle present when no variant file exists.
func TestProbeHookPlanPresence_StrayBundleDirectory_NotThePresenceSignal(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".claude/hooks/mosaic-logger/hook.sh")
	hp := flatHookPlan(probeHooksDir, "logger.sh")

	state := probeHookPlanPresence(ws, hp)

	if state.Present {
		t.Error("Present = true because of a stray <hooks dir>/<key> directory, want false; " +
			"presence is decided by the variant files at <TargetDir>/<TargetName>")
	}
}

// TestProbeHookPlanPresence_AbsentState_IsZeroValue verifies the absent result is the zero
// DeployedArtifactState: no nil-hash, no version fields.
func TestProbeHookPlanPresence_AbsentState_IsZeroValue(t *testing.T) {
	ws := t.TempDir()
	hp := flatHookPlan(probeHooksDir, "logger.sh")

	state := probeHookPlanPresence(ws, hp)

	if !reflect.DeepEqual(state, domain.DeployedArtifactState{}) {
		t.Errorf("absent state = %+v, want the zero DeployedArtifactState", state)
	}
}

// ---------------------------------------------------------------------------
// probeHookPlanPresence - present state conventions
// ---------------------------------------------------------------------------

// TestProbeHookPlanPresence_PresentState_CarriesNilContentHash verifies ContentHash is
// manifest.Hash(nil), the executor's recorded hash for hook bundles.
func TestProbeHookPlanPresence_PresentState_CarriesNilContentHash(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".claude/hooks/logger.sh")

	state := probeHookPlanPresence(ws, flatHookPlan(probeHooksDir, "logger.sh"))

	if !state.Present {
		t.Fatal("Present = false, want true")
	}
	if want := manifest.Hash(nil); state.ContentHash != want {
		t.Errorf("ContentHash = %q, want manifest.Hash(nil) = %q", state.ContentHash, want)
	}
}

// TestProbeHookPlanPresence_PresentState_VersionFieldsEmpty verifies every version field of a
// present state is empty: the manifest entry is the only version source for hooks.
func TestProbeHookPlanPresence_PresentState_VersionFieldsEmpty(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".claude/hooks/logger.sh")

	state := probeHookPlanPresence(ws, flatHookPlan(probeHooksDir, "logger.sh"))

	if !state.Present {
		t.Fatal("Present = false, want true")
	}
	if state.HasVersionInfo() {
		t.Error("HasVersionInfo() = true for a present hook state, want false")
	}
	if state.Version != "" || state.HarnessVersion != "" ||
		state.InjectionsVersion != "" || state.OrchestratorInjectionsVersion != "" {
		t.Errorf("version fields must be empty, got %+v", state)
	}
	if state.ParseFailed {
		t.Error("ParseFailed = true for a present hook state, want false")
	}
}

// ---------------------------------------------------------------------------
// hookPlansByTargetPath
// ---------------------------------------------------------------------------

// hookPlanModule is a HarnessModule whose HookPlan returns a canned result per bundle key.
// Only HookPlan is implemented; the embedded nil interface panics on any other method, which
// would indicate the code under test reaches beyond its contract.
type hookPlanModule struct {
	domain.HarnessModule
	plans map[string]domain.HookPlan
	errs  map[string]error
}

func (m hookPlanModule) HookPlan(req domain.HookPlanRequest) (domain.HookPlan, error) {
	if err := m.errs[req.Bundle.Key]; err != nil {
		return domain.HookPlan{}, err
	}
	return m.plans[req.Bundle.Key], nil
}

func hookPathEntry(key, path string) plan.PlannedPath {
	return plan.PlannedPath{Ref: domain.ArtifactRef{Kind: domain.ArtifactHook, Key: key}, TargetPath: path}
}

// TestHookPlansByTargetPath_KeysPlansByPlannedHookPath verifies each bundle's HookPlan is
// indexed under the bundle's planned hook target path.
func TestHookPlansByTargetPath_KeysPlansByPlannedHookPath(t *testing.T) {
	flat := flatHookPlan(probeHooksDir, "logger.sh")
	other := flatHookPlan(probeHooksDir, "audit.sh")
	module := hookPlanModule{plans: map[string]domain.HookPlan{"mosaic-logger": flat, "audit": other}}
	hooks := []domain.HookBundle{{Key: "mosaic-logger"}, {Key: "audit"}}
	paths := plan.PlannedPaths{
		hookPathEntry("mosaic-logger", ".claude/hooks/mosaic-logger"),
		hookPathEntry("audit", ".claude/hooks/audit"),
	}

	got := hookPlansByTargetPath(module, hooks, paths, domain.ScopeProject)

	if len(got) != 2 {
		t.Fatalf("len(index) = %d, want 2: %+v", len(got), got)
	}
	if hp, ok := got[".claude/hooks/mosaic-logger"]; !ok || len(hp.Files) != 1 || hp.Files[0].TargetName != "logger.sh" {
		t.Errorf("index[.claude/hooks/mosaic-logger] = %+v (found=%v), want the logger HookPlan", hp, ok)
	}
	if hp, ok := got[".claude/hooks/audit"]; !ok || len(hp.Files) != 1 || hp.Files[0].TargetName != "audit.sh" {
		t.Errorf("index[.claude/hooks/audit] = %+v (found=%v), want the audit HookPlan", hp, ok)
	}
}

// TestHookPlansByTargetPath_OmitsUnplannedAndErroringBundles verifies a bundle with no planned
// path or whose HookPlan errors is left out, and unsupported plans are kept.
func TestHookPlansByTargetPath_OmitsUnplannedAndErroringBundles(t *testing.T) {
	module := hookPlanModule{
		plans: map[string]domain.HookPlan{
			"unplanned":   flatHookPlan(probeHooksDir, "a.sh"),
			"unsupported": {Supported: false, Reason: "runtime"},
		},
		errs: map[string]error{"broken": errors.New("boom")},
	}
	hooks := []domain.HookBundle{{Key: "unplanned"}, {Key: "broken"}, {Key: "unsupported"}}
	paths := plan.PlannedPaths{
		hookPathEntry("broken", ".claude/hooks/broken"),
		hookPathEntry("unsupported", ".claude/hooks/unsupported"),
	}

	got := hookPlansByTargetPath(module, hooks, paths, domain.ScopeProject)

	if got == nil {
		t.Fatal("index is nil, want a non-nil map")
	}
	if _, ok := got[".claude/hooks/broken"]; ok {
		t.Error("bundle whose HookPlan errors must be omitted")
	}
	if len(got) != 1 {
		t.Errorf("len(index) = %d, want 1 (only the unsupported bundle): %+v", len(got), got)
	}
	if hp, ok := got[".claude/hooks/unsupported"]; !ok || hp.Supported {
		t.Errorf("index[.claude/hooks/unsupported] = %+v (found=%v), want the unsupported plan kept", hp, ok)
	}
}

// ---------------------------------------------------------------------------
// applyHookPresence
// ---------------------------------------------------------------------------

// TestApplyHookPresence_DeployedBundle_OverwritesAbsentEntry verifies the post-pass turns the
// generic probe's absent hook entry into a present one for a flat-deployed bundle.
func TestApplyHookPresence_DeployedBundle_OverwritesAbsentEntry(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".claude/hooks/logger.sh")
	const key = ".claude/hooks/mosaic-logger"
	state := map[string]domain.DeployedArtifactState{key: {Present: false}}
	hookPlans := map[string]domain.HookPlan{key: flatHookPlan(probeHooksDir, "logger.sh")}

	applyHookPresence(ws, state, hookPlans)

	got := state[key]
	if !got.Present {
		t.Fatalf("state[%s].Present = false after the post-pass, want true", key)
	}
	if got.ContentHash != manifest.Hash(nil) {
		t.Errorf("ContentHash = %q, want manifest.Hash(nil)", got.ContentHash)
	}
}

// TestApplyHookPresence_BundleNotOnDisk_RecordsAbsent verifies the post-pass replaces a stale
// present entry with absent when the variant files are not on disk.
func TestApplyHookPresence_BundleNotOnDisk_RecordsAbsent(t *testing.T) {
	ws := t.TempDir()
	const key = ".claude/hooks/mosaic-logger"
	state := map[string]domain.DeployedArtifactState{
		key: {Present: true, ContentHash: manifest.Hash(nil)},
	}
	hookPlans := map[string]domain.HookPlan{key: flatHookPlan(probeHooksDir, "logger.sh")}

	applyHookPresence(ws, state, hookPlans)

	if state[key].Present {
		t.Errorf("state[%s].Present = true with no variant file on disk, want false", key)
	}
}

// TestApplyHookPresence_NonHookEntriesUntouched verifies entries whose key is not in the hook
// plan index (agents, skills, seeded entries) are not modified or removed.
func TestApplyHookPresence_NonHookEntriesUntouched(t *testing.T) {
	ws := t.TempDir()
	writeWorkspaceFile(t, ws, ".claude/hooks/logger.sh")
	const hookKey = ".claude/hooks/mosaic-logger"
	agent := domain.DeployedArtifactState{Present: true, ContentHash: "sha256:agent", Version: "2.0"}
	skill := domain.DeployedArtifactState{Present: false}
	state := map[string]domain.DeployedArtifactState{
		hookKey:                  {},
		".claude/agents/foo.md":  agent,
		".claude/skills/bar.md":  skill,
	}
	hookPlans := map[string]domain.HookPlan{hookKey: flatHookPlan(probeHooksDir, "logger.sh")}

	applyHookPresence(ws, state, hookPlans)

	if !state[hookKey].Present {
		t.Fatal("hook entry was not updated; the post-pass must apply to hook entries")
	}
	if !reflect.DeepEqual(state[".claude/agents/foo.md"], agent) {
		t.Errorf("agent entry changed to %+v, want it untouched", state[".claude/agents/foo.md"])
	}
	if !reflect.DeepEqual(state[".claude/skills/bar.md"], skill) {
		t.Errorf("skill entry changed to %+v, want it untouched", state[".claude/skills/bar.md"])
	}
	if len(state) != 3 {
		t.Errorf("len(state) = %d, want 3 (no entries added or removed)", len(state))
	}
}

// TestApplyHookPresence_NilOrEmptyHookPlans_NoOp verifies the post-pass does nothing, and does
// not panic, when there are no hook plans (including a nil state map).
func TestApplyHookPresence_NilOrEmptyHookPlans_NoOp(t *testing.T) {
	ws := t.TempDir()
	untouched := domain.DeployedArtifactState{Present: true, ContentHash: "sha256:x"}
	state := map[string]domain.DeployedArtifactState{"a.md": untouched}

	applyHookPresence(ws, state, nil)
	applyHookPresence(ws, state, map[string]domain.HookPlan{})
	applyHookPresence(ws, nil, nil)

	if len(state) != 1 || !reflect.DeepEqual(state["a.md"], untouched) {
		t.Errorf("state changed to %+v, want it unchanged", state)
	}
}
