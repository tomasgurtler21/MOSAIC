---
version: "3.1"
name: "Brownfield PR Audit Workflow"
description: "Audit quality of existing code for PR review — multi-pass research, plan-driven staged audits with 4 types, per-audit PR comment transform with cross-audit deduplication, and post."
hint: "PR audit with multi-pass research, parallel audit tracks, and PR comment integration"
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
