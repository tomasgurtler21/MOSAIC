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
// (header with no data rows). Every refusal produces a *domain.RefusalError
// naming the workflow and the specific condition.
package workflow

import (
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
// The content is the raw bytes between the [[SECTION:Workflow:{id}]] boundary
// tags. The info carries the identifier and version extracted by orchfile.
//
// Required columns: Phase, Subagent, HITL, Input, Output.
// Optional columns: On Success, On Findings.
//
// Returns a *domain.RefusalError with a specific message naming the workflow
// and condition if:
//   - The content contains no parseable routing table
//   - Required columns (Phase, Subagent, HITL, Input, Output) are missing
//   - The table has a header and separator but zero data rows
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

		// Decode HITL: ✅ means true, anything else means false.
		hitl := strings.Contains(hitlCell, "✅")

		// Split comma-separated artifact paths; "-" or empty means no artifacts.
		inputArtifacts := splitArtifacts(inputCell)
		outputArtifacts := splitArtifacts(outputCell)

		// Parse the phase string into its structured form.
		phaseParsed := parsePhase(phase)

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

	return domain.RoutingTable{
		Info: info,
		Rows: rows,
	}, nil
}

// parsePhase converts a phase string into a PhaseParsed value.
// "EXECUTION.[StageNumber]" and any other "EXECUTION." prefix are staged;
// all other phase strings are not staged.
func parsePhase(phase string) domain.PhaseParsed {
	if strings.HasPrefix(phase, "EXECUTION.") {
		return domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}
	}
	return domain.PhaseParsed{Name: phase, IsStaged: false}
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
