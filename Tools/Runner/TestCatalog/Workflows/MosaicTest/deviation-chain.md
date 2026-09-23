---
version: "1.0"
name: "MosaicTest Deviation Chain Workflow"
description: "Runner mode fixture — Auto mode consecutive deviation handling. Two agents return BLOCKED in succession; the orchestrator is consulted twice and re-dispatches each time. Proves the single-decision principle works end to end through back-to-back orchestrator consultations."
hint: "Mode 2 test — two consecutive BLOCKED deviations, two orchestrator consultations, run completion"
author: MOSAIC
id: deviation-chain
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/chain-first.md
  - MosaicTestScript/chain-second.md
  - MosaicTestMarker.md
modes:
  - auto
---

<Workflow type="core" name="deviation-chain" version="1.0">
## MosaicTest Deviation Chain Workflow

**Use when:** Verifying that two consecutive `BLOCKED` results in Auto mode each become a separate deviation, the orchestrator is consulted twice in succession, and the run completes when the second re-dispatch succeeds.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | FALSE | COMPLETE | - | MosaicTestScript/chain-first.md | MosaicTestMarker.md |

**Notes:**
- **Run this workflow in Auto mode.**
- The table has one row. The orchestrator's input_artifacts overrides are what steer each dispatch to a different script fixture — the workflow table is not the discriminator here, the routing fixture is.
- **Pass 1** (chain-first.md, marker absent): returns `BLOCKED`/`E401` and writes the marker. Deviation fires, orchestrator consulted.
- **Pass 2** (chain-second.md via override): returns `BLOCKED`/`E401` unconditionally. Second deviation fires, orchestrator consulted again.
- **Pass 3** (chain-first.md via override, marker now present): returns `SUCCESS`. Engine routes via `On Success = COMPLETE`.
- Seed `Fixtures/deviation-chain` — the whole directory, not anything inside it.

</Workflow>

---

## Design Rationale

### Why two consecutive BLOCKEDs

`deviation-blocked` proves a single deviation-to-consultation round trip. This workflow extends that to a chain: the first consultation's re-dispatch itself produces another deviation. Each consultation is a fresh session through the harness adapter. If the adapter leaks state, caches a stale response, or misparses on the second call, this run catches it.

### Why the orchestrator switches scripts via input_artifacts override

Both passes use the same agent (`mosaictest-scripted`). The routing fixture overrides `input_artifacts` to steer each dispatch to a different script fixture. This is the mechanism the orchestrator uses to vary behaviour across re-dispatches of the same agent — the same mechanism a real orchestrator would use to send a different task to the same subagent.

### Why the marker gate on chain-first

Pass 1 must return BLOCKED (first deviation trigger). Pass 3 must return SUCCESS (run completion). The marker distinguishes the two: absent on pass 1, present on pass 3 (written during pass 1's BLOCKED response). chain-second has no marker gate because it always returns BLOCKED — it exists only to create the second link in the chain.

### Why occurrence counting matters

The routing fixture uses `after mosaictest-scripted BLOCKED` for the first BLOCKED and `after mosaictest-scripted BLOCKED #2` for the second. The `#2` selector is what makes the two consultations route differently. If the orchestrator miscounts occurrences (e.g. counting infrastructure rows or consultation rows as workflow rows), the wrong rule fires and the run either loops or stops unexpectedly.

---

## Expected Run

Six Orchestration.md log rows.

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 0 | `orchestrator-script#pre_consultation#1` | consultation | -- | "" | pre-run consultation response |
| 1 | `mosaictest-scripted#1` | workflow step | RESEARCH | BLOCKED | chain-first / marker absent / wrote marker / BLOCKED E401 |
| 2 | `orchestrator-script#2` | consultation | -- | "" | dispatch with input_artifacts override to chain-second.md |
| 3 | `mosaictest-scripted#3` | workflow step | RESEARCH | BLOCKED | chain-second / unconditional BLOCKED E401 |
| 4 | `orchestrator-script#4` | consultation | -- | "" | dispatch with input_artifacts override to chain-first.md |
| 5 | `mosaictest-scripted#5` | workflow step | RESEARCH | SUCCESS | chain-first / marker present / SUCCESS |

**Run outcome:** COMPLETE. The engine routes `SUCCESS` via `On Success = COMPLETE`.

**Key observations:**
- Two consultation rows (Seq 2 and Seq 4) prove two separate deviation-to-orchestrator round trips.
- The second consultation dispatches back to chain-first.md, proving the orchestrator can redirect to a previously-failed script.
- SUCCESS on row 5 proves the marker persisted across the intervening dispatches and the marker gate resolved correctly.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Run stops after row 1 with `RunDeviationUnresolved` | No routing consultant configured — the harness adapter's `InvokeRaw` path is missing (RUN-4) |
| First consultation works but second fails to parse | The harness adapter leaks state between consecutive `InvokeRaw` calls — session isolation defect |
| Row 3 shows SUCCESS instead of BLOCKED | The input_artifacts override from the first consultation did not reach the stub, or reached it pointing at chain-first.md instead of chain-second.md |
| Row 5 shows BLOCKED again instead of SUCCESS | The marker was not written on pass 1, or the input_artifacts override from the second consultation pointed at chain-second.md instead of chain-first.md |
| The stub stops with "no matching rule" after the second BLOCKED | The routing fixture's `#2` selector did not match — the orchestrator may be counting non-workflow rows as occurrences |
| The routing fixture's `#2` rule fires on the first consultation | The occurrence count includes something unexpected — infrastructure rows, consultation rows, or a stale count from a previous run |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-09-20 | MOSAIC | Initial version. Two consecutive BLOCKED deviations with orchestrator re-dispatch chain. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A three-link chain to test that occurrence counting works beyond #2.
- A variant where the second deviation's re-dispatch goes to a third agent (not back to the first), testing that the orchestrator can fan out rather than bounce.

**Dead ends (tried and rejected):**
- Using two separate rows in the workflow table (one per agent). Adds complexity without covering anything new — the orchestrator's input_artifacts override is the mechanism that steers dispatch, not the workflow row. A single row keeps the fixture focused on the chain behaviour.
