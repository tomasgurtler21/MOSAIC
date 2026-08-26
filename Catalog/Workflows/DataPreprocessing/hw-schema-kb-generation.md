---
version: 0.6
name: HW Schema Knowledge Base Generation Workflow
description: Generate knowledge base documentation for a hardware schematic design. Researches each sheet individually, then synthesizes domain-oriented KB documentation with tiered abstraction — from project overview down to complex circuit subsystems.
hint: "Tested and polished a few times, output verified — not production-proven but solid. Hard prerequisite before deploying: the research step requires a working hw_schema_read tool wired up for your specific schematic format/tooling — this is not a standard tool available out of the box, and the workflow cannot function without it. Verify that tool is provided before selecting this workflow."
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

<Workflow type="core" name="hw-schema-kb-generation" version="0.6">
## HW Schema Knowledge Base Generation Workflow

**Use when:** Generate knowledge base documentation for a **hardware schematic design**. Researches each sheet individually, then synthesizes domain-oriented KB documentation with tiered abstraction — from project overview down to complex circuit subsystems.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| PLANNING | hw-schema-planner | TRUE | hw-schema-research* | - | - | Requirements.md | HWResearchProgress.md |
| RESEARCH.[StageNumber] | hw-schema-research | FALSE | hw-schema-kb-generator(generate) | - | - | Requirements.md, HWResearchProgress.md | HWResearchProgress.md |
| EXECUTION.[StageNumber] | hw-schema-kb-generator(generate) | TRUE | knowledge-base-flag-sorter | - | hw-schema-research* | Requirements.md, HWResearchProgress.md, KBProgress.md | KBProgress.md, KBFlags.md |
| REVIEW | knowledge-base-flag-sorter | FALSE | hw-schema-kb-generator(correct) | - | hw-schema-kb-generator(generate)* | KBProgress.md, KBFlags.md | KBFlagReport.md, KBProgress.md |
| REVIEW.[StageNumber] | hw-schema-kb-generator(correct) | FALSE | knowledge-base-index-assembler | - | - | KBProgress.md, KBFlagReport.md | KBProgress.md |
| COMPLETION | knowledge-base-index-assembler | FALSE | COMPLETE | - | - | KBProgress.md | KBProgress.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format above).

**RESEARCH Stages:** Each stage produces a per-sheet research file (e.g., `SheetsResearch/Sheet-03.md`) and records its output path in HWResearchProgress.md. Per-sheet research files are project files (not orchestration artifacts) — the KB generator accesses them via the file references in HWResearchProgress.md.

**EXECUTION Stages:** Tier-sequential dispatch — Tier 1 runs first (single stage, reads all per-sheet research via HWResearchProgress.md, creates KBProgress.md with KB stages). Deeper tiers dispatch in parallel from recommendations. HITL recommended for Tier 1; per-stage HITL in KBProgress.md allows disabling for deeper tiers.

**REVIEW Stages:** Correction stages run sequentially (bottom-up by tier — order matters). Flag-sorter adds one correction stage per target KB document to KBProgress.md.

**Notes:**
- **Requirements.md is user-created** — must contain schematic project path, KB output path, and optional focus areas
- **Two progress artifacts** — HWResearchProgress.md tracks per-sheet research stages; KBProgress.md tracks KB generation/correction stages. Different concerns, different lifecycles
- **Per-sheet research files are temporary** — project files in a dedicated directory (e.g., `SheetsResearch/`), referenced in HWResearchProgress.md. Can be cleaned up after workflow completion
- **KBProgress.md bootstrap** — first KB generator run creates KBProgress.md based on its analysis of all sheet research; it does not exist as a prerequisite
</Workflow>

---

## Design Rationale

Tested a handful of times with output manually verified and the workflow polished as a result — not yet production-proven, but past the purely theoretical stage.

**This workflow has an unusually explicit external dependency, worth calling out at the workflow level rather than leaving it buried in an agent's tool list.** The research stage requires the ability to actually read hardware schematic sheets, which happens through a dedicated tool the research agent depends on. Unlike the file/terminal tools every other workflow in the catalog assumes are simply available, this one is not generic — it has to be provided by whoever deploys the workflow, wired to their specific schematic format and tooling. Every other workflow in the catalog gets by on generic file/terminal access; this is the one case where the workflow cannot run at all without a project-specific tool being present first. Users select workflows, not agents, so this dependency needs to be visible here, not only inside the research agent's own instructions.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 0.5 | 2026-08-17 | MOSAIC | Changelog tracking begins here; earlier revisions predate this record. |
| 0.6 | 2026-08-26 | MOSAIC | Replace Unicode emoji with ASCII tokens in HITL column (TRUE/FALSE). |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- (none yet)

**Dead ends (tried and rejected):**
- (none yet)
