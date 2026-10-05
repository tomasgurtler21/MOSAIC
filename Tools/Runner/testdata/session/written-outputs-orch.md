<Workflow type="core" name="written-outputs" version="1.0">
## Written Outputs Workflow

A two-agent linear workflow where both agents are HITL-dispatched and declare a
concrete output artifact. Used to verify that only outputs an invocation
actually wrote are registered and gated.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | TRUE | agent-b | - | - | design.md |
| PLANNING | agent-b | TRUE | COMPLETE | - | design.md | result.md |
</Workflow>
