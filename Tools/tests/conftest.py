"""Shared pytest fixtures for boundary tool tests."""
import pathlib

import pytest


FIXTURES_DIR = pathlib.Path(__file__).parent / "fixtures"


@pytest.fixture
def fixtures_dir() -> pathlib.Path:
    """Return the path to the test fixtures directory."""
    return FIXTURES_DIR


@pytest.fixture
def generic_standard_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "generic_standard_input.md"


@pytest.fixture
def generic_standard_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "generic_standard_expected.md"


@pytest.fixture
def generic_validation_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "generic_validation_input.md"


@pytest.fixture
def generic_validation_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "generic_validation_expected.md"


@pytest.fixture
def generic_orchestrator_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "generic_orchestrator_input.md"


@pytest.fixture
def generic_orchestrator_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "generic_orchestrator_expected.md"


@pytest.fixture
def generic_interface_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "generic_interface_input.md"


@pytest.fixture
def generic_interface_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "generic_interface_expected.md"


@pytest.fixture
def harness_codebase_agnostic_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "harness_codebase_agnostic_input.md"


@pytest.fixture
def harness_codebase_agnostic_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "harness_codebase_agnostic_expected.md"


@pytest.fixture
def harness_example_project_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "harness_example_project_input.md"


@pytest.fixture
def harness_example_project_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "harness_example_project_expected.md"


@pytest.fixture
def generic_artifact_template_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "generic_artifact_template_input.md"


@pytest.fixture
def generic_artifact_template_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "generic_artifact_template_expected.md"


@pytest.fixture
def unclassifiable_content_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "unclassifiable_content_input.md"


@pytest.fixture
def malformed_no_closing_separator_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "malformed_no_closing_separator_input.md"


@pytest.fixture
def malformed_no_version_field_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    return fixtures_dir / "malformed_no_version_field_input.md"


@pytest.fixture
def provenance_untagged_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Untagged source file with a '## Artifact Provenance' heading at canonical slot 3."""
    return fixtures_dir / "provenance_untagged_input.md"


@pytest.fixture
def provenance_old_shape_empty_ext_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Already-tagged file with [[SECTION:ArtifactProvenance]] and an empty extension injection."""
    return fixtures_dir / "provenance_old_shape_empty_ext_input.md"


@pytest.fixture
def provenance_old_shape_nonempty_ext_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Already-tagged file with [[SECTION:ArtifactProvenance]] and non-empty extension content."""
    return fixtures_dir / "provenance_old_shape_nonempty_ext_input.md"


@pytest.fixture
def provenance_new_shape_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Already-tagged file with the new [[DEPLOYED:ArtifactProvenance]] shape (idempotency case)."""
    return fixtures_dir / "provenance_new_shape_input.md"
