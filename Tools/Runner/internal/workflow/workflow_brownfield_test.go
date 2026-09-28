package workflow_test

import (
	"testing"
)

func TestParse_Brownfield_RowCount(t *testing.T) {
	table := mustParseBrownfield(t)

	if len(table.Rows) != 13 {
		t.Errorf("want 13 rows, got %d", len(table.Rows))
	}
}

func TestParse_Brownfield_RowIndices_ZeroBasedConsecutive(t *testing.T) {
	table := mustParseBrownfield(t)

	for i, row := range table.Rows {
		if row.Index != i {
			t.Errorf("Rows[%d].Index: want %d, got %d", i, i, row.Index)
		}
	}
}

func TestParse_Brownfield_ExecutionRows_AllStaged(t *testing.T) {
	// Rows 7-12 are EXECUTION.[StageNumber] rows and must all have IsStaged == true.
	table := mustParseBrownfield(t)

	for i := 7; i <= 12; i++ {
		if !table.Rows[i].PhaseParsed.IsStaged {
			t.Errorf("Rows[%d] (EXECUTION.[StageNumber]): PhaseParsed.IsStaged must be true", i)
		}
	}
}

func TestParse_Brownfield_NonExecutionRows_NotStaged(t *testing.T) {
	// Rows 0-6 (RESEARCH, PLANNING, DESIGN) must not be staged.
	table := mustParseBrownfield(t)

	for i := 0; i <= 6; i++ {
		if table.Rows[i].PhaseParsed.IsStaged {
			t.Errorf("Rows[%d] (%s): PhaseParsed.IsStaged must be false", i, table.Rows[i].Phase)
		}
	}
}

func TestParse_Brownfield_ArtifactTemplate_PreservedVerbatim(t *testing.T) {
	// Row 7 (test-writer-tdd) input includes "Stage-{StageNumber}/Plan.md".
	// The template notation must not be altered during parsing.
	table := mustParseBrownfield(t)

	artifacts := table.Rows[7].InputArtifacts
	found := false
	for _, a := range artifacts {
		if a == "Stage-{StageNumber}/Plan.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Rows[7].InputArtifacts: want %q verbatim, got %v", "Stage-{StageNumber}/Plan.md", artifacts)
	}
}

func TestParse_Brownfield_MultiInputArtifacts_SplitCorrectly(t *testing.T) {
	// Row 9 (tests-review-tdd) has 4 comma-separated input paths.
	table := mustParseBrownfield(t)

	got := len(table.Rows[9].InputArtifacts)
	if got != 4 {
		t.Errorf("Rows[9].InputArtifacts: want 4 elements, got %d: %v", got, table.Rows[9].InputArtifacts)
	}
}

func TestParse_Brownfield_OnFindings_FreeFormText_PreservedVerbatim(t *testing.T) {
	// Row 12 (implementation-review) On Findings = "implementation-tdd (or other based on issue)".
	// Free-form qualification text must be preserved verbatim in Value.
	table := mustParseBrownfield(t)

	got := table.Rows[12].OnFindings.Value
	want := "implementation-tdd (or other based on issue)"
	if got != want {
		t.Errorf("Rows[12].OnFindings.Value: want %q, got %q", want, got)
	}
}

func TestParse_Brownfield_GlobPattern_OutputArtifact_Preserved(t *testing.T) {
	// Row 3 (planner-tdd-soft) output includes "Stage-*/Plan.md".
	table := mustParseBrownfield(t)

	artifacts := table.Rows[3].OutputArtifacts
	found := false
	for _, a := range artifacts {
		if a == "Stage-*/Plan.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Rows[3].OutputArtifacts: want %q verbatim, got %v", "Stage-*/Plan.md", artifacts)
	}
}

// ---- implementation-only: starts with EXECUTION rows ----

func TestParse_ImplementationOnly_RowCount(t *testing.T) {
	table := mustParseImplOnly(t)

	if len(table.Rows) != 3 {
		t.Errorf("want 3 rows, got %d", len(table.Rows))
	}
}

func TestParse_ImplementationOnly_FirstRow_IsStaged(t *testing.T) {
	// This workflow has no pre-EXECUTION rows; row 0 must be a staged EXECUTION row.
	table := mustParseImplOnly(t)

	if !table.Rows[0].PhaseParsed.IsStaged {
		t.Error("Rows[0] (EXECUTION.[StageNumber]): PhaseParsed.IsStaged must be true")
	}
}

func TestParse_ImplementationOnly_LastRow_NotStaged(t *testing.T) {
	// Row 2 (REVIEW) must not be staged.
	table := mustParseImplOnly(t)

	if table.Rows[2].PhaseParsed.IsStaged {
		t.Errorf("Rows[2] (REVIEW): PhaseParsed.IsStaged must be false, got true")
	}
}
