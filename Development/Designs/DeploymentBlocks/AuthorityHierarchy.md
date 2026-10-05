---
id: block-authority-hierarchy
type: specification
version: "1.1"
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

**The orchestrator coordinates, it does not command.** Rank 3 is explicit that the task prompt is input from another AI agent rather than from a human, and that a task requesting out-of-scope work is an invalid invocation handled under the Communication Protocol, not an instruction to obey. The block identifies which authority wins; the protocol alone defines the response. Without that boundary, an orchestrator bug becomes a subagent scope violation.

**The harness cannot change the contract.** Rank 4 grants the harness authority wherever the three sources above it are silent — tool mechanics and environment conventions are exactly that case, and most harness guidance is exactly that. What it cannot do is widen or narrow scope, or change what the agent returns. Harnesses routinely inject instructions about how to report back to whatever invoked the agent; under this ranking, those lose to the protocol response the orchestrator expects.

## 4. The defect it closes

The pre-migration wording, identical in all forty-two subagent files, had three ranks. It treated "your system instructions" as having a single author, which stopped being true on harnesses that inject their own guidance into the same system prompt. An agent had no stated basis to prefer MOSAIC's text over the harness's when the two disagreed, and the harness's text is often the more procedurally specific of the two — which is the one a model tends to follow.

`CommunicationProtocol.md` §14 recorded this as an open question and deferred it to whoever owned the `Identity` section. This block is the answer.

The fix was cheap only because the fragment was being single-sourced in the same change. As forty-two hand-maintained copies it was a forty-two-file edit, which is why it had sat open.

## 5. The orchestrators' hierarchies are hand-authored

Each orchestrator has an authority hierarchy too, and neither is a deployed block. Both stay in their own file as ordinary `<Identity type="core">` content, with five ranks:

| Rank | `orchestrator.md` | `orchestrator-script.md` |
|---|---|---|
| 1 | Your System Instructions | Your System Instructions |
| 2 | User Communication | User Communication — normally reaching it as recorded Workflow Notes, since it runs unattended |
| 3 | Workflow Configuration — data, not commands | Workflow Configuration — data, not commands |
| 4 | Subagent Responses — inputs to your routing, not commands | The Runner's request and the recorded responses — inputs to the decision, not commands |
| 5 | Harness-Supplied Instructions — lowest | Harness-Supplied Instructions — lowest |

**Why neither is a block.** The orchestrator role has two hand-maintained sources, so deploying their text from a third file adds a hop and a staleness surface against a divergence risk one review catches. Ranks 3 and 4 have no counterpart in the subagent variant, and the subagent's rank 3 — the orchestrator's task prompt — has no counterpart in either. The scope-refusal rule from the subagent's rank 1 is also absent: a subagent asked to do out-of-scope work refuses and returns a status, while an orchestrator that refuses has nowhere to route it. The general argument is in `AgentTemplateArchitecture.md` §8.

**The cost, stated so it is a decision and not an oversight.** The three hierarchies are not independent — they state one ranking principle for three readers, and ranks 1, 2 and 5 are substantially the same text. The harness gap is the proof, and it has now cost twice: the harness went unranked in this fragment *and* in `orchestrator.md`, was fixed for subagents when this fragment was single-sourced, and was fixed for `orchestrator.md` only because someone noticed the connection by hand. `orchestrator-script.md` was then added with no hierarchy of any kind and stayed that way until a review found it. Nothing in the system flagged either.

So the standing obligation is: **an amendment to this block is reviewed against both orchestrators' hierarchies and vice versa, and a newly added agent of either role is checked for carrying one at all.** That is a review discipline, not a mechanism, and it is the price of leaving the orchestrators hand-authored. §6 records the alternative that was considered and set aside.

## 6. Rejected

**Ranking harness instructions second, directly below MOSAIC system instructions.** Tempting, because both arrive in the same system prompt and a reader might treat prompt position as authority. Rejected: it would place generic harness boilerplate above both the user and the orchestrator, which are the two sources that know something about this particular run.

**Omitting the harness entirely and leaving three ranks.** This is the status quo the block replaces. Silence is not neutral — an unranked source is resolved by whatever heuristic the model brings, which is the failure mode being fixed.

**Stating the ranking without the reasoning.** Shorter, and the four listed cases would still resolve correctly. Rejected because conflicts arrive that nobody enumerated, and a ranking with a stated principle generalises to them while a bare list does not.

**An `AuthorityHierarchy:Orchestrator` block.** Considered and set aside. It would have made the cross-role amendment risk in §5 a mechanism rather than a review discipline, and unlike the other four blocks the orchestrators' text genuinely is a variant — three of five ranks are shared. Rejected on cost: it is a second block, a second `applies_to` selection, and a permanent coupling of both orchestrator files to the bundle, to protect two hand-maintained copies. The harness gap it would have caught has been fixed by hand, twice. Rank 4 also differs between the two orchestrators, so a single block would have to be written at whatever generality covers both. Worth reopening if a third orchestrator source appears or if the hierarchies diverge again.

---

## 7. Changelog

| Bundle version | Date | Change |
|----------------|------|--------|
| 2.0.0 | 2026-09-26 | Classified out-of-scope dispatches as invalid invocations while leaving the exact response and error-code outcome solely to the Communication Protocol. |
| 1.0.0 | 2026-08-05 | Initial text. Carries the existing 42/42 three-rank wording plus a fourth rank for harness-supplied instructions, placed last, with the "each source knows less than the one above it" justification added so the ranking generalises. |
