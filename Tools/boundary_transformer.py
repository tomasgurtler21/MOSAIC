"""Boundary Transformation Tool for MOSAIC agent instruction files.

CLI usage:
    python Tools/boundary_transformer.py <input.md> [--output <output.md>] [--generic-ref <generic.md>]

Exit codes:
    0 -- transformation succeeded
    1 -- error (unclassifiable content, malformed input, missing --generic-ref, file I/O error)
"""
from __future__ import annotations

import argparse
import dataclasses
import pathlib
import re
import sys
from typing import Optional

from boundary_constants import (
    BoundaryKind,
    CANONICAL_SECTIONS,
    EXPECTED_MARKER,
    INJECTION_OLD_MARKER_MAP,
    MARKER_TO_INJECTION_NAME,
    SECTION_HEADING_MAP,
    TAG_PATTERN,
    close_tag,
    open_tag,
)

# Matches a top-level YAML frontmatter key at the start of a line (no leading
# whitespace). Indented lines belong to a multi-line value (list items or nested
# map entries) and are intentionally NOT matched.
_FRONTMATTER_KEY_RE = re.compile(r"^([A-Za-z_][A-Za-z0-9_]*)\s*:")


@dataclasses.dataclass
class TransformError:
    """A single error found during transformation."""
    line_number: int
    message: str


@dataclasses.dataclass
class TransformResult:
    """Return value from transform_file."""
    success: bool
    errors: list[TransformError]
    sections_added: list[str]      # Names of SECTION boundaries added
    injections_added: list[str]    # Names of INJECTION boundaries added
    version_before: str            # Original version string
    version_after: str             # New version string after bump


def transform_file(
    input_path: pathlib.Path,
    output_path: Optional[pathlib.Path] = None,
    generic_ref_path: Optional[pathlib.Path] = None,
) -> TransformResult:
    """Transform a MOSAIC agent instruction file by adding boundary tags.

    Reads the file at input_path, adds [[SECTION:...]] and [[INJECTION:...]]
    boundary tags around all sections and injection points, bumps the major
    version number, and writes the result.

    Args:
        input_path: Path to the agent .md file to transform.
        output_path: Where to write the result. If None, overwrites input_path.
        generic_ref_path: Path to the generic counterpart file. Required when
            transforming harness files (CodebaseAgnostic or ExampleProject
            variants) so the tool can locate injection content by comparing
            against the generic source. Ignored for generic files.

    Returns:
        TransformResult with success status and any errors encountered.
        If generic_ref_path is None and the file is detected as a harness
        file (has transform_version in frontmatter), returns
        TransformResult(success=False, errors=[TransformError(...)]) immediately
        without writing output.
    """
    if output_path is None:
        output_path = input_path

    content = input_path.read_text(encoding="utf-8")
    lines = content.splitlines(keepends=True)

    # Parse frontmatter
    frontmatter_result = _parse_frontmatter(lines)
    if not frontmatter_result["success"]:
        return TransformResult(
            success=False,
            errors=[TransformError(
                line_number=frontmatter_result.get("line_number", 1),
                message=frontmatter_result["error"]
            )],
            sections_added=[],
            injections_added=[],
            version_before="",
            version_after=""
        )

    frontmatter = frontmatter_result["frontmatter"]
    frontmatter_end = frontmatter_result["end_line"]
    version_before = frontmatter.get("version", "")

    # Check if this is a harness file
    is_harness = "transform_version" in frontmatter
    if is_harness and generic_ref_path is None:
        return TransformResult(
            success=False,
            errors=[TransformError(
                line_number=1,
                message="Harness file detected (transform_version in frontmatter) but --generic-ref not provided"
            )],
            sections_added=[],
            injections_added=[],
            version_before=version_before,
            version_after=""
        )

    # Compute bumped version values. These are applied to the verbatim
    # frontmatter block below; no other field is touched.
    version_after = _bump_version(version_before)
    transform_version_after = None
    if "transform_version" in frontmatter:
        transform_version_after = _bump_version(frontmatter["transform_version"])

    # Parse body to identify sections and injections
    body_lines = lines[frontmatter_end:]

    if is_harness:
        # Load and parse generic reference
        generic_content = generic_ref_path.read_text(encoding="utf-8")
        generic_lines = generic_content.splitlines(keepends=True)
        generic_fm_result = _parse_frontmatter(generic_lines)
        generic_body_lines = generic_lines[generic_fm_result["end_line"]:]

        transformed_body = _transform_harness_body(body_lines, generic_body_lines)
    else:
        transformed_body = _transform_generic_body(body_lines)

    if not transformed_body["success"]:
        return TransformResult(
            success=False,
            errors=transformed_body["errors"],
            sections_added=[],
            injections_added=[],
            version_before=version_before,
            version_after=version_after
        )

    # Write output. The frontmatter is emitted verbatim -- every field is
    # preserved in its original order and formatting (including multi-line
    # `permission:` maps and `mcpServers:` lists) -- with only the version and
    # transform_version VALUES rewritten in place.
    output_lines = []
    output_lines.append("---\n")
    for raw in frontmatter_result["raw_lines"]:
        eol = "\n" if raw.endswith("\n") else ""
        if raw[:1] not in (" ", "\t") and raw.strip():
            match = _FRONTMATTER_KEY_RE.match(raw.strip())
            if match:
                key = match.group(1)
                if key == "version":
                    output_lines.append(f"version: {version_after}{eol}")
                    continue
                if key == "transform_version" and transform_version_after is not None:
                    output_lines.append(f"transform_version: {transform_version_after}{eol}")
                    continue
        output_lines.append(raw)
    output_lines.append("---\n")
    output_lines.extend(transformed_body["lines"])

    output_path.write_text("".join(output_lines), encoding="utf-8")

    return TransformResult(
        success=True,
        errors=[],
        sections_added=transformed_body["sections_added"],
        injections_added=transformed_body["injections_added"],
        version_before=version_before,
        version_after=version_after
    )


def _parse_frontmatter(lines: list[str]) -> dict:
    """Parse YAML frontmatter from lines.

    The frontmatter block is preserved verbatim (returned as ``raw_lines``); the
    transformer only rewrites the ``version`` / ``transform_version`` values and
    never reorders, drops, or reformats any other field. Only top-level scalar
    keys are parsed into ``frontmatter`` -- enough to detect the version and
    whether the file is a harness variant. Indented lines belong to a multi-line
    value (e.g. a ``permission:`` map or an ``mcpServers:`` list) and are left
    untouched.

    Returns dict with:
        success: bool
        frontmatter: dict of top-level key -> value string (if success)
        raw_lines: list[str] verbatim frontmatter content lines, excluding the
            surrounding '---' delimiters (if success)
        end_line: int (line index after closing ---; if success)
        error: str (if not success)
        line_number: int (if not success)
    """
    if not lines or lines[0].strip() != "---":
        return {
            "success": False,
            "error": "Missing opening --- for frontmatter",
            "line_number": 1
        }

    # Find closing ---
    closing_line = None
    for i in range(1, len(lines)):
        if lines[i].strip() == "---":
            closing_line = i
            break

    if closing_line is None:
        return {
            "success": False,
            "error": "Missing closing --- for frontmatter",
            "line_number": 1
        }

    # Parse only top-level keys. A top-level key line has no leading whitespace;
    # indented lines are part of a multi-line value and are skipped here (their
    # bytes are still preserved via raw_lines).
    frontmatter = {}
    for i in range(1, closing_line):
        raw = lines[i]
        if not raw.strip():
            continue
        if raw[:1] in (" ", "\t"):
            continue  # nested list item / map entry -- part of a multi-line value
        stripped = raw.strip()
        match = _FRONTMATTER_KEY_RE.match(stripped)
        if not match:
            return {
                "success": False,
                "error": f"Malformed YAML line: {stripped}",
                "line_number": i + 1
            }
        key = match.group(1)
        value = stripped[match.end():].strip()
        frontmatter[key] = value

    if "version" not in frontmatter:
        return {
            "success": False,
            "error": "Missing 'version' field in frontmatter",
            "line_number": 1
        }

    return {
        "success": True,
        "frontmatter": frontmatter,
        "raw_lines": lines[1:closing_line],
        "end_line": closing_line + 1
    }


def _bump_version(version_str: str) -> str:
    """Bump major version, reset minor and patch to 0."""
    parts = version_str.split(".")
    if len(parts) >= 1:
        major = int(parts[0])
        return f"{major + 1}.0.0"
    return "1.0.0"


def _transform_generic_body(lines: list[str]) -> dict:
    """Transform a generic file's body by adding section and injection boundaries.

    Returns dict with:
        success: bool
        lines: list[str] (if success)
        sections_added: list[str] (if success)
        injections_added: list[str] (if success)
        errors: list[TransformError] (if not success)
    """
    result_lines = []
    sections_added = []
    injections_added = []
    errors = []

    # Identify all sections first. Generic files use the strict Identity rule so
    # any non-canonical H2 immediately after Identity is flagged (FR-13).
    sections_result = _identify_sections(lines, strict_identity=True)
    sections = sections_result["sections"]
    errors.extend(sections_result["errors"])

    # If there are errors, return immediately
    if errors:
        return {
            "success": False,
            "errors": errors
        }

    current_section_idx = 0
    in_fenced_block = False
    i = 0

    while i < len(lines):
        line = lines[i]

        # Track fenced code blocks
        if line.strip().startswith("```"):
            in_fenced_block = not in_fenced_block
            result_lines.append(line)
            i += 1
            continue

        # Check if this is a section start
        if current_section_idx < len(sections) and i == sections[current_section_idx]["start_line"]:
            section = sections[current_section_idx]
            section_name = section["name"]
            sections_added.append(section_name)

            # Add section open tag
            result_lines.append(open_tag(BoundaryKind.SECTION, section_name) + "\n")

            # Process section content
            section_end = section["end_line"]
            section_content_start = i

            # Process lines within section
            j = i
            while j < section_end:
                line = lines[j]

                # Track fenced blocks within section
                if line.strip().startswith("```"):
                    in_fenced_block = not in_fenced_block
                    result_lines.append(line)
                    j += 1
                    continue

                # Check for injection/deployed markers
                injection_match = _match_region_marker(line)
                if injection_match:
                    injection_name = injection_match["name"]
                    marker_kind = injection_match["kind"]
                    injections_added.append(injection_name)

                    # Both standalone ("[INJECTION: name]") and list-item
                    # ("- [INJECTION: name]") markers become standalone boundary
                    # tags with no "- " prefix: a tag must occupy its own line to
                    # match TAG_PATTERN and be recognised by the validator.
                    result_lines.append(f"{open_tag(marker_kind, injection_name)}\n")
                    result_lines.append(f"{close_tag(marker_kind, injection_name)}\n")
                    j += 1
                    continue

                # Regular line
                result_lines.append(line)
                j += 1

            # Add section close tag. If the last content line has no trailing
            # newline (file ended without one), add it to keep the close tag
            # on its own line and parseable by TAG_PATTERN.
            if result_lines and not result_lines[-1].endswith("\n"):
                result_lines[-1] += "\n"
            result_lines.append(close_tag(BoundaryKind.SECTION, section_name) + "\n")

            i = section_end
            current_section_idx += 1
            continue

        # Line outside any section (separator, blank line)
        result_lines.append(line)
        i += 1

    return {
        "success": True,
        "lines": result_lines,
        "sections_added": sections_added,
        "injections_added": injections_added
    }


def _identify_sections(lines: list[str], strict_identity: bool = False) -> dict:
    """Identify all section boundaries in the body.

    Args:
        lines: body lines (after frontmatter).
        strict_identity: when True (generic transform path), the Identity section
            ends at the first H2 heading that follows it -- canonical or not --
            so a stray non-canonical H2 right after Identity is left uncovered and
            reported as unclassifiable. When False (harness path), Identity ends
            only before the next CANONICAL heading, because harness orchestrators
            legitimately carry workflow H2 headings inside Identity as
            AvailableWorkflows fill content.

    Returns dict with:
        sections: list of dicts with:
            name: str (canonical section name)
            start_line: int (line index of the heading)
            end_line: int (line index after the closing ---, or len(lines))
        errors: list of TransformError (non-canonical H2 headings)
    """
    sections = []
    errors = []
    in_fenced_block = False

    # Find all canonical section headings. Non-canonical H2 headings are NOT
    # treated as section starts: they are subsections that belong as prose
    # inside the enclosing canonical section (e.g. the orchestrator's
    # "## Core Orchestration Loop" lives inside the ErrorHandling section).
    # all_h2_lines records every H2 heading (canonical or not) so the strict
    # Identity rule can end Identity at the first following H2.
    section_starts = []
    all_h2_lines = []
    for i, line in enumerate(lines):
        if line.strip().startswith("```"):
            in_fenced_block = not in_fenced_block
            continue

        if in_fenced_block:
            continue

        # Check for H2 headings
        if line.startswith("## ") and not line.startswith("###"):
            all_h2_lines.append(i)
            section_name = _canonical_section_for_heading(line)
            if section_name is not None:
                section_starts.append({"name": section_name, "line": i})
            # Non-canonical H2: left for orphan detection / absorption below.

        # Check for H1 heading (Identity section)
        elif line.startswith("# ") and not line.startswith("##"):
            section_starts.append({"name": "Identity", "line": i})

    # Determine end lines.
    for idx, section_start in enumerate(section_starts):
        start_line = section_start["line"]

        if section_start["name"] == "Identity" and idx + 1 < len(section_starts):
            # Identity ends at the --- separator immediately before its boundary
            # heading. On the harness path the boundary heading is the next
            # canonical heading: this lets harness orchestrators keep workflow H2
            # headings (and the --- separators between them) inside Identity as
            # AvailableWorkflows fill content. On the strict generic path the
            # boundary heading is the FIRST following H2 of any kind, so a stray
            # non-canonical H2 right after Identity (which never occurs in a
            # legitimate generic file) is left uncovered and reported as
            # unclassifiable instead of being silently absorbed.
            boundary_line = section_starts[idx + 1]["line"]
            if strict_identity:
                for h2 in all_h2_lines:
                    if h2 > start_line:
                        boundary_line = h2
                        break
            end_line = boundary_line
            for i in range(boundary_line - 1, start_line, -1):
                if lines[i].strip() == "---":
                    end_line = i
                    break
        elif section_start["name"] == "Identity":
            # Identity is the only section -- absorb everything to EOF.
            end_line = len(lines)
        elif idx + 1 < len(section_starts):
            # A canonical H2 section extends to the --- separator immediately
            # before the next canonical heading, absorbing any non-canonical
            # subsections in between.
            next_section_line = section_starts[idx + 1]["line"]
            end_line = next_section_line
            for i in range(next_section_line - 1, start_line, -1):
                if lines[i].strip() == "---":
                    end_line = i
                    break
        else:
            # Last section - goes to EOF
            end_line = len(lines)

        sections.append({
            "name": section_start["name"],
            "start_line": start_line,
            "end_line": end_line
        })

    # Orphan detection: any non-blank, non-separator line that falls outside
    # every section boundary is content the tool cannot classify.
    covered = [False] * len(lines)
    for section in sections:
        for i in range(section["start_line"], min(section["end_line"], len(lines))):
            covered[i] = True

    in_fenced_block = False
    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("```"):
            in_fenced_block = not in_fenced_block
        if covered[i] or not stripped or stripped == "---":
            continue
        if line.startswith("## ") and not line.startswith("###"):
            errors.append(TransformError(
                line_number=i + 1,
                message=f"unclassifiable heading: {stripped}"
            ))
        else:
            errors.append(TransformError(
                line_number=i + 1,
                message=f"unclassifiable content: {stripped}"
            ))

    return {"sections": sections, "errors": errors}


def _canonical_section_for_heading(line: str) -> Optional[str]:
    """Return the canonical section name for a heading line, or None.

    Matches the line against SECTION_HEADING_MAP for the H2 canonical
    sections (Identity/H1 is handled separately by the caller).
    """
    for section_name in CANONICAL_SECTIONS:
        heading = SECTION_HEADING_MAP[section_name]
        if heading == "# ":
            continue  # Identity handled by the caller.
        if line.strip() == heading.strip():
            return section_name
    return None


def _match_region_marker(line: str) -> Optional[dict]:
    """Check if line is an injection or deployed marker (old- or new-style OPEN tag only).

    Matches:
    - Old-style standalone: "[INJECTION: name]"
    - Old-style list-item:  "- [INJECTION: name]"
    - New-style INJECTION open tag: "[[INJECTION:Name]]"
    - New-style DEPLOYED open tag:  "[[DEPLOYED:Name]]" (present in already-transformed
      generic reference files for tool-managed names)

    Does NOT match new-style close tags — those are handled as generic-only lines
    and dropped.

    Returns dict with:
        name: str (canonical name)
        kind: BoundaryKind (INJECTION or DEPLOYED)
        is_list_item: bool
    or None if no match.
    """
    stripped = line.strip()

    # Try list-item format first: "- [INJECTION: name]"
    if stripped.startswith("- "):
        marker_part = stripped[2:].strip()
        if marker_part in MARKER_TO_INJECTION_NAME:
            name = MARKER_TO_INJECTION_NAME[marker_part]
            kind = EXPECTED_MARKER.get(name, BoundaryKind.INJECTION)
            return {
                "name": name,
                "kind": kind,
                "is_list_item": True,
            }

    # Try old-style standalone format: "[INJECTION: name]"
    if stripped in MARKER_TO_INJECTION_NAME:
        name = MARKER_TO_INJECTION_NAME[stripped]
        kind = EXPECTED_MARKER.get(name, BoundaryKind.INJECTION)
        return {
            "name": name,
            "kind": kind,
            "is_list_item": False,
        }

    # Try new-style open tag: "[[INJECTION:Name]]" or "[[DEPLOYED:Name]]"
    # (present in already-transformed generic reference files)
    m = TAG_PATTERN.match(stripped)
    if m and m.group("kind") in ("INJECTION", "DEPLOYED") and not m.group("close"):
        return {
            "name": m.group("name"),
            "kind": BoundaryKind(m.group("kind")),
            "is_list_item": False,
        }

    return None


# Keep the old name as an alias for backward compatibility with any external callers.
_match_injection_marker = _match_region_marker


def _transform_harness_body(harness_lines: list[str], generic_lines: list[str]) -> dict:
    """Transform a harness file's body using the generic reference.

    A harness file is the generic file with some or all injection markers either
    left in place (empty injection) or replaced by concrete fill content. This
    function reconstructs the injection boundaries by walking the harness body
    section by section and, within each section, aligning the harness lines
    against the generic section's injection markers (see _merge_section).
    """
    harness_sections_result = _identify_sections(harness_lines)
    sections = harness_sections_result["sections"]
    errors = harness_sections_result["errors"]

    if errors:
        return {"success": False, "errors": errors}

    generic_sections_result = _identify_sections(generic_lines)
    generic_by_name = {gs["name"]: gs for gs in generic_sections_result["sections"]}

    result_lines = []
    sections_added = []
    injections_added = []

    harness_idx = 0

    for section in sections:
        # Emit any lines that sit between sections (separators, blanks) verbatim.
        while harness_idx < section["start_line"]:
            result_lines.append(harness_lines[harness_idx])
            harness_idx += 1

        sections_added.append(section["name"])
        result_lines.append(open_tag(BoundaryKind.SECTION, section["name"]) + "\n")

        harness_section = harness_lines[section["start_line"]:section["end_line"]]
        generic_section_meta = generic_by_name.get(section["name"])
        if generic_section_meta is not None:
            generic_section = generic_lines[
                generic_section_meta["start_line"]:generic_section_meta["end_line"]
            ]
            merged, injs = _merge_section(harness_section, generic_section)
            result_lines.extend(merged)
            injections_added.extend(injs)
        else:
            result_lines.extend(harness_section)

        result_lines.append(close_tag(BoundaryKind.SECTION, section["name"]) + "\n")
        harness_idx = section["end_line"]

    while harness_idx < len(harness_lines):
        result_lines.append(harness_lines[harness_idx])
        harness_idx += 1

    return {
        "success": True,
        "lines": result_lines,
        "sections_added": sections_added,
        "injections_added": injections_added,
    }


def _is_h3_heading(line: str) -> bool:
    """Return True for a level-3 Markdown heading ('### ...'), not deeper."""
    return line.startswith("### ") and not line.startswith("#### ")


def _next_generic_anchor(generic_section: list[str], start: int) -> Optional[str]:
    """Return the next shared content line in the generic section at/after start.

    A "shared" line is one that both harness and generic files have verbatim.
    We skip blank lines, old-style injection markers, and any new-style
    SECTION/INJECTION boundary tags (open or close) -- those are generated by
    the transformer and are not present in the untransformed harness.
    """
    for i in range(start, len(generic_section)):
        line = generic_section[i]
        if not line.strip():
            continue
        if _match_region_marker(line):
            continue
        # Skip any new-style boundary tags ([[SECTION:...]], [[/SECTION:...]],
        # [[/INJECTION:...]], [[/DEPLOYED:...]]) that appear in already-transformed
        # generic refs.
        if TAG_PATTERN.match(line.strip()):
            continue
        return line
    return None


def _emit_injection(name: str, content: list[str], out: list[str],
                    kind: BoundaryKind = BoundaryKind.INJECTION) -> None:
    """Emit a region boundary wrapping the collected harness fill content.

    A leading '### subsection' heading (and the blank line after it) stays
    OUTSIDE the boundary; trailing blank lines also stay outside. The remaining
    lines become the region body. Empty content yields an empty boundary.

    kind controls whether to emit [[INJECTION:...]] or [[DEPLOYED:...]].
    """
    lead_end = 0
    if content and _is_h3_heading(content[0]):
        lead_end = 1
        while lead_end < len(content) and not content[lead_end].strip():
            lead_end += 1

    trail_start = len(content)
    while trail_start > lead_end and not content[trail_start - 1].strip():
        trail_start -= 1

    out.extend(content[:lead_end])
    out.append(open_tag(kind, name) + "\n")
    out.extend(content[lead_end:trail_start])
    out.append(close_tag(kind, name) + "\n")
    out.extend(content[trail_start:])


def _merge_section(harness_section: list[str], generic_section: list[str]) -> tuple[list[str], list[str]]:
    """Merge one section's harness lines with the generic section's injections.

    Returns (output_lines, injection_names_added).

    Walks the generic section as a stream of fixed lines and injection markers,
    consuming harness lines in parallel:

    - A fixed line shared by both is copied from the harness.
    - A marker still present in the harness becomes an empty boundary.
    - A marker replaced/removed in the harness has its fill content collected
      from the harness (stopping at the next marker, the next shared anchor
      line, or the next '### subsection' heading) and wrapped.
    - Generic-only blank lines are dropped, except a blank that trails an
      injection region (which is emitted to separate it from following content).
    """
    out: list[str] = []
    injections: list[str] = []
    gi = 0
    hi = 0

    while gi < len(generic_section) or hi < len(harness_section):
        gtok = generic_section[gi] if gi < len(generic_section) else None
        hline = harness_section[hi] if hi < len(harness_section) else None

        if gtok is not None and _match_region_marker(gtok):
            gtok_match = _match_region_marker(gtok)
            name = gtok_match["name"]
            marker_kind = gtok_match["kind"]
            harness_marker = _match_region_marker(hline) if hline is not None else None

            if harness_marker is not None and harness_marker["name"] == name:
                # Marker still present in the harness -> empty boundary.
                out.append(open_tag(marker_kind, name) + "\n")
                out.append(close_tag(marker_kind, name) + "\n")
                injections.append(name)
                gi += 1
                hi += 1
                continue

            # Marker removed in the harness: collect this region's fill content.
            anchor = _next_generic_anchor(generic_section, gi + 1)
            content: list[str] = []
            took_header = False
            has_body = False
            while hi < len(harness_section):
                hl = harness_section[hi]
                if _match_region_marker(hl):
                    break
                if anchor is not None and hl == anchor:
                    break
                if _is_h3_heading(hl):
                    if has_body or took_header:
                        break
                    took_header = True
                elif hl.strip():
                    has_body = True
                content.append(hl)
                hi += 1

            _emit_injection(name, content, out, kind=marker_kind)
            injections.append(name)
            gi += 1
            continue

        if gtok is not None and hline is not None and gtok == hline:
            out.append(hline)
            gi += 1
            hi += 1
            continue

        if gtok is not None and (hline is None or gtok != hline):
            # Generic-only line the harness lacks. Preserve a blank that trails
            # an injection region (so an empty/filled injection stays separated
            # from what follows); drop everything else the harness omitted.
            if not gtok.strip():
                prev_nonblank = None
                for k in range(gi - 1, -1, -1):
                    if generic_section[k].strip():
                        prev_nonblank = generic_section[k]
                        break
                if (prev_nonblank is not None
                        and _match_injection_marker(prev_nonblank)
                        and (not out or out[-1].strip())):
                    out.append(gtok)
            gi += 1
            continue

        # Harness-only line (extra blank or content not present in generic).
        out.append(hline)
        hi += 1

    return out, injections


def _main() -> int:
    """CLI entry point. Returns exit code."""
    parser = argparse.ArgumentParser(
        description="Transform a MOSAIC agent instruction .md file by adding boundary tags."
    )
    parser.add_argument("input", type=pathlib.Path, help="Path to the agent .md file to transform")
    parser.add_argument("--output", type=pathlib.Path, default=None,
                        help="Write result to this path instead of overwriting in-place")
    parser.add_argument("--generic-ref", type=pathlib.Path, default=None,
                        help="Path to the generic counterpart file (required for harness files)")
    args = parser.parse_args()

    result = transform_file(args.input, args.output, args.generic_ref)
    if not result.success:
        for err in result.errors:
            print(f"{args.input}:{err.line_number}: {err.message}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(_main())
