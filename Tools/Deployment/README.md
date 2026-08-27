# mosaic-deploy

`mosaic-deploy` transforms and installs MOSAIC generic agent definitions into a
target AI harness workspace. It handles several operations:

- **Deploy** — create a new workspace from scratch by selecting agents, skills,
  workflows, and model assignments, then writing every file in one pass.
- **Update** — bring an existing workspace up to date with the latest agent
  versions, preserving locally-modified files as you choose.
- **Utility/infrastructure only** — deploy only Utility and Infrastructure
  agents and their required skills, without touching workflows, hooks, or
  orchestrator configuration.

The tool reads from the MOSAIC source tree (generic agents, skills, hook bundles,
workflow definitions) and writes harness-specific files to a workspace you own.
It never modifies MOSAIC sources.

---

## Contents

- [Installation](#installation)
- [First run](#first-run)
- [The main flows](#the-main-flows)
  - [Deploy — create a new workspace](#deploy--create-a-new-workspace)
  - [Update — bring a workspace up to date](#update--bring-a-workspace-up-to-date)
  - [Utility/infrastructure only — deploy a subset without workflows](#utilityinfrastructure-only--deploy-a-subset-without-workflows)
  - [Runner deployment](#runner-deployment)
- [Harness-only agents](#harness-only-agents)
  - [How Update detects harness-only agents](#how-update-detects-harness-only-agents)
  - [Refresh-scope prompt](#refresh-scope-prompt)
  - [What is never touched](#what-is-never-touched)
  - [Version staleness](#version-staleness)
- [Promote — generate a generic agent from a harness-only file](#promote--generate-a-generic-agent-from-a-harness-only-file)
- [Config file reference](#config-file-reference)
  - [tool-config.yaml](#tool-configyaml)
  - [user-config.yaml](#user-configyaml)
- [Runtime layout](#runtime-layout)
- [Harness modules](#harness-modules)
- [Build from source](#build-from-source)
- [Import-boundary guard](#import-boundary-guard)
- [Relationship to boundary_transformer.py and boundary_validator.py](#relationship-to-boundary_transformerpy-and-boundary_validatorpy)
- [Troubleshooting](#troubleshooting)

---

## Installation

### Option 1 — Install from the MOSAIC repository (recommended)

Run the following from `Tools/Deployment/` (requires [Task](https://taskfile.dev)
and Go 1.22+):

```sh
task install
```

This compiles the binary for your current platform and writes it to the MOSAIC
root directory. It also creates the `MosaicDeploy/` runtime tree and seeds it
with default config files if they are not already present. Existing config
files are left untouched.

After install, the MOSAIC root contains:

```
MOSAIC/
  mosaic-deploy[.exe]           # the binary
  MosaicDeploy/
    config/
      tool-config.yaml          # project-level config (commit this)
      user-config.yaml          # per-user config (do NOT commit)
    harnesses/                  # optional runtime harness modules
    logs/
      latest.log                # most recent run
      history.log               # all runs, newest first
    fallback/                   # second-choice deployment target
```

### Option 2 — Download a pre-built binary

Download the binary for your platform from the release page and place it in
the MOSAIC root directory. Then create the `MosaicDeploy/` tree manually or
run `task install:config` from `Tools/Deployment/`.

### Prerequisites

- Go 1.22 or later (only for building; the binary itself has no runtime dependency)
- [Task](https://taskfile.dev) (only if using the Taskfile)
- The MOSAIC repository cloned locally

---

## First run

From the MOSAIC root, with the binary installed:

```sh
# Interactive TUI (requires a terminal)
./mosaic-deploy

# CLI — deploy a new workspace
./mosaic-deploy deploy --harness claude-code --workspace /path/to/my-project --auto-confirm

# CLI — update an existing workspace
./mosaic-deploy update --harness claude-code --workspace /path/to/my-project --auto-confirm

# CLI — deploy only utility and infrastructure agents
./mosaic-deploy utility-infra --harness claude-code --workspace /path/to/my-project --auto-confirm
```

On Windows, use `.\mosaic-deploy.exe` instead of `./mosaic-deploy`.

When you launch the TUI (no subcommand), a guided workflow walks you through
harness selection, workspace path, agent and workflow selection, and model
assignment. Selections made during a TUI run are persisted in `user-config.yaml`
so repeated runs are faster.

---

## The main flows

### Deploy — create a new workspace

The deploy flow creates a new workspace. It:

1. Asks you to select a harness (claude-code, opencode, ghcp-cli, etc.)
2. Asks for a workspace path (must exist or be creatable)
3. Lets you select workflows to include in the orchestrator
4. Lets you select utility agents
5. Maps tier-level model preferences
6. Shows a plan for review before writing any file
7. Writes every agent, skill, and hook file

After deploy, the workspace contains harness-specific agent files ready to use.
A `TODO.md` checklist is written to the workspace root listing any items that
need manual attention (empty injection sections, unmapped tools, hook registration steps).

**CLI example:**

```sh
./mosaic-deploy deploy \
  --harness claude-code \
  --workspace /path/to/my-project \
  --scope project \
  --selections selections.yaml \
  --auto-confirm
```

**Selections file (`selections.yaml`):**

```yaml
workflows:
  - quick-fix
  - build
utility_agents:
  - code-reviewer
tier_models:
  HIGH: claude-opus-4-5
  LOW: claude-haiku-3-5
```

Pass `--dry-run` to see the plan without writing any files.

### Update — bring a workspace up to date

The update flow updates an existing workspace to the latest agent versions. It:

1. Reads the deployment manifest (written by the previous deploy/update)
2. Computes which files are stale (version, transform_version, or injections_version changed)
3. Identifies files that have been locally modified since deployment
4. Shows a plan for review
5. Applies updates, using your chosen conflict resolution strategy for modified files

**Conflict resolution (`--conflict`):**

| Value | Behaviour |
|-------|-----------|
| `skip` (default) | Leave the modified file unchanged; add it to the TODO checklist |
| `overwrite` | Replace with the latest version (local edits are lost) |
| `backup` | Copy the local file to `<name>.backup` before overwriting |

**CLI example:**

```sh
./mosaic-deploy update \
  --harness claude-code \
  --workspace /path/to/my-project \
  --conflict backup \
  --auto-confirm
```

### Utility/infrastructure only — deploy a subset without workflows

The utility-infra flow deploys only Utility and Infrastructure agents and the skills they require. It never asks about workflows, hooks, or the orchestrator. Use it when you want to add or refresh these agent groups in a workspace that already has its orchestrator and workflows configured.

The flow:

1. Asks you to select utility agents (or pre-answer with `--utility`)
2. Asks you to select infrastructure agents (or pre-answer with `--infrastructure`)
3. Resolves models for infrastructure agents using the same two-batch logic as the full deploy, including the skip/gap path
4. Shows a plan for review
5. Writes only the selected agents and the skills they require

Workflow-driven agents, the orchestrator, and hook bundles are not touched.

**CLI example:**

```sh
# Ask interactively which agents to deploy.
./mosaic-deploy utility-infra \
  --harness claude-code \
  --workspace /path/to/my-project

# Non-interactive: pre-answer both selections.
./mosaic-deploy utility-infra \
  --harness claude-code \
  --workspace /path/to/my-project \
  --utility "code-reviewer,test-runner" \
  --infrastructure "docker-manager" \
  --auto-confirm
```

Pass `--dry-run` to see the plan without writing any files. See the [CLI Reference](docs/cli.md#utility-infra-subcommand) for the full flag list and nil/empty pre-answer convention.

### Runner deployment

The MOSAIC Runner (`mosaic-run`) executes workflows by invoking agents via the
harness CLI (e.g., `opencode run --agent <id>`). Some harnesses block CLI
invocation of agents deployed with restrictive frontmatter — for example,
OpenCode refuses to run agents marked `mode: subagent` from the command line
because that mode is reserved for agents spawned as child tasks by another agent.

Runner deployment produces a **second parallel directory** of agent files with
Runner-compatible transformations applied. Both the deploy and update flows
support this via the `--runner` flag:

```sh
# First deploy — opt in to Runner support.
./mosaic-deploy deploy \
  --harness opencode \
  --workspace /path/to/my-project \
  --runner \
  --selections selections.yaml \
  --auto-confirm

# Subsequent updates — auto-detected (no --runner needed).
./mosaic-deploy update \
  --harness opencode \
  --workspace /path/to/my-project \
  --auto-confirm
```

**What changes:**

| | Regular directory | Runner directory |
|---|---|---|
| **Path** | `.opencode/agents/` | `.opencode/agents-runner/` |
| **Subagents** | Standard frontmatter (e.g., `mode: subagent`) | Runner-compatible frontmatter (e.g., `mode: primary`) |
| **Orchestrator** | Regular orchestrator | Script-mode orchestrator only |

The regular directory is unchanged — interactive orchestration works exactly as
before. The runner directory is invisible to the harness's interactive agent
selection because harnesses only discover agents in their standard directory.

**Auto-detection on update:** Once a runner directory exists in the workspace,
the deploy tool automatically includes it in every subsequent deploy and update
run. No repeated `--runner` flag is needed — directory presence is the opt-in
signal. This prevents stale runner agents when you forget the flag.

**Known Runner-specific transformations:**

| Harness | Field | Regular | Runner | Why |
|---------|-------|---------|--------|-----|
| OpenCode | `mode` | `subagent` | `primary` | `mode: subagent` blocks CLI invocation via `opencode run` |

This set is expected to grow as new harness-specific constraints are discovered.

---

## Harness-only agents

A *harness-only agent* is an agent file that lives in the workspace's
deployed-agents directory but has no counterpart in `Catalog/` — it was
authored specifically for one harness without a generic source backing it. The
Update flow detects and refreshes these agents automatically, without any
additional flag or configuration.

Harness-only **orchestrators** are out of scope. A file named `orchestrator.md`
or `orchestrator.agent.md` (matched case-insensitively by filename) is never
treated as a harness-only agent by the Update flow, regardless of its contents.

### How Update detects harness-only agents

Before running the conflict loop, the Update flow scans the active harness's
deployed-agents directory. A file is recognised as a harness-only agent only when
**both** detection signals are present:

1. **`transform_version` in frontmatter.** This field is stamped by
   `Tools/OldAgentsTransform/boundary_transformer.py` on every file it
   processes. A hand-authored file that was never run through that tool lacks
   this field and is left completely untouched.

2. **A structurally valid set of canonical boundary tags.** The body must
   contain at least one `<Name type="core">` tag whose name is in the MOSAIC
   canonical vocabulary, and the document must pass structural validation
   (no unbalanced tags, no mismatched tags, no unknown `<Name type="managed">`
   names, no duplicate boundary names).

A file that fails either signal is skipped silently and receives no treatment
during the run. "Untouched" is an assertable property of the run, not an
implication.

**Prerequisite:** a hand-authored agent file must be run through
`Tools/OldAgentsTransform/boundary_transformer.py` before `mosaic-deploy update`
will recognise and maintain it. The transform stamps `transform_version` and
adds the boundary tag structure that satisfies signal two. Without that step the
file is invisible to the Update flow.

### Refresh-scope prompt

When one or more harness-only agents are found, the Update flow asks for each
how much of its content to regenerate. Two options are presented:

| Option | What it regenerates |
|--------|---------------------|
| **Refresh CommunicationProtocol only** | Only the `<CommunicationProtocol type="managed">` region |
| **Refresh all tool-managed DEPLOYED regions** | Every canonical `<Name type="managed">` region in the MOSAIC vocabulary |

An "Apply to all" variant of each option applies the chosen scope to every
remaining harness-only agent in the run without asking again, matching the
conflict loop's apply-to-all behaviour.

When the question is cancelled, skipped, or otherwise not answered, the tool
defaults to **Refresh CommunicationProtocol only**. The scope is never widened
without an explicit affirmative answer.

The prompt fires at most once per harness-only agent (or once total when the
apply-to-all option is used). It is suppressed entirely when no eligible
harness-only agents are found in the scan.

### What is never touched

For harness-only agents the tool owns only the regions it regenerates. Every
other byte in the file is left unchanged:

- **`<Name type="project">` content is preserved verbatim.** Injection regions
  are never merged, compared, diffed, or replaced. There is no generic source
  to validate them against, so they are carried through byte-identical. This is
  a hard guarantee, not a best effort.
- **`<Name type="core">` tag structure and section body prose are not
  reformatted or validated.** Without a generic counterpart there is nothing to
  align section structure against, so section tags, their order, and all content
  between them are left exactly as they are under every scope.
- **`<Name type="managed">` regions that are not in scope** are preserved
  byte-identical. Only the regions the chosen scope names are rewritten.
- **Frontmatter is not touched at all.** No field is added, removed, reordered,
  or restamped: no `version`, `protocol_version`, `bundle_version`, or other
  stamp is written for harness-only agents. Stamping implies a catalog source
  version to stamp from; harness-only agents have none, and writing a stamp would
  make the file appear catalog-backed on the next run.
- **No manifest entry is written.** Harness-only agents remain invisible to the
  deployment manifest. Detection stays purely the two-signal rule described
  above.

### Version staleness

Harness-only agents are always eligible for refresh regardless of whether their
frontmatter carries a `version` field. The Update flow never performs a staleness
comparison for these agents: they bypass the planner that compares deployed
versions against catalog source versions. The user's explicit scope answer (or
the default when the question goes unanswered) is the sole gate on whether the
file is written.

---

## Promote — generate a generic agent from a harness-only file

The `promote` command produces a generic source file from a harness-only agent,
so that a one-off agent can be shared across harnesses through the normal
Deploy / Update flow. It is a separate, opt-in command and is never triggered
by an Update run.

### Eligibility

The source file must satisfy the same two-signal detection rule the Update flow
uses: `transform_version` in frontmatter and a structurally valid set of
canonical boundary tags. An ineligible file is rejected with an error; nothing
is written.

### What promote generates

The generated generic file:

- **Strips every `<Name type="project">` region to an empty tag pair.** No
  harness-specific injection content is carried over. Generic source files use
  empty injection placeholders, which `mosaic-deploy` fills with
  project-specific content at deploy time.
- **Strips every `<Name type="managed">` region to an empty tag pair.** This is
  required: the deployment pipeline's `transform.Apply` rejects a source file
  whose deployed regions already carry content, so a generated file with filled
  regions would be unusable by the very pipeline it is generated for.
- **Carries over `<Name type="core">` tag structure and section body prose
  from the source as-is.**
- **Writes frontmatter per a defined field policy:**
  - `id` — assigned automatically as one greater than the largest numeric id
    currently in the catalog.
  - `version` — taken from the source file's `version` field, or `"1.0.0"`
    when the field is absent (common in hand-authored harness-only agents).
  - `name` — taken from the source `name` field when present; otherwise derived
    from the output filename.
  - `role` — taken from the source `role` field.
  - All other source fields not in the drop set are carried over verbatim (e.g.
    `description`, `recommended_tier`, `tools`, `required_skills`).
  - Deployment and transform stamps are **dropped**: `transform_version`,
    `injections_version`, `bundle_version`, `protocol_version`,
    `tool_mappings_version`, `model`. These are per-deployment artifacts that
    have no place in a generic source. See "The `tool_mappings_version` stamp"
    in `docs/configuration.md` for what that particular field is and where it
    comes from.

The source (harness-only) file is **never modified or deleted**.

### Target category

The target category is required and is never inferred from the source file.
The tool asks interactively when `--category` is absent. Available categories
are derived from the subdirectories present under `Catalog/Subagents/`, plus
a special `UtilityAgents` option that places the file under
`Catalog/UtilityAgents/`.

Registration is automatic: writing a well-formed file into the chosen directory
IS the registration — the catalog discovers agents by scanning those directories.
No catalog file is edited.

### Collision policy

When a file already exists at the destination path, promote refuses to overwrite
it unless `--overwrite` is given explicitly. There is no silent overwrite and no
automatic renaming.

### Deploying after promote

Promote writes the generic file only. Deploying that agent out to a harness
workspace is a separate, user-initiated step:

```sh
./mosaic-deploy update --harness <harness> --workspace /path/to/workspace
```

### CLI

```
mosaic-deploy promote --file <path> [--category <category>] [--overwrite] [--dry-run] [--output json]
```

| Flag | Description |
|------|-------------|
| `--file <path>` | Path to a single harness-only agent file to promote. Must be an existing regular file, not a directory. Required. |
| `--category <name>` | Destination category. When absent, the tool asks interactively. |
| `--overwrite` | Replace an existing generic file at the destination path. |
| `--dry-run` | Validate and compute everything; write no file. |
| `--output json` | Emit the result as a single JSON document instead of human-readable text. |

Exit codes: `0` on success (or dry-run validation pass), `1` on any service
error (source path does not exist, source path is a directory rather than a
regular file, ineligible source, refused collision, category not provided),
`3` on a missing `--file` flag or unparseable flag value.

### Interactive (TUI)

The TUI mode screen includes a **Promote** option. When promote is selected,
the path screen asks for the harness-only agent **file** path (not a
directory). It validates the entry inline before advancing: a path that does
not exist or that points to a directory is rejected with an inline error
message, and the user stays on the path screen until a valid regular file path
is entered. This validation happens before any service call is made. Once a
valid file path is confirmed, the category question is presented interactively.
Both the CLI and TUI paths call the same `Service.Promote` method, so their
behaviour cannot diverge.

---

## Config file reference

### tool-config.yaml

**Location:** `MosaicDeploy/config/tool-config.yaml` (at MOSAIC root)

Project-level configuration. Commit this file to your MOSAIC repository so
all team members share the same settings.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `schema_version` | string | `"1"` | Config schema version. Do not change. |
| `utility_agent_allow_list` | list of strings | `[]` | Keys of utility agents that may be selected during deployment. An empty list means no utility agents are offered. |
| `allow_external_modules` | bool | `false` | Whether external harness modules in `MosaicDeploy/harnesses/` may be used. Must be `true` for the external harness module tier to work. |
| `log_retention_runs` | int | `0` | Maximum history log entries to keep. `0` means keep all. |
| `tool_destinations` | map | `{}` | Generic-tool → harness-tool mappings, keyed by harness id. Declaring a tool here permanently answers the "custom tool" prompt for it. See [docs/configuration.md](docs/configuration.md#tool_destinations). |

**Example:**

```yaml
schema_version: "1"
utility_agent_allow_list:
  - code-reviewer
  - documentation-writer
allow_external_modules: false
log_retention_runs: 50
tool_destinations:
  claude-code:
    - generic: knowledge_base
      destinations:
        - to: main
          names: ["mcp__mosaic-kb__search"]
```

> **Note on defaults:** when this file is absent entirely, all six shipped
> utility agents are enabled by default. Once the file exists, its values are
> authoritative — `utility_agent_allow_list: []` really does mean "none".

### user-config.yaml

**Location:** `MosaicDeploy/config/user-config.yaml` (at MOSAIC root)

Per-user configuration. Do NOT commit this file. The MOSAIC root `.gitignore`
already excludes the entire `MosaicDeploy/` tree.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `schema_version` | string | `"1"` | Config schema version. Do not change. |
| `tier_models` | map | `{}` | Recorded tier-to-model mappings. Structure: `harness-id -> tier-name -> model-id`. Written automatically during deployment. |
| `custom_model_ids` | map | `{}` | Model IDs you typed by hand, per harness. Offered as selectable options (never pre-answers) in later runs. Append-only, deduplicated. |
| `tool_destinations` | map | `{}` | Personal generic-tool → harness-tool mappings. Same syntax as the `tool-config.yaml` key; takes precedence over it. See [docs/configuration.md](docs/configuration.md#tool_destinations). |

**Example (after a deployment run):**

```yaml
schema_version: "1"
tier_models:
  claude-code:
    HIGH: claude-opus-4-5
    LOW: claude-haiku-3-5
  opencode:
    HIGH: gpt-4o
    LOW: gpt-4o-mini
custom_model_ids:
  claude-code:
    - claude-sonnet-4-5-20250929
tool_destinations:
  claude-code:
    - generic: user_interaction
      destinations:
        - to: main
          names: ["mcp__user-feedback__ask_user_questions"]
```

> **The tool rewrites this file** when it records a selection. All keys survive,
> but YAML comments you add do not. Keep notes elsewhere.

For the full key-by-key reference — including `tool_destinations` syntax,
precedence, validation rules, and worked examples — see
[docs/configuration.md](docs/configuration.md).

---

## Runtime layout

After `task install`, the MOSAIC root contains:

```
MOSAIC/
  mosaic-deploy[.exe]           # the installed binary
  MosaicDeploy/
    config/
      tool-config.yaml          # project-level config (utility allow-list, external opt-in, tool mappings)
      user-config.yaml          # per-user config (tier-to-model mappings, custom models, tool mappings)
    harnesses/                  # optional runtime harness modules (drop-in executables)
    logs/
      latest.log                # full log of the most recent run
      history.log               # all run summaries, newest first
    fallback/                   # fallback deployment location when workspace is unavailable
```

`MosaicDeploy/` is entirely git-ignored. The only tracked item is the source
tree under `Tools/Deployment/`, which includes the default config templates.

The `harnesses/` directory is where external harness modules are discovered
when `allow_external_modules: true`. Each module is a subdirectory containing
an executable and a `harness.yaml` descriptor. See [docs/harness-contributor-guide.md](docs/harness-contributor-guide.md).

---

## Harness modules

Four built-in harnesses are compiled into the binary:

| ID | Display name |
|----|--------------|
| `claude-code` | Claude Code |
| `opencode` | OpenCode |
| `ghcp-cli` | GitHub Copilot CLI |
| `vscode-ghcp` | VS Code GitHub Copilot |

To add a harness without writing Go code, create a descriptor YAML file and
pass it with `--harness /path/to/harness.yaml`. See [docs/descriptor-schema.md](docs/descriptor-schema.md).

To build a fully custom harness, implement the external module protocol. See
[docs/harness-contributor-guide.md](docs/harness-contributor-guide.md) and
[docs/external-protocol.md](docs/external-protocol.md).

---

## Build from source

### Build and install (current platform)

```sh
cd Tools/Deployment
task install
```

### Cross-compile for all platforms

```sh
cd Tools/Deployment
task build:all
```

Outputs:
- `Tools/Deployment/dist/mosaic-deploy.exe` — Windows (amd64)
- `Tools/Deployment/dist/mosaic-deploy-linux-amd64` — Linux (amd64)

Both binaries are statically linked with no external runtime dependency
(`CGO_ENABLED=0`).

### Verify static linking

```sh
cd Tools/Deployment
task verify:static:windows
task verify:static:linux
```

### Run tests

```sh
cd Tools/Deployment
task test
```

### Run the end-to-end verification

The end-to-end verification (build → install → deploy → update → locally-modified-file) is the
release gate. Run it before publishing a new binary:

```sh
cd Tools/Deployment
task verify:e2e HARNESS=claude-code
```

---

## Import-boundary guard

The `tools/importcheck` program enforces the module's dependency direction at
build time. Run it with:

```sh
cd Tools/Deployment
task check:imports
```

Or as part of the full release-gate build:

```sh
task build-checked
```

**Rules checked:**

| Package | Rule |
|---------|------|
| `internal/domain` | May not import any package in this module |
| `internal/transform` | May not import `os`, `io/fs`, `net`, `time`, `math/rand`, `crypto/rand`, or any I/O package |
| `internal/app` | May not import `internal/tui` or `internal/cli` |
| any package | May not import `internal/harness/builtin/*` packages directly (use the registry) |

To verify that the check actually catches violations, introduce one deliberately
and confirm the tool fails:

```sh
# Example: add `import "os"` to internal/transform/transform.go
echo 'import _ "os"' >> internal/transform/transform.go
task check:imports   # should exit 1
git checkout internal/transform/transform.go  # restore
```

---

## Relationship to boundary_transformer.py and boundary_validator.py

`Tools/OldAgentsTransform/boundary_transformer.py` and
`Tools/OldAgentsTransform/boundary_validator.py` are separate tools that perform
a different, completed task: a one-shot structural retrofit of MOSAIC generic
source files to add `<Name type="core">` and `<Name type="project">` boundary tags. They
are not part of the deployment pipeline and are not invoked by `mosaic-deploy`.
See [Tools/OldAgentsTransform/README.md](../OldAgentsTransform/README.md).

`mosaic-deploy` reads generic source files that already have the correct
boundary structure and transforms them into harness-specific deployed files.
It introduces a third boundary kind, `<Name type="managed">`, for regions it
regenerates on every deploy (harness constraints, language patterns, the
communication protocol, and similar tool-managed content). These are distinct
from `<Name type="project">` regions, which belong to the user and are preserved
across updates.

For the generic source-file format reference (fields, boundary conventions,
version bump rules), see [Catalog/SourceFilesFormat.md](../../Catalog/SourceFilesFormat.md).

---

## Troubleshooting

### "not a MOSAIC repository"

The tool could not find the MOSAIC repository root (the directory containing
`Catalog/`). Run from inside the MOSAIC repository, or pass the root
explicitly:

```sh
./mosaic-deploy --mosaic-root /path/to/MOSAIC deploy ...
```

### "harness not found"

The harness ID you specified is not recognised. Check the available harnesses:

```sh
./mosaic-deploy deploy --help
```

For a custom descriptor file, pass the full path:

```sh
./mosaic-deploy deploy --harness /path/to/my-harness.yaml ...
```

### "external harness modules require explicit opt-in"

External harness modules are disabled by default for security. To enable them,
set `allow_external_modules: true` in `MosaicDeploy/config/tool-config.yaml`,
or pass `--allow-external` on the command line.

### The binary has a dynamic dependency

If `task verify:static:linux` reports dynamic library references, CGO was
accidentally enabled. Rebuild with:

```sh
CGO_ENABLED=0 go build ./cmd/mosaic-deploy
```

Or use `task build:linux` which sets `CGO_ENABLED=0` explicitly.

### A config file is not being picked up

The tool looks for `MosaicDeploy/config/` under the MOSAIC root. If you moved
the binary, ensure the MOSAIC root is set correctly with `--mosaic-root`.

### Logs

Run logs are written to:

- `MosaicDeploy/logs/latest.log` — full log of the most recent run
- `MosaicDeploy/logs/history.log` — summary of all runs, newest first

The log directory is created on first run if it does not exist.

To write logs to a different directory for one invocation, pass `--log-dir`:

```sh
./mosaic-deploy --log-dir /path/to/my-logs deploy ...
```

Both flag forms are accepted:

```sh
./mosaic-deploy --log-dir /path/to/my-logs deploy ...
./mosaic-deploy --log-dir=/path/to/my-logs deploy ...
```

When `--log-dir` is supplied, the two sink files are written there and the
default `MosaicDeploy/logs` location is not created or written to. The
supplied directory is created on demand. Write failures accumulate as
degradation rather than stopping the run, consistent with the default location.
Omitting `--log-dir` preserves the existing behaviour exactly.

### TODO file

After deploy or update, a `TODO.md` file is written to the workspace root.
It lists every item that needs manual attention: model selections that were
skipped, unmapped tools, injection sections that need project-specific content,
hook registration steps, and skipped files. Work through the checklist before
using the workspace.
