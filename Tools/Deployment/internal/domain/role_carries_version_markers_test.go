package domain_test

// role_carries_version_markers_test.go covers domain.RoleCarriesVersionMarkers.
//
// RoleCarriesVersionMarkers is the single predicate every protocol and bundle staleness
// decision consults. It answers: "is an agent of this role expected to carry a protocol
// version marker and a bundle version stamp in its deployed form?"
//
// The contract is:
//   - False for RoleUtility and RoleStandalone — those roles receive harness transformation
//     only. A deployed utility or standalone agent intentionally carries no protocol marker
//     and no bundle stamp; the absence of either must never be reported as staleness.
//   - True for RoleSubagent and RoleOrchestrator — fully-managed roles that carry both
//     markers and must be detected as stale when the markers are absent or outdated.
//   - True for any unrecognised role — fail-closed behaviour; an unknown role is treated as
//     a fully-managed agent to avoid silently suppressing staleness on an unexpected role.
//
// This predicate must return the same result for any string that is not explicitly excluded,
// so tests exercise an unrecognised string alongside the four known constants.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Excluded roles: RoleUtility and RoleStandalone
// ---------------------------------------------------------------------------

// TestRoleCarriesVersionMarkers_Utility_ReturnsFalse verifies that a utility agent is not
// expected to carry version markers. Utility agents receive harness transformation only and
// intentionally have no protocol or bundle stamp in their deployed file; the absence of
// those markers is by design and must never trigger a staleness update.
func TestRoleCarriesVersionMarkers_Utility_ReturnsFalse(t *testing.T) {
	if domain.RoleCarriesVersionMarkers(domain.RoleUtility) {
		t.Error("RoleCarriesVersionMarkers(RoleUtility) = true, want false; " +
			"utility agents carry no protocol marker or bundle stamp by design")
	}
}

// TestRoleCarriesVersionMarkers_Standalone_ReturnsFalse verifies that a standalone agent is
// not expected to carry version markers. Standalone agents are deployed on their own and
// receive harness transformation only; the same design intent applies as for utility agents.
func TestRoleCarriesVersionMarkers_Standalone_ReturnsFalse(t *testing.T) {
	if domain.RoleCarriesVersionMarkers(domain.RoleStandalone) {
		t.Error("RoleCarriesVersionMarkers(RoleStandalone) = true, want false; " +
			"standalone agents carry no protocol marker or bundle stamp by design")
	}
}

// ---------------------------------------------------------------------------
// Included roles: RoleSubagent and RoleOrchestrator
// ---------------------------------------------------------------------------

// TestRoleCarriesVersionMarkers_Subagent_ReturnsTrue verifies that a subagent is expected
// to carry version markers. Subagents receive full protocol and bundle injection and must be
// detected as stale when those markers are absent or outdated.
func TestRoleCarriesVersionMarkers_Subagent_ReturnsTrue(t *testing.T) {
	if !domain.RoleCarriesVersionMarkers(domain.RoleSubagent) {
		t.Error("RoleCarriesVersionMarkers(RoleSubagent) = false, want true; " +
			"subagents must carry version markers and must be detected as stale when those markers are missing or outdated")
	}
}

// TestRoleCarriesVersionMarkers_Orchestrator_ReturnsTrue verifies that the orchestrator is
// expected to carry version markers. The orchestrator receives a protocol marker and its
// deployed file must be refreshed when the protocol version changes.
func TestRoleCarriesVersionMarkers_Orchestrator_ReturnsTrue(t *testing.T) {
	if !domain.RoleCarriesVersionMarkers(domain.RoleOrchestrator) {
		t.Error("RoleCarriesVersionMarkers(RoleOrchestrator) = false, want true; " +
			"the orchestrator carries a protocol marker and must be detected as stale when it is absent or outdated")
	}
}

// ---------------------------------------------------------------------------
// Fail-closed: unrecognised roles
// ---------------------------------------------------------------------------

// TestRoleCarriesVersionMarkers_UnrecognisedRole_ReturnsTrue verifies that an unrecognised
// role string is treated as a fully-managed agent. The fail-closed rule ensures that a new
// or misspelled role string does not silently suppress staleness detection; it is safer to
// over-update an agent than to leave it stale indefinitely.
func TestRoleCarriesVersionMarkers_UnrecognisedRole_ReturnsTrue(t *testing.T) {
	if !domain.RoleCarriesVersionMarkers("unrecognised-role") {
		t.Error("RoleCarriesVersionMarkers(\"unrecognised-role\") = false, want true; " +
			"unrecognised roles must fail-closed and be treated as fully-managed agents")
	}
}

// TestRoleCarriesVersionMarkers_EmptyString_ReturnsTrue verifies that an empty role string
// is treated as a fully-managed agent under the fail-closed rule.
func TestRoleCarriesVersionMarkers_EmptyString_ReturnsTrue(t *testing.T) {
	if !domain.RoleCarriesVersionMarkers("") {
		t.Error("RoleCarriesVersionMarkers(\"\") = false, want true; " +
			"an empty role string must fail-closed")
	}
}

// ---------------------------------------------------------------------------
// Table-driven: all known roles and representative unknown inputs
// ---------------------------------------------------------------------------

// TestRoleCarriesVersionMarkers_TableDriven exercises every known AgentRole constant and a
// representative sample of unrecognised strings. This is the canonical regression guard:
// any future addition to the role vocabulary that should also be excluded from markers must
// add a row here with wantCarries = false and a matching case in the production function.
func TestRoleCarriesVersionMarkers_TableDriven(t *testing.T) {
	cases := []struct {
		role        domain.AgentRole
		wantCarries bool
		reason      string
	}{
		{domain.RoleSubagent, true,
			"subagents receive full protocol and bundle injection"},
		{domain.RoleOrchestrator, true,
			"the orchestrator receives a protocol marker"},
		{domain.RoleUtility, false,
			"utility agents carry no markers by design — absence must not be detected as staleness"},
		{domain.RoleStandalone, false,
			"standalone agents carry no markers by design — same exclusion as utility"},

		// Fail-closed: unrecognised strings → true.
		{"worker", true, "legacy alias is not in the exclusion set"},
		{"analyst", true, "unknown role must fail-closed"},
		{"", true, "empty string must fail-closed"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.role), func(t *testing.T) {
			got := domain.RoleCarriesVersionMarkers(tc.role)
			if got != tc.wantCarries {
				t.Errorf("RoleCarriesVersionMarkers(%q) = %v, want %v; %s",
					tc.role, got, tc.wantCarries, tc.reason)
			}
		})
	}
}
