package suite_test

// Tests for the carry-through of SubjectModel and StubModel from
// domain.TestResult into report.RunReport inside suite.go.
//
// These tests close the AC14.2 gap identified in the Stage 14 review: every
// other layer (RunEvidence → TestResult, RunReport rendering) is covered
// elsewhere, but the struct literal in suite.go that copies TestResult fields
// onto RunReport is the single place the chain can be broken by an omission
// that all existing tests would miss.

import (
	"context"
	"testing"
)

// TestSuite_SubjectModelCarriedFromTestResultToRunReport asserts that the
// subject model recorded in a run's domain.TestResult is copied into the
// corresponding report.RunReport in the returned report.Result.
//
// The scripted runner produces RunEvidence with SubjectModel set. The suite
// passes that evidence through evaluate.Evaluate (which carries SubjectModel
// onto TestResult), then builds a RunReport from the TestResult. If suite.go
// omits SubjectModel from the RunReport struct literal, this test fails even
// though every other Stage 14 test passes — making the omission visible
// rather than silent.
func TestSuite_SubjectModelCarriedFromTestResultToRunReport(t *testing.T) {
	const wantModel = "claude-opus-4-5"

	// Arrange
	runner := newScriptedRunner()
	ev := passingEvidence()
	ev.SubjectModel = wantModel
	runner.scriptFor("model-test", scriptedOutcome{evidence: ev})

	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("model-test", 1, 1.0))

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	if len(result.Tests) != 1 {
		t.Fatalf("result.Tests length = %d, want 1", len(result.Tests))
	}
	runs := result.Tests[0].Runs
	if len(runs) != 1 {
		t.Fatalf("result.Tests[0].Runs length = %d, want 1", len(runs))
	}
	if got := runs[0].SubjectModel; got != wantModel {
		t.Errorf(
			"RunReport.SubjectModel = %q, want %q\n"+
				"The RunReport struct literal in suite.go must copy SubjectModel "+
				"from the domain.TestResult returned by evaluate.Evaluate, "+
				"exactly as SubjectVersion is copied. An omission here leaves "+
				"AC14.2 undetected by all other Stage 14 tests.",
			got, wantModel,
		)
	}
}

// TestSuite_StubModelCarriedFromTestResultToRunReport asserts that the stub
// model recorded in a run's domain.TestResult is copied into the corresponding
// report.RunReport in the returned report.Result.
//
// Companion to TestSuite_SubjectModelCarriedFromTestResultToRunReport: the
// two fields travel the same path and both must be copied. Having separate
// tests for each means a partial fix (copying only one field) is caught
// immediately.
func TestSuite_StubModelCarriedFromTestResultToRunReport(t *testing.T) {
	const wantModel = "claude-haiku-3-5"

	// Arrange
	runner := newScriptedRunner()
	ev := passingEvidence()
	ev.StubModel = wantModel
	runner.scriptFor("model-test", scriptedOutcome{evidence: ev})

	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("model-test", 1, 1.0))

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	if len(result.Tests) != 1 {
		t.Fatalf("result.Tests length = %d, want 1", len(result.Tests))
	}
	runs := result.Tests[0].Runs
	if len(runs) != 1 {
		t.Fatalf("result.Tests[0].Runs length = %d, want 1", len(runs))
	}
	if got := runs[0].StubModel; got != wantModel {
		t.Errorf(
			"RunReport.StubModel = %q, want %q\n"+
				"The RunReport struct literal in suite.go must copy StubModel "+
				"from the domain.TestResult returned by evaluate.Evaluate, "+
				"exactly as SubjectVersion is copied. An omission here leaves "+
				"AC14.2 undetected by all other Stage 14 tests.",
			got, wantModel,
		)
	}
}

// TestSuite_BothModelsCarriedFromTestResultToRunReport asserts that both
// SubjectModel and StubModel are simultaneously present on the RunReport when
// both are set on the evidence. This catches a copy where only one field is
// copied at the expense of the other (e.g., a typo that assigns SubjectModel
// to both target fields).
func TestSuite_BothModelsCarriedFromTestResultToRunReport(t *testing.T) {
	const wantSubject = "claude-opus-4-5"
	const wantStub = "claude-haiku-3-5"

	// Arrange
	runner := newScriptedRunner()
	ev := passingEvidence()
	ev.SubjectModel = wantSubject
	ev.StubModel = wantStub
	runner.scriptFor("model-test", scriptedOutcome{evidence: ev})

	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("model-test", 1, 1.0))

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	if len(result.Tests) == 0 {
		t.Fatalf("result.Tests is empty")
	}
	runs := result.Tests[0].Runs
	if len(runs) == 0 {
		t.Fatalf("result.Tests[0].Runs is empty")
	}
	run := runs[0]
	if run.SubjectModel != wantSubject || run.StubModel != wantStub {
		t.Errorf(
			"RunReport.SubjectModel = %q, RunReport.StubModel = %q; want %q and %q\n"+
				"Both fields must be copied independently in the RunReport struct "+
				"literal. A typo that copies SubjectModel into both, or StubModel "+
				"into both, is caught only by this combined assertion.",
			run.SubjectModel, run.StubModel, wantSubject, wantStub,
		)
	}
}
