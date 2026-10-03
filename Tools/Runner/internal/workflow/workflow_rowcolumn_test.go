package workflow_test

// Tests for the optional Row column of workflow.Parse.
//
// A deployed workflow table may carry a Row column holding each data row's
// 1-based position. Authored tables do not have it. The column is validated,
// never stored: RoutingRow.Index stays the row identity.

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/workflow"
)

const rowColumnWorkflowID = "row-column-wf"

// rowColumnData is the fixed table content (Phase, Subagent, HITL, Input, Output)
// shared by every Row-column test; only the Row column varies.
var rowColumnData = [][]string{
	{"PLANNING", "planner", "FALSE", "-", "Plan.md"},
	{"PLANNING", "plan-review", "FALSE", "Plan.md", "plan-review.md"},
	{"REVIEW", "final-review", "FALSE", "Plan.md", "final.md"},
	{"REVIEW", "summariser", "FALSE", "final.md", "summary.md"},
}

var rowColumnHeader = []string{"Phase", "Subagent", "HITL", "Input", "Output"}

// buildRowColumnTable renders the shared table. When pos is negative no Row
// column is emitted; otherwise a Row column is inserted at index pos and data
// row i holds values[i].
func buildRowColumnTable(pos int, values []string) string {
	insert := func(cells []string, v string) []string {
		if pos < 0 {
			return cells
		}
		out := append([]string{}, cells[:pos]...)
		out = append(out, v)
		return append(out, cells[pos:]...)
	}
	line := func(cells []string) string { return "| " + strings.Join(cells, " | ") + " |\n" }

	sep := make([]string, len(rowColumnHeader))
	for i := range sep {
		sep[i] = "---"
	}

	var b strings.Builder
	b.WriteString(line(insert(rowColumnHeader, "Row")))
	b.WriteString(line(insert(sep, "---")))
	for i, data := range rowColumnData {
		v := ""
		if pos >= 0 {
			v = values[i]
		}
		b.WriteString(line(insert(data, v)))
	}
	return b.String()
}

func correctRowValues() []string {
	vals := make([]string, len(rowColumnData))
	for i := range vals {
		vals[i] = fmt.Sprint(i + 1)
	}
	return vals
}

func parseRowColumnTable(content string) (domain.RoutingTable, error) {
	return workflow.Parse([]byte(content), domain.WorkflowInfo{ID: rowColumnWorkflowID, Version: "1.0"})
}

func TestParse_RowColumnAbsent_ParsesAllRowsInOrder(t *testing.T) {
	table, err := parseRowColumnTable(buildRowColumnTable(-1, nil))
	if err != nil {
		t.Fatalf("Parse without Row column: %v", err)
	}

	if len(table.Rows) != len(rowColumnData) {
		t.Fatalf("want %d rows, got %d", len(rowColumnData), len(table.Rows))
	}
	for i, row := range table.Rows {
		if row.Index != i || row.Agent != rowColumnData[i][1] {
			t.Errorf("Rows[%d]: want Index %d agent %q, got Index %d agent %q",
				i, i, rowColumnData[i][1], row.Index, row.Agent)
		}
	}
}

func TestParse_RowColumnCorrect_AnyPosition_SameRoutingModelAsWithout(t *testing.T) {
	without, err := parseRowColumnTable(buildRowColumnTable(-1, nil))
	if err != nil {
		t.Fatalf("Parse without Row column: %v", err)
	}

	positions := map[string]int{"first": 0, "middle": 2, "last": len(rowColumnHeader)}
	for name, pos := range positions {
		t.Run(name, func(t *testing.T) {
			with, err := parseRowColumnTable(buildRowColumnTable(pos, correctRowValues()))
			if err != nil {
				t.Fatalf("Parse with correct Row column at %d: %v", pos, err)
			}
			if !reflect.DeepEqual(with.Rows, without.Rows) {
				t.Errorf("routing rows differ with a Row column at %d:\nwith:    %+v\nwithout: %+v",
					pos, with.Rows, without.Rows)
			}
		})
	}
}

func TestParse_RowColumnWrongValue_ReturnsRefusalNamingWorkflowAndRow(t *testing.T) {
	cases := []struct {
		name   string
		values []string
	}{
		{"non-numeric", []string{"1", "2", "abc", "4"}},
		{"gap", []string{"1", "2", "4", "5"}},
		{"duplicate", []string{"1", "2", "3", "3"}},
		{"out-of-order", []string{"1", "3", "2", "4"}},
		{"wrong-start-number", []string{"2", "3", "4", "5"}},
		{"zero-based", []string{"0", "1", "2", "3"}},
		{"missing-value", []string{"1", "2", "", "4"}},
		{"leading-zero", []string{"1", "2", "03", "4"}},
		{"explicit-plus-sign", []string{"1", "2", "+3", "4"}},
		{"decimal-fraction", []string{"1.0", "2", "3", "4"}},
	}

	for _, tc := range cases {
		for _, pos := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/position-%d", tc.name, pos), func(t *testing.T) {
				_, err := parseRowColumnTable(buildRowColumnTable(pos, tc.values))

				if err == nil {
					t.Fatal("Parse must refuse a Row column with an incorrect value")
				}
				re := asRefusalError(t, err)
				if !strings.Contains(re.Resource+" "+re.Reason+" "+err.Error(), rowColumnWorkflowID) {
					t.Errorf("refusal must name workflow %q; got Resource=%q Reason=%q",
						rowColumnWorkflowID, re.Resource, re.Reason)
				}
			})
		}
	}
}

func TestParse_RowColumnWrongValue_RefusalNamesOffendingRow(t *testing.T) {
	// Only data row 3 is wrong; the refusal must point at it by its 1-based number.
	_, err := parseRowColumnTable(buildRowColumnTable(0, []string{"1", "2", "abc", "4"}))

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "3") {
		t.Errorf("refusal Reason must name the offending row 3; got %q", re.Reason)
	}
}
