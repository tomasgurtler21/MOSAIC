<Workflow type="core" name="authored-workflow" version="2.0">
## Authored Workflow

An authored workflow declared directly with type="core" (NodeSection shape).

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| PLANNING | planner | ✅ | - | Plan.md |
</Workflow>

<Workflow type="managed" name="managed-workflow" version="5.1">
## Managed Workflow

A deploy-managed workflow declared with type="managed" (NodeDeployed shape).

| Phase | Subagent | HITL | Input | Output |
|-------|----------|:----:|-------|--------|
| EXECUTION | implementer | ❌ | Plan.md | Result.md |
</Workflow>
