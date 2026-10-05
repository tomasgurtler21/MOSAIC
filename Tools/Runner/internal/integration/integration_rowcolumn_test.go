package integration_test

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/harness"
)

// ===== Workflow table with a deployed Row column =====

const rowColumnWorkflowName = "row-column"

// rowColumnOrchestrator renders an inline orchestrator whose workflow table has
// three sequential PLANNING rows. When rowValues is non-nil the table carries a
// first-column Row (the shape deployment emits), one value per data row.
func rowColumnOrchestrator(rowValues []string) string {
	rows := [][]string{
		{"PLANNING", "agent-a", "FALSE", "agent-b", "-", "-", "a.md"},
		{"PLANNING", "agent-b", "FALSE", "agent-c", "-", "a.md", "b.md"},
		{"PLANNING", "agent-c", "FALSE", "COMPLETE", "-", "b.md", "c.md"},
	}
	header := "| Phase | Subagent | HITL | On Success | On Findings | Input | Output |\n"
	sep := "|-------|----------|:----:|------------|-------------|-------|--------|\n"
	if rowValues != nil {
		header = "| Row " + header
		sep = "|-----" + sep
	}

	var b strings.Builder
	b.WriteString(`<Workflow type="core" name="` + rowColumnWorkflowName + `" version="1.0">` + "\n")
	b.WriteString("## Row Column Workflow\n\n")
	b.WriteString(header)
	b.WriteString(sep)
	for i, r := range rows {
		prefix := "| "
		if rowValues != nil {
			prefix = "| " + rowValues[i] + " | "
		}
		b.WriteString(prefix + strings.Join(r, " | ") + " |\n")
	}
	b.WriteString("</Workflow>\n")
	return b.String()
}

// runRowColumnWorkflow runs the orchestrator built from rowValues and returns
// the outcome together with the mock adapter holding the recorded dispatches.
func runRowColumnWorkflow(t *testing.T, rowValues []string) (domain.RunOutcome, error, *harness.MockAdapter) {
	t.Helper()
	dir := t.TempDir()
	orchPath := writeOrchFile(t, dir, "orchestrator.md", rowColumnOrchestrator(rowValues))
	for _, id := range []string{"agent-a", "agent-b", "agent-c"} {
		writeAgentFile(t, dir, id)
	}

	f := harness.NewMockAdapter()
	for i, id := range []string{"agent-a", "agent-b", "agent-c"} {
		f.Queue(id, harness.ScriptedEntry{Response: &domain.ProtocolResponse{
			AgentInstanceID: id + "#" + string(rune('1'+i)),
			StatusCode:      domain.StatusSUCCESS,
			StatusMessage:   id + " done",
		}})
	}

	sess := newSession(f, filepath.Join(dir, "Orchestration.md"))
	got, err := sess.Start(context.Background(), domain.RunConfig{
		RunID:                integrationRunID,
		OrchestratorFilePath: orchPath,
		WorkflowID:           rowColumnWorkflowName,
		Task:                 "task",
		IsNewRun:             true,
		RunSettings:          domain.RunSettings{Mode: domain.ExecutionModeAuto},
		RunFolder:            dir,
	})
	return got, err, f
}

// TestIntegration_RowColumnTable_DispatchesSameSequenceAsWithoutColumn verifies
// that a workflow table carrying a first-column Row runs end to end exactly like
// the same table without it: same agents, same order, same artifacts.
func TestIntegration_RowColumnTable_DispatchesSameSequenceAsWithoutColumn(t *testing.T) {
	plainOut, plainErr, plain := runRowColumnWorkflow(t, nil)
	requireRunStatus(t, plainOut, plainErr, domain.RunCompleted)

	gotOut, gotErr, withRow := runRowColumnWorkflow(t, []string{"1", "2", "3"})
	requireRunStatus(t, gotOut, gotErr, domain.RunCompleted)

	want := dispatchSummary(plain.Invocations())
	got := dispatchSummary(withRow.Invocations())
	if len(want) != 3 {
		t.Fatalf("baseline run without Row column: want 3 dispatches, got %d", len(want))
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dispatch sequence differs with a Row column:\nwith:    %v\nwithout: %v", got, want)
	}
}

// TestIntegration_RowColumnTable_WrongValue_RefusedBeforeAnyDispatch verifies
// that a hand-edited Row column stops the run at admission, naming the workflow,
// before any agent is dispatched.
func TestIntegration_RowColumnTable_WrongValue_RefusedBeforeAnyDispatch(t *testing.T) {
	got, err, f := runRowColumnWorkflow(t, []string{"1", "3", "2"})

	msg := requireRefused(t, got, err)

	if !strings.Contains(msg, rowColumnWorkflowName) {
		t.Errorf("want refusal message to name workflow %q, got %q", rowColumnWorkflowName, msg)
	}
	if n := len(f.Invocations()); n != 0 {
		t.Errorf("want no dispatches for a refused workflow, got %d", n)
	}
}

// dispatchSummary reduces recorded invocations to the agent and input artifacts
// of each dispatch, in order.
func dispatchSummary(invs []harness.Invocation) []string {
	out := make([]string, 0, len(invs))
	for _, inv := range invs {
		out = append(out, inv.Agent.Identifier+" <- "+strings.Join(inv.Request.InputArtifacts, ","))
	}
	return out
}
