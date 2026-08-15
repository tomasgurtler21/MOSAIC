---
version: 1.0.0
name: orchestrator
description: Placeholder-expanding fixture for golden file tests — exercises {tool-permissions} placeholder expansion and RoleOrchestrator
role: orchestrator
model: {model-identifier}
tools: {tool-permissions}
recommended_tier: HIGH
tier_rationale: fixture for golden tests
required_skills: []
---

<Identity type="core">
# Orchestrator Agent (Test Fixture)

This is a frozen test fixture. It exercises the placeholder-expanding transform profile:
`{tool-permissions}` is used as the tools value and must expand to the full harness tool
universe (the placeholder_expansion list in the harness descriptor). The role is
`RoleOrchestrator`, which selects a different key-ordering and expansion path than the
subagent role used by the other three profiles.

This file intentionally carries minimal body content. Its purpose is to exercise the
transform engine's placeholder expansion logic, not to document agent behaviour.
</Identity>
---

<CommunicationProtocol type="managed">
</CommunicationProtocol>
