package e2e

// T18.6 (disposition): timeout, early exit and state integrity must each be
// distinguishable end to end from an ordinary assertion failure.
//
//   - Early exit: a single-decision test that stops on the early-exit
//     sentinel must end normally rather than as a fault: no ReasonTimeout,
//     no ReasonStateIntegrity in the run's reasons, and a passing verdict
//     when the assertions that were actually observed hold.
//   - Timeout: a run whose declared timeout elapses before the subject
//     reaches a Result turn must earn domain.VerdictTimeout with
//     domain.ReasonTimeout — not a spawn failure, not an ordinary fail. This
//     was blocked until an implementation session fixed
//     internal/runner.superviseExecution to inspect its own supervisor
//     context's deadline rather than the launcher's always-swallowed error
//     (see Stage-18/PlanProgress.md for the root cause and the fix). The
//     scenario needs no new stand-in wiring beyond a way to make the
//     scripted subject genuinely never reach a Result turn within the
//     declared timeout: standin_test.go's SleepBeforeExit sidecar field
//     blocks the stand-in process itself past the timeout, so the parent
//     context's cancellation kills it exactly as it would a real hung
//     subject (mosaic-common/harness.Run's Process.Kill on ctx.Done()).
//   - State integrity: reaching runstate.Store.Update's LockReclaimed path
//     needs the run-state lock to look, at the moment the subject starts,
//     like one a crashed prior holder left behind. Rather than racing real
//     wall-clock contention against the fixed 30-second staleness threshold
//     (what internal/runstate's own concurrency tests already do, and what
//     T18.6's planning notes flagged as "the slowest and fiddliest of the
//     three"), standin_test.go's SeedDeadLock sidecar field writes a lock
//     naming a PID that can never be alive before the first interception
//     runs — the same "verifiably dead holder" fixture
//     internal/runstate/lock_test.go's own
//     TestUpdate_ReclaimsALockHeldByAVerifiablyDeadHolder uses, reached here
//     through the real end-to-end pipeline instead of runstate's package
//     tests. The first interception's own runstate.Store.Update reclaims it
//     immediately — no wait, and fully deterministic. Only the "excluded
//     from pass-rate aggregation" half of AC18.6's state-integrity claim is
//     demonstrated end to end here — see the doc comment on
//     TestDisposition_CompromisedLock_ExcludedFromPassRateAggregation for a
//     genuine composition-seam gap this session found between the retry
//     logic and the real workspace.Manager, which blocks demonstrating the
//     retry's own outcome until a Coder closes it.

import (
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
)

func TestDisposition_EarlyExit_EndsNormally(t *testing.T) {
	sc := Scenario{
		Dir:       "testdata/e2e/early-exit",
		SuitePath: "early-exit.suite.yaml",
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("Task", "greeter"),
				invoke("Task", "helper"),
			},
		),
	}

	out := RunScenario(t, sc)

	assertSingleTestVerdict(t, out, "single-decision", domain.VerdictPass, cli.ExitSuccess)

	if len(out.Result.Tests) != 1 || len(out.Result.Tests[0].Runs) != 1 {
		t.Fatalf("Result.Tests = %+v, want exactly one test with exactly one run\nstdout: %s", out.Result.Tests, out.Stdout)
	}
	run := out.Result.Tests[0].Runs[0]
	for _, reason := range run.Reasons {
		if reason == "TIMEOUT" || reason == "STATE_INTEGRITY" {
			t.Errorf("Runs[0].Reasons contains %q, want an early exit to end normally with no such reason: %+v\nstdout: %s",
				reason, run.Reasons, out.Stdout)
		}
	}
}

// TestDisposition_DeclaredTimeoutElapses_ReportsDistinctTimeoutVerdict covers
// a run whose scripted subject never reaches a Result turn within its
// declared timeout: the sleep the sidecar scripts (2s) is well past the
// declared timeout (200ms), so the parent process's context cancellation
// kills the stand-in before it ever produces a result envelope — the same
// way a real, genuinely hung subject would be ended.
func TestDisposition_DeclaredTimeoutElapses_ReportsDistinctTimeoutVerdict(t *testing.T) {
	sc := Scenario{
		Dir:             "testdata/e2e/timeout",
		SuitePath:       "timeout.suite.yaml",
		Script:          fakeOptionsFor(domain.HarnessCapabilities{SupportsDirectSubstitution: true}, nil),
		SleepBeforeExit: "2s",
	}

	out := RunScenario(t, sc)

	// A TIMEOUT run's own verdict is distinct from FAIL (domain.evaluate.go
	// never lets timedOut collapse into the ordinary reasons-based FAIL
	// branch), but a single-repetition aggregate that is not a pass reads as
	// FAIL — TIMEOUT is a per-run distinction the aggregate and the exit
	// code do not separately carry; the run-level verdict and reason below
	// are where "distinguishable from an ordinary assertion failure" is
	// actually demonstrated.
	if out.ExitCode != cli.ExitTestsFailed {
		t.Fatalf("RunScenario: exit code = %d, want cli.ExitTestsFailed (%d)\nstdout: %s\nstderr: %s",
			out.ExitCode, cli.ExitTestsFailed, out.Stdout, out.Stderr)
	}
	if len(out.Result.Tests) != 1 || out.Result.Tests[0].TestID != "never-finishes" {
		t.Fatalf("Result.Tests = %+v, want exactly one test named %q\nstdout: %s", out.Result.Tests, "never-finishes", out.Stdout)
	}
	if len(out.Result.Tests[0].Runs) != 1 {
		t.Fatalf("Result.Tests = %+v, want exactly one test with exactly one run\nstdout: %s", out.Result.Tests, out.Stdout)
	}
	run := out.Result.Tests[0].Runs[0]
	if run.Verdict != string(domain.VerdictTimeout) {
		t.Errorf("Runs[0].Verdict = %q, want %q\nstdout: %s", run.Verdict, domain.VerdictTimeout, out.Stdout)
	}
	wantReason := false
	for _, reason := range run.Reasons {
		if reason == "TIMEOUT" {
			wantReason = true
		}
		if reason == "STATE_INTEGRITY" || reason == "ASSERTION" {
			t.Errorf("Runs[0].Reasons contains %q, want only TIMEOUT for a run that never reached a Result turn: %+v\nstdout: %s",
				reason, run.Reasons, out.Stdout)
		}
	}
	if !wantReason {
		t.Errorf("Runs[0].Reasons = %+v, want it to contain TIMEOUT\nstdout: %s", run.Reasons, out.Stdout)
	}
}

// TestDisposition_CompromisedLock_ExcludedFromPassRateAggregation covers a
// run whose sandbox starts with a run-state lock naming a holder that
// cannot be alive: the first interception's own runstate.Store.Update
// reclaims it and records domain.RunEventLockReclaimed, which
// internal/evaluate.Evaluate turns into domain.ReasonStateIntegrity and
// internal/evaluate.NeedsRetry reads as "retry this, do not count it" —
// internal/suite.Suite.runRepetition then automatically attempts the one
// retry internal/suite.StateIntegrityRetries affords.
//
// Only half of AC18.6's state-integrity claim is demonstrable end to end as
// currently wired, and this test is deliberately scoped to that half: the
// compromised attempt is excluded from the pass-rate denominator
// (Aggregate.Excluded == 1), which is the "not counted as a subject
// regression" half of the criterion. What this test does not assert on is
// the retry's own outcome, because driving that retry through the real
// production workspace.Manager (not the stub internal/suite's own package
// tests use) surfaces a genuine seam-level gap: runRepetition reuses the
// exact same domain.RunKey for every attempt of one repetition (see
// suite.go's runID, which has no attempt index), but
// workspace.Manager.Create permanently refuses a second Create for a key it
// has already created — even after Teardown — by design (see
// internal/workspace/workspace_test.go's own
// TestCreate_RefusesASecondCreateForARunKeyThisManagerAlreadyCreated). So
// the retry's own runner.Run call fails at setup with
// workspace.ErrSandboxExists before a subject is ever spawned a second
// time, and — because that error path returns a zero-value
// domain.RunEvidence rather than one built by runner.BuildEvidence — the
// retry's evaluation sees a test.Definition with no declared assertions to
// fail, yielding an untrustworthy pass with zero reasons instead of a
// second, genuine state-integrity fault or a genuine recovery. This is
// exactly the kind of composition seam this stage exists to surface: the
// retry logic and the workspace manager are each correct against their own
// package's tests, and still fail to compose. Fixing it (giving each
// attempt within one repetition its own RunKey, or relaxing
// workspace.Manager's one-shot-per-key guarantee for this exact case) is
// implementation work, out of TestWriter's scope — recorded in
// Stage-18/PlanProgress.md for a Coder session. A successor TestWriter
// should extend this test to also assert on the retry's own outcome once
// that gap is closed.
func TestDisposition_CompromisedLock_ExcludedFromPassRateAggregation(t *testing.T) {
	sc := Scenario{
		Dir:       "testdata/e2e/state-integrity",
		SuitePath: "state-integrity.suite.yaml",
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("Task", "greeter"),
				finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
		SeedDeadLock: true,
	}

	out := RunScenario(t, sc)

	if len(out.Result.Tests) != 1 || out.Result.Tests[0].TestID != "lock-compromised" {
		t.Fatalf("Result.Tests = %+v, want exactly one test named %q\nstdout: %s", out.Result.Tests, "lock-compromised", out.Stdout)
	}
	agg := out.Result.Tests[0].Aggregate
	if agg.Excluded != 1 {
		t.Errorf("Tests[0].Aggregate.Excluded = %d, want 1 — the compromised attempt must be excluded from the pass-rate denominator, not counted as a subject regression\nstdout: %s",
			agg.Excluded, out.Stdout)
	}
}
