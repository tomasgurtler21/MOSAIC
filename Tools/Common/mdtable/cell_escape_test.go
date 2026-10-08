package mdtable_test

// Tests for cell escaping and sanitising and the escape-aware row splitting.
//
// Coverage:
//   - EscapeCell writes a pipe as backslash-pipe and changes nothing else.
//   - UnescapeCell reverses EscapeCell for any value.
//   - SanitizeCell turns line breaks into single spaces, trims, escapes pipes.
//   - A sanitised cell with a pipe and a newline renders on one line, adds no
//     column, and parses back to the documented value.
//   - Parse, ParseAt and ParseStrict honour a backslash-pipe as cell content
//     and return the cell raw (backslash kept).
//   - Rows without backslash-pipe split exactly as before.

import (
	"reflect"
	"strings"
	"testing"

	"mosaic-common/mdtable"
)

func TestEscapeCell_Pipe_BecomesBackslashPipe(t *testing.T) {
	cases := map[string]string{
		"a|b":        `a\|b`,
		"||":         `\|\|`,
		"no pipes":   "no pipes",
		"":           "",
		`back\slash`: `back\slash`, // backslashes are not doubled
	}
	for in, want := range cases {
		if got := mdtable.EscapeCell(in); got != want {
			t.Errorf("EscapeCell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnescapeCell_RoundTripsEscapeCell(t *testing.T) {
	values := []string{"", "plain", "a|b", `a\|b`, `trailing\`, "|", `\`, `x\\|y`, "a | b | c"}
	for _, v := range values {
		if got := mdtable.UnescapeCell(mdtable.EscapeCell(v)); got != v {
			t.Errorf("UnescapeCell(EscapeCell(%q)) = %q", v, got)
		}
	}
}

func TestUnescapeCell_BackslashPipe_BecomesPipe(t *testing.T) {
	if got := mdtable.UnescapeCell(`a\|b`); got != "a|b" {
		t.Errorf("UnescapeCell = %q, want %q", got, "a|b")
	}
}

func TestSanitizeCell_LineBreaksAndPipes_BecomeOneSafeLine(t *testing.T) {
	cases := map[string]string{
		"a\nb":       "a b",
		"a\r\nb":     "a b",
		"a\rb":       "a b",
		"a\n\nb":     "a  b", // each break becomes one space
		"  padded  ": "padded",
		"x|y\nz":     `x\|y z`,
		"\n lead":    "lead",
		"trail \r\n": "trail",
		"plain":      "plain",
	}
	for in, want := range cases {
		if got := mdtable.SanitizeCell(in); got != want {
			t.Errorf("SanitizeCell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeCell_ParsedBack_UnescapesToSanitisedValue(t *testing.T) {
	v := "first | second\nthird\r\nfourth"
	tbl := mdtable.Table{
		Header: []string{"Seq", "Summary", "Inputs"},
		Rows:   [][]string{{"1", mdtable.SanitizeCell(v), "-"}},
	}

	out := tbl.RenderCompact()
	got := mustParse(t, out)

	if lines := strings.Count(strings.TrimSuffix(string(out), "\n"), "\n") + 1; lines != 3 {
		t.Errorf("table must stay 3 lines (header, separator, row), got %d", lines)
	}
	if len(got.Rows) != 1 || len(got.Rows[0]) != 3 {
		t.Fatalf("rows = %v, want 1 row of 3 cells", got.Rows)
	}
	want := "first | second third fourth"
	if cell := mdtable.UnescapeCell(got.Rows[0][1]); cell != want {
		t.Errorf("parsed summary = %q, want %q", cell, want)
	}
	if got.Rows[0][2] != "-" {
		t.Errorf("neighbouring cell shifted: %q", got.Rows[0][2])
	}
}

func TestSanitizeCell_PipeInEveryColumn_KeepsColumnCountStrict(t *testing.T) {
	tbl := mdtable.Table{
		Header: []string{"A", "B"},
		Rows:   [][]string{{mdtable.SanitizeCell("a|b"), mdtable.SanitizeCell("|")}},
	}

	got, err := mdtable.ParseStrict(tbl.RenderCompact())

	if err != nil {
		t.Fatalf("ParseStrict of sanitised table: %v", err)
	}
	if len(got.Rows) != 1 || mdtable.UnescapeCell(got.Rows[0][0]) != "a|b" || mdtable.UnescapeCell(got.Rows[0][1]) != "|" {
		t.Errorf("rows = %v", got.Rows)
	}
}

func TestParse_EscapedPipe_IsCellContentAndReturnedRaw(t *testing.T) {
	data := []byte("| A | B |\n| --- | --- |\n| a\\|b | c |\n")

	parsers := map[string]func([]byte) (mdtable.Table, error){
		"Parse":       mdtable.Parse,
		"ParseStrict": mdtable.ParseStrict,
		"ParseAt": func(d []byte) (mdtable.Table, error) {
			tbl, _, _, err := mdtable.ParseAt(d, 0)
			return tbl, err
		},
	}
	for name, parse := range parsers {
		t.Run(name, func(t *testing.T) {
			got, err := parse(data)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			want := [][]string{{`a\|b`, "c"}}
			if !reflect.DeepEqual(got.Rows, want) {
				t.Errorf("rows = %q, want %q", got.Rows, want)
			}
		})
	}
}

func TestParse_EscapedPipeInHeader_StaysOneColumn(t *testing.T) {
	data := []byte("| A\\|B | C |\n| --- | --- |\n| 1 | 2 |\n")

	got := mustParse(t, data)

	if !reflect.DeepEqual(got.Header, []string{`A\|B`, "C"}) {
		t.Errorf("header = %q", got.Header)
	}
}

func TestParse_RowWithoutEscapedPipe_SplitsAsBefore(t *testing.T) {
	data := []byte("| A | B | C |\n| --- | --- | --- |\n| x\\y | | z |\n")

	got := mustParse(t, data)

	want := [][]string{{`x\y`, "", "z"}}
	if !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("rows = %q, want %q", got.Rows, want)
	}
}
