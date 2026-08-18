"""Non-conformance types and codes for the MOSAIC boundary tools.

This module is created at Stage 6 with the minimum surface that
frontmatter_build needs: the NonConformance record and the NC_TIER_PLACEHOLDER
code. Stage 8 extends this module with the remaining NC_* codes,
detect_output_non_conformances, and render_report.

All constants whose names begin with NC_ are stable; their string values
must not change across Stages because they are stored on NonConformance.code
and compared in test assertions.

NC-7A (OutputFormat retains JSON response envelopes) is retired: once no
document can carry an OutputFormat section, the finding can never fire.
"""
from __future__ import annotations

import dataclasses
import pathlib
from collections.abc import Mapping, Sequence

from boundary_constants import TAG_PATTERN
from fence import fence_mask


# ---------------------------------------------------------------------------
# Non-conformance codes
# ---------------------------------------------------------------------------

NC_NO_INJECTIONS:    str = "NC-7B"   # output has zero injection regions (type="project")
NC_HARNESS_PROSE:    str = "NC-7C"   # harness-specific prose in agent-authored sections
NC_DRIFTED_BULLET:   str = "NC-D1F"  # superseded bullet found in drifted wording
NC_TIER_PLACEHOLDER: str = "NC-TIER" # one missing tier key needs manual completion


# ---------------------------------------------------------------------------
# Message templates
# ---------------------------------------------------------------------------

NC_MESSAGES: Mapping[str, str] = {
    NC_TIER_PLACEHOLDER: (
        "Frontmatter key '{detail}' is absent or empty; "
        "set it manually before deploying this agent"
    ),
    NC_NO_INJECTIONS: (
        "Output carries zero injection regions (type=\"project\"); a project cannot "
        "customise this agent — a human should decide whether that is correct"
    ),
    NC_HARNESS_PROSE: (
        "Harness-specific prose found under heading '{detail}'; it belongs in "
        "HarnessConstraints — flagged, not moved"
    ),
    NC_DRIFTED_BULLET: (
        "A superseded bullet ('{detail}') was found in drifted wording and left in "
        "place — a human should reconcile it"
    ),
}

# ---------------------------------------------------------------------------
# Closed vocabulary for the class-7c harness-prose heading heuristic.
# Stage 8 uses this; declared here because it is part of this module's contract.
# ---------------------------------------------------------------------------

HARNESS_PROSE_HEADING_TERMS: tuple[str, ...] = (
    "tool usage",
    "platform",
    "claude code",
    "ghcp cli",
    "opencode",
    "vs code ghcp",
    "copilot",
)


# ---------------------------------------------------------------------------
# Data structures
# ---------------------------------------------------------------------------

@dataclasses.dataclass(frozen=True)
class NonConformance:
    """A single non-conformance finding for one file.

    Fields:
        code        — one of the NC_* constants in this module.
        file        — the path of the file that contains the finding.
        message     — a human-readable description, formatted from NC_MESSAGES[code].
        line_number — 1-based line number, or None when the finding is file-scoped.
        detail      — free-form discriminator within a class:
                        NC_DRIFTED_BULLET  -> the originating DeletionRule.rule_id
                        NC_HARNESS_PROSE   -> the heading text that matched
                        NC_TIER_PLACEHOLDER -> the name of the missing frontmatter key
    """
    code: str
    file: pathlib.Path
    message: str
    line_number: int | None = None
    detail: str | None = None


# ---------------------------------------------------------------------------
# Stage 8 detection helpers
# ---------------------------------------------------------------------------

def _detect_zero_injections(
    path: pathlib.Path,
    lines: Sequence[str],
) -> list[NonConformance]:
    """Class 7b: output carries zero injection open tags (type="project")."""
    for line in lines:
        m = TAG_PATTERN.match(line.strip())
        if m is not None and m.group("close") == "" and m.group("kind") == "INJECTION":
            return []
    return [NonConformance(
        code=NC_NO_INJECTIONS,
        file=path,
        message=NC_MESSAGES[NC_NO_INJECTIONS],
    )]


def _detect_harness_prose(
    path: pathlib.Path,
    lines: Sequence[str],
    sections: Mapping[str, object],
) -> list[NonConformance]:
    """Class 7c: harness-specific H3 headings inside any section region."""
    mask = fence_mask(lines)
    findings: list[NonConformance] = []
    seen_lines: set[int] = set()

    for span in sections.values():
        start = getattr(span, "start", 0)
        end = min(getattr(span, "content_end", len(lines)), len(lines))
        for i in range(start, end):
            if i in seen_lines or mask[i]:
                continue
            line = lines[i]
            if not (line.startswith("### ") and not line.startswith("#### ")):
                continue
            heading_text = line.strip()[4:].strip()
            lower = heading_text.lower()
            if any(term in lower for term in HARNESS_PROSE_HEADING_TERMS):
                findings.append(NonConformance(
                    code=NC_HARNESS_PROSE,
                    file=path,
                    message=NC_MESSAGES[NC_HARNESS_PROSE].format(detail=heading_text),
                    line_number=i + 1,
                    detail=heading_text,
                ))
                seen_lines.add(i)

    return findings


def detect_output_non_conformances(
    path: pathlib.Path,
    lines: Sequence[str],
    sections: Mapping[str, object],
) -> list[NonConformance]:
    """Scan transformed output for classes 7b and 7c.

    Detection only. This function never modifies `lines`.

    NC-7A (OutputFormat JSON envelopes) is retired: no document can carry an
    OutputFormat section after legacy-section deletion, so the finding can never
    fire and the detector is removed.
    """
    findings: list[NonConformance] = []
    findings.extend(_detect_zero_injections(path, lines))
    findings.extend(_detect_harness_prose(path, lines, sections))
    return findings


def render_report(items: Sequence[NonConformance]) -> str:
    """Render a human-readable, per-file grouped report."""
    header = "Non-conformance report"
    out_lines: list[str] = [header, "-" * len(header)]

    file_order: list[pathlib.Path] = []
    by_file: dict[pathlib.Path, list[NonConformance]] = {}
    for nc in items:
        if nc.file not in by_file:
            by_file[nc.file] = []
            file_order.append(nc.file)
        by_file[nc.file].append(nc)

    for file_path in file_order:
        out_lines.append(str(file_path))
        for nc in by_file[file_path]:
            out_lines.append(f"  [{nc.code}] {nc.message}")

    out_lines.append(
        f"{len(items)} non-conformances across {len(file_order)} files."
    )
    return "\n".join(out_lines) + "\n"
