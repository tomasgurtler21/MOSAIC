---
version: "1.0"
name: "Multi-Mode Infra Workflow"
id: workflow-multi-mode
modes:
  - auto
  - auto-review
infrastructure_agents:
  - agent-b
  - agent-c
commits: enabled
---

Multi-mode workflow with infrastructure_agents. Used to verify that
UnionInfrastructureAgentKeys does not produce duplicate entries when a
workflow declares multiple modes (each mode is one CatalogEntry but
infrastructure_agents is declared once per workflow).
