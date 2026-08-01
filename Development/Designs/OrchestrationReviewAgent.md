# Orchestration Review Agent

> **Status:** Draft for review
> **Created:** 2026-08-01
> **Scope:** Design for `orchestration-review` — an infrastructure agent that periodically checks whether a run is still executing as its orchestrator's own instructions say it should, and reports what it finds as advice. Covers what it checks, where its rules come from, how it stays advisory, and how it degrades.

---

## 1. Purpose

Long runs decay. An orchestrator's context window fills, earlier decisions fall out of it, and the run starts drifting from the workflow it is supposed to be executing — advancing past a quality gate that never passed, letting `current_state` fall out of step with the Execution Log, accumulating duplicate or malformed rows in the artifact. None of this announces itself. Each individual step looks locally reasonable; only the accumulation is wrong.

`orchestration-review` is a periodic second pair of eyes. Every so often it reads the run's orchestration artifact and the workflow table the run is supposed to be following, checks one against the other, and says what looks off.

It is deliberately **not clever**. It does not evaluate whether routing decisions were wise, whether the task is going well, or whether the workflow is a good workflow. It checks stated rules against recorded facts and reports mismatches. Its value is that it is right about small things every time, not that it is occasionally insightful about large ones.

Its inputs are the orchestration artifact and two delimited regions of the deployed orchestrator file — **never logs**, and never the orchestrator's prose. It stays fully functional with no tooling installed.

## 2. Where the Rules Come From

Two obvious approaches, both wrong:

- **Write every orchestration rule into the agent's own instructions.** Creates a second statement of the same truth, maintained separately. The orchestrator changes, the checker doesn't, and it starts confidently reporting violations of rules that no longer exist — or silently passing runs that break rules it never learned. A drifting drift-detector is worse than none, because it is trusted.
- **Have the agent read the orchestrator's prose and work out which sentences are checkable invariants.** No duplication, but deciding *what to check* is a reasoning task, so the rule set varies between invocations. That is variance in the worst place: not "did I compare these numbers correctly" but "which rules exist at all." An agent doing free-form rule discovery is not the nitpicker this design wants.

The resolution is that these are not one category of rule. Sorted by what they actually are, each lands somewhere different and neither problem arises:

| Tier | What the rules really are | Source | Why that source is right |
|---|---|---|---|
| **A** | **Artifact schema conformance** — sequence arithmetic, column contracts, marker integrity, registry keys | Carried by the agent | This is a **linter**, and its rules are a format spec. A linter carrying the spec it validates is not duplication, it is the definition of a linter. |
| **B** | **Routing conformance** — did execution follow this run's declared workflow | Read at runtime from the **workflow table** | The table is structured data, not prose. Parsing it is deterministic, and it is per-run, so it could not be carried anyway. |

**Tier A's drift risk is low and self-announcing.** These rules describe the artifact *format*, which is a versioned specification that changes deliberately and rarely. When it does change, revising its validator is an obvious part of that same change — not an easily-forgotten edit in an unrelated file. This is materially different from carrying a copy of the orchestrator's evolving behaviour.

**Tier B is single-source by construction.** The workflow table is the authoritative statement of the happy path, it is machine-readable, and the agent compares log rows against it directly. Nothing is interpreted or remembered.

**The agent therefore never reads the orchestrator's prose at all.** It reads two delimited regions of the deployed orchestrator file — the workflow table and the infrastructure agent declaration (§5.2, §5.3) — plus the artifact. Both are structured; neither is interpreted.

**Exactly one behavioural rule is carried rather than read: the quality-gate exit invariant.** It is stated in the orchestrator's prose, which the agent does not read, so it is carried in the agent's own instructions instead. This is the one deliberate exception to Tier B being single-source, and it is acceptable for the same reason Tier A is: the invariant is a single architectural statement that has been stable since reviewers existed, not a piece of evolving behaviour. The two mechanical inputs it needs — which agents are reviewers, and which creator each is paired with — are still read from the workflow table at runtime, so only the *rule* is carried, never the data it applies to.

**Nothing is added to the orchestrator's system instructions for this agent's benefit.** A delimited block of checkable invariants placed in the orchestrator's prompt was considered and rejected: that prompt exists to instruct the orchestrator, it is already long, and accreting regions into it for other agents' consumption is how a system prompt becomes unmaintainable.

**None of this makes the agent clever.** Applying a fixed checklist and parsing a table are both mechanical. The intended dumbness is about refusing to second-guess routing *decisions* — which remains absolute (§6, Tier C).

## 3. Design Principles

| Principle | Description |
|---|---|
| **Format rules are carried; behaviour is read** | The artifact format spec is carried by the agent, because a linter carrying the spec it validates is what a linter *is* (§2, Tier A). Everything about how this particular run should behave is read at runtime from the workflow table, because it is per-run and could not be carried anyway. The line between them is the whole design: a stale copy of a format spec announces itself at the next spec revision, whereas a stale copy of the orchestrator's behaviour drifts silently and is applied with confidence. |
| **Never guess at what was not read** | Where an input is unavailable, the checks depending on it are skipped and their absence is reported. The agent has no fallback rulebook for routing and never assumes what a workflow probably said. Reporting less is always available; reporting wrong is not. |
| **Advisory by mechanism, not by tone** | Politeness is not a safety property. The agent is advisory because it never returns a status code that invokes routing machinery, so there is no code path by which its output becomes an instruction. Tone reinforces this; the status code enforces it. |
| **Right about small things, silent about large ones** | Mechanical comparisons it can perform correctly every time are in scope. Judgements about whether a decision was sound are out of scope entirely, not attempted at low confidence. An agent that occasionally guesses about complex routing will be wrong often enough to be ignored, taking its reliable findings down with it. |
| **Better than nothing, never worse than nothing** | Every failure mode degrades to doing less and saying so. It never blocks, never halts, never escalates, and never produces a finding it cannot substantiate from the artifact. |
| **Cheap enough to be routine** | One invocation, no artifacts written, no tooling required, findings delivered in `status_message`. If it were expensive it would be run rarely, and a drift check run rarely is a drift check that arrives after the drift. |

## 4. Declaration

```yaml
---
id: 38
version: 1.0.0
name: orchestration-review
description: Checks a run's bookkeeping and routing against its declared workflow, and reports observations
model: {model-identifier}
tools: [file_read, file_search]
recommended_tier: LOW
required_skills: []
infrastructure: review
trigger: INVOCATION_INTERVAL
trigger_param: 30
on_failure: continue
---
```

`infrastructure: review` is the class value. Unlike `checkpoint`, it is not gated by any run-start switch — a `review`-class agent fires for the whole life of every run that declares it. Deactivating it is a deployment decision: an orchestrator that should not run it does not declare it.

`file_search` is required for §5.2's discovery. No write tool of any kind: the agent produces no artifact and modifies nothing, and the absence of the capability is a stronger guarantee than an instruction not to use it.

Roughly every thirty invocations. The interval is a judgement, not a measurement: often enough that drift is caught within a bounded window, rare enough that its cost stays marginal against the run it is checking.

`on_failure: continue` follows from the agent being advisory. Halting a healthy run because an advisory check could not complete inverts the cost — the check exists to improve the run, and stopping the run to punish its absence is worse than the absence.

## 5. Inputs

### 5.1 The orchestration artifact

`Orchestration-{run_id}/Orchestration.md`, supplied read-only in `input_artifacts`.

**This is an explicit exception to a standing rule.** The orchestrator's instructions state that `Orchestration.md` is the orchestrator's alone and subagents never access it. That rule exists to stop subagents making routing decisions from orchestration state — the information asymmetry the architecture depends on. This agent's entire purpose is to check that artifact, and it makes no routing decisions at all, so the rationale does not apply to it. The exception must be stated in both places rather than left as a silent contradiction.

The agent never writes to it.

### 5.2 The workflow table

The statement of this run's happy path, and the **only** thing the agent reads outside the artifact. It does not read the orchestrator's prose, its constraints, or any other part of that prompt — just the one delimited region containing the workflow table for the workflow this run is executing.

That narrowness is deliberate. A structured table is parsed deterministically; surrounding prose would have to be interpreted, and interpretation is where an agent of this kind stops being reliable.

**It reads the orchestrator's injected copy, not a workflow source file.** The distinction does not arise in a deployed workspace: workflow definition files are a MOSAIC authoring artifact and are never deployed into a project. What reaches a running workspace is the orchestrator, with its workflows already injected. The only place a workflow table exists at runtime is inside the deployed orchestrator file, so that is what the agent reads.

This also happens to be the right target on the merits. Checking a run against the canonical definition would answer "does this run match the workflow as designed"; checking it against the injected copy answers "does this run match the workflow its orchestrator is actually executing", which is the question drift detection is asking. If a deployed orchestrator carries an outdated or hand-edited workflow, that is a fact about this run, and the agent should be reading what the orchestrator reads.

**Located by content discovery, not by configuration.** The agent reads `workflow` from the artifact's frontmatter and searches the workspace for the file containing that workflow's region marker:

```
Orchestration.md frontmatter:  workflow: quick-fix
                                    ↓
search for the literal string:  [[SECTION:Workflow:quick-fix]]
                                    ↓
             keep only matches inside  [[INJECTION:AvailableWorkflows]]
                                    ↓
                        → the deployed orchestrator file
```

The containment filter is what makes the match unambiguous. Injected workflow tables live inside the orchestrator's `[[INJECTION:AvailableWorkflows]]` region by construction, so requiring containment selects deployed orchestrators and nothing else — including in a workspace that happens to also hold MOSAIC's own authoring files, such as MOSAIC's own repository.

| Outcome | Behaviour |
|---|---|
| Exactly one match | Use it. Full checking. |
| Several matches | Two orchestrators deployed carrying the same workflow. Cannot determine which drives this run. Report that; skip Tier B. |
| No match | Report that; skip Tier B. |

In both failure rows, **Tier A still runs in full** — see §9. Losing the workflow table costs routing conformance and nothing else.

**Why discovery rather than a deployed path.** A configured path is a deployment-time fact that post-deployment reality can invalidate — files move, a second orchestrator is deployed, a project is restructured. A stale path that still resolves to *a* real file is the worst case, because the agent would then check this run against a different orchestrator's workflow and report confident nonsense. Discovery keyed on the running workflow's own id cannot make that mistake: it either finds an orchestrator carrying this run's workflow, or it finds nothing.

It is also robust to the legitimate case of a user editing their deployed orchestrator to add project context. Edits change content; they do not remove the region markers.

The search target is a long, unusual literal string, which makes this an ordinary content search rather than harness-specific path guesswork.

### 5.3 The infrastructure agent declaration region

`[[INJECTION:InfrastructureAgents]]`, in the same deployed orchestrator file located by §5.2, read in the same pass.

It supplies one thing: the names of the infrastructure agents this orchestrator may dispatch. Tier B reports agents appearing in the Execution Log that the workflow table does not name, and infrastructure agents are never in a workflow table — without this list, every `checkpoint-manager-git` row, and every one of this agent's own rows, would be reported as an anomaly. A drift detector whose most frequent finding is itself would be ignored within one run, taking its real findings with it.

If the region is absent or empty, the orchestrator has no infrastructure agents, and the exclusion list is empty. That is a valid deployment, not a failure.

**This does not cover human-dispatched agents.** `checkpoint-restore-git` is in no workflow table and in no declaration region, because it is dispatched out of band on a human's instruction. Tier B's unknown-agent check is therefore stated as an observation rather than a finding — see §6.

## 6. What It Checks

Two tiers it performs, and one it explicitly refuses.

### Tier A — Artifact consistency

Internal coherence of the artifact against the contracts the orchestrator's instructions state for it. Requires no workflow knowledge.

- `global_sequence` against the highest `Seq` in the Execution Log.
- `current_state` against the last Execution Log row — phase, stage, status, and agent must agree.
- Sequence numbers: contiguous, no duplicates, no gaps.
- `Agent` values matching the `{AgentName}#{Seq}` form, with the suffix matching the row's own `Seq`.
- All declared sections present, with their boundary markers intact.
- `Summary` values containing characters that break the table, or exceeding the stated length rule.
- Artifacts registry: duplicate keys, and `Created By` values naming an invocation with no corresponding Execution Log row.
- `Checkpoint` populated on any row while `checkpoints` is `disabled`.
- Workflow Notes: duplicated or accumulating entries.
- `current_state.error_code` populated without `last_status` being `BLOCKED`, or absent when it is.

These are the cheap, mechanical checks the agent is reliably right about. They also catch the specific failure this agent was conceived for: an orchestrator that starts writing the artifact carelessly as its context degrades — restating notes, duplicating rows, letting frontmatter drift from the log.

### Tier B — Routing conformance

Comparison of the recorded execution against the workflow table, limited to cases decidable by inspection.

- **Quality gates.** The strongest check available, and fully mechanical. The invariant itself is carried by the agent (§2) — it lives in the orchestrator's prose, which this agent does not read — but every piece of *data* it applies to is read from the workflow table at runtime:
  1. A reviewer is identified by the `-review` suffix in its name.
  2. Its paired creator is named by its **On Findings** target in the workflow table. Pairing comes from routing, not from name similarity, so it holds even where the two names differ.
  3. The invariant: the run cannot advance past the pair unless the **reviewer** returned `SUCCESS` last. A creator returning `SUCCESS` after a fix means "I applied corrections," not "the gate passed."

  So the check is: for each reviewer occurrence in the Execution Log that returned `COMPLETED_NEEDS_ACTION`, confirm the fix target ran and the reviewer ran again with `SUCCESS` before any agent downstream of the pair appears. No judgement anywhere.

  It cannot false-positive on audit agents. Assessment agents use an `-audit` suffix specifically because their findings are standalone data rather than a correction gate, so they never match the reviewer pattern and are never held to the invariant. The precision is a consequence of the naming convention rather than a special case the agent has to know about.
- **Dispatched input artifacts against the workflow table's declared inputs.** For each row, the `Inputs` column records what the invocation was actually given; the workflow table declares what that agent should receive. A declared input that is absent from the dispatch — and that already exists in the Artifacts registry, so was available to pass — is reported. This catches a failure that is otherwise entirely silent: when the omitted artifact was not load-bearing, the subagent completes successfully and nothing anywhere records that it worked with less context than intended. It requires the `Inputs` column (§10) to be detectable at all.
- **Agents appearing in the Execution Log that nothing accounts for.** Three sources are legitimate and must be subtracted before anything is reported: the workflow table (§5.2), the infrastructure agent declaration region (§5.3), and human dispatch out of band.

  The third cannot be enumerated — there is no list of what a human may legitimately dispatch, and `checkpoint-restore-git` is exactly such an agent. So an unexplained name is reported as an **observation**, in the same register as the repetition check: the agent states that a name appears which it cannot account for, and asks. It does not assert a violation. This is the honest shape of the check, because the agent genuinely cannot distinguish a routing error from a deliberate human intervention, and a recovery action taken during an incident is precisely when a false accusation is least welcome.
- Advancing on a non-`SUCCESS` status with no routing target in the table that accounts for it.
- Phase or stage transitions the workflow table does not permit.
- Repetition worth noticing — the same agent recurring many times in a short span. **Reported as an observation, never as a conclusion.** The agent states the count and asks; it does not decide whether a loop is pathological, because sometimes it is exactly what the workflow prescribes.

### Tier C — Explicitly out of scope

Not attempted, not attempted at low confidence, not hedged. Simply not done.

- Whether a routing decision was correct or wise.
- Whether the task is progressing acceptably.
- Whether the chosen workflow suits the task.
- The quality or content of any artifact a subagent produced.
- Anything requiring domain knowledge of what the run is building.

If an observation cannot be substantiated by pointing at a rule in the orchestrator's instructions and a fact in the artifact, it is not reported.

## 7. Output

`status_message` only. No artifacts.

An artifact would be written for nobody: the orchestrator is forbidden from reading subagent output artifacts, and no other consumer exists. The advice belongs where the orchestrator will actually encounter it, and `status_message` is also copied into the Execution Log, so the observation is permanently recorded without anything extra being written.

### 7.1 Status code

**Always `SUCCESS`**, whether or not anything was found. `BLOCKED` only when the agent genuinely cannot function — no access to the artifact it was given.

Every other status code invokes orchestrator routing machinery. `COMPLETED_NEEDS_ACTION` routes to a fix target; `NEEDS_CLARIFICATION` stops for input. Both convert an observation into an instruction to act, which is the exact inversion of authority this agent exists to avoid. `SUCCESS` means the orchestrator auto-advances and the observation is simply present — in its context now, and in the log permanently.

This is what makes the agent advisory as a matter of mechanism rather than manners. There is no code path by which its output becomes a command.

### 7.2 Register

Questions and observations. Never imperatives.

| Instead of | Write |
|---|---|
| "Fix `current_state`, it is wrong." | "`current_state.last_agent` is `Planner#4` but the last log row is `Research#5` — out of sync?" |
| "You skipped the review gate." | "`contracts-designer#7` advanced straight to `EXECUTION` without `contracts-review` returning SUCCESS — was that intended?" |
| "Stop looping." | "`Implementation#14` appears six times in the last ten rows — expected for this stage?" |

This is not decoration. An orchestrator receiving an imperative from a subagent is being handed something its authority hierarchy says it must treat as input rather than command, and phrasing that invites compliance makes the wrong response the easy one. Phrasing as a question makes the orchestrator's own judgement the natural next step.

### 7.3 Length

Protocol discipline: one or two sentences. The most significant finding stated in full, the remainder reduced to a count.

```json
{
  "agent_instance_id": "orchestration-review#30",
  "run_id": "20260129T090000Z-a3f9",
  "status_code": "SUCCESS",
  "status_message": "current_state.last_agent is Planner#4 but the last log row is Research#5 — out of sync? Plus 2 minor Summary formatting issues."
}
```

The orchestrator receives the full text. The Execution Log keeps the first and last fifty characters, so the leading finding survives archival. The truncation budget is a useful forcing function rather than a limitation: a nitpicker that emits a wall of text gets skimmed, and skimming loses the one finding that mattered. Leading with the most significant item is what makes the agent worth reading.

Clean runs report cleanly and briefly — *"Artifact consistent, routing matches workflow through Seq 30."* Silence is not an option, because "checked and found nothing" and "did not check" must be distinguishable in the log.

## 8. Staying Advisory

Four independent layers, in decreasing order of strength:

1. **Status code.** Always `SUCCESS`, so no routing machinery is ever invoked. Mechanical; no interpretation required by either party.
2. **Authority hierarchy.** The orchestrator's instructions already state that subagent responses are inputs to its routing decisions, not commands. This agent needs no special case — it is covered by the general rule.
3. **Declaration.** The orchestrator's infrastructure agent declaration region carries a `Description` for each agent, and this agent's reads *"Advisory — reports observations, never instructions."* The orchestrator therefore knows what kind of thing it is receiving before it receives it, rather than having to infer it from the response. The `Class` value `review` is adjacent to this but does a different job: it is a closed vocabulary term for scripts, and does not by itself tell an orchestrator how to treat a response.
4. **Register.** Questions rather than orders, so nothing in the phrasing invites reflexive compliance.

Layer 1 is the one that holds if the others fail, which is why the status code policy is not negotiable for cosmetic reasons.

## 9. Degradation

| Condition | Behaviour |
|---|---|
| Workflow table not found, or ambiguous | **Tier A still runs in full.** Report that routing could not be checked. `SUCCESS`. |
| Infrastructure declaration region absent or empty | Exclusion list is empty; this is a valid deployment. All checks run normally. `SUCCESS`. |
| Orchestrator file found but declaration region unreadable | Skip the unknown-agent check only — reporting it without the exclusion list would produce noise. Every other Tier B check is unaffected. `SUCCESS`. |
| Artifact unreadable or unparseable | `BLOCKED`, `E101`. The one case where the agent genuinely cannot function. |
| Artifact well-formed but nearly empty (early in a run) | Nothing to check yet. Say so. `SUCCESS`. |
| Findings exceed what a `status_message` holds | Report the most significant, count the rest. |

The first row is a direct consequence of §2's split. Because Tier A's rules are carried by the agent rather than read from anywhere, artifact-consistency checking never depends on discovery succeeding — and artifact consistency is precisely the failure this agent was conceived for, an orchestrator writing its blackboard carelessly as its context degrades. Only routing conformance needs the workflow table, and only routing conformance is lost when it cannot be found.

**Tier B is never checked from memory.** If the workflow table is unavailable, routing is not evaluated — the agent does not fall back on assumptions about what the workflow probably said. Per-run routing is not something there is a correct default for, and a confident report against a guessed workflow would be worse than no report.

## 10. Amendments

| Document | Amendment |
|---|---|
| Orchestrator system instructions | The constraint that `Orchestration.md` is never accessed by subagents gains a stated exception for this agent, read-only. Leaving it unstated would put the orchestrator's own constraints in silent contradiction with a deployed agent's behaviour. |
| Orchestration artifact schema §5 | The Execution Log gains an **`Inputs`** column, inserted between `Summary` and `Checkpoint`, recording the `input_artifacts` list of that dispatch. |
| Orchestrator system instructions | The orchestrator writes the `Inputs` column when appending each row. |

**This changes a fixed column contract, which is a breaking change for parsers.** The schema declares the Execution Log's columns fixed, so a consumer written against the current 8-column shape will mis-read a 9-column row. Three consequences follow, and none should be discovered later:

- It is a **major version increment** of the artifact schema, not a minor one. Consumers must be able to tell the shapes apart, and the schema version in frontmatter is how.
- **Consumers read by column name, not position.** The header row is present in every artifact; a parser that binds to it tolerates this insertion and any future one. A parser that counts columns does not, and should be corrected as part of this change rather than pinned to a version forever.

### The `Inputs` column

```markdown
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |
|-----|-------|-------|-------|--------|-----------|---------|--------|------------|
| 5 | Planner#5 | PLANNING | - | SUCCESS | ... | Created 3-stage plan | Research.md, Requirements.md | - |
| 6 | plan-review#6 | PLANNING | - | SUCCESS | ... | Plan is sound | Plan.md | - |
```

Filenames only, comma-separated, with the run-scoped folder prefix omitted — it is identical for every artifact in a run and recoverable from `run_id`. `-` when the dispatch passed none.

**It satisfies the schema's mechanical-over-authored rule exactly.** The orchestrator writes back the list it just sent; there is no judgement, no derivation, and no domain content involved. It is on the same footing as `Status` or `Agent`.

**It is the only way the forgotten-input failure becomes visible.** Without it, nothing anywhere records what a dispatch was given, so an omitted artifact leaves no trace — and when the omission is not load-bearing, the subagent returns SUCCESS and the run continues with no signal that anything was lost. This is an observed failure mode, not a hypothetical one.

Its value is not limited to this agent. It makes each row self-describing about what an invocation received, which is the first thing anyone reconstructing a run wants to know and currently the one thing the log cannot answer.

## 11. Non-Goals

- **Correcting anything.** The agent reports and stops. Fixing an inconsistent artifact is the orchestrator's business, and repairing one would mean writing to a file whose sole owner is the orchestrator.
- **Halting or escalating.** No finding, however severe, stops a run. Severity assessment is exactly the judgement this agent is designed not to attempt, and a nitpicker with a halt button will eventually halt a healthy run.
- **Reading logs.** Inputs are the artifact and two delimited regions of the deployed orchestrator file. Logs are an optional enhancement and nothing in the core runtime may depend on them.
- **Evaluating subagent work.** Whether `Research.md` is any good is a reviewer's job, and those reviewers are workflow agents with the domain context to do it.
- **Cross-run analysis.** One run, one artifact. It never looks at other runs.
- **Detecting drift in its own absence.** It reports on what the artifact records. Work performed outside the orchestration system leaves no trace it can see.

## 12. Open Items

- Whether thirty invocations is the right interval, and whether it should differ by workflow length. Currently a single default, overridable per run.
- Whether a finding repeated across consecutive reviews should be reported again identically, or noted as persisting. Repetition may be the only signal that the orchestrator ignored it; it is also noise.
- Whether the agent should verify that the workflow table it discovered matches the `workflow_version` pinned in the artifact's frontmatter, and what it should do when they differ — the orchestrator may have been redeployed mid-run.

**Resolved:** Tier B's quality-gate check is precise across all workflow shapes and needs no restriction. Reviewers are identified by the `-review` suffix, their paired creators by the On Findings routing target, and the exit invariant is stated explicitly in the orchestrator's instructions — all three mechanical, all three already written down where the agent reads them.
