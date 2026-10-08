package engine

import (
	"mosaic-run/internal/domain"
)

// findNearestPrecedingRowForAgent returns the index of the nearest routing table
// row above fromRow whose Agent matches the given identifier. Group and stage
// boundaries are disregarded. The row fromRow itself and rows below it are never
// candidates. It returns -1 when no row above matches.
func findNearestPrecedingRowForAgent(workflow domain.AdmittedWorkflow, fromRow int, agentName string) int {
	for i := min(fromRow, len(workflow.Table.Rows)) - 1; i >= 0; i-- {
		if workflow.Table.Rows[i].Agent == agentName {
			return workflow.Table.Rows[i].Index
		}
	}
	return -1
}

// findScopedRowForAgent resolves an On Success target agent to a row relative to
// fromRow. Non-EXECUTION rows are preferred: the nearest one after fromRow, else
// the nearest one before it. When the agent has no non-EXECUTION row, the same
// nearest-after-then-before rule applies to any row. It returns -1 when the
// agent has no row other than fromRow.
func findScopedRowForAgent(workflow domain.AdmittedWorkflow, fromRow int, agentName string) int {
	if idx := nearestRowForAgent(workflow, fromRow, agentName, true); idx >= 0 {
		return idx
	}
	return nearestRowForAgent(workflow, fromRow, agentName, false)
}

func nearestRowForAgent(workflow domain.AdmittedWorkflow, fromRow int, agentName string, nonExecutionOnly bool) int {
	rows := workflow.Table.Rows
	matches := func(i int) bool {
		return i != fromRow && rows[i].Agent == agentName &&
			!(nonExecutionOnly && rows[i].PhaseParsed.IsStaged)
	}
	for i := fromRow + 1; i < len(rows); i++ {
		if i >= 0 && matches(i) {
			return rows[i].Index
		}
	}
	for i := min(fromRow, len(rows)) - 1; i >= 0; i-- {
		if matches(i) {
			return rows[i].Index
		}
	}
	return -1
}

// findGroupIndexInWorkflow returns the index into aw.Groups that contains rowIdx,
// or -1 when the row is not in any execution group.
func findGroupIndexInWorkflow(workflow domain.AdmittedWorkflow, rowIdx int) int {
	for i, g := range workflow.Groups {
		if rowIdx >= g.StartRow && rowIdx < g.EndRow {
			return i
		}
	}
	return -1
}

// reviewLoopLimitReached reports whether the reviewer at the current position
// has produced as many COMPLETED_NEEDS_ACTION iterations at the current phase
// and stage as the review loop limit allows. A limit of 0 means no limit.
//
// The count is keyed by reviewer, phase and stage (not by workflow row) and only
// covers the rows since the reviewer's last SUCCESS at that phase and stage.
//
// A CNA row directly following a CNA row of the same agent is a re-dispatch of
// the same iteration (for example after a rejected result) and is not counted
// again. Likewise a reviewer SUCCESS directly following the same reviewer's CNA
// is a gate-discharging re-dispatch of that round, not a pass, and does not
// reset the count; only a reviewer SUCCESS that does not directly follow its
// own CNA does.
func reviewLoopLimitReached(state domain.ArtifactState, pos position) bool {
	limit := state.ReviewLoopLimit
	if limit <= 0 {
		return false
	}
	reviewer := extractAgentName(state.CurrentState.LastAgent)
	count := 0
	for i, e := range state.ExecutionLog {
		if (e.Status != domain.StatusCOMPLETED_NEEDS_ACTION && e.Status != domain.StatusSUCCESS) ||
			e.Phase != pos.entry.Phase ||
			e.Stage != pos.stage ||
			extractAgentName(e.Agent) != reviewer {
			continue
		}
		followsOwnCNA := i > 0 &&
			state.ExecutionLog[i-1].Status == domain.StatusCOMPLETED_NEEDS_ACTION &&
			extractAgentName(state.ExecutionLog[i-1].Agent) == reviewer
		switch {
		case e.Status == domain.StatusSUCCESS && !followsOwnCNA:
			count = 0
		case e.Status == domain.StatusCOMPLETED_NEEDS_ACTION && !followsOwnCNA:
			count++
		}
	}
	return count >= limit
}
