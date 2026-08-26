# Checkpoint Agents

> **Status:** Draft for review
> **Created:** 2026-07-31
> **Last Updated:** 2026-08-01
> **Scope:** Design for the two agents that make checkpointing real — one that preserves restorable state during a run, and one that restores it. Covers what a checkpoint captures, how it is stored in git, how its reference travels back into the orchestration artifact, how a rollback is requested and performed, how it reconciles with work already committed, and how both behave when several runs share a working tree. Committing completed work into the user's own history is a different purpose served by a different class; see `CommitAgent.md`.

---

## 1. Purpose

The orchestration artifact schema already reserves a place for checkpoints: a `Checkpoint` column on the Execution Log, holding an external content-reference, with the guarantee that a non-empty value always names real, restorable content. It deliberately takes no position on what produces that reference or what consumes it.

This document supplies both. It defines two agents:

| Agent | Class | Invoked by | Touches the working tree |
|---|---|---|---|
| `checkpoint-manager-git` | Infrastructure agent (`checkpoint`) | A trigger, automatically | **No** — writes git objects only |
| `checkpoint-restore-git` | Infrastructure agent (`restore`) | Explicit human decision | **Yes** — overwrites files |

## 2. Why Two Agents

A single agent doing both would be smaller to deploy. It would also place a destructive capability inside an agent that fires automatically on a timer.

The two operations differ in every property that matters:

| | `checkpoint-manager-git` | `checkpoint-restore-git` |
|---|---|---|
| Invocation | Automatic, on trigger | Human decision only |
| Frequency | Many times per run | Rarely, often never |
| Working tree | Never modified | Overwritten |
| Failure cost | A missing restore point | Destroyed work |
| Safe with concurrent runs | Always | Never, without checking |
| Preconditions | None | Exclusivity check required |

Single responsibility is the standing rule in this system, and here it also happens to be the safety-relevant choice: the agent that runs unattended is the one that cannot destroy anything, and the agent that can destroy things never runs unattended.

**`checkpoint-restore-git` is declared as an infrastructure agent with `Class = restore`, but its safety property is preserved by its `MANUAL` trigger and by class-based exclusion from automatic trigger evaluation — not by being outside the infrastructure class.** Declaring it as infrastructure makes it discoverable and selectable through the same machinery as other infrastructure agents, so that a manual dispatch can find it by class rather than by hard-coded name, and so that any future alternative restore mechanism is automatically subject to the same rules. Its `MANUAL` trigger means it is never dispatched automatically, regardless of what is written in its declaration row — see `InfrastructureAgentConcept.md` §4. It is dispatched on a human's explicit instruction, after a Tier 3 escalation or a direct user request.

## 3. Design Principles

| Principle | Description |
|---|---|
| **Capture is safe, restore is dangerous** | Every asymmetry in this design follows from this one fact. Committing to git writes objects and moves a ref; it does not touch a single file in the working tree. Restoring overwrites files that other work may depend on. Guards therefore belong on restore, and capture needs none. |
| **The agent uses only what it was given** | A checkpoint commit records `run_id` and its own sequence number, because those are the two things the agent actually holds. It does not record phase, stage, or trigger — those live in the orchestration artifact the agent cannot read, and requesting them would widen the invocation message to duplicate data that a single lookup already provides. |
| **Never restore orchestration state** | A rollback restores project files and nothing else. Restoring the run folder would delete the Execution Log rows containing the very reference used to perform the restore, and would contradict the schema's rule that the sequence counter is never rewound. The record of a rollback must survive the rollback. |
| **Checkpoints stay out of the user's way** | Checkpoint commits are reachable only from a private ref namespace. They never appear in `git log`, `git branch`, or `git status`, are never pushed by default, and never interleave with the user's own commits. A user who never asks about checkpoints should never see one. Making run output *visible* is a different purpose, served by the `commit` class rather than by a checkpoint variant (§4.3). |
| **A recorded checkpoint is always restorable** | Inherited from the artifact schema, and it is what makes `on_failure: halt` correct for capture. A run believing it can roll back when it cannot is worse than a run that stopped and said so. |

## 4. `checkpoint-manager-git`

### 4.1 Declaration

```yaml
---
id: 36
version: 1.0.0
name: checkpoint-manager-git
description: Commits a restorable checkpoint of the working tree
model: {model-identifier}
tools: [file_read, terminal]
recommended_tier: LOW
required_skills: []
infrastructure: checkpoint
triggers:
  - trigger: STAGE_END
    trigger_param: null
  - trigger: INVOCATION_INTERVAL
    trigger_param: 10
on_failure: halt
---
```

`infrastructure: checkpoint` is what makes this agent satisfy a run's `checkpoints: enabled` precondition (§4.6). The class is a closed vocabulary value rather than a boolean specifically so that check is a string comparison rather than a judgement.

**This agent declares two triggers, and needs both.**

`STAGE_END` is the primary one, because a stage is the natural unit of restorable progress: it is the granularity at which work is planned, reviewed, and — when it goes wrong — abandoned. Redoing a stage is the most common rollback in this system, and it needs a restore point sitting exactly on the boundary it returns to.

`INVOCATION_INTERVAL` covers the inside of a stage. Without it, a stage that fails near its end can only be abandoned wholesale, discarding work that was fine. With a `commit`-class agent also running, the two triggers divide cleanly: the interval protects the window where nothing is committed yet — where rollback is always clean, because no branch has moved — and the stage-end checkpoint marks the boundary a redo returns to.

An earlier draft declared `STAGE_END` alone, on the reasoning that a run could override it if a finer grain was wanted. That was wrong: the two are not alternatives. Choosing the interval sacrifices the stage boundary a redo needs; choosing the boundary alone leaves the whole interior of a stage unprotected. Multi-trigger declaration is specified in `InfrastructureAgentConcept.md` §4, and this agent is its first consumer.

A run may override either trigger, or its parameter, at run start.

`on_failure: halt` follows directly from the schema's guarantee. If checkpointing was requested and a checkpoint could not be taken, continuing produces a run that believes it has a restore point it does not have. Stopping and reporting is the honest outcome.

**No `user_interaction` tool.** The agent fires unattended, so an ability to prompt would let it block a run at an arbitrary moment with no human expecting a question. Everything it needs is in the invocation message and the repository.

### 4.2 What it captures

The **entire working tree as it stands**, honouring the repository's own ignore rules. No filtering, no judgement, no distinction between agent-authored and user-authored changes.

Uncommitted user edits are captured along with everything else. A checkpoint is a restore point, not a curated commit: excluding some changes would produce a restore point corresponding to a tree state that never existed on disk, which is precisely what a restore point must not be. Git also cannot reliably attribute authorship of working-tree changes, so any such filter would be guesswork.

**Its own run folder is excluded, by the agent, explicitly.** The agent knows its `run_id`, so it excludes exactly `Orchestration-{run_id}/` and nothing else:

```
git add -A -- . ':!Orchestration-{run_id}'
```

This is the agent's responsibility, not the repository's. Relying on an ignore rule would be relying on a file MOSAIC does not ship and does not control — the user's `.gitignore` belongs to the user's project, and a guarantee this design depends on cannot rest on whether someone configured it correctly.

**The pathspec depends on run folders always being run-scoped.** Every run's orchestration state lives in `Orchestration-{run_id}/` — there is no un-namespaced `Orchestration/` variant. That is what lets both agents identify their own folder exactly (from `run_id`) and every other run's folder by pattern (`Orchestration-*/`), with no configuration and no ambiguity. A bare folder would be matched by neither: it would be captured into every checkpoint and, far worse, overwritten by every restore, silently destroying the audit trail of the rollback being performed. The single convention is a precondition of this design, not a stylistic preference.

Other runs' folders are deliberately **not** excluded. If a `Orchestration-{other}/` folder is tracked in the repository — because the user chose to commit a completed run as a record — then it is project content at this point in time and belongs in the snapshot like anything else. Capturing it is harmless, because restore refuses to write to any run folder at all (§5.3).

### 4.3 Storage

Each checkpoint is a commit reachable only from a private ref:

```
refs/mosaic/checkpoints/{run_id}/{seq}
```

No branch is created, updated, or checked out.

**A visible-branch storage variant was designed and removed.** It would have written the same commits to `refs/heads/mosaic/checkpoints/{run_id}`, identical in every respect except that ordinary git commands could see them. Once the `commit` class existed (`CommitAgent.md`), that variant had no purpose left: it carried mechanical `checkpoint #15` messages and snapshots of half-finished work, so it could never be merged, which left "a human can browse it in a GUI" as its entire value. A user who wants visible, mergeable output of a run wants prose commits, and that is a different class with a different destination. The need for browsable checkpoints may become real later; the case for building it before then was speculative, and §11 already said so.

**The commit is built through a temporary index**, so the user's staging area is never disturbed:

```
GIT_INDEX_FILE=.git/mosaic-{run_id}.index  git add -A -- . ':!Orchestration-{run_id}'
GIT_INDEX_FILE=.git/mosaic-{run_id}.index  git write-tree
git commit-tree {tree} -p {parent} -m "{message}"
git update-ref refs/mosaic/checkpoints/{run_id}/{seq} {commit}
```

**Parent selection.** `{parent}` is this run's previous checkpoint if one exists, otherwise `HEAD`, otherwise nothing — `-p` is omitted entirely. The three cases are checked in that order.

Chaining to the previous checkpoint rather than to `HEAD` is deliberate: the run's checkpoints form one walkable chain regardless of what the user's own branch did meanwhile. `HEAD` is used only for the first checkpoint, to root the chain somewhere meaningful in the user's history.

That `HEAD` may move afterwards — the user commits, switches branch, rebases — is expected and harmless. The chain records what this run's tree looked like at each point; it was never a claim about the user's branch topology. Each checkpoint's *tree* is a complete snapshot, so a restore reads one commit and needs no ancestry at all. Divergence costs nothing because nothing traverses the chain to restore.

This detail is load-bearing rather than incidental. A plain `git add` writes to `.git/index` — the user's staging area — so an agent that used one would silently destroy whatever they had staged, at unpredictable intervals, while claiming to be non-destructive. Directing the index to a private, run-scoped file removes that entirely: no file in the working tree is written, `HEAD` does not move, no branch changes, and the user's index is untouched.

That is what makes capture safe under any concurrency, and it is a stronger guarantee than "does not modify tracked files" — it means the operation is invisible to every other consumer of the repository.

**Why a private ref namespace rather than commits on a branch:**

- Invisible to `git log`, `git branch`, `git status`, and to tab-completion and branch pickers.
- Not pushed by default — `git push` does not carry refs outside `refs/heads` and `refs/tags` without being asked.
- Never interleaved with the user's own history, so nothing needs cleaning up before a review or a merge, and their branch tip never moves under them.
- Namespaced by `run_id`, so two runs checkpointing simultaneously cannot collide on a ref. This resolves the ref-collision hazard directly; it does **not** address a shared working tree, which is a different problem handled in §7.
- **Durable.** Anything under `refs/` is a reachability root, so `git gc` will never collect these commits. They are as permanent as a branch.
- **Removable.** Because nothing is ever built on top of them, the whole namespace for a run can be deleted when the run is finished with (§9), leaving the user's history exactly as it would have been.

Discoverability is not lost. The commit hash is recorded in the Execution Log, so `git show {hash}` works with no knowledge of the namespace at all.

### 4.4 Commit message

A readable subject line and exactly two trailers:

```
MOSAIC checkpoint: checkpoint-manager-git#15

Mosaic-Run-Id: 20260129T090000Z-a3f9
Mosaic-Seq: 15
```

Both values come from what the agent already has — `run_id` from the invocation, `Seq` parsed from its own `agent_instance_id`. Nothing else is included.

Phase, stage, trigger, and timestamp are deliberately absent. Not because they lack value, but because the agent cannot know them: they live in `Orchestration.md`, which subagents never read. Supplying them would mean widening the invocation message to carry data that `run_id` + `Seq` already reaches by lookup — those two fields are a foreign key into the Execution Log, and that row carries phase, stage, timestamp, and the invocation that preceded the checkpoint.

The trailers use git's standard trailer convention, so they are parseable by `git log --format` and `git interpret-trailers` without a custom parser. This makes an orphaned checkpoint self-describing: a commit found later can be attributed to its run even if the run folder was deleted.

### 4.5 Return contract

The agent's `status_message` **must end with** a checkpoint marker:

```
[checkpoint:{full-or-abbreviated-sha}]
```

```json
{
  "agent_instance_id": "checkpoint-manager-git#15",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "SUCCESS",
  "status_message": "Committed checkpoint of working tree (7 files changed). [checkpoint:4f1a08d]"
}
```

The orchestrator or runner extracts the reference and writes it to the `Checkpoint` column of this invocation's row.

**Why the tail of `status_message`, rather than a dedicated protocol field or `result_data`:**

- It requires no protocol change and no per-agent invocation configuration. A dedicated field would mean a protocol version bump for one agent; `result_data` would require the orchestrator to remember to set `include_result_summary` on every checkpoint dispatch, and silently loses the reference if it forgets.
- **It degrades instead of breaking.** `status_message` is copied verbatim into the Execution Log's `Summary` column, and when it exceeds the length limit the schema keeps the **first 50 and last 50 characters**. A marker at the very end therefore survives truncation. If extraction is never implemented, or is broken, or the run is being driven by a bare LLM with no tooling, **the hash is still sitting in `Summary` where a human can read it**. The structured column is an optimisation over a record that already exists.

The marker must be the final characters of `status_message`, with no trailing whitespace or punctuation after the closing bracket, so a consumer can anchor its match to the end of the string.

### 4.6 Preconditions and failure

This agent halts the run when it fails, which makes its failure modes unusually consequential: every one of them stops work that was otherwise healthy. They are therefore enumerated rather than left to the agent's judgement.

**Checked at run start, not by this agent.** If `checkpoints: enabled` and the orchestrator's declaration region contains no agent with `Class = checkpoint`, the configuration is invalid and the run must not start. This is the position the artifact schema previously declined to take, and it can be taken now because the mechanism is discoverable by string comparison rather than inference. It is a start-up check precisely so the run fails before doing work, rather than at the first stage boundary.

**Checked by the agent, on each invocation:**

| Condition | Behaviour |
|---|---|
| Not a git repository | `BLOCKED`. The run halts. A checkpointing run in a non-repository was misconfigured at start; failing loudly is the only honest outcome. |
| Repository with no commits yet | Proceed. `-p` is omitted (§4.3); the checkpoint is a root commit. This is a normal first-checkpoint case, not a failure. |
| Working tree identical to the previous checkpoint | Proceed and commit anyway. An empty checkpoint is cheap — git stores no new tree objects — and skipping it would leave a stage boundary with no restore point, which is exactly the gap the `Checkpoint` column is supposed to make impossible. |
| Mid-rebase, mid-merge, or detached `HEAD` | Proceed. None of these prevent `write-tree`, `commit-tree`, or `update-ref`, and none are modified by them. The operation reads the working tree and writes objects; the repository's in-progress state is irrelevant to both. |
| `update-ref` fails, or the ref already exists | `BLOCKED`. A ref collision means two invocations shared a `run_id` and a `Seq`, which the sequence counter is supposed to make impossible. Overwriting would destroy an existing restore point. |
| Any git command fails for another reason | `BLOCKED`, with the git error in `status_message`. The agent does not retry and does not work around it. |

**Every failure is `BLOCKED`, never `PARTIALLY_DONE`.** A checkpoint either exists and is restorable or it does not exist. There is no partial checkpoint, and a status code implying otherwise would put a reference in the log that does not resolve — the one thing the schema's guarantee forbids.

## 5. `checkpoint-restore-git`

### 5.1 Invocation

```yaml
---
id: 37
version: 1.0.0
name: checkpoint-restore-git
description: Restores the working tree to a previously captured checkpoint
model: {model-identifier}
tools: [file_read, terminal]
recommended_tier: MEDIUM
required_skills: []
infrastructure: restore
triggers:
  - trigger: MANUAL
    trigger_param: null
on_failure: halt
---
```

**`MEDIUM` rather than `LOW`, unlike its counterpart.** `checkpoint-manager-git` does one thing with no branching and cannot damage anything, so it is correctly `LOW`. This agent selects between three reconciliation cases (§5.2), each with its own preconditions and its own git operations, and gets exactly one attempt: a step skipped or reordered here destroys work rather than producing a poor artifact. It is also the rarest agent in the system — often never invoked in a run — so the tier costs almost nothing in practice. This is the one place where paying for a more capable model is unambiguously worth it.

`infrastructure: restore` classifies this agent under the `restore` class, making it discoverable through the infrastructure agent machinery. `triggers: [{trigger: MANUAL, trigger_param: null}]` declares that this agent is available to be dispatched on request — see `InfrastructureAgentConcept.md` §4 on `MANUAL` as a declaration, not an absence — but has no automatic firing condition. `on_failure: halt` applies when the agent returns a non-SUCCESS status: if a restore dispatch fails, the run stops. The executor additionally excludes all `restore`-class agents from automatic trigger evaluation by class (`InfrastructureAgentConcept.md` §3.2), so the safety property is preserved even if a future version of this agent's declaration row were accidentally given a non-MANUAL trigger.

Never triggered automatically. Dispatched only on explicit human decision — typically after a Tier 3 escalation, or a direct user request to abandon recent work.

**It appears in no workflow table.** It is dispatched out of band, on a human's instruction, the way any recovery action is. This has one downstream consequence worth naming: an agent that checks recorded execution against the workflow table will see a log row for an agent the table never mentions. That check must exclude non-workflow agents rather than report them, and the orchestration review agent's design handles it explicitly.

The target checkpoint is supplied in `task_description` as a commit hash the human chose from the Execution Log's `Checkpoint` column. Choosing the target is a human judgement about which point in the run was still good; nothing here automates it.

**`task_description` also carries the run's `commits` and `commit_branch` values**, copied verbatim from the artifact frontmatter (`CommitAgent.md` §6). They are what §5.2's reconciliation decides from, and the agent cannot obtain them any other way: it never reads `Orchestration.md`, and reading the repository's current branch answers a different question — see §5.2. Copying two fields into a dispatch requires no inspection by the orchestrator and no protocol change; it is the same "the agent uses only what it was given" principle the capture agent runs on (§3).

When `commits: disabled`, the pair is still supplied, and the agent takes the tree-only path unconditionally. An absent pair is a dispatch error rather than an implied `disabled`, because the two are indistinguishable from inside the agent and one of them is destructive.

### 5.2 Preconditions

Before touching anything, the agent verifies:

1. **The target is reachable from this run's checkpoint namespace.** Not merely "carries a `Mosaic-Run-Id` trailer" — reachability from `refs/mosaic/checkpoints/{run_id}` (private variant) or `refs/heads/mosaic/checkpoints/{run_id}` (side-branch variant) is the test. The trailer alone is too weak now that other MOSAIC agents author commits: `commit-manager-git` stamps the same provenance trailers on commits it writes into the user's own branch, and those must never be restore targets (`CommitAgent.md` §7). Anchoring the check to the namespace refuses them automatically, with no rule anyone has to remember.
2. **No other run is active in this working directory.** The agent enumerates sibling `Orchestration-{run_id}/` folders and checks each artifact's `current_state.phase`; any run not in a terminal state means the tree is shared. On contention it returns `NEEDS_CLARIFICATION`, naming the runs at risk, so a human decides. It does not proceed on its own judgement.

**This check belongs to the agent, not the orchestrator.** For an orchestrator to perform it would mean listing directories and reading *other runs'* orchestration artifacts — a direct violation of the information asymmetry the architecture depends on, and a worse version of the context pollution the orchestrator's own constraints forbid. The orchestrator learns the outcome through a status code alone, never learning that another run exists.

**It reads other runs' orchestration artifacts, which requires a stated exception.** Enumerating directories is ordinary subagent work and needs none. Opening `Orchestration.md` does: those files belong to their orchestrators, and the orchestrator's own constraints say subagents never access them. The exception is defensible on the same grounds as the orchestration review agent's — this agent makes no routing decision from what it reads. It extracts exactly one field, `current_state.phase`, to answer one question: is that run finished? Nothing else in those artifacts is read, and nothing read from them influences anything but the refuse-or-proceed decision. As with every such exception, it must also be written into the orchestrator's constraint so the two do not silently disagree.

**Reconciling with committed work.** The base behaviour is that the agent writes working-tree files and nothing else. When no `commit`-class agent is running, that is the whole story and restoring is always clean: checkpoints never enter any branch, so the abandoned work was never recorded anywhere, and after a restore the tree simply *is* the earlier state with nothing left to undo.

When a `commit`-class agent is running (`CommitAgent.md`), some of the work being abandoned may already be committed. Restoring the tree without addressing that leaves the tree disagreeing with the branch tip, and the next commit's diff would contain the undo of the abandoned work mixed into whatever else it captures. Leaving that unreconciled is the worst available outcome — it silently corrupts a commit the user will keep — so the agent resolves it, choosing by what the branch will tolerate.

**Only the branch is ever undone.** A rollback is one operation, not two, and there is no counterpart to it on the checkpoint side. A checkpoint is a value: a snapshot in a private namespace that nothing references and nothing builds on, so checkpoints taken during the abandoned work are neither reverted nor deleted — they are simply no longer used, and §9 retains them like any other. A branch is state: a pointer the user reads and the next stage commit extends, so it is the only thing a rollback has to correct. Where both classes are running, this is the practical difference between them, and it is why the reconciliation table below has entries for the branch and none for the checkpoints.

The two agents having fired on the same boundary is what makes the correction well-defined: neither modifies the working tree, so a stage-end checkpoint and a stage-end commit record the same tree, and returning the branch to that commit returns it to a state the checkpoint also describes.

| Case | Condition | Action |
|---|---|---|
| **1. Nothing committed past the target** | `commits: disabled`, or the target checkpoint's `Mosaic-Seq` is ≥ that of the newest run commit | Restore the tree. Nothing to reconcile. |
| **2. Branch is MOSAIC-owned and provably unshared** | `commit_branch` is `mosaic/run/{run_id}`, with no upstream and not contained in any other local branch | Reset the branch to the *reset target* below, then restore the tree. No revert commit, no residue: the abandoned commits are discarded exactly as a checkpoint would be. |
| **3. Anything else** | The user's own branch, or a MOSAIC branch that has been pushed or merged | Restore the tree, then commit the result as a revert. The abandoned attempt and its undo both remain in history, and the next stage's commit is clean. |

**Which case applies is decided by sequence number, not by git ancestry.** The checkpoint chain and the commit-agent's branch are **disconnected graphs** — a checkpoint parents to the previous checkpoint (§4.3), a stage commit to the branch tip — so no ancestry test relates them. `git merge-base` between a checkpoint and a branch commit answers a question about the user's history, not about this run, and using it here silently produces wrong answers.

The comparison is on `Mosaic-Seq`, which both agents stamp for exactly this reason (§4.4, `CommitAgent.md` §4.5). Newest run commit means the newest commit **on `commit_branch`** carrying `Mosaic-Run-Id: {run_id}`; its `Mosaic-Seq` against the target checkpoint's `Mosaic-Seq` decides case 1 versus the rest. Both values are readable with `git interpret-trailers` and need no MOSAIC knowledge beyond the run id.

**The branch is the source, never the Execution Log**, and the distinction is not academic once a run has rolled back before. A case-2 rollback erases commits from the branch while their Execution Log rows remain — the log is append-only and records that the commits were *made*, which stays true. An implementation reading the newest commit-agent row from the log would therefore reconcile against a commit that no longer exists, on a branch already at or before the target, and would rewind a second time from a state that needed no rewinding at all. The log records what happened; the branch records what survived; reconciliation is only ever about what survived.

**The reset target in case 2 is a commit-agent commit, never the checkpoint.** It is selected by three rules, in order:

1. **A run commit whose project content matches the target checkpoint**, if one exists — the newest such commit. Test: `git diff --quiet {commit} {checkpoint} -- . ':!Orchestration-*'`.
2. Otherwise, **the newest run commit whose `Mosaic-Seq` is less than the target's**.
3. If there is none — the target predates the run's first commit — **the run's starting commit**, obtained as the first checkpoint's parent, `refs/mosaic/checkpoints/{run_id}/{first}^`. That is available whenever case 2 is, since case 2 already requires checkpointing to be enabled, and it avoids recording a base commit in frontmatter that the repository already holds.
4. If rule 3 has no answer either — the run began in a repository with no commits at all, so the first checkpoint is a root commit with no parent — **fall through to case 3**: restore the tree and commit the result. There is no commit to reset to, and case 3 needs none. Uncertainty resolving to case 3 is already this table's general rule, and it holds here for the same reason: appending is defined in every repository state, rewinding is not.

**`{first}` is the numerically lowest sequence in the ref namespace, not the lexicographically lowest.** Refs are `refs/mosaic/checkpoints/{run_id}/{seq}` with an unpadded integer, so a plain sort places `10` before `9` and selects the wrong checkpoint — and therefore the wrong base commit. The error is silent until a run exceeds nine checkpoints, at which point it resets the branch to a point later than intended and the abandoned commits it was meant to discard survive.

**Rule 1 exists because the sequence test alone is wrong at a stage boundary.** Both agents fire there, checkpoint first, so the boundary's checkpoint carries a lower `Mosaic-Seq` than the commit recording the same work. A target chosen at that boundary would therefore fall through a Seq-only test to the *previous* stage's commit — discarding a commit that holds exactly the wanted state, along with its prose message, and returning the whole stage as uncommitted changes for no reason. The files would still come out right; the history would be gratuitously damaged.

**Rule 1's pathspec is load-bearing and easy to omit.** The two agents exclude different things — the checkpoint keeps other runs' folders and drops only its own (§4.2), the commit drops all of them (`CommitAgent.md` §4.4) — so their trees are never identical even when their project content is. Comparing tree hashes directly would report a mismatch at every boundary and the rule would silently never fire, leaving the defect it exists to fix. The comparison must be restricted to project paths.

Rule 1 also removes the design's sensitivity to declaration order here: whichever of the two agents takes the lower sequence number, the content test settles the target the same way.

**Resetting the branch to the checkpoint's own sha is prohibited**, and the prohibition is stated because the operation looks correct and its damage is delayed. A branch reset to a checkpoint commit *descends from* it, which destroys every property §4.3 relies on at once: the checkpoint commits become reachable from a branch, so `git log` shows `MOSAIC checkpoint: checkpoint-manager-git#15` interleaved with the user's work, `git push` will carry them, and §9's namespace deletion no longer discards anything because the branch holds them. A restore that did this would convert the run's entire private history into permanent public history as a side effect of undoing one stage.

Resetting to a run commit has none of that. It is already an ancestor of the branch tip, so the branch's ancestry is unchanged apart from being shorter, and the checkpoint chain stays unreferenced and deletable. The tree afterwards comes from the checkpoint and may sit ahead of that commit — the target may be a mid-stage checkpoint with no commit of its own — which is the ordinary state of a branch with uncommitted work, and needs no further reconciliation.

**Ownership is decided by the branch name, not by inspection.** `commit_branch` equal to `mosaic/run/{run_id}` means the branch is MOSAIC's, because that name is reserved and derivable from a field the agent already holds (`CommitAgent.md` §4.3). Anything else is the user's. No heuristic, no configuration, and no question the agent has to ask.

**Repository state is checked against the recorded branch, never the current one.** Before taking case 2 or case 3, the agent verifies `HEAD` is on `commit_branch`, is not detached, and that no rebase or merge is in progress — the same conditions, for the same reasons, that `CommitAgent.md` §4.6 makes `BLOCKED` for the commit agent. Any of them fails here, and the agent returns `BLOCKED` without touching the tree.

Reading the current branch instead would not substitute for this. It reports where `HEAD` *is*, which is trivially available and always looks correct; it cannot report where the run has been *committing*, which is the only thing that matters. The two agree right up until the moment they matter — the user having switched branches mid-run is exactly the case a live read cannot see, because whatever it finds is self-consistent. A recorded value is what makes the divergence detectable.

The failure this prevents is worse than the commit agent's. A stage commit on the wrong branch is misplaced work; a revert on the wrong branch undoes work that branch never contained, while the real branch keeps the abandoned commits and silently corrupts its next commit — both halves of the reconciliation land wrong.

**Case 3's revert commit uses the commit agent's pathspec**, `git add -A -- . ':!Orchestration-*'` (`CommitAgent.md` §4.4). Without it a rollback would drop run folders into the user's permanent history — the outcome that section rejects as worse than any tidiness it buys — and it would do so on the one path where nothing else is auditing what gets committed.

**Case 2 is a bounded exception to "never touches history", and it is defensible only where it applies.** Moving a MOSAIC-owned, unshared branch is the same operation as moving a checkpoint ref: it is our ref, nothing descends from it, and no one else has seen it. The moment either of those stops being true — an upstream exists, or another branch contains the commits — the operation becomes a rewrite of shared history and case 3 applies instead. Uncertainty resolves to case 3, which is always safe.

**Case 3's revert commit carries the same provenance trailers as any MOSAIC-authored commit**, `Mosaic-Run-Id` and `Mosaic-Seq`, taken from the invocation and from this agent's own `agent_instance_id`. The reasoning is `CommitAgent.md` §4.5's: a commit found later should be attributable to its run without the run folder. It also removes an ambiguity rather than leaving it to chance — a second rollback has to identify the newest run commit on a branch where one of the candidates is itself a rollback, and both readings of an untrailered revert happen to produce correct behaviour today, which is not the same as the behaviour being specified.

**Case 3's revert message is mechanical**, derived from the target and the run — `Revert to checkpoint {sha} (stage {N} state)` — and needs no plan artifact. This is why the restore agent performs it rather than dispatching the commit agent afterwards: the commit agent's message comes from a stage plan, and a rollback has none, so it would describe the abandoned work as though it were being done.

**One case the agent reports rather than resolves.** If the user made *their own* commits after the checkpoint was taken, case 3's revert would also undo those. The agent detects this by comparing the branch tip's history against the run's own commits, and returns `NEEDS_CLARIFICATION` naming them, because undoing a user's own work is not a decision an agent should make from inference.

### 5.3 What it restores

**Project files only.** No path under **any** `Orchestration-*/` folder is ever written, under any circumstances.

The exclusion is deliberately broader here than at capture time (§4.2), because the risks are different:

- **Its own run folder** — restoring it would delete the Execution Log rows recording what happened, including the row carrying the very checkpoint reference being restored to. The audit trail of a rollback must survive the rollback.
- **Any other run's folder** — restoring it would overwrite that run's orchestration state, possibly while that run is live. A rollback in one run must never be able to corrupt another.

This is a hard exclusion implemented in the agent, deliberately not a consequence of ignore rules. MOSAIC does not ship a `.gitignore` and has no visibility into the user's; a guarantee this important cannot depend on a file the system neither controls nor can inspect at design time. The agent knows what a run folder looks like and refuses to write to one.

### 5.4 What it records

An ordinary invocation row. Nothing is rewound.

```markdown
| 21 | test-runner#21 | EXECUTION | Implementation.2 | COMPLETED_NEEDS_ACTION | ... | 6 tests failing | Stage-2/Plan.md | - |
| 22 | checkpoint-restore-git#22 | EXECUTION | Implementation.2 | SUCCESS | ... | Restored working tree to 4f1a08d (Seq 15) | - | - |
```

`global_sequence` advances as for any invocation and is never decremented. No prior row is altered. `current_state` is not rewound to the checkpointed row's phase and stage: doing so would leave `current_state` disagreeing with the last Execution Log row, and the schema's recovery procedure resolves that disagreement by trusting the log — silently undoing the rewind on the next restart. The run's files move backward; its history does not.

This is what the existing schema already prescribes when it says a rollback is "just another subagent invocation." This design adds no special row shape, agent name, or status value.

## 6. The `Checkpoint` Column

The reference is written to **the checkpoint agent's own row**, not to the row of the invocation that preceded it.

The schema as currently written describes a checkpoint as taken "right after invocation N," with the reference recorded in invocation N's own row. That is coherent only if the orchestrator preserves content itself, inline, while processing invocation N. Once checkpointing is an agent, the checkpoint necessarily happens *after* row N has been appended — so recording it on row N means editing a written row, in a section the same schema declares strictly append-only and never revisited.

Recording it on the checkpoint agent's own row removes the contradiction rather than papering over it. Nothing is ever edited, the append-only guarantee holds without exception, and a deterministic runner never has to seek back and rewrite a table row it has already written. The row sits immediately after invocation N in every case, so no information is lost: the column means "content was preserved at this point in the log."

## 7. Several Runs, One Working Tree

The hazard is real but narrower than it first appears, because the two agents sit on opposite sides of it.

**Capture is unconditionally safe.** `checkpoint-manager-git` writes git objects and moves a private ref. It reads the working tree; it never writes to it. Two runs checkpointing at the same moment cannot interfere: their refs are namespaced by `run_id`, and neither modifies a file. The only consequence is that run A's checkpoint contains run B's in-flight edits — a superset, which harms nothing.

**Restore is the whole danger.** Overwriting the tree destroys whatever another run was doing. And restore is already, by design, never automatic.

Protection is therefore layered, with each layer doing only what it is architecturally permitted to do:

| Layer | May scan? | Responsibility |
|---|---|---|
| This design | — | States the rule: **concurrent runs sharing a working tree with checkpointing enabled is unsupported** |
| LLM orchestrator | FALSE | Emits a fixed advisory when the user enables checkpoints. No detection of any kind. |
| Script runner | TRUE | May enforce at run start; it is a script, and enumerating run folders costs it nothing |
| `checkpoint-restore-git` | TRUE | The backstop. Checks at the moment of danger, works in both execution modes. |

The orchestrator's warning is a **fixed string, not a finding**. It does not look for other runs; it states unconditionally that if other runs are active, a rollback may destroy their work. The user knows their own workspace. This costs the orchestrator nothing and tells it nothing.

**Git worktrees are out of scope.** A worktree gives genuine isolation — separate files, separate index, shared history — and would remove the hazard structurally. It is not viable here: a worktree contains only *tracked* files, and deployed agents typically live in ignored harness directories, so a fresh worktree contains no agents and the harness starting there finds nothing to dispatch. Making worktrees work would require either mandating that deployed agents be committed to the repository, or provisioning and deploying into each worktree automatically. Both are workspace-provisioning features, not checkpoint features, and neither belongs in this design.

## 8. No Repository Configuration Is Required

**Nothing in this design asks the user to configure their repository.** Both guarantees that matter are enforced by the agents themselves:

| Guarantee | Enforced by |
|---|---|
| The run's own orchestration state stays out of checkpoint commits | `checkpoint-manager-git`, via an explicit pathspec exclusion (§4.2) |
| No orchestration state is ever overwritten by a restore | `checkpoint-restore-git`, via a hard path refusal (§5.3) |

An earlier draft of this design required the user to add `Orchestration-*/` to `.gitignore`. That was wrong on two counts. MOSAIC does not deploy a `.gitignore` and never will — the user's repository is theirs, and its ignore rules are a project decision that has nothing to do with orchestration. And more fundamentally, a safety guarantee cannot rest on configuration the system neither controls nor can verify: if the rule is missing or mistyped, the failure is silent and the consequence is destroyed orchestration state.

Each agent already holds the information needed to be precise without any configuration — `run_id` identifies its own run's folder exactly, and the `Orchestration-{run_id}/` naming convention identifies every other run's folder by pattern.

A user who *wants* to ignore run folders may of course do so, and it will keep their `git status` cleaner. It changes nothing about the behaviour specified here.

## 9. Retention

**All checkpoints for a run are retained.** Nothing prunes, expires, or garbage-collects them.

A checkpoint costs one ref and one commit. Because git stores content by hash, successive checkpoints share every unchanged object, so the marginal cost of a checkpoint is roughly the size of the diff. For a run producing tens of checkpoints, this is negligible against the repository itself.

Deletion is a user operation, not an automated one. The whole namespace for a run is removable in one command once the run is finished with:

```
git for-each-ref --format="%(refname)" refs/mosaic/checkpoints/{run_id} | xargs -n1 git update-ref -d
```

**Removability is what most separates a checkpoint from a commit.** Nothing is ever built on top of a checkpoint — no branch descends from one — so deleting the run's refs discards the commits entirely and leaves the user's history exactly as it would have been. Work written by a `commit`-class agent has no equivalent operation once it has been shared: it is history, and it stays. That asymmetry is the reason the two are separate classes rather than storage options, and retention is where the difference actually bites.

The artifact schema already establishes that live rollback targets are computed at read time by walking the Execution Log backward, with no in-file bookkeeping. Retaining everything is consistent with that: no row ever needs marking as expired, and the append-only section stays append-only.

## 10. Amendments to the Orchestration Artifact Schema

| § | Current | Amendment |
|---|---|---|
| §5 | The `Checkpoint` column is populated on the row of the invocation the checkpoint was taken *after*. | Populated on the row of the invocation that *took* the checkpoint. Meaning becomes "content was preserved at this point in the log." See §6. |
| §5 | Rollback is "just another subagent invocation," unspecified further. | Substance unchanged; the mechanism is now named. `checkpoint-restore-git` is that invocation, and it appends an ordinary row. |
| §12 | Takes no position on `checkpoints: enabled` with no content-preservation mechanism present. | The position can now be taken: the configuration is invalid and the run must not start. The mechanism is discoverable as a `Class = checkpoint` row in the orchestrator's infrastructure agent declaration region, so the check is a string comparison rather than a judgement. See §4.6. |
| §5 | The Execution Log's column contract does not include `Inputs`. | Unchanged by this design, but note the orchestration review agent's design inserts an `Inputs` column before `Checkpoint`. The example rows here are shown in the post-amendment column order, since both land together. |

Only the first changes an existing guarantee's meaning, and it strengthens rather than weakens it: it is what allows the append-only rule to hold without exception, which the current wording cannot do while also making checkpointing an agent.

## 11. Non-Goals

- **Choosing a rollback target.** Which checkpoint to restore to is a human judgement about which point in the run was still good. Nothing here automates or advises it.
- **Non-git checkpoint mechanisms.** A filesystem-snapshot or archive-based checkpoint agent would be a different agent implementing the same return contract (§4.5) and the same `Checkpoint` column semantics. Not designed here.
- **Capturing into the user's own branch.** `checkpoint-manager-git` writes only to its private namespace (§4.3). Committing completed work into the user's history is a different purpose served by a different class, specified in `CommitAgent.md`. The restore agent may write a revert commit under §5.2 case 3, which is reconciliation of a rollback, not capture.
- **Merging or promoting checkpoint commits.** Checkpoints are restore points, not proposed changes. Nothing merges them anywhere.
- **Restoring to a commit that is not a checkpoint.** §5.2's namespace check refuses any target outside `refs/mosaic/checkpoints/{run_id}`, including commits authored by a `commit`-class agent. Choosing an arbitrary point in the user's history to return to is a git operation they perform themselves.
- **Workspace provisioning.** Worktrees, per-run clones, and any other isolation strategy (§7).
- **Resource contention beyond the working tree.** Ports, dev servers, databases, and build caches are untouched by any of this.

## 12. Open Items

- Whether `checkpoint-restore-git` should return `NEEDS_CLARIFICATION` or `BLOCKED` on detecting a concurrent run. `NEEDS_CLARIFICATION` invites a human override, which may be correct when the user knows the other run is idle; `BLOCKED` is firmer but forces a restart to proceed.
- Whether the checkpoint marker should carry an abbreviated or full SHA. Abbreviated is more readable inside a truncated `Summary`; full is unambiguous forever.
- What `INVOCATION_INTERVAL`'s default parameter should be. `10` is a guess. Too small wastes nothing in storage but clutters the Execution Log; too large leaves usable work unprotected inside a long stage.
- Whether a workflow without stages should substitute `PHASE_END` for `STAGE_END` automatically, or require the user to override. Automatic substitution is friendlier; requiring the override makes the configuration honest about what it is protecting.
- Whether §5.2's branch-mismatch refusal should stay `BLOCKED` or invite a human override. Switching to `commit_branch` itself is not an option — the tree is dirty by definition at that point — so the recovery is the user checking the branch out and re-requesting, which `BLOCKED` states plainly. `NEEDS_CLARIFICATION` would read better but implies a choice the agent cannot actually offer.
- Whether §5.2 case 2's "provably unshared" test is strict enough. It checks for an upstream and for containment in other local branches, which covers the realistic cases but cannot see a clone someone else made.

**Resolved:** what happens to `current_state` after a rollback. Nothing, and nothing needs to. §5.4's refusal to rewind it leaves a window where the run's recorded stage is ahead of the files, but that window closes at the next dispatch: `current_state` is rewritten from the routing of whatever invocation follows, so an orchestrator resuming work at stage 3 records stage 3, and every consequence that looked alarming — the `Mosaic-Stage` trailer on the next commit, the stage a later checkpoint is attributed to — follows from the corrected value rather than the stale one. Correcting it is an orchestration decision made at dispatch time, which is where routing decisions belong; a restore agent writing it would be a subagent making one.

The stage number consequently repeats in the Execution Log — two runs of stage 3 rows, separated by the restore row. That is the honest record and needs no special handling: `Seq` remains unique, the log remains append-only, and a reader sees exactly what happened. The one thing this rests on is the orchestrator routing to the right stage after a rollback, which it must do in any case for the run to make sense.

**Resolved:** no automatic checkpoint is taken before a restore. Making a rollback itself reversible was considered and rejected as disproportionate — a rollback is already a last resort reached only after escalation, and the work it discards is bounded by the checkpoint interval, which in a system organised around small stages is cheap to redo. The `PRE_ROLLBACK` trigger is dropped from the class vocabulary as a result, having had no other consumer.
