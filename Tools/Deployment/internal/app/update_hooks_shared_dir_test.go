package app_test

// update_hooks_shared_dir_test.go verifies the shared hooks directory case: two harnesses
// deploy different variants of the same hook bundle into the same directory under the same
// manifest key, and Update run for either harness refreshes the stale hook with that harness's
// own variant files and records the catalog version.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// sharedHarness is one of the two harnesses sharing the hooks directory.
type sharedHarness struct {
	id     string
	layout uhLayout
}

var sharedHarnesses = []sharedHarness{
	{id: "claude-like", layout: uhFlat},
	{id: "vscode-like", layout: uhFlatAlt},
}

func TestUpdate_StaleSharedHook_EitherHarnessRefreshesItsOwnVariantFiles(t *testing.T) {
	for _, h := range sharedHarnesses {
		t.Run(h.id, func(t *testing.T) {
			module := newTestHookModule(h.id, newTestHookPlan(t, h.layout))
			files := uhEditedFiles(h.layout, h.layout.files[0])

			run := runUpdateHooks(t, uhConfig{module: module, recorded: uhStaleVersion, files: files})

			requireUpdateHooksOK(t, run)
			for _, name := range h.layout.files {
				got, ok := run.read(uhHooksDir + "/" + name)
				if !ok {
					t.Errorf("%s is missing after the refresh", name)
					continue
				}
				if want := uhFixture(t, h.layout, name); got != want {
					t.Errorf("%s = %q, want the %s variant content %q", name, got, h.id, want)
				}
			}
			if e, ok := run.hookEntry(); !ok || e.Version != uhCatalogVersion {
				t.Errorf("manifest hook entry = %+v (found %v), want version %s", e, ok, uhCatalogVersion)
			}
			if a, ok := run.hookAction(); !ok || a.Taken != domain.TakenUpdated {
				t.Errorf("hook action = %+v (found %v), want TakenUpdated", a, ok)
			}
		})
	}
}

func TestUpdate_StaleSharedHook_OtherHarnessVariantOnlyFileIsNotCreated(t *testing.T) {
	h := sharedHarnesses[0] // the other variant ships alt-only.sh, which this harness never writes
	module := newTestHookModule(h.id, newTestHookPlan(t, h.layout))

	run := runUpdateHooks(t, uhConfig{module: module, recorded: uhStaleVersion, files: uhEditedFiles(h.layout)})

	requireUpdateHooksOK(t, run)
	if got, _ := run.read(uhHooksDir + "/util.sh"); got != uhFixture(t, h.layout, "util.sh") {
		t.Fatal("the shared hook was not refreshed")
	}
	if _, ok := run.read(uhHooksDir + "/alt-only.sh"); ok {
		t.Error("a file of the other harness's variant was written")
	}
}
