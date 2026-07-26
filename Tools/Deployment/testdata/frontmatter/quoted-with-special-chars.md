---
version: "3.0"
name: "Quick Fix Workflow"
description: "Small changes, bug fixes, or well-understood modifications. Skips research and design."
hint: "Small fixes and bug fixes without research or design"
author: MOSAIC
id: quick-fix
referenced_agents:
  - planner-tdd-soft
  - plan-review
  - implementation-tdd
  - test-runner
artifacts:
  - Plan.md
  - Stage-*/Plan.md
  - Stage-*/PlanProgress.md
  - plan-review.md
  - Stage-{StageNumber}/Plan.md
  - Stage-{StageNumber}/PlanProgress.md
  - TestResults.md
---

Body content.
