package orchstate_test

// Tests for orchstate.Parse / orchstate.ParseFile: extracting the current
// phase, the last recorded status, and the ordered execution-log sequence
// from a MOSAIC orchestration document.
//
// Coverage follows T7.1: a well-formed document, a document with an empty
// execution log, a document mid-workflow, and a malformed or absent
// document handled as a distinguishable condition (AC7.6) rather than as a
// zero value that later reads as "phase not reached".

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mosaic-agent-test/internal/orchstate"
)

const wellFormedDocument = `---
type: orchestration-artifact
run_id: "example-run"
workflow: greenfield-tdd
workflow_version: "3.4"
task: "Example task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T01:00:00Z
global_sequence: 3
checkpoints: disabled
commits: disabled
current_state:
  phase: EXECUTION
  stage: "2"
  last_status: SUCCESS
  last_agent: "implementer#3"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |
|-----|-------|-------|-------|--------|-----------|---------|--------|------------|
| 1 | researcher#1 | RESEARCH | - | SUCCESS | 2026-01-01T00:10:00Z | Did research. | Requirements.md | - |
| 2 | planner#2 | PLANNING | - | SUCCESS | 2026-01-01T00:30:00Z | Wrote plan. | Requirements.md, Research.md | - |
| 3 | implementer#3 | EXECUTION.Implementation | 2 | SUCCESS | 2026-01-01T01:00:00Z | Implemented stage 2. | Plan.md | - |
</ExecutionLog>
`

const emptyExecutionLogDocument = `---
type: orchestration-artifact
run_id: "example-run"
workflow: greenfield-tdd
workflow_version: "3.4"
task: "Example task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 0
checkpoints: disabled
commits: disabled
current_state:
  phase: INIT
  stage: "-"
  last_status: ""
  last_agent: ""
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |
|-----|-------|-------|-------|--------|-----------|---------|--------|------------|
</ExecutionLog>
`

const midWorkflowDocument = `---
type: orchestration-artifact
run_id: "example-run"
workflow: greenfield-tdd
workflow_version: "3.4"
task: "Example task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:10:00Z
global_sequence: 1
checkpoints: disabled
commits: disabled
current_state:
  phase: RESEARCH
  stage: "-"
  last_status: SUCCESS
  last_agent: "researcher#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |
|-----|-------|-------|-------|--------|-----------|---------|--------|------------|
| 1 | researcher#1 | RESEARCH | - | SUCCESS | 2026-01-01T00:10:00Z | Did research. | Requirements.md | - |
</ExecutionLog>
`

const dashInputsDocument = `---
type: orchestration-artifact
run_id: "example-run"
workflow: greenfield-tdd
workflow_version: "3.4"
task: "Example task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:05:00Z
global_sequence: 1
checkpoints: disabled
commits: disabled
current_state:
  phase: RESEARCH
  stage: "-"
  last_status: SUCCESS
  last_agent: "researcher#1"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |
|-----|-------|-------|-------|--------|-----------|---------|--------|------------|
| 1 | researcher#1 | RESEARCH | - | SUCCESS | 2026-01-01T00:05:00Z | Started with no dispatched inputs. | - | - |
</ExecutionLog>
`

const malformedDocument = `---
type: orchestration-artifact
current_state:
  phase: [this is not valid yaml
---

<ExecutionLog type="core">
not a table
</ExecutionLog>
`

// --- Well-formed document ---

func TestParse_WellFormedDocument_ReportsPresent(t *testing.T) {
	state, err := orchstate.Parse([]byte(wellFormedDocument))

	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if !state.Present {
		t.Fatal("Present = false, want true for a well-formed document")
	}
}

func TestParse_WellFormedDocument_ExtractsCurrentPhaseAndLastStatus(t *testing.T) {
	state, err := orchstate.Parse([]byte(wellFormedDocument))

	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if state.Phase != "EXECUTION" {
		t.Errorf("Phase = %q, want %q", state.Phase, "EXECUTION")
	}
	if state.LastStatus != "SUCCESS" {
		t.Errorf("LastStatus = %q, want %q", state.LastStatus, "SUCCESS")
	}
}

func TestParse_WellFormedDocument_ExtractsExecutionLogInOrder(t *testing.T) {
	state, err := orchstate.Parse([]byte(wellFormedDocument))

	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(state.ExecutionLog) != 3 {
		t.Fatalf("len(ExecutionLog) = %d, want 3: got %+v", len(state.ExecutionLog), state.ExecutionLog)
	}
	wantSeqs := []int{1, 2, 3}
	for i, want := range wantSeqs {
		if state.ExecutionLog[i].Seq != want {
			t.Errorf("ExecutionLog[%d].Seq = %d, want %d — rows must stay in document order", i, state.ExecutionLog[i].Seq, want)
		}
	}
}

func TestParse_WellFormedDocument_ExecutionLogRowFieldsMatchTheDocument(t *testing.T) {
	state, err := orchstate.Parse([]byte(wellFormedDocument))

	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(state.ExecutionLog) < 3 {
		t.Fatalf("expected at least 3 execution log rows, got %d", len(state.ExecutionLog))
	}
	last := state.ExecutionLog[2]
	if last.AgentInstance != "implementer#3" {
		t.Errorf("ExecutionLog[2].AgentInstance = %q, want %q", last.AgentInstance, "implementer#3")
	}
	if last.Phase != "EXECUTION.Implementation" {
		t.Errorf("ExecutionLog[2].Phase = %q, want %q", last.Phase, "EXECUTION.Implementation")
	}
	if last.Stage != "2" {
		t.Errorf("ExecutionLog[2].Stage = %q, want %q", last.Stage, "2")
	}
	if last.Status != "SUCCESS" {
		t.Errorf("ExecutionLog[2].Status = %q, want %q", last.Status, "SUCCESS")
	}
	wantTS, parseErr := time.Parse(time.RFC3339, "2026-01-01T01:00:00Z")
	if parseErr != nil {
		t.Fatalf("test fixture timestamp failed to parse: %v", parseErr)
	}
	if !last.Timestamp.Equal(wantTS) {
		t.Errorf("ExecutionLog[2].Timestamp = %v, want %v", last.Timestamp, wantTS)
	}
}

// --- Inputs column maps to InputArtifacts only (ContractsDesign.md:
// domain.ExecutionLogRow has no OutputArtifacts field; the document records
// only dispatched input_artifacts, and Inputs populates InputArtifacts
// after splitting on commas and trimming each element) ---

func TestParse_WellFormedDocument_InputsColumnPopulatesInputArtifactsSplitAndTrimmed(t *testing.T) {
	state, err := orchstate.Parse([]byte(wellFormedDocument))

	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(state.ExecutionLog) < 2 {
		t.Fatalf("expected at least 2 execution log rows, got %d", len(state.ExecutionLog))
	}

	// Row 1: a single input artifact, no comma to split on.
	single := state.ExecutionLog[0]
	wantSingle := []string{"Requirements.md"}
	if len(single.InputArtifacts) != len(wantSingle) || single.InputArtifacts[0] != wantSingle[0] {
		t.Errorf("ExecutionLog[0].InputArtifacts = %+v, want %+v", single.InputArtifacts, wantSingle)
	}

	// Row 2: "Requirements.md, Research.md" — comma-separated, each element
	// trimmed of the leading space the document's formatting introduces.
	multiple := state.ExecutionLog[1]
	wantMultiple := []string{"Requirements.md", "Research.md"}
	if len(multiple.InputArtifacts) != len(wantMultiple) {
		t.Fatalf("ExecutionLog[1].InputArtifacts = %+v, want %+v", multiple.InputArtifacts, wantMultiple)
	}
	for i, want := range wantMultiple {
		if multiple.InputArtifacts[i] != want {
			t.Errorf("ExecutionLog[1].InputArtifacts[%d] = %q, want %q (untrimmed whitespace or unsplit list)", i, multiple.InputArtifacts[i], want)
		}
	}
}

func TestParse_WellFormedDocument_DashInInputsColumnParsesToNilInputArtifacts(t *testing.T) {
	// The document writes "-" for "no value". "-" must parse to the zero
	// value (nil), never be carried through as literal content.
	state, err := orchstate.Parse([]byte(dashInputsDocument))

	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if len(state.ExecutionLog) != 1 {
		t.Fatalf("len(ExecutionLog) = %d, want 1", len(state.ExecutionLog))
	}
	if len(state.ExecutionLog[0].InputArtifacts) != 0 {
		t.Errorf("ExecutionLog[0].InputArtifacts = %+v, want empty for a %q cell", state.ExecutionLog[0].InputArtifacts, "-")
	}
}

// --- Empty execution log ---

func TestParse_EmptyExecutionLog_ReportsPresentWithNoRows(t *testing.T) {
	state, err := orchstate.Parse([]byte(emptyExecutionLogDocument))

	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if !state.Present {
		t.Fatal("Present = false, want true for a well-formed document with an empty log")
	}
	if len(state.ExecutionLog) != 0 {
		t.Errorf("len(ExecutionLog) = %d, want 0 for an empty execution log", len(state.ExecutionLog))
	}
}

// --- Mid-workflow document ---

func TestParse_MidWorkflowDocument_ReportsTheInProgressPhaseNotAZeroValue(t *testing.T) {
	// A run that has not reached its final phase must still report its real
	// current phase, distinguishable from the zero value an unset field
	// would otherwise produce.
	state, err := orchstate.Parse([]byte(midWorkflowDocument))

	if err != nil {
		t.Fatalf("Parse returned unexpected error: %v", err)
	}
	if state.Phase != "RESEARCH" {
		t.Errorf("Phase = %q, want %q", state.Phase, "RESEARCH")
	}
	if state.Phase == "" {
		t.Fatal("Phase is empty — a mid-workflow phase must never read as the zero value")
	}
	if len(state.ExecutionLog) != 1 {
		t.Errorf("len(ExecutionLog) = %d, want 1", len(state.ExecutionLog))
	}
}

// --- Malformed and absent documents (AC7.6) ---

func TestParse_MalformedDocument_ReturnsErrDocumentMalformed(t *testing.T) {
	_, err := orchstate.Parse([]byte(malformedDocument))

	if err == nil {
		t.Fatal("expected an error for a malformed document, got nil")
	}
	if !errors.Is(err, orchstate.ErrDocumentMalformed) {
		t.Errorf("err = %v, want errors.Is(err, orchstate.ErrDocumentMalformed)", err)
	}
	if errors.Is(err, orchstate.ErrDocumentAbsent) {
		t.Error("a malformed document must not also match ErrDocumentAbsent — the two conditions are distinct")
	}
}

func TestParse_MalformedDocument_DoesNotReturnAPresentZeroValue(t *testing.T) {
	state, err := orchstate.Parse([]byte(malformedDocument))

	if err == nil {
		t.Fatal("expected an error for a malformed document, got nil")
	}
	if state.Present {
		t.Error("Present = true on a malformed document, want false — malformed must never read as a valid state")
	}
}

func TestParseFile_AbsentDocument_ReturnsErrDocumentAbsent(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "Orchestration.md")

	_, err := orchstate.ParseFile(missing)

	if err == nil {
		t.Fatal("expected an error for an absent document, got nil")
	}
	if !errors.Is(err, orchstate.ErrDocumentAbsent) {
		t.Errorf("err = %v, want errors.Is(err, orchstate.ErrDocumentAbsent)", err)
	}
	if errors.Is(err, orchstate.ErrDocumentMalformed) {
		t.Error("an absent document must not also match ErrDocumentMalformed — the two conditions are distinct")
	}
}

func TestParseFile_WellFormedDocument_ReadsFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Orchestration.md")
	if err := os.WriteFile(path, []byte(wellFormedDocument), 0o644); err != nil {
		t.Fatalf("failed to write test fixture: %v", err)
	}

	state, err := orchstate.ParseFile(path)

	if err != nil {
		t.Fatalf("ParseFile returned unexpected error: %v", err)
	}
	if state.Phase != "EXECUTION" {
		t.Errorf("Phase = %q, want %q", state.Phase, "EXECUTION")
	}
}
