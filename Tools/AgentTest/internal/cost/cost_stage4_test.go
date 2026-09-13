package cost_test

// Tests for Stage 4: cost.Map handling of LogAnalyzer merge metadata.
//
// After the Stage 3 merge, LogAnalyzer's TotalForRun performs unknown-run
// re-attribution internally and emits two new fields in its JSON output:
//   - unknown_run_merged: count of records successfully merged
//   - unknown_run_residual: count of records that could not be attributed
//
// These tests specify the new behavior:
//   (a) When exitSuccess with merged > 0 and residual == 0, the result is a
//       normal attributed cost at the amount LogAnalyzer reported (which
//       already includes the merged records).
//   (b) When exitSuccess with residual > 0, CostReport.UnknownRunResidual is
//       populated so report.Build can emit a top-level error.
//   (c) exitNoData always returns attributed zero cost -- LogAnalyzer handles
//       the merge internally, so no data truly means no data.
//   (d) exitUsage always returns unavailable -- no UnknownBucketPresent signal.
//   (e) ParseErr cases are not affected by merge metadata.
//
// TDD RED phase: tests that verify (b) fail until mapSuccess is updated
// (I4.2) to carry RunTotal.UnknownRunResidual into domain.CostReport.

import (
	"context"
	"encoding/json"
	"testing"

	"mosaic-agent-test/internal/cost"
	"mosaic-agent-test/internal/domain"
)

// --- Map: merge metadata on exitSuccess ---

// TestMap_SuccessWithMergedUnknownRun_IsNormallyAttributed verifies that when
// LogAnalyzer reports a successful merge (merged > 0, residual == 0), the
// cost report is a normal attributed result at the full reported amount.
// The merged records are already included in Total.Money by LogAnalyzer.
func TestMap_SuccessWithMergedUnknownRun_IsNormallyAttributed(t *testing.T) {
	total := cost.RunTotal{
		Money:            cost.MoneyValue{State: "known", Amount: amount("3.75")},
		Complete:         true,
		UnknownRunMerged: 12,
		// UnknownRunResidual: 0 (zero value -- all records attributed)
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if report.Attribution != domain.AttributionAttributed {
		t.Errorf("Attribution = %q, want %q -- a fully-merged run is attributed at the total amount", report.Attribution, domain.AttributionAttributed)
	}
	if report.TotalUSD != 3.75 {
		t.Errorf("TotalUSD = %v, want 3.75 -- merged amount must be reflected in full total", report.TotalUSD)
	}
}

// TestMap_SuccessWithResidualUnknownRun_IsStillAttributed verifies that when
// LogAnalyzer reports a residual > 0, the cost report is still attributed
// (the residual is a signal, not an attribution downgrade). The residual
// causes a report-level error, not a cost attribution change.
func TestMap_SuccessWithResidualUnknownRun_IsStillAttributed(t *testing.T) {
	total := cost.RunTotal{
		Money:              cost.MoneyValue{State: "known", Amount: amount("2.00")},
		Complete:           true,
		UnknownRunResidual: 5,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if report.Attribution != domain.AttributionAttributed {
		t.Errorf("Attribution = %q, want %q -- residual unknown-run records do not downgrade cost attribution; they trigger a report-level error instead", report.Attribution, domain.AttributionAttributed)
	}
}

// TestMap_SuccessWithResidualUnknownRun_PopulatesResidualOnCostReport verifies
// that when LogAnalyzer reports residual > 0, mapSuccess carries that count
// into CostReport.UnknownRunResidual so report.Build can emit a top-level
// machine-readable error.
//
// This test FAILS until I4.2 updates mapSuccess to set UnknownRunResidual.
func TestMap_SuccessWithResidualUnknownRun_PopulatesResidualOnCostReport(t *testing.T) {
	const wantResidual = 7
	total := cost.RunTotal{
		Money:              cost.MoneyValue{State: "known", Amount: amount("1.50")},
		Complete:           true,
		UnknownRunResidual: wantResidual,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if report.UnknownRunResidual != wantResidual {
		t.Errorf("CostReport.UnknownRunResidual = %d, want %d -- residual count must be carried into CostReport for report.Build to surface as a top-level error", report.UnknownRunResidual, wantResidual)
	}
}

// TestMap_SuccessWithZeroResidual_DoesNotSetResidualOnCostReport verifies that
// when residual == 0 (fully attributed merge), CostReport.UnknownRunResidual
// is 0 -- no spurious error signal.
func TestMap_SuccessWithZeroResidual_DoesNotSetResidualOnCostReport(t *testing.T) {
	total := cost.RunTotal{
		Money:            cost.MoneyValue{State: "known", Amount: amount("0.80")},
		Complete:         true,
		UnknownRunMerged: 3,
		// UnknownRunResidual: 0
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if report.UnknownRunResidual != 0 {
		t.Errorf("CostReport.UnknownRunResidual = %d, want 0 when all unknown-run records were attributed via merge", report.UnknownRunResidual)
	}
}

// TestMap_SuccessWithMergeMetadata_NoDataState_IsGenuineZero verifies that
// when LogAnalyzer runs the merge internally and finds no data (money.state ==
// "no_data"), the result is attributed zero -- not an unknown-bucket signal.
func TestMap_SuccessWithMergeMetadata_NoDataState_IsGenuineZero(t *testing.T) {
	total := cost.RunTotal{
		Money:            cost.MoneyValue{State: "no_data"},
		Complete:         true,
		UnknownRunMerged: 0,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if report.Attribution != domain.AttributionAttributed {
		t.Errorf("Attribution = %q, want %q -- exitSuccess with no_data is a genuine zero-cost run", report.Attribution, domain.AttributionAttributed)
	}
	if report.TotalUSD != 0 {
		t.Errorf("TotalUSD = %v, want 0 for a no_data money state", report.TotalUSD)
	}
}

// --- Map: exitNoData is always a genuine zero (no UnknownBucketPresent check) ---

// TestMap_ExitNoData_WithoutMergeMetadata_IsAlwaysGenuineZero verifies that
// exitNoData always returns attributed zero cost. With LogAnalyzer handling
// the unknown-run merge internally, exitNoData means the tool found no data
// for this run in any source (named or unknown-run bucket).
func TestMap_ExitNoData_WithoutMergeMetadata_IsAlwaysGenuineZero(t *testing.T) {
	// No merge metadata on the MapInput -- LogAnalyzer returned exitNoData,
	// meaning it found no data even after attempting the internal merge.
	report := cost.Map(cost.MapInput{ExitCode: exitNoData})

	if report.Attribution != domain.AttributionAttributed {
		t.Errorf("Attribution = %q, want %q -- exitNoData is a genuine zero: LogAnalyzer handles the merge internally", report.Attribution, domain.AttributionAttributed)
	}
	if report.TotalUSD != 0 {
		t.Errorf("TotalUSD = %v, want 0 for a genuine no-data run", report.TotalUSD)
	}
}

// --- Provider: LogAnalyzer JSON output with merge metadata reaches CostReport ---

// TestCost_LogAnalyzerOutputWithResidual_IsReflectedInCostReport verifies the
// full provider path: when the LogAnalyzer JSON response includes
// unknown_run_residual, the provider parses it and mapSuccess carries it into
// CostReport.UnknownRunResidual.
//
// This test FAILS until I4.2 updates mapSuccess to carry the residual field.
func TestCost_LogAnalyzerOutputWithResidual_IsReflectedInCostReport(t *testing.T) {
	const wantResidual = 4

	// Construct a synthetic LogAnalyzer JSON response with unknown_run_residual.
	total := cost.RunTotal{
		SchemaVersion:      "1",
		RunID:              "run-merge-test",
		Currency:           "USD",
		Money:              cost.MoneyValue{State: "known", Amount: amount("2.50")},
		Complete:           true,
		UnknownRunMerged:   10,
		UnknownRunResidual: wantResidual,
	}
	jsonBytes, err := json.Marshal(total)
	if err != nil {
		t.Fatalf("failed to marshal fake LogAnalyzer output: %v", err)
	}

	provider := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
			return jsonBytes, exitSuccess, nil
		},
	})

	report, err := provider.Cost(context.Background(), domain.CostQuery{
		LogRoot: "/sandbox/logs",
		RunID:   "run-merge-test",
	})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}

	if report.Attribution != domain.AttributionAttributed {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionAttributed)
	}
	if report.UnknownRunResidual != wantResidual {
		t.Errorf("CostReport.UnknownRunResidual = %d, want %d -- residual from LogAnalyzer JSON must reach the cost report", report.UnknownRunResidual, wantResidual)
	}
}

// TestCost_LogAnalyzerOutputWithMergedOnly_ResidualIsZero verifies that when
// the LogAnalyzer JSON response has merged > 0 and no residual field, the
// CostReport.UnknownRunResidual is 0 (absent field decodes to zero).
func TestCost_LogAnalyzerOutputWithMergedOnly_ResidualIsZero(t *testing.T) {
	total := cost.RunTotal{
		SchemaVersion:    "1",
		RunID:            "run-clean-merge",
		Currency:         "USD",
		Money:            cost.MoneyValue{State: "known", Amount: amount("1.00")},
		Complete:         true,
		UnknownRunMerged: 6,
		// UnknownRunResidual: absent/0
	}
	jsonBytes, err := json.Marshal(total)
	if err != nil {
		t.Fatalf("failed to marshal fake LogAnalyzer output: %v", err)
	}

	provider := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
			return jsonBytes, exitSuccess, nil
		},
	})

	report, err := provider.Cost(context.Background(), domain.CostQuery{
		LogRoot: "/sandbox/logs",
		RunID:   "run-clean-merge",
	})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}

	if report.UnknownRunResidual != 0 {
		t.Errorf("CostReport.UnknownRunResidual = %d, want 0 when no residual was reported by LogAnalyzer", report.UnknownRunResidual)
	}
}
