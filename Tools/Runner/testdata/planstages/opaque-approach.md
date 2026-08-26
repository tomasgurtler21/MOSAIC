# Plan: Opaque Approach Token Test

## Overview
A stage plan using workflow-defined, opaque approach tokens that are not in any
fixed recognised set. Verifies that planstages.ReadStages accepts arbitrary tokens verbatim.

## Stages

| Stage | Name | Goal | Depends On | HITL | Approach |
|-------|------|------|------------|:----:|----------|
| 1 | Foundation | First stage | - | FALSE | CustomFlow |
| 2 | Extension | Second stage | 1 | TRUE | CustomFlow |
