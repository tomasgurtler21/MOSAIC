"""Tests for delete_legacy_sections — legacy section deletion before transform.

Covers:
- LegacySectionDeletion and LegacySectionDeletionResult data structure contracts
- LEGACY_SECTION_DELETIONS table: contains the OutputFormat entry
- delete_legacy_sections pure-function behaviour:
    - No-op on empty input
    - No-op when the target section is absent
    - Deletes the H2 heading line and full body content through the next boundary
    - The 'deleted' result field lists the section_id of every deletion that fired
    - An H3 '### Output Format' nested inside another section survives untouched
    - Blank-line hygiene: no run of two consecutive blank lines is created
    - Multiplicity: only the first matching heading is deleted
    - Idempotence: re-running on the output produces deleted == []
- Integration: transform_file produces no OutputFormat section content on the
    generic transform path (T1.2) and on the harness transform path (T1.3)

These tests are in TDD RED phase and will fail until I1.1–I1.3 are implemented:
delete_legacy_sections does not yet exist in region_insertion.py, and
transform_file still emits OutputFormat section content.
"""
from __future__ import annotations

import pathlib
import sys
import textwrap
import tempfile

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

# ---------------------------------------------------------------------------
# Import checks — fail in RED because these names do not exist yet
# ---------------------------------------------------------------------------


def test_legacy_section_deletion_importable() -> None:
    """LegacySectionDeletion must be importable from region_insertion."""
    from region_insertion import LegacySectionDeletion  # noqa: F401


def test_legacy_section_deletion_result_importable() -> None:
    """LegacySectionDeletionResult must be importable from region_insertion."""
    from region_insertion import LegacySectionDeletionResult  # noqa: F401


def test_delete_legacy_sections_importable() -> None:
    """delete_legacy_sections must be importable from region_insertion."""
    from region_insertion import delete_legacy_sections  # noqa: F401


def test_legacy_section_deletions_importable() -> None:
    """LEGACY_SECTION_DELETIONS must be importable from region_insertion."""
    from region_insertion import LEGACY_SECTION_DELETIONS  # noqa: F401


# ---------------------------------------------------------------------------
# LEGACY_SECTION_DELETIONS table contract
# ---------------------------------------------------------------------------


class TestLegacySectionDeletionsTable:
    """LEGACY_SECTION_DELETIONS must declare the OutputFormat retirement entry."""

    def test_table_is_nonempty(self) -> None:
        from region_insertion import LEGACY_SECTION_DELETIONS
        assert len(LEGACY_SECTION_DELETIONS) >= 1, (
            "LEGACY_SECTION_DELETIONS must have at least one entry (OutputFormat)"
        )

    def test_output_format_entry_present(self) -> None:
        from region_insertion import LEGACY_SECTION_DELETIONS
        ids = [entry.section_id for entry in LEGACY_SECTION_DELETIONS]
        assert "OutputFormat" in ids, (
            "LEGACY_SECTION_DELETIONS must contain an entry with section_id='OutputFormat'"
        )

    def test_output_format_entry_heading(self) -> None:
        from region_insertion import LEGACY_SECTION_DELETIONS
        entry = next(e for e in LEGACY_SECTION_DELETIONS if e.section_id == "OutputFormat")
        assert entry.heading == "## Output Format", (
            "The OutputFormat deletion entry must carry heading='## Output Format'"
        )

    def test_output_format_entry_level(self) -> None:
        from region_insertion import LEGACY_SECTION_DELETIONS
        entry = next(e for e in LEGACY_SECTION_DELETIONS if e.section_id == "OutputFormat")
        assert entry.level == 2, (
            "The OutputFormat deletion entry must carry level=2; "
            "this is what keeps a nested '### Output Format' alive"
        )


# ---------------------------------------------------------------------------
# LegacySectionDeletion data structure
# ---------------------------------------------------------------------------


class TestLegacySectionDeletion:
    """LegacySectionDeletion must be a frozen dataclass with the declared fields."""

    def test_construction_with_keyword_args(self) -> None:
        from region_insertion import LegacySectionDeletion
        entry = LegacySectionDeletion(
            section_id="OutputFormat",
            heading="## Output Format",
            level=2,
        )
        assert entry.section_id == "OutputFormat"
        assert entry.heading == "## Output Format"
        assert entry.level == 2

    def test_is_frozen(self) -> None:
        from region_insertion import LegacySectionDeletion
        import dataclasses
        entry = LegacySectionDeletion(section_id="OutputFormat", heading="## Output Format", level=2)
        with pytest.raises((AttributeError, TypeError)):
            entry.section_id = "something_else"  # type: ignore[misc]


# ---------------------------------------------------------------------------
# delete_legacy_sections: no-op cases
# ---------------------------------------------------------------------------


class TestDeleteLegacySectionsNoOp:
    """delete_legacy_sections must be a pure no-op when no entry matches."""

    def _call(self, lines):
        from region_insertion import delete_legacy_sections
        return delete_legacy_sections(lines)

    def test_empty_input_returns_empty_lines(self) -> None:
        result = self._call([])
        assert result.lines == []
        assert result.deleted == []

    def test_no_match_returns_input_unchanged(self) -> None:
        lines = [
            "# MyAgent Agent\n",
            "\n",
            "## Capabilities\n",
            "\n",
            "Some capability prose.\n",
        ]
        result = self._call(lines)
        assert result.lines == lines, (
            "When no entry matches, lines must be element-wise equal to the input"
        )
        assert result.deleted == []

    def test_h3_heading_alone_is_not_deleted(self) -> None:
        """An H3 '### Output Format' must NOT be deleted — only H2 '## Output Format' is retired."""
        lines = [
            "# MyAgent Agent\n",
            "\n",
            "## Capabilities\n",
            "\n",
            "### Output Format\n",
            "\n",
            "Describe output format here.\n",
        ]
        result = self._call(lines)
        assert result.deleted == [], (
            "An H3 '### Output Format' must NOT be deleted; "
            "the match requires level==2 (H2) exactly"
        )
        assert result.lines == lines

    def test_output_format_in_fenced_block_is_not_deleted(self) -> None:
        """A '## Output Format' line inside a fenced code block must not be deleted."""
        lines = [
            "# MyAgent Agent\n",
            "\n",
            "## Capabilities\n",
            "\n",
            "```\n",
            "## Output Format\n",
            "```\n",
            "\n",
            "## Execution Philosophy\n",
            "\n",
            "Some philosophy.\n",
        ]
        result = self._call(lines)
        assert result.deleted == [], (
            "A '## Output Format' line inside a fenced block must not be deleted"
        )
        assert result.lines == lines


# ---------------------------------------------------------------------------
# delete_legacy_sections: deletion behaviour
# ---------------------------------------------------------------------------


class TestDeleteLegacySectionsDeletion:
    """delete_legacy_sections must delete the H2 heading and its full body."""

    def _call(self, lines):
        from region_insertion import delete_legacy_sections
        return delete_legacy_sections(lines)

    def test_deleted_field_contains_output_format(self) -> None:
        """When the H2 section is found, 'deleted' must contain 'OutputFormat'."""
        lines = [
            "## Output Format\n",
            "\n",
            "Return a JSON status block.\n",
        ]
        result = self._call(lines)
        assert "OutputFormat" in result.deleted, (
            "delete_legacy_sections must list 'OutputFormat' in deleted when the section matches"
        )

    def test_heading_line_absent_from_output(self) -> None:
        """The '## Output Format' heading line must not appear in the output."""
        lines = [
            "## Error Handling\n",
            "\n",
            "Some error guidance.\n",
            "\n",
            "---\n",
            "\n",
            "## Output Format\n",
            "\n",
            "Return a JSON status block.\n",
            "\n",
            "---\n",
            "\n",
            "## Execution Philosophy\n",
            "\n",
            "Philosophy prose.\n",
        ]
        result = self._call(lines)
        assert "## Output Format\n" not in result.lines, (
            "The '## Output Format' heading line must not appear in the output"
        )

    def test_body_content_absent_from_output(self) -> None:
        """The body content of the deleted section must not appear in the output."""
        lines = [
            "## Error Handling\n",
            "\n",
            "Some error guidance.\n",
            "\n",
            "---\n",
            "\n",
            "## Output Format\n",
            "\n",
            "Return a JSON status block.\n",
            "\n",
            "---\n",
            "\n",
            "## Execution Philosophy\n",
            "\n",
            "Philosophy prose.\n",
        ]
        result = self._call(lines)
        assert "Return a JSON status block.\n" not in result.lines, (
            "Body content of the deleted section must not appear in the output"
        )

    def test_following_section_survives(self) -> None:
        """The '## Execution Philosophy' heading that follows must survive."""
        lines = [
            "## Output Format\n",
            "\n",
            "Return a JSON status block.\n",
            "\n",
            "---\n",
            "\n",
            "## Execution Philosophy\n",
            "\n",
            "Philosophy prose.\n",
        ]
        result = self._call(lines)
        assert "## Execution Philosophy\n" in result.lines, (
            "The heading that terminates the deleted extent must survive in the output"
        )

    def test_preceding_content_survives(self) -> None:
        """Content before the deleted section must survive unchanged."""
        lines = [
            "## Error Handling\n",
            "\n",
            "Error guidance prose.\n",
            "\n",
            "---\n",
            "\n",
            "## Output Format\n",
            "\n",
            "Return a JSON status block.\n",
        ]
        result = self._call(lines)
        assert "Error guidance prose.\n" in result.lines, (
            "Content before the deleted section must survive in the output"
        )
        assert "## Error Handling\n" in result.lines

    def test_input_not_mutated(self) -> None:
        """delete_legacy_sections must not mutate the input list."""
        lines = [
            "## Output Format\n",
            "\n",
            "Body content.\n",
        ]
        original = list(lines)
        self._call(lines)
        assert lines == original, (
            "delete_legacy_sections must not mutate the input list"
        )

    def test_no_section_tag_in_output(self) -> None:
        """No '<OutputFormat' tag and no '## Output Format' heading must appear in the output."""
        lines = [
            "## Output Format\n",
            "\n",
            "Body content.\n",
            "\n",
            "## Execution Philosophy\n",
        ]
        result = self._call(lines)
        for line in result.lines:
            assert "<OutputFormat" not in line, (
                "No '<OutputFormat' XML tag should appear in output; "
                f"found: {line!r}"
            )
        assert "## Output Format\n" not in result.lines, (
            "No '## Output Format' heading must appear in output after deletion"
        )

    def test_blank_line_hygiene_no_double_blank(self) -> None:
        """No consecutive blank lines must appear in the output after deletion."""
        lines = [
            "## Error Handling\n",
            "\n",
            "Error prose.\n",
            "\n",
            "## Output Format\n",
            "\n",
            "Return JSON.\n",
            "\n",
            "## Execution Philosophy\n",
        ]
        result = self._call(lines)
        for i in range(len(result.lines) - 1):
            assert not (result.lines[i] == "\n" and result.lines[i + 1] == "\n"), (
                f"Consecutive blank lines found at positions {i} and {i + 1}; "
                "blank-line hygiene must collapse them to one"
            )

    def test_mosaic_boundary_open_tag_stops_deletion(self) -> None:
        """A MOSAIC boundary open tag immediately after the deleted body must not be removed.

        The deletion stop condition must recognise any MOSAIC region open tag line
        (matching the pattern '<Word type="...">') and halt deletion there.
        The tag must survive in result.lines; it is the safety guarantee that
        deletion cannot swallow the following MOSAIC region.
        """
        lines = [
            "## Output Format\n",
            "\n",
            "Body content.\n",
            '<Capabilities type="core">\n',
            "\n",
            "## Capabilities\n",
            "\n",
            "Agent capabilities.\n",
            '</Capabilities>\n',
        ]
        result = self._call(lines)
        assert '<Capabilities type="core">\n' in result.lines, (
            "The MOSAIC boundary open tag immediately after the deleted body must survive; "
            "deletion must halt at any region open tag line"
        )
        assert "## Output Format\n" not in result.lines, (
            "The '## Output Format' heading must still be deleted"
        )
        assert "Body content.\n" not in result.lines, (
            "The body line before the boundary tag must still be deleted"
        )

    def test_legacy_marker_line_stops_deletion(self) -> None:
        """A legacy-marker line immediately after the deleted body must not be removed.

        The deletion stop condition must recognise a legacy section separator line
        ('---') and halt deletion there. The separator must survive in result.lines.
        """
        lines = [
            "## Output Format\n",
            "\n",
            "Body content.\n",
            "---\n",
            "\n",
            "## Execution Philosophy\n",
            "\n",
            "Philosophy prose.\n",
        ]
        result = self._call(lines)
        assert "---\n" in result.lines, (
            "The '---' separator line immediately after the deleted body must survive; "
            "deletion must halt at legacy-marker lines"
        )
        assert "## Output Format\n" not in result.lines, (
            "The '## Output Format' heading must still be deleted"
        )
        assert "Body content.\n" not in result.lines, (
            "The body line before the separator must still be deleted"
        )


# ---------------------------------------------------------------------------
# T1.4 — H3 nested inside Communication Protocol block survives
# ---------------------------------------------------------------------------


class TestH3OutputFormatSurvives:
    """An H3 '### Output Format' inside another section must survive while the H2 is deleted.

    This is the critical level-equality guard: the match requires
    line.strip() == '## Output Format' AND heading level == 2 exactly.
    An H3 '### Output Format' does not meet the level condition and must be left in place.
    """

    def _call(self, lines):
        from region_insertion import delete_legacy_sections
        return delete_legacy_sections(lines)

    def test_h3_inside_comms_protocol_block_survives(self) -> None:
        """An H3 '### Output Format' nested inside the Communication Protocol block must survive."""
        lines = [
            "# MyAgent Agent\n",
            "\n",
            "Some identity prose.\n",
            "\n",
            "### Output Format\n",
            "\n",
            "Your entire response is the JSON object defined below.\n",
            "\n",
            "---\n",
            "\n",
            "## Capabilities\n",
            "\n",
            "Agent capabilities.\n",
            "\n",
            "## Output Format\n",
            "\n",
            "Always end with a JSON status block.\n",
        ]
        result = self._call(lines)
        # H3 must survive
        assert "### Output Format\n" in result.lines, (
            "The H3 '### Output Format' nested inside the Communication Protocol block "
            "must survive — only the H2 '## Output Format' is deleted"
        )
        # H3 body must survive
        assert "Your entire response is the JSON object defined below.\n" in result.lines
        # H2 must be deleted
        assert "## Output Format\n" not in result.lines, (
            "The H2 '## Output Format' must be deleted"
        )
        # 'OutputFormat' reported in deleted
        assert "OutputFormat" in result.deleted

    def test_only_h2_is_in_deleted_list(self) -> None:
        """When both H3 and H2 exist, deleted must list 'OutputFormat' exactly once."""
        lines = [
            "### Output Format\n",
            "\n",
            "JSON format description.\n",
            "\n",
            "## Output Format\n",
            "\n",
            "Alternate description.\n",
        ]
        result = self._call(lines)
        assert result.deleted.count("OutputFormat") == 1, (
            "deleted must list 'OutputFormat' exactly once even when both H3 and H2 exist"
        )

    def test_multiplicity_only_first_h2_deleted(self) -> None:
        """Only the first '## Output Format' H2 is deleted; a second one is left in place."""
        lines = [
            "## Output Format\n",
            "\n",
            "First output format body.\n",
            "\n",
            "## Output Format\n",
            "\n",
            "Second output format body.\n",
        ]
        result = self._call(lines)
        # At most one is deleted
        assert result.deleted.count("OutputFormat") <= 1, (
            "deleted must list 'OutputFormat' at most once — only first match is deleted"
        )
        # Second occurrence survives
        assert "Second output format body.\n" in result.lines, (
            "The second '## Output Format' section (if any) must survive — "
            "only the first match is deleted per the multiplicity rule"
        )


# ---------------------------------------------------------------------------
# Idempotence
# ---------------------------------------------------------------------------


class TestDeleteLegacySectionsIdempotence:
    """Applying delete_legacy_sections twice must yield deleted == [] on the second run."""

    def _call(self, lines):
        from region_insertion import delete_legacy_sections
        return delete_legacy_sections(lines)

    def test_second_application_has_empty_deleted(self) -> None:
        lines = [
            "## Error Handling\n",
            "\n",
            "Error prose.\n",
            "\n",
            "## Output Format\n",
            "\n",
            "Return JSON.\n",
            "\n",
            "## Execution Philosophy\n",
            "\n",
            "Philosophy prose.\n",
        ]
        first = self._call(lines)
        second = self._call(first.lines)
        assert second.deleted == [], (
            "delete_legacy_sections is idempotent: second application must yield deleted == []"
        )

    def test_second_application_lines_unchanged(self) -> None:
        lines = [
            "## Output Format\n",
            "\n",
            "Body.\n",
            "\n",
            "## Execution Philosophy\n",
        ]
        first = self._call(lines)
        second = self._call(first.lines)
        assert second.lines == first.lines, (
            "After the first application, a second run must return element-wise equal lines"
        )


# ---------------------------------------------------------------------------
# T1.2 — Integration: generic transform path produces no OutputFormat content
# ---------------------------------------------------------------------------


class TestGenericPathNoOutputFormat:
    """transform_file on the generic path must produce output with no OutputFormat content.

    These tests fail in TDD RED phase because transform_file still emits the
    OutputFormat section (I1.2 has not yet been implemented).
    """

    _FIXTURES_DIR = pathlib.Path(__file__).parent / "fixtures"

    def test_generic_standard_output_has_no_output_format_heading(self, tmp_path) -> None:
        """Transforming generic_standard_input must produce no '## Output Format' heading."""
        from boundary_transformer import transform_file
        input_path = self._FIXTURES_DIR / "generic_standard_input.md"
        output_path = tmp_path / "out.md"
        transform_file(input_path, output_path)
        content = output_path.read_text(encoding="utf-8")
        assert "## Output Format" not in content, (
            "The generic transform output must not contain '## Output Format'; "
            "delete_legacy_sections must remove it before the body-merge path runs"
        )

    def test_generic_standard_output_has_no_output_format_tag(self, tmp_path) -> None:
        """Transforming generic_standard_input must produce no '<OutputFormat' tag."""
        from boundary_transformer import transform_file
        input_path = self._FIXTURES_DIR / "generic_standard_input.md"
        output_path = tmp_path / "out.md"
        transform_file(input_path, output_path)
        content = output_path.read_text(encoding="utf-8")
        assert "<OutputFormat" not in content, (
            "The generic transform output must not contain an '<OutputFormat' section tag"
        )

    def test_generic_standard_output_has_no_output_format_body_content(self, tmp_path) -> None:
        """Transforming generic_standard_input must produce no OutputFormat body prose."""
        from boundary_transformer import transform_file
        input_path = self._FIXTURES_DIR / "generic_standard_input.md"
        output_path = tmp_path / "out.md"
        transform_file(input_path, output_path)
        content = output_path.read_text(encoding="utf-8")
        assert "Always end with a JSON status block." not in content, (
            "The OutputFormat body prose must not appear in the generic transform output; "
            "delete_legacy_sections must have removed the entire section including body"
        )

    def test_generic_standard_output_format_not_in_sections_added(self, tmp_path) -> None:
        """TransformResult.sections_added must not contain 'OutputFormat'."""
        from boundary_transformer import transform_file
        input_path = self._FIXTURES_DIR / "generic_standard_input.md"
        output_path = tmp_path / "out.md"
        result = transform_file(input_path, output_path)
        assert "OutputFormat" not in result.sections_added, (
            "sections_added must not contain 'OutputFormat' after the section is retired"
        )

    def test_generic_transform_sections_added_has_five_sections(self, tmp_path) -> None:
        """TransformResult.sections_added must have exactly 5 entries (no OutputFormat)."""
        from boundary_transformer import transform_file
        input_path = self._FIXTURES_DIR / "generic_standard_input.md"
        output_path = tmp_path / "out.md"
        result = transform_file(input_path, output_path)
        assert len(result.sections_added) == 5, (
            f"Expected 5 sections in sections_added (OutputFormat retired), "
            f"got {len(result.sections_added)}: {result.sections_added}"
        )


# ---------------------------------------------------------------------------
# T1.3 — Integration: harness transform path produces no OutputFormat content
# ---------------------------------------------------------------------------


class TestHarnessPathNoOutputFormat:
    """transform_file on the harness path must produce output with no OutputFormat content.

    These tests fail in TDD RED phase because transform_file still emits the
    OutputFormat section on the harness path (I1.3 has not yet been implemented).
    """

    _FIXTURES_DIR = pathlib.Path(__file__).parent / "fixtures"

    def test_harness_codebase_agnostic_output_has_no_output_format_heading(
        self, tmp_path
    ) -> None:
        """Transforming harness_codebase_agnostic_input must produce no '## Output Format' heading."""
        from boundary_transformer import transform_file
        input_path = self._FIXTURES_DIR / "harness_codebase_agnostic_input.md"
        output_path = tmp_path / "out.md"
        transform_file(input_path, output_path)
        content = output_path.read_text(encoding="utf-8")
        assert "## Output Format" not in content, (
            "The harness transform output must not contain '## Output Format'; "
            "delete_legacy_sections must remove it before either body-merge path runs"
        )

    def test_harness_codebase_agnostic_output_has_no_output_format_tag(
        self, tmp_path
    ) -> None:
        """Transforming harness_codebase_agnostic_input must produce no '<OutputFormat' tag."""
        from boundary_transformer import transform_file
        input_path = self._FIXTURES_DIR / "harness_codebase_agnostic_input.md"
        output_path = tmp_path / "out.md"
        transform_file(input_path, output_path)
        content = output_path.read_text(encoding="utf-8")
        assert "<OutputFormat" not in content, (
            "The harness transform output must not contain an '<OutputFormat' section tag"
        )

    def test_harness_codebase_agnostic_output_has_no_output_format_body(
        self, tmp_path
    ) -> None:
        """Transforming harness_codebase_agnostic_input must produce no OutputFormat body prose."""
        from boundary_transformer import transform_file
        input_path = self._FIXTURES_DIR / "harness_codebase_agnostic_input.md"
        output_path = tmp_path / "out.md"
        transform_file(input_path, output_path)
        content = output_path.read_text(encoding="utf-8")
        assert "Always end with a JSON status block." not in content, (
            "The OutputFormat body prose must not appear in the harness transform output; "
            "delete_legacy_sections must remove the section on the harness path too"
        )

    def test_harness_codebase_agnostic_output_format_not_in_sections_added(
        self, tmp_path
    ) -> None:
        """TransformResult.sections_added on the harness path must not contain 'OutputFormat'."""
        from boundary_transformer import transform_file
        input_path = self._FIXTURES_DIR / "harness_codebase_agnostic_input.md"
        output_path = tmp_path / "out.md"
        result = transform_file(input_path, output_path)
        assert "OutputFormat" not in result.sections_added, (
            "sections_added must not contain 'OutputFormat' on the harness path"
        )

    def test_generic_and_harness_paths_agree_on_output_format_absence(
        self, tmp_path
    ) -> None:
        """Both transform paths must produce output with no OutputFormat content.

        Path agreement is a contract: delete_legacy_sections is called before path dispatch,
        so both paths see the already-cleaned body and neither can re-introduce the section.
        """
        from boundary_transformer import transform_file
        generic_input = self._FIXTURES_DIR / "generic_standard_input.md"
        harness_input = self._FIXTURES_DIR / "harness_codebase_agnostic_input.md"
        generic_out = tmp_path / "generic_out.md"
        harness_out = tmp_path / "harness_out.md"
        transform_file(generic_input, generic_out)
        transform_file(harness_input, harness_out)
        generic_content = generic_out.read_text(encoding="utf-8")
        harness_content = harness_out.read_text(encoding="utf-8")
        assert "## Output Format" not in generic_content, (
            "Generic path output must not contain '## Output Format'"
        )
        assert "## Output Format" not in harness_content, (
            "Harness path output must not contain '## Output Format'"
        )


# ---------------------------------------------------------------------------
# T1.4 integration — H3 survival through the full transform pipeline
# ---------------------------------------------------------------------------


class TestH3SurvivesIntegration:
    """transform_file must keep an H3 '### Output Format' while deleting the H2.

    This integration test uses a temporary fixture file (written inline with
    tmp_path) to confirm that the regex-level H3 protection survives the full
    transform pipeline end-to-end, not just in the unit-level line-list tests.

    The test fails in TDD RED phase because delete_legacy_sections is not yet
    wired into transform_file (I1.2 has not been implemented).
    """

    def test_h3_survives_and_h2_absent_in_transform_output(self, tmp_path) -> None:
        """A document with both '### Output Format' (H3) and '## Output Format' (H2)
        must produce output where the H3 and its prose survive and the H2 is absent.
        """
        from boundary_transformer import transform_file

        # Minimal valid agent document containing both H3 and H2 'Output Format'
        source = textwrap.dedent("""\
            ---
            id: 42
            version: 1.0.0
            name: h3-survival-agent
            description: Agent used to verify H3 Output Format survives transform
            ---

            # H3SurvivalAgent Agent

            You are the **H3SurvivalAgent** agent.

            [INJECTION: identity_extension]

            The Communication Protocol requires your response to be a JSON object:

            ### Output Format

            Your entire response is the JSON object defined below — no prose before it.

            ---

            ## Capabilities

            Core capabilities here.

            ---

            ## Constraints

            - Stay within scope

            ---

            ## Error Handling

            - Handle errors gracefully

            ---

            ## Output Format

            Always end with a JSON status block.

            ---

            ## Execution Philosophy

            Execute with focus.
        """)
        input_path = tmp_path / "h3-survival-agent.md"
        input_path.write_text(source, encoding="utf-8")
        output_path = tmp_path / "h3-survival-agent-out.md"
        transform_file(input_path, output_path)
        content = output_path.read_text(encoding="utf-8")

        assert "### Output Format" in content, (
            "The H3 '### Output Format' heading must survive the full transform pipeline"
        )
        assert "Your entire response is the JSON object defined below" in content, (
            "The H3 body prose must survive in the transform output"
        )
        assert not any(line == "## Output Format" for line in content.splitlines()), (
            "The H2 '## Output Format' heading must be absent from the transform output; "
            "delete_legacy_sections must have removed it before the body-merge path ran"
        )
