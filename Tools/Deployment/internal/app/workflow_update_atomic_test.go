package app_test

// workflow_update_atomic_test.go verifies the atomic opt-in and rollback guarantees for
// the UpdateWorkflows flow.
//
// These tests are in TDD RED phase: the current workflow_update.go implementation does not
// set Atomic:true on the ExecRequest and does not check result.Reverted. The failing tests
// drive the two implementation tasks:
//   - I4.1: Set Atomic:true on the ExecRequest built inside UpdateWorkflows.
//   - I4.2: Check result.Reverted after Execute returns and return *RevertedRunError instead
//     of calling buildSummary.
//
// Verified invariants:
//
// Failed UpdateWorkflows run leaves nothing behind (T4.1):
//   - When the executor signals Reverted:true, UpdateWorkflows returns a zero RunSummary and
//     a non-nil *RevertedRunError — buildSummary is bypassed entirely.
//   - The returned error matches ErrRunReverted under errors.Is.
//   - The returned summary has no Mode or Outcome populated.
//
// Caller receives a failure signal for a reversed run (T4.2):
//   - errors.Is(err, ErrRunReverted) is true.
//   - errors.As yields the concrete *RevertedRunError.
//   - The *RevertedRunError carries the executor's triggering error as Cause.
//   - When the reversal itself had partial failures, UnrestoredPaths is populated.
//   - A reversed run is distinct from a plan-not-confirmed error and from a plain executor error.
//
// Successful run produces a correct summary (T4.3):
//   - A successful atomic run (Reverted:false) returns a non-error RunSummary.
//   - The summary mode is ModeWorkflowsOnly.
//   - The executor's plan contains exactly the orchestrator item, the ActionCreate item for a
//     newly-required agent, and (when applicable) the ActionCreate item for a newly-required
//     skill — no pre-existing agent or skill item appears in the executed plan.
//
// UpdateWorkflows opts into atomic execution; DeployNew does not (T4.4):
//   - UpdateWorkflows sets Atomic:true on the deploy.ExecRequest it passes to the executor.
//     (DeployNew's non-opt-in is covered by TestDeployNew_DoesNotSetAtomic_OnExecRequest in
//     update_atomic_test.go; it is not repeated here.)

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
// Helpers local to this file
// ---------------------------------------------------------------------------

// newRevertedExecResult returns a deploy.ExecResult that signals a clean reversal.
// Reverted is true, Partial carries the trigger, RevertFailures is empty.
func newRevertedExecResult(workspace string, trigger error) deploy.ExecResult {
	return deploy.ExecResult{
		DeploymentRoot: workspace,
		Partial:        trigger,
		Reverted:       true,
		RevertFailures: nil,
	}
}

// newIncompleteRevertExecResult returns a deploy.ExecResult that signals a reversal where
// one journal entry could not be applied. UnrestoredPaths should appear in the resulting error.
func newIncompleteRevertExecResult(workspace string, trigger error, unrestoredPath string) deploy.ExecResult {
	return deploy.ExecResult{
		DeploymentRoot: workspace,
		Partial:        trigger,
		Reverted:       true,
		RevertFailures: []deploy.RevertFailure{
			{Path: unrestoredPath, Err: errors.New("permission denied"), Restore: true},
		},
	}
}

// newWorkflowsOnlyUpdateReq returns a WorkflowUpdateRequest for the minimal workspace with
// AutoConfirmPlan set so the plan review gate does not stop the flow.
func newWorkflowsOnlyUpdateReq(workspace string) app.WorkflowUpdateRequest {
	return app.WorkflowUpdateRequest{
		HarnessID:       "stub-harness",
		WorkspacePath:   workspace,
		WorkflowIDs:     []string{"quick-fix"},
		AutoConfirmPlan: true,
	}
}

// ---------------------------------------------------------------------------
// T4.1 — Failed UpdateWorkflows run: caller gets an error, not a summary
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_RevertedRun_ReturnsZeroSummaryAndError verifies that when the executor
// signals a reversed run (Reverted:true), UpdateWorkflows returns a zero RunSummary and a
// non-nil *RevertedRunError. buildSummary must be bypassed entirely — a summary would enumerate
// deployed artifacts that no longer exist on disk after the reversal.
//
// TDD RED: currently UpdateWorkflows does not check result.Reverted and calls buildSummary
// unconditionally, so this test fails because no error is returned.
func TestUpdateWorkflows_RevertedRun_ReturnsZeroSummaryAndError(t *testing.T) {
	// Arrange — executor signals a clean reversal
	triggerErr := errors.New("simulated write failure")
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	deps.Executor = &stubExecutor{result: newRevertedExecResult(workspace, triggerErr)}
	svc := app.New(deps)

	// Act
	summary, err := svc.UpdateWorkflows(context.Background(), newWorkflowsOnlyUpdateReq(workspace))

	// Assert — must return an error, not a summary
	if err == nil {
		t.Fatal("UpdateWorkflows returned nil error for a reverted run; want *RevertedRunError — " +
			"a reversed run must never produce a summary because buildSummary would describe files " +
			"that no longer exist on disk")
	}
	var revErr *app.RevertedRunError
	if !errors.As(err, &revErr) {
		t.Fatalf("UpdateWorkflows error is %T, want *app.RevertedRunError (err=%v)", err, err)
	}
	// Zero RunSummary invariant: buildSummary must be bypassed for a reverted run.
	if summary.Mode != "" || summary.Outcome != "" {
		t.Errorf("UpdateWorkflows returned a populated RunSummary for a reverted run "+
			"(Mode=%q, Outcome=%q); a reversed run must return a zero RunSummary — "+
			"the summary would describe files that are not on disk after reversal",
			summary.Mode, summary.Outcome)
	}
}

// TestUpdateWorkflows_RevertedRun_SummaryIsZeroValue verifies that the RunSummary returned
// alongside a *RevertedRunError is the zero value — no field is populated, no action records
// are included. This pins the invariant that buildSummary is not called for a reverted run.
//
// TDD RED: currently UpdateWorkflows calls buildSummary unconditionally.
func TestUpdateWorkflows_RevertedRun_SummaryIsZeroValue(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	deps.Executor = &stubExecutor{result: newRevertedExecResult(workspace, errors.New("disk full"))}
	svc := app.New(deps)

	// Act
	summary, err := svc.UpdateWorkflows(context.Background(), newWorkflowsOnlyUpdateReq(workspace))

	// Assert — error must be present; summary must be exactly the zero value
	if err == nil {
		t.Fatal("UpdateWorkflows returned nil error for a reverted run; want non-nil error")
	}
	// A zero RunSummary has no Mode, Outcome, or Actions. We check the fields that
	// buildSummary would populate; comparing Fallback and Workspace is less meaningful
	// since the zero value is FallbackNone ("") and "" respectively.
	if summary.Mode != "" || summary.Outcome != "" || len(summary.Actions) != 0 {
		t.Errorf("RunSummary is not the zero value: got Mode=%q Outcome=%q Actions=%v; "+
			"want zero RunSummary — a reversed run must bypass buildSummary entirely",
			summary.Mode, summary.Outcome, summary.Actions)
	}
}

// TestUpdateWorkflows_RevertedRun_WithNewAgentPlan_StillReturnsError verifies that the
// reverted-run error path fires even when the plan contained a new-agent ActionCreate item.
// The filter and model-resolution steps run normally; only the post-execution branch changes.
//
// TDD RED: currently UpdateWorkflows does not check result.Reverted.
func TestUpdateWorkflows_RevertedRun_WithNewAgentPlan_StillReturnsError(t *testing.T) {
	// Arrange — plan includes a new agent (ActionCreate); executor signals reversal
	triggerErr := errors.New("write failure after first item")
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	// test-runner.md not written — absent, classified as new
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}
	deps.Executor = &stubExecutor{result: newRevertedExecResult(workspace, triggerErr)}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowsOnlyUpdateReq(workspace))

	// Assert — reversal must produce an error regardless of plan content
	if err == nil {
		t.Fatal("UpdateWorkflows returned nil error for a reverted run with new-agent plan; " +
			"want *RevertedRunError — the reverted-run check must fire after Execute returns, " +
			"regardless of whether new agents were present in the plan")
	}
	if !errors.Is(err, app.ErrRunReverted) {
		t.Errorf("error does not match ErrRunReverted; err=%v", err)
	}
}

// ---------------------------------------------------------------------------
// T4.2 — Caller receives the correct failure signal type and content
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_RevertedRun_ErrorMatchesErrRunReverted verifies that the error returned
// for a reversed UpdateWorkflows run matches app.ErrRunReverted under errors.Is. This lets
// frontends distinguish a reversed run from all other failure categories without a type
// assertion, using the same sentinel-matching pattern as ErrPlanNotConfirmed.
//
// TDD RED: currently UpdateWorkflows returns nil error for a Reverted:true result.
func TestUpdateWorkflows_RevertedRun_ErrorMatchesErrRunReverted(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	deps.Executor = &stubExecutor{result: newRevertedExecResult(workspace, errors.New("write failure"))}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowsOnlyUpdateReq(workspace))

	// Assert
	if err == nil {
		t.Fatal("UpdateWorkflows returned nil error for a reverted run; want non-nil")
	}
	if !errors.Is(err, app.ErrRunReverted) {
		t.Errorf("errors.Is(err, app.ErrRunReverted) = false; want true — "+
			"a reversed UpdateWorkflows run must return an error that matches the ErrRunReverted sentinel "+
			"so frontends can distinguish it from other failure categories (err=%v)", err)
	}
}

// TestUpdateWorkflows_RevertedRun_ErrorCarriesOriginalCause verifies that the *RevertedRunError
// returned for a reversed UpdateWorkflows run carries the executor's triggering error as Cause,
// so the user sees why the run failed (not merely that the workspace was restored).
//
// TDD RED: currently UpdateWorkflows returns nil error.
func TestUpdateWorkflows_RevertedRun_ErrorCarriesOriginalCause(t *testing.T) {
	// Arrange — use a sentinel error as the trigger so the test can assert identity
	triggerErr := errors.New("disk quota exceeded while writing orchestrator")
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	deps.Executor = &stubExecutor{result: newRevertedExecResult(workspace, triggerErr)}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowsOnlyUpdateReq(workspace))

	// Assert — Cause must be reachable via errors.Is (through Unwrap) and directly on the struct
	if err == nil {
		t.Fatal("UpdateWorkflows returned nil error; want *RevertedRunError")
	}
	if !errors.Is(err, triggerErr) {
		t.Errorf("errors.Is(err, triggerErr) = false; "+
			"RevertedRunError must unwrap to the executor's triggering error — "+
			"the user must see why the run failed, not only that the workspace was restored (err=%v)", err)
	}
	var revErr *app.RevertedRunError
	if !errors.As(err, &revErr) {
		t.Fatalf("errors.As for *app.RevertedRunError failed; err=%v", err)
	}
	if !errors.Is(revErr.Cause, triggerErr) {
		t.Errorf("RevertedRunError.Cause = %v; want the original trigger error %v", revErr.Cause, triggerErr)
	}
}

// TestUpdateWorkflows_RevertedRun_UnrestoredPathsPopulated verifies that when the reversal
// itself partially fails (RevertFailures non-empty in the executor result), the *RevertedRunError
// carries the unrestored paths so the user knows which paths need manual recovery from backups.
//
// TDD RED: currently UpdateWorkflows returns nil error.
func TestUpdateWorkflows_RevertedRun_UnrestoredPathsPopulated(t *testing.T) {
	// Arrange — executor signals a reversal that could not fully restore one path
	triggerErr := errors.New("initial write failure triggering reversal")
	unrestoredPath := "/workspace/agents/new-agent.md"
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	deps.Executor = &stubExecutor{result: newIncompleteRevertExecResult(workspace, triggerErr, unrestoredPath)}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowsOnlyUpdateReq(workspace))

	// Assert — UnrestoredPaths must be populated so the user knows what to recover
	if err == nil {
		t.Fatal("UpdateWorkflows returned nil error for a reverted run; want *RevertedRunError")
	}
	var revErr *app.RevertedRunError
	if !errors.As(err, &revErr) {
		t.Fatalf("errors.As for *app.RevertedRunError failed; err=%v", err)
	}
	if len(revErr.UnrestoredPaths) == 0 {
		t.Error("RevertedRunError.UnrestoredPaths is empty; " +
			"when the reversal itself could not restore a path, UnrestoredPaths must be populated " +
			"so the user knows which paths require manual recovery from backups")
	} else if revErr.UnrestoredPaths[0] != unrestoredPath {
		t.Errorf("UnrestoredPaths[0] = %q; want %q", revErr.UnrestoredPaths[0], unrestoredPath)
	}
}

// TestUpdateWorkflows_RevertedRun_IsDistinctFromPlanNotConfirmed verifies that a reversed
// run error does NOT match ErrPlanNotConfirmed. The two failure categories are distinct and
// must not be conflated — ErrPlanNotConfirmed signals a user decision to cancel, while
// ErrRunReverted signals an execution failure with workspace restoration.
//
// TDD RED: currently UpdateWorkflows returns nil error, so errors.Is comparisons are trivially
// testing a nil error which returns false. Once the error is returned, this test verifies the
// sentinel identity rule.
func TestUpdateWorkflows_RevertedRun_IsDistinctFromPlanNotConfirmed(t *testing.T) {
	// Arrange
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	deps.Executor = &stubExecutor{result: newRevertedExecResult(workspace, errors.New("exec failure"))}
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowsOnlyUpdateReq(workspace))

	// Assert — ErrPlanNotConfirmed must not match a reversal error
	if err == nil {
		t.Fatal("UpdateWorkflows returned nil error for a reverted run; want *RevertedRunError")
	}
	if errors.Is(err, app.ErrPlanNotConfirmed) {
		t.Error("errors.Is(err, ErrPlanNotConfirmed) = true for a reversed run; " +
			"want false — ErrRunReverted and ErrPlanNotConfirmed are distinct sentinels with " +
			"different TUI routing semantics; they must not conflate")
	}
}

// ---------------------------------------------------------------------------
// T4.3 — Successful run produces the correct summary; exact write set
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_SuccessfulAtomicRun_ReturnsSummaryNotError verifies that when
// UpdateWorkflows executes successfully (executor returns Reverted:false, Partial:nil), the
// flow returns a non-error RunSummary with Mode=ModeWorkflowsOnly. This is the positive case
// that must hold alongside the reverted-run check.
func TestUpdateWorkflows_SuccessfulAtomicRun_ReturnsSummaryNotError(t *testing.T) {
	// Arrange — executor reports a successful run (no reversal)
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Reverted:       false,
		Partial:        nil,
		Actions: []domain.ActionRecord{
			{
				Ref:        domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"},
				TargetPath: workspace + "/orchestrator.md",
				Taken:      domain.TakenUpdated,
			},
		},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.UpdateWorkflows(context.Background(), newWorkflowsOnlyUpdateReq(workspace))

	// Assert — successful run must return a summary, never an error
	if err != nil {
		t.Fatalf("UpdateWorkflows returned error for a successful run: %v; "+
			"a successful atomic run must return a RunSummary, not an error", err)
	}
	if summary.Mode != domain.ModeWorkflowsOnly {
		t.Errorf("summary.Mode = %q, want %q; "+
			"UpdateWorkflows must produce a summary with Mode=ModeWorkflowsOnly", summary.Mode, domain.ModeWorkflowsOnly)
	}
}

// TestUpdateWorkflows_SuccessfulRun_NewAgentAndSkillInExecutedPlan verifies the exact write set
// for a successful UpdateWorkflows run that adds a new agent requiring a new skill: the executor
// receives a plan containing the orchestrator item, the new agent's ActionCreate item, and the
// new skill's ActionCreate item. No pre-existing agent or skill item appears in the plan.
//
// This pins the end-to-end guarantee that a successful run writing exactly the orchestrator,
// the new agent, and the missing required skill is what the executor sees.
//
// TDD RED for the atomic opt-in aspect: the overall plan composition was tested in T3.x but
// this test also asserts Atomic:true on the same request (see T4.4 below), so the T4.4 RED
// failure means this test does not finish verifying its assertions until I4.1 is implemented.
func TestUpdateWorkflows_SuccessfulRun_NewAgentAndSkillInExecutedPlan(t *testing.T) {
	// Arrange — skill-user.md and lean-tdd.md both absent; executor reports success (no reversal)
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newSkillEnabledCatalog()
	deps.Planner = &stubPlanner{plan: newSkillDeployPlan(workspace)}

	spy := &execRequestSpy{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Reverted:       false,
		Actions: []domain.ActionRecord{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"}, TargetPath: workspace + "/orchestrator.md", Taken: domain.TakenUpdated},
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "skill-user"}, TargetPath: workspace + "/skill-user.md", Taken: domain.TakenCreated},
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactSkill, Key: "lean-tdd"}, TargetPath: workspace + "/lean-tdd.md", Taken: domain.TakenCreated},
		},
	}}
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newSkillWorkflowRequest(workspace, "stub-harness"))

	// Assert — successful run, no error
	if err != nil {
		t.Fatalf("UpdateWorkflows error = %v; want nil for a successful run", err)
	}

	if spy.capturedReq == nil {
		t.Fatal("executor was not called; UpdateWorkflows must invoke the executor")
	}

	// Verify the executed plan has exactly the three expected items.
	plan := spy.capturedReq.Plan
	_, hasOrch := findPlanItem(plan.Items, "orchestrator")
	_, hasAgent := findPlanItem(plan.Items, "skill-user")
	_, hasSkill := findPlanItem(plan.Items, "lean-tdd")

	if !hasOrch {
		t.Error("executed plan missing orchestrator item; " +
			"the orchestrator must always be written in UpdateWorkflows")
	}
	if !hasAgent {
		t.Error("executed plan missing new agent item (skill-user); " +
			"an absent required agent must be in the executed plan as ActionCreate")
	}
	if !hasSkill {
		t.Error("executed plan missing new skill item (lean-tdd); " +
			"an absent skill required by a new agent must be in the executed plan as ActionCreate")
	}

	// Verify no non-orchestrator, non-new-agent, non-new-skill items are present.
	for _, item := range plan.Items {
		if item.Ref.Kind == domain.ArtifactHook {
			t.Errorf("executor received hook item %q; hook items must never reach the executor", item.Ref.Key)
		}
	}
}

// TestUpdateWorkflows_SuccessfulRun_ExistingAgentFileNotInPlan verifies that when an agent
// file already exists in the workspace (an already-deployed agent), it is NOT included in the
// executed plan. This pins the "already-deployed artifacts are never written" invariant at the
// plan-content level, complementing the byte-identical filesystem guarantee of AC4.4.
func TestUpdateWorkflows_SuccessfulRun_ExistingAgentFileNotInPlan(t *testing.T) {
	// Arrange — write test-runner.md to simulate an already-deployed agent. The planner stub
	// returns it as ActionUpdate (stale). The filter must exclude it from the executor's plan.
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	// Write the agent file so deployedState classifies it as present.
	writeTempFile(t, workspace, "test-runner.md", []byte("---\nversion: \"1.0\"\n---\nexisting agent content\n"))
	deps.Planner = &stubPlanner{plan: mixedArtifactPlan(workspace)}

	spy := &execRequestSpy{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Reverted:       false,
		Actions: []domain.ActionRecord{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"}, TargetPath: workspace + "/orchestrator.md", Taken: domain.TakenUpdated},
		},
	}}
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, err := svc.UpdateWorkflows(context.Background(), newWorkflowsOnlyUpdateReq(workspace))
	if err != nil {
		t.Fatalf("UpdateWorkflows error = %v; want nil", err)
	}

	// Assert — the already-deployed test-runner must NOT be in the executor's plan
	if spy.capturedReq == nil {
		t.Fatal("executor was not called")
	}
	for _, item := range spy.capturedReq.Plan.Items {
		if item.Ref.Kind == domain.ArtifactAgent && item.Ref.Key == "test-runner" {
			t.Errorf("executor received plan item for already-deployed agent \"test-runner\" "+
				"(action=%s); already-deployed agents must never be planned, written, or version-stamped — "+
				"this is the UpdateWorkflows invariant that makes already-deployed files byte-identical "+
				"before and after a successful run", item.Action)
		}
	}
}

// TestUpdateWorkflows_SuccessfulRun_ReturnsSummaryWithActions verifies that a successful run
// returns a RunSummary that includes the action records from the executor result, so the CLI
// and TUI can report what was written during the run.
func TestUpdateWorkflows_SuccessfulRun_ReturnsSummaryWithActions(t *testing.T) {
	// Arrange — executor returns two action records (orchestrator and new agent)
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}
	deps.Executor = &stubExecutor{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Reverted:       false,
		Actions: []domain.ActionRecord{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"}, TargetPath: workspace + "/orchestrator.md", Taken: domain.TakenUpdated},
		},
	}}
	svc := app.New(deps)

	// Act
	summary, err := svc.UpdateWorkflows(context.Background(), newWorkflowsOnlyUpdateReq(workspace))
	if err != nil {
		t.Fatalf("UpdateWorkflows error = %v; want nil", err)
	}

	// Assert — summary must not be empty; at minimum it has the orchestrator action
	if len(summary.Actions) == 0 {
		t.Error("RunSummary.Actions is empty after a successful run; " +
			"the executor's action records must be included in the summary so the user " +
			"can see what was written during the run")
	}
}

// ---------------------------------------------------------------------------
// T4.4 — UpdateWorkflows opts into atomic execution
// ---------------------------------------------------------------------------

// TestUpdateWorkflows_SetsAtomicTrue_OnExecRequest verifies that UpdateWorkflows passes
// Atomic:true to the executor so the executor's all-or-nothing reversal mode is engaged.
// This is the primary wiring task for Stage 4: without Atomic:true, the executor uses
// partial-failure semantics and a mid-plan write failure leaves the workspace in an
// intermediate state rather than restoring it.
//
// TDD RED: the current workflow_update.go does not set Atomic on the ExecRequest, so this
// test fails with spy.capturedReq.Atomic == false.
func TestUpdateWorkflows_SetsAtomicTrue_OnExecRequest(t *testing.T) {
	// Arrange — capture the full ExecRequest so we can inspect Atomic
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Planner = &stubPlanner{plan: orchestratorOnlyPlan(workspace)}

	spy := &execRequestSpy{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Reverted:       false,
	}}
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowsOnlyUpdateReq(workspace))

	// Assert — the ExecRequest must have Atomic:true
	if spy.capturedReq == nil {
		t.Fatal("executor was not called; UpdateWorkflows must invoke the executor")
	}
	if !spy.capturedReq.Atomic {
		t.Error("ExecRequest.Atomic is false; " +
			"UpdateWorkflows must set Atomic:true on the ExecRequest to engage the executor's " +
			"all-or-nothing reversal mode — without this flag, a mid-plan failure leaves the " +
			"workspace in an intermediate state instead of restoring it")
	}
}

// TestUpdateWorkflows_SetsAtomicTrue_WhenNewAgentsPresent verifies that UpdateWorkflows still
// sets Atomic:true when the plan contains newly-required agents. The atomic flag must be
// unconditional — new-agent runs are arguably more important to reverse because they create
// files that would not otherwise exist if the orchestrator update fails.
//
// TDD RED: the current implementation does not set Atomic at all.
func TestUpdateWorkflows_SetsAtomicTrue_WhenNewAgentsPresent(t *testing.T) {
	// Arrange — test-runner.md absent (new agent); capture the ExecRequest
	stub := interactiontest.NewBuilder().
		AnswerSelectOne(domain.QTierModel, "HIGH", "model-a").
		AnswerReview(true).
		Build()
	deps, workspace := newBaseDeps(t, stub)
	// Do not write test-runner.md — absent, classified as new
	deps.Planner = &stubPlanner{plan: newAgentPlan(workspace)}

	spy := &execRequestSpy{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Reverted:       false,
		Actions: []domain.ActionRecord{
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "orchestrator"}, TargetPath: workspace + "/orchestrator.md", Taken: domain.TakenUpdated},
			{Ref: domain.ArtifactRef{Kind: domain.ArtifactAgent, Key: "test-runner"}, TargetPath: workspace + "/test-runner.md", Taken: domain.TakenCreated},
		},
	}}
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newWorkflowsOnlyUpdateReq(workspace))

	// Assert
	if spy.capturedReq == nil {
		t.Fatal("executor was not called; UpdateWorkflows must call the executor even with new agents")
	}
	if !spy.capturedReq.Atomic {
		t.Error("ExecRequest.Atomic is false when the plan contains new agents; " +
			"Atomic:true must be set unconditionally by UpdateWorkflows — " +
			"new-agent runs are especially important to reverse, because a failed orchestrator " +
			"update would otherwise leave a half-deployed workspace")
	}
}

// TestUpdateWorkflows_SetsAtomicTrue_WhenNewSkillPresent verifies that UpdateWorkflows sets
// Atomic:true even when the plan contains new skill items alongside new agent items. The flag
// must be unconditional regardless of plan composition.
//
// TDD RED: the current implementation does not set Atomic at all.
func TestUpdateWorkflows_SetsAtomicTrue_WhenNewSkillPresent(t *testing.T) {
	// Arrange — skill-user.md and lean-tdd.md both absent; capture the ExecRequest
	stub := interactiontest.NewBuilder().AnswerReview(true).Build()
	deps, workspace := newBaseDeps(t, stub)
	deps.Catalog = newSkillEnabledCatalog()
	deps.Planner = &stubPlanner{plan: newSkillDeployPlan(workspace)}

	spy := &execRequestSpy{result: deploy.ExecResult{
		DeploymentRoot: workspace,
		Reverted:       false,
	}}
	deps.Executor = spy
	svc := app.New(deps)

	// Act
	_, _ = svc.UpdateWorkflows(context.Background(), newSkillWorkflowRequest(workspace, "stub-harness"))

	// Assert
	if spy.capturedReq == nil {
		t.Fatal("executor was not called; UpdateWorkflows must call the executor with skill items present")
	}
	if !spy.capturedReq.Atomic {
		t.Error("ExecRequest.Atomic is false when the plan contains skill items; " +
			"Atomic:true must be set unconditionally — " +
			"a run that creates both an agent file and a skill file must be all-or-nothing")
	}
}
