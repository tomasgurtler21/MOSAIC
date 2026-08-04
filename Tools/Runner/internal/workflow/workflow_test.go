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
//
//   Phase group field (PhaseParsed.Group — Stage 2 additions):
//   - EXECUTION.Test.[StageNumber] → Group == "Test".
//   - EXECUTION.Implementation.[StageNumber] → Group == "Implementation".
//   - Bare EXECUTION.[StageNumber] → Group == "" (no group declared).
//   - Non-EXECUTION phase (RESEARCH) → Group == "".
//   - Grouped phase PhaseParsed.Name stays "EXECUTION".
//   - Grouped phase PhaseParsed.IsStaged == true.
//   - Grouped phase Phase literal is preserved verbatim.
//   - EXECUTION.Test (no [StageNumber]) → *domain.RefusalError (P1).
//   - EXECUTION..[StageNumber] (empty group segment) → *domain.RefusalError (P2).
//   - EXECUTION.Test.1 (stage number not bracketed) → *domain.RefusalError (P1).
//   - Malformed phase refusals name "workflow" as Component.
//   - Malformed phase refusals carry a non-empty Resource.
//
//   Approach table parsing (RoutingTable.ApproachTable — Stage 2 additions):
//   - Heading + table present → ApproachTable.Present() == true.
//   - No heading → ApproachTable.Present() == false (zero value, not an error).
//   - Row count matches the table's data rows.
//   - Approach tokens are preserved verbatim in declaration order.
//   - Group sequence for each row is decoded in column order.
//   - ApproachTable.Sequence returns the correct ordered groups for a known approach.
//   - ApproachTable.Sequence returns (nil, false) for an unknown approach.
//   - A row listing only one group parses correctly (group omitted from other rows).
//   - Three groups in a single row decode correctly.
//   - Table row order is preserved.
//   - Heading present but no table follows → *domain.RefusalError (P3).
//   - Approach table missing the "Approach" column → *domain.RefusalError (P4).
//   - Approach table missing the "Groups" column → *domain.RefusalError (P5).
//   - Approach table has header and separator but zero data rows → *domain.RefusalError (P6).
//   - Empty Approach cell → *domain.RefusalError (P7).
//   - Empty Groups cell → *domain.RefusalError (P7).
//   - Empty group token inside a Groups cell (e.g. "Test, , Impl") → *domain.RefusalError (P7).
//   - Duplicate Approach token across rows → *domain.RefusalError (P8).
//   - Duplicate group token within one Groups cell → *domain.RefusalError (P8).
//   - All approach table refusals name "workflow" as Component.
//   - All approach table refusals carry a non-empty Resource.
//   - Each malformed-phase and approach-table refusal carries a distinguishing keyword in Reason
//     (P1: "expected", P2: "group segment", P3: "heading", P4: "Approach", P5: "Groups",
//      P6: "data rows", P7: "empty", P8: "twice").
//
//   Whitespace inside group segment (untested P2 branch):
//   - EXECUTION.Te st.[StageNumber] (whitespace inside non-empty group segment) → *domain.RefusalError (P2).
//
//   Near-miss reserved heading (exact-match anchoring):
//   - A line containing the heading text but not an exact trimmed match is NOT treated as the
//     reserved heading; the workflow parses successfully with ApproachTable.Present() == false.
//
//   ApproachTable helper methods:
//   - Approaches() returns all declared approach tokens in table order.
//   - GroupNames() deduplicates groups across all rows in first-appearance order.

import (
	"errors"
	"strings"
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

// ---- Stage 2 fixtures: phase group field and approach table parsing ----

// groupedPhasesContent has a non-execution row, a bare EXECUTION row, and two
// grouped EXECUTION rows (Test and Implementation groups). No approach table.
const groupedPhasesContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| RESEARCH | agent-a | ❌ | - | out.md |
| EXECUTION.[StageNumber] | agent-b | ❌ | - | out.md |
| EXECUTION.Test.[StageNumber] | agent-c | ❌ | - | out.md |
| EXECUTION.Implementation.[StageNumber] | agent-d | ❌ | - | out.md |
`

// approachTableContent has a routing table with grouped EXECUTION rows followed
// by a **Execution Groups:** heading and a two-row approach table.
const approachTableContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.Test.[StageNumber] | agent-a | ❌ | - | out.md |
| EXECUTION.Implementation.[StageNumber] | agent-b | ❌ | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
`

// approachTableSingleGroupRowContent has approach rows that each list only one
// group, testing that absent groups are correctly omitted.
const approachTableSingleGroupRowContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.Test.[StageNumber] | agent-a | ❌ | - | out.md |
| EXECUTION.Implementation.[StageNumber] | agent-b | ❌ | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| Implementation-Only | Implementation |
| Tests-Only | Test |
`

// approachTableThreeGroupsContent has an approach row that lists three groups,
// verifying that more than two groups are decoded correctly.
const approachTableThreeGroupsContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.Alpha.[StageNumber] | agent-a | ❌ | - | out.md |
| EXECUTION.Beta.[StageNumber] | agent-b | ❌ | - | out.md |
| EXECUTION.Gamma.[StageNumber] | agent-c | ❌ | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| AllGroups | Alpha, Beta, Gamma |
`

// malformedPhaseGroupNoStageContent has EXECUTION.Test with no [StageNumber]
// segment following the group token (refusal P1).
const malformedPhaseGroupNoStageContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.Test | agent-a | ❌ | - | out.md |
`

// malformedPhaseEmptyGroupContent has EXECUTION..[StageNumber] where the group
// segment between the two dots is empty (refusal P2).
const malformedPhaseEmptyGroupContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION..[StageNumber] | agent-a | ❌ | - | out.md |
`

// malformedPhaseStageNotBracketedContent has EXECUTION.Test.1 where the segment
// after the group token does not begin with "[" (refusal P1).
const malformedPhaseStageNotBracketedContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.Test.1 | agent-a | ❌ | - | out.md |
`

// approachTableHeadingPresentNoTableContent has the reserved heading but no
// markdown table follows it (refusal P3).
const approachTableHeadingPresentNoTableContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | ❌ | - | out.md |

**Execution Groups:**

This is not a table.
`

// approachTableMissingApproachColumnContent has a heading with a table that is
// missing the required "Approach" column (refusal P4).
const approachTableMissingApproachColumnContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | ❌ | - | out.md |

**Execution Groups:**

| Groups |
|--------|
| Test, Implementation |
`

// approachTableMissingGroupsColumnContent has a heading with a table that is
// missing the required "Groups" column (refusal P5).
const approachTableMissingGroupsColumnContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | ❌ | - | out.md |

**Execution Groups:**

| Approach |
|----------|
| TDD |
`

// approachTableZeroDataRowsContent has a heading with a table header and
// separator row but no data rows (refusal P6).
const approachTableZeroDataRowsContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | ❌ | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
`

// approachTableEmptyApproachCellContent has a data row with an empty Approach
// cell (refusal P7).
const approachTableEmptyApproachCellContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | ❌ | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| | Test |
`

// approachTableEmptyGroupsCellContent has a data row with an empty Groups cell
// (refusal P7).
const approachTableEmptyGroupsCellContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | ❌ | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | |
`

// approachTableEmptyTokenInGroupsCellContent has a Groups cell with an empty
// token between commas: "Test, , Implementation" (refusal P7).
const approachTableEmptyTokenInGroupsCellContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | ❌ | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, , Implementation |
`

// approachTableDuplicateApproachContent has two rows with the same Approach
// token (refusal P8).
const approachTableDuplicateApproachContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | ❌ | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| TDD | Implementation, Test |
`

// approachTableDuplicateGroupWithinRowContent has a Groups cell that lists the
// same group token twice: "Test, Implementation, Test" (refusal P8).
const approachTableDuplicateGroupWithinRowContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | ❌ | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation, Test |
`

// ---- Stage 2 helpers ----

func mustParseGroupedPhases(t *testing.T) domain.RoutingTable {
	t.Helper()
	info := domain.WorkflowInfo{ID: "grouped-phases", Version: "1.0"}
	table, err := workflow.Parse([]byte(groupedPhasesContent), info)
	if err != nil {
		t.Fatalf("Parse(grouped-phases): unexpected error: %v", err)
	}
	return table
}

func mustParseApproachTable(t *testing.T) domain.RoutingTable {
	t.Helper()
	info := domain.WorkflowInfo{ID: "grouped-tdd", Version: "1.0"}
	table, err := workflow.Parse([]byte(approachTableContent), info)
	if err != nil {
		t.Fatalf("Parse(grouped-tdd): unexpected error: %v", err)
	}
	return table
}

// ---- Phase group field (T2.1) ----

func TestParse_GroupedPhase_Test_GroupField(t *testing.T) {
	// EXECUTION.Test.[StageNumber] must populate PhaseParsed.Group with "Test".
	table := mustParseGroupedPhases(t)

	got := table.Rows[2].PhaseParsed.Group
	if got != "Test" {
		t.Errorf("Rows[2].PhaseParsed.Group: want %q, got %q", "Test", got)
	}
}

func TestParse_GroupedPhase_Implementation_GroupField(t *testing.T) {
	// EXECUTION.Implementation.[StageNumber] must populate PhaseParsed.Group
	// with "Implementation".
	table := mustParseGroupedPhases(t)

	got := table.Rows[3].PhaseParsed.Group
	if got != "Implementation" {
		t.Errorf("Rows[3].PhaseParsed.Group: want %q, got %q", "Implementation", got)
	}
}

func TestParse_BareExecutionPhase_GroupField_IsEmpty(t *testing.T) {
	// A bare EXECUTION.[StageNumber] row (no group segment) must have an empty Group.
	table := mustParseGroupedPhases(t)

	got := table.Rows[1].PhaseParsed.Group
	if got != "" {
		t.Errorf("Rows[1].PhaseParsed.Group: want %q for bare EXECUTION phase, got %q", "", got)
	}
}

func TestParse_NonExecutionPhase_GroupField_IsEmpty(t *testing.T) {
	// A non-staged phase (RESEARCH) must have an empty Group.
	table := mustParseGroupedPhases(t)

	got := table.Rows[0].PhaseParsed.Group
	if got != "" {
		t.Errorf("Rows[0].PhaseParsed.Group: want %q for RESEARCH phase, got %q", "", got)
	}
}

func TestParse_GroupedPhase_PhaseName_StaysEXECUTION(t *testing.T) {
	// When the phase string has a group segment, PhaseParsed.Name must still
	// be "EXECUTION" — compat's staged-phase-name guard depends on this.
	table := mustParseGroupedPhases(t)

	got := table.Rows[2].PhaseParsed.Name
	if got != "EXECUTION" {
		t.Errorf("Rows[2].PhaseParsed.Name: want %q for EXECUTION.Test.[StageNumber], got %q", "EXECUTION", got)
	}
}

func TestParse_GroupedPhase_IsStaged_True(t *testing.T) {
	// A grouped EXECUTION phase must be flagged as staged.
	table := mustParseGroupedPhases(t)

	if !table.Rows[2].PhaseParsed.IsStaged {
		t.Error("Rows[2] (EXECUTION.Test.[StageNumber]): PhaseParsed.IsStaged must be true")
	}
}

func TestParse_GroupedPhase_Phase_LiteralPreserved(t *testing.T) {
	// The Phase field must carry the literal cell text, including the group segment.
	table := mustParseGroupedPhases(t)

	got := table.Rows[2].Phase
	want := "EXECUTION.Test.[StageNumber]"
	if got != want {
		t.Errorf("Rows[2].Phase: want %q, got %q", want, got)
	}
}

func TestParse_MalformedPhase_GroupWithoutStageNumber_ReturnsError(t *testing.T) {
	// EXECUTION.Test with no [StageNumber] following the group token must be refused
	// (the group segment exists but the required final segment is missing).
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseGroupNoStageContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when EXECUTION.{Group} has no [StageNumber] segment")
	}
}

func TestParse_MalformedPhase_GroupWithoutStageNumber_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseGroupNoStageContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "expected") {
		t.Errorf("RefusalError.Reason should describe expected EXECUTION phase format (P1), got %q", re.Reason)
	}
}

func TestParse_MalformedPhase_EmptyGroupSegment_ReturnsError(t *testing.T) {
	// EXECUTION..[StageNumber] with an empty segment between the two dots must
	// be refused (the group token must be a non-empty CamelCase string).
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseEmptyGroupContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the EXECUTION phase has an empty group segment")
	}
}

func TestParse_MalformedPhase_EmptyGroupSegment_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseEmptyGroupContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "group segment") {
		t.Errorf("RefusalError.Reason should mention group segment (P2), got %q", re.Reason)
	}
}

func TestParse_MalformedPhase_StageNotBracketed_ReturnsError(t *testing.T) {
	// EXECUTION.Test.1 where "1" does not begin with "[" must be refused —
	// the stage segment must always start with "[".
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseStageNotBracketedContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the stage segment does not begin with '['")
	}
}

func TestParse_MalformedPhase_StageNotBracketed_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseStageNotBracketedContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "expected") {
		t.Errorf("RefusalError.Reason should describe expected EXECUTION phase format (P1), got %q", re.Reason)
	}
}

func TestParse_MalformedPhase_RefusalError_ComponentIsWorkflow(t *testing.T) {
	// Phase-parse refusals must name "workflow" as the component.
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseGroupNoStageContent), info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
}

func TestParse_MalformedPhase_RefusalError_ResourceNamesWorkflow(t *testing.T) {
	// Phase-parse refusals must carry a non-empty Resource naming the workflow.
	info := domain.WorkflowInfo{ID: "malformed-phase-workflow", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseGroupNoStageContent), info)

	re := asRefusalError(t, err)
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

// ---- Approach table parsing (T2.2) ----

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

// ---- Approach table refusals ----

func TestParse_ApproachTable_HeadingPresentButNoTable_ReturnsError(t *testing.T) {
	// The **Execution Groups:** heading is present but no table follows it.
	// Parse must return an error rather than silently treating it as no table.
	info := domain.WorkflowInfo{ID: "heading-no-table", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableHeadingPresentNoTableContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the **Execution Groups:** heading is present but no table follows")
	}
}

func TestParse_ApproachTable_HeadingPresentButNoTable_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "heading-no-table", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableHeadingPresentNoTableContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "heading") {
		t.Errorf("RefusalError.Reason should mention the heading (P3), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_MissingApproachColumn_ReturnsError(t *testing.T) {
	// An approach table that is missing the "Approach" column must be refused.
	info := domain.WorkflowInfo{ID: "missing-approach-col", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableMissingApproachColumnContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the Execution Groups table is missing the Approach column")
	}
}

func TestParse_ApproachTable_MissingApproachColumn_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "missing-approach-col", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableMissingApproachColumnContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "Approach") {
		t.Errorf("RefusalError.Reason should name the missing Approach column (P4), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_MissingGroupsColumn_ReturnsError(t *testing.T) {
	// An approach table that is missing the "Groups" column must be refused.
	info := domain.WorkflowInfo{ID: "missing-groups-col", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableMissingGroupsColumnContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the Execution Groups table is missing the Groups column")
	}
}

func TestParse_ApproachTable_MissingGroupsColumn_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "missing-groups-col", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableMissingGroupsColumnContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "Groups") {
		t.Errorf("RefusalError.Reason should name the missing Groups column (P5), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_ZeroDataRows_ReturnsError(t *testing.T) {
	// An approach table with a valid header and separator but no data rows
	// must be refused — a zero-row table has no declared approaches.
	info := domain.WorkflowInfo{ID: "zero-approach-rows", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableZeroDataRowsContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the Execution Groups table has a header but no data rows")
	}
}

func TestParse_ApproachTable_ZeroDataRows_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "zero-approach-rows", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableZeroDataRowsContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "data rows") {
		t.Errorf("RefusalError.Reason should mention data rows (P6), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_EmptyApproachCell_ReturnsError(t *testing.T) {
	// A data row with an empty Approach cell must be refused.
	info := domain.WorkflowInfo{ID: "empty-approach-cell", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableEmptyApproachCellContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when an Approach cell is empty")
	}
}

func TestParse_ApproachTable_EmptyApproachCell_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "empty-approach-cell", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableEmptyApproachCellContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "empty") {
		t.Errorf("RefusalError.Reason should describe the empty Approach cell (P7), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_EmptyGroupsCell_ReturnsError(t *testing.T) {
	// A data row with an empty Groups cell must be refused.
	info := domain.WorkflowInfo{ID: "empty-groups-cell", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableEmptyGroupsCellContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when a Groups cell is empty")
	}
}

func TestParse_ApproachTable_EmptyGroupsCell_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "empty-groups-cell", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableEmptyGroupsCellContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "empty") {
		t.Errorf("RefusalError.Reason should describe the empty Groups cell (P7), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_EmptyTokenInGroupsCell_ReturnsError(t *testing.T) {
	// A Groups cell of "Test, , Implementation" has an empty token between the
	// commas; this must be refused.
	info := domain.WorkflowInfo{ID: "empty-token-in-groups", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableEmptyTokenInGroupsCellContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when a Groups cell contains an empty token")
	}
}

func TestParse_ApproachTable_EmptyTokenInGroupsCell_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "empty-token-in-groups", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableEmptyTokenInGroupsCellContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "empty") {
		t.Errorf("RefusalError.Reason should describe the empty group token (P7), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_DuplicateApproach_ReturnsError(t *testing.T) {
	// Two rows with the same Approach token must be refused.
	info := domain.WorkflowInfo{ID: "duplicate-approach", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableDuplicateApproachContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the same Approach token appears in two rows")
	}
}

func TestParse_ApproachTable_DuplicateApproach_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "duplicate-approach", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableDuplicateApproachContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "twice") {
		t.Errorf("RefusalError.Reason should mention the duplicate declared twice (P8), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_DuplicateGroupWithinRow_ReturnsError(t *testing.T) {
	// A group token listed twice in the same Groups cell must be refused.
	info := domain.WorkflowInfo{ID: "duplicate-group-in-row", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableDuplicateGroupWithinRowContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when a group token appears twice in one Groups cell")
	}
}

func TestParse_ApproachTable_DuplicateGroupWithinRow_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "duplicate-group-in-row", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableDuplicateGroupWithinRowContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "twice") {
		t.Errorf("RefusalError.Reason should mention the duplicate declared twice (P8), got %q", re.Reason)
	}
}

func TestParse_ApproachTable_Refusal_ComponentIsWorkflow(t *testing.T) {
	// Every approach table refusal must name "workflow" as the component.
	info := domain.WorkflowInfo{ID: "missing-approach-col", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableMissingApproachColumnContent), info)

	re := asRefusalError(t, err)
	if re.Component != "workflow" {
		t.Errorf("RefusalError.Component: want %q, got %q", "workflow", re.Component)
	}
}

func TestParse_ApproachTable_Refusal_ResourceNamesWorkflow(t *testing.T) {
	// Every approach table refusal must carry a non-empty Resource identifying
	// the workflow so the user can find the problematic file.
	info := domain.WorkflowInfo{ID: "my-workflow-id", Version: "1.0"}

	_, err := workflow.Parse([]byte(approachTableMissingApproachColumnContent), info)

	re := asRefusalError(t, err)
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
}

// ---- Whitespace-only (non-empty) group segment (untested P2 branch) ----

// malformedPhaseWhitespaceGroupContent has EXECUTION.Te st.[StageNumber] where
// the group segment contains internal whitespace. This exercises the "whitespace"
// branch of the P2 rule, distinct from the empty-segment case (EXECUTION..[StageNumber]).
// Per the algorithm contract: "the segment before the first '.' is empty after
// trimming, or contains whitespace → error."
const malformedPhaseWhitespaceGroupContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.Te st.[StageNumber] | agent-a | ❌ | - | out.md |
`

func TestParse_MalformedPhase_WhitespaceGroupSegment_ReturnsError(t *testing.T) {
	// A group segment that contains whitespace (non-empty but invalid) must be
	// refused. This is the whitespace branch of P2: the segment is non-empty but
	// fails because it contains an internal space, unlike the empty-segment case.
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseWhitespaceGroupContent), info)

	if err == nil {
		t.Fatal("Parse must return an error when the EXECUTION phase group segment contains whitespace")
	}
}

func TestParse_MalformedPhase_WhitespaceGroupSegment_ReturnsRefusalError(t *testing.T) {
	info := domain.WorkflowInfo{ID: "malformed-phase", Version: "1.0"}

	_, err := workflow.Parse([]byte(malformedPhaseWhitespaceGroupContent), info)

	re := asRefusalError(t, err)
	if !strings.Contains(re.Reason, "group segment") {
		t.Errorf("RefusalError.Reason should mention group segment (P2), got %q", re.Reason)
	}
}

// ---- Near-miss reserved heading (exact-match anchoring) ----

// approachTableNearMissHeadingContent has a line that contains the reserved
// heading text but is not an exact trimmed full-line match (trailing text appended).
// The approach table that follows this near-miss line must NOT be decoded.
const approachTableNearMissHeadingContent = `<!-- workflow-version: 1.0 -->
| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | ❌ | - | out.md |

**Execution Groups:** and more text

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
`

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
