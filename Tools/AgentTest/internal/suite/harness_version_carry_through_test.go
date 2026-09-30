package suite_test

// HarnessVersion is copied from the evaluated run into the report's run entry.

import (
	"context"
	"testing"
)

func TestSuite_HarnessVersionCarriedFromEvidenceToRunReport(t *testing.T) {
	// Arrange
	runner := newScriptedRunner()
	ev := passingEvidence()
	ev.HarnessVersion = "2.1.284"
	runner.scriptFor("version-test", scriptedOutcome{evidence: ev})
	s := newSuite(runner, newFakeClock(), &recordingSink{})
	plan := buildPlan(resolvedTest("version-test", 1, 1.0))

	// Act
	result, err := runSuite(t, s, context.Background(), plan)

	// Assert
	if err != nil {
		t.Fatalf("Suite.Run returned an error: %v", err)
	}
	if len(result.Tests) != 1 || len(result.Tests[0].Runs) != 1 {
		t.Fatalf("want one test with one run, got %d tests", len(result.Tests))
	}
	if got := result.Tests[0].Runs[0].HarnessVersion; got != "2.1.284" {
		t.Errorf("RunReport.HarnessVersion = %q, want %q", got, "2.1.284")
	}
}
