---
name: test-agent
triggers:
  - trigger: STAGE_END
    trigger_param: null
  - trigger: MANUAL
    trigger_param: "run-all"
tools:
  - alpha
  - beta
permissions: [read, write]
---

Agent body.
