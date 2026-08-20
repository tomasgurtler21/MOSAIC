package opencode_test

// frontmatter_role_mode_test.go: Tests for role-driven mode field resolution in the
// OpenCode harness Frontmatter method.
//
// The mode field must be resolved from the deploying agent's role via the descriptor's
// value_by_role mechanism, not from AgentKey. The mapping is:
//
//   orchestrator -> primary
//   utility      -> primary
//   standalone   -> primary
//   subagent     -> subagent
//
// Each test calls mod.Frontmatter(req) directly (bypassing transform.Apply) to isolate
// the role-conditional resolution logic. AgentKey is set to a non-orchestrator value for
// all tests so that the hardcoded AgentKey check does not interfere.
//
// RED STATE:
//   TestFrontmatter_ModeIsPrimaryForRoleOrchestrator, _RoleUtility, _RoleStandalone:
//     FAIL until the implementation removes the hardcoded mode block and calls
//     descriptor.ResolveRoleConditionalFields instead.
//
//   TestFrontmatter_ModeIsSubagentForRoleSubagent:
//     PASSES in RED phase (default value matches). Serves as a regression guard.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// findModeField searches a FrontmatterPlan's Set slice for the field with key "mode"
// and returns its scalar value. It reports a fatal error if the field is absent.
func findModeField(t *testing.T, set []domain.FrontmatterField) string {
	t.Helper()
	for _, f := range set {
		if f.Key == "mode" {
			return f.Value.Scalar
		}
	}
	t.Fatalf("\"mode\" field not found in FrontmatterPlan.Set; Set keys: %v", opencodeSetKeys(set))
	return ""
}

// TestFrontmatter_ModeIsPrimaryForRoleOrchestrator verifies that Frontmatter() returns
// mode: primary when Role is RoleOrchestrator. The AgentKey is set to a non-orchestrator
// value so the old hardcoded AgentKey path cannot mask a failing role-based lookup.
//
// RED: FAILS until the implementation uses descriptor.ResolveRoleConditionalFields
// to resolve mode from the value_by_role mapping in opencode.yaml.
func TestFrontmatter_ModeIsPrimaryForRoleOrchestrator(t *testing.T) {
	mod := newModule(t)

	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "any-orchestrator-agent",
		Role:     domain.RoleOrchestrator,
	}

	plan, err := mod.Frontmatter(req)
	if err != nil {
		t.Fatalf("Frontmatter(RoleOrchestrator): %v", err)
	}

	got := findModeField(t, plan.Set)
	if got != "primary" {
		t.Errorf("mode = %q for RoleOrchestrator, want %q; mode must be resolved from the descriptor value_by_role mapping, not from AgentKey", got, "primary")
	}
}

// TestFrontmatter_ModeIsPrimaryForRoleUtility verifies that Frontmatter() returns
// mode: primary when Role is RoleUtility. Utility agents are deployed as primary agents
// (users interact with them directly), not as subagents.
//
// RED: FAILS until the implementation uses descriptor.ResolveRoleConditionalFields
// to resolve mode from the value_by_role mapping in opencode.yaml.
func TestFrontmatter_ModeIsPrimaryForRoleUtility(t *testing.T) {
	mod := newModule(t)

	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "any-utility-agent",
		Role:     domain.RoleUtility,
	}

	plan, err := mod.Frontmatter(req)
	if err != nil {
		t.Fatalf("Frontmatter(RoleUtility): %v", err)
	}

	got := findModeField(t, plan.Set)
	if got != "primary" {
		t.Errorf("mode = %q for RoleUtility, want %q; utility agents are primary agents and must receive mode: primary", got, "primary")
	}
}

// TestFrontmatter_ModeIsPrimaryForRoleStandalone verifies that Frontmatter() returns
// mode: primary when Role is RoleStandalone. Standalone agents are deployed outside any
// workflow and are addressed directly by users, so they must be primary agents.
//
// RED: FAILS until the implementation uses descriptor.ResolveRoleConditionalFields
// to resolve mode from the value_by_role mapping in opencode.yaml.
func TestFrontmatter_ModeIsPrimaryForRoleStandalone(t *testing.T) {
	mod := newModule(t)

	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "any-standalone-agent",
		Role:     domain.RoleStandalone,
	}

	plan, err := mod.Frontmatter(req)
	if err != nil {
		t.Fatalf("Frontmatter(RoleStandalone): %v", err)
	}

	got := findModeField(t, plan.Set)
	if got != "primary" {
		t.Errorf("mode = %q for RoleStandalone, want %q; standalone agents are primary agents and must receive mode: primary", got, "primary")
	}
}

// TestFrontmatter_ModeIsSubagentForRoleSubagent verifies that Frontmatter() returns
// mode: subagent when Role is RoleSubagent. Subagents are invoked by other agents via
// the task tool and must not be directly addressed by users.
//
// PASSES in RED phase (existing default value matches). Serves as a regression guard
// after the descriptor-driven implementation replaces the hardcoded logic.
func TestFrontmatter_ModeIsSubagentForRoleSubagent(t *testing.T) {
	mod := newModule(t)

	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "any-subagent",
		Role:     domain.RoleSubagent,
	}

	plan, err := mod.Frontmatter(req)
	if err != nil {
		t.Fatalf("Frontmatter(RoleSubagent): %v", err)
	}

	got := findModeField(t, plan.Set)
	if got != "subagent" {
		t.Errorf("mode = %q for RoleSubagent, want %q; subagents are invoked by other agents and must receive mode: subagent", got, "subagent")
	}
}
