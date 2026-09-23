---
version: "1.0"
name: "No Infra Fields Workflow"
id: workflow-no-infra
modes:
  - auto
---

Workflow with no infrastructure_agents, checkpoints, or commits fields.
Absent fields must default to: InfrastructureAgents=[]string{} (non-nil empty),
Checkpoints="disabled", Commits="disabled".
