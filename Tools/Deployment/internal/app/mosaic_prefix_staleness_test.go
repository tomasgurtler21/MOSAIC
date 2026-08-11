package app

// mosaic_prefix_staleness_test.go verifies the staleness and migration invariants for
// deployed files that use the mosaic_-prefixed MOSAIC field names.
//
// The migration contract has three assertions:
//
//  1. A deployed file that uses legacy unprefixed names and whose version values match the
//     source stamps must NOT be reported as stale purely because of the name difference.
//     (Legacy names are still accepted by the read path — backward compatibility.)
//
//  2. A deployed file that uses mosaic_-prefixed names and whose version values match the
//     source stamps must NOT be reported as stale — the read path must accept the prefixed
//     names as the primary form.
//
//  3. When both prefixed and legacy names are present, the prefixed value wins and is
//     compared against the source stamp, so the legacy value cannot trigger spurious staleness.
//
// Tests that assert on prefixed-name files (assertions 2 and 3) FAIL until the read path
// uses agentfields.ReadOrder to try the prefixed name first, because currently
// probeDeployedArtifact reads only the legacy names — a file with only prefixed names
// yields empty version fields, which always differ from a non-empty source stamp.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// matchingStamps builds a domain.VersionStamps that exactly matches the version fields
// the fixture deployed file should carry, for use in a "not stale" assertion.
func matchingStamps(version, transformVersion, injectionsVersion string) domain.VersionStamps {
	return domain.VersionStamps{
		Version:           version,
		TransformVersion:  transformVersion,
		InjectionsVersion: injectionsVersion,
	}
}

// probeTempAgent writes content to a temp file and probes it via probeDeployedArtifact.
// Returns the resulting DeployedArtifactState. Helper for staleness tests in this file.
func probeTempAgent(t *testing.T, content []byte) domain.DeployedArtifactState {
	t.Helper()
	ws := t.TempDir()
	writeFile(t, ws, "agent.md", content)
	return probeDeployedArtifact(ws, "agent.md", "")
}

// ---------------------------------------------------------------------------
// Backward compatibility: legacy-named file is not spuriously stale
// ---------------------------------------------------------------------------

// TestStaleness_LegacyNamedFile_MatchingVersions_NotStale verifies that a deployed file
// using the legacy unprefixed names (transform_version, injections_version) is not reported
// as stale when its version values match the source stamps.
// This test PASSES both before and after the implementation.
func TestStaleness_LegacyNamedFile_MatchingVersions_NotStale(t *testing.T) {
	content := []byte("---\n" +
		"version: \"1.5.0\"\n" +
		"transform_version: \"3.0.0\"\n" +
		"injections_version: \"1.2.0\"\n" +
		"---\n\nAgent body.\n")
	state := probeTempAgent(t, content)
	agent := domain.Agent{Key: "my-agent", Version: "1.5.0"}
	stamps := matchingStamps("1.5.0", "3.0.0", "1.2.0")

	deltas := plan.AgentStaleness(state, agent, stamps)

	if len(deltas) != 0 {
		t.Errorf("AgentStaleness with legacy-named file and matching values: got %d deltas, want 0 (not stale); "+
			"legacy names must be accepted for backward compatibility", len(deltas))
		for _, d := range deltas {
			t.Logf("  delta: field=%q deployed=%q source=%q", d.Field, d.Deployed, d.Source)
		}
	}
}

// ---------------------------------------------------------------------------
// Prefixed-name file must not be spuriously stale
// ---------------------------------------------------------------------------

// TestStaleness_PrefixedNamedFile_MatchingTransformVersion_NotStale verifies that a
// deployed file carrying "mosaic_transform_version" (prefixed) is NOT reported as stale
// when its transform version matches the source stamp. FAILS until the read path accepts
// the prefixed name — currently probeDeployedArtifact reads only "transform_version",
// so a file with only the prefixed name yields TransformVersion == "" which differs from
// any non-empty source stamp.
func TestStaleness_PrefixedNamedFile_MatchingTransformVersion_NotStale(t *testing.T) {
	content := []byte("---\n" +
		"version: \"1.5.0\"\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_injections_version: \"1.2.0\"\n" +
		"---\n\nAgent body.\n")
	state := probeTempAgent(t, content)
	agent := domain.Agent{Key: "my-agent", Version: "1.5.0"}
	stamps := matchingStamps("1.5.0", "3.0.0", "1.2.0")

	deltas := plan.AgentStaleness(state, agent, stamps)

	if len(deltas) != 0 {
		t.Errorf("AgentStaleness with prefixed-name file and matching values: got %d deltas, want 0 (not stale); "+
			"mosaic_transform_version must be read and compared against the source stamp", len(deltas))
		for _, d := range deltas {
			t.Logf("  delta: field=%q deployed=%q source=%q", d.Field, d.Deployed, d.Source)
		}
	}
}

// TestStaleness_PrefixedNamedFile_AllStampsMatch_NotStale verifies that a file carrying
// all version stamps under their prefixed names with matching values is not stale.
func TestStaleness_PrefixedNamedFile_AllStampsMatch_NotStale(t *testing.T) {
	content := []byte("---\n" +
		"version: \"2.0.0\"\n" +
		"mosaic_transform_version: \"4.0.0\"\n" +
		"mosaic_injections_version: \"2.0.0\"\n" +
		"mosaic_tool_mappings_version: \"hash1\"\n" +
		"---\n\nAgent body.\n")
	state := probeTempAgent(t, content)
	agent := domain.Agent{Key: "agent-x", Version: "2.0.0"}
	stamps := domain.VersionStamps{
		Version:             "2.0.0",
		TransformVersion:    "4.0.0",
		InjectionsVersion:   "2.0.0",
		ToolMappingsVersion: "hash1",
	}

	deltas := plan.AgentStaleness(state, agent, stamps)

	if len(deltas) != 0 {
		t.Errorf("all prefixed stamps match: got %d deltas, want 0", len(deltas))
		for _, d := range deltas {
			t.Logf("  delta: field=%q deployed=%q source=%q", d.Field, d.Deployed, d.Source)
		}
	}
}

// ---------------------------------------------------------------------------
// Prefixed-name file with mismatched value IS stale (legitimate staleness)
// ---------------------------------------------------------------------------

// TestStaleness_PrefixedNamedFile_TransformVersionMismatch_IsStale verifies that a file
// with "mosaic_transform_version" that differs from the source stamp is correctly reported
// as stale. This distinguishes genuine version staleness from the spurious staleness caused
// by an unread prefixed key. FAILS until the read path accepts the prefixed name.
func TestStaleness_PrefixedNamedFile_TransformVersionMismatch_IsStale(t *testing.T) {
	content := []byte("---\n" +
		"version: \"1.0.0\"\n" +
		"mosaic_transform_version: \"2.0.0\"\n" + // deployed: 2.0.0
		"mosaic_injections_version: \"1.0.0\"\n" +
		"---\n\nAgent body.\n")
	state := probeTempAgent(t, content)
	agent := domain.Agent{Key: "my-agent", Version: "1.0.0"}
	stamps := matchingStamps("1.0.0", "3.0.0", "1.0.0") // source: 3.0.0 (differs)

	deltas := plan.AgentStaleness(state, agent, stamps)

	var foundTransformDelta bool
	for _, d := range deltas {
		if d.Field == "transform_version" {
			foundTransformDelta = true
			if d.Deployed != "2.0.0" {
				t.Errorf("delta.Deployed = %q, want %q (value from the prefixed mosaic_transform_version field)",
					d.Deployed, "2.0.0")
			}
		}
	}
	if !foundTransformDelta {
		t.Errorf("expected a transform_version delta (deployed=2.0.0, source=3.0.0), but got %d deltas: %v",
			len(deltas), deltas)
	}
}

// ---------------------------------------------------------------------------
// Prefixed wins over legacy when both present
// ---------------------------------------------------------------------------

// TestStaleness_BothPrefixedAndLegacy_PrefixedValueUsedForComparison verifies that when
// both "mosaic_transform_version" and "transform_version" are present, the prefixed value
// is used in the staleness comparison, not the legacy value.
//
// Scenario: prefixed value matches source (not stale); legacy value differs (would be stale).
// Expected outcome: zero deltas (prefixed wins and matches).
// FAILS until the read path prefers the prefixed name.
func TestStaleness_BothPrefixedAndLegacy_PrefixedValueUsedForComparison(t *testing.T) {
	content := []byte("---\n" +
		"version: \"1.0.0\"\n" +
		"mosaic_transform_version: \"3.0.0\"\n" + // matches source stamp
		"transform_version: \"1.0.0\"\n" + // legacy — stale value; must be ignored
		"mosaic_injections_version: \"1.2.0\"\n" +
		"injections_version: \"0.1.0\"\n" + // legacy — stale value; must be ignored
		"---\n\nAgent body.\n")
	state := probeTempAgent(t, content)
	agent := domain.Agent{Key: "my-agent", Version: "1.0.0"}
	stamps := matchingStamps("1.0.0", "3.0.0", "1.2.0")

	deltas := plan.AgentStaleness(state, agent, stamps)

	if len(deltas) != 0 {
		t.Errorf("prefixed values match source stamps but legacy values differ: got %d deltas, want 0; "+
			"prefixed mosaic_transform_version must win over legacy transform_version in staleness comparison", len(deltas))
		for _, d := range deltas {
			t.Logf("  delta: field=%q deployed=%q source=%q", d.Field, d.Deployed, d.Source)
		}
	}
}
