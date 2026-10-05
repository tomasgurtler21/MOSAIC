package workflow_test

import (
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/workflow"
)

func TestParse_ApproachTable_HeadingPresentButNoTable_ReturnsError(t *testing.T) {
	// The **Execution Groups:** heading is present but no table follows it.
	// Parse must return an error rather than silently treating it as no table.
	info := domain.WorkflowInfo{ID: "heading-no-table", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableHeadingPresentNoTableContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the **Execution Groups:** heading is present but no table follows")
	}
}

func TestParse_ApproachTable_HeadingPresentButNoTable_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "heading-no-table", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableHeadingPresentNoTableContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "heading") {
		t.Errorf("RefusalError.Reason should mention the heading (P3), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_MissingApproachColumn_ReturnsError(t *testing.T) {
	// An approach table that is missing the "Approach" column must be refused.
	info := domain.WorkflowInfo{ID: "missing-approach-col", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableMissingApproachColumnContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the Execution Groups table is missing the Approach column")
	}
}

func TestParse_ApproachTable_MissingApproachColumn_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "missing-approach-col", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableMissingApproachColumnContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "Approach") {
		t.Errorf("RefusalError.Reason should name the missing Approach column (P4), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_MissingGroupsColumn_ReturnsError(t *testing.T) {
	// An approach table that is missing the "Groups" column must be refused.
	info := domain.WorkflowInfo{ID: "missing-groups-col", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableMissingGroupsColumnContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the Execution Groups table is missing the Groups column")
	}
}

func TestParse_ApproachTable_MissingGroupsColumn_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "missing-groups-col", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableMissingGroupsColumnContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "Groups") {
		t.Errorf("RefusalError.Reason should name the missing Groups column (P5), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_ZeroDataRows_ReturnsError(t *testing.T) {
	// An approach table with a valid header and separator but no data rows
	// must be refused — a zero-row table has no declared approaches.
	info := domain.WorkflowInfo{ID: "zero-approach-rows", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableZeroDataRowsContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the Execution Groups table has a header but no data rows")
	}
}

func TestParse_ApproachTable_ZeroDataRows_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "zero-approach-rows", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableZeroDataRowsContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "data rows") {
		t.Errorf("RefusalError.Reason should mention data rows (P6), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_EmptyApproachCell_ReturnsError(t *testing.T) {
	// A data row with an empty Approach cell must be refused.
	info := domain.WorkflowInfo{ID: "empty-approach-cell", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableEmptyApproachCellContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when an Approach cell is empty")
	}
}

func TestParse_ApproachTable_EmptyApproachCell_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "empty-approach-cell", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableEmptyApproachCellContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "empty") {
		t.Errorf("RefusalError.Reason should describe the empty Approach cell (P7), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_EmptyGroupsCell_ReturnsError(t *testing.T) {
	// A data row with an empty Groups cell must be refused.
	info := domain.WorkflowInfo{ID: "empty-groups-cell", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableEmptyGroupsCellContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when a Groups cell is empty")
	}
}

func TestParse_ApproachTable_EmptyGroupsCell_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "empty-groups-cell", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableEmptyGroupsCellContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "empty") {
		t.Errorf("RefusalError.Reason should describe the empty Groups cell (P7), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_EmptyTokenInGroupsCell_ReturnsError(t *testing.T) {
	// A Groups cell of "Test, , Implementation" has an empty token between the
	// commas; this must be refused.
	info := domain.WorkflowInfo{ID: "empty-token-in-groups", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableEmptyTokenInGroupsCellContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when a Groups cell contains an empty token")
	}
}

func TestParse_ApproachTable_EmptyTokenInGroupsCell_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "empty-token-in-groups", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableEmptyTokenInGroupsCellContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "empty") {
		t.Errorf("RefusalError.Reason should describe the empty group token (P7), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_DuplicateApproach_ReturnsError(t *testing.T) {
	// Two rows with the same Approach token must be refused.
	info := domain.WorkflowInfo{ID: "duplicate-approach", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableDuplicateApproachContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the same Approach token appears in two rows")
	}
}

func TestParse_ApproachTable_DuplicateApproach_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "duplicate-approach", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableDuplicateApproachContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "twice") {
		t.Errorf("RefusalError.Reason should mention the duplicate declared twice (P8), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_DuplicateGroupWithinRow_ReturnsError(t *testing.T) {
	// A group token listed twice in the same Groups cell must be refused.
	info := domain.WorkflowInfo{ID: "duplicate-group-in-row", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableDuplicateGroupWithinRowContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when a group token appears twice in one Groups cell")
	}
}

func TestParse_ApproachTable_DuplicateGroupWithinRow_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "duplicate-group-in-row", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableDuplicateGroupWithinRowContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "twice") {
		t.Errorf("RefusalError.Reason should mention the duplicate declared twice (P8), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_Refusal_ComponentIsWorkflow(t *testing.T) {
	// Every approach table refusal must name "workflow" as the component.
	info := domain.WorkflowInfo{ID: "missing-approach-col", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableMissingApproachColumnContent), info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
}

func TestParse_ApproachTable_Refusal_ResourceNamesWorkflow(t *testing.T) {
	// Every approach table refusal must carry a non-empty Resource identifying
	// the workflow so the user can find the problematic file.
	info := domain.WorkflowInfo{ID: "my-workflow-id", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableMissingApproachColumnContent), info)

	re := asRefusalError(t, err)
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}
