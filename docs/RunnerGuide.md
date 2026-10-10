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

> **Known issue — GHCP CLI:** Runner runs on `ghcp-cli` are currently unreliable. With recent GHCP CLI versions (observed on 1.0.91), GHCP CLI sometimes ignores the requested `--agent` and runs its default agent instead, which shows up as protocol or parsing errors in the run. The failures are intermittent and the cause is not yet confirmed. In limited testing GHCP CLI 1.0.87 did not show the problem, so pinning it may help, but this is not verified. For reliable runs, use `claude-code` or `opencode`.

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
| **orchestrated** | Orchestrator decides every step | Orchestrator decides, including after `PARTIALLY_DONE` and `BLOCKED` with `E501` | High — orchestrator crafts each one | Lowest of all approaches (bounded context), but most orchestrator calls |
| **auto** | Engine follows workflow table | Orchestrator consulted, except for the automatic retries below | Generic on happy path, crafted on deviation | Fewer orchestrator calls |
| **auto-review** | Engine follows table + review loops | Orchestrator only for unresolvable deviations, with the same automatic retries | Generic everywhere except deviations | Fewest orchestrator calls |

**Automatic retries (auto and auto-review).** Two outcomes are re-dispatched by the Runner itself, without consulting the orchestrator:

- `PARTIALLY_DONE`: the same agent runs again on the same row, with its previous output added to its inputs and its previous status message added to its task. This happens up to 3 times in a row; after that the orchestrator is consulted.
- `BLOCKED` with error code `E501` (tool unavailable, which includes a harness failure): the same agent runs again, up to 3 attempts per row and stage since the last `SUCCESS` there; once the budget is spent the orchestrator is consulted.

Every other non-`SUCCESS` status still goes to the orchestrator, including `COMPLETED_NEEDS_ACTION` in auto mode. In auto-review mode a reviewer's `COMPLETED_NEEDS_ACTION` is routed back to its creator automatically until the review loop limit is reached, counted per reviewer, phase and stage since that reviewer's last `SUCCESS`. In orchestrated mode the orchestrator is consulted after every step and none of these shortcuts apply.

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
| `--pre-consult` | No | `true` | One-shot environment consultation at run start (auto/auto-review only); use `--pre-consult=false` to disable |
| `--manual-resolution` | No | `false` | When a consultation fails, let the user choose the next dispatch in an interactive dialogue; needs the TUI (see [Manual routing](#manual-routing)) |
| `--review-loop-limit` | New run | — | Maximum review loop iterations: a positive integer, or `none` for no limit. Required for a new run and fixed once the run exists |
| `--checkpoints` | No | `disabled` | Checkpoint support (`disabled`, `enabled`) |
| `--commits` | No | `disabled` | Commit-class infrastructure dispatch (`disabled`, `enabled`) |
| `--commit-branch` | No | `mosaic-owned` | Commit branch variant (`mosaic-owned`, `user-own`) |
| `--run` | No | — | Resume a specific run by run_id |
| `--new-run` | No | `false` | Force creation of a new run |
| `--executable-path` | No | _(per-harness default)_ | Executable path override for the harness selected by --harness; when absent, each harness uses its own default |
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

At run start, the Runner creates a snapshot or backup of agent files so it can apply harness-specific transforms (e.g., `mode: primary` for OpenCode) without permanently altering the deployed agents. The strategy used depends on how the harness loads agents:

**Copy-and-invoke (path-based harnesses -- Claude Code):** The Runner copies the agents directory to a run-scoped sibling directory (e.g., `.claude/agents-runner-{run_id}/`), applies transforms to the copies, and invokes agents from there. Originals are never modified. On completion, the copy is deleted automatically. If the Runner crashes mid-run, an orphaned `agents-runner-{run_id}/` directory may remain; it is safe to delete manually and does not affect other runs.

**Backup-and-transform (name-based harnesses -- OpenCode, GHCP CLI):** These harnesses always resolve agents by name from their canonical directory and cannot be redirected to a copy. Instead, the Runner copies originals to a shared backup directory (e.g., `.opencode/.agents-backup/`), transforms the originals in-place, and restores from backup on completion. Multiple concurrent runs share the same backup directory and coordinate via per-run lock files inside it.

**Automatic crash recovery:** If the Runner crashes mid-run while using backup-and-transform, it leaves a human-readable `RUNNER-RECOVERY.txt` marker inside the agents directory explaining what happened and providing step-by-step manual restore instructions. On the next run start, the Runner automatically detects and recovers any orphaned backup state. The `.txt` extension is used for the marker so that no harness indexes it as an agent definition.

---

## Pre-Consultation

A mechanism for **auto** and **auto-review** modes, on by default. At run start, the Runner invokes the orchestrator once to collect environment-level guidance (tool paths, project conventions, harness quirks) that gets appended to every subsequent dispatch. If this call fails, the run stops before any workflow agent runs and the run folder is kept; resuming retries it.

```sh
# Disable it
./mosaic-run run   --mode auto   --pre-consult=false   ...
```

**What it fixes:** Subagents not knowing project-specific conventions (e.g., "use `py` not `python`", "skills are at `.claude/skills/`"). It does NOT fix the per-dispatch intelligence gap — that's the fundamental trade-off of auto modes.

**Not needed in orchestrated mode** — the orchestrator already includes environment context in every crafted dispatch.

---

## Manual routing

Normally the orchestrator decides where a run goes when the workflow table does not. With `--manual-resolution` (or the matching TUI setting), a consultation that fails (for example an unusable orchestrator reply) hands that one decision to you instead of ending the run. From the TUI stop screen, **Manual dispatch** does the same once on demand.

The TUI asks you step by step: the **row** to run (or stop the run), the **stage** for a staged row, the **task** description, the **input** and **output** artifacts (the row's defaults are pre-checked, and you can add paths), optional **constraints**, and a **HITL** choice (default, on or off). Esc steps back one question. A dispatch that does not fit the workflow (for example a stage that is not in the plan) is explained and you are taken back to correct it.

The dialogue ends without dispatching anything, and the run stops with its state saved and resumable, when you press Esc at the first question, when the interface cannot ask questions, or when the dialogue gives up (after 3 rejected dispatches, or after 64 questions in one attempt). The non-interactive CLI cannot answer the dialogue at all: with `--manual-resolution` there, a failed consultation ends the run as a resumable stop (exit code 6) rather than dispatching or waiting for input.

---

## Resuming a Run

Every run creates an `Orchestration-{run_id}/` folder in the working directory. If a run stops (graceful stop, deviation, crash), resume it:

```sh
# Resume a specific run
./mosaic-run run --run 20260815T143000Z-a3f9 ...

# The TUI shows resumable runs at startup
./mosaic-run
```

The Runner reads the existing `Orchestration.md`, determines where the run left off, and continues from there. It finds the row and stage of the last step from the Execution Log, the same way it does while the run is live, so a resumed run continues where the live run would have. The counters behind the automatic retries and the review loop limit are read from the log too, so they carry over; only the guard against repeated identical orchestrator dispatches starts again from zero.

In the TUI you do not have to leave the program to continue. After a graceful stop, **Continue** on the done screen resumes the run in place; after an orchestrator stop, **Retry** and **Manual dispatch** on the stop screen do the same. All of them resume the existing run (a run whose `Orchestration.md` exists is never restarted as new) and the progress screen shows the history of what already ran, rebuilt from the Execution Log, rather than starting empty.

If the orchestrator keeps requesting a dispatch that the Runner's loop guard blocks (the same agent dispatched four times in a row on one row), the Runner asks the orchestrator again twice and then stops the run with its state saved, so you can continue it after looking at what happened.

When you press stop in the TUI while the orchestrator is deciding, a decision that finishes after the stop request is discarded rather than dispatched. The Runner tells you so, and the decision is made again on resume.

### Workflow table, rows and resume

- **Row column.** mosaic-deploy adds a first-column `Row` (1..N) to each workflow routing table it rebuilds into an orchestrator; authors never write it. The Runner accepts tables with or without the column and refuses a table whose `Row` numbers do not match row positions, for example after a hand edit.
- **WorkflowRow in the Execution Log.** Each workflow step records the table row it ran in a `WorkflowRow` column directly after `Stage` (`Seq | Agent | Phase | Stage | WorkflowRow | Status | Timestamp | Summary | Inputs | Checkpoint`). Infrastructure, out-of-band and ad-hoc steps record `-`. Live routing and resume identify the last row from this value for every row, staged or not, and stop and report when the agent or group at that row no longer matches. A staged row whose log entry has no stage is refused, with a message naming the row and the `Group.N` form to put in the `Stage` cell.
- **Error marker.** A `BLOCKED` row's `Summary` ends with the error code as a marker such as `[error:E501]`. The Runner reads the marker back to count `E501` attempts, so leave it in place if you edit the log by hand.
- **On Findings.** The target is the nearest row above the row that ran whose agent is the target. The row that ran is not a candidate, and group and stage boundaries are ignored. A target with no preceding row is treated as no target (deviation or escalation), so On Findings targets must sit above their reviewer.
- **Version drift.** A run is pinned to its workflow only by the `workflow_version` string, and the table is re-read on every start, including resume. The recorded-row check catches only edits that change the agent or group at a recorded row. Workflow authors must bump the workflow version whenever they edit a routing table; the Runner then refuses to resume older runs against the changed table unless version drift is explicitly allowed (`--allow-version-drift`).

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
| 2 | Stopped (graceful stop, or a stop by the Runner itself; state saved, resumable) |
| 3 | Invalid arguments |
| 4 | Workflow or artifact refused before any invocation |
| 5 | Deviation unresolved (state saved, resumable) |
| 6 | Stopped by orchestrator consultant, a failed consultation, or an ended manual routing dialogue (resumable) |
| 7 | Commit setup or pre-consultation failed after the run folder was created (kept; resuming retries it) |

---

## Quick Reference

| I want to... | Do this |
|--------------|---------|
| Run a workflow interactively | `mosaic-run` (no subcommand) |
| Run a workflow from CLI | `mosaic-run run --workflow <id> --task "<desc>" --harness claude-code --mode orchestrated` |
| Use the cheapest mode | `--mode auto-review` |
| Get the best task descriptions | `--mode orchestrated` |
| Skip the environment guidance in auto modes | `--pre-consult=false` |
| Resume a stopped run | `--run <run_id>` |
| Preview without a real harness | `--harness fake` |
| Enable checkpoints | `--checkpoints enabled --infra-class "checkpoint=checkpoint-manager-git"` |
| Enable stage commits | `--commits enabled --infra-class "commit=commit-manager-git"` |
| Set a longer timeout per invocation | `--timeout 1h` |
| Choose the next dispatch yourself when a consultation fails | `--manual-resolution` (needs the TUI) |
