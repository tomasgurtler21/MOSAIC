# MosaicTest Fixtures

Fixture data for the `mosaic-run` harness conformance suite. Nothing here is code, and nothing here is read by the runner from this location — every file has to reach the run folder, `Orchestration-{run_id}/`, before the run uses it. Seeding is what puts it there.

## Seed paths — copy-paste

Each workflow has exactly one seed root. Seed that directory and nothing else.

| Workflow | Mode | Seed path |
|----------|------|-----------|
| `smoke-single` | auto, auto-review | `C:\AI\MOSAIC\MOSAIC\Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\smoke-single` |
| `payload-stress` | any | `C:\AI\MOSAIC\MOSAIC\Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\payload-stress` |
| `staged-preplaced-plan` | any | `C:\AI\MOSAIC\MOSAIC\Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\staged-preplaced-plan` |
| `orchestrated-linear` | **orchestrated only** | `C:\AI\MOSAIC\MOSAIC\Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\orchestrated-linear` |
| `orchestrated-backjump` | **orchestrated only** | `C:\AI\MOSAIC\MOSAIC\Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\orchestrated-backjump` |
| `findings-loop` | **auto** and **auto-review** | `C:\AI\MOSAIC\MOSAIC\Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\findings-loop` |
| `deviation-blocked` | **auto** | `C:\AI\MOSAIC\MOSAIC\Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\deviation-blocked` |
| `deviation-ambiguous` | **auto-review** | `C:\AI\MOSAIC\MOSAIC\Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\deviation-ambiguous` |
| `deviation-stop` | **auto** | `C:\AI\MOSAIC\MOSAIC\Tools\Runner\TestCatalog\Workflows\MosaicTest\Fixtures\deviation-stop` |

In the TUI, paste the path at the seed-input screen. On the command line it is `--input <path>`.

Seeding a directory copies every file beneath it, with destinations **relative to the named directory**. Seeding `Fixtures\payload-stress` therefore produces:

```
Orchestration-{run_id}/
├── Orchestration.md          ← the runner's own, never seeded
├── Requirements.md           ← seeded
├── Plan.md                   ← seeded
└── MosaicTestScript/
    ├── payload-unicode.md    ← seeded
    ├── payload-fences.md
    └── payload-json.md
```

## Why one seed root per workflow

Three constraints force this layout, and each one is a way a hand-assembled seed set goes wrong.

**Destinations are relative to the named directory.** Seeding `MosaicTestScript/` copies its *contents* to the run folder root, dropping the `MosaicTestScript/` prefix. Workflow rows declare inputs as `MosaicTestScript/{script}.md`, so the path no longer resolves and `mosaictest-scripted` returns `BLOCKED`/`E101`. The prefix survives only when the directory *containing* `MosaicTestScript/` is the one seeded — which is what each seed root is.

**Exactly one `Requirement*` file is mandatory.** Seeding refuses any seed set with zero or more than one candidate, matched case-insensitively against file sources and the **top-level** files of directory sources. The refusal happens during planning, before the run folder exists, so a run that trips it produces no run folder and no orchestration log. Each seed root carries one `Requirements.md` for this reason alone; no subagent reads it.

**`Plan.md` must land at the run folder root.** The two staged workflows have no pre-EXECUTION row that could write a stage table, so it is pre-placed. A top-level `Plan.md` inside the seed root lands at the root; one nested a directory deeper does not.

**The TUI accepts a single seed path.** There is no way to combine two sources there, so any layout needing more than one is command-line-only. A self-contained seed root is what keeps the suite runnable from the TUI.

## Layout

| Path | Contents |
|------|----------|
| `{workflow-id}/Requirements.md` | Seed-rule placeholder. Never read. |
| `{workflow-id}/Plan.md` | Pre-placed stage table, for workflows that need one. |
| `{workflow-id}/MosaicTestRouting.md` | Routing fixture for the stub orchestrator, for workflows run in a mode that consults it. Fixed filename — a consultation carries no artifact paths, so there is no per-invocation channel to bind a fixture through. |
| `{workflow-id}/MosaicTestScript/` | Behaviour scripts for that workflow. Named for behaviour, never for row number. |

Scripts live under the workflow that uses them and are not shared. No two workflows currently need identical behaviour, so sharing would buy nothing while forcing a layout that trips the prefix rule above. Two workflows that genuinely need one script should hold identical copies — a fixture is a specimen, and drift between two copies is a signal worth seeing rather than a duplication worth removing.

This README stays at the `Fixtures/` root, outside every seed root, so it is never copied into a run folder.

## Why a copy step exists at all

The stubs' only instruction channel is their artifact paths. A routed dispatch carries no `task_description`, so a workflow binds a script to a row through the row's Input column, and artifact paths resolve inside `Orchestration-{run_id}/`. A script fixture is therefore an ordinary run artifact that happens to be authored by hand instead of produced by an agent.

## The script format

Specified in full in `MosaicTestCatalog/Subagents/MosaicTest/mosaictest-scripted.md`, under **The MosaicTest script format**. That file is the specification; these files are instances of it. Two properties are easy to break when authoring a new script:

- **Fences are tildes (`~~~`), never backticks.** Payload fixtures carry backticks and whole fenced blocks, and tilde fences mean no escaping is needed anywhere. Escaping is precisely what must not happen to a payload under test.
- **`status_message` is the primary readout.** Evaluation is a human watching the runner TUI, not a golden diff, so every message names its row, phase, stage where meaningful, and the status being returned. A message that is not self-describing makes the run unreadable.

## Diagnosing a failed seed

| Symptom | Cause |
|---------|-------|
| No run folder, no orchestration log, refusal naming `Requirement*` | Seed root missing its `Requirements.md`, or two seed sources each contributed one. |
| `BLOCKED` / `E101` naming a `MosaicTestScript/...` path | A directory *inside* a seed root was seeded instead of the seed root, flattening the prefix. |
| Stage table not found | `Plan.md` landed below the run folder root — the seeded directory was one level too high. |

`mosaictest-scripted` refuses rather than guessing a status code, so a setup mistake is legible on the first attempt instead of producing a green run that measured nothing. Its `error_reason` names the path it looked for and lists what it found instead.
