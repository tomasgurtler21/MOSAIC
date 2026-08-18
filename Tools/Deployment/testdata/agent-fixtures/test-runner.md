---
id: 2
version: 1.0.0
name: test-runner
description: Tool-heavy fixture for golden file tests — exercises all seven generic tools including terminal
role: subagent
model: {model-identifier}
tools: [file_read, file_write, file_edit, file_search, content_search, terminal, user_interaction]
recommended_tier: MEDIUM
tier_rationale: fixture for golden tests
required_skills: []
---

<Identity type="core">
# TestRunner Agent (Test Fixture)

This is a frozen test fixture. It exercises the tool-heavy transform profile:
all seven generic tools are declared, including `terminal`. The output must list all
corresponding harness tools in universe order.

This file intentionally carries minimal body content. Its purpose is to exercise the
transform engine's tool-mapping logic, not to document agent behaviour.
</Identity>
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>
