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

<Identity type="core">
# FencedMarkersAgent Agent

You are the **FencedMarkersAgent** agent.

<IdentityExtension type="project">
</IdentityExtension>
<ClosingProcedure type="managed">
</ClosingProcedure>
<AuthorityHierarchy type="managed">
</AuthorityHierarchy>

</Identity>
---

<Capabilities type="core">
## Capabilities

### Example Syntax

The following lines appear verbatim inside a fenced code block and must not be converted:

```text
[INJECTION: identity_extension]
<IdentityExtension type="project">
```

<LanguagePatterns type="project">
</LanguagePatterns>

</Capabilities>
---

<Constraints type="core">
## Constraints
<ProtocolConstraints type="managed">
</ProtocolConstraints>

- Stay within scope

<HarnessConstraints type="managed">
</HarnessConstraints>

</Constraints>
---

<ErrorHandling type="core">
## Error Handling
<ErrorHandlingCommon type="managed">
</ErrorHandlingCommon>

- Retry errors once before escalating

<ErrorHandlingExtension type="project">
</ErrorHandlingExtension>

</ErrorHandling>
---

<ExecutionPhilosophy type="core">
## Execution Philosophy

- Context Management: Dedicate full context window.
<ExecutionPhilosophyCommon type="managed">
</ExecutionPhilosophyCommon>
<ContextLimits type="project">
</ContextLimits>
</ExecutionPhilosophy>
