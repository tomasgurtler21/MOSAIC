package app_test

// update_hooks_registration_test.go verifies the registration and conflict behaviour of the
// Update hook refresh: an existing registration target is never modified and is reported as a
// TODO, a missing target is created, hooks that are not written (up to date, or skipped by the
// conflict decision) produce no registration write, no registration TODO and no registration
// step in the reviewed plan, and a present hook without a manifest record follows the
// conflict decision.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

const uhExistingSettings = "{\"userSettings\": true}\n"

func TestUpdate_RefreshedHookWithExistingRegistrationTarget_TargetUntouchedAndTodoReported(t *testing.T) {
	files := staleFlatFiles()
	files[uhRegTarget] = uhExistingSettings

	run := runUpdateHooks(t, uhConfig{recorded: uhStaleVersion, files: files})

	requireUpdateHooksOK(t, run)
	if got, _ := run.read(uhRegTarget); got != uhExistingSettings {
		t.Errorf("registration target = %q, want it byte-identical %q", got, uhExistingSettings)
	}
	if got, _ := run.read(uhHooksDir + "/util.sh"); got != uhFixture(t, uhFlat, "util.sh") {
		t.Fatal("hook was not refreshed")
	}
	if !uhHasRegistrationGap(run.todo.gaps) {
		t.Errorf("no hook-registration TODO for %s: %+v", uhRegStepID, run.todo.gaps)
	}
}

func TestUpdate_RefreshedHook_ReviewedPlanCarriesItsRegistrationStepAndGap(t *testing.T) {
	files := staleFlatFiles()
	files[uhRegTarget] = uhExistingSettings

	run := runUpdateHooks(t, uhConfig{recorded: uhStaleVersion, files: files})

	requireUpdateHooksOK(t, run)
	if run.reviewed == nil {
		t.Fatal("no plan was reviewed")
	}
	if !uhHasRegistrationStep(run.reviewed.Registrations) {
		t.Errorf("reviewed plan lacks the registration step of the refreshed hook: %+v", run.reviewed.Registrations)
	}
	if !uhHasRegistrationGap(run.reviewed.Gaps) {
		t.Errorf("reviewed plan lacks the registration gap of the refreshed hook: %+v", run.reviewed.Gaps)
	}
}

func TestUpdate_RefreshedHookWithMissingRegistrationTarget_TargetCreatedWithFragment(t *testing.T) {
	run := runUpdateHooks(t, uhConfig{recorded: uhStaleVersion, files: staleFlatFiles()})

	requireUpdateHooksOK(t, run)
	got, ok := run.read(uhRegTarget)
	if !ok {
		t.Fatal("registration target was not created")
	}
	if want := uhFragment(t); got != want {
		t.Errorf("registration target = %q, want fragment %q", got, want)
	}
}

func TestUpdate_UpToDateHookWithExistingRegistrationTarget_NoRegistrationTodoOrPlanEntry(t *testing.T) {
	files := uhEditedFiles(uhFlat)
	files[uhRegTarget] = uhExistingSettings

	run := runUpdateHooks(t, uhConfig{recorded: uhCatalogVersion, files: files})

	requireUpdateHooksOK(t, run)
	if a, ok := run.hookAction(); !ok || a.Taken != domain.TakenUnchanged {
		t.Fatalf("hook action = %+v (found %v), want TakenUnchanged", a, ok)
	}
	if uhHasRegistrationGap(run.todo.gaps) {
		t.Errorf("hook-registration TODO reported for an up-to-date hook: %+v", run.todo.gaps)
	}
	if run.reviewed == nil {
		t.Fatal("no plan was reviewed")
	}
	if uhHasRegistrationGap(run.reviewed.Gaps) {
		t.Errorf("reviewed plan carries a registration gap for an up-to-date hook: %+v", run.reviewed.Gaps)
	}
	if uhHasRegistrationStep(run.reviewed.Registrations) {
		t.Errorf("reviewed plan carries registration steps for an up-to-date hook: %+v", run.reviewed.Registrations)
	}
	if got, _ := run.read(uhRegTarget); got != uhExistingSettings {
		t.Errorf("registration target = %q, want it byte-identical", got)
	}
}

func TestUpdate_UpToDateHookWithMissingRegistrationTarget_TargetNotCreated(t *testing.T) {
	run := runUpdateHooks(t, uhConfig{recorded: uhCatalogVersion, files: uhEditedFiles(uhFlat)})

	requireUpdateHooksOK(t, run)
	if a, ok := run.hookAction(); !ok || a.Taken != domain.TakenUnchanged {
		t.Fatalf("hook action = %+v (found %v), want TakenUnchanged", a, ok)
	}
	if _, ok := run.read(uhRegTarget); ok {
		t.Error("registration target was created for an up-to-date hook")
	}
}

func TestUpdate_PresentHookWithoutManifestRecordSkipped_NoWritesAndNoRegistrationTodo(t *testing.T) {
	files := uhEditedFiles(uhFlat, "logger.sh")
	files[uhRegTarget] = uhExistingSettings

	run := runUpdateHooks(t, uhConfig{files: files, conflict: domain.DecisionSkip})

	requireUpdateHooksOK(t, run)
	if got, _ := run.read(uhHooksDir + "/logger.sh"); got != files[uhHooksDir+"/logger.sh"] {
		t.Errorf("skipped hook file = %q, want it untouched", got)
	}
	if _, ok := run.read(uhHooksDir + "/util.sh"); ok {
		t.Error("util.sh was created for a skipped hook")
	}
	if uhHasRegistrationGap(run.todo.gaps) {
		t.Errorf("hook-registration TODO reported for a skipped hook: %+v", run.todo.gaps)
	}
	if !run.todo.hasGapKind(domain.GapSkippedFile) {
		t.Errorf("no skipped-file TODO for the skipped hook: %+v", run.todo.gaps)
	}
	if run.reviewed == nil || uhHasRegistrationStep(run.reviewed.Registrations) || uhHasRegistrationGap(run.reviewed.Gaps) {
		t.Errorf("reviewed plan keeps registration artefacts of a skipped hook: %+v", run.reviewed)
	}
}

func TestUpdate_PresentHookWithoutManifestRecordSkipped_MissingRegistrationTargetNotCreated(t *testing.T) {
	run := runUpdateHooks(t, uhConfig{files: uhEditedFiles(uhFlat, "logger.sh"), conflict: domain.DecisionSkip})

	requireUpdateHooksOK(t, run)
	if _, ok := run.read(uhRegTarget); ok {
		t.Error("registration target was created for a skipped hook")
	}
	if len(run.exec.req.Hooks) != 0 {
		t.Errorf("executor received %d hook plans for a skipped hook, want 0", len(run.exec.req.Hooks))
	}
}

func TestUpdate_PresentHookWithoutManifestRecordOverwritten_RefreshedToCatalog(t *testing.T) {
	files := uhEditedFiles(uhFlat, "logger.sh")

	run := runUpdateHooks(t, uhConfig{files: files, conflict: domain.DecisionOverwrite})

	requireUpdateHooksOK(t, run)
	for _, name := range uhFlat.files {
		if got, _ := run.read(uhHooksDir + "/" + name); got != uhFixture(t, uhFlat, name) {
			t.Errorf("%s = %q, want catalog content", name, got)
		}
	}
	if e, ok := run.hookEntry(); !ok || e.Version != uhCatalogVersion {
		t.Errorf("manifest hook entry = %+v (found %v), want version %s", e, ok, uhCatalogVersion)
	}
}

func TestUpdate_PresentHookWithoutManifestRecordBackedUp_RefreshedToCatalog(t *testing.T) {
	files := uhEditedFiles(uhFlat, "logger.sh")

	run := runUpdateHooks(t, uhConfig{files: files, conflict: domain.DecisionBackupThenOverwrite})

	requireUpdateHooksOK(t, run)
	if got, _ := run.read(uhHooksDir + "/logger.sh"); got != uhFixture(t, uhFlat, "logger.sh") {
		t.Errorf("logger.sh = %q, want catalog content", got)
	}
	if got, _ := run.read(uhHooksDir + "/util.sh"); got != uhFixture(t, uhFlat, "util.sh") {
		t.Errorf("util.sh = %q, want catalog content", got)
	}
}
