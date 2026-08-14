---
version: "2.1"
name: "Brownfield Research Only Workflow"
description: "Exploration, feasibility studies, or codebase analysis for an existing codebase without implementation."
hint: "Research-only for existing codebase — no planning, design, or implementation"
author: MOSAIC
id: brownfield-research-only
referenced_agents:
  - codebase-research
artifacts:
  - Research.md
---

<Workflow type="core" name="brownfield-research-only" version="2.1">
## Brownfield Research Only Workflow

> **Version:** 2.1

**Use when:** Exploration, feasibility studies, or codebase analysis for an **existing codebase** without implementation.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ✅ | COMPLETE | - | - | Research.md |

**Notes:**
- **Brownfield** = existing codebase to analyze

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
