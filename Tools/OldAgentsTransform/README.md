# OldAgentsTransform — legacy agent boundary retrofit

One-shot Python tooling that migrates the **previous generation** of MOSAIC agent
instruction files (plain Markdown with `[INJECTION: name]` comment markers, or no
markers at all) to the **current generation** boundary-tag vocabulary:

```
[[SECTION:Identity]] ... [[/SECTION:Identity]]
[[INJECTION:CodebaseContext]] ... [[/INJECTION:CodebaseContext]]
[[DEPLOYED:CommunicationProtocol]] ... [[/DEPLOYED:CommunicationProtocol]]
```

Emitting this vocabulary is **not** the same as producing a conformant
current-generation file. A clean [`boundary_validator.py`](#validate) run
confirms the tags are well-formed, correctly ordered and paired — it says
nothing about whether the content inside them is right. Even after boundary
tags, conduct regions and frontmatter fields are all in place, a transformed
file can still carry gaps that this tooling detects and reports but does not
fix on its own: a retained JSON response envelope in `Output Format`, zero
`[[INJECTION:...]]` regions, or harness-specific prose sitting in an
agent-authored section. See [Non-conformance report](#non-conformance-report)
below for what is detected and how it is reported.

**Status: completed migration. Archived, not part of any live pipeline.**

These scripts are *not* invoked by `mosaic-deploy` (`Tools/Deployment/`). The
deployment tool assumes source files already carry correct boundary tags — which
is exactly what this tooling produced. Keep this folder for reference, for
re-running against agent files that were authored outside the repo in the old
format, and as the Python-side mirror of the boundary vocabulary.

---

## Contents

| File | Purpose |
|------|---------|
| `boundary_constants.py` | Canonical boundary vocabulary: section names, document order, the closed set of `DEPLOYED` names, parent maps, known frontmatter keys, tag regex. Shared by the other two modules. |
| `boundary_transformer.py` | Adds boundary tags and conduct regions to a single agent `.md` file, skips utility/non-agent files, completes missing frontmatter keys, and bumps the version (minor bump: `M.m.p` -> `M.(m+1).0`) and `transform_version` for harness files. Body text outside the regions and keys it manages is preserved verbatim. |
| `boundary_validator.py` | Validates that a file (or a whole tree) has well-formed, correctly ordered and nested boundaries, no content outside a boundary, and no unknown frontmatter keys for its document kind. Error codes `E000`–`E011`. |
| `batch_transform.py` | Runner that transforms every harness agent file for a platform batch, pairing each with its generic counterpart or routing uncounterparted files through the degraded-quality transform, and prints a non-conformance report after each batch. |
| `tests/` | Pytest suite + fixtures for all modules. |

As of this run the tool also includes seven small supporting modules —
`document_kind.py`, `fence.py`, `file_classification.py`, `frontmatter_build.py`,
`non_conformance.py`, `region_insertion.py` and `deployed_blocks.py` — that the
files above import. Each has a single, narrow contract (fence-aware line
scanning, file-skip classification, frontmatter derivation, non-conformance
records, and so on) and is not restated here; read the module docstring for its
exact contract.

---

## Usage

Run with `py` (not `python`) on this machine.

### Validate

```bash
py Tools/OldAgentsTransform/boundary_validator.py <path-to-file.md>
py Tools/OldAgentsTransform/boundary_validator.py <path-to-dir> --batch
```

Exit `0` = all files valid, `1` = at least one error. Output format:

```
<filepath>:<line>: <error-code> <message>
```

Findings with severity `advice` (e.g. an injection in an unusual parent section)
are printed but never cause a non-zero exit.

### Transform a single file

```bash
# Generic source file (Catalog/...)
py Tools/OldAgentsTransform/boundary_transformer.py <input.md>

# Harness/derived file with its generic counterpart (full-quality transform)
py Tools/OldAgentsTransform/boundary_transformer.py <input.md> \
    --generic-ref "Catalog/Subagents/Execution/implementation-tdd.md"

# Harness-only agent — no generic counterpart (degraded-quality transform, automatic)
py Tools/OldAgentsTransform/boundary_transformer.py <input.md>

# Non-destructive: write elsewhere instead of overwriting in place
py Tools/OldAgentsTransform/boundary_transformer.py <input.md> --output <out.md>

# Force-overwrite an existing --output without confirmation (scripted / batch use)
py Tools/OldAgentsTransform/boundary_transformer.py <input.md> --output <out.md> --force
```

Without `--output` the file is **overwritten in place**. Commit or back up first.

**Overwrite guard:** When `--output` names an existing file, the CLI warns and asks for
confirmation before overwriting. If the session is non-interactive (stdin is not a tty, e.g.
from CI, a script, or captured by a test harness), the default is to **decline** rather than
block. Pass `--force` to overwrite unconditionally without a prompt — intended for scripted
and unattended use.

| Scenario | Behaviour | Exit code |
|----------|-----------|-----------|
| `--output` not given | File overwritten in place, no prompt | 0 |
| `--output` given, file does not exist | File written, no prompt | 0 |
| `--output` given, file exists, `--force` | File overwritten, no prompt | 0 |
| `--output` given, file exists, interactive, user answers `y`/`yes` | File overwritten | 0 |
| `--output` given, file exists, non-interactive (no `--force`) | Declined, file unchanged | **2** |
| `--output` given, file exists, interactive, user declines or presses Enter | Declined, file unchanged | **2** |

Exit code 2 (declined overwrite) is distinct from exit code 1 (transform error) so a script can
tell a refused overwrite apart from a failed transform.

A file is treated as a harness file when its frontmatter contains
`transform_version`. For those:

- **With `--generic-ref`:** the explicitly supplied path is used as the generic
  counterpart. Takes precedence over auto-lookup.
- **Without `--generic-ref`, stem matches:** the single-file CLI automatically
  locates the generic counterpart by filename stem (e.g. `contracts-designer.md`
  → `Catalog/Subagents/Planning/contracts-designer.md`) and produces a
  full-quality result. No flag is required — passing `--generic-ref` is
  optional.
- **Without `--generic-ref`, no stem match:** the tool automatically runs a
  *degraded-quality* transform. No error is returned; a warning is printed to
  stderr identifying the file as degraded. See
  [Harness-only agents](#harness-only-agents-no-generic-counterpart) below for
  what quality is sacrificed.
- **Orchestrator exception:** a harness file named `orchestrator.md` or
  `orchestrator.agent.md` (matched case-insensitively by filename) still
  hard-fails when no generic counterpart is found, and nothing is written.

**Utility and non-agent files are skipped, regardless of harness status.** Before
frontmatter is even read, the file's base name is checked against a fixed
allowlist. A utility agent (e.g. `transformation.md`) or a file that is not a
MOSAIC agent at all (e.g. `SCL Developer.chatmode.md`) is skipped: no output is
written, a message is printed to stderr, and the result carries `skipped=True`.
This check runs on every invocation shape — single file or batch — not only on
harness files.

### Batch transform harness trees

```bash
py Tools/OldAgentsTransform/batch_transform.py --batch B   # Claude Code
py Tools/OldAgentsTransform/batch_transform.py --batch C   # GHCP CLI
py Tools/OldAgentsTransform/batch_transform.py --batch D   # OpenCode
py Tools/OldAgentsTransform/batch_transform.py --batch E   # VS code GHCP
py Tools/OldAgentsTransform/batch_transform.py --all
```

Each batch covers that platform's `CodebaseAgnostic/` and `ExampleProject/`
directories. Generic counterparts are matched **by file stem**. The batch runner
handles each file as follows:

| Condition | What happens | Output prefix |
|-----------|-------------|---------------|
| Utility agent or non-MOSAIC-agent file (any counterpart status) | Skipped before any transform is attempted — no output written | `[SKIP]` |
| Generic counterpart found | Full-quality transform | `[OK]` |
| No counterpart, not an orchestrator | Degraded-quality transform (automatic) | `[DEGRADED]` |
| No counterpart, already transformed | Idempotency skip — file left untouched | `[SKIP]` |
| No counterpart, orchestrator file | Warn and count as error — no transform | `[WARN]` |
| Any transform failure | Error reported | `[ERR]` |

The utility/non-agent skip check runs first, inside `transform_file()` itself,
before the counterpart lookup below it is even consulted — see
[Transform a single file](#transform-a-single-file) above.

The batch summary line reports four counts:

```
Batch B: 12 files processed (2 degraded, 1 skipped), 0 errors.
```

`skipped` counts both idempotency skips and utility/non-agent skips; utility and
non-agent files increment `skipped`, never `processed`, and are never errors.

Harness-only orchestrators (files named `orchestrator.md` or
`orchestrator.agent.md`) increment the error count and receive no output file.
All other harness-only files are processed through the degraded-quality path;
`[DEGRADED]` files are never counted as errors.

Batch paths are hardcoded in `BATCH_DIRS` in `batch_transform.py` and reflect the
platform folder layout at the time of the migration. Verify them before re-running.

### Non-conformance report

Producing well-formed boundary tags is not the same as producing a conformant
file. After the batch summary line, the runner prints a non-conformance report
listing per-file findings for gaps the tool can detect but not fix:

| Code | Meaning |
|------|---------|
| `NC-7A` | `Output Format` still retains a JSON response envelope where the current convention is a table |
| `NC-7B` | The output carries zero `[[INJECTION:...]]` regions — a project cannot customise the agent |
| `NC-7C` | A heading naming a specific harness (e.g. "Claude Code", "GHCP CLI") sits inside an agent-authored section, rather than under `HarnessConstraints` |
| `NC-D1F` | A superseded bullet was found in drifted wording and left in place |
| `NC-TIER` | `recommended_tier` or `tier_rationale` was absent and was written as a placeholder that needs manual completion. Only raised on the **generic path** — these fields are not written on the harness path, so no finding is raised there even when they are absent. |

Findings are recorded on `TransformResult.non_conformances` for a single
transform, accumulated across a batch, and rendered by
`non_conformance.render_report()`. A batch with zero findings still prints the
report, ending in a `0 non-conformances across 0 files.` line. An `[OK]` or
`[DEGRADED]` result can carry findings just as easily as a failing one — this
report is about the shape of the finished file, not about whether the
transform itself succeeded.

### Tests

```bash
py -m pytest Tools/OldAgentsTransform/tests -v
```

The suite resolves module imports relative to its own location, so it works from
any working directory.

---

## Harness-only agents (no generic counterpart)

A *harness-only agent* is a harness file whose filename stem has no matching
file under `Catalog/`. The batch runner and the single-file transformer
both support them through a degraded-quality transform that runs automatically
— no flag is required to enable it.

### What triggers the degraded path

When `transform_file()` receives a file whose frontmatter contains
`transform_version` and no `--generic-ref` is provided, it:

1. **Orchestrator check.** If the filename is `orchestrator.md` or
   `orchestrator.agent.md` (case-insensitive), the existing hard failure is
   returned unchanged. Nothing is written, and an error is reported. Both the
   single-file transformer and the batch runner apply this check identically.

2. **Idempotency guard.** If the file already carries a structurally valid set
   of canonical `[[SECTION:...]]` boundary tags (all tags paired, all names
   recognised), it is left byte-identical and a warning is printed to stderr:

   ```
   <path> already carries canonical boundary tags; skipping
   ```

   The result carries `skipped=True` and `success=True`. No output file is
   written. This prevents re-tagging a file that was already processed.

3. **Degraded transform.** Otherwise, the generic-body transform logic runs
   with non-strict identity classification — the same relaxation normal harness
   files already receive — and a warning is printed to stderr:

   ```
   No generic reference found for <path>; running degraded-quality transform
   ```

   The result carries `degraded=True`, `success=True`, and the warning text
   in `result.warnings`.

### What "degraded" costs

A full-quality harness transform diffs the harness body against the generic
source to locate project-specific injection content and place it under the
correct `[[INJECTION:...]]` tags. The degraded path cannot do this because
there is no reference to diff against. As a result:

- **Injection-merging accuracy is lost.** The tool cannot distinguish
  project-specific injection fill from boilerplate, so injection regions may
  be placed incorrectly or left empty.
- **Section tags and `[[DEPLOYED:...]]` placeholder pairs are still produced.**
  The structural skeleton is correct; only the injection content placement
  is unreliable.

After a degraded transform, review the output and fill injection regions
manually before deploying the file with `mosaic-deploy`.

### Default `version` when `version` is absent

A missing `version` field in frontmatter is no longer a hard failure on **any**
path — generic, harness-with-`--generic-ref`, or degraded. `resolve_version()`
substitutes `"1.0.0"` as the starting value whenever `version` is absent or
empty, and the normal bump rule applies to it from there. The substituted
version appears in `version_before` in the result; the bumped value is written
to the output file's `version` key.

This is a change from the tool's original behaviour, where the fallback was
reachable only on the degraded path — i.e. only when `transform_version` was
present and no `--generic-ref` was given — and a file carrying neither
`version` nor `transform_version` hard-failed on every other path. That gap is
closed: a file missing `version` is transformed, not rejected, regardless of
which path it takes.

The bump itself is a **minor** bump, not major: `M.m.p` becomes `M.(m+1).0`
(e.g. `2.5.0` -> `2.6.0`; a bare `3` becomes `3.1.0`).

### Orchestrator exclusion

Both the single-file transformer and the batch runner classify a file as an
orchestrator exclusively by its filename:

- `orchestrator.md`
- `orchestrator.agent.md`

Both comparisons are case-insensitive. A file whose frontmatter contains
`role: orchestrator` but whose name does not match is **not** treated as an
orchestrator for this purpose. Without a generic reference, orchestrator files
always hard-fail; they are never routed through the degraded path.

---

## Recommended order for a re-run

1. Commit or stash — the transformer overwrites in place.
2. `boundary_validator.py --batch` on the target tree to see the starting state.
3. `boundary_transformer.py` (single file) or `batch_transform.py` (whole tree).
4. `boundary_validator.py --batch` again — a clean run is the acceptance gate.
5. Diff the result: expect boundary tag lines, inserted `[[DEPLOYED:...]]`
   conduct regions (with the prose bullets they supersede removed), derived or
   reconciled frontmatter keys (`name`, `role`, `id`), and a minor version
   bump. For **generic source files** only, also expect `required_skills`,
   `recommended_tier`, and `tier_rationale` (with `TODO` placeholders) when
   those fields were absent. Harness files receive `name` and `role` but never
   `required_skills`, `recommended_tier`, or `tier_rationale`. These are
   deliberate, correct changes, not regressions — a diff that shows only these
   categories is the expected outcome, not one where nothing but tags and
   version changed.

---

## Status and known limitations (2026-08-06)

Verified against 19 real old-generation OpenCode agent files.

| Check | Result |
|-------|--------|
| Test suite | 1153 passed, 1 skipped |
| Transform crashes | 0 |
| Validation clean | **18 of 19** |
| Content loss | none |

Content preservation was verified by stripping boundary tags from every output
and diffing against the source, against the version of the tool current as of
this table's date — before the conduct-region and frontmatter-completeness work
described elsewhere in this README existed. At that point the only removals
were the old `[INJECTION: name]` markers and the `## Communication Protocol`
block, and the only additions were blank lines and a `---` separator. That is
no longer the whole story: the tool now also inserts `[[DEPLOYED:...]]` conduct
regions and deletes the prose bullets they supersede, derives missing
frontmatter keys (`name`, `role`), and reconciles `id` against the generic
reference. On the **generic path only**, it also appends `required_skills` and
tier-placeholder values for `recommended_tier` and `tier_rationale` when those
fields are absent. On the **harness path** these three fields are never appended
— no tier-placeholder non-conformance is raised there either. A body diff
against current output will show these deliberate additions and deletions in
addition to boundary tags.

### Communication Protocol regions are rewritten, not preserved

An untagged `## Communication Protocol` section is **replaced** by an empty
top-level `[[DEPLOYED:CommunicationProtocol]]` pair; its prose is discarded. This
is correct: that content is tool-managed and regenerated by `mosaic-deploy` on
every run. A `ProtocolExtension` marker inside the region is re-emitted as an
empty top-level injection sibling, and an `identity_extension` marker found there
is relocated into the Identity section rather than lost.

Two subtleties this depends on, both worth knowing if you touch the code:

- Sections are identified by *heading range*, so a section's range extends past
  its own closing tag and can swallow a following top-level deployed region. The
  harness path strips `[[DEPLOYED:CommunicationProtocol]]` out of the generic
  section slice before merging (`_strip_deployed_pair`), otherwise the boundary
  name is emitted twice (`E006`).
- The generic and harness paths are separate implementations selected by whether
  `transform_version` is in frontmatter. A fix applied to the body-merge logic
  of one (`_transform_generic_body` / `_transform_harness_body`) does **not**
  automatically reach the other — this asymmetry has already caused one
  regression. Conduct-region insertion (`[[DEPLOYED:ClosingProcedure]]`,
  `AuthorityHierarchy`, and the rest) is a deliberate exception to it: it is a
  single shared function (`region_insertion.apply_conduct_regions`), driven by
  one declarative table, called identically by both paths, specifically so a
  placement fix cannot land on only one of them. Frontmatter completion and
  non-conformance detection are likewise shared between the two paths. The
  asymmetry that remains is scoped to the two body-merge functions themselves.

### Open: boundary tags can land inside fenced code blocks

One file (`orchestrator.md`) still fails:

```
E002 Unmatched closing tag [[/INJECTION:ErrorHandlingExtension]] (no open tag)
```

`_merge_section` aligns harness content against the generic's injection markers
without fence awareness. In this file the harness carries a fenced ASCII diagram
one line longer than the generic's, and that drift *inside* the fence is misread
as injection fill: the opening tag is placed mid-fence while the closing tag
lands outside it. The validator ignores tags inside fences, so it reports the
survivor as orphaned.

This is an alignment defect, not a vocabulary one, and fixing it properly means
making `_merge_section` fence-aware. Until then, hand-fix the affected region:
move the opening tag to just before the fence.

### A clean validator run is not sufficient evidence

A file with no `## Communication Protocol` heading emits only 2-4 boundary tags
total — sometimes a single `[[SECTION:Identity]]` wrapping the whole file. The
validator accepts this because it checks the relative order of the canonical
slots that are *present*, not that all seven exist. Always diff the output;
never trust exit code 0 alone.

This no longer describes the specific utility agents this section originally
named as examples. `transformation.md`, `anthropic-subagent-creator.md`, and
the rest of `file_classification.UTILITY_AGENT_FILENAMES` are now [skipped
outright](#transform-a-single-file) before any transform is attempted — they
produce no output at all, sparse or otherwise. The sparse-tagging risk
described here now applies to any other subagent file that genuinely lacks a
`## Communication Protocol` heading and is not on that skip list.

### Harness-only files receive a degraded-quality transform

The tool matches a file to its generic source **by filename stem**. Agents with
no counterpart under `Catalog/Subagents/` were previously skipped; they now go
through the degraded-quality transform described in
[Harness-only agents](#harness-only-agents-no-generic-counterpart) above. In
the original test corpus that was 7 of 27 files, including `platform-bug-hunter`,
`test-results-analyzer` and `orchestration-test-author`.

(`Web Research` is **not** one of these, despite an earlier version of this
section naming it: a file with neither `version` nor `transform_version` in
its frontmatter takes the generic path, not the harness dispatch, so it was
never routed through the degraded path at all — see [Default `version` when
`version` is absent](#default-version-when-version-is-absent) above for how a
file in that state is handled now.)

The single exception is the orchestrator: a file named `orchestrator.md` or
`orchestrator.agent.md` with no generic counterpart still hard-fails, and the
batch runner counts it as an error rather than routing it through the degraded
path.

---

## Vocabulary drift warning

`boundary_constants.py` is one of **four** copies of the boundary vocabulary:

- `Development/Designs/AgentTemplateArchitecture.md` — the specification (source of truth)
- `Catalog/SourceFilesFormat.md` — the authoring reference
- `Tools/Common/docformat/vocabulary.go` — the Go copy used by `mosaic-deploy`
- `Tools/OldAgentsTransform/boundary_constants.py` — this Python copy

`Tools/Common/docformat/vocabulary_boundary_sync_test.go` asserts the Go copy
matches this Python one. If you change the vocabulary in the design document,
all copies must be updated together, or the tools will disagree about what is
valid. Because this folder is archived, it is the copy most likely to drift —
check it before trusting a validation run.
