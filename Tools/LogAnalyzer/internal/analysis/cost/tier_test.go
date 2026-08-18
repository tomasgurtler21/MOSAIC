package cost_test

// Tests for SelectTier: output tier selection per invocation.
//
// Key invariants under test:
//   - Context below threshold   → TierStandard
//   - Context exactly at threshold → TierLongContext  (>= comparison, not >)
//   - Context above threshold   → TierLongContext
//   - No threshold (HasThreshold=false) → always TierStandard regardless of context
//   - All context categories absent → TierStandard returned with ok=false
//   - Output tokens are excluded from context (only input+cacheRead+cacheCreation count)
//   - Selection is per invocation, not per run

import (
	"testing"

	"mosaic-log-analyzer/internal/analysis/cost"
	"mosaic-log-analyzer/internal/domain"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// tierTestPricingWithThreshold builds a ModelPricing with a long-context threshold.
// All rates are set to a uniform value so tier selection is the only variable.
func tierTestPricingWithThreshold(threshold int64) domain.ModelPricing {
	r, err := domain.ParseRate("1.00")
	if err != nil {
		panic("ParseRate(1.00): " + err.Error())
	}
	return domain.ModelPricing{
		Model:                  "tier-test-model",
		Input:                  r,
		CachedInput:            r,
		CacheWrite:             r,
		OutputUnderThreshold:   r,
		OutputAtThreshold:      r,
		ContextLengthThreshold: threshold,
		HasThreshold:           true,
	}
}

// tierTestPricingFlat builds a ModelPricing with no long-context tier.
func tierTestPricingFlat() domain.ModelPricing {
	r, err := domain.ParseRate("1.00")
	if err != nil {
		panic("ParseRate(1.00): " + err.Error())
	}
	return domain.ModelPricing{
		Model:                "tier-test-model",
		Input:                r,
		CachedInput:          r,
		CacheWrite:           r,
		OutputUnderThreshold: r,
		HasThreshold:         false,
	}
}

// ---------------------------------------------------------------------------
// Tier selection against a threshold
// ---------------------------------------------------------------------------

func TestSelectTier_BelowThreshold_IsStandard(t *testing.T) {
	// Context = 100_000, threshold = 200_000 → context < threshold → TierStandard.
	usage := domain.TokenUsage{
		Input: domain.Tokens(100_000),
	}
	entry := tierTestPricingWithThreshold(200_000)

	tier, ok := cost.SelectTier(usage, entry)

	if !ok {
		t.Error("ok must be true when total context is present")
	}
	if tier != cost.TierStandard {
		t.Errorf("tier = %v, want TierStandard (context 100K < threshold 200K)", tier)
	}
}

func TestSelectTier_ExactlyAtThreshold_IsLongContext(t *testing.T) {
	// Context = 200_000, threshold = 200_000 → context == threshold → TierLongContext.
	// The comparison is >=, so hitting the threshold exactly triggers the long-context tier.
	usage := domain.TokenUsage{
		Input: domain.Tokens(200_000),
	}
	entry := tierTestPricingWithThreshold(200_000)

	tier, ok := cost.SelectTier(usage, entry)

	if !ok {
		t.Error("ok must be true when total context is present")
	}
	if tier != cost.TierLongContext {
		t.Errorf("tier = %v, want TierLongContext (context exactly at threshold uses >= comparison)", tier)
	}
}

func TestSelectTier_AboveThreshold_IsLongContext(t *testing.T) {
	// Context = 300_000, threshold = 200_000 → context > threshold → TierLongContext.
	usage := domain.TokenUsage{
		Input: domain.Tokens(300_000),
	}
	entry := tierTestPricingWithThreshold(200_000)

	tier, ok := cost.SelectTier(usage, entry)

	if !ok {
		t.Error("ok must be true when total context is present")
	}
	if tier != cost.TierLongContext {
		t.Errorf("tier = %v, want TierLongContext (context 300K > threshold 200K)", tier)
	}
}

func TestSelectTier_MultiCategoryContext_SumsAllThree(t *testing.T) {
	// Total context = input + cacheRead + cacheCreation = 50_000 + 100_000 + 60_000 = 210_000.
	// threshold = 200_000 → 210_000 >= 200_000 → TierLongContext.
	// Output is deliberately set large to confirm it does NOT count toward context.
	usage := domain.TokenUsage{
		Input:         domain.Tokens(50_000),
		CacheRead:     domain.Tokens(100_000),
		CacheCreation: domain.Tokens(60_000),
		Output:        domain.Tokens(99_999), // must not contribute to context
	}
	entry := tierTestPricingWithThreshold(200_000)

	tier, ok := cost.SelectTier(usage, entry)

	if !ok {
		t.Error("ok must be true when at least one context category is present")
	}
	if tier != cost.TierLongContext {
		t.Errorf("tier = %v, want TierLongContext (input+cacheRead+cacheCreation = 210K >= 200K)", tier)
	}
}

func TestSelectTier_OutputNotCountedInContext(t *testing.T) {
	// Output tokens are NOT part of the total context for tier selection.
	// When only Output is present (all context categories absent), total context is absent.
	usage := domain.TokenUsage{
		Output: domain.Tokens(999_999), // does not count toward context
	}
	entry := tierTestPricingWithThreshold(100_000)

	_, ok := cost.SelectTier(usage, entry)

	if ok {
		t.Error("ok must be false when only Output is present; output does not contribute to context")
	}
}

func TestSelectTier_PartialContext_UsesOnlyPresentCategories(t *testing.T) {
	// When only some context categories are present, the total is the sum of present ones only.
	// Input = 180_000, CacheRead and CacheCreation absent → total = 180_000 < 200_000.
	usage := domain.TokenUsage{
		Input: domain.Tokens(180_000),
		// CacheRead absent
		// CacheCreation absent
	}
	entry := tierTestPricingWithThreshold(200_000)

	tier, ok := cost.SelectTier(usage, entry)

	if !ok {
		t.Error("ok must be true when at least one context category is present")
	}
	if tier != cost.TierStandard {
		t.Errorf("tier = %v, want TierStandard (partial context 180K < threshold 200K)", tier)
	}
}

func TestSelectTier_PartialContext_CanStillExceedThreshold(t *testing.T) {
	// A partial context (only input present) can still exceed the threshold.
	// Input = 250_000, CacheRead absent, CacheCreation absent → total = 250_000 >= 200_000.
	usage := domain.TokenUsage{
		Input: domain.Tokens(250_000),
	}
	entry := tierTestPricingWithThreshold(200_000)

	tier, ok := cost.SelectTier(usage, entry)

	if !ok {
		t.Error("ok must be true when at least one context category is present")
	}
	if tier != cost.TierLongContext {
		t.Errorf("tier = %v, want TierLongContext (input only = 250K >= threshold 200K)", tier)
	}
}

// ---------------------------------------------------------------------------
// No-threshold models
// ---------------------------------------------------------------------------

func TestSelectTier_NoThreshold_AlwaysStandardRegardlessOfContext(t *testing.T) {
	// A model with HasThreshold=false must always return TierStandard, regardless
	// of how large the total context is. This models flat-priced providers.
	usage := domain.TokenUsage{
		Input: domain.Tokens(999_999_999),
	}
	entry := tierTestPricingFlat()

	tier, _ := cost.SelectTier(usage, entry)

	if tier != cost.TierStandard {
		t.Errorf("tier = %v, want TierStandard (model has no long-context tier, HasThreshold=false)", tier)
	}
}

// ---------------------------------------------------------------------------
// Absent context
// ---------------------------------------------------------------------------

func TestSelectTier_AllContextAbsent_ReturnsFalseOk(t *testing.T) {
	// When all three context categories are absent, total context is absent.
	// SelectTier must return TierStandard with ok=false so the caller can raise a finding.
	usage := domain.TokenUsage{} // all absent
	entry := tierTestPricingWithThreshold(200_000)

	tier, ok := cost.SelectTier(usage, entry)

	if ok {
		t.Error("ok must be false when all context categories (input, cacheRead, cacheCreation) are absent")
	}
	if tier != cost.TierStandard {
		t.Errorf("tier = %v, want TierStandard (default when context absent)", tier)
	}
}

func TestSelectTier_AllContextAbsentNoThreshold_ReturnsFalseOk(t *testing.T) {
	// Even for a model with no threshold, absent total context returns ok=false.
	usage := domain.TokenUsage{} // all absent
	entry := tierTestPricingFlat()

	_, ok := cost.SelectTier(usage, entry)

	if ok {
		t.Error("ok must be false when all context categories are absent, even for a model with no threshold")
	}
}
