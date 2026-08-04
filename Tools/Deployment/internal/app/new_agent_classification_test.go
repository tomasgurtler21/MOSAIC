package app

// new_agent_classification_test.go covers the three pure classification functions
// introduced for the new-agent deployment path: newlyRequiredAgents, newlyRequiredSkillKeys,
// and admitWorkflowUpdateItem.
//
// All tests are white-box (package app) because the functions are package-private.
// All tests are pure unit tests: no filesystem access, no service construction, no catalog or
// interaction stubs — only direct inputs and output assertions.
//
// Verified invariants:
//
// newlyRequiredAgents:
//   - An agent whose target path has an explicit Present=false deployedState entry is returned.
//   - An agent whose target path has Present=true is not returned.
//   - The orchestrator is never returned, regardless of its deployed state.
//   - An agent whose target path is not enumerated in paths is treated as NOT new.
//   - An agent whose target path is in paths but missing from deployedState is treated as NOT new.
//   - Input order is preserved in the output.
//   - A nil or empty agent list returns nil or empty.
//   - The function does not mutate its arguments.
//
// newlyRequiredSkillKeys:
//   - A skill required by a new agent that is absent from deployedState is returned.
//   - A skill required by a new agent that is present in deployedState is not returned.
//   - A skill in catalogSkills but not required by any new agent is not returned.
//   - A skill required by a new agent but absent from catalogSkills is silently skipped.
//   - A skill shared between a new agent and an existing agent is decided on deployed state only.
//   - Adding extra skills to catalogSkills that no new agent requires does not change the result.
//   - No duplicate keys appear in the result.
//
// admitWorkflowUpdateItem:
//   - The orchestrator item is admitted for every action (create, update, unchanged, conflict).
//   - A non-orchestrator agent item with ActionCreate and its key in newAgentKeys is admitted.
//   - A non-orchestrator agent item with ActionCreate but its key NOT in newAgentKeys is dropped.
//   - A non-orchestrator agent item with ActionUpdate is dropped regardless of newAgentKeys.
//   - A non-orchestrator agent item with ActionConflict is dropped regardless of newAgentKeys.
//   - A non-orchestrator agent item with ActionUnchanged is dropped regardless of newAgentKeys.
//   - A skill item with ActionCreate and its key in newSkillKeys is admitted.
//   - A skill item with ActionCreate but its key NOT in newSkillKeys is dropped.
//   - A skill item with ActionUpdate is dropped regardless of newSkillKeys.
//   - A hook item is dropped for every action.

import (
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// absentState builds a deployedState entry whose Present is false.
func absentState() domain.DeployedArtifactState {
	return domain.DeployedArtifactState{Present: false}
}

// presentState builds a deployedState entry whose Present is true.
func presentState() domain.DeployedArtifactState {
	return domain.DeployedArtifactState{Present: true, Version: "1.0"}
}

// agentPath returns a PlannedPath for an agent with the given key, using the conventional
// target path "<key>.md".
func agentPath(key string) plan.PlannedPath {
	return plan.PlannedPath{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: key},
		TargetPath: key + ".md",
	}
}

// skillPath returns a PlannedPath for a skill with the given key, using the conventional
// target path "<key>/SKILL.md".
func skillPath(key string) plan.PlannedPath {
	return plan.PlannedPath{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: key},
		TargetPath: key + "/SKILL.md",
	}
}

// workerAgent builds a minimal worker agent with the given key.
func workerAgent(key string) domain.Agent {
	return domain.Agent{Key: key, Role: domain.RoleWorker}
}

// orchestratorAgent builds a minimal orchestrator agent with the given key.
func orchestratorAgentOf(key string) domain.Agent {
	return domain.Agent{Key: key, Role: domain.RoleOrchestrator}
}

// agentWithSkills builds a worker agent that requires the named skills.
func agentWithSkills(key string, skillKeys ...string) domain.Agent {
	return domain.Agent{Key: key, Role: domain.RoleWorker, RequiredSkills: skillKeys}
}

// skill builds a minimal domain.Skill.
func skill(key string) domain.Skill {
	return domain.Skill{Key: key, EntryFile: "SKILL.md"}
}

// ---------------------------------------------------------------------------
// newlyRequiredAgents — an absent agent is returned as new
// ---------------------------------------------------------------------------

// TestNewlyRequiredAgents_AbsentAgent_IsReturned verifies that an agent whose target path
// has an explicit Present=false entry in deployedState is included in the returned slice.
// This is the primary admission gate: a file that cannot be read at its target path is new.
func TestNewlyRequiredAgents_AbsentAgent_IsReturned(t *testing.T) {
	agents := []domain.Agent{workerAgent("alpha")}
	paths := plan.PlannedPaths{agentPath("alpha")}
	deployedState := map[string]domain.DeployedArtifactState{
		"alpha.md": absentState(),
	}

	result := newlyRequiredAgents(agents, paths, deployedState, "orchestrator")

	found := false
	for _, a := range result {
		if a.Key == "alpha" {
			found = true
			break
		}
	}
	if !found {
		t.Error("agent \"alpha\" is absent from result, want present; " +
			"an agent with an explicit Present=false deployedState entry must be returned as new — " +
			"it has no deployed file and must be created by this run")
	}
}

// TestNewlyRequiredAgents_PresentAgent_IsNotReturned verifies that an agent whose file
// already exists on disk (Present=true) is not returned, even when its model or content
// may differ from the source.
func TestNewlyRequiredAgents_PresentAgent_IsNotReturned(t *testing.T) {
	agents := []domain.Agent{workerAgent("beta")}
	paths := plan.PlannedPaths{agentPath("beta")}
	deployedState := map[string]domain.DeployedArtifactState{
		"beta.md": presentState(),
	}

	result := newlyRequiredAgents(agents, paths, deployedState, "orchestrator")

	for _, a := range result {
		if a.Key == "beta" {
			t.Error("agent \"beta\" is present in result, want absent; " +
				"an agent whose file already exists (Present=true) must never be returned as new — " +
				"it is an already-deployed agent and must not be overwritten by this flow")
			return
		}
	}
}

// TestNewlyRequiredAgents_StaleButPresentAgent_IsNotReturned verifies that an agent whose
// file is present but potentially stale (version mismatch) is not returned. The new-agent
// classification is purely about file presence, not staleness — staleness is handled by the
// Update flow, not the workflow-update flow.
func TestNewlyRequiredAgents_StaleButPresentAgent_IsNotReturned(t *testing.T) {
	agents := []domain.Agent{workerAgent("gamma")}
	paths := plan.PlannedPaths{agentPath("gamma")}
	deployedState := map[string]domain.DeployedArtifactState{
		"gamma.md": {Present: true, Version: "0.9"}, // stale but present
	}

	result := newlyRequiredAgents(agents, paths, deployedState, "orchestrator")

	for _, a := range result {
		if a.Key == "gamma" {
			t.Error("agent \"gamma\" (stale but present) is in result, want absent; " +
				"a stale agent has a deployed file and must not be classified as new — " +
				"staleness is a concern for the Update flow, not the workflow-update flow")
			return
		}
	}
}

// ---------------------------------------------------------------------------
// newlyRequiredAgents — orchestrator is always excluded
// ---------------------------------------------------------------------------

// TestNewlyRequiredAgents_OrchestratorByRole_IsNotReturned verifies that the orchestrator
// is excluded from the result when its Role is RoleOrchestrator, even when it has no
// deployed file. The orchestrator is always handled by existing flow logic; classifying it
// as new would corrupt its handling.
func TestNewlyRequiredAgents_OrchestratorByRole_IsNotReturned(t *testing.T) {
	orch := orchestratorAgentOf("orch")
	agents := []domain.Agent{orch}
	paths := plan.PlannedPaths{agentPath("orch")}
	deployedState := map[string]domain.DeployedArtifactState{
		"orch.md": absentState(), // would qualify as new if not the orchestrator
	}

	result := newlyRequiredAgents(agents, paths, deployedState, "orch")

	for _, a := range result {
		if a.Key == "orch" {
			t.Error("orchestrator is present in result, want excluded; " +
				"the orchestrator must never be classified as a new agent — " +
				"its model and deployment are handled separately from worker agents")
			return
		}
	}
}

// TestNewlyRequiredAgents_OrchestratorByKey_IsNotReturned verifies that an agent is
// excluded by orchestratorKey comparison even when its Role is not RoleOrchestrator.
// This is a belt-and-suspenders safety: both exclusion paths must work independently.
func TestNewlyRequiredAgents_OrchestratorByKey_IsNotReturned(t *testing.T) {
	// An agent with worker role but the orchestrator's key — the key exclusion must fire.
	orch := domain.Agent{Key: "orch", Role: domain.RoleWorker}
	agents := []domain.Agent{orch}
	paths := plan.PlannedPaths{agentPath("orch")}
	deployedState := map[string]domain.DeployedArtifactState{
		"orch.md": absentState(),
	}

	result := newlyRequiredAgents(agents, paths, deployedState, "orch")

	for _, a := range result {
		if a.Key == "orch" {
			t.Error("agent \"orch\" (matching orchestratorKey) is in result, want excluded; " +
				"key-based exclusion must fire even when the agent's Role is not RoleOrchestrator")
			return
		}
	}
}

// ---------------------------------------------------------------------------
// newlyRequiredAgents — conservative "not new" cases
// ---------------------------------------------------------------------------

// TestNewlyRequiredAgents_AgentTargetPathNotInPaths_IsNotReturned verifies that an agent
// whose target path is not enumerated in paths is treated as NOT new. A missing path entry
// means the path could not be resolved; the conservative choice is "not new" to avoid
// overwriting an already-deployed file.
func TestNewlyRequiredAgents_AgentTargetPathNotInPaths_IsNotReturned(t *testing.T) {
	agents := []domain.Agent{workerAgent("delta")}
	paths := plan.PlannedPaths{} // no planned path for "delta"
	deployedState := map[string]domain.DeployedArtifactState{
		"delta.md": absentState(), // would qualify absent, but path not enumerated
	}

	result := newlyRequiredAgents(agents, paths, deployedState, "orchestrator")

	for _, a := range result {
		if a.Key == "delta" {
			t.Error("agent \"delta\" (no enumerated path) is in result, want excluded; " +
				"an agent whose target path is not in PlannedPaths must be treated as NOT new — " +
				"absence of a path entry is not an absent file and must not trigger new-agent logic")
			return
		}
	}
}

// TestNewlyRequiredAgents_AgentMissingFromDeployedState_IsNotReturned verifies that an agent
// whose target path is in paths but has no entry in deployedState is treated as NOT new.
// The conservative rule is: only an explicit Present=false probe result qualifies.
func TestNewlyRequiredAgents_AgentMissingFromDeployedState_IsNotReturned(t *testing.T) {
	agents := []domain.Agent{workerAgent("epsilon")}
	paths := plan.PlannedPaths{agentPath("epsilon")}
	deployedState := map[string]domain.DeployedArtifactState{} // no entry for epsilon.md

	result := newlyRequiredAgents(agents, paths, deployedState, "orchestrator")

	for _, a := range result {
		if a.Key == "epsilon" {
			t.Error("agent \"epsilon\" (missing from deployedState) is in result, want excluded; " +
				"an agent with no deployedState entry must be treated as NOT new — " +
				"only an explicit Present=false probe result qualifies as new")
			return
		}
	}
}

// ---------------------------------------------------------------------------
// newlyRequiredAgents — ordering and purity
// ---------------------------------------------------------------------------

// TestNewlyRequiredAgents_MultipleAbsentAgents_OrderPreserved verifies that when multiple
// agents qualify as new, the output preserves the same relative order as the input agents
// slice. Order consistency matters for model-question sequencing.
func TestNewlyRequiredAgents_MultipleAbsentAgents_OrderPreserved(t *testing.T) {
	agents := []domain.Agent{
		workerAgent("first"),
		workerAgent("second"),
		workerAgent("third"),
	}
	paths := plan.PlannedPaths{
		agentPath("first"),
		agentPath("second"),
		agentPath("third"),
	}
	deployedState := map[string]domain.DeployedArtifactState{
		"first.md":  absentState(),
		"second.md": absentState(),
		"third.md":  absentState(),
	}

	result := newlyRequiredAgents(agents, paths, deployedState, "orchestrator")

	if len(result) != 3 {
		t.Fatalf("result has %d agents, want 3; all three absent agents must be returned", len(result))
	}
	for i, want := range []string{"first", "second", "third"} {
		if result[i].Key != want {
			t.Errorf("result[%d].Key = %q, want %q; input order must be preserved in the output", i, result[i].Key, want)
		}
	}
}

// TestNewlyRequiredAgents_MixedPresence_OnlyAbsentReturned verifies that when agents have
// mixed present/absent state, only the absent ones appear in the result.
func TestNewlyRequiredAgents_MixedPresence_OnlyAbsentReturned(t *testing.T) {
	agents := []domain.Agent{
		workerAgent("absent-one"),
		workerAgent("present-one"),
		workerAgent("absent-two"),
	}
	paths := plan.PlannedPaths{
		agentPath("absent-one"),
		agentPath("present-one"),
		agentPath("absent-two"),
	}
	deployedState := map[string]domain.DeployedArtifactState{
		"absent-one.md":  absentState(),
		"present-one.md": presentState(),
		"absent-two.md":  absentState(),
	}

	result := newlyRequiredAgents(agents, paths, deployedState, "orchestrator")

	foundAbsentOne, foundPresentOne, foundAbsentTwo := false, false, false
	for _, a := range result {
		switch a.Key {
		case "absent-one":
			foundAbsentOne = true
		case "present-one":
			foundPresentOne = true
		case "absent-two":
			foundAbsentTwo = true
		}
	}
	if !foundAbsentOne {
		t.Error("absent-one is missing from result, want present")
	}
	if foundPresentOne {
		t.Error("present-one is in result, want absent")
	}
	if !foundAbsentTwo {
		t.Error("absent-two is missing from result, want present")
	}
}

// TestNewlyRequiredAgents_NilAgents_ReturnsNilOrEmpty verifies that a nil agent list returns
// either nil or an empty slice.
func TestNewlyRequiredAgents_NilAgents_ReturnsNilOrEmpty(t *testing.T) {
	result := newlyRequiredAgents(nil, plan.PlannedPaths{}, nil, "orchestrator")
	if len(result) != 0 {
		t.Errorf("nil agents: result length = %d, want 0", len(result))
	}
}

// TestNewlyRequiredAgents_EmptyAgents_ReturnsNilOrEmpty verifies that an empty agent slice
// returns either nil or an empty slice.
func TestNewlyRequiredAgents_EmptyAgents_ReturnsNilOrEmpty(t *testing.T) {
	result := newlyRequiredAgents([]domain.Agent{}, plan.PlannedPaths{}, nil, "orchestrator")
	if len(result) != 0 {
		t.Errorf("empty agents: result length = %d, want 0", len(result))
	}
}

// TestNewlyRequiredAgents_IsPure_DoesNotMutateArguments verifies that calling
// newlyRequiredAgents does not modify its inputs.
func TestNewlyRequiredAgents_IsPure_DoesNotMutateArguments(t *testing.T) {
	agents := []domain.Agent{workerAgent("zeta")}
	origAgentsLen := len(agents)

	paths := plan.PlannedPaths{agentPath("zeta")}
	origPathsLen := len(paths)

	deployedState := map[string]domain.DeployedArtifactState{
		"zeta.md": absentState(),
	}
	origStateLen := len(deployedState)
	origStatePresent := deployedState["zeta.md"].Present

	_ = newlyRequiredAgents(agents, paths, deployedState, "orchestrator")

	if len(agents) != origAgentsLen {
		t.Errorf("agents slice was mutated: length changed from %d to %d", origAgentsLen, len(agents))
	}
	if len(paths) != origPathsLen {
		t.Errorf("paths slice was mutated: length changed from %d to %d", origPathsLen, len(paths))
	}
	if len(deployedState) != origStateLen {
		t.Errorf("deployedState map was mutated: length changed from %d to %d", origStateLen, len(deployedState))
	}
	if deployedState["zeta.md"].Present != origStatePresent {
		t.Errorf("deployedState[\"zeta.md\"].Present was mutated to %v", deployedState["zeta.md"].Present)
	}
}

// ---------------------------------------------------------------------------
// newlyRequiredSkillKeys — skill required by a new agent and absent
// ---------------------------------------------------------------------------

// TestNewlyRequiredSkillKeys_AbsentSkill_IsReturned verifies that a skill required by a new
// agent and whose file is absent is included in the returned set.
func TestNewlyRequiredSkillKeys_AbsentSkill_IsReturned(t *testing.T) {
	newAgents := []domain.Agent{agentWithSkills("agent-a", "lean-tdd")}
	catalogSkills := []domain.Skill{skill("lean-tdd")}
	paths := plan.PlannedPaths{skillPath("lean-tdd")}
	deployedState := map[string]domain.DeployedArtifactState{
		"lean-tdd/SKILL.md": absentState(),
	}

	result := newlyRequiredSkillKeys(newAgents, catalogSkills, paths, deployedState)

	if !result["lean-tdd"] {
		t.Error("\"lean-tdd\" is not in result, want present; " +
			"a skill required by a new agent whose file is absent must be returned as a new skill key")
	}
}

// TestNewlyRequiredSkillKeys_PresentSkill_IsNotReturned verifies that a skill required by a
// new agent but whose file is already present is not returned. Stale skill content is not
// within the scope of this flow.
func TestNewlyRequiredSkillKeys_PresentSkill_IsNotReturned(t *testing.T) {
	newAgents := []domain.Agent{agentWithSkills("agent-a", "lean-tdd")}
	catalogSkills := []domain.Skill{skill("lean-tdd")}
	paths := plan.PlannedPaths{skillPath("lean-tdd")}
	deployedState := map[string]domain.DeployedArtifactState{
		"lean-tdd/SKILL.md": presentState(),
	}

	result := newlyRequiredSkillKeys(newAgents, catalogSkills, paths, deployedState)

	if result["lean-tdd"] {
		t.Error("\"lean-tdd\" is in result, want absent; " +
			"a skill whose file already exists must not be returned — already-deployed skills are not touched")
	}
}

// ---------------------------------------------------------------------------
// newlyRequiredSkillKeys — catalog-is-lookup-only rule
// ---------------------------------------------------------------------------

// TestNewlyRequiredSkillKeys_SkillNotRequiredByNewAgent_IsNotReturned verifies that a skill
// present in catalogSkills but not required by any agent in newAgents is never returned, even
// when its file is absent. The catalog is a lookup table; only skills required by new agents
// are candidates.
func TestNewlyRequiredSkillKeys_SkillNotRequiredByNewAgent_IsNotReturned(t *testing.T) {
	newAgents := []domain.Agent{workerAgent("agent-a")} // requires no skills
	catalogSkills := []domain.Skill{skill("lean-tdd")}
	paths := plan.PlannedPaths{skillPath("lean-tdd")}
	deployedState := map[string]domain.DeployedArtifactState{
		"lean-tdd/SKILL.md": absentState(), // absent, but agent doesn't require it
	}

	result := newlyRequiredSkillKeys(newAgents, catalogSkills, paths, deployedState)

	if result["lean-tdd"] {
		t.Error("\"lean-tdd\" is in result, want absent; " +
			"a skill not required by any new agent must not be returned — " +
			"catalogSkills is a lookup table, not an admission gate")
	}
}

// TestNewlyRequiredSkillKeys_ExtraCatalogSkills_DoNotChangeResult verifies that padding
// catalogSkills with extra skills that no new agent requires produces an identical result to
// the unpadded slice. This pins the catalog-is-lookup-only property precisely.
func TestNewlyRequiredSkillKeys_ExtraCatalogSkills_DoNotChangeResult(t *testing.T) {
	newAgents := []domain.Agent{agentWithSkills("agent-a", "lean-tdd")}
	paths := plan.PlannedPaths{
		skillPath("lean-tdd"),
		skillPath("extra-skill"),
	}
	deployedState := map[string]domain.DeployedArtifactState{
		"lean-tdd/SKILL.md":    absentState(),
		"extra-skill/SKILL.md": absentState(),
	}

	// Result without extra skill in catalog.
	catalogSkillsNarrow := []domain.Skill{skill("lean-tdd")}
	resultNarrow := newlyRequiredSkillKeys(newAgents, catalogSkillsNarrow, paths, deployedState)

	// Result with extra skill padded into the catalog (extra-skill is absent but not required).
	catalogSkillsWide := []domain.Skill{skill("lean-tdd"), skill("extra-skill")}
	resultWide := newlyRequiredSkillKeys(newAgents, catalogSkillsWide, paths, deployedState)

	// The only key that should be present is "lean-tdd" in both cases.
	if !resultNarrow["lean-tdd"] {
		t.Error("baseline: lean-tdd missing from narrow result; prerequisite for the padding comparison")
	}
	if resultWide["extra-skill"] {
		t.Error("extra-skill is in wide result, want absent; " +
			"adding a skill to catalogSkills that no new agent requires must not change the returned set — " +
			"catalogSkills is a lookup table only, not an admission gate")
	}
	if resultNarrow["lean-tdd"] != resultWide["lean-tdd"] {
		t.Errorf("lean-tdd result differs between narrow (%v) and wide (%v) catalog; "+
			"padding catalogSkills must not affect skills that new agents actually require",
			resultNarrow["lean-tdd"], resultWide["lean-tdd"])
	}
}

// ---------------------------------------------------------------------------
// newlyRequiredSkillKeys — edge cases
// ---------------------------------------------------------------------------

// TestNewlyRequiredSkillKeys_SkillNotInCatalog_IsSkipped verifies that a skill required by a
// new agent but absent from catalogSkills is silently skipped. The missing-skill condition is
// reported elsewhere; this function only resolves what it can look up.
func TestNewlyRequiredSkillKeys_SkillNotInCatalog_IsSkipped(t *testing.T) {
	newAgents := []domain.Agent{agentWithSkills("agent-a", "unknown-skill")}
	catalogSkills := []domain.Skill{} // "unknown-skill" is not in the catalog
	paths := plan.PlannedPaths{}
	deployedState := map[string]domain.DeployedArtifactState{}

	result := newlyRequiredSkillKeys(newAgents, catalogSkills, paths, deployedState)

	// Should not panic or return "unknown-skill".
	if result["unknown-skill"] {
		t.Error("\"unknown-skill\" is in result, want absent; " +
			"a skill key with no matching record in catalogSkills cannot be resolved and must be silently skipped")
	}
}

// TestNewlyRequiredSkillKeys_SharedSkill_DecidedByDeployedState verifies that a skill required
// by both a new agent and an already-deployed agent is admitted only when its file is absent,
// regardless of which agents require it. Gate 1 (new agent requires it) is satisfied via the
// new agent; Gate 2 (absent) is decisive.
func TestNewlyRequiredSkillKeys_SharedSkill_DecidedByDeployedState(t *testing.T) {
	// "lean-tdd" is required by both newAgent (new) and existingAgent (deployed — not in newAgents).
	newAgents := []domain.Agent{agentWithSkills("new-agent", "lean-tdd")}
	// existingAgent is intentionally not in newAgents.
	catalogSkills := []domain.Skill{skill("lean-tdd")}

	// Case A: skill file is absent → must be returned.
	pathsA := plan.PlannedPaths{skillPath("lean-tdd")}
	stateA := map[string]domain.DeployedArtifactState{"lean-tdd/SKILL.md": absentState()}
	resultA := newlyRequiredSkillKeys(newAgents, catalogSkills, pathsA, stateA)
	if !resultA["lean-tdd"] {
		t.Error("Case A (absent shared skill): lean-tdd not in result, want present; " +
			"a shared skill whose file is absent must be returned because the new agent requires it")
	}

	// Case B: skill file is present → must NOT be returned.
	pathsB := plan.PlannedPaths{skillPath("lean-tdd")}
	stateB := map[string]domain.DeployedArtifactState{"lean-tdd/SKILL.md": presentState()}
	resultB := newlyRequiredSkillKeys(newAgents, catalogSkills, pathsB, stateB)
	if resultB["lean-tdd"] {
		t.Error("Case B (present shared skill): lean-tdd is in result, want absent; " +
			"a shared skill whose file is already present must not be returned")
	}
}

// TestNewlyRequiredSkillKeys_NoDuplicateKeys verifies that each skill key appears at most once
// in the result even when multiple new agents require the same skill.
func TestNewlyRequiredSkillKeys_NoDuplicateKeys(t *testing.T) {
	newAgents := []domain.Agent{
		agentWithSkills("agent-a", "shared-skill"),
		agentWithSkills("agent-b", "shared-skill"),
	}
	catalogSkills := []domain.Skill{skill("shared-skill")}
	paths := plan.PlannedPaths{skillPath("shared-skill")}
	deployedState := map[string]domain.DeployedArtifactState{
		"shared-skill/SKILL.md": absentState(),
	}

	result := newlyRequiredSkillKeys(newAgents, catalogSkills, paths, deployedState)

	// result is map[string]bool so by definition no duplicate keys; just verify it's present.
	if !result["shared-skill"] {
		t.Error("shared-skill is missing from result; " +
			"a skill required by multiple new agents and absent from disk must appear in the result set")
	}
}

// TestNewlyRequiredSkillKeys_NilNewAgents_ReturnsNilOrEmpty verifies that a nil new-agent
// list returns nil or empty.
func TestNewlyRequiredSkillKeys_NilNewAgents_ReturnsNilOrEmpty(t *testing.T) {
	result := newlyRequiredSkillKeys(nil, []domain.Skill{skill("lean-tdd")}, plan.PlannedPaths{}, nil)
	if len(result) != 0 {
		t.Errorf("nil newAgents: result length = %d, want 0", len(result))
	}
}

// ---------------------------------------------------------------------------
// admitWorkflowUpdateItem — orchestrator is always admitted
// ---------------------------------------------------------------------------

// TestAdmitWorkflowUpdateItem_OrchestratorCreate_IsAdmitted verifies that the orchestrator
// agent item is admitted when its action is ActionCreate.
func TestAdmitWorkflowUpdateItem_OrchestratorCreate_IsAdmitted(t *testing.T) {
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
		Action: domain.ActionCreate,
	}
	if !admitWorkflowUpdateItem(item, "orchestrator", nil, nil) {
		t.Error("orchestrator with ActionCreate: admit=false, want true; " +
			"the orchestrator must be admitted regardless of action")
	}
}

// TestAdmitWorkflowUpdateItem_OrchestratorUpdate_IsAdmitted verifies that the orchestrator
// agent item is admitted when its action is ActionUpdate.
func TestAdmitWorkflowUpdateItem_OrchestratorUpdate_IsAdmitted(t *testing.T) {
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
		Action: domain.ActionUpdate,
	}
	if !admitWorkflowUpdateItem(item, "orchestrator", nil, nil) {
		t.Error("orchestrator with ActionUpdate: admit=false, want true")
	}
}

// TestAdmitWorkflowUpdateItem_OrchestratorUnchanged_IsAdmitted verifies that the orchestrator
// item is admitted even when its action is ActionUnchanged.
func TestAdmitWorkflowUpdateItem_OrchestratorUnchanged_IsAdmitted(t *testing.T) {
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
		Action: domain.ActionUnchanged,
	}
	if !admitWorkflowUpdateItem(item, "orchestrator", nil, nil) {
		t.Error("orchestrator with ActionUnchanged: admit=false, want true")
	}
}

// TestAdmitWorkflowUpdateItem_OrchestratorConflict_IsAdmitted verifies that the orchestrator
// item is admitted even when it has a local modification conflict.
func TestAdmitWorkflowUpdateItem_OrchestratorConflict_IsAdmitted(t *testing.T) {
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
		Action: domain.ActionConflict,
	}
	if !admitWorkflowUpdateItem(item, "orchestrator", nil, nil) {
		t.Error("orchestrator with ActionConflict: admit=false, want true")
	}
}

// ---------------------------------------------------------------------------
// admitWorkflowUpdateItem — new non-orchestrator agent with ActionCreate
// ---------------------------------------------------------------------------

// TestAdmitWorkflowUpdateItem_NewAgentCreate_IsAdmitted verifies that a non-orchestrator
// agent item with ActionCreate is admitted when its key appears in newAgentKeys.
func TestAdmitWorkflowUpdateItem_NewAgentCreate_IsAdmitted(t *testing.T) {
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		Action: domain.ActionCreate,
	}
	newAgentKeys := map[string]bool{"test-runner": true}

	if !admitWorkflowUpdateItem(item, "orchestrator", newAgentKeys, nil) {
		t.Error("new agent (ActionCreate, in newAgentKeys): admit=false, want true; " +
			"an absent required agent with ActionCreate and its key in newAgentKeys must be admitted to the executor")
	}
}

// TestAdmitWorkflowUpdateItem_NewAgentCreate_NotInNewAgentKeys_IsDropped verifies that a
// non-orchestrator agent item with ActionCreate is dropped when its key is NOT in newAgentKeys.
// A present agent may still have ActionCreate in unusual plan states; it must not be
// admitted unless it was explicitly classified as new.
func TestAdmitWorkflowUpdateItem_NewAgentCreate_NotInNewAgentKeys_IsDropped(t *testing.T) {
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		Action: domain.ActionCreate,
	}
	newAgentKeys := map[string]bool{} // test-runner is not in the set

	if admitWorkflowUpdateItem(item, "orchestrator", newAgentKeys, nil) {
		t.Error("agent with ActionCreate but NOT in newAgentKeys: admit=true, want false; " +
			"a non-orchestrator agent must only be admitted when it is explicitly classified as new")
	}
}

// ---------------------------------------------------------------------------
// admitWorkflowUpdateItem — already-deployed non-orchestrator agents are always dropped
// ---------------------------------------------------------------------------

// TestAdmitWorkflowUpdateItem_ExistingAgentUpdate_IsDropped verifies that a non-orchestrator
// agent with ActionUpdate is dropped, even if newAgentKeys happens to contain its key.
// ActionUpdate means the file is present and stale — it is not new.
func TestAdmitWorkflowUpdateItem_ExistingAgentUpdate_IsDropped(t *testing.T) {
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		Action: domain.ActionUpdate,
	}
	newAgentKeys := map[string]bool{"test-runner": true} // key present but action is Update

	if admitWorkflowUpdateItem(item, "orchestrator", newAgentKeys, nil) {
		t.Error("existing agent (ActionUpdate): admit=true, want false; " +
			"a non-orchestrator agent with ActionUpdate has a present deployed file and must never be " +
			"re-planned or re-written in the workflow-update flow")
	}
}

// TestAdmitWorkflowUpdateItem_ExistingAgentConflict_IsDropped verifies that a non-orchestrator
// agent with ActionConflict is dropped. A conflict means the file is present and locally
// modified — it is an already-deployed artifact and must not be touched.
func TestAdmitWorkflowUpdateItem_ExistingAgentConflict_IsDropped(t *testing.T) {
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		Action: domain.ActionConflict,
	}
	newAgentKeys := map[string]bool{"test-runner": true}

	if admitWorkflowUpdateItem(item, "orchestrator", newAgentKeys, nil) {
		t.Error("existing agent (ActionConflict): admit=true, want false; " +
			"a locally-modified non-orchestrator agent must not be re-planned — " +
			"the workflow-update flow must never overwrite or prompt for already-deployed agents")
	}
}

// TestAdmitWorkflowUpdateItem_ExistingAgentUnchanged_IsDropped verifies that a non-orchestrator
// agent with ActionUnchanged is dropped. An unchanged agent is present and current — it is
// already deployed and must not be re-written.
func TestAdmitWorkflowUpdateItem_ExistingAgentUnchanged_IsDropped(t *testing.T) {
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		Action: domain.ActionUnchanged,
	}
	newAgentKeys := map[string]bool{"test-runner": true}

	if admitWorkflowUpdateItem(item, "orchestrator", newAgentKeys, nil) {
		t.Error("existing agent (ActionUnchanged): admit=true, want false; " +
			"an already-deployed agent with ActionUnchanged must not be re-planned")
	}
}

// ---------------------------------------------------------------------------
// admitWorkflowUpdateItem — skill items
// ---------------------------------------------------------------------------

// TestAdmitWorkflowUpdateItem_NewSkillCreate_IsAdmitted verifies that a skill item with
// ActionCreate is admitted when its key appears in newSkillKeys.
func TestAdmitWorkflowUpdateItem_NewSkillCreate_IsAdmitted(t *testing.T) {
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"},
		Action: domain.ActionCreate,
	}
	newSkillKeys := map[string]bool{"lean-tdd": true}

	if !admitWorkflowUpdateItem(item, "orchestrator", nil, newSkillKeys) {
		t.Error("new skill (ActionCreate, in newSkillKeys): admit=false, want true; " +
			"a skill required by a new agent and absent from disk must be admitted to the executor")
	}
}

// TestAdmitWorkflowUpdateItem_NewSkillCreate_NotInNewSkillKeys_IsDropped verifies that a
// skill item with ActionCreate is dropped when its key is not in newSkillKeys.
func TestAdmitWorkflowUpdateItem_NewSkillCreate_NotInNewSkillKeys_IsDropped(t *testing.T) {
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"},
		Action: domain.ActionCreate,
	}
	newSkillKeys := map[string]bool{} // lean-tdd not in set

	if admitWorkflowUpdateItem(item, "orchestrator", nil, newSkillKeys) {
		t.Error("skill with ActionCreate but NOT in newSkillKeys: admit=true, want false")
	}
}

// TestAdmitWorkflowUpdateItem_ExistingSkillUpdate_IsDropped verifies that a skill with
// ActionUpdate is dropped even when its key is in newSkillKeys. ActionUpdate means the
// skill file is present; stale skills are not within this flow's scope.
func TestAdmitWorkflowUpdateItem_ExistingSkillUpdate_IsDropped(t *testing.T) {
	item := domain.PlanItem{
		Ref:    domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"},
		Action: domain.ActionUpdate,
	}
	newSkillKeys := map[string]bool{"lean-tdd": true}

	if admitWorkflowUpdateItem(item, "orchestrator", nil, newSkillKeys) {
		t.Error("skill (ActionUpdate, in newSkillKeys): admit=true, want false; " +
			"a skill with ActionUpdate has a present file and must not be re-deployed by this flow")
	}
}

// ---------------------------------------------------------------------------
// admitWorkflowUpdateItem — hook items are always dropped
// ---------------------------------------------------------------------------

// TestAdmitWorkflowUpdateItem_HookItem_IsDropped verifies that hook items are dropped
// regardless of their action. Hook deployment is entirely out of scope for the workflow-update
// flow.
func TestAdmitWorkflowUpdateItem_HookItem_IsDropped(t *testing.T) {
	for _, action := range []domain.PlanAction{
		domain.ActionCreate,
		domain.ActionUpdate,
		domain.ActionUnchanged,
		domain.ActionConflict,
	} {
		item := domain.PlanItem{
			Ref:    domain.ArtifactRef{Kind: domain.ArtifactHook, Key: "some-hook"},
			Action: action,
		}
		newAgentKeys := map[string]bool{"some-hook": true} // key present to rule out that explanation
		newSkillKeys := map[string]bool{"some-hook": true}

		if admitWorkflowUpdateItem(item, "orchestrator", newAgentKeys, newSkillKeys) {
			t.Errorf("hook item (action=%s): admit=true, want false; "+
				"hook artifacts must never reach the executor in the workflow-update flow", action)
		}
	}
}
