package engine_test

import (
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
)

// ===== D1 fix: RowNotInGroupError when row not in any ordered group =====
//
// computeNextFromExecution used to return a -1 sentinel when the current row
// was not found in any group of the resolved stage, causing an out-of-bounds
// panic in the caller (buildDispatchStep). These tests verify that after the
// fix the engine returns a StopDecision with a diagnostic message instead.
//
// Scenario: Tests-Only approach limits orderedGroupsForStage to the Test group
// only ([7,9) in brownfield-tdd). When the recorded state places an
// Implementation group row (implementation-tdd, row 9) as the current row,
// that row does not appear in any ordered group, triggering the error.

// creatorInjectionContent is a minimal EXECUTION-only workflow used to exercise
// the FR-7/FR-8 creator-artifact injection path. test-writer-tdd's output
// artifact (Stage-{StageNumber}/PlanProgress.md) is intentionally absent from
// its input artifact list so that creator-artifact injection produces a
// genuinely new entry that would not be present without it.
const creatorInjectionContent = `## Creator Injection Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/impl.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
`

// brownfieldTDDAgents returns a full agent map for brownfield-tdd rows.
func brownfieldTDDAgents() map[string]domain.AgentReference {
	return newTestAgents(
		"codebase-research", "requirements-refinement", "requirements-review",
		"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
		"test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review",
		"test-runner",
	)
}

// TestNext_RowNotInGroup_ReturnsStopDecision verifies that when the current
// EXECUTION row is not found in any ordered group of the resolved stage,
// handleExecutionSuccess returns a StopDecision rather than panicking via
// an out-of-bounds slice access on the -1 sentinel row index.
//
// Setup: brownfield-tdd with Tests-Only approach (only the Test group is
// ordered for stage 1: rows [7,9)). State records implementation-tdd (row 9)
// as the last completed agent. Row 9 is outside [7,9), so no group contains
// it -- the "row not in group" condition.
func TestNext_RowNotInGroup_ReturnsStopDecision(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "2.0")
	// Tests-Only: orderedGroupsForStage returns only the Test group [7,9).
	stages := singleStageSet("Tests-Only")
	agents := brownfieldTDDAgents()
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#5", domain.StatusSUCCESS, 5)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("implementation-tdd#5"),
		Agents:       agents,
		Seq:          5,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	// Must return StopDecision, not panic from buildDispatchStep(-1).
	stop := requireStop(t, dec)
	if stop.Reason == "" {
		t.Error("StopDecision.Reason must be non-empty for diagnostics")
	}
}

// TestNext_RowNotInGroup_StopReasonContainsDiagnostics verifies that the
// StopDecision reason produced when the current row is not in any ordered
// group includes the row index, the stage number, and group information so
// the failure is diagnosable without reproducing the run.
func TestNext_RowNotInGroup_StopReasonContainsDiagnostics(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "2.0")
	stages := singleStageSet("Tests-Only")
	agents := brownfieldTDDAgents()
	// implementation-tdd is row 9; stage 1 with Tests-Only has only [7,9).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#5", domain.StatusSUCCESS, 5)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("implementation-tdd#5"),
		Agents:       agents,
		Seq:          5,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	// Reason must mention the row index (9), the stage (1), and at least one
	// group so the caller can diagnose the routing failure.
	for _, want := range []string{"9", "1"} {
		if !strings.Contains(stop.Reason, want) {
			t.Errorf("StopDecision.Reason %q does not contain expected diagnostic %q", stop.Reason, want)
		}
	}
}

// TestNext_RowNotInGroup_ErrorIsRowNotInGroupError verifies that the underlying
// error produced by computeNextFromExecution is a *domain.RowNotInGroupError
// whose fields identify the failing row and stage precisely.
//
// Because Next embeds the error message in StopDecision.Reason, we verify
// field-level diagnostics by constructing the expected error and comparing
// its Error() string against the stop reason (equivalent to asserting the
// structured fields without coupling to message format beyond key identifiers).
func TestNext_RowNotInGroup_ErrorIsRowNotInGroupError(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "2.0")
	stages := singleStageSet("Tests-Only")
	agents := brownfieldTDDAgents()
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#5", domain.StatusSUCCESS, 5)

	dec := engine.Next(engine.NextInput{
		Workflow:     aw,
		Stages:       stages,
		State:        state,
		LastResponse: successResponse("implementation-tdd#5"),
		Agents:       agents,
		Seq:          5,
		Now:          fixedNow,
		Mode:         domain.ExecutionModeAutoReview,
	})

	stop := requireStop(t, dec)
	// The domain error message format from RowNotInGroupError.Error() includes
	// the stage number and the row index. Verify both appear in the reason.
	// This also confirms the error type (not a generic string) was used.
	if !strings.Contains(stop.Reason, "stage 1") {
		t.Errorf("StopDecision.Reason %q: expected stage number in RowNotInGroupError format", stop.Reason)
	}
	if !strings.Contains(stop.Reason, "row 9") {
		t.Errorf("StopDecision.Reason %q: expected row index in RowNotInGroupError format", stop.Reason)
	}
}

// ===== FR-1b fix: ResumePoint errors on RowNotInGroupError (non-complete workflow) =====

// TestResumePoint_RowNotInGroup_NonCompleteWorkflow_ReturnsError verifies that
// when computeNextFromExecution returns a *domain.RowNotInGroupError (because
// the current EXECUTION row is not in any ordered group), ResumePoint wraps
// it and returns an error rather than silently treating the situation as
// end-of-run (the pre-fix behavior via the adv.RowIndex < 0 branch).
//
// Before the fix: computeNextFromExecution returns {RowIndex:-1, nil} and
// ResumePoint falls through to the adv.RowIndex < 0 guard at line 308,
// returning a spurious end-of-run ResumeInfo with no error.
// After the fix: computeNextFromExecution returns *RowNotInGroupError and
// ResumePoint wraps it via the existing advErr != nil guard (lines 304-306).
func TestResumePoint_RowNotInGroup_NonCompleteWorkflow_ReturnsError(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "2.0")
	stages := singleStageSet("Tests-Only")
	// implementation-tdd (row 9) is not in the Test-Only ordered group [7,9).
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#5", domain.StatusSUCCESS, 5)

	_, err := engine.ResumePoint(aw, stages, state, nil)
	if err == nil {
		t.Fatal("want error when EXECUTION row is not in any ordered group of a non-complete workflow, got nil")
	}
	// The error must wrap *domain.RowNotInGroupError so callers can inspect it.
	var rnigErr *domain.RowNotInGroupError
	if !errors.As(err, &rnigErr) {
		t.Errorf("want error wrapping *domain.RowNotInGroupError, got %T: %v", err, err)
	}
}

// TestResumePoint_RowNotInGroup_ErrorMessageContainsDiagnostics verifies that
// the error returned by ResumePoint when a row is not in any ordered group
// contains enough context to diagnose the failure: the row index and stage.
func TestResumePoint_RowNotInGroup_ErrorMessageContainsDiagnostics(t *testing.T) {
	aw := mustParseAndAdmit(t, brownfieldTDDContent, "brownfield-tdd", "2.0")
	stages := singleStageSet("Tests-Only")
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-tdd#5", domain.StatusSUCCESS, 5)

	_, err := engine.ResumePoint(aw, stages, state, nil)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"9", "1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain expected diagnostic %q (row index or stage)", msg, want)
		}
	}
}

// TestResumePoint_CompleteWorkflow_EndOfRunBehaviorUnchanged verifies that
// when the workflow is genuinely complete (computeNextFromExecution returns
// adv.Complete=true), ResumePoint still returns the end-of-run info with no
// error. The D1/FR-1b fix must not disturb this path.
//
// Setup: onSuccessDivergentContent has no post-execution rows. After the last
// EXECUTION row (implementation-review, row 3) completes, computeNextFromExecution
// exhausts all groups, all stages, and all post-execution rows, returning
// {Complete: true}. ResumePoint should return (ResumeInfo{RowIndex=4}, nil).
func TestResumePoint_CompleteWorkflow_EndOfRunBehaviorUnchanged(t *testing.T) {
	aw := mustParseAndAdmit(t, onSuccessDivergentContent, "on-success-divergent", "1.0")
	// No groups declared; any approach works since groups are implicit.
	stages := singleStageSet("Implementation-Only")
	agents := newTestAgents("test-writer-tdd", "tests-review-tdd", "implementation-tdd", "implementation-review")
	_ = agents // agents not needed for ResumePoint; declared for clarity
	// implementation-review is the last EXECUTION row (row 3); no post-execution rows.
	state := stateAfter("EXECUTION.[StageNumber]", "Stage-1", "implementation-review#4", domain.StatusSUCCESS, 4)

	info, err := engine.ResumePoint(aw, stages, state, nil)
	if err != nil {
		t.Fatalf("want nil error for complete workflow, got: %v", err)
	}
	// RowIndex should be len(rows) = 4, signaling end-of-run.
	wantRowIndex := 4 // len(workflow.Table.Rows) for onSuccessDivergentContent
	if info.RowIndex != wantRowIndex {
		t.Errorf("complete workflow: want RowIndex=%d (end-of-run), got %d", wantRowIndex, info.RowIndex)
	}
	if info.RerunLast {
		t.Error("complete workflow: want RerunLast=false, got true")
	}
}

// ===== FR-7/FR-8: Creator-artifact injection on review loop-back =====
//
// On auto-review COMPLETED_NEEDS_ACTION auto-route-back, the engine must inject
// the target (creator) agent's own previously-produced output artifacts from
// NextInput.ArtifactRegistry into the dispatched step's InputArtifacts,
// alongside the existing review-artifact injection.
//
// The comparison is against step.Request.OutputArtifacts (the resolved, bare
// paths from buildDispatchStep), not against raw row.OutputArtifacts (which
// may contain unresolved template tokens).
//
// creatorInjectionContent: test-writer-tdd's output is Stage-{StageNumber}/PlanProgress.md
// (resolved: Stage-1/PlanProgress.md). Its input list does NOT include this
// artifact, so injection adds a genuinely new entry to the dispatched step.

// TestNext_CreatorArtifactInjection_RegistryPresent_InjectsOutputArtifact
// verifies that when ArtifactRegistry contains an entry matching the target
// row's resolved output artifact path, that path is injected into the
// dispatched step's InputArtifacts.
//
// Scenario: tests-review-tdd returns CNA with OnFindings=test-writer-tdd.
// ArtifactRegistry carries Stage-1/PlanProgress.md (test-writer-tdd's prior
// output, bare path as session would supply after StripRunPrefix). After
// the existing review-artifact injection, the creator output must also appear.
