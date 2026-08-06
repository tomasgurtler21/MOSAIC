# MOSAIC Generic Source Format Reference

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

Every generic agent file (`Agents/Generic/Agents/**/*.md`), the orchestrator
(`Agents/Generic/Orchestrator/orchestrator.md`), and every generic utility
agent (`Agents/Generic/UtilityAgents/*.md`) carries the following frontmatter.

Utility agents carry frontmatter only. They have no boundary tags, are never
deployed into a run, and everything below about regions and canonical order
does not apply to them.

### Standard identity fields (pre-existing)

| Field | Type | Description |
|-------|------|-------------|
| `id` | integer string | Numeric agent identifier for round-tripping. Not present on utility agents or the orchestrator. |
| `version` | semver string | Agent version. Bumped on any change to identity or hand-authored body content. Tiers in `AgentTemplateArchitecture.md` §3.4. Content arriving in a `[[DEPLOYED:]]` region never bumps it. |
| `name` | string | Agent slug (matches file base name). |
| `description` | string | One-line description shown to users. |
| `role` | enum | `subagent` or `orchestrator`. Declares what the agent is; selects which canonical text it receives. Not `utility` — utility agents are outside the schema. |
| `model` | string | Model placeholder (`{model-identifier}`) or a concrete model id in a deployed file. |
| `tools` | flow-list or placeholder | Generic tool vocabulary (`{tool-permissions}` for the orchestrator). |

A deployed agent file additionally carries `bundle_version`, written by the
deployment tool. It is not a source field.

> **Not yet read by the tool.** `role` is specified but role is still inferred
> from the file's path in `domain.AgentRole`, whose enum reads
> `worker`/`orchestrator`/`utility`. The frontmatter vocabulary is `subagent`;
> the code should follow or map explicitly at the boundary.

### Deployment metadata fields (added Stage 2)

| Field | Type | Description |
|-------|------|-------------|
| `recommended_tier` | string | The tier token verbatim from the former `model:` line comment (e.g. `MEDIUM`, `HIGH`, `MEDIUM-HIGH`). Open string — no fixed vocabulary. |
| `tier_rationale` | string | Explanatory text describing why the tier was chosen. Shown to the user during model selection. |
| `required_skills` | flow-list | Skill keys this agent's instructions tell it to load. Empty list (`[]`) when the agent loads no skills. Values are folder names under `Agents/Generic/Skills/`. |

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

Every generic skill entry file (`Agents/Generic/Skills/*/SKILL.md`) carries:

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

Hook bundles live under `Agents/Generic/Hooks/<bundle-id>/`. Each bundle
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

Agent source files use three boundary markers. The marker in the file states
who owns the region, so no external lookup into code or documentation is
needed to understand which regions the tool will touch.

### Marker kinds

| Marker | Written by | Behaviour on deploy / update |
|--------|-----------|------------------------------|
| `[[SECTION:Name]]` … `[[/SECTION:Name]]` | MOSAIC source authors | Content carried byte-identically from the source on every deploy |
| `[[DEPLOYED:Name]]` … `[[/DEPLOYED:Name]]` | The deploy tool | Regenerated by the tool on every deploy; do not author content inside these |
| `[[INJECTION:Name]]` … `[[/INJECTION:Name]]` | Users (project authors) | Preserved byte-identically across updates; left empty on first deploy |

A tag line matches when the trimmed line is exactly `[[` + marker body + `]]`.
Each boundary name may appear at most once per file. `INJECTION` and `DEPLOYED`
regions may be nested inside a `SECTION` or appear at body top level.

**`DEPLOYED` names are a closed set; `INJECTION` names are open.** The tool must
find content for a deployed region, so a name it does not recognise is an error.
An injection region is preserved by matching its name between the deployed file
and the source file, which needs no list — so any injection name is valid and is
preserved. See `AgentTemplateArchitecture.md` §6.1.

### Tool-managed names — declare with `[[DEPLOYED:]]`

The deploy tool writes and regenerates these regions on every deploy. Do not
place user-authored content inside them; it will be overwritten.

| Name | Required parent | Content source |
|------|-----------------|----------------|
| `CommunicationProtocol` | Body top level (second canonical slot) | `CommunicationProtocol.md` |
| `AuthorityHierarchy` | `Identity` | Bundle |
| `ClosingProcedure` | `Identity` | Bundle |
| `AvailableWorkflows` | `Identity` | Assembled from selected workflows |
| `InfrastructureAgents` | `Identity` | Assembled from selected declarations |
| `LanguagePatterns` | `Capabilities` | Deployment configuration |
| `ProtocolConstraints` | `Constraints` | Bundle |
| `HarnessConstraints` | `Constraints` | Selected harness module |
| `CustomConstraints` | `Constraints` | Deployment configuration |
| `ErrorHandlingCommon` | `ErrorHandling` | Bundle |
| `ExecutionPhilosophyCommon` | `ExecutionPhilosophy` | Bundle |

"Bundle" means `Agents/Generic/DeployedSections.md`.

> **Not yet read by the tool.** The five bundle-sourced names above are
> specified but the deployment tool does not read the bundle. Until it does,
> those regions have no content source and the agents are unmigrated.

#### What an absent deployed region costs

| Tier | Names | Absence is |
|------|-------|-----------|
| Contract | `CommunicationProtocol` | Error |
| Conduct | `AuthorityHierarchy`, `ClosingProcedure`, `ProtocolConstraints`, `ErrorHandlingCommon`, `ExecutionPhilosophyCommon` | Warning |
| Deployment | `LanguagePatterns`, `HarnessConstraints`, `CustomConstraints`, `AvailableWorkflows`, `InfrastructureAgents` | Silent |

A region *present* with no content source for the file's role is always an error.

### User-owned names — declare with `[[INJECTION:]]`

Users author content in these regions. The deploy tool preserves them
byte-identically on every update, **whatever they are named**. On first deploy
they are left empty and listed in the `TODO.md` checklist. No injection is ever
required to be filled.

The names below are the ones MOSAIC's own agents carry and ships TODO guidance
for. They are a **suggestion, not an allowlist** — an unlisted name is valid and
preserved like any other.

| Name | Usual parent |
|------|--------------|
| `IdentityExtension` | `Identity` |
| `ProtocolExtension` | Body top level (sibling of `CommunicationProtocol`) |
| `CodebaseContext` | `Capabilities` |
| `OutputArtifactTemplate` | `Capabilities` |
| `SeverityThresholds` | `Capabilities` |
| `SeverityDefinitions` | `Capabilities` |
| `ErrorHandlingExtension` | `ErrorHandling` |
| `ContextLimits` | `ExecutionPhilosophy` |

`ArtifactProvenanceExtension` is retired: the stamp it extended folded into the
orchestration contract. A file still carrying it is stale, not invalid, and its
content is preserved.

The one hard rule about placement: **an injection is never nested inside a
`[[DEPLOYED:]]` region**, which is regenerated wholesale and would destroy it.
Everything else about placement is advice.

### Canonical document order

A source file's top-level boundaries must form a **subsequence** of this list —
sections may be absent, a file may add top-level sections of its own, and any
two of these that are both present must appear in this relative order:

1. `Identity` (section)
2. `CommunicationProtocol` (top-level deployed)
3. `Capabilities` (section)
4. `Constraints` (section)
5. `ErrorHandling` (section)
6. `OutputFormat` (section)
7. `ExecutionPhilosophy` (section)

---

## Scope Note: Placeholder Hook Content

No functional hook scripts ship as part of this project. The
`subagent-logger` bundle exists to establish the deployment structure and
allow Stages 7, 16, and 17 to build and test hook discovery, staleness
detection, deployment, and registration logic against a real source tree.
Authoring the actual capture scripts is follow-up work.
