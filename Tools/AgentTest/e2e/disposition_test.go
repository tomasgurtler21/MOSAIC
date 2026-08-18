package e2e

// Disposition end-to-end tests: timeout, early exit and state integrity must
// each be distinguishable from an ordinary assertion failure.
//
//   - Early exit: a single-decision test that stops on the early-exit
//     sentinel must end normally rather than as a fault.
//   - Timeout: a run whose declared timeout elapses before the subject
//     reaches a Result turn must earn domain.VerdictTimeout with
//     domain.ReasonTimeout.
//   - State integrity: a sandbox whose run-state lock is compromised before
//     the first interception runs must be retried once. Two tests cover the
//     two possible outcomes:
//       - Always-faulting: dead lock seeded on every spawn → two excluded
//         attempts, infrastructure-failure aggregate, FAIL verdict.
//       - Recovering: dead lock seeded only on the first spawn → one excluded
//         attempt, one counted attempt, aggregate PASS.

import (
	"strings"
	"testing"
	"time"

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
				invoke("dispatch", "greeter"),
				invoke("dispatch", "helper"),
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
// cannot be alive: the first interception's runstate.Store.Update reclaims
// it and records domain.RunEventLockReclaimed, which evaluate.Evaluate turns
// into ReasonStateIntegrity and evaluate.NeedsRetry reads as "retry this,
// do not count it". The dead lock is seeded on every spawn, so both the
// original attempt and the retry fault on state integrity.
//
// The composed behavior this test asserts: two excluded attempts, an
// infrastructure-failure aggregate, no workspace.ErrSandboxExists anywhere
// in the output (the retry must reach the subject via its own fresh sandbox
// rather than being blocked at workspace creation).
//
// This is a RED test until I14.1 gives each attempt its own RunKey: without
// that fix, runRepetition reuses the same RunKey for the retry and
// workspace.Manager.Create refuses it with ErrSandboxExists — the subject
// is never re-spawned, and Excluded stays at 1 rather than 2.
func TestDisposition_CompromisedLock_ExcludedFromPassRateAggregation(t *testing.T) {
	sc := Scenario{
		Dir:       "testdata/e2e/state-integrity",
		SuitePath: "state-integrity.suite.yaml",
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("dispatch", "greeter"),
				finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
		SeedDeadLock: true,
		// A non-advancing clock guarantees that defaultRunID (which is used
		// when Options.RunID is nil) produces the same id on both attempts of
		// one repetition, because the timestamp component never changes and the
		// attempt index is not yet folded into the suffix (pre-I14.1). Without
		// this, the two attempts may straddle a real second boundary and receive
		// different run ids by accident of wall-clock timing — the test would
		// then pass for the wrong reason (sandbox creation succeeds because the
		// ids happen to differ, not because I14.1 has been implemented).
		Clock: frozenClock{t: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)},
	}

	out := RunScenario(t, sc)

	if len(out.Result.Tests) != 1 || out.Result.Tests[0].TestID != "lock-compromised" {
		t.Fatalf("Result.Tests = %+v, want exactly one test named %q\nstdout: %s", out.Result.Tests, "lock-compromised", out.Stdout)
	}
	agg := out.Result.Tests[0].Aggregate

	if agg.Excluded != 2 {
		t.Errorf("Tests[0].Aggregate.Excluded = %d, want 2 — both the original attempt and the retry must fault on state integrity and be excluded when the dead lock is seeded on every spawn\nstdout: %s\nstderr: %s",
			agg.Excluded, out.Stdout, out.Stderr)
	}
	if !agg.InfrastructureFailure {
		t.Errorf("Tests[0].Aggregate.InfrastructureFailure = false, want true — two state-integrity faults on one repetition must be reported as an infrastructure failure\nstdout: %s",
			out.Stdout)
	}

	combined := string(out.Stdout) + string(out.Stderr)
	if strings.Contains(combined, "sandbox already exists") {
		t.Errorf("output contains \"sandbox already exists\" — the retry must reach the subject via its own fresh sandbox, not be blocked at workspace creation:\nstdout: %s\nstderr: %s",
			out.Stdout, out.Stderr)
	}
}

// TestDisposition_CompromisedLock_RetryRecovers covers the complementary
// case: the dead lock is seeded only on the first spawn (SeedDeadLockOnce),
// so the retry runs clean. The first attempt is excluded (state integrity),
// the retry is counted and its assertions hold.
//
// Composed behavior asserted: one excluded attempt, one counted attempt,
// aggregate PASS, no infrastructure failure, exit code ExitSuccess.
//
// This is a RED test until I14.1 gives each attempt its own RunKey: without
// that fix, the retry fails at workspace creation with ErrSandboxExists and
// is reported as an infrastructure failure rather than a clean recovery.
func TestDisposition_CompromisedLock_RetryRecovers(t *testing.T) {
	sc := Scenario{
		Dir:       "testdata/e2e/state-integrity",
		SuitePath: "state-integrity-recovery.suite.yaml",
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("dispatch", "greeter"),
				finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
		SeedDeadLockOnce: true,
		// A non-advancing clock guarantees deterministic RED-phase failure:
		// both attempts receive the same run id (pre-I14.1, when the attempt
		// index is not yet folded into the suffix), so the retry always hits
		// workspace.ErrSandboxExists rather than occasionally succeeding because
		// the two attempts happened to straddle a real wall-clock second
		// boundary. Post-I14.1, the attempt index is folded into a distinct
		// suffix despite the frozen timestamp, so the retry gets its own
		// sandbox and this test passes correctly.
		Clock: frozenClock{t: time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)},
	}

	out := RunScenario(t, sc)

	if len(out.Result.Tests) != 1 || out.Result.Tests[0].TestID != "lock-compromised-once" {
		t.Fatalf("Result.Tests = %+v, want exactly one test named %q\nstdout: %s", out.Result.Tests, "lock-compromised-once", out.Stdout)
	}
	agg := out.Result.Tests[0].Aggregate

	if agg.InfrastructureFailure {
		t.Errorf("Tests[0].Aggregate.InfrastructureFailure = true, want false — a single state-integrity fault followed by a successful retry must not be an infrastructure failure\nstdout: %s\nstderr: %s",
			out.Stdout, out.Stderr)
	}
	if agg.Excluded != 1 {
		t.Errorf("Tests[0].Aggregate.Excluded = %d, want 1 — the compromised first attempt is excluded from the pass-rate denominator\nstdout: %s",
			agg.Excluded, out.Stdout)
	}
	if agg.Counted != 1 {
		t.Errorf("Tests[0].Aggregate.Counted = %d, want 1 — the successful retry is counted toward the pass rate\nstdout: %s",
			agg.Counted, out.Stdout)
	}
	if agg.Verdict != string(domain.VerdictPass) {
		t.Errorf("Tests[0].Aggregate.Verdict = %q, want %q — the retry's clean run must yield an overall passing verdict\nstdout: %s\nstderr: %s",
			agg.Verdict, domain.VerdictPass, out.Stdout, out.Stderr)
	}
	if out.ExitCode != cli.ExitSuccess {
		t.Errorf("exit code = %d, want cli.ExitSuccess (%d)\nstdout: %s\nstderr: %s",
			out.ExitCode, cli.ExitSuccess, out.Stdout, out.Stderr)
	}
}
