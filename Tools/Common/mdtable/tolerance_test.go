package mdtable_test

// Tests for parse tolerance: formatting variations of one table parse to the
// same header and rows in Parse and ParseStrict.
//
// Coverage:
//   - Padded versus unpadded cells.
//   - Trailing spaces after the last pipe and indented rows.
//   - Alignment colons in the separator row.
//   - Zero, one or several blank lines (and leading prose) before the table.
//   - Windows line endings.

import (
	"reflect"
	"testing"

	"mosaic-common/mdtable"
)

var toleranceHeader = []string{"Seq", "Agent", "Note"}

var toleranceRows = [][]string{
	{"1", "researcher#1", "done"},
	{"2", "builder#1", ""},
}

const toleranceCanonical = "| Seq | Agent | Note |\n" +
	"| --- | --- | --- |\n" +
	"| 1 | researcher#1 | done |\n" +
	"| 2 | builder#1 |  |\n"

var toleranceVariants = map[string]string{
	"canonical": toleranceCanonical,
	"padded": "| Seq | Agent        | Note |\n" +
		"| --- | ------------ | ---- |\n" +
		"| 1   | researcher#1 | done |\n" +
		"| 2   | builder#1    |      |\n",
	"unpadded": "|Seq|Agent|Note|\n|---|---|---|\n|1|researcher#1|done|\n|2|builder#1||\n",
	"trailing spaces": "| Seq | Agent | Note |   \n" +
		"| --- | --- | --- |  \n" +
		"| 1 | researcher#1 | done | \t\n" +
		"| 2 | builder#1 |  |    \n",
	"indented rows": "  | Seq | Agent | Note |\n" +
		"  | --- | --- | --- |\n" +
		"    | 1 | researcher#1 | done |\n" +
		"\t| 2 | builder#1 |  |\n",
	"alignment colons": "| Seq | Agent | Note |\n" +
		"| :--- | ---: | :---: |\n" +
		"| 1 | researcher#1 | done |\n" +
		"| 2 | builder#1 |  |\n",
	"alignment colons unpadded": "|Seq|Agent|Note|\n|:--|--:|:-:|\n|1|researcher#1|done|\n|2|builder#1||\n",
	"one blank line before":     "\n" + toleranceCanonical,
	"many blank lines before":   "\n\n\n\n" + toleranceCanonical,
	"whitespace-only lines":     "  \n\t\n" + toleranceCanonical,
	"leading prose":             "Some text.\n\n\n" + toleranceCanonical,
	"no final newline":          toleranceCanonical[:len(toleranceCanonical)-1],
	"windows line endings": "| Seq | Agent | Note |\r\n| --- | --- | --- |\r\n" +
		"| 1 | researcher#1 | done |\r\n| 2 | builder#1 |  |\r\n",
	"blank lines after table": toleranceCanonical + "\n\nmore text\n",
}

func TestParse_FormattingVariations_YieldIdenticalHeaderAndRows(t *testing.T) {
	for name, data := range toleranceVariants {
		t.Run(name, func(t *testing.T) {
			got := mustParse(t, []byte(data))
			if !reflect.DeepEqual(got.Header, toleranceHeader) {
				t.Errorf("header = %q, want %q", got.Header, toleranceHeader)
			}
			if !reflect.DeepEqual(got.Rows, toleranceRows) {
				t.Errorf("rows = %q, want %q", got.Rows, toleranceRows)
			}
		})
	}
}

func TestParseStrict_FormattingVariations_YieldIdenticalHeaderAndRows(t *testing.T) {
	for name, data := range toleranceVariants {
		t.Run(name, func(t *testing.T) {
			got, err := mdtable.ParseStrict([]byte(data))
			if err != nil {
				t.Fatalf("ParseStrict: %v", err)
			}
			if !reflect.DeepEqual(got.Header, toleranceHeader) {
				t.Errorf("header = %q, want %q", got.Header, toleranceHeader)
			}
			if !reflect.DeepEqual(got.Rows, toleranceRows) {
				t.Errorf("rows = %q, want %q", got.Rows, toleranceRows)
			}
		})
	}
}

func TestParseStrict_CompactAndPaddedRender_ParseIdentically(t *testing.T) {
	tbl := mdtable.Table{Header: toleranceHeader, Rows: toleranceRows, Widths: []int{8, 20, 30}}

	compact, err1 := mdtable.ParseStrict(tbl.RenderCompact())
	padded, err2 := mdtable.ParseStrict(tbl.Render())

	if err1 != nil || err2 != nil {
		t.Fatalf("errors: compact=%v padded=%v", err1, err2)
	}
	if !reflect.DeepEqual(compact.Header, padded.Header) || !reflect.DeepEqual(compact.Rows, padded.Rows) {
		t.Errorf("compact %+v and padded %+v differ", compact, padded)
	}
	if !reflect.DeepEqual(compact.Rows, toleranceRows) {
		t.Errorf("rows = %q, want %q", compact.Rows, toleranceRows)
	}
}
