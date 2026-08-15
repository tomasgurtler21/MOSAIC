---
version: 2.3.0
name: communication_protocol_input
role: subagent
required_skills: []
recommended_tier: TODO
tier_rationale: "TODO: state why this tier"
---

<Identity type="core">
# TestAgent Agent

You are the **TestAgent** agent in a multi-agent orchestration system.

**Goal:** Test the communication protocol transformer path.

<IdentityExtension type="project">
</IdentityExtension>
<ClosingProcedure type="managed">
</ClosingProcedure>
<AuthorityHierarchy type="managed">
</AuthorityHierarchy>
</Identity>
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<ProtocolExtension type="project">
</ProtocolExtension>
---

<Capabilities type="core">
## Capabilities

### Core Capabilities
- Handle tasks efficiently

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

- Do NOT do bad things

<HarnessConstraints type="managed">
</HarnessConstraints>

</Constraints>
---

<ErrorHandling type="core">
## Error Handling
<ErrorHandlingCommon type="managed">
</ErrorHandlingCommon>

<ErrorHandlingExtension type="project">
</ErrorHandlingExtension>

</ErrorHandling>
---

<ExecutionPhilosophy type="core">
## Execution Philosophy

- **Context Management:** Dedicate your full context window to this task.
<ExecutionPhilosophyCommon type="managed">
</ExecutionPhilosophyCommon>
<ContextLimits type="project">
</ContextLimits>
- **Quality over Completeness:** Stop at a good stopping point.
</ExecutionPhilosophy>
