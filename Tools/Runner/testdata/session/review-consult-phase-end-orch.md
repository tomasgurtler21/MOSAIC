<Workflow type="core" name="linear" version="1.0">
## Phase-End Review Consultation Test Workflow

A two-phase workflow for testing that PHASE_END fires correctly on consultation
re-dispatch. Under prospective semantics, PHASE_END fires when the completed step
is the last row of its phase in the routing table (look-ahead), not by comparing
against a previous step's phase.

Row 0 (PLANNING/agent-a) is the last PLANNING row, so PHASE_END fires when
agent-a completes. The consultation re-dispatches row 0; PHASE_END fires again
because row 0 is still the last PLANNING row.

Row index mapping (zero-based):
  0 = PLANNING / agent-a
  1 = REVIEW   / agent-b

Note: REVIEW is used for the second phase rather than EXECUTION to avoid the
staged-EXECUTION special handling in the engine; REVIEW is a plain non-staged
phase, just like PLANNING.

Note: agent-b (row 1) is declared in the workflow but is never dispatched in the
test scenario; the consultation intercepts after the first PHASE_END on row 0
and re-dispatches row 0 before the workflow can advance to row 1.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b | - | - | - |
| REVIEW | agent-b | FALSE | COMPLETE | - | - | - |
</Workflow>

<InfrastructureAgents type="project">
<InfrastructureAgent type="core" name="review-agent-a" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | PHASE_END | - | continue | Review agent that fires on phase transitions |
</InfrastructureAgent>
</InfrastructureAgents>
