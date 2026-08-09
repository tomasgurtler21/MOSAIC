package app_test

// persistfailure_notice_test.go verifies that a failed convenience-persist (Save failing
// inside persistTierModels or persistCustomModelIDs, called after Executor.Execute has
// already succeeded) is surfaced to the user through a warning Notice — instead of being
// silently dropped via `_ = s.persist...(...)` — and that surfacing it does not misreport an
// otherwise-successful deployment as failed.
//
// Per ContractsDesign.md's decision ("warn, do not abort"): the deployment has already
// completed and the workspace is already written by the time a persist is attempted, so a
// persist failure must never turn a successful RunSummary into an error. The two persists
// attempted at a given call site are independent — a failure of persistTierModels does not
// skip persistCustomModelIDs, and vice versa, and each failure produces its own Notice.
//
// Verified invariants:
//
// Warn, don't abort (AC5.3):
//   - DeployNew still returns a nil error and a populated RunSummary when a persist Save
//     fails after a successful Executor.Execute.
//   - Executor.Execute is still called (the deployment itself is unaffected by a persist
//     failure that happens after it).
//
// The warning is visible (AC5.3):
//   - Interaction.Notify receives a domain.Notice with Level == domain.NoticeWarning.
//   - The Notice names the config file (UserConfigStore.Path()) and states that the
//     deployment succeeded.
//
// Independent attempts:
//   - When both persistTierModels and persistCustomModelIDs are called at the same site and
//     both fail, two separate Notices are recorded — one is not skipped because of the other.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// DeployNew — both persists fail; both are surfaced independently
// ---------------------------------------------------------------------------

// TestDeployNew_PersistSaveFails_SucceedsWithWarningNotice verifies that DeployNew still
// reports success when the post-execution persist Save fails, and that the failure is
// surfaced through a warning Notice naming the config file.
func TestDeployNew_PersistSaveFails_SucceedsWithWarningNotice(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	saveErr := errors.New("permission denied")
	spy := &spyUserConfig{saveErr: saveErr, path: "/mosaic/MosaicDeploy/config/user-config.yaml"}
	executor := &deploySpyExecutor{}
	deps.UserConfig = spy
	deps.Executor = executor
	svc := app.New(deps)

	// Act
	summary, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert — the run itself still succeeds.
	if err != nil {
		t.Fatalf("DeployNew returned an error after a post-execution persist Save failed: %v; "+
			"a persist failure must not misreport an otherwise-successful deployment (AC5.3)", err)
	}
	if !executor.called {
		t.Fatal("Executor.Execute was not called; this test requires a completed deployment to isolate the persist-failure path")
	}
	if summary.DeploymentRoot == "" && summary.Harness.ID == "" {
		t.Error("RunSummary looks like the zero value after a successful deployment with a failed persist; " +
			"the run must still report its actual outcome (AC5.3)")
	}

	// Assert — the failure was surfaced through a warning Notice.
	notices := stub.Notices()
	if len(notices) == 0 {
		t.Fatal("Interaction.Notify was never called after a persist Save failure; " +
			"a failed persist must be surfaced, not silently dropped (AC5.3)")
	}
	for _, n := range notices {
		if n.Level != domain.NoticeWarning {
			t.Errorf("Notice.Level = %q, want %q for a persist-save failure", n.Level, domain.NoticeWarning)
		}
		if !strings.Contains(n.Message, spy.path) {
			t.Errorf("Notice.Message = %q; want it to name the config file %q", n.Message, spy.path)
		}
		if !strings.Contains(strings.ToLower(n.Message), "succeed") {
			t.Errorf("Notice.Message = %q; want it to state that the deployment itself succeeded, "+
				"so the user does not mistake this warning for a failed deployment", n.Message)
		}
	}
}

// TestDeployNew_PersistSaveFails_BothPersistsSurfacedIndependently verifies that when both
// persistTierModels and persistCustomModelIDs fail at DeployNew's persist site, each produces
// its own Notice — a failure of one does not suppress the other's attempt or its Notice.
func TestDeployNew_PersistSaveFails_BothPersistsSurfacedIndependently(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	spy := &spyUserConfig{saveErr: errors.New("disk full"), path: "/mosaic/MosaicDeploy/config/user-config.yaml"}
	deps.UserConfig = spy
	svc := app.New(deps)

	// Act
	_, err := svc.DeployNew(context.Background(), app.DeployRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	})
	if err != nil {
		t.Fatalf("DeployNew: %v", err)
	}

	// Assert — DeployNew calls both persistTierModels and persistCustomModelIDs
	// unconditionally after a successful execution, so both Save attempts fail and each
	// must produce its own Notice.
	if len(spy.saved) < 2 {
		t.Fatalf("UserConfig.Save was called %d time(s); want at least 2 (persistTierModels and "+
			"persistCustomModelIDs are independent attempts)", len(spy.saved))
	}
	notices := stub.Notices()
	if len(notices) < 2 {
		t.Errorf("Interaction.Notify received %d notice(s); want at least 2, one per failed persist "+
			"attempt — a failure of persistTierModels must not suppress persistCustomModelIDs' own attempt "+
			"and Notice, or vice versa", len(notices))
	}
}

// ---------------------------------------------------------------------------
// Update — persistCustomModelIDs failure is surfaced
// ---------------------------------------------------------------------------

// TestUpdate_PersistCustomModelIDsSaveFails_SucceedsWithWarningNotice verifies that Update
// still reports success when its persistCustomModelIDs Save fails, surfaced through a
// warning Notice.
func TestUpdate_PersistCustomModelIDsSaveFails_SucceedsWithWarningNotice(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	saveErr := errors.New("permission denied")
	spy := &spyUserConfig{saveErr: saveErr, path: "/mosaic/MosaicDeploy/config/user-config.yaml"}
	executor := &deploySpyExecutor{}
	deps.UserConfig = spy
	deps.Executor = executor
	svc := app.New(deps)

	// Act — "test-runner.md" is absent, so it is newly-required and reaches the persist step.
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AddWorkflowIDs:  []string{"quick-fix"},
		AutoConfirmPlan: true,
	})

	// Assert
	if err != nil {
		t.Fatalf("Update returned an error after a post-execution persist Save failed: %v; "+
			"a persist failure must not misreport an otherwise-successful deployment (AC5.3)", err)
	}
	if !executor.called {
		t.Fatal("Executor.Execute was not called; this test requires a completed run to isolate the persist-failure path")
	}
	notices := stub.Notices()
	if len(notices) == 0 {
		t.Fatal("Interaction.Notify was never called after Update's persistCustomModelIDs Save failed (AC5.3)")
	}
	found := false
	for _, n := range notices {
		if n.Level == domain.NoticeWarning && strings.Contains(n.Message, spy.path) {
			found = true
		}
	}
	if !found {
		t.Errorf("no warning Notice naming %q was recorded; got: %+v", spy.path, notices)
	}
}

// ---------------------------------------------------------------------------
// UpdateWorkflows — both persists fail; both are surfaced independently
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_PersistSaveFails_SucceedsWithWarningNotices verifies that
// UpdateWorkflows still reports success when its persist Save calls fail, surfaced through
// warning Notices, for a run with a newly-required agent (the only case in which
// UpdateWorkflows calls either persist function).
func TestUpdateWorkflows_PersistSaveFails_SucceedsWithWarningNotices(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	saveErr := errors.New("disk full")
	spy := &spyUserConfig{saveErr: saveErr, path: "/mosaic/MosaicDeploy/config/user-config.yaml"}
	executor := &deploySpyExecutor{}
	deps.UserConfig = spy
	deps.Executor = executor
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowUpdateRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if err != nil {
		t.Fatalf("UpdateWorkflows returned an error after a post-execution persist Save failed: %v; "+
			"a persist failure must not misreport an otherwise-successful deployment (AC5.3)", err)
	}
	if !executor.called {
		t.Fatal("Executor.Execute was not called; this test requires a completed run to isolate the persist-failure path")
	}
	notices := stub.Notices()
	if len(notices) < 2 {
		t.Errorf("Interaction.Notify received %d notice(s) after both UpdateWorkflows persists failed; "+
			"want at least 2 (persistTierModels and persistCustomModelIDs are independent attempts), got: %+v",
			len(notices), notices)
	}
	for _, n := range notices {
		if n.Level != domain.NoticeWarning {
			t.Errorf("Notice.Level = %q, want %q", n.Level, domain.NoticeWarning)
		}
	}
}
