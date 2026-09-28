package compat_test

import (
	"errors"
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
const brownfieldBuildVerifiedContent = `## Brownfield TDD Build-Verified Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | FALSE | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | build-review | FALSE | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-tests.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | build-review | FALSE | implementation-review | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | COMPLETE | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-impl.md | Stage-{StageNumber}/implementation-review.md |

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
// This workflow has 13 rows: 8 pre-EXECUTION, 4 EXECUTION, 1 REVIEW.
const greenFieldTDDContent = `## Greenfield TDD Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | system-designer | requirements-refinement | Requirements.md | requirements-review.md |
| ARCHITECTURE | system-designer | TRUE | system-design-review | - | Requirements.md | SystemDesign.md |
| ARCHITECTURE | system-design-review | FALSE | planner-tdd-soft | system-designer | Requirements.md, SystemDesign.md | system-design-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Requirements.md, SystemDesign.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Requirements.md, Plan.md, Stage-*/Plan.md, SystemDesign.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |

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
const brownfieldTDDContent = `## Brownfield TDD Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | FALSE | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |

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
const quickFixContent = `## Quick Fix Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | implementation-tdd | planner-tdd-soft | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | test-runner | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |
`

// implOnlyContent is the routing table region from
// Workflows/Build/implementation-only.md (version 3.1).
// This workflow has 3 rows: 2 EXECUTION, 1 REVIEW. No pre-EXECUTION rows.
const implOnlyContent = `## Implementation Only Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |
`

// threeGroupsContent is a synthetic workflow with three named groups (Alpha, Beta, Gamma)
// and a single-row approach table. No fixed-vocabulary agent names appear.
const threeGroupsContent = `## Three Groups Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | any-planner | TRUE | - | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| EXECUTION.Alpha.[StageNumber] | agent-a | FALSE | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Beta.[StageNumber] | agent-b | FALSE | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Gamma.[StageNumber] | agent-c | FALSE | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| AllGroups | Alpha, Beta, Gamma |
`

// genericAgentGroupsContent is a synthetic workflow whose agent identifiers appear
// in no hardcoded list. The group partition comes entirely from the Phase column.
const genericAgentGroupsContent = `## Generic Agent Groups

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | any-planner | TRUE | - | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| EXECUTION.Analysis.[StageNumber] | completely-unknown-agent-x | FALSE | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Analysis.[StageNumber] | another-unknown-agent-y | FALSE | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Synthesis.[StageNumber] | yet-another-unknown-z | FALSE | - | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| Forward | Analysis, Synthesis |
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
