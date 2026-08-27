package e2e

// The echo-fidelity end-to-end test: when a scripted collaborator's reply
// differs from what the stub declared, the runner excludes the run from the
// pass-rate aggregate and retries once. If the mismatch recurs on the retry,
// both runs are excluded and the suite reports InfrastructureFailure (exit
// code 1). There is no opt-out and no configuration that suppresses echo
// fidelity evaluation.
//
// The scenario runs the rewrite-prompt path (SupportsDirectSubstitution:
// false), the only path a harness with no direct-substitution capability
// has for stub delivery. The scripted collaborator's post-invocation reply
// (fake.Options.Script) deliberately differs from the stub registry's
// declared response on both attempts, so both runs are excluded and the
// suite escalates to InfrastructureFailure.

import (
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/fake"
)

func TestEchoFidelity_CollaboratorReplyMismatchesStub_ExcludedAndEscalated(t *testing.T) {
	greeter := domain.CollaboratorIdentity{ToolName: "dispatch", AgentIdentity: "greeter"}

	script := fakeOptionsFor(
		domain.HarnessCapabilities{SupportsDirectSubstitution: false},
		[]subjectStep{
			invoke("dispatch", "greeter"),
			finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
		},
	)
	// The stub registry declares status_message "Greeted."; both scripted
	// replies declare a different one, so the observed and expected values
	// differ under CompareEcho on both the original attempt and the retry.
	// Two turns are provided so the retry's collaborator invocation also
	// returns the mismatch and triggers a second exclusion.
	script.Script = map[string][]fake.Turn{
		greeter.Key(): {
			{Body: `{"agent_instance_id":"greeter#1","status_code":"SUCCESS","status_message":"This is not what the stub declared."}`},
			{Body: `{"agent_instance_id":"greeter#1","status_code":"SUCCESS","status_message":"This is not what the stub declared."}`},
		},
	}

	sc := Scenario{
		Dir:       "testdata/e2e/echo-fidelity",
		SuitePath: "echo-fidelity.suite.yaml",
		Script:    script,
	}

	out := RunScenario(t, sc)

	if out.ExitCode != cli.ExitFailure {
		t.Fatalf("RunScenario: exit code = %d, want %d (ExitFailure/InfrastructureFailure)\nstdout: %s\nstderr: %s",
			out.ExitCode, cli.ExitFailure, out.Stdout, out.Stderr)
	}
	if len(out.Result.Tests) != 1 {
		t.Fatalf("Result.Tests = %+v, want exactly one test", out.Result.Tests)
	}
	if len(out.Result.Tests[0].Runs) != 1 {
		t.Fatalf("Result.Tests[0].Runs length = %d, want 1 (the retry run; Runs holds one entry per repetition, not per attempt)\nstdout: %s",
			len(out.Result.Tests[0].Runs), out.Stdout)
	}

	agg := out.Result.Tests[0].Aggregate
	if agg.Excluded != 2 {
		t.Errorf("Aggregate.Excluded = %d, want 2 — both the original attempt and the retry must be excluded on echo mismatch\nstdout: %s\nstderr: %s",
			agg.Excluded, out.Stdout, out.Stderr)
	}
	if !agg.InfrastructureFailure {
		t.Errorf("Aggregate.InfrastructureFailure = false, want true — two echo-mismatch exclusions must escalate to infrastructure failure\nstdout: %s",
			out.Stdout)
	}
	if agg.Counted != 0 {
		t.Errorf("Aggregate.Counted = %d, want 0 — both runs excluded, none counted toward pass rate\nstdout: %s",
			agg.Counted, out.Stdout)
	}

	// Runs contains the retry run (the final/last attempt per repetition).
	// The original attempt's exclusion data is in Aggregate.Exclusions, not here.
	for i, run := range out.Result.Tests[0].Runs {
		var sawEchoFail, sawArtifactPass bool
		for _, a := range run.Assertions {
			switch a.Class {
			case string(domain.ClassEchoFidelity):
				sawEchoFail = a.Outcome == "fail"
				if a.Outcome != "fail" {
					t.Errorf("run[%d]: echo_fidelity assertion outcome = %q, want fail\nstdout: %s", i, a.Outcome, out.Stdout)
				}
			case string(domain.ClassArtifactCreated):
				sawArtifactPass = a.Outcome == "pass"
				if a.Outcome != "pass" {
					t.Errorf("run[%d]: artifact_created assertion outcome = %q, want pass — the echo mismatch alone must decide the verdict, not a coincidental failure elsewhere", i, a.Outcome)
				}
			}
		}
		if !sawEchoFail {
			t.Errorf("run[%d]: no failing echo_fidelity assertion found among %+v; echo fidelity must always be evaluated, with no opt-out", i, run.Assertions)
		}
		if !sawArtifactPass {
			t.Errorf("run[%d]: no passing artifact_created assertion found among %+v; the isolation this test relies on requires it to hold", i, run.Assertions)
		}
	}
}
