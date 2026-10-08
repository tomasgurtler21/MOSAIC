package orchstate_test

// Regression tests that an Orchestration.md in the native layout parses into
// the rows and fields the verdict engine consumes: blank lines around the
// region tags, unpadded tables, and Inputs / registry paths without the
// Orchestration-{run_id}/ prefix.

import (
	"reflect"
	"testing"
	"time"

	"mosaic-agent-test/internal/orchstate"
)

const nativeLayoutDocument = `---
type: orchestration-artifact
run_id: "20261007T174011Z-4f54"
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
  last_status: BLOCKED
  last_agent: "implementer#3"
  error_code: E501
---

<ExecutionLog type="core">

| Seq | Agent | Phase | Stage | WorkflowRow | Status | Timestamp | Summary | Inputs | Checkpoint |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | researcher#1 | RESEARCH | - | 1 | SUCCESS | 2026-01-01T00:10:00Z | Did research. | Requirements.md | - |
| 2 | planner#2 | PLANNING | - | 2 | SUCCESS | 2026-01-01T00:30:00Z | Wrote a \| b plan. | Requirements.md, Research.md | - |
| 3 | implementer#3 | EXECUTION.Implementation | 2 | 3 | BLOCKED | 2026-01-01T01:00:00Z | Tool down. [error:E501] | Stage-2/Plan.md, src/main.go | - |

</ExecutionLog>

<Artifacts type="core">

| Artifact | Created In | Created By |
| --- | --- | --- |
| Requirements.md | RESEARCH | researcher#1 |
| Stage-2/Plan.md | PLANNING | planner#2 |

</Artifacts>

<WorkflowNotes type="core">

| Seq | Note |
| --- | --- |

</WorkflowNotes>
`

func TestParse_NativeLayout_ExtractsFrontmatterState(t *testing.T) {
	got, err := orchstate.Parse([]byte(nativeLayoutDocument))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if !got.Present || got.Phase != "EXECUTION" || got.LastStatus != "BLOCKED" || got.LastErrorCode != "E501" {
		t.Errorf("state: want present EXECUTION/BLOCKED/E501, got %+v", got)
	}
}

func TestParse_NativeLayout_ExtractsRowsAndUnprefixedInputs(t *testing.T) {
	got, err := orchstate.Parse([]byte(nativeLayoutDocument))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(got.ExecutionLog) != 3 {
		t.Fatalf("rows: want 3, got %d: %+v", len(got.ExecutionLog), got.ExecutionLog)
	}
	third := got.ExecutionLog[2]
	if third.Seq != 3 || third.AgentInstance != "implementer#3" || third.Phase != "EXECUTION.Implementation" ||
		third.Stage != "2" || third.Status != "BLOCKED" {
		t.Errorf("row 3 fields: got %+v", third)
	}
	if want := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC); !third.Timestamp.Equal(want) {
		t.Errorf("row 3 timestamp: want %v, got %v", want, third.Timestamp)
	}
	if want := []string{"Stage-2/Plan.md", "src/main.go"}; !reflect.DeepEqual(third.InputArtifacts, want) {
		t.Errorf("row 3 inputs: want %v, got %v", want, third.InputArtifacts)
	}
	if want := []string{"Requirements.md", "Research.md"}; !reflect.DeepEqual(got.ExecutionLog[1].InputArtifacts, want) {
		t.Errorf("row 2 inputs: want %v, got %v", want, got.ExecutionLog[1].InputArtifacts)
	}
}

func TestParse_NativeLayout_DashCellsAreEmpty(t *testing.T) {
	got, err := orchstate.Parse([]byte(nativeLayoutDocument))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	first := got.ExecutionLog[0]
	if first.Stage != "" || first.Checkpoint != "" {
		t.Errorf("dash cells must read as empty, got stage %q checkpoint %q", first.Stage, first.Checkpoint)
	}
}

func TestParse_NativeLayout_EscapedPipeInSummaryIsReturnedRaw(t *testing.T) {
	got, err := orchstate.Parse([]byte(nativeLayoutDocument))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(got.ExecutionLog) != 3 {
		t.Fatalf("rows: want 3, got %d", len(got.ExecutionLog))
	}
	if want := `Wrote a \| b plan.`; got.ExecutionLog[1].Summary != want {
		t.Errorf("summary: want %q (raw, backslash kept), got %q", want, got.ExecutionLog[1].Summary)
	}
}
