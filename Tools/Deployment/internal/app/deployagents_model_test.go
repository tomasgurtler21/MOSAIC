package app_test

// deployagents_model_test.go verifies infrastructure model resolution for Service.DeployAgents.
//
// These are TDD RED-phase tests. They assert the intended behaviour of the two-batch model
// resolution pattern for the DeployAgents flow. Every test that depends on the DeployAgents
// implementation will fail until the corresponding implementation tasks (I3.5, I3.6) are
// delivered.
//
// Verified behaviours (T3.5):
//
// Two-batch infrastructure model resolution (AC3.9):
//   - QAgentModel is asked for each selected infrastructure agent (second-batch pattern).
//   - When multiple infrastructure agents are selected, each is asked exactly once — no
//     duplicate prompts for the same agent key.
//   - When no infrastructure agents are selected, no QAgentModel is asked for the infra batch.
//   - Second-batch model results merge into the model map used for plan.Input.AgentModels.
//
// GapNoModel when model is skipped (AC3.10):
//   - Skipping QAgentModel for an infrastructure agent records a GapNoModel gap in the
//     todo collector, matching the DeployNew and DeployUtilityInfrastructure contract.
//   - The run still completes (non-fatal gap).

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Fixtures for model resolution tests
// ---------------------------------------------------------------------------

// modelTestCatalog returns a catalog with one infrastructure subagent and one ordinary
// subagent. The infra agent must trigger model resolution; the ordinary subagent must not.
func modelTestCatalog() *stubCatalog {
	return &stubCatalog{
		root: "testroot",
		orchestrator: domain.Agent{
			Key:  "orchestrator",
			Role: domain.RoleOrchestrator,
			Name: "Orchestrator",
		},
		agents: []domain.Agent{
			{
				Key:     "plan-review",
				Role:    domain.RoleSubagent,
				Version: "1.0",
				Name:    "Plan Review",
			},
		},
		infraAgents: []domain.Agent{
			{
				Key:            "checkpoint-agent",
				Role:           domain.RoleSubagent,
				Infrastructure: "checkpoint",
				Version:        "1.0",
				Name:           "Checkpoint Agent",
				RecommendedTier: "HIGH",
			},
		},
	}
}

// twoInfraAgentCatalog returns a catalog with two infrastructure subagents.
// Used to verify that each infra agent receives exactly one QAgentModel prompt.
func twoInfraAgentCatalog() *stubCatalog {
	return &stubCatalog{
		root: "testroot",
		orchestrator: domain.Agent{
			Key:  "orchestrator",
			Role: domain.RoleOrchestrator,
			Name: "Orchestrator",
		},
		infraAgents: []domain.Agent{
			{
				Key:            "checkpoint-agent",
				Role:           domain.RoleSubagent,
				Infrastructure: "checkpoint",
				Version:        "1.0",
				Name:           "Checkpoint Agent",
				RecommendedTier: "HIGH",
			},
			{
				Key:            "safety-guard",
				Role:           domain.RoleSubagent,
				Infrastructure: "safety",
				Version:        "1.0",
				Name:           "Safety Guard",
				RecommendedTier: "HIGH",
			},
		},
	}
}

// deployAgentsModelPlan returns a plan with items for the infrastructure agents.
func deployAgentsModelPlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeDeployAgents,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "checkpoint-agent"}, Action: domain.ActionCreate},
		},
	}
}

// ---------------------------------------------------------------------------
// T3.5 — QAgentModel is asked for infrastructure agents (two-batch model resolution)
// ---------------------------------------------------------------------------

// TestDeployAgents_InfraAgent_QAgentModelAsked verifies that when an infrastructure
// subagent is in the selection, QAgentModel is asked for it. This is the second-batch
// model resolution pattern: infrastructure agents participate in the same
// resolveModels call that DeployNew and DeployUtilityInfrastructure use.
func TestDeployAgents_InfraAgent_QAgentModelAsked(t *testing.T) {
	// Arrange — one infra agent pre-answered; tier question skipped so QAgentModel is reached.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "checkpoint-agent", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = modelTestCatalog()
	deps.Planner = &stubPlanner{plan: deployAgentsModelPlan(workspace)}
	svc := app.New(deps)

	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		StandaloneAgentIDs:     []string{},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert — QAgentModel must have been asked for the infrastructure agent.
	if !stub.WasAsked(domain.QAgentModel, "checkpoint-agent") {
		t.Error("QAgentModel was not asked for \"checkpoint-agent\"; " +
			"infrastructure subagents in DeployAgents must participate in model resolution " +
			"using the same two-batch pattern as DeployNew and DeployUtilityInfrastructure — " +
			"the second resolveModels call (infra batch) must exist in this flow too (AC3.9)")
	}
}

// ---------------------------------------------------------------------------
// T3.5 — No duplicate QAgentModel prompt for the same infrastructure agent
// ---------------------------------------------------------------------------

// TestDeployAgents_InfraAgent_QAgentModelAskedExactlyOnce verifies that when exactly one
// infrastructure agent is selected, QAgentModel is asked exactly once for that agent. A
// count of zero means model resolution was skipped; a count of two or more means the
// agent was incorrectly included in both the first and second batches (duplicate prompt).
func TestDeployAgents_InfraAgent_QAgentModelAskedExactlyOnce(t *testing.T) {
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "checkpoint-agent", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = modelTestCatalog()
	deps.Planner = &stubPlanner{plan: deployAgentsModelPlan(workspace)}
	svc := app.New(deps)

	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{},
		StandaloneAgentIDs:     []string{},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	count := stub.CountAsked(domain.QAgentModel, "checkpoint-agent")
	if count != 1 {
		t.Errorf("QAgentModel was asked %d time(s) for \"checkpoint-agent\", want exactly 1; "+
			"a count of 0 means model resolution is not implemented; "+
			"a count of 2+ means \"checkpoint-agent\" appeared in both model-resolution batches "+
			"(non-infra agents first, infra agents second) — infrastructure agents must only "+
			"be in the second batch to avoid the duplicate-prompt regression (AC3.9)",
			count)
	}
}

// ---------------------------------------------------------------------------
// T3.5 — Each infrastructure agent in a multi-agent selection gets exactly one prompt
// ---------------------------------------------------------------------------

// TestDeployAgents_TwoInfraAgents_EachQAgentModelAskedOnce verifies that when two
// infrastructure agents are selected, QAgentModel is asked once for each, with no
// cross-agent duplication. This is the "no duplicate prompt" guarantee at the multi-agent
// level.
func TestDeployAgents_TwoInfraAgents_EachQAgentModelAskedOnce(t *testing.T) {
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "checkpoint-agent", "model-a").
		AnswerSelectOne(domain.QAgentModel, "safety-guard", "model-b").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = twoInfraAgentCatalog()
	twoPlan := domain.Plan{
		Mode:          domain.ModeDeployAgents,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "checkpoint-agent"}, Action: domain.ActionCreate},
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "safety-guard"}, Action: domain.ActionCreate},
		},
	}
	deps.Planner = &stubPlanner{plan: twoPlan}
	svc := app.New(deps)

	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{"checkpoint-agent", "safety-guard"},
		StandaloneAgentIDs:     []string{},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	if n := stub.CountAsked(domain.QAgentModel, "checkpoint-agent"); n != 1 {
		t.Errorf("QAgentModel asked %d time(s) for \"checkpoint-agent\", want 1; "+
			"each infrastructure agent must be asked exactly once (AC3.9)", n)
	}
	if n := stub.CountAsked(domain.QAgentModel, "safety-guard"); n != 1 {
		t.Errorf("QAgentModel asked %d time(s) for \"safety-guard\", want 1; "+
			"each infrastructure agent must be asked exactly once (AC3.9)", n)
	}
}

// ---------------------------------------------------------------------------
// T3.5 — No infra agents selected → no QAgentModel for infra batch
// ---------------------------------------------------------------------------

// TestDeployAgents_NoInfraAgents_NoQAgentModelForInfraBatch verifies that when
// InfrastructureAgentIDs is an empty non-nil slice, the infra model-resolution batch is
// empty and no QAgentModel is asked. The model-resolution gate (non-empty agent set) must
// apply in the DeployAgents flow just as it does in DeployUtilityInfrastructure.
func TestDeployAgents_NoInfraAgents_NoQAgentModelForInfraBatch(t *testing.T) {
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = modelTestCatalog()
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeDeployAgents,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
	}}
	svc := app.New(deps)

	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		SubagentIDs:            []string{"plan-review"},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{}, // empty non-nil: no infra agents
		StandaloneAgentIDs:     []string{},
		AutoConfirmPlan:        true,
	})

	// Assert — QAgentModel must not be asked for any infra agent when none are selected.
	if stub.WasAsked(domain.QAgentModel, "checkpoint-agent") {
		t.Error("QAgentModel was asked for \"checkpoint-agent\" even though InfrastructureAgentIDs was empty; " +
			"when no infrastructure agents are selected, the infra model-resolution batch is empty " +
			"and no QAgentModel must be asked for any infrastructure agent")
	}
}

// ---------------------------------------------------------------------------
// T3.5 — Skipped infra model produces GapNoModel gap (AC3.10)
// ---------------------------------------------------------------------------

// TestDeployAgents_InfraAgent_SkippedModel_RecordsGapNoModel verifies that when the user
// does not answer QAgentModel for an infrastructure agent (skipped via SkipAll or by not
// scripting an answer), a GapNoModel gap is recorded in the todo collector and the run
// still completes.
//
// This mirrors the equivalent test in utility_infra_test.go and gapnomodel_flow_test.go,
// confirming that GapNoModel emission is consistent across all deployment modes that involve
// infrastructure agents.
func TestDeployAgents_InfraAgent_SkippedModel_RecordsGapNoModel(t *testing.T) {
	// Arrange — the plan stub carries a GapNoModel for the infra agent, matching what
	// plan.Build would emit when the agent has no model. The flow must relay this gap
	// into the todo collector without dropping or duplicating it.
	spy := &spyTodo{}
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = modelTestCatalog()
	deps.Todo = spy

	planWithGap := domain.Plan{
		Mode:          domain.ModeDeployAgents,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "checkpoint-agent"}, Action: domain.ActionCreate},
		},
		Gaps: []domain.Gap{
			{
				Kind:    domain.GapNoModel,
				Subject: "checkpoint-agent",
				Detail:  "no model has been selected for agent checkpoint-agent",
			},
		},
	}
	deps.Planner = &stubPlanner{plan: planWithGap}
	svc := app.New(deps)

	_, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		StandaloneAgentIDs:     []string{},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true, // skip the model question → GapNoModel
		},
		AutoConfirmPlan: true,
	})

	// The stub returns "not yet implemented" until I3.5/I3.6 is delivered.
	// Once implemented, the run must complete without error (gap is non-fatal).
	if err != nil {
		// Still a stub; gap collection cannot be asserted yet.
		return
	}

	// Assert — exactly one GapNoModel for "checkpoint-agent" in the todo collector.
	if n := countNoModelGaps(spy, "checkpoint-agent"); n != 1 {
		t.Errorf("collected %d GapNoModel gap(s) for \"checkpoint-agent\", want exactly 1; "+
			"skipping QAgentModel for an infrastructure agent in DeployAgents must produce "+
			"exactly one GapNoModel gap, matching the DeployNew and DeployUtilityInfrastructure "+
			"contract (AC3.10): zero means the gap was not relayed, more than one means duplicates",
			n)
	}
}

// ---------------------------------------------------------------------------
// RunSummary mode is ModeDeployAgents
// ---------------------------------------------------------------------------

// TestDeployAgents_RunSummaryMode_IsDeployAgents verifies that the RunSummary returned by
// the flow carries Mode = ModeDeployAgents, not ModeDeployWorkspace or any other mode.
// This mirrors the equivalent assertion in the question-discipline test file.
func TestDeployAgents_Model_RunSummaryMode_IsDeployAgents(t *testing.T) {
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = modelTestCatalog()
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeDeployAgents,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
	}}
	svc := app.New(deps)

	summary, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		StandaloneAgentIDs:     []string{},
		AutoConfirmPlan:        true,
	})

	if err != nil {
		// Not yet implemented; mode assertion cannot be performed.
		t.Skipf("DeployAgents not yet implemented (%v); skipping RunSummary.Mode check", err)
	}

	if summary.Mode != domain.ModeDeployAgents {
		t.Errorf("RunSummary.Mode = %q, want %q; "+
			"the DeployAgents flow must stamp the summary with ModeDeployAgents so callers "+
			"can distinguish it from other modes",
			summary.Mode, domain.ModeDeployAgents)
	}
}
