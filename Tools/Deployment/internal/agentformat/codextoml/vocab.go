// Package codextoml implements the Codex TOML agent file format translator.
// It imports agentformat for the translator interface and the report type, and
// internal/agentfields for the stamp key vocabulary.
package codextoml

import "mosaic-deploy/internal/agentfields"

// emittedKeys is the closed Codex-native emitted set: the keys a Codex agent file
// may carry as native TOML on MOSAIC's account. This is the vocabulary of the
// Codex file format, not of MOSAIC's field registry, which is why a literal set
// is the correct shape here. Adding a member means Codex accepts a new top-level
// TOML key from a source agent.
var emittedKeys = map[string]struct{}{
	"name":                    {},
	"description":             {},
	"model":                   {},
	"sandbox_mode":            {},
	"developer_instructions":  {},
}

// isStampKey reports whether key is the Deployed name of some entry in
// agentfields.All(). It is Deployed-only on purpose.
//
// agentfields.IsMosaicOnlyDeployedKey delegates to ByDeployedName, which also
// matches Legacy names. Three registry entries carry the unprefixed Legacy names
// "id", "role" and "version" -- ordinary vocabulary keys in this format that a
// user may legitimately hand-add to their Codex file. Matching them via Legacy
// would both misclassify them as stamps and make them unavailable for user-key
// carriage downstream.
//
// Do not call agentfields.IsMosaicOnlyDeployedKey here.
func isStampKey(key string) bool {
	for _, f := range agentfields.All() {
		if f.Deployed == key {
			return true
		}
	}
	return false
}

// isMosaicOwned is the single ownership predicate for this format: a key is
// MOSAIC-owned when it is in the Codex-native emitted set, or when it is the
// Deployed name of a stamp in agentfields.All(). There is no second ownership
// notion anywhere in the translator layer.
//
// Decode's container placement, encode's classification, and the carriage
// resurrection guard all call this one function.
//
// Do not call agentfields.IsKnownMosaicKey here: it returns true for keys Codex
// never emits (role, tools, recommended_tier, tier_rationale, required_skills),
// which would silently suppress carriage of those user keys.
func isMosaicOwned(key string) bool {
	_, inEmitted := emittedKeys[key]
	return inEmitted || isStampKey(key)
}
