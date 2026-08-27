package e2e

// T18.5 (inversion half) — a test declared negative passes when its
// underlying assertions fail and fails when they pass, exercised in both
// directions through the real entry point.
//
// Each direction is its own single-test scenario directory rather than one
// suite filtered down with --tests: preflight.Overrides.TestIDs is declared
// but internal/preflight.Validate never consults it, so --tests currently
// has no filtering effect (see Stage-18/PlanProgress.md). Splitting the
// scenario avoids depending on that gap rather than working around it with
// a report-level index.
//
// The layer-rule half of T18.5 (protocol-message validation suppressed for
// the orchestrator layer, applied for the other two) is not covered here:
// see Stage-18/PlanProgress.md for why it is not currently reachable
// end-to-end — internal/runner.BuildEvidence never populates
// domain.RunEvidence.ProtocolViolations/SubjectProtocolViolations from a
// real run's evidence, so no end-to-end scenario can make that assertion
// class observe a real violation regardless of how the scenario is
// scripted.

import (
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
)

// subjectGreetsOneCollaborator is the scripted subject sequence both
// inversion scenarios share: one direct-substitution invocation, then a
// clean finish.
func subjectGreetsOneCollaborator() domain.HarnessCapabilities {
	return domain.HarnessCapabilities{SupportsDirectSubstitution: true}
}

// TestNegativeInversion_UnderlyingAssertionFails_OverallPasses covers a
// negative test whose declared artifact is never created: the underlying
// artifact_created assertion fails, which is what the negative test
// expects, so the run's verdict must be a pass.
func TestNegativeInversion_UnderlyingAssertionFails_OverallPasses(t *testing.T) {
	sc := Scenario{
		Dir:       "testdata/e2e/negative-inversion-fails",
		SuitePath: "negative-inversion-fails.suite.yaml",
		Script: fakeOptionsFor(
			subjectGreetsOneCollaborator(),
			[]subjectStep{
				invoke("dispatch", "greeter"),
				finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
	}

	out := RunScenario(t, sc)

	assertSingleTestVerdict(t, out, "underlying-fails", domain.VerdictPass, cli.ExitSuccess)
}

// TestNegativeInversion_UnderlyingAssertionPasses_OverallFails is the mirror
// case: the declared artifact really is created (the stub's side effect
// produces it), so the underlying assertion holds — which a negative test
// must not accept — and the run's verdict must be a fail.
func TestNegativeInversion_UnderlyingAssertionPasses_OverallFails(t *testing.T) {
	sc := Scenario{
		Dir:       "testdata/e2e/negative-inversion-passes",
		SuitePath: "negative-inversion-passes.suite.yaml",
		Script: fakeOptionsFor(
			subjectGreetsOneCollaborator(),
			[]subjectStep{
				invoke("dispatch", "greeter"),
				finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
	}

	out := RunScenario(t, sc)

	assertSingleTestVerdict(t, out, "underlying-passes", domain.VerdictFail, cli.ExitTestsFailed)
}

// assertSingleTestVerdict is the shared shape both inversion directions
// check: exactly one test ran, under the expected id, with the expected
// aggregate verdict and process exit code.
func assertSingleTestVerdict(t *testing.T, out Outcome, wantTestID string, wantVerdict domain.Verdict, wantExit int) {
	t.Helper()

	if out.ExitCode != wantExit {
		t.Fatalf("RunScenario: exit code = %d, want %d\nstdout: %s\nstderr: %s",
			out.ExitCode, wantExit, out.Stdout, out.Stderr)
	}
	if len(out.Result.Tests) != 1 || out.Result.Tests[0].TestName != wantTestID {
		t.Fatalf("Result.Tests = %+v, want exactly one test named %q\nstdout: %s", out.Result.Tests, wantTestID, out.Stdout)
	}
	if agg := out.Result.Tests[0].Aggregate.Verdict; agg != string(wantVerdict) {
		t.Errorf("Tests[0].Aggregate.Verdict = %q, want %q\nstdout: %s", agg, wantVerdict, out.Stdout)
	}
}
