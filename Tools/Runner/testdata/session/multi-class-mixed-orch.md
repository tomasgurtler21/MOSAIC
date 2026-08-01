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
| checkpoint | INVOCATION_INTERVAL | 1 | halt | Primary git checkpoint manager |
[[/SECTION:InfrastructureAgent:checkpoint-manager-git]]
[[SECTION:InfrastructureAgent:checkpoint-manager-alt]]
<!-- infra-version: 1.0.0 -->
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| checkpoint | INVOCATION_INTERVAL | 1 | continue | Alternate checkpoint manager |
[[/SECTION:InfrastructureAgent:checkpoint-manager-alt]]
[[SECTION:InfrastructureAgent:review-agent]]
<!-- infra-version: 1.0.0 -->
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | INVOCATION_INTERVAL | 1 | continue | Review agent |
[[/SECTION:InfrastructureAgent:review-agent]]
[[/INJECTION:InfrastructureAgents]]
