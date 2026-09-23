# Harness Package

> Part of: mosaic-run
> Responsibility: Implements the `domain.HarnessAdapter` port for all three supported harnesses (Claude Code, GHCP CLI, OpenCode) plus a `FakeAdapter` for test use, and provides protocol serialization helpers.

## Overview

The `harness` package is a thin adapter layer. Each concrete adapter converts `domain.AgentReference` and `domain.ProtocolRequest` into the neutral `mosaic-common/harness.SpawnRequest` type, delegates subprocess spawning to the shared `mosaic-common/harness` package (which owns CLI argument construction, subprocess lifecycle, and output-envelope parsing), and converts results back to `domain.ProtocolResponse`.

All three production adapters share the same `mosaic-common/harness` spawner infrastructure and the same error taxonomy (`ErrExecutableNotFound`, `ErrNonZeroExit`, `ErrTimeout`, `ErrEmptyResponse`, `ErrMalformedJSON`, `ErrMalformedOutput`). Adapter-specific sentinels are aliased onto the shared module's constants so `errors.Is` works across the package boundary.

## Components

| Component | Purpose |
|-----------|---------|
| `ClaudeCodeAdapter` | Spawns Claude Code CLI per invocation. Extracts the agent's `tools` frontmatter field before spawning and passes tool names to `BuildArgs` via `SpawnRequest.DerivedTools`. Sets `SpawnRequest.ToolsDerived=true` after a successful extraction call so `BuildArgs` knows derivation was performed. Uses `--permission-mode dontAsk` + `--allowedTools` when tools are present; uses `--permission-mode dontAsk` with no `--allowedTools` when the tools field is empty (strict mode, no tools permitted); rejects the invocation before spawning only when the tools field is missing. |
| `GHCPCLIAdapter` | Spawns GHCP CLI per invocation. Supports two permission modes: Blanket (`--yolo --no-ask-user`) and Partial Allowlist (`--allow-tool` entries from `SpawnRequest.DerivedTools` + `--no-ask-user`). Mode is resolved once at adapter construction and stored on the struct. |
| `OpenCodeAdapter` | Spawns OpenCode per invocation. Always passes `--auto`, which converts every `ask` permission to `allow` for that invocation while leaving explicitly-denied capabilities unchanged. No per-tool allowlist is extracted or needed. |
| `FakeAdapter` | Test double. Queues scripted `ProtocolResponse`, `error`, or raw JSON payloads per agent identifier (FIFO). Records all invocations so tests can assert call order and arguments. |
| Protocol helpers | `MarshalRequest` / `UnmarshalResponse` encode and decode Communication Protocol v1.8 JSON messages. |

## Permission-Mode Implementation

### Claude Code: dontAsk + allowedTools

**Before:** `BuildArgs` always emitted `--permission-mode auto` regardless of which agent was being invoked.

**After:**
- `ClaudeCodeAdapter.Invoke` and `InvokeRaw` call `ExtractClaudeCodeTools(agent.DefinitionPath)` (from `mosaic-common/harness`) before building the `SpawnRequest`.
- The returned tool names are placed in `SpawnRequest.DerivedTools`, and `SpawnRequest.ToolsDerived` is set to `true`. `BuildArgs` reads `DerivedTools` (never `AllowedTools`) and emits `--permission-mode dontAsk` followed by `--allowedTools <name>` for each entry. When `DerivedTools` is empty but `ToolsDerived` is `true`, `BuildArgs` still emits `--permission-mode dontAsk` with zero `--allowedTools` entries (strict mode, no tools permitted -- the intended behavior for agents with all-ungated or empty tool lists).
- When `ExtractClaudeCodeTools` returns `ErrToolsMissing`, the adapter returns a wrapped error **before spawning any process**. The error message includes the agent identifier and definition path.
- When `ExtractClaudeCodeTools` returns `([]string{}, nil)` for an empty tools field, the invocation proceeds normally with `ToolsDerived=true` and an empty `DerivedTools` slice.
- Backward compatibility for non-Runner callers (AgentTest): `BuildArgs` falls back to `--permission-mode auto` when `DerivedTools` is nil/empty AND `ToolsDerived` is `false` (the zero value), so AgentTest's invocations are unaffected without any code change on its side.
- `--dangerously-skip-permissions` is never used.

**Non-collision guarantee:** `SpawnRequest.AllowedTools` (populated by AgentTest from a test-authored `allowed_tools` value) and `SpawnRequest.DerivedTools` (populated by Runner from the agent's deployed frontmatter) are separate fields. No builder reads both. This structural separation guarantees that Runner's deterministic permission mode cannot alter AgentTest's invocation behavior.

**The deployed `tools` field for Claude Code** is a comma-separated scalar string in the agent's frontmatter (e.g., `"Read, Write, Edit, Bash"`). `ExtractClaudeCodeTools` splits on commas, trims whitespace, and returns the individual tool names.

### GHCP CLI: Blanket vs Partial Allowlist

**Before:** `BuildGHCPCLIArgs` always emitted `--yolo --no-ask-user` unconditionally.

**After:**
- A `GHCPCLIPermissionMode` type (`"blanket"` | `"allowlist"` | `""`) stored on `GHCPCLIAdapter.mode` selects the behavior:
  - **Blanket** (`GHCPCLIModeBlanket`): `--yolo --no-ask-user` -- identical to prior behavior. `DerivedTools` is ignored.
  - **Partial Allowlist** (`GHCPCLIModePartialAllowlist`): per-tool `--allow-tool <kind>` entries from `DerivedTools` + `--no-ask-user`. `--yolo` is absent.
  - **Unresolved** (zero value): `BuildGHCPCLIArgs` returns `ErrGHCPCLIModeUnresolved` immediately, before building any args. The Runner rejects a GHCP CLI run that reaches this point without a resolved mode.
- When Partial Allowlist mode is active, the adapter always sets `SpawnRequest.ToolsDerived=true` after a successful extraction call. When `DerivedTools` is empty and `ToolsDerived` is `true` (all-ungated or explicitly empty tools list), `BuildGHCPCLIArgs` emits `--no-ask-user` with zero `--allow-tool` entries -- this is valid. When `DerivedTools` is nil/empty AND `ToolsDerived` is `false`, `BuildGHCPCLIArgs` returns `ErrGHCPCLIAllowlistEmpty` (defense-in-depth guard for callers that did not perform derivation, e.g., if the extraction step was somehow bypassed).

**Mode resolution path:**
- Interactive (TUI): A GHCP CLI mode-selection screen presents Blanket / Partial Allowlist to the user. The selection is stored in `ConfigSelection.GHCPCLIMode`.
- Non-interactive (CLI): The `--ghcp-permission-mode` flag accepts `blanket` or `allowlist`.
- Both paths converge in `buildAdapter` in `cmd/mosaic-run/main.go`, which calls `NewGHCPCLIAdapterWithMode(executablePath, timeout, logger, mode)`.
- Existing call sites use `NewGHCPCLIAdapter` (no mode arg) or `NewGHCPCLIAdapterWithLogger`, which default to `GHCPCLIModeBlanket`. This preserves backward compatibility for tests.

**The deployed `tools` field for GHCP CLI** is a flow-style YAML list (e.g., `['read', 'edit', 'search', 'execute', 'ask_user', 'agent']`). `ExtractGHCPCLITools` applies a translation table before returning:

| Deployed name | GHCP CLI `--allow-tool` kind |
|---------------|------------------------------|
| `edit`        | `write`                      |
| `execute`     | `shell`                      |
| `agent`       | `agent` (pass-through)       |
| `skill`       | `skill` (pass-through)       |
| `read`        | (excluded -- reads are ungated in GHCP CLI) |
| `search`      | (excluded -- GHCP CLI auto-allows search)  |
| `ask_user`    | (excluded -- handled by `--no-ask-user` separately) |

### OpenCode: --auto (no change)

OpenCode does not require a per-tool allowlist. `BuildOpenCodeArgs` always emits `--auto`, which converts every `ask` permission to `allow` for the duration of that invocation while leaving explicitly-denied capabilities (e.g., `.env` file access) unchanged. No `DerivedTools` extraction is performed, and no new fields are set on `SpawnRequest` for the OpenCode adapter. `req.AllowedTools` and `req.DerivedTools` are both ignored by `BuildOpenCodeArgs`.

## Constructors

| Constructor | Description |
|-------------|-------------|
| `NewClaudeCodeAdapter(path, timeout)` | Logging disabled. Default behavior. |
| `NewClaudeCodeAdapterWithLogger(path, timeout, logger)` | Adds debug logging. |
| `NewGHCPCLIAdapter(path, timeout)` | Logging disabled. Defaults to `GHCPCLIModeBlanket`. |
| `NewGHCPCLIAdapterWithLogger(path, timeout, logger)` | Adds debug logging. Defaults to `GHCPCLIModeBlanket`. |
| `NewGHCPCLIAdapterWithMode(path, timeout, logger, mode)` | Explicit permission-mode selection. Used by `cmd/mosaic-run` for all new runs. |
| `NewOpenCodeAdapter(path, timeout)` | Logging disabled. |
| `NewOpenCodeAdapterWithLogger(path, timeout, logger)` | Adds debug logging. |

## Error Taxonomy

All three adapters share the same base sentinel set (aliased from `mosaic-common/harness`):

| Sentinel | Meaning |
|----------|---------|
| `ErrExecutableNotFound` | Binary not found or not executable. Wrapped in `domain.HarnessLaunchError` so the TUI override screen can identify it by type. |
| `ErrNonZeroExit` | Process exited non-zero (Claude Code only -- the other two report failure via stream events). |
| `ErrTimeout` | Invocation exceeded configured timeout. |
| `ErrEmptyResponse` | CLI produced no stdout. |
| `ErrMalformedJSON` | stdout is not valid JSON. |
| `ErrMalformedOutput` | stdout is valid JSON but the protocol response could not be located. |

Additional sentinels:

| Sentinel | Harness | Meaning |
|----------|---------|---------|
| `ErrStreamError` | OpenCode | Stream event reported the run failed. |
| `ErrStreamIncomplete` | OpenCode | Stream ended without a terminal event. |
| `ErrGHCPCLIStreamError` | GHCP CLI | Terminal result event reported the run failed. |
| `ErrGHCPCLIStreamIncomplete` | GHCP CLI | Stream ended without a terminal result event. |
| `ErrToolsMissing` | Claude Code, GHCP CLI (Partial) | Agent definition has no `tools` field in frontmatter. |
| `ErrToolsEmpty` | Claude Code, GHCP CLI (Partial) | `tools` field is present but has a wrong shape (not the expected kind) or contains only unrecognised names (misconfiguration). An empty tools field is no longer an error -- it returns `([]string{}, nil)` and the adapter proceeds with `ToolsDerived=true`. |
| `ErrGHCPCLIModeUnresolved` | GHCP CLI | `GHCPCLIMode` is zero (mode not resolved before spawning). |
| `ErrGHCPCLIAllowlistEmpty` | GHCP CLI | Partial Allowlist mode, `DerivedTools` is nil/empty, AND `ToolsDerived` is `false`. Guards against callers that skipped tool derivation entirely. |

## Key Invariants

- No adapter ever uses `--dangerously-skip-permissions` (Claude Code) or any equivalent blanket bypass.
- `SpawnRequest.AllowedTools` is never read by any arg builder. Only `DerivedTools` is read (when the builder supports it).
- The OpenCode adapter never calls `ExtractClaudeCodeTools` or `ExtractGHCPCLITools`. Permission handling is fully covered by `--auto`.
- A missing `tools` field causes a pre-spawn rejection (`ErrToolsMissing`) for Claude Code and for GHCP CLI in Partial Allowlist mode, but is a no-op for GHCP CLI in Blanket mode and entirely irrelevant for OpenCode. An empty `tools` field (empty scalar, zero-item list) is accepted for both Claude Code and GHCP CLI Partial Allowlist: the adapter sets `ToolsDerived=true` and the invocation proceeds in strict dontAsk/no-ask-user mode with no tool entries.
- `SpawnRequest.ToolsDerived` disambiguates an empty `DerivedTools` slice: `true` means derivation ran and produced no gated tools (valid); `false` (zero value) means derivation was not performed (backward-compatible guard applies). AgentTest never sets `ToolsDerived` and is unaffected by this signal. Note: OI-7 (Claude Code dontAsk with empty --allowedTools actually denies all tools) is an unverified assumption at implementation time. If Claude Code treats an empty allowlist differently (e.g., grants all tools), the symptom would be unexpected tool access rather than a crash. Surface this during manual testing if agent behavior seems inconsistent with the intent.
- Context cancellation always returns `ctx.Err()` to the caller, regardless of which harness-level error was originally produced, preserving Runner's existing cancellation semantics.
