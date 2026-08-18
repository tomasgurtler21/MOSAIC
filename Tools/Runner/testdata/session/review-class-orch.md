<Workflow type="core" name="linear" version="1.0">
## Linear Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | ❌ | agent-b | - | - | plan.md |
| PLANNING | agent-b | ❌ | COMPLETE | - | plan.md | result.md |
</Workflow>

<InfrastructureAgents type="project">
<InfrastructureAgent type="core" name="review-agent-a" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | INVOCATION_INTERVAL | 1 | continue | Review agent A |
</InfrastructureAgent>
<InfrastructureAgent type="core" name="review-agent-b" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | INVOCATION_INTERVAL | 1 | continue | Review agent B |
</InfrastructureAgent>
</InfrastructureAgents>
