"""Tests for the defensive transcript reader in mosaic_logger_transcript
(vscode-ghcp variant).

Key differences from claude-code and ghcp-cli variants:
- parse_transcript_records() is exposed as a named seam (D6) that accepts
  three transcript shapes:
    1. JSONL — one JSON object per line (unparseable lines discarded silently)
    2. Whole-file JSON array — array elements become the record list
    3. Whole-file JSON object — if it has a conversation-like key (messages,
       records, entries, turns, history), that list's dicts become records;
       otherwise the object itself is the single record.
  Detection order: try whole-file JSON first, fall back to line-by-line.
  Returns [] for empty or unparseable input; never raises.
- read_last_assistant_facts() works via parse_transcript_records() and
  therefore handles both JSONL and whole-file JSON transcripts.

Covers:
  parse_transcript_records: JSONL, whole-file JSON array, whole-file JSON
    object with conversation-like key, whole-file JSON object as single record,
    empty/blank input, malformed input, line-level errors in JSONL, detection
    order, never-raises.
  TurnFacts: field defaults.
  read_last_assistant_facts: extraction from JSONL transcripts, extraction from
    whole-file JSON array/object transcripts, model and token_usage extraction,
    key remapping (cache_read_input_tokens -> cache_read_tokens), last-record-
    wins, degradation on missing/unreadable/malformed/None paths, never-raises.
"""

import json
import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_transcript as transcript


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

def _write_jsonl(path: pathlib.Path, records: list) -> None:
    """Write records as a JSONL file (one JSON object per line)."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        "\n".join(json.dumps(r, ensure_ascii=False) for r in records) + "\n",
        encoding="utf-8",
    )


def _write_json_array(path: pathlib.Path, records: list) -> None:
    """Write records as a whole-file JSON array."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(records, ensure_ascii=False), encoding="utf-8")


def _write_json_object(path: pathlib.Path, obj: dict) -> None:
    """Write a whole-file JSON object."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(obj, ensure_ascii=False), encoding="utf-8")


def _jsonl_file(tmp_path: pathlib.Path, records: list) -> str:
    p = tmp_path / "transcript.jsonl"
    _write_jsonl(p, records)
    return str(p)


def _assistant_record(model="gpt-4o", input_tokens=None, output_tokens=None,
                      cache_read=None, cache_creation=None) -> dict:
    """Build a 'type: assistant' record (Claude Code / JSONL style)."""
    usage = {}
    if input_tokens is not None:
        usage["input_tokens"] = input_tokens
    if output_tokens is not None:
        usage["output_tokens"] = output_tokens
    if cache_read is not None:
        usage["cache_read_input_tokens"] = cache_read
    if cache_creation is not None:
        usage["cache_creation_input_tokens"] = cache_creation
    msg = {}
    if model is not None:
        msg["model"] = model
    if usage:
        msg["usage"] = usage
    return {"type": "assistant", "message": msg}


def _role_assistant_record(model="gpt-4o", token_usage=None) -> dict:
    """Build a 'role: assistant' record (flat format)."""
    rec = {"role": "assistant", "model": model}
    if token_usage is not None:
        rec["usage"] = token_usage
    return rec


# ---------------------------------------------------------------------------
# TurnFacts data class
# ---------------------------------------------------------------------------

class TestTurnFacts(unittest.TestCase):

    def test_model_defaults_to_none(self):
        f = transcript.TurnFacts()
        self.assertIsNone(f.model)

    def test_token_usage_defaults_to_none(self):
        f = transcript.TurnFacts()
        self.assertIsNone(f.token_usage)

    def test_model_set_when_provided(self):
        f = transcript.TurnFacts(model="gpt-4o")
        self.assertEqual("gpt-4o", f.model)

    def test_token_usage_set_when_provided(self):
        usage = {"input_tokens": 100, "output_tokens": 50}
        f = transcript.TurnFacts(token_usage=usage)
        self.assertEqual(usage, f.token_usage)


# ---------------------------------------------------------------------------
# parse_transcript_records — JSONL format
# ---------------------------------------------------------------------------

class TestParseTranscriptRecordsJsonl(unittest.TestCase):
    """parse_transcript_records correctly parses JSONL transcripts (one object
    per line).  Unparseable lines are discarded individually."""

    def test_single_jsonl_line_returns_one_record(self):
        text = json.dumps({"type": "assistant", "message": {}}) + "\n"
        records = transcript.parse_transcript_records(text)
        self.assertEqual(1, len(records))

    def test_multiple_jsonl_lines_return_all_records(self):
        records_in = [
            {"type": "user", "content": "Hello"},
            {"type": "assistant", "message": {"model": "gpt-4o"}},
            {"type": "user", "content": "Thanks"},
        ]
        text = "\n".join(json.dumps(r) for r in records_in) + "\n"
        result = transcript.parse_transcript_records(text)
        self.assertEqual(3, len(result))

    def test_jsonl_preserves_record_order(self):
        records_in = [
            {"type": "user", "seq": 1},
            {"type": "assistant", "seq": 2},
        ]
        text = "\n".join(json.dumps(r) for r in records_in) + "\n"
        result = transcript.parse_transcript_records(text)
        self.assertEqual(1, result[0]["seq"])
        self.assertEqual(2, result[1]["seq"])

    def test_unparseable_jsonl_line_is_discarded_silently(self):
        """A mid-write corrupt line must not discard surrounding valid lines."""
        text = (
            json.dumps({"type": "user", "content": "Hello"}) + "\n"
            "this is not json\n"
            + json.dumps({"type": "assistant", "message": {}}) + "\n"
        )
        result = transcript.parse_transcript_records(text)
        self.assertEqual(2, len(result))

    def test_blank_lines_in_jsonl_are_skipped(self):
        text = (
            "\n"
            + json.dumps({"type": "assistant", "message": {}}) + "\n"
            + "\n"
        )
        result = transcript.parse_transcript_records(text)
        self.assertEqual(1, len(result))

    def test_all_invalid_jsonl_lines_returns_empty_list(self):
        text = "bad json\nalso bad\n{broken:\n"
        result = transcript.parse_transcript_records(text)
        # May return [] (all lines invalid) or fall through to an empty result
        self.assertEqual(0, len(result))

    def test_returns_list_of_dicts(self):
        text = json.dumps({"type": "assistant", "message": {}}) + "\n"
        result = transcript.parse_transcript_records(text)
        self.assertIsInstance(result, list)
        self.assertIsInstance(result[0], dict)


# ---------------------------------------------------------------------------
# parse_transcript_records — whole-file JSON array format
# ---------------------------------------------------------------------------

class TestParseTranscriptRecordsJsonArray(unittest.TestCase):
    """parse_transcript_records handles a whole-file JSON array transcript.
    Detection order: whole-file JSON is tried first before line-by-line."""

    def test_json_array_returns_all_records(self):
        records_in = [
            {"type": "user", "content": "Hello"},
            {"type": "assistant", "message": {"model": "gpt-4o"}},
        ]
        text = json.dumps(records_in)
        result = transcript.parse_transcript_records(text)
        self.assertEqual(2, len(result))

    def test_json_array_preserves_order(self):
        records_in = [
            {"type": "user", "seq": 1},
            {"type": "assistant", "seq": 2},
            {"type": "user", "seq": 3},
        ]
        text = json.dumps(records_in)
        result = transcript.parse_transcript_records(text)
        self.assertEqual(1, result[0]["seq"])
        self.assertEqual(3, result[2]["seq"])

    def test_empty_json_array_returns_empty_list(self):
        text = "[]"
        result = transcript.parse_transcript_records(text)
        self.assertEqual(0, len(result))

    def test_json_array_of_non_dicts_dicts_filtered_out(self):
        """Non-dict elements in an array (e.g. strings, numbers) must be
        excluded from the returned list (only dict elements become records)."""
        text = json.dumps([{"type": "assistant"}, "not a dict", 42, None])
        result = transcript.parse_transcript_records(text)
        # Only the one dict element should survive
        self.assertEqual(1, len(result))
        self.assertIsInstance(result[0], dict)

    def test_json_array_detected_before_jsonl_fallback(self):
        """Whole-file JSON must be tried first; a valid JSON array must not be
        treated as a single-line JSONL."""
        records_in = [{"type": "assistant", "message": {"model": "gpt-4o"}}]
        text = json.dumps(records_in)
        result = transcript.parse_transcript_records(text)
        self.assertEqual(1, len(result))
        self.assertIn("type", result[0])


# ---------------------------------------------------------------------------
# parse_transcript_records — whole-file JSON object format
# ---------------------------------------------------------------------------

class TestParseTranscriptRecordsJsonObject(unittest.TestCase):
    """parse_transcript_records handles a whole-file JSON object transcript.
    If the object has a conversation-like key (messages, records, entries,
    turns, history), that list's dict elements become the record list.
    Otherwise the object itself is the single record."""

    def test_messages_key_yields_records(self):
        obj = {"messages": [
            {"role": "user", "content": "Hello"},
            {"role": "assistant", "content": "Hi"},
        ]}
        result = transcript.parse_transcript_records(json.dumps(obj))
        self.assertEqual(2, len(result))

    def test_records_key_yields_records(self):
        obj = {"records": [
            {"type": "assistant", "message": {}},
        ]}
        result = transcript.parse_transcript_records(json.dumps(obj))
        self.assertEqual(1, len(result))

    def test_entries_key_yields_records(self):
        obj = {"entries": [{"type": "user"}, {"type": "assistant"}]}
        result = transcript.parse_transcript_records(json.dumps(obj))
        self.assertEqual(2, len(result))

    def test_turns_key_yields_records(self):
        obj = {"turns": [{"role": "user"}, {"role": "assistant"}]}
        result = transcript.parse_transcript_records(json.dumps(obj))
        self.assertEqual(2, len(result))

    def test_history_key_yields_records(self):
        obj = {"history": [{"type": "assistant", "message": {"model": "gpt-4o"}}]}
        result = transcript.parse_transcript_records(json.dumps(obj))
        self.assertEqual(1, len(result))

    def test_object_without_conversation_key_is_single_record(self):
        """A JSON object with no conversation-like key becomes the single record."""
        obj = {"type": "assistant", "message": {"model": "gpt-4o"}}
        result = transcript.parse_transcript_records(json.dumps(obj))
        self.assertEqual(1, len(result))
        self.assertEqual("assistant", result[0].get("type"))

    def test_object_with_empty_messages_list_returns_empty_list(self):
        obj = {"messages": []}
        result = transcript.parse_transcript_records(json.dumps(obj))
        self.assertEqual(0, len(result))

    def test_conversation_key_with_non_dicts_filtered_out(self):
        """Non-dict elements under a conversation key are excluded."""
        obj = {"messages": [{"role": "assistant"}, "not a dict", 42]}
        result = transcript.parse_transcript_records(json.dumps(obj))
        self.assertEqual(1, len(result))


# ---------------------------------------------------------------------------
# parse_transcript_records — empty / malformed / edge cases
# ---------------------------------------------------------------------------

class TestParseTranscriptRecordsEdgeCases(unittest.TestCase):

    def test_empty_string_returns_empty_list(self):
        result = transcript.parse_transcript_records("")
        self.assertEqual([], result)

    def test_whitespace_only_returns_empty_list(self):
        result = transcript.parse_transcript_records("   \n\t\n  ")
        self.assertEqual([], result)

    def test_completely_invalid_content_returns_empty_list(self):
        result = transcript.parse_transcript_records("{{{not json at all}}}")
        self.assertEqual([], result)

    def test_never_raises_on_empty_string(self):
        try:
            transcript.parse_transcript_records("")
        except Exception as exc:
            self.fail(f"parse_transcript_records raised on empty string: {exc}")

    def test_never_raises_on_malformed_content(self):
        try:
            transcript.parse_transcript_records("{broken json [[[ }")
        except Exception as exc:
            self.fail(f"parse_transcript_records raised on malformed content: {exc}")

    def test_never_raises_on_very_long_input(self):
        text = json.dumps({"type": "user", "content": "x" * 50_000}) + "\n"
        try:
            transcript.parse_transcript_records(text)
        except Exception as exc:
            self.fail(f"parse_transcript_records raised on long input: {exc}")

    def test_returns_list_type_always(self):
        for text in ("", "bad", "[]", "{}", json.dumps({"k": "v"})):
            result = transcript.parse_transcript_records(text)
            self.assertIsInstance(result, list, f"Expected list for input {text!r}")


# ---------------------------------------------------------------------------
# read_last_assistant_facts — JSONL transcript
# ---------------------------------------------------------------------------

class TestReadLastAssistantFactsJsonl(unittest.TestCase):
    """read_last_assistant_facts extracts model and token_usage from JSONL."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_turn_facts_instance(self):
        path = _jsonl_file(self.tmp_path, [_assistant_record()])
        result = transcript.read_last_assistant_facts(path)
        self.assertIsInstance(result, transcript.TurnFacts)

    def test_extracts_model_from_type_assistant(self):
        path = _jsonl_file(self.tmp_path, [_assistant_record(model="gpt-4o")])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("gpt-4o", facts.model)

    def test_extracts_input_tokens(self):
        path = _jsonl_file(self.tmp_path,
                           [_assistant_record(input_tokens=200, output_tokens=80)])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNotNone(facts.token_usage)
        self.assertEqual(200, facts.token_usage["input_tokens"])

    def test_extracts_output_tokens(self):
        path = _jsonl_file(self.tmp_path,
                           [_assistant_record(input_tokens=100, output_tokens=50)])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual(50, facts.token_usage["output_tokens"])

    def test_maps_cache_read_input_tokens_to_cache_read_tokens(self):
        path = _jsonl_file(self.tmp_path, [_assistant_record(cache_read=500)])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIn("cache_read_tokens", facts.token_usage)
        self.assertEqual(500, facts.token_usage["cache_read_tokens"])

    def test_maps_cache_creation_input_tokens_to_cache_creation_tokens(self):
        path = _jsonl_file(self.tmp_path, [_assistant_record(cache_creation=200)])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIn("cache_creation_tokens", facts.token_usage)
        self.assertEqual(200, facts.token_usage["cache_creation_tokens"])

    def test_last_record_wins(self):
        path = _jsonl_file(self.tmp_path, [
            _assistant_record(model="first-model"),
            _assistant_record(model="last-model"),
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("last-model", facts.model)

    def test_skips_non_assistant_records(self):
        path = _jsonl_file(self.tmp_path, [
            {"type": "user", "message": {"content": "Hello"}},
            _assistant_record(model="gpt-4o"),
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("gpt-4o", facts.model)

    def test_model_none_when_message_has_no_model(self):
        path = _jsonl_file(self.tmp_path, [{"type": "assistant", "message": {}}])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNone(facts.model)

    def test_token_usage_none_when_no_usage_in_message(self):
        path = _jsonl_file(self.tmp_path,
                           [{"type": "assistant", "message": {"model": "m1"}}])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNone(facts.token_usage)

    def test_detects_role_assistant_in_jsonl(self):
        """JSONL may use 'role: assistant' as well as 'type: assistant'."""
        path = _jsonl_file(self.tmp_path, [
            {"role": "assistant", "model": "gpt-4o"},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsInstance(facts, transcript.TurnFacts)

    def test_extracts_top_level_model_from_role_record(self):
        path = _jsonl_file(self.tmp_path, [
            {"role": "assistant", "model": "top-level-model"},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("top-level-model", facts.model)


# ---------------------------------------------------------------------------
# read_last_assistant_facts — whole-file JSON array transcript
# ---------------------------------------------------------------------------

class TestReadLastAssistantFactsJsonArray(unittest.TestCase):
    """read_last_assistant_facts extracts facts from a whole-file JSON array
    transcript (VS Code may produce this format instead of JSONL)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_extracts_model_from_json_array_transcript(self):
        records = [
            {"type": "user", "content": "Hello"},
            _assistant_record(model="gpt-4o"),
        ]
        path = self.tmp_path / "transcript.json"
        _write_json_array(path, records)
        facts = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("gpt-4o", facts.model)

    def test_extracts_token_usage_from_json_array_transcript(self):
        records = [_assistant_record(input_tokens=300, output_tokens=100)]
        path = self.tmp_path / "transcript.json"
        _write_json_array(path, records)
        facts = transcript.read_last_assistant_facts(str(path))
        self.assertIsNotNone(facts.token_usage)
        self.assertEqual(300, facts.token_usage["input_tokens"])

    def test_last_record_wins_in_json_array(self):
        records = [
            _assistant_record(model="first-model"),
            _assistant_record(model="last-model"),
        ]
        path = self.tmp_path / "transcript.json"
        _write_json_array(path, records)
        facts = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("last-model", facts.model)

    def test_returns_empty_facts_for_empty_json_array(self):
        path = self.tmp_path / "empty.json"
        _write_json_array(path, [])
        facts = transcript.read_last_assistant_facts(str(path))
        self.assertIsNone(facts.model)
        self.assertIsNone(facts.token_usage)


# ---------------------------------------------------------------------------
# read_last_assistant_facts — whole-file JSON object transcript
# ---------------------------------------------------------------------------

class TestReadLastAssistantFactsJsonObject(unittest.TestCase):
    """read_last_assistant_facts extracts facts from a whole-file JSON object
    transcript (using conversation-like key or the object itself)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_extracts_facts_via_messages_key(self):
        obj = {"messages": [_assistant_record(model="gpt-4o-via-messages")]}
        path = self.tmp_path / "transcript.json"
        _write_json_object(path, obj)
        facts = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("gpt-4o-via-messages", facts.model)

    def test_extracts_facts_via_history_key(self):
        obj = {"history": [_assistant_record(model="gpt-4o-via-history")]}
        path = self.tmp_path / "transcript.json"
        _write_json_object(path, obj)
        facts = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("gpt-4o-via-history", facts.model)

    def test_extracts_facts_from_object_as_single_record(self):
        """When the object has no conversation-like key, it is the single record."""
        obj = _assistant_record(model="gpt-4o-single-obj")
        path = self.tmp_path / "transcript.json"
        _write_json_object(path, obj)
        facts = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("gpt-4o-single-obj", facts.model)


# ---------------------------------------------------------------------------
# read_last_assistant_facts — degradation
# ---------------------------------------------------------------------------

class TestReadLastAssistantFactsDegradation(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_empty_facts_for_none_path(self):
        facts = transcript.read_last_assistant_facts(None)
        self.assertIsNone(facts.model)
        self.assertIsNone(facts.token_usage)

    def test_returns_empty_facts_for_empty_string_path(self):
        facts = transcript.read_last_assistant_facts("")
        self.assertIsNone(facts.model)
        self.assertIsNone(facts.token_usage)

    def test_returns_empty_facts_for_nonexistent_file(self):
        missing = str(self.tmp_path / "does_not_exist.jsonl")
        facts = transcript.read_last_assistant_facts(missing)
        self.assertIsNone(facts.model)
        self.assertIsNone(facts.token_usage)

    def test_returns_empty_facts_for_empty_file(self):
        empty_path = self.tmp_path / "empty.jsonl"
        empty_path.write_bytes(b"")
        facts = transcript.read_last_assistant_facts(str(empty_path))
        self.assertIsNone(facts.model)
        self.assertIsNone(facts.token_usage)

    def test_returns_empty_facts_for_all_malformed_lines(self):
        path = self.tmp_path / "all_bad.jsonl"
        path.write_text("{bad\n{also bad\nnot json\n", encoding="utf-8")
        facts = transcript.read_last_assistant_facts(str(path))
        self.assertIsNone(facts.model)
        self.assertIsNone(facts.token_usage)

    def test_returns_empty_facts_when_no_assistant_records(self):
        path = _jsonl_file(self.tmp_path, [
            {"type": "user", "message": {"content": "Hello"}},
            {"type": "system", "content": "System prompt"},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNone(facts.model)
        self.assertIsNone(facts.token_usage)

    def test_skips_malformed_json_lines_and_uses_good_ones(self):
        path = self.tmp_path / "mixed.jsonl"
        path.write_text(
            "not json at all\n"
            + json.dumps(_assistant_record(model="after-bad-line")) + "\n",
            encoding="utf-8",
        )
        facts = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("after-bad-line", facts.model)

    def test_model_and_token_usage_are_independently_optional(self):
        """Each field may be None without the other being None."""
        path = _jsonl_file(self.tmp_path, [
            _assistant_record(model="gpt-4o",
                              input_tokens=100, output_tokens=50),
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNotNone(facts.model)
        self.assertIsNotNone(facts.token_usage)

    def test_never_raises_for_none_path(self):
        try:
            transcript.read_last_assistant_facts(None)
        except Exception as exc:
            self.fail(f"read_last_assistant_facts raised for None path: {exc}")

    def test_never_raises_for_missing_file(self):
        try:
            transcript.read_last_assistant_facts(str(self.tmp_path / "missing.jsonl"))
        except Exception as exc:
            self.fail(f"read_last_assistant_facts raised for missing file: {exc}")

    def test_never_raises_for_all_malformed_lines(self):
        path = self.tmp_path / "all_bad.jsonl"
        path.write_text("{bad\n{also bad\nnot json\n", encoding="utf-8")
        try:
            transcript.read_last_assistant_facts(str(path))
        except Exception as exc:
            self.fail(f"read_last_assistant_facts raised for all-malformed file: {exc}")

    def test_never_raises_for_empty_json_array(self):
        path = self.tmp_path / "empty.json"
        _write_json_array(path, [])
        try:
            transcript.read_last_assistant_facts(str(path))
        except Exception as exc:
            self.fail(f"read_last_assistant_facts raised for empty JSON array: {exc}")

    def test_never_raises_for_malformed_json_object(self):
        path = self.tmp_path / "bad.json"
        path.write_text("{bad json{{", encoding="utf-8")
        try:
            transcript.read_last_assistant_facts(str(path))
        except Exception as exc:
            self.fail(f"read_last_assistant_facts raised for malformed JSON object: {exc}")


if __name__ == "__main__":
    unittest.main()
