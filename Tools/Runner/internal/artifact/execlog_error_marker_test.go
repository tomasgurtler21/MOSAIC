package artifact_test

// Tests for the BLOCKED error code in the Execution Log: Apply records the code
// as a trailing "[error:CODE]" marker on the Summary cell, truncation never cuts
// the marker, parsing reads it back, and re-rendering is stable.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mosaic-run/internal/artifact"
	"mosaic-run/internal/domain"
)

const errorMarkerE501 = "[error:E501]"

// newMarkerStore creates a store on a temp file and returns it with the file
// path and the initial state.
func newMarkerStore(t *testing.T) (domain.ArtifactStore, string, domain.ArtifactState) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Orchestration.md")
	store := artifact.NewFileStore(path)
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	state, err := store.Create(context.Background(), info, "test task", domain.RunSettings{}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), testRunID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return store, path, state
}

// blockedStep builds a BLOCKED step with the given error code and summary.
func blockedStep(seq int, code domain.ErrorCode, summary string) domain.CompletedStep {
	step := newTestStep(seq, "builder#"+strconv.Itoa(seq), "EXECUTION", "", domain.StatusBLOCKED, time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), nil)
	step.ErrorCode = code
	step.Summary = summary
	return step
}

// applyAndRead applies step to state, then re-reads the state from the store.
func applyAndRead(t *testing.T, store domain.ArtifactStore, state domain.ArtifactState, step domain.CompletedStep) (domain.ArtifactState, domain.ArtifactState) {
	t.Helper()
	ctx := context.Background()
	after, err := store.Apply(ctx, state, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	read, err := store.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return after, read
}

// splitRow splits a markdown table line into trimmed cells.
func splitRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	cells := strings.Split(trimmed, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// executionLogHeader returns the Execution Log header cells of the file.
func executionLogHeader(t *testing.T, raw string) []string {
	t.Helper()
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "| Seq") && strings.Contains(line, "Checkpoint") {
			return splitRow(line)
		}
	}
	t.Fatalf("Execution Log header not found in:\n%s", raw)
	return nil
}

// summaryCell returns the raw Summary cell of the Execution Log row with the given seq.
func summaryCell(t *testing.T, raw string, seq string) string {
	t.Helper()
	header := executionLogHeader(t, raw)
	col := -1
	for i, h := range header {
		if h == "Summary" {
			col = i
		}
	}
	if col < 0 {
		t.Fatalf("no Summary column in header %v", header)
	}
	for _, line := range strings.Split(raw, "\n") {
		cells := splitRow(line)
		if strings.HasPrefix(strings.TrimSpace(line), "|") && len(cells) == len(header) && cells[0] == seq {
			return cells[col]
		}
	}
	t.Fatalf("no Execution Log row with Seq %s in:\n%s", seq, raw)
	return ""
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func TestApply_BlockedE501_RendersMarkerAndParsesBackToCode(t *testing.T) {
	store, path, state := newMarkerStore(t)

	after, read := applyAndRead(t, store, state, blockedStep(1, domain.ErrorTOOL_UNAVAILABLE, "tool unavailable"))

	if got := summaryCell(t, readFile(t, path), "1"); got != "tool unavailable "+errorMarkerE501 {
		t.Errorf("Summary cell: want %q, got %q", "tool unavailable "+errorMarkerE501, got)
	}
	if got := after.ExecutionLog[0].ErrorCode; got != domain.ErrorTOOL_UNAVAILABLE {
		t.Errorf("returned entry ErrorCode: want E501, got %q", got)
	}
	entry := read.ExecutionLog[0]
	if entry.ErrorCode != domain.ErrorTOOL_UNAVAILABLE {
		t.Errorf("parsed entry ErrorCode: want E501, got %q", entry.ErrorCode)
	}
	if entry.Summary != "tool unavailable" {
		t.Errorf("parsed entry Summary must hold the text without the marker, got %q", entry.Summary)
	}
}

func TestApply_HarnessErrorStep_RecordsE501Marker(t *testing.T) {
	store, path, state := newMarkerStore(t)
	resp := domain.HarnessErrorResponse("builder#1", testRunID, os.ErrDeadlineExceeded)
	step := blockedStep(1, resp.ErrorCode, resp.StatusMessage)

	_, read := applyAndRead(t, store, state, step)

	if cell := summaryCell(t, readFile(t, path), "1"); !strings.HasSuffix(cell, errorMarkerE501) {
		t.Errorf("harness error row Summary must end with %s, got %q", errorMarkerE501, cell)
	}
	if got := read.ExecutionLog[0].ErrorCode; got != domain.ErrorTOOL_UNAVAILABLE {
		t.Errorf("parsed ErrorCode: want E501, got %q", got)
	}
}

func TestApply_BlockedWithOtherCode_RecordsThatCode(t *testing.T) {
	store, _, state := newMarkerStore(t)

	_, read := applyAndRead(t, store, state, blockedStep(1, domain.ErrorPERMISSION_DENIED, "cannot write"))

	if got := read.ExecutionLog[0].ErrorCode; got != domain.ErrorPERMISSION_DENIED {
		t.Errorf("parsed ErrorCode: want E502, got %q", got)
	}
}

func TestApply_BlockedWithVeryLongMessage_MarkerSurvivesTruncation(t *testing.T) {
	store, path, state := newMarkerStore(t)
	long := strings.Repeat("0123456789", 40)

	_, read := applyAndRead(t, store, state, blockedStep(1, domain.ErrorTOOL_UNAVAILABLE, long))

	cell := summaryCell(t, readFile(t, path), "1")
	if !strings.HasSuffix(cell, " "+errorMarkerE501) {
		t.Errorf("Summary cell must end with the complete marker, got %q", cell)
	}
	if strings.Count(cell, "[error:") != 1 {
		t.Errorf("Summary cell must hold exactly one marker, got %q", cell)
	}
	if len(cell) > len(artifact.TruncateSummary(long))+1+len(errorMarkerE501) {
		t.Errorf("Summary cell must stay bounded by truncation limit plus marker, got %d bytes", len(cell))
	}
	entry := read.ExecutionLog[0]
	if entry.ErrorCode != domain.ErrorTOOL_UNAVAILABLE {
		t.Errorf("parsed ErrorCode: want E501, got %q", entry.ErrorCode)
	}
	if entry.Summary != artifact.TruncateSummary(long) {
		t.Errorf("parsed Summary must be the truncated message without marker, got %q", entry.Summary)
	}
}

func TestApply_NonBlockedRows_CarryNoMarker(t *testing.T) {
	for _, status := range []domain.StatusCode{
		domain.StatusSUCCESS, domain.StatusPARTIALLY_DONE, domain.StatusCOMPLETED_NEEDS_ACTION,
		domain.StatusNEEDS_CLARIFICATION, domain.StatusCAPABILITY_EXCEEDED,
	} {
		t.Run(string(status), func(t *testing.T) {
			store, path, state := newMarkerStore(t)
			step := blockedStep(1, domain.ErrorTOOL_UNAVAILABLE, "some outcome")
			step.Status = status

			_, read := applyAndRead(t, store, state, step)

			if cell := summaryCell(t, readFile(t, path), "1"); cell != "some outcome" {
				t.Errorf("Summary cell of a %s row: want %q, got %q", status, "some outcome", cell)
			}
			if got := read.ExecutionLog[0].ErrorCode; got != domain.ErrorNone {
				t.Errorf("parsed ErrorCode of a %s row: want none, got %q", status, got)
			}
		})
	}
}

func TestApply_NonBlockedSummaryEndingInMarkerShape_IsKeptVerbatim(t *testing.T) {
	store, _, state := newMarkerStore(t)
	step := blockedStep(1, domain.ErrorNone, "agent wrote "+errorMarkerE501)
	step.Status = domain.StatusSUCCESS

	_, read := applyAndRead(t, store, state, step)

	entry := read.ExecutionLog[0]
	if entry.Summary != "agent wrote "+errorMarkerE501 || entry.ErrorCode != domain.ErrorNone {
		t.Errorf("SUCCESS row: want verbatim summary and no code, got %q / %q", entry.Summary, entry.ErrorCode)
	}
}

func TestApply_RepeatedBlockedSteps_NeverDuplicateOrLoseMarkers(t *testing.T) {
	store, path, state := newMarkerStore(t)
	codes := []domain.ErrorCode{domain.ErrorTOOL_UNAVAILABLE, domain.ErrorPERMISSION_DENIED, domain.ErrorTOOL_UNAVAILABLE}
	var read domain.ArtifactState
	for i, code := range codes {
		_, read = applyAndRead(t, store, state, blockedStep(i+1, code, "attempt failed"))
		state = read
	}

	raw := readFile(t, path)
	if n := strings.Count(raw, "[error:"); n != 3 {
		t.Errorf("file must hold exactly 3 markers after 3 BLOCKED steps, got %d", n)
	}
	for i, code := range codes {
		entry := read.ExecutionLog[i]
		if entry.ErrorCode != code || entry.Summary != "attempt failed" {
			t.Errorf("row %d: want %q / %q, got %q / %q", i+1, code, "attempt failed", entry.ErrorCode, entry.Summary)
		}
	}
}

func TestRenderParse_BlockedRowWithCode_IsAFixedPoint(t *testing.T) {
	store, path, state := newMarkerStore(t)
	applyAndRead(t, store, state, blockedStep(1, domain.ErrorTOOL_UNAVAILABLE, "done [commit:abc]"))
	first := []byte(readFile(t, path))

	parsed, err := artifact.Parse(first)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	second, err := artifact.Render(parsed)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("render(parse(file)) must reproduce the file.\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if got := parsed.ExecutionLog[0].Summary; got != "done [commit:abc]" {
		t.Errorf("parsed Summary: want %q, got %q", "done [commit:abc]", got)
	}
}

// blockedRowArtifact returns artifact bytes whose single row is BLOCKED with the given Summary cell.
func blockedRowArtifact(summaryCellValue string) []byte {
	base := string(minimalArtifactWithExecutionRow(""))
	base = strings.Replace(base, "| SUCCESS |", "| BLOCKED |", 1)
	return []byte(strings.Replace(base, "Plan created", summaryCellValue, 1))
}

func TestParse_BlockedRowWithoutMarker_HasEmptyCodeAndNoError(t *testing.T) {
	state, err := artifact.Parse(blockedRowArtifact("native orchestrator wrote this"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	entry := state.ExecutionLog[0]
	if entry.Status != domain.StatusBLOCKED || entry.ErrorCode != domain.ErrorNone {
		t.Errorf("want BLOCKED with no code, got %s / %q", entry.Status, entry.ErrorCode)
	}
	if entry.Summary != "native orchestrator wrote this" {
		t.Errorf("Summary: got %q", entry.Summary)
	}
	rendered, err := artifact.Render(state)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(string(rendered), "[error:") {
		t.Errorf("re-rendering a marker-less BLOCKED row must not invent a marker:\n%s", rendered)
	}
}

func TestParse_BlockedRowWithMarker_ReadsCodeAndStripsMarkerFromSummary(t *testing.T) {
	state, err := artifact.Parse(blockedRowArtifact("tool down " + errorMarkerE501))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	entry := state.ExecutionLog[0]
	if entry.ErrorCode != domain.ErrorTOOL_UNAVAILABLE || entry.Summary != "tool down" {
		t.Errorf("want E501 / %q, got %q / %q", "tool down", entry.ErrorCode, entry.Summary)
	}
}

func TestParse_BlockedRowWithUnknownCode_PreservesItAsWritten(t *testing.T) {
	state, err := artifact.Parse(blockedRowArtifact("odd [error:X9z]"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got := state.ExecutionLog[0].ErrorCode; got != domain.ErrorCode("X9z") {
		t.Errorf("unknown well-formed code must be preserved, got %q", got)
	}
}

func TestParse_BlockedRowWithMarkerNotAtEnd_IsNotRecognised(t *testing.T) {
	state, err := artifact.Parse(blockedRowArtifact("[error:E501] was mentioned first"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	entry := state.ExecutionLog[0]
	if entry.ErrorCode != domain.ErrorNone || entry.Summary != "[error:E501] was mentioned first" {
		t.Errorf("a marker not at the end must stay in the summary, got %q / %q", entry.ErrorCode, entry.Summary)
	}
}

func TestRender_BlockedRows_KeepExecutionLogColumnSet(t *testing.T) {
	store, path, state := newMarkerStore(t)
	applyAndRead(t, store, state, blockedStep(1, domain.ErrorTOOL_UNAVAILABLE, "tool unavailable"))

	got := executionLogHeader(t, readFile(t, path))

	want := []string{"Seq", "Agent", "Phase", "Stage", "WorkflowRow", "Status", "Timestamp", "Summary", "Inputs", "Checkpoint"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Execution Log columns: want %v, got %v", want, got)
	}
}
