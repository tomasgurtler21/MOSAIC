package workflow_test

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/workflow"
)

func TestParse_QuickFix_RowCount(t *testing.T) {
	table := mustParseQuickFix(t)

	if len(table.Rows) != 4 {
		t.Errorf("want 4 rows, got %d", len(table.Rows))
	}
}

func TestParse_QuickFix_RowIndices_ZeroBasedConsecutive(t *testing.T) {
	// Indices must be 0, 1, 2, 3 in declaration order.
	table := mustParseQuickFix(t)

	for i, row := range table.Rows {
		if row.Index != i {
			t.Errorf("Rows[%d].Index: want %d, got %d", i, i, row.Index)
		}
	}
}

func TestParse_QuickFix_HITL_False_ForCrossmark(t *testing.T) {
	// Row 1 (plan-review) has FALSE in the HITL column → HITL must be false.
	table := mustParseQuickFix(t)

	if table.Rows[1].HITL {
		t.Errorf("Rows[1].HITL: want false (FALSE plan-review), got true")
	}
}

func TestParse_QuickFix_HITL_True_ForCheckmark(t *testing.T) {
	// Row 0 (planner-tdd-soft) has TRUE in the HITL column → HITL must be true.
	table := mustParseQuickFix(t)

	if !table.Rows[0].HITL {
		t.Errorf("Rows[0].HITL: want true (TRUE planner-tdd-soft), got false")
	}
}

func TestParse_QuickFix_NonExecutionRow_NotStaged(t *testing.T) {
	// PLANNING rows must not be flagged as staged.
	table := mustParseQuickFix(t)

	if table.Rows[0].PhaseParsed.IsStaged {
		t.Errorf("Rows[0] (PLANNING): PhaseParsed.IsStaged must be false for a non-EXECUTION phase")
	}
}

func TestParse_QuickFix_NonExecutionRow_PhaseName(t *testing.T) {
	// For a PLANNING row, PhaseParsed.Name must be "PLANNING".
	table := mustParseQuickFix(t)

	got := table.Rows[0].PhaseParsed.Name
	if got != "PLANNING" {
		t.Errorf("Rows[0].PhaseParsed.Name: want %q, got %q", "PLANNING", got)
	}
}

func TestParse_QuickFix_NonExecutionRow_Phase_LiteralPreserved(t *testing.T) {
	// The Phase field must carry the literal text from the table cell.
	table := mustParseQuickFix(t)

	got := table.Rows[0].Phase
	if got != "PLANNING" {
		t.Errorf("Rows[0].Phase: want %q, got %q", "PLANNING", got)
	}
}

func TestParse_QuickFix_ExecutionRow_IsStaged(t *testing.T) {
	// Row 2 (EXECUTION.[StageNumber]) must be flagged as staged.
	table := mustParseQuickFix(t)

	if !table.Rows[2].PhaseParsed.IsStaged {
		t.Errorf("Rows[2] (EXECUTION.[StageNumber]): PhaseParsed.IsStaged must be true")
	}
}

func TestParse_QuickFix_ExecutionRow_PhaseName(t *testing.T) {
	// The PhaseParsed.Name for an EXECUTION.[StageNumber] row must be "EXECUTION".
	table := mustParseQuickFix(t)

	got := table.Rows[2].PhaseParsed.Name
	if got != "EXECUTION" {
		t.Errorf("Rows[2].PhaseParsed.Name: want %q, got %q", "EXECUTION", got)
	}
}

func TestParse_QuickFix_ExecutionRow_Phase_LiteralPreserved(t *testing.T) {
	// The Phase field must preserve the literal "EXECUTION.[StageNumber]" string.
	table := mustParseQuickFix(t)

	got := table.Rows[2].Phase
	if got != "EXECUTION.[StageNumber]" {
		t.Errorf("Rows[2].Phase: want %q, got %q", "EXECUTION.[StageNumber]", got)
	}
}

func TestParse_QuickFix_GlobPattern_PreservedVerbatim_OutputArtifact(t *testing.T) {
	// Row 0 output: "Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md".
	// The Stage-*/... glob patterns must survive verbatim in OutputArtifacts.
	table := mustParseQuickFix(t)

	artifacts := table.Rows[0].OutputArtifacts
	found := false
	for _, a := range artifacts {
		if a == "Stage-*/Plan.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Rows[0].OutputArtifacts: want %q to be present verbatim, got %v", "Stage-*/Plan.md", artifacts)
	}
}

func TestParse_QuickFix_TemplatePattern_PreservedVerbatim_InputArtifact(t *testing.T) {
	// Row 2 input: "Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md".
	// The Stage-{StageNumber}/... template must survive verbatim.
	table := mustParseQuickFix(t)

	artifacts := table.Rows[2].InputArtifacts
	found := false
	for _, a := range artifacts {
		if a == "Stage-{StageNumber}/Plan.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Rows[2].InputArtifacts: want %q to be present verbatim, got %v", "Stage-{StageNumber}/Plan.md", artifacts)
	}
}

func TestParse_QuickFix_CommaSeparatedArtifacts_SplitAndTrimmed(t *testing.T) {
	// Row 0 output has three comma-separated paths → OutputArtifacts must have 3 elements.
	table := mustParseQuickFix(t)

	got := len(table.Rows[0].OutputArtifacts)
	if got != 3 {
		t.Errorf("Rows[0].OutputArtifacts: want 3 elements, got %d: %v", got, table.Rows[0].OutputArtifacts)
	}
}

func TestParse_QuickFix_DashInput_ProducesEmptyArtifacts(t *testing.T) {
	// Row 0 has Input = "-" which means no input artifacts.
	// InputArtifacts must be empty (nil or zero-length slice).
	table := mustParseQuickFix(t)

	if len(table.Rows[0].InputArtifacts) != 0 {
		t.Errorf("Rows[0].InputArtifacts: want empty (Input is \"-\"), got %v", table.Rows[0].InputArtifacts)
	}
}

func TestParse_QuickFix_OnSuccess_ColumnPresent(t *testing.T) {
	// The On Success column exists in the quick-fix table, so ColumnPresent must be true.
	table := mustParseQuickFix(t)

	if !table.Rows[0].OnSuccess.ColumnPresent {
		t.Error("Rows[0].OnSuccess.ColumnPresent: want true (column exists in table), got false")
	}
}

func TestParse_QuickFix_OnSuccess_Value_WhenNotDash(t *testing.T) {
	// Row 0 On Success = "plan-review" → Value must be "plan-review".
	table := mustParseQuickFix(t)

	got := table.Rows[0].OnSuccess.Value
	if got != "plan-review" {
		t.Errorf("Rows[0].OnSuccess.Value: want %q, got %q", "plan-review", got)
	}
}

func TestParse_QuickFix_OnFindings_Dash_IsNoHint(t *testing.T) {
	// Row 0 On Findings = "-" → ColumnPresent true, Value "".
	table := mustParseQuickFix(t)

	hint := table.Rows[0].OnFindings
	if !hint.ColumnPresent {
		t.Error("Rows[0].OnFindings.ColumnPresent: want true (column exists), got false")
	}
	if hint.Value != "" {
		t.Errorf("Rows[0].OnFindings.Value: want %q for dash (no hint), got %q", "", hint.Value)
	}
}

func TestParse_QuickFix_OnFindings_Value_WhenNotDash(t *testing.T) {
	// Row 1 On Findings = "planner-tdd-soft" → Value must be "planner-tdd-soft".
	table := mustParseQuickFix(t)

	got := table.Rows[1].OnFindings.Value
	if got != "planner-tdd-soft" {
		t.Errorf("Rows[1].OnFindings.Value: want %q, got %q", "planner-tdd-soft", got)
	}
}

func TestParse_QuickFix_AgentIdentifier_PreservedVerbatim(t *testing.T) {
	// The Subagent column value must be stored verbatim in Agent.
	table := mustParseQuickFix(t)

	got := table.Rows[0].Agent
	if got != "planner-tdd-soft" {
		t.Errorf("Rows[0].Agent: want %q, got %q", "planner-tdd-soft", got)
	}
}

func TestParse_QuickFix_WorkflowInfo_CarriedIntoTable(t *testing.T) {
	// The WorkflowInfo passed to Parse must appear unchanged in RoutingTable.Info.
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	table, err := workflow.Parse([]byte(quickFixContent), info)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if table.Info.ID != info.ID {
		t.Errorf("RoutingTable.Info.ID: want %q, got %q", info.ID, table.Info.ID)
	}
	if table.Info.Version != info.Version {
		t.Errorf("RoutingTable.Info.Version: want %q, got %q", info.Version, table.Info.Version)
	}
}
