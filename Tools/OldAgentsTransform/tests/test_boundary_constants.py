"""Tests for the boundary vocabulary defined in boundary_constants.

Verifies the canonical section list, the canonical document order, the
tool-managed deployed set, and the parent mappings — as defined after
Stage 2 (Boundary Vocabulary Sync):

  - CANONICAL_SECTIONS: 6 section names (unchanged)
  - CANONICAL_ORDER: 7 document slots (ArtifactProvenance removed)
  - CANONICAL_DEPLOYED: 11 tool-managed names (ArtifactProvenance removed,
    5 bundle names added: AuthorityHierarchy, ClosingProcedure,
    ProtocolConstraints, ErrorHandlingCommon, ExecutionPhilosophyCommon)
  - DEPLOYED_PARENT_MAP: 11 entries
  - INJECTION_PARENT_MAP: 8 advisory entries (ArtifactProvenanceExtension
    removed, ProtocolExtension added)
  - EXPECTED_MARKER: built from CANONICAL_DEPLOYED only (no injection allowlist)
  - CANONICAL_INJECTIONS: absent — removed in Stage 2
"""
from __future__ import annotations

import pathlib
import sys

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

from boundary_constants import (  # noqa: E402
    CANONICAL_SECTIONS,
    CANONICAL_ORDER,
    CANONICAL_DEPLOYED,
    DEPLOYED_PARENT_MAP,
    INJECTION_PARENT_MAP,
    EXPECTED_MARKER,
    KNOWN_FRONTMATTER_KEYS,
    SECTION_HEADING_MAP,
    BoundaryKind,
)


# ---------------------------------------------------------------------------
# CANONICAL_SECTIONS: 6 section names, unchanged in Stage 2
# ---------------------------------------------------------------------------


class TestCanonicalSections:
    """CANONICAL_SECTIONS: 6 section names in document order, unchanged in Stage 2."""

    def test_canonical_sections_contains_six_entries(self) -> None:
        assert len(CANONICAL_SECTIONS) == 6, (
            f"Expected 6 canonical sections, got {len(CANONICAL_SECTIONS)}: {CANONICAL_SECTIONS}"
        )

    def test_communication_protocol_not_in_canonical_sections(self) -> None:
        assert "CommunicationProtocol" not in CANONICAL_SECTIONS, (
            "CommunicationProtocol must NOT be in CANONICAL_SECTIONS"
        )

    def test_artifact_provenance_not_in_canonical_sections(self) -> None:
        assert "ArtifactProvenance" not in CANONICAL_SECTIONS, (
            "ArtifactProvenance must NOT be in CANONICAL_SECTIONS — removed in Stage 2"
        )

    def test_canonical_sections_full_order(self) -> None:
        """CANONICAL_SECTIONS must equal the precise 6-entry ordered tuple."""
        expected: tuple[str, ...] = (
            "Identity",
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
# CANONICAL_ORDER: 7 document slots (ArtifactProvenance removed in Stage 2)
# ---------------------------------------------------------------------------


class TestCanonicalOrder:
    """CANONICAL_ORDER: 7 document slots after Stage 2 removes ArtifactProvenance."""

    def test_canonical_order_contains_seven_slots(self) -> None:
        assert len(CANONICAL_ORDER) == 7, (
            f"Expected 7 canonical document slots (ArtifactProvenance removed in Stage 2), "
            f"got {len(CANONICAL_ORDER)}: {CANONICAL_ORDER}"
        )

    def test_identity_is_first_slot(self) -> None:
        assert CANONICAL_ORDER[0] == "Identity", (
            f"Identity must be the first slot, got {CANONICAL_ORDER[0]!r}"
        )

    def test_communication_protocol_is_second_slot(self) -> None:
        assert CANONICAL_ORDER[1] == "CommunicationProtocol", (
            f"CommunicationProtocol must be the second slot, got {CANONICAL_ORDER[1]!r}"
        )

    def test_artifact_provenance_absent_from_canonical_order(self) -> None:
        assert "ArtifactProvenance" not in CANONICAL_ORDER, (
            "ArtifactProvenance must NOT be in CANONICAL_ORDER — removed in Stage 2"
        )

    def test_canonical_order_full_sequence(self) -> None:
        """CANONICAL_ORDER must match the seven-slot sequence exactly."""
        expected: tuple[str, ...] = (
            "Identity",
            "CommunicationProtocol",
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
        for name in CANONICAL_SECTIONS:
            assert name in CANONICAL_ORDER, (
                f"Canonical section {name!r} must appear in CANONICAL_ORDER"
            )


# ---------------------------------------------------------------------------
# CANONICAL_DEPLOYED: 11 tool-managed names after Stage 2
# ---------------------------------------------------------------------------


class TestCanonicalDeployed:
    """CANONICAL_DEPLOYED: 11 tool-managed boundary names after Stage 2."""

    def test_canonical_deployed_contains_eleven_entries(self) -> None:
        assert len(CANONICAL_DEPLOYED) == 11, (
            f"Expected 11 canonical tool-managed boundary names after Stage 2 "
            f"(ArtifactProvenance removed, 5 bundle names added), "
            f"got {len(CANONICAL_DEPLOYED)}: {CANONICAL_DEPLOYED}"
        )

    def test_artifact_provenance_absent_from_canonical_deployed(self) -> None:
        assert "ArtifactProvenance" not in CANONICAL_DEPLOYED, (
            "ArtifactProvenance must NOT be in CANONICAL_DEPLOYED — removed in Stage 2"
        )

    def test_communication_protocol_is_in_canonical_deployed(self) -> None:
        assert "CommunicationProtocol" in CANONICAL_DEPLOYED

    def test_authority_hierarchy_is_in_canonical_deployed(self) -> None:
        assert "AuthorityHierarchy" in CANONICAL_DEPLOYED, (
            "AuthorityHierarchy must be in CANONICAL_DEPLOYED — new bundle name in Stage 2"
        )

    def test_closing_procedure_is_in_canonical_deployed(self) -> None:
        assert "ClosingProcedure" in CANONICAL_DEPLOYED, (
            "ClosingProcedure must be in CANONICAL_DEPLOYED — new bundle name in Stage 2"
        )

    def test_protocol_constraints_is_in_canonical_deployed(self) -> None:
        assert "ProtocolConstraints" in CANONICAL_DEPLOYED, (
            "ProtocolConstraints must be in CANONICAL_DEPLOYED — new bundle name in Stage 2"
        )

    def test_error_handling_common_is_in_canonical_deployed(self) -> None:
        assert "ErrorHandlingCommon" in CANONICAL_DEPLOYED, (
            "ErrorHandlingCommon must be in CANONICAL_DEPLOYED — new bundle name in Stage 2"
        )

    def test_execution_philosophy_common_is_in_canonical_deployed(self) -> None:
        assert "ExecutionPhilosophyCommon" in CANONICAL_DEPLOYED, (
            "ExecutionPhilosophyCommon must be in CANONICAL_DEPLOYED — new bundle name in Stage 2"
        )

    def test_available_workflows_is_in_canonical_deployed(self) -> None:
        assert "AvailableWorkflows" in CANONICAL_DEPLOYED

    def test_infrastructure_agents_is_in_canonical_deployed(self) -> None:
        assert "InfrastructureAgents" in CANONICAL_DEPLOYED

    def test_language_patterns_is_in_canonical_deployed(self) -> None:
        assert "LanguagePatterns" in CANONICAL_DEPLOYED

    def test_harness_constraints_is_in_canonical_deployed(self) -> None:
        assert "HarnessConstraints" in CANONICAL_DEPLOYED

    def test_custom_constraints_is_in_canonical_deployed(self) -> None:
        assert "CustomConstraints" in CANONICAL_DEPLOYED

    def test_canonical_deployed_full_sequence(self) -> None:
        """CANONICAL_DEPLOYED must equal the exact 11-name ordered tuple."""
        expected: tuple[str, ...] = (
            "CommunicationProtocol",
            "AuthorityHierarchy",
            "ClosingProcedure",
            "AvailableWorkflows",
            "InfrastructureAgents",
            "LanguagePatterns",
            "ProtocolConstraints",
            "HarnessConstraints",
            "CustomConstraints",
            "ErrorHandlingCommon",
            "ExecutionPhilosophyCommon",
        )
        assert CANONICAL_DEPLOYED == expected, (
            f"CANONICAL_DEPLOYED mismatch.\nExpected: {expected}\nGot:      {CANONICAL_DEPLOYED}"
        )

    def test_deployed_parent_map_covers_all_canonical_deployed(self) -> None:
        for name in CANONICAL_DEPLOYED:
            assert name in DEPLOYED_PARENT_MAP, (
                f"Tool-managed name {name!r} has no entry in DEPLOYED_PARENT_MAP"
            )


# ---------------------------------------------------------------------------
# DEPLOYED_PARENT_MAP: 11 entries
# ---------------------------------------------------------------------------


class TestDeployedParentMap:
    """DEPLOYED_PARENT_MAP: 11 entries mapping each tool-managed name to its required parent."""

    def test_deployed_parent_map_contains_eleven_entries(self) -> None:
        assert len(DEPLOYED_PARENT_MAP) == 11, (
            f"Expected 11 entries in DEPLOYED_PARENT_MAP, got {len(DEPLOYED_PARENT_MAP)}"
        )

    def test_artifact_provenance_absent_from_deployed_parent_map(self) -> None:
        assert "ArtifactProvenance" not in DEPLOYED_PARENT_MAP, (
            "ArtifactProvenance must NOT be in DEPLOYED_PARENT_MAP — removed in Stage 2"
        )

    def test_communication_protocol_has_none_parent(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("CommunicationProtocol") is None, (
            "CommunicationProtocol must have None parent (top level)"
        )

    def test_authority_hierarchy_parent_is_identity(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("AuthorityHierarchy") == "Identity"

    def test_closing_procedure_parent_is_identity(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("ClosingProcedure") == "Identity"

    def test_available_workflows_parent_is_identity(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("AvailableWorkflows") == "Identity"

    def test_infrastructure_agents_parent_is_identity(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("InfrastructureAgents") == "Identity"

    def test_language_patterns_parent_is_capabilities(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("LanguagePatterns") == "Capabilities"

    def test_protocol_constraints_parent_is_constraints(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("ProtocolConstraints") == "Constraints"

    def test_harness_constraints_parent_is_constraints(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("HarnessConstraints") == "Constraints"

    def test_custom_constraints_parent_is_constraints(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("CustomConstraints") == "Constraints"

    def test_error_handling_common_parent_is_error_handling(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("ErrorHandlingCommon") == "ErrorHandling"

    def test_execution_philosophy_common_parent_is_execution_philosophy(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("ExecutionPhilosophyCommon") == "ExecutionPhilosophy"

    def test_deployed_parent_map_full_exact_values(self) -> None:
        """DEPLOYED_PARENT_MAP must have exactly these 11 entries. None = top level."""
        expected: dict[str, str | None] = {
            "CommunicationProtocol":     None,
            "AuthorityHierarchy":        "Identity",
            "ClosingProcedure":          "Identity",
            "AvailableWorkflows":        "Identity",
            "InfrastructureAgents":      "Identity",
            "LanguagePatterns":          "Capabilities",
            "ProtocolConstraints":       "Constraints",
            "HarnessConstraints":        "Constraints",
            "CustomConstraints":         "Constraints",
            "ErrorHandlingCommon":       "ErrorHandling",
            "ExecutionPhilosophyCommon": "ExecutionPhilosophy",
        }
        assert DEPLOYED_PARENT_MAP == expected, (
            f"DEPLOYED_PARENT_MAP mismatch.\nExpected: {expected}\nGot:      {DEPLOYED_PARENT_MAP}"
        )


# ---------------------------------------------------------------------------
# INJECTION_PARENT_MAP: 8 advisory entries in Stage 2
# ---------------------------------------------------------------------------


class TestInjectionParentMap:
    """INJECTION_PARENT_MAP: 8 advisory entries after Stage 2 changes."""

    def test_injection_parent_map_contains_eight_entries(self) -> None:
        assert len(INJECTION_PARENT_MAP) == 8, (
            f"Expected 8 advisory entries in INJECTION_PARENT_MAP, "
            f"got {len(INJECTION_PARENT_MAP)}: {list(INJECTION_PARENT_MAP)}"
        )

    def test_artifact_provenance_extension_absent_from_injection_parent_map(self) -> None:
        assert "ArtifactProvenanceExtension" not in INJECTION_PARENT_MAP, (
            "ArtifactProvenanceExtension must NOT be in INJECTION_PARENT_MAP — "
            "removed in Stage 2 along with ArtifactProvenance"
        )

    def test_protocol_extension_present_in_injection_parent_map(self) -> None:
        assert "ProtocolExtension" in INJECTION_PARENT_MAP, (
            "ProtocolExtension must be in INJECTION_PARENT_MAP — added in Stage 2"
        )

    def test_protocol_extension_maps_to_none_top_level(self) -> None:
        assert INJECTION_PARENT_MAP.get("ProtocolExtension") is None, (
            "INJECTION_PARENT_MAP['ProtocolExtension'] must be None (top-level sentinel)"
        )

    def test_identity_extension_maps_to_identity(self) -> None:
        assert INJECTION_PARENT_MAP.get("IdentityExtension") == "Identity"

    def test_codebase_context_maps_to_capabilities(self) -> None:
        assert INJECTION_PARENT_MAP.get("CodebaseContext") == "Capabilities"

    def test_output_artifact_template_maps_to_capabilities(self) -> None:
        assert INJECTION_PARENT_MAP.get("OutputArtifactTemplate") == "Capabilities"

    def test_severity_thresholds_maps_to_capabilities(self) -> None:
        assert INJECTION_PARENT_MAP.get("SeverityThresholds") == "Capabilities"

    def test_severity_definitions_maps_to_capabilities(self) -> None:
        assert INJECTION_PARENT_MAP.get("SeverityDefinitions") == "Capabilities"

    def test_error_handling_extension_maps_to_error_handling(self) -> None:
        assert INJECTION_PARENT_MAP.get("ErrorHandlingExtension") == "ErrorHandling"

    def test_context_limits_maps_to_execution_philosophy(self) -> None:
        assert INJECTION_PARENT_MAP.get("ContextLimits") == "ExecutionPhilosophy"

    def test_injection_parent_map_full_exact_values(self) -> None:
        """INJECTION_PARENT_MAP must have exactly these 8 advisory entries."""
        expected: dict[str, str | None] = {
            "ProtocolExtension":      None,
            "IdentityExtension":      "Identity",
            "CodebaseContext":        "Capabilities",
            "OutputArtifactTemplate": "Capabilities",
            "SeverityThresholds":     "Capabilities",
            "SeverityDefinitions":    "Capabilities",
            "ErrorHandlingExtension": "ErrorHandling",
            "ContextLimits":          "ExecutionPhilosophy",
        }
        assert INJECTION_PARENT_MAP == expected, (
            f"INJECTION_PARENT_MAP mismatch.\nExpected: {expected}\nGot:      {INJECTION_PARENT_MAP}"
        )


# ---------------------------------------------------------------------------
# EXPECTED_MARKER: built from CANONICAL_DEPLOYED only (no injection allowlist)
# ---------------------------------------------------------------------------


class TestExpectedMarker:
    """EXPECTED_MARKER maps only tool-managed (CANONICAL_DEPLOYED) names to BoundaryKind.DEPLOYED.

    In Stage 2 CANONICAL_INJECTIONS is removed. EXPECTED_MARKER is rebuilt from
    CANONICAL_DEPLOYED only — no name maps to BoundaryKind.INJECTION.
    """

    def test_expected_marker_contains_only_deployed_entries(self) -> None:
        for name, kind in EXPECTED_MARKER.items():
            assert kind == BoundaryKind.DEPLOYED, (
                f"EXPECTED_MARKER[{name!r}] must be BoundaryKind.DEPLOYED — "
                f"only tool-managed names are in EXPECTED_MARKER after Stage 2, "
                f"got {kind!r}"
            )

    def test_expected_marker_covers_all_canonical_deployed_names(self) -> None:
        for name in CANONICAL_DEPLOYED:
            assert EXPECTED_MARKER.get(name) == BoundaryKind.DEPLOYED, (
                f"EXPECTED_MARKER[{name!r}] must be BoundaryKind.DEPLOYED"
            )

    def test_expected_marker_entry_count_equals_canonical_deployed_count(self) -> None:
        assert len(EXPECTED_MARKER) == len(CANONICAL_DEPLOYED), (
            f"EXPECTED_MARKER must have exactly {len(CANONICAL_DEPLOYED)} entries "
            f"(one per CANONICAL_DEPLOYED name), got {len(EXPECTED_MARKER)}"
        )

    def test_artifact_provenance_absent_from_expected_marker(self) -> None:
        assert "ArtifactProvenance" not in EXPECTED_MARKER, (
            "ArtifactProvenance must NOT be in EXPECTED_MARKER — removed in Stage 2"
        )

    def test_artifact_provenance_extension_absent_from_expected_marker(self) -> None:
        assert "ArtifactProvenanceExtension" not in EXPECTED_MARKER, (
            "ArtifactProvenanceExtension must NOT be in EXPECTED_MARKER — removed in Stage 2"
        )

    def test_injection_names_absent_from_expected_marker(self) -> None:
        # Injection names are open in Stage 2 and are not in EXPECTED_MARKER.
        advisory_names = [
            "IdentityExtension",
            "CodebaseContext",
            "OutputArtifactTemplate",
            "SeverityThresholds",
            "SeverityDefinitions",
            "ErrorHandlingExtension",
            "ContextLimits",
            "ProtocolExtension",
        ]
        for name in advisory_names:
            assert name not in EXPECTED_MARKER, (
                f"Advisory injection name {name!r} must NOT be in EXPECTED_MARKER — "
                f"injection names are open in Stage 2 and are not registered"
            )

    def test_new_bundle_names_map_to_deployed_in_expected_marker(self) -> None:
        bundle_names = [
            "AuthorityHierarchy",
            "ClosingProcedure",
            "ProtocolConstraints",
            "ErrorHandlingCommon",
            "ExecutionPhilosophyCommon",
        ]
        for name in bundle_names:
            assert EXPECTED_MARKER.get(name) == BoundaryKind.DEPLOYED, (
                f"New bundle name {name!r} must map to BoundaryKind.DEPLOYED in EXPECTED_MARKER"
            )


# ---------------------------------------------------------------------------
# SECTION_HEADING_MAP: ArtifactProvenance absent
# ---------------------------------------------------------------------------


class TestSectionHeadingMap:
    """SECTION_HEADING_MAP must NOT have an entry for ArtifactProvenance."""

    def test_artifact_provenance_not_in_section_heading_map(self) -> None:
        assert "ArtifactProvenance" not in SECTION_HEADING_MAP, (
            "ArtifactProvenance must NOT be in SECTION_HEADING_MAP — removed in Stage 2"
        )


# ---------------------------------------------------------------------------
# CANONICAL_INJECTIONS absent (removed in Stage 2)
# ---------------------------------------------------------------------------


class TestCanonicalInjectionsAbsent:
    """CANONICAL_INJECTIONS is removed from boundary_constants in Stage 2."""

    def test_canonical_injections_not_importable(self) -> None:
        import boundary_constants  # noqa: PLC0415
        assert not hasattr(boundary_constants, "CANONICAL_INJECTIONS"), (
            "CANONICAL_INJECTIONS must not exist in boundary_constants — removed in Stage 2. "
            "Any code that imports or references it will fail after Stage 2 implementation."
        )


# ---------------------------------------------------------------------------
# KNOWN_FRONTMATTER_KEYS: role and bundle_version (Stage 4)
# ---------------------------------------------------------------------------


class TestKnownFrontmatterKeys_ContainsRoleAndBundleVersion:
    """KNOWN_FRONTMATTER_KEYS must include both role and bundle_version after Stage 4.

    Two new keys enter the ecosystem during this run:
      - role: declared by every migrated agent source file ("subagent" or "orchestrator").
      - bundle_version: stamped by the deployment tool into every deployed subagent file.

    Without both keys in KNOWN_FRONTMATTER_KEYS, the Python boundary validator raises
    E009 (UNEXPECTED_FRONTMATTER_KEY) for every file that carries either key.  Stage 4
    registers both so the validator accepts them.
    """

    def test_role_is_in_known_frontmatter_keys(self) -> None:
        assert "role" in KNOWN_FRONTMATTER_KEYS, (
            "'role' must be in KNOWN_FRONTMATTER_KEYS after Stage 4. "
            "Without it, the validator raises E009 for every migrated agent source file."
        )

    def test_bundle_version_is_in_known_frontmatter_keys(self) -> None:
        assert "bundle_version" in KNOWN_FRONTMATTER_KEYS, (
            "'bundle_version' must be in KNOWN_FRONTMATTER_KEYS after Stage 4. "
            "Without it, the validator raises E009 for every deployed subagent file "
            "once bundle stamping (Stage 6) ships."
        )

    def test_role_key_is_a_frozenset_member(self) -> None:
        result = "role" in KNOWN_FRONTMATTER_KEYS
        assert result is True, (
            "Membership test 'role' in KNOWN_FRONTMATTER_KEYS returned False; "
            "the key must be present in the frozenset."
        )

    def test_bundle_version_key_is_a_frozenset_member(self) -> None:
        result = "bundle_version" in KNOWN_FRONTMATTER_KEYS
        assert result is True, (
            "Membership test 'bundle_version' in KNOWN_FRONTMATTER_KEYS returned False; "
            "the key must be present in the frozenset."
        )

    def test_existing_keys_are_still_present(self) -> None:
        pre_existing = {"id", "version", "name", "description", "model", "tools"}
        for key in pre_existing:
            assert key in KNOWN_FRONTMATTER_KEYS, (
                f"Pre-existing key {key!r} is missing from KNOWN_FRONTMATTER_KEYS after "
                "Stage 4 additions; adding new keys must not remove existing ones."
            )

    def test_genuinely_unknown_key_is_not_in_known_frontmatter_keys(self) -> None:
        assert "totally_unknown_key_xyz" not in KNOWN_FRONTMATTER_KEYS, (
            "An arbitrary invented key must not appear in KNOWN_FRONTMATTER_KEYS; "
            "the set must remain bounded so E009 can still fire for genuine unknowns."
        )
