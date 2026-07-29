# Plan: No Approach Column

## Overview
A plan with two stages but no Approach column. Valid for single-group workflows,
but invalid when needsApproach is true.

## Stages

| Stage | Name | Goal | Depends On | HITL |
|-------|------|------|------------|:----:|
| 1 | First | First stage | - | ❌ |
| 2 | Second | Second stage | 1 | ❌ |
