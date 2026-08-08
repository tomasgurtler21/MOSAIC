---
type: orchestration-artifact
run_id: "example-run"
workflow: greenfield-tdd
workflow_version: "3.4"
task: "Documentation-grade single-decision example"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:15:00Z
global_sequence: 3
checkpoints: disabled
commits: disabled
current_state:
  phase: COMPLETED
  stage: "-"
  last_status: SUCCESS
  last_agent: "planner#3"
  error_code: null
---

[[SECTION:ExecutionLog]]
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |
|-----|-------|-------|-------|--------|-----------|---------|--------|------------|
| 1 | requirements-refinement#1 | EXECUTION | - | SUCCESS | 2026-01-01T00:05:00Z | Requirements captured. | - | - |
| 2 | researcher#2 | EXECUTION | - | SUCCESS | 2026-01-01T00:10:00Z | Research complete. | - | - |
| 3 | planner#3 | EXECUTION | - | SUCCESS | 2026-01-01T00:15:00Z | Continued the workflow to completion. | - | - |
[[/SECTION:ExecutionLog]]
