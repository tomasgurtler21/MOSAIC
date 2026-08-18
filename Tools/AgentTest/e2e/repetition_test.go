package e2e

// T18.8 — the repetition-and-aggregation end-to-end test: a test declaring
// several repetitions with a pass rate is aggregated correctly across mixed
// outcomes, and the aggregate is what the report and the exit code reflect.
//
// Producing a genuinely mixed per-repetition outcome from one fixed
// subject-turn script (SpawnPlan.Stdin carries the same script to every
// launch of one test, repetitions included) needed one piece of test-only
// stand-in wiring: standin_test.go's SkipInvokeOnRuns, keyed by
// domain.RunKey.RunNumber (read back via readTestIdentity(store), already
// available with no new plumbing — confirming the prior session's own
// suspicion in Stage-18/PlanProgress.md). Both tests in this scenario skip
// the second repetition's only Invoke turn: no collaborator is dispatched,
// so its declared side effect never fires and artifact_created fails that
// run — a fixed 1-of-2 outcome both tests share, differing only in their
// declared pass_rate.

import (
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
)

func TestRepetition_MixedOutcomesAcrossRepetitions_AggregateAgainstDeclaredPassRate(t *testing.T) {
	sc := Scenario{
		Dir:       "testdata/e2e/repetition",
		SuitePath: "repetition.suite.yaml",
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("dispatch", "effecter"),
				finish(`{"agent_instance_id":"standin-subject#1","run_id":"repetition-run","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
		SkipInvokeOnRuns: []int{2},
	}

	out := RunScenario(t, sc)

	// The suite mixes an aggregate pass and an aggregate fail, so the process
	// exit code must be ExitTestsFailed: at least one test's aggregate did
	// not meet its declared pass rate.
	if out.ExitCode != cli.ExitTestsFailed {
		t.Fatalf("RunScenario: exit code = %d, want %d (ExitTestsFailed)\nstdout: %s\nstderr: %s",
			out.ExitCode, cli.ExitTestsFailed, out.Stdout, out.Stderr)
	}

	byID := map[string]wireTestReportDoc{}
	for _, tr := range out.Result.Tests {
		byID[tr.TestID] = tr
	}
	if len(byID) != 2 {
		t.Fatalf("Result.Tests has %d entries, want 2\nstdout: %s", len(byID), out.Stdout)
	}

	meets := byID["aggregation-meets-threshold"]
	below := byID["aggregation-below-threshold"]

	assertAggregate(t, out, meets, domain.VerdictPass, 2, 1)
	assertAggregate(t, out, below, domain.VerdictFail, 2, 1)
}

// assertAggregate checks the shape every aggregation case in this scenario
// shares: the declared repetitions all counted (none excluded — this
// scenario has no state-integrity fault), a fixed number of passes among
// them, and the aggregate verdict the declared pass_rate implies.
func assertAggregate(t *testing.T, out Outcome, tr wireTestReportDoc, wantVerdict domain.Verdict, wantCounted, wantPassed int) {
	t.Helper()

	if tr.TestID == "" {
		t.Fatalf("no result found for the expected test id\nstdout: %s", out.Stdout)
	}
	if got := tr.Aggregate.Verdict; got != string(wantVerdict) {
		t.Errorf("test %q: Aggregate.Verdict = %q, want %q\nstdout: %s", tr.TestID, got, wantVerdict, out.Stdout)
	}
	if tr.Aggregate.Counted != wantCounted {
		t.Errorf("test %q: Aggregate.Counted = %d, want %d\nstdout: %s", tr.TestID, tr.Aggregate.Counted, wantCounted, out.Stdout)
	}
	if tr.Aggregate.Excluded != 0 {
		t.Errorf("test %q: Aggregate.Excluded = %d, want 0 — no state-integrity fault is scripted here\nstdout: %s", tr.TestID, tr.Aggregate.Excluded, out.Stdout)
	}
	if len(tr.Runs) != wantCounted {
		t.Fatalf("test %q: Runs has %d entries, want %d\nstdout: %s", tr.TestID, len(tr.Runs), wantCounted, out.Stdout)
	}

	passed := 0
	for _, r := range tr.Runs {
		if r.Verdict == string(domain.VerdictPass) {
			passed++
		}
	}
	if passed != wantPassed {
		t.Errorf("test %q: %d of %d runs passed, want %d\nstdout: %s", tr.TestID, passed, len(tr.Runs), wantPassed, out.Stdout)
	}
}
