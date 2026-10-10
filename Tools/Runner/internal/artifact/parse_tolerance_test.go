package artifact_test

// Tests that one logical artifact, written with varied column padding, trailing
// spaces, separator alignment and blank lines around the region tags, parses to
// an identical ArtifactState.

import (
	"reflect"
	"strings"
	"testing"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

// layoutVariant describes one way of writing the same artifact content.
type layoutVariant struct {
	name       string
	pad        bool   // pad every column to a common width
	trailing   string // appended to every table line and tag line
	align      string // separator cell text
	gapAfter   string // text between an open tag line and its table
	gapBefore  string // text between a table and its close tag line
	tagIndent  string // prefix of every tag line
	tagTrailer string // suffix of every tag line (before the newline)
}

var layoutVariants = []layoutVariant{
	{name: "compact", align: "---", gapAfter: "", gapBefore: ""},
	{name: "canonical blank lines", align: "---", gapAfter: "\n", gapBefore: "\n"},
	{name: "padded", pad: true, align: "---", gapAfter: "\n", gapBefore: "\n"},
	{name: "trailing spaces on table lines", trailing: "   ", align: "---", gapAfter: "\n", gapBefore: "\n"},
	{name: "left aligned separator", align: ":---", gapAfter: "\n", gapBefore: "\n"},
	{name: "right aligned separator", align: "---:", gapAfter: "", gapBefore: ""},
	{name: "centred separator padded", pad: true, align: ":---:", gapAfter: "\n", gapBefore: "\n"},
	{name: "many blank lines", align: "---", gapAfter: "\n\n\n", gapBefore: "\n\n\n"},
	{name: "whitespace only lines around tables", align: "---", gapAfter: "  \n\t\n", gapBefore: " \n\t \n"},
	{name: "trailing spaces after tags", align: "---", gapAfter: "\n", gapBefore: "\n", tagTrailer: "  "},
	{name: "indented tags", align: "---", gapAfter: "\n", gapBefore: "\n", tagIndent: "  "},
	{name: "tab after tags and no blank lines", align: "---", tagTrailer: "\t"},
}

type tableSpec struct {
	section string
	header  []string
	rows    [][]string
}

var toleranceTables = []tableSpec{
	{
		section: "ExecutionLog",
		header:  []string{"Seq", "Agent", "Phase", "Stage", "WorkflowRow", "Status", "Timestamp", "Summary", "Inputs", "Checkpoint"},
		rows: [][]string{
			{"1", "researcher#1", "RESEARCH", "-", "2", "SUCCESS", "2026-10-07T17:40:11Z", "done", "Requirements.md", "-"},
			{"2", "builder#2", "EXECUTION", "Implementation.1", "3", "BLOCKED", "2026-10-07T17:41:11Z", "tool down [error:E501]", "-", "-"},
		},
	},
	{
		section: "Artifacts",
		header:  []string{"Artifact", "Created In", "Created By"},
		rows:    [][]string{{"Research.md", "RESEARCH", "researcher#1"}},
	},
	{
		section: "WorkflowNotes",
		header:  []string{"Seq", "Note"},
		rows:    [][]string{{"1", "first note"}, {"1", "second note"}},
	},
}

func toleranceFrontmatter() string {
	return "---\n" +
		"type: orchestration-artifact\n" +
		"run_id: " + testRunID + "\n" +
		"workflow: test\n" +
		"workflow_version: \"1.0\"\n" +
		"task: \"test\"\n" +
		"started: 2026-01-01T00:00:00Z\n" +
		"last_updated: 2026-01-01T01:00:00Z\n" +
		"global_sequence: 2\n" +
		"checkpoints: disabled\n" +
		"current_state:\n" +
		"  phase: EXECUTION\n" +
		"  stage: Implementation.1\n" +
		"  last_status: BLOCKED\n" +
		"  last_agent: \"builder#2\"\n" +
		"  error_code: E501\n" +
		"---\n"
}

// separatorCell returns the separator cell, widened with dashes when padding.
func (v layoutVariant) separatorCell(width int) string {
	c := v.align
	for v.pad && len(c) < width {
		c = c[:1] + "-" + c[1:]
	}
	return c
}

// writeTable writes one table in the variant's style.
func (v layoutVariant) writeTable(ts tableSpec) string {
	widths := make([]int, len(ts.header))
	if v.pad {
		all := append([][]string{ts.header}, ts.rows...)
		for _, row := range all {
			for i, c := range row {
				if len(c) > widths[i] {
					widths[i] = len(c)
				}
			}
		}
	}
	line := func(cells []string, separator bool) string {
		var b strings.Builder
		b.WriteString("|")
		for i, c := range cells {
			if separator {
				c = v.separatorCell(widths[i])
			} else if len(c) < widths[i] {
				c += strings.Repeat(" ", widths[i]-len(c))
			}
			b.WriteString(" " + c + " |")
		}
		return b.String() + v.trailing + "\n"
	}
	out := line(ts.header, false) + line(ts.header, true)
	for _, r := range ts.rows {
		out += line(r, false)
	}
	return out
}

func (v layoutVariant) artifactBytes() []byte {
	var b strings.Builder
	b.WriteString(toleranceFrontmatter())
	b.WriteString("\n")
	for i, ts := range toleranceTables {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(v.tagIndent + `<` + ts.section + ` type="core">` + v.tagTrailer + "\n")
		b.WriteString(v.gapAfter)
		b.WriteString(v.writeTable(ts))
		b.WriteString(v.gapBefore)
		b.WriteString(v.tagIndent + `</` + ts.section + `>` + v.tagTrailer + "\n")
	}
	return []byte(b.String())
}

func TestParse_LayoutVariantsOfTheSameContentParseToTheSameState(t *testing.T) {
	reference, err := artifact.Parse(layoutVariants[0].artifactBytes())
	if err != nil {
		t.Fatalf("Parse(reference variant): %v", err)
	}
	assertToleranceReference(t, reference)

	for _, v := range layoutVariants[1:] {
		t.Run(v.name, func(t *testing.T) {
			got, err := artifact.Parse(v.artifactBytes())
			if err != nil {
				t.Fatalf("Parse: %v\n--- input ---\n%s", err, v.artifactBytes())
			}
			if !reflect.DeepEqual(got, reference) {
				t.Errorf("state differs from the compact variant\n got: %+v\nwant: %+v\n--- input ---\n%s", got, reference, v.artifactBytes())
			}
		})
	}
}

// assertToleranceReference pins the content the variants are meant to carry, so
// that equality between variants cannot hold for an all-empty parse.
func assertToleranceReference(t *testing.T, s domain.ArtifactState) {
	t.Helper()
	if len(s.ExecutionLog) != 2 || len(s.ArtifactRegistry) != 1 || len(s.WorkflowNotes) != 2 {
		t.Fatalf("reference counts: log=%d registry=%d notes=%d, want 2/1/2",
			len(s.ExecutionLog), len(s.ArtifactRegistry), len(s.WorkflowNotes))
	}
	blocked := s.ExecutionLog[1]
	if blocked.Summary != "tool down" || blocked.ErrorCode != domain.ErrorCode("E501") || blocked.Stage != "Implementation.1" || blocked.WorkflowRow != 3 {
		t.Errorf("reference BLOCKED row = %+v", blocked)
	}
	if s.WorkflowNotes[1].Note != "second note" || s.ArtifactRegistry[0].Artifact != "Research.md" {
		t.Errorf("reference notes/registry = %+v / %+v", s.WorkflowNotes, s.ArtifactRegistry)
	}
}

func TestParse_StrictAboutHeadersAndColumnCountDespiteLayoutTolerance(t *testing.T) {
	good := layoutVariants[1].artifactBytes()
	extra := strings.Replace(string(good), "| 1 | first note |", "| 1 | first | note |", 1)
	if extra == string(good) {
		t.Fatal("test setup: replacement did not apply")
	}
	if _, err := artifact.Parse([]byte(extra)); err == nil {
		t.Error("Parse of a row with an extra cell succeeded, want a refusal")
	}
}
