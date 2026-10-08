<Workflow type="core" name="consult-row-stage" version="1.0">
## Grouped Staged Workflow with a Review-Class Infrastructure Agent

A workflow with non-staged planning rows followed by grouped staged EXECUTION
rows. Requires a Plan.md with an Approach column in RunFolder. The routing
table is not expanded per stage: each EXECUTION row is one table row that runs
once per plan stage.

Row index mapping (zero-based; the recorded WorkflowRow is the index plus one):
  0 = PLANNING / planner
  1 = PLANNING / plan-review
  2 = EXECUTION.Test.[StageNumber] / test-writer
  3 = EXECUTION.Implementation.[StageNumber] / implementer
  4 = EXECUTION.Implementation.[StageNumber] / build-review
  5 = EXECUTION.Implementation.[StageNumber] / impl-review
  6 = REVIEW / final-review

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | FALSE | plan-review | - | - | Plan.md, Stage-*/Plan.md |
| PLANNING | plan-review | FALSE | test-writer | planner | - | - |
| EXECUTION.Test.[StageNumber] | test-writer | FALSE | implementer | - | - | - |
| EXECUTION.Implementation.[StageNumber] | implementer | FALSE | build-review | - | - | - |
| EXECUTION.Implementation.[StageNumber] | build-review | FALSE | impl-review | implementer | - | - |
| EXECUTION.Implementation.[StageNumber] | impl-review | FALSE | final-review | implementer | - | - |
| REVIEW | final-review | FALSE | COMPLETE | - | - | - |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-Only | Implementation |
</Workflow>

<InfrastructureAgents type="managed">
<InfrastructureAgent type="core" name="review-agent" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | INVOCATION_INTERVAL | 1 | continue | Review agent |
</InfrastructureAgent>
</InfrastructureAgents>
