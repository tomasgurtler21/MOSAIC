# Plan: MosaicTest Staged Findings Loop

> [WARN] **FIXTURE ARTIFACT** — pre-placed by hand before the run, not produced by a planner agent.
> This file exists so the runner has a stage table to read in a workflow with no pre-EXECUTION rows.

## Overview
One stage with two execution groups (Test, Implementation). The Test group holds a findings loop: the gate row returns COMPLETED_NEEDS_ACTION once and is routed back to the writer row.

## Stages

| Stage | Name | Goal | Depends On | HITL | Approach |
|-------|------|------|------------|:----:|----------|
| 1 | Only stage | Run the Test group with one findings loop, then the Implementation group | - | FALSE | TDD |

## Unresolved Questions
<!-- Empty = plan is complete. -->

## Fixture Notes

One stage is enough: the defect under test concerns row position within a stage.
