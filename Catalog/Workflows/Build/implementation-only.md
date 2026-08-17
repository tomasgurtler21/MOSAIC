---
version: "3.1"
name: "Implementation Only Workflow"
description: "Research, planning, and design already complete. Direct implementation from existing artifacts."
hint: "Likely obsolete — this is the EXECUTION/REVIEW tail of brownfield-tdd/greenfield-tdd, split out to manually resume a long workflow past RESEARCH/PLANNING/DESIGN. Native mid-execution run persistence and continuation now covers that need directly; prefer continuing the original run over starting this one."
author: MOSAIC
id: implementation-only
referenced_agents:
  - implementation-tdd
  - implementation-review
  - test-runner
artifacts:
  - Stage-{StageNumber}/Plan.md
  - ContractsDesign.md
  - Stage-{StageNumber}/PlanProgress.md
  - Stage-{StageNumber}/implementation-review.md
  - TestResults.md
---

<Workflow type="core" name="implementation-only" version="3.1">
## Implementation Only Workflow

**Use when:** Research, planning, and design already complete. Direct implementation from existing artifacts.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md): implementation-tdd → implementation-review. This workflow has a fixed subagent sequence — the Approach column is not used.

**Prerequisites:** ContractsDesign.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md must exist

</Workflow>

---

## Design Rationale

Created by splitting the tail (EXECUTION + REVIEW) off `brownfield-tdd`/`greenfield-tdd`, so a run with Plan.md and ContractsDesign.md already produced elsewhere could jump straight to implementation without repeating RESEARCH/PLANNING/DESIGN. That was the only way to "continue" a long workflow at the time: hand-carry the prerequisite artifacts into a fresh run of a shorter workflow.

Mid-execution run persistence and continuation now solves the same problem natively — resuming the actual run that produced those artifacts, rather than starting a new, differently-scoped workflow against artifacts it never produced itself. That makes this workflow's original justification largely obsolete; it survives mainly as a fallback for cases where the artifacts genuinely originate outside any MOSAIC run (e.g. hand-written Plan/ContractsDesign) rather than as a continuation mechanism.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 3.1 | 2026-08-17 | MOSAIC | Changelog tracking begins here; earlier revisions predate this record. |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- (none yet)

**Dead ends (tried and rejected):**
- **Splitting workflows to enable manual continuation:** the underlying pattern this workflow represents. Superseded by native mid-execution run persistence/continuation. Worth remembering if the temptation to split another long workflow into a "resume from phase X" variant comes up again — the run-continuation mechanism is very likely the better answer now.
