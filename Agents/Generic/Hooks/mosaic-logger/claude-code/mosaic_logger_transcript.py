"""mosaic_logger_transcript.py — Defensive transcript JSONL reader.

Owns: reading model and token usage from the last assistant record in a Claude Code
transcript JSONL, defensively (malformed lines discarded, never raises).

Has no bundle dependencies (stdlib only).
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
            with open(transcript_path, "r", encoding="utf-8", errors="replace") as f:
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
