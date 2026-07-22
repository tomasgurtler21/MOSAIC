# GHCP CLI Agents

This folder contains 32 generic agent templates for the GHCP CLI multi-agent orchestration system.

## Agent Index

| ID | Agent | Version | Based On | Description |
|----|-------|---------|----------|-------------|
| 1 | [codebase-research](codebase-research.agent.md) | 2.5.0 | 2.5.0 | Analyzes codebase, explores existing patterns, and documents findings to build foundational understanding for downstream agents |
| 2 | [library-research](library-research.agent.md) | 1.1.2 | 1.1.2 | Researches external libraries, APIs, and documentation to provide comprehensive reference information |
| 3 | [knowledge-base-generator](knowledge-base-generator.agent.md) | 1.4.0 | 1.4.0 | Researches codebase scope and produces N-tier knowledge base documentation optimized for KB consumer navigation |
| 4 | [requirements-refinement](requirements-refinement.agent.md) | 2.3.0 | 2.3.0 | Transforms raw or incomplete requirements into complete, crystal-clear specifications through collaborative user dialogue |
| 5 | [system-designer](system-designer.agent.md) | 2.1.1 | 2.1.1 | Creates high-level system architecture for greenfield projects - defining components, layers, structure, and technology recommendations |
| 6 | [planner-tdd-soft](planner-tdd-soft.agent.md) | 5.4.0 | 5.4.0 | Creates implementation plans with per-stage context isolation following TDD principles |
| 7 | [planner-audit](planner-audit.agent.md) | 2.0.0 | 2.0.0 | Creates audit plans splitting changed files into typed stages for iterative auditing |
| 8 | [contracts-designer](contracts-designer.agent.md) | 2.3.0 | 2.3.0 | Creates technical designs defining interfaces, contracts, data structures, and architectural decisions |
| 9 | [requirements-review](requirements-review.agent.md) | 2.1.1 | 2.1.1 | Reviews requirements completeness, identifies gaps, and ensures sufficient information exists for planning |
| 10 | [system-design-review](system-design-review.agent.md) | 2.1.1 | 2.1.1 | Reviews system design quality for greenfield projects - ensuring architecture is complete, consistent, and implementable |
| 11 | [plan-review](plan-review.agent.md) | 3.3.0 | 3.3.0 | Reviews plan quality, task sizing, dependency correctness, and validates TDD decisions against actual codebase |
| 12 | [contracts-review](contracts-review.agent.md) | 2.2.0 | 2.2.0 | Reviews technical design quality — ensuring interfaces, contracts, and data structures are complete and consistent |
| 13 | [tests-review-tdd](tests-review-tdd.agent.md) | 2.4.0 | 2.4.0 | Reviews test quality, coverage, and TDD RED phase correctness |
| 14 | [implementation-review](implementation-review.agent.md) | 2.3.0 | 2.3.0 | Reviews implementation quality, design compliance, and code standards |
| 15 | [test-writer-tdd](test-writer-tdd.agent.md) | 3.2.0 | 3.2.0 | Writes, updates, and fixes test code — creates failing tests from design specifications (TDD RED phase) |
| 16 | [implementation-tdd](implementation-tdd.agent.md) | 3.3.0 | 3.3.0 | Implements and updates production code to satisfy tests and design specifications |
| 17 | [test-runner](test-runner.agent.md) | 2.2.0 | 2.2.0 | Executes tests and reports results - providing clear pass/fail outcomes and failure diagnostics |
| 18 | [pull-request-comment-interface](pull-request-comment-interface.agent.md) | 1.2.2 | 1.2.2 | Bridges pull request comments with the multi-agent orchestration system |
| 19 | [audit-to-pull-request](audit-to-pull-request.agent.md) | 2.0.0 | 2.0.0 | Transforms verbose audit artifacts into condensed PR-ready comments |
| 20 | [architecture-audit](architecture-audit.agent.md) | 1.2.0 | 1.2.0 | Audits existing system architecture for quality issues — evaluating layers, dependencies, and component boundaries |
| 21 | [contracts-audit](contracts-audit.agent.md) | 1.2.0 | 1.2.0 | Audits existing interfaces, contracts, and data structures for quality issues |
| 22 | [implementation-audit](implementation-audit.agent.md) | 2.0.0 | 2.0.0 | Audits existing code quality — evaluating readability, correctness, security, and maintainability |
| 23 | [tests-audit](tests-audit.agent.md) | 2.0.0 | 2.0.0 | Audits existing test quality — evaluating coverage, clarity, determinism, and edge case handling |
| 24 | [knowledge-base-flag-sorter](knowledge-base-flag-sorter.agent.md) | 1.2.0 | 1.2.0 | Collects correction flags, organizes them bottom-up by target tier, and creates correction stages |
| 25 | [knowledge-base-index-assembler](knowledge-base-index-assembler.agent.md) | 1.2.0 | 1.2.0 | Creates the top-level Index.md in the KB output path from all completed KB documents |
| 26 | [verification-questions-preparer](verification-questions-preparer.agent.md) | 1.2.0 | 1.2.0 | Creates, populates, and validates Q/A verification artifacts |
| 27 | [codebase-question-sampler](codebase-question-sampler.agent.md) | 1.1.0 | 1.1.0 | Deep-dives into codebase implementation to discover details and generates challenge Q/A pairs |
| 28 | [verification-answer-validator](verification-answer-validator.agent.md) | 1.2.0 | 1.2.0 | Compares attempted answers to expected answers and produces a verification report |
| 29 | [hw-schema-research](hw-schema-research.agent.md) | 1.2.0 | 1.2.0 | Analyzes hardware schematics via structured tool queries and documents findings for downstream agents |
| 30 | [hw-schema-kb-generator](hw-schema-kb-generator.agent.md) | 1.3.0 | 1.3.0 | Synthesizes domain-oriented KB documentation from per-sheet research artifacts and direct hw-schema tool queries |
| 31 | [hw-schema-planner](hw-schema-planner.agent.md) | 1.1.0 | 1.1.0 | Plans HW schematic research by discovering all sheets and creating HWResearchProgress.md |
| 32 | [audit-review](audit-review.agent.md) | 1.0.0 | 1.0.0 | Reviews audit findings for quality — verifying evidence accuracy, detecting false positives, validating severity ratings, and ensuring recommendations are actionable |

## Agent Categories

### Research
- **codebase-research** - Gathers context from the codebase
- **library-research** - Researches external libraries and APIs
- **knowledge-base-generator** - Produces N-tier knowledge base documentation

### Planning
- **system-designer** - Designs system architecture
- **planner-tdd-soft** - Creates TDD implementation plans
- **planner-audit** - Creates audit plans splitting changed files into typed stages
- **contracts-designer** - Designs interfaces and contracts
- **knowledge-base-flag-sorter** - Organizes KB correction flags for correction pass

### Validation (Review)
- **requirements-review** - Validates requirements
- **plan-review** - Validates plans
- **contracts-review** - Validates contracts
- **system-design-review** - Validates system design
- **tests-review-tdd** - Validates TDD tests
- **implementation-review** - Validates implementation

### Audit
- **architecture-audit** - Audits existing system architecture
- **contracts-audit** - Audits existing interfaces and contracts
- **implementation-audit** - Audits existing code quality
- **tests-audit** - Audits existing test quality
- **audit-review** - Reviews audit findings for quality and accuracy
- **audit-to-pull-request** - Transforms audit findings into PR-ready comments

### Creation
- **test-writer-tdd** - Writes, updates, and fixes tests (TDD)
- **implementation-tdd** - Creates implementation (TDD)
- **requirements-refinement** - Refines and clarifies requirements
- **knowledge-base-index-assembler** - Assembles the top-level KB index

### Execution
- **test-runner** - Runs tests and reports results

### Integration
- **pull-request-comment-interface** - Bridges PR comments with the orchestration system

### Hardware Schema
- **hw-schema-research** - Analyzes hardware schematics
- **hw-schema-kb-generator** - Synthesizes KB documentation from schematic research
- **hw-schema-planner** - Plans HW schematic research

### Verification
- **verification-questions-preparer** - Creates and validates Q/A verification artifacts
- **codebase-question-sampler** - Generates challenge Q/A pairs from codebase deep-dive
- **verification-answer-validator** - Validates answers against expected results

## Usage

All agents use `[INJECTION: ...]` placeholders that are populated at runtime by the orchestrator with:
- Language-specific patterns
- Codebase context
- Custom constraints
- Output artifact templates
- Identity extensions
- Protocol extensions
- Error handling extensions
- Context limits

See [QuickReference.md](../QuickReference.md) for orchestration details.
