<Workflow type="core" name="staged" version="1.0">
## Staged Workflow (Implementation-Only)

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | COMPLETE | implementation-tdd | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/implementation-review.md |
</Workflow>

<InfrastructureAgents type="project">
<InfrastructureAgent type="core" name="commit-manager-git" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| commit | STAGE_END | - | halt | Git commit manager (fires on stage transition) |
</InfrastructureAgent>
</InfrastructureAgents>
