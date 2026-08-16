package app_test

// deployagents_deployed_state_test.go retargets the deployed-state probing invariant
// onto Service.DeployAgents.
//
// The legacy standalone-only mode carried a test (T5.4) that verified: when a standalone
// agent's target file already exists in the workspace, a subsequent run does not classify
// the agent as ActionCreate. That behaviour belongs to any mode that selects standalone
// agents — including DeployAgents. These tests preserve that coverage on the merged mode.
//
// These are TDD RED-phase tests. They will fail until I6.2 delivers the DeployAgents
// implementation with a correctly-built probe set.
//
// Verified behaviours:
//
// Probe-set includes selected standalone agents (T6.2):
//   - When a standalone agent's target file already exists in the workspace and the real
//     planner is used, DeployAgents does NOT classify the agent as ActionCreate on a second
//     run. This confirms the probe set is built to cover all selected agent classes.
//
// Probe-set includes selected ordinary subagents (T6.2):
//   - Same invariant for an ordinary subagent (Role=RoleSubagent, Infrastructure=""),
//     confirming that direct SubagentIDs entries also populate the probe set.

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
// Fixtures for deployed-state probe tests
// ---------------------------------------------------------------------------

// deployAgentsProbeStandaloneAgent is the standalone agent used in probe-set tests.
var deployAgentsProbeStandaloneAgent = domain.Agent{
	Key:             "session-logger",
	NumericID:       "40",
	Version:         "1.0",
	Name:            "Session Logger",
	Description:     "Logs session events for analysis",
	Role:            domain.RoleStandalone,
	RecommendedTier: "LOW",
}

// deployAgentsProbeSubagent is the ordinary subagent used in probe-set tests.
var deployAgentsProbeSubagent = domain.Agent{
	Key:             "plan-review",
	NumericID:       "50",
	Version:         "1.0",
	Name:            "Plan Review",
	Description:     "Reviews plans before execution",
	Role:            domain.RoleSubagent,
	Category:        "Review",
	RecommendedTier: "HIGH",
}

// catalogForDeployedStateTest returns a catalog with one standalone agent and one ordinary
// subagent, used to exercise the probe-set paths for both classes.
func catalogForDeployedStateTest() *stubCatalog {
	cat := newMinimalCatalog()
	cat.agents = []domain.Agent{deployAgentsProbeSubagent}
	cat.standaloneAgents = []domain.Agent{deployAgentsProbeStandaloneAgent}
	return cat
}

// ---------------------------------------------------------------------------
// T6.2 — Standalone agent file on disk is not ActionCreate on second run
// ---------------------------------------------------------------------------

// TestDeployAgents_RealPlanner_AlreadyDeployedStandaloneAgentNotActionCreate verifies that
// when a standalone agent's target file already exists in the workspace, running DeployAgents
// with the real planner does NOT classify that agent as ActionCreate. This confirms that the
// probe set built by DeployAgents includes standalone agents so their on-disk state is read
// before the planner computes each item's action.
//
// Without a correct probe set, the planner's DeployedState map would miss the standalone
// agent entry, causing every standalone agent to be classified as ActionCreate on every run
// after the first — the same defect the legacy standalone-only mode fixed in its own probe set.
func TestDeployAgents_RealPlanner_AlreadyDeployedStandaloneAgentNotActionCreate(t *testing.T) {
	// Arrange — real planner; plan-capturing executor records the computed plan.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogForDeployedStateTest()
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec
	svc := app.New(deps)

	// Simulate a previously-deployed standalone agent by writing a file at the target path.
	// The version stamp matches the source agent's Version ("1.0") so the planner produces
	// ActionUnchanged rather than ActionCreate. The stub harness module maps agent.Key → key+".md".
	agentContent := []byte("---\nversion: 1.0\n---\n# Session Logger\n")
	agentTargetPath := workspace + "/session-logger.md"
	if err := os.WriteFile(agentTargetPath, agentContent, 0o644); err != nil {
		t.Fatalf("setup: could not write pre-deployed agent file at %q: %v", agentTargetPath, err)
	}

	// Act — second run; the agent is already on disk so it must not be ActionCreate.
	// Pre-answer all selections and skip model questions so the flow runs headlessly.
	_, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:          "stub-harness",
		WorkspacePath:      workspace,
		Scope:              domain.ScopeProject,
		StandaloneAgentIDs: []string{"session-logger"},
		SubagentIDs:        []string{},
		UtilityAgentIDs:    []string{},
		InfrastructureAgentIDs: []string{},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployAgents returned error %v; "+
			"the flow must complete successfully when all inputs are pre-answered and "+
			"the standalone agent file already exists on disk", err)
	}

	// Assert — executor must have been called with a computed plan.
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; DeployAgents must invoke the executor with a computed plan; " +
			"the probe-set construction must include standalone agents so their deployed state is read " +
			"and the planner can classify them correctly on a second run")
	}

	// Assert — the standalone agent must NOT be ActionCreate on the second run.
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "session-logger" {
			if item.Action == domain.ActionCreate {
				t.Errorf("standalone agent \"session-logger\" was classified as ActionCreate "+
					"even though its file already exists at %q; "+
					"DeployAgents must build its probe set to include all selected agents "+
					"(including standalone-class agents) so that the planner detects their "+
					"on-disk state — without this, every run after the first reports all "+
					"standalone agents as new, repeating the defect the legacy standalone-only "+
					"mode fixed in its own probe set",
					agentTargetPath)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// T6.2 — Ordinary subagent file on disk is not ActionCreate on second run
// ---------------------------------------------------------------------------

// TestDeployAgents_RealPlanner_AlreadyDeployedSubagentNotActionCreate verifies that
// when an ordinary subagent's target file already exists in the workspace, running
// DeployAgents with the real planner does NOT classify that agent as ActionCreate. This
// confirms that the probe set built by DeployAgents also covers SubagentIDs entries.
func TestDeployAgents_RealPlanner_AlreadyDeployedSubagentNotActionCreate(t *testing.T) {
	// Arrange — real planner; plan-capturing executor.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = catalogForDeployedStateTest()
	capExec := &planCapturingExecutor{result: newMinimalExecResult(workspace)}
	deps.Planner = plan.New()
	deps.Executor = capExec
	svc := app.New(deps)

	// Simulate a previously-deployed ordinary subagent.
	agentContent := []byte("---\nversion: 1.0\n---\n# Plan Review\n")
	agentTargetPath := workspace + "/plan-review.md"
	if err := os.WriteFile(agentTargetPath, agentContent, 0o644); err != nil {
		t.Fatalf("setup: could not write pre-deployed agent file at %q: %v", agentTargetPath, err)
	}

	// Act — second run with the subagent pre-answered.
	_, err := svc.DeployAgents(context.Background(), app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		Scope:                  domain.ScopeProject,
		SubagentIDs:            []string{"plan-review"},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{},
		StandaloneAgentIDs:     []string{},
		SkipAll: map[domain.QuestionID]bool{
			domain.QTierModel:  true,
			domain.QAgentModel: true,
		},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployAgents returned error %v; "+
			"the flow must complete successfully when the subagent file already exists on disk", err)
	}

	// Assert — executor must have been called.
	if capExec.capturedPlan == nil {
		t.Fatal("executor was not called; DeployAgents must invoke the executor with a computed plan")
	}

	// Assert — the ordinary subagent must NOT be ActionCreate on the second run.
	for _, item := range capExec.capturedPlan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "plan-review" {
			if item.Action == domain.ActionCreate {
				t.Errorf("ordinary subagent \"plan-review\" was classified as ActionCreate "+
					"even though its file already exists at %q; "+
					"DeployAgents must include SubagentIDs entries in its probe set so that "+
					"on-disk state is detected before planning",
					agentTargetPath)
			}
		}
	}
}
