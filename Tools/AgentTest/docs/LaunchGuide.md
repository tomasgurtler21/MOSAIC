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

### 4.3 TUI Run-Configuration Surface

After selecting a harness and models, the TUI lands on the **suite-select screen**. The help bar shows all discoverable keys. Press **Tab** (shown as "configure run" in the help bar) to open the **settings screen**, where every per-run setting is displayed as a labelled row and all are editable.

**Settings screen — five settings:**

| Setting | Edit mode | How to change |
|---------|-----------|---------------|
| Retain sandbox | Cycle | Press Enter to advance: Never → OnFailure → Always → Never |
| Repetitions | Numeric | Press Enter to open the editor, type digits, Enter to confirm or Esc to cancel |
| Report path | Inline text | Press Enter to open the editor, type the path, Enter to confirm or Esc to cancel |
| Catalog folder | Inline text | Press Enter to open the editor, type the path, Enter to confirm or Esc to cancel |
| Max concurrent runs | Numeric | Press Enter to open the editor, type digits (must be ≥ 1), Enter to confirm or Esc to cancel |

Navigate between settings with the **up/down arrow keys**. The cursor marks the focused row. Press **Esc** to return to the suite-select screen without starting a run. Every functional key is shown in the help bar — no key is hidden.

**Repetitions provenance:** The repetitions setting shows whether the displayed value comes from the selected suite file (shown as `N (suite default)`) or is a user override entered in this session (shown as `N (override)`). When the suite declares no default and the user has not set an override, the display reads `suite default`. The displayed value is the one that will actually be used when the run starts.

**Max concurrent runs:** Controls how many tests × repetitions run concurrently across the entire suite — one bound over the full (test × repetition) matrix, not one per nesting level. The default is `4`, chosen conservatively: each concurrent run is a full harness process plus a deployed agent tree plus a relocated harness configuration tree on disk, and every dispatch inside it spawns short-lived interceptor processes contending on that run's own lock file. Four gives most of the wall-clock relief the feature exists for while keeping simultaneous sandboxes to a handful and staying well below the concurrency a provider account is likely to permit. A value of `1` is strictly sequential and reproduces the pre-concurrency behaviour exactly. Values below `1` are rejected.

**Resource cost of a chosen bound.** Each attempt in flight occupies one slot and holds: one sandbox directory on disk (named `<suiteRunID>-<testID>-<runNumber>`), one deployed agent tree (subject orchestrator plus stub collaborators, typically a few hundred KB of text files), one relocated harness configuration tree (potentially several MB, depending on the session transcript the harness accumulates), one growing diagnostic-capture file for the subject's stdout and stderr, one deploy log written during setup, one running harness process, and short-lived interceptor processes for each dispatch. At a bound of N, N complete sets of these resources exist simultaneously. They are released when each slot completes teardown.

**Retention multiplies the footprint.** When sandbox retention is set to `OnFailure` or `Always`, teardown does not delete the sandbox. With `Always`, every attempt leaves its sandbox, deployed agent tree, harness configuration tree, and diagnostic capture on disk until you delete them manually. A 50-repetition suite with `Always` retention retains 50 sandboxes, potentially several hundred MB total. Use `OnFailure` for diagnostic workflows (retain evidence only where it is needed) and `Never` for large-repetition statistical runs.

After configuring, press **Esc** to return to the suite-select screen and **Enter** to start the run.

### 4.4 Launching the CLI

The CLI requires a positional subcommand:

```
Tools\AgentTest\dist\mosaic-agent-test.exe run <suite-file>
```

```
Tools\AgentTest\dist\mosaic-agent-test.exe validate <suite-file>
```

The CLI exits with a structured result and is suitable for scripted use, CI, and cases where stdout must carry the machine-readable report (`--format json`).

**`--max-concurrent-runs <n>`** sets how many tests × repetitions run concurrently across the entire suite. The default is `4` (see the Max concurrent runs section under the TUI settings above for the full rationale). A value of `1` is strictly sequential. Values below `1` are rejected as a usage error.

```
Tools\AgentTest\dist\mosaic-agent-test.exe run suite.yaml --max-concurrent-runs 1
```

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

## 8. Cost Attribution: Configuration and Diagnostics

### 8.1 How the Analyser Finds pricing.yaml

`mosaic-agent-test` invokes `mosaic-log-analyzer` as a subprocess. The analyser resolves its pricing configuration at:

```
<working directory>/MosaicLogAnalyzer/config/pricing.yaml
```

AgentTest sets the analyser subprocess's **working directory** to the resolved MOSAIC root — the same path `--mosaic-root` controls. This is the only mechanism through which the analyser locates pricing configuration; it exposes no override flag of its own.

When the binary lives at `Tools/AgentTest/dist/mosaic-agent-test.exe`, the default `--mosaic-root` resolves to the repository root (`selfDir/../../..`), so the effective pricing file path is:

```
<repo root>\MosaicLogAnalyzer\config\pricing.yaml
```

**A `pricing.yaml` placed inside `Tools/AgentTest/dist/` is never read.** The analyser's working directory is set to the MOSAIC root, not to `dist/`. Placing the file in `dist/` will not fix a missing-pricing error.

### 8.2 Fixing a Missing pricing.yaml

A missing pricing file produces an error similar to:

```
error loading pricing config: open MosaicLogAnalyzer\config\pricing.yaml: The system cannot find the path specified.
```

**Option 1 — Place the file at the correct location.** Copy `pricing.yaml` to:

```
<repo root>\MosaicLogAnalyzer\config\pricing.yaml
```

If the `MosaicLogAnalyzer/config/` directory does not yet exist at the repository root, create it first. The repository already contains this directory under the source tree; `pricing.yaml` belongs there alongside the rest of the log-analyser configuration.

**Option 2 — Override MosaicRoot.** If you cannot modify the repository root, point `--mosaic-root` at the directory that contains your `MosaicLogAnalyzer/config/pricing.yaml`. AgentTest passes this directory to the analyser as its working directory:

```
mosaic-agent-test.exe --mosaic-root D:\my-config-root run my-suite.suite.yaml
```

Or set the environment variable for persistent configuration:

```
MOSAIC_AGENT_TEST_MOSAIC_ROOT=D:\my-config-root
```

**Option 1 is the standard path.**

### 8.3 Cost Attribution Diagnostics

A run's cost report can produce one of four attribution values. Each names a distinct failure cause:

| Attribution | Meaning | Likely cause |
|-------------|---------|--------------|
| `attributed` | Full cost captured for this run | All models priced, run identity bound correctly |
| `partial` | Partial cost captured; some models unpriced | One or more models had no entry in `pricing.yaml`; the attributable amount from priced models is shown and the unpriced model(s) are named |
| `unknown_bucket` | Events exist but in the fallback bucket | Run identity was not resolved before the subject emitted events; events landed in the `unknown-run` folder instead of the per-run folder |
| `unavailable` | No usable cost data | Logger bundle did not run, subject terminated before emitting events, or the analyser could not be invoked |

**Partial attribution:** When one model among several is unpriced, the cost report shows the attributable amount from priced models rather than zeroing the entire run. The unpriced model name is included in the cost detail. One unpriced model does not silence the rest.

**unknown_bucket vs unavailable:** If you see `unknown_bucket`, the subject ran and emitted events but they were attributed to the fallback bucket rather than the per-run folder. This is a run-identity binding failure, not a pricing configuration failure — check that the subject dispatched at least one collaborator using the documented protocol envelope before the cutoff fired. If you see `unavailable` with "no usage data found", the logger bundle either did not run or the subject terminated without emitting any events at all.

**Unpriced model:** If you see `partial` or `unavailable` with a message about model pricing, add the model to `pricing.yaml`. The model name is included in the diagnostic detail when the analyser can identify it.

---

## Related Documents

- `Tools/AgentTest/docs/Design.md` — architecture and design decisions
- `Tools/AgentTest/cmd/mosaic-agent-test/main.go` (`resolveWiringConfig`, `resolveConfiguredPath`) — authoritative defaults and precedence
- `Tools/AgentTest/Taskfile.yml` — full list of build and staging tasks
