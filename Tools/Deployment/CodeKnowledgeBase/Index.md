# mosaic-deploy

> Purpose: Transforms generic MOSAIC agent/skill/workflow/hook sources into harness-specific files and installs (or updates) them into a target AI harness workspace, without ever modifying MOSAIC sources.

## Overview

`mosaic-deploy` is a Go CLI/TUI tool rooted at `Tools/Deployment`. It has two
top-level operations exposed identically through both frontends:

- **Deploy** — build a brand-new harness workspace: select a harness, a
  workspace path, workflows, utility agents, and tier-to-model mappings, then
  write every resolved file in one pass after a plan-review step.
- **Update** — bring an existing (manifest-tracked) workspace up to date with
  the latest generic sources, detecting local modifications and applying a
  chosen conflict-resolution strategy (`skip` / `overwrite` / `backup`).

Both flows are pure data pipelines up until the final write step: read source
→ compute a plan → transform bytes → execute writes → record a manifest → emit
a TODO checklist for anything that needed a human decision.

## Areas / Domains

| Area | Responsibility | Key Relationships |
|------|---------------|-------------------|
| **Domain & Contracts** | Shared vocabulary (all cross-package types) and the two port interfaces (`HarnessModule`, `Interaction`) every other package programs against. Imports nothing else in the module. | Everything depends on it; it depends on nothing internal. |
| **Source Reading** (catalog, docformat) | `catalog` reads the MOSAIC repository tree once and exposes agents/skills/hooks/workflows as typed values — the only package that opens MOSAIC source files for reading. `docformat` is the single authority on the MOSAIC document format (YAML frontmatter + markdown body with `[[SECTION:...]]` / `[[INJECTION:...]]` boundary tags), parsing and serialising with byte-exact fidelity. | catalog is consumed by plan; docformat is consumed by transform and catalog. |
| **Harness System** (descriptor, registry, builtin, external, contracttest) | Abstracts over four ways of providing a harness implementation (compiled-in builtin, descriptor-only YAML, external subprocess module, plus the registry that discovers/aggregates them with deterministic precedence external > descriptor-only > builtin). `contracttest` supplies one shared assertion suite every provision tier is verified against, so no tier writes its own per-method tests. **Caveat:** the external-tier subprocess client (`internal/harness/external`) is fully implemented and tested but is not currently invoked by the production discovery path — `registry.Discover` (the only path `cmd/mosaic-deploy` calls) builds `TierExternal` entries with the same descriptor-driven module used by the descriptor-only tier, never with `external.New`; a discovered external harness's executable is never actually spawned in a real deploy/update run. See [Harness System](./HarnessSystem/Index.md) Tier 2 doc, "Known Complexity". | registry is consumed by main/app; descriptor's MapTools/ApplyFrontmatterSpec algorithms are the single implementation every tier delegates to. |
| **Transformation & Planning** (transform, plan) | `transform` is a pure function (bytes in, bytes out — no I/O, no clock, no randomness) that applies one harness's transformation to one generic source file via docformat, producing an audit Report. `plan` computes a full `domain.Plan` of every action a run would take (reading catalog + manifest snapshot, comparing staleness) without performing any writes. | plan calls into harness modules (via registry) and reads catalog + manifest; transform is invoked during planning/execution to determine per-file output. |
| **Execution & Persistence** (deploy, manifest, config, logging, todo) | `deploy` executes a `domain.Plan`: writes files, manages backups for locally-modified files, performs hook registration, writes the manifest, and records every action as a `domain.ActionRecord` even on partial failure. `manifest` reads/writes `<workspace>/.mosaic/manifest.yaml`, distinguishing absent/empty/corrupt/future-schema states. `config` loads/saves `tool-config.yaml` (project-scoped) and `user-config.yaml` (user-scoped, tier-to-model mappings). `logging` writes `latest.log`/`history.log`, degrading rather than failing on write errors. `todo` collects manual-attention items into `MOSAIC-DEPLOYMENT-TODO.md` and the on-screen summary from one shared source. | deploy is the sole writer of workspace files and the manifest; all five packages are wired together in cmd/mosaic-deploy and consumed by app. |
| **Use-Case Layer** (app) | Owns flow sequencing for DeployNew and Update, decides when to consult the user via the Interaction port, latches `SkippedAll` per question, persists tier-to-model mappings, and assembles the `domain.RunSummary` returned to both frontends. Imports every core package but never tui/cli — frontends depend on app, not the reverse. | Central orchestrator called by both cli and tui; calls plan, deploy, catalog, registry, config, manifest, logging, todo. |
| **Frontends** (cli, tui, cmd/mosaic-deploy) | `cli` is the non-interactive frontend: parses args, builds a pre-answered `Interaction` that never blocks (unresolved questions become TODO items), supports `--output json`. `tui` is the interactive terminal frontend built on Bubble Tea; owns the terminal, presents question-shaped prompts via Interaction, renders plan-review and summary screens, and contains no flow logic of its own. `cmd/mosaic-deploy` is the single entry point: constructs every dependency once, decides CLI vs. TUI (subcommand present, or isatty on both stdin/stdout), and dispatches. | Both frontends depend only on app.Service; main.go is the only place infrastructure is constructed. |
| **Tooling** (tools/importcheck, cmd/harness-opencode-module) | `importcheck` is a build-time guard enforcing the module's import-direction rules (domain imports nothing internal; transform bans I/O packages; app must not import tui/cli; builtin harnesses must be reached only via the registry). `cmd/harness-opencode-module` is a reference external harness module — a standalone binary implementing the JSON-over-stdio protocol from `internal/harness/external`, built from the same source as the built-in OpenCode harness. | importcheck runs via `task check:imports`/`task build-checked`; the opencode module binary is an example external-tier implementation. |

## System-Wide Patterns

- **Ports and adapters:** `domain.HarnessModule` and `domain.Interaction` are the only two port interfaces in the system. Every harness (builtin/descriptor-only/external) implements the former; every frontend (cli/tui) implements the latter. New harnesses or frontends plug in without changing app, plan, or deploy.
- **Plan before write:** No file is written until a `domain.Plan` has been computed and (in interactive/TUI use) shown for review. `--dry-run` on the CLI stops right after plan computation.
- **Pure computation vs. effects:** `transform` and `plan` are read-only/pure; `deploy` is the only package that performs writes. This separation is enforced at build time by the import-boundary guard (`tools/importcheck`), not just by convention.
- **Never fail silently, never abort on degraded auxiliary systems:** logging failures accumulate in `Degraded()` rather than aborting a deployment; catalog reports index/disk mismatches as structured issues rather than aborting on the first one; deploy records `ActionRecord`s even through partial failure so the manifest reflects real on-disk state.
- **One implementation per cross-cutting algorithm:** tool-mapping and frontmatter-field algorithms live once in `harness/descriptor` and are delegated to by every provision tier (builtin, descriptor-only, external), rather than being reimplemented per harness.
- **MOSAIC sources are read-only inputs:** `catalog` is the only package that opens MOSAIC repository files for reading; the tool never writes back into the MOSAIC source tree, only into the target workspace.

## Key Invariants

- `internal/domain` may not import any other package in this module (acyclic dependency root).
- `internal/transform` may not import `os`, `io/fs`, `net`, `time`, `math/rand`, `crypto/rand`, or any I/O package — it must remain a pure function.
- `internal/app` may not import `internal/tui` or `internal/cli` — frontends depend on the use-case layer, never the reverse.
- No package may import `internal/harness/builtin/*` directly; built-in harnesses must be reached through the registry.
- External harness modules require explicit opt-in (`allow_external_modules: true` or `--allow-external`) and are gated at resolution time, not at listing time.
- The deployment executor resolves the deployment root location exactly once, before any content write.
- Manifest content hashes use the canonical `sha256:<lowercase-hex>` form with no byte normalisation — a line-ending-only change is a detectable modification.

## Known Complexity

Areas flagged for deeper-tier documentation (see KBProgress.md):
- **[Harness System](./HarnessSystem/Index.md)** — three provision tiers with precedence rules, a shared contract-test suite, and a JSON-over-stdio subprocess protocol for external modules.
- **[Deployment Pipeline](./DeploymentPipeline/Index.md)** — the catalog → plan → transform → deploy → manifest sequence, including staleness comparison, conflict resolution, and the byte-exact docformat parsing model.
- **[Use-Case & Frontend Layer](./Frontends/Index.md)** — app's flow sequencing (DeployNew/Update), the Interaction port's four question shapes, and how cli/tui each implement it differently.
