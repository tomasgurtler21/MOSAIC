package workflow

import (
	"fmt"
	"strconv"
	"strings"

	"mosaic-common/mdtable"
	"mosaic-run/internal/domain"
)

// rowColumnName is the header of the optional column that holds each data
// row's 1-based position in the table. Deployment injects it; authored tables
// do not carry it.
const rowColumnName = "Row"

// RowNumber returns the 1-based number of a routing row: its position in the
// table, one more than its Index.
func RowNumber(row domain.RoutingRow) int {
	return row.Index + 1
}

// validateRowColumn checks the optional Row column. When the column is absent
// it returns nil. When present, every data row must hold its own 1-based
// position as a plain decimal number; otherwise it returns a
// *domain.RefusalError naming the workflow and the offending row.
func validateRowColumn(t mdtable.Table, workflowID string) error {
	col := t.Column(rowColumnName)
	if col < 0 {
		return nil
	}
	for i, rawRow := range t.Rows {
		want := strconv.Itoa(i + 1)
		got := strings.TrimSpace(rawRow[col])
		if got != want {
			return &domain.RefusalError{
				Component: "workflow",
				Resource:  workflowID,
				Reason: fmt.Sprintf("%q column of row %d must hold %s, found %q",
					rowColumnName, i+1, want, got),
			}
		}
	}
	return nil
}
