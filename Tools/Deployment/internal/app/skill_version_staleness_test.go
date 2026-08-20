package app

// skill_version_staleness_test.go verifies backwards-compatible staleness detection for
// skills under the mosaic_version field migration. The SkillStaleness function compares
// values, not field names; correctness depends on probeDeployedArtifact normalising both
// field name forms into the same DeployedArtifactState.Version value.
//
// Staleness invariants:
//
//   - A deployed skill with the legacy "version" field whose value matches the source
//     skill version must NOT be reported as stale solely because of the field rename.
//     (Backwards compatibility: a legacy-deployed skill is not spuriously updated.)
//
//   - A deployed skill with the new "mosaic_version" field whose value matches the source
//     skill version must NOT be reported as stale.
//     (New deployments are read correctly and not spuriously flagged.)
//
//   - When a skill's version value genuinely changes, a delta is reported regardless of
//     which field name the deployed file uses.
//     (Legitimate staleness is correctly detected for both field name forms.)
//
// Tests that exercise the "mosaic_version" path FAIL until probeDeployedArtifact uses
// ReadOrder for the version field (so that "mosaic_version" is read into state.Version).
// Tests that exercise the legacy "version" path PASS from the start.

import (
	"testing"

	"mosaic-common/docformat"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
	"mosaic-deploy/internal/transform"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// probeSkill writes content to a temp file and probes it via probeDeployedArtifact.
// Returns the resulting DeployedArtifactState. Used by skill staleness tests.
func probeSkill(t *testing.T, content []byte) domain.DeployedArtifactState {
	t.Helper()
	ws := t.TempDir()
	writeFile(t, ws, "skill.md", content)
	return probeDeployedArtifact(ws, "skill.md", "")
}

// sourceSkill builds a domain.Skill with the given version for staleness comparison.
func sourceSkill(version string) domain.Skill {
	return domain.Skill{
		Key:     "test-skill",
		Version: version,
	}
}

// ---------------------------------------------------------------------------
// Legacy "version" field — backwards compatibility
// ---------------------------------------------------------------------------

// TestSkillStaleness_LegacyVersionField_MatchingValues_NotStale verifies that a deployed
// skill file using the legacy "version" field is NOT reported as stale when its version
// value matches the source skill version. This is the backwards-compatibility baseline:
// the field name change alone must not trigger a spurious redeploy.
//
// This test PASSES both before and after the implementation (the probe has always read
// bare "version") and serves as a regression guard.
func TestSkillStaleness_LegacyVersionField_MatchingValues_NotStale(t *testing.T) {
	content := []byte("---\nmosaic_id: lean-tdd\nversion: \"1.5.0\"\nname: Lean TDD\n---\n\nSkill body.\n")
	state := probeSkill(t, content)
	skill := sourceSkill("1.5.0")

	deltas := plan.SkillStaleness(state, skill)

	if len(deltas) != 0 {
		t.Errorf("SkillStaleness with legacy-named file and matching values: got %d deltas, want 0 (not stale); "+
			"a skill deployed with \"version: 1.5.0\" must not be flagged as stale when the source version is also \"1.5.0\"",
			len(deltas))
		for _, d := range deltas {
			t.Logf("  delta: field=%q deployed=%q source=%q", d.Field, d.Deployed, d.Source)
		}
	}
}

// TestSkillStaleness_LegacyVersionField_DifferentValues_IsStale verifies that a deployed
// skill file using the legacy "version" field IS reported as stale when its version
// value differs from the source skill version. This distinguishes genuine staleness from
// the spurious staleness that the field rename alone must not trigger.
//
// This test PASSES from the start.
func TestSkillStaleness_LegacyVersionField_DifferentValues_IsStale(t *testing.T) {
	content := []byte("---\nmosaic_id: lean-tdd\nversion: \"1.0.0\"\nname: Lean TDD\n---\n\nSkill body.\n")
	state := probeSkill(t, content)
	skill := sourceSkill("2.0.0") // source version differs

	deltas := plan.SkillStaleness(state, skill)

	if len(deltas) == 0 {
		t.Error("SkillStaleness: got 0 deltas, want at least 1; " +
			"a skill whose deployed version differs from the source version must be reported as stale")
	}
}

// ---------------------------------------------------------------------------
// New "mosaic_version" field — new deployments read correctly
// ---------------------------------------------------------------------------

// TestSkillStaleness_PrefixedVersionField_MatchingValues_NotStale verifies that a
// deployed skill file using the new "mosaic_version" field is NOT reported as stale when
// its version value matches the source skill version. This is the primary new behaviour:
// a skill redeployed with the new field name must not be spuriously flagged on the next
// scan.
//
// FAILS until probeDeployedArtifact reads "mosaic_version" into state.Version.
func TestSkillStaleness_PrefixedVersionField_MatchingValues_NotStale(t *testing.T) {
	content := []byte("---\nmosaic_id: lean-tdd\nmosaic_version: \"2.0.0\"\nname: Lean TDD\n---\n\nSkill body.\n")
	state := probeSkill(t, content)
	skill := sourceSkill("2.0.0")

	deltas := plan.SkillStaleness(state, skill)

	if len(deltas) != 0 {
		t.Errorf("SkillStaleness with prefixed-name file and matching values: got %d deltas, want 0 (not stale); "+
			"a skill deployed with \"mosaic_version: 2.0.0\" must not be flagged as stale when the source version is also \"2.0.0\"; "+
			"probeDeployedArtifact must read \"mosaic_version\" into state.Version",
			len(deltas))
		for _, d := range deltas {
			t.Logf("  delta: field=%q deployed=%q source=%q", d.Field, d.Deployed, d.Source)
		}
	}
}

// TestSkillStaleness_PrefixedVersionField_DifferentValues_IsStale verifies that a deployed
// skill file using the new "mosaic_version" field IS reported as stale when its version
// value genuinely differs from the source skill version. Legitimate staleness must be
// detected for both field name forms.
//
// FAILS until probeDeployedArtifact reads "mosaic_version" into state.Version (otherwise
// the deployed version appears empty, which would trigger a false-positive stale report
// rather than reporting the correct deployed value).
func TestSkillStaleness_PrefixedVersionField_DifferentValues_IsStale(t *testing.T) {
	// Deployed skill has mosaic_version: 1.0.0; source skill has version 2.0.0.
	content := []byte("---\nmosaic_id: lean-tdd\nmosaic_version: \"1.0.0\"\nname: Lean TDD\n---\n\nSkill body.\n")
	state := probeSkill(t, content)
	skill := sourceSkill("2.0.0")

	deltas := plan.SkillStaleness(state, skill)

	if len(deltas) == 0 {
		t.Error("SkillStaleness: got 0 deltas, want at least 1; " +
			"a skill whose \"mosaic_version\" differs from the source version must be reported as stale")
	}

	// Verify the delta carries the correct deployed value (not an empty string that
	// would result from a missing "version" read).
	for _, d := range deltas {
		if d.Field == "version" {
			if d.Deployed != "1.0.0" {
				t.Errorf("delta.Deployed = %q, want %q; the delta must carry the actual deployed version, "+
					"not an empty string caused by a missing mosaic_version read",
					d.Deployed, "1.0.0")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Prefixed wins over legacy when both present
// ---------------------------------------------------------------------------

// TestSkillStaleness_BothVersionFields_PrefixedValueUsedForComparison verifies that
// when both "mosaic_version" and "version" are present in a deployed skill file, the
// prefixed value is used in the staleness comparison. The legacy value must not cause
// a false-positive stale report when the prefixed value matches the source.
//
// Scenario: mosaic_version matches source (not stale); bare version differs (would be stale).
// Expected: zero deltas (prefixed wins and matches).
// FAILS until probeDeployedArtifact uses ReadOrder which prefers "mosaic_version".
func TestSkillStaleness_BothVersionFields_PrefixedValueUsedForComparison(t *testing.T) {
	content := []byte("---\nmosaic_id: dual-skill\nmosaic_version: \"2.0.0\"\nversion: \"1.0.0\"\n---\n\nSkill body.\n")
	state := probeSkill(t, content)
	skill := sourceSkill("2.0.0") // matches mosaic_version, not bare version

	deltas := plan.SkillStaleness(state, skill)

	if len(deltas) != 0 {
		t.Errorf("SkillStaleness: prefixed value matches source but legacy value differs: got %d deltas, want 0; "+
			"\"mosaic_version\" must win over \"version\" in the staleness comparison", len(deltas))
		for _, d := range deltas {
			t.Logf("  delta: field=%q deployed=%q source=%q", d.Field, d.Deployed, d.Source)
		}
	}
}

// ---------------------------------------------------------------------------
// Redeployment produces the correct field name
// ---------------------------------------------------------------------------

// TestSkillStaleness_RedeployedSkill_OutputUsesPrefixedFieldName verifies that when a
// skill source file is processed through RenameSkillVersionField (as buildContent does
// during deployment), the output carries "mosaic_version" and does NOT carry bare
// "version". This confirms that the rename step produces the correct deployed form.
//
// FAILS until RenameSkillVersionField is implemented (currently the stub returns
// the original bytes unchanged, which still carries bare "version").
func TestSkillStaleness_RedeployedSkill_OutputUsesPrefixedFieldName(t *testing.T) {
	// Source skill file with bare "version" (as found in the catalog).
	srcWithVersion := []byte("---\nmosaic_id: lean-tdd\nversion: \"3.0.0\"\nname: Lean TDD\n---\n\nUpdated skill body.\n")

	// Apply the rename (as buildContent would during redeployment).
	renamed, err := transform.RenameSkillVersionField(srcWithVersion)
	if err != nil {
		t.Fatalf("RenameSkillVersionField failed: %v", err)
	}

	// Verify the renamed output uses the prefixed field name by parsing the frontmatter.
	// This fails until RenameSkillVersionField actually renames the field.
	doc, parseErr := docformat.Parse(renamed)
	if parseErr != nil {
		t.Fatalf("docformat.Parse on RenameSkillVersionField output: %v", parseErr)
	}
	fm := doc.Frontmatter()

	if _, ok := fm.Get("mosaic_version"); !ok {
		t.Error("RenameSkillVersionField output does not contain \"mosaic_version\" in frontmatter; " +
			"the rename must transform the source \"version\" field to \"mosaic_version\"")
	}
	if _, ok := fm.Get("version"); ok {
		t.Error("RenameSkillVersionField output still carries bare \"version\" key in frontmatter; " +
			"the original field must be removed when renamed to \"mosaic_version\"")
	}
}
