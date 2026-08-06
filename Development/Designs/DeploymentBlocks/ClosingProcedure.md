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
| **Fills** | `[[DEPLOYED:ClosingProcedure]]` — placement in `AgentTemplateArchitecture.md` §2.5 |
| **Applies to** | `subagent` |
| **Text lives in** | `Agents/Generic/DeployedSections.md` |

The block's text is in the bundle and nowhere else. This document is its reasoning.

---

## 1. What the block does

Every subagent task ends the same way, whatever the work was: if the human-in-the-loop flag is set, present the finished output for review; then return the protocol response and nothing else. The block states both, positioned so they read as the continuation of the agent's own numbered Process list.

## 2. Why it sits where it does

Principle 7 of `AgentTemplateArchitecture.md`: the nearest instruction wins in practice. A model following a numbered Process list follows it to the end and then stops. An obligation stated three sections up, in a region the agent read before it knew what the task was, competes badly with the last line of the list it is currently executing.

So the closing steps have to be *at the end of the list*. What changed is that they are no longer *in* it — they are a deployed region positioned immediately after it. The agent reads a continuous sequence; the tool maintains one copy.

The ordering against `AuthorityHierarchy` is deliberate: the process and its closing steps form one continuous instruction, and inserting the four-rank ranking between them would break the sequence a model is meant to read straight through.

## 3. The defect this block exists to fix

Twenty-eight of forty-two subagents carried a Process step reading, in substance, *"if `human_in_the_loop: true`, present all output artifacts to the user for review."*

The orchestration contract requires the agent's **complete output** — orchestration artifacts *and* project files. The Process step named artifacts only.

**Why that matters far more than 28/42 suggests.** An agent that writes only source files — implementation, test authoring, refactoring — has no orchestration artifacts at all. Reading that step, such an agent can correctly conclude there is nothing to present, skip the gate entirely, and believe it complied. The instruction did not fail; it was followed exactly, and it was wrong.

This is a candidate root cause for the observed failure of subagents ignoring HITL. It is also precisely the population a `human_approved` stamp cannot detect, because those agents produce no artifact to stamp.

The block states the complete-output obligation and then states the no-artifacts case explicitly, by name, rather than leaving it as an inference from "complete". The population that got this wrong is the population that would have to draw that inference.

## 4. The three refinements the block adds

Beyond correcting the scope of "output", the block closes three gaps the old step left open. Each was a way an agent could believe it had discharged the gate without a human having reviewed anything.

**The gate re-arms.** If the user asks for changes, the agent makes them and presents again. The gate closes only when the user asks for nothing further. Without this, a single presentation discharges the obligation regardless of what the user said.

**Earlier questions do not count.** An agent that consulted the user mid-task about an approach has not run the gate. This is a review of finished output, not a conversation. The observed failure mode — agents contacting the user mid-task and then returning without a review — is exactly this substitution.

**No channel to the user is `BLOCKED`, not permission to proceed.** Error code `E503`. An agent that cannot reach the user has not been excused from the gate; it has hit an environmental block, and the orchestrator is the party that can do something about it.

## 5. Why the second step is stated at all

"Return the protocol response, and nothing else" restates a rule the contract already carries as a key rule and that `ProtocolConstraints` carries as a bullet. That is three statements of one thing, and the general rule is that a third copy is the copy that drifts.

It survives because it is not really a third copy of the rule — it is the terminator of the sequence. A closing procedure whose last numbered step is the HITL gate reads as though the task ends at the gate. Naming the return as the final step is what makes the sequence complete, and it costs one sentence.

Note also that all three statements are now single-sourced, so they cannot drift accidentally — only by someone editing one and not another. See §7.

## 6. Why this is not part of the contract's deployed region

The contract states the obligation: what the gate is, when it applies, what counts as output. This block states the *procedure* — where in the agent's flow it happens, what to do when the user asks for changes, what to do when there is no user.

Membership follows the bundle's decidable test (`DeployedSectionsBundle.md` §2): an orchestrator and a subagent carrying different versions of this block still interoperate. The messages parse, the stamps apply, routing is unaffected. The agent's review behaviour would be worse, which is a quality problem and not a wire disagreement.

## 7. Open — the duplication with the contract

The HITL obligation is now stated twice in every subagent file: once inside `[[DEPLOYED:CommunicationProtocol]]`, once here. Both are single-sourced, so accidental drift is impossible; deliberate drift, by someone editing one and not the other, is not.

Three ways to resolve it, none yet chosen:

1. **Trim the contract's subagent-side obligation** and let this block own the gate where the agent acts on it. The contract would keep the orchestrator-side statement — what the flag means, who sets it — and drop the agent-facing procedure.
2. **Trim this block to a pointer** at the contract region. Rejected on sight: it recreates the exact defect in §3, where the near instruction was the one the agent followed and the far one went unread.
3. **Accept the duplication** and treat divergence as a review matter, on the grounds that this is the one obligation where redundancy is worth paying for.

Option 2 is out. Between 1 and 3, the argument for 3 is that this gate is the one agents are observed to skip, and repetition is spent where compliance is weakest. The argument for 1 is that "one fact, one authority" is a principle the rest of the schema holds to without exception.

---

## 8. Changelog

| Bundle version | Date | Change |
|----------------|------|--------|
| 1.0.0 | 2026-08-05 | Initial text. Replaces the Process-list HITL step carried by 28/42 subagents, corrected from "output artifacts" to complete output, with the no-artifacts case, gate re-arming, the earlier-questions rule, and the `E503` case all stated explicitly. Canonical text matches none of the forty-two source files: the majority wording was the defective one. |
