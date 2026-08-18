---
id: block-error-handling-common
type: specification
version: "1.1"
name: "Block — ErrorHandlingCommon"
description: "The one rule subagent error handling has in common: retry a transient error once, then escalate. Why the number is one, and why nothing else in the section is shared."
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

One bullet: retry a transient error once before escalating. That is the whole block, and everything below is why.

---

## 1. Why the number is one

A single retry, not a policy. A read that timed out, a tool that failed to answer: try again, and if it fails again, escalate.

The alternatives are worse in both directions. **Zero** turns every flaky call into a `BLOCKED` return and a full orchestrator round-trip, which is expensive and usually unnecessary. **More than one, or a backoff schedule,** turns a subagent into a retry framework — it burns context on a loop that cannot report progress, and the orchestrator, which holds the run-level view, is the party that should decide whether a persistently failing tool is worth waiting on.

Escalation is not failure. It is handing the decision to the party that can make it.

## 2. Why this rule has no other home

`CommunicationProtocol.md` §10 lists retry policy, backoff timing, and escalation thresholds as an explicit protocol non-goal: the contract supplies the error code, and what happens next is policy. But it frames that policy as the *orchestrator's* — what a runner does with `E501` versus `E101`. The subagent side, what an agent does before it returns a code at all, is left unstated.

That gap is why the rule existed as hand-copied text in the first place, and why it drifted to 35/42. It is genuinely shared, genuinely subagent-side, and genuinely absent from the contract. This block is its only canonical statement.

## 3. Why nothing else in the section is shared

The rest of `ErrorHandling` is the agent's own mapping of status codes to its own work, and it has to be — `COMPLETED_NEEDS_ACTION` is the expected outcome for a review agent and a rare one for a research agent. The schema's rule for that mapping is in `AgentTemplateArchitecture.md` §4.5.

Two things that look like candidates are not:

**The error code list.** The contract's deployed region carries the full `E101`/`E401`/`E501`/`E502`/`E503` table with names and meanings, a few inches up the same file, plus Key Rule 15 directing `BLOCKED` + code for external blockers. A recall bullet here was a third statement of the same thing. Unlike `ProtocolConstraints`, which compresses rules into imperatives an agent can hold while working, a recall of a table adds no form the agent did not already have. It was removed at bundle 1.0.0 — see §4.

**The `BLOCKED` versus `CAPABILITY_EXCEEDED` distinction.** Worth stating, and stated twice already: the contract's status table defines `BLOCKED` as an external factor, and `ExecutionPhilosophyCommon` draws the three-way distinction against `PARTIALLY_DONE` as well. A fourth statement here would be the one that drifts.

---

## 4. Changelog

| Bundle version | Date | Change |
|----------------|------|--------|
| 1.0.0 | 2026-08-05 | Initial text: retry-once, consolidating measured fragment 7 (35/42, drifting) and taking the contract-correct variant rather than the most common one. An error-code recall bullet was drafted alongside it and removed before release — the contract's deployed region already carries the full table and Key Rule 15, so it was a third statement in one file (§3). |
