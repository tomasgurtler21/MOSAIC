---
version: 1.0
name: Knowledge Base Generation Workflow
description: Generate N-tier knowledge base documentation for a codebase, producing hierarchical documentation optimized for AI agent navigation — tiered from project overview down to complex subsystem specs.
hint: "Recommended first step before any other workflow on a codebase — research agents downstream actively look for an existing KB and perform noticeably better when one is present. The KB is deliberately abstract (no concrete file/class/method names), which makes it resistant to going stale — in practice, feature planners can often fold doc updates into a normal feature plan without much quality loss, so a dedicated re-run of this workflow isn't required after every change."
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

<Workflow type="core" name="kb-generation" version="1.0">
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
</Workflow>

---

## Design Rationale

Excellent, recommended workflow — run this first on a codebase before starting any other workflow against it. The research-phase agents used across the Build/Audit/Design workflows actively look for an existing KB when they run, and their performance improves noticeably when one is present.

The KB's tiered structure is deliberately kept abstract — general subsystem descriptions rather than concrete file, class, or method names — which trades a little specificity for resistance to going stale as the codebase changes underneath it. This pays off in practice: planning agents elsewhere in the catalog can often fold documentation updates for a change directly into a normal feature plan without much quality loss, precisely because the abstraction level means most code-level changes don't invalidate what the KB actually says. That means `kb-correction` is not required after every change — it's there for drift that accumulates past what in-line plan updates catch, not as a mandatory step in every feature's lifecycle.

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
