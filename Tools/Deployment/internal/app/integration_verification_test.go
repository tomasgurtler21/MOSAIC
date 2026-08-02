package app_test

// integration_verification_test.go verifies that the three fix stages (custom model
// propagation, conflict apply-to-all, and infrastructure agent model resolution) compose
// correctly in a single DeployNew flow without regressions.
//
// Verified invariants (T4.1):
//
// Composed flow:
//   - (a) A custom model ID entered for one regular agent propagates to subsequent regular
//     agents in the same resolveModels batch: the second regular agent sees it as a
//     selectable option.
//   - (b) Infrastructure agents receive QAgentModel questions in a distinct second
//     resolveModels batch that follows all regular-agent questions.
//   - (c) A custom model ID entered for a regular agent during the first resolveModels call
//     appears as a selectable option for infrastructure agents in the second resolveModels
//     call (AC4.2: cross-batch custom model propagation).
//   - (d) Tier-model questions answered in the regular-agent batch are not re-asked for the
//     infra-agent batch; QTierModel for a shared tier is asked exactly once (AC4.3).
//   - (e) Multiple conflict items are resolved via the apply-to-all latch: selecting an
//     "all:" compound option on the first conflict suppresses all subsequent conflict prompts.
//   - (f) Infrastructure agents are included in content generation; the Content callback
//     resolves infra-agent plan items without an "unknown agent" error.
//   - The combined run completes successfully with no error.
//
// Design note: (a)–(c) and (e)–(f) require QAgentModel to be asked for the agents under
// test. Because tier model resolution answers QAgentModel implicitly via the tier mapping,
// tests that verify QAgentModel options use SkipAll[QTierModel] so that agents fall through
// to the per-agent question path. (d) / AC4.3 is verified in a dedicated focused test that
// uses a scripted tier answer without SkipAll, allowing the tier non-duplication invariant
// to be observed directly via call-count inspection.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Catalog and plan helpers for integration tests
// ---------------------------------------------------------------------------

// integrationCodeWriter is a second regular worker agent used in multi-agent integration
// scenarios so that in-run propagation across regular agents can be verified.
var integrationCodeWriter = domain.Agent{
	Key:             "code-writer",
	NumericID:       "2",
	Version:         "1.0",
	Name:            "Code Writer",
	Description:     "Writes implementation code",
	Role:            domain.RoleWorker,
	Category:        "Creation",
	RecommendedTier: "HIGH",
}

// catalogWithTwoRegularAndOneInfraAgent returns a catalog with two regular worker agents
// (test-runner, code-writer) both in the HIGH tier, plus one infrastructure agent
// (checkpoint-agent), and a workflow referencing both regular agents.
func catalogWithTwoRegularAndOneInfraAgent() *stubCatalog {
	cat := newMinimalCatalog()
	cat.agents = []domain.Agent{minimalAgent, integrationCodeWriter}
	cat.infraAgents = []domain.Agent{minimalInfraAgent}
	cat.workflows = []domain.Workflow{
		{
			ID:               "quick-fix",
			Name:             "Quick Fix",
			Version:          "1.0",
			Category:         "Build",
			ReferencedAgents: []string{"test-runner", "code-writer"},
		},
	}
	cat.tiers = []domain.TierInfo{
		{
			Tier:      "HIGH",
			Rationale: "Needs a capable model to analyse test output",
			AgentKeys: []string{"test-runner", "code-writer"},
		},
	}
	return cat
}

// integrationPlan returns a plan combining regular-agent and infra-agent ActionCreate items
// with multiple ActionConflict items. This simulates a realistic DeployNew scenario where
// all three fix stages are exercised simultaneously.
func integrationPlan(workspace string) domain.Plan {
	mkConflict := func(key, path string) domain.PlanItem {
		return domain.PlanItem{
			Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: key},
			TargetPath: path,
			Action:     domain.ActionConflict,
			Conflict: &domain.LocalModification{
				RecordedHash: "sha256:old",
				CurrentHash:  "sha256:new",
			},
		}
	}
	return domain.Plan{
		Mode:          domain.ModeDeployNew,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: "test-runner.md",
				Action:     domain.ActionCreate,
			},
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "code-writer"},
				TargetPath: "code-writer.md",
				Action:     domain.ActionCreate,
			},
			infraAgentPlanItem(minimalInfraAgent, "checkpoint-agent.md"),
			mkConflict("conflict-a", "conflict-a.md"),
			mkConflict("conflict-b", "conflict-b.md"),
			mkConflict("conflict-c", "conflict-c.md"),
		},
		Workflows: []string{"quick-fix"},
	}
}

// ---------------------------------------------------------------------------
// Comprehensive cross-cutting integration test (T4.1, AC4.1)
// ---------------------------------------------------------------------------

// TestDeployNew_AllThreeFixes_ComposedIntegration verifies that all three fix stages
// compose correctly in a single DeployNew flow. QTierModel is pre-skipped via SkipAll so
// that QAgentModel is asked for all agents, allowing in-run propagation (a/c) and infra
// batch existence (b) to be verified through option inspection. Tier non-duplication (d)
// is verified in TestDeployNew_TierModelNonDuplication_ExplicitAssertion.
//
//   - (a) Stage 1 in-run propagation: custom model entered for test-runner appears in
//     code-writer's options in the same regular-agent batch.
//   - (b) Stage 3: checkpoint-agent receives QAgentModel in the infra-agent batch.
//   - (c) AC4.2 cross-batch: the custom model from the regular batch appears in
//     checkpoint-agent's options when the infra batch runs.
//   - (e) Stage 2 apply-to-all: first conflict triggers latch; remaining conflicts are
//     not prompted.
//   - (f) Stage 3 content: Content callback resolves checkpoint-agent without error.
//   - Overall: the run completes without error (AC4.1).
func TestDeployNew_AllThreeFixes_ComposedIntegration(t *testing.T) {
	// Arrange — catalog with two regular agents (test-runner, code-writer) and one
	// infrastructure agent (checkpoint-agent).
	cat := catalogWithTwoRegularAndOneInfraAgent()

	// Interaction script:
	//   - QAgentModel "test-runner"  → custom "int-cross-model"  (Stage 1 source)
	//   - QAgentModel "code-writer"  → SkippedOne (default); inspect options for (a)
	//   - QAgentModel "checkpoint-agent" → SkippedOne (default); inspect options for (b/c)
	//   - QLocalModification "conflict-a.md" → "all:overwrite"  (Stage 2 trigger)
	//   - All others → SkippedOne (default)
	//
	// Tier questions are pre-skipped via SkipAll[QTierModel] so agents reach per-agent
	// model questions and option inspection is meaningful.
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QAgentModel, "test-runner"}: {
				Status: domain.Answered,
				Custom: "int-cross-model",
			},
			{domain.QLocalModification, "conflict-a.md"}: {
				Status:   domain.Answered,
				OptionID: "all:overwrite",
			},
		},
	}

	exec := &capturingExecutor{}
	spy := &spyUserConfig{}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = capture
	deps.Executor = exec
	deps.UserConfig = spy
	deps.Planner = &stubPlanner{plan: integrationPlan(workspace)}
	svc := app.New(deps)

	// Act — SkipAll[QTierModel] so all agents fall through to per-agent model questions.
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert — the full composed run must complete without error (AC4.1 overall success).
	if err != nil {
		t.Fatalf("DeployNew returned unexpected error: %v", err)
	}

	// -----------------------------------------------------------------------
	// (a) Stage 1 in-run propagation to subsequent regular agent:
	//     the custom model entered for test-runner must appear in code-writer's options.
	// -----------------------------------------------------------------------
	codeWriterOptions := capture.optionsFor(domain.QAgentModel, "code-writer")
	if codeWriterOptions == nil {
		t.Error("QAgentModel was not asked for \"code-writer\"; " +
			"cannot verify in-run custom model propagation to subsequent regular agents (Stage 1)")
	} else if !optionIDsPresent(codeWriterOptions, "int-cross-model") {
		t.Errorf("QAgentModel options for \"code-writer\" = %v; "+
			"want to contain \"int-cross-model\" entered for \"test-runner\"; "+
			"custom model IDs entered for one agent must propagate to subsequent agents "+
			"in the same resolveModels batch (Stage 1 in-run propagation)", codeWriterOptions)
	}

	// -----------------------------------------------------------------------
	// (b) Stage 3: infrastructure agent must receive QAgentModel in a separate batch.
	// -----------------------------------------------------------------------
	infraOptions := capture.optionsFor(domain.QAgentModel, "checkpoint-agent")
	if infraOptions == nil {
		t.Fatal("QAgentModel was not asked for \"checkpoint-agent\"; " +
			"infrastructure agents must participate in a second resolveModels batch "+
			"(Stage 3 not implemented or the second resolveModels call is missing from DeployNew)")
	}

	// -----------------------------------------------------------------------
	// (c) AC4.2: cross-batch custom model propagation —
	//     the custom model entered in the regular-agent batch must appear as a
	//     selectable option when the infra-agent batch presents QAgentModel.
	// -----------------------------------------------------------------------
	if !optionIDsPresent(infraOptions, "int-cross-model") {
		t.Errorf("QAgentModel options for \"checkpoint-agent\" = %v; "+
			"want to contain \"int-cross-model\" (entered for \"test-runner\" in the regular batch); "+
			"modelResolution.accumulatedOptions from the first resolveModels call must be passed "+
			"as extraOptions to the second call so custom model IDs from the regular batch are "+
			"offered to infra agents (AC4.2: cross-batch custom model propagation)", infraOptions)
	}

	// -----------------------------------------------------------------------
	// (e) Stage 2: apply-to-all latch — only the first conflict item must be asked;
	//     the remaining two must be suppressed by the "all:overwrite" selection.
	// -----------------------------------------------------------------------
	conflictAAsked := false
	conflictBAsked := false
	conflictCAsked := false
	for _, rec := range capture.recorded {
		if rec.ID == domain.QLocalModification {
			switch rec.Subject {
			case "conflict-a.md":
				conflictAAsked = true
			case "conflict-b.md":
				conflictBAsked = true
			case "conflict-c.md":
				conflictCAsked = true
			}
		}
	}
	if !conflictAAsked {
		t.Error("QLocalModification was not asked for \"conflict-a.md\"; " +
			"the first conflict item must always be prompted (Stage 2)")
	}
	if conflictBAsked {
		t.Error("QLocalModification was asked for \"conflict-b.md\" even though " +
			"\"all:overwrite\" was selected on \"conflict-a.md\"; " +
			"the apply-to-all latch must suppress all subsequent conflict prompts " +
			"(Stage 2: applyToAllLatch not activated or not propagating through the conflict loop)")
	}
	if conflictCAsked {
		t.Error("QLocalModification was asked for \"conflict-c.md\" even though " +
			"\"all:overwrite\" was selected on \"conflict-a.md\"; " +
			"the apply-to-all latch must suppress all remaining conflict items (Stage 2)")
	}

	// -----------------------------------------------------------------------
	// (f) Stage 3: content generation — the Content callback must resolve the
	//     infra-agent plan item without an "unknown agent" error.
	// -----------------------------------------------------------------------
	if exec.capturedReq == nil {
		t.Fatal("executor was not called; DeployNew must reach execution for Stage 3 " +
			"content generation to be verifiable (check that an earlier step did not fail)")
	}
	if err, bad := exec.contentErrors["checkpoint-agent"]; bad {
		t.Errorf("Content callback returned an error for \"checkpoint-agent\": %v; "+
			"infrastructure agents must be added to the agentByKey map passed to buildContent "+
			"so the Content callback resolves infra-agent plan items without \"unknown agent\" error "+
			"(Stage 3: infra agent missing from agentByKey)", err)
	}
}

// ---------------------------------------------------------------------------
// AC4.2 focused: cross-batch custom model option propagation
// ---------------------------------------------------------------------------

// TestDeployNew_CrossBatchCustomModel_ExplicitAssertion is a focused, isolated verification
// of AC4.2: a custom model ID entered for a regular agent during the first resolveModels
// call must appear as a selectable option in the options list presented to the infra-agent
// batch during the second resolveModels call.
func TestDeployNew_CrossBatchCustomModel_ExplicitAssertion(t *testing.T) {
	// Arrange — single regular agent (test-runner) enters a custom model.
	// checkpoint-agent (infra) must see that custom model in its QAgentModel options.
	cat := catalogWithOneInfraAgent()
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QAgentModel, "test-runner"}: {
				Status: domain.Answered,
				Custom: "ac42-explicit-custom",
			},
		},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = capture
	svc := app.New(deps)

	// Act — SkipAll[QTierModel] so agents reach per-agent model questions.
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert (b): infra agent must receive QAgentModel in the second batch.
	infraOptions := capture.optionsFor(domain.QAgentModel, "checkpoint-agent")
	if infraOptions == nil {
		t.Fatal("QAgentModel was not asked for \"checkpoint-agent\"; " +
			"the infra-agent batch does not exist — Stage 3 not implemented or " +
			"the second resolveModels call is missing from DeployNew")
	}

	// Assert AC4.2: the custom model from the regular batch must appear in the infra options.
	if !optionIDsPresent(infraOptions, "ac42-explicit-custom") {
		t.Errorf("QAgentModel options for \"checkpoint-agent\" = %v; "+
			"want to contain \"ac42-explicit-custom\" entered for \"test-runner\" in the regular batch; "+
			"modelResolution.accumulatedOptions from the first resolveModels call must be passed "+
			"as extraOptions to the second call (AC4.2: cross-batch custom model propagation)", infraOptions)
	}
}

// ---------------------------------------------------------------------------
// AC4.3 focused: tier-model non-duplication between batches
// ---------------------------------------------------------------------------

// TestDeployNew_TierModelNonDuplication_ExplicitAssertion is a focused verification of AC4.3:
// when the regular-agent batch answers QTierModel for a tier, the same QTierModel question
// must not be asked again in the infra-agent batch for that tier. The tier model answer is
// threaded into the second resolveModels call via tierModelsUsed, so the infra batch already
// has the mapping and skips the tier question.
func TestDeployNew_TierModelNonDuplication_ExplicitAssertion(t *testing.T) {
	// Arrange — both test-runner (regular, HIGH) and checkpoint-agent (infra, HIGH) share
	// the HIGH tier. Answering QTierModel once in the regular batch must suppress it in the
	// infra batch. Because the tier model is resolved, QAgentModel is not needed here.
	cat := catalogWithOneInfraAgent()
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QTierModel, "HIGH"}: {
				Status:   domain.Answered,
				OptionID: "model-a",
			},
			// QTierModel "HIGH" should NOT appear a second time in the infra batch.
			// If it does, the count assertion below fails.
		},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = capture
	svc := app.New(deps)

	// Act — no SkipAll so the tier question is asked normally in the regular batch.
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		AutoConfirmPlan:        true,
	})

	// Assert: QTierModel for "HIGH" must be asked exactly once across both batches.
	count := 0
	for _, rec := range capture.recorded {
		if rec.ID == domain.QTierModel && rec.Subject == "HIGH" {
			count++
		}
	}
	if count == 0 {
		t.Error("QTierModel was not asked for tier \"HIGH\" at all; " +
			"tier model resolution must occur during the regular-agent batch")
	}
	if count > 1 {
		t.Errorf("QTierModel for tier \"HIGH\" was asked %d time(s) across both batches; want exactly 1; "+
			"the tier answer from the regular batch must suppress re-asking in the infra batch — "+
			"modelResolution.tierModelsUsed from the first resolveModels call must be passed "+
			"as the tierModels parameter of the second call (AC4.3: tier non-duplication)", count)
	}
}

// ---------------------------------------------------------------------------
// Stage 2 × Stage 3 interaction: apply-to-all and infra model resolution coexist
// ---------------------------------------------------------------------------

// TestDeployNew_ApplyToAllConflict_DoesNotBlockInfraModelResolution verifies that the
// apply-to-all conflict latch (Stage 2) and infrastructure agent model resolution (Stage 3)
// coexist without interference in a single DeployNew run. Model resolution runs before
// conflict handling in the deploy flow, so the apply-to-all latch activated during conflict
// handling must not affect the earlier model resolution phase — and vice versa.
func TestDeployNew_ApplyToAllConflict_DoesNotBlockInfraModelResolution(t *testing.T) {
	// Arrange — plan with infra-agent ActionCreate and two ActionConflict items.
	// The first conflict triggers apply-to-all; the second must not be asked.
	// The infra agent must receive QAgentModel regardless of the conflict latch state.
	cat := catalogWithOneInfraAgent()
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QLocalModification, "conflict-a.md"}: {
				Status:   domain.Answered,
				OptionID: "all:overwrite",
			},
			{domain.QAgentModel, "checkpoint-agent"}: {
				Status:   domain.Answered,
				OptionID: "model-b",
			},
		},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = capture
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeDeployNew,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: "test-runner.md",
				Action:     domain.ActionCreate,
			},
			infraAgentPlanItem(minimalInfraAgent, "checkpoint-agent.md"),
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "conflict-a"},
				TargetPath: "conflict-a.md",
				Action:     domain.ActionConflict,
				Conflict:   &domain.LocalModification{RecordedHash: "sha256:old", CurrentHash: "sha256:new"},
			},
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "conflict-b"},
				TargetPath: "conflict-b.md",
				Action:     domain.ActionConflict,
				Conflict:   &domain.LocalModification{RecordedHash: "sha256:old", CurrentHash: "sha256:new"},
			},
		},
		Workflows: []string{"quick-fix"},
	}}
	svc := app.New(deps)

	// Act — SkipAll[QTierModel] so infra agent reaches per-agent model question.
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})
	if err != nil {
		t.Fatalf("DeployNew returned unexpected error: %v", err)
	}

	// Assert Stage 3: infra agent must receive QAgentModel; conflict handling must not
	// interfere with model resolution since they are separate flow phases.
	infraAsked := false
	conflictBAsked := false
	for _, rec := range capture.recorded {
		switch {
		case rec.ID == domain.QAgentModel && rec.Subject == "checkpoint-agent":
			infraAsked = true
		case rec.ID == domain.QLocalModification && rec.Subject == "conflict-b.md":
			conflictBAsked = true
		}
	}
	if !infraAsked {
		t.Error("QAgentModel was not asked for \"checkpoint-agent\"; " +
			"the apply-to-all conflict latch must not interfere with infra agent model resolution " +
			"— model resolution and conflict handling are separate flow phases (Stage 3)")
	}
	// Assert Stage 2: the latch must suppress the second conflict item.
	if conflictBAsked {
		t.Error("QLocalModification was asked for \"conflict-b.md\" even though " +
			"\"all:overwrite\" was selected on \"conflict-a.md\"; " +
			"the apply-to-all latch must suppress all subsequent conflict prompts (Stage 2)")
	}
}

// ---------------------------------------------------------------------------
// Stage 1 × Stage 3: custom model persistence from both batches
// ---------------------------------------------------------------------------

// TestDeployNew_BothBatchesCustomModels_PersistedToUserConfig verifies that custom model IDs
// entered in both the regular-agent batch and the infra-agent batch are merged and persisted
// to UserConfig.CustomModelIDs[harnessID] at the end of the run. This confirms that
// persistCustomModelIDs is called with combined accumulatedOptions from both resolveModels
// invocations, as specified in Stage 3 design item 6.
func TestDeployNew_BothBatchesCustomModels_PersistedToUserConfig(t *testing.T) {
	// Arrange — test-runner enters "regular-batch-custom" in the first batch;
	// checkpoint-agent enters "infra-batch-custom" in the second batch.
	// Both must appear in the final persisted UserConfig.
	cat := catalogWithOneInfraAgent()
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QAgentModel, "test-runner"}: {
				Status: domain.Answered,
				Custom: "regular-batch-custom",
			},
			{domain.QAgentModel, "checkpoint-agent"}: {
				Status: domain.Answered,
				Custom: "infra-batch-custom",
			},
		},
	}
	spy := &spyUserConfig{}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = capture
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act — SkipAll[QTierModel] so both agents reach per-agent model questions.
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — the last UserConfig.Save must include custom IDs from both batches.
	if len(spy.saved) == 0 {
		t.Fatal("UserConfig.Save was never called; " +
			"persistCustomModelIDs must be invoked with combined options from both resolveModels calls")
	}
	lastSave := spy.saved[len(spy.saved)-1]
	ids := lastSave.CustomModelIDs["stub-harness"]

	if !containsCustomModelID(ids, "regular-batch-custom") {
		t.Errorf("UserConfig.CustomModelIDs[\"stub-harness\"] = %v; "+
			"want to contain \"regular-batch-custom\" from the regular-agent batch; "+
			"accumulatedOptions from the first resolveModels call must be included in "+
			"the final persistCustomModelIDs invocation", ids)
	}
	if !containsCustomModelID(ids, "infra-batch-custom") {
		t.Errorf("UserConfig.CustomModelIDs[\"stub-harness\"] = %v; "+
			"want to contain \"infra-batch-custom\" from the infra-agent batch; "+
			"accumulatedOptions from the second resolveModels call must be merged with those "+
			"from the first before calling persistCustomModelIDs (Stage 3 design item 6)", ids)
	}
}

// ---------------------------------------------------------------------------
// Stage 3: content generation — infra agent without unknown-agent error,
// even when the plan also contains conflict items
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgent_NoUnknownAgentError_WithConflictsPresent verifies that when the
// plan includes both infra-agent ActionCreate items and ActionConflict items, the Content
// callback still resolves the infra-agent item without an "unknown agent" error. This guards
// against a regression where conflict items in the plan could affect how agentByKey is built.
func TestDeployNew_InfraAgent_NoUnknownAgentError_WithConflictsPresent(t *testing.T) {
	// Arrange — plan has an infra-agent ActionCreate and two ActionConflict items.
	// ConflictDefault is used to skip conflict prompts non-interactively.
	cat := catalogWithOneInfraAgent()
	exec := &capturingExecutor{}
	// A non-nil interaction is required because deploy.go always calls Review.
	// optionCapturingInteraction returns Confirm true in Review so the run proceeds.
	capture := &optionCapturingInteraction{}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Executor = exec
	deps.Interaction = capture
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeDeployNew,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: "test-runner.md",
				Action:     domain.ActionCreate,
			},
			infraAgentPlanItem(minimalInfraAgent, "checkpoint-agent.md"),
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "conflict-x"},
				TargetPath: "conflict-x.md",
				Action:     domain.ActionConflict,
				Conflict:   &domain.LocalModification{RecordedHash: "sha256:old", CurrentHash: "sha256:new"},
			},
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "conflict-y"},
				TargetPath: "conflict-y.md",
				Action:     domain.ActionConflict,
				Conflict:   &domain.LocalModification{RecordedHash: "sha256:old", CurrentHash: "sha256:new"},
			},
		},
		Workflows: []string{"quick-fix"},
	}}
	svc := app.New(deps)

	// Act — ConflictDefault overwrite bypasses conflict prompts non-interactively.
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		ConflictDefault: domain.DecisionOverwrite,
		AutoConfirmPlan: true,
	})

	// Assert — executor must have been called so Content can be verified.
	if exec.capturedReq == nil {
		t.Fatal("executor was not called; cannot verify Content callback for infra agent")
	}
	// Assert — Content must resolve checkpoint-agent without error even when conflict
	// items are also present in the plan (agentByKey is built only from probeSet.Agents
	// and infraAgents, not from conflict-x or conflict-y, so their absence is expected).
	if err, bad := exec.contentErrors["checkpoint-agent"]; bad {
		t.Errorf("Content callback returned an error for \"checkpoint-agent\" "+
			"when conflict items are also present in the plan: %v; "+
			"infrastructure agents must be in agentByKey regardless of conflict item presence "+
			"(Stage 3: infra agents must be added to agentByKey in DeployNew)", err)
	}
}
