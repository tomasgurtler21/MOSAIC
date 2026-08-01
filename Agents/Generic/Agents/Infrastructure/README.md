# Infrastructure

Agents that support orchestration itself rather than the task the run is about.

## Purpose

Infrastructure agents belong to no workflow. They are declared once for an orchestrator, in its `[[INJECTION:InfrastructureAgents]]` region, and are invoked because a **trigger condition** became true — not because a status code routed to them. They perform orchestration-support work: preserving restorable state, committing completed work, checking the run's own bookkeeping.

They are a new *reason to invoke*, not a new *kind of invocation*. In every protocol respect they are ordinary subagents: same communication protocol, same instance-id scheme, same Execution Log rows, same sequence counter.

## Agents

| ID | Agent | Version | Class | Triggers | On Failure | Description |
|----|-------|---------|-------|----------|------------|-------------|
| 36 | [checkpoint-manager-git](./checkpoint-manager-git.md) | 1.0.0 | `checkpoint` | STAGE_END, INVOCATION_INTERVAL(10) | halt | Commits a restorable checkpoint of the working tree to a private ref namespace; never modifies files |
| 38 | [commit-manager-git](./commit-manager-git.md) | 1.0.0 | `commit` | STAGE_END | continue | Commits completed stage work to the user's branch with a prose message; not a restore point |
| 39 | [orchestration-review](./orchestration-review.md) | 1.0.0 | `review` | INVOCATION_INTERVAL(30) | continue | Advisory — reports observations about the run's own bookkeeping and routing; never returns an instruction |
| 37 | [checkpoint-restore-git](./checkpoint-restore-git.md) | 1.0.0 | — | none | — | **Not an infrastructure agent.** Restores the working tree to a checkpoint; dispatched only on explicit human decision |

### Why `checkpoint-restore-git` lives here but is not an infrastructure agent

It is filed alongside its counterpart because the two are one mechanism, and reading either without the other is incomplete. It carries no `infrastructure`, `triggers`, or `on_failure` field, and that absence is the mechanism rather than an omission: an agent with no trigger cannot be fired by trigger evaluation, so there is no configuration — deployed or per-run — under which it runs unattended.

Capture is safe and restore is dangerous. The agent that runs unattended is the one that cannot destroy anything; the agent that can destroy things never runs unattended.

## Class Vocabulary

`infrastructure` carries a closed-vocabulary class value rather than a boolean, so activation preconditions are decidable by string comparison rather than by judgement.

| Class | Meaning | Activation |
|-------|---------|------------|
| `checkpoint` | Preserves restorable content and returns a content-reference. Never writes to a branch the user works on. | Run's `checkpoints` field |
| `commit` | Writes completed work into the user's own history. Produces no restore point and is never a restore target. | Run's `commits` field; restricted to the `STAGE_END` trigger |
| `review` | Inspects the run and reports observations. Produces no artifact and never routes. | None — deactivating it is a deployment decision |

**At most one agent per gated class** may be declared for an orchestrator. Two differently-named agents of the same gated class both fire on the same boundary and both populate a column that is supposed to name one thing.

**A `commit`-class agent does not satisfy the checkpointing precondition.** Its commits are not restore targets, so treating one as the rollback mechanism would let a run start believing it can roll back when nothing can. A run wanting rollback declares a `checkpoint`-class agent, whatever else is running.

## Declaration Order Matters

Where co-triggered agents differ in `on_failure`, declaration order decides how much of a boundary's work is already done when a `halt` lands. **Declare `halt` agents first.** Checkpoint-before-commit means a halt lands before anything has entered the user's history; the reverse produces a committed stage with no restore point marking its boundary — the single state the pairing exists to prevent.

## What Infrastructure Agents Do

- Fire on trigger conditions decidable from the orchestration artifact alone
- Consume the global sequence counter and produce ordinary Execution Log rows
- Operate unattended, with no user-interaction tools
- Return standard Task Response Messages with the standard status codes

## What Infrastructure Agents Do NOT Do

- Appear in any workflow routing table
- Alter routing, skip a phase, or change which workflow agent runs next
- Cause other infrastructure agents to fire — completions never evaluate triggers, so evaluation always terminates
- Prompt the user on their own initiative

## Design Reference

- [InfrastructureAgentConcept.md](../../../../Development/Designs/InfrastructureAgentConcept.md) — the class, declaration, triggers, evaluation, failure policy
- [CheckpointAgents.md](../../../../Development/Designs/CheckpointAgents.md) — capture and restore
- [CommitAgent.md](../../../../Development/Designs/CommitAgent.md) — the `commit` class
- [OrchestrationReviewAgent.md](../../../../Development/Designs/OrchestrationReviewAgent.md) — the `review` class
