[[SECTION:Workflow:linear]]
<!-- workflow-version: 1.0 -->
## Linear Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | ❌ | agent-b | - | - | plan.md |
| PLANNING | agent-b | ❌ | COMPLETE | - | plan.md | result.md |
[[/SECTION:Workflow:linear]]

[[INJECTION:InfrastructureAgents]]
[[SECTION:InfrastructureAgent:checkpoint-manager-git]]
<!-- infra-version: 1.0.0 -->
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| checkpoint | INVOCATION_INTERVAL | 3 | halt | Git checkpoint manager (interval 3) |
[[/SECTION:InfrastructureAgent:checkpoint-manager-git]]
[[/INJECTION:InfrastructureAgents]]
