---
id: 7
version: 1.1.0
name: artifact-move-agent-after
description: Generic agent with an after-marker artifact block for OutputArtifactTemplate order-independence testing
tools: [file_read, file_write]
role: subagent
required_skills: []
recommended_tier: TODO
tier_rationale: "TODO: state why this tier"
---

<Identity type="core">
# ArtifactMoveAgentAfter Agent

You are the **ArtifactMoveAgentAfter** agent.

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

<LanguagePatterns type="project">
</LanguagePatterns>
<CodebaseContext type="project">
</CodebaseContext>
<OutputArtifactTemplate type="project">
### Design Artifact Structure

Your output artifact must follow this structure:

- `run_id` — copied verbatim from the task invocation's `run_id` field
- `created_by` — your own `agent_instance_id`
- `human_approved` — `false`
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
