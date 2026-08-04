"""Tests for the boundary vocabulary defined in boundary_constants.

Verifies the canonical section list, the canonical document order, the
user-owned injection set, the tool-managed deployed set, and the parent
mappings — as defined in the current vocabulary:

  - CANONICAL_SECTIONS: 7 section names (no CommunicationProtocol)
  - CANONICAL_ORDER: 8 document slots (CommunicationProtocol second as DEPLOYED)
  - CANONICAL_INJECTIONS: 8 user-owned names
  - CANONICAL_DEPLOYED: 6 tool-managed names
  - The two registries are disjoint
"""
from __future__ import annotations

import pathlib
import sys

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

from boundary_constants import (  # noqa: E402
    CANONICAL_SECTIONS,
    CANONICAL_ORDER,
    CANONICAL_INJECTIONS,
    CANONICAL_DEPLOYED,
    INJECTION_PARENT_MAP,
    DEPLOYED_PARENT_MAP,
    EXPECTED_MARKER,
    SECTION_HEADING_MAP,
    BoundaryKind,
)


# ---------------------------------------------------------------------------
# CANONICAL_SECTIONS: ArtifactProvenance at index 2
# ---------------------------------------------------------------------------


class TestCanonicalSections:
    """CANONICAL_SECTIONS: 7 section names in document order, no CommunicationProtocol."""

    def test_canonical_sections_contains_seven_entries(self) -> None:
        # CommunicationProtocol is a tool-managed DEPLOYED boundary, not a section.
        assert len(CANONICAL_SECTIONS) == 7, (
            f"Expected 7 canonical sections (CommunicationProtocol is not a section), "
            f"got {len(CANONICAL_SECTIONS)}: {CANONICAL_SECTIONS}"
        )

    def test_communication_protocol_not_in_canonical_sections(self) -> None:
        assert "CommunicationProtocol" not in CANONICAL_SECTIONS, (
            "CommunicationProtocol must NOT be in CANONICAL_SECTIONS; "
            "it is a top-level DEPLOYED boundary declared in CANONICAL_DEPLOYED"
        )

    def test_artifact_provenance_is_present_in_canonical_sections(self) -> None:
        assert "ArtifactProvenance" in CANONICAL_SECTIONS, (
            "ArtifactProvenance must be listed in CANONICAL_SECTIONS"
        )

    def test_artifact_provenance_is_at_index_1(self) -> None:
        # In the 7-entry section list, ArtifactProvenance follows Identity at index 1.
        sections = list(CANONICAL_SECTIONS)
        idx = sections.index("ArtifactProvenance") if "ArtifactProvenance" in sections else -1
        assert idx == 1, (
            f"ArtifactProvenance must be at index 1 in CANONICAL_SECTIONS, "
            f"found at index {idx}"
        )

    def test_artifact_provenance_follows_communication_protocol_in_canonical_order(self) -> None:
        """In CANONICAL_ORDER (document slots), ArtifactProvenance appears after CommunicationProtocol."""
        order = list(CANONICAL_ORDER)
        assert "ArtifactProvenance" in order, "ArtifactProvenance must be in CANONICAL_ORDER"
        assert "CommunicationProtocol" in order, "CommunicationProtocol must be in CANONICAL_ORDER"
        ap_idx = order.index("ArtifactProvenance")
        cp_idx = order.index("CommunicationProtocol")
        assert cp_idx < ap_idx, (
            f"In CANONICAL_ORDER, ArtifactProvenance (slot {ap_idx}) must follow "
            f"CommunicationProtocol (slot {cp_idx})"
        )

    def test_artifact_provenance_precedes_capabilities(self) -> None:
        """ArtifactProvenance must appear before Capabilities in CANONICAL_SECTIONS."""
        sections = list(CANONICAL_SECTIONS)
        ap_idx = sections.index("ArtifactProvenance") if "ArtifactProvenance" in sections else -1
        cap_idx = sections.index("Capabilities") if "Capabilities" in sections else -1
        assert ap_idx != -1 and cap_idx != -1, (
            "ArtifactProvenance and Capabilities must both be in CANONICAL_SECTIONS"
        )
        assert ap_idx < cap_idx, (
            f"ArtifactProvenance (index {ap_idx}) must come before "
            f"Capabilities (index {cap_idx})"
        )

    def test_canonical_sections_full_order(self) -> None:
        """CANONICAL_SECTIONS must equal the precise 7-entry ordered tuple (no CommunicationProtocol)."""
        expected: tuple[str, ...] = (
            "Identity",
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


class TestCanonicalInjections:
    """CANONICAL_INJECTIONS: 8 user-owned names only (no tool-managed names)."""

    def test_canonical_injections_contains_eight_entries(self) -> None:
        # Tool-managed names (LanguagePatterns, HarnessConstraints, etc.) are NOT members.
        assert len(CANONICAL_INJECTIONS) == 8, (
            f"Expected 8 user-owned canonical injections, "
            f"got {len(CANONICAL_INJECTIONS)}: {CANONICAL_INJECTIONS}"
        )

    def test_artifact_provenance_extension_is_present(self) -> None:
        assert "ArtifactProvenanceExtension" in CANONICAL_INJECTIONS, (
            "ArtifactProvenanceExtension must be listed in CANONICAL_INJECTIONS"
        )

    def test_tool_managed_names_not_in_canonical_injections(self) -> None:
        """Tool-managed names must not appear in the user-owned injection registry."""
        for name in ("LanguagePatterns", "HarnessConstraints", "CustomConstraints",
                     "CommunicationProtocol", "AvailableWorkflows", "InfrastructureAgents"):
            assert name not in CANONICAL_INJECTIONS, (
                f"Tool-managed name {name!r} must not be in CANONICAL_INJECTIONS"
            )

    def test_protocol_extension_not_in_canonical_injections(self) -> None:
        """ProtocolExtension was removed from the vocabulary entirely."""
        assert "ProtocolExtension" not in CANONICAL_INJECTIONS, (
            "ProtocolExtension is not canonical under any marker and must not be in CANONICAL_INJECTIONS"
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


class TestCanonicalOrder:
    """CANONICAL_ORDER: 8 document slots including CommunicationProtocol as a DEPLOYED boundary."""

    def test_canonical_order_contains_eight_slots(self) -> None:
        assert len(CANONICAL_ORDER) == 8, (
            f"Expected 8 canonical document slots, got {len(CANONICAL_ORDER)}: {CANONICAL_ORDER}"
        )

    def test_communication_protocol_is_second_slot(self) -> None:
        assert CANONICAL_ORDER[1] == "CommunicationProtocol", (
            f"CommunicationProtocol must be the second canonical document slot, "
            f"got {CANONICAL_ORDER[1]!r}"
        )

    def test_identity_is_first_slot(self) -> None:
        assert CANONICAL_ORDER[0] == "Identity", (
            f"Identity must be the first canonical document slot, got {CANONICAL_ORDER[0]!r}"
        )

    def test_canonical_order_full_sequence(self) -> None:
        """CANONICAL_ORDER must match the eight-slot document sequence exactly."""
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
        assert CANONICAL_ORDER == expected, (
            f"CANONICAL_ORDER mismatch.\nExpected: {expected}\nGot:      {CANONICAL_ORDER}"
        )

    def test_all_canonical_sections_appear_in_canonical_order(self) -> None:
        """Every canonical section name must also appear in CANONICAL_ORDER."""
        for name in CANONICAL_SECTIONS:
            assert name in CANONICAL_ORDER, (
                f"Canonical section {name!r} must appear in CANONICAL_ORDER"
            )


class TestCanonicalDeployed:
    """CANONICAL_DEPLOYED: 6 tool-managed boundary names."""

    def test_canonical_deployed_contains_six_entries(self) -> None:
        assert len(CANONICAL_DEPLOYED) == 6, (
            f"Expected 6 canonical tool-managed boundary names, "
            f"got {len(CANONICAL_DEPLOYED)}: {CANONICAL_DEPLOYED}"
        )

    def test_communication_protocol_is_in_canonical_deployed(self) -> None:
        assert "CommunicationProtocol" in CANONICAL_DEPLOYED, (
            "CommunicationProtocol must be in CANONICAL_DEPLOYED (it is a top-level DEPLOYED boundary)"
        )

    def test_language_patterns_is_in_canonical_deployed(self) -> None:
        assert "LanguagePatterns" in CANONICAL_DEPLOYED, (
            "LanguagePatterns must be in CANONICAL_DEPLOYED"
        )

    def test_harness_constraints_is_in_canonical_deployed(self) -> None:
        assert "HarnessConstraints" in CANONICAL_DEPLOYED

    def test_custom_constraints_is_in_canonical_deployed(self) -> None:
        assert "CustomConstraints" in CANONICAL_DEPLOYED

    def test_available_workflows_is_in_canonical_deployed(self) -> None:
        assert "AvailableWorkflows" in CANONICAL_DEPLOYED

    def test_infrastructure_agents_is_in_canonical_deployed(self) -> None:
        assert "InfrastructureAgents" in CANONICAL_DEPLOYED

    def test_deployed_and_injections_are_disjoint(self) -> None:
        """CANONICAL_DEPLOYED and CANONICAL_INJECTIONS must have no names in common."""
        overlap = set(CANONICAL_DEPLOYED) & set(CANONICAL_INJECTIONS)
        assert overlap == set(), (
            f"CANONICAL_DEPLOYED and CANONICAL_INJECTIONS share names: {overlap}"
        )

    def test_deployed_parent_map_covers_all_canonical_deployed(self) -> None:
        """Every tool-managed name must have an entry in DEPLOYED_PARENT_MAP."""
        for name in CANONICAL_DEPLOYED:
            assert name in DEPLOYED_PARENT_MAP, (
                f"Tool-managed name {name!r} has no entry in DEPLOYED_PARENT_MAP"
            )

    def test_communication_protocol_has_none_parent(self) -> None:
        """CommunicationProtocol must be at body top level (None parent)."""
        assert DEPLOYED_PARENT_MAP.get("CommunicationProtocol") is None, (
            "CommunicationProtocol must have a None parent in DEPLOYED_PARENT_MAP "
            "(it is a top-level boundary, not nested in any section)"
        )

    def test_language_patterns_parent_is_capabilities(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("LanguagePatterns") == "Capabilities"

    def test_harness_constraints_parent_is_constraints(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("HarnessConstraints") == "Constraints"

    def test_custom_constraints_parent_is_constraints(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("CustomConstraints") == "Constraints"

    def test_available_workflows_parent_is_identity(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("AvailableWorkflows") == "Identity"

    def test_infrastructure_agents_parent_is_identity(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("InfrastructureAgents") == "Identity"


class TestExpectedMarker:
    """EXPECTED_MARKER maps every canonical name to its required marker kind."""

    def test_user_owned_names_map_to_injection(self) -> None:
        for name in CANONICAL_INJECTIONS:
            assert EXPECTED_MARKER.get(name) == BoundaryKind.INJECTION, (
                f"User-owned name {name!r} must map to BoundaryKind.INJECTION in EXPECTED_MARKER"
            )

    def test_tool_managed_names_map_to_deployed(self) -> None:
        for name in CANONICAL_DEPLOYED:
            assert EXPECTED_MARKER.get(name) == BoundaryKind.DEPLOYED, (
                f"Tool-managed name {name!r} must map to BoundaryKind.DEPLOYED in EXPECTED_MARKER"
            )

    def test_protocol_extension_not_in_expected_marker(self) -> None:
        """ProtocolExtension was removed entirely and must not be a known marker."""
        assert "ProtocolExtension" not in EXPECTED_MARKER, (
            "ProtocolExtension must not appear in EXPECTED_MARKER"
        )


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
