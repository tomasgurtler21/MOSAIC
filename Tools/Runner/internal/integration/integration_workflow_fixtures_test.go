package integration_test

// Fixtures for the five supported workflows exercised end-to-end.

// planImplOnly is a single-stage Plan.md with Implementation-Only approach,
// used by two-group workflows (TwoGroup=true, needsApproach=true).
const planImplOnly = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL | Approach |
|-------|------|------|------------|:----:|----------|
| 1 | Stage One | Run | - | FALSE | Implementation-Only |
`

// planSingleGroup is a single-stage Plan.md without an Approach column,
// used by single-group workflows (TwoGroup=false, needsApproach=false).
const planSingleGroup = `# Plan

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | Stage One | Run | - | FALSE |
`

const orchBrownfieldTDD = `			<Workflow type="core" name="brownfield-tdd" version="3.5">
## Brownfield TDD Workflow

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
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
</Workflow>
`

const orchBrownfieldTDDBuildVerified = `			<Workflow type="core" name="brownfield-tdd-build-verified" version="2.1">
## Brownfield TDD Build-Verified Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | FALSE | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | build-review | - | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | build-review | FALSE | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | build-review | - | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | build-review | FALSE | implementation-review | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | COMPLETE | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
</Workflow>
`

const orchGreenfieldTDD = `			<Workflow type="core" name="greenfield-tdd" version="3.4">
## Greenfield TDD Workflow

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
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |
</Workflow>
`

const orchImplementationOnly = `			<Workflow type="core" name="implementation-only" version="3.1">
## Implementation Only Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | FALSE | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |
</Workflow>
`

const orchQuickFix = `			<Workflow type="core" name="quick-fix" version="3.0">
## Quick Fix Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | implementation-tdd | planner-tdd-soft | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | test-runner | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| REVIEW | test-runner | FALSE | COMPLETE | implementation-tdd | - | TestResults.md |
</Workflow>
`

// integrationAgentResponse is a scripted SUCCESS response for a named agent.
type integrationAgentResponse struct {
	agent   string
	summary string
}

// integrationWorkflowCase describes one workflow run end-to-end.
type integrationWorkflowCase struct {
	name        string
	workflowID  string
	orchContent string
	plan        string
	agents      []string
	responses   []integrationAgentResponse
}

// newTestFiveWorkflowCases returns the five supported workflow scenarios.
func newTestFiveWorkflowCases() []integrationWorkflowCase {
	return []integrationWorkflowCase{
		{
			name:       "brownfield-tdd",
			workflowID: "brownfield-tdd",
			// Routing table matches brownfield-tdd v3.4.
			// Two-group: test group (test-writer-tdd, tests-review-tdd) +
			//            impl group (implementation-tdd, implementation-review).
			// Seven pre-exec rows; post-exec row: test-runner.
			orchContent: orchBrownfieldTDD,
			plan:        planImplOnly,
			agents: []string{
				"codebase-research", "requirements-refinement", "requirements-review",
				"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
				"test-writer-tdd", "tests-review-tdd",
				"implementation-tdd", "implementation-review",
				"test-runner",
			},
			// Dispatch order with Implementation-Only approach (impl group only):
			// 7 pre-exec + 2 exec (Stage-1) + 1 post-exec = 10
			responses: []integrationAgentResponse{
				{"codebase-research", "research done"},
				{"requirements-refinement", "requirements refined"},
				{"requirements-review", "requirements reviewed"},
				{"planner-tdd-soft", "plan created"},
				{"plan-review", "plan reviewed"},
				{"contracts-designer", "contracts designed"},
				{"contracts-review", "contracts reviewed"},
				{"implementation-tdd", "stage 1 implemented"},
				{"implementation-review", "stage 1 reviewed"},
				{"test-runner", "tests passed"},
			},
		},
		{
			name:       "brownfield-tdd-build-verified",
			workflowID: "brownfield-tdd-build-verified",
			// Two-group: test group (test-writer-tdd, build-review, tests-review-tdd) +
			//            impl group (implementation-tdd, build-review, implementation-review).
			// Seven pre-exec rows; no post-exec rows.
			orchContent: orchBrownfieldTDDBuildVerified,
			plan:        planImplOnly,
			agents: []string{
				"codebase-research", "requirements-refinement", "requirements-review",
				"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
				"test-writer-tdd", "build-review", "tests-review-tdd",
				"implementation-tdd", "implementation-review",
			},
			// Dispatch order with Implementation-Only (impl group: rows 10-12):
			// 7 pre-exec + 3 exec (Stage-1) = 10
			responses: []integrationAgentResponse{
				{"codebase-research", "research done"},
				{"requirements-refinement", "requirements refined"},
				{"requirements-review", "requirements reviewed"},
				{"planner-tdd-soft", "plan created"},
				{"plan-review", "plan reviewed"},
				{"contracts-designer", "contracts designed"},
				{"contracts-review", "contracts reviewed"},
				{"implementation-tdd", "stage 1 implemented"},
				{"build-review", "stage 1 build ok"},
				{"implementation-review", "stage 1 reviewed"},
			},
		},
		{
			name:       "greenfield-tdd",
			workflowID: "greenfield-tdd",
			// Two-group: test group (test-writer-tdd, tests-review-tdd) +
			//            impl group (implementation-tdd, implementation-review).
			// Eight pre-exec rows; post-exec row: test-runner.
			orchContent: orchGreenfieldTDD,
			plan:        planImplOnly,
			agents: []string{
				"requirements-refinement", "requirements-review",
				"system-designer", "system-design-review",
				"planner-tdd-soft", "plan-review", "contracts-designer", "contracts-review",
				"test-writer-tdd", "tests-review-tdd",
				"implementation-tdd", "implementation-review",
				"test-runner",
			},
			// Dispatch order with Implementation-Only (impl group rows 10-11):
			// 8 pre-exec + 2 exec (Stage-1) + 1 post-exec = 11
			responses: []integrationAgentResponse{
				{"requirements-refinement", "requirements refined"},
				{"requirements-review", "requirements reviewed"},
				{"system-designer", "system designed"},
				{"system-design-review", "system design reviewed"},
				{"planner-tdd-soft", "plan created"},
				{"plan-review", "plan reviewed"},
				{"contracts-designer", "contracts designed"},
				{"contracts-review", "contracts reviewed"},
				{"implementation-tdd", "stage 1 implemented"},
				{"implementation-review", "stage 1 reviewed"},
				{"test-runner", "tests passed"},
			},
		},
		{
			name:       "implementation-only",
			workflowID: "implementation-only",
			// Single-group: implementation-tdd → implementation-review.
			// No pre-exec rows; post-exec row: test-runner.
			orchContent: orchImplementationOnly,
			plan:        planSingleGroup,
			agents:      []string{"implementation-tdd", "implementation-review", "test-runner"},
			// Dispatch order: 2 exec (Stage-1) + 1 post-exec = 3
			responses: []integrationAgentResponse{
				{"implementation-tdd", "stage 1 implemented"},
				{"implementation-review", "stage 1 reviewed"},
				{"test-runner", "tests passed"},
			},
		},
		{
			name:       "quick-fix",
			workflowID: "quick-fix",
			// Single-group: implementation-tdd (EXECUTION).
			// Two pre-exec rows; post-exec row: test-runner.
			orchContent: orchQuickFix,
			plan:        planSingleGroup,
			agents:      []string{"planner-tdd-soft", "plan-review", "implementation-tdd", "test-runner"},
			// Dispatch order: 2 pre-exec + 1 exec (Stage-1) + 1 post-exec = 4
			responses: []integrationAgentResponse{
				{"planner-tdd-soft", "plan created"},
				{"plan-review", "plan reviewed"},
				{"implementation-tdd", "stage 1 implemented"},
				{"test-runner", "tests passed"},
			},
		},
	}
}
