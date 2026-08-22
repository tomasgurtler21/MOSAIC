# mosaic-agent-test — Launch Guide

This guide covers where the binary is expected to live, what must be staged beside it, how to launch the CLI and TUI frontends, and what configuration overrides are available. A worked example at the end explains a common pre-flight failure and how to fix it.

---

## 1. Expected Binary Location

`mosaic-agent-test` anchors all of its default paths on `selfDir` — the directory that contains the running binary, resolved via `os.Executable()`. The binary therefore **must live in `Tools/AgentTest/dist/`** for the binary-relative defaults to resolve correctly.

`dist/` is three directories below the repository root:

```
<repo root>/
  Tools/
    AgentTest/
      dist/
        mosaic-agent-test.exe        <- Windows binary
        mosaic-agent-test-linux-amd64  <- Linux binary (when also built)
        logger-bundle/
        mosaic-log-analyzer.exe
        mosaic-deploy.exe
```

This three-level depth is exactly what makes `selfDir/../../..` resolve to the repository root, which is the default value for `--mosaic-root`.

---

## 2. Producing the Staged Distribution

Run the following from the `Tools/AgentTest/` directory.

| Command | What it does |
|---------|-------------|
| `task dist:windows` | Builds `dist/mosaic-agent-test.exe` and stages all runtime dependencies beside it |
| `task dist:linux` | Builds `dist/mosaic-agent-test-linux-amd64` and stages all runtime dependencies beside it |
| `task dist:all` | Builds and stages for both Windows and Linux |

Each `dist:*` task:
1. Cross-compiles the binary for its target platform into `dist/`.
2. Runs `stage:bundle` — copies `hook.yaml` and the `claude-code/` Python files from `.claude/hooks/` into `dist/logger-bundle/`.
3. Copies the cost-analysis tool from `../LogAnalyzer/dist/` into `dist/`.
4. Copies the deployment tool from `../Deployment/dist/` into `dist/`.

After `task dist:windows`, the `dist/` directory contains everything the binary needs to run with no flags and no environment variables.

---

## 3. Expected Staged Layout

The binary expects its runtime dependencies to be present in the following layout inside `dist/`:

```
dist/
  mosaic-agent-test.exe          <- the binary itself
  logger-bundle/
    hook.yaml                    <- the bundle manifest
    claude-code/                 <- one directory per harness variant
      mosaic_logger.py
      mosaic_logger_core.py
      ...
  mosaic-log-analyzer.exe        <- the cost-analysis tool
  mosaic-deploy.exe              <- the deployment tool
```

`logger-bundle/hook.yaml` is the manifest file the harness provisioner reads at startup. The `claude-code/` subdirectory holds the harness-specific Python scripts. If the bundle directory is absent or the manifest is unreadable, the tool reports a pre-flight environment failure naming what it searched for and which override changes the path.

Note: the logger bundle sources live at `.claude/hooks/` in the repository and are **only copied into `dist/`** by the `stage:bundle` task. Running the binary from any location that does not have a staged `logger-bundle/` beside it will fail pre-flight.

---

## 4. Launching the Tool

### 4.1 Frontend Selection

The binary selects a frontend in this order, stopping at the first match:

1. **Interceptor** — if the first positional argument is `intercept`. This route is used exclusively by the harness to relay stub responses; it is not a user-facing subcommand.
2. **TUI** — if `--tui` appears anywhere in the arguments.
3. **CLI** — if any positional subcommand (`run`, `validate`, or similar) appears.
4. **TUI** — if both stdin and stdout are terminals (no subcommand given, running in an interactive terminal).
5. **CLI** — otherwise (piped, redirected, or scripted invocation).

**Consequence:** double-clicking `mosaic-agent-test.exe` in an Explorer window opens the interactive TUI, because both stdin and stdout are terminal-connected at that point.

The CLI and TUI frontends receive the same resolved configuration from the composition root. There is no difference in path resolution between them.

### 4.2 Launching the TUI

From a terminal, with the current directory anywhere:

```
Tools\AgentTest\dist\mosaic-agent-test.exe
```

Or explicitly:

```
Tools\AgentTest\dist\mosaic-agent-test.exe --tui
```

The TUI scans for `*.suite.yaml` files starting from the process working directory (or from the path given by `--suites`). Launching from the repository root lets it discover all suites under `Tools/AgentTest/tests/`.

### 4.3 Launching the CLI

The CLI requires a positional subcommand:

```
Tools\AgentTest\dist\mosaic-agent-test.exe run <suite-file>
```

```
Tools\AgentTest\dist\mosaic-agent-test.exe validate <suite-file>
```

The CLI exits with a structured result and is suitable for scripted use, CI, and cases where stdout must carry the machine-readable report (`--format json`).

---

## 5. Configuration Overrides

All five configurable paths follow the same precedence: **CLI flag → environment variable → binary-relative default**. The flag and environment variable are always equivalent in what they set; the environment variable is convenient for persistent per-machine configuration.

| Path | CLI flag | Environment variable | Binary-relative default |
|------|----------|----------------------|------------------------|
| Logger bundle directory | `--logger-bundle` | `MOSAIC_AGENT_TEST_LOGGER_BUNDLE` | `<selfDir>/logger-bundle` |
| Cost-analysis tool | `--cost-tool` | `MOSAIC_AGENT_TEST_COST_TOOL` | `<selfDir>/mosaic-log-analyzer[.exe]` |
| Deployment tool | `--deploy-tool` | `MOSAIC_AGENT_TEST_DEPLOY_TOOL` | `<selfDir>/mosaic-deploy[.exe]` |
| MOSAIC root | `--mosaic-root` | `MOSAIC_AGENT_TEST_MOSAIC_ROOT` | `<selfDir>/../../..` |
| Catalog folder | `--catalog-folder` | `MOSAIC_AGENT_TEST_CATALOG_FOLDER` | (empty — deploy tool resolves its own) |

The precedence is enforced in `resolveConfiguredPath` in `cmd/mosaic-agent-test/main.go`, which is the authoritative source for these defaults. When in doubt, read that function.

**`--mosaic-root`** redirects the entire MOSAIC root, including the protocol document, skill folders, and bundle specifications that the deploy tool reads. Use it with care: a root that is missing these resources will cause the deploy tool to fail on them, not on the catalogue.

**`--catalog-folder`** redirects only the catalogue source directories (where agents and workflows are scanned from) while leaving all other root-relative resources at the real MOSAIC root. This is the right override for pointing AgentTest at a test catalogue without duplicating the full MOSAIC root structure.

**`[.exe]`** in the default column means the binary appends `.exe` on Windows and nothing on Linux, matching the platform convention for executables.

---

## 6. Worked Example: Wrong Launch Location

### Symptom

You copy or move `mosaic-agent-test.exe` to the repository root and run it from there:

```
C:\AI\MOSAIC\MOSAIC\mosaic-agent-test.exe
```

The tool opens but immediately fails pre-flight with errors similar to:

```
error: environment-unusable: logger-bundle\hook.yaml: The system cannot find the path specified.
```

and later, when the deploy tool is invoked:

```
C:\Catalog\HarnessInjections\Claude Code\HarnessInjections.md: The system cannot find the path specified.
```

### Why It Happens

`selfDir` is the directory that contains the running binary. When the binary lives at the repository root, `selfDir` is the repository root itself.

The two defaults that break are computed differently:

- `LoggerBundleDir` = `selfDir/logger-bundle` → `C:\AI\MOSAIC\MOSAIC\logger-bundle`
  This directory does not exist. The logger bundle source lives at `.claude/hooks/` and is only copied into `dist/logger-bundle/` by the `stage:bundle` task. There is no `logger-bundle/` folder at the repository root.

- `MosaicRoot` = `selfDir/../../..` → `C:\`
  Three `..` segments above the repository root reach the drive root on Windows. The deploy tool then looks for `C:\Catalog\HarnessInjections\...`, which does not exist, producing the second error.

The two defaults use different numbers of path segments (`0` for the bundle, `3` for the root). Moving the binary from `dist/` breaks both — but for different reasons and in different directions.

### How to Fix It

**Option 1 — Run the staged binary.** Always run the binary from `dist/`, not from a copy elsewhere. Build and stage with:

```
cd Tools\AgentTest
task dist:windows
.\dist\mosaic-agent-test.exe
```

This is the intended workflow and requires no flags.

**Option 2 — Supply the overrides.** If you must run the binary from a non-standard location, pass the two overrides explicitly:

```
mosaic-agent-test.exe \
  --logger-bundle C:\AI\MOSAIC\MOSAIC\Tools\AgentTest\dist\logger-bundle \
  --mosaic-root C:\AI\MOSAIC\MOSAIC
```

The same overrides are available as environment variables for persistent configuration:

```
MOSAIC_AGENT_TEST_LOGGER_BUNDLE=C:\AI\MOSAIC\MOSAIC\Tools\AgentTest\dist\logger-bundle
MOSAIC_AGENT_TEST_MOSAIC_ROOT=C:\AI\MOSAIC\MOSAIC
```

**Option 1 is always simpler.** Reserve Option 2 for CI pipelines or developer machines where the binary is installed to a central location outside the source tree.

---

## 7. Sandbox Location and Retention

Each test run creates an isolated sandbox directory where the subject agent and its stub collaborators are deployed before the run starts.

### Sandbox naming scheme

Every sandbox lives under the workspace root and follows this layout:

```
<workspaceRoot>/
  <RunID>-<TestID>-<RunNumber>/
    subject/      <- subject agent deployment
    control/      <- stub collaborator deployment
```

`RunID` is a UUID assigned when the suite run starts. `TestID` is the test's identifier from the suite file. `RunNumber` is a 1-based counter for the current repetition (always `1` unless `--repetitions` is set above 1).

### Default workspace root

The workspace root defaults to:

```
<os.TempDir()>/mosaic-agent-test-workspaces
```

On Windows this resolves to:

```
C:\Users\<user>\AppData\Local\Temp\mosaic-agent-test-workspaces
```

On Linux it resolves to `/tmp/mosaic-agent-test-workspaces`.

To override the workspace root, pass `--workspace-root` with an absolute path:

```
mosaic-agent-test.exe --workspace-root D:\test-sandboxes run my-suite.suite.yaml
```

### Retention cycle

The `--retention` flag (or the TUI retention toggle) controls whether sandbox directories survive after a run completes. The setting cycles through three states:

| State | Behaviour |
|-------|-----------|
| `Never` (default) | Every sandbox is deleted after its run, regardless of outcome. |
| `OnFailure` | Sandboxes from failing runs are kept; sandboxes from passing runs are deleted. |
| `Always` | Every sandbox is kept after its run, regardless of outcome. |

In the TUI, press the retention key to advance through the cycle: `Never → OnFailure → Always → Never → ...`. The current state is shown on the run screen.

A retained sandbox is printed to the report as the sandbox path, so you can inspect the deployed agents and conversation transcripts after the fact.

---

## 8. Worked Example: Missing pricing.yaml

### Symptom

A run completes but reports zero cost for every test, or the cost-analysis step fails with an error similar to:

```
error loading pricing config: open MosaicLogAnalyzer\config\pricing.yaml: The system cannot find the path specified.
```

### Why It Happens

The cost-analysis tool (`mosaic-log-analyzer`) looks for pricing configuration at:

```
<MosaicRoot>/MosaicLogAnalyzer/config/pricing.yaml
```

`MosaicRoot` defaults to `selfDir/../../..` — three directories above the binary, which resolves to the **repository root**, not to `dist/`. When the binary lives at `Tools/AgentTest/dist/mosaic-agent-test.exe`, the default resolution is:

```
selfDir      = Tools\AgentTest\dist
selfDir/..   = Tools\AgentTest
selfDir/../..= Tools
selfDir/../../.. = <repo root>
```

So the expected pricing file path is:

```
<repo root>\MosaicLogAnalyzer\config\pricing.yaml
```

A common mistake is placing the file at:

```
Tools\AgentTest\dist\MosaicLogAnalyzer\config\pricing.yaml
```

This is inside `dist/`, not at the repository root. `MosaicRoot` points at the repository root, so the cost tool never looks inside `dist/` for its config files.

### How to Fix It

**Option 1 — Place the file at the correct location.** Copy `pricing.yaml` to the path the tool actually reads:

```
<repo root>\MosaicLogAnalyzer\config\pricing.yaml
```

If the `MosaicLogAnalyzer/config/` directory does not yet exist at the repository root, create it first.

**Option 2 — Override MosaicRoot.** If you cannot modify the repository root (e.g. a read-only installation), point `--mosaic-root` at the directory that contains your `MosaicLogAnalyzer/config/pricing.yaml`:

```
mosaic-agent-test.exe --mosaic-root D:\my-config-root run my-suite.suite.yaml
```

Or set the environment variable for persistent configuration:

```
MOSAIC_AGENT_TEST_MOSAIC_ROOT=D:\my-config-root
```

**Option 1 is the standard path.** The repository already contains `MosaicLogAnalyzer/config/` under the source tree; `pricing.yaml` belongs there alongside the rest of the log-analyser configuration.

---

## Related Documents

- `Tools/AgentTest/docs/Design.md` — architecture and design decisions
- `Tools/AgentTest/cmd/mosaic-agent-test/main.go` (`resolveWiringConfig`, `resolveConfiguredPath`) — authoritative defaults and precedence
- `Tools/AgentTest/Taskfile.yml` — full list of build and staging tasks
