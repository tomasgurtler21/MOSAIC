---
version: "3.3"
name: "Brownfield Design Workflow"
description: "Architecture review, design proposals, or planning large features for an existing codebase without implementation."
hint: "The RESEARCH/PLANNING/DESIGN head of brownfield-tdd, ending before EXECUTION — the mirror image of implementation-only's EXECUTION/REVIEW tail. Unlike implementation-only, this one has a genuine standalone reason to exist beyond resuming a split run: 'produce a design without implementing it' is a real, distinct request, not just a workaround for something native run-continuation now covers."
author: MOSAIC
id: brownfield-design
referenced_agents:
  - codebase-research
  - requirements-refinement
  - requirements-review
  - planner-tdd-soft
  - plan-review
  - contracts-designer
  - contracts-review
artifacts:
  - Requirements.md
  - Research.md
  - requirements-review.md
  - Plan.md
  - "Stage-*/Plan.md"
  - "Stage-*/PlanProgress.md"
  - plan-review.md
  - ContractsDesign.md
  - contracts-review.md
---

<Workflow type="core" name="brownfield-design" version="3.3">
## Brownfield Design Workflow

> **Version:** 3.2

**Use when:** Architecture review, design proposals, or planning large features for an **existing codebase** without implementation.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | FALSE | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | COMPLETE | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |

**Notes:**
- **Brownfield** = existing codebase with patterns to follow
- contracts-designer + contracts-review are optional - skip both if no new/modified contracts are needed
- Enable HITL on contracts-designer/contracts-review if user review is required

</Workflow>

---

## Design Rationale

Structurally the complement of `implementation-only`: together, RESEARCH → PLANNING → DESIGN (this workflow) plus EXECUTION → REVIEW (`implementation-only`) reconstitute `brownfield-tdd`. Both came from the same split.

The difference is in why each half is still worth keeping. `implementation-only` existed mainly to let a run resume past DESIGN by hand-carrying Plan.md/ContractsDesign.md into a fresh run — a workaround now largely superseded by native mid-execution run persistence and continuation. This half doesn't have that problem: "produce architecture review, a design proposal, or a plan for a large feature, but stop before writing any code" is a legitimate, distinct request in its own right — not a workaround for anything. A user asking for a design review genuinely does not want implementation to happen next, run-continuation or not. That makes this workflow's standalone existence better justified than its sibling's.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 3.2 | 2026-08-17 | MOSAIC | Changelog tracking begins here; earlier revisions predate this record. |
| 3.3 | 2026-08-26 | MOSAIC | Replace Unicode emoji with ASCII tokens in HITL column (TRUE/FALSE). |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- **Move HITL off the creator rows onto a convergence-gated `approval-presenter`.** `requirements-refinement`, `planner-tdd-soft`, and `contracts-designer` are all gated `TRUE` directly, so a human reviews every draft they produce — including rounds their paired reviewer would flag anyway — instead of only the version that already converged. `requirements-to-test-cases` proved the fix (a dedicated presenter row reachable only via the reviewer's `On Success`). Revisit this workflow once that pattern has enough real use to trust.

**Dead ends (tried and rejected):**
- (none yet)
