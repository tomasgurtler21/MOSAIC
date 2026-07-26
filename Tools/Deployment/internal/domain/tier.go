package domain

// Tier is an open string identifying an agent's recommended model tier. No constant set, enum,
// or validation against a fixed list exists anywhere in this module. Tiers are stored, displayed,
// and compared verbatim as written in the source files.
type Tier string

// TierInfo bundles the tier string with its rationale text and the agents that declared it,
// for display during model selection.
type TierInfo struct {
	Tier      Tier
	Rationale string   // first non-empty rationale found in source
	AgentKeys []string // agents declaring this tier, sorted
}

// ModelOrigin records how a model selection was obtained.
type ModelOrigin string

const (
	OriginTierMapping ModelOrigin = "tier-mapping"
	OriginHarnessList ModelOrigin = "harness-list"
	OriginCustom      ModelOrigin = "custom"
	OriginUnresolved  ModelOrigin = "unresolved" // skipped or never offered
)

// ModelSelection is the resolved model for one agent. ModelID is emitted verbatim and is never
// rewritten, reformatted, or validated against the harness model list.
type ModelSelection struct {
	ModelID string
	Origin  ModelOrigin
}

// Resolved reports whether a definite model was selected (as opposed to skipped or unresolved).
func (m ModelSelection) Resolved() bool {
	return m.ModelID != "" && m.Origin != OriginUnresolved
}
