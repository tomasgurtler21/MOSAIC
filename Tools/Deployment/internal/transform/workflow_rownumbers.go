package transform

import (
	"bytes"
	"strconv"

	"mosaic-common/mdtable"
)

const rowColumnName = "Row"

// injectRowColumn returns block with a leading Row column added to its routing table, the
// first markdown table mdtable finds. Data row k (1-based) gets the cell k. An existing Row
// column is dropped first so the result is the same however many times it is applied. Data
// rows are normalised to the header width, as mdtable does when the Runner reads them.
// Every byte outside the table's lines is unchanged, and each table line keeps its own line
// ending. A block without a table, or with one mdtable refuses, is returned as is.
func injectRowColumn(block []byte) []byte {
	table, start, end, err := mdtable.ParseAt(block, 0)
	if err != nil {
		return block
	}
	drop := table.Column(rowColumnName)
	width := len(table.Header)

	out := make([]byte, 0, len(block)+len(table.Rows)*6+16)
	out = append(out, block[:start]...)
	for i, line := range splitTableLines(block[start:end]) {
		body, eol := splitLineEnding(line)
		cells := rawCells(body)
		var lead string
		switch i {
		case 0:
			lead = "| " + rowColumnName + " |"
		case 1:
			lead = "|-----|"
		default:
			lead = "| " + strconv.Itoa(i-1) + " |"
			cells = fitWidth(cells, width)
		}
		out = append(out, lead...)
		for c, cell := range cells {
			if c == drop {
				continue
			}
			out = append(out, cell...)
			out = append(out, '|')
		}
		out = append(out, eol...)
	}
	out = append(out, block[end:]...)
	return out
}

// splitTableLines splits data into lines, each keeping its line terminator.
func splitTableLines(data []byte) [][]byte {
	var lines [][]byte
	for len(data) > 0 {
		n := bytes.IndexByte(data, '\n') + 1
		if n == 0 {
			n = len(data)
		}
		lines = append(lines, data[:n])
		data = data[n:]
	}
	return lines
}

// splitLineEnding separates a line's content from its terminator (LF, CRLF or none).
func splitLineEnding(line []byte) (body, eol []byte) {
	body = bytes.TrimRight(line, "\r\n")
	return body, line[len(body):]
}

// rawCells returns the untrimmed text between the pipes of a pipe row.
func rawCells(body []byte) [][]byte {
	inner := bytes.TrimSpace(body)
	inner = inner[1 : len(inner)-1]
	return bytes.Split(inner, []byte("|"))
}

// fitWidth pads cells with empty cells or cuts them to exactly width cells.
func fitWidth(cells [][]byte, width int) [][]byte {
	for len(cells) < width {
		cells = append(cells, []byte(" "))
	}
	return cells[:width]
}
