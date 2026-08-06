---
id: 10
version: 3.0.0
name: fenced-markers-agent
description: Agent with a fenced code block containing injection-marker-like text, to test that fenced lines are never converted to boundary tags
model: {model-identifier}
tools: [file_read, file_write]
---

[[SECTION:Identity]]
# FencedMarkersAgent Agent

You are the **FencedMarkersAgent** agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]

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

[[DEPLOYED:LanguagePatterns]]
[[/DEPLOYED:LanguagePatterns]]

[[/SECTION:Capabilities]]
---

[[SECTION:Constraints]]
## Constraints

- Stay within scope

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]
[[DEPLOYED:CustomConstraints]]
[[/DEPLOYED:CustomConstraints]]

[[/SECTION:Constraints]]
---

[[SECTION:ErrorHandling]]
## Error Handling

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
[[INJECTION:ContextLimits]]
[[/INJECTION:ContextLimits]]
[[/SECTION:ExecutionPhilosophy]]
