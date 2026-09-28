package compat_test

// Tests for mode-dependent admission.
//
//   - Request-construction checks (agent-mode notation, staged phase name,
//     single staged block, dynamic stage set, execution-group resolution)
//     refuse in every run mode.
//   - The engine-routing shape check (comma-separated multi-target On Success)
//     refuses only in the modes where the engine routes (auto, auto-review, and
//     the unset sentinel, which behaves as the strictest set).
//   - In orchestrated mode a fork/join table is admitted. A fork branch row that
//     ends its execution group or its phase without a join is refused with a
//     message naming the row and the agent.

import (
	"strings"
	"testing"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
)

// allModes lists every run mode Admit can be called with, including the
// unset sentinel.
var allModes = []domain.ExecutionMode{
	domain.ExecutionModeOrchestrated,
	domain.ExecutionModeAuto,
	domain.ExecutionModeAutoReview,
	domain.ExecutionModeUnset,
}

// engineRoutedModes are the modes in which the engine routes On Success itself.
var engineRoutedModes = []domain.ExecutionMode{
	domain.ExecutionModeAuto,
	domain.ExecutionModeAutoReview,
	domain.ExecutionModeUnset,
}

func modeName(m domain.ExecutionMode) string {
	if m == domain.ExecutionModeUnset {
		return "unset"
	}
	return string(m)
}

// forkJoinPhaseContent: a fork inside one non-staged phase, joined before the
// phase ends. Row 0 forks to rows 1 and 2, both join at row 3.
const forkJoinPhaseContent = `## Fork Join Phase Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b, agent-c | - | - | a.md |
| PLANNING | agent-b | FALSE | agent-d | - | a.md | b.md |
| PLANNING | agent-c | FALSE | agent-d | - | a.md | c.md |
| PLANNING | agent-d | FALSE | COMPLETE | - | b.md, c.md | d.md |
`

// forkBranchEndsPhaseContent: agent-c is a fork branch and the last row of
// its phase; there is no join after it.
const forkBranchEndsPhaseContent = `## Fork Branch Ends Phase Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b, agent-c | - | - | a.md |
| PLANNING | agent-b | FALSE | COMPLETE | - | a.md | b.md |
| PLANNING | agent-c | FALSE | COMPLETE | - | a.md | c.md |
`

// forkJoinGroupContent: a fork inside the Test execution group, joined at
// row 4 (the last row of the group). Rows: 0 planner, 1 t-a (forks to t-b and
// t-c), 2 t-b, 3 t-c, 4 t-d (join), 5 i-a (Implementation group).
const forkJoinGroupContent = `## Fork Join Group Workflow

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
`

// forkBranchEndsGroupContent: t-c is a fork branch and the last row of the
// Test group, with an Implementation group following.
const forkBranchEndsGroupContent = `## Fork Branch Ends Group Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | FALSE | - | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | t-a | FALSE | t-b, t-c | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/ta.md |
| EXECUTION.Test.[StageNumber] | t-b | FALSE | - | - | Stage-{StageNumber}/ta.md | Stage-{StageNumber}/tb.md |
| EXECUTION.Test.[StageNumber] | t-c | FALSE | - | - | Stage-{StageNumber}/ta.md | Stage-{StageNumber}/tc.md |
| EXECUTION.Implementation.[StageNumber] | i-a | FALSE | - | - | Stage-{StageNumber}/tb.md | Stage-{StageNumber}/ia.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
`

// ---- Request-construction checks refuse in every mode ----

func TestAdmit_RequestConstructionChecks_RefuseInEveryMode(t *testing.T) {
	plannerRow := domain.RoutingRow{
		Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
		OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"},
	}
	staged := func(idx int, group, agent string, outs ...string) domain.RoutingRow {
		return domain.RoutingRow{
			Index: idx, Phase: "EXECUTION." + group + ".[StageNumber]",
			PhaseParsed:     domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: domain.GroupName(group)},
			Agent:           agent,
			OutputArtifacts: outs,
		}
	}

	cases := []struct {
		name  string
		table domain.RoutingTable
	}{
		{
			name: "agent-with-mode-notation",
			table: domain.RoutingTable{
				Info: domain.WorkflowInfo{ID: "mode-notation", Version: "1.0"},
				Rows: []domain.RoutingRow{
					{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner(review)"},
				},
			},
		},
		{
			name: "non-execution-staged-phase",
			table: domain.RoutingTable{
				Info: domain.WorkflowInfo{ID: "staged-planning", Version: "1.0"},
				Rows: []domain.RoutingRow{
					{Index: 0, Phase: "PLANNING.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: true}, Agent: "planner"},
				},
			},
		},
		{
			name: "multiple-staged-blocks",
			table: domain.RoutingTable{
				Info: domain.WorkflowInfo{ID: "split-execution", Version: "1.0"},
				Rows: []domain.RoutingRow{
					{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner"},
					{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementer", OutputArtifacts: []string{"out.md"}},
					{Index: 2, Phase: "REVIEW", PhaseParsed: domain.PhaseParsed{Name: "REVIEW"}, Agent: "reviewer"},
					{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementer2", OutputArtifacts: []string{"out2.md"}},
				},
			},
		},
		{
			name: "dynamic-stage-set",
			table: domain.RoutingTable{
				Info: domain.WorkflowInfo{ID: "dynamic-stages", Version: "1.0"},
				Rows: []domain.RoutingRow{
					plannerRow,
					{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd",
						OutputArtifacts: []string{"Stage-{StageNumber}/PlanProgress.md", "Stage-*/Plan.md"}},
				},
			},
		},
		{
			name: "execution-groups-non-contiguous",
			table: domain.RoutingTable{
				Info: domain.WorkflowInfo{ID: "non-contiguous", Version: "1.0"},
				Rows: []domain.RoutingRow{
					plannerRow,
					staged(1, "Alpha", "agent-a", "out.md"),
					staged(2, "Beta", "agent-b", "out.md"),
					staged(3, "Alpha", "agent-c", "out.md"),
				},
				ApproachTable: domain.ApproachTable{Rows: []domain.ApproachSequence{
					{Approach: "Forward", Groups: []domain.GroupName{"Alpha", "Beta"}},
				}},
			},
		},
	}

	for _, tc := range cases {
		for _, mode := range allModes {
			t.Run(tc.name+"/"+modeName(mode), func(t *testing.T) {
				_, err := compat.Admit(tc.table, mode)

				if err == nil {
					t.Fatalf("Admit must refuse %s in mode %q", tc.name, modeName(mode))
				}
				asRefusalError(t, err)
			})
		}
	}
}

// ---- Engine-routing shape check is mode-dependent ----

func TestAdmit_MultiTargetOnSuccess_RefusedWhenEngineRoutes(t *testing.T) {
	table := mustParseTable(t, forkJoinPhaseContent, "fork-join-phase", "1.0")

	for _, mode := range engineRoutedModes {
		t.Run(modeName(mode), func(t *testing.T) {
			_, err := compat.Admit(table, mode)

			if err == nil {
				t.Fatalf("Admit must refuse a comma-separated On Success in mode %q", modeName(mode))
			}
			re := asRefusalError(t, err)
			if !strings.Contains(re.Reason, "parallel") {
				t.Errorf("refusal must keep the parallel-dispatch message; got %q", re.Reason)
			}
		})
	}
}

func TestAdmit_MultiTargetOnSuccess_AdmittedInOrchestratedMode(t *testing.T) {
	table := mustParseTable(t, forkJoinPhaseContent, "fork-join-phase", "1.0")

	aw, err := compat.Admit(table, domain.ExecutionModeOrchestrated)

	if err != nil {
		t.Fatalf("orchestrated mode must admit a fork/join table: %v", err)
	}
	if len(aw.Table.Rows) != 4 {
		t.Errorf("admitted table must carry all 4 rows, got %d", len(aw.Table.Rows))
	}
}

func TestAdmit_StagedForkJoinInsideGroup_AdmittedInOrchestratedMode(t *testing.T) {
	// The fork is joined at the last row of its group, so no fork branch ends
	// the group. Group resolution is unchanged.
	table := mustParseTable(t, forkJoinGroupContent, "fork-join-group", "1.0")

	aw, err := compat.Admit(table, domain.ExecutionModeOrchestrated)

	if err != nil {
		t.Fatalf("orchestrated mode must admit a fork joined inside its group: %v", err)
	}
	g, ok := aw.GroupByName("Test")
	if !ok {
		t.Fatal(`GroupByName("Test"): want found=true`)
	}
	if g.StartRow != 1 || g.EndRow != 5 {
		t.Errorf("Test group rows: want [1,5), got [%d,%d)", g.StartRow, g.EndRow)
	}
}

func TestAdmit_StagedForkJoinInsideGroup_RefusedWhenEngineRoutes(t *testing.T) {
	table := mustParseTable(t, forkJoinGroupContent, "fork-join-group", "1.0")

	for _, mode := range engineRoutedModes {
		t.Run(modeName(mode), func(t *testing.T) {
			_, err := compat.Admit(table, mode)

			if err == nil {
				t.Fatalf("Admit must refuse a staged fork/join table in mode %q", modeName(mode))
			}
			asRefusalError(t, err)
		})
	}
}

// ---- Forked-table boundary shapes refused in orchestrated mode ----

func TestAdmit_OrchestratedMode_ForkBranchEndsPhase_Refused(t *testing.T) {
	table := mustParseTable(t, forkBranchEndsPhaseContent, "fork-ends-phase", "1.0")

	_, err := compat.Admit(table, domain.ExecutionModeOrchestrated)

	if err == nil {
		t.Fatal("Admit must refuse a fork branch that is the last row of its phase")
	}
	re := asRefusalError(t, err)
	for _, want := range []string{"agent-c", "row 2", "fork branch"} {
		if !strings.Contains(re.Reason, want) {
			t.Errorf("refusal must contain %q; got %q", want, re.Reason)
		}
	}
	if !strings.Contains(re.Resource, "fork-ends-phase") {
		t.Errorf("refusal Resource must name the workflow; got %q", re.Resource)
	}
}

func TestAdmit_OrchestratedMode_ForkBranchEndsGroup_Refused(t *testing.T) {
	table := mustParseTable(t, forkBranchEndsGroupContent, "fork-ends-group", "1.0")

	_, err := compat.Admit(table, domain.ExecutionModeOrchestrated)

	if err == nil {
		t.Fatal("Admit must refuse a fork branch that is the last row of its execution group")
	}
	re := asRefusalError(t, err)
	for _, want := range []string{"t-c", "row 3", "fork branch"} {
		if !strings.Contains(re.Reason, want) {
			t.Errorf("refusal must contain %q; got %q", want, re.Reason)
		}
	}
}

func TestAdmit_ForkBranchEndsGroup_NotRefusedForBoundaryShapeInEngineRoutedModes(t *testing.T) {
	// In engine-routed modes the refusal is the parallel-dispatch one, not the
	// fork-boundary one: the boundary shape check belongs to orchestrated mode.
	table := mustParseTable(t, forkBranchEndsGroupContent, "fork-ends-group", "1.0")

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	re := asRefusalError(t, err)
	if strings.Contains(re.Reason, "fork branch") {
		t.Errorf("engine-routed refusal must be the parallel-dispatch message, got %q", re.Reason)
	}
}
