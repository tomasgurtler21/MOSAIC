package app_test

// infraagent_test.go verifies that DeployNew includes infrastructure agents in model
// resolution and content generation.
//
// Verified invariants:
//
// Model resolution (T3.1):
//   - QAgentModel is asked for each selected infrastructure agent (no silent GapNoModel)
//   - Infra-agent model questions are a distinct batch: all regular-agent QAgentModel calls
//     precede all infra-agent QAgentModel calls in the question sequence
//   - Resolved infra-agent models are included in plan.Input.Models
//   - Skip-all within the infra-agent batch stops subsequent infra-agent questions
//   - A custom model ID entered in the regular-agent batch appears as a selectable option
//     for infra-agent model questions (cross-batch propagation)
//   - Tier models resolved in the regular-agent batch are not re-asked in the infra batch
//   - Agent skip-all triggered in the regular-agent batch suppresses infra-agent questions
//
// Content generation (T3.2):
//   - The Content callback resolves an infra-agent plan item without "unknown agent" error
//   - VersionStamps includes an entry for each infra-agent plan item

import (
	"context"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Infrastructure agent fixtures
// ---------------------------------------------------------------------------

// minimalInfraAgent is a stub infrastructure agent with the Infrastructure field set,
// making it a member of the infrastructure-agent class vocabulary.
var minimalInfraAgent = domain.Agent{
	Key:             "checkpoint-agent",
	NumericID:       "10",
	Version:         "2.0",
	Name:            "Checkpoint Agent",
	Description:     "Creates checkpoints between stages",
	Role:            domain.RoleWorker,
	Infrastructure:  "checkpoint",
	RecommendedTier: "HIGH",
}

// secondInfraAgent is a second stub infrastructure agent used in multi-infra-agent scenarios.
var secondInfraAgent = domain.Agent{
	Key:             "commit-agent",
	NumericID:       "11",
	Version:         "1.0",
	Name:            "Commit Agent",
	Description:     "Commits workspace changes after a stage",
	Role:            domain.RoleWorker,
	Infrastructure:  "commit",
	RecommendedTier: "HIGH",
}

// catalogWithOneInfraAgent returns a catalog containing the standard regular agents plus
// minimalInfraAgent in the infrastructure agents list.
func catalogWithOneInfraAgent() *stubCatalog {
	cat := newMinimalCatalog()
	cat.infraAgents = []domain.Agent{minimalInfraAgent}
	return cat
}

// catalogWithTwoInfraAgents returns a catalog with two infrastructure agents.
func catalogWithTwoInfraAgents() *stubCatalog {
	cat := newMinimalCatalog()
	cat.infraAgents = []domain.Agent{minimalInfraAgent, secondInfraAgent}
	return cat
}

// infraAgentPlanItem returns a plan item for the given infrastructure agent.
func infraAgentPlanItem(agent domain.Agent, targetPath string) domain.PlanItem {
	return domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: agent.Key},
		TargetPath: targetPath,
		Action:     domain.ActionCreate,
	}
}

// ---------------------------------------------------------------------------
// capturingExecutor — invokes the Content callback and records ExecRequest
// ---------------------------------------------------------------------------

// capturingExecutor records the ExecRequest passed to Execute and invokes the Content
// callback for every agent-kind plan item, capturing any errors. This lets tests assert
// both "no unknown agent" and "VersionStamps present" without depending on the real executor.
type capturingExecutor struct {
	capturedReq   *deploy.ExecRequest
	contentErrors map[string]error // agent key -> error from Content callback
}

func (e *capturingExecutor) Execute(_ context.Context, req deploy.ExecRequest) (deploy.ExecResult, error) {
	cp := req
	e.capturedReq = &cp
	e.contentErrors = make(map[string]error)
	if req.Content != nil {
		for _, item := range req.Plan.Items {
			if item.Ref.Kind == domain.ArtifactAgent {
				if _, err := req.Content(item); err != nil {
					e.contentErrors[item.Ref.Key] = err
				}
			}
		}
	}
	return deploy.ExecResult{DeploymentRoot: req.Plan.WorkspacePath}, nil
}

// ---------------------------------------------------------------------------
// Interaction helpers for infra-agent model resolution tests
// ---------------------------------------------------------------------------

// skipAllOnFirstQAgentModelCall sends SkippedAll on the very first QAgentModel call and
// records the subject of every QAgentModel call made. Other question types return SkippedOne.
type skipAllOnFirstQAgentModelCall struct {
	agentModelSubjects []string
	skippedAllSent     bool
}

func (s *skipAllOnFirstQAgentModelCall) SelectOne(_ context.Context, q domain.ChoiceQuestion) (domain.ChoiceAnswer, error) {
	if q.ID == domain.QAgentModel {
		s.agentModelSubjects = append(s.agentModelSubjects, q.Subject)
		if !s.skippedAllSent {
			s.skippedAllSent = true
			return domain.ChoiceAnswer{Status: domain.SkippedAll}, nil
		}
	}
	return domain.ChoiceAnswer{Status: domain.SkippedOne}, nil
}
func (s *skipAllOnFirstQAgentModelCall) SelectMany(_ context.Context, _ domain.ChoiceQuestion) (domain.MultiChoiceAnswer, error) {
	return domain.MultiChoiceAnswer{Status: domain.SkippedOne}, nil
}
func (s *skipAllOnFirstQAgentModelCall) AskText(_ context.Context, _ domain.TextQuestion) (domain.TextAnswer, error) {
	return domain.TextAnswer{Status: domain.SkippedOne}, nil
}
func (s *skipAllOnFirstQAgentModelCall) Confirm(_ context.Context, _ domain.Question) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}
func (s *skipAllOnFirstQAgentModelCall) Notify(_ context.Context, _ domain.Notice)          {}
func (s *skipAllOnFirstQAgentModelCall) Progress(_ context.Context, _ domain.ProgressEvent) {}
func (s *skipAllOnFirstQAgentModelCall) Review(_ context.Context, _ domain.Plan) (domain.ConfirmAnswer, error) {
	return domain.ConfirmAnswer{Status: domain.Answered, Confirm: true}, nil
}

// ---------------------------------------------------------------------------
// T3.1 — Model resolution: QAgentModel asked for infra agents
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgent_QAgentModelAsked verifies that when infrastructure agents are
// selected, QAgentModel is asked for each of them. Without the fix, no second resolveModels
// call exists for the infra batch, so QAgentModel is never presented for infra agents.
func TestDeployNew_InfraAgent_QAgentModelAsked(t *testing.T) {
	// Arrange — catalog with one infra agent; skip tier questions so QAgentModel is reached.
	cat := catalogWithOneInfraAgent()
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QAgentModel, "checkpoint-agent"}: {Status: domain.Answered, OptionID: "model-a"},
			{domain.QAgentModel, "orchestrator"}:     {Status: domain.Answered, OptionID: "model-a"},
			{domain.QAgentModel, "test-runner"}:      {Status: domain.Answered, OptionID: "model-a"},
		},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert — QAgentModel must have been asked for the infrastructure agent.
	infraAsked := false
	for _, rec := range capture.recorded {
		if rec.ID == domain.QAgentModel && rec.Subject == "checkpoint-agent" {
			infraAsked = true
			break
		}
	}
	if !infraAsked {
		t.Error("QAgentModel was not asked for \"checkpoint-agent\"; " +
			"infrastructure agents must participate in model resolution (resolveModels must be " +
			"called a second time with the infra agents slice during DeployNew)")
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Model resolution: separate batch, not interleaved
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgent_ModelQuestionsNotInterleaved verifies that infra-agent model
// questions are presented as a distinct, non-interleaved batch that follows all regular-agent
// model questions. Without the fix, infra agents are never asked at all.
func TestDeployNew_InfraAgent_ModelQuestionsNotInterleaved(t *testing.T) {
	// Arrange — one regular agent (test-runner) and one infra agent (checkpoint-agent),
	// both needing QAgentModel (no tier mapping, tier questions skipped).
	cat := catalogWithOneInfraAgent()
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QAgentModel, "orchestrator"}:    {Status: domain.Answered, OptionID: "model-a"},
			{domain.QAgentModel, "test-runner"}:     {Status: domain.Answered, OptionID: "model-a"},
			{domain.QAgentModel, "checkpoint-agent"}: {Status: domain.Answered, OptionID: "model-b"},
		},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert — all regular-agent QAgentModel calls must precede all infra-agent QAgentModel
	// calls. A missing infra-agent call is also an error.
	infraAgentKeys := map[string]bool{"checkpoint-agent": true}
	lastRegularIdx := -1
	firstInfraIdx := -1
	for i, rec := range capture.recorded {
		if rec.ID != domain.QAgentModel {
			continue
		}
		if infraAgentKeys[rec.Subject] {
			if firstInfraIdx == -1 {
				firstInfraIdx = i
			}
		} else {
			lastRegularIdx = i
		}
	}
	if firstInfraIdx == -1 {
		t.Fatal("QAgentModel was not asked for infra agent \"checkpoint-agent\"; " +
			"without a second resolveModels call the infra batch does not exist (Stage 3 not implemented)")
	}
	if lastRegularIdx != -1 && lastRegularIdx > firstInfraIdx {
		t.Errorf("model questions were interleaved: last regular-agent QAgentModel at position %d, "+
			"first infra-agent QAgentModel at position %d; "+
			"the infra-agent batch must be a separate resolveModels invocation that runs after "+
			"the regular-agent batch completes",
			lastRegularIdx, firstInfraIdx)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Model resolution: models passed to plan.Input.Models
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgent_ResolvedModelInPlanInput verifies that the model selection for
// an infrastructure agent is merged into plan.Input.Models before the plan is built. Without
// the fix, infra-agent models are absent from plan.Input.Models.
func TestDeployNew_InfraAgent_ResolvedModelInPlanInput(t *testing.T) {
	// Arrange — capturingPlanner records plan.Input so we can inspect Models.
	cat := catalogWithOneInfraAgent()
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QAgentModel, "checkpoint-agent"}: {Status: domain.Answered, OptionID: "model-b"},
		},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = capture
	cp := &capturingPlanner{result: newMinimalPlan(workspace)}
	deps.Planner = cp
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true, domain.QAgentModel: false},
		// Pre-answer regular agents so only checkpoint-agent gets QAgentModel
		AgentModels:     map[string]string{"orchestrator": "model-a", "test-runner": "model-a"},
		AutoConfirmPlan: true,
	})

	// Assert
	if cp.capturedInput == nil {
		t.Fatal("capturingPlanner did not record plan.Input; cannot verify Models")
	}
	sel, ok := cp.capturedInput.Models["checkpoint-agent"]
	if !ok {
		t.Errorf("plan.Input.Models does not contain an entry for \"checkpoint-agent\"; "+
			"the models map returned by the infra-agent resolveModels call must be merged into "+
			"plan.Input.Models before plan.Build is called")
		return
	}
	if sel.ModelID != "model-b" {
		t.Errorf("plan.Input.Models[\"checkpoint-agent\"].ModelID = %q, want %q",
			sel.ModelID, "model-b")
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Model resolution: skip-all within the infra batch
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgent_SkipAll_StopsAskingSubsequentInfraAgents verifies that when the
// user chooses skip-all during the infra-agent batch, subsequent infrastructure agents are not
// asked. Without the fix, neither infra agent is asked at all (the infra batch does not exist),
// which causes the prerequisite assertion to fail.
func TestDeployNew_InfraAgent_SkipAll_StopsAskingSubsequentInfraAgents(t *testing.T) {
	// Arrange — two infra agents; regular agents pre-answered so only infra agents reach QAgentModel.
	// The first infra agent (checkpoint-agent) returns SkippedAll; commit-agent must not be asked.
	cat := catalogWithTwoInfraAgents()
	interaction := &skipAllOnFirstQAgentModelCall{}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = interaction
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent", "commit-agent"},
		// Pre-answer regular agents so the first QAgentModel belongs to the infra batch.
		AgentModels:     map[string]string{"orchestrator": "model-a", "test-runner": "model-a"},
		SkipAll:         map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan: true,
	})

	// Assert — checkpoint-agent must have been asked (the infra batch must exist).
	checkpointAsked := false
	for _, subj := range interaction.agentModelSubjects {
		if subj == "checkpoint-agent" {
			checkpointAsked = true
		}
	}
	if !checkpointAsked {
		t.Fatal("QAgentModel was not asked for \"checkpoint-agent\"; " +
			"the infra-agent batch must exist and present QAgentModel for each infra agent")
	}
	// Assert — commit-agent must not have been asked after skip-all.
	for _, subj := range interaction.agentModelSubjects {
		if subj == "commit-agent" {
			t.Error("QAgentModel was asked for \"commit-agent\" even though SkippedAll was " +
				"returned for \"checkpoint-agent\"; skip-all within the infra batch must stop " +
				"subsequent infra-agent model questions")
		}
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Model resolution: cross-batch custom model propagation (AC3.7)
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgent_CrossBatchCustomModel_AppearsAsOption verifies that a custom
// model ID entered for a regular agent during the first resolveModels call appears as a
// selectable option when infra-agent model questions are presented in the second call.
// Without the fix, QAgentModel is never asked for infra agents, causing a Fatal.
// With the fix but without state threading, the custom ID would be absent from options.
func TestDeployNew_InfraAgent_CrossBatchCustomModel_AppearsAsOption(t *testing.T) {
	// Arrange — test-runner enters custom model "cross-batch-custom-x".
	// checkpoint-agent must see it as an option when its own QAgentModel is presented.
	cat := catalogWithOneInfraAgent()
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QAgentModel, "test-runner"}: {
				Status: domain.Answered,
				Custom: "cross-batch-custom-x",
			},
			// checkpoint-agent: not scripted → SkippedOne; we only inspect the options list.
		},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert — infra agent must see the custom model entered for the regular agent.
	infraOptions := capture.optionsFor(domain.QAgentModel, "checkpoint-agent")
	if infraOptions == nil {
		t.Fatal("QAgentModel was not asked for \"checkpoint-agent\"; " +
			"cannot verify cross-batch custom model propagation (AC3.7) — " +
			"infra-agent resolveModels call does not exist yet")
	}
	if !optionIDsPresent(infraOptions, "cross-batch-custom-x") {
		t.Errorf("QAgentModel options for \"checkpoint-agent\" = %v; "+
			"want to contain \"cross-batch-custom-x\" entered for \"test-runner\" in the regular batch; "+
			"accumulatedOptions from the first resolveModels call must be passed as extraOptions "+
			"to the second call so infra agents see custom IDs from the regular batch (AC3.7)",
			infraOptions)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Model resolution: tier not re-asked in infra batch (AC3.8)
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgent_TierAnsweredInRegularBatch_NotReAsked verifies that when a tier
// model is resolved during the regular-agent batch, QTierModel is not asked again for the
// same tier during the infra-agent batch. This tests that tierModelsUsed from the first call
// is threaded into the second call as the tierModels parameter.
func TestDeployNew_InfraAgent_TierAnsweredInRegularBatch_NotReAsked(t *testing.T) {
	// Arrange — infra agent is in HIGH tier (same tier as test-runner).
	// QTierModel for HIGH is answered in the regular batch; it must not be re-asked for infra.
	cat := catalogWithOneInfraAgent()
	capture := &optionCapturingInteraction{
		selectOneAnswers: map[customModelPropagationKey]domain.ChoiceAnswer{
			{domain.QTierModel, "HIGH"}: {Status: domain.Answered, OptionID: "model-a"},
			// If the infra batch incorrectly re-asks QTierModel "HIGH", it would be recorded.
		},
	}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = capture
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		AutoConfirmPlan:        true,
	})

	// Assert — QTierModel for "HIGH" must be asked at most once.
	tierHighCount := 0
	for _, rec := range capture.recorded {
		if rec.ID == domain.QTierModel && rec.Subject == "HIGH" {
			tierHighCount++
		}
	}
	if tierHighCount > 1 {
		t.Errorf("QTierModel for tier \"HIGH\" was asked %d times; "+
			"a tier model answered in the regular-agent batch must not be re-asked in the "+
			"infra-agent batch — tierModelsUsed from the first resolveModels call must be "+
			"passed as the tierModels parameter of the second call (AC3.8)",
			tierHighCount)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Model resolution: agent skip-all from regular batch carries to infra (AC3.8)
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgent_AgentSkipAllInRegularBatch_CarriesOverToInfraBatch verifies that
// when the user chooses skip-all for QAgentModel during the regular-agent batch, infra-agent
// model questions are also suppressed. This tests that agentSkippedAll from the first call is
// passed as agentSkipOverride to the second call.
func TestDeployNew_InfraAgent_AgentSkipAllInRegularBatch_CarriesOverToInfraBatch(t *testing.T) {
	// Arrange — the orchestrator is the first regular agent and will trigger SkippedAll.
	// test-runner (also regular) must then be skipped, and checkpoint-agent (infra) must not
	// be asked because agentSkippedAll carries into the second resolveModels call.
	cat := catalogWithOneInfraAgent()
	// skipAllOnFirstQAgentModelCall sends SkippedAll on the first QAgentModel call (orchestrator).
	interaction := &skipAllOnFirstQAgentModelCall{}
	deps, workspace := newBaseDeps(t, nil)
	deps.Catalog = cat
	deps.Interaction = interaction
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert — SkippedAll must have been triggered in the regular batch.
	if !interaction.skippedAllSent {
		t.Fatal("SkippedAll was not triggered; interaction did not send SkippedAll for the " +
			"first QAgentModel call as expected")
	}
	// Assert — checkpoint-agent must not be asked because skip-all carries into the infra batch.
	for _, subj := range interaction.agentModelSubjects {
		if subj == "checkpoint-agent" {
			t.Error("QAgentModel was asked for \"checkpoint-agent\" even though SkippedAll was " +
				"returned during the regular-agent batch; the agentSkippedAll return value of the " +
				"first resolveModels call must be passed as agentSkipOverride to the second call " +
				"so skip-all state carries into the infra-agent batch")
		}
	}
}

// ---------------------------------------------------------------------------
// T3.2 — Content generation: no "unknown agent" error
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgent_ContentCallbackResolvesWithoutError verifies that the Content
// callback passed to the executor can resolve an infra-agent plan item without returning an
// "unknown agent" error. Without the fix, infra agents are absent from the agentByKey map
// passed to buildContent, causing buildContent to return "unknown agent %q".
func TestDeployNew_InfraAgent_ContentCallbackResolvesWithoutError(t *testing.T) {
	// Arrange — plan includes both a regular-agent and an infra-agent ActionCreate item.
	// The capturingExecutor invokes the Content callback for each agent item.
	cat := catalogWithOneInfraAgent()
	stub := interactiontest.NewBuilder().Build() // default Review → Confirm
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = cat
	planWithInfra := domain.Plan{
		Mode:          domain.ModeDeployWorkspace,
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
		},
		Workflows: []string{"quick-fix"},
	}
	exec := &capturingExecutor{}
	deps.Planner = &stubPlanner{plan: planWithInfra}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})

	// Assert — executor must have been reached.
	if exec.capturedReq == nil {
		t.Fatal("executor was not called; DeployNew must reach execution for content generation " +
			"to be verifiable")
	}
	// Assert — Content callback must not return "unknown agent" for the infra agent.
	if err, bad := exec.contentErrors["checkpoint-agent"]; bad {
		if strings.Contains(err.Error(), "unknown agent") {
			t.Errorf("Content callback returned \"unknown agent\" error for \"checkpoint-agent\": %v; "+
				"infrastructure agents must be added to the agentByKey map passed to buildContent "+
				"so the Content callback can resolve their plan items (I3.3)", err)
		} else {
			t.Errorf("Content callback returned unexpected error for \"checkpoint-agent\": %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// T3.2 — Content generation: VersionStamps includes infra items
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgent_VersionStampsIncludesInfraItems verifies that the VersionStamps
// map passed to the executor contains an entry for each infra-agent plan item, keyed by
// target path. Without the fix, buildVersionStamps only receives probeSet.Agents (which
// excludes infra agents), leaving infra-agent items without version stamps.
func TestDeployNew_InfraAgent_VersionStampsIncludesInfraItems(t *testing.T) {
	// Arrange — plan includes an infra-agent ActionCreate item.
	cat := catalogWithOneInfraAgent()
	stub2 := interactiontest.NewBuilder().Build() // default Review → Confirm
	deps, workspace := newBaseDeps(t, stub2)
	deps.Catalog = cat
	planWithInfra := domain.Plan{
		Mode:          domain.ModeDeployWorkspace,
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
		},
		Workflows: []string{"quick-fix"},
	}
	exec := &capturingExecutor{}
	deps.Planner = &stubPlanner{plan: planWithInfra}
	deps.Executor = exec
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})

	// Assert — executor must have been reached.
	if exec.capturedReq == nil {
		t.Fatal("executor was not called; cannot verify VersionStamps")
	}
	// Assert — VersionStamps must contain an entry for the infra agent's target path.
	stamp, ok := exec.capturedReq.VersionStamps["checkpoint-agent.md"]
	if !ok {
		t.Errorf("VersionStamps does not contain an entry for \"checkpoint-agent.md\"; "+
			"infra-agent structs must be merged into the agents slice passed to buildVersionStamps "+
			"so that version stamps are computed for infra-agent plan items (I3.4)")
		return
	}
	// The Version field must match the infra agent's declared version.
	if stamp.Version != minimalInfraAgent.Version {
		t.Errorf("VersionStamps[\"checkpoint-agent.md\"].Version = %q, want %q",
			stamp.Version, minimalInfraAgent.Version)
	}
}
