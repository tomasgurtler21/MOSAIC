package claudecode_test

// injection_role_test.go: Tests verifying that Injection respects req.Role for
// orchestrator-specific content selection, not req.AgentKey literal matching.
//
// The core bug: when an orchestrator-role agent has a non-"orchestrator" AgentKey
// (e.g. "orchestrator-script"), the current implementation (AgentKey != "orchestrator"
// check) incorrectly returns subagent-only shared content instead of the
// orchestrator-merged content.
//
// RED: TestInjection_OrchestratorRole_NonOrchestratorKey_ReturnsMergedContent FAILS
// until the implementation switches from AgentKey-based to Role-based gating.
// All tests in this file fail to compile until domain.InjectionRequest gains a Role
// field (implementation task I1.1).

import (
	"strings"
	"testing"

	"mosaic-deploy/internal/domain"
)

// TestInjection_OrchestratorRole_NonOrchestratorKey_ReturnsMergedContent verifies that
// when an agent has Role == domain.RoleOrchestrator but AgentKey is not "orchestrator"
// (e.g. "orchestrator-script"), Injection returns orchestrator-merged content, not
// shared-only content.
//
// This is the exact bug scenario: orchestrators deployed under a non-literal key
// silently receive subagent-level injection content because the gate checks
// AgentKey == "orchestrator" rather than Role == domain.RoleOrchestrator.
//
// For Claude Code HarnessConstraints:
//   - Shared content: "" (empty -- Claude Code has no shared constraint text)
//   - Orchestrator content: non-empty (skills-path and security-warning instructions)
//   - Merged result for orchestrator role: orchestrator-only content (since shared is empty)
//
// RED: FAILS until I1.1 (add Role field to InjectionRequest) and I1.2 (switch
// Injection gate from AgentKey to Role) are both complete.
func TestInjection_OrchestratorRole_NonOrchestratorKey_ReturnsMergedContent(t *testing.T) {
	mod := newModule(t)

	// Baseline: what a genuine subagent receives (shared content only, empty for Claude Code).
	subagentContent, subagentOK := mod.Injection(domain.InjectionRequest{
		Name:     "HarnessConstraints",
		AgentKey: "some-subagent",
	})
	if !subagentOK {
		t.Fatal("baseline: Injection(HarnessConstraints, some-subagent) returned ok=false; " +
			"Claude Code declares HarnessConstraints as an injection it fills (ok=true)")
	}

	// Subject under test: orchestrator-role agent with a non-literal key.
	orchContent, orchOK := mod.Injection(domain.InjectionRequest{
		Name:     "HarnessConstraints",
		AgentKey: "orchestrator-script", // not "orchestrator" -- the bug scenario
		Role:     domain.RoleOrchestrator,
	})
	if !orchOK {
		t.Fatal("Injection(HarnessConstraints, orchestrator-script, RoleOrchestrator) returned ok=false; " +
			"orchestrator-role agents must receive content (ok=true) for HarnessConstraints")
	}

	// An orchestrator-role agent must receive more than the subagent receives.
	// Claude Code's shared HarnessConstraints content is empty; the orchestrator-only
	// content is non-empty. So the merged result must differ from the (empty) subagent content.
	if orchContent == subagentContent {
		t.Errorf("orchestrator-role agent with AgentKey=%q received identical content to subagent (%q); "+
			"role-based gating must return orchestrator-merged content when Role == domain.RoleOrchestrator, "+
			"regardless of AgentKey value",
			"orchestrator-script", subagentContent)
	}

	// The merged content must include the skills-path instruction that distinguishes
	// orchestrator content from shared content for Claude Code.
	if !strings.Contains(orchContent, "cwd/.claude/skills") {
		t.Errorf("orchestrator-role HarnessConstraints content is missing the skills-path instruction\n"+
			"got:  %q\n"+
			"want: content containing \"cwd/.claude/skills\"\n\n"+
			"The orchestrator injection must be served to any agent with Role == domain.RoleOrchestrator, "+
			"not only to agents with AgentKey == \"orchestrator\"",
			orchContent)
	}
}

// TestInjection_OrchestratorRole_WithOrchestratorKey_StillReturnsMergedContent is a
// regression guard: after the role-based fix, agents with the literal "orchestrator"
// AgentKey AND Role == domain.RoleOrchestrator must continue to receive orchestrator
// content. The fix must not accidentally break the existing-key case.
//
// This test is a GREEN-phase regression guard. It passes in RED phase only if
// Role: domain.RoleOrchestrator is added to the request -- which requires I1.1 to
// add the Role field (compile RED until then).
func TestInjection_OrchestratorRole_WithOrchestratorKey_StillReturnsMergedContent(t *testing.T) {
	mod := newModule(t)

	content, ok := mod.Injection(domain.InjectionRequest{
		Name:     "HarnessConstraints",
		AgentKey: "orchestrator",
		Role:     domain.RoleOrchestrator,
	})
	if !ok {
		t.Fatal("Injection(HarnessConstraints, orchestrator, RoleOrchestrator) returned ok=false; " +
			"orchestrator must receive content (ok=true) for HarnessConstraints")
	}
	if !strings.Contains(content, "cwd/.claude/skills") {
		t.Errorf("Injection(HarnessConstraints, orchestrator, RoleOrchestrator) missing skills-path instruction\n"+
			"got: %q\nwant: content containing \"cwd/.claude/skills\"", content)
	}
}

// TestInjection_ZeroRole_ReturnsSharedContentOnly verifies the backward-compatibility
// contract: when Role is zero-value (empty string), Injection returns shared content
// only, regardless of AgentKey value. This ensures that existing callers that construct
// InjectionRequest without setting Role continue to receive the same content they always
// received (shared only).
//
// After the role-based fix:
//   - AgentKey: "orchestrator", Role: "" -> shared content only (backward compat)
//   - AgentKey: "orchestrator-script", Role: "" -> shared content only (backward compat)
//
// RED: compile-fails until I1.1 adds the Role field to InjectionRequest.
// RUNTIME: passes immediately after I1.2 (role-based switch, zero role = shared only).
func TestInjection_ZeroRole_ReturnsSharedContentOnly(t *testing.T) {
	mod := newModule(t)

	// Even with the literal "orchestrator" AgentKey, zero Role must return shared content.
	content, ok := mod.Injection(domain.InjectionRequest{
		Name:     "HarnessConstraints",
		AgentKey: "orchestrator",
		// Role is zero value (not set)
	})
	if !ok {
		t.Fatal("Injection(HarnessConstraints, orchestrator, zero-Role) returned ok=false; " +
			"Claude Code declares HarnessConstraints even when shared content is empty (ok=true)")
	}
	// Shared content for HarnessConstraints is "" (empty string) for Claude Code.
	// With zero Role, the orchestrator-only content must NOT be merged in.
	if content != "" {
		t.Errorf("Injection(HarnessConstraints, orchestrator, zero-Role) = %q, want \"\" (shared content only); "+
			"zero Role must not trigger orchestrator-merged content -- Role == domain.RoleOrchestrator is required",
			content)
	}
}
