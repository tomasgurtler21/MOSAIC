---
id: test-artifact-provenance-in-section
version: 1.0.0
name: test-agent
description: Invalid file - DEPLOYED:ArtifactProvenance nested inside Identity instead of body top level
---

<Identity type="core">
# TestAgent Agent

You are the TestAgent agent.

<ArtifactProvenance type="managed">
ArtifactProvenance placed inside Identity — must be at body top level.
</ArtifactProvenance>

</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<Capabilities type="core">
## Capabilities
Content here.
</Capabilities>

<Constraints type="core">
## Constraints
Content here.
</Constraints>

<ErrorHandling type="core">
## Error Handling
Content here.
</ErrorHandling>

<OutputFormat type="core">
## Output Format
Content here.
</OutputFormat>

<ExecutionPhilosophy type="core">
## Execution Philosophy
Content here.
</ExecutionPhilosophy>
