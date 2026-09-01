<Workflow type="core" name="linear" version="1.0">
## Linear Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | - | plan.md |
| PLANNING | agent-b | FALSE | COMPLETE | - | plan.md | result.md |
</Workflow>
