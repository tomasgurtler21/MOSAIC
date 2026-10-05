<Workflow type="core" name="staged" version="1.0">
## Staged Workflow With HITL On The Last Step

A staged EXECUTION workflow whose last step of every stage (implementation-review)
requires HITL approval of its output. The first step declares no output so it never
takes part in approval checks. Requires Plan.md in RunFolder for template expansion.

Two infrastructure agents are declared: a STAGE_END commit agent and a PHASE_END
review agent, so tests can observe when each boundary trigger fires.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | - | - |
| EXECUTION.[StageNumber] | implementation-review | TRUE | COMPLETE | implementation-tdd | - | Stage-{StageNumber}/implementation-review.md |
</Workflow>

<InfrastructureAgents type="managed">
<InfrastructureAgent type="core" name="commit-manager-git" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| commit | STAGE_END | - | halt | Git commit manager (fires at stage end) |
</InfrastructureAgent>
<InfrastructureAgent type="core" name="review-agent-a" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | PHASE_END | - | continue | Review agent that fires at end of EXECUTION phase |
</InfrastructureAgent>
</InfrastructureAgents>
