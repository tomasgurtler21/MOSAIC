---
version: "1.0"
name: "MosaicTest Deviation Stop Workflow"
description: "Runner mode fixture — Auto mode deviation where the orchestrator stops the run instead of re-dispatching. Proves the stop path works from a deviation context and that the artifact is left in a resumable state."
hint: "Mode 2 test — deviation followed by orchestrator stop, artifact resumability"
author: MOSAIC
id: deviation-stop
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/deviation-stop-fail.md
---

<Workflow type="core" name="deviation-stop" version="1.0">
## MosaicTest Deviation Stop Workflow

**Use when:** Verifying that the orchestrator can stop a run after a deviation, and that the resulting artifact is left in a state where the run can be resumed later.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | FALSE | COMPLETE | - | MosaicTestScript/deviation-stop-fail.md | - |

**Notes:**
- **Run this workflow in Auto mode.**
- The script unconditionally returns `BLOCKED`/`E501`. The engine deviates. The orchestrator stops the run.
- The run should end with `RunStoppedByConsultant`, not `RunDeviationUnresolved`.
- After the run, the artifact's `current_state` should reflect the BLOCKED and be resumable.
- Seed `Fixtures/deviation-stop` — the whole directory, not anything inside it.

</Workflow>

---

## Design Rationale

### Why a separate workflow from deviation-blocked

`deviation-blocked` proves the orchestrator can re-dispatch after a deviation. This workflow proves the other half: the orchestrator can **stop** after a deviation. The two outcomes are different code paths in the session (dispatch instruction vs stop instruction after a deviation consultation).

### Why BLOCKED/E501

`E501` (`TOOL_UNAVAILABLE`) is a fixture choice — it does not matter which error code triggers the deviation, only that BLOCKED does. `E501` is chosen because it is visually distinct from `E401` used in `deviation-blocked`, making the two workflows easy to tell apart in logs.

### Resumability

The stop instruction leaves the artifact exactly as it stands: `current_state.last_agent` names the BLOCKED agent, `current_state.last_status` is BLOCKED. A resumed run in Orchestrated mode would consult the orchestrator with that state. In Auto mode, the engine would see BLOCKED and deviate again. Both paths are valid resume points.

This workflow does not test the resume itself — that is a manual procedure documented in the design. It tests only that the artifact is **left in a state where resume is possible**, meaning `current_state` is consistent with the last Execution Log entry.

---

## Expected Run

One Orchestration.md log row. The stop consultation is not logged.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 1 | `mosaictest-scripted#1` | workflow step | RESEARCH | BLOCKED | fixture-declared tool unavailable, E501 |

**Run outcome:** stopped by the orchestrator (`RunStoppedByConsultant`), with the fixture's stop reason surfaced in the exit message.

**Artifact check:** After the run, `Orchestration.md` frontmatter should show:
- `current_state.last_agent: mosaictest-scripted#1`
- `current_state.last_status: BLOCKED`
- `current_state.error_code: E501`

**RunnerLogs verification:** The debug log must contain:
- A `session.deviation` entry with `kind=non-success-status`
- A `session.consult.stop` entry with the fixture's stop reason

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Run stops with `RunDeviationUnresolved` instead of `RunStoppedByConsultant` | No routing consultant configured (RUN-4), or the consultation failed |
| Consultation succeeds but the run does not stop | The stop instruction was not parsed correctly — check the wire response format |
| `current_state` does not match the last log entry | The artifact was corrupted during the stop path — the session may have written an inconsistent state |
| No log rows at all | The BLOCKED was caught before the Execution Log was written — check that Store.Apply runs before deviation handling |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-17 | MOSAIC | Initial version. Deviation stop and artifact resumability. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A follow-up run that resumes this artifact in Orchestrated mode, proving the resume path from a deviation-stopped artifact actually works.

**Dead ends (tried and rejected):**
- Testing resume as part of this workflow. Resume requires killing and restarting the process, which no workflow definition can express.
