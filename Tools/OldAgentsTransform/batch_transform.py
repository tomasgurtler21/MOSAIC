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
import dataclasses
import pathlib
import sys

# Add Tools/ to sys.path so boundary_transformer can be imported
TOOLS_DIR = pathlib.Path(__file__).parent
sys.path.insert(0, str(TOOLS_DIR))
from boundary_transformer import SkipReason, is_orchestrator_file, transform_file
from non_conformance import NonConformance, render_report
from generic_lookup import REPO_ROOT, build_generic_map, file_base


@dataclasses.dataclass
class BatchSummary:
    """Outcome counts for one batch run."""
    processed: int   # files for which a transform was attempted (excludes skipped-orchestrator)
    degraded: int    # subset of processed that went through the degraded path
    skipped: int     # already-transformed files left untouched
    errors: int      # failed transforms plus harness-only orchestrators
    non_conformances: int = 0   # count of NonConformance items accumulated across the batch

    def ok(self) -> bool:
        """True when the batch produced no errors."""
        return self.errors == 0


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


def run_batch(harness_dirs: list[pathlib.Path], generic_map: dict[str, pathlib.Path],
              batch_label: str) -> BatchSummary:
    """Transform all files in the given harness dirs. Returns a BatchSummary."""
    processed = 0
    degraded = 0
    skipped = 0
    errors = 0
    all_ncs: list[NonConformance] = []

    for hdir in harness_dirs:
        files = get_harness_files(hdir)
        for hfile in files:
            base = file_base(hfile)
            generic_ref = generic_map.get(base)

            if generic_ref is not None:
                # Generic counterpart found: transform normally.
                result = transform_file(hfile, generic_ref_path=generic_ref)
            else:
                # No generic counterpart found.
                # Harness-only orchestrators are skipped and counted as errors.
                if is_orchestrator_file(hfile):
                    print(f"  [WARN]    {hfile}  harness-only orchestrator requires --generic-ref")
                    errors += 1
                    continue

                # Regular harness-only agent: route through the degraded-quality transform.
                result = transform_file(hfile)

            all_ncs.extend(result.non_conformances)

            # Utility-agent and non-agent skips: increment skipped only, not processed.
            if result.skip_reason in (SkipReason.UTILITY_AGENT, SkipReason.NON_AGENT):
                skip_msg = result.warnings[0] if result.warnings else str(hfile)
                print(f"  [SKIP]    {hfile}  {skip_msg}")
                skipped += 1
                continue

            processed += 1
            if result.success:
                if result.skipped:
                    print(f"  [SKIP]    {hfile}  already transformed")
                    skipped += 1
                else:
                    if generic_ref is not None:
                        print(f"  [OK]      {hfile}  {result.version_before} -> {result.version_after}")
                    else:
                        print(
                            f"  [DEGRADED]  {hfile}  "
                            f"{result.version_before} -> {result.version_after}"
                            f"   (no generic reference)"
                        )
                        if result.degraded:
                            degraded += 1
            else:
                errors += 1
                for err in result.errors:
                    print(f"  [ERR]     {hfile}:{err.line_number}: {err.message}",
                          file=sys.stderr)

    print(
        f"\nBatch {batch_label}: {processed} files processed "
        f"({degraded} degraded, {skipped} skipped), {errors} errors."
    )
    print(render_report(all_ncs))
    return BatchSummary(
        processed=processed,
        degraded=degraded,
        skipped=skipped,
        errors=errors,
        non_conformances=len(all_ncs),
    )


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
        summary = run_batch(BATCH_DIRS[batch], generic_map, batch)
        if not summary.ok():
            all_ok = False

    return 0 if all_ok else 1


if __name__ == "__main__":
    sys.exit(main())
