# Plan: MosaicTest HITL Glob Staged

> [WARN] **FIXTURE ARTIFACT** -- pre-placed by hand before the run, not produced by a planner agent.
> This file exists so the runner has a stage set to read when re-deriving stages after a row
> whose output artifacts contain a Stage-* wildcard pattern.

## Overview

Two fixture stages. The stage set is read from this file by the runner after the first dispatch
applies its Stage-* output artifact, populating the session's stage context. Subsequent glob
expansion uses this stage set to resolve Stage-* to Stage-1 and Stage-2 before reading HITL
approvals from the concrete per-stage files.

## Stages

| Stage | Name | Goal | Depends On | HITL | Approach |
|-------|------|------|------------|:----:|----------|
| 1 | Glob stage one | Provide Stage-1/HITLGlobStage.md as an approved artifact for HITL glob expansion | - | FALSE | - |
| 2 | Glob stage two | Provide Stage-2/HITLGlobStage.md as an approved artifact for HITL glob expansion | 1 | FALSE | - |

## Unresolved Questions
<!-- Empty = plan is complete. -->

## Fixture Notes

The `HITL` column is `FALSE` for both stages. This fixture's HITL check is driven by the
orchestrator's `hitl: true` dispatch override rather than by stage-level HITL, so no per-stage
HITL flag is needed. The stage table exists solely to give `expandStageGlobs` a concrete set of
stage numbers to substitute when resolving `Stage-*` in output artifact paths.

`Depends On` is set on stage 2 (depends on stage 1) so the dependency parser has a non-trivial
entry to read. Stage numbering is consecutive from 1 with no forward dependencies, consistent
with runner validation rules.
