package mdtable_test

// Tests for structural-mismatch reporting (ParseStrict, StructureError).
//
// Coverage:
//   - A data row with more cells than the header is reported as *StructureError
//     with row index, line number, cell count and column count.
//   - The first offending row is the one reported; no table is returned.
//   - Fewer cells are padded and not an error; escaped pipes do not count.
//   - A well-formed table parses like Parse.
//   - Parse and ParseAt stay lenient and still cut extra cells.
//   - Other Parse errors surface from ParseStrict.

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"mosaic-common/mdtable"
)

const extraCellsTable = "| A | B |\n" +
	"| --- | --- |\n" +
	"| 1 | 2 |\n" +
	"| 3 | 4 | 5 |\n"

func TestParseStrict_ExtraCells_ReportsStructureError(t *testing.T) {
	tbl, err := mdtable.ParseStrict([]byte(extraCellsTable))

	var se *mdtable.StructureError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *mdtable.StructureError", err)
	}
	if se.Row != 1 || se.Line != 4 || se.Cells != 3 || se.Columns != 2 {
		t.Errorf("StructureError = %+v, want Row 1, Line 4, Cells 3, Columns 2", *se)
	}
	if len(tbl.Header) != 0 || len(tbl.Rows) != 0 {
		t.Errorf("table must be empty on error, got %+v", tbl)
	}
}

func TestParseStrict_ErrorText_NamesRowAndLine(t *testing.T) {
	_, err := mdtable.ParseStrict([]byte(extraCellsTable))

	if err == nil {
		t.Fatal("want an error for a row with extra cells")
	}
	for _, part := range []string{"data row 1", "line 4"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q must contain %q", err.Error(), part)
		}
	}
}

func TestParseStrict_LeadingBlankLines_CountedInLineNumber(t *testing.T) {
	data := "\n\n" + extraCellsTable

	_, err := mdtable.ParseStrict([]byte(data))

	var se *mdtable.StructureError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *mdtable.StructureError", err)
	}
	if se.Line != 6 {
		t.Errorf("Line = %d, want 6 (counted from the start of the input)", se.Line)
	}
}

func TestParseStrict_SeveralOffendingRows_ReportsFirst(t *testing.T) {
	data := "| A |\n| --- |\n| 1 |\n| 2 | x |\n| 3 | y | z |\n"

	_, err := mdtable.ParseStrict([]byte(data))

	var se *mdtable.StructureError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *mdtable.StructureError", err)
	}
	if se.Row != 1 || se.Cells != 2 || se.Columns != 1 {
		t.Errorf("StructureError = %+v, want first offending row (Row 1, Cells 2, Columns 1)", *se)
	}
}

func TestParseStrict_FewerCells_PaddedAndNoError(t *testing.T) {
	data := "| A | B | C |\n| --- | --- | --- |\n| 1 | 2 |\n"

	got, err := mdtable.ParseStrict([]byte(data))

	if err != nil {
		t.Fatalf("a short row is not an error: %v", err)
	}
	if want := [][]string{{"1", "2", ""}}; !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("rows = %q, want %q", got.Rows, want)
	}
}

func TestParseStrict_EscapedPipe_DoesNotCountAsExtraCell(t *testing.T) {
	data := "| A | B |\n| --- | --- |\n| a\\|b | c |\n"

	got, err := mdtable.ParseStrict([]byte(data))

	if err != nil {
		t.Fatalf("escaped pipe must not create a cell: %v", err)
	}
	if want := [][]string{{`a\|b`, "c"}}; !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("rows = %q, want %q", got.Rows, want)
	}
}

func TestParseStrict_UnescapedPipe_IsReportedAsExtraCell(t *testing.T) {
	data := "| A | B |\n| --- | --- |\n| a|b | c |\n"

	_, err := mdtable.ParseStrict([]byte(data))

	var se *mdtable.StructureError
	if !errors.As(err, &se) || se.Cells != 3 {
		t.Errorf("err = %v, want *StructureError with 3 cells", err)
	}
}

func TestParseStrict_WellFormedTable_MatchesParse(t *testing.T) {
	want := mustParse(t, simpleTableBytes)

	got, err := mdtable.ParseStrict(simpleTableBytes)

	if err != nil {
		t.Fatalf("ParseStrict: %v", err)
	}
	if !reflect.DeepEqual(got.Header, want.Header) || !reflect.DeepEqual(got.Rows, want.Rows) ||
		!reflect.DeepEqual(got.Widths, want.Widths) {
		t.Errorf("ParseStrict = %+v, want %+v", got, want)
	}
	if len(got.Rows) == 0 {
		t.Fatal("fixture must have rows")
	}
}

func TestParse_ExtraCells_StaysLenientAndCutsSilently(t *testing.T) {
	got := mustParse(t, []byte(extraCellsTable))

	want := [][]string{{"1", "2"}, {"3", "4"}}
	if !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("rows = %q, want %q", got.Rows, want)
	}
	tbl, _, _, err := mdtable.ParseAt([]byte(extraCellsTable), 0)
	if err != nil || !reflect.DeepEqual(tbl.Rows, want) {
		t.Errorf("ParseAt must stay lenient: rows = %q, err = %v", tbl.Rows, err)
	}
}

func TestParseStrict_OtherParseErrors_AreReturned(t *testing.T) {
	cases := map[string]string{
		"no table":                  "just text\n",
		"header/separator mismatch": "| A | B |\n| --- |\n",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := mdtable.ParseStrict([]byte(data))
			if err == nil {
				t.Fatal("want an error")
			}
			var se *mdtable.StructureError
			if errors.As(err, &se) {
				t.Errorf("must not be a StructureError: %v", err)
			}
		})
	}
}
