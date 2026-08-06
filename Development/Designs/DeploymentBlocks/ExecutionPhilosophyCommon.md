---
id: block-execution-philosophy-common
type: specification
version: "1.0"
name: "Block — ExecutionPhilosophyCommon"
description: "The three shared posture bullets: context management, memory via artifacts, and quality over completeness. Why the last one stopped being per-agent."
author: MOSAIC
status: Draft
---

# Block — `ExecutionPhilosophyCommon:Subagent`

| | |
|---|---|
| **Block** | `ExecutionPhilosophyCommon:Subagent` |
| **Fills** | `[[DEPLOYED:ExecutionPhilosophyCommon]]` — placement in `AgentTemplateArchitecture.md` §2.5 |
| **Applies to** | `subagent` |
| **Text lives in** | `Agents/Generic/DeployedSections.md` |

The block's text is in the bundle and nowhere else. This document is its reasoning.

---

## 1. What the block does

Three bullets stating the working posture every subagent shares: how to treat its context window, where its memory lives, and what to do when it cannot finish.

## 2. Context Management

The bullet tells the agent it may spend its full context window on this task, because follow-up work is handled by spawning new instances.

It exists because the default posture is the opposite one. A model that suspects it may be asked more later rations — it reads less of a file than it should, summarises when it should quote, and stops investigating early. In a hub-and-spoke architecture that caution is pure loss: the agent is invoked once, it will never be asked a follow-up, and whatever it did not read is simply not known.

This was 42/42 and clean. It is in the bundle because it is identical, not because it was broken.

## 3. Memory via Artifacts

Input and output artifacts are the persistent memory between invocations. Anything a successor needs goes into an artifact, not into the response.

The second half is the operative part. An agent that has just finished good work naturally wants to explain it, and the `status_message` is the field in front of it — so continuation context ends up in a routing message that the next agent never sees, instead of in the artifact that it does. The bullet names the wrong destination explicitly rather than only naming the right one.

At 41/42 this was the least-drifted of the four drifting fragments, and the single divergence was a reworded restatement rather than a changed rule.

## 4. Quality over Completeness

The third bullet is the one that changed character in the migration, and it is worth being clear about what happened.

It appeared in all forty-two files in forty-two different wordings, each restating the same status-code guidance in that agent's own vocabulary. On its face that is agent-specific content — forty-two authors wrote forty-two different sentences, so the sentence must vary by agent.

It is not, and the test that settles it: **the specific version already exists somewhere else.** `ErrorHandling` carries each agent's own mapping of the six status codes to its own work, grounded in what that agent actually does. That is where "what does `PARTIALLY_DONE` mean for me?" is answered properly. The philosophy bullet was answering the same question worse, in a section whose purpose is posture rather than mechanics.

So the block carries the generic form: finishing part of the task well beats finishing all of it badly, because a successor continues what is left. Then the three-way distinction that agents get wrong most often:

| Status | When |
|---|---|
| `PARTIALLY_DONE` | A deliberate stop with **more of the same work** remaining |
| `COMPLETED_NEEDS_ACTION` | Finished work that is **a set of items for another agent** to act on |
| `CAPABILITY_EXCEEDED` | Had everything needed and **still could not do it** |

The distinction is stated here rather than left to `ErrorHandling` because it is a posture question before it is a mapping question — an agent choosing between these is deciding how to feel about stopping, and the mapping only helps once it has accepted that stopping is allowed.

## 5. What stays agent-specific

Everything after the block and the `ContextLimits` injection. "Investigation only: report observations, not assessments." "Escalate, don't fight: if the tests seem wrong, return `NEEDS_CLARIFICATION` rather than working around them." These are the section's reason to exist and they belong to the agent.

The rule from §4 generalises: a philosophy bullet earns its place by saying something about *this* agent's work. If a future measurement finds one identical across all agents, it is a candidate for this block by the same argument.

## 6. Membership

Different versions of this block on an orchestrator and a subagent break nothing. The status codes it names are the contract's, stated in the contract's own region; this block recalls three of them and describes when to reach for each. It defines nothing two parties must agree on.

---

## 7. Changelog

| Bundle version | Date | Change |
|----------------|------|--------|
| 1.0.0 | 2026-08-05 | Initial text. Consolidates measured fragments 3 (Context Management, 42/42) and 5 (Memory via Artifacts, 41/42), plus the forty-two per-agent "Quality over Completeness" wordings reduced to one generic bullet carrying the `PARTIALLY_DONE` / `COMPLETED_NEEDS_ACTION` / `CAPABILITY_EXCEEDED` distinction. |
