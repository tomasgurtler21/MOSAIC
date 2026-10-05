package app_test

// hook_probe_flow_test.go verifies, through the DeployHooks service, that the deployed-hook
// state reaching the planner reflects the files the deploy actually writes (flat in the hooks
// directory), and that the real planner then evaluates the manifest-recorded version instead of
// reporting an already-deployed bundle as new.
//
// Fixtures are temp-dir workspaces and stub harness modules; nothing reads Catalog/.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

const (
	flowHooksDir   = ".claude/hooks"
	flowHookKey    = "git-hooks"
	flowHookTarget = flowHooksDir + "/" + flowHookKey
)

// flatHookModule is a harness module whose hook bundles deploy flat into flowHooksDir
// (target path <hooks dir>/<key>, files written as <hooks dir>/<TargetName>).
type flatHookModule struct {
	*stubHarnessModule
}

func (m flatHookModule) TargetPath(req domain.TargetPathRequest) (string, error) {
	if req.Kind == domain.ArtifactHook {
		return flowHooksDir + "/" + req.Key, nil
	}
	return m.stubHarnessModule.TargetPath(req)
}

func (m flatHookModule) HookPlan(_ domain.HookPlanRequest) (domain.HookPlan, error) {
	return domain.HookPlan{
		Supported: true,
		TargetDir: flowHooksDir,
		Files: []domain.HookFile{
			{SourcePath: "/src/logger.sh", TargetName: "logger.sh"},
			{SourcePath: "/src/lib/util.sh", TargetName: "lib/util.sh"},
		},
	}, nil
}

// newFlatHookDeps returns deps whose catalog holds one hook bundle at catalogVersion, whose
// harness deploys it flat, and whose workspace already contains the deployed files when
// deployed is true.
func newFlatHookDeps(t *testing.T, catalogVersion string, deployed bool) (app.Deps, string) {
	t.Helper()
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)

	cat := newMinimalCatalog()
	cat.hooks = []domain.HookBundle{{Key: flowHookKey, Version: catalogVersion}}
	deps.Catalog = cat

	module := flatHookModule{stubHarnessModule: newMinimalModule()}
	deps.Registry = &stubRegistry{
		list:    []domain.HarnessRef{minimalHarness},
		modules: map[string]domain.HarnessModule{"stub-harness": module},
	}

	if deployed {
		full := filepath.Join(workspace, ".claude", "hooks", "lib", "util.sh")
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return deps, workspace
}

// manifestWithHook returns a usable manifest snapshot recording the hook at recordedVersion
// with the nil-content hash the executor stores for hook bundles.
func manifestWithHook(recordedVersion string) *stubManifestStore {
	return &stubManifestStore{snap: manifest.Snapshot{
		State: manifest.StatePresent,
		Manifest: domain.Manifest{Entries: []domain.ManifestEntry{{
			Ref:         domain.ArtifactRef{Kind: domain.ArtifactHook, Key: flowHookKey},
			TargetPath:  flowHookTarget,
			Version:     recordedVersion,
			ContentHash: manifest.Hash(nil),
		}}},
	}}
}

func runFlatDeployHooks(t *testing.T, deps app.Deps, workspace string) {
	t.Helper()
	_, err := app.New(deps).DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		Scope:           domain.ScopeProject,
		HookIDs:         []string{flowHookKey},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployHooks: %v", err)
	}
}

// TestDeployHooks_FlatDeployedBundle_PlannerSeesHookPresent verifies that a bundle deployed
// flat in the workspace reaches the planner as present under the planned hook target path.
func TestDeployHooks_FlatDeployedBundle_PlannerSeesHookPresent(t *testing.T) {
	deps, workspace := newFlatHookDeps(t, "1.0", true)
	capPlan := &capturingPlanner{result: hooksPlan(workspace)}
	deps.Planner = capPlan

	runFlatDeployHooks(t, deps, workspace)

	if capPlan.capturedInput == nil {
		t.Fatal("planner was not called")
	}
	state, ok := capPlan.capturedInput.DeployedState[flowHookTarget]
	if !ok {
		t.Fatalf("DeployedState has no entry for %q: %+v", flowHookTarget, capPlan.capturedInput.DeployedState)
	}
	if !state.Present {
		t.Errorf("DeployedState[%q].Present = false for a bundle deployed flat, want true", flowHookTarget)
	}
	if state.ContentHash != manifest.Hash(nil) {
		t.Errorf("DeployedState[%q].ContentHash = %q, want manifest.Hash(nil)", flowHookTarget, state.ContentHash)
	}
}

// TestDeployHooks_NoFilesOnDisk_PlannerSeesHookAbsent verifies a bundle with no deployed
// files reaches the planner as absent.
func TestDeployHooks_NoFilesOnDisk_PlannerSeesHookAbsent(t *testing.T) {
	deps, workspace := newFlatHookDeps(t, "1.0", false)
	capPlan := &capturingPlanner{result: hooksPlan(workspace)}
	deps.Planner = capPlan

	runFlatDeployHooks(t, deps, workspace)

	if capPlan.capturedInput == nil {
		t.Fatal("planner was not called")
	}
	if capPlan.capturedInput.DeployedState[flowHookTarget].Present {
		t.Errorf("DeployedState[%q].Present = true with no files on disk, want false", flowHookTarget)
	}
}

// capturePlannedHookItem runs DeployHooks with the real planner and returns the hook plan item.
func capturePlannedHookItem(t *testing.T, deps app.Deps, workspace string) domain.PlanItem {
	t.Helper()
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec

	runFlatDeployHooks(t, deps, workspace)

	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; no plan captured")
	}
	item, ok := findPlanItem(capExec.capturedPlan.Items, flowHookKey)
	if !ok {
		t.Fatalf("plan has no hook item for %q: %+v", flowHookKey, capExec.capturedPlan.Items)
	}
	return item
}

// TestDeployHooks_DeployedBundleOlderRecordedVersion_ClassifiedUpdate verifies that with the
// bundle's files on disk and an older manifest-recorded version, the real planner reports an
// update with a version delta, not a new hook bundle.
func TestDeployHooks_DeployedBundleOlderRecordedVersion_ClassifiedUpdate(t *testing.T) {
	deps, workspace := newFlatHookDeps(t, "2.0", true)
	deps.Manifest = manifestWithHook("1.0")

	item := capturePlannedHookItem(t, deps, workspace)

	if item.Action != domain.ActionUpdate {
		t.Fatalf("hook action = %v (reason %q), want ActionUpdate", item.Action, item.Reason)
	}
	if len(item.Stale) == 0 {
		t.Error("hook item has no version delta, want at least one")
	}
}

// TestDeployHooks_DeployedBundleSameRecordedVersion_ClassifiedUnchanged verifies that with the
// bundle's files on disk and an equal manifest-recorded version, the hook is unchanged.
func TestDeployHooks_DeployedBundleSameRecordedVersion_ClassifiedUnchanged(t *testing.T) {
	deps, workspace := newFlatHookDeps(t, "1.0", true)
	deps.Manifest = manifestWithHook("1.0")

	item := capturePlannedHookItem(t, deps, workspace)

	if item.Action != domain.ActionUnchanged {
		t.Fatalf("hook action = %v (reason %q), want ActionUnchanged", item.Action, item.Reason)
	}
}

// TestDeployHooks_NoFilesOnDiskWithManifestEntry_ClassifiedCreate verifies that a manifest
// entry alone does not make a bundle deployed: with no files on disk the hook is created.
func TestDeployHooks_NoFilesOnDiskWithManifestEntry_ClassifiedCreate(t *testing.T) {
	deps, workspace := newFlatHookDeps(t, "2.0", false)
	deps.Manifest = manifestWithHook("1.0")

	item := capturePlannedHookItem(t, deps, workspace)

	if item.Action != domain.ActionCreate {
		t.Fatalf("hook action = %v (reason %q), want ActionCreate", item.Action, item.Reason)
	}
}
