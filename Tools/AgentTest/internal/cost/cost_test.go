package cost_test

// Tests for the delegating cost provider: the pure outcome mapping (Map) and
// the provider's delegation to the log-analysis tool through the Invoke and
// StatDir seams. No log parsing is exercised here — only the mapping from a
// delegated outcome to a domain.CostReport.

import (
	"context"
	"errors"
	"testing"

	"mosaic-agent-test/internal/cost"
	"mosaic-agent-test/internal/domain"
)

// The log-analysis tool's exit codes, mirrored here as plain ints because
// this package must not import mosaic-log-analyzer to learn its own outcome
// contract — only the stable wire shape and exit-code meanings are shared,
// per ContractsDesign.md's reference to Tools/LogAnalyzer/internal/cli/exitcodes.go.
const (
	exitSuccess  = 0
	exitFailure  = 1
	exitNoData   = 2
	exitUsage    = 3
	exitUnusable = 4
)

func amount(s string) *string { return &s }

func TestMap_SuccessWithKnownMoney_IsAttributedAtTheReportedAmount(t *testing.T) {
	total := cost.RunTotal{
		Money:    cost.MoneyValue{State: "known", Amount: amount("1.23")},
		Complete: true,
	}

	report := cost.Map(exitSuccess, total, false, nil)

	if report.Attribution != domain.AttributionAttributed {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionAttributed)
	}
	if report.TotalUSD != 1.23 {
		t.Errorf("TotalUSD = %v, want 1.23", report.TotalUSD)
	}
}

func TestMap_SuccessWithUnpricedMoney_IsUnavailableNotZeroCost(t *testing.T) {
	total := cost.RunTotal{
		Money:    cost.MoneyValue{State: "unpriced"},
		Complete: true,
	}

	report := cost.Map(exitSuccess, total, false, nil)

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
	if report.Detail == "" {
		t.Error("Detail is empty, want it to name the unpriced condition")
	}
}

func TestMap_ProvisionalTotal_IsStillAttributedButFlaggedPartial(t *testing.T) {
	total := cost.RunTotal{
		Provisional: true,
		Money:       cost.MoneyValue{State: "known", Amount: amount("0.50")},
		Complete:    false,
	}

	report := cost.Map(exitSuccess, total, false, nil)

	if report.Attribution != domain.AttributionAttributed {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionAttributed)
	}
	if report.TotalUSD != 0.50 {
		t.Errorf("TotalUSD = %v, want 0.50", report.TotalUSD)
	}
	if report.Detail == "" {
		t.Error("Detail is empty, want it to record that the total is partial")
	}
}

func TestMap_NoDataForRun_FallbackBucketAbsent_IsAGenuineZero(t *testing.T) {
	report := cost.Map(exitNoData, cost.RunTotal{}, false, nil)

	if report.Attribution != domain.AttributionAttributed {
		t.Errorf("Attribution = %q, want %q (a genuine zero-cost run)", report.Attribution, domain.AttributionAttributed)
	}
	if report.TotalUSD != 0 {
		t.Errorf("TotalUSD = %v, want 0", report.TotalUSD)
	}
}

func TestMap_NoDataForRun_FallbackBucketPresent_IsSurfacedNeverAsZero(t *testing.T) {
	report := cost.Map(exitNoData, cost.RunTotal{}, true, nil)

	if report.Attribution != domain.AttributionUnknownBucket {
		t.Errorf("Attribution = %q, want %q — a cost exists but could not be tied to this run", report.Attribution, domain.AttributionUnknownBucket)
	}
}

func TestMap_UsageError_IsUnavailableWithCauseNamed(t *testing.T) {
	report := cost.Map(exitUsage, cost.RunTotal{}, false, nil)

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
	if report.Detail == "" {
		t.Error("Detail is empty, want it to name the cause")
	}
}

func TestMap_UnusableSource_IsUnavailable(t *testing.T) {
	report := cost.Map(exitUnusable, cost.RunTotal{}, false, nil)

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
}

func TestMap_InfrastructureFailureExitCode_IsUnavailable(t *testing.T) {
	report := cost.Map(exitFailure, cost.RunTotal{}, false, nil)

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
}

func TestMap_UnrecognisedExitCode_IsUnavailableNeverAttributed(t *testing.T) {
	report := cost.Map(99, cost.RunTotal{}, false, nil)

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q — an unrecognised exit code must never read as a priced run", report.Attribution, domain.AttributionUnavailable)
	}
}

func TestMap_InvokeError_ToolAbsentOrFailedToStart_IsUnavailable(t *testing.T) {
	report := cost.Map(0, cost.RunTotal{}, false, errors.New("executable not found"))

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
	if report.TotalUSD != 0 {
		t.Errorf("TotalUSD = %v, want 0", report.TotalUSD)
	}
}

// --- Provider delegation --------------------------------------------------

func TestCost_DelegatesToInvokeWithTheQueriedRunAndLogRoot(t *testing.T) {
	var gotPath string
	var gotArgs []string

	provider := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, int, error) {
			gotPath = path
			gotArgs = append([]string(nil), args...)
			return []byte(`{"schema_version":"1","run_id":"run-1","currency":"USD","money":{"state":"known","amount":"2.00"},"complete":true}`), 0, nil
		},
	})

	report, err := provider.Cost(context.Background(), domain.CostQuery{LogRoot: "/sandbox/logs", RunID: "run-1"})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}
	if gotPath != "mosaic-log-analyzer" {
		t.Errorf("Invoke path = %q, want the configured executable", gotPath)
	}
	if len(gotArgs) == 0 {
		t.Error("Invoke received no arguments, want the run id and log root to be passed through")
	}
	if report.Attribution != domain.AttributionAttributed {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionAttributed)
	}
	if report.TotalUSD != 2.00 {
		t.Errorf("TotalUSD = %v, want 2.00", report.TotalUSD)
	}
}

func TestCost_FallbackBucketDetectedViaStatDir_SurfacesAsUnknownBucket(t *testing.T) {
	statDirCalls := 0

	provider := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, int, error) {
			// No data for the queried run.
			return []byte(`{"schema_version":"1","run_id":"run-1","currency":"USD","money":{"state":"no_data"},"complete":true}`), exitNoData, nil
		},
		StatDir: func(path string) bool {
			statDirCalls++
			return true // the unknown-run bucket is present
		},
	})

	report, err := provider.Cost(context.Background(), domain.CostQuery{LogRoot: "/sandbox/logs", RunID: "run-1"})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}
	if statDirCalls == 0 {
		t.Error("StatDir was never called, want the provider to check for the fallback bucket")
	}
	if report.Attribution != domain.AttributionUnknownBucket {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnknownBucket)
	}
}

func TestCost_ToolAbsent_ReportsUnavailableRatherThanAnError(t *testing.T) {
	provider := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, int, error) {
			return nil, 0, errors.New("executable not found")
		},
	})

	report, err := provider.Cost(context.Background(), domain.CostQuery{LogRoot: "/sandbox/logs", RunID: "run-1"})
	if err != nil {
		t.Fatalf("Cost returned an error rather than an unavailable report: %v", err)
	}
	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
}

func TestCost_MalformedOutput_ReportsUnavailableRatherThanAnError(t *testing.T) {
	provider := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string) ([]byte, int, error) {
			return []byte("not json"), 0, nil
		},
	})

	report, err := provider.Cost(context.Background(), domain.CostQuery{LogRoot: "/sandbox/logs", RunID: "run-1"})
	if err != nil {
		t.Fatalf("Cost returned an error rather than an unavailable report: %v", err)
	}
	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
}
