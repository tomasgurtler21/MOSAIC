"""mosaic_logger_transcript.py — Defensive transcript JSONL reader.

Owns: reading model and token usage from assistant records in a Claude Code
transcript JSONL, defensively (malformed lines discarded, never raises).
Exposes both a last-record reader (read_last_assistant_facts, unchanged) and
a per-record reader (read_assistant_records) that yields facts for every
assistant record observed.

Has no bundle dependencies (stdlib only).
"""

import hashlib
import json
import os
import sys


# ---------------------------------------------------------------------------
# Windows long-path helper
# ---------------------------------------------------------------------------

def _long_path_safe(path_str):
    """Return a path string safe for use with open() on Windows paths > 260 chars.

    On Windows, prepends the \\?\\ extended-path prefix so that the OS skips
    the MAX_PATH limit. Normalizes to an absolute backslash path first via
    os.path.abspath() so that \\?\\ is accepted. Paths already carrying the
    prefix are returned unchanged. On non-Windows platforms this is a no-op.
    """
    if sys.platform != "win32":
        return path_str
    if path_str.startswith("\\\\?\\"):
        return path_str
    return "\\\\?\\" + os.path.abspath(path_str)


# ---------------------------------------------------------------------------
# Data structures
# ---------------------------------------------------------------------------

class TurnFacts:
    """Model and token usage extracted from the last assistant record in a transcript.

    Both fields are independently optional; each is None when unresolvable.
    """

    def __init__(self, model=None, token_usage=None):
        self.model = model
        self.token_usage = token_usage


class RecordFacts:
    """Facts extracted from ONE assistant record in a transcript.

    Fields:
      record_id     - str, always non-empty. The stable per-record identifier.
      record_index  - int, 0-based ordinal among assistant records in file order.
      model         - str | None, resolved exactly as read_last_assistant_facts
                      resolves it (message.model, then top-level model).
      token_usage   - dict | None, the mapped usage block; None when no
                      sub-field resolved.
      service_tier  - str | None, first non-empty string among
                      message.usage.service_tier, message.service_tier,
                      message.usage.speed.
    """

    def __init__(self, record_id, record_index,
                 model=None, token_usage=None, service_tier=None):
        self.record_id = record_id
        self.record_index = record_index
        self.model = model
        self.token_usage = token_usage
        self.service_tier = service_tier


# ---------------------------------------------------------------------------
# Key mapping from harness transcript fields to schema field names
# ---------------------------------------------------------------------------

_USAGE_KEY_MAP = [
    ("input_tokens",                "input_tokens"),
    ("output_tokens",               "output_tokens"),
    ("cache_read_input_tokens",     "cache_read_tokens"),
    ("cache_creation_input_tokens", "cache_creation_tokens"),
]


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------

def read_last_assistant_facts(transcript_path):
    """Read the model and token usage of the LAST complete assistant record.

    Parses the transcript JSONL at transcript_path line by line. Malformed or
    non-object lines are discarded rather than treated as fatal. Returns a
    TurnFacts with model=None and token_usage=None on every failure path.

    Contract:
      - Missing, unreadable, or None path -> TurnFacts(None, None)
      - Only records with type == "assistant" are considered.
      - model: message.model, falling back to top-level model; None when
        neither is a non-empty string.
      - token_usage: built from message.usage with remapped keys; each
        sub-field is included only when it is a real number (including 0);
        the whole object is None when no sub-field resolved.
      - Never raises.
    """
    try:
        if not transcript_path:
            return TurnFacts()

        last_facts = TurnFacts()

        try:
            with open(_long_path_safe(transcript_path), "r", encoding="utf-8", errors="replace") as f:
                for raw_line in f:
                    line = raw_line.strip()
                    if not line:
                        continue

                    # Discard unparseable lines (harness may be mid-write)
                    try:
                        record = json.loads(line)
                    except (json.JSONDecodeError, ValueError):
                        continue

                    if not isinstance(record, dict):
                        continue
                    if record.get("type") != "assistant":
                        continue

                    # --- Resolve model ---
                    model = None
                    message = record.get("message")
                    if isinstance(message, dict):
                        msg_model = message.get("model")
                        if isinstance(msg_model, str) and msg_model:
                            model = msg_model

                    if model is None:
                        top_model = record.get("model")
                        if isinstance(top_model, str) and top_model:
                            model = top_model

                    # --- Resolve token_usage ---
                    token_usage = None
                    if isinstance(message, dict):
                        raw_usage = message.get("usage")
                        if isinstance(raw_usage, dict):
                            mapped = {}
                            for src_key, dst_key in _USAGE_KEY_MAP:
                                val = raw_usage.get(src_key)
                                # Keep genuine numbers (including 0), reject None/bool
                                if isinstance(val, (int, float)) and not isinstance(val, bool):
                                    mapped[dst_key] = val
                            if mapped:
                                token_usage = mapped

                    last_facts = TurnFacts(model=model, token_usage=token_usage)

        except Exception:
            # Any I/O or unexpected error degrades to empty facts
            return TurnFacts()

        return last_facts

    except Exception:
        return TurnFacts()


# ---------------------------------------------------------------------------
# Per-record helpers
# ---------------------------------------------------------------------------

def _resolve_record_model(record):
    """Resolve model exactly as read_last_assistant_facts does: message.model,
    falling back to top-level model; None when neither is a non-empty string."""
    message = record.get("message")
    model = None
    if isinstance(message, dict):
        msg_model = message.get("model")
        if isinstance(msg_model, str) and msg_model:
            model = msg_model

    if model is None:
        top_model = record.get("model")
        if isinstance(top_model, str) and top_model:
            model = top_model

    return model


def _resolve_record_token_usage(message):
    """Build the mapped usage block from message.usage, unchanged from B2."""
    token_usage = None
    if isinstance(message, dict):
        raw_usage = message.get("usage")
        if isinstance(raw_usage, dict):
            mapped = {}
            for src_key, dst_key in _USAGE_KEY_MAP:
                val = raw_usage.get(src_key)
                # Keep genuine numbers (including 0), reject None/bool
                if isinstance(val, (int, float)) and not isinstance(val, bool):
                    mapped[dst_key] = val
            if mapped:
                token_usage = mapped
    return token_usage


def _resolve_record_service_tier(message):
    """First non-empty string among message.usage.service_tier,
    message.service_tier, message.usage.speed."""
    if not isinstance(message, dict):
        return None

    usage = message.get("usage")
    if isinstance(usage, dict):
        usage_tier = usage.get("service_tier")
        if isinstance(usage_tier, str) and usage_tier:
            return usage_tier

    message_tier = message.get("service_tier")
    if isinstance(message_tier, str) and message_tier:
        return message_tier

    if isinstance(usage, dict):
        speed = usage.get("speed")
        if isinstance(speed, str) and speed:
            return speed

    return None


def _resolve_record_id(record, record_index, record_line_bytes):
    """message.id, then record uuid, then a deterministic position hash (D1)."""
    message = record.get("message")
    if isinstance(message, dict):
        msg_id = message.get("id")
        if isinstance(msg_id, str) and msg_id:
            return msg_id

    record_uuid = record.get("uuid")
    if isinstance(record_uuid, str) and record_uuid:
        return record_uuid

    digest = hashlib.sha256(record_line_bytes).hexdigest()[:16]
    return "pos:{}:{}".format(record_index, digest)


def read_assistant_records(transcript_path):
    """Return RecordFacts for EVERY assistant record, in file order.

    One streaming single pass, as read_last_assistant_facts is. Preserves
    every element of the existing defensive contract:
      - Missing, unreadable or None path -> []
      - Malformed and non-object lines discarded, not fatal
      - Only records with type == "assistant" considered
      - Each usage sub-field included only when it is a real number
        (including 0) and never when None or bool
      - Never raises; any I/O or unexpected error degrades to []

    Deduplication is explicitly NOT this function's responsibility. Two calls
    against a transcript that has since been appended to legitimately return
    the same leading records with the same record_id values.
    """
    try:
        if not transcript_path:
            return []

        results = []

        try:
            with open(_long_path_safe(transcript_path), "r", encoding="utf-8", errors="replace") as f:
                record_index = 0
                for raw_line in f:
                    line = raw_line.strip()
                    if not line:
                        continue

                    # Discard unparseable lines (harness may be mid-write)
                    try:
                        record = json.loads(line)
                    except (json.JSONDecodeError, ValueError):
                        continue

                    if not isinstance(record, dict):
                        continue
                    if record.get("type") != "assistant":
                        continue

                    message = record.get("message")
                    model = _resolve_record_model(record)
                    token_usage = _resolve_record_token_usage(message)
                    service_tier = _resolve_record_service_tier(message)
                    record_id = _resolve_record_id(
                        record, record_index, line.encode("utf-8")
                    )

                    results.append(RecordFacts(
                        record_id=record_id,
                        record_index=record_index,
                        model=model,
                        token_usage=token_usage,
                        service_tier=service_tier,
                    ))
                    record_index += 1

        except Exception:
            # Any I/O or unexpected error degrades to an empty list
            return []

        return results

    except Exception:
        return []
