---
id: test-validator-e008
version: 1.0.0
name: test-agent
description: Invalid file - injection nested inside wrong parent section (E008)
---

[[SECTION:Identity]]
# TestAgent Agent
Content here. CodebaseContext injection should be inside Capabilities, not Identity.
[[INJECTION:CodebaseContext]]
Codebase context placed in wrong parent section.
[[/INJECTION:CodebaseContext]]
[[/SECTION:Identity]]
---

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]
---

[[DEPLOYED:ArtifactProvenance]]
[[/DEPLOYED:ArtifactProvenance]]

---
[[SECTION:Capabilities]]
## Capabilities
Content here (CodebaseContext already placed incorrectly above).
[[/SECTION:Capabilities]]
---
[[SECTION:Constraints]]
## Constraints
Content here.
[[/SECTION:Constraints]]
---
[[SECTION:ErrorHandling]]
## Error Handling
Content here.
[[/SECTION:ErrorHandling]]
---
[[SECTION:OutputFormat]]
## Output Format
Content here.
[[/SECTION:OutputFormat]]
---
[[SECTION:ExecutionPhilosophy]]
## Execution Philosophy
Content here.
[[/SECTION:ExecutionPhilosophy]]
