---
version: "0.1"
name: "Quick Fix Workflow"
description: "Small changes, bug fixes, or well-understood modifications. Skips research and design."
hint: "Reference anti-pattern, not a recommendation — kept to show what 'fast' costs. Skipping test-writer means bug fixes ship with no regression test; skipping requirements means 'well-understood' is never actually verified. Never used in practice. Prefer brownfield-tdd sized down for real small fixes."
author: MOSAIC
id: quick-fix
referenced_agents:
  - planner-tdd-soft
  - plan-review
  - implementation-tdd
  - test-runner
artifacts:
  - Plan.md
  - Stage-*/Plan.md
  - Stage-*/PlanProgress.md
  - plan-review.md
  - Stage-{StageNumber}/Plan.md
  - Stage-{StageNumber}/PlanProgress.md
  - TestResults.md
---

<Workflow type="core" name="quick-fix" version="0.1">
## Quick Fix Workflow

**Use when:** Small changes, bug fixes, or well-understood modifications. Skips research and design.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | implementation-tdd | planner-tdd-soft | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | test-runner | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |

**Notes:**
- Single-stage plans use Stage-1/ folder for consistency (Decision 15)

</Workflow>

---

## Design Rationale

Put together quickly as a theoretical fast path for small, well-understood changes — skip RESEARCH and DESIGN, go straight from planning to implementation. It was never actually exercised on real work, and is retained deliberately as a documented anti-pattern rather than as something to route real work through.

The structural gap it demonstrates: EXECUTION goes straight to `implementation-tdd` with no `test-writer-tdd`/`tests-review-tdd` step ahead of it, so a bug fix under this workflow ships without a regression test proving the bug is fixed and stays fixed. Compounding that, there's no RESEARCH phase and `Requirements.md` isn't even in the artifact list — the workflow has no gate ensuring the "well-understood modification" is actually well understood before planning starts. Both gaps together show why the two things a small-fix workflow is tempted to cut — requirements clarity and regression coverage — are exactly the two things that catch the failure modes small fixes are most prone to. Worth keeping as the negative example for anyone designing a lighter-weight workflow in the future: this is what "lighter" looks like when it cuts the wrong things.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 0.1 | 2026-08-17 | MOSAIC | Changelog tracking begins here; earlier revisions predate this record. |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- **Move HITL off the creator row onto a convergence-gated `approval-presenter`.** `planner-tdd-soft` is gated `✅` directly, so a human reviews every draft plan it produces — including rounds `plan-review` would flag anyway — instead of only the version that already converged. `requirements-to-test-cases` proved the fix. Lower priority here than in the proven workflows, given this workflow's own unresolved status above.

**Dead ends (tried and rejected):**
- **quick-fix as a workflow you'd actually route real work through:** theoretical fast path, never used in practice. Kept in the catalog on purpose as a documented anti-pattern (missing test-writer step, missing requirements phase) rather than deleted — if a genuinely lighter-weight small-fix workflow is designed later, it should explicitly avoid these two cuts. Note it's also used as the example/fixture workflow ID across a large portion of the Deployment/Runner Go test suites and docs, independent of its catalog role — if its structure is ever revised as an anti-pattern demo, that dependency should be checked first.
