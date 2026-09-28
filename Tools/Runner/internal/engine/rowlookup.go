package engine

import (
	"fmt"

	"mosaic-run/internal/domain"
)

// findCurrentRowIndex returns the routing table row index that was last dispatched,
// derived from the artifact state.
//
// It returns (-1, nil) when the row simply cannot be identified from the state
// (no matching agent+phase row, unparseable stage number) — an ambiguity, not a
// contract violation.
//
// It returns (-1, err) when the row would have been identified by sequence
// arithmetic but that arithmetic could not run (e.g. unresolvable approach).
// The error is propagated verbatim, never collapsed into -1.
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
	// the test group and the implementation group). Use seq-based position to
	// determine which row was last dispatched. The computation accounts for
	// per-stage approach variation so mixed-approach workflows are handled correctly.
	stageNum := parseStageNumber(state.CurrentState.Stage)
	if stageNum == 0 {
		return -1, nil
	}
	rowIdx, err := findExecutionRowBySeq(workflow, stages, stageNum, state.GlobalSequence, matches)
	if err != nil {
		return -1, err // propagate, do not collapse to ambiguous -1
	}
	return rowIdx, nil
}

// findRowForLogEntry returns the row index corresponding to an execution log entry.
// Uses agent+phase for unique agents, falling back to seq-based position for
// agents that appear multiple times in EXECUTION rows.
func findRowForLogEntry(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
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

	// EXECUTION: try unique match first.
	var matches []int
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName && row.PhaseParsed.IsStaged {
			matches = append(matches, row.Index)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return -1, fmt.Errorf("row not found for log entry agent=%q phase=%q", entry.Agent, entry.Phase)
	}

	// Multiple matches: use seq-based position.
	stageNum := parseStageNumber(entry.Stage)
	if stageNum == 0 {
		return -1, fmt.Errorf("cannot parse stage number from %q", entry.Stage)
	}
	rowIdx, seqErr := findExecutionRowBySeq(workflow, stages, stageNum, entry.Seq, matches)
	if seqErr != nil {
		return -1, seqErr // propagate approach error verbatim
	}
	if rowIdx < 0 {
		return -1, fmt.Errorf("cannot map seq=%d in stage %q to a routing row", entry.Seq, entry.Stage)
	}
	return rowIdx, nil
}

// findExecutionRowBySeq maps a global sequence number inside a given stage
// to a routing table row index among the candidate rows in matches, using the
// approach-ordered group structure.
//
// The naive computation (seq minus everything dispatched before this stage)
// assumes one seq increment per active row. An interleaved infrastructure
// invocation within the stage also consumes a seq number, which only ever
// inflates this position, never deflates it. matches (the routing rows that
// share the target agent) is therefore searched for the candidate with the
// largest position not exceeding the computed position -- the correct
// candidate whether or not an infrastructure step landed in between.
//
// Previous stages may have used different approaches (and therefore different
// active row counts). The offset is computed by summing the actual row counts
// across all previous stages rather than assuming a uniform rows-per-stage value.
//
// Returns an error when any stage at or before stageNum has an unresolvable
// approach, since the row-count arithmetic depends on every prior stage's
// ordered sequence.
func findExecutionRowBySeq(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	stageNum domain.StageNumber,
	seq int,
	matches []int,
) (int, error) {
	ordGroups, err := orderedGroupsForStage(workflow, stages, stageNum)
	if err != nil {
		return -1, err
	}

	preExecCount := workflow.PreExecutionEndRow - workflow.PreExecutionStartRow

	// Sum the actual row counts contributed by all stages before stageNum.
	// Each stage may use a different approach, so row counts may differ.
	previousRowsTotal := 0
	if stages != nil {
		for _, entry := range stages.Entries {
			if entry.Number >= stageNum {
				break
			}
			prevGroups, prevErr := orderedGroupsForStage(workflow, stages, entry.Number)
			if prevErr != nil {
				return -1, prevErr
			}
			previousRowsTotal += countActiveRows(prevGroups)
		}
	}

	rowsThisStage := countActiveRows(ordGroups)
	if rowsThisStage == 0 {
		return -1, nil
	}

	// 1-indexed position within the current stage.
	posInStage := seq - preExecCount - previousRowsTotal
	if posInStage <= 0 {
		return -1, nil
	}

	matchSet := make(map[int]bool, len(matches))
	for _, m := range matches {
		matchSet[m] = true
	}

	// Walk the ordered groups assigning each row its 1-indexed position, and
	// track the candidate (from matches) with the largest position not
	// exceeding posInStage. Fall back to the earliest candidate when
	// posInStage falls before every candidate's true position.
	bestRow, bestPos := -1, -1
	firstRow, firstPos := -1, -1
	pos := 0
	for _, g := range ordGroups {
		for r := g.StartRow; r < g.EndRow; r++ {
			pos++
			if !matchSet[r] {
				continue
			}
			if firstRow < 0 || pos < firstPos {
				firstRow, firstPos = r, pos
			}
			if pos <= posInStage && pos > bestPos {
				bestRow, bestPos = r, pos
			}
		}
	}
	if bestRow >= 0 {
		return bestRow, nil
	}
	return firstRow, nil
}

// findFirstRowForAgent returns the index of the first routing table row
// whose Agent matches the given identifier.
func findFirstRowForAgent(workflow domain.AdmittedWorkflow, agentName string) int {
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName {
			return row.Index
		}
	}
	return -1
}

// findFirstNonExecutionRowForAgent returns the index of the first non-EXECUTION
// row whose Agent matches the given identifier.
func findFirstNonExecutionRowForAgent(workflow domain.AdmittedWorkflow, agentName string) int {
	for _, row := range workflow.Table.Rows {
		if row.Agent == agentName && !row.PhaseParsed.IsStaged {
			return row.Index
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
