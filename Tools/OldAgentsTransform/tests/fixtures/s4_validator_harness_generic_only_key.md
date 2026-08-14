---
id: 101
version: 2.0.0
transform_version: 2.0.0
name: harness-generic-only-key-test
description: Harness agent (has transform_version) carrying required_skills — a generic-only key that must be flagged by the validator on the harness path
required_skills: [lean-tdd]
---

<Identity type="core">
# HarnessGenericOnlyKeyTest Agent

You are the **HarnessGenericOnlyKeyTest** agent.

<IdentityExtension type="project">
</IdentityExtension>

</Identity>
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>

---

<Capabilities type="core">
## Capabilities

Fixture content.

</Capabilities>
---

<Constraints type="core">
## Constraints
<ProtocolConstraints type="managed">
</ProtocolConstraints>

<HarnessConstraints type="managed">
</HarnessConstraints>

</Constraints>
---

<ErrorHandling type="core">
## Error Handling
<ErrorHandlingCommon type="managed">
</ErrorHandlingCommon>

</ErrorHandling>
---

<OutputFormat type="core">
## Output Format

Return JSON.

</OutputFormat>
---

<ExecutionPhilosophy type="core">
## Execution Philosophy
<ExecutionPhilosophyCommon type="managed">
</ExecutionPhilosophyCommon>

</ExecutionPhilosophy>
