package mdtable_test

// Tests for Table.RenderCompact: content-driven line length, no padding.
//
// Coverage:
//   - Exact output layout without padding and with a "---" separator.
//   - Stored Widths are ignored (line length driven by the line's own cells).
//   - One very long cell does not widen any other line.
//   - Every line's byte length follows 1 + sum(len(cell)+3).
//   - Output parses back (Parse and ParseStrict) to the same header and rows.
//   - Short rows are written with empty cells; surplus cells are not written.
//   - Render keeps its width-preserving behaviour.

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"mosaic-common/mdtable"
)

func compactLines(out []byte) []string {
	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}

func TestRenderCompact_SimpleTable_WritesUnpaddedLines(t *testing.T) {
	tbl := mdtable.Table{
		Header: []string{"A", "B"},
		Rows:   [][]string{{"x", "yy"}},
	}

	got := string(tbl.RenderCompact())

	want := "| A | B |\n| --- | --- |\n| x | yy |\n"
	if got != want {
		t.Errorf("RenderCompact:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderCompact_StoredWidths_AreIgnored(t *testing.T) {
	tbl := mdtable.Table{
		Header: []string{"A", "B"},
		Rows:   [][]string{{"x", "y"}},
		Widths: []int{40, 60},
	}

	got := string(tbl.RenderCompact())

	want := "| A | B |\n| --- | --- |\n| x | y |\n"
	if got != want {
		t.Errorf("RenderCompact must ignore Widths:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderCompact_OneVeryLongCell_DoesNotWidenOtherLines(t *testing.T) {
	long := strings.Repeat("z", 500)
	tbl := mdtable.Table{
		Header: []string{"Seq", "Summary"},
		Rows: [][]string{
			{"1", "short"},
			{"2", long},
			{"3", "tiny"},
		},
	}

	lines := compactLines(tbl.RenderCompact())

	if len(lines) != 5 {
		t.Fatalf("want 5 lines (header, separator, 3 rows), got %d", len(lines))
	}
	for _, i := range []int{0, 1, 2, 4} {
		if len(lines[i]) > 40 {
			t.Errorf("line %d widened by an unrelated long cell (len %d): %q", i, len(lines[i]), lines[i])
		}
	}
	if !strings.Contains(lines[3], long) {
		t.Errorf("long row must still contain its full cell")
	}
}

func TestRenderCompact_EveryLine_ByteLengthFollowsOwnCells(t *testing.T) {
	tbl := mdtable.Table{
		Header: []string{"Name", "Wert"},
		Rows: [][]string{
			{"alpha", "one"},
			{"café", "über"}, // multi-byte: len counts bytes
			{"", "x"},
		},
		Widths: []int{30, 30},
	}

	lines := compactLines(tbl.RenderCompact())

	cellsPerLine := [][]string{
		tbl.Header,
		{"---", "---"},
		tbl.Rows[0], tbl.Rows[1], tbl.Rows[2],
	}
	if len(lines) != len(cellsPerLine) {
		t.Fatalf("want %d lines, got %d", len(cellsPerLine), len(lines))
	}
	for i, cells := range cellsPerLine {
		want := 1
		for _, c := range cells {
			want += len(c) + 3
		}
		if len(lines[i]) != want {
			t.Errorf("line %d length = %d, want %d: %q", i, len(lines[i]), want, lines[i])
		}
	}
}

func TestRenderCompact_Output_ParsesBackToSameHeaderAndRows(t *testing.T) {
	tbl := mdtable.Table{
		Header: []string{"Seq", "Agent", "Summary"},
		Rows: [][]string{
			{"1", "researcher#1", "did things"},
			{"2", "builder#1", strings.Repeat("long ", 80)[:399]},
			{"3", "-", ""},
		},
		Widths: []int{3, 50, 9},
	}

	parsers := map[string]func([]byte) (mdtable.Table, error){
		"Parse":       mdtable.Parse,
		"ParseStrict": mdtable.ParseStrict,
	}
	for name, parse := range parsers {
		t.Run(name, func(t *testing.T) {
			got, err := parse(tbl.RenderCompact())
			if err != nil {
				t.Fatalf("parse of compact output: %v", err)
			}
			if !reflect.DeepEqual(got.Header, tbl.Header) {
				t.Errorf("header = %v, want %v", got.Header, tbl.Header)
			}
			if !reflect.DeepEqual(got.Rows, tbl.Rows) {
				t.Errorf("rows = %v, want %v", got.Rows, tbl.Rows)
			}
		})
	}
}

func TestRenderCompact_EmptyTable_WritesHeaderAndSeparator(t *testing.T) {
	tbl := mdtable.Table{Header: []string{"A", "B", "C"}}

	got := string(tbl.RenderCompact())

	want := "| A | B | C |\n| --- | --- | --- |\n"
	if got != want {
		t.Errorf("RenderCompact empty:\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderCompact_ShortRow_WrittenWithEmptyCells(t *testing.T) {
	tbl := mdtable.Table{
		Header: []string{"A", "B", "C"},
		Rows:   [][]string{{"x"}},
	}

	lines := compactLines(tbl.RenderCompact())

	if len(lines) != 3 || lines[2] != "| x |  |  |" {
		t.Errorf("short row line = %q, want %q (lines: %q)", lines[len(lines)-1], "| x |  |  |", lines)
	}
}

func TestRenderCompact_SurplusCells_AreNotWritten(t *testing.T) {
	tbl := mdtable.Table{
		Header: []string{"A", "B"},
		Rows:   [][]string{{"x", "y", "extra"}},
	}

	got := string(tbl.RenderCompact())

	if strings.Contains(got, "extra") {
		t.Errorf("cell beyond header must not be written: %q", got)
	}
	if !strings.Contains(got, "| x | y |\n") {
		t.Errorf("row within header columns missing: %q", got)
	}
}

func TestRender_StoredWidths_StillPadColumns(t *testing.T) {
	tbl := mdtable.Table{
		Header: []string{"A"},
		Rows:   [][]string{{"x"}},
		Widths: []int{6},
	}

	got := tbl.Render()

	want := []byte("| A      |\n| ------ |\n| x      |\n")
	if !bytes.Equal(got, want) {
		t.Errorf("Render must stay width-preserving:\n got: %q\nwant: %q", got, want)
	}
}
