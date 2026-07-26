---
version: "3.0"
name: "Quick Fix Workflow"
description: "Small changes, bug fixes, or well-understood modifications."
id: quick-fix
referenced_agents:
  - planner-tdd-soft
  - plan-review
  - implementation-tdd
  - test-runner
artifacts:
  - Plan.md
  - Stage-*/Plan.md
  - Stage-{StageNumber}/Plan.md
---

Body content.
