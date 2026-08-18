package runner_test

// Tests for Stage 15: retention honours assertion verdict.
//
// Stage 15 widens the retain-on-failure failure signal so a run that launches
// cleanly but fails its assertions is treated as a failure for retention
// purposes. The three tasks this file covers:
//
//   T15.1 — A clean-launch assertion failure retains the sandbox, scrubs its
//            sensitive paths, and reports the retained path. The scrub
//            invariant is not exercised by the existing pin tests, which
//            focus on presence/absence of the sandbox and the reported path.
//
//   T15.2 — Pin the cases this stage must not disturb. The existing
//            retention_pin_test.go pins retain-never on a clean pass, retain-
//            always on a clean pass, launch failure under retain-on-failure,
//            and retain-on-failure on a clean pass. The one case that file
//            does not cover is: retain-never removes the sandbox when
//            assertions fail (the verdict must not override retain-never).
//
//   T15.3 — A repeated test with mixed verdicts retains only the failing
//            attempts' sandboxes. Retention is per-attempt; a passing
//            repetition must not be retained just because another repetition
//            of the same test failed.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
	"mosaic-agent-test/internal/runner"
	"mosaic-agent-test/internal/workspace"
)

// TestRetention_OnFailure_AssertionFailure_SensitivePathsAreScrubbed verifies
// that when a sandbox is retained because an assertion failure triggered
// retain-on-failure, its sensitive paths are removed before it is kept on
// disk. A retained sandbox must never contain live harness credential material,
// regardless of how the failure occurred.
//
// This test exercises the Teardown function directly with a real sensitive file
// and AttemptOutcome{Failed: true}, which is the state runner.Run supplies
// after evaluate.Evaluate returns VerdictFail on a clean-launch run.
//
// RED phase note: this test calls Teardown directly with AttemptOutcome{Failed:
// true}. Its correctness is not sensitive to the result.Verdict ==
// domain.VerdictFail widening in runner.Run (runner.go line 154), because it
// bypasses runner.Run entirely and constructs the AttemptOutcome directly. The
// scrub invariant that Teardown must remove sensitive files when retaining was
// already present before Stage 15; this test regresses that invariant rather
// than specifying new behaviour. Reverting line 154 would not cause this test
// to fail.
func TestRetention_OnFailure_AssertionFailure_SensitivePathsAreScrubbed(t *testing.T) {
	// Represent the retained sandbox as a temp directory so the sensitive file
	// physically exists and os.Remove inside scrubSensitive has something to remove.
	sandboxRoot := t.TempDir()
	sensitiveFile := filepath.Join(sandboxRoot, "provisioned-credentials.json")
	if err := os.WriteFile(sensitiveFile, []byte("harness-credential"), 0o600); err != nil {
		t.Fatalf("creating sensitive test file: %v", err)
	}

	ledger := runner.SetupLedger{
		SandboxRoot: sandboxRoot,
		Provisioning: domain.Provisioning{
			Sensitive: []string{sensitiveFile},
		},
	}

	// AttemptOutcome{Failed: true} under RetainOnFailure is exactly what
	// runner.Run produces when the subject launched without error but
	// evaluate.Evaluate returned VerdictFail — the assertion-failure case
	// Stage 15 widens the policy to cover. An empty Deps is safe here: the
	// retain path in Teardown returns before it touches d.Adapter or
	// d.Workspaces.
	outcome, err := runner.Teardown(runner.Deps{}, ledger, runner.AttemptOutcome{
		Policy: domain.RetainOnFailure,
		Failed: true,
	})
	if err != nil {
		t.Fatalf("Teardown returned unexpected error: %v", err)
	}

	if !outcome.Retained {
		t.Error("RetentionOutcome.Retained is false, want true — a failed attempt must be retained under retain-on-failure")
	}
	if outcome.Path != sandboxRoot {
		t.Errorf("RetentionOutcome.Path = %q, want %q — the retained sandbox root must be reported for diagnosis", outcome.Path, sandboxRoot)
	}

	// The sensitive file must be absent after retention: scrubSensitive must
	// have removed it. If it is still present, the scrub step was skipped or
	// the sensitive-paths list was not passed into Teardown.
	if _, statErr := os.Stat(sensitiveFile); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("sensitive file is still present after assertion-failure retention — sensitive paths must be scrubbed before the sandbox is kept on disk, so no live credential material survives")
	}
}

// TestRetention_Never_AssertionFailure_SandboxIsRemoved verifies that
// retain-never removes the sandbox even when the run's assertions failed.
// The failing verdict must not cause retain-never to retain: the policy trumps
// the verdict for both retain-never and retain-always. Only retain-on-failure
// is verdict-sensitive.
//
// RED phase note: this test uses the retain-never policy, which always removes
// the sandbox regardless of verdict. Reverting line 154 in runner.go
// (failed = failed || result.Verdict == domain.VerdictFail) would not cause
// this test to fail, because retain-never's remove behaviour is not
// verdict-sensitive — the sandbox is removed whether or not the failure signal
// is set. The test guards against retain-never being accidentally overridden by
// a verdict, which is a separate invariant from the Stage 15 widening.
func TestRetention_Never_AssertionFailure_SandboxIsRemoved(t *testing.T) {
	h := newHarness(t)
	req := newRequest("retain-never-assertion-fail")
	req.Retention = domain.RetainNever

	// Declare an artifact the stub run will never create, so evaluate.Evaluate
	// produces VerdictFail. The subject launches and completes without error —
	// this is a pure assertion failure, not a launch failure.
	req.Test.Definition.Assertions = domain.Assertions{
		ArtifactCreated: []string{"expected-output.json"},
	}

	// Run must not return an error: the subject launched successfully. Only
	// its assertions failed.
	result, err := runner.Run(context.Background(), h.Deps, req, evaluate.Evaluate)
	if err != nil {
		t.Fatalf("Run returned unexpected error for a clean-launch assertion-failure run: %v", err)
	}

	// Under retain-never, the sandbox must be gone regardless of the verdict.
	// An assertion failure must not override the policy.
	if _, locErr := h.Deps.Workspaces.Locate(req.Key); !errors.Is(locErr, workspace.ErrSandboxNotFound) {
		t.Errorf("Workspaces.Locate after RetainNever+assertion-failure = %v, want ErrSandboxNotFound — retain-never must remove the sandbox even when assertions fail", locErr)
	}

	if result.RetainedSandboxPath != "" {
		t.Errorf("TestResult.RetainedSandboxPath = %q, want empty — retain-never must not report a retained path, regardless of verdict", result.RetainedSandboxPath)
	}
}

// TestRetention_OnFailure_MixedVerdicts_OnlyFailingAttemptSandboxSurvives
// verifies per-attempt retention when a repeated test produces mixed verdicts.
// Each attempt's sandbox is retained or removed independently based on that
// attempt's outcome: a passing attempt must not be retained even when another
// attempt of the same test fails, and the failing attempt must be retained
// even though another attempt of the same test passed.
//
// This models the repeated-test scenario described in Stage 15: one verdict
// per attempt, retention decided per attempt.
//
// RED phase note: removing line 154 in runner.go
// (failed = failed || result.Verdict == domain.VerdictFail) would cause this
// test to fail correctly. Without that line the failing attempt's sandbox
// would not be retained — Workspaces.Locate(failingReq.Key) would return
// ErrSandboxNotFound instead of nil, and failResult.RetainedSandboxPath
// would be empty. This test is therefore the primary RED-phase specification
// for the Stage 15 widening of the retain-on-failure failure signal.
func TestRetention_OnFailure_MixedVerdicts_OnlyFailingAttemptSandboxSurvives(t *testing.T) {
	h := newHarness(t)

	// Attempt 1: passes — no assertions declared, so evaluate.Evaluate returns
	// VerdictPass. Under retain-on-failure this attempt's sandbox must be removed.
	passingReq := runner.Request{
		Key: domain.RunKey{
			RunID:     "run-mixed-rep1",
			TestID:    "mixed-verdict-test",
			RunNumber: 1,
		},
		Test:      newRequest("mixed-verdict-test").Test,
		Settings:  domain.RunSettings{},
		Retention: domain.RetainOnFailure,
	}
	// No assertions on the passing request — evaluate.Evaluate returns VerdictPass.

	passResult, err := runner.Run(context.Background(), h.Deps, passingReq, evaluate.Evaluate)
	if err != nil {
		t.Fatalf("passing attempt returned unexpected error: %v", err)
	}

	// Attempt 2: assertion failure — evaluate.Evaluate returns VerdictFail.
	// Under retain-on-failure this attempt's sandbox must be retained.
	failingReq := runner.Request{
		Key: domain.RunKey{
			RunID:     "run-mixed-rep2",
			TestID:    "mixed-verdict-test",
			RunNumber: 2,
		},
		Test:      newRequest("mixed-verdict-test").Test,
		Settings:  domain.RunSettings{},
		Retention: domain.RetainOnFailure,
	}
	failingReq.Test.Definition.Assertions = domain.Assertions{
		ArtifactCreated: []string{"expected-output.json"},
	}

	failResult, err := runner.Run(context.Background(), h.Deps, failingReq, evaluate.Evaluate)
	if err != nil {
		t.Fatalf("failing attempt returned unexpected error for a clean-launch assertion-failure run: %v", err)
	}

	// The passing attempt's sandbox must have been removed: a passing repetition
	// must not be retained even though another repetition of the same test failed.
	if _, locErr := h.Deps.Workspaces.Locate(passingReq.Key); !errors.Is(locErr, workspace.ErrSandboxNotFound) {
		t.Errorf("Workspaces.Locate for passing attempt = %v, want ErrSandboxNotFound — a passing repetition must not be retained under retain-on-failure even when another repetition of the same test fails", locErr)
	}

	// The passing attempt must not report a retained path. Checking both the
	// disk state (via Locate above) and the reported path field ensures the
	// retention contract is symmetric: the failing side is asserted by both
	// Locate and RetainedSandboxPath; the passing side must be too.
	if passResult.RetainedSandboxPath != "" {
		t.Errorf("passResult.RetainedSandboxPath = %q, want empty — retain-on-failure must not report a retained path for a passing attempt, even when another repetition of the same test fails", passResult.RetainedSandboxPath)
	}

	// The failing attempt's sandbox must be retained: assertion failure is a
	// failure for retain-on-failure purposes.
	if _, locErr := h.Deps.Workspaces.Locate(failingReq.Key); locErr != nil {
		t.Errorf("Workspaces.Locate for failing attempt = %v, want no error — an assertion-failure repetition must be retained under retain-on-failure, independently of whether other repetitions passed", locErr)
	}

	// The failing attempt must report the retained path so the user can find
	// the sandbox for diagnosis.
	if failResult.RetainedSandboxPath == "" {
		t.Error("TestResult.RetainedSandboxPath is empty for the failing attempt, want a non-empty path — the retained sandbox root must be reported so the user can diagnose the assertion failure")
	}
}
