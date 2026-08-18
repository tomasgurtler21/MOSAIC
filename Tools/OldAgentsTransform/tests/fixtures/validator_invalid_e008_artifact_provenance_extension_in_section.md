---
id: test-artifact-provenance-extension-in-section
version: 1.0.0
name: test-agent
description: Invalid file - ArtifactProvenanceExtension nested inside Identity instead of body top level
---

<Identity type="core">
# TestAgent Agent

You are the TestAgent agent.

<ArtifactProvenanceExtension type="project">
ArtifactProvenanceExtension placed inside Identity — must be at body top level.
</ArtifactProvenanceExtension>

</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<ArtifactProvenance type="managed">
</ArtifactProvenance>

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
