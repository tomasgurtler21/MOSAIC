package domain_test

// bundle_regression_test.go pins the invariant that BundleContent.AppliesToRole returns
// false for RoleUtility and RoleStandalone against any bundle whose blocks declare only
// "subagent" and "orchestrator" in their AppliesTo fields.
//
// This regression lock is required because the vocabulary expansion makes
// ParseAgentRole accept "utility" and "standalone" — meaning bundleConformance now
// reaches the AppliesToRole lookup for deployed files carrying these roles. Without
// this lock, a future developer could accidentally add an `applies_to: utility` or
// `applies_to: standalone` bundle block and silently start injecting bundle content
// into files that must never receive it.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// standardSubagentOrchestratorBundle returns a BundleContent that declares only subagent
// and orchestrator blocks — the production case before any new role-specific block is added.
func standardSubagentOrchestratorBundle() domain.BundleContent {
	return domain.BundleContent{
		Version: "1.0.0",
		Blocks: []domain.BundleBlock{
			{
				Name:      "AuthorityHierarchy:Subagent",
				Target:    "AuthorityHierarchy",
				AppliesTo: "subagent",
				Content:   []byte("Subagent authority hierarchy content."),
			},
			{
				Name:      "AuthorityHierarchy:Orchestrator",
				Target:    "AuthorityHierarchy",
				AppliesTo: "orchestrator",
				Content:   []byte("Orchestrator authority hierarchy content."),
			},
		},
	}
}

// ---------------------------------------------------------------------------
// AppliesToRole regression — utility and standalone must never match
// ---------------------------------------------------------------------------

// TestBundleContent_AppliesToRole_UtilityRole_ReturnsFalse pins that a standard bundle
// (subagent + orchestrator blocks only) does not apply to RoleUtility. This ensures that
// once ParseAgentRole accepts "utility", the bundleConformance check still skips utility
// files rather than attempting to validate bundle regions that were never injected.
func TestBundleContent_AppliesToRole_UtilityRole_ReturnsFalse(t *testing.T) {
	// Arrange
	bundle := standardSubagentOrchestratorBundle()

	// Act
	applies := bundle.AppliesToRole(domain.RoleUtility)

	// Assert
	if applies {
		t.Error("BundleContent.AppliesToRole(RoleUtility) = true, want false; " +
			"a bundle declaring only subagent and orchestrator blocks must not apply to utility")
	}
}

// TestBundleContent_AppliesToRole_StandaloneRole_ReturnsFalse pins that a standard bundle
// (subagent + orchestrator blocks only) does not apply to RoleStandalone. This ensures that
// once ParseAgentRole accepts "standalone", the bundleConformance check still skips
// standalone files rather than attempting to validate bundle regions that were never injected.
func TestBundleContent_AppliesToRole_StandaloneRole_ReturnsFalse(t *testing.T) {
	// Arrange
	bundle := standardSubagentOrchestratorBundle()

	// Act
	applies := bundle.AppliesToRole(domain.RoleStandalone)

	// Assert
	if applies {
		t.Error("BundleContent.AppliesToRole(RoleStandalone) = true, want false; " +
			"a bundle declaring only subagent and orchestrator blocks must not apply to standalone")
	}
}

// TestBundleContent_AppliesToRole_SubagentAndOrchestratorStillApply verifies that the
// regression lock does not accidentally break the positive case — subagent and orchestrator
// files must still be subject to bundle conformance.
func TestBundleContent_AppliesToRole_SubagentAndOrchestratorStillApply(t *testing.T) {
	bundle := standardSubagentOrchestratorBundle()

	if !bundle.AppliesToRole(domain.RoleSubagent) {
		t.Error("BundleContent.AppliesToRole(RoleSubagent) = false, want true")
	}
	if !bundle.AppliesToRole(domain.RoleOrchestrator) {
		t.Error("BundleContent.AppliesToRole(RoleOrchestrator) = false, want true")
	}
}
