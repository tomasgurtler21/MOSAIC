---
version: "1.0"
name: "Brownfield System Audit Workflow"
description: "High-level quality assessment of an existing codebase or major subsystem — architecture and contracts audit without file-level analysis."
hint: "High-level system audit — architecture and contracts only, no file-level analysis"
author: MOSAIC
id: brownfield-system-audit
referenced_agents:
  - requirements-refinement
  - codebase-research
  - architecture-audit
  - contracts-audit
artifacts:
  - Requirements.md
  - Research.md
  - ResearchArchitecture.md
  - ResearchContracts.md
  - ArchitectureAudit.md
  - ContractsAudit.md
---

[[SECTION:Workflow:brownfield-system-audit]]
<!-- workflow-version: 1.0 -->
## Brownfield System Audit Workflow

> **Version:** 1.0

**Use when:** High-level quality assessment of an **existing codebase** or major subsystem — architecture and contracts audit without file-level analysis. Use to identify problem areas before deeper per-component audits.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| RESEARCH | requirements-refinement | ✅ | codebase-research | - | - | Requirements.md | Requirements.md |
| RESEARCH | codebase-research | ❌ | codebase-research(architecture) | - | - | Requirements.md | Research.md |
| RESEARCH | codebase-research(architecture) | ❌ | codebase-research(contracts), architecture-audit | - | - | Requirements.md, Research.md | ResearchArchitecture.md |
| RESEARCH | codebase-research(contracts) | ❌ | contracts-audit | - | - | Requirements.md, Research.md, ResearchArchitecture.md | ResearchContracts.md |
| EXECUTION | architecture-audit | ❌ | COMPLETE | - | - | Requirements.md, Research.md, ResearchArchitecture.md | ArchitectureAudit.md |
| EXECUTION | contracts-audit | ❌ | COMPLETE | - | - | Requirements.md, Research.md, ResearchContracts.md | ContractsAudit.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format above).

**Multi-pass research:** `codebase-research(focus)` invokes the same `codebase-research` subagent with a focused task description. The parenthetical suffix disambiguates rows in the workflow table.

**Notes:**
- **Requirements.md is user-created** — must contain audit scope (subsystem, codebase area, or full system) and focus areas
- **Audit completion = success** — findings are data, not failure states; all On Findings are `-`
- Output (ArchitectureAudit.md, ContractsAudit.md) can guide follow-up per-component PR Audit workflows
- Workflow completes when all dispatched subagents have finished (both `architecture-audit` and `contracts-audit` return COMPLETE)

[[/SECTION:Workflow:brownfield-system-audit]]

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
