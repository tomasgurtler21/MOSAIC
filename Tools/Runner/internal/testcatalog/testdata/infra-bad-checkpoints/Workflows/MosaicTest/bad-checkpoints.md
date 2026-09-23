---
version: "1.0"
name: "Bad Checkpoints Workflow"
id: bad-checkpoints
modes:
  - auto
checkpoints: yes
---

Workflow with an invalid checkpoints value. "yes" is not a valid value;
only "enabled" and "disabled" are accepted. Load must return an error.
