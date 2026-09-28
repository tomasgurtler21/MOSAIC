---
id: block-error-handling-common
type: specification
version: "1.1"
name: "Block — ErrorHandlingCommon"
description: "The one rule subagent error handling has in common: retry a safe transient operation once, then return BLOCKED. Why the number is one, and why nothing else in the section is shared."
author: MOSAIC
status: Draft
---

# Block — `ErrorHandlingCommon:Subagent`

| | |
|---|---|
| **Block** | `ErrorHandlingCommon:Subagent` |
| **Fills** | `<ErrorHandlingCommon type="managed">` — placement in `AgentTemplateArchitecture.md` §2.5 |
| **Applies to** | `subagent` |
| **Text lives in** | `Catalog/DeployedSections.md` |

One bullet: retry a safe transient operation once; otherwise return `BLOCKED` with the applicable error code. That is the whole block, and everything below is why.

---

## 1. Why the number is one

A single retry, not a policy. Retry only when the failure may be transient and repeating the operation cannot duplicate a side effect. If retry is unsafe or fails again, return `BLOCKED` with the applicable error code.

The alternatives are worse in both directions. **Zero** turns every flaky call into a `BLOCKED` return and a full orchestrator round-trip, which is expensive and usually unnecessary. **More than one, or a backoff schedule,** turns a subagent into a retry framework — it burns context on a loop that cannot report progress, and the orchestrator, which holds the run-level view, is the party that should decide whether a persistently failing tool is worth waiting on.

Permission failures and known non-transient failures are not retried because repetition cannot repair them. A timed-out operation that may already have produced an external side effect is also not retried, because repetition may duplicate that effect.

## 2. Why this rule has no other home

`CommunicationProtocol.md` §11 lists retry policy, backoff timing, and escalation thresholds as an explicit protocol non-goal: the contract supplies the error code, and what happens next is policy. But it frames that policy as the *orchestrator's* — what a runner does with `E501` versus `E101`. The subagent side, what an agent does before it returns a code at all, is left unstated.

That gap is why the rule existed as hand-copied text in the first place, and why it drifted to 35/42. It is genuinely shared, genuinely subagent-side, and genuinely absent from the contract. This block is its only canonical statement.

## 3. Why nothing else in the section is shared

The rest of `ErrorHandling` is the agent's own mapping of status codes to its own work, and it has to be: only that mapping can define which completed outcomes require action and what incomplete but continuable work looks like for the assignment. The schema's rule for that mapping is in `AgentTemplateArchitecture.md` §4.5.

Two things that look like candidates are not:

**The error code list.** The contract's deployed region carries the full `E100`/`E101`/`E401`/`E501`/`E502`/`E503` table with names and meanings, a few inches up the same file, plus the key rule restricting `error_code` and `error_reason` to `BLOCKED` responses. A recall bullet here was a third statement of the same thing. Unlike `ProtocolConstraints`, which compressed rules into imperatives an agent could hold while working before its removal in v2.3, a recall of a table adds no form the agent did not already have. It was removed at bundle 1.0.0 — see §4.

**The `BLOCKED` versus `CAPABILITY_EXCEEDED` distinction.** Worth stating, and already owned by the contract's status definitions. Each agent's mapping then grounds those definitions in its own work. Repeating the generic distinction here would create another statement that can drift.

---

## 4. Changelog

| Bundle version | Date | Change |
|----------------|------|--------|
| 2.0.0 | 2026-09-26 | Defined safe retry behavior and replaced the ambiguous “escalate” boundary with `BLOCKED` plus the applicable error code. Rationale references corrected: protocol non-goals are §11, and `ProtocolConstraints` is described in the past tense after its v2.3 removal. |
| 1.0.0 | 2026-08-05 | Initial text: retry-once, consolidating measured fragment 7 (35/42, drifting) and taking the contract-correct variant rather than the most common one. An error-code recall bullet was drafted alongside it and removed before release — the contract's deployed region already carries the full table and the key rule confining error fields to `BLOCKED`, so it was a third statement in one file (§3). |
