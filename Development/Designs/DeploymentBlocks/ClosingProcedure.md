---
id: block-closing-procedure
type: specification
version: "1.0"
name: "Block — ClosingProcedure"
description: "The two steps that close every subagent task: the human-in-the-loop review gate and the protocol response. Why the gate left the Process list, and the defect that move fixes."
author: MOSAIC
status: Draft
---

# Block — `ClosingProcedure:Subagent`

| | |
|---|---|
| **Block** | `ClosingProcedure:Subagent` |
| **Fills** | `<ClosingProcedure type="managed">` — placement in `AgentTemplateArchitecture.md` §2.5 |
| **Applies to** | `subagent` |
| **Text lives in** | `Catalog/DeployedSections.md` |

The block's text is in the bundle and nowhere else. This document is its reasoning.

---

## 1. What the block does

Every subagent task ends the same way, whatever the work was: if the human-in-the-loop flag is set, execute the Communication Protocol's output review gate; then return the response that protocol requires. The block supplies only the nearby trigger and sequence. The protocol owns every gate mechanic, completion condition, failure outcome, and response-format rule.

## 2. Why it sits where it does

Principle 7 of `AgentTemplateArchitecture.md`: the nearest instruction wins in practice. A model following a numbered Process list follows it to the end and then stops. An obligation stated three sections up, in a region the agent read before it knew what the task was, competes badly with the last line of the list it is currently executing.

So the closing steps have to be *at the end of the list*. What changed is that they are no longer *in* it — they are a deployed region positioned immediately after it. The agent reads a continuous sequence; the tool maintains one copy.

The ordering against `AuthorityHierarchy` is deliberate: the process and its closing steps form one continuous instruction, and inserting the four-rank ranking between them would break the sequence a model is meant to read straight through.

## 3. The defect this block exists to fix

Twenty-eight of forty-two subagents carried a Process step reading, in substance, *"if `human_in_the_loop: true`, present all output artifacts to the user for review."*

The orchestration contract requires the agent's **complete output** — orchestration artifacts *and* project files. The Process step named artifacts only.

**Why that matters far more than 28/42 suggests.** An agent that writes only source files — implementation, test authoring, refactoring — has no orchestration artifacts at all. Reading that step, such an agent can correctly conclude there is nothing to present, skip the gate entirely, and believe it complied. The instruction did not fail; it was followed exactly, and it was wrong.

This is a candidate root cause for the observed failure of subagents ignoring HITL. It is also precisely the population a `human_approved` stamp cannot detect, because those agents produce no artifact to stamp.

The protocol procedure states the complete-output obligation and covers project-file-only output explicitly rather than leaving it as an inference from "complete". The closing block makes completion of that procedure a final work step. The population that got this wrong is the population that would otherwise have to draw the inference.

## 4. What the authoritative procedure guarantees

The Communication Protocol's Human-in-the-Loop procedure closes the gaps the old step left open. Each was a way an agent could believe it had discharged the gate without a human having reviewed anything. The closing block points to that procedure without restating any of these rules.

**The gate re-arms.** If the user asks for changes, the agent makes them and sends a new review request. The gate closes only when the user approves the latest request with no further changes. Without this, the first review request would discharge the obligation regardless of what the user said.

**Earlier questions do not count.** An agent that consulted the user mid-task about an approach has not run the gate. This is a review of finished output, not a conversation. The observed failure mode — agents contacting the user mid-task and then returning without a review — is exactly this substitution.

**No channel to the user is `BLOCKED`, not permission to proceed.** Error code `E503`. An agent that cannot reach the user has not been excused from the gate; it has hit an environmental block, and the orchestrator is the party that can do something about it.

**The review request uses tool calls, not response prose.** The agent's response is consumed by the orchestrator, not the user. Writing the inventory or approval question in that response has not reached the user — it has broken the JSON contract and produced a response the orchestrator cannot parse. This is the most common mechanical failure of the gate, which is why the authoritative protocol states the channel explicitly rather than relying on the nearby trigger to imply it.

**Complete means an inventory, not a reproduction.** The user needs to know every path that changed and what materially changed there; they do not need every file pasted into the interaction channel. The protocol therefore requires every created, updated, or deleted path to remain visible with its action and a brief purpose-oriented summary. Related files may share a description so the review request scales from one review artifact to a plan containing tens of artifacts without an arbitrary line or word limit. Full contents and diffs remain available when the user asks for them.

**The stamp brackets the review.** `human_approved: false` is present before the first review request and returns on every artifact content write. The metadata-only flip to `true` happens only after the user approves the exact state described by the latest request. Keeping these transitions in the authoritative procedure prevents stale approval from surviving requested changes.

## 5. Why the second step is stated at all

The second step names no envelope fields or formatting rules. It exists only as the terminator of the local sequence: a closing procedure whose last numbered step is the HITL gate reads as though the task ends at the gate. Pointing to the response the protocol requires completes the sequence without creating another statement of that response.

## 6. Why this is not part of the contract's deployed region

The contract states the obligation and the procedure: what the gate is, what the review request contains, which channel it uses, how requested changes re-arm it, what state permits return, how the stamp transitions, and what happens when no user channel exists. This block states only where in the agent's flow the procedure runs and what follows it.

Membership follows the bundle's decidable test (`DeployedSectionsBundle.md` §2): an orchestrator and a subagent carrying different versions of this block still interoperate. The messages parse, the stamps apply, routing is unaffected. The agent's review behaviour would be worse, which is a quality problem and not a wire disagreement.

## 7. One procedure, one nearby trigger

The detailed HITL procedure is stated once, inside `<CommunicationProtocol type="managed">`. Repeating its channel, inventory format, change loop, completion state, failure outcome, or stamp sequence here would create two editable statements of the same boundary behaviour.

A bare omission would fail for the opposite reason: the agent reaches the end of its Process list and sees no local instruction to run the gate. The block therefore supplies a pointer at the action boundary and sequences the protocol response after it. Everything needed to execute either operation lives in the named protocol section.

---

## 8. Changelog

| Bundle version | Date | Change |
|----------------|------|--------|
| 2.0.0 | 2026-09-26 | The detailed HITL procedure and protocol response rules are defined once in the Communication Protocol. The closing block retains only the final-action trigger and sequence, with no copied channel, completion, stamp, error-code, or response-format semantics. |
| 1.3.0 | 2026-08-27 | Agent self-identification during HITL. When multiple agents or runs present output concurrently, the user cannot tell whose work they are approving. Step 1 now requires the agent to state its `agent_instance_id` and `run_id` at the start of any HITL presentation. |
| 1.1.0 | 2026-08-25 | Explicit tool-use requirement for HITL presentation. Agents were "presenting" by writing prose in their response, which goes to the orchestrator and breaks the JSON contract. Step 1 now opens with "Use your user interaction tools to present" and a new bullet states the consequence: the response is consumed by the orchestrator, not the user. §4 extended with the failure-mode rationale. |
| 1.0.0 | 2026-08-05 | Initial text. Replaces the Process-list HITL step carried by 28/42 subagents, corrected from "output artifacts" to complete output, with the no-artifacts case, gate re-arming, the earlier-questions rule, and the `E503` case all stated explicitly. Canonical text matches none of the forty-two source files: the majority wording was the defective one. |
