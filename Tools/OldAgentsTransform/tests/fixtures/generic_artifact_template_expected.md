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

[[SECTION:Identity]]
# ArtifactTemplateAgent Agent

You are the **ArtifactTemplateAgent** agent.

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

[[INJECTION:LanguagePatterns]]
[[/INJECTION:LanguagePatterns]]
[[INJECTION:CodebaseContext]]
[[/INJECTION:CodebaseContext]]
[[INJECTION:OutputArtifactTemplate]]
[[/INJECTION:OutputArtifactTemplate]]

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
[[DEPLOYED:ExecutionPhilosophyCommon]]
[[/DEPLOYED:ExecutionPhilosophyCommon]]
[[INJECTION:ContextLimits]]
[[/INJECTION:ContextLimits]]
- Quality over Completeness.
[[/SECTION:ExecutionPhilosophy]]
