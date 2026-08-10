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

	if _, err := runner.Teardown(h.Deps, ledger, runner.AttemptOutcome{Policy: domain.RetainNever}); err != nil {
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

	if _, err := runner.Teardown(h.Deps, ledger, runner.AttemptOutcome{Policy: domain.RetainNever}); err != nil {
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
	_, err := runner.Teardown(h.Deps, ledger, runner.AttemptOutcome{Policy: domain.RetainNever})
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

// ---------------------------------------------------------------------------
// Sandbox retention (Stage 7): the three policies honoured on every exit
// path a run's Teardown can be reached from, and the credential-scrub
// obligation a retained sandbox carries.
//
// Teardown takes the retention policy and the attempt's failure state as an
// explicit AttemptOutcome parameter, not as fields on SetupLedger: the
// ledger is persisted to disk at the end of setup, before any outcome
// exists, so a Retention or AttemptFailed field on it would be written
// permanently stale inside the very sandbox a diagnosis reads. Run supplies
// AttemptOutcome{Policy: req.Retention, Failed: ...} at each of its four
// Teardown call sites.
// ---------------------------------------------------------------------------

func TestRun_RetainAlways_KeepsTheSandboxOnDisk_AfterNormalCompletion(t *testing.T) {
	h := newHarness(t)
	req := newRequest("retain-always-normal")
	req.Retention = domain.RetainAlways

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if _, err := h.Deps.Workspaces.Locate(req.Key); err != nil {
		t.Errorf("Locate after Run = %v, want the sandbox still found — RetainAlways must keep it", err)
	}
	for _, name := range h.Rec.all() {
		if name == "adapter.Deprovision" {
			t.Error("adapter.Deprovision was called despite RetainAlways; retention must skip deprovision, which would remove the artifacts a diagnosis needs")
		}
	}
}

func TestRun_RetainAlways_KeepsTheSandboxOnDisk_OnSpawnPlanFailure(t *testing.T) {
	h := newHarness(t)
	h.Adapter.planErr = errors.New("spawn plan could not be built")
	req := newRequest("retain-always-spawn-fail")
	req.Retention = domain.RetainAlways

	if _, err := runner.Run(context.Background(), h.Deps, req); err == nil {
		t.Fatal("Run returned no error for a spawn-plan failure")
	}

	if _, err := h.Deps.Workspaces.Locate(req.Key); err != nil {
		t.Errorf("Locate after a spawn-plan failure = %v, want the sandbox retained under RetainAlways", err)
	}
}

func TestRun_RetainAlways_KeepsTheSandboxOnDisk_OnProvisionFailure(t *testing.T) {
	h := newHarness(t)
	h.Adapter.provisionErr = errors.New("provisioning failed")
	req := newRequest("retain-always-provision-fail")
	req.Retention = domain.RetainAlways

	if _, err := runner.Run(context.Background(), h.Deps, req); err == nil {
		t.Fatal("Run returned no error for a provisioning failure")
	}

	if _, err := h.Deps.Workspaces.Locate(req.Key); err != nil {
		t.Errorf("Locate after a provisioning failure = %v, want the sandbox retained under RetainAlways", err)
	}
}

func TestRun_RetainAlways_KeepsTheSandboxOnDisk_OnPanicRecovery(t *testing.T) {
	h := newHarness(t)
	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		panic("simulated failure inside execution")
	}
	req := newRequest("retain-always-panic")
	req.Retention = domain.RetainAlways

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Run let a panic escape rather than recovering and tearing down: %v", r)
			}
		}()
		if _, err := runner.Run(context.Background(), h.Deps, req); err == nil {
			t.Error("Run returned no error for a panicking execution")
		}
	}()

	if _, err := h.Deps.Workspaces.Locate(req.Key); err != nil {
		t.Errorf("Locate after a recovered panic = %v, want the sandbox retained under RetainAlways", err)
	}
}

func TestRun_RetainOnFailure_TearsDownTheSandbox_WhenTheAttemptSucceeds(t *testing.T) {
	h := newHarness(t)
	req := newRequest("retain-on-failure-success")
	req.Retention = domain.RetainOnFailure

	if _, err := runner.Run(context.Background(), h.Deps, req); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	if _, err := h.Deps.Workspaces.Locate(req.Key); !errors.Is(err, workspace.ErrSandboxNotFound) {
		t.Errorf("Locate after a successful RetainOnFailure run = %v, want %v — a successful attempt must not be retained", err, workspace.ErrSandboxNotFound)
	}
}

func TestRun_RetainOnFailure_RetainsTheSandbox_WhenTheAttemptFails(t *testing.T) {
	h := newHarness(t)
	h.Adapter.planErr = errors.New("spawn plan could not be built")
	req := newRequest("retain-on-failure-spawn-fail")
	req.Retention = domain.RetainOnFailure

	if _, err := runner.Run(context.Background(), h.Deps, req); err == nil {
		t.Fatal("Run returned no error for a spawn-plan failure")
	}

	if _, err := h.Deps.Workspaces.Locate(req.Key); err != nil {
		t.Errorf("Locate after a failed RetainOnFailure run = %v, want the sandbox retained", err)
	}
}

// TestRun_RetainOnFailure_RetainsTheSandbox_OnProvisionFailure exercises the
// Teardown call site reached after a provisioning failure — a distinct exit
// path from the spawn-plan-failure one already covered above, and one no
// compiler-checked structural guard pins to the correct AttemptOutcome.Failed
// value (see ContractsDesign.md's "no structural guard over the call sites"
// note).
func TestRun_RetainOnFailure_RetainsTheSandbox_OnProvisionFailure(t *testing.T) {
	h := newHarness(t)
	h.Adapter.provisionErr = errors.New("provisioning failed")
	req := newRequest("retain-on-failure-provision-fail")
	req.Retention = domain.RetainOnFailure

	if _, err := runner.Run(context.Background(), h.Deps, req); err == nil {
		t.Fatal("Run returned no error for a provisioning failure")
	}

	if _, err := h.Deps.Workspaces.Locate(req.Key); err != nil {
		t.Errorf("Locate after a provisioning failure = %v, want the sandbox retained under RetainOnFailure", err)
	}
}

// TestRun_RetainOnFailure_RetainsTheSandbox_OnSeedFileResolutionFailure
// exercises the Teardown call site reached after a seed-file resolution
// failure — the third of the four distinct exit paths Run's Teardown is
// called from.
func TestRun_RetainOnFailure_RetainsTheSandbox_OnSeedFileResolutionFailure(t *testing.T) {
	h := newHarness(t)
	req := newRequest("retain-on-failure-seed-resolution-error")
	req.Retention = domain.RetainOnFailure
	req.Test.Definition.SeedFiles = []domain.SeedFile{
		{Path: "notes/plan.md", Ref: "does-not-exist.json"},
	}

	if _, err := runner.Run(context.Background(), h.Deps, req); err == nil {
		t.Fatal("Run returned no error for a seed-file resolution failure")
	}

	if _, err := h.Deps.Workspaces.Locate(req.Key); err != nil {
		t.Errorf("Locate after a seed-file resolution failure = %v, want the sandbox retained under RetainOnFailure", err)
	}
}

// TestRun_RetainOnFailure_RetainsTheSandbox_OnPanicRecovery exercises the
// Teardown call site reached after a recovered panic — the fourth and final
// distinct exit path, completing RetainOnFailure's coverage of the same
// four-exit-path matrix RetainAlways already has above.
func TestRun_RetainOnFailure_RetainsTheSandbox_OnPanicRecovery(t *testing.T) {
	h := newHarness(t)
	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		panic("simulated failure inside execution")
	}
	req := newRequest("retain-on-failure-panic")
	req.Retention = domain.RetainOnFailure

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Run let a panic escape rather than recovering and tearing down: %v", r)
			}
		}()
		if _, err := runner.Run(context.Background(), h.Deps, req); err == nil {
			t.Error("Run returned no error for a panicking execution")
		}
	}()

	if _, err := h.Deps.Workspaces.Locate(req.Key); err != nil {
		t.Errorf("Locate after a recovered panic = %v, want the sandbox retained under RetainOnFailure", err)
	}
}

// TestRun_RetainAlways_EvidenceCarriesTheRetainedSandboxPath is Stage 7's
// bridge to AC7.3: the report can only print a path Run actually returns.
func TestRun_RetainAlways_EvidenceCarriesTheRetainedSandboxPath(t *testing.T) {
	h := newHarness(t)
	req := newRequest("retain-always-evidence-path")
	req.Retention = domain.RetainAlways

	evidence, err := runner.Run(context.Background(), h.Deps, req)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	sb, locateErr := h.Deps.Workspaces.Locate(req.Key)
	if locateErr != nil {
		t.Fatalf("sandbox was not retained (Locate returned %v); the path assertion below is meaningless without it", locateErr)
	}

	if evidence.RetainedSandboxPath != sb.Root {
		t.Errorf("evidence.RetainedSandboxPath = %q, want %q — a retention feature whose path the report cannot show is not a feature", evidence.RetainedSandboxPath, sb.Root)
	}
}

// TestTeardown_RetainAlways_SkipsDeprovisionAndKeepsTheSandbox is the
// Teardown-direct counterpart to the Run-level tests above: it isolates the
// policy decision from the rest of the lifecycle.
func TestTeardown_RetainAlways_SkipsDeprovisionAndKeepsTheSandbox(t *testing.T) {
	h := newHarness(t)
	sandboxRoot := t.TempDir()
	subjectDir := filepath.Join(sandboxRoot, "subject")
	if err := os.MkdirAll(subjectDir, 0o755); err != nil {
		t.Fatalf("failed to create subject dir: %v", err)
	}

	ledger := runner.SetupLedger{SandboxRoot: sandboxRoot}

	outcome, err := runner.Teardown(h.Deps, ledger, runner.AttemptOutcome{Policy: domain.RetainAlways})
	if err != nil {
		t.Fatalf("Teardown returned unexpected error: %v", err)
	}

	if !outcome.Retained {
		t.Error("RetentionOutcome.Retained = false, want true under RetainAlways")
	}
	if outcome.Path != sandboxRoot {
		t.Errorf("RetentionOutcome.Path = %q, want %q", outcome.Path, sandboxRoot)
	}
	if _, statErr := os.Stat(sandboxRoot); statErr != nil {
		t.Errorf("sandbox root is gone after Teardown under RetainAlways: %v", statErr)
	}
	for _, name := range h.Rec.all() {
		if name == "adapter.Deprovision" {
			t.Error("adapter.Deprovision was called under RetainAlways, want it skipped")
		}
	}
}

// TestTeardown_RetainAlways_ScrubsSensitivePathsBeforeRetaining covers the
// forward obligation Stage 8 depends on: whatever Provisioning.Sensitive
// names is gone before a retained sandbox is left on disk, and everything
// else in the sandbox survives untouched.
func TestTeardown_RetainAlways_ScrubsSensitivePathsBeforeRetaining(t *testing.T) {
	h := newHarness(t)
	sandboxRoot := t.TempDir()
	subjectDir := filepath.Join(sandboxRoot, "subject")
	credDir := filepath.Join(subjectDir, ".config")
	if err := os.MkdirAll(credDir, 0o755); err != nil {
		t.Fatalf("failed to create credential dir: %v", err)
	}
	credFile := filepath.Join(credDir, "credentials.json")
	if err := os.WriteFile(credFile, []byte(`{"token":"secret"}`), 0o600); err != nil {
		t.Fatalf("failed to write credential file: %v", err)
	}
	harmlessFile := filepath.Join(subjectDir, "notes.md")
	if err := os.WriteFile(harmlessFile, []byte("notes"), 0o644); err != nil {
		t.Fatalf("failed to write harmless file: %v", err)
	}

	ledger := runner.SetupLedger{
		SandboxRoot: sandboxRoot,
		Provisioning: domain.Provisioning{
			Sensitive: []string{credFile},
		},
	}

	outcome, err := runner.Teardown(h.Deps, ledger, runner.AttemptOutcome{Policy: domain.RetainAlways})
	if err != nil {
		t.Fatalf("Teardown returned unexpected error: %v", err)
	}

	if _, statErr := os.Stat(credFile); !os.IsNotExist(statErr) {
		t.Errorf("credential file still exists after a retained Teardown (err=%v), want it scrubbed", statErr)
	}
	if _, statErr := os.Stat(harmlessFile); statErr != nil {
		t.Errorf("a non-sensitive file was removed by the scrub step: %v", statErr)
	}

	found := false
	for _, p := range outcome.Scrubbed {
		if p == credFile {
			found = true
		}
	}
	if !found {
		t.Errorf("RetentionOutcome.Scrubbed = %v, want it to list %q", outcome.Scrubbed, credFile)
	}
}

func TestTeardown_RetainOnFailure_AttemptFailed_RetainsTheSandbox(t *testing.T) {
	h := newHarness(t)
	sandboxRoot := t.TempDir()
	if err := os.MkdirAll(sandboxRoot, 0o755); err != nil {
		t.Fatalf("failed to create sandbox root: %v", err)
	}

	ledger := runner.SetupLedger{SandboxRoot: sandboxRoot}

	outcome, err := runner.Teardown(h.Deps, ledger, runner.AttemptOutcome{Policy: domain.RetainOnFailure, Failed: true})
	if err != nil {
		t.Fatalf("Teardown returned unexpected error: %v", err)
	}
	if !outcome.Retained {
		t.Error("RetentionOutcome.Retained = false, want true — RetainOnFailure must behave as RetainAlways when the attempt failed")
	}
	if _, statErr := os.Stat(sandboxRoot); statErr != nil {
		t.Errorf("sandbox root is gone after a failed RetainOnFailure attempt: %v", statErr)
	}
	for _, name := range h.Rec.all() {
		if name == "adapter.Deprovision" {
			t.Error("adapter.Deprovision was called under a failed RetainOnFailure attempt, want it skipped — RetainOnFailure behaves as RetainAlways when the attempt failed")
		}
	}
}

// TestTeardown_RetainOnFailure_AttemptFailed_ScrubsSensitivePathsBeforeRetaining
// mirrors TestTeardown_RetainAlways_ScrubsSensitivePathsBeforeRetaining for
// the other way a sandbox can be retained: RetainOnFailure behaving as
// RetainAlways once the attempt has failed. The scrub obligation
// ("unconditional whenever a sandbox is left on disk") applies here just as
// much as under RetainAlways.
func TestTeardown_RetainOnFailure_AttemptFailed_ScrubsSensitivePathsBeforeRetaining(t *testing.T) {
	h := newHarness(t)
	sandboxRoot := t.TempDir()
	subjectDir := filepath.Join(sandboxRoot, "subject")
	credDir := filepath.Join(subjectDir, ".config")
	if err := os.MkdirAll(credDir, 0o755); err != nil {
		t.Fatalf("failed to create credential dir: %v", err)
	}
	credFile := filepath.Join(credDir, "credentials.json")
	if err := os.WriteFile(credFile, []byte(`{"token":"secret"}`), 0o600); err != nil {
		t.Fatalf("failed to write credential file: %v", err)
	}
	harmlessFile := filepath.Join(subjectDir, "notes.md")
	if err := os.WriteFile(harmlessFile, []byte("notes"), 0o644); err != nil {
		t.Fatalf("failed to write harmless file: %v", err)
	}

	ledger := runner.SetupLedger{
		SandboxRoot: sandboxRoot,
		Provisioning: domain.Provisioning{
			Sensitive: []string{credFile},
		},
	}

	outcome, err := runner.Teardown(h.Deps, ledger, runner.AttemptOutcome{Policy: domain.RetainOnFailure, Failed: true})
	if err != nil {
		t.Fatalf("Teardown returned unexpected error: %v", err)
	}

	if _, statErr := os.Stat(credFile); !os.IsNotExist(statErr) {
		t.Errorf("credential file still exists after a failed RetainOnFailure Teardown (err=%v), want it scrubbed", statErr)
	}
	if _, statErr := os.Stat(harmlessFile); statErr != nil {
		t.Errorf("a non-sensitive file was removed by the scrub step: %v", statErr)
	}

	found := false
	for _, p := range outcome.Scrubbed {
		if p == credFile {
			found = true
		}
	}
	if !found {
		t.Errorf("RetentionOutcome.Scrubbed = %v, want it to list %q", outcome.Scrubbed, credFile)
	}
}

func TestTeardown_RetainOnFailure_AttemptSucceeded_RemovesTheSandbox(t *testing.T) {
	h := newHarness(t)
	sandboxRoot := t.TempDir()
	if err := os.MkdirAll(sandboxRoot, 0o755); err != nil {
		t.Fatalf("failed to create sandbox root: %v", err)
	}

	ledger := runner.SetupLedger{SandboxRoot: sandboxRoot}

	outcome, err := runner.Teardown(h.Deps, ledger, runner.AttemptOutcome{Policy: domain.RetainOnFailure, Failed: false})
	if err != nil {
		t.Fatalf("Teardown returned unexpected error: %v", err)
	}
	if outcome.Retained {
		t.Error("RetentionOutcome.Retained = true, want false — RetainOnFailure must behave as RetainNever when the attempt succeeded")
	}
	if _, statErr := os.Stat(sandboxRoot); !os.IsNotExist(statErr) {
		t.Errorf("sandbox root still exists after a successful RetainOnFailure attempt (err=%v), want it removed", statErr)
	}
}
