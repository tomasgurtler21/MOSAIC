# AgentTest — Test Results Storage & Summary Design

**Status:** Draft
**Scope:** How test results are stored, organized, and summarized across models and harnesses.

---

## 1. Problem Statement

AgentTest produces JSON reports for each suite run. These reports contain rich metadata: orchestrator version, harness identity, model identity, per-test verdicts, pass rates, costs, and durations. Today the reports are dumped to `dist/` with no organization and no cross-run analysis.

The system needs to:

1. **Store** finalized reports in a structured tree for long-term retention.
2. **Summarize** results across two independent axes — model and harness — producing human-readable comparison tables.
3. **Keep debug/development runs out** of the results tree until the user explicitly promotes them.

### 1.1 The Two-Axis Problem

There are two independent environmental variables that affect orchestrator routing accuracy:

- **Model.** The same orchestrator instructions, interpreted by different LLMs, yield different routing correctness rates.
- **Harness.** The same model, run through different agent harnesses (Claude Code, OpenCode, etc.), may score differently because harness system prompts and tool shapes influence the LLM's behaviour.

Both axes are analytically equal — neither is inherently "above" the other. The storage and summary design must serve comparisons along both axes without privileging one.

---

## 2. Storage

### 2.1 Location

`TestResults/` at the MOSAIC repository root. Results are MOSAIC-level artifacts, not tool-internal state.

### 2.2 Tree Structure

```
TestResults/
  {orchestrator-version}/
    {suite}--{harness}--{model-short}--{timestamp}.json
    {suite}--{harness}--{model-short}--{timestamp}.json
    ...
    summary.md                  # generated: per-version summary
  summary.md                    # generated: cross-version summary
```

**One directory level: orchestrator version.** This is the primary grouping because the fundamental question AgentTest answers is "does this orchestrator version route correctly?" Everything else — model, harness, suite — is an environmental variable applied to that version.

**Flat within version.** Reports for all suites, harnesses, and models coexist in a single folder per version. No sub-nesting by harness or model.

### 2.3 Why Flat

Both harness and model are recorded inside the report JSON (`harness_id`, `subject_model` per run). Forcing one into the directory hierarchy above the other is arbitrary — it privileges one comparison axis and makes the other harder to navigate. Since the summary generator reads metadata from the JSON content regardless of directory placement, the tree structure only needs to scope "which version."

Flat also avoids empty directory combinations. Not every harness x model x suite combination will have results. Nesting would create sparse trees with many empty leaves.

The concern with flat is file volume. A version folder running 5 suites x 3 harnesses x 3 models = 45 reports, and re-runs multiply that. In practice this is manageable — reports are small JSON files, and the filename convention provides human-scannable grouping under `ls`.

### 2.4 Filename Convention

```
{suite-id}--{harness-id}--{model-short}--{timestamp}.json
```

- **suite-id**: from the report's `suite_id` field (e.g. `status-routing`)
- **harness-id**: from the runs' `harness_id` field (e.g. `claude-code`, `opencode`)
- **model-short**: a shortened model identifier derived from `subject_model`, stripping the provider prefix (e.g. `claude-sonnet-4.6` from `github-copilot/claude-sonnet-4.6`)
- **timestamp**: ISO 8601 compact (e.g. `20260823T191126`)
- **separator**: `--` (double dash) to avoid ambiguity with hyphens in identifiers

Sorting by name groups by suite first, then harness, then model. Sorting by modification date gives chronological order.

### 2.5 Version Resolution

The `{orchestrator-version}` directory name comes from the report's `subject_version` field, which is resolved from the orchestrator definition's YAML frontmatter `version` field at deploy time. This field must be populated for storage to work — reports with `subject_version: unknown` cannot be meaningfully filed.

If version is unknown, the `store` command should warn and either refuse or file under an `unknown/` bucket, at the user's discretion.

---

## 3. Report Lifecycle

### 3.1 Run Phase (No Storage)

`mosaic-agent-test` executes a suite and dumps the report JSON to the working directory (currently `dist/`). This is a raw output — debug runs, aborted runs, and experimental runs all land here. Nothing is stored in `TestResults/` automatically.

Default behaviour: storage off. The user is iterating, debugging tests, fixing the tool. Most runs are throwaway.

### 3.2 Store Phase (Explicit Promotion)

The user decides a report represents real, finalized results and explicitly stores it:

```
mosaic-agent-test store <report.json>... [--results-dir TestResults/]
mosaic-agent-test store --dir <folder>  [--results-dir TestResults/]
```

The command accepts one or more report files, or `--dir` to store all `.json` report files in a directory (e.g. `--dir dist/`). When given `--dir`, it scans for files matching the report schema (checks `schema_version` field), skipping non-report JSON files silently.

The `store` command, per report:

1. Reads the report JSON.
2. Validates it is an AgentTest report (has `schema_version`, `suite_id`, `tests`).
3. Extracts `subject_version`, `suite_id`, `harness_id`, `subject_model`, and the run timestamp.
4. Validates that `subject_version` is not `unknown` (warns/refuses if so).
5. Constructs the target path: `TestResults/{version}/{suite}--{harness}--{model-short}--{timestamp}.json`.
6. Creates the version directory if it does not exist.
7. Copies (not moves) the report to the target path.

When processing multiple files, the command reports a summary: `Stored 12 reports (3 skipped: 2 non-report, 1 unknown version)`.

The original reports are untouched. The user can re-store, delete, or ignore them.

### 3.3 Summary Phase (Read-Only Generation)

Summary generation is a separate, read-only operation over the stored results:

```
mosaic-agent-test summary [--results-dir TestResults/] [--version v1.6.2]
```

The `summary` command:

1. Scans the results tree (all versions, or a specific version if `--version` is given).
2. Reads all report JSONs, grouping by version, then by harness and model (from JSON content).
3. Generates `summary.md` files at each level (per-version, and root cross-version).
4. Overwrites existing summaries — they are always regenerated from the source reports.

Summaries are **partially** derived artifacts. The data sections (tables, scores) are regenerated from the source reports on every `summary` invocation. The analysis sections (commentary, pattern observations) are written by an LLM or human and preserved across regenerations. See §4 for the boundary between the two.

---

## 4. Summary Format

Summaries are Markdown with two distinct section types:

- **Data sections** — tables, scores, comparisons. Generated deterministically by the Go tool from report JSON. Overwritten on every `summary` run. Marked with `<!-- generated -->` / `<!-- /generated -->` boundaries.
- **Analysis sections** — pattern commentary, performance narratives, cross-suite insights. Written by an LLM or human after reviewing the data sections. Preserved across regenerations. Marked with `<!-- analysis -->` / `<!-- /analysis -->` boundaries.

The `summary` command regenerates all data sections while leaving analysis sections untouched. If a summary file does not exist yet, the tool creates it with empty analysis section placeholders. If it exists, the tool replaces content inside `<!-- generated -->` blocks and leaves everything else as-is.

**Generic preservation rule:** The Go tool treats any `<!-- analysis:* -->` block as opaque — it never reads, modifies, or removes the content between analysis markers. This means analysis sections can be added freely to the summary template without requiring tool changes. The tool's only contract with analysis blocks is: don't touch them. New analysis placeholders can be introduced in the template at any time; existing ones can be left empty indefinitely.

This means the workflow is:

1. Run `mosaic-agent-test summary` — data tables appear/update.
2. Ask an LLM (or write manually) to fill the analysis sections — pattern recognition, performance narratives, recommendations.
3. Run `mosaic-agent-test summary` again after storing new reports — data tables update, analysis sections survive.

Analysis sections become stale when new data arrives. The tool does not warn about this — it is the user's responsibility to update analysis after regenerating data. A reasonable practice is to regenerate analysis whenever data sections change significantly.

### 4.1 Section Boundary Markers

```markdown
<!-- generated:model-comparison -->
| Model | claude-code | opencode | Average |
|-------|-------------|----------|---------|
| claude-sonnet-4.6 | 83.3% | 66.7% | 75.0% |
<!-- /generated:model-comparison -->

<!-- analysis:model-comparison -->
Sonnet 4.6 consistently outperforms on initial routing decisions (first
dispatch correctness is 95%+ across harnesses) but degrades on re-routing
after findings — the findings-route-back test shows a 20% gap between
harnesses, suggesting the harness system prompt influences how the model
interprets COMPLETED_NEEDS_ACTION status codes.
<!-- /analysis:model-comparison -->
```

The `:name` suffix on markers lets the tool match generated/analysis pairs and enables selective regeneration. The names are stable identifiers, not display text.

### 4.2 Per-Version Summary (`TestResults/{version}/summary.md`)

This is the primary analysis document for one orchestrator version.

### 4.1 Per-Version Summary (`TestResults/{version}/summary.md`)

This is the primary analysis document for one orchestrator version.

#### Section 1: Overview

High-level version stats.

```markdown
# Test Results — Orchestrator {version}

Generated: {timestamp}
Reports: {count}

| Metric | Value |
|--------|-------|
| Suites | 3 |
| Total tests | 12 |
| Models tested | 3 |
| Harnesses tested | 2 |
```

#### Section 2: Model Results

One subsection per model. Each contains the overall model score followed by per-suite breakdown tables.

```markdown
## Model: claude-sonnet-4.6

### Overall
| Harness | Tests | Passed | Pass Rate | Avg Duration | Total Cost |
|---------|-------|--------|-----------|--------------|------------|
| claude-code | 12 | 10 | 83.3% | 38s | $0.42 |
| opencode | 12 | 8 | 66.7% | 45s | $0.38 |

### Suite: status-routing (claude-code)
| Test | Reps | Passed | Rate | Avg Duration | Cost | Failure Classes |
|------|------|--------|------|--------------|------|-----------------|
| findings-route-back | 10 | 8 | 80% | 45s | $0.03 | assertion_failure |
| partially-done-redispatch | 10 | 10 | 100% | 32s | $0.02 | — |

### Suite: status-routing (opencode)
| Test | Reps | Passed | Rate | Avg Duration | Cost | Failure Classes |
|------|------|--------|------|--------------|------|-----------------|
| findings-route-back | 10 | 6 | 60% | 52s | $0.03 | assertion_failure |
| partially-done-redispatch | 10 | 9 | 90% | 38s | $0.02 | — |
```

Repeated for each model. Each model subsection ends with an analysis placeholder:

```markdown
<!-- analysis:model-claude-sonnet-4.6 -->
<!-- /analysis:model-claude-sonnet-4.6 -->
```

This is where an LLM or human writes qualitative observations about the model's performance patterns — e.g. "strong on initial dispatch decisions, degrades on re-routing after COMPLETED_NEEDS_ACTION", "consistently misinterprets On Findings when the target has a hyphenated name."

#### Section 3: Model Comparison

Cross-model table, one row per model, aggregated across all suites.

```markdown
## Model Comparison

### Overall Pass Rate
| Model | claude-code | opencode | Average |
|-------|-------------|----------|---------|
| claude-sonnet-4.6 | 83.3% | 66.7% | 75.0% |
| gpt-4o | 66.7% | 58.3% | 62.5% |
| gemini-2.5-pro | 75.0% | 66.7% | 70.8% |

### Per Suite
#### status-routing
| Model | claude-code | opencode |
|-------|-------------|----------|
| claude-sonnet-4.6 | 90% | 75% |
| gpt-4o | 70% | 60% |
| gemini-2.5-pro | 80% | 70% |

<!-- analysis:model-comparison -->
<!-- /analysis:model-comparison -->
```

Analysis placeholder for comparing model strengths/weaknesses against each other — e.g. "Sonnet excels at re-routing decisions but GPT-4o is more consistent on initial dispatch; Gemini sits between both."

#### Section 4: Harness Comparison

Same data, pivoted — one row per harness, comparing across models. This directly answers "does one harness interfere with correct routing more than another?"

```markdown
## Harness Comparison

### Overall Pass Rate
| Harness | claude-sonnet-4.6 | gpt-4o | gemini-2.5-pro | Average |
|---------|--------------------|--------|----------------|---------|
| claude-code | 83.3% | 66.7% | 75.0% | 75.0% |
| opencode | 66.7% | 58.3% | 66.7% | 63.9% |

### Per Suite
#### status-routing
| Harness | claude-sonnet-4.6 | gpt-4o |
|---------|--------------------|--------|
| claude-code | 90% | 70% |
| opencode | 75% | 60% |

<!-- analysis:harness-comparison -->
<!-- /analysis:harness-comparison -->
```

Analysis placeholder for harness-specific observations — e.g. "OpenCode's system prompt biases the model toward advancing rather than routing back, producing a consistent 10-15% penalty on findings-route-back across all models."

#### Section 5: Problem Areas

Tests with the lowest pass rates across all combinations, highlighting where the orchestrator struggles most.

```markdown
## Lowest Scoring Tests
| Suite / Test | Best | Worst | Spread |
|-------------|------|-------|--------|
| status-routing / findings-route-back | 80% (sonnet/cc) | 50% (gpt-4o/oc) | 30% |
| parallel-dispatch / three-way-fan-out | 90% (sonnet/cc) | 70% (gemini/oc) | 20% |

<!-- analysis:problem-areas -->
<!-- /analysis:problem-areas -->
```

Analysis placeholder for deeper investigation of weak spots — e.g. "findings-route-back failures cluster around the second dispatch decision, suggesting the orchestrator correctly reads the first COMPLETED_NEEDS_ACTION but loses context when building the route-back task message."

#### Section 6: Overall Analysis

The final section of the per-version summary is pure analysis — no generated tables. This is where the LLM or human synthesizes patterns across all the data above.

```markdown
<!-- analysis:overall -->
[LLM/human-written synthesis. Examples of what belongs here:]

- Cross-cutting patterns: "All models struggle with findings routing when
  the On Findings target name is similar to the On Success target."
- Harness impact narrative: "OpenCode's system prompt appears to bias the
  model toward advancing rather than routing back, producing a consistent
  10-15% penalty on findings-route-back across all models."
- Cost/performance trade-offs: "GPT-4o is 30% cheaper per run but 15%
  less accurate — break-even depends on repetition count."
- Recommendations: "Findings routing is the weakest area. Consider
  strengthening the orchestrator's On Findings instructions before the
  next version."
<!-- /analysis:overall -->
```

### 4.3 Cross-Version Summary (`TestResults/summary.md`)

Compares orchestrator versions against each other — the regression/progress view.

```markdown
# Test Results — Cross-Version Summary

Generated: {timestamp}

## Version Comparison (best model+harness per version)
| Version | Best Pass Rate | Model | Harness | Tests | Date |
|---------|---------------|-------|---------|-------|------|
| v1.6.2 | 83.3% | claude-sonnet-4.6 | claude-code | 12 | 2026-08-23 |
| v1.6.1 | 75.0% | claude-sonnet-4.6 | claude-code | 12 | 2026-08-20 |

## Version Comparison (average across all combinations)
| Version | Avg Pass Rate | Models | Harnesses | Total Runs |
|---------|--------------|--------|-----------|------------|
| v1.6.2 | 70.8% | 3 | 2 | 360 |
| v1.6.1 | 65.0% | 2 | 1 | 120 |
```

Analysis placeholder for version-over-version trends:

```markdown
<!-- analysis:version-trend -->
v1.6.2 shows a significant improvement on findings routing (+8% average)
but introduced a regression on parallel dispatch fan-out (-3%). The
findings improvement correlates with the revised COMPLETED_NEEDS_ACTION
handling in the orchestrator's routing logic.
<!-- /analysis:version-trend -->
```

---

## 5. Data Available in Report JSON

For reference, these fields in the report schema feed the storage and summary system:

**Report level (one per file):**
- `suite_id` — which suite was run
- `started_at` / `finished_at` — timestamps
- `counts` — verdict distribution (PASS/FAIL counts)
- `total_cost` — aggregate cost
- `catalog_folder` — which agent catalog was used

**Per-test aggregate:**
- `test_name` — the test definition's `name` field (human-readable display name; this field was named `test_id` and carried a string value in the schema before the versioning and stable-ID change)
- `test_id` — the test definition's stable numeric identity (integer; this field was added alongside the `test_name` rename and now carries the numeric `id` from the test definition)
- `description`, `layer`
- `verdict`, `counted`, `passed`, `pass_rate`, `required_pass_rate`
- `total_cost`, `infrastructure_failure`

**Per-run:**
- `run.test_name` — the test definition's `name` (in the run key; the key field was named `test_id` before the rename)
- `test_version` — the test definition's `version` at the time the run was executed (new field; enables staleness detection in the summary generator)
- `test_id` — the test definition's numeric `id` (new per-run field; integer)
- `harness_id` — which harness adapter produced this run
- `subject_model` — model the orchestrator ran on
- `stub_model` — model used for stub responses (if applicable)
- `subject_version` — orchestrator version from frontmatter
- `duration_ms`, `cost`
- `verdict`, `reasons`, `assertions`, `conditions`
- `termination_reason`

**Schema changes made (additive):** The string `test_id` field in per-test and per-run JSON was renamed to `test_name`. A new integer `test_id` field was added at per-test and per-run levels, carrying the stable numeric identity from the test definition. A new `test_version` field was added at the per-run level. These changes are additive at the wire level — old consumers reading `test_id` as a string will encounter an integer instead.

---

## 6. Open Questions

### 6.0 Staleness Detection

The `resultsummary` package can flag stored results that were collected against an older version of a test definition. When the summary generator reads a stored report, it compares each run's `test_version` against the current test definition's `version` field (read from the `.test.yaml` file at summary time).

If the stored `test_version` is lower than the current definition's `version`, the run's results are marked stale in the summary output — for example, a note in the per-test table such as "(results from v1; current definition is v2)". Stale results are not rejected or excluded from aggregation; they are included with a visual marker so the reader can assess relevance.

This detection is informational only. The summary generator does not refuse to generate summaries for stale results, and stale results do not affect verdict aggregation. The reader decides whether old results are still meaningful given what changed.

**Implementation note:** The `resultsummary` package (`internal/resultsummary`) is implemented and reads stored results from the `resultstore` package. The `TestStats` struct carries `TestName` (display name) and `NumericID` (stable identity) for cross-rename tracking. When a test is renamed (`name` changes) but its numeric `id` is unchanged, the summary generator can match historical results to the current definition by numeric ID.

### 6.1 Multiple Models in One Report

A single suite run currently uses one model (set via `subject.model` in the test definition or suite defaults). If a future suite definition allows per-test model overrides, a single report could contain runs with different `subject_model` values. The filename convention uses one model slug — the `store` command would need to handle this (e.g. use the majority model, or `mixed`).

Current position: not a concern yet. Suite runs are homogeneous per model.

### 6.2 File Volume

A version folder running 5 suites x 3 harnesses x 4 models with 3 re-runs each = 180 files. This is manageable for filesystem browsing and glob-based tooling. If volume becomes a concern, introducing a harness or model subdirectory level is a non-breaking change — the `summary` command would just scan one level deeper.

### 6.3 Report Deduplication

If the user stores the same report twice, the timestamp in the filename prevents overwrite. This means the tree can accumulate duplicates. Options:

- **Ignore it.** Duplicates don't affect summary correctness (summary generator could deduplicate by content hash or run IDs).
- **Detect and warn.** The `store` command checks for an existing file with matching suite/harness/model/timestamp and warns.
- **Content-hash naming.** Use a hash of the report content instead of timestamp. Storing the same report twice produces the same filename, naturally deduplicating.

Current position: detect and warn is sufficient.

### 6.4 Partial Reports

A report where some tests have `infrastructure_failure: true` or where the suite was interrupted mid-run may have incomplete data. The `store` command should not reject these — partial results are still valuable — but the summary generator should flag them (e.g. a note in the summary table indicating incomplete data).

### 6.5 Cost Attribution

Reports where cost attribution is `unknown_bucket` or `unavailable` should still be storable and summarized, but cost columns in summaries should show a warning marker rather than `$0.00` which would be misleading.
