---
version: "1.0"
name: "No Section Workflow"
description: "A workflow file that exists on disk but contains no section block."
hint: "missing block"
author: Fixture
id: no-section-workflow
referenced_agents: []
---

This workflow file is intentionally missing the [[SECTION:Workflow:no-section-workflow]] block.
WorkflowSection("no-section-workflow") must return an error because the block is absent.

There is prose here, but no section boundary tags at all.
