# Harness Descriptor Schema Reference

A harness descriptor (`harness.yaml`) is a declarative YAML file that captures everything
about a harness that does not need code. Adding a harness whose variance is purely data
requires no Go code: write a descriptor, place it in the harness folder, and the tool
picks it up automatically.

This document is the primary reference for descriptor authors. Reading Go source is not
required to write a working descriptor.

---

## Quick-start example

The annotated example below covers all seven field groups. Optional sections are marked.
Copy it, remove what you do not need, and fill in your harness's values.

```yaml
# --------------------------------------------------------------------------
# REQUIRED: schema version. Must be "1". The tool rejects any other value.
# --------------------------------------------------------------------------
schema_version: "1"

# --------------------------------------------------------------------------
# REQUIRED: unique, stable identifier for this harness (no spaces).
# --------------------------------------------------------------------------
id: "my-harness"

# --------------------------------------------------------------------------
# REQUIRED: human-readable name shown in the TUI harness picker.
# --------------------------------------------------------------------------
display_name: "My Harness"

# --------------------------------------------------------------------------
# OPTIONAL: version strings stamped into every deployed agent frontmatter.
# Bump transform_version when the transform logic changes; bump
# injections_version when harness injection content changes.
# --------------------------------------------------------------------------
transform_version: "1.0.0"
injections_version: "1.0.0"

# --------------------------------------------------------------------------
# Field group 2 — OPTIONAL: model catalog
#
# ids: the model identifiers presented to the user during model selection.
#   Any string is accepted; no validation against a known list occurs.
# format_hint: display-only guidance, e.g. "provider/model-id". Never
#   enforced; passed through verbatim.
# --------------------------------------------------------------------------
models:
  ids:
    - "provider/fast-model"
    - "provider/smart-model"
    - "SOME-ALL-CAPS-MODEL"          # case is preserved verbatim
    - "accounts/fireworks/models/x"  # deep paths are fine
  format_hint: "provider/model-id"

# --------------------------------------------------------------------------
# Field group 3 — OPTIONAL: tool spec
#
# shape: how tools are written in the deployed frontmatter.
#   "list"       — a flat YAML sequence (e.g. Claude Code agents).
#   "permission" — a nested allow/deny mapping (e.g. OpenCode agents).
#
# universe: the complete set of tool names this harness understands.
#   Declared order is the canonical output order; the tool never reorders
#   entries relative to the universe sequence.
#   - name:         harness-specific name; may be hierarchical ("read/readFile").
#   - unused:       for "permission" shape only — "allow" or "deny" for tools
#                   that the agent does not explicitly use.
#   - by_convention: if true, this tool is emitted for every agent regardless
#                   of which generic tools the agent declares.
#
# mappings: maps generic tool names (from the MOSAIC generic vocabulary) to
#   harness-specific tool names.
#   - generic:       one of: file_read, file_write, file_edit, file_search,
#                    content_search, terminal, user_interaction, skill, subagent.
#   - harness_tools: list of harness tool names this generic tool maps to.
#                    One-to-many (one generic → several harness tools) is
#                    supported. An empty list [] means "explicitly unsupported
#                    by this harness": the tool is acknowledged but not emitted.
#   - field:         (advanced) if non-empty, the tool value is written to this
#                    frontmatter key instead of the main tools field.
#
# custom_tool_template: format string for user-supplied MCP server names.
#   "%s" is replaced with the user's server name. Empty = name used as-is.
#
# placeholder_expansion: the harness tool names that the generic
#   {tool-permissions} placeholder resolves to. Empty = the whole Universe.
# --------------------------------------------------------------------------
tools:
  shape: list
  universe:
    - name: "read/readFile"
      unused: deny
      by_convention: false
    - name: "write/createFile"
      unused: deny
      by_convention: false
    - name: "write/editFile"
      unused: deny
      by_convention: false
    - name: "search/textSearch"
      unused: deny
      by_convention: false
    - name: "search/listDirectory"
      unused: allow
      by_convention: true    # always emitted, regardless of the agent's generic tools
  mappings:
    - generic: "file_read"
      harness_tools:
        - "read/readFile"
    - generic: "file_write"         # one-to-many: one generic → two harness tools
      harness_tools:
        - "write/createFile"
        - "write/editFile"
    - generic: "file_search"
      harness_tools:
        - "search/textSearch"
    - generic: "content_search"     # many-to-one: two generics → same harness tool
      harness_tools:
        - "search/textSearch"
    - generic: "user_interaction"   # explicitly unsupported: present in mapping but empty
      harness_tools: []
  custom_tool_template: "mcp/%s"
  placeholder_expansion:
    - "read/readFile"
    - "write/createFile"

# --------------------------------------------------------------------------
# Field group 4 — OPTIONAL: deployment paths
#
# Each of agents, skills, and hooks has:
#   supported: true if this harness deploys this artifact kind.
#   project:   path relative to the workspace root for project-scoped
#              deployment.
#   user:      map of GOOS → absolute path template for user-scoped
#              deployment. The empty-string key "" is the fallback for any
#              platform not named explicitly.
#              Recognised expansion tokens: ~, ${APPDATA}, ${XDG_CONFIG_HOME}.
#
# When supported is false for hooks, hook deployment is skipped entirely.
# --------------------------------------------------------------------------
paths:
  agents:
    supported: true
    project: ".my-harness/agents"
    user:
      windows: "${APPDATA}/My-Harness/agents"
      "": "~/.config/my-harness/agents"    # fallback for linux, darwin, etc.
  skills:
    supported: true
    project: ".my-harness/skills"
    user:
      "": "~/.config/my-harness/skills"
  hooks:
    supported: false    # set to true and add paths if this harness supports hooks

# --------------------------------------------------------------------------
# Field group 5 — OPTIONAL: file extension rules
#
# Keys are artifact kinds: "agent", "skill", "hook".
# Values are the file extension (including the leading dot) that the harness
# expects. Defaults to ".md" when absent.
# --------------------------------------------------------------------------
extensions:
  agent: ".agent.md"    # e.g. my-agent.agent.md
  skill: ".md"

# --------------------------------------------------------------------------
# Field group 6 — OPTIONAL: frontmatter shaping rules
#
# model_key: frontmatter key where the selected model identifier is written.
#   Empty = model is not written into frontmatter.
# tools_key: frontmatter key where the rendered tool list is written.
#   Empty = the harness module decides.
# add:       fields added to every deployed agent. Values may be YAML
#   scalars, sequences, or mappings.
# drop:      source frontmatter keys to remove from deployed output.
# key_order: desired output key ordering. Keys not listed keep their
#   source-relative order and are appended after the listed keys.
# --------------------------------------------------------------------------
frontmatter:
  model_key: "model"
  tools_key: "tools"
  add:
    - key: "harness-specific-flag"
      value: "true"
    - key: "mode"
      value: "subagent"
  drop:
    - "recommended_tier"
    - "tier_rationale"
  key_order:
    - "id"
    - "version"
    - "name"
    - "model"
    - "tools"
    - "mode"

# --------------------------------------------------------------------------
# Field group 7 — OPTIONAL: harness-level injection content
#
# These blocks fill named [[INJECTION:...]] markers in deployed agents.
# Each entry has:
#   name:    the canonical injection name (e.g. "HarnessConstraints").
#   content: verbatim text placed between the injection boundary tags.
#            May span multiple lines.
#
# Injections not listed here are left empty (the markers remain in place
# for project-specific content to be added later).
# --------------------------------------------------------------------------
injections:
  - name: "HarnessConstraints"
    content: |
      This agent runs under My Harness. Use only tools from the approved
      tool universe declared in the harness descriptor.

# --------------------------------------------------------------------------
# OPTIONAL: hook support
#
# supported:   true if this harness installs hook scripts alongside agents.
# variant_key: which hook-bundle variant folder this harness consumes.
#   Defaults to the harness id when empty.
# --------------------------------------------------------------------------
hooks:
  supported: false
  variant_key: ""
```

---

## Field reference

### Top-level required fields

| Field | Type | Description |
|-------|------|-------------|
| `schema_version` | string | Must be `"1"`. Any other value causes the descriptor to be rejected with `ErrUnsupportedSchemaVersion`. |
| `id` | string | Unique harness identifier. No spaces. Used as the map key in the registry. |
| `display_name` | string | Human-readable name shown in the harness selection screen. |

### Top-level optional fields

| Field | Type | Description |
|-------|------|-------------|
| `transform_version` | string | Stamped into every deployed agent's frontmatter as `transform_version`. Bump when the transform logic changes so stale agents can be detected. |
| `injections_version` | string | Stamped into every deployed agent's frontmatter as `injections_version`. Bump when any injection content changes. |

---

### `models` (optional)

| Field | Type | Description |
|-------|------|-------------|
| `ids` | list of strings | Model identifiers offered during model selection. Any string is accepted — provider prefixes, colons, uppercase, arbitrary formats. Nothing is validated against a known list. |
| `format_hint` | string | Display-only format guidance for the user, e.g. `"provider/model-id"`. Never enforced. |

---

### `tools` (optional)

#### `tools.shape`

| Value | Meaning |
|-------|---------|
| `list` | Tools are written as a flat YAML sequence. |
| `permission` | Tools are written as a nested allow/deny mapping. |

#### `tools.universe`

Ordered list of every tool this harness knows. Declaration order is the only ordering
authority; the deployment tool never reorders entries relative to this sequence.

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Harness-side tool name. May be hierarchical (`"read/readFile"`). |
| `unused` | `allow` or `deny` | For `permission` shape: disposition when the agent does not use this tool. Ignored for `list` shape. |
| `by_convention` | bool | When `true`, this tool is emitted for every agent regardless of the agent's generic tool list. |

#### `tools.mappings`

Maps generic tool names (from the MOSAIC generic vocabulary) to harness tool names.

| Field | Type | Description |
|-------|------|-------------|
| `generic` | string | Generic tool name. Must be unique across all mappings in this descriptor. |
| `harness_tools` | list of strings | Harness tool names this generic maps to. May be empty (`[]`) to mark the tool as explicitly unsupported by this harness. |
| `field` | string | Optional. When non-empty, the tool value is written to this frontmatter key rather than the main tools value. |

**Supported generic tool names:** `file_read`, `file_write`, `file_edit`, `file_search`,
`content_search`, `terminal`, `user_interaction`, `skill`, `subagent`.

#### Tool mapping outcomes

| Outcome | Condition |
|---------|-----------|
| `mapped` | A mapping entry exists for the generic tool (including an entry with an empty `harness_tools` list). |
| `unmapped` | No mapping entry exists and the user has not supplied a custom name or skipped the tool. |
| `custom` | No mapping entry exists but the user supplied a custom MCP server name. |
| `skipped` | The user explicitly declined to configure the tool. |

`mapped` with an empty `harness_tools` (explicitly unsupported) is distinct from `unmapped`
(not in the mapping table at all). Callers that need to distinguish them inspect the outcome
and the `harness_tools` length separately.

---

### `paths` (optional)

#### `paths.agents`, `paths.skills`, `paths.hooks`

| Field | Type | Description |
|-------|------|-------------|
| `supported` | bool | Whether this harness deploys this artifact kind. When `false`, deployment of this kind is skipped. |
| `project` | string | Deployment path relative to the workspace root, for project-scoped deployment. |
| `user` | map | GOOS → absolute path template. The `""` key is the fallback for all unlisted platforms. Tokens `~`, `${APPDATA}`, `${XDG_CONFIG_HOME}` are expanded by the executor at deploy time. |

---

### `extensions` (optional)

A mapping from artifact kind to the file extension used in deployed output.

| Key | Meaning |
|-----|---------|
| `agent` | Extension for deployed agents, e.g. `".agent.md"`. |
| `skill` | Extension for deployed skills, e.g. `".md"`. |
| `hook` | Extension for deployed hook files, e.g. `".sh"`. |

When absent for a kind, the deployment filename is derived from the artifact key with no
extension change.

---

### `frontmatter` (optional)

| Field | Type | Description |
|-------|------|-------------|
| `model_key` | string | Frontmatter key where the selected model is written. Empty = model is not emitted. |
| `tools_key` | string | Frontmatter key where the rendered tool list is written. Empty = the harness module decides. |
| `add` | list | Fields added to every deployed agent. Each entry: `key` (string) and `value` (any YAML value). |
| `drop` | list of strings | Frontmatter keys to remove from the generic source before deployment. |
| `key_order` | list of strings | Desired output key ordering. Keys not listed keep their source-relative order and are appended after the listed keys. |

---

### `injections` (optional)

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Canonical injection name, e.g. `"HarnessConstraints"`. |
| `content` | string | Verbatim text placed between the injection boundary tags. |

Injection names not listed here are left empty in deployed agents. Project-specific
injection content (placed in `[[INJECTION:ProjectContext]]` etc.) is never overwritten
by harness injection data.

---

### `hooks` (optional)

| Field | Type | Description |
|-------|------|-------------|
| `supported` | bool | Whether this harness supports hook scripts. |
| `variant_key` | string | Which hook-bundle variant folder to consume. Defaults to the harness `id` when empty. |

---

## Where the declarative boundary stops

Not everything can be expressed in a descriptor. The following behaviours require Go module
code and are documented here so a descriptor author knows when to escalate:

- **Conditional frontmatter logic:** any rule that branches on agent metadata (role, category,
  key) must be in a module. Descriptors apply the same `add`/`drop`/`key_order` to all agents.
- **Dynamic tool values:** the `permission` shape's allow/deny rules are applied uniformly per
  the `unused` field; harness-specific per-tool rules require a module.
- **Hook registration steps:** the `hooks.variant_key` selects a bundle, but writing the
  registration steps themselves (e.g. entries in `.gitconfig`) is a module concern.
- **Custom placeholder expansion logic:** `PlaceholderExpansion` names fixed tool subsets;
  conditional expansion (e.g. "expand differently for orchestrators") requires a module.

### Exceptions established across the four built-in harnesses

All four built-in harnesses (Claude Code, GHCP CLI, OpenCode, VS Code GHCP) were implemented
using a module + embedded descriptor structure. The exceptions below are documented so that
future descriptor authors recognise when a built-in module is required.

#### Exceptions common to all four harnesses

- **Tool output format serialization:** `tools.shape = "list"` produces a standard KindList
  value, but each harness serializes tools differently. Claude Code emits a comma-separated
  plain scalar; GHCP CLI and VS Code GHCP emit a flow-style single-quoted sequence;
  OpenCode emits a KindMapping (permission shape). None of these formats is expressible
  declaratively — all require module code to post-process or replace the `KindList` returned
  by `descriptor.MapTools`.

- **Skill path key subdirectory:** the `paths.skills.*` templates are flat directory paths with
  no token for an intermediate key segment. All four harnesses deploy skills under
  `<skills-dir>/<key>/<filename>` (e.g. `.github/skills/lean-tdd/SKILL.md`) to prevent
  filename collisions when multiple skills share the same entry filename (`SKILL.md`). Module
  code composes this path; the descriptor provides only the base directory.

- **Runtime version stamps:** `transform_version` and `injections_version` are declared in the
  descriptor as static source values. Stamping them into deployed agent frontmatter alongside
  the resolved model identifier is a pipeline concern — the descriptor's `add` list cannot
  reference the active request. The deployment transform applies these as dedicated Steps 3 and
  4 of `applyFrontmatter`, after the descriptor's static `add` list is applied.

#### Exceptions unique to specific harnesses

- **Claude Code — comma-separated scalar tools:** Claude Code requires tools as a plain scalar
  (`"Read, Write, Edit"`). This is the only harness with this format; module code converts the
  `KindList` result to a `KindScalar`.

- **GHCP CLI and VS Code GHCP — flow-style single-quoted tools:** both require a flow-style
  YAML sequence with single-quoted items: `['read', 'edit']` or `['read/readFile', 'edit/editFiles']`.
  Module code applies `ListFlow` style and `QuoteSingle` on every item to the `KindList` returned
  by `descriptor.MapTools`. This cannot be expressed in the descriptor because the `FieldValue`
  model has no way to specify per-item quote style in a descriptor `add` field.

- **OpenCode — conditional frontmatter (mode field):** regular agents receive `mode: subagent`;
  the orchestrator receives `mode: primary`. This branches on `FrontmatterRequest.AgentKey`,
  which the descriptor's static `add` list cannot inspect.

- **OpenCode — placeholder expansion for permission shape:** `descriptor.MapTools` expands the
  `{tool-permissions}` placeholder into a flat list, but OpenCode needs a full allow/deny
  permission block. Module code overrides the placeholder path to produce the correct shape.

- **VS Code GHCP — `search/listDirectory` universe ordering and `subagent` mapping:**
  `search/listDirectory` is emitted by convention (`by_convention: true`). Its position in the
  universe (after `agent`, before `execute/runInTerminal`) ensures the rendered tool list matches
  the canonical reference output for both the orchestrator (via placeholder expansion) and regular
  agents (via universe sort). The `subagent` → `agent` mapping and the `skill` → `[]` (empty)
  mapping are purely data-driven in the descriptor.

#### Four-harness retrospective: the module-versus-descriptor split

After implementing all four built-in harnesses, the stable split is:

**Belongs in the descriptor (declarative, always):**
- Tool universe and mappings (including one-to-many, many-to-one, empty, and by-convention)
- Placeholder expansion tool lists
- Deployment paths (agents, skills, hooks), hook variant key, and extension rules
- Frontmatter model key, tools key, static `add` fields, `drop` list, and key order
- Injection content (harness-level injection text)
- Model catalog and format hint

**Requires module code (imperative, by exception):**
- Tool output format: any serialization that is not a plain KindList — comma-separated scalar,
  flow-style single-quoted, permission mapping
- Skill key subdirectory: path composition with an intermediate key segment
- Conditional frontmatter: any rule that inspects request fields at transform time
- Placeholder expansion shape override: when the output shape differs from a flat list

**Conclusion:** A purely descriptor-driven harness (no module code beyond the adapter) is
achievable when the tool output is a standard KindList (block-style YAML sequence), skills are
deployed to a flat directory (no key subdirectory), frontmatter `add` fields are static, and
the `{tool-permissions}` placeholder expands to a flat list. None of the four current built-ins
satisfies all four conditions simultaneously. The skill key subdirectory and the tool output
format serialization are the two most common reasons a harness needs module code. A future
descriptor schema version could address both by adding a `skill.key_subdir: true` flag and a
`tools.list_style` and `tools.item_quote` pair to the tool spec.

When a harness's behaviour cannot be expressed declaratively, implement it as a module in
the `harness/builtin/{id}` package and register it with `harness/registry.Register`.
