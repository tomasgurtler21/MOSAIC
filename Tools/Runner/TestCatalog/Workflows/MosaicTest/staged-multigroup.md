---
version: "1.0"
name: "MosaicTest Staged Multi-Group Workflow"
description: "Runner mode fixture — multi-group staged execution with TDD approach. Test group dispatches first per stage, then Implementation group. Proves group-ordering, phase-column notation, and {StageNumber} substitution survive a real harness round trip."
hint: "Harness test — EXECUTION.Test and EXECUTION.Implementation group ordering across two stages"
author: MOSAIC
id: staged-multigroup
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/multigroup-test.md
  - MosaicTestScript/multigroup-impl.md
  - Stage-{StageNumber}/MosaicTestStageTest.md
  - Stage-{StageNumber}/MosaicTestStageImpl.md
modes:
  - auto
---

<Workflow type="core" name="staged-multigroup" version="1.0">
## MosaicTest Staged Multi-Group Workflow

**Use when:** Verifying that the runner dispatches execution groups in the correct order (Test before Implementation) across stages, and that the `EXECUTION.{Group}.[StageNumber]` phase-column notation parses correctly through a real harness CLI.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.Test.[StageNumber] | mosaictest-scripted | FALSE | - | - | MosaicTestScript/multigroup-test.md | Stage-{StageNumber}/MosaicTestStageTest.md |
| EXECUTION.Implementation.[StageNumber] | mosaictest-scripted | FALSE | - | - | MosaicTestScript/multigroup-impl.md, Stage-{StageNumber}/MosaicTestStageTest.md | Stage-{StageNumber}/MosaicTestStageImpl.md |

**Execution Groups:**

| Group | Approach |
|-------|----------|
| Test | TDD |
| Implementation | TDD |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). For each stage, the Test group's rows run first, then the Implementation group's rows, per the TDD approach ordering.

**Notes:**
- **`Plan.md` is a pre-placed fixture, not produced by the run.** It and the scripts must be seeded into the run folder at run creation. Seed `Fixtures/staged-multigroup` — the whole directory, not anything inside it — as the single seed path. See `Fixtures/README.md`.
- There are **no pre-EXECUTION rows**, so admission condition 1 (a pre-EXECUTION row must output `Stage-*/Plan.md`) does not apply. This is deliberate: it is what lets the plan be pre-placed.
- The Implementation row takes the Test row's output (`Stage-{StageNumber}/MosaicTestStageTest.md`) as an input. This chains the two groups within a stage and proves `{StageNumber}` resolved to the same value for both rows.
- `On Success` is `-` because it is ignored inside EXECUTION; row order, group order, and stage progression govern routing there.

</Workflow>

---

## Design Rationale

### Why two groups, not one

`staged-preplaced-plan` proves stage progression and `{StageNumber}` substitution with bare rows (no groups). This workflow adds groups: the engine must parse `EXECUTION.Test.[StageNumber]` and `EXECUTION.Implementation.[StageNumber]`, sort them by group order, and dispatch Test before Implementation within each stage. A bare workflow exercises none of that parsing.

### Why two stages

One stage would prove group ordering within a stage but not that the ordering resets across stages. Two stages prove that the engine runs Test-then-Implementation for stage 1 AND then Test-then-Implementation for stage 2, rather than running all Test rows across all stages followed by all Implementation rows.

### Why TDD approach

The TDD approach orders Test before Implementation. This is the most interesting ordering because it is non-alphabetical — it would not be correct by accident. If the engine dispatched groups alphabetically, Implementation would run before Test, and the run log would show the wrong order.

### Why the Implementation row chains from Test

The Implementation row takes the Test row's output as its input. This guarantees that if the engine dispatched Implementation before Test within a stage, the run would fail visibly — the input artifact would not exist yet. The chaining turns a group-ordering bug into a loud failure rather than a subtle log-order discrepancy.

### Historical context

A past engine crash occurred at glob/wildcard dispatch after the design phase finished (§7.4 in TestCatalogDesign.md). Multi-group staged workflows are the most complex routing path the engine handles, and proving they work end-to-end through every harness is essential for a confident compatibility declaration.

---

## Expected Run: Auto Mode

Five dispatches total: one pre-consultation, then four workflow steps (2 stages × 2 groups).

| Log `Seq` | `Agent` | Kind | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|---|
| 0 | `orchestrator-script#pre_consultation#1` | consultation | — | "" | pre-run consultation response |
| 1 | `mosaictest-scripted#1` | workflow step | EXECUTION.Test.1 | SUCCESS | Test group / stage 1 / wrote test artifact |
| 2 | `mosaictest-scripted#2` | workflow step | EXECUTION.Implementation.1 | SUCCESS | Implementation group / stage 1 / wrote impl artifact |
| 3 | `mosaictest-scripted#3` | workflow step | EXECUTION.Test.2 | SUCCESS | Test group / stage 2 / wrote test artifact |
| 4 | `mosaictest-scripted#4` | workflow step | EXECUTION.Implementation.2 | SUCCESS | Implementation group / stage 2 / wrote impl artifact |

**Run outcome:** COMPLETE. All stages exhausted, no deviations.

**Key observations:**
- Rows 1 and 2 are stage 1: Test before Implementation.
- Rows 3 and 4 are stage 2: Test before Implementation again.
- If group ordering were wrong (Implementation before Test), the chained input artifact would be missing and the run would fail with BLOCKED.
- The `Phase` column in the execution log carries the fully resolved notation (`EXECUTION.Test.1`, not `EXECUTION.Test.[StageNumber]`).

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| Run refuses to start, naming `Plan.md` | Plan fixture was not seeded, or the seed root was wrong. Check the seed path. |
| BLOCKED on the Implementation row, naming the Test artifact | Group ordering is wrong: Implementation dispatched before Test within a stage. The chained input did not exist yet. |
| All Test rows run before any Implementation row | The engine is not resetting group ordering per stage — it is running all of one group across all stages, then the other. |
| Phase column shows `EXECUTION.[StageNumber]` without a group name | The `EXECUTION.{Group}.[StageNumber]` notation was not parsed. The engine may be treating rows as bare. |
| Phase column shows `EXECUTION.Test.[StageNumber]` (literal brackets) | `{StageNumber}` substitution failed in the phase column. |
| Only 2 workflow steps instead of 4 | Stage progression is broken — only one stage ran. Check Plan.md for two stages. |
| Orchestrator consulted mid-run (extra consultation rows) | A deviation occurred. All rows should return SUCCESS in this workflow; a deviation means the stub script was not found or returned an unexpected status. |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-09-20 | MOSAIC | Initial version. Multi-group staged execution with Test/Implementation ordering. |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A three-group variant (Test, Implementation, Review) to verify ordering with more than two groups.
- Adding HITL=TRUE on a specific stage in the plan to test per-stage HITL resolution combined with group ordering.
- A variant where one group has two rows per stage, to test intra-group row ordering alongside inter-group ordering.

**Dead ends (tried and rejected):**
- Using alphabetical group names (Alpha, Beta) to test ordering. The TDD approach defines a specific Test-before-Implementation order; alphabetical names would be ambiguous about what "correct" means.
