<Workflow type="core" name="hitl-stage-end" version="1.0">
## Staged HITL Review Workflow

The stage's last step is a HITL-dispatched reviewer. A commit-class agent fires
at the end of each stage. Used to verify that STAGE_END follows the routed
status after a HITL re-dispatch.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | TRUE | COMPLETE | implementation-tdd | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/implementation-review.md |
</Workflow>

<InfrastructureAgents type="managed">
<InfrastructureAgent type="core" name="commit-manager-git" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| commit | STAGE_END | - | halt | Git commit manager (fires on stage transition) |
</InfrastructureAgent>
</InfrastructureAgents>
