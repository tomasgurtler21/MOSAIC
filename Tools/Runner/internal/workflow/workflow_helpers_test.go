package workflow_test

// Tests for workflow.Parse.
//
// Coverage:
//   Happy path – quick-fix workflow (two PLANNING rows, one EXECUTION, one REVIEW):
//   - Returns the correct number of rows.
//   - Row indices are zero-based and consecutive in declaration order.
//   - HITL column decoded: TRUE → true, FALSE → false.
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
// quickFixContent is the raw bytes of the <Workflow type="core" name="quick-fix" version="3.0">
// region from Workflows/Build/quick-fix.md (version 3.0). The content is the bytes
// between the boundary tags, not including the tags themselves. The version is
// supplied separately via WorkflowInfo when calling workflow.Parse.
const quickFixContent = `## Quick Fix Workflow

**Use when:** Small changes, bug fixes, or well-understood modifications. Skips research and design.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | implementation-tdd | planner-tdd-soft | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | test-runner | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |

**Notes:**
- Single-stage plans use Stage-1/ folder for consistency (Decision 15)
`

// brownfieldContent is the raw bytes of the <Workflow type="core" name="brownfield-tdd-build-verified" version="2.0">
// region from Workflows/Build/brownfield-tdd-build-verified.md (version 2.0).
const brownfieldContent = `## Brownfield TDD Build-Verified Workflow

**Use when:** New features or significant changes to an **existing codebase** requiring test-first development where **compilation/build cannot be verified via standard terminal tools**.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | FALSE | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.[StageNumber] | test-writer-tdd | FALSE | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | build-review | FALSE | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-tests.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | build-review | FALSE | implementation-review | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.[StageNumber] | implementation-review | FALSE | COMPLETE | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-impl.md | Stage-{StageNumber}/implementation-review.md |
`

// implOnlyContent is the raw bytes of the <Workflow type="core" name="implementation-only" version="3.1">
// region from Workflows/Build/implementation-only.md (version 3.1). This
// workflow starts with EXECUTION rows (no pre-EXECUTION rows).
const implOnlyContent = `## Implementation Only Workflow

**Use when:** Research, planning, and design already complete.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |
`

// minimalContent is a minimal routing table with only required columns.
// Used to test that optional columns are absent (ColumnPresent == false).
const minimalContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | planner | TRUE | - | Plan.md |
| EXECUTION.[StageNumber] | implementer | FALSE | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
`

// emptyTableContent has a header and separator row but no data rows.
const emptyTableContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
`

// onSuccessDashContent is a routing table where the On Success cell is "-",
// used to verify the symmetric no-hint behavior for that optional column.
// KC-8/FR-27 requires that "-" in any optional column means no hint, yielding
// OptionalHint{ColumnPresent: true, Value: ""}.
const onSuccessDashContent = `| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | TRUE | - | next-step | - | Plan.md |
`

// outputDashContent is a routing table where the Output cell is "-",
// used to verify that "-" in the Output column produces an empty OutputArtifacts slice,
// symmetric with the Input column behavior tested in TestParse_QuickFix_DashInput_ProducesEmptyArtifacts.
const outputDashContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | planner | TRUE | Plan.md | - |
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

// ---- Stage 2 fixtures: phase group field and approach table parsing ----

// groupedPhasesContent has a non-execution row, a bare EXECUTION row, and two
// grouped EXECUTION rows (Test and Implementation groups). No approach table.
const groupedPhasesContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| RESEARCH | agent-a | FALSE | - | out.md |
| EXECUTION.[StageNumber] | agent-b | FALSE | - | out.md |
| EXECUTION.Test.[StageNumber] | agent-c | FALSE | - | out.md |
| EXECUTION.Implementation.[StageNumber] | agent-d | FALSE | - | out.md |
`

// approachTableContent has a routing table with grouped EXECUTION rows followed
// by a **Execution Groups:** heading and a two-row approach table.
const approachTableContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.Test.[StageNumber] | agent-a | FALSE | - | out.md |
| EXECUTION.Implementation.[StageNumber] | agent-b | FALSE | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
`

// approachTableSingleGroupRowContent has approach rows that each list only one
// group, testing that absent groups are correctly omitted.
const approachTableSingleGroupRowContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.Test.[StageNumber] | agent-a | FALSE | - | out.md |
| EXECUTION.Implementation.[StageNumber] | agent-b | FALSE | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| Implementation-Only | Implementation |
| Tests-Only | Test |
`

// approachTableThreeGroupsContent has an approach row that lists three groups,
// verifying that more than two groups are decoded correctly.
const approachTableThreeGroupsContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.Alpha.[StageNumber] | agent-a | FALSE | - | out.md |
| EXECUTION.Beta.[StageNumber] | agent-b | FALSE | - | out.md |
| EXECUTION.Gamma.[StageNumber] | agent-c | FALSE | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| AllGroups | Alpha, Beta, Gamma |
`

// malformedPhaseGroupNoStageContent has EXECUTION.Test with no [StageNumber]
// segment following the group token (refusal P1).
const malformedPhaseGroupNoStageContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.Test | agent-a | FALSE | - | out.md |
`

// malformedPhaseEmptyGroupContent has EXECUTION..[StageNumber] where the group
// segment between the two dots is empty (refusal P2).
const malformedPhaseEmptyGroupContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION..[StageNumber] | agent-a | FALSE | - | out.md |
`

// malformedPhaseStageNotBracketedContent has EXECUTION.Test.1 where the segment
// after the group token does not begin with "[" (refusal P1).
const malformedPhaseStageNotBracketedContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.Test.1 | agent-a | FALSE | - | out.md |
`

// approachTableHeadingPresentNoTableContent has the reserved heading but no
// markdown table follows it (refusal P3).
const approachTableHeadingPresentNoTableContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | - | out.md |

**Execution Groups:**

This is not a table.
`

// approachTableMissingApproachColumnContent has a heading with a table that is
// missing the required "Approach" column (refusal P4).
const approachTableMissingApproachColumnContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | - | out.md |

**Execution Groups:**

| Groups |
|--------|
| Test, Implementation |
`

// approachTableMissingGroupsColumnContent has a heading with a table that is
// missing the required "Groups" column (refusal P5).
const approachTableMissingGroupsColumnContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | - | out.md |

**Execution Groups:**

| Approach |
|----------|
| TDD |
`

// approachTableZeroDataRowsContent has a heading with a table header and
// separator row but no data rows (refusal P6).
const approachTableZeroDataRowsContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
`

// approachTableEmptyApproachCellContent has a data row with an empty Approach
// cell (refusal P7).
const approachTableEmptyApproachCellContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| | Test |
`

// approachTableEmptyGroupsCellContent has a data row with an empty Groups cell
// (refusal P7).
const approachTableEmptyGroupsCellContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | |
`

// approachTableEmptyTokenInGroupsCellContent has a Groups cell with an empty
// token between commas: "Test, , Implementation" (refusal P7).
const approachTableEmptyTokenInGroupsCellContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, , Implementation |
`

// approachTableDuplicateApproachContent has two rows with the same Approach
// token (refusal P8).
const approachTableDuplicateApproachContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | - | out.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| TDD | Implementation, Test |
`

// approachTableDuplicateGroupWithinRowContent has a Groups cell that lists the
// same group token twice: "Test, Implementation, Test" (refusal P8).
const approachTableDuplicateGroupWithinRowContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | - | out.md |

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

// ---- Whitespace-only (non-empty) group segment (untested P2 branch) ----

// malformedPhaseWhitespaceGroupContent has EXECUTION.Te st.[StageNumber] where
// the group segment contains internal whitespace. This exercises the "whitespace"
// branch of the P2 rule, distinct from the empty-segment case (EXECUTION..[StageNumber]).
// Per the algorithm contract: "the segment before the first '.' is empty after
// trimming, or contains whitespace → error."
const malformedPhaseWhitespaceGroupContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.Te st.[StageNumber] | agent-a | FALSE | - | out.md |
`

// ---- Near-miss reserved heading (exact-match anchoring) ----

// approachTableNearMissHeadingContent has a line that contains the reserved
// heading text but is not an exact trimmed full-line match (trailing text appended).
// The approach table that follows this near-miss line must NOT be decoded.
const approachTableNearMissHeadingContent = `| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | - | out.md |

**Execution Groups:** and more text

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
`
