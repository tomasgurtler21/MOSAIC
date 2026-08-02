# mosaic-logger — Hook Adapter Bundle

A stdlib-only Python 3 hook adapter bundle that captures hook events and writes
structured logs under `OrchestrationLogs/` in your project root. Implemented for
`claude-code` (12 events), `ghcp-cli` (14 events), and `vscode-ghcp` (8 events).

## Supported variants

| Variant | Status |
|---|---|
| `claude-code` | Implemented — Python adapter deployed to `.claude/hooks/` |
| `opencode` | Placeholder — plugin skeleton not yet authored |
| `vscode-ghcp` | Implemented — Python adapter deployed to `.github/hooks/` |
| `ghcp-cli` | Implemented — Python adapter deployed to `.github/hooks/` |

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
form. The `claude-code`, `vscode-ghcp`, and `ghcp-cli` variants are fully authored and
deployable; marking the bundle as a placeholder would be inaccurate. The `opencode`
variant remains a stub, but the schema provides no mechanism to flag it individually.

## Why `content_hash` is absent

The `claude-code` variant lists `../hook.yaml` in its file set so the deployed adapter
can read its own version at runtime (the `run_start.adapter_version` field). Including
`hook.yaml` in the hash computation makes the stored hash unstable by construction: any
change to the version field changes the hash, which must then be updated in the file,
which changes the hash again. `content_hash` is left unset; the deployment tool skips
hash validation when the field is absent.
