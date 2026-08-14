"""Non-conformance types and codes for the MOSAIC boundary tools.

This module is created at Stage 6 with the minimum surface that
frontmatter_build needs: the NonConformance and JsonEnvelopeExample records
and the NC_TIER_PLACEHOLDER code. Stage 8 extends this module with the
remaining NC_* codes, detect_output_non_conformances, and render_report.

All constants whose names begin with NC_ are stable; their string values
must not change across Stages because they are stored on NonConformance.code
and compared in test assertions.
"""
from __future__ import annotations

import dataclasses
import json
import pathlib
from collections.abc import Mapping, Sequence

from boundary_constants import TAG_PATTERN
from fence import fence_mask


# ---------------------------------------------------------------------------
# Non-conformance codes
# ---------------------------------------------------------------------------

NC_JSON_ENVELOPE:    str = "NC-7A"   # OutputFormat retains JSON response envelopes
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
    NC_JSON_ENVELOPE: (
        "Output Format retains a JSON response envelope; the required shape is a "
        "two-column table of Status / error_code / example status_message, with no "
        "JSON fence — a human must judge how to convert it"
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
class JsonEnvelopeExample:
    """A single JSON response envelope example found in an OutputFormat section.

    Populated only for NC_JSON_ENVELOPE findings; the evidence tuple on
    NonConformance is empty for all other codes.
    """
    status_code: str       # e.g. "SUCCESS", "BLOCKED"
    status_message: str    # verbatim, outer quotes stripped
    error_code: str | None # present only on BLOCKED examples; None otherwise
    line_number: int       # 1-based line where the envelope's fence opens


@dataclasses.dataclass(frozen=True)
class NonConformance:
    """A single non-conformance finding for one file.

    Fields:
        code        — one of the NC_* constants in this module.
        file        — the path of the file that contains the finding.
        message     — a human-readable description, formatted from NC_MESSAGES[code].
        line_number — 1-based line number, or None when the finding is file-scoped.
        evidence    — populated only for NC_JSON_ENVELOPE; empty tuple otherwise.
        detail      — free-form discriminator within a class:
                        NC_DRIFTED_BULLET  -> the originating DeletionRule.rule_id
                        NC_HARNESS_PROSE   -> the heading text that matched
                        NC_TIER_PLACEHOLDER -> the name of the missing frontmatter key
    """
    code: str
    file: pathlib.Path
    message: str
    line_number: int | None = None
    evidence: tuple[JsonEnvelopeExample, ...] = ()
    detail: str | None = None


# ---------------------------------------------------------------------------
# Stage 8 detection helpers
# ---------------------------------------------------------------------------

def _iter_fenced_blocks(lines: Sequence[str], start: int, stop: int):
    """Yield (open_idx, close_idx, lang, content_lines) for fenced blocks in [start, stop).

    close_idx is the index of the closing fence line (or `stop` when unterminated).
    content_lines are the lines strictly between the fences.
    """
    i = start
    while i < stop:
        stripped = lines[i].strip()
        if stripped.startswith("```") or stripped.startswith("~~~"):
            fence_marker = stripped[:3]
            lang = stripped[3:].strip()
            open_idx = i
            j = i + 1
            content: list[str] = []
            while j < stop and not lines[j].strip().startswith(fence_marker):
                content.append(lines[j])
                j += 1
            yield open_idx, j, lang, content
            i = j + 1
        else:
            i += 1


def _detect_json_envelopes(
    path: pathlib.Path,
    lines: Sequence[str],
    sections: Mapping[str, object],
) -> list[NonConformance]:
    """Class 7a: JSON response envelopes retained in the Output Format section."""
    of_span = sections.get("OutputFormat")
    if of_span is None:
        return []

    stop = min(getattr(of_span, "content_end", len(lines)), len(lines))
    start = getattr(of_span, "start", 0)

    evidence: list[JsonEnvelopeExample] = []
    for open_idx, _close_idx, lang, content in _iter_fenced_blocks(lines, start, stop):
        text = "".join(content).strip()
        if not text:
            continue
        try:
            obj = json.loads(text)
        except (ValueError, TypeError):
            obj = None
        is_json_lang = lang.strip().lower() == "json"
        if isinstance(obj, dict) and "status_code" in obj and (is_json_lang or True):
            status_code = str(obj.get("status_code", ""))
            status_message = str(obj.get("status_message", ""))
            error_code = obj.get("error_code") if status_code == "BLOCKED" else None
            evidence.append(JsonEnvelopeExample(
                status_code=status_code,
                status_message=status_message,
                error_code=error_code,
                line_number=open_idx + 1,
            ))

    if not evidence:
        return []

    return [NonConformance(
        code=NC_JSON_ENVELOPE,
        file=path,
        message=NC_MESSAGES[NC_JSON_ENVELOPE],
        evidence=tuple(evidence),
    )]


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
    """Scan transformed output for classes 7a, 7b and 7c.

    Detection only. This function never modifies `lines`.
    """
    findings: list[NonConformance] = []
    findings.extend(_detect_json_envelopes(path, lines, sections))
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
            for ex in nc.evidence:
                error_code = ex.error_code if ex.error_code else "-"
                out_lines.append(
                    f'    - status_code={ex.status_code} '
                    f'status_message="{ex.status_message}" error_code={error_code}'
                )

    out_lines.append(
        f"{len(items)} non-conformances across {len(file_order)} files."
    )
    return "\n".join(out_lines) + "\n"
