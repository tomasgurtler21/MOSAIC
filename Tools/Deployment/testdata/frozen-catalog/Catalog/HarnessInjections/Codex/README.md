# Frozen Codex Injection Content

This directory exists so tests can construct a usable Codex harness module over the
frozen-catalog root. Without it, every test that calls `codex.New(opts)` with
`opts.MosaicRoot` pointing at the frozen catalog would fail with "failed to initialise
built-in" because `injectionfile.LoadDir` requires both files to be present.

## What this is

Frozen fixture content for the deployment tool's test suite. It is NOT the live
catalog content for the Codex harness; that is authored in a later stage. This directory
is a stable, minimal snapshot that gives the injectionfile loader something valid to read.

## What this is not

This is not the authoritative Codex harness content. Do NOT update these files in step
with the live `Catalog/HarnessInjections/Codex/` tree. Their purpose is stability:
they exist so the test suite compiles and runs between this stage and the catalog stage,
when no live Codex content exists yet.

## Relationship to other test fixtures

- `internal/harness/builtin/codex/testdata/` -- module-local fixtures for the codex
  package's own unit tests (T5.6). These have specific content matched to constants in
  `codex_test.go`. Do not consolidate them with this directory.
- `testdata/frozen-catalog/Catalog/HarnessInjections/*/` (other harnesses) -- sibling
  directories for claude-code, ghcp-cli, opencode, vscode-ghcp. The golden regression
  test (T5.7) verifies those four harnesses are byte-identical after the descriptor
  schema changes; this directory's presence does not affect that test.

## Why adding here is safe

Each harness's injection content directory is addressed by its own named path constant
(`HarnessContentDirCodex`). The `HarnessInjections/` tree is never enumerated to
discover harnesses. A fifth directory therefore cannot change what any of the four
existing harnesses load, cannot change their deployed output, and cannot perturb the
seam baseline.
