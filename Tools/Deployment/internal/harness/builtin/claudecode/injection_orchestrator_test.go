package claudecode_test

// injection_orchestrator_test.go: Tests for the new Injection(InjectionRequest) contract
// introduced in Stage 4 (orchestrator-only injection mechanism). These tests define the
// TDD RED specification for agent-identity-aware injection resolution.
//
// Test coverage (per plan task T4.1):
//
//  (a) Subagent key returns shared content only — same as pre-Stage-4 behavior.
//  (b) Orchestrator key returns shared content merged with orchestrator-only content.
//  (c) Orchestrator key with no orchestrator-only content for a given name (both sources
//      declared-but-empty) returns ok=true with an empty string.
//  (d) Non-canonical injection names return ok=false regardless of agent key.
//  (e) Determinism — two calls with equal InjectionRequest return equal output.
//  (f) Both sources non-empty for the same name: orchestrator receives exactly
//      sharedContent + "\n\n" + orchContent (Case 3 of the design merge strategy).
//  (g) Only shared content present for a name (orchestrator-only content absent): orchestrator
//      receives shared content unmodified, with no trailing separator (Case 4).
//
// RED STATE:
//   - Case (b): FAILS until I4.2 (create HarnessInjectionsOrchestrator.md) and I4.3
//     (update claudecode.Injection to load the orchestrator file and merge content) are done.
//   - Case (f): FAILS until I4.3 adds orchInjections to module and updates NewWithMapsForTesting.
//   - Cases (a), (c), (d), (e), (g): PASS in RED phase (existing behavior satisfies these).

import (
	"strings"
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/harness/builtin/claudecode"
)

// TestInjection_SubagentKey_ReturnsSharedContentOnly verifies case (a): when AgentKey is
// not "orchestrator", Injection returns only the shared (subagent-level) content.
// For Claude Code HarnessConstraints, the shared content is empty (declared-but-empty).
// This test passes in the RED phase: existing behavior is preserved for non-orchestrator keys.
func TestInjection_SubagentKey_ReturnsSharedContentOnly(t *testing.T) {
	mod := newModule(t)

	// HarnessConstraints is declared-but-empty for Claude Code (no shared constraint content).
	content, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "some-subagent"})
	if !ok {
		t.Fatal("Injection(HarnessConstraints, some-subagent) returned ok=false; Claude Code declares this injection as empty (ok=true)")
	}
	// Subagent must receive only shared content, which is empty for HarnessConstraints.
	if content != "" {
		t.Errorf("subagent should receive shared content only (empty string for HarnessConstraints); got %q", content)
	}
}

// TestInjection_SubagentKey_EmptyAgentKey_ReturnsSharedContentOnly verifies that an empty
// AgentKey (equivalent to a non-orchestrator key) also returns only shared content.
func TestInjection_SubagentKey_EmptyAgentKey_ReturnsSharedContentOnly(t *testing.T) {
	mod := newModule(t)

	content, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: ""})
	if !ok {
		t.Fatal("Injection(HarnessConstraints, \"\") returned ok=false; Claude Code declares this injection as empty (ok=true)")
	}
	if content != "" {
		t.Errorf("empty agent key should receive shared content only (empty string for HarnessConstraints); got %q", content)
	}
}

// TestInjection_OrchestratorKey_ReceivesMergedContent verifies case (b): when AgentKey is
// "orchestrator", Injection returns shared content merged with the orchestrator-only content
// from HarnessInjectionsOrchestrator.md.
//
// For Claude Code HarnessConstraints:
//   - Shared content (HarnessInjections.md): "" (empty, Claude Code has no shared constraint)
//   - Orchestrator-only content (HarnessInjectionsOrchestrator.md): the skills-path and
//     Task-tool-fields instructions migrated from the hand-authored orchestrator file.
//
// Since shared is empty, merged result = orchestrator-only content. The skills-path
// instruction ("cwd/.claude/skills") serves as the distinguishing marker that the
// orchestrator file was loaded and merged.
//
// RED: FAILS until I4.2 (create HarnessInjectionsOrchestrator.md for Claude Code) and
// I4.3 (update claudecode.Injection to embed and merge the orchestrator file) are complete.
func TestInjection_OrchestratorKey_ReceivesMergedContent(t *testing.T) {
	mod := newModule(t)

	content, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "orchestrator"})
	if !ok {
		t.Fatal("Injection(HarnessConstraints, orchestrator) returned ok=false; orchestrator must receive content for HarnessConstraints")
	}
	// The orchestrator-only content must include the skills-path instruction.
	// This is the content currently hand-authored at .claude/agents/orchestrator.md lines 504-507.
	if !strings.Contains(content, "cwd/.claude/skills") {
		t.Errorf("orchestrator HarnessConstraints content is missing the skills-path instruction\ngot:  %q\nwant: content containing \"cwd/.claude/skills\"", content)
	}
}

// TestInjection_OrchestratorKey_DeclaredButBothEmpty_ReturnsOkTrueEmptyString verifies case (c):
// when an injection name is declared in HarnessInjections.md but both the shared content and
// the orchestrator-only content are empty (e.g. LanguagePatterns for Claude Code), the
// orchestrator receives ok=true with an empty string — not ok=false.
//
// This is the "declared-but-empty" scenario (Case 6 of the design merge strategy). It is
// distinct from Case 4 (shared non-empty, orch absent), which is tested separately in
// TestInjection_OrchestratorKey_SharedContentOnly_NoOrchContent_ReturnsSharedUnmodified.
func TestInjection_OrchestratorKey_DeclaredButBothEmpty_ReturnsOkTrueEmptyString(t *testing.T) {
	mod := newModule(t)

	// LanguagePatterns is declared-but-empty in HarnessInjections.md and has no
	// orchestrator-specific content in HarnessInjectionsOrchestrator.md.
	content, ok := mod.Injection(domain.InjectionRequest{Name: "LanguagePatterns", AgentKey: "orchestrator"})
	if !ok {
		t.Fatal("Injection(LanguagePatterns, orchestrator) returned ok=false; declared-but-empty injection must return ok=true")
	}
	// No shared content and no orchestrator-only content → result is empty string.
	if content != "" {
		t.Errorf("orchestrator LanguagePatterns should be empty (no shared or orchestrator-only content declared); got %q", content)
	}
}

// TestInjection_NonCanonicalName_ReturnsFalse_ForAllAgentKeys verifies case (d):
// non-canonical injection names return ok=false regardless of the agent key. This preserves
// the existing contracttest invariant with the new InjectionRequest signature.
func TestInjection_NonCanonicalName_ReturnsFalse_ForAllAgentKeys(t *testing.T) {
	mod := newModule(t)

	nonCanonical := []string{
		"NotAnInjection",
		"",
		"random-garbage-xyz",
		"harness_constraints", // wrong casing — must not match "HarnessConstraints"
	}
	agentKeys := []string{"", "some-subagent", "orchestrator"}

	for _, name := range nonCanonical {
		for _, agentKey := range agentKeys {
			req := domain.InjectionRequest{Name: name, AgentKey: agentKey}
			_, ok := mod.Injection(req)
			if ok {
				t.Errorf("Injection(%q, agentKey=%q) returned ok=true; non-canonical names must return ok=false for all agent keys", name, agentKey)
			}
		}
	}
}

// TestInjection_Deterministic verifies case (e): two calls with equal InjectionRequest
// inputs return equal outputs, for all canonical injection names and all agent keys.
func TestInjection_Deterministic(t *testing.T) {
	mod := newModule(t)

	agentKeys := []string{"", "some-subagent", "orchestrator"}
	for _, name := range docformat.CanonicalInjections {
		for _, agentKey := range agentKeys {
			req := domain.InjectionRequest{Name: name, AgentKey: agentKey}
			c1, ok1 := mod.Injection(req)
			c2, ok2 := mod.Injection(req)
			if ok1 != ok2 || c1 != c2 {
				t.Errorf("Injection(%q, agentKey=%q) non-deterministic: first=(%q,%v), second=(%q,%v)",
					name, agentKey, c1, ok1, c2, ok2)
			}
		}
	}
}

// TestInjection_OrchestratorKey_BothContentSources_ReturnsMergedWithSeparator verifies case (f):
// Case 3 of the design merge strategy — when both the shared (subagent-level) content and the
// orchestrator-only content are non-empty for the same injection name, the orchestrator receives
// exactly sharedContent + "\n\n" + orchContent.
//
// This test directly verifies the blank-line separator format specified in the design. An
// implementation that uses "\n" instead of "\n\n", that reverses the concatenation order, or
// that omits the separator would be caught here.
//
// Uses NewWithMapsForTesting to supply known content so the test does not depend on the actual
// HarnessInjections.md or HarnessInjectionsOrchestrator.md content.
//
// RED: FAILS until I4.3 adds orchInjections to the module struct and NewWithMapsForTesting is
// updated to store orchContent in that field.
func TestInjection_OrchestratorKey_BothContentSources_ReturnsMergedWithSeparator(t *testing.T) {
	sharedContent := "shared constraint content"
	orchContent := "orchestrator-only constraint content"

	mod := claudecode.NewWithMapsForTesting(t,
		map[string]string{"HarnessConstraints": sharedContent},
		map[string]string{"HarnessConstraints": orchContent},
	)

	content, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "orchestrator"})
	if !ok {
		t.Fatal("Injection(HarnessConstraints, orchestrator) returned ok=false; must return ok=true when content exists in either source")
	}
	want := sharedContent + "\n\n" + orchContent
	if content != want {
		t.Errorf("Injection(HarnessConstraints, orchestrator) merge result does not match expected format\ngot:  %q\nwant: %q\n\nWhen both shared and orchestrator-only content are non-empty, they must be joined with a blank-line separator (\"\\n\\n\"), shared content first.", content, want)
	}
}

// TestInjection_OrchestratorKey_SharedContentOnly_NoOrchContent_ReturnsSharedUnmodified
// verifies case (g): Case 4 of the design merge strategy — when shared content is non-empty for
// a given injection name but no orchestrator-only content exists for that name, the orchestrator
// receives the shared content unchanged, with no trailing separator appended.
//
// An implementation that unconditionally appends "\n\n" (even when orchestrator content is
// absent) would fail this test.
//
// Uses NewWithMapsForTesting to supply known shared content. No orchestrator content is provided
// for the tested injection name.
//
// This test PASSES in the RED phase: the current stub returns shared content for all agent keys,
// which is the correct Case 4 behavior. It becomes a regression guard once I4.3 is implemented.
func TestInjection_OrchestratorKey_SharedContentOnly_NoOrchContent_ReturnsSharedUnmodified(t *testing.T) {
	sharedContent := "shared constraint content"

	mod := claudecode.NewWithMapsForTesting(t,
		map[string]string{"HarnessConstraints": sharedContent},
		nil, // no orchestrator-only content for HarnessConstraints
	)

	content, ok := mod.Injection(domain.InjectionRequest{Name: "HarnessConstraints", AgentKey: "orchestrator"})
	if !ok {
		t.Fatal("Injection(HarnessConstraints, orchestrator) returned ok=false; must return ok=true when shared content exists")
	}
	if content != sharedContent {
		t.Errorf("Injection(HarnessConstraints, orchestrator) modified shared content when no orchestrator-only content exists\ngot:  %q\nwant: %q\n\nWhen only shared content is present, the orchestrator must receive it unmodified (no trailing separator).", content, sharedContent)
	}
}
