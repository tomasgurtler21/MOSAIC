<Workflow type="core" name="consult-staged" version="1.0">
## Two-Stage Orchestrated Workflow for Stage Context Tests

A two-stage EXECUTION workflow using the [StageNumber] template syntax with
agent-a and agent-b. Requires Plan.md with Stage-1 and Stage-2 in RunFolder
for template expansion. Used to verify that consultant-routed dispatches
preserve current_state.stage when the run has advanced past the first stage.

Row index mapping after plan expansion with Stage-1 and Stage-2 (zero-based):
  0 = EXECUTION.Stage-1 / agent-a
  1 = EXECUTION.Stage-1 / agent-b
  2 = EXECUTION.Stage-2 / agent-a
  3 = EXECUTION.Stage-2 / agent-b

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | agent-b | - | - | - |
| EXECUTION.[StageNumber] | agent-b | FALSE | COMPLETE | agent-a | - | - |
</Workflow>
