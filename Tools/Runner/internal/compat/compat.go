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
// Duplicate agent identifiers in different rows are allowed (FR-26a).
func Admit(table domain.RoutingTable) (domain.AdmittedWorkflow, error) {
	wfID := string(table.Info.ID)
	resource := fmt.Sprintf("workflow %q", wfID)

	refuse := func(reason string) (domain.AdmittedWorkflow, error) {
		return domain.AdmittedWorkflow{}, &domain.RefusalError{
			Component: "compat",
			Resource:  resource,
			Reason:    reason,
		}
	}

	// --- Condition 6: Agent-with-mode notation ---
	// Check before any structural analysis so the error is precise.
	for _, row := range table.Rows {
		if strings.ContainsAny(row.Agent, "()") {
			return refuse(fmt.Sprintf(
				"agent-with-mode notation is not supported: row %d has agent %q (parentheses not allowed in agent identifiers)",
				row.Index, row.Agent,
			))
		}
	}

	// --- Condition 3: Staged phase with name other than "EXECUTION" ---
	for _, row := range table.Rows {
		if row.PhaseParsed.IsStaged && row.PhaseParsed.Name != "EXECUTION" {
			return refuse(fmt.Sprintf(
				"staged phase %q in row %d is not supported: only EXECUTION may be staged",
				row.PhaseParsed.Name, row.Index,
			))
		}
	}

	// --- Condition 5: Parallel dispatch (comma in OnSuccess.Value) ---
	for _, row := range table.Rows {
		if row.OnSuccess.ColumnPresent && strings.Contains(row.OnSuccess.Value, ",") {
			return refuse(fmt.Sprintf(
				"parallel dispatch is not supported: row %d has OnSuccess %q (comma-separated agents indicate parallel routing)",
				row.Index, row.OnSuccess.Value,
			))
		}
	}

	// Identify EXECUTION rows and the pre/post-EXECUTION ranges.
	firstExecIdx := -1
	lastExecIdx := -1
	for _, row := range table.Rows {
		if row.PhaseParsed.IsStaged {
			if firstExecIdx < 0 {
				firstExecIdx = row.Index
			}
			lastExecIdx = row.Index
		}
	}

	// If no staged rows, there are no further checks to do. Return a non-staged AdmittedWorkflow.
	// (This is an edge case — the supported set always has staged rows.)
	if firstExecIdx < 0 {
		return domain.AdmittedWorkflow{
			Table:          table,
			HasStagedPhase: false,
		}, nil
	}

	// --- Condition 2: Multiple staged phase blocks ---
	// A staged phase block is a contiguous run of staged rows. If non-staged rows
	// appear between staged rows, that's two blocks.
	seenNonStaged := false
	for i := firstExecIdx; i <= lastExecIdx; i++ {
		row := table.Rows[i]
		if !row.PhaseParsed.IsStaged {
			seenNonStaged = true
		} else if seenNonStaged {
			// A staged row appeared after a non-staged row inside the EXECUTION range.
			return refuse(
				"more than one staged phase block: EXECUTION rows are split by non-staged rows, which is not supported",
			)
		}
	}

	// Compute pre/post execution row ranges.
	preExecStart := 0
	preExecEnd := firstExecIdx   // exclusive
	postExecStart := lastExecIdx + 1
	postExecEnd := len(table.Rows)

	// --- Condition 1: Stage source not from plan artifact ---
	// If there are pre-EXECUTION rows and none of them output Stage-*/Plan.md,
	// the stage set cannot come from the plan artifact.
	if preExecEnd > preExecStart {
		hasPlanStageSource := false
		for i := preExecStart; i < preExecEnd; i++ {
			for _, art := range table.Rows[i].OutputArtifacts {
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
			return refuse(
				"stage source is not the plan artifact: no pre-EXECUTION row produces Stage-*/Plan.md; " +
					"the runner requires stages to be defined by the plan artifact",
			)
		}
	}

	// --- Condition 4: Dynamic or growing stage set ---
	// An EXECUTION row that produces Stage-*/Plan.md signals that stages can be
	// added during execution (the plan artifact can grow), which is not supported.
	for i := firstExecIdx; i <= lastExecIdx; i++ {
		row := table.Rows[i]
		if !row.PhaseParsed.IsStaged {
			continue
		}
		for _, art := range row.OutputArtifacts {
			if art == "Stage-*/Plan.md" {
				return refuse(fmt.Sprintf(
					"dynamic stage set detected: EXECUTION row %d produces Stage-*/Plan.md, "+
						"which implies stages can be added during execution; "+
						"only a fixed, pre-determined stage set is supported",
					row.Index,
				))
			}
		}
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

	if !groupsDeclared {
		// All rows are bare (no group segments declared).

		// A3: approach table present but rows are bare.
		if approach.Present() {
			return nil, false, fmt.Errorf(
				"workflow declares an %q table but EXECUTION row %d declares no group segment",
				domain.ExecutionGroupsHeading, execRows[0].Index,
			)
		}

		// Bare workflow: single implicit group with empty Name covering all EXECUTION rows.
		start := execRows[0].Index
		end := execRows[len(execRows)-1].Index + 1
		return []domain.ExecutionGroup{
			{Name: "", StartRow: start, EndRow: end},
		}, false, nil
	}

	// groupsDeclared = true: at least one row has a group segment.

	// A2: grouped rows but no approach table.
	if !approach.Present() {
		for _, row := range execRows {
			if row.PhaseParsed.Group != "" {
				return nil, false, fmt.Errorf(
					"EXECUTION row %d declares group %q but the workflow has no %q table",
					row.Index, row.PhaseParsed.Group, domain.ExecutionGroupsHeading,
				)
			}
		}
	}

	// A3: approach table present but at least one EXECUTION row is bare.
	for _, row := range execRows {
		if row.PhaseParsed.Group == "" {
			return nil, false, fmt.Errorf(
				"workflow declares an %q table but EXECUTION row %d declares no group segment",
				domain.ExecutionGroupsHeading, row.Index,
			)
		}
	}

	// Partition rows by group token, checking A1 (non-contiguous groups).
	type groupState struct {
		startRow int
		endRow   int  // exclusive (last seen row index + 1)
		ended    bool // true once a different group appeared after this one
	}

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
				return nil, false, fmt.Errorf(
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

	// Build the set of groups declared in EXECUTION rows.
	execGroupSet := make(map[domain.GroupName]bool, len(groupOrder))
	for _, name := range groupOrder {
		execGroupSet[name] = true
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
	for _, row := range execRows {
		g := row.PhaseParsed.Group
		if !tableGroupSet[g] {
			return nil, false, fmt.Errorf(
				"EXECUTION row %d declares group %q, which appears in no %q table row",
				row.Index, g, domain.ExecutionGroupsHeading,
			)
		}
	}

	// A4: the approach table names a group that no EXECUTION row belongs to.
	for _, seq := range approach.Rows {
		for _, g := range seq.Groups {
			if !execGroupSet[g] {
				return nil, false, fmt.Errorf(
					"%q table row %q lists group %q, which no EXECUTION row declares",
					domain.ExecutionGroupsHeading, seq.Approach, g,
				)
			}
		}
	}

	return groups, true, nil
}
