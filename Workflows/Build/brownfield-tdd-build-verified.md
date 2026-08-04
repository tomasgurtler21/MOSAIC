---
version: "2.1"
name: "Brownfield TDD Build-Verified Workflow"
description: "New features or significant changes to an existing codebase requiring test-first development where compilation/build cannot be verified via standard terminal tools (e.g., PLC/SCL with proprietary toolchains, embedded systems, cross-compilation environments)."
hint: "Brownfield TDD with build verification for proprietary toolchains"
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

[[SECTION:Workflow:brownfield-tdd-build-verified]]
<!-- workflow-version: 2.1 -->
## Brownfield TDD Build-Verified Workflow

**Use when:** New features or significant changes to an **existing codebase** requiring test-first development where **compilation/build cannot be verified via standard terminal tools** (e.g., PLC/SCL with proprietary toolchains, embedded systems, cross-compilation environments). Adds a dedicated build-and-deploy step between code writing and code review. Review agents execute tests on the target platform to verify TDD RED/GREEN phases.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ❌ | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | ❌ | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | build-review | ❌ | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-tests.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Implementation.[StageNumber] | implementation-tdd | ❌ | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Implementation.[StageNumber] | build-review | ❌ | implementation-review | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.Implementation.[StageNumber] | implementation-review | ❌ | COMPLETE | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-impl.md | Stage-{StageNumber}/implementation-review.md |

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

[[/SECTION:Workflow:brownfield-tdd-build-verified]]

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
