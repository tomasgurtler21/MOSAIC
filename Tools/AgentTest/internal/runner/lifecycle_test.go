package runner_test

// Lifecycle and teardown tests: the ordering of setup steps and, more
// importantly, teardown under every exit path an attempt can take. Teardown
// removes exactly the ledger's contents — a path absent from the ledger is
// never touched.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/runner"
	"mosaic-agent-test/internal/workspace"
)

func TestRun_NormalCompletion_TearsDownTheSandboxAfterward(t *testing.T) {
	h := newHarness(t)
	req := newRequest("happy-path")

	evidence, err := runner.Run(context.Background(), h.Deps, req)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if evidence.SubjectResult.Disposition != domain.DispositionCompleted {
		t.Errorf("evidence.SubjectResult.Disposition = %q, want %q", evidence.SubjectResult.Disposition, domain.DispositionCompleted)
	}

	if _, err := h.Deps.Workspaces.Locate(req.Key); !errors.Is(err, workspace.ErrSandboxNotFound) {
		t.Errorf("Locate after Run = %v, want %v — teardown must have removed the sandbox", err, workspace.ErrSandboxNotFound)
	}
}

func TestRun_LauncherReturnsAnError_StillTearsDownTheSandbox(t *testing.T) {
	h := newHarness(t)
	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		return domain.SubjectResult{Disposition: domain.DispositionSpawnFailed}, errors.New("subject could not be started")
	}
	req := newRequest("launch-error")

	if _, err := runner.Run(context.Background(), h.Deps, req); err == nil {
		t.Error("Run returned no error, want the spawn failure to be reported")
	}

	assertSandboxWasCreatedThenTornDown(t, h)
}

func TestRun_ProvisionFails_StillTearsDownWhatSetupAlreadyCreated(t *testing.T) {
	h := newHarness(t)
	h.Adapter.provisionErr = errors.New("provisioning failed")
	req := newRequest("provision-error")

	if _, err := runner.Run(context.Background(), h.Deps, req); err == nil {
		t.Error("Run returned no error, want the provisioning failure to be reported")
	}

	// Provisioning is attempted only after the sandbox itself exists, so a
	// provisioning failure is a strictly stronger claim about ordering than
	// the other exit paths: the sandbox must have been created before the
	// failure, and still torn down afterward.
	assertSandboxWasCreatedThenTornDown(t, h)
}

func TestRun_SeedFileResolutionFails_StillTearsDownWhatSetupAlreadyCreated(t *testing.T) {
	h := newHarness(t)
	req := newRequest("seed-resolution-error")
	// The fixture root is a fresh, empty temp directory (see newHarness), so
	// this $ref names a file that does not exist there: setup fails at its
	// seed-files step (step 3, before the registry/groups documents, run
	// state and Provision), strictly earlier than the already-covered
	// Provision failure above. Setup must still have created the sandbox
	// before this point, and teardown must still remove it.
	req.Test.Definition.SeedFiles = []domain.SeedFile{
		{Path: "notes/plan.md", Ref: "does-not-exist.json"},
	}

	if _, err := runner.Run(context.Background(), h.Deps, req); err == nil {
		t.Error("Run returned no error, want the seed-file resolution failure to be reported")
	}

	assertSandboxWasCreatedThenTornDown(t, h)

	// Provisioning happens after seeding (step 6, after step 3): a seed
	// failure must never let Provision be reached at all.
	for _, name := range h.Rec.all() {
		if name == "adapter.Provision" {
			t.Error("adapter.Provision was called despite the earlier seed-file failure, want setup to stop before it")
		}
	}
}

func TestRun_PanicDuringExecution_RecoversAndStillTearsDownTheSandbox(t *testing.T) {
	h := newHarness(t)
	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		panic("simulated failure inside execution")
	}
	req := newRequest("panic-in-execution")

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Run let a panic escape rather than recovering and tearing down: %v", r)
			}
		}()
		if _, err := runner.Run(context.Background(), h.Deps, req); err == nil {
			t.Error("Run returned no error for a panicking execution, want the fault to be reported")
		}
	}()

	assertSandboxWasCreatedThenTornDown(t, h)
}

// assertSandboxWasCreatedThenTornDown fails the test unless the recorder
// shows both that a sandbox was actually created for this attempt and that
// it was torn down afterward. Asserting teardown alone would pass trivially
// against an implementation (or a not-yet-written one) that never created
// anything in the first place — the creation half is what makes the
// teardown claim meaningful.
func assertSandboxWasCreatedThenTornDown(t *testing.T, h *harness) {
	t.Helper()
	order := h.Rec.all()

	createIdx, teardownIdx := -1, -1
	for i, name := range order {
		switch name {
		case "workspace.Create":
			if createIdx == -1 {
				createIdx = i
			}
		case "workspace.Teardown":
			if teardownIdx == -1 {
				teardownIdx = i
			}
		}
	}

	if createIdx == -1 {
		t.Fatalf("call order = %v, want it to include workspace.Create — a run that never created a sandbox proves nothing about teardown", order)
	}
	if teardownIdx == -1 {
		t.Errorf("call order = %v, want it to include workspace.Teardown", order)
	} else if teardownIdx < createIdx {
		t.Errorf("workspace.Teardown happened before workspace.Create (order=%v)", order)
	}
}

func TestTeardown_RemovesExactlyWhatTheLedgerRecords(t *testing.T) {
	h := newHarness(t)
	sandboxRoot := t.TempDir()
	subjectDir := filepath.Join(sandboxRoot, "subject")
	if err := os.MkdirAll(subjectDir, 0o755); err != nil {
		t.Fatalf("failed to create subject dir: %v", err)
	}
	seededFile := filepath.Join(subjectDir, "seeded.md")
	if err := os.WriteFile(seededFile, []byte("seeded"), 0o644); err != nil {
		t.Fatalf("failed to write seeded file: %v", err)
	}

	ledger := runner.SetupLedger{
		SandboxRoot: sandboxRoot,
	}

	if err := runner.Teardown(h.Deps, ledger); err != nil {
		t.Fatalf("Teardown returned unexpected error: %v", err)
	}

	if _, err := os.Stat(sandboxRoot); !os.IsNotExist(err) {
		t.Errorf("sandbox root still exists after Teardown, want it removed (err=%v)", err)
	}
}

func TestTeardown_NeverRemovesAFileTheRunDidNotCreate(t *testing.T) {
	h := newHarness(t)

	// A directory that sits alongside the sandbox but was never recorded as
	// created by this run — the negative case: teardown must never reach
	// outside what the ledger actually describes.
	untouchedDir := t.TempDir()
	untouchedFile := filepath.Join(untouchedDir, "not-mine.txt")
	if err := os.WriteFile(untouchedFile, []byte("pre-existing"), 0o644); err != nil {
		t.Fatalf("failed to seed the untouched file: %v", err)
	}

	sandboxRoot := t.TempDir()
	ledger := runner.SetupLedger{SandboxRoot: sandboxRoot}

	if err := runner.Teardown(h.Deps, ledger); err != nil {
		t.Fatalf("Teardown returned unexpected error: %v", err)
	}

	if _, err := os.Stat(untouchedFile); err != nil {
		t.Errorf("the file the run did not create is gone after Teardown: %v", err)
	}
}

func TestTeardown_DeprovisionFails_StillRemovesTheSandboxButSurfacesTheError(t *testing.T) {
	h := newHarness(t)
	h.Adapter.deprovisionErr = errors.New("deprovisioning failed")

	sandboxRoot := t.TempDir()
	subjectDir := filepath.Join(sandboxRoot, "subject")
	if err := os.MkdirAll(subjectDir, 0o755); err != nil {
		t.Fatalf("failed to create subject dir: %v", err)
	}
	seededFile := filepath.Join(subjectDir, "seeded.md")
	if err := os.WriteFile(seededFile, []byte("seeded"), 0o644); err != nil {
		t.Fatalf("failed to write seeded file: %v", err)
	}

	ledger := runner.SetupLedger{SandboxRoot: sandboxRoot}

	// The adapter's own deprovisioning step failed, but that must not be
	// swallowed: it is a real fault, and it must not stop the sandbox this
	// run created from being removed either — the same "teardown always
	// happens" guarantee the panic and provision-failure cases above cover,
	// extended to the adapter's own half of teardown.
	err := runner.Teardown(h.Deps, ledger)
	if err == nil {
		t.Error("Teardown returned no error despite Deprovision failing, want the failure surfaced rather than swallowed")
	}

	if _, statErr := os.Stat(sandboxRoot); !os.IsNotExist(statErr) {
		t.Errorf("sandbox root still exists after Teardown despite the Deprovision failure, want it removed (err=%v)", statErr)
	}
}

func TestSetupLedger_SaveThenLoad_RoundTripsItsContents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "setup.json")

	original := runner.SetupLedger{
		SandboxRoot:  filepath.Join(dir, "sandbox"),
		SeededFiles:  []string{"notes/plan.md"},
		ControlFiles: []string{"registry.json", "groups.json"},
	}

	if err := runner.SaveSetupLedger(path, original); err != nil {
		t.Fatalf("SaveSetupLedger returned unexpected error: %v", err)
	}

	loaded, err := runner.LoadSetupLedger(path)
	if err != nil {
		t.Fatalf("LoadSetupLedger returned unexpected error: %v", err)
	}

	if loaded.SandboxRoot != original.SandboxRoot {
		t.Errorf("loaded.SandboxRoot = %q, want %q", loaded.SandboxRoot, original.SandboxRoot)
	}
	if len(loaded.SeededFiles) != 1 || loaded.SeededFiles[0] != "notes/plan.md" {
		t.Errorf("loaded.SeededFiles = %v, want [notes/plan.md]", loaded.SeededFiles)
	}
	if len(loaded.ControlFiles) != 2 {
		t.Errorf("loaded.ControlFiles = %v, want 2 entries", loaded.ControlFiles)
	}
}
