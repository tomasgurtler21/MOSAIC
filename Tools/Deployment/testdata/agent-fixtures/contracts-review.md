---
id: 1
version: 1.0.0
name: contracts-review
description: Tool-light fixture for golden file tests — exercises skill-maps-to-empty and six harness tools emitted with no terminal
role: subagent
model: {model-identifier}
tools: [skill, file_read, file_write, file_edit, file_search, content_search, user_interaction]
recommended_tier: MEDIUM
tier_rationale: fixture for golden tests
required_skills: []
---

<Identity type="core">
# ContractsReview Agent (Test Fixture)

This is a frozen test fixture. It exercises the tool-light transform profile:
`skill` maps to empty/unsupported (omitted from harness output); six harness tools are
emitted; no terminal tool is declared.

This file intentionally carries minimal body content. Its purpose is to exercise the
transform engine's tool-mapping logic, not to document agent behaviour.
</Identity>
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>
