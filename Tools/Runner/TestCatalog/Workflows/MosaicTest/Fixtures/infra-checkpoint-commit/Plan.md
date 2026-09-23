# Plan: MosaicTest Infrastructure Checkpoint+Commit

> [WARN] **FIXTURE ARTIFACT** — pre-placed by hand before the run, not produced by a planner agent.
> This file exists so the runner has a stage table to read in a workflow with no pre-EXECUTION rows.

## Overview
Two stages of fixture work. Two stages are needed so that STAGE_END fires mid-run (at the stage 1/2 boundary), not only at run completion.

## Stages

| Stage | Name | Goal | Depends On | HITL | Approach |
|-------|------|------|------------|:----:|----------|
| 1 | First infrastructure stage | Run the EXECUTION row once and trigger STAGE_END for checkpoint and commit | - | FALSE | direct |
| 2 | Second infrastructure stage | Run the EXECUTION row again and trigger STAGE_END for checkpoint and commit | 1 | FALSE | direct |

## Unresolved Questions
<!-- Empty = plan is complete. -->

## Fixture Notes

The `Approach` column is `direct` because this is a bare workflow with no execution groups. A bare workflow must ignore the Approach column entirely; a value is present to guard against a regression where bare workflows start honouring approaches.
