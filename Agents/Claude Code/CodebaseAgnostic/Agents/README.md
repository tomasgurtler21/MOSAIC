# Claude Code Agents

This folder contains 14 generic agent templates for the Claude Code multi-agent orchestration system.

## Agent Index

| ID | Agent | Version | Based On | Description |
|----|-------|---------|----------|-------------|
| 1 | [codebase-research](codebase-research.md) | 2.2.0 | 2.2.0 | Researches and analyzes the codebase to gather context |
| 4 | [requirements-refinement](requirements-refinement.md) | 2.1.0 | 2.1.0 | Refines and clarifies requirements with stakeholders |
| 5 | [system-designer](system-designer.md) | 2.0.1 | 2.0.1 | Designs system architecture and components |
| 6 | [planner-tdd-soft](planner-tdd-soft.md) | 4.1.0 | 4.1.0 | Creates implementation plans using TDD approach |
| 8 | [contracts-designer](contracts-designer.md) | 2.1.0 | 2.1.0 | Designs interfaces and API contracts |
| 9 | [requirements-review](requirements-review.md) | 2.0.1 | 2.0.1 | Reviews and validates requirements for completeness |
| 10 | [system-design-review](system-design-review.md) | 2.0.1 | 2.0.1 | Reviews system architecture and design decisions |
| 11 | [plan-review](plan-review.md) | 3.1.0 | 3.1.0 | Reviews implementation plans for feasibility |
| 12 | [contracts-review](contracts-review.md) | 2.1.0 | 2.1.0 | Reviews interface contracts and API designs |
| 13 | [tests-review-tdd](tests-review-tdd.md) | 2.3.0 | 2.3.0 | Reviews tests in TDD workflow for quality |
| 14 | [implementation-review](implementation-review.md) | 2.2.0 | 2.2.0 | Reviews implementation code for quality |
| 15 | [test-writer-tdd](test-writer-tdd.md) | 3.0.0 | 3.0.0 | Writes, updates, and fixes tests following TDD methodology |
| 16 | [implementation-tdd](implementation-tdd.md) | 3.1.0 | 3.1.0 | Implements code to pass TDD tests |
| 17 | [test-runner](test-runner.md) | 2.1.0 | 2.1.0 | Executes tests and reports results |

## Agent Categories

### Research
- **codebase-research** - Gathers context from the codebase

### Planning
- **system-designer** - Designs system architecture
- **planner-tdd-soft** - Creates TDD implementation plans
- **contracts-designer** - Designs interfaces and contracts

### Validation (Review)
- **requirements-review** - Validates requirements
- **plan-review** - Validates plans
- **contracts-review** - Validates contracts
- **system-design-review** - Validates system design
- **tests-review-tdd** - Validates TDD tests
- **implementation-review** - Validates implementation

### Creation
- **test-writer-tdd** - Writes, updates, and fixes tests (TDD)
- **implementation-tdd** - Creates implementation (TDD)

### Execution
- **test-runner** - Runs tests and reports results

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
