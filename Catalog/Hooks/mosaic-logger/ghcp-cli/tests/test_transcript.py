"""Tests for the defensive transcript JSONL reader in mosaic_logger_transcript (ghcp-cli variant).

Key differences from the claude-code variant:
- read_last_assistant_facts checks BOTH 'type: assistant' AND 'role: assistant' patterns
  to maximize compatibility with different transcript formats

Covers:
  read_last_assistant_facts: model and token_usage extraction from last assistant record,
  dual-pattern detection (type and role), key mapping (cache_read_input_tokens -> cache_read_tokens),
  last-record-wins behavior, degradation on missing/malformed/empty transcripts,
  never-raises contract
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

def _write_transcript(path: pathlib.Path, records: list) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        "\n".join(json.dumps(r, ensure_ascii=False) for r in records) + "\n",
        encoding="utf-8",
    )


def _transcript_file(tmp_path: pathlib.Path, records: list) -> str:
    p = tmp_path / "transcript.jsonl"
    _write_transcript(p, records)
    return str(p)


def _assistant_type_record(model="claude-opus-4-5",
                            input_tokens=None, output_tokens=None,
                            cache_read=None, cache_creation=None) -> dict:
    """Build a 'type: assistant' record (Claude Code style)."""
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


def _assistant_role_record(model="ghcp-model-v1") -> dict:
    """Build a 'role: assistant' record (GHCP CLI style)."""
    return {"role": "assistant", "model": model}


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
        f = transcript.TurnFacts(model="claude-opus-4-5")
        self.assertEqual("claude-opus-4-5", f.model)

    def test_token_usage_set_when_provided(self):
        usage = {"input_tokens": 100, "output_tokens": 50}
        f = transcript.TurnFacts(token_usage=usage)
        self.assertEqual(usage, f.token_usage)


# ---------------------------------------------------------------------------
# read_last_assistant_facts -- type: assistant pattern
# ---------------------------------------------------------------------------

class TestReadLastAssistantFactsTypePattern(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_turn_facts_instance(self):
        path = _transcript_file(self.tmp_path, [_assistant_type_record()])
        result = transcript.read_last_assistant_facts(path)
        self.assertIsInstance(result, transcript.TurnFacts)

    def test_extracts_model_from_type_assistant(self):
        path = _transcript_file(self.tmp_path, [_assistant_type_record(model="claude-opus-4-5")])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("claude-opus-4-5", facts.model)

    def test_extracts_input_tokens(self):
        path = _transcript_file(self.tmp_path,
                                [_assistant_type_record(input_tokens=200, output_tokens=80)])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNotNone(facts.token_usage)
        self.assertEqual(200, facts.token_usage["input_tokens"])

    def test_extracts_output_tokens(self):
        path = _transcript_file(self.tmp_path,
                                [_assistant_type_record(input_tokens=100, output_tokens=50)])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual(50, facts.token_usage["output_tokens"])

    def test_maps_cache_read_input_tokens_to_cache_read_tokens(self):
        path = _transcript_file(self.tmp_path,
                                [_assistant_type_record(cache_read=500)])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIn("cache_read_tokens", facts.token_usage)
        self.assertEqual(500, facts.token_usage["cache_read_tokens"])

    def test_maps_cache_creation_input_tokens_to_cache_creation_tokens(self):
        path = _transcript_file(self.tmp_path,
                                [_assistant_type_record(cache_creation=200)])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIn("cache_creation_tokens", facts.token_usage)
        self.assertEqual(200, facts.token_usage["cache_creation_tokens"])

    def test_last_record_wins(self):
        path = _transcript_file(self.tmp_path, [
            _assistant_type_record(model="first-model"),
            _assistant_type_record(model="last-model"),
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("last-model", facts.model)

    def test_skips_non_assistant_records(self):
        path = _transcript_file(self.tmp_path, [
            {"type": "user", "message": {"role": "user", "content": "Hello"}},
            _assistant_type_record(model="claude-opus-4-5"),
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("claude-opus-4-5", facts.model)

    def test_model_none_when_message_has_no_model(self):
        path = _transcript_file(self.tmp_path,
                                [{"type": "assistant", "message": {}}])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNone(facts.model)

    def test_token_usage_none_when_no_usage_in_message(self):
        path = _transcript_file(self.tmp_path,
                                [{"type": "assistant", "message": {"model": "m1"}}])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNone(facts.token_usage)


# ---------------------------------------------------------------------------
# read_last_assistant_facts -- role: assistant pattern (GHCP CLI format)
# ---------------------------------------------------------------------------

class TestReadLastAssistantFactsRolePattern(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_detects_role_assistant_records(self):
        """GHCP CLI transcripts may use 'role: assistant' instead of 'type: assistant'."""
        path = _transcript_file(self.tmp_path, [_assistant_role_record(model="ghcp-model-v1")])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsInstance(facts, transcript.TurnFacts)

    def test_extracts_top_level_model_from_role_record(self):
        """Some formats place model at the top level of the record."""
        path = _transcript_file(self.tmp_path, [
            {"role": "assistant", "model": "ghcp-top-level-model"},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("ghcp-top-level-model", facts.model)

    def test_skips_role_user_records(self):
        path = _transcript_file(self.tmp_path, [
            {"role": "user", "content": "Hello"},
            {"role": "assistant", "model": "correct-model"},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("correct-model", facts.model)

    def test_extracts_token_usage_from_role_assistant_record(self):
        """token_usage at the top level of a role: assistant record is extracted,
        symmetrizing with the type: assistant token_usage tests above."""
        path = _transcript_file(self.tmp_path, [
            {"role": "assistant", "model": "ghcp-model-v1",
             "token_usage": {"input_tokens": 150, "output_tokens": 60}},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNotNone(facts.token_usage)
        self.assertEqual(150, facts.token_usage["input_tokens"])
        self.assertEqual(60, facts.token_usage["output_tokens"])

    def test_mixed_type_and_role_records_returns_last_assistant(self):
        """When both patterns appear, the last assistant record wins."""
        path = _transcript_file(self.tmp_path, [
            _assistant_type_record(model="first-type-model"),
            {"role": "assistant", "model": "last-role-model"},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertEqual("last-role-model", facts.model)


# ---------------------------------------------------------------------------
# read_last_assistant_facts -- degradation
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

    def test_skips_malformed_json_lines(self):
        path = self.tmp_path / "mixed.jsonl"
        path.write_text(
            "not json at all\n"
            + json.dumps(_assistant_type_record(model="after-bad-line")) + "\n",
            encoding="utf-8",
        )
        facts = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("after-bad-line", facts.model)

    def test_skips_blank_lines(self):
        path = self.tmp_path / "blanks.jsonl"
        path.write_text(
            "\n\n" + json.dumps(_assistant_type_record(model="valid")) + "\n\n",
            encoding="utf-8",
        )
        facts = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("valid", facts.model)

    def test_returns_empty_facts_when_no_assistant_records(self):
        path = _transcript_file(self.tmp_path, [
            {"type": "user", "message": {"content": "Hello"}},
            {"type": "system", "content": "System prompt"},
        ])
        facts = transcript.read_last_assistant_facts(path)
        self.assertIsNone(facts.model)
        self.assertIsNone(facts.token_usage)

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

    def test_model_and_token_usage_are_independently_optional(self):
        """Each field may be None without the other being None."""
        path = _transcript_file(self.tmp_path, [
            _assistant_type_record(model="claude-opus-4-5",
                                   input_tokens=100, output_tokens=50),
        ])
        facts = transcript.read_last_assistant_facts(path)
        # Both fields resolved
        self.assertIsNotNone(facts.model)
        self.assertIsNotNone(facts.token_usage)


if __name__ == "__main__":
    unittest.main()
