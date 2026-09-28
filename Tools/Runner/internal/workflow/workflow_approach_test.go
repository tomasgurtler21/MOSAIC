package workflow_test

import (
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/workflow"
)

func TestParse_ApproachTable_Present_WhenHeadingAndTableExist(t *testing.T) {
	// A workflow with the **Execution Groups:** heading and a valid table must
	// report ApproachTable.Present() == true after parsing.
	table := mustParseApproachTable(t)

	if !table.ApproachTable.Present() {
		t.Error("ApproachTable.Present(): want true when heading and table are present, got false")
	}
}

func TestParse_ApproachTable_Absent_WhenNoHeading(t *testing.T) {
	// A workflow without the **Execution Groups:** heading must parse without
	// error and report ApproachTable.Present() == false.
	table := mustParseQuickFix(t)

	if table.ApproachTable.Present() {
		t.Error("ApproachTable.Present(): want false for a workflow with no heading, got true")
	}
}

func TestParse_ApproachTable_RowCount(t *testing.T) {
	// The approach table fixture has two rows; len(ApproachTable.Rows) must be 2.
	table := mustParseApproachTable(t)

	got := len(table.ApproachTable.Rows)
	if got != 2 {
		t.Errorf("len(ApproachTable.Rows): want 2, got %d", got)
	}
}

func TestParse_ApproachTable_FirstRow_ApproachToken(t *testing.T) {
	// The first row's Approach token must be "TDD" (verbatim, case-preserved).
	table := mustParseApproachTable(t)
	if len(table.ApproachTable.Rows) < 1 {
		t.Fatal("ApproachTable.Rows is empty")
	}

	got := string(table.ApproachTable.Rows[0].Approach)
	if got != "TDD" {
		t.Errorf("ApproachTable.Rows[0].Approach: want %q, got %q", "TDD", got)
	}
}

func TestParse_ApproachTable_SecondRow_ApproachToken(t *testing.T) {
	// The second row's Approach token must be "Implementation-First" (verbatim).
	table := mustParseApproachTable(t)
	if len(table.ApproachTable.Rows) < 2 {
		t.Fatal("ApproachTable.Rows has fewer than 2 rows")
	}

	got := string(table.ApproachTable.Rows[1].Approach)
	if got != "Implementation-First" {
		t.Errorf("ApproachTable.Rows[1].Approach: want %q, got %q", "Implementation-First", got)
	}
}

func TestParse_ApproachTable_GroupOrderPreserved_TDD(t *testing.T) {
	// Sequence("TDD") must return ["Test", "Implementation"] — the order from
	// the table cell, not alphabetical or any other derived order.
	table := mustParseApproachTable(t)

	groups, ok := table.ApproachTable.Sequence("TDD")
	if !ok {
		t.Fatal(`ApproachTable.Sequence("TDD"): want ok=true, got false`)
	}
	want := []domain.GroupName{"Test", "Implementation"}
	if len(groups) != len(want) {
		t.Fatalf(`Sequence("TDD"): want %v, got %v`, want, groups)
	}
	for i, g := range groups {
		if g != want[i] {
			t.Errorf(`Sequence("TDD")[%d]: want %q, got %q`, i, want[i], g)
		}
	}
}

func TestParse_ApproachTable_GroupOrderPreserved_ImplementationFirst(t *testing.T) {
	// Sequence("Implementation-First") must return ["Implementation", "Test"] —
	// the inverted order compared to TDD, proving order is cell-driven.
	table := mustParseApproachTable(t)

	groups, ok := table.ApproachTable.Sequence("Implementation-First")
	if !ok {
		t.Fatal(`ApproachTable.Sequence("Implementation-First"): want ok=true, got false`)
	}
	want := []domain.GroupName{"Implementation", "Test"}
	if len(groups) != len(want) {
		t.Fatalf(`Sequence("Implementation-First"): want %v, got %v`, want, groups)
	}
	for i, g := range groups {
		if g != want[i] {
			t.Errorf(`Sequence("Implementation-First")[%d]: want %q, got %q`, i, want[i], g)
		}
	}
}

func TestParse_ApproachTable_Sequence_UnknownApproach_ReturnsFalse(t *testing.T) {
	// Sequence for an approach not declared in the table must return (nil, false).
	table := mustParseApproachTable(t)

	_, ok := table.ApproachTable.Sequence("Nonexistent")
	if ok {
		t.Error(`ApproachTable.Sequence("Nonexistent"): want ok=false for undeclared approach, got true`)
	}
}

func TestParse_ApproachTable_GroupOmittedFromSomeRows(t *testing.T) {
	// When an approach row lists only one group, Sequence must return a
	// single-element slice — absent groups are not padded or defaulted.
	info := domain.WorkflowInfo{ID: "single-group-approach", Version: "1.0"}
	table, err := workflow.Parse([]byte(approachTableSingleGroupRowContent), info)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	groups, ok := table.ApproachTable.Sequence("Implementation-Only")
	if !ok {
		t.Fatal(`Sequence("Implementation-Only"): want ok=true, got false`)
	}
	if len(groups) != 1 || groups[0] != "Implementation" {
		t.Errorf(`Sequence("Implementation-Only"): want ["Implementation"], got %v`, groups)
	}
}

func TestParse_ApproachTable_MoreThanTwoGroups(t *testing.T) {
	// An approach row listing three groups must decode all three in order.
	info := domain.WorkflowInfo{ID: "three-groups", Version: "1.0"}
	table, err := workflow.Parse([]byte(approachTableThreeGroupsContent), info)
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}

	groups, ok := table.ApproachTable.Sequence("AllGroups")
	if !ok {
		t.Fatal(`Sequence("AllGroups"): want ok=true, got false`)
	}
	want := []domain.GroupName{"Alpha", "Beta", "Gamma"}
	if len(groups) != len(want) {
		t.Fatalf(`Sequence("AllGroups"): want %v, got %v`, want, groups)
	}
	for i, g := range groups {
		if g != want[i] {
			t.Errorf(`Sequence("AllGroups")[%d]: want %q, got %q`, i, want[i], g)
		}
	}
}

func TestParse_ApproachTable_RowOrderPreserved(t *testing.T) {
	// Rows in ApproachTable must appear in declaration order (TDD first,
	// Implementation-First second).
	table := mustParseApproachTable(t)
	if len(table.ApproachTable.Rows) < 2 {
		t.Fatal("ApproachTable.Rows has fewer than 2 rows")
	}

	if table.ApproachTable.Rows[0].Approach != "TDD" {
		t.Errorf("ApproachTable.Rows[0].Approach: want %q (declaration order), got %q",
			"TDD", table.ApproachTable.Rows[0].Approach)
	}
	if table.ApproachTable.Rows[1].Approach != "Implementation-First" {
		t.Errorf("ApproachTable.Rows[1].Approach: want %q (declaration order), got %q",
			"Implementation-First", table.ApproachTable.Rows[1].Approach)
	}
}

func TestParse_ApproachTable_NearMissHeading_NotTreatedAsReservedHeading(t *testing.T) {
	// A line that contains the heading text but is not an exact trimmed line match
	// must not activate approach-table parsing. The workflow must parse successfully
	// and report no approach table, proving the exact-match anchoring described in
	// the Risks section of Plan.md.
	info := domain.WorkflowInfo{ID: "near-miss-heading", Version: "1.0"}

	table, err := workflow.Parse([]byte(approachTableNearMissHeadingContent), info)

	if err != nil {
		t.Fatalf("Parse: unexpected error for near-miss heading: %v", err)
	}
	if table.ApproachTable.Present() {
		t.Error("ApproachTable.Present(): want false when heading line is not an exact trimmed match, got true")
	}
}

// ---- ApproachTable helper methods (domain.ApproachTable.Approaches and GroupNames) ----

func TestApproachTable_Approaches_ReturnsTokensInTableOrder(t *testing.T) {
	// Approaches() must return all declared approach tokens in declaration order.
	at := domain.ApproachTable{
		Rows: []domain.ApproachSequence{
			{Approach: "TDD", Groups: []domain.GroupName{"Test", "Implementation"}},
			{Approach: "Implementation-First", Groups: []domain.GroupName{"Implementation", "Test"}},
		},
	}

	got := at.Approaches()

	want := []domain.Approach{"TDD", "Implementation-First"}
	if len(got) != len(want) {
		t.Fatalf("Approaches(): want %v, got %v", want, got)
	}
	for i, a := range got {
		if a != want[i] {
			t.Errorf("Approaches()[%d]: want %q, got %q", i, want[i], a)
		}
	}
}

func TestApproachTable_GroupNames_DeduplicatesInFirstAppearanceOrder(t *testing.T) {
	// GroupNames() must return every group named anywhere in the table, deduplicated,
	// in order of first appearance. When the same group appears in multiple rows,
	// it must appear exactly once at its first-appearance position.
	at := domain.ApproachTable{
		Rows: []domain.ApproachSequence{
			{Approach: "TDD", Groups: []domain.GroupName{"Test", "Implementation"}},
			{Approach: "Implementation-First", Groups: []domain.GroupName{"Implementation", "Test"}},
		},
	}

	got := at.GroupNames()

	// "Test" first appears in row 0, position 0.
	// "Implementation" first appears in row 0, position 1.
	// Row 1 repeats both in reverse order — neither must appear again.
	want := []domain.GroupName{"Test", "Implementation"}
	if len(got) != len(want) {
		t.Fatalf("GroupNames(): want %v, got %v", want, got)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("GroupNames()[%d]: want %q, got %q", i, want[i], g)
		}
	}
}
