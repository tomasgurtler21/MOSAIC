package engine_test

// Boundary helper behavior for forked tables admitted in orchestrated mode.
//
// A fork joined inside its stage/phase keeps table-position boundary
// semantics: the last row of a stage or phase is the last row by table
// position, and fork branch rows before the join are never boundaries. Fork
// branches that would end a stage/phase are refused at admission (covered in
// the compat tests), so the helpers never have to answer for them.

import (
	"testing"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/engine"
	"mosaic-run/internal/workflow"
)

// Rows: 0 planner, 1 t-a (forks to t-b and t-c), 2 t-b, 3 t-c,
// 4 t-d (join, last of the Test group), 5 i-a (Implementation group).
const forkJoinStagedContent = `## Fork Join Staged Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | FALSE | - | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | t-a | FALSE | t-b, t-c | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/ta.md |
| EXECUTION.Test.[StageNumber] | t-b | FALSE | t-d | - | Stage-{StageNumber}/ta.md | Stage-{StageNumber}/tb.md |
| EXECUTION.Test.[StageNumber] | t-c | FALSE | t-d | - | Stage-{StageNumber}/ta.md | Stage-{StageNumber}/tc.md |
| EXECUTION.Test.[StageNumber] | t-d | FALSE | - | - | Stage-{StageNumber}/tb.md | Stage-{StageNumber}/td.md |
| EXECUTION.Implementation.[StageNumber] | i-a | FALSE | - | - | Stage-{StageNumber}/td.md | Stage-{StageNumber}/ia.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Tests-Only | Test |
`

// Rows: 0 agent-a (forks to agent-b and agent-c), 1 agent-b, 2 agent-c,
// 3 agent-d (join, last row of the phase).
const forkJoinPhaseBoundaryContent = `## Fork Join Phase Boundary Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b, agent-c | - | - | a.md |
| PLANNING | agent-b | FALSE | agent-d | - | a.md | b.md |
| PLANNING | agent-c | FALSE | agent-d | - | a.md | c.md |
| PLANNING | agent-d | FALSE | COMPLETE | - | b.md, c.md | d.md |
`

func admitOrchestrated(t *testing.T, content, id string) domain.AdmittedWorkflow {
	t.Helper()
	info := domain.WorkflowInfo{ID: domain.WorkflowID(id), Version: "1.0"}
	table, err := workflow.Parse([]byte(content), info)
	if err != nil {
		t.Fatalf("workflow.Parse(%s): %v", id, err)
	}
	aw, err := compat.Admit(table, domain.ExecutionModeOrchestrated)
	if err != nil {
		t.Fatalf("compat.Admit(%s, orchestrated): %v", id, err)
	}
	return aw
}

func TestIsLastRowOfStage_ForkJoinedInGroup_TestsOnly_OnlyJoinRowIsLast(t *testing.T) {
	aw := admitOrchestrated(t, forkJoinStagedContent, "fork-join-staged")
	stages := singleStageSet("Tests-Only")

	want := map[int]bool{1: false, 2: false, 3: false, 4: true}

	for row, wantLast := range want {
		if got := engine.IsLastRowOfStage(aw, stages, row, 1); got != wantLast {
			t.Errorf("IsLastRowOfStage(row %d): want %v, got %v", row, wantLast, got)
		}
	}
}

func TestIsLastRowOfStage_ForkJoinedInGroup_TDD_JoinRowIsNotLastWhenAnotherGroupFollows(t *testing.T) {
	aw := admitOrchestrated(t, forkJoinStagedContent, "fork-join-staged")
	stages := singleStageSet("TDD")

	want := map[int]bool{1: false, 2: false, 3: false, 4: false, 5: true}

	for row, wantLast := range want {
		if got := engine.IsLastRowOfStage(aw, stages, row, 1); got != wantLast {
			t.Errorf("IsLastRowOfStage(row %d): want %v, got %v", row, wantLast, got)
		}
	}
}

func TestIsLastRowOfPhase_ForkJoinedInExecution_OnlyJoinRowOfLastStageIsLast(t *testing.T) {
	aw := admitOrchestrated(t, forkJoinStagedContent, "fork-join-staged")
	stages := twoStageSet("Tests-Only")

	// Stage 1 never ends the EXECUTION phase, even at the join row.
	for row := 1; row <= 4; row++ {
		if engine.IsLastRowOfPhase(aw, stages, row, 1) {
			t.Errorf("IsLastRowOfPhase(row %d, stage 1): want false, got true", row)
		}
	}
	// Stage 2: only the join row (last of the last group) ends the phase.
	want := map[int]bool{1: false, 2: false, 3: false, 4: true}
	for row, wantLast := range want {
		if got := engine.IsLastRowOfPhase(aw, stages, row, 2); got != wantLast {
			t.Errorf("IsLastRowOfPhase(row %d, stage 2): want %v, got %v", row, wantLast, got)
		}
	}
}

func TestIsLastRowOfPhase_ForkJoinedInNonStagedPhase_OnlyJoinRowIsLast(t *testing.T) {
	aw := admitOrchestrated(t, forkJoinPhaseBoundaryContent, "fork-join-phase-boundary")

	want := map[int]bool{0: false, 1: false, 2: false, 3: true}

	for row, wantLast := range want {
		if got := engine.IsLastRowOfPhase(aw, nil, row, 0); got != wantLast {
			t.Errorf("IsLastRowOfPhase(row %d): want %v, got %v", row, wantLast, got)
		}
	}
}
