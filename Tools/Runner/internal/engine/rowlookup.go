package engine

import (
	"fmt"

	"mosaic-run/internal/domain"
)

// findCurrentRowIndex returns the routing table row index that was last dispatched,
// derived from the artifact state.
//
// A single-row agent, or a non-EXECUTION row, resolves by agent and phase.
// An agent that fills several EXECUTION rows resolves from the row recorded in
// the Execution Log for the step that just ran.
//
// It returns (-1, nil) when the row simply cannot be identified from the state
// (no matching agent+phase row) — an ambiguity, not a contract violation.
//
// It returns (-1, err) when a multi-row agent has no log entry, the entry
// records no row, or the recorded row fails validation against the table. The
// error is propagated verbatim, never collapsed into -1 and never replaced by
// a guessed row.
func findCurrentRowIndex(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	state domain.ArtifactState,
) (int, error) {
	agentName := extractAgentName(state.CurrentState.LastAgent)
	phase := state.CurrentState.Phase

	// An agent that is not a routing table participant at all is not an
	// ambiguity to fall back on -- it is an unresolvable position, and the
	// stop must name this as the cause (AC3.7). After the Apply fix,
	// CurrentState.LastAgent always names a workflow participant in a
	// correctly-recorded artifact, so reaching this branch means the
	// recorded agent genuinely is not one.
	if !isWorkflowParticipant(workflow, agentName) {
		return -1, &domain.PositionUnresolvedError{
			AgentInstance: state.CurrentState.LastAgent,
			Phase:         phase,
			Stage:         state.CurrentState.Stage,
			Cause:         domain.CauseAgentNotInWorkflow,
		}
	}

	// Non-EXECUTION row: find by agent name + phase (unique per phase in supported workflows).
	if !isExecutionPhase(phase) {
		for _, row := range workflow.Table.Rows {
			if row.Agent == agentName && row.Phase == phase {
				return row.Index, nil
			}
		}
		return -1, nil
	}

	// EXECUTION row: collect all matching rows.
	var matches []int
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName && row.PhaseParsed.IsStaged {
			matches = append(matches, row.Index)
		}
	}
	if len(matches) == 0 {
		return -1, nil
	}
	if len(matches) == 1 {
		// Unique agent in EXECUTION — no disambiguation needed.
		return matches[0], nil
	}

	// Multiple EXECUTION rows for this agent (e.g. build-review appears in both
	// the test group and the implementation group). The row is the one recorded
	// in the Execution Log for the step that just ran, validated against the table.
	return recordedRowForAgent(workflow, state.ExecutionLog, state.CurrentState.LastAgent)
}

// findRowForLogEntry returns the row index corresponding to an execution log entry.
//
// A single-row EXECUTION agent, or a non-EXECUTION entry, resolves by agent and
// phase. Non-EXECUTION resolution assumes an agent appears at most once per
// non-EXECUTION phase (true for every supported workflow).
//
// An agent that fills several EXECUTION rows resolves from the row recorded on
// the entry, validated with the same helper live routing uses. A missing or
// invalid recorded row returns an error naming the agent and row; no row is
// ever guessed.
func findRowForLogEntry(
	workflow domain.AdmittedWorkflow,
	entry domain.ExecutionLogEntry,
) (int, error) {
	agentName := extractAgentName(entry.Agent)
	if !isExecutionPhase(entry.Phase) {
		for _, row := range workflow.Table.Rows {
			if row.Agent == agentName && row.Phase == entry.Phase {
				return row.Index, nil
			}
		}
		return -1, fmt.Errorf("row not found for log entry agent=%q phase=%q", entry.Agent, entry.Phase)
	}

	matchCount := 0
	firstMatch := -1
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName && row.PhaseParsed.IsStaged {
			if matchCount == 0 {
				firstMatch = row.Index
			}
			matchCount++
		}
	}
	switch matchCount {
	case 0:
		return -1, fmt.Errorf("row not found for log entry agent=%q phase=%q", entry.Agent, entry.Phase)
	case 1:
		return firstMatch, nil
	}
	return validateRecordedRow(workflow, entry)
}

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
// A CNA row directly following a CNA row of the same agent is a re-dispatch of
// the same iteration (for example after a rejected result) and is not counted
// again.
func reviewLoopLimitReached(state domain.ArtifactState) bool {
	limit := state.ReviewLoopLimit
	if limit <= 0 {
		return false
	}
	reviewer := extractAgentName(state.CurrentState.LastAgent)
	count := 0
	for i, e := range state.ExecutionLog {
		if e.Status != domain.StatusCOMPLETED_NEEDS_ACTION ||
			e.Phase != state.CurrentState.Phase ||
			e.Stage != state.CurrentState.Stage ||
			extractAgentName(e.Agent) != reviewer {
			continue
		}
		if i > 0 {
			prev := state.ExecutionLog[i-1]
			if prev.Status == domain.StatusCOMPLETED_NEEDS_ACTION &&
				extractAgentName(prev.Agent) == reviewer {
				continue
			}
		}
		count++
	}
	return count >= limit
}
