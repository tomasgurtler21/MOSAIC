# Legacy Appendices (Provisional Holding File)

> **PROVISIONAL — Do not treat as authoritative.**
> This file is a mechanical verbatim relocation of shared appendix content from the retired
> `Agents/Generic/Orchestrator/Workflows.md` monolith. No content has been rewritten.
> The permanent home and Gen-2-accurate rewrite of this content is deferred to Roadmap Phase 9.4.
> Until then, this file exists solely so the old monolith can be retired without losing the content.

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
