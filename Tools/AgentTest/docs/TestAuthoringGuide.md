# AgentTest — Test Authoring Guide

How to create test suites that exercise an orchestrator's routing decisions. This guide covers the three authored file formats, the directory layout, what goes where, and worked examples.

All examples in this guide use agent names and workflow structures drawn from the real MOSAIC orchestrator and its catalogue workflows, primarily `brownfield-tdd`. Refer to `Catalog/Workflows/` for the full set of production workflow definitions and their routing tables.

---

## Quick Orientation

AgentTest runs a **real LLM orchestrator** through a **real agent harness**, intercepts every collaborator dispatch, returns scripted stub responses, and evaluates whether the orchestrator routed correctly. You author three files per test:

| File | Format | Purpose |
|------|--------|---------|
| `<suite>.suite.yaml` | YAML | Groups tests, sets shared defaults |
| `<test>.test.yaml` | YAML | One test scenario: subject, stubs, seeds, assertions |
| `<test>.stubs.json` | JSON | What each intercepted collaborator returns |

Plus supporting files: **fixture files** (seeded into the sandbox), and **stub agent definitions** (placeholder `.md` files that make dispatches legal).

### How the Orchestrator Works (Test-Relevant Summary)

Understanding what the orchestrator does is essential for writing tests that exercise real routing behavior:

1. **The orchestrator receives a user prompt** containing task description, workflow type, and checkpoint/commit preferences. It validates this configuration before proceeding.
2. **It creates `Orchestration-{run_id}/Orchestration.md`** — the blackboard artifact with YAML frontmatter (`current_state`), an append-only Execution Log table, an Artifacts registry, and Workflow Notes.
3. **It reads the workflow table** (injected into its system prompt during deployment) to determine which subagent to dispatch next. Workflow tables have columns: Phase, Subagent, HITL, On Success, On Findings, Input, Output.
4. **It dispatches subagents** via the Communication Protocol: a JSON task invocation message with `agent_instance_id`, `run_id`, `task_description`, `input_artifacts`, `output_artifacts`, `human_in_the_loop`, etc.
5. **It routes based on the subagent's status code**: `SUCCESS` auto-advances per the On Success column; `COMPLETED_NEEDS_ACTION` routes per On Findings; `BLOCKED` triggers tiered error handling; etc.
6. **Agent instance IDs** follow the format `{AgentName}#{GlobalSequence}` — e.g. `codebase-research#1`, `requirements-refinement#2`. The sequence counter is global and never reused.

Your test stubs return Communication Protocol responses that drive these routing decisions. Your assertions verify the orchestrator dispatched the right agents in the right order given those responses.

---

## Directory Layout

A test suite is a directory containing its suite file, test definitions, stubs, and a `fixtures/` subdirectory for seed content:

```
tests/
  my-suite/
    my-suite.suite.yaml          # suite file
    routing-bug.test.yaml        # test definition
    routing-bug.stubs.json       # stub registry for that test
    happy-path.test.yaml         # another test definition
    happy-path.stubs.json        # its stubs
    fixtures/                    # seed files referenced by $ref
      orchestration-seed.md
      requirements.md
```

Stub agent definitions are shared across suites and live in the test catalogue at `Tools/AgentTest/catalog/Subagents/TestStubs/`. This includes both workflow agent stubs and infrastructure agent stubs — the deploy tool classifies agents by frontmatter fields, not by subdirectory:

```
Tools/AgentTest/
  catalog/
    Subagents/
      TestStubs/
        codebase-research.md
        requirements-refinement.md
        planner-tdd-soft.md
        checkpoint-manager-git.md   # infrastructure agent stub
        commit-manager-git.md       # infrastructure agent stub
        orchestration-review.md     # infrastructure agent stub
        checkpoint-restore-git.md   # infrastructure agent stub
        ...
  tests/
    my-suite/
      ...
```

---

## File 1: Suite File (`.suite.yaml`)

The suite file groups test definitions and provides shared defaults. It is harness-agnostic — the harness is selected at invocation time, not in the file.

### Schema

```yaml
schema_version: 1
id: <unique-suite-id>                    # REQUIRED
description: >                           # optional, for humans
  What this suite tests.
defaults:                                # all optional, override per-test
  timeout: 15m                           # Go duration string
  turn_limit: 60                         # max LLM turns before forced stop
  repetitions: 1                         # how many times to run each test
  pass_rate: 1.0                         # fraction of repetitions that must pass
tests:
  - path: first-test.test.yaml           # relative to this suite file
  - path: second-test.test.yaml
  - path: third-test.test.yaml
    timeout: 5m                          # per-entry override
    repetitions: 3
    pass_rate: 0.67
```

### Field Reference

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `schema_version` | No | int | Always `1` for now |
| `id` | Yes | string | Unique identifier for the suite |
| `description` | No | string | Human-readable description |
| `defaults.timeout` | No | duration | Go duration string (e.g. `5m`, `15m`, `1h`) |
| `defaults.turn_limit` | No | int | Maximum LLM turns |
| `defaults.repetitions` | No | int | Times to run each test |
| `defaults.pass_rate` | No | float | `0.0`-`1.0`, fraction that must pass |
| `tests[].path` | Yes | string | Relative path to `.test.yaml` file |
| `tests[].timeout` | No | duration | Overrides suite default for this entry |
| `tests[].turn_limit` | No | int | Overrides suite default |
| `tests[].repetitions` | No | int | Overrides suite default |
| `tests[].pass_rate` | No | float | Overrides suite default |
| `tests[].stop_after_invocations` | No | int | Force-stop after N collaborator dispatches |

### Rules

- `id` is required. The parser rejects a suite with no `id`.
- `harness` is a **removed key**. If present, the parser emits a specific error. The harness is always selected externally (e.g. `--harness claude-code`).
- Unknown top-level fields produce an `unknown-field` error.
- Setting layering: test definition's own value > suite entry override > suite defaults. Unset at every level = tool default.

---

## File 2: Test Definition (`.test.yaml`)

One test definition describes one scenario: what the subject is, what stubs to use, what files to seed, and what to assert.

### Minimal Example

The simplest useful test — one dispatch, one assertion. Uses the `brownfield-tdd` workflow (Phase: RESEARCH, Subagent: `codebase-research`, On Success: `requirements-refinement`):

```yaml
schema_version: 1
name: research-dispatch
id: 1
version: 1
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial version"
description: >
  Orchestrator dispatches codebase-research as the first workflow step in brownfield-tdd.
layer: orchestrator
negative: false

subject:
  identity: orchestrator
  agent: orchestrator
  workflows: [brownfield-tdd]
  infrastructure_agents: []
  opening_message: |
    Task: Add rate limiting to the API gateway in src/gateway/
    Workflow: brownfield-tdd
    Checkpoints: disabled
  invocation_kind: orchestrator
  model: sonnet
  allowed_tools: [Task, Read, Write, Edit]

stub_registry: research-dispatch.stubs.json
timeout: 5m
turn_limit: 10
stop_after_invocations: 1

assertions:
  invocation_sequence:
    exact: true
    steps:
      - { tool: dispatch, agent: codebase-research }
```

Note: The `infrastructure_agents: []` field is **required** — omitting it is a parse error. Use an empty list when the test does not involve infrastructure agents.

Note: The `opening_message` must include the fields the orchestrator requires to begin — task, workflow type, and checkpoints preference. Without these, the orchestrator prompts the user for configuration instead of dispatching.

#### Fresh-Start vs Resume Opening Messages

The `opening_message` framing determines whether the orchestrator initializes a new workflow or resumes from existing state. This distinction is critical when tests seed an `Orchestration.md` fixture:

**Fresh-start** (no seeded Orchestration.md — testing the first dispatch):
```yaml
opening_message: |
    Task: Add rate limiting to the API gateway
    Workflow: brownfield-tdd
    Checkpoints: disabled
```

**Resume** (seeded Orchestration.md — testing a mid-workflow routing decision):
```yaml
opening_message: |
    Continue the existing workflow run. Read the Orchestration.md in the
    run-scoped folder and resume from where it left off. Do not ask me any
    further questions - proceed immediately and autonomously.
```

**Why this matters:** If you seed an Orchestration.md with prior execution state but use a fresh-start opening message, the orchestrator will overwrite the seeded file with a blank one and start from scratch — dispatching `codebase-research` instead of the agent your test expects. The harness prepends `run_id: {actual_run_id}` to every opening message, so the orchestrator can always derive the run-scoped folder path.

Do NOT include Task/Workflow/Checkpoints fields in a resume message — those fields signal "new workflow" to the orchestrator and trigger the initialization path regardless of existing state.

Note: `exact: true` with N declared steps is the normal shape for a single-decision test. `stop_after_invocations: N` terminates the subject immediately after it receives the Nth reply, so exactly N invocation-log entries are recorded and no phantom entry appears. Do not assert on `final_state`, `execution_log`, or `artifact_created` for artifacts the subject writes after receiving the final reply — the subject is terminated before it can complete that bookkeeping, and its absence is expected behaviour, not evidence corruption.

### Full Example

All features exercised — based on the `brownfield-tdd` workflow routing table (RESEARCH and PLANNING phases shown):

```
| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | codebase-research | N | requirements-refinement | - | Requirements.md | Research.md |
| RESEARCH | requirements-refinement | Y | requirements-review | - | Research.md, Requirements.md | Requirements.md |
| RESEARCH | requirements-review | N | planner-tdd-soft | requirements-refinement | Requirements.md | requirements-review.md |
| PLANNING | planner-tdd-soft | Y | plan-review | - | Research.md, Requirements.md | Plan.md, Stage-*/Plan.md, Stage-*/PlanProgress.md |
| PLANNING | plan-review | N | contracts-designer | planner-tdd-soft | Requirements.md, Plan.md, ... | plan-review.md |
```

This test exercises the RESEARCH and PLANNING phases — five agents dispatched in workflow order, all returning SUCCESS:

```yaml
schema_version: 1
name: research-planning-happy-path
id: 2
version: 2
changelog:
  - version: 1
    date: "2026-01-01"
    changes: "Initial version covering RESEARCH phase only"
  - version: 2
    date: "2026-06-01"
    changes: "Extended to cover PLANNING phase; added planner-tdd-soft and plan-review assertions"
description: >
  Brownfield-tdd RESEARCH + PLANNING phases: codebase-research -> requirements-refinement
  -> requirements-review -> planner-tdd-soft -> plan-review, all returning SUCCESS.
  Verifies the orchestrator follows On Success routing through the first two phases.
layer: orchestrator
negative: false

subject:
  identity: orchestrator
  agent: orchestrator
  workflows: [brownfield-tdd]
  infrastructure_agents: []
  opening_message: |
    Task: Add rate limiting to the API gateway in src/gateway/
    Workflow: brownfield-tdd
    Checkpoints: disabled
  invocation_kind: orchestrator
  model: sonnet
  allowed_tools: [Task, Read, Write, Edit]

stub_registry: research-planning-happy-path.stubs.json

timeout: 15m
turn_limit: 60
stop_after_invocations: 5

assertions:
  invocation_sequence:
    exact: true
    steps:
      - { tool: dispatch, agent: codebase-research }
      - { tool: dispatch, agent: requirements-refinement }
      - { tool: dispatch, agent: requirements-review }
      - { tool: dispatch, agent: planner-tdd-soft }
      - { tool: dispatch, agent: plan-review }
  artifact_created:
    - Orchestration-{run_id}/Research.md
    - Orchestration-{run_id}/Requirements.md
    - Orchestration-{run_id}/Plan.md
  artifact_not_created:
    - Orchestration-{run_id}/ContractsDesign.md
  task_messages:
    - at: 1
      identity: { tool: dispatch, agent: codebase-research }
      human_in_the_loop: false
      required_output_artifacts:
        - Orchestration-{run_id}/Research.md
      task_description_contains: ["rate limiting", "gateway"]
    - at: 2
      identity: { tool: dispatch, agent: requirements-refinement }
      human_in_the_loop: true
      required_input_artifacts:
        - Orchestration-{run_id}/Research.md
      required_output_artifacts:
        - Orchestration-{run_id}/Requirements.md
```

### Field Reference

**Top-level fields:**

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `schema_version` | No | int | Always `1` |
| `name` | Yes | string | Human-readable display name for the test. This is what the stub registry's `test_id` field must match |
| `id` | Yes | int | Stable numeric identity, positive, unique across all test definitions in the repository. Never changes even if `name` changes |
| `version` | Yes | int | Content version. Start at `1`; increment when assertions, stubs, fixtures, or seed files change |
| `changelog` | Yes | list | Version history. Must contain at least one entry whose `version` field matches the top-level `version` field |
| `description` | No | string | Human-readable description |
| `layer` | Yes | string | `orchestrator` or `subagent` |
| `negative` | No | bool | Default `false`. When `true`, assertion outcomes are inverted (except echo fidelity) |
| `subject` | Yes | object | The agent under test (see below) |
| `stub_registry` | Yes | string | Relative path to `.stubs.json` file |
| `stub_agents` | No | list | Stub agent definitions to render (see below) |
| `timeout` | No | duration | Go duration string |
| `turn_limit` | No | int | Max LLM turns |
| `repetitions` | No | int | Override suite-level repetitions |
| `pass_rate` | No | float | Override suite-level pass rate |
| `stop_after_invocations` | No | int | Force-stop after N collaborator dispatches. Termination fires immediately after the subject receives the Nth reply. Use `exact: true` with exactly N declared steps as the normal single-decision shape. Do not assert on `final_state`, `execution_log`, or artifacts the subject writes after receiving reply N — those may be absent. |
| `seed_files` | No | list | Files to place in sandbox before run |
| `parallel_groups` | No | list | Declare which collaborators may run concurrently |
| `assertions` | No | object | What to verify after the run |

**`subject` fields:**

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `identity` | Yes | string | The subject's identity name |
| `agent` | Yes | string | Catalogue agent key used for deployment |
| `workflows` | No | list of strings | Workflow IDs to inject. `null`/absent = all, `[]` = none, `["id"]` = exactly these |
| `infrastructure_agents` | **Yes** | list of strings | Infrastructure agent IDs to deploy. `[]` = none (region stays empty), `["id"]` = exactly these. **Absent field is a parse error** — every test must declare this explicitly |
| `opening_message` | Yes | string | The initial message the subject receives |
| `invocation_kind` | Yes | string | `orchestrator` or `subagent` |
| `model` | No | string | Model identifier for the subject (e.g. `sonnet`, `claude-sonnet-4-5`) |
| `stub_model` | No | string | Model identifier for stubs deployed alongside the subject on the catalogue path. Optional: omitting it gives stubs the same model as `subject.model`. |
| `allowed_tools` | No | list | Tools the subject may use |

**`stub_agents` entries:**

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `identity.tool` | Yes | string | Tool name (e.g. `dispatch`, `Task`) |
| `identity.agent` | Yes | string | Agent identity |
| `source` | Yes | string | Relative path to the `.md` stub agent definition |

**`seed_files` entries:**

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `path` | Yes | string | Sandbox-relative path. Supports `{run_id}` placeholder |
| `content` | No | string | Inline content (mutually exclusive with `ref`) |
| `ref` | No | string | Filename in the `fixtures/` directory (resolved via `$ref`). Mutually exclusive with `content` |

**`parallel_groups` entries:**

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `name` | Yes | string | Group name (referenced by assertions) |
| `members` | Yes | list | Collaborator identities (`tool` + `agent`) expected to run concurrently |

### The `layer` Field

| Value | Meaning | Effect |
|-------|---------|--------|
| `orchestrator` | Testing an orchestrator's routing decisions | Protocol-message validation is suppressed (the orchestrator doesn't speak the protocol itself) |
| `subagent` | Testing a single subagent's behavior | Protocol-message validation is active |

### The `negative` Field

When `negative: true`, every assertion's pass/fail outcome is **inverted after evaluation** — a test that would normally fail now passes, and vice versa. This is for testing that the orchestrator does *not* do something under specific conditions.

Exception: **echo fidelity** is never inverted. A negative test cannot "expect" the tool's own echo mechanism to be broken.

### Versioning Discipline

Every test definition has a `version` (content version) and a `changelog` (version history). These track what changed in the test itself, independent of the test tool or schema format.

**`version` vs `schema_version`:**
- `schema_version` is the format version — always `1`, incremented only when the test definition schema changes. You do not control this.
- `version` is the content version — starts at `1`, incremented by you when the test's assertions, stubs, fixtures, or seed files change.

**When to bump `version`:**

Bump `version` and add a changelog entry when you modify any of these:
- Assertions (adding, removing, or changing `invocation_sequence`, `task_messages`, `final_state`, etc.)
- The stub registry file referenced by the test
- Fixture files referenced by `seed_files` or stub side effects
- Seed file paths or content

Do not bump `version` for cosmetic edits (whitespace, comment changes, description rewording) that do not affect what the test asserts.

**Changelog format:**

Each `changelog` entry has three fields:
```yaml
changelog:
  - version: 2
    date: "2026-06-15"
    changes: "Added task_messages assertion for the plan-review invocation"
```

At least one entry must have a `version` that matches the top-level `version` field. Additional entries document prior versions. Keep entries in descending version order (newest first) by convention.

**Why versioning matters:**

The test results storage system records `test_version` alongside each run's results. When you bump a test's version after changing its assertions, stored results from the old version are flagged as potentially stale by the summary generator — they were measured against a different set of assertions. This lets you distinguish "this test was always passing" from "this test passed before we made it stricter."

---

## File 3: Stub Registry (`.stubs.json`)

The stub registry declares what each intercepted collaborator returns. This is JSON, not YAML — it is both machine-read and machine-written, and byte comparison matters for echo fidelity.

### Minimal Example

No stubs at all — test with `on_unmatched: "halt"` to stop on the first dispatch:

```json
{
  "schema_version": 1,
  "test_id": "my-test",
  "on_unmatched": "halt",
  "stubs": []
}
```

### Full Example

A complete stub registry for the brownfield-tdd RESEARCH+PLANNING happy-path test. Each stub response follows the Communication Protocol output format — these are the JSON payloads the orchestrator receives as subagent replies:

```json
{
  "schema_version": 1,
  "test_id": "research-planning-happy-path",
  "on_unmatched": "halt",
  "stubs": [
    {
      "match": { "tool": "dispatch", "agent": "codebase-research", "invocation": 1 },
      "response": {
        "agent_instance_id": "codebase-research#1",
        "run_id": "test-run",
        "status_code": "SUCCESS",
        "status_message": "Analyzed gateway codebase. Found Express middleware pattern, existing auth middleware as reference."
      },
      "side_effects": {
        "create_files": [
          { "path": "Orchestration-{run_id}/Research.md", "content": "---\nrun_id: test-run\ncreated_by: codebase-research#1\nhuman_approved: false\n---\n# Research\n## Codebase Analysis\nExpress middleware pattern. Existing auth middleware as reference.\n" }
        ]
      }
    },
    {
      "match": { "tool": "dispatch", "agent": "requirements-refinement", "invocation": 1 },
      "response": {
        "agent_instance_id": "requirements-refinement#2",
        "run_id": "test-run",
        "status_code": "SUCCESS",
        "status_message": "Refined requirements with rate limiting specifics."
      },
      "side_effects": {
        "create_files": [
          { "path": "Orchestration-{run_id}/Requirements.md", "content": "---\nrun_id: test-run\ncreated_by: requirements-refinement#2\nhuman_approved: true\n---\n# Requirements\n## Rate Limiting\n- 100 requests/minute per API key\n- Token bucket algorithm\n" }
        ]
      }
    },
    {
      "match": { "tool": "dispatch", "agent": "requirements-review", "invocation": 1 },
      "response": {
        "agent_instance_id": "requirements-review#3",
        "run_id": "test-run",
        "status_code": "SUCCESS",
        "status_message": "Requirements review passed. No findings."
      },
      "side_effects": {
        "create_files": [
          { "path": "Orchestration-{run_id}/requirements-review.md", "content": "---\nrun_id: test-run\ncreated_by: requirements-review#3\nhuman_approved: false\n---\n# Requirements Review\nNo findings.\n" }
        ]
      }
    },
    {
      "match": { "tool": "dispatch", "agent": "planner-tdd-soft", "invocation": 1 },
      "response": {
        "agent_instance_id": "planner-tdd-soft#4",
        "run_id": "test-run",
        "status_code": "SUCCESS",
        "status_message": "Created 2-stage plan for rate limiting implementation."
      },
      "side_effects": {
        "create_files": [
          { "path": "Orchestration-{run_id}/Plan.md", "content": "---\nrun_id: test-run\ncreated_by: planner-tdd-soft#4\nhuman_approved: true\n---\n# Plan\n## Stages\n| Stage | Description | Approach | HITL | Depends On |\n|-------|------------|----------|------|------------|\n| 1 | Rate limiter middleware | TDD | No | - |\n| 2 | Redis-backed token bucket | TDD | No | 1 |\n" },
          { "path": "Orchestration-{run_id}/Stage-1/Plan.md", "content": "# Stage 1 Plan\n" },
          { "path": "Orchestration-{run_id}/Stage-1/PlanProgress.md", "content": "# Stage 1 Progress\n" },
          { "path": "Orchestration-{run_id}/Stage-2/Plan.md", "content": "# Stage 2 Plan\n" },
          { "path": "Orchestration-{run_id}/Stage-2/PlanProgress.md", "content": "# Stage 2 Progress\n" }
        ]
      }
    },
    {
      "match": { "tool": "dispatch", "agent": "plan-review", "invocation": 1 },
      "response": {
        "agent_instance_id": "plan-review#5",
        "run_id": "test-run",
        "status_code": "SUCCESS",
        "status_message": "Plan review passed. No findings."
      },
      "side_effects": {
        "create_files": [
          { "path": "Orchestration-{run_id}/plan-review.md", "content": "---\nrun_id: test-run\ncreated_by: plan-review#5\nhuman_approved: false\n---\n# Plan Review\nNo findings.\n" }
        ]
      }
    }
  ]
}
```

**Key points about stub responses:**
- The `response` object is the exact Communication Protocol JSON the orchestrator receives. It must include `agent_instance_id`, `run_id`, `status_code`, and `status_message`.
- `agent_instance_id` follows the orchestrator's naming: `{AgentName}#{GlobalSequence}` — use the same agent key as the workflow table, suffixed with the expected sequence number.
- `status_code` is what drives the orchestrator's routing decision. This is the primary lever for creating test conditions.
- Side effects create files the orchestrator expects to find after a subagent completes (the output artifacts declared in the workflow table). Artifact files should include provenance frontmatter (`run_id`, `created_by`, `human_approved`) per the Communication Protocol.

### Field Reference

**Top-level:**

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `schema_version` | No | int | Always `1` |
| `test_id` | Yes | string | Must match the test definition's `name` field (the human-readable display name, not the numeric `id`) |
| `on_unmatched` | Yes | string | `"halt"`, `"passthrough"`, or `"generic-response"` |
| `generic_response` | Conditional | object | Required when `on_unmatched` is `"generic-response"` |
| `stubs` | Yes | array | The stub entries |

**`on_unmatched` policies:**

| Policy | Behavior |
|--------|----------|
| `"halt"` | Unmatched dispatch stops the subject's turn immediately. Use this for most tests — you want to know if an unexpected dispatch happens |
| `"passthrough"` | Unmatched dispatch is allowed to proceed to the real collaborator. Rarely used |
| `"generic-response"` | Unmatched dispatches receive the `generic_response` payload. Useful when you don't care about certain collaborators' responses |

**`stubs[]` entries:**

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `match.tool` | Yes | string | Tool name the orchestrator dispatches with |
| `match.agent` | Yes | string | Agent identity being dispatched |
| `match.invocation` | Yes | int | 1-based per-identity invocation ordinal. The first time `researcher` is dispatched = `1`, the second time = `2`, etc. Counters are **per identity**, not global |
| `response` | Yes | object | The JSON payload returned to the orchestrator. This is the Communication Protocol response the orchestrator sees |
| `side_effects` | No | object | Files to create when this stub matches |

**`response` object:**

The response is the exact JSON the orchestrator receives as the collaborator's reply. AgentTest treats it as opaque JSON — it is returned verbatim to the orchestrator. For MOSAIC orchestrator tests, this must follow the Communication Protocol output format:

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `agent_instance_id` | Yes | string | Format: `{AgentName}#{GlobalSequence}`, e.g. `"planner-tdd-soft#1"`, `"implementation-tdd#3"` |
| `run_id` | Yes | string | Echo the run ID. Any string for testing |
| `status_code` | Yes | string | One of the 6 status codes — this is what drives the orchestrator's routing |
| `status_message` | Yes | string | 1-2 sentence description of outcome |
| `error_code` | Only if BLOCKED | string | `E101`, `E401`, `E501`, `E502`, or `E503` |
| `error_reason` | Only if BLOCKED | string | Human-readable explanation of the blocker |

**The 6 status codes and what routing they trigger:**

| Status Code | Orchestrator Action |
|-------------|---------------------|
| `SUCCESS` | Auto-advance to next subagent per workflow's On Success column |
| `COMPLETED_NEEDS_ACTION` | Route to subagent per On Findings column (e.g. reviewer sends findings back to creator) |
| `PARTIALLY_DONE` | Route to successor subagent (same type) to continue |
| `NEEDS_CLARIFICATION` | Provide context, callback to prior subagent, or escalate to human |
| `CAPABILITY_EXCEEDED` | Try close alternative or escalate to human |
| `BLOCKED` | Apply tiered error handling based on `error_code` |

**`side_effects.create_files[]`:**

| Field | Required | Type | Notes |
|-------|----------|------|-------|
| `path` | Yes | string | Sandbox-relative path. Supports `{run_id}` placeholder. Must not escape the sandbox |
| `content` | Yes (or `ref`) | string | File content to write |
| `ref` | Yes (or `content`) | string | Fixture file reference (resolved from `fixtures/` directory) |

### Stub Matching Rules

- Stubs are matched by the composite key: `(tool, agent, invocation)`.
- The `invocation` ordinal is **per identity** — if the orchestrator dispatches `researcher` three times, the ordinals are `1`, `2`, `3` for that identity. A different agent (e.g. `planner`) has its own independent counter starting at `1`.
- If no stub matches and `on_unmatched` is `"halt"`, the run stops with an unmatched-invocation condition.
- Stub order in the array does not matter — matching is by key, not position.

### Echo Fidelity

Every stub response is subject to **echo fidelity verification**: after the interception pipeline returns the stub response to the orchestrator, it checks (at the post-invocation point, if the harness supports it) that the response the orchestrator received matches the stub exactly. This is evaluated unconditionally on every run — you never declare it, and a negative test never inverts it. Comparison is JSON-semantic (key order and whitespace are ignored), but any surrounding prose added by the harness causes a mismatch.

---

## Fixture Files

Fixture files are content referenced by `$ref` (in seed files or side effects). They live in a `fixtures/` subdirectory relative to the test definition.

### Orchestration Document Fixture

The most common fixture is a pre-built orchestration document used to seed the sandbox, giving the orchestrator a starting state mid-workflow. This is a standard MOSAIC orchestration document with YAML frontmatter and four region blocks (open/close XML-style tags with a `type` attribute).

**Example: brownfield-tdd workflow after PLANNING phase completes.** The orchestrator should resume from here and dispatch `contracts-designer` next (or skip to `test-writer-tdd` if contracts are not needed):

```markdown
---
type: orchestration-artifact
run_id: "test-run"
workflow: brownfield-tdd
workflow_version: "3.7"
task: "Add rate limiting to the API gateway in src/gateway/"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:25:00Z
global_sequence: 5
checkpoints: disabled
commits: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "plan-review#5"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |
|-----|-------|-------|-------|--------|-----------|---------|--------|------------|
| 1 | codebase-research#1 | RESEARCH | - | SUCCESS | 2026-01-01T00:05:00Z | Analyzed gateway codebase. | - | - |
| 2 | requirements-refinement#2 | RESEARCH | - | SUCCESS | 2026-01-01T00:10:00Z | Refined requirements. | Research.md | - |
| 3 | requirements-review#3 | RESEARCH | - | SUCCESS | 2026-01-01T00:15:00Z | Requirements review passed. | Requirements.md | - |
| 4 | planner-tdd-soft#4 | PLANNING | - | SUCCESS | 2026-01-01T00:20:00Z | Created 2-stage plan. | Research.md, Requirements.md | - |
| 5 | plan-review#5 | PLANNING | - | SUCCESS | 2026-01-01T00:25:00Z | Plan review passed. | Plan.md, Stage-1/Plan.md | - |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
|----------|------------|------------|
| Research.md | RESEARCH | codebase-research#1 |
| Requirements.md | RESEARCH | requirements-refinement#2 |
| requirements-review.md | RESEARCH | requirements-review#3 |
| Plan.md | PLANNING | planner-tdd-soft#4 |
| Stage-1/Plan.md | PLANNING | planner-tdd-soft#4 |
| Stage-1/PlanProgress.md | PLANNING | planner-tdd-soft#4 |
| Stage-2/Plan.md | PLANNING | planner-tdd-soft#4 |
| Stage-2/PlanProgress.md | PLANNING | planner-tdd-soft#4 |
| plan-review.md | PLANNING | plan-review#5 |
</Artifacts>

<WorkflowNotes type="core">
| Seq | Note |
|-----|------|
</WorkflowNotes>
```

**Frontmatter fields that matter for assertions:**
- `current_state.phase` and `current_state.last_status` — what the `final_state` assertion checks against
- `global_sequence` — must be consistent with the Execution Log; the orchestrator increments from this value

**Region tags** (e.g. `<ExecutionLog type="core">` / `</ExecutionLog>`) — must be present and intact, each on its own line. The parser uses them to locate sections, not heading structure.

The `current_state` frontmatter and the `<ExecutionLog type="core">` table are what the `final_state` and `execution_log` assertions evaluate against. Seed this when you want to test the orchestrator's behavior **given a specific existing state** (e.g., mid-workflow routing decisions), rather than having it build state from scratch.

### The `{run_id}` Placeholder

AgentTest generates a `RunID` per attempt (format `{YYYYMMDD}T{HHMMSS}Z-{4hex}`, e.g. `20260813T143022Z-7b3a`) and prepends `run_id: {RunID}` to the subject's opening message. The orchestrator adopts this as its own `run_id`. This means the test tool — not the orchestrator — controls the run identity, making seed files and assertions predictable.

Use `{run_id}` in **seed file paths** and **assertion paths** — the runner expands the placeholder before writing or checking:

```yaml
seed_files:
  - path: Orchestration-{run_id}/Orchestration.md
    ref: orchestration-seed.md

assertions:
  artifact_created:
    - Orchestration-{run_id}/Plan.md
```

`{run_id}` is expanded by the interception pipeline in three places:

1. **Stub side-effect paths** (`side_effects.create_files[].path`) — before writing files.
2. **Stub side-effect content** (`side_effects.create_files[].content`) — before materialising inline content.
3. **Stub response JSON** (`response` object fields) — before returning the response to the orchestrator.

Always use `"{run_id}"` for the `run_id` field in stub responses, not a hardcoded literal. The orchestrator can validate that the echoed `run_id` matches its own, and a mismatch causes nondeterministic re-dispatch.

---

## Stub Agent Definitions

Stub agent definitions live in the test catalogue at `Tools/AgentTest/catalog/Subagents/TestStubs/`. They exist solely to make a dispatch "legal" from the harness's perspective — the harness needs an agent file to exist for the dispatch target. **All actual behavior comes from the stub registry, not from these files.**

When using the `--catalog-folder` deployment path (see Design.md §3), all agents referenced by the test workflow are deployed from the test catalogue in a single `mosaic-deploy deploy` call. Individual `stub_agents` entries in the test definition are not needed — the catalogue contains the stub definitions.

### When You Need One

You need a stub agent definition when a test workflow references an agent that does not yet have a file in `catalog/Subagents/TestStubs/`. All 11 agents from the `brownfield-tdd` workflow already have stubs. If your test uses a custom workflow with a new agent name, create a stub for it.

### File Format

Stub agent definitions follow the standard generic-form agent structure with echo instructions:

```markdown
---
id: {next-available-id}
version: 1.0.0
name: {agent-name}
description: Test stub standing in for the {agent-name} collaborator
role: subagent
model: "{model-identifier}"
recommended_tier: TEST-STUB
tier_rationale: Test stub receiving cheap model via shared TEST-STUB tier mapping
tools: []
---

<Identity type="core">
# {Agent Display Name} — Test Echo Agent

You are a test echo agent in an automated test scenario. Your only job is exact reproduction.

When you receive a prompt asking you to respond with specific content, reproduce that content exactly as given. Do not add commentary, explanation, formatting, or wrapping. Do not modify, summarize, or interpret the content. Output only the requested content and nothing else.
</Identity>
```

### Naming Convention

File name = `<agent-key>.md`, where `<agent-key>` matches the `agent` field of the collaborator identity. Use the exact agent key from the workflow table's `Subagent` column:

```
catalog/Subagents/TestStubs/codebase-research.md          -> matches agent: codebase-research
catalog/Subagents/TestStubs/requirements-refinement.md    -> matches agent: requirements-refinement
catalog/Subagents/TestStubs/requirements-review.md        -> matches agent: requirements-review
catalog/Subagents/TestStubs/planner-tdd-soft.md           -> matches agent: planner-tdd-soft
catalog/Subagents/TestStubs/plan-review.md                -> matches agent: plan-review
catalog/Subagents/TestStubs/implementation-tdd.md         -> matches agent: implementation-tdd
catalog/Subagents/TestStubs/implementation-review.md      -> matches agent: implementation-review
catalog/Subagents/TestStubs/test-runner.md                -> matches agent: test-runner
```

### ID Assignment

Each stub agent file carries a numeric ID unique within the `TestStubs/` directory. When adding a new stub, assign the next available integer.

### Current Inventory

| ID | File | Agent Key |
|----|------|-----------|
| 1 | `requirements-refinement.md` | `requirements-refinement` |
| 2 | `researcher.md` | `researcher` |
| 3 | `library-researcher.md` | `library-researcher` |
| 4 | `planner.md` | `planner` |
| 5 | `codebase-research.md` | `codebase-research` |
| 6 | `requirements-review.md` | `requirements-review` |
| 7 | `planner-tdd-soft.md` | `planner-tdd-soft` |
| 8 | `plan-review.md` | `plan-review` |
| 9 | `contracts-designer.md` | `contracts-designer` |
| 10 | `contracts-review.md` | `contracts-review` |
| 11 | `test-writer-tdd.md` | `test-writer-tdd` |
| 12 | `tests-review-tdd.md` | `tests-review-tdd` |
| 13 | `implementation-tdd.md` | `implementation-tdd` |
| 14 | `implementation-review.md` | `implementation-review` |
| 15 | `test-runner.md` | `test-runner` |
| 16 | `checkpoint-manager-git.md` | `checkpoint-manager-git` |
| 17 | `commit-manager-git.md` | `commit-manager-git` |
| 18 | `orchestration-review.md` | `orchestration-review` |
| 19 | `checkpoint-restore-git.md` | `checkpoint-restore-git` |
| 20 | `orchestration-review-interval-10.md` | `orchestration-review-interval-10` |
| 21 | `infra-phase-end.md` | `infra-phase-end` |
| 22 | `restore-stage-end.md` | `restore-stage-end` |
| 23 | `orchestration-review-interval-3.md` | `orchestration-review-interval-3` |
| 24 | `checkpoint-stage-end-only.md` | `checkpoint-stage-end-only` |

Infrastructure agent stubs (IDs 16-19) live in the same `TestStubs/` directory — the deploy tool classifies them by frontmatter (`infrastructure`, `triggers`, `on_failure` fields), not by subdirectory. Variant stubs (IDs 20+) are test-specific configurations with different trigger parameters. See the [Infrastructure Agent Stubs](#infrastructure-agent-stubs) section below.

---

## Infrastructure Agent Stubs

Infrastructure agents fire on trigger conditions (not workflow routing) and perform orchestration-support work such as checkpointing and periodic review. They are declared in the orchestrator's `<InfrastructureAgents>` region, which the deploy tool populates from agent frontmatter when infrastructure agent IDs are specified.

### Default Set

The test catalogue ships four infrastructure agent stubs in `catalog/Subagents/TestStubs/`, alongside the workflow agent stubs. They mirror the production agents with production-equivalent triggers:

| ID | File | Agent Key | Class | Triggers | On Failure |
|----|------|-----------|-------|----------|------------|
| 16 | `checkpoint-manager-git.md` | `checkpoint-manager-git` | `checkpoint` | STAGE_END, INVOCATION_INTERVAL(10) | halt |
| 17 | `commit-manager-git.md` | `commit-manager-git` | `commit` | STAGE_END | continue |
| 18 | `orchestration-review.md` | `orchestration-review` | `review` | INVOCATION_INTERVAL(30) | continue |
| 19 | `checkpoint-restore-git.md` | `checkpoint-restore-git` | `restore` | MANUAL | halt |

These stubs use the same echo-agent body as workflow stub agents — all actual response behaviour comes from the stub registry. The infrastructure-specific frontmatter fields (`infrastructure`, `triggers`, `on_failure`) are what the deploy tool reads to assemble the `<InfrastructureAgent>` declaration block in the orchestrator.

### Using Infrastructure Agents in Tests

Declare the agents you need in `subject.infrastructure_agents`:

```yaml
subject:
  agent: orchestrator
  workflows: [brownfield-tdd]
  infrastructure_agents: [checkpoint-manager-git, commit-manager-git]
  # ...
```

Each listed agent must have a stub registry entry in `.stubs.json` so the interception pipeline knows what to return when the orchestrator dispatches it. Infrastructure agent dispatches consume sequence numbers and appear in the invocation log just like workflow agent dispatches.

### Creating Trigger Variants

The default stubs carry production-equivalent triggers, but tests often need different trigger configurations. For example:

- **I-2** tests `INVOCATION_INTERVAL` — production uses interval 10, but a test with `stop_after_invocations: 3` needs interval 2 to fire within the test's lifespan.
- **I-5** tests `PHASE_END` — no production agent uses this trigger, so a synthetic test-only agent is needed.

Create variant stubs directly in `catalog/Subagents/TestStubs/` with descriptive names:

```markdown
---
id: 20
version: 1.0.0
name: checkpoint-interval-3
description: Test variant — checkpoint agent with INVOCATION_INTERVAL(3) for short tests
role: subagent
model: "{model-identifier}"
recommended_tier: TEST-STUB
tier_rationale: Test stub receiving cheap model via shared TEST-STUB tier mapping
tools: []
infrastructure: checkpoint
triggers:
  - trigger: INVOCATION_INTERVAL
    trigger_param: 3
on_failure: halt
---

<Identity type="core">
# CheckpointInterval3 — Test Echo Agent

You are a test echo agent in an automated test scenario. Your only job is exact reproduction.

When you receive a prompt asking you to respond with specific content, reproduce that content exactly as given. Do not add commentary, explanation, formatting, or wrapping. Do not modify, summarize, or interpret the content. Output only the requested content and nothing else.
</Identity>
```

The agent name is arbitrary — it is the `infrastructure` class field that classifies the agent, not the name. A test references the variant by its name:

```yaml
infrastructure_agents: [checkpoint-interval-3]
```

**Guidelines for variants:**

- Keep the echo-agent body identical across all variants — only frontmatter differs.
- Use descriptive names that encode the variant's purpose: `checkpoint-interval-3`, `infra-phase-end`, `checkpoint-stage-end-only`.
- Assign the next available ID in `TestStubs/` (continuing from 19).
- Variants are cheap — one file per configuration. Create as many as your tests need.
- A gated class (checkpoint, commit, restore) allows at most one active agent per class. Two agents of the same gated class in `infrastructure_agents` is a deployment error.

---

## Assertions Reference

All assertions are optional. Omitting an assertion class means it is not evaluated — distinct from declaring it with an empty/zero value.

### `invocation_sequence`

Asserts on the order of collaborator dispatches.

```yaml
assertions:
  invocation_sequence:
    exact: true       # true = observed must match exactly; false = subsequence match
    steps:
      - { tool: dispatch, agent: planner-tdd-soft }
      - { tool: dispatch, agent: plan-review }
      - { tool: dispatch, agent: implementation-tdd }
      - { tool: dispatch, agent: test-runner }
```

**With a parallel group** (members can appear in any order, but the group as a whole is ordered relative to other steps):

```yaml
assertions:
  invocation_sequence:
    exact: true
    steps:
      - { tool: dispatch, agent: requirements-refinement }
      - group: research-fanout
        members:
          - { tool: dispatch, agent: researcher }
          - { tool: dispatch, agent: library-researcher }
      - { tool: dispatch, agent: planner-tdd }
```

When `exact: true`, the observed sequence must contain exactly these steps and nothing else. This is the normal shape for tests that use `stop_after_invocations: N` — declare exactly N steps with `exact: true`, and the assertion matches the pipeline's record precisely. When `exact: false`, these steps must appear as a subsequence of the observed sequence (other dispatches may appear between them).

### `final_state`

Asserts on the orchestration document's final state (from its YAML frontmatter `current_state`):

```yaml
assertions:
  final_state:
    phase: COMPLETED
    last_status: SUCCESS
```

Both `phase` and `last_status` are optional independently.

### `execution_log`

Asserts on the execution log table in the orchestration document:

```yaml
assertions:
  execution_log:
    agent_ids: ["planner-tdd-soft#1", "plan-review#2", "implementation-tdd#3", "test-runner#4"]
    all_status: SUCCESS                            # every entry has this status
```

Both `agent_ids` and `all_status` are optional independently.

### `protocol_violations`

Asserts the exact count of Communication Protocol violations found in intercepted messages:

```yaml
assertions:
  protocol_violations: 0
```

Note: this assertion is only meaningful for `layer: subagent` tests. For `layer: orchestrator`, protocol-message validation is suppressed.

### `artifact_created`

Asserts that named files exist in the sandbox after the run:

```yaml
assertions:
  artifact_created:
    - Orchestration-{run_id}/Plan.md
    - Orchestration-{run_id}/plan-review.md
```

### `artifact_not_created`

Asserts that named files do NOT exist in the sandbox after the run:

```yaml
assertions:
  artifact_not_created:
    - Orchestration-{run_id}/Design.md
```

### `min_concurrency`

Asserts that a declared parallel group achieved at least the specified number of simultaneous invocations:

```yaml
assertions:
  min_concurrency:
    research-fanout: 2
```

Requires a matching `parallel_groups` declaration in the test definition.

### `task_messages`

Asserts on the content of individual task invocation messages sent by the orchestrator:

```yaml
assertions:
  task_messages:
    - at: 2                                                      # 1-based global sequence number
      identity: { tool: dispatch, agent: plan-review }           # cross-check: right agent at this position
      required_input_artifacts:                                   # from the workflow table's Input column
        - Orchestration-{run_id}/Plan.md
      optional_input_artifacts:                                   # may or may not be passed
        - Orchestration-{run_id}/Stage-1/Plan.md
      required_output_artifacts:                                  # from the workflow table's Output column
        - Orchestration-{run_id}/plan-review.md
      human_in_the_loop: false                                    # from the workflow table's HITL column
      task_description_contains: ["review"]
```

**`at`** is the 1-based global sequence number (not per-identity ordinal).

**`identity`** is optional. When set, it cross-checks against the invocation at that sequence position — a mismatch is reported as a sequence drift, not a message content error.

**Artifact assertions** follow set semantics: every `required_*` entry must be present, each `optional_*` entry may be. Anything present that is in neither list fails the assertion.

#### Artifact Path Leniency

The orchestrator sometimes includes extra artifacts beyond the workflow table's declared set (e.g., stage sub-plans alongside the top-level Plan.md), or uses glob-style paths like `Stage-*/Plan.md`. It may also inconsistently omit the `Orchestration-{run_id}/` prefix from artifact paths, passing bare names like `Stage-1/Plan.md` instead of the fully-qualified `Orchestration-{run_id}/Stage-1/Plan.md`.

When your test's purpose is **routing correctness** (did the orchestrator dispatch the right agent?), not artifact path hygiene, handle these variations pragmatically:

- Put the core routing-relevant artifacts in `required_*` (e.g., the review findings file that proves On Findings routing was used)
- Put predictable but non-essential extras in `optional_*` (e.g., stage plans the planner also produces)
- When the orchestrator inconsistently prefixes paths, list both forms in `optional_*`:

```yaml
optional_input_artifacts:
  - Orchestration-{run_id}/Stage-1/Plan.md   # fully-qualified form
  - Stage-1/Plan.md                           # bare form (orchestrator sometimes omits prefix)
```

This keeps the test focused on its routing assertion while tolerating LLM variance in path formatting. If you want to separately assert path correctness, write a dedicated test for that concern.

**`task_description_contains`** checks that each listed substring appears somewhere in the task description of the invocation message.

### Echo Fidelity (Automatic)

Echo fidelity is evaluated unconditionally on every run. It verifies that each stub response reached the orchestrator unchanged. You never declare it. It is never inverted by `negative: true`. A mismatch results in an `echo_mismatch` outcome class.

---

## Step-by-Step: Creating a New Test Suite

### 1. Create the Directory Structure

```
tests/
  my-routing-tests/
    fixtures/
```

### 2. Identify What You Are Testing

Decide the routing condition you want to test. Start from a real workflow table in `Catalog/Workflows/`. For example, the `brownfield-tdd` workflow has:

```
| Phase    | Subagent                | On Success             | On Findings              |
|----------|-------------------------|------------------------|--------------------------|
| RESEARCH | codebase-research       | requirements-refinement| -                        |
| RESEARCH | requirements-refinement | requirements-review    | -                        |
| RESEARCH | requirements-review     | planner-tdd-soft       | requirements-refinement  |
| PLANNING | planner-tdd-soft        | plan-review            | -                        |
| PLANNING | plan-review             | contracts-designer     | planner-tdd-soft         |
| ...      | ...                     | ...                    | ...                      |
```

A good test scenario: "When `requirements-review` returns `COMPLETED_NEEDS_ACTION`, the orchestrator should route back to `requirements-refinement` (per the On Findings column), not forward to `planner-tdd-soft`."

### 3. Create Stub Agent Definitions (if needed)

Check `Tools/AgentTest/catalog/Subagents/TestStubs/` for existing stubs. All 11 `brownfield-tdd` agents already have stubs (IDs 5-15). If your test uses a custom workflow with agent names not yet in the catalogue, create a stub following the format in the [Stub Agent Definitions](#stub-agent-definitions) section and assign the next available ID.

### 4. Write the Stub Registry

Create `my-routing-tests/findings-reroute.stubs.json`. Design the stub responses to create the exact condition you want to test. Here, `requirements-review` returns `COMPLETED_NEEDS_ACTION` to trigger the On Findings route back to `requirements-refinement`:

```json
{
  "schema_version": 1,
  "test_id": "findings-reroute",
  "on_unmatched": "halt",
  "stubs": [
    {
      "match": { "tool": "dispatch", "agent": "codebase-research", "invocation": 1 },
      "response": {
        "agent_instance_id": "codebase-research#1",
        "run_id": "test-run",
        "status_code": "SUCCESS",
        "status_message": "Analyzed gateway codebase."
      },
      "side_effects": {
        "create_files": [
          { "path": "Orchestration-{run_id}/Research.md", "content": "---\nrun_id: test-run\ncreated_by: codebase-research#1\nhuman_approved: false\n---\n# Research\n" }
        ]
      }
    },
    {
      "match": { "tool": "dispatch", "agent": "requirements-refinement", "invocation": 1 },
      "response": {
        "agent_instance_id": "requirements-refinement#2",
        "run_id": "test-run",
        "status_code": "SUCCESS",
        "status_message": "Refined requirements."
      },
      "side_effects": {
        "create_files": [
          { "path": "Orchestration-{run_id}/Requirements.md", "content": "---\nrun_id: test-run\ncreated_by: requirements-refinement#2\nhuman_approved: true\n---\n# Requirements\n" }
        ]
      }
    },
    {
      "match": { "tool": "dispatch", "agent": "requirements-review", "invocation": 1 },
      "response": {
        "agent_instance_id": "requirements-review#3",
        "run_id": "test-run",
        "status_code": "COMPLETED_NEEDS_ACTION",
        "status_message": "Requirements lack acceptance criteria for edge cases."
      },
      "side_effects": {
        "create_files": [
          { "path": "Orchestration-{run_id}/requirements-review.md", "content": "---\nrun_id: test-run\ncreated_by: requirements-review#3\nhuman_approved: false\n---\n# Requirements Review\n## Findings\n1. Missing acceptance criteria for edge cases\n" }
        ]
      }
    },
    {
      "match": { "tool": "dispatch", "agent": "requirements-refinement", "invocation": 2 },
      "response": {
        "agent_instance_id": "requirements-refinement#4",
        "run_id": "test-run",
        "status_code": "SUCCESS",
        "status_message": "Requirements updated with edge case acceptance criteria."
      }
    }
  ]
}
```

Note: `requirements-refinement` appears twice — invocation 1 (initial refinement) and invocation 2 (fixing review findings). Each has its own `match.invocation` ordinal. The `agent_instance_id` sequence numbers (`#1`, `#2`, `#3`, `#4`) must be globally sequential, matching how the orchestrator would assign them.

### 5. Create Fixtures (if needed)

If your test needs a pre-existing orchestration state (e.g. testing a mid-workflow decision), create a fixture file in `fixtures/`. Example — seeding state after PLANNING to test DESIGN or EXECUTION-phase routing in brownfield-tdd:

```markdown
---
type: orchestration-artifact
run_id: "test-run"
workflow: brownfield-tdd
workflow_version: "3.7"
task: "Add rate limiting to the API gateway in src/gateway/"
started: 2026-01-01T00:00:00Z
last_updated: 2026-01-01T00:25:00Z
global_sequence: 5
checkpoints: disabled
commits: disabled
current_state:
  phase: PLANNING
  stage: null
  last_status: SUCCESS
  last_agent: "plan-review#5"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |
|-----|-------|-------|-------|--------|-----------|---------|--------|------------|
| 1 | codebase-research#1 | RESEARCH | - | SUCCESS | 2026-01-01T00:05:00Z | Analyzed gateway codebase. | - | - |
| 2 | requirements-refinement#2 | RESEARCH | - | SUCCESS | 2026-01-01T00:10:00Z | Refined requirements. | Research.md | - |
| 3 | requirements-review#3 | RESEARCH | - | SUCCESS | 2026-01-01T00:15:00Z | Requirements review passed. | Requirements.md | - |
| 4 | planner-tdd-soft#4 | PLANNING | - | SUCCESS | 2026-01-01T00:20:00Z | Created 2-stage plan. | Research.md, Requirements.md | - |
| 5 | plan-review#5 | PLANNING | - | SUCCESS | 2026-01-01T00:25:00Z | Plan review passed. | Plan.md, Stage-1/Plan.md | - |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
|----------|------------|------------|
| Research.md | RESEARCH | codebase-research#1 |
| Requirements.md | RESEARCH | requirements-refinement#2 |
| requirements-review.md | RESEARCH | requirements-review#3 |
| Plan.md | PLANNING | planner-tdd-soft#4 |
| Stage-1/Plan.md | PLANNING | planner-tdd-soft#4 |
| Stage-1/PlanProgress.md | PLANNING | planner-tdd-soft#4 |
| Stage-2/Plan.md | PLANNING | planner-tdd-soft#4 |
| Stage-2/PlanProgress.md | PLANNING | planner-tdd-soft#4 |
| plan-review.md | PLANNING | plan-review#5 |
</Artifacts>

<WorkflowNotes type="core">
| Seq | Note |
|-----|------|
</WorkflowNotes>
```

### 5b. Assign a Numeric ID

Before writing the test definition, check the repository for the highest numeric `id` already in use across all `.test.yaml` files and use the next available integer. Numeric IDs must be unique across the entire repository, not just within your suite.

```bash
grep -r "^id:" Tools/AgentTest/tests/ --include="*.test.yaml"
```

Pick the next unused integer. Once assigned, never reuse a numeric ID even if the test is deleted — numeric IDs are stable identifiers for the results storage system.

### 6. Write the Test Definition

Create `my-routing-tests/findings-reroute.test.yaml`:

```yaml
schema_version: 1
name: findings-reroute
id: 25
version: 1
changelog:
  - version: 1
    date: "2026-01-15"
    changes: "Initial version"
description: >
  COMPLETED_NEEDS_ACTION from requirements-review should route back to
  requirements-refinement per the On Findings column, not forward to
  planner-tdd-soft.
layer: orchestrator
negative: false

subject:
  identity: orchestrator
  agent: orchestrator
  workflows: [brownfield-tdd]
  infrastructure_agents: []
  opening_message: |
    Task: Add rate limiting to the API gateway in src/gateway/
    Workflow: brownfield-tdd
    Checkpoints: disabled
  invocation_kind: orchestrator
  model: sonnet
  allowed_tools: [Task, Read, Write, Edit]

stub_registry: findings-reroute.stubs.json

timeout: 10m
turn_limit: 30
stop_after_invocations: 4

assertions:
  invocation_sequence:
    exact: true
    steps:
      - { tool: dispatch, agent: codebase-research }
      - { tool: dispatch, agent: requirements-refinement }
      - { tool: dispatch, agent: requirements-review }
      - { tool: dispatch, agent: requirements-refinement }   # routed back per On Findings
```

### 7. Write the Suite File

Create `my-routing-tests/my-routing-tests.suite.yaml`:

```yaml
schema_version: 1
id: my-routing-tests
description: >
  Tests for status-code-driven routing decisions in brownfield-tdd workflow.
defaults:
  timeout: 10m
  turn_limit: 30
  repetitions: 1
  pass_rate: 1.0
tests:
  - path: findings-reroute.test.yaml
```

### 8. Run the Suite

```bash
mosaic-agent-test --harness claude-code --suite tests/my-routing-tests/my-routing-tests.suite.yaml
```

---

## Common Patterns

### Testing a Routing Fork (On Success vs On Findings)

The most common test scenario: verify that the orchestrator routes correctly based on a subagent's status code. The workflow table has two routing columns — **On Success** and **On Findings** — and the orchestrator must follow the right one.

Create two tests from the same workflow table row:
- **Test A:** Stub returns `SUCCESS` → assert the orchestrator follows On Success
- **Test B:** Stub returns `COMPLETED_NEEDS_ACTION` → assert the orchestrator follows On Findings

Example for `requirements-review` in `brownfield-tdd` (On Success = `planner-tdd-soft`, On Findings = `requirements-refinement`):

```yaml
# Test A: requirements-review SUCCESS → planner-tdd-soft
assertions:
  invocation_sequence:
    exact: true
    steps:
      - { tool: dispatch, agent: codebase-research }
      - { tool: dispatch, agent: requirements-refinement }
      - { tool: dispatch, agent: requirements-review }
      - { tool: dispatch, agent: planner-tdd-soft }

# Test B: requirements-review COMPLETED_NEEDS_ACTION → requirements-refinement (callback)
assertions:
  invocation_sequence:
    exact: true
    steps:
      - { tool: dispatch, agent: codebase-research }
      - { tool: dispatch, agent: requirements-refinement }
      - { tool: dispatch, agent: requirements-review }
      - { tool: dispatch, agent: requirements-refinement }  # routed back per On Findings
```

### Testing Mid-Workflow Decisions

Seed an orchestration document via `seed_files` with a `$ref` fixture that places the orchestrator partway through a workflow. The orchestrator reads `current_state` from the frontmatter and the Execution Log, then resumes from that point. This avoids running the entire workflow just to reach the decision point you care about.

```yaml
seed_files:
  - path: Orchestration-{run_id}/Orchestration.md
    ref: after-planning.md
  # Also seed the artifacts the orchestrator expects to find:
  - path: Orchestration-{run_id}/Plan.md
    ref: plan.md
  - path: Orchestration-{run_id}/Research.md
    ref: research.md
  - path: Orchestration-{run_id}/Requirements.md
    ref: requirements.md
```

Important: when seeding mid-workflow, also seed any artifacts the orchestrator would read to make routing decisions (e.g. Plan.md for stage ordering during EXECUTION phase, Research.md and Requirements.md if those inform later dispatches).

### Testing Parallel Dispatch

Some workflows have rows where multiple subagents are eligible simultaneously (e.g. the `greenfield-tdd` workflow fans out to `researcher` and `library-researcher` in the RESEARCH phase). Brownfield-tdd does not use parallel dispatch, but the pattern applies to any workflow that does. Declare a `parallel_groups` entry, include the group in `invocation_sequence` steps, and use `min_concurrency` to assert the dispatches actually ran concurrently:

```yaml
parallel_groups:
  - name: research-fanout
    members:
      - { tool: dispatch, agent: researcher }
      - { tool: dispatch, agent: library-researcher }

assertions:
  invocation_sequence:
    exact: true
    steps:
      - { tool: dispatch, agent: requirements-refinement }
      - group: research-fanout
        members:
          - { tool: dispatch, agent: researcher }
          - { tool: dispatch, agent: library-researcher }
      - { tool: dispatch, agent: planner-tdd }
  min_concurrency:
    research-fanout: 2
```

### Negative Tests

Use `negative: true` when you want to assert the orchestrator does *not* make a particular routing decision. Example: after `implementation-tdd` returns `SUCCESS`, the orchestrator should NOT re-dispatch `implementation-tdd` — it should advance to `implementation-review`:

```yaml
negative: true
assertions:
  invocation_sequence:
    exact: false    # subsequence match
    steps:
      - { tool: dispatch, agent: implementation-tdd }
      - { tool: dispatch, agent: implementation-tdd }   # should NOT happen
```

This test passes if the invocation sequence assertion *fails* — i.e., the orchestrator did not re-dispatch `implementation-tdd` after SUCCESS.

### Statistical Testing (Repetitions)

LLM orchestrators are non-deterministic. For routing decisions where you want to measure reliability:

```yaml
# In the suite file
tests:
  - path: findings-reroute.test.yaml
    repetitions: 10
    pass_rate: 0.8    # 80% of runs must route correctly
```

### Testing the Creator/Reviewer Quality Gate

A key orchestrator invariant: after a reviewer returns `COMPLETED_NEEDS_ACTION` and the creator fixes, the **reviewer must re-validate** before the gate opens. Test this by stubbing the loop. Example using the `requirements-review` → `requirements-refinement` gate in brownfield-tdd:

```json
{
  "stubs": [
    {
      "match": { "tool": "dispatch", "agent": "requirements-review", "invocation": 1 },
      "response": {
        "agent_instance_id": "requirements-review#3",
        "run_id": "test-run",
        "status_code": "COMPLETED_NEEDS_ACTION",
        "status_message": "Requirements lack acceptance criteria for edge cases."
      }
    },
    {
      "match": { "tool": "dispatch", "agent": "requirements-refinement", "invocation": 2 },
      "response": {
        "agent_instance_id": "requirements-refinement#4",
        "run_id": "test-run",
        "status_code": "SUCCESS",
        "status_message": "Requirements updated with edge case acceptance criteria."
      }
    },
    {
      "match": { "tool": "dispatch", "agent": "requirements-review", "invocation": 2 },
      "response": {
        "agent_instance_id": "requirements-review#5",
        "run_id": "test-run",
        "status_code": "SUCCESS",
        "status_message": "Requirements review passed."
      }
    }
  ]
}
```

Assert the orchestrator sends the creator's fix back through the reviewer:

```yaml
assertions:
  invocation_sequence:
    exact: false
    steps:
      - { tool: dispatch, agent: requirements-review }       # reviewer finds issues
      - { tool: dispatch, agent: requirements-refinement }   # creator fixes
      - { tool: dispatch, agent: requirements-review }       # reviewer re-validates
```

### Testing BLOCKED and Error Handling

Test the orchestrator's tiered error handling by stubbing a `BLOCKED` response with an error code. In brownfield-tdd, `implementation-tdd` runs during EXECUTION stages:

```json
{
  "match": { "tool": "dispatch", "agent": "implementation-tdd", "invocation": 1 },
  "response": {
    "agent_instance_id": "implementation-tdd#8",
    "run_id": "test-run",
    "status_code": "BLOCKED",
    "status_message": "Cannot access the database server.",
    "error_code": "E501",
    "error_reason": "Database connection refused on port 5432"
  }
}
```

The orchestrator should apply Tier 1 (auto-retry for E501), so you would expect a second dispatch to the same agent:

```yaml
assertions:
  invocation_sequence:
    exact: false
    steps:
      - { tool: dispatch, agent: implementation-tdd }   # first attempt, BLOCKED
      - { tool: dispatch, agent: implementation-tdd }   # auto-retry
```

### Testing Multiple Invocations of the Same Agent

Use the `invocation` ordinal in the stub registry to return different responses for successive dispatches of the same agent. In brownfield-tdd, `implementation-tdd` is dispatched once per stage — a 2-stage plan means two invocations:

```json
{
  "stubs": [
    {
      "match": { "tool": "dispatch", "agent": "implementation-tdd", "invocation": 1 },
      "response": {
        "agent_instance_id": "implementation-tdd#8",
        "run_id": "test-run",
        "status_code": "SUCCESS",
        "status_message": "Stage 1 implementation complete."
      }
    },
    {
      "match": { "tool": "dispatch", "agent": "implementation-tdd", "invocation": 2 },
      "response": {
        "agent_instance_id": "implementation-tdd#12",
        "run_id": "test-run",
        "status_code": "SUCCESS",
        "status_message": "Stage 2 implementation complete."
      }
    }
  ]
}
```

---

## Diagnostic Errors You May See

The parser validates thoroughly and reports every problem it finds (it does not stop at the first).

| Code | Meaning | Fix |
|------|---------|-----|
| `missing-required-field` | A required field is absent. For test definitions: `name` (display name), `id` (numeric), `version`, or `changelog`. For stub registries: `test_id`. For suites: `id` | Add the missing field |
| `non-positive-id` | The numeric `id` field is zero or negative | Set `id` to a positive integer unique across all test definitions in the repository |
| `non-positive-version` | The `version` field is zero or negative | Set `version` to a positive integer; new tests start at `1` |
| `missing-changelog-match` | No `changelog` entry has a `version` that matches the top-level `version` | Add a changelog entry whose `version` field matches the top-level `version` |
| `duplicate-numeric-id` | Two test definitions in the same suite run share the same numeric `id` | Each test definition must have a unique numeric `id` across the entire repository |
| `unknown-field` | A top-level key is not recognized | Check for typos. The format is strict — only documented fields are allowed |
| `malformed-document` | YAML/JSON parse failure | Fix syntax |
| `malformed-field` | A field value is wrong type/format (e.g. `timeout: "not-a-duration"`) | Fix the value |
| `removed-key-harness` | The `harness` key was found in a suite, suite entry, or test definition | Remove it. Harness selection is always external (`--harness` flag) |
| `missing-generic-response` | `on_unmatched` is `"generic-response"` but no `generic_response` was provided | Add a `generic_response` payload to the stubs file |

---

## Three-Way Coupling: Workflow, Stubs, and Stub Agents

A working test requires consistency across three things:

1. **Workflow routing table** — names agents the orchestrator dispatches. The `Subagent` column in the workflow table (e.g. `codebase-research`, `requirements-refinement`, `requirements-review`, `planner-tdd-soft`, `plan-review`, etc. in `brownfield-tdd`) determines which agents the orchestrator will try to invoke
2. **Stub registry** (`.stubs.json`) — declares what those agents return when intercepted. Every agent in the workflow table that the test will reach needs a stub entry, matched by `(tool, agent, invocation)`
3. **Stub agent definitions** (`agents/*.md`) — placeholder files that make each dispatch legal from the harness's perspective

If an agent appears in the workflow but has no stub entry, and `on_unmatched` is `"halt"`, the run stops with an unmatched invocation. If an agent appears in the workflow but has no stub agent definition, the deploy tool's validation catches it during preflight.

Current state: this three-way consistency is maintained manually by the test author. Preflight validates the deploy side (agent file exists, workflow ID resolves) but does not cross-check that every workflow agent has a matching stub entry. A missing stub entry surfaces at runtime as a well-defined error, not a silent failure.

**Practical tip:** Start from the workflow table. List every agent in the `Subagent` column that your test will reach (following On Success / On Findings from the starting point). Each one needs: (a) a stub entry with the right `match.agent`, (b) a stub agent `.md` file, (c) a `stub_agents` entry in the test definition pointing to that file.

---

## Existing Examples

| Suite | Location | Purpose |
|-------|----------|---------|
| `examples/` | `Tools/AgentTest/examples/` | Documentation-grade examples exercised by Go e2e tests. Uses the scripted `fake` harness — no LLM cost. Start here to understand the format |
| `smoke/` | `Tools/AgentTest/tests/smoke/` | Real-harness smoke tests against Claude Code. Proves the tool's plumbing works end-to-end. Costs money |
| `testdata/examples/` | `Tools/AgentTest/testdata/examples/` | Parser test fixtures. Shows every schema feature but may diverge from the runtime examples |
