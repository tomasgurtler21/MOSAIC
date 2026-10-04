---
version: "1.1"
name: "MosaicTest Findings Loop Workflow"
description: "Runner mode fixture — the one difference between Auto and Auto-review. A reviewer returns COMPLETED_NEEDS_ACTION; in Auto-review the engine routes it back automatically, in Auto it deviates to the orchestrator. Same workflow and fixtures, run twice, two different logs."
hint: "Mode 2 vs Mode 3 test — COMPLETED_NEEDS_ACTION routing, review artifact injection"
author: MOSAIC
id: findings-loop
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/findings-create.md
  - MosaicTestScript/findings-loop.md
  - MosaicTestCreated.md
  - MosaicTestMarker.md
modes:
  - auto
  - auto-review
smoke_set:
  - auto
  - auto-review
---

<Workflow type="core" name="findings-loop" version="1.1">
## MosaicTest Findings Loop Workflow

**Use when:** Proving that Auto and Auto-review differ in exactly one way: how `COMPLETED_NEEDS_ACTION` is routed when the row has an unambiguous `On Findings` target.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | FALSE | next | - | MosaicTestScript/findings-create.md | MosaicTestCreated.md |
| REVIEW | mosaictest-scripted | FALSE | COMPLETE | mosaictest-scripted | MosaicTestScript/findings-loop.md | MosaicTestMarker.md |

**Notes:**
- **Run this workflow twice: once in Auto mode, once in Auto-review mode.** The two runs use identical fixtures and produce different logs. That difference is the proof.
- Row 1 is the creator and always returns `SUCCESS`. Row 2 is the reviewer and is marker-gated. First invocation (marker absent): writes the marker and returns `COMPLETED_NEEDS_ACTION`. Second invocation (marker present): returns `SUCCESS`.
- Row 2's `On Findings` names `mosaictest-scripted`, which resolves to the nearest row above it: row 1, the creator. In Auto-review this triggers the engine's findings auto-route. In Auto it deviates to the orchestrator.
- The two rows use different phases (RESEARCH, REVIEW) because the engine identifies a non-EXECUTION row by agent and phase.
- `On Success = COMPLETE` on row 2 ends the run after the reviewer's second invocation succeeds.
- Seed `Fixtures/findings-loop` — the whole directory, not anything inside it.

</Workflow>

---

## Design Rationale

### Why one workflow, two runs

Auto and Auto-review differ in exactly one place in the engine: when the last status is `COMPLETED_NEEDS_ACTION` and the row's `On Findings` is unambiguous, Auto-review dispatches the findings target automatically; Auto deviates to the orchestrator.

Using **the same workflow and fixtures for both runs** makes the mode the only variable. Two separate workflows would prove only that two different definitions behave differently — not that the mode changed the routing.

### Why the marker-gated loop works

The reviewer script returns `COMPLETED_NEEDS_ACTION` on the first pass (marker absent, writes the marker) and `SUCCESS` on the second pass (marker present). The creator always returns `SUCCESS`. This gives exactly four workflow steps:

1. Creator -> SUCCESS -> `next`
2. Reviewer -> CNA (writes the marker)
3. Route-back to the creator (by the engine or by the orchestrator) -> SUCCESS -> `next`
4. Reviewer -> SUCCESS (marker present) -> COMPLETE

The marker is the reviewer's output artifact `MosaicTestMarker.md`. It sits at the run folder root, so the reviewer finds it on its second pass however the route-back was made.

### Why a creator row and a reviewer row

`On Findings` resolves to the nearest row **above** the row that ran whose agent is the target; the row that ran is never a candidate (Runner `Design.md` §2.4, nearest-preceding rule). A one-row workflow whose reviewer names itself therefore has no target, and Auto-review deviates exactly like Auto, which would erase the difference under test. Version 1.0 was built that way and broke when the rule was introduced.

Two rows also match the real pattern this stands in for: a creator followed by its reviewer, with the reviewer's findings going back to the creator (e.g. `planner-tdd-soft` -> `plan-review`).

In Auto mode the orchestrator's re-dispatch names `mosaictest-scripted`, and the Runner binds a consultant dispatch to the **first** row naming that agent: row 1, the creator. Both modes therefore re-run the same row, and their Execution Logs have the same shape.

### Review artifact injection (Auto-review only)

In Auto-review, the engine's findings auto-route adds the reviewer's output artifacts to the creator's re-dispatch `input_artifacts`. It also adds the creator's own earlier outputs, taken from the artifact registry. Both show in the Inputs column of the route-back step and are an extra check that applies only to the Auto-review run.

---

## Expected Run: Auto Mode

Four Orchestration.md log rows. The engine cannot route CNA, so it deviates to the orchestrator. Neither the pre-consultation nor the deviation-routing consultation allocates a `Seq` or leaves a row in `Orchestration.md`.

| Log `Seq` | `Agent` | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|
| 1 | `mosaictest-scripted#1` | RESEARCH | SUCCESS | row 1 / creator |
| 2 | `mosaictest-scripted#2` | REVIEW | COMPLETED_NEEDS_ACTION | row 2 / reviewer / marker absent |
| 3 | `mosaictest-scripted#3` | RESEARCH | SUCCESS | row 1 / creator (orchestrator re-dispatch) |
| 4 | `mosaictest-scripted#4` | REVIEW | SUCCESS | row 2 / reviewer / marker present |

**Run outcome:** COMPLETE. The engine routes the reviewer's `SUCCESS` via `On Success = COMPLETE`.

**Key observation:** The dispatch log shows an `orchestrator-script` routing consultation between Seq 2 and Seq 3. That is the proof that Auto mode consulted the orchestrator on CNA.

**Inputs column check (Auto):** The routing fixture does not override `input_artifacts` (`none`), so Seq 3 uses the table default only: `MosaicTestScript/findings-create.md`.

---

## Expected Run: Auto-review Mode

Four Orchestration.md log rows. The engine routes CNA itself via `On Findings`.

| Log `Seq` | `Agent` | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|
| 1 | `mosaictest-scripted#1` | RESEARCH | SUCCESS | row 1 / creator |
| 2 | `mosaictest-scripted#2` | REVIEW | COMPLETED_NEEDS_ACTION | row 2 / reviewer / marker absent |
| 3 | `mosaictest-scripted#3` | RESEARCH | SUCCESS | row 1 / creator (engine route-back) |
| 4 | `mosaictest-scripted#4` | REVIEW | SUCCESS | row 2 / reviewer / marker present |

**Run outcome:** COMPLETE. Same as Auto.

**Key observation:** No consultation appears in the dispatch log apart from the pre-consultation. The engine routed CNA to the findings target without asking anyone. The Auto and Auto-review Execution Logs are identical in shape; the only difference is whether the dispatch log shows a routing consultation between Seq 2 and Seq 3.

**Inputs column check (Auto-review only):** Seq 3 should show `MosaicTestScript/findings-create.md`, plus `MosaicTestMarker.md` (the reviewer's output, injected on the route-back) and `MosaicTestCreated.md` (the creator's own earlier output, from the registry).

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Auto-review run shows a routing consultation after Seq 2 | The engine treated CNA as a deviation instead of auto-routing via On Findings. Check `isUnambiguousHint` and `findNearestPrecedingRowForAgent` (the target must resolve to row 1), and that the run is really in Auto-review mode |
| Auto run shows NO consultation in the dispatch log | The engine auto-routed CNA in Auto mode; only Auto-review should do this |
| Seq 3 is the reviewer (REVIEW) instead of the creator | The findings target or the consultant-dispatch row binding resolved to the wrong row |
| Seq 4 returns CNA again instead of SUCCESS | The marker was not written on the reviewer's first pass, or the marker check is broken |
| Auto-review Seq 3 Inputs shows only the script | Review artifact injection failed: the engine did not add the reviewer's output to the route-back |
| The run loops, or ends after Seq 3 | `next` on row 1 or `On Success = COMPLETE` on row 2 is not being evaluated, or the engine misidentified which row ran |
| The stub stops with "no matching rule" (Auto mode) | The routing fixture does not cover the state the orchestrator was consulted in |
| An `orchestrator-script` row appears in `Orchestration.md` (either mode) | Consultations must leave no row in the artifact; a routing consultation wrongly called `Store.Apply` or allocated a `Seq` |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-17 | MOSAIC | Initial version. The single workflow exercising Auto vs Auto-review difference. |
| 1.1 | 2026-10-04 | MOSAIC | Split into a creator row and a reviewer row. The one-row self-targeting `On Findings` stopped resolving under the nearest-preceding rule (Runner `Design.md` §2.4, introduced 2026-10-03), so Auto-review deviated like Auto. Four workflow steps per run instead of two. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- An echo variant ({task_description} in the message) to show that Auto mode's orchestrator-written task description differs from Auto-review's generic one.
- A variant where On Findings is absent or ambiguous, to test that both modes deviate in that case.

**Dead ends (tried and rejected):**
- Two separate workflows, one per mode. Defeats the purpose: the same fixtures under different modes is the proof.
- A single row whose `On Findings` names its own agent (v1.0). The row that ran is never a findings-target candidate, so the self-loop has no target and Auto-review deviates.
