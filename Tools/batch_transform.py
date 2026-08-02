"""Batch transformation runner for MOSAIC harness files.

Usage:
    py Tools/batch_transform.py --batch B
    py Tools/batch_transform.py --batch C
    py Tools/batch_transform.py --batch D
    py Tools/batch_transform.py --batch E
    py Tools/batch_transform.py --all
"""
from __future__ import annotations

import argparse
import pathlib
import sys
import re

# Add Tools/ to sys.path so boundary_transformer can be imported
TOOLS_DIR = pathlib.Path(__file__).parent
sys.path.insert(0, str(TOOLS_DIR))
from boundary_transformer import transform_file

REPO_ROOT = TOOLS_DIR.parent

# Build a mapping: base_name -> Path of the generic file
def build_generic_map() -> dict[str, pathlib.Path]:
    """Return a dict mapping base filename (no extension) to its generic Path."""
    generic_agents_root = REPO_ROOT / "Agents" / "Generic" / "Agents"
    generic_orch = REPO_ROOT / "Agents" / "Generic" / "Orchestrator" / "orchestrator.md"

    mapping: dict[str, pathlib.Path] = {}
    for p in generic_agents_root.rglob("*.md"):
        if p.name == "README.md":
            continue
        base = p.stem  # e.g. "implementation-tdd"
        mapping[base] = p

    # Orchestrator
    if generic_orch.exists():
        mapping["orchestrator"] = generic_orch

    return mapping


def get_harness_files(harness_dir: pathlib.Path) -> list[pathlib.Path]:
    """Return all agent .md / .agent.md files under harness_dir/Agents/ and the orchestrator."""
    agents_dir = harness_dir / "Agents"
    files: list[pathlib.Path] = []
    if agents_dir.exists():
        for p in sorted(agents_dir.iterdir()):
            if p.name == "README.md":
                continue
            if p.suffix == ".md" or p.name.endswith(".agent.md"):
                files.append(p)

    # Orchestrator (may be .md or .agent.md)
    for name in ["orchestrator.md", "orchestrator.agent.md"]:
        orch = harness_dir / name
        if orch.exists():
            files.append(orch)
            break

    return files


def file_base(path: pathlib.Path) -> str:
    """Return the base stem for a file (handles double extension like .agent.md)."""
    name = path.name
    if name.endswith(".agent.md"):
        return name[: -len(".agent.md")]
    return path.stem


def run_batch(harness_dirs: list[pathlib.Path], generic_map: dict[str, pathlib.Path],
              batch_label: str) -> bool:
    """Transform all files in the given harness dirs. Returns True if all succeeded."""
    total = 0
    errors = 0

    for hdir in harness_dirs:
        files = get_harness_files(hdir)
        for hfile in files:
            base = file_base(hfile)
            generic_ref = generic_map.get(base)
            if generic_ref is None:
                print(f"  [WARN] No generic ref for {hfile.relative_to(REPO_ROOT)}", file=sys.stderr)
                errors += 1
                continue

            result = transform_file(hfile, generic_ref_path=generic_ref)
            total += 1
            if result.success:
                print(f"  [OK]   {hfile.relative_to(REPO_ROOT)}  {result.version_before} -> {result.version_after}")
            else:
                errors += 1
                for err in result.errors:
                    print(f"  [ERR]  {hfile.relative_to(REPO_ROOT)}:{err.line_number}: {err.message}",
                          file=sys.stderr)

    print(f"\nBatch {batch_label}: {total} files processed, {errors} errors.")
    return errors == 0


BATCH_DIRS: dict[str, list[pathlib.Path]] = {
    "B": [
        REPO_ROOT / "Agents" / "Claude Code" / "CodebaseAgnostic",
        REPO_ROOT / "Agents" / "Claude Code" / "ExampleProject",
    ],
    "C": [
        REPO_ROOT / "Agents" / "GHCP CLI" / "CodebaseAgnostic",
        REPO_ROOT / "Agents" / "GHCP CLI" / "ExampleProject",
    ],
    "D": [
        REPO_ROOT / "Agents" / "OpenCode" / "CodebaseAgnostic",
        REPO_ROOT / "Agents" / "OpenCode" / "ExampleProject",
    ],
    "E": [
        REPO_ROOT / "Agents" / "VS code GHCP" / "CodebaseAgnostic",
        REPO_ROOT / "Agents" / "VS code GHCP" / "ExampleProject",
    ],
}


def main() -> int:
    parser = argparse.ArgumentParser(description="Batch transform MOSAIC harness files.")
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--batch", choices=["B", "C", "D", "E"],
                       help="Run a single batch")
    group.add_argument("--all", action="store_true",
                       help="Run all harness batches B-E")
    args = parser.parse_args()

    generic_map = build_generic_map()
    print(f"Generic map built: {len(generic_map)} files")

    batches_to_run = list(BATCH_DIRS.keys()) if args.all else [args.batch]
    all_ok = True

    for batch in batches_to_run:
        print(f"\n=== Batch {batch} ===")
        ok = run_batch(BATCH_DIRS[batch], generic_map, batch)
        if not ok:
            all_ok = False

    return 0 if all_ok else 1


if __name__ == "__main__":
    sys.exit(main())
