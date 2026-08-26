---
version: "0.2"
name: "Brownfield PR Fix Workflow"
description: "Resolve PR review comments on an existing codebase — fetch comments, refine scope and test strategy with user, research, plan and implement fixes with optional TDD, generate per-stage resolution replies, merge, and post."
hint: "Fresh new untested workflow, created by combining brownfield-tdd with brownfield-pr-audit, with some modifications."
author: MOSAIC
id: brownfield-pr-fix
referenced_agents:
  - pull-request-comment-interface
  - requirements-refinement
  - requirements-review
  - codebase-research
  - planner-tdd-soft
  - plan-review
  - test-writer-tdd
  - tests-review-tdd
  - implementation-tdd
  - implementation-review
  - fix-to-pr-response
  - test-runner
  - response-merger
artifacts:
  - Requirements.md
  - PullRequestComments.md
  - PullRequestResponses.md
  - requirements-review.md
  - Research.md
  - Plan.md
  - Stage-*/Plan.md
  - Stage-*/PlanProgress.md
  - plan-review.md
  - Stage-{StageNumber}/Plan.md
  - Stage-{StageNumber}/PlanProgress.md
  - Stage-{StageNumber}/tests-review-tdd.md
  - Stage-{StageNumber}/implementation-review.md
  - Stage-{StageNumber}/PullRequestResponses.md
  - TestResults.md
---

<Workflow type="core" name="brownfield-pr-fix" version="1.0">
## Brownfield PR Fix Workflow

**Use when:** Resolving PR review comments on an **existing codebase** — fetch active comments, refine scope and test strategy with user, research context, plan and implement fixes, generate resolution replies per stage, merge, and post back to the PR.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | pull-request-comment-interface(retrieve) | FALSE | requirements-refinement | - | Requirements.md | PullRequestComments.md, PullRequestResponses.md |
| RESEARCH | requirements-refinement | TRUE | requirements-review | - | Requirements.md, PullRequestComments.md | Requirements.md |
| RESEARCH | requirements-review | FALSE | codebase-research | requirements-refinement | Requirements.md | requirements-review.md |
| RESEARCH | codebase-research | FALSE | planner-tdd-soft | - | Requirements.md, PullRequestComments.md | Research.md |
| PLANNING | planner-tdd-soft | TRUE | plan-review | - | Research.md, Requirements.md, PullRequestComments.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | FALSE | test-writer-tdd* | planner-tdd-soft | Requirements.md, PullRequestComments.md, Plan.md, Stage-*/Plan.md | plan-review.md |
| EXECUTION.Test.[StageNumber] | test-writer-tdd | FALSE | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Test.[StageNumber] | tests-review-tdd | FALSE | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.Impl.[StageNumber] | implementation-tdd | FALSE | implementation-review | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.Impl.[StageNumber] | implementation-review | FALSE | fix-to-pr-response | implementation-tdd | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| EXECUTION.Response.[StageNumber] | fix-to-pr-response | FALSE | test-runner | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md, PullRequestComments.md | Stage-{StageNumber}/PullRequestResponses.md |
| REVIEW | test-runner | FALSE | response-merger | planner-tdd-soft | - | TestResults.md |
| REVIEW | response-merger | TRUE | pull-request-comment-interface(post) | - | PullRequestComments.md, Stage-*/PullRequestResponses.md | PullRequestResponses.md |
| COMPLETION | pull-request-comment-interface(post) | TRUE | COMPLETE | - | PullRequestResponses.md | PullRequestResponses.md |

**Execution Groups:**

| Approach | Groups |
|----------|--------|
| TDD | Test, Impl, Response |
| Implementation-First | Impl, Test, Response |
| Implementation-Only | Impl, Response |
| Tests-Only | Test |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). Subagent sequence per stage determined by the `Approach` column in the stage table.

**Notes:**
- **Requirements.md is user-created** — PR ID, branches, optionally minimal scope hints. requirements-refinement enriches it with comment triage and test strategy
- **fix-to-pr-response matches fixes to comments by file/line proximity and semantic similarity** — does not depend on planner referencing thread IDs
- **implementation-review may route back to planner or research** based on finding type
- **Two HITL gates at the end** — response-merger (content), pull-request-comment-interface(post) (timing)

</Workflow>

---

## Design Rationale

The inverse of `brownfield-pr-audit`: where that workflow reads code and produces PR comments, this one reads PR comments and fixes code, then replies to say what was fixed.

Requirements-refinement was added because PR comments are not inherently actionable requirements — they vary from nitpicks to architectural rethinks to questions that aren't fix requests at all. The user needs a gate to triage which comments become work items and to set the test strategy (regression tests for everything, complex fixes only, or none). Without this, the planner would treat every unresolved comment as an equal-priority fix request and guess at the test approach. Requirements-review follows because the enriched Requirements.md is the document the entire downstream chain depends on — if refinement produced something ambiguous or contradictory, better to catch it before planning than after.

The core design tension was where to generate resolution comments — once after all stages (simpler, one agent) or per-stage with a merger (more complex, two agents). Per-stage won because: (1) resolution comments should be grounded in actual code changes, not plan progress summaries; (2) a post-hoc agent reading all stages at once doesn't scale to large PRs; (3) per-stage generation keeps context fresh — the agent sees the diff while it's small and specific.

The planner does not need special instructions to reference PR comment thread IDs in stage plans. `fix-to-pr-response` handles the comment-to-fix mapping independently using file/line proximity from git diffs plus semantic matching against the original comment content. If the planner happens to include thread references (it reads PullRequestComments.md as input, so it might), that's a bonus signal, not a requirement.

test-runner findings route to the planner, not to implementation-tdd. This was a lesson learned from brownfield-tdd v3.7: test failures after all stages complete may indicate plan-level issues (missed dependencies, incorrect sequencing, insufficient test coverage decisions) that only the planner can resolve by restructuring stages.

Two HITL gates at the end serve different purposes: response-merger HITL lets the user review what will be said (content), pull-request-comment-interface(post) HITL lets the user control when it's said (timing synchronization with their own code push).

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 0.1 | 2026-08-20 | MOSAIC | Initial version |
| 0.2 | 2026-08-26 | MOSAIC | Replace Unicode emoji with ASCII tokens in HITL column (TRUE/FALSE). |

---

## Open Ideas / Dead Ends

Capture ideas that were explored but not adopted, and future improvements worth considering. This prevents the same dead ends from being revisited unknowingly.

**Ideas under consideration:**
- **Reuse audit-response-merger instead of new response-merger.** audit-response-merger has the right mechanical pattern (script-driven merge of partial response queues) but is audit-specific — expects TransformReport files, uses file+line overlap dedup for audit findings. The fix-response merge is simpler (thread-ID dedup). If the two mergers converge over time, consider generalizing into one.

**Dead ends (tried and rejected):**
- **Single post-hoc response agent (Option B).** Reading all Stage-*/PlanProgress.md after execution to generate replies in one pass. Rejected: plan progress is too summarized — agents may diverge from the plan for good reasons, and resolution comments need to reflect actual code changes, not checkbox summaries. Also doesn't scale to large PRs.
- **Planner injection for comment thread references.** Considered requiring the planner to include PR comment thread IDs in stage plans via a deploy-time injection. Rejected: the injection only makes sense for this workflow (not a project convention), and `fix-to-pr-response` can independently match fixes to comments via file/line proximity, making the planner dependency unnecessary.
