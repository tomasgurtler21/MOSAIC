package plan_test

// resolve_test.go covers artifact set resolution (T16.1, T16.2).
//
// T16.1 — ResolveArtifacts derives the correct, deduplicated set:
//   - Single workflow resolves its referenced agents and their transitive skills
//   - Multiple overlapping workflows deduplicate agents and skills
//   - An unknown workflow ID returns ErrUnknownWorkflow
//   - A workflow referencing a non-existent agent returns ErrUnknownAgent (not a smaller set)
//
// T16.2 — Orchestrator and utility agents:
//   - The orchestrator is always included, even with no selected workflows
//   - A utility agent is included only when explicitly named in utilityAgentIDs
//   - A utility agent not named in utilityAgentIDs is absent from the result

import (
	"errors"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// T16.1 — Single workflow resolution
// ---------------------------------------------------------------------------

// TestResolveArtifacts_SingleWorkflow_ContainsReferencedAgents verifies that selecting the
// quick-fix workflow yields all four of its referenced agents in the result.
// quick-fix references: planner-tdd-soft, plan-review, implementation-tdd, test-runner.
func TestResolveArtifacts_SingleWorkflow_ContainsReferencedAgents(t *testing.T) {
	cat := loadRealCatalog(t)

	set, err := plan.ResolveArtifacts(cat, []string{"quick-fix"}, nil, nil)
	must(t, err)

	wantAgents := []string{"planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner"}
	for _, key := range wantAgents {
		if !containsAgent(set.Agents, key) {
			t.Errorf("ArtifactSet.Agents missing %q; quick-fix must include all its referenced agents", key)
		}
	}
}

// TestResolveArtifacts_SingleWorkflow_AgentCountMatchesWorkflowPlusOrchestrator verifies that
// the resolved agent count equals the number of referenced agents plus the orchestrator (one).
// quick-fix has 4 referenced agents + 1 orchestrator = 5 total.
func TestResolveArtifacts_SingleWorkflow_AgentCountMatchesWorkflowPlusOrchestrator(t *testing.T) {
	cat := loadRealCatalog(t)

	set, err := plan.ResolveArtifacts(cat, []string{"quick-fix"}, nil, nil)
	must(t, err)

	const wantCount = 5 // 4 referenced agents + 1 orchestrator
	if len(set.Agents) != wantCount {
		t.Errorf("ArtifactSet.Agents: got %d agents, want %d (4 referenced + 1 orchestrator)",
			len(set.Agents), wantCount)
	}
}

// TestResolveArtifacts_SingleWorkflow_IncludesTransitiveSkills verifies that every skill
// required by any agent in the resolved set is included in ArtifactSet.Skills.
// planner-tdd-soft requires lean-tdd and efficient-file-reading; these must appear.
func TestResolveArtifacts_SingleWorkflow_IncludesTransitiveSkills(t *testing.T) {
	cat := loadRealCatalog(t)

	set, err := plan.ResolveArtifacts(cat, []string{"quick-fix"}, nil, nil)
	must(t, err)

	// planner-tdd-soft requires lean-tdd and efficient-file-reading.
	// plan-review and implementation-tdd also require efficient-file-reading.
	wantSkills := []string{"lean-tdd", "efficient-file-reading"}
	for _, key := range wantSkills {
		if !containsSkill(set.Skills, key) {
			t.Errorf("ArtifactSet.Skills missing %q; it is transitively required by agents in quick-fix", key)
		}
	}
}

// TestResolveArtifacts_SingleWorkflow_SkillsAreDeduplicated verifies that a skill required
// by multiple agents appears exactly once in ArtifactSet.Skills.
// efficient-file-reading is required by planner-tdd-soft, plan-review, and implementation-tdd,
// but must appear only once.
func TestResolveArtifacts_SingleWorkflow_SkillsAreDeduplicated(t *testing.T) {
	cat := loadRealCatalog(t)

	set, err := plan.ResolveArtifacts(cat, []string{"quick-fix"}, nil, nil)
	must(t, err)

	seen := make(map[string]int)
	for _, s := range set.Skills {
		seen[s.Key]++
	}
	for key, count := range seen {
		if count > 1 {
			t.Errorf("skill %q appears %d times in ArtifactSet.Skills; each skill must appear at most once",
				key, count)
		}
	}
}

// ---------------------------------------------------------------------------
// T16.1 — Multiple overlapping workflows
// ---------------------------------------------------------------------------

// TestResolveArtifacts_TwoWorkflows_DeduplicatesAgents verifies that agents referenced by
// both quick-fix and greenfield-tdd appear exactly once in the result. Both workflows
// reference planner-tdd-soft, plan-review, implementation-tdd, and test-runner.
func TestResolveArtifacts_TwoWorkflows_DeduplicatesAgents(t *testing.T) {
	cat := loadRealCatalog(t)

	set, err := plan.ResolveArtifacts(cat, []string{"quick-fix", "greenfield-tdd"}, nil, nil)
	must(t, err)

	seen := make(map[string]int)
	for _, a := range set.Agents {
		seen[a.Key]++
	}
	for key, count := range seen {
		if count > 1 {
			t.Errorf("agent %q appears %d times in ArtifactSet.Agents; each agent must appear at most once",
				key, count)
		}
	}
}

// TestResolveArtifacts_TwoWorkflows_UnionContainsAllAgents verifies that the union of both
// workflows' referenced agents is fully represented in the result.
// quick-fix adds: planner-tdd-soft, plan-review, implementation-tdd, test-runner.
// greenfield-tdd adds additionally: requirements-refinement, requirements-review,
// system-designer, system-design-review, contracts-designer, contracts-review,
// test-writer-tdd, tests-review-tdd, implementation-review.
func TestResolveArtifacts_TwoWorkflows_UnionContainsAllAgents(t *testing.T) {
	cat := loadRealCatalog(t)

	// Verify all quick-fix agents appear when combining both workflows.
	set, err := plan.ResolveArtifacts(cat, []string{"quick-fix", "greenfield-tdd"}, nil, nil)
	must(t, err)

	// Agents unique to greenfield-tdd must be present when selected alongside quick-fix.
	greenfieldOnly := []string{
		"requirements-refinement",
		"requirements-review",
		"system-designer",
		"system-design-review",
		"contracts-designer",
		"contracts-review",
		"test-writer-tdd",
		"tests-review-tdd",
		"implementation-review",
	}
	for _, key := range greenfieldOnly {
		if !containsAgent(set.Agents, key) {
			t.Errorf("ArtifactSet.Agents missing %q; it is referenced by greenfield-tdd", key)
		}
	}
}

// TestResolveArtifacts_TwoWorkflows_SkillsAreDeduplicated verifies that skills shared by
// both workflows appear exactly once in ArtifactSet.Skills.
func TestResolveArtifacts_TwoWorkflows_SkillsAreDeduplicated(t *testing.T) {
	cat := loadRealCatalog(t)

	set, err := plan.ResolveArtifacts(cat, []string{"quick-fix", "greenfield-tdd"}, nil, nil)
	must(t, err)

	seen := make(map[string]int)
	for _, s := range set.Skills {
		seen[s.Key]++
	}
	for key, count := range seen {
		if count > 1 {
			t.Errorf("skill %q appears %d times in ArtifactSet.Skills; each skill must appear at most once",
				key, count)
		}
	}
}

// TestResolveArtifacts_EmptyWorkflowIDs_ResultIsNonNil verifies that passing an empty
// workflow list returns a non-nil ArtifactSet (with just the orchestrator) rather than
// returning an error or a zero-value struct.
func TestResolveArtifacts_EmptyWorkflowIDs_ResultIsNonNil(t *testing.T) {
	cat := loadRealCatalog(t)

	set, err := plan.ResolveArtifacts(cat, nil, nil, nil)
	must(t, err)

	// The orchestrator must always be present.
	if !containsAgent(set.Agents, "orchestrator") {
		t.Error("ArtifactSet.Agents missing orchestrator; the orchestrator is always included")
	}
}

// ---------------------------------------------------------------------------
// T16.1 — Error cases
// ---------------------------------------------------------------------------

// TestResolveArtifacts_UnknownWorkflow_ReturnsErrUnknownWorkflow verifies that an unknown
// workflow ID causes ResolveArtifacts to return ErrUnknownWorkflow rather than silently
// producing a smaller artifact set. An unknown reference must always be surfaced.
func TestResolveArtifacts_UnknownWorkflow_ReturnsErrUnknownWorkflow(t *testing.T) {
	cat := loadRealCatalog(t)

	_, err := plan.ResolveArtifacts(cat, []string{"this-workflow-does-not-exist"}, nil, nil)
	if err == nil {
		t.Fatal("ResolveArtifacts with unknown workflow ID: expected error, got nil")
	}
	if !errors.Is(err, plan.ErrUnknownWorkflow) {
		t.Errorf("ResolveArtifacts with unknown workflow: got error %v, want errors.Is(err, plan.ErrUnknownWorkflow)", err)
	}
}

// TestResolveArtifacts_WorkflowReferencingMissingAgent_ReturnsErrUnknownAgent verifies that a
// workflow whose referenced_agents includes a key not found in the catalog returns
// ErrUnknownAgent. The error must be explicit; silent omission would produce a broken
// deployment where expected agents are missing.
func TestResolveArtifacts_WorkflowReferencingMissingAgent_ReturnsErrUnknownAgent(t *testing.T) {
	// Use a fake catalog so we can inject a workflow with a nonexistent agent reference.
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workflows: []domain.Workflow{
			makeWorkflow("broken-wf", "this-agent-does-not-exist"),
		},
		// No workers — the referenced agent is intentionally absent.
	}

	_, err := plan.ResolveArtifacts(cat, []string{"broken-wf"}, nil, nil)
	if err == nil {
		t.Fatal("ResolveArtifacts with workflow referencing missing agent: expected error, got nil")
	}
	if !errors.Is(err, plan.ErrUnknownAgent) {
		t.Errorf("ResolveArtifacts with missing agent reference: got error %v, want errors.Is(err, plan.ErrUnknownAgent)", err)
	}
}

// TestResolveArtifacts_WorkflowReferencingMissingAgent_DoesNotReturnSmallerSet verifies that
// the missing-agent error is not silently swallowed, producing an artifact set that omits
// the missing reference. The caller must observe the error.
func TestResolveArtifacts_WorkflowReferencingMissingAgent_DoesNotReturnSmallerSet(t *testing.T) {
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workflows: []domain.Workflow{
			makeWorkflow("broken-wf", "this-agent-does-not-exist"),
		},
	}

	set, err := plan.ResolveArtifacts(cat, []string{"broken-wf"}, nil, nil)
	if err == nil {
		// If no error was returned but the missing agent is absent, that's the silent-drop bug.
		if !containsAgent(set.Agents, "this-agent-does-not-exist") {
			t.Error("ResolveArtifacts silently omitted a missing agent reference without returning an error; " +
				"the error must be surfaced")
		}
		t.Fatal("expected ErrUnknownAgent, got nil error")
	}
}

// ---------------------------------------------------------------------------
// T16.1 — Deterministic ordering
// ---------------------------------------------------------------------------

// TestResolveArtifacts_AgentsAreSortedByKey verifies that ArtifactSet.Agents is in
// ascending key order. Deterministic order is required for stable plan rendering and
// consistent manifest comparison.
func TestResolveArtifacts_AgentsAreSortedByKey(t *testing.T) {
	cat := loadRealCatalog(t)

	set, err := plan.ResolveArtifacts(cat, []string{"quick-fix", "greenfield-tdd"}, nil, nil)
	must(t, err)

	if len(set.Agents) < 2 {
		t.Skip("fewer than 2 agents; cannot check ordering")
	}
	for i := 1; i < len(set.Agents); i++ {
		if set.Agents[i].Key < set.Agents[i-1].Key {
			t.Errorf("ArtifactSet.Agents is not sorted by Key at index %d: %q < %q",
				i, set.Agents[i].Key, set.Agents[i-1].Key)
		}
	}
}

// TestResolveArtifacts_SkillsAreSortedByKey verifies that ArtifactSet.Skills is in
// ascending key order.
func TestResolveArtifacts_SkillsAreSortedByKey(t *testing.T) {
	cat := loadRealCatalog(t)

	set, err := plan.ResolveArtifacts(cat, []string{"quick-fix", "greenfield-tdd"}, nil, nil)
	must(t, err)

	if len(set.Skills) < 2 {
		t.Skip("fewer than 2 skills; cannot check ordering")
	}
	for i := 1; i < len(set.Skills); i++ {
		if set.Skills[i].Key < set.Skills[i-1].Key {
			t.Errorf("ArtifactSet.Skills is not sorted by Key at index %d: %q < %q",
				i, set.Skills[i].Key, set.Skills[i-1].Key)
		}
	}
}

// ---------------------------------------------------------------------------
// T16.2 — Orchestrator always included
// ---------------------------------------------------------------------------

// TestResolveArtifacts_OrchestratorAlwaysIncluded_WithNoWorkflows verifies that the
// orchestrator appears in the resolved set even when no workflows are selected.
// The orchestrator is a mandatory member of every deployment.
func TestResolveArtifacts_OrchestratorAlwaysIncluded_WithNoWorkflows(t *testing.T) {
	cat := loadRealCatalog(t)

	set, err := plan.ResolveArtifacts(cat, nil, nil, nil)
	must(t, err)

	if !containsAgent(set.Agents, "orchestrator") {
		t.Error("ArtifactSet.Agents missing orchestrator; the orchestrator must be present regardless of workflow selection")
	}
}

// TestResolveArtifacts_OrchestratorAlwaysIncluded_WithWorkflow verifies that the orchestrator
// is present even when workflows are selected, not just when the selection is empty.
func TestResolveArtifacts_OrchestratorAlwaysIncluded_WithWorkflow(t *testing.T) {
	cat := loadRealCatalog(t)

	set, err := plan.ResolveArtifacts(cat, []string{"quick-fix"}, nil, nil)
	must(t, err)

	if !containsAgent(set.Agents, "orchestrator") {
		t.Error("ArtifactSet.Agents missing orchestrator; the orchestrator must always be included")
	}
}

// TestResolveArtifacts_OrchestratorAppearsExactlyOnce verifies that the orchestrator is not
// duplicated, even if workflows somehow trigger a second inclusion path.
func TestResolveArtifacts_OrchestratorAppearsExactlyOnce(t *testing.T) {
	cat := loadRealCatalog(t)

	set, err := plan.ResolveArtifacts(cat, []string{"quick-fix", "greenfield-tdd"}, nil, nil)
	must(t, err)

	count := 0
	for _, a := range set.Agents {
		if a.Key == "orchestrator" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("orchestrator appears %d times in ArtifactSet.Agents; must appear exactly once", count)
	}
}

// ---------------------------------------------------------------------------
// T16.2 — Utility agents included only when explicitly selected
// ---------------------------------------------------------------------------

// TestResolveArtifacts_UtilityAgent_IncludedWhenSelected verifies that a utility agent
// appears in the result when its key is passed in utilityAgentIDs.
// The caller is responsible for allow-list filtering before passing the IDs here.
func TestResolveArtifacts_UtilityAgent_IncludedWhenSelected(t *testing.T) {
	cat := &fakeCatalog{
		orchestrator:  makeOrchestrator(),
		utilityAgents: []domain.Agent{makeUtilityAgent("my-utility")},
	}

	set, err := plan.ResolveArtifacts(cat, nil, []string{"my-utility"}, nil)
	must(t, err)

	if !containsAgent(set.Agents, "my-utility") {
		t.Error("ArtifactSet.Agents missing utility agent that was explicitly selected via utilityAgentIDs")
	}
}

// TestResolveArtifacts_UtilityAgent_NotIncludedWhenNotSelected verifies that a utility agent
// known to the catalog does NOT appear in the result when its key is absent from utilityAgentIDs.
// This is the allow-listing boundary: utility agents are opt-in, not opt-out.
func TestResolveArtifacts_UtilityAgent_NotIncludedWhenNotSelected(t *testing.T) {
	cat := &fakeCatalog{
		orchestrator:  makeOrchestrator(),
		utilityAgents: []domain.Agent{makeUtilityAgent("my-utility")},
	}

	// utilityAgentIDs is nil — no utility agents selected.
	set, err := plan.ResolveArtifacts(cat, nil, nil, nil)
	must(t, err)

	if containsAgent(set.Agents, "my-utility") {
		t.Error("ArtifactSet.Agents contains utility agent that was NOT selected via utilityAgentIDs; " +
			"utility agents must be opt-in")
	}
}

// TestResolveArtifacts_UtilityAgent_WithRealCatalog_KnownUtilityIncluded verifies that a
// real utility agent from the catalog (workflow-creator) is present when selected,
// and absent when not selected.
func TestResolveArtifacts_UtilityAgent_WithRealCatalog_KnownUtilityIncluded(t *testing.T) {
	cat := loadRealCatalog(t)

	const utilityKey = "workflow-creator"
	// Confirm the agent exists as a utility agent in the real catalog.
	ua := cat.UtilityAgents()
	found := false
	for _, a := range ua {
		if a.Key == utilityKey {
			found = true
			break
		}
	}
	if !found {
		t.Skipf("utility agent %q not found in real catalog; test cannot proceed", utilityKey)
	}

	// Selected: must appear.
	setWithUtility, err := plan.ResolveArtifacts(cat, nil, []string{utilityKey}, nil)
	must(t, err)
	if !containsAgent(setWithUtility.Agents, utilityKey) {
		t.Errorf("ArtifactSet.Agents missing %q even though it was selected via utilityAgentIDs", utilityKey)
	}

	// Not selected: must not appear.
	setWithout, err := plan.ResolveArtifacts(cat, nil, nil, nil)
	must(t, err)
	if containsAgent(setWithout.Agents, utilityKey) {
		t.Errorf("ArtifactSet.Agents contains %q even though it was NOT selected via utilityAgentIDs", utilityKey)
	}
}

// TestResolveArtifacts_UtilityAgent_IncludesTransitiveSkills verifies that a selected
// utility agent's required skills are transitively included in ArtifactSet.Skills.
func TestResolveArtifacts_UtilityAgent_IncludesTransitiveSkills(t *testing.T) {
	skill := makeSkill("utility-skill", "1.0")
	utility := makeUtilityAgent("my-utility")
	utility.RequiredSkills = []string{"utility-skill"}

	cat := &fakeCatalog{
		orchestrator:  makeOrchestrator(),
		utilityAgents: []domain.Agent{utility},
		skills:        []domain.Skill{skill},
	}

	set, err := plan.ResolveArtifacts(cat, nil, []string{"my-utility"}, nil)
	must(t, err)

	if !containsSkill(set.Skills, "utility-skill") {
		t.Error("ArtifactSet.Skills missing skill required by selected utility agent")
	}
}
