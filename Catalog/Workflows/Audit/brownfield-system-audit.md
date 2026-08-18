---
version: "1.0"
name: "Brownfield System Audit Workflow"
description: "High-level quality assessment of an existing codebase or major subsystem — architecture and contracts audit without file-level analysis."
hint: "Theoretical, at most field-tested once — treat as unproven. Essentially the fixed architecture/contracts tracks trimmed out of brownfield-pr-audit, with the expensive staged per-file audits and PR comment integration removed. Use to scope out problem areas cheaply before committing to a deeper, per-file audit."
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

<Workflow type="core" name="brownfield-system-audit" version="1.0">
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

</Workflow>

---

## Design Rationale

An essential trim of `brownfield-pr-audit`: keeps its fixed architecture and contracts audit tracks (the high-level, whole-codebase passes) but drops the staged per-file audit tracks (tests-audit, implementation-audit) and the entire PR comment integration layer (audit-to-pull-request, audit-response-merger, pull-request-comment-interface). What's left is a cheap, high-level pass meant to identify problem areas before paying for expensive per-file analysis — its own notes call this out explicitly (output can guide follow-up per-component PR audits).

Theoretical in practice — tested at most once. Treat any claims about its real-world output quality as unverified until it accumulates more actual use.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-17 | MOSAIC | Changelog tracking begins here; earlier revisions predate this record. |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- (none yet)

**Dead ends (tried and rejected):**
- (none yet)
