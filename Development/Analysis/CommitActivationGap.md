# Commit Activation Gap

> **Status:** Analysis — findings for the agent-instruction maintainer. Both design decisions in §3 are resolved and the design amendments in §4 are applied; the instruction changes in §2 are not.
> **Created:** 2026-08-02
> **Last Updated:** 2026-08-02
> **Scope:** Records what is missing from the orchestrator's instructions for the `commit` infrastructure agent class, why each item is missing, and the two design decisions the gap exposed. States the problems and the decisions taken; does not specify the instruction text that resolves them.

---

## 1. Summary

`CommitAgent.md` and its amendments to `InfrastructureAgentConcept.md` and `OrchestrationArtifactFormat.md` are complete and internally consistent. `commit-manager-git` is deployed and correct against its own design. The orchestrator was never amended, and every responsibility `CommitAgent.md` assigns to "run start-up" is therefore unassigned in practice.

`CommitAgent.md` repeatedly names run start-up as the actor for branch creation, branch recording, and the user advisory — §4.3, §6, and §10 all do so. Run start-up in this system is the orchestrator. The design named a real actor and nothing was ever written into it. The result is not a subtle inconsistency: the activation switch the whole mode is gated on does not exist anywhere in the running system.

**The operational consequence.** Because `commits` is never set, and because nothing checks it, a deployed orchestrator declaring `commit-manager-git` dispatches that agent at every `STAGE_END` regardless. A mode designed as opt-in, with an explicit user choice and an advisory about writing into someone's permanent history, is currently on by default and silent. The agent's own preconditions are the only thing standing between that and commits landing on whatever branch the user happened to be on.

## 2. What Is Missing

Each item below exists in a design document and has no counterpart in the orchestrator's instructions.

| # | Missing from the orchestrator | Specified in | Consequence today |
|---|---|---|---|
| 1 | `commits` as a required run-start configuration input. Only Task, Workflow, and Checkpoints are collected. | `CommitAgent.md` §6 | The field is never set, so nothing downstream can gate on it. |
| 2 | `commits` and `commit_branch` in the artifact frontmatter field table. | `OrchestrationArtifactFormat.md` §4 | The schema document carries the amendment; the agent that writes the artifact does not, so the fields never appear in a real run. |
| 3 | Gating of `Class = commit` on `commits: enabled` during trigger evaluation. Only `Class = checkpoint` is gated. | `InfrastructureAgentConcept.md` §6.1 | **The most serious item.** The commit agent fires unconditionally. This is the inverse of the intended behaviour. |
| 4 | A configuration precondition requiring a declared `commit`-class agent when `commits: enabled`. | `InfrastructureAgentConcept.md` §6.1 | A run can be configured for commits against an orchestrator that cannot make them, and will not say so. |
| 5 | Validation that a `commit`-class trigger override names `STAGE_END` and nothing else. | `InfrastructureAgentConcept.md` §6.2, `CommitAgent.md` §4.2 | A configuration error the design requires to prevent the run starting is undetected. |
| 6 | Any mechanism selecting between the MOSAIC-owned and user's-own variants. | `CommitAgent.md` §4.3 | See §3.1 — this is a design gap, not only a deployment one. |
| 7 | Creation of and switching to the MOSAIC-owned branch at run start. | `CommitAgent.md` §4.3, §10 | Follows from 6. Even with the variant selected, nothing performs the operation. |
| 8 | The activation advisory: swept-in user edits, rollback cost by variant, integration options, the recorded branch name stated back. | `CommitAgent.md` §6 | The user is never told what enabling the mode costs them. §6 identifies the advisory as the only point at which a wrong branch is caught before it is in someone's history. |
| 9 | The manual-rollback advisory — commit or revert before continuing. | `CommitAgent.md` §7 | A hand rollback silently corrupts the next stage's commit. |

**Item 3 deserves separate emphasis.** Items 1, 2, and 8 make the mode unconfigurable; item 3 makes it un-disableable. Those fail in opposite directions, and the combination is the worst case: the user is not asked, is not told, and the agent runs anyway.

**Not a finding: declaration order.** `CommitAgent.md` §3 requires `checkpoint-manager-git` to precede `commit-manager-git` in the declaration region, because a `halt` agent landing after a `continue` agent produces a committed stage with no restore point at that boundary. The deployed region has the correct order. Recorded so the next reader knows it was checked rather than overlooked.

## 3. Design Decisions the Gap Exposed

Two of the missing items could not be resolved by writing instructions alone, because the design did not say enough. Both are decided below; both are amendments to `CommitAgent.md` rather than deployment fixes.

### 3.1 The variant is a run-start choice, not a deployment choice

`CommitAgent.md` §4.3 calls MOSAIC-owned and user's-own "deployment variants", and §6 describes the advisory as "a fixed string selected by the deployed variant". That framing does not survive contact with the deployment.

The deployment-time reading assumes the choice is expressed by *which agent is deployed*, which is how deployment choices work everywhere else in this system. It is not the case here: one agent, `commit-manager-git`, serves both variants. It has to, because the variants differ only in who owns the branch named by `commit_branch`, and the agent reads that field without caring how it was populated — §4.3 says as much when it notes that "everything downstream of that field is identical". There is consequently nothing for a deployment to select between, and no injection point anywhere expressing the selection: not in the orchestrator, and not in `commit-manager-git`, whose Destination section describes both variants and commits to neither.

**Decision: the user chooses the variant at run start**, in the same exchange that sets `commits`, with MOSAIC-owned as the recommended default. This costs nothing — the user is already being asked one question about this mode — and it makes the two related fields consistent: `commits` and `commit_branch` are both set once at run start from an explicit user choice, rather than one being a user answer and the other a deployment property leaking into the artifact.

It also repairs §6's advisory. The advisory's content is variant-dependent, and with the variant selected at run start it is chosen from the answer just given rather than from deployment configuration the orchestrator would otherwise have to be told about.

**Consequence for `CommitAgent.md`:** §4.3's "two deployment variants" and §6's "fixed string selected by the deployed variant" are both wrong as written and need amending to run-start selection. Nothing else in the design depends on the choice being made at deployment time — §4.6's branch check, §7's rollback costs, and §8's integration options all key off `commit_branch` and the branch's name, not off how the variant was chosen.

### 3.2 Resolved: a run-start setup dispatch of `commit-manager-git` creates the branch

Deciding the variant at run start does not say what performs `git checkout -b mosaic/run/{run_id}`. Two candidates, and the choice has consequences the design should record rather than leave to whoever writes the instructions.

**Option A — the orchestrator runs the git commands itself.** Direct, and it matches the design's language about run start-up doing this "once, at run start-up, with a clean tree". Its cost is that the orchestrator would perform a git write against the user's repository, which nothing else in its instructions does. The orchestrator's whole posture elsewhere is that it inspects nothing and touches nothing — §6 of `CommitAgent.md` is explicit that even the advisory's branch name is read from the artifact rather than from the repository, specifically so the orchestrator stays out of the workspace. Option A breaches that, for one operation.

**Option B — a special out-of-band dispatch of `commit-manager-git` at run start, to create and switch to the branch.** Keeps every git operation inside an agent that already has `terminal` and already knows this repository shape. Its cost is that it directly contradicts the agent's own design: `CommitAgent.md` §10 lists branch creation and switching as non-goals in terms that leave no room ("*This agent* never creates a branch, switches, or merges"), and the deployed agent's Constraints section states it as a NEVER. Choosing B means amending both — the agent gains a second, distinct mode of operation reached only by explicit dispatch, never by trigger, and the preconditions for that mode are not the ones in §4.6 (there is no recorded branch yet to check `HEAD` against, which is the entire point of the invocation).

**A third possibility worth naming so it is dismissed deliberately:** the user creates the branch by hand before starting. This is the smallest change and the worst outcome — it makes the recommended variant the one requiring manual setup, which will push users toward the user's-own variant that §4.3 recommends against and §7 shows to be the one where rollback residue is permanent.

**Decided: Option B**, now specified as `CommitAgent.md` §4.9.

The argument that settled it was not the one either option was originally weighed on. Option A's cost looked like a one-off breach of the orchestrator's inspects-nothing posture, affordable because it applied to the MOSAIC-owned variant alone. It does not: §4.3 records the user's-own branch "by reading `HEAD` once at run start", and reading `HEAD` is a repository inspection whichever component performs it. **There is no variant in which nothing has to touch git at run start**, so the choice was never "an agent or nothing" — it was only ever which component does it, and the agent is the one that already holds `terminal`, already reasons about this repository's state, and already owns what `commit_branch` means.

That also converts the two variants into one operation rather than two special cases: *establish the commit destination and return its name*. The orchestrator records what comes back and derives nothing, which is a stronger position than it holds today.

The objection that Option B gives a single-purpose agent a second purpose stands and is handled by making the two modes disjoint and saying so — the non-goal in §10 is narrowed rather than dropped, so a trigger-driven invocation still never creates or switches a branch. The precedent is `checkpoint-restore-git`, which is already dispatched out of band on explicit instruction and already produces log rows the workflow table does not name.

**Note on the later invocations.** Whichever option is taken, only the run-start operation is affected. Once `commit_branch` is recorded and `HEAD` is on it, the ordinary trigger-driven invocations need nothing new — the agent reads the field and checks `HEAD` against it exactly as §4.6 already specifies.

## 4. Scope of the Repair

**The generic orchestrator template is the fix site.** `Agents/Generic/Orchestrator/orchestrator.md` is the source; `.claude/agents/orchestrator.md` is a derivative of it. Repairing only the deployed file leaves the gap in the source, and the next transformation reintroduces it. The deployed file should be regenerated rather than hand-edited.

**`commit-manager-git` needs no change for items 1–5 and 8–9.** They are all orchestrator-side. It needs a change only if §3.2 resolves to Option B.

**Design documents needing amendment:**

**Design amendments — all applied.** Listed so the instruction maintainer knows what the designs now say, not as outstanding work:

| Document | § | Change |
|---|---|---|
| `CommitAgent.md` | §4.3 | Variant is a run-start choice, not a deployment property. No variant field; it is derivable from `commit_branch`. |
| `CommitAgent.md` | §4.9 (new) | The run-start setup dispatch: two dispatch modes, what setup does per variant, its own preconditions, and why its failure stops the run. |
| `CommitAgent.md` | §6 | Variant asked alongside `commits`. Explicit run-start ordering: ask → advise → dispatch setup → record. Advisory is selected by the run-start answer, not the deployed variant. |
| `CommitAgent.md` | §9, §10, §11 | Amendment table extended; branch-creation non-goal narrowed to trigger-driven invocations; both decisions recorded with their reasoning. |
| `OrchestrationArtifactFormat.md` | §4 | `commit_branch` is recorded from the setup dispatch's return, never derived by the orchestrator. Variant derivability noted. |
| `InfrastructureAgentConcept.md` | §6.2 | Class trigger restrictions govern trigger evaluation only, not out-of-band dispatch. Without this, §4.9 reads as violating §4.2. |
| `InfrastructureAgentConcept.md` | §12 | New open item: the declaration region describes only trigger-driven behaviour, and `commit` is the first agent with a second mode. |

**What the instruction maintainer still has to do** is everything in §2 — all nine items are orchestrator instruction changes, plus one commit-agent change that §3.2 has now created:

- Items 1–5 and 8–9: orchestrator only.
- Items 6–7: orchestrator gains the variant question and the setup dispatch at run start; `commit-manager-git` gains its setup mode per §4.9, including narrowing its Constraints section, which currently states branch creation as an unqualified NEVER.

## 5. Interim Position for a Run Started Before the Repair

Starting a run against the current deployment with `commit-manager-git` declared means the agent fires at every stage boundary with no `commit_branch` recorded. Its branch-mismatch precondition has nothing to compare against, so the outcome is either a `BLOCKED`/`E502` per stage or — worse, depending on how the absent field is read — commits landing on whatever branch the working tree is on. `on_failure: continue` means neither outcome stops the run or announces itself beyond a log row.

Two ways through, both temporary:

- **Remove the `commit-manager-git` section** from the deployed orchestrator's `[[INJECTION:InfrastructureAgents]]` region. The region is authoritative at runtime, an absent agent is simply never dispatched, and an orchestrator declaring no agent of a class is a valid state. Cleanest if commits are not needed for this run.
- **Pre-create and check out `mosaic/run/{run_id}` by hand**, and add `commits: enabled` and `commit_branch` to the artifact frontmatter at run start. This reproduces by hand what §3.1 and §3.2 will automate, and the agent then behaves exactly as designed.
