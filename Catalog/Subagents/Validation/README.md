# Validation

Quality gates, verification, and review agents.

## Purpose

Validation agents are gatekeepers that ensure quality before proceeding to the next phase. They review, validate, and identify issues - they do not fix or implement solutions.

## Agents

| ID | Agent | Version | Description |
|----|-------|---------|-------------|
| 9 | [requirements-review](./requirements-review.md) | 2.0.1 | Reviews requirements completeness, identifies gaps, ensures sufficient information for planning |
| 12 | [contracts-review](./contracts-review.md) | 2.1.0 | Reviews contracts and design specifications for correctness and completeness |
| 11 | [plan-review](./plan-review.md) | 3.1.0 | Reviews implementation plans for feasibility, completeness, and alignment |
| 10 | [system-design-review](./system-design-review.md) | 2.0.1 | Reviews system design for architecture quality and design principles |
| 14 | [implementation-review](./implementation-review.md) | 2.2.0 | Reviews code quality, design compliance, and code standards |
| 13 | [tests-review-tdd](./tests-review-tdd.md) | 2.3.0 | Reviews test quality, coverage, and TDD RED phase correctness |

## What Validation Agents Do

- Evaluate completeness and consistency
- Detect contradictions and conflicts
- Verify alignment with specifications and designs
- Identify gaps, issues, and improvement opportunities
- Produce structured validation/review reports

## What Validation Agents Do NOT Do

- Gather new information (that's Research)
- Write code or tests (that's Creation)
- Execute tests (that's Execution)
- Make design decisions (that's Planning)

## What They Validate

| Agent | Validates |
|-------|-----------|
| RequirementsReview | Orchestration artifacts (Research.md, requirements docs) |
| ContractsReview | Design specifications (Contracts.md, interfaces, schemas) |
| PlanReview | Implementation plans (Plan.md) |
| SystemDesignReview | System design documents (SystemDesign.md) |
| ImplementationReview | Project files (source code) |
| TestsReview TDD | Project files (test code) |
