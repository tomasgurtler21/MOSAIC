# Plan: MosaicTest Payload Stress

> ⚠️ **FIXTURE ARTIFACT** — pre-placed by hand before the run, not produced by a planner agent.

## Overview
A single stage, so each of the three EXECUTION rows runs exactly once and each payload class is probed once.

## Stages

| Stage | Name | Goal | Depends On | HITL | Approach |
|-------|------|------|------------|:----:|----------|
| 1 | Payload probes | Run the unicode, fenced-backtick, and JSON-in-JSON rows once each | - | ❌ | TDD |

## Unresolved Questions
<!-- Empty = plan is complete. -->

## Fixture Notes

One stage is deliberate. Repeating the stage would repeat identical payloads and add invocations without adding coverage — each payload class is already isolated in its own row.

The `Approach` column is populated but ignored: `payload-stress` is a bare workflow with no execution groups.
