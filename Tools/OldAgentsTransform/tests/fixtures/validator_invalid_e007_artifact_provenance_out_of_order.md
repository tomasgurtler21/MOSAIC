---
id: test-artifact-provenance-out-of-order
version: 1.0.0
name: test-agent
description: Invalid file - DEPLOYED:ArtifactProvenance appears after Capabilities, violating canonical order
---

[[SECTION:Identity]]
# TestAgent Agent
Content here.
[[/SECTION:Identity]]

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]

[[SECTION:Capabilities]]
## Capabilities
Content here (appears at canonical slot 3, before ArtifactProvenance at slot 2 — out of order).
[[/SECTION:Capabilities]]

[[DEPLOYED:ArtifactProvenance]]
[[/DEPLOYED:ArtifactProvenance]]

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
