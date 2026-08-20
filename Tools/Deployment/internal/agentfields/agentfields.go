package agentfields

// agentfields defines the MOSAIC generic/stamp field-name vocabulary and the predicates
// that classify a frontmatter key as "known to MOSAIC". It is a dependency-free leaf
// package: it imports only internal/domain (and only if a value type is needed).
//
// Created in Stage 2 for the unknown-field strip predicate; extended in Stage 4 with the
// legacy ↔ mosaic_-prefixed pairing.

// FieldName pairs the three names one MOSAIC-only bookkeeping field can appear under.
type FieldName struct {
	// Generic is the key used in generic source files under Catalog/Subagents/ or Catalog/UtilityAgents/.
	// Empty when the field exists only in deployed files.
	Generic string
	// Deployed is the mosaic_-prefixed key written into deployed, harness-facing files.
	Deployed string
	// Legacy is the unprefixed key deployed files used before the migration.
	Legacy string
}

// The authoritative registry of MOSAIC-only deployed fields. These are the fields that
// the deployment pipeline writes into deployed agent files on MOSAIC's own account.
// No literal "mosaic_" string may appear outside this package and its tests.
var registry = []FieldName{
	{Generic: "id", Deployed: "mosaic_id", Legacy: "id"},
	{Generic: "role", Deployed: "mosaic_role", Legacy: "role"},
	{Generic: "version", Deployed: "mosaic_version", Legacy: "version"},
	{Generic: "", Deployed: "mosaic_transform_version", Legacy: "transform_version"},
	{Generic: "", Deployed: "mosaic_injections_version", Legacy: "injections_version"},
	{Generic: "", Deployed: "mosaic_tool_mappings_version", Legacy: "tool_mappings_version"},
	{Generic: "", Deployed: "mosaic_bundle_version", Legacy: "bundle_version"},
	{Generic: "", Deployed: "mosaic_orchestrator_injections_version", Legacy: "orchestrator_injections_version"},
	{Generic: "", Deployed: "mosaic_protocol_version", Legacy: "protocol_version"},
}

// genericVocabularyKeys is the set of keys belonging to the generic agent frontmatter
// vocabulary documented in Catalog/SourceFilesFormat.md.
var genericVocabularyKeys = map[string]bool{
	"id":               true,
	"version":          true,
	"name":             true,
	"description":      true,
	"role":             true,
	"model":            true,
	"tools":            true,
	"recommended_tier": true,
	"tier_rationale":   true,
	"required_skills":  true,
	// Infrastructure-agent fields — present only on infrastructure agents.
	"infrastructure": true,
	"triggers":       true,
	"on_failure":     true,
}

// All returns every MOSAIC-only deployed field, in a stable declared order.
func All() []FieldName {
	result := make([]FieldName, len(registry))
	copy(result, registry)
	return result
}

// ByGeneric looks a field up by its generic-source key. ok is false for a key that is
// not a MOSAIC-only deployed field.
func ByGeneric(generic string) (FieldName, bool) {
	for _, f := range registry {
		if f.Generic != "" && f.Generic == generic {
			return f, true
		}
	}
	return FieldName{}, false
}

// ByDeployedName looks a field up by either its Deployed or its Legacy key.
func ByDeployedName(key string) (FieldName, bool) {
	for _, f := range registry {
		if f.Deployed == key || f.Legacy == key {
			return f, true
		}
	}
	return FieldName{}, false
}

// ReadOrder returns the deployed keys to try when reading f back, most-preferred first:
// always {f.Deployed, f.Legacy}. A reader takes the first key present.
func ReadOrder(f FieldName) []string {
	return []string{f.Deployed, f.Legacy}
}

// IsMosaicOnlyDeployedKey reports whether key is the Deployed or Legacy name of any
// entry in All().
func IsMosaicOnlyDeployedKey(key string) bool {
	_, ok := ByDeployedName(key)
	return ok
}

// IsGenericVocabularyKey reports whether key belongs to the generic agent frontmatter
// vocabulary documented in Catalog/SourceFilesFormat.md:
// id, version, name, description, role, model, tools, recommended_tier,
// tier_rationale, required_skills.
func IsGenericVocabularyKey(key string) bool {
	return genericVocabularyKeys[key]
}

// IsKnownMosaicKey reports IsGenericVocabularyKey(key) || IsMosaicOnlyDeployedKey(key).
// It is the "known to MOSAIC" predicate the FR-3 unknown-field strip is defined against.
func IsKnownMosaicKey(key string) bool {
	return IsGenericVocabularyKey(key) || IsMosaicOnlyDeployedKey(key)
}

// PromoteDropKeys returns every key promote removes on MOSAIC's own account: the
// Deployed and Legacy names of every All() entry that has NO generic counterpart, plus "model".
//
// Entries with a non-empty Generic (today: id and role) are excluded from the drop set
// because promote does not drop them — it reconstructs them under their generic key. The
// promote code removes both deployed forms of each such entry explicitly and then writes
// the generic key, so exactly one form survives in the promoted file.
func PromoteDropKeys() []string {
	var keys []string
	for _, f := range registry {
		// Skip entries that have a generic counterpart — promote reconstructs them under
		// their generic key rather than dropping them.
		if f.Generic != "" {
			continue
		}
		if f.Deployed != "" {
			keys = append(keys, f.Deployed)
		}
		if f.Legacy != "" && f.Legacy != f.Deployed {
			keys = append(keys, f.Legacy)
		}
	}
	// "model" is always dropped by promote even though it is not in the registry proper.
	keys = append(keys, "model")
	return keys
}
