---
version: "1.0"
name: "Duplicate Agents Workflow"
id: bad-duplicate-agents
modes:
  - auto
infrastructure_agents:
  - agent-a
  - agent-a
---

Workflow with duplicate entries in infrastructure_agents. Duplicate agent keys
are invalid and Load must return an error.
