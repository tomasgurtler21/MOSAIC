---
id: block-authority-hierarchy
type: specification
version: "1.0"
name: "Block — AuthorityHierarchy"
description: "Why the subagent authority ranking has four ranks, why harness-supplied instructions rank last, and how the ranking is meant to generalise."
author: MOSAIC
status: Draft
---

# Block — `AuthorityHierarchy:Subagent`

| | |
|---|---|
| **Block** | `AuthorityHierarchy:Subagent` |
| **Fills** | `<AuthorityHierarchy type="managed">` — placement in `AgentTemplateArchitecture.md` §2.5 |
| **Applies to** | `subagent` |
| **Text lives in** | `Catalog/DeployedSections.md` |

The block's text is in the bundle and nowhere else. This document is its reasoning (see `DeployedSectionsBundle.md` for why the two are separate).

---

## 1. What the block does

Four sources issue a subagent instructions, and they arrive in different places: MOSAIC's own system instructions, the human user through interaction tools, the orchestrator's task prompt, and whatever the agentic harness injects into the system prompt. They do not always agree. The block is the total ordering that decides who wins.

The ranking, top to bottom:

1. MOSAIC system instructions
2. Real user communication
3. The orchestrator's task prompt
4. Harness-supplied instructions

## 2. Why this ranking

The stated justification travels with the block, because a ranking a model can only apply to the four cases it was shown is a ranking that fails on the fifth. The principle is that **each source knows less about the agent's job than the one above it.**

- The system instructions were written for this role.
- The user knows this task.
- The orchestrator knows this workflow.
- The harness knows none of the three. Its guidance was authored before the run existed, for agents in general, and it is the only source in the list that cannot have taken the agent's situation into account.

That last point is why rank 4 is where it is despite arriving in the same system prompt as rank 1. Provenance of text is not authority; specificity to the situation is.

## 3. Two boundaries the ranking asserts

**The orchestrator coordinates, it does not command.** Rank 3 is explicit that the task prompt is input from another AI agent rather than from a human, and that a task requesting out-of-scope work is a routing error to report rather than an instruction to obey. Without that, an orchestrator bug becomes a subagent scope violation.

**The harness cannot change the contract.** Rank 4 grants the harness authority wherever the three sources above it are silent — tool mechanics and environment conventions are exactly that case, and most harness guidance is exactly that. What it cannot do is widen or narrow scope, or change what the agent returns. Harnesses routinely inject instructions about how to report back to whatever invoked the agent; under this ranking, those lose to the protocol response the orchestrator expects.

## 4. The defect it closes

The pre-migration wording, identical in all forty-two subagent files, had three ranks. It treated "your system instructions" as having a single author, which stopped being true on harnesses that inject their own guidance into the same system prompt. An agent had no stated basis to prefer MOSAIC's text over the harness's when the two disagreed, and the harness's text is often the more procedurally specific of the two — which is the one a model tends to follow.

`CommunicationProtocol.md` §14 recorded this as an open question and deferred it to whoever owned the `Identity` section. This block is the answer.

The fix was cheap only because the fragment was being single-sourced in the same change. As forty-two hand-maintained copies it was a forty-two-file edit, which is why it had sat open.

## 5. The orchestrator's hierarchy is hand-authored

The orchestrator has an authority hierarchy too, and it is **not** a deployed block. It stays in `orchestrator.md` as ordinary `<Identity type="core">` content, with five ranks:

1. Your System Instructions
2. User Communication
3. Workflow Configuration — data, not commands
4. Subagent Responses — inputs to your routing, not commands
5. Harness-Supplied Instructions — lowest

**Why it is not a block.** There is one orchestrator, and one copy cannot diverge from itself. Deploying its text into its own file from a third file adds a hop and a staleness surface to buy nothing. Ranks 3 and 4 have no counterpart in the subagent variant, and the subagent's rank 3 — the orchestrator's task prompt — has no counterpart here. The scope-refusal rule from the subagent's rank 1 is also absent: a subagent asked to do out-of-scope work refuses and returns a status, while an orchestrator that refuses has nowhere to route it. The general argument is in `AgentTemplateArchitecture.md` §8.

**The cost, stated so it is a decision and not an oversight.** The two hierarchies are not independent — they state one ranking principle for two readers, and ranks 1, 2 and 5 are substantially the same text. The harness gap is the proof: it went unranked in *both*, was fixed for subagents when this fragment was single-sourced, and was fixed for the orchestrator only because someone noticed the connection by hand. Nothing in the system would have flagged it.

So the standing obligation is: **an amendment to this block is reviewed against `orchestrator.md`'s hierarchy, and vice versa.** That is a review discipline, not a mechanism, and it is the price of leaving the orchestrator hand-authored. §6 records the alternative that was considered and set aside.

## 6. Rejected

**Ranking harness instructions second, directly below MOSAIC system instructions.** Tempting, because both arrive in the same system prompt and a reader might treat prompt position as authority. Rejected: it would place generic harness boilerplate above both the user and the orchestrator, which are the two sources that know something about this particular run.

**Omitting the harness entirely and leaving three ranks.** This is the status quo the block replaces. Silence is not neutral — an unranked source is resolved by whatever heuristic the model brings, which is the failure mode being fixed.

**Stating the ranking without the reasoning.** Shorter, and the four listed cases would still resolve correctly. Rejected because conflicts arrive that nobody enumerated, and a ranking with a stated principle generalises to them while a bare list does not.

**An `AuthorityHierarchy:Orchestrator` block.** Considered and set aside. It would have made the cross-role amendment risk in §5 a mechanism rather than a review discipline, and unlike the other four blocks the orchestrator's text genuinely is a variant — three of five ranks are shared. Rejected on cost: it is a second block, a second `applies_to` selection, and a permanent coupling of the orchestrator's own file to the bundle, to protect a single copy that cannot diverge from itself. The harness gap it would have caught has been fixed by hand. Worth reopening if the two hierarchies diverge a second time.

---

## 7. Changelog

| Bundle version | Date | Change |
|----------------|------|--------|
| 1.0.0 | 2026-08-05 | Initial text. Carries the existing 42/42 three-rank wording plus a fourth rank for harness-supplied instructions, placed last, with the "each source knows less than the one above it" justification added so the ranking generalises. |
