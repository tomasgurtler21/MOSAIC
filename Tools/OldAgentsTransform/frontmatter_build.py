"""Frontmatter derivation and emission for the MOSAIC boundary transformer.

New in Stage 6. This module derives the three derivable frontmatter fields
(role, name, required_skills), inserts tier placeholders for the two that
cannot be derived, strips or preserves harness-only keys per DocumentKind,
reconciles the id from a generic reference when one is supplied, and emits
the full output frontmatter block.

All public functions are pure: they read no files and write none.
"""
from __future__ import annotations

import dataclasses
import pathlib
import re
from collections.abc import Sequence

from document_kind import DocumentKind
from non_conformance import NC_MESSAGES, NC_TIER_PLACEHOLDER, NonConformance


# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

TIER_PLACEHOLDER: str = "TODO"
TIER_RATIONALE_PLACEHOLDER: str = "TODO: state why this tier"

DERIVED_KEY_ORDER: tuple[str, ...] = (
    "name",
    "role",
    "required_skills",
    "recommended_tier",
    "tier_rationale",
)
"""Order in which absent derived keys are appended.

Keys already present in the source frontmatter keep their original position
and order. Only keys absent from the source are appended, in this order,
after the last source key.
"""

# Matches a top-level YAML frontmatter key at the start of a line.
# Indented lines belong to a multi-line value and are NOT matched.
_KEY_RE: re.Pattern[str] = re.compile(r"^([A-Za-z_][A-Za-z0-9_-]*)\s*:")


# ---------------------------------------------------------------------------
# Data structures
# ---------------------------------------------------------------------------

@dataclasses.dataclass(frozen=True)
class DerivedFrontmatter:
    """Groups the three fields derivable from filename and body before emission.

    Passed to build_output_frontmatter so derivation is separately testable
    from emission.
    """
    name: str
    role: str                # "subagent" | "orchestrator"
    required_skills: list[str]  # possibly empty; [] is a valid, meaningful result


# ---------------------------------------------------------------------------
# Derivation functions
# ---------------------------------------------------------------------------

def derive_role(path: pathlib.Path) -> str:
    """Return 'orchestrator' when the file is an orchestrator, else 'subagent'.

    Uses file_classification.classify_file(path), which is filename-based and
    matches the tool's existing convention. Never returns 'utility' — utility
    files are skipped before this function is reached.

    Args:
        path: The path of the file being transformed.

    Returns:
        'orchestrator' or 'subagent'.
    """
    import file_classification as _fc
    file_class = _fc.classify_file(path)
    if file_class is _fc.FileClass.ORCHESTRATOR:
        return "orchestrator"
    return "subagent"


def derive_name(path: pathlib.Path) -> str:
    """Return the file's base name with trailing '.agent.md' or '.md' removed.

    Case and hyphens are preserved verbatim. Spaces are preserved verbatim;
    the caller quotes the YAML value when it contains a space.

    Examples:
        test-writer.agent.md  ->  test-writer
        orchestrator.md       ->  orchestrator
        Web Research.md       ->  Web Research

    Args:
        path: The path of the file being transformed.

    Returns:
        The derived name string.
    """
    name = path.name
    if name.endswith(".agent.md"):
        return name[: -len(".agent.md")]
    if name.endswith(".md"):
        return name[: -len(".md")]
    return name


def derive_required_skills(body_lines: Sequence[str]) -> list[str]:
    """Return skill keys the agent's Process steps tell it to load.

    A skill reference is a backtick-quoted token matching
    [a-z0-9]+(-[a-z0-9]+)* on a line that also contains the word 'skill'
    (case-insensitive), outside any fenced code block.

    Returns first-appearance order, duplicates removed. Returns [] when no
    skill-load directive is found — [] is a valid, meaningful result, not a
    failure (an agent that loads no skills legitimately declares []).

    Args:
        body_lines: The full body lines of the file (frontmatter excluded),
            with line terminators kept (matching splitlines(keepends=True)).

    Returns:
        A list of skill key strings, possibly empty.
    """
    # Pattern for skill key tokens: backtick-quoted, lowercase alphanum and hyphens
    _SKILL_TOKEN_RE = re.compile(r"`([a-z0-9]+(?:-[a-z0-9]+)*)`")
    _SKILL_WORD_RE = re.compile(r"\bskill\b", re.IGNORECASE)

    # Note: the design specifies scanning only within the Identity section's
    # Process list. This implementation scans all body lines instead. False
    # positives are unlikely because the fence-awareness and the 'skill' word
    # filter make non-Process matches rare, but a Capabilities section
    # discussing skill-related tooling could in principle pollute required_skills.
    # A future improvement should use locate_process_list_end() from
    # region_insertion.py to scope the scan to Process list lines only.
    skills: list[str] = []
    seen: set[str] = set()

    in_fence = False
    for line in body_lines:
        stripped = line.lstrip()
        # Track fenced code blocks
        if stripped.startswith("```") or stripped.startswith("~~~"):
            in_fence = not in_fence
            continue  # fence delimiters are never skill references
        if in_fence:
            continue

        # Only process lines that contain the word 'skill'
        if not _SKILL_WORD_RE.search(line):
            continue

        # Extract all backtick-quoted tokens matching the skill key pattern
        for m in _SKILL_TOKEN_RE.finditer(line):
            token = m.group(1)
            if token not in seen:
                seen.add(token)
                skills.append(token)

    return skills


# ---------------------------------------------------------------------------
# Frontmatter emission helpers
# ---------------------------------------------------------------------------

def _format_scalar(value: str) -> str:
    """Return YAML scalar value, double-quoted when it contains space, ':', or '#'."""
    if any(c in value for c in (" ", ":", "#")):
        return f'"{value}"'
    return value


def _format_skills(skills: list[str]) -> str:
    """Return required_skills as a YAML flow sequence string."""
    if not skills:
        return "[]"
    return "[" + ", ".join(skills) + "]"


# ---------------------------------------------------------------------------
# Frontmatter emission
# ---------------------------------------------------------------------------

def build_output_frontmatter(
    raw_lines: Sequence[str],
    *,
    kind: DocumentKind,
    derived: DerivedFrontmatter,
    version_after: str,
    transform_version_after: str | None,
    generic_id: str | None,
) -> tuple[list[str], list[NonConformance]]:
    """Produce the output frontmatter block's inner lines plus any non-conformances.

    The '---' delimiters are NOT included in either the input or the output.

    Rules, applied in this order:
      1. Every source key keeps its original position, formatting and multi-line
         value, except where a rule below rewrites it.
      2. 'version' value is replaced with version_after. When absent, 'version'
         is appended.
      3. 'transform_version' value is replaced with transform_version_after when
         that argument is not None. When it is None the key is left alone (it
         will be stripped by Rule 4 on the generic path anyway).
      4. When kind is AGENT_GENERIC, every key in HARNESS_ONLY_FRONTMATTER_KEYS
         is dropped from the output, together with its indented continuation
         lines. When kind is AGENT_HARNESS, they are preserved.
      5. 'id' is replaced with generic_id when generic_id is not None. When it
         is None, any existing 'id' is preserved verbatim.
      6. Each of derived.name / derived.role / derived.required_skills is
         written when absent from source; an existing value is left untouched.
      7. 'recommended_tier' and 'tier_rationale' are handled independently.
         For each key: when absent (or present-but-empty) it is written with
         the appropriate placeholder and one NC_TIER_PLACEHOLDER NonConformance
         is returned with detail set to that key's name; when present with a
         non-empty value it is not touched and raises nothing.
      8. Appended keys are emitted in DERIVED_KEY_ORDER after the last source
         key.

    Scalar emission format: 'key: value', unquoted, except a value containing
    a space, ':' or '#', which is double-quoted. required_skills is emitted as
    a YAML flow sequence: 'required_skills: []' or
    'required_skills: [efficient-file-reading, lean-tdd]'.

    Args:
        raw_lines:               Inner frontmatter lines (between --- delimiters),
                                 with line terminators (as from splitlines(True)).
        kind:                    Document kind driving Rule 4.
        derived:                 The three derivable fields (name, role, skills).
        version_after:           The bumped version string to write.
        transform_version_after: The new transform_version value, or None.
        generic_id:              The id from the generic reference file, or None.

    Returns:
        A 2-tuple of (output_lines, non_conformances).
        output_lines is the inner frontmatter lines for the output file.
        non_conformances contains only NC_TIER_PLACEHOLDER findings (one per
        absent or empty tier key), ordered by DERIVED_KEY_ORDER.
    """
    from boundary_constants import HARNESS_ONLY_FRONTMATTER_KEYS

    # First pass: collect present top-level keys and their raw string values.
    present_keys: dict[str, str] = {}
    for line in raw_lines:
        if not line.strip():
            continue
        if line[:1] in (" ", "\t"):
            continue  # continuation / indented line
        m = _KEY_RE.match(line)
        if m:
            key = m.group(1)
            value = line[m.end():].strip()
            if key not in present_keys:
                present_keys[key] = value

    output_lines: list[str] = []

    # Track which tier keys need a placeholder NC (emitted at end in DERIVED_KEY_ORDER).
    tier_nc_needed: set[str] = set()

    version_written = False
    skipping_harness_key = False  # True while we are stripping a harness-only block

    for line in raw_lines:
        stripped = line.rstrip("\n").rstrip("\r")

        if not stripped.strip():
            # Blank line: end any harness-key block and emit verbatim.
            skipping_harness_key = False
            output_lines.append(line)
            continue

        if line[:1] in (" ", "\t"):
            # Indented continuation line.
            if skipping_harness_key:
                continue  # part of a harness-only key block — strip
            output_lines.append(line)
            continue

        # Top-level key line.
        skipping_harness_key = False
        m = _KEY_RE.match(stripped)
        if not m:
            output_lines.append(line)
            continue

        key = m.group(1)
        value = stripped[m.end():].strip()

        # Rule 4: drop harness-only keys on the generic path.
        if kind is DocumentKind.AGENT_GENERIC and key in HARNESS_ONLY_FRONTMATTER_KEYS:
            skipping_harness_key = True
            continue

        # Rule 2: rewrite version.
        if key == "version":
            output_lines.append(f"version: {version_after}\n")
            version_written = True
            continue

        # Rule 3: rewrite transform_version when a new value is supplied.
        if key == "transform_version" and transform_version_after is not None:
            output_lines.append(f"transform_version: {transform_version_after}\n")
            continue

        # Rule 5: rewrite id from generic reference.
        if key == "id" and generic_id is not None:
            output_lines.append(f"id: {generic_id}\n")
            continue

        # Rule 7: tier keys — replace present-but-empty with placeholder.
        if key == "recommended_tier" and not value:
            output_lines.append(f"recommended_tier: {TIER_PLACEHOLDER}\n")
            tier_nc_needed.add("recommended_tier")
            continue

        if key == "tier_rationale" and not value:
            output_lines.append(f"tier_rationale: {_format_scalar(TIER_RATIONALE_PLACEHOLDER)}\n")
            tier_nc_needed.add("tier_rationale")
            continue

        # Keep verbatim.
        output_lines.append(line)

    # Rule 2: append version when it was absent from source.
    if not version_written:
        output_lines.append(f"version: {version_after}\n")

    # Rules 6, 7, 8: append absent derived/tier keys in DERIVED_KEY_ORDER.
    for key in DERIVED_KEY_ORDER:
        if key in present_keys:
            continue  # already in source (handled verbatim or modified above)

        if key == "name":
            output_lines.append(f"name: {_format_scalar(derived.name)}\n")
        elif key == "role":
            output_lines.append(f"role: {_format_scalar(derived.role)}\n")
        elif key == "required_skills":
            output_lines.append(
                f"required_skills: {_format_skills(derived.required_skills)}\n"
            )
        elif key == "recommended_tier":
            output_lines.append(f"recommended_tier: {TIER_PLACEHOLDER}\n")
            tier_nc_needed.add("recommended_tier")
        elif key == "tier_rationale":
            output_lines.append(f"tier_rationale: {_format_scalar(TIER_RATIONALE_PLACEHOLDER)}\n")
            tier_nc_needed.add("tier_rationale")

    # Emit NC_TIER_PLACEHOLDER findings in DERIVED_KEY_ORDER.
    ncs: list[NonConformance] = []
    for key in DERIVED_KEY_ORDER:
        if key in tier_nc_needed:
            msg = NC_MESSAGES[NC_TIER_PLACEHOLDER].format(detail=key)
            ncs.append(NonConformance(
                code=NC_TIER_PLACEHOLDER,
                file=pathlib.Path("."),
                message=msg,
                detail=key,
            ))

    return output_lines, ncs


def read_generic_id(generic_ref_lines: Sequence[str]) -> str | None:
    """Return the 'id' value from a generic reference file's frontmatter, or None.

    Scans only the frontmatter block (between the two '---' delimiters).
    Returns the raw string value unparsed (no type coercion). Returns None
    when the 'id' key is absent from the frontmatter.

    Args:
        generic_ref_lines: All lines of the generic reference file, with or
            without line terminators (both are acceptable).

    Returns:
        The raw id value string, or None.
    """
    _ID_RE = re.compile(r"^id\s*:\s*(.+)")
    in_fm = False
    for line in generic_ref_lines:
        stripped = line.strip()
        if stripped == "---":
            if not in_fm:
                in_fm = True
                continue
            else:
                break  # end of frontmatter
        if in_fm:
            m = _ID_RE.match(stripped)
            if m:
                return m.group(1).strip()
    return None
