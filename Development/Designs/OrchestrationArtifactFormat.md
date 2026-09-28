# Orchestration Artifact Format

> **Status:** Approved
> **Version:** 2.3
> **Created:** 2026-07-28
> **Last Updated:** 2026-09-27
> **Scope:** The schema of `Orchestration.md` — the blackboard artifact an orchestrator (human-driven LLM or a future deterministic script) reads and writes to track execution state for one workflow run. Defines its sections, their mutability rules, and the format each section uses.

---

## 1. Purpose

`Orchestration.md` is the single persistent record of one orchestration run: which subagent ran when, in what phase/stage, with what outcome, and what it produced. For a table-backed run, every orchestrator — whether an LLM interpreting a workflow table or a deterministic script executing the same table — reads and writes this same file, so a run started by one can be resumed by the other. Native ad-hoc runs use the same record for auditability but carry the continuation limitation described in §4.

This document defines that file's schema precisely enough for a script to parse and mutate it without ambiguity, while keeping it fully readable by a human or an LLM with no tooling involved. For table-backed runs, it does not define how the referenced routing table is located or read at runtime — `Orchestration.md` records the active workflow id and version, while resolving that reference is a separate executor concern. Native ad-hoc runs instead carry the reserved identity defined in §4 and resolve one invocation at a time under the native orchestrator's instructions.

## 2. Design Principles

| Principle | Description |
|---|---|
| **Every section's mutability follows its purpose, not a blanket rule** | A section that exists to record history is append-only. A section that exists to describe current state is a keyed registry, updated in place. Forcing one uniform rule onto both produces the wrong shape for whichever purpose it doesn't fit — this schema picks per section instead. |
| **Mechanical over authored** | Every field a script must populate is derived directly from data the orchestrator already has at hand — protocol fields, current phase/stage, sequence counters — never information that requires domain judgment. Where a field looks like free text (e.g. the Execution Log's `Summary`), it's sourced from data a subagent already produced, not invented fresh by the orchestrator — see §5. |
| **Self-describing** | For table-backed runs, everything needed to resume — including after a different orchestrator implementation last touched the file — lives in the file itself. Native ad-hoc runs record every consequential decision they make, but without one governing routing table they do not guarantee deterministic cross-executor continuation (§4). |
| **Human-readable, script-parseable, not both by accident** | Every section is either plain frontmatter (trivially parsed) or a fixed-column table wrapped in an explicit boundary marker (trivially located and skipped). No section relies on prose-parsing heuristics. |
| **No structure without a consumer** | A field, column, or section only earns a place here if something actually reads it for a purpose. Where a prior draft of this schema carried structure "for completeness," it's been folded into something that already exists rather than kept as its own thing — see §5's `Checkpoint` column and §6's registry model. |
| **Minimal overhead by default** | Checkpointing stays optional. Sections with no entries yet render as an empty table under a heading — a script must not treat "section present, zero rows" as an error. |

## 3. Section Model

The file has three kinds of section, distinguished by how a parser is expected to treat them — not by where they sit in the file:

| Tier | Sections | Parser obligation |
|---|---|---|
| **1 — Frontmatter** | Header fields + Current State | Parse fully; these are the fields routing decisions are made from. |
| **2 — Structured, machine-read** | Execution Log, Artifacts | Parse fully as fixed-column tables. Mutability differs per section — see §5, §6. |
| **3 — Structured, semantically opaque** | Workflow Notes | Parse the `Seq` and `Note` row structure only to preserve and append entries. Deterministic executors do not interpret `Note` prose or derive routes from it; LLM orchestrators may read it as continuity context under their routing policy. |

All Tier 2 and Tier 3 sections are wrapped in an explicit boundary tag, `<Name type="core"> ... </Name>`, consistent with the region-marker convention used across agent and workflow files in this workspace. The tag's only job here is to let a parser find the start and end of a section without depending on markdown heading structure or section ordering — it carries no meaning beyond that boundary.

Tier 1 lives in the file's YAML frontmatter block, not inside a region tag — frontmatter is already a well-defined, universally-parsed boundary (the leading `---` / `---` pair), so wrapping it again would be redundant.

**No dedicated resume section:** a mutable Last Action / Next Action / free-form Context block was considered as a way to make resumption cheap without re-deriving it, but proved unnecessary — Current State plus the last Execution Log row already give an orchestrator everything it needs to determine its next action, and any free-form deviation that matters to a future resume belongs in Workflow Notes, not a dedicated mutable section. This schema does not include one.

**No dedicated checkpoints section either**, for the same reason: a checkpoint is always taken immediately after some invocation completes, and that invocation's Execution Log row already carries the phase, stage, and sequence a checkpoint would otherwise restate. Checkpointing is instead one optional column on the Execution Log — see §5.

### 3.1 Tag Syntax

A boundary tag is an XML-style name-first tag occupying its own line. The trimmed line must be exactly the tag — nothing before or after.

**Open tag canonical form:**

```
<ExecutionLog type="core">
<Workflow type="core" name="quick-fix">
<Workflow type="core" name="quick-fix" version="3.0">
```

- Attribute values are double-quoted.
- Attributes are separated by exactly one space.
- Canonical attribute order: `type`, then `name` (when present), then `version` (when present).
- The line carries no other content.

**Close tag canonical form:**

```
</ExecutionLog>
</Workflow>
```

No attributes. The tag name alone. Same own-line rule.

**Type attribute values:**

| `type` value | Region kind | Usage |
|---|---|---|
| `core` | MOSAIC-authored structure | The three data sections of this schema (`ExecutionLog`, `Artifacts`, `WorkflowNotes`); workflow regions and infrastructure agent regions in orchestrator files |
| `project` | Project-supplied content | Slots MOSAIC declares in source for a project to fill, preserved byte-identically on update (e.g. `CodebaseContext`) |
| `managed` | Tool-regenerated content | Regions written by the deployment tool, discarded and regenerated on each deploy (e.g. `InfrastructureAgents`, assembled from the deployment's selections) |
| `custom` | Project-invented regions | Regions a project adds entirely on its own, with no MOSAIC-declared anchor |

**Enumerable regions:** Where multiple regions share a common prefix (workflows, infrastructure agent declarations), the prefix is the tag name and the instance id is carried in a `name` attribute. The closing tag uses only the tag name — no attributes, no id.

| Open tag | Close tag |
|---|---|
| `<ExecutionLog type="core">` | `</ExecutionLog>` |
| `<Workflow type="core" name="quick-fix">` | `</Workflow>` |
| `<InfrastructureAgent type="core" name="build-runner">` | `</InfrastructureAgent>` |

**Version attribute:** Where a region carries a declared version, it appears as a `version` attribute on the opening tag. Regions that carry no version today do not gain one; the attribute is optional and its absence is not an error.

**Inertness rule:** A line is a MOSAIC boundary **only if** it is a well-formed tag on its own line carrying a `type` attribute whose value is one of `core`, `managed`, `project`, or `custom`. Everything else is text, preserved byte-for-byte through parse and serialise:

- `<Foo>` — no `type` attribute → inert text
- `<Foo type="widget">` — unrecognised `type` value → inert text
- `<Foo type="core"> trailing words` — not alone on its line → inert text
- `<Foo type="core"/>` — self-closing → inert text (no self-closing form exists)
- `</Foo>` with no matching open — inert text

**Lenient parse, canonical write.** Attributes may appear in any order, separated by any run of spaces or tabs, and values may be single- or double-quoted. A hand-edited file with reordered attributes is still recognised as a boundary. On serialisation the output is always canonical form.

## 4. Frontmatter (Tier 1)

```yaml
---
type: orchestration-artifact
run_id: "20260129T090000Z-a3f9"
workflow: quick-fix
workflow_version: "3.0"
task: "Add JWT-based authentication to the user service API"
started: 2026-01-29T09:00:00Z
last_updated: 2026-01-29T11:30:00Z
global_sequence: 8
checkpoints: enabled
commits: enabled
commit_branch: mosaic/run/20260129T090000Z-a3f9
review_loop_limit: 3
current_state:
  phase: EXECUTION
  stage: 2
  last_status: SUCCESS
  last_agent: "Implementation#14"
  error_code: null
---
```

(`quick-fix` declares no execution groups, so its stage value is a bare number. A grouped workflow would read `stage: Implementation.2` here — see §4.1.)

| Field | Mutability | Description |
|---|---|---|
| `type` | Set once | Constant `orchestration-artifact`. A `type` discriminator isn't part of the shared-fields convention already in use elsewhere in this workspace (agent and workflow frontmatter identify themselves via `id`/`name`, not a `type` field) — it's included here because this is the first document type without its own dedicated filename pattern to fall back on. Worth reconciling against the wider convention rather than treated as final on its own. |
| `run_id` | Set once | Required unique identifier for this run, minted at artifact creation and never modified. Format: `{YYYYMMDD}T{HHMMSS}Z-{4-char-hex-suffix}` (e.g. `20260129T090000Z-a3f9`). Used to derive the run-scoped folder name (§11) and to correlate dispatch messages and log events with this run. An empty, absent, or malformed value makes the artifact invalid; current executors must refuse to create or resume it. |
| `workflow` | Set once | Id of the active workflow definition. This is a reference only; resolving it to the actual routing table is out of scope here (§1). Native ad-hoc runs use the reserved value `ad-hoc`. |
| `workflow_version` | Set once | Version of the workflow definition pinned at run start. Recorded so drift between this and whatever routing table is later resolved is at least detectable, even though detecting it is not this document's concern. Native ad-hoc runs use `"1.0"`, the version of the ad-hoc orchestration convention rather than a routing-table version. |
| `task` | Set once | Human-readable description of what the run accomplishes. |
| `started` | Set once | ISO-8601 timestamp at file creation. |
| `last_updated` | Updated in place | ISO-8601 timestamp, bumped on every write to this file. |
| `global_sequence` | Updated in place | Monotonically increasing invocation counter. Incremented before each subagent invocation; the incremented value becomes that invocation's `{AgentName}#{Number}` suffix. Never decremented or reused — a rollback-performing invocation is still just an invocation (§5) and gets the next sequence number like any other. |
| `checkpoints` | Set once | `enabled` or `disabled`. Fixed for the life of the run. When `disabled`, the Execution Log's `Checkpoint` column is always empty. |
| `commits` | Set once | `enabled` or `disabled`. Fixed for the life of the run. Controls whether `commit`-class infrastructure agents fire on trigger. Default `disabled`. |
| `commit_branch` | Set once | The branch name the `commit`-class agent commits to, recorded once at run start. Present when `commits: enabled`; absent or `null` when `disabled`. Enables branch-mismatch detection if `HEAD` moves mid-run. The value is whatever the run-start setup dispatch returned (`CommitAgent.md` §4.9) — the orchestrator records it rather than deriving it, and inspects the repository at no point. The run's variant is decidable from this field alone: MOSAIC-owned exactly when the value is `mosaic/run/{run_id}`, which is why no separate variant field exists to disagree with it. |
| `review_loop_limit` | Set once | Optional positive integer chosen by the user at run start. Both orchestrators' shared Routing Policy escalates instead of routing a reviewer's `COMPLETED_NEEDS_ACTION` back once that reviewer has returned it this many times at the current phase and stage, unless Workflow Notes records a user decision to continue. Absent means no limit, which is also how artifacts created before the field existed are read. |
| `infrastructure_overrides` | Set once | Optional block; absent in the common case. When present, each key is an infrastructure agent name whose `triggers` list is replaced by the specified list for the duration of the run. Set once at run start and never modified during the run. An agent name not present in the infrastructure agent declaration region is a start-up error. Shape: `{agent-name}: { triggers: [ { trigger: <trigger>, trigger_param: <param-or-null> } ] }`. |
| `infrastructure_selections` | Set once | Optional map from gated infrastructure class to the one declared agent selected for that class, present only when the deployment offers multiple agents of a gated class. Current gated classes are `checkpoint`, `commit`, and `restore`. Shape: `{checkpoint: checkpoint-manager-git, commit: commit-manager-git}`. The selected name must belong to the keyed class in the deployed declaration region. Persisting the choice makes the active mechanism stable across resume; a resumed executor reads it rather than asking again or choosing a different agent. |
| `runner_mode` | Set once on first Runner use | Optional Runner-owned execution policy: `orchestrated`, `auto`, or `auto-review`. A Runner-created run writes it at creation. When a Runner first resumes a native-created artifact that omits it, the user supplies the value and the Runner records it once. Later Runner resumes reuse it. Native orchestration preserves but does not act on it. |
| `runner_pre_consultation` | Set once on first Runner use | Optional Runner-owned `enabled` or `disabled` choice controlling whether each Runner session reconstructs environment advice before auto-routing. Created and adopted under the same rules as `runner_mode`; native orchestration preserves but does not act on it. |
| `runner_manual_resolution` | Set once on first Runner use | Optional Runner-owned `enabled` or `disabled` choice selecting the user as deviation resolver. Created and adopted under the same rules as `runner_mode`; native orchestration preserves but does not act on it. |
| `current_state.phase` | Updated in place | One of the standard workflow phases (`PLANNING`, `DESIGN`, `EXECUTION`, etc.), always as the bare phase name — see §4.1. `COMPLETED` is the terminal value written after the session finishes successfully — once set to `COMPLETED`, the run is no longer resumable. |
| `current_state.stage` | Updated in place | Stage value when `phase` is `EXECUTION` and the workflow has stages; `null` otherwise. Format defined in §4.1. |
| `current_state.last_status` | Updated in place | The status code the run routes on for the most recently accepted workflow invocation (§8). After a HITL re-dispatch that discharged the gate with `SUCCESS`, this is the original invocation's status (and `error_code` likewise), while `last_agent` names the re-dispatch; the Execution Log keeps both rows as returned. `null` before any has been accepted. |
| `current_state.last_agent` | Updated in place | `{AgentName}#{Number}` of the most recently accepted workflow invocation (§8); `null` before any has been accepted. |
| `current_state.error_code` | Updated in place | Populated only when `last_status` is `BLOCKED`; `null` otherwise. |

`current_state` is the one nested block whose fields are all mutable in place — grouping it makes explicit that this is the single piece of frontmatter an orchestrator overwrites in place after every accepted workflow outcome (§8), versus the surrounding fields which are set once at creation (aside from `last_updated` and `global_sequence`, which update alongside it).

**Runner-owned fields do not create a second artifact format.** Native and Runner orchestration share this schema; for table-backed runs, either can resume the same file. The `runner_*` prefix marks execution policy consumed only by the Runner: native orchestration leaves those fields byte-for-byte unchanged and makes no routing, configuration, or recovery decision from them. A native-created table-backed run legitimately omits them; first Runner use adopts the run by recording them once.

**Ad-hoc continuation boundary.** An artifact with `workflow: ad-hoc` remains a complete record of invocations and outcomes, but it does not point to one routing table from which every future transition can be reconstructed. The native orchestrator records consequential inferred routes and artifact contracts in Workflow Notes. On resume it may continue only when those records and the available workflows resolve the next valid invocation; otherwise it asks the user or refuses to continue. Deterministic executors must refuse an ad-hoc artifact unless they implement this convention explicitly.

### 4.1 Phase and stage value vocabulary

Two facts locate a run in its orchestration: its phase and, inside `EXECUTION`, its stage. Both appear twice in this artifact — in `current_state` (§4) and in the Execution Log's `Phase`/`Stage` columns (§5) — and `Created In` (§6) is composed from them. **All four use the vocabulary defined here.** They are written identically by every executor, so a table-backed run started by an LLM orchestrator and resumed by a script (or the reverse) reads the same values.

**Phase is always the bare phase name.** `EXECUTION` — never `EXECUTION.[StageNumber]`, never `EXECUTION.Test.[StageNumber]`. Those qualified strings are *routing-table* notation identifying a workflow row; they are a property of the workflow definition, not of the run's position, and they never appear in this artifact in any field. A qualified value here breaks two things silently: per-stage HITL resolution and the `EXECUTION` branch of recovery (§9) both compare the phase against the literal string `EXECUTION`, and neither reports a mismatch.

**Stage carries the group.** The stage value is the one and only place a workflow's execution group surfaces in this artifact:

| Situation | `Stage` (log) / `stage` (frontmatter) | `Created In` |
|---|---|---|
| Phase is not `EXECUTION` | `-` / `null` | `RESEARCH`, `PLANNING`, … (phase alone) |
| `EXECUTION`, workflow declares no groups, stage 4 | `4` | `EXECUTION.4` |
| `EXECUTION`, group `Test`, stage 1 | `Test.1` | `EXECUTION.Test.1` |
| `EXECUTION`, group `Implementation`, stage 3 | `Implementation.3` | `EXECUTION.Implementation.3` |

Rules:

- `{StageNumber}` is the 1-based index of the stage in the plan artifact's stage table. Never `0`, never negative — a non-positive number means "not staged", which is written as `-`/`null`, not as a numeric stage.
- `{Group}` is copied verbatim from the group segment of the routing row's `Phase` cell. It is case-sensitive and contains no `.` and no whitespace. A workflow that declares no groups produces a bare number; there is no placeholder group token and no `-` filler in the group position.
- The separator is a single `.`. A parser splits on the **last** `.` so the numeric part is unambiguous.
- **Legacy read tolerance:** earlier orchestrator versions wrote `Stage-{N}` (e.g. `Stage-1`). A consumer must accept that form on read as ungrouped stage `N`. Nothing writes it.

**The stage value is not a folder name.** Per-stage artifacts live under `Stage-{StageNumber}/` (e.g. `Stage-1/Plan.md`), keyed on the stage number alone. Group never appears in a path — the `Test` and `Implementation` groups of stage 1 write into the same `Stage-1/` folder. The two conventions are independent and neither is derived from the other.

Group vocabulary itself — how a workflow declares groups, how the plan artifact's `Approach` column selects a group order, and what a runner refuses — is a workflow-definition concern and is specified in `Workflows/ExecutionGroups.md`. This artifact only records the group the executing row belonged to.

## 5. Execution Log (Tier 2, append-only)

```markdown
<ExecutionLog type="core">
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |
|-----|-------|-------|-------|--------|-----------|---------|--------|------------|
| 1 | Research#1 | RESEARCH | - | SUCCESS | 2026-01-29T09:05:00Z | Analyzed auth requirements, JWT approach selected | - | - |
| 2 | Validator#2 | RESEARCH | - | SUCCESS | 2026-01-29T09:10:00Z | Validated JWT approach feasibility | Research.md | - |
</ExecutionLog>
```

One row per completed subagent invocation, appended after that invocation completes — never before, and never modified afterward. Every field on a row, including `Checkpoint`, is fixed at the moment the row is written; nothing in this section is ever revisited.

| Column | Description |
|---|---|
| `Seq` | Matches `global_sequence` at the time this row was written; also the numeric suffix in `Agent`. |
| `Agent` | `{AgentName}#{Seq}`. |
| `Phase` | Phase during the invocation, as the bare phase name (§4.1). |
| `Stage` | Stage value if `Phase` is `EXECUTION` and the workflow has stages; `-` otherwise. Carries the execution group when the workflow declares one — format in §4.1. |
| `Status` | The subagent's returned status code. |
| `Timestamp` | ISO-8601, invocation completion time. |
| `Summary` | The subagent's own `status_message` from its protocol response, copied across — not text the orchestrator composes itself. This keeps `Summary` inside the "mechanical" category (§2) despite reading like free text: it's copied content, not authored content. Bounded and single-line by construction — a `|` or a literal newline inside this field is invalid and must be stripped or escaped by whatever writes the row. Truncation, when `status_message` exceeds 100 characters, takes the **first 50 and last 50 characters**, joined by ` ... ` — not a naive first-100 cut. The three-period delimiter is ASCII because the Communication Protocol requires orchestration artifacts to use only `U+0000`–`U+007F`. This isn't cosmetic: a verbose `status_message` (which shouldn't happen per protocol, but does) tends to front-load process narration and put the actual outcome in its closing sentence, so a head-only truncation systematically discards the part most worth keeping. Head+tail keeps both the opening context and the conclusion, at the same total character budget. |
| `Inputs` | The `input_artifacts` list dispatched with this invocation; comma-separated filenames with the run-scoped folder prefix omitted (it is identical for every artifact in a run and recoverable from `run_id`). `-` when no artifacts were passed. Sourced directly from the dispatch message — not authored content. On the same mechanical footing as `Status` or `Agent`. |
| `Checkpoint` | Empty (`-`) on almost every row. Populated on the row of the checkpoint agent invocation that took the checkpoint — never on the row of the preceding workflow step. A non-empty value names a real, externally-restorable content reference; a bare placeholder is never valid. |

**Consumers bind by column name, not position.** The `Inputs` column is an insertion that changes the column count from 8 to 9. As of version 2.0, all consumers must locate columns by matching the header row rather than by counting positions — a positional parser would silently misread every row. The header row is present in every artifact; a name-bound parser also tolerates any future column insertions without a code change.

**Checkpoints as a column, not a section.** A checkpoint is taken by a dedicated infrastructure agent dispatched on a trigger condition. That agent's own Execution Log row carries the content-reference in its `Checkpoint` column — not the row of the preceding workflow step. An earlier model placed the reference on the preceding row, on the reasoning that a checkpoint is always "taken right after invocation N," but that reasoning is incoherent once checkpointing is an agent: the checkpoint agent's row is appended only after the checkpoint completes, so populating the preceding row at that point requires editing an already-written row in an append-only section. Recording the reference on the checkpoint agent's own row eliminates the contradiction and upholds the append-only guarantee without exception. The row sits immediately after the workflow step that triggered it, so the phase, stage, and sequence context are fully recoverable from the adjacent rows — restating them in a separate section, as an earlier draft did, was pure duplication.

**A `Checkpoint` entry always means real, restorable content exists — there is no bare/contentless marker.** An earlier draft of this schema allowed populating `Checkpoint` with a placeholder when no content-preservation mechanism was available, on the theory that resetting orchestration state alone (phase/stage/sequence, no file content) was still a useful partial degradation. It isn't: with `checkpoints: enabled` but nothing actually preserving content, a "checkpoint" that can't restore anything isn't a degraded success, it's a broken promise — the whole reason to enable checkpointing is the ability to roll back, and orchestration bookkeeping without matching file content is not that. When `checkpoints: enabled` and the infrastructure agent declaration region contains no agent with `Class = checkpoint`, the configuration is invalid and the run must not start. This check is a string comparison against the declaration region and requires no inference. What this schema guarantees: if `Checkpoint` is non-empty, it names a real, externally-restorable point.

**Rollback isn't a distinct mechanism this schema defines.** Whatever performs a rollback — most plausibly a dedicated agent invocation, given the `Checkpoint` reference points at content something else preserved — is just another subagent invocation as far as `Orchestration.md` is concerned: it gets dispatched, it does its work, and it completes with a normal status code, at which point its own row is appended like any other invocation's. As a non-workflow invocation it leaves `current_state` unchanged (§8): the run's accepted workflow position is not rewound by the restore itself. No special row shape, agent name, or status value is reserved for it here. How that invocation resolves a target checkpoint into an actual restore is entirely its own design.

**Retention needs no in-file marking, and this schema doesn't set a retention policy.** Because `Checkpoint` values are never edited after being written, "which checkpoints are still live" isn't state the file has to track — it can be computed by whoever needs it, by walking the Execution Log backward and treating the most recent non-empty `Checkpoint` values as the live rollback targets, however many that consumer chooses to keep. Older checkpointed rows remain exactly as written (nothing is ever deleted or marked `[EXPIRED]`) — they simply aren't picked as targets once retired by that read-time rule. This avoids the alternative (marking old entries as expired in place), which would have made one column in an otherwise strictly-append-only section mutable after the fact. How many checkpoints to retain, and by what rule, is a policy question for whatever manages checkpoint creation — not a parameter this document defines.

The column is `Agent`. A now-removed `Orchestration.template.md` labelled it `Subgent` — a typo, never an intentional naming choice — and artifacts produced from that template still exist. Consumers may treat a `Subgent` header as a legacy alias for `Agent` on read; nothing writes it.

## 6. Artifacts (Tier 2, keyed registry)

```markdown
<Artifacts type="core">
| Artifact | Created In | Created By |
|----------|------------|------------|
| Research.md | RESEARCH | Research#1 |
| Plan.md | PLANNING | Planner#3 |
| Stage-1/PlanProgress.md | EXECUTION.Implementation.1 | Implementation#10 |
</Artifacts>
```

Unlike the Execution Log, this section is **not** a history — it's a lookup table answering "what artifacts currently exist, and who most recently produced each one." Its whole purpose (letting an orchestrator determine what to pass to the next subagent without re-deriving it from the workflow table each time) is a current-state question, not a historical one; the invocation-by-invocation history of what happened when already lives in the Execution Log, so duplicating it here would only add noise without adding information nothing else already provides.

**Rows are keyed by concrete `Artifact` path.** Insert a row only when an authorized output was actually created or modified. On rework, update the existing row in place; never append a duplicate key. If duplicates are inherited, use the Execution Log to retain the latest producer. The Execution Log remains the invocation history; this table shows only current artifact state.

| Column | Description |
|---|---|
| `Artifact` | Concrete path actually created or modified, with the run-scoped folder prefix omitted exactly as in the Execution Log's `Inputs` column (§5) — e.g. `Stage-2/Plan.md`. The key for this table; wildcard patterns are never stored. |
| `Created In` | `Phase` when the invocation had no stage, `Phase.Stage` when it did — both components taken verbatim from `current_state` at the moment this row is last written, joined by a single `.` (§4.1). Reflects the most recent write, not the original creation. |
| `Created By` | `{AgentName}#{Seq}` of the invocation that most recently produced or reworked it — the same value as that invocation's `Agent` column in the Execution Log, letting the two tables be cross-referenced directly. The single exception is the reserved literal `user`, for artifacts adopted at run init (§11) that no invocation produced; such rows carry `Created In: INIT` and are the one case where a row has no corresponding Execution Log entry. A consumer cross-referencing this column must tolerate that. Once a subagent reworks such a path, the row is overwritten in place like any other and `user` is replaced by the producing invocation. |

**No `Type` column, and no validity-window notation for scope.** A column classifying the artifact (Research / Plan / Review / …) was considered but left out: that classification is already fully recoverable from the artifact's own filename — reserved-keyword naming conventions already used across this workspace (a `Plan`-named file is a routing artifact, a kebab-case `{agent-name}.md` file is a review output, and so on) make a separate stored classification pure duplication of what the name already encodes. A richer scope notation (marking an artifact as valid across a phase range, or across a specific span of stages) was also considered and left out — expressing that requires judgment about how long an artifact stays relevant, which isn't something a script can determine mechanically at write time. Where that distinction matters (e.g. `Iteration1_Review.md` vs. `Iteration2_Review.md`), it's already visible in the filename itself, not something this table needs to additionally encode.

## 7. Workflow Notes (Tier 3)

```markdown
<WorkflowNotes type="core">
| Seq | Note |
|-----|------|
| 1 | Token expiry should be 24 hours per security policy |
| 4 | User confirmed: use RS256 algorithm, not HS256 |
</WorkflowNotes>
```

Free-form orchestrator memory — constraints, clarifications, decisions, and routing conclusions discovered mid-run that later decisions or invocations need but that do not belong in any structured field. Appended, never edited or removed.

The row envelope is deliberately small. `Seq` is the artifact's current `global_sequence` when the note is appended: it identifies the most recent completed recorded invocation whose outcome the orchestrator is responding to, or `0` when the note is written during run initialization before any invocation. `Note` carries the prose. Deterministic executors may parse that structure to preserve and append rows, but they do not interpret `Note` or derive routes from it. LLM orchestrators may read the prose and act on explicit user decisions, clarifications, waivers, and prior routing conclusions under their own routing policy.

Subagents do not read Workflow Notes directly. When a note applies to a later assignment, the orchestrator carries the applicable information into that invocation through the channel defined for the assignment.

## 8. Update Rules

| Section | Mutability | Written by |
|---|---|---|
| Frontmatter (non-`current_state`) | Set once at creation, except `last_updated`/`global_sequence` | Orchestrator, at creation and on every subsequent write |
| `current_state` | Overwritten in place after every **accepted workflow** outcome | Orchestrator, after the invocation completes and any required HITL gate is discharged, plus explicit phase/stage transitions |
| Execution Log | Append-only, including the `Checkpoint` column (fixed at row-write time, never revisited) | Orchestrator, after each invocation completes |
| Artifacts | Keyed registry — inserted on first actual write, updated in place on rework | Orchestrator, after each invocation, for outputs actually created or modified |
| Workflow Notes | Append-only | Orchestrator, between recorded invocations, whenever a response, user decision, or routing conclusion is worth carrying forward |

**Only an accepted workflow outcome updates `current_state`.** In a table-backed run, a row is a workflow row when its `Agent` identifier names a participant in the active workflow table. In an ad-hoc run, it is a task dispatch that is not an infrastructure or explicit out-of-band invocation. Infrastructure-agent invocations are non-workflow rows; their completion leaves `phase`, `stage`, `last_status`, `last_agent`, and `error_code` exactly as they were, so `current_state` continues to name the last accepted workflow step. A workflow invocation whose required HITL gate was not discharged is also unaccepted, unless it returned `BLOCKED` with `E503`; that response reports that the gate could not run and is accepted and routed directly. For an unaccepted attempt, append its returned attempt and advance `global_sequence` and `last_updated`, but leave `current_state` and the Artifacts registry unchanged before re-dispatching it. Infrastructure rows can additionally be identified by matching their agent names against the deployed declaration region.

This encoding deliberately uses the relationship between the log and `current_state` rather than another field: a trailing workflow row whose `Agent` differs from `current_state.last_agent` has not become the accepted workflow position. Recovery reruns that assignment instead of routing on its status. The same conservative rule covers interruption between a native orchestrator's log append and frontmatter update; duplicate work is safer than advancing on an outcome whose acceptance was never durably recorded.

All state writes associated with a recorded invocation happen **after** that invocation completes, never before — there is no "in-progress" state to track separately. The orchestrator may append Workflow Notes while deciding what to do between recorded invocations; those notes use the current `global_sequence` and do not represent another invocation. If a dispatched invocation is interrupted mid-flight, the file simply still reflects the last completed step, which is exactly what recovery (§9) relies on.

**The invariant a write must satisfy is reconcilability, not atomicity.** An earlier draft of this schema mandated that every write be a single, complete rewrite of the file (frontmatter update + Execution Log append + Artifacts upsert in one pass), on the theory that this made "file exists and is well-formed" equivalent to "file reflects a fully-completed step." That's the right guarantee but the wrong requirement: §9 reconciles a frontmatter/Execution Log disagreement conservatively by preserving the prior accepted `current_state` and re-dispatching the trailing workflow assignment. A partially-applied native write is therefore recoverable, though it may duplicate work; a deterministic writer should still make the accepted update atomic. What this schema requires is that any observable intermediate state be reconcilable by §9, which constrains **write order** rather than write count:

**The Execution Log row is written first.** An interruption after the log append but before `current_state` changes leaves the same durable shape as an intentionally unaccepted HITL attempt: the trailing workflow row and `current_state.last_agent` disagree. Recovery conservatively re-dispatches that assignment. The reverse order is unsafe because frontmatter would claim an accepted invocation the audit log does not record. A deterministic writer may update both atomically; a native orchestrator uses ordered targeted edits and accepts the small chance of duplicate work rather than risking an unsupported advance.

How an orchestrator meets this is its own implementation concern, and the right answer differs by executor. A deterministic script should still prefer a genuine atomic replace (write-temp-then-rename), which is free and eliminates the intermediate state entirely. An LLM orchestrator should not: for it, a "single rewrite" means regenerating every historical Execution Log row as output on every step, which grows without bound over a run and gives each step a fresh opportunity to mutate rows this schema declares append-only. Ordered targeted edits are both cheaper and structurally safer there, and §9 covers the residual window.

One residual gap is accepted rather than solved: an interruption between the Execution Log append and the Artifacts upsert leaves a produced path unregistered, and §9 has no reconciliation for it (the log doesn't record artifact paths, so it can't). The consequence is bounded — the Artifacts registry is a lookup convenience, dispatch artifact lists derive from the workflow's routing basis, and the row is restored on that path's next write — so this doesn't warrant additional in-file bookkeeping to close.

## 9. Recovery

On start (or restart), an orchestrator resuming an existing `Orchestration.md`:

1. Parses the frontmatter and validates `run_id`. If it is absent, empty, malformed, or disagrees with the enclosing `Orchestration-{run_id}/` folder, refuse the resume; do not mint or repair identity during recovery. `current_state` then gives phase, stage, last status, last agent, error code directly.
2. Cross-checks against the last **workflow** row of the Execution Log, skipping any trailing infrastructure or explicit out-of-band rows (§8). For a table-backed run, a workflow row names a table participant; for an ad-hoc run, it is any task dispatch not excluded above. When its `Agent` equals `current_state.last_agent`, that row is the last accepted workflow outcome. When they disagree, the trailing workflow row is an unaccepted or interrupted attempt: preserve it as history, leave `current_state` at the prior accepted outcome, and re-dispatch that workflow assignment rather than routing on the row's status. A trailing infrastructure row is not a disagreement because §8 already declares that it does not move `current_state`.
3. Validates `global_sequence`, the last allocated sequence, against the highest `Seq` in the Execution Log (workflow or infrastructure). If behind, it is corrected to `max(Seq)`; a higher stored value is preserved to avoid reusing an interrupted allocation.
4. Interprets `last_status` against the run's routing basis. In a table-backed run, `SUCCESS` follows the success route and `COMPLETED_NEEDS_ACTION` follows the configured action route. In an ad-hoc run, the native orchestrator resolves those targets from the recorded decisions and available-workflow precedent, asking the user when the next invocation remains materially ambiguous. In either kind, `PARTIALLY_DONE` continues the same workflow assignment; `NEEDS_CLARIFICATION` routes to the agent that can supply what is missing, or escalates when only a human can answer; `CAPABILITY_EXCEEDED` escalates without agent substitution; and `BLOCKED` resolves by error code. A status reports the prior invocation's outcome and never names a target by itself. No previous status means a fresh start at the beginning of the first phase.

This is the entire recovery procedure — no dedicated resume section is consulted, per §3's decision to drop it. In a table-backed run, anything needed beyond phase/stage/status/sequence is either derivable from the workflow's routing table (out of scope here, §1) or, for a one-off deviation worth remembering, already in Workflow Notes. In an ad-hoc run, Workflow Notes also carry consequential inferred routing and artifact decisions; if they and the available workflows do not resolve the next valid invocation, deterministic recovery ends at the clarification or refusal boundary in §4.

## 10. Complete Example

```markdown
---
type: orchestration-artifact
run_id: "20260129T080000Z-b2c4"
workflow: greenfield-tdd
workflow_version: "2.1"
task: "Implement user profile CRUD operations with avatar upload support"
started: 2026-01-29T08:00:00Z
last_updated: 2026-01-29T12:45:00Z
global_sequence: 16
checkpoints: enabled
commits: enabled
commit_branch: mosaic/run/20260129T080000Z-b2c4
current_state:
  phase: EXECUTION
  stage: Implementation.2
  last_status: SUCCESS
  last_agent: "Implementation#16"
  error_code: null
---

<ExecutionLog type="core">
| Seq | Agent | Phase | Stage | Status | Timestamp | Summary | Inputs | Checkpoint |
|-----|-------|-------|-------|--------|-----------|---------|--------|------------|
| 1 | Research#1 | RESEARCH | - | SUCCESS | 2026-01-29T08:10:00Z | Analyzed profile feature requirements | - | - |
| 2 | Planner#2 | PLANNING | - | SUCCESS | 2026-01-29T08:35:00Z | Created 2-iteration plan for profile CRUD | - | - |
| 3 | Designer#3 | DESIGN | - | SUCCESS | 2026-01-29T09:15:00Z | Designed ProfileService interface | Research.md | - |
| 4 | checkpoint-manager-git#4 | DESIGN | - | SUCCESS | 2026-01-29T09:16:00Z | Committed checkpoint of working tree (3 files). [checkpoint:4f1a08d] | - | 4f1a08d |
| ... | ... | ... | ... | ... | ... | ... | ... | ... |
| 13 | ImplementationReview#13 | EXECUTION | Implementation.1 | SUCCESS | 2026-01-29T10:50:00Z | All tests pass (3/3) | Plan.md, Stage-1/PlanProgress.md | - |
| 14 | checkpoint-manager-git#14 | EXECUTION | Implementation.1 | SUCCESS | 2026-01-29T10:51:00Z | Committed checkpoint of working tree (7 files). [checkpoint:7c2e9f1] | - | 7c2e9f1 |
| 15 | TestCreator#15 | EXECUTION | Test.2 | SUCCESS | 2026-01-29T11:15:00Z | Created tests for updateProfile endpoint | Plan.md | - |
| 16 | Implementation#16 | EXECUTION | Implementation.2 | SUCCESS | 2026-01-29T12:45:00Z | Implemented updateProfile endpoint | Plan.md, Design.md | - |
</ExecutionLog>

<Artifacts type="core">
| Artifact | Created In | Created By |
|----------|------------|------------|
| Requirements.md | INIT | user |
| Research.md | RESEARCH | Research#1 |
| Plan.md | PLANNING | Planner#2 |
| Design.md | DESIGN | Designer#3 |
| Stage-2/PlanProgress.md | EXECUTION.Implementation.2 | Implementation#16 |
</Artifacts>

<WorkflowNotes type="core">
| Seq | Note |
|-----|------|
| 1 | Avatar images max 2MB, stored in /uploads/avatars/ |
| 6 | Use soft delete for profile removal per user clarification |
</WorkflowNotes>
```

checkpoint-manager-git rows #4 and #14 both carry a real commit reference — both are fully restorable rollback targets. Walking the table backward, a consumer retaining (say) its two most recent checkpoints would find #14 and #4 without anything in the file itself having to record that — how many to retain is that consumer's own policy, not something recorded here (§5).

## 11. Run-Scoped Folder Convention

Each run's `Orchestration.md` is stored inside a dedicated folder derived from its `run_id`:

```
Orchestration-{run_id}/
└── Orchestration.md
```

The folder is rooted at the working directory — the directory the script runner or orchestrator starts in. For example, a run with `run_id` `20260129T090000Z-a3f9` uses the path `Orchestration-20260129T090000Z-a3f9/Orchestration.md` relative to the working directory.

This convention:
- Prevents artifact files from different runs colliding on disk (`agent_instance_id` values such as `Research#1` reset at the start of every new run).
- Makes completed runs scannable by directory listing — any folder matching `Orchestration-{run_id}/` is a candidate run.
- Keeps the artifact path derivable from `run_id` without any additional registry or index.

Subagent invocations express their `input_artifacts` and `output_artifacts` paths with the run-scoped folder as a prefix (e.g. `Orchestration-20260129T090000Z-a3f9/Plan.md`). The orchestrator resolves paths with this prefix to the artifact store at the corresponding location; paths that already include the prefix are not double-prefixed.

### Seed artifact adoption

A run is frequently started from an artifact the user authored beforehand — a requirements document, a specification, a brief. Such a file cannot arrive pre-placed: `run_id` is minted at artifact creation, so the correct destination is unknowable at the moment the user writes the file. Any expectation that seed inputs already sit in the run folder is unsatisfiable by construction.

At run init, therefore, the orchestrator **copies** each user-supplied orchestration artifact into `Orchestration-{run_id}/` and registers it with `Created By: user` (§6). Copy rather than move: the original is user-authored content, and relocating it would strand the input inside a dead run folder if the run aborts before completing, forcing the user to retrieve it before retrying. Divergence between original and copy is not a concern, because the run reads only the copy from that point on.

This applies strictly to orchestration artifacts. Project files — repo content referenced via `input_files` — are never copied into the run folder; doing so would duplicate codebase content into orchestration state and erode the artifact/file distinction the protocol relies on.

Adoption does not make a run reproducible: it still reads project files that mutate underneath it, and this schema makes no attempt to capture those. The narrower property it does secure is that a run's *orchestration artifact set* is complete and archivable on its own, rather than depending on an external path that a subsequent run may overwrite.

## 12. Non-Goals

- How a table-backed workflow's routing table is located and resolved at runtime from the `workflow`/`workflow_version` reference — a separate concern, since the resolution mechanism depends on where the orchestrator runs and isn't part of this artifact's own schema (§1). The reserved ad-hoc identity and its continuation boundary are defined in §4.
- A single, formally unified frontmatter schema across every MOSAIC document type. A shared-fields convention already exists de facto across agent and workflow files in this workspace (`version`, `id`/`name`, and so on); this document's frontmatter (§4) follows that same spirit rather than diverging from it, but doesn't attempt to formally consolidate the convention itself — that consolidation, if it happens, is a separate piece of work.
- The actual mechanism that preserves and restores file content for a `Checkpoint` reference (§5) — this document defines the reference's role, not the mechanism that produces or consumes it.
- What happens when `checkpoints: enabled` but no content-preservation mechanism is available — this is now resolved in §5: the configuration is invalid and the run must not start.
- Checkpoint retention policy — how many checkpoints to keep as live rollback targets, and by what rule. §5 defines this as a read-time computation with no in-file bookkeeping, but the count and selection rule themselves are owned by whatever consumes the Execution Log for rollback, not by this schema.
- Parsing/validation tooling itself — this document specifies the format a validator would check, not the validator.

## 13. Open Items

- Whether `Artifacts` needs any script-runner consumer validation before this schema is locked in, or whether the mechanical-population argument (§6) is sufficient justification on its own.
