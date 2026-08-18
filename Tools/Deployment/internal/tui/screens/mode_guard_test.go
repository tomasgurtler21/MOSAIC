package screens_test

// mode_guard_test.go asserts the final mode-list invariant for Stage 6: the mode screen
// must expose exactly 7 modes, all 7 required modes must be reachable, and neither legacy
// mode identifier ("utility-infra-only", "standalone-only") must be reachable.
//
// Three of these tests are TDD RED-phase tests that fail until the two legacy mode entries
// are removed from modeItems in the screens package. The string literals "utility-infra-only"
// and "standalone-only" are used intentionally instead of domain constants so the guard
// continues to compile after the constants are removed in the implementation phase.
// TestModeScreen_AllRequiredModesPresent passes immediately: all 7 required modes are already
// present in the list alongside the 2 legacy extras.
//
// Verified behaviours:
//
// Exactly 7 unique modes (T6.4):
//   - collectAllModes navigates through all positions and deduplicates; the count must be 7.
//
// All 7 required modes present (T6.4):
//   - Each of deploy-workspace, update-workspace, update-workflows, deploy-agents,
//     deploy-hooks, promote-to-generic, and transform-harness must appear at some position.
//
// No legacy mode identifiers reachable (T6.4):
//   - "utility-infra-only" (formerly ModeUtilityInfraOnly) must not appear at any position.
//   - "standalone-only" (formerly ModeStandaloneOnly) must not appear at any position.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// T6.4 — Mode screen exposes exactly 7 modes
// ---------------------------------------------------------------------------

// TestModeScreen_ExactlySevenModes verifies that the mode screen exposes exactly 7 unique
// modes, matching the set of modes the tool is designed to support after retiring the two
// legacy modes. Any additional mode (including the retired legacy modes) or missing required
// mode causes this test to fail.
func TestModeScreen_ExactlySevenModes(t *testing.T) {
	const wantCount = 7

	modes := collectAllModes(maxModeNavigations)

	if len(modes) != wantCount {
		t.Errorf("mode screen exposes %d unique mode(s); want exactly %d — "+
			"after retiring ModeUtilityInfraOnly and ModeStandaloneOnly the mode list "+
			"must contain exactly 7 entries: deploy-workspace, update-workspace, "+
			"update-workflows, deploy-agents, deploy-hooks, promote-to-generic, and "+
			"transform-harness; got: %v",
			len(modes), wantCount, modes)
	}
}

// ---------------------------------------------------------------------------
// T6.4 — All 7 required modes are present
// ---------------------------------------------------------------------------

// TestModeScreen_AllRequiredModesPresent verifies that each of the 7 required modes is
// reachable at some position in the mode screen. A mode that is absent cannot be selected
// by the user, making the corresponding deploy flow inaccessible.
func TestModeScreen_AllRequiredModesPresent(t *testing.T) {
	required := []domain.RunMode{
		domain.ModeDeployWorkspace,
		domain.ModeUpdateWorkspace,
		domain.ModeUpdateWorkflows,
		domain.ModeDeployAgents,
		domain.ModeDeployHooks,
		domain.ModePromoteToGeneric,
		domain.ModeTransformHarness,
	}

	modes := collectAllModes(maxModeNavigations)
	found := make(map[domain.RunMode]bool, len(modes))
	for _, m := range modes {
		found[m] = true
	}

	for _, want := range required {
		if !found[want] {
			t.Errorf("required mode %q is not reachable in the mode screen; "+
				"all 7 required modes must be present: deploy-workspace, update-workspace, "+
				"update-workflows, deploy-agents, deploy-hooks, promote-to-generic, "+
				"transform-harness",
				want)
		}
	}
}

// ---------------------------------------------------------------------------
// T6.4 — Legacy mode identifiers are not reachable
// ---------------------------------------------------------------------------

// TestModeScreen_LegacyUtilityInfraNotReachable verifies that the mode identifier
// "utility-infra-only" (the string value of the retired ModeUtilityInfraOnly constant)
// is not reachable at any position in the mode screen. String literals are used instead
// of domain constants so this test continues to compile after the constants are removed.
func TestModeScreen_LegacyUtilityInfraNotReachable(t *testing.T) {
	const legacyIdentifier = "utility-infra-only"

	modes := collectAllModes(maxModeNavigations)
	for _, m := range modes {
		if string(m) == legacyIdentifier {
			t.Errorf("legacy mode %q is still reachable in the mode screen; "+
				"ModeUtilityInfraOnly must be removed from the mode list in Stage 6 — "+
				"its behaviour has been absorbed into ModeDeployAgents",
				legacyIdentifier)
		}
	}
}

// TestModeScreen_LegacyStandaloneOnlyNotReachable verifies that the mode identifier
// "standalone-only" (the string value of the retired ModeStandaloneOnly constant)
// is not reachable at any position in the mode screen. String literals are used instead
// of domain constants so this test continues to compile after the constants are removed.
func TestModeScreen_LegacyStandaloneOnlyNotReachable(t *testing.T) {
	const legacyIdentifier = "standalone-only"

	modes := collectAllModes(maxModeNavigations)
	for _, m := range modes {
		if string(m) == legacyIdentifier {
			t.Errorf("legacy mode %q is still reachable in the mode screen; "+
				"ModeStandaloneOnly must be removed from the mode list in Stage 6 — "+
				"its behaviour has been absorbed into ModeDeployAgents",
				legacyIdentifier)
		}
	}
}
