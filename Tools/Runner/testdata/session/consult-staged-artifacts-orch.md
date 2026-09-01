<Workflow type="core" name="consult-staged-arts" version="1.0">
## Two-Stage Orchestrated Workflow with Template Artifact Paths

A two-stage EXECUTION workflow using the [StageNumber] template syntax with
agent-a. Each row declares Stage-{StageNumber}/Output.md in the Output column
so that tests can verify that consultant-routed dispatches (which fall back to
row.OutputArtifacts when the RoutingInstruction does not supply them) resolve
artifact template tokens before dispatching (D3b fix verification).

Row index mapping after plan expansion with Stage-1 and Stage-2 (zero-based):
  0 = EXECUTION.Stage-1 / agent-a  -- Output: Stage-1/Output.md (after resolution)
  1 = EXECUTION.Stage-2 / agent-a  -- Output: Stage-2/Output.md (after resolution)

Before the D3b fix, consultRoute passes the raw "Stage-{StageNumber}/Output.md"
path directly to the ProtocolRequest and CompletedStep without calling
engine.ResolveArtifacts. After the fix, the path is resolved to the
stage-specific form before dispatch.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | agent-a | FALSE | COMPLETE | - | - | Stage-{StageNumber}/Output.md |
</Workflow>
