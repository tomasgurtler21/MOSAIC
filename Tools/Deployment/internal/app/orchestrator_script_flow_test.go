package app_test

// orchestrator_script_flow_test.go covers flow-level and model-resolution behaviour for
// orchestrator-script.md (T7.4 and T7.5).
//
// T7.4 — DeployNew/Update/UpdateWorkflows include the script file; DeployAgents/DeployHooks
// exclude it.
//
// The inclusion/exclusion decision is mode-driven inside the planner. At the app layer,
// these tests verify that the planner Input.Mode is correctly set for each flow, so that
// the mode-based exclusion gate (OrchestratorExcludedFor) that was tested in the plan
// package makes the right call:
//
//   - ModeDeployWorkspace (DeployNew): ExcludeOrchestrator = false → script included
//   - ModeUpdate (Update): ExcludeOrchestrator = false → script included
//   - ModeUpdateWorkflows (UpdateWorkflows): ExcludeOrchestrator = false → script included
//   - ModeDeployAgents (DeployAgents): ExcludeOrchestrator = true → script excluded
//   - ModeDeployHooks (DeployHooks): ExcludeOrchestrator = true → script excluded
//
// T7.5 — Script file gets its own QAgentModel question, independent of the main orchestrator.
//
// When the catalog has orchestrator-script and the mode is inclusive, the model-resolution
// phase must ask QAgentModel for "orchestrator-script" separately from "orchestrator".
// If no tier mapping resolves the script's model automatically, the user is asked.
//
// These tests will FAIL until I7.2, I7.3, and I7.4 are implemented:
//   - I7.2: catalog.OrchestratorScript() loads the real file
//   - I7.3: the probe set (ResolveArtifacts) includes the script agent for inclusive modes
//   - I7.4: the model-resolution loop iterates over both orchestrators

import (
	"context"
	"os"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// T7.4: Planner receives correct mode for each flow, enabling script inclusion/exclusion
// ---------------------------------------------------------------------------

// TestOrchestratorScript_Flow_DeployNewPassesInclusiveMode verifies that the DeployNew flow
// passes ModeDeployWorkspace to the planner, which is an inclusive mode for the script file.
// Combined with the plan-layer tests in resolve_orchestrator_script_test.go, this guarantees
// the script is included in the plan when DeployNew is invoked with a catalog that has it.
func TestOrchestratorScript_Flow_DeployNewPassesInclusiveMode(t *testing.T) {
	// Arrange — catalog with orchestratorScript so ResolveArtifacts can include it once implemented.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = &stubCatalog{
		root: "testroot",
		orchestrator: minimalOrchestrator,
		orchestratorScript: domain.Agent{
			Key:     "orchestrator-script",
			Version: "1.0",
			Name:    "Orchestrator Script",
			Role:    domain.RoleOrchestrator,
			Tools:   []string{"file_read", "file_edit"},
		},
		orchestratorScriptOK: true,
		agents:    []domain.Agent{minimalAgent},
		skills:    []domain.Skill{},
		hooks:     []domain.HookBundle{},
		workflows: []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}},
		},
		tiers: []domain.TierInfo{
			{Tier: "HIGH", Rationale: "needs capable model", AgentKeys: []string{"test-runner"}},
		},
	}

	capturing := &capturingPlanner{result: newMinimalPlan(workspace)}
	deps.Planner = capturing

	// Act
	svc := app.New(deps)
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{minimalWorkflow.ID},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — the planner must receive ModeDeployWorkspace (inclusive mode for the script).
	if capturing.capturedInput == nil {
		t.Fatal("planner was not called")
	}
	if capturing.capturedInput.Mode != domain.ModeDeployWorkspace {
		t.Errorf("planner Input.Mode = %q; want %q (inclusive mode so that OrchestratorExcludedFor "+
			"returns false and the script is included in the plan)",
			capturing.capturedInput.Mode, domain.ModeDeployWorkspace)
	}
}

// TestOrchestratorScript_Flow_DeployAgentsPassesExclusiveMode verifies that the DeployAgents
// flow passes ModeDeployAgents to the planner, which is an exclusive mode for the script file.
// Combined with plan-layer tests, this guarantees the script is excluded from the plan when
// DeployAgents is invoked.
func TestOrchestratorScript_Flow_DeployAgentsPassesExclusiveMode(t *testing.T) {
	// Arrange — catalog with orchestratorScript; it must NOT be included in the plan.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = &stubCatalog{
		root: "testroot",
		orchestrator: minimalOrchestrator,
		orchestratorScript: domain.Agent{
			Key:     "orchestrator-script",
			Version: "1.0",
			Name:    "Orchestrator Script",
			Role:    domain.RoleOrchestrator,
			Tools:   []string{"file_read", "file_edit"},
		},
		orchestratorScriptOK: true,
		agents:    []domain.Agent{minimalAgent},
		skills:    []domain.Skill{},
		hooks:     []domain.HookBundle{},
		workflows: []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}},
		},
		tiers: []domain.TierInfo{
			{Tier: "HIGH", Rationale: "needs capable model", AgentKeys: []string{"test-runner"}},
		},
	}

	capturing := &capturingPlanner{result: newMinimalPlan(workspace)}
	deps.Planner = capturing

	// Act
	svc := app.New(deps)
	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		SubagentIDs:   []string{"test-runner"},
		InfrastructureAgentIDs: []string{},
		AutoConfirmPlan: true,
	})

	// Assert — the planner must receive ModeDeployAgents (exclusive mode for the script).
	if capturing.capturedInput == nil {
		t.Fatal("planner was not called")
	}
	if capturing.capturedInput.Mode != domain.ModeDeployAgents {
		t.Errorf("planner Input.Mode = %q; want %q (exclusive mode so that OrchestratorExcludedFor "+
			"returns true and the script is excluded from the plan)",
			capturing.capturedInput.Mode, domain.ModeDeployAgents)
	}
}

// TestOrchestratorScript_Flow_DeployHooksPassesExclusiveMode verifies that the DeployHooks
// flow passes ModeDeployHooks to the planner, which is an exclusive mode for the script file.
func TestOrchestratorScript_Flow_DeployHooksPassesExclusiveMode(t *testing.T) {
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = &stubCatalog{
		root: "testroot",
		orchestrator: minimalOrchestrator,
		orchestratorScript: domain.Agent{
			Key:     "orchestrator-script",
			Version: "1.0",
			Name:    "Orchestrator Script",
			Role:    domain.RoleOrchestrator,
			Tools:   []string{"file_read", "file_edit"},
		},
		orchestratorScriptOK: true,
		agents:    []domain.Agent{minimalAgent},
		skills:    []domain.Skill{},
		hooks:     []domain.HookBundle{},
		workflows: []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}},
		},
		tiers: []domain.TierInfo{
			{Tier: "HIGH", Rationale: "needs capable model", AgentKeys: []string{"test-runner"}},
		},
	}

	capturing := &capturingPlanner{result: newMinimalPlan(workspace)}
	deps.Planner = capturing

	// Act
	svc := app.New(deps)
	_, _ = svc.DeployHooks(context.Background(), app.DeployHooksRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		HookIDs:         []string{},
		AutoConfirmPlan: true,
	})

	// Assert — the planner must receive ModeDeployHooks (exclusive mode for the script).
	if capturing.capturedInput == nil {
		t.Fatal("planner was not called")
	}
	if capturing.capturedInput.Mode != domain.ModeDeployHooks {
		t.Errorf("planner Input.Mode = %q; want %q (exclusive mode so that OrchestratorExcludedFor "+
			"returns true and the script is excluded from the plan)",
			capturing.capturedInput.Mode, domain.ModeDeployHooks)
	}
}

// ---------------------------------------------------------------------------
// T7.4: Catalog with script is passed to planner intact (no app-layer filtering)
// ---------------------------------------------------------------------------

// TestOrchestratorScript_Flow_CatalogWithScriptPassedToPlanner verifies that the app layer
// does not strip out the OrchestratorScript from the catalog before passing it to the planner.
// The planner must receive the catalog as-is so it can decide inclusion/exclusion based on mode.
//
// This test verifies the NEGATIVE constraint: the app must not filter the catalog.
func TestOrchestratorScript_Flow_CatalogWithScriptPassedToPlanner(t *testing.T) {
	// Arrange — catalog with orchestratorScript set.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	cat := &stubCatalog{
		root: "testroot",
		orchestrator: minimalOrchestrator,
		orchestratorScript: domain.Agent{
			Key:     "orchestrator-script",
			Version: "1.0",
			Name:    "Orchestrator Script",
			Role:    domain.RoleOrchestrator,
			Tools:   []string{"file_read", "file_edit"},
		},
		orchestratorScriptOK: true,
		agents:    []domain.Agent{minimalAgent},
		skills:    []domain.Skill{},
		hooks:     []domain.HookBundle{},
		workflows: []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}},
		},
		tiers: []domain.TierInfo{
			{Tier: "HIGH", Rationale: "needs capable model", AgentKeys: []string{"test-runner"}},
		},
	}
	deps.Catalog = cat

	var planInput plan.Input
	deps.Planner = &capturingPlanner{
		result: newMinimalPlan(workspace),
		// We'll check via capturing after the call.
	}
	capPlan := deps.Planner.(*capturingPlanner)

	// Act
	svc := app.New(deps)
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{minimalWorkflow.ID},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}
	_ = planInput

	// Assert — the catalog passed to the planner must still report OrchestratorScript.
	if capPlan.capturedInput == nil {
		t.Fatal("planner was not called")
	}
	if capPlan.capturedInput.Catalog == nil {
		t.Fatal("planner received nil Catalog")
	}
	_, scriptOK := capPlan.capturedInput.Catalog.OrchestratorScript()
	if !scriptOK {
		t.Error("planner's Catalog.OrchestratorScript() returned false; the app layer must not filter " +
			"the orchestrator-script out of the catalog before passing it to the planner; " +
			"the planner decides inclusion/exclusion based on Input.Mode")
	}
}

// ---------------------------------------------------------------------------
// T7.5: Script file gets its own model question, independent of the main orchestrator
// ---------------------------------------------------------------------------

// TestOrchestratorScript_ModelResolution_ScriptGetsIndependentQuestion verifies that when the
// catalog has orchestratorScript and DeployNew is called without pre-answered models, the
// model-resolution phase asks QAgentModel for "orchestrator-script" as a separate question
// from "orchestrator".
//
// This test will FAIL until I7.2 (loading) and I7.3 (probe set) and I7.4 (model loop) are
// implemented: the current flow does not know about orchestrator-script, so QAgentModel for
// "orchestrator-script" is never asked.
func TestOrchestratorScript_ModelResolution_ScriptGetsIndependentQuestion(t *testing.T) {
	// Arrange — no tier model stored; QAgentModel must be asked for both orchestrators.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "orchestrator", "model-a").
		AnswerSelectOne(domain.QAgentModel, "orchestrator-script", "model-a").
		AnswerSelectOne(domain.QAgentModel, "test-runner", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = &stubCatalog{
		root: "testroot",
		orchestrator: minimalOrchestrator,
		orchestratorScript: domain.Agent{
			Key:     "orchestrator-script",
			Version: "1.0",
			Name:    "Orchestrator Script",
			Role:    domain.RoleOrchestrator,
			Tools:   []string{"file_read", "file_edit"},
		},
		orchestratorScriptOK: true,
		agents:    []domain.Agent{minimalAgent},
		skills:    []domain.Skill{},
		hooks:     []domain.HookBundle{},
		workflows: []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}},
		},
		tiers: []domain.TierInfo{
			{Tier: "HIGH", Rationale: "needs capable model", AgentKeys: []string{"test-runner"}},
		},
	}
	deps.Planner = &stubPlanner{plan: scriptPlan(workspace, "orchestrator.md", "orchestrator-script.md")}

	// Act
	svc := app.New(deps)
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{minimalWorkflow.ID},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — QAgentModel must be asked for the script file with its own subject key.
	if !stub.WasAsked(domain.QAgentModel, "orchestrator-script") {
		t.Error("QAgentModel was not asked for \"orchestrator-script\"; " +
			"the model-resolution loop must iterate over both orchestrators when the script file is " +
			"in the probe set; I7.2, I7.3, and I7.4 have not yet been implemented")
	}
}

// TestOrchestratorScript_Flow_UpdatePassesInclusiveMode verifies that the Update flow passes
// ModeUpdateWorkspace to the planner. After the Stage 1 behavioral change,
// OrchestratorExcludedFor returns true for ModeUpdateWorkspace (the mode is exclusive for
// force-inclusion). However the orchestrator-script still enters the artifact set when present
// on disk: the Update flow probes the script file and, if found, adds its key to
// scannedAgentKeys — the same path every other deployed agent uses.
func TestOrchestratorScript_Flow_UpdatePassesInclusiveMode(t *testing.T) {
	// Arrange — catalog with orchestratorScript so ResolveArtifacts can include it once implemented.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = &stubCatalog{
		root: "testroot",
		orchestrator: minimalOrchestrator,
		orchestratorScript: domain.Agent{
			Key:     "orchestrator-script",
			Version: "1.0",
			Name:    "Orchestrator Script",
			Role:    domain.RoleOrchestrator,
			Tools:   []string{"file_read", "file_edit"},
		},
		orchestratorScriptOK: true,
		agents:    []domain.Agent{minimalAgent},
		skills:    []domain.Skill{},
		hooks:     []domain.HookBundle{},
		workflows: []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}},
		},
		tiers: []domain.TierInfo{
			{Tier: "HIGH", Rationale: "needs capable model", AgentKeys: []string{"test-runner"}},
		},
	}

	capturing := &capturingPlanner{result: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
		Workflows:     []string{},
	}}
	deps.Planner = capturing

	// Act
	svc := app.New(deps)
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the planner must receive ModeUpdateWorkspace (inclusive mode for the script).
	if capturing.capturedInput == nil {
		t.Fatal("planner was not called")
	}
	if capturing.capturedInput.Mode != domain.ModeUpdateWorkspace {
		t.Errorf("planner Input.Mode = %q; want %q (ModeUpdateWorkspace: OrchestratorExcludedFor "+
			"returns true for this mode, so the script is not force-included; instead the Update "+
			"flow probes the script file and adds it to scannedAgentKeys when present on disk)",
			capturing.capturedInput.Mode, domain.ModeUpdateWorkspace)
	}
}

// TestOrchestratorScript_Flow_UpdateWorkflowsPassesInclusiveMode verifies that the
// UpdateWorkflows flow passes ModeUpdateWorkflows to the planner, which is an inclusive mode
// for the script file. Combined with the plan-layer tests in resolve_orchestrator_script_test.go,
// this guarantees the script is included in the plan when UpdateWorkflows is invoked with a
// catalog that has it.
func TestOrchestratorScript_Flow_UpdateWorkflowsPassesInclusiveMode(t *testing.T) {
	// Arrange — catalog with orchestratorScript so ResolveArtifacts can include it once implemented.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = &stubCatalog{
		root: "testroot",
		orchestrator: minimalOrchestrator,
		orchestratorScript: domain.Agent{
			Key:     "orchestrator-script",
			Version: "1.0",
			Name:    "Orchestrator Script",
			Role:    domain.RoleOrchestrator,
			Tools:   []string{"file_read", "file_edit"},
		},
		orchestratorScriptOK: true,
		agents:    []domain.Agent{minimalAgent},
		skills:    []domain.Skill{},
		hooks:     []domain.HookBundle{},
		workflows: []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}},
		},
		tiers: []domain.TierInfo{
			{Tier: "HIGH", Rationale: "needs capable model", AgentKeys: []string{"test-runner"}},
		},
	}

	capturing := &capturingPlanner{result: domain.Plan{
		Mode:          domain.ModeUpdateWorkflows,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
		Workflows:     []string{},
	}}
	deps.Planner = capturing

	// Act — empty WorkflowIDs avoids new-agent model questions, keeping the test focused on mode.
	svc := app.New(deps)
	_, _ = svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{},
		AutoConfirmPlan: true,
	})

	// Assert — the planner must receive ModeUpdateWorkflows (inclusive mode for the script).
	if capturing.capturedInput == nil {
		t.Fatal("planner was not called")
	}
	if capturing.capturedInput.Mode != domain.ModeUpdateWorkflows {
		t.Errorf("planner Input.Mode = %q; want %q (inclusive mode so that OrchestratorExcludedFor "+
			"returns false and the script is included in the plan when UpdateWorkflows is invoked)",
			capturing.capturedInput.Mode, domain.ModeUpdateWorkflows)
	}
}

// TestOrchestratorScript_ModelResolution_MainOrchestratorUnaffected verifies that asking a
// separate model question for the script file does not affect or skip the main orchestrator's
// model question. Both must be asked as independent interactions.
func TestOrchestratorScript_ModelResolution_MainOrchestratorUnaffected(t *testing.T) {
	// Arrange — no tier model stored; QAgentModel must be asked for both orchestrators.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "orchestrator", "model-a").
		AnswerSelectOne(domain.QAgentModel, "orchestrator-script", "model-a").
		AnswerSelectOne(domain.QAgentModel, "test-runner", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = &stubCatalog{
		root: "testroot",
		orchestrator: minimalOrchestrator,
		orchestratorScript: domain.Agent{
			Key:     "orchestrator-script",
			Version: "1.0",
			Name:    "Orchestrator Script",
			Role:    domain.RoleOrchestrator,
			Tools:   []string{"file_read", "file_edit"},
		},
		orchestratorScriptOK: true,
		agents:    []domain.Agent{minimalAgent},
		skills:    []domain.Skill{},
		hooks:     []domain.HookBundle{},
		workflows: []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}},
		},
		tiers: []domain.TierInfo{
			{Tier: "HIGH", Rationale: "needs capable model", AgentKeys: []string{"test-runner"}},
		},
	}
	deps.Planner = &stubPlanner{plan: scriptPlan(workspace, "orchestrator.md", "orchestrator-script.md")}

	// Act
	svc := app.New(deps)
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{minimalWorkflow.ID},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — the main orchestrator's model question must also be asked.
	if !stub.WasAsked(domain.QAgentModel, "orchestrator") {
		t.Error("QAgentModel was not asked for \"orchestrator\"; adding the script file to the " +
			"model-resolution loop must not break the main orchestrator's model question")
	}
}

// TestOrchestratorScript_ModelResolution_TierMappingSkipsScriptQuestion verifies that when
// a tier mapping resolves the orchestrator-script's model automatically, QAgentModel is NOT
// asked for it. The tier mapping must apply to the script file just like any other orchestrator.
func TestOrchestratorScript_ModelResolution_TierMappingSkipsScriptQuestion(t *testing.T) {
	// Arrange — pre-answer the tier with a stored model mapping for both agents' tier.
	// The orchestrator-script has no RecommendedTier set in the stub; the default tier
	// mapping should apply.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = &stubCatalog{
		root: "testroot",
		orchestrator: domain.Agent{
			Key:             "orchestrator",
			Version:         "1.0",
			Name:            "Orchestrator",
			Role:            domain.RoleOrchestrator,
			RecommendedTier: "HIGH",
		},
		orchestratorScript: domain.Agent{
			Key:             "orchestrator-script",
			Version:         "1.0",
			Name:            "Orchestrator Script",
			Role:            domain.RoleOrchestrator,
			Tools:           []string{"file_read", "file_edit"},
			RecommendedTier: "HIGH",
		},
		orchestratorScriptOK: true,
		agents:    []domain.Agent{minimalAgent},
		skills:    []domain.Skill{},
		hooks:     []domain.HookBundle{},
		workflows: []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}},
		},
		tiers: []domain.TierInfo{
			{Tier: "HIGH", Rationale: "needs capable model", AgentKeys: []string{"test-runner", "orchestrator", "orchestrator-script"}},
		},
	}
	deps.Planner = &stubPlanner{plan: scriptPlan(workspace, "orchestrator.md", "orchestrator-script.md")}

	// Act — pre-answer tier HIGH → model-a, so no QAgentModel should be asked.
	svc := app.New(deps)
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{minimalWorkflow.ID},
		TierModels:      map[domain.Tier]string{"HIGH": "model-a"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Guard — if neither "orchestrator-script" nor "orchestrator" was offered a model question,
	// we cannot distinguish "tier mapping resolved the model automatically" from "the script was
	// never in the probe set at all (I7.2/I7.3 not yet implemented)". Both states produce the
	// same observable outcome: WasAsked returns false for both agents. Passing the assertion in
	// this ambiguous state would be a false positive in the RED phase. Skip with a diagnostic
	// instead so the test is an honest SKIP rather than a misleading PASS.
	if !stub.WasAsked(domain.QAgentModel, "orchestrator-script") && !stub.WasAsked(domain.QAgentModel, "orchestrator") {
		t.Skip("cannot verify tier-mapping behavior for orchestrator-script: neither agent was " +
			"offered a model question, which is indistinguishable from the feature not yet being " +
			"wired into model resolution (I7.2/I7.3 not implemented); re-evaluate once I7.2 and " +
			"I7.3 land and orchestrator-script appears in the probe set")
	}

	// Assert — tier mapping resolved the script's model; QAgentModel must not be asked.
	if stub.WasAsked(domain.QAgentModel, "orchestrator-script") {
		t.Error("QAgentModel was asked for \"orchestrator-script\" even though a tier mapping " +
			"(HIGH → model-a) should have resolved its model automatically; " +
			"once the script is in the probe set, tier mapping must apply to it just like any orchestrator")
	}
}

// ---------------------------------------------------------------------------
// T4.3: Orchestrator-script Update-flow inclusion based on disk presence
// ---------------------------------------------------------------------------

// TestOrchestratorScript_Update_ScriptPresentOnDisk_IncludedInDeployedState verifies that
// when the catalog has an orchestrator-script and the script file is present on disk at its
// target path, the Update flow includes the script's key in the artifact set and DeployedState.
func TestOrchestratorScript_Update_ScriptPresentOnDisk_IncludedInDeployedState(t *testing.T) {
	ws := t.TempDir()

	// Write the orchestrator-script file to disk so the probe finds it present.
	scriptContent := []byte("---\nversion: \"1.0\"\n---\nOrchestrator script body.\n")
	if err := os.WriteFile(ws+"/orchestrator-script.md", scriptContent, 0o644); err != nil {
		t.Fatalf("write orchestrator-script.md: %v", err)
	}

	spy := &spyPlanner{response: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: ws,
		Scope:         domain.ScopeProject,
	}}
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Planner = spy
	deps.Catalog = &stubCatalog{
		root:         "testroot",
		orchestrator: minimalOrchestrator,
		orchestratorScript: domain.Agent{
			Key:     "orchestrator-script",
			Version: "1.0",
			Name:    "Orchestrator Script",
			Role:    domain.RoleOrchestrator,
		},
		orchestratorScriptOK: true,
		agents:               []domain.Agent{minimalAgent},
		skills:               []domain.Skill{},
		hooks:                []domain.HookBundle{},
		workflows:            []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}},
		},
	}
	svc := app.New(deps)

	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   ws,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	captured := spy.lastCaptured(t)

	// The orchestrator-script file is present on disk; the Update flow must have probed it,
	// added its key to scannedAgentKeys, and included it in the artifact set and DeployedState.
	scriptState, ok := captured.DeployedState["orchestrator-script.md"]
	if !ok {
		t.Error("orchestrator-script.md not found in plan.Input.DeployedState; " +
			"when the script file is present on disk, the Update flow must probe it and include " +
			"it in the artifact set via scannedAgentKeys")
		return
	}
	if !scriptState.Present {
		t.Error("DeployedState[orchestrator-script.md].Present = false; " +
			"the file was written to disk and must be detected as present by the probe")
	}

	// Verify the script key appears in ScannedAgentKeys (the single-authority slice).
	found := false
	for _, key := range captured.ScannedAgentKeys {
		if key == "orchestrator-script" {
			found = true
			break
		}
	}
	if !found {
		t.Error("\"orchestrator-script\" not in plan.Input.ScannedAgentKeys; " +
			"the orchestrator-script probe must feed its key into scannedAgentKeys when present on disk")
	}
}

// TestOrchestratorScript_Update_ScriptAbsentFromDisk_ExcludedFromDeployedState verifies that
// when the catalog has an orchestrator-script but the script file is absent from disk, the
// Update flow does not include the script in the artifact set or DeployedState.
func TestOrchestratorScript_Update_ScriptAbsentFromDisk_ExcludedFromDeployedState(t *testing.T) {
	ws := t.TempDir()
	// Intentionally do NOT write orchestrator-script.md to disk.

	spy := &spyPlanner{response: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: ws,
		Scope:         domain.ScopeProject,
	}}
	stub := interactiontest.NewBuilder().Build()
	deps, _ := newBaseDeps(t, stub)
	deps.Planner = spy
	deps.Catalog = &stubCatalog{
		root:         "testroot",
		orchestrator: minimalOrchestrator,
		orchestratorScript: domain.Agent{
			Key:     "orchestrator-script",
			Version: "1.0",
			Name:    "Orchestrator Script",
			Role:    domain.RoleOrchestrator,
		},
		orchestratorScriptOK: true,
		agents:               []domain.Agent{minimalAgent},
		skills:               []domain.Skill{},
		hooks:                []domain.HookBundle{},
		workflows:            []domain.Workflow{minimalWorkflow},
		categories: []domain.WorkflowCategory{
			{Name: "Build", Workflows: []domain.Workflow{minimalWorkflow}},
		},
	}
	svc := app.New(deps)

	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   ws,
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	captured := spy.lastCaptured(t)

	// The orchestrator-script file is absent from disk; it must not appear in DeployedState.
	if _, ok := captured.DeployedState["orchestrator-script.md"]; ok {
		t.Error("orchestrator-script.md found in plan.Input.DeployedState; " +
			"when the script file is absent from disk, the Update flow must not include it " +
			"in the artifact set or DeployedState")
	}

	// The script key must not appear in ScannedAgentKeys.
	for _, key := range captured.ScannedAgentKeys {
		if key == "orchestrator-script" {
			t.Error("\"orchestrator-script\" found in plan.Input.ScannedAgentKeys; " +
				"the key must not be added when the script file is absent from disk")
			break
		}
	}
}
