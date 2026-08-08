// Package orchstate parses a MOSAIC orchestration document into the
// evidence the verdict engine consumes: the current phase, the last
// recorded status, and the ordered execution-log sequence.
//
// It reuses mosaic-common/docformat and mosaic-common/mdtable rather than
// hand-rolling markdown parsing, so orchestration-format knowledge stays in
// one place. Performs file I/O in ParseFile; classified as an adapter.
package orchstate

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"mosaic-common/docformat"
	"mosaic-common/mdtable"
	"mosaic-common/mosaic"

	"mosaic-agent-test/internal/domain"
)

// ErrDocumentAbsent is returned when the orchestration document does not
// exist. Distinct from ErrDocumentMalformed: absent means setup never ran or
// the run hasn't started, malformed means it ran and something broke.
var ErrDocumentAbsent = errors.New("orchstate: orchestration document absent")

// ErrDocumentMalformed is returned when the orchestration document exists
// but cannot be parsed as the MOSAIC document format.
var ErrDocumentMalformed = errors.New("orchstate: orchestration document malformed")

// executionLogSection is the [[SECTION:ExecutionLog]] boundary name.
const executionLogSection = "ExecutionLog"

// Parse reads a MOSAIC orchestration document already loaded into memory.
//
// A malformed document is a distinct, matchable condition (wrapping
// ErrDocumentMalformed), never a zero value that later reads as "phase not
// reached".
func Parse(data []byte) (domain.OrchestrationState, error) {
	doc, err := docformat.Parse(data)
	if err != nil {
		return domain.OrchestrationState{}, fmt.Errorf("%w: %v", ErrDocumentMalformed, err)
	}

	state := domain.OrchestrationState{}

	if cs, ok := doc.Frontmatter().Get("current_state"); ok && cs.Kind == mosaic.KindMapping {
		state.Phase = mappingString(cs, "phase")
		state.LastStatus = mappingString(cs, "last_status")
		state.LastErrorCode = mappingString(cs, "error_code")
	}

	node, ok := doc.Body().SectionDeep(executionLogSection)
	if !ok {
		return domain.OrchestrationState{}, fmt.Errorf("%w: no %s section found", ErrDocumentMalformed, executionLogSection)
	}

	table, err := mdtable.Parse(node.Content())
	if err != nil {
		return domain.OrchestrationState{}, fmt.Errorf("%w: %v", ErrDocumentMalformed, err)
	}

	rows, err := parseExecutionLogRows(table)
	if err != nil {
		return domain.OrchestrationState{}, fmt.Errorf("%w: %v", ErrDocumentMalformed, err)
	}

	state.Present = true
	state.ExecutionLog = rows
	return state, nil
}

// ParseFile is the convenience wrapper; a missing file yields an error
// wrapping ErrDocumentAbsent.
func ParseFile(path string) (domain.OrchestrationState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.OrchestrationState{}, fmt.Errorf("%w: %v", ErrDocumentAbsent, err)
	}
	return Parse(data)
}

// mappingString returns the plain scalar text of key in a KindMapping
// FieldValue, or "" when the key is absent or not a scalar.
func mappingString(v mosaic.FieldValue, key string) string {
	for _, p := range v.Pairs {
		if p.Key == key {
			return p.Value.Scalar
		}
	}
	return ""
}

// parseExecutionLogRows converts the parsed [[SECTION:ExecutionLog]] table
// into domain.ExecutionLogRow values, one per column with no surplus (see
// ContractsDesign.md's column-to-field mapping).
func parseExecutionLogRows(t mdtable.Table) ([]domain.ExecutionLogRow, error) {
	seqCol := t.Column("Seq")
	agentCol := t.Column("Agent")
	phaseCol := t.Column("Phase")
	stageCol := t.Column("Stage")
	statusCol := t.Column("Status")
	tsCol := t.Column("Timestamp")
	summaryCol := t.Column("Summary")
	inputsCol := t.Column("Inputs")
	checkpointCol := t.Column("Checkpoint")

	var rows []domain.ExecutionLogRow
	for _, r := range t.Rows {
		row := domain.ExecutionLogRow{}

		if seqCol >= 0 && seqCol < len(r) {
			cell := strings.TrimSpace(r[seqCol])
			if cell != "" && cell != "-" {
				seq, err := strconv.Atoi(cell)
				if err != nil {
					return nil, fmt.Errorf("invalid Seq value %q: %w", cell, err)
				}
				row.Seq = seq
			}
		}
		if agentCol >= 0 && agentCol < len(r) {
			row.AgentInstance = dashToEmpty(r[agentCol])
		}
		if phaseCol >= 0 && phaseCol < len(r) {
			row.Phase = dashToEmpty(r[phaseCol])
		}
		if stageCol >= 0 && stageCol < len(r) {
			row.Stage = dashToEmpty(r[stageCol])
		}
		if statusCol >= 0 && statusCol < len(r) {
			row.Status = dashToEmpty(r[statusCol])
		}
		if tsCol >= 0 && tsCol < len(r) {
			cell := strings.TrimSpace(r[tsCol])
			if cell != "" && cell != "-" {
				ts, err := time.Parse(time.RFC3339, cell)
				if err != nil {
					return nil, fmt.Errorf("invalid Timestamp value %q: %w", cell, err)
				}
				row.Timestamp = ts
			}
		}
		if summaryCol >= 0 && summaryCol < len(r) {
			row.Summary = r[summaryCol]
		}
		if inputsCol >= 0 && inputsCol < len(r) {
			row.InputArtifacts = splitInputArtifacts(r[inputsCol])
		}
		if checkpointCol >= 0 && checkpointCol < len(r) {
			row.Checkpoint = dashToEmpty(r[checkpointCol])
		}

		rows = append(rows, row)
	}
	return rows, nil
}

// dashToEmpty trims a cell and maps the document's "-" ("no value") marker
// to the empty string, never carrying it through as literal content.
func dashToEmpty(cell string) string {
	cell = strings.TrimSpace(cell)
	if cell == "-" {
		return ""
	}
	return cell
}

// splitInputArtifacts splits the Inputs column on commas, trimming each
// element. "-" and the empty cell both parse to nil.
func splitInputArtifacts(cell string) []string {
	cell = strings.TrimSpace(cell)
	if cell == "" || cell == "-" {
		return nil
	}
	parts := strings.Split(cell, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
