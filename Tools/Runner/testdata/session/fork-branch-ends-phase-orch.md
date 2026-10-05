<Workflow type="core" name="fork-branch-ends-phase" version="1.0">
## Fork Branch Ends Phase Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b, agent-c | - | - | a.md |
| PLANNING | agent-b | FALSE | COMPLETE | - | a.md | b.md |
| PLANNING | agent-c | FALSE | COMPLETE | - | a.md | c.md |
</Workflow>
