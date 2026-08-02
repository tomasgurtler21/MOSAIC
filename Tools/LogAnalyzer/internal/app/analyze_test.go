package app_test

// Tests for the end-to-end analysis flow and the no-data case.
//
// These tests verify that:
//   - A source with one run and several agent instances produces a report whose
//     run totals match the expected aggregation.
//   - Each event file is read exactly once even after re-pricing, so logs are
//     never re-read.
//   - A usable but empty source produces OutcomeNoData, not an error.

import (
	"context"
	"testing"

	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// End-to-end analysis: one run, multiple agents
// ---------------------------------------------------------------------------

// buildOneRunTwoAgentService constructs a service wired for one named run
// with an orchestrator stream and two agent-instance streams. The model
// "model-a" is priced. Returns both the service and the event reader so the
// caller can inspect readCounts.
func buildOneRunTwoAgentService(t *testing.T) (*app.Service, *fakeEventReader) {
	t.Helper()

	const modelA = domain.ModelID("model-a")
	const agent1 = domain.AgentInstanceID("TestAgent#1")
	const agent2 = domain.AgentInstanceID("TestAgent#2")

	logSrc := domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return logSrc },
		enumerateFunc: func(src domain.Source) (domain.Inventory, []domain.Finding) {
			return oneRunTwoAgentInventory(src), nil
		},
	}

	reader := newFakeEventReader()
	// Orchestrator stream: assistant turn event (orchestrator usage source).
	reader.addEvents(appTestOrchFile,
		appTestTurnEvent(modelA, domain.TokenUsage{Input: domain.Tokens(1_000_000)}),
		appTestRunEndEvent(),
	)
	// Agent#1: invocation_end event (agent usage source).
	reader.addEvents(appTestAgent1File,
		appTestInvEndEvent(agent1, modelA, domain.TokenUsage{
			Input:  domain.Tokens(1_000_000),
			Output: domain.Tokens(500_000),
		}),
	)
	// Agent#2: invocation_end event.
	reader.addEvents(appTestAgent2File,
		appTestInvEndEvent(agent2, modelA, domain.TokenUsage{
			Input:  domain.Tokens(2_000_000),
			Output: domain.Tokens(1_000_000),
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

	return svc, reader
}

func TestAnalyze_OneRunMultipleAgents_ReportHasOneRun(t *testing.T) {
	// A source with one named run must produce a report containing exactly one
	// named run entry.
	svc, _ := buildOneRunTwoAgentService(t)

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("Outcome = %v, want OutcomeReport", result.Outcome)
	}
	if len(result.Report.Runs) != 1 {
		t.Errorf("len(Report.Runs) = %d, want 1", len(result.Report.Runs))
	}
}

func TestAnalyze_OneRunMultipleAgents_RunHasTwoAgents(t *testing.T) {
	// The single named run must contain two agent actors — one for each agent
	// instance whose event file was enumerated.
	svc, _ := buildOneRunTwoAgentService(t)

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil || result.Outcome != app.OutcomeReport || len(result.Report.Runs) == 0 {
		t.Fatalf("expected a report with one run; err=%v outcome=%v runs=%d",
			err, result.Outcome, len(result.Report.Runs))
	}
	run := result.Report.Runs[0]
	if len(run.Agents) != 2 {
		t.Errorf("len(run.Agents) = %d, want 2", len(run.Agents))
	}
}

func TestAnalyze_OneRunMultipleAgents_InputTokensTotalMatchesExpected(t *testing.T) {
	// The run-level input token total must equal the sum of all agent and
	// orchestrator input tokens: 1M (orch) + 1M (agent1) + 2M (agent2) = 4M.
	svc, _ := buildOneRunTwoAgentService(t)

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil || result.Outcome != app.OutcomeReport || len(result.Report.Runs) == 0 {
		t.Fatalf("expected a report with one run; err=%v outcome=%v", err, result.Outcome)
	}
	run := result.Report.Runs[0]

	wantInput := int64(4_000_000)
	v, ok := run.Totals.Tokens.Input.Value()
	if !ok {
		t.Error("run.Totals.Tokens.Input must be present")
	} else if v != wantInput {
		t.Errorf("run.Totals.Tokens.Input = %d, want %d", v, wantInput)
	}
}

func TestAnalyze_OneRunMultipleAgents_OutputTokensTotalMatchesExpected(t *testing.T) {
	// The run-level output token total must equal 500K (agent1) + 1M (agent2) = 1.5M.
	// The orchestrator turn carries no output tokens in this scenario.
	svc, _ := buildOneRunTwoAgentService(t)

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil || result.Outcome != app.OutcomeReport || len(result.Report.Runs) == 0 {
		t.Fatalf("expected a report with one run; err=%v outcome=%v", err, result.Outcome)
	}
	run := result.Report.Runs[0]

	wantOutput := int64(1_500_000)
	v, ok := run.Totals.Tokens.Output.Value()
	if !ok {
		t.Error("run.Totals.Tokens.Output must be present")
	} else if v != wantOutput {
		t.Errorf("run.Totals.Tokens.Output = %d, want %d", v, wantOutput)
	}
}

func TestAnalyze_OneRunMultipleAgents_EachFileReadExactlyOnce(t *testing.T) {
	// Each discovered event file must be read exactly once during a single Analyze
	// call. This is the anti-double-read invariant from the design.
	svc, reader := buildOneRunTwoAgentService(t)

	_, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})
	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}

	for _, path := range []string{appTestOrchFile, appTestAgent1File, appTestAgent2File} {
		if reader.readCounts[path] != 1 {
			t.Errorf("read count for %q = %d, want 1", path, reader.readCounts[path])
		}
	}
}

func TestAnalyze_PricedModel_MoneyKnownInReport(t *testing.T) {
	// When all models are priced, the run totals must carry MoneyKnown money cells,
	// not MoneyUnpriced or MoneyNoData.
	svc, _ := buildOneRunTwoAgentService(t)

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil || result.Outcome != app.OutcomeReport || len(result.Report.Runs) == 0 {
		t.Fatalf("expected a report with one run; err=%v outcome=%v", err, result.Outcome)
	}
	money := result.Report.Runs[0].Totals.Money

	if money.Input.State != domain.MoneyKnown {
		t.Errorf("run.Totals.Money.Input.State = %v, want MoneyKnown", money.Input.State)
	}
}

func TestAnalyze_PricedModel_MoneyCompleteIsTrue(t *testing.T) {
	// When every model in the run is priced, CategoryMoney.Complete must be true.
	svc, _ := buildOneRunTwoAgentService(t)

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil || result.Outcome != app.OutcomeReport || len(result.Report.Runs) == 0 {
		t.Fatalf("expected a report with one run; err=%v outcome=%v", err, result.Outcome)
	}
	if !result.Report.Runs[0].Totals.Money.Complete {
		t.Error("Totals.Money.Complete must be true when all models are priced")
	}
}

func TestAnalyze_ReportCarriesRunID(t *testing.T) {
	// The report's run must carry the expected run ID so frontends can identify it.
	svc, _ := buildOneRunTwoAgentService(t)

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})

	if err != nil || result.Outcome != app.OutcomeReport || len(result.Report.Runs) == 0 {
		t.Fatalf("expected a report with one run; err=%v outcome=%v", err, result.Outcome)
	}
	run := result.Report.Runs[0]
	if run.Run.ID != appTestRunID {
		t.Errorf("run.Run.ID = %q, want %q", run.Run.ID, appTestRunID)
	}
}

// ---------------------------------------------------------------------------
// No-data flow: usable but empty source
// ---------------------------------------------------------------------------

func TestAnalyze_EmptySource_OutcomeIsNoData(t *testing.T) {
	// A source that is structurally valid (SourceLogsRoot) but contains no run
	// folders and no unattributable bucket must produce OutcomeNoData, not an
	// error and not OutcomeSourceNotFound.
	emptySource := domain.Source{Kind: domain.SourceLogsRoot, Path: "/empty-logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return emptySource },
		enumerateFunc: func(src domain.Source) (domain.Inventory, []domain.Finding) {
			// Empty inventory: no runs, no unattributable bucket.
			return domain.Inventory{Source: src}, nil
		},
	}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/empty-logs",
	})

	if err != nil {
		t.Errorf("Analyze must not return an error for an empty source; got: %v", err)
	}
	if result.Outcome != app.OutcomeNoData {
		t.Errorf("Outcome = %v, want OutcomeNoData for an empty source", result.Outcome)
	}
}

func TestAnalyze_EmptySource_NoError(t *testing.T) {
	// The empty-source case is a described outcome, never an error.
	emptySource := domain.Source{Kind: domain.SourceLogsRoot, Path: "/empty-logs"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return emptySource },
		enumerateFunc: func(src domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{Source: src}, nil
		},
	}

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	_, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/empty-logs",
	})

	if err != nil {
		t.Errorf("Analyze returned unexpected error for empty source: %v", err)
	}
}

func TestAnalyze_SingleRunFolder_OutcomeReport(t *testing.T) {
	// A source classified as SourceSingleRun (the path is itself one run folder)
	// must also produce a report when events are present — not only SourceLogsRoot
	// sources are usable.
	const modelA = domain.ModelID("model-a")
	const agent1 = domain.AgentInstanceID("TestAgent#1")

	singleRunSrc := domain.Source{Kind: domain.SourceSingleRun, Path: "/test-logs/20260101T000000Z-abcd"}

	logSource := &fakeLogSource{
		classifyFunc: func(_ string) domain.Source { return singleRunSrc },
		enumerateFunc: func(src domain.Source) (domain.Inventory, []domain.Finding) {
			return domain.Inventory{
				Source: src,
				Runs: []domain.RunEntry{
					{
						Run:              appTestRunRef,
						Dir:              "/test-logs/20260101T000000Z-abcd",
						OrchestratorFile: appTestOrchFile,
						Agents: []domain.AgentEntry{
							{
								Dir:          "/test-logs/20260101T000000Z-abcd/TestAgent#1",
								InstanceHint: "TestAgent#1",
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
		appTestInvEndEvent(agent1, modelA, domain.TokenUsage{Input: domain.Tokens(500_000)}),
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

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs/20260101T000000Z-abcd",
	})

	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Errorf("Outcome = %v, want OutcomeReport for a single-run source with events", result.Outcome)
	}
}
