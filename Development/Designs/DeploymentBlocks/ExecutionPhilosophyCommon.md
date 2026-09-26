---
id: block-execution-philosophy-common
type: specification
version: "1.0"
name: "Block — ExecutionPhilosophyCommon"
description: "The three shared subagent postures: use the available context for the current task, preserve continuation memory in artifacts, and respect single-responsibility boundaries."
author: MOSAIC
status: Draft
---

# Block — `ExecutionPhilosophyCommon:Subagent`

| | |
|---|---|
| **Block** | `ExecutionPhilosophyCommon:Subagent` |
| **Fills** | `<ExecutionPhilosophyCommon type="managed">` — placement in `AgentTemplateArchitecture.md` §2.5 |
| **Applies to** | `subagent` |
| **Text lives in** | `Catalog/DeployedSections.md` |

The block's text is in the bundle and nowhere else. This document is its reasoning.

---

## 1. What the block does

Three bullets stating the working posture every subagent shares: how to treat its context window, where its memory lives, and that it respects single-responsibility boundaries.

## 2. Context Management

The bullet tells the agent it may spend its full context window on this task, because follow-up work is handled by spawning new instances.

It exists because the default posture is the opposite one. A model that suspects it may be asked more later rations — it reads less of a file than it should, summarises when it should quote, and stops investigating early. In a hub-and-spoke architecture that caution is pure loss: the agent is invoked once, it will never be asked a follow-up, and whatever it did not read is simply not known.

This was 42/42 and clean. It is in the bundle because it is identical, not because it was broken.

## 3. Memory via Artifacts

Input and output artifacts are the persistent memory between invocations. Anything a successor needs goes into an artifact, not into the response.

The second half is the operative part. An agent that has just finished good work naturally wants to explain it, and the `status_message` is the field in front of it — so continuation context ends up in a routing message that the next agent never sees, instead of in the artifact that it does. The bullet names the wrong destination explicitly rather than only naming the right one.

At 41/42 this was the least-drifted of the four drifting fragments, and the single divergence was a reworded restatement rather than a changed rule.

## 4. Single Responsibility

"Note work that belongs to another agent; do not do it yourself" states nothing in the contract. It is the single-responsibility architecture, expressed as the behaviour it demands at the moment an agent notices adjacent work it could easily do.

It belongs in a shared block rather than in each agent's own list because it is identical for every agent — what varies is which work is adjacent, and that is what the `Scope` DO NOT list in `Identity` covers.

This bullet was originally in `ProtocolConstraints:Subagent` and moved here when that block was removed (bundle 2.0.0). It was never a protocol rule — it is execution discipline about how an agent scopes its own work.

## 5. What stays agent-specific

Everything after the block and the `ContextLimits` injection. "Investigation only: report observations, not assessments." "Escalate, don't fight: if the tests seem wrong, return `NEEDS_CLARIFICATION` rather than working around them." These are the section's reason to exist and they belong to the agent.

Status selection is not shared execution philosophy. Generic meanings and routing semantics belong to the Communication Protocol; each agent's concrete mapping belongs to its `ErrorHandling` section. A philosophy bullet earns its place by saying something about *this* agent's work. If a future measurement finds one identical across all agents, it is a candidate for this block only when it does not restate the contract.

## 5. Membership

Different versions of this block on an orchestrator and a subagent break nothing. It defines no message field, status meaning, routing implication, permission, or other fact two parties must agree on.

---

## 7. Changelog

| Bundle version | Date | Change |
|----------------|------|--------|
| 2.0.0 | 2026-09-26 | Removed generic status-selection guidance from the bundle. Status meanings and routing semantics belong to the Communication Protocol; concrete conditions belong to each agent's status mapping. Added single-responsibility bullet, moved from `ProtocolConstraints:Subagent` when that block was removed — it was never a protocol rule but execution discipline about scope boundaries. |
| 1.0.0 | 2026-08-05 | Initial text. Consolidates measured fragments 3 (Context Management, 42/42) and 5 (Memory via Artifacts, 41/42), plus the forty-two per-agent "Quality over Completeness" wordings reduced to one generic bullet carrying the `PARTIALLY_DONE` / `COMPLETED_NEEDS_ACTION` / `CAPABILITY_EXCEEDED` distinction. |
