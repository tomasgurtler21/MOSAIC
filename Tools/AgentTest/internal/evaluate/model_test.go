package evaluate_test

// Tests for SubjectModel and StubModel being carried from RunEvidence through
// evaluate.Evaluate into TestResult (TDD RED phase).
//
// evaluate.Evaluate projects domain.RunEvidence into domain.TestResult. Like
// SubjectVersion before them, SubjectModel and StubModel must pass through
// unchanged — neither the verdict engine nor any assertion class touches them,
// and the report layer reads them from TestResult.
//
// These tests start from baseEvidence (a clean, otherwise-passing run) and
// set SubjectModel or StubModel, then assert that the field appears unchanged
// on the returned TestResult. They say nothing about which assertions fired or
// what the verdict was — only the field carry-through is exercised here.
//
// The design contract (ContractsDesign.md § domain.RunEvidence and
// domain.TestResult) states that evaluate.Evaluate copies both fields through
// on every return path, including the spawn-failure early return — a run that
// never started still reports which model it was going to use.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// --- SubjectModel carry-through ---

// TestEvaluate_SubjectModelCarriedThroughToTestResult asserts that when
// RunEvidence.SubjectModel is non-empty, the same value appears on the
// returned TestResult.SubjectModel. The verdict engine must not inspect or
// transform this field.
func TestEvaluate_SubjectModelCarriedThroughToTestResult(t *testing.T) {
	// Arrange
	ev := baseEvidence()
	const wantModel = "claude-opus-4-5"
	ev.SubjectModel = wantModel

	// Act
	result := evaluate.Evaluate(ev)

	// Assert
	if result.SubjectModel != wantModel {
		t.Errorf("TestResult.SubjectModel = %q, want %q; "+
			"evaluate.Evaluate must carry SubjectModel from RunEvidence into "+
			"TestResult unchanged, exactly as SubjectVersion is carried",
			result.SubjectModel, wantModel)
	}
}

// TestEvaluate_AbsentSubjectModelCarriedAsEmpty asserts that when
// RunEvidence.SubjectModel is empty — meaning no model was recorded for this
// run — TestResult.SubjectModel is also empty. The "" → "unknown" display
// mapping belongs to the report layer, not the verdict engine.
func TestEvaluate_AbsentSubjectModelCarriedAsEmpty(t *testing.T) {
	// Arrange
	ev := baseEvidence()
	ev.SubjectModel = "" // no model recorded — a legal state

	// Act
	result := evaluate.Evaluate(ev)

	// Assert
	if result.SubjectModel != "" {
		t.Errorf("TestResult.SubjectModel = %q, want empty string; "+
			"evaluate.Evaluate must not transform an absent model — "+
			"the '' → 'unknown' mapping is the report layer's responsibility, not the verdict engine's",
			result.SubjectModel)
	}
}

// TestEvaluate_SubjectModelDoesNotAffectVerdict asserts that a non-empty
// SubjectModel has no effect on the verdict. The subject model is informational
// only; it is never a pass/fail criterion.
func TestEvaluate_SubjectModelDoesNotAffectVerdict(t *testing.T) {
	// Arrange — otherwise-passing evidence with a model set
	ev := baseEvidence()
	ev.SubjectModel = "claude-opus-4-5"

	// Act
	result := evaluate.Evaluate(ev)

	// Assert: verdict must remain PASS; a model present is not a failure
	if result.Verdict != domain.VerdictPass {
		t.Errorf("Evaluate with SubjectModel set produced verdict %q, want %q; "+
			"SubjectModel is informational and must never influence the verdict",
			result.Verdict, domain.VerdictPass)
	}
}

// TestEvaluate_SubjectModelCarriedThroughOnSpawnFailurePath asserts that when
// the subject process refused to start (DispositionSpawnFailed), SubjectModel
// is still carried from RunEvidence into TestResult. A run that never started
// still reports which model it was going to use — this is the explicit design
// contract from the spawn-failure early return path.
func TestEvaluate_SubjectModelCarriedThroughOnSpawnFailurePath(t *testing.T) {
	// Arrange — spawn-failed evidence with a model set
	ev := spawnFailedEvidence("Authentication error: not logged in", 1)
	const wantModel = "claude-opus-4-5"
	ev.SubjectModel = wantModel

	// Act
	result := evaluate.Evaluate(ev)

	// Assert: the model must still appear on the result despite the early return
	if result.SubjectModel != wantModel {
		t.Errorf("TestResult.SubjectModel = %q, want %q on spawn-failure path; "+
			"evaluate.Evaluate must copy SubjectModel through on every return path, "+
			"including the spawn-failure early return — a run that never started still "+
			"reports which model it was going to use",
			result.SubjectModel, wantModel)
	}
}

// --- StubModel carry-through ---

// TestEvaluate_StubModelCarriedThroughToTestResult asserts that when
// RunEvidence.StubModel is non-empty, the same value appears on the returned
// TestResult.StubModel. The verdict engine must not inspect or transform this
// field.
func TestEvaluate_StubModelCarriedThroughToTestResult(t *testing.T) {
	// Arrange
	ev := baseEvidence()
	const wantModel = "claude-haiku-3-5"
	ev.StubModel = wantModel

	// Act
	result := evaluate.Evaluate(ev)

	// Assert
	if result.StubModel != wantModel {
		t.Errorf("TestResult.StubModel = %q, want %q; "+
			"evaluate.Evaluate must carry StubModel from RunEvidence into "+
			"TestResult unchanged, exactly as SubjectVersion is carried",
			result.StubModel, wantModel)
	}
}

// TestEvaluate_AbsentStubModelCarriedAsEmpty asserts that when
// RunEvidence.StubModel is empty — meaning no stub model was recorded — then
// TestResult.StubModel is also empty. The "" → "unknown" display mapping
// belongs to the report layer, not the verdict engine.
func TestEvaluate_AbsentStubModelCarriedAsEmpty(t *testing.T) {
	// Arrange
	ev := baseEvidence()
	ev.StubModel = "" // no stub model recorded — a legal state

	// Act
	result := evaluate.Evaluate(ev)

	// Assert
	if result.StubModel != "" {
		t.Errorf("TestResult.StubModel = %q, want empty string; "+
			"evaluate.Evaluate must not transform an absent stub model — "+
			"the '' → 'unknown' mapping is the report layer's responsibility, not the verdict engine's",
			result.StubModel)
	}
}

// TestEvaluate_StubModelDoesNotAffectVerdict asserts that a non-empty
// StubModel has no effect on the verdict. The stub model is informational
// only; it is never a pass/fail criterion.
func TestEvaluate_StubModelDoesNotAffectVerdict(t *testing.T) {
	// Arrange — otherwise-passing evidence with a stub model set
	ev := baseEvidence()
	ev.StubModel = "claude-haiku-3-5"

	// Act
	result := evaluate.Evaluate(ev)

	// Assert: verdict must remain PASS; a stub model present is not a failure
	if result.Verdict != domain.VerdictPass {
		t.Errorf("Evaluate with StubModel set produced verdict %q, want %q; "+
			"StubModel is informational and must never influence the verdict",
			result.Verdict, domain.VerdictPass)
	}
}

// TestEvaluate_StubModelCarriedThroughOnSpawnFailurePath asserts that when
// the subject process refused to start (DispositionSpawnFailed), StubModel is
// still carried from RunEvidence into TestResult. A run that never started
// still reports which model its stubs were going to use — this is the explicit
// design contract from the spawn-failure early return path.
func TestEvaluate_StubModelCarriedThroughOnSpawnFailurePath(t *testing.T) {
	// Arrange — spawn-failed evidence with a stub model set
	ev := spawnFailedEvidence("Authentication error: not logged in", 1)
	const wantModel = "claude-haiku-3-5"
	ev.StubModel = wantModel

	// Act
	result := evaluate.Evaluate(ev)

	// Assert: the stub model must still appear on the result despite the early return
	if result.StubModel != wantModel {
		t.Errorf("TestResult.StubModel = %q, want %q on spawn-failure path; "+
			"evaluate.Evaluate must copy StubModel through on every return path, "+
			"including the spawn-failure early return — a run that never started still "+
			"reports which stub model it was going to use",
			result.StubModel, wantModel)
	}
}
