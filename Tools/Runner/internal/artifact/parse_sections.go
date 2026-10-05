package artifact

import (
	"bytes"
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

	openIdx := bytes.Index(body, openTag)
	if openIdx < 0 {
		return nil, false
	}

	contentStart := openIdx + len(openTag)
	remaining := body[contentStart:]

	closeIdx := bytes.Index(remaining, closeTag)
	if closeIdx < 0 {
		return nil, false
	}

	return remaining[:closeIdx], true
}

// parseExecutionLog parses the execution log table from section content.
func parseExecutionLog(content []byte) ([]domain.ExecutionLogEntry, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, nil
	}
	t, err := mdtable.Parse(content)
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
			n, err := strconv.Atoi(strings.TrimSpace(row[seqCol]))
			if err == nil {
				entry.Seq = n
			}
		}
		if agentCol >= 0 {
			entry.Agent = strings.TrimSpace(row[agentCol])
		}
		if phaseCol >= 0 {
			entry.Phase = strings.TrimSpace(row[phaseCol])
		}
		if stageCol >= 0 {
			v := strings.TrimSpace(row[stageCol])
			if v == "-" {
				v = ""
			}
			entry.Stage = v
		}
		if workflowRowCol >= 0 {
			entry.WorkflowRow = parseWorkflowRowCell(row[workflowRowCol])
		}
		if statusCol >= 0 {
			entry.Status = domain.StatusCode(strings.TrimSpace(row[statusCol]))
		}
		if tsCol >= 0 {
			ts, err := time.Parse(time.RFC3339, strings.TrimSpace(row[tsCol]))
			if err == nil {
				entry.Timestamp = ts
			}
		}
		if summaryCol >= 0 {
			entry.Summary = strings.TrimSpace(row[summaryCol])
		}
		if inputsCol >= 0 {
			v := strings.TrimSpace(row[inputsCol])
			if v == "-" {
				v = ""
			}
			entry.Inputs = v
		}
		if checkpointCol >= 0 {
			v := strings.TrimSpace(row[checkpointCol])
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
	t, err := mdtable.Parse(content)
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
			entry.Artifact = strings.TrimSpace(row[artCol])
		}
		if createdInCol >= 0 {
			entry.CreatedIn = strings.TrimSpace(row[createdInCol])
		}
		if createdByCol >= 0 {
			entry.CreatedBy = strings.TrimSpace(row[createdByCol])
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
	t, err := mdtable.Parse(content)
	if err != nil {
		return nil, err
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
			n, err := strconv.Atoi(strings.TrimSpace(row[seqCol]))
			if err == nil {
				note.Seq = n
			}
		}
		if noteCol >= 0 {
			note.Note = strings.TrimSpace(row[noteCol])
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
