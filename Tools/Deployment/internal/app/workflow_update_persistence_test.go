package app_test

// workflow_update_persistence_test.go verifies that the UpdateWorkflows flow follows the
// interactive-only persistence rule (R1) and that the len(newAgents) > 0 gate composes
// correctly with it (AC1.5):
//
//   - When newly-required agents trigger interactive tier model selection, the selected
//     models ARE persisted to UserConfig.TierModels.
//
//   - When no new agents are detected, the len(newAgents) > 0 gate evaluates to false,
//     model resolution is skipped entirely, and no tier models are persisted. This verifies
//     that the outer gate correctly blocks model persistence when no new agents are detected.
//
//   - When a tier model was previously persisted and is re-loaded from UserConfig as a
//     pre-answer inside resolveModels, it must NOT be re-persisted by UpdateWorkflows even
//     when len(newAgents) > 0. Only interactively-answered tier models are eligible for
//     persistence (R1 interactive-only gate, AC1.1).
//
// Note: WorkflowUpdateRequest has no TierModels or AgentModels fields. Tier models reach
// the flow exclusively from persisted UserConfig (re-supplied as pre-answers inside
// resolveModels) or from interactive QTierModel answers. The interactive-only gate inside
// resolveModels (interactivelyResolvedTiers) transparently handles this distinction.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Tier model persistence — UpdateWorkflows
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_NewAgent_InteractiveTierModel_SavedToUserConfig verifies that when
// UpdateWorkflows encounters a newly-required agent and the user answers QTierModel
// interactively, the tier-to-model mapping IS persisted to UserConfig after the run.
//
// This test verifies that both the len(newAgents) > 0 gate and the interactive-only gate
// compose correctly: the outer gate enables model resolution (new agent is present), and the
// inner gate (interactive answer recorded in interactivelyResolvedTiers) enables persistence.
//
// This is a regression guard for existing correct behavior: the current implementation
// already calls resolveModels for new agents and persists interactively-answered tier
// models. The test locks in this invariant so that changes in the R1 persistence path
// (I1.2) do not accidentally break the interactive case for UpdateWorkflows.
func TestUpdateWorkflows_NewAgent_InteractiveTierModel_SavedToUserConfig(t *testing.T) {
	// Arrange — test-runner.md absent (new agent); interaction scripted to answer QTierModel.
	// newAgentPlan returns orchestrator (ActionUpdate) + test-runner (ActionCreate), matching
	// the planner output when test-runner is a newly-required but absent agent.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	spy := &spyUserConfig{}
	deps.UserConfig = spy
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	// test-runner.md is NOT written — the flow must classify it as a new agent and call
	// resolveModels, which will ask QTierModel for the "HIGH" tier.
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v", err)
	}

	// Assert — the interactively-answered tier model must appear in the saved UserConfig.
	if len(spy.saved) == 0 {
		t.Fatal("UserConfigStore.Save was never called; an interactively-answered tier " +
			"selection for a newly-required agent must be persisted after UpdateWorkflows " +
			"when len(newAgents) > 0 and the answer was given through Interaction (R1, AC1.5)")
	}
	lastSave := spy.saved[len(spy.saved)-1]
	if lastSave.TierModels["stub-harness"]["HIGH"] != "model-a" {
		t.Errorf("saved TierModels[\"stub-harness\"][\"HIGH\"] = %q, want \"model-a\"; "+
			"a tier model answered interactively during UpdateWorkflows for a new agent must "+
			"be persisted to UserConfig (R1 interactive-only gate, AC1.5 gate composition)",
			lastSave.TierModels["stub-harness"]["HIGH"])
	}
}

// TestUpdateWorkflows_NewAgent_PreLoadedTierModel_NotRePersisted verifies that when a
// tier model was previously persisted to UserConfig and is re-loaded by resolveModels as
// a pre-answer for the current run, UpdateWorkflows does NOT write it back to UserConfig
// even when len(newAgents) > 0 (a new agent is present).
//
// This is the primary corruption scenario for UpdateWorkflows in AgentTest: the runner
// loads its own previously-persisted tier config back as pre-answers on every run. Without
// the R1 interactive-only gate, each run would re-persist the pre-loaded value, and
// concurrent runs would overwrite each other's legitimate interactive selections.
//
// This test will fail against the current implementation (which passes tierModelsUsed,
// including pre-answers, to persistTierModels) and pass once I1.2 switches the
// UpdateWorkflows call site to interactivelyResolvedTiers.
func TestUpdateWorkflows_NewAgent_PreLoadedTierModel_NotRePersisted(t *testing.T) {
	// Arrange — seed UserConfig with a pre-existing tier model so resolveModels will load
	// it as a pre-answer. No QTierModel interaction is scripted; the pre-loaded value is
	// the only tier model available, so no interactive question will be asked.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	spy := &spyUserConfig{
		cfg: config.UserConfig{
			TierModels: map[string]map[domain.Tier]string{
				"stub-harness": {"HIGH": "pre-loaded-model"},
			},
		},
	}
	deps.UserConfig = spy
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	// test-runner.md is NOT written — the flow must classify it as a new agent and call
	// resolveModels. resolveModels loads UserConfig and uses the pre-existing "HIGH" tier
	// mapping as a pre-answer, so QTierModel is not asked interactively.
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v", err)
	}

	// Assert — the pre-loaded tier model must NOT appear in any saved UserConfig snapshot.
	// persistTierModels must only be called with interactivelyResolvedTiers, which is empty
	// because no interactive question was asked.
	for i, saved := range spy.saved {
		if harnessMap, ok := saved.TierModels["stub-harness"]; ok {
			if harnessMap["HIGH"] == "pre-loaded-model" {
				t.Errorf("saved[%d].TierModels[\"stub-harness\"][\"HIGH\"] = \"pre-loaded-model\"; "+
					"want absent; a tier model loaded from UserConfig as a pre-answer must not "+
					"be re-persisted by UpdateWorkflows (R1 interactive-only gate, AC1.1)", i)
			}
		}
	}
}

// TestUpdateWorkflows_NoNewAgents_TierModelNotPersisted verifies that when all required
// workflow agents are already deployed, UpdateWorkflows skips model resolution entirely
// (len(newAgents) == 0 gate evaluates to false) and no tier model mappings are written to
// UserConfig.
//
// This confirms that the outer len(newAgents) > 0 gate correctly composes with the
// interactive-only inner gate: when the outer gate is false, neither model resolution nor
// model persistence runs, regardless of what the inner gate would evaluate to.
func TestUpdateWorkflows_NoNewAgents_TierModelNotPersisted(t *testing.T) {
	// Arrange — test-runner.md present (already deployed); no model questions should be asked.
	// orchestratorOnlyPlan returns only the orchestrator item, matching the planner output
	// when all workflow-required agents are already deployed.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	spy := &spyUserConfig{}
	deps.UserConfig = spy
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	// Write test-runner.md to signal it is already deployed; the flow must not classify it
	// as a new agent and must not call resolveModels.
	writeTempFile(t, workspace, "test-runner.md", []byte("---\nversion: \"1.0\"\n---\nDeployed.\n"))
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("UpdateWorkflows: %v", err)
	}

	// Assert — no tier models must be persisted when len(newAgents) == 0.
	for i, saved := range spy.saved {
		if len(saved.TierModels) > 0 {
			t.Errorf("saved[%d].TierModels = %v; want empty; "+
				"UpdateWorkflows must not persist any tier models when all required agents "+
				"are already deployed (len(newAgents) == 0 gate, AC1.5 gate composition)", i, saved.TierModels)
		}
	}
}
