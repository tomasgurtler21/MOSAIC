"""Tests for the boundary vocabulary defined in boundary_constants.

Verifies the canonical section list, the canonical document order, the
tool-managed deployed set, and the parent mappings — as defined after the
vocabulary correction that drops `LanguagePatterns` and `CustomConstraints`
from the tool-managed set:

  - CANONICAL_SECTIONS: 6 section names (unchanged)
  - CANONICAL_ORDER: 7 document slots (ArtifactProvenance removed)
  - CANONICAL_DEPLOYED: 9 tool-managed names (ArtifactProvenance removed
    earlier; LanguagePatterns and CustomConstraints removed by this change)
  - DEPLOYED_PARENT_MAP: 9 entries
  - INJECTION_PARENT_MAP: 9 advisory entries (ArtifactProvenanceExtension
    removed, ProtocolExtension added earlier; LanguagePatterns added by
    this change with parent Capabilities. CustomConstraints is NOT
    re-catalogued here — it is deleted outright.)
  - EXPECTED_MARKER: built from CANONICAL_DEPLOYED only (no injection allowlist)
  - CANONICAL_INJECTIONS: absent — removed in Stage 2
"""
from __future__ import annotations

import pathlib
import sys

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

import boundary_constants  # noqa: E402  (used for deferred symbol access in new tests)

from boundary_constants import (  # noqa: E402
    CANONICAL_SECTIONS,
    CANONICAL_ORDER,
    CANONICAL_DEPLOYED,
    DEPLOYED_PARENT_MAP,
    INJECTION_PARENT_MAP,
    EXPECTED_MARKER,
    KNOWN_FRONTMATTER_KEYS,
    SECTION_HEADING_MAP,
    TAG_PATTERN,
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
# CANONICAL_DEPLOYED: 9 tool-managed names after the vocabulary correction
# ---------------------------------------------------------------------------


class TestCanonicalDeployed:
    """CANONICAL_DEPLOYED: 9 tool-managed boundary names.

    LanguagePatterns and CustomConstraints are removed by this change:
    LanguagePatterns moves to the advisory injection catalogue, CustomConstraints
    is deleted outright with no replacement.
    """

    def test_canonical_deployed_contains_nine_entries(self) -> None:
        assert len(CANONICAL_DEPLOYED) == 9, (
            f"Expected 9 canonical tool-managed boundary names "
            f"(LanguagePatterns and CustomConstraints removed), "
            f"got {len(CANONICAL_DEPLOYED)}: {CANONICAL_DEPLOYED}"
        )

    def test_artifact_provenance_absent_from_canonical_deployed(self) -> None:
        assert "ArtifactProvenance" not in CANONICAL_DEPLOYED, (
            "ArtifactProvenance must NOT be in CANONICAL_DEPLOYED — removed in Stage 2"
        )

    def test_language_patterns_absent_from_canonical_deployed(self) -> None:
        assert "LanguagePatterns" not in CANONICAL_DEPLOYED, (
            "LanguagePatterns must NOT be in CANONICAL_DEPLOYED — it moves to the "
            "advisory injection catalogue in this change"
        )

    def test_custom_constraints_absent_from_canonical_deployed(self) -> None:
        assert "CustomConstraints" not in CANONICAL_DEPLOYED, (
            "CustomConstraints must NOT be in CANONICAL_DEPLOYED — deleted outright "
            "with no replacement in this change"
        )

    def test_communication_protocol_is_in_canonical_deployed(self) -> None:
        assert "CommunicationProtocol" in CANONICAL_DEPLOYED

    def test_authority_hierarchy_is_in_canonical_deployed(self) -> None:
        assert "AuthorityHierarchy" in CANONICAL_DEPLOYED

    def test_closing_procedure_is_in_canonical_deployed(self) -> None:
        assert "ClosingProcedure" in CANONICAL_DEPLOYED

    def test_protocol_constraints_is_in_canonical_deployed(self) -> None:
        assert "ProtocolConstraints" in CANONICAL_DEPLOYED

    def test_error_handling_common_is_in_canonical_deployed(self) -> None:
        assert "ErrorHandlingCommon" in CANONICAL_DEPLOYED

    def test_execution_philosophy_common_is_in_canonical_deployed(self) -> None:
        assert "ExecutionPhilosophyCommon" in CANONICAL_DEPLOYED

    def test_available_workflows_is_in_canonical_deployed(self) -> None:
        assert "AvailableWorkflows" in CANONICAL_DEPLOYED

    def test_infrastructure_agents_is_in_canonical_deployed(self) -> None:
        assert "InfrastructureAgents" in CANONICAL_DEPLOYED

    def test_harness_constraints_is_in_canonical_deployed(self) -> None:
        assert "HarnessConstraints" in CANONICAL_DEPLOYED

    def test_canonical_deployed_full_sequence(self) -> None:
        """CANONICAL_DEPLOYED must equal the exact 9-name ordered tuple, matching
        the Go copy name-for-name and index-for-index (AC2.1)."""
        expected: tuple[str, ...] = (
            "CommunicationProtocol",
            "AuthorityHierarchy",
            "ClosingProcedure",
            "AvailableWorkflows",
            "InfrastructureAgents",
            "ProtocolConstraints",
            "HarnessConstraints",
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
# DEPLOYED_PARENT_MAP: 9 entries
# ---------------------------------------------------------------------------


class TestDeployedParentMap:
    """DEPLOYED_PARENT_MAP: 9 entries mapping each tool-managed name to its required parent."""

    def test_deployed_parent_map_contains_nine_entries(self) -> None:
        assert len(DEPLOYED_PARENT_MAP) == 9, (
            f"Expected 9 entries in DEPLOYED_PARENT_MAP, got {len(DEPLOYED_PARENT_MAP)}"
        )

    def test_artifact_provenance_absent_from_deployed_parent_map(self) -> None:
        assert "ArtifactProvenance" not in DEPLOYED_PARENT_MAP, (
            "ArtifactProvenance must NOT be in DEPLOYED_PARENT_MAP — removed in Stage 2"
        )

    def test_language_patterns_absent_from_deployed_parent_map(self) -> None:
        assert "LanguagePatterns" not in DEPLOYED_PARENT_MAP, (
            "LanguagePatterns must NOT be in DEPLOYED_PARENT_MAP — no longer tool-managed"
        )

    def test_custom_constraints_absent_from_deployed_parent_map(self) -> None:
        assert "CustomConstraints" not in DEPLOYED_PARENT_MAP, (
            "CustomConstraints must NOT be in DEPLOYED_PARENT_MAP — deleted outright"
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

    def test_protocol_constraints_parent_is_constraints(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("ProtocolConstraints") == "Constraints"

    def test_harness_constraints_parent_is_constraints(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("HarnessConstraints") == "Constraints"

    def test_error_handling_common_parent_is_error_handling(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("ErrorHandlingCommon") == "ErrorHandling"

    def test_execution_philosophy_common_parent_is_execution_philosophy(self) -> None:
        assert DEPLOYED_PARENT_MAP.get("ExecutionPhilosophyCommon") == "ExecutionPhilosophy"

    def test_deployed_parent_map_full_exact_values(self) -> None:
        """DEPLOYED_PARENT_MAP must have exactly these 9 entries. None = top level."""
        expected: dict[str, str | None] = {
            "CommunicationProtocol":     None,
            "AuthorityHierarchy":        "Identity",
            "ClosingProcedure":          "Identity",
            "AvailableWorkflows":        "Identity",
            "InfrastructureAgents":      "Identity",
            "ProtocolConstraints":       "Constraints",
            "HarnessConstraints":        "Constraints",
            "ErrorHandlingCommon":       "ErrorHandling",
            "ExecutionPhilosophyCommon": "ExecutionPhilosophy",
        }
        assert DEPLOYED_PARENT_MAP == expected, (
            f"DEPLOYED_PARENT_MAP mismatch.\nExpected: {expected}\nGot:      {DEPLOYED_PARENT_MAP}"
        )


# ---------------------------------------------------------------------------
# INJECTION_PARENT_MAP: 9 advisory entries after the vocabulary correction
# ---------------------------------------------------------------------------


class TestInjectionParentMap:
    """INJECTION_PARENT_MAP: 9 advisory entries. LanguagePatterns is added with
    parent Capabilities; CustomConstraints is NOT re-catalogued here — it is
    deleted outright, not moved to the injection side."""

    def test_injection_parent_map_contains_nine_entries(self) -> None:
        assert len(INJECTION_PARENT_MAP) == 9, (
            f"Expected 9 advisory entries in INJECTION_PARENT_MAP "
            f"(LanguagePatterns added), "
            f"got {len(INJECTION_PARENT_MAP)}: {list(INJECTION_PARENT_MAP)}"
        )

    def test_artifact_provenance_extension_absent_from_injection_parent_map(self) -> None:
        assert "ArtifactProvenanceExtension" not in INJECTION_PARENT_MAP, (
            "ArtifactProvenanceExtension must NOT be in INJECTION_PARENT_MAP — "
            "removed in Stage 2 along with ArtifactProvenance"
        )

    def test_custom_constraints_absent_from_injection_parent_map(self) -> None:
        assert "CustomConstraints" not in INJECTION_PARENT_MAP, (
            "CustomConstraints must NOT be in INJECTION_PARENT_MAP — it is deleted "
            "outright, not re-catalogued as an advisory injection name"
        )

    def test_language_patterns_present_in_injection_parent_map(self) -> None:
        assert "LanguagePatterns" in INJECTION_PARENT_MAP, (
            "LanguagePatterns must be in INJECTION_PARENT_MAP — added by this change"
        )

    def test_language_patterns_maps_to_capabilities(self) -> None:
        assert INJECTION_PARENT_MAP.get("LanguagePatterns") == "Capabilities", (
            "INJECTION_PARENT_MAP['LanguagePatterns'] must be 'Capabilities'"
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
        """INJECTION_PARENT_MAP must have exactly these 9 advisory entries."""
        expected: dict[str, str | None] = {
            "ProtocolExtension":      None,
            "IdentityExtension":      "Identity",
            "CodebaseContext":        "Capabilities",
            "LanguagePatterns":       "Capabilities",
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

    def test_language_patterns_absent_from_expected_marker(self) -> None:
        assert EXPECTED_MARKER.get("LanguagePatterns") is None, (
            "EXPECTED_MARKER.get('LanguagePatterns') must miss (None) now that "
            "LanguagePatterns has left CANONICAL_DEPLOYED — this is the exact "
            "mechanism that makes old-marker resolution fall to BoundaryKind.INJECTION"
        )

    def test_custom_constraints_absent_from_expected_marker(self) -> None:
        assert EXPECTED_MARKER.get("CustomConstraints") is None, (
            "EXPECTED_MARKER.get('CustomConstraints') must miss (None) now that "
            "CustomConstraints has left CANONICAL_DEPLOYED"
        )

    def test_injection_names_absent_from_expected_marker(self) -> None:
        # Injection names are open and are not in EXPECTED_MARKER.
        advisory_names = [
            "IdentityExtension",
            "CodebaseContext",
            "LanguagePatterns",
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
                f"injection names are open and are not registered"
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


# ---------------------------------------------------------------------------
# TAG_PATTERN — compound-name acceptance (Stage 1)
# ---------------------------------------------------------------------------


class TestTagPatternCompoundNameAcceptance:
    """TAG_PATTERN must match compound tag names of the form Prefix:id and Prefix:hyphen-id.

    The current name class [A-Za-z]+ excludes colons and hyphens, so compound
    names do not match.  After the Stage 1 fix the name class admits them, and
    the group layout (close, kind, name) is preserved — name yields the full
    compound string.
    """

    # --- Regression: simple names must still match -----------------------

    def test_simple_section_open_tag_still_matches(self) -> None:
        assert TAG_PATTERN.match("[[SECTION:Identity]]") is not None, (
            "Simple SECTION open tag must still match TAG_PATTERN after the name-class relaxation"
        )

    def test_simple_deployed_open_tag_still_matches(self) -> None:
        assert TAG_PATTERN.match("[[DEPLOYED:CommunicationProtocol]]") is not None, (
            "Simple DEPLOYED open tag must still match TAG_PATTERN"
        )

    def test_simple_injection_open_tag_still_matches(self) -> None:
        assert TAG_PATTERN.match("[[INJECTION:IdentityExtension]]") is not None, (
            "Simple INJECTION open tag must still match TAG_PATTERN"
        )

    def test_simple_section_close_tag_still_matches(self) -> None:
        assert TAG_PATTERN.match("[[/SECTION:Identity]]") is not None, (
            "Simple SECTION close tag must still match TAG_PATTERN"
        )

    # --- Compound-name acceptance (currently RED) ------------------------

    def test_compound_section_name_with_colon_matches(self) -> None:
        m = TAG_PATTERN.match("[[SECTION:AuthorityHierarchy:Subagent]]")
        assert m is not None, (
            "TAG_PATTERN must match a compound SECTION name like "
            "'[[SECTION:AuthorityHierarchy:Subagent]]'"
        )

    def test_compound_deployed_name_with_colon_matches(self) -> None:
        m = TAG_PATTERN.match("[[DEPLOYED:ClosingProcedure:Subagent]]")
        assert m is not None, (
            "TAG_PATTERN must match a compound DEPLOYED name like "
            "'[[DEPLOYED:ClosingProcedure:Subagent]]'"
        )

    def test_compound_injection_name_with_colon_matches(self) -> None:
        m = TAG_PATTERN.match("[[INJECTION:Workflow:quick-fix]]")
        assert m is not None, (
            "TAG_PATTERN must match a compound INJECTION name like "
            "'[[INJECTION:Workflow:quick-fix]]'"
        )

    def test_compound_close_tag_with_colon_matches(self) -> None:
        m = TAG_PATTERN.match("[[/SECTION:AuthorityHierarchy:Subagent]]")
        assert m is not None, (
            "TAG_PATTERN must match a compound close SECTION tag"
        )

    def test_hyphenated_segment_in_compound_name_matches(self) -> None:
        m = TAG_PATTERN.match("[[SECTION:Workflow:quick-fix]]")
        assert m is not None, (
            "TAG_PATTERN must match a compound name whose qualifier contains a hyphen"
        )

    def test_compound_name_yields_full_compound_string_in_name_group(self) -> None:
        m = TAG_PATTERN.match("[[SECTION:AuthorityHierarchy:Subagent]]")
        assert m is not None, "Must match before checking groups"
        assert m.group("name") == "AuthorityHierarchy:Subagent", (
            "The 'name' capture group must yield the full compound string including the colon"
        )

    def test_compound_deployed_name_yields_correct_name_group(self) -> None:
        m = TAG_PATTERN.match("[[DEPLOYED:ClosingProcedure:Subagent]]")
        assert m is not None, "Must match before checking groups"
        assert m.group("name") == "ClosingProcedure:Subagent"

    def test_compound_name_kind_group_is_preserved(self) -> None:
        m = TAG_PATTERN.match("[[DEPLOYED:ClosingProcedure:Subagent]]")
        assert m is not None, "Must match before checking groups"
        assert m.group("kind") == "DEPLOYED"

    def test_compound_close_tag_close_group_is_slash(self) -> None:
        m = TAG_PATTERN.match("[[/DEPLOYED:ClosingProcedure:Subagent]]")
        assert m is not None, "Must match before checking groups"
        assert m.group("close") == "/", "Close group must be '/' for a close tag"

    def test_compound_open_tag_close_group_is_empty(self) -> None:
        m = TAG_PATTERN.match("[[DEPLOYED:ClosingProcedure:Subagent]]")
        assert m is not None, "Must match before checking groups"
        assert m.group("close") == "", "Close group must be empty for an open tag"

    def test_all_five_bundle_section_open_tags_match(self) -> None:
        """All five DeployedSections.md block tags must match TAG_PATTERN."""
        bundle_section_tags = [
            "[[SECTION:AuthorityHierarchy:Subagent]]",
            "[[SECTION:ClosingProcedure:Subagent]]",
            "[[SECTION:ProtocolConstraints:Subagent]]",
            "[[SECTION:ErrorHandlingCommon:Subagent]]",
            "[[SECTION:ExecutionPhilosophyCommon:Subagent]]",
        ]
        for tag in bundle_section_tags:
            assert TAG_PATTERN.match(tag) is not None, (
                f"TAG_PATTERN must match bundle section open tag {tag!r}"
            )

    def test_all_five_bundle_section_close_tags_match(self) -> None:
        """All five DeployedSections.md close tags must match TAG_PATTERN."""
        bundle_section_close_tags = [
            "[[/SECTION:AuthorityHierarchy:Subagent]]",
            "[[/SECTION:ClosingProcedure:Subagent]]",
            "[[/SECTION:ProtocolConstraints:Subagent]]",
            "[[/SECTION:ErrorHandlingCommon:Subagent]]",
            "[[/SECTION:ExecutionPhilosophyCommon:Subagent]]",
        ]
        for tag in bundle_section_close_tags:
            assert TAG_PATTERN.match(tag) is not None, (
                f"TAG_PATTERN must match bundle section close tag {tag!r}"
            )


# ---------------------------------------------------------------------------
# TAG_PATTERN — rejection guards after relaxation (Stage 1)
# ---------------------------------------------------------------------------


class TestTagPatternRejectionAfterRelaxation:
    """TAG_PATTERN must still reject malformed tag lines after the name-class relaxation.

    These tests guard against over-widening: accepting compound names must not
    silently accept adjacent malformed forms.
    """

    def test_name_starting_with_digit_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[SECTION:1invalid]]") is None, (
            "A name starting with a digit must not match TAG_PATTERN"
        )

    def test_name_starting_with_hyphen_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[SECTION:-invalid]]") is None, (
            "A name starting with a hyphen must not match TAG_PATTERN"
        )

    def test_trailing_colon_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[SECTION:trailing:]]") is None, (
            "A name with a trailing colon (empty segment after it) must not match TAG_PATTERN"
        )

    def test_doubled_colon_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[SECTION::doubled]]") is None, (
            "A name starting with a colon (doubled separator) must not match TAG_PATTERN"
        )

    def test_empty_name_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[SECTION:]]") is None, (
            "An empty name must not match TAG_PATTERN"
        )

    def test_leading_space_inside_brackets_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[ SECTION:Identity]]") is None, (
            "Leading whitespace inside the brackets must prevent a match"
        )

    def test_trailing_space_inside_brackets_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[SECTION:Identity ]]") is None, (
            "Trailing whitespace inside the brackets must prevent a match"
        )

    def test_qualifier_starting_with_digit_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[SECTION:Valid:1bad]]") is None, (
            "A compound qualifier starting with a digit must not match TAG_PATTERN"
        )

    def test_qualifier_starting_with_hyphen_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[SECTION:Valid:-bad]]") is None, (
            "A compound qualifier starting with a hyphen must not match TAG_PATTERN"
        )


# ---------------------------------------------------------------------------
# tag_base_name() — base name extraction (Stage 1)
# ---------------------------------------------------------------------------


class TestTagBaseName:
    """tag_base_name() returns the portion of a tag name before the first colon.

    Used by the idempotency guard so a file carrying compound names is still
    recognised as already-transformed.  NOT used by the validator's canonical-name
    check (which is strict for agent documents).
    """

    def test_compound_name_returns_base_before_colon(self) -> None:
        # Arrange / Act
        result = boundary_constants.tag_base_name("AuthorityHierarchy:Subagent")

        # Assert
        assert result == "AuthorityHierarchy", (
            "tag_base_name must return the portion before the first colon"
        )

    def test_simple_name_returns_the_name_itself(self) -> None:
        result = boundary_constants.tag_base_name("Identity")
        assert result == "Identity", (
            "tag_base_name on a simple (non-compound) name must return the name unchanged"
        )

    def test_multi_segment_compound_name_returns_first_segment(self) -> None:
        result = boundary_constants.tag_base_name("Workflow:quick-fix")
        assert result == "Workflow", (
            "tag_base_name must return only the segment before the first colon, "
            "not the full remainder"
        )

    def test_base_name_of_all_five_bundle_block_names(self) -> None:
        """Each bundle block name's base must match an entry in CANONICAL_DEPLOYED."""
        bundle_names = [
            ("AuthorityHierarchy:Subagent",       "AuthorityHierarchy"),
            ("ClosingProcedure:Subagent",          "ClosingProcedure"),
            ("ProtocolConstraints:Subagent",       "ProtocolConstraints"),
            ("ErrorHandlingCommon:Subagent",       "ErrorHandlingCommon"),
            ("ExecutionPhilosophyCommon:Subagent", "ExecutionPhilosophyCommon"),
        ]
        for full_name, expected_base in bundle_names:
            result = boundary_constants.tag_base_name(full_name)
            assert result == expected_base, (
                f"tag_base_name({full_name!r}) must return {expected_base!r}, got {result!r}"
            )
            assert result in CANONICAL_DEPLOYED, (
                f"Base name {result!r} from {full_name!r} must be in CANONICAL_DEPLOYED "
                "so the idempotency guard can recognise it"
            )


# ---------------------------------------------------------------------------
# tag_qualifier() — qualifier extraction (Stage 1)
# ---------------------------------------------------------------------------


class TestTagQualifier:
    """tag_qualifier() returns the portion after the first colon, or None for simple names."""

    def test_compound_name_returns_qualifier_after_colon(self) -> None:
        result = boundary_constants.tag_qualifier("AuthorityHierarchy:Subagent")
        assert result == "Subagent", (
            "tag_qualifier must return the portion after the first colon"
        )

    def test_simple_name_returns_none(self) -> None:
        result = boundary_constants.tag_qualifier("Identity")
        assert result is None, (
            "tag_qualifier on a simple name must return None"
        )

    def test_hyphenated_qualifier_is_returned_intact(self) -> None:
        result = boundary_constants.tag_qualifier("Workflow:quick-fix")
        assert result == "quick-fix", (
            "tag_qualifier must preserve hyphens in the qualifier"
        )


# ---------------------------------------------------------------------------
# BUNDLE_FRONTMATTER_KEYS constant (Stage 1)
# ---------------------------------------------------------------------------


class TestBundleFrontmatterKeyConstant:
    """BUNDLE_FRONTMATTER_KEYS must be a frozenset containing the four bundle-only keys."""

    def test_bundle_frontmatter_keys_constant_exists(self) -> None:
        assert hasattr(boundary_constants, "BUNDLE_FRONTMATTER_KEYS"), (
            "boundary_constants must export BUNDLE_FRONTMATTER_KEYS after Stage 1"
        )

    def test_bundle_frontmatter_keys_is_a_frozenset(self) -> None:
        keys = boundary_constants.BUNDLE_FRONTMATTER_KEYS
        assert isinstance(keys, frozenset), (
            "BUNDLE_FRONTMATTER_KEYS must be a frozenset (immutable shared constant)"
        )

    def test_type_key_is_in_bundle_frontmatter_keys(self) -> None:
        assert "type" in boundary_constants.BUNDLE_FRONTMATTER_KEYS, (
            "'type' must be in BUNDLE_FRONTMATTER_KEYS — used in DeployedSections.md frontmatter"
        )

    def test_author_key_is_in_bundle_frontmatter_keys(self) -> None:
        assert "author" in boundary_constants.BUNDLE_FRONTMATTER_KEYS, (
            "'author' must be in BUNDLE_FRONTMATTER_KEYS"
        )

    def test_status_key_is_in_bundle_frontmatter_keys(self) -> None:
        assert "status" in boundary_constants.BUNDLE_FRONTMATTER_KEYS, (
            "'status' must be in BUNDLE_FRONTMATTER_KEYS"
        )

    def test_blocks_key_is_in_bundle_frontmatter_keys(self) -> None:
        assert "blocks" in boundary_constants.BUNDLE_FRONTMATTER_KEYS, (
            "'blocks' must be in BUNDLE_FRONTMATTER_KEYS — the list of block declarations"
        )

    def test_bundle_frontmatter_keys_has_exactly_four_members(self) -> None:
        keys = boundary_constants.BUNDLE_FRONTMATTER_KEYS
        assert len(keys) == 4, (
            f"BUNDLE_FRONTMATTER_KEYS must have exactly 4 members, got {len(keys)}: {keys}"
        )


# ---------------------------------------------------------------------------
# KNOWN_FRONTMATTER_KEYS — bundle keys included (Stage 1)
# ---------------------------------------------------------------------------


class TestKnownFrontmatterKeysBundleKeys:
    """The four bundle-document keys must be members of KNOWN_FRONTMATTER_KEYS after Stage 1.

    Without them the validator raises E009 for every key in the DeployedSections.md
    frontmatter that is not already in the allowlist.
    """

    def test_type_key_in_known_frontmatter_keys(self) -> None:
        assert "type" in KNOWN_FRONTMATTER_KEYS, (
            "'type' must be in KNOWN_FRONTMATTER_KEYS; without it the validator raises "
            "E009 for DeployedSections.md which carries 'type: bundle'"
        )

    def test_author_key_in_known_frontmatter_keys(self) -> None:
        assert "author" in KNOWN_FRONTMATTER_KEYS, (
            "'author' must be in KNOWN_FRONTMATTER_KEYS; DeployedSections.md carries 'author: MOSAIC'"
        )

    def test_status_key_in_known_frontmatter_keys(self) -> None:
        assert "status" in KNOWN_FRONTMATTER_KEYS, (
            "'status' must be in KNOWN_FRONTMATTER_KEYS; DeployedSections.md carries 'status: Draft'"
        )

    def test_blocks_key_in_known_frontmatter_keys(self) -> None:
        assert "blocks" in KNOWN_FRONTMATTER_KEYS, (
            "'blocks' must be in KNOWN_FRONTMATTER_KEYS; DeployedSections.md carries "
            "a 'blocks:' list"
        )

    def test_known_frontmatter_keys_is_still_a_frozenset(self) -> None:
        assert isinstance(KNOWN_FRONTMATTER_KEYS, frozenset), (
            "KNOWN_FRONTMATTER_KEYS must remain a frozenset after adding bundle keys; "
            "it is imported as an immutable shared constant"
        )

    def test_pre_existing_keys_not_removed_by_bundle_key_addition(self) -> None:
        """Adding bundle keys must not evict any key that was already present."""
        pre_existing = {"id", "version", "name", "description", "role", "bundle_version"}
        for key in pre_existing:
            assert key in KNOWN_FRONTMATTER_KEYS, (
                f"Pre-existing key {key!r} is missing from KNOWN_FRONTMATTER_KEYS after "
                "Stage 1 bundle key additions"
            )

    def test_bundle_frontmatter_keys_is_subset_of_known_frontmatter_keys(self) -> None:
        """Every member of BUNDLE_FRONTMATTER_KEYS must be reachable via KNOWN_FRONTMATTER_KEYS."""
        for key in boundary_constants.BUNDLE_FRONTMATTER_KEYS:
            assert key in KNOWN_FRONTMATTER_KEYS, (
                f"Bundle key {key!r} is in BUNDLE_FRONTMATTER_KEYS but not in "
                "KNOWN_FRONTMATTER_KEYS; the union property is violated"
            )


# ---------------------------------------------------------------------------
# BoundaryKind.CUSTOM enum member — vocabulary extension
# ---------------------------------------------------------------------------


class TestBoundaryKindCustomMember:
    """BoundaryKind must carry a CUSTOM member with string value 'CUSTOM'."""

    def test_custom_member_exists(self) -> None:
        assert hasattr(BoundaryKind, "CUSTOM"), (
            "BoundaryKind must have a CUSTOM member — it is not present yet"
        )

    def test_custom_member_value_is_string_CUSTOM(self) -> None:
        assert BoundaryKind.CUSTOM.value == "CUSTOM", (
            "BoundaryKind.CUSTOM.value must be the string 'CUSTOM'"
        )

    def test_four_members_total(self) -> None:
        """BoundaryKind must have exactly four members after CUSTOM is added."""
        assert len(BoundaryKind) == 4, (
            f"Expected 4 BoundaryKind members (SECTION, INJECTION, DEPLOYED, CUSTOM), "
            f"got {len(BoundaryKind)}: {list(BoundaryKind)}"
        )

    def test_existing_three_members_still_present(self) -> None:
        """SECTION, INJECTION, and DEPLOYED must still exist with their current values."""
        assert BoundaryKind.SECTION.value == "SECTION"
        assert BoundaryKind.INJECTION.value == "INJECTION"
        assert BoundaryKind.DEPLOYED.value == "DEPLOYED"

    def test_custom_is_distinct_from_injection(self) -> None:
        assert BoundaryKind.CUSTOM != BoundaryKind.INJECTION, (
            "BoundaryKind.CUSTOM must be a separate member from BoundaryKind.INJECTION"
        )

    def test_custom_is_distinct_from_deployed(self) -> None:
        assert BoundaryKind.CUSTOM != BoundaryKind.DEPLOYED, (
            "BoundaryKind.CUSTOM must be a separate member from BoundaryKind.DEPLOYED"
        )


# ---------------------------------------------------------------------------
# TAG_PATTERN — CUSTOM kind acceptance
# ---------------------------------------------------------------------------


class TestTagPatternCustomKindAcceptance:
    """TAG_PATTERN must match CUSTOM open and close tags, including compound and
    hyphenated names, once the CUSTOM alternative is added to the kind group."""

    def test_simple_custom_open_tag_matches(self) -> None:
        m = TAG_PATTERN.match("[[CUSTOM:CustomConstraints]]")
        assert m is not None, (
            "TAG_PATTERN must match a simple CUSTOM open tag like "
            "'[[CUSTOM:CustomConstraints]]'"
        )

    def test_simple_custom_close_tag_matches(self) -> None:
        m = TAG_PATTERN.match("[[/CUSTOM:CustomConstraints]]")
        assert m is not None, (
            "TAG_PATTERN must match a simple CUSTOM close tag like "
            "'[[/CUSTOM:CustomConstraints]]'"
        )

    def test_custom_tag_kind_group_is_CUSTOM(self) -> None:
        m = TAG_PATTERN.match("[[CUSTOM:CustomConstraints]]")
        assert m is not None, "Must match before checking groups"
        assert m.group("kind") == "CUSTOM", (
            "The 'kind' capture group must yield 'CUSTOM' for a custom open tag"
        )

    def test_custom_tag_name_group_yields_name(self) -> None:
        m = TAG_PATTERN.match("[[CUSTOM:CustomConstraints]]")
        assert m is not None, "Must match before checking groups"
        assert m.group("name") == "CustomConstraints"

    def test_custom_open_tag_close_group_is_empty(self) -> None:
        m = TAG_PATTERN.match("[[CUSTOM:CustomConstraints]]")
        assert m is not None, "Must match before checking groups"
        assert m.group("close") == "", (
            "Close group must be empty string for an open tag"
        )

    def test_custom_close_tag_close_group_is_slash(self) -> None:
        m = TAG_PATTERN.match("[[/CUSTOM:CustomConstraints]]")
        assert m is not None, "Must match before checking groups"
        assert m.group("close") == "/", (
            "Close group must be '/' for a close tag"
        )

    def test_compound_custom_name_with_colon_matches(self) -> None:
        m = TAG_PATTERN.match("[[CUSTOM:Project:my-feature]]")
        assert m is not None, (
            "TAG_PATTERN must match a compound CUSTOM name like "
            "'[[CUSTOM:Project:my-feature]]'"
        )

    def test_compound_custom_name_yields_full_name_group(self) -> None:
        m = TAG_PATTERN.match("[[CUSTOM:Project:my-feature]]")
        assert m is not None, "Must match before checking groups"
        assert m.group("name") == "Project:my-feature", (
            "The 'name' capture group must yield the full compound string "
            "including the internal colon"
        )

    def test_hyphenated_custom_name_matches(self) -> None:
        m = TAG_PATTERN.match("[[CUSTOM:My-component]]")
        assert m is not None, (
            "TAG_PATTERN must match a CUSTOM tag whose name contains a hyphen"
        )

    def test_compound_custom_close_tag_matches(self) -> None:
        m = TAG_PATTERN.match("[[/CUSTOM:Project:my-feature]]")
        assert m is not None, (
            "TAG_PATTERN must match a compound CUSTOM close tag"
        )

    def test_compound_custom_close_tag_kind_group(self) -> None:
        m = TAG_PATTERN.match("[[/CUSTOM:Project:my-feature]]")
        assert m is not None, "Must match before checking groups"
        assert m.group("kind") == "CUSTOM"

    def test_existing_kinds_still_match_after_custom_added(self) -> None:
        """Adding CUSTOM to the alternation must not break existing kind acceptance."""
        assert TAG_PATTERN.match("[[SECTION:Identity]]") is not None
        assert TAG_PATTERN.match("[[INJECTION:IdentityExtension]]") is not None
        assert TAG_PATTERN.match("[[DEPLOYED:CommunicationProtocol]]") is not None


class TestTagPatternCustomKindRejection:
    """TAG_PATTERN must still reject malformed CUSTOM tags after the CUSTOM
    alternative is added to the kind group."""

    def test_custom_empty_name_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[CUSTOM:]]") is None, (
            "An empty CUSTOM name must not match TAG_PATTERN"
        )

    def test_custom_name_starting_with_digit_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[CUSTOM:1invalid]]") is None, (
            "A CUSTOM name starting with a digit must not match TAG_PATTERN"
        )

    def test_custom_name_starting_with_hyphen_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[CUSTOM:-bad]]") is None, (
            "A CUSTOM name starting with a hyphen must not match TAG_PATTERN"
        )

    def test_custom_trailing_colon_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[CUSTOM:trailing:]]") is None, (
            "A CUSTOM name with a trailing colon (empty segment after it) "
            "must not match TAG_PATTERN"
        )

    def test_custom_qualifier_starting_with_digit_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[CUSTOM:Valid:1bad]]") is None, (
            "A compound CUSTOM qualifier starting with a digit must not match TAG_PATTERN"
        )

    def test_custom_qualifier_starting_with_hyphen_does_not_match(self) -> None:
        assert TAG_PATTERN.match("[[CUSTOM:Valid:-bad]]") is None, (
            "A compound CUSTOM qualifier starting with a hyphen must not match TAG_PATTERN"
        )

    def test_existing_malformed_rejections_still_hold(self) -> None:
        """Adding CUSTOM must not break existing rejection rules for other kinds."""
        assert TAG_PATTERN.match("[[SECTION:]]") is None
        assert TAG_PATTERN.match("[[INJECTION:1bad]]") is None
        assert TAG_PATTERN.match("[[DEPLOYED:-bad]]") is None
        assert TAG_PATTERN.match("[[SECTION:trailing:]]") is None


# ---------------------------------------------------------------------------
# Tag helpers — CUSTOM kind
# ---------------------------------------------------------------------------


class TestTagHelperCustomKind:
    """open_tag and close_tag must render [[CUSTOM:Name]] / [[/CUSTOM:Name]] tags
    once BoundaryKind.CUSTOM exists — they are already generic over BoundaryKind
    and need no code change beyond the enum gaining the member."""

    def test_open_tag_custom_simple_name(self) -> None:
        result = boundary_constants.open_tag(BoundaryKind.CUSTOM, "CustomConstraints")
        assert result == "[[CUSTOM:CustomConstraints]]", (
            f"open_tag(BoundaryKind.CUSTOM, 'CustomConstraints') must return "
            f"'[[CUSTOM:CustomConstraints]]', got {result!r}"
        )

    def test_close_tag_custom_simple_name(self) -> None:
        result = boundary_constants.close_tag(BoundaryKind.CUSTOM, "CustomConstraints")
        assert result == "[[/CUSTOM:CustomConstraints]]", (
            f"close_tag(BoundaryKind.CUSTOM, 'CustomConstraints') must return "
            f"'[[/CUSTOM:CustomConstraints]]', got {result!r}"
        )

    def test_open_tag_custom_compound_name(self) -> None:
        result = boundary_constants.open_tag(BoundaryKind.CUSTOM, "Project:my-feature")
        assert result == "[[CUSTOM:Project:my-feature]]"

    def test_close_tag_custom_compound_name(self) -> None:
        result = boundary_constants.close_tag(BoundaryKind.CUSTOM, "Project:my-feature")
        assert result == "[[/CUSTOM:Project:my-feature]]"

    def test_open_tag_custom_result_matches_tag_pattern(self) -> None:
        """open_tag(CUSTOM, ...) output must round-trip through TAG_PATTERN once
        CUSTOM is added to the kind alternation."""
        result = boundary_constants.open_tag(BoundaryKind.CUSTOM, "CustomConstraints")
        m = TAG_PATTERN.match(result)
        assert m is not None, (
            "The result of open_tag(BoundaryKind.CUSTOM, 'CustomConstraints') must "
            "match TAG_PATTERN once CUSTOM is in the alternation"
        )
        assert m.group("kind") == "CUSTOM"
        assert m.group("close") == ""
        assert m.group("name") == "CustomConstraints"

    def test_close_tag_custom_result_matches_tag_pattern(self) -> None:
        """close_tag(CUSTOM, ...) output must round-trip through TAG_PATTERN once
        CUSTOM is added to the kind alternation."""
        result = boundary_constants.close_tag(BoundaryKind.CUSTOM, "CustomConstraints")
        m = TAG_PATTERN.match(result)
        assert m is not None, (
            "The result of close_tag(BoundaryKind.CUSTOM, 'CustomConstraints') must "
            "match TAG_PATTERN once CUSTOM is in the alternation"
        )
        assert m.group("kind") == "CUSTOM"
        assert m.group("close") == "/"
        assert m.group("name") == "CustomConstraints"


# ---------------------------------------------------------------------------
# Stage 4: GENERIC_ONLY_FRONTMATTER_KEYS and narrowed FRONTMATTER_KEYS_BY_KIND
# ---------------------------------------------------------------------------
#
# Stage 4 adds GENERIC_ONLY_FRONTMATTER_KEYS to boundary_constants.py and
# narrows the AGENT_HARNESS allowlist so it excludes required_skills,
# recommended_tier, and tier_rationale (which are now generic-only).
#
# Tests in this class are in TDD RED phase: they fail until Stage 4 (I4.1)
# adds the GENERIC_ONLY_FRONTMATTER_KEYS set and updates the derivation of
# AGENT_COMMON_FRONTMATTER_KEYS and FRONTMATTER_KEYS_BY_KIND accordingly.


class TestGenericOnlyFrontmatterKeys:
    """GENERIC_ONLY_FRONTMATTER_KEYS must exist, contain the three deployment-pipeline-only
    fields, and properly drive the per-kind allowlist split."""

    _GENERIC_ONLY_FIELDS: tuple[str, ...] = (
        "required_skills",
        "recommended_tier",
        "tier_rationale",
    )

    # --- GENERIC_ONLY_FRONTMATTER_KEYS existence and type ---

    def test_generic_only_frontmatter_keys_exists(self) -> None:
        """GENERIC_ONLY_FRONTMATTER_KEYS must be exported from boundary_constants."""
        assert hasattr(boundary_constants, "GENERIC_ONLY_FRONTMATTER_KEYS"), (
            "boundary_constants must export GENERIC_ONLY_FRONTMATTER_KEYS after Stage 4. "
            "The attribute is absent — Stage 4 (I4.1) has not been implemented yet."
        )

    def test_generic_only_frontmatter_keys_is_a_frozenset(self) -> None:
        """GENERIC_ONLY_FRONTMATTER_KEYS must be a frozenset (immutable shared constant)."""
        keys = boundary_constants.GENERIC_ONLY_FRONTMATTER_KEYS
        assert isinstance(keys, frozenset), (
            f"GENERIC_ONLY_FRONTMATTER_KEYS must be a frozenset, got {type(keys).__name__}"
        )

    # --- GENERIC_ONLY_FRONTMATTER_KEYS membership ---

    def test_required_skills_is_in_generic_only_keys(self) -> None:
        """required_skills must be a member of GENERIC_ONLY_FRONTMATTER_KEYS."""
        assert "required_skills" in boundary_constants.GENERIC_ONLY_FRONTMATTER_KEYS, (
            "'required_skills' must be in GENERIC_ONLY_FRONTMATTER_KEYS after Stage 4; "
            "it is a deployment-pipeline-only field stripped from deployed output."
        )

    def test_recommended_tier_is_in_generic_only_keys(self) -> None:
        """recommended_tier must be a member of GENERIC_ONLY_FRONTMATTER_KEYS."""
        assert "recommended_tier" in boundary_constants.GENERIC_ONLY_FRONTMATTER_KEYS, (
            "'recommended_tier' must be in GENERIC_ONLY_FRONTMATTER_KEYS after Stage 4."
        )

    def test_tier_rationale_is_in_generic_only_keys(self) -> None:
        """tier_rationale must be a member of GENERIC_ONLY_FRONTMATTER_KEYS."""
        assert "tier_rationale" in boundary_constants.GENERIC_ONLY_FRONTMATTER_KEYS, (
            "'tier_rationale' must be in GENERIC_ONLY_FRONTMATTER_KEYS after Stage 4."
        )

    def test_generic_only_keys_contains_exactly_three_members(self) -> None:
        """GENERIC_ONLY_FRONTMATTER_KEYS must have exactly three members."""
        keys = boundary_constants.GENERIC_ONLY_FRONTMATTER_KEYS
        assert len(keys) == 3, (
            f"GENERIC_ONLY_FRONTMATTER_KEYS must have exactly 3 members "
            f"(required_skills, recommended_tier, tier_rationale), got {len(keys)}: {keys}"
        )

    # --- Disjointness invariant ---

    def test_generic_only_and_harness_only_are_disjoint(self) -> None:
        """GENERIC_ONLY_FRONTMATTER_KEYS and HARNESS_ONLY_FRONTMATTER_KEYS must be disjoint."""
        from boundary_constants import HARNESS_ONLY_FRONTMATTER_KEYS
        overlap = boundary_constants.GENERIC_ONLY_FRONTMATTER_KEYS & HARNESS_ONLY_FRONTMATTER_KEYS
        assert overlap == frozenset(), (
            f"GENERIC_ONLY_FRONTMATTER_KEYS and HARNESS_ONLY_FRONTMATTER_KEYS must be disjoint; "
            f"overlap: {overlap}"
        )

    # --- AGENT_HARNESS allowlist exclusion ---

    def test_required_skills_not_in_agent_harness_allowlist(self) -> None:
        """required_skills must NOT be in the AGENT_HARNESS per-kind allowlist."""
        from boundary_constants import FRONTMATTER_KEYS_BY_KIND
        from document_kind import DocumentKind
        harness_keys = FRONTMATTER_KEYS_BY_KIND[DocumentKind.AGENT_HARNESS]
        assert "required_skills" not in harness_keys, (
            "'required_skills' must NOT be in FRONTMATTER_KEYS_BY_KIND[AGENT_HARNESS] after "
            "Stage 4; it is a generic-only key and must not appear in the harness allowlist."
        )

    def test_recommended_tier_not_in_agent_harness_allowlist(self) -> None:
        """recommended_tier must NOT be in the AGENT_HARNESS per-kind allowlist."""
        from boundary_constants import FRONTMATTER_KEYS_BY_KIND
        from document_kind import DocumentKind
        harness_keys = FRONTMATTER_KEYS_BY_KIND[DocumentKind.AGENT_HARNESS]
        assert "recommended_tier" not in harness_keys, (
            "'recommended_tier' must NOT be in FRONTMATTER_KEYS_BY_KIND[AGENT_HARNESS] after Stage 4."
        )

    def test_tier_rationale_not_in_agent_harness_allowlist(self) -> None:
        """tier_rationale must NOT be in the AGENT_HARNESS per-kind allowlist."""
        from boundary_constants import FRONTMATTER_KEYS_BY_KIND
        from document_kind import DocumentKind
        harness_keys = FRONTMATTER_KEYS_BY_KIND[DocumentKind.AGENT_HARNESS]
        assert "tier_rationale" not in harness_keys, (
            "'tier_rationale' must NOT be in FRONTMATTER_KEYS_BY_KIND[AGENT_HARNESS] after Stage 4."
        )

    # --- AGENT_GENERIC allowlist inclusion ---

    def test_required_skills_in_agent_generic_allowlist(self) -> None:
        """required_skills must be in the AGENT_GENERIC per-kind allowlist."""
        from boundary_constants import FRONTMATTER_KEYS_BY_KIND
        from document_kind import DocumentKind
        generic_keys = FRONTMATTER_KEYS_BY_KIND[DocumentKind.AGENT_GENERIC]
        assert "required_skills" in generic_keys, (
            "'required_skills' must be in FRONTMATTER_KEYS_BY_KIND[AGENT_GENERIC] after Stage 4."
        )

    def test_recommended_tier_in_agent_generic_allowlist(self) -> None:
        """recommended_tier must be in the AGENT_GENERIC per-kind allowlist."""
        from boundary_constants import FRONTMATTER_KEYS_BY_KIND
        from document_kind import DocumentKind
        generic_keys = FRONTMATTER_KEYS_BY_KIND[DocumentKind.AGENT_GENERIC]
        assert "recommended_tier" in generic_keys, (
            "'recommended_tier' must be in FRONTMATTER_KEYS_BY_KIND[AGENT_GENERIC] after Stage 4."
        )

    def test_tier_rationale_in_agent_generic_allowlist(self) -> None:
        """tier_rationale must be in the AGENT_GENERIC per-kind allowlist."""
        from boundary_constants import FRONTMATTER_KEYS_BY_KIND
        from document_kind import DocumentKind
        generic_keys = FRONTMATTER_KEYS_BY_KIND[DocumentKind.AGENT_GENERIC]
        assert "tier_rationale" in generic_keys, (
            "'tier_rationale' must be in FRONTMATTER_KEYS_BY_KIND[AGENT_GENERIC] after Stage 4."
        )

    # --- Union invariant ---

    def test_known_frontmatter_keys_equals_union_of_per_kind_sets(self) -> None:
        """KNOWN_FRONTMATTER_KEYS must equal the union of all FRONTMATTER_KEYS_BY_KIND values.

        This invariant must hold after Stage 4 narrows the harness allowlist and adds
        GENERIC_ONLY_FRONTMATTER_KEYS. The generic entry re-adds the generic-only keys,
        so the union remains equal to KNOWN_FRONTMATTER_KEYS.
        """
        from boundary_constants import FRONTMATTER_KEYS_BY_KIND
        union = set().union(*FRONTMATTER_KEYS_BY_KIND.values())
        assert union == set(KNOWN_FRONTMATTER_KEYS), (
            "KNOWN_FRONTMATTER_KEYS must equal the union of all FRONTMATTER_KEYS_BY_KIND values. "
            f"Missing from union: {set(KNOWN_FRONTMATTER_KEYS) - union}. "
            f"Extra in union: {union - set(KNOWN_FRONTMATTER_KEYS)}."
        )

    # --- AGENT_COMMON_FRONTMATTER_KEYS still contains role ---

    def test_role_still_in_agent_common_frontmatter_keys(self) -> None:
        """AGENT_COMMON_FRONTMATTER_KEYS must still contain role after Stage 4.

        role is a common field present on both generic and harness paths. Removing
        generic-only keys from AGENT_COMMON must not accidentally remove role.
        """
        from boundary_constants import AGENT_COMMON_FRONTMATTER_KEYS
        assert "role" in AGENT_COMMON_FRONTMATTER_KEYS, (
            "'role' must still be in AGENT_COMMON_FRONTMATTER_KEYS after Stage 4 narrows it; "
            "role is a common field, not a generic-only field."
        )

    # --- Generic-only keys excluded from AGENT_COMMON ---

    def test_required_skills_not_in_agent_common_frontmatter_keys(self) -> None:
        """required_skills must NOT be in AGENT_COMMON_FRONTMATTER_KEYS after Stage 4.

        Stage 4 removes required_skills from AGENT_COMMON by subtracting GENERIC_ONLY_FRONTMATTER_KEYS.
        It is then re-added to the AGENT_GENERIC entry only.
        """
        from boundary_constants import AGENT_COMMON_FRONTMATTER_KEYS
        assert "required_skills" not in AGENT_COMMON_FRONTMATTER_KEYS, (
            "'required_skills' must NOT be in AGENT_COMMON_FRONTMATTER_KEYS after Stage 4; "
            "it is a generic-only key and must not be common to both kinds."
        )
