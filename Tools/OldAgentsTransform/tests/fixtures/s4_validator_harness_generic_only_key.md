---
id: 101
version: 2.0.0
transform_version: 2.0.0
name: harness-generic-only-key-test
description: Harness agent (has transform_version) carrying required_skills — a generic-only key that must be flagged by the validator on the harness path
required_skills: [lean-tdd]
---

[[SECTION:Identity]]
# HarnessGenericOnlyKeyTest Agent

You are the **HarnessGenericOnlyKeyTest** agent.

[[INJECTION:IdentityExtension]]
[[/INJECTION:IdentityExtension]]

[[/SECTION:Identity]]
---

[[DEPLOYED:CommunicationProtocol]]
[[/DEPLOYED:CommunicationProtocol]]

---

[[SECTION:Capabilities]]
## Capabilities

Fixture content.

[[/SECTION:Capabilities]]
---

[[SECTION:Constraints]]
## Constraints
[[DEPLOYED:ProtocolConstraints]]
[[/DEPLOYED:ProtocolConstraints]]

[[DEPLOYED:HarnessConstraints]]
[[/DEPLOYED:HarnessConstraints]]

[[/SECTION:Constraints]]
---

[[SECTION:ErrorHandling]]
## Error Handling
[[DEPLOYED:ErrorHandlingCommon]]
[[/DEPLOYED:ErrorHandlingCommon]]

[[/SECTION:ErrorHandling]]
---

[[SECTION:OutputFormat]]
## Output Format

Return JSON.

[[/SECTION:OutputFormat]]
---

[[SECTION:ExecutionPhilosophy]]
## Execution Philosophy
[[DEPLOYED:ExecutionPhilosophyCommon]]
[[/DEPLOYED:ExecutionPhilosophyCommon]]

[[/SECTION:ExecutionPhilosophy]]
