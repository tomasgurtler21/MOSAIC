# vscode-ghcp variant

This variant reuses the `claude-code` file set (declared via `reuses: claude-code` in
`hook.yaml`). No files are duplicated here; the deployment tool resolves the actual
files from the `claude-code` variant folder.

The `vscode-ghcp` variant has its own registration steps (see `hook.yaml`) that differ
from the `claude-code` variant: it additionally requires the user-level VS Code setting
`chat.hooks.enabled: true`, which cannot be set by the deployment tool.
