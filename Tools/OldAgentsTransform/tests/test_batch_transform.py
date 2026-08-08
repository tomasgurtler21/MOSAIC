"""Tests for Stage 3: batch runner degraded fallback for files with no generic counterpart.

Tests cover:
- Harness files with no generic counterpart are routed through the degraded transform path
  and are not counted as errors (TDD RED — requires I3.1 implementation)
- Harness-only orchestrators are still skipped and counted as errors (TDD RED — requires I3.2)
- Files with generic counterparts continue to be transformed normally (TDD RED — requires I3.1)
- Batch output clearly identifies files processed through the degraded path (TDD RED — I3.3)
- Temporary-directory fixtures exercise batch discovery without touching real repository files

All test classes are in TDD RED phase: they will fail until batch_transform.py is updated
with BatchSummary, the new run_batch return type, degraded-path routing, orchestrator
filename exclusion, and the new output format.
"""
from __future__ import annotations

import pathlib
import sys
import textwrap

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

import batch_transform as bt  # noqa: E402

# ---------------------------------------------------------------------------
# T3.5 — Minimal fixture content templates (avoids reading real repo files)
# ---------------------------------------------------------------------------

# The fixtures directory contains harness_only_transformed_input.md, which this
# module uses for the "already transformed" scenario so that has_canonical_boundary_tags
# recognises it without duplicating content.
_FIXTURES_DIR = pathlib.Path(__file__).parent / "fixtures"

# A minimal harness-only agent file: has transform_version, no boundary tags.
# Structured so that boundary_transformer.py's degraded path can transform it
# successfully (distinct H2 headings separated by --- delimiters).
_HARNESS_AGENT = textwrap.dedent("""\
    ---
    id: 10
    version: 1.0.0
    transform_version: 1.0.0
    name: batch-test-agent
    description: Minimal harness-only agent for batch runner tests
    ---

    # BatchTestAgent Agent

    You are the **BatchTestAgent** agent in a multi-agent orchestration system.

    **Goal:** Serve as a minimal test fixture for batch runner Stage 3 tests.

    **Scope:**
    - You DO: Provide fixture data for tests
    - You DO NOT: Run in production

    ---

    ## Capabilities

    ### Core Capabilities
    - Provide fixture data for batch runner tests

    ---

    ## Constraints

    - Stay within test scope
    - Do not run in production

    ---

    ## Error Handling

    - Handle errors gracefully

    ---

    ## Output Format

    Return results as structured data.
""")

# A minimal harness-only agent whose frontmatter carries `transform_version` but
# no `version` field, so default_version() must supply a substitute.
_HARNESS_AGENT_NO_VERSION = textwrap.dedent("""\
    ---
    id: 11
    transform_version: 1.0.0
    name: no-version-batch-agent
    description: Harness agent with no version field for default_version tests
    ---

    # NoVersionBatchAgent Agent

    You are the **NoVersionBatchAgent** agent.

    ---

    ## Capabilities

    - Serve as a test fixture

    ---

    ## Constraints

    - Do not run in production
""")

# A minimal harness-only orchestrator file — named orchestrator.md so
# is_orchestrator_file() returns True and run_batch must skip it.
_HARNESS_ORCHESTRATOR = textwrap.dedent("""\
    ---
    id: 12
    version: 1.0.0
    transform_version: 1.0.0
    name: orchestrator
    description: Minimal harness orchestrator fixture for batch runner tests
    ---

    # Orchestrator Agent

    You are the Orchestrator.

    ---

    ## Capabilities

    - Orchestrate multi-agent runs
""")

# A minimal generic (non-harness) counterpart file: no transform_version, has version.
# Named so it can be placed in a separate directory and keyed by stem in generic_map.
_GENERIC_AGENT = textwrap.dedent("""\
    ---
    id: 13
    version: 1.0.0
    name: matched-agent
    description: Minimal generic agent counterpart for batch runner tests
    ---

    # MatchedAgent Agent

    You are the **MatchedAgent** agent.

    ---

    ## Capabilities

    ### Core Capabilities
    - Does stuff

    ---

    ## Constraints

    - Does not run in production

    ---

    ## Error Handling

    - Handle errors gracefully

    ---

    ## Output Format

    Return results as structured data.
""")


# ---------------------------------------------------------------------------
# T3.5 — Pytest fixtures: synthetic harness directories
# ---------------------------------------------------------------------------

@pytest.fixture()
def harness_dir(tmp_path: pathlib.Path) -> pathlib.Path:
    """Return an empty synthetic harness directory with an Agents/ subdirectory.

    No real repository agent files are touched; all content is created in tmp_path.
    """
    hdir = tmp_path / "SyntheticHarness"
    hdir.mkdir()
    (hdir / "Agents").mkdir()
    return hdir


@pytest.fixture()
def harness_file_no_generic(harness_dir: pathlib.Path) -> pathlib.Path:
    """Write a minimal untransformed harness agent file with no generic_map entry.

    Because this agent's stem ('no-generic-agent') is absent from generic_map,
    run_batch must route it through the degraded transform path rather than counting
    it as an error.
    """
    agent_file = harness_dir / "Agents" / "no-generic-agent.md"
    agent_file.write_text(_HARNESS_AGENT, encoding="utf-8")
    return agent_file


@pytest.fixture()
def harness_file_no_version(harness_dir: pathlib.Path) -> pathlib.Path:
    """Write a harness agent with transform_version but no version field.

    Used to verify that the degraded path uses default_version() when version is absent.
    """
    agent_file = harness_dir / "Agents" / "no-version-batch-agent.md"
    agent_file.write_text(_HARNESS_AGENT_NO_VERSION, encoding="utf-8")
    return agent_file


@pytest.fixture()
def harness_file_already_transformed(harness_dir: pathlib.Path) -> pathlib.Path:
    """Write a harness agent that already carries canonical boundary tags.

    Sourced from fixtures/harness_only_transformed_input.md so that
    has_canonical_boundary_tags() reliably detects it as already transformed.
    When run_batch routes it through the degraded path (no generic counterpart),
    transform_file's idempotency guard must detect it and return skipped=True.
    """
    source_content = (_FIXTURES_DIR / "harness_only_transformed_input.md").read_text(
        encoding="utf-8"
    )
    agent_file = harness_dir / "Agents" / "already-transformed-agent.md"
    agent_file.write_text(source_content, encoding="utf-8")
    return agent_file


@pytest.fixture()
def harness_orchestrator_md(harness_dir: pathlib.Path) -> pathlib.Path:
    """Write a harness orchestrator.md at the harness directory root (not under Agents/).

    is_orchestrator_file() returns True for this filename so run_batch must skip it
    and count it as an error rather than routing it through the degraded path.
    Placed according to get_harness_files(): orchestrators live at harness_dir root.
    """
    orch_file = harness_dir / "orchestrator.md"
    orch_file.write_text(_HARNESS_ORCHESTRATOR, encoding="utf-8")
    return orch_file


@pytest.fixture()
def harness_orchestrator_agent_md(harness_dir: pathlib.Path) -> pathlib.Path:
    """Write a harness orchestrator.agent.md at the harness directory root.

    is_orchestrator_file() returns True for this filename so run_batch must skip it
    and count it as an error rather than routing it through the degraded path.
    """
    orch_file = harness_dir / "orchestrator.agent.md"
    orch_file.write_text(_HARNESS_ORCHESTRATOR, encoding="utf-8")
    return orch_file


@pytest.fixture()
def harness_dir_with_generic_match(
    tmp_path: pathlib.Path,
) -> tuple[pathlib.Path, dict[str, pathlib.Path]]:
    """Create a harness directory with one agent whose stem matches a generic_map entry.

    Returns (harness_dir, generic_map).  The harness agent is named 'matched-agent.md';
    the generic_map maps the stem 'matched-agent' to a synthetic generic file so that
    run_batch routes the harness file through the normal (non-degraded) transform.
    """
    hdir = tmp_path / "HarnessWithGeneric"
    hdir.mkdir()
    (hdir / "Agents").mkdir()

    harness_content = _HARNESS_AGENT.replace(
        "name: batch-test-agent", "name: matched-agent"
    ).replace("id: 10", "id: 20")
    (hdir / "Agents" / "matched-agent.md").write_text(harness_content, encoding="utf-8")

    generic_dir = tmp_path / "GenericAgents"
    generic_dir.mkdir()
    generic_file = generic_dir / "matched-agent.md"
    generic_file.write_text(_GENERIC_AGENT, encoding="utf-8")

    return hdir, {"matched-agent": generic_file}


# ---------------------------------------------------------------------------
# BatchSummary dataclass contract (prerequisite for all other tests)
# ---------------------------------------------------------------------------

class TestBatchSummaryDataclass:
    """BatchSummary must be defined in batch_transform with the correct fields and ok()."""

    def test_batch_summary_class_exists(self):
        """BatchSummary must be defined in batch_transform."""
        assert hasattr(bt, "BatchSummary"), (
            "bt.BatchSummary not found; BatchSummary must be defined in batch_transform.py"
        )

    def test_batch_summary_has_processed_field(self):
        """BatchSummary must accept a processed kwarg counting transform attempts."""
        s = bt.BatchSummary(processed=3, degraded=1, skipped=0, errors=0)
        assert s.processed == 3

    def test_batch_summary_has_degraded_field(self):
        """BatchSummary must accept a degraded kwarg counting degraded-path files."""
        s = bt.BatchSummary(processed=1, degraded=1, skipped=0, errors=0)
        assert s.degraded == 1

    def test_batch_summary_has_skipped_field(self):
        """BatchSummary must accept a skipped kwarg counting already-transformed files."""
        s = bt.BatchSummary(processed=2, degraded=0, skipped=1, errors=0)
        assert s.skipped == 1

    def test_batch_summary_has_errors_field(self):
        """BatchSummary must accept an errors kwarg counting failures and skipped orchestrators."""
        s = bt.BatchSummary(processed=0, degraded=0, skipped=0, errors=2)
        assert s.errors == 2

    def test_batch_summary_ok_true_when_no_errors(self):
        """BatchSummary.ok() must return True when errors is 0."""
        s = bt.BatchSummary(processed=2, degraded=1, skipped=0, errors=0)
        assert s.ok() is True

    def test_batch_summary_ok_false_when_errors_nonzero(self):
        """BatchSummary.ok() must return False when errors > 0."""
        s = bt.BatchSummary(processed=1, degraded=0, skipped=0, errors=1)
        assert s.ok() is False

    def test_batch_summary_ok_false_for_pure_error_run(self):
        """BatchSummary.ok() must return False when processed=0 and errors>0 (orchestrator only)."""
        s = bt.BatchSummary(processed=0, degraded=0, skipped=0, errors=1)
        assert s.ok() is False


# ---------------------------------------------------------------------------
# T3.1 — No-generic-counterpart path: degraded transform, not error
# ---------------------------------------------------------------------------

class TestNogenericCounterpartDegradedPath:
    """A harness file with no generic counterpart must be transformed via the degraded
    path rather than counted as an error."""

    def test_run_batch_returns_batch_summary(
        self, harness_file_no_generic, harness_dir
    ):
        """run_batch must return a BatchSummary, not a bool."""
        result = bt.run_batch([harness_dir], {}, "TEST")
        assert isinstance(result, bt.BatchSummary), (
            f"run_batch must return a BatchSummary; got {type(result).__name__!r}"
        )

    def test_no_generic_counterpart_increments_processed(
        self, harness_file_no_generic, harness_dir
    ):
        """A harness file with no generic counterpart must increment processed."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.processed == 1, (
            f"Expected processed=1 for degraded-path file; got {summary.processed}"
        )

    def test_no_generic_counterpart_increments_degraded(
        self, harness_file_no_generic, harness_dir
    ):
        """A harness file with no generic counterpart must increment the degraded counter."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.degraded == 1, (
            f"Expected degraded=1 for file without generic counterpart; got {summary.degraded}"
        )

    def test_no_generic_counterpart_does_not_increment_errors(
        self, harness_file_no_generic, harness_dir
    ):
        """The degraded path must NOT increment the errors counter."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.errors == 0, (
            f"Expected errors=0 for degraded-path file; got {summary.errors}"
        )

    def test_ok_returns_true_when_only_degraded(
        self, harness_file_no_generic, harness_dir
    ):
        """ok() must return True when the batch contains only degraded-path transforms."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.ok() is True

    def test_no_generic_counterpart_file_is_transformed(
        self, harness_file_no_generic, harness_dir
    ):
        """The degraded-path transform must write output (the file content changes)."""
        content_before = harness_file_no_generic.read_text(encoding="utf-8")
        bt.run_batch([harness_dir], {}, "TEST")
        content_after = harness_file_no_generic.read_text(encoding="utf-8")
        assert content_after != content_before, (
            "Degraded-path transform must write output to the file; content was unchanged"
        )

    def test_no_generic_counterpart_output_contains_boundary_tags(
        self, harness_file_no_generic, harness_dir
    ):
        """The degraded-path output must contain [[SECTION:...]] boundary tags."""
        bt.run_batch([harness_dir], {}, "TEST")
        content_after = harness_file_no_generic.read_text(encoding="utf-8")
        assert "[[SECTION:" in content_after, (
            "Degraded-path output must contain [[SECTION:...]] boundary tags"
        )

    def test_degraded_and_errors_are_mutually_exclusive(
        self, harness_file_no_generic, harness_dir
    ):
        """A file counted as degraded must not also be counted as an error."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert not (summary.degraded > 0 and summary.errors > 0), (
            "degraded and errors must be mutually exclusive for a no-counterpart non-orchestrator file"
        )

    def test_no_version_field_uses_default_version(
        self, harness_file_no_version, harness_dir
    ):
        """When frontmatter has no version field, the degraded path must still succeed."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.processed == 1
        assert summary.degraded == 1
        assert summary.errors == 0

    def test_multiple_no_generic_files_all_degraded(
        self, harness_dir, tmp_path
    ):
        """All harness files with no generic counterpart in a single harness dir are degraded."""
        for i in range(3):
            content = _HARNESS_AGENT.replace("id: 10", f"id: {100 + i}").replace(
                "name: batch-test-agent", f"name: batch-agent-{i}"
            )
            (harness_dir / "Agents" / f"batch-agent-{i}.md").write_text(
                content, encoding="utf-8"
            )

        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.processed == 3, (
            f"Expected processed=3 for three degraded-path files; got {summary.processed}"
        )
        assert summary.degraded == 3, (
            f"Expected degraded=3; got {summary.degraded}"
        )
        assert summary.errors == 0


# ---------------------------------------------------------------------------
# T3.2 — Harness-only orchestrators: skipped and counted as errors
# ---------------------------------------------------------------------------

class TestHarnessOnlyOrchestratorSkip:
    """Harness-only orchestrators must still be skipped and counted as errors by run_batch."""

    def test_orchestrator_md_increments_errors(
        self, harness_orchestrator_md, harness_dir
    ):
        """orchestrator.md with no generic counterpart must be counted as an error."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.errors == 1, (
            f"Expected errors=1 for harness-only orchestrator.md; got {summary.errors}"
        )

    def test_orchestrator_agent_md_increments_errors(
        self, harness_orchestrator_agent_md, harness_dir
    ):
        """orchestrator.agent.md with no generic counterpart must be counted as an error."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.errors == 1, (
            f"Expected errors=1 for harness-only orchestrator.agent.md; got {summary.errors}"
        )

    def test_orchestrator_md_not_processed(
        self, harness_orchestrator_md, harness_dir
    ):
        """orchestrator.md must NOT increment processed (no transform was attempted)."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.processed == 0, (
            f"Expected processed=0 for skipped orchestrator; got {summary.processed}"
        )

    def test_orchestrator_agent_md_not_processed(
        self, harness_orchestrator_agent_md, harness_dir
    ):
        """orchestrator.agent.md must NOT increment processed (no transform was attempted)."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.processed == 0, (
            f"Expected processed=0 for skipped orchestrator; got {summary.processed}"
        )

    def test_orchestrator_not_degraded(
        self, harness_orchestrator_md, harness_dir
    ):
        """An orchestrator skip must not increment the degraded counter."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.degraded == 0, (
            f"Expected degraded=0 for skipped orchestrator; got {summary.degraded}"
        )

    def test_ok_returns_false_for_orchestrator_only_batch(
        self, harness_orchestrator_md, harness_dir
    ):
        """ok() must return False when the only file is a harness-only orchestrator."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.ok() is False

    def test_orchestrator_md_content_not_written(
        self, harness_orchestrator_md, harness_dir
    ):
        """orchestrator.md must not be written; its content must be byte-identical after run."""
        content_before = harness_orchestrator_md.read_text(encoding="utf-8")
        bt.run_batch([harness_dir], {}, "TEST")
        content_after = harness_orchestrator_md.read_text(encoding="utf-8")
        assert content_after == content_before, (
            "Harness-only orchestrator must not be transformed; content changed unexpectedly"
        )

    def test_orchestrator_agent_md_content_not_written(
        self, harness_orchestrator_agent_md, harness_dir
    ):
        """orchestrator.agent.md must not be written; its content must be byte-identical after run."""
        content_before = harness_orchestrator_agent_md.read_text(encoding="utf-8")
        bt.run_batch([harness_dir], {}, "TEST")
        content_after = harness_orchestrator_agent_md.read_text(encoding="utf-8")
        assert content_after == content_before, (
            "Harness-only orchestrator must not be transformed; content changed unexpectedly"
        )

    def test_non_orchestrator_filename_routes_to_degraded_not_error(
        self, harness_dir
    ):
        """A file with orchestrator-like content but a non-orchestrator filename is NOT skipped.

        Classification is canonical by filename only; frontmatter role is never consulted.
        A file named 'my-orchestrator.md' inside Agents/ with transform_version must be
        routed through the degraded path, not skipped as a harness-only orchestrator.
        """
        content = _HARNESS_ORCHESTRATOR.replace(
            "name: orchestrator", "name: my-orchestrator"
        ).replace("id: 12", "id: 50")
        non_orch_file = harness_dir / "Agents" / "my-orchestrator.md"
        non_orch_file.write_text(content, encoding="utf-8")

        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.errors == 0, (
            "A file named 'my-orchestrator.md' must not be classified as an orchestrator "
            "(filename classification only); got errors > 0"
        )
        assert summary.processed == 1, (
            "A non-orchestrator filename must be processed via the degraded path"
        )
        assert summary.degraded == 1

    def test_mixed_orchestrator_and_regular_agent(
        self, harness_orchestrator_md, harness_file_no_generic, harness_dir
    ):
        """When both an orchestrator and a regular agent are present, counts are correct.

        orchestrator.md: errors=1 (skipped as orchestrator)
        no-generic-agent.md: processed=1, degraded=1
        Combined: processed=1, degraded=1, errors=1
        """
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.errors == 1, (
            f"Expected errors=1 (orchestrator skip); got {summary.errors}"
        )
        assert summary.processed == 1, (
            f"Expected processed=1 (the regular agent); got {summary.processed}"
        )
        assert summary.degraded == 1, (
            f"Expected degraded=1 (the regular agent); got {summary.degraded}"
        )


# ---------------------------------------------------------------------------
# T3.3 — Generic-counterpart path: unchanged behavior and counting
# ---------------------------------------------------------------------------

class TestGenericCounterpartUnchanged:
    """Files with a matching generic counterpart must be transformed normally
    with unchanged processed/degraded/errors counting semantics."""

    def test_run_batch_returns_batch_summary_for_matched_files(
        self, harness_dir_with_generic_match
    ):
        """run_batch must return a BatchSummary even when all files have generic counterparts."""
        hdir, generic_map = harness_dir_with_generic_match
        result = bt.run_batch([hdir], generic_map, "TEST")
        assert isinstance(result, bt.BatchSummary), (
            f"run_batch must return a BatchSummary; got {type(result).__name__!r}"
        )

    def test_generic_counterpart_increments_processed(
        self, harness_dir_with_generic_match
    ):
        """A file with a matching generic counterpart must increment processed."""
        hdir, generic_map = harness_dir_with_generic_match
        summary = bt.run_batch([hdir], generic_map, "TEST")
        assert summary.processed == 1, (
            f"Expected processed=1 for file with generic counterpart; got {summary.processed}"
        )

    def test_generic_counterpart_does_not_increment_degraded(
        self, harness_dir_with_generic_match
    ):
        """A file with a matching generic counterpart must NOT increment the degraded counter."""
        hdir, generic_map = harness_dir_with_generic_match
        summary = bt.run_batch([hdir], generic_map, "TEST")
        assert summary.degraded == 0, (
            f"Expected degraded=0 for file with generic counterpart; got {summary.degraded}"
        )

    def test_generic_counterpart_does_not_increment_skipped(
        self, harness_dir_with_generic_match
    ):
        """A file with a matching generic counterpart must NOT increment the skipped counter."""
        hdir, generic_map = harness_dir_with_generic_match
        summary = bt.run_batch([hdir], generic_map, "TEST")
        assert summary.skipped == 0, (
            f"Expected skipped=0 for file with generic counterpart; got {summary.skipped}"
        )

    def test_mixed_batch_routing_counts(self, harness_dir, tmp_path):
        """When matched and unmatched files coexist, each is routed independently.

        matched.md (in generic_map): processed=1, degraded=0
        unmatched.md (not in generic_map): processed=1, degraded=1
        Combined: processed=2, degraded=1, errors=0
        """
        matched_content = _HARNESS_AGENT.replace(
            "name: batch-test-agent", "name: matched"
        ).replace("id: 10", "id: 30")
        (harness_dir / "Agents" / "matched.md").write_text(
            matched_content, encoding="utf-8"
        )

        unmatched_content = _HARNESS_AGENT.replace(
            "name: batch-test-agent", "name: unmatched"
        ).replace("id: 10", "id: 31")
        (harness_dir / "Agents" / "unmatched.md").write_text(
            unmatched_content, encoding="utf-8"
        )

        generic_dir = tmp_path / "GenericForMixed"
        generic_dir.mkdir()
        generic_file = generic_dir / "matched.md"
        generic_file.write_text(
            _GENERIC_AGENT.replace("name: matched-agent", "name: matched"),
            encoding="utf-8",
        )
        generic_map = {"matched": generic_file}

        summary = bt.run_batch([harness_dir], generic_map, "TEST")
        assert summary.processed == 2, (
            f"Expected processed=2 (one matched, one degraded); got {summary.processed}"
        )
        assert summary.degraded == 1, (
            f"Expected degraded=1 (only the unmatched file); got {summary.degraded}"
        )
        assert summary.errors == 0, (
            f"Expected errors=0; got {summary.errors}"
        )


# ---------------------------------------------------------------------------
# T3.4 — Batch output format: degraded files visibly distinguished
# ---------------------------------------------------------------------------

class TestBatchOutputFormat:
    """Batch output must visibly distinguish files processed through the degraded path
    from files processed normally."""

    def test_degraded_prefix_in_stdout_for_no_generic_counterpart(
        self, harness_file_no_generic, harness_dir, capsys
    ):
        """run_batch must print a line with '[DEGRADED]' to stdout for degraded-path files."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "[DEGRADED]" in captured.out, (
            "Expected '[DEGRADED]' prefix in stdout for file without generic counterpart; "
            f"got stdout: {captured.out!r}"
        )

    def test_no_ok_prefix_for_pure_degraded_file(
        self, harness_file_no_generic, harness_dir, capsys
    ):
        """A file processed through the degraded path must NOT print an [OK] line."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "[OK]" not in captured.out, (
            "Expected no '[OK]' prefix for a degraded-path file; "
            f"got stdout: {captured.out!r}"
        )

    def test_warn_prefix_for_harness_only_orchestrator(
        self, harness_orchestrator_md, harness_dir, capsys
    ):
        """run_batch must emit a '[WARN]' line when a harness-only orchestrator is skipped."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        all_output = captured.out + captured.err
        assert "[WARN]" in all_output, (
            "Expected '[WARN]' for harness-only orchestrator skip; "
            f"stdout={captured.out!r} stderr={captured.err!r}"
        )

    def test_ok_prefix_for_file_with_generic_counterpart(
        self, harness_dir_with_generic_match, capsys
    ):
        """run_batch must print an '[OK]' line for a successfully transformed file with a generic ref."""
        hdir, generic_map = harness_dir_with_generic_match
        bt.run_batch([hdir], generic_map, "TEST")
        captured = capsys.readouterr()
        assert "[OK]" in captured.out, (
            "Expected '[OK]' prefix for file with generic counterpart; "
            f"got stdout: {captured.out!r}"
        )

    def test_skip_prefix_for_already_transformed_file(
        self, harness_file_already_transformed, harness_dir, capsys
    ):
        """run_batch must print a '[SKIP]' line for an already-transformed file."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "[SKIP]" in captured.out, (
            "Expected '[SKIP]' prefix for already-transformed file with no generic counterpart; "
            f"got stdout: {captured.out!r}"
        )

    def test_already_transformed_file_increments_skipped_not_degraded(
        self, harness_file_already_transformed, harness_dir
    ):
        """An already-transformed file with no counterpart must increment skipped, not degraded."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.skipped == 1, (
            f"Expected skipped=1 for already-transformed file; got {summary.skipped}"
        )
        assert summary.degraded == 0, (
            f"Expected degraded=0 for already-transformed file; got {summary.degraded}"
        )

    def test_already_transformed_file_increments_processed(
        self, harness_file_already_transformed, harness_dir
    ):
        """An already-transformed file is still counted in processed (transform was attempted)."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.processed == 1, (
            f"Expected processed=1 for already-transformed file (transform was attempted); "
            f"got {summary.processed}"
        )

    def test_summary_line_present_in_stdout(
        self, harness_file_no_generic, harness_dir, capsys
    ):
        """run_batch must print a summary line to stdout."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "Batch TEST:" in captured.out, (
            f"Expected 'Batch TEST:' in stdout; got: {captured.out!r}"
        )

    def test_summary_line_format_processed_count(
        self, harness_file_no_generic, harness_dir, capsys
    ):
        """Summary line must report the correct processed count."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "1 files processed" in captured.out, (
            f"Expected '1 files processed' in summary line; got: {captured.out!r}"
        )

    def test_summary_line_format_degraded_count(
        self, harness_file_no_generic, harness_dir, capsys
    ):
        """Summary line must report the degraded count in the parenthetical."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "1 degraded" in captured.out, (
            f"Expected '1 degraded' in summary parenthetical; got: {captured.out!r}"
        )

    def test_summary_line_format_errors_count(
        self, harness_file_no_generic, harness_dir, capsys
    ):
        """Summary line must report errors count."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "0 errors" in captured.out, (
            f"Expected '0 errors' in summary; got: {captured.out!r}"
        )

    def test_summary_line_format_for_orchestrator_error(
        self, harness_orchestrator_md, harness_dir, capsys
    ):
        """Summary line must report errors=1 when only a harness-only orchestrator was found."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "1 errors" in captured.out, (
            f"Expected '1 errors' in summary; got: {captured.out!r}"
        )

    def test_summary_line_format_skipped_count(
        self, harness_file_already_transformed, harness_dir, capsys
    ):
        """Summary line must report the skipped count in the parenthetical."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "1 skipped" in captured.out, (
            f"Expected '1 skipped' in summary parenthetical; got: {captured.out!r}"
        )

    def test_degraded_line_contains_version_arrow(
        self, harness_file_no_generic, harness_dir, capsys
    ):
        """The [DEGRADED] output line must include a 'version_before -> version_after' segment."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        degraded_lines = [
            line for line in captured.out.splitlines() if "[DEGRADED]" in line
        ]
        assert degraded_lines, (
            f"Expected at least one [DEGRADED] line in stdout; got: {captured.out!r}"
        )
        assert "->" in degraded_lines[0], (
            f"[DEGRADED] line must include 'version_before -> version_after'; "
            f"got: {degraded_lines[0]!r}"
        )

    def test_degraded_line_notes_no_generic_reference(
        self, harness_file_no_generic, harness_dir, capsys
    ):
        """The [DEGRADED] output line must note that no generic reference was available."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        degraded_lines = [
            line for line in captured.out.splitlines() if "[DEGRADED]" in line
        ]
        assert degraded_lines, (
            f"Expected at least one [DEGRADED] line in stdout; got: {captured.out!r}"
        )
        assert "no generic reference" in degraded_lines[0].lower(), (
            f"[DEGRADED] line must note 'no generic reference'; got: {degraded_lines[0]!r}"
        )

    def test_degraded_not_in_stdout_for_generic_counterpart_file(
        self, harness_dir_with_generic_match, capsys
    ):
        """A file with a generic counterpart must not produce a [DEGRADED] line."""
        hdir, generic_map = harness_dir_with_generic_match
        bt.run_batch([hdir], generic_map, "TEST")
        captured = capsys.readouterr()
        assert "[DEGRADED]" not in captured.out, (
            "Expected no '[DEGRADED]' line for file with generic counterpart; "
            f"got stdout: {captured.out!r}"
        )


# ===========================================================================
# Stage 5 — Utility & Non-Agent File Skipping in run_batch (T5.3)
# ===========================================================================
#
# These tests cover:
#   T5.3a  A utility-agent file in the batch increments skipped but NOT processed
#   T5.3b  A non-agent file in the batch increments skipped but NOT processed
#   T5.3c  Utility-agent and non-agent skips do NOT increment errors
#   T5.3d  run_batch prints '[SKIP]' with the reason for utility-agent files
#   T5.3e  run_batch prints '[SKIP]' with the reason for non-agent files
#   T5.3f  The skip reason text identifies utility agents vs non-agent files
#   T5.3g  Utility-agent file content is unchanged after run_batch
#   T5.3h  Utility-agent skips are distinguishable from already-transformed skips
#          (already-transformed increments processed; utility-agent does not)
#   T5.3i  Mixed scenario: utility skip + normal file counts correctly
#
# All tests here are RED until Stage 5 implementation is complete.
# ===========================================================================

# Minimal content for skip-fixture files.  The content is never read by
# transform_file (skip happens by filename before content access), so any
# non-empty string suffices.
_MINIMAL_SKIP_CONTENT = "# Skip fixture\n\nContent is never read.\n"


# ---------------------------------------------------------------------------
# T5.3 — Pytest fixtures for Stage 5 skip scenarios
# ---------------------------------------------------------------------------

@pytest.fixture()
def utility_agent_file(harness_dir: pathlib.Path) -> pathlib.Path:
    """Write a file named 'transformation.md' into harness_dir/Agents/.

    'transformation' is in the UTILITY_AGENT_FILENAMES allowlist, so run_batch
    must skip it without writing output and without incrementing processed.
    """
    agent_file = harness_dir / "Agents" / "transformation.md"
    agent_file.write_text(_MINIMAL_SKIP_CONTENT, encoding="utf-8")
    return agent_file


@pytest.fixture()
def non_agent_file(harness_dir: pathlib.Path) -> pathlib.Path:
    """Write a file named 'Cheap orchestrator.md' into harness_dir/Agents/.

    'cheap orchestrator' is in the NON_AGENT_FILENAMES allowlist, so run_batch
    must skip it without writing output and without incrementing processed.
    """
    agent_file = harness_dir / "Agents" / "Cheap orchestrator.md"
    agent_file.write_text(_MINIMAL_SKIP_CONTENT, encoding="utf-8")
    return agent_file


@pytest.fixture()
def system_prompt_capturer_file(harness_dir: pathlib.Path) -> pathlib.Path:
    """Write a file named 'system-prompt-capturer.md' into harness_dir/Agents/.

    Used to verify skip behavior for a second utility-agent name.
    """
    agent_file = harness_dir / "Agents" / "system-prompt-capturer.md"
    agent_file.write_text(_MINIMAL_SKIP_CONTENT, encoding="utf-8")
    return agent_file


# ---------------------------------------------------------------------------
# T5.3a — Utility-agent skip: counter behavior
# ---------------------------------------------------------------------------

class TestUtilityAgentSkipCounters:
    """run_batch must not count a utility-agent skip as processed, degraded, or an error."""

    def test_utility_agent_does_not_increment_processed(
        self, utility_agent_file, harness_dir
    ):
        """A utility-agent file must NOT increment the processed counter."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.processed == 0, (
            f"Expected processed=0 for utility-agent skip; got {summary.processed}"
        )

    def test_utility_agent_increments_skipped(
        self, utility_agent_file, harness_dir
    ):
        """A utility-agent file must increment the skipped counter."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.skipped == 1, (
            f"Expected skipped=1 for utility-agent skip; got {summary.skipped}"
        )

    def test_utility_agent_does_not_increment_errors(
        self, utility_agent_file, harness_dir
    ):
        """A utility-agent file must NOT increment the errors counter."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.errors == 0, (
            f"Expected errors=0 for utility-agent skip; got {summary.errors}"
        )

    def test_utility_agent_does_not_increment_degraded(
        self, utility_agent_file, harness_dir
    ):
        """A utility-agent file must NOT increment the degraded counter."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.degraded == 0, (
            f"Expected degraded=0 for utility-agent skip; got {summary.degraded}"
        )

    def test_ok_returns_true_for_utility_agent_only_batch(
        self, utility_agent_file, harness_dir
    ):
        """ok() must return True when the only file is a utility-agent (not an error)."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.ok() is True, (
            "ok() must return True for a batch that only skipped a utility-agent file"
        )

    def test_utility_agent_file_content_unchanged_after_batch(
        self, utility_agent_file, harness_dir
    ):
        """A utility-agent file must not be modified or overwritten by run_batch."""
        content_before = utility_agent_file.read_text(encoding="utf-8")
        bt.run_batch([harness_dir], {}, "TEST")
        content_after = utility_agent_file.read_text(encoding="utf-8")
        assert content_after == content_before, (
            "run_batch must not write to a utility-agent file; content changed unexpectedly"
        )


# ---------------------------------------------------------------------------
# T5.3b — Non-agent skip: counter behavior
# ---------------------------------------------------------------------------

class TestNonAgentSkipCounters:
    """run_batch must not count a non-agent skip as processed, degraded, or an error."""

    def test_non_agent_does_not_increment_processed(
        self, non_agent_file, harness_dir
    ):
        """A non-agent file must NOT increment the processed counter."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.processed == 0, (
            f"Expected processed=0 for non-agent skip; got {summary.processed}"
        )

    def test_non_agent_increments_skipped(
        self, non_agent_file, harness_dir
    ):
        """A non-agent file must increment the skipped counter."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.skipped == 1, (
            f"Expected skipped=1 for non-agent skip; got {summary.skipped}"
        )

    def test_non_agent_does_not_increment_errors(
        self, non_agent_file, harness_dir
    ):
        """A non-agent file must NOT increment the errors counter."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.errors == 0, (
            f"Expected errors=0 for non-agent skip; got {summary.errors}"
        )

    def test_ok_returns_true_for_non_agent_only_batch(
        self, non_agent_file, harness_dir
    ):
        """ok() must return True when the only file is a non-agent (not an error)."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.ok() is True

    def test_non_agent_file_content_unchanged_after_batch(
        self, non_agent_file, harness_dir
    ):
        """A non-agent file must not be modified by run_batch."""
        content_before = non_agent_file.read_text(encoding="utf-8")
        bt.run_batch([harness_dir], {}, "TEST")
        content_after = non_agent_file.read_text(encoding="utf-8")
        assert content_after == content_before, (
            "run_batch must not write to a non-agent file; content changed unexpectedly"
        )


# ---------------------------------------------------------------------------
# T5.3d/e — Batch output format: [SKIP] with reason
# ---------------------------------------------------------------------------

class TestUtilityAgentSkipOutputFormat:
    """run_batch must print a '[SKIP]' line that identifies the utility-agent reason."""

    def test_skip_prefix_in_stdout_for_utility_agent(
        self, utility_agent_file, harness_dir, capsys
    ):
        """run_batch must print a line with '[SKIP]' for a utility-agent file."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "[SKIP]" in captured.out, (
            "Expected '[SKIP]' prefix in stdout for utility-agent file; "
            f"got stdout: {captured.out!r}"
        )

    def test_skip_line_contains_filename_for_utility_agent(
        self, utility_agent_file, harness_dir, capsys
    ):
        """The [SKIP] line must include the filename so the skip is attributable."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        skip_lines = [line for line in captured.out.splitlines() if "[SKIP]" in line]
        assert skip_lines, "Expected at least one [SKIP] line in stdout"
        assert "transformation" in skip_lines[0].lower() or "transformation.md" in skip_lines[0], (
            f"[SKIP] line must contain the filename 'transformation'; got {skip_lines[0]!r}"
        )

    def test_skip_line_contains_utility_reason(
        self, utility_agent_file, harness_dir, capsys
    ):
        """The [SKIP] line for a utility-agent must identify the reason as 'utility agent'."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        skip_lines = [line for line in captured.out.splitlines() if "[SKIP]" in line]
        assert skip_lines, "Expected at least one [SKIP] line in stdout"
        assert "utility" in skip_lines[0].lower(), (
            f"[SKIP] line for utility-agent must mention 'utility'; got {skip_lines[0]!r}"
        )

    def test_skip_line_does_not_say_already_transformed(
        self, utility_agent_file, harness_dir, capsys
    ):
        """The [SKIP] line for a utility-agent must NOT say 'already transformed'."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        skip_lines = [line for line in captured.out.splitlines() if "[SKIP]" in line]
        assert skip_lines, "Expected at least one [SKIP] line in stdout"
        assert "already transformed" not in skip_lines[0].lower(), (
            f"[SKIP] line for utility-agent must not say 'already transformed'; "
            f"got {skip_lines[0]!r}"
        )


class TestNonAgentSkipOutputFormat:
    """run_batch must print a '[SKIP]' line that identifies the non-agent reason."""

    def test_skip_prefix_in_stdout_for_non_agent(
        self, non_agent_file, harness_dir, capsys
    ):
        """run_batch must print a line with '[SKIP]' for a non-agent file."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "[SKIP]" in captured.out, (
            "Expected '[SKIP]' prefix in stdout for non-agent file; "
            f"got stdout: {captured.out!r}"
        )

    def test_skip_line_contains_filename_for_non_agent(
        self, non_agent_file, harness_dir, capsys
    ):
        """The [SKIP] line must include the filename for the non-agent skip."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        skip_lines = [line for line in captured.out.splitlines() if "[SKIP]" in line]
        assert skip_lines, "Expected at least one [SKIP] line in stdout"
        # The filename 'Cheap orchestrator.md' contains the word 'orchestrator'
        assert "orchestrator" in skip_lines[0].lower() or "cheap" in skip_lines[0].lower(), (
            f"[SKIP] line must contain the filename; got {skip_lines[0]!r}"
        )

    def test_skip_line_contains_non_agent_reason(
        self, non_agent_file, harness_dir, capsys
    ):
        """The [SKIP] line for a non-agent must mention 'not a MOSAIC agent' or similar."""
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        skip_lines = [line for line in captured.out.splitlines() if "[SKIP]" in line]
        assert skip_lines, "Expected at least one [SKIP] line in stdout"
        # The SKIP_NON_AGENT message includes "not a MOSAIC agent file"
        assert "mosaic" in skip_lines[0].lower() or "agent" in skip_lines[0].lower(), (
            f"[SKIP] line for non-agent must mention 'MOSAIC' or 'agent'; got {skip_lines[0]!r}"
        )


# ---------------------------------------------------------------------------
# T5.3f — Skip reason distinguishes utility-agent from non-agent
# ---------------------------------------------------------------------------

class TestSkipReasonTextDistinguishesClasses:
    """The [SKIP] output line must carry a reason that distinguishes utility-agent from non-agent."""

    def test_utility_skip_reason_differs_from_non_agent_skip_reason(
        self, harness_dir, capsys, tmp_path
    ):
        """A utility-agent skip and a non-agent skip must produce different reason text."""
        utility_file = harness_dir / "Agents" / "workflow-creator.md"
        utility_file.write_text(_MINIMAL_SKIP_CONTENT, encoding="utf-8")
        non_agent = harness_dir / "Agents" / "SCL developer.chatmode.md"
        non_agent.write_text(_MINIMAL_SKIP_CONTENT, encoding="utf-8")

        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()

        skip_lines = [line for line in captured.out.splitlines() if "[SKIP]" in line]
        assert len(skip_lines) == 2, (
            f"Expected exactly 2 [SKIP] lines (one per file); got {skip_lines!r}"
        )
        # The two skip lines must not be identical — they must identify different reasons
        assert skip_lines[0] != skip_lines[1], (
            "The two [SKIP] lines must carry different reason text; they were identical: "
            f"{skip_lines[0]!r}"
        )


# ---------------------------------------------------------------------------
# T5.3h — Utility skip distinguishable from already-transformed skip
# ---------------------------------------------------------------------------

class TestUtilitySkipVsAlreadyTransformedSkip:
    """Utility-agent and already-transformed skips must be distinguishable by counter behavior.

    Already-transformed: processed=1, skipped=1 (transform was attempted)
    Utility-agent:       processed=0, skipped=1 (transform was never attempted)
    """

    def test_utility_skip_processed_is_zero(self, utility_agent_file, harness_dir):
        """A utility-agent skip must leave processed at 0."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.processed == 0, (
            f"Utility-agent skip: expected processed=0; got {summary.processed}"
        )

    def test_already_transformed_skip_processed_is_one(
        self, harness_file_already_transformed, harness_dir
    ):
        """An already-transformed skip must increment processed (transform was attempted)."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.processed == 1, (
            f"Already-transformed skip: expected processed=1; got {summary.processed}"
        )

    def test_mixed_utility_and_already_transformed(
        self, utility_agent_file, harness_file_already_transformed, harness_dir
    ):
        """When both a utility-agent and an already-transformed file are present:
        processed=1 (only the already-transformed counts), skipped=2, errors=0.
        """
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.skipped == 2, (
            f"Expected skipped=2 (one utility-agent, one already-transformed); "
            f"got {summary.skipped}"
        )
        assert summary.processed == 1, (
            f"Expected processed=1 (only already-transformed increments processed); "
            f"got {summary.processed}"
        )
        assert summary.errors == 0, (
            f"Expected errors=0; got {summary.errors}"
        )


# ---------------------------------------------------------------------------
# T5.3i — Mixed scenario: utility skip + normal transformable file
# ---------------------------------------------------------------------------

class TestMixedUtilitySkipAndNormalFile:
    """A batch with a utility-agent file and a normal agent file must count each independently."""

    def test_utility_skip_plus_normal_processed_and_skipped(
        self, utility_agent_file, harness_file_no_generic, harness_dir
    ):
        """utility-agent skip + a normal degraded file: processed=1, skipped=1, errors=0."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.processed == 1, (
            f"Expected processed=1 (only the normal file); got {summary.processed}"
        )
        assert summary.skipped == 1, (
            f"Expected skipped=1 (only the utility-agent file); got {summary.skipped}"
        )
        assert summary.errors == 0, (
            f"Expected errors=0; got {summary.errors}"
        )

    def test_non_agent_skip_plus_normal_processed_and_skipped(
        self, non_agent_file, harness_file_no_generic, harness_dir
    ):
        """non-agent skip + a normal degraded file: processed=1, skipped=1, errors=0."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.processed == 1, (
            f"Expected processed=1 (only the normal file); got {summary.processed}"
        )
        assert summary.skipped == 1, (
            f"Expected skipped=1 (only the non-agent file); got {summary.skipped}"
        )
        assert summary.errors == 0, (
            f"Expected errors=0; got {summary.errors}"
        )

    def test_two_utility_agents_both_counted_as_skipped(
        self, utility_agent_file, system_prompt_capturer_file, harness_dir
    ):
        """Two utility-agent files must each increment skipped; processed remains 0."""
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.skipped == 2, (
            f"Expected skipped=2 for two utility-agent files; got {summary.skipped}"
        )
        assert summary.processed == 0, (
            f"Expected processed=0 (no actual transforms); got {summary.processed}"
        )
        assert summary.errors == 0


# ===========================================================================
# Stage 8 — Batch Non-Conformance Report (T8.5)
# ===========================================================================
#
# These tests verify:
#   - BatchSummary gains a non_conformances count field
#   - run_batch accumulates non-conformances from each transform result
#   - run_batch prints render_report output to stdout after the summary line
#   - BatchSummary.ok() remains True even when non-conformances exist (they are not fatal)
#
# All tests fail in RED until:
#   - BatchSummary.non_conformances field is added
#   - detect_output_non_conformances is wired into transform_file
#   - run_batch accumulates the counts and calls render_report
# ===========================================================================

# A minimal harness-only agent with no injection markers in its source.
# After transformation through the degraded path, its output carries no
# [[INJECTION:]] regions, which should be detected as NC_NO_INJECTIONS
# once detect_output_non_conformances is wired into transform_file.
_HARNESS_AGENT_NO_INJECTIONS = textwrap.dedent("""\
    ---
    id: 95
    version: 1.0.0
    transform_version: 1.0.0
    name: no-injections-agent
    description: Harness agent with no injection markers; simulates an already-deployed agent
    ---

    # NoInjectionsAgent Agent

    You are the **NoInjectionsAgent** agent.

    ---

    ## Capabilities

    Core capabilities are deployed inline.

    ---

    ## Constraints

    All constraints are deployed inline.

    ---

    ## Error Handling

    Error handling is deployed inline.

    ---

    ## Output Format

    Return results as structured data.

    ---

    ## Execution Philosophy

    Execute with focus and discipline.
""")


@pytest.fixture()
def harness_file_no_injections(harness_dir: pathlib.Path) -> pathlib.Path:
    """Write a harness agent with no injection markers into harness_dir/Agents/.

    After transformation through the degraded path, its output carries no
    [[INJECTION:]] regions. Used to exercise NC_NO_INJECTIONS detection once
    detect_output_non_conformances is wired into transform_file.
    """
    agent_file = harness_dir / "Agents" / "no-injections-agent.md"
    agent_file.write_text(_HARNESS_AGENT_NO_INJECTIONS, encoding="utf-8")
    return agent_file


class TestBatchNonConformanceReport:
    """run_batch must accumulate non-conformances and print a per-file report after the summary.

    All tests fail in RED until:
    - BatchSummary gains a non_conformances count field
    - detect_output_non_conformances is wired into transform_file
    - run_batch accumulates findings and calls render_report to print them
    """

    def test_batch_summary_has_non_conformances_field(self):
        """BatchSummary must accept a non_conformances keyword argument after Stage 8."""
        s = bt.BatchSummary(processed=1, degraded=0, skipped=0, errors=0, non_conformances=0)
        assert s.non_conformances == 0, (
            f"BatchSummary.non_conformances must equal 0; got {s.non_conformances!r}"
        )

    def test_batch_summary_non_conformances_defaults_to_zero(self):
        """BatchSummary.non_conformances must default to 0 so existing constructions stay valid."""
        s = bt.BatchSummary(processed=0, degraded=0, skipped=0, errors=0)
        assert s.non_conformances == 0, (
            "BatchSummary.non_conformances must default to 0 when not supplied"
        )

    def test_batch_summary_ok_true_when_only_ncs_present(
        self, harness_file_no_generic, harness_dir
    ):
        """BatchSummary.ok() must return True even when non-conformances are reported.

        Non-conformances are reported to humans for manual resolution; they are not
        treated as batch failures. ok() depends only on the errors counter.
        """
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert summary.ok() is True, (
            "ok() must return True when the only issue is non-conformances (not errors)"
        )

    def test_run_batch_prints_non_conformance_report_header_to_stdout(
        self, harness_file_no_generic, harness_dir, capsys
    ):
        """run_batch must print the 'Non-conformance report' header to stdout after the summary.

        Fails in RED: run_batch does not yet call render_report.
        """
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "Non-conformance report" in captured.out, (
            "run_batch must print the non-conformance report header to stdout after the batch "
            f"summary line; got stdout: {captured.out!r}"
        )

    def test_nc_report_appears_after_batch_summary_line(
        self, harness_file_no_generic, harness_dir, capsys
    ):
        """The non-conformance report must appear AFTER the 'Batch TEST:' summary line.

        Fails in RED: run_batch does not yet call render_report.
        """
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        output = captured.out
        summary_pos = output.find("Batch TEST:")
        report_pos = output.find("Non-conformance report")
        assert summary_pos != -1, "Precondition: batch summary line must be present"
        assert report_pos != -1, (
            "Non-conformance report header must be present in stdout; "
            f"got stdout: {output!r}"
        )
        assert report_pos > summary_pos, (
            "Non-conformance report must appear AFTER the batch summary line"
        )

    def test_run_batch_nc_count_in_batch_summary(
        self, harness_file_no_injections, harness_dir
    ):
        """BatchSummary.non_conformances must count NC items detected across all processed files.

        Fails in RED: field does not exist and wiring is not in place.
        """
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert hasattr(summary, "non_conformances"), (
            "BatchSummary must have a non_conformances field after Stage 8 implementation"
        )
        assert isinstance(summary.non_conformances, int), (
            "BatchSummary.non_conformances must be an integer count"
        )
        # The no-injections agent should produce at least one NC finding (NC_NO_INJECTIONS)
        assert summary.non_conformances >= 1, (
            f"Expected at least 1 non-conformance from a no-injection agent; "
            f"got {summary.non_conformances}"
        )

    def test_run_batch_zero_nc_report_when_no_findings(
        self, harness_dir, capsys
    ):
        """When no non-conformances are detected, the report must say '0 non-conformances across 0 files.'

        Fails in RED: run_batch does not yet call render_report.
        The harness_dir fixture provides an empty Agents/ directory with no agent files.
        run_batch processes zero files and calls render_report([]), which must return the
        '0 non-conformances across 0 files.' count line. This guarantees zero NC findings
        without relying on any fixture's post-transform output shape.
        """
        bt.run_batch([harness_dir], {}, "TEST")
        captured = capsys.readouterr()
        assert "0 non-conformances across 0 files." in captured.out, (
            "When no NCs are detected, the report must say '0 non-conformances across 0 files.'; "
            f"got stdout: {captured.out!r}"
        )

    def test_multi_file_batch_nc_count_accumulates(self, harness_dir):
        """BatchSummary.non_conformances must accumulate across all processed files.

        Fails in RED: field does not exist and wiring is not in place.
        Three no-injection agents each produce NC_NO_INJECTIONS; total must be >= 3.
        """
        for i in range(3):
            content = _HARNESS_AGENT_NO_INJECTIONS.replace(
                "id: 95", f"id: {195 + i}"
            ).replace(
                "name: no-injections-agent", f"name: no-injections-agent-{i}"
            )
            (harness_dir / "Agents" / f"no-injections-agent-{i}.md").write_text(
                content, encoding="utf-8"
            )
        summary = bt.run_batch([harness_dir], {}, "TEST")
        assert hasattr(summary, "non_conformances"), (
            "BatchSummary must have a non_conformances field"
        )
        assert summary.non_conformances >= 3, (
            f"Expected at least 3 non-conformances from 3 no-injection files; "
            f"got {summary.non_conformances}"
        )
