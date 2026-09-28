// Package compat implements workflow admission validation and execution group
// resolution. It enforces that only supported workflow shapes are allowed to
// run, and resolves the EXECUTION rows of admitted workflows into contiguous
// named execution groups driven by the Phase column.
//
// The single entry point is Admit. It checks each FR-18a condition individually
// so that every refusal produces a distinct, named message.
//
// Groups are resolved by the group segment of each EXECUTION row's PhaseParsed.Group
// field. Agent identifiers are never inspected for classification. Three or more
// groups resolve correctly. The GroupsDeclared and ApproachTable fields are carried
// onto the admitted workflow for downstream consumers.
//
// The GroupByName method on AdmittedWorkflow enables lookup by workflow-defined name.
//
// Admission refusals A1–A5 enforce cross-consistency between the EXECUTION rows'
// group segments and the workflow's Execution Groups approach table.
// Case-sensitive matching is required throughout.
package compat

import (
	"fmt"
	"strings"

	"mosaic-run/internal/domain"
)

// Admit validates that the routing table is inside the supported workflow subset
// and resolves its EXECUTION rows into contiguous execution groups.
//
// Returns an AdmittedWorkflow on success. Returns *domain.RefusalError on failure,
// with Component set to "compat" and Resource identifying the workflow and the
// specific row or condition that caused the refusal.
//
// FR-18a conditions checked (each produces a distinct message):
//  1. Stage source other than the plan artifact.
//  2. More than one staged phase block.
//  3. Staged phase whose name is not "EXECUTION".
//  4. Dynamic or growing stage set.
//  5. Any parallel dispatch (Waits For notation in routing hints).
//  6. Agent-with-mode notation ("agent-name(mode)").
//  7. EXECUTION rows that cannot be resolved into contiguous execution groups.
//
// Conditions 1-4, 6 and 7 build dispatch requests and apply in every run mode.
// Condition 5 is an engine-routing shape check and applies only when the engine
// routes (auto, auto-review, and the unset sentinel). In orchestrated mode it is
// replaced by a fork-branch boundary check: a fork branch may not be the last
// row of its phase or execution group, because the last-row helpers answer by
// table position and a branch has no join to end on.
//
// Duplicate agent identifiers in different rows are allowed (FR-26a).
func Admit(table domain.RoutingTable, mode domain.ExecutionMode) (domain.AdmittedWorkflow, error) {
	wfID := string(table.Info.ID)
	resource := fmt.Sprintf("workflow %q", wfID)

	refuse := func(reason string) (domain.AdmittedWorkflow, error) {
		return domain.AdmittedWorkflow{}, &domain.RefusalError{
			Component: "compat",
			Resource:  resource,
			Reason:    reason,
		}
	}

	// Check before any structural analysis so the error is precise.
	if reason, failed := checkAgentModeNotation(table.Rows); failed {
		return refuse(reason)
	}

	if reason, failed := checkStagedPhaseName(table.Rows); failed {
		return refuse(reason)
	}

	// Engine-routing shape checks apply only where the engine routes On Success
	// itself. In orchestrated mode the consultant dispatches one agent at a
	// time, so a fork/join table is admitted, subject to its boundary shape.
	if mode == domain.ExecutionModeOrchestrated {
		if reason, failed := checkForkBranchBoundary(table.Rows); failed {
			return refuse(reason)
		}
	} else if reason, failed := checkParallelDispatch(table.Rows); failed {
		return refuse(reason)
	}

	// Identify the EXECUTION row range.
	firstExecIdx, lastExecIdx := findStagedRange(table.Rows)

	// If no staged rows, there are no further checks to do. Return a non-staged AdmittedWorkflow.
	// (This is an edge case — the supported set always has staged rows.)
	if firstExecIdx < 0 {
		return domain.AdmittedWorkflow{
			Table:          table,
			HasStagedPhase: false,
		}, nil
	}

	if reason, failed := checkSingleStagedBlock(table.Rows, firstExecIdx, lastExecIdx); failed {
		return refuse(reason)
	}

	// Compute pre/post execution row ranges.
	preExecStart := 0
	preExecEnd := firstExecIdx // exclusive
	postExecStart := lastExecIdx + 1
	postExecEnd := len(table.Rows)

	if reason, failed := checkPlanStageSource(table.Rows, preExecStart, preExecEnd); failed {
		return refuse(reason)
	}

	if reason, failed := checkDynamicStageSet(table.Rows, firstExecIdx, lastExecIdx); failed {
		return refuse(reason)
	}

	// Collect EXECUTION rows only.
	var execRows []domain.RoutingRow
	for i := firstExecIdx; i <= lastExecIdx; i++ {
		if table.Rows[i].PhaseParsed.IsStaged {
			execRows = append(execRows, table.Rows[i])
		}
	}

	// --- Condition 7: Resolve execution groups ---
	// Partition EXECUTION rows into contiguous named groups using the Phase column's
	// group segment. Agent identifiers are never inspected for classification.
	groups, groupsDeclared, err := resolveGroups(execRows, table.ApproachTable)
	if err != nil {
		return refuse(err.Error())
	}

	return domain.AdmittedWorkflow{
		Table:                 table,
		Groups:                groups,
		GroupsDeclared:        groupsDeclared,
		ApproachTable:         table.ApproachTable,
		PreExecutionStartRow:  preExecStart,
		PreExecutionEndRow:    preExecEnd,
		PostExecutionStartRow: postExecStart,
		PostExecutionEndRow:   postExecEnd,
		HasStagedPhase:        true,
	}, nil
}

// checkAgentModeNotation implements FR-18a condition 6: agent-with-mode
// notation ("agent-name(mode)") is not supported.
func checkAgentModeNotation(rows []domain.RoutingRow) (reason string, failed bool) {
	for _, row := range rows {
		if strings.ContainsAny(row.Agent, "()") {
			return fmt.Sprintf(
				"agent-with-mode notation is not supported: row %d has agent %q (parentheses not allowed in agent identifiers)",
				row.Index, row.Agent,
			), true
		}
	}
	return "", false
}

// checkStagedPhaseName implements FR-18a condition 3: a staged phase whose
// name is not "EXECUTION" is not supported.
func checkStagedPhaseName(rows []domain.RoutingRow) (reason string, failed bool) {
	for _, row := range rows {
		if row.PhaseParsed.IsStaged && row.PhaseParsed.Name != "EXECUTION" {
			return fmt.Sprintf(
				"staged phase %q in row %d is not supported: only EXECUTION may be staged",
				row.PhaseParsed.Name, row.Index,
			), true
		}
	}
	return "", false
}

// checkParallelDispatch implements FR-18a condition 5: a comma-separated
// OnSuccess value indicates parallel dispatch, which is not supported.
func checkParallelDispatch(rows []domain.RoutingRow) (reason string, failed bool) {
	for _, row := range rows {
		if row.OnSuccess.ColumnPresent && strings.Contains(row.OnSuccess.Value, ",") {
			return fmt.Sprintf(
				"parallel dispatch is not supported: row %d has OnSuccess %q (comma-separated agents indicate parallel routing)",
				row.Index, row.OnSuccess.Value,
			), true
		}
	}
	return "", false
}

// checkForkBranchBoundary refuses a fork branch row that is the last row of its
// phase. A branch is a row named in the multi-target On Success of an earlier row
// of the same phase. Execution groups carry distinct Phase values, so this also
// covers the last row of an execution group.
func checkForkBranchBoundary(rows []domain.RoutingRow) (reason string, failed bool) {
	for i, row := range rows {
		if !row.OnSuccess.ColumnPresent || !strings.Contains(row.OnSuccess.Value, ",") {
			continue
		}
		targets := map[string]bool{}
		for _, t := range strings.Split(row.OnSuccess.Value, ",") {
			targets[strings.TrimSpace(t)] = true
		}
		for j := i + 1; j < len(rows) && rows[j].Phase == row.Phase; j++ {
			endsPhase := j+1 == len(rows) || rows[j+1].Phase != row.Phase
			if endsPhase && targets[rows[j].Agent] {
				return fmt.Sprintf(
					"fork branch %q at row %d is the last row of its phase %q: a fork branch must be followed by a join row",
					rows[j].Agent, rows[j].Index, rows[j].Phase,
				), true
			}
		}
	}
	return "", false
}

// checkSingleStagedBlock implements FR-18a condition 2: a staged phase block
// is a contiguous run of staged rows. If non-staged rows appear between
// staged rows within the EXECUTION range, that's two blocks, which is not
// supported.
func checkSingleStagedBlock(rows []domain.RoutingRow, firstExecIdx, lastExecIdx int) (reason string, failed bool) {
	seenNonStaged := false
	for i := firstExecIdx; i <= lastExecIdx; i++ {
		row := rows[i]
		if !row.PhaseParsed.IsStaged {
			seenNonStaged = true
		} else if seenNonStaged {
			// A staged row appeared after a non-staged row inside the EXECUTION range.
			return "more than one staged phase block: EXECUTION rows are split by non-staged rows, which is not supported", true
		}
	}
	return "", false
}

// checkPlanStageSource implements FR-18a condition 1: if there are
// pre-EXECUTION rows and none of them output Stage-*/Plan.md, the stage set
// cannot come from the plan artifact.
func checkPlanStageSource(rows []domain.RoutingRow, preExecStart, preExecEnd int) (reason string, failed bool) {
	if preExecEnd <= preExecStart {
		return "", false
	}

	hasPlanStageSource := false
	for i := preExecStart; i < preExecEnd; i++ {
		for _, art := range rows[i].OutputArtifacts {
			if art == "Stage-*/Plan.md" {
				hasPlanStageSource = true
				break
			}
		}
		if hasPlanStageSource {
			break
		}
	}
	if !hasPlanStageSource {
		return "stage source is not the plan artifact: no pre-EXECUTION row produces Stage-*/Plan.md; " +
			"the runner requires stages to be defined by the plan artifact", true
	}
	return "", false
}

// checkDynamicStageSet implements FR-18a condition 4: an EXECUTION row that
// produces Stage-*/Plan.md signals that stages can be added during execution
// (the plan artifact can grow), which is not supported.
func checkDynamicStageSet(rows []domain.RoutingRow, firstExecIdx, lastExecIdx int) (reason string, failed bool) {
	for i := firstExecIdx; i <= lastExecIdx; i++ {
		row := rows[i]
		if !row.PhaseParsed.IsStaged {
			continue
		}
		for _, art := range row.OutputArtifacts {
			if art == "Stage-*/Plan.md" {
				return fmt.Sprintf(
					"dynamic stage set detected: EXECUTION row %d produces Stage-*/Plan.md, "+
						"which implies stages can be added during execution; "+
						"only a fixed, pre-determined stage set is supported",
					row.Index,
				), true
			}
		}
	}
	return "", false
}

// resolveGroups partitions the EXECUTION rows into contiguous execution groups
// using each row's PhaseParsed.Group. Agent identifiers are never inspected.
//
// The bool result is GroupsDeclared: true iff at least one row carries a group
// segment. When false the result is the single implicit group with an empty Name
// spanning every staged row.
//
// Returns an error for catalogue rows A1–A5: A1 and A3 from the rows alone, and
// A2, A4, A5 from cross-checking the declared group set against the approach
// table (zero-valued when the workflow declares none).
func resolveGroups(
	execRows []domain.RoutingRow,
	approach domain.ApproachTable,
) ([]domain.ExecutionGroup, bool, error) {
	if len(execRows) == 0 {
		return nil, false, fmt.Errorf("EXECUTION rows cannot be resolved: no EXECUTION rows found")
	}

	// Determine if any EXECUTION row carries a group segment.
	groupsDeclared := false
	for _, row := range execRows {
		if row.PhaseParsed.Group != "" {
			groupsDeclared = true
			break
		}
	}

	// A2/A3: cross-check whether an approach table is required, absent, or
	// contradicted by a bare row, given whether any row declares a group.
	if err := checkGroupsRequireApproachTable(execRows, approach, groupsDeclared); err != nil {
		return nil, false, err
	}

	if !groupsDeclared {
		// Bare workflow: single implicit group with empty Name covering all EXECUTION rows.
		start := execRows[0].Index
		end := execRows[len(execRows)-1].Index + 1
		return []domain.ExecutionGroup{
			{Name: "", StartRow: start, EndRow: end},
		}, false, nil
	}

	// Partition rows by group token, checking A1 (non-contiguous groups).
	groups, err := partitionGroups(execRows)
	if err != nil {
		return nil, false, err
	}

	// A5/A4: cross-check the declared group set against the approach table.
	if err := checkGroupSetsMatch(groups, approach); err != nil {
		return nil, false, err
	}

	return groups, true, nil
}

// checkGroupsRequireApproachTable enforces A2 and A3: an approach table must
// be present if and only if the EXECUTION rows declare groups, and once
// present, every EXECUTION row must declare a group (none may be bare).
func checkGroupsRequireApproachTable(execRows []domain.RoutingRow, approach domain.ApproachTable, groupsDeclared bool) error {
	if !groupsDeclared {
		// All rows are bare (no group segments declared).

		// A3: approach table present but rows are bare.
		if approach.Present() {
			return fmt.Errorf(
				"workflow declares an %q table but EXECUTION row %d declares no group segment",
				domain.ExecutionGroupsHeading, execRows[0].Index,
			)
		}
		return nil
	}

	// groupsDeclared = true: at least one row has a group segment.

	// A2: grouped rows but no approach table.
	if !approach.Present() {
		for _, row := range execRows {
			if row.PhaseParsed.Group != "" {
				return fmt.Errorf(
					"EXECUTION row %d declares group %q but the workflow has no %q table",
					row.Index, row.PhaseParsed.Group, domain.ExecutionGroupsHeading,
				)
			}
		}
	}

	// A3: approach table present but at least one EXECUTION row is bare.
	for _, row := range execRows {
		if row.PhaseParsed.Group == "" {
			return fmt.Errorf(
				"workflow declares an %q table but EXECUTION row %d declares no group segment",
				domain.ExecutionGroupsHeading, row.Index,
			)
		}
	}
	return nil
}

// partitionGroups partitions execRows into contiguous named execution groups
// by their PhaseParsed.Group token, in first-appearance order. It implements
// A1: a group that has already ended may not be re-opened by a later row.
func partitionGroups(execRows []domain.RoutingRow) ([]domain.ExecutionGroup, error) {
	var groupOrder []domain.GroupName
	groupStates := map[domain.GroupName]*groupState{}
	currentGroup := domain.GroupName("")

	for _, row := range execRows {
		g := row.PhaseParsed.Group

		if g != currentGroup {
			// Switching to a different group.
			if currentGroup != "" {
				// The previous group has now ended.
				groupStates[currentGroup].ended = true
			}

			state, seen := groupStates[g]
			if seen && state.ended {
				// A1: this group already ended; a row is re-opening it.
				return nil, fmt.Errorf(
					"execution groups must be contiguous row ranges: row %d declares group %q, which already ended at row %d",
					row.Index, g, state.endRow-1,
				)
			}

			if !seen {
				groupStates[g] = &groupState{startRow: row.Index, endRow: row.Index + 1}
				groupOrder = append(groupOrder, g)
			}
			currentGroup = g
		}

		// Extend the current group to include this row.
		groupStates[g].endRow = row.Index + 1
	}

	// Build the groups slice in first-appearance order.
	groups := make([]domain.ExecutionGroup, len(groupOrder))
	for i, name := range groupOrder {
		s := groupStates[name]
		groups[i] = domain.ExecutionGroup{
			Name:     name,
			StartRow: s.startRow,
			EndRow:   s.endRow,
		}
	}
	return groups, nil
}

// checkGroupSetsMatch cross-checks the resolved execution groups against the
// approach table's declared groups. It implements A5 (an EXECUTION row
// declares a group absent from every approach table row) and A4 (the
// approach table names a group that no EXECUTION row belongs to), in that
// order.
func checkGroupSetsMatch(groups []domain.ExecutionGroup, approach domain.ApproachTable) error {
	// Build the set of groups declared in EXECUTION rows.
	execGroupSet := make(map[domain.GroupName]bool, len(groups))
	for _, g := range groups {
		execGroupSet[g.Name] = true
	}

	// Build the set of groups named anywhere in the approach table.
	tableGroupSet := make(map[domain.GroupName]bool)
	for _, seq := range approach.Rows {
		for _, g := range seq.Groups {
			tableGroupSet[g] = true
		}
	}

	// A5: an EXECUTION row declares a group absent from every approach table row.
	// Check before A4 so the offending row's token (verbatim from the Phase column)
	// appears in the refusal message, enabling the author to correct capitalisation.
	//
	// Iterate groups in resolved order so the message references the group
	// name as declared by the EXECUTION rows (row indices are not tracked
	// once partitioned, so the group's start row stands in for the row).
	for _, g := range groups {
		if !tableGroupSet[g.Name] {
			return fmt.Errorf(
				"EXECUTION row %d declares group %q, which appears in no %q table row",
				g.StartRow, g.Name, domain.ExecutionGroupsHeading,
			)
		}
	}

	// A4: the approach table names a group that no EXECUTION row belongs to.
	for _, seq := range approach.Rows {
		for _, g := range seq.Groups {
			if !execGroupSet[g] {
				return fmt.Errorf(
					"%q table row %q lists group %q, which no EXECUTION row declares",
					domain.ExecutionGroupsHeading, seq.Approach, g,
				)
			}
		}
	}
	return nil
}

// groupState tracks the row range of a single execution group during partitioning.
type groupState struct {
	startRow int
	endRow   int  // exclusive (last seen row index + 1)
	ended    bool // true once a different group appeared after this one
}

// findStagedRange returns the first and last row.Index values in rows that have
// PhaseParsed.IsStaged set. Returns (-1, -1) if no staged rows exist.
func findStagedRange(rows []domain.RoutingRow) (first, last int) {
	first, last = -1, -1
	for _, row := range rows {
		if row.PhaseParsed.IsStaged {
			if first < 0 {
				first = row.Index
			}
			last = row.Index
		}
	}
	return first, last
}
