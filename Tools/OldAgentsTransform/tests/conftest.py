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
    """Already-tagged file with <ArtifactProvenance type="core"> and an empty extension injection."""
    return fixtures_dir / "provenance_old_shape_empty_ext_input.md"


@pytest.fixture
def provenance_old_shape_nonempty_ext_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Already-tagged file with <ArtifactProvenance type="core"> and non-empty extension content."""
    return fixtures_dir / "provenance_old_shape_nonempty_ext_input.md"


@pytest.fixture
def provenance_new_shape_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Already-tagged file with the new <ArtifactProvenance type="managed"> shape (idempotency case)."""
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
    """Expected output for communication_protocol_input: <CommunicationProtocol type="managed">
    at top level between </Identity> and <Capabilities type="core">, IdentityExtension
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
    the input (no <CommunicationProtocol type="managed"> generated from it, no
    unclassifiable-content error); the genuine region transformed per the emission contract.
    """
    return fixtures_dir / "fenced_protocol_heading_expected.md"


@pytest.fixture
def harness_only_transformed_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Hand-authored harness-only agent that already carries a full set of correctly paired
    canonical XML boundary tags.

    Used to verify that has_canonical_boundary_tags returns True for an already-transformed
    harness-only file, and as a reference fixture for Stage 2 and later stages.
    """
    return fixtures_dir / "harness_only_transformed_input.md"


@pytest.fixture
def harness_only_untransformed_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Hand-authored harness-only agent in legacy (pre-boundary-tag) format.

    Contains no XML boundary tags.  Used to verify that
    has_canonical_boundary_tags returns False for an untagged harness-only file.
    """
    return fixtures_dir / "harness_only_untransformed_input.md"


@pytest.fixture
def harness_only_no_version_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Harness file whose frontmatter carries transform_version but no version field.

    Used to verify that default_version() can supply a substitute value for the
    missing version field, and that the existing hard failure in _parse_frontmatter
    still fires on the current paths (Stage 1: the function exists but is not yet
    wired into transform_file).
    """
    return fixtures_dir / "harness_only_no_version_input.md"


@pytest.fixture
def harness_only_noncanonical_identity_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Harness-only agent file with a non-canonical H2 heading inside the Identity section range.

    Contains `transform_version` in frontmatter (harness signal) and a `## System Context`
    heading between the H1 Identity heading and the `---` separator before `## Capabilities`.

    On the strict generic path (strict_identity=True) this triggers an
    "unclassifiable heading" error because strict Identity ends at the first H2 of any
    kind.  On the non-strict degraded path (strict_identity=False) the heading is
    absorbed into Identity and the transform succeeds without error.

    Used to verify that the degraded path applies relaxed identity classification.
    """
    return fixtures_dir / "harness_only_noncanonical_identity_input.md"


@pytest.fixture
def generic_identity_regions_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file whose Identity section carries a Process list (with the two
    deletable closing steps) and a '### Authority Hierarchy' block.

    Used to verify that ClosingProcedure and AuthorityHierarchy are emitted and
    the superseded prose is deleted on the generic transform path.
    """
    return fixtures_dir / "generic_identity_regions_input.md"


@pytest.fixture
def generic_identity_regions_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Expected output for generic_identity_regions_input: both regions emitted, Process
    steps and Authority Hierarchy block deleted, all other content unchanged."""
    return fixtures_dir / "generic_identity_regions_expected.md"


@pytest.fixture
def harness_identity_regions_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Harness agent file (carries transform_version) whose Identity section carries a
    Process list (with the two deletable closing steps) and a '### Authority Hierarchy'
    block.

    Used to verify that ClosingProcedure and AuthorityHierarchy are emitted and the
    superseded prose is deleted on the harness transform path.
    """
    return fixtures_dir / "harness_identity_regions_input.md"


@pytest.fixture
def harness_identity_regions_generic_ref(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic reference file for harness_identity_regions_input.  Has the same Identity
    section structure so the harness merge produces content equivalent to the generic path.
    """
    return fixtures_dir / "harness_identity_regions_generic_ref.md"


@pytest.fixture
def harness_ah_merged_tag_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Harness agent file whose Identity section carries an Authority Hierarchy block
    with real prose.

    The paired generic reference (harness_ah_merged_tag_generic_ref) already carries
    the empty <AuthorityHierarchy type="managed"> / </AuthorityHierarchy> tag pair
    in place of the prose, simulating an already-transformed Catalog generic reference.
    On the harness merge path the merge inserts those empty tags between the heading
    and the prose, reproducing the merged-tag scenario where the block-extent scan
    must be transparent to the region's own tags.
    """
    return fixtures_dir / "harness_ah_merged_tag_input.md"


@pytest.fixture
def harness_ah_merged_tag_generic_ref(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic reference for harness_ah_merged_tag_input.

    Contains <AuthorityHierarchy type="managed"> / </AuthorityHierarchy> tags already
    in the Identity section (simulating an already-transformed Catalog file), so that
    the harness merge places those empty tags between the '### Authority Hierarchy'
    heading and the prose in the merged body.  The deletion scan must treat the region's
    own tags as transparent and delete the full heading-plus-prose block.
    """
    return fixtures_dir / "harness_ah_merged_tag_generic_ref.md"


@pytest.fixture
def generic_no_version_no_transform_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file with neither 'version' nor 'transform_version' in frontmatter.

    Represents a genuinely old hand-authored agent (e.g. 'Web Research.md') that
    pre-dates the version field convention.  Takes the generic transform path.
    After Stage 7, transform_file must substitute default_version() and succeed;
    before Stage 7 the generic path hard-fails with 'Missing version field'.
    """
    return fixtures_dir / "generic_no_version_no_transform_input.md"


# ---------------------------------------------------------------------------
# Stage 6 fixtures — frontmatter completeness & id reconciliation
# ---------------------------------------------------------------------------

@pytest.fixture
def s6_generic_no_tier_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file missing both recommended_tier and tier_rationale.

    Used to verify that transform_file inserts placeholders for both keys and
    records two NC_TIER_PLACEHOLDER findings on result.non_conformances.
    """
    return fixtures_dir / "s6_generic_no_tier_input.md"


@pytest.fixture
def s6_generic_with_tier_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file with both recommended_tier and tier_rationale already set.

    Used to verify that transform_file does NOT add placeholders when both keys
    are already present.
    """
    return fixtures_dir / "s6_generic_with_tier_input.md"


@pytest.fixture
def s6_harness_id10_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Harness agent file with id: 10.

    Used to verify that transform_file substitutes the generic reference's id
    when --generic-ref is supplied, and preserves id: 10 when it is not.
    """
    return fixtures_dir / "s6_harness_id10_input.md"


@pytest.fixture
def s6_generic_ref_id51(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic reference file with id: 51.

    Paired with s6_harness_id10_input to verify id reconciliation.  After
    transform with this generic ref the output must carry id: 51 (not id: 10).
    """
    return fixtures_dir / "s6_generic_ref_id51.md"


@pytest.fixture
def s6_generic_with_harness_keys_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file (no transform_version) carrying 'model' and 'mode' keys.

    Used to verify that the generic-path output strips harness-only keys.
    """
    return fixtures_dir / "s6_generic_with_harness_keys_input.md"


@pytest.fixture
def s6_harness_with_harness_keys_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Harness agent file (has transform_version) carrying 'model' and 'mode' keys.

    Used to verify that the harness-path output retains harness-only keys.
    """
    return fixtures_dir / "s6_harness_with_harness_keys_input.md"


@pytest.fixture
def s6_validator_generic_harness_key(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Already-transformed generic agent file carrying 'mode' (a harness-only key).

    Used to verify that validate_file flags E009 for a harness-only key in a
    generic-kind document after Stage 6.
    """
    return fixtures_dir / "s6_validator_generic_harness_key.md"


@pytest.fixture
def s6_validator_harness_harness_key(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Already-transformed harness agent file (has transform_version) carrying 'mode'.

    Used to verify that validate_file accepts harness-only keys in a harness-kind
    document and does not raise E009.
    """
    return fixtures_dir / "s6_validator_harness_harness_key.md"


@pytest.fixture
def s6_generic_missing_derived_keys_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file missing name, role, required_skills, recommended_tier, and tier_rationale.

    Used to verify that transform_file writes all three derivable fields (name, role,
    required_skills) and both tier placeholders when they are entirely absent from
    source frontmatter.  The body contains no skill-load directives so required_skills
    should be emitted as [].  This is the direct AC6.2 integration assertion.
    """
    return fixtures_dir / "s6_generic_missing_derived_keys_input.md"


@pytest.fixture
def s6_harness_degraded_no_tier_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Harness agent file (has transform_version) lacking tier keys, name, role,
    and required_skills.

    When passed to transform_file WITHOUT a generic_ref_path the harness-no-ref
    condition triggers the degraded write-out path.  Used to verify that tier
    placeholder insertion, harness-only key preservation, and derived-field writing
    all occur on the degraded path as well as the normal path.
    """
    return fixtures_dir / "s6_harness_degraded_no_tier_input.md"


# ---------------------------------------------------------------------------
# Stage 3 fixtures — ErrorHandlingCommon and ExecutionPhilosophyCommon regions
# ---------------------------------------------------------------------------

@pytest.fixture
def s3_generic_eh_ep_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file whose ErrorHandling section carries both the retry bullet and
    the error-code recall bullet (to be deleted), plus status-mapping bullets that must
    survive.  ExecutionPhilosophy carries all three deletable bullets with the ContextLimits
    marker positioned mid-bullet-block (the ordering-hazard case).

    Used to verify that ErrorHandlingCommon and ExecutionPhilosophyCommon are emitted on
    the generic transform path, deletions are applied, and ExecutionPhilosophyCommon
    precedes ContextLimits regardless of the marker's source position.
    """
    return fixtures_dir / "s3_generic_eh_ep_input.md"


@pytest.fixture
def s3_generic_eh_ep_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Expected output for s3_generic_eh_ep_input: both regions emitted, retry and
    error-code recall bullets deleted, status-mapping bullets kept, EP bullets deleted,
    ExecutionPhilosophyCommon preceding ContextLimits, Fix Precision bullet kept.
    """
    return fixtures_dir / "s3_generic_eh_ep_expected.md"


@pytest.fixture
def s3_harness_eh_ep_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Harness agent file (carries transform_version) matching s3_generic_eh_ep_input
    in body content.

    Used to verify that ErrorHandlingCommon and ExecutionPhilosophyCommon are emitted on
    the harness transform path with the same placement and deletion behaviour as the
    generic path.
    """
    return fixtures_dir / "s3_harness_eh_ep_input.md"


@pytest.fixture
def s3_harness_eh_ep_generic_ref(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic reference file for s3_harness_eh_ep_input.  Body content matches the
    generic input so the harness merge produces equivalent section structure.
    """
    return fixtures_dir / "s3_harness_eh_ep_generic_ref.md"


# ---------------------------------------------------------------------------
# Constraints section region fixtures (ProtocolConstraints + HarnessConstraints)
# ---------------------------------------------------------------------------

@pytest.fixture
def s4_generic_constraints_regions_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file whose Constraints section carries all five ProtocolConstraints
    bullets and both legacy [INJECTION: harness_constraints] and
    [INJECTION: custom_constraints] markers.

    Used to verify that ProtocolConstraints and HarnessConstraints are emitted
    and the superseded bullets are deleted on the generic transform path.
    """
    return fixtures_dir / "s4_generic_constraints_regions_input.md"


@pytest.fixture
def s4_harness_constraints_regions_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Harness agent file (has transform_version) whose Constraints section carries
    all five ProtocolConstraints bullets and a [INJECTION: custom_constraints] marker.

    Used to verify that ProtocolConstraints and HarnessConstraints are emitted
    and the superseded bullets are deleted on the harness transform path.
    """
    return fixtures_dir / "s4_harness_constraints_regions_input.md"


@pytest.fixture
def s4_harness_constraints_regions_generic_ref(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic reference file for s4_harness_constraints_regions_input.

    Carries the same Constraints structure (all five bullets plus both legacy
    markers) so the harness merge produces output equivalent to the generic path.
    """
    return fixtures_dir / "s4_harness_constraints_regions_generic_ref.md"


@pytest.fixture
def s4_generic_no_custom_constraints_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file whose Constraints section carries all five ProtocolConstraints
    bullets but NO harness_constraints or custom_constraints markers.

    Used to verify that HarnessConstraints falls back to the section content end
    when CustomConstraints is absent from the output.
    """
    return fixtures_dir / "s4_generic_no_custom_constraints_input.md"


@pytest.fixture
def s4_generic_drifted_bullet_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file whose Constraints section carries the known drifted wording
    of PC-bullet-5 ('Note implementation decisions for other agents but don't make them')
    instead of the canonical wording.

    Used to verify the handling of the drifted fifth bullet: either deleted by the
    tolerant strict regex, or recorded as unmatched for later reporting.
    """
    return fixtures_dir / "s4_generic_drifted_bullet_input.md"


@pytest.fixture
def s8_generic_drift_probe_only_bullet_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file whose Constraints section carries a THIRD wording of PC-bullet-5
    ('Note items relevant to other agents for future review') — distinct from both the
    canonical text and the known drifted wording covered by s4_generic_drifted_bullet_input.

    This wording trips PC-bullet-5's drift_probe (starts with "Note", mentions "agents")
    but does NOT match its strict pattern (no "do not"/"don't" clause), so the bullet is
    left in place and lands in deletions_unmatched rather than deletions_applied — the
    only shape from which Stage 8's NC_DRIFTED_BULLET can be raised, per the PC-bullet-5
    contract in ContractsDesign.md (any third wording is caught by the probe, not the
    strict pattern, which is deliberately anchored on the canonical and known-drifted
    wordings only).
    """
    return fixtures_dir / "s8_generic_drift_probe_only_bullet_input.md"


@pytest.fixture
def s4_generic_critical_tool_usage_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent file whose Constraints section carries all five ProtocolConstraints
    bullets AND a hand-written '### Critical Tool Usage Constraint' heading block.

    Used to verify that the Critical Tool Usage Constraint block is left untouched
    by the ProtocolConstraints and HarnessConstraints emission logic.
    """
    return fixtures_dir / "s4_generic_critical_tool_usage_input.md"


@pytest.fixture
def s4_harness_no_legacy_hc_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Harness agent file (has transform_version) whose Constraints section carries
    all five ProtocolConstraints bullets and a [INJECTION: custom_constraints] marker,
    but NO [INJECTION: harness_constraints] legacy marker.

    Used to verify that HarnessConstraints is emitted via the table-driven
    apply_conduct_regions path, not via the legacy marker conversion.  This isolates
    the Stage-4 HarnessConstraints placement contract on the harness path, in the same
    way that s4_generic_no_custom_constraints_input isolates it on the generic path.
    """
    return fixtures_dir / "s4_harness_no_legacy_hc_input.md"


@pytest.fixture
def stage10_json_envelope_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent whose Output Format section retains a JSON response envelope.

    Used by the Stage 10 acceptance corpus to exercise the NC_JSON_ENVELOPE
    report-coverage detection class end-to-end.
    """
    return fixtures_dir / "stage10_json_envelope_input.md"


@pytest.fixture
def stage10_no_injections_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent carrying zero injection markers anywhere in the body.

    Used by the Stage 10 acceptance corpus to exercise the NC_NO_INJECTIONS
    report-coverage detection class end-to-end.
    """
    return fixtures_dir / "stage10_no_injections_input.md"


@pytest.fixture
def stage10_harness_prose_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent whose Capabilities section carries a harness-specific H3 heading.

    Used by the Stage 10 acceptance corpus to exercise the NC_HARNESS_PROSE
    report-coverage detection class end-to-end.
    """
    return fixtures_dir / "stage10_harness_prose_input.md"


@pytest.fixture
def s4_harness_no_legacy_hc_generic_ref(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic reference file for s4_harness_no_legacy_hc_input.

    Carries the same Constraints structure (all five bullets plus [INJECTION:
    custom_constraints]) but no [INJECTION: harness_constraints] marker, so the
    harness merge produces output that must obtain HarnessConstraints solely from
    the table-driven path.
    """
    return fixtures_dir / "s4_harness_no_legacy_hc_generic_ref.md"


# ---------------------------------------------------------------------------
# OutputArtifactTemplate content move fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def art_move_generic_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent whose Capabilities section carries a '### Design Artifact Structure'
    block placed *before* the injection markers.

    Used to verify that move_artifact_structure_block relocates the block inside the
    OutputArtifactTemplate region on the generic transform path.
    """
    return fixtures_dir / "art_move_generic_input.md"


@pytest.fixture
def art_move_generic_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Expected output for art_move_generic_input: the Design Artifact Structure block
    has been moved inside the OutputArtifactTemplate region and is absent from its
    original position."""
    return fixtures_dir / "art_move_generic_expected.md"


@pytest.fixture
def art_move_block_after_marker_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic agent whose Capabilities section carries a '### Design Artifact Structure'
    block placed *after* the injection markers, testing order-independence of the move.

    The block appears after [INJECTION: output_artifact_template], so after body transform
    it sits after the already-emitted OutputArtifactTemplate region pair.  The move must
    still relocate the block inside the region tags.
    """
    return fixtures_dir / "art_move_block_after_marker_input.md"


@pytest.fixture
def art_move_harness_input(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Harness agent carrying a '### Design Artifact Structure' block under Capabilities.

    Used to verify that move_artifact_structure_block runs on the harness transform path
    and produces the same Capabilities shape as the generic path.
    """
    return fixtures_dir / "art_move_harness_input.md"


@pytest.fixture
def art_move_harness_generic_ref(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Generic reference file for art_move_harness_input.

    Carries the same Capabilities structure (with Design Artifact Structure block) as the
    harness input so the harness merge produces a body where the block is present and
    eligible for relocation.
    """
    return fixtures_dir / "art_move_harness_generic_ref.md"


@pytest.fixture
def art_move_block_after_marker_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Expected output for art_move_block_after_marker_input: the Design Artifact Structure
    block has been moved inside the OutputArtifactTemplate region, even though it originally
    appeared after the [INJECTION: output_artifact_template] marker in the source."""
    return fixtures_dir / "art_move_block_after_marker_expected.md"


@pytest.fixture
def art_move_harness_expected(fixtures_dir: pathlib.Path) -> pathlib.Path:
    """Expected output for art_move_harness_input: the Design Artifact Structure block
    has been moved inside the OutputArtifactTemplate region on the harness transform path."""
    return fixtures_dir / "art_move_harness_expected.md"
