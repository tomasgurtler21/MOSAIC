package e2e

// T18.4 — the peak-concurrency end-to-end test: a scripted run with
// genuinely overlapping collaborator invocations produces a reconstructed
// peak matching what was scripted, and the corresponding assertion both
// passes when it should and fails when the declared minimum is not met.
//
// "Genuinely overlapping" here means what internal/concurrency.Peaks is
// actually defined to read off the invocation log: two invocations whose
// recorded start/end intervals overlap. The scripted subject achieves that
// by dispatching both collaborators as one batch — both pre-invocation
// interceptions fire before either post-invocation interception does (see
// concurrencyBatch in standin_test.go) — which is the only kind of
// concurrency a single-process scripted stand-in can honestly produce, and
// exactly the evidence the reconstruction is defined over.
//
// This is also the specific defect T18.4 exists to demonstrate absent: the
// first-generation implementation always reported zero peak concurrency, so
// a min_concurrency assertion could never pass. Both directions are
// exercised here — a minimum the run actually reached, and one it did
// not — to show the assertion is capable of both outcomes rather than
// silently incapable of the one that matters.
//
// Both collaborator replies travel the rewrite-prompt path
// (SupportsDirectSubstitution: false): direct substitution never triggers
// the post-invocation phase (there is no real collaborator response for the
// harness to observe), so a substituted invocation's interval never closes
// and internal/concurrency.Peaks excludes it from the overlap computation
// entirely rather than reporting a false concurrency figure.

import (
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/fake"
)

func TestConcurrency_OverlappingInvocations_ReconstructsPeakBothWays(t *testing.T) {
	researcher := domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "researcher"}
	libResearcher := domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "library-researcher"}

	script := fakeOptionsFor(
		domain.HarnessCapabilities{SupportsDirectSubstitution: false},
		[]subjectStep{
			// No Result turn between these two Invoke turns: the stand-in
			// treats them as one concurrency batch (see standin_test.go).
			invoke("dispatch", "researcher"),
			invoke("dispatch", "library-researcher"),
			finish(`{"agent_instance_id":"standin-subject#1","run_id":"concurrency-run","status_code":"SUCCESS","status_message":"Done."}`),
		},
	)
	script.Script = map[string][]fake.Turn{
		researcher.Key(): {
			{Body: `{"agent_instance_id":"researcher#1","run_id":"concurrency-run","status_code":"SUCCESS","status_message":"Researched."}`},
		},
		libResearcher.Key(): {
			{Body: `{"agent_instance_id":"library-researcher#2","run_id":"concurrency-run","status_code":"SUCCESS","status_message":"Library research done."}`},
		},
	}

	sc := Scenario{
		Dir:       "testdata/e2e/concurrency",
		SuitePath: "concurrency.suite.yaml",
		Script:    script,
	}

	out := RunScenario(t, sc)

	// One test's declared minimum matches what happened (pass), the other's
	// does not (fail), so the suite as a whole reports a test failure.
	if out.ExitCode != cli.ExitTestsFailed {
		t.Fatalf("RunScenario: exit code = %d, want %d (ExitTestsFailed)\nstdout: %s\nstderr: %s",
			out.ExitCode, cli.ExitTestsFailed, out.Stdout, out.Stderr)
	}

	byID := map[string]wireTestReportDoc{}
	for _, tr := range out.Result.Tests {
		byID[tr.TestName] = tr
	}
	if len(byID) != 2 {
		t.Fatalf("Result.Tests has %d entries, want 2\nstdout: %s", len(byID), out.Stdout)
	}

	assertMinConcurrency(t, out, byID["min-concurrency-pass"], domain.VerdictPass, domain.AssertionPass)
	assertMinConcurrency(t, out, byID["min-concurrency-fail"], domain.VerdictFail, domain.AssertionFail)
}

func assertMinConcurrency(t *testing.T, out Outcome, tr wireTestReportDoc, wantVerdict domain.Verdict, wantOutcome domain.AssertionOutcome) {
	t.Helper()

	if got := tr.Aggregate.Verdict; got != string(wantVerdict) {
		t.Errorf("test %q: aggregate verdict = %q, want %q\nstdout: %s", tr.TestName, got, wantVerdict, out.Stdout)
	}
	if len(tr.Runs) != 1 {
		t.Fatalf("test %q: Runs = %+v, want exactly one run", tr.TestName, tr.Runs)
	}

	var found bool
	for _, a := range tr.Runs[0].Assertions {
		if a.Class != string(domain.ClassMinConcurrency) {
			continue
		}
		found = true
		if a.Outcome != string(wantOutcome) {
			t.Errorf("test %q: min_concurrency assertion outcome = %q, want %q\nstdout: %s",
				tr.TestName, a.Outcome, wantOutcome, out.Stdout)
		}
	}
	if !found {
		t.Errorf("test %q: no min_concurrency assertion found among %+v", tr.TestName, tr.Runs[0].Assertions)
	}
}
