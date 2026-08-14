---
version: 0.1
name: Knowledge Base Correction Workflow
description: Apply known corrections to an existing knowledge base. Input is user-provided correction instructions in Requirements.md — could be pasted verification findings, direct feedback, or change descriptions.
hint: Apply targeted corrections to an existing knowledge base
author: MOSAIC
id: kb-correction
referenced_agents:
  - knowledge-base-generator
  - knowledge-base-index-assembler
artifacts:
  - Requirements.md
  - KBProgress.md
---

<Workflow type="core" name="kb-correction" version="0.1">
## Knowledge Base Correction Workflow

**Use when:** Apply known corrections to an existing knowledge base. Input is user-provided correction instructions in Requirements.md — could be pasted verification findings, direct feedback, or change descriptions. Generator navigates existing KB via KnowledgeBase/Index.md, determines what needs updating, and applies corrections.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | knowledge-base-generator(update) | ✅ | knowledge-base-index-assembler | - | Requirements.md | KBProgress.md |
| COMPLETION | knowledge-base-index-assembler | ❌ | COMPLETE | - | KBProgress.md | KBProgress.md |

**EXECUTION Stages:** First invocation reads Requirements.md and existing KB structure (KnowledgeBase/Index.md), creates KBProgress.md with one stage per KB document that needs updating. Subsequent stages update one KB document each.

**Notes:**
- **Requirements.md is user-created** — must contain correction instructions (verification findings, feedback, change descriptions)
- **KBProgress.md bootstrap** — first generator run creates KBProgress.md; it does not exist as a prerequisite
- **May create new tiers** — if corrections reveal areas needing deeper documentation, generator adds new stages to KBProgress.md
- **No re-verification** — after corrections, workflow completes. Run a separate verification workflow to confirm fixes
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
