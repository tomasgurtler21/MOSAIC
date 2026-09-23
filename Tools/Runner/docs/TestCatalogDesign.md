# MosaicTest Catalogue — Design

> **Status:** Draft
> **Created:** 2026-08-17
> **Scope:** How the `mosaic-run` end-to-end test suite works: what it tests, what stub agents it uses, how their behaviour is fixed by fixture files, which test workflows exist, and how a run is checked. Assumes the production defects in `Requirements.md` (RUN-1 … RUN-8, DEP-1) are fixed.

---

## 0. Words Used In This Document

Plain meanings for the few terms that keep coming up.

| Term | What it means |
|------|--------------|
| **Consultation** | The Runner asking the orchestrator "what should I run next?" It runs the orchestrator through the harness CLI, just like any agent, but the message sent and the answer expected are a different shape from a normal agent's. In Mode 1 this happens before every step; in Modes 2 and 3 only when the Runner cannot work out the next step itself. |
| **Stub** | A fake agent that does no real work. It reads a fixture file and returns exactly what that file says. Its purpose is to make a run's outcome fixed in advance. |
| **Fixture** | A file that tells a stub what to do. Not code, not read by the Runner — read by the stub. |
| **Expected run** | A list, written before running anything, of the steps a test run should produce: who is invoked, in what order, and what each returns. Possible because every stub is driven by a fixture. |
| **Deviation** | Anything the Runner cannot route on its own: a result other than success, a routing hint it cannot resolve, or a harness failure. A deviation is what triggers a consultation in Modes 2 and 3. |
| **Seed folder** | A folder of fixture files copied into a run's folder before the run starts. |

---

## 1. What This Suite Tests

The Runner has three kinds of tests. This suite is the third kind, and it should only test things the first two cannot.

| Kind | What runs | What it proves |
|------|-----------|----------------|
| **Unit tests** | One Go package, everything else faked | That package's logic |
| **Full-stack tests** | The whole Runner, real artifact files, but a fake harness and a fake orchestrator | Routing, all three modes, deviations, approval checks |
| **This suite** | A real `mosaic-run` process, a real harness CLI, real deployed agent files, real models | Everything the fakes replace |

The three execution modes are **already tested** by the full-stack tests. This suite must not repeat that work. What only this suite can reach:

- **Talking to a real harness CLI** — how it is launched, how the prompt gets in, how the reply comes out, timeouts, exit codes.
- **Asking the orchestrator what to run next** — the full-stack tests replace the orchestrator with a fake, so the code that really talks to it is never used there.
- **Reading a real model's reply** — whether the Runner can pull a routing instruction out of text a real model wrote, not text a test author wrote.
- **Deployment** — whether a deployed orchestrator file is actually readable by the Runner and whether all agents resolve.
- **Data surviving the round trip** — identifiers and awkward text coming back unmangled.

**This suite does not test whether the real orchestrator makes good decisions.** That is a separate question for a separate tool. Here we use a *stub* orchestrator so that no model judgement can affect the result.

---

## 2. One Suite, Every Harness

The test catalogue is harness-agnostic, exactly like the normal catalogue. It contains stub agents, test workflows and fixture files — nothing harness-specific.

So the plan is:

1. Deploy the test catalogue to a workspace, once per harness, using the normal deploy tool.
2. Run the same suite against each one.
3. Expect the same results everywhere. A difference between harnesses **is** a finding — that is largely why the suite exists.

There are no per-harness workflows or fixtures. The only per-harness work is in the Runner's own code: each harness adapter needs the code for asking the orchestrator what to run next (RUN-4 in `Requirements.md`). None of them has it today.

---

## 3. The Rules Every Stub Follows

Four rules already apply to the existing stubs and still hold. Two are new, needed because we are adding an orchestrator.

**Existing rules:**

1. **A run must be predictable before it starts.** Reading the fixture files tells you exactly what the run will do. A stub that makes its own decisions ruins this, and ruins it silently — the run still looks fine.
2. **Failing loudly beats passing quietly.** If a fixture does not say what to do, the stub reports an error and says what is missing. A stub that guesses produces a green run that tested nothing.
3. **Behaviour goes in fixture files, not in agent files.** One stub agent serves many workflow rows. Adding a test case means adding a fixture, not adding an agent.
4. **`status_message` is what the tester reads.** A person reads the run output, so every message says which row, phase, stage and status it belongs to.

**New rules:**

5. **Fixtures are matched by run state, not by counting invocations.** Explained in §5.3 — this is the most important design choice here.
6. **One workflow per mechanism.** A real harness run is slow and costs money, so several checks about one mechanism share a workflow. Two different mechanisms do not, because when it breaks you need to know which one broke.

---

## 4. The Stub Agents

| Stub | What it is | State |
|------|-----------|-------|
| `mosaictest-scripted` | A normal subagent. Returns whatever protocol response its fixture tells it to. | Exists. E1, E2, E3 all done (§4.1) |
| `orchestrator-script` | A stub orchestrator. Returns whatever routing instruction its fixture tells it to. | Written (§5) |
| `orchestrator` | A placeholder, never invoked (§4.2). | Written |
| `mosaictest-checkpoint` | Checkpoint infrastructure stub. Returns success with a fake checkpoint marker, touches no git. | Implemented. Used by `infra-checkpoint-commit` |
| `mosaictest-commit` | Commit infrastructure stub. Returns success with a fake `[branch:mosaictest-run]` marker, touches no git. Used for both the run-start setup dispatch and subsequent `STAGE_END` trigger dispatches. | Implemented. Used by `infra-checkpoint-commit` |
| `mosaictest-review` | Review infrastructure stub. Returns success with a canned observation message, inspects nothing. | Implemented. Used by `infra-review-consult` |

### 4.1 Three Additions Needed to `mosaictest-scripted`

Some planned workflows cannot be built until these exist. All three are small — each is a single new field in the fixture format.

**E1 — Let the fixture set the approval flag.**
When the stub writes an artifact it stamps provenance frontmatter, but it cannot control the `human_approved` field inside it. To test the approval check we need the stub to write an artifact that is deliberately *not* approved, then one that *is*. So the fixture's write section must specify the approval value.

**State: Implemented.** The stub reads `### HitlApproval` (`true`|`false`) from the outcome block. When present, stamps `human_approved: <value>` in output artifacts after provenance. When absent, no `human_approved` line (current behaviour preserved).

**E2 — Let the fixture make the stub echo back the task description it received.**
Right now the stub only repeats text from its own fixture. It never reports what the Runner actually sent it. That means nothing in the run shows whether the orchestrator's task description arrived.

This matters more than it sounds: **echoing is the only way this suite can see what was sent in a dispatch.** Without it, Mode 1's whole point — that the orchestrator writes a useful task description and the subagent receives it — is assumed, never checked.

**State: Implemented.** The stub reads `echo: task_description` from the fixture and includes the received task_description in its status_message.

**E3 — Make the human-review refusal a fixture choice instead of automatic.**
Today, if the stub is dispatched with human review turned on, it immediately returns `BLOCKED` / `E503` and does nothing else. That is intentional — it has no tool for talking to a user. But it also means **every human-review row is impossible to test**, so the approval check can never run.

Fix: the fixture decides. One fixture keeps the old refusal behaviour (that check is still worth having). Another tells the stub to write its artifacts and report success, so the approval path can be tested.

**State: Implemented.** The stub reads `### HitlBehaviour` (`proceed`|`refuse`) from the outcome block. When `proceed`, skips the `human_in_the_loop` E503 check and processes the fixture normally. When `refuse` or absent, current behaviour (immediate `BLOCKED`/`E503`).

---

### 4.2 Why the Stub Orchestrator Is Named `orchestrator-script`

It cannot be called `mosaictest-orchestrator`, and the reason is a hard constraint rather than a preference.

The deployment tool loads a catalogue's orchestrators from **two fixed filenames** inside the catalogue's `Orchestrator/` folder: `orchestrator.md` and `orchestrator-script.md`. It does not scan for them and it does not read a name from frontmatter. A stub under any other filename is simply not loaded as an orchestrator, gets no workflows injected into it, and is therefore useless to the Runner — which reads the run's workflow definition out of the orchestrator file it is given.

Two consequences:

- The stub orchestrator is `TestCatalog/Orchestrator/orchestrator-script.md`. Its frontmatter carries no `id`, because the schema gives orchestrators none.
- A **placeholder `orchestrator.md`** sits beside it. The deployment tool includes a catalogue's conversational orchestrator unconditionally, and a catalogue lacking one deploys an empty unnamed agent artifact while still reporting success. The placeholder removes that failure mode. Nothing invokes it; if it ever runs, it says so and stops.

### 4.3 `mosaictest-commit` — Commit Infrastructure Stub

Structurally identical to `mosaictest-checkpoint`: zero tools, hardcoded behaviour, one predetermined response shape. Different class, different marker.

**Class:** `commit` (frontmatter `infrastructure: commit`).
**Trigger:** `STAGE_END`.
**Marker:** `[branch:mosaictest-run]` — a fixed fake branch name. Unlike the checkpoint stub's per-invocation sha, the branch name is constant because the real commit agent establishes a branch once and reuses it.

**Two invocation contexts, same response:**

1. **Run-start setup dispatch** (Design.md §4.2): The Runner dispatches the commit-class agent once before the dispatch loop. The stub returns SUCCESS with `[branch:mosaictest-run]` at the end of `status_message`. The Runner extracts `commit_branch` from the marker and records it in the artifact frontmatter. If the marker is missing, the run refuses to start.

2. **Trigger dispatches** (during the run): Fired by `STAGE_END`. Same SUCCESS, same marker. The Runner records the commit row as an infrastructure-flagged execution log entry.

**Message template** (for `mosaictest-commit#3`):

```
MosaicTest infrastructure stub / class=commit / declared trigger=STAGE_END / instance=mosaictest-commit#3 / no git performed / returning SUCCESS [branch:mosaictest-run]
```

**Error handling:** Same as `mosaictest-checkpoint` — `BLOCKED`/`E503` for `human_in_the_loop: true`, SUCCESS for everything else. `on_failure: halt` (matching the real commit agent: a commit failure that passes silently would lose work).

---

## 5. The Stub Orchestrator

### 5.1 Why the Existing Stub Cannot Do This Job

A consultation is not a normal subagent exchange:

- It has a different request (artifact path, context, the last agent's full message) and a different reply (a dispatch instruction or a stop instruction).
- A normal dispatch carries artifact paths. `mosaictest-scripted` finds its fixture *through* those paths. A consultation carries no artifact paths at all.

So neither the message format nor the fixture-finding trick carries over. It needs its own stub.

### 5.2 How It Finds Its Fixture

The consultation request gives the stub the path to the run's `Orchestration.md`. That is the only useful thing that varies.

So the stub takes the folder that file is in, and reads its fixture from a **fixed filename** there: `MosaicTestRouting.md`.

One run has one routing fixture. Each workflow's seed folder carries its own copy, so the filename can stay fixed while every workflow gets different behaviour.

### 5.3 How It Knows What to Answer

The stub remembers nothing — every consultation is a brand new session. So it has to work out what to answer by looking at the run's state, which it reads from the execution log in `Orchestration.md`.

This works because the Runner is required to write the last agent's result into the artifact *before* asking the orchestrator anything. So the last workflow row in the log is always the step that triggered this consultation.

The fixture is a list of rules. Each rule says "when the run is in this state, answer this":

| Rule matches on | Meaning |
|-----------------|---------|
| `run-start` | Nothing has run yet |
| `after {agent} {STATUS}` | The last step was that agent returning that status |
| `after {agent} {STATUS} #{n}` | Same, but only the *n*th time it happens |

The `#{n}` form is only for the rare case where the same agent returns the same status twice and must be routed differently each time. The stub gets the count by counting matching rows in the log, so even that is read from the artifact rather than remembered.

**If no rule matches, the stub stops the run and says so** — naming the state it saw and listing the rules it has.

#### Why matching on state instead of just counting invocations

The obvious design is a simple list: "1st time asked → do this, 2nd time → do that." It is easier to write and it is the wrong choice here.

Suppose the Runner has a bug and consults one extra time, or one time fewer. With a numbered list, every remaining answer shifts by one. The stub keeps giving valid-looking answers to the wrong questions. The run finishes green and tested nothing — which rule 2 calls the worst possible outcome.

With state matching, that same bug produces a state no rule covers, and the stub stops and reports it. **The bug becomes visible instead of being absorbed.**

A useful side effect: the stub never has to recognise its own past invocations, so it does not care how consultation rows are labelled in the log. That removes a dependency on RUN-6.

### 5.4 What the Fixture Contains

For each rule, either:

- **dispatch** — which agent to run next, the task description to send, and optionally replacements for input artifacts, output artifacts, constraints, or the human-review flag. Leaving those out means "use the workflow table's own values", which is itself the thing being tested.
- **stop** — the reason.

Plus, separately, the **pre-consultation** answer: the advice strings to return when the Runner asks at run start, or an explicit "none".

Text is carried in tilde-fenced blocks, matching the existing fixture format, so fixtures can contain backticks without escaping.

### 5.5 Rules the Stub Orchestrator Must Never Break

- Never invoke a subagent itself. It answers; the Runner dispatches.
- Never write the execution log, current state, artifact registry or frontmatter.
- Never modify project files.
- Never invent an answer the fixture does not give. Missing, silent or broken fixture means stop loudly.

---

## 6. Fixture Files and Seeding

The existing arrangement stays: **one seed folder per workflow, seeded as a whole folder.** This is required because seeding copies paths relative to the folder you name, and the `MosaicTestScript/` prefix only survives if you seed the folder that contains it.

Each seed folder holds:

| File | Why |
|------|-----|
| `Requirements.md` | Seeding requires exactly one requirements-like file. Nothing reads it. |
| `Plan.md` | A ready-made stage table, for workflows where no row produces one. |
| `MosaicTestRouting.md` | The routing fixture, for workflows run in a mode that consults the orchestrator. |
| `MosaicTestScript/{behaviour}.md` | One file per subagent behaviour the workflow needs. |

Behaviour files are named after what they do, not after a row number, so one file can serve the same behaviour in several workflows.

---

## 6a. Workflow Frontmatter Fields

Every workflow `.md` file under `Workflows/MosaicTest/` carries two machine-readable YAML frontmatter fields alongside the existing `version`, `name`, `id`, `referenced_agents`, and `artifacts` fields:

```yaml
modes:
  - auto          # one or more of: auto, auto-review, orchestrated
smoke_set:
  - auto          # subset of modes; omit the field entirely when not in the Smoke Set
```

**`modes`** (required): The execution modes this workflow supports. A workflow with multiple modes is tested once per mode by the automation tool. Valid values are the three Runner modes: `auto`, `auto-review`, `orchestrated`.

**`smoke_set`** (optional): Which of this workflow's modes belong to the Smoke Set (§10). Each entry must also appear in `modes`. When the field is absent or the list is empty, the workflow has no Smoke Set membership. The four Smoke Set entries are: `smoke-single/auto`, `orchestrated-linear/orchestrated`, `findings-loop/auto`, `findings-loop/auto-review`.

**`pre_consult`** (optional, boolean): Whether the pre-consultation path is exercised for this workflow's test runs. Valid values: `true`, `false`. When the field is absent the default is `true` (pre-consultation enabled). Set to `false` only for workflows where the pre-consultation step is intentionally skipped.

**`infrastructure_agents`** (optional, list of strings): The infrastructure agent keys this workflow requires. Each entry must match an agent key in the test catalog's `Subagents/MosaicTest/` directory (e.g., `mosaictest-review`). When absent, the field defaults to an empty list — the test runner emits `--infrastructure=` (empty) so no infrastructure agents are active for that run. Only workflows that test infrastructure features declare this field. All other workflows omit it entirely.

**`checkpoints`** (optional, string): Whether checkpoint support should be enabled for this workflow's test runs. Valid values: `enabled`, `disabled`. When absent, defaults to `disabled`. Set to `enabled` only for workflows that require a checkpoint-class infrastructure agent (i.e., that declare a checkpoint agent in `infrastructure_agents`).

**`commits`** (optional, string): Whether commit-class infrastructure dispatch should be enabled for this workflow's test runs. Valid values: `enabled`, `disabled`. When absent, defaults to `disabled`. Set to `enabled` only for workflows that require a commit-class infrastructure agent (i.e., that declare a commit agent in `infrastructure_agents`).

**Authoring rule:** When you add a new workflow, decide which modes it should run under and add the `modes` field. If any (workflow, mode) pair should be part of the Smoke Set, add `smoke_set` listing those modes. If the workflow tests infrastructure agents, add `infrastructure_agents` (and `checkpoints`/`commits` as needed). Update `RunningTests.md`'s workflow table to match.

---

## 7. The Test Workflows

Three original workflows test the harness connection itself and are the first thing to run against a new or changed harness: `smoke-single`, `payload-stress`, `staged-preplaced-plan`.

The remaining workflows test Runner modes, routing mechanisms, and edge cases. The table below shows every workflow — implemented and planned.

| Workflow | Mode | What it tests | State |
|----------|------|--------------|-------|
| `smoke-single` | Auto, Auto-review | Harness works at all; single invocation, envelope parse, identifier echo | **Implemented** |
| `payload-stress` | Auto, Auto-review, Orchestrated | Fenced blocks, JSON in messages, Unicode survive the round trip | **Implemented** |
| `staged-preplaced-plan` | Auto, Auto-review, Orchestrated | Staged execution with a pre-placed Plan.md | **Implemented** |
| `orchestrated-linear` | Orchestrated | Orchestrator is asked before every step including the first; the stop instruction works; the task description it writes actually reaches the subagent | **Implemented** |
| `orchestrated-backjump` | Orchestrated | Instruction overrides for artifacts, constraints and human review on successive dispatches of the same row | **Implemented** |
| `findings-loop` | Auto **and** Auto-review | The one difference between these two modes (§7.1) | **Implemented** |
| `deviation-blocked` | Auto | A `BLOCKED` result becomes a deviation, the orchestrator is asked, and the run carries on | **Implemented** |
| `deviation-ambiguous` | Auto-review | A routing hint the Runner cannot resolve becomes a deviation | **Implemented** |
| `deviation-stop` | Auto | The orchestrator ends the run, and the artifact can still be resumed afterwards | **Implemented** |
| `hitl-glob-staged` | Orchestrated | HITL approval check with `Stage-*` glob output artifacts; glob expansion resolves to concrete Stage-N paths before approval is read (§7.3) | **Implemented** |
| `infra-checkpoint-commit` | Auto | Checkpoint trigger (`INVOCATION_INTERVAL`) and commit trigger (`STAGE_END`) both fire; checkpoint marker picked up; commit `[branch:{name}]` marker extracted; no cascading between infrastructure dispatches (§7.6) | **Implemented** |
| `infra-review-consult` | Auto | Review trigger (`INVOCATION_INTERVAL(3)`) fires; review agent returns observations; Runner follows up with an orchestrator routing consultation passing the review's `status_message` as `last_status_message` (§7.7) | **Implemented** |
| `preconsult-advice` | Auto | Pre-consultation advice reaches auto-routed dispatches, and does **not** reach orchestrator-written ones | **Implemented** |
| `staged-multigroup` | Auto | Multi-group staged execution (TDD approach: Test group → Implementation group); exercises `EXECUTION.Test.[StageNumber]` and `EXECUTION.Implementation.[StageNumber]` phase parsing through a real harness (§7.4) | **Implemented** |
| `hitl-escalate` | Auto | The approval check uses up its re-dispatch and escalates to a deviation. Uses stub enhancements E1 + E3 (§7.8) | **Implemented** |
| `deviation-chain` | Auto | Two consecutive deviations requiring two orchestrator consultations before the run completes; exercises the single-decision chain through a real harness (§7.5) | **Implemented** |

### 7.1 `findings-loop` — One Workflow, Run Under Two Modes

Auto and Auto-review differ in exactly one way. When a reviewer reports findings:

- **Auto** asks the orchestrator what to do.
- **Auto-review** sends it straight back to the workflow table's findings target without asking, and adds the reviewer's output file to that agent's inputs.

The clearest way to prove this is **the same workflow and the same fixtures, run twice — once per mode — producing two different, separately documented runs.** If we used two different workflows instead, we would only be proving that two different definitions behave differently, not that the *mode* is what changed it.

In Auto-review, the routing fixture's findings rule simply never fires. That the run finishes without ever firing it is the proof.

### 7.2 `preconsult-advice` Needs Both Kinds of Dispatch

Pre-consultation advice is added only to dispatches the Runner builds itself, never to ones the orchestrator writes. To show the "never" half, the run needs both kinds — so this workflow also causes one deviation. The echoing stub (E2) then shows the advice present on the auto-built dispatch and absent on the orchestrator-written one.

### 7.3 `hitl-glob-staged` — Glob Expansion in HITL Approval Checks

Added during implementation to cover a real bug: the HITL approval check was reading from the literal `Stage-*/HITLGlobStage.md` path instead of expanding the glob. This workflow exercises the `expandStageGlobs` path with `Stage-*` output artifacts and orchestrator-controlled `hitl_override`.

This replaces the originally planned `hitl-approve` workflow. The original design assumed E1 and E3 stub enhancements would be available; `hitl-glob-staged` works with the existing stub by using the `Status != SUCCESS` shortcut in `DecideHITLCompliance`.

**Coverage note:** The approval-reading-from-artifacts path (where an agent returns `SUCCESS` with HITL=true and the Runner reads `human_approved` from the artifact) is exercised by `hitl-escalate` (§7.8), which uses E1 and E3.

### 7.4 `staged-multigroup` — Multi-Group Staged Execution

This workflow exercises the full group-ordering path through a real harness: `EXECUTION.Test.[StageNumber]` rows dispatched first, then `EXECUTION.Implementation.[StageNumber]` rows, across at least two stages.

**Why this matters:** The engine's group-ordering logic is covered by unit tests, but the phase-column notation (`EXECUTION.Test.[StageNumber]`) passes through the harness CLI and model response parsing. A historical crash occurred when the engine tried to resolve a glob/wildcard dispatch after the design phase finished — exactly the kind of bug that only manifests through a real harness. A multi-group staged workflow is the most complex routing path the engine handles, and proving it works end-to-end through every harness is essential for a confident compatibility declaration.

**Shape:** Two execution groups (Test, Implementation), two stages, with the TDD approach. At least 8 EXECUTION dispatches total (2 groups × 2 rows × 2 stages, or similar). Uses the `mosaictest-scripted` stub with appropriate per-group fixtures.

### 7.5 `deviation-chain` — Consecutive Deviations

This workflow exercises the single-decision principle (Design.md §2.6) through a real harness: the first agent returns `BLOCKED`, the orchestrator re-dispatches a different agent, that agent also returns `BLOCKED`, the orchestrator is consulted again and re-dispatches the original agent (which now succeeds).

**Why this matters:** `deviation-blocked` tests a single deviation → single consultation → resolution. But the single-decision principle means complex deviations produce a chain of consultations. Each consultation is a fresh session through the harness adapter; consecutive consultations test parsing fidelity under back-to-back orchestrator calls. The routing fixture needs at least three rules to drive this scenario.

### 7.6 `infra-checkpoint-commit` — Checkpoint and Commit Triggers

A staged Auto-mode workflow (at least 2 stages) with both checkpoint-class and commit-class infrastructure agents declared. The workflow frontmatter declares `infrastructure_agents: [mosaictest-checkpoint, mosaictest-commit]`, `checkpoints: enabled`, and `commits: enabled`. The test runner reads these fields and passes `--infrastructure=mosaictest-checkpoint,mosaictest-commit --checkpoints enabled --commits enabled` to the `mosaic-run run` subprocess, so the agents are active and the session's startup checks pass.

**Stubs used:**

| Stub | Class | Trigger | What it proves |
|------|-------|---------|----------------|
| `mosaictest-checkpoint` | checkpoint | `STAGE_END` (from agent frontmatter) | Checkpoint trigger fires at stage boundary; fake `[checkpoint:f00d{NNNN}]` marker extracted and recorded |
| `mosaictest-commit` | commit | `STAGE_END` (from agent frontmatter) | Commit setup dispatch at run start extracts `[branch:mosaictest-run]`; subsequent `STAGE_END` triggers fire and record commit rows |

**What this proves:**
- Commit setup dispatch runs at run start and extracts the branch marker into artifact frontmatter
- `STAGE_END` fires exactly at stage boundaries for both checkpoint and commit agents
- Trigger evaluation fires after workflow steps (not after infrastructure steps — no cascading)
- Checkpoint marker is recorded in the execution log
- Infrastructure steps are recorded as infrastructure-flagged rows (don't update `current_state`)

**Shape:** 2 stages × 1 row per stage = 2 workflow steps. After stage 1 completes, `STAGE_END` fires for both checkpoint and commit. After stage 2 completes (end of run), `STAGE_END` fires again for both. Plus the commit setup dispatch at run start. Minimal workflow that exercises both classes.

**Run configuration:** `--checkpoints enabled --commits enabled --commit-branch mosaic-owned`. The commit setup dispatch is the first thing that fires (before any workflow step), which is itself a key assertion — if the setup fails or the branch marker is missing, the run refuses to start.

### 7.7 `infra-review-consult` — Review Trigger and Orchestrator Follow-Up

An Auto-mode workflow with a review-class infrastructure agent (`mosaictest-review`) declared. The workflow frontmatter declares `infrastructure_agents: [mosaictest-review]`. The test runner reads this field and passes `--infrastructure=mosaictest-review` to the `mosaic-run run` subprocess, so only the review agent is active. The agent's default trigger is `INVOCATION_INTERVAL(3)` (from its frontmatter), so the workflow must have at least 3 workflow steps to fire it.

**What this proves:** The unique thing about the review class — after the review agent fires, the Runner does a **follow-up routing consultation** with the script-mode orchestrator, passing the review's `status_message` as `last_status_message` (ScriptOrchestratorContract.md §2.1). This is the only infrastructure class that triggers an additional orchestrator invocation.

**Stubs used:**

| Stub | Class | Trigger | Role |
|------|-------|---------|------|
| `mosaictest-scripted` | (workflow) | — | 3+ workflow rows to trigger the review agent |
| `mosaictest-review` | review | `INVOCATION_INTERVAL(3)` | Fires after 3rd workflow step; returns canned observation |
| `orchestrator-script` | (routing) | — | Receives the review's `status_message` in a follow-up consultation |

**Shape:** 3 workflow rows (e.g. RESEARCH → PLANNING → DESIGN, one `mosaictest-scripted` row each). After the 3rd row completes, `INVOCATION_INTERVAL(3)` fires `mosaictest-review`. The review returns its canned message. The Runner then does a routing consultation with `orchestrator-script`, passing the review's `status_message` as `last_status_message`. The orchestrator fixture needs a rule matching the post-review state — it can simply dispatch the next agent or stop.

**Expected log:** 3 workflow steps → review infra step → consultation step → (continue or stop). The consultation step proves the review-to-orchestrator chain works end to end. The `last_status_message` in the consultation request contains the review's canned observation text.

### 7.8 `hitl-escalate` — HITL Approval Failure and Escalation

An Auto-mode workflow where the stub returns SUCCESS with HITL=true, but the artifact has `human_approved: false`. The approval check fails, the Runner re-dispatches (one retry), the stub returns SUCCESS again with `human_approved: false` again, and the Runner escalates to a deviation. The orchestrator then stops the run.

**Uses stub enhancements E1 + E3** (§4.1):
- E3: `HitlBehaviour: proceed` — stub processes the fixture normally instead of auto-refusing on HITL=true
- E1: `HitlApproval: false` — stub writes `human_approved: false` in the output artifact

**What this proves:**
- HITL approval check reads `human_approved` from the artifact (not just the `Status != SUCCESS` shortcut)
- Failed approval triggers a re-dispatch (not an immediate escalation)
- Second failure escalates to a deviation
- The deviation reaches the orchestrator

**Shape:** One row, HITL=true. Fixture writes artifact with `human_approved: false` on both invocations. Expected log: dispatch → SUCCESS (approval fails) → redispatch → SUCCESS (approval fails again) → deviation → consultation → stop.

---

## 8. Automated Test Execution

### 8.1 The `test` Subcommand

The Runner includes a `test` subcommand, gated behind the `--dev` flag, that automates the deploy → seed → run → check cycle for every (workflow, mode, harness) combination in the test catalog:

```
mosaic-run --dev test --catalog <path-to-MOSAIC-repo> --suite smoke --harness claude-code
```

**Key flags:**

| Flag | Required | Description |
|------|----------|-------------|
| `--catalog` | Yes | Path to the MOSAIC repo root (where `Tools/Runner/TestCatalog/` lives) |
| `--suite` | One of suite/workflow | `smoke` (the 4-run smoke set) or `full` (all implemented workflows) |
| `--workflow` | One of suite/workflow | Specific workflow ID (repeatable); mutually exclusive with `--suite` |
| `--mode` | No | Execution mode filter; only valid with `--workflow` |
| `--harness` | No | Harness ID filter (repeatable); defaults to all CLI harnesses |
| `--ghcp-permission-mode` | When ghcp-cli | `blanket` or `allowlist` |

**What it does per test case:**

1. Loads the test catalog from `TestCatalog/Workflows/MosaicTest/`
2. Reads each workflow's frontmatter (`modes`, `smoke_set`, `pre_consult`) to enumerate (workflow, mode) pairs
3. For each pair × each harness: deploys the test catalog, seeds fixtures, runs `mosaic-run run` with the correct flags, and checks the result
4. Prints a structured pass/fail summary and exits 0 (all pass) or 1 (any failure)

**What it checks:** The current checker validates run outcome (completed, stopped, refused) and execution log structure.

### 8.2 What the Automated Checker Cannot Do

The `test` subcommand cannot:

- **Kill and resume a run** — resume testing requires process lifecycle control. See §13.2.
- **Simulate harness errors** — timeout/crash injection requires adapter-level hooks. See §13.3.
- **Evaluate subjective quality** — whether a task description is "good enough" is a human judgement.

These gaps are covered by manual procedures documented in `RunningTests.md`. Tooling gaps that prevent specific test workflows from running are tracked separately in `ToolingGaps.md`.

### 8.3 Relationship to Workflow Frontmatter

The `test` subcommand reads the `modes`, `smoke_set`, and `pre_consult` frontmatter fields (§6a) from each workflow file. These fields are the contract between the workflow author and the automation tool:

- `modes` → which (workflow, mode) pairs to enumerate
- `smoke_set` → which pairs belong to the `--suite smoke` set
- `pre_consult` → whether `--pre-consult=false` should be passed to the run

When adding a new workflow, setting these fields correctly is what makes it runnable by `mosaic-run --dev test`.

---

## 9. Checking a Run Manually

A person reads the run output and the resulting `Orchestration.md`. That is fine, but "look at it and see" is not good enough on its own.

**So every workflow document lists, up front, the steps the run should produce.** Because every stub is driven by fixtures, we know this before running anything. Checking a run then means comparing the real execution log against that list.

Here is the idea, for a three-row Mode 1 workflow:

| Step | Who runs | Kind | Result |
|------|----------|------|--------|
| 1 | `orchestrator-script` | consultation | dispatch → `mosaictest-scripted` (row 1) |
| 2 | `mosaictest-scripted` | workflow step, row 1 | SUCCESS |
| 3 | `orchestrator-script` | consultation | dispatch → `mosaictest-scripted` (row 2) |
| 4 | `mosaictest-scripted` | workflow step, row 2 | SUCCESS |
| 5 | `orchestrator-script` | consultation | dispatch → `mosaictest-scripted` (row 3) |
| 6 | `mosaictest-scripted` | workflow step, row 3 | SUCCESS |
| 7 | `orchestrator-script` | consultation | stop |

Each workflow document fills this in with the real column values, including sequence numbers, phases and stages.

Why bother: it turns checking into a comparison instead of an opinion, and a mismatch tells you where to look.

- **A step whose content is wrong** — wrong status, mangled message, missing marker → suspect the harness, or the Runner's parsing of the reply.
- **A step that is missing, extra, or out of order** → suspect the Runner's routing.

Each workflow document also says what a failure of *its own* mechanism looks like, so someone who did not write it can act on a mismatch.

### 9.1 Where the Actual Run Comes From

**Nothing needs to be built to capture it.** The Runner already writes the run as a table: the Execution Log inside `Orchestration.md`. It has one row per step, with these columns:

`Seq` · `Agent` · `Phase` · `Stage` · `Status` · `Timestamp` · `Summary` · `Inputs` · `Checkpoint`

That table *is* the actual run. Checking a run means comparing it against the expected table in the workflow document. By hand today; by machine later if we choose (§9.2).

There is also a debug log file, which records harness stdin/stdout, parse failures and consultation events. That is for diagnosing *why* a step went wrong, not for checking *which* steps ran.

### 9.2 Automating the Comparison Later

Automating this is worth doing and is smaller than it sounds — but it should come second, not first.

**What makes it small:** the actual run is already a table in a file. A checker reads that table and compares it to an expected one. No instrumentation, no new logging, no changes to the Runner.

**What makes it fiddly:** some columns vary between runs and cannot be compared directly.

| Column | Comparable? |
|--------|------------|
| `Agent`, `Phase`, `Stage`, `Status`, `Seq` | Yes — the Runner decides these, and the same fixtures always produce the same values |
| `Checkpoint` | Presence yes, exact value no — the stub makes up a fake marker |
| `Timestamp` | No — differs every run |
| `Inputs` | Only after removing the run id, which is in every path |
| `Summary` | Loosely — it comes back through a real model, so exact text is not guaranteed. Best checked as "contains this", not "equals this" |

So a byte-for-byte file comparison will not work. A comparison of selected columns will.

**Why second and not first:** the table above is a prediction. Which columns are *actually* stable — especially across different harnesses — is something we learn by running the suite by hand a few times. Building the checker first means guessing at that and then rewriting it. Running by hand first is not wasted effort; it is how we find out what the checker should compare.

**They are separate pieces of work.** The catalogue is markdown authoring — stub agents, workflows, fixtures. The checker is Go code that parses a table and diffs it. Different skills, different review, and they can ship as separate stages. The expected tables written for the manual phase become the checker's input unchanged, so nothing is thrown away.

---

## 10. The Smoke Set

The full suite is 16 workflows and 18 runs per harness (when all planned workflows are implemented). That is too slow to run on every change.

**The smoke set is 4 runs and covers all three modes plus talking to the orchestrator:**

| Run | Why it is in the smoke set |
|-----|---------------------------|
| `smoke-single` | Does the harness work at all? If this fails, nothing else is worth running. |
| `orchestrated-linear` | Mode 1, plus asking the orchestrator what to run next, end to end. |
| `findings-loop` under Auto | Mode 2. |
| `findings-loop` under Auto-review | Mode 3. |

Run the smoke set on any change to the Runner or an adapter. Run the full suite before a release, and whenever a harness is added or upgraded.

---

## 11. Naming Conventions

| Thing | Convention |
|-------|-----------|
| Stub agent names | `mosaictest-*` |
| Behaviour fixtures | `MosaicTestScript/{behaviour}.md`, named after the behaviour |
| Routing fixtures | `MosaicTestRouting.md`, at the seed folder root, one per workflow |
| Artifacts a run writes | Start with `MosaicTest`; per-stage ones as `Stage-{StageNumber}/MosaicTest*` |
| Workflow files | One per test case, named after the mechanism tested |
| `Plan.md` | Keeps its real name — workflow admission requires it. No other reserved name is reused. |

---

## 12. Decisions Worth Recording

**Match fixtures on run state, not on invocation number.** A numbered list keeps answering plausibly when the Runner consults an unexpected number of times, so the exact bug we want to catch produces a green run. State matching stops loudly instead. It also means the stub does not care how consultation rows are labelled in the log.

**A fixed filename for the routing fixture.** `mosaictest-scripted` finds its fixture through its input artifact paths. A consultation carries no artifact paths, so that trick is unavailable and a fixed filename in the run folder is the only option left.

**Same workflow, two modes, for the Auto vs Auto-review difference.** Sharing the assets is what makes the mode the only variable.

**The stub orchestrator stops instead of guessing.** Stop is already part of the contract, needs no new vocabulary, and puts the failure in the run output where the tester is looking.

**Human-review refusal becomes a fixture choice rather than being deleted.** The current automatic refusal encodes something true — a stub with no user-contact tool cannot complete a review gate. Making it one option among several keeps that check while unblocking the approval tests.

**Resume is checked by procedure, not by a workflow.** Testing resume means interrupting a run part-way through, which no workflow definition can express. Instead: run `orchestrated-linear`, kill the process mid-run, restart it against the same run folder, and confirm it continues correctly. Documented as a manual procedure, kept out of the smoke set. Mode cannot change on resume — the Runner reads it back from the artifact and ignores the caller — so there is nothing to test there.

---

## 13. Open Items

### 13.1 Resolved — Stub Enhancements E1 and E3

E1 (`HitlApproval`) and E3 (`HitlBehaviour`) are implemented in mosaictest-scripted v2.3.0. The `hitl-escalate` workflow (§7.8) uses both.

### 13.2 Resume Procedure

Resume testing is documented as a manual procedure (§12, decision 6) but the procedure itself is not written up in `RunningTests.md`. It should be: run `orchestrated-linear`, kill the process mid-run, restart against the same run folder, confirm continuation. This cannot be automated within the current test automation harness — it requires process lifecycle control that `mosaic-run test` does not provide.

### 13.3 Harness Error Simulation

No test workflow exercises the harness-error-to-synthetic-BLOCKED path (Design.md §3.3). This is hard to trigger reliably in E2E: it requires making a real harness call fail (agent file missing at invoke time, timeout, etc.). The session unit tests cover this via fake adapter error injection. Documenting this as a known gap; automated coverage may require a dedicated test mode in the harness adapter.

### 13.5 Resolved

Both earlier open questions are decided:

- **All three harness adapters get the code for asking the orchestrator what to run next.** The goal is to test every harness, so every adapter needs it. (Requirement RUN-4.)
- **The comparison is automated in a second phase**, not the first. See §9.2 for why, and for what the checker can and cannot compare.
