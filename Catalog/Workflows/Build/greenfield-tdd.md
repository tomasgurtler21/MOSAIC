---
version: "3.4"
name: "Greenfield TDD Workflow"
description: "Building a new project from scratch requiring system architecture, test-first development, and full design."
hint: "Proven and powerful, but early phases can be overwhelmed on large scopes — quality degrades gradually, not a hard failure. Size the build accordingly."
author: MOSAIC
id: greenfield-tdd
referenced_agents:
  - requirements-refinement
  - requirements-review
  - system-designer
  - system-design-review
  - planner-tdd-soft
  - plan-review
  - contracts-designer
  - contracts-review
  - test-writer-tdd
  - tests-review-tdd
  - implementation-tdd
  - implementation-review
  - test-runner
artifacts:
  - Requirements.md
  - requirements-review.md
  - SystemDesign.md
  - system-design-review.md
  - Plan.md
  - Stage-*/Plan.md
  - Stage-*/PlanProgress.md
  - plan-review.md
  - ContractsDesign.md
  - contracts-review.md
  - Stage-{StageNumber}/Plan.md
  - Stage-{StageNumber}/PlanProgress.md
  - Stage-{StageNumber}/tests-review-tdd.md
  - Stage-{StageNumber}/implementation-review.md
  - TestResults.md
---

<Workflow type="core" name="greenfield-tdd" version="3.4">
## Greenfield TDD Workflow

**Use when:** Building a **new project from scratch** requiring system architecture, test-first development, and full design.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | system-designer | requirements-refinement | Requirements.md | requirements-review.md |
| ARCHITECTURE | system-designer | ✅ | system-design-review | - | Requirements.md | SystemDesign.md |
| ARCHITECTURE | system-design-review | ❌ | planner-tdd-soft | system-designer | Requirements.md, SystemDesign.md | system-design-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Requirements.md, SystemDesign.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Requirements.md, Plan.md, Stage-*/Plan.md, SystemDesign.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | ❌ | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). Subagent sequence per stage determined by the `Approach` column in the stage table.

**Notes:**
- **Greenfield** = no existing codebase, architecture created from scratch
- If system-design-review finds requirements issues → system-designer evaluates and may loop to requirements-refinement

</Workflow>

---

## Design Rationale

Close sibling of `brownfield-tdd`, differing mainly in the added ARCHITECTURE phase (system-designer / system-design-review) needed because there is no existing codebase to anchor design decisions in.

Proven and heavily used in practice, with consistently good results. Quality is sensitive to the size of the thing being built: the early phases (requirements, architecture, planning) must hold a large amount of context, and on larger greenfield scopes they can become overwhelmed. This does not cause a hard failure — no collapse — but produces a gradual quality dropoff, the same pattern seen under context overload generally.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 3.4 | 2026-08-17 | MOSAIC | Changelog tracking begins here; earlier revisions predate this record. |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- **Third dimension: `{Domain}` alongside `{Phase}`.** For large greenfield scopes, partition planning/execution artifacts by domain as well as phase — e.g. `Plan.{Domain}.md`, `Stage-{N}/Plan.{Domain}.md` — instead of one flat phase sequence trying to hold the whole system. This workflow is the first candidate to receive it. Not started; gated on achieving solid test coverage of the orchestrator first, since this is a structural change to how artifacts are addressed.
- **Move HITL off the creator rows onto a convergence-gated `approval-presenter`.** `requirements-refinement`, `system-designer`, `planner-tdd-soft`, and `contracts-designer` are all gated `✅` directly, so a human reviews every draft they produce — including rounds their paired reviewer would flag anyway — instead of only the version that already converged. `requirements-to-test-cases` proved the fix (a dedicated presenter row reachable only via the reviewer's `On Success`). Revisit this workflow once that pattern has enough real use to trust.

**Dead ends (tried and rejected):**
- (none yet)
