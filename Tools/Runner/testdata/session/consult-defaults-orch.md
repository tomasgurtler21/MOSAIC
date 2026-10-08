<Workflow type="core" name="consult-row-stage" version="1.0">
## Grouped Staged Workflow with Artifact Defaults for Consultation Defaults Tests

Rows declare input and output artifacts, including Stage-* and {StageNumber}
patterns, so that the defaults a dispatch receives can be compared between the
engine-routed and the consultation-routed paths. Requires a Plan.md in RunFolder.

Row index mapping (zero-based; the recorded WorkflowRow is the index plus one):
  0 = PLANNING / planner
  1 = EXECUTION.Test.[StageNumber] / test-writer
  2 = EXECUTION.Implementation.[StageNumber] / implementer
  3 = EXECUTION.Implementation.[StageNumber] / build-review
  4 = REVIEW / final-review (Stage-* input on a non-staged row)
  5 = REVIEW / impl-review (input pattern that cannot resolve on a non-staged row)

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | FALSE | test-writer | - | - | Plan.md, Stage-*/Plan.md |
| EXECUTION.Test.[StageNumber] | test-writer | FALSE | implementer | - | Stage-*/Plan.md, Stage-{StageNumber}/notes.md | Stage-{StageNumber}/tests.md |
| EXECUTION.Implementation.[StageNumber] | implementer | FALSE | build-review | - | Stage-{StageNumber}/tests.md | Stage-{StageNumber}/impl.md |
| EXECUTION.Implementation.[StageNumber] | build-review | FALSE | final-review | implementer | Stage-{StageNumber}/impl.md | Stage-{StageNumber}/build-review.md |
| REVIEW | final-review | FALSE | impl-review | - | Stage-*/Plan.md | final-notes.md |
| REVIEW | impl-review | FALSE | COMPLETE | - | Stage-{StageNumber}/Plan.md | - |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-Only | Implementation |
</Workflow>
