package app_test

// Tests for the basic total-cost-per-run query path.
//
// These tests verify that:
//   - RunTotalFor correctly extracts the total for a named run from a report.
//   - RunTotalFor returns (zero, false) for a run ID not in the report.
//   - RunTotalFor sets Complete=false when any model in the run was unpriced.
//   - RunTotalFor carries UnpricedModels from the run entry into the result.
//   - RunTotalFor populates PartialAmount with the priced-actors' contribution
//     when the run mixes priced and unpriced models.
//   - RunTotalFor leaves PartialAmount zero-valued when no models were priced.
//   - TotalForRun runs the full analysis flow and returns the named run's total.
//   - TotalForRun returns the OutcomeSourceNotFound outcome when no source is found.
//   - TotalForRun populates UnpricedModels and PartialAmount for a mixed-model run.

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

// ---------------------------------------------------------------------------
// RunTotalFor: unpriced model identity (T5.1)
// ---------------------------------------------------------------------------

func TestRunTotalFor_UnpricedModel_UnpricedModelsCarried(t *testing.T) {
	// RunTotalFor must carry the run's UnpricedModels into the result so a
	// consumer can name the model(s) that prevented complete pricing. The
	// identity is already present on the RunReport; this test pins that it
	// reaches the RunTotal without truncation.
	const unpricedModel = domain.ModelID("unknown-model-v1")
	report := domain.Report{
		Runs: []domain.RunReport{
			{
				Run:            appTestRunRef,
				UnpricedModels: []domain.ModelID{unpricedModel},
				Totals: domain.Totals{
					Tokens: domain.TokenUsage{Input: domain.Tokens(1_000_000)},
					Money: domain.CategoryMoney{
						Total:    domain.UnpricedMoney(),
						Complete: false,
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
	if len(total.UnpricedModels) != 1 || total.UnpricedModels[0] != unpricedModel {
		t.Errorf("total.UnpricedModels = %v, want [%q]", total.UnpricedModels, unpricedModel)
	}
}

func TestRunTotalFor_MultipleUnpricedModels_AllNamed(t *testing.T) {
	// When a run has two distinct unpriced models, both must appear in
	// total.UnpricedModels so the consumer can report every model by name.
	unpriced := []domain.ModelID{"model-x", "model-y"}
	report := domain.Report{
		Runs: []domain.RunReport{
			{
				Run:            appTestRunRef,
				UnpricedModels: unpriced,
				Totals: domain.Totals{
					Money: domain.CategoryMoney{
						Total:    domain.UnpricedMoney(),
						Complete: false,
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
	if len(total.UnpricedModels) != 2 {
		t.Fatalf("len(total.UnpricedModels) = %d, want 2", len(total.UnpricedModels))
	}
	foundX, foundY := false, false
	for _, m := range total.UnpricedModels {
		switch m {
		case "model-x":
			foundX = true
		case "model-y":
			foundY = true
		}
	}
	if !foundX {
		t.Errorf("total.UnpricedModels missing %q; got %v", "model-x", total.UnpricedModels)
	}
	if !foundY {
		t.Errorf("total.UnpricedModels missing %q; got %v", "model-y", total.UnpricedModels)
	}
}

func TestRunTotalFor_FullyPricedRun_UnpricedModelsEmpty(t *testing.T) {
	// A fully-priced run must produce an empty UnpricedModels slice. No model
	// name should appear when every model contributing to the run had pricing.
	report := domain.Report{
		Runs: []domain.RunReport{
			{
				Run:            appTestRunRef,
				UnpricedModels: nil,
				Totals: domain.Totals{
					Money: domain.CategoryMoney{
						Total:    domain.KnownMoney(domain.Money(1_000_000_000)),
						Complete: true,
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
	if len(total.UnpricedModels) != 0 {
		t.Errorf("total.UnpricedModels = %v, want empty for a fully-priced run",
			total.UnpricedModels)
	}
}

func TestRunTotalFor_PartiallyPricedRun_PartialAmountCarriesKnownPortion(t *testing.T) {
	// When a run mixes a priced actor (orchestrator) with an unpriced actor
	// (agent), PartialAmount must carry the amount attributable to the priced
	// actor so a consumer can display it alongside the unpriced model names.
	const wantNanos = int64(5_000_000_000) // $5.00
	report := domain.Report{
		Runs: []domain.RunReport{
			{
				Run:            appTestRunRef,
				UnpricedModels: []domain.ModelID{"unknown-model"},
				Totals: domain.Totals{
					Money: domain.CategoryMoney{
						// The run total reflects only the priced actor's contribution
						// because the unpriced actor contributes nothing countable.
						Total:    domain.KnownMoney(domain.Money(wantNanos)),
						Complete: false, // unpriced model present
					},
				},
				Orchestrator: domain.ActorReport{
					Actor:          domain.Orchestrator(),
					UnpricedModels: nil, // orchestrator is fully priced
					Totals: domain.Totals{
						Money: domain.CategoryMoney{
							Total:    domain.KnownMoney(domain.Money(wantNanos)),
							Complete: true,
						},
					},
				},
				Agents: []domain.ActorReport{
					{
						Actor:          domain.AgentInstance("TestAgent#1"),
						UnpricedModels: []domain.ModelID{"unknown-model"},
						Totals: domain.Totals{
							Money: domain.CategoryMoney{
								Total:    domain.UnpricedMoney(),
								Complete: false,
							},
						},
					},
				},
			},
		},
	}

	total, ok := app.RunTotalFor(report, appTestRunID)

	if !ok {
		t.Fatalf("RunTotalFor returned (_, false) for an existing run")
	}
	if total.PartialAmount.State != domain.MoneyKnown {
		t.Fatalf("total.PartialAmount.State = %v, want MoneyKnown for a partially-priced run",
			total.PartialAmount.State)
	}
	if got := total.PartialAmount.Amount.Nanos(); got != wantNanos {
		t.Errorf("total.PartialAmount.Amount = %d nanos, want %d nanos ($5.00)", got, wantNanos)
	}
}

func TestRunTotalFor_AllModelsUnpriced_PartialAmountZero(t *testing.T) {
	// When no model in the run was priced, PartialAmount must remain zero-valued.
	// There is nothing to attribute, so MoneyNoData (the zero value) is correct.
	report := domain.Report{
		Runs: []domain.RunReport{
			{
				Run:            appTestRunRef,
				UnpricedModels: []domain.ModelID{"unknown-model"},
				Totals: domain.Totals{
					Money: domain.CategoryMoney{
						Total:    domain.UnpricedMoney(),
						Complete: false,
					},
				},
				Orchestrator: domain.ActorReport{
					Actor:          domain.Orchestrator(),
					UnpricedModels: []domain.ModelID{"unknown-model"},
					Totals: domain.Totals{
						Money: domain.CategoryMoney{
							Total:    domain.UnpricedMoney(),
							Complete: false,
						},
					},
				},
			},
		},
	}

	total, ok := app.RunTotalFor(report, appTestRunID)

	if !ok {
		t.Fatalf("RunTotalFor returned (_, false) for an existing run")
	}
	// Zero-valued MoneyValue has State == MoneyNoData. When nothing was priced
	// there is no partial amount to carry, so the field must be zero-valued.
	if total.PartialAmount.State != domain.MoneyNoData {
		t.Errorf("total.PartialAmount.State = %v, want MoneyNoData (zero-valued) when no models are priced",
			total.PartialAmount.State)
	}
}

// ---------------------------------------------------------------------------
// TotalForRun: unpriced model identity via full analysis pipeline (T5.1)
// ---------------------------------------------------------------------------

func TestTotalForRun_MixedModels_UnpricedModelsAndPartialAmount(t *testing.T) {
	// A run with one priced model (model-a) and one unpriced model (unknown-model)
	// must return a RunTotal where UnpricedModels names the unpriced model and
	// PartialAmount carries a known amount from the priced actor's contribution.
	const modelA = domain.ModelID("model-a")
	const agent1 = domain.AgentInstanceID("TestAgent#1")
	const agent2 = domain.AgentInstanceID("TestAgent#2")

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
							{
								Dir:          "/test-logs/20260101T000000Z-abcd/TestAgent#2",
								InstanceHint: agent2,
								EventFile:    appTestAgent2File,
							},
						},
					},
				},
			}, nil
		},
	}

	reader := newFakeEventReader()
	reader.addEvents(appTestOrchFile, appTestRunEndEvent())
	// Agent1 uses model-a (priced); its tokens contribute to PartialAmount.
	reader.addEvents(appTestAgent1File,
		appTestInvEndEvent(agent1, modelA, domain.TokenUsage{
			Input:  domain.Tokens(1_000_000),
			Output: domain.Tokens(500_000),
		}),
	)
	// Agent2 uses unknown-model (not in pricing table); this causes incompleteness.
	reader.addEvents(appTestAgent2File,
		appTestInvEndEvent(agent2, "unknown-model", domain.TokenUsage{
			Input:  domain.Tokens(500_000),
			Output: domain.Tokens(250_000),
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

	// The unpriced model must be named in UnpricedModels.
	if len(total.UnpricedModels) == 0 {
		t.Error("total.UnpricedModels is empty; want it to contain the unpriced model name")
	} else {
		found := false
		for _, m := range total.UnpricedModels {
			if m == "unknown-model" {
				found = true
			}
		}
		if !found {
			t.Errorf("total.UnpricedModels = %v; want to contain %q", total.UnpricedModels, "unknown-model")
		}
	}

	// The priced actor's contribution must be present in PartialAmount.
	if total.PartialAmount.State != domain.MoneyKnown {
		t.Errorf("total.PartialAmount.State = %v, want MoneyKnown; a priced model (model-a) contributed to the run",
			total.PartialAmount.State)
	}
}
