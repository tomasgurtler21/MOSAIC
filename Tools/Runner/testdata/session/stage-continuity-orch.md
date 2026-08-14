<Workflow type="core" name="stage-continuity" version="1.0">
## Stage Continuity Workflow

Workflow combining a pre-EXECUTION planning row that emits Stage-* outputs
with a staged EXECUTION phase, so the stage-set-continuity transition can be
exercised end to end: the planning row is dispatched first, the plan file
appears in the run folder during the run, and the run must then enter
EXECUTION and dispatch the stage 1 rows rather than stopping.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | ❌ | reviewer | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | reviewer | ❌ | implementation-tdd | planner | Plan.md | Stage-*/Plan.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | COMPLETE | implementation-tdd | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/implementation-review.md |
</Workflow>
