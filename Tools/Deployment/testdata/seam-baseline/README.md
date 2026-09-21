# Seam Baseline

This directory holds the pre-seam regression baseline for all four existing harnesses
(claude-code, ghcp-cli, opencode, vscode-ghcp), captured before any pipeline code
changes.

## What it is

The seam stage inserts a decode step into the app layer's read funnel. After decode
is inserted, every deployed file read for an update goes through decode before the
transform sees it. For the four Markdown harnesses the decode step is the identity, so
the output must be byte-identical to what the unmodified pipeline produced. This
directory holds that "unmodified pipeline" record.

## What it contains

For each harness, two path types are captured:

- `create/` - output for an agent with no prior deployed file (nil Deployed in the
  transform request). One file per fixture agent.
- `update/` - output for an agent that does have a prior deployed file. Each update
  case stores two files:
    - `<agent>.output.md` - the transform output produced when Deployed = prior bytes
    - `<agent>.prior.md` - the prior deployed bytes used as input (Deployed field)

The update-path fixture demonstrates the full "read prior -> transform" cycle that
the seam stage inserts decode into. Byte-identity of `.output.md` after the seam
proves that decode is the identity for Markdown content.

## How it is used

The committed driver at `Tools/Deployment/internal/app/seam_baseline_test.go` both
writes this directory (with -update flag) and replays it (default mode). The seam
stage runs the driver in replay mode after inserting decode; a passing run proves
byte-identity.

## This directory is never regenerated

Once committed, the files in this directory must not be regenerated. They are the
pre-seam record. Regenerating them to make a comparison pass defeats the purpose.

The driver's -update flag may be used only for the initial capture (this stage) and
for any extension that adds new fixture cases. It must never be used to "fix" a
failing comparison.
