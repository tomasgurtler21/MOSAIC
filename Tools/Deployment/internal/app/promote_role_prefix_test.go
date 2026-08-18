package app

// promote_role_prefix_test.go verifies that buildGenericAgent correctly handles the role
// field when promoting a deployed harness-only agent to a generic source file.
//
// The promote direction contract for the role field:
//   - The promoted generic file carries the bare "role" key with the correct role value.
//   - The deployed "mosaic_role" key does not survive into the generic output — it is
//     removed before the generic "role" is written, so no prefixed key appears in a
//     generic source.
//   - The correct role is produced regardless of whether the source file carries the role
//     under the legacy bare key or the migrated mosaic_role key. Neither must silently
//     fall back to subagent when the role is orchestrator, utility, or standalone.
//   - Promote writes a bare "role" key, not "mosaic_role", so the promoted generic file
//     can be used as a catalog source immediately.
//
// The "not fall back to subagent" invariant is tested at two levels:
//   1. eligibleHarnessOnly meta.Role reading (see mosaic_role_read_test.go, T3.3): this is
//      the service-layer path from file → meta.Role → PromoteInput.Role.
//   2. buildGenericAgent output (this file, T3.4): given a source carrying "mosaic_role",
//      the output must not carry "mosaic_role", and must carry bare "role" with the correct
//      value when PromoteInput.Role is supplied.
//
// Tests that assert "mosaic_role is absent from output" FAIL until I3.5 removes the prefixed
// key in the promote path (either via PromoteDropKeys including mosaic_role, or via an
// explicit removal like the id field's ReadOrder removal). Tests that check the bare "role"
// value currently PASS or FAIL depending on whether mosaic_role leaks into the output.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// minimalEligibleMosaicRoleSource returns a deployed source carrying all MOSAIC stamps
// under their prefixed names, including "mosaic_role: orchestrator". This is what a
// fully-migrated deployed file that was deployed as an orchestrator looks like.
func minimalEligibleMosaicRoleSource(role string) []byte {
	return []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_injections_version: \"1.2.0\"\n" +
		"version: \"1.5.0\"\n" +
		"mosaic_id: \"55\"\n" +
		"name: role-promote-test-agent\n" +
		"description: Agent for promote role prefix tests\n" +
		"mosaic_role: " + role + "\n" +
		"recommended_tier: MEDIUM\n" +
		"tier_rationale: standard capability\n" +
		"tools: [file_read, file_write]\n" +
		"required_skills: []\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the RolePromoteTestAgent.\n" +
		"</Identity>\n")
}

// minimalEligibleBareRoleSource returns a deployed source carrying MOSAIC stamps under their
// legacy unprefixed names, including the bare "role: orchestrator" key. This is what a
// pre-migration deployed file looks like.
func minimalEligibleBareRoleSource(role string) []byte {
	return []byte("---\n" +
		"transform_version: \"3.0.0\"\n" +
		"injections_version: \"1.2.0\"\n" +
		"version: \"1.5.0\"\n" +
		"id: \"55\"\n" +
		"name: role-promote-test-agent\n" +
		"description: Agent for promote role prefix tests\n" +
		"role: " + role + "\n" +
		"recommended_tier: MEDIUM\n" +
		"tier_rationale: standard capability\n" +
		"tools: [file_read, file_write]\n" +
		"required_skills: []\n" +
		"---\n" +
		"<Identity type=\"core\">\n" +
		"You are the RolePromoteTestAgent.\n" +
		"</Identity>\n")
}

// promoteRoleSource calls buildGenericAgent on src with the given role, id, and key.
// t.Fatal is called if promotion fails.
func promoteRoleSource(t *testing.T, src []byte, role domain.AgentRole) []byte {
	t.Helper()
	out, err := buildGenericAgent(PromoteInput{
		Source:    src,
		NumericID: "55",
		Key:       "role-promote-test-agent",
		Role:      role,
	})
	if err != nil {
		t.Fatalf("buildGenericAgent failed: %v", err)
	}
	return out
}

// ---------------------------------------------------------------------------
// T3.4 — Promoted output carries bare role and no prefixed key
// ---------------------------------------------------------------------------

// TestPromoteRole_MosaicRoleSource_NoPrefixedKeyInOutput verifies that when the deployed
// source carries "mosaic_role: orchestrator", the promoted generic output does not carry
// "mosaic_role". The prefixed deployed key must not survive into a generic source.
//
// FAILS until I3.5 removes mosaic_role from the promoted output (either by adding it to
// PromoteDropKeys or by an explicit ReadOrder removal like the id field uses).
func TestPromoteRole_MosaicRoleSource_NoPrefixedKeyInOutput(t *testing.T) {
	src := minimalEligibleMosaicRoleSource("orchestrator")
	out := promoteRoleSource(t, src, domain.RoleOrchestrator)

	keys := parseFrontmatterKeys(t, out)
	for _, k := range keys {
		if k == "mosaic_role" {
			t.Error("promoted generic output carries \"mosaic_role\"; the prefixed deployed key must not appear in a generic source file")
		}
	}
}

// TestPromoteRole_BareRoleSource_NoPrefixedKeyInOutput verifies that when the deployed
// source carries the legacy bare "role: orchestrator" key, the promoted output also does
// not carry "mosaic_role". This confirms the promote path never introduces the prefixed key.
// This test PASSES both before and after the implementation.
func TestPromoteRole_BareRoleSource_NoPrefixedKeyInOutput(t *testing.T) {
	src := minimalEligibleBareRoleSource("orchestrator")
	out := promoteRoleSource(t, src, domain.RoleOrchestrator)

	keys := parseFrontmatterKeys(t, out)
	for _, k := range keys {
		if k == "mosaic_role" {
			t.Error("promoted generic output carries \"mosaic_role\" even though source used legacy bare role; the promote path must not introduce the prefixed key")
		}
	}
}

// TestPromoteRole_MosaicRoleSource_BareRoleInOutput verifies that the promoted output
// carries the bare "role" key. The generic source must always use the bare key; the prefixed
// form is only for deployed files. This test PASSES both before and after the implementation.
func TestPromoteRole_MosaicRoleSource_BareRoleInOutput(t *testing.T) {
	src := minimalEligibleMosaicRoleSource("orchestrator")
	out := promoteRoleSource(t, src, domain.RoleOrchestrator)

	if _, ok := parseFrontmatterField(t, out, "role"); !ok {
		t.Error("promoted generic output does not carry \"role\"; the promote path must always write the bare role key in the generic output")
	}
}

// ---------------------------------------------------------------------------
// T3.4 — Role value correctness for each role
// ---------------------------------------------------------------------------

// TestPromoteRole_OrchestratorRole_CorrectValueInOutput verifies that an orchestrator role
// produces a promoted output with "role: orchestrator". PASSES both before and after;
// this pins the value contract for non-subagent roles.
func TestPromoteRole_OrchestratorRole_CorrectValueInOutput(t *testing.T) {
	// Test with both source forms to confirm both produce the same result.
	for _, tc := range []struct {
		name string
		src  []byte
	}{
		{"mosaic_role_source", minimalEligibleMosaicRoleSource("orchestrator")},
		{"bare_role_source", minimalEligibleBareRoleSource("orchestrator")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := promoteRoleSource(t, tc.src, domain.RoleOrchestrator)
			v, ok := parseFrontmatterField(t, out, "role")
			if !ok {
				t.Fatal("promoted output does not carry \"role\" key")
			}
			if v != "orchestrator" {
				t.Errorf("role = %q, want \"orchestrator\"; orchestrator role must survive promote correctly", v)
			}
		})
	}
}

// TestPromoteRole_UtilityRole_CorrectValueInOutput verifies that a utility role produces
// "role: utility" in the promoted output.
func TestPromoteRole_UtilityRole_CorrectValueInOutput(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  []byte
	}{
		{"mosaic_role_source", minimalEligibleMosaicRoleSource("utility")},
		{"bare_role_source", minimalEligibleBareRoleSource("utility")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := promoteRoleSource(t, tc.src, domain.RoleUtility)
			v, ok := parseFrontmatterField(t, out, "role")
			if !ok {
				t.Fatal("promoted output does not carry \"role\" key")
			}
			if v != "utility" {
				t.Errorf("role = %q, want \"utility\"; utility role must survive promote correctly", v)
			}
		})
	}
}

// TestPromoteRole_StandaloneRole_CorrectValueInOutput verifies that a standalone role
// produces "role: standalone" in the promoted output.
func TestPromoteRole_StandaloneRole_CorrectValueInOutput(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  []byte
	}{
		{"mosaic_role_source", minimalEligibleMosaicRoleSource("standalone")},
		{"bare_role_source", minimalEligibleBareRoleSource("standalone")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := promoteRoleSource(t, tc.src, domain.RoleStandalone)
			v, ok := parseFrontmatterField(t, out, "role")
			if !ok {
				t.Fatal("promoted output does not carry \"role\" key")
			}
			if v != "standalone" {
				t.Errorf("role = %q, want \"standalone\"; standalone role must survive promote correctly", v)
			}
		})
	}
}

// TestPromoteRole_AllRoles_NoMosaicRoleInOutput verifies across all four role values that
// "mosaic_role" never appears in the promoted generic output, regardless of which key form
// the source used. The table covers both mosaic_role and bare role sources for each value.
//
// The mosaic_role-source rows FAIL until I3.5 removes the prefixed key from the promote path.
func TestPromoteRole_AllRoles_NoMosaicRoleInOutput(t *testing.T) {
	cases := []struct {
		name     string
		src      []byte
		role     domain.AgentRole
	}{
		{"subagent_mosaic_source", minimalEligibleMosaicRoleSource("subagent"), domain.RoleSubagent},
		{"orchestrator_mosaic_source", minimalEligibleMosaicRoleSource("orchestrator"), domain.RoleOrchestrator},
		{"utility_mosaic_source", minimalEligibleMosaicRoleSource("utility"), domain.RoleUtility},
		{"standalone_mosaic_source", minimalEligibleMosaicRoleSource("standalone"), domain.RoleStandalone},
		{"subagent_bare_source", minimalEligibleBareRoleSource("subagent"), domain.RoleSubagent},
		{"orchestrator_bare_source", minimalEligibleBareRoleSource("orchestrator"), domain.RoleOrchestrator},
		{"utility_bare_source", minimalEligibleBareRoleSource("utility"), domain.RoleUtility},
		{"standalone_bare_source", minimalEligibleBareRoleSource("standalone"), domain.RoleStandalone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := promoteRoleSource(t, tc.src, tc.role)
			keys := parseFrontmatterKeys(t, out)
			for _, k := range keys {
				if k == "mosaic_role" {
					t.Errorf("%s: promoted output carries \"mosaic_role\"; the prefixed deployed key must not appear in a generic source", tc.name)
				}
			}
		})
	}
}
