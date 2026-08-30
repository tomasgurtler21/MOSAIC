<Workflow type="core" name="hitl-glob-selfref-downstream" version="1.0">
## HITL Glob Self-Referential Downstream Workflow

A two-step workflow where planner (HITL=TRUE) produces Stage-*/Plan.md output
artifacts with no preceding stage-establishing row, and plan-review (HITL=FALSE)
immediately follows and consumes Stage-*/Plan.md as an input artifact. Used to
verify that the stage set established during planner's HITL check is available
for the downstream input-artifact consumer, and that the run completes via the
intended path rather than through HITL escalation.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | TRUE | plan-review | - | - | Stage-*/Plan.md |
| PLANNING | plan-review | FALSE | COMPLETE | - | Stage-*/Plan.md | - |
</Workflow>
