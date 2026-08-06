# mosaic-deploy

`mosaic-deploy` transforms and installs MOSAIC generic agent definitions into a
target AI harness workspace. It handles two operations:

- **Deploy** — create a new workspace from scratch by selecting agents, skills,
  workflows, and model assignments, then writing every file in one pass.
- **Update** — bring an existing workspace up to date with the latest agent
  versions, preserving locally-modified files as you choose.

The tool reads from the MOSAIC source tree (generic agents, skills, hook bundles,
workflow definitions) and writes harness-specific files to a workspace you own.
It never modifies MOSAIC sources.

---

## Contents

- [Installation](#installation)
- [First run](#first-run)
- [The two flows](#the-two-flows)
  - [Deploy — create a new workspace](#deploy--create-a-new-workspace)
  - [Update — bring a workspace up to date](#update--bring-a-workspace-up-to-date)
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
```

On Windows, use `.\mosaic-deploy.exe` instead of `./mosaic-deploy`.

When you launch the TUI (no subcommand), a guided workflow walks you through
harness selection, workspace path, agent and workflow selection, and model
assignment. Selections made during a TUI run are persisted in `user-config.yaml`
so repeated runs are faster.

---

## The two flows

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

**Example:**

```yaml
schema_version: "1"
utility_agent_allow_list:
  - code-reviewer
  - documentation-writer
allow_external_modules: false
log_retention_runs: 50
```

### user-config.yaml

**Location:** `MosaicDeploy/config/user-config.yaml` (at MOSAIC root)

Per-user configuration. Do NOT commit this file. The MOSAIC root `.gitignore`
already excludes the entire `MosaicDeploy/` tree.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `schema_version` | string | `"1"` | Config schema version. Do not change. |
| `tier_models` | map | `{}` | Recorded tier-to-model mappings. Structure: `harness-id -> tier-name -> model-id`. Written automatically during deployment. |

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
```

---

## Runtime layout

After `task install`, the MOSAIC root contains:

```
MOSAIC/
  mosaic-deploy[.exe]           # the installed binary
  MosaicDeploy/
    config/
      tool-config.yaml          # project-level config (utility allow-list, external opt-in)
      user-config.yaml          # per-user config (tier-to-model mappings)
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
source files to add `[[SECTION:...]]` and `[[INJECTION:...]]` boundary tags. They
are not part of the deployment pipeline and are not invoked by `mosaic-deploy`.
See [Tools/OldAgentsTransform/README.md](../OldAgentsTransform/README.md).

`mosaic-deploy` reads generic source files that already have the correct
boundary structure and transforms them into harness-specific deployed files.
It introduces a third marker kind, `[[DEPLOYED:Name]]`, for regions it
regenerates on every deploy (harness constraints, language patterns, the
communication protocol, and similar tool-managed content). These are distinct
from `[[INJECTION:Name]]` regions, which belong to the user and are preserved
across updates.

For the generic source-file format reference (fields, boundary conventions,
version bump rules), see [Agents/Generic/SourceFilesFormat.md](../../Agents/Generic/SourceFilesFormat.md).

---

## Troubleshooting

### "not a MOSAIC repository"

The tool could not find the MOSAIC repository root (the directory containing
`Agents/Generic/`). Run from inside the MOSAIC repository, or pass the root
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

### TODO file

After deploy or update, a `TODO.md` file is written to the workspace root.
It lists every item that needs manual attention: model selections that were
skipped, unmapped tools, injection sections that need project-specific content,
hook registration steps, and skipped files. Work through the checklist before
using the workspace.
