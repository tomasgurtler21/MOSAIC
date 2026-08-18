# Harness System

> Part of: mosaic-deploy
> Responsibility: Abstracts over every way a harness implementation can be supplied to the tool, and supplies the shared algorithms that keep all provision tiers behaviourally identical.

## Overview

A "harness" (Claude Code, GitHub Copilot CLI, OpenCode, VS Code GHCP, or a
third-party harness someone else authors) is anything that can implement the
single `domain.HarnessModule` port: map generic tools to harness-specific
ones, shape frontmatter, resolve deployment paths, supply injection content,
and plan hook deployment. This area exists so that `transform`, `plan`, and
`deploy` never need to know *how* a harness is provided — only that they hold
a `domain.HarnessModule`.

Three tiers can provide a `HarnessModule`, in ascending precedence:

1. **Built-in** — Go code compiled into the binary, one sub-package per
   harness, each embedding its own `harness.yaml` descriptor and registering
   itself via `init()`.
2. **Descriptor-only** — a `harness.yaml` dropped on disk under
   `<MosaicRoot>/MosaicDeploy/harnesses/<folder>/` with no code at all. Every
   method is driven purely by shared, descriptor-interpreting algorithms.
3. **External** — a descriptor-only folder that additionally ships an
   executable (`harness-exec[.exe|.bat]`). The executable speaks a JSON-over-stdio
   protocol so a harness can be implemented in any language, out of process.
   `registry.Discover` spawns the executable via `external.New` when the external
   tier is admitted, routing every `HarnessModule` method call to the subprocess.

A fourth package, `contracttest`, is not a provision tier — it is the shared
assertion suite every tier's own test package drives its module through, so
no tier hand-writes its own copy of "does this obey the `HarnessModule`
contract" checks.

## Components / Subdomains

| Component | Purpose |
|-----------|---------|
| **descriptor** | Owns the YAML wire format for `harness.yaml` (`Load`/`Parse`/`Validate`), maps it onto the tag-free `domain.HarnessDescriptor`, and exports the three shared algorithms every tier ultimately delegates to: `MapTools`, `ApplyFrontmatterSpec`, `ResolveTargetPath`. |
| **registry** | `Discover` walks built-in factories plus on-disk harness folders, applies tier precedence, and returns a `Registry` whose `List`/`Resolve` are the only way any other package obtains a `HarnessModule`. Also holds `runtimeModule`, the descriptor-driven implementation used for the descriptor-only tier. External-tier harnesses are constructed via `external.New` instead. |
| **builtin** | Parent namespace for four harness sub-packages (`claudecode`, `ghcpcli`, `opencode`, `vscodeghcp`). Each embeds its own descriptor YAML and adds the minimal module code needed for behaviour the descriptor schema cannot express. |
| **external** | Client-side implementation of the JSON-over-stdio protocol: `external.New` spawns a subprocess, performs a version handshake, and forwards every `HarnessModule` method as one JSON request/response line. Defines the full error taxonomy for subprocess failure modes. |
| **contracttest** | `contracttest.Run(t, module, fixtures)` drives universal invariants unconditionally, plus optional per-method sub-suites (`ToolCases`, `FrontmatterCases`, `InjectionCases`, `TargetPathCases`, `HookPlanCases`) when fixtures are supplied. |

## Key Flows

### Descriptor loading and validation
`descriptor.Load`/`Parse` unmarshals YAML into an unexported `wireDescriptor`
(the only type with `yaml` struct tags), rejects unknown fields, checks
`schema_version` against `SupportedSchemaVersions` (currently only `"1"`) as
a distinct error from a missing version, maps the wire struct onto
`domain.HarnessDescriptor`, then runs `Validate` for semantic checks (required
fields present, no duplicate `tools.mappings[].generic` entries). Multiple
validation problems collapse into one returned error whose message notes how
many more exist, so descriptor authors see one File:Line:Field per fix cycle
but know there is more to do.

### Tool mapping (`MapTools`)
The single implementation of "map an agent's generic tool list to
harness-specific names" for every tier. Per generic tool, in priority order:
explicitly skipped by the user → user-supplied custom name (via
`CustomToolTemplate`) → an entry in `descriptor.Tools.Mappings` (present even
with an empty `HarnessTools`, meaning "explicitly unsupported") → unmapped.
Output rendering then branches on `ToolSpec.Shape`:
- **List shape** (`buildListToolFields`) — by-convention tools are always
  included; mapped/custom tools are deduplicated and sorted by descriptor
  `Universe` order; tools with a non-empty `Field` on their mapping are
  diverted into their own separate frontmatter key instead of the main list
  (e.g. an MCP-server-style secondary field).
- **Permission shape** (`buildPermissionToolFields`) — every tool in the
  descriptor's `Universe` appears exactly once as a key/value pair
  (`allow` when resolved or by-convention, otherwise the descriptor's
  declared `Unused` disposition), always in `Universe` order.

A separate placeholder path (`req.Placeholder` non-empty, `req.Generic`
empty) expands `PlaceholderExpansion` (or the full `Universe` if that list is
empty) into a flow-style list — this is how orchestrator/utility agents that
declare the `{tool-permissions}` placeholder get a full tool set without a
harness needing bespoke placeholder code.

### Frontmatter shaping (`ApplyFrontmatterSpec`)
Translates a descriptor's static `frontmatter.add` / `frontmatter.drop` /
`frontmatter.key_order` declarations directly into a `domain.FrontmatterPlan`
(`Set`, `Remove`, `KeyOrder`). The module never edits a document itself —
`transform` applies the plan via `docformat`. This keeps the port
implementable across a process boundary (the external tier ships the same
plan shape over JSON).

### Harness discovery and precedence (`registry.Discover`)
1. Snapshots all built-in factories registered via `init()` and constructs
   each (cheap, in-memory) to obtain its `HarnessRef`.
2. Reads every immediate subdirectory of
   `<MosaicRoot>/MosaicDeploy/harnesses/`; folders without a `harness.yaml`
   are silently skipped. A folder additionally containing a recognised
   executable (`harness-exec.exe`/`harness-exec.bat` on Windows,
   `harness-exec` elsewhere) is tagged `TierExternal`; otherwise
   `TierDescriptor`.
3. Merges candidates by id with deterministic precedence
   **external > descriptor-only > built-in** — a runtime-provisioned harness
   overrides a shipped built-in with the same id, enabling harness patching
   without a rebuild. Precedence never depends on map iteration order (ids
   are sorted before merge).
4. A descriptor that fails to load is still listed (with `Usable: false` and
   a human-readable `UnusableReason`) rather than silently disappearing, so
   the harness-selection UI can explain why.
5. An external-tier harness is listed regardless of opt-in status, but
   `Usable` is only true when `Options.AllowExternal` was set; `Resolve`
   re-checks the same gate independently of how the entry was listed, so no
   caller can obtain an external module purely by discovering it first.

### External module protocol (client side)
`external.New(execPath, descriptor, opts)` spawns the executable over
anonymous pipes, sends a `handshake` request carrying `ProtocolVersion`
("1.0"), and requires the reply to echo the same version — any mismatch
(`ErrProtocolMismatch`) makes the harness unusable at resolution time, not
mid-transform. Every subsequent `HarnessModule` method becomes one
`{protocol, id, method, params}` JSON line out and one
`{protocol, id, result|error}` JSON line back, using wire mirrors of
`domain.FieldValue`/`FrontmatterField`/`HookBundle` etc. that round-trip the
Kind/Scalar/Quote/Items/Pairs/List/Comment shape.

Error taxonomy distinguishes: missing/non-executable file
(`ErrExecutableNotFound`, checked at `New` time), a process that exits
non-zero before answering (`ErrNonZeroExit`, exit code and captured stderr
included), a process that closes stdout without writing anything
(`ErrEmptyResponse`), unparseable JSON (`ErrMalformedResponse`), and a
request that outruns the configured timeout (`ErrTimeout`, default 30s —
the process is killed). A Go-runtime deadlock message on stderr (bare
`select {}` in a fake test harness) is deliberately reclassified as
`ErrTimeout` rather than `ErrNonZeroExit` so cross-platform fake harnesses in
tests behave identically to a real hang.

`Close()` kills the subprocess and is idempotent; calling any method after
`Close()` transparently reconnects (spawns a fresh subprocess, re-handshakes)
rather than erroring. This exists specifically so `contracttest.Run` — which
calls `Close()` as part of its universal invariants and then keeps exercising
per-method behaviour on the same module value — works unmodified against the
external tier.

### Built-in module exceptions
All four built-in harnesses are thin wrappers around the same shared
algorithms; each documents (in its package doc comment) the small number of
things its descriptor genuinely cannot express and that require Go code:

| Harness | Descriptor-inexpressible behaviour |
|---------|-------------------------------------|
| **claudecode** | Tools rendered as a comma-separated scalar (not a YAML list); skill path key-subdirectory composition; version stamps applied later by transform, not by the module. |
| **ghcpcli** | Tools rendered as a flow-style, single-quoted YAML sequence (descriptor `FieldValue` has no combined flow+per-item-quote option); same skill-subdirectory and version-stamp notes; hooks unsupported entirely. |
| **opencode** | Tools replaced by a full permission mapping; `mode: subagent` vs `mode: primary` depends on whether the agent is the orchestrator (inspects `AgentKey`, which a static descriptor `Add` field cannot do); distinct key order; hooks deployed as plugins with no registration steps. Explicitly designed with no in-process-only assumptions so the identical logic can run as the external reference module (`cmd/harness-opencode-module`). |
| **vscodeghcp** | Same flow-style/single-quote tool rendering and skill-subdirectory notes as ghcpcli; hook variant reuses the `claude-code` variant's files (resolved upstream by the catalog, not by module code); the "enable chat hooks" registration step is always `Performable: false` because it requires a user-level VS Code setting the tool cannot write, so it always surfaces as a TODO item. |

### Contract testing
Every built-in, the descriptor-only adapter, and the external adapter are
driven through `contracttest.Run` in their own `_test.go` files with
harness-specific `Fixtures`. Universal invariants (checked unconditionally,
no fixtures required) include: non-empty and stable `Ref().ID`; same
`Descriptor()` pointer across calls; one `ToolResolution` per requested
generic tool in request order; `FrontmatterPlan.KeyOrder` and `.Remove` never
overlap; `TargetPath` for an unknown artifact kind always returns
`domain.ErrArtifactUnsupported` and never a non-empty path with a nil error;
`Injection` returns `ok=false` for any name outside
`docformat.CanonicalInjections`; an unsupported `HookPlan` always carries a
non-empty `Reason` and no `Files`; every method is deterministic across two
identical calls; `Close()` is nil-returning and idempotent. This is what lets
the system claim "built-in, descriptor-only, and external implementations
are indistinguishable to every consumer" without every tier re-testing that
claim independently.

## Relationships

| Talks To | For |
|----------|-----|
| **domain** | The only port interfaces (`HarnessModule`, tool/frontmatter/path/hook request-response shapes) and the tag-free `HarnessDescriptor`/`FieldValue` vocabulary this whole area is built around. |
| **docformat** | `contracttest` imports `docformat.CanonicalInjections` to assert unknown injection names are correctly rejected by every tier. |
| **catalog** | Resolves hook variant reuse (e.g. `vscode-ghcp` reusing `claude-code`'s files) before calling a module's `HookPlan`, so modules never implement reuse logic themselves. |
| **transform / plan / deploy** | Obtain a `HarnessModule` exclusively through `registry.Resolve`; never construct or import a `builtin/*` package directly. |
| **cmd/mosaic-deploy** | The only place `registry.Discover` is called in production, wiring `AllowExternal` from the CLI flag or `tool-config.yaml`. |
| **cmd/harness-opencode-module** | A standalone binary that imports `opencode.New()` and speaks the `external` package's protocol server-side, serving as the reference implementation for anyone authoring a real external module. |

## Key Concepts

| Concept | Meaning |
|---------|---------|
| **ProvisionTier** | `builtin` \| `descriptor` \| `external` — how a `HarnessModule` was constructed; carried on `HarnessRef` for display and precedence. |
| **HarnessDescriptor vs wire type** | The domain type carries no YAML struct tags (so `domain` stays decoupled from the YAML library); an unexported `wireDescriptor` in `descriptor` owns all tags and is mapped across. |
| **ToolShape** | `list` (flat/diverted tool list) vs `permission` (full allow/deny map over the declared `Universe`) — determines which tool-field builder runs. |
| **FieldValue** | The shared scalar/list/mapping value representation used for every rendered frontmatter field; it is also the exact shape mirrored by the external protocol's wire types, so a harness's rendering logic is representable identically in-process or over JSON. |
| **InjectionClass** | `harness` (filled from the descriptor on every transform) / `project` (always empty on create, preserved on update) / `workflow` (assembled from selections) — `Injection(name)` only ever answers for the `harness` class. |
| **Diversion field** | A tool mapping whose `Field` is non-empty routes its harness tool names to a separate frontmatter key instead of the main tools list (e.g. splitting MCP tools from native tools). |

## Boundaries

- **Owns:** the `HarnessModule` abstraction itself; descriptor parsing/validation; the three shared descriptor-driven algorithms; tier discovery and precedence; the external protocol's client-side wire format and error taxonomy; contract verification shared across tiers.
- **Does not own:** applying a `FrontmatterPlan` to an actual document (that's `transform` + `docformat`); reading MOSAIC source files or resolving hook-variant reuse (that's `catalog`); writing any file to disk (that's `deploy`); deciding which harness the user picks or presenting the selection UI (that's `app`/frontends).

## Invariants & Conventions

- No package outside `harness/registry` may import `internal/harness/builtin/*` directly — built-in harnesses must be reached through the registry (module-level import-boundary rule, enforced by `tools/importcheck`).
- `domain.HarnessDescriptor` carries no struct tags (CD-1) — only the wire type in `descriptor` does.
- Tool-mapping and frontmatter-shaping logic exists exactly once (CD-2), in `harness/descriptor`; every tier delegates rather than reimplementing.
- `HarnessModule` implementations must be deterministic and free of hidden state — enforced by `contracttest`'s determinism checks on `Tools`, `Frontmatter`, and `Injection`.
- External harnesses require explicit opt-in (`allow_external_modules: true` in `tool-config.yaml`, or `--allow-external` on the CLI) and are gated at `Resolve()` time, never at `List()` time.
- A descriptor's `schema_version` must be one of `descriptor.SupportedSchemaVersions` (currently `{"1"}`); an unsupported (but present) version is a distinct, sentinel-matched error (`ErrUnsupportedSchemaVersion`) from a missing one.

## Known Complexity

**The external-module subprocess protocol is fully implemented and wired into production harness resolution.** `registry.Discover` — the only discovery path `cmd/mosaic-deploy/main.go` calls — constructs `TierExternal` harnesses (detected by the presence of a `harness-exec[.exe|.bat]` file next to `harness.yaml`) via `external.New`, which spawns the subprocess, performs the JSON-over-stdio version handshake, and forwards every subsequent `HarnessModule` method call to the subprocess. In a real `deploy`/`update` run, a harness folder that ships an executable spawns that executable and routes all harness logic through it; a descriptor-only folder (no executable) uses the descriptor-driven `runtimeModule` as before. The `external` package's complete JSON-over-stdio client (handshake, per-request timeout, full error taxonomy, transparent reconnect) is exercised end-to-end by CLI and TUI runs whenever an external harness is selected.
