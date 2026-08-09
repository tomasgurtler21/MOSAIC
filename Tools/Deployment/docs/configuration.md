# Configuration Reference

Complete reference for the two configuration files `mosaic-deploy` reads at
startup. Both live in `MosaicDeploy/config/` at the MOSAIC root and are seeded
from the templates in `Tools/Deployment/config/` by `task install`.

- [The two files](#the-two-files)
- [tool-config.yaml](#tool-configyaml)
  - [schema_version](#schema_version)
  - [utility_agent_allow_list](#utility_agent_allow_list)
  - [allow_external_modules](#allow_external_modules)
  - [log_retention_runs](#log_retention_runs)
- [user-config.yaml](#user-configyaml)
  - [tier_models](#tier_models)
  - [custom_model_ids](#custom_model_ids)
- [tool_destinations](#tool_destinations)
  - [Why you want it](#why-you-want-it)
  - [Structure](#structure)
  - [Mapping fields](#mapping-fields)
  - [Destination fields](#destination-fields)
  - [Precedence](#precedence)
  - [Worked examples](#worked-examples)
  - [Validation errors](#validation-errors)
  - [Effect on update staleness](#effect-on-update-staleness)
- [What is NOT configurable here](#what-is-not-configurable-here)
- [Harness IDs and generic tool names](#harness-ids-and-generic-tool-names)

---

## The two files

| File | Scope | Commit it? | Written by the tool? |
|------|-------|-----------|----------------------|
| `MosaicDeploy/config/tool-config.yaml` | Project / team | Yes | No — hand-edited only |
| `MosaicDeploy/config/user-config.yaml` | Per user, per machine | **No** | Yes — rewritten when you confirm a selection |

`MosaicDeploy/` is git-ignored in its entirety at the MOSAIC root. If you want
`tool-config.yaml` under version control, either force-add it or track the
template at `Tools/Deployment/config/tool-config.yaml` instead.

Both files are optional:

- If `tool-config.yaml` is **absent**, built-in defaults apply — including an
  allow-list enabling all six shipped utility agents.
- If `tool-config.yaml` is **present**, its values are authoritative. An omitted
  key takes its zero value, which for `utility_agent_allow_list` means *no*
  utility agents, not *all* of them. This is the one place where "absent file"
  and "present file with the key omitted" differ.
- If `user-config.yaml` is absent, an empty config applies and the file is
  created on the first selection you confirm.

An unrecognised key in either file is ignored. A malformed value is a fatal
startup error.

> **Comments are not preserved in `user-config.yaml`.** The tool rewrites the
> whole file when it records a selection. All keys survive; your comments do
> not. Keep notes elsewhere. `tool-config.yaml` is never rewritten, so comments
> there are safe.

---

## tool-config.yaml

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `schema_version` | string | `"1"` | Config schema version. Do not change. |
| `utility_agent_allow_list` | list of strings | see below | Utility agent keys selectable during deployment. |
| `allow_external_modules` | bool | `false` | Whether external harness module binaries may run. |
| `log_retention_runs` | int | `0` | Max history log entries; `0` = unbounded. |
| `tool_destinations` | map | `{}` | Generic-tool → harness-tool mappings. See [tool_destinations](#tool_destinations). |

### schema_version

Forward-compatibility marker. Files carrying an older value are migrated
transparently on load: all recognisable values are preserved and the version is
updated. Do not edit this by hand.

### utility_agent_allow_list

Only agents whose key — the file base name without extension — appears in this
list are offered in the utility-agent selection UI. Everything else is omitted
regardless of what the catalog contains.

The six shipped utility agent keys:

```
anthropic-subagent-creator
harness-bug-hunter
orchestration-architect
system-prompt-capturer
transformation
workflow-creator
```

When the whole config file is absent, all six are enabled by default: opting out
of one agent is simpler UX than opting in to six, and none of these agents
performs arbitrary user-defined actions. Once the file exists, `[]` genuinely
means "none".

```yaml
utility_agent_allow_list:
  - orchestration-architect
  - workflow-creator
```

### allow_external_modules

Gates the **external** harness tier: a subdirectory under
`MosaicDeploy/harnesses/` containing a `harness-exec` executable alongside its
`harness.yaml`. Enabling this runs that binary as a subprocess, which is an
elevated trust boundary — hence the `false` default.

The `--allow-external` command-line flag enables it for one run without editing
the file. Either source is sufficient.

**Descriptor-only harnesses are not gated by this setting.** A directory
containing only a `harness.yaml` with no executable is always discovered and
always usable. See [What is NOT configurable here](#what-is-not-configurable-here).

### log_retention_runs

Caps the number of run entries retained in `MosaicDeploy/logs/history.log`;
oldest first are dropped. `0` means keep everything. `MosaicDeploy/logs/latest.log`
always holds the most recent run in full and ignores this setting.

---

## user-config.yaml

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `schema_version` | string | `"1"` | Config schema version. Do not change. |
| `tier_models` | map | `{}` | Recorded tier-to-model selections. |
| `custom_model_ids` | map | `{}` | Model IDs you typed by hand, offered as options later. |
| `tool_destinations` | map | `{}` | Personal generic-tool → harness-tool mappings. See [tool_destinations](#tool_destinations). |

### tier_models

`harness-id → tier-name → model-id`. Written automatically when you confirm a
model selection, so agents in an already-answered tier stop prompting.

```yaml
tier_models:
  claude-code:
    HIGH: claude-opus-4-5
    LOW: claude-haiku-3-5
  opencode:
    HIGH: gpt-4o
    LOW: gpt-4o-mini
```

Only **tier-level** mappings persist. Per-agent model overrides are deliberately
not stored: a tier mapping expresses a standing preference for a class of agents,
whereas a per-agent choice is a one-time override that should not silently apply
to future runs.

Tier keys are stored verbatim and are never validated against the tiers present
in source. A tier since renamed or removed does not cause a load error — it is
simply never matched.

### custom_model_ids

`harness-id → list of model IDs`. Remembers model IDs you typed by hand so they
appear as selectable **options** in future tier-model and agent-model questions.
They are options, not pre-answers: they never select themselves. This does not
contradict the "no per-agent model persistence" rule above — these are
option-pool entries, not mappings.

The list is append-only across runs and deduplicated by exact string match.
Delete an entry to stop it being offered.

```yaml
custom_model_ids:
  claude-code:
    - claude-sonnet-4-5-20250929
  opencode:
    - openrouter/qwen/qwen3-coder
```

---

## tool_destinations

Valid in **both** config files with identical syntax and semantics.

### Why you want it

A generic agent declares generic tool names (`file_read`, `terminal`, …). Each
harness descriptor maps those to its own tool names. When an agent declares a
generic tool the target harness has **no mapping for**, deployment prompts you
to type a harness-side name for it.

Those prompt answers are **not persisted**. Only `tier_models` and
`custom_model_ids` are written back to `user-config.yaml`. So the same prompt
returns on every deploy and every update, forever.

Declaring the mapping in `tool_destinations` answers it permanently: the tool is
now *mapped*, so it is never asked about again.

It also answers the mirror-image question. During `promote` (deployed agent →
generic), a harness-side tool with no reverse mapping prompts "which generic
tool is this?". Reverse resolution scans the same table, so **one entry silences
both directions**.

### Structure

`harness-id → ordered list of mappings`.

```yaml
tool_destinations:
  <harness-id>:
    - generic: <generic tool name>
      destinations:
        - to: main | field
          field: <frontmatter key>      # to: field only
          format: <value format>        # to: field only
          separator: <string>           # format: scalar only
          names: [<harness tool names>]
```

The syntax is **intentionally identical** to the `tools.mappings:` block of a
harness descriptor. An entry can be copy-pasted between a `harness.yaml` and a
config file unchanged, in either direction. Both are validated by the same
routine, so a declaration legal in one is legal in the other. See
[descriptor-schema.md](descriptor-schema.md) for the descriptor side.

### Mapping fields

| Field | Required | Rules |
|-------|----------|-------|
| `generic` | yes | Generic tool name. Must be non-empty and unique within one harness's list. |
| `destinations` | yes | Ordered list of output targets. May be empty — see below. |

**`destinations: []` is meaningful.** An empty list declares the generic tool
*explicitly unsupported by this harness*. It emits nothing and silences the
prompt. This is different from omitting the mapping entirely, which leaves the
tool unmapped and keeps prompting.

A mapping may declare several destinations; each is rendered independently.

### Destination fields

| Field | Required | Allowed when | Notes |
|-------|----------|--------------|-------|
| `to` | yes | always | `main` or `field`. |
| `field` | when `to: field` | `to: field` only | Frontmatter key to emit. Forbidden with `to: main`. |
| `format` | no | `to: field` only | `list-block` (default), `list-flow`, `scalar`. Forbidden with `to: main`. |
| `separator` | no | `format: scalar` only | Defaults to `", "`. |
| `names` | yes | always | Non-empty list of harness-side tool or MCP server names. |

- **`to: main`** appends the names to the harness's own tools key, rendered in
  the harness's own tools format. That is why `format` is forbidden here — the
  harness owns the rendering. For a permission-shape harness (OpenCode), a name
  landing in `main` is set to `allow` in the permission map.
- **`to: field`** writes the names to a separate frontmatter key of your
  choosing, rendered in `format`. Names within one field key are sorted
  lexicographically; field keys are emitted in first-seen order. A `to: field`
  destination never enters a permission map.

Two destinations within one mapping may not share the same `(to, field)` pair.
When several destinations contribute to the same field key across different
mappings, the **first** contributor establishes that key's `format` and
`separator`; later ones only add names.

### Precedence

Per generic tool, highest first:

```
user-config.yaml  >  tool-config.yaml  >  harness descriptor
```

Precedence applies to a generic tool as a **whole unit**. A higher-precedence
declaration replaces the entire destination set beneath it; destination lists
are never merged across layers.

To *extend* a built-in mapping rather than replace it, restate the built-in
destinations alongside your new ones:

```yaml
# claude-code's descriptor maps file_read -> Read.
# This keeps Read AND adds an MCP server, because the whole set is restated.
tool_destinations:
  claude-code:
    - generic: file_read
      destinations:
        - to: main
          names: ["Read"]
        - to: field
          field: mcp_servers
          names: ["mosaic-kb"]
```

Output ordering is deterministic and never depends on map iteration: descriptor
tools first in descriptor order, then project-only tools in declaration order,
then user-only tools in declaration order.

### Worked examples

**1 — Stop the repeated prompt for a tool your harness doesn't know.**
This is the common case.

```yaml
# tool-config.yaml
tool_destinations:
  claude-code:
    - generic: knowledge_base
      destinations:
        - to: main
          names: ["mcp__mosaic-kb__search"]
```

**2 — Fan one generic tool out to two destinations.**
The harness's own `tools` key *and* a separate `mcp_servers` key.

```yaml
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: main
          names: ["AskUserQuestion"]
        - to: field
          field: mcp_servers
          format: list-block
          names: ["user-feedback"]
```

Produces:

```yaml
tools: Read, Write, AskUserQuestion
mcp_servers:
  - user-feedback
```

**3 — A field destination rendered as one comma-separated scalar.**

```yaml
tool_destinations:
  claude-code:
    - generic: browser
      destinations:
        - to: field
          field: extra_tools
          format: scalar
          separator: ", "
          names: ["playwright", "fetch"]
```

Produces `extra_tools: fetch, playwright` (names sorted within the field).

**4 — Declare a tool unsupported on one harness.**
No output, no prompt.

```yaml
tool_destinations:
  opencode:
    - generic: knowledge_base
      destinations: []
```

**5 — Personal override on top of a team default.**
The project file gives everyone a default; your machine uses a different server.

```yaml
# tool-config.yaml (committed)
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: main
          names: ["AskUserQuestion"]

# user-config.yaml (yours only) — replaces the above for this tool
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: main
          names: ["mcp__user-feedback__ask_user_questions"]
```

### Validation errors

A malformed declaration is a **fatal startup error** naming the file and the
exact dotted path, so you can locate it without guesswork:

```
/path/MosaicDeploy/config/tool-config.yaml: tool_destinations.claude-code[1].destinations[0].field: destination of kind "field" requires a non-empty field name
```

The rules enforced:

| # | Rule |
|---|------|
| 1 | `generic` must be unique within a harness's list |
| 2 | `generic` must be non-empty |
| 3 | `to` must be `main` or `field` |
| 4 | `to: field` requires a non-empty `field` |
| 5 | `to: main` must not declare `field` |
| 6 | `format` must be `list-block`, `list-flow`, or `scalar` |
| 7 | `to: main` must not declare `format` |
| 8 | `separator` is only meaningful with `format: scalar` |
| 9 | No two destinations in one mapping may share a `(to, field)` pair |
| 10 | A destination inside a non-empty list must declare at least one name — omit the destination, or use `destinations: []`, to mark a tool unsupported |

Harnesses are checked in sorted ID order, so the first reported error is stable
across runs.

### The `tool_mappings_version` stamp

Every deployed agent's frontmatter carries a `tool_mappings_version: <hex>`
field. This is bookkeeping written by the deployment tool, not something you
author or edit:

- **What it is** — a content hash of the effective `tool_destinations`
  mappings for that agent's harness.
- **Where it comes from** — both config files combined: the project-level
  `tool-config.yaml` and the user-level `user-config.yaml`, per harness.
- **What it is for** — letting `update` detect that a harness's tool mappings
  changed without re-diffing the full tool list. Editing `tool_destinations`
  changes the hash, so already-deployed agents are correctly detected as
  stale on the next `update` and regenerated with the new mappings. You do
  not need to redeploy from scratch.
- **Empty is normal** — when neither config file declares any mappings for a
  harness, the hash is empty, so agents deployed before this feature existed
  do not report spurious staleness.
- **Safe to ignore, do not hand-edit** — the value has no meaning to read by
  eye; hand-editing it only causes incorrect staleness detection on the next
  `update`. Leave it alone.
- **Stripped on promote** — the reverse `promote` flow (turning a deployed
  agent back into generic source) strips this field along with the other
  deployment stamps, since it is per-deployment and has no place in a
  generic source.

---

## What is NOT configurable here

These files tune *mappings and selection*. They cannot change a harness's tool
universe, permission shape, frontmatter keys, output paths, or file extensions.

To change those without rebuilding the binary, drop a full descriptor at:

```
MosaicDeploy/harnesses/<any-folder-name>/harness.yaml
```

Discovery precedence is `external > descriptor-only > built-in`, and the `id:`
field inside the YAML is authoritative — not the folder name. So a descriptor
declaring `id: "claude-code"` **overrides the compiled-in built-in entirely, at
runtime, with no rebuild**. You may place `HarnessInjections.md` and
`HarnessInjectionsOrchestrator.md` alongside it.

Trade-off: a descriptor-only override loses any Go-side behaviour the built-in
module adds beyond the shared descriptor algorithms. Prefer `tool_destinations`
when a mapping change is all you need; reach for a descriptor override only when
you must change the harness's shape.

See [harness-contributor-guide.md](harness-contributor-guide.md) and
[descriptor-schema.md](descriptor-schema.md).

---

## Harness IDs and generic tool names

Built-in harness IDs, for use as `tool_destinations` keys:

| ID | Display name | Tool shape |
|----|--------------|-----------|
| `claude-code` | Claude Code | list; emitted as a comma-separated scalar |
| `opencode` | OpenCode | permission (allow/deny map) |
| `ghcp-cli` | GitHub Copilot CLI | list |
| `vscode-ghcp` | VS Code GitHub Copilot | list |

A descriptor-only or external harness uses the `id:` declared in its
`harness.yaml`.

Generic tool names every built-in harness already maps. You only need a
`tool_destinations` entry for one of these if you want to **override** it:

```
file_read   file_write   file_edit
file_search content_search
terminal    subagent     user_interaction   skill
```

Any other generic tool name appearing in your source agents is unmapped on every
built-in harness and will prompt on every run until you declare it.
