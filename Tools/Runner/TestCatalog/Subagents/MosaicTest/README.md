# MosaicTest

Stub agents that exist to be run, not to do work. They are fixtures for the `mosaic-run` harness conformance suite.

## Purpose

`mosaic-run` (`Tools/Runner`) executes a MOSAIC workflow in plain Go, with no LLM orchestrator in the loop, by shelling out to an agentic harness CLI — Claude Code, GHCP CLI, OpenCode. The **harness boundary** is where the runner's own unit and golden-file tests stop: how each CLI is invoked and with what flags, how the model is selected, how the prompt is delivered, what the response envelope looks like and how the protocol JSON is lifted out of it, exit codes and timeouts, working directory semantics, and the escaping of large or awkward payloads. Every one of those differs per harness and none is exercised by a `FakeHarness`.

Measuring it requires real end-to-end runs, which requires agents. These are those agents: cheap, deterministic, and doing nothing. A run built from them is fully predictable before it starts, so any deviation observed is attributable to the harness rather than to a model's judgement.

**They are not deployed by accident.** `mosaic-deploy` takes an explicit `workflows:` list and resolves agents transitively from each workflow's `referenced_agents`, so this category ships only into a workspace that names a `MosaicTest` workflow.

## Agents

| ID | Agent | Version | Kind | Tier | Description |
|----|-------|---------|------|:----:|-------------|
| 40 | [mosaictest-scripted](./mosaictest-scripted.md) | 2.2.0 | Routed | LOW | Reads the script fixture bound to its row and returns exactly the response it specifies |
| 41 | [mosaictest-checkpoint](./mosaictest-checkpoint.md) | 2.1.0 | Infrastructure, `checkpoint` | LOW | Returns SUCCESS with a fake `[checkpoint:{sha}]` marker; performs no git operations |
| 42 | [mosaictest-review](./mosaictest-review.md) | 2.1.0 | Infrastructure, `review` | LOW | Returns SUCCESS with a self-describing message; inspects nothing |

### The stub orchestrator is not in this folder

A catalogue's orchestrator is loaded from a fixed filename, so this catalogue's stub orchestrator lives at [`../../Orchestrator/orchestrator-script.md`](../../Orchestrator/orchestrator-script.md) and carries no `id` — the schema gives orchestrators none.

It is what `mosaic-run` is pointed at, and it is the reason this catalogue can be run in Orchestrated mode at all. Its fixture format is specified inside its own file. A placeholder `orchestrator.md` sits beside it, never invoked, present only because the deployment tool includes a catalogue's conversational orchestrator unconditionally.

## Behaviour Lives in Fixtures, Not in Agent Files

A routed dispatch carries only `agent_instance_id`, `run_id`, `input_artifacts`, `output_artifacts`, and `human_in_the_loop`. `task_description` and `constraints` are never populated for a routed dispatch, and agent-with-mode notation (`agent(mode)`) is refused at workflow admission. **The artifact paths are therefore the only per-row discriminator available.**

So `mosaictest-scripted` is bound to its behaviour through its Input column: exactly one of its input artifacts sits under a `MosaicTestScript/` prefix, and that file is its instruction set. One agent serves every routed row in every MosaicTest workflow; adding a test case adds a fixture, not an agent.

The script format is specified inside [mosaictest-scripted.md](./mosaictest-scripted.md), so fixture authors and the agent read one spec rather than two.

**The two infrastructure stubs cannot be scripted.** Their dispatch carries no artifacts at all, so there is no channel to hand them a fixture. Their behaviour is baked into their agent files, which is why they are fixed and tiny.

## Conventions

| Thing | Convention |
|---|---|
| Agent slugs | `mosaictest-*` |
| Script fixtures | `MosaicTestScript/{behaviour}.md` — named for behaviour, not row number, so one script serves identical rows across workflows |
| Run artifacts | `MosaicTest*`-prefixed, e.g. `MosaicTestArtifact-1.md`; stage-scoped as `Stage-{StageNumber}/MosaicTestStage.md` |
| Workflows | `Workflows/MosaicTest/` — one workflow file per test case |

`Plan.md` keeps its real name because workflow admission requires it. Nothing else reuses a reserved workflow-semantic name (Plan, Progress, Review, Audit, Research).

## Properties These Agents Must Keep

- **They never refuse meaningless input.** Fixture artifacts are gibberish by design. An agent that objects to content, tidies it, or asks about it destroys the measurement while leaving the run looking healthy. Obeying the fixture *is* the scope.
- **They echo `run_id` and `agent_instance_id` verbatim.** Whether those values survive the harness round trip is one of the things under test, so a normalised or reconstructed value reports a pass that did not happen.
- **`status_message` is the primary readout.** Evaluation is a human watching the runner's TUI, not a golden diff, so every message names its row, phase, stage where meaningful, and the status being returned.
- **They never guess.** Where a fixture fails to determine an answer, the response is `BLOCKED`. A guessed status produces a green run that measured nothing — the only outcome worse than a loud failure.
- **They are stateless across invocations.** `agent_instance_id` is `{agent}#{N}` over a global counter, so it carries no "which invocation of me is this" signal. A marker artifact, checked by content and reset by overwriting, is the only legitimate source of that.

## Design Reference

- [TestCatalogDesign.md](../../../docs/TestCatalogDesign.md) — the suite: what it tests, the stub cast, the fixture formats, the workflow set, and how a run is checked
- [CommunicationProtocol.md](../../../../../Development/Designs/CommunicationProtocol.md) — the request and response envelopes these agents conform to
- [InfrastructureAgentConcept.md](../../../../../Development/Designs/InfrastructureAgentConcept.md) — classes, triggers, evaluation, failure policy

Execution group notation is not referenced here: the staged MosaicTest workflows use bare rows and declare no groups, which is itself one of their assertions.
