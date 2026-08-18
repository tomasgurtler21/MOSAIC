// Package mdtable parses and renders pipe-delimited markdown tables.
//
// A markdown table consists of a header row, a separator row (pipes and dashes),
// and zero or more data rows. All rows are pipe-delimited. Cell values are trimmed
// of leading and trailing whitespace.
//
// mdtable depends only on the Go standard library.
package mdtable

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// Table is the in-memory representation of a fixed-column markdown table.
// It is a value type: Parse produces one, mutators return a new Table.
type Table struct {
	// Header contains the column names in declaration order.
	Header []string
	// Rows contains the data rows in declaration order.
	// Each row is a slice of cell values with the same length as Header.
	Rows [][]string
	// Widths contains the column widths (in characters) from the original
	// separator row. Preserved on round-trip so that re-rendered tables
	// maintain the original column alignment.
	Widths []int
}

// Parse reads the first markdown table found in the given byte slice.
// A table starts with a header row (pipe-delimited), followed by a separator
// row (pipes and dashes/colons), followed by zero or more data rows.
//
// Cell values are trimmed of leading/trailing whitespace.
// Column widths are derived from the separator row for round-trip fidelity.
//
// Returns an error if no table is found, or if the header row and separator
// row have a different number of columns.
func Parse(data []byte) (Table, error) {
	t, _, _, err := ParseAt(data, 0)
	return t, err
}

// ParseAt reads the first markdown table found at or after the given byte offset.
// Returns the table and the byte range [start, end) it occupies in data, so that
// callers can splice a re-rendered table back into the original byte slice.
//
// Returns an error if no table is found at or after offset.
func ParseAt(data []byte, offset int) (Table, int, int, error) {
	if offset < 0 || offset > len(data) {
		return Table{}, 0, 0, errors.New("mdtable: offset out of range")
	}

	lines, lineOffsets := splitLines(data[offset:])
	// Adjust offsets to be relative to data (not data[offset:]).
	for i := range lineOffsets {
		lineOffsets[i] += offset
	}

	// Scan for the first pair of (header, separator) rows.
	for i := 0; i < len(lines)-1; i++ {
		if !isPipeRow(lines[i]) {
			continue
		}
		if !isSeparatorRow(lines[i+1]) {
			continue
		}

		// Found header at lines[i], separator at lines[i+1].
		header := parseRow(lines[i])
		widths := separatorWidths(lines[i+1])

		if len(header) != len(widths) {
			return Table{}, 0, 0, fmt.Errorf(
				"mdtable: header has %d columns but separator has %d columns",
				len(header), len(widths))
		}

		tableStart := lineOffsets[i]

		// Collect data rows: all consecutive pipe rows after the separator.
		var rows [][]string
		j := i + 2
		for j < len(lines) && isPipeRow(lines[j]) {
			row := parseRow(lines[j])
			// Normalise row length to match header.
			for len(row) < len(header) {
				row = append(row, "")
			}
			if len(row) > len(header) {
				row = row[:len(header)]
			}
			rows = append(rows, row)
			j++
		}

		// End offset: beginning of line after last table row.
		tableEnd := lineOffsets[j] // if j == len(lines), this is offset+len(data[offset:]) = len(data)

		t := Table{
			Header: header,
			Rows:   rows,
			Widths: widths,
		}
		return t, tableStart, tableEnd, nil
	}

	return Table{}, 0, 0, errors.New("mdtable: no table found")
}

// Column returns the zero-based index of the named column, or -1 if absent.
func (t Table) Column(name string) int {
	for i, h := range t.Header {
		if h == name {
			return i
		}
	}
	return -1
}

// Cell returns the cell value at the given row and column index.
// Panics if row or col is out of range.
func (t Table) Cell(row, col int) string {
	return t.Rows[row][col]
}

// AppendRow returns a new Table with the given row appended.
// The values slice must have the same length as Header.
func (t Table) AppendRow(values []string) Table {
	if len(values) != len(t.Header) {
		panic(fmt.Sprintf("mdtable: AppendRow: values length %d != header length %d", len(values), len(t.Header)))
	}
	newRows := make([][]string, len(t.Rows)+1)
	copy(newRows, t.Rows)
	newRows[len(t.Rows)] = append([]string(nil), values...)
	return Table{Header: t.Header, Rows: newRows, Widths: t.Widths}
}

// UpsertRow returns a new Table where the row whose cell at keyCol equals
// keyVal is replaced with the given values. If no such row exists, the
// row is appended. The values slice must have the same length as Header.
func (t Table) UpsertRow(keyCol int, keyVal string, values []string) Table {
	if len(values) != len(t.Header) {
		panic(fmt.Sprintf("mdtable: UpsertRow: values length %d != header length %d", len(values), len(t.Header)))
	}
	newRows := make([][]string, len(t.Rows))
	for i, row := range t.Rows {
		newRows[i] = append([]string(nil), row...)
	}
	for i, row := range newRows {
		if row[keyCol] == keyVal {
			newRows[i] = append([]string(nil), values...)
			return Table{Header: t.Header, Rows: newRows, Widths: t.Widths}
		}
	}
	// Not found: append.
	newRows = append(newRows, append([]string(nil), values...))
	return Table{Header: t.Header, Rows: newRows, Widths: t.Widths}
}

// Render serialises the table to markdown bytes. Column widths are taken from
// the Widths field (preserving the original alignment). If a cell value exceeds
// the stored width, the column is widened to fit. The separator row uses dashes
// only (no alignment colons).
func (t Table) Render() []byte {
	// Compute effective widths: max of stored widths and actual content widths.
	widths := make([]int, len(t.Header))
	copy(widths, t.Widths)
	for i, h := range t.Header {
		if len(h) > widths[i] {
			widths[i] = len(h)
		}
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	var buf bytes.Buffer

	// Header row.
	buf.WriteByte('|')
	for i, h := range t.Header {
		buf.WriteByte(' ')
		buf.WriteString(h)
		buf.WriteString(strings.Repeat(" ", widths[i]-len(h)))
		buf.WriteString(" |")
	}
	buf.WriteByte('\n')

	// Separator row.
	buf.WriteByte('|')
	for _, w := range widths {
		buf.WriteByte(' ')
		buf.WriteString(strings.Repeat("-", w))
		buf.WriteString(" |")
	}
	buf.WriteByte('\n')

	// Data rows.
	for _, row := range t.Rows {
		buf.WriteByte('|')
		for i := range t.Header {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			buf.WriteByte(' ')
			buf.WriteString(cell)
			buf.WriteString(strings.Repeat(" ", widths[i]-len(cell)))
			buf.WriteString(" |")
		}
		buf.WriteByte('\n')
	}

	return buf.Bytes()
}

// --- internal helpers ---

// splitLines splits data into lines, preserving line endings.
// Returns the lines and the byte offset of each line's start within data.
// A sentinel entry is appended to lineOffsets for the end of data.
func splitLines(data []byte) ([][]byte, []int) {
	var lines [][]byte
	var offsets []int
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			lines = append(lines, data[start:i+1])
			offsets = append(offsets, start)
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
		offsets = append(offsets, start)
	}
	// Sentinel: offset of position after last byte.
	offsets = append(offsets, len(data))
	return lines, offsets
}

// isPipeRow returns true if the line (trimmed) starts and ends with '|'.
func isPipeRow(line []byte) bool {
	trimmed := bytes.TrimSpace(line)
	return len(trimmed) >= 2 && trimmed[0] == '|' && trimmed[len(trimmed)-1] == '|'
}

// isSeparatorRow returns true if the line looks like a markdown table separator:
// a pipe row where every cell contains only dashes, colons, and spaces.
func isSeparatorRow(line []byte) bool {
	if !isPipeRow(line) {
		return false
	}
	cells := parseRow(line)
	for _, cell := range cells {
		for _, ch := range cell {
			if ch != '-' && ch != ':' && ch != ' ' {
				return false
			}
		}
		// Must have at least one dash.
		if !strings.Contains(cell, "-") {
			return false
		}
	}
	return true
}

// parseRow splits a pipe-delimited row into trimmed cell values.
// Assumes the line starts and ends with '|'.
func parseRow(line []byte) []string {
	trimmed := strings.TrimSpace(string(line))
	// Strip leading and trailing '|'.
	if len(trimmed) > 0 && trimmed[0] == '|' {
		trimmed = trimmed[1:]
	}
	if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '|' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	parts := strings.Split(trimmed, "|")
	result := make([]string, len(parts))
	for i, p := range parts {
		result[i] = strings.TrimSpace(p)
	}
	return result
}

// separatorWidths returns the width of each column as determined by the separator
// row's dash sequences (not counting the surrounding spaces and pipes).
func separatorWidths(line []byte) []int {
	cells := parseRow(line)
	widths := make([]int, len(cells))
	for i, cell := range cells {
		// Width is the length of the cell content (dashes + colons).
		widths[i] = len(strings.TrimSpace(cell))
	}
	return widths
}
