package app

// skill_version_probe_test.go verifies that probeDeployedArtifact correctly reads the
// skill version from deployed skill files regardless of whether the file uses the legacy
// "version" field (written before the mosaic_ prefix migration) or the new "mosaic_version"
// field (written after migration).
//
// The reading contract for skill version requires:
//
//   - A deployed skill with "version: X" yields DeployedArtifactState.Version == "X"
//     (backwards compatibility — legacy files are accepted).
//   - A deployed skill with "mosaic_version: X" yields DeployedArtifactState.Version == "X"
//     (new field name — the prefixed form is read correctly).
//   - When both "mosaic_version" and "version" are present (transition state), the prefixed
//     "mosaic_version" value is used and the legacy value is ignored.
//
// These tests FAIL until probeDeployedArtifact is updated to use readDeployedStamp for
// the version field (which in turn uses agentfields.ReadOrder to try "mosaic_version"
// first, then fall back to "version") AND the agentfields registry contains the skill
// version entry.

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Legacy field name — backwards compatibility
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_SkillWithLegacyVersionField_VersionPopulated verifies that a
// deployed skill file carrying the legacy "version" field yields the correct version value
// in the probe state. This test documents the backwards-compatibility baseline: skills
// deployed before the migration must not be treated as having an unknown version.
//
// This test PASSES from the start (probeDeployedArtifact already reads "version") and
// serves as a regression guard to ensure the implementation does not break legacy reading.
func TestProbeDeployedArtifact_SkillWithLegacyVersionField_VersionPopulated(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_id: lean-tdd\nversion: \"1.5.0\"\nname: Lean TDD\n---\n\nSkill body.\n")
	writeFile(t, ws, "lean-tdd.md", content)

	state := probeDeployedArtifact(ws, "lean-tdd.md", "")

	if !state.Present {
		t.Fatal("expected Present: true for a readable file, got false")
	}
	if state.Version != "1.5.0" {
		t.Errorf("Version = %q, want %q; legacy \"version\" field must be read correctly",
			state.Version, "1.5.0")
	}
}

// ---------------------------------------------------------------------------
// New field name — "mosaic_version" is read correctly
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_SkillWithMosaicVersionField_VersionPopulated verifies that a
// deployed skill file carrying the new "mosaic_version" field yields the correct version
// value in the probe state. This is the primary new behaviour introduced by the skill
// version prefix migration.
//
// FAILS until probeDeployedArtifact uses readDeployedStamp(fm, "version") which reads
// "mosaic_version" first via agentfields.ReadOrder.
func TestProbeDeployedArtifact_SkillWithMosaicVersionField_VersionPopulated(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_id: lean-tdd\nmosaic_version: \"2.0.0\"\nname: Lean TDD\n---\n\nSkill body.\n")
	writeFile(t, ws, "lean-tdd.md", content)

	state := probeDeployedArtifact(ws, "lean-tdd.md", "")

	if !state.Present {
		t.Fatal("expected Present: true for a readable file, got false")
	}
	if state.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q; \"mosaic_version\" field must be read into DeployedArtifactState.Version",
			state.Version, "2.0.0")
	}
}

// TestProbeDeployedArtifact_SkillWithMosaicVersionField_NoBareVersionInFile verifies
// that a file carrying ONLY "mosaic_version" (no bare "version") still yields a non-empty
// Version in the probe state. This distinguishes the test from a file that has both
// forms, ensuring the prefixed name alone is sufficient.
// FAILS until the implementation reads "mosaic_version".
func TestProbeDeployedArtifact_SkillWithMosaicVersionField_NoBareVersionInFile(t *testing.T) {
	ws := t.TempDir()
	// Only "mosaic_version" — no bare "version" field.
	content := []byte("---\nmosaic_id: some-skill\nmosaic_version: \"3.1.0\"\n---\n\nBody.\n")
	writeFile(t, ws, "some-skill.md", content)

	state := probeDeployedArtifact(ws, "some-skill.md", "")

	if state.Version == "" {
		t.Error("Version is empty; \"mosaic_version\" must be read even when bare \"version\" is absent; " +
			"probeDeployedArtifact must use ReadOrder (prefixed first, legacy second) for the version field")
	}
	if state.Version != "3.1.0" {
		t.Errorf("Version = %q, want %q", state.Version, "3.1.0")
	}
}

// ---------------------------------------------------------------------------
// Both field names present — prefixed wins
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_SkillWithBothVersionFields_PrefixedWins verifies that when
// a deployed skill file contains both "mosaic_version" and "version" (a transition state
// that must not arise in practice but whose reading behaviour must be well-defined), the
// prefixed "mosaic_version" value is used and the legacy "version" value is ignored.
//
// FAILS until probeDeployedArtifact uses ReadOrder which tries "mosaic_version" first.
func TestProbeDeployedArtifact_SkillWithBothVersionFields_PrefixedWins(t *testing.T) {
	ws := t.TempDir()
	// "mosaic_version" carries "2.0.0"; bare "version" carries a different value "1.0.0".
	// The prefixed value must win.
	content := []byte("---\nmosaic_id: dual-version-skill\nmosaic_version: \"2.0.0\"\nversion: \"1.0.0\"\n---\n\nBody.\n")
	writeFile(t, ws, "skill.md", content)

	state := probeDeployedArtifact(ws, "skill.md", "")

	if state.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q (prefixed value); when both \"mosaic_version\" and \"version\" are present, "+
			"the prefixed \"mosaic_version\" must take priority", state.Version, "2.0.0")
	}
}

// ---------------------------------------------------------------------------
// No version field at all
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_SkillWithNoVersionField_VersionIsEmpty verifies that a
// deployed skill file carrying neither "version" nor "mosaic_version" yields an empty
// Version in the probe state. This is the existing graceful-degradation behaviour and
// must be preserved after the probe changes.
//
// This test PASSES both before and after the implementation.
func TestProbeDeployedArtifact_SkillWithNoVersionField_VersionIsEmpty(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_id: no-version-skill\nname: No Version\n---\n\nBody.\n")
	writeFile(t, ws, "skill.md", content)

	state := probeDeployedArtifact(ws, "skill.md", "")

	if !state.Present {
		t.Fatal("expected Present: true")
	}
	if state.Version != "" {
		t.Errorf("Version = %q, want empty; neither \"version\" nor \"mosaic_version\" is present", state.Version)
	}
}

// ---------------------------------------------------------------------------
// Agent files retain bare "version" after the implementation change
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_AgentWithBareVersionField_VersionReadCorrectly verifies
// that deployed agent files — which use bare "version" by design — continue to yield
// the correct Version value after the probe is updated to use ReadOrder. The ReadOrder
// fallback to "version" must activate for agents (which have no "mosaic_version" field),
// so that agent staleness detection is unaffected.
//
// This test PASSES from the start and CONTINUES to pass after the implementation, acting
// as a regression guard for the FR-7 out-of-scope boundary.
func TestProbeDeployedArtifact_AgentWithBareVersionField_VersionReadCorrectly(t *testing.T) {
	ws := t.TempDir()
	// Deployed agent file using bare "version" (the agent convention, unchanged).
	content := []byte("---\nmosaic_id: 42\nversion: \"5.0.0\"\nmosaic_transform_version: \"3.0.0\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if !state.Present {
		t.Fatal("expected Present: true")
	}
	if state.Version != "5.0.0" {
		t.Errorf("Version = %q, want %q; bare \"version\" field must still be read correctly for agent files after the probe change",
			state.Version, "5.0.0")
	}
}
