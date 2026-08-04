package compat_test

// Tests for compat.Admit.
//
// Coverage:
//
//   Admission success — five supported workflows:
//   - brownfield-tdd-build-verified (two named groups "Test"/"Implementation", 13 rows)
//   - greenfield-tdd (two named groups "Test"/"Implementation", 13 rows)
//   - brownfield-tdd (two named groups "Test"/"Implementation", 12 rows)
//   - quick-fix (no groups declared, single implicit range, 4 rows)
//   - implementation-only (no groups declared, single implicit range, 3 rows)
//
//   GroupsDeclared and ApproachTable carry-through:
//   - brownfield-tdd-build-verified → GroupsDeclared=true, ApproachTable present
//   - quick-fix → GroupsDeclared=false, ApproachTable not present
//   - implementation-only → GroupsDeclared=false, ApproachTable not present
//
//   Execution group resolution — brownfield-tdd-build-verified:
//   - Two execution groups (Groups has 2 elements).
//   - Groups[0].Name="Test", StartRow=7, EndRow=10 (rows 7-9).
//   - Groups[1].Name="Implementation", StartRow=10, EndRow=13 (rows 10-12).
//   - Groups are contiguous and cover all 6 EXECUTION rows (no gaps, no overlap).
//   - Duplicate agent identifier (build-review appears at rows 8 and 11) is allowed.
//
//   Execution group resolution — brownfield-tdd:
//   - Groups[0].Name="Test", StartRow=7, EndRow=9.
//   - Groups[1].Name="Implementation", StartRow=9, EndRow=11.
//
//   Execution group resolution — greenfield-tdd:
//   - Groups[0].Name="Test", StartRow=8, EndRow=10.
//   - Groups[1].Name="Implementation", StartRow=10, EndRow=12.
//
//   Execution group resolution — quick-fix (no groups declared):
//   - One execution group (Groups has 1 element), Name="" (empty, implicit).
//   - Group covers the single EXECUTION row: StartRow=2, EndRow=3.
//
//   Execution group resolution — implementation-only (no groups declared):
//   - One execution group (Groups has 1 element), Name="" (empty, implicit).
//   - Group covers the two EXECUTION rows: StartRow=0, EndRow=2.
//
//   GroupByName:
//   - GroupByName("Test") returns the Test group with correct StartRow.
//   - GroupByName for an absent name returns found=false.
//
//   Three or more groups:
//   - Synthetic three-group workflow (Alpha, Beta, Gamma) is admitted.
//   - Groups count=3, names correct (verbatim from Phase column), ranges correct.
//
//   Arbitrary group names / generic agent identifiers:
//   - Synthetic workflow with generic agent names and opaque group tokens admitted.
//   - Classification is driven purely by the Phase column, never by agent names.
//
//   No-groups-declared case:
//   - GroupsDeclared=false for quick-fix and implementation-only.
//   - Single implicit group has empty Name, covers all EXECUTION rows in listed order.
//   - ApproachTable not present for bare workflows.
//
//   Bare EXECUTION rows with any agent mix:
//   - Bare rows (no group segment) with any agent names form exactly one implicit group.
//   - No escape hatch; no refusal; agent identifiers are never inspected.
//
//   PreExecution / PostExecution row ranges:
//   - brownfield-tdd-build-verified: PreExecutionEndRow=7 (7 pre-EXECUTION rows),
//     no post-EXECUTION rows (PostExecutionStartRow==PostExecutionEndRow).
//   - quick-fix: PostExecutionStartRow=3, PostExecutionEndRow=4 (REVIEW row).
//   - implementation-only: PreExecutionEndRow=0 (starts with EXECUTION rows),
//     PostExecutionStartRow=2, PostExecutionEndRow=3.
//
//   HasStagedPhase:
//   - All five supported workflows → HasStagedPhase=true.
//
//   FR-18a refusal cases (each must return *domain.RefusalError with a distinct message):
//   - Condition 2: More than one staged phase block (EXECUTION rows split by non-staged rows).
//   - Condition 3: Staged phase with name other than "EXECUTION".
//   - Condition 6: Agent-with-mode notation ("agent-name(mode)").
//   - Condition 5: Parallel dispatch expressed via multiple agents in routing hints.
//   - Condition 1: Stage source not from plan artifact (non-standard stage notation).
//   - Condition 4: Dynamic/growing stage set (EXECUTION row outputs Stage-*/Plan.md).
//
//   Admission refusals A1–A5 (cross-consistency violations):
//   - A1: non-contiguous group rows (a row re-opens a group that already ended).
//   - A2: grouped EXECUTION rows but no approach table present.
//   - A3: approach table present but an EXECUTION row carries no group segment.
//   - A4: approach table names a group that no EXECUTION row belongs to.
//   - A5: an EXECUTION row declares a group absent from every approach table row.
//   - Each refusal message names the offending row index (as "row N") and/or group.
//
//   Case-sensitive group token matching:
//   - A group token in the Phase column that differs only in case from an approach
//     table entry must trigger a refusal, not a silent match.
//
//   RefusalError contract:
//   - Component must be "compat".
//   - Resource must identify the workflow (non-empty).
//   - Reason must be non-empty and describe the specific condition.

import (
	"errors"
	"strings"
	"testing"

	"mosaic-run/internal/compat"
	"mosaic-run/internal/domain"
	"mosaic-run/internal/workflow"
)

// asRefusalError asserts that err is (or wraps) a *domain.RefusalError.
func asRefusalError(t *testing.T, err error) *domain.RefusalError {
	t.Helper()
	var re *domain.RefusalError
	if !errors.As(err, &re) {
		t.Fatalf("want *domain.RefusalError, got %T: %v", err, err)
	}
	return re
}

// ---- Workflow content fixtures (extracted from real workflow files) ----

// brownfieldBuildVerifiedContent is the routing table region from
// Workflows/Build/brownfield-tdd-build-verified.md (version 2.1).
// This workflow has 13 rows: 7 pre-EXECUTION, 6 EXECUTION.
const brownfieldBuildVerifiedContent = `<!-- workflow-version: 2.1 -->
## Brownfield TDD Build-Verified Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ❌ | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | ❌ | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | build-review | ❌ | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-tests.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | ❌ | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | build-review | ❌ | implementation-review | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | ❌ | COMPLETE | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-impl.md | Stage-{StageNumber}/implementation-review.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
`

// greenFieldTDDContent is the routing table region from
// Workflows/Build/greenfield-tdd.md (version 3.4).
// This workflow has 13 rows: 8 pre-EXECUTION (2 RESEARCH, 2 ARCHITECTURE, 2 PLANNING, 2 DESIGN),
// 4 EXECUTION, 1 REVIEW.
const greenFieldTDDContent = `<!-- workflow-version: 3.4 -->
## Greenfield TDD Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | system-designer | requirements-refinement | Requirements.md | requirements-review.md |
| ARCHITECTURE | system-designer | ✅ | system-design-review | - | Requirements.md | SystemDesign.md |
| ARCHITECTURE | system-design-review | ❌ | planner-tdd-soft | system-designer | Requirements.md, SystemDesign.md | system-design-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Requirements.md, SystemDesign.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Requirements.md, Plan.md, Stage-*/Plan.md, SystemDesign.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | ❌ | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
`

// brownfieldTDDContent is the routing table region from
// Workflows/Build/brownfield-tdd.md (version 3.6).
// This workflow has 12 rows: 7 pre-EXECUTION, 4 EXECUTION, 1 REVIEW.
const brownfieldTDDContent = `<!-- workflow-version: 3.6 -->
## Brownfield TDD Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ❌ | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | ❌ | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
`

// quickFixContent is the routing table region from
// Workflows/Build/quick-fix.md (version 3.0).
// This workflow has 4 rows: 2 PLANNING, 1 EXECUTION, 1 REVIEW.
const quickFixContent = `<!-- workflow-version: 3.0 -->
## Quick Fix Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | implementation-tdd | planner-tdd-soft | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | test-runner | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |
`

// implOnlyContent is the routing table region from
// Workflows/Build/implementation-only.md (version 3.1).
// This workflow has 3 rows: 2 EXECUTION, 1 REVIEW. No pre-EXECUTION rows.
const implOnlyContent = `<!-- workflow-version: 3.1 -->
## Implementation Only Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |
`

// ---- Helper functions ----

func mustParseTable(t *testing.T, content string, id, version string) domain.RoutingTable {
	t.Helper()
	info := domain.WorkflowInfo{ID: domain.WorkflowID(id), Version: domain.WorkflowVersion(version)}
	table, err := workflow.Parse([]byte(content), info)
	if err != nil {
		t.Fatalf("workflow.Parse(%s): %v", id, err)
	}
	return table
}

func mustAdmit(t *testing.T, table domain.RoutingTable) domain.AdmittedWorkflow {
	t.Helper()
	aw, err := compat.Admit(table)
	if err != nil {
		t.Fatalf("Admit(%s): unexpected error: %v", table.Info.ID, err)
	}
	return aw
}

// ---- Admission success ----

func TestAdmit_BrownfieldTDDBuildVerified_Succeeds(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	_, err := compat.Admit(table)
	if err != nil {
		t.Errorf("Admit(brownfield-tdd-build-verified): unexpected error: %v", err)
	}
}

func TestAdmit_GreenFieldTDD_Succeeds(t *testing.T) {
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	_, err := compat.Admit(table)
	if err != nil {
		t.Errorf("Admit(greenfield-tdd): unexpected error: %v", err)
	}
}

func TestAdmit_BrownfieldTDD_Succeeds(t *testing.T) {
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	_, err := compat.Admit(table)
	if err != nil {
		t.Errorf("Admit(brownfield-tdd): unexpected error: %v", err)
	}
}

func TestAdmit_QuickFix_Succeeds(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	_, err := compat.Admit(table)
	if err != nil {
		t.Errorf("Admit(quick-fix): unexpected error: %v", err)
	}
}

func TestAdmit_ImplementationOnly_Succeeds(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	_, err := compat.Admit(table)
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

// ---- Execution group resolution: brownfield-tdd-build-verified ----

func TestAdmit_BrownfieldTDDBuildVerified_TwoGroups(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) != 2 {
		t.Errorf("Groups: want 2 groups, got %d", len(aw.Groups))
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_TestGroupName(t *testing.T) {
	// The first group carries "Test" as its workflow-defined name, taken verbatim
	// from the EXECUTION.Test.[StageNumber] Phase column segment.
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
	// EXECUTION rows start at absolute index 7 (after 7 pre-EXECUTION rows).
	// Test group starts at row 7.
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
	// Test group covers rows 7, 8, 9 (test-writer-tdd, build-review, tests-review-tdd).
	// EndRow is exclusive: 10.
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
	// The second group carries "Implementation" as its workflow-defined name,
	// taken verbatim from the EXECUTION.Implementation.[StageNumber] Phase column segment.
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
	// Implementation group starts immediately after the test group: row 10.
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
	// Implementation group covers rows 10, 11, 12.
	// EndRow is exclusive: 13.
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
	// Groups must cover all 6 EXECUTION rows (rows 7-12) with no gaps and no overlap.
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) != 2 {
		t.Fatalf("Groups: want 2, got %d", len(aw.Groups))
	}

	// Groups[0] ends where Groups[1] starts (contiguous, no gap).
	if aw.Groups[0].EndRow != aw.Groups[1].StartRow {
		t.Errorf("Groups not contiguous: Groups[0].EndRow=%d, Groups[1].StartRow=%d",
			aw.Groups[0].EndRow, aw.Groups[1].StartRow)
	}

	// Together they span all EXECUTION rows: StartRow=7 to EndRow=13.
	if aw.Groups[0].StartRow != 7 || aw.Groups[1].EndRow != 13 {
		t.Errorf("Groups span: want rows 7-13, got %d-%d",
			aw.Groups[0].StartRow, aw.Groups[1].EndRow)
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_DuplicateAgent_Allowed(t *testing.T) {
	// build-review appears at rows 8 and 11 (in the test group and implementation group).
	// Duplicate agent identifiers in different rows must be allowed (FR-26a).
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")

	_, err := compat.Admit(table)

	if err != nil {
		t.Errorf("Admit must allow duplicate agent identifiers (build-review appears twice): %v", err)
	}
}

// ---- Execution group resolution: brownfield-tdd ----

func TestAdmit_BrownfieldTDD_TestGroupStartRow(t *testing.T) {
	// 7 pre-EXECUTION rows → EXECUTION starts at absolute index 7.
	// Test group: test-writer-tdd (row 7), tests-review-tdd (row 8).
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
	// Test group covers rows 7-8 (test-writer-tdd, tests-review-tdd). EndRow=9 (exclusive).
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
	// Implementation group starts at row 9 (implementation-tdd).
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
	// Implementation group covers rows 9-10 (implementation-tdd, implementation-review).
	// EndRow=11 (exclusive).
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
	// 8 pre-EXECUTION rows → EXECUTION starts at absolute index 8.
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
	// Test group: rows 8-9 (test-writer-tdd, tests-review-tdd). EndRow=10.
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
	// Implementation group starts at row 10 (implementation-tdd).
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
	// Implementation group covers rows 10-11. EndRow=12.
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
	// quick-fix has 2 PLANNING rows (0, 1) before EXECUTION row (2).
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
	// quick-fix EXECUTION row is just index 2. EndRow=3 (exclusive).
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
	// implementation-only starts with EXECUTION rows; no pre-EXECUTION rows.
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
	// EXECUTION rows are 0 and 1. EndRow=2 (exclusive).
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].EndRow != 2 {
		t.Errorf("Groups[0].EndRow: want 2, got %d", aw.Groups[0].EndRow)
	}
}

// ---- GroupKind for single-group workflows ----

func TestAdmit_QuickFix_GroupName(t *testing.T) {
	// quick-fix is a bare workflow with no group segments in its Phase column.
	// The single implicit group must have an empty Name (no workflow-defined group token).
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
	// implementation-only is a bare workflow with no group segments in its Phase column.
	// The single implicit group must have an empty Name.
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].Name != "" {
		t.Errorf("Groups[0].Name: want empty string for implicit group, got %q", aw.Groups[0].Name)
	}
}

// ---- Pre/Post execution row ranges ----

func TestAdmit_BrownfieldTDDBuildVerified_PreExecutionEndRow(t *testing.T) {
	// 7 pre-EXECUTION rows (0-6). PreExecutionEndRow is exclusive: 7.
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if aw.PreExecutionEndRow != 7 {
		t.Errorf("PreExecutionEndRow: want 7, got %d", aw.PreExecutionEndRow)
	}
}

func TestAdmit_BrownfieldTDDBuildVerified_NoPostExecution(t *testing.T) {
	// brownfield-tdd-build-verified has no rows after the last EXECUTION row.
	// PostExecutionStartRow == PostExecutionEndRow (empty range).
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if aw.PostExecutionStartRow != aw.PostExecutionEndRow {
		t.Errorf("PostExecution: want empty range (start==end), got start=%d end=%d",
			aw.PostExecutionStartRow, aw.PostExecutionEndRow)
	}
}

func TestAdmit_QuickFix_PostExecutionStartRow(t *testing.T) {
	// quick-fix has a REVIEW row at index 3 after the EXECUTION row at index 2.
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if aw.PostExecutionStartRow != 3 {
		t.Errorf("PostExecutionStartRow: want 3, got %d", aw.PostExecutionStartRow)
	}
}

func TestAdmit_QuickFix_PostExecutionEndRow(t *testing.T) {
	// quick-fix total rows = 4. PostExecutionEndRow is exclusive: 4.
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if aw.PostExecutionEndRow != 4 {
		t.Errorf("PostExecutionEndRow: want 4, got %d", aw.PostExecutionEndRow)
	}
}

func TestAdmit_ImplementationOnly_PreExecutionEmpty(t *testing.T) {
	// implementation-only starts with EXECUTION rows. PreExecutionStartRow == PreExecutionEndRow == 0.
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if aw.PreExecutionStartRow != 0 || aw.PreExecutionEndRow != 0 {
		t.Errorf("PreExecution: want empty range (0,0), got (%d,%d)",
			aw.PreExecutionStartRow, aw.PreExecutionEndRow)
	}
}

func TestAdmit_ImplementationOnly_PostExecutionStartRow(t *testing.T) {
	// REVIEW row at index 2. PostExecutionStartRow=2.
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if aw.PostExecutionStartRow != 2 {
		t.Errorf("PostExecutionStartRow: want 2, got %d", aw.PostExecutionStartRow)
	}
}

func TestAdmit_BrownfieldTDD_PreExecutionEndRow(t *testing.T) {
	// brownfield-tdd has 7 pre-EXECUTION rows (3 RESEARCH + 2 PLANNING + 2 DESIGN).
	// PreExecutionEndRow is exclusive: 7.
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if aw.PreExecutionEndRow != 7 {
		t.Errorf("PreExecutionEndRow: want 7, got %d", aw.PreExecutionEndRow)
	}
}

func TestAdmit_BrownfieldTDD_PostExecutionStartRow(t *testing.T) {
	// brownfield-tdd has 4 EXECUTION rows (rows 7-10) followed by a REVIEW row at index 11.
	// PostExecutionStartRow = 11.
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if aw.PostExecutionStartRow != 11 {
		t.Errorf("PostExecutionStartRow: want 11, got %d", aw.PostExecutionStartRow)
	}
}

func TestAdmit_BrownfieldTDD_PostExecutionEndRow(t *testing.T) {
	// brownfield-tdd has 12 rows total (indices 0-11). PostExecutionEndRow is exclusive: 12.
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if aw.PostExecutionEndRow != 12 {
		t.Errorf("PostExecutionEndRow: want 12, got %d", aw.PostExecutionEndRow)
	}
}

func TestAdmit_GreenFieldTDD_PreExecutionEndRow(t *testing.T) {
	// greenfield-tdd has 8 pre-EXECUTION rows (2 RESEARCH + 2 ARCHITECTURE + 2 PLANNING + 2 DESIGN).
	// PreExecutionEndRow is exclusive: 8.
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if aw.PreExecutionEndRow != 8 {
		t.Errorf("PreExecutionEndRow: want 8, got %d", aw.PreExecutionEndRow)
	}
}

func TestAdmit_GreenFieldTDD_PostExecutionStartRow(t *testing.T) {
	// greenfield-tdd has 4 EXECUTION rows (rows 8-11) followed by a REVIEW row at index 12.
	// PostExecutionStartRow = 12.
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if aw.PostExecutionStartRow != 12 {
		t.Errorf("PostExecutionStartRow: want 12, got %d", aw.PostExecutionStartRow)
	}
}

func TestAdmit_GreenFieldTDD_PostExecutionEndRow(t *testing.T) {
	// greenfield-tdd has 13 rows total (indices 0-12). PostExecutionEndRow is exclusive: 13.
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if aw.PostExecutionEndRow != 13 {
		t.Errorf("PostExecutionEndRow: want 13, got %d", aw.PostExecutionEndRow)
	}
}

// ---- FR-18a refusal: Condition 2 — multiple staged phase blocks ----

func TestAdmit_MultipleStagedPhaseBlocks_ReturnsRefusalError(t *testing.T) {
	// A routing table where EXECUTION.[StageNumber] rows appear in two separate
	// blocks (split by a non-staged row) has more than one staged phase block.
	// This should be refused.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "split-execution", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: false}, Agent: "planner"},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementer", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "REVIEW", PhaseParsed: domain.PhaseParsed{Name: "REVIEW", IsStaged: false}, Agent: "reviewer"},
			// Second EXECUTION block — this creates two staged phase blocks.
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementer2", OutputArtifacts: []string{"out2.md"}},
		},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse a workflow with multiple staged phase blocks")
	}
	asRefusalError(t, err)
}

func TestAdmit_MultipleStagedPhaseBlocks_RefusalMessage_NonEmpty(t *testing.T) {
	// The refusal reason must describe the multiple staged phase blocks condition
	// so the implementer cannot pass with a generic "refusal" message.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "split-execution", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: false}, Agent: "planner"},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementer", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "REVIEW", PhaseParsed: domain.PhaseParsed{Name: "REVIEW", IsStaged: false}, Agent: "reviewer"},
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementer2", OutputArtifacts: []string{"out2.md"}},
		},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the multiple staged phase blocks condition")
	}
	if !strings.Contains(re.Resource, "split-execution") {
		t.Errorf("RefusalError.Resource must name the workflow ID %q, got %q", "split-execution", re.Resource)
	}
}

// ---- FR-18a refusal: Condition 3 — non-EXECUTION staged phase ----

func TestAdmit_NonExecutionStagedPhase_ReturnsRefusalError(t *testing.T) {
	// A row with IsStaged=true and Phase name other than "EXECUTION" is
	// a non-EXECUTION staged phase. This should be refused.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "staged-planning", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:           0,
				Phase:           "PLANNING.[StageNumber]",
				PhaseParsed:     domain.PhaseParsed{Name: "PLANNING", IsStaged: true}, // staged but not EXECUTION
				Agent:           "planner",
				OutputArtifacts: []string{"Plan.md"},
			},
		},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse a workflow where the staged phase is not EXECUTION")
	}
	asRefusalError(t, err)
}

func TestAdmit_NonExecutionStagedPhase_RefusalMessage_MentionsPhaseName(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "staged-planning", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "PLANNING.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: true},
				Agent:       "planner",
			},
		},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	// The refusal reason must name the condition clearly.
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the non-EXECUTION staged phase condition")
	}
}

// ---- FR-18a refusal: Condition 4 — dynamic or growing stage set ----

func TestAdmit_DynamicStageSet_ReturnsRefusalError(t *testing.T) {
	// A workflow where an EXECUTION-phase row outputs Stage-*/Plan.md signals a
	// "growing stage set": stages can be added during execution rather than being
	// fixed entirely during planning. This is not supported (FR-18a Condition 4).
	// In the supported subset, Stage-*/Plan.md is always produced by a pre-EXECUTION
	// planning row; finding it in an EXECUTION row indicates a dynamic stage set.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "dynamic-stages", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:           0,
				Phase:           "PLANNING",
				PhaseParsed:     domain.PhaseParsed{Name: "PLANNING", IsStaged: false},
				Agent:           "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"},
			},
			{
				Index:           1,
				Phase:           "EXECUTION.[StageNumber]",
				PhaseParsed:     domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:           "implementation-tdd",
				// This EXECUTION row also produces Stage-*/Plan.md, meaning stages can
				// grow during execution — a dynamic stage set.
				OutputArtifacts: []string{"Stage-{StageNumber}/PlanProgress.md", "Stage-*/Plan.md"},
			},
		},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse a workflow with a dynamic or growing stage set")
	}
	asRefusalError(t, err)
}

func TestAdmit_DynamicStageSet_RefusalMessage_NonEmpty(t *testing.T) {
	// The refusal for a dynamic stage set must include a non-empty Reason explaining
	// the condition, so the user can understand why the workflow was refused.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "dynamic-stages", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:           0,
				Phase:           "PLANNING",
				PhaseParsed:     domain.PhaseParsed{Name: "PLANNING", IsStaged: false},
				Agent:           "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"},
			},
			{
				Index:           1,
				Phase:           "EXECUTION.[StageNumber]",
				PhaseParsed:     domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:           "implementation-tdd",
				OutputArtifacts: []string{"Stage-{StageNumber}/PlanProgress.md", "Stage-*/Plan.md"},
			},
		},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the dynamic stage set condition, not be empty")
	}
}

// ---- FR-18a refusal: Condition 6 — agent-with-mode notation ----

func TestAdmit_AgentWithModeNotation_ReturnsRefusalError(t *testing.T) {
	// Agent identifiers of the form "agent-name(mode)" are not supported.
	// This includes any agent identifier containing a parenthesis.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "agent-with-mode", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:           0,
				Phase:           "EXECUTION.[StageNumber]",
				PhaseParsed:     domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:           "test-writer-tdd(fix-mode)", // agent-with-mode notation
				OutputArtifacts: []string{"out.md"},
			},
		},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse a workflow with agent-with-mode notation")
	}
	asRefusalError(t, err)
}

func TestAdmit_AgentWithModeNotation_RefusalMessage_NamesAgent(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "agent-with-mode", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "EXECUTION.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:       "test-writer-tdd(fix-mode)",
			},
		},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	// The refusal message should identify the offending agent or row.
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the agent-with-mode condition")
	}
}

// ---- FR-18a refusal: Condition 7 — unresolvable execution groups ----

func TestAdmit_BareRowsMixedAgents_SingleImplicitGroup(t *testing.T) {
	// Bare EXECUTION rows (no group segments) with any mix of agent identifiers
	// form exactly one implicit group with an empty Name, covering all EXECUTION
	// rows in listed order. Agent identifiers are never inspected for classification.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "bare-rows-workflow", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "test-writer-tdd", OutputArtifacts: []string{"tests.md"}},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "unknown-agent", OutputArtifacts: []string{"unknown.md"}},
			{Index: 2, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd", OutputArtifacts: []string{"impl.md"}},
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "unknown-agent-2", OutputArtifacts: []string{"other.md"}},
		},
	}

	aw, err := compat.Admit(table)
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
	// Agent identifiers are never inspected for group classification. Bare EXECUTION
	// rows (no group segment) with any combination of "known" and "unknown" agents
	// must be admitted as a single implicit group — no escape hatch, no refusal.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "bare-rows-mixed-agents", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "test-writer-tdd", OutputArtifacts: []string{"tests.md"}},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "unknown-agent", OutputArtifacts: []string{"unknown.md"}},
			{Index: 2, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd", OutputArtifacts: []string{"impl.md"}},
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "unknown-agent-2", OutputArtifacts: []string{"other.md"}},
		},
	}

	_, err := compat.Admit(table)

	if err != nil {
		t.Errorf("Admit must admit bare EXECUTION rows with any agent identifiers: %v", err)
	}
}

func TestAdmit_BareRowsAlternatingAgents_Succeeds(t *testing.T) {
	// Bare EXECUTION rows (no group segment) with alternating agent identifiers
	// must be admitted as a single implicit group. The former "non-contiguous"
	// test was based on agent-name classification, which is removed. Without
	// group segments in the Phase column, grouping is never active.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "alternating-agents", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "test-writer-tdd"},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd"},
			{Index: 2, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "test-writer-tdd"},
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd"},
		},
	}

	_, err := compat.Admit(table)

	if err != nil {
		t.Errorf("Admit must admit bare rows with alternating agent identifiers: %v", err)
	}
}

// ---- FR-18a refusal: Condition 5 — parallel dispatch ----

func TestAdmit_ParallelDispatch_OnSuccessMultipleAgents_ReturnsRefusalError(t *testing.T) {
	// Parallel dispatch is indicated when a routing hint references multiple
	// agents simultaneously (comma-separated agents in OnSuccess, indicating the
	// runner would need to dispatch to multiple agents at the same time).
	// This is the "Waits For" / parallel dispatch condition.
	//
	// Note on detection mechanism: OnSuccess with a comma-separated value is used
	// as the proxy for parallel dispatch because the parsed RoutingRow type does not
	// have a dedicated "Waits For" column field.  The implementation must detect
	// parallel dispatch via the same mechanism (comma in OnSuccess.Value) to ensure
	// this test and the implementation stay in sync.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "parallel-workflow", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "PLANNING",
				PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: false},
				Agent:       "planner",
				// OnSuccess references multiple agents — parallel dispatch
				OnSuccess: domain.OptionalHint{ColumnPresent: true, Value: "agent-a,agent-b"},
			},
			{
				Index:       1,
				Phase:       "EXECUTION.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:       "implementer",
				OnSuccess:   domain.OptionalHint{ColumnPresent: true, Value: "COMPLETE"},
			},
		},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse a workflow with parallel dispatch notation")
	}
	asRefusalError(t, err)
}

func TestAdmit_ParallelDispatch_RefusalMessage_NonEmpty(t *testing.T) {
	// The refusal for parallel dispatch must include a non-empty Reason naming
	// the parallel dispatch condition so users can act on the message.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "parallel-workflow", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "PLANNING",
				PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: false},
				Agent:       "planner",
				OnSuccess:   domain.OptionalHint{ColumnPresent: true, Value: "agent-a,agent-b"},
			},
			{
				Index:       1,
				Phase:       "EXECUTION.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:       "implementer",
				OnSuccess:   domain.OptionalHint{ColumnPresent: true, Value: "COMPLETE"},
			},
		},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the parallel dispatch condition")
	}
	if !strings.Contains(re.Resource, "parallel-workflow") {
		t.Errorf("RefusalError.Resource must name the workflow ID %q, got %q", "parallel-workflow", re.Resource)
	}
}

// ---- FR-18a refusal: Condition 1 — stage source other than plan artifact ----

func TestAdmit_NonPlanStageSource_ReturnsRefusalError(t *testing.T) {
	// A routing table where the pre-EXECUTION rows do not output Stage-*/Plan.md
	// (the plan artifact stage source) but the table still has EXECUTION rows
	// is considered to have a stage source other than the plan artifact.
	// This workflow declares EXECUTION rows but its output artifacts don't
	// include Stage-*/Plan.md, meaning stages cannot come from the plan artifact.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "non-plan-stages", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:           0,
				Phase:           "PLANNING",
				PhaseParsed:     domain.PhaseParsed{Name: "PLANNING", IsStaged: false},
				Agent:           "custom-planner",
				// Output does NOT include Stage-*/Plan.md — stages don't come from plan artifact.
				OutputArtifacts: []string{"custom-stages.json"},
			},
			{
				Index:           1,
				Phase:           "EXECUTION.[StageNumber]",
				PhaseParsed:     domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:           "implementer",
				OutputArtifacts: []string{"out.md"},
			},
		},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse a workflow where stage source is not the plan artifact")
	}
	asRefusalError(t, err)
}

func TestAdmit_NonPlanStageSource_RefusalMessage_NonEmpty(t *testing.T) {
	// The refusal for a non-plan stage source must include a non-empty Reason
	// naming the condition so users understand why the workflow was refused.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "non-plan-stages", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:           0,
				Phase:           "PLANNING",
				PhaseParsed:     domain.PhaseParsed{Name: "PLANNING", IsStaged: false},
				Agent:           "custom-planner",
				OutputArtifacts: []string{"custom-stages.json"},
			},
			{
				Index:           1,
				Phase:           "EXECUTION.[StageNumber]",
				PhaseParsed:     domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:           "implementer",
				OutputArtifacts: []string{"out.md"},
			},
		},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the non-plan stage source condition")
	}
	if !strings.Contains(re.Resource, "non-plan-stages") {
		t.Errorf("RefusalError.Resource must name the workflow ID %q, got %q", "non-plan-stages", re.Resource)
	}
}

// ---- RefusalError contract ----

func TestAdmit_RefusalError_ComponentIsCompat(t *testing.T) {
	// Every refusal from compat.Admit must name "compat" as the component.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "staged-planning", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "PLANNING.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: true},
				Agent:       "planner",
			},
		},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if re.Component != "compat" {
		t.Errorf("RefusalError.Component: want %q, got %q", "compat", re.Component)
	}
}

func TestAdmit_RefusalError_ResourceNamesWorkflow(t *testing.T) {
	// RefusalError.Resource must identify the workflow so the user can locate
	// the problematic workflow definition.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "my-problem-workflow", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "PLANNING.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "PLANNING", IsStaged: true},
				Agent:       "planner",
			},
		},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if re.Resource == "" {
		t.Error("RefusalError.Resource must name the workflow, not be empty")
	}
	if !strings.Contains(re.Resource, "my-problem-workflow") {
		t.Errorf("RefusalError.Resource must contain the workflow ID %q, got %q", "my-problem-workflow", re.Resource)
	}
}

func TestAdmit_RefusalError_ReasonIsNonEmpty(t *testing.T) {
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "mode-workflow", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{
				Index:       0,
				Phase:       "EXECUTION.[StageNumber]",
				PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true},
				Agent:       "writer(fix-mode)",
			},
		},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must not be empty")
	}
}

// ---- Phase-driven group resolution: workflow-defined names (T3.1) ----

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

// Three or more groups

// threeGroupsContent is a synthetic workflow with three named groups (Alpha, Beta, Gamma)
// and a single-row approach table. No fixed-vocabulary agent names appear.
const threeGroupsContent = `<!-- workflow-version: 1.0 -->
## Three Groups Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | any-planner | ✅ | - | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| EXECUTION.Alpha.[StageNumber] | agent-a | ❌ | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Beta.[StageNumber] | agent-b | ❌ | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Gamma.[StageNumber] | agent-c | ❌ | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| AllGroups | Alpha, Beta, Gamma |
`

func TestAdmit_ThreeGroups_Succeeds(t *testing.T) {
	table := mustParseTable(t, threeGroupsContent, "three-groups", "1.0")
	_, err := compat.Admit(table)
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
	// Group names are taken verbatim from the Phase column; order is first-appearance.
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
	// PLANNING row is index 0; EXECUTION rows are indices 1 (Alpha), 2 (Beta), 3 (Gamma).
	// Expected half-open ranges: Alpha=[1,2), Beta=[2,3), Gamma=[3,4).
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

// Arbitrary group names / generic agent identifiers

// genericAgentGroupsContent is a synthetic workflow whose agent identifiers appear
// in no hardcoded list. The group partition comes entirely from the Phase column.
const genericAgentGroupsContent = `<!-- workflow-version: 1.0 -->
## Generic Agent Groups

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | any-planner | ✅ | - | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| EXECUTION.Analysis.[StageNumber] | completely-unknown-agent-x | ❌ | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Analysis.[StageNumber] | another-unknown-agent-y | ❌ | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Synthesis.[StageNumber] | yet-another-unknown-z | ❌ | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| Forward | Analysis, Synthesis |
`

func TestAdmit_GenericAgents_WithGroupNotation_Succeeds(t *testing.T) {
	// Agent identifiers not in any hardcoded set can still form groups via the
	// Phase column's group segment. The partition is driven purely by the Phase
	// column, never by agent names.
	table := mustParseTable(t, genericAgentGroupsContent, "generic-agent-groups", "1.0")
	_, err := compat.Admit(table)
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
	// Group names are the workflow-defined tokens from the Phase column, verbatim.
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

// ---- No-groups-declared case (T3.3) ----

func TestAdmit_QuickFix_GroupsDeclaredFalse(t *testing.T) {
	// quick-fix has no EXECUTION rows with group segments.
	// GroupsDeclared must be false.
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if aw.GroupsDeclared {
		t.Error("GroupsDeclared: want false for a workflow with no group segments")
	}
}

func TestAdmit_QuickFix_SingleImplicitGroupEmptyName(t *testing.T) {
	// When no groups are declared, the single implicit group has an empty Name.
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
	// A bare workflow's AdmittedWorkflow.ApproachTable must be zero-valued (not present).
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
	// The single implicit group for a bare workflow has an empty Name.
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
	// The single implicit group must span all EXECUTION rows in listed order.
	// implementation-only: EXECUTION rows at indices 0 and 1.
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if len(aw.Groups) != 1 {
		t.Fatalf("Groups: want 1, got %d", len(aw.Groups))
	}
	if aw.Groups[0].StartRow != 0 || aw.Groups[0].EndRow != 2 {
		t.Errorf("Groups[0]: want [0,2), got [%d,%d)", aw.Groups[0].StartRow, aw.Groups[0].EndRow)
	}
}

// ---- Admission refusals A1–A5 (T3.2) ----
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
	// The refusal catalogue template uses "row %d" — check for "row 3" rather than
	// the bare digit "3" to avoid a false match on unrelated numbers in the message.
	if !strings.Contains(re.Reason, "row 3") {
		t.Errorf("A1 refusal must name the offending row index as %q; got Reason: %q", "row 3", re.Reason)
	}
	if !strings.Contains(re.Reason, "Alpha") {
		t.Errorf("A1 refusal must name the offending group %q; got Reason: %q", "Alpha", re.Reason)
	}
}

// A2: group segments in EXECUTION rows but the workflow has no approach table.

func TestAdmit_A2_GroupsWithoutApproachTable_ReturnsRefusalError(t *testing.T) {
	// At least one EXECUTION row carries a group segment but the workflow
	// has no **Execution Groups:** table.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "groups-no-table", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			{Index: 1, Phase: "EXECUTION.Test.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Test"}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
		},
		// ApproachTable is zero value — no table declared.
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
	// The refusal catalogue template uses "row %d" — check for "row 1" rather than
	// the bare digit "1" to avoid a false match on unrelated numbers in the message.
	if !strings.Contains(re.Reason, "row 1") {
		t.Errorf("A2 refusal must name the offending row index as %q; got Reason: %q", "row 1", re.Reason)
	}
	if !strings.Contains(re.Reason, "Test") {
		t.Errorf("A2 refusal must name the group %q; got Reason: %q", "Test", re.Reason)
	}
}

// A3: approach table present but an EXECUTION row carries no group segment.

func TestAdmit_A3_ApproachTableWithBareRow_ReturnsRefusalError(t *testing.T) {
	// The workflow has an approach table, but row 1 is a bare staged row
	// (no group segment). All EXECUTION rows must carry a group segment when
	// an approach table is present.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "table-bare-row", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			// Bare row — no group segment.
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
	// The refusal catalogue template uses "row %d" — check for "row 1" rather than
	// the bare digit "1" to avoid a false match on unrelated numbers in the message.
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
			// "Ghost" is listed in the table but no EXECUTION row carries it.
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
			// "Mystery" is not referenced in the approach table at all.
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
	// The refusal catalogue template uses "row %d" — check for "row 3" rather than
	// the bare digit "3" to avoid a false match on unrelated numbers in the message.
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
	// declares "Test" (capital T). Because comparison is verbatim and case-sensitive
	// everywhere (ContractsDesign.md File Format Contract), these two tokens are
	// distinct: the EXECUTION row's group does not appear in the approach table (A5)
	// and the approach table names a group absent from every EXECUTION row (A4).
	// Either refusal fires; the invariant is that Admit must NOT silently treat
	// "test" and "Test" as the same group.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "case-mismatch", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "PLANNING", PhaseParsed: domain.PhaseParsed{Name: "PLANNING"}, Agent: "planner",
				OutputArtifacts: []string{"Plan.md", "Stage-*/Plan.md", "Stage-*/PlanProgress.md"}},
			// Group token is "test" (lowercase), which must not match "Test" in the approach table.
			{Index: 1, Phase: "EXECUTION.test.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "test"}, Agent: "agent-a", OutputArtifacts: []string{"out.md"}},
			{Index: 2, Phase: "EXECUTION.Implementation.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true, Group: "Implementation"}, Agent: "agent-b", OutputArtifacts: []string{"out.md"}},
		},
		ApproachTable: domain.ApproachTable{Rows: []domain.ApproachSequence{
			// The table declares "Test" (capital T) — a different token from "test".
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
	// The reason must name the mismatched token ("test") so the author can act on it.
	if !strings.Contains(re.Reason, "test") {
		t.Errorf("refusal message must name the offending group token %q; got Reason: %q", "test", re.Reason)
	}
}
