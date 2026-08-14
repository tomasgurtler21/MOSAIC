---
version: "3.1"
name: "Implementation Only Workflow"
description: "Research, planning, and design already complete. Direct implementation from existing artifacts."
hint: "Direct implementation from existing plan and contracts"
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

Explain why this workflow is structured the way it is. What trade-offs were made? Why are stages ordered as they are? What alternatives were considered and rejected? This section helps future maintainers understand the thinking behind the workflow rather than just reading what it does.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | YYYY-MM-DD | | Initial version |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- (none yet)

**Dead ends (tried and rejected):**
- (none yet)
