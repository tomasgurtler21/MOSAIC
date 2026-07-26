# MOSAIC Generic Source Format Reference

This document describes the conventions for MOSAIC generic source files:
agent instruction files, skill files, and hook bundle manifests. It is the
authoritative reference for the fields added in Stage 2 and the version bump
rules that govern them.

---

## Agent Frontmatter Fields

Every generic agent file (`Agents/Generic/Agents/**/*.md`), the orchestrator
(`Agents/Generic/Orchestrator/orchestrator.md`), and every generic utility
agent (`Agents/Generic/UtilityAgents/*.md`) carries the following frontmatter.

### Standard identity fields (pre-existing)

| Field | Type | Description |
|-------|------|-------------|
| `id` | integer string | Numeric agent identifier for round-tripping. Not present on utility agents or the orchestrator. |
| `version` | semver string | Agent version. Bumped on any change to identity or body content. |
| `name` | string | Agent slug (matches file base name). |
| `description` | string | One-line description shown to users. |
| `model` | string | Model placeholder (`{model-identifier}`) or a concrete model id in a deployed file. |
| `tools` | flow-list or placeholder | Generic tool vocabulary (`{tool-permissions}` for the orchestrator). |

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

## Scope Note: Placeholder Hook Content

No functional hook scripts ship as part of this project. The
`subagent-logger` bundle exists to establish the deployment structure and
allow Stages 7, 16, and 17 to build and test hook discovery, staleness
detection, deployment, and registration logic against a real source tree.
Authoring the actual capture scripts is follow-up work.
