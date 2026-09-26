---
id: block-protocol-constraints
type: specification
version: "3.0"
name: "Block — ProtocolConstraints (Removed)"
description: "Why the ProtocolConstraints bundle block was removed: its protocol-restatement bullets were redundant, and its remaining content moved to the protocol and to ExecutionPhilosophyCommon."
author: MOSAIC
status: Superseded
---

# Block — `ProtocolConstraints:Subagent` (Removed)

| | |
|---|---|
| **Block** | `ProtocolConstraints:Subagent` |
| **Status** | **Removed in bundle 2.0.0 / AgentTemplateArchitecture v2.3** |
| **Was in** | `Catalog/DeployedSections.md` |

This block and its `<ProtocolConstraints type="managed">` region have been removed from the system. This document preserves the reasoning for the removal and the original design rationale.

## 0. Why this block was removed

Six of the block's eight bullets restated rules already present in the Communication Protocol's deployed text (Key Rules 1, 8, 9, the artifact access section, and the status code table). The remaining two were:

- **ASCII-only outputs** -- moved to the Communication Protocol as Key Rule 10 and a new "Artifact Format" subsection. It was always a protocol rule about artifact and message format, not a conduct constraint.
- **Single responsibility** ("note work that belongs to another agent; do not do it yourself") -- moved to `ExecutionPhilosophyCommon:Subagent` in the bundle. It is execution discipline, not a protocol rule.

The block existed because the previous MOSAIC generation carried a general constraints reminder section at the end of every agent. When the new generation split content into protocol vs. bundle, the positional habit carried forward without anyone asking whether these constraints were protocol content that happened to sit in the constraints section.

The architectural problem was that ProtocolConstraints versioned with the bundle, independently of the protocol it restated. A protocol change that updated artifact access rules or status code semantics required a separate, easy-to-forget update to ProtocolConstraints. A stale ProtocolConstraints in the high-attention Constraints section would then contradict the updated protocol in the Communication Protocol section -- producing exactly the failure the repetition was supposed to prevent, but in the wrong direction.

The bundle's membership test (DeployedSectionsBundle.md section 2) requires that a block "is not a contract" and that "nothing in it defines something two parties must agree on." ProtocolConstraints failed this test for six of eight bullets. The bundle spec acknowledged this with a hedging paragraph that has now been removed.

---

The original design rationale follows for historical reference.

---

## 1. What the block does

The block places shared imperatives at the head of the section whose purpose is rules of that kind. Its artifact rules identify the current run, prohibit directory discovery, state input/output permissions, protect existing outputs from blind replacement, and preserve autonomy over project files. The remaining bullets carry the existing ASCII, JSON-response, status-code, and single-responsibility constraints.

The artifact, project-file, JSON-response, and status-code bullets compress rules the orchestration contract states a few inches up the same file. The ASCII-only and single-responsibility bullets are shared conduct constraints rather than contract restatements.

## 2. Why deliberate repetition is correct here

"One fact, one authority" is principle 4 of the schema, and this block appears to break it. It does not, and the distinction is worth stating precisely because it is the one place the schema spends repetition on purpose.

The contract's deployed region **states the rules**. It is a specification: complete, precise, and written to be correct rather than to be obeyed at a particular moment. This block **is the agent's constraint list**, at the top of the section a model consults when asking "what am I not allowed to do?"

Three things justify the spend:

**These are the rules agents are observed to break.** Repetition is not free and it is not worthless. It is spent where compliance is weakest, and artifact-boundary violations and invented status codes are the two most common failures in practice.

**Compression changes the form.** The contract's artifact access rules are a specification with a permission matrix and edge cases. Here they are a compact group of imperatives an agent can hold while working. That is a different artefact serving a different moment, not a second copy of the same one.

**Position is part of the instruction.** A model attends unevenly across a long prompt. A rule in `Constraints` is found by an agent looking for constraints; the same rule inside the contract region is found by an agent reading the contract, which happens once, early, before it knows what the task is.

The line the contract-restating bullets do *not* cross: they restate rather than extend. If one disagrees with the contract, the contract is right and the block is defective. The separately identified conduct constraints do not define message or artifact permissions between two parties.

## 3. The artifact-access bullets

The artifact bullets form one permission model; omitting any part changes its meaning.

**Orchestration directories are closed.** Every file under an `Orchestration-*` directory is workflow state. The agent may access only exact listed paths in the current run's `Orchestration-{run_id}/` directory and does not enumerate the directory to discover more. This keeps prior runs and other workflow steps out of its context.

**List membership grants operations.** `input_artifacts` grants read access; `output_artifacts` grants read and write access. Output read access matters when a later review round or continuation writes to an artifact that already contains useful state.

**Existing output is state, not an empty destination.** The existence check prevents a model from treating every output-shaped path as a new file. Reading before replacement or truncation is required unless the task explicitly calls for wholesale replacement, while append-only or other safe operations remain the agent's judgement.

**Project files are open.** Files outside every `Orchestration-*` directory remain autonomous within the agent's scope. Without this positive half, the closed artifact rule reads like a general prohibition on touching files, and implementation agents become unable to do their jobs.

## 4. The single-responsibility bullet is not a restatement

"Note work that belongs to another agent; do not do it yourself" states nothing in the contract. It is the single-responsibility architecture, expressed as the behaviour it demands at the moment an agent notices adjacent work it could easily do.

It belongs in a shared block rather than in each agent's own list because it is identical for every agent — what varies is which work is adjacent, and that is what the `Scope` DO NOT list in `Identity` covers.

## 5. ASCII-only outputs

Orchestration artifacts and JSON responses are limited to ASCII so logs and later user-driven processing do not depend on Unicode handling. Non-ASCII source text is converted to an ASCII equivalent when reproduced; project source files themselves are unchanged unless the task requires otherwise.

## 6. What was deleted rather than moved here

The fragment *"Always end with a JSON status block"* appeared in all forty-two files, in `OutputFormat`, introducing the worked JSON examples that section no longer carries. It was deleted, not migrated.

Its instruction is stated twice already — as a bullet in this block and as a key rule of the contract itself — and the sentence it was introducing no longer exists. A third statement whose only distinguishing feature was its position before examples that were removed has no remaining case for existing.

This is the only one of the eight measured fragments that was deleted rather than single-sourced. The full reasoning is in `Development/Analysis/AgentBodyDrift.md`.

---

## 7. Changelog

| Bundle version | Date | Change |
|----------------|------|--------|
| 2.0.0 | 2026-09-26 | **Block removed.** Six protocol-restatement bullets deleted (redundant with Communication Protocol Key Rules and artifact access section). ASCII-only rule moved to Communication Protocol Key Rule 10 and Artifact Format subsection. Single-responsibility bullet moved to `ExecutionPhilosophyCommon:Subagent`. The `<ProtocolConstraints type="managed">` region removed from the agent template architecture. |
| 2.0.0 (pre-removal) | 2026-09-26 | Artifact access now distinguishes classification from permission. All `Orchestration-*` directories are closed state; only exact current-run paths may be accessed. Inputs are readable, outputs are readable and writable, and existing outputs are checked before writing and protected from blind replacement. Project autonomy now applies only outside orchestration directories. ASCII wording now defines the exact range and conversion behavior. |
| 1.0.0 | 2026-08-05 | Initial text. Consolidates measured fragments 2 (JSON response and status code discipline, 42/42) and 6 (orchestration artifact and project file access, 39/42), taking the contract-correct variant of the drifted pair, and adds the single-responsibility bullet. |
