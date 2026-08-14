---
version: "1.0"
name: "MosaicTest Staged Pre-placed Plan Workflow"
description: "Harness conformance fixture — staged execution driven by a pre-placed Plan.md, with no pre-EXECUTION rows. Isolates stage reading and {StageNumber} substitution from planner behaviour."
hint: "Harness test — stage progression and {StageNumber} substitution from a fixture plan"
author: MOSAIC
id: staged-preplaced-plan
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/stage-write.md
  - MosaicTestScript/stage-echo.md
  - Stage-{StageNumber}/MosaicTestStage.md
---

<Workflow type="core" name="staged-preplaced-plan" version="1.0">
## MosaicTest Staged Pre-placed Plan Workflow

**Use when:** Verifying that the runner reads a stage table, progresses through stages in order, and substitutes `{StageNumber}` into artifact paths — without any planner agent involved.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | mosaictest-scripted | ❌ | - | - | MosaicTestScript/stage-write.md | Stage-{StageNumber}/MosaicTestStage.md |
| EXECUTION.[StageNumber] | mosaictest-scripted | ❌ | - | - | MosaicTestScript/stage-echo.md, Stage-{StageNumber}/MosaicTestStage.md | - |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). Both rows run for every stage, in table order.

**Notes:**
- **`Plan.md` is a pre-placed fixture, not produced by the run.** It and the scripts must be seeded into the run folder at run creation. Seed `Fixtures/staged-preplaced-plan` — the whole directory, not anything inside it — as the single seed path. See `Fixtures/README.md`.
- There are **no pre-EXECUTION rows**, so admission condition 1 (a pre-EXECUTION row must output `Stage-*/Plan.md`) does not apply. This is deliberate: it is what lets the plan be pre-placed.
- Bare rows, no groups, no `**Execution Groups:**` table. The fixture plan carries an `Approach` column anyway, which a bare workflow must ignore entirely — that non-effect is itself an assertion of this fixture.
- `On Success` is `-` because it is ignored inside EXECUTION; row order and stage progression govern routing there.

</Workflow>

---

## Design Rationale

Two rows and two stages give four invocations, which is the smallest run that can distinguish "stages advanced" from "rows advanced." A single row would leave the two indistinguishable in the log; a third row adds nothing new.

The second row takes the first row's output as its input. That chains the stage artifact within a stage and proves `{StageNumber}` resolved to the same value for both rows — a substitution bug that produced `Stage-1/` for one row and `Stage-2/` for the next would otherwise pass unnoticed, because each file would still be written somewhere plausible.

**Why the plan is pre-placed.** `staged-single-group` (not yet authored) covers the other half of this: a stub planner that writes the stage table itself, which additionally exercises the `Stage-*` re-derivation path where the runner re-reads `Plan.md` after any row whose outputs contain a `Stage-*` wildcard. Splitting the two means a stage-reading failure here cannot be blamed on planner behaviour, and vice versa.

**Why the fixture plan carries an `Approach` column.** A bare workflow ignores it. That used to be an accident of classification and is now documented behaviour, so a fixture that supplies a value and observes no effect is worth having — it guards against a regression where bare workflows start honouring approaches.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-03 | MOSAIC | Initial version |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A three-stage variant to check that stage progression is not hardcoded to two.
- Setting stage 2's HITL to ✅ in the fixture plan to assert per-stage HITL resolution (effective HITL is the row flag OR the stage flag).

**Dead ends (tried and rejected):**
- Declaring `Stage-{StageNumber}/Plan.md` as a row input. It would require per-stage plan files in the fixture tree for no added coverage — `{StageNumber}` substitution is already proven by the output path.
