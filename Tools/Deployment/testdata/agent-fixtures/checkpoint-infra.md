---
id: 5
version: 1.0.0
name: test-runner
description: Tool-heavy fixture for golden file tests — exercises all seven generic tools including terminal
role: subagent
model: {model-identifier}
tools: [file_read, file_write, file_edit, file_search, content_search, terminal, user_interaction]
recommended_tier: MEDIUM
tier_rationale: fixture for golden tests
required_skills: []
infrastructure: checkpoint
triggers:
  - trigger: STAGE_END
on_failure: halt
---

<Identity type="core">
# CheckpointInfra Agent (Test Fixture)

This fixture is identical to the test-runner fixture except that it carries the three
source-only infrastructure keys (infrastructure, triggers, on_failure).
</Identity>
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>
