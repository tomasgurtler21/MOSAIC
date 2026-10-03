package engine

import (
	"mosaic-run/internal/domain"
)

// recordedRowForAgent returns the routing table row index recorded for the
// step that just ran: the row of the most recent Execution Log entry whose
// Agent equals the exact instance id lastAgent. Entries of other instances,
// infrastructure steps and non-participants are never consulted.
//
// It returns *domain.PositionUnresolvedError when no entry matches, the entry
// records no row, or the recorded row fails validateRecordedRow.
func recordedRowForAgent(
	workflow domain.AdmittedWorkflow,
	log []domain.ExecutionLogEntry,
	lastAgent string,
) (int, error) {
	for i := len(log) - 1; i >= 0; i-- {
		if log[i].Agent != lastAgent {
			continue
		}
		return validateRecordedRow(workflow, log[i])
	}
	return -1, &domain.PositionUnresolvedError{
		AgentInstance: lastAgent,
		Cause:         domain.CauseNoRecordedRow,
	}
}

// validateRecordedRow checks that the row recorded on entry still describes
// the step: the row exists in the table, holds the entry's agent, and lies in
// the group named by the entry's Stage. It returns the zero-based row index.
//
// The check catches a table edit that changed which agent or group sits at the
// recorded row number. An edit that leaves the same agent and group there is
// not detected.
func validateRecordedRow(
	workflow domain.AdmittedWorkflow,
	entry domain.ExecutionLogEntry,
) (int, error) {
	if entry.WorkflowRow == domain.NoWorkflowRow {
		return -1, recordedRowError(entry, domain.CauseNoRecordedRow)
	}
	idx := entry.WorkflowRow.Index()
	if idx < 0 || idx >= len(workflow.Table.Rows) {
		return -1, recordedRowError(entry, domain.CauseRecordedRowInvalid)
	}
	row := workflow.Table.Rows[idx]
	if row.Agent != extractAgentName(entry.Agent) {
		return -1, recordedRowError(entry, domain.CauseRecordedRowInvalid)
	}
	stageGroup, _, ok := domain.ParseStageValue(entry.Stage)
	if !ok {
		return -1, recordedRowError(entry, domain.CauseRecordedRowInvalid)
	}
	groupIdx := findGroupIndexInWorkflow(workflow, idx)
	if groupIdx < 0 || workflow.Groups[groupIdx].Name != stageGroup {
		return -1, recordedRowError(entry, domain.CauseRecordedRowInvalid)
	}
	return idx, nil
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
