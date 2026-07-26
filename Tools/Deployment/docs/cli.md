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
