# Version Summary: 7.4.1

<!-- generated:overview -->
## Overview

- **Version:** 7.4.1
- **Reports:** 25
- **Total Tests:** 11030
- **Suites:** execution-groups, hitl-gate, infrastructure-triggers, route-back, status-routing, wildcard-expansion
- **Models:** claude-opus-4-6, claude-sonnet-4-6, claude-sonnet-5, gpt-5.6-luna, gpt-5.6-sol, gpt-5.6-terra
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
| claude-sonnet-5 | 800 | 86% | $10.04/100t |
| gpt-5.6-luna | 2593 | 32% | $0.12/100t |
| gpt-5.6-sol | 2595 | 89% | $7.73/100t |
| gpt-5.6-terra | 2595 | 79% | $2.66/100t |

<!-- /generated:model-comparison -->

**IMPORTANT** - Harnes comparison is WIP, currently comparison does not make much sense

<!-- generated:harness-comparison -->
## Harness Comparison

| Harness | Tests | Pass Rate | Cost |
|---------|-------|-----------|------|
| claude-code | 3247 | 81% | $14.22/100t |
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

### claude-sonnet-5

| Suite | Harness | Tests | Pass Rate | Cost |
|-------|---------|-------|-----------|------|
| status-routing | claude-code | 800 | 86% | $10.04/100t |

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

<!-- analysis:overall-analysis -->
<!-- /analysis:overall-analysis -->
