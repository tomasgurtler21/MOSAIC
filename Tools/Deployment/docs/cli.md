# mosaic-deploy CLI Reference

The `mosaic-deploy` tool provides two modes: an interactive TUI (when no subcommand is given and a terminal is attached) and a non-interactive CLI (when a subcommand is provided). This document covers the CLI.

## Design decision: multi-valued selections via a file

Multi-valued selections — workflows, utility agents, hooks, and per-tier model mappings — are expressed via a **YAML selections file** supplied with `--selections <path>` rather than as repeated flags.

**Rationale:** Repeated flags grow unmanageable for batch multi-workspace scripting. A selections file is stable, shareable, and version-controllable: a team can commit one canonical `selections.yaml` per project and pass it to every invocation. Repeated flags, by contrast, produce long and fragile shell one-liners. Single-valued flags (`--harness`, `--workspace`, `--scope`) remain as flags for ergonomics on one-off invocations.

---

## Global flags

These flags are accepted by all subcommands.

| Flag | Type | Description |
|------|------|-------------|
| `--verbose` | bool | Enable verbose progress output |
| `--mosaic-root <path>` | string | Override the MOSAIC root directory |
| `--allow-external` | bool | Enable external harness modules (opt-in for security) |

---

## `deploy` subcommand

Deploy MOSAIC agents and skills into a new workspace.

```
mosaic-deploy deploy [flags]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--harness <id>` | string | — | Harness ID to deploy (e.g. `claude-code`) |
| `--workspace <path>` | string | — | Absolute path to the target workspace |
| `--scope <project\|user>` | string | `project` | Deployment scope |
| `--selections <path>` | string | — | Path to a YAML selections file |
| `--output <json>` | string | — | Machine-readable output format |
| `--dry-run` | bool | false | Plan without writing any files |
| `--auto-confirm` | bool | false | Skip plan review and proceed automatically |

### Selections file format

```yaml
# selections.yaml
workflows:
  - quick-fix
  - build
utility_agents:
  - code-reviewer
hooks:
  - pre-commit
tier_models:
  HIGH: claude-opus-4
  LOW: claude-haiku-3
```

Unknown keys in the selections file are silently ignored (forward-compatible).

---

## `update` subcommand

Update an existing workspace deployment to the latest agent versions.

```
mosaic-deploy update [flags]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--harness <id>` | string | — | Harness ID |
| `--workspace <path>` | string | — | Absolute path to the existing workspace |
| `--conflict <skip\|overwrite\|backup>` | string | `skip` | How to handle locally-modified files |
| `--output <json>` | string | — | Machine-readable output format |
| `--dry-run` | bool | false | Plan without writing any files |
| `--auto-confirm` | bool | false | Skip plan review |

The `--conflict` flag controls what happens when a file in the workspace has been locally modified since the last deployment:
- `skip` (default): leave the file unchanged and record a TODO item
- `overwrite`: replace the file with the latest version (local edits are lost)
- `backup`: copy the local file to `<name>.backup` before overwriting

---

## `transform` subcommand

Convert one or more already-deployed agent files from one harness to another.

```
mosaic-deploy transform [flags]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--harness <id>` | string | — | Source harness ID — the harness the input files were deployed for (required) |
| `--target-harness <id>` | string | — | Target harness ID — the harness to transform into (required) |
| `--path <path>` | string | — | Agent file or directory of agent files to transform (required) |
| `--target-model <id>` | string | — | Target harness model identifier; used as fallback for source models not named in `--model-map`; absent means model field is left empty |
| `--model-map <src=tgt>` | string (repeatable) | — | Map a source model to a target model; repeat per source model; unmapped sources fall back to `--target-model` |
| `--overwrite` | bool | false | Replace an existing destination file |
| `--dry-run` | bool | false | Compute and report outcomes without writing any file |
| `--output <json>` | string | — | Machine-readable output format |

`--path` is quote-stripped via the shared path-input helper, so shell-quoted paths (e.g. from drag-and-drop) work without manual editing.

### Input and enumeration

`--path` may name a single agent file or a directory of agent files. When a directory is given, enumeration is **non-recursive**: only regular files directly under the given directory are considered; subdirectories are not descended into. Files whose extension does not match the source harness's agent extension are silently excluded.

The destination path for each transformed file is resolved by the target harness's own path-resolution logic, relative to the workspace (project) root. The service derives the workspace root from the source file's directory by stripping the source harness's declared agents directory suffix (e.g. `.claude/agents`) — so output files land at `<workspace-root>/<target-harness-relative-path>` (e.g. `<workspace>/.opencode/agents/foo.opencode.md`), not nested inside the source harness's own directory. Use `--dry-run` to preview destination paths before committing.

### Per-file outcomes

Each file in the input is processed independently. A file that does not match the source harness, or is not a MOSAIC agent at all, is recorded as a skip and processing continues. The outcome report lists every file with one of four statuses:

| Status | Meaning |
|--------|---------|
| `transformed` | Output produced at `destinationPath` (or computed, under `--dry-run`) |
| `skipped-mismatch` | File's detected harness does not match `--harness` |
| `skipped-not-agent` | File is not a transformed MOSAIC agent |
| `failed` | File matched but could not be transformed or written (includes refused-overwrite) |

No destination file is overwritten without `--overwrite`; an existing destination without the flag produces a `failed` entry for that file and the batch continues.

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | All considered files transformed successfully |
| 1 | An unrecoverable error (unresolvable harness, unreadable path, user cancellation) |
| 3 | Invalid arguments (missing required flag) |

### JSON output

With `--output json`, a single `TransformHarnessResult` JSON document is written to stdout containing `sourceHarnessId`, `targetHarnessId`, `inputPath`, `inputIsDirectory`, `dryRun`, the per-file `files` array, and the summary counts (`transformed`, `skippedMismatch`, `skippedNotAgent`, `failed`). The exit code is unchanged.

### Example

```sh
# Dry-run to preview which files would be transformed and where output would land.
mosaic-deploy transform \
  --harness claude-code \
  --target-harness opencode \
  --path /path/to/agents/ \
  --dry-run

# Transform a directory, replacing any existing destination files.
mosaic-deploy transform \
  --harness claude-code \
  --target-harness opencode \
  --path /path/to/agents/ \
  --overwrite \
  --output json
```

---

## `utility-infra` subcommand

Deploy only Utility and Infrastructure agents and the skills they require. Workflow selection, hook configuration, and orchestrator rewriting are never performed.

```
mosaic-deploy utility-infra [flags]
```

### When to use this subcommand

Use `utility-infra` when you want to add or refresh utility and infrastructure agents in an existing workspace without touching the orchestrator or any workflow configuration. It is the narrowest deploy operation: it asks only the two agent-group questions and writes only the files those agents require.

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--harness <id>` | string | — | Harness ID (required) |
| `--workspace <path>` | string | — | Absolute path to the target workspace (required) |
| `--utility <ids>` | string | — | Comma-separated utility agent IDs; absent = ask interactively; empty string = none |
| `--infrastructure <ids>` | string | — | Comma-separated infrastructure agent IDs; absent = ask interactively; empty string = none |
| `--conflict <skip\|overwrite\|backup>` | string | `skip` | How to handle locally-modified files |
| `--output <json>` | string | — | Machine-readable output format |
| `--dry-run` | bool | false | Compute and report without writing any file |
| `--auto-confirm` | bool | false | Auto-confirm the deployment plan without prompting |

`--workspace` is quote-stripped via the shared path-input helper, so shell-quoted paths (e.g. from drag-and-drop) work without manual editing.

### Nil vs empty convention for `--utility` and `--infrastructure`

The two agent-ID flags follow the CD-6 nil/empty convention used by all pre-answer flags in this tool:

- **Flag absent** — the tool asks the question interactively (or records a TODO if no terminal is available).
- **Flag present with a value** — the flag's value is split on commas; the resulting list (which may be empty) pre-answers the selection without prompting.

This means `--utility ""` explicitly selects no utility agents and skips the prompt, while omitting `--utility` entirely leaves the choice to the interactive session.

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | All selected agents deployed successfully |
| 1 | An unrecoverable error occurred |
| 2 | Run completed but some items were skipped; see the TODO file |
| 3 | Invalid arguments (missing required flag or unknown flag value) |

### JSON output

With `--output json`, a single `RunSummary` JSON document is written to stdout containing at least `Mode`, `Outcome`, and `WorkspacePath`. The exit code is unchanged. A document is written even when `Outcome` is `"failed"`.

### Examples

```sh
# Interactive: ask which utility and infrastructure agents to deploy.
mosaic-deploy utility-infra \
  --harness claude-code \
  --workspace /path/to/my-project

# Non-interactive: pre-answer both selections; no prompts are shown.
mosaic-deploy utility-infra \
  --harness claude-code \
  --workspace /path/to/my-project \
  --utility "code-reviewer,test-runner" \
  --infrastructure "docker-manager" \
  --auto-confirm

# Explicitly deploy no utility agents, ask about infrastructure.
mosaic-deploy utility-infra \
  --harness claude-code \
  --workspace /path/to/my-project \
  --utility ""

# Dry run — compute the plan without writing anything.
mosaic-deploy utility-infra \
  --harness claude-code \
  --workspace /path/to/my-project \
  --utility "code-reviewer" \
  --infrastructure "" \
  --dry-run \
  --output json
```

---

## Exit codes

| Code | Name | Meaning |
|------|------|---------|
| 0 | success | All items deployed successfully |
| 1 | failure | An unrecoverable error occurred |
| 2 | completed-with-skips | Run completed but some items were skipped; see TODO file |
| 3 | usage | Invalid arguments or unrecognised subcommand |

---

## Machine-readable output (`--output json`)

When `--output json` is set, `mosaic-deploy` writes a single JSON document to stdout containing the full `RunSummary`. The exit code is unchanged. Scripting consumers should read from stdout and parse JSON; diagnostic messages go to stderr.

```sh
result=$(mosaic-deploy deploy --harness claude-code --workspace /path --auto-confirm --output json)
echo "$result" | jq '.Outcome'
```

The JSON object always contains at least `Mode`, `Outcome`, and `WorkspacePath`. A JSON document is written even when `Outcome` is `"failed"`, so consumers can inspect the failure reason programmatically.

---

## Unresolvable questions

When a question cannot be answered from the provided flags or the selections file, the CLI **skips the item and records a TODO entry** rather than blocking or applying a silent default. The run completes with exit code 2 (`completed-with-skips`). The TODO file written to the workspace root lists every item that needs manual attention.

This skip behavior is specific to the non-interactive CLI. The CLI never cancels a run mid-selection — it has no cancel keypress and never produces the abort-the-whole-run outcome that pressing Esc on a selection screen produces in the interactive TUI. A TUI user who wants to proceed with nothing selected for a given category presses Skip on that screen; a CLI user achieves the same outcome by omitting the selection from their `--selections` file. Both paths let the run continue with an empty selection and a TODO gap.

---

## Batch update example

The following shell script updates all workspaces listed in a file, using a shared selections file. It collects the JSON output from each run for post-processing.

```sh
#!/usr/bin/env bash
# batch-update.sh: update every workspace in workspaces.txt
# Usage: ./batch-update.sh workspaces.txt selections.yaml

set -euo pipefail

WORKSPACES_FILE="${1:?usage: batch-update.sh <workspaces.txt> <selections.yaml>}"
SELECTIONS="${2:?usage: batch-update.sh <workspaces.txt> <selections.yaml>}"
RESULTS_DIR="update-results-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$RESULTS_DIR"

while IFS= read -r workspace; do
  [[ -z "$workspace" || "$workspace" == \#* ]] && continue

  slug="${workspace//\//_}"
  echo "Updating: $workspace"

  mosaic-deploy update \
    --harness claude-code \
    --workspace "$workspace" \
    --conflict skip \
    --auto-confirm \
    --output json \
    > "$RESULTS_DIR/${slug}.json" || true

  outcome=$(jq -r '.Outcome' "$RESULTS_DIR/${slug}.json" 2>/dev/null || echo "unknown")
  echo "  outcome: $outcome"

done < "$WORKSPACES_FILE"

echo ""
echo "Results written to: $RESULTS_DIR/"
echo "Failures:"
grep -l '"failed"' "$RESULTS_DIR"/*.json 2>/dev/null || echo "  (none)"
```

Run it with:

```sh
chmod +x batch-update.sh
./batch-update.sh workspaces.txt selections.yaml
```

Where `workspaces.txt` contains one workspace path per line (blank lines and `#` comments ignored), and `selections.yaml` contains the shared workflow and model selections.
