<Workflow type="core" name="linear" version="1.0">
## Linear Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | - | plan.md | result.md |
</Workflow>

<InfrastructureAgents type="project">
<InfrastructureAgent type="core" name="checkpoint-manager-git" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| checkpoint | INVOCATION_INTERVAL | 2 | halt | Git checkpoint manager (interval 2) |
</InfrastructureAgent>
</InfrastructureAgents>
