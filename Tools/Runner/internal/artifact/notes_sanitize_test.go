package artifact_test

// Tests for cell sanitising (pipes and line breaks cannot break a table), the
// BLOCKED error marker surviving it, repeated Workflow Notes Seq values, and the
// refusal of structurally corrupt tables instead of silently dropping content.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mosaic-common/mdtable"
	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

const (
	notesRefusalPrefix = "failed to parse workflow notes: "
	logRefusalPrefix   = "failed to parse execution log: "
	registryPrefix     = "failed to parse artifact registry: "
)

// renderAndParse renders state and parses the result.
func renderAndParse(t *testing.T, state domain.ArtifactState) (domain.ArtifactState, []byte) {
	t.Helper()
	data, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got, err := artifact.Parse(data)
	if err != nil {
		t.Fatalf("Parse of rendered output: %v\n%s", err, data)
	}
	return got, data
}

func TestRender_NoteWithPipeAndNewlineRoundTripsWithoutBreakingTheTable(t *testing.T) {
	state := domain.ArtifactState{RunID: testRunID, WorkflowNotes: []domain.WorkflowNote{
		{Seq: 1, Note: "left | right\nsecond line"},
		{Seq: 2, Note: "plain"},
	}}
	got, data := renderAndParse(t, state)

	if len(got.WorkflowNotes) != 2 {
		t.Fatalf("notes = %+v, want 2 rows (the table was broken)\n%s", got.WorkflowNotes, data)
	}
	if want := "left | right second line"; got.WorkflowNotes[0].Note != want {
		t.Errorf("note[0] = %q, want %q (pipe kept, line break becomes one space)", got.WorkflowNotes[0].Note, want)
	}
	if got.WorkflowNotes[1].Note != "plain" || got.WorkflowNotes[1].Seq != 2 {
		t.Errorf("note[1] = %+v, want the following row intact", got.WorkflowNotes[1])
	}
	for _, l := range strings.Split(string(data), "\n") {
		if strings.Contains(l, "second line") && !strings.HasPrefix(l, "|") {
			t.Errorf("note continuation escaped onto its own line: %q", l)
		}
	}
}

func TestRender_EveryWrittenCellIsSanitised(t *testing.T) {
	state := domain.ArtifactState{
		RunID: testRunID,
		ExecutionLog: []domain.ExecutionLogEntry{{
			Seq: 1, Agent: "a|gent#1", Phase: "PH|ASE", Stage: "G|r.1", WorkflowRow: 2,
			Status: domain.StatusSUCCESS, Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Summary: "sum | mary\r\nnext", Inputs: "in|put.md\nmore", Checkpoint: "ch|eck",
		}},
		ArtifactRegistry: []domain.ArtifactRegistryEntry{{Artifact: "A|rt.md", CreatedIn: "P|H", CreatedBy: "b|y#1"}},
	}
	got, data := renderAndParse(t, state)

	if len(got.ExecutionLog) != 1 || len(got.ArtifactRegistry) != 1 {
		t.Fatalf("rows: log=%d registry=%d, want 1/1\n%s", len(got.ExecutionLog), len(got.ArtifactRegistry), data)
	}
	e := got.ExecutionLog[0]
	checks := map[string][2]string{
		"Agent":      {e.Agent, "a|gent#1"},
		"Phase":      {e.Phase, "PH|ASE"},
		"Stage":      {e.Stage, "G|r.1"},
		"Summary":    {e.Summary, "sum | mary next"},
		"Inputs":     {e.Inputs, "in|put.md more"},
		"Checkpoint": {e.Checkpoint, "ch|eck"},
	}
	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %q, want %q", name, c[0], c[1])
		}
	}
	r := got.ArtifactRegistry[0]
	if r.Artifact != "A|rt.md" || r.CreatedIn != "P|H" || r.CreatedBy != "b|y#1" {
		t.Errorf("registry entry = %+v, want pipes preserved", r)
	}
}

func TestRender_BlockedRowErrorMarkerSurvivesSanitising(t *testing.T) {
	state := domain.ArtifactState{RunID: testRunID, ExecutionLog: []domain.ExecutionLogEntry{{
		Seq: 1, Agent: "builder#1", Phase: "EXECUTION", WorkflowRow: 3, Status: domain.StatusBLOCKED,
		ErrorCode: domain.ErrorCode("E501"), Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Summary: "tool | unavailable\nretrying",
	}}}
	got, data := renderAndParse(t, state)

	if !strings.Contains(string(data), "[error:E501] |") {
		t.Errorf("rendered Summary cell does not end with the intact marker:\n%s", data)
	}
	if len(got.ExecutionLog) != 1 {
		t.Fatalf("log rows = %d, want 1 (the table was broken)\n%s", len(got.ExecutionLog), data)
	}
	e := got.ExecutionLog[0]
	if e.ErrorCode != domain.ErrorCode("E501") {
		t.Errorf("ErrorCode = %q, want E501", e.ErrorCode)
	}
	if e.Summary != "tool | unavailable retrying" {
		t.Errorf("Summary = %q, want the sanitised text without the marker", e.Summary)
	}
	again, _ := renderAndParse(t, got)
	if again.ExecutionLog[0].Summary != e.Summary || again.ExecutionLog[0].ErrorCode != e.ErrorCode {
		t.Errorf("second round trip changed the row: %+v -> %+v", e, again.ExecutionLog[0])
	}
}

func TestParse_RepeatedNotesSeqAreKeptInFileOrder(t *testing.T) {
	notes := []domain.WorkflowNote{{Seq: 1, Note: "a"}, {Seq: 1, Note: "b"}, {Seq: 2, Note: "c"}, {Seq: 1, Note: "d"}}
	got, _ := renderAndParse(t, domain.ArtifactState{RunID: testRunID, WorkflowNotes: notes})
	if len(got.WorkflowNotes) != len(notes) {
		t.Fatalf("notes = %+v, want %+v", got.WorkflowNotes, notes)
	}
	for i := range notes {
		if got.WorkflowNotes[i] != notes[i] {
			t.Errorf("note[%d] = %+v, want %+v", i, got.WorkflowNotes[i], notes[i])
		}
	}
}

// artifactWithSections builds an artifact whose three section bodies are given verbatim.
func artifactWithSections(execLog, registry, notes string) []byte {
	return []byte(toleranceFrontmatter() + "\n" +
		"<ExecutionLog type=\"core\">\n" + execLog + "</ExecutionLog>\n\n" +
		"<Artifacts type=\"core\">\n" + registry + "</Artifacts>\n\n" +
		"<WorkflowNotes type=\"core\">\n" + notes + "</WorkflowNotes>\n")
}

const (
	validExecLog   = "| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n"
	validRegistry  = "| Artifact | Created In | Created By |\n| --- | --- | --- |\n"
	validNotesHead = "| Seq | Note |\n| --- | --- |\n"
)

func TestParse_CorruptWorkflowNotesAreRefusedNotDropped(t *testing.T) {
	cases := map[string]string{
		"no table":                  "some prose instead of a table\n",
		"separator column mismatch": "| Seq | Note |\n| --- |\n| 1 | a |\n",
		"extra cells in a row":      validNotesHead + "| 1 | left | right |\n",
		"header without Note":       "| Seq | Text |\n| --- | --- |\n| 1 | a |\n",
		"header without Seq":        "| Id | Note |\n| --- | --- |\n| 1 | a |\n",
	}
	for name, notes := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := artifact.Parse(artifactWithSections(validExecLog, validRegistry, notes))
			if err == nil {
				t.Fatal("Parse succeeded, want a refusal (notes must not become an empty list)")
			}
			re := asRefusalError(t, err)
			if !strings.HasPrefix(re.Reason, notesRefusalPrefix) {
				t.Errorf("Reason = %q, want prefix %q", re.Reason, notesRefusalPrefix)
			}
			if re.Cause == nil {
				t.Error("Cause is nil, want the underlying error")
			}
		})
	}
}

func TestParse_AbsentOrBlankWorkflowNotesMeanNoNotes(t *testing.T) {
	blank := artifactWithSections(validExecLog, validRegistry, "\n  \n")
	got, err := artifact.Parse(blank)
	if err != nil || len(got.WorkflowNotes) != 0 {
		t.Errorf("blank notes section: notes=%v err=%v, want none and no error", got.WorkflowNotes, err)
	}
	absent := strings.Replace(string(blank), "<WorkflowNotes type=\"core\">\n\n  \n</WorkflowNotes>\n", "", 1)
	got, err = artifact.Parse([]byte(absent))
	if err != nil || len(got.WorkflowNotes) != 0 {
		t.Errorf("absent notes section: notes=%v err=%v, want none and no error", got.WorkflowNotes, err)
	}
}

func TestParse_ExtraCellsInExecutionLogOrRegistryAreRefusedWithRepairHint(t *testing.T) {
	extraLog := validExecLog +
		"| 1 | a#1 | P | - | SUCCESS | 2026-01-01T00:00:00Z | ok | - | - |\n" +
		"| 2 | a#2 | P | - | SUCCESS | 2026-01-01T00:00:00Z | x | y | - | - |\n"
	extraRegistry := validRegistry + "| A.md | P | a#1 |\n| B.md | P | a#2 | extra |\n"
	cases := []struct {
		name   string
		data   []byte
		prefix string
	}{
		{"execution log", artifactWithSections(extraLog, validRegistry, validNotesHead), logRefusalPrefix},
		{"artifact registry", artifactWithSections(validExecLog, extraRegistry, validNotesHead), registryPrefix},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := artifact.Parse(c.data)
			if err == nil {
				t.Fatal("Parse succeeded, want a refusal instead of silently cutting the extra cell")
			}
			re := asRefusalError(t, err)
			if !strings.HasPrefix(re.Reason, c.prefix) {
				t.Errorf("Reason = %q, want prefix %q", re.Reason, c.prefix)
			}
			var se *mdtable.StructureError
			if !errors.As(err, &se) {
				t.Fatalf("error %v does not wrap *mdtable.StructureError", err)
			}
			if !strings.Contains(re.Reason, "data row 1") || !strings.Contains(re.Reason, "line ") {
				t.Errorf("Reason = %q, want the data-row index and the line number", re.Reason)
			}
		})
	}
}

func TestStore_CorruptNotesRefuseReadAndSetPhaseAndLeaveTheFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Orchestration.md")
	corrupt := artifactWithSections(validExecLog, validRegistry, validNotesHead+"| 1 | left | right |\n")
	if err := os.WriteFile(path, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	store := artifact.NewFileStore(path)
	ctx := context.Background()

	if _, err := store.Read(ctx); err == nil {
		t.Fatal("Read succeeded on corrupt notes, want a refusal")
	}
	if _, err := store.SetPhase(ctx, domain.ArtifactState{}, "COMPLETED", time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("SetPhase succeeded on corrupt notes, want a refusal (it would replace the notes with an empty table)")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(corrupt) {
		t.Errorf("a refused operation changed the file:\n%s", after)
	}
}
