<Workflow type="core" name="hitl-glob-selfref" version="1.0">
## HITL Glob Self-Referential Workflow

A single-step workflow where planner (HITL=TRUE) is both the Stage-* output
producer and the HITL subject. There is no preceding row to establish a stage
set, so the session must derive stages from planner's own output before the
HITL compliance check. Used in session-level tests for the self-referential
HITL+Stage-* glob approval scenario.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | TRUE | COMPLETE | - | - | Stage-*/Plan.md |
</Workflow>
