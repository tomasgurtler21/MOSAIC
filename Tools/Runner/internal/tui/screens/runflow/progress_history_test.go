package runflow

// progress_history_test.go verifies the history methods of ProgressScreen:
// SetHistory replaces every row with a copy of the rows it is given (so a
// restart never duplicates rows already shown), leaves no row running, leaves
// stop state, status line and the rows appended afterwards intact, and Rows
// returns a copy in display order.

import (
	"reflect"
	"strings"
	"testing"
)

func historySample() []ProgressRow {
	return []ProgressRow{
		{AgentInstance: "planner#1", Phase: "PLANNING", Stage: "", Status: "SUCCESS"},
		{AgentInstance: "builder#2", Phase: "EXECUTION", Stage: "Implementation.1", Status: "BLOCKED"},
	}
}

func TestProgressScreen_SetHistory_ReplacesExistingRows(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())
	s.AppendRow(ProgressRow{AgentInstance: "stale#9", Phase: "OLD", Status: "running"})
	s.CompleteRow("SUCCESS")

	s.SetHistory(historySample())

	if got, want := s.Rows(), historySample(); !reflect.DeepEqual(got, want) {
		t.Errorf("Rows() = %+v after SetHistory, want exactly the history rows %+v", got, want)
	}
}

func TestProgressScreen_SetHistory_Empty_ClearsRows(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())
	s.AppendRow(ProgressRow{AgentInstance: "stale#9", Status: "SUCCESS"})

	s.SetHistory(nil)

	if got := s.Rows(); len(got) != 0 {
		t.Errorf("Rows() = %+v after SetHistory(nil), want no rows", got)
	}
}

func TestProgressScreen_SetHistory_LeavesNoRowRunning(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())
	s.AppendRow(ProgressRow{AgentInstance: "live#5", Phase: "EXECUTION", Status: "running"})

	s.SetHistory(historySample())
	// A completion notice that arrives with no running row must not rewrite
	// the status of a history row.
	s.CompleteRow("SUCCESS")

	got := s.Rows()
	if len(got) != 2 || got[1].Status != "BLOCKED" {
		t.Errorf("Rows() = %+v after CompleteRow on a history-only screen, want history unchanged", got)
	}
}

func TestProgressScreen_AppendRow_AfterSetHistory_AppendsAfterHistory(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())
	s.SetHistory(historySample())

	s.AppendRow(ProgressRow{AgentInstance: "next#3", Phase: "EXECUTION", Status: "running"})
	s.CompleteRow("SUCCESS")

	got := s.Rows()
	if len(got) != 3 {
		t.Fatalf("len(Rows()) = %d, want 3 (2 history + 1 live): %+v", len(got), got)
	}
	if got[2].AgentInstance != "next#3" || got[2].Status != "SUCCESS" {
		t.Errorf("live row = %+v, want next#3 completed with SUCCESS after the history rows", got[2])
	}
	if got[0].AgentInstance != "planner#1" || got[1].AgentInstance != "builder#2" {
		t.Errorf("history rows reordered or rewritten: %+v", got[:2])
	}
}

func TestProgressScreen_SetHistory_CopiesInput(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())
	in := historySample()
	s.SetHistory(in)

	in[0].AgentInstance = "mutated#0"

	if got := s.Rows(); got[0].AgentInstance != "planner#1" {
		t.Errorf("Rows()[0].AgentInstance = %q after the caller mutated its slice, want planner#1", got[0].AgentInstance)
	}
}

func TestProgressScreen_Rows_ReturnsCopy(t *testing.T) {
	s := NewProgressScreen(80, 24, progressStyles())
	s.SetHistory(historySample())

	rows := s.Rows()
	if len(rows) == 0 {
		t.Fatal("precondition: Rows() is empty after SetHistory")
	}
	rows[0].AgentInstance = "mutated#0"

	if got := s.Rows(); got[0].AgentInstance != "planner#1" {
		t.Errorf("Rows()[0].AgentInstance = %q after mutating a returned slice, want planner#1", got[0].AgentInstance)
	}
}

func TestProgressScreen_SetHistory_KeepsStopStateAndStatus(t *testing.T) {
	s := armedProgressScreen()
	s.SetStatus("keep this status", true)
	started := s.startTime

	s.SetHistory(historySample())

	if !s.GracefulStop() {
		t.Error("GracefulStop() = false after SetHistory, want the stop latch untouched")
	}
	if s.status != "keep this status" || !s.statusErr {
		t.Errorf("status = %q (err=%v) after SetHistory, want it untouched", s.status, s.statusErr)
	}
	if !s.startTime.Equal(started) {
		t.Errorf("startTime changed from %v to %v, want it untouched", started, s.startTime)
	}
}

func TestProgressScreen_SetHistory_RowsAppearInView(t *testing.T) {
	s := NewProgressScreen(120, 24, progressStyles())
	s.SetHistory([]ProgressRow{{AgentInstance: "planner#1", Phase: "PLANNING", Status: "SUCCESS"}})

	view := s.View()
	for _, want := range []string{"planner#1", "SUCCESS"} {
		if !strings.Contains(view, want) {
			t.Errorf("view does not show %q for a history row:\n%s", want, view)
		}
	}
}
