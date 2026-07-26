# External Harness Module Protocol

The external harness module protocol lets third-party tools integrate with the MOSAIC deployment system. An external module is a standalone executable that speaks a simple JSON-over-stdio protocol. MOSAIC launches it as a subprocess, exchanges single-line JSON messages over stdin/stdout, and treats it identically to a built-in harness for all transformation and deployment operations.

## Overview

```
MOSAIC (adapter)                    External module (subprocess)
──────────────────────────────────────────────────────────────
  Start subprocess
  ─── handshake request ─────────────────────────────────────>
  <── handshake response ─────────────────────────────────────
  ─── tools request ──────────────────────────────────────────>
  <── tools response ─────────────────────────────────────────
  ...
  Close stdin (signals shutdown)
  ←── subprocess exits
```

The module runs for as long as MOSAIC needs it. MOSAIC sends requests one at a time, waiting for a response before sending the next. The module must process each request and reply on stdout before reading the next one from stdin.

## Message Format

Every message is a single UTF-8 JSON object terminated by a newline (`\n`). No pretty-printing. No multi-line messages.

### Request (MOSAIC → module)

```json
{"protocol":"1.0","id":"42","method":"tools","params":{...}}
```

| Field | Type | Description |
|-------|------|-------------|
| `protocol` | string | Protocol version; always `"1.0"` |
| `id` | string | Opaque request identifier; echo back unchanged in the response |
| `method` | string | Method name; see [Methods](#methods) |
| `params` | object | Method-specific parameters; see each method below |

### Response (module → MOSAIC)

Success:
```json
{"protocol":"1.0","id":"42","result":{...}}
```

Error:
```json
{"protocol":"1.0","id":"42","error":{"code":"unsupported_artifact","message":"kind 'workflow' not supported"}}
```

| Field | Type | Description |
|-------|------|-------------|
| `protocol` | string | Must be `"1.0"` |
| `id` | string | Echo of the request `id` |
| `result` | object | Present on success; absent on error |
| `error` | object | Present on error; absent on success |
| `error.code` | string | Machine-readable error code |
| `error.message` | string | Human-readable description |

## Protocol Version

The current protocol version is `"1.0"`. MOSAIC sends `"1.0"` in every request. The module MUST echo `"1.0"` in every response. A response with a different `protocol` value causes MOSAIC to close the connection with `ErrProtocolMismatch`.

## Methods

### handshake

The first exchange. MOSAIC sends this immediately after starting the subprocess. The module responds with its identity. If the response's `protocol` field does not match `"1.0"`, MOSAIC rejects the module.

**Request params:**
```json
{"protocol":"1.0"}
```

**Response result:**
```json
{
  "protocol": "1.0",
  "harness": {
    "id": "my-harness",
    "display_name": "My Harness",
    "tier": "external",
    "usable": true
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `protocol` | string | Must be `"1.0"` |
| `harness.id` | string | Harness identifier; used in provenance logging |
| `harness.display_name` | string | Human-readable name for the TUI |
| `harness.tier` | string | Always `"external"` for external modules |
| `harness.usable` | bool | `true` unless the module has a startup error |

### tools

Map an agent's generic tool list to harness-specific frontmatter fields.

**Request params:**
```json
{
  "agent_key": "test-runner",
  "generic": ["file_read", "terminal", "user_interaction"],
  "placeholder": "",
  "custom_names": {},
  "skipped_tools": {}
}
```

| Field | Type | Description |
|-------|------|-------------|
| `agent_key` | string | The agent being transformed |
| `generic` | string[] | Generic tool names from the agent source |
| `placeholder` | string | Non-empty when the source used a placeholder (e.g. `{tool-permissions}`) instead of a list |
| `custom_names` | object | Map from generic tool name to user-supplied MCP server name |
| `skipped_tools` | object | Map from generic tool name to `true` when the user chose to skip it |

**Response result:**
```json
{
  "fields": [
    {"key": "tools", "value": {"kind": "list", "list": "block", "items": [...]}}
  ],
  "resolutions": [
    {"generic": "file_read", "outcome": "mapped", "harness_tools": ["read"]},
    {"generic": "terminal", "outcome": "mapped", "harness_tools": ["bash"]},
    {"generic": "user_interaction", "outcome": "mapped", "harness_tools": ["question"]}
  ]
}
```

The `resolutions` array MUST contain exactly one entry per entry in `generic`, in the same order. Each entry's `generic` field MUST match the corresponding request entry. The `outcome` field is one of: `"mapped"`, `"custom"`, `"skipped"`, `"unmapped"`.

### frontmatter

Build the ordered list of field operations to apply to an agent's frontmatter.

**Request params:**
```json
{
  "kind": "agent",
  "agent_key": "test-runner",
  "source": [...],
  "model": {"model_id": "provider/model", "origin": "harness-list"},
  "tool_fields": [...],
  "versions": {
    "version": "1.0.0",
    "transform_version": "3.0.0",
    "injections_version": "1.3.1"
  }
}
```

**Response result:**
```json
{
  "set": [
    {"key": "mode", "value": {"kind": "scalar", "scalar": "subagent"}}
  ],
  "remove": ["name", "recommended_tier", "tier_rationale", "required_skills", "tools"],
  "key_order": ["id", "version", "transform_version", "injections_version", "description", "mode", "model", "permission"]
}
```

A key MUST NOT appear in both `set` and `remove`. A key MUST NOT appear in both `key_order` and `remove`.

### target_path

Resolve the deployment path for one artifact.

**Request params:**
```json
{
  "kind": "agent",
  "key": "test-runner",
  "file_name": "",
  "scope": "project",
  "goos": "linux"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `kind` | string | Artifact kind: `"agent"`, `"skill"`, or `"hook"` |
| `key` | string | Artifact slug |
| `file_name` | string | Source filename; relevant for skills and hooks |
| `scope` | string | `"project"` or `"user"` |
| `goos` | string | Target OS as a Go `GOOS` value (e.g. `"linux"`, `"darwin"`, `"windows"`) |

**Response result (success):**
```json
{"path": ".myharness/agents/test-runner.md"}
```

**Response result (unsupported):**
```json
{"error": {"code": "unsupported_artifact", "message": "kind 'workflow' not supported"}}
```

Use `"unsupported_artifact"` as the error code for any artifact kind your harness does not support. MOSAIC maps this to `domain.ErrArtifactUnsupported`.

### injection

Return the harness-level content for one canonical injection name.

**Request params:**
```json
{"name": "HarnessConstraints"}
```

**Response result:**
```json
{"content": "- **Example constraint:** ...", "ok": true}
```

Set `"ok": false` (and `"content": ""`) for injection names your harness does not fill. MOSAIC leaves those injection points empty.

Canonical injection names: `HarnessConstraints`, `LanguagePatterns`, `IdentityExtension`, `ProtocolExtension`, `CodebaseContext`, `OutputArtifactTemplate`, `CustomConstraints`, `ErrorHandlingExtension`, `ContextLimits`, `AvailableWorkflows`.

### hook_plan

Resolve a hook bundle deployment plan.

**Request params:**
```json
{
  "bundle": {
    "key": "subagent-logger",
    "version": "1.0.0",
    "variants": {
      "my-harness": {
        "harness_id": "my-harness",
        "supported": true,
        "files": [
          {"source_path": "/path/to/hook.ts", "target_name": "subagent-logger.ts"}
        ],
        "registration": []
      }
    }
  },
  "scope": "project"
}
```

**Response result (supported):**
```json
{
  "supported": true,
  "target_dir": ".myharness/plugins",
  "files": [
    {"source_path": "/path/to/hook.ts", "target_name": "subagent-logger.ts"}
  ],
  "registration": []
}
```

**Response result (unsupported):**
```json
{
  "supported": false,
  "reason": "my-harness does not support hook bundles"
}
```

When `supported` is `false`, `reason` MUST be non-empty and `files` MUST be absent or empty.

## FieldValue Wire Format

`FieldValue` appears in tool fields, frontmatter set operations, and source frontmatter. The `kind` discriminates the structure:

**Scalar:**
```json
{"kind": "scalar", "scalar": "subagent", "quote": ""}
```
`quote` is `""` (plain), `"single"`, or `"double"`.

**List:**
```json
{"kind": "list", "list": "block", "items": [
  {"kind": "scalar", "scalar": "read", "quote": ""}
]}
```
`list` is `"block"` (one item per line) or `"flow"` (inline `[a, b, c]`). Always set `list` explicitly; do not omit it.

**Mapping:**
```json
{"kind": "mapping", "pairs": [
  {"key": "read", "value": {"kind": "scalar", "scalar": "allow", "quote": ""}},
  {"key": "write", "value": {"kind": "scalar", "scalar": "deny", "quote": ""}}
]}
```
Pairs are order-significant.

## Process Lifecycle

1. MOSAIC starts the module executable with its stdin and stdout connected to the adapter's pipes.
2. The module reads one JSON line from stdin (the handshake), writes its response, and enters its request loop.
3. For each request, the module reads one JSON line, writes one JSON response, and loops.
4. When MOSAIC is done, it closes its end of stdin. The module SHOULD exit cleanly when it detects EOF on stdin.
5. MOSAIC kills the module after a configurable timeout (default: 30 seconds per request) if no response arrives.

The module SHOULD NOT write anything to stdout before receiving a request. Any output before the handshake response will be treated as a malformed response.

## Error Handling

- If a method is not recognised, respond with `{"code": "unsupported_method", "message": "..."}`.
- If params cannot be decoded, respond with `{"code": "bad_params", "message": "..."}`.
- For internal errors, respond with `{"code": "internal", "message": "..."}`.
- The module MUST NOT exit non-zero in response to a normal request error. Only crash on unrecoverable errors.

## Subprocess Stderr

The module may write diagnostic output to stderr. MOSAIC captures stderr and includes it in error messages when the module exits non-zero, so diagnostic messages are useful for debugging. MOSAIC does not process stderr for protocol purposes.
