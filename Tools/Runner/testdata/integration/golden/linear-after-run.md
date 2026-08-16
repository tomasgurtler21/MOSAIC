---
type: orchestration-artifact
workflow: linear
workflow_version: "1.0"
task: "test task"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:00:00Z
global_sequence: 2
mode: auto
checkpoints: disabled
commits: disabled
commit_branch_variant: mosaic-owned
pre_consultation: disabled
manual_resolution: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "agent-b#2"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent     | Phase    | Stage | Status  | Timestamp            | Summary       | Inputs  | Checkpoint |
| --- | --------- | -------- | ----- | ------- | -------------------- | ------------- | ------- | ---------- |
| 1   | agent-a#1 | PLANNING | -     | SUCCESS | 2026-01-01T00:00:00Z | planning done | -       | -          |
| 2   | agent-b#2 | PLANNING | -     | SUCCESS | 2026-01-01T00:00:00Z | review done   | plan.md | -          |
</ExecutionLog>

<Artifacts type="core">
| Artifact  | Created In | Created By |
| --------- | ---------- | ---------- |
| plan.md   | PLANNING   | agent-a#1  |
| result.md | PLANNING   | agent-b#2  |
</Artifacts>

<WorkflowNotes type="core">
| Seq | Note |
| --- | ---- |
</WorkflowNotes>
