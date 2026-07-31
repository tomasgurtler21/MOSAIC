package claudecode_test

// frontmatter_orchestrator_version_test.go: Tests for conditional stamping of
// orchestrator_injections_version in the Frontmatter method (Stage 5, AC5.2, AC5.3).
//
// The orchestrator_injections_version field must be stamped into the deployed orchestrator
// agent frontmatter (AC5.2) but must be absent from subagent deployed frontmatter (AC5.3).
// The harness module's Frontmatter() method is responsible for this conditional inclusion:
// when AgentKey == "orchestrator" and Versions.OrchestratorInjectionsVersion is non-empty,
// the returned FrontmatterPlan.Set must include a field with key
// "orchestrator_injections_version". For all other AgentKey values, the field must not
// appear in Set.
//
// Additionally, the descriptor's key_order must position "orchestrator_injections_version"
// immediately after "injections_version", so that the deployed orchestrator file has the
// field in the correct position relative to the adjacent version fields.
//
// RED STATE:
//   TestFrontmatter_OrchestratorAgent_StampsOrchestratorInjectionsVersion:
//     FAILS until I5.3 updates Frontmatter() to conditionally stamp
//     orchestrator_injections_version based on AgentKey.
//
//   TestFrontmatter_SubagentAgent_DoesNotStampOrchestratorInjectionsVersion:
//     PASSES in RED phase (the field is not yet stamped for any agent key). Serves as a
//     regression guard: after I5.3 is implemented, this test catches any implementation
//     that stamps the field for all agent keys instead of orchestrator-only.
//
//   TestDescriptor_KeyOrder_ContainsOrchestratorInjectionsVersionAfterInjectionsVersion:
//     FAILS until I5.4 updates the claude-code descriptor's key_order list.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// TestFrontmatter_OrchestratorAgent_StampsOrchestratorInjectionsVersion verifies that
// calling Frontmatter with AgentKey="orchestrator" and a non-empty
// OrchestratorInjectionsVersion in Versions produces a FrontmatterPlan whose Set slice
// contains a field with key "orchestrator_injections_version" carrying the provided
// version value. This is the stamping path required by AC5.2: the deployed orchestrator
// file must carry the orchestrator_injections_version frontmatter field.
//
// The test uses a minimal FrontmatterRequest (no Source fields, empty model) to isolate
// the version-stamping behavior from the other frontmatter shaping logic.
func TestFrontmatter_OrchestratorAgent_StampsOrchestratorInjectionsVersion(t *testing.T) {
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

	// Scan Set for the orchestrator_injections_version field.
	var found *domain.FrontmatterField
	for i := range plan.Set {
		if plan.Set[i].Key == "orchestrator_injections_version" {
			f := plan.Set[i]
			found = &f
			break
		}
	}
	if found == nil {
		t.Fatalf("Frontmatter(orchestrator) Set does not contain key \"orchestrator_injections_version\"; "+
			"the field must be stamped when AgentKey==\"orchestrator\" and OrchestratorInjectionsVersion is non-empty. "+
			"Set keys: %v", setKeys(plan.Set))
	}
	if found.Value.Scalar != wantVersion {
		t.Errorf("Set[\"orchestrator_injections_version\"].Value.Scalar = %q; want %q",
			found.Value.Scalar, wantVersion)
	}
}

// TestFrontmatter_SubagentAgent_DoesNotStampOrchestratorInjectionsVersion verifies that
// calling Frontmatter with a non-orchestrator AgentKey does not produce
// "orchestrator_injections_version" in the returned FrontmatterPlan.Set. This satisfies
// AC5.3: subagent deployed files must not carry the orchestrator_injections_version field.
//
// The source VersionStamps intentionally carries an empty OrchestratorInjectionsVersion
// (as the harness module produces for subagents), mirroring the real call-site behavior.
// This test passes in RED phase and serves as a regression guard after implementation.
func TestFrontmatter_SubagentAgent_DoesNotStampOrchestratorInjectionsVersion(t *testing.T) {
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

	// Verify the field is absent from Set.
	for _, f := range plan.Set {
		if f.Key == "orchestrator_injections_version" {
			t.Errorf("Frontmatter(subagent \"test-writer\") Set contains key "+
				"\"orchestrator_injections_version\" = %q; this field must not appear in "+
				"subagent frontmatter (AC5.3: subagent deployed files do not carry the field)",
				f.Value.Scalar)
		}
	}
}

// TestDescriptor_KeyOrder_ContainsOrchestratorInjectionsVersionAfterInjectionsVersion
// verifies that the claude-code descriptor's key_order list includes
// "orchestrator_injections_version" in the position immediately after "injections_version".
// The descriptor's key_order controls the position of each field in the deployed agent
// file's frontmatter; incorrect positioning would put the new field out of the expected
// location relative to its sibling version fields.
//
// RED: FAILS until I5.4 adds "orchestrator_injections_version" to the claude-code
// descriptor's key_order list.
func TestDescriptor_KeyOrder_ContainsOrchestratorInjectionsVersionAfterInjectionsVersion(t *testing.T) {
	mod := newModule(t)
	desc := mod.Descriptor()

	keyOrder := desc.Frontmatter.KeyOrder

	injectionsIdx := -1
	orchIdx := -1
	for i, key := range keyOrder {
		if key == "injections_version" {
			injectionsIdx = i
		}
		if key == "orchestrator_injections_version" {
			orchIdx = i
		}
	}

	if orchIdx == -1 {
		t.Fatalf("descriptor key_order does not contain \"orchestrator_injections_version\"; "+
			"current key_order: %v", keyOrder)
	}
	if injectionsIdx == -1 {
		t.Fatalf("descriptor key_order does not contain \"injections_version\" (unexpected); "+
			"current key_order: %v", keyOrder)
	}
	if orchIdx != injectionsIdx+1 {
		t.Errorf("\"orchestrator_injections_version\" is at key_order[%d], want key_order[%d] "+
			"(immediately after \"injections_version\" at index %d); "+
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
