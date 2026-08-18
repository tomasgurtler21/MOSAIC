package app

// refresh_scope.go declares the RefreshScope type, its two recognised values, and the
// RefreshDecision type that carries the full outcome of the harness-only refresh-scope prompt.
// The Regions() method maps each scope to the set of managed region names it covers,
// reading from docformat.CanonicalDeployed rather than duplicating the list here.

import "mosaic-common/docformat"

// RefreshDecision is the outcome of the harness-only refresh-scope prompt for one agent.
// It is the tool's record of user consent: a harness-only agent is refreshed only when the
// user explicitly said so.
type RefreshDecision struct {
	// Refresh reports whether the user authorised a refresh of this agent. False means the
	// user declined (skip, skip-all, cancel) or the prompt could not be delivered. When
	// false, the caller must emit no plan item and no content-plan entry for the agent.
	Refresh bool

	// Scope is the breadth the user chose. It is meaningful only when Refresh is true, and
	// is the zero value otherwise. Callers must not read it when Refresh is false, and must
	// not log it as though a refresh occurred.
	Scope RefreshScope

	// ApplyToAll latches this decision — including a declined one — onto every remaining
	// harness-only agent in the run, so the prompt is not re-asked. Set by both the "apply
	// to all" answer variants (with Refresh true) and by a SkippedAll outcome (with Refresh
	// false).
	ApplyToAll bool
}

// RefreshScope selects how much of a harness-only agent the Update flow regenerates.
// Because there is no generic source to align against, the tool can only rewrite regions
// it fully owns; this type says how many of them.
type RefreshScope string

const (
	// RefreshProtocolOnly refreshes only the <CommunicationProtocol type="managed"> region.
	// It is the narrow scope chosen when the user explicitly selects it. A non-answered prompt
	// outcome (skip, cancel, error) is represented as RefreshDecision{Refresh: false}, not as
	// this value; callers must check Refresh before consulting Scope.
	RefreshProtocolOnly RefreshScope = "protocol-only"
	// RefreshAllDeployed refreshes every canonical tool-managed managed region.
	RefreshAllDeployed RefreshScope = "all-deployed"
)

// Regions returns the region names this scope covers, in canonical order.
//
//   - RefreshAllDeployed returns a copy of docformat.CanonicalDeployed. The list is read
//     from the shared vocabulary, never duplicated as a literal here, so it cannot drift
//     from Tools/OldAgentsTransform/boundary_constants.py:CANONICAL_DEPLOYED.
//   - Every other value, including the zero value and any unrecognised string, returns
//     []string{"CommunicationProtocol"}. Widening the scope always requires an explicit,
//     recognised value.
//
// The returned slice is freshly allocated; mutating it does not affect the vocabulary.
func (s RefreshScope) Regions() []string {
	if s == RefreshAllDeployed {
		// Return a fresh copy so callers cannot corrupt the shared vocabulary by mutating
		// the returned slice.
		result := make([]string, len(docformat.CanonicalDeployed))
		copy(result, docformat.CanonicalDeployed)
		return result
	}
	// Every other value — including the zero value and any unrecognised string — defaults
	// to the narrow scope. Widening always requires an explicit, recognised value.
	return []string{"CommunicationProtocol"}
}
