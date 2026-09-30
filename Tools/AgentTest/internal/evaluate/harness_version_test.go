package evaluate_test

// HarnessVersion is carried from RunEvidence into TestResult unchanged.

import (
	"testing"

	"mosaic-agent-test/internal/evaluate"
)

func TestEvaluate_HarnessVersionCarriedThrough(t *testing.T) {
	// Arrange
	ev := baseEvidence()
	ev.HarnessVersion = "2.1.284"

	// Act
	result := evaluate.Evaluate(ev)

	// Assert
	if result.HarnessVersion != "2.1.284" {
		t.Errorf("TestResult.HarnessVersion = %q, want %q", result.HarnessVersion, "2.1.284")
	}
}
