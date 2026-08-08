---
id: 10
version: 2.3.0
name: fenced-markers-agent
description: Agent with a fenced code block containing injection-marker-like text, to test that fenced lines are never converted to boundary tags
tools: [file_read, file_write]
role: subagent
required_skills: []
recommended_tier: TODO
tier_rationale: "TODO: state why this tier"
---

[[SECTION:Identity]]
# FencedMarkersAgent Agent

You are the **FencedMarkersAgent** agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]
[[DEPLOYED:ClosingProcedure]]
[[/DEPLOYED:ClosingProcedure]]
[[DEPLOYED:AuthorityHierarchy]]
[[/DEPLOYED:AuthorityHierarchy]]

[[/SECTION:Identity]]
---

[[SECTION:Capabilities]]
## Capabilities

### Example Syntax

The following lines appear verbatim inside a fenced code block and must not be converted:

```text
[INJECTION: identity_extension]
[[INJECTION:IdentityExtension]]
```

[[INJECTION:LanguagePatterns]]
[[/INJECTION:LanguagePatterns]]

[[/SECTION:Capabilities]]
---

[[SECTION:Constraints]]
## Constraints
[[DEPLOYED:ProtocolConstraints]]
[[/DEPLOYED:ProtocolConstraints]]

- Stay within scope

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]

[[/SECTION:Constraints]]
---

[[SECTION:ErrorHandling]]
## Error Handling
[[DEPLOYED:ErrorHandlingCommon]]
[[/DEPLOYED:ErrorHandlingCommon]]

- Retry errors once before escalating

[[INJECTION:ErrorHandlingExtension]]
[[/INJECTION:ErrorHandlingExtension]]

[[/SECTION:ErrorHandling]]
---

[[SECTION:OutputFormat]]
## Output Format

Return a JSON status block.

[[/SECTION:OutputFormat]]
---

[[SECTION:ExecutionPhilosophy]]
## Execution Philosophy

- Context Management: Dedicate full context window.
[[DEPLOYED:ExecutionPhilosophyCommon]]
[[/DEPLOYED:ExecutionPhilosophyCommon]]
[[INJECTION:ContextLimits]]
[[/INJECTION:ContextLimits]]
[[/SECTION:ExecutionPhilosophy]]
