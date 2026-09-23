# Deployment Guide

How to deploy, update, and manage MOSAIC agent workspaces using `mosaic-deploy`.

This guide is for **project authors** who want to get MOSAIC running in their AI coding tool workspace. It covers the main operations, essential configuration, and common tasks. For exhaustive flag-by-flag detail, see `Tools/Deployment/docs/cli.md`.

---

## At a Glance

| Operation | What it does | Command |
|-----------|-------------|---------|
| **Deploy** | Create a new workspace from scratch | `mosaic-deploy deploy` |
| **Update** | Bring an existing workspace to latest versions | `mosaic-deploy update` |
| **Update workflows** | Replace the workflow set without touching agents or hooks | `mosaic-deploy workflows` |
| **Deploy agents** | Deploy a chosen mix of agents without touching orchestrator or hooks | `mosaic-deploy agents` |
| **Deploy hooks** | Deploy hook bundles alone | `mosaic-deploy hooks` |
| **Transform** | Convert deployed agents from one harness to another | `mosaic-deploy transform` |
| **Promote** | Turn a harness-only agent into a reusable generic source | `mosaic-deploy promote` |

**Two ways to run:** Launch without a subcommand for the interactive TUI, or pass a subcommand for scriptable CLI mode.

**Built-in harnesses:** `claude-code`, `opencode`, `ghcp-cli`, `vscode-ghcp`.

---

## Deploy — New Workspace

Creates a workspace from scratch: selects harness, workflows, utility agents, and model assignments, then writes all files.

**Interactive:**

```sh
./mosaic-deploy
```

The TUI walks you through every step.

**CLI:**

```sh
./mosaic-deploy deploy \
  --harness claude-code \
  --workspace /path/to/my-project \
  --selections selections.yaml \
  --auto-confirm
```

**Selections file (`selections.yaml`):**

```yaml
workflows:
  - quick-fix
  - build
utility_agents:
  - code-reviewer
tier_models:
  HIGH: claude-opus-4-5
  LOW: claude-haiku-3-5
```

Add `--dry-run` to preview the plan without writing anything.

---

## Update — Existing Workspace

Compares deployed versions against the catalog, shows what's stale, and applies updates — preserving your `project` and `custom` region content.

```sh
./mosaic-deploy update \
  --harness claude-code \
  --workspace /path/to/my-project \
  --auto-confirm
```

### Conflict resolution

When a file was locally modified since the last deploy:

| `--conflict` value | What happens |
|-------------------|-------------|
| `skip` (default) | File untouched; added to TODO checklist |
| `overwrite` | Replaced with latest (local edits lost) |
| `backup` | Local file copied to `<name>.backup`, then overwritten |

---

## Update Workflows — Replace the Workflow Set

Rewrites only the orchestrator file with a new set of workflows. No agent, skill, or hook file is touched, even if stale. The selected workflow set **fully replaces** the currently deployed set — workflows you don't reselect are removed.

Use this to add, remove, or swap workflows without disturbing the rest of the deployment. If the new workflows require agents that aren't deployed yet, those agents are deployed automatically in the same run.

**Interactive:**

```sh
./mosaic-deploy
# → Select "Update workflows"
```

The TUI marks currently deployed workflows so you can see what you're keeping.

**CLI:**

```sh
./mosaic-deploy workflows \
  --harness claude-code \
  --workspace /path/to/my-project \
  --workflows "kb-generation,brownfield-tdd" \
  --auto-confirm
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--harness <id>` | string | — | Harness ID |
| `--workspace <path>` | string | — | Absolute path to the existing workspace |
| `--workflows <ids>` | string | — | Comma-separated workflow IDs (replaces the current set); absent = ask interactively |
| `--conflict <skip\|overwrite\|backup>` | string | `skip` | How to handle a locally-modified orchestrator file |
| `--dry-run` | bool | false | Plan without writing any files |
| `--auto-confirm` | bool | false | Skip plan review |
| `--output <json>` | string | — | Machine-readable output format |

---

## Deploy Agents — Add Individual Agents

Deploys a chosen mix of subagents, utility agents, infrastructure agents, and standalone agents plus the skills they require. No workflow, hook, or orchestrator work is performed.

Use this to add or update individual agents across any catalog category without disturbing workflows, hooks, or the orchestrator.

**Interactive:**

```sh
./mosaic-deploy
# → Select "Deploy agents"
```

The TUI presents a browsable list grouped by catalog category (Subagents, Utility Agents, Standalone Agents, etc.), mirroring the folder structure in the catalog.

**CLI:**

```sh
./mosaic-deploy agents \
  --harness claude-code \
  --workspace /path/to/my-project \
  --utility "injections-helper,code-reviewer" \
  --yes
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--harness <id>` | string | — | Harness ID (required) |
| `--workspace <path>` | string | — | Workspace directory (required) |
| `--subagents <ids>` | string | — | Comma-separated ordinary subagent IDs; absent = ask; empty = none |
| `--utility <ids>` | string | — | Comma-separated utility agent IDs; absent = ask; empty = none |
| `--infra <ids>` | string | — | Comma-separated infrastructure agent IDs; absent = ask; empty = none |
| `--standalone <ids>` | string | — | Comma-separated standalone agent IDs; absent = ask; empty = none |
| `--conflict <skip\|overwrite\|backup>` | string | `skip` | How to handle locally-modified files |
| `--dry-run` | bool | false | Plan without writing any files |
| `--yes` | bool | false | Auto-confirm the deployment plan |
| `--output <json>` | string | — | Machine-readable output format |

When any per-class flag (`--subagents`, `--utility`, `--infra`, `--standalone`) is given, the interactive question is skipped entirely and unspecified classes are treated as empty.

---

## Deploy Hooks — Add Hook Bundles

Deploys hook bundles alone. No agent, model, workflow, or orchestrator work is performed.

**Interactive:**

```sh
./mosaic-deploy
# → Select "Deploy hooks"
```

**CLI:**

```sh
./mosaic-deploy hooks \
  --harness claude-code \
  --workspace /path/to/my-project \
  --hooks "pre-commit" \
  --yes
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--harness <id>` | string | — | Harness ID (required) |
| `--workspace <path>` | string | — | Workspace directory (required) |
| `--hooks <ids>` | string | — | Comma-separated hook bundle IDs; absent = ask; empty = none |
| `--conflict <skip\|overwrite\|backup>` | string | `skip` | How to handle locally-modified files |
| `--dry-run` | bool | false | Plan without writing any files |
| `--yes` | bool | false | Auto-confirm the deployment plan |
| `--output <json>` | string | — | Machine-readable output format |

---

## Transform — Cross-Harness Conversion

Converts already-deployed agent files from one harness to another.

```sh
# Preview what would happen
./mosaic-deploy transform \
  --harness claude-code \
  --target-harness opencode \
  --path /path/to/agents/ \
  --dry-run

# Execute the transformation
./mosaic-deploy transform \
  --harness claude-code \
  --target-harness opencode \
  --path /path/to/agents/ \
  --overwrite
```

Map models between harnesses with `--model-map`:

```sh
--model-map "claude-opus-4-5=gpt-4o" --model-map "claude-haiku-3-5=gpt-4o-mini"
```

---

## Promote — Generic from Harness-Only

Turns a harness-only agent (one with no generic source) into a reusable generic agent in the catalog.

```sh
./mosaic-deploy promote \
  --file /path/to/harness-only-agent.md \
  --category Research
```

The source file is never modified. The generated generic has empty `project` and `managed` regions, ready for the normal deploy pipeline.

---

## Configuration

On first run, the tool creates a `MosaicDeploy/` directory next to the binary with default config files, logs, and an optional external harnesses folder. Two config files live in `MosaicDeploy/config/`:

### tool-config.yaml — Project-level (commit this)

```yaml
schema_version: "1"
utility_agent_allow_list:
  - code-reviewer
  - documentation-writer
allow_external_modules: false
log_retention_runs: 50
```

| Field | What it controls |
|-------|-----------------|
| `utility_agent_allow_list` | Which utility agents are selectable. `[]` = none. File absent = all six defaults. |
| `allow_external_modules` | Whether external harness modules in `MosaicDeploy/harnesses/` are allowed |
| `log_retention_runs` | Max history log entries (`0` = keep all) |
| `tool_destinations` | Permanent generic-tool to harness-tool mappings (silences repeated prompts) |

### user-config.yaml — Per-user (do NOT commit)

Auto-written when you confirm selections. Stores your tier-to-model mappings and custom model IDs so repeated runs are faster.

```yaml
schema_version: "1"
tier_models:
  claude-code:
    HIGH: claude-opus-4-5
    LOW: claude-haiku-3-5
custom_model_ids:
  claude-code:
    - claude-sonnet-4-5-20250929
```

> **Warning:** The tool rewrites this file on each run. YAML comments you add will not survive.

---

## The TODO File

After every deploy or update, the tool writes `MOSAIC-DEPLOYMENT-TODO-<timestamp>.md` to the workspace root. It lists:

- **Unfilled project regions** — slots you should fill for best agent performance
- **Skipped files** — locally modified files left unchanged
- **Unmapped tools** — generic tools without harness equivalents
- **Hook registration steps** — manual steps to complete

Work through this checklist before using the workspace. Filling project regions is the single highest-impact post-deployment action. See the [Agent Customization Guide](AgentCustomizationGuide.md) for how.

---

## Logs

| File | Content |
|------|---------|
| `MosaicDeploy/logs/latest.log` | Full log of the most recent run |
| `MosaicDeploy/logs/history.log` | Summary of all runs, newest first |

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Unrecoverable error |
| 2 | Completed with skips (check TODO file) |
| 3 | Invalid arguments |

---

## Troubleshooting

| Problem | Fix |
|---------|-----|
| "not a MOSAIC repository" | Run from inside the MOSAIC repo, or pass `--mosaic-root /path/to/MOSAIC` |
| "harness not found" | Check the ID (`claude-code`, `opencode`, `ghcp-cli`, `vscode-ghcp`). For custom descriptors, pass the full file path. |
| "external harness modules require explicit opt-in" | Set `allow_external_modules: true` in `tool-config.yaml`, or pass `--allow-external` |
| Config file not picked up | The tool looks in `MosaicDeploy/config/` under the MOSAIC root. Verify `--mosaic-root` if you moved the binary. |

---

## Quick Reference

| I want to... | Do this |
|--------------|---------|
| Deploy MOSAIC to a new project | `mosaic-deploy deploy --harness claude-code --workspace /path` |
| Update agents to latest versions | `mosaic-deploy update --harness claude-code --workspace /path` |
| Add or swap workflows in an existing workspace | `mosaic-deploy workflows --harness claude-code --workspace /path` |
| Add specific agents without touching orchestrator | `mosaic-deploy agents --harness claude-code --workspace /path` |
| Add hook bundles only | `mosaic-deploy hooks --harness claude-code --workspace /path` |
| Preview without writing files | Add `--dry-run` to any command |
| Keep my local edits as backups during update | `--conflict backup` |
| Convert agents to a different harness | `mosaic-deploy transform --harness X --target-harness Y --path /path` |
| Turn a one-off agent into a reusable generic | `mosaic-deploy promote --file /path/to/agent.md --category Category` |
| Use the interactive wizard | Just run `mosaic-deploy` with no subcommand |
| Script deployments for CI | Use `--auto-confirm --output json` |
| Control which utility agents are offered | Edit `utility_agent_allow_list` in `tool-config.yaml` |
| Check what happened in the last run | Read `MosaicDeploy/logs/latest.log` |
