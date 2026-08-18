package app

// mosaic_prefix_read_test.go verifies that every deployed-file read site accepts both the
// prefixed (mosaic_-prefixed) and legacy (unprefixed) names for MOSAIC-only bookkeeping
// fields, with the prefixed name winning when both are present.
//
// Read sites covered:
//   - probeDeployedArtifact   (deployedstate.go)       — reads transform_version,
//     injections_version, orchestrator_injections_version, tool_mappings_version,
//     bundle_version, and (implicitly through eligibility) id.
//   - eligibleHarnessOnly     (harness_only_discovery.go) — reads transform_version for
//     signal one; reads id for NumericID metadata.
//   - buildDeployedAgentIndex (deployed_agent_index.go) — reads id to build the id index.
//
// Every test in this file FAILS until the read paths use agentfields.ReadOrder to try the
// prefixed name first and fall back to the legacy name.

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeAgentFile creates a subdirectory (subdir) inside dir and writes filename with
// content inside it. Returns the subdir path for use as an agentsDir.
func writeAgentFile(t *testing.T, dir, subdir, filename string, content []byte) {
	t.Helper()
	sub := filepath.Join(dir, subdir)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", sub, err)
	}
	if err := os.WriteFile(filepath.Join(sub, filename), content, 0o644); err != nil {
		t.Fatalf("write %q: %v", filename, err)
	}
}

// minimalCanonicalBody is a minimal document body with one canonical <Identity type="core">
// block. Combined with a frontmatter block carrying transform_version (or its prefixed
// equivalent), it satisfies eligibleHarnessOnly's two-signal check.
const minimalCanonicalBody = "<Identity type=\"core\">\nMinimal identity section.\n</Identity>\n"

// ---------------------------------------------------------------------------
// probeDeployedArtifact — prefixed names
// ---------------------------------------------------------------------------

// TestProbeDeployedArtifact_PrefixedTransformVersion_ReadCorrectly verifies that when a
// deployed file carries "mosaic_transform_version" (the prefixed name), probeDeployedArtifact
// reads and populates state.TransformVersion with its value.
// FAILS until the read path tries the prefixed name via agentfields.ReadOrder.
func TestProbeDeployedArtifact_PrefixedTransformVersion_ReadCorrectly(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_transform_version: \"3.0.0\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if !state.Present {
		t.Fatal("expected Present: true for a readable file")
	}
	if state.TransformVersion != "3.0.0" {
		t.Errorf("TransformVersion = %q, want %q; probeDeployedArtifact must read the prefixed mosaic_transform_version key",
			state.TransformVersion, "3.0.0")
	}
}

// TestProbeDeployedArtifact_PrefixedInjectionsVersion_ReadCorrectly verifies that a file
// carrying "mosaic_injections_version" populates state.InjectionsVersion correctly.
func TestProbeDeployedArtifact_PrefixedInjectionsVersion_ReadCorrectly(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_injections_version: \"1.5.0\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.InjectionsVersion != "1.5.0" {
		t.Errorf("InjectionsVersion = %q, want %q; probeDeployedArtifact must read mosaic_injections_version",
			state.InjectionsVersion, "1.5.0")
	}
}

// TestProbeDeployedArtifact_PrefixedToolMappingsVersion_ReadCorrectly verifies that a file
// carrying "mosaic_tool_mappings_version" populates state.ToolMappingsVersion correctly.
func TestProbeDeployedArtifact_PrefixedToolMappingsVersion_ReadCorrectly(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_tool_mappings_version: \"hash99\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.ToolMappingsVersion != "hash99" {
		t.Errorf("ToolMappingsVersion = %q, want %q; probeDeployedArtifact must read mosaic_tool_mappings_version",
			state.ToolMappingsVersion, "hash99")
	}
}

// TestProbeDeployedArtifact_PrefixedBundleVersion_ReadCorrectly verifies that a file
// carrying "mosaic_bundle_version" populates state.BundleVersion correctly.
func TestProbeDeployedArtifact_PrefixedBundleVersion_ReadCorrectly(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_bundle_version: \"b2.0\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.BundleVersion != "b2.0" {
		t.Errorf("BundleVersion = %q, want %q; probeDeployedArtifact must read mosaic_bundle_version",
			state.BundleVersion, "b2.0")
	}
}

// TestProbeDeployedArtifact_PrefixedOrchestratorInjectionsVersion_ReadCorrectly verifies
// that "mosaic_orchestrator_injections_version" populates state.OrchestratorInjectionsVersion.
func TestProbeDeployedArtifact_PrefixedOrchestratorInjectionsVersion_ReadCorrectly(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nmosaic_orchestrator_injections_version: \"o1.0\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.OrchestratorInjectionsVersion != "o1.0" {
		t.Errorf("OrchestratorInjectionsVersion = %q, want %q; probeDeployedArtifact must read mosaic_orchestrator_injections_version",
			state.OrchestratorInjectionsVersion, "o1.0")
	}
}

// TestProbeDeployedArtifact_LegacyTransformVersion_StillReadCorrectly verifies backward
// compatibility: a file carrying the legacy "transform_version" key (without the prefix)
// must still populate state.TransformVersion. This test PASSES both before and after
// the implementation.
func TestProbeDeployedArtifact_LegacyTransformVersion_StillReadCorrectly(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\ntransform_version: \"2.0.0\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.TransformVersion != "2.0.0" {
		t.Errorf("TransformVersion = %q, want %q; legacy transform_version must still be read for backward compatibility",
			state.TransformVersion, "2.0.0")
	}
}

// TestProbeDeployedArtifact_BothPrefixedAndLegacyTransformVersion_PrefixedWins verifies the
// tie-breaking rule: when both "mosaic_transform_version" and "transform_version" are present,
// the prefixed value is used and the legacy value is ignored.
// FAILS until the read path tries the prefixed name first and prefers it.
func TestProbeDeployedArtifact_BothPrefixedAndLegacyTransformVersion_PrefixedWins(t *testing.T) {
	ws := t.TempDir()
	// Both keys present — prefixed has "3.0.0", legacy has "2.0.0". Prefixed must win.
	content := []byte("---\nmosaic_transform_version: \"3.0.0\"\ntransform_version: \"2.0.0\"\n---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if state.TransformVersion != "3.0.0" {
		t.Errorf("TransformVersion = %q, want %q; prefixed mosaic_transform_version must win over legacy transform_version",
			state.TransformVersion, "3.0.0")
	}
}

// TestProbeDeployedArtifact_AllPrefixedStamps_AllFieldsPopulated verifies that a deployed
// file carrying all six prefixed MOSAIC stamp keys has every corresponding state field
// populated from the prefixed key's value.
func TestProbeDeployedArtifact_AllPrefixedStamps_AllFieldsPopulated(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\n" +
		"version: \"1.0.0\"\n" +
		"mosaic_transform_version: \"3.0.0\"\n" +
		"mosaic_injections_version: \"1.5.0\"\n" +
		"mosaic_tool_mappings_version: \"hash77\"\n" +
		"mosaic_bundle_version: \"b3.0\"\n" +
		"mosaic_orchestrator_injections_version: \"o2.0\"\n" +
		"---\n\nAgent body.\n")
	writeFile(t, ws, "agent.md", content)

	state := probeDeployedArtifact(ws, "agent.md", "")

	if !state.Present {
		t.Fatal("expected Present: true")
	}
	if state.TransformVersion != "3.0.0" {
		t.Errorf("TransformVersion = %q, want %q", state.TransformVersion, "3.0.0")
	}
	if state.InjectionsVersion != "1.5.0" {
		t.Errorf("InjectionsVersion = %q, want %q", state.InjectionsVersion, "1.5.0")
	}
	if state.ToolMappingsVersion != "hash77" {
		t.Errorf("ToolMappingsVersion = %q, want %q", state.ToolMappingsVersion, "hash77")
	}
	if state.BundleVersion != "b3.0" {
		t.Errorf("BundleVersion = %q, want %q", state.BundleVersion, "b3.0")
	}
	if state.OrchestratorInjectionsVersion != "o2.0" {
		t.Errorf("OrchestratorInjectionsVersion = %q, want %q", state.OrchestratorInjectionsVersion, "o2.0")
	}
}

// ---------------------------------------------------------------------------
// eligibleHarnessOnly — prefixed transform_version signal
// ---------------------------------------------------------------------------

// TestEligibleHarnessOnly_PrefixedTransformVersion_ReportsEligible verifies that a file
// carrying "mosaic_transform_version" (prefixed) satisfies eligibility signal one. Currently
// the function only reads "transform_version" (legacy), so this test FAILS until the
// implementation tries the prefixed name via ReadOrder.
func TestEligibleHarnessOnly_PrefixedTransformVersion_ReportsEligible(t *testing.T) {
	src := []byte("---\nmosaic_transform_version: \"2.1.0\"\n---\n" + minimalCanonicalBody)

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Errorf("expected Eligible: true for file with mosaic_transform_version, got false: %s",
			verdict.Reason)
	}
}

// TestEligibleHarnessOnly_LegacyTransformVersion_StillEligible verifies backward
// compatibility: a file carrying the legacy "transform_version" key must still be eligible.
// This test PASSES both before and after the implementation.
func TestEligibleHarnessOnly_LegacyTransformVersion_StillEligible(t *testing.T) {
	src := []byte("---\ntransform_version: \"2.1.0\"\n---\n" + minimalCanonicalBody)

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Errorf("expected Eligible: true for file with legacy transform_version, got false: %s", verdict.Reason)
	}
}

// TestEligibleHarnessOnly_PrefixedTransformVersion_MetaTransformVersionPopulated verifies
// that when the file carries "mosaic_transform_version", the returned
// HarnessOnlyMeta.TransformVersion is populated with its value (not an empty string).
// FAILS until the implementation reads the prefixed key.
func TestEligibleHarnessOnly_PrefixedTransformVersion_MetaTransformVersionPopulated(t *testing.T) {
	src := []byte("---\nmosaic_transform_version: \"2.5.0\"\n---\n" + minimalCanonicalBody)

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Fatalf("eligibility check failed (unexpected): %s", verdict.Reason)
	}
	if verdict.Meta.TransformVersion != "2.5.0" {
		t.Errorf("Meta.TransformVersion = %q, want %q; prefixed key value must be reflected in metadata",
			verdict.Meta.TransformVersion, "2.5.0")
	}
}

// TestEligibleHarnessOnly_PrefixedMosaicID_MetaNumericIDPopulated verifies that when the
// deployed file carries "mosaic_id" (the prefixed name), HarnessOnlyMeta.NumericID is
// populated with its value. FAILS until the implementation reads mosaic_id via ReadOrder.
func TestEligibleHarnessOnly_PrefixedMosaicID_MetaNumericIDPopulated(t *testing.T) {
	src := []byte("---\n" +
		"mosaic_transform_version: \"2.1.0\"\n" +
		"mosaic_id: \"42\"\n" +
		"---\n" + minimalCanonicalBody)

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Fatalf("eligibility check failed (unexpected): %s", verdict.Reason)
	}
	if verdict.Meta.NumericID != "42" {
		t.Errorf("Meta.NumericID = %q, want %q; mosaic_id must be read as the numeric ID",
			verdict.Meta.NumericID, "42")
	}
}

// TestEligibleHarnessOnly_LegacyID_MetaNumericIDStillPopulated verifies backward
// compatibility: a file carrying the legacy "id" key still populates NumericID.
// This test PASSES both before and after the implementation.
func TestEligibleHarnessOnly_LegacyID_MetaNumericIDStillPopulated(t *testing.T) {
	src := []byte("---\n" +
		"transform_version: \"2.1.0\"\n" +
		"id: \"7\"\n" +
		"---\n" + minimalCanonicalBody)

	verdict := eligibleHarnessOnly(src)

	if !verdict.Eligible {
		t.Fatalf("eligibility check failed (unexpected): %s", verdict.Reason)
	}
	if verdict.Meta.NumericID != "7" {
		t.Errorf("Meta.NumericID = %q, want %q; legacy id key must still populate NumericID", verdict.Meta.NumericID, "7")
	}
}

// ---------------------------------------------------------------------------
// buildDeployedAgentIndex — prefixed id key
// ---------------------------------------------------------------------------

// TestBuildDeployedAgentIndex_PrefixedMosaicID_AgentIndexed verifies that a deployed file
// carrying "mosaic_id" is indexed correctly. FAILS until the implementation reads
// mosaic_id via agentfields.ReadOrder.
func TestBuildDeployedAgentIndex_PrefixedMosaicID_AgentIndexed(t *testing.T) {
	ws := t.TempDir()
	// Agent file uses the prefixed mosaic_id.
	content := []byte("---\nmosaic_id: \"42\"\nmosaic_transform_version: \"3.0.0\"\n---\n\nBody.\n")
	writeAgentFile(t, ws, "agents", "my-agent.md", content)

	idx := buildDeployedAgentIndex(ws, "agents")

	entries := idx.Lookup("42")
	if len(entries) == 0 {
		t.Error("agent with mosaic_id: \"42\" not found in index; buildDeployedAgentIndex must read the prefixed mosaic_id key")
	} else if len(entries) == 1 {
		want := filepath.Join("agents", "my-agent.md")
		if entries[0].TargetPath != want {
			t.Errorf("TargetPath = %q, want %q", entries[0].TargetPath, want)
		}
	}
}

// TestBuildDeployedAgentIndex_LegacyID_AgentStillIndexed verifies backward compatibility:
// a deployed file carrying the legacy "id" key must still be indexed.
// This test PASSES both before and after the implementation.
func TestBuildDeployedAgentIndex_LegacyID_AgentStillIndexed(t *testing.T) {
	ws := t.TempDir()
	content := []byte("---\nid: \"99\"\ntransform_version: \"3.0.0\"\n---\n\nBody.\n")
	writeAgentFile(t, ws, "agents", "legacy-agent.md", content)

	idx := buildDeployedAgentIndex(ws, "agents")

	entries := idx.Lookup("99")
	if len(entries) == 0 {
		t.Error("agent with legacy id: \"99\" not found in index; legacy id must still be indexed for backward compatibility")
	}
}

// TestBuildDeployedAgentIndex_PrefixedIDWinsOverLegacy_WhenBothPresent verifies the
// tie-breaking rule for the index: when both "mosaic_id" and the legacy "id" are present
// in a file, the prefixed value is used as the index key.
// FAILS until the implementation prefers the prefixed key.
func TestBuildDeployedAgentIndex_PrefixedIDWinsOverLegacy_WhenBothPresent(t *testing.T) {
	ws := t.TempDir()
	// Both keys present: mosaic_id=100, id=200. The index should use 100.
	content := []byte("---\nmosaic_id: \"100\"\nid: \"200\"\ntransform_version: \"3.0.0\"\n---\n\nBody.\n")
	writeAgentFile(t, ws, "agents", "dual-id-agent.md", content)

	idx := buildDeployedAgentIndex(ws, "agents")

	if len(idx.Lookup("100")) == 0 {
		t.Error("agent not found under mosaic_id (100); prefixed mosaic_id must win when both forms are present")
	}
	if len(idx.Lookup("200")) != 0 {
		t.Error("agent found under legacy id (200); legacy id must not win when the prefixed mosaic_id is present")
	}
}
