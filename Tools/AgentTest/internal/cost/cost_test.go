package cost_test

// Tests for the delegating cost provider: the pure outcome mapping (Map) and
// the provider's delegation to the log-analysis tool through the Invoke and
// StatDir seams. No log parsing is exercised here — only the mapping from a
// delegated outcome to a domain.CostReport.

import (
	"context"
	"errors"
	"strconv"
	"strings"
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

// --- Existing exit-code mappings, pinned unchanged (AC9.4) -----------------
//
// These exercise cost.Map through cost.MapInput rather than positional
// arguments — the call shape changed per ContractsDesign.md's "cost mapping
// surface" — but every asserted outcome is identical to what Map produced
// before this stage.

func TestMap_SuccessWithKnownMoney_IsAttributedAtTheReportedAmount(t *testing.T) {
	total := cost.RunTotal{
		Money:    cost.MoneyValue{State: "known", Amount: amount("1.23")},
		Complete: true,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

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

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

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

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

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
	report := cost.Map(cost.MapInput{ExitCode: exitNoData})

	if report.Attribution != domain.AttributionAttributed {
		t.Errorf("Attribution = %q, want %q (a genuine zero-cost run)", report.Attribution, domain.AttributionAttributed)
	}
	if report.TotalUSD != 0 {
		t.Errorf("TotalUSD = %v, want 0", report.TotalUSD)
	}
}

func TestMap_NoDataForRun_FallbackBucketPresent_IsSurfacedNeverAsZero(t *testing.T) {
	report := cost.Map(cost.MapInput{ExitCode: exitNoData, UnknownBucketPresent: true})

	if report.Attribution != domain.AttributionUnknownBucket {
		t.Errorf("Attribution = %q, want %q — a cost exists but could not be tied to this run", report.Attribution, domain.AttributionUnknownBucket)
	}
}

func TestMap_UsageError_IsUnavailableWithCauseNamed(t *testing.T) {
	report := cost.Map(cost.MapInput{ExitCode: exitUsage})

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
	if report.Detail == "" {
		t.Error("Detail is empty, want it to name the cause")
	}
}

func TestMap_UnusableSource_IsUnavailable(t *testing.T) {
	report := cost.Map(cost.MapInput{ExitCode: exitUnusable})

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
}

func TestMap_InfrastructureFailureExitCode_IsUnavailable(t *testing.T) {
	report := cost.Map(cost.MapInput{ExitCode: exitFailure})

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
}

func TestMap_UnrecognisedExitCode_IsUnavailableNeverAttributed(t *testing.T) {
	report := cost.Map(cost.MapInput{ExitCode: 99})

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q — an unrecognised exit code must never read as a priced run", report.Attribution, domain.AttributionUnavailable)
	}
}

func TestMap_InvokeError_ToolAbsentOrFailedToStart_IsUnavailable(t *testing.T) {
	report := cost.Map(cost.MapInput{InvokeErr: errors.New("executable not found")})

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
	if report.TotalUSD != 0 {
		t.Errorf("TotalUSD = %v, want 0", report.TotalUSD)
	}
}

// --- Delegate failure-mode diagnostics (T9.1, T9.2, AC9.1-AC9.3) -----------
//
// The delegate can fail in ways the current mapping conflates. Each of the
// following must produce a distinct, diagnostic report — never the generic
// "delegating to the log-analysis tool failed" message a JSON-unmarshal
// error is mistaken for today.

func TestMap_NonZeroExitEmptyStdout_IsReportedAsUnparseableOutputNotAnInvocationFailure(t *testing.T) {
	report := cost.Map(cost.MapInput{
		ExecutablePath: "mosaic-log-analyzer",
		RunID:          "run-1",
		LogRoot:        "/sandbox/logs",
		ExitCode:       exitUsage,
		StdoutLen:      0,
		ParseErr:       errors.New("unexpected end of JSON input"),
	})

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
	if strings.Contains(report.Detail, "delegating to the log-analysis tool failed") {
		t.Errorf("Detail = %q, a non-zero exit with unparseable stdout must not be reported as an invocation failure", report.Detail)
	}
	if strings.Contains(report.Detail, "JSON") {
		t.Errorf("Detail = %q, a non-zero exit's empty/unparseable stdout must not be reported as a JSON parse error (AC9.1)", report.Detail)
	}
}

func TestMap_NonZeroExitUnparseableStdout_IsReportedAsUnparseableOutputNotAnInvocationFailure(t *testing.T) {
	report := cost.Map(cost.MapInput{
		ExecutablePath: "mosaic-log-analyzer",
		RunID:          "run-1",
		LogRoot:        "/sandbox/logs",
		ExitCode:       exitFailure,
		StdoutLen:      11,
		ParseErr:       errors.New("invalid character 'n' looking for beginning of value"),
	})

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
	if strings.Contains(report.Detail, "delegating to the log-analysis tool failed") {
		t.Errorf("Detail = %q, a non-zero exit with unparseable stdout must not be reported as an invocation failure", report.Detail)
	}
	if strings.Contains(report.Detail, "JSON") {
		t.Errorf("Detail = %q, a non-zero exit's unparseable stdout must not be reported as a JSON parse error (AC9.1)", report.Detail)
	}
	if !strings.Contains(report.Detail, "mosaic-log-analyzer") {
		t.Errorf("Detail = %q, want it to name the executable (AC9.2)", report.Detail)
	}
	if !strings.Contains(report.Detail, strconv.Itoa(exitFailure)) {
		t.Errorf("Detail = %q, want it to name the exit code %d (AC9.2)", report.Detail, exitFailure)
	}
	if !strings.Contains(report.Detail, "/sandbox/logs") {
		t.Errorf("Detail = %q, want it to name the queried log root (AC9.2)", report.Detail)
	}
}

func TestMap_ZeroExitUnparseableStdout_IsDistinguishedFromANonZeroExitFailure(t *testing.T) {
	zeroExit := cost.Map(cost.MapInput{
		ExecutablePath: "mosaic-log-analyzer",
		RunID:          "run-1",
		LogRoot:        "/sandbox/logs",
		ExitCode:       exitSuccess,
		StdoutLen:      8,
		ParseErr:       errors.New("invalid character 'n' looking for beginning of value"),
	})
	nonZeroExit := cost.Map(cost.MapInput{
		ExecutablePath: "mosaic-log-analyzer",
		RunID:          "run-1",
		LogRoot:        "/sandbox/logs",
		ExitCode:       exitFailure,
		StdoutLen:      8,
		ParseErr:       errors.New("invalid character 'n' looking for beginning of value"),
	})

	if zeroExit.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", zeroExit.Attribution, domain.AttributionUnavailable)
	}
	if zeroExit.Detail == nonZeroExit.Detail {
		t.Errorf("a zero exit that produced unparseable output and a non-zero exit that produced unparseable output must not read alike: both = %q", zeroExit.Detail)
	}
	if !strings.Contains(zeroExit.Detail, "mosaic-log-analyzer") {
		t.Errorf("Detail = %q, want it to name the executable (AC9.2)", zeroExit.Detail)
	}
	if !strings.Contains(zeroExit.Detail, strconv.Itoa(8)) {
		t.Errorf("Detail = %q, want it to name StdoutLen (%d bytes) for a zero-exit run whose output could not be decoded", zeroExit.Detail, 8)
	}
}

func TestMap_InvocationFailureVsParseFailure_ProduceDistinguishableReports(t *testing.T) {
	invocationFailure := cost.Map(cost.MapInput{
		ExecutablePath: "mosaic-log-analyzer",
		RunID:          "run-1",
		LogRoot:        "/sandbox/logs",
		InvokeErr:      errors.New("executable not found"),
	})
	parseFailure := cost.Map(cost.MapInput{
		ExecutablePath: "mosaic-log-analyzer",
		RunID:          "run-1",
		LogRoot:        "/sandbox/logs",
		ExitCode:       exitUsage,
		ParseErr:       errors.New("unexpected end of JSON input"),
	})

	if invocationFailure.Attribution != domain.AttributionUnavailable {
		t.Errorf("invocationFailure.Attribution = %q, want %q", invocationFailure.Attribution, domain.AttributionUnavailable)
	}
	if parseFailure.Attribution != domain.AttributionUnavailable {
		t.Errorf("parseFailure.Attribution = %q, want %q", parseFailure.Attribution, domain.AttributionUnavailable)
	}
	if invocationFailure.Detail == parseFailure.Detail {
		t.Errorf("\"could not be invoked\" and \"ran and produced unparseable output\" must not read alike (AC9.3): both = %q", invocationFailure.Detail)
	}
}

func TestMap_ParseFailureDetail_NamesExecutableExitCodeAndLogRoot(t *testing.T) {
	report := cost.Map(cost.MapInput{
		ExecutablePath: "mosaic-log-analyzer",
		RunID:          "run-42",
		LogRoot:        "/sandbox/run-42/logs",
		ExitCode:       exitUsage,
		StdoutLen:      0,
		ParseErr:       errors.New("unexpected end of JSON input"),
	})

	if !strings.Contains(report.Detail, "mosaic-log-analyzer") {
		t.Errorf("Detail = %q, want it to name the executable (AC9.2)", report.Detail)
	}
	if !strings.Contains(report.Detail, strconv.Itoa(exitUsage)) {
		t.Errorf("Detail = %q, want it to name the exit code %d (AC9.2)", report.Detail, exitUsage)
	}
	if !strings.Contains(report.Detail, "/sandbox/run-42/logs") {
		t.Errorf("Detail = %q, want it to name the queried log root (AC9.2)", report.Detail)
	}
}

func TestMap_InvocationFailureDetail_NamesExecutable(t *testing.T) {
	report := cost.Map(cost.MapInput{
		ExecutablePath: "mosaic-log-analyzer",
		RunID:          "run-1",
		LogRoot:        "/sandbox/logs",
		InvokeErr:      errors.New("executable not found"),
	})

	if !strings.Contains(report.Detail, "mosaic-log-analyzer") {
		t.Errorf("Detail = %q, want it to name the executable that could not be invoked (AC9.2)", report.Detail)
	}
}

// --- Provider delegation -----------------------------------------------

func TestCost_DelegatesToInvokeWithTheQueriedRunAndLogRoot(t *testing.T) {
	var gotPath string
	var gotArgs []string

	provider := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
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
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
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
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
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
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
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

// TestCost_NonZeroExit_MalformedOutput_ReportsUnparseableOutputNotAnInvocationFailure
// pins the defect this stage fixes at the provider level: the log root does
// not exist, the log-analysis tool exits non-zero with empty stdout, and
// today's Cost() reports a JSON-unmarshal error instead of naming the actual
// cause. See Stage-9/Plan.md.
func TestCost_NonZeroExit_MalformedOutput_ReportsUnparseableOutputNotAnInvocationFailure(t *testing.T) {
	provider := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
			return []byte(""), exitUsage, nil // log root does not exist: empty stdout, non-zero exit
		},
	})

	report, err := provider.Cost(context.Background(), domain.CostQuery{LogRoot: "/sandbox/missing-logs", RunID: "run-1"})
	if err != nil {
		t.Fatalf("Cost returned an error rather than an unavailable report: %v", err)
	}
	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
	if strings.Contains(report.Detail, "JSON") {
		t.Errorf("Detail = %q, a non-zero exit with empty stdout must not be reported as a JSON parse error (AC9.1)", report.Detail)
	}
	if !strings.Contains(report.Detail, "/sandbox/missing-logs") {
		t.Errorf("Detail = %q, want it to name the queried log root (AC9.2)", report.Detail)
	}
	if !strings.Contains(report.Detail, strconv.Itoa(exitUsage)) {
		t.Errorf("Detail = %q, want it to name the exit code %d (AC9.2)", report.Detail, exitUsage)
	}
}
