package catalog

import (
	"sort"

	"mosaic-deploy/internal/domain"
)

// buildTiers collects every distinct RecommendedTier string from all agents in agentIdx
// (workers, orchestrator, and utility agents), groups them, and returns one TierInfo per
// distinct tier string. Tier strings are returned verbatim — no normalisation, case-folding,
// or validation against a fixed list is performed.
func buildTiers(agentIdx map[string]domain.Agent) []domain.TierInfo {
	type tierAccum struct {
		rationale string
		keys      []string
	}
	tierMap := make(map[string]*tierAccum)

	for _, a := range agentIdx {
		tier := string(a.RecommendedTier)
		if tier == "" {
			continue
		}
		if tierMap[tier] == nil {
			tierMap[tier] = &tierAccum{}
		}
		acc := tierMap[tier]
		// Keep the first non-empty rationale found.
		if acc.rationale == "" && a.TierRationale != "" {
			acc.rationale = a.TierRationale
		}
		acc.keys = append(acc.keys, a.Key)
	}

	result := make([]domain.TierInfo, 0, len(tierMap))
	for tier, acc := range tierMap {
		sort.Strings(acc.keys)
		result = append(result, domain.TierInfo{
			Tier:      domain.Tier(tier),
			Rationale: acc.rationale,
			AgentKeys: acc.keys,
		})
	}

	// Sort by tier string for stable, deterministic output.
	sort.Slice(result, func(i, j int) bool {
		return string(result[i].Tier) < string(result[j].Tier)
	})

	return result
}
