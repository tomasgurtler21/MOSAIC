package app_test

// deployagents_persistence_test.go verifies that the deployAgents flow follows the
// interactive-only persistence rule (R1):
//
//   - Pre-answered tier models (from DeployAgentsRequest.TierModels) must NOT be persisted
//     to UserConfig.TierModels. The caller supplied these as one-shot overrides for this run;
//     writing them back would incorrectly treat them as permanent user preferences and is the
//     primary corruption trigger in high-concurrency AgentTest scenarios.
//
//   - Interactively-answered tier models (via Interaction.SelectOne for QTierModel) MUST
//     still be persisted to UserConfig.TierModels, matching the deploy-new persistence contract.
//
//   - Pre-answered agent models (from DeployAgentsRequest.AgentModels) must NOT be persisted
//     to UserConfig.CustomModelIDs. Only custom model IDs entered interactively through the
//     ans.Custom branch in resolveModels are eligible for persistence.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Tier model persistence — DeployAgents
// ---------------------------------------------------------------------------

// TestDeployAgents_TierPreAnswered_NotSavedToUserConfig verifies that when TierModels is
// pre-supplied in DeployAgentsRequest, the pre-answered tier-to-model mapping is NOT
// written back to UserConfig. Pre-answered tier models are caller-supplied overrides for
// this run only and must not be treated as persistent user preferences (R1).
//
// The spy starts with an empty UserConfig so any tier model appearing in spy.saved was
// actively persisted by the flow, not pre-loaded from a prior config state.
func TestDeployAgents_TierPreAnswered_NotSavedToUserConfig(t *testing.T) {
	// Arrange — pre-answer the "HIGH" tier; no QTierModel interaction scripted.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	spy := &spyUserConfig{}
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:     "stub-harness",
		WorkspacePath: workspace,
		SubagentIDs:   []string{"test-runner"},
		TierModels: map[domain.Tier]string{
			"HIGH": "model-b",
		},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployAgents: %v", err)
	}

	// Assert — "model-b" must NOT appear in any saved config's TierModels. The pre-answered
	// tier model was supplied by the caller for this run only and must not be written back to
	// the persistent user config (R1: interactive-only persistence rule).
	for i, saved := range spy.saved {
		if saved.TierModels["stub-harness"]["HIGH"] == "model-b" {
			t.Errorf("saved[%d].TierModels[\"stub-harness\"][\"HIGH\"] = \"model-b\"; "+
				"pre-answered tier selections from DeployAgentsRequest.TierModels must NOT be "+
				"persisted to user config — only interactively-answered tier selections are "+
				"eligible for persistence (R1: interactive-only persistence rule)", i)
		}
	}
}

// TestDeployAgents_TierInteractivelyAnswered_SavedToUserConfig verifies that when a tier
// model is selected through Interaction (QTierModel) during a DeployAgents run, the
// selection IS persisted to UserConfig after the run. Interactively-answered tier models
// represent genuine user preferences and must be remembered across runs.
func TestDeployAgents_TierInteractivelyAnswered_SavedToUserConfig(t *testing.T) {
	// Arrange — no pre-answer for the "HIGH" tier; interaction scripted to answer "model-a".
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	spy := &spyUserConfig{}
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		SubagentIDs:     []string{"test-runner"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployAgents: %v", err)
	}

	// Assert — the interactively-answered tier model must appear in the saved UserConfig.
	// This regression guard ensures that the R1 change (filtering pre-answered tiers from
	// persistence) does not accidentally also filter interactively-answered tiers.
	if len(spy.saved) == 0 {
		t.Fatal("UserConfigStore.Save was never called; interactively-answered tier " +
			"selections must be persisted to UserConfig after a DeployAgents run")
	}
	lastSave := spy.saved[len(spy.saved)-1]
	if lastSave.TierModels["stub-harness"]["HIGH"] != "model-a" {
		t.Errorf("saved TierModels[\"stub-harness\"][\"HIGH\"] = %q, want \"model-a\"; "+
			"a tier model answered interactively during DeployAgents must be persisted to "+
			"UserConfig (R1 applies only to pre-answered tiers, not interactive answers)",
			lastSave.TierModels["stub-harness"]["HIGH"])
	}
}

// ---------------------------------------------------------------------------
// Custom model ID persistence — DeployAgents
// ---------------------------------------------------------------------------

// TestDeployAgents_PreAnsweredAgentModels_CustomIDNotPersisted verifies that when agent
// models are pre-answered via DeployAgentsRequest.AgentModels, those model IDs do NOT
// appear in UserConfig.CustomModelIDs after the run. Only custom model IDs entered
// interactively (via the ans.Custom branch in resolveModels) are eligible for persistence.
//
// This mirrors the same invariant tested for DeployNew in
// TestDeployNew_PreAnsweredAgentModels_CustomIDNotPersisted.
func TestDeployAgents_PreAnsweredAgentModels_CustomIDNotPersisted(t *testing.T) {
	// Arrange — "pre-answered-da-model-abc" is supplied as a pre-answer for test-runner.
	// No interaction is scripted for QAgentModel; the pre-answer is used directly without
	// entering the interactive ans.Custom branch that feeds accumulatedOptions.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &spyUserConfig{}
	deps, workspace := newBaseDeps(t, stub)
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		SubagentIDs:     []string{"test-runner"},
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true},
		AgentModels:     map[string]string{"test-runner": "pre-answered-da-model-abc"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployAgents: %v", err)
	}

	// Assert — "pre-answered-da-model-abc" must NOT appear in any saved CustomModelIDs.
	for i, saved := range spy.saved {
		ids := saved.CustomModelIDs["stub-harness"]
		if containsCustomModelID(ids, "pre-answered-da-model-abc") {
			t.Errorf("saved[%d].CustomModelIDs[\"stub-harness\"] = %v; "+
				"want to NOT contain \"pre-answered-da-model-abc\" which was supplied as a "+
				"pre-answer via AgentModels — only custom model IDs entered interactively "+
				"(via ans.Custom) are eligible for persistence (R1)", i, ids)
		}
	}
}
