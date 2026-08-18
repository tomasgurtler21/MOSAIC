"""Shared, pure conduct-region emission and prose deletion for MOSAIC agent transforms.

Both the generic and harness body-transform paths call apply_conduct_regions
with the same declarative CONDUCT_REGIONS table.  The table is the single source
of truth for region placement and deletion rules, making it structurally impossible
for a placement fix to land on one path and miss the other.
"""
from __future__ import annotations

import dataclasses
import pathlib
import re
from enum import Enum
from typing import Mapping, Sequence

from boundary_constants import (
    BoundaryKind,
    DEPLOYED_PARENT_MAP,
    MARKER_TO_INJECTION_NAME,
    SECTION_HEADING_MAP,
    TAG_PATTERN,
    open_tag,
    close_tag,
    tag_base_name,
)
from fence import fence_mask, first_unfenced, safe_insertion_index
from non_conformance import NC_DRIFTED_BULLET, NC_MESSAGES, NonConformance


# ---------------------------------------------------------------------------
# Legacy section deletion data structures
# ---------------------------------------------------------------------------

@dataclasses.dataclass(frozen=True)
class LegacySectionDeletion:
    """One retired legacy section that the transform deletes outright."""
    section_id: str   # stable id for bookkeeping, e.g. "OutputFormat"
    heading: str      # exact heading line text, e.g. "## Output Format"
    level: int        # Markdown heading level the match requires, e.g. 2


@dataclasses.dataclass
class LegacySectionDeletionResult:
    """Return value from delete_legacy_sections."""
    lines: list[str]      # the body with matched sections removed
    deleted: list[str]    # section_id values that matched, in document order


# ---------------------------------------------------------------------------
# Artifact-structure move data structures and constants
# ---------------------------------------------------------------------------

#: Name of the injection region that receives the moved artifact-structure block.
ARTIFACT_TEMPLATE_REGION_NAME: str = "OutputArtifactTemplate"

#: Matches an H3 heading whose text ends in "Artifact Structure" (any prefix).
#: Exactly three '#' characters are required: H2 and H4 do not match.
ARTIFACT_STRUCTURE_HEADING_PATTERN: str = r"^###\s+(?P<title>\S.*\bArtifact Structure)\s*$"

_ARTIFACT_STRUCTURE_HEADING_RE: re.Pattern[str] = re.compile(
    ARTIFACT_STRUCTURE_HEADING_PATTERN
)


@dataclasses.dataclass
class ArtifactTemplateMoveResult:
    """Return value from move_artifact_structure_block."""
    lines: list[str]            # the body with the block relocated
    moved: bool                 # True only when a block was actually relocated
    heading_text: str | None    # the matched heading line, stripped; None when moved is False


# ---------------------------------------------------------------------------
# Declarative table of retired legacy sections.  Adding a future retired section
# is one more tuple entry; no code changes are needed.
LEGACY_SECTION_DELETIONS: tuple[LegacySectionDeletion, ...] = (
    LegacySectionDeletion(
        section_id="OutputFormat",
        heading="## Output Format",
        level=2,
    ),
)


def delete_legacy_sections(
    lines: Sequence[str],
    *,
    deletions: Sequence[LegacySectionDeletion] = LEGACY_SECTION_DELETIONS,
) -> LegacySectionDeletionResult:
    """Remove retired legacy sections from a raw body before any transform step runs.

    Pure: reads and writes no files; mutates neither argument.

    For each entry in `deletions`, the first unfenced line whose stripped text
    equals `entry.heading` and whose heading level equals `entry.level` exactly
    is treated as the start of the deleted span.  The span extends through the
    line before the first subsequent line that is:
      - an unfenced heading of level ≤ entry.level,
      - a MOSAIC boundary tag line (TAG_PATTERN.match(...) is not None),
      - a legacy region marker or old-style boundary marker,
      - a '---' separator that is NOT preceded by a blank line (a '---' that IS
        preceded by a blank line is a trailing section separator that falls inside
        the deleted extent and is deleted with the section), or
      - end of input.

    Only the first match per declared entry is deleted.  A second occurrence of
    the same heading is left in place.
    """
    if not lines:
        return LegacySectionDeletionResult(lines=[], deleted=[])

    mask = fence_mask(list(lines))
    lines_list: list[str] = list(lines)
    lines_to_delete: set[int] = set()
    deleted: list[str] = []

    # Build the set of legacy-marker strings for fast lookup
    legacy_markers: frozenset[str] = frozenset(MARKER_TO_INJECTION_NAME.keys())
    old_style_prefixes: tuple[str, ...] = ("[[SECTION:", "[[DEPLOYED:", "[[INJECTION:")

    def _is_stop_line(stripped: str, prev_stripped: str) -> bool:
        """Return True when the line is a deletion stop boundary.

        A `---` separator line stops the deletion only when the line immediately
        preceding it is non-blank.  A `---` preceded by a blank line is a trailing
        section separator that falls inside the deleted extent and is deleted with it.
        """
        # MOSAIC boundary tag
        if TAG_PATTERN.match(stripped) is not None:
            return True
        # Legacy region marker
        if stripped in legacy_markers:
            return True
        # Old-style marker prefix
        if any(stripped.startswith(p) for p in old_style_prefixes):
            return True
        # Section separator: stops only when NOT preceded by a blank line
        if stripped == "---" and prev_stripped != "":
            return True
        return False

    for entry in deletions:
        # Find the first unfenced line matching the heading (text AND level)
        heading_idx: int | None = None
        for i, line in enumerate(lines_list):
            if mask[i]:
                continue
            stripped = line.strip()
            if stripped != entry.heading:
                continue
            # Verify heading level by counting leading '#' chars
            level = 0
            for ch in stripped:
                if ch == "#":
                    level += 1
                else:
                    break
            if level == entry.level:
                heading_idx = i
                break

        if heading_idx is None:
            continue  # This entry did not match; no-op

        # Find the end of the extent: first stop line after the heading.
        # prev_stripped tracks the stripped content of the last non-fence line
        # seen, so that the ---/blank-line distinction can be applied.
        extent_end = len(lines_list)
        prev_stripped = ""  # tracks the last non-fence, non-deleted line's stripped text
        for i in range(heading_idx + 1, len(lines_list)):
            if mask[i]:
                continue
            stripped = lines_list[i].strip()
            # Stop at a heading of level ≤ entry.level
            if stripped.startswith("#"):
                next_level = 0
                for ch in stripped:
                    if ch == "#":
                        next_level += 1
                    else:
                        break
                if next_level <= entry.level:
                    extent_end = i
                    break
            if _is_stop_line(stripped, prev_stripped):
                extent_end = i
                break
            prev_stripped = stripped

        # Mark heading and body lines for deletion
        for i in range(heading_idx, extent_end):
            lines_to_delete.add(i)
        deleted.append(entry.section_id)

    if not lines_to_delete:
        return LegacySectionDeletionResult(lines=lines_list, deleted=[])

    # Build new line list, applying blank-line hygiene at each gap
    raw_result: list[str] = []
    gap_positions: list[int] = []

    i = 0
    while i < len(lines_list):
        if i in lines_to_delete:
            while i < len(lines_list) and i in lines_to_delete:
                i += 1
            gap_positions.append(len(raw_result))
            continue
        raw_result.append(lines_list[i])
        i += 1

    # Collapse double blanks created by deletion: when both the line before and
    # after a deletion gap are blank, remove the one immediately after the gap.
    indices_to_remove: set[int] = set()
    for g in gap_positions:
        before_blank = g > 0 and raw_result[g - 1].strip() == ""
        after_blank = g < len(raw_result) and raw_result[g].strip() == ""
        if before_blank and after_blank:
            indices_to_remove.add(g)

    result_lines = [ln for idx, ln in enumerate(raw_result) if idx not in indices_to_remove]
    return LegacySectionDeletionResult(lines=result_lines, deleted=deleted)


# ---------------------------------------------------------------------------
# Data structures
# ---------------------------------------------------------------------------

@dataclasses.dataclass(frozen=True)
class SectionSpan:
    """Locates a canonical section within a body-line list."""
    name: str           # canonical section name, e.g. "Identity"
    heading_line: int   # 0-based index of the section heading line
    start: int          # 0-based index of first content line after heading
    content_end: int    # 0-based index ONE PAST the last content line
    end: int            # 0-based index ONE PAST the section, including any '---' separator

    def __post_init__(self) -> None:
        if not (self.heading_line < self.start <= self.content_end <= self.end):
            raise ValueError(
                f"SectionSpan invariant violated: "
                f"heading_line={self.heading_line} start={self.start} "
                f"content_end={self.content_end} end={self.end}"
            )


class DeletionKind(Enum):
    HEADING_BLOCK   = "heading_block"    # heading + everything to next heading of <= level
    EXACT_BULLET    = "exact_bullet"     # bullet whose normalised text equals pattern
    REGEX_BULLET    = "regex_bullet"     # bullet whose text matches pattern as a regex
    NUMBERED_STEP   = "numbered_step"    # ordered-list item matching pattern


@dataclasses.dataclass(frozen=True)
class DeletionRule:
    """One deletion rule belonging to a RegionSpec."""
    rule_id: str                       # stable id, e.g. "AH-block", "CP-json-step"
    kind: DeletionKind
    pattern: str
    required: bool = False             # True: absence recorded in deletions_unmatched
    drift_probe: str | None = None     # permissive regex for a drifted instance


class Anchor(Enum):
    SECTION_START       = "section_start"        # first line of the section body
    SECTION_CONTENT_END = "section_content_end"  # at content_end
    AFTER_PROCESS_LIST  = "after_process_list"   # Identity only
    BEFORE_REGION       = "before_region"        # immediately before anchor_ref open tag
    AFTER_REGION        = "after_region"         # immediately after anchor_ref close tag


@dataclasses.dataclass(frozen=True)
class RegionSpec:
    """One row of CONDUCT_REGIONS: declarative placement + deletion for one deployed region."""
    name: str                                       # canonical DEPLOYED name
    parent_section: str                             # canonical section name
    anchor: Anchor
    anchor_ref: tuple[BoundaryKind, str] | None = None
    fallback_anchor: Anchor | None = None
    supersedes: tuple[DeletionRule, ...] = ()


@dataclasses.dataclass
class RegionInsertionResult:
    """Return value from apply_conduct_regions."""
    lines: list[str]                        # the transformed body
    deployed_added: list[str]               # names emitted, document order
    deletions_applied: list[str]            # rule_id values whose pattern matched
    deletions_unmatched: list[str]          # rule_ids that did not match (required or probe)
    non_conformances: list[NonConformance]  # exclusively NC_DRIFTED_BULLET, one per drift_probe hit


# ---------------------------------------------------------------------------
# Deletion rules for Stage 2 regions
# ---------------------------------------------------------------------------

_CP_HITL_STEP = DeletionRule(
    rule_id="CP-hitl-step",
    kind=DeletionKind.NUMBERED_STEP,
    pattern=(
        r"(?:When|If) `human_in_the_loop: true`, present all output artifacts to the user "
        r"for review"
    ),
    required=False,
    drift_probe=r"(?i)(human|HITL).*(review|present)",
)

_CP_JSON_STEP = DeletionRule(
    rule_id="CP-json-step",
    kind=DeletionKind.NUMBERED_STEP,
    pattern=r"Return ONLY output json defined by communication protocol with status",
    required=True,
    drift_probe=r"(?i)(JSON.*(return|respond)|(return|respond).*JSON)",
)

_AH_BLOCK = DeletionRule(
    rule_id="AH-block",
    kind=DeletionKind.HEADING_BLOCK,
    pattern=r"### Authority Hierarchy",
    required=True,
    drift_probe=r"(?i)^#{2,4}\s+Authority Hierarchy",
)

# ---------------------------------------------------------------------------
# Deletion rules for Stage 3 regions
# ---------------------------------------------------------------------------

_EH_RETRY = DeletionRule(
    rule_id="EH-retry",
    kind=DeletionKind.REGEX_BULLET,
    pattern=r"Retry (?:a )?transient errors? once",
    required=True,
    drift_probe=r"(?i)retry.*(transient|error)",
)

_EH_ERRCODES = DeletionRule(
    rule_id="EH-errcodes",
    kind=DeletionKind.REGEX_BULLET,
    pattern=r"missing prerequisites",
    required=False,
    drift_probe=r"(?i)BLOCKED.*E[0-9]",
)

_EP_CONTEXT = DeletionRule(
    rule_id="EP-context",
    kind=DeletionKind.REGEX_BULLET,
    pattern=r"Context Management.*Follow-up (?:work|tasks) (?:is|are) handled by spawning new agent instances",
    required=True,
    drift_probe=r"(?i)Context Management",
)

_EP_MEMORY = DeletionRule(
    rule_id="EP-memory",
    kind=DeletionKind.REGEX_BULLET,
    pattern=r"Memory via Artifacts.*persistent memory",
    required=True,
    drift_probe=r"(?i)Memory via Artifacts",
)

_EP_QUALITY = DeletionRule(
    rule_id="EP-quality",
    kind=DeletionKind.REGEX_BULLET,
    pattern=r"Quality over Completeness.*PARTIALLY_DONE",
    required=True,
    drift_probe=r"(?i)Quality over Completeness",
)

# ---------------------------------------------------------------------------
# Deletion rules for Stage 4 regions (ProtocolConstraints)
# ---------------------------------------------------------------------------

# PC-bullet-1 through PC-bullet-4: exact-match bullets, corpus-verified.
_PC_BULLET_1 = DeletionRule(
    rule_id="PC-bullet-1",
    kind=DeletionKind.REGEX_BULLET,
    pattern=r"\*\*Orchestration Artifacts:\*\* NEVER access .*`input_artifacts`/`output_artifacts`",
    required=True,
    drift_probe=r"(?i)^\*\*Orchestration Artifacts:\*\*",
)

_PC_BULLET_2 = DeletionRule(
    rule_id="PC-bullet-2",
    kind=DeletionKind.REGEX_BULLET,
    pattern=r"\*\*Project Files:\*\* You MAY (?:read, modify, or create|access) any project file",
    required=True,
    drift_probe=r"(?i)^\*\*Project Files:\*\*",
)

_PC_BULLET_3 = DeletionRule(
    rule_id="PC-bullet-3",
    kind=DeletionKind.EXACT_BULLET,
    pattern="NEVER skip the JSON response block",
    required=True,
    drift_probe=r"(?i)^NEVER skip\b.*\bJSON\b",
)

_PC_BULLET_4 = DeletionRule(
    rule_id="PC-bullet-4",
    kind=DeletionKind.EXACT_BULLET,
    pattern="NEVER invent status codes",
    required=True,
    drift_probe=r"(?i)^NEVER invent\b",
)

# PC-bullet-5: regex rule matching both canonical and the known drifted wording.
# The strict pattern requires "agent(s)" to be mentioned to avoid over-matching
# unrelated "Note ... do not ..." bullets.
_PC_BULLET_5 = DeletionRule(
    rule_id="PC-bullet-5",
    kind=DeletionKind.REGEX_BULLET,
    pattern=r"(?i)Note\b.*\bagents?\b.*(?:do\s+not\b|don.t\b)",
    required=True,
    drift_probe=r"(?i)^Note.+\bagents?\b",
)

# ---------------------------------------------------------------------------
# CONDUCT_REGIONS table — Stage 2 adds rows 1 and 2; Stage 3 adds rows 3 and 4;
# Stage 4 adds rows 5 and 6 (ProtocolConstraints, HarnessConstraints).
# ---------------------------------------------------------------------------

CONDUCT_REGIONS: tuple[RegionSpec, ...] = (
    RegionSpec(
        name="ClosingProcedure",
        parent_section="Identity",
        anchor=Anchor.AFTER_PROCESS_LIST,
        anchor_ref=None,
        fallback_anchor=Anchor.SECTION_CONTENT_END,
        supersedes=(_CP_HITL_STEP, _CP_JSON_STEP),
    ),
    RegionSpec(
        name="AuthorityHierarchy",
        parent_section="Identity",
        anchor=Anchor.AFTER_REGION,
        anchor_ref=(BoundaryKind.DEPLOYED, "ClosingProcedure"),
        fallback_anchor=Anchor.SECTION_CONTENT_END,
        supersedes=(_AH_BLOCK,),
    ),
    RegionSpec(
        name="ErrorHandlingCommon",
        parent_section="ErrorHandling",
        anchor=Anchor.SECTION_START,
        anchor_ref=None,
        fallback_anchor=None,
        supersedes=(_EH_RETRY, _EH_ERRCODES),
    ),
    RegionSpec(
        name="ExecutionPhilosophyCommon",
        parent_section="ExecutionPhilosophy",
        anchor=Anchor.BEFORE_REGION,
        anchor_ref=(BoundaryKind.INJECTION, "ContextLimits"),
        fallback_anchor=Anchor.SECTION_START,
        supersedes=(_EP_CONTEXT, _EP_MEMORY, _EP_QUALITY),
    ),
    RegionSpec(
        name="ProtocolConstraints",
        parent_section="Constraints",
        anchor=Anchor.SECTION_START,
        anchor_ref=None,
        fallback_anchor=None,
        supersedes=(_PC_BULLET_1, _PC_BULLET_2, _PC_BULLET_3, _PC_BULLET_4, _PC_BULLET_5),
    ),
    RegionSpec(
        name="HarnessConstraints",
        parent_section="Constraints",
        # CustomConstraints is retired from CANONICAL_DEPLOYED, so it can no longer
        # serve as this row's anchor_ref. Re-anchored to AFTER_REGION of
        # ProtocolConstraints, the row immediately preceding it in the Constraints
        # section, with SECTION_CONTENT_END kept as fallback for files where
        # ProtocolConstraints was not emitted.
        anchor=Anchor.AFTER_REGION,
        anchor_ref=(BoundaryKind.DEPLOYED, "ProtocolConstraints"),
        fallback_anchor=Anchor.SECTION_CONTENT_END,
        supersedes=(),
    ),
)
"""The declarative placement + deletion table.

Populated incrementally across stages.  Stage 2 adds ClosingProcedure and
AuthorityHierarchy.  Stage 3 adds ErrorHandlingCommon and ExecutionPhilosophyCommon.
Stage 4 adds ProtocolConstraints and HarnessConstraints.
"""


# ---------------------------------------------------------------------------
# Internal helpers
# ---------------------------------------------------------------------------

def _normalise_list_text(line: str) -> str:
    """Strip leading list markers (-, *, N.) and surrounding whitespace,
    then collapse internal whitespace runs to a single space."""
    text = line.strip()
    # Remove numbered list marker
    text = re.sub(r"^\d+\.\s+", "", text)
    # Remove bullet markers
    text = re.sub(r"^[-*]\s+", "", text)
    # Collapse internal whitespace
    text = re.sub(r"\s+", " ", text)
    return text


def _line_matches_rule(line: str, rule: DeletionRule) -> bool:
    """Return True when `line` strictly matches `rule`."""
    norm = _normalise_list_text(line)
    if rule.kind == DeletionKind.NUMBERED_STEP:
        return bool(re.search(rule.pattern, norm))
    elif rule.kind == DeletionKind.EXACT_BULLET:
        return norm == rule.pattern
    elif rule.kind == DeletionKind.REGEX_BULLET:
        return bool(re.search(rule.pattern, norm))
    elif rule.kind == DeletionKind.HEADING_BLOCK:
        return line.strip() == rule.pattern
    return False


def _line_matches_drift_probe(line: str, rule: DeletionRule) -> bool:
    """Return True when `line` matches the drift probe (if any)."""
    if rule.drift_probe is None:
        return False
    norm = _normalise_list_text(line)
    return bool(re.search(rule.drift_probe, norm))


def find_heading_block_end(
    lines: Sequence[str],
    heading_idx: int,
    mask: Sequence[bool],
    *,
    own_region: str | None = None,
) -> int:
    """Return the index one past the heading block starting at heading_idx.

    For a HEADING_BLOCK rule, the block runs from the heading through every
    line up to (but not including) the next heading of the same or higher level,
    outside any fence.

    heading_idx must point to a line starting with '### ' (H3).

    own_region: when provided, boundary tags belonging to this region (matched
    by tag_base_name) are transparent to the scan and do not stop it. Every
    other region's tags remain opaque.  own_region=None (default) preserves the
    original behaviour: every boundary tag stops the scan.
    """
    # Determine heading level from the pattern
    heading_line = lines[heading_idx].strip()
    level = 0
    for ch in heading_line:
        if ch == "#":
            level += 1
        else:
            break

    # Scan forward for next heading of same or higher level (fewer or equal #s),
    # or an opaque boundary tag.
    for i in range(heading_idx + 1, len(lines)):
        if mask[i]:
            continue
        line = lines[i].strip()
        if line.startswith("#"):
            next_level = 0
            for ch in line:
                if ch == "#":
                    next_level += 1
                else:
                    break
            if next_level <= level:
                return i
        # Check boundary tags — transparent when they belong to own_region
        m = TAG_PATTERN.match(line)
        if m is not None:
            if own_region is not None and tag_base_name(m.group("name")) == own_region:
                continue  # own region's tag is transparent
            return i
    return len(lines)


# Keep the private alias for backwards compatibility with any internal call sites
# that pre-date the public promotion.
_find_heading_block_end = find_heading_block_end


def _apply_deletions(
    lines: list[str],
    section: SectionSpan,
    rules: tuple[DeletionRule, ...],
    own_region: str | None = None,
) -> tuple[list[str], list[str], list[str], list[NonConformance]]:
    """Apply deletion rules within a section, returning
    (new_lines, applied, unmatched, non_conformances).

    Operates on the full line list but only matches within [section.start, section.content_end).
    Returns a new list of all lines (with matched spans removed), plus bookkeeping lists.

    A rule whose strict `pattern` does not match but whose `drift_probe` does yields
    exactly one NC_DRIFTED_BULLET NonConformance (per the outcome contract on
    DeletionRule), in addition to being recorded in `unmatched`. The `file` field is
    a placeholder — the caller (boundary_transformer.transform_file) replaces it with
    the real input path before merging into TransformResult.non_conformances.
    """
    mask = fence_mask(lines)
    applied: list[str] = []
    unmatched: list[str] = []
    non_conformances: list[NonConformance] = []

    # Build a set of line indices to delete
    lines_to_delete: set[int] = set()
    # Track which lines were consumed by a strict match (so drift probe skips them)
    strict_matched_lines: set[int] = set()

    # First pass: find strict matches
    for rule in rules:
        found = False
        if rule.kind == DeletionKind.HEADING_BLOCK:
            # Find the heading in the section range
            for i in range(section.start, section.content_end):
                if mask[i]:
                    continue
                if _line_matches_rule(lines[i], rule):
                    block_end = find_heading_block_end(
                        lines, i, mask, own_region=own_region
                    )
                    # Clamp to section content_end
                    block_end = min(block_end, section.content_end)
                    for j in range(i, block_end):
                        lines_to_delete.add(j)
                        strict_matched_lines.add(j)
                    applied.append(rule.rule_id)
                    found = True
                    break
        elif rule.kind in (DeletionKind.NUMBERED_STEP, DeletionKind.EXACT_BULLET, DeletionKind.REGEX_BULLET):
            for i in range(section.start, section.content_end):
                if mask[i] or i in strict_matched_lines:
                    continue
                if _line_matches_rule(lines[i], rule):
                    lines_to_delete.add(i)
                    strict_matched_lines.add(i)
                    applied.append(rule.rule_id)
                    found = True
                    break

        if not found:
            # Check drift probe
            drift_found = False
            if rule.drift_probe is not None:
                for i in range(section.start, section.content_end):
                    if mask[i] or i in strict_matched_lines:
                        continue
                    if _line_matches_drift_probe(lines[i], rule):
                        # Probe matched but strict didn't — leave in place, record
                        # unmatched, and raise exactly one NC_DRIFTED_BULLET finding
                        # per the outcome contract on DeletionRule.
                        unmatched.append(rule.rule_id)
                        non_conformances.append(NonConformance(
                            code=NC_DRIFTED_BULLET,
                            file=pathlib.Path("."),
                            message=NC_MESSAGES[NC_DRIFTED_BULLET].format(detail=rule.rule_id),
                            line_number=i + 1,
                            detail=rule.rule_id,
                        ))
                        drift_found = True
                        break
            if not drift_found and rule.required:
                unmatched.append(rule.rule_id)

    if not lines_to_delete:
        return list(lines), applied, unmatched, non_conformances

    # Build new line list, tracking the result position where each contiguous
    # deletion gap starts.  After building raw_result, we go back and remove one
    # blank line at each gap where the lines immediately before and after the gap
    # are both blank — so that a deletion surrounded by blank lines does not leave
    # a double blank, while content outside the touched regions stays byte-identical.
    raw_result: list[str] = []
    gap_positions: list[int] = []   # index in raw_result immediately after each gap

    i = 0
    while i < len(lines):
        if i in lines_to_delete:
            # Skip the entire contiguous deleted block and record the gap position.
            while i < len(lines) and i in lines_to_delete:
                i += 1
            gap_positions.append(len(raw_result))
            continue
        raw_result.append(lines[i])
        i += 1

    # For each deletion gap: if the line immediately before the gap and the line
    # immediately after the gap are both blank, remove the one after the gap so
    # that at most one blank line separates the surrounding content.
    indices_to_remove: set[int] = set()
    for g in gap_positions:
        before_blank = g > 0 and raw_result[g - 1].strip() == ""
        after_blank = g < len(raw_result) and raw_result[g].strip() == ""
        if before_blank and after_blank:
            indices_to_remove.add(g)

    result = [ln for idx, ln in enumerate(raw_result) if idx not in indices_to_remove]
    return result, applied, unmatched, non_conformances


def _region_already_present(lines: Sequence[str], name: str) -> bool:
    """Return True when a deployed region open tag for name already exists in lines."""
    open_t = open_tag(BoundaryKind.DEPLOYED, name)
    for line in lines:
        if line.strip() == open_t:
            return True
    return False


def _find_anchor_index(
    lines: Sequence[str],
    section: SectionSpan,
    spec: RegionSpec,
) -> int | None:
    """Compute the line index at which to insert spec's region tags.

    Returns None if the anchor cannot be resolved (e.g. missing anchor_ref
    and no fallback).
    """
    mask = fence_mask(lines)

    if spec.anchor == Anchor.AFTER_PROCESS_LIST:
        end = locate_process_list_end(lines, section)
        if end is not None:
            return safe_insertion_index(lines, end) if end < len(lines) and mask[end] else end
        # Fall through to fallback
        if spec.fallback_anchor is not None:
            return _resolve_fallback(lines, section, spec.fallback_anchor, mask)
        return None

    elif spec.anchor == Anchor.AFTER_REGION:
        if spec.anchor_ref is not None:
            kind, ref_name = spec.anchor_ref
            close = close_tag(kind, ref_name)
            for i in range(section.start, section.content_end):
                if lines[i].strip() == close:
                    idx = i + 1
                    if idx < len(lines) and mask[idx]:
                        idx = safe_insertion_index(lines, idx)
                    return idx
        # anchor_ref not found — use fallback
        if spec.fallback_anchor is not None:
            return _resolve_fallback(lines, section, spec.fallback_anchor, mask)
        return None

    elif spec.anchor == Anchor.BEFORE_REGION:
        if spec.anchor_ref is not None:
            kind, ref_name = spec.anchor_ref
            open_t = open_tag(kind, ref_name)
            for i in range(section.start, section.content_end):
                if lines[i].strip() == open_t:
                    idx = i
                    if mask[idx]:
                        idx = safe_insertion_index(lines, idx)
                    return idx
        if spec.fallback_anchor is not None:
            return _resolve_fallback(lines, section, spec.fallback_anchor, mask)
        return None

    elif spec.anchor == Anchor.SECTION_START:
        idx = section.start
        if idx < len(lines) and mask[idx]:
            idx = safe_insertion_index(lines, idx)
        return idx

    elif spec.anchor == Anchor.SECTION_CONTENT_END:
        return _resolve_fallback(lines, section, Anchor.SECTION_CONTENT_END, mask)

    return None


def _resolve_fallback(
    lines: Sequence[str],
    section: SectionSpan,
    fallback: Anchor,
    mask: list[bool],
) -> int:
    if fallback == Anchor.SECTION_CONTENT_END:
        idx = section.content_end
        if idx < len(lines) and mask[idx]:
            idx = safe_insertion_index(lines, idx)
        return idx
    elif fallback == Anchor.SECTION_START:
        idx = section.start
        if idx < len(lines) and mask[idx]:
            idx = safe_insertion_index(lines, idx)
        return idx
    return section.content_end


# ---------------------------------------------------------------------------
# Public functions
# ---------------------------------------------------------------------------

def apply_conduct_regions(
    lines: Sequence[str],
    sections: Mapping[str, SectionSpan],
    *,
    specs: Sequence[RegionSpec] = CONDUCT_REGIONS,
) -> RegionInsertionResult:
    """Insert every applicable deployed region and delete the prose it supersedes.

    Pure: reads and writes no files, mutates neither argument.
    """
    working_lines: list[str] = list(lines)
    deployed_added: list[str] = []
    deletions_applied: list[str] = []
    deletions_unmatched: list[str] = []
    non_conformances: list[NonConformance] = []

    for spec in specs:
        section = sections.get(spec.parent_section)
        if section is None:
            continue

        # Re-locate the section before deletion so that line indices are correct
        # even when earlier specs (on different parent sections) inserted or deleted
        # lines, causing the stored span to be stale.
        section = _relocate_section(working_lines, section)

        # Apply deletions first (so the anchor index reflects deleted content)
        if spec.supersedes:
            # Recompute section span after previous iterations may have changed working_lines
            # The section span indices may be stale; we use the original section for now
            # (apply_conduct_regions is called once per file, sections from caller)
            working_lines, applied, unmatched, rule_ncs = _apply_deletions(
                working_lines, section, spec.supersedes, own_region=spec.name
            )
            deletions_applied.extend(applied)
            deletions_unmatched.extend(unmatched)
            non_conformances.extend(rule_ncs)
            # Update section's content_end after deletions
            # (deletion may have shifted indices via text rebuild)
            # Re-locate the section after deletions
            section = _relocate_section(working_lines, section)

        # Skip if region already present
        if _region_already_present(working_lines, spec.name):
            continue

        # Find insertion index
        insert_idx = _find_anchor_index(working_lines, section, spec)
        if insert_idx is None:
            continue

        # Ensure insert_idx is within valid range
        insert_idx = min(insert_idx, len(working_lines))

        # Insert region tags
        tag_lines = [
            open_tag(BoundaryKind.DEPLOYED, spec.name) + "\n",
            close_tag(BoundaryKind.DEPLOYED, spec.name) + "\n",
        ]
        working_lines = working_lines[:insert_idx] + tag_lines + working_lines[insert_idx:]
        deployed_added.append(spec.name)

        # Update section span to account for the two inserted lines
        section = SectionSpan(
            name=section.name,
            heading_line=section.heading_line,
            start=section.start,
            content_end=section.content_end + 2,
            end=section.end + 2,
        )
        # Update all remaining sections (needed for subsequent specs that share the section)
        # We rebuild sections mapping with updated span
        sections = dict(sections)
        sections[spec.parent_section] = section

    return RegionInsertionResult(
        lines=working_lines,
        deployed_added=deployed_added,
        deletions_applied=deletions_applied,
        deletions_unmatched=deletions_unmatched,
        non_conformances=non_conformances,
    )


def _relocate_section(lines: list[str], old_section: SectionSpan) -> SectionSpan:
    """After a text-rebuild deletion pass, re-locate the section heading and
    recompute content_end from the new line list.

    The heading text is unchanged; we scan for it by matching the same heading
    type (H1 for Identity, H2 for others) by prefix.
    """
    # Find the heading line again by scanning from 0
    # Use SECTION_HEADING_MAP prefix
    heading_prefix = SECTION_HEADING_MAP.get(old_section.name, "")
    for i, line in enumerate(lines):
        stripped = line.rstrip("\n")
        if heading_prefix == "# ":
            # H1 match
            if stripped.startswith("# ") and not stripped.startswith("##"):
                heading_line = i
                start = i + 1
                # Find content_end: last non-blank line before --- or next section
                content_end = start
                end = len(lines)
                for j in range(start, len(lines)):
                    ln = lines[j].strip()
                    if ln == "---":
                        end = j + 1
                        break
                    if ln.startswith("## ") and not ln.startswith("###"):
                        end = j
                        break
                # content_end = last non-blank line index + 1 before end,
                # not counting [[/SECTION:...]] close tags
                ce = start
                for j in range(start, end):
                    s = lines[j].strip()
                    if not s or s == "---":
                        continue
                    _m = TAG_PATTERN.match(s)
                    if _m is not None and _m.group("close") == "/" and _m.group("name") == tag_base_name(old_section.name):
                        break
                    ce = j + 1
                return SectionSpan(
                    name=old_section.name,
                    heading_line=heading_line,
                    start=start,
                    content_end=ce,
                    end=end,
                )
        else:
            # Match the exact heading text (e.g. "## Constraints") so that two
            # H2 sections with the same prefix never confuse each other.
            if stripped == heading_prefix:
                heading_line = i
                start = i + 1
                content_end = start
                end = len(lines)
                for j in range(start, len(lines)):
                    ln = lines[j].strip()
                    if ln == "---":
                        end = j + 1
                        break
                    if ln.startswith("## ") and not ln.startswith("###") and j > start:
                        end = j
                        break
                ce = start
                for j in range(start, end):
                    s = lines[j].strip()
                    if not s or s == "---":
                        continue
                    _m = TAG_PATTERN.match(s)
                    if _m is not None and _m.group("close") == "/" and _m.group("name") == tag_base_name(old_section.name):
                        break
                    ce = j + 1
                return SectionSpan(
                    name=old_section.name,
                    heading_line=heading_line,
                    start=start,
                    content_end=ce,
                    end=end,
                )
    # Fallback: return old section unchanged
    return old_section


def locate_process_list_end(lines: Sequence[str], identity: SectionSpan) -> int | None:
    """Return the index one past the last line of the Identity section's Process
    list, or None when no Process list is found.

    A Process list is the run of ordered-list items ('N. ') and their
    continuation lines following a heading whose text is 'Process' (any heading
    level), outside any fence.
    """
    mask = fence_mask(lines)

    # Find a Process heading inside the identity section
    process_heading_idx = None
    for i in range(identity.start, identity.content_end):
        if mask[i]:
            continue
        line = lines[i].strip()
        # Match any heading level: ##+ Process
        if re.match(r"^#+\s+Process\s*$", line):
            process_heading_idx = i
            break

    if process_heading_idx is None:
        return None

    # Find the last numbered list item (and continuation lines) after the heading,
    # stopping at the next heading of any level (e.g. ### Authority Hierarchy).
    last_list_line = None
    for i in range(process_heading_idx + 1, identity.content_end):
        if mask[i]:
            continue
        line = lines[i]
        stripped = line.strip()
        # Stop at any heading (another subsection starts — no longer in Process list)
        if stripped.startswith("#"):
            break
        # Numbered list item: starts with "N. "
        if re.match(r"^\d+\.", stripped):
            last_list_line = i
        elif last_list_line is not None and stripped and line[:1] in (' ', '\t'):
            # Indented continuation line belonging to the previous numbered item
            last_list_line = i

    if last_list_line is None:
        return None

    return last_list_line + 1


def find_section_spans(lines: Sequence[str]) -> dict[str, SectionSpan]:
    """Locate canonical sections by heading, fence-aware.

    Wraps the existing heading table (SECTION_HEADING_MAP). content_end is the
    index one past the last non-blank content line, excluding a trailing '---'
    separator.
    """
    mask = fence_mask(lines)
    result: dict[str, SectionSpan] = {}

    # Find heading positions
    heading_positions: list[tuple[str, int]] = []  # (section_name, line_idx)

    for i, line in enumerate(lines):
        if mask[i]:
            continue
        stripped = line.rstrip("\n")
        for section_name, prefix in SECTION_HEADING_MAP.items():
            if prefix == "# ":
                if stripped.startswith("# ") and not stripped.startswith("##"):
                    heading_positions.append((section_name, i))
                    break
            else:
                if stripped.startswith(prefix):
                    heading_positions.append((section_name, i))
                    break

    # For each heading, compute start, content_end, end
    for k, (section_name, heading_idx) in enumerate(heading_positions):
        start = heading_idx + 1

        # Upper bound: start of next heading (or end of file)
        if k + 1 < len(heading_positions):
            next_heading_idx = heading_positions[k + 1][1]
        else:
            next_heading_idx = len(lines)

        # Find the last '---' separator between this heading and the next.
        # Scan FORWARD so that boundary tags like [[SECTION:...]] inserted between
        # sections do not prematurely stop the search. We want the last '---' that
        # appears strictly before next_heading_idx.
        sep_idx = None
        for j in range(heading_idx + 1, next_heading_idx):
            if lines[j].strip() == "---":
                sep_idx = j

        if sep_idx is not None:
            end = sep_idx + 1
        else:
            end = next_heading_idx

        # content_end: one past the last non-blank, non-separator content line,
        # stopping before the section's own close tag which is structural.
        content_end = start
        for j in range(start, end):
            stripped = lines[j].strip()
            if not stripped or stripped == "---":
                continue
            # Stop at the section's own closing tag — don't count it as content
            _cm = TAG_PATTERN.match(stripped)
            if _cm is not None and _cm.group("close") == "/" and _cm.group("name") == tag_base_name(section_name):
                break
            content_end = j + 1

        # Ensure invariant: start <= content_end
        if content_end < start:
            content_end = start

        result[section_name] = SectionSpan(
            name=section_name,
            heading_line=heading_idx,
            start=start,
            content_end=content_end,
            end=end,
        )

    return result


def move_artifact_structure_block(
    lines: Sequence[str],
    sections: Mapping[str, SectionSpan],
    *,
    region_name: str = ARTIFACT_TEMPLATE_REGION_NAME,
    parent_section: str = "Capabilities",
) -> ArtifactTemplateMoveResult:
    """Locate an artifact-structure H3 block in Capabilities and move it into the
    OutputArtifactTemplate region as project-region default content.

    Pure: reads and writes no files; mutates neither argument.

    The function is a no-op (moved=False, lines unchanged) in four cases:
    - The parent_section is absent from sections.
    - No OutputArtifactTemplate open/close tag pair exists within Capabilities.
    - The OutputArtifactTemplate region already contains non-blank content.
    - No H3 heading matching ARTIFACT_STRUCTURE_HEADING_PATTERN exists within Capabilities.

    When a move succeeds the content appears exactly once in the output: removed
    from its original position and inserted immediately after the open tag.
    Trailing blank lines are trimmed from the moved block; the heading line itself
    is retained as the first line of the moved content.
    """
    cap = sections.get(parent_section)
    if cap is None:
        return ArtifactTemplateMoveResult(lines=list(lines), moved=False, heading_text=None)

    lines_list: list[str] = list(lines)
    mask: list[bool] = fence_mask(lines_list)

    open_t = open_tag(BoundaryKind.INJECTION, region_name)
    close_t = close_tag(BoundaryKind.INJECTION, region_name)

    # Locate the OutputArtifactTemplate open and close tags within Capabilities.
    open_idx: int | None = None
    close_idx: int | None = None
    for i in range(cap.start, cap.content_end):
        stripped = lines_list[i].strip()
        if open_idx is None and stripped == open_t:
            open_idx = i
        elif open_idx is not None and stripped == close_t:
            close_idx = i
            break

    if open_idx is None or close_idx is None:
        # No region pair found — leave block in place.
        return ArtifactTemplateMoveResult(lines=list(lines), moved=False, heading_text=None)

    # Occupied-region guard: when the region already has non-blank content, do nothing.
    for i in range(open_idx + 1, close_idx):
        if lines_list[i].strip():
            return ArtifactTemplateMoveResult(lines=list(lines), moved=False, heading_text=None)

    # Find the artifact-structure heading within [cap.start, cap.content_end).
    # Scoping the search to the Capabilities span is a contract, not an optimisation.
    block_start: int | None = None
    for i in range(cap.start, cap.content_end):
        if mask[i]:
            continue
        if _ARTIFACT_STRUCTURE_HEADING_RE.match(lines_list[i].rstrip("\n")):
            block_start = i
            break

    if block_start is None:
        return ArtifactTemplateMoveResult(lines=list(lines), moved=False, heading_text=None)

    heading_text: str = lines_list[block_start].strip()

    # Compute block extent: from heading through the line before the first
    # subsequent unfenced heading of level ≤ 3, MOSAIC boundary tag, or
    # cap.content_end, whichever comes first.
    block_end: int = cap.content_end
    for i in range(block_start + 1, cap.content_end):
        if mask[i]:
            continue
        stripped = lines_list[i].strip()
        if stripped.startswith("#"):
            nl = 0
            for ch in stripped:
                if ch == "#":
                    nl += 1
                else:
                    break
            if nl <= 3:
                block_end = i
                break
        if TAG_PATTERN.match(stripped) is not None:
            block_end = i
            break

    # Trim trailing blank lines from the block (spec contract).
    actual_end: int = block_end
    while actual_end > block_start + 1 and not lines_list[actual_end - 1].strip():
        actual_end -= 1

    block_lines: list[str] = lines_list[block_start:actual_end]

    # Remove block from its original position and insert after open_idx,
    # handling both orderings (block before region and block after region).
    if block_start < open_idx:
        # Block is before the region pair.
        removal_count = actual_end - block_start
        after_removal = lines_list[:block_start] + lines_list[actual_end:]

        # Blank-line hygiene: collapse a double blank created by the removal.
        gap_pos = block_start
        before_blank = gap_pos > 0 and after_removal[gap_pos - 1].strip() == ""
        after_blank = (
            gap_pos < len(after_removal) and after_removal[gap_pos].strip() == ""
        )
        new_open_idx = open_idx - removal_count
        if before_blank and after_blank:
            after_removal = after_removal[:gap_pos] + after_removal[gap_pos + 1:]
            new_open_idx -= 1

        result_lines = (
            after_removal[:new_open_idx + 1]
            + block_lines
            + after_removal[new_open_idx + 1:]
        )
    else:
        # Block is after the region pair (block_start > close_idx).
        # Insert the block after open_idx first, then remove from its (now-shifted)
        # original position.
        after_insert = (
            lines_list[:open_idx + 1]
            + block_lines
            + lines_list[open_idx + 1:]
        )
        # block_start shifted forward by len(block_lines) due to insertion before it.
        shift = len(block_lines)
        new_block_start = block_start + shift
        new_actual_end = actual_end + shift
        after_removal = after_insert[:new_block_start] + after_insert[new_actual_end:]

        # Blank-line hygiene: collapse a double blank at the removal gap.
        gap_pos = new_block_start
        before_blank = gap_pos > 0 and after_removal[gap_pos - 1].strip() == ""
        after_blank = (
            gap_pos < len(after_removal) and after_removal[gap_pos].strip() == ""
        )
        if before_blank and after_blank:
            after_removal = after_removal[:gap_pos] + after_removal[gap_pos + 1:]

        result_lines = after_removal

    return ArtifactTemplateMoveResult(
        lines=result_lines,
        moved=True,
        heading_text=heading_text,
    )
