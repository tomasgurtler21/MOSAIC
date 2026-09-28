<Workflow type="core" name="hitl-review" version="1.0">
## HITL Review Workflow

A three-agent workflow with a HITL-dispatched reviewer. The reviewer routes
findings back to agent-a and success on to agent-c. Used to verify which status
is routed and recorded after a HITL re-dispatch.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | - | design.md |
| PLANNING | agent-b | TRUE | agent-c | agent-a | design.md | review.md |
| PLANNING | agent-c | FALSE | COMPLETE | - | review.md | result.md |
</Workflow>
