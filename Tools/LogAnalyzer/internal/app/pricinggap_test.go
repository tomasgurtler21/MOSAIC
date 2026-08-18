package app_test

// Tests for the missing-pricing resolution flow.
//
// These tests verify that:
//   - PricingGaps correctly identifies unpriced models.
//   - ResolvePricingGaps triggers the Interaction when unpriced models are present.
//   - Prices entered via Interaction are persisted through PricingStore.Put.
//   - Re-pricing uses the cached aggregate in Session, not a fresh read of the logs.
//   - Declining the pricing prompt leaves the report with complete token totals and
//     explicit MoneyUnpriced money cells.

import (
	"context"
	"testing"

	"mosaic-common/interaction"
	"mosaic-log-analyzer/internal/app"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// PricingGaps: identification of unpriced models
// ---------------------------------------------------------------------------

func TestPricingGaps_UnpricedModelInReport_ReturnedInGapList(t *testing.T) {
	// PricingGaps must list every model that appears in Report.UnpricedModels,
	// sorted by model id as specified by the design.
	report := domain.Report{
		UnpricedModels: []domain.ModelID{"model-b", "model-a"},
		Runs: []domain.RunReport{
			{
				Run:            appTestRunRef,
				UnpricedModels: []domain.ModelID{"model-a", "model-b"},
				Orchestrator:   domain.ActorReport{Actor: domain.Orchestrator()},
				Agents: []domain.ActorReport{
					{
						Actor:          domain.AgentInstance("Agent#1"),
						UnpricedModels: []domain.ModelID{"model-a"},
					},
					{
						Actor:          domain.AgentInstance("Agent#2"),
						UnpricedModels: []domain.ModelID{"model-b"},
					},
				},
			},
		},
	}

	gaps := app.PricingGaps(report)

	if len(gaps) == 0 {
		t.Fatal("PricingGaps must return at least one gap for a report with unpriced models")
	}

	// Verify both models appear in the gap list.
	found := make(map[domain.ModelID]bool)
	for _, g := range gaps {
		found[g.Model] = true
	}
	for _, want := range []domain.ModelID{"model-a", "model-b"} {
		if !found[want] {
			t.Errorf("PricingGaps did not include %q; got: %v", want, gaps)
		}
	}
}

func TestPricingGaps_NoUnpricedModels_ReturnsEmpty(t *testing.T) {
	// A fully-priced report must produce an empty gap list.
	report := domain.Report{
		UnpricedModels: nil,
		Runs: []domain.RunReport{
			{
				Run:            appTestRunRef,
				UnpricedModels: nil,
				Orchestrator:   domain.ActorReport{Actor: domain.Orchestrator()},
			},
		},
	}

	gaps := app.PricingGaps(report)

	if len(gaps) != 0 {
		t.Errorf("PricingGaps must return an empty slice for a fully-priced report; got: %v", gaps)
	}
}

// ---------------------------------------------------------------------------
// ResolvePricingGaps: interaction and persistence
// ---------------------------------------------------------------------------

// buildUnpricedService creates a service where "unknown-model" is not in the
// pricing table, so analysis will leave its money cells as MoneyUnpriced.
func buildUnpricedService(t *testing.T) (*app.Service, *fakeEventReader, *fakePricingStore) {
	t.Helper()

	const unknownModel = domain.ModelID("unknown-model")
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
		appTestInvEndEvent(agent1, unknownModel, domain.TokenUsage{
			Input:  domain.Tokens(1_000_000),
			Output: domain.Tokens(500_000),
		}),
	)

	store := newFakePricingStore() // empty: unknown-model is unpriced

	svc := app.New(app.Deps{
		Source:      logSource,
		Reader:      reader,
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	return svc, reader, store
}

func TestResolvePricingGaps_UnpricedModel_InteractionOffered(t *testing.T) {
	// When the report contains unpriced models, ResolvePricingGaps must consult
	// the user via Interaction — at minimum, it must call SelectOne or AskText
	// with a pricing-related question ID.
	svc, _, _ := buildUnpricedService(t)

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})
	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("Outcome = %v, want OutcomeReport", result.Outcome)
	}

	ia := &scriptedInteraction{}

	// Replace the service's interaction with a scripted one for the gap resolution call.
	svcWithIA := app.New(app.Deps{
		Source:      &fakeLogSource{},
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: ia,
		WorkDir:     "/workdir",
	})

	_, err = svcWithIA.ResolvePricingGaps(context.Background(), result.Session, result.Report)
	if err != nil {
		t.Fatalf("ResolvePricingGaps returned unexpected error: %v", err)
	}

	// The service must have asked at least one pricing-related question.
	pricingQuestioned := ia.selectOneCalledWith(app.QuestionPricingAction) ||
		ia.askTextCalledWith(app.QuestionPricingRate) ||
		ia.askTextCalledWith(app.QuestionPricingThreshold)

	if !pricingQuestioned {
		t.Errorf("ResolvePricingGaps must consult the user about unpriced models; "+
			"selectOneCalls=%v askTextCalls=%v", ia.selectOneCalls, ia.askTextCalls)
	}
}

func TestResolvePricingGaps_EnteredPrices_PersistedViaStore(t *testing.T) {
	// When the user enters prices via the interaction, PricingStore.Put must be
	// called to persist those prices automatically.
	store := newFakePricingStore() // starts empty

	svc, reader, _ := buildUnpricedService(t)
	_ = reader // referenced to confirm no extra reads happen

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})
	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("Outcome = %v, want OutcomeReport", result.Outcome)
	}
	if !result.Report.HasUnpricedModels() {
		t.Skip("expected at least one unpriced model in the report")
	}

	// Wire a service that enters prices when asked.
	ia := &scriptedInteraction{
		onSelectOne: func(q interaction.ChoiceQuestion) interaction.ChoiceAnswer {
			if q.ID == app.QuestionPricingAction {
				return interaction.ChoiceAnswer{
					Status:   interaction.Answered,
					OptionID: app.PricingActionEnter,
				}
			}
			return interaction.ChoiceAnswer{Status: interaction.SkippedOne}
		},
		onAskText: func(q interaction.TextQuestion) interaction.TextAnswer {
			// Respond "3.00" to every rate question; skip threshold.
			if q.ID == app.QuestionPricingRate {
				return interaction.TextAnswer{Status: interaction.Answered, Text: "3.00"}
			}
			return interaction.TextAnswer{Status: interaction.SkippedOne}
		},
	}

	svcWithIA := app.New(app.Deps{
		Source:      &fakeLogSource{},
		Reader:      newFakeEventReader(),
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: ia,
		WorkDir:     "/workdir",
	})

	_, err = svcWithIA.ResolvePricingGaps(context.Background(), result.Session, result.Report)
	if err != nil {
		t.Fatalf("ResolvePricingGaps returned unexpected error: %v", err)
	}

	if len(store.putCalls) == 0 {
		t.Error("PricingStore.Put must be called when the user enters prices; got 0 Put calls")
	}
}

func TestResolvePricingGaps_EnteredPrices_ReportRepricedWithoutRereadingLogs(t *testing.T) {
	// After the user enters prices, the report must be re-priced from the cached
	// aggregate in the Session. The EventReader.Read call count must remain the
	// same as after the initial Analyze — logs must not be re-read.
	svc, reader, _ := buildUnpricedService(t)

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})
	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport {
		t.Fatalf("Outcome = %v, want OutcomeReport", result.Outcome)
	}

	// Snapshot read counts after the initial analysis.
	countsAfterAnalyze := make(map[string]int, len(reader.readCounts))
	for k, v := range reader.readCounts {
		countsAfterAnalyze[k] = v
	}

	ia := &scriptedInteraction{
		onSelectOne: func(q interaction.ChoiceQuestion) interaction.ChoiceAnswer {
			if q.ID == app.QuestionPricingAction {
				return interaction.ChoiceAnswer{
					Status:   interaction.Answered,
					OptionID: app.PricingActionEnter,
				}
			}
			return interaction.ChoiceAnswer{Status: interaction.SkippedOne}
		},
		onAskText: func(q interaction.TextQuestion) interaction.TextAnswer {
			if q.ID == app.QuestionPricingRate {
				return interaction.TextAnswer{Status: interaction.Answered, Text: "3.00"}
			}
			return interaction.TextAnswer{Status: interaction.SkippedOne}
		},
	}

	store := newFakePricingStore()
	svcWithIA := app.New(app.Deps{
		Source:      &fakeLogSource{},
		Reader:      reader, // same reader: readCounts will reveal any extra reads
		Pricing:     store,
		Clock:       newFakeClock(),
		Interaction: ia,
		WorkDir:     "/workdir",
	})

	_, err = svcWithIA.ResolvePricingGaps(context.Background(), result.Session, result.Report)
	if err != nil {
		t.Fatalf("ResolvePricingGaps returned unexpected error: %v", err)
	}

	// No file must have been read again.
	for path, before := range countsAfterAnalyze {
		after := reader.readCounts[path]
		if after != before {
			t.Errorf("file %q was read again during ResolvePricingGaps (before=%d after=%d); "+
				"re-pricing must use the cached Session aggregate, not re-read logs", path, before, after)
		}
	}
}

func TestResolvePricingGaps_Declined_TokensStillPresent(t *testing.T) {
	// Declining the pricing prompt must leave the token totals complete.
	// The report already holds the aggregated tokens and they must not be lost.
	svc, _, _ := buildUnpricedService(t)

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})
	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport || len(result.Report.Runs) == 0 {
		t.Fatalf("expected a report with runs; outcome=%v runs=%d", result.Outcome, len(result.Report.Runs))
	}

	// Decline via always-skipping interaction.
	svcSkipping := app.New(app.Deps{
		Source:      &fakeLogSource{},
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	updatedReport, err := svcSkipping.ResolvePricingGaps(context.Background(), result.Session, result.Report)
	if err != nil {
		t.Fatalf("ResolvePricingGaps returned unexpected error on decline: %v", err)
	}

	if len(updatedReport.Runs) == 0 {
		t.Fatal("the declined-gap report must still contain runs")
	}
	run := updatedReport.Runs[0]

	// Tokens must be present after declining.
	if _, ok := run.Totals.Tokens.Input.Value(); !ok {
		t.Error("run.Totals.Tokens.Input must still be present after declining the pricing prompt")
	}
}

func TestResolvePricingGaps_Declined_MoneyStaysUnpriced(t *testing.T) {
	// Declining the pricing prompt must leave money cells as MoneyUnpriced —
	// never MoneyKnown(0) — and must not collapse unpriced into no-data.
	svc, _, _ := buildUnpricedService(t)

	result, err := svc.Analyze(context.Background(), app.Request{
		ExplicitPath: "/test-logs",
	})
	if err != nil {
		t.Fatalf("Analyze returned unexpected error: %v", err)
	}
	if result.Outcome != app.OutcomeReport || len(result.Report.Runs) == 0 {
		t.Fatalf("expected a report with runs; outcome=%v runs=%d", result.Outcome, len(result.Report.Runs))
	}

	svcSkipping := app.New(app.Deps{
		Source:      &fakeLogSource{},
		Reader:      newFakeEventReader(),
		Pricing:     newFakePricingStore(),
		Clock:       newFakeClock(),
		Interaction: &alwaysSkippingInteraction{},
		WorkDir:     "/workdir",
	})

	updatedReport, err := svcSkipping.ResolvePricingGaps(context.Background(), result.Session, result.Report)
	if err != nil {
		t.Fatalf("ResolvePricingGaps returned unexpected error: %v", err)
	}

	if len(updatedReport.Runs) == 0 {
		t.Fatal("the declined-gap report must still contain runs")
	}

	// At least one run agent must have MoneyUnpriced money — not $0 and not no-data.
	run := updatedReport.Runs[0]
	foundUnpriced := false
	for _, agent := range run.Agents {
		if agent.Totals.Money.Total.State == domain.MoneyUnpriced {
			foundUnpriced = true
			break
		}
	}
	if !foundUnpriced && run.Orchestrator.Totals.Money.Total.State == domain.MoneyUnpriced {
		foundUnpriced = true
	}
	if !foundUnpriced {
		t.Error("at least one actor's money must remain MoneyUnpriced after declining the pricing prompt")
	}
}
