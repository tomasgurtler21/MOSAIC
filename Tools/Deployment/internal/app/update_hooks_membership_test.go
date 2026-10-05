package app_test

// update_hooks_membership_test.go verifies which Catalog hook bundles Service.Update brings
// into a run: only bundles whose variant files are on disk for the run's harness. A Catalog
// hook that is not deployed, a manifest entry without files, and a runtime-provisioned harness
// (unsupported hook plan, no files) produce no plan item, no files and no manifest change.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

func TestUpdate_DeployedHook_ReachesPlannerWithItsPresence(t *testing.T) {
	run := runUpdateHooks(t, uhConfig{recorded: uhStaleVersion, files: staleFlatFiles()})

	requireUpdateHooksOK(t, run)
	if len(run.planIn.HookIDs) != 1 || run.planIn.HookIDs[0] != uhHookKey {
		t.Fatalf("planner HookIDs = %v, want [%s]", run.planIn.HookIDs, uhHookKey)
	}
	if !run.planIn.DeployedState[uhHookTarget].Present {
		t.Errorf("planner DeployedState[%q].Present = false for a deployed hook", uhHookTarget)
	}
}

func TestUpdate_CatalogHookNotOnDisk_ProducesNoItemFilesOrManifestEntry(t *testing.T) {
	run := runUpdateHooks(t, uhConfig{})

	requireUpdateHooksOK(t, run)
	assertNoHookActivity(t, run)
	if e, ok := run.hookEntry(); ok {
		t.Errorf("manifest gained a hook entry: %+v", e)
	}
}

func TestUpdate_ManifestEntryWithoutFiles_HookIsNotDeployed(t *testing.T) {
	run := runUpdateHooks(t, uhConfig{recorded: uhStaleVersion})

	requireUpdateHooksOK(t, run)
	assertNoHookActivity(t, run)
	if e, ok := run.hookEntry(); ok && e.Version != uhStaleVersion {
		t.Errorf("manifest hook entry version = %s, want it unchanged at %s", e.Version, uhStaleVersion)
	}
}

func TestUpdate_RuntimeProvisionedHarness_ProducesNoHookItemReportLineOrManifestChange(t *testing.T) {
	module := newTestHookModule(uhHarnessID, domain.HookPlan{Supported: false, Reason: "runtime provisioned"})
	module.descriptor.Paths.Hooks = domain.ScopedPaths{Supported: true, Project: uhHooksDir}
	files := map[string]string{uhHooksDir + "/logger.sh": "provisioned at runtime\n"}

	run := runUpdateHooks(t, uhConfig{module: module, recorded: uhStaleVersion, files: files})

	requireUpdateHooksOK(t, run)
	assertNoHookActivity(t, run)
	if got, _ := run.read(uhHooksDir + "/logger.sh"); got != files[uhHooksDir+"/logger.sh"] {
		t.Errorf("runtime-provisioned hook file changed: %q", got)
	}
	if e, ok := run.hookEntry(); ok && e.Version != uhStaleVersion {
		t.Errorf("manifest hook entry version = %s, want it unchanged at %s", e.Version, uhStaleVersion)
	}
}

func TestUpdate_HarnessWithoutBundleVariant_ProducesNoHookActivity(t *testing.T) {
	module := newTestHookModule(uhHarnessID, domain.HookPlan{Supported: true, TargetDir: uhHooksDir})
	files := map[string]string{uhHooksDir + "/logger.sh": "an unrelated file\n"}

	run := runUpdateHooks(t, uhConfig{module: module, files: files})

	requireUpdateHooksOK(t, run)
	assertNoHookActivity(t, run)
}

// assertNoHookActivity fails when the run planned, reported or wrote anything for the hook.
func assertNoHookActivity(t *testing.T, run *uhRun) {
	t.Helper()
	if len(run.planIn.HookIDs) != 0 {
		t.Errorf("planner HookIDs = %v, want none", run.planIn.HookIDs)
	}
	if item, ok := run.hookItem(); ok {
		t.Errorf("plan has a hook item: %+v", item)
	}
	if a, ok := run.hookAction(); ok {
		t.Errorf("run reported a hook action: %+v", a)
	}
	if len(run.exec.req.Hooks) != 0 {
		t.Errorf("executor received %d hook plans, want 0", len(run.exec.req.Hooks))
	}
	if _, ok := run.read(uhHooksDir + "/util.sh"); ok {
		t.Error("hook file util.sh was written")
	}
	if _, ok := run.read(uhRegTarget); ok {
		t.Error("registration target was created")
	}
}
