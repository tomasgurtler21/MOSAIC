package compat_test

// Tests for execution group resolution: row ranges and group names for
// brownfield-tdd-build-verified, brownfield-tdd, greenfield-tdd, quick-fix,
// and implementation-only workflows.

import (
	"testing"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
)

// ---- Execution group resolution: brownfield-tdd-build-verified ----

func TestAdmit_BrownfieldTDDBuildVerified_TwoGroups(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) != 2 {
		t.Errorf("Groups: want 2 groups, got %d", len(aw.Groups))
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_TestGroupName(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1 group")
	}
	if aw.Groups[0].Name != "Test" {
		t.Errorf("Groups[0].Name: want %q, got %q", "Test", aw.Groups[0].Name)
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_TestGroupStartRow(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1 group")
	}
	if aw.Groups[0].StartRow != 7 {
		t.Errorf("Groups[0].StartRow: want 7, got %d", aw.Groups[0].StartRow)
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_TestGroupEndRow(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1 group")
	}
	if aw.Groups[0].EndRow != 10 {
		t.Errorf("Groups[0].EndRow: want 10 (exclusive), got %d", aw.Groups[0].EndRow)
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_ImplementationGroupName(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 2 {
		t.Fatal("Groups: want at least 2 groups")
	}
	if aw.Groups[1].Name != "Implementation" {
		t.Errorf("Groups[1].Name: want %q, got %q", "Implementation", aw.Groups[1].Name)
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_ImplementationGroupStartRow(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 2 {
		t.Fatal("Groups: want at least 2 groups")
	}
	if aw.Groups[1].StartRow != 10 {
		t.Errorf("Groups[1].StartRow: want 10, got %d", aw.Groups[1].StartRow)
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_ImplementationGroupEndRow(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 2 {
		t.Fatal("Groups: want at least 2 groups")
	}
	if aw.Groups[1].EndRow != 13 {
		t.Errorf("Groups[1].EndRow: want 13 (exclusive), got %d", aw.Groups[1].EndRow)
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_GroupsCoverAllExecutionRows(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) != 2 {
		t.Fatalf("Groups: want 2, got %d", len(aw.Groups))
	}
	if aw.Groups[0].EndRow != aw.Groups[1].StartRow {
		t.Errorf("Groups not contiguous: Groups[0].EndRow=%d, Groups[1].StartRow=%d",
			aw.Groups[0].EndRow, aw.Groups[1].StartRow)
	}
	if aw.Groups[0].StartRow != 7 || aw.Groups[1].EndRow != 13 {
		t.Errorf("Groups span: want rows 7-13, got %d-%d",
			aw.Groups[0].StartRow, aw.Groups[1].EndRow)
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_DuplicateAgent_Allowed(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")

	_, err := compat.Admit(table, domain.ExecutionModeAuto)

	if err != nil {
		t.Errorf("Admit must allow duplicate agent identifiers (build-review appears twice): %v", err)
	}
}

// ---- Execution group resolution: brownfield-tdd ----

func TestAdmit_BrownfieldTDD_TestGroupStartRow(t *testing.T) {
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].StartRow != 7 {
		t.Errorf("Groups[0].StartRow: want 7, got %d", aw.Groups[0].StartRow)
	}
}

func TestAdmit_BrownfieldTDD_TestGroupEndRow(t *testing.T) {
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].EndRow != 9 {
		t.Errorf("Groups[0].EndRow: want 9, got %d", aw.Groups[0].EndRow)
	}
}

func TestAdmit_BrownfieldTDD_ImplementationGroupStartRow(t *testing.T) {
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 2 {
		t.Fatal("Groups: want at least 2")
	}
	if aw.Groups[1].StartRow != 9 {
		t.Errorf("Groups[1].StartRow: want 9, got %d", aw.Groups[1].StartRow)
	}
}

func TestAdmit_BrownfieldTDD_ImplementationGroupEndRow(t *testing.T) {
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 2 {
		t.Fatal("Groups: want at least 2")
	}
	if aw.Groups[1].EndRow != 11 {
		t.Errorf("Groups[1].EndRow: want 11, got %d", aw.Groups[1].EndRow)
	}
}

// ---- Execution group resolution: greenfield-tdd ----

func TestAdmit_GreenFieldTDD_TestGroupStartRow(t *testing.T) {
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].StartRow != 8 {
		t.Errorf("Groups[0].StartRow: want 8, got %d", aw.Groups[0].StartRow)
	}
}

func TestAdmit_GreenFieldTDD_TestGroupEndRow(t *testing.T) {
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].EndRow != 10 {
		t.Errorf("Groups[0].EndRow: want 10, got %d", aw.Groups[0].EndRow)
	}
}

func TestAdmit_GreenFieldTDD_ImplementationGroupStartRow(t *testing.T) {
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 2 {
		t.Fatal("Groups: want at least 2")
	}
	if aw.Groups[1].StartRow != 10 {
		t.Errorf("Groups[1].StartRow: want 10, got %d", aw.Groups[1].StartRow)
	}
}

func TestAdmit_GreenFieldTDD_ImplementationGroupEndRow(t *testing.T) {
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 2 {
		t.Fatal("Groups: want at least 2")
	}
	if aw.Groups[1].EndRow != 12 {
		t.Errorf("Groups[1].EndRow: want 12, got %d", aw.Groups[1].EndRow)
	}
}

// ---- Execution group resolution: quick-fix (single-group) ----

func TestAdmit_QuickFix_OneGroup(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) != 1 {
		t.Errorf("Groups: want 1 group for single-group workflow, got %d", len(aw.Groups))
	}
}

func TestAdmit_QuickFix_GroupStartRow(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].StartRow != 2 {
		t.Errorf("Groups[0].StartRow: want 2, got %d", aw.Groups[0].StartRow)
	}
}

func TestAdmit_QuickFix_GroupEndRow(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].EndRow != 3 {
		t.Errorf("Groups[0].EndRow: want 3, got %d", aw.Groups[0].EndRow)
	}
}

// ---- Execution group resolution: implementation-only (single-group) ----

func TestAdmit_ImplementationOnly_OneGroup(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if len(aw.Groups) != 1 {
		t.Errorf("Groups: want 1 group for single-group workflow, got %d", len(aw.Groups))
	}
}

func TestAdmit_ImplementationOnly_GroupStartRow(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].StartRow != 0 {
		t.Errorf("Groups[0].StartRow: want 0, got %d", aw.Groups[0].StartRow)
	}
}

func TestAdmit_ImplementationOnly_GroupEndRow(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].EndRow != 2 {
		t.Errorf("Groups[0].EndRow: want 2, got %d", aw.Groups[0].EndRow)
	}
}

// ---- GroupName for single-group workflows ----

func TestAdmit_QuickFix_GroupName(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].Name != "" {
		t.Errorf("Groups[0].Name: want empty string for implicit group, got %q", aw.Groups[0].Name)
	}
}

func TestAdmit_ImplementationOnly_GroupName(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].Name != "" {
		t.Errorf("Groups[0].Name: want empty string for implicit group, got %q", aw.Groups[0].Name)
	}
}
