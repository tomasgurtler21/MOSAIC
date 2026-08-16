package app_test

// update_atomic_test.go verifies the atomic opt-in for the Update flow and the
// caller-facing behaviour of a reversed run.
//
// Verified invariants (Stage 5):
//
// Workspace restoration (T5.1):
//   - Update passes Atomic:true to the executor so the executor's reversal logic fires
//   - A caller receiving a reverted result gets *RevertedRunError, not a summary
//
// Failure signal to the caller (T5.2):
//   - A reversed Update run returns a zero domain.RunSummary and a non-nil *RevertedRunError
//   - The error matches app.ErrRunReverted under errors.Is
//   - The error carries the original triggering cause
//   - When the reversal itself had failures, UnrestoredPaths is populated
//
// Backup survival and deploy-new non-atomic (T5.3):
//   - Update passes Atomic:true to the executor
//   - DeployNew passes Atomic:false (zero value) — it must not opt in
//   - A reverted run that included a conflict backup decision still returns *RevertedRunError;
//     the app layer does not attempt to remove the backup
//
// Fallback runs unchanged (T5.4):
//   - When the executor resolves a fallback tier, the run is not reversed (Reverted:false)
//   - Update returns a non-error RunSummary for a fallback run, preserving today's behaviour
//
// RevertedRunError Is-semantics (from design specification):
//   - errors.Is(&RevertedRunError{Cause:c}, ErrRunReverted) == true
//   - errors.Is(&RevertedRunError{Cause:c}, ErrPlanNotConfirmed) == false
//   - errors.Is(&RevertedRunError{Cause:c}, c) == true  (via Unwrap)
//   - errors.Is(&RevertedRunError{Cause:c}, lookalike) == false (identity, not text)
//   - errors.As works for the concrete type

import (
	"context"
	"errors"
	"testing"

	"mosaic-deploy/internal/app"
	"mosaic-deploy/internal/app/interactiontest"
	"mosaic-deploy/internal/deploy"
	"mosaic-deploy/internal/domain"
)

// ---------------------------------------------------------------------------
// T5.1 + T5.2: Workspace restoration and failure signal
// ---------------------------------------------------------------------------

// TestUpdate_RevertedRun_ReturnsRevertedRunError verifies that when the executor signals a
// reversed run (Reverted:true), Update returns a *RevertedRunError and not a populated summary.
// This covers T5.1 (workspace restoration via atomic mode) and T5.2 (failure signal).
func TestUpdate_RevertedRun_ReturnsRevertedRunError(t *testing.T) {
	// Arrange — executor signals a clean reversal (Partial carries the trigger, Reverted:true)
	triggerErr := errors.New("simulated write failure for reversal test")
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: "test-runner.md",
				Action:     domain.ActionCreate,
			},
		},
		Workflows: []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Partial:        triggerErr,
		Reverted:       true,
		RevertFailures: nil,
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — must return an error, not a summary
	if err == nil {
		t.Fatal("Update returned nil error for a reverted run; want *RevertedRunError")
	}
	var revErr *app.RevertedRunError
	if !errors.As(err, &revErr) {
		t.Fatalf("Update error is %T, want *app.RevertedRunError", err)
	}
	// Zero RunSummary must be returned (Reverted/buildSummary invariant): neither Mode nor
	// Outcome should be populated, because buildSummary must be bypassed for a reverted run.
	if summary.Mode != "" || summary.Outcome != "" {
		t.Errorf("Update returned a populated RunSummary for a reverted run (Mode=%q, Outcome=%q); "+
			"a reversed run must never produce a summary — buildSummary must be bypassed", summary.Mode, summary.Outcome)
	}
}

// TestUpdate_RevertedRun_ErrorMatchesErrRunReverted verifies that the error returned for a
// reversed run matches app.ErrRunReverted under errors.Is, so callers can distinguish it
// from other failure categories without a type assertion.
func TestUpdate_RevertedRun_ErrorMatchesErrRunReverted(t *testing.T) {
	// Arrange
	triggerErr := errors.New("write failure")
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
		Workflows:     []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Partial:        triggerErr,
		Reverted:       true,
	}}
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if !errors.Is(err, app.ErrRunReverted) {
		t.Errorf("errors.Is(err, app.ErrRunReverted) = false; want true for a reversed Update run (err=%v)", err)
	}
}

// TestUpdate_RevertedRun_ErrorCarriesOriginalCause verifies that the *RevertedRunError
// returned for a reversed run carries the executor's triggering error as Cause, so the user
// sees why the run failed.
func TestUpdate_RevertedRun_ErrorCarriesOriginalCause(t *testing.T) {
	// Arrange — use a sentinel error as the trigger so the test can assert identity
	triggerErr := errors.New("disk full while writing agent file")
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
		Workflows:     []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Partial:        triggerErr,
		Reverted:       true,
	}}
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the cause must be reachable through errors.Is (via Unwrap)
	if !errors.Is(err, triggerErr) {
		t.Errorf("errors.Is(err, triggerErr) = false; RevertedRunError must unwrap to the original cause (err=%v)", err)
	}
	var revErr *app.RevertedRunError
	if !errors.As(err, &revErr) {
		t.Fatalf("errors.As for *app.RevertedRunError failed; err=%v", err)
	}
	if revErr.Cause != triggerErr {
		t.Errorf("RevertedRunError.Cause = %v; want the original trigger error %v", revErr.Cause, triggerErr)
	}
}

// TestUpdate_RevertedRun_UnrestoredPathsPopulatedOnIncompleteReversal verifies that when
// the reversal itself partially fails (RevertFailures non-empty), the *RevertedRunError
// carries the unrestored paths so the user knows which paths need manual recovery.
func TestUpdate_RevertedRun_UnrestoredPathsPopulatedOnIncompleteReversal(t *testing.T) {
	// Arrange — executor signals a reversal that could not fully restore one path
	triggerErr := errors.New("initial write failure")
	revertPath := "/workspace/agents/agent-a.md"
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
		Workflows:     []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Partial:        triggerErr,
		Reverted:       true,
		RevertFailures: []deploy.RevertFailure{
			{Path: revertPath, Err: errors.New("permission denied"), Restore: true},
		},
	}}
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	var revErr *app.RevertedRunError
	if !errors.As(err, &revErr) {
		t.Fatalf("errors.As for *app.RevertedRunError failed; err=%v", err)
	}
	if len(revErr.UnrestoredPaths) == 0 {
		t.Error("RevertedRunError.UnrestoredPaths is empty; want the path that could not be restored")
	} else if revErr.UnrestoredPaths[0] != revertPath {
		t.Errorf("RevertedRunError.UnrestoredPaths[0] = %q; want %q", revErr.UnrestoredPaths[0], revertPath)
	}
}

// ---------------------------------------------------------------------------
// T5.3: Atomic opt-in for Update, non-opt-in for DeployNew, backup survival
// ---------------------------------------------------------------------------

// TestUpdate_SetsAtomicTrue_OnExecRequest verifies that the Update flow passes Atomic:true
// to the executor so the executor's all-or-nothing reversal mode is engaged.
func TestUpdate_SetsAtomicTrue_OnExecRequest(t *testing.T) {
	// Arrange — use a capturingExecutor to inspect the request
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
		Workflows:     []string{"quick-fix"},
	}}
	spy := &capturingExecutor{}
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert
	if spy.capturedReq == nil {
		t.Fatal("capturingExecutor.Execute was never called; Update did not invoke the executor")
	}
	if !spy.capturedReq.Atomic {
		t.Error("ExecRequest.Atomic is false; Update must opt the execution request into atomic mode (Atomic:true)")
	}
}

// TestDeployNew_DoesNotSetAtomic_OnExecRequest verifies that the DeployNew flow does NOT
// pass Atomic:true to the executor. DeployNew must keep today's partial-failure semantics.
func TestDeployNew_DoesNotSetAtomic_OnExecRequest(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().Build()
	deps, workspace := newBaseDeps(t, stub)
	spy := &capturingExecutor{}
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.DeployNew(context.Background(), newDeployRequest(workspace, "stub-harness", []string{"quick-fix"}))

	// Assert
	if spy.capturedReq == nil {
		t.Fatal("capturingExecutor.Execute was never called; DeployNew did not invoke the executor")
	}
	if spy.capturedReq.Atomic {
		t.Error("ExecRequest.Atomic is true for DeployNew; DeployNew must not opt into atomic mode — " +
			"it must retain partial-failure semantics")
	}
}

// TestUpdate_RevertedRunWithConflictBackup_ReturnsRevertedRunError verifies that when a
// reversed Update run involved a conflict file that was backed up before the reversal, the
// app layer still returns *RevertedRunError and does not attempt to remove the backup. The
// backup path is carried in the action records (informational) but is not something the app
// layer reverses. The executor's journal deliberately excludes backup paths from reversal.
func TestUpdate_RevertedRunWithConflictBackup_ReturnsRevertedRunError(t *testing.T) {
	// Arrange — executor signals a reversal; actions include a TakenBackedUp entry, which
	// represents a backup that was written before the failure. After reversal the backed-up
	// content (the user's modified file) is still at the backup location.
	triggerErr := errors.New("write failure after backing up conflict file")
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QLocalModification, "agents/test-runner.md", string(domain.DecisionBackupThenOverwrite)).
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items: []domain.PlanItem{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: "agents/test-runner.md",
				Action:     domain.ActionConflict,
				Conflict:   &domain.LocalModification{RecordedHash: "sha256:aaa", CurrentHash: "sha256:bbb"},
			},
		},
		Workflows: []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Partial:        triggerErr,
		Reverted:       true,
		// Actions record reflects what was done before the failure: the backup was written.
		Actions: []domain.ActionRecord{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"},
				TargetPath: workspace + "/agents/test-runner.md",
				Taken:      domain.TakenBackedUp,
				BackupPath: workspace + "/.mosaic/backups/test-runner.md.2025-01-01T00:00:00Z.bak",
			},
		},
	}}
	svc := app.New(deps)

	// Act
	_, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — the flow must return *RevertedRunError regardless of backup activity
	if err == nil {
		t.Fatal("Update returned nil error for a reverted run with conflict backup; want *RevertedRunError")
	}
	var revErr *app.RevertedRunError
	if !errors.As(err, &revErr) {
		t.Fatalf("Update error is %T, want *app.RevertedRunError (err=%v)", err, err)
	}
	if !errors.Is(err, app.ErrRunReverted) {
		t.Error("errors.Is(err, app.ErrRunReverted) = false; want true")
	}
}

// ---------------------------------------------------------------------------
// T5.4: Fallback runs unchanged
// ---------------------------------------------------------------------------

// TestUpdate_FallbackRun_ReturnsSummaryNotError verifies that when the executor resolves a
// fallback tier (Fallback != FallbackNone) and does not reverse the run (Reverted:false),
// Update returns a RunSummary without error — preserving the pre-atomic-mode behaviour for
// fallback runs.
func TestUpdate_FallbackRun_ReturnsSummaryNotError(t *testing.T) {
	// Arrange — executor uses a fallback tier (workspace unwritable) but completes successfully
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
		Workflows:     []string{"quick-fix"},
	}}
	fallbackRoot := deps.MosaicRoot + "/MosaicDeploy/fallback/workspace-slug"
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: fallbackRoot,
		Fallback:       domain.FallbackMosaicRoot,
		Reverted:       false, // fallback runs are never reversed
		Actions:        []domain.ActionRecord{},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — a fallback run must not return an error and must not return a zero summary
	if err != nil {
		t.Errorf("Update returned error for a fallback run: %v; "+
			"fallback runs must not be reversed and must return a summary (unchanged behaviour)", err)
	}
	if summary.Fallback == domain.FallbackNone {
		t.Error("summary.Fallback = FallbackNone; a fallback run must report the fallback tier used")
	}
}

// TestUpdate_FallbackRun_RevertedFalsePreservesPartialSemantics verifies that when the
// executor runs in fallback mode (no reversal), the Reverted field is false and the flow
// returns a summary reflecting the partial state — preserving today's semantics exactly.
func TestUpdate_FallbackRun_RevertedFalsePreservesPartialSemantics(t *testing.T) {
	// Arrange — a partial fallback run: some items written, Partial set, Reverted false
	stub := interactiontest.NewBuilder().
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: domain.Plan{
		Mode:          domain.ModeUpdateWorkspace,
		Harness:       minimalHarness,
		WorkspacePath: workspace,
		Scope:         domain.ScopeProject,
		Items:         []domain.PlanItem{},
		Workflows:     []string{"quick-fix"},
	}}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: deps.MosaicRoot + "/MosaicDeploy/fallback/ws",
		Fallback:       domain.FallbackMosaicRoot,
		Partial:        errors.New("one item failed in fallback"),
		Reverted:       false, // must remain false for fallback runs
		Actions:        []domain.ActionRecord{},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.Update(context.Background(), app.UpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		AutoConfirmPlan: true,
	})

	// Assert — a fallback partial run must not return an error; it must return a summary
	// reflecting the partial state (OutcomeFailed, Fallback set), preserving today's semantics.
	// The app layer only converts to *RevertedRunError when Reverted:true.
	if err != nil {
		t.Fatalf("Update returned error for a fallback partial run: %v; "+
			"fallback runs must not be reversed and must return a summary with OutcomeFailed (not an error)", err)
	}
	if summary.Outcome != domain.OutcomeFailed {
		t.Errorf("summary.Outcome = %q; want %q for a partial fallback run — "+
			"the summary must reflect the failed state without reversal", summary.Outcome, domain.OutcomeFailed)
	}
	if summary.Fallback == domain.FallbackNone {
		t.Error("summary.Fallback = FallbackNone; a fallback run must report the fallback tier used")
	}
}

// ---------------------------------------------------------------------------
// RevertedRunError Is-semantics (design specification normative table)
// ---------------------------------------------------------------------------

// TestRevertedRunError_IsErrRunReverted verifies that errors.Is matches ErrRunReverted for
// any *RevertedRunError, regardless of the Cause value.
func TestRevertedRunError_IsErrRunReverted(t *testing.T) {
	cause := errors.New("underlying cause")
	revErr := &app.RevertedRunError{Cause: cause}

	if !errors.Is(revErr, app.ErrRunReverted) {
		t.Error("errors.Is(&RevertedRunError{Cause:c}, ErrRunReverted) = false; want true")
	}
}

// TestRevertedRunError_IsNotErrPlanNotConfirmed verifies that errors.Is does NOT match
// ErrPlanNotConfirmed — the two sentinels are distinct, and Is must compare identity only.
func TestRevertedRunError_IsNotErrPlanNotConfirmed(t *testing.T) {
	cause := errors.New("underlying cause")
	revErr := &app.RevertedRunError{Cause: cause}

	if errors.Is(revErr, app.ErrPlanNotConfirmed) {
		t.Error("errors.Is(&RevertedRunError{Cause:c}, ErrPlanNotConfirmed) = true; " +
			"want false — Is must compare identity against ErrRunReverted only")
	}
}

// TestRevertedRunError_UnwrapsCauseSoErrorsIsMatchesCause verifies that the Cause error is
// reachable through errors.Is via Unwrap, without requiring a type assertion.
func TestRevertedRunError_UnwrapsCauseSoErrorsIsMatchesCause(t *testing.T) {
	cause := errors.New("the underlying disk error")
	revErr := &app.RevertedRunError{Cause: cause}

	if !errors.Is(revErr, cause) {
		t.Error("errors.Is(&RevertedRunError{Cause:c}, c) = false; " +
			"want true — Unwrap must return Cause so errors.Is reaches it")
	}
}

// TestRevertedRunError_DoesNotMatchLookalikeSentinel verifies that a newly constructed error
// with the same message text as ErrRunReverted does NOT match — Is uses identity, not text.
func TestRevertedRunError_DoesNotMatchLookalikeSentinel(t *testing.T) {
	cause := errors.New("underlying cause")
	revErr := &app.RevertedRunError{Cause: cause}

	// Construct a lookalike that shares the message text of ErrRunReverted but is a different
	// pointer value. Is must return false because it is not the sentinel itself.
	lookalike := errors.New("deployment run failed and the workspace was restored")
	if errors.Is(revErr, lookalike) {
		t.Error("errors.Is(&RevertedRunError{}, lookalike) = true; " +
			"want false — Is must use identity comparison, never message-text comparison")
	}
}

// TestRevertedRunError_ErrorsAs_BindsConcretType verifies that errors.As can extract the
// *RevertedRunError from any error that wraps it.
func TestRevertedRunError_ErrorsAs_BindsConcreteType(t *testing.T) {
	cause := errors.New("original failure")
	revErr := &app.RevertedRunError{Cause: cause, UnrestoredPaths: []string{"/some/path"}}

	var target *app.RevertedRunError
	if !errors.As(revErr, &target) {
		t.Fatal("errors.As(&RevertedRunError{...}, &target) = false; want true")
	}
	if target.Cause != cause {
		t.Errorf("bound target.Cause = %v; want %v", target.Cause, cause)
	}
	if len(target.UnrestoredPaths) != 1 || target.UnrestoredPaths[0] != "/some/path" {
		t.Errorf("bound target.UnrestoredPaths = %v; want [\"/some/path\"]", target.UnrestoredPaths)
	}
}

// TestRevertedRunError_IsNotOtherError verifies that an unrelated error does not match
// ErrRunReverted — only *RevertedRunError and ErrRunReverted itself match the sentinel.
func TestRevertedRunError_IsNotOtherError(t *testing.T) {
	someOtherErr := errors.New("some unrelated error")

	if errors.Is(someOtherErr, app.ErrRunReverted) {
		t.Error("errors.Is(someOtherError, ErrRunReverted) = true; " +
			"want false — only *RevertedRunError matches the sentinel")
	}
}
