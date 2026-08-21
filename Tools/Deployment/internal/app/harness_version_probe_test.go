package app

// harness_version_probe_test.go covers the read-path changes to probeDeployedArtifact and
// the new extractDeployedInjectionVersion helper, both in support of the harness version
// field rename and the injection version tag-attribute read.
//
// The tests cover:
//
//   Read-path update for harness version (T3.1):
//   - probeDeployedArtifact reads mosaic_harness_version from frontmatter into HarnessVersion.
//   - probeDeployedArtifact prefers mosaic_harness_version over mosaic_transform_version when
//     both are present (ReadOrder: new name first).
//
//   Injection version tag reading (T3.1):
//   - extractDeployedInjectionVersion returns the version attribute from the first
//     InjectionHarness-class region (e.g. HarnessConstraints) in a deployed file.
//   - probeDeployedArtifact stores the extracted tag version in InjectionsVersion.
//   - probeDeployedArtifact leaves OrchestratorInjectionsVersion empty: the read side
//     stores a single role-selected value in InjectionsVersion only. Role-conditional
//     comparison happens in AgentStaleness, not in the probe.
//
//   Backwards compatibility (T3.3):
//   - A deployed file with mosaic_transform_version but no mosaic_harness_version still
//     populates HarnessVersion via ReadOrder fallback.
//   - A deployed file with mosaic_injections_version in frontmatter but no tag version
//     attribute on InjectionHarness regions populates InjectionsVersion via frontmatter
//     fallback in extractDeployedInjectionVersion.
//   - A deployed file with both a tag version AND a legacy mosaic_injections_version in
//     frontmatter: the tag version wins (primary path takes precedence over fallback).

import (
	"testing"

	"mosaic-deploy/internal/manifest"
)

// ---------------------------------------------------------------------------
// T3.1 — probeDeployedArtifact reads mosaic_harness_version into HarnessVersion
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_MosaicHarnessVersion_PopulatesHarnessVersion verifies that a
// deployed file carrying mosaic_harness_version in frontmatter has that value read into
// state.HarnessVersion. This is the primary read path after the rename.
func TestProbeDeployedArtifact_MosaicHarnessVersion_PopulatesHarnessVersion(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_version: \"2.0\"\nmosaic_harness_version: \"4.0\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if !state.Present {
		t.Fatal("expected Present: true for readable file")
	}
	if state.HarnessVersion != "4.0" {
		t.Errorf("HarnessVersion = %q, want %q; mosaic_harness_version in frontmatter must populate HarnessVersion",
			state.HarnessVersion, "4.0")
	}
}

// TestProbeDeployedArtifact_BothHarnessVersionAndLegacyTransformVersion_PrefersHarnessVersion
// verifies that when a deployed file (perhaps in a mixed-fleet transition period) carries both
// mosaic_harness_version and mosaic_transform_version, the mosaic_harness_version value wins.
// ReadOrder: the new prefixed name is tried first.
func TestProbeDeployedArtifact_BothHarnessVersionAndLegacyTransformVersion_PrefersHarnessVersion(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_harness_version: \"4.0\"\nmosaic_transform_version: \"3.0\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.HarnessVersion != "4.0" {
		t.Errorf("HarnessVersion = %q, want %q; "+
			"mosaic_harness_version must be preferred over mosaic_transform_version via ReadOrder",
			state.HarnessVersion, "4.0")
	}
}

// TestProbeDeployedArtifact_HarnessVersionAbsent_HarnessVersionFieldEmpty verifies that when
// neither mosaic_harness_version nor its legacy form is present, HarnessVersion is empty.
// This distinguishes a clean pre-migration file (only transform_version, handled by T3.3)
// from a completely unversioned file.
func TestProbeDeployedArtifact_HarnessVersionAbsent_HarnessVersionFieldEmpty(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_version: \"2.0\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.HarnessVersion != "" {
		t.Errorf("HarnessVersion = %q, want empty when mosaic_harness_version is absent from frontmatter",
			state.HarnessVersion)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — extractDeployedInjectionVersion: tag-attribute reading
// ---------------------------------------------------------------------------

// TestExtractDeployedInjectionVersion_HarnessRegionWithVersionAttribute_ReturnsTagVersion
// verifies that when a deployed file contains an InjectionHarness-class region (such as
// HarnessConstraints) with a version attribute on its opening tag, extractDeployedInjectionVersion
// returns that version. This is the primary path after the injection version relocation from
// frontmatter to tag attributes.
func TestExtractDeployedInjectionVersion_HarnessRegionWithVersionAttribute_ReturnsTagVersion(t *testing.T) {
	data := []byte("---\nmosaic_version: \"2.0\"\n---\n\n" +
		"<HarnessConstraints type=\"managed\" version=\"1.5\">\n" +
		"Harness constraint content.\n" +
		"</HarnessConstraints>\n")

	got := extractDeployedInjectionVersion(data, "injections_version")

	if got != "1.5" {
		t.Errorf("extractDeployedInjectionVersion: got %q, want %q; "+
			"version attribute on HarnessConstraints tag must be returned",
			got, "1.5")
	}
}

// TestExtractDeployedInjectionVersion_NoVersionAttribute_ReturnsEmpty verifies that when the
// InjectionHarness region carries no version attribute, extractDeployedInjectionVersion returns
// an empty string from the primary path (before the frontmatter fallback is consulted).
func TestExtractDeployedInjectionVersion_NoVersionAttribute_ReturnsEmpty(t *testing.T) {
	data := []byte("---\nmosaic_version: \"2.0\"\n---\n\n" +
		"<HarnessConstraints type=\"managed\">\n" +
		"Harness constraint content (no version attribute).\n" +
		"</HarnessConstraints>\n")

	// No legacy frontmatter key either, so the fallback also returns empty.
	got := extractDeployedInjectionVersion(data, "injections_version")

	if got != "" {
		t.Errorf("extractDeployedInjectionVersion: got %q, want empty; "+
			"absent version attribute with no frontmatter fallback must return empty string",
			got)
	}
}

// TestExtractDeployedInjectionVersion_NoHarnessRegion_ReturnsEmpty verifies that when the
// deployed file has no InjectionHarness-class regions at all, extractDeployedInjectionVersion
// returns an empty string (no tag to read from).
func TestExtractDeployedInjectionVersion_NoHarnessRegion_ReturnsEmpty(t *testing.T) {
	data := []byte("---\nmosaic_version: \"2.0\"\n---\n\nPlain agent body with no managed regions.\n")

	got := extractDeployedInjectionVersion(data, "injections_version")

	if got != "" {
		t.Errorf("extractDeployedInjectionVersion: got %q, want empty for file with no harness regions",
			got)
	}
}

// TestExtractDeployedInjectionVersion_MultipleHarnessRegions_ReturnsFirstTagVersion verifies
// that when a deployed file contains multiple InjectionHarness-class regions (e.g.
// HarnessConstraints and HarnessIdentity), both carrying the same version (written by
// applyHarnessRegion with a single role-selected value), extractDeployedInjectionVersion
// returns the version from the first region found without requiring all regions to agree.
func TestExtractDeployedInjectionVersion_MultipleHarnessRegions_ReturnsFirstTagVersion(t *testing.T) {
	data := []byte("---\nmosaic_version: \"2.0\"\n---\n\n" +
		"<HarnessConstraints type=\"managed\" version=\"2.1\">\n" +
		"First harness region.\n" +
		"</HarnessConstraints>\n" +
		"<HarnessIdentity type=\"managed\" version=\"2.1\">\n" +
		"Second harness region (same version, as written by applyHarnessRegion).\n" +
		"</HarnessIdentity>\n")

	got := extractDeployedInjectionVersion(data, "injections_version")

	if got != "2.1" {
		t.Errorf("extractDeployedInjectionVersion with multiple harness regions: got %q, want %q",
			got, "2.1")
	}
}

// TestExtractDeployedInjectionVersion_EmptyData_ReturnsEmpty verifies that empty input
// returns an empty string without panicking (graceful degradation).
func TestExtractDeployedInjectionVersion_EmptyData_ReturnsEmpty(t *testing.T) {
	got := extractDeployedInjectionVersion(nil, "injections_version")

	if got != "" {
		t.Errorf("extractDeployedInjectionVersion with nil data: got %q, want empty", got)
	}
}

// TestExtractDeployedInjectionVersion_MalformedDocument_ReturnsEmpty verifies that a document
// that fails to parse returns an empty string without panicking. Graceful degradation is
// required: a parse failure must not propagate as an error to callers.
//
// docformat.Parse rejects YAML with an unclosed flow-sequence bracket as a parse error.
// This is confirmed by the docformat test suite (TestParse_InvalidYAML_UnclosedBracket_ReturnsError).
func TestExtractDeployedInjectionVersion_MalformedDocument_ReturnsEmpty(t *testing.T) {
	// Unclosed YAML bracket — docformat.Parse returns a non-nil error for this input.
	data := []byte("---\ntools: [a, b\n---\n")

	got := extractDeployedInjectionVersion(data, "injections_version")

	if got != "" {
		t.Errorf("extractDeployedInjectionVersion with malformed document: got %q, want empty; "+
			"parse failure must degrade gracefully", got)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — probeDeployedArtifact uses tag-based injection version reading
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_HarnessRegionWithVersionTag_PopulatesInjectionsVersion verifies
// that when a deployed agent file carries an InjectionHarness-class region with a version
// attribute, probeDeployedArtifact stores that version in state.InjectionsVersion. This
// replaces the old behaviour of reading mosaic_injections_version from frontmatter.
func TestProbeDeployedArtifact_HarnessRegionWithVersionTag_PopulatesInjectionsVersion(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_version: \"2.0\"\nmosaic_harness_version: \"4.0\"\n---\n\n" +
		"<HarnessConstraints type=\"managed\" version=\"1.7\">\n" +
		"Harness constraint content.\n" +
		"</HarnessConstraints>\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.InjectionsVersion != "1.7" {
		t.Errorf("InjectionsVersion = %q, want %q; "+
			"version attribute on InjectionHarness region must populate InjectionsVersion",
			state.InjectionsVersion, "1.7")
	}
}

// TestProbeDeployedArtifact_OrchestratorInjectionsVersionAlwaysEmpty verifies that the read
// side no longer populates state.OrchestratorInjectionsVersion from any source. After Stage 3,
// the read side stores a single role-selected injection version value in InjectionsVersion
// only. The role-conditional comparison between deployed and source values is the responsibility
// of AgentStaleness (the compare side), not the probe (the read side).
func TestProbeDeployedArtifact_OrchestratorInjectionsVersionAlwaysEmpty(t *testing.T) {
	ws := t.TempDir()
	// Simulate a pre-Stage-3 deployed orchestrator file that still carries the old
	// mosaic_orchestrator_injections_version frontmatter key.
	content := []byte("---\nmosaic_version: \"2.0\"\n" +
		"mosaic_harness_version: \"4.0\"\n" +
		"mosaic_orchestrator_injections_version: \"1.9\"\n" +
		"---\n\nOrchestrator body.\n")
	writeFile(t, ws, "orchestrator.md", content)

	state := probeDeployedArtifact(ws, "orchestrator.md", "")

	if state.OrchestratorInjectionsVersion != "" {
		t.Errorf("OrchestratorInjectionsVersion = %q, want empty; "+
			"the read side must no longer populate OrchestratorInjectionsVersion from any source; "+
			"role-conditional comparison is handled in AgentStaleness",
			state.OrchestratorInjectionsVersion)
	}
}

// TestProbeDeployedArtifact_HarnessVersionAndInjectionsVersionPopulatedTogether verifies that
// both HarnessVersion (from frontmatter) and InjectionsVersion (from tag) can be populated
// simultaneously from a single deployed file parse — they use different read paths.
func TestProbeDeployedArtifact_HarnessVersionAndInjectionsVersionPopulatedTogether(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_version: \"2.0\"\nmosaic_harness_version: \"4.1\"\n---\n\n" +
		"<HarnessConstraints type=\"managed\" version=\"1.8\">\n" +
		"Content.\n" +
		"</HarnessConstraints>\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.HarnessVersion != "4.1" {
		t.Errorf("HarnessVersion = %q, want %q", state.HarnessVersion, "4.1")
	}
	if state.InjectionsVersion != "1.8" {
		t.Errorf("InjectionsVersion = %q, want %q", state.InjectionsVersion, "1.8")
	}
	if state.ContentHash != manifest.Hash(content) {
		t.Errorf("ContentHash mismatch; content hash must be unaffected by the new read paths")
	}
}

// ---------------------------------------------------------------------------
// T3.3 — Backwards compatibility: legacy field fallback
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_LegacyMosaicTransformVersion_FallsBackToHarnessVersion verifies
// that a pre-migration deployed file carrying mosaic_transform_version (no mosaic_harness_version)
// still populates state.HarnessVersion via the ReadOrder fallback. This ensures a mixed fleet
// (some agents already migrated, some not) is handled without spurious re-deploys caused by
// an empty HarnessVersion on pre-migration files.
func TestProbeDeployedArtifact_LegacyMosaicTransformVersion_FallsBackToHarnessVersion(t *testing.T) {
	ws := t.TempDir()
	// Pre-migration deployed file: has mosaic_transform_version but no mosaic_harness_version.
	content := []byte("---\nmosaic_version: \"2.0\"\nmosaic_transform_version: \"3.5\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.HarnessVersion != "3.5" {
		t.Errorf("HarnessVersion = %q, want %q; "+
			"pre-migration file with only mosaic_transform_version must populate HarnessVersion via fallback",
			state.HarnessVersion, "3.5")
	}
}

// TestProbeDeployedArtifact_LegacyUnprefixedTransformVersion_FallsBackToHarnessVersion verifies
// that a file carrying the fully-legacy unprefixed transform_version key (before mosaic_ prefix
// was introduced) still populates HarnessVersion via the two-level ReadOrder fallback chain.
func TestProbeDeployedArtifact_LegacyUnprefixedTransformVersion_FallsBackToHarnessVersion(t *testing.T) {
	ws := t.TempDir()
	// Fully-legacy file: has unprefixed transform_version (no mosaic_ prefix, no harness_version).
	content := []byte("---\nversion: \"1.0\"\ntransform_version: \"2.0\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.HarnessVersion != "2.0" {
		t.Errorf("HarnessVersion = %q, want %q; "+
			"fully-legacy file with unprefixed transform_version must populate HarnessVersion via ReadOrder fallback",
			state.HarnessVersion, "2.0")
	}
}

// TestExtractDeployedInjectionVersion_LegacyFrontmatterKey_FallsBackWhenNoTagAttribute verifies
// that when the deployed file carries mosaic_injections_version in frontmatter but the
// InjectionHarness regions carry no version attribute (pre-tag-migration), the frontmatter
// fallback populates the result. After Stage 2's write-side migration, new deployments always
// carry tag attributes; this fallback handles files deployed before that migration.
func TestExtractDeployedInjectionVersion_LegacyFrontmatterKey_FallsBackWhenNoTagAttribute(t *testing.T) {
	data := []byte("---\nmosaic_version: \"2.0\"\nmosaic_injections_version: \"1.2\"\n---\n\n" +
		"<HarnessConstraints type=\"managed\">\n" +
		"Content (no version attribute on tag — pre-migration).\n" +
		"</HarnessConstraints>\n")

	got := extractDeployedInjectionVersion(data, "injections_version")

	if got != "1.2" {
		t.Errorf("extractDeployedInjectionVersion with no tag version but legacy frontmatter: "+
			"got %q, want %q; frontmatter fallback must activate when tag attribute is absent",
			got, "1.2")
	}
}

// TestExtractDeployedInjectionVersion_TagVersionWinsOverFrontmatter verifies that when a
// deployed file carries both a tag version attribute AND a legacy mosaic_injections_version
// in frontmatter, the tag version is returned (primary path wins over fallback).
func TestExtractDeployedInjectionVersion_TagVersionWinsOverFrontmatter(t *testing.T) {
	data := []byte("---\nmosaic_version: \"2.0\"\nmosaic_injections_version: \"1.2\"\n---\n\n" +
		"<HarnessConstraints type=\"managed\" version=\"2.0\">\n" +
		"Content with tag version.\n" +
		"</HarnessConstraints>\n")

	got := extractDeployedInjectionVersion(data, "injections_version")

	if got != "2.0" {
		t.Errorf("extractDeployedInjectionVersion with both tag version and frontmatter: "+
			"got %q, want %q; tag attribute must take precedence over frontmatter fallback",
			got, "2.0")
	}
}

// TestProbeDeployedArtifact_OrchestratorInjectionsVersionAlwaysEmpty_TagOnlyCase verifies that
// an orchestrator file carrying an InjectionHarness-class region with a version tag — but no
// mosaic_orchestrator_injections_version frontmatter key — still leaves OrchestratorInjectionsVersion
// empty. The read side stores the extracted injection version in InjectionsVersion only; it
// never writes OrchestratorInjectionsVersion. This covers the tag-only scenario (the existing
// TestProbeDeployedArtifact_OrchestratorInjectionsVersionAlwaysEmpty covers the frontmatter-
// source scenario).
func TestProbeDeployedArtifact_OrchestratorInjectionsVersionAlwaysEmpty_TagOnlyCase(t *testing.T) {
	ws := t.TempDir()
	// Orchestrator file with a tag version on harness regions but no orchestrator-specific
	// frontmatter key. After Stage 2, this is the normal deployed shape.
	content := []byte("---\nmosaic_version: \"2.0\"\nmosaic_harness_version: \"4.0\"\n---\n\n" +
		"<HarnessConstraints type=\"managed\" version=\"7.0\">\n" +
		"Orchestrator harness content.\n" +
		"</HarnessConstraints>\n")
	writeFile(t, ws, "orchestrator.md", content)

	state := probeDeployedArtifact(ws, "orchestrator.md", "")

	if state.InjectionsVersion != "7.0" {
		t.Errorf("InjectionsVersion = %q, want %q; "+
			"tag version on harness region must populate InjectionsVersion",
			state.InjectionsVersion, "7.0")
	}
	if state.OrchestratorInjectionsVersion != "" {
		t.Errorf("OrchestratorInjectionsVersion = %q, want empty; "+
			"the read side must never populate OrchestratorInjectionsVersion — "+
			"the tag version is stored in InjectionsVersion regardless of agent role",
			state.OrchestratorInjectionsVersion)
	}
}

// TestProbeDeployedArtifact_LegacyInjectionsFrontmatter_PopulatesInjectionsVersion verifies
// the end-to-end backwards compat path: a deployed file with mosaic_injections_version in
// frontmatter and no tag attribute has its frontmatter value placed into state.InjectionsVersion
// by probeDeployedArtifact.
func TestProbeDeployedArtifact_LegacyInjectionsFrontmatter_PopulatesInjectionsVersion(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_version: \"1.5\"\nmosaic_injections_version: \"3.0\"\n---\n\n" +
		"<HarnessConstraints type=\"managed\">\n" +
		"Pre-migration harness region (no version attribute).\n" +
		"</HarnessConstraints>\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.InjectionsVersion != "3.0" {
		t.Errorf("InjectionsVersion = %q, want %q; "+
			"pre-migration file with mosaic_injections_version in frontmatter must populate InjectionsVersion via fallback",
			state.InjectionsVersion, "3.0")
	}
}
