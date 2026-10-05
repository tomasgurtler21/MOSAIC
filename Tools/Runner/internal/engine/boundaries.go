package engine

import (
	"fmt"

	"mosaic-run/internal/domain"
)

// orderedGroupsForStage returns the execution groups in the order they run for
// the given stage, resolved from the workflow's approach table.
//
// When the workflow declares no groups, the approach is ignored entirely and the
// workflow's single implicit group is returned without error.
//
// Returns *domain.UnresolvableApproachError when the stage's Approach value has
// no matching table row. It never falls back to the declared order, to a default
// approach, or to running every group.
func orderedGroupsForStage(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	stageNum domain.StageNumber,
) ([]domain.ExecutionGroup, error) {
	// No groups declared: ignore the approach entirely.
	if !workflow.GroupsDeclared {
		return workflow.Groups, nil
	}

	// Grouped workflow: look up the approach for this stage.
	if stages == nil {
		return nil, fmt.Errorf("stage %d: staged workflow requires a stage set but none is available", stageNum)
	}
	entry, ok := stages.Entry(stageNum)
	if !ok {
		return nil, fmt.Errorf("stage %d has no entry in stage set", stageNum)
	}

	// Look up the approach in the workflow's approach table.
	groupNames, found := workflow.ApproachTable.Sequence(entry.Approach)
	if !found {
		return nil, &domain.UnresolvableApproachError{
			WorkflowID: workflow.Table.Info.ID,
			Stage:      stageNum,
			Approach:   entry.Approach,
			Declared:   workflow.ApproachTable.Approaches(),
		}
	}

	// Map group names to execution groups in sequence order.
	result := make([]domain.ExecutionGroup, 0, len(groupNames))
	for _, name := range groupNames {
		g, ok := workflow.GroupByName(name)
		if !ok {
			// This should be unreachable for an admitted workflow (guaranteed by A4),
			// but surface an error rather than silently skipping.
			return nil, fmt.Errorf("approach %q references group %q, which no EXECUTION row declares", entry.Approach, name)
		}
		result = append(result, g)
	}
	return result, nil
}

// IsLastRowOfStage reports whether the routing table row at rowIdx is the last
// row dispatched for stageNum in the admitted workflow.
//
// The row is the last in its stage when it equals the final row of the last
// ordered group for that stage. Returns false when stageNum is 0 or when group
// ordering cannot be resolved (e.g. an unresolvable approach error).
func IsLastRowOfStage(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	rowIdx int,
	stageNum domain.StageNumber,
) bool {
	if stageNum == 0 {
		return false
	}
	ordGroups, err := orderedGroupsForStage(workflow, stages, stageNum)
	if err != nil || len(ordGroups) == 0 {
		return false
	}
	lastGroup := ordGroups[len(ordGroups)-1]
	return rowIdx == lastGroup.EndRow-1
}

// IsLastRowOfPhase reports whether the routing table row at rowIdx is the last
// row dispatched for its phase in the admitted workflow.
//
// For non-EXECUTION rows (stageNum == 0): the row is the last of its phase
// when no subsequent routing table row declares the same phase name.
//
// For EXECUTION rows (stageNum > 0): the EXECUTION phase spans all stages.
// The row is the last only when it is the last row of the last group of the
// last stage — that is, the very last step of the entire EXECUTION phase.
// A step in any earlier stage is never the last of the EXECUTION phase.
//
// Returns false when rowIdx is out of range or when group ordering fails.
func IsLastRowOfPhase(
	workflow domain.AdmittedWorkflow,
	stages *domain.StageSet,
	rowIdx int,
	stageNum domain.StageNumber,
) bool {
	rows := workflow.Table.Rows
	if rowIdx < 0 || rowIdx >= len(rows) {
		return false
	}
	row := rows[rowIdx]

	if row.PhaseParsed.IsStaged && stageNum > 0 && stages != nil && stages.Count() > 0 {
		// EXECUTION PHASE_END fires only at the very last step of the entire
		// EXECUTION phase: the last row of the last stage's last group.
		lastEntry := stages.Entries[stages.Count()-1]
		if stageNum != lastEntry.Number {
			return false
		}
		ordGroups, err := orderedGroupsForStage(workflow, stages, lastEntry.Number)
		if err != nil || len(ordGroups) == 0 {
			return false
		}
		lastGroup := ordGroups[len(ordGroups)-1]
		return rowIdx == lastGroup.EndRow-1
	}

	// Non-EXECUTION rows: last row of its phase when no subsequent row shares
	// the same phase name.
	phaseName := row.PhaseParsed.Name
	for i := rowIdx + 1; i < len(rows); i++ {
		if rows[i].PhaseParsed.Name == phaseName {
			return false
		}
	}
	return true
}
