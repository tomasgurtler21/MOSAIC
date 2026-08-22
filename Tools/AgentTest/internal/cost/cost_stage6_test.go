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
	"path/filepath"
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
// Cause 3: identity never bound to the expected bucket — distinct attribution
// ---------------------------------------------------------------------------

func TestMap_IdentityNeverBound_ExitUsage_WithLogsRootAndBucketPresent_IsUnknownBucket(t *testing.T) {
	// When the analyser exits with exitUsage AND LogsRoot is supplied AND the
	// unknown-run sibling bucket exists, the run is AttributionUnknownBucket —
	// not Unavailable. This is a different defect: the run happened and its
	// events landed in the fallback bucket, but were never tied to the run ID.
	report := cost.Map(cost.MapInput{
		ExitCode:             exitUsage,
		LogRoot:              "/sandbox/OrchestrationLogs/run-99",
		LogsRoot:             "/sandbox/OrchestrationLogs",
		UnknownBucketPresent: true,
	})

	if report.Attribution != domain.AttributionUnknownBucket {
		t.Errorf("Attribution = %q, want %q — a run whose events landed in the fallback bucket must be distinguished from a run with no events at all", report.Attribution, domain.AttributionUnknownBucket)
	}
}

func TestMap_IdentityNeverBound_ExitUsage_WithLogsRootButBucketAbsent_IsUnavailableNotUnknownBucket(t *testing.T) {
	report := cost.Map(cost.MapInput{
		ExitCode:             exitUsage,
		LogRoot:              "/sandbox/OrchestrationLogs/run-99",
		LogsRoot:             "/sandbox/OrchestrationLogs",
		UnknownBucketPresent: false,
	})

	if report.Attribution != domain.AttributionUnavailable {
		t.Errorf("Attribution = %q, want %q — no fallback bucket means genuinely no data", report.Attribution, domain.AttributionUnavailable)
	}
}

// ---------------------------------------------------------------------------
// Three causes are mutually distinct
// ---------------------------------------------------------------------------

func TestMap_ThreeCauses_ProduceDistinctAttributions(t *testing.T) {
	partialAmt := "1.00"
	unpricedModel := cost.RunTotal{
		Money:          cost.MoneyValue{State: "unpriced"},
		UnpricedModels: []string{"model-a"},
		PartialMoney:   &cost.MoneyValue{State: "known", Amount: &partialAmt},
	}
	cause1 := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: unpricedModel})   // usage captured, model unpriced
	cause2 := cost.Map(cost.MapInput{ExitCode: exitUsage, LogRoot: "/logs/run-1"})    // no usage data at all
	cause3 := cost.Map(cost.MapInput{ExitCode: exitUsage, LogsRoot: "/logs", UnknownBucketPresent: true}) // identity mismatch

	if cause1.Attribution == cause2.Attribution {
		t.Errorf("cause1 (unpriced model) and cause2 (no usage data) share attribution %q — they must be distinct", cause1.Attribution)
	}
	if cause2.Attribution == cause3.Attribution {
		t.Errorf("cause2 (no usage data) and cause3 (unknown bucket) share attribution %q — they must be distinct", cause2.Attribution)
	}
	if cause1.Attribution == cause3.Attribution {
		t.Errorf("cause1 (unpriced model) and cause3 (unknown bucket) share attribution %q — they must be distinct", cause1.Attribution)
	}
}

func TestMap_ThreeCauses_ProduceDistinctDetails(t *testing.T) {
	partialAmt := "1.00"
	unpricedModel := cost.RunTotal{
		Money:          cost.MoneyValue{State: "unpriced"},
		UnpricedModels: []string{"model-a"},
		PartialMoney:   &cost.MoneyValue{State: "known", Amount: &partialAmt},
	}
	cause1 := cost.Map(cost.MapInput{ExitCode: exitSuccess, Total: unpricedModel})
	cause2 := cost.Map(cost.MapInput{ExitCode: exitUsage, LogRoot: "/logs/run-1", LogsRoot: ""})
	cause3 := cost.Map(cost.MapInput{ExitCode: exitUsage, LogsRoot: "/logs", UnknownBucketPresent: true})

	if cause1.Detail == cause2.Detail {
		t.Errorf("cause1 (unpriced model) and cause2 (no usage data) have the same detail %q", cause1.Detail)
	}
	if cause2.Detail == cause3.Detail {
		t.Errorf("cause2 (no usage data) and cause3 (unknown bucket) have the same detail %q — an unknown-bucket run must name the bucket, not look like a missing-log-root", cause2.Detail)
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

func TestCost_LogsRootPopulated_FallbackBucketStatUsesLogsRootSiblingNotRunFolderChild(t *testing.T) {
	const logRoot = "/sandbox/OrchestrationLogs/run-42"
	const logsRoot = "/sandbox/OrchestrationLogs"

	var statPaths []string
	p := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
			// Simulate "no data for this run" with exitNoData so the provider
			// performs the fallback-bucket stat.
			return []byte(`{"schema_version":"1","run_id":"run-42","currency":"USD","money":{"state":"no_data"},"complete":true}`), exitNoData, nil
		},
		StatDir: func(path string) bool {
			statPaths = append(statPaths, path)
			return false
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

	wantSiblingPath := filepath.Join(logsRoot, "unknown-run")
	wrongChildPath := filepath.Join(logRoot, "unknown-run")

	foundSibling := false
	foundChild := false
	for _, p := range statPaths {
		if p == wantSiblingPath {
			foundSibling = true
		}
		if p == wrongChildPath {
			foundChild = true
		}
	}
	if !foundSibling {
		t.Errorf("StatDir was not called with %q (sibling of run folder) — the fallback bucket must be looked for alongside the run folder, not inside it; called with: %v", wantSiblingPath, statPaths)
	}
	if foundChild {
		t.Errorf("StatDir was called with %q (child of run folder) — this is the pre-Stage-6 bug; the fallback bucket is a sibling of the run folder, not a child of it", wrongChildPath)
	}
}

func TestCost_LogsRootEmpty_FallbackBucketStatFallsBackToLegacyPath(t *testing.T) {
	// When LogsRoot is not supplied, the provider preserves the pre-Stage-6
	// behaviour (stat child of run folder) so callers that have not been
	// updated yet continue to work.
	const logRoot = "/sandbox/OrchestrationLogs/run-42"

	var statPaths []string
	p := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
			return []byte(`{"schema_version":"1","run_id":"run-42","currency":"USD","money":{"state":"no_data"},"complete":true}`), exitNoData, nil
		},
		StatDir: func(path string) bool {
			statPaths = append(statPaths, path)
			return false
		},
	})

	_, err := p.Cost(context.Background(), domain.CostQuery{
		LogRoot:  logRoot,
		LogsRoot: "",
		RunID:    "run-42",
	})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}

	legacyPath := filepath.Join(logRoot, "unknown-run")
	found := false
	for _, p := range statPaths {
		if p == legacyPath {
			found = true
		}
	}
	if !found {
		t.Errorf("StatDir was not called with legacy path %q when LogsRoot is absent — backward compatibility must be preserved; called with: %v", legacyPath, statPaths)
	}
}

func TestCost_ExitUsageWithLogsRootAndBucketPresent_IsAttributionUnknownBucket(t *testing.T) {
	// This is the key reachability test: a misbound run hits exitUsage (the
	// analyser cannot find the run folder under the logs root), and with
	// LogsRoot present and the sibling bucket detected, the provider must
	// surface AttributionUnknownBucket. Today the exitUsage branch is
	// unreachable for this detection path.
	const logsRoot = "/sandbox/OrchestrationLogs"

	p := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
			return []byte(""), exitUsage, nil // run folder not found
		},
		StatDir: func(path string) bool {
			return true // unknown-run bucket exists
		},
	})

	report, err := p.Cost(context.Background(), domain.CostQuery{
		LogRoot:  logsRoot + "/run-99",
		LogsRoot: logsRoot,
		RunID:    "run-99",
	})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}
	if report.Attribution != domain.AttributionUnknownBucket {
		t.Errorf("Attribution = %q, want %q — a misbound run hitting exitUsage with a present fallback bucket must be surfaced as unknown-bucket, not unavailable", report.Attribution, domain.AttributionUnknownBucket)
	}
}

func TestCost_ExitNoDataWithLogsRootAndBucketPresentAtSiblingPath_IsAttributionUnknownBucket(t *testing.T) {
	// Confirms the exitNoData path also works with the corrected sibling stat.
	const logRoot = "/sandbox/OrchestrationLogs/run-1"
	const logsRoot = "/sandbox/OrchestrationLogs"

	wantSiblingBucket := filepath.Join(logsRoot, "unknown-run")
	var statReceivedLogsRoot bool
	p := cost.New(cost.Options{
		ExecutablePath: "mosaic-log-analyzer",
		Invoke: func(ctx context.Context, path string, args []string, workingDir string) ([]byte, int, error) {
			return []byte(`{"schema_version":"1","run_id":"run-1","currency":"USD","money":{"state":"no_data"},"complete":true}`), exitNoData, nil
		},
		StatDir: func(path string) bool {
			if path == wantSiblingBucket {
				statReceivedLogsRoot = true
			}
			return true
		},
	})

	report, err := p.Cost(context.Background(), domain.CostQuery{
		LogRoot:  logRoot,
		LogsRoot: logsRoot,
		RunID:    "run-1",
	})
	if err != nil {
		t.Fatalf("Cost returned unexpected error: %v", err)
	}
	if report.Attribution != domain.AttributionUnknownBucket {
		t.Errorf("Attribution = %q, want %q", report.Attribution, domain.AttributionUnknownBucket)
	}
	if !statReceivedLogsRoot {
		t.Error("StatDir was not called with a path under the logs root — want it to stat the sibling bucket, not a child of the run folder")
	}
}
