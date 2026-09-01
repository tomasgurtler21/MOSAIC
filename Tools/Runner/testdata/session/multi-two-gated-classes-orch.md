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
| checkpoint | INVOCATION_INTERVAL | 1 | halt | Primary git checkpoint manager |
</InfrastructureAgent>
<InfrastructureAgent type="core" name="checkpoint-manager-alt" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| checkpoint | INVOCATION_INTERVAL | 1 | continue | Alternate checkpoint manager |
</InfrastructureAgent>
<InfrastructureAgent type="core" name="commit-manager-git" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| commit | INVOCATION_INTERVAL | 1 | halt | Primary git commit manager |
</InfrastructureAgent>
<InfrastructureAgent type="core" name="commit-manager-alt" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| commit | INVOCATION_INTERVAL | 1 | continue | Alternate commit manager |
</InfrastructureAgent>
</InfrastructureAgents>
