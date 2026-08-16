package app_test

// deployagents_infra_probe_test.go verifies that infrastructure agents present on disk are
// reflected in the probe set passed to the planner when the deploy-agents flow is used, and
// that including them in the probe set does not cause duplicate model resolution.
//
// These are the deploy-agents counterparts of the deploy-new infrastructure probe tests in
// infra_probe_test.go (build tag: stage3). The deploy-agents variants carry no build tag so
// they execute in every standard `go test ./...` run and satisfy AC6.6's coverage requirement.
//
// Shared test fixtures (infraAgentForProbeTest, catalogWithInfraAgentForProbe,
// writeInfraAgentFile, capturingPlannerWithPlan) are defined here so that both this file and
// infra_probe_test.go (stage3) can reference them. When building with -tags stage3 both files
// compile in the same package, so the fixtures remain accessible to the deploy-new tests.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Shared fixtures
// ---------------------------------------------------------------------------

// infraAgentForProbeTest is a minimal infrastructure agent used to verify probe-set inclusion.
var infraAgentForProbeTest = domain.Agent{
	Key:             "security-scanner",
	NumericID:       "42",
	Version:         "1.0",
	Name:            "Security Scanner",
	Description:     "Scans for vulnerabilities",
	Role:            domain.RoleWorker,
	Infrastructure:  "security",
	RecommendedTier: "HIGH",
}

// catalogWithInfraAgentForProbe returns a catalog with one infrastructure agent in addition
// to the standard minimal catalog contents.
func catalogWithInfraAgentForProbe() *stubCatalog {
	cat := newMinimalCatalog()
	cat.infraAgents = []domain.Agent{infraAgentForProbeTest}
	return cat
}

// writeInfraAgentFile writes a minimal deployed infrastructure agent file to the workspace and
// returns the file path relative to the workspace root (the target path).
// The file content includes a version stamp so the planner can compare versions.
func writeInfraAgentFile(t *testing.T, workspace string, agentKey string) string {
	t.Helper()
	// The stubHarnessModule.TargetPath returns req.Key + ".md".
	relPath := agentKey + ".md"
	content := []byte("---\nversion: \"1.0\"\n---\n\nInfrastructure agent deployed file.\n")
	if err := os.WriteFile(filepath.Join(workspace, relPath), content, 0o644); err != nil {
		t.Fatalf("writeInfraAgentFile: %v", err)
	}
	return relPath
}

// capturingPlannerWithPlan wraps capturingPlanner with a fixed plan result. It is used in
// tests that need to inspect plan.Input without caring about the plan itself.
func capturingPlannerWithPlan(result domain.Plan) *capturingPlanner {
	return &capturingPlanner{result: result}
}

// ---------------------------------------------------------------------------
// Deploy-agents flow: infrastructure agent present on disk → DeployedState
// ---------------------------------------------------------------------------

// TestDeployAgents_InfraAgentAbsentFromDisk_DeployedStateShowsAbsent verifies the baseline
// case for the deploy-agents flow: when no infrastructure agent file exists on disk, the
// DeployedState entry for that agent's target path has Present: false. This confirms the probe
// path is plumbed through correctly even when there is nothing on disk to find.
func TestDeployAgents_InfraAgentAbsentFromDisk_DeployedStateShowsAbsent(t *testing.T) {
	// Arrange — workspace has no deployed infra agent file (first run).
	workspace := t.TempDir()

	cat := catalogWithInfraAgentForProbe()
	cp := capturingPlannerWithPlan(newMinimalPlan(workspace))
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()

	deps, _ := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Planner = cp

	svc := app.New(deps)
	req := app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		Scope:                  domain.ScopeProject,
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key},
		StandaloneAgentIDs:     []string{},
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployAgents(context.Background(), req)

	// Assert
	if cp.capturedInput == nil {
		t.Fatal("planner Build was not called")
	}

	infraTargetPath := infraAgentForProbeTest.Key + ".md"
	entry := cp.capturedInput.DeployedState[infraTargetPath]
	if entry.Present {
		t.Errorf("DeployedState[%q].Present = true for an absent file in deploy-agents flow; "+
			"an infrastructure agent not yet deployed must appear absent in DeployedState",
			infraTargetPath)
	}
}

// TestDeployAgents_InfraAgentOnDisk_DeployedStateShowsPresent verifies that when a previously
// deployed infrastructure agent file exists on disk and the agent is selected via
// InfrastructureAgentIDs, the planner receives a DeployedState entry for that path with
// Present: true. Without a correct probe set, the planner classifies the agent as ActionCreate
// on every run after the first.
func TestDeployAgents_InfraAgentOnDisk_DeployedStateShowsPresent(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	infraTargetPath := writeInfraAgentFile(t, workspace, infraAgentForProbeTest.Key)

	cat := catalogWithInfraAgentForProbe()
	cp := capturingPlannerWithPlan(newMinimalPlan(workspace))
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()

	deps, _ := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Planner = cp

	svc := app.New(deps)
	req := app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		Scope:                  domain.ScopeProject,
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key},
		StandaloneAgentIDs:     []string{},
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployAgents(context.Background(), req)

	// Assert
	if cp.capturedInput == nil {
		t.Fatal("planner Build was not called; service may have returned early due to a different error")
	}

	entry, ok := cp.capturedInput.DeployedState[infraTargetPath]
	if !ok {
		t.Errorf("DeployedState does not contain an entry for infrastructure agent target path %q "+
			"in the deploy-agents flow; infrastructure agents must be included in the probe set so "+
			"their on-disk state is probed before planning",
			infraTargetPath)
		return
	}

	if !entry.Present {
		t.Errorf("DeployedState[%q].Present = false in deploy-agents flow, want true; "+
			"the infrastructure agent file is on disk and must be detected as present",
			infraTargetPath)
	}
}

// TestDeployAgents_InfraAgentModelResolvedExactlyOnce verifies the no-duplicate-model-resolution
// invariant for the deploy-agents flow. Including infrastructure agents in the probe set must
// not cause the model question for each infrastructure agent to be asked twice (once via
// orderedAgents from probeSet and once via the explicit infraAgents loop).
func TestDeployAgents_InfraAgentModelResolvedExactlyOnce(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	writeInfraAgentFile(t, workspace, infraAgentForProbeTest.Key)

	cat := catalogWithInfraAgentForProbe()
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QAgentModel, infraAgentForProbeTest.Key, "model-a").
		AnswerReview(true).
		Build()

	deps, _ := newBaseDeps(t, stub)
	deps.Catalog = cat

	svc := app.New(deps)
	req := app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		Scope:                  domain.ScopeProject,
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key},
		StandaloneAgentIDs:     []string{},
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployAgents(context.Background(), req)

	// Assert
	count := stub.CountAsked(domain.QAgentModel, infraAgentForProbeTest.Key)
	if count > 1 {
		t.Errorf("QAgentModel for infrastructure agent %q was asked %d times in "+
			"DeployAgents, want at most 1; the deploy-agents flow must filter infrastructure "+
			"agents from the first-batch model resolution to prevent duplicate prompts",
			infraAgentForProbeTest.Key, count)
	}
}

// TestDeployAgents_InfraAgentIDsPreAnswered_QInfrastructureAgentsNeverAsked verifies the
// selection non-re-prompting invariant for the deploy-agents flow. When InfrastructureAgentIDs
// is pre-answered in the request, the flow must never ask QInfrastructureAgents through the
// Interaction port. Including infrastructure agents in the probe set must not cause the
// question to be re-asked.
func TestDeployAgents_InfraAgentIDsPreAnswered_QInfrastructureAgentsNeverAsked(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	writeInfraAgentFile(t, workspace, infraAgentForProbeTest.Key)

	cat := catalogWithInfraAgentForProbe()
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()

	deps, _ := newBaseDeps(t, stub)
	deps.Catalog = cat

	svc := app.New(deps)
	req := app.DeployAgentsRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		Scope:                  domain.ScopeProject,
		SubagentIDs:            []string{},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key}, // pre-answered
		StandaloneAgentIDs:     []string{},
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployAgents(context.Background(), req)

	// Assert
	if stub.WasAsked(domain.QInfrastructureAgents, "") {
		t.Error("QInfrastructureAgents was asked even though InfrastructureAgentIDs was pre-supplied " +
			"in the deploy-agents flow; widening the probe set must not cause re-prompting of agent selection")
	}
}
