package plan_test

// resolve_orchestrator_exclusion_test.go covers the orchestrator-exclusion mechanism
// in artifact resolution. OrchestratorExcludedFor returns true for ModeDeployAgents,
// ModeDeployHooks, and ModeUpdateWorkspace; false for all other workspace-oriented modes.
//
// OrchestratorExcludedFor (pure mode → bool predicate):
//   - Returns true for ModeDeployAgents, ModeDeployHooks, and ModeUpdateWorkspace
//   - Returns false for deploy-workspace, update-workflows, promote-to-generic, transform-harness
//
// ResolveArtifactsFrom — ExcludeOrchestrator: true (deploy-agents / deploy-hooks / update-workspace path):
//   - Result contains no orchestrator agent when ScannedAgentKeys is empty
//   - Orchestrator is present when ScannedAgentKeys contains its key (AC1.3)
//   - Orchestrator is absent when ScannedAgentKeys does not contain its key (AC1.4)
//   - Result contains the selected standalone agent
//   - Skill set equals exactly the transitive skills of the selected agents
//   - Holds even when StandaloneAgentIDs is an empty slice
//
// ResolveArtifactsFrom — ExcludeOrchestrator: false (zero value, workspace modes):
//   - Workflow selections keep including the orchestrator
//   - Utility-agent-only selections keep including the orchestrator
//   - Infrastructure-agent-only selections keep including the orchestrator
//   - Hook-only selections keep including the orchestrator
//   - Empty selections (zero-value Selection) keep including the orchestrator
//
// ResolveArtifacts (positional legacy form):
//   - ExcludeOrchestrator is implicitly false; orchestrator is always included
//
// plan.Build with workspace modes:
//   - ModeDeployWorkspace includes the orchestrator
//   - ModeUpdateWorkspace includes the orchestrator when ScannedAgentKeys contains its key (AC1.6)

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
// modes that still force-include the orchestrator (deploy-workspace, update-workflows,
// promote-to-generic, transform-harness) return false from OrchestratorExcludedFor.
// update-workspace is not in this list — it returns true so the orchestrator enters only via
// ScannedAgentKeys when the workspace scan discovers it on disk.
func TestOrchestratorExcludedFor_WorkspaceModes_ReturnsFalse(t *testing.T) {
	workspaceModes := []domain.RunMode{
		domain.ModeDeployWorkspace,
		domain.ModeUpdateWorkflows,
		domain.ModePromoteToGeneric,
		domain.ModeTransformHarness,
	}
	for _, mode := range workspaceModes {
		if plan.OrchestratorExcludedFor(mode) {
			t.Errorf("OrchestratorExcludedFor(%q) = true, want false; "+
				"this workspace mode must force-include the orchestrator in its artifact set; "+
				"only deploy-agents, deploy-hooks, and update-workspace must exclude forced inclusion",
				mode)
		}
	}
}

// TestOrchestratorExcludedFor_UpdateWorkspace_ReturnsTrue verifies that ModeUpdateWorkspace
// returns true from OrchestratorExcludedFor. When excluded from forced inclusion, the
// orchestrator can still enter the artifact set via ScannedAgentKeys if the workspace scan
// found it on disk — the same path every other deployed agent uses.
func TestOrchestratorExcludedFor_UpdateWorkspace_ReturnsTrue(t *testing.T) {
	if !plan.OrchestratorExcludedFor(domain.ModeUpdateWorkspace) {
		t.Error("OrchestratorExcludedFor(ModeUpdateWorkspace) = false, want true; " +
			"update-workspace must not force-include the orchestrator: when the orchestrator is " +
			"absent from the workspace, the forced inclusion incorrectly adds it to the artifact set; " +
			"the orchestrator should enter only via ScannedAgentKeys when the workspace scan finds it on disk")
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
// ResolveArtifactsFrom — ExcludeOrchestrator: true + ScannedAgentKeys (update-workspace path)
// ---------------------------------------------------------------------------

// TestResolveArtifactsFrom_ExcludeOrchestrator_ScannedOrchestratorPresent verifies that when
// ExcludeOrchestrator is true and ScannedAgentKeys contains the orchestrator key, the
// orchestrator is included in the artifact set. This is the update-workspace path: the
// orchestrator is not force-included, but enters via the workspace scan result (AC1.3).
func TestResolveArtifactsFrom_ExcludeOrchestrator_ScannedOrchestratorPresent(t *testing.T) {
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		ExcludeOrchestrator: true,
		ScannedAgentKeys:    []string{"orchestrator"},
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with ExcludeOrchestrator=true and orchestrator in ScannedAgentKeys: %v", err)
	}

	if !containsAgent(set.Agents, "orchestrator") {
		t.Error("ArtifactSet.Agents does not contain orchestrator even though ScannedAgentKeys includes its key; " +
			"when ExcludeOrchestrator is true the orchestrator must still enter via ScannedAgentKeys — " +
			"this is the update-workspace path where the workspace scan discovered the orchestrator on disk (AC1.3)")
	}
}

// TestResolveArtifactsFrom_ExcludeOrchestrator_OrchestratorAbsentWhenNotScanned verifies that
// when ExcludeOrchestrator is true and ScannedAgentKeys does not contain the orchestrator key,
// the orchestrator is absent from the artifact set. This represents an update-workspace run
// where the orchestrator is not deployed in the workspace (AC1.4).
func TestResolveArtifactsFrom_ExcludeOrchestrator_OrchestratorAbsentWhenNotScanned(t *testing.T) {
	worker := makeAgent("test-runner", "1.0")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		workers:      []domain.Agent{worker},
	}

	set, err := plan.ResolveArtifactsFrom(cat, plan.Selection{
		ExcludeOrchestrator: true,
		ScannedAgentKeys:    []string{"test-runner"},
	})
	if err != nil {
		t.Fatalf("ResolveArtifactsFrom with ExcludeOrchestrator=true and orchestrator absent from ScannedAgentKeys: %v", err)
	}

	if containsAgent(set.Agents, "orchestrator") {
		t.Error("ArtifactSet.Agents contains orchestrator even though ScannedAgentKeys does not include it; " +
			"when ExcludeOrchestrator is true and the orchestrator key is absent from ScannedAgentKeys, " +
			"the orchestrator must not appear in the artifact set — this represents an update-workspace run " +
			"where the orchestrator is not deployed in the workspace (AC1.4)")
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
// ModeUpdateWorkspace includes the orchestrator in the plan when ScannedAgentKeys contains
// the orchestrator key. For update-workspace, OrchestratorExcludedFor returns true so the
// orchestrator is not force-included; it must enter via ScannedAgentKeys (AC1.6).
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
		Catalog:          cat,
		Module:           module,
		Mode:             domain.ModeUpdateWorkspace,
		WorkspacePath:    "/fake/workspace",
		Scope:            domain.ScopeProject,
		GOOS:             "linux",
		Manifest:         absentSnapshot(),
		WorkflowIDs:      []string{"quick-fix"},
		ScannedAgentKeys: []string{"orchestrator"},
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
		t.Error("plan does not contain orchestrator for ModeUpdateWorkspace with orchestrator in ScannedAgentKeys; " +
			"when the workspace scan discovers the orchestrator on disk it must appear in the built plan (AC1.6); " +
			"if OrchestratorExcludedFor now correctly returns true for ModeUpdateWorkspace, the orchestrator " +
			"is no longer force-included and must enter via ScannedAgentKeys instead")
	}
}

// All tests above use absentSnapshot() defined in testhelpers_test.go (same package).
