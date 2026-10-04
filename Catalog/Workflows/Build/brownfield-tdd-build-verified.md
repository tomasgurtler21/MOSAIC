---
version: "2.4"
name: "Brownfield TDD Build-Verified Workflow"
description: "New features or significant changes to an existing codebase requiring test-first development where compilation/build cannot be verified via standard terminal tools (e.g., PLC/SCL with proprietary toolchains, embedded systems, cross-compilation environments)."
hint: "Field-verified (used for real PLC programming), not just theoretical. Use over standard brownfield-tdd whenever building and deployment require a non-trivial toolchain — offloading those mechanics onto a dedicated build-review agent keeps them out of the writer agents' context, while downstream reviewers execute tests."
author: MOSAIC
id: brownfield-tdd-build-verified
referenced_agents:
  - codebase-research
  - requirements-refinement
  - requirements-review
  - planner-tdd-soft
  - plan-review
  - contracts-designer
  - contracts-review
  - test-writer-tdd
  - build-review
  - tests-review-tdd
  - implementation-tdd
  - implementation-review
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
  - Stage-{StageNumber}/build-review-tests.md
  - Stage-{StageNumber}/tests-review-tdd.md
  - Stage-{StageNumber}/build-review-impl.md
  - Stage-{StageNumber}/implementation-review.md
---

<Workflow type="core" name="brownfield-tdd-build-verified" version="2.4">
## Brownfield TDD Build-Verified Workflow

**Use when:** New features or significant changes to an **existing codebase** requiring test-first development where **compilation/build cannot be verified via standard terminal tools** (e.g., PLC/SCL with proprietary toolchains, embedded systems, cross-compilation environments). Adds a dedicated build-and-deploy step between code writing and code review. Review agents execute tests on the target platform to verify TDD RED/GREEN phases.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | FALSE | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | planner-tdd-soft | requirements-refinement | Requirements.md, Research.md | requirements-review.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | TRUE | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | FALSE | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | build-review | FALSE | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-tests.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | FALSE | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | build-review | FALSE | implementation-review | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | FALSE | COMPLETE | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-impl.md | Stage-{StageNumber}/implementation-review.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Implementation |
| Implementation-First | Implementation, Test |
| Implementation-Only | Implementation |
| Tests-Only | Test |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). Subagent sequence per stage determined by the `Approach` column in the stage table.

**Notes:**
- **build-review** is a mechanical agent: imports source files into the build system, manages build dependencies, compiles/builds, deploys to target platform, and reports success/failure. On failure (`COMPLETED_NEEDS_ACTION`), routes back to the paired writer agent via On Findings.
- **build-review appears twice** per TDD stage — once after test writing (On Findings → test-writer-tdd), once after implementation (On Findings → implementation-tdd). Same agent, different On Findings targets per position. **Separate output artifacts** for context isolation: `build-review-tests.md` (test build) and `build-review-impl.md` (implementation build).
- **Review agents execute tests:** tests-review-tdd verifies TDD RED phase (tests fail because implementation is missing), implementation-review verifies TDD GREEN phase (tests pass after implementation). Each reads its respective build-review artifact for deployment metadata needed to trigger test execution on the target platform.
- **Brownfield** = existing codebase with patterns to discover and follow
- contracts-designer + contracts-review are optional — skip both if no new contracts are needed
- **When to use over standard Brownfield TDD:** Use this workflow when the build/compile toolchain is not accessible via standard terminal commands and requires specialized tool invocations (MCP servers, COM automation, proprietary IDEs, etc.)

</Workflow>

---

## Design Rationale

Identical to `brownfield-tdd` except for how build/test execution is handled. In standard `brownfield-tdd`, the execution agents (test-writer-tdd, implementation-tdd, and their reviewers) are expected to build and run tests themselves via simple terminal commands. This workflow exists for cases where that path doesn't work — the build/test toolchain is complex enough (proprietary IDEs, COM automation, MCP servers, cross-compilation, target deployment) that making every execution agent competent at it would overload their context with mechanics unrelated to their actual job.

The fix is a dedicated `build-review` agent that owns source import, build, and deployment exclusively, inserted between each writer and its reviewer. It does not execute tests. This keeps build-system and deployment mechanics in one place; downstream review agents use the deployment metadata in its report to execute tests and verify the appropriate TDD RED or GREEN outcome.

Field-verified on real PLC/SCL programming work, not just a theoretical variant.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 2.1 | 2026-08-17 | MOSAIC | Changelog tracking begins here; earlier revisions predate this record. |
| 2.2 | 2026-08-26 | MOSAIC | Replace Unicode emoji with ASCII tokens in HITL column (TRUE/FALSE). |
| 2.3 | 2026-09-28 | MOSAIC | Add Research.md to requirements-review inputs; its codebase alignment and feasibility checks run on research findings. |
| 2.4 | 2026-09-29 | MOSAIC | Clarify toolchain responsibility: build-review imports, builds, and deploys but does not execute tests; downstream review agents execute tests and verify TDD RED/GREEN outcomes. |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- **Move HITL off the creator rows onto a convergence-gated `approval-presenter`.** Same shape as `brownfield-tdd`: `requirements-refinement`, `planner-tdd-soft`, and `contracts-designer` are gated `TRUE` directly, so a human reviews every draft they produce — including rounds their paired reviewer would flag anyway — instead of only the version that already converged. `requirements-to-test-cases` proved the fix. Revisit this workflow once that pattern has enough real use to trust.

**Dead ends (tried and rejected):**
- (none yet)
