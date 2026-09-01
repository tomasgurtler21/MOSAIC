package app_test

// custommodelpersistence_test.go verifies that custom model IDs entered during a run are
// persisted to UserConfig and that previously-persisted IDs are offered as selectable
// options in subsequent runs. Persistence must work for both the DeployNew and Update flows.
//
// Verified invariants:
//
// Persistence from DeployNew:
//   - A custom model ID entered via QAgentModel is saved to UserConfig.CustomModelIDs for
//     the current harness after a DeployNew run completes.
//   - A custom model ID entered via QTierModel is also saved to UserConfig.CustomModelIDs.
//
// Cross-run option enrichment:
//   - On a subsequent DeployNew run, a model ID in UserConfig.CustomModelIDs[harnessID]
//     appears as a selectable option in both QAgentModel and QTierModel questions.
//   - The persisted custom IDs are options only — QAgentModel is still asked even if a
//     persisted ID matches the harness model list; the IDs are never auto-applied.
//
// Persistence from Update:
//   - After an Update run, UserConfig.CustomModelIDs for the current harness are preserved
//     (the Update flow must call persistCustomModelIDs so that prior custom IDs are not lost
//     and any new ones from that run are added).
//
// These tests will fail until:
//   - resolveModels collects accumulated custom IDs and returns them in modelResolution
//   - persistCustomModelIDs is implemented and called in both DeployNew and Update

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/config"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// containsCustomModelID reports whether ids contains the given target string.
func containsCustomModelID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Persistence from DeployNew — agent-level custom model
// ---------------------------------------------------------------------------

// TestDeployNew_CustomAgentModel_PersistedToCustomModelIDs verifies that when the user
// enters a custom model ID for an agent's model question during DeployNew, that ID is
// saved to UserConfig.CustomModelIDs[harnessID] at the end of the run.
func TestDeployNew_CustomAgentModel_PersistedToCustomModelIDs(t *testing.T) {
	// Arrange — scripted to enter "agent-custom-persist-xyz" for test-runner.
	// AutoConfirmPlan bypasses the review step, so no AnswerReview is needed.
	stub := interactiontest.NewBuilder().
		AnswerSelectOneCustom(domain.QAgentModel, "test-runner", "agent-custom-persist-xyz").
		Build()
	spy := &spyUserConfig{}
	deps, workspace := newBaseDeps(t, stub)
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — the custom model ID must appear in the saved UserConfig.
	if len(spy.saved) == 0 {
		t.Fatal("UserConfig.Save was never called; custom model IDs must be persisted after DeployNew")
	}
	lastSave := spy.saved[len(spy.saved)-1]
	ids := lastSave.CustomModelIDs["stub-harness"]
	if !containsCustomModelID(ids, "agent-custom-persist-xyz") {
		t.Errorf("UserConfig.CustomModelIDs[\"stub-harness\"] = %v after DeployNew; "+
			"want to contain \"agent-custom-persist-xyz\" entered for \"test-runner\"; "+
			"custom model IDs entered at agent level must be persisted to UserConfig", ids)
	}
}

// ---------------------------------------------------------------------------
// Persistence from DeployNew — tier-level custom model
// ---------------------------------------------------------------------------

// TestDeployNew_CustomTierModel_PersistedToCustomModelIDs verifies that when the user
// enters a custom model ID at the tier level (QTierModel), that ID is saved to
// UserConfig.CustomModelIDs[harnessID] at the end of the run.
func TestDeployNew_CustomTierModel_PersistedToCustomModelIDs(t *testing.T) {
	// Arrange — scripted to enter "tier-custom-persist-abc" for the HIGH tier.
	// AutoConfirmPlan bypasses the review step, so no AnswerReview is needed.
	stub := interactiontest.NewBuilder().
		AnswerSelectOneCustom(domain.QTierModel, "HIGH", "tier-custom-persist-abc").
		Build()
	spy := &spyUserConfig{}
	deps, workspace := newBaseDeps(t, stub)
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert
	if len(spy.saved) == 0 {
		t.Fatal("UserConfig.Save was never called; custom model IDs must be persisted after DeployNew")
	}
	lastSave := spy.saved[len(spy.saved)-1]
	ids := lastSave.CustomModelIDs["stub-harness"]
	if !containsCustomModelID(ids, "tier-custom-persist-abc") {
		t.Errorf("UserConfig.CustomModelIDs[\"stub-harness\"] = %v after DeployNew; "+
			"want to contain \"tier-custom-persist-abc\" entered for tier \"HIGH\"; "+
			"custom model IDs entered at tier level must be persisted to UserConfig", ids)
	}
}

// ---------------------------------------------------------------------------
// Cross-run option enrichment — agent model
// ---------------------------------------------------------------------------

// TestDeployNew_PersistedCustomModelIDs_AppearedInAgentOptions verifies that on a
// subsequent DeployNew run, custom model IDs stored in UserConfig.CustomModelIDs appear
// as selectable options in QAgentModel questions, even though they were not present in the
// harness's built-in model list.
func TestDeployNew_PersistedCustomModelIDs_AppearedInAgentOptions(t *testing.T) {
	// Arrange — seed UserConfig with a custom model ID from a prior run.
	spy := &spyUserConfig{
		cfg: config.UserConfig{
			CustomModelIDs: map[string][]string{
				"stub-harness": {"previously-persisted-model-007"},
			},
		},
	}

	// Use capturing interaction to inspect what options are presented for test-runner.
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			// All agents: SkippedOne (default) — we only care about presented options
		},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.UserConfig = spy
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan: true,
	})

	// Assert — "previously-persisted-model-007" must appear as a selectable option.
	testRunnerOptions := capture.optionsFor(domain.QAgentModel, "test-runner")
	if testRunnerOptions == nil {
		t.Fatal("QAgentModel was not asked for \"test-runner\"; cannot verify persisted option appearance")
	}
	if !optionIDsPresent(testRunnerOptions, "previously-persisted-model-007") {
		t.Errorf("QAgentModel options for \"test-runner\" = %v; "+
			"want to contain \"previously-persisted-model-007\" loaded from UserConfig.CustomModelIDs; "+
			"persisted custom model IDs must appear as selectable options in subsequent runs", testRunnerOptions)
	}
}

// TestDeployNew_PersistedCustomModelIDs_AppearedInTierOptions verifies that on a subsequent
// DeployNew run, custom model IDs stored in UserConfig.CustomModelIDs also appear in the
// options for QTierModel questions, not only QAgentModel.
func TestDeployNew_PersistedCustomModelIDs_AppearedInTierOptions(t *testing.T) {
	// Arrange — seed UserConfig with a custom model ID from a prior run.
	spy := &spyUserConfig{
		cfg: config.UserConfig{
			CustomModelIDs: map[string][]string{
				"stub-harness": {"persisted-tier-option-99"},
			},
		},
	}

	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			// All questions: SkippedOne (default)
		},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.UserConfig = spy
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — "persisted-tier-option-99" must appear in the QTierModel options.
	tierOptions := capture.optionsFor(domain.QTierModel, "HIGH")
	if tierOptions == nil {
		t.Fatal("QTierModel was not asked for \"HIGH\"; cannot verify persisted option in tier question")
	}
	if !optionIDsPresent(tierOptions, "persisted-tier-option-99") {
		t.Errorf("QTierModel options for \"HIGH\" = %v; "+
			"want to contain \"persisted-tier-option-99\" loaded from UserConfig.CustomModelIDs; "+
			"persisted custom model IDs must appear as selectable options in tier model questions", tierOptions)
	}
}

// ---------------------------------------------------------------------------
// Persistence deduplication across runs
// ---------------------------------------------------------------------------

// TestDeployNew_SameCustomModelIDEnteredByTwoAgents_StoredOnlyOnceInUserConfig verifies
// that when the same custom model ID is entered by two different agents in a single DeployNew
// run, it is saved to UserConfig.CustomModelIDs exactly once — not once per agent. The
// append-only merge inside persistCustomModelIDs must deduplicate by exact string match.
func TestDeployNew_SameCustomModelIDEnteredByTwoAgents_StoredOnlyOnceInUserConfig(t *testing.T) {
	// Arrange — two agents in HIGH tier, each scripted to enter "duplicate-persist-id".
	// Fresh UserConfig so the result reflects only what this run persists.
	spy := &spyUserConfig{}
	stub := interactiontest.NewBuilder().
		AnswerSelectOneCustom(domain.QAgentModel, "test-runner", "duplicate-persist-id").
		AnswerSelectOneCustom(domain.QAgentModel, "code-writer", "duplicate-persist-id").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = twoAgentSameTierCatalog()
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — "duplicate-persist-id" must appear exactly once in the saved UserConfig.
	if len(spy.saved) == 0 {
		t.Fatal("UserConfig.Save was never called; custom model IDs must be persisted after DeployNew")
	}
	lastSave := spy.saved[len(spy.saved)-1]
	ids := lastSave.CustomModelIDs["stub-harness"]
	count := 0
	for _, id := range ids {
		if id == "duplicate-persist-id" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("UserConfig.CustomModelIDs[\"stub-harness\"] contains \"duplicate-persist-id\" "+
			"%d time(s) after two agents entered the same custom ID in one run, want exactly 1; "+
			"persistCustomModelIDs must deduplicate accumulated custom IDs before saving to UserConfig",
			count)
	}
}

// ---------------------------------------------------------------------------
// Pre-answered agent models — NOT persisted as custom model IDs
// ---------------------------------------------------------------------------

// TestDeployNew_PreAnsweredAgentModels_CustomIDNotPersisted verifies that when an agent
// model is pre-answered via AgentModels in the DeployRequest (not entered interactively
// through Interaction), the model ID does NOT appear in UserConfig.CustomModelIDs after the
// run. Pre-answered model IDs are caller-supplied overrides for this run only and must not
// be treated as user-entered custom IDs eligible for persistence.
//
// This locks in the invariant that appendCustomID is only called from ans.Custom branches
// in resolveModels — never from the pre-answer merge path. A pre-answered AgentModels value
// bypasses the interactive SelectOne question and therefore never enters the ans.Custom path
// that feeds accumulatedOptions and ultimately persistCustomModelIDs.
func TestDeployNew_PreAnsweredAgentModels_CustomIDNotPersisted(t *testing.T) {
	// Arrange — "pre-answered-agent-model-xyz" is supplied as a pre-answer for test-runner
	// via AgentModels. No interaction is scripted for QAgentModel, confirming the pre-answer
	// is used directly without going through the interactive ans.Custom branch.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	spy := &spyUserConfig{}
	deps, workspace := newBaseDeps(t, stub)
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true},
		AgentModels:     map[string]string{"test-runner": "pre-answered-agent-model-xyz"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — "pre-answered-agent-model-xyz" must NOT appear in any saved CustomModelIDs.
	// Pre-answered agent models bypass the interactive ans.Custom branch in resolveModels
	// and must not be accumulated into the custom IDs that are persisted to UserConfig.
	for i, saved := range spy.saved {
		ids := saved.CustomModelIDs["stub-harness"]
		if containsCustomModelID(ids, "pre-answered-agent-model-xyz") {
			t.Errorf("saved[%d].CustomModelIDs[\"stub-harness\"] = %v; "+
				"want to NOT contain \"pre-answered-agent-model-xyz\" which was supplied as a "+
				"pre-answer via AgentModels, not entered through interactive custom-ID input — "+
				"only custom model IDs entered via ans.Custom are eligible for persistence (R1)", i, ids)
		}
	}
}

// ---------------------------------------------------------------------------
// Update flow persistence
// ---------------------------------------------------------------------------

// TestUpdate_CustomModelIDs_PersistedAfterRun verifies that when a new custom model ID is
// entered interactively during an Update run, UserConfig.Save is called and the saved
// config contains both the pre-existing custom model IDs (loaded from UserConfig before
// the run) and the new ID entered during the run. persistCustomModelIDs merges
// interactively-entered IDs with any IDs already in UserConfig so prior custom IDs are
// never lost.
//
// Under R1 (interactive-only persistence), persistCustomModelIDs only calls Save when at
// least one custom ID was entered via an interactive question during the current run. This
// test scripts a QAgentModel custom answer to trigger that path; without an interactive
// answer no Save occurs (the pre-existing IDs are unchanged and safe in UserConfig).
func TestUpdate_CustomModelIDs_PersistedAfterRun(t *testing.T) {
	// Arrange — seed UserConfig with a prior custom model ID and script a new custom ID
	// entry via QAgentModel so that persistCustomModelIDs is triggered during the run.
	spy := &spyUserConfig{
		cfg: config.UserConfig{
			CustomModelIDs: map[string][]string{
				"stub-harness": {"prior-run-custom-model"},
			},
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectOneCustom(domain.QAgentModel, "test-runner", "new-custom-update-xyz").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act — AddWorkflowIDs drives resolveModels to ask about model selection for the
	// workflow's agents; SkipAll for QTierModel forces the per-agent QAgentModel path.
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{"quick-fix"},
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — Save must have been called because a custom ID was entered interactively,
	// and the saved config must contain both the pre-existing and newly-entered IDs.
	if len(spy.saved) == 0 {
		t.Fatal("UserConfig.Save was not called during Update; " +
			"persistCustomModelIDs must call Save when a custom model ID is entered " +
			"interactively during the run")
	}
	lastSave := spy.saved[len(spy.saved)-1]
	ids := lastSave.CustomModelIDs["stub-harness"]
	if !containsCustomModelID(ids, "prior-run-custom-model") {
		t.Errorf("UserConfig.CustomModelIDs[\"stub-harness\"] = %v after Update; "+
			"want to contain \"prior-run-custom-model\" that was set before the Update run; "+
			"persistCustomModelIDs must merge interactively-entered IDs with pre-existing ones "+
			"so prior custom model IDs are not lost", ids)
	}
}

// TestUpdate_NewCustomAgentModel_EnteredDuringRun_IsPersistedToUserConfig verifies that
// when the user enters a new custom model ID for an agent model question during an Update run,
// that ID is saved to UserConfig.CustomModelIDs[harnessID] at the end of the run. This is
// the Update-flow counterpart to TestDeployNew_CustomAgentModel_PersistedToCustomModelIDs.
//
// AddWorkflowIDs ensures the agent set is non-empty so the Update flow calls resolveModels
// and asks QAgentModel. SkipAll for QTierModel forces the per-agent question path.
func TestUpdate_NewCustomAgentModel_EnteredDuringRun_IsPersistedToUserConfig(t *testing.T) {
	// Arrange — fresh UserConfig; scripted to enter a new custom ID via QAgentModel.
	spy := &spyUserConfig{}
	stub := interactiontest.NewBuilder().
		AnswerSelectOneCustom(domain.QAgentModel, "test-runner", "update-run-new-custom-xyz").
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{"quick-fix"},
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Assert — the new custom ID entered during the Update run must appear in the saved config.
	if len(spy.saved) == 0 {
		t.Fatal("UserConfig.Save was never called during Update; " +
			"the Update flow must call persistCustomModelIDs so new custom model IDs " +
			"entered via QAgentModel during an Update run are saved to UserConfig")
	}
	lastSave := spy.saved[len(spy.saved)-1]
	ids := lastSave.CustomModelIDs["stub-harness"]
	if !containsCustomModelID(ids, "update-run-new-custom-xyz") {
		t.Errorf("UserConfig.CustomModelIDs[\"stub-harness\"] = %v after Update; "+
			"want to contain \"update-run-new-custom-xyz\" entered during the Update run; "+
			"new custom model IDs entered during an Update run must be persisted to UserConfig "+
			"(AC1.3: only custom model IDs entered interactively are persisted)", ids)
	}
}
