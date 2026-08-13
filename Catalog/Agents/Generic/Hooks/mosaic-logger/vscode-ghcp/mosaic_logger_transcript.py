"""mosaic_logger_transcript.py — Defensive transcript reader for the VS Code GHCP adapter.

Owns: parsing model and token usage from the last assistant record in a transcript
that may be JSONL, a whole-file JSON array, or a whole-file JSON object.

Has no bundle dependencies (stdlib only).

Key divergence from claude-code (D6): the transcript format is undocumented and
explicitly unstable. parse_transcript_records() is the single format-detection
seam. It accepts three shapes:
  1. JSONL — one JSON object per line (unparseable lines discarded individually).
  2. Whole-file JSON array — array's dict elements become the record list.
  3. Whole-file JSON object — if it has a conversation-like key (messages, records,
     entries, turns, history), that list's dict elements become the record list;
     otherwise the object itself is the single record.
Detection order: try whole-file JSON first, fall back to line-by-line.
"""

import json


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


# ---------------------------------------------------------------------------
# Key mapping from harness transcript fields to schema field names
# ---------------------------------------------------------------------------

_USAGE_KEY_MAP = [
    ("input_tokens",                "input_tokens"),
    ("output_tokens",               "output_tokens"),
    ("cache_read_input_tokens",     "cache_read_tokens"),
    ("cache_creation_input_tokens", "cache_creation_tokens"),
]

# Conversation-like keys to look for in a whole-file JSON object
_CONVERSATION_KEYS = ("messages", "records", "entries", "turns", "history")


# ---------------------------------------------------------------------------
# Format detection seam (D6)
# ---------------------------------------------------------------------------

def parse_transcript_records(text: str) -> list:
    """Parse raw transcript text into a flat list of record dicts.

    Accepts three transcript shapes, tried in order:
      1. Whole-file JSON (tried first):
         - If the parsed result is a list, filter to dicts only and return.
         - If the parsed result is a dict:
             * If it has a conversation-like key (messages, records, entries,
               turns, history) whose value is a list, use that list filtered
               to dicts.
             * Otherwise treat the dict itself as the single record.
      2. JSONL fallback: parse line by line, discarding unparseable lines
         individually.

    Returns [] for empty input or any content that yields no valid records.
    Never raises.
    """
    try:
        if not text or not text.strip():
            return []

        # Try whole-file JSON first (D6 detection order)
        try:
            parsed = json.loads(text)
            if isinstance(parsed, list):
                return [r for r in parsed if isinstance(r, dict)]
            if isinstance(parsed, dict):
                for key in _CONVERSATION_KEYS:
                    val = parsed.get(key)
                    if isinstance(val, list):
                        return [r for r in val if isinstance(r, dict)]
                # Object with no conversation key — the object itself is the record
                return [parsed]
        except (json.JSONDecodeError, ValueError):
            pass

        # JSONL fallback: line-by-line, discard unparseable lines
        records = []
        for line in text.splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                record = json.loads(line)
                if isinstance(record, dict):
                    records.append(record)
            except (json.JSONDecodeError, ValueError):
                continue
        return records

    except Exception:
        return []


# ---------------------------------------------------------------------------
# Public API
# ---------------------------------------------------------------------------

def read_last_assistant_facts(transcript_path) -> TurnFacts:
    """Read the model and token usage of the LAST complete assistant record.

    Reads and parses the transcript at transcript_path using
    parse_transcript_records (which handles JSONL and whole-file JSON).
    Malformed content is discarded rather than treated as fatal.
    Returns a TurnFacts with model=None and token_usage=None on every
    failure path.

    Contract:
      - Missing, unreadable, None, or empty path -> TurnFacts(None, None)
      - Records with type == "assistant" OR role == "assistant" are considered.
      - model: message.model, falling back to top-level model; None when
        neither is a non-empty string.
      - token_usage: built from message.usage (or top-level usage) with
        remapped keys; each sub-field is included only when it is a real
        number (including 0); the whole object is None when no sub-field
        resolved.
      - Never raises.
    """
    try:
        if not transcript_path:
            return TurnFacts()

        try:
            with open(transcript_path, "r", encoding="utf-8", errors="replace") as f:
                text = f.read()
        except Exception:
            return TurnFacts()

        records = parse_transcript_records(text)

        last_facts = TurnFacts()
        for record in records:
            if not isinstance(record, dict):
                continue
            # Accept both type==assistant and role==assistant
            if record.get("type") != "assistant" and record.get("role") != "assistant":
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
            # Try message.usage first, fall back to top-level usage
            raw_usage = None
            if isinstance(message, dict):
                raw_usage = message.get("usage")
            if not isinstance(raw_usage, dict):
                raw_usage = record.get("usage")

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

        return last_facts

    except Exception:
        return TurnFacts()
