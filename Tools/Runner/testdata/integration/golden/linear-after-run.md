---
type: orchestration-artifact
workflow: linear
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 2
checkpoints: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "agent-b#2"
  error_code: null
---

[[SECTION:ExecutionLog]]
| Seq | Agent     | Phase    | Stage | Status  | Timestamp            | Summary       | Checkpoint |
| --- | --------- | -------- | ----- | ------- | -------------------- | ------------- | ---------- |
| 1   | agent-a#1 | PLANNING | -     | SUCCESS | 2026-01-01T00:00:00Z | planning done | -          |
| 2   | agent-b#2 | PLANNING | -     | SUCCESS | 2026-01-01T00:00:00Z | review done   | -          |
[[/SECTION:ExecutionLog]]

[[SECTION:Artifacts]]
| Artifact  | Created In | Created By |
| --------- | ---------- | ---------- |
| plan.md   | PLANNING   | agent-a#1  |
| result.md | PLANNING   | agent-b#2  |
[[/SECTION:Artifacts]]

[[SECTION:WorkflowNotes]]
| Seq | Note |
| --- | ---- |
[[/SECTION:WorkflowNotes]]
