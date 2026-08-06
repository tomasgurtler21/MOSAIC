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


@pytest.fixture
def generic_hyphenated_key_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file whose frontmatter contains a hyphenated key (base-version: 1.0.0)."""
    return fixtures_dir / "generic_hyphenated_key_input.md"


@pytest.fixture
def generic_hyphenated_key_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Expected output: base-version line byte-identical to input; only version bumped."""
    return fixtures_dir / "generic_hyphenated_key_expected.md"


@pytest.fixture
def malformed_generic_ref_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic reference file whose frontmatter fails _parse_frontmatter.

    The frontmatter contains a bare-word line with no key: value shape, which
    triggers 'Malformed YAML line' in _parse_frontmatter. The failure mode is
    independent of the hyphenated-key regex so the Defect 3 fix cannot silently
    make this a valid file.
    """
    return fixtures_dir / "malformed_generic_ref_input.md"


@pytest.fixture
def generic_fenced_markers_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file with a fenced code block inside a canonical section.

    The fenced block contains old-style and new-style injection-marker-like text
    that must NOT be converted to boundary tags. One genuine marker outside the
    fence verifies that marker processing still works correctly.
    """
    return fixtures_dir / "generic_fenced_markers_input.md"


@pytest.fixture
def generic_fenced_markers_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Expected output for generic_fenced_markers_input.

    Fenced block content is byte-identical to the input; the genuine marker
    outside the fence is correctly converted to a boundary tag pair.
    """
    return fixtures_dir / "generic_fenced_markers_expected.md"


@pytest.fixture
def generic_fenced_provenance_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file whose Capabilities section ends with an open fence delimiter.

    The fence's closing delimiter appears after the section separator, in the outer
    loop territory.  Because _identify_sections is not fence-aware for the provenance
    heading scan, it detects the '## Artifact Provenance' line (which sits between
    the opening and closing fence delimiters, outside any canonical section's range)
    as provenance_region["start_line"].  This exercises the outer-loop provenance-
    region guard (I3.2): when the outer loop reaches that line, in_fenced_block is
    True (carried from the Capabilities inner loop), and the guard must skip the
    provenance interception.
    """
    return fixtures_dir / "generic_fenced_provenance_input.md"


@pytest.fixture
def generic_fenced_provenance_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Expected output for generic_fenced_provenance_input.

    The '## Artifact Provenance' text and its closing fence delimiter pass through
    verbatim (no ArtifactProvenanceExtension injection is emitted).  Markers in the
    Constraints section that follows are correctly converted, confirming that
    in_fenced_block is reset to False once the closing delimiter is processed.
    """
    return fixtures_dir / "generic_fenced_provenance_expected.md"


@pytest.fixture
def communication_protocol_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Old-format file with Identity H1, a '## Communication Protocol' H2 containing prose
    plus both [INJECTION: identity_extension] and [INJECTION: protocol_extension], and
    multiple following canonical sections.
    """
    return fixtures_dir / "communication_protocol_input.md"


@pytest.fixture
def communication_protocol_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Expected output for communication_protocol_input: [[DEPLOYED:CommunicationProtocol]]
    at top level between [[/SECTION:Identity]] and [[SECTION:Capabilities]], IdentityExtension
    relocated inside Identity, ProtocolExtension as an empty top-level injection pair, old prose
    discarded.
    """
    return fixtures_dir / "communication_protocol_expected.md"


@pytest.fixture
def fenced_protocol_heading_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """File with a fenced code block containing a literal '## Communication Protocol' line,
    positioned outside every canonical section's line range (outer loop territory between
    Identity's closing separator and the next canonical H2), so covered[] cannot mask the
    fence gap.  Also contains a genuine '## Communication Protocol' region elsewhere in the
    file to verify that exactly one region is detected.
    """
    return fixtures_dir / "fenced_protocol_heading_input.md"


@pytest.fixture
def fenced_protocol_heading_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Expected output for fenced_protocol_heading_input: fenced content byte-identical to
    the input (no [[DEPLOYED:CommunicationProtocol]] generated from it, no
    unclassifiable-content error); the genuine region transformed per the emission contract.
    """
    return fixtures_dir / "fenced_protocol_heading_expected.md"
