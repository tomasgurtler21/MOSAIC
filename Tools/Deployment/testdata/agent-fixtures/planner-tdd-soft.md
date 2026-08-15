---
id: 3
version: 1.0.0
name: planner-tdd-soft
description: Skill-using fixture for golden file tests — exercises skill plus terminal alongside the full file-access tool set
role: subagent
model: {model-identifier}
tools: [skill, file_read, file_write, file_edit, file_search, content_search, terminal, user_interaction]
recommended_tier: HIGH
tier_rationale: fixture for golden tests
required_skills: []
---

<Identity type="core">
# Planner TDD Agent (Test Fixture)

This is a frozen test fixture. It exercises the skill-using transform profile:
both `skill` and `terminal` are declared alongside the full file-access tool set.
`skill` maps to an empty or supported harness tool depending on the harness;
`terminal` maps to the harness bash/execute tool and must appear in the output.

This file intentionally carries minimal body content. Its purpose is to exercise the
transform engine's tool-mapping logic, not to document agent behaviour.
</Identity>
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>
