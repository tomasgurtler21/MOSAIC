---
version: "1.0"
name: "Product Comparison Workflow"
description: "Compares two or more existing product codebases across a fixed set of dimensions — each product researched independently, then synthesized into a side-by-side comparison."
hint: "Very easy to use as template for any comparison, just remove/add topics as needed."
author: MOSAIC
id: product-comparison
referenced_agents:
  - product-research
  - end-user-experience-research
  - maintainability-research
  - extensibility-research
  - design-quality-research
  - quality-mechanisms-research
  - human-oversight-research
  - cost-research
  - security-research
  - performance-research
  - topic-analyst
  - comparison-analyst
  - comparison-review
artifacts:
  - Requirements.md
  - "{Product}-ProductResearch.md"
  - "{Product}-EndUserExperienceResearch.md"
  - "{Product}-MaintainabilityResearch.md"
  - "{Product}-ExtensibilityResearch.md"
  - "{Product}-DesignQualityResearch.md"
  - "{Product}-QualityMechanismsResearch.md"
  - "{Product}-HumanOversightResearch.md"
  - "{Product}-CostResearch.md"
  - "{Product}-SecurityResearch.md"
  - "{Product}-PerformanceResearch.md"
  - "{Topic}Comparison.md"
  - ComparisonAnalysis.md
  - comparison-review.md
---

<Workflow type="core" name="product-comparison" version="1.0">
## Product Comparison Workflow

**Use when:** Comparing two or more **existing product codebases** that solve a similar problem. Each product is researched and analyzed independently across a fixed set of dimensions, then synthesized into a single side-by-side comparison.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | product-research | - | end-user-experience-research, maintainability-research, extensibility-research, design-quality-research, quality-mechanisms-research, human-oversight-research, cost-research, security-research, performance-research | - | Requirements.md | {Product}-ProductResearch.md |
| RESEARCH | end-user-experience-research | - | topic-analyst | - | Requirements.md, {Product}-ProductResearch.md | {Product}-EndUserExperienceResearch.md |
| RESEARCH | maintainability-research | - | topic-analyst | - | Requirements.md, {Product}-ProductResearch.md | {Product}-MaintainabilityResearch.md |
| RESEARCH | extensibility-research | - | topic-analyst | - | Requirements.md, {Product}-ProductResearch.md | {Product}-ExtensibilityResearch.md |
| RESEARCH | design-quality-research | - | topic-analyst | - | Requirements.md, {Product}-ProductResearch.md | {Product}-DesignQualityResearch.md |
| RESEARCH | quality-mechanisms-research | - | topic-analyst | - | Requirements.md, {Product}-ProductResearch.md | {Product}-QualityMechanismsResearch.md |
| RESEARCH | human-oversight-research | - | topic-analyst | - | Requirements.md, {Product}-ProductResearch.md | {Product}-HumanOversightResearch.md |
| RESEARCH | cost-research | - | topic-analyst | - | Requirements.md, {Product}-ProductResearch.md | {Product}-CostResearch.md |
| RESEARCH | security-research | - | topic-analyst | - | Requirements.md, {Product}-ProductResearch.md | {Product}-SecurityResearch.md |
| RESEARCH | performance-research | - | topic-analyst | - | Requirements.md, {Product}-ProductResearch.md | {Product}-PerformanceResearch.md |
| EXECUTION | topic-analyst | - | comparison-analyst | {Topic}-research | Requirements.md, *-{Topic}Research.md | {Topic}Comparison.md |
| EXECUTION | comparison-analyst | - | comparison-review | topic-analyst | Requirements.md, *Comparison.md | ComparisonAnalysis.md |
| REVIEW | comparison-review | - | COMPLETE | comparison-analyst | Requirements.md, ComparisonAnalysis.md, *Comparison.md | comparison-review.md |

**Notes:**
- **No planner.** Requirements.md is user-authored — lists each product (name + repo path) and comparison criteria. The orchestrator fans out one branch per product directly.
- **Placeholders:** `{Product}` = product identifier from Requirements.md. `{Topic}` = dimension name (e.g., `maintainability`). `*` = glob across products or dimensions.
- **Tier 1 agents are verdict-free** — they document *what is* for a single product, no quality judgments. Evaluation starts at tier 2 (`topic-analyst`).
- **Barriers are implicit from artifacts** — `topic-analyst` cannot run until all products' findings for its dimension exist; `comparison-analyst` cannot run until all `*Comparison.md` exist.
- **Pluggable dimensions:** add a `{dim}-research` row + its entry in `product-research`'s On Success; remove to drop. `topic-analyst` is generic.

</Workflow>

---

## Design Rationale

This workflow was designed for and successfully tested on comparing multi-agent orchestration frameworks — two complex software products analyzed across nine quality dimensions. The three-tier architecture (findings then per-topic comparison then synthesis) emerged from the insight that fair comparison requires strict separation between evidence gathering and evaluation.

**Why separate dimension research agents instead of one generic dimension-research agent:** Each dimension agent carries dimension-specific guidance (what to look for, where evidence typically lives, illustrative investigation angles) baked into its instructions. A single generic agent would need this guidance injected per-invocation, which the current architecture doesn't support — injections are set at deploy time, not at dispatch time. The trade-off is more agents in the catalog; the gain is that each dimension agent knows its domain cold without runtime parameterization.

**Why no planner:** The product list comes directly from the user-authored Requirements.md. There's nothing to plan — the orchestrator reads the product list and seeds one branch per product. This is a deliberate simplification: the set of dimensions is fixed in the workflow table, and the set of products is fixed in Requirements.md. A planner would add cost and latency for zero decision value.

**Why topic-analyst is generic (one agent, dispatched per dimension) while dimension research agents are specialized:** Different design pressures. A dimension research agent reads raw code and needs dimension-specific guidance on what to look for. A topic-analyst reads structured findings artifacts that already share a common format — it compares products within a dimension, and the comparison discipline is identical regardless of dimension. Specializing it would multiply agents with no quality gain.

**Why EXECUTION phase for analysis agents:** The orchestrator's phase vocabulary (RESEARCH, ARCHITECTURE, PLANNING, DESIGN, EXECUTION, REVIEW, COMPLETION) doesn't include an ANALYSIS phase. EXECUTION is the "do the main work" phase, which fits — the analysis/synthesis tiers are the main work of this workflow, distinct from both the upstream research (evidence gathering) and the downstream review (quality gate).

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-08-20 | MOSAIC | Initial catalog version — extracted and converted from the old orchestrator-embedded workflow that was successfully tested on multi-agent framework comparison |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- **HITL on comparison-review or comparison-analyst:** The current workflow runs fully autonomous (no HITL gates). For high-stakes comparisons, adding HITL on `comparison-review` or `comparison-analyst` would let a human validate the synthesis before completion. Not added in v1.0 because the first successful test ran without HITL and the review gate caught issues adequately.
- **Configurable dimension sets:** Instead of listing all 9 dimensions in the workflow table, allow Requirements.md to specify which dimensions to run. This would require orchestrator-level support for conditional row activation, which doesn't exist yet.

**Dead ends (tried and rejected):**
- (none yet — this is a first catalog extraction)
