package workflow_test

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/workflow"
)

func TestParse_MinimalTable_OnSuccess_ColumnPresentFalse(t *testing.T) {
	// A table with no On Success column must have ColumnPresent == false on every row.
	info := domain.WorkflowInfo{ID: "minimal", Version: "1.0"}
	table, err := workflow.Parse([]byte(minimalContent), info)
	if err != nil {
		t.Fatalf("Parse(minimal): %v", err)
	}

	for i, row := range table.Rows {
		if row.OnSuccess.ColumnPresent {
			t.Errorf("Rows[%d].OnSuccess.ColumnPresent: want false (column absent), got true", i)
		}
	}
}

func TestParse_MinimalTable_OnFindings_ColumnPresentFalse(t *testing.T) {
	// A table with no On Findings column must have ColumnPresent == false on every row.
	info := domain.WorkflowInfo{ID: "minimal", Version: "1.0"}
	table, err := workflow.Parse([]byte(minimalContent), info)
	if err != nil {
		t.Fatalf("Parse(minimal): %v", err)
	}

	for i, row := range table.Rows {
		if row.OnFindings.ColumnPresent {
			t.Errorf("Rows[%d].OnFindings.ColumnPresent: want false (column absent), got true", i)
		}
	}
}

func TestParse_QuickFix_OnFindings_ColumnPresent_WhenColumnExists(t *testing.T) {
	// When the On Findings column exists, ColumnPresent must be true regardless of cell value.
	table := mustParseQuickFix(t)

	for i, row := range table.Rows {
		if !row.OnFindings.ColumnPresent {
			t.Errorf("Rows[%d].OnFindings.ColumnPresent: want true (column exists), got false", i)
		}
	}
}

func TestParse_QuickFix_OnSuccess_ColumnPresent_WhenColumnExists(t *testing.T) {
	table := mustParseQuickFix(t)

	for i, row := range table.Rows {
		if !row.OnSuccess.ColumnPresent {
			t.Errorf("Rows[%d].OnSuccess.ColumnPresent: want true (column exists), got false", i)
		}
	}
}

func TestParse_OnSuccessDash_ColumnPresent_ValueEmpty(t *testing.T) {
	// On Success column value "-" must produce OptionalHint{ColumnPresent: true, Value: ""},
	// the same no-hint behavior as "-" in the On Findings column and as an absent column
	// value. This verifies the symmetric handling required by KC-8/FR-27.
	info := domain.WorkflowInfo{ID: "on-success-dash", Version: "1.0"}
	table, err := workflow.Parse([]byte(onSuccessDashContent), info)
	if err != nil {
		t.Fatalf("Parse(on-success-dash): unexpected error: %v", err)
	}

	hint := table.Rows[0].OnSuccess
	if !hint.ColumnPresent {
		t.Error("Rows[0].OnSuccess.ColumnPresent: want true (column exists), got false")
	}
	if hint.Value != "" {
		t.Errorf("Rows[0].OnSuccess.Value: want %q for dash cell (no hint), got %q", "", hint.Value)
	}
}

func TestParse_OutputDash_ProducesEmptyArtifacts(t *testing.T) {
	// Output column value "-" must produce an empty OutputArtifacts slice,
	// symmetric with the Input column behavior (TestParse_QuickFix_DashInput_ProducesEmptyArtifacts).
	info := domain.WorkflowInfo{ID: "output-dash", Version: "1.0"}
	table, err := workflow.Parse([]byte(outputDashContent), info)
	if err != nil {
		t.Fatalf("Parse(output-dash): unexpected error: %v", err)
	}

	if len(table.Rows[0].OutputArtifacts) != 0 {
		t.Errorf("Rows[0].OutputArtifacts: want empty (Output is \"-\"), got %v", table.Rows[0].OutputArtifacts)
	}
}
