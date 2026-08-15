package plan_test

// resolve_orchestrator_exclusion_test.go covers the orchestrator-exclusion mechanism
// added to artifact resolution for the standalone-only deploy mode.
//
// These are TDD RED-phase tests. They specify the intended behaviour of contracts that do not
// yet exist in the implementation. Every test here will fail to compile or will fail at
// runtime until the corresponding implementation tasks (I1.1 through I1.3) are delivered.
//
// OrchestratorExcludedFor (new pure function):
//   - Returns true only for domain.ModeStandaloneOnly
//   - Returns false for every other RunMode, so no pre-existing caller changes behaviour
//
// ResolveArtifactsFrom — ExcludeOrchestrator: true (standalone-only):
//   - Result contains no orchestrator agent (AC1.2)
//   - Result contains the selected standalone agent
//   - Skill set equals exactly the transitive skills of the selected agents (no orchestrator-only bleed)
//   - Holds even when StandaloneAgentIDs is an empty slice (no agents selected)
//
// ResolveArtifactsFrom — ExcludeOrchestrator: false (zero value, every other mode):
//   - Workflow selections keep including the orchestrator (AC1.4)
//   - Utility-agent-only selections keep including the orchestrator (AC1.4)
//   - Infrastructure-agent-only selections keep including the orchestrator (AC1.4)
//   - Hook-only selections keep including the orchestrator (AC1.4)
//   - Empty selections (zero-value Selection) keep including the orchestrator (AC1.4)
//
// ResolveArtifacts (positional legacy form):
//   - ExcludeOrchestrator is implicitly false; orchestrator is always included
//
// plan.Build with ModeStandaloneOnly:
//   - The built plan contains no plan item with Ref.Key == "orchestrator" (AC1.3)
//   - The built plan does contain the selected standalone agent
//   - plan.Build with ModeDeployNew still includes the orchestrator (AC1.4 at build level)

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

// TestOrchestratorExcludedFor_StandaloneOnly_ReturnsTrue verifies that the function returns
// true for domain.ModeStandaloneOnly. This is the only mode that must suppress the orchestrator
// from the resolved artifact set; the function is the single authority so the probe path and
// the plan-build path can never disagree.
func TestOrchestratorExcludedFor_StandaloneOnly_ReturnsTrue(t *testing.T) {
	if !plan.OrchestratorExcludedFor(domain.ModeStandaloneOnly) {
		t.Error("OrchestratorExcludedFor(ModeStandaloneOnly) = false, want true; " +
			"standalone-only runs must not resolve the orchestrator so that a locally-modified " +
			"orchestrator file does not trigger a conflict question and does not appear in the plan")
	}
}

// TestOrchestratorExcludedFor_OtherModes_ReturnsFalse verifies that every RunMode other than
// ModeStandaloneOnly returns false, preserving the always-include behaviour every pre-existing
// caller relies on. A default of false means the zero value is safe for existing call sites.
func TestOrchestratorExcludedFor_OtherModes_ReturnsFalse(t *testing.T) {
	otherModes := []domain.RunMode{
		domain.ModeDeployNew,
		domain.ModeUpdate,
		domain.ModeWorkflowsOnly,
		domain.ModePromote,
		domain.ModeTransformHarness,
		domain.ModeUtilityInfraOnly,
	}
	for _, mode := range otherModes {
		if plan.OrchestratorExcludedFor(mode) {
			t.Errorf("OrchestratorExcludedFor(%q) = true, want false; "+
				"only ModeStandaloneOnly must exclude the orchestrator; "+
				"all other modes must keep including it so no pre-existing behaviour changes (AC1.4)",
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

// ---------------------------------------------------------------------------
// plan.Build with ModeStandaloneOnly — no orchestrator plan item (AC1.3)
// ---------------------------------------------------------------------------

// TestBuild_ModeStandaloneOnly_NoOrchestratorPlanItem verifies that when Build is called with
// Mode = ModeStandaloneOnly, the resulting plan contains no plan item whose Ref.Key is
// "orchestrator". This covers the plan-build half of the invariant: the probe path and the
// build path both use OrchestratorExcludedFor so they cannot disagree. If Build does not
// consult the exclusion predicate, it will call ResolveArtifactsFrom without
// ExcludeOrchestrator=true and include the orchestrator in the plan.
func TestBuild_ModeStandaloneOnly_NoOrchestratorPlanItem(t *testing.T) {
	standalone := makeStandaloneAgent("session-logger")
	cat := &fakeCatalog{
		orchestrator:     makeOrchestrator(),
		standaloneAgents: []domain.Agent{standalone},
	}

	module := newFakeModule()
	p := plan.New()

	in := plan.Input{
		Catalog:            cat,
		Module:             module,
		Mode:               domain.ModeStandaloneOnly,
		WorkspacePath:      "/fake/workspace",
		Scope:              domain.ScopeProject,
		GOOS:               "linux",
		Manifest:           absentSnapshot(),
		StandaloneAgentIDs: []string{"session-logger"},
		Models: map[string]domain.ModelSelection{
			"session-logger": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: nil,
	}

	built, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build with ModeStandaloneOnly: %v", err)
	}

	for _, item := range built.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "orchestrator" {
			t.Errorf("plan item found for orchestrator (action=%v, target=%q) in a ModeStandaloneOnly plan; "+
				"standalone-only plans must contain no orchestrator item (AC1.3); "+
				"if OrchestratorExcludedFor is not wired into Build, ResolveArtifactsFrom is called without "+
				"ExcludeOrchestrator=true and the orchestrator enters the artifact set unconditionally",
				item.Action, item.TargetPath)
		}
	}
}

// TestBuild_ModeStandaloneOnly_StandaloneAgentIsPlanned verifies that the selected standalone
// agent appears in the plan when Mode = ModeStandaloneOnly, confirming that the orchestrator
// exclusion suppresses only the orchestrator and does not silently empty the plan.
func TestBuild_ModeStandaloneOnly_StandaloneAgentIsPlanned(t *testing.T) {
	standalone := makeStandaloneAgent("session-logger")
	cat := &fakeCatalog{
		orchestrator:     makeOrchestrator(),
		standaloneAgents: []domain.Agent{standalone},
	}

	module := newFakeModule()
	p := plan.New()

	in := plan.Input{
		Catalog:            cat,
		Module:             module,
		Mode:               domain.ModeStandaloneOnly,
		WorkspacePath:      "/fake/workspace",
		Scope:              domain.ScopeProject,
		GOOS:               "linux",
		Manifest:           absentSnapshot(),
		StandaloneAgentIDs: []string{"session-logger"},
		Models: map[string]domain.ModelSelection{
			"session-logger": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: nil,
	}

	built, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build with ModeStandaloneOnly: %v", err)
	}

	found := false
	for _, item := range built.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "session-logger" {
			found = true
		}
	}
	if !found {
		t.Error("plan does not contain standalone agent \"session-logger\" in a ModeStandaloneOnly plan; " +
			"orchestrator exclusion must suppress only the orchestrator, not the selected standalone agents")
	}
}

// TestBuild_ModeDeployNew_OrchestratorPlanItemPresent verifies that Build with ModeDeployNew
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
		Mode:          domain.ModeDeployNew,
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
		t.Fatalf("Build with ModeDeployNew: %v", err)
	}

	found := false
	for _, item := range built.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "orchestrator" {
			found = true
		}
	}
	if !found {
		t.Error("plan does not contain orchestrator for ModeDeployNew; " +
			"the orchestrator must remain in the plan for all non-standalone-only modes (AC1.4); " +
			"if OrchestratorExcludedFor is incorrectly returning true for ModeDeployNew, this will fail")
	}
}

// TestBuild_ModeUtilityInfraOnly_OrchestratorPlanItemPresent verifies that Build with
// ModeUtilityInfraOnly still includes the orchestrator, covering the utility/infrastructure
// mode as a representative non-standalone mode (AC1.4).
func TestBuild_ModeUtilityInfraOnly_OrchestratorPlanItemPresent(t *testing.T) {
	infraAgent := makeInfrastructureAgent("checkpoint-manager-git", "checkpoint")
	cat := &fakeCatalog{
		orchestrator: makeOrchestrator(),
		infraAgents:  []domain.Agent{infraAgent},
	}

	module := newFakeModule()
	p := plan.New()

	in := plan.Input{
		Catalog:                cat,
		Module:                 module,
		Mode:                   domain.ModeUtilityInfraOnly,
		WorkspacePath:          "/fake/workspace",
		Scope:                  domain.ScopeProject,
		GOOS:                   "linux",
		Manifest:               absentSnapshot(),
		InfrastructureAgentIDs: []string{"checkpoint-manager-git"},
		Models: map[string]domain.ModelSelection{
			"checkpoint-manager-git": {ModelID: "test-model", Origin: domain.OriginHarnessList},
			"orchestrator":           {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: nil,
	}

	built, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build with ModeUtilityInfraOnly: %v", err)
	}

	found := false
	for _, item := range built.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "orchestrator" {
			found = true
		}
	}
	if !found {
		t.Error("plan does not contain orchestrator for ModeUtilityInfraOnly; " +
			"the orchestrator must remain in plans for all non-standalone-only modes (AC1.4)")
	}
}

// TestBuild_ModeStandaloneOnly_PlanModeIsStandaloneOnly verifies that Build correctly
// propagates the mode into the resulting plan even when the orchestrator exclusion is active.
// This confirms that the mode field is not corrupted by the exclusion logic.
func TestBuild_ModeStandaloneOnly_PlanModeIsStandaloneOnly(t *testing.T) {
	standalone := makeStandaloneAgent("session-logger")
	cat := &fakeCatalog{
		orchestrator:     makeOrchestrator(),
		standaloneAgents: []domain.Agent{standalone},
	}

	module := newFakeModule()
	p := plan.New()

	in := plan.Input{
		Catalog:            cat,
		Module:             module,
		Mode:               domain.ModeStandaloneOnly,
		WorkspacePath:      "/fake/workspace",
		Scope:              domain.ScopeProject,
		GOOS:               "linux",
		Manifest:           absentSnapshot(),
		StandaloneAgentIDs: []string{"session-logger"},
		Models: map[string]domain.ModelSelection{
			"session-logger": {ModelID: "test-model", Origin: domain.OriginHarnessList},
		},
		DeployedState: nil,
	}

	built, err := p.Build(context.Background(), in)
	if err != nil {
		t.Fatalf("Build with ModeStandaloneOnly: %v", err)
	}

	if built.Mode != domain.ModeStandaloneOnly {
		t.Errorf("Plan.Mode = %q, want %q; "+
			"Build must propagate the input Mode into the resulting plan unchanged",
			built.Mode, domain.ModeStandaloneOnly)
	}
}

// All tests above use absentSnapshot() defined in testhelpers_test.go (same package).
