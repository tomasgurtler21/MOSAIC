package compat_test

// Tests for pre/post-execution row ranges, three-or-more groups, generic
// agent group notation, the no-groups-declared case, and bare-row handling.

import (
	"testing"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
)

// ---- Pre/Post execution row ranges ----

func TestAdmit_BrownfieldTDDBuildVerified_PreExecutionEndRow(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if aw.PreExecutionEndRow != 7 {
		t.Errorf("PreExecutionEndRow: want 7, got %d", aw.PreExecutionEndRow)
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_NoPostExecution(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if aw.PostExecutionStartRow != aw.PostExecutionEndRow {
		t.Errorf("PostExecution: want empty range (start==end), got start=%d end=%d",
			aw.PostExecutionStartRow, aw.PostExecutionEndRow)
	}
}

func TestAdmit_QuickFix_PostExecutionStartRow(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if aw.PostExecutionStartRow != 3 {
		t.Errorf("PostExecutionStartRow: want 3, got %d", aw.PostExecutionStartRow)
	}
}

func TestAdmit_QuickFix_PostExecutionEndRow(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if aw.PostExecutionEndRow != 4 {
		t.Errorf("PostExecutionEndRow: want 4, got %d", aw.PostExecutionEndRow)
	}
}

func TestAdmit_ImplementationOnly_PreExecutionEmpty(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if aw.PreExecutionStartRow != 0 || aw.PreExecutionEndRow != 0 {
		t.Errorf("PreExecution: want empty range (0,0), got (%d,%d)",
			aw.PreExecutionStartRow, aw.PreExecutionEndRow)
	}
}

func TestAdmit_ImplementationOnly_PostExecutionStartRow(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if aw.PostExecutionStartRow != 2 {
		t.Errorf("PostExecutionStartRow: want 2, got %d", aw.PostExecutionStartRow)
	}
}

func TestAdmit_BrownfieldTDD_PreExecutionEndRow(t *testing.T) {
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if aw.PreExecutionEndRow != 7 {
		t.Errorf("PreExecutionEndRow: want 7, got %d", aw.PreExecutionEndRow)
	}
}

func TestAdmit_BrownfieldTDD_PostExecutionStartRow(t *testing.T) {
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if aw.PostExecutionStartRow != 11 {
		t.Errorf("PostExecutionStartRow: want 11, got %d", aw.PostExecutionStartRow)
	}
}

func TestAdmit_BrownfieldTDD_PostExecutionEndRow(t *testing.T) {
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if aw.PostExecutionEndRow != 12 {
		t.Errorf("PostExecutionEndRow: want 12, got %d", aw.PostExecutionEndRow)
	}
}

func TestAdmit_GreenFieldTDD_PreExecutionEndRow(t *testing.T) {
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if aw.PreExecutionEndRow != 8 {
		t.Errorf("PreExecutionEndRow: want 8, got %d", aw.PreExecutionEndRow)
	}
}

func TestAdmit_GreenFieldTDD_PostExecutionStartRow(t *testing.T) {
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if aw.PostExecutionStartRow != 12 {
		t.Errorf("PostExecutionStartRow: want 12, got %d", aw.PostExecutionStartRow)
	}
}

func TestAdmit_GreenFieldTDD_PostExecutionEndRow(t *testing.T) {
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if aw.PostExecutionEndRow != 13 {
		t.Errorf("PostExecutionEndRow: want 13, got %d", aw.PostExecutionEndRow)
	}
}

// ---- Three or more groups ----

func TestAdmit_ThreeGroups_Succeeds(t *testing.T) {
	table := mustParseTable(t, threeGroupsContent, "three-groups", "1.0")
	_, err := compat.Admit(table, domain.ExecutionModeAuto)
	if err != nil {
		t.Errorf("Admit(three-groups): unexpected error: %v", err)
	}
}

func TestAdmit_ThreeGroups_GroupCount(t *testing.T) {
	table := mustParseTable(t, threeGroupsContent, "three-groups", "1.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) != 3 {
		t.Errorf("Groups: want 3 groups, got %d", len(aw.Groups))
	}
}

func TestAdmit_ThreeGroups_GroupNames(t *testing.T) {
	table := mustParseTable(t, threeGroupsContent, "three-groups", "1.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) != 3 {
		t.Fatalf("Groups: want 3, got %d", len(aw.Groups))
	}
	want := []domain.GroupName{"Alpha", "Beta", "Gamma"}
	for i, w := range want {
		if aw.Groups[i].Name != w {
			t.Errorf("Groups[%d].Name: want %q, got %q", i, w, aw.Groups[i].Name)
		}
	}
}

func TestAdmit_ThreeGroups_GroupsDeclared(t *testing.T) {
	table := mustParseTable(t, threeGroupsContent, "three-groups", "1.0")
	aw := mustAdmit(t, table)

	if !aw.GroupsDeclared {
		t.Error("GroupsDeclared: want true for three-group workflow")
	}
}

func TestAdmit_ThreeGroups_ContiguousRanges(t *testing.T) {
	table := mustParseTable(t, threeGroupsContent, "three-groups", "1.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) != 3 {
		t.Fatalf("Groups: want 3, got %d", len(aw.Groups))
	}
	cases := []struct {
		name     domain.GroupName
		startRow int
		endRow   int
	}{
		{"Alpha", 1, 2},
		{"Beta", 2, 3},
		{"Gamma", 3, 4},
	}
	for i, c := range cases {
		g := aw.Groups[i]
		if g.StartRow != c.startRow || g.EndRow != c.endRow {
			t.Errorf("Groups[%d] (%q): want [%d,%d), got [%d,%d)",
				i, c.name, c.startRow, c.endRow, g.StartRow, g.EndRow)
		}
	}
}

// ---- Arbitrary group names / generic agent identifiers ----

func TestAdmit_GenericAgents_WithGroupNotation_Succeeds(t *testing.T) {
	table := mustParseTable(t, genericAgentGroupsContent, "generic-agent-groups", "1.0")
	_, err := compat.Admit(table, domain.ExecutionModeAuto)
	if err != nil {
		t.Errorf("Admit: generic agents with group notation must be admitted: %v", err)
	}
}

func TestAdmit_GenericAgents_WithGroupNotation_CorrectGroupCount(t *testing.T) {
	table := mustParseTable(t, genericAgentGroupsContent, "generic-agent-groups", "1.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) != 2 {
		t.Errorf("Groups: want 2, got %d", len(aw.Groups))
	}
}

func TestAdmit_GenericAgents_WithGroupNotation_GroupNames(t *testing.T) {
	table := mustParseTable(t, genericAgentGroupsContent, "generic-agent-groups", "1.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 2 {
		t.Fatal("Groups: want at least 2")
	}
	if aw.Groups[0].Name != "Analysis" {
		t.Errorf("Groups[0].Name: want %q, got %q", "Analysis", aw.Groups[0].Name)
	}
	if aw.Groups[1].Name != "Synthesis" {
		t.Errorf("Groups[1].Name: want %q, got %q", "Synthesis", aw.Groups[1].Name)
	}
}

// ---- No-groups-declared case ----

func TestAdmit_QuickFix_GroupsDeclaredFalse(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if aw.GroupsDeclared {
		t.Error("GroupsDeclared: want false for a workflow with no group segments")
	}
}

func TestAdmit_QuickFix_SingleImplicitGroupEmptyName(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want 1 group for bare workflow")
	}
	if aw.Groups[0].Name != "" {
		t.Errorf("Groups[0].Name: want empty string for implicit group, got %q", aw.Groups[0].Name)
	}
}

func TestAdmit_QuickFix_NoApproachTable(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if aw.ApproachTable.Present() {
		t.Error("ApproachTable: want not present for a bare workflow")
	}
}

func TestAdmit_ImplementationOnly_GroupsDeclaredFalse(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if aw.GroupsDeclared {
		t.Error("GroupsDeclared: want false for implementation-only (bare workflow)")
	}
}

func TestAdmit_ImplementationOnly_SingleImplicitGroupEmptyName(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want 1 group for bare workflow")
	}
	if aw.Groups[0].Name != "" {
		t.Errorf("Groups[0].Name: want empty string for implicit group, got %q", aw.Groups[0].Name)
	}
}

func TestAdmit_ImplementationOnly_SingleGroupCoversAllExecutionRows(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if len(aw.Groups) != 1 {
		t.Fatalf("Groups: want 1, got %d", len(aw.Groups))
	}
	if aw.Groups[0].StartRow != 0 || aw.Groups[0].EndRow != 2 {
		t.Errorf("Groups[0]: want [0,2), got [%d,%d)", aw.Groups[0].StartRow, aw.Groups[0].EndRow)
	}
}

// ---- Bare EXECUTION rows with any agent mix ----

func TestAdmit_BareRowsMixedAgents_SingleImplicitGroup(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "bare-rows-workflow", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "test-writer-tdd", OutputArtifacts: []string{"tests.md"}},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "unknown-agent", OutputArtifacts: []string{"unknown.md"}},
			{Index: 2, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd", OutputArtifacts: []string{"impl.md"}},
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "unknown-agent-2", OutputArtifacts: []string{"other.md"}},
		},
	}

	aw, err := compat.Admit(table, domain.ExecutionModeAuto)
	if err != nil {
		t.Fatalf("Admit: bare rows with mixed agents must be admitted: %v", err)
	}

	if len(aw.Groups) != 1 {
		t.Errorf("Groups: want 1 implicit group, got %d", len(aw.Groups))
	}
	if len(aw.Groups) > 0 && aw.Groups[0].Name != "" {
		t.Errorf("Groups[0].Name: want empty string for implicit group, got %q", aw.Groups[0].Name)
	}
}

func TestAdmit_BareRowsMixedKnownUnknownAgents_Succeeds(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "bare-rows-mixed-agents", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "test-writer-tdd", OutputArtifacts: []string{"tests.md"}},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "unknown-agent", OutputArtifacts: []string{"unknown.md"}},
			{Index: 2, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd", OutputArtifacts: []string{"impl.md"}},
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "unknown-agent-2", OutputArtifacts: []string{"other.md"}},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	if err != nil {
		t.Errorf("Admit must admit bare EXECUTION rows with any agent identifiers: %v", err)
	}
}

func TestAdmit_BareRowsAlternatingAgents_Succeeds(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "alternating-agents", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "test-writer-tdd"},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd"},
			{Index: 2, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "test-writer-tdd"},
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd"},
		},
	}

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	if err != nil {
		t.Errorf("Admit must admit bare rows with alternating agent identifiers: %v", err)
	}
}
