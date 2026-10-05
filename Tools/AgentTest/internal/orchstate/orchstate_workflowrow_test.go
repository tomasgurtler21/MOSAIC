package orchstate_test

// Tests that an Execution Log carrying a WorkflowRow column parses into the
// same entries as the same log without it: columns are bound by header name,
// so the extra column is ignored.

import (
	"reflect"
	"testing"

	"mosaic-agent-test/internal/orchstate"
)

const workflowRowFrontmatter = `---
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

`

const logWithoutWorkflowRow = workflowRowFrontmatter + `<ExecutionLog type="core">
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |
|-----|-------|-------|-------|--------|-----------|---------|--------|------------|
| 1 | researcher#1 | RESEARCH | - | SUCCESS | 2026-01-01T00:10:00Z | Did research. | Requirements.md | - |
| 2 | planner#2 | PLANNING | - | SUCCESS | 2026-01-01T00:30:00Z | Wrote plan. | Requirements.md, Research.md | - |
| 3 | implementer#3 | EXECUTION.Implementation | 2 | SUCCESS | 2026-01-01T01:00:00Z | Implemented stage 2. | Plan.md | - |
</ExecutionLog>
`

const logWithWorkflowRow = workflowRowFrontmatter + `<ExecutionLog type="core">
| Seq | Agent | Phase | Stage | WorkflowRow | Status | Timestamp | Summary | Inputs | Checkpoint |
|-----|-------|-------|-------|-------------|--------|-----------|---------|--------|------------|
| 1 | researcher#1 | RESEARCH | - | 1 | SUCCESS | 2026-01-01T00:10:00Z | Did research. | Requirements.md | - |
| 2 | planner#2 | PLANNING | - | - | SUCCESS | 2026-01-01T00:30:00Z | Wrote plan. | Requirements.md, Research.md | - |
| 3 | implementer#3 | EXECUTION.Implementation | 2 | 3 | SUCCESS | 2026-01-01T01:00:00Z | Implemented stage 2. | Plan.md | - |
</ExecutionLog>
`

func TestParse_WorkflowRowColumn_YieldsSameEntriesAsLogWithoutIt(t *testing.T) {
	// Arrange
	without, err := orchstate.Parse([]byte(logWithoutWorkflowRow))
	if err != nil {
		t.Fatalf("Parse without WorkflowRow: %v", err)
	}

	// Act
	with, err := orchstate.Parse([]byte(logWithWorkflowRow))

	// Assert
	if err != nil {
		t.Fatalf("Parse with WorkflowRow: %v", err)
	}
	if len(with.ExecutionLog) != 3 {
		t.Fatalf("got %d log entries, want 3", len(with.ExecutionLog))
	}
	if !reflect.DeepEqual(with.ExecutionLog, without.ExecutionLog) {
		t.Errorf("entries differ:\nwith:    %+v\nwithout: %+v", with.ExecutionLog, without.ExecutionLog)
	}
}
