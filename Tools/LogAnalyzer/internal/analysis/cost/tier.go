package cost

import "mosaic-log-analyzer/internal/domain"

// Tier names which output rate applies to an invocation.
type Tier uint8

const (
	TierStandard    Tier = iota // output priced at OutputUnderThreshold
	TierLongContext             // output priced at OutputAtThreshold
)

// SelectTier chooses the output tier for ONE invocation.
//
// Binding rules:
//   - Total context = input + cache read + cache creation (absent excluded).
//   - Threshold comparison is >= : exactly at the threshold selects TierLongContext.
//   - A model with no threshold (HasThreshold == false) is always TierStandard.
//   - When total context is absent entirely, TierStandard is selected and ok is
//     false so the caller can raise a finding.
//   - Selection is per invocation, NEVER per run. Summing a run's tokens and then
//     tiering is incorrect by contract.
func SelectTier(usage domain.TokenUsage, entry domain.ModelPricing) (t Tier, ok bool) {
	totalContext := usage.TotalContext()
	ctxValue, ctxPresent := totalContext.Value()

	// A model with no long-context tier is always TierStandard.
	// ok reflects whether context data was present, regardless of threshold config.
	if !entry.HasThreshold {
		return TierStandard, ctxPresent
	}

	// Total context absent: cannot determine tier; default to standard.
	if !ctxPresent {
		return TierStandard, false
	}

	// The comparison is >=: exactly at the threshold triggers the long-context tier.
	if ctxValue >= entry.ContextLengthThreshold {
		return TierLongContext, true
	}
	return TierStandard, true
}
