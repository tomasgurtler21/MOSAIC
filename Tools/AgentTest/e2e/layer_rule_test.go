package e2e

// T18.5 (layer half) — the layer rules take effect end to end:
// protocol-message validation is suppressed for the orchestrator layer and
// applied for the other two.
//
// (The inversion half of T18.5 is negative_inversion_test.go.)
//
// All three tests in this scenario share the same facts: one collaborator
// ("helper") replies with an invented status code — the one Communication
// Protocol violation these facts contain — and each test declares
// protocol_violations: 0, which is wrong for a layer that applies
// validation (subagent, integration) and, by the design's own layer table,
// not_evaluated — and therefore trivially non-failing — for the
// orchestrator layer, regardless of what the collaborator actually said.
// Declaring the same wrong value under all three layers is what isolates
// the layer rule as the only variable: a suite where the orchestrator test
// merely declared a correct value would prove nothing about suppression.
//
// The helper's reply travels the rewrite-prompt path
// (SupportsDirectSubstitution: false) for the same reason detection_test.go
// and concurrency_test.go need it: protocol_violations facts come from a
// post-invocation echo record, and direct substitution never produces one.

import (
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/fake"
)

func TestLayerRule_ProtocolValidation_SuppressedForOrchestratorAppliedElsewhere(t *testing.T) {
	helper := domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "helper"}

	script := fakeOptionsFor(
		domain.HarnessCapabilities{SupportsDirectSubstitution: false},
		[]subjectStep{
			invoke("dispatch", "helper"),
			finish(`{"agent_instance_id":"standin-subject#1","run_id":"layer-run","status_code":"SUCCESS","status_message":"Done."}`),
		},
	)
	script.Script = map[string][]fake.Turn{
		helper.Key(): {
			{Body: `{"agent_instance_id":"helper#1","run_id":"layer-run","status_code":"MAYBE","status_message":"Helped."}`},
		},
	}

	sc := Scenario{
		Dir:       "testdata/e2e/layer-rule",
		SuitePath: "layer-rule.suite.yaml",
		Script:    script,
	}

	out := RunScenario(t, sc)

	// subagent-layer and integration-layer fail; orchestrator-layer passes:
	// the suite as a whole reports a test failure.
	if out.ExitCode != cli.ExitTestsFailed {
		t.Fatalf("RunScenario: exit code = %d, want %d (ExitTestsFailed)\nstdout: %s\nstderr: %s",
			out.ExitCode, cli.ExitTestsFailed, out.Stdout, out.Stderr)
	}

	byID := map[string]wireTestReportDoc{}
	for _, tr := range out.Result.Tests {
		byID[tr.TestName] = tr
	}
	if len(byID) != 3 {
		t.Fatalf("Result.Tests has %d entries, want 3\nstdout: %s", len(byID), out.Stdout)
	}

	assertProtocolViolationsOutcome(t, out, byID["subagent-layer"], domain.VerdictFail, domain.AssertionFail)
	assertProtocolViolationsOutcome(t, out, byID["integration-layer"], domain.VerdictFail, domain.AssertionFail)
	assertProtocolViolationsOutcome(t, out, byID["orchestrator-layer"], domain.VerdictPass, domain.AssertionNotEvaluated)
}

func assertProtocolViolationsOutcome(t *testing.T, out Outcome, tr wireTestReportDoc, wantVerdict domain.Verdict, wantOutcome domain.AssertionOutcome) {
	t.Helper()

	if got := tr.Aggregate.Verdict; got != string(wantVerdict) {
		t.Errorf("test %q: aggregate verdict = %q, want %q\nstdout: %s", tr.TestName, got, wantVerdict, out.Stdout)
	}
	if len(tr.Runs) != 1 {
		t.Fatalf("test %q: Runs = %+v, want exactly one run", tr.TestName, tr.Runs)
	}

	var found bool
	for _, a := range tr.Runs[0].Assertions {
		if a.Class != string(domain.ClassProtocolViolations) {
			continue
		}
		found = true
		if a.Outcome != string(wantOutcome) {
			t.Errorf("test %q: protocol_violations assertion outcome = %q, want %q\nstdout: %s",
				tr.TestName, a.Outcome, wantOutcome, out.Stdout)
		}
	}
	if !found {
		t.Errorf("test %q: no protocol_violations assertion found among %+v", tr.TestName, tr.Runs[0].Assertions)
	}
}
