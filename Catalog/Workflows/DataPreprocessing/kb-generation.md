---
version: 0.5
name: Knowledge Base Generation Workflow
description: Generate N-tier knowledge base documentation for a codebase, producing hierarchical documentation optimized for AI agent navigation — tiered from project overview down to complex subsystem specs.
hint: Generate tiered KB documentation for a codebase with flag-based correction loop
author: MOSAIC
id: kb-generation
referenced_agents:
  - knowledge-base-generator
  - knowledge-base-flag-sorter
  - knowledge-base-index-assembler
artifacts:
  - Requirements.md
  - KBProgress.md
  - KBFlags.md
  - KBFlagReport.md
---

[[SECTION:Workflow:kb-generation]]
<!-- workflow-version: 0.5 -->
## Knowledge Base Generation Workflow

**Use when:** Generate N-tier knowledge base documentation for a codebase. Produces hierarchical documentation optimized for AI agent navigation — tiered from project overview down to complex subsystem specs.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| EXECUTION.[StageNumber] | knowledge-base-generator(generate) | ✅ | knowledge-base-flag-sorter | - | - | Requirements.md, KBProgress.md | KBProgress.md, KBFlags.md |
| REVIEW | knowledge-base-flag-sorter | ❌ | knowledge-base-generator(correct) | - | knowledge-base-generator(generate)* | KBProgress.md, KBFlags.md | KBFlagReport.md, KBProgress.md |
| REVIEW.[StageNumber] | knowledge-base-generator(correct) | ❌ | knowledge-base-index-assembler | - | - | KBProgress.md, KBFlagReport.md | KBProgress.md |
| COMPLETION | knowledge-base-index-assembler | ❌ | COMPLETE | - | - | KBProgress.md | KBProgress.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format above).

**EXECUTION Stages:** Tier-sequential dispatch — Tier 1 runs first (single stage). After all stages in one tier complete, dispatch all stages in the next tier in parallel. Each generator run may add deeper-tier stages to KBProgress.md. Flag-sorter waits for all generation stages across all tiers to complete. HITL recommended for Tier 1; per-stage HITL in KBProgress.md allows disabling for deeper tiers.

**REVIEW Stages:** Correction stages run sequentially (bottom-up by tier — order matters). Flag-sorter adds one correction stage per target KB document to KBProgress.md.

**Notes:**
- **Requirements.md is user-created** — must contain generation scope and optional focus areas
- **KBProgress.md bootstrap** — first generator run creates KBProgress.md; it does not exist as a prerequisite
- **Dynamic stage creation** — generators add deeper-tier stages to KBProgress.md during execution; flag-sorter adds correction stages after generation
- **Refresh/update** — re-run on an existing KB to refresh after codebase changes. Generator updates existing documents and flags new drift
- **Verification** — run a separate Knowledge Verification workflow after generation to test KB quality
[[/SECTION:Workflow:kb-generation]]

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
