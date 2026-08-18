package app_test

// failure_test.go verifies flow-level failure handling: unwritable workspace with fallback,
// a workflow referencing a missing agent, and a corrupt manifest.
//
// Verified invariants (T18.8):
//
// Unwritable workspace:
//   - When the primary workspace is unwritable, DeployNew falls back to an alternate location
//   - The RunSummary.Fallback field is set to a non-workspace tier
//   - The DeploymentRoot in the summary reflects the actual location used
//   - DeployNew returns no error when a fallback is found (the run completed with gaps)
//
// Workflow referencing a missing agent:
//   - When a workflow's ReferencedAgents list includes an agent key not in the catalog,
//     the planner returns an error (ErrUnknownAgent)
//   - DeployNew returns an error in this case
//
// Corrupt manifest:
//   - A corrupt manifest (StateCorrupt) causes conflict-classification for any existing file,
//     which then requires user resolution through QLocalModification
//   - The flow proceeds and does not panic or return an error simply because the manifest is corrupt

import (
	"context"
	"errors"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
	"mosaic-deploy/internal/manifest"
	"mosaic-deploy/internal/plan"
)

// ---------------------------------------------------------------------------
// Unwritable workspace — fallback (T18.8)
// ---------------------------------------------------------------------------

// TestDeployNew_UnwritableWorkspace_SummaryShowsFallback verifies that when the executor
// falls back from the primary workspace to an alternate location, RunSummary.Fallback is
// set to a non-workspace tier and the run still succeeds.
func TestDeployNew_UnwritableWorkspace_SummaryShowsFallback(t *testing.T) {
	// Arrange — executor returns a FallbackMosaicRoot result
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: deps.MosaicRoot + "/MosaicDeploy/fallback/workspace-slug",
		Fallback:       domain.FallbackMosaicRoot,
		Actions:        []domain.ActionRecord{},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err != nil {
		t.Fatalf("DeployNew returned error on fallback: %v; a successful fallback must not return an error", err)
	}
	if summary.Fallback == domain.FallbackNone {
		t.Error("summary.Fallback = FallbackNone; a fallback deployment must set a non-workspace fallback tier in the summary")
	}
	if summary.DeploymentRoot == workspace {
		t.Errorf("summary.DeploymentRoot = %q (the primary workspace); " +
			"a fallback must use a different deployment root and report it in the summary", workspace)
	}
}

// TestDeployNew_AllFallbacksExhausted_ReturnsError verifies that when the executor
// exhausts all fallback locations (ErrNoWritableLocation), DeployNew returns an error.
func TestDeployNew_AllFallbacksExhausted_ReturnsError(t *testing.T) {
	// Arrange — executor simulates no writable location found
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Executor = &stubExecutor{err: deploy.ErrNoWritableLocation}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err == nil {
		t.Error("DeployNew returned nil error when all fallback locations were exhausted; " +
			"ErrNoWritableLocation must propagate as a run error")
	}
	if !errors.Is(err, deploy.ErrNoWritableLocation) {
		t.Errorf("error = %v; want an error wrapping deploy.ErrNoWritableLocation", err)
	}
}

// ---------------------------------------------------------------------------
// Workflow referencing a missing agent (T18.8)
// ---------------------------------------------------------------------------

// TestDeployNew_WorkflowReferencesMissingAgent_PlannerReturnsError verifies that when a
// workflow references an agent key that is not in the catalog, the planner returns an error
// and DeployNew propagates it.
func TestDeployNew_WorkflowReferencesMissingAgent_PlannerReturnsError(t *testing.T) {
	// Arrange — planner returns ErrUnknownAgent
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{err: plan.ErrUnknownAgent}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err == nil {
		t.Error("DeployNew returned nil error when a workflow references a missing agent; " +
			"ErrUnknownAgent from the planner must surface as a run error")
	}
	if !errors.Is(err, plan.ErrUnknownAgent) {
		t.Errorf("error = %v; want an error wrapping plan.ErrUnknownAgent", err)
	}
}

// TestDeployNew_WorkflowNotInCatalog_PlannerReturnsError verifies that when a workflow ID
// is not found in the catalog, the planner returns ErrUnknownWorkflow.
func TestDeployNew_WorkflowNotInCatalog_PlannerReturnsError(t *testing.T) {
	// Arrange — planner returns ErrUnknownWorkflow
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{err: plan.ErrUnknownWorkflow}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"nonexistent-workflow"}))

	// Assert
	if err == nil {
		t.Error("DeployNew returned nil error for a workflow that does not exist in the catalog; " +
			"ErrUnknownWorkflow must surface as a run error")
	}
}

// ---------------------------------------------------------------------------
// Corrupt manifest (T18.8)
// ---------------------------------------------------------------------------

// TestUpdate_CorruptManifest_ClassifiesExistingFilesAsConflict verifies that when the
// manifest is corrupt, the update flow uses the planner's conservative classification
// (ActionConflict with ManifestMissing: true) and asks for user input per conflict.
func TestUpdate_CorruptManifest_ClassifiesExistingFilesAsConflict(t *testing.T) {
	// Arrange — manifest is corrupt; planner returns a conflict item (ManifestMissing=true)
	conflictItem := domain.PlanItem{
		Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
		TargetPath: "test-runner.md",
		Action:     domain.ActionConflict,
		Conflict: &domain.LocalModification{
			RecordedHash:    "",
			CurrentHash:     "sha256:actual",
			ManifestMissing: true,
		},
	}
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "test-runner.md", string(domain.DecisionSkip)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Manifest = &stubManifestStore{
		snap: manifest.Snapshot{
			State: manifest.StateCorrupt,
			Err:   errors.New("yaml: cannot unmarshal !! str value"),
		},
	}
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{conflictItem},
		Workflows:     []string{"quick-fix"},
	}}
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — corrupt manifest must not abort the flow; user should be prompted per file
	if err != nil {
		t.Errorf("Update returned error for a corrupt manifest: %v; "+
			"a corrupt manifest is a recoverable state — the planner classifies files and the flow prompts the user", err)
	}
	if !stub.WasAsked(domain.QLocalModification, "test-runner.md") {
		t.Error("QLocalModification was not asked despite corrupt manifest; " +
			"a corrupt manifest causes conservative conflict classification (AC16.4) which must trigger prompting")
	}
}

// TestUpdate_CorruptManifest_DoesNotPanic verifies that a corrupt manifest snapshot does
// not cause a panic anywhere in the update flow.
func TestUpdate_CorruptManifest_DoesNotPanic(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Manifest = &stubManifestStore{
		snap: manifest.Snapshot{
			State: manifest.StateCorrupt,
			Err:   errors.New("corrupt"),
		},
	}
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
		Workflows:     []string{"quick-fix"},
	}}
	svc := app.New(deps)

	// Act — must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Update panicked on corrupt manifest: %v; the flow must handle a corrupt manifest gracefully", r)
		}
	}()
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})
}

// TestDeployNew_AbsentManifest_RunsWithoutError verifies that an absent manifest (first
// deploy) does not return an error. The StateAbsent case is normal first-run state.
func TestDeployNew_AbsentManifest_RunsWithoutError(t *testing.T) {
	// Arrange — manifest is absent (StateAbsent)
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Manifest = &stubManifestStore{
		snap: manifest.Snapshot{State: manifest.StateAbsent},
	}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err != nil {
		t.Errorf("DeployNew returned error for absent manifest: %v; "+
			"an absent manifest is normal first-run state and must not produce an error", err)
	}
}
