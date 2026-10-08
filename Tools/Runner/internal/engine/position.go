package engine

import (
	"fmt"

	"mosaic-run/internal/domain"
)

// position is the workflow position derived from the Execution Log entry of
// the step that just ran. Live routing (Next) and resume (ResumePoint) both
// derive row and stage through resolvePosition, so they cannot disagree.
type position struct {
	entry    domain.ExecutionLogEntry
	rowIdx   int // zero-based routing table row; -1 when the entry cannot be identified
	stage    string
	stageNum domain.StageNumber
}

// currentPosition derives the position of the step that last ran from the
// artifact state. The entry is the last Execution Log entry of the exact
// instance recorded as LastAgent; when the log holds none, an entry built from
// the current state (which records no workflow row) stands in.
//
// It returns an error when the recorded agent is not a workflow participant,
// or when resolvePosition refuses the entry.
func currentPosition(workflow domain.AdmittedWorkflow, state domain.ArtifactState) (position, error) {
	cs := state.CurrentState
	if !isWorkflowParticipant(workflow, extractAgentName(cs.LastAgent)) {
		// Not an ambiguity to fall back on: the recorded agent genuinely is not
		// a participant, and the stop must name this as the cause.
		return position{rowIdx: -1}, &domain.PositionUnresolvedError{
			AgentInstance: cs.LastAgent,
			Phase:         cs.Phase,
			Stage:         cs.Stage,
			Cause:         domain.CauseAgentNotInWorkflow,
		}
	}
	return resolvePosition(workflow, lastEntryOfAgent(state))
}

// lastEntryOfAgent returns the last log entry whose Agent equals the exact
// instance id in CurrentState.LastAgent, or the synthetic entry built from
// CurrentState when there is none.
func lastEntryOfAgent(state domain.ArtifactState) domain.ExecutionLogEntry {
	cs := state.CurrentState
	for i := len(state.ExecutionLog) - 1; i >= 0; i-- {
		if state.ExecutionLog[i].Agent == cs.LastAgent {
			return state.ExecutionLog[i]
		}
	}
	return domain.ExecutionLogEntry{
		Agent:       cs.LastAgent,
		Phase:       cs.Phase,
		Stage:       cs.Stage,
		WorkflowRow: domain.NoWorkflowRow,
	}
}

// resolvePosition derives row and stage from one log entry.
//
// Whenever the entry records a workflow row, that row is the position for
// every row type, validated against the table. Only an entry that records no
// row falls back to resolving by agent and phase, in which case rowIdx is -1
// (with a nil error) when no row matches.
func resolvePosition(workflow domain.AdmittedWorkflow, entry domain.ExecutionLogEntry) (position, error) {
	if entry.WorkflowRow != domain.NoWorkflowRow {
		return recordedPosition(workflow, entry)
	}
	return fallbackPosition(workflow, entry)
}

// recordedPosition validates the row recorded on entry against the table: the
// row exists and holds the entry's agent. For a staged row the entry must also
// carry a stage whose group is the row's group.
func recordedPosition(workflow domain.AdmittedWorkflow, entry domain.ExecutionLogEntry) (position, error) {
	idx := entry.WorkflowRow.Index()
	if idx < 0 || idx >= len(workflow.Table.Rows) {
		return position{rowIdx: -1}, recordedRowError(entry, domain.CauseRecordedRowInvalid)
	}
	row := workflow.Table.Rows[idx]
	if row.Agent != extractAgentName(entry.Agent) {
		return position{rowIdx: -1}, recordedRowError(entry, domain.CauseRecordedRowInvalid)
	}
	pos := position{entry: entry, rowIdx: idx, stage: entry.Stage, stageNum: parseStageNumber(entry.Stage)}
	if !row.PhaseParsed.IsStaged {
		return pos, nil
	}
	if entry.Stage == "" {
		return position{rowIdx: -1}, &domain.PositionUnresolvedError{
			AgentInstance: entry.Agent,
			Phase:         entry.Phase,
			Stage:         entry.Stage,
			Cause:         domain.CauseStagedRowWithoutStage,
			RecordedRow:   entry.WorkflowRow,
			Group:         row.PhaseParsed.Group,
		}
	}
	stageGroup, _, ok := domain.ParseStageValue(entry.Stage)
	if !ok {
		return position{rowIdx: -1}, recordedRowError(entry, domain.CauseRecordedRowInvalid)
	}
	groupIdx := findGroupIndexInWorkflow(workflow, idx)
	if groupIdx < 0 || workflow.Groups[groupIdx].Name != stageGroup {
		return position{rowIdx: -1}, recordedRowError(entry, domain.CauseRecordedRowInvalid)
	}
	return pos, nil
}

// fallbackPosition resolves an entry that records no workflow row by agent and
// phase: a non-EXECUTION entry by the first row with that agent and phase, an
// EXECUTION entry by the agent's single staged row. An agent that fills several
// EXECUTION rows cannot be resolved without a recorded row.
func fallbackPosition(workflow domain.AdmittedWorkflow, entry domain.ExecutionLogEntry) (position, error) {
	agentName := extractAgentName(entry.Agent)
	pos := position{entry: entry, rowIdx: -1, stage: entry.Stage, stageNum: parseStageNumber(entry.Stage)}
	if !isExecutionPhase(entry.Phase) {
		for _, row := range workflow.Table.Rows {
			if row.Agent == agentName && row.Phase == entry.Phase {
				pos.rowIdx = row.Index
				return pos, nil
			}
		}
		return pos, nil
	}
	var matches []int
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName && row.PhaseParsed.IsStaged {
			matches = append(matches, row.Index)
		}
	}
	switch len(matches) {
	case 0:
		return pos, nil
	case 1:
		pos.rowIdx = matches[0]
		return pos, nil
	}
	return position{rowIdx: -1}, recordedRowError(entry, domain.CauseNoRecordedRow)
}

func recordedRowError(entry domain.ExecutionLogEntry, cause domain.PositionUnresolvedCause) error {
	return &domain.PositionUnresolvedError{
		AgentInstance: entry.Agent,
		Phase:         entry.Phase,
		Stage:         entry.Stage,
		Cause:         cause,
		RecordedRow:   entry.WorkflowRow,
	}
}

// resumePosition resolves the position of a workflow log entry for ResumePoint.
// An entry that cannot be identified at all is an error naming agent and phase;
// no row is ever guessed.
func resumePosition(workflow domain.AdmittedWorkflow, entry domain.ExecutionLogEntry) (position, error) {
	pos, err := resolvePosition(workflow, entry)
	if err == nil && pos.rowIdx < 0 {
		err = fmt.Errorf("row not found for log entry agent=%q phase=%q", entry.Agent, entry.Phase)
	}
	if err != nil {
		return position{rowIdx: -1}, fmt.Errorf("resume: %w", err)
	}
	return pos, nil
}

// enteringExecution reports the resume point for a run whose last step was a
// non-EXECUTION row and whose next row is a staged EXECUTION row: execution is
// entered at the first row of the first group of the first stage, exactly as
// live routing enters it. ok is false when the next row is not staged or the
// entry point cannot be computed, in which case the positional next row stands.
func enteringExecution(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	nextRow domain.RoutingRow,
) (info domain.ResumeInfo, ok bool) {
	if !nextRow.PhaseParsed.IsStaged || !workflow.HasStagedPhase || stages == nil || len(stages.Entries) == 0 {
		return domain.ResumeInfo{}, false
	}
	first := stages.Entries[0]
	groups, err := orderedGroupsForStage(workflow, stages, first.Number)
	if err != nil || len(groups) == 0 {
		return domain.ResumeInfo{}, false
	}
	rowIdx := groups[0].StartRow
	return domain.ResumeInfo{
		RowIndex:    rowIdx,
		Phase:       workflow.Table.Rows[rowIdx].Phase,
		Stage:       fmt.Sprintf("Stage-%d", first.Number),
		StageNumber: first.Number,
		GroupIndex:  findGroupIndexInWorkflow(workflow, rowIdx),
	}, true
}

// resumeAfterNonExecution is the resume point after a cleanly completed
// non-EXECUTION row: the row immediately after it, or the entry point of
// EXECUTION when that row is staged.
func resumeAfterNonExecution(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	currentRowIdx int,
	seq int,
) domain.ResumeInfo {
	nextRowIdx := currentRowIdx + 1
	if nextRowIdx >= len(workflow.Table.Rows) {
		return domain.ResumeInfo{RowIndex: nextRowIdx, GroupIndex: -1, Seq: seq}
	}
	nextRow := workflow.Table.Rows[nextRowIdx]
	if info, ok := enteringExecution(workflow, stages, nextRow); ok {
		info.Seq = seq
		return info
	}
	return domain.ResumeInfo{RowIndex: nextRowIdx, Phase: nextRow.Phase, GroupIndex: -1, Seq: seq}
}
