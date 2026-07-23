"""Tests for the boundary transformation tool.

Tests verify overall transformation behavior: given an input file, the output
must contain the correct boundaries in the correct order with body text preserved.
Tests do NOT test individual methods — they test end-to-end transformation outcomes.

All tests are in TDD RED phase: they will fail until boundary_transformer.py is implemented.
"""
from __future__ import annotations

import pathlib
import re
import subprocess
import sys

import pytest

# Resolve the Tools directory so we can import from it regardless of where
# pytest is invoked from.
_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

from boundary_transformer import TransformError, TransformResult, transform_file  # noqa: E402

# Matches old-format injection marker lines in their two allowed forms:
#   standalone:  [INJECTION: name]
#   list-item:   - [INJECTION: name]
# Used to exclude transformation-target lines from verbatim-preservation comparisons.
_OLD_INJECTION_LINE_RE = re.compile(r"^\s*(?:- )?\[INJECTION: \w+\]$")


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _read(path: pathlib.Path) -> str:
    return path.read_text(encoding="utf-8")


def _transform_to_tmp(
    input_path: pathlib.Path,
    tmp_path: pathlib.Path,
    generic_ref_path: pathlib.Path | None = None,
) -> tuple[TransformResult, pathlib.Path]:
    """Run transform_file writing output to a temporary file; return (result, output_path)."""
    output_path = tmp_path / input_path.name
    result = transform_file(input_path, output_path, generic_ref_path)
    return result, output_path


# ---------------------------------------------------------------------------
# Generic standard agent — all 7 sections, 9 common injections
# ---------------------------------------------------------------------------

class TestGenericStandard:
    """A standard generic agent file with all common injections, version 2.2.0."""

    def test_transformation_succeeds(
        self, generic_standard_input, tmp_path
    ):
        """Transformation of a well-formed generic agent file must succeed."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_all_seven_sections_added(
        self, generic_standard_input, tmp_path
    ):
        """All 7 canonical section boundaries must be added to a standard generic file."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        expected_sections = [
            "Identity",
            "CommunicationProtocol",
            "Capabilities",
            "Constraints",
            "ErrorHandling",
            "OutputFormat",
            "ExecutionPhilosophy",
        ]
        assert result.sections_added == expected_sections

    def test_common_injections_added(
        self, generic_standard_input, tmp_path
    ):
        """All 9 common injection boundaries must be added."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        expected_injections = [
            "IdentityExtension",
            "ProtocolExtension",
            "LanguagePatterns",
            "CodebaseContext",
            "OutputArtifactTemplate",
            "HarnessConstraints",
            "CustomConstraints",
            "ErrorHandlingExtension",
            "ContextLimits",
        ]
        assert result.injections_added == expected_injections

    def test_version_bumped_to_next_major(
        self, generic_standard_input, tmp_path
    ):
        """Version 2.2.0 must be bumped to 3.0.0 (next major, reset minor and patch)."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert result.version_before == "2.2.0"
        assert result.version_after == "3.0.0"

    def test_output_matches_expected_exactly(
        self, generic_standard_input, generic_standard_expected, tmp_path
    ):
        """The full output file must match the expected fixture byte-for-byte."""
        _, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        assert _read(output_path) == _read(generic_standard_expected)

    def test_frontmatter_not_boundaried(
        self, generic_standard_input, tmp_path
    ):
        """YAML frontmatter must remain outside all section and injection boundaries."""
        _, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        content = _read(output_path)
        lines = content.splitlines()
        # Frontmatter is between the first and second '---' delimiters.
        # No [[SECTION or [[INJECTION tag should appear before the closing '---'.
        in_frontmatter = False
        for line in lines:
            if line == "---":
                if not in_frontmatter:
                    in_frontmatter = True
                    continue
                else:
                    break  # end of frontmatter
            if in_frontmatter:
                assert not line.startswith("[["), (
                    f"Boundary tag found inside frontmatter: {line!r}"
                )

    def test_body_text_preserved_verbatim(
        self, generic_standard_input, tmp_path
    ):
        """Instruction body text must survive transformation character-for-character.

        Old-format [INJECTION: name] markers are transformation targets and must be
        replaced, not left as-is.  New-format boundary tags are additive markup.
        Everything else must appear in the output unchanged and in the same order.
        """
        _, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        original = _read(generic_standard_input)
        transformed = _read(output_path)
        # Guard: old-format markers must be gone — if any remain the transformer
        # failed to replace them (false-negative protection).
        assert "[INJECTION: " not in transformed, (
            "Old-format [INJECTION: ...] markers must be replaced by [[INJECTION:...]] "
            "boundary tags; at least one marker was left unconverted in the output."
        )
        # Verbatim check: non-target body text must be identical in both files.
        original_body = _extract_body_lines(original)
        transformed_body = _extract_body_lines_excluding_tags(transformed)
        assert original_body == transformed_body

    def test_section_open_tags_precede_headings(
        self, generic_standard_input, tmp_path
    ):
        """Each [[SECTION:X]] open tag must appear on the line immediately before its heading."""
        _, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        lines = _read(output_path).splitlines()
        heading_to_section = {
            "# ": "Identity",
            "## Communication Protocol": "CommunicationProtocol",
            "## Capabilities": "Capabilities",
            "## Constraints": "Constraints",
            "## Error Handling": "ErrorHandling",
            "## Output Format": "OutputFormat",
            "## Execution Philosophy": "ExecutionPhilosophy",
        }
        for i, line in enumerate(lines):
            for prefix, section_name in heading_to_section.items():
                if line.startswith(prefix) and not line.startswith("##"):
                    # H1 heading — check previous line is [[SECTION:Identity]]
                    assert i > 0 and lines[i - 1] == f"[[SECTION:{section_name}]]", (
                        f"Expected [[SECTION:{section_name}]] before heading '{line}'"
                    )
                elif line == f"## {_heading_text(section_name)}":
                    assert i > 0 and lines[i - 1] == f"[[SECTION:{section_name}]]", (
                        f"Expected [[SECTION:{section_name}]] before heading '{line}'"
                    )

    def test_section_close_tags_precede_separators(
        self, generic_standard_input, tmp_path
    ):
        """Each [[/SECTION:X]] close tag must appear immediately before the '---' that ends it."""
        _, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        lines = _read(output_path).splitlines()
        for i, line in enumerate(lines):
            if line.startswith("[[/SECTION:"):
                # The very next line must be '---' (section separator)
                # OR this is the last section (EOF)
                if i + 1 < len(lines):
                    assert lines[i + 1] == "---", (
                        f"Expected '---' after '{line}' at line {i + 2}, got {lines[i + 1]!r}"
                    )

    def test_last_section_close_tag_at_eof(
        self, generic_standard_input, tmp_path
    ):
        """The [[/SECTION:ExecutionPhilosophy]] close tag must be the last line of output."""
        _, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        last_line = _read(output_path).rstrip("\n").splitlines()[-1]
        assert last_line == "[[/SECTION:ExecutionPhilosophy]]"

    def test_context_limits_standalone_format(
        self, generic_standard_input, tmp_path
    ):
        """A standalone [INJECTION: context_limits] must become [[INJECTION:ContextLimits]] on its own line."""
        _, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        lines = _read(output_path).splitlines()
        # Find the ContextLimits open tag; it must NOT be prefixed with '- '
        cl_lines = [l for l in lines if "[[INJECTION:ContextLimits]]" in l]
        assert len(cl_lines) == 1, "Expected exactly one [[INJECTION:ContextLimits]] open tag"
        assert cl_lines[0] == "[[INJECTION:ContextLimits]]", (
            f"ContextLimits open tag has unexpected prefix: {cl_lines[0]!r}"
        )


# ---------------------------------------------------------------------------
# Validation agent — adds severity_thresholds, severity_definitions, list-item context_limits
# ---------------------------------------------------------------------------

class TestGenericValidation:
    """A validation agent with severity injections and list-item context_limits."""

    def test_transformation_succeeds(self, generic_validation_input, tmp_path):
        result, _ = _transform_to_tmp(generic_validation_input, tmp_path)
        assert result.success is True

    def test_severity_injections_added(self, generic_validation_input, tmp_path):
        """SeverityThresholds and SeverityDefinitions must be added for validation agents."""
        result, _ = _transform_to_tmp(generic_validation_input, tmp_path)
        assert "SeverityThresholds" in result.injections_added
        assert "SeverityDefinitions" in result.injections_added

    def test_version_bumped_2_3_0(self, generic_validation_input, tmp_path):
        """Version 2.3.0 must be bumped to 3.0.0."""
        result, _ = _transform_to_tmp(generic_validation_input, tmp_path)
        assert result.version_before == "2.3.0"
        assert result.version_after == "3.0.0"

    def test_output_matches_expected_exactly(
        self, generic_validation_input, generic_validation_expected, tmp_path
    ):
        _, output_path = _transform_to_tmp(generic_validation_input, tmp_path)
        assert _read(output_path) == _read(generic_validation_expected)

    def test_context_limits_list_item_format(
        self, generic_validation_input, tmp_path
    ):
        """A list-item [INJECTION: context_limits] must preserve the '- ' prefix on the open tag."""
        _, output_path = _transform_to_tmp(generic_validation_input, tmp_path)
        lines = _read(output_path).splitlines()
        cl_lines = [l for l in lines if "[[INJECTION:ContextLimits]]" in l]
        assert len(cl_lines) == 1
        assert cl_lines[0] == "- [[INJECTION:ContextLimits]]", (
            f"List-item ContextLimits must keep '- ' prefix, got: {cl_lines[0]!r}"
        )

    def test_close_tag_after_list_item_injection(
        self, generic_validation_input, tmp_path
    ):
        """The [[/INJECTION:ContextLimits]] close tag must follow on the very next line."""
        _, output_path = _transform_to_tmp(generic_validation_input, tmp_path)
        lines = _read(output_path).splitlines()
        for i, line in enumerate(lines):
            if line == "- [[INJECTION:ContextLimits]]":
                assert i + 1 < len(lines) and lines[i + 1] == "[[/INJECTION:ContextLimits]]", (
                    "Close tag must immediately follow open tag for empty list-item injection"
                )
                break
        else:
            pytest.fail("Did not find '- [[INJECTION:ContextLimits]]' in output")


# ---------------------------------------------------------------------------
# Orchestrator — available_workflows, unique non-canonical subsections
# ---------------------------------------------------------------------------

class TestGenericOrchestrator:
    """Orchestrator file: available_workflows injection, non-canonical subsections in ErrorHandling."""

    def test_transformation_succeeds(self, generic_orchestrator_input, tmp_path):
        result, _ = _transform_to_tmp(generic_orchestrator_input, tmp_path)
        assert result.success is True

    def test_six_sections_added(self, generic_orchestrator_input, tmp_path):
        """Orchestrator has no OutputFormat section — only 6 section boundaries."""
        result, _ = _transform_to_tmp(generic_orchestrator_input, tmp_path)
        assert "OutputFormat" not in result.sections_added
        assert len(result.sections_added) == 6

    def test_available_workflows_injection_added(
        self, generic_orchestrator_input, tmp_path
    ):
        result, _ = _transform_to_tmp(generic_orchestrator_input, tmp_path)
        assert "AvailableWorkflows" in result.injections_added

    def test_unique_subsections_not_given_own_boundaries(
        self, generic_orchestrator_input, tmp_path
    ):
        """Core Orchestration Loop, Agent Callbacks, State Recovery must NOT get section boundaries."""
        _, output_path = _transform_to_tmp(generic_orchestrator_input, tmp_path)
        content = _read(output_path)
        for name in ("CoreOrchestrationLoop", "AgentCallbacks", "StateRecovery"):
            assert f"[[SECTION:{name}]]" not in content, (
                f"Non-canonical subsection got an unexpected boundary: [[SECTION:{name}]]"
            )
        for heading in (
            "## Core Orchestration Loop",
            "## Agent Callbacks vs Rollbacks",
            "## State Recovery",
        ):
            assert heading in content, (
                f"Non-canonical heading '{heading}' must be preserved in output"
            )

    def test_unique_subsections_inside_error_handling_boundary(
        self, generic_orchestrator_input, tmp_path
    ):
        """Non-canonical subsections must appear between [[SECTION:ErrorHandling]] and [[/SECTION:ErrorHandling]]."""
        _, output_path = _transform_to_tmp(generic_orchestrator_input, tmp_path)
        content = _read(output_path)
        eh_open = content.find("[[SECTION:ErrorHandling]]")
        eh_close = content.find("[[/SECTION:ErrorHandling]]")
        assert eh_open != -1 and eh_close != -1

        # All three unique subsection headings must be between the two tags
        for heading in (
            "## Core Orchestration Loop",
            "## Agent Callbacks vs Rollbacks",
            "## State Recovery (After Restart)",
        ):
            pos = content.find(heading)
            assert pos != -1, f"Heading '{heading}' not found in output"
            assert eh_open < pos < eh_close, (
                f"Heading '{heading}' is not inside [[SECTION:ErrorHandling]] boundary"
            )

    def test_version_bumped_5_3_1(self, generic_orchestrator_input, tmp_path):
        """Version 5.3.1 must be bumped to 6.0.0."""
        result, _ = _transform_to_tmp(generic_orchestrator_input, tmp_path)
        assert result.version_before == "5.3.1"
        assert result.version_after == "6.0.0"

    def test_output_matches_expected_exactly(
        self, generic_orchestrator_input, generic_orchestrator_expected, tmp_path
    ):
        _, output_path = _transform_to_tmp(generic_orchestrator_input, tmp_path)
        assert _read(output_path) == _read(generic_orchestrator_expected)


# ---------------------------------------------------------------------------
# Interface agent — missing language_patterns, codebase_context, output_artifact_template
# ---------------------------------------------------------------------------

class TestGenericInterface:
    """Interface agent file that omits several common injections."""

    def test_transformation_succeeds(self, generic_interface_input, tmp_path):
        result, _ = _transform_to_tmp(generic_interface_input, tmp_path)
        assert result.success is True

    def test_absent_injections_not_added(self, generic_interface_input, tmp_path):
        """Injections without markers in the file must NOT be added."""
        result, _ = _transform_to_tmp(generic_interface_input, tmp_path)
        for absent in ("LanguagePatterns", "CodebaseContext", "OutputArtifactTemplate"):
            assert absent not in result.injections_added, (
                f"Injection {absent} was added but no marker existed in the input"
            )

    def test_version_bumped_4_2_0(self, generic_interface_input, tmp_path):
        """Version 4.2.0 must be bumped to 5.0.0."""
        result, _ = _transform_to_tmp(generic_interface_input, tmp_path)
        assert result.version_before == "4.2.0"
        assert result.version_after == "5.0.0"

    def test_all_seven_sections_still_added(self, generic_interface_input, tmp_path):
        """All 7 section boundaries must still be added even when injections are missing."""
        result, _ = _transform_to_tmp(generic_interface_input, tmp_path)
        assert len(result.sections_added) == 7

    def test_output_matches_expected_exactly(
        self, generic_interface_input, generic_interface_expected, tmp_path
    ):
        _, output_path = _transform_to_tmp(generic_interface_input, tmp_path)
        assert _read(output_path) == _read(generic_interface_expected)


# ---------------------------------------------------------------------------
# CodebaseAgnostic harness — harness_constraints content filled, other markers present
# ---------------------------------------------------------------------------

class TestHarnessCodebaseAgnostic:
    """CodebaseAgnostic harness: harness_constraints is filled, transform_version present."""

    def test_transformation_requires_generic_ref(
        self, harness_codebase_agnostic_input, tmp_path
    ):
        """Transforming a harness file without --generic-ref must fail with an error."""
        result = transform_file(
            harness_codebase_agnostic_input,
            tmp_path / harness_codebase_agnostic_input.name,
            generic_ref_path=None,
        )
        assert result.success is False
        assert len(result.errors) >= 1
        assert any("generic" in e.message.lower() or "transform_version" in e.message.lower()
                   for e in result.errors)

    def test_transformation_succeeds_with_generic_ref(
        self, harness_codebase_agnostic_input, generic_standard_input, tmp_path
    ):
        """Transformation with a valid generic ref must succeed."""
        result, _ = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert result.success is True

    def test_harness_constraints_boundary_added(
        self, harness_codebase_agnostic_input, generic_standard_input, tmp_path
    ):
        """HarnessConstraints boundary must be inserted around the filled content."""
        result, output_path = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert "HarnessConstraints" in result.injections_added
        content = _read(output_path)
        assert "[[INJECTION:HarnessConstraints]]" in content
        assert "[[/INJECTION:HarnessConstraints]]" in content

    def test_filled_harness_content_preserved_inside_boundary(
        self, harness_codebase_agnostic_input, generic_standard_input, tmp_path
    ):
        """Filled harness_constraints content must be preserved verbatim inside the boundary."""
        _, output_path = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        content = _read(output_path)
        open_pos = content.find("[[INJECTION:HarnessConstraints]]")
        close_pos = content.find("[[/INJECTION:HarnessConstraints]]")
        assert open_pos != -1 and close_pos != -1 and open_pos < close_pos
        inner = content[open_pos:close_pos]
        assert "Use only the Read, Write, Edit, Bash" in inner

    def test_same_boundary_set_as_generic_ref(
        self, harness_codebase_agnostic_input, generic_standard_input, tmp_path
    ):
        """The transformed harness must have the same injection names as the transformed generic."""
        result_harness, _ = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        result_generic, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert set(result_harness.injections_added) == set(result_generic.injections_added)

    def test_transform_version_also_bumped(
        self, harness_codebase_agnostic_input, generic_standard_input, tmp_path
    ):
        """transform_version in frontmatter must also have its major number incremented."""
        _, output_path = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        content = _read(output_path)
        # transform_version: 2.2.0 -> 3.0.0
        assert "transform_version: 3.0.0" in content

    def test_output_matches_expected_exactly(
        self,
        harness_codebase_agnostic_input,
        harness_codebase_agnostic_expected,
        generic_standard_input,
        tmp_path,
    ):
        _, output_path = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert _read(output_path) == _read(harness_codebase_agnostic_expected)


# ---------------------------------------------------------------------------
# ExampleProject harness — ALL injection markers removed/filled
# ---------------------------------------------------------------------------

class TestHarnessExampleProject:
    """ExampleProject harness: all injection markers are removed; all content is filled."""

    def test_transformation_requires_generic_ref(
        self, harness_example_project_input, tmp_path
    ):
        """Transforming an ExampleProject harness without --generic-ref must fail."""
        result = transform_file(
            harness_example_project_input,
            tmp_path / harness_example_project_input.name,
            generic_ref_path=None,
        )
        assert result.success is False

    def test_transformation_succeeds_with_generic_ref(
        self, harness_example_project_input, generic_standard_input, tmp_path
    ):
        result, _ = _transform_to_tmp(
            harness_example_project_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert result.success is True

    def test_all_generic_injections_present_in_output(
        self, harness_example_project_input, generic_standard_input, tmp_path
    ):
        """Every injection from the generic ref must have a boundary in the ExampleProject output."""
        result_generic, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        result_example, _ = _transform_to_tmp(
            harness_example_project_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert set(result_example.injections_added) == set(result_generic.injections_added)

    def test_filled_injection_content_inside_boundaries(
        self, harness_example_project_input, generic_standard_input, tmp_path
    ):
        """Filled injection content must appear between the open and close boundary tags."""
        _, output_path = _transform_to_tmp(
            harness_example_project_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        content = _read(output_path)

        # LanguagePatterns content
        lp_open = content.find("[[INJECTION:LanguagePatterns]]")
        lp_close = content.find("[[/INJECTION:LanguagePatterns]]")
        assert lp_open != -1 and lp_close != -1
        lp_inner = content[lp_open:lp_close]
        assert "Use Python 3.10+" in lp_inner

        # CodebaseContext content
        cc_open = content.find("[[INJECTION:CodebaseContext]]")
        cc_close = content.find("[[/INJECTION:CodebaseContext]]")
        assert cc_open != -1 and cc_close != -1
        cc_inner = content[cc_open:cc_close]
        assert "MyProject API" in cc_inner

        # ContextLimits content
        clim_open = content.find("[[INJECTION:ContextLimits]]")
        clim_close = content.find("[[/INJECTION:ContextLimits]]")
        assert clim_open != -1 and clim_close != -1
        clim_inner = content[clim_open:clim_close]
        assert "200k tokens" in clim_inner

    def test_output_matches_expected_exactly(
        self,
        harness_example_project_input,
        harness_example_project_expected,
        generic_standard_input,
        tmp_path,
    ):
        _, output_path = _transform_to_tmp(
            harness_example_project_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert _read(output_path) == _read(harness_example_project_expected)


# ---------------------------------------------------------------------------
# Artifact-template prose edge case — fenced code block + consecutive empty injections
# ---------------------------------------------------------------------------

class TestArtifactTemplateProse:
    """Fenced code block with lookalike headings must not trigger new section boundaries."""

    def test_transformation_succeeds(self, generic_artifact_template_input, tmp_path):
        result, _ = _transform_to_tmp(generic_artifact_template_input, tmp_path)
        assert result.success is True

    def test_fenced_block_headings_not_treated_as_sections(
        self, generic_artifact_template_input, tmp_path
    ):
        """# and ## inside fenced code blocks must never become section boundaries."""
        _, output_path = _transform_to_tmp(generic_artifact_template_input, tmp_path)
        content = _read(output_path)
        lines = content.splitlines()
        in_fence = False
        for line in lines:
            if line.startswith("```"):
                in_fence = not in_fence
                continue
            if in_fence:
                assert not line.startswith("[[SECTION:"), (
                    f"Section tag found inside fenced code block: {line!r}"
                )

    def test_capabilities_has_exactly_one_open_section_tag(
        self, generic_artifact_template_input, tmp_path
    ):
        """Exactly one [[SECTION:Capabilities]] open tag must appear (not one per heading in the template)."""
        _, output_path = _transform_to_tmp(generic_artifact_template_input, tmp_path)
        content = _read(output_path)
        assert content.count("[[SECTION:Capabilities]]") == 1

    def test_static_template_prose_not_boundaried_as_injection(
        self, generic_artifact_template_input, tmp_path
    ):
        """Static template prose inside a fenced block must not be wrapped in injection boundaries."""
        _, output_path = _transform_to_tmp(generic_artifact_template_input, tmp_path)
        content = _read(output_path)
        # The fenced block content should remain unchanged
        assert "# Report Title" in content
        assert "## Summary" in content
        assert "## Findings" in content
        # These headings inside the fenced block must not have [[SECTION:...]] wrappers
        assert "[[SECTION:Summary]]" not in content
        assert "[[SECTION:Findings]]" not in content

    def test_empty_injection_markers_after_template_wrapped(
        self, generic_artifact_template_input, tmp_path
    ):
        """Empty [INJECTION: ...] markers following the template prose must be wrapped."""
        result, _ = _transform_to_tmp(generic_artifact_template_input, tmp_path)
        for name in ("LanguagePatterns", "CodebaseContext", "OutputArtifactTemplate"):
            assert name in result.injections_added

    def test_version_bumped_1_0_0(self, generic_artifact_template_input, tmp_path):
        """Version 1.0.0 must be bumped to 2.0.0."""
        result, _ = _transform_to_tmp(generic_artifact_template_input, tmp_path)
        assert result.version_before == "1.0.0"
        assert result.version_after == "2.0.0"

    def test_output_matches_expected_exactly(
        self, generic_artifact_template_input, generic_artifact_template_expected, tmp_path
    ):
        _, output_path = _transform_to_tmp(generic_artifact_template_input, tmp_path)
        assert _read(output_path) == _read(generic_artifact_template_expected)


# ---------------------------------------------------------------------------
# Version bump edge cases
# ---------------------------------------------------------------------------

class TestVersionBump:
    """Version bumping must increment the major component and reset minor + patch."""

    @pytest.mark.parametrize("version_before, version_after", [
        ("2.2.0", "3.0.0"),
        ("2.3.0", "3.0.0"),
        ("5.3.1", "6.0.0"),
        ("4.2.0", "5.0.0"),
        ("1.0.0", "2.0.0"),
        ("10.9.8", "11.0.0"),
    ])
    def test_major_bumped_minor_and_patch_reset(
        self, version_before, version_after, generic_standard_input, tmp_path
    ):
        """Major must increment; minor and patch must be reset to 0."""
        # Rewrite frontmatter to use the parametrized version, then transform.
        content = _read(generic_standard_input)
        original_version_line = next(
            l for l in content.splitlines() if l.startswith("version:")
        )
        modified_content = content.replace(
            original_version_line, f"version: {version_before}", 1
        )
        patched_input = tmp_path / f"patched_{version_before.replace('.', '_')}.md"
        patched_input.write_text(modified_content, encoding="utf-8")

        result = transform_file(patched_input, tmp_path / "out.md")
        assert result.version_before == version_before
        assert result.version_after == version_after


# ---------------------------------------------------------------------------
# Error conditions
# ---------------------------------------------------------------------------

class TestErrorConditions:
    """The transformer must report errors and exit non-zero for invalid inputs."""

    def test_harness_without_generic_ref_returns_failure(
        self, harness_codebase_agnostic_input, tmp_path
    ):
        """A file with transform_version in frontmatter requires --generic-ref; missing it is an error."""
        result = transform_file(
            harness_codebase_agnostic_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.success is False
        assert result.errors  # at least one error reported

    def test_harness_without_generic_ref_does_not_write_output(
        self, harness_codebase_agnostic_input, tmp_path
    ):
        """When --generic-ref is missing for a harness file, no output file must be written."""
        output_path = tmp_path / "out.md"
        transform_file(harness_codebase_agnostic_input, output_path, generic_ref_path=None)
        assert not output_path.exists()

    def test_transform_error_has_line_number(
        self, harness_codebase_agnostic_input, tmp_path
    ):
        """Each TransformError must include a 1-based line_number."""
        result = transform_file(
            harness_codebase_agnostic_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        for err in result.errors:
            assert isinstance(err, TransformError)
            assert err.line_number >= 1
            assert err.message  # non-empty message


# ---------------------------------------------------------------------------
# CLI integration tests (subprocess)
# ---------------------------------------------------------------------------

_TRANSFORMER_CLI = _TOOLS_DIR / "boundary_transformer.py"


class TestCLI:
    """CLI exit codes and stderr output."""

    def test_cli_exits_zero_on_success(self, generic_standard_input, tmp_path):
        """CLI must exit 0 when transformation succeeds."""
        output_path = tmp_path / "out.md"
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(generic_standard_input), "--output", str(output_path)],
            capture_output=True, text=True,
        )
        assert proc.returncode == 0

    def test_cli_exits_nonzero_on_harness_without_ref(
        self, harness_codebase_agnostic_input, tmp_path
    ):
        """CLI must exit non-zero when a harness file is given without --generic-ref.

        The non-zero exit must come from the tool detecting the missing --generic-ref,
        not from an uncaught exception (e.g. NotImplementedError) crashing the process.
        """
        output_path = tmp_path / "out.md"
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_codebase_agnostic_input), "--output", str(output_path)],
            capture_output=True, text=True,
        )
        # Reject a crash-based exit: a NotImplementedError traceback is not a
        # valid implementation of "detect missing --generic-ref and report it."
        assert "NotImplementedError" not in proc.stderr, (
            "CLI must not crash with NotImplementedError; it must detect the "
            "missing --generic-ref and exit non-zero with a descriptive error."
        )
        assert proc.returncode != 0

    def test_cli_prints_error_to_stderr_on_failure(
        self, harness_codebase_agnostic_input, tmp_path
    ):
        """CLI must print a meaningful error message to stderr (not stdout) on failure.

        A Python crash traceback sent to stderr does not satisfy this requirement —
        the error must be a structured, human-readable message that includes the input
        file path so the caller can identify which file caused the problem.
        """
        output_path = tmp_path / "out.md"
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_codebase_agnostic_input), "--output", str(output_path)],
            capture_output=True, text=True,
        )
        # A raw traceback is not a structured error message.
        assert "NotImplementedError" not in proc.stderr, (
            "CLI stderr must contain a structured error message, not a crash traceback."
        )
        # The error message must name the problematic file so callers can act on it.
        assert str(harness_codebase_agnostic_input) in proc.stderr, (
            "The input file path must appear in the stderr error output."
        )
        assert proc.stderr.strip()   # something on stderr
        assert not proc.stdout.strip()  # nothing on stdout

    def test_cli_no_stdout_on_success(self, generic_standard_input, tmp_path):
        """CLI must produce no stdout output on success (and must actually succeed)."""
        output_path = tmp_path / "out.md"
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(generic_standard_input), "--output", str(output_path)],
            capture_output=True, text=True,
        )
        assert proc.returncode == 0, f"CLI should have succeeded but got exit {proc.returncode}; stderr: {proc.stderr}"
        assert not proc.stdout.strip()

    def test_cli_stderr_includes_file_path_on_failure(
        self, harness_codebase_agnostic_input, tmp_path
    ):
        """Error messages on stderr must include the input file path."""
        output_path = tmp_path / "out.md"
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_codebase_agnostic_input), "--output", str(output_path)],
            capture_output=True, text=True,
        )
        assert str(harness_codebase_agnostic_input) in proc.stderr

    def test_cli_overwrites_in_place_when_no_output_flag(
        self, generic_standard_input, generic_standard_expected, tmp_path
    ):
        """Without --output, CLI must overwrite the input file in-place."""
        # Copy the input fixture to tmp_path so we don't modify the original.
        import shutil
        work_copy = tmp_path / "work.md"
        shutil.copy(generic_standard_input, work_copy)

        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI), str(work_copy)],
            capture_output=True, text=True,
        )
        assert proc.returncode == 0
        assert _read(work_copy) == _read(generic_standard_expected)

    def test_cli_with_generic_ref_flag(
        self,
        harness_codebase_agnostic_input,
        harness_codebase_agnostic_expected,
        generic_standard_input,
        tmp_path,
    ):
        """--generic-ref flag must enable harness file transformation."""
        output_path = tmp_path / "out.md"
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_codebase_agnostic_input),
             "--output", str(output_path),
             "--generic-ref", str(generic_standard_input)],
            capture_output=True, text=True,
        )
        assert proc.returncode == 0
        assert _read(output_path) == _read(harness_codebase_agnostic_expected)


# ---------------------------------------------------------------------------
# Private helpers used only in this test module
# ---------------------------------------------------------------------------

def _heading_text(section_name: str) -> str:
    """Return the heading text (after '## ') for a canonical section name."""
    _map = {
        "CommunicationProtocol": "Communication Protocol",
        "Capabilities": "Capabilities",
        "Constraints": "Constraints",
        "ErrorHandling": "Error Handling",
        "OutputFormat": "Output Format",
        "ExecutionPhilosophy": "Execution Philosophy",
    }
    return _map.get(section_name, section_name)


def _extract_body_lines(content: str) -> list[str]:
    """Return non-frontmatter, non-separator, non-injection-marker lines from the input.

    Old-format [INJECTION: name] markers are transformation targets — they are
    replaced (not preserved) by the transformer, so they are excluded here to
    allow symmetric comparison with _extract_body_lines_excluding_tags output.
    """
    lines = content.splitlines()
    # Skip frontmatter (between first and second '---') and '---' separators.
    in_fm = False
    past_fm = False
    body = []
    for line in lines:
        if line == "---":
            if not in_fm:
                in_fm = True
                continue
            else:
                past_fm = True
                continue
        if past_fm:
            # Old-format injection markers are transformation targets, not preserved text.
            if _OLD_INJECTION_LINE_RE.match(line):
                continue
            body.append(line)
    return body


def _extract_body_lines_excluding_tags(content: str) -> list[str]:
    """Return non-frontmatter, non-boundary-tag lines from transformed content.

    Excludes:
    - YAML frontmatter block and '---' separators
    - New-format boundary tags: [[SECTION:...]], [[/SECTION:...]], [[INJECTION:...]], [[/INJECTION:...]]
    - List-item variants of new-format injection tags (e.g. '- [[INJECTION:ContextLimits]]')
    - Old-format injection markers [INJECTION: name] (transformation targets that must
      not appear in the output; their exclusion here prevents false-negative passes if
      the transformer accidentally leaves them unconverted — the explicit assertion in
      test_body_text_preserved_verbatim catches that case directly).
    """
    lines = content.splitlines()
    in_fm = False
    past_fm = False
    body = []
    for line in lines:
        if line == "---":
            if not in_fm:
                in_fm = True
                continue
            else:
                past_fm = True
                continue
        if past_fm:
            stripped = line.strip()
            # Skip new-format boundary tags (open and close)
            if stripped.startswith("[[SECTION:") or stripped.startswith("[[/SECTION:"):
                continue
            if stripped.startswith("[[INJECTION:") or stripped.startswith("[[/INJECTION:"):
                continue
            # Also skip lines that are ONLY a list-item wrapping an injection tag
            if stripped.startswith("- [[INJECTION:") or stripped.startswith("- [[/INJECTION:"):
                continue
            # Skip old-format injection markers (should have been replaced; the explicit
            # assert in the verbatim test guards against them being left in the output)
            if _OLD_INJECTION_LINE_RE.match(line):
                continue
            body.append(line)
    return body


# ---------------------------------------------------------------------------
# Frontmatter key preservation (AC2.5)
# ---------------------------------------------------------------------------

def _parse_frontmatter_keys(content: str) -> set[str]:
    """Return the set of YAML key names present in the frontmatter block."""
    lines = content.splitlines()
    in_fm = False
    keys: set[str] = set()
    for line in lines:
        if line == "---":
            if not in_fm:
                in_fm = True
                continue
            else:
                break  # end of frontmatter
        if in_fm and ":" in line:
            key = line.split(":")[0].strip()
            if key:
                keys.add(key)
    return keys


class TestFrontmatterKeys:
    """The transformer must not introduce new YAML frontmatter keys (AC2.5 / FR-12)."""

    def test_no_new_frontmatter_keys_introduced(
        self, generic_standard_input, tmp_path
    ):
        """Transformation must not add any YAML frontmatter keys that were not in the input."""
        _, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        input_keys = _parse_frontmatter_keys(_read(generic_standard_input))
        output_keys = _parse_frontmatter_keys(_read(output_path))
        new_keys = output_keys - input_keys
        assert not new_keys, (
            f"Transformation introduced unexpected frontmatter keys: {new_keys!r}"
        )

    def test_version_key_name_is_unchanged(
        self, generic_standard_input, tmp_path
    ):
        """Only the value of 'version' changes; the key name must remain 'version'."""
        _, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        output_keys = _parse_frontmatter_keys(_read(output_path))
        assert "version" in output_keys, (
            "The 'version' key must still be present after transformation."
        )

    def test_transform_version_not_added_to_generic_file(
        self, generic_standard_input, tmp_path
    ):
        """A generic file (no transform_version in input) must not gain a transform_version key."""
        input_keys = _parse_frontmatter_keys(_read(generic_standard_input))
        assert "transform_version" not in input_keys, (
            "Precondition: generic_standard_input must not have transform_version"
        )
        _, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        output_keys = _parse_frontmatter_keys(_read(output_path))
        assert "transform_version" not in output_keys, (
            "transform_version must not be injected into generic files that did not have it."
        )


# ---------------------------------------------------------------------------
# Unclassifiable content (AC2.6)
# ---------------------------------------------------------------------------

class TestUnclassifiableContent:
    """Content that cannot be mapped to any canonical boundary must be reported and exit non-zero."""

    def test_result_is_failure(self, unclassifiable_content_input, tmp_path):
        """Transformation of a file with unclassifiable content must return success=False."""
        result = transform_file(
            unclassifiable_content_input,
            tmp_path / unclassifiable_content_input.name,
        )
        assert result.success is False

    def test_error_list_is_nonempty(self, unclassifiable_content_input, tmp_path):
        """At least one TransformError must be reported for unclassifiable content."""
        result = transform_file(
            unclassifiable_content_input,
            tmp_path / unclassifiable_content_input.name,
        )
        assert len(result.errors) >= 1

    def test_error_has_line_number_and_message(
        self, unclassifiable_content_input, tmp_path
    ):
        """Each TransformError must carry a 1-based line_number and a non-empty message."""
        result = transform_file(
            unclassifiable_content_input,
            tmp_path / unclassifiable_content_input.name,
        )
        for err in result.errors:
            assert isinstance(err, TransformError)
            assert isinstance(err.line_number, int) and err.line_number >= 1, (
                f"Expected 1-based integer line_number, got {err.line_number!r}"
            )
            assert err.message, "TransformError.message must be non-empty"

    def test_cli_exits_nonzero(self, unclassifiable_content_input, tmp_path):
        """CLI must exit non-zero when the input contains unclassifiable content."""
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(unclassifiable_content_input), "--output", str(tmp_path / "out.md")],
            capture_output=True, text=True,
        )
        assert "NotImplementedError" not in proc.stderr, (
            "CLI must report the classification error, not crash with NotImplementedError."
        )
        assert proc.returncode != 0

    def test_cli_stderr_includes_file_path(
        self, unclassifiable_content_input, tmp_path
    ):
        """The file path must appear in the stderr error output (per AC2.6)."""
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(unclassifiable_content_input), "--output", str(tmp_path / "out.md")],
            capture_output=True, text=True,
        )
        assert str(unclassifiable_content_input) in proc.stderr, (
            "stderr must contain the input file path so the caller can identify the problem file."
        )

    def test_cli_stderr_includes_line_number(
        self, unclassifiable_content_input, tmp_path
    ):
        """The error output must embed a line number in the format 'path:N: message' (per AC2.6)."""
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(unclassifiable_content_input), "--output", str(tmp_path / "out.md")],
            capture_output=True, text=True,
        )
        assert re.search(r":\d+:", proc.stderr), (
            "stderr must embed a 1-based line number as ':N:' "
            "(e.g. '/path/file.md:18: unclassifiable heading ...')"
        )


# ---------------------------------------------------------------------------
# Malformed frontmatter error paths (Major-3)
# ---------------------------------------------------------------------------

class TestMalformedFrontmatter:
    """The transformer must fail gracefully when YAML frontmatter is structurally invalid."""

    def test_missing_closing_separator_returns_failure(
        self, malformed_no_closing_separator_input, tmp_path
    ):
        """A file whose frontmatter block has no closing '---' must produce success=False."""
        result = transform_file(
            malformed_no_closing_separator_input,
            tmp_path / malformed_no_closing_separator_input.name,
        )
        assert result.success is False
        assert result.errors, "At least one error must be reported for malformed frontmatter."

    def test_missing_version_field_returns_failure(
        self, malformed_no_version_field_input, tmp_path
    ):
        """A file with no 'version' key in frontmatter must produce success=False."""
        result = transform_file(
            malformed_no_version_field_input,
            tmp_path / malformed_no_version_field_input.name,
        )
        assert result.success is False
        assert result.errors, "At least one error must be reported when version field is absent."

    def test_malformed_frontmatter_does_not_write_output(
        self, malformed_no_closing_separator_input, tmp_path
    ):
        """When frontmatter is malformed, no partial output file must be written."""
        output_path = tmp_path / "out.md"
        transform_file(malformed_no_closing_separator_input, output_path)
        assert not output_path.exists(), (
            "Partial output must not be written when the input has malformed frontmatter."
        )

    def test_malformed_frontmatter_cli_exits_nonzero(
        self, malformed_no_closing_separator_input, tmp_path
    ):
        """CLI must exit non-zero when frontmatter is malformed (missing closing '---')."""
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(malformed_no_closing_separator_input),
             "--output", str(tmp_path / "out.md")],
            capture_output=True, text=True,
        )
        assert "NotImplementedError" not in proc.stderr, (
            "CLI must report the parse error, not crash with NotImplementedError."
        )
        assert proc.returncode != 0

    def test_missing_version_cli_exits_nonzero(
        self, malformed_no_version_field_input, tmp_path
    ):
        """CLI must exit non-zero when the version field is absent from frontmatter."""
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(malformed_no_version_field_input),
             "--output", str(tmp_path / "out.md")],
            capture_output=True, text=True,
        )
        assert "NotImplementedError" not in proc.stderr
        assert proc.returncode != 0
