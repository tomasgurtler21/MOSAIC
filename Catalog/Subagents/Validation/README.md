# Validation

Quality gates, verification, and review agents.

## Purpose

Validation agents are gatekeepers that ensure quality before proceeding to the next phase. They review, validate, and identify issues - they do not fix or implement solutions.

## Agents

| ID | Agent | Version | Description |
|----|-------|---------|-------------|
| 9 | [requirements-review](./requirements-review.md) | 5.0.4 | Reviews requirements for completeness at their declared depth, consistency, and feasibility against research findings |
| 12 | [contracts-review](./contracts-review.md) | 4.4.3 | Reviews contracts and design specifications for correctness and completeness |
| 11 | [plan-review](./plan-review.md) | 5.3.1 | Reviews implementation plans for feasibility, completeness, and alignment |
| 10 | [system-design-review](./system-design-review.md) | 3.4.1 | Reviews system design for architecture quality and design principles |
| 14 | [implementation-review](./implementation-review.md) | 4.2.3 | Reviews code quality, design compliance, and code standards |
| 13 | [tests-review-tdd](./tests-review-tdd.md) | 3.4.1 | Reviews test quality, coverage, and TDD RED discriminability |
| 35 | [build-review](./build-review.md) | 4.0.0 | Imports, builds, and deploys source changes, reporting actionable build results and deployment metadata without executing tests |
| 65 | [comparison-review](./comparison-review.md) | 1.0.2 | Autonomous quality gate on the comparison synthesis - validates balance, evidence, criteria alignment, and consistency with per-topic comparisons |

## What Validation Agents Do

- Evaluate completeness and consistency
- Detect contradictions and conflicts
- Verify alignment with specifications and designs
- Identify gaps, issues, and improvement opportunities
- Produce structured validation/review reports

## What Validation Agents Do NOT Do

- Gather new information (that's Research)
- Write code or tests (that's Creation)
- Own general-purpose test execution as a separate workflow step (that's Execution); a validation agent may run tests needed to verify its review condition
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
| BuildReview | Build imports, compilation results, and target deployment metadata |
| ComparisonReview | Orchestration artifacts (ComparisonAnalysis.md against per-topic comparisons and Requirements.md) |
