package mdtable_test

// Tests for the mdtable package: parsing, cell access, mutation, rendering, and byte-range splicing.
//
// Coverage:
//   - Parse reads the first pipe-delimited markdown table from a byte slice.
//   - Parse extracts the correct column names from the header row.
//   - Parse extracts cell values trimmed of leading/trailing whitespace.
//   - Parse returns the correct number of data rows.
//   - Parse derives column widths from the separator row's dash sequences.
//   - Parse on a byte slice with no table returns an error.
//   - Parse when header and separator column counts differ returns an error.
//   - Parse handles a table with zero data rows (header + separator only).
//   - Column returns the zero-based index of a named column.
//   - Column returns -1 for a name that does not appear in the header.
//   - Cell returns the value at the given row and column index.
//   - AppendRow returns a new Table with one additional row at the end.
//   - AppendRow leaves the original Table unmodified (value semantics).
//   - UpsertRow replaces the row whose key column matches the given value.
//   - UpsertRow appends a new row when no existing row has the given key value.
//   - UpsertRow leaves the original Table unmodified (value semantics).
//   - Render serialises the table to markdown bytes with pipe-delimited rows.
//   - Render preserves column widths from the Widths field.
//   - Render widens a column when a cell value exceeds the stored width.
//   - Render produces output that re-parses to an equivalent Table (round-trip stability).
//   - ParseAt finds the first table at or after the given byte offset.
//   - ParseAt returns the byte range [start, end) occupied by the table in the input.
//   - ParseAt byte range allows correct splicing of a re-rendered table into the original document.
//   - ParseAt returns an error when no table is found at or after the given offset.
//   - ParseAt returns an error when the offset is out of range.
//   - AppendRow panics when the values slice length differs from the header length.
//   - UpsertRow panics when the values slice length differs from the header length.
//   - Cell panics when the row or column index is out of range.
//   - Render on a Table with nil Widths derives widths from content and produces valid output.

import (
	"bytes"
	"testing"

	"mosaic-common/mdtable"
)

// --- helpers ---

// mustParse parses a table from bytes and fails the test if parsing fails.
func mustParse(t *testing.T, data []byte) mdtable.Table {
	t.Helper()
	tbl, err := mdtable.Parse(data)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	return tbl
}

// simpleTable is a minimal two-column table used across multiple tests.
var simpleTableBytes = []byte(
	"| Name   | Value  |\n" +
		"| ------ | ------ |\n" +
		"| alpha  | one    |\n" +
		"| beta   | two    |\n",
)

// threeColumnTableBytes is a three-column table used in width and render tests.
var threeColumnTableBytes = []byte(
	"| Phase    | Agent      | HITL |\n" +
		"| -------- | ---------- | ---- |\n" +
		"| RESEARCH | researcher | yes  |\n" +
		"| PLANNING | planner    | no   |\n",
)

// --- Parse: happy path ---

func TestParse_TwoColumnTable_HeaderContainsBothColumns(t *testing.T) {
	tbl := mustParse(t, simpleTableBytes)

	if len(tbl.Header) != 2 {
		t.Fatalf("Header length: want 2, got %d", len(tbl.Header))
	}
	if tbl.Header[0] != "Name" {
		t.Errorf("Header[0]: want %q, got %q", "Name", tbl.Header[0])
	}
	if tbl.Header[1] != "Value" {
		t.Errorf("Header[1]: want %q, got %q", "Value", tbl.Header[1])
	}
}

func TestParse_TwoColumnTable_RowCountMatchesDataRows(t *testing.T) {
	tbl := mustParse(t, simpleTableBytes)

	if len(tbl.Rows) != 2 {
		t.Errorf("Rows count: want 2, got %d", len(tbl.Rows))
	}
}

func TestParse_CellValues_TrimmedOfWhitespace(t *testing.T) {
	// Input cell values have surrounding whitespace (from pipe-delimited formatting).
	// Parse must trim all cell values.
	tbl := mustParse(t, simpleTableBytes)

	if tbl.Rows[0][0] != "alpha" {
		t.Errorf("Rows[0][0]: want %q, got %q", "alpha", tbl.Rows[0][0])
	}
	if tbl.Rows[0][1] != "one" {
		t.Errorf("Rows[0][1]: want %q, got %q", "one", tbl.Rows[0][1])
	}
	if tbl.Rows[1][0] != "beta" {
		t.Errorf("Rows[1][0]: want %q, got %q", "beta", tbl.Rows[1][0])
	}
}

func TestParse_SeparatorRow_DeterminesColumnWidths(t *testing.T) {
	// The separator row "| ------ | ------ |" has dashes of length 6 in each cell.
	tbl := mustParse(t, simpleTableBytes)

	if len(tbl.Widths) != 2 {
		t.Fatalf("Widths length: want 2, got %d", len(tbl.Widths))
	}
	if tbl.Widths[0] != 6 {
		t.Errorf("Widths[0]: want 6 (from six dashes in separator), got %d", tbl.Widths[0])
	}
	if tbl.Widths[1] != 6 {
		t.Errorf("Widths[1]: want 6 (from six dashes in separator), got %d", tbl.Widths[1])
	}
}

func TestParse_ThreeColumnTable_HeaderAndWidthsMatch(t *testing.T) {
	tbl := mustParse(t, threeColumnTableBytes)

	if len(tbl.Header) != 3 {
		t.Fatalf("Header length: want 3, got %d", len(tbl.Header))
	}
	if tbl.Header[0] != "Phase" {
		t.Errorf("Header[0]: want %q, got %q", "Phase", tbl.Header[0])
	}
	if tbl.Header[1] != "Agent" {
		t.Errorf("Header[1]: want %q, got %q", "Agent", tbl.Header[1])
	}
	if tbl.Header[2] != "HITL" {
		t.Errorf("Header[2]: want %q, got %q", "HITL", tbl.Header[2])
	}
	// Separator: "| -------- | ---------- | ---- |" → widths [8, 10, 4].
	if tbl.Widths[0] != 8 {
		t.Errorf("Widths[0]: want 8, got %d", tbl.Widths[0])
	}
	if tbl.Widths[1] != 10 {
		t.Errorf("Widths[1]: want 10, got %d", tbl.Widths[1])
	}
	if tbl.Widths[2] != 4 {
		t.Errorf("Widths[2]: want 4, got %d", tbl.Widths[2])
	}
}

func TestParse_HeaderAndSeparatorOnly_ReturnsEmptyRows(t *testing.T) {
	// A table with only a header and separator row (no data rows) is valid.
	input := []byte("| Col |\n| --- |\n")

	tbl, err := mdtable.Parse(input)

	if err != nil {
		t.Fatalf("Parse on header+separator-only table: unexpected error: %v", err)
	}
	if len(tbl.Header) != 1 {
		t.Errorf("Header length: want 1, got %d", len(tbl.Header))
	}
	if len(tbl.Rows) != 0 {
		t.Errorf("Rows count: want 0, got %d", len(tbl.Rows))
	}
}

func TestParse_TablePrecededByText_ParsesSuccessfully(t *testing.T) {
	// Parse must skip non-table lines and find the first table in the input.
	input := []byte("Some introductory text.\n\n| Col |\n| --- |\n| val |\n")

	tbl, err := mdtable.Parse(input)

	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if len(tbl.Rows) != 1 {
		t.Errorf("Rows count: want 1, got %d", len(tbl.Rows))
	}
	if tbl.Rows[0][0] != "val" {
		t.Errorf("Rows[0][0]: want %q, got %q", "val", tbl.Rows[0][0])
	}
}

// --- Parse: error cases ---

func TestParse_NoTable_ReturnsError(t *testing.T) {
	input := []byte("No table here.\nJust plain text.\n")

	_, err := mdtable.Parse(input)

	if err == nil {
		t.Error("Parse on input with no table: expected error, got nil")
	}
}

func TestParse_EmptyInput_ReturnsError(t *testing.T) {
	_, err := mdtable.Parse([]byte{})

	if err == nil {
		t.Error("Parse on empty input: expected error, got nil")
	}
}

func TestParse_HeaderAndSeparatorColumnCountMismatch_ReturnsError(t *testing.T) {
	// A header with 3 columns and a separator with 2 columns is malformed.
	input := []byte("| A | B | C |\n| -- | -- |\n| x | y | z |\n")

	_, err := mdtable.Parse(input)

	if err == nil {
		t.Error("Parse on mismatched header/separator column count: expected error, got nil")
	}
}

// --- Column ---

func TestColumn_ExistingColumnName_ReturnsZeroBasedIndex(t *testing.T) {
	tbl := mustParse(t, threeColumnTableBytes)

	if col := tbl.Column("Phase"); col != 0 {
		t.Errorf("Column(\"Phase\"): want 0, got %d", col)
	}
	if col := tbl.Column("Agent"); col != 1 {
		t.Errorf("Column(\"Agent\"): want 1, got %d", col)
	}
	if col := tbl.Column("HITL"); col != 2 {
		t.Errorf("Column(\"HITL\"): want 2, got %d", col)
	}
}

func TestColumn_AbsentName_ReturnsNegativeOne(t *testing.T) {
	tbl := mustParse(t, threeColumnTableBytes)

	col := tbl.Column("Status")

	if col != -1 {
		t.Errorf("Column(\"Status\"): want -1 for absent column, got %d", col)
	}
}

// --- Cell ---

func TestCell_ReturnsValueAtGivenRowAndColumnIndex(t *testing.T) {
	tbl := mustParse(t, threeColumnTableBytes)

	// Row 0: "RESEARCH", "researcher", "yes"
	if v := tbl.Cell(0, 0); v != "RESEARCH" {
		t.Errorf("Cell(0,0): want %q, got %q", "RESEARCH", v)
	}
	if v := tbl.Cell(0, 1); v != "researcher" {
		t.Errorf("Cell(0,1): want %q, got %q", "researcher", v)
	}
	if v := tbl.Cell(0, 2); v != "yes" {
		t.Errorf("Cell(0,2): want %q, got %q", "yes", v)
	}

	// Row 1: "PLANNING", "planner", "no"
	if v := tbl.Cell(1, 0); v != "PLANNING" {
		t.Errorf("Cell(1,0): want %q, got %q", "PLANNING", v)
	}
	if v := tbl.Cell(1, 2); v != "no" {
		t.Errorf("Cell(1,2): want %q, got %q", "no", v)
	}
}

// --- AppendRow ---

func TestAppendRow_IncreasesRowCountByOne(t *testing.T) {
	tbl := mustParse(t, simpleTableBytes)
	original := len(tbl.Rows)

	tbl2 := tbl.AppendRow([]string{"gamma", "three"})

	if len(tbl2.Rows) != original+1 {
		t.Errorf("after AppendRow: want %d rows, got %d", original+1, len(tbl2.Rows))
	}
}

func TestAppendRow_NewRowAppearsAtEnd(t *testing.T) {
	tbl := mustParse(t, simpleTableBytes)

	tbl2 := tbl.AppendRow([]string{"gamma", "three"})

	last := tbl2.Rows[len(tbl2.Rows)-1]
	if last[0] != "gamma" {
		t.Errorf("last row[0]: want %q, got %q", "gamma", last[0])
	}
	if last[1] != "three" {
		t.Errorf("last row[1]: want %q, got %q", "three", last[1])
	}
}

func TestAppendRow_OriginalTableRowCountUnchanged(t *testing.T) {
	// AppendRow must not modify the receiver (value semantics).
	tbl := mustParse(t, simpleTableBytes)
	originalCount := len(tbl.Rows)

	_ = tbl.AppendRow([]string{"gamma", "three"})

	if len(tbl.Rows) != originalCount {
		t.Errorf("original table modified by AppendRow: was %d rows, now %d rows", originalCount, len(tbl.Rows))
	}
}

func TestAppendRow_ExistingRowsUnchanged(t *testing.T) {
	// AppendRow must preserve the existing rows exactly.
	tbl := mustParse(t, simpleTableBytes)

	tbl2 := tbl.AppendRow([]string{"gamma", "three"})

	if tbl2.Rows[0][0] != "alpha" {
		t.Errorf("Rows[0][0] after append: want %q, got %q", "alpha", tbl2.Rows[0][0])
	}
	if tbl2.Rows[1][0] != "beta" {
		t.Errorf("Rows[1][0] after append: want %q, got %q", "beta", tbl2.Rows[1][0])
	}
}

// --- UpsertRow ---

func TestUpsertRow_ExistingKey_ReplacesMatchingRow(t *testing.T) {
	tbl := mustParse(t, simpleTableBytes)
	keyCol := tbl.Column("Name")

	tbl2 := tbl.UpsertRow(keyCol, "alpha", []string{"alpha", "updated"})

	// Row count must not change when replacing an existing row.
	if len(tbl2.Rows) != len(tbl.Rows) {
		t.Errorf("UpsertRow with existing key: row count changed from %d to %d", len(tbl.Rows), len(tbl2.Rows))
	}
	// The replaced row must carry the new values.
	if tbl2.Rows[0][1] != "updated" {
		t.Errorf("replaced row[1]: want %q, got %q", "updated", tbl2.Rows[0][1])
	}
}

func TestUpsertRow_AbsentKey_AppendsRow(t *testing.T) {
	tbl := mustParse(t, simpleTableBytes)
	keyCol := tbl.Column("Name")
	originalCount := len(tbl.Rows)

	tbl2 := tbl.UpsertRow(keyCol, "delta", []string{"delta", "four"})

	if len(tbl2.Rows) != originalCount+1 {
		t.Errorf("UpsertRow with absent key: want %d rows, got %d", originalCount+1, len(tbl2.Rows))
	}
	last := tbl2.Rows[len(tbl2.Rows)-1]
	if last[0] != "delta" {
		t.Errorf("appended row[0]: want %q, got %q", "delta", last[0])
	}
	if last[1] != "four" {
		t.Errorf("appended row[1]: want %q, got %q", "four", last[1])
	}
}

func TestUpsertRow_OriginalTableUnchanged(t *testing.T) {
	// UpsertRow must not modify the receiver (value semantics).
	tbl := mustParse(t, simpleTableBytes)
	keyCol := tbl.Column("Name")
	originalValue := tbl.Rows[0][1]

	_ = tbl.UpsertRow(keyCol, "alpha", []string{"alpha", "should-not-persist"})

	if tbl.Rows[0][1] != originalValue {
		t.Errorf("original row[1] modified by UpsertRow: was %q, now %q", originalValue, tbl.Rows[0][1])
	}
}

func TestUpsertRow_OnlyFirstMatchReplaced(t *testing.T) {
	// When a key appears more than once, UpsertRow replaces the first matching row only.
	input := []byte(
		"| Key | Value |\n" +
			"| --- | ----- |\n" +
			"| dup | first  |\n" +
			"| dup | second |\n",
	)
	tbl := mustParse(t, input)
	keyCol := tbl.Column("Key")

	tbl2 := tbl.UpsertRow(keyCol, "dup", []string{"dup", "replaced"})

	// First matching row is replaced; second duplicate is preserved.
	if tbl2.Rows[0][1] != "replaced" {
		t.Errorf("first dup row[1]: want %q, got %q", "replaced", tbl2.Rows[0][1])
	}
	if tbl2.Rows[1][1] != "second" {
		t.Errorf("second dup row[1] must be preserved; want %q, got %q", "second", tbl2.Rows[1][1])
	}
	if len(tbl2.Rows) != 2 {
		t.Errorf("row count: want 2, got %d", len(tbl2.Rows))
	}
}

// --- Render ---

func TestRender_ProducesValidPipeDelimitedOutput(t *testing.T) {
	tbl := mustParse(t, simpleTableBytes)

	rendered := tbl.Render()

	if len(rendered) == 0 {
		t.Fatal("Render: returned empty bytes")
	}
	if rendered[0] != '|' {
		t.Error("Render: output must start with '|'")
	}
}

func TestRender_RenderedOutputReparsesToEquivalentTable(t *testing.T) {
	// A table parsed from Render output must have the same header and rows as the original.
	tbl := mustParse(t, threeColumnTableBytes)

	rendered := tbl.Render()

	reparsed, err := mdtable.Parse(rendered)
	if err != nil {
		t.Fatalf("Parse of rendered table: %v", err)
	}
	if len(reparsed.Header) != len(tbl.Header) {
		t.Errorf("reparsed Header length: want %d, got %d", len(tbl.Header), len(reparsed.Header))
	}
	for i := range tbl.Header {
		if reparsed.Header[i] != tbl.Header[i] {
			t.Errorf("Header[%d]: want %q, got %q", i, tbl.Header[i], reparsed.Header[i])
		}
	}
	if len(reparsed.Rows) != len(tbl.Rows) {
		t.Errorf("reparsed Rows count: want %d, got %d", len(tbl.Rows), len(reparsed.Rows))
	}
	for i, row := range tbl.Rows {
		for j, cell := range row {
			if reparsed.Rows[i][j] != cell {
				t.Errorf("reparsed Rows[%d][%d]: want %q, got %q", i, j, cell, reparsed.Rows[i][j])
			}
		}
	}
}

func TestRender_StableRoundTrip_RenderThenParseAndRenderAgainIsIdentical(t *testing.T) {
	// When a table is built with explicit widths and rendered, then parsed and rendered again,
	// the two renders must be byte-identical (Render is stable).
	original := mdtable.Table{
		Header: []string{"Phase", "Agent", "HITL"},
		Rows: [][]string{
			{"RESEARCH", "researcher", "yes"},
			{"PLANNING", "planner", "no"},
		},
		Widths: []int{8, 10, 4},
	}

	first := original.Render()

	reparsed, err := mdtable.Parse(first)
	if err != nil {
		t.Fatalf("Parse of first render: %v", err)
	}

	second := reparsed.Render()

	if !bytes.Equal(first, second) {
		t.Errorf("Render is not stable:\nfirst render:\n%s\nsecond render:\n%s", first, second)
	}
}

func TestRender_PreservesStoredColumnWidths(t *testing.T) {
	// When all cell values are shorter than the stored widths, Render pads them to the
	// stored width rather than collapsing the column to the minimum content width.
	tbl := mdtable.Table{
		Header: []string{"Key", "Value"},
		Rows: [][]string{
			{"a", "b"},
		},
		Widths: []int{10, 10},
	}

	rendered := tbl.Render()

	// Separator row must use the stored width (10 dashes per column).
	if !bytes.Contains(rendered, []byte("----------")) {
		t.Error("Render: separator must contain 10 dashes to reflect stored column width of 10")
	}
}

func TestRender_WidensColumnWhenCellValueExceedsStoredWidth(t *testing.T) {
	// When a cell value is longer than the stored width, the column is widened to fit.
	tbl := mdtable.Table{
		Header: []string{"Key"},
		Rows: [][]string{
			{"this-is-a-long-value"},
		},
		Widths: []int{3},
	}

	rendered := tbl.Render()

	// The long value must appear in full in the output.
	if !bytes.Contains(rendered, []byte("this-is-a-long-value")) {
		t.Error("Render: long cell value must appear in full in the output, not truncated")
	}
	// The rendered output must re-parse successfully.
	_, err := mdtable.Parse(rendered)
	if err != nil {
		t.Errorf("Parse of widened render: %v", err)
	}
}

func TestRender_AllDataRowsIncludedInOutput(t *testing.T) {
	tbl := mustParse(t, threeColumnTableBytes)

	rendered := tbl.Render()

	if !bytes.Contains(rendered, []byte("RESEARCH")) {
		t.Error("Render: first data row value \"RESEARCH\" missing from output")
	}
	if !bytes.Contains(rendered, []byte("PLANNING")) {
		t.Error("Render: second data row value \"PLANNING\" missing from output")
	}
}

func TestRender_AppendedRow_AppearsInOutput(t *testing.T) {
	tbl := mustParse(t, simpleTableBytes)
	tbl2 := tbl.AppendRow([]string{"gamma", "three"})

	rendered := tbl2.Render()

	if !bytes.Contains(rendered, []byte("gamma")) {
		t.Error("Render: appended row value \"gamma\" must appear in rendered output")
	}
}

func TestRender_UpsertedRow_ReflectedInOutput(t *testing.T) {
	tbl := mustParse(t, simpleTableBytes)
	keyCol := tbl.Column("Name")
	tbl2 := tbl.UpsertRow(keyCol, "alpha", []string{"alpha", "updated"})

	rendered := tbl2.Render()

	if !bytes.Contains(rendered, []byte("updated")) {
		t.Error("Render: upserted row value \"updated\" must appear in rendered output")
	}
}

// --- ParseAt ---

func TestParseAt_ZeroOffset_FindsTableAsIfCallingParse(t *testing.T) {
	tbl, start, end, err := mdtable.ParseAt(simpleTableBytes, 0)

	if err != nil {
		t.Fatalf("ParseAt with offset 0: unexpected error: %v", err)
	}
	if start != 0 {
		t.Errorf("start: want 0, got %d", start)
	}
	if end != len(simpleTableBytes) {
		t.Errorf("end: want %d (entire input), got %d", len(simpleTableBytes), end)
	}
	if len(tbl.Header) != 2 {
		t.Errorf("Header length: want 2, got %d", len(tbl.Header))
	}
}

func TestParseAt_ZeroOffset_SkipsPreambleAndFindsTable(t *testing.T) {
	preamble := []byte("This is preamble text.\n\n")
	doc := append(preamble, simpleTableBytes...)

	tbl, start, end, err := mdtable.ParseAt(doc, 0)

	if err != nil {
		t.Fatalf("ParseAt: %v", err)
	}
	// The table must start after the preamble.
	if start != len(preamble) {
		t.Errorf("start: want %d (after preamble), got %d", len(preamble), start)
	}
	// The table must end at the end of the document.
	if end != len(doc) {
		t.Errorf("end: want %d, got %d", len(doc), end)
	}
	if len(tbl.Rows) != 2 {
		t.Errorf("Rows count: want 2, got %d", len(tbl.Rows))
	}
}

func TestParseAt_ByteRange_CanBeUsedToSpliceRerenderedTable(t *testing.T) {
	// The primary use case for ParseAt: parse a table from inside a larger document,
	// modify it, render the new table, and splice it back into the original document.
	preamble := []byte("Preamble text.\n\n")
	postamble := []byte("\nPostamble text.\n")
	doc := append(append(preamble, simpleTableBytes...), postamble...)

	tbl, start, end, err := mdtable.ParseAt(doc, 0)
	if err != nil {
		t.Fatalf("ParseAt: %v", err)
	}

	// Append a row and splice the new table back in.
	tbl2 := tbl.AppendRow([]string{"gamma", "three"})
	rendered := tbl2.Render()

	var buf bytes.Buffer
	buf.Write(doc[:start])
	buf.Write(rendered)
	buf.Write(doc[end:])
	result := buf.Bytes()

	// The preamble and postamble must be preserved.
	if !bytes.Contains(result, []byte("Preamble text.")) {
		t.Error("spliced document: preamble is missing")
	}
	if !bytes.Contains(result, []byte("Postamble text.")) {
		t.Error("spliced document: postamble is missing")
	}
	// The new row must appear in the spliced document.
	if !bytes.Contains(result, []byte("gamma")) {
		t.Error("spliced document: appended row value \"gamma\" is missing")
	}
	// Original rows must still be present.
	if !bytes.Contains(result, []byte("alpha")) {
		t.Error("spliced document: original row value \"alpha\" is missing")
	}
}

func TestParseAt_OffsetPastFirstTable_FindsSecondTable(t *testing.T) {
	// When the offset is past the first table, ParseAt finds the next one.
	firstTable := []byte("| A |\n| - |\n| x |\n")
	gap := []byte("\nsome text\n\n")
	secondTable := []byte("| B |\n| - |\n| y |\n")
	doc := append(append(firstTable, gap...), secondTable...)
	offsetPastFirst := len(firstTable) + 1

	tbl, _, _, err := mdtable.ParseAt(doc, offsetPastFirst)

	if err != nil {
		t.Fatalf("ParseAt past first table: unexpected error: %v", err)
	}
	if len(tbl.Header) != 1 || tbl.Header[0] != "B" {
		t.Errorf("ParseAt at offset past first table: want second table with header \"B\", got %v", tbl.Header)
	}
}

func TestParseAt_NoTableAfterOffset_ReturnsError(t *testing.T) {
	input := []byte("| A |\n| - |\n| x |\n")
	// Set offset to end of file: no table can start at or after this position.
	offset := len(input)

	_, _, _, err := mdtable.ParseAt(input, offset)

	if err == nil {
		t.Error("ParseAt with offset at end of input: expected error, got nil")
	}
}

func TestParseAt_NegativeOffset_ReturnsError(t *testing.T) {
	_, _, _, err := mdtable.ParseAt(simpleTableBytes, -1)

	if err == nil {
		t.Error("ParseAt with negative offset: expected error, got nil")
	}
}

func TestParseAt_OffsetBeyondDataLength_ReturnsError(t *testing.T) {
	_, _, _, err := mdtable.ParseAt(simpleTableBytes, len(simpleTableBytes)+1)

	if err == nil {
		t.Error("ParseAt with offset beyond data length: expected error, got nil")
	}
}

// --- AppendRow: panic contract ---

func TestAppendRow_WrongValuesLength_Panics(t *testing.T) {
	// AppendRow must panic when the values slice length differs from the header length.
	// This is an intentional API contract: callers must provide exactly one value per column.
	tbl := mustParse(t, simpleTableBytes) // 2 columns

	defer func() {
		if r := recover(); r == nil {
			t.Error("AppendRow with wrong values length: expected panic, got none")
		}
	}()

	_ = tbl.AppendRow([]string{"only-one"}) // want 2, got 1 — must panic
}

// --- UpsertRow: panic contract ---

func TestUpsertRow_WrongValuesLength_Panics(t *testing.T) {
	// UpsertRow must panic when the values slice length differs from the header length.
	tbl := mustParse(t, simpleTableBytes) // 2 columns
	keyCol := tbl.Column("Name")

	defer func() {
		if r := recover(); r == nil {
			t.Error("UpsertRow with wrong values length: expected panic, got none")
		}
	}()

	_ = tbl.UpsertRow(keyCol, "alpha", []string{"too", "many", "values"}) // want 2, got 3 — must panic
}

// --- Cell: panic contract ---

func TestCell_OutOfRange_Panics(t *testing.T) {
	// Cell must panic when either the row or column index is out of range.
	// This is an intentional API contract: callers must validate indices before calling Cell.
	tbl := mustParse(t, simpleTableBytes) // 2 rows, 2 cols

	t.Run("row out of range", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Cell with row index out of range: expected panic, got none")
			}
		}()
		_ = tbl.Cell(99, 0)
	})

	t.Run("col out of range", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Cell with col index out of range: expected panic, got none")
			}
		}()
		_ = tbl.Cell(0, 99)
	})
}

// --- Render: nil Widths ---

func TestRender_NilWidths_ExpandsFromContent(t *testing.T) {
	// Render on a Table with nil Widths must produce valid output by deriving column widths
	// from the cell content. This covers the zero-width starting point: each column expands
	// to fit its widest cell value when no stored widths are provided.
	tbl := mdtable.Table{
		Header: []string{"Name", "Value"},
		Rows: [][]string{
			{"alpha", "one"},
			{"beta", "two"},
		},
		// Widths is nil (zero value for []int)
	}

	rendered := tbl.Render()

	if len(rendered) == 0 {
		t.Fatal("Render with nil Widths: returned empty output")
	}
	// The rendered table must parse back to an equivalent table.
	reparsed, err := mdtable.Parse(rendered)
	if err != nil {
		t.Fatalf("Parse of nil-Widths render: %v", err)
	}
	if len(reparsed.Header) != 2 {
		t.Errorf("reparsed Header length: want 2, got %d", len(reparsed.Header))
	}
	if reparsed.Header[0] != "Name" {
		t.Errorf("reparsed Header[0]: want %q, got %q", "Name", reparsed.Header[0])
	}
	if reparsed.Header[1] != "Value" {
		t.Errorf("reparsed Header[1]: want %q, got %q", "Value", reparsed.Header[1])
	}
	if len(reparsed.Rows) != 2 {
		t.Errorf("reparsed Rows count: want 2, got %d", len(reparsed.Rows))
	}
	if reparsed.Rows[0][0] != "alpha" {
		t.Errorf("reparsed Rows[0][0]: want %q, got %q", "alpha", reparsed.Rows[0][0])
	}
	if reparsed.Rows[1][0] != "beta" {
		t.Errorf("reparsed Rows[1][0]: want %q, got %q", "beta", reparsed.Rows[1][0])
	}
}
