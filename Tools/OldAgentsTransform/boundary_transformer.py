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
import enum
import pathlib
import re
import sys
from collections.abc import Mapping
from typing import Optional

from boundary_constants import (
    BoundaryKind,
    CANONICAL_DEPLOYED,
    CANONICAL_SECTIONS,
    EXPECTED_MARKER,
    INJECTION_OLD_MARKER_MAP,
    MARKER_TO_INJECTION_NAME,
    SECTION_HEADING_MAP,
    TAG_PATTERN,
    close_tag,
    open_tag,
    tag_base_name,
)
from region_insertion import apply_conduct_regions, find_section_spans

import file_classification as _fc

from document_kind import classify_document as _classify_document
from non_conformance import NonConformance, detect_output_non_conformances
from frontmatter_build import (
    DerivedFrontmatter,
    build_output_frontmatter,
    derive_name,
    derive_required_skills,
    derive_role,
    read_generic_id,
)

# ---------------------------------------------------------------------------
# Module constants
# ---------------------------------------------------------------------------

# Default version assigned when the degraded path finds no `version` in frontmatter.
DEFAULT_VERSION: str = "1.0.0"


class SkipReason(enum.Enum):
    """Reason a file was skipped without transformation."""
    ALREADY_TRANSFORMED = "already-transformed"
    UTILITY_AGENT       = "utility-agent"
    NON_AGENT           = "non-agent-file"

# Warning texts emitted to stderr and carried in TransformResult.warnings.
WARN_NO_GENERIC_REF: str = (
    "No generic reference found for {path}; running degraded-quality transform"
)
WARN_ALREADY_TRANSFORMED: str = (
    "{path} already carries canonical boundary tags; skipping"
)

# The harness hard-error message, kept as a constant so both the single-file
# path and any test can assert it without duplicating the literal.
ERR_HARNESS_NO_GENERIC_REF: str = (
    "Harness file detected (transform_version in frontmatter) but --generic-ref not provided"
)

# Matches a top-level YAML frontmatter key at the start of a line (no leading
# whitespace). Indented lines belong to a multi-line value (list items or nested
# map entries) and are intentionally NOT matched.
_FRONTMATTER_KEY_RE = re.compile(r"^([A-Za-z_][A-Za-z0-9_-]*)\s*:")


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
    deployed_added: list[str]      # Names of DEPLOYED boundaries emitted
    version_before: str            # Original version string
    version_after: str             # New version string after bump
    # --- added by harness-only support; all default so existing constructions compile ---
    degraded: bool = False
    """True when the transform ran through the degraded (no generic reference) path."""
    skipped: bool = False
    """True when the file was left untouched because it is already transformed.
    When skipped is True, success is True, no output file was written, and
    version_after == version_before."""
    warnings: list[str] = dataclasses.field(default_factory=list)
    """Human-readable warnings emitted during this transform, in emission order."""
    skip_reason: SkipReason | None = None
    """When skipped is True, identifies why the file was skipped. None for a normal transform."""
    non_conformances: list[NonConformance] = dataclasses.field(default_factory=list)
    """Non-conformance findings produced during this transform (e.g. NC_TIER_PLACEHOLDER)."""


# ---------------------------------------------------------------------------
# Stage 1 detection helpers
# ---------------------------------------------------------------------------

def is_orchestrator_file(path: pathlib.Path) -> bool:
    """Return True when path names the orchestrator agent.

    Classification is canonical by filename: True if and only if the base name is
    exactly "orchestrator.md" or "orchestrator.agent.md" (compared case-insensitively).
    Frontmatter is not consulted; a `role: orchestrator` value in a file whose name does
    not match returns False.
    """
    name = path.name.lower()
    return name in ("orchestrator.md", "orchestrator.agent.md")


def has_canonical_boundary_tags(body: str) -> bool:
    """Return True when body already carries a structurally valid set of canonical
    [[SECTION:...]] boundary tags.

    True requires ALL of:
      - at least one [[SECTION:Name]] open tag whose Name is in
        boundary_constants.CANONICAL_SECTIONS;
      - every [[SECTION:Name]] open tag present has a matching [[/SECTION:Name]] close
        tag, and no close tag is unmatched;
      - every SECTION name present is in CANONICAL_SECTIONS;
      - every [[DEPLOYED:Name]] name present is in boundary_constants.CANONICAL_DEPLOYED
        and is likewise correctly paired.

    [[INJECTION:...]] names are open vocabulary and are not checked for membership, only
    for pairing.

    An incidental "[[...]]"-shaped string in prose that is not a well-formed tag line,
    an unpaired tag, or a non-canonical SECTION/DEPLOYED name each make this False.
    A tag is recognised only when it occupies a whole line (after stripping the line
    terminator), matching how the Go docformat lexer recognises tags.
    """
    canonical_sections_set = set(CANONICAL_SECTIONS)
    canonical_deployed_set = set(CANONICAL_DEPLOYED)

    open_sections: list[str] = []
    open_deployed: list[str] = []
    open_injections: list[str] = []
    has_canonical_section = False

    for line in body.splitlines():
        m = TAG_PATTERN.match(line)
        if m is None:
            continue

        is_close = bool(m.group("close"))
        kind = m.group("kind")
        name = m.group("name")

        if kind == "SECTION":
            if not is_close:
                # Membership check uses the base name so compound names like
                # 'Identity:Something' are still recognised as canonical Identity.
                if tag_base_name(name) not in canonical_sections_set:
                    return False
                open_sections.append(name)
                has_canonical_section = True
            else:
                # Pairing check uses the full name — the close tag must match
                # the exact open tag name, not just the base name.
                if not open_sections or open_sections[-1] != name:
                    return False
                open_sections.pop()

        elif kind == "DEPLOYED":
            if not is_close:
                # Membership check uses the base name for the same reason.
                if tag_base_name(name) not in canonical_deployed_set:
                    return False
                open_deployed.append(name)
            else:
                # Pairing check uses the full name.
                if not open_deployed or open_deployed[-1] != name:
                    return False
                open_deployed.pop()

        elif kind == "INJECTION":
            if not is_close:
                open_injections.append(name)
            else:
                if not open_injections or open_injections[-1] != name:
                    return False
                open_injections.pop()

    if open_sections or open_deployed or open_injections:
        return False

    return has_canonical_section


def default_version() -> str:
    """Return the version value to assign when frontmatter carries none.

    Returns DEFAULT_VERSION. Provided as a function rather than only a constant so the
    generation policy has one call site to change.
    """
    return DEFAULT_VERSION


def bump_version(version_str: str) -> str:
    """Return the minor-bumped version.

    'M.m.p' -> 'M.(m+1).0'.  '2.5.0' -> '2.6.0'.  '3' -> '3.1.0'.  '1.2' -> '1.3.0'.
    A string whose first dot-separated part is not a non-negative integer returns
    DEFAULT_VERSION.  Extra parts beyond the third are discarded.

    Replaces the always-major behaviour. `_bump_version` is retained as a thin
    alias delegating here so existing internal call sites keep working; the alias's
    behaviour changes with it, which is the intent.
    """
    if not version_str:
        return DEFAULT_VERSION
    parts = version_str.split(".")
    try:
        major = int(parts[0])
    except ValueError:
        return DEFAULT_VERSION
    if major < 0:
        return DEFAULT_VERSION
    try:
        minor = int(parts[1]) if len(parts) >= 2 else 0
    except ValueError:
        return DEFAULT_VERSION
    return f"{major}.{minor + 1}.0"


def resolve_version(frontmatter: Mapping[str, str]) -> str:
    """Return frontmatter['version'] when present and non-empty, else default_version().

    Applies on every path — generic, harness and degraded — replacing the
    hard-fail on a missing 'version'. A file carrying neither 'version' nor
    'transform_version' is therefore transformed, not rejected.
    """
    v = frontmatter.get("version", "")
    return v if v else default_version()


# ---------------------------------------------------------------------------
# Main transform entry point
# ---------------------------------------------------------------------------

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

    # Classify by filename before touching the file content.  Utility agents
    # and non-MOSAIC-agent files are skipped entirely — no output is written.
    _file_class = _fc.classify_file(input_path)
    if _fc.is_skippable(_file_class):
        msg = _fc.skip_message(input_path, _file_class)
        print(msg, file=sys.stderr)
        _skip_reason = (
            SkipReason.UTILITY_AGENT
            if _file_class is _fc.FileClass.UTILITY
            else SkipReason.NON_AGENT
        )
        return TransformResult(
            success=True,
            errors=[],
            sections_added=[],
            injections_added=[],
            deployed_added=[],
            version_before="",
            version_after="",
            skipped=True,
            skip_reason=_skip_reason,
            warnings=[msg],
        )

    content = input_path.read_text(encoding="utf-8")
    lines = content.splitlines(keepends=True)

    # Parse frontmatter leniently (without version requirement) so we can
    # detect transform_version and route the degraded path before deciding
    # whether to enforce the version field.
    frontmatter_result = _parse_frontmatter(lines, require_version=False)
    if not frontmatter_result["success"]:
        return TransformResult(
            success=False,
            errors=[TransformError(
                line_number=frontmatter_result.get("line_number", 1),
                message=frontmatter_result["error"]
            )],
            sections_added=[],
            injections_added=[],
            deployed_added=[],
            version_before="",
            version_after=""
        )

    frontmatter = frontmatter_result["frontmatter"]
    frontmatter_end = frontmatter_result["end_line"]

    # Check if this is a harness file (has transform_version in frontmatter).
    is_harness = "transform_version" in frontmatter

    # --- Degraded path dispatch ---
    # When a harness file arrives with no generic reference, we automatically
    # fall back to a degraded-quality transform rather than hard-failing.
    # Orchestrator files (detected by filename) are the sole exception.
    if is_harness and generic_ref_path is None:
        version_before_raw = frontmatter.get("version", "")

        # 1. Orchestrator check: preserve the existing hard-error for orchestrators.
        if is_orchestrator_file(input_path):
            return TransformResult(
                success=False,
                errors=[TransformError(
                    line_number=1,
                    message=ERR_HARNESS_NO_GENERIC_REF,
                )],
                sections_added=[],
                injections_added=[],
                deployed_added=[],
                version_before=version_before_raw,
                version_after="",
                degraded=False,
                skipped=False,
                warnings=[],
            )

        # 2. Idempotency guard: if the file already carries valid canonical
        #    boundary tags, skip it rather than re-running the transform.
        body_lines = lines[frontmatter_end:]
        body_str = "".join(body_lines)
        if has_canonical_boundary_tags(body_str):
            warn = WARN_ALREADY_TRANSFORMED.format(path=input_path)
            print(warn, file=sys.stderr)
            return TransformResult(
                success=True,
                errors=[],
                sections_added=[],
                injections_added=[],
                deployed_added=[],
                version_before=version_before_raw,
                version_after=version_before_raw,
                degraded=False,
                skipped=True,
                skip_reason=SkipReason.ALREADY_TRANSFORMED,
                warnings=[warn],
            )

        # 3. Degraded transform: apply the generic-body logic with non-strict
        #    identity classification. When version is absent, default_version()
        #    supplies a substitute so the transform can proceed.
        version_before = resolve_version(frontmatter)
        version_after = _bump_version(version_before)
        transform_version_after = _bump_version(frontmatter["transform_version"])

        transformed_body = _transform_generic_body(body_lines, strict_identity=False)
        if not transformed_body["success"]:
            return TransformResult(
                success=False,
                errors=transformed_body["errors"],
                sections_added=[],
                injections_added=[],
                deployed_added=[],
                version_before=version_before,
                version_after=version_after,
            )

        # Apply conduct-region insertion on the degraded path, matching the
        # behaviour of the non-degraded paths so that ClosingProcedure and
        # AuthorityHierarchy are emitted and their superseded prose is deleted.
        _body_for_regions = transformed_body["lines"]
        _section_spans = find_section_spans(_body_for_regions)
        _region_result = apply_conduct_regions(_body_for_regions, _section_spans)
        transformed_body = dict(transformed_body)
        transformed_body["lines"] = _region_result.lines
        transformed_body["deployed_added"] = (
            transformed_body.get("deployed_added", []) + _region_result.deployed_added
        )
        _region_ncs = [
            dataclasses.replace(nc, file=input_path)
            for nc in _region_result.non_conformances
        ]

        # Emit the degraded-path warning to stderr (also carried in the result).
        warn = WARN_NO_GENERIC_REF.format(path=input_path)
        print(warn, file=sys.stderr)

        # Build frontmatter using build_output_frontmatter so all Stage 6 rules
        # apply on the degraded path too: derived fields, tier placeholders, and
        # harness-only key handling.
        _deg_kind = _classify_document(list(frontmatter.keys()), frontmatter)
        _deg_derived = DerivedFrontmatter(
            name=derive_name(input_path),
            role=derive_role(input_path),
            required_skills=derive_required_skills(body_lines),
        )
        _deg_fm_lines, _deg_fm_ncs = build_output_frontmatter(
            frontmatter_result["raw_lines"],
            kind=_deg_kind,
            derived=_deg_derived,
            version_after=version_after,
            transform_version_after=transform_version_after,
            generic_id=None,
        )
        _deg_fm_ncs = [dataclasses.replace(nc, file=input_path) for nc in _deg_fm_ncs]

        _deg_final_sections = find_section_spans(transformed_body["lines"])
        _deg_output_ncs = detect_output_non_conformances(
            input_path, transformed_body["lines"], _deg_final_sections
        )

        output_lines = ["---\n"] + _deg_fm_lines + ["---\n"]
        output_lines.extend(transformed_body["lines"])

        output_path.write_text("".join(output_lines), encoding="utf-8")

        return TransformResult(
            success=True,
            errors=[],
            sections_added=transformed_body["sections_added"],
            injections_added=transformed_body["injections_added"],
            deployed_added=transformed_body.get("deployed_added", []),
            version_before=version_before,
            version_after=version_after,
            degraded=True,
            skipped=False,
            warnings=[warn],
            non_conformances=_region_ncs + _deg_fm_ncs + _deg_output_ncs,
        )

    # --- Non-degraded paths ---
    # Use resolve_version to supply a default when 'version' is absent, rather
    # than hard-failing. A file missing 'version' is transformed with the
    # default version as version_before.
    version_before = resolve_version(frontmatter)

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
        if not generic_fm_result["success"]:
            return TransformResult(
                success=False,
                errors=[TransformError(
                    line_number=generic_fm_result.get("line_number", 1),
                    message=f"Failed to parse frontmatter of generic reference {generic_ref_path}: {generic_fm_result['error']}"
                )],
                sections_added=[],
                injections_added=[],
                deployed_added=[],
                version_before=version_before,
                version_after=""
            )
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
            deployed_added=[],
            version_before=version_before,
            version_after=version_after
        )

    # Apply conduct-region insertion on both generic and harness paths.
    # find_section_spans locates sections in the already-tagged output body so that
    # apply_conduct_regions can anchor against headings and existing boundary tags.
    _body_for_regions = transformed_body["lines"]
    _section_spans = find_section_spans(_body_for_regions)
    _region_result = apply_conduct_regions(_body_for_regions, _section_spans)
    transformed_body = dict(transformed_body)
    transformed_body["lines"] = _region_result.lines
    transformed_body["deployed_added"] = (
        transformed_body.get("deployed_added", []) + _region_result.deployed_added
    )
    _region_ncs = [
        dataclasses.replace(nc, file=input_path)
        for nc in _region_result.non_conformances
    ]

    # Determine the generic id for id reconciliation (harness path only).
    _generic_id: str | None = None
    if is_harness:
        _generic_id = read_generic_id(generic_lines)

    # Build output frontmatter using build_output_frontmatter so all Stage 6
    # rules apply: derived fields, tier placeholders, harness-only key stripping,
    # id reconciliation.  Keys not touched by any rule are preserved verbatim in
    # their original order and formatting (including multi-line maps and lists).
    _kind = _classify_document(list(frontmatter.keys()), frontmatter)
    _derived = DerivedFrontmatter(
        name=derive_name(input_path),
        role=derive_role(input_path),
        required_skills=derive_required_skills(body_lines),
    )
    _fm_lines, _fm_ncs = build_output_frontmatter(
        frontmatter_result["raw_lines"],
        kind=_kind,
        derived=_derived,
        version_after=version_after,
        transform_version_after=transform_version_after,
        generic_id=_generic_id,
    )
    _fm_ncs = [dataclasses.replace(nc, file=input_path) for nc in _fm_ncs]

    _final_sections = find_section_spans(transformed_body["lines"])
    _output_ncs = detect_output_non_conformances(
        input_path, transformed_body["lines"], _final_sections
    )

    output_lines = ["---\n"] + _fm_lines + ["---\n"]
    output_lines.extend(transformed_body["lines"])

    output_path.write_text("".join(output_lines), encoding="utf-8")

    return TransformResult(
        success=True,
        errors=[],
        sections_added=transformed_body["sections_added"],
        injections_added=transformed_body["injections_added"],
        deployed_added=transformed_body.get("deployed_added", []),
        version_before=version_before,
        version_after=version_after,
        non_conformances=_region_ncs + _fm_ncs + _output_ncs,
    )


def _parse_frontmatter(lines: list[str], require_version: bool = True) -> dict:
    """Parse YAML frontmatter from lines.

    The frontmatter block is preserved verbatim (returned as ``raw_lines``); the
    transformer only rewrites the ``version`` / ``transform_version`` values and
    never reorders, drops, or reformats any other field. Only top-level scalar
    keys are parsed into ``frontmatter`` -- enough to detect the version and
    whether the file is a harness variant. Indented lines belong to a multi-line
    value (e.g. a ``permission:`` map or an ``mcpServers:`` list) and are left
    untouched.

    Args:
        lines: file lines including frontmatter.
        require_version: when True (the default), returns failure if the
            frontmatter carries no ``version`` field. Pass False on the
            degraded path so a missing version can be supplied by default_version().

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

    if require_version and "version" not in frontmatter:
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
    """Thin alias for bump_version. Retained for internal call-site compatibility."""
    return bump_version(version_str)


def _transform_generic_body(lines: list[str], strict_identity: bool = True) -> dict:
    """Transform a generic file's body by adding section and injection boundaries.

    ArtifactProvenance is retired (Stage 5). Any input shape that carries it is
    handled by stripping the deployed tags and preserving only the sibling injection:

    - New shape already present (``[[DEPLOYED:ArtifactProvenance]]`` at top
      level): deployed tags are stripped; the sibling injection is kept unchanged.
    - Old shape (``[[SECTION:ArtifactProvenance]]``): the section block is
      removed; any ``[[INJECTION:ArtifactProvenanceExtension]]`` content inside
      is extracted and re-emitted as a top-level injection sibling (Contract T-B).
    - Untagged source (``## Artifact Provenance`` heading): the heading region
      is recognised and its prose body is discarded; the sibling injection is
      emitted at canonical slot 3 without any deployed region (Contract T-A).
    - No provenance region at all: normal pass-through (Contract T-C, absence).

    Returns dict with:
        success: bool
        lines: list[str] (if success)
        sections_added: list[str] (if success)
        injections_added: list[str] (if success)
        deployed_added: list[str] (if success)
        errors: list[TransformError] (if not success)
    """
    # Route based on what provenance shape (if any) is already present in the body.
    for line in lines:
        stripped = line.strip()
        if stripped == "[[DEPLOYED:ArtifactProvenance]]":
            # ArtifactProvenance is retired. Strip its deployed tags; keep injection.
            return _strip_artifact_provenance_deployed_tags(lines)
        if stripped == "[[SECTION:ArtifactProvenance]]":
            # Old shape — retire the section, preserving any injection content.
            return _retire_old_provenance_section(lines)

    # Untagged source (or no provenance region at all): run the normal
    # section-tagging pass.  _identify_sections marks any '## Artifact Provenance'
    # heading region as covered so orphan detection does not flag it.

    result_lines = []
    sections_added = []
    injections_added = []
    deployed_added = []
    errors = []

    # Identify all sections first. Generic files use the strict Identity rule so
    # any non-canonical H2 immediately after Identity is flagged (FR-13).
    # The degraded path passes strict_identity=False to absorb non-canonical
    # H2 headings inside Identity rather than flagging them as unclassifiable.
    sections_result = _identify_sections(lines, strict_identity=strict_identity)
    sections = sections_result["sections"]
    provenance_region = sections_result.get("provenance_region")
    comm_region = sections_result.get("communication_protocol_region")
    errors.extend(sections_result["errors"])

    # If there are errors, return immediately
    if errors:
        return {
            "success": False,
            "errors": errors
        }

    # Pre-scan the Communication Protocol region for markers that must be
    # relocated or emitted at top level rather than discarded with the prose.
    found_identity_extension = False
    found_protocol_extension = False
    if comm_region is not None:
        for _j in range(comm_region["start_line"], min(comm_region["end_line"], len(lines))):
            _m = _match_region_marker(lines[_j])
            if _m is not None:
                if _m["name"] == "IdentityExtension":
                    found_identity_extension = True
                elif _m["name"] == "ProtocolExtension":
                    found_protocol_extension = True

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

        # While inside a fenced code block, append verbatim — no provenance
        # region interception, no section detection, no marker matching.
        if in_fenced_block:
            result_lines.append(line)
            i += 1
            continue

        # Check if this is the start of an untagged provenance heading region.
        # ArtifactProvenance is retired: emit only the injection sibling, not the
        # deployed region. The heading prose is discarded as before.
        if provenance_region is not None and i == provenance_region["start_line"]:
            result_lines.extend(_build_injection_only(""))
            injections_added.append("ArtifactProvenanceExtension")
            i = provenance_region["end_line"]
            continue

        # Check if this is the start of an untagged Communication Protocol region.
        # The entire region is replaced with a top-level DEPLOYED boundary pair.
        # Markers found inside the region are handled via pre-scan: IdentityExtension
        # is relocated into Identity (done below when the Identity section closes),
        # and ProtocolExtension is emitted as an empty top-level injection sibling.
        if comm_region is not None and i == comm_region["start_line"]:
            result_lines.append("[[DEPLOYED:CommunicationProtocol]]\n")
            result_lines.append("[[/DEPLOYED:CommunicationProtocol]]\n")
            deployed_added.append("CommunicationProtocol")
            if found_protocol_extension:
                result_lines.append("\n")
                result_lines.append("[[INJECTION:ProtocolExtension]]\n")
                result_lines.append("[[/INJECTION:ProtocolExtension]]\n")
                injections_added.append("ProtocolExtension")
            # When the region ends with a '---' separator, emit it verbatim so the
            # document structure (separator before next section) is preserved.
            region_end = min(comm_region["end_line"], len(lines))
            if (region_end > comm_region["start_line"]
                    and lines[region_end - 1].strip() == "---"):
                result_lines.append("---\n")
            i = comm_region["end_line"]
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

                # While inside a fenced code block, append verbatim — no marker
                # matching, no section detection.
                if in_fenced_block:
                    result_lines.append(line)
                    j += 1
                    continue

                # Check for injection/deployed markers
                injection_match = _match_region_marker(line)
                if injection_match:
                    injection_name = injection_match["name"]
                    marker_kind = injection_match["kind"]

                    # Deliberate, name-scoped rule: the legacy custom_constraints
                    # marker is preserved as [[INJECTION:CustomConstraints]] only
                    # when content immediately follows it; an empty marker is
                    # dropped entirely (no open or close tag emitted). Every other
                    # old marker keeps the unconditional empty-pair behaviour below.
                    if injection_name == "CustomConstraints":
                        content_lines, next_j = _collect_custom_constraints_content(
                            lines, j + 1, section_end
                        )
                        if content_lines:
                            result_lines.append(f"{open_tag(marker_kind, injection_name)}\n")
                            result_lines.extend(content_lines)
                            result_lines.append(f"{close_tag(marker_kind, injection_name)}\n")
                            injections_added.append(injection_name)
                        j = next_j
                        continue

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

            # When an [INJECTION: identity_extension] marker was found in the
            # Communication Protocol region (past Identity's end_line), relocate
            # it to the end of the Identity body. The marker's source line lies
            # outside Identity's range so in-place emission is impossible; instead,
            # we inject it here immediately before the section close tag, preceded
            # by one blank line when the current last result line is non-blank.
            if section_name == "Identity" and found_identity_extension:
                if result_lines and result_lines[-1].strip():
                    result_lines.append("\n")
                result_lines.append("[[INJECTION:IdentityExtension]]\n")
                result_lines.append("[[/INJECTION:IdentityExtension]]\n")
                injections_added.append("IdentityExtension")

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
        "injections_added": injections_added,
        "deployed_added": deployed_added,
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
    # comm_protocol_line records the first '## Communication Protocol' heading
    # (outside fenced blocks) for use in the non-strict Identity end computation.
    section_starts = []
    all_h2_lines = []
    comm_protocol_line = None  # line index of first unambiguous CP heading
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
            elif line.strip() == "## Communication Protocol" and comm_protocol_line is None:
                comm_protocol_line = i
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
            else:
                # On the non-strict (harness) path, also treat '## Communication
                # Protocol' as an Identity terminator when it appears before the
                # next canonical heading. Without this guard, Identity absorbs the
                # entire CP region body and the validator reports E008.
                if (comm_protocol_line is not None
                        and start_line < comm_protocol_line < boundary_line):
                    boundary_line = comm_protocol_line
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

    # Before orphan detection: mark any '## Artifact Provenance' heading region as
    # covered so it does not raise an unclassifiable-heading error.  This heading is
    # no longer in SECTION_HEADING_MAP (ArtifactProvenance is a tool-managed deployed
    # name), but untagged source files may still carry it.  The caller is responsible
    # for replacing it with the new deployed-region shape (Contract T-A).
    provenance_region = None
    for i, line in enumerate(lines):
        if line.strip() == "## Artifact Provenance" and not covered[i]:
            prov_start = i
            # Scan forward for the boundary: the next '---' separator (include it)
            # or the start of the next canonical heading (exclude it).
            prov_end = len(lines)
            for j in range(i, len(lines)):
                if lines[j].strip() == "---":
                    prov_end = j + 1  # include the separator
                    break
                if j > i and (
                    (lines[j].startswith("# ") and not lines[j].startswith("##"))
                    or (lines[j].startswith("## ")
                        and not lines[j].startswith("###")
                        and _canonical_section_for_heading(lines[j]) is not None)
                ):
                    prov_end = j
                    break
            for k in range(prov_start, min(prov_end, len(lines))):
                covered[k] = True
            provenance_region = {"start_line": prov_start, "end_line": prov_end}
            break  # only one provenance heading expected per file

    # Detect any '## Communication Protocol' heading region. This scan carries
    # its own fence tracking (unlike the provenance scan above) so that a literal
    # '## Communication Protocol' line inside a fenced code block positioned
    # outside every canonical section's range is not mistaken for a real region.
    communication_protocol_region = None
    _cp_fenced = False
    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("```"):
            _cp_fenced = not _cp_fenced
        if stripped == "## Communication Protocol" and not covered[i] and not _cp_fenced:
            cp_start = i
            cp_end = len(lines)
            # Forward scan for the region boundary. Carries its own fence state
            # seeded from the current _cp_fenced value (which is False here since
            # the heading itself is not inside a fence).
            _cp_end_fenced = False
            for j in range(i + 1, len(lines)):
                jstripped = lines[j].strip()
                if jstripped.startswith("```"):
                    _cp_end_fenced = not _cp_end_fenced
                if _cp_end_fenced:
                    continue
                if jstripped == "---":
                    cp_end = j + 1  # include the separator in the region
                    break
                if j > i and (
                    (lines[j].startswith("# ") and not lines[j].startswith("##"))
                    or (lines[j].startswith("## ")
                        and not lines[j].startswith("###")
                        and _canonical_section_for_heading(lines[j]) is not None)
                ):
                    cp_end = j  # exclude the next heading
                    break
            for k in range(cp_start, min(cp_end, len(lines))):
                covered[k] = True
            communication_protocol_region = {"start_line": cp_start, "end_line": cp_end}
            break  # at most one CP region per file

    in_fenced_block = False
    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped.startswith("```"):
            in_fenced_block = not in_fenced_block
        if covered[i] or not stripped or stripped == "---":
            continue
        # Fence delimiters and content inside fenced blocks outside every section
        # range are not classifiable content — suppress orphan errors for them.
        if stripped.startswith("```") or in_fenced_block:
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

    return {
        "sections": sections,
        "errors": errors,
        "provenance_region": provenance_region,
        "communication_protocol_region": communication_protocol_region,
    }


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


def _collect_custom_constraints_content(
    lines: list[str], start: int, section_end: int
) -> tuple[list[str], int]:
    """Collect the fill content immediately following a legacy custom_constraints
    marker, stopping at the first blank line, the next marker, or section_end.

    Returns (content_lines, next_index): content_lines is empty when the marker
    is immediately followed by a blank line (or nothing at all) -- the "empty
    region" case that the caller drops entirely. next_index is the index to
    resume the outer scan from, one past whatever was consumed here.
    """
    content_lines: list[str] = []
    k = start
    while k < section_end:
        line = lines[k]
        if not line.strip():
            break
        if _match_region_marker(line):
            break
        content_lines.append(line)
        k += 1
    return content_lines, k


def _strip_artifact_provenance_deployed_tags(lines: list[str]) -> dict:
    """Remove ``[[DEPLOYED:ArtifactProvenance]]`` and its close tag from the body.

    ArtifactProvenance is retired. When a file already carries the new-shape
    deployed region, this function strips just those two tag lines (open and
    close) while leaving every other line — including the sibling
    ``[[INJECTION:ArtifactProvenanceExtension]]`` region — unchanged.

    Returns a body-transform result dict (same shape as ``_transform_generic_body``).
    """
    DEPLOYED_OPEN = "[[DEPLOYED:ArtifactProvenance]]"
    DEPLOYED_CLOSE = "[[/DEPLOYED:ArtifactProvenance]]"

    result_lines = [
        line for line in lines
        if line.strip() not in (DEPLOYED_OPEN, DEPLOYED_CLOSE)
    ]
    return {
        "success": True,
        "lines": result_lines,
        "sections_added": [],
        "injections_added": [],
        "deployed_added": [],
        "errors": [],
    }


def _retire_old_provenance_section(lines: list[str]) -> dict:
    """Remove the ``[[SECTION:ArtifactProvenance]]`` block, preserving injection content.

    ArtifactProvenance is retired. When a file carries the old section shape,
    this function finds the section span, extracts any nested
    ``[[INJECTION:ArtifactProvenanceExtension]]`` content, and replaces the
    entire section with only the injection sibling (using ``_build_injection_only``).
    Every line before and after the span is passed through unchanged.

    Returns a body-transform result dict (same shape as ``_transform_generic_body``).
    """
    OLD_OPEN = "[[SECTION:ArtifactProvenance]]"
    OLD_CLOSE = "[[/SECTION:ArtifactProvenance]]"

    sec_open_idx = None
    sec_close_idx = None
    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped == OLD_OPEN and sec_open_idx is None:
            sec_open_idx = i
        elif stripped == OLD_CLOSE and sec_open_idx is not None:
            sec_close_idx = i
            break

    if sec_open_idx is None or sec_close_idx is None:
        return {
            "success": False,
            "errors": [TransformError(
                line_number=1,
                message="Could not locate [[SECTION:ArtifactProvenance]] block",
            )],
        }

    inner_lines = lines[sec_open_idx + 1:sec_close_idx]
    ext_content = _extract_provenance_extension_content(inner_lines)
    injection_lines = _build_injection_only(ext_content)

    result_lines = list(lines[:sec_open_idx]) + injection_lines + list(lines[sec_close_idx + 1:])
    return {
        "success": True,
        "lines": result_lines,
        "sections_added": [],
        "injections_added": ["ArtifactProvenanceExtension"],
        "deployed_added": [],
        "errors": [],
    }


def _build_injection_only(ext_content: str) -> list[str]:
    """Return lines that make up only the injection sibling (no deployed region).

    ArtifactProvenance is retired. This function produces the injection without
    the now-retired ``[[DEPLOYED:ArtifactProvenance]]`` region::

        [[INJECTION:ArtifactProvenanceExtension]]
        {ext_content}
        [[/INJECTION:ArtifactProvenanceExtension]]

    ``ext_content`` is spliced in verbatim; an empty string yields an empty region.
    """
    shape: list[str] = [
        "[[INJECTION:ArtifactProvenanceExtension]]\n",
    ]
    if ext_content:
        shape.append(ext_content)
    shape.append("[[/INJECTION:ArtifactProvenanceExtension]]\n")
    return shape


def _extract_provenance_extension_content(inner_lines: list[str]) -> str:
    """Extract content between [[INJECTION:ArtifactProvenanceExtension]] markers.

    Scans ``inner_lines`` (the lines inside a provenance section or region) for the
    injection open/close pair and returns the bytes between them as a single string.
    Newlines on each content line are preserved exactly.  Returns ``""`` when the
    markers are absent or when the injection is empty.
    """
    inj_open = "[[INJECTION:ArtifactProvenanceExtension]]"
    inj_close = "[[/INJECTION:ArtifactProvenanceExtension]]"

    inj_open_idx = None
    inj_close_idx = None
    for i, line in enumerate(inner_lines):
        stripped = line.strip()
        if stripped == inj_open and inj_open_idx is None:
            inj_open_idx = i
        elif stripped == inj_close and inj_open_idx is not None:
            inj_close_idx = i
            break

    if inj_open_idx is None or inj_close_idx is None:
        return ""
    return "".join(inner_lines[inj_open_idx + 1:inj_close_idx])


def _build_provenance_new_shape(ext_content: str) -> list[str]:
    """Return the list of lines that make up the new-shape provenance region.

    Produces::

        [[DEPLOYED:ArtifactProvenance]]
        [[/DEPLOYED:ArtifactProvenance]]

        [[INJECTION:ArtifactProvenanceExtension]]
        {ext_content}
        [[/INJECTION:ArtifactProvenanceExtension]]

    ``ext_content`` is spliced in verbatim between the injection markers; it
    already ends with ``\\n`` when non-empty (it is extracted directly from the
    input lines).  An empty ``ext_content`` yields an empty injection region.
    """
    shape: list[str] = [
        "[[DEPLOYED:ArtifactProvenance]]\n",
        "[[/DEPLOYED:ArtifactProvenance]]\n",
        "\n",
        "[[INJECTION:ArtifactProvenanceExtension]]\n",
    ]
    if ext_content:
        shape.append(ext_content)
    shape.append("[[/INJECTION:ArtifactProvenanceExtension]]\n")
    return shape


def _rewrite_old_provenance_to_new_shape(lines: list[str]) -> dict:
    """Replace a [[SECTION:ArtifactProvenance]] block with the new deployed-region shape.

    Finds the old ``[[SECTION:ArtifactProvenance]]`` … ``[[/SECTION:ArtifactProvenance]]``
    span, extracts any nested ``[[INJECTION:ArtifactProvenanceExtension]]`` content
    (preserving it byte-identically), and replaces the entire span with the new shape.
    Every line before and after the span is passed through unchanged.

    Returns a body-transform result dict (same shape as ``_transform_generic_body``).
    """
    OLD_OPEN = "[[SECTION:ArtifactProvenance]]"
    OLD_CLOSE = "[[/SECTION:ArtifactProvenance]]"

    sec_open_idx = None
    sec_close_idx = None
    for i, line in enumerate(lines):
        stripped = line.strip()
        if stripped == OLD_OPEN and sec_open_idx is None:
            sec_open_idx = i
        elif stripped == OLD_CLOSE and sec_open_idx is not None:
            sec_close_idx = i
            break

    if sec_open_idx is None or sec_close_idx is None:
        return {
            "success": False,
            "errors": [TransformError(
                line_number=1,
                message="Could not locate [[SECTION:ArtifactProvenance]] block",
            )],
        }

    inner_lines = lines[sec_open_idx + 1:sec_close_idx]
    ext_content = _extract_provenance_extension_content(inner_lines)
    new_shape = _build_provenance_new_shape(ext_content)

    result_lines = list(lines[:sec_open_idx]) + new_shape + list(lines[sec_close_idx + 1:])
    return {
        "success": True,
        "lines": result_lines,
        "sections_added": [],
        "injections_added": ["ArtifactProvenanceExtension"],
        "deployed_added": ["ArtifactProvenance"],
        "errors": [],
    }


def _emit_comm_protocol_region(
    lines: list[str],
    region: dict,
    found_protocol_extension: bool,
    out: list[str],
    deployed_added: list[str],
    injections_added: list[str],
) -> int:
    """Replace an untagged Communication Protocol region with its new-shape tags.

    The region's prose is discarded: its content is tool-managed and regenerated
    on every deploy, so it is replaced by an empty top-level
    ``[[DEPLOYED:CommunicationProtocol]]`` pair. A ``ProtocolExtension`` marker
    found inside the region is re-emitted as an empty top-level injection
    sibling. A trailing ``---`` separator is preserved so the document structure
    around the following section is unchanged.

    Appends to ``out`` and returns the line index to resume reading from.
    """
    out.append("[[DEPLOYED:CommunicationProtocol]]\n")
    out.append("[[/DEPLOYED:CommunicationProtocol]]\n")
    deployed_added.append("CommunicationProtocol")
    if found_protocol_extension:
        out.append("\n")
        out.append("[[INJECTION:ProtocolExtension]]\n")
        out.append("[[/INJECTION:ProtocolExtension]]\n")
        injections_added.append("ProtocolExtension")
    region_end = min(region["end_line"], len(lines))
    if region_end > region["start_line"] and lines[region_end - 1].strip() == "---":
        out.append("---\n")
    return region["end_line"]


def _scan_region_for_relocated_markers(
    lines: list[str], region: Optional[dict]
) -> tuple[bool, bool]:
    """Return (found_identity_extension, found_protocol_extension) for a region.

    Old-format injection markers can sit inside the Communication Protocol
    region, which is discarded wholesale. Any marker found there must be
    re-emitted elsewhere rather than lost with the prose, so callers pre-scan
    for them before the region is consumed.
    """
    found_identity = False
    found_protocol = False
    if region is None:
        return found_identity, found_protocol
    for j in range(region["start_line"], min(region["end_line"], len(lines))):
        marker = _match_region_marker(lines[j])
        if marker is None:
            continue
        if marker["name"] == "IdentityExtension":
            found_identity = True
        elif marker["name"] == "ProtocolExtension":
            found_protocol = True
    return found_identity, found_protocol


def _strip_deployed_pair(lines: list[str], name: str) -> list[str]:
    """Return `lines` with any `[[DEPLOYED:name]]` / `[[/DEPLOYED:name]]` tag
    lines removed, leaving everything between them untouched.

    Sections are identified by heading range, so a section's range can extend
    past its own closing tag and swallow a following top-level deployed region.
    Callers that re-emit that region themselves strip it here first to avoid
    emitting the boundary name twice.
    """
    open_line = open_tag(BoundaryKind.DEPLOYED, name)
    close_line = close_tag(BoundaryKind.DEPLOYED, name)
    return [ln for ln in lines if ln.strip() not in (open_line, close_line)]


def _transform_harness_body(harness_lines: list[str], generic_lines: list[str]) -> dict:
    """Transform a harness file's body using the generic reference.

    A harness file is the generic file with some or all injection markers either
    left in place (empty injection) or replaced by concrete fill content. This
    function reconstructs the injection boundaries by walking the harness body
    section by section and, within each section, aligning the harness lines
    against the generic section's injection markers (see _merge_section).

    Content between sections is copied verbatim, with one exception: an untagged
    Communication Protocol region is intercepted and rewritten to the new
    top-level deployed shape. Without that interception the region would be
    copied out as untagged prose sitting outside every boundary (E005) while an
    empty deployed pair was left nested inside Identity (E008).
    """
    harness_sections_result = _identify_sections(harness_lines)
    sections = harness_sections_result["sections"]
    errors = harness_sections_result["errors"]

    if errors:
        return {"success": False, "errors": errors}

    generic_sections_result = _identify_sections(generic_lines)
    generic_by_name = {gs["name"]: gs for gs in generic_sections_result["sections"]}

    comm_region = harness_sections_result.get("communication_protocol_region")
    found_identity_extension, found_protocol_extension = (
        _scan_region_for_relocated_markers(harness_lines, comm_region)
    )

    result_lines = []
    sections_added = []
    injections_added = []
    deployed_added: list[str] = []

    harness_idx = 0

    def _copy_verbatim_until(stop: int) -> None:
        """Copy harness lines up to `stop`, intercepting the protocol region."""
        nonlocal harness_idx
        while harness_idx < stop:
            if comm_region is not None and harness_idx == comm_region["start_line"]:
                harness_idx = _emit_comm_protocol_region(
                    harness_lines, comm_region, found_protocol_extension,
                    result_lines, deployed_added, injections_added,
                )
                continue
            result_lines.append(harness_lines[harness_idx])
            harness_idx += 1

    for section in sections:
        # Emit any lines that sit between sections (separators, blanks) verbatim.
        _copy_verbatim_until(section["start_line"])

        sections_added.append(section["name"])
        result_lines.append(open_tag(BoundaryKind.SECTION, section["name"]) + "\n")

        harness_section = harness_lines[section["start_line"]:section["end_line"]]
        generic_section_meta = generic_by_name.get(section["name"])
        if generic_section_meta is not None:
            generic_section = generic_lines[
                generic_section_meta["start_line"]:generic_section_meta["end_line"]
            ]
            if comm_region is not None:
                # We emit CommunicationProtocol ourselves at top level; drop any
                # copy the generic section range happens to overlap.
                generic_section = _strip_deployed_pair(
                    generic_section, "CommunicationProtocol"
                )
            merged, injs = _merge_section(harness_section, generic_section)
            result_lines.extend(merged)
            injections_added.extend(injs)
        else:
            result_lines.extend(harness_section)

        # An [INJECTION: identity_extension] marker found inside the discarded
        # Communication Protocol region belongs to Identity, whose range ends
        # before it. Relocate it to the end of the Identity body — but only if
        # the section merge did not already produce the boundary from the
        # generic reference, which would otherwise duplicate it.
        if (section["name"] == "Identity"
                and found_identity_extension
                and "IdentityExtension" not in injections_added):
            if result_lines and result_lines[-1].strip():
                result_lines.append("\n")
            result_lines.append("[[INJECTION:IdentityExtension]]\n")
            result_lines.append("[[/INJECTION:IdentityExtension]]\n")
            injections_added.append("IdentityExtension")

        if result_lines and not result_lines[-1].endswith("\n"):
            result_lines[-1] += "\n"
        result_lines.append(close_tag(BoundaryKind.SECTION, section["name"]) + "\n")
        harness_idx = section["end_line"]

    _copy_verbatim_until(len(harness_lines))

    return {
        "success": True,
        "lines": result_lines,
        "sections_added": sections_added,
        "injections_added": injections_added,
        "deployed_added": deployed_added,
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
    # Set when a CustomConstraints drop leaves `out` ending on a blank line:
    # the next blank line this loop would otherwise emit is skipped once, so
    # dropping the region never leaves two blank lines where the marker's
    # own surrounding blanks and the section's trailing blank now abut.
    collapse_next_blank = False

    def _append_line(line: str) -> None:
        nonlocal collapse_next_blank
        if collapse_next_blank and not line.strip():
            collapse_next_blank = False
            return
        if line.strip():
            collapse_next_blank = False
        out.append(line)

    while gi < len(generic_section) or hi < len(harness_section):
        gtok = generic_section[gi] if gi < len(generic_section) else None
        hline = harness_section[hi] if hi < len(harness_section) else None

        if gtok is not None and _match_region_marker(gtok):
            gtok_match = _match_region_marker(gtok)
            name = gtok_match["name"]
            marker_kind = gtok_match["kind"]
            harness_marker = _match_region_marker(hline) if hline is not None else None

            if harness_marker is not None and harness_marker["name"] == name:
                # Marker still present in the harness -> empty boundary, except
                # the deliberate CustomConstraints rule: an empty legacy marker
                # is dropped entirely rather than emitted as an empty injection.
                if name != "CustomConstraints":
                    out.append(open_tag(marker_kind, name) + "\n")
                    out.append(close_tag(marker_kind, name) + "\n")
                    injections.append(name)
                elif out and not out[-1].strip():
                    collapse_next_blank = True
                gi += 1
                hi += 1
                continue

            # Marker removed in the harness: collect this region's fill content,
            # advancing `hi` exactly as far as it would have gone before (so any
            # literal marker text further down the harness is still matched at
            # the right position on a later iteration).
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

            if marker_kind == BoundaryKind.DEPLOYED:
                # A DEPLOYED region's body is always empty (AD-8): the deployment
                # tool fills it later, so collected harness text is never this
                # region's fill, unlike an INJECTION marker. A leading '### sub-
                # heading' (and the blank line after it) stays outside and before
                # the tag, matching _emit_injection's convention; the remaining
                # collected text -- what would have been the fill body -- is
                # placed after the close tag, verbatim, as ordinary section
                # content rather than as the region's body.
                lead_end = 0
                if content and _is_h3_heading(content[0]):
                    lead_end = 1
                    while lead_end < len(content) and not content[lead_end].strip():
                        lead_end += 1
                out.extend(content[:lead_end])
                out.append(open_tag(marker_kind, name) + "\n")
                out.append(close_tag(marker_kind, name) + "\n")
                out.extend(content[lead_end:])
                injections.append(name)
            elif name == "CustomConstraints" and not any(c.strip() for c in content):
                # Deliberate CustomConstraints rule: no fill content was collected
                # (harness omitted the marker and supplied nothing in its place),
                # so the region is dropped entirely rather than emitted empty.
                for c in content:
                    _append_line(c)
                if out and not out[-1].strip():
                    collapse_next_blank = True
            else:
                _emit_injection(name, content, out, kind=marker_kind)
                injections.append(name)
            gi += 1
            continue

        if gtok is not None and hline is not None and gtok == hline:
            _append_line(hline)
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
                prev_marker = (
                    _match_injection_marker(prev_nonblank)
                    if prev_nonblank is not None else None
                )
                # CustomConstraints is always dropped when its collected fill is
                # empty (never emitted as a region), so there is no injection to
                # separate from what follows -- the trailing blank is not preserved.
                if (prev_marker is not None
                        and prev_marker["name"] != "CustomConstraints"
                        and (not out or out[-1].strip())):
                    _append_line(gtok)
            gi += 1
            continue

        # Harness-only line (extra blank or content not present in generic).
        _append_line(hline)
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
