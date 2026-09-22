---
version: "1.0"
name: "Bad Commits Workflow"
id: bad-commits
modes:
  - auto
commits: Enabled
---

Workflow with an invalid commits value. "Enabled" (capital E) is not valid;
matching is case-sensitive so only "enabled" is accepted. Load must return an error.
