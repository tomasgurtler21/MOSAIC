package artifact_test

// Tests for the layout of the rendered Orchestration.md body: every core table
// is separated from its region tags by a blank line, and line length is driven
// by the row's own content (no column-width padding).

import (
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

const (
	execLogHeaderLine = "| Seq | Agent | Phase | Stage | WorkflowRow | Status | Timestamp | Summary | Inputs | Checkpoint |"
	execLogSepLine    = "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |"
)

var layoutSections = []string{"ExecutionLog", "Artifacts", "WorkflowNotes"}

func layoutTimestamp() time.Time {
	return time.Date(2026, 10, 7, 17, 40, 11, 0, time.UTC)
}

// layoutState builds a state with rows in all three tables.
func layoutState(summary string) domain.ArtifactState {
	return domain.ArtifactState{
		RunID: testRunID,
		ExecutionLog: []domain.ExecutionLogEntry{
			{Seq: 1, Agent: "researcher#1", Phase: "RESEARCH", WorkflowRow: 2, Status: domain.StatusSUCCESS,
				Timestamp: layoutTimestamp(), Summary: "done", Inputs: "Requirements.md"},
			{Seq: 2, Agent: "builder#2", Phase: "EXECUTION", Stage: "Implementation.1", WorkflowRow: 3,
				Status: domain.StatusSUCCESS, Timestamp: layoutTimestamp(), Summary: summary},
		},
		ArtifactRegistry: []domain.ArtifactRegistryEntry{
			{Artifact: "Research.md", CreatedIn: "RESEARCH", CreatedBy: "researcher#1"},
		},
		WorkflowNotes: []domain.WorkflowNote{{Seq: 1, Note: "first note"}, {Seq: 2, Note: "second note"}},
	}
}

// renderBodyLines renders the state and returns the lines after the frontmatter.
func renderBodyLines(t *testing.T, state domain.ArtifactState) []string {
	t.Helper()
	data, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	dashes := 0
	for i, l := range lines {
		if l == "---" {
			dashes++
			if dashes == 2 {
				return lines[i+1:]
			}
		}
	}
	t.Fatalf("frontmatter end not found in:\n%s", data)
	return nil
}

func indexOfLine(t *testing.T, lines []string, want string) int {
	t.Helper()
	for i, l := range lines {
		if l == want {
			return i
		}
	}
	t.Fatalf("line %q not found in body:\n%s", want, strings.Join(lines, "\n"))
	return -1
}

func TestRender_BlankLineSeparatesEachTableFromItsRegionTags(t *testing.T) {
	populated := layoutState("second summary")
	empty := domain.ArtifactState{RunID: testRunID}
	for name, state := range map[string]domain.ArtifactState{"populated": populated, "empty": empty} {
		t.Run(name, func(t *testing.T) {
			lines := renderBodyLines(t, state)
			for _, section := range layoutSections {
				open := indexOfLine(t, lines, `<`+section+` type="core">`)
				closeIdx := indexOfLine(t, lines, `</`+section+`>`)
				if lines[open+1] != "" {
					t.Errorf("%s: line after the open tag = %q, want a blank line", section, lines[open+1])
				}
				if lines[closeIdx-1] != "" {
					t.Errorf("%s: line before the close tag = %q, want a blank line", section, lines[closeIdx-1])
				}
				first, last := lines[open+2], lines[closeIdx-2]
				if !strings.HasPrefix(first, "| ") || !strings.HasPrefix(last, "|") {
					t.Fatalf("%s: table not found between tags: first=%q last=%q", section, first, last)
				}
				for _, l := range lines[open+2 : closeIdx-1] {
					if !strings.HasPrefix(l, "|") {
						t.Errorf("%s: table has a non-table line %q (blank lines inside the table break it)", section, l)
					}
				}
			}
		})
	}
}

func TestRender_SectionsStaySeparatedByOneBlankLine(t *testing.T) {
	lines := renderBodyLines(t, layoutState("second summary"))
	pairs := [][2]string{{"ExecutionLog", "Artifacts"}, {"Artifacts", "WorkflowNotes"}}
	for _, p := range pairs {
		closeIdx := indexOfLine(t, lines, `</`+p[0]+`>`)
		open := indexOfLine(t, lines, `<`+p[1]+` type="core">`)
		if open != closeIdx+2 || lines[closeIdx+1] != "" {
			t.Errorf("%s -> %s: want exactly one blank line between sections, got close at %d, open at %d", p[0], p[1], closeIdx, open)
		}
	}
}

func TestRender_TablesAreWrittenWithoutColumnPadding(t *testing.T) {
	lines := renderBodyLines(t, layoutState("second summary"))
	want := []string{
		execLogHeaderLine,
		execLogSepLine,
		"| 1 | researcher#1 | RESEARCH | - | 2 | SUCCESS | 2026-10-07T17:40:11Z | done | Requirements.md | - |",
		"| 2 | builder#2 | EXECUTION | Implementation.1 | 3 | SUCCESS | 2026-10-07T17:40:11Z | second summary | - | - |",
		"| Artifact | Created In | Created By |",
		"| --- | --- | --- |",
		"| Research.md | RESEARCH | researcher#1 |",
		"| Seq | Note |",
		"| --- | --- |",
		"| 1 | first note |",
		"| 2 | second note |",
	}
	for _, w := range want {
		indexOfLine(t, lines, w)
	}
}

func TestRender_LineLengthIsDrivenByTheRowsOwnContent(t *testing.T) {
	long := strings.Repeat("x", 90)
	short := "ok"
	linesLong := renderBodyLines(t, layoutState(long))
	linesShort := renderBodyLines(t, layoutState(short))

	rowLong := indexOfLine(t, linesLong, "| 2 | builder#2 | EXECUTION | Implementation.1 | 3 | SUCCESS | 2026-10-07T17:40:11Z | "+long+" | - | - |")
	rowShort := indexOfLine(t, linesShort, "| 2 | builder#2 | EXECUTION | Implementation.1 | 3 | SUCCESS | 2026-10-07T17:40:11Z | "+short+" | - | - |")
	// The first row must not grow because the second row has a long cell.
	first := "| 1 | researcher#1 | RESEARCH | - | 2 | SUCCESS | 2026-10-07T17:40:11Z | done | Requirements.md | - |"
	if len(linesLong[rowLong-1]) != len(first) || len(linesShort[rowShort-1]) != len(first) {
		t.Errorf("first row length = %d / %d, want %d regardless of the other row's content",
			len(linesLong[rowLong-1]), len(linesShort[rowShort-1]), len(first))
	}
	// Header and separator are not stretched by the widest cell.
	if linesLong[rowLong-3] != execLogHeaderLine || linesLong[rowLong-2] != execLogSepLine {
		t.Errorf("header/separator = %q / %q, want the unpadded forms", linesLong[rowLong-3], linesLong[rowLong-2])
	}
}

func TestRender_NoLineCarriesRunsOfPaddingSpaces(t *testing.T) {
	lines := renderBodyLines(t, layoutState(strings.Repeat("y", 80)))
	for _, l := range lines {
		if strings.HasPrefix(l, "|") && strings.Contains(l, "  ") {
			t.Errorf("table line carries padding spaces: %q", l)
		}
	}
}
