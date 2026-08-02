package analysis_test

// Tests for the Price and Analyze composed entry points.
//
// These tests exercise the full pipeline from a domain.Aggregate (or an Input of
// raw events) through pricing into a domain.Report. They verify:
//   - Priced models produce MoneyKnown money cells in the report.
//   - Unpriced models produce token totals plus explicit MoneyUnpriced money cells.
//   - CategoryMoney.Complete=false when any model in the run was unpriced.
//   - AllRuns totals cover named runs only; the unattributable bucket is separate.
//   - Provisional flag from the aggregate is preserved in the RunReport.
//   - The Analyze function merges findings from both phases plus carried findings.
//   - Both Price and Analyze are deterministic: identical inputs produce identical output.

import (
	"testing"

	"mosaic-log-analyzer/internal/analysis"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Price/Analyze helpers (supplement those defined in aggregate_test.go)
// ---------------------------------------------------------------------------

// mustParseRateAnalyze parses a rate string; fatal on error.
func mustParseRateAnalyze(t *testing.T, s string) domain.Rate {
	t.Helper()
	r, err := domain.ParseRate(s)
	if err != nil {
		t.Fatalf("ParseRate(%q): %v", s, err)
	}
	return r
}

// flatModelPricing returns a ModelPricing with no long-context threshold.
// Rates: input="3.00", cachedInput="0.30", cacheWrite="3.75", outputUnder="15.00".
func flatModelPricing(t *testing.T, model domain.ModelID) domain.ModelPricing {
	t.Helper()
	return domain.ModelPricing{
		Model:                model,
		Input:                mustParseRateAnalyze(t, "3.00"),
		CachedInput:          mustParseRateAnalyze(t, "0.30"),
		CacheWrite:           mustParseRateAnalyze(t, "3.75"),
		OutputUnderThreshold: mustParseRateAnalyze(t, "15.00"),
		HasThreshold:         false,
	}
}

// tieredModelPricing returns a ModelPricing with a long-context threshold.
// Rates: input="3.00", cachedInput="0.30", cacheWrite="3.75",
//
//	outputUnder="15.00", outputAt="22.50".
func tieredModelPricing(t *testing.T, model domain.ModelID, threshold int64) domain.ModelPricing {
	t.Helper()
	p := flatModelPricing(t, model)
	p.OutputAtThreshold = mustParseRateAnalyze(t, "22.50")
	p.ContextLengthThreshold = threshold
	p.HasThreshold = true
	return p
}

// singleAgentAggregate builds an Aggregate for one named run with one agent
// and the supplied invocations.
func singleAgentAggregate(
	runRef domain.RunRef,
	agentID domain.AgentInstanceID,
	invocations ...domain.InvocationUsage,
) domain.Aggregate {
	return domain.Aggregate{
		Runs: []domain.RunAggregate{
			{
				Run:          runRef,
				Provisional:  false,
				Orchestrator: domain.ActorAggregate{Actor: domain.Orchestrator()},
				Agents: []domain.ActorAggregate{
					{
						Actor:       domain.AgentInstance(agentID),
						Invocations: invocations,
					},
				},
			},
		},
	}
}

// singleAgentAggregateProvisional builds an Aggregate for one named provisional run.
func singleAgentAggregateProvisional(
	runRef domain.RunRef,
	agentID domain.AgentInstanceID,
	invocations ...domain.InvocationUsage,
) domain.Aggregate {
	agg := singleAgentAggregate(runRef, agentID, invocations...)
	agg.Runs[0].Provisional = true
	return agg
}

// testInvocationUsage constructs a simple InvocationUsage.
func testInvocationUsage(model domain.ModelID, usage domain.TokenUsage) domain.InvocationUsage {
	return domain.InvocationUsage{
		Model:   model,
		Harness: domain.HarnessClaudeCode,
		Usage:   usage,
	}
}

// testSource returns a minimal Source for use in Price/Analyze calls.
func testSource() domain.Source {
	return domain.Source{Kind: domain.SourceLogsRoot, Path: "/test-logs"}
}

// assertMoneyKnown asserts that a MoneyValue is Known with the given nanos.
func assertMoneyKnown(t *testing.T, name string, mv domain.MoneyValue, wantNanos int64) {
	t.Helper()
	if mv.State != domain.MoneyKnown {
		t.Errorf("%s: state = %v, want MoneyKnown", name, mv.State)
		return
	}
	if got := mv.Amount.Nanos(); got != wantNanos {
		t.Errorf("%s: amount = %d nanos, want %d nanos", name, got, wantNanos)
	}
}

// assertMoneyState asserts a MoneyValue has the expected state.
func assertMoneyState(t *testing.T, name string, mv domain.MoneyValue, want domain.MoneyState) {
	t.Helper()
	if mv.State != want {
		t.Errorf("%s: state = %v, want %v", name, mv.State, want)
	}
}

// ---------------------------------------------------------------------------
// T4.6 — Price: priced models produce Known money
// ---------------------------------------------------------------------------

func TestPrice_PricedModel_ReturnsKnownMoney(t *testing.T) {
	// A model present in the pricing table must produce MoneyKnown cells in the report.
	// 1_000_000 input tokens at "3.00"/1M = 3_000_000_000 nanos.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{flatModelPricing(t, model)})

	agg := singleAgentAggregate(testRunRef,
		"Agent#1",
		testInvocationUsage(model, domain.TokenUsage{
			Input: domain.Tokens(1_000_000),
		}),
	)

	report, _ := analysis.Price(agg, table, testSource())

	if len(report.Runs) == 0 {
		t.Fatal("expected at least one run in the report")
	}
	run := report.Runs[0]
	if len(run.Agents) == 0 {
		t.Fatal("expected at least one agent in the run report")
	}
	agent := run.Agents[0]

	// 1M input tokens at "3.00"/1M = 3_000_000_000 nanos.
	assertMoneyKnown(t, "agent Input money", agent.Totals.Money.Input, 3_000_000_000)
}

func TestPrice_PricedModel_OutputUnderThreshold(t *testing.T) {
	// Context < threshold → output priced at OutputUnderThreshold.
	// 1_000_000 output tokens at "15.00"/1M = 15_000_000_000 nanos.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{tieredModelPricing(t, model, 200_000)})

	agg := singleAgentAggregate(testRunRef,
		"Agent#1",
		testInvocationUsage(model, domain.TokenUsage{
			Input:  domain.Tokens(100_000), // context < threshold
			Output: domain.Tokens(1_000_000),
		}),
	)

	report, _ := analysis.Price(agg, table, testSource())

	if len(report.Runs) == 0 || len(report.Runs[0].Agents) == 0 {
		t.Fatal("expected a run and an agent in the report")
	}
	agent := report.Runs[0].Agents[0]

	assertMoneyKnown(t, "agent Output money (under threshold)", agent.Totals.Money.Output, 15_000_000_000)
}

func TestPrice_PricedModel_OutputAtThreshold(t *testing.T) {
	// Context >= threshold → output priced at OutputAtThreshold.
	// 1_000_000 output tokens at "22.50"/1M = 22_500_000_000 nanos.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{tieredModelPricing(t, model, 200_000)})

	agg := singleAgentAggregate(testRunRef,
		"Agent#1",
		testInvocationUsage(model, domain.TokenUsage{
			Input:  domain.Tokens(200_000), // context == threshold → TierLongContext
			Output: domain.Tokens(1_000_000),
		}),
	)

	report, _ := analysis.Price(agg, table, testSource())

	if len(report.Runs) == 0 || len(report.Runs[0].Agents) == 0 {
		t.Fatal("expected a run and an agent in the report")
	}
	agent := report.Runs[0].Agents[0]

	assertMoneyKnown(t, "agent Output money (at threshold)", agent.Totals.Money.Output, 22_500_000_000)
}

// ---------------------------------------------------------------------------
// T4.6 — Price: unpriced models
// ---------------------------------------------------------------------------

func TestPrice_UnpricedModel_MoneyIsUnpriced(t *testing.T) {
	// A model absent from the pricing table must yield MoneyUnpriced money cells
	// in the agent report — never MoneyKnown(0) — per AC4.5.
	table := domain.EmptyPricingTable()

	agg := singleAgentAggregate(testRunRef,
		"Agent#1",
		testInvocationUsage("unknown-model", domain.TokenUsage{
			Input:  domain.Tokens(1_000_000),
			Output: domain.Tokens(1_000_000),
		}),
	)

	report, _ := analysis.Price(agg, table, testSource())

	if len(report.Runs) == 0 || len(report.Runs[0].Agents) == 0 {
		t.Fatal("expected a run and an agent in the report")
	}
	agent := report.Runs[0].Agents[0]

	assertMoneyState(t, "agent Input money (unpriced model)", agent.Totals.Money.Input, domain.MoneyUnpriced)
	assertMoneyState(t, "agent Output money (unpriced model)", agent.Totals.Money.Output, domain.MoneyUnpriced)
}

func TestPrice_UnpricedModel_TokensPresent(t *testing.T) {
	// Token totals must be present in the report even when the model is unpriced.
	table := domain.EmptyPricingTable()

	agg := singleAgentAggregate(testRunRef,
		"Agent#1",
		testInvocationUsage("unknown-model", domain.TokenUsage{
			Input:  domain.Tokens(500_000),
			Output: domain.Tokens(250_000),
		}),
	)

	report, _ := analysis.Price(agg, table, testSource())

	if len(report.Runs) == 0 || len(report.Runs[0].Agents) == 0 {
		t.Fatal("expected a run and an agent in the report")
	}
	agent := report.Runs[0].Agents[0]

	if v, ok := agent.Totals.Tokens.Input.Value(); !ok || v != 500_000 {
		t.Errorf("agent.Totals.Tokens.Input = (%d, %v), want (500_000, true)", v, ok)
	}
	if v, ok := agent.Totals.Tokens.Output.Value(); !ok || v != 250_000 {
		t.Errorf("agent.Totals.Tokens.Output = (%d, %v), want (250_000, true)", v, ok)
	}
}

func TestPrice_UnpricedModel_AppearsInUnpricedModelsList(t *testing.T) {
	// An unpriced model must appear in RunReport.UnpricedModels and Report.UnpricedModels.
	table := domain.EmptyPricingTable()

	agg := singleAgentAggregate(testRunRef,
		"Agent#1",
		testInvocationUsage("unknown-model", domain.TokenUsage{Input: domain.Tokens(100)}),
	)

	report, _ := analysis.Price(agg, table, testSource())

	if !report.HasUnpricedModels() {
		t.Error("report.HasUnpricedModels() must be true when at least one model lacks pricing")
	}

	found := false
	for _, m := range report.UnpricedModels {
		if m == "unknown-model" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("'unknown-model' must appear in report.UnpricedModels; got: %v", report.UnpricedModels)
	}
}

func TestPrice_UnpricedModel_CompleteFalse(t *testing.T) {
	// When a run contains an unpriced model, CategoryMoney.Complete must be false —
	// signalling that the money total is partial (AC4.5, per CategoryMoney contract).
	table := domain.EmptyPricingTable()

	agg := singleAgentAggregate(testRunRef,
		"Agent#1",
		testInvocationUsage("unknown-model", domain.TokenUsage{Input: domain.Tokens(1_000_000)}),
	)

	report, _ := analysis.Price(agg, table, testSource())

	if len(report.Runs) == 0 {
		t.Fatal("expected at least one run in the report")
	}
	if report.Runs[0].Totals.Money.Complete {
		t.Error("Totals.Money.Complete must be false when the run contains an unpriced model")
	}
}

// ---------------------------------------------------------------------------
// T4.6 — Price: AllRuns excludes the unattributable bucket
// ---------------------------------------------------------------------------

func TestPrice_AllRunsExcludesUnattributable(t *testing.T) {
	// Report.AllRuns must total named runs only. The unattributable bucket's
	// tokens and money must be excluded from AllRuns and held in Report.Unattributable.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{flatModelPricing(t, model)})

	namedInput := int64(1_000_000)
	unattribInput := int64(999_999)

	agg := domain.Aggregate{
		Runs: []domain.RunAggregate{
			{
				Run:          testRunRef,
				Orchestrator: domain.ActorAggregate{Actor: domain.Orchestrator()},
				Agents: []domain.ActorAggregate{
					{
						Actor: domain.AgentInstance("Agent#1"),
						Invocations: []domain.InvocationUsage{
							testInvocationUsage(model, domain.TokenUsage{Input: domain.Tokens(namedInput)}),
						},
					},
				},
			},
		},
		Unattributable: &domain.RunAggregate{
			Run:          domain.UnattributableRun(),
			Orchestrator: domain.ActorAggregate{Actor: domain.Orchestrator()},
			Agents: []domain.ActorAggregate{
				{
					Actor: domain.AgentInstance("Unattrib#1"),
					Invocations: []domain.InvocationUsage{
						testInvocationUsage(model, domain.TokenUsage{Input: domain.Tokens(unattribInput)}),
					},
				},
			},
		},
	}

	report, _ := analysis.Price(agg, table, testSource())

	// AllRuns must reflect only the named run (namedInput tokens).
	if v, ok := report.AllRuns.Tokens.Input.Value(); !ok || v != namedInput {
		t.Errorf("AllRuns.Tokens.Input = (%d, %v), want (%d, true) — unattributable must not be included",
			v, ok, namedInput)
	}

	// The unattributable bucket must be present in its own field.
	if report.Unattributable == nil {
		t.Error("Report.Unattributable must be non-nil when the aggregate has an unattributable bucket")
	}
}

// ---------------------------------------------------------------------------
// T4.6 — Price: provisional flag preserved
// ---------------------------------------------------------------------------

func TestPrice_ProvisionalRun_FlagPreservedInReport(t *testing.T) {
	// A provisional run in the aggregate must produce a RunReport with Provisional=true.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{flatModelPricing(t, model)})

	agg := singleAgentAggregateProvisional(testRunRef,
		"Agent#1",
		testInvocationUsage(model, domain.TokenUsage{Input: domain.Tokens(100)}),
	)

	report, _ := analysis.Price(agg, table, testSource())

	if len(report.Runs) == 0 {
		t.Fatal("expected at least one run in the report")
	}
	if !report.Runs[0].Provisional {
		t.Error("RunReport.Provisional must be true when the RunAggregate was provisional")
	}
}

func TestPrice_NonProvisionalRun_FlagFalseInReport(t *testing.T) {
	// A non-provisional run must produce RunReport.Provisional=false.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{flatModelPricing(t, model)})

	agg := singleAgentAggregate(testRunRef, // Provisional=false
		"Agent#1",
		testInvocationUsage(model, domain.TokenUsage{Input: domain.Tokens(100)}),
	)

	report, _ := analysis.Price(agg, table, testSource())

	if len(report.Runs) == 0 {
		t.Fatal("expected at least one run in the report")
	}
	if report.Runs[0].Provisional {
		t.Error("RunReport.Provisional must be false when the RunAggregate was not provisional")
	}
}

// ---------------------------------------------------------------------------
// T4.6 — Price: Currency field
// ---------------------------------------------------------------------------

func TestPrice_CurrencyField_IsUSD(t *testing.T) {
	// The Report.Currency field must always be domain.Currency ("USD").
	report, _ := analysis.Price(domain.Aggregate{}, domain.EmptyPricingTable(), testSource())

	if report.Currency != domain.Currency {
		t.Errorf("Report.Currency = %q, want %q", report.Currency, domain.Currency)
	}
}

// ---------------------------------------------------------------------------
// T4.6 — Price: determinism
// ---------------------------------------------------------------------------

func TestPrice_Deterministic(t *testing.T) {
	// Calling Price twice with identical inputs must produce identical outputs.
	// This is the core of AC4.7.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{flatModelPricing(t, model)})

	agg := singleAgentAggregate(testRunRef,
		"Agent#1",
		testInvocationUsage(model, domain.TokenUsage{
			Input:  domain.Tokens(1_000_000),
			Output: domain.Tokens(500_000),
		}),
	)

	report1, _ := analysis.Price(agg, table, testSource())
	report2, _ := analysis.Price(agg, table, testSource())

	// Compare a selection of fields that would differ if non-determinism existed.
	if len(report1.Runs) != len(report2.Runs) {
		t.Fatalf("run count differs: %d vs %d", len(report1.Runs), len(report2.Runs))
	}
	if len(report1.Runs) == 0 {
		t.Skip("no runs to compare (Price stub returns empty; RED phase expected)")
	}

	money1 := report1.Runs[0].Totals.Money.Input
	money2 := report2.Runs[0].Totals.Money.Input

	if money1.State != money2.State {
		t.Errorf("money state differs across calls: %v vs %v", money1.State, money2.State)
	}
	if money1.State == domain.MoneyKnown && money1.Amount != money2.Amount {
		t.Errorf("money amount differs across calls: %v vs %v", money1.Amount, money2.Amount)
	}

	if len(report1.UnpricedModels) != len(report2.UnpricedModels) {
		t.Errorf("UnpricedModels length differs: %d vs %d",
			len(report1.UnpricedModels), len(report2.UnpricedModels))
	}
}

// ---------------------------------------------------------------------------
// T4.6 — Analyze: composed entry point
// ---------------------------------------------------------------------------

func TestAnalyze_ProducesNonEmptyReport(t *testing.T) {
	// Analyze must produce a non-empty report when given events that can be
	// attributed. This is the most basic correctness check for the composed function.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{flatModelPricing(t, model)})

	agentID := domain.AgentInstanceID("TestAgent#1")
	in := analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: agentStream(testRunRef, agentID),
				Events: []domain.Event{
					invEndEvent(agentID, model, domain.TokenUsage{
						Input:  domain.Tokens(1_000_000),
						Output: domain.Tokens(500_000),
					}),
					runEndEvent(),
				},
			},
		},
	}

	report := analysis.Analyze(in, table, testSource(), nil)

	if report.IsEmpty() {
		t.Error("Analyze must produce a non-empty report when events contain attributable usage")
	}
	if len(report.Runs) == 0 {
		t.Error("Analyze must produce at least one named run in the report")
	}
}

func TestAnalyze_PricedRun_MoneyKnown(t *testing.T) {
	// Analyze with a priced model must produce MoneyKnown cells in the report.
	// 1_000_000 input tokens at "3.00"/1M = 3_000_000_000 nanos.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{flatModelPricing(t, model)})

	agentID := domain.AgentInstanceID("Agent#1")
	in := oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, model, domain.TokenUsage{Input: domain.Tokens(1_000_000)}),
		runEndEvent(),
	)

	report := analysis.Analyze(in, table, testSource(), nil)

	if len(report.Runs) == 0 {
		t.Fatal("Analyze produced no runs in the report")
	}
	run := report.Runs[0]
	if len(run.Agents) == 0 {
		t.Fatal("Analyze produced no agents in the run report")
	}
	agent := run.Agents[0]

	assertMoneyKnown(t, "agent Input money (Analyze)", agent.Totals.Money.Input, 3_000_000_000)
}

func TestAnalyze_CarriedFindingsMergedIntoReport(t *testing.T) {
	// Carried-in findings (pre-existing from a prior stage, e.g. log scanning)
	// must appear in the report's QualitySummary alongside findings raised during
	// aggregation and pricing.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{flatModelPricing(t, model)})

	carriedFinding := domain.Finding{
		Kind:     domain.FindingUnreadableFile,
		Severity: domain.SeverityWarning,
		Path:     "/some/file.jsonl",
		Run:      testRunRef,
	}

	agentID := domain.AgentInstanceID("Agent#1")
	in := oneAgentStream(testRunRef, agentID,
		invEndEvent(agentID, model, domain.TokenUsage{Input: domain.Tokens(100)}),
	)

	report := analysis.Analyze(in, table, testSource(), []domain.Finding{carriedFinding})

	found := hasFindingKind(report.Quality.Findings, domain.FindingUnreadableFile)
	if !found {
		t.Errorf("carried FindingUnreadableFile must appear in report.Quality.Findings; got: %v",
			report.Quality.Findings)
	}
}

func TestAnalyze_AggregationFindingsMergedIntoReport(t *testing.T) {
	// Findings raised during aggregation (e.g. missing token usage) must be
	// present in the report's QualitySummary.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{flatModelPricing(t, model)})

	agentID := domain.AgentInstanceID("Agent#1")
	in := oneAgentStream(testRunRef, agentID,
		invEndEventNoUsage(agentID, model), // raises FindingMissingTokenUsage
	)

	report := analysis.Analyze(in, table, testSource(), nil)

	if !hasFindingKind(report.Quality.Findings, domain.FindingMissingTokenUsage) {
		t.Errorf("FindingMissingTokenUsage from aggregation must appear in report.Quality.Findings; got: %v",
			report.Quality.Findings)
	}
}

func TestAnalyze_Deterministic(t *testing.T) {
	// Calling Analyze twice with identical inputs must produce identical reports.
	// This is the determinism contract required by AC4.7.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{flatModelPricing(t, model)})

	agentID := domain.AgentInstanceID("Agent#1")

	buildInput := func() analysis.Input {
		return oneAgentStream(testRunRef, agentID,
			invEndEvent(agentID, model, domain.TokenUsage{
				Input:  domain.Tokens(1_000_000),
				Output: domain.Tokens(500_000),
			}),
			runEndEvent(),
		)
	}

	report1 := analysis.Analyze(buildInput(), table, testSource(), nil)
	report2 := analysis.Analyze(buildInput(), table, testSource(), nil)

	if len(report1.Runs) != len(report2.Runs) {
		t.Fatalf("run count differs across Analyze calls: %d vs %d",
			len(report1.Runs), len(report2.Runs))
	}

	if len(report1.Runs) == 0 {
		t.Skip("no runs to compare (Analyze stub returns empty; RED phase expected)")
	}

	money1 := report1.Runs[0].Totals.Money.Input
	money2 := report2.Runs[0].Totals.Money.Input

	if money1.State != money2.State {
		t.Errorf("money state differs across Analyze calls: %v vs %v", money1.State, money2.State)
	}
	if money1.State == domain.MoneyKnown && money1.Amount != money2.Amount {
		t.Errorf("money amount differs across Analyze calls: %v vs %v", money1.Amount, money2.Amount)
	}

	if len(report1.UnpricedModels) != len(report2.UnpricedModels) {
		t.Errorf("UnpricedModels length differs: %d vs %d",
			len(report1.UnpricedModels), len(report2.UnpricedModels))
	}
}

func TestAnalyze_UnattributableExcludedFromAllRuns(t *testing.T) {
	// When Analyze sees events attributed to the unattributable bucket, those must
	// land in Report.Unattributable, not AllRuns.
	model := domain.ModelID("test-model")
	table := domain.NewPricingTable([]domain.ModelPricing{flatModelPricing(t, model)})

	namedAgent := domain.AgentInstanceID("Named#1")
	unattribAgent := domain.AgentInstanceID("Unattrib#1")

	namedRef := testRunRef
	unattribRef := domain.UnattributableRun()

	in := analysis.Input{
		Streams: []analysis.Stream{
			{
				Ref: agentStream(namedRef, namedAgent),
				Events: []domain.Event{
					invEndEvent(namedAgent, model, domain.TokenUsage{Input: domain.Tokens(1_000_000)}),
				},
			},
			{
				Ref: agentStream(unattribRef, unattribAgent),
				Events: []domain.Event{
					invEndEvent(unattribAgent, model, domain.TokenUsage{Input: domain.Tokens(999_999)}),
				},
			},
		},
	}

	report := analysis.Analyze(in, table, testSource(), nil)

	// AllRuns must reflect only the named run (1,000,000 tokens).
	// If the unattributable bucket (999,999 tokens) were incorrectly included, the total would be 1,999,999.
	// Use v > 1_000_000 so the check does not fire for the correct value 1,000,000.
	if v, ok := report.AllRuns.Tokens.Input.Value(); ok && v > 1_000_000 {
		t.Errorf("AllRuns.Tokens.Input = %d, must not include the unattributable bucket's 999_999 tokens", v)
	}

	if report.Unattributable == nil {
		t.Error("Report.Unattributable must be non-nil when the input contains unattributable events")
	}
}
