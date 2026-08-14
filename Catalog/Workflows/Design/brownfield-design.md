---
version: "3.2"
name: "Brownfield Design Workflow"
description: "Architecture review, design proposals, or planning large features for an existing codebase without implementation."
hint: "Full design workflow for existing codebase — research, requirements, planning, and design"
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

<Workflow type="core" name="brownfield-design" version="3.2">
## Brownfield Design Workflow

> **Version:** 3.2

**Use when:** Architecture review, design proposals, or planning large features for an **existing codebase** without implementation.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ❌ | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | COMPLETE | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |

**Notes:**
- **Brownfield** = existing codebase with patterns to follow
- contracts-designer + contracts-review are optional - skip both if no new/modified contracts are needed
- Enable HITL on contracts-designer/contracts-review if user review is required

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
