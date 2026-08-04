---
id: 4
version: 2.0.0
name: artifact-template-agent
description: An agent whose Capabilities section contains a fenced code block with lookalike headings followed by empty injection markers
model: {model-identifier}
tools: [file_read, file_write]
---

[[SECTION:Identity]]
# ArtifactTemplateAgent Agent

You are the **ArtifactTemplateAgent** agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]

[[/SECTION:Identity]]
---

[[SECTION:Capabilities]]
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

[[DEPLOYED:LanguagePatterns]]
[[/DEPLOYED:LanguagePatterns]]
[[INJECTION:CodebaseContext]]
[[/INJECTION:CodebaseContext]]
[[INJECTION:OutputArtifactTemplate]]
[[/INJECTION:OutputArtifactTemplate]]

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

- Retry transient errors

[[INJECTION:ErrorHandlingExtension]]
[[/INJECTION:ErrorHandlingExtension]]

[[/SECTION:ErrorHandling]]
---

[[SECTION:OutputFormat]]
## Output Format

Always end with a JSON status block.

[[/SECTION:OutputFormat]]
---

[[SECTION:ExecutionPhilosophy]]
## Execution Philosophy

- Context Management: Dedicate full context window.
[[INJECTION:ContextLimits]]
[[/INJECTION:ContextLimits]]
- Quality over Completeness.
[[/SECTION:ExecutionPhilosophy]]
