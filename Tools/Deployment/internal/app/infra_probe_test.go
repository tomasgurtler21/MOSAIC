//go:build stage3

package app_test

// infra_probe_test.go verifies that infrastructure agents present on disk are reflected in
// the probe set passed to the planner, and that the wider probe set does not cause duplicate
// model resolution for infrastructure agents.
//
// T3.1 — Infrastructure agent presence in both deploy flows:
//
//   The defect: plan.ResolveArtifacts is called with nil for infrastructureAgentIDs at both
//   call sites (deploy.go ~line 103 and utility_infra_service.go ~line 88), so the probe set
//   never contains infrastructure agents. plan.EnumerateTargetPaths is never given their target
//   paths, so probeDeployedStateWithIndex never probes them. plan.Input.DeployedState lacks
//   entries for these paths, so the planner's map lookup yields a zero value (Present: false),
//   and classifyAgentItem short-circuits to ActionCreate on every run after the first.
//
//   After the fix (I3.1, I3.2), the probe set includes infrastructure agents, so:
//     - Their target paths are enumerated
//     - probeDeployedStateWithIndex reads their on-disk state
//     - plan.Input.DeployedState contains their entries with Present: true when the files exist
//
//   These tests assert the post-fix state. They use capturingPlanner to inspect plan.Input
//   directly, bypassing the stub planner so the actual probe pipeline runs.
//
// T3.4 — Regression: no duplicate model prompts, no re-prompting of infrastructure selection:
//
//   Including infrastructure agents in the probe set means probeSet.Agents may now contain
//   infrastructure agents. If the first resolveModels batch (which iterates orderedAgents
//   derived from probeSet.Agents) includes infrastructure agents without filtering, the same
//   agent would be asked for a model in both the first batch (via probeSet) and the second
//   batch (via the explicit infraAgents loop). These tests assert that each infrastructure
//   agent's model is asked at most once and that QInfrastructureAgents is never re-asked when
//   the IDs are pre-supplied.
//
// These tests will fail (DeployedState not containing infra agent entries) until I3.1 and I3.2
// are implemented, which is the intended TDD RED state.

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
// T3.1 — Deploy-new flow: infrastructure agent present on disk → DeployedState entry
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgentOnDisk_DeployedStateShowsPresent verifies that when a previously
// deployed infrastructure agent file exists on disk and the agent is selected for the run,
// the planner receives a DeployedState entry for that agent's target path with Present: true.
//
// Before the fix, the infrastructure agents are excluded from the probe set so their target
// paths are never probed; the planner receives no entry (or a zero-value entry with
// Present: false), causing the planner to classify the agent as ActionCreate on every run.
func TestDeployNew_InfraAgentOnDisk_DeployedStateShowsPresent(t *testing.T) {
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
	req := app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployNew(context.Background(), req)

	// Assert
	if cp.capturedInput == nil {
		t.Fatal("planner Build was not called; service may have returned early due to a different error")
	}

	entry, ok := cp.capturedInput.DeployedState[infraTargetPath]
	if !ok {
		t.Errorf("DeployedState does not contain an entry for infrastructure agent target path %q; "+
			"infrastructure agents must be included in the probe set so their on-disk state is probed",
			infraTargetPath)
		return
	}

	if !entry.Present {
		t.Errorf("DeployedState[%q].Present = false, want true; "+
			"the infrastructure agent file exists on disk and must be detected as present",
			infraTargetPath)
	}
}

// TestDeployNew_InfraAgentAbsentFromDisk_DeployedStateShowsAbsent verifies that when no
// infrastructure agent file exists on disk (first deployment), the DeployedState entry for
// that path has Present: false. This is the baseline case that must remain correct after the fix.
func TestDeployNew_InfraAgentAbsentFromDisk_DeployedStateShowsAbsent(t *testing.T) {
	// Arrange — workspace has no deployed infra agent file.
	workspace := t.TempDir()

	cat := catalogWithInfraAgentForProbe()
	cp := capturingPlannerWithPlan(newMinimalPlan(workspace))
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()

	deps, _ := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Planner = cp

	svc := app.New(deps)
	req := app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployNew(context.Background(), req)

	// Assert
	if cp.capturedInput == nil {
		t.Fatal("planner Build was not called")
	}

	infraTargetPath := infraAgentForProbeTest.Key + ".md"
	entry := cp.capturedInput.DeployedState[infraTargetPath]
	if entry.Present {
		t.Errorf("DeployedState[%q].Present = true for an absent file; "+
			"an infrastructure agent not yet deployed must appear absent in DeployedState",
			infraTargetPath)
	}
}

// TestDeployUtilityInfrastructure_InfraAgentAbsentFromDisk_DeployedStateShowsAbsent verifies
// the symmetric baseline for the utility/infrastructure flow: when no infrastructure agent file
// exists on disk, the DeployedState entry for that path has Present: false. This baseline must
// remain correct after the probe-set inclusion fix (I3.2).
func TestDeployUtilityInfrastructure_InfraAgentAbsentFromDisk_DeployedStateShowsAbsent(t *testing.T) {
	// Arrange — workspace has no deployed infra agent file.
	workspace := t.TempDir()

	cat := catalogWithInfraAgentForProbe()
	utilityInfraResult := domain.Plan{
		Mode:          domain.ModeUtilityInfraOnly,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
	}
	cp := capturingPlannerWithPlan(utilityInfraResult)
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()

	deps, _ := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Planner = cp

	svc := app.New(deps)
	req := app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key},
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), req)

	// Assert
	if cp.capturedInput == nil {
		t.Fatal("planner Build was not called")
	}

	infraTargetPath := infraAgentForProbeTest.Key + ".md"
	entry := cp.capturedInput.DeployedState[infraTargetPath]
	if entry.Present {
		t.Errorf("DeployedState[%q].Present = true for an absent file in utility/infrastructure flow; "+
			"an infrastructure agent not yet deployed must appear absent in DeployedState",
			infraTargetPath)
	}
}

// ---------------------------------------------------------------------------
// T3.1 — Utility/infrastructure flow: infrastructure agent present on disk → DeployedState
// ---------------------------------------------------------------------------

// TestDeployUtilityInfrastructure_InfraAgentOnDisk_DeployedStateShowsPresent verifies the same
// probe-set inclusion for the utility/infrastructure-only flow. Both flow functions have the
// same defect (nil passed for infrastructureAgentIDs in plan.ResolveArtifacts) and both need
// the same fix.
func TestDeployUtilityInfrastructure_InfraAgentOnDisk_DeployedStateShowsPresent(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	infraTargetPath := writeInfraAgentFile(t, workspace, infraAgentForProbeTest.Key)

	cat := catalogWithInfraAgentForProbe()
	utilityInfraResult := domain.Plan{
		Mode:          domain.ModeUtilityInfraOnly,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
	}
	cp := capturingPlannerWithPlan(utilityInfraResult)
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()

	deps, _ := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Planner = cp

	svc := app.New(deps)
	req := app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key},
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), req)

	// Assert
	if cp.capturedInput == nil {
		t.Fatal("planner Build was not called; service may have returned early due to a different error")
	}

	entry, ok := cp.capturedInput.DeployedState[infraTargetPath]
	if !ok {
		t.Errorf("DeployedState does not contain an entry for infrastructure agent target path %q "+
			"in the utility/infrastructure flow; the same probe-set inclusion fix must apply to both flows",
			infraTargetPath)
		return
	}

	if !entry.Present {
		t.Errorf("DeployedState[%q].Present = false in utility/infrastructure flow, want true; "+
			"the infrastructure agent file is on disk and must be probed correctly",
			infraTargetPath)
	}
}

// ---------------------------------------------------------------------------
// T3.4 — Regression: infrastructure selection not re-prompted
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgentIDsPreAnswered_QInfrastructureAgentsNeverAsked verifies that when
// InfrastructureAgentIDs is pre-answered in the request (non-nil), the flow never asks
// QInfrastructureAgents through the Interaction port. Including infrastructure agents in the
// probe set must not cause the question to be re-asked.
func TestDeployNew_InfraAgentIDsPreAnswered_QInfrastructureAgentsNeverAsked(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	writeInfraAgentFile(t, workspace, infraAgentForProbeTest.Key)

	cat := catalogWithInfraAgentForProbe()
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()

	deps, _ := newBaseDeps(t, stub)
	deps.Catalog = cat

	svc := app.New(deps)
	req := app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key}, // pre-answered
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployNew(context.Background(), req)

	// Assert
	if stub.WasAsked(domain.QInfrastructureAgents, "") {
		t.Error("QInfrastructureAgents was asked even though InfrastructureAgentIDs was pre-supplied; " +
			"widening the probe set must not cause infrastructure selection to be re-prompted")
	}
}

// TestDeployNew_InfraAgentModelResolvedExactlyOnce verifies that when infrastructure agents are
// included in the probe set, the model question for each infrastructure agent is asked at most
// once. Before the fix, infra agents were not in probeSet, so orderedAgents (derived from
// probeSet.Agents) did not include them and they were only asked in the second batch.
//
// After a correct fix, infrastructure agents are in the probe set but must be filtered out of
// the first-batch model resolution (orderedAgents) to prevent a duplicate prompt in both the
// first batch (via probeSet.Agents) and the second batch (via the explicit infraAgents loop).
func TestDeployNew_InfraAgentModelResolvedExactlyOnce(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	writeInfraAgentFile(t, workspace, infraAgentForProbeTest.Key)

	cat := catalogWithInfraAgentForProbe()
	stub := interactiontest.NewBuilder().
		// Pre-answer the infra agent model question with a specific model.
		AnswerSelectOne(domain.QAgentModel, infraAgentForProbeTest.Key, "model-a").
		AnswerReview(true).
		Build()

	deps, _ := newBaseDeps(t, stub)
	deps.Catalog = cat

	svc := app.New(deps)
	req := app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployNew(context.Background(), req)

	// Assert: QAgentModel for the infra agent was asked at most once.
	count := stub.CountAsked(domain.QAgentModel, infraAgentForProbeTest.Key)
	if count > 1 {
		t.Errorf("QAgentModel for infrastructure agent %q was asked %d times, want at most 1; "+
			"widening the probe set must not cause infrastructure agents to appear in both "+
			"the first-batch and second-batch model resolution",
			infraAgentForProbeTest.Key, count)
	}
}

// TestDeployUtilityInfrastructure_InfraAgentModelResolvedExactlyOnce verifies the same
// no-duplicate-model-resolution invariant for the utility/infrastructure-only flow.
func TestDeployUtilityInfrastructure_InfraAgentModelResolvedExactlyOnce(t *testing.T) {
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
	req := app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key},
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), req)

	// Assert
	count := stub.CountAsked(domain.QAgentModel, infraAgentForProbeTest.Key)
	if count > 1 {
		t.Errorf("QAgentModel for infrastructure agent %q was asked %d times in "+
			"DeployUtilityInfrastructure, want at most 1; the same duplicate-model-resolution "+
			"protection must apply to the utility/infrastructure flow",
			infraAgentForProbeTest.Key, count)
	}
}

// TestDeployUtilityInfrastructure_InfraAgentIDsPreAnswered_QInfrastructureAgentsNeverAsked
// verifies the selection non-re-prompting invariant for the utility/infrastructure flow.
func TestDeployUtilityInfrastructure_InfraAgentIDsPreAnswered_QInfrastructureAgentsNeverAsked(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	writeInfraAgentFile(t, workspace, infraAgentForProbeTest.Key)

	cat := catalogWithInfraAgentForProbe()
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()

	deps, _ := newBaseDeps(t, stub)
	deps.Catalog = cat

	svc := app.New(deps)
	req := app.UtilityInfraRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key}, // pre-answered
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployUtilityInfrastructure(context.Background(), req)

	// Assert
	if stub.WasAsked(domain.QInfrastructureAgents, "") {
		t.Error("QInfrastructureAgents was asked even though InfrastructureAgentIDs was pre-supplied " +
			"in the utility/infrastructure flow; widening the probe set must not cause re-prompting")
	}
}

// ---------------------------------------------------------------------------
// T3.1 / T3.4 — plan.Input.InfrastructureAgentIDs is wired through correctly
// ---------------------------------------------------------------------------

// TestDeployNew_InfraAgentSelected_PlanInputContainsInfraIDs verifies that when infrastructure
// agents are pre-selected, the plan.Input.InfrastructureAgentIDs slice passed to Build contains
// the same IDs. This is a prerequisite for the planner's own classification to work correctly.
func TestDeployNew_InfraAgentSelected_PlanInputContainsInfraIDs(t *testing.T) {
	// Arrange
	workspace := t.TempDir()
	cat := catalogWithInfraAgentForProbe()
	cp := capturingPlannerWithPlan(newMinimalPlan(workspace))
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()

	deps, _ := newBaseDeps(t, stub)
	deps.Catalog = cat
	deps.Planner = cp

	svc := app.New(deps)
	req := app.DeployRequest{
		HarnessID:              "stub-harness",
		WorkspacePath:          workspace,
		WorkflowIDs:            []string{"quick-fix"},
		UtilityAgentIDs:        []string{},
		InfrastructureAgentIDs: []string{infraAgentForProbeTest.Key},
		HookIDs:                []string{},
		AutoConfirmPlan:        true,
	}

	// Act
	_, _ = svc.DeployNew(context.Background(), req)

	// Assert
	if cp.capturedInput == nil {
		t.Fatal("planner Build was not called")
	}

	found := false
	for _, id := range cp.capturedInput.InfrastructureAgentIDs {
		if id == infraAgentForProbeTest.Key {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("plan.Input.InfrastructureAgentIDs = %v, does not contain %q; "+
			"the selected infrastructure agent IDs must be forwarded to the planner",
			cp.capturedInput.InfrastructureAgentIDs, infraAgentForProbeTest.Key)
	}
}

