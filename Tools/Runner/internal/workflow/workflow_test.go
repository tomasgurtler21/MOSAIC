package workflow_test

// Tests for workflow.Parse.
//
// Coverage:
//   Happy path – quick-fix workflow (two PLANNING rows, one EXECUTION, one REVIEW):
//   - Returns the correct number of rows.
//   - Row indices are zero-based and consecutive in declaration order.
//   - HITL column decoded: ✅ → true, ❌ → false.
//   - Non-EXECUTION row Phase is not staged (PhaseParsed.IsStaged == false).
//   - Non-EXECUTION row PhaseParsed.Name matches the literal phase string.
//   - EXECUTION.[StageNumber] row is staged (PhaseParsed.IsStaged == true).
//   - EXECUTION.[StageNumber] row PhaseParsed.Name == "EXECUTION".
//   - Comma-separated artifact paths are split and trimmed.
//   - Stage-*/... glob paths are preserved verbatim in output artifacts.
//   - Stage-{StageNumber}/... template paths are preserved verbatim in input artifacts.
//   - Input column "-" produces an empty InputArtifacts slice.
//   - On Success column is present (ColumnPresent == true when column exists).
//   - On Findings column value "-" produces OptionalHint with Value == "".
//   - WorkflowInfo is carried unchanged into RoutingTable.Info.
//
//   Happy path – brownfield-tdd-build-verified (13 rows, two EXECUTION groups):
//   - Returns 13 rows.
//   - Row indices are zero-based and consecutive.
//   - All EXECUTION rows have PhaseParsed.IsStaged == true.
//   - Artifact template paths preserved verbatim (Stage-{StageNumber}/...).
//   - Multi-artifact input cells split into the correct number of elements.
//   - Free-form On Findings text preserved verbatim.
//   - Agent identifier preserved verbatim.
//
//   Happy path – implementation-only (starts with EXECUTION rows):
//   - Returns 3 rows.
//   - Row 0 is a staged EXECUTION row (workflow starts without pre-EXECUTION rows).
//
//   Optional column handling:
//   - Table without On Success column → OnSuccess.ColumnPresent == false for all rows.
//   - Table without On Findings column → OnFindings.ColumnPresent == false for all rows.
//   - On Success and On Findings with "-" → ColumnPresent true, Value "".
//
//   Refusal cases:
//   - No parseable table → *domain.RefusalError.
//   - Missing required column Phase → *domain.RefusalError.
//   - Missing required column Subagent → *domain.RefusalError.
//   - Missing required column HITL → *domain.RefusalError.
//   - Missing required column Input → *domain.RefusalError.
//   - Missing required column Output → *domain.RefusalError.
//   - Table with header and separator but zero data rows → *domain.RefusalError.

import (
	"errors"
	"testing"

	"mosaic-run/internal/domain"
	"mosaic-run/internal/workflow"
)

// asRefusalError asserts that err is (or wraps) a *domain.RefusalError.
// Calls t.Fatal on failure.
func asRefusalError(t *testing.T, err error) *domain.RefusalError {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	return re
}

// ---- Fixture content extracted from real workflow files ----
//
// quickFixContent is the raw bytes of the [[SECTION:Workflow:quick-fix]] region
// from Workflows/Build/quick-fix.md (version 3.0). The content is the bytes
// between the boundary tags, not including the tags themselves.
const quickFixContent = `<!-- workflow-version: 3.0 -->
## Quick Fix Workflow

**Use when:** Small changes, bug fixes, or well-understood modifications. Skips research and design.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | implementation-tdd | planner-tdd-soft | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | test-runner | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |

**Notes:**
- Single-stage plans use Stage-1/ folder for consistency (Decision 15)
`

// brownfieldContent is the raw bytes of the [[SECTION:Workflow:brownfield-tdd-build-verified]]
// region from Workflows/Build/brownfield-tdd-build-verified.md (version 2.0).
const brownfieldContent = `<!-- workflow-version: 2.0 -->
## Brownfield TDD Build-Verified Workflow

**Use when:** New features or significant changes to an **existing codebase** requiring test-first development where **compilation/build cannot be verified via standard terminal tools**.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ❌ | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.[StageNumber] | test-writer-tdd | ❌ | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | build-review | ❌ | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-tests.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | build-review | ❌ | implementation-review | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | COMPLETE | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-impl.md | Stage-{StageNumber}/implementation-review.md |
`

// implOnlyContent is the raw bytes of the [[SECTION:Workflow:implementation-only]]
// region from Workflows/Build/implementation-only.md (version 3.1). This
// workflow starts with EXECUTION rows (no pre-EXECUTION rows).
const implOnlyContent = `<!-- workflow-version: 3.1 -->
## Implementation Only Workflow

**Use when:** Research, planning, and design already complete.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |
`

// minimalContent is a minimal routing table with only required columns.
// Used to test that optional columns are absent (ColumnPresent == false).
const minimalContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | planner | ✅ | - | Plan.md |
| EXECUTION.[StageNumber] | implementer | ❌ | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
`

// emptyTableContent has a header and separator row but no data rows.
const emptyTableContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
`

// onSuccessDashContent is a routing table where the On Success cell is "-",
// used to verify the symmetric no-hint behavior for that optional column.
// KC-8/FR-27 requires that "-" in any optional column means no hint, yielding
// OptionalHint{ColumnPresent: true, Value: ""}.
const onSuccessDashContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | ✅ | - | next-step | - | Plan.md |
`

// outputDashContent is a routing table where the Output cell is "-",
// used to verify that "-" in the Output column produces an empty OutputArtifacts slice,
// symmetric with the Input column behavior tested in TestParse_QuickFix_DashInput_ProducesEmptyArtifacts.
const outputDashContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | planner | ✅ | Plan.md | - |
`

// ---- Helpers ----

func mustParseQuickFix(t *testing.T) domain.RoutingTable {
	t.Helper()
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	table, err := workflow.Parse([]byte(quickFixContent), info)
	if err != nil {
		t.Fatalf("Parse(quick-fix): unexpected error: %v", err)
	}
	return table
}

func mustParseBrownfield(t *testing.T) domain.RoutingTable {
	t.Helper()
	info := domain.WorkflowInfo{ID: "brownfield-tdd-build-verified", Version: "2.0"}
	table, err := workflow.Parse([]byte(brownfieldContent), info)
	if err != nil {
		t.Fatalf("Parse(brownfield): unexpected error: %v", err)
	}
	return table
}

func mustParseImplOnly(t *testing.T) domain.RoutingTable {
	t.Helper()
	info := domain.WorkflowInfo{ID: "implementation-only", Version: "3.1"}
	table, err := workflow.Parse([]byte(implOnlyContent), info)
	if err != nil {
		t.Fatalf("Parse(implementation-only): unexpected error: %v", err)
	}
	return table
}

// ---- Quick-fix: happy path ----

func TestParse_QuickFix_RowCount(t *testing.T) {
	table := mustParseQuickFix(t)

	if len(table.Rows) != 4 {
		t.Errorf("want 4 rows, got %d", len(table.Rows))
	}
}

func TestParse_QuickFix_RowIndices_ZeroBasedConsecutive(t *testing.T) {
	// Indices must be 0, 1, 2, 3 in declaration order.
	table := mustParseQuickFix(t)

	for i, row := range table.Rows {
		if row.Index != i {
			t.Errorf("Rows[%d].Index: want %d, got %d", i, i, row.Index)
		}
	}
}

func TestParse_QuickFix_HITL_False_ForCrossmark(t *testing.T) {
	// Row 1 (plan-review) has ❌ in the HITL column → HITL must be false.
	table := mustParseQuickFix(t)

	if table.Rows[1].HITL {
		t.Errorf("Rows[1].HITL: want false (❌ plan-review), got true")
	}
}

func TestParse_QuickFix_HITL_True_ForCheckmark(t *testing.T) {
	// Row 0 (planner-tdd-soft) has ✅ in the HITL column → HITL must be true.
	table := mustParseQuickFix(t)

	if !table.Rows[0].HITL {
		t.Errorf("Rows[0].HITL: want true (✅ planner-tdd-soft), got false")
	}
}

func TestParse_QuickFix_NonExecutionRow_NotStaged(t *testing.T) {
	// PLANNING rows must not be flagged as staged.
	table := mustParseQuickFix(t)

	if table.Rows[0].PhaseParsed.IsStaged {
		t.Errorf("Rows[0] (PLANNING): PhaseParsed.IsStaged must be false for a non-EXECUTION phase")
	}
}

func TestParse_QuickFix_NonExecutionRow_PhaseName(t *testing.T) {
	// For a PLANNING row, PhaseParsed.Name must be "PLANNING".
	table := mustParseQuickFix(t)

	got := table.Rows[0].PhaseParsed.Name
	if got != "PLANNING" {
		t.Errorf("Rows[0].PhaseParsed.Name: want %q, got %q", "PLANNING", got)
	}
}

func TestParse_QuickFix_NonExecutionRow_Phase_LiteralPreserved(t *testing.T) {
	// The Phase field must carry the literal text from the table cell.
	table := mustParseQuickFix(t)

	got := table.Rows[0].Phase
	if got != "PLANNING" {
		t.Errorf("Rows[0].Phase: want %q, got %q", "PLANNING", got)
	}
}

func TestParse_QuickFix_ExecutionRow_IsStaged(t *testing.T) {
	// Row 2 (EXECUTION.[StageNumber]) must be flagged as staged.
	table := mustParseQuickFix(t)

	if !table.Rows[2].PhaseParsed.IsStaged {
		t.Errorf("Rows[2] (EXECUTION.[StageNumber]): PhaseParsed.IsStaged must be true")
	}
}

func TestParse_QuickFix_ExecutionRow_PhaseName(t *testing.T) {
	// The PhaseParsed.Name for an EXECUTION.[StageNumber] row must be "EXECUTION".
	table := mustParseQuickFix(t)

	got := table.Rows[2].PhaseParsed.Name
	if got != "EXECUTION" {
		t.Errorf("Rows[2].PhaseParsed.Name: want %q, got %q", "EXECUTION", got)
	}
}

func TestParse_QuickFix_ExecutionRow_Phase_LiteralPreserved(t *testing.T) {
	// The Phase field must preserve the literal "EXECUTION.[StageNumber]" string.
	table := mustParseQuickFix(t)

	got := table.Rows[2].Phase
	if got != "EXECUTION.[StageNumber]" {
		t.Errorf("Rows[2].Phase: want %q, got %q", "EXECUTION.[StageNumber]", got)
	}
}

func TestParse_QuickFix_GlobPattern_PreservedVerbatim_OutputArtifact(t *testing.T) {
	// Row 0 output: "Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md".
	// The Stage-*/... glob patterns must survive verbatim in OutputArtifacts.
	table := mustParseQuickFix(t)

	artifacts := table.Rows[0].OutputArtifacts
	found := false
	for _, a := range artifacts {
		if a == "Stage-*/Plan.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Rows[0].OutputArtifacts: want %q to be present verbatim, got %v", "Stage-*/Plan.md", artifacts)
	}
}

func TestParse_QuickFix_TemplatePattern_PreservedVerbatim_InputArtifact(t *testing.T) {
	// Row 2 input: "Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md".
	// The Stage-{StageNumber}/... template must survive verbatim.
	table := mustParseQuickFix(t)

	artifacts := table.Rows[2].InputArtifacts
	found := false
	for _, a := range artifacts {
		if a == "Stage-{StageNumber}/Plan.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Rows[2].InputArtifacts: want %q to be present verbatim, got %v", "Stage-{StageNumber}/Plan.md", artifacts)
	}
}

func TestParse_QuickFix_CommaSeparatedArtifacts_SplitAndTrimmed(t *testing.T) {
	// Row 0 output has three comma-separated paths → OutputArtifacts must have 3 elements.
	table := mustParseQuickFix(t)

	got := len(table.Rows[0].OutputArtifacts)
	if got != 3 {
		t.Errorf("Rows[0].OutputArtifacts: want 3 elements, got %d: %v", got, table.Rows[0].OutputArtifacts)
	}
}

func TestParse_QuickFix_DashInput_ProducesEmptyArtifacts(t *testing.T) {
	// Row 0 has Input = "-" which means no input artifacts.
	// InputArtifacts must be empty (nil or zero-length slice).
	table := mustParseQuickFix(t)

	if len(table.Rows[0].InputArtifacts) != 0 {
		t.Errorf("Rows[0].InputArtifacts: want empty (Input is \"-\"), got %v", table.Rows[0].InputArtifacts)
	}
}

func TestParse_QuickFix_OnSuccess_ColumnPresent(t *testing.T) {
	// The On Success column exists in the quick-fix table, so ColumnPresent must be true.
	table := mustParseQuickFix(t)

	if !table.Rows[0].OnSuccess.ColumnPresent {
		t.Error("Rows[0].OnSuccess.ColumnPresent: want true (column exists in table), got false")
	}
}

func TestParse_QuickFix_OnSuccess_Value_WhenNotDash(t *testing.T) {
	// Row 0 On Success = "plan-review" → Value must be "plan-review".
	table := mustParseQuickFix(t)

	got := table.Rows[0].OnSuccess.Value
	if got != "plan-review" {
		t.Errorf("Rows[0].OnSuccess.Value: want %q, got %q", "plan-review", got)
	}
}

func TestParse_QuickFix_OnFindings_Dash_IsNoHint(t *testing.T) {
	// Row 0 On Findings = "-" → ColumnPresent true, Value "".
	table := mustParseQuickFix(t)

	hint := table.Rows[0].OnFindings
	if !hint.ColumnPresent {
		t.Error("Rows[0].OnFindings.ColumnPresent: want true (column exists), got false")
	}
	if hint.Value != "" {
		t.Errorf("Rows[0].OnFindings.Value: want %q for dash (no hint), got %q", "", hint.Value)
	}
}

func TestParse_QuickFix_OnFindings_Value_WhenNotDash(t *testing.T) {
	// Row 1 On Findings = "planner-tdd-soft" → Value must be "planner-tdd-soft".
	table := mustParseQuickFix(t)

	got := table.Rows[1].OnFindings.Value
	if got != "planner-tdd-soft" {
		t.Errorf("Rows[1].OnFindings.Value: want %q, got %q", "planner-tdd-soft", got)
	}
}

func TestParse_QuickFix_AgentIdentifier_PreservedVerbatim(t *testing.T) {
	// The Subagent column value must be stored verbatim in Agent.
	table := mustParseQuickFix(t)

	got := table.Rows[0].Agent
	if got != "planner-tdd-soft" {
		t.Errorf("Rows[0].Agent: want %q, got %q", "planner-tdd-soft", got)
	}
}

func TestParse_QuickFix_WorkflowInfo_CarriedIntoTable(t *testing.T) {
	// The WorkflowInfo passed to Parse must appear unchanged in RoutingTable.Info.
	info := domain.WorkflowInfo{ID: "quick-fix", Version: "3.0"}
	table, err := workflow.Parse([]byte(quickFixContent), info)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if table.Info.ID != info.ID {
		t.Errorf("RoutingTable.Info.ID: want %q, got %q", info.ID, table.Info.ID)
	}
	if table.Info.Version != info.Version {
		t.Errorf("RoutingTable.Info.Version: want %q, got %q", info.Version, table.Info.Version)
	}
}

// ---- Brownfield: happy path ----

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

// ---- Optional column handling ----

func TestParse_MinimalTable_OnSuccess_ColumnPresentFalse(t *testing.T) {
	// A table with no On Success column must have ColumnPresent == false on every row.
	info := domain.WorkflowInfo{ID: "minimal", Version: "1.0"}
	table, err := workflow.Parse([]byte(minimalContent), info)
	if err != nil {
		t.Fatalf("Parse(minimal): %v", err)
	}

	for i, row := range table.Rows {
		if row.OnSuccess.ColumnPresent {
			t.Errorf("Rows[%d].OnSuccess.ColumnPresent: want false (column absent), got true", i)
		}
	}
}

func TestParse_MinimalTable_OnFindings_ColumnPresentFalse(t *testing.T) {
	// A table with no On Findings column must have ColumnPresent == false on every row.
	info := domain.WorkflowInfo{ID: "minimal", Version: "1.0"}
	table, err := workflow.Parse([]byte(minimalContent), info)
	if err != nil {
		t.Fatalf("Parse(minimal): %v", err)
	}

	for i, row := range table.Rows {
		if row.OnFindings.ColumnPresent {
			t.Errorf("Rows[%d].OnFindings.ColumnPresent: want false (column absent), got true", i)
		}
	}
}

func TestParse_QuickFix_OnFindings_ColumnPresent_WhenColumnExists(t *testing.T) {
	// When the On Findings column exists, ColumnPresent must be true regardless of cell value.
	table := mustParseQuickFix(t)

	for i, row := range table.Rows {
		if !row.OnFindings.ColumnPresent {
			t.Errorf("Rows[%d].OnFindings.ColumnPresent: want true (column exists), got false", i)
		}
	}
}

func TestParse_QuickFix_OnSuccess_ColumnPresent_WhenColumnExists(t *testing.T) {
	table := mustParseQuickFix(t)

	for i, row := range table.Rows {
		if !row.OnSuccess.ColumnPresent {
			t.Errorf("Rows[%d].OnSuccess.ColumnPresent: want true (column exists), got false", i)
		}
	}
}

func TestParse_OnSuccessDash_ColumnPresent_ValueEmpty(t *testing.T) {
	// On Success column value "-" must produce OptionalHint{ColumnPresent: true, Value: ""},
	// the same no-hint behavior as "-" in the On Findings column and as an absent column
	// value. This verifies the symmetric handling required by KC-8/FR-27.
	info := domain.WorkflowInfo{ID: "on-success-dash", Version: "1.0"}
	table, err := workflow.Parse([]byte(onSuccessDashContent), info)
	if err != nil {
		t.Fatalf("Parse(on-success-dash): unexpected error: %v", err)
	}

	hint := table.Rows[0].OnSuccess
	if !hint.ColumnPresent {
		t.Error("Rows[0].OnSuccess.ColumnPresent: want true (column exists), got false")
	}
	if hint.Value != "" {
		t.Errorf("Rows[0].OnSuccess.Value: want %q for dash cell (no hint), got %q", "", hint.Value)
	}
}

func TestParse_OutputDash_ProducesEmptyArtifacts(t *testing.T) {
	// Output column value "-" must produce an empty OutputArtifacts slice,
	// symmetric with the Input column behavior (TestParse_QuickFix_DashInput_ProducesEmptyArtifacts).
	info := domain.WorkflowInfo{ID: "output-dash", Version: "1.0"}
	table, err := workflow.Parse([]byte(outputDashContent), info)
	if err != nil {
		t.Fatalf("Parse(output-dash): unexpected error: %v", err)
	}

	if len(table.Rows[0].OutputArtifacts) != 0 {
		t.Errorf("Rows[0].OutputArtifacts: want empty (Output is \"-\"), got %v", table.Rows[0].OutputArtifacts)
	}
}

// ---- Refusal cases ----

func TestParse_NoTable_ReturnsError(t *testing.T) {
	content := []byte("<!-- workflow-version: 1.0 -->\nNo routing table here.\n")
	info := domain.WorkflowInfo{ID: "no-table", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	if err == nil {
		t.Fatal("Parse must return an error when no routing table is present")
	}
}

func TestParse_NoTable_ReturnsRefusalError(t *testing.T) {
	content := []byte("<!-- workflow-version: 1.0 -->\nNo routing table here.\n")
	info := domain.WorkflowInfo{ID: "no-table", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	asRefusalError(t, err)
}

func TestParse_MissingRequiredColumn_Phase_ReturnsRefusalError(t *testing.T) {
	content := []byte(`<!-- workflow-version: 1.0 -->
| Subagent | HITL | Input | Output |
|----------|:----:|-------|--------|
| planner | ✅ | - | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-phase", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	if err == nil {
		t.Fatal("Parse must return an error when Phase column is missing")
	}
	asRefusalError(t, err)
}

func TestParse_MissingRequiredColumn_Subagent_ReturnsRefusalError(t *testing.T) {
	content := []byte(`<!-- workflow-version: 1.0 -->
| Phase | HITL | Input | Output |
|-------|:----:|-------|--------|
| PLANNING | ✅ | - | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-subagent", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	if err == nil {
		t.Fatal("Parse must return an error when Subagent column is missing")
	}
	asRefusalError(t, err)
}

func TestParse_MissingRequiredColumn_HITL_ReturnsRefusalError(t *testing.T) {
	content := []byte(`<!-- workflow-version: 1.0 -->
| Phase | Subagent | Input | Output |
|-------|----------|-------|--------|
| PLANNING | planner | - | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-hitl", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	if err == nil {
		t.Fatal("Parse must return an error when HITL column is missing")
	}
	asRefusalError(t, err)
}

func TestParse_MissingRequiredColumn_Input_ReturnsRefusalError(t *testing.T) {
	content := []byte(`<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Output |
|-------|----------|:----:|--------|
| PLANNING | planner | ✅ | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-input", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	if err == nil {
		t.Fatal("Parse must return an error when Input column is missing")
	}
	asRefusalError(t, err)
}

func TestParse_MissingRequiredColumn_Output_ReturnsRefusalError(t *testing.T) {
	content := []byte(`<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input |
|-------|----------|:----:|-------|
| PLANNING | planner | ✅ | - |
`)
	info := domain.WorkflowInfo{ID: "missing-output", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	if err == nil {
		t.Fatal("Parse must return an error when Output column is missing")
	}
	asRefusalError(t, err)
}

// Missing-column refusal: Component and Resource contract
//
// The five missing-required-column cases must all populate RefusalError with
// Component == "workflow" and a non-empty Resource naming the workflow, so that
// callers and error messages can attribute and locate the problem.

func TestParse_MissingRequiredColumn_Phase_RefusalError_ComponentAndResource(t *testing.T) {
	content := []byte(`<!-- workflow-version: 1.0 -->
| Subagent | HITL | Input | Output |
|----------|:----:|-------|--------|
| planner | ✅ | - | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-phase", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

func TestParse_MissingRequiredColumn_Subagent_RefusalError_ComponentAndResource(t *testing.T) {
	content := []byte(`<!-- workflow-version: 1.0 -->
| Phase | HITL | Input | Output |
|-------|:----:|-------|--------|
| PLANNING | ✅ | - | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-subagent", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

func TestParse_MissingRequiredColumn_HITL_RefusalError_ComponentAndResource(t *testing.T) {
	content := []byte(`<!-- workflow-version: 1.0 -->
| Phase | Subagent | Input | Output |
|-------|----------|-------|--------|
| PLANNING | planner | - | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-hitl", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

func TestParse_MissingRequiredColumn_Input_RefusalError_ComponentAndResource(t *testing.T) {
	content := []byte(`<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Output |
|-------|----------|:----:|--------|
| PLANNING | planner | ✅ | Plan.md |
`)
	info := domain.WorkflowInfo{ID: "missing-input", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

func TestParse_MissingRequiredColumn_Output_RefusalError_ComponentAndResource(t *testing.T) {
	content := []byte(`<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input |
|-------|----------|:----:|-------|
| PLANNING | planner | ✅ | - |
`)
	info := domain.WorkflowInfo{ID: "missing-output", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

func TestParse_EmptyTable_ZeroDataRows_ReturnsRefusalError(t *testing.T) {
	// A table with a valid header and separator but no data rows must be refused.
	info := domain.WorkflowInfo{ID: "empty-table", Version: "1.0"}

	_, err := workflow.Parse([]byte(emptyTableContent), info)

	if err == nil {
		t.Fatal("Parse must return an error for a table with zero data rows")
	}
	asRefusalError(t, err)
}

func TestParse_RefusalError_ComponentIsWorkflow(t *testing.T) {
	// Every refusal from workflow.Parse must name "workflow" as the component.
	content := []byte("No table.\n")
	info := domain.WorkflowInfo{ID: "no-table", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
}

func TestParse_RefusalError_ResourceNamesWorkflow(t *testing.T) {
	// The RefusalError.Resource must identify the workflow so the user can
	// find the problematic file.
	content := []byte("No table.\n")
	info := domain.WorkflowInfo{ID: "my-special-workflow", Version: "1.0"}

	_, err := workflow.Parse(content, info)

	re := asRefusalError(t, err)
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}
