package app

// injection_region_probe_test.go covers the HasInjectionRegion signal population in
// probeDeployedArtifact. HasInjectionRegion must be true when the deployed file contains at
// least one InjectionHarness-class region (regardless of whether that region carries a version
// attribute), and false in all other cases (no such region, file absent, file unparseable).
//
// These tests verify the signal-threading path: that the probe layer correctly populates
// DeployedArtifactState.HasInjectionRegion so that AgentStaleness can use it to gate the
// InjectionsVersion comparison.
//
// Behaviors verified:
//
//   Signal = true:
//   - A deployed file with an InjectionHarness-class region carrying a version attribute
//     produces HasInjectionRegion = true.
//   - A deployed file with an InjectionHarness-class region carrying NO version attribute
//     also produces HasInjectionRegion = true. The signal reflects region presence, not
//     version presence; a version-less region still gates the comparison.
//
//   Signal = false:
//   - A deployed file with no InjectionHarness-class regions produces HasInjectionRegion = false.
//   - An absent or unreadable file produces HasInjectionRegion = false (zero value default).

import (
	"testing"
)

// ---------------------------------------------------------------------------
// HasInjectionRegion = true: InjectionHarness region present
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_InjectionHarnessRegionWithVersion_HasInjectionRegionTrue verifies
// that when a deployed file contains an InjectionHarness-class region (e.g. HarnessConstraints)
// with a version attribute on its opening tag, probeDeployedArtifact sets HasInjectionRegion
// to true. This is the primary signal path: region found with version attribute.
func TestProbeDeployedArtifact_InjectionHarnessRegionWithVersion_HasInjectionRegionTrue(t *testing.T) {
	// Arrange: file with an InjectionHarness-class region carrying a version attribute.
	ws := t.TempDir()
	content := []byte("---\nmosaic_version: \"2.0\"\nmosaic_harness_version: \"4.0\"\n---\n\n" +
		"<HarnessConstraints type=\"managed\" version=\"1.5\">\n" +
		"Harness constraint content.\n" +
		"</HarnessConstraints>\n")
	writeFile(t, ws, "agent.md", content)

	// Act
	state := probeDeployedArtifact(ws, "agent.md", "")

	// Assert: HasInjectionRegion must be true.
	if !state.HasInjectionRegion {
		t.Errorf(
			"probeDeployedArtifact on file with InjectionHarness region (version=%q): "+
				"HasInjectionRegion = false, want true; "+
				"the probe must set HasInjectionRegion when any InjectionHarness-class region is found",
			"1.5",
		)
	}
}

// TestProbeDeployedArtifact_InjectionHarnessRegionNoVersionAttribute_HasInjectionRegionTrue
// verifies that HasInjectionRegion is true even when the InjectionHarness-class region carries
// no version attribute. The signal reflects whether the region is present at all -- a region
// without a version attribute still means the agent's body contains injection harness content,
// and the InjectionsVersion comparison in AgentStaleness should proceed normally.
func TestProbeDeployedArtifact_InjectionHarnessRegionNoVersionAttribute_HasInjectionRegionTrue(t *testing.T) {
	// Arrange: file with an InjectionHarness-class region but no version attribute on the tag.
	ws := t.TempDir()
	content := []byte("---\nmosaic_version: \"2.0\"\nmosaic_harness_version: \"4.0\"\n---\n\n" +
		"<HarnessConstraints type=\"managed\">\n" +
		"Harness constraint content (no version attribute on tag).\n" +
		"</HarnessConstraints>\n")
	writeFile(t, ws, "agent.md", content)

	// Act
	state := probeDeployedArtifact(ws, "agent.md", "")

	// Assert: HasInjectionRegion must be true even though no version attribute is present.
	if !state.HasInjectionRegion {
		t.Errorf(
			"probeDeployedArtifact on file with InjectionHarness region (no version attribute): "+
				"HasInjectionRegion = false, want true; "+
				"a region without a version attribute still counts as an injection region; "+
				"HasInjectionRegion reflects region presence, not version presence",
		)
	}
}

// TestProbeDeployedArtifact_MultipleInjectionHarnessRegions_HasInjectionRegionTrue verifies
// that when a file contains multiple InjectionHarness-class regions (e.g. HarnessConstraints
// and HarnessIdentity, as written by applyHarnessRegion), HasInjectionRegion is true.
func TestProbeDeployedArtifact_MultipleInjectionHarnessRegions_HasInjectionRegionTrue(t *testing.T) {
	// Arrange: file with two InjectionHarness-class regions.
	ws := t.TempDir()
	content := []byte("---\nmosaic_version: \"2.0\"\nmosaic_harness_version: \"4.0\"\n---\n\n" +
		"<HarnessConstraints type=\"managed\" version=\"2.1\">\n" +
		"First harness region.\n" +
		"</HarnessConstraints>\n" +
		"<HarnessIdentity type=\"managed\" version=\"2.1\">\n" +
		"Second harness region.\n" +
		"</HarnessIdentity>\n")
	writeFile(t, ws, "agent.md", content)

	// Act
	state := probeDeployedArtifact(ws, "agent.md", "")

	// Assert
	if !state.HasInjectionRegion {
		t.Errorf(
			"probeDeployedArtifact on file with multiple InjectionHarness regions: "+
				"HasInjectionRegion = false, want true",
		)
	}
}

// ---------------------------------------------------------------------------
// HasInjectionRegion = false: no InjectionHarness region
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_NoInjectionHarnessRegion_HasInjectionRegionFalse verifies that
// when a deployed file contains no InjectionHarness-class regions, HasInjectionRegion is false.
// This is the expected state for agents whose harness does not emit injection harness content
// (e.g. mosaic-helper). A false HasInjectionRegion signals to AgentStaleness that the
// InjectionsVersion comparison must be skipped to avoid false-positive staleness.
func TestProbeDeployedArtifact_NoInjectionHarnessRegion_HasInjectionRegionFalse(t *testing.T) {
	// Arrange: file with no InjectionHarness-class regions (plain agent body).
	ws := t.TempDir()
	content := []byte("---\nmosaic_version: \"2.0\"\nmosaic_harness_version: \"4.0\"\n---\n\n" +
		"Plain agent body with no managed harness regions.\n")
	writeFile(t, ws, "agent.md", content)

	// Act
	state := probeDeployedArtifact(ws, "agent.md", "")

	// Assert
	if state.HasInjectionRegion {
		t.Errorf(
			"probeDeployedArtifact on file with no InjectionHarness regions: "+
				"HasInjectionRegion = true, want false; "+
				"agents without injection harness content must not trigger the InjectionsVersion comparison",
		)
	}
}

// TestProbeDeployedArtifact_FileAbsent_HasInjectionRegionFalse verifies that when the target
// file does not exist, probeDeployedArtifact returns HasInjectionRegion = false (zero value).
// An absent file cannot contain any regions; the default false is the correct degraded state.
func TestProbeDeployedArtifact_FileAbsent_HasInjectionRegionFalse(t *testing.T) {
	// Arrange: workspace with no file at the target path.
	ws := t.TempDir()

	// Act
	state := probeDeployedArtifact(ws, "nonexistent.md", "")

	// Assert
	if state.Present {
		t.Fatal("expected Present=false for absent file")
	}
	if state.HasInjectionRegion {
		t.Errorf(
			"probeDeployedArtifact on absent file: HasInjectionRegion = true, want false; "+
				"an absent file must leave HasInjectionRegion at its zero value (false)",
		)
	}
}
