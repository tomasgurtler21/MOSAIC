---
version: "1.0"
name: "Empty Agent Entry Workflow"
id: bad-empty-agent
modes:
  - auto
infrastructure_agents:
  - agent-a
  - ""
---

Workflow with an empty string in infrastructure_agents. An empty entry is
invalid and Load must return an error.
