---
version: "3.7"
name: "Brownfield TDD Workflow"
description: "New features or significant changes to an existing codebase requiring test-first development with full research and design."
hint: "The main workhorse — best on shell-buildable codebases. Works best at a certain feature size, discovered through use rather than designed in. The two size-mismatch directions fail differently: too small wastes cost on overspecified Requirements/Plan and triggers unnecessary correction round-trips; too big genuinely starves execution agents of context, making them more error-prone and pushing you toward needing stronger models."
author: MOSAIC
id: brownfield-tdd
referenced_agents:
  - codebase-research
  - requirements-refinement
  - requirements-review
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
  - Research.md
  - requirements-review.md
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

<Workflow type="core" name="brownfield-tdd" version="3.7">
## Brownfield TDD Workflow

**Use when:** New features or significant changes to an **existing codebase** requiring test-first development with full research and design.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ❌ | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | ❌ | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | planner-tdd-soft  | - | TestResults.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). Subagent sequence per stage determined by the `Approach` column in the stage table.

**Notes:**
- **Brownfield** = existing codebase with patterns to discover and follow
- contracts-designer + contracts-review are optional - skip both if no new contracts are needed
- implementation-review may identify other issues than code itself → callback to codebase-research, planner-tdd-soft, contracts-designer

</Workflow>

---

## Design Rationale

The king of workflows — the main workhorse. Works very well for software development where the environment is open to shell commands (`dotnet build`, `dotnet test`, etc. — build/test feedback loops the review and test-runner steps can act on). All of MOSAIC's own tooling was built using this workflow.

Quality tracks feature sizing more than it tracks raw context budget. This was never designed in — it's a pattern noticed through use: the workflow simply works well at a certain feature granularity, and the two directions of mismatch fail for different underlying reasons, not just "quality suffers" symmetrically.

**Too small:** Requirements and Plan tend to over-engineer — producing more specificity than the change actually needs. The concrete cost is not a quality defect in the output so much as waste and friction: unnecessary correction round-trips through requirements-review/plan-review, spent on a feature that didn't need that level of scrutiny in the first place.

**Too big:** Plan and Design tend to stay too vague to pin down the real scope, because the non-execution phases genuinely cannot hold enough context to spec a large feature precisely. This one is a real quality risk, not just wasted cost — the execution agents inherit under-specified direction, downstream errors become more likely, and the practical mitigation is reaching for stronger models to compensate, not just accepting slower convergence. Same underlying sensitivity as `greenfield-tdd`'s context-overload behavior, showing up here as an oversized-feature failure mode specifically rather than a general one.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 3.7 | 2026-08-06 | MOSAIC | Make test-runner findings route back to planner |
| 3.6 | 2026-08-05 | MOSAIC | Initial version |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- **Move HITL off the creator rows onto a convergence-gated `approval-presenter`.** `requirements-refinement`, `planner-tdd-soft`, and `contracts-designer` are all gated `✅` directly, so a human reviews every draft they produce — including rounds their paired reviewer (`requirements-review`, `plan-review`, `contracts-review`) would flag anyway — instead of only the version that already converged. `requirements-to-test-cases` proved the fix (a dedicated presenter row reachable only via the reviewer's `On Success`, so it fires once per convergence, not once per round). Revisit this workflow once that pattern has enough real use to trust.

**Dead ends (tried and rejected):**
- (none yet)
