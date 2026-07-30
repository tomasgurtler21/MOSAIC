# MOSAIC Log Format

> **Status:** Draft for review
> **Created:** 2026-07-27
> **Last Updated:** 2026-07-27
> **Scope:** The canonical log event schema, the on-disk artifact layout, run identity, and the merge utility's contract. 

---

## 1. Purpose

This document defines a canonical, harness-agnostic log format for MOSAIC orchestration: run and session lifecycle, every subagent invocation, every conversation turn (orchestrator and subagent, user and assistant), subagent-internal tool calls, notifications, and context compaction — together with the on-disk layout that carries them and a merge utility that flattens a run into one chronological stream.

Logging is an optional, parallel observer of orchestration: it observes from the outside and is never a step within orchestration itself. Nothing in the core runtime or the script runner may depend on logs existing. Consumers — a future cost analyzer and test-evaluation tooling — must tolerate partial or absent logs, including entire event types being absent for a given harness.

This document is the complete, self-contained specification for this log format. It is the binding reference any per-harness hook adapter implements against.

## 2. Design Principles

- **Runtime safety outranks consumer convenience.** Where the two conflict, favor the orchestration runtime (critical, core) over log consumers (optional, non-core tooling). This principle drives the per-invocation JSONL scoping (§4) and the merge utility's offline enrichment (§7).
- **Self-contained logs.** The canonical event stream carries full conversation content, so a consumer reconstructs an entire run from the JSONL alone, never falling back to a harness transcript. Raw transcript exports remain an independent verification path, not the primary record.
- **Fields degrade, never fabricate.** Every optional field is populated only when the adapter can actually supply it; when it can't, the field is omitted entirely — never null, zero, or a placeholder value. The same applies at the event-type level: an entire event type may be absent for a harness that can't supply it.
- **No cost field.** Token counts are the canonical unit; cost derivation is a downstream consumer's job, not part of this schema.
- **No dedicated error event type.** A failed or aborted invocation is still an `invocation_end` event whose `status_code` reflects the failure when determinable. `run_end`/`session_end` carry an outcome/reason field rather than spawning error-specific event types.
- **Prefer harness-native identifiers over adapter-invented state.** Wherever a harness or its API already hands the adapter a usable identifier (a tool-call id, a message index), the adapter uses it rather than minting and tracking its own. For `run_id`, the authoritative value is the one the script runner authored and embedded in dispatch content — the adapter extracts it rather than inventing its own. What's avoided is *high-frequency* state: a counter that would need a write on every turn or every tool call. That's the line — not "no local state," but "no state whose write frequency scales with conversation activity."
- **Derive, don't track, anything computable from paired timestamps or file order.** `timestamp` is mandatory on every event, and every start/end pair for a correlation key lives in the same file (§3.5). Durations and ordinal indices are therefore always reconstructable by any reader of that file — they are not part of the wire schema at all, and adapters never carry state purely to compute one.

## 3. Canonical Log Event Schema

The machine-readable format is JSONL: one JSON object per event per line, append-only, strictly one-JSON-object-per-line at all times even though individual lines carry full message text and can be large.

**What `Required`/`Optional` mean in every field table below:** `Required` — always present on that event type. `Optional` — present with a real value when the producer can supply or resolve one; when it can't, **the key is absent from the JSON object entirely** — never present with `null`, never a placeholder or zero value (§2, "Fields degrade, never fabricate"). This applies identically whether the producer is a hook adapter writing the raw per-file JSONL or the merge utility computing an injected field (§7) — `Optional` there means "present whenever honestly resolvable," not "rarely present." A merge-injected field that's `Optional` isn't a weaker guarantee than `Required` would be for clean input — it's the only honest option once genuinely unresolvable input (a crashed process, a truncated log, an ambiguous fallback-ID collision) is something the schema has to tolerate rather than paper over.

### 3.1 Event catalog

| Event | Purpose |
|---|---|
| `run_start` | An orchestration run begins. |
| `run_end` | An orchestration run ends (successfully or otherwise). |
| `session_start` | A harness session begins or is resumed. Distinct from a run: a session may span several runs, and a run may resume an existing session. |
| `session_end` | A harness session ends. |
| `invocation_start` | A subagent invocation begins. |
| `invocation_end` | A subagent invocation ends. |
| `turn` | One conversation turn — a user or assistant message — for either the orchestrator or a subagent. |
| `tool_call_start` | The orchestrator or a subagent begins a tool call. |
| `tool_call_end` | A tool call completes (successfully or with an error). |
| `notification` | The harness asked for human input: a permission prompt, an idle notification, or similar. |
| `compaction` | A context-window compaction or equivalent context-management event occurred. |

No other event types exist in schema version 1. A human prompt to the orchestrator is modeled as a `turn` event with `role: "user"`, not a distinct event type — a human prompt is simply a conversation turn.

`tool_call` is deliberately two events, not one. A single post-hoc event would assume tool calls are synchronous and non-overlapping, which isn't true — parallel and background tool execution is a real case this schema has to survive (it's the same reason the invocation JSONL is scoped per-invocation rather than shared: concurrent execution is common, not an edge case). Splitting into start/end mirrors `invocation_start`/`invocation_end` and matches how harnesses natively hook this (paired before/after events), rather than forcing the adapter to buffer a call's state until it resolves.

### 3.2 Coverage and degradation

- `invocation_start` and `invocation_end` are **mandatory** for every harness adapter — an adapter that cannot emit these is not a valid adapter.
- All other event types are emitted only where the harness can actually supply them. An entire event type may be absent for a given harness.
- Consumers must tolerate an entire event type being absent from a log, exactly as they tolerate an absent field.
- The schema's field set and event set are defined from MOSAIC's own needs, not limited to the intersection of what every harness happens to support.

### 3.3 Common envelope (all events)

| Field | Type | Required | Notes |
|---|---|---|---|
| `schema_version` | string | Required | See §6. |
| `event` | enum (§3.1) | Required | Discriminator field. |
| `timestamp` | string (ISO 8601) | Required | Time the adapter captured/emitted the event. |
| `harness` | enum: `"claude-code"` \| `"opencode"` \| `"vscode-ghcp"` | Required | Which harness adapter emitted this entry. |
| `session_id` | string | Optional | Harness-native session identifier, normalized to this single field name regardless of the harness's native casing. |
| `run_id` | string | Optional | Identifier of the orchestration run, when determinable. Primary correlation key for the merge utility — see §4. |

`actor` and source provenance are deliberately **not** part of the per-file envelope — see §7.

### 3.4 Per-event field blocks

Event-specific fields are declared as named, typed fields, not an untyped free-form blob — a free-form `data` object would stop the schema being a contract, and a flat all-optional field set would make it impossible to distinguish "not applicable to this event type" from "the harness couldn't supply it."

**`run_start`**

| Field | Type | Required | Notes |
|---|---|---|---|
| `run_id` | string | Required | Overrides the envelope's optional status — a run event must identify its run. |
| `cwd` | string | Optional | Working directory the run started in. |
| `model` | string | Optional | Orchestrator model identifier. |
| `adapter_version` | string | Optional | Version of the hook bundle that emitted this run's logs. There is no single "MOSAIC version" to report — agents, skills, hooks, and workflows are each versioned independently — so this names the one component that's actually meaningful here: the adapter itself. Sourced at runtime by reading the `version` field out of the hook bundle's own `hook.yaml`, which is deployed alongside the adapter scripts as an ordinary bundle file (no deploy-time content substitution, unlike the mechanism used to stamp agent files with their own version). This keeps the reported value permanently in sync with `hook.yaml` without requiring any change to the deployment tool. |

**`run_end`**

| Field | Type | Required | Notes |
|---|---|---|---|
| `run_id` | string | Required | |
| `outcome` | string | Optional | Terminal outcome/reason (completed, aborted, interrupted, limit reached). Failures are expressed here, never as a distinct error event. |

No `duration_ms` field — see §3.5.

**`session_start`**

| Field | Type | Required | Notes |
|---|---|---|---|
| `session_id` | string | Required | Overrides the envelope's optional status. |
| `resumed` | boolean | Optional | Whether this is a fresh session or a resume of an existing one. |
| `cwd` | string | Optional | |
| `model` | string | Optional | |

**`session_end`**

| Field | Type | Required | Notes |
|---|---|---|---|
| `session_id` | string | Required | |
| `reason` | string | Optional | Why the session ended. |

No `duration_ms` field — see §3.5.

**`invocation_start`**

| Field | Type | Required | Notes |
|---|---|---|---|
| `agent_instance_id` | string | Required | `{AgentName}#{Number}` form, e.g. `"RequirementsRefinement#2"`, matching the communication protocol's instance-ID convention. In the normal case the adapter doesn't mint or count this at all: the orchestrator already assigns it when dispatching the subagent, per the existing communication protocol, and it's present in the dispatch content (prompt/tool-call arguments) the adapter observes — the adapter's job is to extract it, not generate it. Only if the harness can't determine the full form does the adapter fall back to the same `{prefix}_{timestamp}-{4-char random suffix}` pattern as `run_id` (§4.2) rather than omitting the field — never a locally-tracked sequence counter, which would need a write on every invocation. The random suffix (not just a timestamp) matters here: see the `call_id` fallback note below for why timestamp-only fallbacks aren't safe enough once same-type parallelism is common. The value itself doesn't need to be the correlation mechanism between start and end — that pairing is handled separately (whatever harness-native session/agent identifier the adapter already has), so a lower-effort ID here costs nothing. |
| `agent_type` | string | Optional | Bare agent name (e.g., `"RequirementsRefinement"`). May be unreliable on some harnesses; adapters must not attempt to work around known harness bugs here, and consumers must tolerate incorrect values. |
| `prompt` | string | Optional | Full invocation prompt text sent to the subagent. |

**`invocation_end`**

| Field | Type | Required | Notes |
|---|---|---|---|
| `agent_instance_id` | string | Required | Correlation key back to `invocation_start`. |
| `status_code` | enum: `SUCCESS`, `COMPLETED_NEEDS_ACTION`, `PARTIALLY_DONE`, `NEEDS_CLARIFICATION`, `CAPABILITY_EXCEEDED`, `BLOCKED` | Optional | Populated when the adapter can determine the subagent's returned status; absent if the harness gives no access to response content. A failed invocation is reported here, never as a separate error event. |
| `response` | string | Optional | Full final response text from the subagent. |
| `model` | string | Optional | Model identifier used for the invocation. |
| `token_usage` | object | Optional | Present only if at least one sub-field is available. Sub-fields, each independently optional: `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_creation_tokens` (all numbers). |

No `duration_ms` field — see §3.5.

**`turn`**

| Field | Type | Required | Notes |
|---|---|---|---|
| `role` | enum: `"user"` \| `"assistant"` | Required | Discriminates a human/orchestrator-issued turn from a model-produced turn. Covers both orchestrator and subagent conversations; actor is implicit from file location (§7). |
| `content` | string | Required | Full message text, not truncated. |
| `model` | string | Optional | Model that produced an assistant turn. |
| `token_usage` | object | Optional | Same sub-field structure as `invocation_end.token_usage`. Enables per-turn token accounting for both orchestrator and subagent conversations. |

No `turn_index` field — see §3.5.

**`tool_call_start`**

| Field | Type | Required | Notes |
|---|---|---|---|
| `call_id` | string | Required | Correlation key to the matching `tool_call_end`. Prefer the harness/API-native call identifier when one exists (e.g. a `tool_use` block's own `id`) over minting one — see the harness-native-identifiers principle in §2. If the harness genuinely exposes no such id, fall back to `{tool_name}_{timestamp}-{4-char random suffix}` — **not** a bare timestamp. A timestamp-only fallback (as originally used for `agent_instance_id`) is *not* safe enough here: the highest-probability failure case for `call_id` isn't two unrelated calls colliding by chance, it's several calls **to the same tool**, dispatched **together** — e.g. an agent issuing five parallel `Read` calls in one turn — which is close to the common case for parallel tool use, not a rare edge case. Same `tool_name` and the same dispatch batch makes a millisecond-level timestamp collision plausible, not just theoretically possible. The random suffix is what actually closes this gap, at zero added state (still one random draw per event, no counter). |
| `tool_name` | string | Required | Name of the tool invoked. |
| `tool_input` | object \| string | Optional | Arguments passed to the tool. Capture policy: §5. |

No `call_index` field — see §3.5.

**`tool_call_end`**

| Field | Type | Required | Notes |
|---|---|---|---|
| `call_id` | string | Required | Correlation key back to `tool_call_start`. |
| `status` | enum: `"success"` \| `"error"` | Optional | Outcome of the call. Tool failures are reported here, never as a separate error event. |
| `error` | string | Optional | Error detail when `status` is `"error"`. |
| `tool_output` | object \| string | Optional | Result returned by the tool. Capture policy: §5. |

No `duration_ms` field — see §3.5.

Per-call granularity (one event pair per call, not aggregated counts) was chosen because it supports analysis counts alone can't — ordering, timing, failure patterns — and aggregate counts are trivially derivable from per-call records while the reverse is not. The start/end split additionally makes overlapping and out-of-order completion (parallel or backgrounded tool calls) representable at all, which a single post-hoc event cannot express.

**`notification`**

| Field | Type | Required | Notes |
|---|---|---|---|
| `notification_type` | string | Required | Kind of notification (e.g. permission request, idle prompt). |
| `message` | string | Optional | Text shown to the human. |
| `requires_response` | boolean | Optional | Whether the run is blocked awaiting human input. |
| `resolution` | string | Optional | How it was resolved (approved, denied, timed out), when the harness reports it. |
| `notification_id` | string | Optional | Correlation key across two `notification` events representing the same prompt, for harnesses that expose ask and resolution as separate hooks. Absent when a harness reports the notification as a single event (ask+resolution together, or ask-only with no resolution visibility) — in that case one `notification` event is already complete on its own. |

Harnesses vary in whether a human-input prompt is observable as one event or two, and the schema doesn't force a single model: a harness with single-shot visibility emits one self-contained `notification` event with whatever of `message`/`requires_response`/`resolution` it can populate; a harness with distinct ask/resolve hooks emits two events sharing the same `notification_id` — the first with `requires_response: true` and no `resolution`, the second carrying `resolution` once known. Consumers must not assume exactly one `notification` event per human prompt; they pair by `notification_id` when it's present and treat an event without one as already complete.

**`compaction`**

| Field | Type | Required | Notes |
|---|---|---|---|
| `trigger` | enum: `"auto"` \| `"manual"` | Optional | What caused the compaction. |
| `tokens_before` | number | Optional | Context size prior to compaction. |
| `tokens_after` | number | Optional | Context size after compaction. |

### 3.5 Derived fields (not in the wire schema)

`duration_ms` (on `run_end`, `session_end`, `invocation_end`, `tool_call_end`), `turn_index`, and `call_index` were in earlier drafts of this schema as adapter-populated optional fields. They're deliberately absent from the wire format instead, because "optional, populate when the harness supplies it" turned out to describe a path that never actually exists:

- No harness has any concept of "orchestration run," so `run_end.duration_ms` could never have a genuine native source — a harness handing over "run duration" would actually be reporting something else mislabeled as one.
- No documented hook payload on any of the three harnesses includes a duration field at all, so `invocation_end.duration_ms` and `tool_call_end.duration_ms` have no realistic native source either.
- Populating `turn_index`/`call_index` natively would require an adapter-maintained counter surviving across process invocations — exactly the high-frequency state the harness-native-identifiers principle (§2) rules out.

An "optional" field that structurally can never be populated isn't a real degradation case, it's dead weight in the contract. Since every start/end pair for a correlation key (`run_id`, `session_id`, `agent_instance_id`, `call_id`) is always written to the same file — run/session lifecycle events and the orchestrator's own turns/tool calls to `00_orchestrator_events.jsonl` (§4.3), everything else to the relevant invocation's `03_events.jsonl` — any reader of that one file can trivially compute:

- **Duration:** `end.timestamp - start.timestamp` for the matching pair.
- **`turn_index` / `call_index`:** ordinal position among same-type events for the same actor, in file order (file order is already temporal order within one file).

This doesn't require cross-file assembly — pairing needs only the one file both events already live in, so nothing about producing or reading the raw per-file logs depends on the merge utility. But the merge utility computes and injects these into its output anyway (§7) rather than leaving every consumer to reimplement the same pairing logic: it's the same rationale that justifies the merge utility existing at all — solve discovery/ordering/pairing once, centrally, instead of N times across the cost analyzer, test evaluation, and ad-hoc tooling. The cost is confined to the merged output (an offline, regeneratable artifact) and is small — a few bytes per applicable line, negligible next to the full-message-text volume already accepted in §5/§8 — so nothing is gained by leaving it undone at merge time.

## 4. Run Identity and On-Disk Layout

### 4.1 Why runs need their own identity

Invocation events are scoped per invocation, not written to a shared run-wide file at runtime — this keeps the orchestration runtime safe under parallel subagent execution (the orchestrator can spawn several subagents within a single inference response, and their hook processes can fire near-simultaneously; no log file may be written by more than one in-flight invocation). `run_id` is the primary correlation key the merge utility uses to reassemble a run from its scattered per-invocation files.

The orchestrator's own events are the deliberate exception to per-invocation scoping, not a contradiction of it. There is exactly one orchestrator per run — no concurrent-writer risk exists for its own turns, tool calls, or the run/session lifecycle events, because nothing else is writing on its behalf at the same time the way multiple parallel subagents write to their own separate invocation files. That's why the orchestrator's `turn`/`tool_call_*`/`notification`/`compaction` events, alongside `run_start`/`run_end`/`session_start`/`session_end`, share one small run-root file (`00_orchestrator_events.jsonl`, §4.3) instead of each needing invocation-style isolation.

That makes `run_id` load-bearing for storage, not just for correlation: artifacts from different runs must never collide on disk. `agent_instance_id` values (`Research#1`, `RequirementsRefinement#2`, …) are scoped to a single run's own sequence and reset to `#1` at the start of every new run. That sequence isn't adapter-invented state — it's the orchestrator's own per-type dispatch numbering, already part of the existing communication protocol and already present in the dispatch content the adapter observes, not something the logging system mints or tracks. So a flat, unscoped directory would let a second run's invocation folders silently collide with or overwrite a first run's, with no warning and no recovery. Every invocation folder and the orchestrator transcript therefore live under a per-run root directory.

**Why not key storage on `session_id` instead of minting a separate `run_id`?** The schema already distinguishes the two on purpose: a session may span several runs, and a run may resume an existing session. In the common case they coincide, but the schema's own model allows them to diverge, and an adapter has no reliable way to know in advance which case it's in. Keying storage on `session_id` would be correct today and silently wrong the moment a session is resumed for a second, unrelated run. `run_id` is the concept the rest of the schema already commits to for correlation; storage should commit to the same one.

### 4.2 Adopting `run_id`

An adapter cannot ask the orchestrator "what run is this" — it is an external observer with no access to orchestrator-internal state. The adapter resolves `run_id` by **extracting it from the dispatch content** of the events it observes, rather than minting or tracking one itself.

- **Format:** `{YYYYMMDD}T{HHMMSS}Z-{4-char-hex-suffix}`, e.g. `20260727T170000Z-a3f9`. Sortable by creation time, collision-resistant, human-scannable in a directory listing. The minting authority is the script runner (or orchestrator), which generates `run_id` once at artifact creation; the adapter merely reads it from the payloads it intercepts.
- **Extraction rule:** the adapter searches each incoming event payload for a value matching the `run_id` format (`\d{8}T\d{6}Z-[0-9a-f]{4}`). It first looks for a structured key-value match (`run_id: ...` or `"run_id": "..."`); if that fails, it falls back to the earliest bare occurrence of the format pattern in the same payload. Matching stops at the first confident hit. When no match is found, `run_id` is treated as absent for that event.
- **Per-event extraction sources:** extraction is applied to the prompt-bearing field of the event where one is available. For events with no prompt-bearing field (e.g. `SessionStart`, `SessionEnd`, `PreToolUse`, `PostToolUse`, `PreCompact`, `PostCompact`), extraction yields no result.
- **`run_start`/`run_end` triggering:** `run_start` is emitted on `SessionStart`; `run_end` is emitted on `SessionEnd`. These are emitted regardless of whether extraction succeeds — when `run_id` is absent, the events are routed to the `unknown-run/` degradation bucket (§4.3).

**`unknown-run/` degradation bucket.** When extraction fails for an event — because the event has no prompt-bearing field, or because the payload contains no recognizable `run_id` value — the adapter uses the literal folder name `unknown-run` in place of a real `run_id` for that event's storage path. This means the event is still logged (nothing is silently dropped), and a consumer examining the log directory can inspect `unknown-run/` to investigate events that arrived with no discernible run context. Events from concurrent sessions where extraction consistently fails may interleave within `unknown-run/` — this is an accepted limitation of extraction-based identity.

**No marker files.** The previous marker-file mechanism (`OrchestrationLogs/.run-markers/{session_id}.json`) is removed. The adapter no longer mints `run_id`, writes marker files, or consults staleness timeouts. Run identity is authored by the script runner and propagated through dispatch content; the adapter's job is solely to extract it.

### 4.3 Directory layout

```
OrchestrationLogs/
├── {run_id}/
│   ├── 00_orchestrator_session.raw           # orchestrator transcript export, §4.4, atomic replace
│   ├── 00_orchestrator_session.meta.json     # sidecar: harness, native format, source, capture timestamp
│   ├── 00_orchestrator_events.jsonl          # run/session lifecycle + orchestrator's own turn/tool_call/notification/compaction events, §4.1, §3.5
│   └── {agent_instance_id}/                  # one folder per invocation, e.g. "RequirementsRefinement#2"
│       ├── 01_input.md                       # input artifact: metadata table + raw prompt, §4.5
│       ├── 02_output.md                      # output artifact: metadata table + raw response, §4.5
│       ├── 03_events.jsonl                   # canonical event stream for this invocation, §3
│       ├── 04_session.raw                    # full session transcript export, raw byte-for-byte, §4.5
│       └── 04_session.meta.json              # sidecar for the above
├── {run_id}/...                              # sibling runs, never colliding
└── unknown-run/                              # degradation bucket: events whose run_id could not be extracted (§4.2)
    └── {agent_instance_id}/                  # same internal structure as a real run folder
        └── ...
```

`{agent_instance_id}` folder names are sanitized for filesystem safety (`<>:"/\|?*` replaced with underscores). Numbering (`00_`, `01_`, …) exists purely for human browsability when sorted in a file explorer and carries no machine meaning. `unknown-run/` follows the same internal structure as a real run folder — per-invocation `{agent_instance_id}/` subdirectories with the same file set — so tooling that processes `{run_id}/` directories can process `unknown-run/` with the same code path.

### 4.4 Orchestrator-level transcript and events

Two run-root files carry the orchestrator's own record, distinct in purpose:

- **`00_orchestrator_session.raw`** (+ `.meta.json` sidecar) is the raw transcript export — the verification path, per §4.5's raw-export policy. It's refreshed on subagent lifecycle events so it stays current, which is the one deliberate exception to "no file written by more than one in-flight invocation": concurrent hook processes from parallel subagent spawns may refresh it near-simultaneously. Writes are an **atomic replace**: write to a temp file, then rename over the target. Under contention this can only lose a refresh, which the next subagent event restores, and can never leave a torn or half-written file.
- **`00_orchestrator_events.jsonl`** is the canonical, typed event stream for everything scoped to the orchestrator rather than to any one invocation: `run_start`/`run_end`, `session_start`/`session_end`, and the orchestrator's own `turn`/`tool_call_start`/`tool_call_end`/`notification`/`compaction` events (§3.5, §4.1). Unlike the transcript above, this file is append-only, matching the discipline of the per-invocation `03_events.jsonl` files — there's no equivalent overwrite risk here since the orchestrator is the file's sole writer.

### 4.5 Human-readable and verification artifacts

- **Input/output artifacts** (`01_input.md`, `02_output.md`) exist for human browsing, not for tooling — tooling reads the JSONL, which already carries the same content. Each is a metadata table (agent instance ID, session ID, timestamps, and other envelope-equivalent fields available at that point) plus the raw prompt or response content.
- **Session transcript export** (`04_session.raw`) is the subagent's complete session history, sourced from whatever transcript/export mechanism the harness provides, stored **raw — byte-for-byte** with no parsing, re-rendering, or reformatting. Its role is an independent verification path: a cross-check that the JSONL capture is complete, not the primary record.
- **Sidecar metadata** (`*.meta.json`) accompanies every raw transcript export, recording at minimum the emitting harness, the native format of the export, the source path/mechanism it came from, and the capture timestamp — since each harness exports a different, otherwise format-ambiguous native format.

## 5. Tool Payload Capture Policy

`tool_call_start.tool_input` and `tool_call_end.tool_output` are, in the abstract, unbounded — a single file read could dwarf an entire conversation.

**Decision: full capture, no artificial size cap.** In practice, tool output is not unbounded: a tool result becomes part of the calling agent's own context, so if it didn't fit, the harness itself would already have truncated or rejected it before an adapter ever saw it. `tool_output` is therefore bounded by the same thing that already bounds the `turn` events' full-content policy — the invocation's own context window, which for MOSAIC subagents is on the order of 100–200K tokens by design (subagents are scoped to finish within a single context window). A cap on top of that bound wouldn't be protecting against a genuinely unbounded case; it would just truncate information that already fits comfortably within a size the schema accepts elsewhere in the same file.

This keeps the capture policy uniform: `turn.content` is full, `tool_input`/`tool_output` is full, for the same self-containment reason and under the same natural bound. No truncation-marker convention is needed because there's nothing this policy is triggered to truncate.

Accepted consequence: per-invocation JSONL files can be large (tens of MB in a tool-heavy invocation) and consumers must stream rather than fully buffer — the same volume tradeoff already accepted for full turn content, now applied consistently rather than carved out as a special case for tool calls. If real-world logs later show the context-window bound doesn't hold for some harness, the fix is to add a cap then, with evidence, not to speculatively cap now against a case that may not occur.

## 6. Schema Versioning

MOSAIC's existing versioning convention — `X.Y.Z`, already carried by every agent, skill, and hook bundle's `version` field — applies here too. `schema_version` starts at `"1.0.0"` and follows the same bump discipline:

| Component | Meaning for this schema |
|---|---|
| **X** | Breaking change to the event catalog or common envelope — a consumer written against the old major version can no longer correctly parse the new stream. |
| **Y** | New event type or new field added — old consumers still parse correctly (unknown fields/events are ignorable), new consumers get more. |
| **Z** | Clarification or non-semantic fix (e.g., a corrected field description) with no wire-format change. |

Every event carries `schema_version` in its common envelope (§3.3).

## 7. Merge Utility

Per-invocation event files stay minimal — they omit actor identity, because the folder an event's file lives in already identifies the actor. A merge utility reads a run's per-invocation JSONL files and produces one chronologically-ordered stream, **injecting** the fields that become necessary once events from many sources are interleaved: at minimum an `actor` identifier and source-file provenance. This keeps adapters from ever needing to determine their own actor identity at write time — a deliberate choice, since harness-reported agent identity is known to be unreliable on some harnesses. The merged schema is a **documented superset** of the per-file schema, not a different one.

**Location:** a new `Tools/LogMerge/` folder, following the existing one-folder-per-supporting-tool convention. Implementation language is out of scope for this document — a log-format design doc fixes the format and contract, not the tool's tech stack.

**Contract:**

- **Input:** a `run_id` (or a path to `OrchestrationLogs/{run_id}/`). The utility discovers `00_orchestrator_events.jsonl` at the run root plus every `{agent_instance_id}/03_events.jsonl` under it — it does not take an explicit file list.
- **Output:** a single chronologically-ordered JSONL stream, where every event is enriched with:

  | Injected field | Type | Required | Notes |
  |---|---|---|---|
  | `actor` | string | Required | `"orchestrator"` for events sourced from `00_orchestrator_events.jsonl`, or the subagent's `agent_instance_id` for events under an invocation folder. Derived from the source file's location, not from harness-reported identity. |
  | `source_file` | string | Required | Provenance: relative path of the JSONL the event was read from. |
  | `duration_ms` | number | Optional | On `run_end`, `session_end`, `invocation_end`, `tool_call_end` only. Computed as `end.timestamp - start.timestamp` when exactly one matching start event for the same correlation key (`run_id`, `session_id`, `agent_instance_id`, `call_id`) is found; omitted — never guessed — when the pair can't be unambiguously resolved (missing start, truncated log, more than one candidate). |
  | `turn_index` / `call_index` | number | Optional | On `turn` / `tool_call_start` only. Ordinal position among same-type events sharing the same `actor`, in file order. |

- **Ordering key:** `timestamp` (ISO 8601, common envelope). Ties are broken by input file order then line order — nothing in the schema promises sub-timestamp-resolution ordering.
- **Read-only, offline:** never invoked by the orchestration runtime; touches nothing under `OrchestrationLogs/` except to read it. This keeps the runtime-safety cost (§4.1) off the critical path and lands assembly cost on optional, consumer-side tooling instead.
- **Why derived fields land here rather than staying purely optional-per-consumer:** see §3.5 — computing them once in the shared merge tool avoids every downstream consumer reimplementing the same pairing logic, at a cost (a few bytes per applicable line, confined to the offline merged artifact) small enough not to be worth avoiding.

No `cost_usd` field exists anywhere in the schema. No error/exception event type exists.

## 8. Non-Functional Requirements

- **Format stability:** the JSONL machine format remains strictly one-JSON-object-per-line at all times, even though lines may be large. Human-readable artifacts and raw transcripts are separate, non-parseable-as-JSONL files.
- **Resilience:** consumers and the rest of the system must function correctly with missing, partial, or malformed log data.
- **Concurrency:** log writing is safe under parallel subagent execution, including the case where the orchestrator spawns several subagents within a single inference response and their hook processes fire near-simultaneously. No log file may end up interleaved, torn, or half-written.
- **Volume:** full-content capture (turns and tool calls) means substantial log volume. Consumers must stream rather than fully buffer, and must tolerate multi-kilobyte individual lines.
- **Maintainability:** transcript capture must not depend on parsing harness-internal formats, so a harness version change cannot silently break or corrupt log output.
- **Compatibility:** hook scripts run on both Windows and Linux.

## 9. Non-Goals

- Per-harness hook adapters and the mechanism each harness uses to populate these events — a separate design.
- The cost analyzer and test-evaluation tooling — this document defines the logs those will later consume, nothing about their own logic.
- Implementation language/framework for the merge utility.
- Writing the functional hook implementations themselves — that's adapter execution work, not format design.

## 10. Open Items

Nothing here blocks adapter implementation or any other downstream work:

- Whether future test-evaluation tooling needs supplementary fields layered on top of this schema without polluting production logs with test-only concerns.
