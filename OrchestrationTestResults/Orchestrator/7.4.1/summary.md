# Version Summary: 7.4.1

<!-- generated:overview -->
## Overview

- **Version:** 7.4.1
- **Reports:** 24
- **Total Tests:** 10230
- **Suites:** execution-groups, hitl-gate, infrastructure-triggers, route-back, status-routing, wildcard-expansion
- **Models:** claude-opus-4-6, claude-sonnet-4-6, gpt-5.6-luna, gpt-5.6-sol, gpt-5.6-terra
- **Harnesses:** claude-code, opencode

<!-- /generated:overview -->

**IMPORTANT** - Models Pass rate comparison is wrong until listed models execute same suites (Tests count more or less matches across models)

**IMPORTANT** - Costs are inaccurate WIP, and relative cost can be compared only between models tested at same harness, and even that is inaccurate in same cases. Comparing models from different harnesses can be done only for Pass rate.
<!-- generated:model-comparison -->
## Model Comparison

| Model | Tests | Pass Rate | Cost |
|-------|-------|-----------|------|
| claude-opus-4-6 | 200 | 7% | $43.59/100t |
| claude-sonnet-4-6 | 2247 | 85% | $13.09/100t |
| gpt-5.6-luna | 2593 | 32% | $0.12/100t |
| gpt-5.6-sol | 2595 | 89% | $7.73/100t |
| gpt-5.6-terra | 2595 | 79% | $2.66/100t |

<!-- /generated:model-comparison -->

**IMPORTANT** - Harnes comparison is WIP, currently comparison does not make much sense

<!-- generated:harness-comparison -->
## Harness Comparison

| Harness | Tests | Pass Rate | Cost |
|---------|-------|-----------|------|
| claude-code | 2447 | 79% | $15.58/100t |
| opencode | 7783 | 67% | $3.50/100t |

<!-- /generated:harness-comparison -->

<!-- generated:model-results -->
## Model Results

### claude-opus-4-6

| Suite | Harness | Tests | Pass Rate | Cost |
|-------|---------|-------|-----------|------|
| route-back | claude-code | 200 | 7% | $43.59/100t |

### claude-sonnet-4-6

| Suite | Harness | Tests | Pass Rate | Cost |
|-------|---------|-------|-----------|------|
| execution-groups | claude-code | 300 | 100% | $7.63/100t |
| hitl-gate | claude-code | 300 | 100% | $6.60/100t |
| infrastructure-triggers | claude-code | 651 | 99% | $17.73/100t |
| route-back | claude-code | 200 | 8% | $19.28/100t |
| status-routing | claude-code | 796 | 83% | $12.24/100t |

### gpt-5.6-luna

| Suite | Harness | Tests | Pass Rate | Cost |
|-------|---------|-------|-----------|------|
| execution-groups | opencode | 300 | 39% | $0.03/100t |
| hitl-gate | opencode | 300 | 31% | $0.02/100t |
| infrastructure-triggers | opencode | 698 | 44% | $0.26/100t |
| route-back | opencode | 200 | 0% | $0.06/100t |
| status-routing | opencode | 795 | 38% | $0.13/100t |
| wildcard-expansion | opencode | 300 | 5% | $0.01/100t |

### gpt-5.6-sol

| Suite | Harness | Tests | Pass Rate | Cost |
|-------|---------|-------|-----------|------|
| execution-groups | opencode | 300 | 100% | $2.58/100t |
| hitl-gate | opencode | 299 | 99% | $2.30/100t |
| infrastructure-triggers | opencode | 700 | 100% | $9.18/100t |
| route-back | opencode | 199 | 17% | $17.00/100t |
| status-routing | opencode | 797 | 86% | $9.89/100t |
| wildcard-expansion | opencode | 300 | 100% | $2.99/100t |

### gpt-5.6-terra

| Suite | Harness | Tests | Pass Rate | Cost |
|-------|---------|-------|-----------|------|
| execution-groups | opencode | 300 | 98% | $0.66/100t |
| hitl-gate | opencode | 300 | 86% | $0.66/100t |
| infrastructure-triggers | opencode | 698 | 73% | $3.15/100t |
| route-back | opencode | 200 | 21% | $6.22/100t |
| status-routing | opencode | 797 | 81% | $3.56/100t |
| wildcard-expansion | opencode | 300 | 97% | $0.78/100t |

<!-- /generated:model-results -->

<!-- generated:problem-areas -->
## Problem Areas

| Suite | ID | Test | Best Rate | Best Combo | Worst Rate | Worst Combo | Spread |
|-------|----|------|-----------|------------|------------|-------------|--------|
| execution-groups | 50 | impl-first-reorder | 100% | claude-sonnet-4-6/claude-code | 35% | gpt-5.6-luna/opencode | 65% |
| execution-groups | 51 | impl-only-skip-tests | 100% | claude-sonnet-4-6/claude-code | 43% | gpt-5.6-luna/opencode | 57% |
| execution-groups | 52 | tests-only-skip-impl | 100% | claude-sonnet-4-6/claude-code | 40% | gpt-5.6-luna/opencode | 60% |
| hitl-gate | 55 | hitl-plan-stage-all-agents | 100% | claude-sonnet-4-6/claude-code | 53% | gpt-5.6-luna/opencode | 47% |
| hitl-gate | 56 | hitl-plan-stage-override | 100% | claude-sonnet-4-6/claude-code | 33% | gpt-5.6-luna/opencode | 67% |
| hitl-gate | 57 | hitl-redispatch-unapproved | 99% | claude-sonnet-4-6/claude-code | 6% | gpt-5.6-luna/opencode | 93% |
| infrastructure-triggers | 58 | gated-checkpoint-disabled | 100% | claude-sonnet-4-6/claude-code | 38% | gpt-5.6-luna/opencode | 62% |
| infrastructure-triggers | 59 | interval-overdue | 100% | gpt-5.6-sol/opencode | 42% | gpt-5.6-terra/opencode | 58% |
| infrastructure-triggers | 60 | interval-precise-boundary | 100% | claude-sonnet-4-6/claude-code | 55% | gpt-5.6-luna/opencode | 45% |
| infrastructure-triggers | 61 | multiple-triggers-same-boundary | 100% | claude-sonnet-4-6/claude-code | 37% | gpt-5.6-luna/opencode | 63% |
| infrastructure-triggers | 62 | phase-end-trigger | 100% | claude-sonnet-4-6/claude-code | 45% | gpt-5.6-luna/opencode | 55% |
| infrastructure-triggers | 63 | restore-class-exclusion | 100% | claude-sonnet-4-6/claude-code | 38% | gpt-5.6-luna/opencode | 62% |
| infrastructure-triggers | 64 | stage-end-checkpoint | 100% | claude-sonnet-4-6/claude-code | 42% | gpt-5.6-luna/opencode | 58% |
| route-back | 66 | contracts-routeback-quality-gate | 42% | gpt-5.6-terra/opencode | 0% | gpt-5.6-luna/opencode | 42% |
| status-routing | 68 | blocked-e101-retry | 100% | claude-sonnet-4-6/claude-code | 58% | gpt-5.6-luna/opencode | 42% |
| status-routing | 69 | blocked-e501-retry | 100% | claude-sonnet-4-6/claude-code | 49% | gpt-5.6-luna/opencode | 51% |
| status-routing | 70 | blocked-e503-hitl-retry | 15% | gpt-5.6-luna/opencode | 0% | claude-sonnet-4-6/claude-code | 15% |
| status-routing | 71 | capability-exceeded-escalate | 100% | gpt-5.6-sol/opencode | 82% | claude-sonnet-4-6/claude-code | 18% |
| status-routing | 72 | creator-fix-rereview | 100% | claude-sonnet-4-6/claude-code | 14% | gpt-5.6-luna/opencode | 86% |
| status-routing | 73 | findings-route-back | 88% | gpt-5.6-sol/opencode | 0% | gpt-5.6-luna/opencode | 88% |
| status-routing | 74 | needs-clarification-no-advance | 100% | claude-sonnet-4-6/claude-code | 15% | gpt-5.6-luna/opencode | 85% |
| status-routing | 75 | partially-done-redispatch | 100% | claude-sonnet-4-6/claude-code | 52% | gpt-5.6-luna/opencode | 48% |
| wildcard-expansion | 76 | wildcard-after-routeback | 100% | gpt-5.6-sol/opencode | 2% | gpt-5.6-luna/opencode | 98% |
| wildcard-expansion | 77 | wildcard-dual-expansion | 100% | gpt-5.6-sol/opencode | 7% | gpt-5.6-luna/opencode | 93% |
| wildcard-expansion | 78 | wildcard-input-expansion | 99% | gpt-5.6-sol/opencode | 7% | gpt-5.6-luna/opencode | 92% |

<!-- /generated:problem-areas -->

<!-- analysis:overall-analysis -->
<!-- /analysis:overall-analysis -->
