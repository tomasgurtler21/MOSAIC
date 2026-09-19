---
version: "1.0"
name: "Invalid Smoke Set Workflow"
id: bad-smoke
modes:
  - auto
smoke_set:
  - orchestrated
---

This workflow's smoke_set contains "orchestrated" which is not in its modes list.
