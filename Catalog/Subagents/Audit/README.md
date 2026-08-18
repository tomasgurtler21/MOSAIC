# Audit

Code quality analysis agents for existing codebases.

## Purpose

Audit agents perform deep, evidence-based analysis of existing code — evaluating quality, structure, and correctness across architecture, contracts, implementation, and tests. They produce verbose findings with location, evidence, and recommendations. They do not fix or remediate issues.

## Agents

| ID | Agent | Version | Description |
|----|-------|---------|-------------|
| 20 | [architecture-audit](./architecture-audit.md) | 1.1.0 | Audits existing system architecture for quality issues — layers, dependencies, component boundaries, and pattern adherence |
| 21 | [contracts-audit](./contracts-audit.md) | 1.1.0 | Audits existing interfaces, contracts, and data structures for quality issues with verbose findings |
| 22 | [implementation-audit](./implementation-audit.md) | 1.1.0 | Audits existing code quality — readability, correctness, security, and maintainability with verbose findings |
| 23 | [tests-audit](./tests-audit.md) | 1.1.0 | Audits existing test quality — coverage, clarity, determinism, and edge case handling with verbose findings |

## Audit vs Validation Distinction

| Aspect | Audit | Validation |
|--------|-------|-----------|
| **Subject** | Existing code in the codebase | Artifacts and proposals produced in current workflow |
| **Focus** | Accumulated quality issues and technical debt | Correctness and completeness of new work |
| **Output** | Verbose findings with evidence | Pass/fail with gaps identified |
| **When used** | Codebase quality analysis workflows | After each creation step in TDD workflows |
| **Example** | "UserService has 12 responsibilities — god class violation" | "Plan stage 2 is missing test tasks for the new parser" |

## What Audit Agents Do

- Read actual codebase files to assess existing quality
- Produce verbose, evidence-based findings (location, evidence, explanation, recommendation, impact)
- Evaluate quality across their domain (architecture, contracts, implementation, or tests)
- Support iterative multi-invocation audits — each invocation handles a subset of files from AuditProgress.md

## What Audit Agents Do NOT Do

- Fix or remediate identified issues — output is analysis only
- Validate proposals against requirements (that's Validation)
- Plan implementation work (that's Planning)
- Write or modify code (that's Creation)

## Audit Workflow

Audit agents are typically orchestrated by:
1. **PlannerAudit** — splits changed/relevant files into typed stages (Implementation or Tests)
2. **ImplementationAudit / TestsAudit / etc.** — each processes one stage from AuditProgress.md

Each audit agent invocation handles exactly one stage from the AuditPlan.md — enabling parallel execution across stages.
