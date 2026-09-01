---
version: "0.2"
name: "Brownfield Research Only Workflow"
description: "Exploration, feasibility studies, or codebase analysis for an existing codebase without implementation."
hint: "Never used for real research work as far as known — its practical value has been as the simplest possible real workflow (one phase, one subagent, one HITL gate) to smoke-test that the orchestrator itself works end-to-end, distinct from the MosaicTest harness-conformance fixtures."
author: MOSAIC
id: brownfield-research-only
referenced_agents:
  - codebase-research
artifacts:
  - Research.md
---

<Workflow type="core" name="brownfield-research-only" version="0.2">
## Brownfield Research Only Workflow

> **Version:** 0.1

**Use when:** Exploration, feasibility studies, or codebase analysis for an **existing codebase** without implementation.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | TRUE | COMPLETE | - | - | Research.md |

**Notes:**
- **Brownfield** = existing codebase to analyze

</Workflow>

---

## Design Rationale

Structurally trivial by design: one phase, one subagent (`codebase-research`), one HITL gate, straight to COMPLETE. As a research deliverable this has seen little to no real use. Its actual value has been as a minimal real-workflow smoke test — proving the orchestrator can drive a standalone subagent through a complete run, as distinct from the `MosaicTest` category's dedicated harness-conformance fixtures (which test the harness, not the orchestrator's workflow-following behavior against a real, productive workflow).

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 0.1 | 2026-08-17 | MOSAIC | Changelog tracking begins here; earlier revisions predate this record. |
| 0.2 | 2026-08-26 | MOSAIC | Replace Unicode emoji with ASCII tokens in HITL column (TRUE/FALSE). |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- (none yet)

**Dead ends (tried and rejected):**
- (none yet)
