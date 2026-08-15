---
id: 4
version: 1.1.0
name: artifact-template-agent
description: An agent whose Capabilities section contains a fenced code block with lookalike headings followed by empty injection markers
tools: [file_read, file_write]
role: subagent
required_skills: []
recommended_tier: TODO
tier_rationale: "TODO: state why this tier"
---

<Identity type="core">
# ArtifactTemplateAgent Agent

You are the **ArtifactTemplateAgent** agent.

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

### Core Capabilities
- Do useful things

### Output Artifact Template

Your output artifact MUST follow this structure:

```markdown
# Report Title

## Summary
[Brief overview]

## Findings

### Critical (Blocks Approval)
- [Issue description]

## Recommendations
- [Recommendation 1]
```

**Key Points:**
- Always include all sections above

<LanguagePatterns type="project">
</LanguagePatterns>
<CodebaseContext type="project">
</CodebaseContext>
<OutputArtifactTemplate type="project">
</OutputArtifactTemplate>

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

- Retry transient errors

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
- Quality over Completeness.
</ExecutionPhilosophy>
