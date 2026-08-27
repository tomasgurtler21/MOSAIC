# Runner Guide

How to use `mosaic-run` to execute MOSAIC orchestration workflows automatically.

This guide is for **project authors** who want to run multi-agent workflows without manually driving the orchestrator. The Runner reads a workflow definition, dispatches subagents through your AI coding tool, records results, and handles deviations — replacing the orchestrator's mechanical work while preserving its intelligent routing when needed.

---

## At a Glance

| Concept | Summary |
|---------|---------|
| **What it does** | Executes a MOSAIC workflow end-to-end: reads the workflow table, dispatches subagents, writes `Orchestration.md`, handles deviations |
| **Two ways to run** | Interactive TUI (no subcommand) or scriptable CLI (`mosaic-run run`) |
| **Three execution modes** | **Orchestrated** (orchestrator decides everything), **Auto** (engine routes happy path), **Auto-review** (engine also routes review loops) |
| **Supported harnesses** | `claude-code`, `opencode`, `ghcp-cli` |
| **State file** | `Orchestration-{run_id}/Orchestration.md` — atomic writes, crash-safe, resumable |

**Why not just use the orchestrator agent?** A persistent orchestrator session accumulates context with every dispatch — every tool call, artifact edit, and subagent response stays in the context window. Cost grows roughly quadratically with run length. The Runner offloads all mechanical work (artifact writes, sequence tracking, harness invocations) and keeps orchestrator calls bounded: each starts a fresh session reading only the compact `Orchestration.md`.

---

## Running a Workflow

### Interactive (TUI)

```sh
./mosaic-run
```

The TUI walks you through: harness selection (which auto-discovers the orchestrator script), workflow selection, mode configuration, and then shows live progress as the run executes.

### CLI

```sh
./mosaic-run run \
  --workflow quick-fix \
  --task "Fix the login timeout bug reported in issue #42" \
  --harness claude-code \
  --mode auto \
  --timeout 30m
```

The orchestrator script is discovered automatically from the harness-convention agents directory (e.g., `.claude/agents/orchestrator-script.md` for `claude-code`). If the workspace has not been deployed for the selected harness, the Runner exits immediately with a clear error.

---

## Execution Modes

The mode controls how much routing the Runner handles versus what it delegates to a script-mode orchestrator agent.

| Mode | Happy path routing | Deviation routing | Task description quality | Cost |
|------|-------------------|-------------------|-------------------------|------|
| **orchestrated** | Orchestrator decides every step | Orchestrator decides | High — orchestrator crafts each one | Lowest of all approaches (bounded context), but most orchestrator calls |
| **auto** | Engine follows workflow table | Orchestrator consulted | Generic on happy path, crafted on deviation | Fewer orchestrator calls |
| **auto-review** | Engine follows table + review loops | Orchestrator only for unresolvable deviations | Generic everywhere except deviations | Fewest orchestrator calls |

### Which mode to choose?

- **orchestrated** (recommended default) — Best quality. The orchestrator crafts targeted task descriptions for every dispatch. Each invocation is cheap (fresh session, bounded context).
- **auto** — Good when happy-path subagents perform well from their own instructions and input artifacts alone. Orchestrator still handles deviations.
- **auto-review** — Maximum automation. Use when review-loop routing is fully captured by the workflow table's On Findings column and subagents reliably self-direct.

All three modes are dramatically cheaper than running the orchestrator agent manually, because the Runner eliminates context window growth.

---

## Key CLI Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--workflow` | Yes | — | Workflow identifier (must exist in the orchestrator file) |
| `--task` | Yes | — | Task description for the run |
| `--harness` | Yes | `fake` | Harness adapter (`claude-code`, `opencode`, `ghcp-cli`) |
| `--mode` | Yes | — | Execution mode (`orchestrated`, `auto`, `auto-review`) |
| `--timeout` | No | `30m` | Per-invocation timeout for the harness adapter |
| `--pre-consult` | No | `false` | One-shot environment consultation at run start (auto/auto-review only) |
| `--manual-resolution` | No | `false` | Let the user resolve deviations interactively on consultation failure |
| `--checkpoints` | No | `disabled` | Checkpoint support (`disabled`, `enabled`) |
| `--commits` | No | `disabled` | Commit-class infrastructure dispatch (`disabled`, `enabled`) |
| `--commit-branch` | No | `mosaic-owned` | Commit branch variant (`mosaic-owned`, `user-own`) |
| `--run` | No | — | Resume a specific run by run_id |
| `--new-run` | No | `false` | Force creation of a new run |
| `--claude-path` | No | `claude` | Path to the Claude Code CLI binary |
| `--infra-class` | No | — | Non-interactive agent-per-class mappings (e.g. `checkpoint=checkpoint-manager-git,commit=commit-manager-git`) |

### Orchestrator Auto-Discovery

There is no `--orchestrator-file` flag. The Runner derives the orchestrator path from the `--harness` value using the harness-convention agents directory:

| Harness | Expected orchestrator path |
|---------|---------------------------|
| `claude-code` | `.claude/agents/orchestrator-script.md` |
| `opencode` | `.opencode/agents/orchestrator-script.md` |
| `ghcp-cli` | `.github/agents/orchestrator-script.md` |

If the file does not exist, the Runner exits immediately with a message indicating the workspace is not properly deployed for the selected harness. Run the Deploy tool to populate the agents directory before using the Runner.

### Agent Snapshot Directories

At run start, the Runner creates a run-scoped copy of the agents directory alongside the regular one (e.g., `.claude/agents-runner-{run_id}/`). This snapshot is used for all invocations and deleted automatically when the run completes or stops gracefully. If the Runner crashes mid-run, an orphaned `agents-runner-{run_id}/` directory may remain -- it is safe to delete manually and does not affect other runs.

---

## Pre-Consultation

An opt-in mechanism for **auto** and **auto-review** modes. At run start, the Runner invokes the orchestrator once to collect environment-level guidance (tool paths, project conventions, harness quirks) that gets appended to every subsequent dispatch.

```sh
./mosaic-run run \
  --mode auto \
  --pre-consult \
  ...
```

**What it fixes:** Subagents not knowing project-specific conventions (e.g., "use `py` not `python`", "skills are at `.claude/skills/`"). It does NOT fix the per-dispatch intelligence gap — that's the fundamental trade-off of auto modes.

**Not needed in orchestrated mode** — the orchestrator already includes environment context in every crafted dispatch.

---

## Resuming a Run

Every run creates an `Orchestration-{run_id}/` folder in the working directory. If a run stops (graceful stop, deviation, crash), resume it:

```sh
# Resume a specific run
./mosaic-run run --run 20260815T143000Z-a3f9 ...

# The TUI shows resumable runs at startup
./mosaic-run
```

The Runner reads the existing `Orchestration.md`, determines where the run left off, and continues from there.

---

## Checkpoints and Commits

### Checkpoints

When enabled, the Runner dispatches a checkpoint infrastructure agent at configured intervals to snapshot the working tree state.

```sh
--checkpoints enabled --infra-class "checkpoint=checkpoint-manager-git"
```

### Commits

When enabled, the Runner dispatches a commit infrastructure agent to commit completed stage work to the branch.

```sh
--commits enabled --infra-class "commit=commit-manager-git" --commit-branch mosaic-owned
```

| Branch variant | Behavior |
|---------------|----------|
| `mosaic-owned` | Runner manages a dedicated branch |
| `user-own` | Commits go to the user's current branch |

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Run completed successfully |
| 1 | Unexpected infrastructure error |
| 2 | Graceful stop (state saved, resumable) |
| 3 | Invalid arguments |
| 4 | Workflow or artifact refused before any invocation |
| 5 | Deviation unresolved (state saved, resumable) |
| 6 | Stopped by orchestrator consultant (resumable) |

---

## Quick Reference

| I want to... | Do this |
|--------------|---------|
| Run a workflow interactively | `mosaic-run` (no subcommand) |
| Run a workflow from CLI | `mosaic-run run --workflow <id> --task "<desc>" --harness claude-code --mode orchestrated` |
| Use the cheapest mode | `--mode auto-review` |
| Get the best task descriptions | `--mode orchestrated` |
| Add environment guidance in auto modes | `--pre-consult` |
| Resume a stopped run | `--run <run_id>` |
| Preview without a real harness | `--harness fake` |
| Enable checkpoints | `--checkpoints enabled --infra-class "checkpoint=checkpoint-manager-git"` |
| Enable stage commits | `--commits enabled --infra-class "commit=commit-manager-git"` |
| Set a longer timeout per invocation | `--timeout 1h` |
| Resolve deviations manually | `--manual-resolution` |
