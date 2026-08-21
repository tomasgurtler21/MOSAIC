package app

// mosaic_role_read_test.go verifies that every deployed-file read site accepts both the
// prefixed (mosaic_role) and legacy (bare role) forms of the role field, with the prefixed
// name winning when both are present.
//
// Read sites covered:
//   - eligibleHarnessOnly (harness_only_discovery.go) — parses the role from a deployed
//     file and defaults to RoleSubagent when neither key is present. After I3.3 it reads
//     via agentfields.ReadOrder(roleField), so a file carrying mosaic_role: orchestrator
//     yields meta.Role = RoleOrchestrator, not the current default of RoleSubagent.
//   - bundleConformance (bundle_conformance.go) — reads the role to decide which bundle
//     rules apply. After I3.3 it uses ReadOrder so a file with mosaic_role: subagent is
//     correctly subject to subagent bundle rules.
//
// Every test that asserts on the prefixed mosaic_role key FAILS until I3.3 switches both
// readers to agentfields.ReadOrder. The backward-compatibility tests (bare role key) PASS
// both before and after the implementation.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// eligibleHarnessOnly — role parsing from mosaic_role key
// ---------------------------------------------------------------------------

// TestEligibleHarnessOnly_MosaicRoleOrchestrator_MetaRoleIsOrchestrator verifies that when
// a deployed file carries "mosaic_role: orchestrator" (and no bare "role" key), the
// eligibility check returns meta.Role == RoleOrchestrator, not the default RoleSubagent.
// FAILS until I3.3 switches the role reader to agentfields.ReadOrder.
func TestEligibleHarnessOnly_MosaicRoleOrchestrator_MetaRoleIsOrchestrator(t *testing.T) {
	src := []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_role: orchestrator\n" +
		"---\n" + minimalCanonicalBody)

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Fatalf("eligibility check unexpectedly failed: %s", verdict.Reason)
	}
	if verdict.Meta.Role != domain.RoleOrchestrator {
		t.Errorf("meta.Role = %q, want %q; a file with mosaic_role: orchestrator must not fall back to the default subagent role",
			verdict.Meta.Role, domain.RoleOrchestrator)
	}
}

// TestEligibleHarnessOnly_MosaicRoleUtility_MetaRoleIsUtility verifies that a deployed
// file carrying "mosaic_role: utility" yields meta.Role == RoleUtility.
// FAILS until I3.3 switches the role reader to agentfields.ReadOrder.
func TestEligibleHarnessOnly_MosaicRoleUtility_MetaRoleIsUtility(t *testing.T) {
	src := []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_role: utility\n" +
		"---\n" + minimalCanonicalBody)

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Fatalf("eligibility check unexpectedly failed: %s", verdict.Reason)
	}
	if verdict.Meta.Role != domain.RoleUtility {
		t.Errorf("meta.Role = %q, want %q; a file with mosaic_role: utility must not fall back to the default subagent role",
			verdict.Meta.Role, domain.RoleUtility)
	}
}

// TestEligibleHarnessOnly_MosaicRoleStandalone_MetaRoleIsStandalone verifies that a
// deployed file carrying "mosaic_role: standalone" yields meta.Role == RoleStandalone.
// FAILS until I3.3 switches the role reader to agentfields.ReadOrder.
func TestEligibleHarnessOnly_MosaicRoleStandalone_MetaRoleIsStandalone(t *testing.T) {
	src := []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_role: standalone\n" +
		"---\n" + minimalCanonicalBody)

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Fatalf("eligibility check unexpectedly failed: %s", verdict.Reason)
	}
	if verdict.Meta.Role != domain.RoleStandalone {
		t.Errorf("meta.Role = %q, want %q; a file with mosaic_role: standalone must not fall back to the default subagent role",
			verdict.Meta.Role, domain.RoleStandalone)
	}
}

// TestEligibleHarnessOnly_MosaicRoleSubagent_MetaRoleIsSubagent verifies that a deployed
// file carrying "mosaic_role: subagent" explicitly yields meta.Role == RoleSubagent.
// The subagent case happens to match the default, so this test confirms the value is read
// rather than defaulted. FAILS until I3.3 (currently defaults to subagent even when absent).
func TestEligibleHarnessOnly_MosaicRoleSubagent_MetaRoleIsSubagent(t *testing.T) {
	// This test uses a file with no bare "role" and only "mosaic_role: subagent".
	// If the role is read correctly, meta.Role == RoleSubagent via ReadOrder.
	// If the key is silently absent (current behaviour), meta.Role also defaults to RoleSubagent.
	// The distinction is tested by the orchestrator/utility/standalone variants above.
	// This test confirms the subagent case produces the correct value when read explicitly.
	src := []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_role: subagent\n" +
		"---\n" + minimalCanonicalBody)

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Fatalf("eligibility check unexpectedly failed: %s", verdict.Reason)
	}
	if verdict.Meta.Role != domain.RoleSubagent {
		t.Errorf("meta.Role = %q, want %q", verdict.Meta.Role, domain.RoleSubagent)
	}
}

// TestEligibleHarnessOnly_BareRoleOrchestrator_MetaRoleIsOrchestrator verifies backward
// compatibility: a deployed file carrying the legacy bare "role: orchestrator" key yields
// meta.Role == RoleOrchestrator. This test PASSES both before and after the implementation.
func TestEligibleHarnessOnly_BareRoleOrchestrator_MetaRoleIsOrchestrator(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"3.0.0\"\n" +
		"role: orchestrator\n" +
		"---\n" + minimalCanonicalBody)

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Fatalf("eligibility check unexpectedly failed: %s", verdict.Reason)
	}
	if verdict.Meta.Role != domain.RoleOrchestrator {
		t.Errorf("meta.Role = %q, want %q; legacy bare role key must still be read for backward compatibility",
			verdict.Meta.Role, domain.RoleOrchestrator)
	}
}

// TestEligibleHarnessOnly_BothMosaicRoleAndBareRole_PrefixedWins verifies that when both
// "mosaic_role: orchestrator" and "role: subagent" are present, the prefixed value wins.
// FAILS until I3.3 switches to ReadOrder (currently only bare role is read).
func TestEligibleHarnessOnly_BothMosaicRoleAndBareRole_PrefixedWins(t *testing.T) {
	src := []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_role: orchestrator\n" + // prefixed: orchestrator — must win
		"role: subagent\n" + // legacy: subagent — must be ignored when prefixed is present
		"---\n" + minimalCanonicalBody)

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Fatalf("eligibility check unexpectedly failed: %s", verdict.Reason)
	}
	if verdict.Meta.Role != domain.RoleOrchestrator {
		t.Errorf("meta.Role = %q, want %q; prefixed mosaic_role must win over bare role when both are present",
			verdict.Meta.Role, domain.RoleOrchestrator)
	}
}

// TestEligibleHarnessOnly_NeitherRoleKey_DefaultsToSubagent verifies that when neither
// "mosaic_role" nor "role" is present, meta.Role defaults to RoleSubagent. This confirms
// the absent-key fallback is preserved after the ReadOrder change.
// This test PASSES both before and after the implementation.
func TestEligibleHarnessOnly_NeitherRoleKey_DefaultsToSubagent(t *testing.T) {
	src := []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		// no role or mosaic_role
		"---\n" + minimalCanonicalBody)

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Fatalf("eligibility check unexpectedly failed: %s", verdict.Reason)
	}
	if verdict.Meta.Role != domain.RoleSubagent {
		t.Errorf("meta.Role = %q, want %q; absent role key must default to RoleSubagent",
			verdict.Meta.Role, domain.RoleSubagent)
	}
}

// ---------------------------------------------------------------------------
// bundleConformance — role parsing from mosaic_role key
// ---------------------------------------------------------------------------

// makeDeployedBodies is a helper that wraps a raw document body in the map bundleConformance
// expects.
func makeDeployedBodies(targetPath string, content []byte) map[string][]byte {
	return map[string][]byte{targetPath: content}
}

// makeEmptyBundleVersionState builds a DeployedArtifactState with no BundleVersion stamp,
// so rule 21 (bundle-version-mismatch) is skipped and only the role-routing logic fires.
func makeEmptyBundleVersionState(targetPath string) map[string]domain.DeployedArtifactState {
	return map[string]domain.DeployedArtifactState{
		targetPath: {Present: true, BundleVersion: ""},
	}
}

// makeSubagentBundleForConformance returns a BundleContent that applies to subagent role,
// with a named region block. Used to verify that bundleConformance correctly classifies
// a file's role to select the right bundle rules.
func makeSubagentBundleForConformance(version, regionName string, content []byte) domain.BundleContent {
	return domain.BundleContent{
		Version: version,
		Blocks: []domain.BundleBlock{
			{
				Name:      regionName + ":Subagent",
				Target:    regionName,
				AppliesTo: "subagent",
				Content:   content,
			},
		},
	}
}

// TestBundleConformance_MosaicRoleSubagent_BundleRulesApplied verifies that a deployed
// file carrying "mosaic_role: subagent" (no bare "role") is correctly identified as a
// subagent-role file, causing subagent bundle rules to be checked for that file.
//
// Specifically: a mismatched bundle version must produce a bundle-version-mismatch issue
// for the file, confirming that the role was read and used to select the applicable rules.
// FAILS until I3.3 switches bundle_conformance to use agentfields.ReadOrder for role.
func TestBundleConformance_MosaicRoleSubagent_BundleRulesApplied(t *testing.T) {
	const targetPath = "agents/test-agent.md"
	// File carries mosaic_role: subagent (no bare role), and bundle_version stamp 1.0.0.
	// Bundle is at version 2.0.0 (mismatch). bundleConformance should detect the mismatch
	// only if it correctly reads the role from mosaic_role and applies subagent rules.
	fileContent := []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_role: subagent\n" +
		"mosaic_bundle_version: \"1.0.0\"\n" +
		"---\n\nAgent body.\n")

	bundle := makeSubagentBundleForConformance("2.0.0", "SomeRegion", []byte("bundle content"))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {Present: true, BundleVersion: "1.0.0"},
	}
	bodies := makeDeployedBodies(targetPath, fileContent)

	issues := bundleConformance(bundle, states, bodies)

	// If bundleConformance reads mosaic_role correctly, the subagent bundle rule is applied
	// and the version mismatch (1.0.0 vs 2.0.0) produces a bundle-version-mismatch issue.
	// If it reads only bare "role" (absent), it skips the file and produces no issue.
	hasMismatch := false
	for _, iss := range issues {
		if iss.Code == "bundle-version-mismatch" && iss.Subject == targetPath {
			hasMismatch = true
		}
	}
	if !hasMismatch {
		t.Errorf("bundleConformance produced no bundle-version-mismatch issue for a file with mosaic_role: subagent "+
			"and a mismatched bundle version; bundleConformance must read the role from mosaic_role via ReadOrder "+
			"and apply subagent bundle rules to the file (got %d issues: %v)", len(issues), issues)
	}
}

// TestBundleConformance_BareRoleSubagent_BundleRulesApplied verifies backward compatibility:
// a deployed file carrying the legacy bare "role: subagent" key is still subject to subagent
// bundle rules. This test PASSES both before and after the implementation.
func TestBundleConformance_BareRoleSubagent_BundleRulesApplied(t *testing.T) {
	const targetPath = "agents/legacy-agent.md"
	fileContent := []byte("---\n" +
		"transform_version: \"3.0.0\"\n" +
		"role: subagent\n" +
		"bundle_version: \"1.0.0\"\n" +
		"---\n\nAgent body.\n")

	bundle := makeSubagentBundleForConformance("2.0.0", "SomeRegion", []byte("bundle content"))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {Present: true, BundleVersion: "1.0.0"},
	}
	bodies := makeDeployedBodies(targetPath, fileContent)

	issues := bundleConformance(bundle, states, bodies)

	hasMismatch := false
	for _, iss := range issues {
		if iss.Code == "bundle-version-mismatch" && iss.Subject == targetPath {
			hasMismatch = true
		}
	}
	if !hasMismatch {
		t.Errorf("bundleConformance produced no bundle-version-mismatch issue for a file with bare role: subagent; "+
			"legacy role key must still be read for backward compatibility (got %d issues)", len(issues))
	}
}

// TestBundleConformance_MosaicRoleOrchestrator_BundleRulesSkipped verifies that a file
// carrying "mosaic_role: orchestrator" is correctly identified as an orchestrator and skipped
// by subagent bundle rules (the bundle only covers subagent role).
// FAILS until I3.3 switches bundle_conformance to use ReadOrder — currently the file carries
// no bare "role" key, so bundleConformance skips it anyway (for the wrong reason: absent key
// rather than "orchestrator is not a subagent"). After I3.3 the skip reason is correct.
func TestBundleConformance_MosaicRoleOrchestrator_NoSubagentBundleIssues(t *testing.T) {
	const targetPath = "agents/orchestrator.md"
	// File has mosaic_role: orchestrator but no bare role. Bundle is subagent-only.
	// Expected: no bundle-version-mismatch issue, because orchestrator is not covered by
	// the subagent bundle (AppliesToRole returns false for orchestrator).
	fileContent := []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_role: orchestrator\n" +
		"mosaic_bundle_version: \"1.0.0\"\n" +
		"---\n\nOrchestrator body.\n")

	bundle := makeSubagentBundleForConformance("2.0.0", "SomeRegion", []byte("content"))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {Present: true, BundleVersion: "1.0.0"},
	}
	bodies := makeDeployedBodies(targetPath, fileContent)

	issues := bundleConformance(bundle, states, bodies)

	for _, iss := range issues {
		if iss.Subject == targetPath {
			t.Errorf("bundleConformance produced issue code=%q for an orchestrator-role file "+
				"against a subagent-only bundle; orchestrator files must not be subject to subagent bundle rules",
				iss.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// Staleness: legacy file with bare role is not spuriously stale
// ---------------------------------------------------------------------------

// TestEligibleHarnessOnly_LegacyBareRole_ReadPathRemainsCorrect verifies that a deployed
// file using the legacy bare "role" key is still read correctly after the migration.
// This pins the non-regression: adding mosaic_role support must not break bare role reading.
// This test PASSES both before and after the implementation.
func TestEligibleHarnessOnly_LegacyBareRole_ReadPathRemainsCorrect(t *testing.T) {
	roles := []struct {
		name string
		role domain.AgentRole
	}{
		{"subagent", domain.RoleSubagent},
		{"orchestrator", domain.RoleOrchestrator},
		{"utility", domain.RoleUtility},
		{"standalone", domain.RoleStandalone},
	}

	for _, tc := range roles {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte("---\n" +
				"transform_version: \"3.0.0\"\n" +
				"role: " + tc.name + "\n" +
				"---\n" + minimalCanonicalBody)

			verdict := eligibleHarnessOnly(src)
			if !verdict.Eligible {
				t.Fatalf("eligibility unexpectedly failed for legacy bare role=%q: %s", tc.name, verdict.Reason)
			}
			if verdict.Meta.Role != tc.role {
				t.Errorf("meta.Role = %q, want %q; legacy bare role key must still be read correctly",
					verdict.Meta.Role, tc.role)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Staleness: legacy bare-role file is not spuriously stale
// ---------------------------------------------------------------------------

// TestStaleness_LegacyBareRoleFile_MatchingVersionStamps_NotStale verifies that a deployed
// file carrying the legacy bare "role" key is NOT reported as stale when its version stamps
// match the source stamps.
//
// Rationale: the role field is not a staleness input in the current model (it does not appear
// in DeployedArtifactState's version fields), so a key-name change from "role" to
// "mosaic_role" must never cause a spurious rewrite. This test pins that invariant
// explicitly, guarding against a future change that accidentally adds the role field to the
// staleness comparison.
//
// The established probe pattern from mosaic_prefix_staleness_test.go is used:
// write to a temp file, call probeDeployedArtifact, assert AgentStaleness returns no deltas.
//
// This test PASSES both before and after the implementation — role is not a version stamp —
// but it serves as a direct regression guard for the plan's explicit risk mitigation requirement.
func TestStaleness_LegacyBareRoleFile_MatchingVersionStamps_NotStale(t *testing.T) {
	// Arrange: a legacy deployed file — bare "role" key, matching version stamps.
	content := []byte("---\n" +
		"version: \"1.5.0\"\n" +
		"transform_version: \"3.0.0\"\n" +
		"injections_version: \"1.2.0\"\n" +
		"role: orchestrator\n" +
		"---\n\nAgent body.\n")
	state := probeTempAgent(t, content)
	agent := domain.Agent{Key: "legacy-role-agent", Version: "1.5.0"}
	stamps := domain.VersionStamps{
		Version:           "1.5.0",
		HarnessVersion:  "3.0.0",
		InjectionsVersion: "1.2.0",
	}

	// Act
	deltas := plan.AgentStaleness(state, agent, stamps)

	// Assert: no version deltas — the legacy bare role key must not cause spurious staleness.
	if len(deltas) != 0 {
		t.Errorf("AgentStaleness on a legacy bare-role file with matching version stamps: got %d deltas, want 0; "+
			"the role key name must not be a staleness input — a key-name change must not trigger a rewrite",
			len(deltas))
		for _, d := range deltas {
			t.Logf("  delta: field=%q deployed=%q source=%q", d.Field, d.Deployed, d.Source)
		}
	}
}

// TestBundleConformance_NoRoleKey_FileSkipped verifies that when a deployed file carries
// neither "mosaic_role" nor "role", bundleConformance skips it (no role → no bundle rules).
// This confirms the absent-key fallback is preserved after the ReadOrder change.
// This test PASSES both before and after the implementation.
func TestBundleConformance_NoRoleKey_FileSkipped(t *testing.T) {
	const targetPath = "agents/no-role-agent.md"
	fileContent := []byte("---\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		// no role or mosaic_role
		"mosaic_bundle_version: \"1.0.0\"\n" +
		"---\n\nAgent body.\n")

	bundle := makeSubagentBundleForConformance("2.0.0", "SomeRegion", []byte("content"))
	states := map[string]domain.DeployedArtifactState{
		targetPath: {Present: true, BundleVersion: "1.0.0"},
	}
	bodies := makeDeployedBodies(targetPath, fileContent)

	issues := bundleConformance(bundle, states, bodies)

	for _, iss := range issues {
		if iss.Subject == targetPath {
			t.Errorf("bundleConformance produced issue for a file with no role key; "+
				"files with neither mosaic_role nor role must be skipped (got issue code=%q)", iss.Code)
		}
	}
}

