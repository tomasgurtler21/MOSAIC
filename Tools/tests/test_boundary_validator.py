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

    def test_non_canonical_injection_name_returns_e004_error(self) -> None:
        # Arrange
        # [[INJECTION:Bogus]] uses a name not in CANONICAL_INJECTIONS.
        fixture = _fixture("validator_invalid_e004_injection_non_canonical.md")

        # Act
        errors = validate_file(fixture)

        # Assert
        assert len(errors) >= 1, "Expected at least one error for a non-canonical injection name"
        codes = [e.error_code for e in errors]
        assert "E004" in codes, (
            f"Expected E004 (NON_CANONICAL_NAME) for injection kind but got codes: {codes}"
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
