<Workflow type="core" name="stage-star-output" version="1.0">
## Stage-Star Output Workflow

Workflow where the first row (planner) produces Stage-* output artifacts.
After the planner completes, the session must re-read the stage set so that
the reviewer row can expand Stage-*/Plan.md into per-stage paths.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner | ❌ | reviewer | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | reviewer | ❌ | COMPLETE | planner | Plan.md, Stage-*/Plan.md | review.md |
</Workflow>
