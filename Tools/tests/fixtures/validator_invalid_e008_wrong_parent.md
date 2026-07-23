---
id: test-validator-e008
version: 1.0.0
name: test-agent
description: Invalid file - injection nested inside wrong parent section (E008)
---

[[SECTION:Identity]]
# TestAgent Agent
Content here. LanguagePatterns injection should be inside Capabilities, not Identity.
[[INJECTION:LanguagePatterns]]
Language patterns content placed in wrong parent section.
[[/INJECTION:LanguagePatterns]]
[[/SECTION:Identity]]
---
[[SECTION:CommunicationProtocol]]
## Communication Protocol
Content here.
[[/SECTION:CommunicationProtocol]]
---
[[SECTION:Capabilities]]
## Capabilities
Content here (no LanguagePatterns injection here because it was already placed above).
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
