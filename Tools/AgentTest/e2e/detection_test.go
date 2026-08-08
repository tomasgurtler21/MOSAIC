package e2e

// T18.2 — detection tests: one per class of subject misbehaviour, each
// asserting not merely that the run failed but that the *named assertion*
// failed.
//
// All fourteen tests in this scenario share one fixed set of facts, produced
// by one shared subject/collaborator script run identically for every test
// in the suite (see RunScenario and Scenario.Script): the subject dispatches
// "greeter" (creates greeting.txt) and then "helper" (creates nothing, and
// whose response carries an invented status code — the one deliberate
// Communication Protocol violation these facts contain), then finishes. Each
// test below declares a different assertion against those same facts, half
// naming the true value (pass) and half naming a deliberately wrong one
// (fail) — proving each class is evaluated for real rather than always
// reporting the same outcome regardless of what is declared.
//
// Both collaborator replies travel the rewrite-prompt path
// (SupportsDirectSubstitution: false): the protocol_violations facts this
// scenario needs depend on a post-invocation echo record
// (internal/runner.BuildEvidence reads Communication Protocol violations off
// RecordEnd.Echo.Observed), which only the rewrite-prompt path produces —
// direct substitution never triggers the post-invocation phase, because
// there is no real collaborator response for the harness to observe.
//
// Nine reachable declarable classes are covered below (the tenth,
// min_concurrency, has its own scenario — see concurrency_test.go). Seven of
// the nine need no orchestration document at all (invocation_sequence,
// artifact_created, artifact_not_created, task_message,
// protocol_violations); final_state.phase, final_state.last_status,
// execution_log.agent_ids and execution_log.all_status need one seeded,
// because the scripted subject never edits it itself and the seeded content
// is therefore the final content — the same pattern
// testdata/examples/single-decision.test.yaml documents. final_state and
// execution_log each contribute two independently evaluated subfields
// (phase/last_status, agent_ids/all_status); final-state-pass and
// execution-log-pass each declare both subfields correctly and so double as
// the passing-direction case for both classes in their pair, while
// final-state-last-status-fail and execution-log-all-status-fail each
// isolate their one subfield as the only declared assertion, so the failing
// direction is attributable to that subfield alone rather than conflated
// with its sibling.
//
// Echo fidelity is not among the nine: it already has its own end-to-end
// test (T18.3, echo_fidelity_test.go) and is deliberately kept passing here,
// by scripting each collaborator's reply to match its stub's declared
// response verbatim, so it never dominates the verdict these tests are
// actually checking.

import (
	"testing"

	"mosaic-agent-test/internal/cli"
	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/harness/fake"
)

// detectionScript is the fixed subject/collaborator sequence every detection
// test in this scenario observes identically.
func detectionScript() fake.Options {
	greeter := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "greeter"}
	helper := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "helper"}

	opts := fakeOptionsFor(
		domain.HarnessCapabilities{SupportsDirectSubstitution: false},
		[]subjectStep{
			invoke("Task", "greeter"),
			invoke("Task", "helper"),
			finish(`{"agent_instance_id":"standin-subject#1","run_id":"detect-run","status_code":"SUCCESS","status_message":"Done."}`),
		},
	)
	// Each reply matches its stub's declared response verbatim (see the
	// registries under testdata/e2e/detection), so echo fidelity always
	// passes and never masks what these tests actually check. Every message
	// (including the subject's own) carries run_id, so the only
	// Communication Protocol violation these facts contain is the helper's
	// invented status code — the true protocol_violations fact every
	// protocol-violations test is declared against.
	opts.Script = map[string][]fake.Turn{
		greeter.Key(): {
			{Body: `{"agent_instance_id":"greeter#1","run_id":"detect-run","status_code":"SUCCESS","status_message":"Greeted."}`},
		},
		helper.Key(): {
			{Body: `{"agent_instance_id":"helper#2","run_id":"detect-run","status_code":"MAYBE","status_message":"Helped."}`},
		},
	}
	return opts
}

// detectionDetails supplies the task-message content the task_message
// detection tests need: only the greeter's first invocation carries a
// distinguishing phrase.
func detectionDetails() map[string][]sidecarDetail {
	greeter := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "greeter"}
	helper := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "helper"}
	return map[string][]sidecarDetail{
		greeter.Key(): {{AgentInstanceID: "greeter#1", TaskDescription: "Greet the collaborator politely."}},
		helper.Key():  {{AgentInstanceID: "helper#2", TaskDescription: "Assist without creating files."}},
	}
}

func runDetectionScenario(t *testing.T) Outcome {
	t.Helper()
	sc := Scenario{
		Dir:               "testdata/e2e/detection",
		SuitePath:         "detection.suite.yaml",
		Script:            detectionScript(),
		InvocationDetails: detectionDetails(),
	}
	return RunScenario(t, sc)
}

// detectionCase names one test in the shared scenario and what its overall
// verdict, and its named assertion's outcome, must be.
type detectionCase struct {
	testID    string
	class     domain.AssertionClass
	verdict   domain.Verdict
	assertion domain.AssertionOutcome
}

func TestDetection_EveryAssertionClass_NamesItsOwnOutcome(t *testing.T) {
	out := runDetectionScenario(t)

	// The suite mixes passing and failing tests, so the process exit code
	// must be ExitTestsFailed: at least one test regressed.
	if out.ExitCode != cli.ExitTestsFailed {
		t.Fatalf("RunScenario: exit code = %d, want %d (ExitTestsFailed)\nstdout: %s\nstderr: %s",
			out.ExitCode, cli.ExitTestsFailed, out.Stdout, out.Stderr)
	}

	cases := []detectionCase{
		{"sequence-pass", domain.ClassInvocationSequence, domain.VerdictPass, domain.AssertionPass},
		{"sequence-fail", domain.ClassInvocationSequence, domain.VerdictFail, domain.AssertionFail},
		{"artifact-created-pass", domain.ClassArtifactCreated, domain.VerdictPass, domain.AssertionPass},
		{"artifact-created-fail", domain.ClassArtifactCreated, domain.VerdictFail, domain.AssertionFail},
		{"artifact-not-created-pass", domain.ClassArtifactNotCreated, domain.VerdictPass, domain.AssertionPass},
		{"artifact-not-created-fail", domain.ClassArtifactNotCreated, domain.VerdictFail, domain.AssertionFail},
		{"final-state-pass", domain.ClassFinalPhase, domain.VerdictPass, domain.AssertionPass},
		{"final-state-pass", domain.ClassFinalStatus, domain.VerdictPass, domain.AssertionPass},
		{"final-state-fail", domain.ClassFinalPhase, domain.VerdictFail, domain.AssertionFail},
		{"final-state-last-status-fail", domain.ClassFinalStatus, domain.VerdictFail, domain.AssertionFail},
		{"execution-log-pass", domain.ClassExecutionLogAgentIDs, domain.VerdictPass, domain.AssertionPass},
		{"execution-log-pass", domain.ClassExecutionLogAllStatus, domain.VerdictPass, domain.AssertionPass},
		{"execution-log-fail", domain.ClassExecutionLogAgentIDs, domain.VerdictFail, domain.AssertionFail},
		{"execution-log-all-status-fail", domain.ClassExecutionLogAllStatus, domain.VerdictFail, domain.AssertionFail},
		{"task-message-pass", domain.ClassTaskMessage, domain.VerdictPass, domain.AssertionPass},
		{"task-message-fail", domain.ClassTaskMessage, domain.VerdictFail, domain.AssertionFail},
		{"protocol-violations-pass", domain.ClassProtocolViolations, domain.VerdictPass, domain.AssertionPass},
		{"protocol-violations-fail", domain.ClassProtocolViolations, domain.VerdictFail, domain.AssertionFail},
	}

	byID := map[string]wireTestReportDoc{}
	for _, tr := range out.Result.Tests {
		byID[tr.TestID] = tr
	}
	wantTestIDs := map[string]bool{}
	for _, c := range cases {
		wantTestIDs[c.testID] = true
	}
	if len(byID) != len(wantTestIDs) {
		t.Fatalf("Result.Tests has %d entries, want %d\nstdout: %s", len(byID), len(wantTestIDs), out.Stdout)
	}

	for _, c := range cases {
		t.Run(c.testID+"/"+string(c.class), func(t *testing.T) {
			tr, ok := byID[c.testID]
			if !ok {
				t.Fatalf("no result for test %q\nstdout: %s", c.testID, out.Stdout)
			}
			if got := tr.Aggregate.Verdict; got != string(c.verdict) {
				t.Errorf("test %q: aggregate verdict = %q, want %q\nstdout: %s", c.testID, got, c.verdict, out.Stdout)
			}
			if len(tr.Runs) != 1 {
				t.Fatalf("test %q: Runs = %+v, want exactly one run", c.testID, tr.Runs)
			}
			var found bool
			for _, a := range tr.Runs[0].Assertions {
				if a.Class != string(c.class) {
					continue
				}
				found = true
				if a.Outcome != string(c.assertion) {
					t.Errorf("test %q: assertion %s outcome = %q, want %q\nstdout: %s",
						c.testID, c.class, a.Outcome, c.assertion, out.Stdout)
				}
			}
			if !found {
				t.Errorf("test %q: no %s assertion found among %+v", c.testID, c.class, tr.Runs[0].Assertions)
			}
		})
	}
}
