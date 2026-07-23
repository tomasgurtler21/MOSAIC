---
id: test-validator-subset
version: 1.0.0
name: test-agent
description: Valid file with all 7 sections but only one injection
---

[[SECTION:Identity]]
# TestAgent Agent

You are the TestAgent agent in a multi-agent orchestration system.

**Goal:** Test that not all 12 injections are required.

[[INJECTION:IdentityExtension]]
Only this injection is present in this file.
[[/INJECTION:IdentityExtension]]

[[/SECTION:Identity]]
---

[[SECTION:CommunicationProtocol]]
## Communication Protocol

Protocol content without any injection.

[[/SECTION:CommunicationProtocol]]
---

[[SECTION:Capabilities]]
## Capabilities

Capabilities content without any injection.

[[/SECTION:Capabilities]]
---

[[SECTION:Constraints]]
## Constraints

Constraint content without any injection.

[[/SECTION:Constraints]]
---

[[SECTION:ErrorHandling]]
## Error Handling

Error handling content without any injection.

[[/SECTION:ErrorHandling]]
---

[[SECTION:OutputFormat]]
## Output Format

Output format content without any injection.

[[/SECTION:OutputFormat]]
---

[[SECTION:ExecutionPhilosophy]]
## Execution Philosophy

Execution philosophy content without any injection.

[[/SECTION:ExecutionPhilosophy]]
