package evaluate_test

// Shared builders for the verdict-engine test suite in this package. Every
// builder returns a value that already passes a minimal, otherwise-clean
// run: individual tests start from one of these and mutate only the field
// their case is about, so a failure is attributable to the one thing the
// test changed.

import (
	"time"

	"mosaic-agent-test/internal/domain"
)

func identity(tool, agent string) domain.CollaboratorIdentity {
	return domain.CollaboratorIdentity{ToolName: tool, AgentIdentity: agent}
}

func researcher() domain.CollaboratorIdentity  { return identity("Task", "researcher") }
func reviewer() domain.CollaboratorIdentity    { return identity("Task", "reviewer") }
func testWriter() domain.CollaboratorIdentity  { return identity("Task", "test-writer") }
func implementer() domain.CollaboratorIdentity { return identity("Task", "implementer") }

// startRecord builds a start record for a collaborator invocation, whose
// message parsed cleanly.
func startRecord(seq, ordinal int, id domain.CollaboratorIdentity, group string, at time.Time, msg domain.TaskMessage) domain.LogRecord {
	msg.Extraction = domain.ExtractionParsed
	return domain.LogRecord{
		Kind:      domain.RecordStart,
		Seq:       seq,
		Ordinal:   ordinal,
		Identity:  id,
		Group:     group,
		Timestamp: at,
		Outcome:   domain.OutcomeRewritePrompt,
		Message:   &msg,
	}
}

// endRecord builds a matching end record with a faithful echo by default.
func endRecord(seq, ordinal int, id domain.CollaboratorIdentity, at time.Time) domain.LogRecord {
	return domain.LogRecord{
		Kind:      domain.RecordEnd,
		Seq:       seq,
		Ordinal:   ordinal,
		Identity:  id,
		Timestamp: at,
		Echo:      &domain.EchoOutcome{Match: true},
	}
}

// mismatchedEndRecord builds a matching end record whose echo did not match
// the stub it was given.
func mismatchedEndRecord(seq, ordinal int, id domain.CollaboratorIdentity, at time.Time) domain.LogRecord {
	rec := endRecord(seq, ordinal, id, at)
	rec.Echo = &domain.EchoOutcome{Match: false, Expected: []byte(`{"status_code":"SUCCESS"}`), Observed: "something else", Diff: "status_code differs"}
	return rec
}

// plainMessage is a minimal, well-formed task message for a collaborator
// invocation, cheap to override field-by-field per test.
func plainMessage(agentInstanceID string) domain.TaskMessage {
	return domain.TaskMessage{
		AgentInstanceID: agentInstanceID,
		RunID:           "run-1",
		TaskDescription: "Research the topic and write findings.",
		InputArtifacts:  []string{"Requirements.md"},
		OutputArtifacts: []string{"Research.md"},
	}
}

// baseDefinition is a minimal test definition: subagent layer, not
// negative, no declared assertions. Tests add the assertion(s) their case
// is about.
func baseDefinition() domain.TestDefinition {
	return domain.TestDefinition{
		SchemaVersion: 1,
		Name:          "example",
		Layer:         domain.LayerSubagent,
	}
}

// baseKey is the RunKey every builder below uses, so tests can assert
// Evaluate carries it through unchanged.
func baseKey() domain.RunKey {
	return domain.RunKey{RunID: "run-1", TestName: "example", RunNumber: 1}
}

// baseEvidence is a minimal, internally consistent, otherwise-clean run: one
// collaborator invocation with a faithful echo, the subject completed
// normally, and the orchestration document was read successfully. Every
// declared assertion in a test built on top of this evidence describes
// exactly the one thing that test is about.
func baseEvidence() domain.RunEvidence {
	start := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Second)
	return domain.RunEvidence{
		Definition: baseDefinition(),
		Key:        baseKey(),
		Records: []domain.LogRecord{
			startRecord(1, 1, researcher(), "", start, plainMessage("researcher#1")),
			endRecord(1, 1, researcher(), end),
		},
		SubjectResult: domain.SubjectResult{
			ProtocolMessage: wellFormedResponse,
			Disposition:     domain.DispositionCompleted,
			ExitCode:        0,
			Duration:        end.Sub(start),
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
		LogRoot:                   "/sandbox/subject/OrchestrationLogs/20260807T120000Z-aaaa",
		LogsProduced:              true,
	}
}

const wellFormedResponse = `{
  "agent_instance_id": "orchestrator#1",
  "run_id": "run-1",
  "status_code": "SUCCESS",
  "status_message": "Run completed."
}`

// hasAssertion finds the AssertionResult for class/target in got, so a test
// can assert on one class without depending on slice order or on the
// presence of assertions for other classes.
func findAssertion(t interface {
	Helper()
	Fatalf(string, ...any)
}, results []domain.AssertionResult, class domain.AssertionClass, target string) domain.AssertionResult {
	t.Helper()
	for _, r := range results {
		if r.Class == class && r.Target == target {
			return r
		}
	}
	t.Fatalf("no AssertionResult found for class %q target %q in %+v", class, target, results)
	return domain.AssertionResult{}
}

func hasReason(reasons []domain.FailureReason, want domain.FailureReason) bool {
	for _, r := range reasons {
		if r == want {
			return true
		}
	}
	return false
}

func hasConditionKind(conditions []domain.RunCondition, want domain.RunConditionKind) bool {
	for _, c := range conditions {
		if c.Kind == want {
			return true
		}
	}
	return false
}

// findCondition returns the first RunCondition of kind in conditions, so a
// test can assert on its Detail without depending on slice order.
func findCondition(t interface {
	Helper()
	Fatalf(string, ...any)
}, conditions []domain.RunCondition, kind domain.RunConditionKind) domain.RunCondition {
	t.Helper()
	for _, c := range conditions {
		if c.Kind == kind {
			return c
		}
	}
	t.Fatalf("no RunCondition found for kind %q in %+v", kind, conditions)
	return domain.RunCondition{}
}
