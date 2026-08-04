package domain

import "bytes"

// ProtocolVariant identifies one of the two deployable protocol text blocks.
type ProtocolVariant string

const (
	// ProtocolSubagent is the protocol variant for worker and utility agents.
	ProtocolSubagent ProtocolVariant = "subagent"
	// ProtocolOrchestrator is the protocol variant for orchestrator agents.
	ProtocolOrchestrator ProtocolVariant = "orchestrator"
)

// VariantForRole maps an agent role to its protocol variant.
// RoleOrchestrator maps to ProtocolOrchestrator; every other role (worker, utility) maps to
// ProtocolSubagent. This is the single place the many-to-one role→variant relationship is
// expressed.
func VariantForRole(role AgentRole) ProtocolVariant {
	if role == RoleOrchestrator {
		return ProtocolOrchestrator
	}
	return ProtocolSubagent
}

// ProtocolContent is the deployable payload of the canonical protocol source document,
// loaded once per run by the app layer. The zero value is invalid and is rejected by the
// transform.
type ProtocolContent struct {
	// Version is the protocol source document's frontmatter `version` scalar, verbatim.
	// Stamped into the deployed region's version marker and compared by the planner.
	Version string
	// Blocks holds the inner content of each role block, keyed by variant. Both variants are
	// present in a successfully loaded value.
	Blocks map[ProtocolVariant][]byte
}

// For returns the block for the given role.
// ok is false when the block is absent or has whitespace-only content.
func (p ProtocolContent) For(role AgentRole) (block []byte, ok bool) {
	v := VariantForRole(role)
	b, found := p.Blocks[v]
	if !found || len(bytes.TrimSpace(b)) == 0 {
		return nil, false
	}
	return b, true
}
