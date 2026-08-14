---
id: test-validator-e008
version: 1.0.0
name: test-agent
description: Invalid file - injection nested inside wrong parent section (E008)
---

<Identity type="core">
# TestAgent Agent
Content here. CodebaseContext injection should be inside Capabilities, not Identity.
<CodebaseContext type="project">
Codebase context placed in wrong parent section.
</CodebaseContext>
</Identity>
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>
---

<ArtifactProvenance type="managed">
</ArtifactProvenance>

---
<Capabilities type="core">
## Capabilities
Content here (CodebaseContext already placed incorrectly above).
</Capabilities>
---
<Constraints type="core">
## Constraints
Content here.
</Constraints>
---
<ErrorHandling type="core">
## Error Handling
Content here.
</ErrorHandling>
---
<OutputFormat type="core">
## Output Format
Content here.
</OutputFormat>
---
<ExecutionPhilosophy type="core">
## Execution Philosophy
Content here.
</ExecutionPhilosophy>
