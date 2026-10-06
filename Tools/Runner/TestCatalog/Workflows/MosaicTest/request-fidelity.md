---
version: "1.0"
name: "MosaicTest Request Fidelity Workflow"
description: "Harness conformance fixture: request-direction payload fidelity. Proves that what the Runner sends (input artifact paths and task_description) reaches the agent byte for byte, including through Windows .cmd/.bat shims, which pass the command line through cmd.exe."
hint: "Harness test: request payload survives the trip to the agent (cmd.exe shim safe)"
author: MOSAIC
id: request-fidelity
referenced_agents:
  - mosaictest-scripted
artifacts:
  - MosaicTestScript/request-%OS%-fidelity.md
modes:
  - auto
---

<Workflow type="core" name="request-fidelity" version="1.0">
## MosaicTest Request Fidelity Workflow

**Use when:** Checking that a harness delivers the **request** to the agent intact. `payload-stress` covers the opposite direction (agent → Runner). On Windows a native executable and an npm `.cmd` shim take different delivery paths, so the result is only meaningful per install.

| Phase | Subagent | HITL | On Success | On Findings | Input | Output |
|-------|----------|:----:|------------|-------------|-------|--------|
| RESEARCH | mosaictest-scripted | FALSE | COMPLETE | - | MosaicTestScript/request-%OS%-fidelity.md | - |

**Notes:**
- **Run this workflow in Auto mode** with pre-consultation enabled (the default). The payload under test is the pre-consultation advice, which the Runner appends to the auto-routed dispatch.
- The `%OS%` in the script's file name is literal and intentional. Do not rename it.
- Seed `Fixtures/request-fidelity` (the whole directory, not anything inside it) as the single seed path. See `Fixtures/README.md`.
- The failure this workflow exists to catch only shows on a Windows install that is a `.cmd`/`.bat` shim (e.g. npm `opencode.cmd`, `copilot.cmd`). The test runner currently resolves one executable per harness from `PATH`, so the shim is covered only when it's the install the runner resolves. Covering every install is a test-runner requirement (`TestCatalogDesign.md` §2.1 rule 4; `Requirements_RunnerOsFidelity.md`), not a manual step for this test.

</Workflow>

---

## Design Rationale

### Why this workflow exists

On 2026-10-06 an OpenCode run on Windows reached the orchestrator with an empty request. The prompt was passed as one command-line argument containing newlines, and the npm `opencode.cmd` shim is launched through `cmd /c`, which cuts the command line at the first newline. Every existing auto-mode test would have failed in that environment, but none were ever run there: the development machine has OpenCode as a native WinGet `opencode.exe`, so no `cmd.exe` was involved.

None of the existing assertions could catch request-side damage either. `task_contains` reads the **dispatch log**, which records what the Runner *sent*. In the incident that log was complete and correct. Only what arrived at the agent was wrong.

### Why the file name carries `%OS%`

The suite is scored on exit code and status codes, so a request-side defect must change a status to be caught without a human reading the TUI. The stub's only instruction channel is its script path. Putting a `cmd.exe` expansion token in that path gives a deterministic, machine-checked signal:

| Delivery path | What the stub receives | Result |
|---|---|---|
| stdin, or a native executable via CreateProcess | `request-%OS%-fidelity.md` | script found, `SUCCESS` |
| any `cmd.exe` hop (`cmd /c shim.cmd ...`) | `request-Windows_NT-fidelity.md` | script missing, `FIXTURE_ERROR` (run fails) |
| command line cut or split inside `task_description` | no `input_artifacts` at all (serialised after it) | `FIXTURE_ERROR` (run fails) |

`OS` is chosen because Windows always defines it, so the expansion is guaranteed whenever `cmd.exe` is involved. On Linux and macOS there is no `cmd.exe` and the name passes through unchanged.

### Why the advice is the content payload

Pre-consultation advice is the only request-side free text in an auto-routed dispatch, and it is serialised before `input_artifacts`. Partial damage that leaves the JSON parseable (an expanded `%PATH%`, a swallowed `^`, a collapsed quote) does not change the status, so it is surfaced through the `{task_description}` echo for a human to compare with `MosaicTestRouting.md`. The path check above is the automated half; the echo is the diagnostic half.

---

## Expected Run

| Log `Seq` | `Agent` | `Phase` | `Status` | `Summary` shows |
|:---:|---|---|---|---|
| 1 | `mosaictest-scripted#1` | RESEARCH | SUCCESS | script path with literal %OS% arrived intact, followed by the echoed task_description |

The echoed task_description must contain the advice from `MosaicTestRouting.md` **unchanged**: `%OS%` and `%PATH%` literal (not expanded), `100%%` with both percent signs, every `^`, the pipe character, `!OS!`, all quotes and backslashes, and the second line. It runs from `MOSAICTEST-REQUEST-FIDELITY` through `MOSAICTEST-REQUEST-FIDELITY-END`.

**Run outcome:** COMPLETE.

---

## What a Failure Means Here

| Observation | Where to look |
|---|---|
| `FIXTURE_ERROR`, path names `Windows_NT` instead of `%OS%` | The request passed through `cmd.exe`. The harness builder puts prompt content on the command line and the executable is a `.cmd`/`.bat` shim (`Tools/Common/harness`, `ResolveExecutable` + the harness's argument builder) |
| `FIXTURE_ERROR`, no `MosaicTestScript/` path at all | The request was cut before `input_artifacts`: a newline in a command-line argument through `cmd.exe`, or a split at the pipe character |
| Pre-consultation fails, or the orchestrator reports an empty or garbled request | Same defect on the orchestrator's raw-invocation path (`InvokeRaw`) |
| `SUCCESS`, but the echo differs from the advice (expanded `%PATH%`, missing `^` or quotes, lost second line) | Partial command-line damage that left the JSON parseable |
| `SUCCESS`, but the echo lacks `MOSAICTEST-REQUEST-FIDELITY` | Pre-consultation advice was not appended. See `preconsult-advice` first |
| Stray console noise mentioning `MOSAICTEST-PIPE-NOT-A-COMMAND` | `cmd.exe` treated the `|` as a pipe: the request was split into two commands |

---

## Changelog

| Version | Date | Author | Summary |
|---------|------|--------|---------|
| 1.0 | 2026-10-06 | MOSAIC | Initial version, after the OpenCode `.cmd` shim truncation incident |

---

## Open Ideas / Dead Ends

**Ideas under consideration:**
- A response-side assertion (`status_message` contains …), so the echo is checked automatically instead of by a human. Requested as R4 in `Requirements_RunnerOsFidelity.md`; once it exists, `request-fidelity-auto.expected.json` should use it.
- A test of very long requests (command-line length limits: about 32 KB through CreateProcess, about 8 KB through `cmd.exe`). It only matters while any harness still puts prompt content on the command line.

**Dead ends (tried and rejected):**
- Relying on `task_contains`: it checks the dispatch log, which is the Runner's record of intent, and passes even when delivery is broken.
