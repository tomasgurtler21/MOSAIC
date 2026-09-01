package app_test

// deployagents_perage_model_fallback_test.go verifies that per-agent model prompts
// (QAgentModel) are presented for all agent classes in Service.DeployAgents when their
// tier model is unresolved.
//
// These are TDD RED-phase tests. They will fail until the implementation in
// deployagents_service.go passes agentSkipOverride=false (instead of true) to the first-batch
// resolveModels call, enabling QAgentModel for utility, standalone, and ordinary subagent
// classes alongside the existing infrastructure-agent behavior.
//
// Verified behaviours (new per-agent fallback, all agent classes):
//
//   - A utility agent with an unresolved tier receives a QAgentModel prompt.
//   - A standalone agent with an unresolved tier receives a QAgentModel prompt.
//   - An ordinary subagent with an unresolved tier receives a QAgentModel prompt.
//   - When all four agent classes are present with unresolved tiers, every non-infra
//     agent class (utility, standalone, ordinary subagent) in the first batch receives a
//     QAgentModel prompt. Infrastructure agents are covered by the infra-batch tests.
//
// Verified unchanged behaviour (tier-resolved exemption):
//
//   - When a tier model is pre-resolved for an agent's tier (via TierModels), no
//     QAgentModel prompt is presented for that agent.

import (
	"context"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Fixtures for per-agent model fallback tests
// ---------------------------------------------------------------------------

// perAgentModelFallbackCatalog returns a catalog with one agent in each deployable class:
// ordinary subagent, utility agent, standalone agent, and infrastructure agent. Every agent
// declares RecommendedTier="HIGH" so tier resolution is the normal path; skipping tier
// questions leaves the tier unresolved and causes each agent to need a per-agent prompt.
func perAgentModelFallbackCatalog() *stubCatalog {
	return &stubCatalog{
		root: "testroot",
		orchestrator: domain.Agent{
			Key:  "orchestrator",
			Role: domain.RoleOrchestrator,
			Name: "Orchestrator",
		},
		agents: []domain.Agent{
			{
				Key:             "plan-review",
				Role:            domain.RoleSubagent,
				Version:         "1.0",
				Name:            "Plan Review",
				RecommendedTier: "HIGH",
			},
		},
		utilityAgents: []domain.Agent{
			{
				Key:             "file-manager",
				Role:            domain.RoleUtility,
				Version:         "1.0",
				Name:            "File Manager",
				RecommendedTier: "HIGH",
			},
		},
		standaloneAgents: []domain.Agent{
			{
				Key:             "session-logger",
				Role:            domain.RoleStandalone,
				Version:         "1.0",
				Name:            "Session Logger",
				RecommendedTier: "HIGH",
			},
		},
		infraAgents: []domain.Agent{
			{
				Key:             "checkpoint-agent",
				Role:            domain.RoleSubagent,
				Infrastructure:  "checkpoint",
				Version:         "1.0",
				Name:            "Checkpoint Agent",
				RecommendedTier: "HIGH",
			},
		},
		tiers: []domain.TierInfo{
			{
				Tier:      "HIGH",
				Rationale: "Capable model required",
				AgentKeys: []string{"plan-review", "file-manager", "session-logger", "checkpoint-agent"},
			},
		},
	}
}

// utilityOnlyPlan returns a plan with one item for the utility agent.
func utilityOnlyPlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeDeployAgents,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "file-manager"}, Action: domain.ActionCreate},
		},
	}
}

// standaloneOnlyPlan returns a plan with one item for the standalone agent.
func standaloneOnlyPlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeDeployAgents,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "session-logger"}, Action: domain.ActionCreate},
		},
	}
}

// subagentOnlyPlan returns a plan with one item for the ordinary subagent.
func subagentOnlyPlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeDeployAgents,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "plan-review"}, Action: domain.ActionCreate},
		},
	}
}

// allAgentClassesPlan returns a plan with items for all four agent classes.
func allAgentClassesPlan(workspace string) domain.Plan {
	return domain.Plan{
		Mode:          domain.ModeDeployAgents,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "plan-review"}, Action: domain.ActionCreate},
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "file-manager"}, Action: domain.ActionCreate},
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "session-logger"}, Action: domain.ActionCreate},
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "checkpoint-agent"}, Action: domain.ActionCreate},
		},
	}
}

// ---------------------------------------------------------------------------
// Utility agent receives QAgentModel when tier is unresolved
// ---------------------------------------------------------------------------

// TestDeployAgents_UtilityAgent_QAgentModelAsked_WhenTierUnresolved verifies that when a
// utility agent is selected and all tier dialogs are skipped (leaving the tier unresolved),
// the flow presents a QAgentModel prompt for the utility agent.
//
// Under the old behavior (agentSkipOverride=true), QAgentModel was suppressed for all
// non-infrastructure agents in the DeployAgents first batch. Under the new behavior
// (agentSkipOverride=false), every agent with an unresolved tier model receives a
// per-agent model prompt, including utility agents.
func TestDeployAgents_UtilityAgent_QAgentModelAsked_WhenTierUnresolved(t *testing.T) {
	// Arrange — utility agent pre-answered; tier questions skipped so QAgentModel is reached.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "file-manager", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = perAgentModelFallbackCatalog()
	deps.Planner = &stubPlanner{plan: utilityOnlyPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{"file-manager"},
		InfrastructureAgentIDs: []string{},
		StandaloneAgentIDs:     []string{},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert — QAgentModel must have been asked for the utility agent.
	if !stub.WasAsked(domain.QAgentModel, "file-manager") {
		t.Error("QAgentModel was not asked for \"file-manager\" (utility agent); " +
			"after skipping all tier dialogs, utility agents must receive a per-agent model " +
			"prompt — the first-batch resolveModels call must pass agentSkipOverride=false " +
			"so utility agents are no longer suppressed from model selection")
	}
}

// ---------------------------------------------------------------------------
// Standalone agent receives QAgentModel when tier is unresolved
// ---------------------------------------------------------------------------

// TestDeployAgents_StandaloneAgent_QAgentModelAsked_WhenTierUnresolved verifies that when a
// standalone agent is selected and all tier dialogs are skipped, the flow presents a
// QAgentModel prompt for the standalone agent.
//
// The old behavior suppressed QAgentModel for standalone agents in the DeployAgents first
// batch. The new behavior (agentSkipOverride=false) asks QAgentModel for every agent whose
// tier model remains unresolved, including standalone agents.
func TestDeployAgents_StandaloneAgent_QAgentModelAsked_WhenTierUnresolved(t *testing.T) {
	// Arrange — standalone agent pre-answered; tier questions skipped so QAgentModel is reached.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "session-logger", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = perAgentModelFallbackCatalog()
	deps.Planner = &stubPlanner{plan: standaloneOnlyPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		StandaloneAgentIDs:     []string{"session-logger"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert — QAgentModel must have been asked for the standalone agent.
	if !stub.WasAsked(domain.QAgentModel, "session-logger") {
		t.Error("QAgentModel was not asked for \"session-logger\" (standalone agent); " +
			"after skipping all tier dialogs, standalone agents must receive a per-agent model " +
			"prompt — the first-batch resolveModels call must pass agentSkipOverride=false " +
			"so standalone agents are no longer suppressed from model selection")
	}
}

// ---------------------------------------------------------------------------
// Non-infrastructure agent classes receive QAgentModel when tier is unresolved
// ---------------------------------------------------------------------------

// TestDeployAgents_NonInfraAgentClasses_QAgentModelAsked_WhenTierUnresolved verifies that
// when all four agent classes are present and all tier dialogs are skipped, each non-infra
// agent (utility, standalone, and ordinary subagent) receives a QAgentModel prompt.
// Infrastructure agents are covered separately via the existing infra-batch tests.
//
// This is the combined non-infra fallback scenario: three non-infra agent classes, tier
// skipped, all non-infra agents get per-agent prompts in the first batch.
func TestDeployAgents_NonInfraAgentClasses_QAgentModelAsked_WhenTierUnresolved(t *testing.T) {
	// Arrange — all first-batch agents scripted; tier questions skipped.
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, "plan-review", "model-a").
		AnswerSelectOne(domain.QAgentModel, "file-manager", "model-a").
		AnswerSelectOne(domain.QAgentModel, "session-logger", "model-a").
		AnswerSelectOne(domain.QAgentModel, "checkpoint-agent", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = perAgentModelFallbackCatalog()
	deps.Planner = &stubPlanner{plan: allAgentClassesPlan(workspace)}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		SubagentIDs:            []string{"plan-review"},
		UtilityAgentIDs:        []string{"file-manager"},
		InfrastructureAgentIDs: []string{"checkpoint-agent"},
		StandaloneAgentIDs:     []string{"session-logger"},
		SkipAll:                map[domain.QuestionID]bool{domain.QTierModel: true},
		AutoConfirmPlan:        true,
	})

	// Assert — QAgentModel must have been asked for every first-batch (non-infra) agent class.
	if !stub.WasAsked(domain.QAgentModel, "plan-review") {
		t.Error("QAgentModel was not asked for \"plan-review\" (ordinary subagent); " +
			"all non-infrastructure agents must receive per-agent model prompts when tier is " +
			"unresolved — the first-batch resolveModels call must use agentSkipOverride=false")
	}
	if !stub.WasAsked(domain.QAgentModel, "file-manager") {
		t.Error("QAgentModel was not asked for \"file-manager\" (utility agent); " +
			"all non-infrastructure agents must receive per-agent model prompts when tier is " +
			"unresolved — the first-batch resolveModels call must use agentSkipOverride=false")
	}
	if !stub.WasAsked(domain.QAgentModel, "session-logger") {
		t.Error("QAgentModel was not asked for \"session-logger\" (standalone agent); " +
			"all non-infrastructure agents must receive per-agent model prompts when tier is " +
			"unresolved — the first-batch resolveModels call must use agentSkipOverride=false")
	}
}

// ---------------------------------------------------------------------------
// Tier-resolved agents are exempt from QAgentModel (unchanged behavior)
// ---------------------------------------------------------------------------

// TestDeployAgents_UtilityAgent_NoQAgentModel_WhenTierResolved verifies that when a utility
// agent's tier model is pre-resolved (supplied via TierModels), no QAgentModel prompt is
// presented for that agent. This is existing behavior that must be preserved after the
// agentSkipOverride change.
//
// The exemption applies to all agent classes: if TierModels supplies a model for the agent's
// recommended tier, the agent's model is resolved at the tier level and QAgentModel is
// bypassed.
func TestDeployAgents_UtilityAgent_NoQAgentModel_WhenTierResolved(t *testing.T) {
	// Arrange — tier "HIGH" pre-resolved via TierModels; no QAgentModel answer scripted.
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = perAgentModelFallbackCatalog()
	deps.Planner = &stubPlanner{plan: utilityOnlyPlan(workspace)}
	svc := app.New(deps)

	// Act — TierModels pre-resolves "HIGH" so QAgentModel must be bypassed for file-manager.
	_, _ = svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{"file-manager"},
		InfrastructureAgentIDs: []string{},
		StandaloneAgentIDs:     []string{},
		TierModels:             map[domain.Tier]string{"HIGH": "model-a"},
		AutoConfirmPlan:        true,
	})

	// Assert — QAgentModel must NOT be asked when the agent's tier is already resolved.
	if stub.WasAsked(domain.QAgentModel, "file-manager") {
		t.Error("QAgentModel was asked for \"file-manager\" even though its tier \"HIGH\" was " +
			"pre-resolved via TierModels; when a tier model is resolved for an agent's tier, " +
			"no per-agent model prompt must be presented for that agent (unchanged behavior)")
	}
}
