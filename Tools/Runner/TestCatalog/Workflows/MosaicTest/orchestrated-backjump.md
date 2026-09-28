---
version: "1.0"
name: "MosaicTest Orchestrated Backjump Workflow"
description: "Runner mode fixture — Orchestrated mode instruction overrides. The orchestrator dispatches the same row three times: once with no overrides, once with an input_artifacts override, and once with hitl=true. Proves that override fields reach the Runner and affect dispatch."
hint: "Mode 1 test — consultation override fields (input_artifacts, hitl_override)"
author: MOSAIC
id: orchestrated-backjump
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/backjump-echo.md
modes:
  - orchestrated
---

<Workflow type="core" name="orchestrated-backjump" version="1.0">
## MosaicTest Orchestrated Backjump Workflow

**Use when:** Verifying that the orchestrator's dispatch instruction override fields (`input_artifacts`, `hitl_override`) propagate through the Runner to the dispatched agent. Also confirms the Runner handles a BLOCKED result in Orchestrated mode (another consultation).

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | FALSE | COMPLETE | - | MosaicTestScript/backjump-echo.md | - |

**Notes:**
- **Run this workflow in Orchestrated mode only.**
- Three dispatches of the single row, each with different overrides. The third dispatch sets `hitl_override: true`, which causes the stub to return `BLOCKED`/`E503` (it has no user-interaction tool). The orchestrator then stops.
- Non-staged. No `Plan.md` required.
- Seed `Fixtures/orchestrated-backjump` — the whole directory, not anything inside it.

</Workflow>

---

## Design Rationale

### What "backjump" means here

The design document names this workflow for the orchestrator sending the run back to an earlier row. True multi-row backjump is blocked by first-match resolution: when the orchestrator names an agent, the Runner resolves to the first matching row, so two rows with the same agent are indistinguishable. That gap is documented as a candidate RUN-9.

This workflow tests the other half of what the design describes: **instruction overrides**. The orchestrator overrides `input_artifacts` and `hitl_override` on successive dispatches of the same row, proving those fields propagate through the Runner to the dispatched agent.

### Three dispatches, escalating overrides

1. **No overrides.** The table's own `Input` column supplies `MosaicTestScript/backjump-echo.md`. The stub echoes task_description_1.
2. **`input_artifacts` override.** The orchestrator supplies `[MosaicTestScript/backjump-echo.md, MosaicTestExtraInput.md]`. The `Inputs` column in the Execution Log shows both paths. The stub still finds its one `MosaicTestScript/` path and echoes task_description_2.
3. **`hitl_override: true`.** The stub has no user-interaction tool, so it returns `BLOCKED`/`E503` without reading the script. This proves `hitl_override` reached the dispatch and was applied as `human_in_the_loop`.

The BLOCKED triggers a fourth consultation, where the orchestrator stops.

### Why the HITL override proves propagation

When `mosaictest-scripted` is dispatched with `human_in_the_loop: true`, it returns `BLOCKED`/`E503` by design — it declares no user-interaction tool and cannot discharge the review gate. This is its only path to BLOCKED in normal operation, so observing BLOCKED in the log is unambiguous proof that the override reached the agent. No extension (E1, E3) is needed.

</Workflow>

---

## Expected Run

Three Orchestration.md log rows. Every consultation — including the one before each dispatch and the final stop — dispatches to the orchestrator but allocates no `Seq` and leaves no row in `Orchestration.md`; each surfaces only as a dispatch-log entry, and the stop additionally as a `session.consult.stop` event in RunnerLogs and as the TUI/CLI exit message.

| Log `Seq` | `Agent` | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|
| 1 | `mosaictest-scripted#1` | RESEARCH | SUCCESS | echo of task_description_1 |
| 2 | `mosaictest-scripted#2` | RESEARCH | SUCCESS | echo of task_description_2; Inputs shows both paths |
| 3 | `mosaictest-scripted#3` | RESEARCH | BLOCKED | E503 message (hitl=true, no user-interaction tool) |

**Run outcome:** stopped by the orchestrator (`RunStoppedByConsultant`). The fixture's stop reason names the BLOCKED as the expected trigger.

**Inputs column check:** Row 1 should show only `MosaicTestScript/backjump-echo.md`. Row 2 should show `MosaicTestScript/backjump-echo.md, MosaicTestExtraInput.md` (or the orchestration-prefixed paths).

**RunnerLogs verification:** The debug log must contain a `session.consult.stop` entry with the fixture's stop reason.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Row 2's Inputs column shows only the table default (one path) | The Runner is ignoring the orchestrator's `input_artifacts` override |
| Row 3 shows SUCCESS instead of BLOCKED | The `hitl_override` did not propagate — the agent was dispatched without `human_in_the_loop: true` |
| Row 3 shows BLOCKED but the stop consultation does not appear | The Runner did not consult the orchestrator after a BLOCKED in Orchestrated mode |
| The run completes after row 1 | Mode confusion — the engine is routing via `On Success = COMPLETE` instead of consulting |
| Summaries are identical across rows 1 and 2 | Task descriptions not carried through — same issue as orchestrated-linear |
| An `orchestrator-script` row appears in `Orchestration.md`, or `Seq` skips a number | Consultations must leave no row and allocate no `Seq`; a consultation wrongly called `Store.Apply` |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-17 | MOSAIC | Initial version. Override mechanism test; true multi-row backjump deferred pending row addressing. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- `output_artifacts` override, once the Execution Log gains an Outputs column or another observation channel exists for it.
- `constraints` override, once an observation channel exists (the Execution Log does not surface constraints).
- True multi-row backjump (row 1 → row 2 → back to row 1) after row addressing for repeated agents is resolved.

**Dead ends (tried and rejected):**
- Two rows with different scripts for the backjump. Same first-match resolution problem as orchestrated-linear: both rows are `mosaictest-scripted`, so the orchestrator cannot address row 2.
