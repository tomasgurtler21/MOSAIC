<Workflow type="core" name="linear" version="1.0">
## HITL Linear Workflow

A two-agent linear workflow where the first agent requires HITL review.
Used in Stage 6 tests for HITL compliance verification.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | TRUE | agent-b | - | - | plan.md |
| PLANNING | agent-b | TRUE | COMPLETE | - | plan.md | result.md |
</Workflow>
