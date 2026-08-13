# Creation

Agents that build project files (code and tests).

## Purpose

Creation agents write actual project files - source code and test code. They transform plans and designs into executable code that implements the specified behavior.

## Agents

| ID | Agent | Version | Description |
|----|-------|---------|-------------|
| 16 | [implementation-tdd](./implementation-tdd.md) | 3.1.0 | Writes implementation code that makes tests pass (TDD GREEN phase) |
| 15 | [test-writer-tdd](./test-writer-tdd.md) | 3.0.0 | Writes, updates, and fixes test code (TDD RED phase and beyond) |
| 25 | [knowledge-base-index-assembler](./knowledge-base-index-assembler.md) | 1.1.0 | Creates top-level Index.md in the KB output path from completed KB documents |

## TDD Phases

| Phase | Agent | Creates |
|-------|-------|---------|
| RED | TestWriter TDD | Failing tests that define behavior |
| GREEN | Implementation TDD | Code that makes tests pass |
| REFACTOR | Implementation TDD | Cleaner code while keeping tests green |

## What Creation Agents Do

- Write test code based on design specifications
- Write implementation code to satisfy tests
- Create contract files (interfaces, DTOs, enums)
- Follow existing codebase patterns and conventions
- Handle errors and edge cases as specified

## What Creation Agents Do NOT Do

- Gather requirements (that's Research)
- Validate code quality (that's Validation)
- Create designs (that's Planning)
- Execute tests (that's Execution)

## Output Types

Creation agents produce **project files**, not just orchestration artifacts:
- Source code files (e.g., `src/UserService.ts`)
- Test files (e.g., `tests/UserService.test.ts`)
- Contract files (interfaces, types, enums)
