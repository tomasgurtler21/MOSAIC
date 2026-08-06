---
id: test-artifact-provenance-extension-in-section
version: 1.0.0
name: test-agent
description: Invalid file - ArtifactProvenanceExtension nested inside Identity instead of body top level
---

[[SECTION:Identity]]
# TestAgent Agent

You are the TestAgent agent.

[[INJECTION:ArtifactProvenanceExtension]]
ArtifactProvenanceExtension placed inside Identity — must be at body top level.
[[/INJECTION:ArtifactProvenanceExtension]]

[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]

[[DEPLOYED:ArtifactProvenance]]
[[/DEPLOYED:ArtifactProvenance]]

[[SECTION:Capabilities]]
## Capabilities
Content here.
[[/SECTION:Capabilities]]

[[SECTION:Constraints]]
## Constraints
Content here.
[[/SECTION:Constraints]]

[[SECTION:ErrorHandling]]
## Error Handling
Content here.
[[/SECTION:ErrorHandling]]

[[SECTION:OutputFormat]]
## Output Format
Content here.
[[/SECTION:OutputFormat]]

[[SECTION:ExecutionPhilosophy]]
## Execution Philosophy
Content here.
[[/SECTION:ExecutionPhilosophy]]
