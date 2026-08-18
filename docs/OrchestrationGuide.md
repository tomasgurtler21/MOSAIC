# Orchestration Guide

How to start, configure, and manage MOSAIC orchestration runs.

This guide is for **project authors** who have a deployed MOSAIC workspace and want to run multi-agent workflows. It covers the startup sequence, configuration options, run isolation, and the infrastructure features that keep runs safe and auditable.

---

## At a Glance

| Concept | Summary |
|---------|---------|
| **What happens** | An orchestrator coordinates specialized subagents through a workflow: research, planning, design, execution, review |
| **Run folder** | Each run gets its own `Orchestration-{run_id}/` folder with all artifacts |
| **State file** | `Orchestration-{run_id}/Orchestration.md` tracks every dispatch, status, and artifact |
| **Two ways to start** | Directly via the orchestrator agent, or via `mosaic-run` (see [Runner Guide](RunnerGuide.md)) |
| **Parallel runs** | Multiple runs can execute simultaneously in separate folders |

---

## Starting a Run

### What You Need

1. **A deployed workspace** with an orchestrator agent (see [Deployment Guide](DeploymentGuide.md))
2. **A requirements document** (or at least a task description) describing what you want done
3. **A workflow choice** — which workflow to use (e.g., `brownfield-tdd`, `quick-fix`)

### The Startup Conversation

When you invoke the orchestrator, it asks for three required pieces of configuration before doing anything:

| Setting | Required? | What it means |
|---------|-----------|---------------|
| **Task** | Yes | What needs to be accomplished |
| **Workflow** | Yes | Which workflow to follow (orchestrator shows available options) |
| **Checkpoints** | Yes | Enable recovery checkpoints? `enabled` or `disabled` |
| **Commits** | Conditional | Commit each stage to git? Only asked if the deployment includes a commit agent |

The orchestrator will not proceed until Task, Workflow, and Checkpoints are explicitly specified. No silent defaults on these.

### Example Start

```
You:    Use brownfield-tdd workflow. Task: Implement the search feature 
        per Requirements.md. Checkpoints enabled.

Orchestrator: [creates run folder, adopts seed artifacts, begins workflow]
```

---

## Seed Artifacts

Your starting documents (requirements, specifications, briefs) were written before the run existed, so they live outside the run folder. The orchestrator handles this automatically:

1. **Copies** each seed artifact into `Orchestration-{run_id}/`, keeping its filename
2. **Leaves the original untouched** — your file is never moved or modified
3. **Registers the copy** in Orchestration.md as `Created By: user`
4. **References only the copy** in all dispatches from that point on

**Why this matters:** If a run fails or you abort it, your original file is exactly where you left it. You can start a fresh run from the same seed without any cleanup.

**What counts as a seed artifact:** Only orchestration artifacts (requirements docs, specs). Regular project files (source code, configs) are passed as `input_files` hints and stay where they are.

**When it's required:** If the workflow's first subagent expects an input artifact (typically `Requirements.md`), you must provide it as a seed. Without it, the first subagent will report `BLOCKED` with `E101 INPUT_NOT_FOUND` and the run won't start. Check the workflow table's Input column for the first step to see what's needed.

---

## Run Isolation

Every run creates its own folder:

```
Orchestration-20260817T193127Z-37d2/
  Orchestration.md          # State file (the "blackboard")
  Requirements.md           # Adopted seed artifact
  Research.md               # Created by codebase-research
  Plan.md                   # Created by planner
  ContractsDesign.md        # Created by contracts-designer
  Stage-1/
    Plan.md                 # Per-stage plan
    PlanProgress.md         # Per-stage progress tracker
    tests-review-tdd.md     # Review output
    implementation-review.md
  Stage-2/
    ...
```

The `run_id` format is `{YYYYMMDD}T{HHMMSS}Z-{4-char-hex}` (e.g., `20260817T193127Z-37d2`), making each folder unique.

### Parallel Runs

Multiple orchestration runs can execute simultaneously. Each lives in its own folder with its own `Orchestration.md`, sequence counter, and artifacts. They are completely independent.

**The caveat:** Parallel runs work smoothly as long as they don't modify the same project files. Two runs editing the same source file will conflict just like two developers editing the same file — the second one to write wins, and the first one's changes are lost. Plan accordingly: runs targeting different parts of the codebase parallelize cleanly; runs touching the same files should be sequential.

---

## Checkpoints

Checkpoints are recovery snapshots taken automatically during a run. When enabled, a checkpoint agent captures the state of the working tree at regular intervals.

### What They Do

- **Capture** a restorable snapshot at every stage boundary and periodically (e.g., every 10 invocations)
- **Record** a content-reference (git commit hash) in the Execution Log's `Checkpoint` column
- **Enable rollback** — if something goes wrong, you can tell the orchestrator to restore to a specific checkpoint

### When to Enable

- **Enable** for long runs, risky changes, or anything you'd want to undo partially
- **Disable** for quick tasks where starting over is cheaper than rolling back

### How Rollback Works

Rollback is always human-initiated — the orchestrator never rolls back on its own. If something goes wrong:

1. The orchestrator escalates to you
2. You pick a checkpoint from the Execution Log (each has a visible commit hash)
3. The orchestrator dispatches a restore agent to revert to that point
4. The run continues from there with a new sequence number (history is never rewritten)

**Important:** Checkpoints require a checkpoint-class agent in your deployment. If you enable checkpoints but your deployment doesn't include one, the orchestrator will tell you at startup and ask you to choose: run without checkpoints, or redeploy with one.

### Cost

Every checkpoint is an extra subagent dispatch — it consumes a sequence number and a full agent invocation. With the default triggers (every stage boundary + every 10 invocations), a 5-stage run with 26 workflow dispatches might add ~7 checkpoint dispatches on top. Factor this into your cost estimate for long runs.

---

## Commits

Commits are an optional mode where completed stages are committed to git as they finish. Unlike checkpoints (which live in a private namespace), commits go into real git history.

### When Commits Are Available

The orchestrator only asks about commits if your deployment declares a commit-class agent. If it doesn't, the feature simply doesn't exist for that deployment — no question asked, no configuration needed.

### Two Variants

If commits are enabled, you choose where they go:

| Variant | Branch | Recommended? |
|---------|--------|:------------:|
| **MOSAIC-owned** | A new branch created for this run (e.g., `mosaic/run/20260817T...`) | Yes |
| **User's own** | Your current branch | No |

**Why MOSAIC-owned is recommended:** If a stage needs to be redone, the abandoned attempt can be cleanly discarded on a run-owned branch. On your own branch, both the failed attempt and its fix stay in history permanently.

### What to Know

- Each stage boundary produces a commit with a message describing the completed work
- Any uncommitted changes of your own get swept into these commits (git can't tell whose changes are whose) — commit or stash your work before stages complete if you want it separate
- On a MOSAIC-owned branch, you integrate the results afterward (a squash merge lands the entire run as one clean commit)
- Like checkpoints, each commit is an extra subagent dispatch at every stage boundary — adds to the run's total cost

---

## Orchestration Review

If your deployment includes a review-class infrastructure agent, the orchestrator periodically audits its own bookkeeping. The review fires automatically at a configured interval (e.g., every 30 invocations) and checks:

- Is the Execution Log consistent with `current_state`?
- Are artifact registrations correct?
- Is routing following the declared workflow?

The review agent is advisory — it reports observations but doesn't change routing. If it finds issues, it records them; the run continues. Its `On Failure` policy is `continue`, so even if the review itself fails, the run isn't interrupted.

**Deployment choice:** The review agent is optional. If you don't deploy one, no reviews happen and the run proceeds without self-auditing. For short runs this is fine. For long runs (20+ invocations), deploying one catches bookkeeping drift early — at the cost of one extra subagent dispatch per interval.

---

## Resuming a Run

If a run is interrupted (crash, context loss, session break), you can resume it. The orchestrator:

1. Reads `Orchestration.md` to recover workflow state
2. Cross-checks `current_state` against the last Execution Log row
3. Validates the sequence counter
4. If in EXECUTION phase, reads progress artifacts to determine task state
5. Continues from where it left off

To resume, simply point the orchestrator at the existing `Orchestration-{run_id}/Orchestration.md`. It picks up from the last completed step.

**The conservative rule:** When uncertain about how much progress was made, the orchestrator assumes less rather than more. Re-running a step is safer than skipping one.

---

## Quick Reference

### Typical Run Lifecycle

```
1. You invoke orchestrator with task + workflow + checkpoint preference
2. Orchestrator creates Orchestration-{run_id}/ folder
3. Seed artifacts are copied in
4. Workflow executes: RESEARCH → PLANNING → DESIGN → EXECUTION → REVIEW
5. Each subagent dispatch gets a sequence number and Execution Log entry
6. Infrastructure agents (checkpoints, commits, review) fire at their triggers
7. Run completes or escalates to you on issues
```

### Key Files

Every run has these (the rest depends on the workflow):

| File | What it is |
|------|-----------|
| `Orchestration.md` | Central state — frontmatter (current position), Execution Log (history), Artifacts (registry) |
| Seed artifacts (e.g., `Requirements.md`) | Your starting documents, copied into the run folder |

Workflows with staged execution also produce:

| File | What it is |
|------|-----------|
| `Plan.md` | Routing artifact — stage table, ordering, dependencies |
| `Stage-{N}/Plan.md` | Per-stage detailed plan |
| `Stage-{N}/PlanProgress.md` | Per-stage progress tracker (what's done vs pending) |

Other artifacts (design docs, review outputs) vary by workflow. Check your workflow's table for the full list.
