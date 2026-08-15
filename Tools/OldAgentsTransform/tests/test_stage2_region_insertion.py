"""Tests for region_insertion.py — fence-aware region emission and prose deletion.

Covers:
- find_section_spans: locating canonical sections with content_end anchoring
- locate_process_list_end: finding the end of the Identity Process list
- apply_conduct_regions: ClosingProcedure and AuthorityHierarchy insertion
- Deletion rules for Process steps and the Authority Hierarchy block
- Insertion/deletion guarantees (fence safety, idempotency, isolation, uniformity)
- CONDUCT_REGIONS table structure for the two Stage-2 rows

Now that Stage 8 has wired the drift-probe branch into apply_conduct_regions, a
probe-only hit on any rule — regardless of the Stage that introduced it — raises
exactly one NC_DRIFTED_BULLET finding (detail == rule_id) in addition to recording
rule_id in deletions_unmatched. This module's two probe-only tests
(CP-json-step, CP-hitl-step) assert that post-Stage-8 steady state.
"""
from __future__ import annotations

import pathlib
import sys

import pytest

_TOOLS_DIR = pathlib.Path(__file__).parent.parent
sys.path.insert(0, str(_TOOLS_DIR))

from boundary_constants import BoundaryKind  # noqa: E402
from fence import fence_mask  # noqa: E402
from non_conformance import NC_DRIFTED_BULLET  # noqa: E402
from region_insertion import (  # noqa: E402
    Anchor,
    CONDUCT_REGIONS,
    DeletionKind,
    DeletionRule,
    RegionInsertionResult,
    RegionSpec,
    SectionSpan,
    apply_conduct_regions,
    find_heading_block_end,
    find_section_spans,
    locate_process_list_end,
    _AH_BLOCK,
    _CP_HITL_STEP,
    _CP_JSON_STEP,
    _EH_RETRY,
    _EP_CONTEXT,
    _PC_BULLET_1,
    _PC_BULLET_2,
    _PC_BULLET_5,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _L(*texts: str) -> list[str]:
    """Build a kept-newline line list from bare text strings."""
    return [t if t.endswith("\n") else t + "\n" for t in texts]


def _identity_span(lines: list[str], heading: int = 0) -> SectionSpan:
    """Construct a SectionSpan for the Identity section in a small test list.

    heading is the 0-based index of the H1 heading line.
    start is heading + 1; content_end is len(lines) - 1 (last non-blank);
    end is len(lines).
    """
    # Find last non-blank line
    content_end = len(lines)
    for i in range(len(lines) - 1, heading, -1):
        if lines[i].strip():
            content_end = i + 1
            break
    return SectionSpan(
        name="Identity",
        heading_line=heading,
        start=heading + 1,
        content_end=content_end,
        end=len(lines),
    )


def _minimal_spec(name: str, supersedes: tuple[DeletionRule, ...] = ()) -> RegionSpec:
    """Construct a minimal RegionSpec for use with a custom specs= argument."""
    return RegionSpec(
        name=name,
        parent_section="Identity",
        anchor=Anchor.AFTER_PROCESS_LIST,
        fallback_anchor=Anchor.SECTION_CONTENT_END,
        supersedes=supersedes,
    )


# ---------------------------------------------------------------------------
# SectionSpan invariant
# ---------------------------------------------------------------------------

class TestSectionSpanInvariant:
    """SectionSpan raises ValueError when its ordering invariant is violated."""

    def test_valid_span_constructs_without_error(self):
        SectionSpan(name="Identity", heading_line=0, start=1, content_end=5, end=6)

    def test_start_less_than_heading_line_raises(self):
        with pytest.raises(ValueError):
            SectionSpan(name="Identity", heading_line=3, start=2, content_end=5, end=6)

    def test_content_end_less_than_start_raises(self):
        with pytest.raises(ValueError):
            SectionSpan(name="Identity", heading_line=0, start=3, content_end=2, end=6)

    def test_end_less_than_content_end_raises(self):
        with pytest.raises(ValueError):
            SectionSpan(name="Identity", heading_line=0, start=1, content_end=5, end=4)


# ---------------------------------------------------------------------------
# find_section_spans
# ---------------------------------------------------------------------------

class TestFindSectionSpans:
    """find_section_spans locates canonical sections by heading and computes content_end."""

    def test_finds_identity_by_h1_heading(self):
        lines = _L(
            "# TestAgent Agent",
            "",
            "You are the agent.",
            "",
            "---",
            "## Capabilities",
        )
        spans = find_section_spans(lines)
        assert "Identity" in spans

    def test_content_end_excludes_trailing_separator(self):
        """content_end must be the index one past the last non-blank content line,
        not the index of the '---' separator line."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "Agent prose.",
            "",
            "---",
            "## Capabilities",
        )
        spans = find_section_spans(lines)
        identity = spans["Identity"]
        # content_end should point one past "Agent prose." (index 2), not at "---" (index 4)
        assert identity.content_end == 3  # one past index 2

    def test_section_absent_from_dict_when_not_in_input(self):
        """A canonical section not present in the lines is absent from the result."""
        lines = _L("# TestAgent Agent", "", "Prose.", "")
        spans = find_section_spans(lines)
        assert "Capabilities" not in spans

    def test_heading_inside_fence_not_recognised_as_section(self):
        """A '## Capabilities' line inside a fenced block must not create a section span."""
        lines = _L(
            "# TestAgent Agent",
            "```",
            "## Capabilities",
            "```",
            "",
        )
        spans = find_section_spans(lines)
        assert "Capabilities" not in spans

    def test_span_invariant_holds_for_found_sections(self):
        """Every returned SectionSpan satisfies heading_line < start <= content_end <= end."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "Agent prose.",
            "",
            "---",
            "## Capabilities",
            "",
            "Cap prose.",
        )
        spans = find_section_spans(lines)
        for name, span in spans.items():
            assert span.heading_line < span.start, f"{name}: heading_line < start violated"
            assert span.start <= span.content_end, f"{name}: start <= content_end violated"
            assert span.content_end <= span.end, f"{name}: content_end <= end violated"


# ---------------------------------------------------------------------------
# locate_process_list_end
# ---------------------------------------------------------------------------

class TestLocateProcessListEnd:
    """locate_process_list_end returns the index one past the Process list."""

    def test_finds_end_of_numbered_process_list(self):
        """Returns the index one past the last numbered step of the Process list."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "Description.",
            "",
            "### Process",
            "1. Step one.",
            "2. Step two.",
            "",
            "### Authority Hierarchy",
        )
        identity = _identity_span(lines)
        end = locate_process_list_end(lines, identity)
        # The Process list ends after index 6 ("2. Step two."), so end == 7
        assert end == 7

    def test_returns_none_when_no_process_heading(self):
        """Returns None when the Identity section has no '### Process' heading."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "Just prose, no process list.",
            "",
        )
        identity = _identity_span(lines)
        assert locate_process_list_end(lines, identity) is None

    def test_includes_continuation_lines(self):
        """Multi-line steps (continuation lines not starting with 'N.') are included."""
        lines = _L(
            "# TestAgent Agent",
            "### Process",
            "1. Step one, which is a very long step",
            "   that continues on this line.",
            "2. Step two.",
            "",
        )
        identity = _identity_span(lines)
        end = locate_process_list_end(lines, identity)
        # Last step line is index 4 ("2. Step two."), so end == 5
        assert end == 5

    def test_fence_aware(self):
        """A '### Process' heading inside a fence must not be recognised as the list."""
        lines = _L(
            "# TestAgent Agent",
            "```",
            "### Process",
            "1. fenced step",
            "```",
            "",
        )
        identity = _identity_span(lines)
        assert locate_process_list_end(lines, identity) is None


# ---------------------------------------------------------------------------
# apply_conduct_regions — ClosingProcedure insertion placement
# ---------------------------------------------------------------------------

class TestApplyConductRegionsClosingProcedure:
    """ClosingProcedure is inserted immediately after the Process list end."""

    def _identity_with_process_list(self) -> list[str]:
        return _L(
            "# TestAgent Agent",
            "",
            "Agent prose.",
            "",
            "### Process",
            "1. Step one.",
            "2. Return ONLY output json defined by communication protocol with status",
            "",
            "Other content.",
        )

    def test_closing_procedure_open_tag_placed_after_process_list(self):
        """<ClosingProcedure type="managed"> must appear immediately after the last Process step."""
        lines = self._identity_with_process_list()
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_JSON_STEP,))
        result = apply_conduct_regions(
            lines, {"Identity": identity}, specs=(cp_spec,)
        )
        out = result.lines
        # Step 2 ("Return ONLY output json...") is deleted by CP-json-step, so
        # "Step one." is the last remaining Process step.  The ClosingProcedure
        # open tag must appear on the very next line — adjacency is the load-bearing
        # part of AC2.2 ("nothing between the Process list and ClosingProcedure").
        cp_open = '<ClosingProcedure type="managed">\n'
        step_idx = next(
            i for i, l in enumerate(out)
            if "Step one." in l
        )
        assert out[step_idx + 1].strip() == cp_open.strip(), (
            f"ClosingProcedure open tag must immediately follow the last Process step, "
            f"but found: {out[step_idx + 1]!r}"
        )

    def test_closing_procedure_appears_in_deployed_added(self):
        """ClosingProcedure must be listed in deployed_added."""
        lines = self._identity_with_process_list()
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure")
        result = apply_conduct_regions(
            lines, {"Identity": identity}, specs=(cp_spec,)
        )
        assert "ClosingProcedure" in result.deployed_added

    def test_fallback_to_section_content_end_when_no_process_list(self):
        """When no Process list exists, ClosingProcedure falls back to SECTION_CONTENT_END."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "No process list here.",
            "",
        )
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure")
        result = apply_conduct_regions(
            lines, {"Identity": identity}, specs=(cp_spec,)
        )
        assert "ClosingProcedure" in result.deployed_added

    def test_not_applied_when_identity_section_absent(self):
        """When Identity is absent from sections, ClosingProcedure is not emitted."""
        lines = _L("Some content.", "")
        cp_spec = _minimal_spec("ClosingProcedure")
        result = apply_conduct_regions(lines, {}, specs=(cp_spec,))
        assert "ClosingProcedure" not in result.deployed_added


# ---------------------------------------------------------------------------
# apply_conduct_regions — AuthorityHierarchy insertion placement
# ---------------------------------------------------------------------------

class TestApplyConductRegionsAuthorityHierarchy:
    """AuthorityHierarchy is inserted immediately after ClosingProcedure."""

    def _identity_with_both_specs_input(self) -> tuple[list[str], dict]:
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            "2. Return ONLY output json defined by communication protocol with status",
            "",
            '<ClosingProcedure type="managed">',
            "</ClosingProcedure>",
            "",
            "### Authority Hierarchy",
            "",
            "Hierarchy prose.",
            "",
        )
        identity = _identity_span(lines)
        return lines, {"Identity": identity}

    def test_authority_hierarchy_emitted_after_closing_procedure(self):
        """<AuthorityHierarchy type="managed"> must appear after </ClosingProcedure>."""
        lines, sections = self._identity_with_both_specs_input()
        ah_spec = RegionSpec(
            name="AuthorityHierarchy",
            parent_section="Identity",
            anchor=Anchor.AFTER_REGION,
            anchor_ref=(BoundaryKind.DEPLOYED, "ClosingProcedure"),
            fallback_anchor=Anchor.SECTION_CONTENT_END,
            supersedes=(_AH_BLOCK,),
        )
        result = apply_conduct_regions(lines, sections, specs=(ah_spec,))
        out = result.lines
        cp_close_idx = next(
            i for i, l in enumerate(out)
            if l.strip() == "</ClosingProcedure>"
        )
        ah_open_idx = next(
            (i for i, l in enumerate(out)
             if l.strip() == '<AuthorityHierarchy type="managed">'),
            None,
        )
        assert ah_open_idx is not None, "AuthorityHierarchy open tag not found in output"
        assert ah_open_idx > cp_close_idx, (
            "AuthorityHierarchy must appear after ClosingProcedure close tag"
        )

    def test_authority_hierarchy_appears_in_deployed_added(self):
        """AuthorityHierarchy must be listed in deployed_added."""
        lines, sections = self._identity_with_both_specs_input()
        ah_spec = RegionSpec(
            name="AuthorityHierarchy",
            parent_section="Identity",
            anchor=Anchor.AFTER_REGION,
            anchor_ref=(BoundaryKind.DEPLOYED, "ClosingProcedure"),
            fallback_anchor=Anchor.SECTION_CONTENT_END,
            supersedes=(_AH_BLOCK,),
        )
        result = apply_conduct_regions(lines, sections, specs=(ah_spec,))
        assert "AuthorityHierarchy" in result.deployed_added

    def test_both_regions_ordering_closing_before_authority(self):
        """When both specs are applied, ClosingProcedure must precede AuthorityHierarchy."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            "2. Return ONLY output json defined by communication protocol with status",
            "",
            "### Authority Hierarchy",
            "",
            "Hierarchy prose.",
            "",
        )
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_JSON_STEP,))
        ah_spec = RegionSpec(
            name="AuthorityHierarchy",
            parent_section="Identity",
            anchor=Anchor.AFTER_REGION,
            anchor_ref=(BoundaryKind.DEPLOYED, "ClosingProcedure"),
            fallback_anchor=Anchor.SECTION_CONTENT_END,
            supersedes=(_AH_BLOCK,),
        )
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec, ah_spec))
        out = result.lines
        cp_open_idx = next(i for i, l in enumerate(out) if '<ClosingProcedure type="managed">' in l)
        ah_open_idx = next(i for i, l in enumerate(out) if '<AuthorityHierarchy type="managed">' in l)
        assert cp_open_idx < ah_open_idx

    def test_deployed_added_order_matches_document_order(self):
        """deployed_added lists regions in document (emission) order."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            "2. Return ONLY output json defined by communication protocol with status",
            "",
            "### Authority Hierarchy",
            "",
            "Hierarchy prose.",
            "",
        )
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_JSON_STEP,))
        ah_spec = RegionSpec(
            name="AuthorityHierarchy",
            parent_section="Identity",
            anchor=Anchor.AFTER_REGION,
            anchor_ref=(BoundaryKind.DEPLOYED, "ClosingProcedure"),
            fallback_anchor=Anchor.SECTION_CONTENT_END,
            supersedes=(_AH_BLOCK,),
        )
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec, ah_spec))
        assert result.deployed_added.index("ClosingProcedure") < result.deployed_added.index("AuthorityHierarchy")

    def test_authority_hierarchy_fallback_anchor_when_closing_procedure_absent(self):
        """When the AFTER_REGION anchor_ref (ClosingProcedure) is absent from the file,
        AuthorityHierarchy falls back to SECTION_CONTENT_END and is still emitted.

        This exercises the named fallback_anchor field on RegionSpec, which is
        otherwise silent when both regions are always applied together in integration
        paths.
        """
        lines = _L(
            "# TestAgent Agent",
            "",
            "Content without a ClosingProcedure region.",
            "",
        )
        identity = _identity_span(lines)
        ah_spec = RegionSpec(
            name="AuthorityHierarchy",
            parent_section="Identity",
            anchor=Anchor.AFTER_REGION,
            anchor_ref=(BoundaryKind.DEPLOYED, "ClosingProcedure"),
            fallback_anchor=Anchor.SECTION_CONTENT_END,
            supersedes=(),
        )
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(ah_spec,))
        assert "AuthorityHierarchy" in result.deployed_added, (
            "AuthorityHierarchy must be emitted via fallback_anchor when "
            "ClosingProcedure (the AFTER_REGION anchor_ref) is absent from the file"
        )

    def test_not_applied_when_identity_section_absent_authority_hierarchy(self):
        """When Identity is absent from sections, AuthorityHierarchy is not emitted."""
        lines = _L("Some content.", "")
        ah_spec = RegionSpec(
            name="AuthorityHierarchy",
            parent_section="Identity",
            anchor=Anchor.SECTION_CONTENT_END,
            supersedes=(),
        )
        result = apply_conduct_regions(lines, {}, specs=(ah_spec,))
        assert "AuthorityHierarchy" not in result.deployed_added


# ---------------------------------------------------------------------------
# apply_conduct_regions — deletion rules
# ---------------------------------------------------------------------------

class TestApplyConductRegionsDeletionRules:
    """Deletion rules correctly delete, record, or leave prose depending on matches."""

    def _lines_with_json_step(self) -> tuple[list[str], dict]:
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            "2. Return ONLY output json defined by communication protocol with status",
            "",
        )
        return lines, {"Identity": _identity_span(lines)}

    def _lines_with_hitl_and_json_steps(self) -> tuple[list[str], dict]:
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            "2. When `human_in_the_loop: true`, present all output artifacts to the user for review/approval (final action before returning response)",
            "3. Return ONLY output json defined by communication protocol with status",
            "",
        )
        return lines, {"Identity": _identity_span(lines)}

    def test_cp_json_step_deleted_from_output(self):
        """The JSON-return Process step must not appear in the output lines."""
        lines, sections = self._lines_with_json_step()
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_JSON_STEP,))
        result = apply_conduct_regions(lines, sections, specs=(cp_spec,))
        out_text = "".join(result.lines)
        assert "Return ONLY output json" not in out_text

    def test_cp_json_step_in_deletions_applied(self):
        """CP-json-step must appear in deletions_applied when the step is present."""
        lines, sections = self._lines_with_json_step()
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_JSON_STEP,))
        result = apply_conduct_regions(lines, sections, specs=(cp_spec,))
        assert "CP-json-step" in result.deletions_applied

    def test_cp_hitl_step_deleted_from_output(self):
        """The HITL-review Process step must not appear in the output when present."""
        lines, sections = self._lines_with_hitl_and_json_steps()
        cp_spec = _minimal_spec(
            "ClosingProcedure", supersedes=(_CP_HITL_STEP, _CP_JSON_STEP)
        )
        result = apply_conduct_regions(lines, sections, specs=(cp_spec,))
        out_text = "".join(result.lines)
        assert "review/approval" not in out_text

    def test_cp_json_step_required_goes_to_deletions_unmatched_when_absent(self):
        """When CP-json-step (required=True) is not found, it lands in deletions_unmatched."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one that is not the JSON step.",
            "",
        )
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_JSON_STEP,))
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec,))
        assert "CP-json-step" in result.deletions_unmatched

    def test_cp_hitl_step_optional_not_in_deletions_unmatched_when_absent(self):
        """When CP-hitl-step (required=False) is not found and has no drift match,
        it must NOT appear in deletions_unmatched."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one only.",
            "",
        )
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_HITL_STEP,))
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec,))
        assert "CP-hitl-step" not in result.deletions_unmatched

    def test_cp_json_step_drift_probe_goes_to_deletions_unmatched_not_deleted(self):
        """A drifted JSON-return step (probe matches, pattern does not) is left in place,
        CP-json-step appears in deletions_unmatched, and exactly one NC_DRIFTED_BULLET
        finding with detail=='CP-json-step' is raised (the drift-probe branch wired into
        apply_conduct_regions at Stage 8 fires for every probe-only hit)."""
        drifted_step = "Respond ONLY with the JSON object per the communication protocol"
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            f"2. {drifted_step}",
            "",
        )
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_JSON_STEP,))
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec,))
        out_text = "".join(result.lines)
        # Drifted line must remain (not deleted)
        assert drifted_step in out_text
        # Rule id in unmatched
        assert "CP-json-step" in result.deletions_unmatched
        # Exactly one NC_DRIFTED_BULLET finding for this probe-only hit
        assert len(result.non_conformances) == 1
        assert result.non_conformances[0].code == NC_DRIFTED_BULLET
        assert result.non_conformances[0].detail == "CP-json-step"

    def test_ah_block_deleted_from_heading_through_next_heading(self):
        """The full ### Authority Hierarchy block (heading + prose) must be removed."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            "",
            "### Authority Hierarchy",
            "",
            "Hierarchy prose.",
            "",
            "**Why this ranking.** Because.",
            "",
            "### Other Heading",
        )
        identity = _identity_span(lines)
        ah_spec = RegionSpec(
            name="AuthorityHierarchy",
            parent_section="Identity",
            anchor=Anchor.AFTER_PROCESS_LIST,
            fallback_anchor=Anchor.SECTION_CONTENT_END,
            supersedes=(_AH_BLOCK,),
        )
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(ah_spec,))
        out_text = "".join(result.lines)
        assert "### Authority Hierarchy" not in out_text
        assert "Hierarchy prose." not in out_text
        assert "Why this ranking" not in out_text

    def test_ah_block_required_goes_to_deletions_unmatched_when_absent(self):
        """When AH-block (required=True) is not found, it lands in deletions_unmatched."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "No authority hierarchy heading here.",
            "",
        )
        identity = _identity_span(lines)
        ah_spec = RegionSpec(
            name="AuthorityHierarchy",
            parent_section="Identity",
            anchor=Anchor.SECTION_CONTENT_END,
            supersedes=(_AH_BLOCK,),
        )
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(ah_spec,))
        assert "AH-block" in result.deletions_unmatched

    def test_ah_block_not_deleted_inside_fence(self):
        """A ### Authority Hierarchy heading inside a fenced block must not be deleted.

        Lines masked by fence_mask never match for deletion rules (DeletionRule
        matching contract). A fenced heading must survive in the output.
        """
        lines = _L(
            "# TestAgent Agent",
            "",
            "```",
            "### Authority Hierarchy",
            "",
            "Authority prose.",
            "```",
            "",
        )
        identity = _identity_span(lines)
        ah_spec = RegionSpec(
            name="AuthorityHierarchy",
            parent_section="Identity",
            anchor=Anchor.SECTION_CONTENT_END,
            supersedes=(_AH_BLOCK,),
        )
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(ah_spec,))
        out_text = "".join(result.lines)
        assert "### Authority Hierarchy" in out_text, (
            "Fenced ### Authority Hierarchy heading must not be deleted by AH-block rule"
        )

    def test_cp_json_step_not_deleted_inside_fence(self):
        """A JSON-return numbered step inside a fenced block must not be deleted.

        Lines masked by fence_mask never match for deletion rules (DeletionRule
        matching contract). A fenced step must survive in the output.
        """
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            "```",
            "2. Return ONLY output json defined by communication protocol with status",
            "```",
            "",
        )
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_JSON_STEP,))
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec,))
        out_text = "".join(result.lines)
        assert "Return ONLY output json" in out_text, (
            "Fenced JSON-return step must not be deleted by CP-json-step rule"
        )

    def test_cp_hitl_step_drift_probe_goes_to_deletions_unmatched_not_deleted(self):
        """A drifted HITL step (drift_probe matches, strict pattern does not) is left
        in place and CP-hitl-step appears in deletions_unmatched — regardless of
        required=False.

        This verifies that the outcome contract's row 2 ('probe-only hit ->
        deletions_unmatched') is independent of `required`.  A naive implementation
        that adds to deletions_unmatched only when required=True would satisfy every
        other test for CP-hitl-step yet violate this contract.
        """
        drifted_step = "When human_in_the_loop is true, present your output to the user for review"
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            f"2. {drifted_step}",
            "3. Return ONLY output json defined by communication protocol with status",
            "",
        )
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_HITL_STEP, _CP_JSON_STEP))
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec,))
        out_text = "".join(result.lines)
        # Drifted line must remain in place (not deleted — the probe is permissive,
        # deleting on a permissive match would be worse than leaving it)
        assert drifted_step in out_text, "Drifted HITL step must not be deleted"
        # Rule id must appear in unmatched even though required=False
        assert "CP-hitl-step" in result.deletions_unmatched, (
            "CP-hitl-step must appear in deletions_unmatched when only drift probe "
            "matches, regardless of required=False"
        )
        # Exactly one NC_DRIFTED_BULLET finding for this probe-only hit (the drift-probe
        # branch wired into apply_conduct_regions at Stage 8 fires regardless of `required`)
        assert len(result.non_conformances) == 1
        assert result.non_conformances[0].code == NC_DRIFTED_BULLET
        assert result.non_conformances[0].detail == "CP-hitl-step"


# ---------------------------------------------------------------------------
# apply_conduct_regions — guarantees
# ---------------------------------------------------------------------------

class TestApplyConductRegionsGuarantees:
    """Invariants that must hold regardless of the input content."""

    def test_no_tag_emitted_inside_fenced_block(self):
        """No managed-region tag must appear at a line masked by fence_mask."""
        lines = _L(
            "# TestAgent Agent",
            "```python",
            "### Process",
            "1. step",
            "```",
            "",
        )
        identity = SectionSpan(name="Identity", heading_line=0, start=1, content_end=5, end=6)
        cp_spec = _minimal_spec("ClosingProcedure")
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec,))
        from fence import fence_mask
        mask = fence_mask(result.lines)
        for i, (line, is_masked) in enumerate(zip(result.lines, mask)):
            if is_masked:
                assert 'type="managed"' not in line, (
                    f"Tag emitted on fence-masked line {i}: {line!r}"
                )

    def test_idempotent_when_closing_procedure_already_present(self):
        """ClosingProcedure must not be re-emitted when already in the input."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            '<ClosingProcedure type="managed">',
            "</ClosingProcedure>",
            "",
        )
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure")
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec,))
        cp_count = sum(1 for l in result.lines if l.strip() == '<ClosingProcedure type="managed">')
        assert cp_count == 1, "ClosingProcedure must appear exactly once"
        assert "ClosingProcedure" not in result.deployed_added

    def test_non_conformances_empty_at_this_stage(self):
        """non_conformances must be an empty list — drift detection is wired later."""
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            "2. Return ONLY output json defined by communication protocol with status",
            "",
        )
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_JSON_STEP,))
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec,))
        assert result.non_conformances == []

    def test_emitted_region_is_exactly_two_lines(self):
        """A deployed region must be emitted as exactly '<Name type="managed">\\n' then
        '</Name>\\n', with no blank line between them."""
        lines = _L("# TestAgent Agent", "", "Prose.", "")
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure")
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec,))
        out = result.lines
        cp_open_idx = next(i for i, l in enumerate(out) if '<ClosingProcedure type="managed">' in l)
        assert out[cp_open_idx].strip() == '<ClosingProcedure type="managed">'
        assert out[cp_open_idx + 1].strip() == "</ClosingProcedure>"

    def test_lines_outside_touched_regions_byte_identical(self):
        """Lines not involved in insertion or deletion must be byte-identical in the output."""
        sentinel = "SENTINEL_PRESERVATION_CHECK\n"
        lines = _L(
            "# TestAgent Agent",
            sentinel.rstrip("\n"),
            "",
            "### Process",
            "1. Return ONLY output json defined by communication protocol with status",
            "",
        )
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_JSON_STEP,))
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec,))
        assert sentinel in result.lines, "Untouched line must be byte-identical in output"

    def test_idempotent_when_authority_hierarchy_already_present(self):
        """AuthorityHierarchy must not be re-emitted when already present in the input."""
        lines = _L(
            "# TestAgent Agent",
            "",
            '<AuthorityHierarchy type="managed">',
            "</AuthorityHierarchy>",
            "",
        )
        identity = _identity_span(lines)
        ah_spec = RegionSpec(
            name="AuthorityHierarchy",
            parent_section="Identity",
            anchor=Anchor.SECTION_CONTENT_END,
            fallback_anchor=Anchor.SECTION_CONTENT_END,
            supersedes=(),
        )
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(ah_spec,))
        ah_count = sum(
            1 for l in result.lines if l.strip() == '<AuthorityHierarchy type="managed">'
        )
        assert ah_count == 1, "AuthorityHierarchy must appear exactly once"
        assert "AuthorityHierarchy" not in result.deployed_added

    def test_no_double_blank_line_after_deletion(self):
        """When a deletion target is preceded and followed by a blank line, exactly one
        blank line remains — the blank-line-collapse guarantee prevents a double blank
        line from being introduced.
        """
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            "",
            "2. Return ONLY output json defined by communication protocol with status",
            "",
            "Other content.",
        )
        identity = _identity_span(lines)
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_JSON_STEP,))
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(cp_spec,))
        out_text = "".join(result.lines)
        assert "\n\n\n" not in out_text, (
            "Deletion of a step surrounded by blank lines must not leave a double "
            "blank line; exactly one blank line must remain"
        )


# ---------------------------------------------------------------------------
# CONDUCT_REGIONS table
# ---------------------------------------------------------------------------

class TestConductRegionsTable:
    """CONDUCT_REGIONS table must declare the two Stage-2 rows correctly."""

    def test_table_contains_closing_procedure(self):
        """CONDUCT_REGIONS must have a ClosingProcedure spec."""
        assert any(s.name == "ClosingProcedure" for s in CONDUCT_REGIONS), (
            "No ClosingProcedure row found in CONDUCT_REGIONS"
        )

    def test_table_contains_authority_hierarchy(self):
        """CONDUCT_REGIONS must have an AuthorityHierarchy spec."""
        assert any(s.name == "AuthorityHierarchy" for s in CONDUCT_REGIONS), (
            "No AuthorityHierarchy row found in CONDUCT_REGIONS"
        )

    def test_closing_procedure_precedes_authority_hierarchy_in_table(self):
        """ClosingProcedure must appear before AuthorityHierarchy in CONDUCT_REGIONS."""
        names = [s.name for s in CONDUCT_REGIONS]
        assert names.index("ClosingProcedure") < names.index("AuthorityHierarchy")

    def test_closing_procedure_parent_section_is_identity(self):
        """ClosingProcedure's parent_section must be 'Identity'."""
        cp = next(s for s in CONDUCT_REGIONS if s.name == "ClosingProcedure")
        assert cp.parent_section == "Identity"

    def test_authority_hierarchy_parent_section_is_identity(self):
        """AuthorityHierarchy's parent_section must be 'Identity'."""
        ah = next(s for s in CONDUCT_REGIONS if s.name == "AuthorityHierarchy")
        assert ah.parent_section == "Identity"

    def test_all_parent_sections_agree_with_deployed_parent_map(self):
        """Every spec's parent_section must agree with DEPLOYED_PARENT_MAP."""
        from boundary_constants import DEPLOYED_PARENT_MAP
        for spec in CONDUCT_REGIONS:
            expected_parent = DEPLOYED_PARENT_MAP.get(spec.name)
            assert spec.parent_section == expected_parent, (
                f"{spec.name}: parent_section={spec.parent_section!r} "
                f"but DEPLOYED_PARENT_MAP says {expected_parent!r}"
            )

    def test_closing_procedure_supersedes_cp_json_step(self):
        """ClosingProcedure's supersedes tuple must include CP-json-step."""
        cp = next(s for s in CONDUCT_REGIONS if s.name == "ClosingProcedure")
        rule_ids = {r.rule_id for r in cp.supersedes}
        assert "CP-json-step" in rule_ids

    def test_authority_hierarchy_supersedes_ah_block(self):
        """AuthorityHierarchy's supersedes tuple must include AH-block."""
        ah = next(s for s in CONDUCT_REGIONS if s.name == "AuthorityHierarchy")
        rule_ids = {r.rule_id for r in ah.supersedes}
        assert "AH-block" in rule_ids


# ---------------------------------------------------------------------------
# Helpers shared by the orphan-deletion test classes below
# ---------------------------------------------------------------------------

def _eh_span(lines: list[str], heading: int = 0) -> SectionSpan:
    """Build a SectionSpan for an ErrorHandling section."""
    content_end = len(lines)
    for i in range(len(lines) - 1, heading, -1):
        if lines[i].strip():
            content_end = i + 1
            break
    return SectionSpan(
        name="ErrorHandling",
        heading_line=heading,
        start=heading + 1,
        content_end=content_end,
        end=len(lines),
    )


def _ep_span(lines: list[str], heading: int = 0) -> SectionSpan:
    """Build a SectionSpan for an ExecutionPhilosophy section."""
    content_end = len(lines)
    for i in range(len(lines) - 1, heading, -1):
        if lines[i].strip():
            content_end = i + 1
            break
    return SectionSpan(
        name="ExecutionPhilosophy",
        heading_line=heading,
        start=heading + 1,
        content_end=content_end,
        end=len(lines),
    )


def _constraints_span(lines: list[str], heading: int = 0) -> SectionSpan:
    """Build a SectionSpan for a Constraints section."""
    content_end = len(lines)
    for i in range(len(lines) - 1, heading, -1):
        if lines[i].strip():
            content_end = i + 1
            break
    return SectionSpan(
        name="Constraints",
        heading_line=heading,
        start=heading + 1,
        content_end=content_end,
        end=len(lines),
    )


# ---------------------------------------------------------------------------
# find_heading_block_end — own_region transparency
# ---------------------------------------------------------------------------

class TestFindHeadingBlockEndOwnRegion:
    """find_heading_block_end(own_region=...) treats the named region's own tags
    as transparent while keeping every other region's tags as opaque stop
    boundaries.  These tests pin both the fix (own-tag transparency) and the
    surviving safety guard (foreign-tag opacity)."""

    def test_own_region_open_tag_is_transparent(self):
        """When own_region matches, the open tag of that region does not stop the scan.

        The block extent must continue past the own-region open tag and include the
        prose that follows it.
        """
        lines = _L(
            "# Agent",
            "### Authority Hierarchy",
            "",
            '<AuthorityHierarchy type="managed">',
            "</AuthorityHierarchy>",
            "",
            "Four sources issue you instructions.",
            "**Why this ranking.** Because.",
            "",
            "### Other Heading",
        )
        mask = fence_mask(list(lines))
        end = find_heading_block_end(lines, 1, mask, own_region="AuthorityHierarchy")
        # Must reach "### Other Heading" (index 9), not stop at the AH open tag (index 3)
        assert end == 9, (
            f"Block extent stopped at index {end} rather than reaching the next heading "
            "at index 9; own-region tag must be transparent to the scan"
        )

    def test_own_region_close_tag_is_transparent(self):
        """Both the open and close tags of own_region are transparent to the scan."""
        lines = _L(
            "# Agent",
            "### Authority Hierarchy",
            "",
            '<AuthorityHierarchy type="managed">',
            "</AuthorityHierarchy>",
            "Prose below the close tag.",
            "",
            "### Next Heading",
        )
        mask = fence_mask(list(lines))
        end = find_heading_block_end(lines, 1, mask, own_region="AuthorityHierarchy")
        # Must reach "### Next Heading" (index 7)
        assert end == 7, (
            f"Own-region close tag at index 4 must be transparent; expected end=7 got {end}"
        )

    def test_full_block_deleted_when_own_tags_interrupt_heading_block(self):
        """apply_conduct_regions deletes the heading AND the prose even when the region's
        own tag pair has been merged in between the heading and the prose.

        This is the merged-tag Defect A scenario: the generic reference's empty AH tags
        land between the heading and the prose during the harness merge, causing the old
        block-extent scan to stop too early.  With own_region="AuthorityHierarchy" the
        scan is transparent to those tags and the full block is deleted.
        """
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            "",
            "### Authority Hierarchy",
            "",
            '<AuthorityHierarchy type="managed">',
            "</AuthorityHierarchy>",
            "",
            "Four sources issue you instructions.",
            "**Why this ranking.** Because.",
            "",
            "### Other Heading",
            "",
            "Other content.",
            "",
        )
        identity = _identity_span(lines)
        ah_spec = RegionSpec(
            name="AuthorityHierarchy",
            parent_section="Identity",
            anchor=Anchor.SECTION_CONTENT_END,
            fallback_anchor=Anchor.SECTION_CONTENT_END,
            supersedes=(_AH_BLOCK,),
        )
        result = apply_conduct_regions(lines, {"Identity": identity}, specs=(ah_spec,))
        out_text = "".join(result.lines)
        assert "### Authority Hierarchy" not in out_text, (
            "Authority Hierarchy heading must be deleted when own tags interrupt the block"
        )
        assert "Four sources issue you instructions" not in out_text, (
            "Authority Hierarchy prose must be deleted even when own tags interrupt the block"
        )
        assert "Why this ranking" not in out_text, (
            "Full prose body must be deleted, not just lines before the merged-in tag pair"
        )
        assert "AH-block" in result.deletions_applied, (
            "AH-block must appear in deletions_applied when the block was found and deleted"
        )

    def test_foreign_region_tag_still_stops_the_scan(self):
        """A tag belonging to a different region must still stop the block extent.

        The own_region exemption is scoped to the named region only.  Crossing a
        different region's boundary via the heading-block deletion is forbidden and
        the existing guard must remain unconditional.
        """
        lines = _L(
            "# Agent",
            "### Authority Hierarchy",
            "",
            "Prose before the foreign boundary.",
            '<ClosingProcedure type="managed">',
            "</ClosingProcedure>",
            "",
            "Prose after the foreign boundary.",
        )
        mask = fence_mask(list(lines))
        end = find_heading_block_end(lines, 1, mask, own_region="AuthorityHierarchy")
        # Must stop at ClosingProcedure open tag (index 4), not continue past it
        assert end == 4, (
            f"Foreign-region tag at index 4 must stop the scan; expected end=4 got {end}"
        )

    def test_no_own_region_preserves_all_tags_as_stop_boundaries(self):
        """own_region=None preserves the original behaviour: every boundary tag stops the scan."""
        lines = _L(
            "# Agent",
            "### Authority Hierarchy",
            "",
            '<AuthorityHierarchy type="managed">',
            "</AuthorityHierarchy>",
            "",
            "Prose that must NOT be reached.",
        )
        mask = fence_mask(list(lines))
        end = find_heading_block_end(lines, 1, mask, own_region=None)
        # Must stop at the AH open tag (index 3) when own_region is None
        assert end == 3, (
            f"own_region=None must treat every tag as opaque; expected end=3 got {end}"
        )


# ---------------------------------------------------------------------------
# CP-hitl-step — "If" wording deletion (Defect B)
# ---------------------------------------------------------------------------

class TestCPHitlStepIfVariant:
    """CP-hitl-step must delete the HITL process step regardless of whether it uses
    'When' or 'If' to introduce the human_in_the_loop condition.

    The "If" wording appears in the real legacy source (observed during planning).
    The strict pattern must be widened to cover both forms without over-matching a
    superficially similar but semantically different step.

    Tests labelled RED will fail until the CP-hitl-step pattern is widened.
    """

    def _lines_with_if_hitl_step(self) -> tuple[list[str], dict]:
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            "2. If `human_in_the_loop: true`, present all output artifacts to the user "
               "for review/approval (final action before returning response)",
            "3. Return ONLY output json defined by communication protocol with status",
            "",
        )
        return lines, {"Identity": _identity_span(lines)}

    def test_if_variant_deleted_from_output(self):
        """The HITL step with 'If' wording must be deleted by CP-hitl-step (RED until I4.2)."""
        lines, sections = self._lines_with_if_hitl_step()
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_HITL_STEP, _CP_JSON_STEP))
        result = apply_conduct_regions(lines, sections, specs=(cp_spec,))
        out_text = "".join(result.lines)
        assert "present all output artifacts to the user for review" not in out_text, (
            "CP-hitl-step must delete the step when worded with 'If' (Defect B fix)"
        )

    def test_if_variant_in_deletions_applied(self):
        """CP-hitl-step must appear in deletions_applied when the 'If' variant is matched (RED)."""
        lines, sections = self._lines_with_if_hitl_step()
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_HITL_STEP, _CP_JSON_STEP))
        result = apply_conduct_regions(lines, sections, specs=(cp_spec,))
        assert "CP-hitl-step" in result.deletions_applied, (
            "CP-hitl-step must appear in deletions_applied for the 'If' variant"
        )

    def test_when_variant_still_deleted_from_output(self):
        """The 'When' variant of the HITL step must still be deleted (regression guard)."""
        when_step = (
            "When `human_in_the_loop: true`, present all output artifacts to the user "
            "for review/approval (final action before returning response)"
        )
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            f"2. {when_step}",
            "3. Return ONLY output json defined by communication protocol with status",
            "",
        )
        sections = {"Identity": _identity_span(lines)}
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_HITL_STEP, _CP_JSON_STEP))
        result = apply_conduct_regions(lines, sections, specs=(cp_spec,))
        out_text = "".join(result.lines)
        assert "present all output artifacts to the user for review" not in out_text, (
            "The 'When' variant must still be deleted by CP-hitl-step"
        )

    def test_if_near_miss_not_deleted(self):
        """A step using 'If human_in_the_loop' but taking a different action must not be deleted.

        The widened pattern must remain narrow enough that an unrelated 'If ...' step
        is not caught.  This near-miss proves the pattern cannot over-match.
        """
        near_miss = (
            "If `human_in_the_loop: true`, ask the user a clarifying question "
            "before proceeding"
        )
        lines = _L(
            "# TestAgent Agent",
            "",
            "### Process",
            "1. Step one.",
            f"2. {near_miss}",
            "3. Return ONLY output json defined by communication protocol with status",
            "",
        )
        sections = {"Identity": _identity_span(lines)}
        cp_spec = _minimal_spec("ClosingProcedure", supersedes=(_CP_HITL_STEP, _CP_JSON_STEP))
        result = apply_conduct_regions(lines, sections, specs=(cp_spec,))
        out_text = "".join(result.lines)
        assert near_miss in out_text, (
            "A near-miss step that uses 'If human_in_the_loop' but takes a different "
            "action must not be deleted by CP-hitl-step"
        )
        assert "CP-hitl-step" not in result.deletions_applied, (
            "CP-hitl-step must not appear in deletions_applied for the near-miss"
        )


# ---------------------------------------------------------------------------
# Drifted real-source wording — deletion (Defect C)
# ---------------------------------------------------------------------------

class TestDriftedRulePatterns:
    """Each drifted rule must delete the real-source wording observed in the legacy corpus
    while leaving near-miss prose intact.

    PC-bullet-1 and PC-bullet-2 tests are RED until I4.3 widens their patterns and
    changes their kind to REGEX_BULLET.  EH-retry and EP-context tests are GREEN
    because those patterns were already widened in the implementation.
    """

    # --- PC-bullet-1: Orchestration Artifacts drifted wording (RED) ---

    def test_pc_bullet_1_drifted_wording_deleted(self):
        """PC-bullet-1 must delete the real-source drifted wording (RED until I4.3)."""
        drifted = (
            "- **Orchestration Artifacts:** NEVER access orchestration artifacts "
            "not in your `input_artifacts`/`output_artifacts` lists"
        )
        lines = _L(
            "## Constraints",
            "",
            drifted,
            "- Other constraint.",
            "",
        )
        sections = {"Constraints": _constraints_span(lines)}
        pc_spec = _minimal_spec(
            "ProtocolConstraints",
            supersedes=(_PC_BULLET_1,),
        )
        pc_spec = RegionSpec(
            name="ProtocolConstraints",
            parent_section="Constraints",
            anchor=Anchor.SECTION_START,
            supersedes=(_PC_BULLET_1,),
        )
        result = apply_conduct_regions(lines, sections, specs=(pc_spec,))
        out_text = "".join(result.lines)
        assert "NEVER access orchestration artifacts not in your" not in out_text, (
            "PC-bullet-1 must delete the drifted wording "
            "'NEVER access orchestration artifacts not in your ...'"
        )
        assert "PC-bullet-1" in result.deletions_applied, (
            "PC-bullet-1 must appear in deletions_applied for the drifted wording"
        )

    def test_pc_bullet_1_drifted_wording_not_silently_stranded(self):
        """The drifted PC-bullet-1 wording must not survive adjacent to the inserted tag (RED)."""
        drifted = (
            "- **Orchestration Artifacts:** NEVER access orchestration artifacts "
            "not in your `input_artifacts`/`output_artifacts` lists"
        )
        lines = _L(
            "## Constraints",
            "",
            drifted,
            "",
        )
        sections = {"Constraints": _constraints_span(lines)}
        pc_spec = RegionSpec(
            name="ProtocolConstraints",
            parent_section="Constraints",
            anchor=Anchor.SECTION_START,
            supersedes=(_PC_BULLET_1,),
        )
        result = apply_conduct_regions(lines, sections, specs=(pc_spec,))
        # The drifted bullet must be deleted, not left beside the inserted region tags
        out_text = "".join(result.lines)
        assert "NEVER access orchestration artifacts not in your" not in out_text, (
            "Drifted PC-bullet-1 wording must be deleted, not stranded beside the region tags"
        )

    def test_pc_bullet_1_near_miss_not_deleted(self):
        """A bullet resembling PC-bullet-1 but not superseded must not be deleted."""
        near_miss = (
            "- **Orchestration Artifacts:** Read every artifact named in your "
            "`input_artifacts`/`output_artifacts` lists"
        )
        lines = _L(
            "## Constraints",
            "",
            near_miss,
            "",
        )
        sections = {"Constraints": _constraints_span(lines)}
        pc_spec = RegionSpec(
            name="ProtocolConstraints",
            parent_section="Constraints",
            anchor=Anchor.SECTION_START,
            supersedes=(_PC_BULLET_1,),
        )
        result = apply_conduct_regions(lines, sections, specs=(pc_spec,))
        out_text = "".join(result.lines)
        assert "Read every artifact named" in out_text, (
            "A near-miss that starts with '**Orchestration Artifacts:**' but uses different "
            "phrasing must not be deleted by PC-bullet-1"
        )
        assert "PC-bullet-1" not in result.deletions_applied, (
            "PC-bullet-1 must not appear in deletions_applied for a near-miss bullet"
        )

    # --- PC-bullet-2: Project Files drifted wording (RED) ---

    def test_pc_bullet_2_drifted_wording_deleted(self):
        """PC-bullet-2 must delete the real-source drifted wording (RED until I4.3)."""
        drifted = (
            "- **Project Files:** You MAY access any project file "
            "(files not listed as orchestration artifacts)"
        )
        lines = _L(
            "## Constraints",
            "",
            drifted,
            "- Other constraint.",
            "",
        )
        sections = {"Constraints": _constraints_span(lines)}
        pc_spec = RegionSpec(
            name="ProtocolConstraints",
            parent_section="Constraints",
            anchor=Anchor.SECTION_START,
            supersedes=(_PC_BULLET_2,),
        )
        result = apply_conduct_regions(lines, sections, specs=(pc_spec,))
        out_text = "".join(result.lines)
        assert "You MAY access any project file" not in out_text, (
            "PC-bullet-2 must delete the drifted wording 'You MAY access any project file'"
        )
        assert "PC-bullet-2" in result.deletions_applied, (
            "PC-bullet-2 must appear in deletions_applied for the drifted wording"
        )

    def test_pc_bullet_2_near_miss_not_deleted(self):
        """A bullet resembling PC-bullet-2 but not superseded must not be deleted."""
        near_miss = (
            "- **Project Files:** You MAY NOT modify any project file outside your output list"
        )
        lines = _L(
            "## Constraints",
            "",
            near_miss,
            "",
        )
        sections = {"Constraints": _constraints_span(lines)}
        pc_spec = RegionSpec(
            name="ProtocolConstraints",
            parent_section="Constraints",
            anchor=Anchor.SECTION_START,
            supersedes=(_PC_BULLET_2,),
        )
        result = apply_conduct_regions(lines, sections, specs=(pc_spec,))
        out_text = "".join(result.lines)
        assert "You MAY NOT modify" in out_text, (
            "A near-miss that starts with '**Project Files:**' but uses 'MAY NOT' must "
            "not be deleted by PC-bullet-2"
        )
        assert "PC-bullet-2" not in result.deletions_applied

    # --- EH-retry: drifted wording (GREEN — pattern already widened) ---

    def test_eh_retry_drifted_wording_deleted(self):
        """EH-retry must delete 'Retry transient errors once' (plural, no 'a')."""
        drifted = "- **Retry transient errors once** before escalating"
        lines = _L(
            "## Error Handling",
            "",
            drifted,
            "",
        )
        sections = {"ErrorHandling": _eh_span(lines)}
        eh_spec = RegionSpec(
            name="ErrorHandlingCommon",
            parent_section="ErrorHandling",
            anchor=Anchor.SECTION_START,
            supersedes=(_EH_RETRY,),
        )
        result = apply_conduct_regions(lines, sections, specs=(eh_spec,))
        out_text = "".join(result.lines)
        assert "Retry transient errors once" not in out_text, (
            "EH-retry must delete the drifted plural wording 'Retry transient errors once'"
        )
        assert "EH-retry" in result.deletions_applied

    def test_eh_retry_near_miss_not_deleted(self):
        """'Retry transient errors up to three times' must not be deleted by EH-retry."""
        near_miss = "- Retry transient errors up to three times before escalating"
        lines = _L(
            "## Error Handling",
            "",
            near_miss,
            "",
        )
        sections = {"ErrorHandling": _eh_span(lines)}
        eh_spec = RegionSpec(
            name="ErrorHandlingCommon",
            parent_section="ErrorHandling",
            anchor=Anchor.SECTION_START,
            supersedes=(_EH_RETRY,),
        )
        result = apply_conduct_regions(lines, sections, specs=(eh_spec,))
        out_text = "".join(result.lines)
        assert "up to three times" in out_text, (
            "EH-retry must not delete a bullet about retrying 'up to three times' "
            "— that wording is a near-miss, not the superseded bullet"
        )
        assert "EH-retry" not in result.deletions_applied

    # --- EP-context: drifted wording (GREEN — pattern already widened) ---

    def test_ep_context_drifted_wording_deleted(self):
        """EP-context must delete '...Follow-up tasks are handled by spawning new agent instances'."""
        drifted = (
            "- **Context Management:** You can dedicate your full context window to this task. "
            "Follow-up tasks are handled by spawning new agent instances."
        )
        lines = _L(
            "## Execution Philosophy",
            "",
            drifted,
            "",
        )
        sections = {"ExecutionPhilosophy": _ep_span(lines)}
        ep_spec = RegionSpec(
            name="ExecutionPhilosophyCommon",
            parent_section="ExecutionPhilosophy",
            anchor=Anchor.SECTION_START,
            supersedes=(_EP_CONTEXT,),
        )
        result = apply_conduct_regions(lines, sections, specs=(ep_spec,))
        out_text = "".join(result.lines)
        assert "Follow-up tasks are handled by spawning new agent instances" not in out_text, (
            "EP-context must delete the drifted wording with 'tasks are'"
        )
        assert "EP-context" in result.deletions_applied

    def test_ep_context_near_miss_not_deleted(self):
        """'Follow-up tasks are handled by re-invoking this same agent' must not be deleted."""
        near_miss = (
            "- **Context Management:** Follow-up tasks are handled by "
            "re-invoking this same agent."
        )
        lines = _L(
            "## Execution Philosophy",
            "",
            near_miss,
            "",
        )
        sections = {"ExecutionPhilosophy": _ep_span(lines)}
        ep_spec = RegionSpec(
            name="ExecutionPhilosophyCommon",
            parent_section="ExecutionPhilosophy",
            anchor=Anchor.SECTION_START,
            supersedes=(_EP_CONTEXT,),
        )
        result = apply_conduct_regions(lines, sections, specs=(ep_spec,))
        out_text = "".join(result.lines)
        assert "re-invoking this same agent" in out_text, (
            "EP-context must not delete a bullet about re-invoking the same agent "
            "— that is a near-miss, not the superseded wording"
        )
        assert "EP-context" not in result.deletions_applied


# ---------------------------------------------------------------------------
# PC-bullet-5 — benign no-op when the line is absent
# ---------------------------------------------------------------------------

class TestPCBullet5AbsentIsNoOp:
    """When the source never carried the PC-bullet-5 line, applying the rule must
    leave the document unchanged.  This is a structural no-op, not a deletion failure.
    The rule is required=True so it appears in deletions_unmatched, but nothing is
    deleted and the probe only fires if there is a similar-looking line to fire on.
    """

    def test_pc_bullet_5_absent_no_content_deleted(self):
        """No content is deleted when the source has no PC-bullet-5 wording."""
        other_bullet = "- Stay within scope"
        lines = _L(
            "## Constraints",
            "",
            other_bullet,
            "",
        )
        sections = {"Constraints": _constraints_span(lines)}
        pc_spec = RegionSpec(
            name="ProtocolConstraints",
            parent_section="Constraints",
            anchor=Anchor.SECTION_START,
            supersedes=(_PC_BULLET_5,),
        )
        result = apply_conduct_regions(lines, sections, specs=(pc_spec,))
        out_text = "".join(result.lines)
        assert other_bullet in out_text, (
            "An unrelated bullet must survive untouched when PC-bullet-5 finds no match"
        )

    def test_pc_bullet_5_absent_rule_in_deletions_unmatched(self):
        """A required rule with no match records its id in deletions_unmatched."""
        lines = _L(
            "## Constraints",
            "",
            "- Stay within scope",
            "",
        )
        sections = {"Constraints": _constraints_span(lines)}
        pc_spec = RegionSpec(
            name="ProtocolConstraints",
            parent_section="Constraints",
            anchor=Anchor.SECTION_START,
            supersedes=(_PC_BULLET_5,),
        )
        result = apply_conduct_regions(lines, sections, specs=(pc_spec,))
        assert "PC-bullet-5" in result.deletions_unmatched, (
            "PC-bullet-5 is required=True; an unmatched no-op must land in deletions_unmatched"
        )
        assert "PC-bullet-5" not in result.deletions_applied, (
            "PC-bullet-5 must not appear in deletions_applied when no line was deleted"
        )

    def test_pc_bullet_5_absent_no_non_conformances(self):
        """When PC-bullet-5 finds nothing at all (neither strict nor probe), no NC is raised."""
        lines = _L(
            "## Constraints",
            "",
            "- Stay within scope",
            "",
        )
        sections = {"Constraints": _constraints_span(lines)}
        pc_spec = RegionSpec(
            name="ProtocolConstraints",
            parent_section="Constraints",
            anchor=Anchor.SECTION_START,
            supersedes=(_PC_BULLET_5,),
        )
        result = apply_conduct_regions(lines, sections, specs=(pc_spec,))
        assert result.non_conformances == [], (
            "An absent bullet that matches neither the strict pattern nor the probe "
            "must not produce any NC_DRIFTED_BULLET finding"
        )


# ---------------------------------------------------------------------------
# Drift probe invariant — every rule in CONDUCT_REGIONS has a probe
# ---------------------------------------------------------------------------

class TestAllConductRegionsRulesHaveDriftProbe:
    """Every DeletionRule in CONDUCT_REGIONS must carry a non-None drift_probe so that
    a future wording drift is reported as NC_DRIFTED_BULLET rather than silently left
    in place adjacent to the inserted region tags."""

    def test_no_rule_has_none_drift_probe(self):
        """All DeletionRules in CONDUCT_REGIONS must have drift_probe is not None."""
        missing = [
            f"{spec.name}/{rule.rule_id}"
            for spec in CONDUCT_REGIONS
            for rule in spec.supersedes
            if rule.drift_probe is None
        ]
        assert missing == [], (
            f"Rules with drift_probe=None violate the ContractsDesign.md invariant "
            f"(every rule must have a probe so drifted wording is reported): {missing}"
        )
