# mosaic-log-analyzer

> Purpose: A standalone Go CLI/TUI tool that analyses MOSAIC orchestration logs (JSONL event files) to report per-run token usage and estimated USD cost, tolerating and surfacing data-quality problems instead of failing on them.

## Areas / Domains

| Area | Responsibility | Key Relationships |
|------|---------------|-------------------|
| **domain** | Base vocabulary layer: every type, value object, and port interface (`LogSource`, `EventReader`, `PricingStore`, `Clock`, `Interaction`) the rest of the module depends on. Imports nothing else in this module. | Depended on by every other package; depends on none of them. |
| **app** | The single use-case layer. Wires the three I/O adapters together via domain ports, drives the pure `analysis` core, and produces a priced `domain.Report`. Owns the `Service` type both frontends call into, source resolution, and the interactive pricing-gap resolution flow. | Called by `cli` and `tui`; calls `analysis`, and the adapters through domain-port interfaces (not directly). Never imports either frontend. |
| **analysis** (+ `analysis/cost`) | The pure decision core. Takes decoded events and a pricing table and produces a fully priced `domain.Report`, accumulating data-quality findings. No I/O, no randomness — deterministic and unit-testable with fixtures. `cost` is its pricing-arithmetic sub-package (tier selection, nanodollar cost computation). | Called by `app` only; imports only `domain`. |
| **logscan** | I/O adapter implementing `domain.LogSource`. Discovers log data by inspecting directory structure only (never reads file contents): default `OrchestrationLogs/` probing, path classification (logs root vs. single run vs. unusable), and full inventory enumeration. | Implements a `domain` port; used by `app` through that port. |
| **logread** | I/O adapter implementing `domain.EventReader`. Streams JSONL event files line by line, never buffering a whole file into memory. Malformed/truncated/unrecognised lines become `domain.Finding`s rather than aborting the read. | Implements a `domain` port; used by `app` through that port. |
| **pricing** | I/O adapter implementing `domain.PricingStore`. Owns the external YAML pricing config end-to-end: load, per-entry validation (invalid entries become findings, not load failures), and persistence of entries written by the interactive pricing-gap flow. Translates between USD-per-million-tokens (wire format) and internal nanodollars. | Implements a `domain` port; used by `app` through that port. |
| **cli** | Frontend adapter: the cobra command tree (`report`, `total`, `pricing path`), stable exit codes, JSON wire contracts, and the non-blocking `Interaction` implementation used when no terminal is attached (resolves every answer from flags immediately, or skips — never blocks). | Drives `app`; must not import `tui`. |
| **tui** (+ `tui/screens`) | Frontend adapter: the interactive, full-screen bubbletea terminal UI (built on the shared `mosaic-common/tui` scaffold) for browsing run reports, drilling into per-actor breakdowns, and entering missing model prices interactively. Default frontend when a terminal is attached. `screens` defines the `Screen` interface and the five screen IDs (source, runs, run-detail, pricing, no-data) that the top-level `tui.Model` routes input/render to uniformly. | Drives `app`; must not import `cli`. |
| **cmd/mosaic-log-analyzer** | Composition root. The only place concrete adapter implementations are constructed and wired together (via `app.Deps`), and the only place that decides CLI vs. TUI dispatch. | Imports everything; nothing imports it. |
| **tools/importcheck** | A standalone dev-time checker (not part of the shipped binary) that statically parses imports of every non-test source file and fails the build if any package violates the dependency-direction rules below. Run via `task check:imports`. | Enforces the architecture described in this document; does not participate in runtime flows. |

## System-Wide Patterns

- **Hexagonal / ports-and-adapters architecture.** `domain` defines port interfaces (`LogSource`, `EventReader`, `PricingStore`, `Clock`, `Interaction`); `logscan`, `logread`, `pricing` are the production adapters, `app` is the use-case layer that depends only on the ports, and `cli`/`tui` are sibling frontend adapters that both drive `app` and never each other. The dependency direction is machine-enforced by `tools/importcheck` (`task check:imports`), not just documented convention — treat any import-direction change as needing that check.
- **Never fail on bad input data — describe it instead.** Across `logscan`, `logread`, and `pricing`, malformed, missing, or unrecognised data produces a `domain.Finding` (with a `FindingKind`, severity, and location) and processing continues. Errors are reserved for genuine infrastructure failure (context cancellation, an unparseable pricing *document* as opposed to an invalid *entry*). This is the tool's central design principle — every layer honors it.
- **Money is always explicit about incompleteness.** `CategoryMoney.Complete` is false whenever any contributing model lacked a price; totals in that state are partial sums that frontends must render as such, never silently presented as final.
- **Interaction port is the only user-consultation channel**, shared from `mosaic-common/interaction`. The CLI's implementation never blocks (resolves from flags or skips); the TUI's implementation renders an overlay and supplies answers asynchronously. `app`-layer code that calls `Interaction` must work correctly against a non-blocking implementation.
- **Session enables re-pricing without re-reading logs.** `app.Session` caches the pre-pricing `domain.Aggregate` and the pricing table used; when new prices are entered interactively, `Session.Reprice` recomputes the `Report` purely from cached state — logs are never re-read for a price update.

## Key Invariants

- `domain` imports nothing else in this module — this is the base vocabulary layer and is machine-verified.
- `analysis` and `analysis/cost` import only `domain` from this module and no I/O packages (`os`, `net`, `syscall`, `math/rand`, `crypto/rand`) — the analysis core is pure and safe for concurrent, lock-free use.
- `app` must not import `internal/tui` or `internal/cli` — both frontends depend on `app`, never the reverse.
- `cli` and `tui` (including `tui/screens`) must not import each other — they are sibling frontends selected once, in the composition root, based on TTY detection and the `--tui` flag.
- A missing or empty log source is **never** a hard failure (`OutcomeSourceNotFound` / `OutcomeNoData`); an error return from `Service.Analyze` is reserved for genuine infrastructure failure.
- Every `domain.Report` carries its `QualitySummary`, so a frontend can never present partial/lossy data as complete without the information being available to say so.
- Entered pricing entries are persisted automatically (no separate confirmation step) as soon as validation passes; declining/skipping the pricing-gap flow is a normal outcome, not an error.

## Known Complexity

No areas currently warrant deeper-tier documentation beyond this overview — see KBProgress.md for the reasoning behind that call.
