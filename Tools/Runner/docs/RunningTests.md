# Running MosaicTest Suite

Quick-start guide for running the harness conformance test suite — both the automated path (`mosaic-run --dev test`) and the manual path (individual `mosaic-run run` commands).

## Automated Test Execution (Recommended)

The `test` subcommand automates the full deploy → seed → run → check cycle. It is gated behind the `--dev` flag.

### Prerequisites

- `mosaic-run.exe` built and available in the test workspace.
- A deployed test workspace with all three harnesses. The current workspace is `C:\AI\MOSAIC\script runner test`.
- The MOSAIC repo root (where `Tools/Runner/TestCatalog/` lives).

### Running the Smoke Set

```bash
cd "C:/AI/MOSAIC/script runner test"
./mosaic-run.exe --dev test \
  --catalog "C:/AI/MOSAIC/MOSAIC" \
  --suite smoke \
  --harness claude-code
```

### Running the Full Suite

```bash
./mosaic-run.exe --dev test \
  --catalog "C:/AI/MOSAIC/MOSAIC" \
  --suite full \
  --harness claude-code
```

### Running a Specific Workflow

```bash
./mosaic-run.exe --dev test \
  --catalog "C:/AI/MOSAIC/MOSAIC" \
  --workflow deviation-blocked --mode auto \
  --harness claude-code
```

### Running All Harnesses

Omit `--harness` to run against all CLI harnesses, or repeat the flag:

```bash
./mosaic-run.exe --dev test \
  --catalog "C:/AI/MOSAIC/MOSAIC" \
  --suite smoke \
  --harness claude-code --harness opencode --harness ghcp-cli
```

For GHCP CLI, add `--ghcp-permission-mode blanket` (or `allowlist`).

### Test Subcommand Flags

| Flag | Required | Description |
|------|----------|-------------|
| `--catalog` | Yes | Path to the MOSAIC repo root |
| `--suite` | One of suite/workflow | `smoke` or `full` |
| `--workflow` | One of suite/workflow | Specific workflow ID (repeatable); mutually exclusive with `--suite` |
| `--mode` | No | Execution mode filter; only valid with `--workflow` |
| `--harness` | No | Harness ID (repeatable); defaults to all CLI harnesses |
| `--ghcp-permission-mode` | When ghcp-cli | `blanket` or `allowlist` |

---

## Manual Test Execution

For workflows that cannot be automated, or for debugging a specific failure, use `mosaic-run run` directly.

### Prerequisites

- A deployed test workspace with all three harnesses (claude-code, opencode, ghcp-cli). The current workspace is `C:\AI\MOSAIC\script runner test`.
- `mosaic-run.exe` in that workspace.
- Fixture seed paths from `Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\README.md`.

### CLI Command

```
cd "<test-workspace>"
./mosaic-run.exe run \
  --workflow <workflow-id> \
  --task "<any description>" \
  --mode <mode> \
  --harness <harness-id> \
  --new-run \
  --input "<absolute-path-to-fixture-seed-folder>"
```

The orchestrator file is **auto-discovered** from the `--harness` value using each harness's agents directory convention (e.g., `opencode` → `.opencode/agents/orchestrator-script.md`). There is no `--orchestrator-file` flag.

## Harness Values

| Harness | `--harness` | Auto-Discovered Orchestrator |
|---------|-------------|------------------------------|
| Claude Code | `claude-code` | `.claude/agents/orchestrator-script.md` |
| OpenCode | `opencode` | `.opencode/agents/orchestrator-script.md` |
| GHCP CLI | `ghcp-cli` | `.github/agents/orchestrator-script.md` |

## Workflow Modes

Each workflow has a required mode. Using the wrong mode causes failures (e.g. running an auto-only workflow in orchestrated mode triggers orchestrator consultation with no routing fixture).

| Workflow | Mode(s) | Fixture Seed Folder | State |
|----------|---------|-------------------|-------|
| `smoke-single` | `auto`, `auto-review` | `Fixtures/smoke-single` | Implemented |
| `payload-stress` | `auto`, `auto-review`, `orchestrated` | `Fixtures/payload-stress` | Implemented |
| `staged-preplaced-plan` | `auto`, `auto-review`, `orchestrated` | `Fixtures/staged-preplaced-plan` | Implemented |
| `orchestrated-linear` | `orchestrated` | `Fixtures/orchestrated-linear` | Implemented |
| `orchestrated-backjump` | `orchestrated` | `Fixtures/orchestrated-backjump` | Implemented |
| `findings-loop` | `auto` **and** `auto-review` (run twice) | `Fixtures/findings-loop` | Implemented |
| `deviation-blocked` | `auto` | `Fixtures/deviation-blocked` | Implemented |
| `deviation-ambiguous` | `auto-review` | `Fixtures/deviation-ambiguous` | Implemented |
| `deviation-stop` | `auto` | `Fixtures/deviation-stop` | Implemented |
| `hitl-glob-staged` | `orchestrated` | `Fixtures/hitl-glob-staged` | Implemented |
| `infra-checkpoint-commit` | `auto` | `Fixtures/infra-checkpoint-commit` | Implemented |
| `infra-review-consult` | `auto` | `Fixtures/infra-review-consult` | Implemented |
| `preconsult-advice` | `auto` | `Fixtures/preconsult-advice` | Implemented |
| `staged-multigroup` | `auto` | `Fixtures/staged-multigroup` | Implemented |
| `deviation-chain` | `auto` | `Fixtures/deviation-chain` | Implemented |
| `hitl-escalate` | `auto` | `Fixtures/hitl-escalate` | Implemented |

Fixture seed folders live under `Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\`. Use absolute paths for `--input`.

## Example: smoke-single on all three harnesses

```bash
cd "C:/AI/MOSAIC/script runner test"
FIXTURES="C:/AI/MOSAIC/MOSAIC/Tools/Runner/TestCatalog/Workflows/MosaicTest/Fixtures"

# Claude Code
./mosaic-run.exe run --workflow smoke-single --task "Smoke test" --mode auto \
  --harness claude-code --new-run --input "$FIXTURES/smoke-single"

# OpenCode
./mosaic-run.exe run --workflow smoke-single --task "Smoke test" --mode auto \
  --harness opencode --new-run --input "$FIXTURES/smoke-single"

# GHCP CLI
./mosaic-run.exe run --workflow smoke-single --task "Smoke test" --mode auto \
  --harness ghcp-cli --new-run --input "$FIXTURES/smoke-single"
```

## Checking Results

**Dispatch logs** (`RunnerLogs/<run_id>/<run_id>-dispatch.log`): JSONL with request/response pairs at the protocol level. Best for quick pass/fail assessment.

**Runner logs** (`RunnerLogs/<run_id>/<run_id>.log`): Full harness I/O including raw stdout/stderr. Use for diagnosing parser or harness adapter bugs.

**Orchestration artifact** (`Orchestration-<run_id>/Orchestration.md`): The execution log table. Compare against the expected run table in the workflow document.

## Workflow Frontmatter Fields

Every workflow `.md` file in `Workflows/MosaicTest/` declares two machine-readable fields in its YAML frontmatter block:

**`modes`** (required, list of strings): The execution modes this workflow supports. Valid values: `auto`, `auto-review`, `orchestrated`. The test automation tool uses this to enumerate (workflow, mode) pairs. Running a workflow in a mode not declared here is unsupported and will produce unexpected behaviour.

**`smoke_set`** (optional, list of strings): The subset of `modes` entries that belong to the Smoke Set. Each entry must also appear in `modes`. When absent or empty, the workflow has no Smoke Set membership.

**`pre_consult`** (optional, boolean): Whether the pre-consultation path is exercised for this workflow's test runs. Valid values: `true`, `false`. When the field is absent the default is `true` (pre-consultation enabled). Set to `false` only for workflows where the pre-consultation step is intentionally skipped.

Example (from `smoke-single.md`):

```yaml
modes:
  - auto
  - auto-review
smoke_set:
  - auto
```

When authoring a new workflow, add both fields to its frontmatter and update this table. Use the mode vocabulary exactly as shown above (lowercase, hyphen-separated).

## Smoke Set

Run these 4 to cover all three modes plus orchestrator consultation:

1. `smoke-single` (auto) — does the harness work at all?
2. `orchestrated-linear` (orchestrated) — mode 1 + orchestrator end-to-end
3. `findings-loop` (auto) — mode 2
4. `findings-loop` (auto-review) — mode 3

Automated: `./mosaic-run.exe --dev test --catalog <repo> --suite smoke --harness <harness>`

---

## Manual-Only Test Procedures

These tests require process lifecycle control that `mosaic-run --dev test` cannot provide. They must be run manually.

### Resume After Mid-Run Interruption

**Purpose:** Verify that the Runner can resume a run after being killed mid-execution.

**Procedure:**

1. Start `orchestrated-linear` in orchestrated mode:
   ```bash
   ./mosaic-run.exe run --workflow orchestrated-linear --task "Resume test" \
     --mode orchestrated --harness <harness-id> --new-run \
     --input "$FIXTURES/orchestrated-linear"
   ```
2. Kill the process during execution (after at least one dispatch completes but before the run finishes). Use Ctrl+C or `kill`.
3. Note the run ID from the output (or find it in `RunnerLogs/`).
4. Resume:
   ```bash
   ./mosaic-run.exe run --workflow orchestrated-linear --task "Resume test" \
     --mode orchestrated --harness <harness-id> --run <run-id>
   ```
5. **Expected:** The run picks up where it left off. The execution log shows the sequence continuing from the interrupted point. If the last step was mid-flight (no completion logged), it is re-dispatched.

**What a failure looks like:**
- Run refuses to start with a version mismatch or artifact error
- Run restarts from the beginning instead of resuming
- Sequence numbers are duplicated or skip values
