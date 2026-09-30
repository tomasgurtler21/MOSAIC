# mosaic-logger — Hook Adapter Bundle

A hook adapter bundle that captures hook events and writes structured logs under
`OrchestrationLogs/` in your project root. Implemented for `claude-code` (12 events),
`ghcp-cli` (14 events), `vscode-ghcp` (8 events), and `opencode` (4 hooks).

## Supported variants

| Variant | Status |
|---|---|
| `claude-code` | Implemented — Python adapter deployed to `.claude/hooks/` |
| `opencode` | Implemented — TypeScript plugin deployed to `.opencode/plugins/` |
| `vscode-ghcp` | Implemented — Python adapter deployed to `.github/hooks/` |
| `ghcp-cli` | Implemented — Python adapter deployed to `.github/hooks/` |

## Claude Code: subagent result delivery

On Claude Code 2.1.271+ in auto mode a subagent delivers its result through a
`SubagentHandback` tool call. The report is `tool_input.message`, and it reaches
the caller just before the hand-back's PostToolUse hook fires. The adapter
therefore writes `invocation_end` (`response`, `status_code`, timestamp), the
final `turn` and `02_output.md` from that PostToolUse. `model` and `token_usage`
are read from the agent transcript at hand-back time. `SubagentStop` fires later
and its `last_assistant_message` is only post-hand-back commentary; it does not
produce a second `invocation_end`.

Without a hand-back (older CLI, non-auto mode, or Haiku as the main model, which
silently falls back from auto to default) `SubagentStop` /
`last_assistant_message` remains the delivery point.

Known limitation: if the agent transcript has not been flushed at hand-back time,
`invocation_end` omits `model` and `token_usage`, and they are never back-filled.
`Tools/LogAnalyzer` reports the missing fields as an issue for that invocation.
Tokens used after the hand-back are still counted through `usage_record` events.

`tool_call_end` carries the tool's output in `tool_output`, taken from the
PostToolUse `tool_response`, without truncation.

## Interpreter requirement

The `claude-code` variant requires **`python3`** to be available in the environment
where the Claude Code harness runs hook commands. The registration fragment uses:

```json
{
  "type": "command",
  "command": "python3",
  "args": ["${CLAUDE_PROJECT_DIR}/.claude/hooks/mosaic_logger.py"]
}
```

### Windows: using `python` or `py` instead of `python3`

On Windows installs where `python` or `py` is available but `python3` is not, update
the `command` field in the registration fragment pasted into `.claude/settings.json`:

```json
"command": "python"
```

or

```json
"command": "py"
```

The deployment tool emits the fragment for manual paste when `.claude/settings.json`
already exists. Make the one-line tweak before pasting.

## Why `placeholder` is absent

The schema's `placeholder` flag is a single bundle-level boolean with no per-variant
form. All four variants — `claude-code`, `vscode-ghcp`, `ghcp-cli`, and `opencode` —
are fully authored and deployable; marking the bundle as a placeholder would be
inaccurate. The schema provides no per-variant mechanism to distinguish authoring
states, so the whole-bundle key is omitted.

## Why `content_hash` is absent

The `claude-code` variant lists `../hook.yaml` in its file set so the deployed adapter
can read its own version at runtime (the `run_start.adapter_version` field). Including
`hook.yaml` in the hash computation makes the stored hash unstable by construction: any
change to the version field changes the hash, which must then be updated in the file,
which changes the hash again. `content_hash` is left unset; the deployment tool skips
hash validation when the field is absent.
