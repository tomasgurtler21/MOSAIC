# Plan: Test Feature

## Overview
A feature with four stages and Approach column.

## Stages

| Stage | Name | Goal | Depends On | HITL | Approach |
|-------|------|------|------------|:----:|----------|
| 1 | Foundation | Build the base | - | FALSE | TDD |
| 2 | Core | Implement core logic | 1 | FALSE | Implementation-First |
| 3 | Extras | Add extra features | 2 | TRUE | Implementation-Only |
| 4 | Polish | Final polish | 3 | FALSE | Tests-Only |
