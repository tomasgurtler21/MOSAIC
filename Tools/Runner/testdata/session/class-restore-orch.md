[[SECTION:Workflow:linear]]
<!-- workflow-version: 1.0 -->
## Linear Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | ❌ | agent-b | - | - | plan.md |
| PLANNING | agent-b | ❌ | COMPLETE | - | plan.md | result.md |
[[/SECTION:Workflow:linear]]

[[INJECTION:InfrastructureAgents]]
[[SECTION:InfrastructureAgent:checkpoint-restore-s3]]
<!-- infra-version: 1.0.0 -->
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| restore | INVOCATION_INTERVAL | 1 | halt | S3-based restore agent (must never be auto-triggered) |
[[/SECTION:InfrastructureAgent:checkpoint-restore-s3]]
[[SECTION:InfrastructureAgent:checkpoint-manager-git]]
<!-- infra-version: 1.0.0 -->
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| checkpoint | INVOCATION_INTERVAL | 999 | continue | Placeholder checkpoint agent to satisfy checkpoint precondition |
[[/SECTION:InfrastructureAgent:checkpoint-manager-git]]
[[/INJECTION:InfrastructureAgents]]
