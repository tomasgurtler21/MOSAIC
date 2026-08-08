"""Bundle reader for the canonical deployed-sections bundle.

Parses DeployedSections.md (or any `type: bundle` file) into typed Python
objects. Intended consumers are acceptance tests (uniformity checks against
canonical block names) and future deployment-tool integrations.

This module depends on the compound-name widening delivered by Stage 1
(TAG_PATTERN must admit `[[SECTION:Prefix:id]]` names); it must not be loaded
in an environment where that widening is absent.
"""
from __future__ import annotations

import dataclasses
import pathlib
import re
from typing import TYPE_CHECKING

from boundary_constants import TAG_PATTERN

_TOOLS_DIR = pathlib.Path(__file__).parent        # Tools/OldAgentsTransform
_REPO_ROOT = _TOOLS_DIR.parent.parent             # repo root (two levels up)

DEFAULT_BUNDLE_PATH: pathlib.Path = _REPO_ROOT / "Agents" / "Generic" / "DeployedSections.md"
"""Absolute path to the canonical block bundle in the repository."""

# Patterns used for manual frontmatter parsing.
_FM_TOP_KEY_RE: re.Pattern[str] = re.compile(
    r"^([A-Za-z_][A-Za-z0-9_-]*)\s*:\s*(.*)"
)
_FM_BLOCK_ITEM_RE: re.Pattern[str] = re.compile(
    r"^  -\s+(.*)"
)
_FM_BLOCK_KV_RE: re.Pattern[str] = re.compile(
    r"^    ([A-Za-z_][A-Za-z0-9_-]*)\s*:\s*(.*)"
)


@dataclasses.dataclass(frozen=True)
class DeployedBlock:
    """One block declared in the bundle.

    ``name`` is the compound SECTION name used in the bundle body
    (e.g. ``"AuthorityHierarchy:Subagent"``). ``target`` is the canonical
    DEPLOYED name the deployment tool writes into agent files
    (e.g. ``"AuthorityHierarchy"``).
    """

    name: str           # compound SECTION name, e.g. "AuthorityHierarchy:Subagent"
    target: str         # canonical DEPLOYED name, e.g. "AuthorityHierarchy"
    applies_to: str     # audience string, e.g. "subagent"
    specified_in: str   # repo-relative path of the design document, as declared
    body: str           # region body text, line terminators included, tags excluded


@dataclasses.dataclass(frozen=True)
class Bundle:
    """The parsed canonical block bundle."""

    path: pathlib.Path
    bundle_version: str
    blocks: tuple[DeployedBlock, ...]

    def block_for(self, target: str, applies_to: str = "subagent") -> DeployedBlock | None:
        """Return the block for a canonical deployed name and audience, or None.

        Matches on both ``target`` and ``applies_to``; returns the first match.
        """
        for block in self.blocks:
            if block.target == target and block.applies_to == applies_to:
                return block
        return None

    def targets(self) -> frozenset[str]:
        """Return the canonical deployed names the bundle carries a block for.

        Used by acceptance tests to assert that every region the transformer
        emits has a canonical block behind it in the bundle.
        """
        return frozenset(block.target for block in self.blocks)


class BundleError(Exception):
    """Raised when the bundle cannot be parsed or is internally inconsistent."""


# ---------------------------------------------------------------------------
# Internal helpers
# ---------------------------------------------------------------------------

def _strip_yaml_quotes(value: str) -> str:
    """Strip surrounding double quotes from a bare YAML scalar value."""
    stripped = value.strip()
    if len(stripped) >= 2 and stripped[0] == '"' and stripped[-1] == '"':
        return stripped[1:-1]
    return stripped


def _find_frontmatter_bounds(lines: list[str]) -> int:
    """Return the 0-based index of the closing '---' line.

    Raises BundleError when the file does not start with '---' or has no
    closing delimiter.
    """
    if not lines or lines[0].rstrip("\r\n") != "---":
        raise BundleError("Bundle file does not start with a '---' frontmatter delimiter.")
    for i in range(1, len(lines)):
        if lines[i].rstrip("\r\n") == "---":
            return i
    raise BundleError("Bundle frontmatter has no closing '---' delimiter.")


def _parse_frontmatter(
    lines: list[str],
    fm_end: int,
) -> tuple[dict[str, str], list[dict[str, str]]]:
    """Parse scalar top-level keys and the 'blocks:' list from frontmatter.

    Returns ``(scalars, block_decls)`` where ``scalars`` is a dict of
    top-level string-valued keys and ``block_decls`` is the parsed list under
    the ``blocks:`` key.  Each element of ``block_decls`` is a dict of
    string-to-string key-value pairs.

    Raises BundleError on structural problems.
    """
    scalars: dict[str, str] = {}
    block_decls: list[dict[str, str]] = []
    current_block: dict[str, str] | None = None
    in_blocks = False

    for i in range(1, fm_end):
        raw = lines[i]
        line = raw.rstrip("\r\n")

        if not line.strip():
            continue

        # Block item continuation key: "    key: value"
        kv_m = _FM_BLOCK_KV_RE.match(line)
        if kv_m and in_blocks and current_block is not None:
            key = kv_m.group(1)
            value = _strip_yaml_quotes(kv_m.group(2))
            current_block[key] = value
            continue

        # Block sequence item: "  - key: value"  (name is on the same line)
        item_m = _FM_BLOCK_ITEM_RE.match(line)
        if item_m:
            if not in_blocks:
                raise BundleError(
                    f"Unexpected list item at frontmatter line {i + 1}."
                )
            if current_block is not None:
                block_decls.append(current_block)
            current_block = {}
            rest = item_m.group(1)
            # The first key-value may be inline with the dash
            inline_m = _FM_TOP_KEY_RE.match(rest)
            if inline_m:
                key = inline_m.group(1)
                value = _strip_yaml_quotes(inline_m.group(2))
                current_block[key] = value
            continue

        # Top-level key: "key: value"
        top_m = _FM_TOP_KEY_RE.match(line)
        if top_m:
            key = top_m.group(1)
            value = _strip_yaml_quotes(top_m.group(2))
            if key == "blocks":
                # Flush any in-progress block from a previous top-level key
                # (should not occur, but be safe)
                if current_block is not None:
                    block_decls.append(current_block)
                    current_block = None
                in_blocks = True
            else:
                in_blocks = False
                scalars[key] = value

    if current_block is not None:
        block_decls.append(current_block)

    return scalars, block_decls


def _extract_section_bodies(body_lines: list[str]) -> dict[str, str]:
    """Scan body lines for SECTION regions and return name -> body text.

    The body text has line terminators included and excludes the open/close
    tags themselves. Uses TAG_PATTERN so only syntactically valid compound
    names are recognised.
    """
    sections: dict[str, str] = {}
    i = 0
    while i < len(body_lines):
        stripped = body_lines[i].rstrip("\r\n")
        m = TAG_PATTERN.match(stripped)
        if m and not m.group("close") and m.group("kind") == "SECTION":
            name = m.group("name")
            body_start = i + 1
            j = body_start
            while j < len(body_lines):
                close_stripped = body_lines[j].rstrip("\r\n")
                close_m = TAG_PATTERN.match(close_stripped)
                if (
                    close_m
                    and close_m.group("close") == "/"
                    and close_m.group("kind") == "SECTION"
                    and close_m.group("name") == name
                ):
                    sections[name] = "".join(body_lines[body_start:j])
                    i = j + 1
                    break
                j += 1
            else:
                # No matching close tag found — skip this open tag
                i += 1
        else:
            i += 1
    return sections


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------

def load_bundle(path: pathlib.Path = DEFAULT_BUNDLE_PATH) -> Bundle:
    """Parse the canonical block bundle and return a ``Bundle`` instance.

    Reads the ``blocks:`` frontmatter list for each block's declared
    ``name``, ``target``, ``applies_to`` and ``specified_in``, and the body
    of each ``[[SECTION:<name>]] ... [[/SECTION:<name>]]`` region.

    Raises ``BundleError`` when:
    - the file cannot be read;
    - the frontmatter has no ``blocks:`` list (or the list is empty);
    - a declared block has no matching SECTION region in the body;
    - a SECTION region in the body has no corresponding declaration.
    """
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise BundleError(f"Cannot read bundle file '{path}': {exc}") from exc

    lines = text.splitlines(keepends=True)

    try:
        fm_end = _find_frontmatter_bounds(lines)
    except BundleError:
        raise

    try:
        scalars, block_decls = _parse_frontmatter(lines, fm_end)
    except BundleError:
        raise
    except Exception as exc:
        raise BundleError(f"Unexpected error parsing frontmatter of '{path}': {exc}") from exc

    if not block_decls:
        raise BundleError(
            f"Bundle '{path}' frontmatter has no 'blocks:' list (or the list is empty)."
        )

    bundle_version = scalars.get("bundle_version", "")

    # Extract SECTION regions from the body (lines after the closing '---')
    body_lines = lines[fm_end + 1:]
    sections = _extract_section_bodies(body_lines)

    # Validate coverage: every declaration must have a region and vice versa.
    declared_names: set[str] = set()
    for decl in block_decls:
        name = decl.get("name", "")
        if not name:
            raise BundleError(
                f"A block declaration in '{path}' is missing the 'name' field."
            )
        declared_names.add(name)

    section_names = set(sections.keys())

    missing_sections = declared_names - section_names
    if missing_sections:
        raise BundleError(
            f"Declared blocks have no SECTION region in '{path}': "
            f"{sorted(missing_sections)}"
        )

    extra_sections = section_names - declared_names
    if extra_sections:
        raise BundleError(
            f"SECTION regions have no block declaration in '{path}': "
            f"{sorted(extra_sections)}"
        )

    # Build the DeployedBlock tuple in declaration order.
    deployed_blocks: list[DeployedBlock] = []
    for decl in block_decls:
        name = decl["name"]  # guaranteed non-empty by earlier check
        deployed_blocks.append(
            DeployedBlock(
                name=name,
                target=decl.get("target", ""),
                applies_to=decl.get("applies_to", ""),
                specified_in=decl.get("specified_in", ""),
                body=sections[name],
            )
        )

    return Bundle(
        path=path,
        bundle_version=bundle_version,
        blocks=tuple(deployed_blocks),
    )
