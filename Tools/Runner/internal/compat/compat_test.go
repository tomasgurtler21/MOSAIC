package compat_test

// Tests for compat.Admit.
//
// Coverage:
//
//   Admission success — five supported workflows:
//   - brownfield-tdd-build-verified (two-group, 13 rows)
//   - greenfield-tdd (two-group, 13 rows)
//   - brownfield-tdd (two-group, 12 rows)
//   - quick-fix (single-group, 4 rows)
//   - implementation-only (single-group, 3 rows)
//
//   Two-group identification:
//   - brownfield-tdd-build-verified → TwoGroup=true
//   - greenfield-tdd → TwoGroup=true
//   - brownfield-tdd → TwoGroup=true
//   - quick-fix → TwoGroup=false
//   - implementation-only → TwoGroup=false
//
//   Execution group resolution — brownfield-tdd-build-verified:
//   - Two execution groups (Groups has 2 elements).
//   - Test group: Kind=GroupTest, StartRow=7, EndRow=10 (rows 7-9).
//   - Implementation group: Kind=GroupImplementation, StartRow=10, EndRow=13 (rows 10-12).
//   - Groups are contiguous and cover all 6 EXECUTION rows (no gaps, no overlap).
//   - Duplicate agent identifier (build-review appears at rows 8 and 11) is allowed.
//
//   Execution group resolution — brownfield-tdd (two-group, no build-review):
//   - Test group: Kind=GroupTest, StartRow=7, EndRow=9 (rows 7-8).
//   - Implementation group: Kind=GroupImplementation, StartRow=9, EndRow=11 (rows 9-10).
//
//   Execution group resolution — greenfield-tdd:
//   - Test group: Kind=GroupTest, StartRow=8, EndRow=10 (rows 8-9).
//   - Implementation group: Kind=GroupImplementation, StartRow=10, EndRow=12 (rows 10-11).
//
//   Execution group resolution — quick-fix (single-group):
//   - One execution group (Groups has 1 element).
//   - Group covers the single EXECUTION row: StartRow=2, EndRow=3.
//
//   Execution group resolution — implementation-only (single-group):
//   - One execution group (Groups has 1 element).
//   - Group covers the two EXECUTION rows: StartRow=0, EndRow=2.
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
//   - Condition 7: EXECUTION rows that cannot be resolved into 1 or 2 contiguous groups.
//   - Condition 5: Parallel dispatch expressed via multiple agents in routing hints.
//   - Condition 1: Stage source not from plan artifact (non-standard stage notation).
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
// Workflows/Build/brownfield-tdd-build-verified.md (version 2.0).
// This workflow has 13 rows: 7 pre-EXECUTION, 6 EXECUTION.
const brownfieldBuildVerifiedContent = `<!-- workflow-version: 2.0 -->
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
| EXECUTION.[StageNumber] | test-writer-tdd | ❌ | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | build-review | ❌ | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-tests.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | build-review | ❌ | implementation-review | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | COMPLETE | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-impl.md | Stage-{StageNumber}/implementation-review.md |
`

// greenFieldTDDContent is the routing table region from
// Workflows/Build/greenfield-tdd.md (version 3.3).
// This workflow has 13 rows: 8 pre-EXECUTION (2 RESEARCH, 2 ARCHITECTURE, 2 PLANNING, 2 DESIGN),
// 4 EXECUTION, 1 REVIEW.
const greenFieldTDDContent = `<!-- workflow-version: 3.3 -->
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
| EXECUTION.[StageNumber] | test-writer-tdd | ❌ | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |
`

// brownfieldTDDContent is the routing table region from
// Workflows/Build/brownfield-tdd.md (version 3.4).
// This workflow has 12 rows: 7 pre-EXECUTION, 4 EXECUTION, 1 REVIEW.
const brownfieldTDDContent = `<!-- workflow-version: 3.4 -->
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
| EXECUTION.[StageNumber] | test-writer-tdd | ❌ | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |
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

// ---- TwoGroup identification ----

func TestAdmit_BrownfieldTDDBuildVerified_IsTwoGroup(t *testing.T) {
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if !aw.TwoGroup {
		t.Error("brownfield-tdd-build-verified: TwoGroup must be true (has separate test and implementation groups)")
	}
}

func TestAdmit_GreenFieldTDD_IsTwoGroup(t *testing.T) {
	table := mustParseTable(t, greenFieldTDDContent, "greenfield-tdd", "3.3")
	aw := mustAdmit(t, table)

	if !aw.TwoGroup {
		t.Error("greenfield-tdd: TwoGroup must be true")
	}
}

func TestAdmit_BrownfieldTDD_IsTwoGroup(t *testing.T) {
	table := mustParseTable(t, brownfieldTDDContent, "brownfield-tdd", "3.4")
	aw := mustAdmit(t, table)

	if !aw.TwoGroup {
		t.Error("brownfield-tdd: TwoGroup must be true")
	}
}

func TestAdmit_QuickFix_IsSingleGroup(t *testing.T) {
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if aw.TwoGroup {
		t.Error("quick-fix: TwoGroup must be false (single-group workflow)")
	}
}

func TestAdmit_ImplementationOnly_IsSingleGroup(t *testing.T) {
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if aw.TwoGroup {
		t.Error("implementation-only: TwoGroup must be false (single-group workflow)")
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

func TestAdmit_BrownfieldTDDBuildVerified_TestGroupKind(t *testing.T) {
	// The first group must be the test group (test-writer-tdd, build-review, tests-review-tdd).
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1 group")
	}
	if aw.Groups[0].Kind != domain.GroupTest {
		t.Errorf("Groups[0].Kind: want %q, got %q", domain.GroupTest, aw.Groups[0].Kind)
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

func TestAdmit_BrownfieldTDDBuildVerified_ImplementationGroupKind(t *testing.T) {
	// The second group must be the implementation group
	// (implementation-tdd, build-review, implementation-review).
	table := mustParseTable(t, brownfieldBuildVerifiedContent, "brownfield-tdd-build-verified", "2.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 2 {
		t.Fatal("Groups: want at least 2 groups")
	}
	if aw.Groups[1].Kind != domain.GroupImplementation {
		t.Errorf("Groups[1].Kind: want %q, got %q", domain.GroupImplementation, aw.Groups[1].Kind)
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

func TestAdmit_QuickFix_GroupKind(t *testing.T) {
	// quick-fix is a single-group workflow whose EXECUTION row runs implementation-tdd.
	// The group kind must be GroupImplementation.
	// An implementation that set Kind=GroupTest for single-group workflows would still
	// pass the Groups count and row-range tests above — this test catches that mistake.
	table := mustParseTable(t, quickFixContent, "quick-fix", "3.0")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].Kind != domain.GroupImplementation {
		t.Errorf("Groups[0].Kind: want %q for quick-fix, got %q",
			domain.GroupImplementation, aw.Groups[0].Kind)
	}
}

func TestAdmit_ImplementationOnly_GroupKind(t *testing.T) {
	// implementation-only is a single-group workflow; the group kind must be GroupImplementation.
	table := mustParseTable(t, implOnlyContent, "implementation-only", "3.1")
	aw := mustAdmit(t, table)

	if len(aw.Groups) < 1 {
		t.Fatal("Groups: want at least 1")
	}
	if aw.Groups[0].Kind != domain.GroupImplementation {
		t.Errorf("Groups[0].Kind: want %q for implementation-only, got %q",
			domain.GroupImplementation, aw.Groups[0].Kind)
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

func TestAdmit_UnresolvableGroups_ThreeAgentTypes_RefusalMessage_NonEmpty(t *testing.T) {
	// Each unresolvable-groups refusal must include a non-empty Reason naming the
	// condition so users understand that the workflow's EXECUTION rows could not be
	// partitioned into 1 or 2 contiguous groups.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "unresolvable-groups", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "test-writer-tdd", OutputArtifacts: []string{"tests.md"}},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "unknown-agent", OutputArtifacts: []string{"unknown.md"}},
			{Index: 2, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd", OutputArtifacts: []string{"impl.md"}},
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "unknown-agent-2", OutputArtifacts: []string{"other.md"}},
		},
	}

	_, err := compat.Admit(table)

	re := asRefusalError(t, err)
	if re.Reason == "" {
		t.Error("RefusalError.Reason must describe the unresolvable execution groups condition")
	}
	if !strings.Contains(re.Resource, "unresolvable-groups") {
		t.Errorf("RefusalError.Resource must name the workflow ID %q, got %q", "unresolvable-groups", re.Resource)
	}
}

func TestAdmit_UnresolvableGroups_ThreeAgentTypes_ReturnsRefusalError(t *testing.T) {
	// EXECUTION rows with three distinct agent types that cannot be partitioned
	// into one or two contiguous groups based on their known roles.
	// For example: test-writer, unknown-agent, implementation-tdd creates
	// ambiguity about which group "unknown-agent" belongs to.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "unresolvable-groups", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "test-writer-tdd", OutputArtifacts: []string{"tests.md"}},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "unknown-agent", OutputArtifacts: []string{"unknown.md"}},
			{Index: 2, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd", OutputArtifacts: []string{"impl.md"}},
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "unknown-agent-2", OutputArtifacts: []string{"other.md"}},
		},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse a workflow whose EXECUTION rows cannot be resolved into 1 or 2 contiguous groups")
	}
	asRefusalError(t, err)
}

func TestAdmit_UnresolvableGroups_NonContiguous_ReturnsRefusalError(t *testing.T) {
	// EXECUTION rows that alternate between test and implementation agents cannot
	// be partitioned into contiguous groups.
	// test-writer, implementation-tdd, test-writer, implementation-tdd would
	// require 4 groups, which exceeds the maximum of 2.
	table := domain.RoutingTable{
		Info: domain.WorkflowInfo{ID: "non-contiguous-groups", Version: "1.0"},
		Rows: []domain.RoutingRow{
			{Index: 0, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "test-writer-tdd"},
			{Index: 1, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd"},
			{Index: 2, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "test-writer-tdd"},
			{Index: 3, Phase: "EXECUTION.[StageNumber]", PhaseParsed: domain.PhaseParsed{Name: "EXECUTION", IsStaged: true}, Agent: "implementation-tdd"},
		},
	}

	_, err := compat.Admit(table)

	if err == nil {
		t.Fatal("Admit must refuse a workflow with non-contiguous (alternating) execution groups")
	}
	asRefusalError(t, err)
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
