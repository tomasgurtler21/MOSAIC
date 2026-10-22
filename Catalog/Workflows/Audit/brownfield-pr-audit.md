---
version: "4.1"
name: "Brownfield PR Audit Workflow"
description: "Audit quality of existing code for PR review — multi-pass research, plan-driven staged audits with 4 types, per-audit PR comment transform with cross-audit deduplication, a final human-gated review of the consolidated comment queue, and post."
hint: "Proven, not just theoretical — run a handful of times against real PRs with sufficient quality. Its purpose is straightforward (let AI participate as a reviewer on a live pull request) but the machinery underneath is the most elaborate in the catalog — 4 audit types running as parallel stages, per-stage PR comment transforms, cross-audit dedup. Requirements.md must be user-authored with PR ID, branches, and scope before starting."
author: MOSAIC
id: brownfield-pr-audit
referenced_agents:
  - pull-request-comment-interface
  - pr-requirements-analyzer
  - codebase-research
  - planner-audit
  - tests-audit
  - audit-to-pull-request
  - implementation-audit
  - architecture-audit
  - audit-review
  - contracts-audit
  - audit-response-merger
  - pr-response-review
artifacts:
  - Requirements.md
  - PullRequestComments.md
  - PullRequestResponses.md
  - Research.md
  - AuditPlan.md
  - Stage-*/AuditPlan.md
  - Stage-*/AuditProgress.md
  - ResearchArchitecture.md
  - ResearchContracts.md
  - Stage-{Stage}/AuditPlan.md
  - Stage-{Stage}/AuditProgress.md
  - Stage-{Stage}/TestsAudit.md
  - Stage-{Stage}/ImplementationAudit.md
  - Stage-{Stage}/ArchitectureAudit.md
  - Stage-{Stage}/architecture-audit-review.md
  - Stage-{Stage}/ContractsAudit.md
  - Stage-{Stage}/contracts-audit-review.md
  - Stage-{Stage}/PullRequestResponses.md
  - Stage-{Stage}/TransformReport.md
  - Stage-*/PullRequestResponses.md
  - Stage-*/TransformReport.md
  - AuditTransformReport.md
  - PullRequestResponsesReview.md
---

<Workflow type="core" name="brownfield-pr-audit" version="4.1">
## Brownfield PR Audit Workflow

**Use when:** Audit quality of **existing code** for PR review — multi-pass research, plan-driven staged audits of 4 types running in parallel, per-audit PR comment transform with cross-audit deduplication, a final human-gated review of the consolidated comment queue that merges or drops redundant comments, and post.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | pull-request-comment-interface(retrieve) | FALSE | NEXT | - | Requirements.md | PullRequestComments.md, PullRequestResponses.md |
| RESEARCH | pr-requirements-analyzer | TRUE | NEXT | - | Requirements.md, PullRequestComments.md | Requirements.md |
| RESEARCH | codebase-research | FALSE | NEXT | - | Requirements.md | Research.md |
| PLANNING | planner-audit | TRUE | NEXT | - | Requirements.md, Research.md | AuditPlan.md, Stage-*/AuditPlan.md, Stage-*/AuditProgress.md |
| RESEARCH | codebase-research(architecture) | FALSE | NEXT | - | Requirements.md, Research.md | ResearchArchitecture.md |
| RESEARCH | codebase-research(contracts) | FALSE | [Stage] | - | Requirements.md, Research.md, ResearchArchitecture.md | ResearchContracts.md |
| EXECUTION.[Stage].Tests | tests-audit | FALSE | NEXT | - | Requirements.md, Research.md, Stage-{Stage}/AuditPlan.md, Stage-{Stage}/AuditProgress.md | Stage-{Stage}/TestsAudit.md, Stage-{Stage}/AuditProgress.md |
| EXECUTION.[Stage].Tests | audit-to-pull-request(tests) | FALSE | COMPLETE | - | Requirements.md, PullRequestComments.md, PullRequestResponses.md, Stage-{Stage}/TestsAudit.md | Stage-{Stage}/PullRequestResponses.md, Stage-{Stage}/TransformReport.md |
| EXECUTION.[Stage].Implementation | implementation-audit | FALSE | NEXT | - | Requirements.md, Research.md, Stage-{Stage}/AuditPlan.md, Stage-{Stage}/AuditProgress.md | Stage-{Stage}/ImplementationAudit.md, Stage-{Stage}/AuditProgress.md |
| EXECUTION.[Stage].Implementation | audit-to-pull-request(impl) | FALSE | COMPLETE | - | Requirements.md, PullRequestComments.md, PullRequestResponses.md, Stage-{Stage}/ImplementationAudit.md | Stage-{Stage}/PullRequestResponses.md, Stage-{Stage}/TransformReport.md |
| EXECUTION.[Stage].Architecture | architecture-audit | FALSE | NEXT | - | Requirements.md, Research.md, ResearchArchitecture.md, Stage-{Stage}/AuditPlan.md, Stage-{Stage}/AuditProgress.md | Stage-{Stage}/ArchitectureAudit.md, Stage-{Stage}/AuditProgress.md |
| EXECUTION.[Stage].Architecture | audit-review(architecture) | FALSE | NEXT | architecture-audit | Requirements.md, Research.md, ResearchArchitecture.md, Stage-{Stage}/ArchitectureAudit.md | Stage-{Stage}/architecture-audit-review.md |
| EXECUTION.[Stage].Architecture | audit-to-pull-request(architecture) | FALSE | COMPLETE | - | Requirements.md, PullRequestComments.md, PullRequestResponses.md, Stage-{Stage}/ArchitectureAudit.md, Stage-{Stage}/architecture-audit-review.md | Stage-{Stage}/PullRequestResponses.md, Stage-{Stage}/TransformReport.md |
| EXECUTION.[Stage].Contracts | contracts-audit | FALSE | NEXT | - | Requirements.md, Research.md, ResearchContracts.md, Stage-{Stage}/AuditPlan.md, Stage-{Stage}/AuditProgress.md | Stage-{Stage}/ContractsAudit.md, Stage-{Stage}/AuditProgress.md |
| EXECUTION.[Stage].Contracts | audit-review(contracts) | FALSE | NEXT | contracts-audit | Requirements.md, Research.md, ResearchContracts.md, Stage-{Stage}/ContractsAudit.md | Stage-{Stage}/contracts-audit-review.md |
| EXECUTION.[Stage].Contracts | audit-to-pull-request(contracts) | FALSE | COMPLETE | - | Requirements.md, PullRequestComments.md, PullRequestResponses.md, Stage-{Stage}/ContractsAudit.md, Stage-{Stage}/contracts-audit-review.md | Stage-{Stage}/PullRequestResponses.md, Stage-{Stage}/TransformReport.md |
| REVIEW | audit-response-merger | FALSE | NEXT | - | Requirements.md, PullRequestComments.md, AuditPlan.md, Stage-*/PullRequestResponses.md, Stage-*/TransformReport.md | PullRequestResponses.md, AuditTransformReport.md |
| REVIEW | pr-response-review | TRUE | NEXT | - | PullRequestResponses.md, AuditTransformReport.md, PullRequestComments.md | PullRequestResponses.md, PullRequestResponsesReview.md |
| COMPLETION | pull-request-comment-interface(post) | TRUE | COMPLETE | - | PullRequestResponses.md | PullRequestResponses.md |

**Blocks:**

| Block | Source | Section |
|-------|--------|---------|
| Stage | AuditPlan.md | Stages |

**Groups:**

| Block | Approach | Groups |
|-------|----------|--------|
| Stage | Tests | Tests |
| Stage | Implementation | Implementation |
| Stage | Architecture | Architecture |
| Stage | Contracts | Contracts |
</Workflow>

---

## Design Rationale

Purpose is simple: let AI participate as a reviewer on a live pull request, posting real PR comments. Everything else in the workflow's complexity exists in service of that one integration point — pulling existing PR comments in, and posting audit findings back out as comments, without losing track of which finding came from which audit.

Field-used only a couple of times so far, but with sufficient quality to call it proven rather than theoretical — worth trusting for real PR review work, not just a design exercise.

**Seed.** `Requirements.md` is user-created and must contain the PR ID, branches, audit scope and focus areas. Audit category recommendations in it are advisory: the planner decides which audit stages to create.

**Every audit is a stage (4.0).** Up to 3.2 the workflow ran two kinds of audit side by side: fixed whole-PR architecture and contracts tracks (a fork) and the planner's file stages (a block). WorkflowFormat 2.x forbids entering a block while a fork is open (B3), so the two can no longer overlap. Rather than serialise them, the fixed tracks became stages: `planner-audit` always adds one whole-PR `Architecture` stage and one whole-PR `Contracts` stage alongside its Implementation and Tests stages and any focused Architecture/Contracts deep-dives. All audits now run in parallel inside one block, one group per audit type chosen by the stage's `Approach`. The whole-PR stages are exempt from the planner's stage size limit — their audit is high-level; an agent that runs out of room returns `PARTIALLY_DONE` and is re-dispatched.

**Human gates early, then unattended.** The user approves the PR analysis and the audit plan up front; the focused research passes and every audit then run without intervention until the reviewed PR comments are presented for approval before posting. The architecture and contracts research run after the plan, sequentially (contracts research reads the architecture research), and before the block, because the Architecture and Contracts groups read them.

**Reviewed groups.** The Architecture and Contracts groups carry an `audit-review` step whose suggested fix target is the audit in the same group. Since 4.0 this also applies to focused deep-dive stages of those types, which previously went to PR comments unreviewed.

**Two-pass deduplication before posting (4.1).** Several audit types read the same files, so one issue often reaches the queue several times: at disjoint line ranges, as overlapping multi-bullet comments, or once per file for a systemic finding. A field run posted 105 comments of which 12-15 were such duplicates. `audit-response-merger` stays script-driven, because it must scale to dozens of partial files, and only sees candidate groups its scripts link. `pr-response-review` then reads the whole consolidated queue in one window (about 105 entries fit comfortably) and removes what the scripts cannot see, logging every merge and drop in `PullRequestResponsesReview.md`. The human gate moved from the merger to this row: approving a queue of 100+ comments is not a practical review, approving a short decision log plus the pruned queue is. `On Findings` is `-` because the agent repairs the queue itself; its only action-requiring outcome is malformed entries, which go to the human.

### Pattern: script-mediated context scaling via schema-conformant JSON artifacts

This workflow is the clearest demonstration in the catalog of a pattern worth naming explicitly, visible in how `audit-to-pull-request` and `audit-response-merger` interact.

The problem: `audit-response-merger` consolidates the partial PR-response-queue and transform-report artifacts written by every parallel `audit-to-pull-request` instance. With many parallel audit stages, that can mean 30+ partial files with hundreds of entries — reading them into context manually (find, open, extract, merge by hand) is not just expensive, it's not reliably possible; the agent runs out of context or loops.

The fix has two parts, and both matter:

1. **Producers write structured JSON embedded in markdown, conforming to a schema they read from a template artifact** — `audit-to-pull-request` doesn't invent its own output shape; it reads the response-queue schema from a template and writes to it exactly.
2. **The consumer discovers the schema from one sample, then writes scripts to process the rest.** `audit-response-merger` reads exactly one partial file of each type with `file_read` to learn the field names and structure, then authors and runs scripts that parse, extract, group, and merge every remaining file. LLM reasoning is reserved for the one step that's genuinely semantic — judging whether two candidate findings describe the same underlying issue — everything mechanical (parsing, linking candidates by line proximity, shared identifiers and text similarity, counting, assembling output) is scripted.

The critical dependency this creates: **both the artifact's producer and its consumer must be aware of its format up front**, so the consumer's scripts can run immediately against real data instead of needing a discovery phase against every file. That's why the schema lives in a template artifact both sides read, rather than being implicit in whatever the producer happens to write. Skipping this — letting producers freelance their output shape — would force the consumer back into per-file manual reading, defeating the entire pattern.

This is a reusable pattern beyond this workflow specifically: whenever a workflow fans out to many parallel instances that need to be reconsolidated, and the volume of partial output would overwhelm a merging agent's context if read manually, the answer is the same — schema-conformant JSON-in-markdown artifacts plus a script-driven consumer that only spends LLM reasoning on genuinely semantic judgment calls.

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 3.1 | 2026-08-17 | MOSAIC | Changelog tracking begins here; earlier revisions predate this record. |
| 3.2 | 2026-08-26 | MOSAIC | Replace Unicode emoji with ASCII tokens in HITL column (TRUE/FALSE). |
| 4.0 | 2026-10-10 | MOSAIC | Migrated to WorkflowFormat 2.16 and restructured: the fixed architecture/contracts tracks (a fork) and the planner's stages (a block) could no longer run side by side (B3), so the fixed tracks became whole-PR `Architecture` and `Contracts` stages that `planner-audit` always creates. One declared `[Stage]` block (`AuditPlan.md`, `Stages`) with one group per audit type selected by `Approach`; `(staged)` duplicate rows, the fork and the `Waits For` column removed. Focused research now runs after planning and before the block. Legacy `*` targets, the in-region version line and the notes removed (seed note moved to Design Rationale). |
| 4.1 | 2026-10-21 | MOSAIC | Added `pr-response-review` (REVIEW, HITL TRUE) between `audit-response-merger` and the poster: a whole-queue pass that merges same-issue comments, drops per-file copies of systemic findings and comments covered by open PR threads, and writes `PullRequestResponsesReview.md`. The human gate moved from the merger (now HITL FALSE) to this row. |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- (none yet)

**Dead ends (tried and rejected):**
- **Fixed tracks alongside the stage block (≤ 3.2).** A fork running whole-PR audits in parallel with the planner's stage block. Not expressible since WorkflowFormat 2.7 (no block entered while a fork is open); replaced by whole-PR stages in 4.0.
