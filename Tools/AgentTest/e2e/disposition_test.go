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
	"mosaic-agent-test/internal/harness/fake"
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

	// A cutoff-terminated run is evaluated exactly like any other run: its
	// verdict is determined by its assertions, not by a special disposition
	// code. The single-decision test declares invocation_sequence.exact: true
	// with 1 step (greeter). With the Stage 2 implementation the invocation
	// log has exactly 1 entry (greeter only, no phantom helper record), so the
	// assertion passes and the verdict is PASS.
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

	// Exact invocation-log count: the invocation_sequence.exact: true
	// assertion declared in the fixture must pass, confirming the invocation
	// log contains exactly N=1 entries and no phantom (N+1)th dispatch was
	// recorded. A failure here means a phantom record is still being emitted.
	foundSequenceAssertion := false
	for _, a := range run.Assertions {
		if a.Class == string(domain.ClassInvocationSequence) {
			foundSequenceAssertion = true
			if a.Outcome != "pass" {
				t.Errorf("invocation_sequence assertion outcome = %q, want pass: the invocation log must contain exactly N=1 entries for a stop_after_invocations: 1 run; a non-pass result indicates a phantom (N+1)th record was emitted\nstdout: %s",
					a.Outcome, out.Stdout)
			}
		}
	}
	if !foundSequenceAssertion {
		t.Errorf("no invocation_sequence assertion found in run report; the fixture declares exact: true and it must be evaluated\nstdout: %s", out.Stdout)
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
	if len(out.Result.Tests) != 1 || out.Result.Tests[0].TestName != "never-finishes" {
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

// TestDisposition_ReplyRecoveryCutoff_ReportsEarlyExitWithExactCount covers
// the reply-recovery branch of the hard subject cutoff: a suite run with
// stop_after_invocations: 2 against a fake harness that declares
// SupportsReplyRecovery but not SupportsDirectSubstitution. The cutoff fires
// at the completion event of the second (Nth) dispatch, not at its
// pre-invocation point, because turns are consumed at completion on this
// branch.
//
// Two properties are asserted:
//  1. The run's termination reason is "early_exit" (AC5.2: the run reports
//     the early-exit disposition, not completed/timeout/state_integrity).
//  2. The invocation_sequence.exact: true assertion declared in the fixture
//     passes, confirming exactly N=2 invocation records exist and no phantom
//     (N+1)th record was emitted (AC5.3).
//
// This is a RED test until both the fake adapter's completion-phase handling
// (I5.1) and the stand-in's completion-event driving (T5.1) are implemented:
// without them, the completion event never reaches the decision core, the
// cutoff never fires, and the run does not end with DispositionEarlyExit.
func TestDisposition_ReplyRecoveryCutoff_ReportsEarlyExitWithExactCount(t *testing.T) {
	greeterID := domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "greeter"}
	helperID := domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "helper"}

	sc := Scenario{
		Dir:       "testdata/e2e/reply-recovery-cutoff",
		SuitePath: "reply-recovery-cutoff.suite.yaml",
		Script: fake.Options{
			Capabilities: domain.HarnessCapabilities{
				SupportsReplyRecovery: true,
				CorrelationField:      "token",
			},
			// Script provides the collaborator replies consumed at PhaseCompletion —
			// one per dispatch. The stubs declare no echo body to compare against,
			// so the actual reply content does not affect assertion outcomes.
			Script: map[string][]fake.Turn{
				greeterID.Key(): {{Body: `{"agent_instance_id":"greeter#1","status_code":"SUCCESS","status_message":"Greeted."}`}},
				helperID.Key():  {{Body: `{"agent_instance_id":"helper#1","status_code":"SUCCESS","status_message":"Helped."}`}},
			},
			SubjectTurns: []fake.SubjectTurn{
				{Invoke: &greeterID},
				{Invoke: &helperID},
				// No further turns: after the Nth (helper) dispatch's completion
				// event fires the sentinel, the harness kills the stand-in.
			},
		},
	}

	out := RunScenario(t, sc)

	// The suite's reply-recovery-stubbed test must have run and its
	// invocation_sequence assertion must have passed, proving exactly N=2
	// invocation records exist.
	var stubbedRun *wireRunReportDoc
	for _, tst := range out.Result.Tests {
		if tst.TestName == "reply-recovery-stubbed" {
			if len(tst.Runs) > 0 {
				r := tst.Runs[0]
				stubbedRun = &r
			}
		}
	}
	if stubbedRun == nil {
		t.Fatalf("reply-recovery-stubbed test not found in result or had no runs\nstdout: %s\nstderr: %s",
			out.Stdout, out.Stderr)
	}

	// AC5.2: the run must report the early-exit termination reason.
	if stubbedRun.TerminationReason != string(domain.DispositionEarlyExit) {
		t.Errorf("reply-recovery-stubbed run TerminationReason = %q, want %q — the cutoff must fire at the Nth dispatch's completion event and report the early-exit disposition\nstdout: %s",
			stubbedRun.TerminationReason, domain.DispositionEarlyExit, out.Stdout)
	}

	// No fault reasons: early exit must end normally, not as timeout or state
	// integrity.
	for _, reason := range stubbedRun.Reasons {
		if reason == "TIMEOUT" || reason == "STATE_INTEGRITY" {
			t.Errorf("reply-recovery-stubbed run Reasons contains %q, want early exit to end normally: %+v\nstdout: %s",
				reason, stubbedRun.Reasons, out.Stdout)
		}
	}

	// AC5.3: the invocation_sequence assertion must pass — exactly N=2 entries
	// in the invocation log, no phantom (N+1)th record.
	foundSequenceAssertion := false
	for _, a := range stubbedRun.Assertions {
		if a.Class == string(domain.ClassInvocationSequence) {
			foundSequenceAssertion = true
			if a.Outcome != "pass" {
				t.Errorf("invocation_sequence assertion outcome = %q, want pass: the invocation log must contain exactly N=2 entries for a stop_after_invocations: 2 reply-recovery run\nstdout: %s",
					a.Outcome, out.Stdout)
			}
		}
	}
	if !foundSequenceAssertion {
		t.Errorf("no invocation_sequence assertion found in reply-recovery-stubbed run; the fixture declares exact: true and it must be evaluated\nstdout: %s", out.Stdout)
	}
}

// TestDisposition_ReplyRecoveryCutoff_UnmatchedNthDispatch_StillCutsOff
// covers AC5.4: the hard subject cutoff holds even when the Nth dispatch
// matched no registered stub and the unmatched policy is passthrough. The
// cutoff rule depends on the invocation sequence number, not on whether a
// pending stub was found, so it fires at the completion event of the Nth
// dispatch regardless.
//
// This is a RED test until the decision core's ResolveSeq falls back to the
// sequence counter when no pending stub exists (I5.1) and the completion event
// is driven by the stand-in for passthrough dispatches (T5.1): without both,
// the counter-fallback path is never exercised end to end.
func TestDisposition_ReplyRecoveryCutoff_UnmatchedNthDispatch_StillCutsOff(t *testing.T) {
	greeterID := domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "greeter"}
	helperID := domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "helper"}

	sc := Scenario{
		Dir:       "testdata/e2e/reply-recovery-cutoff",
		SuitePath: "reply-recovery-cutoff.suite.yaml",
		Script: fake.Options{
			Capabilities: domain.HarnessCapabilities{
				SupportsReplyRecovery: true,
				CorrelationField:      "token",
			},
			// Script: nil — the Nth dispatch (helper) has no stub and its
			// ObservedResponse from completion does not need to match anything.
			// Greeter's response at completion is also unscripted; the stub
			// declares no echo body so no comparison is performed.
			Script: nil,
			SubjectTurns: []fake.SubjectTurn{
				{Invoke: &greeterID},
				{Invoke: &helperID},
			},
		},
	}

	out := RunScenario(t, sc)

	var unmatchedRun *wireRunReportDoc
	for _, tst := range out.Result.Tests {
		if tst.TestName == "reply-recovery-unmatched" {
			if len(tst.Runs) > 0 {
				r := tst.Runs[0]
				unmatchedRun = &r
			}
		}
	}
	if unmatchedRun == nil {
		t.Fatalf("reply-recovery-unmatched test not found in result or had no runs\nstdout: %s\nstderr: %s",
			out.Stdout, out.Stderr)
	}

	// AC5.4: the cutoff must still report early-exit even for an unmatched Nth.
	if unmatchedRun.TerminationReason != string(domain.DispositionEarlyExit) {
		t.Errorf("reply-recovery-unmatched run TerminationReason = %q, want %q — the cutoff must fire at the Nth completion even when the dispatch matched no stub\nstdout: %s",
			unmatchedRun.TerminationReason, domain.DispositionEarlyExit, out.Stdout)
	}

	for _, reason := range unmatchedRun.Reasons {
		if reason == "TIMEOUT" || reason == "STATE_INTEGRITY" {
			t.Errorf("reply-recovery-unmatched run Reasons contains %q, want early exit to end normally: %+v\nstdout: %s",
				reason, unmatchedRun.Reasons, out.Stdout)
		}
	}

	// AC5.3 for the unmatched case: exactly N=2 invocation records in the log.
	foundSequenceAssertion := false
	for _, a := range unmatchedRun.Assertions {
		if a.Class == string(domain.ClassInvocationSequence) {
			foundSequenceAssertion = true
			if a.Outcome != "pass" {
				t.Errorf("invocation_sequence assertion outcome = %q, want pass: the invocation log must contain exactly N=2 entries even when the Nth dispatch was unmatched\nstdout: %s",
					a.Outcome, out.Stdout)
			}
		}
	}
	if !foundSequenceAssertion {
		t.Errorf("no invocation_sequence assertion found in reply-recovery-unmatched run; the fixture declares exact: true and it must be evaluated\nstdout: %s", out.Stdout)
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

	if len(out.Result.Tests) != 1 || out.Result.Tests[0].TestName != "lock-compromised" {
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

	if len(out.Result.Tests) != 1 || out.Result.Tests[0].TestName != "lock-compromised-once" {
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
