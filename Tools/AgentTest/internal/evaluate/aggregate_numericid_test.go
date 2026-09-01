package evaluate_test

// Tests verifying that evaluate.Aggregate populates AggregateResult.NumericID
// from the first TestResult's NumericID field.
//
// The design specifies the first-result-wins pattern, mirroring the existing
// pattern already used for TestID/TestName:
//   if out.NumericID == 0 { out.NumericID = r.NumericID }
//
// These tests compile against the current code (NumericID exists on
// AggregateResult after the Stage 2 contract additions) but FAIL because
// evaluate.Aggregate does not yet populate it. They will pass once I2.10
// updates aggregate.go.

import (
	"testing"

	"mosaic-agent-test/internal/domain"
	"mosaic-agent-test/internal/evaluate"
)

// passResultWithNumericID builds a TestResult that passed and carries a
// specific NumericID, so aggregate tests can verify population.
func passResultWithNumericID(n int, numericID int) domain.TestResult {
	r := passResult(n)
	r.NumericID = numericID
	return r
}

// TestAggregate_PopulatesNumericIDFromFirstResult verifies that Aggregate
// sets AggregateResult.NumericID from the first TestResult's NumericID. The
// numeric ID is the stable identity for cross-rename tracking; a report that
// omits it silently breaks any downstream consumer that uses it to correlate
// results across definition renames.
func TestAggregate_PopulatesNumericIDFromFirstResult(t *testing.T) {
	results := []domain.TestResult{
		passResultWithNumericID(1, 42),
		passResultWithNumericID(2, 42),
	}
	policy := domain.RepetitionPolicy{Repetitions: 2, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.NumericID != 42 {
		t.Errorf("AggregateResult.NumericID = %d, want 42 — Aggregate must populate NumericID from the first TestResult's NumericID using the first-result-wins pattern", got.NumericID)
	}
}

// TestAggregate_NumericID_FirstResultWins verifies the first-result-wins
// pattern: when multiple results carry the same NumericID (as they should,
// since they all came from the same definition), the first one determines the
// aggregate value. This mirrors the TestID/TestName population rule.
func TestAggregate_NumericID_FirstResultWins(t *testing.T) {
	// In practice all repetitions of a test share the same NumericID. The
	// first-result-wins pattern is defined to handle the general case.
	first := passResultWithNumericID(1, 42)
	second := passResultWithNumericID(2, 42)
	results := []domain.TestResult{first, second}
	policy := domain.RepetitionPolicy{Repetitions: 2, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.NumericID != 42 {
		t.Errorf("AggregateResult.NumericID = %d, want 42", got.NumericID)
	}
}

// TestAggregate_NumericID_ZeroResultsLeavesZero verifies that when the input
// results slice is empty, NumericID stays at zero — there is no result to
// take the value from. This is a degenerate case but must not panic.
func TestAggregate_NumericID_ZeroResultsLeavesZero(t *testing.T) {
	policy := domain.RepetitionPolicy{Repetitions: 0, PassRate: 1.0}

	got := evaluate.Aggregate(nil, policy)

	if got.NumericID != 0 {
		t.Errorf("AggregateResult.NumericID = %d, want 0 — no results means no NumericID to carry through", got.NumericID)
	}
}

// TestAggregate_NumericID_ZeroValueResultLeavesZero verifies that when all
// results carry NumericID=0 (as test helpers that bypass the parser do),
// the aggregate NumericID is also 0. Zero is the correct value here; the
// aggregate must not substitute a default.
func TestAggregate_NumericID_ZeroValueResultLeavesZero(t *testing.T) {
	// passResult helpers in aggregate_test.go do not set NumericID, so they
	// carry 0.
	results := []domain.TestResult{passResult(1), passResult(2)}
	policy := domain.RepetitionPolicy{Repetitions: 2, PassRate: 1.0}

	got := evaluate.Aggregate(results, policy)

	if got.NumericID != 0 {
		t.Errorf("AggregateResult.NumericID = %d, want 0 — results without a NumericID must aggregate to 0, not a substitute value", got.NumericID)
	}
}
