"""Shared repo-root resolution and generic-counterpart lookup."""
from __future__ import annotations
import pathlib

# Absolute path to the mosaic repo root, resolved from this module's own
# location, independent of the process's current working directory.
#
# This module lives at <repo>/Tools/OldAgentsTransform/generic_lookup.py.
# Three parent hops: generic_lookup.py → OldAgentsTransform/ → Tools/ → <repo>
REPO_ROOT: pathlib.Path = pathlib.Path(__file__).parent.parent.parent

# Root of the generic agent tree: REPO_ROOT / "Catalog" / "Subagents"
GENERIC_AGENTS_ROOT: pathlib.Path = REPO_ROOT / "Catalog" / "Subagents"

# REPO_ROOT / "Catalog" / "Orchestrator" / "orchestrator.md"
GENERIC_ORCHESTRATOR_PATH: pathlib.Path = (
    REPO_ROOT / "Catalog" / "Orchestrator" / "orchestrator.md"
)


def file_base(path: pathlib.Path) -> str:
    """Return the base stem of a file, handling the double extension .agent.md.

    'contracts-designer.agent.md' -> 'contracts-designer'
    'contracts-designer.md'       -> 'contracts-designer'
    """
    name = path.name
    if name.endswith(".agent.md"):
        return name[: -len(".agent.md")]
    return path.stem


def build_generic_map() -> dict[str, pathlib.Path]:
    """Return a mapping of base filename -> generic file Path.

    Globs GENERIC_AGENTS_ROOT recursively for '*.md', skipping files named
    'README.md', keying each remaining file by Path.stem. Adds an
    'orchestrator' entry pointing at GENERIC_ORCHESTRATOR_PATH when that file
    exists. Returns an empty mapping when GENERIC_AGENTS_ROOT does not exist;
    it never raises for a missing tree.

    Matching semantics are unchanged from batch_transform.build_generic_map():
    last writer wins on a stem collision; no disambiguation is performed.
    """
    mapping: dict[str, pathlib.Path] = {}

    if not GENERIC_AGENTS_ROOT.exists():
        if GENERIC_ORCHESTRATOR_PATH.exists():
            mapping["orchestrator"] = GENERIC_ORCHESTRATOR_PATH
        return mapping

    for p in GENERIC_AGENTS_ROOT.rglob("*.md"):
        if p.name == "README.md":
            continue
        mapping[p.stem] = p

    if GENERIC_ORCHESTRATOR_PATH.exists():
        mapping["orchestrator"] = GENERIC_ORCHESTRATOR_PATH

    return mapping


def find_generic_ref(
    path: pathlib.Path,
    generic_map: dict[str, pathlib.Path] | None = None,
) -> pathlib.Path | None:
    """Return the generic counterpart of `path`, or None when there is no match.

    Looks up file_base(path) in `generic_map`. When `generic_map` is None, one
    is built via build_generic_map(). Pure lookup: reads no file content and
    consults no frontmatter, so callers own the decision of when lookup applies.
    """
    if generic_map is None:
        generic_map = build_generic_map()
    return generic_map.get(file_base(path))
