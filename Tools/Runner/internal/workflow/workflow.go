// Package workflow parses a routing table from a workflow region's raw content
// into a flat, ordered, index-identified model. It uses mdtable to parse the
// pipe-delimited markdown table and produces a RoutingTable value whose rows
// preserve declaration order.
//
// Parse does not interpret the table: no compat checks, no grouping, no path
// resolution. Artifact paths are preserved verbatim (templates are not resolved
// here). Optional columns (On Success, On Findings) use OptionalHint to
// distinguish "column absent" from "column present with no hint".
//
// Refusals: no parseable routing table, missing required columns, empty table
// (header with no data rows), malformed EXECUTION phase strings, malformed
// Execution Groups table. Every refusal produces a *domain.RefusalError
// naming the workflow and the specific condition.
package workflow

import (
	"bytes"
	"fmt"
	"strings"

	"mosaic-common/mdtable"
	"mosaic-run/internal/domain"
)

// Required column names that must be present in every routing table.
var requiredColumns = []string{"Phase", "Subagent", "HITL", "Input", "Output"}

// Parse turns a workflow region's raw content into a flat, ordered,
// row-identified routing table model. It parses but does not interpret:
// no refusal decisions, no grouping, no path resolution.
//
// The content is the raw bytes inside the <Workflow type="core" name="{id}">
// region. The info carries the identifier and version extracted by orchfile.
//
// Required columns: Phase, Subagent, HITL, Input, Output.
// Optional columns: On Success, On Findings.
//
// Returns a *domain.RefusalError with a specific message naming the workflow
// and condition if:
//   - The content contains no parseable routing table
//   - Required columns (Phase, Subagent, HITL, Input, Output) are missing
//   - The table has a header and separator but zero data rows
//   - An EXECUTION phase string is malformed
//   - The **Execution Groups:** heading is present but the approach table is invalid
func Parse(content []byte, info domain.WorkflowInfo) (domain.RoutingTable, error) {
	workflowID := string(info.ID)

	t, err := mdtable.Parse(content)
	if err != nil {
		return domain.RoutingTable{}, &domain.RefusalError{
			Component: "workflow",
			Resource:  workflowID,
			Reason:    "no parseable routing table found",
		}
	}

	// Validate that all required columns are present.
	for _, col := range requiredColumns {
		if t.Column(col) < 0 {
			return domain.RoutingTable{}, &domain.RefusalError{
				Component: "workflow",
				Resource:  workflowID,
				Reason:    "required column missing: " + col,
			}
		}
	}

	// Refuse tables with a valid header/separator but no data rows.
	if len(t.Rows) == 0 {
		return domain.RoutingTable{}, &domain.RefusalError{
			Component: "workflow",
			Resource:  workflowID,
			Reason:    "routing table has no data rows",
		}
	}

	// Resolve column indices.
	phaseCol := t.Column("Phase")
	subagentCol := t.Column("Subagent")
	hitlCol := t.Column("HITL")
	inputCol := t.Column("Input")
	outputCol := t.Column("Output")
	onSuccessCol := t.Column("On Success")
	onFindingsCol := t.Column("On Findings")

	rows := make([]domain.RoutingRow, 0, len(t.Rows))
	for i, rawRow := range t.Rows {
		phase := rawRow[phaseCol]
		agent := rawRow[subagentCol]
		hitlCell := rawRow[hitlCol]
		inputCell := rawRow[inputCol]
		outputCell := rawRow[outputCol]

		// Decode HITL: TRUE means true, anything else means false.
		hitl := strings.EqualFold(strings.TrimSpace(hitlCell), "TRUE")

		// Split comma-separated artifact paths; "-" or empty means no artifacts.
		inputArtifacts := splitArtifacts(inputCell)
		outputArtifacts := splitArtifacts(outputCell)

		// Parse the phase string into its structured form. Returns an error
		// for malformed EXECUTION phase strings.
		phaseParsed, parseErr := parsePhase(phase, i)
		if parseErr != nil {
			return domain.RoutingTable{}, &domain.RefusalError{
				Component: "workflow",
				Resource:  workflowID,
				Reason:    parseErr.Error(),
			}
		}

		// Resolve optional columns.
		onSuccess := resolveOptionalHint(rawRow, onSuccessCol)
		onFindings := resolveOptionalHint(rawRow, onFindingsCol)

		rows = append(rows, domain.RoutingRow{
			Index:           i,
			Phase:           phase,
			PhaseParsed:     phaseParsed,
			Agent:           agent,
			HITL:            hitl,
			InputArtifacts:  inputArtifacts,
			OutputArtifacts: outputArtifacts,
			OnSuccess:       onSuccess,
			OnFindings:      onFindings,
		})
	}

	// Locate and decode the Execution Groups approach table, if declared.
	var approachTable domain.ApproachTable
	if headingOffset, found := findExecutionGroupsHeading(content); found {
		approachTable, err = parseApproachTable(content, headingOffset, workflowID)
		if err != nil {
			return domain.RoutingTable{}, err
		}
	}

	return domain.RoutingTable{
		Info:          info,
		Rows:          rows,
		ApproachTable: approachTable,
	}, nil
}

// parsePhase converts a phase string into a PhaseParsed value.
// It is fallible: a malformed EXECUTION phase string returns an error rather
// than being silently normalised to a bare staged row.
//
// row is the zero-based row index within the routing table; it is included in
// refusal messages so callers do not need to re-wrap the error.
//
// Algorithm:
//   - A phase not starting with "EXECUTION." yields a non-staged result, no error.
//   - rest = everything after "EXECUTION.":
//   - rest starts with "[" → bare staged row, Group "".
//   - rest has no "." → error (missing [StageNumber] segment).
//   - segment before first "." is empty or contains whitespace → error (P2).
//   - remainder after that "." does not start with "[" → error (P1).
//   - otherwise → grouped staged row with the segment as Group.
func parsePhase(phase string, row int) (domain.PhaseParsed, error) {
	const prefix = "EXECUTION."
	if !strings.HasPrefix(phase, prefix) {
		return domain.PhaseParsed{Name: phase, IsStaged: false}, nil
	}
	rest := phase[len(prefix):]

	// Bare staged row: EXECUTION.[StageNumber]
	if strings.HasPrefix(rest, "[") {
		return domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: ""}, nil
	}

	// Grouped staged row: EXECUTION.{Group}.[StageNumber]
	dotIdx := strings.Index(rest, ".")
	if dotIdx < 0 {
		// No second dot — [StageNumber] segment is missing entirely.
		return domain.PhaseParsed{}, fmt.Errorf(
			"malformed EXECUTION phase %q in row %d: expected EXECUTION.{Group}.[StageNumber]", phase, row)
	}

	segment := rest[:dotIdx]
	// Group token must be non-empty after trimming and must not contain whitespace.
	if strings.TrimSpace(segment) == "" || strings.ContainsAny(segment, " \t\n\r") {
		return domain.PhaseParsed{}, fmt.Errorf(
			"malformed EXECUTION phase %q in row %d: group segment must be a single non-empty token", phase, row)
	}

	remainder := rest[dotIdx+1:]
	if !strings.HasPrefix(remainder, "[") {
		// The segment after the group token is not a bracketed stage number.
		return domain.PhaseParsed{}, fmt.Errorf(
			"malformed EXECUTION phase %q in row %d: expected EXECUTION.{Group}.[StageNumber]", phase, row)
	}

	return domain.PhaseParsed{
		Name:     "EXECUTION",
		IsStaged: true,
		Group:    domain.GroupName(segment),
	}, nil
}

// findExecutionGroupsHeading scans content line by line for an exact trimmed
// full-line match of domain.ExecutionGroupsHeading. Returns the byte offset
// immediately after that line (where the approach table might follow) and true
// when found. Returns (0, false) when the heading is absent.
func findExecutionGroupsHeading(content []byte) (int, bool) {
	lines := bytes.Split(content, []byte("\n"))
	offset := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(string(line))
		if trimmed == domain.ExecutionGroupsHeading {
			// Return the offset after this line (start of the next line).
			return offset + len(line) + 1, true
		}
		offset += len(line) + 1
	}
	return 0, false
}

// parseApproachTable reads and validates the Execution Groups approach table
// from content starting at headingOffset. headingOffset is the byte position
// immediately after the reserved heading line.
//
// Returns a populated ApproachTable on success, or a *domain.RefusalError for
// any structural problem (P3–P8 in the refusal catalogue).
func parseApproachTable(content []byte, headingOffset int, workflowID string) (domain.ApproachTable, error) {
	refusal := func(reason string) (domain.ApproachTable, error) {
		return domain.ApproachTable{}, &domain.RefusalError{
			Component: "workflow",
			Resource:  workflowID,
			Reason:    reason,
		}
	}

	// P3: heading present but no table follows.
	tbl, _, _, err := mdtable.ParseAt(content, headingOffset)
	if err != nil {
		return refusal(`no Execution Groups table found after the "**Execution Groups:**" heading`)
	}

	// P4: Approach column missing.
	approachCol := tbl.Column("Approach")
	if approachCol < 0 {
		return refusal(`Execution Groups table is missing the required "Approach" column`)
	}

	// P5: Groups column missing.
	groupsCol := tbl.Column("Groups")
	if groupsCol < 0 {
		return refusal(`Execution Groups table is missing the required "Groups" column`)
	}

	// P6: table header present but zero data rows.
	if len(tbl.Rows) == 0 {
		return refusal("Execution Groups table has no data rows")
	}

	seenApproach := make(map[domain.Approach]bool)
	rows := make([]domain.ApproachSequence, 0, len(tbl.Rows))

	for rowIdx, rawRow := range tbl.Rows {
		approachCell := rawRow[approachCol]
		groupsCell := rawRow[groupsCol]

		// P7: empty Approach cell.
		if approachCell == "" {
			return refusal(fmt.Sprintf("Execution Groups table row %d has an empty Approach value", rowIdx))
		}

		// P7: empty Groups cell.
		if groupsCell == "" {
			return refusal(fmt.Sprintf("Execution Groups table row %d has an empty Groups value", rowIdx))
		}

		approach := domain.Approach(approachCell)

		// P8: duplicate Approach token across rows.
		if seenApproach[approach] {
			return refusal(fmt.Sprintf("Execution Groups table declares approach %q twice", approach))
		}
		seenApproach[approach] = true

		// Parse the comma-separated group token sequence.
		parts := strings.Split(groupsCell, ",")
		seenGroup := make(map[domain.GroupName]bool)
		groups := make([]domain.GroupName, 0, len(parts))

		for _, p := range parts {
			token := strings.TrimSpace(p)
			// P7: empty token inside a Groups cell.
			if token == "" {
				return refusal(fmt.Sprintf("Execution Groups table row %d has an empty Groups value", rowIdx))
			}
			g := domain.GroupName(token)
			// P8: duplicate group token within one Groups cell.
			if seenGroup[g] {
				return refusal(fmt.Sprintf("Execution Groups table declares group %q twice", g))
			}
			seenGroup[g] = true
			groups = append(groups, g)
		}

		rows = append(rows, domain.ApproachSequence{
			Approach: approach,
			Groups:   groups,
		})
	}

	return domain.ApproachTable{Rows: rows}, nil
}

// splitArtifacts splits a comma-separated artifact path cell into individual
// trimmed paths. Returns nil when the cell is "-" or empty (no artifacts).
func splitArtifacts(cell string) []string {
	if cell == "-" || cell == "" {
		return nil
	}
	parts := strings.Split(cell, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// resolveOptionalHint returns an OptionalHint for an optional column.
// When colIdx < 0, the column is absent (ColumnPresent == false).
// When the column is present but the cell is "-" or empty, Value is "".
func resolveOptionalHint(row []string, colIdx int) domain.OptionalHint {
	if colIdx < 0 {
		return domain.OptionalHint{ColumnPresent: false}
	}
	val := row[colIdx]
	if val == "-" || val == "" {
		return domain.OptionalHint{ColumnPresent: true, Value: ""}
	}
	return domain.OptionalHint{ColumnPresent: true, Value: val}
}
