package plan_test

// resolve_subagentids_test.go covers Selection.SubagentIDs resolution and the
// OrchestratorExcludedFor predicate for ModeDeployAgents.
//
// These are TDD RED-phase tests. They specify the intended behaviour of contracts that do not
// yet exist in the implementation. Every test that depends on SubagentIDs handling will fail
// to produce correct results until the corresponding implementation tasks (I3.1, I3.2) are
// delivered.
//
// Selection.SubagentIDs resolution (T3.1):
//   - A named subagent enters the artifact set when its key appears in SubagentIDs
//   - A subagent not named in SubagentIDs is absent from the result
//   - An unknown key in SubagentIDs returns ErrUnknownAgent
//   - SubagentIDs deduplicates with workflow-referenced agents
//   - SubagentIDs deduplicates with InfrastructureAgentIDs
//   - The resulting Agents slice is sorted by key
//   - Required skills of SubagentIDs entries are transitively included
//   - SubagentIDs never causes the orchestrator to enter the set; orchestrator
//     inclusion remains governed by ExcludeOrchestrator
//
// OrchestratorExcludedFor — ModeDeployAgents (T3.1):
//   - OrchestratorExcludedFor(ModeDeployAgents) must return true: the mode never
//     rewrites the orchestrator
//   - All pre-existing modes that were false before remain false (non-regression)
//
// plan.Build with ModeDeployAgents (T3.1):
//   - The built plan contains no plan item with Ref.Key == "orchestrator"
//   - The built plan does contain the selected subagent

import (
	"context"
	"errors"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Helpers specific to subagent-ID tests
// ---------------------------------------------------------------------------

// makeSubagent returns a minimal domain.Agent with role RoleSubagent and no
// Infrastructure class, representing an ordinary (non-infrastructure) subagent.
func makeSubagent(key string) domain.Agent {
	return domain.Agent{
		Key:            key,
		Version:        "1.0",
		Name:           key + "-name",
		Description:    key + "-description",
		Role:           domain.RoleSubagent,
		RequiredSkills: []string{},
		SourcePath:     "/fake/agents/" + key + ".md",
	}
}

// ---------------------------------------------------------------------------
// OrchestratorExcludedFor — ModeDeployAgents
// ---------------------------------------------------------------------------

// TestOrchestratorExcludedFor_DeployAgents_ReturnsTrue verifies that
// OrchestratorExcludedFor returns true for domain.ModeDeployAgents. This is the single
// authority that gates orchestrator inclusion in the probe path and the build path, so
// both agree without additional coordination.
func TestOrchestratorExcludedFor_DeployAgents_ReturnsTrue(t *testing.T) {
	if !plan.OrchestratorExcludedFor(domain.ModeDeployAgents) {
		t.Error("OrchestratorExcludedFor(ModeDeployAgents) = false, want true; " +
			"the deploy-agents mode must never include the orchestrator in its artifact set " +
			"so that no deployed orchestrator file is ever overwritten or conflict-questioned " +
			"by this mode; OrchestratorExcludedFor is the single authority — if it returns false " +
			"here, both ResolveArtifactsFrom and Build will silently include the orchestrator")
	}
}

// TestOrchestratorExcludedFor_DeployHooks_ReturnsTrue verifies that
// OrchestratorExcludedFor returns true for domain.ModeDeployHooks. The hooks-only mode
// also never rewrites the orchestrator.
func TestOrchestratorExcludedFor_DeployHooks_ReturnsTrue(t *testing.T) {
	if !plan.OrchestratorExcludedFor(domain.ModeDeployHooks) {
		t.Error("OrchestratorExcludedFor(ModeDeployHooks) = false, want true; " +
			"the deploy-hooks mode never writes agent content and must not include the " +
			"orchestrator in its artifact set")
	}
}

// TestOrchestratorExcludedFor_AllOtherModes_ReturnFalse verifies that the update to
// OrchestratorExcludedFor for ModeDeployAgents and ModeDeployHooks does not accidentally
// change the return value for workspace-oriented modes. Every workspace-mode caller relies on
// the zero value (false = include orchestrator). ModeStandaloneOnly has been retired in
// Stage 6 and is no longer in the mode set.
func TestOrchestratorExcludedFor_AllOtherModes_ReturnFalse(t *testing.T) {
	otherModes := []domain.RunMode{
		domain.ModeDeployWorkspace,
		domain.ModeUpdateWorkspace,
		domain.ModeUpdateWorkflows,
		domain.ModePromoteToGeneric,
		domain.ModeTransformHarness,
	}
	for _, mode := range otherModes {
		if plan.OrchestratorExcludedFor(mode) {
			t.Errorf("OrchestratorExcludedFor(%q) = true, want false; "+
				"only ModeDeployAgents and ModeDeployHooks must exclude the orchestrator; "+
				"every workspace-oriented mode must preserve today's include behavior (AC3.4)",
				mode)
		}
	}
}

// ---------------------------------------------------------------------------
// ResolveArtifactsFrom — SubagentIDs: basic inclusion
// ---------------------------------------------------------------------------

// TestResolveArtifactsFrom_SubagentIDs_NamedSubagentInArtifactSet verifies that a
// subagent whose key appears in Selection.SubagentIDs is present in the resolved artifact
// set. SubagentIDs is the direct-naming counterpart to workflow-referenced agents for the
// deploy-agents mode.
func TestResolveArtifactsFrom_SubagentIDs_NamedSubagentInArtifactSet(t *testing.T) {
	agent := makeSubagent("contracts-designer")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		SubagentIDs:         []string{"contracts-designer"},
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with SubagentIDs: %v", err)
	}

	if !containsAgent(set.Agents, "contracts-designer") {
		t.Error("ArtifactSet.Agents missing \"contracts-designer\" even though it appeared in SubagentIDs; " +
			"SubagentIDs must bring directly named subagents into the artifact set, " +
			"matching the behavior of InfrastructureAgentIDs and StandaloneAgentIDs for their respective classes")
	}
}

// TestResolveArtifactsFrom_SubagentIDs_NilSubagentIDsDoesNotInclude verifies that a
// worker agent present in the catalog does NOT enter the artifact set when SubagentIDs is
// nil. Subagents are opt-in through SubagentIDs just as utility agents are opt-in through
// UtilityAgentIDs.
func TestResolveArtifactsFrom_SubagentIDs_NilSubagentIDsDoesNotInclude(t *testing.T) {
	agent := makeSubagent("contracts-designer")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		SubagentIDs:         nil, // nil → no direct subagents selected
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with nil SubagentIDs: %v", err)
	}

	if containsAgent(set.Agents, "contracts-designer") {
		t.Error("ArtifactSet.Agents contains \"contracts-designer\" even though SubagentIDs was nil; " +
			"subagents must not enter the artifact set unless their key appears in SubagentIDs, " +
			"just as utility agents do not appear unless selected via UtilityAgentIDs")
	}
}

// ---------------------------------------------------------------------------
// ResolveArtifactsFrom — SubagentIDs: error on unknown key
// ---------------------------------------------------------------------------

// TestResolveArtifactsFrom_SubagentIDs_UnknownID_ReturnsErrUnknownAgent verifies that
// an unknown key in SubagentIDs returns ErrUnknownAgent rather than silently producing a
// smaller artifact set. An unknown reference must be surfaced, matching the contract for
// InfrastructureAgentIDs and StandaloneAgentIDs.
func TestResolveArtifactsFrom_SubagentIDs_UnknownID_ReturnsErrUnknownAgent(t *testing.T) {
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		// No workers — the key below is intentionally absent.
	}

	_, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		SubagentIDs: []string{"this-subagent-does-not-exist"},
	})
	if err == nil {
		t.Fatal("ResolveArtifactsFrom with unknown SubagentID: expected error, got nil; " +
			"an unresolvable SubagentID key is a user error that must be surfaced rather " +
			"than silently producing an artifact set missing the intended agent")
	}
	if !errors.Is(err, plan.ErrUnknownAgent) {
		t.Errorf("ResolveArtifactsFrom with unknown SubagentID: got error %v, want errors.Is(err, plan.ErrUnknownAgent); "+
			"the error must wrap ErrUnknownAgent so callers can detect the specific failure kind "+
			"without string-matching the error message",
			err)
	}
}

// ---------------------------------------------------------------------------
// ResolveArtifactsFrom — SubagentIDs: deduplication
// ---------------------------------------------------------------------------

// TestResolveArtifactsFrom_SubagentIDs_DeduplicatesWithWorkflowAgents verifies that an
// agent referenced by both a workflow and SubagentIDs appears exactly once in the result.
// Deduplication applies across all sources, consistent with the existing behavior for
// utility agents, infrastructure agents, and standalone agents.
func TestResolveArtifactsFrom_SubagentIDs_DeduplicatesWithWorkflowAgents(t *testing.T) {
	agent := makeSubagent("plan-review")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		workflows: []domain.Workflow{
			makeWorkflow("quick-fix", "plan-review"),
		},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		WorkflowIDs: []string{"quick-fix"},
		SubagentIDs: []string{"plan-review"},
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with workflow + SubagentIDs overlap: %v", err)
	}

	count := 0
	for _, a := range set.Agents {
		if a.Key == "plan-review" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("agent \"plan-review\" appears %d times in ArtifactSet.Agents; "+
			"must appear exactly once even when selected via both a workflow reference "+
			"and SubagentIDs (dedup-and-deterministic-order discipline)",
			count)
	}
}

// TestResolveArtifactsFrom_SubagentIDs_MultipleEntries_AllIncluded verifies that when
// multiple SubagentIDs are selected, all appear in the result.
func TestResolveArtifactsFrom_SubagentIDs_MultipleEntries_AllIncluded(t *testing.T) {
	agentA := makeSubagent("implementation-tdd")
	agentB := makeSubagent("plan-review")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agentA, agentB},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		SubagentIDs:         []string{"implementation-tdd", "plan-review"},
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with two SubagentIDs: %v", err)
	}

	for _, key := range []string{"implementation-tdd", "plan-review"} {
		if !containsAgent(set.Agents, key) {
			t.Errorf("ArtifactSet.Agents missing %q; "+
				"all keys listed in SubagentIDs must appear in the resolved agent set",
				key)
		}
	}
}

// ---------------------------------------------------------------------------
// ResolveArtifactsFrom — SubagentIDs: deterministic ordering
// ---------------------------------------------------------------------------

// TestResolveArtifactsFrom_SubagentIDs_AreSortedByKey verifies that subagents added via
// SubagentIDs are included in the sorted-by-key output alongside all other agents. The
// sort is global across all sources, matching existing behavior.
func TestResolveArtifactsFrom_SubagentIDs_AreSortedByKey(t *testing.T) {
	agentZ := makeSubagent("zzz-subagent")
	agentA := makeSubagent("aaa-subagent")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agentZ, agentA},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		SubagentIDs:         []string{"zzz-subagent", "aaa-subagent"},
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with unordered SubagentIDs: %v", err)
	}

	if len(set.Agents) < 2 {
		t.Skip("fewer than 2 agents; cannot check ordering")
	}
	for i := 1; i < len(set.Agents); i++ {
		if set.Agents[i].Key < set.Agents[i-1].Key {
			t.Errorf("ArtifactSet.Agents is not sorted by Key at index %d: %q < %q; "+
				"subagents resolved via SubagentIDs must be sorted alongside all other agents "+
				"to produce a deterministic, stable plan",
				i, set.Agents[i].Key, set.Agents[i-1].Key)
		}
	}
}

// ---------------------------------------------------------------------------
// ResolveArtifactsFrom — SubagentIDs: transitive skills
// ---------------------------------------------------------------------------

// TestResolveArtifactsFrom_SubagentIDs_IncludesTransitiveSkills verifies that a selected
// subagent's required skills are transitively included in ArtifactSet.Skills. Skills are
// collected from the included agent set regardless of how the agent entered that set.
func TestResolveArtifactsFrom_SubagentIDs_IncludesTransitiveSkills(t *testing.T) {
	skill := makeSkill("lean-tdd", "1.0")
	agent := makeSubagent("test-writer-tdd")
	agent.RequiredSkills = []string{"lean-tdd"}

	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
		skills:       []domain.Skill{skill},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		SubagentIDs:         []string{"test-writer-tdd"},
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with SubagentIDs requiring skills: %v", err)
	}

	if !containsSkill(set.Skills, "lean-tdd") {
		t.Error("ArtifactSet.Skills missing \"lean-tdd\"; " +
			"skills transitively required by agents selected via SubagentIDs must be " +
			"included in the resolved skill set, matching the transitive closure contract "+
			"that applies to all other agent selection paths")
	}
}

// ---------------------------------------------------------------------------
// ResolveArtifactsFrom — SubagentIDs with ExcludeOrchestrator
// ---------------------------------------------------------------------------

// TestResolveArtifactsFrom_SubagentIDs_ExcludeOrchestrator_NoOrchestrator verifies that
// setting ExcludeOrchestrator=true with SubagentIDs selected still suppresses the
// orchestrator. This is the exact configuration that ModeDeployAgents uses:
// OrchestratorExcludedFor(ModeDeployAgents)=true, selection contains SubagentIDs.
func TestResolveArtifactsFrom_SubagentIDs_ExcludeOrchestrator_NoOrchestrator(t *testing.T) {
	agent := makeSubagent("contracts-designer")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		SubagentIDs:         []string{"contracts-designer"},
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with SubagentIDs + ExcludeOrchestrator: %v", err)
	}

	for _, a := range set.Agents {
		if a.Key == "orchestrator" {
			t.Errorf("ArtifactSet.Agents contains orchestrator even though ExcludeOrchestrator was true; "+
				"the deploy-agents mode sets ExcludeOrchestrator=true so the orchestrator must "+
				"never enter the artifact set — a present orchestrator file would otherwise trigger "+
				"a conflict question or plan item for a mode that should not touch it (AC3.4)")
		}
	}
}

// ---------------------------------------------------------------------------
// plan.Build with ModeDeployAgents — no orchestrator plan item
// ---------------------------------------------------------------------------

// TestBuild_ModeDeployAgents_NoOrchestratorPlanItem verifies that Build with
// Mode=ModeDeployAgents produces no plan item whose Ref.Key is "orchestrator". This covers
// the plan-build half of the invariant; the probe path is covered by
// TestOrchestratorExcludedFor_DeployAgents_ReturnsTrue. Both paths consult
// OrchestratorExcludedFor so they cannot disagree once it is wired in.
func TestBuild_ModeDeployAgents_NoOrchestratorPlanItem(t *testing.T) {
	agent := makeSubagent("contracts-designer")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
	}

	module := newFakeModule()
	p := plan.New()

	in := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeDeployAgents,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		SubagentIDs:   []string{"contracts-designer"},
		Models: map[string]domain.ModelSelection{
			"contracts-designer": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: nil,
	}

	built, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build with ModeDeployAgents: %v", err)
	}

	for _, item := range built.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "orchestrator" {
			t.Errorf("plan item found for orchestrator (action=%v, target=%q) in a ModeDeployAgents plan; "+
				"the deploy-agents mode must never include the orchestrator in its plan — "+
				"if OrchestratorExcludedFor is not wired into Build for ModeDeployAgents, "+
				"ResolveArtifactsFrom is called without ExcludeOrchestrator=true and the "+
				"orchestrator enters the artifact set unconditionally",
				item.Action, item.TargetPath)
		}
	}
}

// TestBuild_ModeDeployAgents_SubagentIsPlanned verifies that the selected subagent appears
// in the plan when Mode=ModeDeployAgents, confirming that orchestrator exclusion suppresses
// only the orchestrator and not the selected subagents.
func TestBuild_ModeDeployAgents_SubagentIsPlanned(t *testing.T) {
	agent := makeSubagent("contracts-designer")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{agent},
	}

	module := newFakeModule()
	p := plan.New()

	in := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeDeployAgents,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		SubagentIDs:   []string{"contracts-designer"},
		Models: map[string]domain.ModelSelection{
			"contracts-designer": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: nil,
	}

	built, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build with ModeDeployAgents: %v", err)
	}

	found := false
	for _, item := range built.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "contracts-designer" {
			found = true
		}
	}
	if !found {
		t.Error("plan does not contain subagent \"contracts-designer\" in a ModeDeployAgents plan; " +
			"orchestrator exclusion must suppress only the orchestrator, not the selected subagents; " +
			"if SubagentIDs is not forwarded into Selection by Build, the subagent silently vanishes")
	}
}
