package suite_test

// Tests for the carry-through of TestVersion (Definition.Version) and
// NumericID (Definition.NumericID) from domain.TestResult into
// report.RunReport and report.TestReport inside suite.go.
//
// These tests close the gap identified in the Stage 2 review: every other
// layer (RunEvidence -> TestResult via evaluate.Evaluate, and TestResult ->
// AggregateResult via evaluate.Aggregate) is covered by tests in the evaluate
// package, but the struct literal in suite.go that copies TestResult fields
// onto RunReport is the single place the chain can be broken by an omission
// that all existing Stage 2 tests would miss.
//
// The wire tests in internal/report bypass suite.go by constructing RunReport
// directly with hardcoded values, so a suite.go that never copies these fields
// would let every wire test pass while producing zero values in the report JSON.

import (
	"context"
	"testing"

	"mosaic-agent-test/internal/domain"
)

// TestSuite_TestVersionCarriedFromTestResultToRunReport asserts that the
// content version recorded in a run's domain.TestResult is copied into the
// corresponding report.RunReport in the returned report.Result.
//
// The scripted runner produces RunEvidence with Definition.Version set.
// evaluate.Evaluate carries Definition.Version onto TestResult.Version; suite.go
// must then copy TestResult.Version into RunReport.TestVersion. If suite.go
// omits this copy, this test fails even though every other Stage 2 test passes,
// making the omission visible rather than silent.
func TestSuite_TestVersionCarriedFromTestResultToRunReport(t *testing.T) {
	const wantVersion = 3

	// Arrange
	runner := newScriptedRunner()
	ev := passingEvidence()
	ev.Definition = domain.TestDefinition{
		Name:    "version-test",
		Version: wantVersion,
		Layer:   domain.LayerSubagent,
	}
	runner.scriptFor("version-test", scriptedOutcome{evidence: ev})

	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("version-test", 1, 1.0))

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
	if got := runs[0].TestVersion; got != wantVersion {
		t.Errorf(
			"RunReport.TestVersion = %d, want %d\n"+
				"The RunReport struct literal in suite.go must copy Version "+
				"from the domain.TestResult returned by evaluate.Evaluate into "+
				"RunReport.TestVersion. An omission here produces a silently-zero "+
				"test_version in the report JSON, undetected by all other Stage 2 tests.",
			got, wantVersion,
		)
	}
}

// TestSuite_NumericIDCarriedFromTestResultToRunReport asserts that the
// numeric identity recorded in a run's domain.TestResult is copied into the
// corresponding report.RunReport in the returned report.Result.
//
// Companion to TestSuite_TestVersionCarriedFromTestResultToRunReport: NumericID
// travels the same suite.go copy path and must be independently tested so that
// a partial fix (copying only TestVersion) is immediately visible.
func TestSuite_NumericIDCarriedFromTestResultToRunReport(t *testing.T) {
	const wantNumericID = 42

	// Arrange
	runner := newScriptedRunner()
	ev := passingEvidence()
	ev.Definition = domain.TestDefinition{
		Name:      "numericid-test",
		NumericID: wantNumericID,
		Layer:     domain.LayerSubagent,
	}
	runner.scriptFor("numericid-test", scriptedOutcome{evidence: ev})

	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("numericid-test", 1, 1.0))

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
	if got := runs[0].NumericID; got != wantNumericID {
		t.Errorf(
			"RunReport.NumericID = %d, want %d\n"+
				"The RunReport struct literal in suite.go must copy NumericID "+
				"from the domain.TestResult returned by evaluate.Evaluate into "+
				"RunReport.NumericID. An omission here produces a silently-zero "+
				"numeric test_id in the report JSON, undetected by all other Stage 2 tests.",
			got, wantNumericID,
		)
	}
}

// TestSuite_NumericIDCarriedToTestReport asserts that the numeric identity is
// present at the per-test level (report.TestReport.NumericID) as well as the
// per-run level (report.RunReport.NumericID).
//
// TestReport.NumericID is populated from domain.AggregateResult.NumericID,
// which suite.go derives from the aggregate. If suite.go omits this copy, the
// per-test NumericID is zero in the report JSON even when the per-run NumericID
// is correct. The two assertions cover distinct copy sites in suite.go.
func TestSuite_NumericIDCarriedToTestReport(t *testing.T) {
	const wantNumericID = 42

	// Arrange
	runner := newScriptedRunner()
	ev := passingEvidence()
	ev.Definition = domain.TestDefinition{
		Name:      "numericid-testreport-test",
		NumericID: wantNumericID,
		Layer:     domain.LayerSubagent,
	}
	runner.scriptFor("numericid-testreport-test", scriptedOutcome{evidence: ev})

	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("numericid-testreport-test", 1, 1.0))

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	if len(result.Tests) != 1 {
		t.Fatalf("result.Tests length = %d, want 1", len(result.Tests))
	}
	if got := result.Tests[0].NumericID; got != wantNumericID {
		t.Errorf(
			"TestReport.NumericID = %d, want %d\n"+
				"The TestReport struct literal in suite.go must copy NumericID from "+
				"domain.AggregateResult.NumericID into TestReport.NumericID. "+
				"An omission here produces a silently-zero numeric test_id at the "+
				"per-test level in the report JSON, undetected by all other Stage 2 tests.",
			got, wantNumericID,
		)
	}
}

// TestSuite_BothVersionAndNumericIDCarriedToRunReport asserts that both
// TestVersion and NumericID are simultaneously present on the RunReport when
// both are set on the evidence. This catches a copy where only one field is
// copied at the expense of the other (e.g., a typo that assigns Version to
// both target fields).
func TestSuite_BothVersionAndNumericIDCarriedToRunReport(t *testing.T) {
	const wantVersion = 3
	const wantNumericID = 42

	// Arrange
	runner := newScriptedRunner()
	ev := passingEvidence()
	ev.Definition = domain.TestDefinition{
		Name:      "both-fields-test",
		Version:   wantVersion,
		NumericID: wantNumericID,
		Layer:     domain.LayerSubagent,
	}
	runner.scriptFor("both-fields-test", scriptedOutcome{evidence: ev})

	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("both-fields-test", 1, 1.0))

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
	if run.TestVersion != wantVersion || run.NumericID != wantNumericID {
		t.Errorf(
			"RunReport.TestVersion = %d, RunReport.NumericID = %d; want %d and %d\n"+
				"Both fields must be copied independently in the RunReport struct "+
				"literal. A typo that copies Version into both, or NumericID into "+
				"both, is caught only by this combined assertion.",
			run.TestVersion, run.NumericID, wantVersion, wantNumericID,
		)
	}
}
