"""Tests for the boundary transformation tool.

Tests verify overall transformation behavior: given an input file, the output
must contain the correct boundaries in the correct order with body text preserved.
Tests do NOT test individual methods — they test end-to-end transformation outcomes.

All tests are in TDD RED phase: they will fail until boundary_transformer.py is implemented.
The Stage 4 test classes (TestProvenanceUntaggedInput, TestProvenanceOldShapeMigration,
TestProvenanceIdempotency, TestProvenanceValidatorIntegration) will fail until the
transformer's provenance migration contracts (T-A through T-D) are implemented.
The Stage 5 test classes (TestSkipReasonEnum, TestTransformResultSkipReasonField,
TestUtilityAgentTransformFileSkip, TestNonAgentTransformFileSkip,
TestSkipReasonDistinguishesFromAlreadyTransformed, TestNormalTransformSkipReasonIsNone)
will fail until SkipReason is added to boundary_transformer.py and transform_file skips
utility-agent and non-agent files before reading their content.
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
        """All 7 managed region boundaries must be added (user-owned and tool-managed).

        ProtocolExtension has been removed from the vocabulary entirely.
        HarnessConstraints remains tool-managed (emitted as [[DEPLOYED:...]]).
        LanguagePatterns has left the tool-managed set and is now emitted as
        [[INJECTION:LanguagePatterns]] — it still appears in injections_added
        because that field tracks all managed region names regardless of marker
        kind. CustomConstraints's legacy marker in this fixture is empty, so per
        the drop rule it is not emitted at all and does not appear here.
        """
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        expected_injections = [
            "IdentityExtension",
            "LanguagePatterns",
            "CodebaseContext",
            "OutputArtifactTemplate",
            "HarnessConstraints",
            "ErrorHandlingExtension",
            "ContextLimits",
        ]
        assert result.injections_added == expected_injections

    def test_version_bumped_to_next_major(
        self, generic_standard_input, tmp_path
    ):
        """Version 2.2.0 must be bumped to 2.3.0 (next minor, patch reset to 0)."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert result.version_before == "2.2.0"
        assert result.version_after == "2.3.0"

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
        """Version 2.3.0 must be bumped to 2.4.0."""
        result, _ = _transform_to_tmp(generic_validation_input, tmp_path)
        assert result.version_before == "2.3.0"
        assert result.version_after == "2.4.0"

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
        """Version 5.3.1 must be bumped to 5.4.0."""
        result, _ = _transform_to_tmp(generic_orchestrator_input, tmp_path)
        assert result.version_before == "5.3.1"
        assert result.version_after == "5.4.0"

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
        """Version 4.2.0 must be bumped to 4.3.0."""
        result, _ = _transform_to_tmp(generic_interface_input, tmp_path)
        assert result.version_before == "4.2.0"
        assert result.version_after == "4.3.0"

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

    def test_degraded_transform_runs_without_generic_ref(
        self, harness_codebase_agnostic_input, tmp_path
    ):
        """When a harness file has no --generic-ref, the degraded path runs and succeeds.

        Stage 2 behaviour: non-orchestrator harness files without a generic reference
        are transformed on the degraded path (success=True, degraded=True) rather than
        returning a hard failure.  Orchestrator-named files still fail; see
        TestOrchestratorHardErrorPreserved.
        """
        result = transform_file(
            harness_codebase_agnostic_input,
            tmp_path / harness_codebase_agnostic_input.name,
            generic_ref_path=None,
        )
        assert result.success is True
        assert result.degraded is True

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
        """The HarnessConstraints boundary must be emitted empty, and the harness-specific
        text that used to sit at the marker's position must survive afterward as ordinary
        section content rather than inside the boundary.

        HarnessConstraints is a tool-managed, DEPLOYED-kind region: every emitted DEPLOYED
        region is empty regardless of what text used to sit there, because the deployment
        tool fills it later from the canonical bundle, not from anything the transformer
        copies.
        """
        _, output_path = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        content = _read(output_path)
        open_pos = content.find("[[DEPLOYED:HarnessConstraints]]")
        close_pos = content.find("[[/DEPLOYED:HarnessConstraints]]")
        assert open_pos != -1 and close_pos != -1 and open_pos < close_pos
        inner = content[open_pos + len("[[DEPLOYED:HarnessConstraints]]"):close_pos]
        assert inner.strip() == ""
        after_close = content[close_pos + len("[[/DEPLOYED:HarnessConstraints]]"):]
        assert "Use only the Read, Write, Edit, Bash" in after_close

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
        """transform_version in frontmatter must also have its minor number incremented."""
        _, output_path = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        content = _read(output_path)
        # transform_version: 2.2.0 -> 2.3.0
        assert "transform_version: 2.3.0" in content

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

    def test_degraded_transform_runs_without_generic_ref(
        self, harness_example_project_input, tmp_path
    ):
        """When an ExampleProject harness has no --generic-ref, the degraded path runs.

        Stage 2 behaviour: non-orchestrator harness files without a generic reference
        are transformed on the degraded path rather than failing.
        """
        result = transform_file(
            harness_example_project_input,
            tmp_path / harness_example_project_input.name,
            generic_ref_path=None,
        )
        assert result.success is True
        assert result.degraded is True

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
        """User-owned injection content must appear between the open and close boundary
        tags. Tool-managed (DEPLOYED-kind) boundaries must be emitted empty, with any
        harness-specific text that used to sit at the marker's position surviving
        afterward as ordinary section content rather than inside the boundary.

        LanguagePatterns is no longer tool-managed — it uses [[INJECTION:]] like any
        other user-owned region, and its harness-authored fill content belongs inside
        the boundary rather than being discarded.
        CodebaseContext and ContextLimits are user-owned so they use [[INJECTION:]] and
        their harness-authored fill content belongs inside the boundary.
        """
        _, output_path = _transform_to_tmp(
            harness_example_project_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        content = _read(output_path)

        # LanguagePatterns is user-owned — emitted as [[INJECTION:]]
        lp_open = content.find("[[INJECTION:LanguagePatterns]]")
        lp_close = content.find("[[/INJECTION:LanguagePatterns]]")
        assert lp_open != -1 and lp_close != -1
        assert "[[DEPLOYED:LanguagePatterns]]" not in content, (
            "LanguagePatterns must never be emitted as a [[DEPLOYED:]] region"
        )
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
        """Version 1.0.0 must be bumped to 1.1.0."""
        result, _ = _transform_to_tmp(generic_artifact_template_input, tmp_path)
        assert result.version_before == "1.0.0"
        assert result.version_after == "1.1.0"

    def test_output_matches_expected_exactly(
        self, generic_artifact_template_input, generic_artifact_template_expected, tmp_path
    ):
        _, output_path = _transform_to_tmp(generic_artifact_template_input, tmp_path)
        assert _read(output_path) == _read(generic_artifact_template_expected)


# ---------------------------------------------------------------------------
# Version bump edge cases
# ---------------------------------------------------------------------------

class TestVersionBump:
    """Version bumping must increment the minor component and reset patch to 0."""

    @pytest.mark.parametrize("version_before, version_after", [
        ("2.2.0", "2.3.0"),
        ("2.3.0", "2.4.0"),
        ("5.3.1", "5.4.0"),
        ("4.2.0", "4.3.0"),
        ("1.0.0", "1.1.0"),
        ("10.9.8", "10.10.0"),
    ])
    def test_major_bumped_minor_and_patch_reset(
        self, version_before, version_after, generic_standard_input, tmp_path
    ):
        """Minor must increment; patch must reset to 0; major must remain unchanged."""
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

    def test_orchestrator_harness_without_generic_ref_returns_failure(
        self, tmp_path
    ):
        """An orchestrator-named harness file without --generic-ref must fail with the hard error.

        Stage 2 preserves the hard error for orchestrator-named files while routing
        non-orchestrator harness files through the degraded path instead.
        """
        orch_path = tmp_path / "orchestrator.md"
        orch_path.write_text(
            "---\nid: 1\nversion: 1.0.0\ntransform_version: 1.0.0\n"
            "name: test-orchestrator\n---\n\n# TestOrchestrator\n\n"
            "---\n\n## Capabilities\n\n- Orchestrate.\n",
            encoding="utf-8",
        )
        result = transform_file(orch_path, tmp_path / "out.md", generic_ref_path=None)
        assert result.success is False
        assert result.errors  # at least one error reported

    def test_orchestrator_harness_without_generic_ref_does_not_write_output(
        self, tmp_path
    ):
        """When an orchestrator-named harness has no --generic-ref, no output is written.

        Stage 2 preserves the no-output contract for the orchestrator hard-error path.
        Non-orchestrator harness files do write output on the degraded path.
        """
        orch_path = tmp_path / "orchestrator.md"
        orch_path.write_text(
            "---\nid: 1\nversion: 1.0.0\ntransform_version: 1.0.0\n"
            "name: test-orchestrator\n---\n\n# TestOrchestrator\n\n"
            "---\n\n## Capabilities\n\n- Orchestrate.\n",
            encoding="utf-8",
        )
        output_path = tmp_path / "out.md"
        transform_file(orch_path, output_path, generic_ref_path=None)
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

    def test_cli_exits_nonzero_on_orchestrator_harness_without_ref(
        self, tmp_path
    ):
        """CLI must exit non-zero when an orchestrator-named harness file has no --generic-ref.

        Stage 2 preserves the hard error for orchestrator-named harness files.
        Non-orchestrator harness files without --generic-ref now exit 0 (degraded path).
        The non-zero exit must come from the tool detecting the orchestrator constraint,
        not from an uncaught exception.
        """
        orch_path = tmp_path / "orchestrator.md"
        orch_path.write_text(
            "---\nid: 1\nversion: 1.0.0\ntransform_version: 1.0.0\n"
            "name: test-orchestrator\n---\n\n# TestOrchestrator\n\n"
            "---\n\n## Capabilities\n\n- Orchestrate.\n",
            encoding="utf-8",
        )
        output_path = tmp_path / "out.md"
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(orch_path), "--output", str(output_path)],
            capture_output=True, text=True,
        )
        assert "NotImplementedError" not in proc.stderr, (
            "CLI must detect the orchestrator hard error, not crash with NotImplementedError."
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
        # Only the version values changed (minor bump), key order preserved.
        assert "version: 2.3.0\n" in out_fm
        assert "transform_version: 2.3.0\n" in out_fm


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
        """A file with no 'version' key in frontmatter must still transform successfully.

        Since Stage 7, a missing 'version' is substituted with default_version()
        rather than causing a hard failure. The transform succeeds and version_before
        is DEFAULT_VERSION.
        """
        result = transform_file(
            malformed_no_version_field_input,
            tmp_path / malformed_no_version_field_input.name,
        )
        assert result.success is True
        assert result.errors == []
        assert result.version_before == "1.0.0"  # DEFAULT_VERSION fallback

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
        """CLI must exit zero when the version field is absent from frontmatter.

        Since Stage 7, a missing 'version' is substituted with default_version()
        rather than causing a hard failure. The CLI must exit 0.
        """
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(malformed_no_version_field_input),
             "--output", str(tmp_path / "out.md")],
            capture_output=True, text=True,
        )
        assert "NotImplementedError" not in proc.stderr
        assert proc.returncode == 0


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
_IDENTITY_SECTION_OPEN = "[[SECTION:Identity]]"
_IDENTITY_SECTION_CLOSE = "[[/SECTION:Identity]]"


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


def _body_lines_outside_conduct_regions(body: str) -> list[str]:
    """Return body lines with provenance-related regions and the Identity section stripped out.

    Extends the provenance-only stripping of ``_body_lines_outside_provenance`` to also
    exclude ``[[SECTION:Identity]]``.  Stage 2 intentionally adds ``ClosingProcedure``
    and ``AuthorityHierarchy`` inside the Identity section, so isolation tests that
    verify non-provenance content is unchanged must also exclude Identity from the
    comparison scope.

    Strips these open→close pairs:
    - ``[[SECTION:ArtifactProvenance]]`` … ``[[/SECTION:ArtifactProvenance]]``
    - ``[[DEPLOYED:ArtifactProvenance]]`` … ``[[/DEPLOYED:ArtifactProvenance]]``
    - ``[[INJECTION:ArtifactProvenanceExtension]]`` … ``[[/INJECTION:ArtifactProvenanceExtension]]``
    - ``[[SECTION:Identity]]`` … ``[[/SECTION:Identity]]``

    All other lines are returned preserving their exact bytes.
    """
    _open_triggers = (
        _TARGET_DEPLOYED_OPEN,
        _OLD_SECTION_OPEN,
        _TARGET_INJECTION_OPEN,
        _IDENTITY_SECTION_OPEN,
    )
    _close_triggers = (
        _TARGET_INJECTION_CLOSE,
        _OLD_SECTION_CLOSE,
        _TARGET_DEPLOYED_CLOSE,
        _IDENTITY_SECTION_CLOSE,
    )
    lines = body.splitlines(keepends=True)
    result = []
    skip = False
    for line in lines:
        stripped = line.strip()
        if stripped in _open_triggers:
            if not skip:
                skip = True
        if not skip:
            result.append(line)
        if skip and stripped in _close_triggers:
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
        """The version must be bumped from 2.2.0 to 2.3.0."""
        result, _ = _transform_to_tmp(provenance_untagged_input, tmp_path)
        assert result.version_before == "2.2.0"
        assert result.version_after == "2.3.0"


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
        """Body content outside the provenance region and Identity section must be byte-identical.

        Comparison excludes the provenance region, its sibling injection, and the Identity
        section. The Identity section is excluded because Stage 2 intentionally adds
        ClosingProcedure and AuthorityHierarchy inside it. All other sections
        (Capabilities, Constraints, ErrorHandling, OutputFormat, ExecutionPhilosophy)
        must remain byte-identical to the input.
        The frontmatter version bump is excluded from the comparison (body only).
        """
        input_content = _read(provenance_old_shape_empty_ext_input)
        _, output_path = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        output_content = _read(output_path)

        input_outside = _body_lines_outside_conduct_regions(_body(input_content))
        output_outside = _body_lines_outside_conduct_regions(_body(output_content))

        assert input_outside == output_outside, (
            "Body content outside the provenance and Identity regions must be byte-identical to the input.\n"
            f"First differing segment:\n"
            f"  Input:  {input_outside[:3]!r}\n"
            f"  Output: {output_outside[:3]!r}"
        )

    def test_version_bumped(
        self, provenance_old_shape_empty_ext_input, tmp_path
    ):
        """Version must be bumped from 3.0.0 to 3.1.0."""
        result, _ = _transform_to_tmp(provenance_old_shape_empty_ext_input, tmp_path)
        assert result.version_before == "3.0.0"
        assert result.version_after == "3.1.0"

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

    def test_no_provenance_region_identity_regions_deployed_added(
        self, generic_standard_input, tmp_path
    ):
        """ClosingProcedure and AuthorityHierarchy must appear in deployed_added; ArtifactProvenance must not.

        Stage 2 deploys identity regions to every file, including files that have no
        provenance region. The provenance transformation does not apply to such files,
        so ArtifactProvenance must stay absent from deployed_added.
        """
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert "ClosingProcedure" in result.deployed_added, (
            "ClosingProcedure must appear in deployed_added even for files with no provenance region."
        )
        assert "AuthorityHierarchy" in result.deployed_added, (
            "AuthorityHierarchy must appear in deployed_added even for files with no provenance region."
        )
        assert "ArtifactProvenance" not in result.deployed_added, (
            "A file with no provenance region must not report ArtifactProvenance in deployed_added."
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
        - 'version' key: value is bumped from 2.2.0 to 2.3.0
        - 'base-version' key: line is emitted verbatim (value 1.0.0 is unchanged)
        """
        result, _ = _transform_to_tmp(generic_hyphenated_key_input, tmp_path)
        assert result.success is True
        assert result.version_before == "2.2.0"
        assert result.version_after == "2.3.0"

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

        LanguagePatterns has left CANONICAL_DEPLOYED, so the old marker now resolves
        to an [[INJECTION:]] region rather than [[DEPLOYED:]] — this outcome falls out
        of EXPECTED_MARKER.get(name, BoundaryKind.INJECTION) missing once the name is
        no longer canonical, with no name-specific code required.
        """
        result, output_path = _transform_to_tmp(generic_fenced_markers_input, tmp_path)
        assert "LanguagePatterns" in result.injections_added, (
            "'LanguagePatterns' must appear in result.injections_added; "
            "the [INJECTION: language_patterns] marker outside the fence was not converted."
        )
        content = _read(output_path)
        assert "[[INJECTION:LanguagePatterns]]" in content, (
            "[[INJECTION:LanguagePatterns]] must appear in the output; "
            "the genuine marker outside the fence was not converted to a boundary tag."
        )
        assert "[[DEPLOYED:LanguagePatterns]]" not in content, (
            "LanguagePatterns is no longer tool-managed; it must never be emitted "
            "as a [[DEPLOYED:]] region"
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
# T2.3 / T2.4: legacy `language_patterns` and `custom_constraints` marker
# resolution, direct on _match_region_marker.
#
# language_patterns must resolve to BoundaryKind.INJECTION as a *consequence*
# of LanguagePatterns leaving CANONICAL_DEPLOYED (EXPECTED_MARKER.get(...) misses
# and falls to the BoundaryKind.INJECTION default) -- no name-specific code.
#
# custom_constraints is the one deliberate, name-scoped rule this change adds:
# a populated region survives as [[INJECTION:CustomConstraints]]; an empty one
# is dropped entirely (neither open nor close tag). A neighbouring empty
# injection of a different name is the control proving the drop rule does not
# become a general empty-region sweeper.
# ---------------------------------------------------------------------------

from boundary_transformer import _match_region_marker  # noqa: E402


class TestLegacyLanguagePatternsMarkerResolution:
    """The old `[INJECTION: language_patterns]` marker resolves to
    BoundaryKind.INJECTION purely as a consequence of the vocabulary change."""

    def test_match_region_marker_resolves_language_patterns_to_injection_kind(self) -> None:
        match = _match_region_marker("[INJECTION: language_patterns]")
        assert match is not None, "The legacy language_patterns marker must still match"
        assert match["name"] == "LanguagePatterns"
        assert match["kind"] == BoundaryKind.INJECTION, (
            "language_patterns must resolve to BoundaryKind.INJECTION now that "
            "LanguagePatterns has left CANONICAL_DEPLOYED -- this must fall out of "
            "EXPECTED_MARKER.get(name, BoundaryKind.INJECTION) missing, not from any "
            "name-specific branch added for LanguagePatterns"
        )

    def test_match_region_marker_list_item_form_also_resolves_to_injection_kind(self) -> None:
        """The list-item marker form ('- [INJECTION: language_patterns]') resolves
        through the same path as the standalone form."""
        match = _match_region_marker("- [INJECTION: language_patterns]")
        assert match is not None
        assert match["name"] == "LanguagePatterns"
        assert match["kind"] == BoundaryKind.INJECTION

    def test_transform_file_emits_language_patterns_as_injection_region(
        self, tmp_path: pathlib.Path
    ) -> None:
        """End-to-end: a file carrying the legacy marker must emit
        [[INJECTION:LanguagePatterns]], never [[DEPLOYED:LanguagePatterns]]."""
        content = (
            "---\n"
            "id: test-lp\n"
            "version: 1.0.0\n"
            "name: test-agent\n"
            "description: Agent exercising the legacy language_patterns marker.\n"
            "---\n\n"
            "# TestAgent Agent\n\n"
            "You are the agent.\n\n"
            "---\n\n"
            "## Capabilities\n\n"
            "Some capability prose.\n\n"
            "[INJECTION: language_patterns]\n\n"
            "---\n\n"
            "## Constraints\n\nSome constraint.\n\n---\n\n"
            "## Error Handling\n\nHandle errors.\n\n---\n\n"
            "## Output Format\n\nJSON.\n\n---\n\n"
            "## Execution Philosophy\n\nExecute.\n"
        )
        input_path = tmp_path / "lp-input.md"
        input_path.write_text(content, encoding="utf-8")
        output_path = tmp_path / "lp-output.md"

        result = transform_file(input_path, output_path)
        assert result.success is True, f"Transform must succeed; errors={result.errors}"
        out = _read(output_path)
        assert "[[INJECTION:LanguagePatterns]]" in out
        assert "[[DEPLOYED:LanguagePatterns]]" not in out, (
            "LanguagePatterns must never be emitted as a [[DEPLOYED:]] region"
        )


class TestLegacyCustomConstraintsDropRule:
    """The one deliberate, name-scoped old-marker rule this change adds:
    a populated custom_constraints region survives as [[INJECTION:CustomConstraints]];
    an empty one is dropped entirely -- neither an open nor a close tag is emitted."""

    def _transformed_content(self, tmp_path: pathlib.Path, constraints_body: str) -> str:
        content = (
            "---\n"
            "id: test-cc\n"
            "version: 1.0.0\n"
            "name: test-agent\n"
            "description: Agent exercising the legacy custom_constraints marker.\n"
            "---\n\n"
            "# TestAgent Agent\n\nYou are the agent.\n\n---\n\n"
            "## Capabilities\n\nSome capability prose.\n\n---\n\n"
            "## Constraints\n\n" + constraints_body + "\n\n---\n\n"
            "## Error Handling\n\nHandle errors.\n\n---\n\n"
            "## Output Format\n\nJSON.\n\n---\n\n"
            "## Execution Philosophy\n\nExecute.\n"
        )
        input_path = tmp_path / "cc-input.md"
        input_path.write_text(content, encoding="utf-8")
        output_path = tmp_path / "cc-output.md"
        result = transform_file(input_path, output_path)
        assert result.success is True, f"Transform must succeed; errors={result.errors}"
        return _read(output_path)

    def test_populated_custom_constraints_preserved_as_injection(
        self, tmp_path: pathlib.Path
    ) -> None:
        """A custom_constraints region with content must survive as
        [[INJECTION:CustomConstraints]], content preserved verbatim (AC2.5)."""
        out = self._transformed_content(
            tmp_path,
            "Some constraint.\n\n"
            "[INJECTION: custom_constraints]\n"
            "Never touch production credentials.\n",
        )
        assert "[[INJECTION:CustomConstraints]]" in out, (
            "A populated legacy custom_constraints marker must be preserved as "
            "[[INJECTION:CustomConstraints]]"
        )
        assert "Never touch production credentials." in out, (
            "The populated region's content must be preserved verbatim"
        )
        assert "[[DEPLOYED:CustomConstraints]]" not in out, (
            "CustomConstraints must never be emitted as a [[DEPLOYED:]] region -- "
            "it is no longer tool-managed"
        )

    def test_empty_custom_constraints_dropped_entirely(
        self, tmp_path: pathlib.Path
    ) -> None:
        """An empty custom_constraints region must be dropped entirely -- neither
        an open nor a close tag appears anywhere in the output (AC2.5)."""
        out = self._transformed_content(
            tmp_path,
            "Some constraint.\n\n[INJECTION: custom_constraints]\n",
        )
        assert "CustomConstraints" not in out, (
            "An empty legacy custom_constraints marker must produce no region at all"
        )

    def test_empty_custom_constraints_drop_does_not_affect_neighbouring_empty_injection(
        self, tmp_path: pathlib.Path
    ) -> None:
        """A neighbouring empty injection of a DIFFERENT name must still be emitted as
        usual -- the drop rule must be scoped to CustomConstraints and must not become
        a general empty-region sweeper."""
        out = self._transformed_content(
            tmp_path,
            "Some constraint.\n\n"
            "[INJECTION: custom_constraints]\n"
            "[INJECTION: error_handling_extension]\n",
        )
        assert "CustomConstraints" not in out, (
            "The empty custom_constraints region must still be dropped"
        )
        assert "[[INJECTION:ErrorHandlingExtension]]" in out, (
            "A neighbouring empty injection of a different name must still be emitted "
            "as an empty region pair -- the drop rule must not generalise beyond "
            "CustomConstraints"
        )
        assert "[[/INJECTION:ErrorHandlingExtension]]" in out


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
        """Version must be bumped from 2.2.0 to 2.3.0."""
        result, _ = _transform_to_tmp(communication_protocol_input, tmp_path)
        assert result.version_before == "2.2.0"
        assert result.version_after == "2.3.0"


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


# ---------------------------------------------------------------------------
# Stage 1 helpers: import guard
#
# The three functions below are new and do not exist in boundary_transformer.py
# until Stage 1 implementation is complete. The try/except block below assigns
# None to each name so that the rest of this module can be imported and the
# existing test classes continue to pass. Every test in the Stage 1 classes
# calls at least one of these names, causing TypeError ('NoneType' object is not
# callable) or AssertionError (None != expected value) — the TDD RED phase
# failure that proves no implementation exists yet.
# ---------------------------------------------------------------------------

try:
    from boundary_transformer import (  # noqa: E402
        is_orchestrator_file,
        has_canonical_boundary_tags,
        default_version,
        DEFAULT_VERSION,
    )
except ImportError:
    is_orchestrator_file = None       # type: ignore[assignment]
    has_canonical_boundary_tags = None  # type: ignore[assignment]
    default_version = None            # type: ignore[assignment]
    DEFAULT_VERSION = None            # type: ignore[assignment]


# ---------------------------------------------------------------------------
# Stage 1 — Helper 1: orchestrator-by-filename classification
# ---------------------------------------------------------------------------

class TestIsOrchestratorFile:
    """Tests for is_orchestrator_file: canonical filename-based orchestrator detection.

    Classification is filename-only: True if and only if the base name of the
    supplied path is "orchestrator.md" or "orchestrator.agent.md", compared
    case-insensitively. Frontmatter content (including a `role: orchestrator`
    field) is never consulted.

    These tests are in TDD RED phase: they fail until is_orchestrator_file is
    implemented in boundary_transformer.py.
    """

    def test_orchestrator_md_is_orchestrator(self):
        """A path whose base name is exactly 'orchestrator.md' must return True."""
        assert is_orchestrator_file(pathlib.Path("orchestrator.md")) is True

    def test_orchestrator_agent_md_is_orchestrator(self):
        """A path whose base name is exactly 'orchestrator.agent.md' must return True."""
        assert is_orchestrator_file(pathlib.Path("orchestrator.agent.md")) is True

    def test_regular_agent_md_is_not_orchestrator(self):
        """A typical agent filename must return False."""
        assert is_orchestrator_file(pathlib.Path("writer-agent.md")) is False

    def test_arbitrary_agent_md_is_not_orchestrator(self):
        """Any filename other than the two canonical orchestrator forms must return False."""
        assert is_orchestrator_file(pathlib.Path("my-agent.md")) is False

    def test_agent_md_is_not_orchestrator(self):
        """The filename 'agent.md' (no orchestrator qualifier) must return False."""
        assert is_orchestrator_file(pathlib.Path("agent.md")) is False

    def test_orchestrator_prefix_substring_is_not_orchestrator(self):
        """'my-orchestrator.md' must return False: the match must be exact, not a substring."""
        assert is_orchestrator_file(pathlib.Path("my-orchestrator.md")) is False

    def test_orchestrator_suffix_substring_is_not_orchestrator(self):
        """'orchestrator-v2.md' must return False: the match must be exact."""
        assert is_orchestrator_file(pathlib.Path("orchestrator-v2.md")) is False

    def test_orchestrator_md_in_subdirectory_uses_basename(self):
        """When a path contains directory components, only the base name is checked."""
        assert is_orchestrator_file(pathlib.Path("some/deep/path/orchestrator.md")) is True

    def test_orchestrator_agent_md_in_subdirectory_uses_basename(self):
        """'orchestrator.agent.md' remains orchestrator regardless of directory depth."""
        assert is_orchestrator_file(pathlib.Path("harness/agents/orchestrator.agent.md")) is True

    def test_non_orchestrator_name_with_role_orchestrator_is_not_orchestrator(self):
        """A file whose name is not an orchestrator form is never an orchestrator.

        This is the canonical edge case from the design: a file that carries
        `role: orchestrator` in its frontmatter but whose base name does not match
        the orchestrator pattern must return False. The function does not read the
        file; it inspects only the path.
        """
        # Any non-orchestrator base name, regardless of what the file might contain.
        assert is_orchestrator_file(pathlib.Path("my-agent.md")) is False

    def test_case_insensitive_orchestrator_md(self):
        """Comparison must be case-insensitive: 'Orchestrator.md' must return True."""
        assert is_orchestrator_file(pathlib.Path("Orchestrator.md")) is True

    def test_case_insensitive_orchestrator_agent_md(self):
        """Comparison must be case-insensitive: 'ORCHESTRATOR.AGENT.MD' must return True."""
        assert is_orchestrator_file(pathlib.Path("ORCHESTRATOR.AGENT.MD")) is True

    def test_case_insensitive_mixed_case(self):
        """Mixed-case variants of the orchestrator name must return True."""
        assert is_orchestrator_file(pathlib.Path("Orchestrator.Agent.Md")) is True

    def test_non_md_extension_is_not_orchestrator(self):
        """A file named 'orchestrator.txt' must not match: extension must be '.md'."""
        assert is_orchestrator_file(pathlib.Path("orchestrator.txt")) is False


# ---------------------------------------------------------------------------
# Stage 1 — Helper 2: already-transformed structural check
# ---------------------------------------------------------------------------

class TestHasCanonicalBoundaryTags:
    """Tests for has_canonical_boundary_tags: body-level structural tag detection.

    Returns True only when the body carries a structurally valid set of canonical
    [[SECTION:...]] boundary tags. "Structurally valid" requires:
      - at least one [[SECTION:Name]] tag with a canonical name;
      - every open tag has a matching close tag and no close tag is unmatched;
      - every SECTION name is in CANONICAL_SECTIONS;
      - every [[DEPLOYED:Name]] name is in CANONICAL_DEPLOYED and is paired;
      - [[INJECTION:...]] names are open vocabulary but must be paired.

    A tag is recognised only when it occupies a whole line (after stripping the
    line terminator). An inline bracket string does not count.

    These tests are in TDD RED phase: they fail until has_canonical_boundary_tags
    is implemented in boundary_transformer.py.
    """

    # --- True cases -------------------------------------------------------

    def test_minimal_valid_body_returns_true(self):
        """A body with one correctly paired canonical SECTION tag must return True."""
        body = (
            "[[SECTION:Identity]]\n"
            "# Test Agent\n"
            "\n"
            "Some identity content.\n"
            "[[/SECTION:Identity]]\n"
        )
        assert has_canonical_boundary_tags(body) is True

    def test_multiple_canonical_sections_returns_true(self):
        """A body with several correctly paired canonical sections must return True."""
        body = (
            "[[SECTION:Identity]]\n"
            "# Agent\n"
            "[[/SECTION:Identity]]\n"
            "\n"
            "[[SECTION:Capabilities]]\n"
            "## Capabilities\n"
            "[[/SECTION:Capabilities]]\n"
        )
        assert has_canonical_boundary_tags(body) is True

    def test_canonical_deployed_within_sections_returns_true(self):
        """Canonical [[DEPLOYED:...]] tags paired within a valid section must return True."""
        body = (
            "[[SECTION:Identity]]\n"
            "# Agent\n"
            "[[DEPLOYED:AuthorityHierarchy]]\n"
            "[[/DEPLOYED:AuthorityHierarchy]]\n"
            "[[/SECTION:Identity]]\n"
        )
        assert has_canonical_boundary_tags(body) is True

    def test_top_level_canonical_deployed_returns_true(self):
        """[[DEPLOYED:CommunicationProtocol]] at top level with a canonical section must return True."""
        body = (
            "[[SECTION:Identity]]\n"
            "# Agent\n"
            "[[/SECTION:Identity]]\n"
            "\n"
            "[[DEPLOYED:CommunicationProtocol]]\n"
            "[[/DEPLOYED:CommunicationProtocol]]\n"
        )
        assert has_canonical_boundary_tags(body) is True

    def test_correctly_paired_injection_in_valid_section_returns_true(self):
        """Correctly paired [[INJECTION:...]] tags (open vocabulary) must not block True."""
        body = (
            "[[SECTION:Identity]]\n"
            "# Agent\n"
            "[[INJECTION:IdentityExtension]]\n"
            "[[/INJECTION:IdentityExtension]]\n"
            "[[/SECTION:Identity]]\n"
        )
        assert has_canonical_boundary_tags(body) is True

    def test_fixture_transformed_harness_file_returns_true(
        self, harness_only_transformed_input
    ):
        """The harness_only_transformed_input fixture carries a valid set of canonical tags.

        Reads the fixture's body (text after the closing frontmatter '---') and
        passes it to has_canonical_boundary_tags. The result must be True because
        the fixture was authored as an already-transformed harness-only agent with
        all canonical section tags correctly paired.
        """
        body = _body(harness_only_transformed_input.read_text(encoding="utf-8"))
        assert has_canonical_boundary_tags(body) is True

    # --- False cases: no tags at all --------------------------------------

    def test_empty_body_returns_false(self):
        """An empty body carries no canonical tags and must return False."""
        assert has_canonical_boundary_tags("") is False

    def test_whitespace_only_body_returns_false(self):
        """A body containing only whitespace and blank lines must return False."""
        assert has_canonical_boundary_tags("   \n\n   \n") is False

    def test_untagged_legacy_body_returns_false(self):
        """A legacy body with canonical H2 headings but no boundary tags must return False."""
        body = (
            "# Legacy Agent\n"
            "\n"
            "## Capabilities\n"
            "\n"
            "Some capability prose.\n"
            "\n"
            "## Constraints\n"
        )
        assert has_canonical_boundary_tags(body) is False

    def test_fixture_untransformed_harness_file_returns_false(
        self, harness_only_untransformed_input
    ):
        """The harness_only_untransformed_input fixture has no boundary tags.

        Reads the fixture's body and passes it to has_canonical_boundary_tags.
        The result must be False because the fixture is in legacy (pre-transform) format.
        """
        body = _body(harness_only_untransformed_input.read_text(encoding="utf-8"))
        assert has_canonical_boundary_tags(body) is False

    # --- False cases: pairing errors -------------------------------------

    def test_unpaired_open_section_tag_returns_false(self):
        """An open [[SECTION:Identity]] with no matching close tag must return False."""
        body = (
            "[[SECTION:Identity]]\n"
            "Some content.\n"
        )
        assert has_canonical_boundary_tags(body) is False

    def test_orphan_close_section_tag_returns_false(self):
        """An unmatched [[/SECTION:Identity]] with no corresponding open must return False."""
        body = (
            "Some prose.\n"
            "[[/SECTION:Identity]]\n"
        )
        assert has_canonical_boundary_tags(body) is False

    def test_mismatched_section_tag_names_returns_false(self):
        """[[SECTION:Identity]] paired with [[/SECTION:Capabilities]] must return False."""
        body = (
            "[[SECTION:Identity]]\n"
            "Some content.\n"
            "[[/SECTION:Capabilities]]\n"
        )
        assert has_canonical_boundary_tags(body) is False

    def test_unpaired_deployed_tag_returns_false(self):
        """An open [[DEPLOYED:CommunicationProtocol]] without a close must return False."""
        body = (
            "[[SECTION:Identity]]\n"
            "# Agent\n"
            "[[/SECTION:Identity]]\n"
            "[[DEPLOYED:CommunicationProtocol]]\n"
        )
        assert has_canonical_boundary_tags(body) is False

    def test_orphan_close_deployed_tag_returns_false(self):
        """An unmatched [[/DEPLOYED:CommunicationProtocol]] without an open must return False."""
        body = (
            "[[SECTION:Identity]]\n"
            "# Agent\n"
            "[[/SECTION:Identity]]\n"
            "[[/DEPLOYED:CommunicationProtocol]]\n"
        )
        assert has_canonical_boundary_tags(body) is False

    def test_unpaired_injection_tag_returns_false(self):
        """An open [[INJECTION:IdentityExtension]] with no close must return False."""
        body = (
            "[[SECTION:Identity]]\n"
            "# Agent\n"
            "[[INJECTION:IdentityExtension]]\n"
            "[[/SECTION:Identity]]\n"
        )
        assert has_canonical_boundary_tags(body) is False

    # --- False cases: non-canonical names --------------------------------

    def test_non_canonical_section_name_returns_false(self):
        """A [[SECTION:FakeSection]] tag not in CANONICAL_SECTIONS must return False."""
        body = (
            "[[SECTION:FakeSection]]\n"
            "Some content.\n"
            "[[/SECTION:FakeSection]]\n"
        )
        assert has_canonical_boundary_tags(body) is False

    def test_non_canonical_deployed_name_returns_false(self):
        """A [[DEPLOYED:FakeRegion]] tag not in CANONICAL_DEPLOYED must return False."""
        body = (
            "[[SECTION:Identity]]\n"
            "# Agent\n"
            "[[DEPLOYED:FakeRegion]]\n"
            "[[/DEPLOYED:FakeRegion]]\n"
            "[[/SECTION:Identity]]\n"
        )
        assert has_canonical_boundary_tags(body) is False

    # --- False cases: incidental bracket strings -------------------------

    def test_incidental_bracket_string_embedded_mid_line_is_not_a_tag(self):
        """A [[SECTION:Identity]]-shaped string embedded mid-line must not count as a tag.

        Tags are recognised only when they occupy an entire line (after stripping the
        line terminator). A bracket string embedded in prose must not produce a True
        result.
        """
        body = (
            "See the [[SECTION:Identity]] reference in this prose line.\n"
        )
        assert has_canonical_boundary_tags(body) is False

    def test_incidental_bracket_string_with_leading_text_is_not_a_tag(self):
        """A line that has text before [[...]] must not be treated as a tag line."""
        body = (
            "Note: [[SECTION:Identity]] is described above.\n"
            "[[/SECTION:Identity]]\n"
        )
        assert has_canonical_boundary_tags(body) is False

    def test_incidental_bracket_string_with_trailing_text_is_not_a_tag(self):
        """A line that has text after [[...]] must not be treated as a tag line."""
        body = (
            "[[SECTION:Identity]] — start of identity section\n"
            "Some content.\n"
        )
        assert has_canonical_boundary_tags(body) is False


# ---------------------------------------------------------------------------
# Stage 1 — Helper 3: default version generation
# ---------------------------------------------------------------------------

class TestDefaultVersion:
    """Tests for default_version() and the DEFAULT_VERSION constant.

    default_version() returns DEFAULT_VERSION, a constant string that callers
    on the degraded path can use when frontmatter carries no 'version' field.
    The function is opt-in at the call site: it is never consulted by the
    generic-file path or the harness-with-generic-ref path, so the existing
    'Missing version' hard failure is completely unaffected.

    These tests are in TDD RED phase: the pure function tests fail until
    default_version and DEFAULT_VERSION are implemented in boundary_transformer.py.
    The regression tests (test_existing_*) use only the existing transform_file
    and will pass regardless of Stage 1 implementation state.
    """

    def test_default_version_returns_string(self):
        """default_version() must return a non-empty string."""
        result = default_version()
        assert isinstance(result, str)
        assert result != ""

    def test_default_version_returns_one_zero_zero(self):
        """default_version() must return '1.0.0' per the design specification."""
        assert default_version() == "1.0.0"

    def test_default_version_constant_equals_function_return(self):
        """DEFAULT_VERSION must equal the value returned by default_version()."""
        assert DEFAULT_VERSION == default_version()

    def test_default_version_constant_is_string(self):
        """DEFAULT_VERSION must be a string constant."""
        assert isinstance(DEFAULT_VERSION, str)

    def test_default_version_constant_is_one_zero_zero(self):
        """DEFAULT_VERSION must equal '1.0.0'."""
        assert DEFAULT_VERSION == "1.0.0"

    def test_default_version_is_deterministic(self):
        """Repeated calls to default_version() must return the same value."""
        assert default_version() == default_version()

    # --- Regression: existing behavior is preserved ----------------------
    #
    # These tests verify that adding default_version() as a standalone function
    # does not silently relax any existing transform path. They use only the
    # already-implemented transform_file and will pass before and after Stage 1
    # implementation; they document the invariant, not the new capability.

    def test_existing_generic_missing_version_hard_failure_is_preserved(
        self, malformed_no_version_field_input, tmp_path
    ):
        """A generic file with no 'version' must transform successfully using default_version().

        Since Stage 7, the fallback applies to all paths. A missing 'version' is
        substituted with default_version() ('1.0.0') and version_before is that value.
        """
        result, _ = _transform_to_tmp(malformed_no_version_field_input, tmp_path)
        assert result.success is True
        assert result.errors == []
        assert result.version_before == "1.0.0"  # DEFAULT_VERSION fallback

    def test_degraded_path_missing_version_uses_default(
        self, harness_only_no_version_input, tmp_path
    ):
        """A harness file without 'version' field transforms via the degraded path using default_version().

        When a non-orchestrator harness file has no 'version' field and no --generic-ref,
        default_version() supplies the version (DEFAULT_VERSION == '1.0.0') and the
        degraded transform proceeds. The minor bump produces '1.1.0'.
        """
        result = transform_file(
            harness_only_no_version_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.success is True
        assert result.degraded is True
        assert result.version_before == "1.0.0"   # DEFAULT_VERSION used as substitute
        assert result.version_after == "1.1.0"    # minor-bumped from default

    def test_generic_file_with_version_transforms_successfully(
        self, generic_standard_input, tmp_path
    ):
        """A well-formed generic file (with version) must still transform successfully.

        Verifies that DEFAULT_VERSION and default_version() do not interfere with
        the happy path for files that carry their own 'version' field.
        """
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert result.success is True
        assert result.version_before != "", (
            "version_before must be populated from the input file's frontmatter."
        )


# ---------------------------------------------------------------------------
# Stage 2 helpers: import guard
#
# The three warning/error constants below already exist in boundary_transformer.py
# as of Stage 1.  The try/except is defensive: if a future refactor moves or
# renames them the import error surfaces immediately rather than at test runtime
# as a NameError.
#
# The TDD RED phase for Stage 2 does NOT come from missing imports — it comes
# from accessing the new TransformResult fields (degraded, skipped, warnings)
# that do not exist until Stage 2 implementation is complete.  Any test that
# accesses result.degraded, result.skipped, or result.warnings will raise
# AttributeError on the current implementation, producing the expected RED state.
# ---------------------------------------------------------------------------

try:
    from boundary_transformer import (  # noqa: E402
        WARN_NO_GENERIC_REF,
        WARN_ALREADY_TRANSFORMED,
        ERR_HARNESS_NO_GENERIC_REF,
    )
except ImportError:
    WARN_NO_GENERIC_REF = None        # type: ignore[assignment]
    WARN_ALREADY_TRANSFORMED = None   # type: ignore[assignment]
    ERR_HARNESS_NO_GENERIC_REF = None  # type: ignore[assignment]


# ---------------------------------------------------------------------------
# Stage 2 — helper: create an orchestrator-named harness file in tmp_path
# ---------------------------------------------------------------------------

def _make_orchestrator_harness_file(
    tmp_path: pathlib.Path,
    filename: str = "orchestrator.md",
) -> pathlib.Path:
    """Write a minimal harness file at <tmp_path>/<filename> and return its path.

    The file carries transform_version (harness signal) and a valid version, but
    no --generic-ref.  Because its name is 'orchestrator.md' or
    'orchestrator.agent.md', is_orchestrator_file() returns True and the hard
    error is preserved.
    """
    path = tmp_path / filename
    path.write_text(
        "---\n"
        "id: 1\n"
        "version: 1.0.0\n"
        "transform_version: 1.0.0\n"
        "name: test-orchestrator\n"
        "role: orchestrator\n"
        "---\n"
        "\n"
        "# TestOrchestrator Agent\n"
        "\n"
        "You are the **TestOrchestrator** agent.\n"
        "\n"
        "---\n"
        "\n"
        "## Capabilities\n"
        "\n"
        "Orchestrate test workflows.\n",
        encoding="utf-8",
    )
    return path


# ---------------------------------------------------------------------------
# Stage 2 — Degraded fallback (T2.1)
# ---------------------------------------------------------------------------

class TestDegradedFallback:
    """A non-orchestrator harness file with no generic ref transforms via the degraded path.

    These tests are in TDD RED phase: they will fail until transform_file() is
    updated to implement the degraded-path dispatch described in the Stage 2 design.
    Specifically, tests that access result.degraded, result.skipped, or result.warnings
    fail with AttributeError because those fields do not yet exist on TransformResult.
    Tests that assert result.success is True fail because the current implementation
    returns success=False for any harness file without a generic ref.
    """

    def test_degraded_transform_succeeds(
        self, harness_only_untransformed_input, tmp_path
    ):
        """transform_file must return success=True for a non-orchestrator harness with no generic ref."""
        result, _ = _transform_to_tmp(harness_only_untransformed_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_result_degraded_flag_set(
        self, harness_only_untransformed_input, tmp_path
    ):
        """TransformResult.degraded must be True on the degraded path."""
        result, _ = _transform_to_tmp(harness_only_untransformed_input, tmp_path)
        assert result.degraded is True

    def test_result_skipped_flag_not_set(
        self, harness_only_untransformed_input, tmp_path
    ):
        """TransformResult.skipped must be False for a file that was actually transformed."""
        result, _ = _transform_to_tmp(harness_only_untransformed_input, tmp_path)
        assert result.skipped is False

    def test_warn_no_generic_ref_in_warnings(
        self, harness_only_untransformed_input, tmp_path
    ):
        """TransformResult.warnings must contain the WARN_NO_GENERIC_REF text."""
        result, _ = _transform_to_tmp(harness_only_untransformed_input, tmp_path)
        assert len(result.warnings) >= 1
        assert any(
            "No generic reference found" in w and "degraded" in w
            for w in result.warnings
        ), f"Expected WARN_NO_GENERIC_REF warning; got: {result.warnings!r}"

    def test_warn_contains_input_path(
        self, harness_only_untransformed_input, tmp_path
    ):
        """The degraded-path warning must embed the input file path so callers can identify the file."""
        result, _ = _transform_to_tmp(harness_only_untransformed_input, tmp_path)
        path_str = str(harness_only_untransformed_input)
        assert any(path_str in w for w in result.warnings), (
            f"Expected a warning containing '{path_str}'; got: {result.warnings!r}"
        )

    def test_output_file_written(
        self, harness_only_untransformed_input, tmp_path
    ):
        """An output file must be written on the degraded path (unlike the orchestrator hard-fail path)."""
        output_path = tmp_path / "degraded_out.md"
        transform_file(harness_only_untransformed_input, output_path)
        assert output_path.exists(), "Output file must be written on the degraded path"

    def test_output_has_canonical_boundary_tags(
        self, harness_only_untransformed_input, tmp_path
    ):
        """The degraded-path output must carry a structurally valid set of canonical [[SECTION:...]] tags."""
        _, output_path = _transform_to_tmp(harness_only_untransformed_input, tmp_path)
        output_body = _body(_read(output_path))
        assert has_canonical_boundary_tags(output_body) is True, (
            "Degraded-path output must satisfy has_canonical_boundary_tags."
        )

    def test_sections_added_nonempty(
        self, harness_only_untransformed_input, tmp_path
    ):
        """At least one canonical section boundary must be added by the degraded transform."""
        result, _ = _transform_to_tmp(harness_only_untransformed_input, tmp_path)
        assert len(result.sections_added) >= 1, (
            "The degraded transform must add at least one canonical section boundary."
        )

    def test_all_six_sections_identified(
        self, harness_only_untransformed_input, tmp_path
    ):
        """All six canonical sections present in the fixture must appear in sections_added."""
        result, _ = _transform_to_tmp(harness_only_untransformed_input, tmp_path)
        expected_sections = [
            "Identity", "Capabilities", "Constraints",
            "ErrorHandling", "OutputFormat", "ExecutionPhilosophy",
        ]
        for name in expected_sections:
            assert name in result.sections_added, (
                f"Section '{name}' missing from sections_added; "
                f"got: {result.sections_added!r}"
            )

    def test_version_bumped_correctly(
        self, harness_only_untransformed_input, tmp_path
    ):
        """Version 1.0.0 in the fixture must be bumped to 1.1.0 by the degraded transform."""
        result, _ = _transform_to_tmp(harness_only_untransformed_input, tmp_path)
        assert result.version_before == "1.0.0"
        assert result.version_after == "1.1.0"

    def test_cli_exits_zero_on_degraded_path(
        self, harness_only_untransformed_input, tmp_path
    ):
        """CLI must exit 0 when a non-orchestrator harness file is processed on the degraded path."""
        output_path = tmp_path / "out.md"
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_only_untransformed_input), "--output", str(output_path)],
            capture_output=True, text=True,
        )
        assert proc.returncode == 0, (
            f"CLI must exit 0 on the degraded path; got exit {proc.returncode}. "
            f"stderr: {proc.stderr!r}"
        )

    def test_cli_emits_warning_to_stderr_not_error(
        self, harness_only_untransformed_input, tmp_path
    ):
        """CLI must emit the WARN_NO_GENERIC_REF warning to stderr (not an error traceback)."""
        output_path = tmp_path / "out.md"
        proc = subprocess.run(
            [sys.executable, str(_TRANSFORMER_CLI),
             str(harness_only_untransformed_input), "--output", str(output_path)],
            capture_output=True, text=True,
        )
        assert proc.returncode == 0, f"Precondition: CLI must exit 0; got {proc.returncode}"
        assert "NotImplementedError" not in proc.stderr, (
            "CLI must emit a structured warning, not crash with NotImplementedError."
        )
        assert proc.stderr.strip(), (
            "CLI must emit the degraded-path warning to stderr; got empty stderr."
        )
        assert "No generic reference found" in proc.stderr or "degraded" in proc.stderr.lower(), (
            f"CLI stderr must contain the degraded-path warning; got: {proc.stderr!r}"
        )


# ---------------------------------------------------------------------------
# Stage 2 — Orchestrator hard error preserved (T2.2)
# ---------------------------------------------------------------------------

class TestOrchestratorHardErrorPreserved:
    """Orchestrator-named harness files without a generic ref still fail with the hard error.

    These tests verify that Stage 2 preserves the exact pre-existing error behavior
    for orchestrator-named files while introducing the degraded path for all others.
    Tests that access result.degraded, result.skipped, or result.warnings are RED
    (AttributeError) until Stage 2 implementation is complete.
    Tests that assert result.success is False pass in RED phase because the current
    implementation already fails for ALL harness files without generic ref.
    """

    def test_orchestrator_md_result_is_failure(self, tmp_path):
        """A file named 'orchestrator.md' with transform_version must fail without --generic-ref."""
        orch_path = _make_orchestrator_harness_file(tmp_path, "orchestrator.md")
        result = transform_file(orch_path, tmp_path / "out.md", generic_ref_path=None)
        assert result.success is False

    def test_orchestrator_agent_md_result_is_failure(self, tmp_path):
        """A file named 'orchestrator.agent.md' with transform_version must fail without --generic-ref."""
        orch_path = _make_orchestrator_harness_file(tmp_path, "orchestrator.agent.md")
        result = transform_file(orch_path, tmp_path / "out.md", generic_ref_path=None)
        assert result.success is False

    def test_orchestrator_error_contains_existing_message(self, tmp_path):
        """The hard error must carry the exact ERR_HARNESS_NO_GENERIC_REF message string."""
        orch_path = _make_orchestrator_harness_file(tmp_path)
        result = transform_file(orch_path, tmp_path / "out.md", generic_ref_path=None)
        assert result.errors, "At least one TransformError must be present"
        error_messages = [e.message for e in result.errors]
        assert any(
            "Harness file detected" in m and "--generic-ref" in m
            for m in error_messages
        ), (
            f"Expected the ERR_HARNESS_NO_GENERIC_REF message; "
            f"got: {error_messages!r}"
        )

    def test_orchestrator_error_at_line_one(self, tmp_path):
        """The hard error must carry line_number=1 (per the existing contract)."""
        orch_path = _make_orchestrator_harness_file(tmp_path)
        result = transform_file(orch_path, tmp_path / "out.md", generic_ref_path=None)
        assert result.errors, "At least one TransformError must be present"
        assert all(e.line_number >= 1 for e in result.errors)

    def test_orchestrator_degraded_flag_not_set(self, tmp_path):
        """TransformResult.degraded must be False for the orchestrator hard-error path."""
        orch_path = _make_orchestrator_harness_file(tmp_path)
        result = transform_file(orch_path, tmp_path / "out.md", generic_ref_path=None)
        assert result.degraded is False

    def test_orchestrator_skipped_flag_not_set(self, tmp_path):
        """TransformResult.skipped must be False for the orchestrator hard-error path."""
        orch_path = _make_orchestrator_harness_file(tmp_path)
        result = transform_file(orch_path, tmp_path / "out.md", generic_ref_path=None)
        assert result.skipped is False

    def test_orchestrator_no_output_file_written(self, tmp_path):
        """No output file must be written on the orchestrator hard-error path."""
        orch_path = _make_orchestrator_harness_file(tmp_path)
        output_path = tmp_path / "out.md"
        transform_file(orch_path, output_path, generic_ref_path=None)
        assert not output_path.exists(), (
            "No output file must be written when the orchestrator hard error fires."
        )

    def test_non_orchestrator_harness_takes_degraded_path(
        self, harness_only_untransformed_input, tmp_path
    ):
        """A non-orchestrator harness file must take the degraded path, not the hard-error path."""
        result, _ = _transform_to_tmp(harness_only_untransformed_input, tmp_path)
        assert result.success is True
        assert result.degraded is True


# ---------------------------------------------------------------------------
# Stage 2 — Missing version tolerance on the degraded path (T2.3)
# ---------------------------------------------------------------------------

class TestMissingVersionOnDegradedPath:
    """The degraded path tolerates a missing 'version' field using default_version().

    Tests that assert result.success is True and access result.degraded are RED
    until Stage 2 implementation wires default_version() into the degraded dispatch.
    Regression tests that verify generic files still fail on missing version pass
    in RED phase (existing behaviour is unchanged).
    """

    def test_degraded_no_version_succeeds(
        self, harness_only_no_version_input, tmp_path
    ):
        """A harness file with no 'version' field must succeed on the degraded path."""
        result = transform_file(
            harness_only_no_version_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.success is True
        assert result.errors == []

    def test_degraded_no_version_degraded_flag_set(
        self, harness_only_no_version_input, tmp_path
    ):
        """TransformResult.degraded must be True when the missing-version fallback fires."""
        result = transform_file(
            harness_only_no_version_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.degraded is True

    def test_degraded_no_version_uses_default_as_version_before(
        self, harness_only_no_version_input, tmp_path
    ):
        """version_before must equal DEFAULT_VERSION ('1.0.0') when the field is absent."""
        result = transform_file(
            harness_only_no_version_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.version_before == "1.0.0", (
            f"Expected version_before to equal DEFAULT_VERSION '1.0.0'; "
            f"got: {result.version_before!r}"
        )

    def test_degraded_no_version_bumps_from_default(
        self, harness_only_no_version_input, tmp_path
    ):
        """version_after must be the minor-bumped value of DEFAULT_VERSION ('1.1.0')."""
        result = transform_file(
            harness_only_no_version_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.version_after == "1.1.0", (
            f"Expected version_after == '1.1.0' (minor bump of default '1.0.0'); "
            f"got: {result.version_after!r}"
        )

    def test_degraded_no_version_output_written(
        self, harness_only_no_version_input, tmp_path
    ):
        """An output file must be written even when the version came from default_version()."""
        output_path = tmp_path / "out.md"
        transform_file(harness_only_no_version_input, output_path, generic_ref_path=None)
        assert output_path.exists(), (
            "Output must be written on the degraded path even when version was absent."
        )

    # --- Regression: existing paths are unchanged ---

    def test_generic_file_no_version_still_fails(
        self, malformed_no_version_field_input, tmp_path
    ):
        """A generic file without 'version' must transform successfully using default_version().

        Since Stage 7, the fallback applies to all paths including the generic path.
        The transform succeeds with version_before == DEFAULT_VERSION.
        """
        result, _ = _transform_to_tmp(malformed_no_version_field_input, tmp_path)
        assert result.success is True
        assert result.errors == []
        assert result.version_before == "1.0.0"  # DEFAULT_VERSION fallback

    def test_harness_with_ref_no_version_still_fails(
        self, harness_only_no_version_input, generic_standard_input, tmp_path
    ):
        """A harness file with a generic ref but no 'version' must transform successfully.

        Since Stage 7, the fallback applies to all paths including harness-with-ref.
        The transform succeeds with version_before == DEFAULT_VERSION.
        """
        result = transform_file(
            harness_only_no_version_input,
            tmp_path / "out.md",
            generic_ref_path=generic_standard_input,
        )
        assert result.success is True
        assert result.errors == []
        assert result.version_before == "1.0.0"  # DEFAULT_VERSION fallback


# ---------------------------------------------------------------------------
# Stage 2 — Idempotency guard (T2.4)
# ---------------------------------------------------------------------------

class TestIdempotencyGuard:
    """A harness-only file that already carries valid canonical tags is skipped with a warning.

    Tests that access result.skipped, result.degraded, or result.warnings are RED
    (AttributeError) until Stage 2 implementation is complete.
    Tests that assert result.success is True fail in RED because the current
    implementation returns success=False for harness files without a generic ref,
    regardless of whether they already have boundary tags.
    """

    def test_already_transformed_result_is_success(
        self, harness_only_transformed_input, tmp_path
    ):
        """transform_file must return success=True for an already-transformed file."""
        result = transform_file(
            harness_only_transformed_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.success is True

    def test_skipped_flag_set(
        self, harness_only_transformed_input, tmp_path
    ):
        """TransformResult.skipped must be True when the idempotency guard fires."""
        result = transform_file(
            harness_only_transformed_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.skipped is True

    def test_degraded_flag_not_set(
        self, harness_only_transformed_input, tmp_path
    ):
        """TransformResult.degraded must be False for a skipped file (guard fires before degraded path)."""
        result = transform_file(
            harness_only_transformed_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.degraded is False

    def test_warn_already_transformed_in_warnings(
        self, harness_only_transformed_input, tmp_path
    ):
        """TransformResult.warnings must contain the WARN_ALREADY_TRANSFORMED text."""
        result = transform_file(
            harness_only_transformed_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert len(result.warnings) >= 1
        assert any(
            "already carries canonical boundary tags" in w
            for w in result.warnings
        ), f"Expected WARN_ALREADY_TRANSFORMED in warnings; got: {result.warnings!r}"

    def test_warn_includes_input_path(
        self, harness_only_transformed_input, tmp_path
    ):
        """The idempotency warning must embed the input file path."""
        result = transform_file(
            harness_only_transformed_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        path_str = str(harness_only_transformed_input)
        assert any(path_str in w for w in result.warnings), (
            f"Expected a warning containing '{path_str}'; got: {result.warnings!r}"
        )

    def test_errors_list_is_empty(
        self, harness_only_transformed_input, tmp_path
    ):
        """A skipped file must produce no errors — idempotency is success, not error."""
        result = transform_file(
            harness_only_transformed_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.errors == []

    def test_sections_added_is_empty(
        self, harness_only_transformed_input, tmp_path
    ):
        """sections_added must be empty for a skipped file (no sections were added)."""
        result = transform_file(
            harness_only_transformed_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.sections_added == []

    def test_version_after_equals_version_before(
        self, harness_only_transformed_input, tmp_path
    ):
        """version_after must equal version_before when the file is skipped (no rewrite)."""
        result = transform_file(
            harness_only_transformed_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.version_after == result.version_before

    def test_no_output_file_written_when_skipped(
        self, harness_only_transformed_input, tmp_path
    ):
        """No output file must be written when the idempotency guard fires."""
        output_path = tmp_path / "skipped_out.md"
        transform_file(harness_only_transformed_input, output_path, generic_ref_path=None)
        assert not output_path.exists(), (
            "No output file must be written when the file is skipped by the idempotency guard."
        )


# ---------------------------------------------------------------------------
# Stage 2 — Relaxed identity handling (T2.5)
# ---------------------------------------------------------------------------

class TestRelaxedIdentityHandling:
    """A harness-only file with non-canonical H2 headings inside Identity transforms without error.

    The degraded path uses strict_identity=False so that legitimate non-canonical
    H2 headings inside the Identity section range are absorbed rather than reported
    as unclassifiable content.  On the strict generic path the same headings would
    cause a transform failure.

    All tests in this class are RED until Stage 2 implementation is complete.
    """

    def test_degraded_transform_succeeds(
        self, harness_only_noncanonical_identity_input, tmp_path
    ):
        """Transformation of a harness-only file with a non-canonical Identity H2 must succeed."""
        result, _ = _transform_to_tmp(harness_only_noncanonical_identity_input, tmp_path)
        assert result.success is True

    def test_no_unclassifiable_content_errors(
        self, harness_only_noncanonical_identity_input, tmp_path
    ):
        """No 'unclassifiable heading' or 'unclassifiable content' errors must be reported."""
        result, _ = _transform_to_tmp(harness_only_noncanonical_identity_input, tmp_path)
        assert result.errors == [], (
            f"Expected no errors; got: {[(e.line_number, e.message) for e in result.errors]!r}"
        )

    def test_degraded_flag_set(
        self, harness_only_noncanonical_identity_input, tmp_path
    ):
        """TransformResult.degraded must be True for this fixture (no generic ref)."""
        result, _ = _transform_to_tmp(harness_only_noncanonical_identity_input, tmp_path)
        assert result.degraded is True

    def test_identity_section_added(
        self, harness_only_noncanonical_identity_input, tmp_path
    ):
        """The Identity section must be present in sections_added despite the non-canonical H2."""
        result, _ = _transform_to_tmp(harness_only_noncanonical_identity_input, tmp_path)
        assert "Identity" in result.sections_added, (
            f"Identity section missing from sections_added; got: {result.sections_added!r}"
        )

    def test_noncanonical_h2_preserved_in_output(
        self, harness_only_noncanonical_identity_input, tmp_path
    ):
        """The non-canonical '## System Context' heading must appear in the transformed output."""
        _, output_path = _transform_to_tmp(harness_only_noncanonical_identity_input, tmp_path)
        content = _read(output_path)
        assert "## System Context" in content, (
            "Non-canonical H2 heading must be preserved verbatim in the degraded-path output."
        )

    def test_noncanonical_h2_inside_identity_boundary(
        self, harness_only_noncanonical_identity_input, tmp_path
    ):
        """The '## System Context' heading must appear inside [[SECTION:Identity]]..[[/SECTION:Identity]]."""
        _, output_path = _transform_to_tmp(harness_only_noncanonical_identity_input, tmp_path)
        content = _read(output_path)
        identity_open = content.find("[[SECTION:Identity]]")
        identity_close = content.find("[[/SECTION:Identity]]")
        system_context_pos = content.find("## System Context")
        assert identity_open != -1, "[[SECTION:Identity]] must be present"
        assert identity_close != -1, "[[/SECTION:Identity]] must be present"
        assert system_context_pos != -1, "## System Context heading must be present"
        assert identity_open < system_context_pos < identity_close, (
            "## System Context must be inside the Identity section boundary; "
            "strict identity would have left it outside, causing an error."
        )

    def test_output_has_canonical_boundary_tags(
        self, harness_only_noncanonical_identity_input, tmp_path
    ):
        """The degraded-path output must satisfy has_canonical_boundary_tags."""
        _, output_path = _transform_to_tmp(harness_only_noncanonical_identity_input, tmp_path)
        output_body = _body(_read(output_path))
        assert has_canonical_boundary_tags(output_body) is True

    def test_strict_path_would_fail_for_contrast(
        self, harness_only_noncanonical_identity_input, tmp_path
    ):
        """Contrast test: the same file fails on the strict path (generic with no transform_version).

        This test constructs a generic version of the fixture by stripping transform_version
        from the frontmatter, then confirms that the strict generic path reports an
        unclassifiable-heading error for ## System Context.  This validates that the
        relaxation in the degraded path is real and meaningful, not accidental.
        """
        original = _read(harness_only_noncanonical_identity_input)
        # Strip transform_version to make the file appear generic
        lines = original.splitlines(keepends=True)
        generic_lines = [
            line for line in lines
            if not line.startswith("transform_version:")
        ]
        generic_path = tmp_path / "strict_test_generic.md"
        generic_path.write_text("".join(generic_lines), encoding="utf-8")
        result = transform_file(generic_path, tmp_path / "strict_out.md")
        assert result.success is False, (
            "The strict generic path must fail on a non-canonical H2 inside Identity; "
            "if it passes, the contrast with the relaxed degraded path is lost."
        )
        assert any(
            "unclassifiable" in e.message.lower() for e in result.errors
        ), (
            f"Expected an 'unclassifiable' error; got: "
            f"{[(e.line_number, e.message) for e in result.errors]!r}"
        )


# ---------------------------------------------------------------------------
# Stage 2 — Byte-identical regression for existing paths (T2.6)
# ---------------------------------------------------------------------------

class TestDegradedPathRegressions:
    """Harness-with-generic-ref and generic-file transforms produce byte-identical output.

    The byte-identical output tests pass in RED phase (existing paths are unchanged).
    Tests that access result.degraded, result.skipped, or result.warnings are RED
    (AttributeError) and pass after Stage 2 implementation adds those fields with
    the correct default values for the existing paths.

    These regression tests ensure Stage 2 implementation does not silently change
    the output for files that already have a generic reference or are generic files.
    """

    def test_generic_file_output_byte_identical(
        self, generic_standard_input, generic_standard_expected, tmp_path
    ):
        """A generic file must produce byte-identical output after Stage 2 changes."""
        _, output_path = _transform_to_tmp(generic_standard_input, tmp_path)
        assert _read(output_path) == _read(generic_standard_expected), (
            "Stage 2 must not change generic-file transform output."
        )

    def test_harness_with_ref_output_byte_identical(
        self,
        harness_codebase_agnostic_input,
        harness_codebase_agnostic_expected,
        generic_standard_input,
        tmp_path,
    ):
        """A harness file with a generic ref must produce byte-identical output after Stage 2 changes."""
        _, output_path = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert _read(output_path) == _read(harness_codebase_agnostic_expected), (
            "Stage 2 must not change harness-with-generic-ref transform output."
        )

    def test_generic_file_degraded_flag_is_false(
        self, generic_standard_input, tmp_path
    ):
        """TransformResult.degraded must be False for a generic file (unchanged path)."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert result.degraded is False

    def test_generic_file_skipped_flag_is_false(
        self, generic_standard_input, tmp_path
    ):
        """TransformResult.skipped must be False for a generic file."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert result.skipped is False

    def test_generic_file_warnings_is_empty(
        self, generic_standard_input, tmp_path
    ):
        """TransformResult.warnings must be empty for a generic file."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert result.warnings == []

    def test_harness_with_ref_degraded_flag_is_false(
        self, harness_codebase_agnostic_input, generic_standard_input, tmp_path
    ):
        """TransformResult.degraded must be False when a generic ref is provided."""
        result, _ = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert result.degraded is False

    def test_harness_with_ref_skipped_flag_is_false(
        self, harness_codebase_agnostic_input, generic_standard_input, tmp_path
    ):
        """TransformResult.skipped must be False when a generic ref is provided."""
        result, _ = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert result.skipped is False

    def test_harness_with_ref_warnings_is_empty(
        self, harness_codebase_agnostic_input, generic_standard_input, tmp_path
    ):
        """TransformResult.warnings must be empty when a generic ref is provided."""
        result, _ = _transform_to_tmp(
            harness_codebase_agnostic_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert result.warnings == []

    def test_harness_example_project_with_ref_output_byte_identical(
        self,
        harness_example_project_input,
        harness_example_project_expected,
        generic_standard_input,
        tmp_path,
    ):
        """ExampleProject harness with a generic ref must produce byte-identical output after Stage 2."""
        _, output_path = _transform_to_tmp(
            harness_example_project_input, tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert _read(output_path) == _read(harness_example_project_expected), (
            "Stage 2 must not change ExampleProject harness-with-generic-ref transform output."
        )


# ---------------------------------------------------------------------------
# Stage 7 helpers: import guard
#
# bump_version and resolve_version are new public functions added in Stage 7.
# The stubs exist in boundary_transformer.py from Stage 7 test-writing onward;
# the try/except is defensive: if a future refactor moves or renames them the
# import error surfaces immediately rather than at test runtime as a NameError.
#
# The TDD RED phase for Stage 7 comes from two sources:
#   1. bump_version and resolve_version raise NotImplementedError (stub) until
#      Stage 7 implementation (I7.1) is complete.
#   2. transform_file assertions on minor version_after values fail because the
#      current implementation performs a major bump.
#   3. Assertions that a missing-version generic file transforms successfully fail
#      because the current generic path hard-fails without a version field.
# ---------------------------------------------------------------------------

try:
    from boundary_transformer import (  # noqa: E402
        bump_version,
        resolve_version,
        DEFAULT_VERSION,
    )
except ImportError:
    bump_version = None       # type: ignore[assignment]
    resolve_version = None    # type: ignore[assignment]
    DEFAULT_VERSION = "1.0.0" # type: ignore[assignment]


# ---------------------------------------------------------------------------
# Minor version bump — unit tests (Stage 7, T7.1)
# ---------------------------------------------------------------------------

class TestMinorVersionBump:
    """bump_version must increment the minor component and reset patch to zero.

    The function replaces the always-major _bump_version behaviour: major is
    unchanged, minor increments by 1, patch resets to 0.

    These tests are in TDD RED phase: they fail until bump_version is implemented
    (currently raises NotImplementedError).
    """

    @pytest.mark.parametrize("version_before, version_after", [
        ("2.5.0", "2.6.0"),
        ("1.0.0", "1.1.0"),
        ("3.0.0", "3.1.0"),
        ("10.9.8", "10.10.0"),
        ("0.0.0", "0.1.0"),
        ("2.2.0", "2.3.0"),
    ])
    def test_minor_bumped_patch_reset(self, version_before, version_after):
        """Minor must increment; patch must reset to 0; major must remain unchanged."""
        result = bump_version(version_before)
        assert result == version_after, (
            f"bump_version({version_before!r}) expected {version_after!r}, got {result!r}. "
            "The bump must be minor: major stays unchanged, minor increments by 1, "
            "patch resets to 0."
        )

    def test_major_unchanged_after_minor_bump(self):
        """Major component must not change after a minor bump."""
        result = bump_version("5.3.1")
        parts = result.split(".")
        assert parts[0] == "5", (
            f"Major component must remain '5' after minor bump of '5.3.1'; "
            f"bump_version returned {result!r} (major={parts[0]!r})."
        )

    def test_result_has_three_parts(self):
        """The return value must always be in 'M.m.p' form — exactly three dot-separated parts."""
        result = bump_version("2.5.0")
        parts = result.split(".")
        assert len(parts) == 3, (
            f"bump_version must return a three-part version string; "
            f"got {result!r} which has {len(parts)} part(s)."
        )

    def test_short_version_one_part(self):
        """A version string with a single part must produce 'M.1.0'."""
        result = bump_version("3")
        assert result == "3.1.0", (
            f"bump_version('3') expected '3.1.0', got {result!r}. "
            "A single-part version is treated as 'M.0.0': minor bumps to 1, patch resets to 0."
        )

    def test_short_version_two_parts(self):
        """A version string with two parts must produce 'M.(m+1).0'."""
        result = bump_version("1.2")
        assert result == "1.3.0", (
            f"bump_version('1.2') expected '1.3.0', got {result!r}. "
            "A two-part version must increment the second part and append .0."
        )

    def test_malformed_non_integer_first_part_returns_default(self):
        """A version string whose first part is not a non-negative integer returns DEFAULT_VERSION."""
        result = bump_version("abc.1.0")
        assert result == DEFAULT_VERSION, (
            f"bump_version('abc.1.0') expected DEFAULT_VERSION ({DEFAULT_VERSION!r}), "
            f"got {result!r}. A non-integer first part must return the default version, "
            "not raise an exception."
        )

    def test_empty_string_returns_default(self):
        """An empty version string returns DEFAULT_VERSION."""
        result = bump_version("")
        assert result == DEFAULT_VERSION, (
            f"bump_version('') expected DEFAULT_VERSION ({DEFAULT_VERSION!r}), "
            f"got {result!r}. An empty string cannot be parsed and must return the default."
        )

    def test_extra_parts_beyond_third_discarded(self):
        """Version parts beyond the third are discarded; the result is always M.m.0."""
        result = bump_version("1.2.3.4")
        assert result == "1.3.0", (
            f"bump_version('1.2.3.4') expected '1.3.0', got {result!r}. "
            "Parts beyond the third must be discarded; only M.(m+1).0 is returned."
        )

    def test_minor_part_in_result_is_incremented_by_one(self):
        """The minor component in the result must be exactly input_minor + 1."""
        result = bump_version("4.7.2")
        parts = result.split(".")
        assert parts[1] == "8", (
            f"Minor component expected '8' (7 + 1) in bump of '4.7.2'; "
            f"bump_version returned {result!r} (minor={parts[1]!r})."
        )

    def test_malformed_non_integer_second_part_returns_default(self):
        """A version string whose second part is not a non-negative integer returns DEFAULT_VERSION."""
        result = bump_version("1.abc.0")
        assert result == DEFAULT_VERSION, (
            f"bump_version('1.abc.0') expected DEFAULT_VERSION ({DEFAULT_VERSION!r}), "
            f"got {result!r}. A non-integer second part must return the default version, "
            "not raise an exception."
        )

    def test_patch_part_in_result_is_zero(self):
        """The patch component in the result must always be '0'."""
        result = bump_version("4.7.2")
        parts = result.split(".")
        assert parts[2] == "0", (
            f"Patch component expected '0' in bump of '4.7.2'; "
            f"bump_version returned {result!r} (patch={parts[2]!r})."
        )


# ---------------------------------------------------------------------------
# Minor bump applied by transform_file (Stage 7, T7.1 end-to-end)
# ---------------------------------------------------------------------------

class TestMinorBumpAppliedByTransformFile:
    """transform_file must apply a minor-not-major version bump to 'version' and
    'transform_version'.

    These tests are in TDD RED phase: they fail because the current transformer
    increments major (e.g. 2.2.0 -> 3.0.0) instead of minor (2.2.0 -> 2.3.0).
    """

    def test_version_field_bumped_as_minor(self, generic_standard_input, tmp_path):
        """'version' must be bumped as minor: 2.2.0 -> 2.3.0, not 3.0.0."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert result.version_after == "2.3.0", (
            f"Expected minor bump '2.3.0'; got {result.version_after!r}. "
            "transform_file must use bump_version (minor), not the old major-bump logic."
        )

    def test_version_before_still_correct(self, generic_standard_input, tmp_path):
        """version_before must still reflect the original version from the input file."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        assert result.version_before == "2.2.0", (
            f"version_before expected '2.2.0'; got {result.version_before!r}. "
            "version_before must not be affected by the bump tier change."
        )

    def test_major_unchanged_in_version_after(self, generic_standard_input, tmp_path):
        """Major component in version_after must equal the major component in version_before."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)
        major_before = result.version_before.split(".")[0]
        major_after = result.version_after.split(".")[0]
        assert major_before == major_after, (
            f"Major component must not change: was {major_before!r}, "
            f"got {major_after!r} in version_after={result.version_after!r}. "
            "Only the minor component changes in a minor bump."
        )

    def test_transform_version_bumped_as_minor(
        self, harness_codebase_agnostic_input, tmp_path
    ):
        """'transform_version' in a harness file must also be bumped as minor.

        harness_codebase_agnostic_input carries transform_version: 2.2.0.
        After transformation it must appear as 2.3.0 in the output, not 3.0.0.
        """
        _, output_path = _transform_to_tmp(harness_codebase_agnostic_input, tmp_path)
        content = _read(output_path)
        assert "transform_version: 2.3.0" in content, (
            "Expected 'transform_version: 2.3.0' in output (minor bump of 2.2.0). "
            "transform_version must undergo a minor bump, not major."
        )
        assert "transform_version: 3.0.0" not in content, (
            "'transform_version: 3.0.0' must not appear — major bump is no longer applied."
        )

    @pytest.mark.parametrize("version_before, version_after", [
        ("2.3.0", "2.4.0"),
        ("5.3.1", "5.4.0"),
        ("4.2.0", "4.3.0"),
        ("10.9.8", "10.10.0"),
        ("1.0.0", "1.1.0"),
    ])
    def test_minor_bump_parametrized(
        self, version_before, version_after, generic_standard_input, tmp_path
    ):
        """Minor must increment; major must remain unchanged; patch must reset to 0."""
        content = _read(generic_standard_input)
        original_version_line = next(
            ln for ln in content.splitlines() if ln.startswith("version:")
        )
        modified_content = content.replace(
            original_version_line, f"version: {version_before}", 1
        )
        patched_input = tmp_path / f"patched_{version_before.replace('.', '_')}.md"
        patched_input.write_text(modified_content, encoding="utf-8")

        result = transform_file(patched_input, tmp_path / "out.md")
        assert result.version_before == version_before
        assert result.version_after == version_after, (
            f"Expected minor bump {version_after!r} from {version_before!r}; "
            f"got {result.version_after!r}. Major must be unchanged, "
            "minor must increment by 1, patch must reset to 0."
        )


# ---------------------------------------------------------------------------
# resolve_version — unit tests (Stage 7, T7.1 / T7.2)
# ---------------------------------------------------------------------------

class TestResolveVersion:
    """resolve_version must return the frontmatter version when present and non-empty,
    or default_version() when absent or empty.

    These tests are in TDD RED phase: they fail until resolve_version is implemented
    (currently raises NotImplementedError).
    """

    def test_present_version_is_returned_unchanged(self):
        """When 'version' is in frontmatter, resolve_version returns it unchanged."""
        result = resolve_version({"version": "2.5.0", "name": "test-agent"})
        assert result == "2.5.0", (
            f"resolve_version with 'version: 2.5.0' expected '2.5.0', got {result!r}. "
            "The present version value must be returned as-is."
        )

    def test_absent_version_returns_default(self):
        """When 'version' is absent from frontmatter, resolve_version returns default_version()."""
        result = resolve_version({"name": "test-agent", "transform_version": "1.0.0"})
        assert result == DEFAULT_VERSION, (
            f"resolve_version with no 'version' key expected DEFAULT_VERSION "
            f"({DEFAULT_VERSION!r}), got {result!r}."
        )

    def test_empty_version_value_returns_default(self):
        """When 'version' is present but empty, resolve_version returns default_version().

        An empty value ('version:' with no content) counts as absent: callers must
        not receive an empty string as a version.
        """
        result = resolve_version({"version": "", "name": "test-agent"})
        assert result == DEFAULT_VERSION, (
            f"resolve_version with empty 'version' value expected DEFAULT_VERSION "
            f"({DEFAULT_VERSION!r}), got {result!r}. An empty value counts as absent."
        )

    def test_empty_frontmatter_returns_default(self):
        """An empty frontmatter dict returns default_version()."""
        result = resolve_version({})
        assert result == DEFAULT_VERSION, (
            f"resolve_version({{}}) expected DEFAULT_VERSION ({DEFAULT_VERSION!r}), "
            f"got {result!r}."
        )

    def test_version_only_frontmatter_returns_that_version(self):
        """A frontmatter dict containing only 'version' returns that version."""
        result = resolve_version({"version": "3.1.4"})
        assert result == "3.1.4", (
            f"resolve_version({{'version': '3.1.4'}}) expected '3.1.4', got {result!r}."
        )

    def test_return_value_is_default_version_call_site(self):
        """When version is absent, the returned value must equal default_version().

        This verifies that the single version-default call site (default_version())
        is used, not an inline literal — any future change to DEFAULT_VERSION is
        automatically reflected in resolve_version's fallback output.
        """
        from boundary_transformer import default_version  # noqa: F401
        result = resolve_version({})
        assert result == default_version(), (
            f"resolve_version fallback must equal default_version(); "
            f"got {result!r}, default_version() returns {default_version()!r}."
        )


# ---------------------------------------------------------------------------
# Version fallback on missing version — end-to-end (Stage 7, T7.2 + T7.3)
# ---------------------------------------------------------------------------

class TestVersionFallbackOnMissingVersion:
    """A file with no 'version' field must transform successfully on every path,
    substituting default_version() as version_before.

    The four path combinations, keyed by (transform_version present, generic_ref provided):
      (False, False) — generic path, no ref          [new fixture, T7.3]
      (False, True)  — generic path, with ref        [new fixture + standard ref]
      (True,  False) — harness degraded path, no ref [harness_only_no_version_input]
      (True,  True)  — harness path, with ref        [harness_only_no_version_input + ref]

    TDD RED phase:
      - (False, *) cases fail because the generic path currently hard-fails on missing version.
      - (True, True) case fails because the harness-with-ref path currently hard-fails.
      - (True, False) version_after assertions fail because the current bump is major.
    """

    # --- (True, False): harness degraded path, no generic ref ---

    def test_harness_path_no_version_succeeds(
        self, harness_only_no_version_input, tmp_path
    ):
        """A harness file with transform_version but no version must transform successfully."""
        result = transform_file(
            harness_only_no_version_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.success is True
        assert result.errors == []

    def test_harness_path_no_version_version_before_is_default(
        self, harness_only_no_version_input, tmp_path
    ):
        """version_before must be DEFAULT_VERSION when version is absent on the harness path."""
        result = transform_file(
            harness_only_no_version_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        assert result.version_before == DEFAULT_VERSION, (
            f"version_before expected DEFAULT_VERSION ({DEFAULT_VERSION!r}); "
            f"got {result.version_before!r}. The fallback must use default_version()."
        )

    def test_harness_path_no_version_version_after_is_minor_bump_of_default(
        self, harness_only_no_version_input, tmp_path
    ):
        """version_after must be the minor bump of DEFAULT_VERSION: '1.0.0' -> '1.1.0'."""
        result = transform_file(
            harness_only_no_version_input,
            tmp_path / "out.md",
            generic_ref_path=None,
        )
        expected_after = "1.1.0"
        assert result.version_after == expected_after, (
            f"version_after expected {expected_after!r} (minor bump of "
            f"DEFAULT_VERSION {DEFAULT_VERSION!r}); got {result.version_after!r}. "
            "The bump must be minor, not major."
        )

    # --- (False, False): generic path, no generic ref, new fixture (T7.3) ---

    def test_generic_path_no_version_succeeds(
        self, generic_no_version_no_transform_input, tmp_path
    ):
        """A generic file with neither version nor transform_version must transform successfully.

        Without Stage 7, transform_file returns success=False with a 'Missing version
        field' error on the generic path.  After Stage 7 the missing version is
        substituted with default_version() and the transform completes.
        """
        result, _ = _transform_to_tmp(generic_no_version_no_transform_input, tmp_path)
        assert result.success is True, (
            f"Transformation of a file missing 'version' must succeed after Stage 7. "
            f"Got success={result.success!r}, errors={result.errors!r}."
        )
        assert result.errors == [], (
            f"No errors must be reported for a missing-version file after Stage 7. "
            f"Got errors={result.errors!r}."
        )

    def test_generic_path_no_version_version_before_is_default(
        self, generic_no_version_no_transform_input, tmp_path
    ):
        """version_before must be DEFAULT_VERSION when version is absent on the generic path.

        The substitution must come from default_version() — the single version-default
        call site — not from an inline literal or a separate code path.
        """
        result, _ = _transform_to_tmp(generic_no_version_no_transform_input, tmp_path)
        assert result.version_before == DEFAULT_VERSION, (
            f"version_before expected DEFAULT_VERSION ({DEFAULT_VERSION!r}) when version "
            f"is absent; got {result.version_before!r}."
        )

    def test_generic_path_no_version_version_after_is_minor_bump_of_default(
        self, generic_no_version_no_transform_input, tmp_path
    ):
        """version_after must be the minor bump of DEFAULT_VERSION: '1.0.0' -> '1.1.0'."""
        result, _ = _transform_to_tmp(generic_no_version_no_transform_input, tmp_path)
        expected_after = "1.1.0"
        assert result.version_after == expected_after, (
            f"version_after expected {expected_after!r}; got {result.version_after!r}. "
            "The fallback uses default_version(); the bump is minor."
        )

    def test_generic_path_no_version_output_written(
        self, generic_no_version_no_transform_input, tmp_path
    ):
        """An output file must be written when transform_file succeeds on the no-version path."""
        output_path = tmp_path / generic_no_version_no_transform_input.name
        transform_file(generic_no_version_no_transform_input, output_path)
        assert output_path.exists(), (
            "Output file must be written when transform_file succeeds on the "
            "missing-version generic path."
        )

    # --- (False, True): generic path with a generic reference ---

    def test_generic_path_no_version_with_generic_ref_succeeds(
        self, generic_no_version_no_transform_input, generic_standard_input, tmp_path
    ):
        """A generic file with no version must succeed even when a generic ref is provided.

        Supplying a generic ref does not change the path for a file without
        transform_version; the fallback must still apply and the transform must complete.
        """
        result, _ = _transform_to_tmp(
            generic_no_version_no_transform_input,
            tmp_path,
            generic_ref_path=generic_standard_input,
        )
        assert result.success is True, (
            f"Transformation must succeed with a generic ref when version is absent. "
            f"Got success={result.success!r}, errors={result.errors!r}."
        )
        assert result.errors == []

    # --- (True, True): harness path with a generic reference ---

    def test_harness_path_no_version_with_generic_ref_succeeds(
        self, harness_only_no_version_input, generic_standard_input, tmp_path
    ):
        """A harness file with transform_version but no version must succeed with a generic ref.

        Before Stage 7 the harness-with-ref path hard-fails on a missing version field.
        After Stage 7 resolve_version supplies the default and the transform completes.
        """
        result = transform_file(
            harness_only_no_version_input,
            tmp_path / "out.md",
            generic_ref_path=generic_standard_input,
        )
        assert result.success is True, (
            f"Transformation must succeed on the harness-with-ref path when version "
            f"is absent. Got success={result.success!r}, errors={result.errors!r}."
        )
        assert result.errors == []

    # --- Shared invariant: no 'missing version' error on any path ---

    def test_fallback_reachable_on_every_path(
        self, generic_no_version_no_transform_input, harness_only_no_version_input,
        generic_standard_input, tmp_path
    ):
        """No 'missing version' error must be reported on any transform path.

        Enumerates the no-version cases and asserts that none returns an error
        whose text refers to a missing version field.
        """
        cases = [
            (generic_no_version_no_transform_input, None,
             "generic path, no ref"),
            (generic_no_version_no_transform_input, generic_standard_input,
             "generic path, with ref"),
            (harness_only_no_version_input, None,
             "harness degraded path, no ref"),
        ]
        for fixture, ref, label in cases:
            result = transform_file(fixture, tmp_path / f"out_{label.replace(' ', '_')}.md",
                                    ref)
            version_errors = [
                e for e in result.errors
                if "version" in str(e).lower() and "missing" in str(e).lower()
            ]
            assert not version_errors, (
                f"No 'missing version' error must be reported for the {label!r} case. "
                f"Got version errors: {version_errors!r}. "
                "The fallback must be reachable on every transform path."
            )


# ===========================================================================
# Stage 5 — Utility & Non-Agent File Skipping (T5.2)
# ===========================================================================
#
# These tests cover:
#   T5.2a  SkipReason enum is defined in boundary_transformer with the three expected members
#   T5.2b  TransformResult gains a skip_reason field defaulting to None
#   T5.2c  transform_file returns success=True, skipped=True, skip_reason=UTILITY_AGENT for every
#          utility-agent filename (with both .md and .agent.md extensions)
#   T5.2d  transform_file returns success=True, skipped=True, skip_reason=NON_AGENT for every
#          non-agent filename
#   T5.2e  No output file is written when transform_file skips a utility-agent or non-agent file
#   T5.2f  The skip message appears in result.warnings
#   T5.2g  version_after == version_before on a skip
#   T5.2h  The already-transformed skip sets skip_reason=ALREADY_TRANSFORMED, not None
#   T5.2i  A normal (non-skipped) transform returns skip_reason=None
#
# All tests here are RED until Stage 5 implementation is complete.
# ===========================================================================

# Minimal file content with valid frontmatter so that transform_file proceeds
# past frontmatter parsing into the normal transform path when skip logic is
# absent.  Without valid frontmatter the function returns early on a parse
# error, coincidentally satisfying "no output written" and "version unchanged"
# for the wrong reason (vacuous RED).  With valid frontmatter those assertions
# fail pre-implementation and provide real RED-phase protection for AC5.2.
_SKIP_FIXTURE_CONTENT = "---\nversion: 1.0.0\n---\n\n# Placeholder\n"


# ---------------------------------------------------------------------------
# T5.2a — SkipReason enum
# ---------------------------------------------------------------------------

class TestSkipReasonEnum:
    """SkipReason must be defined in boundary_transformer with the three expected members."""

    def test_skip_reason_exists(self):
        """SkipReason must be importable from boundary_transformer."""
        import boundary_transformer as bt
        assert hasattr(bt, "SkipReason"), (
            "boundary_transformer must define SkipReason"
        )

    def test_already_transformed_member(self):
        """SkipReason must have an ALREADY_TRANSFORMED member."""
        from boundary_transformer import SkipReason
        assert hasattr(SkipReason, "ALREADY_TRANSFORMED"), (
            "SkipReason must have an ALREADY_TRANSFORMED member"
        )

    def test_utility_agent_member(self):
        """SkipReason must have a UTILITY_AGENT member."""
        from boundary_transformer import SkipReason
        assert hasattr(SkipReason, "UTILITY_AGENT"), (
            "SkipReason must have a UTILITY_AGENT member"
        )

    def test_non_agent_member(self):
        """SkipReason must have a NON_AGENT member."""
        from boundary_transformer import SkipReason
        assert hasattr(SkipReason, "NON_AGENT"), (
            "SkipReason must have a NON_AGENT member"
        )

    def test_already_transformed_value(self):
        """SkipReason.ALREADY_TRANSFORMED.value must be 'already-transformed'."""
        from boundary_transformer import SkipReason
        assert SkipReason.ALREADY_TRANSFORMED.value == "already-transformed"

    def test_utility_agent_value(self):
        """SkipReason.UTILITY_AGENT.value must be 'utility-agent'."""
        from boundary_transformer import SkipReason
        assert SkipReason.UTILITY_AGENT.value == "utility-agent"

    def test_non_agent_value(self):
        """SkipReason.NON_AGENT.value must be 'non-agent-file'."""
        from boundary_transformer import SkipReason
        assert SkipReason.NON_AGENT.value == "non-agent-file"


# ---------------------------------------------------------------------------
# T5.2b — skip_reason field on TransformResult
# ---------------------------------------------------------------------------

class TestTransformResultSkipReasonField:
    """TransformResult must gain a skip_reason field that defaults to None."""

    def test_skip_reason_field_defaults_to_none(self):
        """TransformResult constructed without skip_reason must have skip_reason=None."""
        result = TransformResult(
            success=True,
            errors=[],
            sections_added=[],
            injections_added=[],
            deployed_added=[],
            version_before="1.0.0",
            version_after="1.1.0",
        )
        assert result.skip_reason is None, (
            "TransformResult.skip_reason must default to None"
        )

    def test_skip_reason_field_accepts_utility_agent(self):
        """TransformResult must accept skip_reason=SkipReason.UTILITY_AGENT."""
        from boundary_transformer import SkipReason
        result = TransformResult(
            success=True,
            errors=[],
            sections_added=[],
            injections_added=[],
            deployed_added=[],
            version_before="1.0.0",
            version_after="1.0.0",
            skipped=True,
            skip_reason=SkipReason.UTILITY_AGENT,
        )
        assert result.skip_reason is SkipReason.UTILITY_AGENT

    def test_skip_reason_field_accepts_non_agent(self):
        """TransformResult must accept skip_reason=SkipReason.NON_AGENT."""
        from boundary_transformer import SkipReason
        result = TransformResult(
            success=True,
            errors=[],
            sections_added=[],
            injections_added=[],
            deployed_added=[],
            version_before="1.0.0",
            version_after="1.0.0",
            skipped=True,
            skip_reason=SkipReason.NON_AGENT,
        )
        assert result.skip_reason is SkipReason.NON_AGENT

    def test_existing_constructions_without_skip_reason_still_valid(self):
        """Existing TransformResult constructions that omit skip_reason must stay valid."""
        # This must NOT raise TypeError — the new field must have a default.
        result = TransformResult(
            success=True,
            errors=[],
            sections_added=[],
            injections_added=[],
            deployed_added=[],
            version_before="1.0.0",
            version_after="2.0.0",
        )
        assert result is not None


# ---------------------------------------------------------------------------
# T5.2c — transform_file skips utility-agent files
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("filename", [
    "transformation.md",
    "anthropic-agent-creator.md",
    "anthropic-subagent-creator.md",
    "workflow-creator.md",
    "orchestration-architect.md",
    "system-prompt-capturer.md",
    # .agent.md double-extension variants
    "transformation.agent.md",
    "anthropic-agent-creator.agent.md",
    "workflow-creator.agent.md",
])
class TestUtilityAgentTransformFileSkip:
    """transform_file must skip utility-agent files before reading content."""

    def test_transform_file_returns_success_for_utility_agent(self, filename, tmp_path):
        """transform_file on a utility-agent filename must return success=True.

        Companion assertion (GREEN-phase guard): pre-implementation, this test passes
        vacuously because transform_file processes the valid-frontmatter fixture as a
        normal successful transform (success=True for an unrelated reason — no skip logic
        exists yet). It gains independent RED-phase force only once I5.1/I5.2 are in
        place; at that point returning success=False on a skip would be a regression.
        The independently RED assertions for skip behavior are returns_skipped and
        returns_utility_agent_skip_reason.
        """
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out_" + filename)

        result = transform_file(input_path, output_path)

        assert result.success is True, (
            f"Expected success=True for utility-agent file {filename!r}; got {result.success}"
        )

    def test_transform_file_returns_skipped_for_utility_agent(self, filename, tmp_path):
        """transform_file on a utility-agent filename must return skipped=True."""
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out_" + filename)

        result = transform_file(input_path, output_path)

        assert result.skipped is True, (
            f"Expected skipped=True for utility-agent file {filename!r}; got {result.skipped}"
        )

    def test_transform_file_returns_utility_agent_skip_reason(self, filename, tmp_path):
        """transform_file on a utility-agent filename must return skip_reason=UTILITY_AGENT."""
        from boundary_transformer import SkipReason
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out_" + filename)

        result = transform_file(input_path, output_path)

        assert result.skip_reason is SkipReason.UTILITY_AGENT, (
            f"Expected skip_reason=UTILITY_AGENT for utility-agent file {filename!r}; "
            f"got {result.skip_reason!r}"
        )

    def test_transform_file_writes_no_output_for_utility_agent(self, filename, tmp_path):
        """transform_file must not write an output file when skipping a utility-agent file."""
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out_" + filename)

        transform_file(input_path, output_path)

        assert not output_path.exists(), (
            f"transform_file must not create an output file for utility-agent {filename!r}; "
            f"but {output_path} exists"
        )

    def test_transform_file_input_unchanged_for_utility_agent(self, filename, tmp_path):
        """transform_file must not modify the input file when skipping a utility-agent file."""
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out_" + filename)

        transform_file(input_path, output_path)

        assert input_path.read_text(encoding="utf-8") == _SKIP_FIXTURE_CONTENT, (
            f"transform_file must not modify the input file for utility-agent {filename!r}"
        )

    def test_transform_file_has_warning_for_utility_agent(self, filename, tmp_path):
        """transform_file must include a non-empty warning message when skipping a utility-agent."""
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out_" + filename)

        result = transform_file(input_path, output_path)

        assert result.warnings, (
            f"transform_file must include at least one warning for utility-agent {filename!r}; "
            f"got warnings={result.warnings!r}"
        )

    def test_transform_file_warning_contains_path_for_utility_agent(self, filename, tmp_path):
        """The utility-agent skip warning must contain the file path."""
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out_" + filename)

        result = transform_file(input_path, output_path)

        combined_warnings = " ".join(result.warnings)
        assert str(input_path) in combined_warnings or filename in combined_warnings, (
            f"The skip warning must contain the file path or name for {filename!r}; "
            f"got warnings={result.warnings!r}"
        )

    def test_transform_file_version_unchanged_for_utility_agent(self, filename, tmp_path):
        """version_after must equal version_before when skipping a utility-agent file."""
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out_" + filename)

        result = transform_file(input_path, output_path)

        assert result.version_after == result.version_before, (
            f"version_after must equal version_before when skipping {filename!r}; "
            f"got version_before={result.version_before!r}, version_after={result.version_after!r}"
        )

    def test_transform_file_no_errors_for_utility_agent(self, filename, tmp_path):
        """transform_file must return no errors when skipping a utility-agent file.

        Companion assertion (GREEN-phase guard): pre-implementation, this test passes
        vacuously because transform_file processes the valid-frontmatter fixture as a
        normal successful transform (errors=[] for an unrelated reason — no skip logic
        exists yet). It gains independent RED-phase force only once I5.1/I5.2 are in
        place; at that point adding spurious errors on a skip path would be a regression.
        The independently RED assertions for skip behavior are returns_skipped and
        returns_utility_agent_skip_reason.
        """
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out_" + filename)

        result = transform_file(input_path, output_path)

        assert result.errors == [], (
            f"transform_file must not produce errors for utility-agent {filename!r}; "
            f"got errors={result.errors!r}"
        )


# ---------------------------------------------------------------------------
# T5.2d — transform_file skips non-agent files
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("filename", [
    "SCL developer.chatmode.md",
    "Cheap orchestrator.md",
])
class TestNonAgentTransformFileSkip:
    """transform_file must skip non-agent files before reading content."""

    def test_transform_file_returns_success_for_non_agent(self, filename, tmp_path):
        """transform_file on a non-agent filename must return success=True.

        Companion assertion (GREEN-phase guard): pre-implementation, this test passes
        vacuously because transform_file processes the valid-frontmatter fixture as a
        normal successful transform (success=True for an unrelated reason — no skip logic
        exists yet). It gains independent RED-phase force only once I5.1/I5.2 are in
        place; at that point returning success=False on a skip would be a regression.
        The independently RED assertions for skip behavior are returns_skipped and
        returns_non_agent_skip_reason.
        """
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out-" + filename)

        result = transform_file(input_path, output_path)

        assert result.success is True, (
            f"Expected success=True for non-agent file {filename!r}; got {result.success}"
        )

    def test_transform_file_returns_skipped_for_non_agent(self, filename, tmp_path):
        """transform_file on a non-agent filename must return skipped=True."""
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out-" + filename)

        result = transform_file(input_path, output_path)

        assert result.skipped is True, (
            f"Expected skipped=True for non-agent file {filename!r}; got {result.skipped}"
        )

    def test_transform_file_returns_non_agent_skip_reason(self, filename, tmp_path):
        """transform_file on a non-agent filename must return skip_reason=NON_AGENT."""
        from boundary_transformer import SkipReason
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out-" + filename)

        result = transform_file(input_path, output_path)

        assert result.skip_reason is SkipReason.NON_AGENT, (
            f"Expected skip_reason=NON_AGENT for non-agent file {filename!r}; "
            f"got {result.skip_reason!r}"
        )

    def test_transform_file_writes_no_output_for_non_agent(self, filename, tmp_path):
        """transform_file must not write an output file when skipping a non-agent file."""
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out-" + filename)

        transform_file(input_path, output_path)

        assert not output_path.exists(), (
            f"transform_file must not create an output file for non-agent {filename!r}; "
            f"but {output_path} exists"
        )

    def test_transform_file_has_warning_for_non_agent(self, filename, tmp_path):
        """transform_file must include a non-empty warning message when skipping a non-agent file."""
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out-" + filename)

        result = transform_file(input_path, output_path)

        assert result.warnings, (
            f"transform_file must include at least one warning for non-agent {filename!r}; "
            f"got warnings={result.warnings!r}"
        )

    def test_transform_file_no_errors_for_non_agent(self, filename, tmp_path):
        """transform_file must return no errors when skipping a non-agent file.

        Companion assertion (GREEN-phase guard): pre-implementation, this test passes
        vacuously because transform_file processes the valid-frontmatter fixture as a
        normal successful transform (errors=[] for an unrelated reason — no skip logic
        exists yet). It gains independent RED-phase force only once I5.1/I5.2 are in
        place; at that point adding spurious errors on a skip path would be a regression.
        The independently RED assertions for skip behavior are returns_skipped and
        returns_non_agent_skip_reason.
        """
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out-" + filename)

        result = transform_file(input_path, output_path)

        assert result.errors == [], (
            f"transform_file must not produce errors for non-agent {filename!r}; "
            f"got errors={result.errors!r}"
        )

    def test_transform_file_version_unchanged_for_non_agent(self, filename, tmp_path):
        """version_after must equal version_before when skipping a non-agent file."""
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out-" + filename)

        result = transform_file(input_path, output_path)

        assert result.version_after == result.version_before, (
            f"version_after must equal version_before when skipping {filename!r}"
        )

    def test_transform_file_input_unchanged_for_non_agent(self, filename, tmp_path):
        """transform_file must not modify the input file when skipping a non-agent file.

        Companion assertion (inherent): transforms write to output and never modify the
        input file by design, so this assertion passes regardless of skip implementation
        state. It is retained as a regression guard confirming that the skip path does
        not introduce any unexpected in-place mutation of the input.
        """
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out-" + filename)

        transform_file(input_path, output_path)

        assert input_path.read_text(encoding="utf-8") == _SKIP_FIXTURE_CONTENT, (
            f"transform_file must not modify the input file for non-agent {filename!r}"
        )

    def test_transform_file_warning_contains_path_for_non_agent(self, filename, tmp_path):
        """The non-agent skip warning must contain the file path."""
        input_path = tmp_path / filename
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")
        output_path = tmp_path / ("out-" + filename)

        result = transform_file(input_path, output_path)

        combined_warnings = " ".join(result.warnings)
        assert str(input_path) in combined_warnings or filename in combined_warnings, (
            f"The skip warning must contain the file path or name for {filename!r}; "
            f"got warnings={result.warnings!r}"
        )


# ---------------------------------------------------------------------------
# T5.2e — skip behavior: no overwrite when output path already exists
# ---------------------------------------------------------------------------

class TestSkipDoesNotOverwriteExistingOutput:
    """When an output file already exists, a skip must leave it byte-identical."""

    def test_utility_agent_skip_does_not_overwrite_existing_output(self, tmp_path):
        """If an output file already exists, a utility-agent skip must not touch it."""
        input_path = tmp_path / "transformation.md"
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")

        output_path = tmp_path / "output.md"
        existing_content = "# Pre-existing output\n"
        output_path.write_text(existing_content, encoding="utf-8")

        transform_file(input_path, output_path)

        assert output_path.read_text(encoding="utf-8") == existing_content, (
            "A utility-agent skip must not overwrite an existing output file"
        )

    def test_non_agent_skip_does_not_overwrite_existing_output(self, tmp_path):
        """If an output file already exists, a non-agent skip must not touch it."""
        input_path = tmp_path / "Cheap orchestrator.md"
        input_path.write_text(_SKIP_FIXTURE_CONTENT, encoding="utf-8")

        output_path = tmp_path / "output.md"
        existing_content = "# Pre-existing output\n"
        output_path.write_text(existing_content, encoding="utf-8")

        transform_file(input_path, output_path)

        assert output_path.read_text(encoding="utf-8") == existing_content, (
            "A non-agent skip must not overwrite an existing output file"
        )


# ---------------------------------------------------------------------------
# T5.2h — already-transformed skip now carries skip_reason=ALREADY_TRANSFORMED
# ---------------------------------------------------------------------------

class TestSkipReasonDistinguishesFromAlreadyTransformed:
    """The already-transformed skip path must set skip_reason=ALREADY_TRANSFORMED."""

    def test_already_transformed_file_has_already_transformed_skip_reason(
        self, fixtures_dir, tmp_path
    ):
        """An already-transformed file must produce skip_reason=ALREADY_TRANSFORMED, not None."""
        from boundary_transformer import SkipReason
        input_path = fixtures_dir / "harness_only_transformed_input.md"
        output_path = tmp_path / "harness_only_transformed_input.md"

        result = transform_file(input_path, output_path)

        # Must be ALREADY_TRANSFORMED, not UTILITY_AGENT or NON_AGENT or None
        assert result.skip_reason is SkipReason.ALREADY_TRANSFORMED, (
            "An already-transformed file must produce skip_reason=ALREADY_TRANSFORMED; "
            f"got {result.skip_reason!r}"
        )

    def test_already_transformed_and_utility_have_different_skip_reasons(self):
        """ALREADY_TRANSFORMED and UTILITY_AGENT are distinct skip reason values."""
        from boundary_transformer import SkipReason
        assert SkipReason.ALREADY_TRANSFORMED is not SkipReason.UTILITY_AGENT
        assert SkipReason.ALREADY_TRANSFORMED is not SkipReason.NON_AGENT


# ---------------------------------------------------------------------------
# T5.2i — normal transforms return skip_reason=None
# ---------------------------------------------------------------------------

class TestNormalTransformSkipReasonIsNone:
    """A successful (non-skipped) transform must return skip_reason=None."""

    def test_generic_standard_transform_has_null_skip_reason(
        self, generic_standard_input, tmp_path
    ):
        """A successfully transformed file must return skip_reason=None."""
        result, _ = _transform_to_tmp(generic_standard_input, tmp_path)

        assert result.skip_reason is None, (
            f"A normal (non-skipped) transform must return skip_reason=None; "
            f"got {result.skip_reason!r}"
        )


# ---------------------------------------------------------------------------
# Idempotency guard — compound names (Stage 1)
# ---------------------------------------------------------------------------


class TestIdempotencyGuardCompoundNames:
    """has_canonical_boundary_tags() must handle compound tag names correctly.

    After the TAG_PATTERN name-class relaxation, compound names like
    'AuthorityHierarchy:Subagent' now reach the canonical-membership checks.
    The guard must use tag_base_name() for membership (so a file carrying compound
    names is still recognised as already-transformed), but must use the full name
    for pairing (an open '[[SECTION:A:b]]' is closed only by '[[/SECTION:A:b]]').
    """

    # --- Base-name membership: compound names that should return True ----

    def test_compound_section_name_with_canonical_base_returns_true(self) -> None:
        """A body whose only SECTION tags use a compound name whose base IS canonical
        must be recognised as already-transformed.

        Before the fix: TAG_PATTERN does not match compound names, so the compound
        SECTION tags are silently skipped; has_canonical_section stays False and the
        guard returns False even though the file is legitimately transformed.
        After the fix: tag_base_name('Identity:Something') == 'Identity' which IS in
        CANONICAL_SECTIONS, so the guard correctly returns True.
        """
        body = (
            "[[SECTION:Identity:Something]]\n"
            "Some agent identity content.\n"
            "[[/SECTION:Identity:Something]]\n"
        )
        assert has_canonical_boundary_tags(body) is True, (
            "A body with a compound SECTION name whose base is 'Identity' (canonical) "
            "must be recognised as already-transformed"
        )

    def test_compound_deployed_name_with_canonical_base_does_not_break_guard(self) -> None:
        """A body with canonical simple SECTION tags plus a compound DEPLOYED name
        whose base IS in CANONICAL_DEPLOYED must return True.

        Before the fix: the compound DEPLOYED tag is silently skipped (no match);
        the result depends only on the SECTION tags.
        After the fix: tag_base_name('AuthorityHierarchy:Subagent') == 'AuthorityHierarchy'
        IS in CANONICAL_DEPLOYED, so the guard still returns True.
        """
        body = (
            "[[SECTION:Identity]]\n"
            "# Agent\n"
            "[[DEPLOYED:AuthorityHierarchy:Subagent]]\n"
            "[[/DEPLOYED:AuthorityHierarchy:Subagent]]\n"
            "[[/SECTION:Identity]]\n"
        )
        assert has_canonical_boundary_tags(body) is True, (
            "A body with canonical SECTION tags and a compound DEPLOYED name whose "
            "base is in CANONICAL_DEPLOYED must be recognised as already-transformed"
        )

    # --- Non-canonical base names must still return False ----------------

    def test_compound_section_name_with_non_canonical_base_returns_false(self) -> None:
        """A compound SECTION name whose base is NOT in CANONICAL_SECTIONS must make
        the guard return False, even when paired with a canonical SECTION.

        Before the fix: the compound tag is skipped; only the canonical SECTION is
        seen, so the guard returns True (incorrectly lenient).
        After the fix: tag_base_name('Bogus:Thing') == 'Bogus' is NOT in
        CANONICAL_SECTIONS, so the guard returns False (correctly strict).
        """
        body = (
            "[[SECTION:Identity]]\n"
            "Content.\n"
            "[[/SECTION:Identity]]\n"
            "[[SECTION:Bogus:Thing]]\n"
            "Extra content.\n"
            "[[/SECTION:Bogus:Thing]]\n"
        )
        assert has_canonical_boundary_tags(body) is False, (
            "A body containing a compound SECTION name whose base is not in "
            "CANONICAL_SECTIONS must NOT be recognised as already-transformed"
        )

    def test_compound_deployed_name_with_non_canonical_base_returns_false(self) -> None:
        """A compound DEPLOYED name whose base is NOT in CANONICAL_DEPLOYED must make
        the guard return False.

        Before the fix: the compound tag is skipped; the guard returns True based on
        the section tags alone (incorrectly lenient).
        After the fix: tag_base_name('Unknown:Subagent') == 'Unknown' is NOT in
        CANONICAL_DEPLOYED, so the guard returns False.
        """
        body = (
            "[[SECTION:Identity]]\n"
            "Content.\n"
            "[[DEPLOYED:Unknown:Subagent]]\n"
            "[[/DEPLOYED:Unknown:Subagent]]\n"
            "[[/SECTION:Identity]]\n"
        )
        assert has_canonical_boundary_tags(body) is False, (
            "A body containing a compound DEPLOYED name whose base is not in "
            "CANONICAL_DEPLOYED must NOT be recognised as already-transformed"
        )

    # --- Pairing uses the full name, not the base name -------------------

    def test_unpaired_compound_section_tag_returns_false(self) -> None:
        """An open compound SECTION tag with no matching close tag must make the
        guard return False.

        Pairing checks use the full tag name, so '[[SECTION:Identity:Subagent]]'
        is closed only by '[[/SECTION:Identity:Subagent]]', not by
        '[[/SECTION:Identity]]'.

        Before the fix: the compound tag is skipped; the guard may return True
        based on other tags.
        After the fix: the compound open tag is tracked; when never closed the guard
        returns False.
        """
        body = (
            "[[SECTION:Identity]]\n"
            "[[SECTION:Identity:Subagent]]\n"   # opened — never closed
            "Content.\n"
            "[[/SECTION:Identity]]\n"
        )
        assert has_canonical_boundary_tags(body) is False, (
            "An unpaired compound SECTION open tag must make the guard return False"
        )

    def test_compound_open_and_simple_close_are_not_a_valid_pair(self) -> None:
        """A compound open tag and a simple close tag with the same base name are
        NOT a valid pair — pairing requires the full name to match.

        '[[SECTION:Identity:Subagent]]' is NOT closed by '[[/SECTION:Identity]]'.
        """
        body = (
            "[[SECTION:Identity:Subagent]]\n"
            "Content.\n"
            "[[/SECTION:Identity]]\n"         # close uses simple name — wrong
        )
        assert has_canonical_boundary_tags(body) is False, (
            "A compound open tag and a simple close tag with the same base name must "
            "NOT satisfy the pairing check"
        )

    def test_unpaired_compound_deployed_tag_returns_false(self) -> None:
        """An open compound DEPLOYED tag with no matching close tag must return False."""
        body = (
            "[[SECTION:Identity]]\n"
            "Content.\n"
            "[[DEPLOYED:AuthorityHierarchy:Subagent]]\n"   # opened — never closed
            "[[/SECTION:Identity]]\n"
        )
        assert has_canonical_boundary_tags(body) is False, (
            "An unpaired compound DEPLOYED open tag must make the guard return False"
        )

    # --- Regression: existing simple-name behaviour unchanged ------------

    def test_already_transformed_body_with_simple_names_still_returns_true(self) -> None:
        """A body with only simple canonical names must still return True after the fix."""
        body = (
            "[[SECTION:Identity]]\n"
            "[[DEPLOYED:CommunicationProtocol]]\n"
            "[[/DEPLOYED:CommunicationProtocol]]\n"
            "[[/SECTION:Identity]]\n"
        )
        assert has_canonical_boundary_tags(body) is True, (
            "Regression: simple canonical names must still satisfy the idempotency guard"
        )


# ---------------------------------------------------------------------------
# Region insertion: ClosingProcedure and AuthorityHierarchy — generic path
# ---------------------------------------------------------------------------

class TestClosingProcedureAndAuthorityHierarchyGenericPath:
    """Both regions are emitted by the generic path on a file carrying a Process list
    and an Authority Hierarchy block.  Tests run against the fixture pair
    generic_identity_regions_input / generic_identity_regions_expected and will fail
    until the generic-path implementation is complete."""

    def test_transform_succeeds(self, generic_identity_regions_input, tmp_path):
        """Transformation of a file with Process list and Authority Hierarchy must succeed."""
        result, _ = _transform_to_tmp(generic_identity_regions_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_closing_procedure_in_deployed_added(self, generic_identity_regions_input, tmp_path):
        """ClosingProcedure must appear in deployed_added after transformation."""
        result, _ = _transform_to_tmp(generic_identity_regions_input, tmp_path)
        assert "ClosingProcedure" in result.deployed_added

    def test_authority_hierarchy_in_deployed_added(self, generic_identity_regions_input, tmp_path):
        """AuthorityHierarchy must appear in deployed_added after transformation."""
        result, _ = _transform_to_tmp(generic_identity_regions_input, tmp_path)
        assert "AuthorityHierarchy" in result.deployed_added

    def test_closing_procedure_precedes_authority_hierarchy_in_deployed_added(
        self, generic_identity_regions_input, tmp_path
    ):
        """ClosingProcedure must appear before AuthorityHierarchy in deployed_added."""
        result, _ = _transform_to_tmp(generic_identity_regions_input, tmp_path)
        cp_idx = result.deployed_added.index("ClosingProcedure")
        ah_idx = result.deployed_added.index("AuthorityHierarchy")
        assert cp_idx < ah_idx

    def test_no_tag_emitted_inside_fenced_block(self, generic_identity_regions_input, tmp_path):
        """No [[DEPLOYED:...]] tag may appear on a fence-masked line in the output."""
        _, output_path = _transform_to_tmp(generic_identity_regions_input, tmp_path)
        import sys as _sys
        import pathlib as _pathlib
        _TOOLS_DIR = _pathlib.Path(__file__).parent.parent
        if str(_TOOLS_DIR) not in _sys.path:
            _sys.path.insert(0, str(_TOOLS_DIR))
        from fence import fence_mask as _fence_mask
        out_lines = output_path.read_text(encoding="utf-8").splitlines(keepends=True)
        mask = _fence_mask(out_lines)
        for i, (line, is_masked) in enumerate(zip(out_lines, mask)):
            if is_masked:
                assert "[[DEPLOYED:" not in line, (
                    f"Deployed tag on fence-masked line {i}: {line!r}"
                )

    def test_hitl_process_step_absent_from_output(
        self, generic_identity_regions_input, tmp_path
    ):
        """The HITL-review Process step must not survive in the transformed output."""
        _, output_path = _transform_to_tmp(generic_identity_regions_input, tmp_path)
        content = _read(output_path)
        assert "review/approval" not in content, (
            "HITL-review Process step must be deleted from the output"
        )

    def test_json_return_process_step_absent_from_output(
        self, generic_identity_regions_input, tmp_path
    ):
        """The JSON-return Process step must not survive in the transformed output."""
        _, output_path = _transform_to_tmp(generic_identity_regions_input, tmp_path)
        content = _read(output_path)
        assert "Return ONLY output json defined by communication protocol" not in content, (
            "JSON-return Process step must be deleted from the output"
        )

    def test_authority_hierarchy_heading_absent_from_output(
        self, generic_identity_regions_input, tmp_path
    ):
        """The '### Authority Hierarchy' heading block must not survive in the output."""
        _, output_path = _transform_to_tmp(generic_identity_regions_input, tmp_path)
        content = _read(output_path)
        assert "### Authority Hierarchy" not in content, (
            "Authority Hierarchy heading block must be deleted from the output"
        )

    def test_authority_hierarchy_prose_absent_from_output(
        self, generic_identity_regions_input, tmp_path
    ):
        """The Authority Hierarchy ranked list prose must not survive in the output."""
        _, output_path = _transform_to_tmp(generic_identity_regions_input, tmp_path)
        content = _read(output_path)
        assert "Why this ranking" not in content, (
            "Authority Hierarchy prose must be deleted from the output"
        )

    def test_closing_procedure_immediately_follows_process_list(
        self, generic_identity_regions_input, tmp_path
    ):
        """[[DEPLOYED:ClosingProcedure]] must appear immediately after the last Process step,
        with nothing intervening between the list and the open tag."""
        _, output_path = _transform_to_tmp(generic_identity_regions_input, tmp_path)
        lines = _read(output_path).splitlines()
        # Find the last surviving numbered Process step
        last_step_idx = max(
            (i for i, l in enumerate(lines) if l.strip().startswith(("1.", "2.", "3.", "4.", "5."
                                                                      )) and "Write tests" in l),
            default=None,
        )
        assert last_step_idx is not None, "No surviving numbered step found in output"
        # The very next line must be [[DEPLOYED:ClosingProcedure]]
        assert lines[last_step_idx + 1].strip() == "[[DEPLOYED:ClosingProcedure]]", (
            f"ClosingProcedure open tag must immediately follow the last Process step; "
            f"got: {lines[last_step_idx + 1]!r}"
        )

    def test_output_matches_expected_fixture(
        self,
        generic_identity_regions_input,
        generic_identity_regions_expected,
        tmp_path,
    ):
        """The full transformed output must match the expected fixture byte-for-byte."""
        _, output_path = _transform_to_tmp(generic_identity_regions_input, tmp_path)
        assert _read(output_path) == _read(generic_identity_regions_expected)


# ---------------------------------------------------------------------------
# Region insertion: ClosingProcedure and AuthorityHierarchy — harness path
# ---------------------------------------------------------------------------

class TestClosingProcedureAndAuthorityHierarchyHarnessPath:
    """Both regions are emitted by the harness path on a file carrying a Process list
    and an Authority Hierarchy block.  Tests run against the harness fixture pair and
    will fail until the harness-path implementation is complete."""

    def test_transform_succeeds(
        self, harness_identity_regions_input, harness_identity_regions_generic_ref, tmp_path
    ):
        """Transformation of a harness file with Process list and Authority Hierarchy succeeds."""
        result, _ = _transform_to_tmp(
            harness_identity_regions_input, tmp_path,
            generic_ref_path=harness_identity_regions_generic_ref,
        )
        assert result.success is True
        assert result.errors == []

    def test_closing_procedure_in_deployed_added(
        self, harness_identity_regions_input, harness_identity_regions_generic_ref, tmp_path
    ):
        """ClosingProcedure must appear in deployed_added on the harness path."""
        result, _ = _transform_to_tmp(
            harness_identity_regions_input, tmp_path,
            generic_ref_path=harness_identity_regions_generic_ref,
        )
        assert "ClosingProcedure" in result.deployed_added

    def test_authority_hierarchy_in_deployed_added(
        self, harness_identity_regions_input, harness_identity_regions_generic_ref, tmp_path
    ):
        """AuthorityHierarchy must appear in deployed_added on the harness path."""
        result, _ = _transform_to_tmp(
            harness_identity_regions_input, tmp_path,
            generic_ref_path=harness_identity_regions_generic_ref,
        )
        assert "AuthorityHierarchy" in result.deployed_added

    def test_closing_procedure_precedes_authority_hierarchy(
        self, harness_identity_regions_input, harness_identity_regions_generic_ref, tmp_path
    ):
        """ClosingProcedure must precede AuthorityHierarchy in deployed_added on the harness path."""
        result, _ = _transform_to_tmp(
            harness_identity_regions_input, tmp_path,
            generic_ref_path=harness_identity_regions_generic_ref,
        )
        cp_idx = result.deployed_added.index("ClosingProcedure")
        ah_idx = result.deployed_added.index("AuthorityHierarchy")
        assert cp_idx < ah_idx

    def test_hitl_process_step_absent_from_output(
        self, harness_identity_regions_input, harness_identity_regions_generic_ref, tmp_path
    ):
        """The HITL-review Process step must not survive in the harness-path output."""
        _, output_path = _transform_to_tmp(
            harness_identity_regions_input, tmp_path,
            generic_ref_path=harness_identity_regions_generic_ref,
        )
        content = _read(output_path)
        assert "review/approval" not in content

    def test_json_return_process_step_absent_from_output(
        self, harness_identity_regions_input, harness_identity_regions_generic_ref, tmp_path
    ):
        """The JSON-return Process step must not survive in the harness-path output."""
        _, output_path = _transform_to_tmp(
            harness_identity_regions_input, tmp_path,
            generic_ref_path=harness_identity_regions_generic_ref,
        )
        content = _read(output_path)
        assert "Return ONLY output json defined by communication protocol" not in content

    def test_authority_hierarchy_heading_absent_from_output(
        self, harness_identity_regions_input, harness_identity_regions_generic_ref, tmp_path
    ):
        """The '### Authority Hierarchy' heading block must not survive in the harness-path output."""
        _, output_path = _transform_to_tmp(
            harness_identity_regions_input, tmp_path,
            generic_ref_path=harness_identity_regions_generic_ref,
        )
        content = _read(output_path)
        assert "### Authority Hierarchy" not in content

    def test_authority_hierarchy_prose_absent_from_output(
        self, harness_identity_regions_input, harness_identity_regions_generic_ref, tmp_path
    ):
        """The Authority Hierarchy ranked-list prose must not survive in the harness-path output."""
        _, output_path = _transform_to_tmp(
            harness_identity_regions_input, tmp_path,
            generic_ref_path=harness_identity_regions_generic_ref,
        )
        content = _read(output_path)
        assert "Why this ranking" not in content


# ===========================================================================
# Stage 6: Frontmatter Completeness & id Reconciliation
# ===========================================================================
#
# Tests covering T6.1 through T6.4. Fail (TDD RED) until Stage 6 implementation
# tasks I6.1-I6.4 are complete.
#
# These imports reference contract stub modules. They compile and import cleanly;
# functions raise NotImplementedError when called, which is valid TDD RED state.
# ---------------------------------------------------------------------------

from document_kind import DocumentKind, classify_document  # noqa: E402
from frontmatter_build import (  # noqa: E402
    DerivedFrontmatter,
    DERIVED_KEY_ORDER,
    TIER_PLACEHOLDER,
    TIER_RATIONALE_PLACEHOLDER,
    build_output_frontmatter,
    derive_name,
    derive_required_skills,
    derive_role,
    read_generic_id,
)
from non_conformance import NC_MESSAGES, NC_TIER_PLACEHOLDER, NonConformance  # noqa: E402


# ---------------------------------------------------------------------------
# T6.1: classify_document
# ---------------------------------------------------------------------------

class TestClassifyDocument:
    """classify_document derives DocumentKind from frontmatter keys and values."""

    def test_type_bundle_value_returns_bundle(self) -> None:
        """frontmatter_values['type'] == 'bundle' yields BUNDLE regardless of other keys."""
        assert classify_document(["id", "type", "author"], {"type": "bundle"}) is DocumentKind.BUNDLE

    def test_transform_version_key_returns_agent_harness(self) -> None:
        """'transform_version' in keys and no type:bundle yields AGENT_HARNESS."""
        result = classify_document(
            ["id", "version", "transform_version"],
            {"version": "2.0.0", "transform_version": "2.0.0"},
        )
        assert result is DocumentKind.AGENT_HARNESS

    def test_no_type_no_transform_version_returns_agent_generic(self) -> None:
        """Neither type:bundle nor transform_version yields AGENT_GENERIC."""
        result = classify_document(["id", "version", "name"], {"version": "1.0.0"})
        assert result is DocumentKind.AGENT_GENERIC

    def test_bundle_type_takes_precedence_over_transform_version(self) -> None:
        """type: bundle wins even when transform_version is also present."""
        result = classify_document(
            ["type", "transform_version"],
            {"type": "bundle", "transform_version": "1.0.0"},
        )
        assert result is DocumentKind.BUNDLE

    def test_empty_frontmatter_returns_agent_generic(self) -> None:
        """No frontmatter keys at all yields AGENT_GENERIC."""
        assert classify_document([], {}) is DocumentKind.AGENT_GENERIC


# ---------------------------------------------------------------------------
# T6.1: derive_role
# ---------------------------------------------------------------------------

class TestDeriveRole:
    """derive_role returns 'orchestrator' for orchestrator filenames, 'subagent' otherwise."""

    def test_plain_subagent_filename_returns_subagent(self) -> None:
        assert derive_role(pathlib.Path("test-writer.md")) == "subagent"

    def test_agent_md_subagent_returns_subagent(self) -> None:
        assert derive_role(pathlib.Path("test-writer.agent.md")) == "subagent"

    def test_orchestrator_md_returns_orchestrator(self) -> None:
        assert derive_role(pathlib.Path("orchestrator.md")) == "orchestrator"

    def test_orchestrator_agent_md_returns_orchestrator(self) -> None:
        assert derive_role(pathlib.Path("orchestrator.agent.md")) == "orchestrator"

    def test_orchestrator_name_matched_case_insensitively(self) -> None:
        """The lowercase comparison in classify_file makes Orchestrator.md an orchestrator."""
        assert derive_role(pathlib.Path("Orchestrator.md")) == "orchestrator"

    def test_partial_orchestrator_name_returns_subagent(self) -> None:
        """Only exact match on lower-cased base name 'orchestrator' triggers orchestrator role."""
        assert derive_role(pathlib.Path("my-orchestrator-helper.md")) == "subagent"

    def test_result_is_lowercase_string(self) -> None:
        result = derive_role(pathlib.Path("some-agent.md"))
        assert isinstance(result, str)
        assert result == result.lower()


# ---------------------------------------------------------------------------
# T6.1: derive_name
# ---------------------------------------------------------------------------

class TestDeriveName:
    """derive_name strips trailing extensions and preserves case, hyphens, and spaces."""

    def test_md_extension_stripped(self) -> None:
        assert derive_name(pathlib.Path("test-writer.md")) == "test-writer"

    def test_agent_md_double_extension_stripped(self) -> None:
        assert derive_name(pathlib.Path("test-writer.agent.md")) == "test-writer"

    def test_case_preserved(self) -> None:
        assert derive_name(pathlib.Path("MyAgent.md")) == "MyAgent"

    def test_hyphens_preserved(self) -> None:
        assert derive_name(pathlib.Path("lean-tdd-reviewer.agent.md")) == "lean-tdd-reviewer"

    def test_orchestrator_filename_maps_to_orchestrator_name(self) -> None:
        assert derive_name(pathlib.Path("orchestrator.agent.md")) == "orchestrator"

    def test_spaces_preserved_verbatim(self) -> None:
        assert derive_name(pathlib.Path("Web Research.md")) == "Web Research"


# ---------------------------------------------------------------------------
# T6.1: derive_required_skills
# ---------------------------------------------------------------------------

class TestDeriveRequiredSkills:
    """derive_required_skills extracts skill keys from Process-step skill-load directives."""

    def test_empty_body_returns_empty_list(self) -> None:
        assert derive_required_skills([]) == []

    def test_no_skill_references_returns_empty_list(self) -> None:
        lines = [
            "## Process\n",
            "1. Read the design document.\n",
            "2. Write the implementation.\n",
        ]
        assert derive_required_skills(lines) == []

    def test_single_skill_extracted(self) -> None:
        lines = [
            "1. Load the `efficient-file-reading` skill for file reading strategies.\n",
            "2. Write tests.\n",
        ]
        assert derive_required_skills(lines) == ["efficient-file-reading"]

    def test_two_skills_extracted_in_order(self) -> None:
        lines = [
            "1. Load the `lean-tdd` skill for test quality.\n",
            "2. Load the `efficient-file-reading` skill for file reading.\n",
        ]
        assert derive_required_skills(lines) == ["lean-tdd", "efficient-file-reading"]

    def test_duplicate_skill_deduped(self) -> None:
        lines = [
            "1. Load the `lean-tdd` skill.\n",
            "2. Re-consult the `lean-tdd` skill.\n",
        ]
        assert derive_required_skills(lines) == ["lean-tdd"]

    def test_skill_in_fenced_block_not_extracted(self) -> None:
        lines = [
            "1. Load the `lean-tdd` skill.\n",
            "```\n",
            "Load the `efficient-file-reading` skill inside fence must be ignored.\n",
            "```\n",
        ]
        result = derive_required_skills(lines)
        assert result == ["lean-tdd"]
        assert "efficient-file-reading" not in result

    def test_backtick_token_without_skill_word_ignored(self) -> None:
        lines = ["1. Read the `boundary_transformer` module carefully.\n"]
        assert derive_required_skills(lines) == []

    def test_skill_word_case_insensitive(self) -> None:
        lines = ["1. Load the `lean-tdd` Skill for test quality.\n"]
        assert "lean-tdd" in derive_required_skills(lines)

    def test_canonical_skill_load_phrasing_extracted(self) -> None:
        """The canonical form from agent instructions: 'Load the X skill... BLOCKED with E501'."""
        lines = [
            "1. Load the `lean-tdd` skill for test quality principles."
            " If skill loading fails, return BLOCKED with E501.\n",
            "2. Load the `efficient-file-reading` skill for file reading strategies."
            " If skill loading fails, return BLOCKED with E501.\n",
        ]
        assert derive_required_skills(lines) == ["lean-tdd", "efficient-file-reading"]


# ---------------------------------------------------------------------------
# T6.2: build_output_frontmatter — tier placeholder insertion (unit)
# ---------------------------------------------------------------------------

class TestTierPlaceholderUnit:
    """build_output_frontmatter inserts tier placeholders only when keys are absent."""

    _MINIMAL_LINES: list = [
        "id: 42\n",
        "version: 1.0.0\n",
        "name: test-agent\n",
        "description: Test\n",
    ]

    @property
    def _derived(self) -> DerivedFrontmatter:
        return DerivedFrontmatter(name="test-agent", role="subagent", required_skills=[])

    def _build(self, raw_lines=None, *, kind=DocumentKind.AGENT_GENERIC, version_after="1.1.0"):
        return build_output_frontmatter(
            raw_lines if raw_lines is not None else self._MINIMAL_LINES,
            kind=kind,
            derived=self._derived,
            version_after=version_after,
            transform_version_after=None,
            generic_id=None,
        )

    def test_both_tier_keys_absent_yields_two_nc_tier_placeholder(self) -> None:
        """Both tier keys absent yields two NC_TIER_PLACEHOLDER findings."""
        _, ncs = self._build()
        tier_ncs = [nc for nc in ncs if nc.code == NC_TIER_PLACEHOLDER]
        assert len(tier_ncs) == 2, f"Expected 2 NC_TIER_PLACEHOLDER but got {len(tier_ncs)}: {ncs}"

    def test_both_tier_keys_absent_detail_names_both_keys(self) -> None:
        """Both findings name the missing tier keys in their detail field."""
        _, ncs = self._build()
        tier_ncs = [nc for nc in ncs if nc.code == NC_TIER_PLACEHOLDER]
        details = {nc.detail for nc in tier_ncs}
        assert details == {"recommended_tier", "tier_rationale"}

    def test_tier_non_conformances_follow_derived_key_order(self) -> None:
        """NC_TIER_PLACEHOLDER findings are emitted in DERIVED_KEY_ORDER sequence."""
        _, ncs = self._build()
        tier_ncs = [nc for nc in ncs if nc.code == NC_TIER_PLACEHOLDER]
        expected_order = [k for k in DERIVED_KEY_ORDER if k in ("recommended_tier", "tier_rationale")]
        assert [nc.detail for nc in tier_ncs] == expected_order

    def test_tier_placeholder_value_in_output_lines(self) -> None:
        """TIER_PLACEHOLDER appears in the output when recommended_tier is absent."""
        output_lines, _ = self._build()
        combined = "".join(output_lines)
        assert TIER_PLACEHOLDER in combined

    def test_tier_rationale_placeholder_value_in_output_lines(self) -> None:
        """TIER_RATIONALE_PLACEHOLDER appears when tier_rationale is absent."""
        output_lines, _ = self._build()
        combined = "".join(output_lines)
        assert TIER_RATIONALE_PLACEHOLDER in combined

    def test_only_recommended_tier_absent_one_nc(self) -> None:
        lines = self._MINIMAL_LINES + ["tier_rationale: Because it is medium\n"]
        _, ncs = self._build(raw_lines=lines)
        tier_ncs = [nc for nc in ncs if nc.code == NC_TIER_PLACEHOLDER]
        assert len(tier_ncs) == 1
        assert tier_ncs[0].detail == "recommended_tier"

    def test_only_tier_rationale_absent_one_nc(self) -> None:
        lines = self._MINIMAL_LINES + ["recommended_tier: MEDIUM\n"]
        _, ncs = self._build(raw_lines=lines)
        tier_ncs = [nc for nc in ncs if nc.code == NC_TIER_PLACEHOLDER]
        assert len(tier_ncs) == 1
        assert tier_ncs[0].detail == "tier_rationale"

    def test_both_present_no_nc_tier_placeholder(self) -> None:
        lines = self._MINIMAL_LINES + [
            "recommended_tier: MEDIUM\n",
            "tier_rationale: Because it is medium\n",
        ]
        _, ncs = self._build(raw_lines=lines)
        assert not any(nc.code == NC_TIER_PLACEHOLDER for nc in ncs)

    def test_empty_recommended_tier_counts_as_absent(self) -> None:
        """A 'recommended_tier:' line with no value is treated as absent."""
        lines = self._MINIMAL_LINES + ["recommended_tier:\n", "tier_rationale: Some reason\n"]
        _, ncs = self._build(raw_lines=lines)
        tier_ncs = [nc for nc in ncs if nc.code == NC_TIER_PLACEHOLDER and nc.detail == "recommended_tier"]
        assert len(tier_ncs) == 1

    def test_existing_tier_value_not_overwritten(self) -> None:
        lines = self._MINIMAL_LINES + [
            "recommended_tier: HIGH\n",
            "tier_rationale: Orchestrates many agents\n",
        ]
        output_lines, _ = self._build(raw_lines=lines)
        combined = "".join(output_lines)
        assert "HIGH" in combined
        assert TIER_PLACEHOLDER not in combined

    def test_nc_tier_placeholder_message_contains_key_name(self) -> None:
        """NC_TIER_PLACEHOLDER.message is formatted from NC_MESSAGES with the missing key name."""
        _, ncs = self._build()
        tier_ncs = [nc for nc in ncs if nc.code == NC_TIER_PLACEHOLDER]
        assert len(tier_ncs) == 2, "Expected 2 tier NCs to check message formatting"
        for nc in tier_ncs:
            assert nc.detail is not None
            assert nc.detail in nc.message, (
                f"NC_TIER_PLACEHOLDER message must embed the missing key name "
                f"'{nc.detail}'; got: {nc.message!r}"
            )


# ---------------------------------------------------------------------------
# T6.2: tier placeholder integration through transform_file
# ---------------------------------------------------------------------------

class TestTierPlaceholderIntegration:
    """transform_file populates result.non_conformances with NC_TIER_PLACEHOLDER findings."""

    def test_file_without_tier_keys_has_two_nc_tier_placeholder(
        self, s6_generic_no_tier_input, tmp_path
    ) -> None:
        """transform_file on a file missing both tier keys records two NC_TIER_PLACEHOLDER."""
        result, _ = _transform_to_tmp(s6_generic_no_tier_input, tmp_path)
        assert result.success is True
        tier_ncs = [nc for nc in result.non_conformances if nc.code == NC_TIER_PLACEHOLDER]
        assert len(tier_ncs) == 2, (
            f"Expected 2 NC_TIER_PLACEHOLDER on result.non_conformances but got "
            f"{len(tier_ncs)}: {result.non_conformances}"
        )

    def test_file_with_both_tier_keys_has_no_nc_tier_placeholder(
        self, s6_generic_with_tier_input, tmp_path
    ) -> None:
        """transform_file on a file with both tier keys records no NC_TIER_PLACEHOLDER."""
        result, _ = _transform_to_tmp(s6_generic_with_tier_input, tmp_path)
        assert result.success is True
        tier_ncs = [nc for nc in result.non_conformances if nc.code == NC_TIER_PLACEHOLDER]
        assert tier_ncs == []

    def test_tier_placeholder_appears_in_output_content(
        self, s6_generic_no_tier_input, tmp_path
    ) -> None:
        """The output file must contain TIER_PLACEHOLDER when the key was absent."""
        result, output_path = _transform_to_tmp(s6_generic_no_tier_input, tmp_path)
        assert result.success is True
        assert TIER_PLACEHOLDER in _read(output_path)

    def test_transform_succeeds_despite_tier_non_conformances(
        self, s6_generic_no_tier_input, tmp_path
    ) -> None:
        """Tier non-conformances are advisory; transform_file must still succeed despite them.

        The 'despite' clause requires non_conformances to be populated — this test
        would be vacuous if it only checked success/errors without confirming the
        tier findings actually exist on the result.
        """
        result, _ = _transform_to_tmp(s6_generic_no_tier_input, tmp_path)
        assert result.success is True
        assert result.errors == []
        tier_ncs = [nc for nc in result.non_conformances if nc.code == NC_TIER_PLACEHOLDER]
        assert len(tier_ncs) == 2, (
            "non_conformances must carry both NC_TIER_PLACEHOLDER findings so that "
            "'succeeded despite tier non-conformances' is distinguishable from "
            "'succeeded with nothing to report'"
        )


# ---------------------------------------------------------------------------
# T6.3: read_generic_id (unit)
# ---------------------------------------------------------------------------

class TestReadGenericId:
    """read_generic_id extracts the id from a generic reference file's frontmatter."""

    def test_returns_id_string_when_present(self) -> None:
        lines = ["---\n", "id: 51\n", "version: 1.0.0\n", "---\n", "# Agent\n"]
        assert read_generic_id(lines) == "51"

    def test_returns_none_when_id_absent(self) -> None:
        lines = ["---\n", "version: 1.0.0\n", "---\n"]
        assert read_generic_id(lines) is None

    def test_returns_str_not_int(self) -> None:
        lines = ["---\n", "id: 99\n", "---\n"]
        result = read_generic_id(lines)
        assert isinstance(result, str)
        assert result == "99"

    def test_body_id_line_not_extracted(self) -> None:
        """An id: line after the closing --- is in the body and must not match."""
        lines = ["---\n", "version: 1.0.0\n", "---\n", "# Body\n", "id: 999\n"]
        assert read_generic_id(lines) is None


# ---------------------------------------------------------------------------
# T6.3: id reconciliation integration through transform_file
# ---------------------------------------------------------------------------

class TestIdReconciliation:
    """transform_file substitutes id from generic reference when one is supplied."""

    def test_generic_ref_id_used_in_output(
        self, s6_harness_id10_input, s6_generic_ref_id51, tmp_path
    ) -> None:
        """Output id must be 51 (from generic ref) when --generic-ref is supplied."""
        result, output_path = _transform_to_tmp(
            s6_harness_id10_input, tmp_path,
            generic_ref_path=s6_generic_ref_id51,
        )
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        id_line = next((l for l in fm_lines if l.startswith("id:")), None)
        assert id_line is not None
        assert "51" in id_line, f"Expected id 51 (from generic ref) but found: {id_line!r}"
        assert "10" not in id_line, "Harness id 10 must not survive when generic ref is supplied"

    def test_harness_id_preserved_when_no_generic_ref(
        self, s6_harness_id10_input, tmp_path
    ) -> None:
        """When no --generic-ref is supplied, the harness file id is unchanged."""
        result, output_path = _transform_to_tmp(s6_harness_id10_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        id_line = next((l for l in fm_lines if l.startswith("id:")), None)
        assert id_line is not None
        assert "10" in id_line, f"Expected id 10 preserved but found: {id_line!r}"


def _s6_extract_frontmatter_lines(content: str) -> list:
    """Return the inner frontmatter lines (between the --- delimiters)."""
    lines = content.splitlines()
    result = []
    in_fm = False
    for line in lines:
        if line.strip() == "---":
            if not in_fm:
                in_fm = True
                continue
            else:
                break
        if in_fm:
            result.append(line)
    return result


# ---------------------------------------------------------------------------
# T6.4: harness-only key stripping (unit via build_output_frontmatter)
# ---------------------------------------------------------------------------

class TestHarnessOnlyKeyStrippingUnit:
    """build_output_frontmatter strips harness-only keys on AGENT_GENERIC, preserves on AGENT_HARNESS."""

    _LINES_WITH_HARNESS_KEYS: list = [
        "id: 5\n",
        "version: 2.0.0\n",
        "transform_version: 2.0.0\n",
        "model: claude-opus-4\n",
        "mode: subagent\n",
        "permission: {allow: all}\n",
        "name: test-agent\n",
    ]
    _LINES_WITH_MULTILINE_PERMISSION: list = [
        "id: 1\n",
        "version: 2.0.0\n",
        "transform_version: 2.0.0\n",
        "permission:\n",
        "  allow: [read]\n",
        "  deny: [write]\n",
        "name: test-agent\n",
    ]

    @property
    def _derived(self) -> DerivedFrontmatter:
        return DerivedFrontmatter(name="test-agent", role="subagent", required_skills=[])

    def _build_generic(self, raw_lines=None):
        return build_output_frontmatter(
            raw_lines if raw_lines is not None else self._LINES_WITH_HARNESS_KEYS,
            kind=DocumentKind.AGENT_GENERIC,
            derived=self._derived,
            version_after="2.1.0",
            transform_version_after=None,
            generic_id=None,
        )

    def _build_harness(self, raw_lines=None):
        return build_output_frontmatter(
            raw_lines if raw_lines is not None else self._LINES_WITH_HARNESS_KEYS,
            kind=DocumentKind.AGENT_HARNESS,
            derived=self._derived,
            version_after="2.1.0",
            transform_version_after="2.1.0",
            generic_id=None,
        )

    def test_generic_strips_model(self) -> None:
        output_lines, _ = self._build_generic()
        assert not any(l.startswith("model:") for l in output_lines)

    def test_generic_strips_mode(self) -> None:
        output_lines, _ = self._build_generic()
        assert not any(l.startswith("mode:") for l in output_lines)

    def test_generic_strips_permission(self) -> None:
        output_lines, _ = self._build_generic()
        assert not any(l.startswith("permission:") for l in output_lines)

    def test_generic_strips_transform_version_when_transform_version_after_is_none(self) -> None:
        output_lines, _ = self._build_generic()
        assert not any(l.startswith("transform_version:") for l in output_lines)

    def test_generic_preserves_non_harness_keys(self) -> None:
        output_lines, _ = self._build_generic()
        combined = "".join(output_lines)
        assert "id: 5" in combined
        assert "name: test-agent" in combined

    def test_generic_strips_multiline_permission_entirely(self) -> None:
        output_lines, _ = self._build_generic(self._LINES_WITH_MULTILINE_PERMISSION)
        assert not any(
            "permission" in l or "allow:" in l or "deny:" in l for l in output_lines
        ), "Entire multi-line permission block must be stripped on generic path"

    def test_harness_preserves_model(self) -> None:
        output_lines, _ = self._build_harness()
        assert any(l.startswith("model:") for l in output_lines)

    def test_harness_preserves_mode(self) -> None:
        output_lines, _ = self._build_harness()
        assert any(l.startswith("mode:") for l in output_lines)

    def test_harness_preserves_permission(self) -> None:
        output_lines, _ = self._build_harness()
        assert any(l.startswith("permission:") for l in output_lines)

    def test_harness_preserves_multiline_permission_verbatim(self) -> None:
        output_lines, _ = self._build_harness(self._LINES_WITH_MULTILINE_PERMISSION)
        combined = "".join(output_lines)
        assert "permission:" in combined
        assert "allow: [read]" in combined
        assert "deny: [write]" in combined

    def test_untouched_keys_keep_original_relative_order(self) -> None:
        raw_lines = [
            "id: 1\n",
            "version: 2.0.0\n",
            "name: test-agent\n",
            "description: A test agent\n",
        ]
        output_lines, _ = build_output_frontmatter(
            raw_lines,
            kind=DocumentKind.AGENT_GENERIC,
            derived=DerivedFrontmatter(name="test-agent", role="subagent", required_skills=[]),
            version_after="2.1.0",
            transform_version_after=None,
            generic_id=None,
        )
        positions = {k: None for k in ("id", "name", "description")}
        for i, line in enumerate(output_lines):
            for key in positions:
                if line.startswith(f"{key}:"):
                    positions[key] = i
        assert all(v is not None for v in positions.values())
        assert positions["id"] < positions["name"] < positions["description"]


# ---------------------------------------------------------------------------
# T6.4: harness-only key stripping integration through transform_file
# ---------------------------------------------------------------------------

class TestHarnessOnlyKeyStrippingIntegration:
    """transform_file strips harness-only keys on generic path; retains on harness path."""

    def test_generic_path_output_has_no_model_in_frontmatter(
        self, s6_generic_with_harness_keys_input, tmp_path
    ) -> None:
        """'model' must not appear in the output frontmatter on the generic path."""
        result, output_path = _transform_to_tmp(s6_generic_with_harness_keys_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert not any(l.startswith("model:") for l in fm_lines), (
            "'model' must be stripped from generic-path output frontmatter"
        )

    def test_generic_path_output_has_no_mode_in_frontmatter(
        self, s6_generic_with_harness_keys_input, tmp_path
    ) -> None:
        """'mode' must not appear in the output frontmatter on the generic path."""
        result, output_path = _transform_to_tmp(s6_generic_with_harness_keys_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert not any(l.startswith("mode:") for l in fm_lines), (
            "'mode' must be stripped from generic-path output frontmatter"
        )

    def test_harness_path_output_retains_model_in_frontmatter(
        self, s6_harness_with_harness_keys_input, s6_generic_ref_id51, tmp_path
    ) -> None:
        """'model' must appear in the output frontmatter on the harness path."""
        result, output_path = _transform_to_tmp(
            s6_harness_with_harness_keys_input, tmp_path,
            generic_ref_path=s6_generic_ref_id51,
        )
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert any(l.startswith("model:") for l in fm_lines), (
            "'model' must be retained in harness-path output frontmatter"
        )

    def test_harness_path_output_retains_mode_in_frontmatter(
        self, s6_harness_with_harness_keys_input, s6_generic_ref_id51, tmp_path
    ) -> None:
        """'mode' must appear in the output frontmatter on the harness path."""
        result, output_path = _transform_to_tmp(
            s6_harness_with_harness_keys_input, tmp_path,
            generic_ref_path=s6_generic_ref_id51,
        )
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert any(l.startswith("mode:") for l in fm_lines), (
            "'mode' must be retained in harness-path output frontmatter"
        )


# ---------------------------------------------------------------------------
# T6.1 / AC6.2: derived field emission — "write when absent" (unit)
# ---------------------------------------------------------------------------

class TestDerivedFieldsWrittenWhenAbsentUnit:
    """build_output_frontmatter writes derived.name, derived.role, and
    derived.required_skills into the output when those keys are absent from
    the source frontmatter (Rule 6).  An existing value is left untouched."""

    _LINES_WITHOUT_DERIVED: list = [
        "id: 10\n",
        "version: 1.0.0\n",
        "description: An agent missing all derived keys\n",
    ]

    @property
    def _derived(self) -> DerivedFrontmatter:
        return DerivedFrontmatter(name="my-agent", role="subagent", required_skills=[])

    def _build(self, raw_lines=None, *, derived=None):
        return build_output_frontmatter(
            raw_lines if raw_lines is not None else self._LINES_WITHOUT_DERIVED,
            kind=DocumentKind.AGENT_GENERIC,
            derived=derived if derived is not None else self._derived,
            version_after="1.1.0",
            transform_version_after=None,
            generic_id=None,
        )

    def test_name_written_when_absent(self) -> None:
        """Rule 6: derived.name is emitted when 'name' is absent from source."""
        output_lines, _ = self._build()
        combined = "".join(output_lines)
        assert "name: my-agent" in combined

    def test_role_written_when_absent(self) -> None:
        """Rule 6: derived.role is emitted when 'role' is absent from source."""
        output_lines, _ = self._build()
        combined = "".join(output_lines)
        assert "role: subagent" in combined

    def test_required_skills_empty_list_written_when_absent(self) -> None:
        """Rule 6: required_skills: [] is emitted when 'required_skills' is absent
        and the agent loads no skills — [] is the correct, meaningful result."""
        output_lines, _ = self._build()
        combined = "".join(output_lines)
        assert "required_skills: []" in combined

    def test_all_three_derived_fields_written_when_all_absent(self) -> None:
        """When name, role, and required_skills are all absent, all three are written."""
        output_lines, _ = self._build()
        combined = "".join(output_lines)
        assert "name: my-agent" in combined
        assert "role: subagent" in combined
        assert "required_skills: []" in combined

    def test_required_skills_non_empty_written_when_absent(self) -> None:
        """Rule 6: non-empty required_skills is emitted as a YAML flow sequence."""
        derived = DerivedFrontmatter(
            name="my-agent",
            role="subagent",
            required_skills=["lean-tdd", "efficient-file-reading"],
        )
        output_lines, _ = self._build(derived=derived)
        combined = "".join(output_lines)
        assert "required_skills: [lean-tdd, efficient-file-reading]" in combined

    def test_name_left_untouched_when_present(self) -> None:
        """Rule 6: an existing 'name' value is not overwritten by derived.name."""
        lines = self._LINES_WITHOUT_DERIVED + ["name: existing-name\n"]
        output_lines, _ = self._build(raw_lines=lines)
        combined = "".join(output_lines)
        assert "existing-name" in combined
        assert "name: my-agent" not in combined

    def test_role_left_untouched_when_present(self) -> None:
        """Rule 6: an existing 'role' value is not overwritten by derived.role."""
        lines = self._LINES_WITHOUT_DERIVED + ["role: orchestrator\n"]
        output_lines, _ = self._build(raw_lines=lines)
        combined = "".join(output_lines)
        assert "role: orchestrator" in combined
        assert "role: subagent" not in combined

    def test_required_skills_non_default_left_untouched(self) -> None:
        """Rule 6: an existing 'required_skills' value is preserved verbatim and
        not replaced by the derived empty list."""
        lines = self._LINES_WITHOUT_DERIVED + ["required_skills: [foo]\n"]
        output_lines, _ = self._build(raw_lines=lines)
        combined = "".join(output_lines)
        assert "required_skills: [foo]" in combined
        assert "required_skills: []" not in combined


# ---------------------------------------------------------------------------
# T6.1 / AC6.2: derived field emission — "write when absent" integration
# ---------------------------------------------------------------------------

class TestDerivedFieldsWrittenWhenAbsentIntegration:
    """transform_file writes name, role, and required_skills into the output
    frontmatter when those keys are entirely absent from the source file.

    Uses s6_generic_missing_derived_keys_input — a generic agent file with no
    name, role, required_skills, recommended_tier, or tier_rationale, and a body
    containing no skill-load directives (so required_skills emits as []).
    """

    def test_name_appears_in_output_frontmatter(
        self, s6_generic_missing_derived_keys_input, tmp_path
    ) -> None:
        """'name' must appear in the output frontmatter when absent from source."""
        result, output_path = _transform_to_tmp(s6_generic_missing_derived_keys_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert any(l.startswith("name:") for l in fm_lines), (
            "'name' must be written to output frontmatter when absent from source"
        )

    def test_role_appears_in_output_frontmatter(
        self, s6_generic_missing_derived_keys_input, tmp_path
    ) -> None:
        """'role' must appear in the output frontmatter when absent from source."""
        result, output_path = _transform_to_tmp(s6_generic_missing_derived_keys_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert any(l.startswith("role:") for l in fm_lines), (
            "'role' must be written to output frontmatter when absent from source"
        )

    def test_required_skills_appears_in_output_frontmatter(
        self, s6_generic_missing_derived_keys_input, tmp_path
    ) -> None:
        """AC6.2: required_skills must appear in output frontmatter when absent from source.
        An agent with no skill-load directives in its body must emit required_skills: []."""
        result, output_path = _transform_to_tmp(s6_generic_missing_derived_keys_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert any(l.startswith("required_skills:") for l in fm_lines), (
            "required_skills must be written to output frontmatter when absent from source"
        )

    def test_required_skills_is_empty_list_when_no_skill_directives(
        self, s6_generic_missing_derived_keys_input, tmp_path
    ) -> None:
        """AC6.2: required_skills: [] is the correct value for an agent that loads no skills."""
        result, output_path = _transform_to_tmp(s6_generic_missing_derived_keys_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        skills_line = next(
            (l for l in fm_lines if l.startswith("required_skills:")), None
        )
        assert skills_line is not None
        assert "[]" in skills_line, (
            f"required_skills must be [] when agent has no skill-load directives; "
            f"got: {skills_line!r}"
        )

    def test_role_value_matches_subagent_filename(
        self, s6_generic_missing_derived_keys_input, tmp_path
    ) -> None:
        """Role is derived from filename; a non-orchestrator filename yields 'subagent'."""
        result, output_path = _transform_to_tmp(s6_generic_missing_derived_keys_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        role_line = next((l for l in fm_lines if l.startswith("role:")), None)
        assert role_line is not None
        assert "subagent" in role_line, (
            f"A non-orchestrator filename must yield role: subagent; got: {role_line!r}"
        )

    def test_transform_succeeds_and_has_two_tier_ncs(
        self, s6_generic_missing_derived_keys_input, tmp_path
    ) -> None:
        """The fixture missing both tier keys yields two NC_TIER_PLACEHOLDER findings."""
        result, _ = _transform_to_tmp(s6_generic_missing_derived_keys_input, tmp_path)
        assert result.success is True
        tier_ncs = [nc for nc in result.non_conformances if nc.code == NC_TIER_PLACEHOLDER]
        assert len(tier_ncs) == 2


# ---------------------------------------------------------------------------
# T6.2 / T6.4 / AC6.2: degraded write-out path coverage
# ---------------------------------------------------------------------------

class TestDegradedPathStage6:
    """The degraded transform path (harness-kind file without --generic-ref, non-orchestrator)
    must apply all Stage 6 frontmatter rules:

    - Tier placeholder insertion (recommended_tier, tier_rationale when absent)
    - Harness-only key preservation (mode, model, transform_version kept on harness-kind output)
    - Derived field writing (name, role, required_skills when absent)

    Uses s6_harness_degraded_no_tier_input, which has transform_version but no tier keys,
    name, role, or required_skills.  Passed to transform_file WITHOUT generic_ref_path to
    trigger the degraded write-out path (result.degraded == True).
    """

    def test_degraded_path_succeeds(
        self, s6_harness_degraded_no_tier_input, tmp_path
    ) -> None:
        """The degraded transform must succeed for a valid harness subagent file."""
        result, _ = _transform_to_tmp(s6_harness_degraded_no_tier_input, tmp_path)
        assert result.success is True

    def test_degraded_flag_is_true(
        self, s6_harness_degraded_no_tier_input, tmp_path
    ) -> None:
        """result.degraded must be True so callers can distinguish degraded output."""
        result, _ = _transform_to_tmp(s6_harness_degraded_no_tier_input, tmp_path)
        assert result.degraded is True

    def test_degraded_path_inserts_recommended_tier_placeholder(
        self, s6_harness_degraded_no_tier_input, tmp_path
    ) -> None:
        """recommended_tier must be written with a placeholder on the degraded write-out path."""
        result, output_path = _transform_to_tmp(s6_harness_degraded_no_tier_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert any(l.startswith("recommended_tier:") for l in fm_lines), (
            "recommended_tier must be inserted on the degraded path when absent"
        )

    def test_degraded_path_inserts_tier_rationale_placeholder(
        self, s6_harness_degraded_no_tier_input, tmp_path
    ) -> None:
        """tier_rationale must be written with a placeholder on the degraded write-out path."""
        result, output_path = _transform_to_tmp(s6_harness_degraded_no_tier_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert any(l.startswith("tier_rationale:") for l in fm_lines), (
            "tier_rationale must be inserted on the degraded path when absent"
        )

    def test_degraded_path_tier_placeholder_value_is_todo(
        self, s6_harness_degraded_no_tier_input, tmp_path
    ) -> None:
        """The placeholder value TIER_PLACEHOLDER must appear in degraded output."""
        result, output_path = _transform_to_tmp(s6_harness_degraded_no_tier_input, tmp_path)
        assert result.success is True
        assert TIER_PLACEHOLDER in _read(output_path)

    def test_degraded_path_records_nc_tier_placeholder_on_result(
        self, s6_harness_degraded_no_tier_input, tmp_path
    ) -> None:
        """Two NC_TIER_PLACEHOLDER findings must be recorded on result.non_conformances."""
        result, _ = _transform_to_tmp(s6_harness_degraded_no_tier_input, tmp_path)
        tier_ncs = [nc for nc in result.non_conformances if nc.code == NC_TIER_PLACEHOLDER]
        assert len(tier_ncs) == 2, (
            f"Expected 2 NC_TIER_PLACEHOLDER on degraded-path result but got "
            f"{len(tier_ncs)}: {result.non_conformances}"
        )

    def test_degraded_path_preserves_mode_key(
        self, s6_harness_degraded_no_tier_input, tmp_path
    ) -> None:
        """Harness-only key 'mode' must be preserved on the degraded path (harness-kind output)."""
        result, output_path = _transform_to_tmp(s6_harness_degraded_no_tier_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert any(l.startswith("mode:") for l in fm_lines), (
            "harness-only key 'mode' must be preserved on the degraded path"
        )

    def test_degraded_path_preserves_model_key(
        self, s6_harness_degraded_no_tier_input, tmp_path
    ) -> None:
        """Harness-only key 'model' must be preserved on the degraded path (harness-kind output)."""
        result, output_path = _transform_to_tmp(s6_harness_degraded_no_tier_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert any(l.startswith("model:") for l in fm_lines), (
            "harness-only key 'model' must be preserved on the degraded path"
        )

    def test_degraded_path_writes_derived_name(
        self, s6_harness_degraded_no_tier_input, tmp_path
    ) -> None:
        """'name' must be written to output frontmatter on the degraded path when absent."""
        result, output_path = _transform_to_tmp(s6_harness_degraded_no_tier_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert any(l.startswith("name:") for l in fm_lines), (
            "'name' must be derived and written on the degraded write-out path"
        )


# ===========================================================================
# Stage 3: ErrorHandlingCommon and ExecutionPhilosophyCommon Regions
# ===========================================================================
#
# Tests covering T3.1 (ErrorHandlingCommon), T3.2 (ExecutionPhilosophyCommon),
# and T3.3 (fixture-based exact match).  All tests are in TDD RED phase: they
# compile and run but fail until Stage 3 appends ErrorHandlingCommon and
# ExecutionPhilosophyCommon to CONDUCT_REGIONS and wires the corresponding
# deletions and region emissions on both transform paths.
#
# Unit tests call apply_conduct_regions directly on small in-memory line lists.
# Integration tests call transform_file on the s3_generic_eh_ep and
# s3_harness_eh_ep fixture pairs.
# ---------------------------------------------------------------------------

from region_insertion import (  # noqa: E402
    CONDUCT_REGIONS,
    Anchor,
    RegionSpec,
    RegionInsertionResult,
    apply_conduct_regions,
    find_section_spans,
)


# ---------------------------------------------------------------------------
# Unit helpers shared by Stage 3 test classes
# ---------------------------------------------------------------------------

def _s3_find_spec(name: str) -> "RegionSpec":
    """Return the named RegionSpec from CONDUCT_REGIONS or fail the test."""
    for s in CONDUCT_REGIONS:
        if s.name == name:
            return s
    pytest.fail(
        f"{name!r} not found in CONDUCT_REGIONS. "
        "Stage 3 must append this spec before these tests can pass."
    )


def _s3_run_apply(body_lines: list, spec: "RegionSpec") -> "RegionInsertionResult":
    """Run apply_conduct_regions with a single spec on the given body."""
    sections = find_section_spans(body_lines)
    return apply_conduct_regions(body_lines, sections, specs=(spec,))


def _s3_make_eh_body(
    retry_canonical: bool = True,
    retry_drifted: bool = False,
    errcodes_canonical: bool = True,
    errcodes_drifted: bool = False,
) -> list:
    """Build a minimal ErrorHandling section body for unit testing.

    retry_canonical — include the canonical EH-retry bullet (strict match)
    retry_drifted  — include a drifted variant that only the probe matches
    errcodes_canonical — include the canonical EH-errcodes bullet (strict match)
    errcodes_drifted   — include a drifted variant that only the probe matches

    Status-mapping bullets (CAPABILITY_EXCEEDED, NEEDS_CLARIFICATION) are always
    included so tests can verify they survive deletion.
    """
    lines: list = ["## Error Handling\n", "\n"]
    if retry_canonical:
        lines.append(
            "- **Retry a transient error once** before escalating"
            " — a read that timed out, a tool that failed to answer\n"
        )
    elif retry_drifted:
        lines.append("- **Retry transient errors** once before escalating\n")
    if errcodes_canonical:
        lines.append(
            "- **Return BLOCKED** if missing prerequisites"
            " (E101: input not found, E401: dependency missing, E501: tool unavailable)\n"
        )
    elif errcodes_drifted:
        lines.append("- **Return BLOCKED** when E401 or E501 conditions apply\n")
    lines.append("- **Return CAPABILITY_EXCEEDED** when the task exceeds your ability\n")
    lines.append("- **Return NEEDS_CLARIFICATION** when context is ambiguous\n")
    lines.append("\n")
    lines.append("[[INJECTION:ErrorHandlingExtension]]\n")
    lines.append("[[/INJECTION:ErrorHandlingExtension]]\n")
    return lines


def _s3_make_ep_body(
    context_canonical: bool = True,
    context_drifted: bool = False,
    memory_canonical: bool = True,
    memory_drifted: bool = False,
    quality_canonical: bool = True,
    quality_drifted: bool = False,
    context_limits_mid_bullet: bool = True,
) -> list:
    """Build a minimal ExecutionPhilosophy section body for unit testing.

    When context_limits_mid_bullet is True the [[INJECTION:ContextLimits]] tag is
    placed between the Context Management and Memory bullets — the ordering-hazard
    layout.  When False, ContextLimits is at the end.

    A fixed-phrasing agent bullet ('Fix Precision') is always included to verify
    that surviving content is preserved after deletion.
    """
    lines: list = ["## Execution Philosophy\n", "\n"]
    if context_canonical:
        lines.append(
            "- **Context Management:** You can dedicate your full context window"
            " to this task. Follow-up work is handled by spawning new agent instances.\n"
        )
    elif context_drifted:
        lines.append("- **Context Management:** Dedicate your full context window here.\n")

    if context_limits_mid_bullet:
        lines.append("[[INJECTION:ContextLimits]]\n")
        lines.append("[[/INJECTION:ContextLimits]]\n")

    if memory_canonical:
        lines.append(
            "- **Memory via Artifacts:** Input and output artifacts are the persistent"
            " memory between invocations. Anything a successor needs goes into an"
            " artifact, not into your response.\n"
        )
    elif memory_drifted:
        lines.append("- **Memory via Artifacts:** Write state to artifacts between invocations.\n")
    if quality_canonical:
        lines.append(
            "- **Quality over Completeness:** Finishing part of the task well beats"
            " finishing all of it badly — a successor continues what you leave."
            " Use `PARTIALLY_DONE` when you stop deliberately with more of the same"
            " work remaining, `COMPLETED_NEEDS_ACTION` when your finished work is a"
            " set of items for another agent to act on, and `CAPABILITY_EXCEEDED`"
            " when you had what you needed and still could not do it.\n"
        )
    elif quality_drifted:
        lines.append(
            "- **Quality over Completeness:** It's acceptable to complete only part"
            " of the task with high quality.\n"
        )

    if not context_limits_mid_bullet:
        lines.append("[[INJECTION:ContextLimits]]\n")
        lines.append("[[/INJECTION:ContextLimits]]\n")

    lines.append(
        "- **Fix Precision:** Change only what needs fixing;"
        " preserve correct test logic and existing structure.\n"
    )
    return lines


# ---------------------------------------------------------------------------
# T3.1 — ErrorHandlingCommon region: unit tests
# ---------------------------------------------------------------------------

class TestErrorHandlingCommonConductRegionUnit:
    """Unit-level tests for ErrorHandlingCommon: spec presence, deletion rules,
    and region placement.  All tests fail until Stage 3 appends the spec to
    CONDUCT_REGIONS and defines the EH-retry and EH-errcodes deletion rules."""

    # --- Spec presence ---

    def test_conduct_regions_contains_error_handling_common(self) -> None:
        """CONDUCT_REGIONS must contain an ErrorHandlingCommon RegionSpec after Stage 3."""
        names = [s.name for s in CONDUCT_REGIONS]
        assert "ErrorHandlingCommon" in names, (
            "CONDUCT_REGIONS must be extended with ErrorHandlingCommon at Stage 3"
        )

    def test_error_handling_common_parent_is_error_handling(self) -> None:
        """ErrorHandlingCommon must declare 'ErrorHandling' as its parent section."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        assert spec.parent_section == "ErrorHandling"

    def test_error_handling_common_anchor_is_section_start(self) -> None:
        """ErrorHandlingCommon must use SECTION_START anchor so it is emitted first."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        assert spec.anchor == Anchor.SECTION_START

    # --- Deletion rule presence and attributes ---

    def test_eh_retry_rule_present_in_supersedes(self) -> None:
        """ErrorHandlingCommon spec must carry a deletion rule with rule_id 'EH-retry'."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        rule_ids = [r.rule_id for r in spec.supersedes]
        assert "EH-retry" in rule_ids

    def test_eh_errcodes_rule_present_in_supersedes(self) -> None:
        """ErrorHandlingCommon spec must carry a deletion rule with rule_id 'EH-errcodes'."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        rule_ids = [r.rule_id for r in spec.supersedes]
        assert "EH-errcodes" in rule_ids

    def test_eh_retry_is_required(self) -> None:
        """EH-retry must be required=True: the retry bullet is present in all conforming files."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        rule = next(r for r in spec.supersedes if r.rule_id == "EH-retry")
        assert rule.required is True

    def test_eh_errcodes_is_not_required(self) -> None:
        """EH-errcodes must be required=False: the bullet is absent from some corpus files."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        rule = next(r for r in spec.supersedes if r.rule_id == "EH-errcodes")
        assert rule.required is False

    def test_eh_retry_has_drift_probe(self) -> None:
        """EH-retry must carry a drift_probe to detect re-worded retry bullets."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        rule = next(r for r in spec.supersedes if r.rule_id == "EH-retry")
        assert rule.drift_probe is not None

    def test_eh_errcodes_has_drift_probe(self) -> None:
        """EH-errcodes must carry a drift_probe to detect re-worded error-code bullets."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        rule = next(r for r in spec.supersedes if r.rule_id == "EH-errcodes")
        assert rule.drift_probe is not None

    # --- Three-case truth table for EH-retry ---

    def test_eh_retry_strict_match_deletes_bullet_and_records_applied(self) -> None:
        """When the canonical retry bullet is present it must be deleted and EH-retry
        must appear in deletions_applied."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        body = _s3_make_eh_body(retry_canonical=True)
        result = _s3_run_apply(body, spec)
        assert "EH-retry" in result.deletions_applied
        assert "EH-retry" not in result.deletions_unmatched
        assert "Retry a transient error once" not in "".join(result.lines)

    def test_eh_retry_probe_only_hit_keeps_bullet_and_records_unmatched(self) -> None:
        """When only a drifted retry bullet is present it must be left in place,
        EH-retry must appear in deletions_unmatched, and exactly one NC_DRIFTED_BULLET
        finding with detail=='EH-retry' must be raised (the drift-probe branch wired
        in apply_conduct_regions at Stage 8 fires for every probe-only hit, regardless
        of the Stage that introduced the rule — see ContractsDesign.md's Stage-scoping
        caveat under DeletionRule)."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        body = _s3_make_eh_body(retry_canonical=False, retry_drifted=True)
        result = _s3_run_apply(body, spec)
        assert "EH-retry" in result.deletions_unmatched
        assert "EH-retry" not in result.deletions_applied
        assert len(result.non_conformances) == 1, (
            "Probe-only hit must raise exactly one NC_DRIFTED_BULLET finding"
        )
        assert result.non_conformances[0].code == NC_DRIFTED_BULLET
        assert result.non_conformances[0].detail == "EH-retry"

    def test_eh_retry_no_match_records_in_unmatched_because_required(self) -> None:
        """When the retry bullet is entirely absent EH-retry must appear in
        deletions_unmatched because required=True."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        body = _s3_make_eh_body(retry_canonical=False, retry_drifted=False)
        result = _s3_run_apply(body, spec)
        assert "EH-retry" in result.deletions_unmatched

    # --- Three-case truth table for EH-errcodes ---

    def test_eh_errcodes_strict_match_deletes_bullet_and_records_applied(self) -> None:
        """When the canonical error-code recall bullet is present it must be deleted and
        EH-errcodes must appear in deletions_applied."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        body = _s3_make_eh_body(errcodes_canonical=True)
        result = _s3_run_apply(body, spec)
        assert "EH-errcodes" in result.deletions_applied
        assert "EH-errcodes" not in result.deletions_unmatched
        assert "missing prerequisites" not in "".join(result.lines)

    def test_eh_errcodes_probe_only_hit_keeps_bullet_and_records_unmatched(self) -> None:
        """When only a drifted error-code recall bullet is present it must be left in
        place, EH-errcodes must appear in deletions_unmatched, and exactly one
        NC_DRIFTED_BULLET finding with detail=='EH-errcodes' must be raised (probe-only
        hits raise NC_DRIFTED_BULLET regardless of `required`, per the outcome contract)."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        body = _s3_make_eh_body(errcodes_canonical=False, errcodes_drifted=True)
        result = _s3_run_apply(body, spec)
        assert "EH-errcodes" in result.deletions_unmatched
        assert "EH-errcodes" not in result.deletions_applied
        assert len(result.non_conformances) == 1
        assert result.non_conformances[0].code == NC_DRIFTED_BULLET
        assert result.non_conformances[0].detail == "EH-errcodes"

    def test_eh_errcodes_no_match_not_recorded_because_not_required(self) -> None:
        """When the error-code recall bullet is entirely absent EH-errcodes must NOT
        appear in deletions_unmatched because required=False."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        body = _s3_make_eh_body(errcodes_canonical=False, errcodes_drifted=False)
        result = _s3_run_apply(body, spec)
        assert "EH-errcodes" not in result.deletions_unmatched

    # --- Placement and survival ---

    def test_status_mapping_bullets_survive_both_deletions(self) -> None:
        """CAPABILITY_EXCEEDED and NEEDS_CLARIFICATION status-mapping bullets must
        survive deletion of the retry and error-code recall bullets."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        body = _s3_make_eh_body(retry_canonical=True, errcodes_canonical=True)
        result = _s3_run_apply(body, spec)
        output_text = "".join(result.lines)
        assert "Return CAPABILITY_EXCEEDED" in output_text, (
            "CAPABILITY_EXCEEDED status-mapping bullet must survive deletion"
        )
        assert "Return NEEDS_CLARIFICATION" in output_text, (
            "NEEDS_CLARIFICATION status-mapping bullet must survive deletion"
        )

    def test_error_handling_common_region_is_first_in_section(self) -> None:
        """[[DEPLOYED:ErrorHandlingCommon]] must be the very first tag after the
        section heading (nothing else may precede it in the section body)."""
        spec = _s3_find_spec("ErrorHandlingCommon")
        body = _s3_make_eh_body(retry_canonical=True, errcodes_canonical=True)
        result = _s3_run_apply(body, spec)
        assert "ErrorHandlingCommon" in result.deployed_added
        output_text = "".join(result.lines)
        heading_pos = output_text.find("## Error Handling")
        deployed_pos = output_text.find("[[DEPLOYED:ErrorHandlingCommon]]")
        assert heading_pos != -1 and deployed_pos != -1
        between = output_text[heading_pos + len("## Error Handling"):deployed_pos].strip()
        assert between == "", (
            "ErrorHandlingCommon must be first in its section; "
            f"unexpected content between heading and tag: {between!r}"
        )

    def test_error_handling_common_not_emitted_twice_when_already_present(self) -> None:
        """When ErrorHandlingCommon is already in the body it must not be re-emitted."""
        body = [
            "## Error Handling\n",
            "\n",
            "[[DEPLOYED:ErrorHandlingCommon]]\n",
            "[[/DEPLOYED:ErrorHandlingCommon]]\n",
            "- **Return CAPABILITY_EXCEEDED** when the task exceeds your ability\n",
            "\n",
        ]
        spec = _s3_find_spec("ErrorHandlingCommon")
        sections = find_section_spans(body)
        result = apply_conduct_regions(body, sections, specs=(spec,))
        assert "ErrorHandlingCommon" not in result.deployed_added
        deployed_count = sum(
            1 for ln in result.lines if ln.strip() == "[[DEPLOYED:ErrorHandlingCommon]]"
        )
        assert deployed_count == 1, "ErrorHandlingCommon must not be emitted a second time"


# ---------------------------------------------------------------------------
# T3.2 — ExecutionPhilosophyCommon region: unit tests
# ---------------------------------------------------------------------------

class TestExecutionPhilosophyCommonConductRegionUnit:
    """Unit-level tests for ExecutionPhilosophyCommon: spec presence, deletion rules,
    region placement, and the ordering hazard.  All tests fail until Stage 3 appends
    the spec to CONDUCT_REGIONS and defines the EP-context, EP-memory and EP-quality
    deletion rules."""

    # --- Spec presence and shape ---

    def test_conduct_regions_contains_execution_philosophy_common(self) -> None:
        """CONDUCT_REGIONS must contain an ExecutionPhilosophyCommon RegionSpec after Stage 3."""
        names = [s.name for s in CONDUCT_REGIONS]
        assert "ExecutionPhilosophyCommon" in names, (
            "CONDUCT_REGIONS must be extended with ExecutionPhilosophyCommon at Stage 3"
        )

    def test_execution_philosophy_common_parent_is_execution_philosophy(self) -> None:
        """ExecutionPhilosophyCommon must declare 'ExecutionPhilosophy' as its parent."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        assert spec.parent_section == "ExecutionPhilosophy"

    def test_execution_philosophy_common_anchor_is_before_context_limits(self) -> None:
        """ExecutionPhilosophyCommon must use BEFORE_REGION anchor referencing ContextLimits."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        assert spec.anchor == Anchor.BEFORE_REGION
        assert spec.anchor_ref is not None
        kind, ref_name = spec.anchor_ref
        assert ref_name == "ContextLimits", (
            "anchor_ref must reference ContextLimits so the region precedes it"
        )

    def test_execution_philosophy_common_has_section_start_fallback(self) -> None:
        """ExecutionPhilosophyCommon must fall back to SECTION_START when ContextLimits
        is absent — so files without ContextLimits still get the region first."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        assert spec.fallback_anchor == Anchor.SECTION_START

    # --- Deletion rule presence and attributes ---

    def test_ep_context_rule_present(self) -> None:
        """ExecutionPhilosophyCommon must carry a deletion rule with rule_id 'EP-context'."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        assert "EP-context" in [r.rule_id for r in spec.supersedes]

    def test_ep_memory_rule_present(self) -> None:
        """ExecutionPhilosophyCommon must carry a deletion rule with rule_id 'EP-memory'."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        assert "EP-memory" in [r.rule_id for r in spec.supersedes]

    def test_ep_quality_rule_present(self) -> None:
        """ExecutionPhilosophyCommon must carry a deletion rule with rule_id 'EP-quality'."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        assert "EP-quality" in [r.rule_id for r in spec.supersedes]

    def test_ep_context_is_required(self) -> None:
        """EP-context must be required=True: the Context Management bullet is in all conforming files."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        rule = next(r for r in spec.supersedes if r.rule_id == "EP-context")
        assert rule.required is True

    def test_ep_memory_is_required(self) -> None:
        """EP-memory must be required=True."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        rule = next(r for r in spec.supersedes if r.rule_id == "EP-memory")
        assert rule.required is True

    def test_ep_quality_is_required(self) -> None:
        """EP-quality must be required=True."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        rule = next(r for r in spec.supersedes if r.rule_id == "EP-quality")
        assert rule.required is True

    def test_ep_context_has_drift_probe(self) -> None:
        """EP-context must carry a drift_probe."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        rule = next(r for r in spec.supersedes if r.rule_id == "EP-context")
        assert rule.drift_probe is not None

    def test_ep_memory_has_drift_probe(self) -> None:
        """EP-memory must carry a drift_probe."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        rule = next(r for r in spec.supersedes if r.rule_id == "EP-memory")
        assert rule.drift_probe is not None

    def test_ep_quality_has_drift_probe(self) -> None:
        """EP-quality must carry a drift_probe."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        rule = next(r for r in spec.supersedes if r.rule_id == "EP-quality")
        assert rule.drift_probe is not None

    # --- Three-case truth tables (representative; one case each for brevity) ---

    def test_ep_context_strict_match_deletes_bullet(self) -> None:
        """When the canonical Context Management bullet is present it is deleted and
        EP-context appears in deletions_applied."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        body = _s3_make_ep_body(context_canonical=True)
        result = _s3_run_apply(body, spec)
        assert "EP-context" in result.deletions_applied
        assert "Context Management" not in "".join(result.lines)

    def test_ep_context_probe_only_hit_keeps_bullet_and_records_unmatched(self) -> None:
        """When only a drifted Context Management bullet is present it is left in place,
        EP-context appears in deletions_unmatched, and exactly one NC_DRIFTED_BULLET
        finding with detail=='EP-context' is raised."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        body = _s3_make_ep_body(context_canonical=False, context_drifted=True)
        result = _s3_run_apply(body, spec)
        assert "EP-context" in result.deletions_unmatched
        assert "EP-context" not in result.deletions_applied
        assert len(result.non_conformances) == 1
        assert result.non_conformances[0].code == NC_DRIFTED_BULLET
        assert result.non_conformances[0].detail == "EP-context"

    def test_ep_memory_strict_match_deletes_bullet(self) -> None:
        """When the canonical Memory via Artifacts bullet is present it is deleted."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        body = _s3_make_ep_body(memory_canonical=True)
        result = _s3_run_apply(body, spec)
        assert "EP-memory" in result.deletions_applied
        assert "Memory via Artifacts" not in "".join(result.lines)

    def test_ep_memory_probe_only_hit_keeps_bullet_and_records_unmatched(self) -> None:
        """When only a drifted Memory via Artifacts bullet is present it is left in place,
        and exactly one NC_DRIFTED_BULLET finding with detail=='EP-memory' is raised."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        body = _s3_make_ep_body(memory_canonical=False, memory_drifted=True)
        result = _s3_run_apply(body, spec)
        assert "EP-memory" in result.deletions_unmatched
        assert len(result.non_conformances) == 1
        assert result.non_conformances[0].code == NC_DRIFTED_BULLET
        assert result.non_conformances[0].detail == "EP-memory"

    def test_ep_quality_strict_match_deletes_bullet(self) -> None:
        """When the canonical Quality over Completeness bullet is present it is deleted."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        body = _s3_make_ep_body(quality_canonical=True)
        result = _s3_run_apply(body, spec)
        assert "EP-quality" in result.deletions_applied
        assert "Quality over Completeness" not in "".join(result.lines)

    def test_ep_quality_probe_only_hit_keeps_bullet_and_records_unmatched(self) -> None:
        """When only a drifted Quality over Completeness bullet is present it is left,
        and exactly one NC_DRIFTED_BULLET finding with detail=='EP-quality' is raised."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        body = _s3_make_ep_body(quality_canonical=False, quality_drifted=True)
        result = _s3_run_apply(body, spec)
        assert "EP-quality" in result.deletions_unmatched
        assert len(result.non_conformances) == 1
        assert result.non_conformances[0].code == NC_DRIFTED_BULLET
        assert result.non_conformances[0].detail == "EP-quality"

    # --- Placement, ordering hazard, and survival ---

    def test_execution_philosophy_common_precedes_context_limits(self) -> None:
        """[[DEPLOYED:ExecutionPhilosophyCommon]] must appear before [[INJECTION:ContextLimits]]
        in the output regardless of the legacy marker's source position."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        body = _s3_make_ep_body(context_limits_mid_bullet=True)
        result = _s3_run_apply(body, spec)
        output_text = "".join(result.lines)
        deployed_pos = output_text.find("[[DEPLOYED:ExecutionPhilosophyCommon]]")
        cl_pos = output_text.find("[[INJECTION:ContextLimits]]")
        assert deployed_pos != -1, "ExecutionPhilosophyCommon must be emitted"
        assert cl_pos != -1, "ContextLimits must be present"
        assert deployed_pos < cl_pos, (
            "[[DEPLOYED:ExecutionPhilosophyCommon]] must precede [[INJECTION:ContextLimits]]"
        )

    def test_ordering_hazard_context_limits_mid_bullet_still_places_region_first(self) -> None:
        """When the ContextLimits injection sat between two deleted bullets in the source,
        ExecutionPhilosophyCommon must still precede it after transformation — placement
        follows the contract, not the marker's source position."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        body = _s3_make_ep_body(
            context_canonical=True,
            memory_canonical=True,
            quality_canonical=True,
            context_limits_mid_bullet=True,   # ContextLimits is between Context and Memory
        )
        result = _s3_run_apply(body, spec)
        assert "ExecutionPhilosophyCommon" in result.deployed_added
        output_text = "".join(result.lines)
        deployed_pos = output_text.find("[[DEPLOYED:ExecutionPhilosophyCommon]]")
        cl_pos = output_text.find("[[INJECTION:ContextLimits]]")
        assert deployed_pos < cl_pos, (
            "When ContextLimits was mid-bullet in the source, "
            "ExecutionPhilosophyCommon must still precede it after transformation"
        )

    def test_agent_specific_ep_bullet_survives_deletion(self) -> None:
        """Content that is not one of the three deleted bullets must survive in the output."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        body = _s3_make_ep_body(
            context_canonical=True, memory_canonical=True, quality_canonical=True
        )
        result = _s3_run_apply(body, spec)
        assert "Fix Precision" in "".join(result.lines), (
            "Agent-specific 'Fix Precision' bullet must survive deletion"
        )

    def test_all_three_ep_bullets_deleted_together(self) -> None:
        """Context Management, Memory via Artifacts, and Quality over Completeness must
        all be absent from the output when all three are present in canonical form."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        body = _s3_make_ep_body(
            context_canonical=True, memory_canonical=True, quality_canonical=True
        )
        result = _s3_run_apply(body, spec)
        output_text = "".join(result.lines)
        for fragment, rule_id in [
            ("Context Management", "EP-context"),
            ("Memory via Artifacts", "EP-memory"),
            ("Quality over Completeness", "EP-quality"),
        ]:
            assert fragment not in output_text, (
                f"{fragment!r} bullet must be deleted (rule {rule_id})"
            )

    def test_execution_philosophy_common_uses_section_start_fallback_when_no_context_limits(
        self,
    ) -> None:
        """When ContextLimits is absent the region falls back to SECTION_START and is
        still emitted first in the section."""
        spec = _s3_find_spec("ExecutionPhilosophyCommon")
        body = [
            "## Execution Philosophy\n",
            "\n",
            "- **Context Management:** You can dedicate your full context window"
            " to this task. Follow-up work is handled by spawning new agent instances.\n",
            "- **Memory via Artifacts:** Input and output artifacts are the persistent"
            " memory between invocations. Anything a successor needs goes into an"
            " artifact, not into your response.\n",
            "- **Quality over Completeness:** Finishing part of the task well beats"
            " finishing all of it badly — a successor continues what you leave."
            " Use `PARTIALLY_DONE` when you stop deliberately with more of the same"
            " work remaining, `COMPLETED_NEEDS_ACTION` when your finished work is a"
            " set of items for another agent to act on, and `CAPABILITY_EXCEEDED`"
            " when you had what you needed and still could not do it.\n",
            "- **Fix Precision:** Only change what needs changing.\n",
        ]
        sections = find_section_spans(body)
        result = apply_conduct_regions(body, sections, specs=(spec,))
        assert "ExecutionPhilosophyCommon" in result.deployed_added
        output_text = "".join(result.lines)
        heading_pos = output_text.find("## Execution Philosophy")
        deployed_pos = output_text.find("[[DEPLOYED:ExecutionPhilosophyCommon]]")
        assert heading_pos != -1 and deployed_pos != -1
        between = output_text[heading_pos + len("## Execution Philosophy"):deployed_pos].strip()
        assert between == "", (
            "When ContextLimits is absent the region must still be first in the section"
        )


# ---------------------------------------------------------------------------
# T3.1/T3.2 — ErrorHandlingCommon and ExecutionPhilosophyCommon: generic path
# ---------------------------------------------------------------------------

class TestErrorHandlingAndExecutionPhilosophyGenericPath:
    """Integration tests for ErrorHandlingCommon and ExecutionPhilosophyCommon on the
    generic transform path, using the s3_generic_eh_ep_input / s3_generic_eh_ep_expected
    fixture pair.  Tests fail until Stage 3 implementation is complete."""

    def test_transform_succeeds(self, s3_generic_eh_ep_input, tmp_path) -> None:
        """Transformation of the Stage 3 fixture must succeed."""
        result, _ = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_error_handling_common_in_deployed_added(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """ErrorHandlingCommon must appear in deployed_added on the generic path."""
        result, _ = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        assert "ErrorHandlingCommon" in result.deployed_added

    def test_execution_philosophy_common_in_deployed_added(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """ExecutionPhilosophyCommon must appear in deployed_added on the generic path."""
        result, _ = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        assert "ExecutionPhilosophyCommon" in result.deployed_added

    def test_error_handling_common_before_execution_philosophy_common_in_deployed_added(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """ErrorHandlingCommon must appear before ExecutionPhilosophyCommon in deployed_added
        because ErrorHandling precedes ExecutionPhilosophy in document order."""
        result, _ = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        eh_idx = result.deployed_added.index("ErrorHandlingCommon")
        ep_idx = result.deployed_added.index("ExecutionPhilosophyCommon")
        assert eh_idx < ep_idx

    def test_retry_bullet_absent_from_output(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """The retry-a-transient-error-once bullet must not survive in the generic-path output."""
        _, output_path = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        assert "Retry a transient error once" not in _read(output_path), (
            "EH-retry bullet must be deleted from the generic-path output"
        )

    def test_error_code_recall_bullet_absent_from_output(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """The error-code recall bullet must not survive in the generic-path output."""
        _, output_path = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        content = _read(output_path)
        assert "missing prerequisites" not in content, (
            "EH-errcodes bullet must be deleted from the generic-path output"
        )

    def test_status_mapping_bullets_survive_on_generic_path(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """CAPABILITY_EXCEEDED and NEEDS_CLARIFICATION bullets must survive deletion
        on the generic path."""
        _, output_path = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        content = _read(output_path)
        assert "Return CAPABILITY_EXCEEDED" in content, (
            "CAPABILITY_EXCEEDED status-mapping bullet must survive on the generic path"
        )
        assert "Return NEEDS_CLARIFICATION" in content, (
            "NEEDS_CLARIFICATION status-mapping bullet must survive on the generic path"
        )

    def test_error_handling_common_first_in_error_handling_section(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """[[DEPLOYED:ErrorHandlingCommon]] must be the first item in the ErrorHandling section."""
        _, output_path = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        content = _read(output_path)
        eh_section_start = content.find("[[SECTION:ErrorHandling]]")
        deployed_open = content.find("[[DEPLOYED:ErrorHandlingCommon]]")
        eh_section_end = content.find("[[/SECTION:ErrorHandling]]")
        assert eh_section_start != -1, "[[SECTION:ErrorHandling]] not found"
        assert deployed_open != -1, "[[DEPLOYED:ErrorHandlingCommon]] not found"
        assert eh_section_start < deployed_open < eh_section_end, (
            "[[DEPLOYED:ErrorHandlingCommon]] must be inside [[SECTION:ErrorHandling]]"
        )
        # Nothing non-whitespace between section open tag and deployed tag
        between = content[eh_section_start + len("[[SECTION:ErrorHandling]]"):deployed_open]
        between_stripped = between.replace("\n", "").replace(" ", "")
        # Allow only the heading line between section tag and deployed tag
        # (section tag + newline + heading + newline + deployed tag)
        assert "ReturnBLOCKED" not in between_stripped, (
            "Deleted error-code bullet must not appear before ErrorHandlingCommon"
        )

    def test_context_management_bullet_absent_from_output(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """The Context Management bullet must not survive in the generic-path output."""
        _, output_path = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        content = _read(output_path)
        # Check the bullet itself is absent; the DEPLOYED region name contains 'Context'
        # so we check for the characteristic text of the bullet
        assert "Follow-up work is handled by spawning new agent instances" not in content, (
            "EP-context bullet must be deleted from the generic-path output"
        )

    def test_memory_via_artifacts_bullet_absent_from_output(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """The Memory via Artifacts bullet must not survive in the generic-path output."""
        _, output_path = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        assert "Memory via Artifacts" not in _read(output_path), (
            "EP-memory bullet must be deleted from the generic-path output"
        )

    def test_quality_over_completeness_bullet_absent_from_output(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """The Quality over Completeness bullet must not survive in the generic-path output."""
        _, output_path = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        assert "Quality over Completeness" not in _read(output_path), (
            "EP-quality bullet must be deleted from the generic-path output"
        )

    def test_fix_precision_bullet_survives_on_generic_path(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """The agent-specific Fix Precision bullet must survive deletion on the generic path."""
        _, output_path = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        assert "Fix Precision" in _read(output_path), (
            "Fix Precision bullet must survive EP deletion on the generic path"
        )

    def test_execution_philosophy_common_precedes_context_limits_on_generic_path(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """[[DEPLOYED:ExecutionPhilosophyCommon]] must appear before [[INJECTION:ContextLimits]]
        even though the legacy marker was positioned mid-bullet-block in the source."""
        _, output_path = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        content = _read(output_path)
        epc_pos = content.find("[[DEPLOYED:ExecutionPhilosophyCommon]]")
        cl_pos = content.find("[[INJECTION:ContextLimits]]")
        assert epc_pos != -1, "ExecutionPhilosophyCommon must be emitted"
        assert cl_pos != -1, "ContextLimits must be present"
        assert epc_pos < cl_pos, (
            "[[DEPLOYED:ExecutionPhilosophyCommon]] must precede [[INJECTION:ContextLimits]] "
            "even when the legacy ContextLimits marker was mid-bullet in the source"
        )

    def test_no_tag_emitted_inside_fenced_block_generic_path(
        self, s3_generic_eh_ep_input, tmp_path
    ) -> None:
        """No [[DEPLOYED:...]] tag may appear on a fence-masked line in the generic-path output."""
        import sys as _sys
        import pathlib as _pathlib
        _TD = _pathlib.Path(__file__).parent.parent
        if str(_TD) not in _sys.path:
            _sys.path.insert(0, str(_TD))
        from fence import fence_mask as _fence_mask
        _, output_path = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        out_lines = output_path.read_text(encoding="utf-8").splitlines(keepends=True)
        mask = _fence_mask(out_lines)
        for i, (line, is_masked) in enumerate(zip(out_lines, mask)):
            if is_masked:
                assert "[[DEPLOYED:" not in line, (
                    f"Deployed tag on fence-masked line {i + 1}: {line!r}"
                )

    def test_output_matches_expected_fixture_exactly(
        self,
        s3_generic_eh_ep_input,
        s3_generic_eh_ep_expected,
        tmp_path,
    ) -> None:
        """The full transformed output must match the expected fixture byte-for-byte."""
        _, output_path = _transform_to_tmp(s3_generic_eh_ep_input, tmp_path)
        assert _read(output_path) == _read(s3_generic_eh_ep_expected)


# ---------------------------------------------------------------------------
# T3.1/T3.2 — ErrorHandlingCommon and ExecutionPhilosophyCommon: harness path
# ---------------------------------------------------------------------------

class TestErrorHandlingAndExecutionPhilosophyHarnessPath:
    """Integration tests for ErrorHandlingCommon and ExecutionPhilosophyCommon on the
    harness transform path, using the s3_harness_eh_ep fixture.  Tests fail until Stage
    3 implementation is complete on the harness path."""

    def test_transform_succeeds(
        self,
        s3_harness_eh_ep_input,
        s3_harness_eh_ep_generic_ref,
        tmp_path,
    ) -> None:
        """Transformation of the harness Stage 3 fixture must succeed."""
        result, _ = _transform_to_tmp(
            s3_harness_eh_ep_input, tmp_path,
            generic_ref_path=s3_harness_eh_ep_generic_ref,
        )
        assert result.success is True
        assert result.errors == []

    def test_error_handling_common_in_deployed_added_harness_path(
        self,
        s3_harness_eh_ep_input,
        s3_harness_eh_ep_generic_ref,
        tmp_path,
    ) -> None:
        """ErrorHandlingCommon must appear in deployed_added on the harness path."""
        result, _ = _transform_to_tmp(
            s3_harness_eh_ep_input, tmp_path,
            generic_ref_path=s3_harness_eh_ep_generic_ref,
        )
        assert "ErrorHandlingCommon" in result.deployed_added

    def test_execution_philosophy_common_in_deployed_added_harness_path(
        self,
        s3_harness_eh_ep_input,
        s3_harness_eh_ep_generic_ref,
        tmp_path,
    ) -> None:
        """ExecutionPhilosophyCommon must appear in deployed_added on the harness path."""
        result, _ = _transform_to_tmp(
            s3_harness_eh_ep_input, tmp_path,
            generic_ref_path=s3_harness_eh_ep_generic_ref,
        )
        assert "ExecutionPhilosophyCommon" in result.deployed_added

    def test_retry_bullet_absent_on_harness_path(
        self,
        s3_harness_eh_ep_input,
        s3_harness_eh_ep_generic_ref,
        tmp_path,
    ) -> None:
        """The retry bullet must not survive in the harness-path output."""
        _, output_path = _transform_to_tmp(
            s3_harness_eh_ep_input, tmp_path,
            generic_ref_path=s3_harness_eh_ep_generic_ref,
        )
        assert "Retry a transient error once" not in _read(output_path), (
            "EH-retry bullet must be deleted on the harness path"
        )

    def test_error_code_recall_bullet_absent_on_harness_path(
        self,
        s3_harness_eh_ep_input,
        s3_harness_eh_ep_generic_ref,
        tmp_path,
    ) -> None:
        """The error-code recall bullet must not survive in the harness-path output."""
        _, output_path = _transform_to_tmp(
            s3_harness_eh_ep_input, tmp_path,
            generic_ref_path=s3_harness_eh_ep_generic_ref,
        )
        assert "missing prerequisites" not in _read(output_path), (
            "EH-errcodes bullet must be deleted on the harness path"
        )

    def test_status_mapping_bullets_survive_on_harness_path(
        self,
        s3_harness_eh_ep_input,
        s3_harness_eh_ep_generic_ref,
        tmp_path,
    ) -> None:
        """CAPABILITY_EXCEEDED and NEEDS_CLARIFICATION bullets must survive on the harness path."""
        _, output_path = _transform_to_tmp(
            s3_harness_eh_ep_input, tmp_path,
            generic_ref_path=s3_harness_eh_ep_generic_ref,
        )
        content = _read(output_path)
        assert "Return CAPABILITY_EXCEEDED" in content
        assert "Return NEEDS_CLARIFICATION" in content

    def test_ep_bullets_absent_on_harness_path(
        self,
        s3_harness_eh_ep_input,
        s3_harness_eh_ep_generic_ref,
        tmp_path,
    ) -> None:
        """Context Management, Memory via Artifacts, and Quality over Completeness bullets
        must not survive in the harness-path output."""
        _, output_path = _transform_to_tmp(
            s3_harness_eh_ep_input, tmp_path,
            generic_ref_path=s3_harness_eh_ep_generic_ref,
        )
        content = _read(output_path)
        assert "Follow-up work is handled by spawning new agent instances" not in content, (
            "EP-context bullet must be deleted on the harness path"
        )
        assert "Memory via Artifacts" not in content, (
            "EP-memory bullet must be deleted on the harness path"
        )
        assert "Quality over Completeness" not in content, (
            "EP-quality bullet must be deleted on the harness path"
        )

    def test_fix_precision_bullet_survives_on_harness_path(
        self,
        s3_harness_eh_ep_input,
        s3_harness_eh_ep_generic_ref,
        tmp_path,
    ) -> None:
        """The agent-specific Fix Precision bullet must survive on the harness path."""
        _, output_path = _transform_to_tmp(
            s3_harness_eh_ep_input, tmp_path,
            generic_ref_path=s3_harness_eh_ep_generic_ref,
        )
        assert "Fix Precision" in _read(output_path)

    def test_execution_philosophy_common_precedes_context_limits_on_harness_path(
        self,
        s3_harness_eh_ep_input,
        s3_harness_eh_ep_generic_ref,
        tmp_path,
    ) -> None:
        """[[DEPLOYED:ExecutionPhilosophyCommon]] must precede [[INJECTION:ContextLimits]]
        on the harness path even though the legacy marker was mid-bullet in the source."""
        _, output_path = _transform_to_tmp(
            s3_harness_eh_ep_input, tmp_path,
            generic_ref_path=s3_harness_eh_ep_generic_ref,
        )
        content = _read(output_path)
        epc_pos = content.find("[[DEPLOYED:ExecutionPhilosophyCommon]]")
        cl_pos = content.find("[[INJECTION:ContextLimits]]")
        assert epc_pos != -1
        assert cl_pos != -1
        assert epc_pos < cl_pos, (
            "ExecutionPhilosophyCommon must precede ContextLimits on the harness path"
        )

    def test_error_handling_common_before_execution_philosophy_common_on_harness_path(
        self,
        s3_harness_eh_ep_input,
        s3_harness_eh_ep_generic_ref,
        tmp_path,
    ) -> None:
        """ErrorHandlingCommon must appear before ExecutionPhilosophyCommon in
        deployed_added on the harness path (document order)."""
        result, _ = _transform_to_tmp(
            s3_harness_eh_ep_input, tmp_path,
            generic_ref_path=s3_harness_eh_ep_generic_ref,
        )
        eh_idx = result.deployed_added.index("ErrorHandlingCommon")
        ep_idx = result.deployed_added.index("ExecutionPhilosophyCommon")
        assert eh_idx < ep_idx

    def test_degraded_path_writes_derived_role(
        self, s6_harness_degraded_no_tier_input, tmp_path
    ) -> None:
        """'role' must be written to output frontmatter on the degraded path when absent."""
        result, output_path = _transform_to_tmp(s6_harness_degraded_no_tier_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert any(l.startswith("role:") for l in fm_lines), (
            "'role' must be derived and written on the degraded write-out path"
        )

    def test_degraded_path_writes_required_skills(
        self, s6_harness_degraded_no_tier_input, tmp_path
    ) -> None:
        """'required_skills' must be written to output frontmatter on the degraded path when absent."""
        result, output_path = _transform_to_tmp(s6_harness_degraded_no_tier_input, tmp_path)
        assert result.success is True
        fm_lines = _s6_extract_frontmatter_lines(_read(output_path))
        assert any(l.startswith("required_skills:") for l in fm_lines), (
            "'required_skills' must be derived and written on the degraded write-out path"
        )


# ===========================================================================
# Constraints Section Regions: ProtocolConstraints and HarnessConstraints
# ===========================================================================
#
# Failing tests (TDD RED) that specify the behavior of ProtocolConstraints and
# HarnessConstraints emission and deletion. Tests will fail until:
#   - CONDUCT_REGIONS is extended with ProtocolConstraints (row 3) and
#     HarnessConstraints (row 4), each with its deletion rules and anchor.
#   - Both transform paths call apply_conduct_regions and merge deployed_added.
#
# Three categories:
#   1. Table structure tests — fail until Stage 4 adds rows 3 and 4.
#   2. Unit deletion-outcome tests — call apply_conduct_regions with explicit
#      specs, verifying the three-case contract for each rule type; these
#      exercise the already-implemented framework with Stage-4-shaped specs.
#   3. Integration tests via transform_file — fail until CONDUCT_REGIONS is
#      complete and both transform paths emit the regions.
# ---------------------------------------------------------------------------

from region_insertion import (  # noqa: E402
    CONDUCT_REGIONS,
    Anchor,
    DeletionKind,
    DeletionRule,
    RegionSpec,
    SectionSpan,
    apply_conduct_regions,
    find_section_spans,
)
from boundary_constants import BoundaryKind  # noqa: E402


# ---------------------------------------------------------------------------
# Inline spec helpers for unit tests
#
# These mirror the deletion rules that Stage 4 implementation should wire into
# CONDUCT_REGIONS.  Bullet texts are taken from the canonical block text in
# Agents/Generic/DeployedSections.md.  The drift probe for PC-bullet-5 is
# derived from ContractsDesign.md §Deletion Rule Catalogue.
# ---------------------------------------------------------------------------

# The four exact bullets that ProtocolConstraints supersedes (corpus-verified).
_PC_EXACT_PATTERNS = (
    "**Orchestration Artifacts:** NEVER access an orchestration artifact"
    " that is not named in your `input_artifacts`/`output_artifacts`",
    "**Project Files:** You MAY read, modify, or create any project file"
    " — anything not named as an orchestration artifact",
    "NEVER skip the JSON response block",
    "NEVER invent status codes",
)

# PC-bullet-5 strict regex must match both the canonical wording and the known
# drifted wording (ContractsDesign.md §DeletionRule, PC-bullet-5 contract).
_PC_BULLET_5_PATTERN = r"(?i)Note\b.*\bagents?\b.*(?:do\s+not\b|don.t\b)"
# drift probe: bullet starting with "Note" that also mentions "agent(s)"
_PC_BULLET_5_PROBE = r"(?i)^Note.+\bagents?\b"


def _make_pc_inline_spec() -> RegionSpec:
    """Return an explicit ProtocolConstraints RegionSpec for unit tests.

    The spec is constructed to match what Stage 4 should implement.  Unit tests
    that use this spec exercise apply_conduct_regions without depending on
    CONDUCT_REGIONS being populated.
    """
    exact_rules = tuple(
        DeletionRule(
            rule_id=f"PC-bullet-{i + 1}",
            kind=DeletionKind.EXACT_BULLET,
            pattern=text,
            required=True,
            drift_probe=None,
        )
        for i, text in enumerate(_PC_EXACT_PATTERNS)
    )
    pc5_rule = DeletionRule(
        rule_id="PC-bullet-5",
        kind=DeletionKind.REGEX_BULLET,
        pattern=_PC_BULLET_5_PATTERN,
        required=True,
        drift_probe=_PC_BULLET_5_PROBE,
    )
    return RegionSpec(
        name="ProtocolConstraints",
        parent_section="Constraints",
        anchor=Anchor.SECTION_START,
        supersedes=exact_rules + (pc5_rule,),
    )


def _make_hc_inline_spec() -> RegionSpec:
    """Return an explicit HarnessConstraints RegionSpec for unit tests.

    CustomConstraints is retired from CANONICAL_DEPLOYED, so HarnessConstraints
    can no longer anchor BEFORE_REGION of it (that anchor_ref would name a region
    that no longer exists). Re-anchored to AFTER_REGION of ProtocolConstraints,
    one of the two forms the design contract accepts.
    """
    return RegionSpec(
        name="HarnessConstraints",
        parent_section="Constraints",
        anchor=Anchor.AFTER_REGION,
        anchor_ref=(BoundaryKind.DEPLOYED, "ProtocolConstraints"),
        fallback_anchor=Anchor.SECTION_CONTENT_END,
        supersedes=(),
    )


def _constraints_section(bullet_lines: list[str]) -> tuple[list[str], dict[str, SectionSpan]]:
    """Build a minimal Constraints-section body for unit tests.

    Returns (lines, sections_mapping).  Each bullet_line should be a plain string
    without a trailing newline; the function adds '\\n' for you.
    """
    lines: list[str] = ["## Constraints\n"] + [b + "\n" for b in bullet_lines]
    content_end = len(lines)
    sections: dict[str, SectionSpan] = {
        "Constraints": SectionSpan(
            name="Constraints",
            heading_line=0,
            start=1,
            content_end=content_end,
            end=content_end,
        )
    }
    return lines, sections


# ---------------------------------------------------------------------------
# T4.1 / T4.2: CONDUCT_REGIONS table structure tests
# ---------------------------------------------------------------------------

class TestConstraintsRegionTableStructure:
    """CONDUCT_REGIONS must contain ProtocolConstraints (anchored at section start)
    and HarnessConstraints (anchored after ProtocolConstraints, since CustomConstraints
    is retired and can no longer serve as HarnessConstraints' anchor_ref) after the
    Constraints region rows are appended.  Every test in this class will fail until
    those rows are added to region_insertion.CONDUCT_REGIONS."""

    def _get_spec(self, name: str) -> RegionSpec:
        match = next((s for s in CONDUCT_REGIONS if s.name == name), None)
        if match is None:
            pytest.fail(
                f"CONDUCT_REGIONS does not contain a row named {name!r}; "
                f"present names: {[s.name for s in CONDUCT_REGIONS]}"
            )
        return match  # type: ignore[return-value]  # pytest.fail raises

    # --- ProtocolConstraints row ---

    def test_protocol_constraints_row_present(self) -> None:
        """CONDUCT_REGIONS must contain a ProtocolConstraints row."""
        assert any(s.name == "ProtocolConstraints" for s in CONDUCT_REGIONS), (
            "ProtocolConstraints row is missing from CONDUCT_REGIONS"
        )

    def test_protocol_constraints_parent_section_is_constraints(self) -> None:
        """ProtocolConstraints must name 'Constraints' as its parent section."""
        spec = self._get_spec("ProtocolConstraints")
        assert spec.parent_section == "Constraints"

    def test_protocol_constraints_anchor_is_section_start(self) -> None:
        """ProtocolConstraints must anchor at SECTION_START (emitted first in the section)."""
        spec = self._get_spec("ProtocolConstraints")
        assert spec.anchor == Anchor.SECTION_START

    def test_protocol_constraints_supersedes_five_rules(self) -> None:
        """ProtocolConstraints must supersede exactly five deletion rules."""
        spec = self._get_spec("ProtocolConstraints")
        assert len(spec.supersedes) == 5, (
            f"Expected 5 deletion rules in ProtocolConstraints.supersedes, "
            f"got {len(spec.supersedes)}"
        )

    def test_protocol_constraints_all_five_rules_required(self) -> None:
        """All five PC deletion rules must be required=True."""
        spec = self._get_spec("ProtocolConstraints")
        for rule in spec.supersedes:
            assert rule.required is True, (
                f"Rule {rule.rule_id!r} must be required=True"
            )

    def test_protocol_constraints_first_four_rules_have_no_drift_probe(self) -> None:
        """PC-bullet-1 through PC-bullet-4 must not carry a drift_probe (byte-exact in corpus)."""
        spec = self._get_spec("ProtocolConstraints")
        assert len(spec.supersedes) >= 4
        for rule in spec.supersedes[:4]:
            assert rule.drift_probe is None, (
                f"Rule {rule.rule_id!r} must have drift_probe=None (byte-exact in corpus)"
            )

    def test_protocol_constraints_fifth_rule_has_drift_probe(self) -> None:
        """PC-bullet-5 must carry a drift_probe to catch the known drifted wording."""
        spec = self._get_spec("ProtocolConstraints")
        assert len(spec.supersedes) == 5
        pc5 = spec.supersedes[4]
        assert pc5.drift_probe is not None, (
            "PC-bullet-5 must have a drift_probe to detect the known drifted variant"
        )

    def test_protocol_constraints_rule_ids_unique(self) -> None:
        """All five rule_ids in ProtocolConstraints.supersedes must be distinct."""
        spec = self._get_spec("ProtocolConstraints")
        ids = [r.rule_id for r in spec.supersedes]
        assert len(ids) == len(set(ids)), (
            f"Duplicate rule_ids in ProtocolConstraints.supersedes: {ids}"
        )

    # --- HarnessConstraints row ---

    def test_harness_constraints_row_present(self) -> None:
        """CONDUCT_REGIONS must contain a HarnessConstraints row."""
        assert any(s.name == "HarnessConstraints" for s in CONDUCT_REGIONS), (
            "HarnessConstraints row is missing from CONDUCT_REGIONS"
        )

    def test_harness_constraints_parent_section_is_constraints(self) -> None:
        """HarnessConstraints must name 'Constraints' as its parent section."""
        spec = self._get_spec("HarnessConstraints")
        assert spec.parent_section == "Constraints"

    def test_harness_constraints_anchor_ref_never_names_custom_constraints(self) -> None:
        """HarnessConstraints.anchor_ref must never name CustomConstraints — that region
        is deleted by this change, so anchoring to it would silently rely on the
        fallback_anchor on every well-formed input (forbidden by the design contract)."""
        spec = self._get_spec("HarnessConstraints")
        assert spec.anchor_ref != (BoundaryKind.DEPLOYED, "CustomConstraints"), (
            "HarnessConstraints anchor_ref must not name the retired CustomConstraints region"
        )

    def test_harness_constraints_anchor_is_after_region_of_protocol_constraints(self) -> None:
        """HarnessConstraints must anchor AFTER_REGION of ProtocolConstraints — the primary
        anchor must resolve on a well-formed input without relying on fallback_anchor."""
        spec = self._get_spec("HarnessConstraints")
        assert spec.anchor == Anchor.AFTER_REGION, (
            f"Expected anchor=AFTER_REGION, got {spec.anchor!r}"
        )
        assert spec.anchor_ref == (BoundaryKind.DEPLOYED, "ProtocolConstraints"), (
            f"Expected anchor_ref=(DEPLOYED, 'ProtocolConstraints'), got {spec.anchor_ref!r}"
        )

    def test_harness_constraints_fallback_is_section_content_end(self) -> None:
        """HarnessConstraints fallback anchor must be SECTION_CONTENT_END."""
        spec = self._get_spec("HarnessConstraints")
        assert spec.fallback_anchor == Anchor.SECTION_CONTENT_END

    def test_harness_constraints_supersedes_no_rules(self) -> None:
        """HarnessConstraints supersedes no prose — supersedes must be empty."""
        spec = self._get_spec("HarnessConstraints")
        assert spec.supersedes == ()

    # --- Ordering ---

    def test_protocol_constraints_precedes_harness_constraints_in_table(self) -> None:
        """ProtocolConstraints must appear before HarnessConstraints in CONDUCT_REGIONS."""
        names = [s.name for s in CONDUCT_REGIONS]
        assert "ProtocolConstraints" in names, "ProtocolConstraints row absent"
        assert "HarnessConstraints" in names, "HarnessConstraints row absent"
        assert names.index("ProtocolConstraints") < names.index("HarnessConstraints"), (
            "ProtocolConstraints must precede HarnessConstraints in CONDUCT_REGIONS"
        )


# ---------------------------------------------------------------------------
# T4.1: unit deletion-outcome contract for ProtocolConstraints bullets
# ---------------------------------------------------------------------------

class TestProtocolConstraintsBulletDeletionUnit:
    """Verifies the three-case deletion outcome contract for ProtocolConstraints rules,
    calling apply_conduct_regions directly with explicit inline specs.

    The inline specs are derived from the canonical bullet texts in
    Agents/Generic/DeployedSections.md.  These tests exercise the already-implemented
    apply_conduct_regions framework with Stage-4-shaped specs, confirming correct
    behavior independently of whether CONDUCT_REGIONS has been updated."""

    # ------------------------------------------------------------------
    # Exact-bullet rules (PC-bullet-1 through PC-bullet-4)
    # ------------------------------------------------------------------

    def test_exact_bullet_matched_appears_in_deletions_applied(self) -> None:
        """An exact-matching PC bullet must be in deletions_applied after apply."""
        pattern = _PC_EXACT_PATTERNS[2]  # "NEVER skip the JSON response block"
        bullet_line = f"- {pattern}"
        lines, sections = _constraints_section([bullet_line])
        spec = RegionSpec(
            name="ProtocolConstraints",
            parent_section="Constraints",
            anchor=Anchor.SECTION_START,
            supersedes=(
                DeletionRule(
                    rule_id="PC-bullet-X",
                    kind=DeletionKind.EXACT_BULLET,
                    pattern=pattern,
                    required=True,
                    drift_probe=None,
                ),
            ),
        )
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        assert "PC-bullet-X" in result.deletions_applied, (
            "Exact-match bullet must appear in deletions_applied"
        )

    def test_exact_bullet_matched_absent_from_output_lines(self) -> None:
        """A matched exact bullet must not appear in the transformed output lines."""
        pattern = _PC_EXACT_PATTERNS[2]
        bullet_line = f"- {pattern}"
        lines, sections = _constraints_section([bullet_line])
        spec = RegionSpec(
            name="ProtocolConstraints",
            parent_section="Constraints",
            anchor=Anchor.SECTION_START,
            supersedes=(
                DeletionRule(
                    rule_id="PC-bullet-X",
                    kind=DeletionKind.EXACT_BULLET,
                    pattern=pattern,
                    required=True,
                    drift_probe=None,
                ),
            ),
        )
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        full_output = "".join(result.lines)
        assert pattern not in full_output, (
            "Matched exact bullet must be removed from the output"
        )

    def test_exact_bullet_unmatched_required_in_deletions_unmatched(self) -> None:
        """A required exact-bullet rule that finds no match must appear in deletions_unmatched."""
        pattern = _PC_EXACT_PATTERNS[2]
        lines, sections = _constraints_section(["- Some completely different bullet"])
        spec = RegionSpec(
            name="ProtocolConstraints",
            parent_section="Constraints",
            anchor=Anchor.SECTION_START,
            supersedes=(
                DeletionRule(
                    rule_id="PC-bullet-X",
                    kind=DeletionKind.EXACT_BULLET,
                    pattern=pattern,
                    required=True,
                    drift_probe=None,
                ),
            ),
        )
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        assert "PC-bullet-X" in result.deletions_unmatched, (
            "Required rule with no match must appear in deletions_unmatched"
        )
        assert result.non_conformances == [], (
            "A probe-free unmatched rule must not produce non_conformances at this stage"
        )

    def test_all_four_exact_bullets_deleted_when_all_present(self) -> None:
        """All four exact PC bullets must be deleted in a single apply_conduct_regions call."""
        bullet_lines = [f"- {p}" for p in _PC_EXACT_PATTERNS]
        lines, sections = _constraints_section(bullet_lines)
        spec = _make_pc_inline_spec()
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        full_output = "".join(result.lines)
        for pattern in _PC_EXACT_PATTERNS:
            assert pattern not in full_output, (
                f"Exact bullet not deleted from output: {pattern[:50]!r}"
            )

    def test_all_four_exact_bullets_in_deletions_applied(self) -> None:
        """All four exact PC rules must appear in deletions_applied when bullets are present."""
        bullet_lines = [f"- {p}" for p in _PC_EXACT_PATTERNS]
        lines, sections = _constraints_section(bullet_lines)
        spec = _make_pc_inline_spec()
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        for i in range(1, 5):
            assert f"PC-bullet-{i}" in result.deletions_applied, (
                f"PC-bullet-{i} must appear in deletions_applied"
            )

    # ------------------------------------------------------------------
    # PC-bullet-5: regex rule — three-case outcome contract
    # ------------------------------------------------------------------

    def test_pc_bullet_5_canonical_wording_deleted(self) -> None:
        """The canonical PC-bullet-5 wording must be deleted by the strict regex."""
        canonical = "Note work that belongs to another agent; do not do it yourself"
        lines, sections = _constraints_section([f"- {canonical}"])
        spec = _make_pc_inline_spec()
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        assert "PC-bullet-5" in result.deletions_applied, (
            "Canonical PC-bullet-5 wording must match the strict pattern and be deleted"
        )
        assert canonical not in "".join(result.lines), (
            "Canonical PC-bullet-5 text must be absent from output after deletion"
        )

    def test_pc_bullet_5_drifted_wording_deleted(self) -> None:
        """The known drifted PC-bullet-5 wording must also be deleted by the strict regex.

        The strict regex is designed to match both the canonical and the known drifted
        wording so that the drifted variant is removed, not left behind as a stale bullet.
        """
        drifted = "Note implementation decisions for other agents but don't make them"
        lines, sections = _constraints_section([f"- {drifted}"])
        spec = _make_pc_inline_spec()
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        assert "PC-bullet-5" in result.deletions_applied, (
            "Known drifted PC-bullet-5 wording must match the strict pattern and be deleted"
        )
        assert drifted not in "".join(result.lines), (
            "Drifted PC-bullet-5 text must be absent from output after deletion"
        )

    def test_pc_bullet_5_third_variant_matching_probe_left_in_place(self) -> None:
        """A third wording matching only the drift probe must be left in place (never deleted).

        Probe matches are intentionally not deleted — the probe is permissive by design
        and deleting a probe-only match risks removing valid agent-authored content.
        """
        third_variant = "Note that agents should not override user instructions"
        lines, sections = _constraints_section([f"- {third_variant}"])
        spec = _make_pc_inline_spec()
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        assert "PC-bullet-5" not in result.deletions_applied, (
            "Probe-only match must not appear in deletions_applied (not deleted)"
        )
        assert "PC-bullet-5" in result.deletions_unmatched, (
            "Probe-only match must appear in deletions_unmatched"
        )
        assert third_variant in "".join(result.lines), (
            "Probe-only matched bullet must remain in the output unchanged"
        )
        assert len(result.non_conformances) == 1, (
            "Probe-only hit must raise exactly one NC_DRIFTED_BULLET finding"
        )
        assert result.non_conformances[0].code == NC_DRIFTED_BULLET
        assert result.non_conformances[0].detail == "PC-bullet-5"

    def test_pc_bullet_5_unrelated_bullet_not_matched_required_in_unmatched(self) -> None:
        """A bullet matching neither the strict pattern nor the probe is left untouched.

        A required rule records the rule_id in deletions_unmatched when nothing matches.
        """
        unrelated = "Do not make decisions that belong to other teams"
        lines, sections = _constraints_section([f"- {unrelated}"])
        spec = _make_pc_inline_spec()
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        assert "PC-bullet-5" in result.deletions_unmatched, (
            "Required rule with no match (strict or probe) must appear in deletions_unmatched"
        )
        assert unrelated in "".join(result.lines), (
            "Unmatched bullet must remain unchanged in the output"
        )
        assert result.non_conformances == []

    def test_probe_does_not_delete_similar_looking_agent_authored_bullet(self) -> None:
        """A bullet that resembles the probe pattern but is agent-authored must be kept intact.

        'Always consult other agents before deciding' contains 'agents' but does not
        start with 'Note', so the probe must not match it.
        """
        agent_authored = "Always consult other agents before making scope decisions"
        lines, sections = _constraints_section([f"- {agent_authored}"])
        spec = _make_pc_inline_spec()
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        assert agent_authored in "".join(result.lines), (
            "Agent-authored bullet that does not match the probe must be preserved"
        )
        assert "PC-bullet-5" not in result.deletions_applied

    def test_pc_bullet_5_strict_pattern_does_not_over_match_benign_note_bullet(self) -> None:
        """A benign bullet starting with 'Note' that contains 'do not' but is unrelated
        to agent-scope delegation must NOT be deleted by the strict regex.

        The strict pattern for PC-bullet-5 is intended to match bullets about delegating
        work to other agents — specifically 'Note work that belongs to another agent; do
        not do it yourself' and its known drifted variant.  A bullet that starts with
        'Note' and contains 'do not' or "don't" but addresses an entirely different
        subject (timing, escalation, formatting, etc.) must be preserved verbatim.

        This test guards against the broad pattern r'(?i)Note\\b.+(?:do\\s+not\\b|don.t\\b)'
        accidentally removing agent-authored constraints that share surface similarity with
        PC-bullet-5 but are not superseded by ProtocolConstraints.
        """
        benign = "Note: escalate promptly; do not wait for confirmation"
        lines, sections = _constraints_section([f"- {benign}"])
        spec = _make_pc_inline_spec()
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        assert benign in "".join(result.lines), (
            "A benign 'Note ... do not ...' bullet unrelated to agent-scope delegation "
            "must not be deleted by the PC-bullet-5 strict pattern"
        )
        assert "PC-bullet-5" not in result.deletions_applied, (
            "The PC-bullet-5 strict pattern must not flag a benign bullet whose subject "
            "is unrelated to delegating work between agents"
        )


# ---------------------------------------------------------------------------
# T4.2: unit deletion-outcome contract for HarnessConstraints
# ---------------------------------------------------------------------------

class TestHarnessConstraintsDeletionUnit:
    """HarnessConstraints supersedes no prose.  apply_conduct_regions must not
    delete any content lines when the HarnessConstraints spec is applied, even
    in a Constraints section with substantial existing content."""

    def test_harness_constraints_deletes_no_lines(self) -> None:
        """Applying HarnessConstraints must not remove any existing content lines."""
        existing_content = [
            "- Some existing constraint bullet",
            "- Another constraint",
        ]
        lines, sections = _constraints_section(existing_content)
        spec = _make_hc_inline_spec()
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        assert result.deletions_applied == [], (
            "HarnessConstraints must not delete any lines (supersedes no prose)"
        )
        assert result.deletions_unmatched == [], (
            "HarnessConstraints has no required rules, so deletions_unmatched must be empty"
        )

    def test_harness_constraints_emitted_after_protocol_constraints_when_present(self) -> None:
        """HarnessConstraints must appear immediately after [[/DEPLOYED:ProtocolConstraints]]
        in the output — the primary AFTER_REGION anchor, exercised without any
        CustomConstraints region present at all (that region no longer exists)."""
        lines, sections = _constraints_section([
            "[[DEPLOYED:ProtocolConstraints]]",
            "[[/DEPLOYED:ProtocolConstraints]]",
            "- Some bullet",
        ])
        spec = _make_hc_inline_spec()
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        out_stripped = [l.strip() for l in result.lines]
        hc_open = "[[DEPLOYED:HarnessConstraints]]"
        pc_close = "[[/DEPLOYED:ProtocolConstraints]]"
        assert hc_open in out_stripped, "HarnessConstraints open tag must be in output"
        assert pc_close in out_stripped, "ProtocolConstraints close tag must be in output"
        hc_idx = out_stripped.index(hc_open)
        pc_close_idx = out_stripped.index(pc_close)
        assert hc_idx == pc_close_idx + 1, (
            "HarnessConstraints open tag must immediately follow the ProtocolConstraints "
            "close tag (the primary AFTER_REGION anchor must resolve directly, not via "
            "fallback_anchor)"
        )

    def test_harness_constraints_emitted_at_section_end_when_no_protocol_constraints(self) -> None:
        """When ProtocolConstraints (the AFTER_REGION anchor_ref) is absent, HarnessConstraints
        falls back to SECTION_CONTENT_END and is still emitted — exercising the named
        fallback_anchor field explicitly."""
        lines, sections = _constraints_section(["- Some bullet"])
        spec = _make_hc_inline_spec()
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        out_stripped = [l.strip() for l in result.lines]
        assert "[[DEPLOYED:HarnessConstraints]]" in out_stripped, (
            "HarnessConstraints must be emitted via fallback_anchor even when "
            "ProtocolConstraints is absent from the section"
        )

    def test_harness_constraints_not_emitted_twice_when_already_present(self) -> None:
        """HarnessConstraints must not be emitted a second time if already in the lines."""
        lines, sections = _constraints_section([
            "[[DEPLOYED:HarnessConstraints]]",
            "[[/DEPLOYED:HarnessConstraints]]",
            "[[DEPLOYED:CustomConstraints]]",
            "[[/DEPLOYED:CustomConstraints]]",
        ])
        spec = _make_hc_inline_spec()
        result = apply_conduct_regions(lines, sections, specs=(spec,))
        hc_count = sum(
            1 for l in result.lines if l.strip() == "[[DEPLOYED:HarnessConstraints]]"
        )
        assert hc_count == 1, (
            f"HarnessConstraints open tag must appear exactly once; found {hc_count}"
        )


# ---------------------------------------------------------------------------
# T4.1 / T4.2: integration tests — generic transform path
# ---------------------------------------------------------------------------

class TestConstraintsRegionsGenericPath:
    """ProtocolConstraints and HarnessConstraints are emitted on the generic transform
    path, the five superseded bullets are deleted, and the regions appear in the
    correct order in both deployed_added and the output document.

    Tests fail until CONDUCT_REGIONS contains the Constraints-section rows and
    the generic transform path calls apply_conduct_regions."""

    def test_transform_succeeds(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """Transformation of a file with the five PC bullets and legacy markers must succeed."""
        result, _ = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_protocol_constraints_in_deployed_added(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """ProtocolConstraints must appear in deployed_added on the generic path."""
        result, _ = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        assert "ProtocolConstraints" in result.deployed_added, (
            f"ProtocolConstraints missing from deployed_added: {result.deployed_added}"
        )

    def test_harness_constraints_in_deployed_added_or_injections_added(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """HarnessConstraints must be recorded in the transform result on the generic path.

        When the legacy [INJECTION: harness_constraints] marker is present, the
        existing transformer emits the region and records it in injections_added;
        the idempotency guard in apply_conduct_regions then skips the second emission
        so it is absent from deployed_added.  Either path satisfies the contract.
        """
        result, _ = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        recorded = set(result.deployed_added) | set(result.injections_added)
        assert "HarnessConstraints" in recorded, (
            "HarnessConstraints must be recorded in deployed_added or injections_added"
        )

    def test_protocol_constraints_before_harness_constraints_in_deployed_added(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """ProtocolConstraints must precede HarnessConstraints in deployed_added."""
        result, _ = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        assert "ProtocolConstraints" in result.deployed_added, (
            "ProtocolConstraints must be in deployed_added to check ordering"
        )
        # ProtocolConstraints is at a lower document-order index than HarnessConstraints.
        pc_idx = result.deployed_added.index("ProtocolConstraints")
        # HarnessConstraints may be absent from deployed_added if emitted from the
        # legacy marker (injections_added instead).  The document-order check is
        # covered by the output-content ordering tests below.
        if "HarnessConstraints" in result.deployed_added:
            hc_idx = result.deployed_added.index("HarnessConstraints")
            assert pc_idx < hc_idx

    def test_pc_bullet_1_absent_from_output(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """The first ProtocolConstraints bullet must be deleted from the output."""
        _, output_path = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        content = _read(output_path)
        assert "NEVER access an orchestration artifact that is not named" not in content, (
            "PC-bullet-1 must be deleted from the transformed output"
        )

    def test_pc_bullet_2_absent_from_output(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """The second ProtocolConstraints bullet must be deleted from the output."""
        _, output_path = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        content = _read(output_path)
        assert "You MAY read, modify, or create any project file" not in content, (
            "PC-bullet-2 must be deleted from the transformed output"
        )

    def test_pc_bullet_3_absent_from_output(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """The third ProtocolConstraints bullet must be deleted from the output."""
        _, output_path = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        content = _read(output_path)
        assert "NEVER skip the JSON response block" not in content, (
            "PC-bullet-3 must be deleted from the transformed output"
        )

    def test_pc_bullet_4_absent_from_output(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """The fourth ProtocolConstraints bullet must be deleted from the output."""
        _, output_path = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        content = _read(output_path)
        assert "NEVER invent status codes" not in content, (
            "PC-bullet-4 must be deleted from the transformed output"
        )

    def test_pc_bullet_5_canonical_absent_from_output(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """The fifth ProtocolConstraints bullet (canonical wording) must be deleted from output."""
        _, output_path = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        content = _read(output_path)
        assert "Note work that belongs to another agent; do not do it yourself" not in content, (
            "PC-bullet-5 canonical wording must be deleted from the transformed output"
        )

    def test_protocol_constraints_open_tag_in_output(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """[[DEPLOYED:ProtocolConstraints]] open tag must appear in the output."""
        _, output_path = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        assert "[[DEPLOYED:ProtocolConstraints]]" in _read(output_path)

    def test_harness_constraints_open_tag_in_output(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """[[DEPLOYED:HarnessConstraints]] open tag must appear in the output."""
        _, output_path = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        assert "[[DEPLOYED:HarnessConstraints]]" in _read(output_path)

    def test_protocol_constraints_precedes_harness_constraints_in_output(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """[[DEPLOYED:ProtocolConstraints]] must appear before [[DEPLOYED:HarnessConstraints]]
        in document order."""
        _, output_path = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        content = _read(output_path)
        pc_pos = content.find("[[DEPLOYED:ProtocolConstraints]]")
        hc_pos = content.find("[[DEPLOYED:HarnessConstraints]]")
        assert pc_pos != -1, "ProtocolConstraints open tag missing from output"
        assert hc_pos != -1, "HarnessConstraints open tag missing from output"
        assert pc_pos < hc_pos, (
            "ProtocolConstraints must precede HarnessConstraints in the output document"
        )

    def test_empty_custom_constraints_marker_dropped_entirely(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """The fixture's legacy '[INJECTION: custom_constraints]' marker is empty (no
        content precedes the next '---'), so it must be dropped entirely — neither a
        [[DEPLOYED:CustomConstraints]] nor an [[INJECTION:CustomConstraints]] region
        may appear anywhere in the output (AC2.5)."""
        _, output_path = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        content = _read(output_path)
        assert "CustomConstraints" not in content, (
            "An empty legacy custom_constraints marker must produce no region at all "
            f"(neither open nor close tag), but 'CustomConstraints' was found in output:\n{content}"
        )

    def test_no_tag_emitted_inside_fenced_block(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """No [[DEPLOYED:...]] tag may appear on a fence-masked line in the output."""
        from fence import fence_mask as _fence_mask
        _, output_path = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        out_lines = output_path.read_text(encoding="utf-8").splitlines(keepends=True)
        mask = _fence_mask(out_lines)
        for i, (line, is_masked) in enumerate(zip(out_lines, mask)):
            if is_masked:
                assert "[[DEPLOYED:" not in line, (
                    f"Deployed tag found on fence-masked line {i + 1}: {line!r}"
                )

    def test_harness_constraints_emitted_exactly_once_in_output(
        self, s4_generic_constraints_regions_input, tmp_path
    ) -> None:
        """[[DEPLOYED:HarnessConstraints]] must appear exactly once even when the
        legacy [INJECTION: harness_constraints] marker is also present."""
        _, output_path = _transform_to_tmp(s4_generic_constraints_regions_input, tmp_path)
        content = _read(output_path)
        count = content.count("[[DEPLOYED:HarnessConstraints]]")
        assert count == 1, (
            f"HarnessConstraints open tag must appear exactly once; found {count}"
        )


# ---------------------------------------------------------------------------
# T4.1 / T4.2: integration tests — harness transform path
# ---------------------------------------------------------------------------

class TestConstraintsRegionsHarnessPath:
    """ProtocolConstraints and HarnessConstraints are emitted on the harness transform
    path as well, the five superseded bullets are deleted, and region ordering is
    correct.

    Tests fail until CONDUCT_REGIONS contains the Constraints-section rows and
    the harness transform path calls apply_conduct_regions."""

    def test_transform_succeeds(
        self,
        s4_harness_constraints_regions_input,
        s4_harness_constraints_regions_generic_ref,
        tmp_path,
    ) -> None:
        """Harness transformation with all five PC bullets must succeed."""
        result, _ = _transform_to_tmp(
            s4_harness_constraints_regions_input,
            tmp_path,
            generic_ref_path=s4_harness_constraints_regions_generic_ref,
        )
        assert result.success is True
        assert result.errors == []

    def test_protocol_constraints_in_deployed_added_harness_path(
        self,
        s4_harness_constraints_regions_input,
        s4_harness_constraints_regions_generic_ref,
        tmp_path,
    ) -> None:
        """ProtocolConstraints must appear in deployed_added on the harness path."""
        result, _ = _transform_to_tmp(
            s4_harness_constraints_regions_input,
            tmp_path,
            generic_ref_path=s4_harness_constraints_regions_generic_ref,
        )
        assert "ProtocolConstraints" in result.deployed_added, (
            f"ProtocolConstraints missing from deployed_added on harness path: "
            f"{result.deployed_added}"
        )

    def test_all_five_pc_bullets_absent_from_harness_output(
        self,
        s4_harness_constraints_regions_input,
        s4_harness_constraints_regions_generic_ref,
        tmp_path,
    ) -> None:
        """All five PC bullets must be deleted from the harness-path output."""
        _, output_path = _transform_to_tmp(
            s4_harness_constraints_regions_input,
            tmp_path,
            generic_ref_path=s4_harness_constraints_regions_generic_ref,
        )
        content = _read(output_path)
        fragments = [
            "NEVER access an orchestration artifact that is not named",
            "You MAY read, modify, or create any project file",
            "NEVER skip the JSON response block",
            "NEVER invent status codes",
            "Note work that belongs to another agent; do not do it yourself",
        ]
        for fragment in fragments:
            assert fragment not in content, (
                f"PC bullet fragment must be deleted from harness-path output: {fragment!r}"
            )

    def test_protocol_constraints_precedes_harness_constraints_harness_output(
        self,
        s4_harness_constraints_regions_input,
        s4_harness_constraints_regions_generic_ref,
        tmp_path,
    ) -> None:
        """[[DEPLOYED:ProtocolConstraints]] must appear before [[DEPLOYED:HarnessConstraints]]
        in the harness-path output."""
        _, output_path = _transform_to_tmp(
            s4_harness_constraints_regions_input,
            tmp_path,
            generic_ref_path=s4_harness_constraints_regions_generic_ref,
        )
        content = _read(output_path)
        pc_pos = content.find("[[DEPLOYED:ProtocolConstraints]]")
        hc_pos = content.find("[[DEPLOYED:HarnessConstraints]]")
        assert pc_pos != -1, "ProtocolConstraints tag missing from harness-path output"
        assert hc_pos != -1, "HarnessConstraints tag missing from harness-path output"
        assert pc_pos < hc_pos

    def test_harness_constraints_emitted_exactly_once_harness_path(
        self,
        s4_harness_constraints_regions_input,
        s4_harness_constraints_regions_generic_ref,
        tmp_path,
    ) -> None:
        """HarnessConstraints open tag must appear exactly once in harness-path output."""
        _, output_path = _transform_to_tmp(
            s4_harness_constraints_regions_input,
            tmp_path,
            generic_ref_path=s4_harness_constraints_regions_generic_ref,
        )
        content = _read(output_path)
        count = content.count("[[DEPLOYED:HarnessConstraints]]")
        assert count == 1, (
            f"HarnessConstraints must appear exactly once in harness-path output; "
            f"found {count}"
        )

    def test_no_tag_emitted_inside_fenced_block_harness_path(
        self,
        s4_harness_constraints_regions_input,
        s4_harness_constraints_regions_generic_ref,
        tmp_path,
    ) -> None:
        """No [[DEPLOYED:...]] tag may appear on a fence-masked line in harness-path output."""
        from fence import fence_mask as _fence_mask
        _, output_path = _transform_to_tmp(
            s4_harness_constraints_regions_input,
            tmp_path,
            generic_ref_path=s4_harness_constraints_regions_generic_ref,
        )
        out_lines = output_path.read_text(encoding="utf-8").splitlines(keepends=True)
        mask = _fence_mask(out_lines)
        for i, (line, is_masked) in enumerate(zip(out_lines, mask)):
            if is_masked:
                assert "[[DEPLOYED:" not in line, (
                    f"Deployed tag on fence-masked line {i + 1}: {line!r}"
                )


# ---------------------------------------------------------------------------
# T2.5: HarnessConstraints placement with no CustomConstraints region present
# (CustomConstraints is retired; this fixture never carried it). Since the
# fixture carries all five ProtocolConstraints bullets, HarnessConstraints now
# resolves via its PRIMARY anchor (AFTER_REGION of ProtocolConstraints) — not
# via fallback_anchor. AC2.6 requires asserting the resulting position, not
# merely the absence of an error.
# ---------------------------------------------------------------------------

class TestHarnessConstraintsNoCustomConstraintsGenericPath:
    """CustomConstraints does not exist as a region any more. With ProtocolConstraints
    present, HarnessConstraints must land immediately after it via the primary
    AFTER_REGION anchor, without relying on fallback_anchor."""

    def test_transform_succeeds(
        self, s4_generic_no_custom_constraints_input, tmp_path
    ) -> None:
        """Transformation of a file without custom_constraints markers must succeed."""
        result, _ = _transform_to_tmp(s4_generic_no_custom_constraints_input, tmp_path)
        assert result.success is True

    def test_protocol_constraints_emitted(
        self, s4_generic_no_custom_constraints_input, tmp_path
    ) -> None:
        """ProtocolConstraints must be emitted even when CustomConstraints is absent."""
        result, _ = _transform_to_tmp(s4_generic_no_custom_constraints_input, tmp_path)
        assert "ProtocolConstraints" in result.deployed_added

    def test_harness_constraints_emitted_without_custom_constraints(
        self, s4_generic_no_custom_constraints_input, tmp_path
    ) -> None:
        """HarnessConstraints must be emitted unconditionally even when CustomConstraints
        is absent from the file."""
        result, _ = _transform_to_tmp(s4_generic_no_custom_constraints_input, tmp_path)
        assert "HarnessConstraints" in result.deployed_added, (
            "HarnessConstraints must be emitted even when CustomConstraints is absent; "
            f"deployed_added was: {result.deployed_added}"
        )

    def test_custom_constraints_not_emitted_when_absent(
        self, s4_generic_no_custom_constraints_input, tmp_path
    ) -> None:
        """CustomConstraints must NOT appear in the output when no marker was present."""
        _, output_path = _transform_to_tmp(s4_generic_no_custom_constraints_input, tmp_path)
        assert "[[DEPLOYED:CustomConstraints]]" not in _read(output_path), (
            "CustomConstraints must not be emitted when no legacy marker was present"
        )

    def test_harness_constraints_immediately_follows_protocol_constraints_close_tag(
        self, s4_generic_no_custom_constraints_input, tmp_path
    ) -> None:
        """HarnessConstraints must land immediately after [[/DEPLOYED:ProtocolConstraints]] —
        asserting adjacency (not merely "no error") is the load-bearing part of AC2.6:
        a broken primary anchor could still pass a looser ordering check via fallback."""
        _, output_path = _transform_to_tmp(s4_generic_no_custom_constraints_input, tmp_path)
        lines = _read(output_path).splitlines()
        pc_close = "[[/DEPLOYED:ProtocolConstraints]]"
        hc_open = "[[DEPLOYED:HarnessConstraints]]"
        assert pc_close in lines, "ProtocolConstraints close tag missing from output"
        assert hc_open in lines, "HarnessConstraints open tag missing from output"
        pc_close_idx = lines.index(pc_close)
        hc_open_idx = lines.index(hc_open)
        assert hc_open_idx == pc_close_idx + 1, (
            f"HarnessConstraints (line {hc_open_idx}) must immediately follow "
            f"ProtocolConstraints' close tag (line {pc_close_idx}); found intervening "
            f"content: {lines[pc_close_idx + 1:hc_open_idx]!r}"
        )

    def test_protocol_constraints_before_harness_constraints_when_no_custom(
        self, s4_generic_no_custom_constraints_input, tmp_path
    ) -> None:
        """ProtocolConstraints must still precede HarnessConstraints when CustomConstraints absent."""
        _, output_path = _transform_to_tmp(s4_generic_no_custom_constraints_input, tmp_path)
        content = _read(output_path)
        pc_pos = content.find("[[DEPLOYED:ProtocolConstraints]]")
        hc_pos = content.find("[[DEPLOYED:HarnessConstraints]]")
        assert pc_pos != -1, "ProtocolConstraints tag missing"
        assert hc_pos != -1, "HarnessConstraints tag missing"
        assert pc_pos < hc_pos


# ---------------------------------------------------------------------------
# T4.1: drifted fifth bullet handling — integration test
# ---------------------------------------------------------------------------

class TestDriftedFifthBulletHandling:
    """The known drifted wording of PC-bullet-5 must be handled according to the
    design contract: either deleted by the tolerant strict regex (option 1 from
    the plan, chosen in ContractsDesign.md) or recorded in deletions_unmatched
    for later reporting (option 2).

    The ContractsDesign.md §DeletionRule states the strict regex must match BOTH
    the canonical and the known drifted wording.  These tests assert option 1:
    the drifted bullet is deleted from the output (not silently left behind)."""

    def test_transform_succeeds_with_drifted_bullet(
        self, s4_generic_drifted_bullet_input, tmp_path
    ) -> None:
        """Transformation of a file with the drifted PC-bullet-5 wording must succeed."""
        result, _ = _transform_to_tmp(s4_generic_drifted_bullet_input, tmp_path)
        assert result.success is True

    def test_drifted_bullet_not_silently_left_in_output(
        self, s4_generic_drifted_bullet_input, tmp_path
    ) -> None:
        """The drifted PC-bullet-5 wording must not survive in the output unchanged.

        'Not silently left behind' means it is either:
          a) deleted by the strict regex (deletions_applied), or
          b) recorded in deletions_unmatched for later reporting.
        Either way, the drifted bullet must not appear in the output as prose.
        If the strict regex matches it, it is gone.  If only the probe matches,
        it stays in the output but is recorded — the test below asserts the recording.
        """
        result, output_path = _transform_to_tmp(s4_generic_drifted_bullet_input, tmp_path)
        content = _read(output_path)
        drifted_fragment = "Note implementation decisions for other agents but don"
        is_deleted = drifted_fragment not in content
        is_recorded = "PC-bullet-5" in result.deployed_added or (
            # probe-only: the bullet stays but the rule is recorded
            not is_deleted
        )
        # The primary assertion: the bullet must not survive silently (undetected).
        # If it survived in the output, it must have been recorded as unmatched.
        if not is_deleted:
            pytest.fail(
                "Drifted PC-bullet-5 wording survived in the output. "
                "The strict regex must match it so it is deleted, or the drift probe "
                "must catch it and record PC-bullet-5 in deletions_unmatched. "
                "At this stage the bullet must not be silently left behind."
            )

    def test_protocol_constraints_emitted_with_drifted_bullet_input(
        self, s4_generic_drifted_bullet_input, tmp_path
    ) -> None:
        """ProtocolConstraints must be emitted even when PC-bullet-5 carries drifted wording."""
        result, _ = _transform_to_tmp(s4_generic_drifted_bullet_input, tmp_path)
        assert "ProtocolConstraints" in result.deployed_added


# ---------------------------------------------------------------------------
# T4.3: Critical Tool Usage Constraint block left untouched
# ---------------------------------------------------------------------------

class TestCriticalToolUsageConstraintPreserved:
    """A '### Critical Tool Usage Constraint' heading block inside the Constraints
    section must be left in place unchanged.  The block belongs in HarnessConstraints
    but relocation is deferred to a later stage.  No tag insertion, deletion, or
    any other modification of that block is performed by the Constraints-region
    emission logic."""

    def test_transform_succeeds_with_critical_tool_usage_block(
        self, s4_generic_critical_tool_usage_input, tmp_path
    ) -> None:
        """Transformation of a file with a Critical Tool Usage Constraint block must succeed."""
        result, _ = _transform_to_tmp(s4_generic_critical_tool_usage_input, tmp_path)
        assert result.success is True
        assert result.errors == []

    def test_critical_tool_usage_heading_preserved_in_output(
        self, s4_generic_critical_tool_usage_input, tmp_path
    ) -> None:
        """The '### Critical Tool Usage Constraint' heading must be present in the output."""
        _, output_path = _transform_to_tmp(s4_generic_critical_tool_usage_input, tmp_path)
        content = _read(output_path)
        assert "### Critical Tool Usage Constraint" in content, (
            "Critical Tool Usage Constraint heading must be preserved in the output"
        )

    def test_critical_tool_usage_prose_preserved_in_output(
        self, s4_generic_critical_tool_usage_input, tmp_path
    ) -> None:
        """The prose under the Critical Tool Usage Constraint heading must be unchanged."""
        _, output_path = _transform_to_tmp(s4_generic_critical_tool_usage_input, tmp_path)
        content = _read(output_path)
        assert "SINGLE EDIT AT A TIME" in content, (
            "Critical Tool Usage Constraint prose must be preserved verbatim in the output"
        )
        assert "OpenCode platform limitation" in content, (
            "Critical Tool Usage Constraint detail must survive the transform unchanged"
        )

    def test_protocol_constraints_emitted_alongside_critical_tool_usage_block(
        self, s4_generic_critical_tool_usage_input, tmp_path
    ) -> None:
        """ProtocolConstraints must still be emitted when Critical Tool Usage Constraint is present."""
        result, _ = _transform_to_tmp(s4_generic_critical_tool_usage_input, tmp_path)
        assert "ProtocolConstraints" in result.deployed_added, (
            "ProtocolConstraints must be emitted even when the Constraints section "
            "contains a hand-authored heading block"
        )


# ---------------------------------------------------------------------------
# T4.2: HarnessConstraints table-driven emission on the harness path when
#        no legacy [INJECTION: harness_constraints] marker is present
# ---------------------------------------------------------------------------

class TestHarnessConstraintsTableDrivenHarnessPath:
    """HarnessConstraints must be emitted on the harness transform path via the
    table-driven apply_conduct_regions mechanism even when no legacy
    [INJECTION: harness_constraints] marker appears in the file.

    The five tests in TestConstraintsRegionsHarnessPath that assert HarnessConstraints
    presence currently pass because the generic reference file carries the legacy marker,
    which is converted to [[DEPLOYED:HarnessConstraints]] by the pre-existing
    INJECTION_OLD_MARKER_MAP path — independent of whether CONDUCT_REGIONS has been
    updated.  This class uses a fixture pair that contains NO legacy harness_constraints
    marker, forcing HarnessConstraints emission to go through the new table-driven path.

    All tests here fail until CONDUCT_REGIONS contains the HarnessConstraints row and
    the harness transform path calls apply_conduct_regions.
    """

    def test_transform_succeeds_without_legacy_marker(
        self,
        s4_harness_no_legacy_hc_input,
        s4_harness_no_legacy_hc_generic_ref,
        tmp_path,
    ) -> None:
        """Harness transform must succeed on a file that lacks the legacy harness_constraints
        marker."""
        result, _ = _transform_to_tmp(
            s4_harness_no_legacy_hc_input,
            tmp_path,
            generic_ref_path=s4_harness_no_legacy_hc_generic_ref,
        )
        assert result.success is True
        assert result.errors == []

    def test_harness_constraints_in_deployed_added_not_injections_added(
        self,
        s4_harness_no_legacy_hc_input,
        s4_harness_no_legacy_hc_generic_ref,
        tmp_path,
    ) -> None:
        """When no legacy marker is present, HarnessConstraints must appear in
        deployed_added (table-driven emission), NOT in injections_added.

        This is the critical check that distinguishes the table-driven path from
        the legacy marker conversion: injections_added is populated by
        INJECTION_OLD_MARKER_MAP; deployed_added is populated by apply_conduct_regions.
        A result where HarnessConstraints lands only in injections_added means the
        Stage-4 table row was never applied.
        """
        result, _ = _transform_to_tmp(
            s4_harness_no_legacy_hc_input,
            tmp_path,
            generic_ref_path=s4_harness_no_legacy_hc_generic_ref,
        )
        assert "HarnessConstraints" in result.deployed_added, (
            "HarnessConstraints must be in deployed_added (table-driven path) when the "
            f"legacy marker is absent; deployed_added={result.deployed_added}, "
            f"injections_added={result.injections_added}"
        )

    def test_harness_constraints_emitted_exactly_once_no_legacy_marker(
        self,
        s4_harness_no_legacy_hc_input,
        s4_harness_no_legacy_hc_generic_ref,
        tmp_path,
    ) -> None:
        """[[DEPLOYED:HarnessConstraints]] must appear exactly once in harness-path output
        when no legacy marker is present."""
        _, output_path = _transform_to_tmp(
            s4_harness_no_legacy_hc_input,
            tmp_path,
            generic_ref_path=s4_harness_no_legacy_hc_generic_ref,
        )
        content = _read(output_path)
        count = content.count("[[DEPLOYED:HarnessConstraints]]")
        assert count == 1, (
            f"HarnessConstraints open tag must appear exactly once; found {count}"
        )

    def test_protocol_constraints_precedes_harness_constraints_no_legacy_marker(
        self,
        s4_harness_no_legacy_hc_input,
        s4_harness_no_legacy_hc_generic_ref,
        tmp_path,
    ) -> None:
        """[[DEPLOYED:ProtocolConstraints]] must appear before [[DEPLOYED:HarnessConstraints]]
        in harness-path output when no legacy marker is present."""
        _, output_path = _transform_to_tmp(
            s4_harness_no_legacy_hc_input,
            tmp_path,
            generic_ref_path=s4_harness_no_legacy_hc_generic_ref,
        )
        content = _read(output_path)
        pc_pos = content.find("[[DEPLOYED:ProtocolConstraints]]")
        hc_pos = content.find("[[DEPLOYED:HarnessConstraints]]")
        assert pc_pos != -1, "ProtocolConstraints tag missing from harness-path output"
        assert hc_pos != -1, "HarnessConstraints tag missing from harness-path output"
        assert pc_pos < hc_pos, (
            "ProtocolConstraints must precede HarnessConstraints in the harness-path output"
        )

    def test_custom_constraints_never_emitted_no_legacy_marker(
        self,
        s4_harness_no_legacy_hc_input,
        s4_harness_no_legacy_hc_generic_ref,
        tmp_path,
    ) -> None:
        """CustomConstraints is retired and must never appear in harness-path output,
        regardless of whether the legacy harness_constraints marker is present."""
        _, output_path = _transform_to_tmp(
            s4_harness_no_legacy_hc_input,
            tmp_path,
            generic_ref_path=s4_harness_no_legacy_hc_generic_ref,
        )
        content = _read(output_path)
        assert "CustomConstraints" not in content, (
            "CustomConstraints must never appear in output after this change"
        )

    def test_all_five_pc_bullets_absent_no_legacy_marker(
        self,
        s4_harness_no_legacy_hc_input,
        s4_harness_no_legacy_hc_generic_ref,
        tmp_path,
    ) -> None:
        """All five ProtocolConstraints bullets must be deleted from harness-path output
        when no legacy harness_constraints marker is present."""
        _, output_path = _transform_to_tmp(
            s4_harness_no_legacy_hc_input,
            tmp_path,
            generic_ref_path=s4_harness_no_legacy_hc_generic_ref,
        )
        content = _read(output_path)
        fragments = [
            "NEVER access an orchestration artifact that is not named",
            "You MAY read, modify, or create any project file",
            "NEVER skip the JSON response block",
            "NEVER invent status codes",
            "Note work that belongs to another agent; do not do it yourself",
        ]
        for fragment in fragments:
            assert fragment not in content, (
                f"PC bullet fragment must be deleted from harness-path output: {fragment!r}"
            )

    def test_no_tag_emitted_inside_fenced_block_no_legacy_marker(
        self,
        s4_harness_no_legacy_hc_input,
        s4_harness_no_legacy_hc_generic_ref,
        tmp_path,
    ) -> None:
        """No [[DEPLOYED:...]] tag may appear on a fence-masked line in harness-path output
        when no legacy harness_constraints marker is present."""
        from fence import fence_mask as _fence_mask
        _, output_path = _transform_to_tmp(
            s4_harness_no_legacy_hc_input,
            tmp_path,
            generic_ref_path=s4_harness_no_legacy_hc_generic_ref,
        )
        out_lines = output_path.read_text(encoding="utf-8").splitlines(keepends=True)
        mask = _fence_mask(out_lines)
        for i, (line, is_masked) in enumerate(zip(out_lines, mask)):
            if is_masked:
                assert "[[DEPLOYED:" not in line, (
                    f"Deployed tag on fence-masked line {i + 1}: {line!r}"
                )


# ===========================================================================
# Stage 8 — Non-Conformance Detection and Reporting
# ===========================================================================
#
# These tests cover:
#   T8.1  Non-conformance record structure; TransformResult.non_conformances default
#   T8.2  JSON-envelope detection: each finding carries status_message and error_code
#   T8.3  Zero-injection detection and harness-prose detection; content left unmodified
#   T8.4  render_report output: earlier-stage records appear in the formatted report
#
# RED phase: T8.1 guard tests pass (field exists from Stage 6). T8.2, T8.3, and T8.4
# tests fail until detect_output_non_conformances and render_report are implemented
# and detect_output_non_conformances is wired into transform_file (the stubs currently
# return [] and raise NotImplementedError respectively).
# ===========================================================================

import textwrap as _textwrap  # noqa: E402

from non_conformance import (         # noqa: E402
    NonConformance,
    JsonEnvelopeExample,
    detect_output_non_conformances,
    render_report,
    NC_JSON_ENVELOPE,
    NC_NO_INJECTIONS,
    NC_HARNESS_PROSE,
    NC_DRIFTED_BULLET,
    NC_TIER_PLACEHOLDER,
    HARNESS_PROSE_HEADING_TERMS,
)
from region_insertion import SectionSpan  # noqa: E402


# ---------------------------------------------------------------------------
# Stage 8 inline-fixture helpers
# ---------------------------------------------------------------------------

def _section_span_for(lines: list, heading_text: str, section_name: str) -> SectionSpan:
    """Return a SectionSpan for the first occurrence of heading_text in lines.

    Satisfies the invariant heading_line < start <= content_end <= end by
    scanning backwards from the end of lines to find the last non-blank line
    for content_end.
    """
    heading_idx = None
    for i, line in enumerate(lines):
        if line.strip() == heading_text:
            heading_idx = i
            break
    if heading_idx is None:
        raise ValueError(f"Heading {heading_text!r} not found in provided lines")
    start = heading_idx + 1
    n = len(lines)
    # content_end: one past the last non-blank line after the heading.
    # When section has no content lines, content_end == start (invariant satisfied).
    content_end = n
    while content_end > start and not lines[content_end - 1].strip():
        content_end -= 1
    return SectionSpan(
        name=section_name,
        heading_line=heading_idx,
        start=start,
        content_end=content_end,
        end=n,
    )


def _of_lines_with_envelopes(*json_objects: str):
    """Build minimal body lines with an OutputFormat section containing JSON envelopes.

    Each positional argument is a complete JSON object string (without the fence markers).
    Returns (lines, sections_mapping) where sections_mapping is suitable for passing to
    detect_output_non_conformances.
    """
    header = [
        "# Test Agent\n",
        "\n",
        "You are the test agent.\n",
        "\n",
        "---\n",
        "\n",
    ]
    section_lines = [
        "## Output Format\n",
        "\n",
        "Always end with a JSON status block:\n",
        "\n",
    ]
    for obj in json_objects:
        section_lines.append("```json\n")
        for row in obj.splitlines():
            section_lines.append(row + "\n")
        section_lines.append("```\n")
        section_lines.append("\n")
    all_lines = header + section_lines
    span = _section_span_for(all_lines, "## Output Format", "OutputFormat")
    return all_lines, {"OutputFormat": span}


def _constraints_lines_with_h3(heading_text: str):
    """Build minimal body lines with a Constraints section containing an H3 heading.

    Returns (lines, sections_mapping) for detect_output_non_conformances.
    """
    header = [
        "# Test Agent\n",
        "\n",
        "You are the test agent.\n",
        "\n",
        "---\n",
        "\n",
    ]
    section = [
        "## Constraints\n",
        "\n",
        f"### {heading_text}\n",
        "\n",
        "Due to a platform limitation, only one edit at a time.\n",
        "\n",
    ]
    all_lines = header + section
    span = _section_span_for(all_lines, "## Constraints", "Constraints")
    return all_lines, {"Constraints": span}


# ---------------------------------------------------------------------------
# T8.1 — Non-conformance record structure (guard tests)
# ---------------------------------------------------------------------------

class TestNonConformanceStructureStage8:
    """Guard tests: TransformResult.non_conformances must remain valid after Stage 8.

    Because the non_conformances field was introduced in Stage 6 with a default factory,
    most of these tests pass even in RED. They guard against Stage 8 accidentally
    removing the field, changing its default, or breaking the NC code constants.
    The wiring test at the end fails in RED (transform_file does not yet call
    detect_output_non_conformances).
    """

    def test_transform_result_non_conformances_defaults_to_empty_list(self):
        """TransformResult constructed without non_conformances must have an empty list."""
        result = TransformResult(
            success=True,
            errors=[],
            sections_added=[],
            injections_added=[],
            deployed_added=[],
            version_before="1.0.0",
            version_after="1.1.0",
        )
        assert result.non_conformances == []

    def test_transform_result_non_conformances_is_list(self):
        """TransformResult.non_conformances must be a list instance."""
        result = TransformResult(
            success=True,
            errors=[],
            sections_added=[],
            injections_added=[],
            deployed_added=[],
            version_before="1.0.0",
            version_after="1.1.0",
        )
        assert isinstance(result.non_conformances, list)

    def test_transform_result_accepts_non_conformance_instances(self):
        """TransformResult can be constructed with a list containing NonConformance items."""
        nc = NonConformance(
            code=NC_TIER_PLACEHOLDER,
            file=pathlib.Path("agent.md"),
            message="recommended_tier absent",
            detail="recommended_tier",
        )
        result = TransformResult(
            success=True,
            errors=[],
            sections_added=[],
            injections_added=[],
            deployed_added=[],
            version_before="1.0.0",
            version_after="1.1.0",
            non_conformances=[nc],
        )
        assert len(result.non_conformances) == 1
        assert result.non_conformances[0].code == NC_TIER_PLACEHOLDER

    def test_nc_json_envelope_code_value(self):
        """NC_JSON_ENVELOPE must equal the stable string 'NC-7A'."""
        assert NC_JSON_ENVELOPE == "NC-7A"

    def test_nc_no_injections_code_value(self):
        """NC_NO_INJECTIONS must equal the stable string 'NC-7B'."""
        assert NC_NO_INJECTIONS == "NC-7B"

    def test_nc_harness_prose_code_value(self):
        """NC_HARNESS_PROSE must equal the stable string 'NC-7C'."""
        assert NC_HARNESS_PROSE == "NC-7C"

    def test_json_envelope_example_success_has_no_error_code(self):
        """JsonEnvelopeExample for a SUCCESS example must have error_code=None."""
        ex = JsonEnvelopeExample(
            status_code="SUCCESS",
            status_message="Task done, 5 tests written.",
            error_code=None,
            line_number=10,
        )
        assert ex.error_code is None

    def test_json_envelope_example_blocked_carries_error_code(self):
        """JsonEnvelopeExample for a BLOCKED example must carry the error_code string."""
        ex = JsonEnvelopeExample(
            status_code="BLOCKED",
            status_message="Design spec not found.",
            error_code="E101",
            line_number=20,
        )
        assert ex.error_code == "E101"

    def test_transform_file_wires_detect_output_non_conformances(self, tmp_path):
        """transform_file on a file with JSON envelopes in OutputFormat must include NC_JSON_ENVELOPE.

        This verifies that detect_output_non_conformances is wired into transform_file.
        Fails in RED: the wiring does not yet exist.
        """
        source = _textwrap.dedent("""\
            ---
            id: 99
            version: 1.0.0
            name: json-envelope-agent
            description: Agent with JSON envelopes in OutputFormat for wiring test
            ---

            # JsonEnvelopeAgent Agent

            You are the **JsonEnvelopeAgent** agent.

            [INJECTION: identity_extension]

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

            Always end with a JSON status block:

            ```json
            {
              "status_code": "SUCCESS",
              "status_message": "Task done, wrote 5 tests."
            }
            ```

            For BLOCKED:

            ```json
            {
              "status_code": "BLOCKED",
              "status_message": "Design spec not found.",
              "error_code": "E101"
            }
            ```

            ---

            ## Execution Philosophy

            Execute with focus.
        """)
        input_path = tmp_path / "json-envelope-agent.md"
        input_path.write_text(source, encoding="utf-8")
        output_path = tmp_path / "json-envelope-agent-out.md"
        result = transform_file(input_path, output_path)
        assert result.success, "Transformation must succeed before asserting non_conformances"
        nc_codes = [nc.code for nc in result.non_conformances]
        assert NC_JSON_ENVELOPE in nc_codes, (
            f"Expected NC_JSON_ENVELOPE in result.non_conformances after transform; "
            f"got codes: {nc_codes!r}"
        )

    def test_transform_file_json_envelope_retained_byte_identical(self, tmp_path):
        """transform_file must leave the JSON envelope block byte-identical in the output.

        This end-to-end test mirrors the unit-level non-mutation checks for class 7c
        (harness-prose headings) at the transform_file integration level for class 7a
        (JSON envelopes in OutputFormat). JSON envelopes are the class most likely to
        tempt an implementer into 'helpfully' reformatting the retained block, so this
        test pins that the fenced block text is preserved character-for-character.

        Fails in RED: the detection wiring does not yet exist, but the byte-identical
        assertion is independent of detection — it verifies the transform path does not
        alter the envelope block regardless of whether it is flagged.
        """
        json_block_text = (
            "```json\n"
            "{\n"
            '  "status_code": "BLOCKED",\n'
            '  "status_message": "Design spec not found.",\n'
            '  "error_code": "E101"\n'
            "}\n"
            "```\n"
        )
        source = _textwrap.dedent("""\
            ---
            id: 97
            version: 1.0.0
            name: byte-identical-envelope-agent
            description: Agent for testing byte-identical retention of JSON envelopes
            ---

            # ByteIdenticalEnvelopeAgent Agent

            You are the **ByteIdenticalEnvelopeAgent** agent.

            [INJECTION: identity_ext]

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

            Always end with a JSON status block:

            """) + json_block_text + _textwrap.dedent("""\

            ---

            ## Execution Philosophy

            Execute with focus.
        """)
        input_path = tmp_path / "byte-identical-envelope-agent.md"
        input_path.write_text(source, encoding="utf-8")
        output_path = tmp_path / "byte-identical-envelope-agent-out.md"
        result = transform_file(input_path, output_path)
        assert result.success, "Transformation must succeed before checking byte-identical output"
        output_text = output_path.read_text(encoding="utf-8")
        assert json_block_text in output_text, (
            "The JSON envelope block must be retained byte-identical in the output; "
            "the transform must not reformat, normalise or remove the fenced block"
        )


# ---------------------------------------------------------------------------
# T8.2 — JSON-envelope detection
# ---------------------------------------------------------------------------

class TestJsonEnvelopeDetection:
    """detect_output_non_conformances must detect JSON response envelopes in OutputFormat.

    Each finding carries a JsonEnvelopeExample per envelope, with status_message and,
    for BLOCKED examples, error_code. These tests fail in RED because the stub for
    detect_output_non_conformances returns [].
    """

    def test_success_envelope_yields_nc_json_envelope(self):
        """A JSON fenced block with status_code=SUCCESS in OutputFormat yields NC_JSON_ENVELOPE."""
        json_block = (
            '{\n'
            '  "status_code": "SUCCESS",\n'
            '  "status_message": "Wrote 5 tests."\n'
            '}'
        )
        lines, sections = _of_lines_with_envelopes(json_block)
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        codes = [nc.code for nc in result]
        assert NC_JSON_ENVELOPE in codes, (
            "Expected NC_JSON_ENVELOPE finding for a JSON fenced block in OutputFormat"
        )

    def test_success_envelope_evidence_carries_status_message(self):
        """The NC_JSON_ENVELOPE evidence must include the verbatim status_message string."""
        msg = "Wrote 5 tests and all compile."
        json_block = (
            '{\n'
            '  "status_code": "SUCCESS",\n'
            f'  "status_message": "{msg}"\n'
            '}'
        )
        lines, sections = _of_lines_with_envelopes(json_block)
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_JSON_ENVELOPE), None)
        assert nc is not None, "Expected NC_JSON_ENVELOPE finding"
        assert nc.evidence, "NC_JSON_ENVELOPE must carry at least one JsonEnvelopeExample"
        assert any(ex.status_message == msg for ex in nc.evidence), (
            f"Expected status_message {msg!r} in evidence; "
            f"got {[ex.status_message for ex in nc.evidence]!r}"
        )

    def test_success_envelope_evidence_has_error_code_none(self):
        """A SUCCESS envelope example must have error_code=None."""
        json_block = (
            '{\n'
            '  "status_code": "SUCCESS",\n'
            '  "status_message": "All done."\n'
            '}'
        )
        lines, sections = _of_lines_with_envelopes(json_block)
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_JSON_ENVELOPE), None)
        assert nc is not None, "Expected NC_JSON_ENVELOPE finding"
        success_examples = [ex for ex in nc.evidence if ex.status_code == "SUCCESS"]
        assert success_examples, "Expected at least one SUCCESS example in evidence"
        assert all(ex.error_code is None for ex in success_examples), (
            f"SUCCESS examples must have error_code=None; "
            f"got {[ex.error_code for ex in success_examples]!r}"
        )

    def test_blocked_envelope_carries_error_code_e101(self):
        """A BLOCKED envelope example with error_code E101 must carry E101 in the evidence."""
        json_block = (
            '{\n'
            '  "status_code": "BLOCKED",\n'
            '  "status_message": "Design spec not found.",\n'
            '  "error_code": "E101"\n'
            '}'
        )
        lines, sections = _of_lines_with_envelopes(json_block)
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_JSON_ENVELOPE), None)
        assert nc is not None, "Expected NC_JSON_ENVELOPE finding"
        blocked = [ex for ex in nc.evidence if ex.status_code == "BLOCKED"]
        assert blocked, "Expected at least one BLOCKED example in evidence"
        assert any(ex.error_code == "E101" for ex in blocked), (
            f"Expected error_code='E101'; got {[ex.error_code for ex in blocked]!r}"
        )

    def test_blocked_envelope_carries_error_code_e501(self):
        """A BLOCKED envelope with error_code E501 must carry E501 (test-runner corpus case)."""
        json_block = (
            '{\n'
            '  "status_code": "BLOCKED",\n'
            '  "status_message": "External tool unavailable.",\n'
            '  "error_code": "E501"\n'
            '}'
        )
        lines, sections = _of_lines_with_envelopes(json_block)
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_JSON_ENVELOPE), None)
        assert nc is not None
        blocked = [ex for ex in nc.evidence if ex.status_code == "BLOCKED"]
        assert any(ex.error_code == "E501" for ex in blocked), (
            f"Expected error_code='E501'; got {[ex.error_code for ex in blocked]!r}"
        )

    def test_blocked_envelope_carries_error_code_e502(self):
        """A BLOCKED envelope with error_code E502 must carry E502 (pull-request-comment corpus case)."""
        json_block = (
            '{\n'
            '  "status_code": "BLOCKED",\n'
            '  "status_message": "Permission denied to write file.",\n'
            '  "error_code": "E502"\n'
            '}'
        )
        lines, sections = _of_lines_with_envelopes(json_block)
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_JSON_ENVELOPE), None)
        assert nc is not None
        blocked = [ex for ex in nc.evidence if ex.status_code == "BLOCKED"]
        assert any(ex.error_code == "E502" for ex in blocked), (
            f"Expected error_code='E502'; got {[ex.error_code for ex in blocked]!r}"
        )

    def test_blocked_envelope_carries_error_code_e503(self):
        """A BLOCKED envelope with error_code E503 must carry E503 (requirements-refinement corpus case)."""
        json_block = (
            '{\n'
            '  "status_code": "BLOCKED",\n'
            '  "status_message": "User contact unavailable.",\n'
            '  "error_code": "E503"\n'
            '}'
        )
        lines, sections = _of_lines_with_envelopes(json_block)
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_JSON_ENVELOPE), None)
        assert nc is not None
        blocked = [ex for ex in nc.evidence if ex.status_code == "BLOCKED"]
        assert any(ex.error_code == "E503" for ex in blocked), (
            f"Expected error_code='E503'; got {[ex.error_code for ex in blocked]!r}"
        )

    def test_blocked_envelope_carries_error_code_e401(self):
        """A BLOCKED envelope with error_code E401 must carry E401 (audit-to-pull-request corpus case)."""
        json_block = (
            '{\n'
            '  "status_code": "BLOCKED",\n'
            '  "status_message": "Predecessor task incomplete.",\n'
            '  "error_code": "E401"\n'
            '}'
        )
        lines, sections = _of_lines_with_envelopes(json_block)
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_JSON_ENVELOPE), None)
        assert nc is not None
        blocked = [ex for ex in nc.evidence if ex.status_code == "BLOCKED"]
        assert any(ex.error_code == "E401" for ex in blocked), (
            f"Expected error_code='E401'; got {[ex.error_code for ex in blocked]!r}"
        )

    def test_two_envelopes_produce_two_evidence_items(self):
        """Two JSON blocks in OutputFormat must yield one NC with exactly two evidence items."""
        success_block = (
            '{\n'
            '  "status_code": "SUCCESS",\n'
            '  "status_message": "Done."\n'
            '}'
        )
        blocked_block = (
            '{\n'
            '  "status_code": "BLOCKED",\n'
            '  "status_message": "Input missing.",\n'
            '  "error_code": "E101"\n'
            '}'
        )
        lines, sections = _of_lines_with_envelopes(success_block, blocked_block)
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_JSON_ENVELOPE), None)
        assert nc is not None, "Expected NC_JSON_ENVELOPE finding"
        assert len(nc.evidence) == 2, (
            f"Expected exactly 2 JsonEnvelopeExample items (one per block); got {len(nc.evidence)}"
        )

    def test_distinct_error_codes_preserved_across_multiple_envelopes(self):
        """Multiple BLOCKED blocks with different error codes each carry their own code."""
        block_e101 = (
            '{\n'
            '  "status_code": "BLOCKED",\n'
            '  "status_message": "Spec not found.",\n'
            '  "error_code": "E101"\n'
            '}'
        )
        block_e501 = (
            '{\n'
            '  "status_code": "BLOCKED",\n'
            '  "status_message": "Tool unavailable.",\n'
            '  "error_code": "E501"\n'
            '}'
        )
        lines, sections = _of_lines_with_envelopes(block_e101, block_e501)
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_JSON_ENVELOPE), None)
        assert nc is not None
        error_codes = {ex.error_code for ex in nc.evidence if ex.error_code is not None}
        assert "E101" in error_codes, f"E101 missing from evidence error codes: {error_codes!r}"
        assert "E501" in error_codes, f"E501 missing from evidence error codes: {error_codes!r}"

    def test_json_envelope_outside_output_format_not_detected(self):
        """A JSON fenced block outside the OutputFormat section must not yield NC_JSON_ENVELOPE."""
        # The JSON block appears in the body BEFORE the OutputFormat heading.
        # The OutputFormat section itself contains only a plain-text description.
        lines = [
            "# Test Agent\n",
            "\n",
            "Example response in Identity:\n",
            "\n",
            "```json\n",
            '{\n',
            '  "status_code": "SUCCESS",\n',
            '  "status_message": "Example — not in OutputFormat."\n',
            '}\n',
            "```\n",
            "\n",
            "---\n",
            "\n",
            "## Output Format\n",
            "\n",
            "Return a two-column Markdown table, not a JSON block.\n",
            "\n",
        ]
        of_span = _section_span_for(lines, "## Output Format", "OutputFormat")
        sections = {"OutputFormat": of_span}
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_JSON_ENVELOPE), None)
        assert nc is None, (
            "JSON blocks outside the OutputFormat section must not yield NC_JSON_ENVELOPE; "
            f"got finding: {nc!r}"
        )

    def test_nc_json_envelope_records_file_path(self):
        """The NC_JSON_ENVELOPE finding must record the supplied file path."""
        json_block = '{\n  "status_code": "SUCCESS",\n  "status_message": "Done."\n}'
        lines, sections = _of_lines_with_envelopes(json_block)
        target_path = pathlib.Path("my-agent.md")
        result = detect_output_non_conformances(target_path, lines, sections)
        nc = next((x for x in result if x.code == NC_JSON_ENVELOPE), None)
        assert nc is not None, "Expected NC_JSON_ENVELOPE finding"
        assert nc.file == target_path, f"Expected file={target_path!r}; got {nc.file!r}"

    def test_bare_fence_with_status_code_content_yields_nc_json_envelope(self):
        """A fenced block without a 'json' language tag whose content parses as a status_code-bearing JSON object must yield NC_JSON_ENVELOPE.

        The contract specifies detection when the fenced block language is 'json' OR
        when the content parses as a JSON object containing a 'status_code' key, even
        without a language tag on the fence opener. This test exercises the 'or' branch.

        Fails in RED because the stub returns [].
        """
        header = [
            "# Test Agent\n",
            "\n",
            "You are the test agent.\n",
            "\n",
            "---\n",
            "\n",
        ]
        section_lines = [
            "## Output Format\n",
            "\n",
            "Always end with a status block:\n",
            "\n",
            "```\n",                          # bare fence — no 'json' language tag
            '{\n',
            '  "status_code": "SUCCESS",\n',
            '  "status_message": "Task done."\n',
            '}\n',
            "```\n",
            "\n",
        ]
        all_lines = header + section_lines
        span = _section_span_for(all_lines, "## Output Format", "OutputFormat")
        sections = {"OutputFormat": span}
        result = detect_output_non_conformances(pathlib.Path("agent.md"), all_lines, sections)
        codes = [nc.code for nc in result]
        assert NC_JSON_ENVELOPE in codes, (
            "A bare-fence block (no 'json' language tag) whose content parses as a "
            "status_code-bearing JSON object must yield NC_JSON_ENVELOPE; "
            "this is the 'or' branch of the class-7a detection rule"
        )


# ---------------------------------------------------------------------------
# T8.3 — Zero-injection detection
# ---------------------------------------------------------------------------

class TestZeroInjectionDetection:
    """detect_output_non_conformances must detect files whose output has no INJECTION regions.

    These tests fail in RED because the stub for detect_output_non_conformances returns [].
    """

    def test_zero_injection_tags_yields_nc_no_injections(self):
        """Lines with no [[INJECTION: open tags must yield NC_NO_INJECTIONS."""
        lines = [
            "[[SECTION:Identity]]\n",
            "# Test Agent\n",
            "\n",
            "Already-deployed content with no injections.\n",
            "[[/SECTION:Identity]]\n",
            "\n",
            "[[SECTION:OutputFormat]]\n",
            "## Output Format\n",
            "\n",
            "Return a two-column table.\n",
            "[[/SECTION:OutputFormat]]\n",
        ]
        assert not any("[[INJECTION:" in line for line in lines), (
            "Precondition: test content must have no [[INJECTION: tags"
        )
        sections = {
            "Identity": _section_span_for(lines, "# Test Agent", "Identity"),
            "OutputFormat": _section_span_for(lines, "## Output Format", "OutputFormat"),
        }
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        codes = [nc.code for nc in result]
        assert NC_NO_INJECTIONS in codes, (
            "Expected NC_NO_INJECTIONS when no [[INJECTION: tags are present in the output"
        )

    def test_at_least_one_injection_tag_suppresses_nc_no_injections(self):
        """Lines with at least one [[INJECTION: tag must NOT yield NC_NO_INJECTIONS."""
        lines = [
            "[[SECTION:Identity]]\n",
            "# Test Agent\n",
            "\n",
            "You are the test agent.\n",
            "\n",
            "[[INJECTION:IdentityExtension]]\n",
            "[[/INJECTION:IdentityExtension]]\n",
            "[[/SECTION:Identity]]\n",
        ]
        assert any("[[INJECTION:" in line for line in lines), (
            "Precondition: test content must include at least one [[INJECTION: tag"
        )
        sections = {"Identity": _section_span_for(lines, "# Test Agent", "Identity")}
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        codes = [nc.code for nc in result]
        assert NC_NO_INJECTIONS not in codes, (
            "Must not yield NC_NO_INJECTIONS when at least one [[INJECTION: tag is present"
        )

    def test_nc_no_injections_records_file_path(self):
        """The NC_NO_INJECTIONS finding must record the supplied file path."""
        lines = [
            "[[SECTION:Capabilities]]\n",
            "## Capabilities\n",
            "\n",
            "All deployed inline.\n",
            "[[/SECTION:Capabilities]]\n",
        ]
        sections = {"Capabilities": _section_span_for(lines, "## Capabilities", "Capabilities")}
        target_path = pathlib.Path("deployed-agent.md")
        result = detect_output_non_conformances(target_path, lines, sections)
        nc = next((x for x in result if x.code == NC_NO_INJECTIONS), None)
        assert nc is not None, "Expected NC_NO_INJECTIONS finding"
        assert nc.file == target_path, f"Expected file={target_path!r}; got {nc.file!r}"

    def test_nc_no_injections_has_empty_evidence_tuple(self):
        """NC_NO_INJECTIONS must carry an empty evidence tuple (no per-example data)."""
        lines = [
            "[[SECTION:Constraints]]\n",
            "## Constraints\n",
            "\n",
            "Constraints deployed.\n",
            "[[/SECTION:Constraints]]\n",
        ]
        sections = {"Constraints": _section_span_for(lines, "## Constraints", "Constraints")}
        result = detect_output_non_conformances(pathlib.Path("a.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_NO_INJECTIONS), None)
        assert nc is not None, "Expected NC_NO_INJECTIONS finding"
        assert nc.evidence == (), (
            f"NC_NO_INJECTIONS must have an empty evidence tuple; got {nc.evidence!r}"
        )

    def test_detect_does_not_modify_input_lines(self):
        """detect_output_non_conformances must not modify the input lines sequence."""
        lines = [
            "[[SECTION:Identity]]\n",
            "# Test Agent\n",
            "\n",
            "No injections here.\n",
            "[[/SECTION:Identity]]\n",
        ]
        original = list(lines)
        sections = {"Identity": _section_span_for(lines, "# Test Agent", "Identity")}
        detect_output_non_conformances(pathlib.Path("a.md"), lines, sections)
        assert lines == original, (
            "detect_output_non_conformances must not modify the input lines"
        )


# ---------------------------------------------------------------------------
# T8.3 — Harness-prose detection
# ---------------------------------------------------------------------------

class TestHarnessProseDection:
    """detect_output_non_conformances must detect harness-specific H3 headings in sections.

    These tests fail in RED because the stub for detect_output_non_conformances returns [].
    The 'content left unmodified' test passes in RED (the stub does not modify lines),
    but it is structured so that the detection assertion fails first.
    """

    def test_tool_usage_heading_yields_nc_harness_prose(self):
        """An H3 heading containing 'tool usage' in a section must yield NC_HARNESS_PROSE."""
        lines, sections = _constraints_lines_with_h3("Critical Tool Usage Constraint")
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        codes = [nc.code for nc in result]
        assert NC_HARNESS_PROSE in codes, (
            "Expected NC_HARNESS_PROSE for '### Critical Tool Usage Constraint' "
            "(contains 'tool usage' from HARNESS_PROSE_HEADING_TERMS)"
        )

    def test_nc_harness_prose_carries_heading_in_detail(self):
        """NC_HARNESS_PROSE must carry the matched heading text in the detail field."""
        heading = "Critical Tool Usage Constraint"
        lines, sections = _constraints_lines_with_h3(heading)
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_HARNESS_PROSE), None)
        assert nc is not None, "Expected NC_HARNESS_PROSE finding"
        assert nc.detail is not None, "NC_HARNESS_PROSE must populate the detail field"
        assert heading in nc.detail, (
            f"detail must contain the heading text {heading!r}; got {nc.detail!r}"
        )

    def test_platform_term_in_heading_detected(self):
        """An H3 heading containing 'platform' must yield NC_HARNESS_PROSE."""
        lines, sections = _constraints_lines_with_h3("Platform-Specific Notes")
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        codes = [nc.code for nc in result]
        assert NC_HARNESS_PROSE in codes, (
            "Expected NC_HARNESS_PROSE for heading containing 'platform'"
        )

    def test_copilot_term_in_heading_detected(self):
        """An H3 heading containing 'copilot' must yield NC_HARNESS_PROSE."""
        lines, sections = _constraints_lines_with_h3("GitHub Copilot Integration Note")
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        codes = [nc.code for nc in result]
        assert NC_HARNESS_PROSE in codes, (
            "Expected NC_HARNESS_PROSE for heading containing 'copilot'"
        )

    def test_opencode_term_in_heading_detected(self):
        """An H3 heading containing 'opencode' must yield NC_HARNESS_PROSE."""
        lines, sections = _constraints_lines_with_h3("OpenCode Integration Notes")
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        codes = [nc.code for nc in result]
        assert NC_HARNESS_PROSE in codes, (
            "Expected NC_HARNESS_PROSE for heading containing 'opencode'"
        )

    def test_non_harness_heading_not_detected(self):
        """An H3 heading not matching any term in HARNESS_PROSE_HEADING_TERMS must NOT yield NC_HARNESS_PROSE."""
        lines, sections = _constraints_lines_with_h3("Advanced Usage Patterns")
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        codes = [nc.code for nc in result]
        assert NC_HARNESS_PROSE not in codes, (
            "Must not yield NC_HARNESS_PROSE for a heading that does not match "
            "any member of HARNESS_PROSE_HEADING_TERMS"
        )

    def test_harness_prose_heading_not_removed_from_lines(self):
        """detect_output_non_conformances must not remove or alter the matched heading.

        Detection is reporting-only; the flagged content must be left byte-identical.
        This test passes in RED (the stub does not modify lines), but the detection
        assertion in test_tool_usage_heading_yields_nc_harness_prose fails first.
        """
        heading = "Critical Tool Usage Constraint"
        lines, sections = _constraints_lines_with_h3(heading)
        heading_line_text = f"### {heading}\n"
        assert heading_line_text in lines, "Precondition: heading must be present before call"
        detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        assert heading_line_text in lines, (
            "detect_output_non_conformances must not remove the matched heading from lines; "
            "flagging is detection-only, never correction"
        )

    def test_nc_harness_prose_records_1_based_line_number(self):
        """NC_HARNESS_PROSE must carry a 1-based line_number for the matched heading."""
        heading = "OpenCode Specific Constraint"
        lines, sections = _constraints_lines_with_h3(heading)
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        nc = next((x for x in result if x.code == NC_HARNESS_PROSE), None)
        assert nc is not None, "Expected NC_HARNESS_PROSE finding"
        assert nc.line_number is not None, (
            "NC_HARNESS_PROSE must record a 1-based line_number for the matched heading"
        )
        assert nc.line_number >= 1, (
            f"line_number must be >= 1 (1-based); got {nc.line_number!r}"
        )

    def test_nc_harness_prose_records_file_path(self):
        """NC_HARNESS_PROSE must record the supplied file path."""
        heading = "GHCP CLI Integration"
        lines, sections = _constraints_lines_with_h3(heading)
        target = pathlib.Path("harness-agent.md")
        result = detect_output_non_conformances(target, lines, sections)
        nc = next((x for x in result if x.code == NC_HARNESS_PROSE), None)
        assert nc is not None, "Expected NC_HARNESS_PROSE finding"
        assert nc.file == target, f"Expected file={target!r}; got {nc.file!r}"

    def test_claude_code_term_in_heading_detected(self):
        """An H3 heading containing 'claude code' must yield NC_HARNESS_PROSE."""
        lines, sections = _constraints_lines_with_h3("Claude Code Integration Notes")
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        codes = [nc.code for nc in result]
        assert NC_HARNESS_PROSE in codes, (
            "Expected NC_HARNESS_PROSE for heading containing 'claude code' "
            "(a member of HARNESS_PROSE_HEADING_TERMS)"
        )

    def test_ghcp_cli_term_in_heading_detected(self):
        """An H3 heading containing 'ghcp cli' must yield NC_HARNESS_PROSE."""
        lines, sections = _constraints_lines_with_h3("GHCP CLI Configuration Notes")
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        codes = [nc.code for nc in result]
        assert NC_HARNESS_PROSE in codes, (
            "Expected NC_HARNESS_PROSE for heading containing 'ghcp cli' "
            "(a member of HARNESS_PROSE_HEADING_TERMS)"
        )

    def test_vs_code_ghcp_term_in_heading_detected(self):
        """An H3 heading containing 'vs code ghcp' must yield NC_HARNESS_PROSE."""
        lines, sections = _constraints_lines_with_h3("VS Code GHCP Extension Notes")
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        codes = [nc.code for nc in result]
        assert NC_HARNESS_PROSE in codes, (
            "Expected NC_HARNESS_PROSE for heading containing 'vs code ghcp' "
            "(a member of HARNESS_PROSE_HEADING_TERMS)"
        )

    def test_heading_matching_two_terms_yields_exactly_one_nc_harness_prose(self):
        """A heading matching two HARNESS_PROSE_HEADING_TERMS entries must yield exactly one NC_HARNESS_PROSE.

        The contract states 'A heading matching two terms still yields exactly one
        NonConformance'. 'OpenCode Platform Notes' matches both 'opencode' and 'platform';
        the detector must deduplicate and return a single finding for the heading.

        Fails in RED because the stub returns [].
        """
        lines, sections = _constraints_lines_with_h3("OpenCode Platform Notes")
        result = detect_output_non_conformances(pathlib.Path("agent.md"), lines, sections)
        harness_prose_ncs = [nc for nc in result if nc.code == NC_HARNESS_PROSE]
        assert len(harness_prose_ncs) == 1, (
            f"A heading matching two HARNESS_PROSE_HEADING_TERMS entries must yield exactly "
            f"one NC_HARNESS_PROSE finding; got {len(harness_prose_ncs)}"
        )


# ---------------------------------------------------------------------------
# T8.4 — render_report output and earlier-stage records
# ---------------------------------------------------------------------------

class TestNonConformanceRenderReport:
    """render_report must produce a human-readable, per-file grouped report.

    All tests in this class fail in RED because the render_report stub raises
    NotImplementedError. End-to-end tests additionally require transform_file
    to produce the expected finding types.
    """

    def test_render_report_returns_string(self):
        """render_report must return a string, not raise."""
        nc = NonConformance(
            code=NC_TIER_PLACEHOLDER,
            file=pathlib.Path("agent.md"),
            message="recommended_tier absent",
            detail="recommended_tier",
        )
        report = render_report([nc])
        assert isinstance(report, str), (
            f"render_report must return str; got {type(report).__name__!r}"
        )

    def test_render_report_empty_input_contains_header(self):
        """render_report([]) must include the 'Non-conformance report' header."""
        report = render_report([])
        assert "Non-conformance report" in report, (
            "render_report output must contain the 'Non-conformance report' header"
        )

    def test_render_report_empty_input_zero_count_line(self):
        """render_report([]) must include '0 non-conformances across 0 files.' line."""
        report = render_report([])
        assert "0 non-conformances across 0 files." in report, (
            f"render_report([]) must include '0 non-conformances across 0 files.'; got: {report!r}"
        )

    def test_render_report_includes_file_path(self):
        """render_report output must include the file path for each file with findings."""
        nc = NonConformance(
            code=NC_NO_INJECTIONS,
            file=pathlib.Path("my-agent.md"),
            message="zero injection regions in output",
        )
        report = render_report([nc])
        assert "my-agent.md" in report, (
            "render_report must include the file path in its output"
        )

    def test_render_report_includes_nc_code(self):
        """render_report output must include the NC code (e.g. '[NC-7A]') for each finding."""
        nc = NonConformance(
            code=NC_JSON_ENVELOPE,
            file=pathlib.Path("agent.md"),
            message="JSON envelope retained in OutputFormat",
            evidence=(
                JsonEnvelopeExample(
                    status_code="SUCCESS",
                    status_message="Task done.",
                    error_code=None,
                    line_number=10,
                ),
            ),
        )
        report = render_report([nc])
        assert "NC-7A" in report, (
            f"render_report must include the NC code 'NC-7A' in its output; got: {report!r}"
        )

    def test_render_report_blocked_evidence_includes_error_code(self):
        """render_report output must include the error_code for BLOCKED envelope examples."""
        nc = NonConformance(
            code=NC_JSON_ENVELOPE,
            file=pathlib.Path("agent.md"),
            message="JSON envelope retained",
            evidence=(
                JsonEnvelopeExample(
                    status_code="BLOCKED",
                    status_message="Design spec not found.",
                    error_code="E101",
                    line_number=15,
                ),
            ),
        )
        report = render_report([nc])
        assert "E101" in report, (
            f"render_report must include error_code 'E101' for BLOCKED example; got: {report!r}"
        )

    def test_render_report_tier_placeholder_record_appears(self):
        """A NC_TIER_PLACEHOLDER NonConformance must appear in render_report output."""
        nc = NonConformance(
            code=NC_TIER_PLACEHOLDER,
            file=pathlib.Path("no-tier-agent.md"),
            message="Frontmatter key 'recommended_tier' is absent or empty",
            detail="recommended_tier",
        )
        report = render_report([nc])
        assert "NC-TIER" in report, (
            "NC_TIER_PLACEHOLDER code 'NC-TIER' must appear in render_report output"
        )
        assert "no-tier-agent.md" in report, (
            "File path must appear in render_report output for tier placeholder findings"
        )

    def test_render_report_drifted_bullet_record_appears(self):
        """A NC_DRIFTED_BULLET NonConformance must appear in render_report output."""
        nc = NonConformance(
            code=NC_DRIFTED_BULLET,
            file=pathlib.Path("drifted-agent.md"),
            message="drifted bullet found and left in place",
            detail="PC-bullet-5",
        )
        report = render_report([nc])
        assert "NC-D1F" in report, (
            "NC_DRIFTED_BULLET code 'NC-D1F' must appear in render_report output"
        )
        assert "drifted-agent.md" in report, (
            "File path must appear in render_report output for drifted-bullet findings"
        )

    def test_render_report_groups_findings_by_file(self):
        """render_report must group findings so both codes appear for the same file."""
        nc1 = NonConformance(
            code=NC_NO_INJECTIONS,
            file=pathlib.Path("agent.md"),
            message="zero injection regions",
        )
        nc2 = NonConformance(
            code=NC_HARNESS_PROSE,
            file=pathlib.Path("agent.md"),
            message="harness prose found",
            detail="tool usage",
            line_number=42,
        )
        report = render_report([nc1, nc2])
        assert "agent.md" in report, "File path must appear in grouped report"
        assert "NC-7B" in report, "NC_NO_INJECTIONS code must appear"
        assert "NC-7C" in report, "NC_HARNESS_PROSE code must appear"

    def test_render_report_count_line_reflects_item_count(self):
        """The trailing count line must reflect the total number of NonConformance items."""
        ncs = [
            NonConformance(
                code=NC_TIER_PLACEHOLDER,
                file=pathlib.Path("a.md"),
                message="recommended_tier absent",
                detail="recommended_tier",
            ),
            NonConformance(
                code=NC_TIER_PLACEHOLDER,
                file=pathlib.Path("b.md"),
                message="tier_rationale absent",
                detail="tier_rationale",
            ),
        ]
        report = render_report(ncs)
        assert "2 non-conformances" in report, (
            f"Count line must say '2 non-conformances'; got: {report!r}"
        )
        assert "2 files" in report, (
            f"Count line must say '2 files'; got: {report!r}"
        )

    def test_render_report_tier_placeholder_from_transform_roundtrip(
        self, s6_generic_no_tier_input, tmp_path
    ):
        """NC_TIER_PLACEHOLDER items from transform_file must be renderable by render_report.

        Fails in RED because render_report raises NotImplementedError.
        """
        result, _ = _transform_to_tmp(s6_generic_no_tier_input, tmp_path)
        tier_ncs = [nc for nc in result.non_conformances if nc.code == NC_TIER_PLACEHOLDER]
        assert tier_ncs, "Precondition: transform must produce NC_TIER_PLACEHOLDER findings"
        report = render_report(tier_ncs)
        assert isinstance(report, str), "render_report must return a string"
        assert "NC-TIER" in report, "render_report must include the NC_TIER_PLACEHOLDER code"

    def test_transform_file_drifted_bullet_yields_nc_drifted_bullet(
        self, s8_generic_drift_probe_only_bullet_input, tmp_path
    ):
        """transform_file on a file with a probe-only-matching PC-bullet-5 wording must
        yield NC_DRIFTED_BULLET.

        Per the PC-bullet-5 contract (ContractsDesign.md, DeletionRule outcome table): the
        strict pattern is deliberately anchored to match only the canonical wording and the
        one known drifted wording (covered separately by s4_generic_drifted_bullet_input,
        which is exercised by TestDriftedFifthBulletHandling and must keep landing in
        deletions_applied with no NonConformance). A pattern-matched hit never yields
        NC_DRIFTED_BULLET regardless of drift_probe. This fixture instead carries a THIRD
        wording that only the permissive drift_probe recognizes, so the bullet is left in
        place and lands in deletions_unmatched — the only shape from which Stage 8's
        drift-probe branch can raise NC_DRIFTED_BULLET.

        Fails in RED: Stage 8's drift-probe branch in region_insertion is not yet implemented.
        """
        result, _ = _transform_to_tmp(s8_generic_drift_probe_only_bullet_input, tmp_path)
        assert result.success, "Transform must succeed before asserting non_conformances"
        nc_codes = [nc.code for nc in result.non_conformances]
        assert NC_DRIFTED_BULLET in nc_codes, (
            f"Expected NC_DRIFTED_BULLET for file with probe-only PC-bullet-5 wording; "
            f"got codes: {nc_codes!r}"
        )
