# Plan: MosaicTest Staged Pre-placed Plan

> ⚠️ **FIXTURE ARTIFACT** — pre-placed by hand before the run, not produced by a planner agent.
> This file exists so the runner has a stage table to read in a workflow with no pre-EXECUTION rows.

## Overview
Two stages of fixture work, so that stage progression can be distinguished from row progression in the execution log.

## Stages

| Stage | Name | Goal | Depends On | HITL | Approach |
|-------|------|------|------------|:----:|----------|
| 1 | First fixture stage | Run both EXECUTION rows once and write Stage-1/MosaicTestStage.md | - | ❌ | TDD |
| 2 | Second fixture stage | Run both EXECUTION rows again and write Stage-2/MosaicTestStage.md | 1 | ❌ | TDD |

## Unresolved Questions
<!-- Empty = plan is complete. -->

## Fixture Notes

The `Approach` column is populated deliberately even though `staged-preplaced-plan` is a **bare** workflow with no execution groups. A bare workflow must ignore this column entirely. Observing that both stages run their rows in table order — rather than doing anything TDD-shaped — is one of the assertions of this fixture.

`Depends On` is set on stage 2 so the dependency parser has something non-trivial to read. Numbering is consecutive from 1 and carries no forward dependencies, both of which the runner validates.
