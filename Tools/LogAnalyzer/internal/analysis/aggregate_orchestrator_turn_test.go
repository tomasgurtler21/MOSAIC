package analysis_test

// Tests for which orchestrator-stream turn events count as orchestrator usage.
//
// Rule under test: on the orchestrator stream, a turn whose role is "user" is
// not a model invocation and contributes nothing. Assistant turns, and turns
// with an empty or any other role, are counted. Subagent and usage_record
// rules are covered in aggregate_test.go.
//
// Helpers (orchStream, turnEvent, usageRecordEvent, requireOneRun, ...) are
// shared from aggregate_test.go in the same package.

import (
	"testing"

	"mosaic-log-analyzer/internal/analysis"
	"mosaic-log-analyzer/internal/domain"
)

const pricedTurnModel = domain.ModelID("priced-model")

// roleTurnEvent builds a turn event with an explicit role and no model/usage
// unless supplied.
func roleTurnEvent(role domain.TurnRole, model domain.ModelID, usage domain.TokenUsage) domain.Event {
	return domain.Event{
		Type: domain.EventTurn,
		Turn: &domain.TurnFields{
			Role:     role,
			Model:    model,
			Usage:    usage,
			HasUsage: !usage.IsEmpty(),
		},
	}
}

// userTurnEvent builds a user turn as adapters emit it: no model, no usage.
func userTurnEvent() domain.Event {
	return roleTurnEvent(domain.TurnUser, "", domain.TokenUsage{})
}

// pricedAssistantTurn builds an assistant turn on the priced test model.
func pricedAssistantTurn(input int64) domain.Event {
	return turnEvent(pricedTurnModel, domain.TokenUsage{Input: domain.Tokens(input)})
}

// orchestratorOnlyInput wraps events in a single orchestrator stream.
func orchestratorOnlyInput(events ...domain.Event) analysis.Input {
	return analysis.Input{
		Streams: []analysis.Stream{{Ref: orchStream(testRunRef), Events: events}},
	}
}

// pricingForTurnModel prices only pricedTurnModel.
func pricingForTurnModel() domain.PricingTable {
	return domain.NewPricingTable([]domain.ModelPricing{{
		Model:                pricedTurnModel,
		Input:                domain.Rate(3_000_000_000),
		CachedInput:          domain.Rate(300_000_000),
		CacheWrite:           domain.Rate(3_750_000_000),
		OutputUnderThreshold: domain.Rate(15_000_000_000),
	}})
}

// priceOrchestrator aggregates, prices, and returns the single run report.
func priceOrchestrator(t *testing.T, in analysis.Input) (domain.RunAggregate, domain.RunReport) {
	t.Helper()
	agg, _ := analysis.Aggregate(in)
	report, _ := analysis.Price(agg, pricingForTurnModel(), domain.Source{})
	if len(report.Runs) != 1 {
		t.Fatalf("got %d priced runs, want 1", len(report.Runs))
	}
	return requireOneRun(t, agg), report.Runs[0]
}

func TestAggregate_OrchestratorUserTurns_AreNotInvocations(t *testing.T) {
	run, _ := priceOrchestrator(t, orchestratorOnlyInput(
		userTurnEvent(),
		pricedAssistantTurn(100),
		userTurnEvent(),
		pricedAssistantTurn(50),
	))

	if got := len(run.Orchestrator.Invocations); got != 2 {
		t.Errorf("orchestrator invocations = %d, want 2 (assistant turns only)", got)
	}
	assertTokenPresent(t, "Orchestrator Input", run.Orchestrator.Totals().Input, 150)
}

func TestAggregate_OrchestratorUserTurns_LeaveCostComplete(t *testing.T) {
	_, report := priceOrchestrator(t, orchestratorOnlyInput(
		userTurnEvent(),
		pricedAssistantTurn(100),
		userTurnEvent(),
		pricedAssistantTurn(50),
	))

	if !report.Orchestrator.Totals.Money.Complete {
		t.Error("orchestrator cost must be complete when every assistant turn is priced")
	}
	if !report.Totals.Money.Complete {
		t.Error("run cost must be complete when every assistant turn is priced")
	}
	if len(report.UnpricedModels) != 0 {
		t.Errorf("unpriced models = %v, want none", report.UnpricedModels)
	}
}

func TestAggregate_OrchestratorUserTurnsOnly_ContributeNoInvocations(t *testing.T) {
	agg, _ := analysis.Aggregate(orchestratorOnlyInput(userTurnEvent(), userTurnEvent()))
	for _, run := range agg.Runs {
		if got := len(run.Orchestrator.Invocations); got != 0 {
			t.Errorf("orchestrator invocations = %d, want 0 for user turns only", got)
		}
	}
}

func TestAggregate_OrchestratorUserTurnCarryingUsage_IsStillExcluded(t *testing.T) {
	// Exclusion is keyed on the role alone, not on a missing model or usage.
	run, _ := priceOrchestrator(t, orchestratorOnlyInput(
		roleTurnEvent(domain.TurnUser, pricedTurnModel, domain.TokenUsage{Input: domain.Tokens(9999)}),
		pricedAssistantTurn(100),
	))

	assertTokenPresent(t, "Orchestrator Input", run.Orchestrator.Totals().Input, 100)
}

func TestAggregate_OrchestratorAssistantTurnWithoutModel_IsCountedAndUnpriced(t *testing.T) {
	run, report := priceOrchestrator(t, orchestratorOnlyInput(
		userTurnEvent(),
		pricedAssistantTurn(100),
		roleTurnEvent(domain.TurnAssistant, "", domain.TokenUsage{Input: domain.Tokens(5)}),
	))

	if got := len(run.Orchestrator.Invocations); got != 2 {
		t.Errorf("orchestrator invocations = %d, want 2 (both assistant turns)", got)
	}
	if report.Orchestrator.Totals.Money.Complete {
		t.Error("an assistant turn without a model must keep orchestrator cost incomplete")
	}
}

func TestAggregate_OrchestratorTurnWithEmptyRole_IsCounted(t *testing.T) {
	run, report := priceOrchestrator(t, orchestratorOnlyInput(
		roleTurnEvent("", pricedTurnModel, domain.TokenUsage{Input: domain.Tokens(70)}),
		userTurnEvent(),
	))

	if got := len(run.Orchestrator.Invocations); got != 1 {
		t.Errorf("orchestrator invocations = %d, want 1 (empty-role turn counted)", got)
	}
	assertTokenPresent(t, "Orchestrator Input", run.Orchestrator.Totals().Input, 70)
	if !report.Orchestrator.Totals.Money.Complete {
		t.Error("priced empty-role turn must yield complete orchestrator cost")
	}
}

func TestAggregate_OrchestratorWithUsageRecords_UserTurnsDoNotAffectTotals(t *testing.T) {
	run, report := priceOrchestrator(t, orchestratorOnlyInput(
		userTurnEvent(),
		pricedAssistantTurn(9999), // legacy sampled; ignored once a usage_record exists
		usageRecordEvent("", "rec-1", pricedTurnModel, domain.TokenUsage{Input: domain.Tokens(200)}),
		userTurnEvent(),
	))

	assertTokenPresent(t, "Orchestrator Input", run.Orchestrator.Totals().Input, 200)
	if !report.Orchestrator.Totals.Money.Complete {
		t.Error("orchestrator cost from priced usage_record must be complete")
	}
}

func TestAggregate_UserTurnOnOrchestratorStream_StillRaisesUnrecognisedHarnessFinding(t *testing.T) {
	ev := userTurnEvent()
	ev.Harness = domain.Harness("totally-unknown-harness")

	_, findings := analysis.Aggregate(orchestratorOnlyInput(ev))

	if !hasFindingKind(findings, domain.FindingUnrecognisedHarness) {
		t.Errorf("expected FindingUnrecognisedHarness for a user turn with unknown harness; got: %v", findings)
	}
}
