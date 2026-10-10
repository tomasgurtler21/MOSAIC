package mdtable

import "fmt"

// StructureError reports a data row with more cells than the header.
type StructureError struct {
	Row     int // zero-based data-row index (0 = first row after the separator)
	Line    int // 1-based line number of the row within the parsed input
	Cells   int // cells found in the row (escape-aware split)
	Columns int // columns in the header
}

// Error describes the offending row, its line and the cell/column counts.
func (e *StructureError) Error() string {
	return fmt.Sprintf("mdtable: data row %d (line %d) has %d cells but the header has %d columns",
		e.Row, e.Line, e.Cells, e.Columns)
}

// ParseStrict is Parse with structural checking: a data row with more cells
// than the header yields (Table{}, *StructureError).
func ParseStrict(data []byte) (Table, error) {
	t, _, _, err := parseAt(data, 0, true)
	return t, err
}
