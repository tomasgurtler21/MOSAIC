[[SECTION:Workflow:staged]]
<!-- workflow-version: 1.0 -->
## Staged Workflow (Implementation-Only)

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | COMPLETE | implementation-tdd | Stage-{StageNumber}/Plan.md | Stage-{StageNumber}/implementation-review.md |
[[/SECTION:Workflow:staged]]

[[INJECTION:InfrastructureAgents]]
[[SECTION:InfrastructureAgent:commit-manager-git]]
<!-- infra-version: 1.0.0 -->
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| commit | STAGE_END | - | halt | Git commit manager (fires on stage transition) |
[[/SECTION:InfrastructureAgent:commit-manager-git]]
[[/INJECTION:InfrastructureAgents]]
