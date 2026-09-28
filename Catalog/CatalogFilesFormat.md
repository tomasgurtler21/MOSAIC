# MOSAIC Catalog Files Format Reference

This document describes the conventions for MOSAIC generic source files:
agent instruction files, skill files, and hook bundle manifests.

**On agent files, this document is a tool-facing copy, not the authority.**
`Development/Designs/AgentTemplateArchitecture.md` specifies the agent file
schema — frontmatter, region kinds, canonical order, per-section content, and
the validator rules. This file restates the machine-readable parts of that
schema for readers working from the tool side, alongside
`Tools/Common/docformat/vocabulary.go` and `Tools/OldAgentsTransform/boundary_constants.py`.
**All three are copies and must be updated together**; where any of them
disagrees with the design document, the design document is right.

Unique to this document, and not specified anywhere else: skill frontmatter
fields, hook bundle structure, and their version bump rules.

---

## Agent Frontmatter Fields

Every generic agent file (`Catalog/Subagents/**/*.md`), both orchestrators
(`Catalog/Orchestrator/orchestrator.md` and
`Catalog/Orchestrator/orchestrator-script.md`), every generic utility agent
(`Catalog/UtilityAgents/*.md`), and every standalone agent
(`Catalog/StandaloneAgents/*.md`) carries the following frontmatter.

Utility and standalone agents receive harness transformation only — frontmatter
mapping, model substitution, tool name mapping. No bundle content is deployed
into them, no `mosaic_bundle_version` is stamped, and they are not checked for
canonical order or required regions. Whether such a file uses boundary tags or
follows the section structure below is its author's choice; the tool transforms
what it finds and does not report what is absent. See
`AgentTemplateArchitecture.md` §5A.

### Standard identity fields (pre-existing)

| Field | Type | Description |
|-------|------|-------------|
| `id` | integer string | Numeric agent identifier for round-tripping. Present only on `role: subagent` files — absent from the orchestrators, utility agents, and standalone agents. |
| `version` | semver string | Agent version. Bumped on any change to identity or hand-authored body content. Tiers in `AgentTemplateArchitecture.md` §3.4. Content arriving in a `managed` region never bumps it. |
| `name` | string | Agent slug (matches file base name). |
| `description` | string | One-line description shown to users. |
| `role` | enum | `subagent`, `orchestrator`, `utility`, or `standalone`. Declares what the agent is. `subagent` lives under `Catalog/Subagents/`, `orchestrator` under `Catalog/Orchestrator/`, `utility` under `Catalog/UtilityAgents/`, `standalone` under `Catalog/StandaloneAgents/`. The latter two are outside the agent-body schema and the bundle. |
| `model` | string | Model placeholder (`{model-identifier}`) or a concrete model id in a deployed file. |
| `tools` | flow-list or placeholder | Generic tool vocabulary (`{tool-permissions}` for the orchestrator). |
| `infrastructure` | enum | Infrastructure agents only (`Catalog/Subagents/Infrastructure/`). The agent's class: `checkpoint`, `commit`, `restore`, or `review`. |
| `triggers` | list | Infrastructure agents only. Default triggers, each `trigger` plus `trigger_param` (`null` where the trigger takes none). |
| `on_failure` | enum | Infrastructure agents only. Default failure policy: `halt` or `continue`. |

The three infrastructure fields are assembly defaults: the deployment tool reads them to build the orchestrator's `<InfrastructureAgents>` region, and nothing consults them at runtime. Harness descriptors drop them, so they do not appear in deployed agent files. Their meaning is owned by `Development/Designs/InfrastructureAgentConcept.md` §3.2.

### Generic tool vocabulary

The `tools` field lists capabilities from a **closed vocabulary** of generic
tool names. During deployment, the tool maps each generic name to one or more
harness-specific tool names via the harness descriptor's `tools.mappings`
block. The mapping is defined per harness; agent authors use only the generic
names.

| Generic name | Grants | Notes |
|---|---|---|
| `file_read` | Read files from the filesystem | |
| `file_write` | Create or overwrite files | |
| `file_edit` | Make targeted edits to existing files | |
| `file_search` | Find files by name or glob pattern | |
| `content_search` | Search file contents (grep / ripgrep) | |
| `terminal` | Execute shell commands | Grant only when the agent runs something |
| `subagent` | Launch or delegate to other agents | |
| `user_interaction` | Ask the user questions mid-execution | |
| `skill` | Load skill modules at runtime | |

**This vocabulary is closed.** If an agent needs a tool not listed here (e.g.
an MCP server), it becomes a custom tool mapping question at deploy time. The
mapping can be answered permanently in `tool-config.yaml` under
`tool_destinations` — see `Tools/Deployment/docs/configuration.md` for the
full reference.

The orchestrator uses the placeholder `{tool-permissions}` instead of listing
individual tools; the deployment tool expands it to the harness's full tool
set.

The authoritative copy of this vocabulary is `Tools/Deployment/docs/descriptor-schema.md`
§`tools.mappings`. This section restates it for agent authors; where the two
disagree, `descriptor-schema.md` is right.

A deployed agent file additionally carries MOSAIC bookkeeping fields written by
the deployment tool. These are not source fields and are not present in generic
source files under `Catalog/`.

All MOSAIC-only bookkeeping fields in deployed files carry a `mosaic_` prefix,
so a reader can distinguish MOSAIC bookkeeping from fields the harness runtime
actually consumes. The fields and their generic-source counterparts are defined
in `Tools/Deployment/internal/agentfields` — that package is the single source
of truth for the pairing.

| Deployed field | Generic source field | Written by |
|----------------|---------------------|------------|
| `mosaic_id` | `id` | deploy transform (rename of source `id`) |
| `mosaic_role` | `role` | deploy transform (rename of source `role`) |
| `mosaic_version` | `version` | deploy transform (rename of source `version`) |
| `mosaic_bundle_version` | — | deploy transform |
| `mosaic_harness_version` | — | harness descriptor |
| `mosaic_tool_mappings_version` | — | harness descriptor |

**Legacy names:** Deployed files produced before the current field layout may
carry unprefixed names (`bundle_version`, `version`, `role`) or former transform
and injection-version fields. Read sites accept those migration forms while
preferring the current prefixed field or managed-region version attribute. The
next update writes the current form.

### Deployment metadata fields (added Stage 2)

| Field | Type | Description |
|-------|------|-------------|
| `recommended_tier` | string | The tier token verbatim from the former `model:` line comment (e.g. `MEDIUM`, `HIGH`, `MEDIUM-HIGH`). Open string — no fixed vocabulary. |
| `tier_rationale` | string | Explanatory text describing why the tier was chosen. Shown to the user during model selection. |
| `required_skills` | flow-list | Skill keys this agent's instructions tell it to load. Empty list (`[]`) when the agent loads no skills. Values are folder names under `Catalog/Skills/`. |

#### Placement

New fields are appended after the last pre-existing frontmatter field, before
the closing `---`. Key order within the frontmatter block is significant only
for round-trip fidelity; the deployment tool respects source order for keys
it does not rewrite.

#### `recommended_tier` values

`recommended_tier` is an open string. The deployment tool presents it to the
user during model selection and uses it as the key for tier-to-model mappings.
It is never validated against a fixed enum. Examples in the current source:
`LOW`, `LOW-MEDIUM`, `MEDIUM`, `MEDIUM-HIGH`, `HIGH`.

#### `required_skills` derivation rule

`required_skills` lists only the skills the agent's own instruction body
explicitly names in a "Load ... Skill" step. Where an agent names no skill,
the field is an empty list. Skills are never inferred from context.

---

## Skill Frontmatter Fields

Every generic skill entry file (`Catalog/Skills/*/SKILL.md`) carries:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Skill identifier (matches folder name). |
| `version` | semver string | Skill version. See bump rule below. |
| `description` | string | One-line description. |

### Skill version bump rule

Bump the `version` field in `SKILL.md` whenever the skill's content changes in
a way that affects the guidance an agent receives. Patch bump (`x.y.Z`) for
clarifications and wording fixes. Minor bump (`x.Y.0`) for new guidance
sections or meaningfully expanded coverage. Major bump (`X.0.0`) for guidance
that changes previously recommended behaviour.

Initial version for skills that did not previously carry a version field is
`1.0.0`.

---

## Hook Bundle Structure

Hook bundles live under `Catalog/Hooks/<bundle-id>/`. Each bundle
contains a `hook.yaml` manifest and one sub-folder per harness variant.

### hook.yaml schema

```yaml
schema_version: "1"        # Schema version for this file format
id: <bundle-id>            # Matches the folder name
version: <semver>          # Bundle version; see bump rule below
description: <string>      # One-line description
placeholder: <bool>        # true while variant content is not yet authored

variants:
  <harness-id>:
    supported: <bool>       # false => this harness cannot use this hook
    files:                  # omit when supported: false
      - source: <filename>  # file name inside the variant folder
        target: <filename>  # filename at the deployment target
    reuses: <harness-id>    # adopt file list from named variant; omit own files entry
    registration:           # list of steps the tool must perform or report
      - id: <step-id>
        target_path: <path> # relative to workspace; empty string when tool cannot perform step
        performable: <bool> # false => always a TODO item (e.g. a user-level setting)
        instruction: <string>
        fragment: |         # exact content to write, or show to user if target already exists
          ...
```

#### Variant folders

Each supported harness variant has a sub-folder named `<harness-id>/`
containing its source files, unless it declares `reuses`. The `reuses` field
names the harness id whose file set this variant adopts; in that case the
variant folder may contain only documentation (e.g. a `README.md`) and no
deployable files.

Variant folders for unsupported harnesses (`supported: false`) need not exist.

#### placeholder flag

When `placeholder: true` is set in `hook.yaml`, the deployment tool marks all
files in the bundle as pending authoring and includes a notice in the TODO
checklist. Placeholder variant files must contain a comment or note stating
clearly that they are placeholders and that authoring is follow-up work.

### Hook bundle version bump rule

Bump the bundle `version` field whenever any file inside the bundle changes
(including `hook.yaml` itself). Patch bump for registration or configuration
changes that do not alter the hook's observable behaviour. Minor bump for new
functionality or new supported harnesses. Major bump for breaking changes to
the hook's event contract or file layout.

---

## Boundary Tag Conventions

Agent source files use four boundary tag types. A line is a MOSAIC boundary
only if the trimmed line is exactly an XML tag carrying a `type` attribute
whose value is one of `core`, `managed`, `project`, or `custom`. The `type`
value states who owns the region, so no external lookup into code or
documentation is needed to understand which regions the tool will touch.

### Tag kinds

| Open tag | Close tag | Written by | Behaviour on deploy / update |
|----------|-----------|-----------|------------------------------|
| `<Name type="core">` | `</Name>` | MOSAIC source authors | Content carried byte-identically from the source on every deploy |
| `<Name type="managed">` | `</Name>` | The deploy tool | Regenerated by the tool on every deploy; do not author content inside these |
| `<Name type="project">` | `</Name>` | MOSAIC source authors, then the adopting project | Usually empty in source; may carry deploy-once default content. Once deployed, project content is preserved byte-identically across updates |
| `<Name type="custom">` | `</Name>` | Project authors | Never in source; project-invented; preserved byte-identically across updates |

A tag occupies its own line in full — no self-closing tags, and a tag line
carries no other content before or after the tag. An empty region is an open
tag line followed immediately by a close tag line. A line that is not alone
on its own line, carries an unrecognised `type` value, or is self-closing is
inert text and is preserved byte-for-byte.

Each boundary name may appear at most once per file. `managed`, `project`,
and `custom` regions may be nested inside a `core` region or appear at body
top level.

**`managed` names are a closed set.** The tool must find content for a
managed region, so a name it does not recognise is an error.

**`project` vs `custom`:** Both hold project-authored content preserved
byte-identically. The difference is provenance: `project` regions are declared
in the source file and follow schema reorders automatically; `custom` regions
are project-invented, have no source anchor, and are parked at end of file on
schema reorder with a TODO for the user to reposition. See
`AgentTemplateArchitecture.md` §6.1.

### Compound names

An enumerable region puts its prefix in the tag name and its id in a `name`
attribute:

| Logical name | Open tag | Close tag |
|---|---|---|
| `Identity` | `<Identity type="core">` | `</Identity>` |
| `Workflow:quick-fix` | `<Workflow type="core" name="quick-fix">` | `</Workflow>` |
| `InfrastructureAgent:build-runner` | `<InfrastructureAgent type="core" name="build-runner">` | `</InfrastructureAgent>` |

Canonical attribute order is `type`, then `name` (when present), then
`version` (when present). Attribute values are double-quoted.

### Version attribute

Where a region carries a declared version, it appears as a `version`
attribute on the opening tag:

```
<CommunicationProtocol type="managed" version="1.10">
```

The attribute is optional; its absence is not an error. Regions that carried
no version before the migration do not gain one.

### Tool-managed names — declare with `<Name type="managed">`

The deploy tool writes and regenerates these regions on every deploy. Do not
place user-authored content inside them; it will be overwritten.

| Name | Required parent | Content source |
|------|-----------------|----------------|
| `CommunicationProtocol` | Body top level (second canonical slot) | `CommunicationProtocol.md` |
| `AuthorityHierarchy` | `Identity` | Bundle |
| `ClosingProcedure` | `Identity` | Bundle |
| `AvailableWorkflows` | `Identity` | Assembled from selected workflows |
| `InfrastructureAgents` | `Identity` | Assembled from selected declarations |
| `HarnessConstraints` | `Constraints` | Selected harness module |
| `ErrorHandlingCommon` | `ErrorHandling` | Bundle |
| `ExecutionPhilosophyCommon` | `ExecutionPhilosophy` | Bundle |

"Bundle" means `Catalog/DeployedSections.md`.

Eight names, and every one of them names a generator that exists. A name with
nothing to fill it does not belong here — see `AgentTemplateArchitecture.md`
§2.5.1. `LanguagePatterns` and `CustomConstraints` were listed here until
2026-08-08 with the source "Deployment configuration", which was never a real
mechanism. Neither is a MOSAIC region any longer: a project that wants
language patterns declares its own `type="custom"` region, and
`CustomConstraints` no longer exists.

#### What an absent managed region costs

| Tier | Names | Absence is |
|------|-------|-----------|
| Contract | `CommunicationProtocol` | Error |
| Conduct | `AuthorityHierarchy`, `ClosingProcedure`, `ErrorHandlingCommon`, `ExecutionPhilosophyCommon` | Warning |
| Deployment | `HarnessConstraints`, `AvailableWorkflows`, `InfrastructureAgents` | Silent |

A region *present* with no content source for the file's role is always an error.

### Source-declared names — declare with `<Name type="project">`

These are usually declared empty in MOSAIC's source files so projects can fill
them. A source region may instead carry default content: the deploy tool copies
that content on initial deployment, then treats the deployed region as project
owned and preserves it byte-identically on every update. Later changes to the
source default affect new deployments only. `SeverityThresholds` in validation
agents is the primary current example. On schema reorder, project regions follow
the source's new position automatically. No project-declared injection is ever
required to be filled.

| Name | Usual parent |
|------|--------------|
| `CodebaseContext` | `Capabilities` |
| `OutputArtifactTemplate` | `Capabilities` |
| `SeverityThresholds` | `Capabilities` |
| `SeverityDefinitions` | `Capabilities` |
| `ContextLimits` | `ExecutionPhilosophy` |

`ArtifactProvenanceExtension` is retired: the stamp it extended folded into the
orchestration contract. A file still carrying it is stale, not invalid, and its
content is preserved.

`ProtocolExtension` is not a catalogued project-declared injection name.
Projects needing to extend the protocol use
`<ProtocolExtension type="custom">` as a top-level sibling of the managed
contract region — `custom` regions are project-invented and need no source
declaration. See `AgentTemplateArchitecture.md` §6.2.1.

### Project-invented names — declare with `<Name type="custom">`

Projects may invent any name and add `<Name type="custom">` regions to their
deployed files. These are preserved byte-identically on update. On schema
reorder, custom regions with no surviving parent are parked at end of file
with a TODO to reposition.

`LanguagePatterns` is the most common example — not declared in any source
file because language patterns are meaningful only once a project has a
language.

`project` and `custom` regions **may be nested inside a `managed` region**.
The tool preserves nested user-owned regions when regenerating the managed
parent — it writes the new canonical text around them. This is the natural
placement for project extensions of managed content (e.g. custom error
handling inside `ErrorHandlingCommon`).

### Canonical document order

A source file's top-level boundaries must form a **subsequence** of this list —
sections may be absent, a file may add top-level sections of its own, and any
two of these that are both present must appear in this relative order:

1. `Identity` (core)
2. `CommunicationProtocol` (top-level managed)
3. `Capabilities` (core)
4. `Constraints` (core)
5. `ErrorHandling` (core)
6. `ExecutionPhilosophy` (core)

---

## Scope Note: Placeholder Hook Content

No functional hook scripts ship as part of this project. The
`subagent-logger` bundle exists to establish the deployment structure and
allow Stages 7, 16, and 17 to build and test hook discovery, staleness
detection, deployment, and registration logic against a real source tree.
Authoring the actual capture scripts is follow-up work.
