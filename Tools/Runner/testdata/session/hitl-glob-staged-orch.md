<Workflow type="core" name="hitl-glob-staged" version="1.0">
## HITL Glob-Staged Workflow

A two-step workflow where planner (HITL=false) produces Stage-* output artifacts
to trigger stage-set derivation, and agent-a (HITL=true) produces Stage-* output
artifacts that the session must expand before performing HITL approval checks.

Used in session-level tests for HITL + Stage-* glob approval (Stage 1 fix).

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | FALSE | agent-a | - | - | Stage-*/Plan.md |
| PLANNING | agent-a | TRUE | COMPLETE | - | - | Stage-*/Plan.md |
</Workflow>
