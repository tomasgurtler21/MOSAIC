# Commit Agent

> **Status:** Draft for review
> **Created:** 2026-08-01
> **Scope:** Design for the agent that commits completed stage work into the user's own branch. Covers why this is a distinct class rather than a checkpoint variant, what it commits and where, how its commit message is derived without reading orchestration state, the preconditions that make writing to someone's history safe, what a rollback of committed work costs, and how a MOSAIC-owned branch is integrated afterwards.

---

## 1. Purpose

`CheckpointAgents.md` specifies content preservation for the purpose of **undo**: snapshots taken on a trigger, stored where the user never sees them, existing only so work can be abandoned and recovered. Nobody reads a checkpoint. Its whole value is that it can be restored and then deleted.

Some users want the opposite thing. They want each completed stage to land in their branch as a real commit, with a message describing what was built, visible to `git log`, pushable, reviewable, and permanent. Not a restore point — a piece of their project's history that MOSAIC happened to author.

This document defines `commit-manager-git`, an infrastructure agent of the new `commit` class, for that user.

**This is not a storage variant of checkpointing.** The two look similar — both make git commits on a stage boundary — and that similarity is misleading. They differ in purpose, destination, message, permanence, and who is responsible for undoing them. Serving the second user by making checkpoints visible produces the worst of both: mechanical `checkpoint #15` messages in their permanent history, and restore points entangled with work they have merged and pushed.

| | `checkpoint` class | `commit` class |
|---|---|---|
| Purpose | Undo | History |
| Destination | Private refs, never a branch | A branch, either MOSAIC-owned or the user's own (§4.3) |
| Message | Mechanical: `checkpoint #15` | Prose, derived from the stage plan |
| Read by a human | Almost never | Always |
| Valid restore target | Yes | No — a rollback reconciles *past* these commits rather than returning to one (§7) |
| Deletable | Yes, one command | Not once integrated. Before that, a MOSAIC-owned branch can still be discarded whole (§8) — which is the point of integrating late |
| Empty diff | Committed anyway | Skipped |
| Activation | `checkpoints: enabled` | `commits: enabled` (§6) |

## 2. Design Principles

| Principle | Description |
|---|---|
| **MOSAIC is authoring the user's history** | Every other design in this system is careful to leave the user's repository untouched. This one deliberately does not. That is the entire point of the mode, and it is why the agent is opt-in, branch-pinned, and refuses to act in any repository state where a commit would be ambiguous. The cost is stated plainly rather than mitigated into invisibility. |
| **A commit is a unit of meaning, not an interval** | Checkpoints fire on whatever schedule the user likes, because a snapshot is meaningful at any instant. A commit is only meaningful at a boundary where some described piece of work is finished. This is why `STAGE_END` is the only permitted trigger (§4.2), and why an empty diff is skipped rather than committed. |
| **The message comes from the plan, not from the agent's imagination** | The agent is given the stage's plan and progress artifacts, so it describes work that was actually specified rather than inferring intent from a diff. This costs nothing — the artifacts already exist and the dispatch mechanism already carries them. |
| **The agent uses only what it was given** | Inherited from `CheckpointAgents.md` §3. The agent never reads `Orchestration.md`. Everything it needs — the run, its sequence number, the stage number, what the stage was for — arrives in the invocation message or in `input_artifacts`. |
| **Refuse rather than guess** | A checkpoint agent can afford to shrug at a detached `HEAD` or a mid-rebase repository, because writing an object changes nothing. Committing during those states produces a commit in a place the user did not intend and may not find. Where the checkpoint agent proceeds, this agent stops (§4.6). |

## 3. Relationship to Checkpointing

The two classes are independent switches. All four combinations are valid and mean something:

| `checkpoints` | `commits` | Result |
|---|---|---|
| disabled | disabled | No content preservation. The default. |
| enabled | disabled | Undo available. Nothing enters the user's history. The type-1 user: let MOSAIC work, commit at the end. |
| disabled | enabled | Work lands in the branch as it completes. No agent-driven rollback; the user reverts by hand (§7). |
| enabled | enabled | Both. Independent purposes, and cheap — see below. |

**Running both is not wasteful.** The obvious objection is that at every `STAGE_END` two commits of an identical tree are produced seconds apart. In storage terms this is nearly free: git stores content by hash, so the checkpoint's tree is the *same object* the commit already wrote. The marginal cost is one small commit object and one ref.

The two remain worth having together because they answer different questions. The commit is what the user keeps; the checkpoint is what the run rolls back to.

**Why checkpointing is still worth running when this agent is.** The question deserves a direct answer, because the two produce near-identical commits at every stage boundary and the obvious conclusion is that one of them is redundant.

They are **structurally out of sync**, and that is the first half of the answer. This agent is pinned to `STAGE_END` and cannot be moved (§4.2); the checkpoint agent additionally runs on `INVOCATION_INTERVAL`. So they coincide at boundaries and diverge everywhere else. Two things follow:

**1. Rollback granularity inside a stage.** With commits alone, the smallest undoable unit is an entire stage — a stage failing at its eighteenth invocation discards all eighteen, including the fifteen that were fine. No commit can cover that window, because a mid-stage commit has no completed work to describe. This is the case the interval trigger exists for and the one commits structurally cannot serve.

**2. A guarantee versus a best effort.** Checkpointing is `on_failure: halt`, so `checkpoints: enabled` means a restore point exists or the run stops and says so. Committing is `on_failure: continue`, is skipped on an empty diff, and is refused outright on a detached `HEAD`, a moved branch, or a rebase or merge in progress (§4.6). A rollback mechanism cannot rest on something permitted to be silently absent.

**And the reason that asymmetry cannot simply be fixed** is the load-bearing point. Making commits the restore mechanism would force this agent to `halt` — §4.7's argument for `continue` is precisely that nothing depends on the commit existing, and then something would. But every state in §4.6 that blocks a commit is one the *user* creates by using git normally: switching branch, checking out an old commit to look at something, starting a rebase in another window. Coupling those to `halt` lets a user stop a healthy run by doing ordinary work in their own repository.

The checkpoint agent has no equivalent failure mode, and not by luck. `CheckpointAgents.md` §4.6 has it proceed through every one of those states, because writing objects to a private ref touches no branch, no index, and no file, and does not care what the repository is in the middle of. **The mechanism that guarantees a restore point has to be one the user cannot disturb**, which is what puts it in a class that never writes to a branch.

**What this argument does not claim.** At a boundary where a commit *did* succeed, that commit could have supplied the files — restoring content from a commit requires no branch manipulation at all. The stage-boundary checkpoint is therefore close to redundant whenever commits are on. It survives on the two grounds above — the commit is not guaranteed to be there, and one restore mechanism is better than "the checkpoint if there is one, otherwise the commit, unless commits were off." That is a simplicity and reliability argument, not an impossibility one, and it is worth stating as such.

**A supported way to reduce the overlap.** A run that finds the duplicated boundary capture unnecessary may override the checkpoint agent's triggers to `INVOCATION_INTERVAL` alone (`InfrastructureAgentConcept.md` §6.2). In-stage granularity is kept, the exact duplication is dropped, and stage-boundary rollback then depends on the commit having been made. Restore targets become heterogeneous as a result, so the reconciliation in `CheckpointAgents.md` §5.2 is not simplified by it — a partial saving, offered rather than recommended.

**When both fire on the same boundary, the checkpoint agent must be declared first.** Trigger evaluation runs agents in declaration order, and neither agent modifies the working tree, so the two see an identical tree whichever way round they go — the ordering is not about what they capture. It is about failure. The checkpoint agent is `on_failure: halt` and this one is `continue`, so declaring this one first produces a run that halts *after* the stage has entered the user's history but before any restore point marks that boundary: a committed stage that cannot be redone, which is the single state the pairing exists to prevent. Checkpoint-first, the halt lands before anything is committed and the boundary is simply never reached.

The general rule this instantiates — that declaration order is significant wherever co-triggered agents differ in failure policy — is noted in `InfrastructureAgentConcept.md` §5.

**A `commit`-class agent does not satisfy the checkpointing precondition.** `InfrastructureAgentConcept.md` §6.1 gates `checkpoints: enabled` on a declared agent with `Class = checkpoint`, and that rule is unchanged by this document. A run wanting rollback must declare a checkpoint agent, even if a commit agent is also present. This follows directly from §7: commits made by this agent are not restore targets, so treating one as the checkpoint mechanism would let a run start believing it can roll back when nothing can.

## 4. `commit-manager-git`

### 4.1 Declaration

```yaml
---
id: 38
version: 1.0.0
name: commit-manager-git
description: Commits completed stage work to the user's branch
model: {model-identifier}
tools: [file_read, terminal]
recommended_tier: LOW
required_skills: []
infrastructure: commit
triggers:
  - trigger: STAGE_END
    trigger_param: null
on_failure: continue
---
```

**No `user_interaction` tool**, for the same reason as the checkpoint agent: it fires unattended, and an agent that can prompt is an agent that can block a run at a moment no human is watching for a question.

### 4.2 `STAGE_END` is the only permitted trigger

`InfrastructureAgentConcept.md` §6.2 lets a run override a declared agent's trigger. For this class that override is a configuration error, and a run naming any other trigger for a `commit`-class agent must not start.

The reason is §2's second principle. A commit fired on `INVOCATION_INTERVAL` lands mid-stage, on no boundary, with no completed plan to describe — the agent would be asked to write a message about work that is half-done. `PHASE_END` is closer to defensible but produces one enormous commit per phase, which is the granularity the user chose this mode to avoid. `MANUAL` would be coherent but has no consumer.

This is a narrower rule than the class system currently expresses, and §9 records it as an amendment rather than leaving it as prose here.

### 4.3 Destination

Two variants, chosen by the user at run start (§6), differing only in **who owns the branch being committed to** — and therefore in what a rollback is allowed to do to it.

| Variant | Branch | Rewinding a committed stage | User integrates by |
|---|---|---|---|
| **MOSAIC-owned** (recommended) | `mosaic/run/{run_id}`, created at run start from the current tip | Permitted while unshared: the branch is moved back and the abandoned commits vanish | Merging when satisfied |
| **User's own** | Whatever branch they are on at run start | Never: a revert commit is appended and the failed attempt persists | Nothing — it is already in their history |

**The MOSAIC-owned variant switches to its branch once, at run start**, and stays there for the run. This is the ordinary feature-branch flow. It is not done by a trigger-driven invocation of this agent — those only ever commit — but by the run-start setup dispatch specified in §4.9. The branch name is recorded in `commit_branch` (§6) exactly as in the other variant, so everything downstream of that field is identical.

**The variant is a run-start choice, not a deployment property.** An earlier draft of this section called these "deployment variants", on the implicit model that a deployment expresses the choice by which agent it deploys — which is how every other deployment-time choice in this system works. That model does not apply here. One agent serves both variants, and it must: they differ only in who owns the branch named by `commit_branch`, and this agent reads that field without caring how it was populated, which is what the sentence above means by everything downstream being identical. There is consequently nothing for a deployment to select *between*, and no injection point could express the selection without duplicating the agent.

Making it a run-start choice also costs nothing. The user is already being asked one question about this mode — whether to enable it at all — and the variant is the natural second half of that question. It keeps `commits` and `commit_branch` consistent as a pair, both set once at run start from an explicit user choice, rather than one being a user's answer and the other a deployment property leaking into the run artifact. And it is what lets §6's advisory be selected from the answer just given, rather than from configuration the orchestrator would otherwise have to be told about separately.

**No variant field is recorded, because the branch name already carries it.** A run's variant is decidable from `commit_branch` alone: it is MOSAIC-owned exactly when the value is `mosaic/run/{run_id}`. This is the same derivability that makes branch *ownership* answerable by `checkpoint-restore-git` without configuration (below), and it means the choice needs no second field that could disagree with the first.

**The MOSAIC-owned branch name is fixed as `mosaic/run/{run_id}`, and that is load-bearing rather than cosmetic.** Because it is derivable from a field every agent already holds, a second agent can determine *whether a branch is ours* without being told: `checkpoint-restore-git` decides between rewinding the branch and appending a revert by asking whether `refs/heads/mosaic/run/{run_id}` exists and `HEAD` is on it (`CheckpointAgents.md` §5.2). A memorable, task-derived name would read better in `git branch` and would make that question unanswerable without extra configuration, which is a poor trade for a branch that exists to be merged and deleted.

**The user's-own branch is recorded by reading `HEAD` once at run start**, not by asking the user to type a name. The two are the same guarantee — what matters is that a value is *recorded*, so a later mismatch is detectable (§4.6) — and reading it costs the user nothing. The recorded name is stated back to them in §6's advisory, which is where a wrong branch gets caught, before any work has been committed.

That read is also a git operation, performed by the same run-start setup dispatch (§4.9). This is worth stating because it is easy to assume only the MOSAIC-owned variant needs anything to happen at run start; both do, and the user's-own variant is the one where the requirement is least visible.

An alternative was considered and rejected: leaving the user on their own branch and writing commits onto a side branch they are not on, using the `commit-tree` and `update-ref` mechanism the checkpoint agent uses. It works, but it leaves the working tree full of changes that are committed *somewhere the user is not*, so their `git status` shows the entire run's output as uncommitted, and the eventual merge collides with a dirty tree. Switching once is simpler and matches what the user would have done by hand.

**Why MOSAIC-owned is recommended.** It is the only configuration in which redoing a committed stage stays clean. Because nothing descends from a MOSAIC-owned branch and no one else has seen it, the rollback can discard the abandoned commits outright, exactly as it would discard a checkpoint — the mechanism is in `CheckpointAgents.md` §5.2, case 2. The user still gets readable commits they can inspect and merge; they give up nothing except the need to merge at the end.

**And why that advantage is conditional.** It holds only while the branch is unshared. Once the user pushes it, or merges it into their own branch mid-run, the abandoned commits are somewhere else too, and rewinding stops being safe — the rollback falls back to a revert commit and the failed attempt becomes permanent, exactly as in the user's-own-branch variant. Incremental merging therefore trades away the main benefit of the variant. That is a legitimate choice, but it should be a knowing one, and §6's advisory says so.

### 4.4 What it commits

**Everything in the working tree, excluding every orchestration run folder:**

```
git add -A -- . ':!Orchestration-*'
```

This is a **deliberate divergence** from `CheckpointAgents.md` §4.2, which excludes only the agent's own run folder and captures other runs' folders as ordinary project content. That is correct for a snapshot — those files existed on disk, and a restore point must correspond to a tree state that really occurred. It is wrong here. Orchestration bookkeeping is not the user's project history, and committing another run's transcript into their branch permanently is a worse outcome than any tidiness it might buy.

**The user's own unrelated edits are committed too.** Git cannot reliably attribute authorship of working-tree changes, and any filter would be guesswork; excluding some changes would also produce commits that do not correspond to any state the tree was ever in. So whatever the user was editing when the stage ended lands in the commit, under a message describing MOSAIC's work.

This is the honest cost of the mode and it has no clean fix. It is stated here, and must be stated to the user when they enable commits (§6), rather than discovered later in a diff. A user who wants their own work kept separate should commit or stash it before a stage completes.

### 4.5 Message

The subject line describes the stage's work; two or three trailers carry provenance.

```
Implement profile update endpoint and its tests

Mosaic-Run-Id: 20260129T090000Z-a3f9
Mosaic-Seq: 16
Mosaic-Stage: 2
```

**The subject is derived from `input_artifacts`.** The agent is dispatched with the stage's plan, and where it exists the stage's progress artifact:

```
Orchestration-{run_id}/Stage-{N}/Plan.md
Orchestration-{run_id}/Stage-{N}/PlanProgress.md
```

The plan states what the stage was for; the progress artifact states what was actually completed. Between them the agent has everything a good subject line needs, and needs no diff analysis, no widened invocation message, and no protocol change. These are ordinary subagent output artifacts, not `Orchestration.md`, so passing them requires no access exception — `InfrastructureAgentConcept.md` §8 already covers this.

**The stage number comes from the artifact path**, not from orchestration state. `Stage-{N}/` is in `input_artifacts`, so the agent can populate `Mosaic-Stage` without ever reading `Orchestration.md`. This is the same foreign-key reasoning `CheckpointAgents.md` §4.4 uses for `run_id` + `Seq`: the agent records what it holds, and everything else is reachable by lookup from those.

Trailers use git's standard convention, so `git log --format` and `git interpret-trailers` parse them without a custom tool. This makes any MOSAIC-authored commit attributable to its run and stage long after the run folder is gone.

**No `Mosaic-Checkpoint` trailer, and the commit is not reachable from any checkpoint ref.** That is what makes §7's refusal automatic rather than a rule someone has to remember.

### 4.6 Preconditions

Stricter than the checkpoint agent's, and stricter in precisely the places where writing an object is harmless but writing history is not.

| Condition | Behaviour | Why it differs from `CheckpointAgents.md` §4.6 |
|---|---|---|
| Not a git repository | `BLOCKED` | Same. A run committing into a non-repository was misconfigured at start. |
| `HEAD` is not on the run's recorded branch (§6) | `BLOCKED` | **Diverges.** The checkpoint agent has no branch to be on. Here, a user who switched branches mid-run would otherwise have MOSAIC's work committed onto whatever they switched to. |
| Detached `HEAD` | `BLOCKED` | **Diverges.** The checkpoint agent proceeds — `write-tree` does not care. A commit made here belongs to no branch and is easily lost. |
| Mid-rebase or mid-merge | `BLOCKED` | **Diverges.** Same reasoning. Committing into an in-progress operation produces a state the user did not ask for and may struggle to unpick. |
| Working tree matches `HEAD` (nothing to commit) | `SUCCESS`, no commit made | **Diverges.** The checkpoint agent commits an empty checkpoint deliberately, so no stage boundary lacks a restore point. An empty commit in real history is noise with no corresponding value. |
| Repository with no commits yet | Proceed; the commit is a root commit | Same. |
| Any git command fails otherwise | `BLOCKED`, with the git error in `status_message` | Same. No retry, no workaround. |

Every failure is `BLOCKED`; there is no partial commit.

### 4.7 `on_failure: continue`, and its one consequence

A failed commit does not leave the run believing something false. Nothing downstream depends on the commit existing, no guarantee is broken, and the situation is self-healing: because the agent commits everything outstanding, the next stage's commit picks up whatever the failed one missed. Halting a healthy run because a bookkeeping commit could not be made inverts the cost, exactly as `InfrastructureAgentConcept.md` §7 argues for advisory agents.

**The consequence worth naming:** after a skipped or failed commit, the next successful commit contains more work than its message describes. Its subject comes from the current stage's plan, but its diff includes the previous stage's work as well.

This is tolerable and traceable — `Mosaic-Seq` and `Mosaic-Stage` record which invocation produced the commit, and the gap in the Execution Log shows what happened — but it is a real imprecision, and §11 records the open question of whether the executor should re-supply the earlier stage's plan when it knows a commit was missed.

**The same imprecision arrives by a second route, which has nothing to do with failure.** A rollback to a mid-stage checkpoint leaves the retained part of that stage in the working tree as uncommitted work (`CheckpointAgents.md` §5.2 — the branch goes back to a commit at or before the target, the files come from the checkpoint). The next stage-boundary commit sweeps it up, so its diff contains the surviving half of an abandoned stage alongside the stage its message describes.

Worth naming separately because the mitigation in §11 does not reach it: re-supplying a skipped stage's plan assumes a stage whose work is wholly present, and here only part of one is. The honest position is that any commit's diff is the tree at that boundary, and its message describes the stage that ended there — those coincide in the ordinary case and not after a rollback.

### 4.8 Return contract

An ordinary Task Response Message. The `status_message` **must end with** a commit marker:

```
[commit:{full-or-abbreviated-sha}]
```

```json
{
  "agent_instance_id": "commit-manager-git#16",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "SUCCESS",
  "status_message": "Committed stage 2 work to feature/profiles (12 files changed). [commit:9c2e41b]"
}
```

The marker must be the final characters, with no trailing whitespace or punctuation after the closing bracket.

**Nothing extracts it, and that is deliberate.** It exists so the hash survives truncation, not so a consumer can lift it into a column. §4.8's remaining paragraphs are the argument for why no column should exist to lift it into.

**Why a marker at all, when the hash is already in the message.** `Summary` is `status_message` copied across, truncated at 100 characters by keeping the **first 50 and last 50** — and this agent's messages routinely exceed that. §4.7 requires it to name the case where a commit contains more work than its subject describes, which produces messages around 130 characters with the hash sitting in the middle, exactly where head-and-tail truncation destroys it. An earlier draft of this section claimed the hash was safe because it was in `status_message`; that claim did not hold, and a tail-anchored marker is what makes it hold. This is the same reasoning `CheckpointAgents.md` §4.5 applies to the checkpoint marker, and it transfers unchanged — one convention across both agents rather than one having a convention and the other a habit.

**The `Checkpoint` column is not populated.** The orchestration artifact schema guarantees that a non-empty value there names real, restorable content, and `CheckpointAgents.md` §5.2 has the restore agent refuse anything outside the checkpoint ref namespace. A commit hash in that column would therefore name content that the system's own restore mechanism declines to restore — a reference that resolves for a human reading git, but not for the procedure the column exists to serve. Leaving it empty keeps the column's meaning exact.

**And no `Commits` column is added alongside it.** The obvious symmetry — a structured field for commit references, parallel to the one checkpoints get — is wrong, because the two references differ in the one property that column carries.

A checkpoint reference is **durable by construction**. Nothing prunes it, nothing builds on it, and only an explicit user command deletes the namespace (`CheckpointAgents.md` §9), which is what lets the schema promise that a non-empty value always resolves. A commit reference has no such property, and three ordinary operations this design *recommends* destroy it: a case-2 rollback discards the abandoned commits with the branch move (`CheckpointAgents.md` §5.2), a squash merge discards every stage commit and its trailers (§8), and any rebase the user performs does the same. A `Commits` column would therefore be the only column in the Execution Log whose values systematically stop resolving, sitting immediately beside one that guarantees the opposite — and it would look authoritative while doing it. Prose in `Summary` reads as a historical statement, which stays true after the commit is gone; a structured reference reads as a pointer, and a dangling pointer next to a live one is a trap.

**Nothing needs one in any case.** The restore agent is specified to read commit state from the branch and never from the Execution Log (`CheckpointAgents.md` §5.2), so a column would be a reference it is forbidden to use. A human choosing a rollback target chooses a *checkpoint*, so commit hashes are not part of that decision. And the mapping is already available git-natively — `git log --format='%h %(trailers:key=Mosaic-Seq,valueonly)' {commit_branch}` yields every run commit against its sequence number, and `Seq` is the foreign key into the Execution Log. That is the same foreign-key reasoning `CheckpointAgents.md` §4.4 uses to justify not stamping phase and stage into checkpoint commits.

The structural statement underneath all of this: **a checkpoint needs a reference in the log because it is invisible everywhere else; a commit does not, because being visible everywhere else is its entire purpose.** The commit is in the user's branch where `git log` finds it with no MOSAIC knowledge at all.

### 4.9 The run-start setup dispatch

Everything above assumes `commit_branch` holds a branch name and that `HEAD` is already on it when the agent fires. Something has to establish both, once, before the first stage boundary. This section specifies what.

**The agent has exactly two dispatch modes**, and they share nothing but the agent:

| Mode | Reached by | Does |
|---|---|---|
| **Commit** (§4.1–§4.8) | Trigger evaluation, on `STAGE_END` | Commits the working tree to `commit_branch`. Never touches branches. |
| **Setup** (this section) | One explicit out-of-band dispatch at run start, never a trigger | Establishes the commit destination and returns its name. Makes no commit. |

**What setup does, by variant:**

| Variant | Operation | Returns |
|---|---|---|
| MOSAIC-owned | Create `mosaic/run/{run_id}` from the current tip and switch to it | The branch name |
| User's own | Read `HEAD` | The current branch name |

Both are the same operation in the sense that matters: *determine where this run's commits will go, put the repository in that state, and report the name back*. The orchestrator records what comes back into `commit_branch` and never derives it itself.

**The name travels in a tail-anchored marker**, on the same convention as §4.8's commit marker and `CheckpointAgents.md` §4.5's checkpoint marker:

```
[branch:{branch-name}]
```

A successful setup response ends `status_message` with it, containing the branch name exactly as git holds it. A `BLOCKED` setup carries no marker, since no destination was established.

**Why a marker rather than prose or `result_data`.** The orchestrator has to obtain a machine-usable value from this response, and it has exactly one existing mechanism for that — tail-marker extraction, which it already performs for checkpoint references. Prose would require it to parse a sentence. `result_data` would work, but it is absent from the Execution Log, so the recorded branch would exist only in frontmatter with no trace in the run's history of where it came from; a marker lands in `Summary` as a side effect of the copy that already happens. The tail position earns its keep for the same reason as the other two markers: `Summary` truncation keeps the first and last 50 characters, and a branch name written mid-sentence is inside the part that gets dropped.

**A missing or unreadable marker is a failed setup**, not a degraded one. Every other marker in this system degrades gracefully — a lost checkpoint reference leaves a hash a human can still read in `Summary`, because the structured column is an optimisation over a record that already exists. This one does not, because no other record of the branch exists anywhere: the orchestrator cannot fall back on deriving it (that is the inspection §4.9 exists to avoid, and it is impossible for the user's-own variant in any case), and a run with no destination fails every subsequent branch check. So the orchestrator treats an unmarked success as `BLOCKED` and does not start the run.

**Why an agent rather than the orchestrator.** The orchestrator is the one component in this system specified to inspect nothing — §6 is explicit that even the advisory's branch name is read from the run artifact rather than from the repository, so that the orchestrator learns nothing about the workspace. Having it run `git checkout -b` would breach that for the MOSAIC-owned variant. The decisive point is that it would breach it for the **user's-own** variant too: recording that branch means reading `HEAD`, which is a repository inspection whoever performs it. There is no variant in which nothing has to touch git at run start, so the choice is not "agent or nothing" but "agent or orchestrator", and the agent is the component that already holds `terminal`, already reasons about this repository's state, and already owns what `commit_branch` means.

**Why this agent rather than a new one.** A dedicated setup agent would exist to run one command, would need a class of its own or an awkward home in the class vocabulary, and would split ownership of `commit_branch` across two agents — one that decides the destination and one that enforces it. The preconditions the two would need are the same preconditions reasoned about from the same place. Splitting them invites the two halves to drift.

**Out-of-band dispatch is established machinery, not a new concept.** `checkpoint-restore-git` is already dispatched this way: outside trigger evaluation, on explicit instruction, consuming a sequence number and taking an ordinary Execution Log row, with the orchestrator instructed that a log row for an agent the workflow table never names is expected rather than a routing error. Setup uses the same path. In particular it does **not** conflict with §4.2's rule that `commit` permits only `STAGE_END`: that rule constrains *trigger evaluation*, and setup is not a trigger.

**Preconditions**, which are necessarily not §4.6's — there is no recorded branch yet to check `HEAD` against, and establishing one is the entire point of the invocation:

| Condition | Behaviour |
|---|---|
| Not a git repository | `BLOCKED`. Commits were requested against something that cannot hold them, and the run must not start believing otherwise. |
| Mid-rebase or mid-merge | `BLOCKED`. Same reasoning as §4.6 — the repository is in a state the user is in the middle of, and moving it is not ours to do. |
| MOSAIC-owned, and `mosaic/run/{run_id}` already exists | `BLOCKED`. A run's branch is derived from its own id, so an existing one means either a colliding run id or a re-run against a used branch. Both need a human. |
| MOSAIC-owned, detached `HEAD` | Proceed. Creating a branch at the current commit is exactly the right move here, and it resolves the detachment rather than inheriting it. |
| User's own, detached `HEAD` | `BLOCKED`. There is no branch name to record, and inventing one would defeat the mismatch detection the field exists for. |
| Dirty working tree | Proceed, both variants. Creating a branch at the current tip and switching to it carries changes across without conflict, and the user's outstanding edits are swept into the first commit in any case (§4.4) — which the advisory has already told them. |

**Failure is fatal to the run, unlike a failed commit.** §4.7's argument for `on_failure: continue` is that nothing depends on any individual commit existing. Everything depends on this one invocation: without it there is no destination, and every subsequent trigger-driven invocation fails its branch check. A `BLOCKED` setup means the run does not start, and the user chooses between fixing the repository state and running with `commits: disabled`. This is not a change to the agent's declared `on_failure`, which governs trigger-driven firing; it is a property of the run-start sequence, in the same way that a failed configuration precondition stops a run without any agent's failure policy being consulted.

**Setup takes an ordinary Execution Log row** and consumes a sequence number, like any invocation. Its `Summary` names the branch established and the variant it reflects, and ends with the branch marker. It emits no commit marker, because it makes no commit, and the `Checkpoint` column stays empty as on any commit-class row.

## 5. Execution Log Representation

An ordinary row, like any infrastructure agent invocation:

```markdown
| 15 | Implementation#15 | EXECUTION | GREEN | SUCCESS | ... | Implemented updateProfile endpoint | Stage-2/Plan.md | - |
| 16 | commit-manager-git#16 | EXECUTION | GREEN | SUCCESS | ... | Committed stage 2 work to feature/profiles (12 files). [commit:9c2e41b] | Stage-2/Plan.md, Stage-2/PlanProgress.md | - |
```

`Seq` is consumed as for any agent, the `Checkpoint` column stays empty (§4.8), and `Inputs` records the artifacts the message was derived from — which, unusually for this system, is a genuine audit trail of *why the commit says what it says*.

The commit marker sits at the tail of `Summary`, where truncation cannot reach it (§4.8). It is read there by a human and by nothing else — no column consumes it, and no runner extracts it.

## 6. Activation

**A new run-start field, `commits: enabled | disabled`**, set once from an explicit user choice and fixed for the life of the run. Default `disabled`.

`InfrastructureAgentConcept.md` §6.1 currently gates only the `checkpoint` class, on the argument that a `review`-class agent is cheap, non-destructive, and has no wrong answer, so asking the user about it would be a question with no content. That argument does not extend here. Committing to someone's branch is neither cheap to undo nor invisible, and there is emphatically a wrong answer. This class needs its own switch.

**The question is asked only when a `commit`-class agent is declared.** A deployment with no such agent cannot commit, so the choice has one possible answer; asking anyway spends the user's attention on a decision they do not have and invites a "yes" the run must then refuse. In that deployment `commits` is recorded as `disabled` without the topic being raised. The precondition that `commits: enabled` requires a declared `commit`-class agent still exists, but it guards the paths that bypass the question — a value supplied in the starting prompt, carried in from a saved configuration, or present in a resumed artifact — rather than the user's own answer.

**This deliberately differs from how `checkpoints` is handled, and the difference is not an inconsistency.** Checkpointing is asked unconditionally even where no `checkpoint`-class agent is declared, and the resulting refusal is the point: a user who wants rollback and is told the deployment cannot provide it has learned something worth knowing at a moment when they can still fix it, since checkpointing is safe, cheap, and something most runs benefit from. Commits are neither universally wanted nor safe by default — the mode writes permanently into the user's history — so surfacing an unavailable option there advertises a capability the user might not have wanted, to no benefit. The test is whether being told "not available here" is useful to the user, and it is only useful when the answer would likely have been yes.

**When commits are enabled, the user also chooses the variant** (§4.3) — MOSAIC-owned or their own branch — as the second half of the same question. MOSAIC-owned is recommended, for the reason §4.3 gives: it is the only configuration in which redoing a committed stage stays clean.

**The target branch is recorded at run start** alongside the switch:

```yaml
commits: enabled
commit_branch: feature/profiles
```

Recording it rather than re-deriving it is what makes §4.6's branch check possible at all.

**The value comes from the setup dispatch, not from the orchestrator.** §4.9 specifies the invocation: it establishes the destination the chosen variant implies and returns the name in a `[branch:{name}]` marker at the tail of `status_message`, and the orchestrator writes the marker's contents verbatim. The orchestrator neither derives `mosaic/run/{run_id}` itself nor reads `HEAD` to learn the user's current branch — both are repository inspections, and it performs none.

**The half-derivable trap is why this is stated as a prohibition rather than a preference.** `mosaic/run/{run_id}` genuinely is reconstructible from a field the orchestrator already holds, so a reader who only considers the recommended variant will conclude that the marker is redundant. It is not: the user's-own branch is knowable only by reading `HEAD`. A rule permitting reconstruction where it is possible produces an orchestrator that derives one variant and extracts the other, which is the same wrong behaviour arriving silently rather than obviously.

**Order matters within run start**, because two of these steps are irreversible in opposite directions:

1. Ask whether commits are enabled, and if so, which variant.
2. State the advisory (below), including what will be committed and what a rollback costs.
3. Dispatch setup (§4.9); on `BLOCKED`, the run does not start.
4. Record `commits` and the returned `commit_branch` in the artifact.

The advisory precedes the dispatch because the dispatch may create a branch, and a user who has been told what the mode does should be told before anything in their repository moves. It cannot name the branch for the user's-own variant before the dispatch has read `HEAD`, so for that variant the recorded name is stated back immediately after step 4 — which is still before any commit exists, which is the guarantee §4.3 needs. An agent that read the current branch each time it fired would happily follow the user wherever they went; an agent checking against a recorded value notices that they moved. It also keeps the run self-describing, for the same reason `infrastructure_overrides` is recorded in the artifact rather than passed as an executor argument: a run resumed by a different executor commits to the same branch it started on.

**The user must be told what they are enabling.** At run start, when `commits: enabled` is chosen, a fixed advisory — not a finding, not the result of any inspection — states:

- that MOSAIC will commit at every stage boundary, **naming the recorded branch**. This is the point at which a wrong branch is caught, and it is the only such point: after the first commit lands, the mistake is in someone's history;
- that uncommitted work of their own will be swept into those commits (§4.4);
- what a rollback will cost, which depends on the variant. On a MOSAIC-owned branch, that rewinding stays clean while the branch is unmerged and unpushed, and that anything left behind afterwards is theirs to discard or squash at integration (§8). On their own branch, that a redone stage leaves its failed attempt in history permanently;
- on the MOSAIC-owned variant only, that the branch is theirs to integrate afterwards, that a squash merge lands the run as one commit and carries no rollback residue, and that a squash should carry `Mosaic-Run-Id` if they want the run attributable later (§8).

This costs the orchestrator nothing and tells it nothing about the workspace: it is a fixed string selected by the variant the user just chose, with the branch name substituted into it. The name comes from the setup dispatch's return, or from the artifact once recorded — never from the repository. The orchestrator inspects nothing.

## 7. Rolling Back Committed Work

Redoing a stage is the most common rollback in this system, and once a stage has been committed, a rollback has to deal with the commit as well as the files. This section states what that costs; the mechanism belongs to `checkpoint-restore-git` and is specified in `CheckpointAgents.md` §5.2.

**Commits made by this agent are never restore targets.** The restore agent's precondition — reachability from `refs/mosaic/checkpoints/{run_id}` — refuses them automatically, with no rule anyone has to remember. Rolling back means returning to a *checkpoint*; the commits are something the rollback has to reconcile, not something it aims at. It follows that **a run wanting rollback must declare a checkpoint agent** (§3), whatever this agent is doing.

**What a redo costs, by variant:**

| Redo target | Checkpoints only | + commits to MOSAIC-owned branch | + commits to user's branch |
|---|---|---|---|
| Current, uncommitted stage | Clean | Clean — no commit exists yet | Clean |
| An earlier, committed stage | Clean | Clean while the branch is unshared; the abandoned commits are discarded with the branch move | Failed attempt and its revert both remain in history |

The middle column is the reason `MOSAIC-owned` is the recommended variant (§4.3), and the third column is the honest cost of writing directly into someone's history: their branch has commits nobody can take back, so the only safe reconciliation is to append the undo.

**On a MOSAIC-owned branch, even the third column's residue is erasable.** A revert commit only appears there once the branch has been pushed or merged, and whatever residue accumulates lives on a branch that has not yet reached the user's own history. §8's integration choices decide how much of it ever does — a squash merge carries none. This is why enabling commits does not force a user to choose between agent-driven rollback and clean history; the two are separated by deferring integration rather than by giving one of them up.

**Why the restore agent performs the revert rather than dispatching this one.** This agent's message comes from a stage plan, and a rollback has no plan; handed the plan of the stage being undone, it would describe that work as though it were being done, attached to a commit that undoes it. The restore agent's revert message is mechanical and needs no artifact. Chaining the two would also open a window where the tree contradicts the branch with nothing recording why, for an operation that should be atomic.

**An earlier draft of this design banned agent rollback of committed work outright**, on the grounds that `git revert` and `git reset` are operations this user already knows, and that choosing between them needs judgement about whether the branch is shared. The first half of that still holds and is why the *choice* is constrained rather than offered. The second half did not survive: leaving the reconciliation undone silently corrupts the next stage's commit, which is worse than any of the options, and the share-state check turns out to be decidable enough — an upstream and containment in other local branches cover the realistic cases, and uncertainty resolves to the always-safe revert.

**The residual hazard is the user doing it by hand.** If they roll back manually and do not commit or revert before the next stage completes, that stage's commit contains the undo of the abandoned work mashed together with the new work. This is handled the way `CheckpointAgents.md` §7 handles concurrent runs: a **fixed advisory string** from the orchestrator, with no detection of any kind — if you roll back manually, commit or revert before continuing. The orchestrator inspects nothing and learns nothing about the repository.

## 8. Integration

Nothing in this system merges anything. Integration is the user's operation, performed by hand after the run — this section exists because *which* operation they choose determines whether §7's rollback residue ever reaches their history, and that turns out to be the answer to the objection this mode most often attracts.

Applies to the MOSAIC-owned variant only. In the user's-own variant the work is already in their history; there is nothing to integrate.

| Operation | Result in the user's branch | Rollback residue visible |
|---|---|---|
| `git branch -D mosaic/run/{run_id}` | Nothing. The run is discarded whole. | — |
| **`git merge --squash`** | One commit containing the run's net diff | **No.** Abandoned stages, reverts, and reverts-of-reverts collapse into the net result |
| `git merge --no-ff` | Every stage commit, in order, under a merge commit | Yes, in full |
| `git rebase -i`, or cherry-picking stage commits | Whatever they select | Whatever they leave in |

**The mode's apparent dilemma is false.** A user who wants agent-driven rollback *and* a clean history does not have to trade one against the other, and does not face an all-or-nothing choice between accepting the whole run and discarding it. They defer: the branch absorbs whatever churn the run produces, and a squash merge lands the outcome as a single commit. Per-stage granularity is available too — a range of stage commits squashed per stage, with reverted stages dropped — because stage commits are meaningful units, which is the entire reason §4.2 restricts the trigger to `STAGE_END`.

**One cost, stated because §4.5 otherwise over-promises.** A squash discards the individual commits and their `Mosaic-Seq` and `Mosaic-Stage` trailers with them; only the net diff survives. A user who wants the run attributable afterwards should carry `Mosaic-Run-Id` on the squash commit — it is the one trailer that still resolves to something, since the run folder is reachable from it and holds the per-stage detail the discarded commits carried. Nothing enforces this; it is a line in §6's advisory, not a mechanism.

**Why this is not a non-goal despite no agent performing it.** §9 lists merging as something this agent never does, and that stays true. But a design that recommends a variant on the grounds that it "stays clean" owes the reader the operation that makes it clean; without it, the recommendation rests on a step the user is left to invent.

## 9. Amendments to Other Designs

| Document | § | Current | Amendment |
|---|---|---|---|
| `InfrastructureAgentConcept.md` | §3.2 | Class vocabulary has two values, `checkpoint` and `review`. | Adds `commit`: writes completed work into the user's own history. Produces no restore point and is never a restore target. |
| `InfrastructureAgentConcept.md` | §6.1 | Only the `checkpoint` class is gated, on the argument that other classes have no wrong answer. | The `commit` class is gated by its own `commits` field. The stated rationale still holds for `review` and is now scoped to it rather than to "every other class". |
| `InfrastructureAgentConcept.md` | §6.2 | A run may override any declared agent's `trigger` and `trigger_param`. | A class may restrict which triggers are valid for it. `commit` permits only `STAGE_END` (§4.2); an override naming another is a configuration error and the run must not start. |
| `InfrastructureAgentConcept.md` | §12 | Open item: whether a third class beyond `checkpoint` and `review` is foreseeable. | Resolved. `commit` is the third, introduced with the two rules that distinguish it — its own activation gate and its trigger restriction. |
| `OrchestrationArtifactFormat.md` | §4 | Frontmatter enumerates `checkpoints` among the run-start fields. | Adds `commits: enabled \| disabled` and `commit_branch`, both set once at run start and fixed for the run (§6). `commit_branch` is populated from the setup dispatch's return (§4.9), never derived by the orchestrator. No variant field is added: the variant is decidable from the branch name (§4.3). |
| `InfrastructureAgentConcept.md` | §6.2 | A class may restrict which triggers are valid for it; `commit` permits `STAGE_END` only. | Scope clarified: a class trigger restriction governs **trigger evaluation** only. It does not constrain out-of-band dispatch, which reaches an agent by explicit instruction rather than by a trigger. `commit`'s setup mode (§4.9) and `restore`'s manual invocation are both out of band, and neither is an exception to the restriction — they are outside what it governs. |
| `InfrastructureAgentConcept.md` | §12 | Open item list. | Adds: whether an agent having two dispatch modes with different preconditions and different failure consequences should be expressible in the declaration region, which currently describes only trigger-driven behaviour. `commit` is the first agent with such a mode. |
| `InfrastructureAgentConcept.md` | §3.2, §4 | An agent declares one `trigger` and one `trigger_param`. | Replaced by a `triggers` list. Required by the checkpoint agent, which needs `STAGE_END` and `INVOCATION_INTERVAL` at once as soon as this class exists (§3). |
| `InfrastructureAgentConcept.md` | §12 | Open item: whether an agent should be able to declare more than one trigger. | Resolved by the above, with a concrete consumer rather than in anticipation of one. |
| `InfrastructureAgentConcept.md` | §5 | Declaration order is the tie-break when several triggers fire after the same invocation, described as arbitrary but deterministic. | Still arbitrary, but no longer inconsequential: where co-triggered agents differ in `on_failure`, order determines how much of a boundary's work is already done when a `halt` lands. `halt` agents are declared first. This pairing is the first case where it matters (§3). |
| `CheckpointAgents.md` | §5.2 | A restore target is verified by its `Mosaic-Run-Id` trailer, and the agent writes working-tree files only. | Target check tightened to reachability from `refs/mosaic/checkpoints/{run_id}`, so commits authored by this agent are refused as targets (§7). The agent additionally reconciles with committed work in three cases — tree-only, branch move, or revert commit — which introduces a bounded exception to its never-touches-history guarantee. Because it now writes commits, it inherits this agent's repository-state preconditions (§4.6) and its run-folder pathspec (§4.4) on that path, and receives `commits` and `commit_branch` in `task_description`, since it cannot read them from `Orchestration.md`. |

## 10. Non-Goals

- **Rolling back committed work *by this agent*.** The reconciliation is performed, but by `checkpoint-restore-git`, in the same invocation as the restore itself (§7, and `CheckpointAgents.md` §5.2). This agent is never dispatched as part of a rollback — its message comes from a stage plan, and a rollback has none.
- **Curating what goes into a commit.** The agent commits the whole tree minus run folders. Selecting hunks, splitting a stage into several commits, or separating the user's edits from MOSAIC's is not attempted, and §4.4 explains why it cannot be done reliably.
- **Pushing.** Nothing here runs `git push`. Whether and when commits leave the machine is the user's decision — and in the MOSAIC-owned variant it is the decision that ends clean rollback (§4.3).
- **Branch creation and switching on any trigger-driven invocation.** A commit-mode invocation never creates a branch or switches: it commits to the branch recorded in `commit_branch` and refuses if `HEAD` is not there (§4.6). Branch creation happens exactly once per run, in the setup dispatch (§4.9), which makes no commit. The two modes are disjoint, and neither ever does the other's job.
- **Merging, rebasing, and branch deletion, in either mode.** Integration is the user's own operation — §8 states the choices available to them and what each costs, without any of them being performed by anything here.
- **Pull requests, tags, or release artifacts.** Out of scope entirely.
- **Serving as a checkpoint mechanism.** §3. A run wanting rollback declares a checkpoint agent.
- **Commit message style configuration.** Conventional Commits, ticket prefixes, and house styles are real needs and are not addressed in this version. §10.

## 11. Open Items

- Whether the executor should re-supply an earlier stage's plan when it knows a commit was skipped, so the next commit's message covers the work it actually contains (§4.7). The alternative is accepting the imprecision, which is traceable but wrong.
- Whether commit message style should be configurable — a Conventional Commits prefix, a ticket reference, a house format. The mechanism would be an injection point rather than a run-time field, since it is a property of the project rather than the run. Deferred until someone asks.
- Whether `PlanProgress.md` should be required in `input_artifacts` or merely used when present. Requiring it makes messages more accurate; making it optional keeps the agent usable in workflows that do not produce one.
**Resolved:** whether the variant is a deployment choice or a run-start choice. Run-start (§4.3, §6). The deployment reading assumed a deployment expresses the choice by which agent it deploys, which is how deployment choices work elsewhere in this system; it does not hold here, because one agent serves both variants and reads `commit_branch` without caring how it was set. Nothing was left for a deployment to select between, and no injection point existed or could exist without duplicating the agent. This was found by deploying the design rather than by reading it.

**Resolved:** how the established branch name reaches the orchestrator. A tail-anchored `[branch:{name}]` marker on the setup response (§4.9), extracted into `commit_branch` (§6). The design previously said only that setup "returns the name", which specified an obligation without a channel — and the two channels a reader would reach for are both worse: prose requires the orchestrator to parse a sentence, and `result_data` never reaches the Execution Log, leaving the recorded branch with no trace of its origin. The marker reuses extraction machinery the orchestrator already performs for checkpoint references. Found by writing the orchestrator's instructions against this section and discovering there was nothing to write.

**Resolved:** whether the activation question is asked in a deployment with no `commit`-class agent. It is not (§6). The competing consideration is the `checkpoints` precedent, where the question is asked unconditionally and an unavailable capability produces an informative refusal. That precedent does not transfer: it is worth telling a user that safe, broadly useful rollback is missing, and not worth advertising a mode that writes irreversibly into their history to a user who never asked for it.

**Resolved:** what creates the branch and records `commit_branch`. A single out-of-band setup dispatch of this agent at run start (§4.9). The alternative — the orchestrator running the git commands itself — was rejected on the ground that it is the one component specified to inspect nothing, and on the sharper ground that it would have to breach that in **both** variants: recording the user's own branch means reading `HEAD`, which is a repository inspection whoever does it. There is no variant in which nothing touches git at run start, so the real choice was which component does, not whether one does. A dedicated setup agent was also rejected: it would exist for one command and would split ownership of `commit_branch` across two agents that would then be free to drift.

**Resolved:** what the MOSAIC-owned branch should be named. `mosaic/run/{run_id}` (§4.3). The competing option — a memorable name derived from the task — was rejected on a stronger ground than collision risk: a derivable name makes branch *ownership* decidable by a second agent from `run_id` alone, which is what lets `checkpoint-restore-git` choose between rewinding the branch and appending a revert without being configured. Readability in `git branch` is a weak return on losing that, for a branch whose purpose is to be merged and deleted.

**Resolved:** whether a stage that ends in failure should be committed. Not a live question — no current workflow definition routes a stage to its end in a failed state, so the case does not arise. If a future workflow allows it, the default matters and the argument is real in both directions: the work exists on disk either way, but committing known-broken work into someone's history is a poor default. Revisit then rather than now.

**Resolved:** whether this should be a storage variant of `checkpoint-manager-git` rather than its own class. It should not. The two agents share a trigger and a mechanism but differ in purpose, destination, message, permanence, restorability, empty-diff handling, and activation — which is every property that distinguishes one agent from another. Sharing a class would have forced §6.1's activation rule to treat a non-restorable commit as a restore mechanism.
