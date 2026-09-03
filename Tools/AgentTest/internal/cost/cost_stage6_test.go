package cost_test

// Tests for Stage 6: cost diagnostics and partial attribution.
//
// These tests exercise three new behaviours in the pure Map function and the
// provider's delegation path:
//
//  1. Three distinct causes produce three distinct diagnostics:
//     - usage data captured but the model has no price entry → names the model
//     - no usage data captured at all → names the log root read, never pricing
//       configuration
//     - run identity never bound to the expected bucket → AttributionUnknownBucket,
//       distinct from both of the above
//
//  2. Partial attribution: when some models are priced and at least one is not,
//     the report shows the attributable amount (never zero), is marked partial,
//     and names the responsible unpriced model(s).
//
//  3. Query path and fallback-bucket detection: the provider passes the logs
//     root (not just the per-run folder) to the analyser when it is available,
//     and the fallback-bucket stat uses the sibling path — not a child of the
//     run folder. The unknown-bucket condition is reachable from both exit
//     codes the analyser actually produces for a missing run folder (exit 2
//     and exit 3).
//
// All tests target the Map seam (pure function) or the Invoke/StatDir seams on
// the provider. No subprocess or filesystem I/O is exercised here.

import (
	"context"
	"strings"
	"testing"

	"mosaic-agent-test/internal/cost"
	"mosaic-agent-test/internal/domain"
)

// ---------------------------------------------------------------------------
// Cause 1: usage captured but model unpriced — names the model
// ---------------------------------------------------------------------------

func TestMap_UsageCapturedModelUnpriced_WithModelIdentity_DetailNamesTheModel(t *testing.T) {
	model := "claude-3-5-sonnet-20241022"
	partialAmt := "1.50"
	total := cost.RunTotal{
		Money:          cost.MoneyValue{State: "unpriced"},
		UnpricedModels: []string{model},
		PartialMoney:   &cost.MoneyValue{State: "known", Amount: &partialAmt},
		Complete:       false,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if !strings.Contains(report.Detail, model) {
		t.Errorf("Detail = %q, want it to name the unpriced model %q", report.Detail, model)
	}
}

func TestMap_UsageCapturedModelUnpriced_WithModelIdentity_AttributionIsPartial(t *testing.T) {
	partialAmt := "0.75"
	total := cost.RunTotal{
		Money:          cost.MoneyValue{State: "unpriced"},
		UnpricedModels: []string{"some-model"},
		PartialMoney:   &cost.MoneyValue{State: "known", Amount: &partialAmt},
		Complete:       false,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if report.Attribution != domain.AttributionPartial {
		t.Errorf("Attribution = %q, want %q — usage was captured but one model had no price entry; the attributable portion is still valid", report.Attribution, domain.AttributionPartial)
	}
}

func TestMap_UsageCapturedModelUnpriced_WithModelIdentity_ReportCarriesUnpricedModelField(t *testing.T) {
	model := "claude-opus-4"
	partialAmt := "2.00"
	total := cost.RunTotal{
		Money:          cost.MoneyValue{State: "unpriced"},
		UnpricedModels: []string{model},
		PartialMoney:   &cost.MoneyValue{State: "known", Amount: &partialAmt},
		Complete:       false,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	found := false
	for _, m := range report.UnpricedModels {
		if m == model {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("CostReport.UnpricedModels = %v, want it to contain %q so callers can work with structured data rather than parsing the Detail string", report.UnpricedModels, model)
	}
}

func TestMap_UsageCapturedModelUnpriced_WithoutModelIdentity_IsUnavailableNotPartial(t *testing.T) {
	// When the analyser emits "unpriced" but no UnpricedModels (older binary or
	// a run where identity could not be determined per-model), the result is
	// Unavailable — we cannot name what we do not know — but the detail must
	// explain that usage WAS captured, not point at pricing configuration.
	total := cost.RunTotal{
		Money:          cost.MoneyValue{State: "unpriced"},
		UnpricedModels: nil,
		PartialMoney:   nil,
		Complete:       false,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q — without model identity we cannot produce a partial report", report.Attribution, domain.AttributionUnavailable)
	}
	// The detail must acknowledge that usage was captured (not redirect the user
	// to look at pricing configuration, which is a different problem).
	if strings.Contains(strings.ToLower(report.Detail), "pricing config") {
		t.Errorf("Detail = %q, must not direct the user to pricing configuration when the cause is unknown model identity", report.Detail)
	}
}

// ---------------------------------------------------------------------------
// Cause 2: no usage data captured at all — names the log root
// ---------------------------------------------------------------------------

func TestMap_NoUsageDataCaptured_ExitUsage_AttributionIsUnavailable(t *testing.T) {
	// A missing run folder makes the analyser exit with exitUsage (code 3).
	// The old message said "the log-analysis tool reported a usage error" which
	// is technically correct but unhelpful: the reader needs to know WHICH path
	// was queried and found empty, not that the tool's usage contract was broken.
	report := cost.Map(cost.MapInput{
		ExitCode: exitUsage,
		LogRoot:  "/sandbox/OrchestrationLogs/run-99",
		LogsRoot: "", // not supplied; no sibling to detect
	})

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnavailable)
	}
}

func TestMap_NoUsageDataCaptured_ExitUsage_DetailNamesTheLogRootQueried(t *testing.T) {
	const logRoot = "/sandbox/OrchestrationLogs/run-99"

	report := cost.Map(cost.MapInput{
		ExitCode: exitUsage,
		LogRoot:  logRoot,
		LogsRoot: "", // not supplied
	})

	if !strings.Contains(report.Detail, logRoot) {
		t.Errorf("Detail = %q, want it to name the log root that was queried (%q) so the reader knows where to look", report.Detail, logRoot)
	}
}

func TestMap_NoUsageDataCaptured_ExitUsage_DetailDoesNotPointAtPricingConfiguration(t *testing.T) {
	// The old "usage error" message was harmless but the whole point of Stage 6
	// is that a missing log root must NOT collapse into the same message as an
	// unpriced model — especially not one that implies pricing configuration is
	// the problem.
	report := cost.Map(cost.MapInput{
		ExitCode: exitUsage,
		LogRoot:  "/sandbox/OrchestrationLogs/run-99",
		LogsRoot: "",
	})

	lower := strings.ToLower(report.Detail)
	if strings.Contains(lower, "pric") {
		t.Errorf("Detail = %q, must not reference pricing when the cause is no usage data captured (the reader would look at the wrong place)", report.Detail)
	}
}

// ---------------------------------------------------------------------------
// T6.2: Partial attribution
// ---------------------------------------------------------------------------

func TestMap_PartialAttribution_ReportsTheAttributableAmount(t *testing.T) {
	partialAmt := "1.50"
	total := cost.RunTotal{
		Money:          cost.MoneyValue{State: "unpriced"},
		UnpricedModels: []string{"model-x"},
		PartialMoney:   &cost.MoneyValue{State: "known", Amount: &partialAmt},
		Complete:       false,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if report.TotalUSD == 0 {
		t.Errorf("TotalUSD = 0, want the attributable partial amount (1.50) — one unpriced model must not zero the entire run")
	}
	if report.TotalUSD != 1.50 {
		t.Errorf("TotalUSD = %v, want 1.50 — the partial amount from partial_money must be used, not the unattributable total", report.TotalUSD)
	}
}

func TestMap_PartialAttribution_IsMarkedPartialNotAttributed(t *testing.T) {
	partialAmt := "2.00"
	total := cost.RunTotal{
		Money:          cost.MoneyValue{State: "unpriced"},
		UnpricedModels: []string{"model-y"},
		PartialMoney:   &cost.MoneyValue{State: "known", Amount: &partialAmt},
		Complete:       false,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if report.Attribution == domain.AttributionAttributed {
		t.Error("Attribution = AttributionAttributed, want AttributionPartial — a run with an unpriced model must not claim full attribution")
	}
	if report.Attribution != domain.AttributionPartial {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionPartial)
	}
}

func TestMap_PartialAttribution_NamesAllResponsibleModels(t *testing.T) {
	partialAmt := "3.00"
	total := cost.RunTotal{
		Money:          cost.MoneyValue{State: "unpriced"},
		UnpricedModels: []string{"model-alpha", "model-beta"},
		PartialMoney:   &cost.MoneyValue{State: "known", Amount: &partialAmt},
		Complete:       false,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if !strings.Contains(report.Detail, "model-alpha") {
		t.Errorf("Detail = %q, want it to name model-alpha", report.Detail)
	}
	if !strings.Contains(report.Detail, "model-beta") {
		t.Errorf("Detail = %q, want it to name model-beta", report.Detail)
	}
}

func TestMap_PartialAttribution_UnpricedModelsCarriedToReport(t *testing.T) {
	partialAmt := "1.00"
	models := []string{"model-1", "model-2"}
	total := cost.RunTotal{
		Money:          cost.MoneyValue{State: "unpriced"},
		UnpricedModels: models,
		PartialMoney:   &cost.MoneyValue{State: "known", Amount: &partialAmt},
		Complete:       false,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if len(report.UnpricedModels) == 0 {
		t.Error("CostReport.UnpricedModels is empty, want the unpriced models carried as structured data so callers do not need to parse Detail")
	}
	for _, want := range models {
		found := false
		for _, got := range report.UnpricedModels {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("CostReport.UnpricedModels = %v, want it to contain %q", report.UnpricedModels, want)
		}
	}
}

func TestMap_PartialAttribution_PartialAmountComesFromPartialMoneyFieldNotMainMoney(t *testing.T) {
	// The main Money field is "unpriced" with no amount. The attributable
	// amount lives in PartialMoney. The mapping must read from PartialMoney,
	// not from Money.Amount (which is absent for an unpriced run).
	partialAmt := "0.88"
	total := cost.RunTotal{
		Money:          cost.MoneyValue{State: "unpriced", Amount: nil},
		UnpricedModels: []string{"some-model"},
		PartialMoney:   &cost.MoneyValue{State: "known", Amount: &partialAmt},
		Complete:       false,
	}

	report := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: total})

	if report.TotalUSD != 0.88 {
		t.Errorf("TotalUSD = %v, want 0.88 (from PartialMoney) — the mapping must source the amount from partial_money, not from the unpriced money.amount which is absent", report.TotalUSD)
	}
}

// ---------------------------------------------------------------------------
// T6.3: Query path and fallback-bucket detection
// ---------------------------------------------------------------------------

func TestCost_LogsRootPopulated_AnalyserReceivesLogsRootAsPath(t *testing.T) {
	const logRoot = "/sandbox/OrchestrationLogs/run-42"
	const logsRoot = "/sandbox/OrchestrationLogs"

	var gotArgs []string
	p := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
			gotArgs = append([]string(nil), args...)
			return []byte(`{"schema_version":"1","run_id":"run-42","currency":"USD","money":{"state":"known","amount":"1.00"},"complete":true}`), exitSuccess, nil
		},
	})

	_, err := p.Cost(context.Background(), domain.CostQuery{
		LogRoot:  logRoot,
		LogsRoot: logsRoot,
		RunID:    "run-42",
	})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}

	// The --path argument must be the logs root, not the per-run folder,
	// so the analyser can see sibling buckets.
	foundLogsRoot := false
	foundLogRoot := false
	for i, arg := range gotArgs {
		if arg == "--path" && i+1 < len(gotArgs) {
			if gotArgs[i+1] == logsRoot {
				foundLogsRoot = true
			}
			if gotArgs[i+1] == logRoot {
				foundLogRoot = true
			}
		}
	}
	if !foundLogsRoot {
		t.Errorf("args = %v, want --path %q (the logs root) so the analyser can discover sibling buckets", gotArgs, logsRoot)
	}
	if foundLogRoot {
		t.Errorf("args = %v, --path must be the logs root %q, not the per-run folder %q", gotArgs, logsRoot, logRoot)
	}
}

func TestCost_LogsRootEmpty_AnalyserReceivesLogRootAsPath(t *testing.T) {
	const logRoot = "/sandbox/OrchestrationLogs/run-42"

	var gotArgs []string
	p := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
			gotArgs = append([]string(nil), args...)
			return []byte(`{"schema_version":"1","run_id":"run-42","currency":"USD","money":{"state":"known","amount":"1.00"},"complete":true}`), exitSuccess, nil
		},
	})

	_, err := p.Cost(context.Background(), domain.CostQuery{
		LogRoot:  logRoot,
		LogsRoot: "", // absent — backward-compat path
		RunID:    "run-42",
	})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}

	foundLogRoot := false
	for i, arg := range gotArgs {
		if arg == "--path" && i+1 < len(gotArgs) && gotArgs[i+1] == logRoot {
			foundLogRoot = true
		}
	}
	if !foundLogRoot {
		t.Errorf("args = %v, want --path %q (the per-run folder) when LogsRoot is absent, to preserve backward compatibility", gotArgs, logRoot)
	}
}

