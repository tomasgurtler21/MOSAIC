package e2e

// T18.11 — the documentation-grade sample suite: one full-workflow
// orchestrator-layer test and one single-decision subagent-layer test,
// authored under Tools/AgentTest/examples/ (distinct from
// testdata/examples/, which is pre-flight's own reference fixtures, parsed
// but never executed). This test runs both through the real entry point:
// a pre-flight failure or a run failure here breaks the build, so the
// samples cannot silently drift from the implementation they document.

import (
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/fake"
)

func TestExamples_DocumentationGradeSamples_PassPreflightAndRunGreen(t *testing.T) {
	requirementsRefinement := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "requirements-refinement"}
	researcher := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}
	libResearcher := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "library-researcher"}
	planner := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "planner"}

	script := fakeOptionsFor(
		domain.HarnessCapabilities{SupportsDirectSubstitution: false},
		[]subjectStep{
			invoke("Task", "requirements-refinement"),
			// No Result turn between these two Invoke turns: one concurrency
			// batch, matching the declared research-fanout parallel group.
			invoke("Task", "researcher"),
			invoke("Task", "library-researcher"),
			invoke("Task", "planner"),
			finish(`{"agent_instance_id":"orchestrator#5","run_id":"full-workflow-run","status_code":"SUCCESS","status_message":"Workflow complete."}`),
		},
	)
	// Every reply matches its stub's declared response verbatim (see
	// examples/full-workflow.stubs.json), and every message carries run_id,
	// so protocol_violations: 0 holds and echo fidelity never masks what
	// this sample's other assertions check.
	script.Script = map[string][]fake.Turn{
		requirementsRefinement.Key(): {
			{Body: `{"agent_instance_id":"requirements-refinement#1","run_id":"full-workflow-run","status_code":"SUCCESS","status_message":"Requirements captured."}`},
		},
		researcher.Key(): {
			{Body: `{"agent_instance_id":"researcher#2","run_id":"full-workflow-run","status_code":"SUCCESS","status_message":"Research complete."}`},
		},
		libResearcher.Key(): {
			{Body: `{"agent_instance_id":"library-researcher#3","run_id":"full-workflow-run","status_code":"SUCCESS","status_message":"No relevant libraries found."}`},
		},
		planner.Key(): {
			{Body: `{"agent_instance_id":"planner#4","run_id":"full-workflow-run","status_code":"SUCCESS","status_message":"Plan written."}`},
		},
	}
	details := map[string][]sidecarDetail{
		requirementsRefinement.Key(): {{AgentInstanceID: "requirements-refinement#1", TaskDescription: "Refine the requirements."}},
		researcher.Key():             {{AgentInstanceID: "researcher#2", TaskDescription: "Research the approach."}},
		libResearcher.Key():          {{AgentInstanceID: "library-researcher#3", TaskDescription: "Research candidate libraries."}},
		planner.Key():                {{AgentInstanceID: "planner#4", TaskDescription: "Write the plan for this workflow."}},
	}

	sc := Scenario{
		Dir:               "examples",
		SuitePath:         "examples.suite.yaml",
		Script:            script,
		InvocationDetails: details,
		// single-decision shares this scenario's one Scenario.Script (every
		// stand-in process replays it from its start, whichever test the run
		// belongs to) but must not dispatch any collaborator at all: its own
		// stub registry is empty, and it exists to demonstrate a subject that
		// decides without dispatching anyone.
		SkipInvokeForTestIDs: []string{"single-decision"},
	}

	out := RunScenario(t, sc)

	if out.ExitCode != cli.ExitSuccess {
		t.Fatalf("RunScenario: exit code = %d, want %d (ExitSuccess) — the sample suite must pass pre-flight and run green\nstdout: %s\nstderr: %s",
			out.ExitCode, cli.ExitSuccess, out.Stdout, out.Stderr)
	}
	if out.Result.SuiteID != "documentation-examples" {
		t.Errorf("Result.SuiteID = %q, want %q\nstdout: %s", out.Result.SuiteID, "documentation-examples", out.Stdout)
	}

	byID := map[string]wireTestReportDoc{}
	for _, tr := range out.Result.Tests {
		byID[tr.TestID] = tr
	}
	if len(byID) != 2 {
		t.Fatalf("Result.Tests has %d entries, want 2\nstdout: %s", len(byID), out.Stdout)
	}

	for _, id := range []string{"full-workflow", "single-decision"} {
		tr, ok := byID[id]
		if !ok {
			t.Fatalf("no result for sample test %q\nstdout: %s", id, out.Stdout)
		}
		if got := tr.Aggregate.Verdict; got != string(domain.VerdictPass) {
			t.Errorf("sample %q: Aggregate.Verdict = %q, want %q\nstdout: %s", id, got, domain.VerdictPass, out.Stdout)
		}
		for _, run := range tr.Runs {
			for _, a := range run.Assertions {
				// protocol_violations is not_evaluated rather than pass for
				// the full-workflow sample: it declares layer: orchestrator,
				// and protocol-message validation is suppressed for that
				// layer by design (see layer_rule_test.go) — not_evaluated
				// here is the correct outcome, not a failure.
				if a.Outcome != string(domain.AssertionPass) && a.Outcome != string(domain.AssertionNotEvaluated) {
					t.Errorf("sample %q: assertion %s(%s) outcome = %q, want %q or %q\nstdout: %s",
						id, a.Class, a.Target, a.Outcome, domain.AssertionPass, domain.AssertionNotEvaluated, out.Stdout)
				}
			}
		}
	}
}
