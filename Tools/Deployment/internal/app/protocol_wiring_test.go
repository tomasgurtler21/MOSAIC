package app_test

// protocol_wiring_test.go verifies that the deploy-new, update, and workflow-update flows
// correctly wire protocol source loading into the run lifecycle.
//
// T5.3 — Run-level wiring:
//
//   Load failure aborts the run before any file is written:
//     - When ProtocolLoader returns an error, DeployNew returns that error and does not call
//       the executor.
//     - When ProtocolLoader returns an error, Update returns that error and does not call the
//       executor.
//     - When ProtocolLoader returns an error, UpdateWorkflows returns that error and does not
//       call the executor.
//
//   Loaded protocol version reaches the planner:
//     - When ProtocolLoader returns a version string, plan.Input.ProtocolVersion carries that
//       string when the planner is called during DeployNew.
//     - When ProtocolLoader returns a version string, plan.Input.ProtocolVersion carries that
//       string when the planner is called during Update.
//     - When ProtocolLoader returns a version string, plan.Input.ProtocolVersion carries that
//       string when the planner is called during UpdateWorkflows.

import (
	"context"
	"errors"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/catalog"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// Test doubles local to this file
// ---------------------------------------------------------------------------

// stubProtocolLoader satisfies catalog.ProtocolLoader with a fixed return value.
type stubProtocolLoader struct {
	content domain.ProtocolContent
	err     error
}

func (s *stubProtocolLoader) LoadProtocol(_ string) (domain.ProtocolContent, error) {
	return s.content, s.err
}

// Compile-time interface check.
var _ catalog.ProtocolLoader = (*stubProtocolLoader)(nil)

// protocolTestExecutorSpy satisfies deploy.Executor, records whether Execute was called,
// and returns a pre-configured result without touching the filesystem.
type protocolTestExecutorSpy struct {
	result deploy.ExecResult
	called bool
}

func (s *protocolTestExecutorSpy) Execute(_ context.Context, _ deploy.ExecRequest) (deploy.ExecResult, error) {
	s.called = true
	return s.result, nil
}

// Compile-time interface check.
var _ deploy.Executor = (*protocolTestExecutorSpy)(nil)

// minimalProtocolContent returns a ProtocolContent with both blocks populated and the given
// version string. Used to supply a successful loader result in wiring tests.
func minimalProtocolContent(version string) domain.ProtocolContent {
	return domain.ProtocolContent{
		Version: version,
		Blocks: map[domain.ProtocolVariant][]byte{
			domain.ProtocolSubagent:     []byte("## Communication Protocol\n\nSubagent protocol text.\n"),
			domain.ProtocolOrchestrator: []byte("## Communication Protocol\n\nOrchestrator protocol text.\n"),
		},
	}
}

// ---------------------------------------------------------------------------
// Protocol load failure — abort before write, all three flows
// ---------------------------------------------------------------------------

// TestDeployNew_ProtocolLoadError_AbortsBeforeExecutor verifies that when ProtocolLoader
// returns an error, DeployNew propagates it and does not invoke the executor. No file
// write must occur before protocol loading succeeds.
func TestDeployNew_ProtocolLoadError_AbortsBeforeExecutor(t *testing.T) {
	// Arrange
	loadErr := errors.New("protocol source missing: test sentinel")
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)

	spy := &protocolTestExecutorSpy{result: newMinimalExecResult(workspace)}
	deps.Executor = spy
	deps.ProtocolLoader = &stubProtocolLoader{err: loadErr}
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — the flow must surface the load error.
	if err == nil {
		t.Fatal("DeployNew returned nil error when ProtocolLoader failed; " +
			"a protocol load failure must abort the run and return an error")
	}
	// Assert — the returned error must wrap the original load error, not an unrelated error.
	if !errors.Is(err, loadErr) {
		t.Errorf("DeployNew error = %v; want an error wrapping the protocol loader's error %v; "+
			"swallowing the original error would hide the real failure from the caller", err, loadErr)
	}

	// Assert — the executor must not be called (no writes must occur).
	if spy.called {
		t.Error("executor was called during DeployNew despite ProtocolLoader failure; " +
			"the run must abort before any file is written when protocol loading fails")
	}
}

// TestUpdate_ProtocolLoadError_AbortsBeforeExecutor verifies that when ProtocolLoader
// returns an error, the update flow propagates it without calling the executor.
func TestUpdate_ProtocolLoadError_AbortsBeforeExecutor(t *testing.T) {
	// Arrange
	loadErr := errors.New("protocol source missing: test sentinel")
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)

	spy := &protocolTestExecutorSpy{result: newMinimalExecResult(workspace)}
	deps.Executor = spy
	deps.ProtocolLoader = &stubProtocolLoader{err: loadErr}
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if err == nil {
		t.Fatal("Update returned nil error when ProtocolLoader failed; " +
			"a protocol load failure must abort the update run and return an error")
	}
	// The returned error must wrap the original load error, not an unrelated error.
	if !errors.Is(err, loadErr) {
		t.Errorf("Update error = %v; want an error wrapping the protocol loader's error %v; "+
			"swallowing the original error would hide the real failure from the caller", err, loadErr)
	}
	if spy.called {
		t.Error("executor was called during Update despite ProtocolLoader failure; " +
			"no file must be written before protocol loading succeeds")
	}
}

// TestUpdateWorkflows_ProtocolLoadError_AbortsBeforeExecutor verifies that when
// ProtocolLoader returns an error, the workflow-update flow propagates it without calling
// the executor.
func TestUpdateWorkflows_ProtocolLoadError_AbortsBeforeExecutor(t *testing.T) {
	// Arrange
	loadErr := errors.New("protocol source missing: test sentinel")
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)

	spy := &protocolTestExecutorSpy{result: newMinimalExecResult(workspace)}
	deps.Executor = spy
	deps.ProtocolLoader = &stubProtocolLoader{err: loadErr}
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
		t.Fatal("UpdateWorkflows returned nil error when ProtocolLoader failed; " +
			"a protocol load failure must abort the workflow-update run and return an error")
	}
	// The returned error must wrap the original load error, not an unrelated error.
	if !errors.Is(err, loadErr) {
		t.Errorf("UpdateWorkflows error = %v; want an error wrapping the protocol loader's error %v; "+
			"swallowing the original error would hide the real failure from the caller", err, loadErr)
	}
	if spy.called {
		t.Error("executor was called during UpdateWorkflows despite ProtocolLoader failure; " +
			"no file must be written before protocol loading succeeds")
	}
}

// ---------------------------------------------------------------------------
// Loaded protocol version reaches plan.Input — all three flows
// ---------------------------------------------------------------------------

// TestDeployNew_ProtocolVersion_ReachesPlannerInput verifies that when ProtocolLoader
// returns a version string, that version is propagated to plan.Input.ProtocolVersion before
// the planner is called. The planner uses this field to detect protocol staleness.
func TestDeployNew_ProtocolVersion_ReachesPlannerInput(t *testing.T) {
	// Arrange
	const wantVersion = "1.9"
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)

	plannerSpy := &spyPlanner{response: newMinimalPlan(workspace)}
	deps.Planner = plannerSpy
	deps.ProtocolLoader = &stubProtocolLoader{content: minimalProtocolContent(wantVersion)}
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert — the planner must have received the protocol version from the loader.
	captured := plannerSpy.lastCaptured(t)
	if captured.ProtocolVersion != wantVersion {
		t.Errorf("plan.Input.ProtocolVersion = %q during DeployNew, want %q; "+
			"the protocol source version must reach the planner for staleness detection",
			captured.ProtocolVersion, wantVersion)
	}
}

// TestUpdate_ProtocolVersion_ReachesPlannerInput verifies that the loaded protocol version
// reaches plan.Input.ProtocolVersion during the update flow.
func TestUpdate_ProtocolVersion_ReachesPlannerInput(t *testing.T) {
	// Arrange
	const wantVersion = "1.9"
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
	deps.ProtocolLoader = &stubProtocolLoader{content: minimalProtocolContent(wantVersion)}
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	captured := plannerSpy.lastCaptured(t)
	if captured.ProtocolVersion != wantVersion {
		t.Errorf("plan.Input.ProtocolVersion = %q during Update, want %q; "+
			"the protocol version must reach the planner in the update flow",
			captured.ProtocolVersion, wantVersion)
	}
}

// ---------------------------------------------------------------------------
// NOTE: Protocol.Blocks propagation to transform.Request — deferred
// ---------------------------------------------------------------------------
//
// A test asserting that transform.Request.Protocol.Blocks (not just ProtocolVersion in
// plan.Input) carries the loaded block content for all three flows was considered but is
// not yet written. The current infrastructure cannot observe the transform.Request without
// either (a) routing a real agent through the executor or (b) adding a transform-level spy.
//
// The stubPlanner in these tests returns a plan with no PlanItems, so buildContent's
// returned closure is never invoked and the transform is never called. Observing Blocks
// would require wiring a PlanItem through the stub plan and intercepting the transform.Request,
// which is a broader test-infrastructure addition that will be addressed when the full
// integration path (deploy.go, update.go, workflow_update.go + buildContent) is under test
// as part of I5.3/I5.4. The version-propagation tests above are sufficient to confirm that
// the loader result reaches the plan layer; the Blocks path is confirmed separately by the
// catalog loader tests (T5.1) and the transform tests for applyProtocolRegion.

// TestUpdateWorkflows_ProtocolVersion_ReachesPlannerInput verifies that the loaded protocol
// version reaches plan.Input.ProtocolVersion during the workflow-update flow.
func TestUpdateWorkflows_ProtocolVersion_ReachesPlannerInput(t *testing.T) {
	// Arrange
	const wantVersion = "1.9"
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
	deps.ProtocolLoader = &stubProtocolLoader{content: minimalProtocolContent(wantVersion)}
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
	if captured.ProtocolVersion != wantVersion {
		t.Errorf("plan.Input.ProtocolVersion = %q during UpdateWorkflows, want %q; "+
			"the protocol version must reach the planner in all three flows",
			captured.ProtocolVersion, wantVersion)
	}
}
