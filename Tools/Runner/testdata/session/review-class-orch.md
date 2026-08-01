[[SECTION:Workflow:linear]]
<!-- workflow-version: 1.0 -->
## Linear Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | ❌ | agent-b | - | - | plan.md |
| PLANNING | agent-b | ❌ | COMPLETE | - | plan.md | result.md |
[[/SECTION:Workflow:linear]]

[[INJECTION:InfrastructureAgents]]
[[SECTION:InfrastructureAgent:review-agent-a]]
<!-- infra-version: 1.0.0 -->
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | INVOCATION_INTERVAL | 1 | continue | Review agent A |
[[/SECTION:InfrastructureAgent:review-agent-a]]
[[SECTION:InfrastructureAgent:review-agent-b]]
<!-- infra-version: 1.0.0 -->
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | INVOCATION_INTERVAL | 1 | continue | Review agent B |
[[/SECTION:InfrastructureAgent:review-agent-b]]
[[/INJECTION:InfrastructureAgents]]
