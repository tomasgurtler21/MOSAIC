---
id: test-artifact-provenance-out-of-order
version: 1.0.0
name: test-agent
description: Invalid file - DEPLOYED:ArtifactProvenance appears after Capabilities, violating canonical order
---

<Identity type="core">
# TestAgent Agent
Content here.
</Identity>

<CommunicationProtocol type="managed">
</CommunicationProtocol>

<Capabilities type="core">
## Capabilities
Content here (appears at canonical slot 3, before ArtifactProvenance at slot 2 — out of order).
</Capabilities>

<ArtifactProvenance type="managed">
</ArtifactProvenance>

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
