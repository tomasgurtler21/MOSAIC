package cost

import "mosaic-log-analyzer/internal/domain"

// InvocationCost is the priced result for one invocation.
type InvocationCost struct {
	Model  domain.ModelID
	Tier   Tier
	Priced bool // false when the model had no pricing entry
	Tokens domain.TokenUsage
	Money  domain.CategoryMoney
}

// PriceInvocation prices one invocation.
//
// Binding rules:
//   - The cliff is all-or-nothing: when TierLongContext is selected the ENTIRE
//     invocation's output is priced at OutputAtThreshold, not only the tokens
//     above the threshold. No graduated/split calculation exists in this package.
//   - A model with no entry (found == false) yields Priced=false and every money
//     cell MoneyUnpriced — never MoneyKnown(0).
//   - An absent token category yields MoneyNoData for that category and
//     contributes nothing to the total.
func PriceInvocation(inv domain.InvocationUsage, table domain.PricingTable) InvocationCost {
	result := InvocationCost{
		Model:  inv.Model,
		Tokens: inv.Usage,
	}

	entry, found := table.Lookup(inv.Model)
	if !found {
		// No pricing entry: all money cells are MoneyUnpriced — never MoneyKnown(0).
		unpriced := domain.UnpricedMoney()
		total, complete := domain.SumMoney(unpriced, unpriced, unpriced, unpriced)
		result.Priced = false
		result.Money = domain.CategoryMoney{
			Input:         unpriced,
			CacheRead:     unpriced,
			CacheCreation: unpriced,
			Output:        unpriced,
			Total:         total,
			Complete:      complete,
		}
		return result
	}

	result.Priced = true

	// Determine output tier from total context vs. threshold (per invocation).
	tier, _ := SelectTier(inv.Usage, entry)
	result.Tier = tier

	// Price each category independently using its configured rate.
	// An absent token category yields MoneyNoData via Rate.Apply.
	inputMoney := entry.Input.Apply(inv.Usage.Input)
	cacheReadMoney := entry.CachedInput.Apply(inv.Usage.CacheRead)
	cacheCreationMoney := entry.CacheWrite.Apply(inv.Usage.CacheCreation)

	// The cliff is all-or-nothing: TierLongContext reprices the ENTIRE output
	// at OutputAtThreshold — no graduated/split calculation.
	var outputRate domain.Rate
	if tier == TierLongContext {
		outputRate = entry.OutputAtThreshold
	} else {
		outputRate = entry.OutputUnderThreshold
	}
	outputMoney := outputRate.Apply(inv.Usage.Output)

	total, complete := domain.SumMoney(inputMoney, cacheReadMoney, cacheCreationMoney, outputMoney)

	result.Money = domain.CategoryMoney{
		Input:         inputMoney,
		CacheRead:     cacheReadMoney,
		CacheCreation: cacheCreationMoney,
		Output:        outputMoney,
		Total:         total,
		Complete:      complete,
	}

	return result
}

// SumCosts folds per-invocation costs into one CategoryMoney, setting Complete
// false when any contributing invocation was unpriced.
func SumCosts(costs ...InvocationCost) domain.CategoryMoney {
	inputs := make([]domain.MoneyValue, len(costs))
	cacheReads := make([]domain.MoneyValue, len(costs))
	cacheCreations := make([]domain.MoneyValue, len(costs))
	outputs := make([]domain.MoneyValue, len(costs))

	for i, c := range costs {
		inputs[i] = c.Money.Input
		cacheReads[i] = c.Money.CacheRead
		cacheCreations[i] = c.Money.CacheCreation
		outputs[i] = c.Money.Output
	}

	inputTotal, inputComplete := domain.SumMoney(inputs...)
	cacheReadTotal, cacheReadComplete := domain.SumMoney(cacheReads...)
	cacheCreationTotal, cacheCreationComplete := domain.SumMoney(cacheCreations...)
	outputTotal, outputComplete := domain.SumMoney(outputs...)

	// The overall total is the money sum across all four categories.
	total, _ := domain.SumMoney(inputTotal, cacheReadTotal, cacheCreationTotal, outputTotal)

	// Complete is false when any per-category sum contained an unpriced value.
	complete := inputComplete && cacheReadComplete && cacheCreationComplete && outputComplete

	return domain.CategoryMoney{
		Input:         inputTotal,
		CacheRead:     cacheReadTotal,
		CacheCreation: cacheCreationTotal,
		Output:        outputTotal,
		Total:         total,
		Complete:      complete,
	}
}
