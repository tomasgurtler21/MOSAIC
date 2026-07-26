# Harness Contributor Guide

This guide explains how to build a custom harness module for MOSAIC. A harness defines how MOSAIC transforms generic agent definitions into harness-specific deployable files. Three provision tiers exist: **built-in** (compiled into MOSAIC), **descriptor-only** (declared in a YAML file with no code), and **external** (a standalone executable).

Read this guide to decide which tier fits your harness, then follow the step-by-step instructions for your chosen approach.

## Which Tier Should You Use?

### Descriptor-only (recommended for most cases)

If your harness can be fully described by:
- A mapping from generic tool names to harness tool names
- Static frontmatter additions and removals
- Fixed deployment path templates

…then a descriptor-only harness requires no code at all. Create a single YAML file and register it with MOSAIC. See [Descriptor-only harness](#descriptor-only-harness).

### External module

If your harness needs:
- Logic that varies by agent key (e.g. orchestrators get a different configuration)
- Dynamic tool expansion based on runtime state
- Complex path generation
- Integration with an external system

…then build an external module: a standalone executable that MOSAIC launches as a subprocess. See [External module](#external-module).

### Built-in (internal only)

Built-in harnesses are compiled into MOSAIC's binary. This is only for harnesses maintained in the MOSAIC repository itself (OpenCode, Claude Code, etc.). Third-party contributors should use the external module tier instead.

---

## Descriptor-only Harness

### Create the descriptor file

Create a YAML file anywhere MOSAIC can load it. The file must follow the [descriptor schema](descriptor-schema.md). A minimal example:

```yaml
schema_version: "1"
id: my-harness
display_name: My Harness

tools:
  shape: list
  universe:
    - name: read_file
    - name: write_file
    - name: run_command
  mappings:
    - generic: file_read
      harness_tools: [read_file]
    - generic: file_write
      harness_tools: [write_file]
    - generic: file_edit
      harness_tools: [write_file]
    - generic: terminal
      harness_tools: [run_command]

paths:
  agents:
    project: .myharness/agents
    user:
      "": ~/.myharness/agents
  skills:
    supported: false

frontmatter:
  drop:
    - name
    - recommended_tier
    - tier_rationale
    - required_skills
    - tools
  key_order:
    - id
    - version
    - description
    - model
    - tools
```

### Register with MOSAIC

Point MOSAIC at your descriptor file using the `--harness` flag:

```
mosaic-deploy --harness /path/to/my-harness.yaml deploy
```

Or register it in your project's MOSAIC configuration file.

### Test it

Run MOSAIC's built-in conformance check to verify your descriptor is well-formed:

```
mosaic-deploy --harness /path/to/my-harness.yaml validate
```

---

## External Module

An external module is a subprocess that MOSAIC communicates with using the [JSON-over-stdio protocol](external-protocol.md). You can write it in any language.

### Prerequisites

Read the full [external protocol specification](external-protocol.md) before implementing. The key points:
- One JSON object per line on stdin (request) and stdout (response)
- Protocol version `"1.0"` on every message
- First exchange must be the `handshake` method
- Module exits when MOSAIC closes its stdin

### Step 1: Implement the handshake

Your executable must respond to the `handshake` method as its first exchange:

```
Request:  {"protocol":"1.0","id":"1","method":"handshake","params":{"protocol":"1.0"}}
Response: {"protocol":"1.0","id":"1","result":{"protocol":"1.0","harness":{"id":"my-harness","display_name":"My Harness","tier":"external","usable":true}}}
```

If your harness fails to start (e.g. missing configuration), set `"usable": false` in the handshake response and MOSAIC will surface the error to the user.

### Step 2: Implement each method

Implement the methods your harness needs. The required methods are:

| Method | Required | Description |
|--------|----------|-------------|
| `handshake` | Yes | Protocol negotiation and identity declaration |
| `tools` | Yes | Map generic tools to harness-specific fields |
| `frontmatter` | Yes | Declare frontmatter operations (adds, removes, key order) |
| `target_path` | Yes | Resolve deployment path for an artifact |
| `injection` | Yes | Return harness-level injection content |
| `hook_plan` | Yes | Resolve hook bundle deployment plan |

For methods you don't implement meaningfully (e.g. `injection` if you fill no injections), respond with the appropriate "not filled" or "unsupported" result rather than an error.

### Step 3: Add a descriptor YAML

Even external modules must provide a descriptor file. The descriptor carries the static information MOSAIC needs (display name, path templates, model list) that does not require code. The module's handshake response is used for runtime identity; the descriptor is loaded at startup.

Create a descriptor YAML ([schema](descriptor-schema.md)) alongside your module.

### Step 4: Write a wrapper script or binary

MOSAIC launches the module as a subprocess. On Windows, `.bat` files are supported automatically (MOSAIC invokes them via `cmd /c`). On Unix, any executable works.

If your module is a script, ensure it is executable (`chmod +x`) and has a proper shebang line.

### Step 5: Test with the reference implementation

MOSAIC ships with a reference external module (`cmd/harness-opencode-module`) that wraps the built-in OpenCode harness. You can use it to verify that your integration pipeline works before implementing your own logic:

```
go build -o /tmp/opencode-module mosaic-deploy/cmd/harness-opencode-module
```

Start MOSAIC with the reference module:
```
mosaic-deploy --harness-module /tmp/opencode-module --harness opencode.yaml deploy
```

### Step 6: Verify against the contract test suite

MOSAIC includes a shared contract test (`internal/harness/contracttest`) that all provision tiers must pass. When you wrap your module with the `external.New` adapter in a Go test, run:

```go
contracttest.Run(t, m, contracttest.Fixtures{
    ToolCases:        [...],
    FrontmatterCases: [...],
    InjectionCases:   {...},
    TargetPathCases:  [...],
    HookPlanCases:    [...],
})
```

Pass full fixtures that reflect your harness's expected behaviour. The contract test verifies universal invariants (determinism, nil safety, error sentinel correctness) and per-method output correctness.

---

## FieldValue Encoding

Both descriptor-only and external modules must produce `FieldValue` objects in the correct wire format. The three kinds are:

**Scalar** (a simple text value):
```json
{"kind": "scalar", "scalar": "allow", "quote": ""}
```
Set `"quote"` to `""` for unquoted values, `"single"` for `'value'`, `"double"` for `"value"`.

**List** (a sequence of values):
```json
{"kind": "list", "list": "block", "items": [
  {"kind": "scalar", "scalar": "read_file", "quote": ""}
]}
```
Always set `"list"` explicitly: `"block"` for block-style YAML (one item per line), `"flow"` for inline `[a, b, c]`. Never omit `"list"`.

**Mapping** (key-value pairs, order-significant):
```json
{"kind": "mapping", "pairs": [
  {"key": "read", "value": {"kind": "scalar", "scalar": "allow", "quote": ""}},
  {"key": "write", "value": {"kind": "scalar", "scalar": "deny", "quote": ""}}
]}
```

---

## Tools Method: Shape Variants

The `tools` method can use two shapes:

**List shape** (most harnesses): emit a list field with harness tool names.
```json
{
  "fields": [{"key": "tools", "value": {"kind": "list", "list": "block", "items": [
    {"kind": "scalar", "scalar": "read_file", "quote": ""}
  ]}}],
  "resolutions": [{"generic": "file_read", "outcome": "mapped", "harness_tools": ["read_file"]}]
}
```

**Permission shape** (OpenCode-style): emit a mapping where every tool in the universe appears with `"allow"` or `"deny"`.
```json
{
  "fields": [{"key": "permission", "value": {"kind": "mapping", "pairs": [
    {"key": "read_file", "value": {"kind": "scalar", "scalar": "allow", "quote": ""}},
    {"key": "write_file", "value": {"kind": "scalar", "scalar": "deny", "quote": ""}}
  ]}}],
  "resolutions": [{"generic": "file_read", "outcome": "mapped", "harness_tools": ["read_file"]}]
}
```

The `resolutions` array must always have one entry per entry in `request.generic`, in the same order.

---

## Common Mistakes

**Omitting `list` style on list values**: MOSAIC requires an explicit `"list"` field (`"block"` or `"flow"`). Omitting it produces non-deterministic YAML output. Always set it.

**Wrong resolution count**: `resolutions` must have exactly as many entries as `request.generic`, in the same order. Extra or missing entries are contract violations.

**Exiting non-zero on soft errors**: A request error (unsupported artifact kind, bad params) must be communicated as a JSON error response, not a process exit. Only exit non-zero for unrecoverable failures.

**Writing to stdout before handshake response**: Any output before the first handshake response will be treated as a malformed response. Write nothing until you are ready to respond to the handshake.

**Protocol version mismatch**: Always echo `"1.0"` in the `protocol` field of every response, including the handshake. A different value causes MOSAIC to reject the module with `ErrProtocolMismatch`.

---

## Testing Checklist

Before submitting a harness, verify:

- [ ] Descriptor YAML is valid (`mosaic-deploy --harness myharness.yaml validate`)
- [ ] All generic tool mappings are declared
- [ ] Deployment paths are tested for `ScopeProject` and `ScopeUser` on the target platforms
- [ ] `injection("HarnessConstraints")` returns your constraints (or `ok=false` if none)
- [ ] `hook_plan` returns `Supported=false` with a non-empty `Reason` if hooks are not supported
- [ ] `TargetPath` for an unknown artifact kind returns `ErrArtifactUnsupported` (code `"unsupported_artifact"`)
- [ ] Each method returns consistent results on repeated calls with equal input (determinism)
- [ ] `Close()` (or subprocess exit on stdin EOF) does not hang or panic

For external modules, additionally:

- [ ] Handshake correctly echoes `"1.0"` in the protocol field
- [ ] Module exits cleanly when stdin is closed
- [ ] Module does not write to stdout before the handshake response
- [ ] Subprocess stderr contains useful diagnostic information when exiting non-zero
