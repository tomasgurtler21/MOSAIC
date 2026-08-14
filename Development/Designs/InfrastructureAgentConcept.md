# Infrastructure Agent Concept

> **Status:** Draft for review — consolidated against the concrete agent designs
> **Created:** 2026-07-31
> **Last Updated:** 2026-08-01
> **Scope:** Defines the infrastructure agent class — agents that belong to no workflow but are executed on triggers during orchestration. Covers how they are declared, discovered, triggered, invoked, recorded, and how their failures are handled. Does not specify any concrete infrastructure agent; those have their own designs.

> **Authoring note.** This document was first drafted ahead of the concrete infrastructure agents, then revised against them. That order mattered: designing the instances removed a trigger (`PRE_ROLLBACK`) that had no consumer, changed the justification for another (`PHASE_END`) from anticipated need to deliberate framework generality, and moved three misplaced schema amendments out of §10 and into the design that actually owns them. Sections that generalise beyond what the instances demonstrate now say so explicitly rather than presenting themselves as established.

---

## 1. Purpose

Every agent in the system to date is a **workflow agent**: it appears as a row in a workflow's routing table, it is reached because the previous agent's status code routed to it, and it exists to advance the task the run is about.

Some work does not fit that shape. Committing a restorable checkpoint after each execution stage, or periodically checking that a long run has not drifted off its declared workflow, are both things that should happen *during* orchestration without being *steps in* it. Expressing them as workflow rows would be wrong twice over: they would have to be duplicated into every workflow that wants them, and they would misrepresent bookkeeping as task progress.

This document defines the **infrastructure agent** as a distinct class, so those behaviours have a home that is orthogonal to workflows.

An infrastructure agent is an agent that:

- is **declared once for an orchestrator**, not per workflow;
- is invoked because a **trigger condition** became true, not because a status code routed to it;
- performs **orchestration-support work** — checkpointing, self-review, bookkeeping — rather than work on the task the run exists to accomplish;
- is otherwise an **ordinary subagent**: same communication protocol, same instance-id scheme, same Execution Log treatment.

That last point is the load-bearing one. Infrastructure agents are a new *reason to invoke*, not a new *kind of invocation*.

## 2. Design Principles

| Principle | Description |
|---|---|
| **A new reason to invoke, not a new kind of invocation** | Infrastructure agents use the standard communication protocol, consume the global sequence counter, and get ordinary Execution Log rows. Nothing downstream — parser, log analyzer, recovery procedure — needs a special case for them. A design that required one would be paying a permanent tax across every consumer to save a little specification effort here. |
| **The deployed orchestrator file is the runtime contract** | Both executors — an LLM orchestrator reading its own system prompt, and a deterministic runner parsing that same file — discover infrastructure agents from one place. Agent frontmatter is the *source* a deployment tool assembles from, never a thing consulted at runtime. |
| **Hand-deployable** | Everything here must be achievable by a person writing files by hand, with no tooling installed. Automated assembly is a convenience layer over a format that stands on its own. |
| **Triggers are evaluated from the artifact, never from memory** | Every trigger condition is decidable from `Orchestration.md` alone. No trigger depends on state held only in an executor's context, so a run interrupted and resumed by a different executor fires the same triggers it would have fired otherwise. |
| **No cascades** | Infrastructure agent completions never evaluate triggers. An infrastructure agent can therefore never cause another one to fire, directly or transitively, and trigger evaluation always terminates. |
| **Availability and activation are different decisions** | Which infrastructure agents exist is fixed when the orchestrator is deployed. Whether and how often they fire is chosen by the user when a run starts. Collapsing the two would force a redeployment to change an interval. |

## 3. Declaration

### 3.1 The runtime contract

An orchestrator declares its infrastructure agents in a dedicated injection region. This region, in the **deployed** orchestrator file, is the single authoritative statement of what fires and when:

```markdown
<InfrastructureAgents type="project">

<InfrastructureAgent type="core" name="checkpoint-manager-git" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| checkpoint | STAGE_END | - | halt | Commits a restorable checkpoint of the working tree. Returns a content-reference; never modifies files. |
| checkpoint | INVOCATION_INTERVAL | 10 | halt | As above, covering the interior of a stage. |
</InfrastructureAgent>

<InfrastructureAgent type="core" name="commit-manager-git" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| commit | STAGE_END | - | continue | Commits completed stage work to the user's own branch. Not a restore point; never rolled back by any agent. |
</InfrastructureAgent>

<InfrastructureAgent type="core" name="orchestration-review" version="1.0.0">
| Class | Trigger | Param | On Failure | Description |
|-------|---------|-------|------------|-------------|
| review | INVOCATION_INTERVAL | 30 | continue | Advisory — reports observations about the run's own bookkeeping. Never returns an instruction to act. |
</InfrastructureAgent>

</InfrastructureAgents>
```

The structure deliberately mirrors how workflows are already embedded in the same file: one `<{Prefix} type="core" name="{id}">` block per item, a version attribute on its opening tag, and discovery by depth-agnostic section lookup on the `InfrastructureAgent:` name prefix. A consumer that already enumerates workflow regions can enumerate these with the same machinery and a different prefix.

**Declaration rules:**

- The section name after the `InfrastructureAgent:` prefix is the agent name, and is what the executor dispatches to. An empty identifier is invalid.
- Duplicate identifiers within one file are invalid. An executor encountering them must refuse to start rather than pick one.
- **Multiple same-class agents are permitted in the declaration region; the executor selects one per gated class at run start.** A class is gated when concurrent operation of multiple agents of that class would be ambiguous or harmful — currently `checkpoint`, `commit`, and `restore`. Declaring two or more differently-named agents of a gated class is valid. When a run starts and the declaration region contains more than one agent of the same gated class, the executor prompts the user to select which one to use for that class; only the selected agent's triggers are evaluated for the life of the run. When exactly one agent of a gated class is declared, it is auto-selected without prompting. This selection model resolves the "both fire on the same boundary" concern without a deployment-time prohibition: the executor enforces that at most one agent per gated class is active per run, but allows a deployment to offer several options — for example, alternative checkpoint storage backends or restore mechanisms — and defers the choice to the moment when the user actually knows which they want. Non-gated classes are unrestricted and never subject to selection; multiple `review`-class agents with different remits may all fire concurrently. Note that gating is decoupled from activation switches (§6.1): `checkpoint` and `commit` are both gated and carry activation switches; `restore` is gated but carries no activation switch (it uses a `MANUAL` trigger instead). The gating criterion is "ambiguity or harm from concurrent operation", not "carries an activation switch".
- The `version` attribute is required on each region's opening tag, for the same staleness-detection reason workflow regions carry one.
- **A section may contain more than one row**, one per trigger the agent declares. `Class` and `On Failure` must be identical across those rows — they are properties of the agent, not of a trigger — and a section whose rows disagree on either is invalid. Duplicate triggers within one section are invalid for the same reason duplicate identifiers are: an executor must refuse rather than pick one.
- An absent or empty `<InfrastructureAgents type="project">` region means this orchestrator has no infrastructure agents. This is valid and must not be treated as an error — it is the correct state for a minimal deployment.

**`Class` and `Description` address different readers, and neither substitutes for the other.**

`Class` is a closed vocabulary, matched by a script. It is what makes §6.1's activation rule decidable without interpretation: a validator can answer "does this orchestrator declare a checkpoint-class agent?" by string comparison. Free text could not be checked that way.

`Description` is prose, read by the orchestrator. It states how the response should be treated — that `orchestration-review` returns observations rather than instructions, that `checkpoint-manager-git` never touches the working tree. `class: review` alone does not convey that; the orchestrator would have to know what the class name implies, which is exactly the kind of carried-elsewhere knowledge this document avoids. It also makes the region legible to a person deploying by hand, who otherwise sees only agent names and triggers.

**The region is also read by `orchestration-review`.** It needs the list of declared agent names to avoid reporting infrastructure agent invocations as agents the workflow table does not name (that agent's design, §6). This is the only consumer of the region other than the executor itself.

### 3.2 The assembly source

An infrastructure agent declares its own default trigger in its own frontmatter, alongside the fields agent files already carry:

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

| Field | Meaning |
|---|---|
| `infrastructure` | The agent's class, from the vocabulary below. Its presence marks the agent as belonging to the infrastructure class at all, and its value states which kind. |
| `triggers` | One or more default trigger conditions, each from the vocabulary in §4, with its parameter where the trigger type takes one. |
| `on_failure` | `halt` or `continue` — see §7. |

**Why a list rather than a single trigger.** An earlier draft allowed exactly one, on the reasoning that no agent needed more and that one row per agent kept the declaration table simple. `checkpoint-manager-git` broke it as soon as the `commit` class existed. It needs `STAGE_END`, because a stage redo returns to a stage boundary and must find a restore point there, *and* `INVOCATION_INTERVAL`, because the interior of a stage is otherwise unprotected and is where rollback is cheapest. Those are not alternatives — picking either one loses a case the other covers, and no run-time override can supply both.

The declaration region (§3.1) therefore carries one row per trigger for a multi-trigger agent, keeping the table's shape uniform while the section groups them under one agent name.

**Class vocabulary.** Currently four values:

| Class | Meaning |
|---|---|
| `checkpoint` | Preserves restorable content and returns a content-reference. Never writes to a branch the user works on. Gated by the run's `checkpoints` setting (§6.1). |
| `commit` | Writes completed work into the user's own history. Produces no restore point and is never a restore target. Gated by the run's `commits` setting (§6.1), and restricted to the `STAGE_END` trigger (§6.2). |
| `review` | Inspects the run and reports observations. Produces no artifact and never routes. |
| `restore` | Returns the working tree to a previously captured checkpoint. Carries a `MANUAL` trigger, meaning it never fires automatically and acts only on explicit human instruction. Excluded from automatic trigger evaluation by class, not by name, so any restore-class agent is automatically exempt regardless of what triggers its declaration row names. Gated (§3.1) because a manual dispatch must unambiguously know which restore mechanism to invoke; concurrent operation of multiple restore agents would be ambiguous. Carries no activation switch (§6.1): the `MANUAL` trigger is the mechanism that prevents automatic firing, not a per-run on/off switch. |

A closed vocabulary is the point: an open one could not support §6.1's validation, which is the only reason the field carries a value rather than a boolean. New classes are added deliberately, alongside whatever rule needs to distinguish them — a class with no consumer should not be introduced.

`commit` earns its place on that test. It arrives with two rules that no existing class expresses — its own activation gate, and a restriction on which triggers are valid for it — and it must be distinguishable from `checkpoint` for a reason the system already depends on: a `commit`-class agent makes git commits on a stage boundary exactly as a `checkpoint`-class agent does, but its output is not restorable by any agent, so it must not satisfy the checkpointing precondition. Collapsing the two would let a run start believing it can roll back when nothing can. The full argument is in `CommitAgent.md` §1 and §3.

`restore` earns its place on the same test. It must be distinguishable from `checkpoint` for the inverse reason: a restore-class agent does not preserve content and does not satisfy the checkpointing precondition, so merging it with `checkpoint` would cause a run with only a restore agent to believe it can checkpoint when it cannot. It must also be distinguishable from ordinary subagents because it needs to be discoverable and selectable through the infrastructure agent machinery — so that a manual dispatch can find it by class rather than by hard-coded name, and so that any future restore-class agent (for example, one backed by S3 rather than git) is automatically subject to the same exclusion from trigger evaluation without a code change.

**This supersedes an earlier `infrastructure: true`.** The boolean marked membership but not kind, which left §6.1's activation rule undecidable: an executor could see that agents existed without being able to tell whether any of them was the checkpoint mechanism the run's configuration required. Carrying the class instead answers both questions with one field.

**`user_interaction` is deliberately absent from `checkpoint-manager-git`'s tools.** It fires unattended on a trigger; an agent that could prompt mid-run would be able to block a run at an arbitrary point with no human expecting it. Infrastructure agents that genuinely need a human — none currently — would declare `human_in_the_loop` at dispatch (§8) rather than reaching for the user on their own initiative.

These fields are the **default** a deployment tool reads when assembling §3.1's region. They are not consulted at runtime by anything. A person deploying by hand may write the region directly and never look at them; a project may override them at deployment time; and a run may override the parameter at run start (§6).

This mirrors `required_skills`: the agent file states its own requirement, tooling acts on it, and nothing at runtime re-derives it from the source file.

## 4. Trigger Vocabulary

Four triggers. Each is decidable from `Orchestration.md` alone.

| Trigger | Param | Fires when |
|---|---|---|
| `INVOCATION_INTERVAL` | `n` (positive integer) | `global_sequence` minus the `Seq` of this agent's most recent Execution Log row is ≥ `n`. If the agent has no prior row, `global_sequence` ≥ `n`. |
| `PHASE_END` | — | The just-completed invocation's routing changes `current_state.phase`. |
| `STAGE_END` | — | The just-completed invocation's routing changes `current_state.stage` (only meaningful in `EXECUTION`). |
| `MANUAL` | — | Never fires automatically. Invoked only by explicit user or orchestrator request. |

**On `PHASE_END` having no current consumer.** Neither concrete infrastructure agent declares it: checkpointing defaults to `STAGE_END`, periodic review to `INVOCATION_INTERVAL`. It is retained anyway, on the principle that MOSAIC is a framework rather than an application. Phases are a construct the system already has, and a workflow without stages has no other boundary to act on; offering the trigger costs one row in this table and lets users make that call for themselves, rather than the framework deciding on their behalf that no one will ever want it.

This is deliberately *not* an argument for keeping phases. If phases are removed from the system, this trigger is removed with them — a trigger is a poor reason to preserve a construct that has otherwise outlived its use.

**On `PRE_ROLLBACK` having been considered and dropped.** An earlier draft included a trigger firing immediately before a restore, so that a rollback could itself be rolled back. It was removed: a rollback is already a last resort reached only after escalation, and the work it discards is bounded by the checkpoint interval, which in a system organised around small stages is inexpensive to redo. Guarding a rare operation against its own rarity added a concept to the vocabulary for a case that does not justify one.

**On `trigger_param`.** `INVOCATION_INTERVAL` remains its only consumer. The generic field is retained rather than collapsed into an `interval` field on that one trigger, so that the declaration table has a single uniform shape regardless of which trigger a row names. This is a small, reversible cost; if no second parameterised trigger ever appears, collapsing it later changes one table and one frontmatter field.

**On `INVOCATION_INTERVAL` being a threshold rather than a modulus.** The obvious formulation is `global_sequence % n == 0`, which is stateless and cheap. It is also wrong: it silently skips ticks. If a workflow invocation takes sequence 29 and a checkpoint invocation takes 30, the next workflow invocation is 31 — the multiple of 30 was consumed by an invocation that did not evaluate triggers (§5), so the interval never fires at all. Over a long run with frequent checkpointing, an agent on a 30-interval can be starved indefinitely.

The threshold form has no such failure. It costs a backward scan of the Execution Log for the agent's own most recent row, which is bounded, requires no new frontmatter field, and — because it reads only the artifact — survives a crash and a change of executor identically to every other trigger here.

**`MANUAL` is a declaration, not an absence.** An agent declared with `MANUAL` is available to be invoked on request and is subject to every other rule in this document; it simply has no automatic firing condition. This is distinct from not declaring the agent at all, which makes it undispatchable.

**Every trigger fires after an invocation completes, never before one.** This follows from §5: trigger evaluation is defined against artifact state, and the artifact only reflects an invocation once that invocation has finished. There is no before-the-fact trigger in this vocabulary, and adding one would mean evaluating a condition against state that has not yet been written.

## 5. Trigger Evaluation

Trigger evaluation happens **after a workflow invocation completes and its `Orchestration.md` updates have been written** — that is, after the Execution Log row is appended and `current_state` is updated, and before the next workflow agent is dispatched.

Writing first is not incidental. Triggers are defined against artifact state (§2), so evaluating them before the artifact reflects the completed invocation would evaluate them against stale state. It also means an interruption between the write and the trigger firing loses at most the trigger, never the record of the invocation.

**Procedure, after each workflow invocation:**

1. Write the invocation's Execution Log row, then its `current_state` update, in the order the orchestration artifact schema already requires.
2. Evaluate each declared infrastructure agent's trigger against the updated artifact, in the order the agents appear in the `<InfrastructureAgents type="project">` region.
3. For each trigger that fired, dispatch that agent as an ordinary invocation (§8) and process its response fully — including writing its own Execution Log row — before evaluating the next.
4. Do **not** evaluate triggers after an infrastructure agent completes.

**Step 4 is what makes this terminate.** Because infrastructure agent completions never evaluate triggers, no infrastructure agent can cause another to fire, and no run of trigger evaluation can be longer than the number of declared agents. Without this rule, an agent whose own invocation satisfies a `PHASE_END` or interval condition could re-trigger itself or another agent indefinitely.

**An agent fires at most once per evaluation, however many of its triggers matched.** A stage boundary that also satisfies a checkpoint agent's interval condition produces one checkpoint, not two. The triggers are alternative reasons to run the agent, not independent invocations of it, and firing twice would consume two sequence numbers and write two identical Execution Log rows for one event. When several of an agent's triggers match, they collapse to a single dispatch.

**Declaration order is the tie-break.** When several triggers fire after the same invocation — a stage boundary that is also a phase boundary, say — the agents run in the order they appear in the declaration region. This is arbitrary but deterministic, and determinism is the property that matters: both executors must produce the same Execution Log from the same run.

**Arbitrary does not mean inconsequential.** Where co-triggered agents differ in `on_failure`, order decides how much of the boundary's work has already happened when a `halt` lands, and that can be the difference between a recoverable state and an unrecoverable one. A deployment declaring several agents on the same trigger should order them accordingly, `halt` agents first, so that an agent whose failure stops the run stops it before a `continue` agent has committed the run to anything. `CommitAgent.md` §3 works this through for the concrete pairing where it bites.

## 6. Run-Time Configuration

Deployment decides *which* infrastructure agents exist. A run decides *whether and how often* they fire.

### 6.1 Activation

`Orchestration.md` already carries a `checkpoints: enabled | disabled` field, set once at run start from an explicit user choice and fixed for the life of the run. That field is the activation switch for agents declared with `Class = checkpoint`. When `disabled`, their triggers are never evaluated and they never fire.

`commit` is gated the same way, by its own `commits: enabled | disabled` field, specified in `CommitAgent.md` §6. Each gated class has its own switch rather than sharing one, because they are independent choices: all four combinations of the two are valid and mean different things.

**A class is gated when there is a wrong answer.** That is the test, and it is why `review` is not. Checkpointing has a real per-run cost and a real precondition, so the user has to be asked. Committing writes permanently into the user's own branch, which is neither cheap to undo nor invisible, so the user certainly has to be asked. A `review`-class agent has neither property — it is cheap, it cannot fail destructively, and its `on_failure: continue` means the worst case is a run that proceeds without it. A switch for it would be a question at run start with no wrong answer, which is a question worth not asking.

Deactivating a `review`-class agent is therefore a deployment decision, not a run decision: an orchestrator that should not run it does not declare it. This is consistent with §2's separation — availability is fixed at deployment, and activation exists only where a genuine per-run choice exists.

This also makes a precondition the orchestrator already states — that enabling checkpoints requires something able to actually preserve content — mechanically checkable for the first time. Previously it was a judgement an LLM had to make about its own configuration. Now it is a string comparison against the `Class` column: does the declaration region contain a row with `Class = checkpoint`? A script can evaluate that; so can a human reading the file.

**`Class = commit` does not satisfy it**, despite such an agent also making git commits at stage boundaries. Its commits are not restore targets — the restore agent refuses anything outside the checkpoint ref namespace — so accepting one here would let a run start with `checkpoints: enabled` and no rollback capability whatsoever, which is precisely the state this check exists to prevent. A run wanting both behaviours declares both agents.

What happens when it does not — whether the run refuses to start, warns, or downgrades — is a checkpointing policy rather than a property of the infrastructure agent class, and is specified in the checkpoint agents' own design.

### 6.2 Trigger override

A run may override a declared trigger, and its parameter, without redeploying, via an optional frontmatter block:

```yaml
checkpoints: enabled
infrastructure_overrides:
  orchestration-review:
    triggers:
      - trigger: INVOCATION_INTERVAL
        trigger_param: 15
  checkpoint-manager-git:
    triggers:
      - trigger: STAGE_END
      - trigger: INVOCATION_INTERVAL
        trigger_param: 25
```

| Property | Rule |
|---|---|
| Mutability | Set once at run start; never modified during the run. |
| Scope | May change `triggers` only. Never `Class` or `on_failure`. |
| Replacement, not merge | An override supplies the agent's complete trigger list, replacing the declared one. Merging would make it impossible to *remove* a declared trigger, and would leave the effective configuration split across two places rather than stated in one. |
| Class restrictions | A class may declare that only certain triggers are valid for it. `commit` permits `STAGE_END` only; an override naming any other trigger is a configuration error and the run must not start. The restriction belongs to the class because it follows from what the agent produces, not from a per-run preference — a commit is only meaningful at a boundary where some described piece of work is finished, and no other trigger lands on one. See `CommitAgent.md` §4.2. |
| Scope of a class restriction | A class trigger restriction governs **trigger evaluation** and nothing else. It does not constrain out-of-band dispatch, where an agent is reached by explicit instruction rather than because a condition became true. Two agents are dispatched that way — `restore`, always, and `commit` once at run start to establish its destination (`CommitAgent.md` §4.9) — and neither is an exception to the restriction, because neither is a trigger. Reading the restriction as governing every invocation would make the run-start dispatch look like a violation of a rule it is outside of. |
| Unknown agent | An override naming an agent absent from the declaration region is a configuration error; the run must not start. Silently ignoring it would leave the user believing a setting took effect when it did not. |
| Absent block | Every agent uses its declared trigger. This is the common case and the block should normally be absent entirely. |

Overrides deliberately **cannot** change `on_failure` or `Class`, and cannot activate or deactivate an agent. Failure policy is a property of what the agent does — whether the run is still trustworthy without it — not a per-run preference. Class is what the agent *is*, and letting a run restate it would let a run assert that some agent satisfies the checkpoint precondition when it does not. Activation is already expressed by `checkpoints`, and a second mechanism for it would create states where the two disagree.

Recording overrides in the artifact rather than passing them as executor arguments keeps the run self-describing: an orchestrator resuming a run started by a different executor fires the same triggers, because the configuration travelled with the run rather than with the process that started it.

## 7. Failure Policy

Each infrastructure agent declares `on_failure` as either `halt` or `continue`. It applies when the agent returns any status code other than `SUCCESS`.

| Policy | Behaviour |
|---|---|
| `halt` | Stop the run and escalate to the user. The Execution Log row is written first, so the failure is on the record before the run stops. |
| `continue` | Record the row and proceed to the next workflow agent as though the trigger had not fired. |

The policy is per-agent because the correct answer genuinely differs by agent, and a uniform rule is wrong in one direction or the other for every agent it is applied to.

An agent whose job is to preserve restorable state must be `halt`. A run configured with checkpointing enabled, whose checkpoint agent silently failed, is a run that believes it can roll back and cannot — which is precisely the broken promise the orchestration artifact schema forbids when it requires that a recorded checkpoint always names real, restorable content. Continuing produces exactly that state.

An agent whose output is advisory must be `continue`. Halting a healthy run because an advisory check could not complete inverts the cost: the check exists to improve the run, and stopping the run to punish its absence is worse than the absence.

**A `halt` is not a rollback.** The run stops where it is, with its artifact intact and its last completed invocation recorded. What happens next is a human decision.

## 8. Protocol Participation

Infrastructure agents are ordinary subagents in every protocol respect. Specifically:

- They receive a standard Task Invocation Message and return a standard Task Response Message, with the same fields and the same six status codes.
- They consume `global_sequence`. The counter is incremented before dispatch, and the incremented value becomes the `#N` in their `agent_instance_id`, exactly as for workflow agents.
- They echo `agent_instance_id` and `run_id` like any other agent.
- They receive orchestration artifacts through `input_artifacts` / `output_artifacts` and are bound by the same strict-access rule.
- They stamp provenance on artifacts they produce, under the same rules as any other agent.
- They may be dispatched with `human_in_the_loop: true`, and honour it identically.

**On the strict-access rule and workspace inspection.** The rule governs *orchestration artifacts*: an agent writes only what `output_artifacts` names, and treats orchestration artifacts not listed in `input_artifacts` as out of bounds. It has never governed the workspace at large — subagents read project files, search directories, and run tools as a matter of course, which is most of what they exist to do.

Two consequences the concrete designs rely on, stated here once so they are not re-argued per agent:

- **Reading a file that is not an orchestration artifact needs no exception.** An infrastructure agent searching the workspace for the deployed orchestrator, or inspecting the repository it is checkpointing, is doing ordinary subagent work.
- **Reading an orchestration artifact does need one, and it must be stated in both places.** `Orchestration.md` belongs to the orchestrator, and the orchestrator's own instructions say subagents never access it. An agent that reads one — its own run's or another's — must have that exception written into its design *and* into the orchestrator's constraint, so the two never silently disagree. The rationale behind the original rule is that subagents must not make routing decisions from orchestration state; an exception is only defensible where the agent makes no routing decisions at all.
The one thing an executor does differently is *why* it dispatched them and *what it does with the response* — both governed by §5 and §7 rather than by a workflow routing table.

**They are not exempt from the sequence counter.** Exempting them would require a second identifier scheme for their instances, would leave gaps or collisions in the Execution Log's `Seq` column, and would break the artifact schema's existing recovery rule that `global_sequence` is reconcilable against the highest `Seq` in the log. The cost of including them is one extra row per firing; the cost of excluding them is a special case in every consumer.

## 9. Execution Log Representation

An infrastructure agent invocation produces one ordinary Execution Log row, appended after it completes, with the phase and stage current at the time it ran:

```markdown
| 14 | Implementation#14 | EXECUTION | Implementation.2 | SUCCESS | 2026-01-29T12:45:00Z | Implemented updateProfile endpoint | Stage-2/Plan.md, Contracts.md | - |
| 15 | checkpoint-manager-git#15 | EXECUTION | Implementation.2 | SUCCESS | 2026-01-29T12:46:00Z | Committed checkpoint for stage 2 | - | 4f1a08d |
```

No new column marks a row as infrastructure. The agent name already identifies it, and a consumer that needs to distinguish them matches against the declaration region (§3.1) it already reads. A column here would encode information the row already carries.

**This is not an argument against columns in general.** The orchestration review agent's design adds an `Inputs` column to this same table, and does so correctly: it records what a dispatch was given, which is a fact nothing anywhere else preserves. The distinction is whether the column adds information or restates it. An infrastructure flag restates the agent name; `Inputs` has no other source. The examples above are shown with `Inputs` populated, since both changes land together.

**The checkpoint reference goes on the infrastructure agent's own row.** This is a deliberate change from the orchestration artifact schema as currently written, and §10 records it as an amendment. The reasoning: that schema describes a checkpoint as taken "right after invocation N" and its reference recorded in *invocation N's own row*, which is coherent only if the orchestrator preserves content itself, inline, while processing that invocation. Once checkpointing is an infrastructure agent, the checkpoint necessarily happens after row N has already been appended — so recording it on row N means editing a written row, in a section the same schema declares strictly append-only and never revisited.

Putting the reference on the checkpoint agent's own row avoids the contradiction entirely. Nothing is ever edited, the append-only guarantee holds without exception, and a deterministic runner never needs to seek back and rewrite. The row sits immediately after invocation N in any case, so nothing is lost in interpretation: the checkpoint restores the state as of the point in the log where its row appears.

## 10. Amendments to the Orchestration Artifact Schema

The infrastructure agent class itself requires exactly one change to `OrchestrationArtifactFormat.md`:

| § | Current text | Amendment |
|---|---|---|
| §4 | Frontmatter fields are enumerated without any infrastructure configuration. | Adds the optional `infrastructure_overrides` block (§6.2), set once at run start, permitting a run to replace a declared agent's `triggers` list without redeployment. |

Run-start fields belonging to a specific class — `checkpoints`, and the `commits` and `commit_branch` pair introduced by `CommitAgent.md` — are amendments those designs own, for the reason given below.

An earlier draft of this section listed three further amendments — the `Checkpoint` column's meaning, the rollback mechanism, and the handling of `checkpoints: enabled` with no mechanism present. All three are properties of checkpointing specifically, not of the infrastructure agent class, and belong in the checkpoint agents' own design, which is where they now live. A class document specifying its instances' amendments is the same category error as a class document specifying its instances' behaviour.

## 11. Non-Goals

- **Any concrete infrastructure agent.** `checkpoint-manager-git` and `orchestration-review` appear here only as examples of the class. Their commit conventions, drift heuristics, output formats, and status code mappings belong to their own designs.
- **The mechanism that preserves or restores content.** This document specifies that a checkpoint agent returns a content-reference and where that reference is recorded. What a reference denotes, and how it is resolved back into restored content, is the concrete agent's concern.
- **How a rollback is requested and executed.** No trigger in §4's vocabulary fires automatically for rollback. The restore agent (`checkpoint-restore-git`) is declared as an infrastructure agent with `Class = restore` so that it can be discovered and selected through the same machinery as any other infrastructure agent — but it carries a `MANUAL` trigger, which per §4 means it never fires automatically, and it is excluded from trigger evaluation by class (§3.2). Being an infrastructure agent is what makes it discoverable and selectable; the `MANUAL` trigger and class-based exclusion are what keep it from firing unless a human explicitly requests it. The non-goal is the rollback mechanism itself — how a checkpoint target is chosen, how the user requests the restore, and how the artifact is reconciled afterwards — not the restore agent's class membership.
- **Infrastructure agents that modify the workflow.** Nothing here lets an infrastructure agent alter routing, skip a phase, or change which workflow agent runs next. An agent that detects a problem reports it; acting on the report is a routing decision belonging to the executor and, where it matters, to a human.
- **Cross-run coordination.** Two concurrent runs each fire their own triggers against their own artifacts. Contention over a resource both runs' infrastructure agents touch — a shared git branch being the obvious case — is not addressed here.
- **A dedicated infrastructure agent template.** Infrastructure agents use the same canonical section structure as every other agent. The frontmatter fields in §3.2 are additive; they do not imply a separate template.

## 12. Open Items

- Whether the `on_failure: halt` path should attempt any retry before halting, or halt on first failure. Retry semantics for ordinary agents are already tiered by error code; whether infrastructure agents inherit that tiering or bypass it is unresolved.
- Whether an override should be able to *add* a trigger without restating the declared ones. Replacement is the current rule (§6.2) and is unambiguous, but it means a run wanting one extra trigger must repeat the rest.
- Whether a class should be able to restrict `on_failure` as well as `trigger` (§6.2). `commit` establishes that classes can constrain their configuration; whether that generalises to the failure policy is untested, and no current class needs it.
- Whether an agent having **two dispatch modes** should be expressible in the declaration region (§3.1), which currently describes only trigger-driven behaviour. `commit` is the first such agent: its setup mode (`CommitAgent.md` §4.9) has different preconditions from its commit mode and a different failure consequence — a failed setup stops the run, while its declared `on_failure: continue` governs only trigger-driven firing. Nothing breaks today, because the run-start sequence dispatches setup explicitly and does not consult the region for it. But the region currently reads as a complete statement of what an agent does, and for this agent it is not. Whether to extend the region or to accept that out-of-band modes live in the agent's own design is open.

**Resolved:** whether an infrastructure agent may declare more than one trigger. It may (§3.2). The single-trigger rule survived only as long as no agent needed otherwise; `checkpoint-manager-git` needs `STAGE_END` and `INVOCATION_INTERVAL` simultaneously once a `commit`-class agent can be running, because a stage redo returns to a boundary while in-stage rollback needs points between boundaries, and neither trigger supplies the other's case. Simultaneous matches collapse to one dispatch (§5), so the change costs nothing downstream.

**Resolved:** whether a third class beyond `checkpoint` and `review` is foreseeable. It arrived: `commit` (§3.2), specified in `CommitAgent.md`. It met the standard the vocabulary was closed to enforce — introduced alongside the rules that need to distinguish it, rather than in anticipation of a consumer. Opening the vocabulary also produced two generalisations the original pair did not require: per-class activation switches (§6.1) and class-level trigger restrictions (§6.2). Both are now framed as properties any class may have, so a fourth class needs no further change to this document.

**Resolved:** whether `infrastructure` should be a boolean or carry a value. It carries the class (§3.2). The boolean marked membership but not kind, which left the activation rule in §6.1 undecidable — an executor could see that infrastructure agents existed without being able to tell whether any of them satisfied the run's checkpointing precondition.
