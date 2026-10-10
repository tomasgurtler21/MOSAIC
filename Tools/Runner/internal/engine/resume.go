package engine

import (
	"fmt"

	"mosaic-run/internal/domain"
)

// ResumePoint determines where to continue from a parsed artifact.
// Pure function -- no I/O, no side effects.
//
// Position is resolved from the last *workflow* entry in the execution log:
// trailing infrastructure entries are skipped, in sequence order, using the
// recognition rule (agent absent from workflow.Table, present in infra).
// infra may be nil, meaning no infrastructure agents are declared.
//
// If the artifact has no execution log entries, or none that is a workflow
// entry, returns the first row.
// If the last *workflow* logged invocation completed cleanly (its agent
// matches current_state.last_agent), returns the row after it. Trailing
// infrastructure entries appearing after it are not an interruption.
// If a mismatch is detected between the last workflow execution log entry
// and current_state, the last workflow step was interrupted in-flight and
// must be re-dispatched (ResumeInfo.RerunLast = true, FR-33).
//
// Returns *domain.PositionUnresolvedError when the position cannot be
// determined.
func ResumePoint(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	state domain.ArtifactState,
	infra domain.InfraAgentSet,
) (domain.ResumeInfo, error) {
	if len(state.ExecutionLog) == 0 {
		// Fresh start: resume from the first row.
		return domain.ResumeInfo{
			RowIndex:    0,
			Phase:       firstRowPhase(workflow),
			Stage:       "",
			StageNumber: 0,
			GroupIndex:  -1,
			Seq:         0,
			RerunLast:   false,
		}, nil
	}

	lastWorkflowEntry, found, findErr := lastWorkflowLogEntry(workflow, state.ExecutionLog, infra)
	if findErr != nil {
		return domain.ResumeInfo{}, fmt.Errorf("resume: %w", findErr)
	}
	if !found {
		// The log holds no workflow entry at all (only infrastructure activity
		// on record so far): resume from the first row, as if fresh.
		return domain.ResumeInfo{
			RowIndex:    0,
			Phase:       firstRowPhase(workflow),
			Stage:       "",
			StageNumber: 0,
			GroupIndex:  -1,
			Seq:         0,
			RerunLast:   false,
		}, nil
	}

	// Interruption detection: the last WORKFLOW entry doesn't match
	// CurrentState. Trailing infrastructure entries appearing after it are
	// not an interruption.
	interrupted := lastWorkflowEntry.Agent != state.CurrentState.LastAgent

	if interrupted {
		// The last workflow log entry was dispatched but not recorded in
		// CurrentState. Re-run the interrupted row.
		pos, err := resumePosition(workflow, lastWorkflowEntry)
		if err != nil {
			return domain.ResumeInfo{}, err
		}
		rowIdx := pos.rowIdx
		row := workflow.Table.Rows[rowIdx]
		stageNum := pos.stageNum
		groupIdx := -1
		if row.PhaseParsed.IsStaged {
			groupIdx = findGroupIndexInWorkflow(workflow, rowIdx)
		}
		return domain.ResumeInfo{
			RowIndex:    rowIdx,
			Phase:       row.Phase,
			Stage:       lastWorkflowEntry.Stage,
			StageNumber: stageNum,
			GroupIndex:  groupIdx,
			Seq:         state.GlobalSequence,
			RerunLast:   true,
		}, nil
	}

	// Clean completion: advance to the next row after the last workflow step.
	pos, err := resumePosition(workflow, lastWorkflowEntry)
	if err != nil {
		return domain.ResumeInfo{}, err
	}
	currentRowIdx := pos.rowIdx

	currentRow := workflow.Table.Rows[currentRowIdx]

	if !currentRow.PhaseParsed.IsStaged {
		return resumeAfterNonExecution(workflow, stages, currentRowIdx, state.GlobalSequence), nil
	}

	// EXECUTION row: apply group/stage logic.
	currentStageNum := pos.stageNum
	adv, advErr := computeNextFromExecution(workflow, stages, currentRowIdx, currentStageNum)
	if advErr != nil {
		return domain.ResumeInfo{}, fmt.Errorf("resume: %w", advErr)
	}

	if adv.Complete {
		return domain.ResumeInfo{
			RowIndex:    len(workflow.Table.Rows),
			Phase:       "",
			Stage:       "",
			StageNumber: 0,
			GroupIndex:  -1,
			Seq:         state.GlobalSequence,
			RerunLast:   false,
		}, nil
	}
	// Defense-in-depth: unreachable after RowNotInGroupError replaced the -1
	// sentinel in computeNextFromExecution. Retained as a defensive guard only.
	if adv.RowIndex < 0 {
		return domain.ResumeInfo{
			RowIndex:    len(workflow.Table.Rows),
			Phase:       "",
			Stage:       "",
			StageNumber: 0,
			GroupIndex:  -1,
			Seq:         state.GlobalSequence,
			RerunLast:   false,
		}, nil
	}

	nextRow := workflow.Table.Rows[adv.RowIndex]
	nextGroupIdx := -1
	if nextRow.PhaseParsed.IsStaged {
		nextGroupIdx = findGroupIndexInWorkflow(workflow, adv.RowIndex)
	}

	return domain.ResumeInfo{
		RowIndex:    adv.RowIndex,
		Phase:       nextRow.Phase,
		Stage:       adv.StageString,
		StageNumber: adv.StageNumber,
		GroupIndex:  nextGroupIdx,
		Seq:         state.GlobalSequence,
		RerunLast:   false,
	}, nil
}

// lastWorkflowLogEntry returns the most recent execution log entry produced
// by a workflow participant, skipping any trailing infrastructure entries
// (recognised by derivation: absent from the routing table, present in infra).
//
// found is false when the log holds no workflow entry at all -- only
// infrastructure activity is on record so far.
//
// Returns *domain.PositionUnresolvedError when a trailing entry is neither a
// workflow participant nor a recognised infrastructure agent: the position
// cannot be determined in that case. infra may be nil, meaning no
// infrastructure agents are declared.
func lastWorkflowLogEntry(
	workflow domain.AdmittedWorkflow,
	log []domain.ExecutionLogEntry,
	infra domain.InfraAgentSet,
) (entry domain.ExecutionLogEntry, found bool, err error) {
	for i := len(log) - 1; i >= 0; i-- {
		e := log[i]
		agentName := extractAgentName(e.Agent)
		if isWorkflowParticipant(workflow, agentName) {
			return e, true, nil
		}
		if infra != nil && infra.IsInfrastructureAgent(e.Agent) {
			continue // trailing infrastructure entry: keep looking
		}
		return domain.ExecutionLogEntry{}, false, &domain.PositionUnresolvedError{
			AgentInstance: e.Agent,
			Phase:         e.Phase,
			Stage:         e.Stage,
			Cause:         domain.CauseAgentNotInWorkflow,
		}
	}
	return domain.ExecutionLogEntry{}, false, nil
}

// isWorkflowParticipant reports whether agentName names a routing table
// participant for this workflow, in any row.
func isWorkflowParticipant(workflow domain.AdmittedWorkflow, agentName string) bool {
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName {
			return true
		}
	}
	return false
}

// noStageSetReason builds the stop reason for a staged row reached with no
// stage set available. base is the generic message for the call site
// (distinct wording for initial dispatch vs. entering EXECUTION from a
// prior row). When stageSource is the zero value ("not stated"), base is
// returned unchanged. Otherwise the message names the path the stage table
// was looked for, and distinguishes "was supposed to be seeded here and is
// missing" from "this run never had one" via Seeded.
func noStageSetReason(base string, stageSource domain.StageSource) string {
	if stageSource == (domain.StageSource{}) {
		return base
	}
	if stageSource.Seeded {
		return fmt.Sprintf("%s: a stage table was seeded at %s but is missing", base, stageSource.Path)
	}
	return fmt.Sprintf("%s: no stage table was found at %s", base, stageSource.Path)
}
