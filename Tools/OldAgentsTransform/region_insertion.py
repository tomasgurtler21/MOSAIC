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
    SECTION_HEADING_MAP,
    open_tag,
    close_tag,
)
from fence import fence_mask, first_unfenced, safe_insertion_index
from non_conformance import NC_DRIFTED_BULLET, NC_MESSAGES, NonConformance


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
        r"When `human_in_the_loop: true`, present all output artifacts to the user "
        r"for review/approval \(final action before returning response\)"
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
    drift_probe=None,
)

# ---------------------------------------------------------------------------
# Deletion rules for Stage 3 regions
# ---------------------------------------------------------------------------

_EH_RETRY = DeletionRule(
    rule_id="EH-retry",
    kind=DeletionKind.REGEX_BULLET,
    pattern=r"Retry a transient error once",
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
    pattern=r"Context Management.*Follow-up work is handled by spawning new agent instances",
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
    kind=DeletionKind.EXACT_BULLET,
    pattern=(
        "**Orchestration Artifacts:** NEVER access an orchestration artifact"
        " that is not named in your `input_artifacts`/`output_artifacts`"
    ),
    required=True,
    drift_probe=None,
)

_PC_BULLET_2 = DeletionRule(
    rule_id="PC-bullet-2",
    kind=DeletionKind.EXACT_BULLET,
    pattern=(
        "**Project Files:** You MAY read, modify, or create any project file"
        " — anything not named as an orchestration artifact"
    ),
    required=True,
    drift_probe=None,
)

_PC_BULLET_3 = DeletionRule(
    rule_id="PC-bullet-3",
    kind=DeletionKind.EXACT_BULLET,
    pattern="NEVER skip the JSON response block",
    required=True,
    drift_probe=None,
)

_PC_BULLET_4 = DeletionRule(
    rule_id="PC-bullet-4",
    kind=DeletionKind.EXACT_BULLET,
    pattern="NEVER invent status codes",
    required=True,
    drift_probe=None,
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


def _find_heading_block_end(lines: Sequence[str], heading_idx: int, mask: list[bool]) -> int:
    """Return the index one past the heading block starting at heading_idx.

    For a HEADING_BLOCK rule, the block runs from the heading through every
    line up to (but not including) the next heading of the same or higher level,
    outside any fence.

    heading_idx must point to a line starting with '### ' (H3).
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
    # or a boundary tag ([[INJECTION:, [[DEPLOYED:, [[SECTION:, [[/...) — those
    # mark regions that the deletion must not cross.
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
        # Stop at any boundary tag — don't delete across region boundaries
        if line.startswith("[[") and (
            line.startswith("[[INJECTION:")
            or line.startswith("[[/INJECTION:")
            or line.startswith("[[DEPLOYED:")
            or line.startswith("[[/DEPLOYED:")
            or line.startswith("[[SECTION:")
            or line.startswith("[[/SECTION:")
        ):
            return i
    return len(lines)


def _apply_deletions(
    lines: list[str],
    section: SectionSpan,
    rules: tuple[DeletionRule, ...],
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
                    block_end = _find_heading_block_end(lines, i, mask)
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
    """Return True when a [[DEPLOYED:name]] open tag already exists in lines."""
    open = f"[[DEPLOYED:{name}]]"
    for line in lines:
        if line.strip() == open:
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
            close = f"[[/{kind.value}:{ref_name}]]"
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
            open_t = f"[[{kind.value}:{ref_name}]]"
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
                working_lines, section, spec.supersedes
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
            f"[[DEPLOYED:{spec.name}]]\n",
            f"[[/DEPLOYED:{spec.name}]]\n",
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
                    if s.startswith("[[/SECTION:"):
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
                    if s.startswith("[[/SECTION:"):
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
        # stopping before any [[/SECTION:...]] close tag which is structural.
        content_end = start
        for j in range(start, end):
            stripped = lines[j].strip()
            if not stripped or stripped == "---":
                continue
            # Stop at a closing section tag — don't count it as content
            if stripped.startswith("[[/SECTION:"):
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
