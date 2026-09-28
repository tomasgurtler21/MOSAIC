package artifact

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"mosaic-common/docformat"
	"mosaic-common/mdtable"
	"mosaic-run/internal/domain"
)

// Render serialises an ArtifactState to the canonical markdown bytes.
//
// Stage and Checkpoint fields that are "" in the ArtifactState are rendered as
// "-" in the execution log table. No other component ever sees or produces "-".
//
// Column widths in the output tables are at least as wide as the header and each
// cell value. Round-tripping a parsed file through Render produces identical bytes
// when the column widths in the original separator row were already >= all cell
// values.
func Render(state domain.ArtifactState) ([]byte, error) {
	var buf bytes.Buffer

	if !domain.IsValidRunID(state.RunID) {
		return nil, &domain.RefusalError{
			Component: "artifact",
			Reason:    "cannot render an artifact without a valid run_id",
			Cause:     &domain.RunIdentityError{Problem: runIDProblem(state.RunID), RunID: state.RunID},
		}
	}

	// --- Frontmatter ---
	buf.WriteString("---\n")
	buf.WriteString("type: orchestration-artifact\n")
	buf.WriteString("run_id: " + state.RunID + "\n")
	buf.WriteString("workflow: " + string(state.Workflow) + "\n")
	buf.WriteString("workflow_version: \"" + string(state.WorkflowVersion) + "\"\n")
	buf.WriteString("task: \"" + state.Task + "\"\n")
	buf.WriteString("started: " + state.Started.UTC().Format(time.RFC3339) + "\n")
	buf.WriteString("last_updated: " + state.LastUpdated.UTC().Format(time.RFC3339) + "\n")
	buf.WriteString("global_sequence: " + strconv.Itoa(state.GlobalSequence) + "\n")
	if state.Checkpoints {
		buf.WriteString("checkpoints: enabled\n")
	} else {
		buf.WriteString("checkpoints: disabled\n")
	}
	if state.Commits {
		buf.WriteString("commits: enabled\n")
	} else {
		buf.WriteString("commits: disabled\n")
	}
	if state.CommitBranch != "" {
		buf.WriteString("commit_branch: " + state.CommitBranch + "\n")
	}
	if state.ReviewLoopLimit > 0 {
		buf.WriteString("review_loop_limit: " + strconv.Itoa(state.ReviewLoopLimit) + "\n")
	}
	if len(state.InfrastructureOverrides) > 0 {
		buf.WriteString("infrastructure_overrides:\n")
		for _, ov := range state.InfrastructureOverrides {
			buf.WriteString("  " + ov.AgentName + ":\n")
			buf.WriteString("    triggers:\n")
			for _, tr := range ov.Triggers {
				buf.WriteString("      - trigger: " + tr.Trigger + "\n")
				if tr.Param != "" {
					buf.WriteString("        trigger_param: " + tr.Param + "\n")
				}
			}
		}
	}
	if len(state.InfraClassSelections) > 0 {
		buf.WriteString("infrastructure_selections:\n")
		classes := make([]string, 0, len(state.InfraClassSelections))
		for class := range state.InfraClassSelections {
			classes = append(classes, class)
		}
		sort.Slice(classes, func(i, j int) bool { return infraClassRank(classes[i]) < infraClassRank(classes[j]) || (infraClassRank(classes[i]) == infraClassRank(classes[j]) && classes[i] < classes[j]) })
		for _, class := range classes {
			buf.WriteString("  " + class + ": " + state.InfraClassSelections[class] + "\n")
		}
	}
	if state.Mode != domain.ExecutionModeUnset {
		buf.WriteString("runner_mode: " + string(state.Mode) + "\n")
		buf.WriteString("runner_pre_consultation: " + enabledDisabled(state.PreConsultation) + "\n")
		buf.WriteString("runner_manual_resolution: " + enabledDisabled(state.ManualResolution) + "\n")
	}
	for _, entry := range state.UnknownFrontmatter {
		for _, line := range entry.Lines {
			buf.WriteString(line + "\n")
		}
	}
	buf.WriteString("current_state:\n")
	cs := state.CurrentState
	buf.WriteString("  phase: " + renderNullable(cs.Phase) + "\n")
	buf.WriteString("  stage: " + renderNullable(cs.Stage) + "\n")
	buf.WriteString("  last_status: " + renderNullable(string(cs.LastStatus)) + "\n")
	buf.WriteString("  last_agent: " + renderNullableQuoted(cs.LastAgent) + "\n")
	buf.WriteString("  error_code: " + renderNullable(string(cs.ErrorCode)) + "\n")
	buf.WriteString("---\n")

	// --- Body ---
	buf.WriteString("\n")

	// ExecutionLog section
	execLogOpen, err := docformat.RenderOpenTagLine(docformat.NodeSection, "ExecutionLog", "")
	if err != nil {
		return nil, fmt.Errorf("artifact: render ExecutionLog open tag: %w", err)
	}
	execLogClose, err := docformat.RenderCloseTagLine("ExecutionLog")
	if err != nil {
		return nil, fmt.Errorf("artifact: render ExecutionLog close tag: %w", err)
	}
	buf.Write(execLogOpen)
	buf.Write(renderExecutionLog(state.ExecutionLog))
	buf.Write(execLogClose)

	buf.WriteString("\n")

	// Artifacts section
	artifactsOpen, err := docformat.RenderOpenTagLine(docformat.NodeSection, "Artifacts", "")
	if err != nil {
		return nil, fmt.Errorf("artifact: render Artifacts open tag: %w", err)
	}
	artifactsClose, err := docformat.RenderCloseTagLine("Artifacts")
	if err != nil {
		return nil, fmt.Errorf("artifact: render Artifacts close tag: %w", err)
	}
	buf.Write(artifactsOpen)
	buf.Write(renderArtifactRegistry(state.ArtifactRegistry))
	buf.Write(artifactsClose)

	buf.WriteString("\n")

	// WorkflowNotes section
	workflowNotesOpen, err := docformat.RenderOpenTagLine(docformat.NodeSection, "WorkflowNotes", "")
	if err != nil {
		return nil, fmt.Errorf("artifact: render WorkflowNotes open tag: %w", err)
	}
	workflowNotesClose, err := docformat.RenderCloseTagLine("WorkflowNotes")
	if err != nil {
		return nil, fmt.Errorf("artifact: render WorkflowNotes close tag: %w", err)
	}
	buf.Write(workflowNotesOpen)
	buf.Write(renderWorkflowNotes(state.WorkflowNotes))
	buf.Write(workflowNotesClose)

	return buf.Bytes(), nil
}

// TruncateSummary applies the head-50 + tail-50 truncation rule from the artifact
// format spec to the given summary string.
//
// Messages of 100 characters or fewer are returned unchanged.
// Messages longer than 100 characters are truncated: the first 50 and last 50
// characters are kept, joined by " ... " (space, three periods, space), so the
// rendered artifact stays ASCII.
//
// Pipe characters ("|") and newlines are stripped from the result because they
// are invalid inside a markdown table cell. Stripping is applied before truncation
// so that the 50/50 split is counted on the already-clean string.
func TruncateSummary(s string) string {
	// Strip pipe characters and newlines.
	clean := strings.NewReplacer("|", "", "\n", "", "\r", "").Replace(s)

	if len(clean) <= 100 {
		return clean
	}

	head := clean[:50]
	tail := clean[len(clean)-50:]
	return head + " ... " + tail
}

// renderExecutionLog renders the execution log entries as a markdown table.
func renderExecutionLog(entries []domain.ExecutionLogEntry) []byte {
	headers := []string{"Seq", "Agent", "Phase", "Stage", "Status", "Timestamp", "Summary", "Inputs", "Checkpoint"}
	t := mdtable.Table{Header: headers}

	for _, e := range entries {
		stage := e.Stage
		if stage == "" {
			stage = "-"
		}
		inputs := e.Inputs
		if inputs == "" {
			inputs = "-"
		}
		checkpoint := e.Checkpoint
		if checkpoint == "" {
			checkpoint = "-"
		}
		row := []string{
			strconv.Itoa(e.Seq),
			e.Agent,
			e.Phase,
			stage,
			string(e.Status),
			e.Timestamp.UTC().Format(time.RFC3339),
			e.Summary,
			inputs,
			checkpoint,
		}
		t = t.AppendRow(row)
	}

	return t.Render()
}

// renderArtifactRegistry renders the artifact registry entries as a markdown table.
func renderArtifactRegistry(entries []domain.ArtifactRegistryEntry) []byte {
	headers := []string{"Artifact", "Created In", "Created By"}
	t := mdtable.Table{Header: headers}

	for _, e := range entries {
		row := []string{e.Artifact, e.CreatedIn, e.CreatedBy}
		t = t.AppendRow(row)
	}

	return t.Render()
}

// renderWorkflowNotes renders the workflow notes as a markdown table.
func renderWorkflowNotes(notes []domain.WorkflowNote) []byte {
	headers := []string{"Seq", "Note"}
	t := mdtable.Table{Header: headers}

	for _, n := range notes {
		row := []string{strconv.Itoa(n.Seq), n.Note}
		t = t.AppendRow(row)
	}

	return t.Render()
}

// enabledDisabled renders a boolean as the "enabled"/"disabled" frontmatter value.
func enabledDisabled(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

// infraClassRank orders infrastructure_selections keys checkpoint, commit, restore.
func infraClassRank(class string) int {
	switch class {
	case "checkpoint":
		return 0
	case "commit":
		return 1
	case "restore":
		return 2
	}
	return 3
}

// runIDProblem classifies an unusable run_id.
func runIDProblem(runID string) domain.RunIdentityProblem {
	if runID == "" {
		return domain.RunIdentityEmpty
	}
	return domain.RunIdentityMalformed
}

// renderNullable renders a string value, replacing "" with "null".
func renderNullable(s string) string {
	if s == "" {
		return "null"
	}
	return s
}

// renderNullableQuoted renders a string value as double-quoted YAML, replacing "" with "null".
func renderNullableQuoted(s string) string {
	if s == "" {
		return "null"
	}
	return `"` + s + `"`
}
