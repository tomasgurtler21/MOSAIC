# OpenCode External Harness Module — Deployment Guide

This guide shows how to build the `harness-opencode-module` binary, place it so the
`mosaic-deploy` tool discovers it as an external harness, and verify that discovery
succeeded. It targets **Windows** and assumes no prior knowledge of the external harness
system.

> **Stale documentation warning:** `Tools/Deployment/docs/harness-contributor-guide.md`
> contains CLI examples that no longer match the real tool — specifically
> `--harness <path>.yaml`, `--harness-module`, and a `validate` subcommand that do not
> exist. Use `Tools/Deployment/docs/cli.md` and `Tools/Deployment/internal/cli/run.go` as
> the authoritative references for all flags and subcommands.

---

## What this module is

`harness-opencode-module` is a standalone executable that implements the JSON-over-stdio
external harness protocol for OpenCode. It is built from the same `opencode.New()` source
as the built-in OpenCode harness, so it produces identical results — the purpose is to
demonstrate that the external harness mechanism works end-to-end.

The module reads single-line JSON requests from stdin, dispatches each to the in-process
OpenCode implementation, and writes single-line JSON responses to stdout. It exits when
stdin is closed (the host process went away) and exits with code 1 if initialisation fails.

---

## Prerequisites

- **Go toolchain** (1.22 or later) installed and on `PATH`
- **Windows** — the executable detection logic on Windows recognises `harness-exec.exe`
  and `harness-exec.bat`; Unix expects a plain `harness-exec` file
- **Task runner** (optional) — the `Taskfile.yml` tasks require a POSIX-compatible shell,
  typically provided by Git for Windows' `sh.exe`; the manual steps in this guide use
  plain PowerShell and do not require Task

---

## Concept: `--mosaic-root` vs `--workspace`

Understanding these two directories prevents the most common setup mistake.

| Term | What it is |
|------|-----------|
| `--mosaic-root` | The MOSAIC repository directory — the tool reads catalog sources, harness descriptors, and external harness folders from here. External module folders go under **this** directory at `<mosaic-root>\MosaicDeploy\harnesses\`. |
| `--workspace` | The target directory the tool deploys agent files **into**. This is a separate project directory, not the MOSAIC repository. |

If `--mosaic-root` is omitted, the tool uses the directory it was launched from as the
MOSAIC root. For any non-trivial setup, pass it explicitly.

---

## Step 1: Build the executable

Open PowerShell. Change to the `Tools/Deployment` module directory (the directory that
contains `go.mod`):

```powershell
cd C:\path\to\MOSAIC\Tools\Deployment
go build -o harness-opencode-module.exe .\cmd\harness-opencode-module
```

This produces `harness-opencode-module.exe` in the current directory. You will rename it
in the next step.

---

## Step 2: Create the harness folder

Create a folder under `<mosaic-root>\MosaicDeploy\harnesses\`. The folder name is
**irrelevant to discovery** — the registry identifies the harness by the `id:` field in
`harness.yaml`, not by the folder name. Choose any name; `opencode-external` is used here:

```powershell
New-Item -ItemType Directory -Path "C:\path\to\MOSAIC\MosaicDeploy\harnesses\opencode-external" -Force
```

---

## Step 3: Author `harness.yaml`

Copy the built-in OpenCode descriptor into the folder as `harness.yaml`:

```powershell
Copy-Item `
  "C:\path\to\MOSAIC\Tools\Deployment\internal\harness\builtin\opencode\opencode.yaml" `
  "C:\path\to\MOSAIC\MosaicDeploy\harnesses\opencode-external\harness.yaml"
```

Confirm that the file contains `id: "opencode"`. Because `id` determines which harness is
overridden, this descriptor causes the external module to supersede the built-in OpenCode
harness. Tier precedence is: external > descriptor-only > built-in.

---

## Step 4: Name the executable

Copy (or rename) the built binary to exactly `harness-exec.exe` inside the harness folder:

```powershell
Copy-Item `
  "C:\path\to\MOSAIC\Tools\Deployment\harness-opencode-module.exe" `
  "C:\path\to\MOSAIC\MosaicDeploy\harnesses\opencode-external\harness-exec.exe"
```

> **Common failure point:** The executable must be named exactly `harness-exec.exe` on
> Windows. Any other name — including the build output name `harness-opencode-module.exe`
> — is not recognised, and the folder is treated as descriptor-only rather than external.
> `harness-exec.bat` is also accepted (used for script stubs when you cannot produce a
> compiled binary), but `.exe` is the correct form for a compiled Go binary.

The harness folder should now contain exactly two files:

```
MosaicDeploy\harnesses\opencode-external\
    harness.yaml
    harness-exec.exe
```

---

## Step 5: Enable external modules

External harness modules require explicit opt-in before the deploy tool will use them.
Without opt-in, the harness appears in the registry but is **not usable** — any attempt to
deploy against it returns:

```
error: external harness modules require explicit opt-in
```

Two opt-in routes are available:

**Option A — per-run flag (no config change):**

Pass `--allow-external` on every command invocation:

```powershell
mosaic-deploy.exe deploy --allow-external --harness opencode ...
```

**Option B — persistent config (survives across invocations):**

Open `<mosaic-root>\MosaicDeploy\config\tool-config.yaml` and set:

```yaml
allow_external_modules: true
```

This enables external modules for all subsequent runs without requiring the flag. Revert
to `false` to disable.

Choose Option A during testing; switch to Option B once you are confident in the module.

---

## Step 6: Set the `MOSAIC_ROOT` environment variable

The module reads `MOSAIC_ROOT` from the environment at startup. It passes this value to
`opencode.New()` so the harness can locate catalog sources at the correct MOSAIC root.

Set it for the current PowerShell session:

```powershell
$env:MOSAIC_ROOT = "C:\path\to\MOSAIC"
```

To verify the variable is set:

```powershell
$env:MOSAIC_ROOT
```

If `MOSAIC_ROOT` is absent or empty when the module starts, it is passed as an empty string
to `opencode.New()`. The module still starts — whether this causes incorrect behaviour
depends on what `opencode.New()` does with an empty root. For a correct deployment, always
set `MOSAIC_ROOT` to the same path you pass as `--mosaic-root` to the deploy tool.

> Note: `MOSAIC_ROOT` is read by the **module subprocess** at the time it starts. The
> deploy tool itself uses `--mosaic-root` (or the current directory) for discovery. Because
> the subprocess is not currently launched during a deploy run (see Known Limitation below),
> `MOSAIC_ROOT` has no practical effect until the registry gap is closed.

---

## Step 7: Verify discovery

Run a dry-run deploy against a scratch workspace. Replace `C:\path\to\MOSAIC` and
`C:\scratch\workspace` with your actual paths:

```powershell
mosaic-deploy.exe deploy `
  --harness opencode `
  --workspace "C:\scratch\workspace" `
  --mosaic-root "C:\path\to\MOSAIC" `
  --allow-external `
  --dry-run `
  --auto-confirm `
  --output json
```

In the JSON output, look for the `"Harness"` field:

```json
{
  "Harness": {
    "ID": "opencode",
    "Tier": "external",
    "Usable": true,
    "RequiresOptIn": true,
    "ExecutablePath": "C:\\...\\MosaicDeploy\\harnesses\\opencode-external\\harness-exec.exe"
  },
  ...
}
```

`"Tier": "external"` and `"Usable": true` confirm that the external harness was discovered
and admitted by the registry.

**What failed opt-in looks like:** If you omit `--allow-external` (and
`allow_external_modules` is `false` in `tool-config.yaml`), the command returns:

```
error: external harness modules require explicit opt-in
```

This confirms the harness is present in the registry but blocked until opt-in is provided.

---

## Taskfile context

From the `Tools/Deployment` directory, two Taskfile tasks are relevant:

**`task install`** builds the `mosaic-deploy` binary for the current platform and installs
it at the MOSAIC root. It also creates the `MosaicDeploy/` runtime tree
(`config/`, `harnesses/`, `logs/`, `fallback/`) and seeds default config files if they are
not already present. Existing `tool-config.yaml` and `user-config.yaml` files are left
untouched. Running `task install` does not place the external module — you must copy the
files manually as described above.

**`task verify:e2e HARNESS=opencode`** runs a full build → install → deploy → update cycle
against a scratch workspace. This task exercises the **built-in** OpenCode harness, not the
external module. It is useful for confirming the overall deploy pipeline works, but it does
not validate the external module folder layout or the opt-in mechanism.

---

## Known limitation: the registry gap

The deploy tool discovers external harness folders and gates them correctly, but it does
**not** actually spawn the external executable during a real deploy or update run. When an
external harness is admitted (opt-in provided), the registry constructs the same
descriptor-driven module used by descriptor-only harnesses — the subprocess is never
launched and the JSON-over-stdio protocol is never exercised.

As a result:

- The steps in this guide produce a correctly discovered `TierExternal` harness with
  `Usable: true`.
- The deploy run uses the OpenCode descriptor's built-in logic, not the external
  subprocess's logic — so the observable behaviour is identical to a descriptor-only
  harness with the same `harness.yaml`.
- "Built-in behaviour still observed" is expected; it is not a sign of a misconfiguration.

For the full explanation and for how to exercise the module through a Go test instead of
the CLI, see [`EXTERNAL-MODULE-GAP.md`](../../../../EXTERNAL-MODULE-GAP.md) at the
repository root.

---

## Note on git

`MosaicDeploy/` is git-ignored in its entirety at the MOSAIC root. Files you place there
— `harness.yaml`, `harness-exec.exe`, and any config edits — are untracked and are not
preserved by git. After a fresh clone, the entire `MosaicDeploy/` directory must be
recreated (for example, by running `task install` then re-creating the harness folder
manually).

If the external module setup needs to survive across clones, keep a tracked copy of the
files elsewhere in the repository and add a documented step to copy them into
`MosaicDeploy/harnesses/<folder-name>/` after install.

---

## Troubleshooting

**Harness not listed at all / deploy proceeds as built-in with no external mention**

The registry silently skips folders that contain no `harness.yaml`. Check:
- The folder exists under `<mosaic-root>\MosaicDeploy\harnesses\` (not under
  `Tools/Deployment/` or anywhere else).
- The folder contains `harness.yaml` (exact name, lowercase).

**Harness listed but not usable (`error: external harness modules require explicit opt-in`)**

The folder and descriptor are found, but the executable file name is not recognised:
- Check that the executable is named exactly `harness-exec.exe` (not
  `harness-opencode-module.exe` or any other name). Run
  `Get-ChildItem "C:\path\to\MOSAIC\MosaicDeploy\harnesses\opencode-external\"` to
  confirm.
- If the executable name is correct and the error persists, opt-in is missing — ensure
  `--allow-external` is on the command line or `allow_external_modules: true` is set in
  `tool-config.yaml`.

**Harness discovered but opt-in not taking effect**

Confirm the `--mosaic-root` path points to the directory that contains `MosaicDeploy/`.
If `--mosaic-root` is omitted, the tool uses the launch directory, which may not be the
MOSAIC root.

**Built-in behaviour still observed after setting up the external module**

This is expected. See the Known Limitation section above. The registry gap means the
external subprocess is never spawned; the descriptor-driven logic runs instead. The setup
is correct — this is a limitation of the current deploy tool, not a misconfiguration.

**Wrong `--harness` value**

`--harness` takes a **harness ID string** such as `opencode`, not a path. The ID must
match the `id:` field in `harness.yaml`. Passing a file path or a folder name will result
in `error: harness not found`.
