package claudecode_test

// frontmatter_orchestrator_version_test.go: Tests for conditional stamping of
// mosaic_orchestrator_injections_version in the Frontmatter method.
//
// The mosaic_orchestrator_injections_version field must be stamped into the deployed
// orchestrator agent frontmatter but must be absent from subagent deployed frontmatter.
// The harness module's Frontmatter() method is responsible for this conditional inclusion:
// when AgentKey == "orchestrator" and Versions.OrchestratorInjectionsVersion is non-empty,
// the returned FrontmatterPlan.Set must include a field with key
// "mosaic_orchestrator_injections_version". For all other AgentKey values, the field must
// not appear in Set.
//
// Additionally, the descriptor's key_order must position "mosaic_orchestrator_injections_version"
// immediately after "mosaic_injections_version", so that the deployed orchestrator file has the
// field in the correct position relative to the adjacent version fields.
//
// RED STATE:
//   TestFrontmatter_OrchestratorAgent_StampsMosaicOrchestratorInjectionsVersion:
//     FAILS until I4.3 updates Frontmatter() to stamp "mosaic_orchestrator_injections_version"
//     (the prefixed name) instead of the legacy "orchestrator_injections_version".
//
//   TestFrontmatter_SubagentAgent_DoesNotStampMosaicOrchestratorInjectionsVersion:
//     PASSES in RED phase (the field is not stamped for any agent key yet). Serves as a
//     regression guard: after I4.3, this test catches an implementation that stamps the
//     field for all agent keys instead of orchestrator-only.
//
//   TestDescriptor_KeyOrder_ContainsMosaicOrchestratorInjectionsVersionAfterMosaicInjectionsVersion:
//     FAILS until I4.4 updates the claude-code descriptor's key_order entries to the prefixed
//     names (mosaic_injections_version, mosaic_orchestrator_injections_version).

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// TestFrontmatter_OrchestratorAgent_StampsMosaicOrchestratorInjectionsVersion verifies that
// calling Frontmatter with AgentKey="orchestrator" and a non-empty
// OrchestratorInjectionsVersion in Versions produces a FrontmatterPlan whose Set slice
// contains a field with key "mosaic_orchestrator_injections_version" carrying the provided
// version value. The deployed orchestrator file must carry the prefixed field name.
//
// The test uses a minimal FrontmatterRequest (no Source fields, empty model) to isolate
// the version-stamping behavior from the other frontmatter shaping logic.
func TestFrontmatter_OrchestratorAgent_StampsMosaicOrchestratorInjectionsVersion(t *testing.T) {
	mod := newModule(t)

	const wantVersion = "1.0"
	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "orchestrator",
		Versions: domain.VersionStamps{
			Version:                       "5.0",
			TransformVersion:              "3.0",
			InjectionsVersion:             "2.0",
			OrchestratorInjectionsVersion: wantVersion,
		},
	}

	plan, err := mod.Frontmatter(req)
	if err != nil {
		t.Fatalf("Frontmatter(orchestrator): %v", err)
	}

	// Scan Set for the mosaic_orchestrator_injections_version field.
	var found *domain.FrontmatterField
	for i := range plan.Set {
		if plan.Set[i].Key == "mosaic_orchestrator_injections_version" {
			f := plan.Set[i]
			found = &f
			break
		}
	}
	if found == nil {
		t.Fatalf("Frontmatter(orchestrator) Set does not contain key \"mosaic_orchestrator_injections_version\"; "+
			"the field must be stamped under the prefixed name when AgentKey==\"orchestrator\" and "+
			"OrchestratorInjectionsVersion is non-empty. "+
			"Set keys: %v", setKeys(plan.Set))
	}
	if found.Value.Scalar != wantVersion {
		t.Errorf("Set[\"mosaic_orchestrator_injections_version\"].Value.Scalar = %q; want %q",
			found.Value.Scalar, wantVersion)
	}
}

// TestFrontmatter_SubagentAgent_DoesNotStampMosaicOrchestratorInjectionsVersion verifies that
// calling Frontmatter with a non-orchestrator AgentKey does not produce
// "mosaic_orchestrator_injections_version" in the returned FrontmatterPlan.Set. Subagent
// deployed files must not carry the orchestrator_injections_version field under any name.
//
// The source VersionStamps intentionally carries an empty OrchestratorInjectionsVersion
// (as the harness module produces for subagents), mirroring the real call-site behavior.
// This test passes in RED phase and serves as a regression guard after implementation.
func TestFrontmatter_SubagentAgent_DoesNotStampMosaicOrchestratorInjectionsVersion(t *testing.T) {
	mod := newModule(t)

	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "test-writer",
		Versions: domain.VersionStamps{
			Version:                       "2.0",
			TransformVersion:              "1.5",
			InjectionsVersion:             "3.0",
			OrchestratorInjectionsVersion: "", // empty for subagents — harness does not populate it
		},
	}

	plan, err := mod.Frontmatter(req)
	if err != nil {
		t.Fatalf("Frontmatter(test-writer subagent): %v", err)
	}

	// Verify neither the prefixed nor the legacy form appears in Set.
	for _, f := range plan.Set {
		if f.Key == "mosaic_orchestrator_injections_version" {
			t.Errorf("Frontmatter(subagent \"test-writer\") Set contains key "+
				"\"mosaic_orchestrator_injections_version\" = %q; this field must not appear in "+
				"subagent frontmatter",
				f.Value.Scalar)
		}
		if f.Key == "orchestrator_injections_version" {
			t.Errorf("Frontmatter(subagent \"test-writer\") Set contains legacy key "+
				"\"orchestrator_injections_version\" = %q; this field must not appear in "+
				"subagent frontmatter under any name",
				f.Value.Scalar)
		}
	}
}

// TestDescriptor_KeyOrder_ContainsMosaicOrchestratorInjectionsVersionAfterMosaicInjectionsVersion
// verifies that the claude-code descriptor's key_order list includes
// "mosaic_orchestrator_injections_version" in the position immediately after
// "mosaic_injections_version". Both entries must use the prefixed names after Stage 4.
//
// RED: FAILS until I4.4 updates the claude-code descriptor's key_order entries to the
// prefixed names "mosaic_injections_version" and "mosaic_orchestrator_injections_version".
func TestDescriptor_KeyOrder_ContainsMosaicOrchestratorInjectionsVersionAfterMosaicInjectionsVersion(t *testing.T) {
	mod := newModule(t)
	desc := mod.Descriptor()

	keyOrder := desc.Frontmatter.KeyOrder

	injectionsIdx := -1
	orchIdx := -1
	for i, key := range keyOrder {
		if key == "mosaic_injections_version" {
			injectionsIdx = i
		}
		if key == "mosaic_orchestrator_injections_version" {
			orchIdx = i
		}
	}

	if orchIdx == -1 {
		t.Fatalf("descriptor key_order does not contain \"mosaic_orchestrator_injections_version\"; "+
			"current key_order: %v", keyOrder)
	}
	if injectionsIdx == -1 {
		t.Fatalf("descriptor key_order does not contain \"mosaic_injections_version\" (unexpected); "+
			"current key_order: %v", keyOrder)
	}
	if orchIdx != injectionsIdx+1 {
		t.Errorf("\"mosaic_orchestrator_injections_version\" is at key_order[%d], want key_order[%d] "+
			"(immediately after \"mosaic_injections_version\" at index %d); "+
			"current key_order: %v",
			orchIdx, injectionsIdx+1, injectionsIdx, keyOrder)
	}
}

// setKeys returns the Key fields from a slice of FrontmatterField, for use in error messages.
func setKeys(fields []domain.FrontmatterField) []string {
	keys := make([]string, len(fields))
	for i, f := range fields {
		keys[i] = f.Key
	}
	return keys
}
