# Internal Summary: 7.4.1

<!-- generated:internal-overview -->
## Overview

- **Version:** 7.4.1
- **Reports:** 32
- **Suites:** execution-groups, hitl-gate, infrastructure-triggers, route-back, status-routing, wildcard-expansion
- **Models:** claude-opus-4-6, claude-opus-5, claude-sonnet-4-6, claude-sonnet-5, gpt-5.6-luna, gpt-5.6-sol, gpt-5.6-terra
- **Harnesses:** claude-code, opencode

<!-- /generated:internal-overview -->

<!-- generated:problem-areas -->
## Problem Areas

| Suite | ID | Test | Best Rate | Best Combo | Worst Rate | Worst Combo | Spread |
|-------|----|------|-----------|------------|------------|-------------|--------|
| execution-groups | 50 | impl-first-reorder | 100% (100) | claude-sonnet-4-6/claude-code | 99% (100) | claude-sonnet-5/claude-code | 1% |
| execution-groups | 51 | impl-only-skip-tests | 100% (100) | claude-sonnet-4-6/claude-code | 43% (100/101) | gpt-5.6-luna/opencode | 57% |
| hitl-gate | 55 | hitl-plan-stage-all-agents | 100% (100) | claude-sonnet-4-6/claude-code | 53% (100/101) | gpt-5.6-luna/opencode | 47% |
| hitl-gate | 56 | hitl-plan-stage-override | 100% (100) | claude-sonnet-4-6/claude-code | 33% (100/101) | gpt-5.6-luna/opencode | 67% |
| hitl-gate | 57 | hitl-redispatch-unapproved | 100% (100) | claude-sonnet-5/claude-code | 96% (100/101) | gpt-5.6-sol/opencode | 4% |
| infrastructure-triggers | 59 | interval-overdue | 100% (100) | gpt-5.6-sol/opencode | 42% (100/101) | gpt-5.6-terra/opencode | 58% |
| infrastructure-triggers | 62 | phase-end-trigger | 100% (100) | claude-sonnet-4-6/claude-code | 45% (100) | gpt-5.6-luna/opencode | 55% |
| route-back | 66 | contracts-routeback-quality-gate | 100% (100) | claude-opus-5/claude-code | 0% (100) | gpt-5.6-luna/opencode | 100% |
| status-routing | 69 | blocked-e501-retry | 100% (100) | claude-opus-4-6/claude-code | 99% (100) | claude-sonnet-5/claude-code | 1% |
| status-routing | 71 | capability-exceeded-escalate | 100% (100) | claude-opus-4-6/claude-code | 82% (100) | claude-sonnet-4-6/claude-code | 18% |
| status-routing | 72 | creator-fix-rereview | 100% (100) | claude-opus-4-6/claude-code | 99% (100) | claude-sonnet-5/claude-code | 1% |
| status-routing | 73 | findings-route-back | 94% (100) | claude-sonnet-5/claude-code | 91% (100) | claude-opus-4-6/claude-code | 3% |
| status-routing | 74 | needs-clarification-no-advance | 100% (100) | claude-opus-4-6/claude-code | 15% (100) | gpt-5.6-luna/opencode | 85% |
| wildcard-expansion | 76 | wildcard-after-routeback | 100% (100) | claude-sonnet-5/claude-code | 2% (100) | gpt-5.6-luna/opencode | 98% |
| wildcard-expansion | 77 | wildcard-dual-expansion | 100% (100) | claude-sonnet-4-6/claude-code | 7% (100/101) | gpt-5.6-luna/opencode | 93% |
| wildcard-expansion | 78 | wildcard-input-expansion | 99% (100/101) | gpt-5.6-sol/opencode | 7% (100) | gpt-5.6-luna/opencode | 92% |

<!-- /generated:problem-areas -->

<!-- generated:infrastructure-failures -->
## Infrastructure Failures

| Suite | ID | Test | Best Rate | Best Combo | Worst Rate | Worst Combo | Spread |
|-------|----|------|-----------|------------|------------|-------------|--------|
| execution-groups | 50 | impl-first-reorder | 96% (100/109) | gpt-5.6-terra/opencode | 35% (100/102) | gpt-5.6-luna/opencode | 61% |
| execution-groups | 51 | impl-only-skip-tests | 100% (100/103) | gpt-5.6-sol/opencode | 99% (100/105) | gpt-5.6-terra/opencode | 1% |
| execution-groups | 52 | tests-only-skip-impl | 100% (100/104) | gpt-5.6-sol/opencode | 40% (100/103) | gpt-5.6-luna/opencode | 60% |
| hitl-gate | 56 | hitl-plan-stage-override | 100% (99/103) | gpt-5.6-sol/opencode | 99% (100/103) | gpt-5.6-terra/opencode | 1% |
| hitl-gate | 57 | hitl-redispatch-unapproved | 74% (100/104) | gpt-5.6-terra/opencode | 6% (100/102) | gpt-5.6-luna/opencode | 68% |
| infrastructure-triggers | 58 | gated-checkpoint-disabled | 100% (100/104) | gpt-5.6-sol/opencode | 38% (100/102) | gpt-5.6-luna/opencode | 62% |
| infrastructure-triggers | 60 | interval-precise-boundary | 100% (100/103) | gpt-5.6-sol/opencode | 55% (100/102) | gpt-5.6-luna/opencode | 45% |
| infrastructure-triggers | 61 | multiple-triggers-same-boundary | 100% (51/149) | claude-sonnet-4-6/claude-code | 37% (99/102) | gpt-5.6-luna/opencode | 63% |
| infrastructure-triggers | 62 | phase-end-trigger | 100% (100/102) | gpt-5.6-sol/opencode | 100% (100/102) | gpt-5.6-sol/opencode | 0% |
| infrastructure-triggers | 63 | restore-class-exclusion | 100% (100/105) | gpt-5.6-sol/opencode | 38% (99/105) | gpt-5.6-luna/opencode | 62% |
| infrastructure-triggers | 64 | stage-end-checkpoint | 100% (100/102) | gpt-5.6-sol/opencode | 42% (100/102) | gpt-5.6-luna/opencode | 58% |
| route-back | 67 | planner-routeback-quality-gate | 0% (99/106) | gpt-5.6-sol/opencode | 0% (99/106) | gpt-5.6-sol/opencode | 0% |
| status-routing | 68 | blocked-e101-retry | 100% (100/108) | gpt-5.6-sol/opencode | 58% (100/102) | gpt-5.6-luna/opencode | 42% |
| status-routing | 69 | blocked-e501-retry | 100% (100/104) | gpt-5.6-sol/opencode | 49% (99/107) | gpt-5.6-luna/opencode | 51% |
| status-routing | 70 | blocked-e503-hitl-retry | 80% (75/125) | claude-opus-4-6/claude-code | 1% (100/102) | gpt-5.6-sol/opencode | 79% |
| status-routing | 72 | creator-fix-rereview | 100% (100/104) | gpt-5.6-sol/opencode | 14% (100/102) | gpt-5.6-luna/opencode | 86% |
| status-routing | 73 | findings-route-back | 88% (100/102) | gpt-5.6-sol/opencode | 0% (99/105) | gpt-5.6-luna/opencode | 88% |
| status-routing | 74 | needs-clarification-no-advance | 100% (100/102) | gpt-5.6-sol/opencode | 100% (100/102) | gpt-5.6-sol/opencode | 0% |
| status-routing | 75 | partially-done-redispatch | 100% (97/110) | gpt-5.6-sol/opencode | 52% (98/112) | gpt-5.6-luna/opencode | 48% |

<!-- /generated:infrastructure-failures -->

<!-- generated:exclusions-detail -->
## Exclusions Detail

| Suite | Test | Reason | Termination | Detail |
|-------|------|--------|-------------|--------|
| execution-groups | impl-first-reorder | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-first-reorder | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-first-reorder | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-first-reorder | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-first-reorder | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-first-reorder | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-first-reorder | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-first-reorder | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-first-reorder | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-first-reorder | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-first-reorder | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-first-reorder | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-only-skip-tests | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-only-skip-tests | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-only-skip-tests | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-only-skip-tests | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-only-skip-tests | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-only-skip-tests | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-only-skip-tests | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-only-skip-tests | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | impl-only-skip-tests | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | tests-only-skip-impl | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | tests-only-skip-impl | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | tests-only-skip-impl | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | tests-only-skip-impl | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | tests-only-skip-impl | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | tests-only-skip-impl | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | tests-only-skip-impl | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | tests-only-skip-impl | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | tests-only-skip-impl | echo_mismatch | early_exit | invocation 1: echo mismatch |
| execution-groups | tests-only-skip-impl | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-plan-stage-all-agents | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-plan-stage-all-agents | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-plan-stage-override | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-plan-stage-override | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-plan-stage-override | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-plan-stage-override | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-plan-stage-override | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-plan-stage-override | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-plan-stage-override | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-plan-stage-override | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-redispatch-unapproved | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-redispatch-unapproved | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-redispatch-unapproved | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-redispatch-unapproved | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-redispatch-unapproved | spawn_failed | spawn_failed | harness process exited non-zero |
| hitl-gate | hitl-redispatch-unapproved | echo_mismatch | early_exit | invocation 1: echo mismatch |
| hitl-gate | hitl-redispatch-unapproved | spawn_failed | spawn_failed | harness process exited non-zero |
| infrastructure-triggers | gated-checkpoint-disabled | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | gated-checkpoint-disabled | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| infrastructure-triggers | gated-checkpoint-disabled | echo_mismatch | early_exit | invocation 1: echo mismatch; invocation 2: echo mismatch |
| infrastructure-triggers | gated-checkpoint-disabled | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | gated-checkpoint-disabled | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | gated-checkpoint-disabled | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| infrastructure-triggers | gated-checkpoint-disabled | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | gated-checkpoint-disabled | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | gated-checkpoint-disabled | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | gated-checkpoint-disabled | echo_mismatch | early_exit | invocation 2: echo mismatch |
| infrastructure-triggers | interval-overdue | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | interval-precise-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | interval-precise-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | interval-precise-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | interval-precise-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | interval-precise-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | interval-precise-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | interval-precise-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | interval-precise-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | interval-precise-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | interval-precise-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | interval-precise-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | interval-precise-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 3: echo mismatch; assertion failures also present |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 2: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 3: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | multiple-triggers-same-boundary | echo_mismatch | early_exit | invocation 1: echo mismatch; invocation 3: echo mismatch; assertion failures also present |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | multiple-triggers-same-boundary | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| infrastructure-triggers | phase-end-trigger | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | phase-end-trigger | echo_mismatch | early_exit | invocation 2: echo mismatch |
| infrastructure-triggers | phase-end-trigger | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 2: echo mismatch |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 2: echo mismatch |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch; invocation 2: echo mismatch |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| infrastructure-triggers | restore-class-exclusion | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| infrastructure-triggers | stage-end-checkpoint | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | stage-end-checkpoint | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | stage-end-checkpoint | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| infrastructure-triggers | stage-end-checkpoint | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | stage-end-checkpoint | echo_mismatch | early_exit | invocation 1: echo mismatch |
| infrastructure-triggers | stage-end-checkpoint | echo_mismatch | early_exit | invocation 2: echo mismatch |
| route-back | contracts-routeback-quality-gate | echo_mismatch | early_exit | invocation 1: echo mismatch |
| route-back | contracts-routeback-quality-gate | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| route-back | planner-routeback-quality-gate | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| route-back | planner-routeback-quality-gate | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| route-back | planner-routeback-quality-gate | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| route-back | planner-routeback-quality-gate | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| route-back | planner-routeback-quality-gate | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| route-back | planner-routeback-quality-gate | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| route-back | planner-routeback-quality-gate | spawn_failed | spawn_failed | harness process exited non-zero |
| route-back | planner-routeback-quality-gate | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | blocked-e101-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e101-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e101-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e101-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e101-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e101-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e101-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e101-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e101-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e101-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e101-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e101-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e501-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | blocked-e503-hitl-retry | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | blocked-e503-hitl-retry | spawn_failed | spawn_failed | harness process exited non-zero (exit status 1) |
| status-routing | creator-fix-rereview | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | creator-fix-rereview | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | creator-fix-rereview | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | creator-fix-rereview | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | creator-fix-rereview | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | creator-fix-rereview | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | creator-fix-rereview | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | creator-fix-rereview | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | findings-route-back | spawn_failed | spawn_failed | harness process exited non-zero: Warning: no stdin data received in 3s, proceeding without it. If piping from a slow command, redirect stdin explicitly: < /dev/null to skip, or wait longer.
Error: Input must be provided either through stdin or as a prompt argument when using --print
 |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | findings-route-back | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 2): fatal error: bcryptprimitives.dll not found
runtime: panic before malloc heap initialized

runtime stack:
runtime.throw({0x7ff6433267fe?, 0x4e4cbff868?})
	C:/Program Files/Go/src/runtime/panic.go:1229 +0x4d fp=0x4e4cbff7b8 sp=0x4e4cbff788 pc=0x7ff642fbec0d
runtime.loadOptionalSyscalls()
	C:/Program Files/Go/src/runtime/os_windows.go:267 +0x349 fp=0x4e4cbff8a0 sp=0x4e4cbff7b8 pc=0x7ff642f85829
runtime.osinit()
	C:/Program Files/Go/src/runtime/os_windows.go:464 +0x3d fp=0x4e4cbff920 sp=0x4e4cbff8a0 pc=0x7ff642f85f3d
runtime.rt0_go()
	C:/Program Files/Go/src/runtime/asm_amd64.s:372 +0x13e fp=0x4e4cbff928 sp=0x4e4cbff920 pc=0x7ff642fc3f3e
 |
| status-routing | findings-route-back | spawn_failed | spawn_failed | harness process exited non-zero: Systém nemůže nalézt uvedenou cestu.
 |
| status-routing | findings-route-back | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: agentdeploy: deployment tool unavailable: deployment tool "C:\\AI\\MOSAIC\\MOSAIC\\Tools\\AgentTest\\dist\\mosaic-deploy.exe" could not be invoked: fork/exec C:\AI\MOSAIC\MOSAIC\Tools\AgentTest\dist\mosaic-deploy.exe: K dokončení požadované služby je stránkovací soubor příliš malý. |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | findings-route-back | spawn_failed | spawn_failed | harness process exited non-zero (exit status 3221226505) |
| status-routing | findings-route-back | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: agentdeploy: deployment tool unavailable: deployment tool "C:\\AI\\MOSAIC\\MOSAIC\\Tools\\AgentTest\\dist\\mosaic-deploy.exe" could not be invoked: fork/exec C:\AI\MOSAIC\MOSAIC\Tools\AgentTest\dist\mosaic-deploy.exe: K dokončení požadované služby je stránkovací soubor příliš malý. |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 1: echo mismatch; assertion failures also present |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | findings-route-back | spawn_failed | spawn_failed | harness process exited non-zero: The system cannot execute the specified program.
 |
| status-routing | findings-route-back | spawn_failed | spawn_failed | harness process exited non-zero (exit status 3221226505) |
| status-routing | findings-route-back | infrastructure |  | runner/deploy error before subject started: runner: deploying catalogue path: deploy failed (exit 2): fatal error: runtime: cannot allocate memory

runtime stack:
runtime.throw({0x7ff643327075?, 0x7ff643615d40?})
	C:/Program Files/Go/src/runtime/panic.go:1229 +0x4d fp=0x4db9ff690 sp=0x4db9ff660 pc=0x7ff642fbec0d
runtime.persistentalloc1(0x100, 0x4db9ff718?, 0x7ff64365cc20)
	C:/Program Files/Go/src/runtime/malloc.go:2379 +0x265 fp=0x4db9ff6e8 sp=0x4db9ff690 pc=0x7ff642f5ecc5
runtime.persistentalloc.func1()
	C:/Program Files/Go/src/runtime/malloc.go:2332 +0x28 fp=0x4db9ff718 sp=0x4db9ff6e8 pc=0x7ff642f5ea48
runtime.persistentalloc(0x7ff642f41000?, 0x7ff643615d40?, 0x7ff643614940?)
	C:/Program Files/Go/src/runtime/malloc.go:2331 +0x45 fp=0x4db9ff760 sp=0x4db9ff718 pc=0x7ff642f5e9e5
runtime.(*addrRanges).init(0x7ff64362bef8, 0x7ff64365cc20)
	C:/Program Files/Go/src/runtime/mranges.go:258 +0x3a fp=0x4db9ff788 sp=0x4db9ff760 pc=0x7ff642f82e7a
runtime.(*pageAlloc).init(0x7ff64361be68, 0x7ff64361be60, 0x7ff64365cc20, 0x0)
	C:/Program Files/Go/src/runtime/mpagealloc.go:324 +0x71 fp=0x4db9ff7b8 sp=0x4db9ff788 pc=0x7ff642f7cb91
runtime.(*mheap).init(0x7ff64361be60)
	C:/Program Files/Go/src/runtime/mheap.go:821 +0x225 fp=0x4db9ff7f0 sp=0x4db9ff7b8 pc=0x7ff642f78cc5
runtime.mallocinit()
	C:/Program Files/Go/src/runtime/malloc.go:493 +0xfd fp=0x4db9ff838 sp=0x4db9ff7f0 pc=0x7ff642f5c83d
runtime.schedinit()
	C:/Program Files/Go/src/runtime/proc.go:877 +0xf0 fp=0x4db9ff890 sp=0x4db9ff838 pc=0x7ff642f8dcd0
runtime.rt0_go()
	C:/Program Files/Go/src/runtime/asm_amd64.s:373 +0x143 fp=0x4db9ff898 sp=0x4db9ff890 pc=0x7ff642fc3f43
 |
| status-routing | findings-route-back | spawn_failed | spawn_failed | harness process exited non-zero (exit status 3221226505) |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | findings-route-back | spawn_failed | spawn_failed | harness process exited non-zero: Warning: no stdin data received in 3s, proceeding without it. If piping from a slow command, redirect stdin explicitly: < /dev/null to skip, or wait longer.
Error: Input must be provided either through stdin or as a prompt argument when using --print
 |
| status-routing | findings-route-back | spawn_failed | spawn_failed | harness process exited non-zero: Warning: no stdin data received in 3s, proceeding without it. If piping from a slow command, redirect stdin explicitly: < /dev/null to skip, or wait longer.
Error: Input must be provided either through stdin or as a prompt argument when using --print
 |
| status-routing | findings-route-back | spawn_failed | spawn_failed | harness process exited non-zero: Warning: no stdin data received in 3s, proceeding without it. If piping from a slow command, redirect stdin explicitly: < /dev/null to skip, or wait longer.
Error: Input must be provided either through stdin or as a prompt argument when using --print
 |
| status-routing | findings-route-back | spawn_failed | spawn_failed | harness process exited non-zero (exit status 3221226505) |
| status-routing | findings-route-back | spawn_failed | spawn_failed | harness process exited non-zero (exit status 3221226505) |
| status-routing | findings-route-back | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | needs-clarification-no-advance | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | needs-clarification-no-advance | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch; invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 2: echo mismatch; assertion failures also present |
| status-routing | partially-done-redispatch | echo_mismatch | early_exit | invocation 1: echo mismatch |
| wildcard-expansion | wildcard-after-routeback | echo_mismatch | early_exit | invocation 1: echo mismatch |
| wildcard-expansion | wildcard-dual-expansion | echo_mismatch | early_exit | invocation 1: echo mismatch |
| wildcard-expansion | wildcard-dual-expansion | echo_mismatch | early_exit | invocation 1: echo mismatch |
| wildcard-expansion | wildcard-dual-expansion | echo_mismatch | early_exit | invocation 1: echo mismatch |
| wildcard-expansion | wildcard-input-expansion | echo_mismatch | early_exit | invocation 1: echo mismatch |

<!-- /generated:exclusions-detail -->

<!-- analysis:internal-analysis -->
<!-- /analysis:internal-analysis -->
