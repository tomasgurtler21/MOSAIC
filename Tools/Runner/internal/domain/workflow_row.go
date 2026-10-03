package domain

// WorkflowRow is the 1-based number of a routing-table row: the value of a
// deployed workflow table's Row column and of the Execution Log's
// WorkflowRow column. NoWorkflowRow means "no row" (infrastructure,
// out-of-band and ad-hoc steps, and entries from logs without the column).
type WorkflowRow int

// NoWorkflowRow is the zero value. It never collides with a valid row (>= 1).
const NoWorkflowRow WorkflowRow = 0

// WorkflowRowFromIndex converts a zero-based routing-table row index into its
// 1-based row number. It is the single index-to-number conversion point.
func WorkflowRowFromIndex(index int) WorkflowRow {
	return WorkflowRow(index + 1)
}

// Index returns the zero-based routing-table row index of a valid row.
func (r WorkflowRow) Index() int {
	return int(r) - 1
}
