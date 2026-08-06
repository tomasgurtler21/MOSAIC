package app_test

// bundle_wiring_test.go verifies that the deploy-new, update, and workflow-update flows
// correctly wire bundle loading into the run lifecycle.
//
// T6.6 — Run-level wiring:
//
//   Load failure aborts the run before any file is written:
//     - When BundleLoader returns an error, DeployNew returns that error and does not call
//       the executor.
//     - When BundleLoader returns an error, Update returns that error and does not call the
//       executor.
//     - When BundleLoader returns an error, UpdateWorkflows returns that error and does not
//       call the executor.
//
//   Loaded bundle version reaches the planner:
//     - When BundleLoader returns a version string, plan.Input.BundleVersion carries that
//       string when the planner is called during DeployNew.
//     - When BundleLoader returns a version string, plan.Input.BundleVersion carries that
//       string when the planner is called during Update.
//     - When BundleLoader returns a version string, plan.Input.BundleVersion carries that
//       string when the planner is called during UpdateWorkflows.

import (
	"context"
	"errors"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Test doubles local to this file
// ---------------------------------------------------------------------------

// stubBundleLoader satisfies catalog.BundleLoader with a fixed return value.
type stubBundleLoader struct {
	content domain.BundleContent
	err     error
}

func (s *stubBundleLoader) LoadBundle(_ string) (domain.BundleContent, error) {
	return s.content, s.err
}

// Compile-time interface check.
var _ catalog.BundleLoader = (*stubBundleLoader)(nil)

// minimalBundleContent returns a BundleContent with all five subagent blocks and the given
// version string, suitable for use in wiring tests that do not exercise the bundle filling
// logic itself.
func minimalBundleContent(version string) domain.BundleContent {
	return domain.BundleContent{
		Version: version,
		Blocks: []domain.BundleBlock{
			{Name: "AuthorityHierarchy:Subagent", Target: "AuthorityHierarchy", AppliesTo: "subagent", Content: []byte("authority hierarchy content\n")},
			{Name: "ClosingProcedure:Subagent", Target: "ClosingProcedure", AppliesTo: "subagent", Content: []byte("closing procedure content\n")},
			{Name: "ProtocolConstraints:Subagent", Target: "ProtocolConstraints", AppliesTo: "subagent", Content: []byte("protocol constraints content\n")},
			{Name: "ErrorHandlingCommon:Subagent", Target: "ErrorHandlingCommon", AppliesTo: "subagent", Content: []byte("error handling common content\n")},
			{Name: "ExecutionPhilosophyCommon:Subagent", Target: "ExecutionPhilosophyCommon", AppliesTo: "subagent", Content: []byte("execution philosophy common content\n")},
		},
	}
}

// ---------------------------------------------------------------------------
// Bundle load failure — abort before write, all three flows
// ---------------------------------------------------------------------------

// TestDeployNew_BundleLoadError_AbortsBeforeExecutor verifies that when BundleLoader
// returns an error, DeployNew propagates it and does not invoke the executor. No file
// write must occur before bundle loading succeeds.
func TestDeployNew_BundleLoadError_AbortsBeforeExecutor(t *testing.T) {
	// Arrange
	loadErr := errors.New("bundle source missing: test sentinel")
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)

	spy := &protocolTestExecutorSpy{result: newMinimalExecResult(workspace)}
	deps.Executor = spy
	deps.BundleLoader = &stubBundleLoader{err: loadErr}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — the flow must surface the load error.
	if err == nil {
		t.Fatal("DeployNew returned nil error when BundleLoader failed; " +
			"a bundle load failure must abort the run and return an error")
	}
	// The returned error must wrap the original load error.
	if !errors.Is(err, loadErr) {
		t.Errorf("DeployNew error = %v; want an error wrapping the bundle loader's error %v; "+
			"swallowing the original error would hide the real failure from the caller", err, loadErr)
	}
	// The executor must not be called (no writes must occur).
	if spy.called {
		t.Error("executor was called during DeployNew despite BundleLoader failure; " +
			"the run must abort before any file is written when bundle loading fails")
	}
}

// TestUpdate_BundleLoadError_AbortsBeforeExecutor verifies that when BundleLoader
// returns an error, the update flow propagates it without calling the executor.
func TestUpdate_BundleLoadError_AbortsBeforeExecutor(t *testing.T) {
	// Arrange
	loadErr := errors.New("bundle source missing: test sentinel")
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)

	spy := &protocolTestExecutorSpy{result: newMinimalExecResult(workspace)}
	deps.Executor = spy
	deps.BundleLoader = &stubBundleLoader{err: loadErr}
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if err == nil {
		t.Fatal("Update returned nil error when BundleLoader failed; " +
			"a bundle load failure must abort the update run and return an error")
	}
	if !errors.Is(err, loadErr) {
		t.Errorf("Update error = %v; want an error wrapping the bundle loader's error %v",
			err, loadErr)
	}
	if spy.called {
		t.Error("executor was called during Update despite BundleLoader failure; " +
			"no file must be written before bundle loading succeeds")
	}
}

// TestUpdateWorkflows_BundleLoadError_AbortsBeforeExecutor verifies that when BundleLoader
// returns an error, the workflow-update flow propagates it without calling the executor.
func TestUpdateWorkflows_BundleLoadError_AbortsBeforeExecutor(t *testing.T) {
	// Arrange
	loadErr := errors.New("bundle source missing: test sentinel")
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)

	spy := &protocolTestExecutorSpy{result: newMinimalExecResult(workspace)}
	deps.Executor = spy
	deps.BundleLoader = &stubBundleLoader{err: loadErr}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err == nil {
		t.Fatal("UpdateWorkflows returned nil error when BundleLoader failed; " +
			"a bundle load failure must abort the workflow-update run and return an error")
	}
	if !errors.Is(err, loadErr) {
		t.Errorf("UpdateWorkflows error = %v; want an error wrapping the bundle loader's error %v",
			err, loadErr)
	}
	if spy.called {
		t.Error("executor was called during UpdateWorkflows despite BundleLoader failure; " +
			"no file must be written before bundle loading succeeds")
	}
}

// ---------------------------------------------------------------------------
// Loaded bundle version reaches plan.Input — all three flows
// ---------------------------------------------------------------------------

// TestDeployNew_BundleVersion_ReachesPlannerInput verifies that when BundleLoader returns
// a version string, that version is propagated to plan.Input.BundleVersion before the
// planner is called. The planner uses this field to detect bundle staleness.
func TestDeployNew_BundleVersion_ReachesPlannerInput(t *testing.T) {
	// Arrange
	const wantVersion = "1.0.0"
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)

	plannerSpy := &spyPlanner{response: newMinimalPlan(workspace)}
	deps.Planner = plannerSpy
	deps.BundleLoader = &stubBundleLoader{content: minimalBundleContent(wantVersion)}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — the planner must have received the bundle version from the loader.
	captured := plannerSpy.lastCaptured(t)
	if captured.BundleVersion != wantVersion {
		t.Errorf("plan.Input.BundleVersion = %q during DeployNew, want %q; "+
			"the bundle source version must reach the planner for staleness detection",
			captured.BundleVersion, wantVersion)
	}
}

// TestUpdate_BundleVersion_ReachesPlannerInput verifies that the loaded bundle version
// reaches plan.Input.BundleVersion during the update flow.
func TestUpdate_BundleVersion_ReachesPlannerInput(t *testing.T) {
	// Arrange
	const wantVersion = "1.0.0"
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)

	updatePlan := domain.Plan{
		Mode:          domain.ModeUpdate,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
		Workflows:     []string{"quick-fix"},
	}
	plannerSpy := &spyPlanner{response: updatePlan}
	deps.Planner = plannerSpy
	deps.BundleLoader = &stubBundleLoader{content: minimalBundleContent(wantVersion)}
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	captured := plannerSpy.lastCaptured(t)
	if captured.BundleVersion != wantVersion {
		t.Errorf("plan.Input.BundleVersion = %q during Update, want %q; "+
			"the bundle version must reach the planner in the update flow",
			captured.BundleVersion, wantVersion)
	}
}

// TestUpdateWorkflows_BundleVersion_ReachesPlannerInput verifies that the loaded bundle
// version reaches plan.Input.BundleVersion during the workflow-update flow.
func TestUpdateWorkflows_BundleVersion_ReachesPlannerInput(t *testing.T) {
	// Arrange
	const wantVersion = "1.0.0"
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)

	wfPlan := domain.Plan{
		Mode:          domain.ModeWorkflowsOnly,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
		Workflows:     []string{"quick-fix"},
	}
	plannerSpy := &spyPlanner{response: wfPlan}
	deps.Planner = plannerSpy
	deps.BundleLoader = &stubBundleLoader{content: minimalBundleContent(wantVersion)}
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	captured := plannerSpy.lastCaptured(t)
	if captured.BundleVersion != wantVersion {
		t.Errorf("plan.Input.BundleVersion = %q during UpdateWorkflows, want %q; "+
			"the bundle version must reach the planner in all three flows",
			captured.BundleVersion, wantVersion)
	}
}
