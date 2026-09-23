<Workflow type="core" name="staged" version="1.0">
## Two-Stage EXECUTION Workflow for PHASE_END Prospective Semantics Test

A two-stage EXECUTION workflow using the [StageNumber] template syntax with
agent-a and agent-b. Requires Plan.md with Stage-1 and Stage-2 in RunFolder
for template expansion.

Used to verify that EXECUTION PHASE_END fires only after the last step of the
*last* stage (the last step of the entire EXECUTION phase), not after the last
step of each individual stage.

FR-3 prospective semantics: the look-ahead for EXECUTION PHASE_END spans the
entire EXECUTION phase across all stages. PHASE_END fires only when the
completed row is the last row of the last stage in the stage set.

Row index mapping after plan expansion with Stage-1 and Stage-2 (zero-based):
  0 = EXECUTION.Stage-1 / agent-a
  1 = EXECUTION.Stage-1 / agent-b  (last Stage-1 step, but NOT last EXECUTION step)
  2 = EXECUTION.Stage-2 / agent-a
  3 = EXECUTION.Stage-2 / agent-b  (last Stage-2 step = last EXECUTION step: PHASE_END fires)

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | agent-b | - | - | - |
| EXECUTION.[StageNumber] | agent-b | FALSE | COMPLETE | agent-a | - | - |
</Workflow>

<InfrastructureAgents type="project">
<InfrastructureAgent type="core" name="review-agent-a" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | PHASE_END | - | continue | Review agent that fires at end of EXECUTION phase |
</InfrastructureAgent>
</InfrastructureAgents>
