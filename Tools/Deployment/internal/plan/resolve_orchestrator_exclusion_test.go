package plan_test

// resolve_orchestrator_exclusion_test.go covers the orchestrator-exclusion mechanism
// in artifact resolution. After Stage 6 retires ModeStandaloneOnly and ModeUtilityInfraOnly,
// OrchestratorExcludedFor returns true only for ModeDeployAgents and ModeDeployHooks and
// false for all workspace-oriented modes.
//
// OrchestratorExcludedFor (pure mode → bool predicate):
//   - Returns true for ModeDeployAgents and ModeDeployHooks
//   - Returns false for all workspace-oriented modes (deploy-workspace, update-workspace,
//     update-workflows, promote-to-generic, transform-harness)
//
// ResolveArtifactsFrom — ExcludeOrchestrator: true (deploy-agents / deploy-hooks path):
//   - Result contains no orchestrator agent (AC1.2)
//   - Result contains the selected standalone agent
//   - Skill set equals exactly the transitive skills of the selected agents
//   - Holds even when StandaloneAgentIDs is an empty slice
//
// ResolveArtifactsFrom — ExcludeOrchestrator: false (zero value, workspace modes):
//   - Workflow selections keep including the orchestrator (AC1.4)
//   - Utility-agent-only selections keep including the orchestrator (AC1.4)
//   - Infrastructure-agent-only selections keep including the orchestrator (AC1.4)
//   - Hook-only selections keep including the orchestrator (AC1.4)
//   - Empty selections (zero-value Selection) keep including the orchestrator (AC1.4)
//
// ResolveArtifacts (positional legacy form):
//   - ExcludeOrchestrator is implicitly false; orchestrator is always included
//
// plan.Build with workspace modes:
//   - ModeDeployWorkspace includes the orchestrator (AC1.4)
//   - ModeUpdateWorkspace includes the orchestrator (AC1.4)

import (
	"context"
	"testing"

	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Fixture helpers — standalone-specific additions to testhelpers_test.go
// ---------------------------------------------------------------------------

// makeStandaloneAgent is defined in staleness_role_gate_test.go (same package) and is used
// directly throughout this file. No local redeclaration is needed.

// ---------------------------------------------------------------------------
// OrchestratorExcludedFor — pure mode → bool predicate
// ---------------------------------------------------------------------------

// TestOrchestratorExcludedFor_WorkspaceModes_ReturnsFalse verifies that workspace-oriented
// modes (deploy-workspace, update-workspace, update-workflows, promote-to-generic,
// transform-harness) return false from OrchestratorExcludedFor, preserving the always-include
// behaviour every pre-existing caller relies on. Only the agent-deploy modes (deploy-agents,
// deploy-hooks) return true; the retired standalone-only mode is no longer in the set.
func TestOrchestratorExcludedFor_WorkspaceModes_ReturnsFalse(t *testing.T) {
	workspaceModes := []domain.RunMode{
		domain.ModeDeployWorkspace,
		domain.ModeUpdateWorkspace,
		domain.ModeUpdateWorkflows,
		domain.ModePromoteToGeneric,
		domain.ModeTransformHarness,
	}
	for _, mode := range workspaceModes {
		if plan.OrchestratorExcludedFor(mode) {
			t.Errorf("OrchestratorExcludedFor(%q) = true, want false; "+
				"workspace-mode runs must include the orchestrator in their artifact set; "+
				"only deploy-agents and deploy-hooks must exclude it (AC1.4)",
				mode)
		}
	}
}

// ---------------------------------------------------------------------------
// ResolveArtifactsFrom — ExcludeOrchestrator: true (standalone-only path)
// ---------------------------------------------------------------------------

// TestResolveArtifactsFrom_ExcludeOrchestrator_NoOrchestrator verifies that setting
// ExcludeOrchestrator to true suppresses the orchestrator from the resolved artifact set
// even when standalone agents are selected. The orchestrator must not appear under any key
// in ArtifactSet.Agents when the field is true (AC1.2).
func TestResolveArtifactsFrom_ExcludeOrchestrator_NoOrchestrator(t *testing.T) {
	standalone := makeStandaloneAgent("session-logger")
	cat := &fakeCatalog{
		orchestrator:     makeOrchestrator(),
		standaloneAgents: []domain.Agent{standalone},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		StandaloneAgentIDs:  []string{"session-logger"},
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with ExcludeOrchestrator=true: %v", err)
	}

	for _, a := range set.Agents {
		if a.Key == "orchestrator" {
			t.Errorf("ArtifactSet.Agents contains orchestrator (key=%q) even though ExcludeOrchestrator was true; "+
				"a standalone-only resolution must never include the orchestrator in its artifact set (AC1.2); "+
				"if ExcludeOrchestrator is not yet implemented, the orchestrator will appear unconditionally "+
				"because ResolveArtifactsFrom currently seeds the set with c.Orchestrator() before any selection",
				a.Key)
		}
	}
}

// TestResolveArtifactsFrom_ExcludeOrchestrator_StandaloneAgentPresent verifies that the
// selected standalone agent is present in the result when ExcludeOrchestrator is true.
// Excluding the orchestrator must not silently empty the set or suppress other agents.
func TestResolveArtifactsFrom_ExcludeOrchestrator_StandaloneAgentPresent(t *testing.T) {
	standalone := makeStandaloneAgent("session-logger")
	cat := &fakeCatalog{
		orchestrator:     makeOrchestrator(),
		standaloneAgents: []domain.Agent{standalone},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		StandaloneAgentIDs:  []string{"session-logger"},
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with ExcludeOrchestrator=true: %v", err)
	}

	if !containsAgent(set.Agents, "session-logger") {
		t.Error("ArtifactSet.Agents missing selected standalone agent \"session-logger\"; " +
			"ExcludeOrchestrator must suppress only the orchestrator, not the selected standalone agents")
	}
}

// TestResolveArtifactsFrom_ExcludeOrchestrator_SkillSetExcludesOrchestratorOnlySkills verifies
// that when ExcludeOrchestrator is true, the resolved skill set contains exactly the skills
// transitively required by the selected standalone agents — skills required solely by the
// excluded orchestrator must not bleed into the result. This guards against the risk noted in
// the plan: if the orchestrator is logically excluded but its skills still resolve, the plan
// would contain skill artifacts that no included agent uses.
func TestResolveArtifactsFrom_ExcludeOrchestrator_SkillSetExcludesOrchestratorOnlySkills(t *testing.T) {
	orchestratorOnlySkill := makeSkill("orc-only-skill", "1.0")
	standaloneSkill := makeSkill("standalone-skill", "1.0")

	orc := makeOrchestrator()
	orc.RequiredSkills = []string{"orc-only-skill"}

	standalone := makeStandaloneAgent("session-logger")
	standalone.RequiredSkills = []string{"standalone-skill"}

	cat := &fakeCatalog{
		orchestrator:     orc,
		standaloneAgents: []domain.Agent{standalone},
		skills:           []domain.Skill{orchestratorOnlySkill, standaloneSkill},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		StandaloneAgentIDs:  []string{"session-logger"},
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with ExcludeOrchestrator=true: %v", err)
	}

	// The standalone agent's required skill must be present.
	if !containsSkill(set.Skills, "standalone-skill") {
		t.Error("ArtifactSet.Skills missing \"standalone-skill\"; " +
			"skills required by selected standalone agents must always be included")
	}

	// The orchestrator-only skill must be absent: the orchestrator was excluded, so no included
	// agent requires this skill. The skill set is the transitive closure of included agents only.
	if containsSkill(set.Skills, "orc-only-skill") {
		t.Error("ArtifactSet.Skills contains \"orc-only-skill\" even though ExcludeOrchestrator was true; " +
			"the skill set of an exclusion run is exactly the transitive closure of the selected agents — " +
			"orchestrator-only skills must not appear when the orchestrator is excluded")
	}
}

// TestResolveArtifactsFrom_ExcludeOrchestrator_EmptyStandaloneIDs_EmptyAgentSet verifies that
// ExcludeOrchestrator=true with an empty StandaloneAgentIDs yields an empty agent set. This
// represents a standalone run with zero selected agents: no orchestrator, no other agents.
func TestResolveArtifactsFrom_ExcludeOrchestrator_EmptyStandaloneIDs_EmptyAgentSet(t *testing.T) {
	cat := &fakeCatalog{
		orchestrator:     makeOrchestrator(),
		standaloneAgents: []domain.Agent{makeStandaloneAgent("session-logger")},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		StandaloneAgentIDs:  []string{},
		ExcludeOrchestrator: true,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with ExcludeOrchestrator=true and empty IDs: %v", err)
	}

	if containsAgent(set.Agents, "orchestrator") {
		t.Error("ArtifactSet.Agents contains orchestrator with ExcludeOrchestrator=true and empty StandaloneAgentIDs; " +
			"ExcludeOrchestrator must suppress the orchestrator unconditionally, even when no other agents are selected")
	}
	if len(set.Agents) != 0 {
		t.Errorf("ArtifactSet.Agents = %d agents, want 0; "+
			"an excluded orchestrator and no selected agents must produce an empty agent set",
			len(set.Agents))
	}
}

// ---------------------------------------------------------------------------
// ResolveArtifactsFrom — ExcludeOrchestrator: false (existing callers unchanged, AC1.4)
// ---------------------------------------------------------------------------

// TestResolveArtifactsFrom_WorkflowSelection_OrchestratorPresent verifies that a workflow-driven
// resolution with ExcludeOrchestrator=false still includes the orchestrator. The default zero
// value (false) must preserve today's always-include behaviour for every pre-existing caller.
func TestResolveArtifactsFrom_WorkflowSelection_OrchestratorPresent(t *testing.T) {
	worker := makeAgent("test-runner", "1.0")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{worker},
		workflows:    []domain.Workflow{makeWorkflow("quick-fix", "test-runner")},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		WorkflowIDs:         []string{"quick-fix"},
		ExcludeOrchestrator: false,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom workflow selection: %v", err)
	}

	if !containsAgent(set.Agents, "orchestrator") {
		t.Error("ArtifactSet.Agents missing orchestrator for workflow selection with ExcludeOrchestrator=false; " +
			"workflow runs must keep including the orchestrator — the zero value of ExcludeOrchestrator is include (AC1.4)")
	}
}

// TestResolveArtifactsFrom_UtilitySelection_OrchestratorPresent verifies that a
// utility-agent-only resolution still includes the orchestrator (AC1.4).
func TestResolveArtifactsFrom_UtilitySelection_OrchestratorPresent(t *testing.T) {
	cat := &fakeCatalog{
		orchestrator:  makeOrchestrator(),
		utilityAgents: []domain.Agent{makeUtilityAgent("my-utility")},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		UtilityAgentIDs:     []string{"my-utility"},
		ExcludeOrchestrator: false,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom utility selection: %v", err)
	}

	if !containsAgent(set.Agents, "orchestrator") {
		t.Error("ArtifactSet.Agents missing orchestrator for utility-agent selection with ExcludeOrchestrator=false; " +
			"utility/infrastructure runs must keep including the orchestrator (AC1.4)")
	}
}

// TestResolveArtifactsFrom_InfraSelection_OrchestratorPresent verifies that an
// infrastructure-agent-only resolution still includes the orchestrator (AC1.4).
func TestResolveArtifactsFrom_InfraSelection_OrchestratorPresent(t *testing.T) {
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		infraAgents:  []domain.Agent{makeInfrastructureAgent("checkpoint-manager-git", "checkpoint")},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		InfrastructureAgentIDs: []string{"checkpoint-manager-git"},
		ExcludeOrchestrator:    false,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom infrastructure selection: %v", err)
	}

	if !containsAgent(set.Agents, "orchestrator") {
		t.Error("ArtifactSet.Agents missing orchestrator for infrastructure selection with ExcludeOrchestrator=false; " +
			"infrastructure runs must keep including the orchestrator (AC1.4)")
	}
}

// TestResolveArtifactsFrom_HookSelection_OrchestratorPresent verifies that a hook-only
// resolution still includes the orchestrator (AC1.4).
func TestResolveArtifactsFrom_HookSelection_OrchestratorPresent(t *testing.T) {
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		hooks:        []domain.HookBundle{makeHookBundle("my-hooks", "1.0")},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		HookIDs:             []string{"my-hooks"},
		ExcludeOrchestrator: false,
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom hook selection: %v", err)
	}

	if !containsAgent(set.Agents, "orchestrator") {
		t.Error("ArtifactSet.Agents missing orchestrator for hook selection with ExcludeOrchestrator=false; " +
			"hook runs must keep including the orchestrator (AC1.4)")
	}
}

// TestResolveArtifactsFrom_ZeroValueSelection_OrchestratorPresent verifies that a zero-value
// Selection (all fields nil/false) still includes the orchestrator. The zero value must be
// the safe default that preserves today's always-include behaviour (AC1.4).
func TestResolveArtifactsFrom_ZeroValueSelection_OrchestratorPresent(t *testing.T) {
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom zero-value selection: %v", err)
	}

	if !containsAgent(set.Agents, "orchestrator") {
		t.Error("ArtifactSet.Agents missing orchestrator for zero-value Selection; " +
			"the zero value of ExcludeOrchestrator (false) must include the orchestrator — " +
			"every pre-existing caller that constructs Selection without the new field must be unaffected (AC1.4)")
	}
}

// ---------------------------------------------------------------------------
// ResolveArtifacts (positional legacy form) — orchestrator always included
// ---------------------------------------------------------------------------

// TestResolveArtifacts_LegacyForm_AlwaysIncludesOrchestrator verifies that the positional
// legacy form ResolveArtifacts leaves ExcludeOrchestrator at its zero value (false) and
// therefore always includes the orchestrator. No existing caller changes behaviour because
// the new field cannot be set through this form.
func TestResolveArtifacts_LegacyForm_AlwaysIncludesOrchestrator(t *testing.T) {
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
	}

	set, err := plan.ResolveArtifacts(cat, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("ResolveArtifacts legacy positional form: %v", err)
	}

	if !containsAgent(set.Agents, "orchestrator") {
		t.Error("ResolveArtifacts (positional legacy form) returned a set without the orchestrator; " +
			"the legacy form must not gain ExcludeOrchestrator behaviour — its zero ExcludeOrchestrator means include")
	}
}

// TestBuild_ModeDeployNew_OrchestratorPlanItemPresent verifies that Build with ModeDeployWorkspace
// still includes the orchestrator in the plan, confirming the exclusion is strictly mode-gated.
// This is the negative case that pins AC1.4 at the plan-build level.
func TestBuild_ModeDeployNew_OrchestratorPlanItemPresent(t *testing.T) {
	worker := makeAgent("test-runner", "1.0")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{worker},
		workflows:    []domain.Workflow{makeWorkflow("quick-fix", "test-runner")},
	}

	module := newFakeModule()
	p := plan.New()

	in := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeDeployWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		WorkflowIDs:   []string{"quick-fix"},
		Models: map[string]domain.ModelSelection{
			"test-runner":  {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: nil,
	}

	built, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build with ModeDeployWorkspace: %v", err)
	}

	found := false
	for _, item := range built.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "orchestrator" {
			found = true
		}
	}
	if !found {
		t.Error("plan does not contain orchestrator for ModeDeployWorkspace; " +
			"the orchestrator must remain in the plan for all non-standalone-only modes (AC1.4); " +
			"if OrchestratorExcludedFor is incorrectly returning true for ModeDeployWorkspace, this will fail")
	}
}

// TestBuild_ModeUpdateWorkspace_OrchestratorPlanItemPresent verifies that Build with
// ModeUpdateWorkspace still includes the orchestrator, confirming the exclusion is strictly
// mode-gated to deploy-agents and deploy-hooks only. This covers a representative workspace
// mode (AC1.4) and complements the ModeDeployWorkspace test above.
func TestBuild_ModeUpdateWorkspace_OrchestratorPlanItemPresent(t *testing.T) {
	worker := makeAgent("test-runner", "1.0")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{worker},
		workflows:    []domain.Workflow{makeWorkflow("quick-fix", "test-runner")},
	}

	module := newFakeModule()
	p := plan.New()

	in := plan.Input{
		Catalog:       cat,
		Module:        module,
		Mode:          domain.ModeUpdateWorkspace,
		WorkspacePath: "/fake/workspace",
		Scope:         domain.ScopeProject,
		GOOS:          "linux",
		Manifest:      absentSnapshot(),
		WorkflowIDs:   []string{"quick-fix"},
		Models: map[string]domain.ModelSelection{
			"test-runner":  {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: nil,
	}

	built, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build with ModeUpdateWorkspace: %v", err)
	}

	found := false
	for _, item := range built.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "orchestrator" {
			found = true
		}
	}
	if !found {
		t.Error("plan does not contain orchestrator for ModeUpdateWorkspace; " +
			"the orchestrator must remain in plans for all workspace-oriented modes (AC1.4); " +
			"only deploy-agents and deploy-hooks exclude the orchestrator")
	}
}

// All tests above use absentSnapshot() defined in testhelpers_test.go (same package).
