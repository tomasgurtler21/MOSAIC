---
version: 0.2
name: Knowledge Base Correction Workflow
description: Apply known corrections to an existing knowledge base. Input is user-provided correction instructions in Requirements.md — could be pasted verification findings, direct feedback, or change descriptions.
hint: "Theoretical — no confirmed real use. Its niche may be narrower than it looks: kb-generation already refreshes an existing KB and flags drift on re-run, and in practice a planner can often fold small doc corrections into a normal feature plan without a dedicated pass. Consider whether generation's own refresh path already covers your case before reaching for this."
author: MOSAIC
id: kb-correction
referenced_agents:
  - knowledge-base-generator
  - knowledge-base-index-assembler
artifacts:
  - Requirements.md
  - KBProgress.md
---

<Workflow type="core" name="kb-correction" version="0.2">
## Knowledge Base Correction Workflow

**Use when:** Apply known corrections to an existing knowledge base. Input is user-provided correction instructions in Requirements.md — could be pasted verification findings, direct feedback, or change descriptions. Generator navigates existing KB via KnowledgeBase/Index.md, determines what needs updating, and applies corrections.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | knowledge-base-generator(update) | TRUE | knowledge-base-index-assembler | - | Requirements.md | KBProgress.md |
| COMPLETION | knowledge-base-index-assembler | FALSE | COMPLETE | - | KBProgress.md | KBProgress.md |

**EXECUTION Stages:** First invocation reads Requirements.md and existing KB structure (KnowledgeBase/Index.md), creates KBProgress.md with one stage per KB document that needs updating. Subsequent stages update one KB document each.

**Notes:**
- **Requirements.md is user-created** — must contain correction instructions (verification findings, feedback, change descriptions)
- **KBProgress.md bootstrap** — first generator run creates KBProgress.md; it does not exist as a prerequisite
- **May create new tiers** — if corrections reveal areas needing deeper documentation, generator adds new stages to KBProgress.md
- **No re-verification** — after corrections, workflow completes. Run a separate verification workflow to confirm fixes
</Workflow>

---

## Design Rationale

Intended as the targeted-correction counterpart to `kb-generation`: apply known, user-specified corrections (verification findings, direct feedback, change descriptions) rather than doing a full generation/refresh pass. No confirmed real use so far — theoretical.

Its actual necessity is unclear. `kb-generation` already handles refreshing an existing KB and flagging drift when re-run on a changed codebase, and separately, planners running normal feature work have shown they can often fold small KB corrections into their own plan without a dedicated correction pass. Between those two, the scope this workflow is meant to fill — corrections that are neither a full drift-driven refresh nor small enough to ride along in a feature plan — may be narrow enough that this workflow is overkill. Worth revisiting once (or if) a real case shows up that neither of the other two paths covers.

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
- **Possible redundancy with kb-generation's refresh path and planner-folded corrections.** Never confirmed in real use. Before investing further in this workflow, check whether a real correction case actually falls outside what kb-generation's refresh/drift-flagging and normal feature-plan doc updates already handle.

**Dead ends (tried and rejected):**
- (none yet)
