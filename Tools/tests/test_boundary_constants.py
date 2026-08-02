"""Tests for ArtifactProvenance vocabulary additions in boundary_constants.

These tests verify the new MOSAIC canonical section and injection entries
required to add the ArtifactProvenance section to the vocabulary:

  - CANONICAL_SECTIONS includes ArtifactProvenance at index 2 (8 sections total)
  - CANONICAL_INJECTIONS includes ArtifactProvenanceExtension (13 injections total)
  - INJECTION_PARENT_MAP maps ArtifactProvenanceExtension to ArtifactProvenance
  - SECTION_HEADING_MAP maps ArtifactProvenance to "## Artifact Provenance"

These tests are in TDD RED phase and will fail until boundary_constants.py is updated
to include the new vocabulary entries.
"""
from __future__ import annotations

import pathlib
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

from boundary_constants import (  # noqa: E402
    CANONICAL_SECTIONS,
    CANONICAL_INJECTIONS,
    INJECTION_PARENT_MAP,
    SECTION_HEADING_MAP,
)


# ---------------------------------------------------------------------------
# CANONICAL_SECTIONS: ArtifactProvenance at index 2
# ---------------------------------------------------------------------------


class TestCanonicalSectionsArtifactProvenance:
    """CANONICAL_SECTIONS must include ArtifactProvenance at index 2 (8 total)."""

    def test_canonical_sections_contains_eight_entries(self) -> None:
        # After adding ArtifactProvenance the total rises from 7 to 8.
        assert len(CANONICAL_SECTIONS) == 8, (
            f"Expected 8 canonical sections after ArtifactProvenance addition, "
            f"got {len(CANONICAL_SECTIONS)}: {CANONICAL_SECTIONS}"
        )

    def test_artifact_provenance_is_present_in_canonical_sections(self) -> None:
        assert "ArtifactProvenance" in CANONICAL_SECTIONS, (
            "ArtifactProvenance must be listed in CANONICAL_SECTIONS"
        )

    def test_artifact_provenance_is_at_index_2(self) -> None:
        # Arrange
        sections = list(CANONICAL_SECTIONS)

        # Act
        idx = sections.index("ArtifactProvenance") if "ArtifactProvenance" in sections else -1

        # Assert
        assert idx == 2, (
            f"ArtifactProvenance must be at index 2, found at index {idx}"
        )

    def test_artifact_provenance_follows_communication_protocol(self) -> None:
        """ArtifactProvenance must appear immediately after CommunicationProtocol."""
        sections = list(CANONICAL_SECTIONS)
        if "ArtifactProvenance" not in sections or "CommunicationProtocol" not in sections:
            pytest.skip("Required section names not present — prerequisite not met")

        ap_idx = sections.index("ArtifactProvenance")
        cp_idx = sections.index("CommunicationProtocol")

        assert cp_idx < ap_idx, (
            f"ArtifactProvenance (index {ap_idx}) must come after "
            f"CommunicationProtocol (index {cp_idx})"
        )

    def test_artifact_provenance_precedes_capabilities(self) -> None:
        """ArtifactProvenance must appear immediately before Capabilities."""
        sections = list(CANONICAL_SECTIONS)
        if "ArtifactProvenance" not in sections or "Capabilities" not in sections:
            pytest.skip("Required section names not present — prerequisite not met")

        ap_idx = sections.index("ArtifactProvenance")
        cap_idx = sections.index("Capabilities")

        assert ap_idx < cap_idx, (
            f"ArtifactProvenance (index {ap_idx}) must come before "
            f"Capabilities (index {cap_idx})"
        )

    def test_canonical_sections_full_order(self) -> None:
        """CANONICAL_SECTIONS must equal the precise 8-entry ordered tuple."""
        # This is the authoritative lockstep contract with vocabulary.go.
        expected: tuple[str, ...] = (
            "Identity",
            "CommunicationProtocol",
            "ArtifactProvenance",
            "Capabilities",
            "Constraints",
            "ErrorHandling",
            "OutputFormat",
            "ExecutionPhilosophy",
        )

        assert CANONICAL_SECTIONS == expected, (
            f"CANONICAL_SECTIONS order mismatch.\n"
            f"Expected: {expected}\n"
            f"Got:      {CANONICAL_SECTIONS}"
        )


# ---------------------------------------------------------------------------
# CANONICAL_INJECTIONS: ArtifactProvenanceExtension (13 total)
# ---------------------------------------------------------------------------


class TestCanonicalInjectionsArtifactProvenanceExtension:
    """CANONICAL_INJECTIONS must include ArtifactProvenanceExtension (13 injections total)."""

    def test_canonical_injections_contains_thirteen_entries(self) -> None:
        # After adding ArtifactProvenanceExtension the total rises from 12 to 13.
        assert len(CANONICAL_INJECTIONS) == 13, (
            f"Expected 13 canonical injections after ArtifactProvenanceExtension addition, "
            f"got {len(CANONICAL_INJECTIONS)}"
        )

    def test_artifact_provenance_extension_is_present(self) -> None:
        assert "ArtifactProvenanceExtension" in CANONICAL_INJECTIONS, (
            "ArtifactProvenanceExtension must be listed in CANONICAL_INJECTIONS"
        )


# ---------------------------------------------------------------------------
# INJECTION_PARENT_MAP: ArtifactProvenanceExtension -> ArtifactProvenance
# ---------------------------------------------------------------------------


class TestInjectionParentMapArtifactProvenanceExtension:
    """INJECTION_PARENT_MAP must map ArtifactProvenanceExtension to ArtifactProvenance."""

    def test_artifact_provenance_extension_maps_to_artifact_provenance(self) -> None:
        # Arrange
        actual = INJECTION_PARENT_MAP.get("ArtifactProvenanceExtension")

        # Assert
        assert actual == "ArtifactProvenance", (
            f"INJECTION_PARENT_MAP['ArtifactProvenanceExtension'] must be 'ArtifactProvenance', "
            f"got {actual!r}"
        )


# ---------------------------------------------------------------------------
# SECTION_HEADING_MAP: ArtifactProvenance -> "## Artifact Provenance"
# ---------------------------------------------------------------------------


class TestSectionHeadingMapArtifactProvenance:
    """SECTION_HEADING_MAP must map ArtifactProvenance to '## Artifact Provenance'."""

    def test_artifact_provenance_heading_entry_exists(self) -> None:
        assert "ArtifactProvenance" in SECTION_HEADING_MAP, (
            "ArtifactProvenance must have an entry in SECTION_HEADING_MAP"
        )

    def test_artifact_provenance_heading_value_is_correct(self) -> None:
        # Arrange
        actual = SECTION_HEADING_MAP.get("ArtifactProvenance")

        # Assert
        assert actual == "## Artifact Provenance", (
            f"SECTION_HEADING_MAP['ArtifactProvenance'] must be '## Artifact Provenance', "
            f"got {actual!r}"
        )
