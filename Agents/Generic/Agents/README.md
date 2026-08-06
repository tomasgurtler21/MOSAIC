# Agents

Agent definitions organized by function.

## Functions

| Function | Purpose | Agents |
|----------|---------|--------|
| [Research](./Research/) | Information gathering and exploration | codebase-research, knowledge-base-generator, codebase-question-sampler, library-research, hw-schema-research, hw-schema-kb-generator, document-research |
| [Validation](./Validation/) | Quality gates, verification, and review | requirements-review, contracts-review, plan-review, system-design-review, implementation-review, tests-review-tdd, verification-answer-validator, test-scenario-review, test-case-review, requirements-testability-review |
| [Planning](./Planning/) | Work breakdown, sequencing, and architecture | planner-tdd-soft, planner-audit, contracts-designer, system-designer, requirements-refinement, pr-requirements-analyzer, knowledge-base-flag-sorter, verification-questions-preparer, hw-schema-planner, test-scenario-designer |
| [Audit](./Audit/) | Evidence-based analysis of existing codebase quality | architecture-audit, contracts-audit, implementation-audit, tests-audit, audit-review |
| [Creation](./Creation/) | Building project files (code and tests) | implementation-tdd, test-writer-tdd, knowledge-base-index-assembler, test-case-writer |
| [Execution](./Execution/) | Running tests and tools | test-runner |
| [Interface](./Interface/) | Bidirectional bridges with external systems | audit-to-pull-request, audit-response-merger, pull-request-comment-interface, test-case-export, approval-presenter |
| [Infrastructure](./Infrastructure/) | Orchestration support fired by triggers, not by routing | checkpoint-manager-git, checkpoint-restore-git, commit-manager-git, orchestration-review |
| [MosaicTest](./MosaicTest/) | Deterministic stubs used as fixtures by the `mosaic-run` harness conformance suite — they do no work, and exist so end-to-end runs measure the harness rather than a model | mosaictest-scripted, mosaictest-checkpoint, mosaictest-review |

## Agent Summary

| ID | Agent | Function | Version | Tier | Description |
|----|-------|----------|---------|:----:|-------------|
| 1 | codebase-research | Research | 2.4.0 | MEDIUM-HIGH | Analyzes codebase, explores patterns, documents findings |
| 3 | knowledge-base-generator | Research | 1.3.0 | MEDIUM-HIGH | Researches codebase scope and produces N-tier knowledge base documentation |
| 2 | library-research | Research | 1.0.2 | MEDIUM | Researches libraries, frameworks, and external dependencies |
| 27 | codebase-question-sampler | Research | 1.0.0 | MEDIUM | Deep-dives into codebase implementation to discover details and generate challenge Q/A pairs |
| 29 | hw-schema-research | Research | 1.1.0 | MEDIUM | Analyzes hardware schematics via structured tool queries, explores circuit topology and component relationships |
| 30 | hw-schema-kb-generator | Research | 1.2.0 | MEDIUM-HIGH | Synthesizes domain-oriented KB documentation from per-sheet research artifacts and direct hw-schema tool queries |
| 43 | document-research | Research | 1.0.0 | HIGH | Resolves a requirement to dependency closure from large external specification documents via runtime retrieval, with source locators |
| 9 | requirements-review | Validation | 2.0.1 | MEDIUM | Reviews requirements completeness and consistency |
| 12 | contracts-review | Validation | 2.1.0 | MEDIUM | Reviews contracts/design specifications for correctness |
| 11 | plan-review | Validation | 3.1.0 | MEDIUM | Reviews implementation plans for feasibility |
| 10 | system-design-review | Validation | 2.0.1 | MEDIUM | Reviews system design for architecture quality |
| 14 | implementation-review | Validation | 2.2.0 | MEDIUM | Reviews code quality and design compliance |
| 13 | tests-review-tdd | Validation | 2.3.0 | MEDIUM | Reviews test quality and TDD compliance |
| 28 | verification-answer-validator | Validation | 1.1.0 | LOW-MEDIUM | Compares attempted answers to expected answers, judges match/mismatch/partial |
| 45 | test-scenario-review | Validation | 1.0.0 | MEDIUM | Reviews a test scenario space for coverage completeness, justified exclusions, and traceability to requirements |
| 47 | test-case-review | Validation | 1.0.0 | MEDIUM | Reviews abstract test cases for format conformance, faithfulness to the scenario model, and end-to-end traceability |
| 49 | requirements-testability-review | Validation | 1.0.0 | MEDIUM-HIGH | Judges whether a resolved requirement is testable and its dossier sufficient for scenario derivation — specialised alternative to requirements-review |
| 6 | planner-tdd-soft | Planning | 4.1.0 | HIGH | Creates implementation plans with task breakdown |
| 7 | planner-audit | Planning | 3.0.0 | MEDIUM | Creates audit plans with 4 typed stages (Implementation, Tests, Architecture, Contracts) — splits files into stages for iterative auditing |
| 8 | contracts-designer | Planning | 2.1.0 | HIGH | Defines interfaces, contracts, and data structures |
| 5 | system-designer | Planning | 2.0.1 | HIGH | Designs system architecture and component interactions |
| 4 | requirements-refinement | Planning | 2.1.0 | MEDIUM-HIGH | Refines and clarifies requirements based on research |
| 24 | knowledge-base-flag-sorter | Planning | 1.1.0 | LOW-MEDIUM | Collects correction flags, organizes bottom-up by tier, creates correction stages |
| 26 | verification-questions-preparer | Planning | 1.1.0 | MEDIUM | Creates, populates (via HITL or autonomously), and validates Q/A verification artifacts |
| 31 | hw-schema-planner | Planning | 1.0.0 | MEDIUM | Plans HW schematic research by discovering sheets and creating HWResearchProgress.md |
| 33 | pr-requirements-analyzer | Planning | 1.0.0 | MEDIUM | Analyzes PR context — fetches git metadata, summarizes existing comment threads, enriches Requirements.md with confirmed scope |
| 44 | test-scenario-designer | Planning | 1.0.0 | HIGH | Enumerates the test scenario space a requirement implies, with dimensions, values, and justified exclusions |
| 20 | architecture-audit | Audit | 1.1.0 | MEDIUM-HIGH | Audits existing system architecture for quality issues |
| 21 | contracts-audit | Audit | 1.1.0 | MEDIUM-HIGH | Audits existing interfaces and contracts for quality issues |
| 22 | implementation-audit | Audit | 1.1.0 | MEDIUM | Audits existing code quality — readability, correctness, security, maintainability |
| 23 | tests-audit | Audit | 1.1.0 | MEDIUM | Audits existing test quality — coverage, clarity, determinism, edge cases |
| 32 | audit-review | Audit | 1.0.0 | MEDIUM | Reviews audit findings for quality — validates evidence, filters false positives, checks severity |
| 16 | implementation-tdd | Creation | 3.1.0 | MEDIUM | Writes implementation code (TDD GREEN phase) |
| 15 | test-writer-tdd | Creation | 3.0.0 | MEDIUM | Writes, updates, and fixes test code (TDD RED phase and beyond) |
| 25 | knowledge-base-index-assembler | Creation | 1.1.0 | LOW | Creates top-level Index.md in the KB output path from completed KB documents |
| 46 | test-case-writer | Creation | 1.0.0 | MEDIUM-HIGH | Renders an approved scenario space into abstract test cases conforming to a project-defined format |
| 17 | test-runner | Execution | 2.1.0 | MEDIUM | Executes tests and reports results |
| 19 | audit-to-pull-request | Interface | 3.0.0 | HIGH | Transforms a single audit artifact into PR-ready comments with context zone intelligence |
| 34 | audit-response-merger | Interface | 1.0.0 | MEDIUM | Merges partial PR response queues from parallel audit-to-pull-request instances with cross-audit deduplication |
| 18 | pull-request-comment-interface | Interface | 1.1.2 | LOW | Bridges PR comments with orchestration system |
| 48 | test-case-export | Interface | 1.0.0 | LOW-MEDIUM | Transforms approved abstract test cases into a target test management system's import format |
| 50 | approval-presenter | Interface | 1.0.1 | LOW-MEDIUM | Presents a converged artifact for human approval and stamps human_approved on it; performs no analysis |
| 36 | checkpoint-manager-git | Infrastructure | 1.0.0 | LOW | Commits a restorable checkpoint of the working tree to a private git ref namespace |
| 37 | checkpoint-restore-git | Infrastructure | 1.0.0 | MEDIUM | Restores the working tree to a checkpoint and reconciles the branch with committed work |
| 38 | commit-manager-git | Infrastructure | 1.0.0 | LOW | Commits completed stage work to the user's branch with a plan-derived message |
| 39 | orchestration-review | Infrastructure | 1.0.0 | LOW | Advisory — checks a run's bookkeeping and routing against its declared workflow |
| 40 | mosaictest-scripted | MosaicTest | 1.0.0 | LOW | Harness test fixture — reads the script fixture bound to its row and returns exactly the response it specifies |
| 41 | mosaictest-checkpoint | MosaicTest | 1.0.0 | LOW | Harness test fixture — `checkpoint`-class stub returning a fake `[checkpoint:{sha}]` marker; performs no git operations |
| 42 | mosaictest-review | MosaicTest | 1.0.0 | LOW | Harness test fixture — `review`-class stub returning SUCCESS on an interval trigger; inspects nothing |

See [ModelSelectionGuide.md](../../../Documentation/ModelSelectionGuide.md) for tier definitions and model recommendations.

## Design Reference

See [AgentFolderReorganization.md](../../../Development/Designs/AgentFolderReorganization.md) for decisions and rationale behind this structure.
