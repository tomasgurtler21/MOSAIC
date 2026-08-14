package app

// refresh_scope.go declares the RefreshScope type and its two recognised values.
// The Regions() method maps each scope to the set of managed region names it covers,
// reading from docformat.CanonicalDeployed rather than duplicating the list here.

import "mosaic-common/docformat"

// RefreshScope selects how much of a harness-only agent the Update flow regenerates.
// Because there is no generic source to align against, the tool can only rewrite regions
// it fully owns; this type says how many of them.
type RefreshScope string

const (
	// RefreshProtocolOnly refreshes only the <CommunicationProtocol type="managed"> region.
	// It is the narrow scope and the safe default for every non-answered prompt outcome.
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
