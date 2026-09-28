package compat_test

// Tests for admission refusals A1-A5 (cross-consistency violations between
// EXECUTION rows' group segments and the approach table) and case-sensitive
// group token matching.

import (
	"strings"
	"testing"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
)

// ---- Admission refusals A1-A5 ----
//
// Each refusal targets a specific cross-consistency violation between the
// EXECUTION rows' group segments and the workflow's approach table.
// Fixtures use generic agent identifiers to prove that classification
// is driven by the Phase column, not by agent names.

// A1: a row re-opens a group that already ended (non-contiguous rows).

func TestAdmit_A1_NonContiguousGroups_ReturnsRefusalError(t *testing.T) {
	// Alpha, Beta, Alpha: row 3 re-opens group Alpha after Beta began.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "non-contiguous", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.Alpha.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Alpha"}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Beta.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Beta"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
			{Index: 3, Phase: "EXECUTION.Alpha.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Alpha"}, Agent: "agent-c", OutputArtifacts: []string{"out.md"}},
		},
		ApproachTable: domain.ApproachTable{Rows: []domain.ApproachSequence{
			{Approach: "Forward", Groups: []domain.GroupName{"Alpha", "Beta"}},
		}},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse non-contiguous group rows (A1)")
	}
	asRefusalError(t, err)
}

func TestAdmit_A1_NonContiguousGroups_RefusalMessage_NamesRowAndGroup(t *testing.T) {
	// The A1 message must name the offending row index and the group token.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "non-contiguous", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.Alpha.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Alpha"}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Beta.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Beta"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
			{Index: 3, Phase: "EXECUTION.Alpha.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Alpha"}, Agent: "agent-c", OutputArtifacts: []string{"out.md"}},
		},
		ApproachTable: domain.ApproachTable{Rows: []domain.ApproachSequence{
			{Approach: "Forward", Groups: []domain.GroupName{"Alpha", "Beta"}},
		}},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "row 3") {
		t.Errorf("A1 refusal must name the offending row index as %q; got Reason: %q", "row 3", re.Reason)
	}
	if !strings.Contains(re.Reason, "Alpha") {
		t.Errorf("A1 refusal must name the offending group %q; got Reason: %q", "Alpha", re.Reason)
	}
}

// A2: group segments in EXECUTION rows but the workflow has no approach table.

func TestAdmit_A2_GroupsWithoutApproachTable_ReturnsRefusalError(t *testing.T) {
	// At least one EXECUTION row carries a group segment but the workflow has no table.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "groups-no-table", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.Test.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Test"}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
		},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse grouped EXECUTION rows when no approach table is present (A2)")
	}
	asRefusalError(t, err)
}

func TestAdmit_A2_GroupsWithoutApproachTable_RefusalMessage_NamesRowAndGroup(t *testing.T) {
	// The A2 message must name the first offending row (index 1) and its group token.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "groups-no-table", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.Test.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Test"}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
		},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "row 1") {
		t.Errorf("A2 refusal must name the offending row index as %q; got Reason: %q", "row 1", re.Reason)
	}
	if !strings.Contains(re.Reason, "Test") {
		t.Errorf("A2 refusal must name the group %q; got Reason: %q", "Test", re.Reason)
	}
}

// A3: approach table present but an EXECUTION row carries no group segment.

func TestAdmit_A3_ApproachTableWithBareRow_ReturnsRefusalError(t *testing.T) {
	// The workflow has an approach table, but row 1 is a bare staged row (no group segment).
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "table-bare-row", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: ""}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
		},
		ApproachTable: domain.ApproachTable{Rows: []domain.ApproachSequence{
			{Approach: "Forward", Groups: []domain.GroupName{"Test", "Implementation"}},
		}},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse a bare EXECUTION row when an approach table is present (A3)")
	}
	asRefusalError(t, err)
}

func TestAdmit_A3_ApproachTableWithBareRow_RefusalMessage_NamesRow(t *testing.T) {
	// The A3 message must name the offending bare row (index 1).
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "table-bare-row", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: ""}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
		},
		ApproachTable: domain.ApproachTable{Rows: []domain.ApproachSequence{
			{Approach: "Forward", Groups: []domain.GroupName{"Test", "Implementation"}},
		}},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "row 1") {
		t.Errorf("A3 refusal must name the offending bare row index as %q; got Reason: %q", "row 1", re.Reason)
	}
}

// A4: approach table names a group that no EXECUTION row belongs to.

func TestAdmit_A4_ApproachTableGroupNotInExecutionRows_ReturnsRefusalError(t *testing.T) {
	// The approach table lists "Ghost" which no EXECUTION row declares.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "ghost-group", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.Test.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Test"}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
		},
		ApproachTable: domain.ApproachTable{Rows: []domain.ApproachSequence{
			{Approach: "Forward", Groups: []domain.GroupName{"Test", "Implementation", "Ghost"}},
		}},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse when the approach table names a group absent from all EXECUTION rows (A4)")
	}
	asRefusalError(t, err)
}

func TestAdmit_A4_ApproachTableGroupNotInExecutionRows_RefusalMessage_NamesGroup(t *testing.T) {
	// The A4 message must name the phantom group "Ghost".
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "ghost-group", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.Test.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Test"}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
		},
		ApproachTable: domain.ApproachTable{Rows: []domain.ApproachSequence{
			{Approach: "Forward", Groups: []domain.GroupName{"Test", "Implementation", "Ghost"}},
		}},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "Ghost") {
		t.Errorf("A4 refusal must name the phantom group %q; got Reason: %q", "Ghost", re.Reason)
	}
}

// A5: an EXECUTION row declares a group absent from every approach table row.

func TestAdmit_A5_ExecutionGroupNotInApproachTable_ReturnsRefusalError(t *testing.T) {
	// Row 3 declares group "Mystery" which appears in no approach table row.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "mystery-group", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.Test.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Test"}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
			{Index: 3, Phase: "EXECUTION.Mystery.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Mystery"}, Agent: "agent-c", OutputArtifacts: []string{"out.md"}},
		},
		ApproachTable: domain.ApproachTable{Rows: []domain.ApproachSequence{
			{Approach: "Forward", Groups: []domain.GroupName{"Test", "Implementation"}},
		}},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse when an EXECUTION row's group is absent from every approach table row (A5)")
	}
	asRefusalError(t, err)
}

func TestAdmit_A5_ExecutionGroupNotInApproachTable_RefusalMessage_NamesRowAndGroup(t *testing.T) {
	// The A5 message must name the offending row (index 3) and group "Mystery".
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "mystery-group", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.Test.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Test"}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
			{Index: 3, Phase: "EXECUTION.Mystery.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Mystery"}, Agent: "agent-c", OutputArtifacts: []string{"out.md"}},
		},
		ApproachTable: domain.ApproachTable{Rows: []domain.ApproachSequence{
			{Approach: "Forward", Groups: []domain.GroupName{"Test", "Implementation"}},
		}},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "row 3") {
		t.Errorf("A5 refusal must name the offending row index as %q; got Reason: %q", "row 3", re.Reason)
	}
	if !strings.Contains(re.Reason, "Mystery") {
		t.Errorf("A5 refusal must name the offending group %q; got Reason: %q", "Mystery", re.Reason)
	}
}

// ---- Case-sensitive group token matching ----

func TestAdmit_GroupTokenMatching_IsCaseSensitive_ReturnsRefusalError(t *testing.T) {
	// The Phase column carries group token "test" (all lowercase). The approach table
	// declares "Test" (capital T). These are distinct tokens; Admit must refuse.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "case-mismatch", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.test.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "test"}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
		},
		ApproachTable: domain.ApproachTable{Rows: []domain.ApproachSequence{
			{Approach: "Forward", Groups: []domain.GroupName{"Test", "Implementation"}},
		}},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal(`Admit must refuse: group token "test" in Phase column must not match "Test" in the approach table (comparison is case-sensitive)`)
	}
	asRefusalError(t, err)
}

func TestAdmit_GroupTokenMatching_IsCaseSensitive_RefusalMessage_NamesOffender(t *testing.T) {
	// The refusal message must identify the mismatched token so the author can
	// correct the capitalisation in the workflow file.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "case-mismatch", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.test.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "test"}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
		},
		ApproachTable: domain.ApproachTable{Rows: []domain.ApproachSequence{
			{Approach: "Forward", Groups: []domain.GroupName{"Test", "Implementation"}},
		}},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must not be empty for case-mismatch refusal")
	}
	if !strings.Contains(re.Reason, "test") {
		t.Errorf("refusal message must name the offending group token %q; got Reason: %q", "test", re.Reason)
	}
}
