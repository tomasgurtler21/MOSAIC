# Execution

Agents that run tests and tools.

## Purpose

Execution agents run things and report results. They execute tests, capture output, and provide diagnostics - they do not fix issues or write code.

## Agents

| ID | Agent | Version | Description |
|----|-------|---------|-------------|
| 17 | [test-runner](./test-runner.md) | 2.1.0 | Executes tests and reports pass/fail outcomes with failure diagnostics |

## What Execution Agents Do

- Execute test suites using appropriate test runners
- Capture and report test results (pass/fail/skip counts)
- Provide detailed failure diagnostics
- Report code coverage metrics when available
- Produce structured test result artifacts

## What Execution Agents Do NOT Do

- Write tests (that's Creation)
- Write implementation code (that's Creation)
- Fix failing tests or implementation (that's Creation)
- Review code quality (that's Validation)

## Output

Execution agents produce orchestration artifacts with execution results:
- TestResults.md with pass/fail counts, failure details, coverage metrics
