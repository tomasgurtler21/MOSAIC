package artifact

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"time"

	"mosaic-common/docformat"
	"mosaic-common/mdtable"
	"mosaic-run/internal/domain"
)

// extractSectionContent extracts the bytes between a section's open and close tags.
// Returns (content, true) on success, (nil, false) if the section is not found.
func extractSectionContent(body []byte, sectionName string) ([]byte, bool) {
	openTag, err := docformat.RenderOpenTagLine(docformat.NodeSection, sectionName, "")
	if err != nil {
		return nil, false
	}
	closeTag, err := docformat.RenderCloseTagLine(sectionName)
	if err != nil {
		return nil, false
	}

	openEnd, ok := findTagLineEnd(body, bytes.TrimSpace(openTag), 0)
	if !ok {
		return nil, false
	}
	closeStart, ok := findTagLineStart(body, bytes.TrimSpace(closeTag), openEnd)
	if !ok {
		return nil, false
	}
	return dropBlankLines(body[openEnd:closeStart]), true
}

// parseExecutionLog parses the execution log table from section content.
func parseExecutionLog(content []byte) ([]domain.ExecutionLogEntry, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, nil
	}
	t, err := mdtable.ParseStrict(content)
	if err != nil {
		return nil, err
	}
	if len(t.Rows) == 0 {
		return nil, nil
	}

	seqCol := t.Column("Seq")
	agentCol := t.Column("Agent")
	phaseCol := t.Column("Phase")
	stageCol := t.Column("Stage")
	statusCol := t.Column("Status")
	tsCol := t.Column("Timestamp")
	summaryCol := t.Column("Summary")
	inputsCol := t.Column("Inputs")
	checkpointCol := t.Column("Checkpoint")
	workflowRowCol := t.Column(ExecLogWorkflowRowHeader)

	var entries []domain.ExecutionLogEntry
	for _, row := range t.Rows {
		entry := domain.ExecutionLogEntry{}

		if seqCol >= 0 {
			n, err := strconv.Atoi(cellText(row[seqCol]))
			if err == nil {
				entry.Seq = n
			}
		}
		if agentCol >= 0 {
			entry.Agent = cellText(row[agentCol])
		}
		if phaseCol >= 0 {
			entry.Phase = cellText(row[phaseCol])
		}
		if stageCol >= 0 {
			v := cellText(row[stageCol])
			if v == "-" {
				v = ""
			}
			entry.Stage = v
		}
		if workflowRowCol >= 0 {
			entry.WorkflowRow = parseWorkflowRowCell(row[workflowRowCol])
		}
		if statusCol >= 0 {
			entry.Status = domain.StatusCode(cellText(row[statusCol]))
		}
		if tsCol >= 0 {
			ts, err := time.Parse(time.RFC3339, cellText(row[tsCol]))
			if err == nil {
				entry.Timestamp = ts
			}
		}
		if summaryCol >= 0 {
			entry.Summary = cellText(row[summaryCol])
			if entry.Status == domain.StatusBLOCKED {
				entry.Summary, entry.ErrorCode = domain.SplitErrorMarker(entry.Summary)
			}
		}
		if inputsCol >= 0 {
			v := cellText(row[inputsCol])
			if v == "-" {
				v = ""
			}
			entry.Inputs = v
		}
		if checkpointCol >= 0 {
			v := cellText(row[checkpointCol])
			if v == "-" {
				v = ""
			}
			entry.Checkpoint = v
		}

		entries = append(entries, entry)
	}
	return entries, nil
}

// parseArtifactRegistry parses the artifact registry table from section content.
func parseArtifactRegistry(content []byte) ([]domain.ArtifactRegistryEntry, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, nil
	}
	t, err := mdtable.ParseStrict(content)
	if err != nil {
		return nil, err
	}
	if len(t.Rows) == 0 {
		return nil, nil
	}

	artCol := t.Column("Artifact")
	createdInCol := t.Column("Created In")
	createdByCol := t.Column("Created By")

	var entries []domain.ArtifactRegistryEntry
	for _, row := range t.Rows {
		entry := domain.ArtifactRegistryEntry{}
		if artCol >= 0 {
			entry.Artifact = cellText(row[artCol])
		}
		if createdInCol >= 0 {
			entry.CreatedIn = cellText(row[createdInCol])
		}
		if createdByCol >= 0 {
			entry.CreatedBy = cellText(row[createdByCol])
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// parseWorkflowNotes parses the workflow notes table from section content.
func parseWorkflowNotes(content []byte) ([]domain.WorkflowNote, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, nil
	}
	t, err := mdtable.ParseStrict(content)
	if err != nil {
		return nil, err
	}
	if t.Column("Seq") < 0 || t.Column("Note") < 0 {
		return nil, errors.New("workflow notes table must have the columns Seq and Note")
	}
	if len(t.Rows) == 0 {
		return nil, nil
	}

	seqCol := t.Column("Seq")
	noteCol := t.Column("Note")

	var notes []domain.WorkflowNote
	for _, row := range t.Rows {
		note := domain.WorkflowNote{}
		if seqCol >= 0 {
			n, err := strconv.Atoi(cellText(row[seqCol]))
			if err == nil {
				note.Seq = n
			}
		}
		if noteCol >= 0 {
			note.Note = cellText(row[noteCol])
		}
		notes = append(notes, note)
	}
	return notes, nil
}

// parseWorkflowRowCell reads a WorkflowRow cell. Only a canonical positive
// decimal number is a row; "-", empty and any other text mean no row.
func parseWorkflowRowCell(cell string) domain.WorkflowRow {
	v := strings.TrimSpace(cell)
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || strconv.Itoa(n) != v {
		return domain.NoWorkflowRow
	}
	return domain.WorkflowRow(n)
}
