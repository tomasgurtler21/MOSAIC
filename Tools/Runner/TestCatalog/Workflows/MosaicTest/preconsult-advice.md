---
version: "1.0"
name: "MosaicTest Pre-Consultation Advice Workflow"
description: "Runner mode fixture — Auto mode pre-consultation advice. Proves that pre-consultation strings reach auto-routed dispatches and do NOT reach orchestrator-written dispatches. Uses {task_description} echo to make the presence or absence observable."
hint: "Mode 2 test — pre-consultation advice reaches auto-routed dispatches only"
author: MOSAIC
id: preconsult-advice
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/preconsult-blocked.md
  - MosaicTestScript/preconsult-echo.md
  - MosaicTestMarker.md
modes:
  - auto
---

<Workflow type="core" name="preconsult-advice" version="1.0">
## MosaicTest Pre-Consultation Advice Workflow

**Use when:** Verifying that pre-consultation advice strings are appended to auto-routed dispatches and are NOT appended to orchestrator-written dispatches.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | FALSE | next | - | MosaicTestScript/preconsult-blocked.md | MosaicTestMarker.md |
| PLANNING | mosaictest-scripted | FALSE | COMPLETE | - | MosaicTestScript/preconsult-echo.md | - |

**Notes:**
- **Run this workflow in Auto mode.**
- The routing fixture's `## Pre-Consultation` section carries two advice strings with distinctive markers: `PRECONSULT-ADVICE-MARKER` in `task_description` and `PRECONSULT-CONSTRAINT-MARKER` in `constraints`. The orchestrator-script returns these during the pre-consultation invocation, and the Runner stores them in session state.
- **Row 1** is marker-gated. First invocation (marker absent): returns `BLOCKED`/`E401` and writes the marker. The `BLOCKED` triggers a deviation; the orchestrator re-dispatches the same agent with its own `task_description` (which does NOT contain the preconsult advice). Second invocation (marker present): returns `SUCCESS` with `{task_description}` echo.
- **Row 2** is auto-routed by the engine (SUCCESS on row 1 routes via `next`). The Runner builds this dispatch itself and appends the pre-consultation strings. The stub echoes `{task_description}`, which should contain the `PRECONSULT-ADVICE-MARKER`.
- The proof is in comparing the `{task_description}` echoes: row 1's second invocation (orchestrator-dispatched) shows the orchestrator's own text, and row 2 (auto-routed) shows the marker.
- Seed `Fixtures/preconsult-advice` — the whole directory, not anything inside it.

</Workflow>

---

## Design Rationale

### Why both auto-routed and orchestrator-dispatched steps are needed

Pre-consultation advice is defined by ScriptOrchestratorContract.md §5 as appended to auto-routed dispatches only. To prove the "only" part, we need a dispatch the orchestrator wrote — and its `task_description` must NOT contain the marker. A workflow with only auto-routed steps would prove presence but not absence.

### Why BLOCKED triggers the deviation

`BLOCKED` is the simplest deviation trigger: one error code, one clear signal. The orchestrator re-dispatches the same agent, which is the pattern `deviation-blocked` already uses. The re-dispatch is the orchestrator-written dispatch we need for the "no advice" half of the proof.

### Why two rows, not one

A single-row workflow can only produce auto-routed OR orchestrator-routed dispatches in a single invocation context. Two rows let the run naturally produce both: the deviation on row 1 gives an orchestrator dispatch, and the engine's own routing to row 2 gives an auto dispatch. The `{task_description}` echo on each makes the difference observable.

### Why this workflow does not set `pre_consult: false`

The default is `true`. This workflow NEEDS pre-consultation to run — it is testing that the mechanism works. Setting it to `false` would defeat the purpose.

---

## Expected Run

Five Orchestration.md log rows.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 0 | `orchestrator-script#pre_consultation#1` | consultation | — | "" | pre-consultation advice strings (PRECONSULT-ADVICE-MARKER, PRECONSULT-CONSTRAINT-MARKER) |
| 1 | `mosaictest-scripted#1` | workflow step | RESEARCH | BLOCKED | marker absent, returning BLOCKED E401, wrote marker |
| 2 | `orchestrator-script#2` | consultation | — | "" | dispatch instruction for re-dispatch (orchestrator's own task_description) |
| 3 | `mosaictest-scripted#3` | workflow step | RESEARCH | SUCCESS | marker present, echoes task_description — should NOT contain PRECONSULT-ADVICE-MARKER |
| 4 | `mosaictest-scripted#4` | workflow step | PLANNING | SUCCESS | echoes task_description — SHOULD contain PRECONSULT-ADVICE-MARKER |

**Run outcome:** COMPLETE. Row 1 SUCCESS routes via `next`; row 2 SUCCESS routes to COMPLETE.

**The proof (two observations):**
1. Row 3's `status_message` echoes the orchestrator's task_description. It should contain `MOSAICTEST-PRECONSULT-REDISPATCH` (the orchestrator's own text) and should NOT contain `PRECONSULT-ADVICE-MARKER`.
2. Row 4's `status_message` echoes the auto-routed task_description. It SHOULD contain `PRECONSULT-ADVICE-MARKER` because the Runner appended the pre-consultation advice.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Row 4's echo does NOT contain PRECONSULT-ADVICE-MARKER | The Runner is not appending pre-consultation strings to auto-routed dispatches — the session state is empty or the append path is broken |
| Row 3's echo DOES contain PRECONSULT-ADVICE-MARKER | The Runner is appending pre-consultation strings to orchestrator-written dispatches — the guard that skips orchestrator dispatches is missing |
| Pre-consultation row (Seq 0) is absent | Pre-consultation is disabled or the harness adapter's pre-consultation path is broken |
| Pre-consultation row returns empty `{}` | The routing fixture's Pre-Consultation section is not being parsed — check MosaicTestRouting.md |
| Run stops after Seq 1 with deviation unresolved | The orchestrator consultation path is not wired for this harness |
| Row 3 returns BLOCKED again | The marker was not written on the first pass — check the script's Write section |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-09-20 | MOSAIC | Initial version. Pre-consultation advice presence and absence via {task_description} echo. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A variant that also checks the `constraints` field echo, if the stub gains the ability to echo constraints in addition to task_description.
- Running with `--pre-consult=false` and confirming the marker is absent from BOTH dispatches, to test the disable path.

**Dead ends (tried and rejected):**
- (none yet)
