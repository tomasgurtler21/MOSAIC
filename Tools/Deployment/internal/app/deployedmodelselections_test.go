package app

// deployedmodelselections_test.go covers the deployedModelSelections helper and the
// OriginDeployed constant declared in domain/tier.go.
//
// These are white-box tests (package app) because deployedModelSelections is
// package-private. They are pure unit tests: no filesystem access, no service
// construction, no catalog or interaction stubs required.
//
// Verified invariants:
//
// OriginDeployed constant:
//   - Is a distinct value from OriginUnresolved
//   - A ModelSelection{ModelID: non-empty, Origin: OriginDeployed} reports Resolved() == true
//
// deployedModelSelections — per-agent contract table:
//   - Agent whose deployed file carries a non-empty ModelID is present in the result
//   - The selection's ModelID equals the verbatim value from the deployed state
//   - The selection's Origin is OriginDeployed
//   - The selection satisfies Resolved() == true
//   - Agent whose deployed file has an empty ModelID is absent from the result map
//   - Agent whose deployed file is absent (Present: false) is absent from the result map
//   - Agent whose target path is not in PlannedPaths is absent from the result map
//   - Empty agent list yields a nil or empty map (both acceptable per the contract)
//   - Multiple agents with mixed deployed state produce the correct combination

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// OriginDeployed constant
// ---------------------------------------------------------------------------

// TestOriginDeployed_IsDistinctFromOriginUnresolved verifies that OriginDeployed has a
// distinct value from OriginUnresolved. These two origins have opposite semantics —
// "deployed" means a model was found; "unresolved" means no model was selected — and
// must not be accidentally equated.
func TestOriginDeployed_IsDistinctFromOriginUnresolved(t *testing.T) {
	if domain.OriginDeployed == domain.OriginUnresolved {
		t.Errorf("OriginDeployed (%q) must not equal OriginUnresolved (%q); "+
			"the two origins have opposite semantics and must be separately distinguishable",
			domain.OriginDeployed, domain.OriginUnresolved)
	}
}

// TestOriginDeployed_ModelSelectionWithNonEmptyModelIDIsResolved verifies that a
// ModelSelection with Origin == OriginDeployed and a non-empty ModelID reports
// Resolved() == true. This is required so that transform.applyFrontmatter preserves
// the model key rather than removing it when the selection originated from a deployed file.
func TestOriginDeployed_ModelSelectionWithNonEmptyModelIDIsResolved(t *testing.T) {
	sel := domain.ModelSelection{ModelID: "claude-opus-4-5", Origin: domain.OriginDeployed}
	if !sel.Resolved() {
		t.Errorf("ModelSelection{ModelID: %q, Origin: OriginDeployed}.Resolved() = false, want true; "+
			"a model read back from a deployed file must satisfy Resolved() so the transform "+
			"writes the model key into regenerated content rather than stripping it",
			sel.ModelID)
	}
}

// TestOriginDeployed_EmptyModelIDWithOriginDeployedIsNotResolved verifies that an empty
// ModelID is not considered resolved even when Origin is OriginDeployed. This preserves
// the invariant that Resolved() requires both a non-empty ModelID and a non-unresolved
// origin — a partially-populated selection must not accidentally pass as resolved.
func TestOriginDeployed_EmptyModelIDWithOriginDeployedIsNotResolved(t *testing.T) {
	sel := domain.ModelSelection{ModelID: "", Origin: domain.OriginDeployed}
	if sel.Resolved() {
		t.Error("ModelSelection{ModelID: \"\", Origin: OriginDeployed}.Resolved() = true, want false; " +
			"an empty ModelID must never be treated as resolved, regardless of origin")
	}
}

// ---------------------------------------------------------------------------
// deployedModelSelections — agent with embedded model
// ---------------------------------------------------------------------------

// TestDeployedModelSelections_AgentWithEmbeddedModel_IsPresentInResult verifies that when
// an agent's deployed artifact carries a non-empty ModelID, the result map contains an
// entry for that agent key.
func TestDeployedModelSelections_AgentWithEmbeddedModel_IsPresentInResult(t *testing.T) {
	agents := []domain.Agent{
		{Key: "agent-a", Role: domain.RoleWorker},
	}
	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"}, TargetPath: "agent-a.md"},
	}
	deployedState := map[string]domain.DeployedArtifactState{
		"agent-a.md": {Present: true, ModelID: "claude-opus-4-5"},
	}

	result := deployedModelSelections(agents, paths, deployedState)

	if _, ok := result["agent-a"]; !ok {
		t.Error("agent-a is absent from result map, want it present; " +
			"an agent whose deployed file carries a non-empty ModelID must appear in the result")
	}
}

// TestDeployedModelSelections_AgentWithEmbeddedModel_SelectionIsResolved verifies that the
// model selection produced for an agent with an embedded model satisfies Resolved() == true.
// This is the property that prevents the transform from stripping the model key in
// regenerated content.
func TestDeployedModelSelections_AgentWithEmbeddedModel_SelectionIsResolved(t *testing.T) {
	agents := []domain.Agent{{Key: "agent-a", Role: domain.RoleWorker}}
	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"}, TargetPath: "agent-a.md"},
	}
	deployedState := map[string]domain.DeployedArtifactState{
		"agent-a.md": {Present: true, ModelID: "claude-opus-4-5"},
	}

	result := deployedModelSelections(agents, paths, deployedState)

	sel, ok := result["agent-a"]
	if !ok {
		t.Fatal("agent-a is absent from result map; prerequisite for the Resolved() check")
	}
	if !sel.Resolved() {
		t.Errorf("result[\"agent-a\"].Resolved() = false, want true; "+
			"the selection for an agent with an embedded model must be resolved so the "+
			"planner does not emit a spurious GapNoModel gap and the transform preserves "+
			"the model key in regenerated content")
	}
}

// TestDeployedModelSelections_AgentWithEmbeddedModel_ModelIDIsVerbatim verifies that the
// ModelID in the produced selection is the verbatim value from the deployed state — not
// normalised, reformatted, or validated against the harness model list.
func TestDeployedModelSelections_AgentWithEmbeddedModel_ModelIDIsVerbatim(t *testing.T) {
	const embeddedModel = "claude-opus-4-5"
	agents := []domain.Agent{{Key: "agent-a", Role: domain.RoleWorker}}
	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"}, TargetPath: "agent-a.md"},
	}
	deployedState := map[string]domain.DeployedArtifactState{
		"agent-a.md": {Present: true, ModelID: embeddedModel},
	}

	result := deployedModelSelections(agents, paths, deployedState)

	sel, ok := result["agent-a"]
	if !ok {
		t.Fatal("agent-a is absent from result map; prerequisite for the ModelID verbatim check")
	}
	if sel.ModelID != embeddedModel {
		t.Errorf("result[\"agent-a\"].ModelID = %q, want %q; "+
			"the model ID must be carried verbatim from the deployed state without normalisation",
			sel.ModelID, embeddedModel)
	}
}

// TestDeployedModelSelections_AgentWithEmbeddedModel_OriginIsDeployed verifies that the
// Origin of the produced selection is OriginDeployed, marking its provenance as "read back
// from an already-deployed file" and distinguishing it from interactively resolved or
// tier-mapped selections.
func TestDeployedModelSelections_AgentWithEmbeddedModel_OriginIsDeployed(t *testing.T) {
	agents := []domain.Agent{{Key: "agent-a", Role: domain.RoleWorker}}
	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"}, TargetPath: "agent-a.md"},
	}
	deployedState := map[string]domain.DeployedArtifactState{
		"agent-a.md": {Present: true, ModelID: "claude-opus-4-5"},
	}

	result := deployedModelSelections(agents, paths, deployedState)

	sel, ok := result["agent-a"]
	if !ok {
		t.Fatal("agent-a is absent from result map; prerequisite for the Origin check")
	}
	if sel.Origin != domain.OriginDeployed {
		t.Errorf("result[\"agent-a\"].Origin = %q, want %q (OriginDeployed); "+
			"a selection built from a deployed file must record its provenance with OriginDeployed "+
			"so the origin is distinguishable from interactively resolved or tier-mapped selections",
			sel.Origin, domain.OriginDeployed)
	}
}

// ---------------------------------------------------------------------------
// deployedModelSelections — absent-from-result cases
// ---------------------------------------------------------------------------

// TestDeployedModelSelections_AgentWithEmptyModelID_AbsentFromResult verifies that an agent
// whose deployed file is present but carries an empty ModelID is absent from the result map.
// "Present but empty" means the harness emits no model key, or the key was absent or
// unparseable. Downstream gap evaluation must still flag this agent.
func TestDeployedModelSelections_AgentWithEmptyModelID_AbsentFromResult(t *testing.T) {
	agents := []domain.Agent{{Key: "agent-a", Role: domain.RoleWorker}}
	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"}, TargetPath: "agent-a.md"},
	}
	deployedState := map[string]domain.DeployedArtifactState{
		"agent-a.md": {Present: true, ModelID: ""}, // present file but no embedded model
	}

	result := deployedModelSelections(agents, paths, deployedState)

	if _, ok := result["agent-a"]; ok {
		t.Error("agent-a is present in result map, want absent; " +
			"an agent whose deployed file has an empty ModelID must be absent so downstream " +
			"gap evaluation still flags it for missing model")
	}
}

// TestDeployedModelSelections_AgentWithAbsentDeployedFile_AbsentFromResult verifies that an
// agent whose deployed file does not exist (Present: false) is absent from the result map.
func TestDeployedModelSelections_AgentWithAbsentDeployedFile_AbsentFromResult(t *testing.T) {
	agents := []domain.Agent{{Key: "agent-a", Role: domain.RoleWorker}}
	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"}, TargetPath: "agent-a.md"},
	}
	deployedState := map[string]domain.DeployedArtifactState{
		"agent-a.md": {Present: false, ModelID: ""}, // file does not exist on disk
	}

	result := deployedModelSelections(agents, paths, deployedState)

	if _, ok := result["agent-a"]; ok {
		t.Error("agent-a is present in result map, want absent; " +
			"an agent whose deployed file is absent (Present: false) must not appear in the result")
	}
}

// TestDeployedModelSelections_AgentNotInDeployedState_AbsentFromResult verifies that an
// agent whose target path has no entry in the deployed state map is absent from the result.
// This can happen when the target path was not included in the probe map.
func TestDeployedModelSelections_AgentNotInDeployedState_AbsentFromResult(t *testing.T) {
	agents := []domain.Agent{{Key: "agent-a", Role: domain.RoleWorker}}
	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"}, TargetPath: "agent-a.md"},
	}
	deployedState := map[string]domain.DeployedArtifactState{} // no entry for agent-a.md

	result := deployedModelSelections(agents, paths, deployedState)

	if _, ok := result["agent-a"]; ok {
		t.Error("agent-a is present in result map, want absent; " +
			"an agent whose target path is absent from the deployed state map must be absent from the result")
	}
}

// TestDeployedModelSelections_AgentTargetPathNotInPaths_AbsentFromResult verifies that an
// agent whose target path is not in the PlannedPaths is absent from the result map. Path
// lookup via PlannedPaths.Path is the canonical lookup mechanism; if the path is not there,
// the agent has no planned path and cannot have a deployed model entry.
func TestDeployedModelSelections_AgentTargetPathNotInPaths_AbsentFromResult(t *testing.T) {
	agents := []domain.Agent{{Key: "agent-a", Role: domain.RoleWorker}}
	paths := plan.PlannedPaths{} // no planned paths — agent-a has no entry
	deployedState := map[string]domain.DeployedArtifactState{
		"agent-a.md": {Present: true, ModelID: "claude-opus-4-5"},
	}

	result := deployedModelSelections(agents, paths, deployedState)

	if _, ok := result["agent-a"]; ok {
		t.Error("agent-a is present in result map, want absent; " +
			"an agent whose target path is not in PlannedPaths must be absent from the result map "+
			"(PlannedPaths.Path is the canonical path lookup; a missing entry means no planned path)")
	}
}

// ---------------------------------------------------------------------------
// deployedModelSelections — edge cases
// ---------------------------------------------------------------------------

// TestDeployedModelSelections_NilAgents_ReturnsNilOrEmpty verifies that a nil agent list
// produces either a nil or empty result map. Callers only read the map via index
// (zero-value lookup), so both nil and empty are acceptable.
func TestDeployedModelSelections_NilAgents_ReturnsNilOrEmpty(t *testing.T) {
	result := deployedModelSelections(nil, plan.PlannedPaths{}, nil)

	if len(result) != 0 {
		t.Errorf("nil agent list: result map has %d entries, want 0; "+
			"a nil or empty agent list must produce a nil or empty result map", len(result))
	}
}

// TestDeployedModelSelections_EmptyAgents_ReturnsNilOrEmpty verifies that an empty (non-nil)
// agent slice produces either a nil or empty result map.
func TestDeployedModelSelections_EmptyAgents_ReturnsNilOrEmpty(t *testing.T) {
	result := deployedModelSelections([]domain.Agent{}, plan.PlannedPaths{}, nil)

	if len(result) != 0 {
		t.Errorf("empty agent list: result map has %d entries, want 0; "+
			"an empty agent list must produce a nil or empty result map", len(result))
	}
}

// ---------------------------------------------------------------------------
// deployedModelSelections — multiple agents, mixed deployed state
// ---------------------------------------------------------------------------

// TestDeployedModelSelections_MultipleAgentsMixedState_CorrectCombination verifies that
// when multiple agents have mixed deployed state (some with model, some without, some absent),
// the result map contains exactly the agents with non-empty ModelIDs, with correct selections.
func TestDeployedModelSelections_MultipleAgentsMixedState_CorrectCombination(t *testing.T) {
	agents := []domain.Agent{
		{Key: "with-model", Role: domain.RoleWorker},
		{Key: "no-model", Role: domain.RoleWorker},
		{Key: "absent-file", Role: domain.RoleWorker},
	}
	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "with-model"}, TargetPath: "with-model.md"},
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "no-model"}, TargetPath: "no-model.md"},
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "absent-file"}, TargetPath: "absent-file.md"},
	}
	deployedState := map[string]domain.DeployedArtifactState{
		"with-model.md":  {Present: true, ModelID: "claude-opus-4-5"},
		"no-model.md":    {Present: true, ModelID: ""},   // present but no model
		"absent-file.md": {Present: false, ModelID: ""},  // file does not exist
	}

	result := deployedModelSelections(agents, paths, deployedState)

	// with-model agent must be present with the correct resolved selection.
	if sel, ok := result["with-model"]; !ok {
		t.Error("with-model is absent from result, want present; " +
			"an agent with a non-empty embedded ModelID must appear in the result map")
	} else {
		if sel.ModelID != "claude-opus-4-5" {
			t.Errorf("with-model: ModelID = %q, want %q; ModelID must be carried verbatim",
				sel.ModelID, "claude-opus-4-5")
		}
		if sel.Origin != domain.OriginDeployed {
			t.Errorf("with-model: Origin = %q, want %q (OriginDeployed)",
				sel.Origin, domain.OriginDeployed)
		}
		if !sel.Resolved() {
			t.Error("with-model: Resolved() = false, want true")
		}
	}

	// no-model agent must be absent (empty ModelID → gap evaluation must still flag it).
	if _, ok := result["no-model"]; ok {
		t.Error("no-model is present in result, want absent; " +
			"an agent with an empty embedded ModelID must be absent so gap evaluation still flags it")
	}

	// absent-file agent must be absent (no deployed file → no model to read).
	if _, ok := result["absent-file"]; ok {
		t.Error("absent-file is present in result, want absent; " +
			"an agent whose deployed file does not exist must be absent from the result map")
	}
}

// TestDeployedModelSelections_OnlyAgentsInResult_NotSkillsOrHooks verifies that the result
// map contains only entries for agents (ArtifactAgent kind), not for skills or hooks that
// might also appear in the PlannedPaths. The function operates on its agents slice, not on
// the full PlannedPaths, so non-agent artifacts must not produce entries.
func TestDeployedModelSelections_OnlyAgentsInResult_NotSkillsOrHooks(t *testing.T) {
	// Only one agent in the agents slice; paths also includes a skill and a hook.
	agents := []domain.Agent{
		{Key: "agent-a", Role: domain.RoleWorker},
	}
	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"}, TargetPath: "agent-a.md"},
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}, TargetPath: "lean-tdd/SKILL.md"},
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "test-hooks"}, TargetPath: "test-hooks/hook.sh"},
	}
	deployedState := map[string]domain.DeployedArtifactState{
		"agent-a.md":         {Present: true, ModelID: "claude-opus-4-5"},
		"lean-tdd/SKILL.md":  {Present: true, ModelID: ""},
		"test-hooks/hook.sh": {Present: false, ModelID: ""},
	}

	result := deployedModelSelections(agents, paths, deployedState)

	// Only the agent entry is expected.
	if _, ok := result["agent-a"]; !ok {
		t.Error("agent-a is absent from result, want present")
	}
	// Skills and hooks must not appear as entries (only agents are passed in the agents slice).
	if len(result) > 1 {
		t.Errorf("result map has %d entries, want at most 1 (only the agent); "+
			"skills and hooks must not appear in the result", len(result))
	}
}

// TestDeployedModelSelections_IsPureFunction_DoesNotMutateArguments verifies that
// deployedModelSelections does not mutate the agents slice, the paths slice, or the
// deployedState map. It reads its inputs only.
func TestDeployedModelSelections_IsPureFunction_DoesNotMutateArguments(t *testing.T) {
	agents := []domain.Agent{
		{Key: "agent-a", Role: domain.RoleWorker},
	}
	originalAgentsLen := len(agents)

	paths := plan.PlannedPaths{
		{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "agent-a"}, TargetPath: "agent-a.md"},
	}
	originalPathsLen := len(paths)

	deployedState := map[string]domain.DeployedArtifactState{
		"agent-a.md": {Present: true, ModelID: "claude-opus-4-5"},
	}
	originalStateLen := len(deployedState)

	_ = deployedModelSelections(agents, paths, deployedState)

	if len(agents) != originalAgentsLen {
		t.Errorf("agents slice was mutated: length changed from %d to %d", originalAgentsLen, len(agents))
	}
	if len(paths) != originalPathsLen {
		t.Errorf("paths slice was mutated: length changed from %d to %d", originalPathsLen, len(paths))
	}
	if len(deployedState) != originalStateLen {
		t.Errorf("deployedState map was mutated: length changed from %d to %d", originalStateLen, len(deployedState))
	}
	// Verify the original entry was not changed.
	if deployedState["agent-a.md"].ModelID != "claude-opus-4-5" {
		t.Errorf("deployedState[\"agent-a.md\"].ModelID was mutated to %q; deployedModelSelections must not modify its inputs",
			deployedState["agent-a.md"].ModelID)
	}
}
