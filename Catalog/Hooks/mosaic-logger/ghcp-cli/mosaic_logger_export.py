"""mosaic_logger_export.py -- Byte-for-byte transcript export with sidecar metadata."""

import datetime
import json
import pathlib

import mosaic_logger_core as core


# ---------------------------------------------------------------------------
# Data structures
# ---------------------------------------------------------------------------

class ExportResult:
    """Outcome of an export_transcript call."""

    def __init__(self, ok: bool,
                 raw_path: "pathlib.Path | None" = None,
                 meta_path: "pathlib.Path | None" = None,
                 byte_count: "int | None" = None,
                 reason: "str | None" = None):
        self.ok = ok
        self.raw_path = raw_path
        self.meta_path = meta_path
        self.byte_count = byte_count
        self.reason = reason


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _now_iso() -> str:
    """Current UTC time as ISO 8601 with ms precision and Z suffix."""
    now = datetime.datetime.now(datetime.timezone.utc)
    ms = now.microsecond // 1000
    return now.strftime("%Y-%m-%dT%H:%M:%S.") + f"{ms:03d}Z"


def _sidecar_path(raw_path: pathlib.Path) -> pathlib.Path:
    """Derive the sidecar path from the raw path."""
    return raw_path.with_suffix("").with_suffix(".meta.json")


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------

def build_sidecar(source_path: str,
                  source_mechanism: str,
                  byte_count: int) -> dict:
    """Build the sidecar document with harness='ghcp-cli' and
    native_format='ghcp-cli-transcript-jsonl'."""
    return {
        "harness": core.HARNESS,
        "native_format": "ghcp-cli-transcript-jsonl",
        "source_path": source_path,
        "source_mechanism": source_mechanism,
        "captured_at": _now_iso(),
        "byte_count": byte_count,
    }


def export_transcript(source_path: "str | None",
                      target_raw: pathlib.Path,
                      source_mechanism: str) -> ExportResult:
    """Copy transcript byte-for-byte via atomic replace. Write sidecar on success.
    Never raises."""
    try:
        if not source_path:
            return ExportResult(ok=False, reason="empty or None source_path")

        src = pathlib.Path(source_path)
        try:
            data = src.read_bytes()
        except Exception as exc:
            core.debug_log(f"export_transcript: cannot read {source_path}", exc)
            return ExportResult(ok=False, reason=f"unreadable: {exc}")

        byte_count = len(data)
        target_raw.parent.mkdir(parents=True, exist_ok=True)
        ok = core.atomic_replace(target_raw, data)
        if not ok:
            return ExportResult(ok=False, reason="atomic_replace failed for raw file")

        sidecar_doc = build_sidecar(source_path, source_mechanism, byte_count)
        sidecar_json = json.dumps(sidecar_doc, ensure_ascii=False).encode("utf-8")
        meta_path = _sidecar_path(target_raw)
        core.atomic_replace(meta_path, sidecar_json)

        return ExportResult(ok=True, raw_path=target_raw, meta_path=meta_path,
                            byte_count=byte_count)

    except Exception as exc:
        core.debug_log("export_transcript failed unexpectedly", exc)
        return ExportResult(ok=False, reason=str(exc))
