"""Tests for the format validator tool.

Tests verify validation behavior: given a boundary-tagged .md file, the
validator returns the correct ValidationError list (empty for valid files,
populated with the right error codes for invalid files). CLI tests verify
exit codes and output format.

All tests are in TDD RED phase: they will fail until boundary_validator.py
is implemented.
"""
from __future__ import annotations

import pathlib
import re
import shutil
import subprocess
import sys

import pytest

# Resolve the Tools directory so we can import from it regardless of where
# pytest is invoked from.
_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

import boundary_constants  # noqa: E402
from boundary_validator import ValidationError, validate_batch, validate_file  # noqa: E402

_FIXTURES_DIR = pathlib.Path(__file__).parent / "fixtures"

# Path to the validator script for CLI integration tests.
_VALIDATOR_SCRIPT = _TOOLS_DIR / "boundary_validator.py"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _run_cli(*args: str) -> subprocess.CompletedProcess[str]:
    """Run the validator CLI with the given arguments and capture output."""
    return subprocess.run(
        [sys.executable, str(_VALIDATOR_SCRIPT), *args],
        capture_output=True,
        text=True,
    )


def _fixture(name: str) -> pathlib.Path:
    """Return the path to a named fixture file."""
    return _FIXTURES_DIR / name


# ---------------------------------------------------------------------------
# Valid file cases — validate_file should return an empty list
# ---------------------------------------------------------------------------

class TestValidateFileValidCases:
    """validate_file returns no errors for well-formed boundaried files."""

    def test_valid_full_file_with_all_sections_and_injections_returns_no_errors(self) -> None:
        # Arrange
        fixture = _fixture("validator_valid_full.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert errors == [], (
            f"Expected no errors for a fully valid file but got: {errors}"
        )

    def test_valid_file_with_subset_of_injections_returns_no_errors(self) -> None:
        # Arrange
        # Not all 12 injections are required — only one is present here.
        fixture = _fixture("validator_valid_subset_injections.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert errors == [], (
            f"Expected no errors when only a subset of injections are present but got: {errors}"
        )

    def test_valid_file_with_empty_injection_boundaries_returns_no_errors(self) -> None:
        # Arrange
        # Injection boundaries with open tag immediately followed by close tag are valid.
        fixture = _fixture("validator_valid_empty_injections.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert errors == [], (
            f"Expected no errors for empty injection boundaries but got: {errors}"
        )


# ---------------------------------------------------------------------------
# Error detection — validate_file should return errors with specific codes
# ---------------------------------------------------------------------------

class TestValidateFileErrorDetection:
    """validate_file returns ValidationError instances with the correct error codes."""

    def test_missing_close_tag_returns_e001_error(self) -> None:
        # Arrange
        # SECTION:Identity opened but never closed.
        fixture = _fixture("validator_invalid_e001_missing_close.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for a missing close tag"
        codes = [e.error_code for e in errors]
        assert "E001" in codes, (
            f"Expected E001 (UNMATCHED_OPEN) but got codes: {codes}"
        )

    def test_missing_close_tag_error_has_valid_line_number(self) -> None:
        # Arrange
        fixture = _fixture("validator_invalid_e001_missing_close.md")

        # Act
        errors = validate_file(fixture)
        e001_errors = [e for e in errors if e.error_code == "E001"]

        # Assert
        assert e001_errors, "Expected at least one E001 error"
        for error in e001_errors:
            assert error.line_number > 0, (
                f"E001 error should have a positive line_number but got {error.line_number}"
            )

    def test_orphan_close_tag_returns_e002_error(self) -> None:
        # Arrange
        # [[/SECTION:Identity]] appears with no corresponding open tag.
        fixture = _fixture("validator_invalid_e002_orphan_close.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for an orphan close tag"
        codes = [e.error_code for e in errors]
        assert "E002" in codes, (
            f"Expected E002 (UNMATCHED_CLOSE) but got codes: {codes}"
        )

    def test_orphan_close_tag_error_line_points_to_close_tag_line(self) -> None:
        # Arrange
        fixture = _fixture("validator_invalid_e002_orphan_close.md")

        # Act
        errors = validate_file(fixture)
        e002_errors = [e for e in errors if e.error_code == "E002"]

        # Assert
        assert e002_errors, "Expected at least one E002 error"
        # The orphan close tag is on line 8 of the fixture (6 frontmatter lines + 1 blank + tag).
        # An exact assertion catches off-by-one errors in the implementation's line counter.
        for error in e002_errors:
            assert error.line_number == 8, (
                f"E002 error should point to line 8 (the orphan close tag) but got {error.line_number}"
            )

    def test_mismatched_open_close_names_returns_e003_error(self) -> None:
        # Arrange
        # [[SECTION:Identity]] is opened but [[/SECTION:CommunicationProtocol]] closes it.
        fixture = _fixture("validator_invalid_e003_name_mismatch.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for mismatched tag names"
        codes = [e.error_code for e in errors]
        assert "E003" in codes, (
            f"Expected E003 (NAME_MISMATCH) but got codes: {codes}"
        )

    def test_non_canonical_boundary_name_returns_e004_error(self) -> None:
        # Arrange
        # [[SECTION:CustomSection]] uses a name not in CANONICAL_SECTIONS.
        fixture = _fixture("validator_invalid_e004_non_canonical.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for a non-canonical name"
        codes = [e.error_code for e in errors]
        assert "E004" in codes, (
            f"Expected E004 (NON_CANONICAL_NAME) but got codes: {codes}"
        )

    def test_content_outside_boundary_returns_e005_error(self) -> None:
        # Arrange
        # A non-blank content line appears after frontmatter but outside any boundary.
        fixture = _fixture("validator_invalid_e005_content_outside.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for content outside boundary"
        codes = [e.error_code for e in errors]
        assert "E005" in codes, (
            f"Expected E005 (CONTENT_OUTSIDE_BOUNDARY) but got codes: {codes}"
        )

    def test_near_miss_tag_with_space_treated_as_content_triggers_e005(self) -> None:
        # Arrange
        # [[SECTION: Identity]] (space after colon) does not match TAG_PATTERN.
        # It is treated as ordinary content and triggers E005 when outside a boundary.
        fixture = _fixture("validator_invalid_e005_near_miss_tag.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, (
            "Expected at least one error for a near-miss tag treated as outside-boundary content"
        )
        codes = [e.error_code for e in errors]
        assert "E005" in codes, (
            f"Expected E005 for near-miss tag treated as content but got codes: {codes}"
        )

    def test_duplicate_boundary_name_returns_e006_error(self) -> None:
        # Arrange
        # [[SECTION:Identity]] appears twice in the file.
        fixture = _fixture("validator_invalid_e006_duplicate.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for a duplicate boundary name"
        codes = [e.error_code for e in errors]
        assert "E006" in codes, (
            f"Expected E006 (DUPLICATE_BOUNDARY) but got codes: {codes}"
        )

    def test_wrong_section_order_returns_e007_error(self) -> None:
        # Arrange
        # Capabilities appears before Identity, violating canonical section order.
        fixture = _fixture("validator_invalid_e007_wrong_order.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for wrong section order"
        codes = [e.error_code for e in errors]
        assert "E007" in codes, (
            f"Expected E007 (WRONG_SECTION_ORDER) but got codes: {codes}"
        )

    def test_injection_in_wrong_parent_section_returns_e008_error(self) -> None:
        # Arrange
        # LanguagePatterns injection is nested inside Identity instead of Capabilities.
        fixture = _fixture("validator_invalid_e008_wrong_parent.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for injection in wrong parent"
        codes = [e.error_code for e in errors]
        assert "E008" in codes, (
            f"Expected E008 (INJECTION_WRONG_PARENT) but got codes: {codes}"
        )

    def test_unexpected_frontmatter_key_returns_e009_error(self) -> None:
        # Arrange
        # Frontmatter contains 'unknown_key' which is not in KNOWN_FRONTMATTER_KEYS.
        fixture = _fixture("validator_invalid_e009_frontmatter_key.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for unexpected frontmatter key"
        codes = [e.error_code for e in errors]
        assert "E009" in codes, (
            f"Expected E009 (UNEXPECTED_FRONTMATTER_KEY) but got codes: {codes}"
        )

    def test_malformed_yaml_frontmatter_returns_e000_error(self) -> None:
        # Arrange
        # Frontmatter has no closing --- separator.
        fixture = _fixture("validator_invalid_e000_malformed_yaml.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for malformed YAML"
        codes = [e.error_code for e in errors]
        assert "E000" in codes, (
            f"Expected E000 (IO_OR_PARSE_ERROR) but got codes: {codes}"
        )

    def test_malformed_yaml_returns_only_e000_and_no_further_checks(self) -> None:
        # Arrange
        # When YAML is malformed, the validator must stop and return only E000.
        # It must not attempt further validation rules on the file.
        fixture = _fixture("validator_invalid_e000_malformed_yaml.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert all(e.error_code == "E000" for e in errors), (
            "When YAML frontmatter is malformed, only E000 errors should be returned; "
            f"got: {[e.error_code for e in errors]}"
        )

    def test_all_errors_include_file_path_matching_input(self) -> None:
        # Arrange
        fixture = _fixture("validator_invalid_e001_missing_close.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert errors, "Expected at least one error"
        for error in errors:
            assert error.file_path == fixture, (
                f"Error file_path should match the input path but got {error.file_path}"
            )

    def test_unmatched_injection_open_tag_returns_e001_error(self) -> None:
        # Arrange
        # [[INJECTION:IdentityExtension]] is opened inside Identity but never closed.
        fixture = _fixture("validator_invalid_e001_injection_missing_close.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for an unmatched injection open tag"
        codes = [e.error_code for e in errors]
        assert "E001" in codes, (
            f"Expected E001 (UNMATCHED_OPEN) for injection kind but got codes: {codes}"
        )

    def test_orphan_injection_close_tag_returns_e002_error(self) -> None:
        # Arrange
        # [[/INJECTION:IdentityExtension]] appears with no corresponding open tag.
        fixture = _fixture("validator_invalid_e002_injection_orphan_close.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for an orphan injection close tag"
        codes = [e.error_code for e in errors]
        assert "E002" in codes, (
            f"Expected E002 (UNMATCHED_CLOSE) for injection kind but got codes: {codes}"
        )

    def test_open_injection_name_is_valid_no_e004_error(self) -> None:
        # Arrange — injection names are open as of Stage 2: any name is accepted.
        # [[INJECTION:Bogus]] is no longer rejected with E004; it is preserved as a
        # project-class injection. The fixture is re-used to confirm the absence of E004.
        fixture = _fixture("validator_invalid_e004_injection_non_canonical.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        codes = [e.error_code for e in errors]
        assert "E004" not in codes, (
            f"Stage 2 makes injection names open: E004 must not be raised for an unknown "
            f"injection name; got codes: {codes}"
        )

    def test_duplicate_injection_name_returns_e006_error(self) -> None:
        # Arrange
        # [[INJECTION:IdentityExtension]] appears twice in the same file.
        fixture = _fixture("validator_invalid_e006_duplicate_injection.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for a duplicate injection name"
        codes = [e.error_code for e in errors]
        assert "E006" in codes, (
            f"Expected E006 (DUPLICATE_BOUNDARY) for duplicate injection name but got codes: {codes}"
        )

    def test_content_outside_boundary_error_line_points_to_content_line(self) -> None:
        # Arrange
        # The first outside-boundary content line is on line 8 of the fixture
        # (6 frontmatter lines + 1 blank + the content line).
        fixture = _fixture("validator_invalid_e005_content_outside.md")

        # Act
        errors = validate_file(fixture)
        e005_errors = [e for e in errors if e.error_code == "E005"]

        # Assert
        assert e005_errors, "Expected at least one E005 error"
        # Verify the first E005 error points exactly to the offending content line.
        first_e005 = e005_errors[0]
        assert first_e005.line_number == 8, (
            f"E005 error should point to line 8 (the first outside-boundary content line) "
            f"but got {first_e005.line_number}"
        )


# ---------------------------------------------------------------------------
# ValidationError string format
# ---------------------------------------------------------------------------

class TestValidationErrorStr:
    """ValidationError.__str__ produces the CLI output format."""

    def test_str_format_matches_specification(self) -> None:
        # Arrange
        # The spec requires: <filepath>:<line>: <error-code> <message>
        # Use platform-neutral relative path so the assertion is not sensitive to
        # OS path separator differences (e.g. / vs \ on Windows).
        file_path = pathlib.Path("some") / "agent.md"
        error = ValidationError(
            file_path=file_path,
            line_number=42,
            error_code="E001",
            message="Unmatched open tag SECTION:Identity",
        )

        # Act
        result = str(error)

        # Assert
        # Build the expected string the same way pathlib renders the path so the
        # test passes on both Windows and Unix without hardcoding a separator.
        expected = f"{file_path}:42: E001 Unmatched open tag SECTION:Identity"
        assert result == expected, (
            f"Expected '{expected}' but got '{result}'"
        )

    def test_str_format_matches_regex_pattern(self) -> None:
        # Arrange
        # Verify the format pattern for any ValidationError, not just the exact string above.
        file_path = pathlib.Path("Tools/tests/fixtures/some_file.md")
        error = ValidationError(
            file_path=file_path,
            line_number=7,
            error_code="E005",
            message="Content outside boundary",
        )

        # Act
        result = str(error)

        # Assert
        # Pattern: <anything>:<integer>: <Eaaa> <non-empty message>
        pattern = re.compile(r"^.+:\d+: E\d{3} .+$")
        assert pattern.match(result), (
            f"ValidationError str '{result}' does not match expected format "
            f"'<filepath>:<line>: <E-code> <message>'"
        )

    def test_str_uses_line_number_without_zero_padding(self) -> None:
        # Arrange
        error = ValidationError(
            file_path=pathlib.Path("test.md"),
            line_number=5,
            error_code="E002",
            message="Orphan close tag",
        )

        # Act
        result = str(error)

        # Assert
        assert ":5:" in result, (
            f"Line number 5 should appear as ':5:' without zero-padding, got: '{result}'"
        )

    def test_str_has_no_trailing_whitespace(self) -> None:
        # Arrange
        error = ValidationError(
            file_path=pathlib.Path("test.md"),
            line_number=1,
            error_code="E001",
            message="Some message",
        )

        # Act
        result = str(error)

        # Assert
        assert result == result.rstrip(), (
            f"ValidationError str should have no trailing whitespace but got '{result}'"
        )


# ---------------------------------------------------------------------------
# Batch mode — validate_batch
# ---------------------------------------------------------------------------

class TestValidateBatch:
    """validate_batch recursively validates all .md files in a directory."""

    def test_batch_mixed_directory_returns_only_invalid_files(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # Create a directory with one valid and one invalid file.
        valid_file = tmp_path / "valid.md"
        invalid_file = tmp_path / "invalid.md"
        shutil.copy(_fixture("validator_valid_full.md"), valid_file)
        shutil.copy(_fixture("validator_invalid_e001_missing_close.md"), invalid_file)

        # Act
        results = validate_batch(tmp_path)

        # Assert
        assert invalid_file in results, (
            "The invalid file should appear in the batch results dict"
        )
        assert valid_file not in results, (
            "Valid files should be omitted from the batch results dict"
        )

    def test_batch_all_valid_directory_returns_empty_dict(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        shutil.copy(_fixture("validator_valid_full.md"), tmp_path / "valid1.md")
        shutil.copy(_fixture("validator_valid_subset_injections.md"), tmp_path / "valid2.md")

        # Act
        results = validate_batch(tmp_path)

        # Assert
        assert results == {}, (
            f"Expected empty dict for all-valid directory but got: {results}"
        )

    def test_batch_reports_correct_errors_for_invalid_files(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        invalid_file = tmp_path / "invalid.md"
        shutil.copy(_fixture("validator_invalid_e001_missing_close.md"), invalid_file)

        # Act
        results = validate_batch(tmp_path)

        # Assert
        assert invalid_file in results
        errors = results[invalid_file]
        assert len(errors) >= 1
        codes = [e.error_code for e in errors]
        assert "E001" in codes

    def test_batch_recurses_into_subdirectories(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        subdir = tmp_path / "subdir"
        subdir.mkdir()
        invalid_file = subdir / "invalid.md"
        shutil.copy(_fixture("validator_invalid_e002_orphan_close.md"), invalid_file)

        # Act
        results = validate_batch(tmp_path)

        # Assert
        assert invalid_file in results, (
            "validate_batch must recurse into subdirectories"
        )

    def test_batch_skips_non_md_files(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # A .txt file with invalid content should be ignored.
        txt_file = tmp_path / "not_an_agent.txt"
        txt_file.write_text("[[/SECTION:Identity]]\n", encoding="utf-8")
        valid_md = tmp_path / "valid.md"
        shutil.copy(_fixture("validator_valid_full.md"), valid_md)

        # Act
        results = validate_batch(tmp_path)

        # Assert
        assert results == {}, (
            "Non-.md files should be ignored; expected empty dict"
        )

    def test_batch_unreadable_file_returns_e000_with_line_number_zero(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # Simulate an OS-unreadable file by removing read permissions.
        # This test may behave differently on Windows where chmod is limited;
        # it is skipped on Windows.
        if sys.platform == "win32":
            pytest.skip("Permission-based test not reliable on Windows")

        unreadable = tmp_path / "unreadable.md"
        unreadable.write_text("---\nid: x\n---\n", encoding="utf-8")
        unreadable.chmod(0o000)

        try:
            # Act
            results = validate_batch(tmp_path)

            # Assert
            assert unreadable in results, (
                "An unreadable file should appear in results with an E000 error"
            )
            errors = results[unreadable]
            assert len(errors) == 1
            assert errors[0].error_code == "E000"
            assert errors[0].line_number == 0, (
                "E000 from an unreadable file must have line_number=0"
            )
        finally:
            unreadable.chmod(0o644)


# ---------------------------------------------------------------------------
# CLI integration — exit codes and output format via subprocess
# ---------------------------------------------------------------------------

class TestCLIInterface:
    """The CLI exits with code 0 on success, 1 on errors, and formats output correctly."""

    def test_cli_valid_file_exits_with_code_zero(self) -> None:
        # Arrange
        fixture = _fixture("validator_valid_full.md")

        # Act
        result = _run_cli(str(fixture))

        # Assert
        assert result.returncode == 0, (
            f"Expected exit code 0 for a valid file but got {result.returncode}. "
            f"stderr: {result.stderr}"
        )

    def test_cli_valid_file_produces_no_stdout_output(self) -> None:
        # Arrange
        fixture = _fixture("validator_valid_full.md")

        # Act
        result = _run_cli(str(fixture))

        # Assert
        assert result.returncode == 0
        assert result.stdout == "", (
            f"Expected no stdout output for a valid file but got: '{result.stdout}'"
        )

    def test_cli_invalid_file_exits_with_nonzero_code(self) -> None:
        # Arrange
        fixture = _fixture("validator_invalid_e001_missing_close.md")

        # Act
        result = _run_cli(str(fixture))

        # Assert
        assert result.returncode != 0, (
            "Expected non-zero exit code for a file with validation errors"
        )
        # A non-zero exit due to a CLI crash (unhandled exception) would produce no stdout.
        # This assertion ensures the non-zero exit comes from validation output, not a crash.
        assert result.stdout.strip(), (
            "Expected at least one error line on stdout; a bare crash (no stdout) is not "
            "a valid RED-phase failure — it would pass even without any validation logic"
        )

    def test_cli_invalid_file_output_matches_error_format(self) -> None:
        # Arrange
        # Output format: <filepath>:<line>: <error-code> <message>
        fixture = _fixture("validator_invalid_e001_missing_close.md")
        pattern = re.compile(r"^.+:\d+: E\d{3} .+$", re.MULTILINE)

        # Act
        result = _run_cli(str(fixture))

        # Assert
        assert result.returncode != 0
        assert result.stdout.strip(), (
            "Expected at least one line of output for an invalid file"
        )
        for line in result.stdout.strip().splitlines():
            assert pattern.match(line), (
                f"Output line does not match expected format '<filepath>:<line>: <code> <message>': "
                f"'{line}'"
            )

    def test_cli_output_lines_contain_correct_error_code(self) -> None:
        # Arrange
        fixture = _fixture("validator_invalid_e001_missing_close.md")

        # Act
        result = _run_cli(str(fixture))

        # Assert
        assert result.returncode != 0
        assert "E001" in result.stdout, (
            f"Expected 'E001' in CLI output but got: '{result.stdout}'"
        )

    def test_cli_batch_flag_with_mixed_directory_exits_nonzero(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        shutil.copy(_fixture("validator_valid_full.md"), tmp_path / "valid.md")
        shutil.copy(
            _fixture("validator_invalid_e001_missing_close.md"), tmp_path / "invalid.md"
        )

        # Act
        result = _run_cli(str(tmp_path), "--batch")

        # Assert
        assert result.returncode != 0, (
            "Expected non-zero exit code for a batch directory with invalid files"
        )
        assert result.stdout.strip(), (
            "Expected at least one error line in stdout for a batch directory with invalid files"
        )

    def test_cli_batch_flag_with_all_valid_directory_exits_zero(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        shutil.copy(_fixture("validator_valid_full.md"), tmp_path / "valid1.md")
        shutil.copy(
            _fixture("validator_valid_subset_injections.md"), tmp_path / "valid2.md"
        )

        # Act
        result = _run_cli(str(tmp_path), "--batch")

        # Assert
        assert result.returncode == 0, (
            f"Expected exit code 0 for all-valid batch directory but got {result.returncode}. "
            f"stderr: {result.stderr}"
        )
        assert result.stdout == "", (
            f"Expected no stdout output for all-valid batch directory but got: '{result.stdout}'"
        )

    def test_cli_batch_reports_errors_only_for_invalid_files(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # Use names that are not substrings of each other so substring checks
        # on stdout are unambiguous ("valid.md" is a substring of "invalid.md",
        # which would make the "not in" assertion permanently unsatisfiable).
        valid_file = tmp_path / "good.md"
        invalid_file = tmp_path / "broken.md"
        shutil.copy(_fixture("validator_valid_full.md"), valid_file)
        shutil.copy(_fixture("validator_invalid_e005_content_outside.md"), invalid_file)

        # Act
        result = _run_cli(str(tmp_path), "--batch")

        # Assert
        assert result.returncode != 0
        # The invalid file's path should appear in the output
        assert "broken.md" in result.stdout, (
            "Expected the invalid file's name in the CLI output"
        )
        # The valid file's path should not appear in the output
        assert "good.md" not in result.stdout, (
            "The valid file's name should not appear in CLI output"
        )


# ---------------------------------------------------------------------------
# ArtifactProvenance removed from vocabulary (Stage 2)
# ---------------------------------------------------------------------------
#
# ArtifactProvenance and ArtifactProvenanceExtension are removed from the vocabulary
# in Stage 2. After Stage 2 implementation is complete:
#
#   - [[DEPLOYED:ArtifactProvenance]] produces E011 (unrecognised tool-managed boundary
#     name), since ArtifactProvenance is no longer in CANONICAL_DEPLOYED or EXPECTED_MARKER.
#     The parent check (E008) is skipped — it only applies to canonical deployed names.
#     The order check (E007) is likewise skipped — ArtifactProvenance is absent from
#     CANONICAL_ORDER.
#
#   - [[INJECTION:ArtifactProvenanceExtension]] produces no error. Injection names are
#     open: an unlisted name is preserved and never flagged or rejected. The parent
#     check (E008) is skipped for unlisted injection names. No E004 fires (the
#     CANONICAL_INJECTIONS allowlist is removed along with the E004 injection check).
#
# These tests replace the pre-Stage-2 classes that asserted ArtifactProvenance as a
# valid canonical deployed boundary and ArtifactProvenanceExtension as a top-level
# injection with an enforced parent rule — behavior this stage explicitly retires.


class TestArtifactProvenanceRemovedFromVocabulary:
    """After Stage 2, [[DEPLOYED:ArtifactProvenance]] is unknown and produces E011.

    ArtifactProvenance is absent from CANONICAL_DEPLOYED, CANONICAL_ORDER, and
    DEPLOYED_PARENT_MAP. Any file carrying [[DEPLOYED:ArtifactProvenance]] receives
    E011 (unrecognised tool-managed boundary name). The parent (E008) and order (E007)
    checks are skipped for non-canonical deployed names.
    """

    def test_artifact_provenance_deployed_at_top_level_produces_e011(self) -> None:
        # Fixture has [[DEPLOYED:ArtifactProvenance]] at body top level. After Stage 2,
        # ArtifactProvenance is not in CANONICAL_DEPLOYED so E011 fires.
        fixture = _fixture("validator_valid_artifact_provenance_top_level.md")

        errors = validate_file(fixture)

        codes = [e.error_code for e in errors]
        assert "E011" in codes, (
            f"Expected E011 for [[DEPLOYED:ArtifactProvenance]] (removed from vocabulary), "
            f"got codes: {codes}"
        )

    def test_artifact_provenance_deployed_e011_message_names_artifact_provenance(self) -> None:
        # The E011 message must identify the offending name.
        fixture = _fixture("validator_valid_artifact_provenance_top_level.md")

        errors = validate_file(fixture)
        e011_errors = [e for e in errors if e.error_code == "E011"]

        assert e011_errors, "Expected at least one E011 error"
        ap_e011 = [e for e in e011_errors if "ArtifactProvenance" in e.message]
        assert ap_e011, (
            f"Expected E011 mentioning 'ArtifactProvenance', "
            f"got E011 messages: {[e.message for e in e011_errors]}"
        )

    def test_artifact_provenance_deployed_in_section_also_produces_e011(self) -> None:
        # E011 fires regardless of placement; the parent check (E008) is skipped for
        # non-canonical deployed names.
        fixture = _fixture("validator_invalid_e008_artifact_provenance_in_section.md")

        errors = validate_file(fixture)

        codes = [e.error_code for e in errors]
        assert "E011" in codes, (
            f"Expected E011 for [[DEPLOYED:ArtifactProvenance]] inside a section, "
            f"got codes: {codes}"
        )

    def test_artifact_provenance_deployed_does_not_produce_e008(self) -> None:
        # The parent check (E008) only fires for canonical deployed names. An unknown
        # deployed name is flagged with E011 only; no placement check is performed.
        fixture = _fixture("validator_invalid_e008_artifact_provenance_in_section.md")

        errors = validate_file(fixture)

        e008_errors = [e for e in errors if e.error_code == "E008"]
        ap_e008 = [e for e in e008_errors if "ArtifactProvenance" in e.message]
        assert ap_e008 == [], (
            f"Expected no E008 for non-canonical [[DEPLOYED:ArtifactProvenance]] "
            f"(parent check skipped for unknown names), got: {ap_e008}"
        )

    def test_artifact_provenance_deployed_does_not_produce_e007(self) -> None:
        # ArtifactProvenance is absent from CANONICAL_ORDER; the order check is skipped.
        # A file with ArtifactProvenance appearing after Capabilities must not produce E007
        # for that name — only E011.
        fixture = _fixture("validator_invalid_e007_artifact_provenance_out_of_order.md")

        errors = validate_file(fixture)

        e007_errors = [e for e in errors if e.error_code == "E007"]
        ap_e007 = [e for e in e007_errors if "ArtifactProvenance" in e.message]
        assert ap_e007 == [], (
            f"Expected no E007 for [[DEPLOYED:ArtifactProvenance]] (removed from CANONICAL_ORDER); "
            f"only E011 should fire. Got E007 errors: {ap_e007}"
        )


class TestArtifactProvenanceExtensionAsOpenInjection:
    """After Stage 2, [[INJECTION:ArtifactProvenanceExtension]] is an open, unlisted name.

    ArtifactProvenanceExtension is removed from INJECTION_PARENT_MAP. Injection names
    are open: an unlisted name is preserved and never flagged. No E004 (non-canonical
    injection) fires because CANONICAL_INJECTIONS is removed. No E008 (wrong parent)
    fires because the parent check only applies to names present in INJECTION_PARENT_MAP.
    """

    def test_artifact_provenance_extension_at_top_level_produces_no_error_for_injection(
        self,
    ) -> None:
        # Fixture has [[INJECTION:ArtifactProvenanceExtension]] at body top level.
        # After Stage 2, this is an open injection name and must not produce any
        # injection-related error. (The fixture also has [[DEPLOYED:ArtifactProvenance]]
        # which will produce E011 — we check only errors mentioning the injection name.)
        #
        # Vocabulary-state guard: before Stage 2 implementation removes it,
        # ArtifactProvenanceExtension is still in INJECTION_PARENT_MAP with parent ""
        # (top level), which coincidentally matches the fixture placement and causes the
        # behavior assertion below to pass without exercising the "open names" policy.
        # This assertion fails against that stray pre-Stage-2 state, enforcing RED phase.
        assert "ArtifactProvenanceExtension" not in boundary_constants.INJECTION_PARENT_MAP, (
            "ArtifactProvenanceExtension must be absent from INJECTION_PARENT_MAP after "
            "Stage 2. Its presence in the pre-Stage-2 stray state causes the behavior "
            "assertion below to pass trivially (correct placement → no error), which "
            "defeats the purpose of this test."
        )
        fixture = _fixture("validator_valid_artifact_provenance_top_level.md")

        errors = validate_file(fixture)

        ape_errors = [e for e in errors if "ArtifactProvenanceExtension" in e.message]
        assert ape_errors == [], (
            f"Expected no errors for open injection name 'ArtifactProvenanceExtension', "
            f"got: {ape_errors}"
        )

    def test_artifact_provenance_extension_nested_in_section_produces_no_e008(self) -> None:
        # ArtifactProvenanceExtension is not in INJECTION_PARENT_MAP; parent check skipped.
        # The fixture has it nested inside Identity — no E008 must fire for the injection.
        fixture = _fixture("validator_invalid_e008_artifact_provenance_extension_in_section.md")

        errors = validate_file(fixture)

        e008_errors = [e for e in errors if e.error_code == "E008"]
        ape_e008 = [e for e in e008_errors if "ArtifactProvenanceExtension" in e.message]
        assert ape_e008 == [], (
            f"Expected no E008 for open injection name 'ArtifactProvenanceExtension' "
            f"nested inside a section (parent check skipped for unlisted names), "
            f"got: {ape_e008}"
        )

    def test_artifact_provenance_extension_produces_no_e004(self) -> None:
        # After Stage 2, CANONICAL_INJECTIONS is removed; no E004 fires for injection names.
        #
        # Vocabulary-state guard: before Stage 2 implementation removes it,
        # ArtifactProvenanceExtension is still in CANONICAL_INJECTIONS, so no E004 fires
        # for the wrong reason (it IS recognised, not because injections are open).
        # This assertion fails against that stray pre-Stage-2 state (where CANONICAL_INJECTIONS
        # exists and contains ArtifactProvenanceExtension), enforcing RED phase.
        # After Stage 2, CANONICAL_INJECTIONS is removed entirely; getattr returns ()
        # and the assertion passes.
        _canonical_injections = getattr(boundary_constants, "CANONICAL_INJECTIONS", ())
        assert "ArtifactProvenanceExtension" not in _canonical_injections, (
            "ArtifactProvenanceExtension must be absent from CANONICAL_INJECTIONS after "
            "Stage 2 (CANONICAL_INJECTIONS itself should be removed entirely). Its presence "
            "in the pre-Stage-2 stray state caused E004 not to fire for the wrong reason, "
            "making the behavior assertion below pass trivially."
        )
        fixture = _fixture("validator_valid_artifact_provenance_top_level.md")

        errors = validate_file(fixture)

        e004_errors = [e for e in errors if e.error_code == "E004"]
        ape_e004 = [e for e in e004_errors if "ArtifactProvenanceExtension" in e.message]
        assert ape_e004 == [], (
            f"Expected no E004 for 'ArtifactProvenanceExtension' (injection names are open; "
            f"CANONICAL_INJECTIONS is removed), got: {ape_e004}"
        )


# ---------------------------------------------------------------------------
# Python validator behavior for open injection names and unknown deployed names
# (AC2.5 / AC2.6)
# ---------------------------------------------------------------------------
#
# AC2.5: an unrecognised [[INJECTION:]] name produces no error from the Python validator.
# AC2.6: an unrecognised [[DEPLOYED:]] name still produces an error (E011).
#
# These mirror the Go suite's TestValidate_OpenInjectionName_ProducesNoIssue and
# TestValidate_BundleNameAsInjection_ReportsWrongMarkerIssue from
# vocabulary_boundary_sync_test.go, exercising the Python validate_file entry point
# directly — the coverage gap identified by the Stage 2 test review.


class TestPythonValidatorOpenNamesAC2_5_AC2_6:
    """Python validator tests for AC2.5 and AC2.6.

    AC2.5: validate_file produces no error for any unrecognised [[INJECTION:]] name.
    AC2.6: validate_file produces E011 for any unrecognised [[DEPLOYED:]] name.
    """

    def test_open_injection_name_produces_no_error_from_python_validator(self) -> None:
        # AC2.5: the Python validator accepts an unrecognised injection name without error.
        # Fixture has [[INJECTION:CustomOpenExtension]] inside Identity — an unlisted name.
        fixture = _fixture("validator_valid_open_injection_name.md")

        errors = validate_file(fixture)

        injection_errors = [e for e in errors if "CustomOpenExtension" in e.message]
        assert injection_errors == [], (
            f"AC2.5: expected no errors for open injection name 'CustomOpenExtension', "
            f"got: {injection_errors}"
        )

    def test_open_injection_name_does_not_produce_e004_from_python_validator(self) -> None:
        # AC2.5: no E004 (non-canonical injection) fires for any injection name after Stage 2.
        fixture = _fixture("validator_valid_open_injection_name.md")

        errors = validate_file(fixture)

        e004_errors = [e for e in errors if e.error_code == "E004"]
        assert e004_errors == [], (
            f"AC2.5: expected no E004 errors (CANONICAL_INJECTIONS removed; all injection "
            f"names are open), got: {e004_errors}"
        )

    def test_unknown_deployed_name_produces_e011_from_python_validator(self) -> None:
        # AC2.6: the Python validator still reports an error for an unrecognised [[DEPLOYED:]] name.
        # ArtifactProvenance is removed from CANONICAL_DEPLOYED after Stage 2.
        fixture = _fixture("validator_valid_artifact_provenance_top_level.md")

        errors = validate_file(fixture)

        codes = [e.error_code for e in errors]
        assert "E011" in codes, (
            f"AC2.6: expected E011 for unknown [[DEPLOYED:ArtifactProvenance]], "
            f"got codes: {codes}"
        )

    def test_unknown_deployed_name_error_is_e011_not_e004_from_python_validator(self) -> None:
        # AC2.6: E011 is the correct code for unknown deployed names; E004 is for sections.
        fixture = _fixture("validator_valid_artifact_provenance_top_level.md")

        errors = validate_file(fixture)

        codes = [e.error_code for e in errors]
        assert "E011" in codes, f"AC2.6: expected E011 for unknown deployed name, got: {codes}"
        e004_for_deployed = [
            e for e in errors
            if e.error_code == "E004" and "ArtifactProvenance" in e.message
        ]
        assert e004_for_deployed == [], (
            f"AC2.6: expected no E004 for [[DEPLOYED:ArtifactProvenance]] (E011 is the correct "
            f"code for unknown deployed names), got: {e004_for_deployed}"
        )


# ---------------------------------------------------------------------------
# Known frontmatter keys: role and bundle_version (Stage 4)
# ---------------------------------------------------------------------------
#
# Two new keys enter the ecosystem during this run:
#   - role: declared by every migrated agent source file (subagent or orchestrator)
#   - bundle_version: stamped by the deployment tool into every deployed subagent file
#
# Without both keys in KNOWN_FRONTMATTER_KEYS, the boundary validator raises E009
# (UNEXPECTED_FRONTMATTER_KEY) for the affected files.  Stage 4 adds both keys to
# the allowlist so the validator accepts them.


class TestKnownFrontmatterKeys_RoleAndBundleVersion:
    """validate_file raises no E009 for the role and bundle_version frontmatter keys."""

    def test_file_with_role_key_returns_no_e009_error(self) -> None:
        # Arrange
        # The fixture declares role: subagent in frontmatter.  After Stage 4, this key
        # is in KNOWN_FRONTMATTER_KEYS and the validator must not reject it with E009.
        fixture = _fixture("validator_valid_role_and_bundle_version.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        e009_errors = [e for e in errors if e.error_code == "E009"]
        role_e009 = [e for e in e009_errors if "role" in e.message]
        assert role_e009 == [], (
            "Expected no E009 error for the 'role' frontmatter key after Stage 4 adds it "
            f"to KNOWN_FRONTMATTER_KEYS, but got: {role_e009}"
        )

    def test_file_with_bundle_version_key_returns_no_e009_error(self) -> None:
        # Arrange
        # The fixture declares bundle_version: 1.0.0 in frontmatter.  This key is the
        # tool-written stamp; after Stage 4 it is in KNOWN_FRONTMATTER_KEYS and must not
        # trigger E009.
        fixture = _fixture("validator_valid_role_and_bundle_version.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        e009_errors = [e for e in errors if e.error_code == "E009"]
        bv_e009 = [e for e in e009_errors if "bundle_version" in e.message]
        assert bv_e009 == [], (
            "Expected no E009 error for the 'bundle_version' frontmatter key after Stage 4 "
            f"adds it to KNOWN_FRONTMATTER_KEYS, but got: {bv_e009}"
        )

    def test_file_with_both_role_and_bundle_version_returns_no_e009(self) -> None:
        # Arrange
        # The fixture declares both role and bundle_version.  Neither must produce E009.
        # This is the combined assertion that both keys are accepted simultaneously —
        # registering only one would leave the other rejected.
        fixture = _fixture("validator_valid_role_and_bundle_version.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        e009_errors = [e for e in errors if e.error_code == "E009"]
        new_key_e009 = [
            e for e in e009_errors
            if "role" in e.message or "bundle_version" in e.message
        ]
        assert new_key_e009 == [], (
            "Expected no E009 errors for either 'role' or 'bundle_version' frontmatter keys; "
            f"both must be in KNOWN_FRONTMATTER_KEYS after Stage 4. Got: {new_key_e009}"
        )

    def test_genuinely_unknown_key_still_returns_e009(self) -> None:
        # Arrange
        # Adding role and bundle_version to the allowlist must not accidentally disable E009
        # for keys that genuinely do not belong.  The existing E009 fixture carries an
        # unknown key; it must still be rejected.
        fixture = _fixture("validator_invalid_e009_frontmatter_key.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        codes = [e.error_code for e in errors]
        assert "E009" in codes, (
            "Expected E009 for the unknown frontmatter key in the existing fixture after "
            "Stage 4 adds role and bundle_version to the allowlist. The allowlist addition "
            "must not disable E009 for genuinely unknown keys. "
            f"Got codes: {codes}"
        )

    def test_role_key_does_not_appear_in_e009_message_for_valid_file(self) -> None:
        # Arrange
        # Regression: if the validator generates E009 and happens to reference the key
        # name in the message, the 'role' key must not appear there for a valid file.
        fixture = _fixture("validator_valid_role_and_bundle_version.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        for error in errors:
            if error.error_code == "E009":
                assert "role" not in error.message or "bundle_version" in error.message, (
                    f"E009 error references 'role' for a file where role is a known key: {error}"
                )

    def test_bundle_version_key_is_accepted_without_role_key(self, tmp_path: pathlib.Path) -> None:
        # Arrange
        # bundle_version is a tool-written stamp; it may appear without role (e.g. in a
        # deployed file for a category that did not yet receive the role field).
        # Verify bundle_version alone does not trigger E009.
        content = (
            "---\n"
            "id: test-bv-only\n"
            "version: 1.0.0\n"
            "name: test-agent\n"
            "description: Deployed file with bundle_version but no role field.\n"
            "bundle_version: 2.3.1\n"
            "---\n\n"
            "[[SECTION:Identity]]\n"
            "# TestAgent Agent\n"
            "Content here.\n"
            "[[/SECTION:Identity]]\n"
        )
        agent_file = tmp_path / "deployed-agent.md"
        agent_file.write_text(content, encoding="utf-8")

        # Act
        errors = validate_file(agent_file)

        # Assert
        bv_e009 = [e for e in errors if e.error_code == "E009" and "bundle_version" in e.message]
        assert bv_e009 == [], (
            "Expected no E009 for bundle_version frontmatter key when it appears without a "
            f"role field; bundle_version must be accepted as a known key. Got: {bv_e009}"
        )

    def test_role_key_is_accepted_without_bundle_version_key(self, tmp_path: pathlib.Path) -> None:
        # Arrange
        # Source agent files declare role but do not carry bundle_version (that stamp is
        # added by the deploy tool).  Verify role alone does not trigger E009.
        content = (
            "---\n"
            "id: test-role-only\n"
            "version: 1.0.0\n"
            "name: test-agent\n"
            "description: Source file with role field but no bundle_version stamp.\n"
            "role: subagent\n"
            "---\n\n"
            "[[SECTION:Identity]]\n"
            "# TestAgent Agent\n"
            "Content here.\n"
            "[[/SECTION:Identity]]\n"
        )
        agent_file = tmp_path / "source-agent.md"
        agent_file.write_text(content, encoding="utf-8")

        # Act
        errors = validate_file(agent_file)

        # Assert
        role_e009 = [e for e in errors if e.error_code == "E009" and "role" in e.message]
        assert role_e009 == [], (
            "Expected no E009 for role frontmatter key when it appears without a bundle_version "
            f"stamp; role must be accepted as a known key. Got: {role_e009}"
        )


# ---------------------------------------------------------------------------
# Stage 3 validator relaxations (T3.4)
# ---------------------------------------------------------------------------
#
# Three checks are relaxed in Stage 3. The Python validator must match the Go
# validator's behaviour for all three:
#
#   1. Unknown injection names: any [[INJECTION:]] name is valid and produces no
#      finding of any kind. (Already covered by Stage 2 tests; confirmed here as
#      a regression guard.)
#
#   2. Subsequence order (E007): non-canonical sections between canonical sections
#      do not affect the E007 order check. The order check is a subsequence test:
#      names absent from CANONICAL_ORDER are skipped entirely. Missing canonical
#      sections (only a subset present) also pass.
#
#   3. Injection parent advisory: a misplaced [[INJECTION:]] region is reported at
#      advisory severity rather than error severity. The finding is present but
#      never fatal: the CLI exits 0 when only advisory findings exist. A misplaced
#      [[DEPLOYED:]] region remains a hard error (E008 at error severity).
#
# Implementation note: Stage 3 adds a `severity` attribute to ValidationError
# (defaulting to "error") so callers can distinguish advisory findings from errors.
# The CLI exit-code logic changes to exit 0 when no error-severity finding exists.


class TestStage3UnknownInjectionNamesRegression:
    """Unknown injection names produce no finding of any kind (regression guard).

    Injection names are open unconditionally. Any [[INJECTION:]] name that is not in
    CanonicalDeployed is valid, preserved, and produces no finding — not even an advisory
    finding. This was established in Stage 2; the Stage 3 tests confirm it still holds
    after advisory severity is introduced.
    """

    def test_unknown_injection_name_produces_no_finding_at_any_severity(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # A document with [[INJECTION:CompletelyUnknownName]] — not in any table.
        content = (
            "---\n"
            "id: test-unknown\n"
            "version: 1.0.0\n"
            "name: test-agent\n"
            "description: Agent with unknown injection name.\n"
            "---\n\n"
            "[[SECTION:Identity]]\n"
            "# TestAgent Agent\n"
            "[[INJECTION:CompletelyUnknownName]]\n"
            "Content.\n"
            "[[/INJECTION:CompletelyUnknownName]]\n"
            "[[/SECTION:Identity]]\n"
        )
        agent_file = tmp_path / "unknown-injection.md"
        agent_file.write_text(content, encoding="utf-8")

        # Act
        findings = validate_file(agent_file)

        # Assert
        unknown_findings = [f for f in findings if "CompletelyUnknownName" in f.message]
        assert unknown_findings == [], (
            "Unknown injection name must produce no finding of any kind after Stage 3; "
            f"got: {unknown_findings}"
        )

    def test_unknown_injection_name_does_not_produce_e004(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        content = (
            "---\n"
            "id: test-unknown\n"
            "version: 1.0.0\n"
            "name: test-agent\n"
            "description: Agent with unknown injection name.\n"
            "---\n\n"
            "[[SECTION:Identity]]\n"
            "[[INJECTION:TotallyBogusName]]\n"
            "[[/INJECTION:TotallyBogusName]]\n"
            "[[/SECTION:Identity]]\n"
        )
        agent_file = tmp_path / "unknown-injection2.md"
        agent_file.write_text(content, encoding="utf-8")

        # Act
        findings = validate_file(agent_file)

        # Assert
        e004_findings = [f for f in findings if f.error_code == "E004"]
        injection_e004 = [f for f in e004_findings if "TotallyBogusName" in f.message]
        assert injection_e004 == [], (
            "E004 must not be produced for unknown injection names (injection names are open); "
            f"got: {injection_e004}"
        )


class TestStage3SubsequenceOrderCheck:
    """E007 order check is a subsequence test: non-canonical sections are skipped.

    A document's top-level boundaries must form a subsequence of CANONICAL_ORDER.
    A canonical section may be absent; a document may carry additional top-level
    sections under any name and they are skipped by the check. Any two canonical
    entries that are both present must appear in the listed relative order.
    """

    def test_noncanonical_sections_between_canonical_sections_do_not_produce_e007(
        self,
    ) -> None:
        # Arrange
        # The fixture has Identity, PrologueSection (non-canonical), Capabilities,
        # InternalNotes (non-canonical), Constraints — canonical entries are in the
        # correct relative order. E004 will fire for the non-canonical names; we
        # only assert that E007 does not fire.
        fixture = _fixture("validator_valid_extra_noncanonical_sections.md")

        # Act
        findings = validate_file(fixture)

        # Assert
        e007_findings = [f for f in findings if f.error_code == "E007"]
        assert e007_findings == [], (
            "Non-canonical sections between canonical sections must not produce E007. "
            "The order check is a subsequence test that skips names absent from "
            f"CANONICAL_ORDER. Got E007 findings: {e007_findings}"
        )

    def test_document_with_only_subset_of_canonical_sections_does_not_produce_e007(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # Only Identity and Constraints are present (no Capabilities, ErrorHandling, etc.).
        # Absent canonical sections are never an order issue.
        content = (
            "---\n"
            "id: test-subset\n"
            "version: 1.0.0\n"
            "name: test-agent\n"
            "description: Document with only a subset of canonical sections.\n"
            "---\n\n"
            "[[SECTION:Identity]]\n"
            "# TestAgent Agent\n"
            "Identity content.\n"
            "[[/SECTION:Identity]]\n\n"
            "[[SECTION:Constraints]]\n"
            "## Constraints\n"
            "Constraints content.\n"
            "[[/SECTION:Constraints]]\n"
        )
        agent_file = tmp_path / "subset-sections.md"
        agent_file.write_text(content, encoding="utf-8")

        # Act
        findings = validate_file(agent_file)

        # Assert
        e007_findings = [f for f in findings if f.error_code == "E007"]
        assert e007_findings == [], (
            "A document with only a subset of canonical sections in correct relative order "
            f"must not produce E007. Got: {e007_findings}"
        )

    def test_two_canonical_sections_in_wrong_relative_order_still_produces_e007(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # Capabilities appears before Identity — wrong canonical order.
        # The subsequence relaxation only skips absent or non-canonical entries; it does
        # NOT tolerate misordering among entries that are both present.
        content = (
            "---\n"
            "id: test-wrong-order\n"
            "version: 1.0.0\n"
            "name: test-agent\n"
            "description: Document with canonical sections in wrong order.\n"
            "---\n\n"
            "[[SECTION:Capabilities]]\n"
            "## Capabilities\n"
            "Capabilities before Identity — wrong order.\n"
            "[[/SECTION:Capabilities]]\n\n"
            "[[SECTION:Identity]]\n"
            "# TestAgent Agent\n"
            "Identity appears after Capabilities.\n"
            "[[/SECTION:Identity]]\n"
        )
        agent_file = tmp_path / "wrong-canonical-order.md"
        agent_file.write_text(content, encoding="utf-8")

        # Act
        findings = validate_file(agent_file)

        # Assert
        e007_findings = [f for f in findings if f.error_code == "E007"]
        assert e007_findings, (
            "Two canonical sections in wrong relative order must still produce E007. "
            "The subsequence check enforces ordering among present canonical entries."
        )


class TestStage3InjectionParentAdvisory:
    """Misplaced [[INJECTION:]] regions are advisory, not errors.

    In Stage 3, an injection placed in an unusual location harms nobody. It is
    reported at advisory severity so the author is informed, but it never fails
    validation (CLI exits 0). A misplaced [[DEPLOYED:]] region remains a hard
    error (E008 at error severity) — the advisory relaxation applies only to
    [[INJECTION:]].

    Implementation: Stage 3 adds a `severity` attribute to ValidationError
    (defaulting to "error"). Advisory findings use severity "advice". The CLI
    exit code changes to 1 only when at least one finding has severity "error".
    """

    def test_injection_wrong_parent_finding_has_advice_severity(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # IdentityExtension inside Capabilities — advisory parent mismatch.
        content = (
            "---\n"
            "id: test-advisory\n"
            "version: 1.0.0\n"
            "name: test-agent\n"
            "description: Agent with injection in wrong parent section.\n"
            "---\n\n"
            "[[SECTION:Capabilities]]\n"
            "## Capabilities\n"
            "[[INJECTION:IdentityExtension]]\n"
            "Extension content.\n"
            "[[/INJECTION:IdentityExtension]]\n"
            "[[/SECTION:Capabilities]]\n"
        )
        agent_file = tmp_path / "advisory-injection.md"
        agent_file.write_text(content, encoding="utf-8")

        # Act
        findings = validate_file(agent_file)

        # Assert
        injection_findings = [f for f in findings if "IdentityExtension" in f.message]
        assert injection_findings, (
            "Expected a finding for the misplaced injection to confirm advisory severity "
            "(absence of a finding is also valid but must be explicitly tested separately)"
        )
        for finding in injection_findings:
            assert hasattr(finding, "severity"), (
                "ValidationError must have a 'severity' attribute after Stage 3; "
                "finding has no severity field"
            )
            assert finding.severity == "advice", (
                f"Injection wrong-parent must be advisory severity after Stage 3, "
                f"got {finding.severity!r}"
            )

    def test_cli_exits_zero_when_only_injection_advisory_findings_exist(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # A document with only a misplaced injection (advisory). No error-severity findings.
        content = (
            "---\n"
            "id: test-cli-advisory\n"
            "version: 1.0.0\n"
            "name: test-agent\n"
            "description: Agent with only an advisory finding.\n"
            "---\n\n"
            "[[SECTION:Capabilities]]\n"
            "## Capabilities\n"
            "[[INJECTION:IdentityExtension]]\n"
            "Extension content.\n"
            "[[/INJECTION:IdentityExtension]]\n"
            "[[/SECTION:Capabilities]]\n"
        )
        agent_file = tmp_path / "advisory-only.md"
        agent_file.write_text(content, encoding="utf-8")

        # Act
        result = _run_cli(str(agent_file))

        # Assert
        assert result.returncode == 0, (
            "CLI must exit 0 when only advisory findings exist. "
            "Injection wrong-parent is advisory after Stage 3 and must not fail the run. "
            f"Got returncode={result.returncode}, stdout={result.stdout!r}"
        )

    def test_deployed_wrong_parent_severity_remains_error(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # LanguagePatterns inside Identity — deployed region in wrong parent. Must remain
        # a hard error. The advisory relaxation applies only to [[INJECTION:]] regions.
        content = (
            "---\n"
            "id: test-deployed-parent\n"
            "version: 1.0.0\n"
            "name: test-agent\n"
            "description: Agent with deployed region in wrong parent section.\n"
            "---\n\n"
            "[[SECTION:Identity]]\n"
            "# TestAgent Agent\n"
            "[[DEPLOYED:LanguagePatterns]]\n"
            "[[/DEPLOYED:LanguagePatterns]]\n"
            "[[/SECTION:Identity]]\n"
        )
        agent_file = tmp_path / "deployed-wrong-parent.md"
        agent_file.write_text(content, encoding="utf-8")

        # Act
        findings = validate_file(agent_file)

        # Assert
        e008_findings = [
            f for f in findings
            if f.error_code == "E008" and "LanguagePatterns" in f.message
        ]
        assert e008_findings, (
            "A misplaced [[DEPLOYED:]] region must still produce an E008 finding after Stage 3"
        )
        for finding in e008_findings:
            assert hasattr(finding, "severity"), (
                "ValidationError must have a 'severity' attribute after Stage 3"
            )
            assert finding.severity == "error", (
                f"Deployed wrong-parent must remain error severity after Stage 3, "
                f"got {finding.severity!r}"
            )

    def test_cli_exits_nonzero_when_deployed_wrong_parent_exists(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # A document with a misplaced deployed region (hard error). CLI must exit 1.
        content = (
            "---\n"
            "id: test-deployed-err\n"
            "version: 1.0.0\n"
            "name: test-agent\n"
            "description: Agent with deployed region in wrong parent section.\n"
            "---\n\n"
            "[[SECTION:Identity]]\n"
            "# TestAgent Agent\n"
            "[[DEPLOYED:LanguagePatterns]]\n"
            "[[/DEPLOYED:LanguagePatterns]]\n"
            "[[/SECTION:Identity]]\n"
        )
        agent_file = tmp_path / "deployed-error.md"
        agent_file.write_text(content, encoding="utf-8")

        # Act
        result = _run_cli(str(agent_file))

        # Assert
        assert result.returncode != 0, (
            "CLI must exit non-zero when an error-severity finding (deployed wrong-parent) exists. "
            f"Got returncode={result.returncode}"
        )


# ---------------------------------------------------------------------------
# Hyphenated frontmatter key recognised by validator (Stage 1, Defect 3)
# ---------------------------------------------------------------------------

class TestHyphenatedFrontmatterKeyValidator:
    """The validator must genuinely match hyphenated frontmatter keys, not silently skip them.

    The current _YAML_KEY_PATTERN omits '-' so 'base-version' fails to match.  The
    'if key_match:' branch is falsy, and the key is silently skipped — it is never
    checked against KNOWN_FRONTMATTER_KEYS, so no E009 fires regardless of whether the
    key is known or unknown.

    After widening the pattern to r"^([A-Za-z_][A-Za-z0-9_-]*)\\s*:", hyphenated keys
    are genuinely matched and routed into the KNOWN_FRONTMATTER_KEYS check:
    - 'base-version' (already in KNOWN_FRONTMATTER_KEYS) → no E009
    - An unknown hyphenated key → E009 (proof that matching is real, not vacuous)

    test_known_hyphenated_key_base_version_produces_no_e009 would pass in both the
    buggy and fixed states (silently skipped ≡ no E009, same as matched-and-known).
    test_unknown_hyphenated_key_produces_e009 is the RED-phase discriminator: it
    asserts E009 for an unknown hyphenated key, which the buggy silently-skip path
    never raises.
    """

    def test_known_hyphenated_key_base_version_produces_no_e009(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # A well-formed file whose frontmatter carries 'base-version: 1.0.0'.
        # base-version is already in KNOWN_FRONTMATTER_KEYS; after the fix the key is
        # matched, found in the allowlist, and accepted without E009.
        content = (
            "---\n"
            "id: test-hyphen-known\n"
            "version: 1.0.0\n"
            "base-version: 1.0.0\n"
            "name: test-agent\n"
            "description: Agent with a known hyphenated frontmatter key.\n"
            "---\n\n"
            "[[SECTION:Identity]]\n"
            "# TestAgent Agent\n"
            "Content here.\n"
            "[[/SECTION:Identity]]\n"
        )
        agent_file = tmp_path / "hyphen-known.md"
        agent_file.write_text(content, encoding="utf-8")

        # Act
        errors = validate_file(agent_file)

        # Assert
        e009_errors = [e for e in errors if e.error_code == "E009"]
        base_version_e009 = [e for e in e009_errors if "base-version" in e.message]
        assert base_version_e009 == [], (
            "Expected no E009 error for 'base-version' (a key in KNOWN_FRONTMATTER_KEYS). "
            f"Got: {base_version_e009}"
        )

    def test_unknown_hyphenated_key_produces_e009(
        self, tmp_path: pathlib.Path
    ) -> None:
        # Arrange
        # A file whose frontmatter carries 'custom-hyphen-field: some-value', a hyphenated
        # key that is NOT in KNOWN_FRONTMATTER_KEYS.
        #
        # Before the fix (buggy): _YAML_KEY_PATTERN does not match 'custom-hyphen-field',
        # so the key is silently skipped — no E009 is raised.  This test FAILS in RED phase.
        #
        # After the fix (widened pattern): the key is matched, checked against
        # KNOWN_FRONTMATTER_KEYS, found absent, and E009 is raised.  Test PASSES.
        content = (
            "---\n"
            "id: test-hyphen-unknown\n"
            "version: 1.0.0\n"
            "custom-hyphen-field: some-value\n"
            "name: test-agent\n"
            "description: Agent with an unknown hyphenated frontmatter key.\n"
            "---\n\n"
            "[[SECTION:Identity]]\n"
            "# TestAgent Agent\n"
            "Content here.\n"
            "[[/SECTION:Identity]]\n"
        )
        agent_file = tmp_path / "hyphen-unknown.md"
        agent_file.write_text(content, encoding="utf-8")

        # Act
        errors = validate_file(agent_file)

        # Assert
        e009_errors = [e for e in errors if e.error_code == "E009"]
        assert e009_errors, (
            "Expected at least one E009 error for 'custom-hyphen-field' (not in "
            "KNOWN_FRONTMATTER_KEYS). The buggy _YAML_KEY_PATTERN silently skips hyphenated "
            "keys so no E009 fires; after widening the pattern the key is matched and "
            "checked, producing E009 for an unknown hyphenated key."
        )
        hyphen_key_e009 = [e for e in e009_errors if "custom-hyphen-field" in e.message]
        assert hyphen_key_e009, (
            "The E009 error message must identify 'custom-hyphen-field' as the offending key. "
            f"Got E009 messages: {[e.message for e in e009_errors]}"
        )
