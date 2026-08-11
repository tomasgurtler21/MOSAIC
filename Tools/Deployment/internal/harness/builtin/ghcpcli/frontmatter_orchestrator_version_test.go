package ghcpcli_test

// frontmatter_orchestrator_version_test.go: Tests for conditional stamping of
// mosaic_orchestrator_injections_version in the GHCP CLI harness Frontmatter method.
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
// immediately after "mosaic_injections_version".
//
// RED STATE:
//   TestGHCPCLI_Frontmatter_OrchestratorAgent_StampsMosaicOrchestratorInjectionsVersion:
//     FAILS until I4.3 updates the GHCP CLI Frontmatter() to stamp the prefixed name.
//
//   TestGHCPCLI_Frontmatter_SubagentAgent_DoesNotStampMosaicOrchestratorInjectionsVersion:
//     PASSES in RED phase. Regression guard after implementation.
//
//   TestGHCPCLI_Descriptor_KeyOrder_ContainsMosaicOrchestratorInjectionsVersionAfterMosaicInjectionsVersion:
//     FAILS until I4.4 updates the ghcp-cli descriptor's key_order entries to the prefixed names.

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// TestGHCPCLI_Frontmatter_OrchestratorAgent_StampsMosaicOrchestratorInjectionsVersion
// verifies that calling Frontmatter with AgentKey="orchestrator" and a non-empty
// OrchestratorInjectionsVersion produces a FrontmatterPlan whose Set slice contains a field
// with key "mosaic_orchestrator_injections_version" carrying the provided version value.
//
// The test uses a minimal FrontmatterRequest to isolate the version-stamping behavior.
func TestGHCPCLI_Frontmatter_OrchestratorAgent_StampsMosaicOrchestratorInjectionsVersion(t *testing.T) {
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
			"Set keys: %v", ghcpSetKeys(plan.Set))
	}
	if found.Value.Scalar != wantVersion {
		t.Errorf("Set[\"mosaic_orchestrator_injections_version\"].Value.Scalar = %q; want %q",
			found.Value.Scalar, wantVersion)
	}
}

// TestGHCPCLI_Frontmatter_SubagentAgent_DoesNotStampMosaicOrchestratorInjectionsVersion
// verifies that calling Frontmatter with a non-orchestrator AgentKey does not produce
// "mosaic_orchestrator_injections_version" in the returned FrontmatterPlan.Set. Subagent
// deployed files must not carry the field under any name.
//
// Passes in RED phase; serves as a regression guard after implementation.
func TestGHCPCLI_Frontmatter_SubagentAgent_DoesNotStampMosaicOrchestratorInjectionsVersion(t *testing.T) {
	mod := newModule(t)

	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "test-writer",
		Versions: domain.VersionStamps{
			Version:                       "2.0",
			TransformVersion:              "1.5",
			InjectionsVersion:             "3.0",
			OrchestratorInjectionsVersion: "", // empty for subagents
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

// TestGHCPCLI_Descriptor_KeyOrder_ContainsMosaicOrchestratorInjectionsVersionAfterMosaicInjectionsVersion
// verifies that the ghcp-cli descriptor's key_order list includes
// "mosaic_orchestrator_injections_version" in the position immediately after
// "mosaic_injections_version". Both entries must use the prefixed names after Stage 4.
//
// RED: FAILS until I4.4 updates the ghcp-cli descriptor's key_order entries to the prefixed names.
func TestGHCPCLI_Descriptor_KeyOrder_ContainsMosaicOrchestratorInjectionsVersionAfterMosaicInjectionsVersion(t *testing.T) {
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

// ghcpSetKeys returns the Key fields from a slice of FrontmatterField, for use in error messages.
func ghcpSetKeys(fields []domain.FrontmatterField) []string {
	keys := make([]string, len(fields))
	for i, f := range fields {
		keys[i] = f.Key
	}
	return keys
}
