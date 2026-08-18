package plan_test

// dest_mapping_staleness_test.go proves the staleness detection gap for AC4.4/AC4.5.
//
// Background: The real plan.Planner (via AgentStaleness) compares version stamps read from
// the deployed file against current source values to decide whether to re-render an agent.
// It currently checks version, transform_version, injections_version, and
// orchestrator_injections_version. None of these change when the user edits only their
// tool-destination config — so a config-mapping-only change silently produces ActionUnchanged,
// leaving the deployed file's destination field stale or absent.
//
// AC4.4 ("running deploy and then update repeatedly leaves the destination field present and
// identical") and AC4.5 ("removing the mapping from config removes the destination field on
// the next update") both depend on the planner correctly classifying a config-mapping-only
// change as ActionUpdate. Without that, no implementation task can close the gap: the
// planner will never call Content for the agent, so the updated registry module's
// mapping result never reaches transform.Apply, and the destination field is never
// regenerated.
//
// These tests specify the required behavior and must currently FAIL (TDD RED phase):
//
//   - TestAgentStaleness_ToolMappingsVersionMismatch_ReturnsDelta and
//     TestAgentStaleness_ToolMappingsVersionMismatch_DeltaFieldIsToolMappingsVersion
//     reference domain.DeployedArtifactState.ToolMappingsVersion and
//     domain.VersionStamps.ToolMappingsVersion — fields that do not exist yet.
//     Both tests therefore fail to compile until the implementation adds those fields.
//
//   - TestAgentStaleness_ToolMappingsVersionMatch_ReturnsNoExtraDelta likewise
//     references the same new fields. It guards against false positives once the
//     implementation lands: matching versions must not produce a spurious delta.
//
//   - TestBuild_ToolMappingsVersionMismatch_ClassifiesAsActionUpdate calls
//     plan.New().Build() and references plan.Input.ToolMappingsVersion — a field that
//     does not yet exist. It also fails to compile until the planner is extended.
//
// All four tests are the correct RED-phase specification for the implementation task that
// must add ToolMappingsVersion support to domain.DeployedArtifactState, domain.VersionStamps,
// plan.Input, and AgentStaleness.

import (
	"context"
	"testing"
	"time"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// AgentStaleness — ToolMappingsVersion field comparison
// ---------------------------------------------------------------------------

// TestAgentStaleness_ToolMappingsVersionMismatch_ReturnsDelta verifies that when the
// deployed file's tool_mappings_version stamp differs from the current effective mapping
// version in stamps, AgentStaleness returns a non-empty delta slice.
//
// This is the mechanism required for AC4.5: a config-mapping-only change causes the deployed
// file (carrying the old stamp) to be detected as stale, so the planner emits ActionUpdate
// on the next run and the destination field is regenerated or removed.
//
// References domain.DeployedArtifactState.ToolMappingsVersion and
// domain.VersionStamps.ToolMappingsVersion — compile error until implementation.
func TestAgentStaleness_ToolMappingsVersionMismatch_ReturnsDelta(t *testing.T) {
	// All static version stamps match — only the mapping version differs.
	deployed := domain.DeployedArtifactState{
		Present:             true,
		ContentHash:         "sha256:abc",
		Version:             "1.0",
		TransformVersion:    "1.0",
		InjectionsVersion:   "1.0",
		ToolMappingsVersion: "config-hash-v1", // stamp in the deployed file from the previous deploy
	}
	agent := makeAgent("dest-test-agent", "1.0")
	stamps := domain.VersionStamps{
		Version:             "1.0",  // unchanged — no agent or descriptor edit
		TransformVersion:    "1.0",  // unchanged
		InjectionsVersion:   "1.0",  // unchanged
		ToolMappingsVersion: "config-hash-v2", // current effective mapping hash after config change
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) == 0 {
		t.Error("AgentStaleness with ToolMappingsVersion mismatch: got 0 deltas, want at least 1; " +
			"a config-mapping-only change (all other version stamps identical) must produce a " +
			"staleness delta so the planner emits ActionUpdate and regenerates the destination field " +
			"(AC4.4/AC4.5); without this, mosaic-deploy update leaves a stale or absent destination " +
			"field even after the user changes their tool-destination config")
	}
}

// TestAgentStaleness_ToolMappingsVersionMismatch_DeltaFieldIsToolMappingsVersion verifies that
// when the deployed and current effective mapping versions differ, the resulting delta carries
// Field == "tool_mappings_version" and the correct Deployed/Source values.
//
// The field name "tool_mappings_version" is the frontmatter key the deploy executor will
// stamp into deployed agent files (analogous to "transform_version" and "injections_version").
// It must be exactly this string so the executor's resolveVersionStamp switch can recognise
// and write it.
//
// References domain.DeployedArtifactState.ToolMappingsVersion and
// domain.VersionStamps.ToolMappingsVersion — compile error until implementation.
func TestAgentStaleness_ToolMappingsVersionMismatch_DeltaFieldIsToolMappingsVersion(t *testing.T) {
	deployed := domain.DeployedArtifactState{
		Present:             true,
		ContentHash:         "sha256:abc",
		Version:             "1.0",
		TransformVersion:    "1.0",
		InjectionsVersion:   "1.0",
		ToolMappingsVersion: "config-hash-v1",
	}
	agent := makeAgent("dest-test-agent", "1.0")
	stamps := domain.VersionStamps{
		Version:             "1.0",
		TransformVersion:    "1.0",
		InjectionsVersion:   "1.0",
		ToolMappingsVersion: "config-hash-v2",
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	var found *domain.VersionDelta
	for i := range deltas {
		if deltas[i].Field == "tool_mappings_version" {
			found = &deltas[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("AgentStaleness: no delta with Field == %q; got deltas: %v; "+
			"the tool_mappings_version delta must use exactly this field name so the executor "+
			"can stamp it into the deployed file's frontmatter; any other name would leave the "+
			"deployed file without a mapping version stamp and prevent future staleness detection",
			"tool_mappings_version", deltas)
	}
	if found.Deployed != "config-hash-v1" {
		t.Errorf("delta.Deployed = %q, want %q (stamp value read from the deployed file)",
			found.Deployed, "config-hash-v1")
	}
	if found.Source != "config-hash-v2" {
		t.Errorf("delta.Source = %q, want %q (current effective mapping version)",
			found.Source, "config-hash-v2")
	}
}

// TestAgentStaleness_ToolMappingsVersionMatch_ReturnsNoExtraDelta guards against false
// positives: when the deployed file's tool_mappings_version stamp matches the current
// effective mapping version, and all other version stamps also match, AgentStaleness
// must return an empty delta slice (ActionUnchanged path).
//
// Without this guard, a correct implementation could accidentally emit a spurious delta on
// every update run, causing unnecessary re-renders even when nothing has changed.
//
// References domain.DeployedArtifactState.ToolMappingsVersion and
// domain.VersionStamps.ToolMappingsVersion — compile error until implementation.
func TestAgentStaleness_ToolMappingsVersionMatch_ReturnsNoExtraDelta(t *testing.T) {
	const mappingHash = "config-hash-v1"

	deployed := domain.DeployedArtifactState{
		Present:             true,
		ContentHash:         "sha256:abc",
		Version:             "1.0",
		TransformVersion:    "1.0",
		InjectionsVersion:   "1.0",
		ToolMappingsVersion: mappingHash,
	}
	agent := makeAgent("dest-test-agent", "1.0")
	stamps := domain.VersionStamps{
		Version:             "1.0",
		TransformVersion:    "1.0",
		InjectionsVersion:   "1.0",
		ToolMappingsVersion: mappingHash, // same hash — no config change
	}

	deltas := plan.AgentStaleness(deployed, agent, stamps)

	if len(deltas) != 0 {
		t.Errorf("AgentStaleness with all stamps matching (including ToolMappingsVersion): "+
			"got %d deltas, want 0; identical mapping versions must not produce a staleness delta "+
			"(would cause unnecessary re-renders on every update run)", len(deltas))
	}
}

// ---------------------------------------------------------------------------
// plan.New().Build — end-to-end classification with ToolMappingsVersion
// ---------------------------------------------------------------------------

// TestBuild_ToolMappingsVersionMismatch_ClassifiesAsActionUpdate exercises the full
// plan.New().Build() path with a deployed-state fixture that differs from the source ONLY
// in ToolMappingsVersion (all other version stamps match and the content hash matches the
// manifest). The test asserts the plan item is classified ActionUpdate.
//
// This is the real-planner counterpart to the stub-planner test in destination_field_test.go
// (TestUpdate_DestMappingInRegistry_ContentFunctionProducesDestinationField). Where that
// test proves the content generation path works once ActionUpdate is given, this test proves
// the planner itself produces ActionUpdate when only the mapping config changes — the gap
// that motivated both the prior review's Critical #1 and this re-review's sole blocking finding.
//
// The test references plan.Input.ToolMappingsVersion — the planner's injection point for the
// current effective mapping version hash computed by the composition point from the user-local
// and project-level config stores. This field does not exist until the implementation adds it.
// The test therefore fails to compile until then, establishing the RED state.
//
// Once the implementation lands (new implementation task, see PlanProgress.md notes), this test
// must turn GREEN while TestUpdate_DestMappingInRegistry_ContentFunctionProducesDestinationField
// is updated to remove its stubPlanner and use the real planner instead.
//
// References plan.Input.ToolMappingsVersion and domain.DeployedArtifactState.ToolMappingsVersion
// → compile error until implementation.
func TestBuild_ToolMappingsVersionMismatch_ClassifiesAsActionUpdate(t *testing.T) {
	const targetPath = "agents/dest-test-agent.md"
	const contentHash = "sha256:abc123"
	const oldMappingHash = "config-hash-v1"
	const newMappingHash = "config-hash-v2"

	agent := makeAgent("dest-test-agent", "1.0")

	manifestEntry := makeManifestEntry(agentRef("dest-test-agent"), targetPath, "1.0", contentHash)
	manifestEntry.TransformVersion = "1.0"
	manifestEntry.InjectionsVersion = "1.0"

	snap := presentSnapshot(domain.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		HarnessID:     "test-harness",
		UpdatedAt:     time.Now(),
		Entries:       []domain.ManifestEntry{manifestEntry},
	})

	// Deployed file: present, hash matches (not locally modified), all static version stamps
	// match the source — the ONLY difference is ToolMappingsVersion, representing a user who
	// changed their tool-destination config between the last deploy and this update run.
	ds := map[string]domain.DeployedArtifactState{
		targetPath: {
			Present:             true,
			ContentHash:         contentHash,
			Version:             "1.0",
			TransformVersion:    "1.0",
			InjectionsVersion:   "1.0",
			ToolMappingsVersion: oldMappingHash, // stamp from the previous deploy
		},
	}

	// Build the base plan.Input using the existing staleness_test.go helper.
	// Then inject ToolMappingsVersion so classifyAgentItem can include it in stamps.
	// plan.Input.ToolMappingsVersion does not exist until the implementation adds it.
	input := buildInputWithSingleAgent(agent, snap, ds)
	input.ToolMappingsVersion = newMappingHash // current effective mapping hash after config change

	p, err := plan.New().Build(context.Background(), input)
	if err != nil {
		t.Fatalf("plan.New().Build: %v", err)
	}

	item, ok := findItem(p.Items, "dest-test-agent")
	if !ok {
		t.Fatal("plan has no item for dest-test-agent")
	}
	if item.Action != domain.ActionUpdate {
		t.Errorf("dest-test-agent with config-mapping-only change (ToolMappingsVersion %q → %q): "+
			"Action = %q, want %q; "+
			"when the effective tool-destination mapping set changes (user edited config), "+
			"the planner must classify the agent as ActionUpdate so the Content function is called "+
			"and the destination field is regenerated from the updated registry module (AC4.4/AC4.5); "+
			"ActionUnchanged here means mosaic-deploy update silently leaves the destination field "+
			"stale or absent until some other version stamp changes",
			oldMappingHash, newMappingHash, item.Action, domain.ActionUpdate)
	}

	// The delta must name "tool_mappings_version" so the executor can stamp the new hash.
	found := false
	for _, d := range item.Stale {
		if d.Field == "tool_mappings_version" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ActionUpdate item for dest-test-agent has no delta with Field == %q; "+
			"got Stale: %v; the executor relies on this field name to write the mapping version "+
			"stamp into the deployed file, enabling future staleness detection",
			"tool_mappings_version", item.Stale)
	}
}
