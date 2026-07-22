# Planning

Work breakdown, sequencing, and architecture agents.

## Purpose

Planning agents define WHAT work to do, WHEN to do it, and HOW it should be structured. They create plans and designs that guide downstream implementation - they do not implement.

## Agents

| ID | Agent | Version | Description |
|----|-------|---------|-------------|
| 6 | [planner-tdd-soft](./planner-tdd-soft.md) | 4.1.0 | Creates implementation plans with task breakdown, sequencing, and dependencies |
| 7 | [planner-audit](./planner-audit.md) | 3.0.0 | Creates audit plans splitting changed files into typed stages (Implementation, Tests, Architecture, Contracts) for iterative auditing |
| 8 | [contracts-designer](./contracts-designer.md) | 2.1.0 | Defines interfaces, contracts, data structures, and component specifications |
| 5 | [system-designer](./system-designer.md) | 2.0.1 | Designs system architecture, component interactions, and high-level structure |
| 4 | [requirements-refinement](./requirements-refinement.md) | 2.1.0 | Refines and clarifies requirements based on research findings |
| 24 | [knowledge-base-flag-sorter](./knowledge-base-flag-sorter.md) | 1.1.0 | Collects correction flags from KBFlags.md, organizes bottom-up by tier, creates correction stages in KBProgress.md |
| 33 | [pr-requirements-analyzer](./pr-requirements-analyzer.md) | 1.0.0 | Analyzes PR context — fetches changed file list and stats, summarizes existing comment threads, confirms audit scope with user, enriches Requirements.md with PR metadata |

## Planning vs Design Distinction

| Aspect | Planner | ContractsDesigner | SystemDesigner |
|--------|---------|-------------------|----------------|
| Focus | WHAT and WHEN | HOW (detailed) | HOW (high-level) |
| Output | Plan.md, Stage-{N}/Plan.md, Stage-{N}/PlanProgress.md | Contracts.md | SystemDesign.md |
| Defines | Tasks, stages, sequencing | Interfaces, contracts, schemas | Architecture, components, interactions |
| Example | "Stage 1: Create auth service" | "IAuthService.login(LoginRequest) → AuthResult" | "Auth service communicates with User service via REST" |

## What Planning Agents Do

- Analyze validated requirements and research findings
- Break down work into discrete, implementable tasks
- Define task sequencing and dependencies
- Identify milestones and checkpoints
- Define interfaces, contracts, and data structures
- Document architectural decisions with rationale

## What Planning Agents Do NOT Do

- Gather requirements (that's Research)
- Validate requirements (that's Validation)
- Write implementation code or tests (that's Creation)
- Execute tests (that's Execution)
