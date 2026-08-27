package suite_test

// Tests for echo-mismatch retry behaviour.
//
// Behaviour specified:
//  - A repetition whose first attempt ends with an echo mismatch is retried
//    exactly once, exactly as a spawn-failure or state-integrity fault is.
//  - The retry runs in the same scheduling slot as the original attempt and
//    gets its own, distinct run identity (and therefore its own sandbox).
//  - When both attempts produce an echo mismatch, no further attempt is made.
//    The aggregate reports InfrastructureFailure=true.

import (
	"context"
	"testing"
	"time"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/suite"
)

// echoMismatchEvidence returns evidence that evaluate.Evaluate turns into
// VerdictFail with ReasonEchoMismatch, modelling a run where a stubbed
// collaborator did not echo its stub faithfully. It is built locally because
// the equivalent helper in the evaluate_test package is not importable from
// this package.
//
// The evidence satisfies all declared assertions (FinalPhase, FinalStatus)
// so ReasonEchoMismatch is the only failure source — there is no state-integrity
// event and no spawn-failed disposition, both of which would precede echo_mismatch
// in the exclusion precedence chain.
func echoMismatchEvidence() domain.RunEvidence {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	finalPhase := "complete"
	finalStatus := "SUCCESS"

	collaborator := domain.CollaboratorIdentity{ToolName: "Task", AgentIdentity: "researcher"}

	startMsg := domain.TaskMessage{
		AgentInstanceID: "researcher#1",
		RunID:           "run-echo-mismatch",
		TaskDescription: "Research the topic and write findings.",
		InputArtifacts:  []string{"Requirements.md"},
		OutputArtifacts: []string{"Research.md"},
		Extraction:      domain.ExtractionParsed,
	}

	// Start record matches the collaborator invocation; echo comparison fires on
	// the end record.
	startRec := domain.LogRecord{
		Kind:      domain.RecordStart,
		Seq:       1,
		Ordinal:   1,
		Identity:  collaborator,
		Timestamp: start,
		Outcome:   domain.OutcomeRewritePrompt,
		Message:   &startMsg,
	}

	// End record carries a mismatched echo: the collaborator did not reproduce
	// the stub it was given. Match=false is the signal evaluate.Evaluate uses to
	// set ReasonEchoMismatch.
	endRec := domain.LogRecord{
		Kind:      domain.RecordEnd,
		Seq:       1,
		Ordinal:   1,
		Identity:  collaborator,
		Timestamp: start.Add(time.Second),
		Echo: &domain.EchoOutcome{
			Match:    false,
			Expected: []byte(`{"status_code":"SUCCESS"}`),
			Observed: "something else",
			Diff:     "status_code differs",
		},
	}

	// A well-formed subagent protocol response so the subject's own protocol
	// check passes and does not add unrelated failure reasons.
	const subjectProtocolMsg = `{
  "agent_instance_id": "orchestrator#1",
  "run_id": "run-echo-mismatch",
  "status_code": "SUCCESS",
  "status_message": "Run completed."
}`

	return domain.RunEvidence{
		Definition: domain.TestDefinition{
			SchemaVersion: 1,
			Name:          "echo-mismatch-test",
			Layer:         domain.LayerSubagent,
			Assertions: domain.Assertions{
				// FinalPhase and FinalStatus match Orchestration below, so these
				// assertions pass and do not add ReasonAssertion to the result.
				FinalPhase:  &finalPhase,
				FinalStatus: &finalStatus,
			},
		},
		Records: []domain.LogRecord{startRec, endRec},
		SubjectResult: domain.SubjectResult{
			ProtocolMessage: subjectProtocolMsg,
			Disposition:     domain.DispositionCompleted, // not spawn_failed
			ExitCode:        0,
			Duration:        time.Second,
		},
		Orchestration: domain.OrchestrationState{
			Present:    true,
			Phase:      "complete",
			LastStatus: "SUCCESS",
		},
		SnapshotFiles:             []string{"Research.md"},
		ProtocolViolations:        map[domain.ViolationClassKey]int{},
		SubjectProtocolViolations: map[domain.ViolationClassKey]int{},
		PeakConcurrency:           map[string]int{},
		Cost:                      domain.CostReport{TotalUSD: 0.05, Attribution: domain.AttributionAttributed},
		Duration:                  5 * time.Second,
		LogsProduced:              true,
	}
}

// ---------------------------------------------------------------------------
// Echo-mismatch is retried exactly once with a distinct identity
// ---------------------------------------------------------------------------

// TestSuiteRun_EchoMismatch_IsRetried_ExactlyOnce verifies that a repetition
// whose first attempt ends with an echo mismatch is retried exactly once: the
// runner is called twice for that repetition and the second call carries a
// distinct run identity from the first.
func TestSuiteRun_EchoMismatch_IsRetried_ExactlyOnce(t *testing.T) {
	runner := newScriptedRunner()
	runner.scriptFor("test-a",
		// Attempt 0: echo mismatch — triggers the retry.
		scriptedOutcome{evidence: echoMismatchEvidence()},
		// Attempt 1 (retry): passes cleanly.
		scriptedOutcome{evidence: passingEvidence()},
	)

	s := suite.New(suite.Options{
		Runner:            runner,
		Clock:             newFakeClock(),
		MaxConcurrentRuns: 1,
		RunID:             deterministicRunID,
	})

	result, err := runSuite(t, s, context.Background(), buildPlan(resolvedTest("test-a", 1, 1.0)))
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	calls := runner.callsFor("test-a")
	if len(calls) != 2 {
		t.Fatalf("runner was called %d times for test-a, want 2 (original attempt + one retry)", len(calls))
	}

	// The retry must have a distinct run identity so it gets its own sandbox.
	if calls[0].RunID == calls[1].RunID {
		t.Errorf("original attempt and retry share RunID %q — the retry must receive a distinct identity to avoid writing into the same sandbox", calls[0].RunID)
	}

	// The aggregate must reflect one excluded (echo-mismatch) and one counted
	// (successful retry).
	if len(result.Tests) != 1 {
		t.Fatalf("got %d test reports, want 1", len(result.Tests))
	}
	agg := result.Tests[0].Aggregate
	if agg.Excluded != 1 {
		t.Errorf("Excluded = %d, want 1 — the echo-mismatch attempt must be excluded from the denominator", agg.Excluded)
	}
	if agg.Counted != 1 {
		t.Errorf("Counted = %d, want 1 — the successful retry is the one that counts", agg.Counted)
	}
	if agg.Verdict != domain.VerdictPass {
		t.Errorf("Verdict = %q, want PASS — the retry passed and determines the final verdict", agg.Verdict)
	}
}

// ---------------------------------------------------------------------------
// Echo-mismatch recurrence stops after two attempts and signals infrastructure
// ---------------------------------------------------------------------------

// TestSuiteRun_EchoMismatch_Recurrence_StopsAndReportsInfrastructureFailure
// verifies that the retry count is fixed at one: if the retry also fails with
// an echo mismatch, no further attempt is made. The aggregate reports
// InfrastructureFailure=true, indicating the exclusion is systemic rather than
// a one-off fluke.
func TestSuiteRun_EchoMismatch_Recurrence_StopsAndReportsInfrastructureFailure(t *testing.T) {
	runner := newScriptedRunner()
	runner.scriptFor("test-a",
		// Attempt 0: echo mismatch.
		scriptedOutcome{evidence: echoMismatchEvidence()},
		// Attempt 1 (retry): echo mismatch again.
		scriptedOutcome{evidence: echoMismatchEvidence()},
		// Attempt 2: would be a third attempt — must never be called.
		scriptedOutcome{evidence: passingEvidence()},
	)

	s := suite.New(suite.Options{
		Runner:            runner,
		Clock:             newFakeClock(),
		MaxConcurrentRuns: 1,
		RunID:             deterministicRunID,
	})

	result, err := runSuite(t, s, context.Background(), buildPlan(resolvedTest("test-a", 1, 1.0)))
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}

	calls := runner.callsFor("test-a")
	if len(calls) != 2 {
		t.Fatalf("runner was called %d times for test-a, want exactly 2 — an echo-mismatch is retried at most once, not until it passes", len(calls))
	}

	if len(result.Tests) != 1 {
		t.Fatalf("got %d test reports, want 1", len(result.Tests))
	}
	agg := result.Tests[0].Aggregate
	if !agg.InfrastructureFailure {
		t.Errorf("InfrastructureFailure = false, want true — recurring echo mismatch on both the original and the retry must signal an infrastructure problem rather than a subject regression")
	}
}
