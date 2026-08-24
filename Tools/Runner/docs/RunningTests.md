# Running MosaicTest Suite

Quick-start guide for running the harness conformance test suite via CLI.

## Prerequisites

- A deployed test workspace with all three harnesses (claude-code, opencode, ghcp-cli). The current workspace is `C:\AI\MOSAIC\script runner test`.
- `mosaic-run.exe` in that workspace.
- Fixture seed paths from `Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\README.md`.

## CLI Command

```
cd "<test-workspace>"
./mosaic-run.exe run \
  --orchestrator-file "<harness-orchestrator-path>" \
  --workflow <workflow-id> \
  --task "<any description>" \
  --mode <mode> \
  --harness <harness-id> \
  --new-run \
  --input "<absolute-path-to-fixture-seed-folder>"
```

## Harness-Specific Values

| Harness | `--harness` | `--orchestrator-file` |
|---------|-------------|----------------------|
| Claude Code | `claude-code` | `.claude/agents/orchestrator-script.md` |
| OpenCode | `opencode` | `.opencode/agents/orchestrator-script.md` |
| GHCP CLI | `ghcp-cli` | `.github/agents/orchestrator-script.md` |

## Workflow Modes

Each workflow has a required mode. Using the wrong mode causes failures (e.g. running an auto-only workflow in orchestrated mode triggers orchestrator consultation with no routing fixture).

| Workflow | Mode(s) | Fixture Seed Folder |
|----------|---------|-------------------|
| `smoke-single` | `auto`, `auto-review` | `Fixtures/smoke-single` |
| `payload-stress` | `auto`, `auto-review` | `Fixtures/payload-stress` |
| `staged-preplaced-plan` | `auto`, `auto-review` | `Fixtures/staged-preplaced-plan` |
| `orchestrated-linear` | `orchestrated` | `Fixtures/orchestrated-linear` |
| `orchestrated-backjump` | `orchestrated` | `Fixtures/orchestrated-backjump` |
| `findings-loop` | `auto` **and** `auto-review` (run twice) | `Fixtures/findings-loop` |
| `deviation-blocked` | `auto` | `Fixtures/deviation-blocked` |
| `deviation-ambiguous` | `auto-review` | `Fixtures/deviation-ambiguous` |
| `deviation-stop` | `auto` | `Fixtures/deviation-stop` |

Fixture seed folders live under `Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\`. Use absolute paths for `--input`.

## Example: smoke-single on all three harnesses

```bash
cd "C:/AI/MOSAIC/script runner test"
FIXTURES="C:/AI/MOSAIC/MOSAIC/Tools/Runner/TestCatalog/Workflows/MosaicTest/Fixtures"

# Claude Code
./mosaic-run.exe run --orchestrator-file .claude/agents/orchestrator-script.md \
  --workflow smoke-single --task "Smoke test" --mode auto \
  --harness claude-code --new-run --input "$FIXTURES/smoke-single"

# OpenCode
./mosaic-run.exe run --orchestrator-file .opencode/agents/orchestrator-script.md \
  --workflow smoke-single --task "Smoke test" --mode auto \
  --harness opencode --new-run --input "$FIXTURES/smoke-single"

# GHCP CLI
./mosaic-run.exe run --orchestrator-file .github/agents/orchestrator-script.md \
  --workflow smoke-single --task "Smoke test" --mode auto \
  --harness ghcp-cli --new-run --input "$FIXTURES/smoke-single"
```

## Checking Results

**Dispatch logs** (`DispatchLogs/<run_id>.log`): JSONL with request/response pairs at the protocol level. Best for quick pass/fail assessment.

**Runner logs** (`RunnerLogs/<run_id>.log`): Full harness I/O including raw stdout/stderr. Use for diagnosing parser or harness adapter bugs.

**Orchestration artifact** (`Orchestration-<run_id>/Orchestration.md`): The execution log table. Compare against the expected run table in the workflow document.

## Smoke Set

Run these 4 to cover all three modes plus orchestrator consultation:

1. `smoke-single` (auto) — does the harness work at all?
2. `orchestrated-linear` (orchestrated) — mode 1 + orchestrator end-to-end
3. `findings-loop` (auto) — mode 2
4. `findings-loop` (auto-review) — mode 3
