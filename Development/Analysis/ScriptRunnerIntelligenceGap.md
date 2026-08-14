# Script Runner Dispatch Intelligence Gap

**Question:** The script runner dispatches subagents with bare-minimum protocol requests — empty `task_description`, no `constraints`. The real orchestrator (an LLM) processes a wealth of context (harness constraints, project state, agent capabilities, prior results) and synthesizes intelligent dispatch messages. This is a fundamental intelligence gap, not a missing feature. How do we give the script runner access to LLM-grade dispatch intelligence without turning it into an LLM orchestrator?

---

## The Problem

### What the real orchestrator does

The LLM orchestrator sits at the center of a rich context:
- **Harness injections** — e.g., "tell subagents where skills are at `cwd/.claude/skills`"
- **Project knowledge** — CLAUDE.md memories, codebase conventions, prior run learnings
- **Workflow semantics** — understanding *why* a subagent is being called, not just *that* it's next in the table

The orchestrator processes all of this and synthesizes it into the `task_description` and `constraints` fields of each dispatch. It doesn't know agent internals (which skill a test-writer uses, how a researcher searches) — but it knows the environment those agents will operate in, and it communicates that. This environmental synthesis is the orchestrator's core value beyond pure routing.

### What the script runner does

The engine's `buildDispatchStep` constructs a `ProtocolRequest` with:
- `agent_instance_id` — filled
- `input_artifacts` / `output_artifacts` — filled from workflow table
- `human_in_the_loop` — computed
- `task_description` — **empty string**
- `constraints` — **never set**

The routing is correct. The subagent gets the right artifacts. But it gets **zero contextual intelligence** about the environment it's operating in.

### Observable symptoms

These are all manifestations of the same root cause:

1. **Skills discovery failure** — orchestrator's HarnessConstraints tell it to relay skills paths; script runner can't
2. **Missing project conventions** — orchestrator might relay `py` instead of `python`; script runner can't
3. **No task framing** — orchestrator explains *why* this agent is being invoked and what to focus on; script runner sends nothing
4. **No cross-step context** — orchestrator might summarize what the previous agent found; script runner can't

Each of these could be "fixed" with a bespoke mechanism (a `--constraints` flag, a deployment-time injection, a new workflow column). But that's whack-a-mole — every new symptom requires new custom code in the runner, and the runner is a deterministic state machine that should stay that way.

---

## Root Cause

The script runner replaced the orchestrator's **routing intelligence** (which is deterministic and table-driven) but not its **dispatch intelligence** (which requires understanding context and synthesizing natural-language guidance). These are two different capabilities, and the runner only replaces one.

The runner's architecture is correct: `engine` is a pure routing function, `session` is the imperative shell. Neither should contain LLM logic. But the gap remains.

---

## Proposed Solution: One-Shot Orchestrator Pre-Consultation

### Concept

At run startup, before the dispatch loop begins, the script runner invokes the **real orchestrator agent** once. The orchestrator already has all the context it needs — its deployed file contains harness injections, project context comes through the harness's normal mechanisms (CLAUDE.md, etc.). No extra context needs to be assembled or passed by the runner.

The orchestrator returns a structured response: generic strings that the runner appends to protocol request fields for all dispatches. The runner doesn't interpret these strings — it just appends them mechanically.

### Key design principle: generic, not per-agent

The orchestrator produces **generic environment-level guidance**, not per-agent overrides. It doesn't know (and shouldn't try to know) what each subagent does internally. It knows things like:
- Where skills live in this project
- What command alias to use for Python
- What harness quirks subagents should be aware of

These are the same things the real orchestrator would include in *every* dispatch message. They're environment facts, not agent-specific instructions.

### How it works

```
Run Start
  ├── Load workflow, resolve agents, read stages (existing)
  ├── *** NEW: Pre-consultation (opt-in) ***
  │     Invoke: real orchestrator agent via HarnessAdapter
  │     Input:  (nothing extra — orchestrator's own deployed context suffices)
  │     Output: structured YAML with field append strings
  │     Store:  append strings held in session state
  ├── Enter dispatch loop
        └── For each dispatch:
              └── engine builds ProtocolRequest (existing)
              └── session appends stored strings to task_description / constraints
              └── session invokes harness (existing)
```

### Example orchestrator output

```yaml
task_description: |
  Skills are located at .claude/skills/ in the project root — read the relevant
  skill from there by name. Use `py` not `python` for the Python interpreter.
constraints: |
  When running Python, always use `py`, never `python`.
```

These strings are appended verbatim to every dispatch. The runner has no opinion on the content.

### Why this solves the problem, not just symptoms

- **The LLM processes its own context once** — harness injections, project conventions, environment facts — and produces a structured result the runner applies mechanically
- **No custom runner code per symptom** — new harness constraint? The orchestrator reads it (it's already in its deployed file) and incorporates it into its output. The runner code doesn't change
- **The runner stays deterministic** — it doesn't interpret the strings, just appends them. Engine stays pure. Session stays an imperative shell
- **No extra context assembly** — unlike a dedicated advisor agent that would need the orchestrator file, agent definitions, and project context passed to it, the real orchestrator already has all of this. The runner just invokes it and reads the response. No context drift risk

### Architecture fit

This fits cleanly into the existing run-start sequence (`session.go` steps 1–11). It would slot in after agent resolution (step 6) and before the dispatch loop. The invocation goes through the same `HarnessAdapter` port as everything else — the orchestrator agent is already resolved by `agentresolve` as an `InvocationOrchestrator` kind. No new infrastructure needed.

The append strings are a new field on session state, consulted in the dispatch loop right before `Harness.Invoke`. The engine remains untouched.

---

## Design Decisions

### Use the real orchestrator agent

Using a dedicated "advisor" agent would require assembling and passing all the context the orchestrator already receives through deployment (harness injections, project injections, etc.). That's duplicated context with drift risk. The orchestrator already has everything — just invoke it with a focused task.

### No per-agent overrides

The orchestrator doesn't know agent internals. It doesn't know which agent uses which skill, or how each agent structures its work. It knows **environment facts** — paths, command aliases, harness quirks. These apply equally to all agents. The output is flat strings to append, not a per-agent map.

### No field restrictions

The response schema allows any protocol request field name. There's no reason to artificially limit to `task_description` and `constraints` — if the orchestrator identifies something that belongs in another field, it should be able to say so. The runner validates that field names correspond to real `ProtocolRequest` fields and rejects unknown ones.

### Opt-in with hard failure

The user explicitly opts into pre-consultation (e.g., a TUI dialog or CLI flag). This is not silent or automatic. If the user opts in and the orchestrator invocation fails for any reason, the run refuses to start — the user explicitly chose this feature and must explicitly acknowledge the risk of proceeding without it. No silent degradation.

### No caching

Caching the orchestrator's output across runs is tempting but risky — project context, CLAUDE.md, and harness injections can change between runs. The cost of one extra LLM invocation at startup is low relative to the cost of a full run with stale guidance. Caching may be worth revisiting later, but not for v1.

---

## Open Questions

1. **What task message does the runner send to the orchestrator for this consultation?** It needs to be precise enough to get structured YAML back, not a free-form essay. Probably a short, fixed prompt baked into the runner: "Given your context, produce YAML with field values to append to every subagent dispatch..."

2. **Output validation scope.** The runner should validate field names against `ProtocolRequest` struct fields. Should it also validate value types (e.g., reject a non-string value for `task_description`)? Probably yes — strict is safer with LLM output.

3. **Append semantics.** For string fields like `task_description`, appending is straightforward (concatenate with newline separator). For slice fields like `input_files`, appending means extending the slice. For bool fields like `human_in_the_loop`, "append" doesn't make sense — should those be rejected, or treated as overrides? Simplest answer: only allow string fields for v1.

4. **How does this interact with infrastructure agent dispatches?** Infrastructure agents already get a hardcoded `TaskDescription` from session (`"infrastructure agent dispatch: {name}"`). Should the append strings also apply to infrastructure dispatches? Probably yes — they operate in the same environment.
