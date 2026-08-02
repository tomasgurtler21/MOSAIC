package app_test

// Tests for the basic total-cost-per-run query path.
//
// These tests verify that:
//   - RunTotalFor correctly extracts the total for a named run from a report.
//   - RunTotalFor returns (zero, false) for a run ID not in the report.
//   - RunTotalFor sets Complete=false when any model in the run was unpriced.
//   - TotalForRun runs the full analysis flow and returns the named run's total.
//   - TotalForRun returns the OutcomeSourceNotFound outcome when no source is found.

import (
	"context"
	"testing"

	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// RunTotalFor: extraction from an in-memory report
// ---------------------------------------------------------------------------

func TestRunTotalFor_ExistingRun_ReturnsTotal(t *testing.T) {
	// RunTotalFor must extract the total for the named run when it exists.
	const wantInput = int64(4_000_000)
	const wantOutput = int64(1_500_000)

	report := domain.Report{
		Runs: []domain.RunReport{
			{
				Run: appTestRunRef,
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{
						Input:  domain.Tokens(wantInput),
						Output: domain.Tokens(wantOutput),
					},
					Money: domain.CategoryMoney{
						Total:    domain.KnownMoney(domain.Money(34_500_000_000)),
						Complete: true,
					},
				},
				Orchestrator: domain.ActorReport{Actor: domain.Orchestrator()},
			},
		},
		Currency: domain.Currency,
	}

	total, ok := app.RunTotalFor(report, appTestRunID)

	if !ok {
		t.Fatalf("RunTotalFor returned (_, false) for run %q that exists in the report", appTestRunID)
	}
	if total.RunID != appTestRunID {
		t.Errorf("total.RunID = %q, want %q", total.RunID, appTestRunID)
	}
	if v, present := total.Tokens.Input.Value(); !present || v != wantInput {
		t.Errorf("total.Tokens.Input = (%d, %v), want (%d, true)", v, present, wantInput)
	}
	if v, present := total.Tokens.Output.Value(); !present || v != wantOutput {
		t.Errorf("total.Tokens.Output = (%d, %v), want (%d, true)", v, present, wantOutput)
	}
}

func TestRunTotalFor_ExistingRun_MoneyState(t *testing.T) {
	// RunTotalFor must carry the money state from the run report's total.
	report := domain.Report{
		Runs: []domain.RunReport{
			{
				Run: appTestRunRef,
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{Input: domain.Tokens(1_000_000)},
					Money: domain.CategoryMoney{
						Total:    domain.KnownMoney(domain.Money(3_000_000_000)),
						Complete: true,
					},
				},
				Orchestrator: domain.ActorReport{Actor: domain.Orchestrator()},
			},
		},
		Currency: domain.Currency,
	}

	total, ok := app.RunTotalFor(report, appTestRunID)

	if !ok {
		t.Fatalf("RunTotalFor returned (_, false) for an existing run")
	}
	if total.Money.State != domain.MoneyKnown {
		t.Errorf("total.Money.State = %v, want MoneyKnown", total.Money.State)
	}
}

func TestRunTotalFor_NonExistentRunID_ReturnsFalse(t *testing.T) {
	// RunTotalFor must return (zero, false) when the run ID is not in the report.
	report := domain.Report{
		Runs: []domain.RunReport{
			{
				Run:          appTestRunRef,
				Orchestrator: domain.ActorReport{Actor: domain.Orchestrator()},
			},
		},
	}

	_, ok := app.RunTotalFor(report, "20260101T999999Z-zzzz")

	if ok {
		t.Error("RunTotalFor must return false for a run ID not present in the report")
	}
}

func TestRunTotalFor_EmptyReport_ReturnsFalse(t *testing.T) {
	// RunTotalFor must return (zero, false) for an empty report.
	_, ok := app.RunTotalFor(domain.Report{}, appTestRunID)

	if ok {
		t.Error("RunTotalFor must return false when the report has no runs")
	}
}

func TestRunTotalFor_UnpricedModel_CompleteFalse(t *testing.T) {
	// When the run has at least one unpriced model, RunTotalFor must set
	// Complete=false to signal that the money total is partial.
	report := domain.Report{
		Runs: []domain.RunReport{
			{
				Run:            appTestRunRef,
				UnpricedModels: []domain.ModelID{"unknown-model"},
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{Input: domain.Tokens(1_000_000)},
					Money: domain.CategoryMoney{
						Total:    domain.UnpricedMoney(),
						Complete: false, // partial money: one model unpriced
					},
				},
				Orchestrator: domain.ActorReport{Actor: domain.Orchestrator()},
			},
		},
	}

	total, ok := app.RunTotalFor(report, appTestRunID)

	if !ok {
		t.Fatalf("RunTotalFor returned (_, false) for an existing run")
	}
	if total.Complete {
		t.Error("total.Complete must be false when the run has unpriced models")
	}
}

func TestRunTotalFor_ProvisionalRun_ProvisionalFlagSet(t *testing.T) {
	// A provisional run (no run_end/session_end observed) must have Provisional=true
	// in the extracted total.
	report := domain.Report{
		Runs: []domain.RunReport{
			{
				Run:          appTestRunRef,
				Provisional:  true,
				Orchestrator: domain.ActorReport{Actor: domain.Orchestrator()},
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{Input: domain.Tokens(500)},
				},
			},
		},
	}

	total, ok := app.RunTotalFor(report, appTestRunID)

	if !ok {
		t.Fatalf("RunTotalFor returned (_, false) for an existing run")
	}
	if !total.Provisional {
		t.Error("total.Provisional must be true for a provisional run")
	}
}

// ---------------------------------------------------------------------------
// TotalForRun: end-to-end use-case entry point
// ---------------------------------------------------------------------------

func TestTotalForRun_OneRun_ReturnsCorrectTotal(t *testing.T) {
	// TotalForRun must run the analysis flow and return the total for the
	// requested run ID.
	const modelA = domain.ModelID("model-a")
	const agent1 = domain.AgentInstanceID("TestAgent#1")

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              appTestRunRef,
						Dir:              "/test-logs/20260101T000000Z-abcd",
						OrchestratorFile: appTestOrchFile,
						Agents: []domain.AgentEntry{
							{
								Dir:          "/test-logs/20260101T000000Z-abcd/TestAgent#1",
								InstanceHint: agent1,
								EventFile:    appTestAgent1File,
							},
						},
					},
				},
			}, nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(appTestOrchFile, appTestRunEndEvent())
	reader.addEvents(appTestAgent1File,
		appTestInvEndEvent(agent1, modelA, domain.TokenUsage{
			Input:  domain.Tokens(1_000_000),
			Output: domain.Tokens(500_000),
		}),
	)

	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing(modelA)})

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	total, outcome, err := svc.TotalForRun(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	}, appTestRunID)

	if err != nil {
		t.Fatalf("TotalForRun returned unexpected error: %v", err)
	}
	if outcome != app.OutcomeReport {
		t.Fatalf("outcome = %v, want OutcomeReport", outcome)
	}
	if total.RunID != appTestRunID {
		t.Errorf("total.RunID = %q, want %q", total.RunID, appTestRunID)
	}
}

func TestTotalForRun_UnpricedModel_CompleteFalse(t *testing.T) {
	// When a run contains an unpriced model, TotalForRun must return a RunTotal
	// with Complete=false to signal that the money amount is a partial sum.
	const agent1 = domain.AgentInstanceID("TestAgent#1")

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              appTestRunRef,
						Dir:              "/test-logs/20260101T000000Z-abcd",
						OrchestratorFile: appTestOrchFile,
						Agents: []domain.AgentEntry{
							{
								Dir:          "/test-logs/20260101T000000Z-abcd/TestAgent#1",
								InstanceHint: agent1,
								EventFile:    appTestAgent1File,
							},
						},
					},
				},
			}, nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(appTestOrchFile, appTestRunEndEvent())
	// "unknown-model" has no pricing entry → unpriced.
	reader.addEvents(appTestAgent1File,
		appTestInvEndEvent(agent1, "unknown-model", domain.TokenUsage{
			Input:  domain.Tokens(1_000_000),
			Output: domain.Tokens(500_000),
		}),
	)

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     newFakePricingStore(), // empty: unknown-model unpriced
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	total, outcome, err := svc.TotalForRun(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	}, appTestRunID)

	if err != nil {
		t.Fatalf("TotalForRun returned unexpected error: %v", err)
	}
	if outcome != app.OutcomeReport {
		t.Fatalf("outcome = %v, want OutcomeReport", outcome)
	}
	if total.Complete {
		t.Error("total.Complete must be false when the run contains an unpriced model")
	}
}

func TestTotalForRun_SourceNotFound_OutcomeSourceNotFound(t *testing.T) {
	// TotalForRun must propagate the OutcomeSourceNotFound outcome when no source
	// is available and the prompt is skipped.
	svc := app.New(app.Deps{
		Source:      &fakeLogSource{}, // Default always returns SourceNotFound.
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	_, outcome, err := svc.TotalForRun(context.Background(), app.Request{}, appTestRunID)

	if err != nil {
		t.Errorf("TotalForRun must not return an error for a missing source; got: %v", err)
	}
	if outcome != app.OutcomeSourceNotFound {
		t.Errorf("outcome = %v, want OutcomeSourceNotFound", outcome)
	}
}

func TestTotalForRun_RunIDNotInReport_ReturnsFalseOutcome(t *testing.T) {
	// When TotalForRun succeeds but the requested run ID is not in the analysis
	// result (e.g. the source contains a different run), the function must
	// communicate that the run was not found without panicking.
	const modelA = domain.ModelID("model-a")
	const agent1 = domain.AgentInstanceID("TestAgent#1")

	src := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return src },
		enumerateFunc: func(s domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: s,
				Runs: []domain.RunEntry{
					{
						Run:              appTestRunRef, // run ID: appTestRunID
						Dir:              "/test-logs/20260101T000000Z-abcd",
						OrchestratorFile: appTestOrchFile,
						Agents: []domain.AgentEntry{
							{
								Dir:          "/test-logs/20260101T000000Z-abcd/TestAgent#1",
								InstanceHint: agent1,
								EventFile:    appTestAgent1File,
							},
						},
					},
				},
			}, nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(appTestOrchFile, appTestRunEndEvent())
	reader.addEvents(appTestAgent1File,
		appTestInvEndEvent(agent1, modelA, domain.TokenUsage{Input: domain.Tokens(100)}),
	)

	store := newFakePricingStore()
	store.table = domain.NewPricingTable([]domain.ModelPricing{flatPricing(modelA)})

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	// Request a run ID that does not exist in the analysis result.
	total, outcome, err := svc.TotalForRun(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	}, "20260101T999999Z-zzzz")

	// The function must not panic and must communicate the not-found outcome.
	// A zero-value RunTotal with an appropriate outcome is the expected shape.
	if err != nil {
		t.Errorf("TotalForRun returned unexpected error for a missing run: %v", err)
	}
	// Outcome should be some non-report indicator, or total should be the zero value.
	// Accept any outcome other than OutcomeReport when the run was not in the result.
	if outcome == app.OutcomeReport && total.RunID == "" {
		// ok: returned an empty total — acceptable indication of not found
	} else if outcome == app.OutcomeReport && total.RunID != "" {
		t.Errorf("TotalForRun returned a non-empty RunTotal for a run ID not in the analysis result; "+
			"RunID=%q", total.RunID)
	}
}
