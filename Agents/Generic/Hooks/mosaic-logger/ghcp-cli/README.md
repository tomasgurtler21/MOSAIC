# ghcp-cli variant

A stdlib-only Python hook adapter for the GitHub Copilot CLI (GHCP CLI). Deployed to
`.github/hooks/` alongside the registration file, it captures 14 hook events and writes
structured logs under `OrchestrationLogs/` in your project root.

## File set

The adapter is a flat set of 9 Python modules, all deployed to `.github/hooks/`:

| Module | Role |
|---|---|
| `mosaic_logger.py` | Dispatcher entrypoint — reads stdin, routes by event name, exits 0 |
| `mosaic_logger_core.py` | Constants, `HookContext`, event envelope, path layout, write primitives |
| `mosaic_logger_runstate.py` | ID extraction, pending-dispatch queue, agent-name correlation map |
| `mosaic_logger_transcript.py` | Best-effort model/token extraction from the session transcript |
| `mosaic_logger_export.py` | Byte-for-byte transcript copy and sidecar metadata |
| `mosaic_logger_artifacts.py` | `01_input.md` / `02_output.md` rendering and writing |
| `mosaic_logger_handlers_session.py` | `sessionStart`, `sessionEnd`, `userPromptSubmitted`, `userPromptTransformed`, `preCompact`, `agentStop` |
| `mosaic_logger_handlers_invocation.py` | `subagentStart`, `subagentStop` |
| `mosaic_logger_handlers_tools.py` | `preToolUse`, `postToolUse`, `postToolUseFailure` |

A copy of `hook.yaml` (the bundle manifest) is also deployed alongside the modules so
the adapter can stamp `adapter_version` on `run_start` events at runtime.

## Deployment target

All files deploy to `.github/hooks/`. The registration file
`.github/hooks/mosaic-logger.json` must exist in the same directory for GHCP CLI to
discover it. The bare `mosaic_logger.py` script name in each command entry resolves
because `cwd` is set to `.github/hooks` in every registered fragment entry.

## Interpreter requirement

The deployed registration file configures one command string per event. Each command
string names a Python interpreter (`python`, `python3`, or `py` — depending on what
was configured) followed by the script name and event name.

Python interpreter availability varies across operating systems, distributions, and
individual installs. On some systems `python3` is the only resolvable name; on others
only `python` is available; on Windows installs `py` is common. No single interpreter
name resolves universally.

When the configured interpreter command does not resolve on your machine, the hook
events that GHCP CLI fires will silently produce no log output. To correct this:

1. Open the deployed configuration file (`.github/hooks/mosaic-logger.json` in your
   project root).
2. Locate the `"command"` entries — there is one per registered event, giving the same
   interpreter name each time.
3. Replace the interpreter name in every `"command"` entry with the name that resolves
   correctly in your environment.

The script name and event name that follow the interpreter name must be preserved
exactly. Only the leading interpreter token changes.

## Supported events

The adapter registers exactly 14 events:

`sessionStart`, `sessionEnd`, `userPromptSubmitted`, `userPromptTransformed`,
`preToolUse`, `postToolUse`, `postToolUseFailure`, `preCompact`, `agentStop`,
`subagentStart`, `subagentStop`, `permissionRequest`, `errorOccurred`, `notification`

The `subagentStart` event is registered synchronously so the agent-id mapping is
written before the subagent issues its first tool call. All other events are
asynchronous.
