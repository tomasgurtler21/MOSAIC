---
id: block-protocol-constraints
type: specification
version: "1.0"
name: "Block — ProtocolConstraints"
description: "Five bullets at the head of every subagent's Constraints section that restate contract rules on purpose. Why deliberate repetition is correct here and nowhere else."
author: MOSAIC
status: Draft
---

# Block — `ProtocolConstraints:Subagent`

| | |
|---|---|
| **Block** | `ProtocolConstraints:Subagent` |
| **Fills** | `[[DEPLOYED:ProtocolConstraints]]` — placement in `AgentTemplateArchitecture.md` §2.5 |
| **Applies to** | `subagent` |
| **Text lives in** | `Agents/Generic/DeployedSections.md` |

The block's text is in the bundle and nowhere else. This document is its reasoning.

---

## 1. What the block does

Five imperatives, at the head of the section whose entire purpose is rules of that kind. They cover, in order: orchestration artifact access, project file access, never skipping the JSON response, never inventing status codes, and noting rather than doing another agent's work.

Every one of them except the last compresses something the orchestration contract already states a few inches up the same file.

## 2. Why deliberate repetition is correct here

"One fact, one authority" is principle 4 of the schema, and this block appears to break it. It does not, and the distinction is worth stating precisely because it is the one place the schema spends repetition on purpose.

The contract's deployed region **states the rules**. It is a specification: complete, precise, and written to be correct rather than to be obeyed at a particular moment. This block **is the agent's constraint list**, at the top of the section a model consults when asking "what am I not allowed to do?"

Three things justify the spend:

**These are the rules agents are observed to break.** Repetition is not free and it is not worthless. It is spent where compliance is weakest, and artifact-boundary violations and invented status codes are the two most common failures in practice.

**Compression changes the form.** The contract's artifact access rules are a specification with a role matrix and edge cases. Here they are two lines an agent can hold while working. That is a different artefact serving a different moment, not a second copy of the same one.

**Position is part of the instruction.** A model attends unevenly across a long prompt. A rule in `Constraints` is found by an agent looking for constraints; the same rule inside the contract region is found by an agent reading the contract, which happens once, early, before it knows what the task is.

The line this does *not* cross: the block restates, it never extends. No rule appears here that the contract does not state. If the two ever disagree, the contract is right and this block is a defect — which is why the membership test in `DeployedSectionsBundle.md` §2 admits it. A version mismatch here breaks nothing on the wire.

## 3. The two access bullets

The first two bullets are the pair that matters most, and they are stated as a matched positive and negative because either alone misleads.

**Orchestration artifacts are closed.** An agent never touches one that is not named in its `input_artifacts` or `output_artifacts`. This is what makes the artifact flow in a workflow table a real constraint rather than a suggestion, and it is what lets the orchestrator reason about what a subagent could possibly have read.

**Project files are open.** Anything not named as an orchestration artifact may be read, modified, or created. Without this stated, the first bullet reads as a general prohibition on touching files, and an implementation agent has no way to tell that its entire job is exempt. The measured 39/42 drift on this pair suggests authors had been rewording exactly this ambiguity by hand.

## 4. The fifth bullet is not a restatement

"Note work that belongs to another agent; do not do it yourself" states nothing in the contract. It is the single-responsibility architecture, expressed as the behaviour it demands at the moment an agent notices adjacent work it could easily do.

It belongs in a shared block rather than in each agent's own list because it is identical for every agent — what varies is which work is adjacent, and that is what the `Scope` DO NOT list in `Identity` covers.

## 5. What was deleted rather than moved here

The fragment *"Always end with a JSON status block"* appeared in all forty-two files, in `OutputFormat`, introducing the worked JSON examples that section no longer carries. It was deleted, not migrated.

Its instruction is stated twice already — as a bullet in this block and as a key rule of the contract itself — and the sentence it was introducing no longer exists. A third statement whose only distinguishing feature was its position before examples that were removed has no remaining case for existing.

This is the only one of the eight measured fragments that was deleted rather than single-sourced. The full reasoning is in `Development/Analysis/AgentBodyDrift.md`.

---

## 6. Changelog

| Bundle version | Date | Change |
|----------------|------|--------|
| 1.0.0 | 2026-08-05 | Initial text. Consolidates measured fragments 2 (JSON response and status code discipline, 42/42) and 6 (orchestration artifact and project file access, 39/42), taking the contract-correct variant of the drifted pair, and adds the single-responsibility bullet. |
