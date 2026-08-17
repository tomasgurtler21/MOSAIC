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
| `mosaictest-scripted` | A normal subagent. Returns whatever protocol response its fixture tells it to. | Exists. E2 added; E1 and E3 still needed (§4.1) |
| `orchestrator-script` | A stub orchestrator. Returns whatever routing instruction its fixture tells it to. | Written (§5) |
| `orchestrator` | A placeholder, never invoked (§4.2). | Written |
| `mosaictest-checkpoint` | Checkpoint infrastructure stub. Returns success with a fake checkpoint marker, touches no git. | Exists, but no workflow uses it yet |
| `mosaictest-review` | Review infrastructure stub. Returns success, inspects nothing. | Exists, but no workflow uses it yet |

### 4.1 Three Additions Needed to `mosaictest-scripted`

Some planned workflows cannot be built until these exist.

**E1 — Let the fixture set the approval flag.**
When the stub writes an artifact it stamps provenance frontmatter, but it cannot control the `human_approved` field inside it. To test the approval check we need the stub to write an artifact that is deliberately *not* approved, then one that *is*. So the fixture's write section must specify the approval value.

**E2 — Let the fixture make the stub echo back the task description it received.**
Right now the stub only repeats text from its own fixture. It never reports what the Runner actually sent it. That means nothing in the run shows whether the orchestrator's task description arrived.

This matters more than it sounds: **echoing is the only way this suite can see what was sent in a dispatch.** Without it, Mode 1's whole point — that the orchestrator writes a useful task description and the subagent receives it — is assumed, never checked.

**E3 — Make the human-review refusal a fixture choice instead of automatic.**
Today, if the stub is dispatched with human review turned on, it immediately returns `BLOCKED` / `E503` and does nothing else. That is intentional — it has no tool for talking to a user. But it also means **every human-review row is impossible to test**, so the approval check can never run.

Fix: the fixture decides. One fixture keeps the old refusal behaviour (that check is still worth having). Another tells the stub to write its artifacts and report success, so the approval path can be tested.

---

### 4.2 Why the Stub Orchestrator Is Named `orchestrator-script`

It cannot be called `mosaictest-orchestrator`, and the reason is a hard constraint rather than a preference.

The deployment tool loads a catalogue's orchestrators from **two fixed filenames** inside the catalogue's `Orchestrator/` folder: `orchestrator.md` and `orchestrator-script.md`. It does not scan for them and it does not read a name from frontmatter. A stub under any other filename is simply not loaded as an orchestrator, gets no workflows injected into it, and is therefore useless to the Runner — which reads the run's workflow definition out of the orchestrator file it is given.

Two consequences:

- The stub orchestrator is `TestCatalog/Orchestrator/orchestrator-script.md`. Its frontmatter carries no `id`, because the schema gives orchestrators none.
- A **placeholder `orchestrator.md`** sits beside it. The deployment tool includes a catalogue's conversational orchestrator unconditionally, and a catalogue lacking one deploys an empty unnamed agent artifact while still reporting success. The placeholder removes that failure mode. Nothing invokes it; if it ever runs, it says so and stops.

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

## 7. The Test Workflows

Three existing workflows stay as they are. They test the harness connection itself and are the first thing to run against a new or changed harness: `smoke-single`, `payload-stress`, `staged-preplaced-plan`.

Nine are added:

| Workflow | Mode | What it tests |
|----------|------|--------------|
| `orchestrated-linear` | Orchestrated | Orchestrator is asked before every step including the first; the stop instruction works; the task description it writes actually reaches the subagent |
| `orchestrated-backjump` | Orchestrated | Orchestrator sends the run back to an earlier row; instruction replacements for artifacts, constraints and human review |
| `findings-loop` | Auto **and** Auto-review | The one difference between these two modes (§7.1) |
| `deviation-blocked` | Auto | A `BLOCKED` result becomes a deviation, the orchestrator is asked, and the run carries on |
| `deviation-ambiguous` | Auto | A routing hint the Runner cannot resolve becomes a deviation |
| `deviation-stop` | Auto | The orchestrator ends the run, and the artifact can still be resumed afterwards |
| `hitl-approve` | Auto | The approval check passes — and only after one re-dispatch |
| `hitl-escalate` | Auto | The approval check uses up its re-dispatch and escalates to a deviation |
| `infra-triggers` | Auto | Checkpoint and review triggers fire; the checkpoint marker is picked up; no cascading |
| `preconsult-advice` | Auto | Pre-consultation advice reaches auto-routed dispatches, and does **not** reach orchestrator-written ones |

### 7.1 `findings-loop` — One Workflow, Run Under Two Modes

Auto and Auto-review differ in exactly one way. When a reviewer reports findings:

- **Auto** asks the orchestrator what to do.
- **Auto-review** sends it straight back to the workflow table's findings target without asking, and adds the reviewer's output file to that agent's inputs.

The clearest way to prove this is **the same workflow and the same fixtures, run twice — once per mode — producing two different, separately documented runs.** If we used two different workflows instead, we would only be proving that two different definitions behave differently, not that the *mode* is what changed it.

In Auto-review, the routing fixture's findings rule simply never fires. That the run finishes without ever firing it is the proof.

### 7.2 `preconsult-advice` Needs Both Kinds of Dispatch

Pre-consultation advice is added only to dispatches the Runner builds itself, never to ones the orchestrator writes. To show the "never" half, the run needs both kinds — so this workflow also causes one deviation. The echoing stub (E2) then shows the advice present on the auto-built dispatch and absent on the orchestrator-written one.

---

## 8. Checking a Run

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

### 8.1 Where the Actual Run Comes From

**Nothing needs to be built to capture it.** The Runner already writes the run as a table: the Execution Log inside `Orchestration.md`. It has one row per step, with these columns:

`Seq` · `Agent` · `Phase` · `Stage` · `Status` · `Timestamp` · `Summary` · `Inputs` · `Checkpoint`

That table *is* the actual run. Checking a run means comparing it against the expected table in the workflow document. By hand today; by machine later if we choose (§8.2).

There is also a debug log file, which records harness stdin/stdout, parse failures and consultation events. That is for diagnosing *why* a step went wrong, not for checking *which* steps ran.

### 8.2 Automating the Comparison Later

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

## 9. The Smoke Set

The full suite is 12 workflows and 13 runs per harness. That is too slow to run on every change.

**The smoke set is 4 runs and covers all three modes plus talking to the orchestrator:**

| Run | Why it is in the smoke set |
|-----|---------------------------|
| `smoke-single` | Does the harness work at all? If this fails, nothing else is worth running. |
| `orchestrated-linear` | Mode 1, plus asking the orchestrator what to run next, end to end. |
| `findings-loop` under Auto | Mode 2. |
| `findings-loop` under Auto-review | Mode 3. |

Run the smoke set on any change to the Runner or an adapter. Run the full suite before a release, and whenever a harness is added or upgraded.

---

## 10. Naming Conventions

| Thing | Convention |
|-------|-----------|
| Stub agent names | `mosaictest-*` |
| Behaviour fixtures | `MosaicTestScript/{behaviour}.md`, named after the behaviour |
| Routing fixtures | `MosaicTestRouting.md`, at the seed folder root, one per workflow |
| Artifacts a run writes | Start with `MosaicTest`; per-stage ones as `Stage-{StageNumber}/MosaicTest*` |
| Workflow files | One per test case, named after the mechanism tested |
| `Plan.md` | Keeps its real name — workflow admission requires it. No other reserved name is reused. |

---

## 11. Decisions Worth Recording

**Match fixtures on run state, not on invocation number.** A numbered list keeps answering plausibly when the Runner consults an unexpected number of times, so the exact bug we want to catch produces a green run. State matching stops loudly instead. It also means the stub does not care how consultation rows are labelled in the log.

**A fixed filename for the routing fixture.** `mosaictest-scripted` finds its fixture through its input artifact paths. A consultation carries no artifact paths, so that trick is unavailable and a fixed filename in the run folder is the only option left.

**Same workflow, two modes, for the Auto vs Auto-review difference.** Sharing the assets is what makes the mode the only variable.

**The stub orchestrator stops instead of guessing.** Stop is already part of the contract, needs no new vocabulary, and puts the failure in the run output where the tester is looking.

**Human-review refusal becomes a fixture choice rather than being deleted.** The current automatic refusal encodes something true — a stub with no user-contact tool cannot complete a review gate. Making it one option among several keeps that check while unblocking the approval tests.

**Resume is checked by procedure, not by a workflow.** Testing resume means interrupting a run part-way through, which no workflow definition can express. Instead: run `orchestrated-linear`, kill the process mid-run, restart it against the same run folder, and confirm it continues correctly. Documented as a manual procedure, kept out of the smoke set. Mode cannot change on resume — the Runner reads it back from the artifact and ignores the caller — so there is nothing to test there.

---

## 12. Open Items

None. Both earlier open questions are now decided:

- **All three harness adapters get the code for asking the orchestrator what to run next.** The goal is to test every harness, so every adapter needs it. (Requirement RUN-4.)
- **The comparison is automated in a second phase**, not the first. See §8.2 for why, and for what the checker can and cannot compare.
