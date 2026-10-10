package engine

import "mosaic-run/internal/domain"

// PartiallyDoneRedispatchLimit is N, the Runner-only bound on mechanical
// re-dispatches of one assignment that returned PARTIALLY_DONE in the auto and
// auto-review modes. With k the length of the trailing PARTIALLY_DONE run at
// the last step's row and stage, the engine re-dispatches while
// k <= PartiallyDoneRedispatchLimit and deviates when k > it. The native
// policy has no cap; after the bound the orchestrator applies the unchanged
// policy with judgement.
const PartiallyDoneRedispatchLimit = 3

// E501AttemptLimit is the native Tier 1 budget: at most three BLOCKED/E501
// attempts per workflow row and stage since the last SUCCESS there.
const E501AttemptLimit = 3

// PartiallyDoneContextPrefix introduces the previous status message that a
// PARTIALLY_DONE re-dispatch carries in its task description.
const PartiallyDoneContextPrefix = "Your previous invocation of this assignment returned PARTIALLY_DONE. Its status message was:"

// E501BudgetRemaining returns E501AttemptLimit minus the number of BLOCKED log
// rows with ErrorCode E501 at (row, stage) after the last SUCCESS row at the
// same (row, stage); never negative. Rows at other rows or stages, rows with
// NoWorkflowRow, and BLOCKED rows with another or no error code are ignored.
// stage is the recorded group form ("" for non-staged rows).
func E501BudgetRemaining(log []domain.ExecutionLogEntry, row domain.WorkflowRow, stage string) int {
	used := 0
	for i := len(log) - 1; i >= 0; i-- {
		e := log[i]
		if e.WorkflowRow != row || e.Stage != stage {
			continue
		}
		if e.Status == domain.StatusSUCCESS {
			break
		}
		if e.Status == domain.StatusBLOCKED && e.ErrorCode == domain.ErrorTOOL_UNAVAILABLE {
			used++
		}
	}
	return max(E501AttemptLimit-used, 0)
}

// partiallyDoneRunLength is k: the length of the trailing run of
// PARTIALLY_DONE log rows at (row, stage). Rows without a recorded workflow
// row (infrastructure and ad-hoc steps) are skipped; any other row ends the run.
func partiallyDoneRunLength(log []domain.ExecutionLogEntry, row domain.WorkflowRow, stage string) int {
	k := 0
	for i := len(log) - 1; i >= 0; i-- {
		e := log[i]
		if e.WorkflowRow == domain.NoWorkflowRow {
			continue
		}
		if e.WorkflowRow != row || e.Stage != stage || e.Status != domain.StatusPARTIALLY_DONE {
			break
		}
		k++
	}
	return k
}

// mechanicalRetry decides the auto-mode re-dispatch of the same assignment
// after a PARTIALLY_DONE or BLOCKED/E501 step. ok is false when the step is not
// retryable or the bound or budget is used up, in which case the caller
// deviates. All counting comes from the Execution Log.
func mechanicalRetry(
	in NextInput,
	pos position,
	status domain.StatusCode,
) (dec domain.EngineDecision, ok bool) {
	row := domain.WorkflowRowFromIndex(pos.rowIdx)
	log := in.State.ExecutionLog
	var kind domain.RetryKind
	switch status {
	case domain.StatusPARTIALLY_DONE:
		if partiallyDoneRunLength(log, row, pos.stage) > PartiallyDoneRedispatchLimit {
			return domain.EngineDecision{}, false
		}
		kind = domain.RetryPartiallyDone
	case domain.StatusBLOCKED:
		if lastErrorCode(in, pos) != domain.ErrorTOOL_UNAVAILABLE ||
			E501BudgetRemaining(log, row, pos.stage) <= 0 {
			return domain.EngineDecision{}, false
		}
		kind = domain.RetryToolUnavailable
	default:
		return domain.EngineDecision{}, false
	}
	step, err := buildDispatchStep(in.Workflow, in.Stages, pos.rowIdx, pos.stageNum, pos.stage,
		in.Agents, in.Seq, in.Now, in.RefreshedStages)
	if err != nil {
		return domain.EngineDecision{Stop: &domain.StopDecision{Reason: err.Error()}}, true
	}
	step.Retry = kind
	if kind == domain.RetryPartiallyDone {
		addPartiallyDoneContext(&step, in, pos)
	}
	return domain.EngineDecision{Dispatch: &domain.DispatchDecision{Steps: []domain.DispatchStep{step}}}, true
}

// lastErrorCode is the error code of the last step: from the live response, or
// on resume from the recorded log entry (falling back to the current state).
func lastErrorCode(in NextInput, pos position) domain.ErrorCode {
	if in.LastResponse != nil {
		return in.LastResponse.ErrorCode
	}
	if pos.entry.ErrorCode != domain.ErrorNone {
		return pos.entry.ErrorCode
	}
	return in.State.CurrentState.ErrorCode
}

// addPartiallyDoneContext hands the previous output to the re-dispatched agent:
// the previous output artifacts join the inputs and the previous status message
// is appended to the task description.
func addPartiallyDoneContext(step *domain.DispatchStep, in NextInput, pos position) {
	msg := pos.entry.Summary
	if in.LastResponse != nil {
		msg = in.LastResponse.StatusMessage
	}
	step.Request.InputArtifacts = injectReviewArtifacts(in.State.RunID, step.Request.InputArtifacts, in.LastOutputArtifacts)
	step.Request.TaskDescription = domain.GenericTaskDescription + "\n\n" +
		PartiallyDoneContextPrefix + "\n" + msg
}
