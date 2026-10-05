---
version: "1.0"
name: "MosaicTest Staged Findings Loop Workflow"
description: "Runner mode fixture — an Auto-review findings loop inside a staged EXECUTION group, where the same agent fills several rows. Proves the engine still knows which row it is on after a route-back, instead of working the row out from the invocation count."
hint: "Mode 3 test — row identity after an On Findings route-back inside EXECUTION, with duplicate-agent rows"
author: MOSAIC
id: staged-findings-loop
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/loop-writer.md
  - MosaicTestScript/loop-gate.md
  - MosaicTestScript/loop-review.md
  - MosaicTestScript/loop-impl.md
  - Stage-{StageNumber}/MosaicTestLoopWritten.md
  - Stage-{StageNumber}/MosaicTestLoopGate.md
  - Stage-{StageNumber}/MosaicTestLoopReview.md
  - Stage-{StageNumber}/MosaicTestLoopImpl.md
modes:
  - auto-review
---

<Workflow type="core" name="staged-findings-loop" version="1.0">
## MosaicTest Staged Findings Loop Workflow

**Use when:** Checking that after an Auto-review route-back inside a staged EXECUTION group, the engine continues from the row that actually ran, not from a row derived from the invocation count.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.Test.[StageNumber] | mosaictest-scripted | FALSE | - | - | MosaicTestScript/loop-writer.md | Stage-{StageNumber}/MosaicTestLoopWritten.md |
| EXECUTION.Test.[StageNumber] | mosaictest-scripted | FALSE | - | mosaictest-scripted | MosaicTestScript/loop-gate.md | Stage-{StageNumber}/MosaicTestLoopGate.md |
| EXECUTION.Test.[StageNumber] | mosaictest-scripted | FALSE | - | - | MosaicTestScript/loop-review.md | Stage-{StageNumber}/MosaicTestLoopReview.md |
| EXECUTION.Implementation.[StageNumber] | mosaictest-scripted | FALSE | - | - | MosaicTestScript/loop-impl.md | Stage-{StageNumber}/MosaicTestLoopImpl.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). One stage, TDD approach: the three Test rows, then the Implementation row.

**Notes:**
- **Auto-review only.** The route-back under test is the engine's own On Findings auto-route; Auto mode would hand it to the orchestrator instead.
- Row 1 (writer) and row 3 (review) and row 4 (impl) always return SUCCESS. Row 2 (gate) is marker-gated: the first pass writes its marker and returns `COMPLETED_NEEDS_ACTION`; the second pass finds the marker and returns `SUCCESS`.
- Row 2's `On Findings` is `mosaictest-scripted`, which the engine resolves to the first row with that agent: row 1. That is the intended route-back target (writer), standing in for `build-review → test-writer-tdd` in `brownfield-tdd-build-verified`.
- Every row uses the same agent, so every row is a duplicate-agent row. That is the condition the test needs.
- `Plan.md` is pre-placed. Seed `Fixtures/staged-findings-loop`, the whole directory.

</Workflow>

---

## Design Rationale

### The suspected defect

To work out which EXECUTION row it just ran, the engine matches on agent name. When several EXECUTION rows share that agent, `findCurrentRowIndex` falls back to `findExecutionRowBySeq`, which turns the global invocation number into a position within the stage. That arithmetic assumes **one invocation per row**. A findings route-back breaks that assumption: it runs an earlier row again, so the invocation count moves ahead of the real row position. The engine then picks a later row than the one that actually ran and carries on from there, skipping rows.

The artifact already records the group (`Stage` = `Test.1`), but row lookup does not use it.

### How this workflow exposes it

| Seq | Row actually run | Row the seq arithmetic picks | Correct next | Buggy next |
|:---:|---|---|---|---|
| 1 | 1 writer | 1 | 2 gate | 2 gate |
| 2 | 2 gate → CNA | 2 | route back → 1 | route back → 1 |
| 3 | 1 writer (route-back) | **3 review** | 2 gate | **4 impl** |

On the buggy path, the gate never runs again and the review row never runs. The run still finishes with exit code 0 because every step it did dispatch succeeded. The difference shows up only in the number and order of dispatches, which is what `.expected.json` checks.

### Relation to the production workflow

In `brownfield-tdd-build-verified`, only `build-review` is duplicated (rows 54 and 57), so the drift has to reach the other `build-review` row before it causes a misroute. One `tests-review-tdd` findings loop is enough:

`test-writer(1) → build-review(2) → tests-review(3) CNA → test-writer(4) → build-review(5)`

Position 5 is the Implementation group's `build-review`. The engine would then go to `implementation-review` and skip `tests-review-tdd` and `implementation-tdd`. This workflow triggers the same code path with less setup: every row shares one agent, so a single route-back is enough to cause the drift.

### Why one stage

The defect is about position within a stage. A second stage adds dispatches without adding a new condition.

---

## Expected Run: Auto-review Mode

Six workflow steps plus the pre-consultation. The pre-consultation takes no `Seq` and leaves no `Orchestration.md` row.

| Log `Seq` | `Agent` | `Phase` | `Stage` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 1 | `mosaictest-scripted#1` | EXECUTION | Test.1 | SUCCESS | row 1 / writer |
| 2 | `mosaictest-scripted#2` | EXECUTION | Test.1 | COMPLETED_NEEDS_ACTION | row 2 / gate / marker absent |
| 3 | `mosaictest-scripted#3` | EXECUTION | Test.1 | SUCCESS | row 1 / writer (route-back) |
| 4 | `mosaictest-scripted#4` | EXECUTION | Test.1 | SUCCESS | row 2 / gate / marker present |
| 5 | `mosaictest-scripted#5` | EXECUTION | Test.1 | SUCCESS | row 3 / review |
| 6 | `mosaictest-scripted#6` | EXECUTION | Implementation.1 | SUCCESS | row 4 / impl |

**Run outcome:** COMPLETE. No consultation after the pre-consultation.

**Inputs column check:** Seq 3 (the route-back) should list `Stage-1/MosaicTestLoopGate.md`, the gate's output that the engine injects on the findings route-back, in addition to `MosaicTestScript/loop-writer.md`.

### Buggy run, for recognition

| Log `Seq` | `Stage` | `Status` | `Summary` shows |
|:---:|---|---|---|
| 1 | Test.1 | SUCCESS | row 1 / writer |
| 2 | Test.1 | COMPLETED_NEEDS_ACTION | row 2 / gate / marker absent |
| 3 | Test.1 | SUCCESS | row 1 / writer (route-back) |
| 4 | Implementation.1 | SUCCESS | row 4 / impl |

Four steps instead of six; the second gate pass and the review row are missing.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Seq 4 is row 4 (`Implementation.1`), run ends after 4 steps | The suspected defect: `findCurrentRowIndex` / `findExecutionRowBySeq` identified seq 3 as row 3 instead of row 1. Row lookup for duplicate-agent EXECUTION rows does not account for route-backs. |
| Seq 3 is not row 1 | The On Findings route-back target was resolved wrong (`findFirstRowForAgent`), or the stage string was lost on the route-back dispatch. |
| A consultation appears after seq 2 | The engine treated the CNA as a deviation. Check `isUnambiguousHint` on row 2's On Findings, and that the run is in Auto-review mode. The routing fixture stops the run here on purpose. |
| Seq 4 returns CNA again | The gate's marker was not written on the first pass, or the route-back did not keep `Stage-1` in the marker path. |
| `FIXTURE_ERROR` naming `MosaicTestScript/` | The seed root was wrong; seed `Fixtures/staged-findings-loop` itself. |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-10-03 | MOSAIC | Initial version. Reproduces suspected row drift after a findings route-back among duplicate-agent EXECUTION rows. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A variant whose route-back targets a row in the *Implementation* group while the same agent also has a Test-group row, to cover `findFirstRowForAgent` resolving the route-back target outside the current group.

**Dead ends (tried and rejected):**
- (none yet)
