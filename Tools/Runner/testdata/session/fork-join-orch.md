<Workflow type="core" name="fork-join" version="1.0">
## Fork Join Workflow

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | agent-a | FALSE | agent-b, agent-c | - | - | a.md |
| PLANNING | agent-b | FALSE | agent-d | - | a.md | b.md |
| PLANNING | agent-c | FALSE | agent-d | - | a.md | c.md |
| PLANNING | agent-d | FALSE | COMPLETE | - | b.md, c.md | d.md |
</Workflow>
