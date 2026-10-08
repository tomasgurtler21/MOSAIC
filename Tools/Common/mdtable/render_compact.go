package mdtable

import "bytes"

// RenderCompact serialises the table without column-width padding. Every line
// is "|" followed, for each header column, by " " + cell + " |", then "\n".
// The separator row writes "---" for every column. Widths is ignored.
// Missing cells are written empty; cells beyond the header are not written.
func (t Table) RenderCompact() []byte {
	var buf bytes.Buffer
	cols := len(t.Header)
	writeLine := func(cell func(i int) string) {
		buf.WriteByte('|')
		for i := 0; i < cols; i++ {
			buf.WriteByte(' ')
			buf.WriteString(cell(i))
			buf.WriteString(" |")
		}
		buf.WriteByte('\n')
	}
	writeLine(func(i int) string { return t.Header[i] })
	writeLine(func(int) string { return "---" })
	for _, row := range t.Rows {
		writeLine(func(i int) string {
			if i < len(row) {
				return row[i]
			}
			return ""
		})
	}
	return buf.Bytes()
}
