package runner_test

// Retention outcome regression guards for Stage 4.
//
// Stage 4 moves the normal-path teardown behind the TestRunner port (so the
// suite supplies an evaluator and the runner triggers teardown after calling
// it). The retention DECISION must remain identical to today's: retain-always
// always retains, retain-never always removes, and retain-on-failure retains
// exactly when the attempt failed. These tests pin those three outcomes so any
// regression surfaces immediately.
//
// runner.Run accepts a single named eval domain.AttemptEvaluator parameter.
// Tests that do not exercise evaluation pass nil; tests that need to drive a
// specific verdict (e.g. assertion failure) pass evaluate.Evaluate directly.

import (
	"context"
	"errors"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
	"mosaic-agent-test/internal/runner"
	"mosaic-agent-test/internal/workspace"
)

// TestRetention_Always_SandboxIsKeptAfterNormalCompletion pins that
// RetainAlways retains the sandbox regardless of outcome, and that
// RetainedSandboxPath is non-empty so the report can surface the location.
func TestRetention_Always_SandboxIsKeptAfterNormalCompletion(t *testing.T) {
	h := newHarness(t)
	req := newRequest("pin-retain-always-pass")
	req.Retention = domain.RetainAlways

	result, err := runner.Run(context.Background(), h.Deps, req, nil)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Sandbox must remain on disk.
	if _, locErr := h.Deps.Workspaces.Locate(req.Key); locErr != nil {
		t.Errorf("Workspaces.Locate after RetainAlways run returned %v, want no error — the sandbox must stay on disk", locErr)
	}

	// RetainedSandboxPath must be set so the report can print the path.
	if result.RetainedSandboxPath == "" {
		t.Error("TestResult.RetainedSandboxPath is empty for a RetainAlways run, want a non-empty path so the report can tell the user where the sandbox was kept")
	}
}

// TestRetention_Never_SandboxIsRemovedAfterNormalCompletion pins that
// RetainNever removes the sandbox and produces an empty RetainedSandboxPath.
func TestRetention_Never_SandboxIsRemovedAfterNormalCompletion(t *testing.T) {
	h := newHarness(t)
	req := newRequest("pin-retain-never-pass")
	req.Retention = domain.RetainNever

	result, err := runner.Run(context.Background(), h.Deps, req, nil)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Sandbox must not remain on disk.
	if _, locErr := h.Deps.Workspaces.Locate(req.Key); !errors.Is(locErr, workspace.ErrSandboxNotFound) {
		t.Errorf("Workspaces.Locate after RetainNever run returned %v, want ErrSandboxNotFound — the sandbox must be removed", locErr)
	}

	if result.RetainedSandboxPath != "" {
		t.Errorf("TestResult.RetainedSandboxPath = %q, want empty for a RetainNever run", result.RetainedSandboxPath)
	}
}

// TestRetention_OnFailure_LaunchError_SandboxIsKept pins that
// RetainOnFailure retains the sandbox when the subject failed to launch —
// this is the primary trigger that must not be removed or weakened by
// the Stage 4 refactor.
func TestRetention_OnFailure_LaunchError_SandboxIsKept(t *testing.T) {
	h := newHarness(t)
	h.Launcher.launchFn = func(ctx context.Context, plan domain.SpawnPlan) (domain.SubjectResult, error) {
		return domain.SubjectResult{Disposition: domain.DispositionSpawnFailed},
			errors.New("subject could not be started: executable not found in PATH")
	}
	req := newRequest("pin-retain-on-failure-launch-err")
	req.Retention = domain.RetainOnFailure

	// Run returns the launch error, but the sandbox must be kept for diagnosis.
	result, runErr := runner.Run(context.Background(), h.Deps, req, nil)
	if runErr == nil {
		t.Fatal("Run returned no error for a launch failure, want the launch error surfaced")
	}

	// Sandbox must remain on disk.
	if _, locErr := h.Deps.Workspaces.Locate(req.Key); locErr != nil {
		t.Errorf("Workspaces.Locate after RetainOnFailure+launch-error returned %v, want no error — the sandbox must be retained when the attempt failed to launch", locErr)
	}

	// RetainedSandboxPath must be set on the returned result.
	if result.RetainedSandboxPath == "" {
		t.Error("TestResult.RetainedSandboxPath is empty after a launch-error retention, want a non-empty path so the report can surface where the sandbox was kept")
	}
}

// TestRetention_OnFailure_CleanLaunch_SandboxIsRemoved pins that
// RetainOnFailure does NOT retain when the subject launched and completed
// without a launch error. With no assertions declared, the result is
// VerdictPass, and no retain should occur. This baseline must remain true
// after Stage 4 widens the failure signal to include assertion failures.
func TestRetention_OnFailure_CleanLaunch_SandboxIsRemoved(t *testing.T) {
	h := newHarness(t)
	req := newRequest("pin-retain-on-failure-clean")
	req.Retention = domain.RetainOnFailure

	// No assertions declared → evaluate.Evaluate returns VerdictPass.
	result, err := runner.Run(context.Background(), h.Deps, req, nil)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Sandbox must not remain on disk.
	if _, locErr := h.Deps.Workspaces.Locate(req.Key); !errors.Is(locErr, workspace.ErrSandboxNotFound) {
		t.Errorf("Workspaces.Locate after RetainOnFailure+clean-launch returned %v, want ErrSandboxNotFound — a successful run must not be retained", locErr)
	}

	if result.RetainedSandboxPath != "" {
		t.Errorf("TestResult.RetainedSandboxPath = %q, want empty for a clean-launch RetainOnFailure run", result.RetainedSandboxPath)
	}
}

// TestRetention_Never_DeprovisionIsCalledForHarnessCleanup pins that
// RetainNever calls Deprovision on the adapter so harness-specific cleanup
// (hook files, credential material) is completed — not merely the sandbox
// directory removed. This invariant must survive Stage 4's sequencing change.
func TestRetention_Never_DeprovisionIsCalledForHarnessCleanup(t *testing.T) {
	h := newHarness(t)
	req := newRequest("pin-retain-never-deprovision")
	req.Retention = domain.RetainNever

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	calls := h.Rec.all()
	found := false
	for _, c := range calls {
		if c == "adapter.Deprovision" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("call order = %v, want adapter.Deprovision to appear — RetainNever must call Deprovision for harness-specific cleanup", calls)
	}
}

// TestRetention_OnFailure_AssertionFailure_SandboxIsKept pins that
// RetainOnFailure retains the sandbox when the subject launched and completed
// cleanly but its assertions failed. Stage 4 widens the failure signal from
// "launch error only" to "launch error OR assertion failure via VerdictFail":
// this test is the pin that catches any omission or reversal of that widening.
//
// Stage 4 widened Failed to include result.Verdict == VerdictFail, so the
// runner retains the sandbox whenever assertions fail, not only on a launch
// error. evaluate.Evaluate is passed as the evaluator so the runner observes
// the VerdictFail produced by the declared but missing ArtifactCreated
// assertion, setting Failed = true and triggering retention.
func TestRetention_OnFailure_AssertionFailure_SandboxIsKept(t *testing.T) {
	h := newHarness(t)
	req := newRequest("pin-retain-on-failure-assertion-fail")
	req.Retention = domain.RetainOnFailure

	// Declare an artifact the stub run will never create, so evaluate.Evaluate
	// returns VerdictFail once the Stage 4 evaluator is wired in. The stub
	// launcher completes without error — this is a pure assertion failure, not
	// a launch failure.
	req.Test.Definition.Assertions = domain.Assertions{
		ArtifactCreated: []string{"expected-output.json"},
	}

	// Run must not return an error: the subject launched and completed; only
	// its assertions failed. Pass evaluate.Evaluate as the evaluator so the
	// runner sees a VerdictFail and widens Failed to include assertion failures.
	result, err := runner.Run(context.Background(), h.Deps, req, evaluate.Evaluate)
	if err != nil {
		t.Fatalf("Run returned unexpected error for a clean-launch assertion-failure run: %v", err)
	}

	// Sandbox must remain on disk: RetainOnFailure must retain when assertions
	// fail, not only when the launch errors. If Locate returns ErrSandboxNotFound,
	// it means Failed was derived from the launch error alone — the pre-Stage-4
	// behaviour this pin test exists to guard against.
	if _, locErr := h.Deps.Workspaces.Locate(req.Key); locErr != nil {
		t.Errorf("Workspaces.Locate after RetainOnFailure+assertion-failure returned %v, want no error — the sandbox must be retained when assertions fail, not only when the launch errors", locErr)
	}

	// RetainedSandboxPath must be set so the report can surface the location.
	if result.RetainedSandboxPath == "" {
		t.Error("TestResult.RetainedSandboxPath is empty after an assertion-failure retention, want a non-empty path so the report can tell the user where the sandbox was kept")
	}
}

// TestRetention_Always_DeprovisionIsSkipped pins that RetainAlways does NOT
// call Deprovision, keeping the sandbox fully intact for diagnosis. This
// must hold after Stage 4 moves teardown behind the evaluator callback.
func TestRetention_Always_DeprovisionIsSkipped(t *testing.T) {
	h := newHarness(t)
	req := newRequest("pin-retain-always-no-deprovision")
	req.Retention = domain.RetainAlways

	if _, err := runner.Run(context.Background(), h.Deps, req, nil); err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	for _, c := range h.Rec.all() {
		if c == "adapter.Deprovision" {
			t.Errorf("adapter.Deprovision was called for a RetainAlways run (call order = %v), want it skipped — a retained sandbox must not be deprovisioned", h.Rec.all())
			break
		}
	}
}
