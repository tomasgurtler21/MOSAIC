"""Test-support helpers for the Stage 10 corpus-level acceptance assertions.

Not production code: these exist so uniformity, isolation and deletion-completeness
checks over a whole transformed corpus are expressible without ad-hoc parsing in
every test. See ContractsDesign.md, "acceptance_helpers - Verification Contracts".
"""
from __future__ import annotations

import re
from collections.abc import Collection, Iterable

from boundary_constants import BoundaryKind, close_tag, open_tag

# Recognises any boundary tag line (open or close), any kind, any name -- used
# only to enumerate region names present in a text; does not enforce the strict
# compound-name grammar of boundary_constants.TAG_PATTERN.
#
# Matches the new XML-tag syntax:
#   open:  <Name type="core|managed|project|custom">  (with optional name="qualifier")
#   close: </Name>
_ANY_TAG_RE = re.compile(
    r'^<(?P<close>/?)(?P<tag>[A-Za-z][A-Za-z0-9]*)'
    r'(?:\s+type="(?P<type>core|managed|project|custom)")?'
    r'(?:\s+name="(?P<qualifier>[^"]*)")?'
    r'\s*>\s*$'
)

# Maps the XML type attribute value to the BoundaryKind string value.
_TYPE_TO_KIND: dict[str, str] = {
    "core":    "SECTION",
    "managed": "DEPLOYED",
    "project": "INJECTION",
    "custom":  "CUSTOM",
}


def extract_region(text: str, kind: BoundaryKind, name: str) -> str | None:
    """Return the exact text between a region's open and close tags.

    Line terminators of the interior lines are kept; both tag lines themselves
    are excluded. Returns None when the region (an open tag with a matching
    close tag later in the text) is not present.
    """
    open_line = open_tag(kind, name)
    close_line = close_tag(kind, name)
    lines = text.splitlines(keepends=True)

    start = None
    for i, line in enumerate(lines):
        if line.rstrip("\r\n") == open_line:
            start = i
            break
    if start is None:
        return None

    for j in range(start + 1, len(lines)):
        if lines[j].rstrip("\r\n") == close_line:
            return "".join(lines[start + 1:j])
    return None


def region_names(text: str, kind: BoundaryKind) -> list[str]:
    """Return every open-tag region name of the given kind, in document order."""
    names: list[str] = []
    for line in text.splitlines():
        m = _ANY_TAG_RE.match(line.strip())
        if m and not m.group("close"):
            tag_kind = _TYPE_TO_KIND.get(m.group("type") or "", "")
            if tag_kind == kind.value:
                tag_name = m.group("tag")
                qualifier = m.group("qualifier")
                names.append(f"{tag_name}:{qualifier}" if qualifier else tag_name)
    return names


def outside_regions(text: str, names: Iterable[tuple[BoundaryKind, str]]) -> str:
    """Return `text` with the named regions and their tags removed.

    Two files agreeing under this function differ only inside those regions.
    A name in `names` that is absent from `text` is simply a no-op for that name.
    """
    open_lines = {open_tag(k, n) for k, n in names}
    close_lines = {close_tag(k, n) for k, n in names}

    result: list[str] = []
    skipping = False
    for line in text.splitlines(keepends=True):
        stripped = line.rstrip("\r\n")
        if not skipping and stripped in open_lines:
            skipping = True
            continue
        if skipping:
            if stripped in close_lines:
                skipping = False
            continue
        result.append(line)
    return "".join(result)


def contains_fragment(text: str, fragment: str) -> bool:
    """Whitespace-normalised substring test.

    Used for the deletion-completeness assertion: 'no fragment the fix was meant
    to delete survives'. Both `text` and `fragment` have internal whitespace runs
    collapsed to a single space before comparison, so wrapping/indentation
    differences do not cause a false negative.
    """
    def _norm(s: str) -> str:
        return " ".join(s.split())

    return _norm(fragment) in _norm(text)


def strip_tag_lines(lines: Collection[str]) -> list[str]:
    """Return `lines` with every boundary tag line (any kind, open or close) removed.

    Not part of the design's acceptance_helpers contract, but a small local
    convenience used by the isolation assertions: comparing prose with tag
    wrapping subtracted out on both sides of a transform.
    """
    kept: list[str] = []
    for line in lines:
        if _ANY_TAG_RE.match(line.rstrip("\r\n").strip()):
            continue
        kept.append(line)
    return kept
