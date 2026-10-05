package compat_test

// Tests for admission success and GroupsDeclared/ApproachTable/GroupByName carry-through.

import (
	"testing"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
)

// ---- Admission success ----

func TestAdmit_BrownfieldTDDBuildVerified_Succeeds(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	_, err := compat.Admit(table, domain.ExecutionModeAuto)
	if err != nil {
		t.Errorf("Admit(brownfield-tdd-build-verified): unexpected error: %v", err)
	}
}

func TestAdmit_GreenFieldTDD_Succeeds(t *testing.T) {
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	_, err := compat.Admit(table, domain.ExecutionModeAuto)
	if err != nil {
		t.Errorf("Admit(greenfield-tdd): unexpected error: %v", err)
	}
}

func TestAdmit_BrownfieldTDD_Succeeds(t *testing.T) {
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	_, err := compat.Admit(table, domain.ExecutionModeAuto)
	if err != nil {
		t.Errorf("Admit(brownfield-tdd): unexpected error: %v", err)
	}
}

func TestAdmit_QuickFix_Succeeds(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	_, err := compat.Admit(table, domain.ExecutionModeAuto)
	if err != nil {
		t.Errorf("Admit(quick-fix): unexpected error: %v", err)
	}
}

func TestAdmit_ImplementationOnly_Succeeds(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	_, err := compat.Admit(table, domain.ExecutionModeAuto)
	if err != nil {
		t.Errorf("Admit(implementation-only): unexpected error: %v", err)
	}
}

// ---- HasStagedPhase ----

func TestAdmit_BrownfieldTDDBuildVerified_HasStagedPhase(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if !aw.HasStagedPhase {
		t.Error("brownfield-tdd-build-verified: HasStagedPhase must be true")
	}
}

func TestAdmit_QuickFix_HasStagedPhase(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if !aw.HasStagedPhase {
		t.Error("quick-fix: HasStagedPhase must be true (has EXECUTION.[StageNumber] rows)")
	}
}

func TestAdmit_GreenFieldTDD_HasStagedPhase(t *testing.T) {
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if !aw.HasStagedPhase {
		t.Error("greenfield-tdd: HasStagedPhase must be true (has EXECUTION.[StageNumber] rows)")
	}
}

func TestAdmit_BrownfieldTDD_HasStagedPhase(t *testing.T) {
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if !aw.HasStagedPhase {
		t.Error("brownfield-tdd: HasStagedPhase must be true (has EXECUTION.[StageNumber] rows)")
	}
}

func TestAdmit_ImplementationOnly_HasStagedPhase(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if !aw.HasStagedPhase {
		t.Error("implementation-only: HasStagedPhase must be true (has EXECUTION.[StageNumber] rows)")
	}
}

// ---- Phase-driven group resolution: workflow-defined names ----

func TestAdmit_BrownfieldTDDBuildVerified_GroupsDeclared(t *testing.T) {
	// brownfield-tdd-build-verified has EXECUTION rows with group segments.
	// GroupsDeclared must be true.
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if !aw.GroupsDeclared {
		t.Error("GroupsDeclared: want true (workflow has EXECUTION rows with group segments)")
	}
}

func TestAdmit_GreenFieldTDD_GroupsDeclared(t *testing.T) {
	// greenfield-tdd EXECUTION rows carry group segments (Test, Implementation).
	// GroupsDeclared must be true.
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if !aw.GroupsDeclared {
		t.Error("GroupsDeclared: want true (greenfield-tdd EXECUTION rows carry group segments)")
	}
}

func TestAdmit_BrownfieldTDD_GroupsDeclared(t *testing.T) {
	// brownfield-tdd EXECUTION rows carry group segments (Test, Implementation).
	// GroupsDeclared must be true.
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if !aw.GroupsDeclared {
		t.Error("GroupsDeclared: want true (brownfield-tdd EXECUTION rows carry group segments)")
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_ApproachTableCarriedThrough(t *testing.T) {
	// The approach table declared in the workflow content must be carried onto the
	// AdmittedWorkflow so downstream consumers can resolve group ordering.
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if !aw.ApproachTable.Present() {
		t.Error("ApproachTable: want present (workflow declares an Execution Groups table)")
	}
}

func TestAdmit_GroupByName_FindsGroup(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	g, ok := aw.GroupByName("Test")
	if !ok {
		t.Fatal(`GroupByName("Test"): want found=true, got false`)
	}
	if g.StartRow != 7 {
		t.Errorf(`GroupByName("Test").StartRow: want 7, got %d`, g.StartRow)
	}
}

func TestAdmit_GroupByName_MissingGroupReturnsFalse(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	_, ok := aw.GroupByName("NoSuchGroup")
	if ok {
		t.Error(`GroupByName("NoSuchGroup"): want found=false, got true`)
	}
}
