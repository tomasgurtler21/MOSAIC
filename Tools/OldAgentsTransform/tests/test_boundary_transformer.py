"""Tests for the boundary transformation tool.

Tests verify overall transformation behavior: given an input file, the output
must contain the correct boundaries in the correct order with body text preserved.
Tests do NOT test individual methods — they test end-to-end transformation outcomes.

All tests are in TDD RED phase: they will fail until boundary_transformer.py is implemented.
The Stage 4 test classes (TestProvenanceUntaggedInput, TestProvenanceOldShapeMigration,
TestProvenanceIdempotency, TestProvenanceValidatorIntegration) will fail until the
transformer's provenance migration contracts (T-A through T-D) are implemented.
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
from boundary_validator import validate_file  # noqa: E402

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

    def test_all_six_sections_added(
        self, generic_standard_input, tmp_path
    ):
        """All 6 recognised section boundaries must be added to a standard generic file.

        CommunicationProtocol is no longer an authored section heading; the protocol
        slot is a top-level [[DEPLOYED:CommunicationProtocol]] boundary filled by the
        deploy tool, not by the transformer.
        """
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        expected_sections = [
            "Identity",
            "Capabilities",
            "Constraints",
            "ErrorHandling",
            "OutputFormat",
            "ExecutionPhilosophy",
        ]
        assert result.sections_added == expected_sections

    def test_common_regions_added(
        self, generic_standard_input, tmp_path
    ):
        """All 8 managed region boundaries must be added (user-owned and tool-managed).

        ProtocolExtension has been removed from the vocabulary entirely.
        LanguagePatterns, HarnessConstraints, and CustomConstraints are now
        tool-managed (emitted as [[DEPLOYED:...]]).  They still appear in
        injections_added because TransformResult.injections_added tracks all
        managed region names regardless of marker kind.
        """
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        expected_injections = [
            "IdentityExtension",
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
        """A list-item [INJECTION: context_limits] must become a standalone tag (no '- ' prefix).

        A boundary tag must occupy its own line to match TAG_PATTERN and be
        recognised by the validator, so the '- ' list-item prefix is dropped.
        """
        _, output_path = _transform_to_tmp(generic_validation_input, tmp_path)
        lines = _read(output_path).splitlines()
        cl_lines = [l for l in lines if "[[INJECTION:ContextLimits]]" in l]
        assert len(cl_lines) == 1
        assert cl_lines[0] == "[[INJECTION:ContextLimits]]", (
            f"List-item ContextLimits must become a standalone tag with no '- ' "
            f"prefix, got: {cl_lines[0]!r}"
        )

    def test_close_tag_after_list_item_injection(
        self, generic_validation_input, tmp_path
    ):
        """The [[/INJECTION:ContextLimits]] close tag must follow on the very next line."""
        _, output_path = _transform_to_tmp(generic_validation_input, tmp_path)
        lines = _read(output_path).splitlines()
        for i, line in enumerate(lines):
            if line == "[[INJECTION:ContextLimits]]":
                assert i + 1 < len(lines) and lines[i + 1] == "[[/INJECTION:ContextLimits]]", (
                    "Close tag must immediately follow open tag for empty list-item injection"
                )
                break
        else:
            pytest.fail("Did not find '[[INJECTION:ContextLimits]]' in output")


# ---------------------------------------------------------------------------
# Orchestrator — available_workflows, unique non-canonical subsections
# ---------------------------------------------------------------------------

class TestGenericOrchestrator:
    """Orchestrator file: available_workflows injection, non-canonical subsections in ErrorHandling."""

    def test_transformation_succeeds(self, generic_orchestrator_input, tmp_path):
        result, _ = _transform_to_tmp(generic_orchestrator_input, tmp_path)
        assert result.success is True

    def test_five_sections_added(self, generic_orchestrator_input, tmp_path):
        """Orchestrator has no OutputFormat section — only 5 section boundaries.

        CommunicationProtocol is no longer an authored section, and the
        orchestrator source also has no OutputFormat section, leaving
        Identity, Capabilities, Constraints, ErrorHandling, ExecutionPhilosophy.
        """
        result, _ = _transform_to_tmp(generic_orchestrator_input, tmp_path)
        assert "OutputFormat" not in result.sections_added
        assert len(result.sections_added) == 5

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

    def test_all_six_sections_still_added(self, generic_interface_input, tmp_path):
        """All 6 recognised section boundaries must still be added even when injections are missing."""
        result, _ = _transform_to_tmp(generic_interface_input, tmp_path)
        assert len(result.sections_added) == 6

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
        """HarnessConstraints boundary must be inserted around the filled content.

        HarnessConstraints is a tool-managed name so it is emitted as [[DEPLOYED:]].
        """
        result, output_path = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert "HarnessConstraints" in result.injections_added
        content = _read(output_path)
        assert "[[DEPLOYED:HarnessConstraints]]" in content
        assert "[[/DEPLOYED:HarnessConstraints]]" in content

    def test_filled_harness_content_preserved_inside_boundary(
        self, harness_codebase_agnostic_input, generic_standard_input, tmp_path
    ):
        """Filled harness_constraints content must be preserved verbatim inside the boundary.

        HarnessConstraints is tool-managed so the boundary tags are [[DEPLOYED:]].
        """
        _, output_path = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        content = _read(output_path)
        open_pos = content.find("[[DEPLOYED:HarnessConstraints]]")
        close_pos = content.find("[[/DEPLOYED:HarnessConstraints]]")
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
        """Filled injection content must appear between the open and close boundary tags.

        LanguagePatterns is tool-managed so its boundary uses [[DEPLOYED:]].
        CodebaseContext and ContextLimits are user-owned so they use [[INJECTION:]].
        """
        _, output_path = _transform_to_tmp(
            harness_example_project_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        content = _read(output_path)

        # LanguagePatterns is tool-managed — emitted as [[DEPLOYED:]]
        lp_open = content.find("[[DEPLOYED:LanguagePatterns]]")
        lp_close = content.find("[[/DEPLOYED:LanguagePatterns]]")
        assert lp_open != -1 and lp_close != -1
        lp_inner = content[lp_open:lp_close]
        assert "Use Python 3.10+" in lp_inner

        # CodebaseContext is user-owned — emitted as [[INJECTION:]]
        cc_open = content.find("[[INJECTION:CodebaseContext]]")
        cc_close = content.find("[[/INJECTION:CodebaseContext]]")
        assert cc_open != -1 and cc_close != -1
        cc_inner = content[cc_open:cc_close]
        assert "MyProject API" in cc_inner

        # ContextLimits is user-owned — emitted as [[INJECTION:]]
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
        "ArtifactProvenance": "Artifact Provenance",
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
            # Skip new-format boundary tags (open and close) for all three kinds
            if stripped.startswith("[[SECTION:") or stripped.startswith("[[/SECTION:"):
                continue
            if stripped.startswith("[[INJECTION:") or stripped.startswith("[[/INJECTION:"):
                continue
            if stripped.startswith("[[DEPLOYED:") or stripped.startswith("[[/DEPLOYED:"):
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

    def test_harness_specific_frontmatter_fields_preserved_verbatim(
        self, generic_standard_input, tmp_path
    ):
        """Harness-specific frontmatter fields must survive transformation byte-for-byte.

        Regression guard: an earlier transformer emitted only a hardcoded subset of
        frontmatter keys, silently dropping harness-specific fields such as an
        inline `mode`, a multi-line `permission:` map, and an `mcpServers:` list.
        The transformer must preserve the entire frontmatter block verbatim and
        change only the version / transform_version values.
        """
        harness_fm = (
            "---\n"
            "id: 99\n"
            "version: 2.2.0\n"
            "transform_version: 2.2.0\n"
            "injections_version: 1.1.0\n"
            "name: extra-fields-agent\n"
            "description: Agent carrying harness-specific multi-line frontmatter\n"
            "mode: subagent\n"
            "model: claude-opus-4\n"
            "tools: Read, Write, Edit, Bash, Glob, Grep, AskUserQuestion\n"
            "permission:\n"
            "  read: allow\n"
            "  write: allow\n"
            "  bash: deny\n"
            "mcpServers:\n"
            "  - hw-schema\n"
            "  - user-feedback\n"
            "---\n"
        )
        body = _read(generic_standard_input).split("---\n", 2)[2]
        input_path = tmp_path / "extra_fields_input.md"
        input_path.write_text(harness_fm + body, encoding="utf-8")

        output_path = tmp_path / "extra_fields_output.md"
        result = transform_file(input_path, output_path, generic_standard_input)
        assert result.success is True
        out = _read(output_path)
        out_fm = out.split("---\n", 2)[1]

        # Every non-version field must be preserved exactly, including the
        # multi-line map and list values with their indentation.
        assert "mode: subagent\n" in out_fm
        assert "permission:\n  read: allow\n  write: allow\n  bash: deny\n" in out_fm
        assert "mcpServers:\n  - hw-schema\n  - user-feedback\n" in out_fm
        # Only the version values changed (major bump), key order preserved.
        assert "version: 3.0.0\n" in out_fm
        assert "transform_version: 3.0.0\n" in out_fm


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


# ---------------------------------------------------------------------------
# Malformed generic-reference guard
# ---------------------------------------------------------------------------

class TestMalformedGenericRef:
    """Transforming a harness file against a generic ref with unparseable frontmatter
    must return TransformResult(success=False) and must not raise any exception."""

    def test_returns_failure_not_raises(
        self,
        harness_codebase_agnostic_input: pathlib.Path,
        malformed_generic_ref_input: pathlib.Path,
        tmp_path: pathlib.Path,
    ) -> None:
        """transform_file must return success=False rather than raising KeyError."""
        output_path = tmp_path / harness_codebase_agnostic_input.name
        result = transform_file(
            harness_codebase_agnostic_input,
            output_path,
            generic_ref_path=malformed_generic_ref_input,
        )
        assert result.success is False

    def test_errors_list_is_nonempty(
        self,
        harness_codebase_agnostic_input: pathlib.Path,
        malformed_generic_ref_input: pathlib.Path,
        tmp_path: pathlib.Path,
    ) -> None:
        """At least one TransformError must be reported for the generic-ref parse failure."""
        output_path = tmp_path / harness_codebase_agnostic_input.name
        result = transform_file(
            harness_codebase_agnostic_input,
            output_path,
            generic_ref_path=malformed_generic_ref_input,
        )
        assert len(result.errors) >= 1

    def test_error_message_references_generic_ref_path(
        self,
        harness_codebase_agnostic_input: pathlib.Path,
        malformed_generic_ref_input: pathlib.Path,
        tmp_path: pathlib.Path,
    ) -> None:
        """The TransformError message must contain the generic-ref path so the failure is traceable."""
        output_path = tmp_path / harness_codebase_agnostic_input.name
        result = transform_file(
            harness_codebase_agnostic_input,
            output_path,
            generic_ref_path=malformed_generic_ref_input,
        )
        assert result.errors, "Precondition: at least one error must be present"
        # At least one error must name the generic-ref file so the caller can identify it.
        generic_ref_str = str(malformed_generic_ref_input)
        assert any(generic_ref_str in err.message for err in result.errors), (
            f"Expected at least one TransformError whose message contains the generic-ref path "
            f"'{generic_ref_str}'; got: {[err.message for err in result.errors]!r}"
        )

    def test_no_output_file_written_on_failure(
        self,
        harness_codebase_agnostic_input: pathlib.Path,
        malformed_generic_ref_input: pathlib.Path,
        tmp_path: pathlib.Path,
    ) -> None:
        """No partial output file must be written when the generic-ref frontmatter is malformed."""
        output_path = tmp_path / "malformed_ref_out.md"
        transform_file(
            harness_codebase_agnostic_input,
            output_path,
            generic_ref_path=malformed_generic_ref_input,
        )
        assert not output_path.exists(), (
            "Partial output must not be written when the generic-ref frontmatter cannot be parsed."
        )

    def test_result_fields_populated_consistently(
        self,
        harness_codebase_agnostic_input: pathlib.Path,
        malformed_generic_ref_input: pathlib.Path,
        tmp_path: pathlib.Path,
    ) -> None:
        """Failure result fields must match the documented contract for this early-return path.

        sections_added, injections_added, and deployed_added must all be empty lists.
        version_after must be empty string.
        version_before must be the input file's own version value (already parsed).
        """
        output_path = tmp_path / harness_codebase_agnostic_input.name
        result = transform_file(
            harness_codebase_agnostic_input,
            output_path,
            generic_ref_path=malformed_generic_ref_input,
        )
        assert result.sections_added == []
        assert result.injections_added == []
        assert result.deployed_added == []
        assert result.version_after == ""
        # version_before must be populated from the input file's already-parsed frontmatter.
        assert result.version_before != "", (
            "version_before must carry the input file's version even on generic-ref failure"
        )

    def test_error_line_number_is_one_based(
        self,
        harness_codebase_agnostic_input: pathlib.Path,
        malformed_generic_ref_input: pathlib.Path,
        tmp_path: pathlib.Path,
    ) -> None:
        """Each TransformError must carry a 1-based line_number >= 1."""
        output_path = tmp_path / harness_codebase_agnostic_input.name
        result = transform_file(
            harness_codebase_agnostic_input,
            output_path,
            generic_ref_path=malformed_generic_ref_input,
        )
        assert result.errors, "Precondition: at least one error must be present"
        for err in result.errors:
            assert isinstance(err, TransformError)
            assert err.line_number >= 1, (
                f"line_number must be 1-based, got {err.line_number!r}"
            )
            assert err.message, "TransformError.message must be non-empty"


# ---------------------------------------------------------------------------
# Provenance migration helpers and test classes — deferred pending Stage 2 decision
# ---------------------------------------------------------------------------
#
# The test classes below (TestProvenanceUntaggedInput, TestProvenanceOldShapeMigration,
# TestProvenanceIdempotency, TestProvenanceValidatorIntegration) and the helpers in this
# section test the transformer's provenance migration behavior: converting old
# [[SECTION:ArtifactProvenance]] files or untagged '## Artifact Provenance' headings into
# the [[DEPLOYED:ArtifactProvenance]] + [[INJECTION:ArtifactProvenanceExtension]] shape.
#
# These classes are deliberately out of scope for Stage 2 tests for the following reason:
# Stage 2 removes ArtifactProvenance from the vocabulary entirely (not from CANONICAL_DEPLOYED,
# CANONICAL_ORDER, or DEPLOYED_PARENT_MAP). The transformer's provenance migration feature
# produces files that carry [[DEPLOYED:ArtifactProvenance]], which becomes an E011
# (unrecognised tool-managed boundary name) after Stage 2 implementation is complete.
# Whether the transformer should continue to do provenance migration (placing a now-retired
# name) or should be updated to remove that migration path entirely is a design decision
# that belongs to the stage that retires or removes provenance migration from
# boundary_transformer.py — not to Stage 2's vocabulary-sync task I2.4, which only updates
# the transformer for the removed CANONICAL_INJECTIONS constant.
#
# Until that decision is made and implemented, these test classes remain in the file as a
# record of the provenance migration contract (T-A through T-D). They will continue to
# fail against the pre-implementation baseline and must be reconciled before they can be
# included in a passing test run.

_TARGET_DEPLOYED_OPEN = "[[DEPLOYED:ArtifactProvenance]]"
_TARGET_DEPLOYED_CLOSE = "[[/DEPLOYED:ArtifactProvenance]]"
_TARGET_INJECTION_OPEN = "[[INJECTION:ArtifactProvenanceExtension]]"
_TARGET_INJECTION_CLOSE = "[[/INJECTION:ArtifactProvenanceExtension]]"
_OLD_SECTION_OPEN = "[[SECTION:ArtifactProvenance]]"
_OLD_SECTION_CLOSE = "[[/SECTION:ArtifactProvenance]]"


def _body(content: str) -> str:
    """Return the content after the closing frontmatter '---' (body only)."""
    lines = content.splitlines(keepends=True)
    in_fm = False
    for i, line in enumerate(lines):
        if line.strip() == "---":
            if not in_fm:
                in_fm = True
            else:
                return "".join(lines[i + 1:])
    return content


def _body_lines_outside_provenance(body: str) -> list[str]:
    """Return body lines with the entire provenance-related region stripped out.

    Removes every line from the first line of a provenance open tag
    (``[[SECTION:ArtifactProvenance]]``, ``[[DEPLOYED:ArtifactProvenance]]``,
    or ``[[INJECTION:ArtifactProvenanceExtension]]`` when at top level)
    through the corresponding close tag.  All other lines are returned
    preserving their exact bytes.

    After ArtifactProvenance retirement, transformer output uses only the
    injection sibling (no deployed region). This helper strips the injection
    block in output the same way it strips the old section block in input,
    so callers get a stable basis for comparing the non-provenance body.

    NOTE: This function is only correct when the injection is NOT nested inside
    a ``[[SECTION:ArtifactProvenance]]`` block. Do not use with old-shape inputs
    that have a non-empty ``[[INJECTION:ArtifactProvenanceExtension]]`` inside
    their section — the injection close tag would prematurely end the skip.
    """
    lines = body.splitlines(keepends=True)
    result = []
    skip = False
    for line in lines:
        stripped = line.strip()
        if stripped in (_TARGET_DEPLOYED_OPEN, _OLD_SECTION_OPEN, _TARGET_INJECTION_OPEN):
            if not skip:
                skip = True
        if not skip:
            result.append(line)
        if skip and stripped in (_TARGET_INJECTION_CLOSE, _OLD_SECTION_CLOSE, _TARGET_DEPLOYED_CLOSE):
            skip = False
    return result


def _extract_injection_content(body: str) -> str:
    """Return the bytes between [[INJECTION:ArtifactProvenanceExtension]] and its close tag.

    Returns '' if the markers are absent or the region is empty.  Preserves
    newlines exactly as they appear in the body, including any trailing newline
    on the last content line.
    """
    open_marker = _TARGET_INJECTION_OPEN + "\n"
    close_marker = _TARGET_INJECTION_CLOSE
    start = body.find(open_marker)
    if start == -1:
        return ""
    content_start = start + len(open_marker)
    end = body.find(close_marker, content_start)
    if end == -1:
        return ""
    return body[content_start:end]


# ---------------------------------------------------------------------------
# Untagged input (Contract T-A)
# ---------------------------------------------------------------------------

class TestProvenanceUntaggedInput:
    """A source file with '## Artifact Provenance' heading transforms to the new deployed-region shape.

    Contract T-A: the heading region is recognised, its prose body is discarded,
    and the empty deployed region plus sibling injection are emitted at canonical
    slot 3 (after Identity, before Capabilities).  No unclassifiable-heading error
    is reported.
    """

    def test_transformation_succeeds(self, provenance_untagged_input, tmp_path):
        """Transformation must succeed: no unclassifiable-heading error for '## Artifact Provenance'."""
        result, _ = _transform_to_tmp(provenance_untagged_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_provenance_heading_not_in_sections_added(
        self, provenance_untagged_input, tmp_path
    ):
        """ArtifactProvenance must appear in deployed_added, never in sections_added."""
        result, _ = _transform_to_tmp(provenance_untagged_input, tmp_path)
        assert "ArtifactProvenance" not in result.sections_added, (
            "ArtifactProvenance is a tool-managed deployed name; it must not appear in sections_added."
        )

    def test_provenance_in_deployed_added(self, provenance_untagged_input, tmp_path):
        """ArtifactProvenance is retired; it must not appear in deployed_added."""
        result, _ = _transform_to_tmp(provenance_untagged_input, tmp_path)
        assert "ArtifactProvenance" not in result.deployed_added, (
            "ArtifactProvenance is retired and must not appear in deployed_added."
        )

    def test_provenance_extension_in_injections_added(
        self, provenance_untagged_input, tmp_path
    ):
        """ArtifactProvenanceExtension must appear in injections_added when the sibling is emitted."""
        result, _ = _transform_to_tmp(provenance_untagged_input, tmp_path)
        assert "ArtifactProvenanceExtension" in result.injections_added

    def test_output_contains_deployed_open_tag(
        self, provenance_untagged_input, tmp_path
    ):
        """ArtifactProvenance is retired; its deployed open tag must NOT appear in the output."""
        _, output_path = _transform_to_tmp(provenance_untagged_input, tmp_path)
        assert _TARGET_DEPLOYED_OPEN not in _read(output_path), (
            "ArtifactProvenance is retired; [[DEPLOYED:ArtifactProvenance]] must not appear in output."
        )

    def test_output_contains_deployed_close_tag(
        self, provenance_untagged_input, tmp_path
    ):
        """ArtifactProvenance is retired; its deployed close tag must NOT appear in the output."""
        _, output_path = _transform_to_tmp(provenance_untagged_input, tmp_path)
        assert _TARGET_DEPLOYED_CLOSE not in _read(output_path), (
            "ArtifactProvenance is retired; [[/DEPLOYED:ArtifactProvenance]] must not appear in output."
        )

    def test_output_contains_sibling_injection_open_tag(
        self, provenance_untagged_input, tmp_path
    ):
        """Output must contain the [[INJECTION:ArtifactProvenanceExtension]] open tag."""
        _, output_path = _transform_to_tmp(provenance_untagged_input, tmp_path)
        assert _TARGET_INJECTION_OPEN in _read(output_path)

    def test_output_contains_sibling_injection_close_tag(
        self, provenance_untagged_input, tmp_path
    ):
        """Output must contain the [[/INJECTION:ArtifactProvenanceExtension]] close tag."""
        _, output_path = _transform_to_tmp(provenance_untagged_input, tmp_path)
        assert _TARGET_INJECTION_CLOSE in _read(output_path)

    def test_deployed_region_is_empty_in_output(
        self, provenance_untagged_input, tmp_path
    ):
        """ArtifactProvenance is retired; no deployed region must appear in the output."""
        _, output_path = _transform_to_tmp(provenance_untagged_input, tmp_path)
        content = _read(output_path)
        assert _TARGET_DEPLOYED_OPEN not in content, (
            "ArtifactProvenance is retired; no [[DEPLOYED:ArtifactProvenance]] region should appear in output."
        )

    def test_deployed_region_before_sibling_injection(
        self, provenance_untagged_input, tmp_path
    ):
        """ArtifactProvenance is retired; the injection appears without a preceding deployed region."""
        _, output_path = _transform_to_tmp(provenance_untagged_input, tmp_path)
        content = _read(output_path)
        deployed_pos = content.find(_TARGET_DEPLOYED_OPEN)
        injection_pos = content.find(_TARGET_INJECTION_OPEN)
        assert deployed_pos == -1, (
            "ArtifactProvenance is retired; [[DEPLOYED:ArtifactProvenance]] must not appear in output."
        )
        assert injection_pos != -1, (
            "[[INJECTION:ArtifactProvenanceExtension]] must still appear in the output."
        )

    def test_provenance_region_at_canonical_slot_3(
        self, provenance_untagged_input, tmp_path
    ):
        """The injection sibling must appear after Identity and before Capabilities (canonical slot 3)."""
        _, output_path = _transform_to_tmp(provenance_untagged_input, tmp_path)
        content = _read(output_path)
        identity_close_pos = content.find("[[/SECTION:Identity]]")
        injection_pos = content.find(_TARGET_INJECTION_OPEN)
        capabilities_open_pos = content.find("[[SECTION:Capabilities]]")
        assert identity_close_pos != -1, "[[/SECTION:Identity]] must be present"
        assert injection_pos != -1, "[[INJECTION:ArtifactProvenanceExtension]] must be present"
        assert capabilities_open_pos != -1, "[[SECTION:Capabilities]] must be present"
        assert identity_close_pos < injection_pos < capabilities_open_pos, (
            "ArtifactProvenanceExtension injection must appear after Identity and before Capabilities."
        )

    def test_no_old_section_tag_in_output(self, provenance_untagged_input, tmp_path):
        """The output must not contain the old [[SECTION:ArtifactProvenance]] tag."""
        _, output_path = _transform_to_tmp(provenance_untagged_input, tmp_path)
        assert _OLD_SECTION_OPEN not in _read(output_path), (
            "Old [[SECTION:ArtifactProvenance]] tag must not appear in the output; "
            "the section was migrated to a deployed region."
        )

    def test_provenance_prose_discarded(self, provenance_untagged_input, tmp_path):
        """The old prose body of the Artifact Provenance section must be discarded."""
        _, output_path = _transform_to_tmp(provenance_untagged_input, tmp_path)
        content = _read(output_path)
        assert "Every output file produced by this agent must carry two provenance fields" \
               not in content, (
            "The old Artifact Provenance prose body must be discarded by the transformer."
        )

    def test_other_sections_added(self, provenance_untagged_input, tmp_path):
        """All six canonical sections must still be added despite the provenance heading removal."""
        result, _ = _transform_to_tmp(provenance_untagged_input, tmp_path)
        for name in ("Identity", "Capabilities", "Constraints",
                     "ErrorHandling", "OutputFormat", "ExecutionPhilosophy"):
            assert name in result.sections_added, (
                f"Section '{name}' missing from sections_added; provenance handling "
                "must not affect other section boundaries."
            )

    def test_version_bumped(self, provenance_untagged_input, tmp_path):
        """The version must be bumped from 2.2.0 to 3.0.0."""
        result, _ = _transform_to_tmp(provenance_untagged_input, tmp_path)
        assert result.version_before == "2.2.0"
        assert result.version_after == "3.0.0"


# ---------------------------------------------------------------------------
# Old-shape migration (Contract T-B)
# ---------------------------------------------------------------------------

class TestProvenanceOldShapeMigration:
    """A [[SECTION:ArtifactProvenance]] block is rewritten to the new deployed-region shape.

    Contract T-B: the [[SECTION:ArtifactProvenance]] ... [[/SECTION:ArtifactProvenance]]
    block is replaced by [[DEPLOYED:ArtifactProvenance]] + [[INJECTION:ArtifactProvenanceExtension]],
    the extension injection's inner content is carried across byte-identically, and every
    byte outside the provenance region and its sibling injection is unchanged apart from
    the frontmatter version bump.
    """

    def test_empty_ext_transformation_succeeds(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """Transformation of the old [[SECTION:ArtifactProvenance]] shape must succeed."""
        result, _ = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_nonempty_ext_transformation_succeeds(
        self, provenance_old_shape_nonempty_ext_input, tmp_path
    ):
        """Transformation with non-empty extension content must also succeed."""
        result, _ = _transform_to_tmp(provenance_old_shape_nonempty_ext_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_provenance_in_deployed_added(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """ArtifactProvenance is retired; it must not appear in deployed_added after old-shape migration."""
        result, _ = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        assert "ArtifactProvenance" not in result.deployed_added, (
            "ArtifactProvenance is retired; the old section must be stripped, not converted to deployed."
        )

    def test_old_section_tag_absent_in_output(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """The old [[SECTION:ArtifactProvenance]] open tag must be absent from the output."""
        _, output_path = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        assert _OLD_SECTION_OPEN not in _read(output_path), (
            "[[SECTION:ArtifactProvenance]] must be replaced; the old open tag must not survive."
        )

    def test_old_section_close_tag_absent_in_output(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """The old [[/SECTION:ArtifactProvenance]] close tag must be absent from the output."""
        _, output_path = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        assert _OLD_SECTION_CLOSE not in _read(output_path), (
            "[[/SECTION:ArtifactProvenance]] must be replaced; the old close tag must not survive."
        )

    def test_new_deployed_open_tag_present(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """ArtifactProvenance is retired; its deployed open tag must NOT appear in the output."""
        _, output_path = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        assert _TARGET_DEPLOYED_OPEN not in _read(output_path), (
            "ArtifactProvenance is retired; [[DEPLOYED:ArtifactProvenance]] must not appear in output."
        )

    def test_new_deployed_close_tag_present(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """ArtifactProvenance is retired; its deployed close tag must NOT appear in the output."""
        _, output_path = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        assert _TARGET_DEPLOYED_CLOSE not in _read(output_path), (
            "ArtifactProvenance is retired; [[/DEPLOYED:ArtifactProvenance]] must not appear in output."
        )

    def test_sibling_injection_open_tag_present(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """The sibling [[INJECTION:ArtifactProvenanceExtension]] open tag must be in the output."""
        _, output_path = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        assert _TARGET_INJECTION_OPEN in _read(output_path)

    def test_sibling_injection_close_tag_present(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """The sibling [[/INJECTION:ArtifactProvenanceExtension]] close tag must be in the output."""
        _, output_path = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        assert _TARGET_INJECTION_CLOSE in _read(output_path)

    def test_extension_content_preserved_when_empty(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """When the extension injection has no content, the output region must also be empty."""
        _, output_path = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        output_body = _body(_read(output_path))
        carried = _extract_injection_content(output_body)
        assert carried == "", (
            f"Empty extension content must round-trip as empty; got: {carried!r}"
        )

    def test_extension_content_preserved_byte_identically(
        self, provenance_old_shape_nonempty_ext_input, tmp_path
    ):
        """Non-empty extension content must be carried across byte-identically."""
        input_body = _body(_read(provenance_old_shape_nonempty_ext_input))
        _, output_path = _transform_to_tmp(provenance_old_shape_nonempty_ext_input, tmp_path)
        output_body = _body(_read(output_path))

        # Extract the original content from the input's nested injection.
        orig_open = _OLD_SECTION_OPEN
        orig_inj_open = _TARGET_INJECTION_OPEN + "\n"
        orig_inj_close = _TARGET_INJECTION_CLOSE
        sec_start = input_body.find(orig_open)
        assert sec_start != -1, "Precondition: input must contain [[SECTION:ArtifactProvenance]]"
        region = input_body[sec_start:]
        inj_start = region.find(orig_inj_open)
        assert inj_start != -1, "Precondition: input must contain [[INJECTION:ArtifactProvenanceExtension]]"
        content_start = inj_start + len(orig_inj_open)
        inj_end = region.find(orig_inj_close, content_start)
        assert inj_end != -1, "Precondition: extension injection must have a close tag"
        original_ext_content = region[content_start:inj_end]

        carried = _extract_injection_content(output_body)
        assert carried == original_ext_content, (
            f"Extension injection content must be preserved byte-identically.\n"
            f"  Original: {original_ext_content!r}\n"
            f"  Carried:  {carried!r}"
        )

    def test_isolation_body_outside_provenance_region_unchanged(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """Every byte of the body outside the provenance region must be identical to the input.

        Comparison excludes the provenance region and its sibling injection.
        The frontmatter version bump is excluded from the comparison (body only).
        """
        input_content = _read(provenance_old_shape_empty_ext_input)
        _, output_path = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        output_content = _read(output_path)

        input_outside = _body_lines_outside_provenance(_body(input_content))
        output_outside = _body_lines_outside_provenance(_body(output_content))

        assert input_outside == output_outside, (
            "Body content outside the provenance region must be byte-identical to the input.\n"
            f"First differing segment:\n"
            f"  Input:  {input_outside[:3]!r}\n"
            f"  Output: {output_outside[:3]!r}"
        )

    def test_version_bumped(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """Version must be bumped from 3.0.0 to 4.0.0."""
        result, _ = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        assert result.version_before == "3.0.0"
        assert result.version_after == "4.0.0"

    def test_provenance_in_deployed_added_not_sections_added(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """ArtifactProvenance is retired; it must not appear in deployed_added or sections_added."""
        result, _ = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        assert "ArtifactProvenance" not in result.deployed_added, (
            "ArtifactProvenance is retired and must not appear in deployed_added."
        )
        assert "ArtifactProvenance" not in result.sections_added, (
            "ArtifactProvenance must not appear in sections_added."
        )


# ---------------------------------------------------------------------------
# Idempotency and no-region (Contract T-C)
# ---------------------------------------------------------------------------

class TestProvenanceIdempotency:
    """Files already in the new shape or with no provenance region transform without structural change.

    Contract T-C: a body already in the target shape transforms to a byte-identical
    body; a body with no provenance region gains none and is not an error.
    """

    def test_new_shape_transformation_succeeds(
        self, provenance_new_shape_input, tmp_path
    ):
        """Transforming a file already in the new shape must succeed."""
        result, _ = _transform_to_tmp(provenance_new_shape_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_new_shape_body_is_byte_identical(
        self, provenance_new_shape_input, tmp_path
    ):
        """ArtifactProvenance is retired; its deployed tags are stripped from a new-shape input.

        The frontmatter version bump and the deployed-tag removal are the two permitted
        changes; the rest of the body (including the injection sibling) must be identical.
        """
        _, output_path = _transform_to_tmp(provenance_new_shape_input, tmp_path)
        output_body = _body(_read(output_path))
        assert _TARGET_DEPLOYED_OPEN not in output_body, (
            "ArtifactProvenance is retired; [[DEPLOYED:ArtifactProvenance]] must be stripped "
            "even when the input already carried the new-shape deployed region."
        )
        assert _TARGET_INJECTION_OPEN in output_body, (
            "The sibling injection must remain in the output after stripping the deployed tags."
        )

    def test_new_shape_deployed_still_empty(
        self, provenance_new_shape_input, tmp_path
    ):
        """ArtifactProvenance is retired; the deployed region is absent (stripped) in the output."""
        _, output_path = _transform_to_tmp(provenance_new_shape_input, tmp_path)
        output_body = _body(_read(output_path))
        assert _TARGET_DEPLOYED_OPEN not in output_body, (
            "ArtifactProvenance is retired; its deployed region must not appear in the output "
            "even when the input carried the new-shape deployed region."
        )

    def test_new_shape_no_old_section_tag_introduced(
        self, provenance_new_shape_input, tmp_path
    ):
        """A second transformation must not introduce [[SECTION:ArtifactProvenance]] tags."""
        _, output_path = _transform_to_tmp(provenance_new_shape_input, tmp_path)
        assert _OLD_SECTION_OPEN not in _read(output_path)
        assert _OLD_SECTION_CLOSE not in _read(output_path)

    def test_new_shape_exactly_one_deployed_open_tag(
        self, provenance_new_shape_input, tmp_path
    ):
        """ArtifactProvenance is retired; zero [[DEPLOYED:ArtifactProvenance]] tags must appear."""
        _, output_path = _transform_to_tmp(provenance_new_shape_input, tmp_path)
        content = _read(output_path)
        count = content.count(_TARGET_DEPLOYED_OPEN)
        assert count == 0, (
            f"Expected 0 [[DEPLOYED:ArtifactProvenance]] open tags (retired); "
            f"found {count}."
        )

    def test_new_shape_exactly_one_sibling_injection_open_tag(
        self, provenance_new_shape_input, tmp_path
    ):
        """Exactly one [[INJECTION:ArtifactProvenanceExtension]] open tag must appear."""
        _, output_path = _transform_to_tmp(provenance_new_shape_input, tmp_path)
        content = _read(output_path)
        count = content.count(_TARGET_INJECTION_OPEN)
        assert count == 1, (
            f"Expected exactly 1 [[INJECTION:ArtifactProvenanceExtension]] open tag; "
            f"found {count}. Idempotency failure: double-tagging detected."
        )

    def test_no_region_transformation_succeeds(
        self, generic_standard_input, tmp_path
    ):
        """A file with no provenance region at all must transform without error."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_no_region_gains_no_provenance_region(
        self, generic_standard_input, tmp_path
    ):
        """A file with no provenance region must not gain one after transformation."""
        _, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        content = _read(output_path)
        assert _TARGET_DEPLOYED_OPEN not in content, (
            "A file with no provenance region must not gain [[DEPLOYED:ArtifactProvenance]]."
        )
        assert _TARGET_INJECTION_OPEN not in content, (
            "A file with no provenance region must not gain [[INJECTION:ArtifactProvenanceExtension]]."
        )

    def test_no_region_deployed_added_is_empty(
        self, generic_standard_input, tmp_path
    ):
        """deployed_added must be empty for a file with no provenance region."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert result.deployed_added == [], (
            "A file with no provenance region must not report any deployed_added entries."
        )


# ---------------------------------------------------------------------------
# Validator integration (Contract T-D)
# ---------------------------------------------------------------------------

class TestProvenanceValidatorIntegration:
    """Transformer output for each provenance path must pass boundary_validator.py.

    Contract T-D (isolation): this class validates that the transformer's output
    for all four input shapes (untagged, old-shape empty ext, old-shape non-empty
    ext, new-shape, no-region) contains no validator errors.
    """

    def _assert_validates_clean(self, output_path: pathlib.Path) -> None:
        """Assert that validate_file(output_path) returns an empty error list."""
        errors = validate_file(output_path)
        assert errors == [], (
            f"Transformer output must pass boundary_validator.py with no errors.\n"
            f"Errors found in {output_path}:\n"
            + "\n".join(f"  {e}" for e in errors)
        )

    def test_untagged_input_output_validates(
        self, provenance_untagged_input, tmp_path
    ):
        """Transformer output for the untagged '## Artifact Provenance' path must pass the validator."""
        result, output_path = _transform_to_tmp(provenance_untagged_input, tmp_path)
        assert result.success is True, (
            "Precondition: transformation must succeed before validator check."
        )
        self._assert_validates_clean(output_path)

    def test_old_shape_empty_ext_output_validates(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """Transformer output for the old [[SECTION:ArtifactProvenance]] shape (empty ext) must pass."""
        result, output_path = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        assert result.success is True, (
            "Precondition: transformation must succeed before validator check."
        )
        self._assert_validates_clean(output_path)

    def test_old_shape_nonempty_ext_output_validates(
        self, provenance_old_shape_nonempty_ext_input, tmp_path
    ):
        """Transformer output for the old [[SECTION:ArtifactProvenance]] shape (non-empty ext) must pass."""
        result, output_path = _transform_to_tmp(
            provenance_old_shape_nonempty_ext_input, tmp_path
        )
        assert result.success is True, (
            "Precondition: transformation must succeed before validator check."
        )
        self._assert_validates_clean(output_path)

    def test_new_shape_output_validates(
        self, provenance_new_shape_input, tmp_path
    ):
        """Transformer output for the new-shape (idempotency) path must pass the validator."""
        result, output_path = _transform_to_tmp(provenance_new_shape_input, tmp_path)
        assert result.success is True, (
            "Precondition: transformation must succeed before validator check."
        )
        self._assert_validates_clean(output_path)

    def test_no_region_output_validates(
        self, generic_standard_input, tmp_path
    ):
        """Transformer output for a file with no provenance region must pass the validator."""
        result, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        assert result.success is True, (
            "Precondition: transformation must succeed before validator check."
        )
        self._assert_validates_clean(output_path)

    def test_sibling_injection_at_body_top_level_in_output(
        self, provenance_untagged_input, tmp_path
    ):
        """The ArtifactProvenanceExtension injection must be at body top level, not inside a section.

        This is the 'wrong-parent' invariant from the validator: INJECTION_PARENT_MAP maps
        ArtifactProvenanceExtension to "" (top level).  A transformer that emits it nested
        inside [[SECTION:Identity]] or any other section would fail E008.
        """
        _, output_path = _transform_to_tmp(provenance_untagged_input, tmp_path)
        content = _read(output_path)

        # The injection must appear after all section close tags and before any section open tag
        # that follows it — i.e., it must be at body top level.
        injection_pos = content.find(_TARGET_INJECTION_OPEN)
        assert injection_pos != -1, "ArtifactProvenanceExtension must be present in the output."

        # Any [[SECTION:X]] open tag that precedes the injection's position must be
        # closed before the injection (i.e., a [[/SECTION:X]] must exist between it
        # and the injection position).
        import re as _re
        section_open_re = _re.compile(r"\[\[SECTION:[A-Za-z]+\]\]")
        section_close_re = _re.compile(r"\[\[/SECTION:[A-Za-z]+\]\]")
        before_injection = content[:injection_pos]
        opens_before = len(section_open_re.findall(before_injection))
        closes_before = len(section_close_re.findall(before_injection))
        assert opens_before == closes_before, (
            f"ArtifactProvenanceExtension must be at body top level (not inside any section). "
            f"Found {opens_before} section open tags and {closes_before} section close tags "
            f"before the injection — they must balance."
        )


# ---------------------------------------------------------------------------
# Hyphenated frontmatter key (Stage 1, Defect 3)
# ---------------------------------------------------------------------------

class TestHyphenatedFrontmatterKey:
    """A file whose frontmatter contains a hyphenated key (base-version) must transform correctly.

    The current transformer regex omits '-' from the character class, so 'base-version: 1.0.0'
    fails to match and causes an early return with success=False.  After widening the regex
    to r"^([A-Za-z_][A-Za-z0-9_-]*)\\s*:", the key round-trips verbatim and version is still
    correctly bumped.

    These tests are in TDD RED phase: they fail until _FRONTMATTER_KEY_RE is widened.
    """

    def test_transformation_succeeds(
        self, generic_hyphenated_key_input, tmp_path
    ):
        """Transformation of a file with a hyphenated frontmatter key must succeed.

        The current transformer's _FRONTMATTER_KEY_RE rejects 'base-version' as malformed,
        so this test fails until the regex is widened to admit hyphens after the first char.
        """
        result, _ = _transform_to_tmp(generic_hyphenated_key_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_hyphenated_key_preserved_verbatim_in_output(
        self, generic_hyphenated_key_input, tmp_path
    ):
        """The 'base-version' line must appear byte-for-byte in the output frontmatter.

        After widening the regex, the output-writing loop must fall through to
        output_lines.append(raw) for 'base-version' because it does not equal
        'version' or 'transform_version'.  Before the fix, transformation fails
        entirely (success=False), so this assertion is never reached.
        """
        result, output_path = _transform_to_tmp(generic_hyphenated_key_input, tmp_path)
        assert result.success is True, (
            "Precondition: transformation must succeed before checking key preservation"
        )
        content = _read(output_path)
        lines = content.splitlines()
        # Locate frontmatter (between first and second '---')
        in_fm = False
        fm_lines: list[str] = []
        for line in lines:
            if line == "---":
                if not in_fm:
                    in_fm = True
                    continue
                else:
                    break
            if in_fm:
                fm_lines.append(line)
        assert "base-version: 1.0.0" in fm_lines, (
            "The 'base-version: 1.0.0' frontmatter line must appear verbatim in the output. "
            f"Frontmatter lines found: {fm_lines}"
        )

    def test_version_still_rewritten_alongside_hyphenated_key(
        self, generic_hyphenated_key_input, tmp_path
    ):
        """The 'version' value must still be bumped even when a hyphenated key is present.

        This guards the risk that widening the regex causes the output-writing loop to
        mistake 'base-version' for 'version' and rewrite it.  The correct behaviour is:
        - 'version' key: value is bumped from 2.2.0 to 3.0.0
        - 'base-version' key: line is emitted verbatim (value 1.0.0 is unchanged)
        """
        result, _ = _transform_to_tmp(generic_hyphenated_key_input, tmp_path)
        assert result.success is True
        assert result.version_before == "2.2.0"
        assert result.version_after == "3.0.0"

    def test_output_matches_expected_exactly(
        self, generic_hyphenated_key_input, generic_hyphenated_key_expected, tmp_path
    ):
        """The full output file must match the expected fixture byte-for-byte.

        The expected fixture has 'base-version: 1.0.0' preserved verbatim and
        'version: 3.0.0' (bumped from 2.2.0), with all section and injection
        boundaries correctly added.
        """
        _, output_path = _transform_to_tmp(generic_hyphenated_key_input, tmp_path)
        assert _read(output_path) == _read(generic_hyphenated_key_expected)


# ---------------------------------------------------------------------------
# Fenced code block protection (Stage 3)
# ---------------------------------------------------------------------------

class TestFencedMarkersInput:
    """Lines inside a fenced code block must never be converted to boundary tags.

    The input fixture contains a fenced block inside the Capabilities section
    whose content includes old-style [INJECTION: ...] and new-style
    [[INJECTION:...]] marker-like text.  One genuine [INJECTION: language_patterns]
    marker sits outside the fence and must still be converted correctly.

    These tests are in TDD RED phase: they fail until the in_fenced_block guard
    is added to the inner section-processing loop in _transform_generic_body.
    """

    def test_transformation_succeeds(
        self, generic_fenced_markers_input, tmp_path
    ):
        """Transformation of a file with a fenced block must succeed with no errors."""
        result, _ = _transform_to_tmp(generic_fenced_markers_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_output_matches_expected_exactly(
        self, generic_fenced_markers_input, generic_fenced_markers_expected, tmp_path
    ):
        """The full output file must match the expected fixture byte-for-byte.

        The expected fixture has the fenced block content identical to the input
        and the outside marker converted to its boundary tag pair.
        """
        _, output_path = _transform_to_tmp(generic_fenced_markers_input, tmp_path)
        assert _read(output_path) == _read(generic_fenced_markers_expected)

    def test_no_boundary_tags_inside_fence(
        self, generic_fenced_markers_input, tmp_path
    ):
        """Fenced block content must be passed through verbatim — the transformer must not
        emit or inject boundary tags inside a fence.

        The input fixture already contains a new-style [[INJECTION:IdentityExtension]] line
        inside the fenced block as example syntax.  After transformation that line must
        survive unchanged.  The correct invariant is therefore not "no [[ inside a fence"
        but rather "fenced content in the output is byte-for-byte identical to fenced
        content in the input".  If the transformer emits a spurious tag inside the fence,
        the output fenced lines will differ from the input fenced lines and the assertion
        fails.  If the transformer correctly passes the existing [[ line through verbatim,
        both lists will be identical and the assertion passes.
        """
        _, output_path = _transform_to_tmp(generic_fenced_markers_input, tmp_path)

        def _extract_fenced_lines(path: pathlib.Path) -> list[str]:
            in_fence = False
            fenced: list[str] = []
            for line in _read(path).splitlines():
                if line.strip().startswith("```"):
                    in_fence = not in_fence
                    continue
                if in_fence:
                    fenced.append(line)
            return fenced

        input_fenced = _extract_fenced_lines(generic_fenced_markers_input)
        output_fenced = _extract_fenced_lines(output_path)
        assert output_fenced == input_fenced, (
            "Fenced block content changed during transformation.\n"
            f"Input fenced lines:  {input_fenced!r}\n"
            f"Output fenced lines: {output_fenced!r}\n"
            "Lines inside fences must be passed through verbatim — "
            "the transformer must neither add boundary tags nor alter any fenced line."
        )

    def test_fenced_old_style_marker_preserved_verbatim(
        self, generic_fenced_markers_input, tmp_path
    ):
        """The old-style marker inside the fence must survive in the output unchanged.

        [INJECTION: identity_extension] appears inside a fenced block in the input.
        With the bug, the transformer converts it to a [[INJECTION:...]] pair,
        altering the fenced content.  With the fix, it passes through as-is.
        """
        _, output_path = _transform_to_tmp(generic_fenced_markers_input, tmp_path)
        lines = _read(output_path).splitlines()
        in_fence = False
        fenced_lines: list[str] = []
        for line in lines:
            if line.strip().startswith("```"):
                in_fence = not in_fence
                continue
            if in_fence:
                fenced_lines.append(line)
        assert "[INJECTION: identity_extension]" in fenced_lines, (
            "The old-style marker '[INJECTION: identity_extension]' must appear "
            "verbatim inside the fenced block in the output.  If it is absent, "
            "the transformer converted it when it should have left it alone."
        )

    def test_genuine_marker_outside_fence_converted(
        self, generic_fenced_markers_input, tmp_path
    ):
        """The genuine [INJECTION: language_patterns] outside the fence must be converted.

        This assertion confirms that suppressing conversion inside the fence does
        not accidentally suppress conversion of markers that sit outside any fence.
        """
        result, output_path = _transform_to_tmp(generic_fenced_markers_input, tmp_path)
        assert "LanguagePatterns" in result.injections_added, (
            "'LanguagePatterns' must appear in result.injections_added; "
            "the [INJECTION: language_patterns] marker outside the fence was not converted."
        )
        content = _read(output_path)
        assert "[[DEPLOYED:LanguagePatterns]]" in content, (
            "[[DEPLOYED:LanguagePatterns]] must appear in the output; "
            "the genuine marker outside the fence was not converted to a boundary tag."
        )

    def test_fenced_content_does_not_duplicate_injection_names(
        self, generic_fenced_markers_input, tmp_path
    ):
        """Marker-like text inside the fence must not create duplicate injection names in result.

        IdentityExtension is referenced inside the fenced block as an example.
        The transformer must not count that reference as a real marker conversion —
        only the genuine [INJECTION: identity_extension] in the Identity section
        should produce one IdentityExtension entry in injections_added.
        """
        result, _ = _transform_to_tmp(generic_fenced_markers_input, tmp_path)
        identity_ext_count = result.injections_added.count("IdentityExtension")
        assert identity_ext_count == 1, (
            f"Expected exactly 1 'IdentityExtension' in injections_added "
            f"(from the genuine marker in Identity section); got {identity_ext_count}. "
            "Fenced marker-like text must not be counted as a real conversion."
        )

    def test_output_validates_cleanly(
        self, generic_fenced_markers_input, tmp_path
    ):
        """The transformed output must pass validate_file with no errors.

        In particular, boundary tags erroneously emitted inside a fenced block
        by the buggy transformer cause E002 (unmatched closing tag) because the
        validator's own fence guard would see the open tag inside the fence (and
        skip it) while the corresponding close tag lands outside (and is checked).
        After the fix, no tags are emitted inside fences, so no E002 appears.
        """
        result, output_path = _transform_to_tmp(generic_fenced_markers_input, tmp_path)
        assert result.success is True, (
            "Precondition: transformation must succeed before the validator check."
        )
        errors = validate_file(output_path)
        assert errors == [], (
            f"Transformer output must pass boundary_validator.py with no errors.\n"
            f"Errors found in {output_path}:\n"
            + "\n".join(f"  {e}" for e in errors)
        )


# ---------------------------------------------------------------------------
# Outer-loop provenance-region guard (I3.2 coverage)
# ---------------------------------------------------------------------------

class TestFencedProvenanceInput:
    """The outer loop's provenance-region check must not fire while in_fenced_block is True.

    The input fixture has a fenced code block whose opening delimiter appears as
    the last content line inside the Capabilities section and whose closing
    delimiter appears outside that section, in the outer loop territory.

    Because _identify_sections' provenance heading scan is not fence-aware, it
    detects the '## Artifact Provenance' line (which sits between the opening
    and closing delimiters, outside any canonical section's covered range) as
    provenance_region["start_line"].

    When the outer loop reaches that line, in_fenced_block is True — carried
    from the inner section loop that toggled the flag on the opening delimiter.
    Without the I3.2 guard, the outer loop intercepts the provenance region,
    emits a spurious [[INJECTION:ArtifactProvenanceExtension]] pair, skips the
    closing fence delimiter (leaving in_fenced_block True for all subsequent
    sections), and discards the fenced content.

    These tests are in TDD RED phase: they fail until the in_fenced_block guard
    is added to the outer loop in _transform_generic_body before the provenance-
    region interception check.
    """

    def test_no_spurious_provenance_injection(
        self, generic_fenced_provenance_input, tmp_path
    ):
        """ArtifactProvenanceExtension must not appear in injections_added.

        The '## Artifact Provenance' text in the fixture sits inside a fence
        span that the outer loop processes while in_fenced_block is True.
        With the I3.2 guard, provenance-region interception is skipped for that
        line, so no ArtifactProvenanceExtension injection is emitted and the
        text passes through verbatim.  Without the guard, the interception fires
        spuriously and pollutes injections_added.
        """
        result, _ = _transform_to_tmp(generic_fenced_provenance_input, tmp_path)
        assert "ArtifactProvenanceExtension" not in result.injections_added, (
            "'ArtifactProvenanceExtension' must NOT appear in injections_added.\n"
            "The '## Artifact Provenance' line is reached by the outer loop while "
            "in_fenced_block is True; the I3.2 guard must prevent provenance-region "
            "interception in that state.\n"
            f"Got injections_added={result.injections_added!r}"
        )

    def test_fenced_provenance_heading_preserved_verbatim(
        self, generic_fenced_provenance_input, tmp_path
    ):
        """The '## Artifact Provenance' text must appear verbatim in the output.

        Without the I3.2 guard, provenance-region interception consumes the
        line (replacing it with [[INJECTION:ArtifactProvenanceExtension]] tags)
        and skips the closing fence delimiter.  With the fix, the outer loop's
        else branch appends the line verbatim.
        """
        _, output_path = _transform_to_tmp(generic_fenced_provenance_input, tmp_path)
        content = _read(output_path)
        assert "## Artifact Provenance" in content, (
            "The literal text '## Artifact Provenance' must appear verbatim in "
            "the output.\n"
            "Without the I3.2 guard, the provenance-region interception replaces "
            "it with [[INJECTION:ArtifactProvenanceExtension]] tags and discards "
            "the fenced content entirely."
        )

    def test_output_matches_expected_exactly(
        self, generic_fenced_provenance_input, generic_fenced_provenance_expected,
        tmp_path
    ):
        """The full output file must match the expected fixture byte-for-byte.

        The expected fixture has the '## Artifact Provenance' text and its closing
        fence delimiter verbatim, and Constraints markers correctly converted to
        boundary tag pairs.  Without the I3.2 guard, the output contains a spurious
        ArtifactProvenanceExtension injection and unconverted Constraints markers.
        """
        _, output_path = _transform_to_tmp(generic_fenced_provenance_input, tmp_path)
        assert _read(output_path) == _read(generic_fenced_provenance_expected)


# ---------------------------------------------------------------------------
# CommunicationProtocol region (Stage 4)
# ---------------------------------------------------------------------------

def _count_section_opens_before(content: str, pos: int) -> int:
    """Return the number of [[SECTION:...]] open tags appearing before byte offset pos."""
    return len(re.findall(r"\[\[SECTION:[A-Za-z]+\]\]", content[:pos]))


def _count_section_closes_before(content: str, pos: int) -> int:
    """Return the number of [[/SECTION:...]] close tags appearing before byte offset pos."""
    return len(re.findall(r"\[\[/SECTION:[A-Za-z]+\]\]", content[:pos]))


class TestCommunicationProtocolInput:
    """Old-format '## Communication Protocol' heading is lifted to a top-level DEPLOYED boundary.

    The input carries prose plus both [INJECTION: identity_extension] and
    [INJECTION: protocol_extension] inside the '## Communication Protocol' region.
    After transformation:
    - [[DEPLOYED:CommunicationProtocol]] / [[/DEPLOYED:CommunicationProtocol]] appears at
      top level between [[/SECTION:Identity]] and [[SECTION:Capabilities]].
    - IdentityExtension is relocated inside [[SECTION:Identity]], at the end of the Identity
      body, preceded by one blank line.
    - ProtocolExtension is emitted as an empty top-level [[INJECTION:ProtocolExtension]] pair
      immediately after the deployed block.
    - Old prose is discarded; no untagged leftover text appears.

    These tests are in TDD RED phase: they fail until the CommunicationProtocol region
    handling is implemented in boundary_transformer.py.
    """

    def test_transformation_succeeds(
        self, communication_protocol_input, tmp_path
    ):
        """Transformation of a file with '## Communication Protocol' must succeed with no errors."""
        result, _ = _transform_to_tmp(communication_protocol_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_output_matches_expected_exactly(
        self, communication_protocol_input, communication_protocol_expected, tmp_path
    ):
        """The full output file must match the expected fixture byte-for-byte.

        The expected fixture is authored from the CommunicationProtocol emission contract,
        not derived from tool output, making this assertion an independent correctness oracle.
        """
        _, output_path = _transform_to_tmp(communication_protocol_input, tmp_path)
        assert _read(output_path) == _read(communication_protocol_expected)

    def test_communication_protocol_in_deployed_added(
        self, communication_protocol_input, tmp_path
    ):
        """'CommunicationProtocol' must appear in TransformResult.deployed_added."""
        result, _ = _transform_to_tmp(communication_protocol_input, tmp_path)
        assert "CommunicationProtocol" in result.deployed_added, (
            "TransformResult.deployed_added must contain 'CommunicationProtocol' when a "
            "'## Communication Protocol' region is detected and converted."
        )

    def test_deployed_boundary_not_nested_in_any_section(
        self, communication_protocol_input, tmp_path
    ):
        """[[DEPLOYED:CommunicationProtocol]] must be at body top level, not inside any [[SECTION:]].

        The structural invariant: at the byte offset of [[DEPLOYED:CommunicationProtocol]],
        the number of [[SECTION:...]] open tags must equal the number of [[/SECTION:...]]
        close tags that precede it — i.e. the nesting depth is zero.
        """
        _, output_path = _transform_to_tmp(communication_protocol_input, tmp_path)
        content = _read(output_path)
        deployed_pos = content.find("[[DEPLOYED:CommunicationProtocol]]")
        assert deployed_pos != -1, "[[DEPLOYED:CommunicationProtocol]] must be present in the output."
        opens_before = _count_section_opens_before(content, deployed_pos)
        closes_before = _count_section_closes_before(content, deployed_pos)
        assert opens_before == closes_before, (
            f"[[DEPLOYED:CommunicationProtocol]] is nested inside a [[SECTION:...]] block. "
            f"Found {opens_before} section open tags and {closes_before} section close tags "
            f"before it — they must balance for top-level placement."
        )

    def test_deployed_boundary_between_identity_and_capabilities(
        self, communication_protocol_input, tmp_path
    ):
        """[[DEPLOYED:CommunicationProtocol]] must appear after [[/SECTION:Identity]] and before
        [[SECTION:Capabilities]], satisfying CANONICAL_ORDER slot 1.
        """
        _, output_path = _transform_to_tmp(communication_protocol_input, tmp_path)
        content = _read(output_path)
        identity_close_pos = content.find("[[/SECTION:Identity]]")
        deployed_pos = content.find("[[DEPLOYED:CommunicationProtocol]]")
        capabilities_open_pos = content.find("[[SECTION:Capabilities]]")
        assert identity_close_pos != -1, "[[/SECTION:Identity]] must be present."
        assert deployed_pos != -1, "[[DEPLOYED:CommunicationProtocol]] must be present."
        assert capabilities_open_pos != -1, "[[SECTION:Capabilities]] must be present."
        assert identity_close_pos < deployed_pos < capabilities_open_pos, (
            "[[DEPLOYED:CommunicationProtocol]] must appear after [[/SECTION:Identity]] "
            "and before [[SECTION:Capabilities]]."
        )

    def test_old_prose_not_in_output(
        self, communication_protocol_input, tmp_path
    ):
        """Old '## Communication Protocol' prose body must be discarded.

        The prose text from the region must not appear as untagged leftover text in the output.
        Untagged leftovers would be reported by orphan detection and by the validator.
        """
        _, output_path = _transform_to_tmp(communication_protocol_input, tmp_path)
        content = _read(output_path)
        assert "You operate under **Communication Protocol v1.0**" not in content, (
            "Old Communication Protocol prose must be discarded; it must not appear as "
            "untagged text in the output."
        )
        assert "Additional prose that gets discarded during migration." not in content, (
            "Old Communication Protocol prose must be discarded; it must not appear as "
            "untagged text in the output."
        )

    def test_old_heading_not_in_output_as_untagged_text(
        self, communication_protocol_input, tmp_path
    ):
        """The raw '## Communication Protocol' heading line must not survive as bare Markdown.

        After transformation, the heading is replaced by [[DEPLOYED:CommunicationProtocol]].
        Its appearance as a literal Markdown heading in the output would indicate the region
        was not intercepted and its prose was left as unclassifiable content.
        """
        _, output_path = _transform_to_tmp(communication_protocol_input, tmp_path)
        content = _read(output_path)
        # The heading must not appear outside a fenced block
        lines = content.splitlines()
        in_fence = False
        for line in lines:
            if line.strip().startswith("```"):
                in_fence = not in_fence
                continue
            if not in_fence:
                assert line != "## Communication Protocol", (
                    "The '## Communication Protocol' heading must not appear as a bare Markdown "
                    "heading in the output — it should have been replaced by the DEPLOYED boundary."
                )

    def test_output_validates_cleanly(
        self, communication_protocol_input, tmp_path
    ):
        """Transformer output must pass validate_file with no errors, specifically no E008.

        E008 fires when a DEPLOYED boundary is nested inside a section rather than at top level.
        A correct implementation places [[DEPLOYED:CommunicationProtocol]] at body top level
        (after [[/SECTION:Identity]]), so E008 must not appear.
        E007 (canonical order) is also covered: CommunicationProtocol is at CANONICAL_ORDER[1],
        and the emission contract places it after Identity and before Capabilities, satisfying
        the order check.
        """
        result, output_path = _transform_to_tmp(communication_protocol_input, tmp_path)
        assert result.success is True, (
            "Precondition: transformation must succeed before the validator check."
        )
        errors = validate_file(output_path)
        assert errors == [], (
            f"Transformer output must pass boundary_validator.py with no errors.\n"
            f"Errors found in {output_path}:\n"
            + "\n".join(f"  {e}" for e in errors)
        )

    def test_protocol_extension_pair_is_empty(
        self, communication_protocol_input, tmp_path
    ):
        """[[INJECTION:ProtocolExtension]] open tag must be immediately followed by its close tag.

        The ProtocolExtension pair is contractually empty: [INJECTION: protocol_extension] is
        an old-style single-line sentinel with no wrapped body.  The open tag and close tag
        must be adjacent lines with nothing between them.
        """
        _, output_path = _transform_to_tmp(communication_protocol_input, tmp_path)
        lines = _read(output_path).splitlines()
        for i, line in enumerate(lines):
            if line == "[[INJECTION:ProtocolExtension]]":
                assert i + 1 < len(lines) and lines[i + 1] == "[[/INJECTION:ProtocolExtension]]", (
                    "[[INJECTION:ProtocolExtension]] open tag must be immediately followed by "
                    "[[/INJECTION:ProtocolExtension]] close tag with nothing between them. "
                    f"Line after open tag: {lines[i + 1]!r}"
                )
                break
        else:
            pytest.fail(
                "[[INJECTION:ProtocolExtension]] not found in output. "
                "The [INJECTION: protocol_extension] marker must be preserved as a tagged "
                "top-level region rather than discarded with the surrounding prose."
            )

    def test_protocol_extension_not_nested_in_any_section(
        self, communication_protocol_input, tmp_path
    ):
        """[[INJECTION:ProtocolExtension]] must be at body top level, not inside any [[SECTION:]].

        INJECTION_PARENT_MAP maps ProtocolExtension to None (top level). Emitting it inside
        a section would violate the parent contract and cause the validator to report E008.
        """
        _, output_path = _transform_to_tmp(communication_protocol_input, tmp_path)
        content = _read(output_path)
        ext_pos = content.find("[[INJECTION:ProtocolExtension]]")
        assert ext_pos != -1, "[[INJECTION:ProtocolExtension]] must be present in the output."
        opens_before = _count_section_opens_before(content, ext_pos)
        closes_before = _count_section_closes_before(content, ext_pos)
        assert opens_before == closes_before, (
            f"[[INJECTION:ProtocolExtension]] is nested inside a [[SECTION:...]] block. "
            f"Found {opens_before} section open tags and {closes_before} section close tags "
            f"before it — they must balance for top-level placement."
        )

    def test_protocol_extension_in_injections_added(
        self, communication_protocol_input, tmp_path
    ):
        """'ProtocolExtension' must appear in TransformResult.injections_added."""
        result, _ = _transform_to_tmp(communication_protocol_input, tmp_path)
        assert "ProtocolExtension" in result.injections_added, (
            "TransformResult.injections_added must contain 'ProtocolExtension' when a "
            "[INJECTION: protocol_extension] marker is found in the Communication Protocol region."
        )

    def test_identity_extension_emitted_inside_identity_section(
        self, communication_protocol_input, tmp_path
    ):
        """[[INJECTION:IdentityExtension]] relocated from the CP region must appear inside
        [[SECTION:Identity]], not at top level or inside a different section.

        The marker's source line lies past Identity's end_line, so in-place emission is
        impossible.  The transformer must relocate it to the end of the Identity body,
        immediately before [[/SECTION:Identity]], preceded by one blank line.
        """
        _, output_path = _transform_to_tmp(communication_protocol_input, tmp_path)
        content = _read(output_path)
        identity_open_pos = content.find("[[SECTION:Identity]]")
        identity_close_pos = content.find("[[/SECTION:Identity]]")
        ext_pos = content.find("[[INJECTION:IdentityExtension]]")
        assert identity_open_pos != -1, "[[SECTION:Identity]] must be present."
        assert identity_close_pos != -1, "[[/SECTION:Identity]] must be present."
        assert ext_pos != -1, "[[INJECTION:IdentityExtension]] must be present in the output."
        assert identity_open_pos < ext_pos < identity_close_pos, (
            "[[INJECTION:IdentityExtension]] must appear between [[SECTION:Identity]] and "
            "[[/SECTION:Identity]], i.e. inside the Identity section body."
        )

    def test_identity_extension_in_injections_added(
        self, communication_protocol_input, tmp_path
    ):
        """'IdentityExtension' must appear in TransformResult.injections_added."""
        result, _ = _transform_to_tmp(communication_protocol_input, tmp_path)
        assert "IdentityExtension" in result.injections_added, (
            "TransformResult.injections_added must contain 'IdentityExtension' when a "
            "[INJECTION: identity_extension] marker is found in the Communication Protocol region."
        )

    def test_all_canonical_sections_added(
        self, communication_protocol_input, tmp_path
    ):
        """All six canonical sections must be added despite the Communication Protocol region removal."""
        result, _ = _transform_to_tmp(communication_protocol_input, tmp_path)
        for name in ("Identity", "Capabilities", "Constraints",
                     "ErrorHandling", "OutputFormat", "ExecutionPhilosophy"):
            assert name in result.sections_added, (
                f"Section '{name}' missing from sections_added; Communication Protocol region "
                "handling must not affect other section boundaries."
            )

    def test_version_bumped(
        self, communication_protocol_input, tmp_path
    ):
        """Version must be bumped from 2.2.0 to 3.0.0."""
        result, _ = _transform_to_tmp(communication_protocol_input, tmp_path)
        assert result.version_before == "2.2.0"
        assert result.version_after == "3.0.0"


# ---------------------------------------------------------------------------
# Fence-detection guard for Communication Protocol region (Stage 4, T4.7/T4.8)
# ---------------------------------------------------------------------------

class TestFencedProtocolHeadingInput:
    """A fenced '## Communication Protocol' line outside every section range must not be detected.

    The input fixture places a fenced code block containing a literal
    '## Communication Protocol' line in the outer loop territory (between
    Identity's closing separator and the next canonical H2 heading).  This position
    is outside every canonical section's line range, so covered[] cannot mask the
    gap — the new scan must carry its own fence tracking to suppress detection.

    A genuine '## Communication Protocol' region follows the fenced block and must
    be detected and converted exactly once.

    These tests are in TDD RED phase: they fail until the fence-aware detection scan
    is implemented in _identify_sections.
    """

    def test_transformation_succeeds(
        self, fenced_protocol_heading_input, tmp_path
    ):
        """Transformation must succeed with no errors.

        A misdetection of the fenced '## Communication Protocol' line as a region would
        cause the outer loop to intercept a fenced line while in_fenced_block=True, producing
        a malformed output (the fence is never closed, desynchronising in_fenced_block for
        all subsequent sections).  success=False or non-empty errors would indicate the bug.
        """
        result, _ = _transform_to_tmp(fenced_protocol_heading_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_output_matches_expected_exactly(
        self, fenced_protocol_heading_input, fenced_protocol_heading_expected, tmp_path
    ):
        """The full output file must match the expected fixture byte-for-byte.

        The expected fixture has the fenced content identical to the input (the '## Communication
        Protocol' line inside the fence is verbatim) and the genuine region transformed per the
        emission contract.  A fence-blind scan would produce a different output (spurious
        DEPLOYED block from the fenced line, missing DEPLOYED block for the genuine region),
        failing this assertion.
        """
        _, output_path = _transform_to_tmp(fenced_protocol_heading_input, tmp_path)
        assert _read(output_path) == _read(fenced_protocol_heading_expected)

    def test_exactly_one_communication_protocol_in_deployed_added(
        self, fenced_protocol_heading_input, tmp_path
    ):
        """TransformResult.deployed_added must contain exactly one 'CommunicationProtocol' entry.

        The fenced '## Communication Protocol' line must not produce a second entry.  A
        fence-blind detection scan would detect both the fenced and the genuine heading,
        producing two entries and potentially doubling the emitted DEPLOYED blocks.
        """
        result, _ = _transform_to_tmp(fenced_protocol_heading_input, tmp_path)
        count = result.deployed_added.count("CommunicationProtocol")
        assert count == 1, (
            f"Expected exactly one 'CommunicationProtocol' entry in deployed_added; "
            f"got {count}.  The fenced line must not be detected as a region — only the "
            f"genuine '## Communication Protocol' heading should produce an entry."
        )

    def test_fenced_content_preserved_verbatim(
        self, fenced_protocol_heading_input, tmp_path
    ):
        """The '## Communication Protocol' text inside the fence must appear verbatim in the output.

        If the fenced line is misdetected as a region start, the outer loop intercepts it,
        discards its content, and emits [[DEPLOYED:CommunicationProtocol]] instead.  The
        fenced line would then be absent from the output, failing this assertion.
        """
        _, output_path = _transform_to_tmp(fenced_protocol_heading_input, tmp_path)

        def _extract_fenced_lines(path: pathlib.Path) -> list[str]:
            in_fence = False
            fenced: list[str] = []
            for line in _read(path).splitlines():
                if line.strip().startswith("```"):
                    in_fence = not in_fence
                    continue
                if in_fence:
                    fenced.append(line)
            return fenced

        input_fenced = _extract_fenced_lines(fenced_protocol_heading_input)
        output_fenced = _extract_fenced_lines(output_path)
        assert output_fenced == input_fenced, (
            "Fenced block content changed during transformation.\n"
            f"Input fenced lines:  {input_fenced!r}\n"
            f"Output fenced lines: {output_fenced!r}\n"
            "The '## Communication Protocol' line inside the fence must pass through verbatim."
        )

    def test_genuine_region_converted(
        self, fenced_protocol_heading_input, tmp_path
    ):
        """The genuine '## Communication Protocol' region must be converted to a DEPLOYED boundary.

        Suppressing the fenced heading must not accidentally suppress detection of the
        genuine region that follows it.
        """
        result, output_path = _transform_to_tmp(fenced_protocol_heading_input, tmp_path)
        assert "CommunicationProtocol" in result.deployed_added, (
            "The genuine '## Communication Protocol' region must be detected and its name "
            "recorded in deployed_added."
        )
        content = _read(output_path)
        assert "[[DEPLOYED:CommunicationProtocol]]" in content, (
            "[[DEPLOYED:CommunicationProtocol]] must appear in the output from the genuine region."
        )

    def test_output_validates_cleanly(
        self, fenced_protocol_heading_input, tmp_path
    ):
        """Transformer output for the fenced fixture must pass validate_file with no errors.

        Misdetecting the fenced heading would produce a malformed fence span (the fenced
        content is consumed and the fence's closing delimiter is skipped), causing E002
        (unmatched closing tag) or E008 (wrong-parent DEPLOYED) in the validator output.
        After the fix, no such errors appear.
        """
        result, output_path = _transform_to_tmp(fenced_protocol_heading_input, tmp_path)
        assert result.success is True, (
            "Precondition: transformation must succeed before the validator check."
        )
        errors = validate_file(output_path)
        assert errors == [], (
            f"Transformer output must pass boundary_validator.py with no errors.\n"
            f"Errors found in {output_path}:\n"
            + "\n".join(f"  {e}" for e in errors)
        )
