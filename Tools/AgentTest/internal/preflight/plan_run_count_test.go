package preflight_test

// Tests for Plan.TotalRuns: the single derivation of the run count a
// pre-flighted plan implies, without executing the plan.
//
// Each test constructs a Plan directly (no I/O, no filesystem) and asserts
// TotalRuns returns the expected integer. The cases cover explicit repetitions,
// absent repetitions (each test counts as one run), a mixture of the two, and
// an empty plan.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/preflight"
)

// ptr is a helper that returns a pointer to the given int value, keeping test
// data inline and readable.
func ptr(n int) *int { return &n }

func TestPlan_TotalRuns_AllExplicitRepetitions(t *testing.T) {
	// Arrange
	plan := preflight.Plan{
		Tests: []preflight.ResolvedTest{
			{Settings: domain.RunSettings{Repetitions: ptr(3)}},
			{Settings: domain.RunSettings{Repetitions: ptr(5)}},
		},
	}

	// Act
	got := plan.TotalRuns()

	// Assert
	const want = 8
	if got != want {
		t.Errorf("TotalRuns() = %d, want %d", got, want)
	}
}

func TestPlan_TotalRuns_NoRepetitionsDeclared(t *testing.T) {
	// Each test with no Repetitions setting contributes exactly one run.
	// Arrange
	plan := preflight.Plan{
		Tests: []preflight.ResolvedTest{
			{Settings: domain.RunSettings{}},
			{Settings: domain.RunSettings{}},
			{Settings: domain.RunSettings{}},
		},
	}

	// Act
	got := plan.TotalRuns()

	// Assert
	const want = 3
	if got != want {
		t.Errorf("TotalRuns() = %d, want %d (each test without repetitions must count as 1)", got, want)
	}
}

func TestPlan_TotalRuns_MixedRepetitions(t *testing.T) {
	// Some tests declare explicit repetitions; others do not. The undeclared
	// ones each contribute 1.
	// Arrange
	plan := preflight.Plan{
		Tests: []preflight.ResolvedTest{
			{Settings: domain.RunSettings{Repetitions: ptr(4)}},
			{Settings: domain.RunSettings{}},           // contributes 1
			{Settings: domain.RunSettings{Repetitions: ptr(2)}},
		},
	}

	// Act
	got := plan.TotalRuns()

	// Assert
	const want = 7 // 4 + 1 + 2
	if got != want {
		t.Errorf("TotalRuns() = %d, want %d", got, want)
	}
}

func TestPlan_TotalRuns_EmptyPlan(t *testing.T) {
	// A plan with no tests must return 0, not panic or return some default.
	// Arrange
	plan := preflight.Plan{}

	// Act
	got := plan.TotalRuns()

	// Assert
	const want = 0
	if got != want {
		t.Errorf("TotalRuns() = %d, want %d for a plan with no tests", got, want)
	}
}
