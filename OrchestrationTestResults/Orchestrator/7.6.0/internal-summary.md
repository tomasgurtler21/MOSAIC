# Internal Summary: 7.6.0

<!-- generated:internal-overview -->
## Overview

- **Version:** 7.6.0
- **Reports:** 23
- **Suites:** execution-groups, hitl-gate, infrastructure-triggers, route-back, status-routing, wildcard-expansion
- **Models:** claude-haiku-4-5, claude-opus-5, claude-opus-5-5, claude-sonnet-5, claude-sonnet-5-5, gpt-5.6-terra
- **Harnesses:** claude-code, opencode

<!-- /generated:internal-overview -->

<!-- generated:problem-areas -->
## Problem Areas

| Suite | ID | Test | Best Rate | Best Combo | Worst Rate | Worst Combo | Spread |
|-------|----|------|-----------|------------|------------|-------------|--------|
| execution-groups | 50 | impl-first-reorder | 100% (100) | claude-opus-5-5/claude-code | 60% (100) | claude-haiku-4-5/claude-code | 40% |
| execution-groups | 51 | impl-only-skip-tests | 100% (100) | claude-opus-5-5/claude-code | 50% (100) | claude-haiku-4-5/claude-code | 50% |
| execution-groups | 52 | tests-only-skip-impl | 100% (100) | claude-opus-5-5/claude-code | 92% (100) | claude-haiku-4-5/claude-code | 8% |
| hitl-gate | 55 | hitl-plan-stage-all-agents | 100% (100) | claude-sonnet-5-5/claude-code | 24% (100) | claude-haiku-4-5/claude-code | 76% |
| hitl-gate | 56 | hitl-plan-stage-override | 100% (100) | claude-opus-5-5/claude-code | 54% (100) | claude-haiku-4-5/claude-code | 46% |
| hitl-gate | 57 | hitl-redispatch-unapproved | 100% (100) | claude-opus-5-5/claude-code | 63% (100) | claude-haiku-4-5/claude-code | 37% |
| infrastructure-triggers | 58 | gated-checkpoint-disabled | 100% (100) | claude-opus-5-5/claude-code | 61% (100) | claude-haiku-4-5/claude-code | 39% |
| infrastructure-triggers | 59 | interval-overdue | 100% (100) | claude-sonnet-5-5/claude-code | 15% (100) | claude-haiku-4-5/claude-code | 85% |
| infrastructure-triggers | 60 | interval-precise-boundary | 100% (100) | claude-sonnet-5-5/claude-code | 13% (100) | claude-haiku-4-5/claude-code | 87% |
| infrastructure-triggers | 61 | multiple-triggers-same-boundary | 99% (100) | claude-sonnet-5-5/claude-code | 41% (100) | claude-haiku-4-5/claude-code | 58% |
| infrastructure-triggers | 62 | phase-end-trigger | 100% (100) | claude-opus-5-5/claude-code | 17% (100) | claude-haiku-4-5/claude-code | 83% |
| infrastructure-triggers | 63 | restore-class-exclusion | 100% (100) | claude-opus-5-5/claude-code | 57% (100) | claude-haiku-4-5/claude-code | 43% |
| infrastructure-triggers | 64 | stage-end-checkpoint | 100% (100) | claude-opus-5-5/claude-code | 33% (100) | claude-haiku-4-5/claude-code | 67% |
| route-back | 66 | contracts-routeback-quality-gate | 100% (100) | claude-opus-5/claude-code | 4% (100) | claude-haiku-4-5/claude-code | 96% |
| route-back | 67 | planner-routeback-quality-gate | 1% (100) | claude-haiku-4-5/claude-code | 0% (100) | claude-opus-5/claude-code | 1% |
| status-routing | 68 | blocked-e101-retry | 100% (100) | claude-sonnet-5-5/claude-code | 0% (100) | gpt-5.6-terra/opencode | 100% |
| status-routing | 69 | blocked-e501-retry | 100% (100) | claude-sonnet-5-5/claude-code | 75% (100) | claude-haiku-4-5/claude-code | 25% |
| status-routing | 70 | blocked-e503-hitl-retry | 100% (100) | claude-sonnet-5-5/claude-code | 86% (100/101) | claude-haiku-4-5/claude-code | 14% |
| status-routing | 71 | capability-exceeded-escalate | 100% (100) | claude-haiku-4-5/claude-code | 80% (100) | gpt-5.6-terra/opencode | 20% |
| status-routing | 72 | creator-fix-rereview | 100% (100) | claude-sonnet-5-5/claude-code | 74% (100) | claude-haiku-4-5/claude-code | 26% |
| status-routing | 73 | findings-route-back | 100% (100) | claude-sonnet-5-5/claude-code | 39% (100) | claude-haiku-4-5/claude-code | 61% |
| status-routing | 74 | needs-clarification-no-advance | 100% (100) | claude-sonnet-5-5/claude-code | 45% (100) | claude-haiku-4-5/claude-code | 55% |
| status-routing | 75 | partially-done-redispatch | 100% (100) | claude-sonnet-5-5/claude-code | 88% (100) | claude-haiku-4-5/claude-code | 12% |
| wildcard-expansion | 76 | wildcard-after-routeback | 100% (100) | claude-sonnet-5-5/claude-code | 79% (100) | claude-haiku-4-5/claude-code | 21% |
| wildcard-expansion | 77 | wildcard-dual-expansion | 100% (100) | claude-sonnet-5-5/claude-code | 75% (100) | claude-haiku-4-5/claude-code | 25% |
| wildcard-expansion | 78 | wildcard-input-expansion | 100% (100) | claude-sonnet-5-5/claude-code | 48% (100) | claude-haiku-4-5/claude-code | 52% |

<!-- /generated:problem-areas -->

<!-- generated:infrastructure-failures -->
## Infrastructure Failures

| Suite | ID | Test | Best Rate | Best Combo | Worst Rate | Worst Combo | Spread |
|-------|----|------|-----------|------------|------------|-------------|--------|
| execution-groups | 52 | tests-only-skip-impl | 99% (83/100) | claude-sonnet-5/claude-code | 99% (83/100) | claude-sonnet-5/claude-code | 0% |
| status-routing | 69 | blocked-e501-retry | 100% (98/105) | gpt-5.6-terra/opencode | 100% (98/105) | gpt-5.6-terra/opencode | 0% |
| status-routing | 70 | blocked-e503-hitl-retry | 77% (99/102) | gpt-5.6-terra/opencode | 77% (99/102) | gpt-5.6-terra/opencode | 0% |
| status-routing | 73 | findings-route-back | 93% (100/102) | gpt-5.6-terra/opencode | 93% (100/102) | gpt-5.6-terra/opencode | 0% |
| status-routing | 75 | partially-done-redispatch | 100% (99/111) | gpt-5.6-terra/opencode | 100% (99/111) | gpt-5.6-terra/opencode | 0% |

<!-- /generated:infrastructure-failures -->

<!-- generated:exclusions-detail -->
## Exclusions Detail

| Suite | Test | Reason | Termination | Detail |
|-------|------|--------|-------------|--------|
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| execution-groups | tests-only-skip-impl | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 1):  |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | spawn_failed | spawn_failed | harness process exited non-zero |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e501-retry | spawn_failed | spawn_failed | harness process exited non-zero |
| status-routing | blocked-e501-retry | spawn_failed | spawn_failed | harness process exited non-zero |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | creator-fix-rereview | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | needs-clarification-no-advance | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |

<!-- /generated:exclusions-detail -->

<!-- analysis:internal-analysis -->
<!-- /analysis:internal-analysis -->
