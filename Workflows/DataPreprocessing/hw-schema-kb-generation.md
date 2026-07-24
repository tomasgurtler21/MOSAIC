---
version: 0.5
name: HW Schema Knowledge Base Generation Workflow
description: Generate knowledge base documentation for a hardware schematic design. Researches each sheet individually, then synthesizes domain-oriented KB documentation with tiered abstraction — from project overview down to complex circuit subsystems.
hint: Generate tiered KB documentation from hardware schematic sheets
author: MOSAIC
id: hw-schema-kb-generation
referenced_agents:
  - hw-schema-planner
  - hw-schema-research
  - hw-schema-kb-generator
  - knowledge-base-flag-sorter
  - knowledge-base-index-assembler
artifacts:
  - Requirements.md
  - HWResearchProgress.md
  - KBProgress.md
  - KBFlags.md
  - KBFlagReport.md
---

[[SECTION:Workflow:hw-schema-kb-generation]]
<!-- workflow-version: 0.5 -->
## HW Schema Knowledge Base Generation Workflow

**Use when:** Generate knowledge base documentation for a **hardware schematic design**. Researches each sheet individually, then synthesizes domain-oriented KB documentation with tiered abstraction — from project overview down to complex circuit subsystems.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| PLANNING | hw-schema-planner | ✅ | hw-schema-research* | - | - | Requirements.md | HWResearchProgress.md |
| RESEARCH.[StageNumber] | hw-schema-research | ❌ | hw-schema-kb-generator(generate) | - | - | Requirements.md, HWResearchProgress.md | HWResearchProgress.md |
| EXECUTION.[StageNumber] | hw-schema-kb-generator(generate) | ✅ | knowledge-base-flag-sorter | - | hw-schema-research* | Requirements.md, HWResearchProgress.md, KBProgress.md | KBProgress.md, KBFlags.md |
| REVIEW | knowledge-base-flag-sorter | ❌ | hw-schema-kb-generator(correct) | - | hw-schema-kb-generator(generate)* | KBProgress.md, KBFlags.md | KBFlagReport.md, KBProgress.md |
| REVIEW.[StageNumber] | hw-schema-kb-generator(correct) | ❌ | knowledge-base-index-assembler | - | - | KBProgress.md, KBFlagReport.md | KBProgress.md |
| COMPLETION | knowledge-base-index-assembler | ❌ | COMPLETE | - | - | KBProgress.md | KBProgress.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format above).

**RESEARCH Stages:** Each stage produces a per-sheet research file (e.g., `SheetsResearch/Sheet-03.md`) and records its output path in HWResearchProgress.md. Per-sheet research files are project files (not orchestration artifacts) — the KB generator accesses them via the file references in HWResearchProgress.md.

**EXECUTION Stages:** Tier-sequential dispatch — Tier 1 runs first (single stage, reads all per-sheet research via HWResearchProgress.md, creates KBProgress.md with KB stages). Deeper tiers dispatch in parallel from recommendations. HITL recommended for Tier 1; per-stage HITL in KBProgress.md allows disabling for deeper tiers.

**REVIEW Stages:** Correction stages run sequentially (bottom-up by tier — order matters). Flag-sorter adds one correction stage per target KB document to KBProgress.md.

**Notes:**
- **Requirements.md is user-created** — must contain schematic project path, KB output path, and optional focus areas
- **Two progress artifacts** — HWResearchProgress.md tracks per-sheet research stages; KBProgress.md tracks KB generation/correction stages. Different concerns, different lifecycles
- **Per-sheet research files are temporary** — project files in a dedicated directory (e.g., `SheetsResearch/`), referenced in HWResearchProgress.md. Can be cleaned up after workflow completion
- **KBProgress.md bootstrap** — first KB generator run creates KBProgress.md based on its analysis of all sheet research; it does not exist as a prerequisite
[[/SECTION:Workflow:hw-schema-kb-generation]]

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
