---
version: "1.0"
name: "MosaicTest Findings Loop Workflow"
description: "Runner mode fixture — the one difference between Auto and Auto-review. A reviewer returns COMPLETED_NEEDS_ACTION; in Auto-review the engine routes it back automatically, in Auto it deviates to the orchestrator. Same workflow and fixtures, run twice, two different logs."
hint: "Mode 2 vs Mode 3 test — COMPLETED_NEEDS_ACTION routing, review artifact injection"
author: MOSAIC
id: findings-loop
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/findings-loop.md
  - MosaicTestMarker.md
---

<Workflow type="core" name="findings-loop" version="1.0">
## MosaicTest Findings Loop Workflow

**Use when:** Proving that Auto and Auto-review differ in exactly one way: how `COMPLETED_NEEDS_ACTION` is routed when the row has an unambiguous `On Findings` target.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | FALSE | COMPLETE | mosaictest-scripted | MosaicTestScript/findings-loop.md | MosaicTestMarker.md |

**Notes:**
- **Run this workflow twice: once in Auto mode, once in Auto-review mode.** The two runs use identical fixtures and produce different logs. That difference is the proof.
- The script is marker-gated. First invocation (marker absent): writes the marker and returns `COMPLETED_NEEDS_ACTION`. Second invocation (marker present): returns `SUCCESS`.
- `On Findings` points back to the same row (`mosaictest-scripted`). In Auto-review this triggers the engine's findings auto-route. In Auto this deviates to the orchestrator.
- `On Success = COMPLETE` terminates the run after the second invocation succeeds.
- Seed `Fixtures/findings-loop` — the whole directory, not anything inside it.

</Workflow>

---

## Design Rationale

### Why one workflow, two runs

Auto and Auto-review differ in exactly one place in the engine: when the last status is `COMPLETED_NEEDS_ACTION` and the row's `On Findings` is unambiguous, Auto-review dispatches the findings target automatically; Auto deviates to the orchestrator.

Using **the same workflow and fixtures for both runs** makes the mode the only variable. Two separate workflows would prove only that two different definitions behave differently — not that the mode changed the routing.

### Why the marker-gated loop works

The script returns `COMPLETED_NEEDS_ACTION` on the first pass (marker absent, writes marker) and `SUCCESS` on the second pass (marker present). This gives exactly two invocations:

1. First dispatch -> CNA
2. Re-dispatch (by engine or by orchestrator) -> SUCCESS -> COMPLETE

The marker is the output artifact `MosaicTestMarker.md`. When Auto-review auto-routes the CNA back, it injects the reviewing agent's output artifacts into the re-dispatch's `input_artifacts`. Since the output artifact IS the marker artifact, the stub can still find it on the second pass.

### Why On Findings = mosaictest-scripted works here

`On Findings` resolves to the first row matching the agent name. There is only one row, so it resolves to itself — which is the intended behaviour. The loop-back IS the test.

### Review artifact injection (Auto-review only)

In Auto-review mode, the engine's findings auto-route adds the CNA row's output artifacts to the re-dispatched step's `input_artifacts`. The Inputs column of the second invocation should show both the script fixture AND the marker file. This is observable and is an additional check specific to the Auto-review run.

---

## Expected Run: Auto Mode

Three Orchestration.md log rows. The engine cannot route CNA, so it deviates to the orchestrator.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 1 | `mosaictest-scripted#1` | workflow step | RESEARCH | COMPLETED_NEEDS_ACTION | marker absent, writing marker, returning CNA |
| 2 | `orchestrator-script#2` | consultation | — | SUCCESS | dispatch instruction for the re-dispatch |
| 3 | `mosaictest-scripted#3` | workflow step | RESEARCH | SUCCESS | marker present, returning SUCCESS |

**Run outcome:** COMPLETE. The engine routes `SUCCESS` via `On Success = COMPLETE`.

**Key observation:** A consultation row (Seq 2) appears between the two workflow steps. This is what proves Auto mode consulted the orchestrator on CNA.

---

## Expected Run: Auto-review Mode

Two Orchestration.md log rows. The engine routes CNA automatically via `On Findings`.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 1 | `mosaictest-scripted#1` | workflow step | RESEARCH | COMPLETED_NEEDS_ACTION | marker absent, writing marker, returning CNA |
| 2 | `mosaictest-scripted#2` | workflow step | RESEARCH | SUCCESS | marker present, returning SUCCESS |

**Run outcome:** COMPLETE. Same as Auto.

**Key observation:** No consultation row. The engine routed CNA to the findings target and re-dispatched without asking anyone.

**Inputs column check (Auto-review only):** Row 2 should show both `MosaicTestScript/findings-loop.md` AND `MosaicTestMarker.md`. The second path is the review artifact injection — the engine added the CNA row's output to the re-dispatch's inputs.

**Inputs column check (Auto):** Row 3's inputs depend on whether the orchestrator's dispatch overrides `input_artifacts`. The routing fixture does not override them (`none`), so the table defaults apply — only `MosaicTestScript/findings-loop.md`.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Auto-review run has a consultation row | The engine treated CNA as a deviation instead of auto-routing via On Findings — mode logic broken |
| Auto run has NO consultation row | The engine auto-routed CNA in Auto mode — it should not; only Auto-review does this |
| Second invocation returns CNA again instead of SUCCESS | The marker was not written on the first pass, or the marker check is broken |
| Auto-review row 2 Inputs shows only the script (no marker) | Review artifact injection failed — the engine did not add the CNA output to the re-dispatch |
| The run never completes, loops indefinitely | The marker is being reset between invocations, or On Success = COMPLETE is not being evaluated |
| The stub stops with "no matching rule" (Auto mode) | The routing fixture does not cover the state the orchestrator was consulted in |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-17 | MOSAIC | Initial version. The single workflow exercising Auto vs Auto-review difference. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- An echo variant ({task_description} in the message) to show that Auto mode's orchestrator-written task description differs from Auto-review's generic one.
- A variant where On Findings is absent or ambiguous, to test that both modes deviate in that case.

**Dead ends (tried and rejected):**
- Two separate workflows, one per mode. Defeats the purpose: the same fixtures under different modes is the proof.
