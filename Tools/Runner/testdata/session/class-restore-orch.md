<Workflow type="core" name="linear" version="1.0">
## Linear Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | ❌ | agent-b | - | - | plan.md |
| PLANNING | agent-b | ❌ | COMPLETE | - | plan.md | result.md |
</Workflow>

<InfrastructureAgents type="project">
<InfrastructureAgent type="core" name="checkpoint-restore-s3" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| restore | INVOCATION_INTERVAL | 1 | halt | S3-based restore agent (must never be auto-triggered) |
</InfrastructureAgent>
<InfrastructureAgent type="core" name="checkpoint-manager-git" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| checkpoint | INVOCATION_INTERVAL | 999 | continue | Placeholder checkpoint agent to satisfy checkpoint precondition |
</InfrastructureAgent>
</InfrastructureAgents>
