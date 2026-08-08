package e2e

// T18.1 — the happy-path end-to-end test: an authored suite, test
// definition and stub registry go in; the tool runs the whole pipeline
// through the scripted adapter; the verdict is a pass, the report contains
// what the run actually did, and the process exit code is the success code.
//
// This test drives internal/cli.Execute — the non-interactive frontend's
// real entry point — rather than internal/suite or internal/runner
// directly, so the wiring between them is part of what is under test (see
// RunScenario in harness_test.go).

import (
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
)

func TestHappyPath_SubstitutedInvocation_PassesEndToEnd(t *testing.T) {
	sc := Scenario{
		Dir:       "testdata/e2e/happy-path",
		SuitePath: "happy-path.suite.yaml",
		Script: fakeOptionsFor(
			domain.HarnessCapabilities{SupportsDirectSubstitution: true},
			[]subjectStep{
				invoke("Task", "greeter"),
				finish(`{"agent_instance_id":"standin-subject#1","status_code":"SUCCESS","status_message":"Done."}`),
			},
		),
	}

	out := RunScenario(t, sc)

	if out.ExitCode != cli.ExitSuccess {
		t.Fatalf("RunScenario: exit code = %d, want %d (ExitSuccess)\nstdout: %s\nstderr: %s",
			out.ExitCode, cli.ExitSuccess, out.Stdout, out.Stderr)
	}
	if out.Result.SuiteID != "happy-path-e2e" {
		t.Errorf("Result.SuiteID = %q, want %q\nstdout: %s", out.Result.SuiteID, "happy-path-e2e", out.Stdout)
	}
	if got := out.Result.Counts[string(domain.VerdictPass)]; got != 1 {
		t.Errorf("Result.Counts[PASS] = %d, want 1\nstdout: %s", got, out.Stdout)
	}
	if out.Result.InfrastructureFailures != 0 {
		t.Errorf("Result.InfrastructureFailures = %d, want 0\nstdout: %s", out.Result.InfrastructureFailures, out.Stdout)
	}
	if len(out.Result.Tests) != 1 || out.Result.Tests[0].TestID != "greet" {
		t.Fatalf("Result.Tests = %+v, want exactly one test named %q", out.Result.Tests, "greet")
	}
	if agg := out.Result.Tests[0].Aggregate.Verdict; agg != string(domain.VerdictPass) {
		t.Errorf("Tests[0].Aggregate.Verdict = %q, want %q", agg, domain.VerdictPass)
	}
	for _, run := range out.Result.Tests[0].Runs {
		for _, a := range run.Assertions {
			if a.Outcome != "pass" {
				t.Errorf("assertion %s(%s) outcome = %q, want %q\nstdout: %s", a.Class, a.Target, a.Outcome, "pass", out.Stdout)
			}
		}
	}
}
