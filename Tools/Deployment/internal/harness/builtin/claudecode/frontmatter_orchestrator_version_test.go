package claudecode_test

// frontmatter_orchestrator_version_test.go: Tests verifying that
// mosaic_orchestrator_injections_version has been relocated from frontmatter to region tag
// attributes (Stage 2). The Frontmatter() method must NOT stamp this field for any agent key.
//
// After Stage 2, the orchestrator injections version is written as a version attribute on
// InjectionHarness-class region tags by applyHarnessRegion, not as a frontmatter field.
// The descriptor's key_order must not contain "mosaic_orchestrator_injections_version".

import (
	"testing"

	"mosaic-deploy/internal/domain"
)

// TestFrontmatter_OrchestratorAgent_DoesNotStampMosaicOrchestratorInjectionsVersion verifies
// that calling Frontmatter with AgentKey="orchestrator" does NOT produce a field with key
// "mosaic_orchestrator_injections_version" in FrontmatterPlan.Set. Stage 2 relocated this
// version from frontmatter to InjectionHarness-class region tag attributes.
func TestFrontmatter_OrchestratorAgent_DoesNotStampMosaicOrchestratorInjectionsVersion(t *testing.T) {
	mod := newModule(t)

	req := domain.FrontmatterRequest{
		Kind:     domain.ArtifactAgent,
		AgentKey: "orchestrator",
		Versions: domain.VersionStamps{
			Version:                       "5.0",
			HarnessVersion:              "3.0",
			InjectionsVersion:             "2.0",
			OrchestratorInjectionsVersion: "1.0",
		},
	}

	plan, err := mod.Frontmatter(req)
	if err != nil {
		t.Fatalf("Frontmatter(orchestrator): %v", err)
	}

	// Verify the field is absent from Set for all agents after Stage 2.
	for i := range plan.Set {
		if plan.Set[i].Key == "mosaic_orchestrator_injections_version" {
			t.Errorf("Frontmatter(orchestrator) Set contains key \"mosaic_orchestrator_injections_version\" = %q; "+
				"Stage 2 relocated this version to region tag attributes — it must not appear in frontmatter for any agent",
				plan.Set[i].Value.Scalar)
		}
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
			HarnessVersion:              "1.5",
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

// TestDescriptor_KeyOrder_DoesNotContainMosaicOrchestratorInjectionsVersion verifies that
// the claude-code descriptor's key_order list does NOT contain
// "mosaic_orchestrator_injections_version". Stage 2 relocated this version field from
// frontmatter to region tag attributes, so it no longer needs a key_order position.
func TestDescriptor_KeyOrder_DoesNotContainMosaicOrchestratorInjectionsVersion(t *testing.T) {
	mod := newModule(t)
	desc := mod.Descriptor()

	for _, key := range desc.Frontmatter.KeyOrder {
		if key == "mosaic_orchestrator_injections_version" {
			t.Errorf("descriptor key_order contains \"mosaic_orchestrator_injections_version\"; "+
				"Stage 2 relocated this version to region tag attributes — it must not appear in key_order; "+
				"current key_order: %v", desc.Frontmatter.KeyOrder)
		}
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
