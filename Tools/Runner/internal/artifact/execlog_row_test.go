package artifact_test

// Tests for the WorkflowRow column of the Execution Log: rendered header and
// cells, Render -> Parse round trip, header-based lookup, tolerance of logs
// without the column or with non-canonical cells, and Store.Apply.

import (
	"context"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

const wantExecLogHeader = "| Seq | Agent | Phase | Stage | WorkflowRow | Status | Timestamp | Summary | Inputs | Checkpoint |"

const rowLogSeparator = "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |"

// squeezeSpaces collapses runs of whitespace so header comparison ignores padding.
func squeezeSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// rowTestState builds a state whose log holds a workflow entry (row 3) and an
// infrastructure entry (no row).
func rowTestState() domain.ArtifactState {
	ts := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	return domain.ArtifactState{
		Type:        "orchestration-artifact",
		RunID:       testRunID,
		Workflow:    "test",
		Started:     ts,
		LastUpdated: ts,
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "worker#1", Phase: "EXECUTION", Stage: "1",
				WorkflowRow: 3, Status: domain.StatusSUCCESS, Timestamp: ts, Summary: "work"},
			{Seq: 2, Agent: "checkpoint-manager-git#2", Phase: "EXECUTION", Stage: "1",
				WorkflowRow: domain.NoWorkflowRow, Status: domain.StatusSUCCESS, Timestamp: ts, Summary: "cp"},
		},
	}
}

func renderLines(t *testing.T, st domain.ArtifactState) []string {
	t.Helper()
	data, err := artifact.Render(st)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return strings.Split(string(data), "\n")
}

func TestRender_ExecutionLogHeader_HasWorkflowRowAfterStage(t *testing.T) {
	var header string
	for _, l := range renderLines(t, rowTestState()) {
		if strings.HasPrefix(l, "| Seq") {
			header = l
			break
		}
	}
	if header == "" {
		t.Fatal("no Execution Log header line rendered")
	}
	if got := squeezeSpaces(header); got != wantExecLogHeader {
		t.Errorf("header:\nwant %s\ngot  %s", wantExecLogHeader, got)
	}
}

func TestRender_ExecutionLogCells_DecimalRowOrDash(t *testing.T) {
	var cells [][]string
	for _, l := range renderLines(t, rowTestState()) {
		if strings.HasPrefix(l, "| 1 ") || strings.HasPrefix(l, "| 2 ") {
			var c []string
			for _, p := range strings.Split(strings.Trim(l, "|"), "|") {
				c = append(c, strings.TrimSpace(p))
			}
			cells = append(cells, c)
		}
	}
	if len(cells) != 2 {
		t.Fatalf("want 2 log rows, got %d", len(cells))
	}
	// WorkflowRow is the fifth column (index 4).
	if len(cells[0]) < 5 || cells[0][4] != "3" {
		t.Errorf("workflow entry cells: want WorkflowRow %q, got %v", "3", cells[0])
	}
	if len(cells[1]) < 5 || cells[1][4] != "-" {
		t.Errorf("infrastructure entry cells: want WorkflowRow %q, got %v", "-", cells[1])
	}
}

func TestRoundTrip_WorkflowRow_PreservedPerEntry(t *testing.T) {
	data, err := artifact.Render(rowTestState())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	parsed, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed.ExecutionLog) != 2 {
		t.Fatalf("want 2 entries, got %d", len(parsed.ExecutionLog))
	}
	if got := parsed.ExecutionLog[0].WorkflowRow; got != 3 {
		t.Errorf("workflow entry row: want 3, got %d", got)
	}
	if got := parsed.ExecutionLog[1].WorkflowRow; got != domain.NoWorkflowRow {
		t.Errorf("infrastructure entry row: want NoWorkflowRow, got %d", got)
	}
}

// artifactWithLog builds minimal artifact bytes around the supplied Execution
// Log table lines.
func artifactWithLog(logLines ...string) []byte {
	return []byte("---\n" +
		"type: orchestration-artifact\n" +
		"run_id: " + testRunID + "\n" +
		"workflow: test\n" +
		"workflow_version: \"1.0\"\n" +
		"task: \"test\"\n" +
		"started: 2026-01-01T00:00:00Z\n" +
		"last_updated: 2026-01-01T01:00:00Z\n" +
		"global_sequence: 1\n" +
		"checkpoints: disabled\n" +
		"current_state:\n" +
		"  phase: EXECUTION\n" +
		"  stage: null\n" +
		"  last_status: SUCCESS\n" +
		"  last_agent: \"worker#1\"\n" +
		"  error_code: null\n" +
		"---\n\n" +
		"<ExecutionLog type=\"core\">\n" +
		strings.Join(logLines, "\n") + "\n" +
		"</ExecutionLog>\n\n" +
		"<Artifacts type=\"core\">\n" +
		"| Artifact | Created In | Created By |\n" +
		"| -------- | ---------- | ---------- |\n" +
		"</Artifacts>\n\n" +
		"<WorkflowNotes type=\"core\">\n" +
		"| Seq | Note |\n" +
		"| --- | ---- |\n" +
		"</WorkflowNotes>\n")
}

func TestParse_LogWithoutWorkflowRowColumn_ParsesWithNoRow(t *testing.T) {
	data := artifactWithLog(
		"| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |",
		"| --- | --- | --- | --- | --- | --- | --- | --- | --- |",
		"| 1 | worker#1 | EXECUTION | 1 | SUCCESS | 2026-01-01T01:00:00Z | did it | - | - |",
	)
	parsed, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(parsed.ExecutionLog) != 1 {
		t.Fatalf("want 1 entry, got %d", len(parsed.ExecutionLog))
	}
	e := parsed.ExecutionLog[0]
	if e.WorkflowRow != domain.NoWorkflowRow {
		t.Errorf("want NoWorkflowRow, got %d", e.WorkflowRow)
	}
	if e.Agent != "worker#1" || e.Status != domain.StatusSUCCESS || e.Summary != "did it" {
		t.Errorf("other columns misread: %+v", e)
	}
}

func TestParse_WorkflowRowColumn_LocatedByHeaderNotPosition(t *testing.T) {
	// WorkflowRow is moved to the last position; Stage and Status stay readable.
	data := artifactWithLog(
		"| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint | WorkflowRow |",
		rowLogSeparator,
		"| 1 | worker#1 | EXECUTION | 1 | SUCCESS | 2026-01-01T01:00:00Z | did it | - | - | 4 |",
	)
	parsed, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	e := parsed.ExecutionLog[0]
	if e.WorkflowRow != 4 {
		t.Errorf("WorkflowRow: want 4, got %d", e.WorkflowRow)
	}
	if e.Stage != "1" || e.Status != domain.StatusSUCCESS {
		t.Errorf("other columns misread: Stage=%q Status=%q", e.Stage, e.Status)
	}
}

func TestParse_NonCanonicalWorkflowRowCells_ReadAsNoRow(t *testing.T) {
	cases := []struct{ name, cell string }{
		{"dash", "-"},
		{"empty", ""},
		{"letters", "abc"},
		{"zero", "0"},
		{"negative", "-2"},
		{"plus sign", "+3"},
		{"leading zeros", "007"},
		{"decimal point", "1.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := artifactWithLog(
				wantExecLogHeader,
				rowLogSeparator,
				"| 1 | worker#1 | EXECUTION | 1 | "+c.cell+" | SUCCESS | 2026-01-01T01:00:00Z | did it | - | - |",
			)
			parsed, err := artifact.Parse(data)
			if err != nil {
				t.Fatalf("Parse must not fail on cell %q: %v", c.cell, err)
			}
			if got := parsed.ExecutionLog[0].WorkflowRow; got != domain.NoWorkflowRow {
				t.Errorf("cell %q: want NoWorkflowRow, got %d", c.cell, got)
			}
		})
	}
}

func TestParse_CanonicalMultiDigitWorkflowRow_Read(t *testing.T) {
	data := artifactWithLog(
		wantExecLogHeader,
		rowLogSeparator,
		"| 1 | worker#1 | EXECUTION | 1 | 12 | SUCCESS | 2026-01-01T01:00:00Z | did it | - | - |",
	)
	parsed, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := parsed.ExecutionLog[0].WorkflowRow; got != 12 {
		t.Errorf("want 12, got %d", got)
	}
}

// ---- Store.Apply ----

func TestApply_StepWithRow_AppendsEntryWithThatRow(t *testing.T) {
	store, state := mustCreateStore(t)
	step := newTestStep(1, "worker#1", "EXECUTION", "1", domain.StatusSUCCESS, time.Now(), nil)
	step.WorkflowRow = 5

	after, err := store.Apply(context.Background(), state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	last := after.ExecutionLog[len(after.ExecutionLog)-1]
	if last.WorkflowRow != 5 {
		t.Errorf("entry WorkflowRow: want 5, got %d", last.WorkflowRow)
	}
}

func TestApply_StepWithoutRow_AppendsNoRowEntry(t *testing.T) {
	store, state := mustCreateStore(t)
	step := newTestStep(1, "checkpoint-manager-git#1", "EXECUTION", "1", domain.StatusSUCCESS, time.Now(), nil)
	step.IsInfrastructure = true

	after, err := store.Apply(context.Background(), state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	last := after.ExecutionLog[len(after.ExecutionLog)-1]
	if last.WorkflowRow != domain.NoWorkflowRow {
		t.Errorf("entry WorkflowRow: want NoWorkflowRow, got %d", last.WorkflowRow)
	}
}

func TestApply_RowPersistedToFile_ReadBackFromStore(t *testing.T) {
	store, state := mustCreateStore(t)
	ctx := context.Background()
	step := newTestStep(1, "worker#1", "EXECUTION", "1", domain.StatusSUCCESS, time.Now(), nil)
	step.WorkflowRow = 2
	if _, err := store.Apply(ctx, state, step); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	read, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	last := read.ExecutionLog[len(read.ExecutionLog)-1]
	if last.WorkflowRow != 2 {
		t.Errorf("row read back from file: want 2, got %d", last.WorkflowRow)
	}
}
