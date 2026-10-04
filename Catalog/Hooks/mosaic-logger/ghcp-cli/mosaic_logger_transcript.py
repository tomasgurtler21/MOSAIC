"""mosaic_logger_transcript.py -- Defensive JSONL transcript reader.

Has no bundle dependencies (stdlib only).

Checks for both 'type: assistant' and 'role: assistant' patterns to maximize
compatibility with GHCP CLI transcripts that may differ in structure from
Claude Code transcripts.
"""

import json


# ---------------------------------------------------------------------------
# Data structures
# ---------------------------------------------------------------------------

class TurnFacts:
    """Model and token usage from the last assistant record.
    Both fields independently optional; each is None when unresolvable."""

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

def read_last_assistant_facts(transcript_path) -> TurnFacts:
    """Read model and token_usage from the LAST assistant record in transcript JSONL.

    Malformed lines discarded. Returns TurnFacts(None, None) on every failure path.
    Never raises.

    Checks for both 'type: assistant' and 'role: assistant' patterns to maximize
    compatibility with different transcript formats.
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

                    try:
                        record = json.loads(line)
                    except (json.JSONDecodeError, ValueError):
                        continue

                    if not isinstance(record, dict):
                        continue

                    # Accept both 'type: assistant' and 'role: assistant'
                    is_assistant = (
                        record.get("type") == "assistant" or
                        record.get("role") == "assistant"
                    )
                    if not is_assistant:
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
                                if isinstance(val, (int, float)) and not isinstance(val, bool):
                                    mapped[dst_key] = val
                            if mapped:
                                token_usage = mapped

                    # Fallback: check top-level 'token_usage' field (GHCP CLI role:assistant format)
                    if token_usage is None:
                        top_usage = record.get("token_usage")
                        if isinstance(top_usage, dict):
                            mapped = {}
                            for src_key, dst_key in _USAGE_KEY_MAP:
                                val = top_usage.get(src_key)
                                if isinstance(val, (int, float)) and not isinstance(val, bool):
                                    mapped[dst_key] = val
                            if mapped:
                                token_usage = mapped

                    last_facts = TurnFacts(model=model, token_usage=token_usage)

        except Exception:
            return TurnFacts()

        return last_facts

    except Exception:
        return TurnFacts()


def _text_of(content) -> "str | None":
    """Text of a message content value: a string, or the text blocks of a list."""
    if isinstance(content, str):
        return content or None
    if isinstance(content, list):
        parts = [b.get("text") for b in content
                 if isinstance(b, dict) and b.get("type") == "text"
                 and isinstance(b.get("text"), str) and b.get("text")]
        return "\n".join(parts) or None
    return None


def _assistant_text(record: dict) -> "str | None":
    """Assistant text of one record, or None when it is not an assistant record
    or carries no text. Understands the GHCP event shape (assistant.message with
    data.content) and the type/role 'assistant' shapes."""
    if record.get("type") == "assistant.message":
        data = record.get("data")
        return _text_of(data.get("content")) if isinstance(data, dict) else None
    if record.get("type") != "assistant" and record.get("role") != "assistant":
        return None
    message = record.get("message")
    if isinstance(message, dict):
        text = _text_of(message.get("content"))
        if text is not None:
            return text
    return _text_of(record.get("content"))


def read_last_assistant_text(transcript_path) -> "str | None":
    """Text of the LAST assistant record that carries text, or None when the
    transcript is missing, unreadable, malformed or holds no assistant text.
    Tool-use blocks are never part of the text. Never raises."""
    try:
        if not isinstance(transcript_path, str) or not transcript_path:
            return None
        last = None
        with open(transcript_path, "r", encoding="utf-8", errors="replace") as f:
            for raw_line in f:
                line = raw_line.strip()
                if not line:
                    continue
                try:
                    record = json.loads(line)
                except (json.JSONDecodeError, ValueError):
                    continue
                if isinstance(record, dict):
                    text = _assistant_text(record)
                    if text is not None:
                        last = text
        return last
    except Exception:
        return None
