# Orchestration Workflow Templates

> **Status:** Template Library  
> **Version:** 6.9  
> **Last Updated:** 2026-04-15

This file contains pre-defined workflow templates for injection into concrete Orchestrators via `[INJECTION: available_workflows]`.

---

## How to Use

1. **Generic Orchestrator:** No workflows pre-loaded. Users describe their workflow.
2. **Concrete Orchestrator:** Copy relevant workflow(s) to `[INJECTION: available_workflows]` section.
3. **User Selection:** User selects by name (e.g., "Execute Full TDD workflow").

---

## Workflow Table Format

Each workflow uses a compact table with these columns:

| Column | Description |
|--------|-------------|
| **Phase** | Workflow phase (RESEARCH, ARCHITECTURE, PLANNING, DESIGN, EXECUTION, REVIEW). Use `.[StageNumber]` suffix for EXECUTION stages. |
| **Subagent** | The subagent to invoke |
| **HITL** | Human-in-the-loop: ✅ = subagent contacts user for review/approval, ❌ = fully autonomous |
| **On Success** | Next subagent(s) to invoke, or COMPLETE to end workflow. Comma-separated for parallel fork. |
| **On Findings** | Where to route COMPLETED_NEEDS_ACTION (subagent that needs to fix issues) |
| **Waits For** | *(Optional column — only in parallel workflows)* Subagents that must ALL complete before this subagent starts |
| **Input** | Orchestration artifacts the subagent reads (maps to `input_artifacts` in task message) |
| **Output** | Orchestration artifacts the subagent writes (maps to `output_artifacts` in task message) |

### Sequential vs Parallel Workflows

**Sequential workflows** use the standard 7-column format (no Waits For column). On Success always lists a single next subagent — the orchestrator follows a linear chain.

**Parallel workflows** add the **Waits For** column (8 columns total) and support these patterns:

| Pattern | How Expressed | Example |
|---------|---------------|---------|
| **Fork** (one → many parallel) | Comma-separated targets in On Success | `codebase-research(arch), planner-audit` |
| **Join** (many → one, barrier) | List predecessors in Waits For | `architecture-audit, contracts-audit` |
| **Staged dispatch** | Asterisk `*` suffix on subagent name | `tests-audit*, implementation-audit*` |

**Fork:** When On Success lists multiple subagents separated by commas, the orchestrator invokes all of them in parallel.

**Join/Barrier:** A subagent with a Waits For list is not invoked until ALL listed predecessors have completed. Multiple predecessors may route to the same target via their On Success — the Waits For on the target determines when it actually starts.

**Staged dispatch (`*`):** The asterisk suffix means "dispatch per-stage based on the progress artifact (AuditProgress.md, KBProgress.md, or Plan.md)." Each stage runs the appropriate typed agent independently, all in parallel. In Waits For, `agent*` means "wait for all staged instances of that agent to complete."

**Artifact naming convention:**
- **CamelCase** = Primary deliverables (Requirements.md, SystemDesign.md, ContractsDesign.md) — what humans read
- **kebab-case** = Review/validation artifacts named after producing subagent (requirements-review.md, plan-review.md) — quality gate feedback
- **Reserved keywords** (Plan, Progress, Requirements, Research, Review, Audit, Verification) carry semantic meaning — see `Development/Designs/OrchestrationSemantics.md`

---

## Greenfield TDD Workflow

> **Version:** 3.3

**Use when:** Building a **new project from scratch** requiring system architecture, test-first development, and full design.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | system-designer | requirements-refinement | Requirements.md | requirements-review.md |
| ARCHITECTURE | system-designer | ✅ | system-design-review | - | Requirements.md | SystemDesign.md |
| ARCHITECTURE | system-design-review | ❌ | planner-tdd-soft | system-designer | Requirements.md, SystemDesign.md | system-design-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Requirements.md, SystemDesign.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Requirements.md, Plan.md, Stage-*/Plan.md, SystemDesign.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.[StageNumber] | test-writer-tdd | ❌ | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). Subagent sequence per stage determined by the `Approach` column in the stage table:
- `TDD` — test-writer-tdd → tests-review-tdd → implementation-tdd → implementation-review
- `Implementation-First` — implementation-tdd → implementation-review → test-writer-tdd → tests-review-tdd
- `Implementation-Only` — implementation-tdd → implementation-review (no test agents)
- `Tests-Only` — test-writer-tdd → tests-review-tdd (no implementation agents)

**Notes:**
- **Greenfield** = no existing codebase, architecture created from scratch
- If system-design-review finds requirements issues → system-designer evaluates and may loop to requirements-refinement

---

## Brownfield TDD Workflow

> **Version:** 3.4

**Use when:** New features or significant changes to an **existing codebase** requiring test-first development with full research and design.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ❌ | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.[StageNumber] | test-writer-tdd | ❌ | tests-review-tdd | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). Subagent sequence per stage determined by the `Approach` column in the stage table:
- `TDD` — test-writer-tdd → tests-review-tdd → implementation-tdd → implementation-review
- `Implementation-First` — implementation-tdd → implementation-review → test-writer-tdd → tests-review-tdd
- `Implementation-Only` — implementation-tdd → implementation-review (no test agents)
- `Tests-Only` — test-writer-tdd → tests-review-tdd (no implementation agents)

**Notes:**
- **Brownfield** = existing codebase with patterns to discover and follow
- contracts-designer + contracts-review are optional - skip both if no new contracts are needed
- implementation-review may identify other issues than code itself → callback to codebase-research, planner-tdd-soft, contracts-designer

---

## Quick Fix Workflow

> **Version:** 3.0

**Use when:** Small changes, bug fixes, or well-understood modifications. Skips research and design.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | - | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | implementation-tdd | planner-tdd-soft | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | test-runner | - | Stage-{StageNumber}/Plan.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |

**Notes:**
- Single-stage plans use Stage-1/ folder for consistency (Decision 15)

---

## Brownfield Research Only Workflow

> **Version:** 2.1

**Use when:** Exploration, feasibility studies, or codebase analysis for an **existing codebase** without implementation.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ✅ | COMPLETE | - | - | Research.md |

**Notes:**
- **Brownfield** = existing codebase to analyze

---

## Brownfield Design Review Workflow

> **Version:** 3.2

**Use when:** Architecture review, design proposals, or planning large features for an **existing codebase** without implementation.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ❌ | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | COMPLETE | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |

**Notes:**
- **Brownfield** = existing codebase with patterns to follow
- contracts-designer + contracts-review are optional - skip both if no new/modified contracts are needed
- Enable HITL on contracts-designer/contracts-review if user review is required

---

## Implementation Only Workflow

> **Version:** 3.1

**Use when:** Research, planning, and design already complete. Direct implementation from existing artifacts.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | implementation-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | test-runner | implementation-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/implementation-review.md |
| REVIEW | test-runner | ❌ | COMPLETE | implementation-tdd | - | TestResults.md |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md): implementation-tdd → implementation-review. This workflow has a fixed subagent sequence — the Approach column is not used.

**Prerequisites:** ContractsDesign.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md must exist

---

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

---

## Brownfield System Audit Workflow

> **Version:** 1.0

**Use when:** High-level quality assessment of an **existing codebase** or major subsystem — architecture and contracts audit without file-level analysis. Use to identify problem areas before deeper per-component audits.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| RESEARCH | requirements-refinement | ✅ | codebase-research | - | - | Requirements.md | Requirements.md |
| RESEARCH | codebase-research | ❌ | codebase-research(architecture) | - | - | Requirements.md | Research.md |
| RESEARCH | codebase-research(architecture) | ❌ | codebase-research(contracts), architecture-audit | - | - | Requirements.md, Research.md | ResearchArchitecture.md |
| RESEARCH | codebase-research(contracts) | ❌ | contracts-audit | - | - | Requirements.md, Research.md, ResearchArchitecture.md | ResearchContracts.md |
| EXECUTION | architecture-audit | ❌ | COMPLETE | - | - | Requirements.md, Research.md, ResearchArchitecture.md | ArchitectureAudit.md |
| EXECUTION | contracts-audit | ❌ | COMPLETE | - | - | Requirements.md, Research.md, ResearchContracts.md | ContractsAudit.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format above).

**Multi-pass research:** `codebase-research(focus)` invokes the same `codebase-research` subagent with a focused task description. The parenthetical suffix disambiguates rows in the workflow table.

**Notes:**
- **Requirements.md is user-created** — must contain audit scope (subsystem, codebase area, or full system) and focus areas
- **Audit completion = success** — findings are data, not failure states; all On Findings are `-`
- Output (ArchitectureAudit.md, ContractsAudit.md) can guide follow-up per-component PR Audit workflows
- Workflow completes when all dispatched subagents have finished (both `architecture-audit` and `contracts-audit` return COMPLETE)

---

## Knowledge Base Generation Workflow

> **Version:** 0.5

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

---

## Knowledge Verification (Human) Workflow

> **Version:** 0.4

**Use when:** Verify knowledge quality using **architect-provided challenge questions**. Tests whether an agent can answer expert questions using available knowledge sources + codebase. Produces a diagnostic report — remediation is a separate concern.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| RESEARCH | verification-questions-preparer | ✅ | codebase-research* | - | - | - | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md |
| EXECUTION.[StageNumber] | codebase-research | ❌ | verification-answer-validator | - | - | VerificationQuestions.md, VerificationAttemptedAnswers.md | VerificationAttemptedAnswers.md |
| REVIEW | verification-answer-validator | ✅ | COMPLETE | - | codebase-research* | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md | VerificationReport.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format above).

**EXECUTION Stages:** One stage per question batch, all dispatched in parallel via `codebase-research*`. Stages tracked in VerificationAttemptedAnswers.md.

**Notes:**
- **Diagnostic only** — workflow ends at VerificationReport.md. To act on findings, run a separate remediation workflow (e.g., Knowledge Base Correction)
- **Knowledge-source agnostic** — verifies whether questions can be answered from whatever knowledge sources the codebase-research agent has access to (KB, docs, code comments, etc.)

---

## Knowledge Verification (Sampler) Workflow

> **Version:** 0.4

**Use when:** Verify knowledge quality using **automated random sampling**. A sampler agent explores the codebase, generates challenge questions about non-obvious details, then tests whether available knowledge sources guide an agent to the correct answers. Produces a diagnostic report — remediation is a separate concern.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| RESEARCH | verification-questions-preparer(create) | ❌ | codebase-question-sampler | - | - | - | VerificationQuestions.md, VerificationAnswers.md |
| RESEARCH | codebase-question-sampler | ❌ | verification-questions-preparer(validate) | - | - | VerificationQuestions.md, VerificationAnswers.md | VerificationQuestions.md, VerificationAnswers.md |
| RESEARCH | verification-questions-preparer(validate) | ❌ | codebase-research* | - | - | VerificationQuestions.md, VerificationAnswers.md | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md |
| EXECUTION.[StageNumber] | codebase-research | ❌ | verification-answer-validator | - | - | VerificationQuestions.md, VerificationAttemptedAnswers.md | VerificationAttemptedAnswers.md |
| REVIEW | verification-answer-validator | ❌ | COMPLETE | - | codebase-research* | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md | VerificationReport.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format above).

**EXECUTION Stages:** One stage per question batch, all dispatched in parallel via `codebase-research*`. Stages tracked in VerificationAttemptedAnswers.md.

**Notes:**
- **Q/A artifact lifecycle** — `(create)` creates empty artifacts → `codebase-question-sampler` populates → `(validate)` validates and creates batched `VerificationAttemptedAnswers.md`
- **Diagnostic only** — workflow ends at VerificationReport.md. To act on findings, run a separate remediation workflow (e.g., Knowledge Base Correction)

---

## Knowledge Base Correction Workflow

> **Version:** 0.1

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

---


## Knowledge Verification (Sampler + Human) Workflow

> **Version:** 1.0

**Use when:** Verify knowledge quality using **both** architect-provided challenge questions **and** automated random sampling. Gathers questions from both sources in parallel, tests whether an agent can answer them using available knowledge sources + codebase, and produces a unified diagnostic report. Remediation is a separate concern.

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| RESEARCH | verification-questions-preparer(create) | ❌ | verification-questions-preparer(human), codebase-question-sampler | - | - | - | VerificationQuestions.md, VerificationAnswers.md |
| RESEARCH | verification-questions-preparer(human) | ✅ | verification-questions-preparer(validate) | - | - | VerificationQuestions.md, VerificationAnswers.md | VerificationQuestions.md, VerificationAnswers.md |
| RESEARCH | codebase-question-sampler | ❌ | verification-questions-preparer(validate) | - | - | VerificationQuestions.md, VerificationAnswers.md | VerificationQuestions.md, VerificationAnswers.md |
| RESEARCH | verification-questions-preparer(validate) | ❌ | codebase-research* | - | verification-questions-preparer(human), codebase-question-sampler | VerificationQuestions.md, VerificationAnswers.md | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md |
| EXECUTION.[StageNumber] | codebase-research | ❌ | verification-answer-validator | - | - | VerificationQuestions.md, VerificationAttemptedAnswers.md | VerificationAttemptedAnswers.md |
| REVIEW | verification-answer-validator | ✅ | COMPLETE | - | codebase-research* | VerificationQuestions.md, VerificationAnswers.md, VerificationAttemptedAnswers.md | VerificationReport.md |

**Parallel execution:** This workflow uses the Waits For column for parallel dispatch (see Workflow Table Format in Workflows.md).

**RESEARCH parallel fork:** `verification-questions-preparer(create)` forks into two parallel tracks — `verification-questions-preparer(human)` (HITL, user provides Q/A pairs) and `codebase-question-sampler` (autonomous, explores codebase for implementation details). Both write to the same shared artifacts (VerificationQuestions.md, VerificationAnswers.md) with independent numbering. `verification-questions-preparer(validate)` is the join point — waits for both to complete before validating all pairs and creating batched VerificationAttemptedAnswers.md.

**EXECUTION Stages:** One stage per question batch, all dispatched in parallel via `codebase-research*`. Stages tracked in VerificationAttemptedAnswers.md.

**Notes:**
- **Diagnostic only** — workflow ends at VerificationReport.md. To act on findings, run a separate remediation workflow (e.g., Knowledge Base Correction)
- **Knowledge-source agnostic** — verifies whether questions can be answered from whatever knowledge sources the codebase-research agent has access to (KB, docs, code comments, etc.)
- **Q/A artifact lifecycle** — `(create)` creates empty artifacts → `(human)` and `codebase-question-sampler` populate in parallel → `(validate)` validates all pairs from both sources and creates batched VerificationAttemptedAnswers.md
- **Shared artifact concurrency** — both the human preparer and sampler append to the same VerificationQuestions.md and VerificationAnswers.md. The `Source` field (`user` vs `agent`) distinguishes origin. Both agents append with independent numbering — the validate step handles any numbering conflicts
- **Question sources are independent** — if one source produces no questions (e.g., user has no questions, or sampler finds the codebase too small), the workflow continues with whatever questions the other source produced

---

## HW Schema Knowledge Base Generation Workflow

> **Version:** 0.5

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

---

## Brownfield TDD Build-Verified Workflow

> **Version:** 2.0

**Use when:** New features or significant changes to an **existing codebase** requiring test-first development where **compilation/build cannot be verified via standard terminal tools** (e.g., PLC/SCL with proprietary toolchains, embedded systems, cross-compilation environments). Adds a dedicated build-and-deploy step between code writing and code review. Review agents execute tests on the target platform to verify TDD RED/GREEN phases.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | ❌ | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | ✅ | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | ❌ | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | ✅ | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | ❌ | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md | plan-review.md |
| DESIGN | contracts-designer | ✅ | contracts-review | - | Research.md, Requirements.md, Plan.md, Stage-*/Plan.md | ContractsDesign.md |
| DESIGN | contracts-review | ❌ | test-writer-tdd | contracts-designer | Plan.md, Stage-*/Plan.md, ContractsDesign.md | contracts-review.md |
| EXECUTION.[StageNumber] | test-writer-tdd | ❌ | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | build-review | ❌ | tests-review-tdd | test-writer-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-tests.md |
| EXECUTION.[StageNumber] | tests-review-tdd | ❌ | implementation-tdd | test-writer-tdd | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-tests.md | Stage-{StageNumber}/tests-review-tdd.md |
| EXECUTION.[StageNumber] | implementation-tdd | ❌ | build-review | - | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/PlanProgress.md |
| EXECUTION.[StageNumber] | build-review | ❌ | implementation-review | implementation-tdd | Stage-{StageNumber}/PlanProgress.md | Stage-{StageNumber}/build-review-impl.md |
| EXECUTION.[StageNumber] | implementation-review | ❌ | COMPLETE | implementation-tdd (or other based on issue) | Stage-{StageNumber}/Plan.md, ContractsDesign.md, Stage-{StageNumber}/PlanProgress.md, Stage-{StageNumber}/build-review-impl.md | Stage-{StageNumber}/implementation-review.md |

**EXECUTION Stages:** Loop per stage (stages defined in Plan.md). Subagent sequence per stage determined by the `Approach` column in the stage table:
- `TDD` — test-writer-tdd → build-review → tests-review-tdd → implementation-tdd → build-review → implementation-review
- `Implementation-First` — implementation-tdd → build-review → implementation-review → test-writer-tdd → build-review → tests-review-tdd
- `Implementation-Only` — implementation-tdd → build-review → implementation-review (no test agents)
- `Tests-Only` — test-writer-tdd → build-review → tests-review-tdd (no implementation agents)

**Notes:**
- **build-review** is a mechanical agent: imports source files into the build system, manages build dependencies, compiles/builds, deploys to target platform, and reports success/failure. On failure (`COMPLETED_NEEDS_ACTION`), routes back to the paired writer agent via On Findings.
- **build-review appears twice** per TDD stage — once after test writing (On Findings → test-writer-tdd), once after implementation (On Findings → implementation-tdd). Same agent, different On Findings targets per position. **Separate output artifacts** for context isolation: `build-review-tests.md` (test build) and `build-review-impl.md` (implementation build).
- **Review agents execute tests:** tests-review-tdd verifies TDD RED phase (tests fail because implementation is missing), implementation-review verifies TDD GREEN phase (tests pass after implementation). Each reads its respective build-review artifact for deployment metadata needed to trigger test execution on the target platform.
- **Brownfield** = existing codebase with patterns to discover and follow
- contracts-designer + contracts-review are optional — skip both if no new contracts are needed
- **When to use over standard Brownfield TDD:** Use this workflow when the build/compile toolchain is not accessible via standard terminal commands and requires specialized tool invocations (MCP servers, COM automation, proprietary IDEs, etc.)

---

## Appendix: Subagent Reference

| ID | Subagent | Role | Context |
|----|----------|------|---------|
| 1 | codebase-research | Gather context, analyze existing codebase patterns | Brownfield |
| 2 | library-research | Research libraries, frameworks, and external dependencies | Both |
| 4 | requirements-refinement | Transform raw requirements into complete specs through user dialogue | Both |
| 9 | requirements-review | Validate requirements completeness and consistency (quality gate) | Both |
| 5 | system-designer | Create high-level system architecture and structure | Greenfield |
| 10 | system-design-review | Review system design quality and completeness | Greenfield |
| 6 | planner-tdd-soft | Create implementation plan with stages — outputs Plan.md (brief routing artifact) + per-stage Stage-{N}/Plan.md and Stage-{N}/PlanProgress.md | Both |
| 7 | planner-audit | Create audit plan with typed stages (Implementation, Tests, Architecture, Contracts) — outputs AuditPlan.md (brief routing artifact) + per-stage Stage-{N}/AuditPlan.md and Stage-{N}/AuditProgress.md | Brownfield |
| 11 | plan-review | Review plan quality, validate against actual code | Both |
| 8 | contracts-designer | Create technical design/interfaces (contracts) | Both |
| 12 | contracts-review | Review design contracts, validate testability and patterns | Both |
| 15 | test-writer-tdd | Write, update, and fix tests (TDD RED phase primary; also handles test updates and fixes from review feedback) | Both |
| 13 | tests-review-tdd | Review test quality and coverage | Both |
| 16 | implementation-tdd | Implement code (TDD GREEN phase) | Both |
| 14 | implementation-review | Review implementation quality | Both |
| 17 | test-runner | Execute tests, report results | Both |
| 21 | contracts-audit | Audit existing interfaces/contracts for quality issues | Brownfield |
| 20 | architecture-audit | Audit existing system architecture for quality issues | Brownfield |
| 23 | tests-audit | Audit existing test quality — writes findings to per-stage artifact (Stage-{N}/TestsAudit.md) | Brownfield |
| 22 | implementation-audit | Audit existing code quality — writes findings to per-stage artifact (Stage-{N}/ImplementationAudit.md) | Brownfield |
| 32 | audit-review | Review audit findings for quality — validates evidence, filters false positives, checks severity ratings. Scopes review to the audit artifact's scope. Paired with auditor via On Findings routing | Brownfield |
| 19 | audit-to-pull-request | Transform a single audit artifact into condensed PR comments — hunk-level scope filtering, deduplicates against existing comments, writes partial PullRequestResponses and TransformReport | Brownfield |
| 33 | pr-requirements-analyzer | Analyze PR context — fetch changed file list and stats, summarize existing comment threads, confirm audit scope with user, enrich Requirements.md with PR metadata | Brownfield |
| 34 | audit-response-merger | Merge partial PullRequestResponses from parallel audit-to-pull-request instances — cross-audit deduplication, consolidated PullRequestResponses.md and AuditTransformReport.md | Brownfield |
| 18 | pull-request-comment-interface | Bridge PR comments with orchestration system | Both |
| 35 | build-review | Import sources into build system, compile/build, report success or errors back to paired writer agent | Both |
| 3 | knowledge-base-generator | Research codebase scope and produce/update KB documents at appropriate tier. Modes: generate (new docs + deeper-tier recommendations + correction flags), correct (apply organized flags, validating against codebase), update (apply corrections from external findings to existing KB documents) | Brownfield |
| 24 | knowledge-base-flag-sorter | Collect correction flags from KBFlags.md, organize bottom-up by target tier, and create correction stages in KBProgress.md — one stage per target KB document | Brownfield |
| 25 | knowledge-base-index-assembler | Create top-level Index.md from all completed KB documents | Brownfield |
| 27 | codebase-question-sampler | Deep-dive into codebase implementation to discover details and generate challenge Q&A pairs that test KB navigation quality | Brownfield |
| 28 | verification-answer-validator | Compare attempted answers to expected answers, judge match/mismatch/partial | Both |
| 26 | verification-questions-preparer | Create, populate (via HITL or autonomously), and validate Q/A verification artifacts. Owns the Q/A artifact format specification | Both |
| 29 | hw-schema-research | Analyze hardware schematics via structured tool queries — explore circuit topology, component relationships, and signal flow | HW Schematic |
| 30 | hw-schema-kb-generator | Research schematic sheets via structured tool queries and produce KB documentation describing sheet functions, signal topology, and cross-sheet relationships. Modes: generate (KB docs + deeper-tier recommendations + correction flags), correct (apply organized flags, validating against schematic) | HW Schematic |
| 31 | hw-schema-planner | Plan HW schematic research — query sheet inventory and create research stages. ⚠️ Not yet created | HW Schematic |

**HITL Guidelines:**
- HITL settings in workflow tables above are recommendations, not requirements
- Users can enable/disable HITL per-workflow or per-stage based on their needs
- Stage-level HITL field in the Plan artifact (e.g., Plan.md stage table, KBProgress.md for KB workflows) allows selective human oversight

---

## Appendix: Custom Workflow Template

Use this template to define new workflows:

**Sequential workflow (7 columns):**
```markdown
## {Workflow Name}

> **Version:** 1.0

**Use when:** {Description of when to use this workflow}

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| {PHASE} | {Subagent} | ❌/✅ | {Next} | {FixTarget or -} | {input artifacts or -} | {output artifact} |

**Notes:**
- {Any special routing rules or constraints}
```

**Parallel workflow (8 columns — add Waits For):**
```markdown
## {Workflow Name}

> **Version:** 1.0

**Use when:** {Description of when to use this workflow}

| Phase | Subagent | HITL | On Success | On Findings | Waits For | Input | Output |
|-------|----------|:----:|------------|-------------|-----------|-------|--------|
| {PHASE} | {Subagent} | ❌/✅ | {Next or A, B} | {FixTarget or -} | {predecessors or -} | {input artifacts or -} | {output artifact} |

**Notes:**
- {Fork/join points, parallel tracks, staged dispatch}
```

---

*End of Document*
