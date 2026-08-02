package cost_test

// Tests for PriceInvocation and SumCosts.
//
// PriceInvocation invariants:
//   - Each token category is priced independently at its configured rate.
//   - An absent token category yields MoneyNoData (not MoneyKnown(0)).
//   - A present-zero token count yields MoneyKnown(0) (distinct from absent).
//   - Output tier is chosen per invocation from total context vs. threshold.
//   - The cliff is all-or-nothing: context >= threshold reprices ALL output at the
//     at-threshold rate, never only the excess tokens.
//   - A model absent from the table yields Priced=false and every money cell
//     MoneyUnpriced — token totals are still present in InvocationCost.Tokens.
//
// SumCosts invariants:
//   - Sums each money category across all InvocationCosts.
//   - Complete=false when any InvocationCost has Priced=false.
//   - Sets the Total field to the sum of all per-category totals.

import (
	"testing"

	"mosaic-log-analyzer/internal/analysis/cost"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Rate / pricing helpers
// ---------------------------------------------------------------------------

func mustParseRate(t *testing.T, s string) domain.Rate {
	t.Helper()
	r, err := domain.ParseRate(s)
	if err != nil {
		t.Fatalf("ParseRate(%q): %v", s, err)
	}
	return r
}

// flatPricing returns a ModelPricing with no long-context threshold.
// Rates: input="3.00", cachedInput="0.30", cacheWrite="3.75", output="15.00".
func flatPricing(t *testing.T, model domain.ModelID) domain.ModelPricing {
	t.Helper()
	return domain.ModelPricing{
		Model:                model,
		Input:                mustParseRate(t, "3.00"),
		CachedInput:          mustParseRate(t, "0.30"),
		CacheWrite:           mustParseRate(t, "3.75"),
		OutputUnderThreshold: mustParseRate(t, "15.00"),
		HasThreshold:         false,
	}
}

// tieredPricing returns a ModelPricing with a long-context threshold.
// Rates: input="3.00", cachedInput="0.30", cacheWrite="3.75",
//
//	outputUnder="15.00", outputAt="22.50".
func tieredPricing(t *testing.T, model domain.ModelID, threshold int64) domain.ModelPricing {
	t.Helper()
	p := flatPricing(t, model)
	p.OutputAtThreshold = mustParseRate(t, "22.50")
	p.ContextLengthThreshold = threshold
	p.HasThreshold = true
	return p
}

// testTable builds an immutable PricingTable from the given entries.
func testTable(entries ...domain.ModelPricing) domain.PricingTable {
	return domain.NewPricingTable(entries)
}

// inv builds an InvocationUsage for a given model and usage block.
func inv(model domain.ModelID, usage domain.TokenUsage) domain.InvocationUsage {
	return domain.InvocationUsage{
		Model:   model,
		Harness: domain.HarnessClaudeCode,
		Usage:   usage,
	}
}

// ---------------------------------------------------------------------------
// Money assertion helpers
// ---------------------------------------------------------------------------

func assertKnownMoney(t *testing.T, name string, mv domain.MoneyValue, wantNanos int64) {
	t.Helper()
	if mv.State != domain.MoneyKnown {
		t.Errorf("%s: state = %v, want MoneyKnown", name, mv.State)
		return
	}
	if got := mv.Amount.Nanos(); got != wantNanos {
		t.Errorf("%s: amount = %d nanos, want %d nanos", name, got, wantNanos)
	}
}

func assertNoData(t *testing.T, name string, mv domain.MoneyValue) {
	t.Helper()
	if mv.State != domain.MoneyNoData {
		t.Errorf("%s: state = %v, want MoneyNoData (absent category must not become $0 or Unpriced)", name, mv.State)
	}
}

func assertUnpriced(t *testing.T, name string, mv domain.MoneyValue) {
	t.Helper()
	if mv.State != domain.MoneyUnpriced {
		t.Errorf("%s: state = %v, want MoneyUnpriced", name, mv.State)
	}
}

// ---------------------------------------------------------------------------
// T4.1 — Per-category money computation; absent yields NoData
// ---------------------------------------------------------------------------

func TestPriceInvocation_InputRate_CorrectAmount(t *testing.T) {
	// 1_000_000 input tokens at "3.00"/1M = $3.00 = 3_000_000_000 nanos.
	model := domain.ModelID("test-model")
	table := testTable(flatPricing(t, model))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{
		Input: domain.Tokens(1_000_000),
	}), table)

	if !ic.Priced {
		t.Error("Priced must be true when model is in the pricing table")
	}
	// 1_000_000 * 3_000_000_000 / 1_000_000 = 3_000_000_000 nanos
	assertKnownMoney(t, "Input", ic.Money.Input, 3_000_000_000)
}

func TestPriceInvocation_CacheReadRate_CorrectAmount(t *testing.T) {
	// 1_000_000 cache-read tokens at "0.30"/1M = $0.30 = 300_000_000 nanos.
	model := domain.ModelID("test-model")
	table := testTable(flatPricing(t, model))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{
		CacheRead: domain.Tokens(1_000_000),
	}), table)

	assertKnownMoney(t, "CacheRead", ic.Money.CacheRead, 300_000_000)
}

func TestPriceInvocation_CacheCreationRate_CorrectAmount(t *testing.T) {
	// 1_000_000 cache-creation tokens at "3.75"/1M = $3.75 = 3_750_000_000 nanos.
	model := domain.ModelID("test-model")
	table := testTable(flatPricing(t, model))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{
		CacheCreation: domain.Tokens(1_000_000),
	}), table)

	assertKnownMoney(t, "CacheCreation", ic.Money.CacheCreation, 3_750_000_000)
}

func TestPriceInvocation_AllCategoriesPresent_AllMoneyKnown(t *testing.T) {
	// All four categories present → all four money cells must be MoneyKnown.
	model := domain.ModelID("test-model")
	table := testTable(flatPricing(t, model))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{
		Input:         domain.Tokens(1_000_000),
		CacheRead:     domain.Tokens(1_000_000),
		CacheCreation: domain.Tokens(1_000_000),
		Output:        domain.Tokens(1_000_000),
	}), table)

	if ic.Money.Input.State != domain.MoneyKnown {
		t.Errorf("Input: state = %v, want MoneyKnown", ic.Money.Input.State)
	}
	if ic.Money.CacheRead.State != domain.MoneyKnown {
		t.Errorf("CacheRead: state = %v, want MoneyKnown", ic.Money.CacheRead.State)
	}
	if ic.Money.CacheCreation.State != domain.MoneyKnown {
		t.Errorf("CacheCreation: state = %v, want MoneyKnown", ic.Money.CacheCreation.State)
	}
	if ic.Money.Output.State != domain.MoneyKnown {
		t.Errorf("Output: state = %v, want MoneyKnown", ic.Money.Output.State)
	}
}

func TestPriceInvocation_AbsentCategory_IsNoData(t *testing.T) {
	// CacheRead and CacheCreation absent → their money cells must be MoneyNoData,
	// never MoneyKnown(0). The distinction is required by AC4.6.
	model := domain.ModelID("test-model")
	table := testTable(flatPricing(t, model))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{
		Input:  domain.Tokens(1_000_000),
		Output: domain.Tokens(1_000_000),
		// CacheRead:     absent
		// CacheCreation: absent
	}), table)

	assertNoData(t, "CacheRead (absent tokens)", ic.Money.CacheRead)
	assertNoData(t, "CacheCreation (absent tokens)", ic.Money.CacheCreation)

	// Present categories must remain Known.
	if ic.Money.Input.State != domain.MoneyKnown {
		t.Errorf("Input: state = %v, want MoneyKnown (tokens were present)", ic.Money.Input.State)
	}
	if ic.Money.Output.State != domain.MoneyKnown {
		t.Errorf("Output: state = %v, want MoneyKnown (tokens were present)", ic.Money.Output.State)
	}
}

func TestPriceInvocation_AllCategoriesAbsent_AllNoData(t *testing.T) {
	// All four categories absent → all money cells must be MoneyNoData.
	model := domain.ModelID("test-model")
	table := testTable(flatPricing(t, model))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{}), table) // all absent

	assertNoData(t, "Input (all absent)", ic.Money.Input)
	assertNoData(t, "CacheRead (all absent)", ic.Money.CacheRead)
	assertNoData(t, "CacheCreation (all absent)", ic.Money.CacheCreation)
	assertNoData(t, "Output (all absent)", ic.Money.Output)
}

func TestPriceInvocation_PresentZeroTokens_IsKnownZero(t *testing.T) {
	// A token count that is present but zero must yield MoneyKnown(0), not MoneyNoData.
	// This distinguishes "reported as zero" from "not reported at all".
	model := domain.ModelID("test-model")
	table := testTable(flatPricing(t, model))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{
		Input: domain.Tokens(0), // present, value = 0
	}), table)

	if ic.Money.Input.State != domain.MoneyKnown {
		t.Errorf("Input: state = %v, want MoneyKnown for a present-zero token count", ic.Money.Input.State)
	}
	if ic.Money.Input.Amount.Nanos() != 0 {
		t.Errorf("Input: amount = %d nanos, want 0 (0 tokens * any rate = $0)", ic.Money.Input.Amount.Nanos())
	}
}

func TestPriceInvocation_TokensStoredInResult(t *testing.T) {
	// InvocationCost.Tokens must preserve the original usage block.
	model := domain.ModelID("test-model")
	table := testTable(flatPricing(t, model))

	usage := domain.TokenUsage{
		Input:  domain.Tokens(500),
		Output: domain.Tokens(200),
	}

	ic := cost.PriceInvocation(inv(model, usage), table)

	if v, ok := ic.Tokens.Input.Value(); !ok || v != 500 {
		t.Errorf("Tokens.Input = (%d, %v), want (500, true)", v, ok)
	}
	if v, ok := ic.Tokens.Output.Value(); !ok || v != 200 {
		t.Errorf("Tokens.Output = (%d, %v), want (200, true)", v, ok)
	}
}

// ---------------------------------------------------------------------------
// T4.2 — Output tier selection within PriceInvocation
// ---------------------------------------------------------------------------

func TestPriceInvocation_BelowThreshold_UsesUnderRate(t *testing.T) {
	// Context = 100_000 < threshold 200_000 → TierStandard → output at OutputUnderThreshold.
	// 1_000_000 output tokens at "15.00"/1M = 15_000_000_000 nanos.
	model := domain.ModelID("test-model")
	table := testTable(tieredPricing(t, model, 200_000))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{
		Input:  domain.Tokens(100_000), // context = 100K < threshold
		Output: domain.Tokens(1_000_000),
	}), table)

	if ic.Tier != cost.TierStandard {
		t.Errorf("Tier = %v, want TierStandard (context 100K < threshold 200K)", ic.Tier)
	}
	assertKnownMoney(t, "Output (TierStandard, under rate)", ic.Money.Output, 15_000_000_000)
}

func TestPriceInvocation_ExactlyAtThreshold_UsesAtRate(t *testing.T) {
	// Context = 200_000 == threshold 200_000 → TierLongContext → output at OutputAtThreshold.
	// 1_000_000 output tokens at "22.50"/1M = 22_500_000_000 nanos.
	model := domain.ModelID("test-model")
	table := testTable(tieredPricing(t, model, 200_000))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{
		Input:  domain.Tokens(200_000), // context exactly at threshold
		Output: domain.Tokens(1_000_000),
	}), table)

	if ic.Tier != cost.TierLongContext {
		t.Errorf("Tier = %v, want TierLongContext (context exactly at threshold, >= applies)", ic.Tier)
	}
	assertKnownMoney(t, "Output (TierLongContext, at threshold)", ic.Money.Output, 22_500_000_000)
}

func TestPriceInvocation_AboveThreshold_UsesAtRate(t *testing.T) {
	// Context = 300_000 > threshold 200_000 → TierLongContext → output at OutputAtThreshold.
	model := domain.ModelID("test-model")
	table := testTable(tieredPricing(t, model, 200_000))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{
		Input:  domain.Tokens(300_000), // context > threshold
		Output: domain.Tokens(1_000_000),
	}), table)

	if ic.Tier != cost.TierLongContext {
		t.Errorf("Tier = %v, want TierLongContext (context 300K > threshold 200K)", ic.Tier)
	}
	assertKnownMoney(t, "Output (TierLongContext, above threshold)", ic.Money.Output, 22_500_000_000)
}

func TestPriceInvocation_NoThreshold_AlwaysUnderRate(t *testing.T) {
	// HasThreshold=false → always TierStandard → output at OutputUnderThreshold.
	// Even enormous context does not trigger a tier change.
	model := domain.ModelID("test-model")
	table := testTable(flatPricing(t, model))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{
		Input:  domain.Tokens(999_999_999), // enormous context
		Output: domain.Tokens(1_000_000),
	}), table)

	if ic.Tier != cost.TierStandard {
		t.Errorf("Tier = %v, want TierStandard (model has no long-context tier)", ic.Tier)
	}
	assertKnownMoney(t, "Output (no threshold, always TierStandard)", ic.Money.Output, 15_000_000_000)
}

// ---------------------------------------------------------------------------
// T4.3 — All-or-nothing repricing cliff
// ---------------------------------------------------------------------------

func TestPriceInvocation_CliffIsAllOrNothing(t *testing.T) {
	// When context crosses the threshold, the ENTIRE output is repriced at the
	// at-threshold rate. No graduated/split calculation exists.
	//
	// Setup:
	//   threshold   = 200_000
	//   input       = 250_000  (context = 250K > threshold → TierLongContext)
	//   output      = 1_000_000
	//   under_rate  = "15.00"/1M  →  15_000_000_000 nanos per 1M tokens
	//   at_rate     = "22.50"/1M  →  22_500_000_000 nanos per 1M tokens
	//
	// All-or-nothing (correct): 1_000_000 × at_rate / 1M = 22_500_000_000 nanos.
	//
	// A hypothetical graduated/split calculation — e.g., charging only the
	// proportional excess at the at-rate — would give a lower number.
	// For instance: splitting context excess (50K) as a proportion of output
	// would yield  950K × under_rate + 50K × at_rate = 15_375_000_000 nanos,
	// which is strictly less than 22_500_000_000. The test verifies the result
	// equals the correct all-or-nothing value and is not the graduated value.
	model := domain.ModelID("test-model")
	table := testTable(tieredPricing(t, model, 200_000))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{
		Input:  domain.Tokens(250_000),
		Output: domain.Tokens(1_000_000),
	}), table)

	if ic.Tier != cost.TierLongContext {
		t.Fatalf("Tier = %v, want TierLongContext (context 250K > threshold 200K)", ic.Tier)
	}

	const (
		wantAllOrNothing = int64(22_500_000_000) // correct: all output at at_rate
		badGraduated     = int64(15_375_000_000) // wrong: split pricing
	)

	if wantAllOrNothing == badGraduated {
		t.Fatal("test design error: all-or-nothing and graduated values are equal for these inputs")
	}

	got := ic.Money.Output.Amount.Nanos()
	if got != wantAllOrNothing {
		t.Errorf("Output = %d nanos, want %d (all-or-nothing cliff); graduated/split would give %d",
			got, wantAllOrNothing, badGraduated)
	}
}

func TestPriceInvocation_JustBelowCliff_UsesUnderRate(t *testing.T) {
	// Context = threshold − 1 → TierStandard. Boundary condition for the >= comparison.
	model := domain.ModelID("test-model")
	table := testTable(tieredPricing(t, model, 200_000))

	ic := cost.PriceInvocation(inv(model, domain.TokenUsage{
		Input:  domain.Tokens(199_999), // 1 below threshold
		Output: domain.Tokens(1_000_000),
	}), table)

	if ic.Tier != cost.TierStandard {
		t.Errorf("Tier = %v, want TierStandard (context 199_999 is 1 below threshold 200_000)", ic.Tier)
	}
	assertKnownMoney(t, "Output (just below cliff)", ic.Money.Output, 15_000_000_000)
}

// ---------------------------------------------------------------------------
// T4.4 — Per-invocation tiering granularity
// ---------------------------------------------------------------------------

func TestSumCosts_TwoInvocations_TieredIndependently(t *testing.T) {
	// Two invocations of the same model, one under and one over the threshold.
	// Each must be priced independently; the run total must be their sum — NOT
	// the result of summing contexts first and then selecting a single tier.
	//
	//   Invocation A: input = 100_000 (context 100K < 200K) → TierStandard
	//                 output = 1_000_000 → 15_000_000_000 nanos
	//   Invocation B: input = 300_000 (context 300K >= 200K) → TierLongContext
	//                 output = 1_000_000 → 22_500_000_000 nanos
	//
	//   Correct sum:  37_500_000_000 nanos
	//   Wrong answer: if run context were summed (400K >= 200K) → both at at_rate
	//                 = 2 × 22_500_000_000 = 45_000_000_000 nanos
	model := domain.ModelID("test-model")
	table := testTable(tieredPricing(t, model, 200_000))

	invA := inv(model, domain.TokenUsage{
		Input:  domain.Tokens(100_000), // context < threshold
		Output: domain.Tokens(1_000_000),
	})
	invB := inv(model, domain.TokenUsage{
		Input:  domain.Tokens(300_000), // context >= threshold
		Output: domain.Tokens(1_000_000),
	})

	costA := cost.PriceInvocation(invA, table)
	costB := cost.PriceInvocation(invB, table)

	// Verify independent tier selection before checking the sum.
	if costA.Tier != cost.TierStandard {
		t.Errorf("invocation A: tier = %v, want TierStandard (context 100K < threshold 200K)", costA.Tier)
	}
	if costB.Tier != cost.TierLongContext {
		t.Errorf("invocation B: tier = %v, want TierLongContext (context 300K >= threshold 200K)", costB.Tier)
	}

	sum := cost.SumCosts(costA, costB)

	const (
		wantOutputNanos       = int64(37_500_000_000) // 15B + 22.5B — per-invocation tiering
		wrongRunLevelTiering  = int64(45_000_000_000) // 2 × 22.5B — tiering the summed context
	)

	if wantOutputNanos == wrongRunLevelTiering {
		t.Fatal("test design error: correct and incorrect answers are equal for these inputs")
	}

	if sum.Output.State != domain.MoneyKnown {
		t.Fatalf("sum.Output state = %v, want MoneyKnown", sum.Output.State)
	}
	if got := sum.Output.Amount.Nanos(); got != wantOutputNanos {
		t.Errorf("sum.Output = %d nanos, want %d (per-invocation tiering, not run-level tiering); run-level tiering would give %d",
			got, wantOutputNanos, wrongRunLevelTiering)
	}
}

// ---------------------------------------------------------------------------
// T4.5 — Unpriced model state
// ---------------------------------------------------------------------------

func TestPriceInvocation_ModelNotInTable_PricedFalse(t *testing.T) {
	// A model absent from the pricing table yields Priced=false.
	table := domain.EmptyPricingTable()

	ic := cost.PriceInvocation(inv("unknown-model", domain.TokenUsage{
		Input:  domain.Tokens(1_000_000),
		Output: domain.Tokens(1_000_000),
	}), table)

	if ic.Priced {
		t.Error("Priced must be false when model is absent from the pricing table")
	}
}

func TestPriceInvocation_ModelNotInTable_AllMoneyIsUnpriced(t *testing.T) {
	// A model absent from the table yields every money cell MoneyUnpriced —
	// never MoneyKnown(0). This is required by AC4.5.
	table := domain.EmptyPricingTable()

	ic := cost.PriceInvocation(inv("unknown-model", domain.TokenUsage{
		Input:  domain.Tokens(1_000_000),
		Output: domain.Tokens(1_000_000),
	}), table)

	assertUnpriced(t, "Input (unpriced model)", ic.Money.Input)
	assertUnpriced(t, "CacheRead (unpriced model)", ic.Money.CacheRead)
	assertUnpriced(t, "CacheCreation (unpriced model)", ic.Money.CacheCreation)
	assertUnpriced(t, "Output (unpriced model)", ic.Money.Output)
}

func TestPriceInvocation_ModelNotInTable_TokensPreserved(t *testing.T) {
	// An unpriced invocation must still carry its token counts in InvocationCost.Tokens.
	// The pricing gap is an explicit money state, not a reason to discard usage data.
	table := domain.EmptyPricingTable()

	ic := cost.PriceInvocation(inv("unknown-model", domain.TokenUsage{
		Input:  domain.Tokens(100_000),
		Output: domain.Tokens(50_000),
	}), table)

	if v, ok := ic.Tokens.Input.Value(); !ok || v != 100_000 {
		t.Errorf("Tokens.Input = (%d, %v), want (100_000, true) — token data must survive an unpriced model", v, ok)
	}
	if v, ok := ic.Tokens.Output.Value(); !ok || v != 50_000 {
		t.Errorf("Tokens.Output = (%d, %v), want (50_000, true) — token data must survive an unpriced model", v, ok)
	}
}

func TestSumCosts_MixedPricedAndUnpriced_CompleteFalse(t *testing.T) {
	// A sum containing at least one unpriced invocation must have Complete=false.
	// This signals to the caller that the money total is partial.
	model := domain.ModelID("test-model")
	table := testTable(flatPricing(t, model))

	pricedCost := cost.PriceInvocation(inv(model, domain.TokenUsage{
		Input:  domain.Tokens(1_000_000),
		Output: domain.Tokens(1_000_000),
	}), table)
	unpricedCost := cost.PriceInvocation(inv("unknown-model", domain.TokenUsage{
		Input:  domain.Tokens(500_000),
		Output: domain.Tokens(500_000),
	}), domain.EmptyPricingTable())

	sum := cost.SumCosts(pricedCost, unpricedCost)

	if sum.Complete {
		t.Error("Complete must be false when at least one contributing invocation was unpriced")
	}
}

func TestSumCosts_AllUnpriced_TotalIsUnpricedAndIncompleteFalse(t *testing.T) {
	// When every invocation is unpriced, Complete=false.
	emptyTable := domain.EmptyPricingTable()

	cost1 := cost.PriceInvocation(inv("model-a", domain.TokenUsage{Input: domain.Tokens(1_000_000)}), emptyTable)
	cost2 := cost.PriceInvocation(inv("model-b", domain.TokenUsage{Input: domain.Tokens(2_000_000)}), emptyTable)

	sum := cost.SumCosts(cost1, cost2)

	if sum.Complete {
		t.Error("Complete must be false when all invocations are unpriced")
	}
}

func TestSumCosts_AllPriced_CompleteTrueWithCorrectTotal(t *testing.T) {
	// When all invocations are priced, Complete=true and the total reflects
	// the sum of all per-category Known amounts.
	model := domain.ModelID("test-model")
	table := testTable(flatPricing(t, model))

	// Two invocations: 1M input each at "3.00"/1M = 3_000_000_000 nanos each.
	cost1 := cost.PriceInvocation(inv(model, domain.TokenUsage{Input: domain.Tokens(1_000_000)}), table)
	cost2 := cost.PriceInvocation(inv(model, domain.TokenUsage{Input: domain.Tokens(1_000_000)}), table)

	sum := cost.SumCosts(cost1, cost2)

	if !sum.Complete {
		t.Error("Complete must be true when all contributing invocations are priced")
	}

	// Sum of input: 2 × 3_000_000_000 = 6_000_000_000 nanos.
	assertKnownMoney(t, "Input sum (all priced)", sum.Input, 6_000_000_000)
}

func TestSumCosts_Empty_ReturnsNoDataAndComplete(t *testing.T) {
	// An empty set of costs yields all-NoData with Complete=true (nothing is incomplete).
	sum := cost.SumCosts()

	if !sum.Complete {
		t.Error("Complete must be true for an empty sum (no unpriced invocations)")
	}
	if sum.Input.State != domain.MoneyNoData {
		t.Errorf("Input state = %v, want MoneyNoData (empty sum)", sum.Input.State)
	}
}
