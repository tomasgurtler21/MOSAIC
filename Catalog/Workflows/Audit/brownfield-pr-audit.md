---
version: "3.1"
name: "Brownfield PR Audit Workflow"
description: "Audit quality of existing code for PR review — multi-pass research, plan-driven staged audits with 4 types, per-audit PR comment transform with cross-audit deduplication, and post."
hint: "Proven, not just theoretical — run a handful of times against real PRs with sufficient quality. Its purpose is straightforward (let AI participate as a reviewer on a live pull request) but the machinery underneath is the most elaborate in the catalog — 4 parallel audit tracks, per-track PR comment transforms, cross-audit dedup. Requirements.md must be user-authored with PR ID, branches, and scope before starting."
author: MOSAIC
id: brownfield-pr-audit
referenced_agents:
  - pull-request-comment-interface
  - pr-requirements-analyzer
  - codebase-research
  - planner-audit
  - architecture-audit
  - audit-review
  - contracts-audit
  - tests-audit
  - implementation-audit
  - audit-to-pull-request
  - audit-response-merger
artifacts:
  - Requirements.md
  - PullRequestComments.md
  - PullRequestResponses.md
  - Research.md
  - ResearchArchitecture.md
  - ResearchContracts.md
  - AuditPlan.md
  - "Stage-*/AuditPlan.md"
  - "Stage-*/AuditProgress.md"
  - ArchitectureAudit.md
  - architecture-audit-review.md
  - ContractsAudit.md
  - contracts-audit-review.md
  - ArchitectureAudit-PullRequestResponses.md
  - ArchitectureAudit-TransformReport.md
  - ContractsAudit-PullRequestResponses.md
  - ContractsAudit-TransformReport.md
  - "Stage-{StageNumber}/TestsAudit.md"
  - "Stage-{StageNumber}/ImplementationAudit.md"
  - "Stage-{StageNumber}/ArchitectureAudit.md"
  - "Stage-{StageNumber}/ContractsAudit.md"
  - "Stage-{StageNumber}/PullRequestResponses.md"
  - "Stage-{StageNumber}/TransformReport.md"
  - "*-PullRequestResponses.md"
  - "*-TransformReport.md"
  - "Stage-*/PullRequestResponses.md"
  - "Stage-*/TransformReport.md"
  - AuditTransformReport.md
---

<Workflow type="core" name="brownfield-pr-audit" version="3.1">
## Brownfield PR Audit Workflow

> **Version:** 3.1

**Use when:** Audit quality of **existing code** for PR review — multi-pass research (general → architecture → contracts), plan-driven staged audits with 4 types, per-audit PR comment transform with cross-audit deduplication, and post. Leverages parallel execution for independent audit tracks and per-audit `audit-to-pull-request` instances.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| RESEARCH | pull-request-comment-interface(retrieve) | ❌ | pr-requirements-analyzer | - | - | Requirements.md | PullRequestComments.md, PullRequestResponses.md |
| RESEARCH | pr-requirements-analyzer | ✅ | codebase-research | - | - | Requirements.md, PullRequestComments.md | Requirements.md |
| RESEARCH | codebase-research | ❌ | codebase-research(architecture), planner-audit | - | - | Requirements.md | Research.md |
| RESEARCH | codebase-research(architecture) | ❌ | codebase-research(contracts), architecture-audit | - | - | Requirements.md, Research.md | ResearchArchitecture.md |
| PLANNING | planner-audit | ✅ | tests-audit*, implementation-audit*, architecture-audit*, contracts-audit* | - | - | Requirements.md, Research.md | AuditPlan.md, Stage-*/AuditPlan.md, Stage-*/AuditProgress.md |
| RESEARCH | codebase-research(contracts) | ❌ | contracts-audit | - | - | Requirements.md, Research.md, ResearchArchitecture.md | ResearchContracts.md |
| EXECUTION | architecture-audit | ❌ | audit-review(architecture) | - | - | Requirements.md, Research.md, ResearchArchitecture.md | ArchitectureAudit.md |
| EXECUTION | audit-review(architecture) | ❌ | audit-to-pull-request(architecture) | architecture-audit | - | Requirements.md, Research.md, ResearchArchitecture.md, ArchitectureAudit.md | architecture-audit-review.md |
| EXECUTION | contracts-audit | ❌ | audit-review(contracts) | - | - | Requirements.md, Research.md, ResearchContracts.md | ContractsAudit.md |
| EXECUTION | audit-review(contracts) | ❌ | audit-to-pull-request(contracts) | contracts-audit | - | Requirements.md, Research.md, ResearchContracts.md, ContractsAudit.md | contracts-audit-review.md |
| EXECUTION.[StageNumber] | tests-audit | ❌ | audit-to-pull-request(tests) | - | - | Requirements.md, Research.md, Stage-{StageNumber}/AuditPlan.md, Stage-{StageNumber}/AuditProgress.md | Stage-{StageNumber}/TestsAudit.md, Stage-{StageNumber}/AuditProgress.md |
| EXECUTION.[StageNumber] | implementation-audit | ❌ | audit-to-pull-request(impl) | - | - | Requirements.md, Research.md, Stage-{StageNumber}/AuditPlan.md, Stage-{StageNumber}/AuditProgress.md | Stage-{StageNumber}/ImplementationAudit.md, Stage-{StageNumber}/AuditProgress.md |
| EXECUTION.[StageNumber] | architecture-audit(staged) | ❌ | audit-to-pull-request(arch-staged) | - | - | Requirements.md, Research.md, ResearchArchitecture.md, Stage-{StageNumber}/AuditPlan.md, Stage-{StageNumber}/AuditProgress.md | Stage-{StageNumber}/ArchitectureAudit.md, Stage-{StageNumber}/AuditProgress.md |
| EXECUTION.[StageNumber] | contracts-audit(staged) | ❌ | audit-to-pull-request(contracts-staged) | - | - | Requirements.md, Research.md, ResearchContracts.md, Stage-{StageNumber}/AuditPlan.md, Stage-{StageNumber}/AuditProgress.md | Stage-{StageNumber}/ContractsAudit.md, Stage-{StageNumber}/AuditProgress.md |
| EXECUTION | audit-to-pull-request(architecture) | ❌ | audit-response-merger | - | - | Requirements.md, PullRequestComments.md, PullRequestResponses.md, ArchitectureAudit.md, architecture-audit-review.md | ArchitectureAudit-PullRequestResponses.md, ArchitectureAudit-TransformReport.md |
| EXECUTION | audit-to-pull-request(contracts) | ❌ | audit-response-merger | - | - | Requirements.md, PullRequestComments.md, PullRequestResponses.md, ContractsAudit.md, contracts-audit-review.md | ContractsAudit-PullRequestResponses.md, ContractsAudit-TransformReport.md |
| EXECUTION.[StageNumber] | audit-to-pull-request(tests) | ❌ | audit-response-merger | - | - | Requirements.md, PullRequestComments.md, PullRequestResponses.md, Stage-{StageNumber}/TestsAudit.md | Stage-{StageNumber}/PullRequestResponses.md, Stage-{StageNumber}/TransformReport.md |
| EXECUTION.[StageNumber] | audit-to-pull-request(impl) | ❌ | audit-response-merger | - | - | Requirements.md, PullRequestComments.md, PullRequestResponses.md, Stage-{StageNumber}/ImplementationAudit.md | Stage-{StageNumber}/PullRequestResponses.md, Stage-{StageNumber}/TransformReport.md |
| EXECUTION.[StageNumber] | audit-to-pull-request(arch-staged) | ❌ | audit-response-merger | - | - | Requirements.md, PullRequestComments.md, PullRequestResponses.md, Stage-{StageNumber}/ArchitectureAudit.md | Stage-{StageNumber}/PullRequestResponses.md, Stage-{StageNumber}/TransformReport.md |
| EXECUTION.[StageNumber] | audit-to-pull-request(contracts-staged) | ❌ | audit-response-merger | - | - | Requirements.md, PullRequestComments.md, PullRequestResponses.md, Stage-{StageNumber}/ContractsAudit.md | Stage-{StageNumber}/PullRequestResponses.md, Stage-{StageNumber}/TransformReport.md |
| REVIEW | audit-response-merger | ✅ | pull-request-comment-interface(post) | - | audit-to-pull-request(architecture), audit-to-pull-request(contracts), audit-to-pull-request(tests)\*, audit-to-pull-request(impl)\*, audit-to-pull-request(arch-staged)\*, audit-to-pull-request(contracts-staged)\* | Requirements.md, PullRequestComments.md, AuditPlan.md, *-PullRequestResponses.md, Stage-*/PullRequestResponses.md, *-TransformReport.md, Stage-*/TransformReport.md | PullRequestResponses.md, AuditTransformReport.md |
| COMPLETION | pull-request-comment-interface(post) | ✅ | COMPLETE | - | - | PullRequestResponses.md | PullRequestResponses.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format above).

**Multi-pass research:** `codebase-research(focus)` invokes the same `codebase-research` subagent with a focused task description. The parenthetical suffix disambiguates rows in the workflow table.

**Plan-driven audit types:** Planner creates stages with 4 types: Implementation, Tests, Architecture, Contracts. Architecture/Contracts stages are optional — planner creates them only for focused deep-dives. The fixed (non-staged) architecture/contracts tracks always run for general high-level audit.

**Notes:**
- **Requirements.md is user-created** — must contain PR ID, branches, audit scope, and focus areas
- Audit category recommendations in Requirements.md are advisory — the workflow always dispatches the fixed architecture/contracts tracks; planner decides which staged types to create
- `architecture-audit(staged)` / `contracts-audit(staged)` — same underlying agent as the fixed instances; parenthetical suffix disambiguates rows

</Workflow>

---

## Design Rationale

Purpose is simple: let AI participate as a reviewer on a live pull request, posting real PR comments. Everything else in the workflow's complexity exists in service of that one integration point — pulling existing PR comments in, and posting audit findings back out as comments, without losing track of which finding came from which audit track.

Field-used only a couple of times so far, but with sufficient quality to call it proven rather than theoretical — worth trusting for real PR review work, not just a design exercise.

### Pattern: script-mediated context scaling via schema-conformant JSON artifacts

This workflow is the clearest demonstration in the catalog of a pattern worth naming explicitly, visible in how `audit-to-pull-request` and `audit-response-merger` interact.

The problem: `audit-response-merger` consolidates the partial PR-response-queue and transform-report artifacts written by every parallel `audit-to-pull-request` instance. With many parallel audit tracks across many stages, that can mean 30+ partial files with hundreds of entries — reading them into context manually (find, open, extract, merge by hand) is not just expensive, it's not reliably possible; the agent runs out of context or loops.

The fix has two parts, and both matter:

1. **Producers write structured JSON embedded in markdown, conforming to a schema they read from a template artifact** — `audit-to-pull-request` doesn't invent its own output shape; it reads the response-queue schema from a template and writes to it exactly.
2. **The consumer discovers the schema from one sample, then writes scripts to process the rest.** `audit-response-merger` reads exactly one partial file of each type with `file_read` to learn the field names and structure, then authors and runs scripts that parse, extract, group, and merge every remaining file. LLM reasoning is reserved for the one step that's genuinely semantic — judging whether two candidate findings describe the same underlying issue — everything mechanical (parsing, grouping by overlapping line ranges, counting, assembling output) is scripted.

The critical dependency this creates: **both the artifact's producer and its consumer must be aware of its format up front**, so the consumer's scripts can run immediately against real data instead of needing a discovery phase against every file. That's why the schema lives in a template artifact both sides read, rather than being implicit in whatever the producer happens to write. Skipping this — letting producers freelance their output shape — would force the consumer back into per-file manual reading, defeating the entire pattern.

This is a reusable pattern beyond this workflow specifically: whenever a workflow fans out to many parallel instances that need to be reconsolidated, and the volume of partial output would overwhelm a merging agent's context if read manually, the answer is the same — schema-conformant JSON-in-markdown artifacts plus a script-driven consumer that only spends LLM reasoning on genuinely semantic judgment calls.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 3.1 | 2026-08-17 | MOSAIC | Changelog tracking begins here; earlier revisions predate this record. |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- (none yet)

**Dead ends (tried and rejected):**
- (none yet)
