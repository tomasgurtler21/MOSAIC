<Workflow type="core" name="manual-routing" version="1.0">
## Grouped Staged Workflow with Thirteen Rows for Manual Routing Tests

A workflow whose build-review agent is the twelfth routing table row, a staged
Implementation row. Requires a Plan.md with an Approach column in RunFolder.

Row numbers (1-based; the recorded WorkflowRow equals the row number):
   1 = PLANNING / planner
   2 = PLANNING / plan-review
   3 = PLANNING / spec-writer
   4 = PLANNING / spec-review
   5 = EXECUTION.Test.[StageNumber] / test-writer
   6 = EXECUTION.Test.[StageNumber] / test-review
   7 = EXECUTION.Implementation.[StageNumber] / implementer
   8 = EXECUTION.Implementation.[StageNumber] / refactorer
   9 = EXECUTION.Implementation.[StageNumber] / lint-check
  10 = EXECUTION.Implementation.[StageNumber] / doc-writer
  11 = EXECUTION.Implementation.[StageNumber] / impl-review
  12 = EXECUTION.Implementation.[StageNumber] / build-review
  13 = REVIEW / final-review

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | FALSE | plan-review | - | - | Plan.md, Stage-*/Plan.md |
| PLANNING | plan-review | FALSE | spec-writer | planner | - | - |
| PLANNING | spec-writer | FALSE | spec-review | - | - | - |
| PLANNING | spec-review | FALSE | test-writer | spec-writer | - | - |
| EXECUTION.Test.[StageNumber] | test-writer | FALSE | test-review | - | - | - |
| EXECUTION.Test.[StageNumber] | test-review | FALSE | implementer | test-writer | - | - |
| EXECUTION.Implementation.[StageNumber] | implementer | FALSE | refactorer | - | - | - |
| EXECUTION.Implementation.[StageNumber] | refactorer | FALSE | lint-check | - | - | - |
| EXECUTION.Implementation.[StageNumber] | lint-check | FALSE | doc-writer | - | - | - |
| EXECUTION.Implementation.[StageNumber] | doc-writer | FALSE | impl-review | - | - | - |
| EXECUTION.Implementation.[StageNumber] | impl-review | FALSE | build-review | implementer | - | - |
| EXECUTION.Implementation.[StageNumber] | build-review | FALSE | final-review | implementer | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/Build.md |
| REVIEW | final-review | FALSE | COMPLETE | - | - | - |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-Only | Implementation |
</Workflow>
