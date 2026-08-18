package e2e

// T18.3 — the echo-fidelity end-to-end test: a scripted collaborator that
// returns something other than what the stub declared fails the test
// unconditionally, with the mismatch identified — no opt-out, no
// configuration that suppresses it.
//
// The scenario runs the rewrite-prompt path (SupportsDirectSubstitution:
// false), the only path a harness with no direct-substitution capability
// has for stub delivery. The scripted collaborator's post-invocation reply
// (fake.Options.Script) deliberately differs from the stub registry's
// declared response, so echo fidelity — evaluated on every stubbed
// invocation with no author opt-out — must fail the run even though the
// declared artifact_created assertion is independently satisfied.

import (
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/fake"
)

func TestEchoFidelity_CollaboratorReplyMismatchesStub_FailsUnconditionally(t *testing.T) {
	greeter := domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "greeter"}

	script := fakeOptionsFor(
		domain.HarnessCapabilities{SupportsDirectSubstitution: false},
		[]subjectStep{
			invoke("dispatch", "greeter"),
			finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
		},
	)
	// The stub registry declares status_message "Greeted."; this scripted
	// reply declares a different one, so the observed and expected values
	// differ under CompareEcho.
	script.Script = map[string][]fake.Turn{
		greeter.Key(): {
			{Body: `{"agent_instance_id":"greeter#1","status_code":"SUCCESS","status_message":"This is not what the stub declared."}`},
		},
	}

	sc := Scenario{
		Dir:       "testdata/e2e/echo-fidelity",
		SuitePath: "echo-fidelity.suite.yaml",
		Script:    script,
	}

	out := RunScenario(t, sc)

	if out.ExitCode != cli.ExitTestsFailed {
		t.Fatalf("RunScenario: exit code = %d, want %d (ExitTestsFailed)\nstdout: %s\nstderr: %s",
			out.ExitCode, cli.ExitTestsFailed, out.Stdout, out.Stderr)
	}
	if got := out.Result.Counts[string(domain.VerdictFail)]; got != 1 {
		t.Errorf("Result.Counts[FAIL] = %d, want 1\nstdout: %s", got, out.Stdout)
	}
	if len(out.Result.Tests) != 1 || len(out.Result.Tests[0].Runs) != 1 {
		t.Fatalf("Result.Tests = %+v, want exactly one test with one run", out.Result.Tests)
	}

	run := out.Result.Tests[0].Runs[0]
	if run.Verdict != string(domain.VerdictFail) {
		t.Errorf("run.Verdict = %q, want %q\nstdout: %s", run.Verdict, domain.VerdictFail, out.Stdout)
	}

	var sawEchoFail, sawArtifactPass bool
	for _, a := range run.Assertions {
		switch a.Class {
		case string(domain.ClassEchoFidelity):
			sawEchoFail = a.Outcome == "fail"
			if a.Outcome != "fail" {
				t.Errorf("echo_fidelity assertion outcome = %q, want fail\nstdout: %s", a.Outcome, out.Stdout)
			}
		case string(domain.ClassArtifactCreated):
			sawArtifactPass = a.Outcome == "pass"
			if a.Outcome != "pass" {
				t.Errorf("artifact_created assertion outcome = %q, want pass — the echo mismatch alone must decide the verdict, not a coincidental failure elsewhere", a.Outcome)
			}
		}
	}
	if !sawEchoFail {
		t.Errorf("no failing echo_fidelity assertion found among %+v; echo fidelity must always be evaluated, with no opt-out", run.Assertions)
	}
	if !sawArtifactPass {
		t.Errorf("no passing artifact_created assertion found among %+v; the isolation this test relies on requires it to hold", run.Assertions)
	}
}
