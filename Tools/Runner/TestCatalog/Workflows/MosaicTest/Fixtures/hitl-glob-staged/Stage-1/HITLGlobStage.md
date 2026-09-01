---
human_approved: true
---

# MosaicTest HITL Glob Stage Artifact - Stage 1

> [WARN] **FIXTURE ARTIFACT** -- pre-placed with `human_approved: true` for HITL compliance demonstration.

This file is a seed-placed fixture. It exists so that when the runner's HITL approval check
expands the `Stage-*/HITLGlobStage.md` output artifact pattern to concrete paths, it finds
`Stage-1/HITLGlobStage.md` on disk with `human_approved: true` in its frontmatter.

No agent in the `hitl-glob-staged` workflow writes to this file. The `mosaictest-scripted`
stub skips writing to wildcard paths (paths containing `*`), and this file's path does not
contain a wildcard -- it is produced by glob expansion, not present in the row's raw output
artifact list. The file therefore retains its pre-placed `human_approved: true` value for the
duration of the run.

The content is fixture data and means nothing beyond its role as an approval-check target.
