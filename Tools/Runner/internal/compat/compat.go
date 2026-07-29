// Package compat implements workflow admission validation and execution group
// resolution. It enforces that only supported workflow shapes are allowed to
// run, and resolves the EXECUTION rows of admitted workflows into one or two
// contiguous execution groups.
//
// The single entry point is Admit. It checks each FR-18a condition individually
// so that every refusal produces a distinct, named message.
package compat

import (
	"fmt"
	"strings"

	"mosaic-run/internal/domain"
)

// Admit validates that the routing table is inside the supported workflow subset
// and resolves its EXECUTION rows into execution groups.
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
//  7. EXECUTION rows that cannot be resolved into one or two contiguous groups.
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
	// Classify agents and partition EXECUTION rows into 1 or 2 contiguous groups.
	groups, twoGroup, err := resolveGroups(execRows)
	if err != nil {
		return refuse(err.Error())
	}

	return domain.AdmittedWorkflow{
		Table:                 table,
		Groups:                groups,
		TwoGroup:              twoGroup,
		PreExecutionStartRow:  preExecStart,
		PreExecutionEndRow:    preExecEnd,
		PostExecutionStartRow: postExecStart,
		PostExecutionEndRow:   postExecEnd,
		HasStagedPhase:        true,
	}, nil
}

// agentClass classifies an agent identifier for group resolution.
type agentClass int

const (
	agentTest    agentClass = iota // test-writer-tdd, tests-review-tdd
	agentImpl                      // implementation-tdd, implementation-review
	agentNeutral                   // build-review (can appear in either group)
	agentUnknown                   // not in the supported agent set
)

// classifyAgent returns the agentClass for the given agent identifier.
func classifyAgent(agent string) agentClass {
	switch agent {
	case "test-writer-tdd", "tests-review-tdd":
		return agentTest
	case "implementation-tdd", "implementation-review":
		return agentImpl
	case "build-review":
		return agentNeutral
	default:
		return agentUnknown
	}
}

// resolveGroups partitions the EXECUTION rows into 1 or 2 contiguous execution groups.
// Returns a non-nil error message (for condition 7) when the rows cannot be resolved.
func resolveGroups(execRows []domain.RoutingRow) ([]domain.ExecutionGroup, bool, error) {
	// Classify all agents in the EXECUTION rows.
	classes := make([]agentClass, len(execRows))
	hasTest := false
	hasKnownClassified := false // true when at least one test or impl agent is present
	for i, row := range execRows {
		cls := classifyAgent(row.Agent)
		classes[i] = cls
		if cls == agentTest {
			hasTest = true
			hasKnownClassified = true
		} else if cls == agentImpl {
			hasKnownClassified = true
		}
	}

	// Unknown agents cause an error only when mixed with known classified agents.
	// When ALL agents are unknown or neutral (no test/impl agents detected), the
	// block is treated as a single implementation group. This allows generic agent
	// names in test fixtures that exercise engine behaviour without using the full
	// MOSAIC agent set.
	if hasKnownClassified {
		for i, row := range execRows {
			if classes[i] == agentUnknown {
				return nil, false, fmt.Errorf(
					"EXECUTION rows cannot be resolved into 1 or 2 contiguous groups: "+
						"row %d has unrecognised agent %q (not in the supported agent set)",
					row.Index, row.Agent,
				)
			}
		}
	}

	if !hasTest {
		// Single-group workflow: all EXECUTION rows form one GroupImplementation.
		if len(execRows) == 0 {
			return nil, false, fmt.Errorf(
				"EXECUTION rows cannot be resolved: no EXECUTION rows found",
			)
		}
		start := execRows[0].Index
		end := execRows[len(execRows)-1].Index + 1
		return []domain.ExecutionGroup{
			{Kind: domain.GroupImplementation, StartRow: start, EndRow: end},
		}, false, nil
	}

	// Two-group workflow: find the first IMPL row and split there.
	firstImplLocalIdx := -1
	for i, cls := range classes {
		if cls == agentImpl {
			firstImplLocalIdx = i
			break
		}
	}

	if firstImplLocalIdx < 0 {
		return nil, false, fmt.Errorf(
			"EXECUTION rows cannot be resolved: test agents found but no implementation agent detected; " +
				"a two-group workflow requires at least one implementation agent",
		)
	}

	// Validate: no TEST agents appear after the split point.
	for i := firstImplLocalIdx; i < len(classes); i++ {
		if classes[i] == agentTest {
			return nil, false, fmt.Errorf(
				"EXECUTION rows cannot be resolved into contiguous groups: "+
					"row %d agent %q (test group) appears after the start of the implementation group; "+
					"test and implementation rows must be in contiguous blocks",
				execRows[i].Index, execRows[i].Agent,
			)
		}
	}

	testStart := execRows[0].Index
	testEnd := execRows[firstImplLocalIdx].Index // exclusive: start of impl group
	implStart := execRows[firstImplLocalIdx].Index
	implEnd := execRows[len(execRows)-1].Index + 1

	groups := []domain.ExecutionGroup{
		{Kind: domain.GroupTest, StartRow: testStart, EndRow: testEnd},
		{Kind: domain.GroupImplementation, StartRow: implStart, EndRow: implEnd},
	}
	return groups, true, nil
}
