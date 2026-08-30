# Version Summary: 7.4.1

<!-- generated:overview -->
## Overview

- **Version:** 7.4.1
- **Reports:** 18
- **Total Tests:** 7238
- **Suites:** execution-groups, hitl-gate, infrastructure-triggers, route-back, status-routing, wildcard-expansion
- **Models:** claude-sonnet-4-6, gpt-5.6-luna, gpt-5.6-sol, gpt-5.6-terra
- **Harnesses:** claude-code, opencode

<!-- /generated:overview -->

<!-- generated:model-comparison -->
## Model Comparison

| Model | Tests | Pass Rate | Cost |
|-------|-------|-----------|------|
| claude-sonnet-4-6 | 1451 | 87% | $13.55/100t |
| gpt-5.6-luna | 2593 | 32% | $0.12/100t |
| gpt-5.6-sol | 599 | 99% | $2.44/100t |
| gpt-5.6-terra | 2595 | 79% | $2.66/100t |

<!-- /generated:model-comparison -->

<!-- generated:harness-comparison -->
## Harness Comparison

| Harness | Tests | Pass Rate | Cost |
|---------|-------|-----------|------|
| claude-code | 1451 | 87% | $13.55/100t |
| opencode | 5787 | 60% | $1.50/100t |

<!-- /generated:harness-comparison -->

<!-- generated:model-results -->
## Model Results

### claude-sonnet-4-6

| Suite | Harness | Tests | Pass Rate | Cost |
|-------|---------|-------|-----------|------|
| execution-groups | claude-code | 300 | 100% | $7.63/100t |
| hitl-gate | claude-code | 300 | 100% | $6.60/100t |
| infrastructure-triggers | claude-code | 651 | 99% | $17.73/100t |
| route-back | claude-code | 200 | 8% | $19.28/100t |

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
| infrastructure-triggers | 59 | interval-overdue | 94% | claude-sonnet-4-6/claude-code | 42% | gpt-5.6-terra/opencode | 52% |
| infrastructure-triggers | 60 | interval-precise-boundary | 100% | claude-sonnet-4-6/claude-code | 55% | gpt-5.6-luna/opencode | 45% |
| infrastructure-triggers | 61 | multiple-triggers-same-boundary | 100% | claude-sonnet-4-6/claude-code | 37% | gpt-5.6-luna/opencode | 63% |
| infrastructure-triggers | 62 | phase-end-trigger | 100% | claude-sonnet-4-6/claude-code | 45% | gpt-5.6-luna/opencode | 55% |
| infrastructure-triggers | 63 | restore-class-exclusion | 100% | claude-sonnet-4-6/claude-code | 38% | gpt-5.6-luna/opencode | 62% |
| infrastructure-triggers | 64 | stage-end-checkpoint | 100% | claude-sonnet-4-6/claude-code | 42% | gpt-5.6-luna/opencode | 58% |
| route-back | 66 | contracts-routeback-quality-gate | 42% | gpt-5.6-terra/opencode | 0% | gpt-5.6-luna/opencode | 42% |
| status-routing | 68 | blocked-e101-retry | 93% | gpt-5.6-terra/opencode | 58% | gpt-5.6-luna/opencode | 35% |
| status-routing | 69 | blocked-e501-retry | 99% | gpt-5.6-terra/opencode | 49% | gpt-5.6-luna/opencode | 49% |
| status-routing | 70 | blocked-e503-hitl-retry | 15% | gpt-5.6-luna/opencode | 9% | gpt-5.6-terra/opencode | 6% |
| status-routing | 71 | capability-exceeded-escalate | 99% | gpt-5.6-luna/opencode | 97% | gpt-5.6-terra/opencode | 2% |
| status-routing | 72 | creator-fix-rereview | 100% | gpt-5.6-terra/opencode | 14% | gpt-5.6-luna/opencode | 86% |
| status-routing | 73 | findings-route-back | 56% | gpt-5.6-terra/opencode | 0% | gpt-5.6-luna/opencode | 56% |
| status-routing | 74 | needs-clarification-no-advance | 99% | gpt-5.6-terra/opencode | 15% | gpt-5.6-luna/opencode | 84% |
| status-routing | 75 | partially-done-redispatch | 99% | gpt-5.6-terra/opencode | 52% | gpt-5.6-luna/opencode | 47% |
| wildcard-expansion | 76 | wildcard-after-routeback | 94% | gpt-5.6-terra/opencode | 2% | gpt-5.6-luna/opencode | 92% |
| wildcard-expansion | 77 | wildcard-dual-expansion | 99% | gpt-5.6-terra/opencode | 7% | gpt-5.6-luna/opencode | 92% |
| wildcard-expansion | 78 | wildcard-input-expansion | 98% | gpt-5.6-terra/opencode | 7% | gpt-5.6-luna/opencode | 91% |

<!-- /generated:problem-areas -->

<!-- analysis:overall-analysis -->
<!-- /analysis:overall-analysis -->
