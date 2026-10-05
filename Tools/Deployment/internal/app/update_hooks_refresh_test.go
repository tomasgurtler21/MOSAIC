package app_test

// update_hooks_refresh_test.go verifies that Service.Update refreshes a hook bundle that is
// already deployed in the workspace to the catalog version: every file of the harness's hook
// variant is rewritten (local edits overwritten, missing variant files created), obsolete
// files stay, the manifest records the catalog version, and an up-to-date hook is reported
// unchanged and never rewritten. Both the flat layout and the nested lib/ layout are covered.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// staleFlatFiles is a flat-layout workspace: a locally edited logger.sh, no util.sh (a variant
// file the earlier deploy did not have) and an obsolete file the catalog no longer ships.
func staleFlatFiles() map[string]string {
	return map[string]string{
		uhHooksDir + "/logger.sh":      "LOCALLY EDITED logger\n",
		uhHooksDir + "/old-helper.sh": "obsolete helper\n",
	}
}

func TestUpdate_StaleFlatHook_AllVariantFilesWrittenWithCatalogContent(t *testing.T) {
	run := runUpdateHooks(t, uhConfig{recorded: uhStaleVersion, files: staleFlatFiles()})

	requireUpdateHooksOK(t, run)
	for _, name := range uhFlat.files {
		got, ok := run.read(uhHooksDir + "/" + name)
		if !ok {
			t.Errorf("%s is missing after the refresh", name)
			continue
		}
		if want := uhFixture(t, uhFlat, name); got != want {
			t.Errorf("%s = %q, want catalog content %q", name, got, want)
		}
	}
}

func TestUpdate_StaleFlatHook_ObsoleteFileLeftInPlace(t *testing.T) {
	run := runUpdateHooks(t, uhConfig{recorded: uhStaleVersion, files: staleFlatFiles()})

	requireUpdateHooksOK(t, run)
	if got, ok := run.read(uhHooksDir + "/old-helper.sh"); !ok || got != "obsolete helper\n" {
		t.Errorf("obsolete hook file = %q (present %v), want it left untouched", got, ok)
	}
	// The refresh must have happened for the obsolete-file assertion to be meaningful.
	if got, _ := run.read(uhHooksDir + "/util.sh"); got != uhFixture(t, uhFlat, "util.sh") {
		t.Errorf("util.sh was not created by the refresh")
	}
}

func TestUpdate_StaleFlatHook_ReportedUpdatedWithCatalogVersionInManifest(t *testing.T) {
	run := runUpdateHooks(t, uhConfig{recorded: uhStaleVersion, files: staleFlatFiles()})

	requireUpdateHooksOK(t, run)
	item, ok := run.hookItem()
	if !ok || item.Action != domain.ActionUpdate {
		t.Fatalf("hook plan item = %+v (found %v), want ActionUpdate", item, ok)
	}
	if a, ok := run.hookAction(); !ok || a.Taken != domain.TakenUpdated {
		t.Errorf("hook action = %+v (found %v), want TakenUpdated", a, ok)
	}
	if e, ok := run.hookEntry(); !ok || e.Version != uhCatalogVersion {
		t.Errorf("manifest hook entry = %+v (found %v), want version %s", e, ok, uhCatalogVersion)
	}
}

func TestUpdate_StaleNestedHook_FlatAndLibFilesWrittenWithCatalogContent(t *testing.T) {
	module := newTestHookModule(uhHarnessID, newTestHookPlan(t, uhNested))
	files := map[string]string{
		uhHooksDir + "/logger.ts":  "LOCALLY EDITED logger\n",
		uhHooksDir + "/old-lib.ts": "obsolete\n",
	}

	run := runUpdateHooks(t, uhConfig{module: module, recorded: uhStaleVersion, files: files})

	requireUpdateHooksOK(t, run)
	for _, name := range uhNested.files {
		got, ok := run.read(uhHooksDir + "/" + name)
		if !ok {
			t.Errorf("%s is missing after the refresh", name)
			continue
		}
		if want := uhFixture(t, uhNested, name); got != want {
			t.Errorf("%s = %q, want catalog content %q", name, got, want)
		}
	}
	if got, ok := run.read(uhHooksDir + "/old-lib.ts"); !ok || got != "obsolete\n" {
		t.Errorf("obsolete nested-layout file = %q (present %v), want it left untouched", got, ok)
	}
	if e, ok := run.hookEntry(); !ok || e.Version != uhCatalogVersion {
		t.Errorf("manifest hook entry = %+v (found %v), want version %s", e, ok, uhCatalogVersion)
	}
}

func TestUpdate_NestedHookWithOnlyLibFilePresent_IsDeployedAndRefreshed(t *testing.T) {
	module := newTestHookModule(uhHarnessID, newTestHookPlan(t, uhNested))
	files := map[string]string{uhHooksDir + "/lib/util.ts": "LOCALLY EDITED util\n"}

	run := runUpdateHooks(t, uhConfig{module: module, recorded: uhStaleVersion, files: files})

	requireUpdateHooksOK(t, run)
	if got, _ := run.read(uhHooksDir + "/lib/util.ts"); got != uhFixture(t, uhNested, "lib/util.ts") {
		t.Errorf("lib/util.ts was not refreshed: %q", got)
	}
	if _, ok := run.read(uhHooksDir + "/logger.ts"); !ok {
		t.Error("logger.ts was not created by the refresh")
	}
}

func TestUpdate_UpToDateHook_ReportedUnchangedAndFilesNotRewritten(t *testing.T) {
	files := uhEditedFiles(uhFlat)

	run := runUpdateHooks(t, uhConfig{recorded: uhCatalogVersion, files: files})

	requireUpdateHooksOK(t, run)
	if a, ok := run.hookAction(); !ok || a.Taken != domain.TakenUnchanged {
		t.Fatalf("hook action = %+v (found %v), want TakenUnchanged", a, ok)
	}
	for rel, want := range files {
		if got, _ := run.read(rel); got != want {
			t.Errorf("%s = %q, want it left byte-identical %q", rel, got, want)
		}
	}
	if len(run.exec.req.Hooks) != 0 {
		t.Errorf("executor received %d hook plans for an up-to-date hook, want 0", len(run.exec.req.Hooks))
	}
}

func TestUpdate_UpToDateNestedHook_FilesNotRewritten(t *testing.T) {
	module := newTestHookModule(uhHarnessID, newTestHookPlan(t, uhNested))
	files := uhEditedFiles(uhNested)

	run := runUpdateHooks(t, uhConfig{module: module, recorded: uhCatalogVersion, files: files})

	requireUpdateHooksOK(t, run)
	if a, ok := run.hookAction(); !ok || a.Taken != domain.TakenUnchanged {
		t.Fatalf("hook action = %+v (found %v), want TakenUnchanged", a, ok)
	}
	for rel, want := range files {
		if got, _ := run.read(rel); got != want {
			t.Errorf("%s = %q, want it left byte-identical %q", rel, got, want)
		}
	}
}
