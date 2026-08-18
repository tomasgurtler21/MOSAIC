"""Tests for the defensive transcript JSONL reader in mosaic_logger_transcript.

Covers T3.4: read_last_assistant_facts returns the model and token_usage of the
LAST complete assistant record, with correct key mapping, independent optional
sub-fields, degradation on missing/malformed/empty transcripts, and never-raises
contract.

ALL tests in this file are expected to FAIL (RED phase) until
mosaic_logger_transcript.read_last_assistant_facts is implemented.
"""

import json
import os
import pathlib
import re
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_transcript as transcript


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

def _write_transcript(path: pathlib.Path, records: list) -> None:
    """Write a list of dicts as JSONL lines, creating parent dirs as needed."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        "\n".join(json.dumps(r, ensure_ascii=False) for r in records) + "\n",
        encoding="utf-8",
    )


def _assistant_record(
    model: str | None = "claude-opus-4-5",
    input_tokens: int | None = None,
    output_tokens: int | None = None,
    cache_read_input_tokens: int | None = None,
    cache_creation_input_tokens: int | None = None,
    message_id: str | None = None,
    record_uuid: str | None = None,
    usage_service_tier: str | None = None,
    message_service_tier: str | None = None,
    usage_speed: str | None = None,
) -> dict:
    """Build a transcript assistant record with the documented structure.

    The identifier and service-tier parameters are additive and only present
    in the record when explicitly supplied, so pre-existing callers that omit
    them are unaffected.
    """
    usage = {}
    if input_tokens is not None:
        usage["input_tokens"] = input_tokens
    if output_tokens is not None:
        usage["output_tokens"] = output_tokens
    if cache_read_input_tokens is not None:
        usage["cache_read_input_tokens"] = cache_read_input_tokens
    if cache_creation_input_tokens is not None:
        usage["cache_creation_input_tokens"] = cache_creation_input_tokens
    if usage_service_tier is not None:
        usage["service_tier"] = usage_service_tier
    if usage_speed is not None:
        usage["speed"] = usage_speed

    msg: dict = {}
    if model is not None:
        msg["model"] = model
    if message_id is not None:
        msg["id"] = message_id
    if message_service_tier is not None:
        msg["service_tier"] = message_service_tier
    if usage:
        msg["usage"] = usage

    record: dict = {"type": "assistant", "message": msg}
    if record_uuid is not None:
        record["uuid"] = record_uuid
    return record


def _user_record(content: str = "Hello") -> dict:
    return {"type": "user", "message": {"role": "user", "content": content}}


# ---------------------------------------------------------------------------
# Module-level: TurnFacts return type contract
# ---------------------------------------------------------------------------

class TestTurnFactsReturnType(unittest.TestCase):
    """read_last_assistant_facts must return an object with model and token_usage."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_object_with_model_attribute(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [_assistant_record(model="claude-opus-4-5")])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertTrue(
            hasattr(result, "model"),
            "Return value has no 'model' attribute",
        )

    def test_returns_object_with_token_usage_attribute(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [_assistant_record()])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertTrue(
            hasattr(result, "token_usage"),
            "Return value has no 'token_usage' attribute",
        )


# ---------------------------------------------------------------------------
# Model resolution
# ---------------------------------------------------------------------------

class TestModelResolution(unittest.TestCase):
    """model comes from message.model with fallback to top-level model."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_model_from_message_model(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {"type": "assistant", "message": {"model": "claude-opus-4-5", "usage": {}}},
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("claude-opus-4-5", result.model)

    def test_model_from_top_level_key_when_message_model_absent(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {"type": "assistant", "model": "claude-haiku-3", "message": {"usage": {}}},
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("claude-haiku-3", result.model)

    def test_message_model_takes_priority_over_top_level_model(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {
                "type": "assistant",
                "model": "top-level-model",
                "message": {"model": "message-model", "usage": {}},
            },
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("message-model", result.model)

    def test_model_none_when_both_absent(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {"type": "assistant", "message": {"usage": {"input_tokens": 5}}},
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertIsNone(result.model)

    def test_model_none_when_message_model_is_empty_string(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {"type": "assistant", "message": {"model": "", "usage": {}}},
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertIsNone(result.model)

    def test_returns_last_assistant_record_model(self):
        """When multiple assistant records exist, return model from the LAST one."""
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {"type": "assistant", "message": {"model": "first-model", "usage": {}}},
            _user_record(),
            {"type": "assistant", "message": {"model": "last-model", "usage": {}}},
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("last-model", result.model)


# ---------------------------------------------------------------------------
# Token usage key mapping
# ---------------------------------------------------------------------------

class TestTokenUsageKeyMapping(unittest.TestCase):
    """Harness key names are remapped to the schema names."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_input_tokens_mapped_correctly(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(input_tokens=100),
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertIsNotNone(result.token_usage)
        self.assertEqual(100, result.token_usage["input_tokens"])

    def test_output_tokens_mapped_correctly(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(output_tokens=50),
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertIsNotNone(result.token_usage)
        self.assertEqual(50, result.token_usage["output_tokens"])

    def test_cache_read_input_tokens_mapped_to_cache_read_tokens(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(cache_read_input_tokens=200),
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertIsNotNone(result.token_usage)
        self.assertIn("cache_read_tokens", result.token_usage)
        self.assertEqual(200, result.token_usage["cache_read_tokens"])

    def test_cache_creation_input_tokens_mapped_to_cache_creation_tokens(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(cache_creation_input_tokens=10),
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertIsNotNone(result.token_usage)
        self.assertIn("cache_creation_tokens", result.token_usage)
        self.assertEqual(10, result.token_usage["cache_creation_tokens"])

    def test_old_harness_key_names_not_present_in_output(self):
        """The output must use remapped names; original harness key names must not leak."""
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(
                cache_read_input_tokens=100,
                cache_creation_input_tokens=5,
            ),
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertIsNotNone(result.token_usage)
        self.assertNotIn("cache_read_input_tokens", result.token_usage)
        self.assertNotIn("cache_creation_input_tokens", result.token_usage)

    def test_all_four_fields_mapped_when_all_present(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(
                input_tokens=100,
                output_tokens=50,
                cache_read_input_tokens=200,
                cache_creation_input_tokens=10,
            ),
        ])
        result = transcript.read_last_assistant_facts(str(path))
        usage = result.token_usage
        self.assertEqual(100, usage["input_tokens"])
        self.assertEqual(50, usage["output_tokens"])
        self.assertEqual(200, usage["cache_read_tokens"])
        self.assertEqual(10, usage["cache_creation_tokens"])


# ---------------------------------------------------------------------------
# token_usage independence — each sub-field is independently optional
# ---------------------------------------------------------------------------

class TestTokenUsageIndependentSubfields(unittest.TestCase):
    """Each token_usage sub-field is independently optional; only resolved ones appear."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_only_input_tokens_present(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [_assistant_record(input_tokens=100)])
        result = transcript.read_last_assistant_facts(str(path))
        usage = result.token_usage
        self.assertIsNotNone(usage)
        self.assertIn("input_tokens", usage)
        self.assertNotIn("output_tokens", usage)
        self.assertNotIn("cache_read_tokens", usage)
        self.assertNotIn("cache_creation_tokens", usage)

    def test_only_cache_read_tokens_present(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [_assistant_record(cache_read_input_tokens=300)])
        result = transcript.read_last_assistant_facts(str(path))
        usage = result.token_usage
        self.assertIsNotNone(usage)
        self.assertIn("cache_read_tokens", usage)
        self.assertNotIn("input_tokens", usage)

    def test_token_usage_none_when_no_real_sub_field(self):
        """token_usage must be None entirely when no sub-field has a real number."""
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {"type": "assistant", "message": {"model": "claude-opus-4-5", "usage": {}}},
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertIsNone(result.token_usage)

    def test_token_usage_none_when_usage_key_absent(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {"type": "assistant", "message": {"model": "claude-opus-4-5"}},
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertIsNone(result.token_usage)

    def test_sub_field_with_value_zero_is_kept(self):
        """0 is a real number and must not be dropped by the reader."""
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {"type": "assistant", "message": {
                "model": "m",
                "usage": {"input_tokens": 0, "output_tokens": 50},
            }},
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertIsNotNone(result.token_usage)
        self.assertIn("input_tokens", result.token_usage)
        self.assertEqual(0, result.token_usage["input_tokens"])


# ---------------------------------------------------------------------------
# Degradation — missing file, unreadable file, no assistant records
# ---------------------------------------------------------------------------

class TestDegradation(unittest.TestCase):
    """read_last_assistant_facts degrades to (None, None) on every failure path."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _assert_degraded(self, result, label: str) -> None:
        self.assertIsNone(result.model, f"{label}: model should be None")
        self.assertIsNone(result.token_usage, f"{label}: token_usage should be None")

    def test_missing_file_returns_none_model_and_none_usage(self):
        result = transcript.read_last_assistant_facts(
            str(self.tmp_path / "nonexistent.jsonl")
        )
        self._assert_degraded(result, "missing file")

    def test_none_path_returns_none_model_and_none_usage(self):
        result = transcript.read_last_assistant_facts(None)
        self._assert_degraded(result, "None path")

    def test_empty_file_returns_none_model_and_none_usage(self):
        path = self.tmp_path / "empty.jsonl"
        path.write_bytes(b"")
        result = transcript.read_last_assistant_facts(str(path))
        self._assert_degraded(result, "empty file")

    def test_file_with_only_user_records_returns_none_model_and_none_usage(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [_user_record("Hello"), _user_record("Again")])
        result = transcript.read_last_assistant_facts(str(path))
        self._assert_degraded(result, "only user records")

    def test_file_with_only_tool_records_returns_none_model_and_none_usage(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {"type": "tool_result", "content": "ok"},
            {"type": "tool_use", "name": "Bash"},
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self._assert_degraded(result, "only tool records")


# ---------------------------------------------------------------------------
# Degradation — malformed lines are discarded, not fatal
# ---------------------------------------------------------------------------

class TestMalformedLineHandling(unittest.TestCase):
    """Unparseable lines are discarded; remaining valid data is still returned."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_malformed_final_line_does_not_cause_failure(self):
        """A truncated final line (harness mid-write) is discarded, not fatal."""
        path = self.tmp_path / "t.jsonl"
        good_record = json.dumps(
            {"type": "assistant", "message": {"model": "claude-opus-4-5", "usage": {"input_tokens": 10}}}
        )
        # Final line is incomplete (simulates mid-write truncation)
        path.write_bytes((good_record + "\n{truncated").encode("utf-8"))
        result = transcript.read_last_assistant_facts(str(path))
        # Must not raise; the good record should still be returned
        self.assertEqual("claude-opus-4-5", result.model)

    def test_file_with_all_malformed_lines_returns_degraded(self):
        path = self.tmp_path / "t.jsonl"
        path.write_text("not json\n{broken\n", encoding="utf-8")
        result = transcript.read_last_assistant_facts(str(path))
        self.assertIsNone(result.model)
        self.assertIsNone(result.token_usage)

    def test_malformed_line_between_valid_records_is_skipped(self):
        path = self.tmp_path / "t.jsonl"
        first = json.dumps(
            {"type": "assistant", "message": {"model": "first", "usage": {}}}
        )
        last = json.dumps(
            {"type": "assistant", "message": {"model": "last", "usage": {"input_tokens": 5}}}
        )
        path.write_text(
            first + "\nnot json\n" + last + "\n",
            encoding="utf-8",
        )
        result = transcript.read_last_assistant_facts(str(path))
        # Malformed middle line skipped; last record is still found
        self.assertEqual("last", result.model)
        self.assertEqual(5, result.token_usage["input_tokens"])


# ---------------------------------------------------------------------------
# Never-raises contract
# ---------------------------------------------------------------------------

class TestNeverRaises(unittest.TestCase):
    """read_last_assistant_facts must never propagate an exception."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_does_not_raise_on_none_path(self):
        try:
            transcript.read_last_assistant_facts(None)
        except Exception as exc:
            self.fail(f"raised on None path: {exc}")

    def test_does_not_raise_on_missing_file(self):
        try:
            transcript.read_last_assistant_facts(
                str(self.tmp_path / "does_not_exist.jsonl")
            )
        except Exception as exc:
            self.fail(f"raised on missing file: {exc}")

    def test_does_not_raise_on_empty_file(self):
        path = self.tmp_path / "empty.jsonl"
        path.write_bytes(b"")
        try:
            transcript.read_last_assistant_facts(str(path))
        except Exception as exc:
            self.fail(f"raised on empty file: {exc}")

    def test_does_not_raise_on_fully_malformed_file(self):
        path = self.tmp_path / "bad.jsonl"
        path.write_bytes(b"\x00\xff\x01binary\n{bad json")
        try:
            transcript.read_last_assistant_facts(str(path))
        except Exception as exc:
            self.fail(f"raised on malformed file: {exc}")

    def test_does_not_raise_on_json_null_in_usage(self):
        """Null values inside usage fields must be skipped, not raised."""
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {
                "type": "assistant",
                "message": {
                    "model": "m",
                    "usage": {
                        "input_tokens": None,
                        "output_tokens": 50,
                    },
                },
            }
        ])
        try:
            transcript.read_last_assistant_facts(str(path))
        except Exception as exc:
            self.fail(f"raised on null usage sub-field: {exc}")

    def test_does_not_raise_on_non_dict_line(self):
        """A valid JSON but non-object line (e.g., array) should be discarded."""
        path = self.tmp_path / "t.jsonl"
        path.write_text('[1, 2, 3]\n{"type": "assistant", "message": {"model": "m"}}\n',
                        encoding="utf-8")
        try:
            transcript.read_last_assistant_facts(str(path))
        except Exception as exc:
            self.fail(f"raised on non-dict line: {exc}")


# ---------------------------------------------------------------------------
# Last-record semantics
# ---------------------------------------------------------------------------

class TestLastRecordSemantics(unittest.TestCase):
    """The LAST complete assistant record is the one used, not the first."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_last_of_multiple_assistant_records_is_used(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="first", input_tokens=100),
            _user_record(),
            _assistant_record(model="second", input_tokens=200),
            _user_record(),
            _assistant_record(model="third", input_tokens=300),
        ])
        result = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("third", result.model)
        self.assertEqual(300, result.token_usage["input_tokens"])

    def test_non_assistant_records_between_assistant_records_are_ignored(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="assistant-model", input_tokens=10),
            {"type": "tool_use", "name": "Bash"},
            {"type": "tool_result", "content": "ok"},
        ])
        result = transcript.read_last_assistant_facts(str(path))
        # The assistant record is still the last assistant record seen
        self.assertEqual("assistant-model", result.model)


# ---------------------------------------------------------------------------
# read_assistant_records: every assistant record, in file order
#
# ALL tests below are expected to FAIL (RED phase) until
# mosaic_logger_transcript.read_assistant_records and .RecordFacts are
# implemented.
# ---------------------------------------------------------------------------

_FALLBACK_ID_PATTERN = re.compile(r"^pos:\d+:[0-9a-f]{16}$")


class TestReadAssistantRecordsOrdering(unittest.TestCase):
    """read_assistant_records yields facts for every assistant record, not just the last."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_empty_transcript_returns_empty_list(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual([], result)

    def test_single_assistant_record_returns_one_entry(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [_assistant_record(model="only-model")])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual(1, len(result))
        self.assertEqual("only-model", result[0].model)

    def test_every_assistant_record_is_returned_not_just_the_last(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="first", input_tokens=10),
            _assistant_record(model="second", input_tokens=20),
            _assistant_record(model="third", input_tokens=30),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual(3, len(result))

    def test_records_are_returned_in_file_order(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="first"),
            _assistant_record(model="second"),
            _assistant_record(model="third"),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual(["first", "second", "third"], [r.model for r in result])

    def test_non_assistant_records_interspersed_do_not_affect_order(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="first"),
            _user_record(),
            {"type": "tool_use", "name": "Bash"},
            _assistant_record(model="second"),
            {"type": "tool_result", "content": "ok"},
            _assistant_record(model="third"),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual(["first", "second", "third"], [r.model for r in result])

    def test_record_index_is_zero_based_ordinal_among_assistant_records(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _user_record(),
            _assistant_record(model="a"),
            _user_record(),
            _assistant_record(model="b"),
            _assistant_record(model="c"),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual([0, 1, 2], [r.record_index for r in result])


# ---------------------------------------------------------------------------
# Stable per-record identifier
# ---------------------------------------------------------------------------

class TestRecordIdDerivation(unittest.TestCase):
    """record_id: message.id, then record uuid, then a deterministic position hash."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_record_id_is_always_a_non_empty_string(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [_assistant_record(model="m")])
        result = transcript.read_assistant_records(str(path))
        self.assertIsInstance(result[0].record_id, str)
        self.assertNotEqual("", result[0].record_id)

    def test_record_id_uses_message_id_when_present(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="m", message_id="msg_01ABC"),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual("msg_01ABC", result[0].record_id)

    def test_record_id_falls_back_to_uuid_when_message_id_absent(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="m", record_uuid="uuid-1234"),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual("uuid-1234", result[0].record_id)

    def test_message_id_takes_priority_over_uuid(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="m", message_id="msg_priority", record_uuid="uuid-ignored"),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual("msg_priority", result[0].record_id)

    def test_record_id_falls_back_to_uuid_when_message_id_is_empty_string(self):
        # RecordFacts docstring specifies message.id is used only when it is
        # a non-empty string. An empty string at the highest-priority source
        # must not be treated as a resolved (empty) id - it must fall
        # through to the uuid source.
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="m", message_id="", record_uuid="uuid-fallback"),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual("uuid-fallback", result[0].record_id)

    def test_record_id_falls_back_to_hash_when_message_id_and_uuid_are_empty_strings(self):
        # Empty string at both the message.id and uuid sources must fall
        # through all the way to the deterministic position hash, not be
        # treated as a resolved (empty) id at either source.
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="m", message_id="", record_uuid=""),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertRegex(result[0].record_id, _FALLBACK_ID_PATTERN)

    def test_record_id_falls_back_to_position_hash_when_no_id_or_uuid(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [_assistant_record(model="m")])
        result = transcript.read_assistant_records(str(path))
        self.assertRegex(result[0].record_id, _FALLBACK_ID_PATTERN)

    def test_record_ids_are_distinct_between_records(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="a", input_tokens=1),
            _assistant_record(model="b", input_tokens=2),
            _assistant_record(model="c", input_tokens=3),
        ])
        result = transcript.read_assistant_records(str(path))
        ids = [r.record_id for r in result]
        self.assertEqual(len(ids), len(set(ids)))

    def test_record_ids_distinct_for_content_identical_records(self):
        # Two records with byte-identical content (same model, no other
        # fields) must still yield distinct fallback ids. If an implementation
        # dropped record_index and hashed content alone, this would collapse
        # to a single id.
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="m"),
            _assistant_record(model="m"),
        ])
        result = transcript.read_assistant_records(str(path))
        ids = [r.record_id for r in result]
        self.assertEqual(2, len(ids))
        self.assertNotEqual(ids[0], ids[1])

    def test_record_id_stable_across_reads_of_appended_transcript_via_message_id(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="a", message_id="msg_a"),
            _assistant_record(model="b", message_id="msg_b"),
        ])
        first_read = transcript.read_assistant_records(str(path))
        first_ids = [r.record_id for r in first_read]

        with open(path, "a", encoding="utf-8") as f:
            f.write(json.dumps(_assistant_record(model="c", message_id="msg_c")) + "\n")

        second_read = transcript.read_assistant_records(str(path))
        second_ids = [r.record_id for r in second_read]

        self.assertEqual(3, len(second_read))
        self.assertEqual(first_ids, second_ids[:2])

    def test_record_id_stable_across_reads_of_appended_transcript_via_fallback_hash(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="a", input_tokens=1),
            _assistant_record(model="b", input_tokens=2),
        ])
        first_read = transcript.read_assistant_records(str(path))
        first_ids = [r.record_id for r in first_read]

        with open(path, "a", encoding="utf-8") as f:
            f.write(json.dumps(_assistant_record(model="c", input_tokens=3)) + "\n")

        second_read = transcript.read_assistant_records(str(path))
        second_ids = [r.record_id for r in second_read]

        self.assertEqual(3, len(second_read))
        self.assertEqual(first_ids, second_ids[:2])


# ---------------------------------------------------------------------------
# Service tier capture
# ---------------------------------------------------------------------------

class TestServiceTierCapture(unittest.TestCase):
    """service_tier is captured per record where present, None where absent."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_service_tier_none_when_absent_entirely(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [_assistant_record(model="m")])
        result = transcript.read_assistant_records(str(path))
        self.assertIsNone(result[0].service_tier)

    def test_service_tier_from_message_usage_service_tier(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="m", usage_service_tier="standard"),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual("standard", result[0].service_tier)

    def test_service_tier_from_message_service_tier_when_usage_tier_absent(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="m", message_service_tier="priority"),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual("priority", result[0].service_tier)

    def test_service_tier_from_usage_speed_when_others_absent(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="m", usage_speed="fast"),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual("fast", result[0].service_tier)

    def test_usage_service_tier_takes_priority_over_message_service_tier(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(
                model="m",
                usage_service_tier="from-usage",
                message_service_tier="from-message",
            ),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual("from-usage", result[0].service_tier)

    def test_message_service_tier_takes_priority_over_usage_speed(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(
                model="m",
                message_service_tier="from-message",
                usage_speed="from-speed",
            ),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual("from-message", result[0].service_tier)

    def test_empty_string_usage_service_tier_falls_through_to_next_source(self):
        # RecordFacts docstring specifies "first NON-EMPTY string among ...".
        # An empty string at the highest-priority source must not be treated
        # as a resolved (empty) tier - it must fall through to the next
        # source in priority order.
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(
                model="m",
                usage_service_tier="",
                message_service_tier="priority",
            ),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual("priority", result[0].service_tier)

    def test_empty_string_service_tier_at_every_source_yields_none(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="m", usage_service_tier=""),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertIsNone(result[0].service_tier)

    def test_service_tier_captured_independently_per_record(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="a", usage_service_tier="standard"),
            _assistant_record(model="b"),
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual("standard", result[0].service_tier)
        self.assertIsNone(result[1].service_tier)


# ---------------------------------------------------------------------------
# Defensive contract preserved on the new reader
# ---------------------------------------------------------------------------

class TestReadAssistantRecordsDefensiveContract(unittest.TestCase):
    """read_assistant_records preserves every element of the existing defensive contract."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_missing_file_returns_empty_list(self):
        result = transcript.read_assistant_records(str(self.tmp_path / "nonexistent.jsonl"))
        self.assertEqual([], result)

    def test_none_path_returns_empty_list(self):
        result = transcript.read_assistant_records(None)
        self.assertEqual([], result)

    def test_unreadable_path_returns_empty_list(self):
        # A directory cannot be opened as a file; this exercises the I/O failure path.
        result = transcript.read_assistant_records(str(self.tmp_path))
        self.assertEqual([], result)

    def test_malformed_line_between_valid_records_is_skipped(self):
        path = self.tmp_path / "t.jsonl"
        first = json.dumps(_assistant_record(model="first"))
        last = json.dumps(_assistant_record(model="last"))
        path.write_text(first + "\nnot json\n" + last + "\n", encoding="utf-8")
        result = transcript.read_assistant_records(str(path))
        self.assertEqual(["first", "last"], [r.model for r in result])

    def test_non_object_lines_are_discarded(self):
        path = self.tmp_path / "t.jsonl"
        path.write_text(
            '[1, 2, 3]\n' + json.dumps(_assistant_record(model="m")) + "\n",
            encoding="utf-8",
        )
        result = transcript.read_assistant_records(str(path))
        self.assertEqual(1, len(result))
        self.assertEqual("m", result[0].model)

    def test_non_assistant_records_are_excluded(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _user_record(),
            {"type": "tool_use", "name": "Bash"},
            {"type": "tool_result", "content": "ok"},
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertEqual([], result)

    def test_missing_model_yields_none_model(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {"type": "assistant", "message": {"usage": {"input_tokens": 5}}},
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertIsNone(result[0].model)

    def test_missing_usage_yields_none_token_usage(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [_assistant_record(model="m")])
        result = transcript.read_assistant_records(str(path))
        self.assertIsNone(result[0].token_usage)

    def test_present_zero_value_is_kept_not_dropped(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [_assistant_record(model="m", input_tokens=0)])
        result = transcript.read_assistant_records(str(path))
        self.assertIsNotNone(result[0].token_usage)
        self.assertEqual(0, result[0].token_usage["input_tokens"])

    def test_none_value_in_usage_subfield_is_excluded(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {
                "type": "assistant",
                "message": {"model": "m", "usage": {"input_tokens": None, "output_tokens": 50}},
            },
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertNotIn("input_tokens", result[0].token_usage)
        self.assertEqual(50, result[0].token_usage["output_tokens"])

    def test_bool_value_in_usage_subfield_is_excluded(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {
                "type": "assistant",
                "message": {"model": "m", "usage": {"input_tokens": True, "output_tokens": 50}},
            },
        ])
        result = transcript.read_assistant_records(str(path))
        self.assertNotIn("input_tokens", result[0].token_usage)

    def test_cache_creation_tokens_mapping_is_unchanged_and_flattened(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="m", cache_creation_input_tokens=10),
        ])
        result = transcript.read_assistant_records(str(path))
        usage = result[0].token_usage
        self.assertIn("cache_creation_tokens", usage)
        self.assertEqual(10, usage["cache_creation_tokens"])
        # No split-tier fields must be introduced.
        self.assertNotIn("ephemeral_1h_input_tokens", usage)
        self.assertNotIn("ephemeral_5m_input_tokens", usage)

    def test_nested_ephemeral_cache_tier_object_is_ignored(self):
        # A record carrying a nested cache_creation object with ephemeral-tier
        # sub-fields alongside the flattened value must still yield only the
        # four mapped keys - no tier fields are ever introduced (D6/AC4.6).
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            {
                "type": "assistant",
                "message": {
                    "model": "m",
                    "usage": {
                        "input_tokens": 5,
                        "cache_creation_input_tokens": 10,
                        "cache_creation": {
                            "ephemeral_1h_input_tokens": 7,
                            "ephemeral_5m_input_tokens": 3,
                        },
                    },
                },
            },
        ])
        result = transcript.read_assistant_records(str(path))
        usage = result[0].token_usage
        self.assertEqual(
            {"input_tokens", "cache_creation_tokens"},
            set(usage.keys()),
        )
        self.assertEqual(10, usage["cache_creation_tokens"])


# ---------------------------------------------------------------------------
# Never-raises contract on the new reader
# ---------------------------------------------------------------------------

class TestReadAssistantRecordsNeverRaises(unittest.TestCase):
    """read_assistant_records must never propagate an exception."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_does_not_raise_on_none_path(self):
        try:
            transcript.read_assistant_records(None)
        except Exception as exc:
            self.fail(f"raised on None path: {exc}")

    def test_does_not_raise_on_missing_file(self):
        try:
            transcript.read_assistant_records(str(self.tmp_path / "does_not_exist.jsonl"))
        except Exception as exc:
            self.fail(f"raised on missing file: {exc}")

    def test_does_not_raise_on_binary_garbage(self):
        path = self.tmp_path / "bad.jsonl"
        path.write_bytes(b"\x00\xff\x01binary\n{bad json")
        try:
            transcript.read_assistant_records(str(path))
        except Exception as exc:
            self.fail(f"raised on malformed file: {exc}")

    def test_does_not_raise_on_directory_path(self):
        try:
            transcript.read_assistant_records(str(self.tmp_path))
        except Exception as exc:
            self.fail(f"raised on directory path: {exc}")

    def test_does_not_raise_on_truncated_final_line(self):
        path = self.tmp_path / "t.jsonl"
        good_record = json.dumps(_assistant_record(model="m", input_tokens=10))
        path.write_bytes((good_record + "\n{truncated").encode("utf-8"))
        try:
            result = transcript.read_assistant_records(str(path))
        except Exception as exc:
            self.fail(f"raised on truncated final line: {exc}")
        self.assertEqual(["m"], [r.model for r in result])


# ---------------------------------------------------------------------------
# Existing last-record consumers are unaffected by the new reader
# ---------------------------------------------------------------------------

class TestExistingLastRecordConsumerUnaffected(unittest.TestCase):
    """read_last_assistant_facts keeps returning last-record semantics unchanged."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_last_record_facts_unaffected_by_multi_record_transcript(self):
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [
            _assistant_record(model="first", input_tokens=100),
            _assistant_record(model="second", input_tokens=200),
            _assistant_record(model="third", input_tokens=300),
        ])
        last_facts = transcript.read_last_assistant_facts(str(path))
        self.assertEqual("third", last_facts.model)
        self.assertEqual(300, last_facts.token_usage["input_tokens"])

    def test_last_record_facts_has_no_record_id_or_service_tier_attributes_added(self):
        """TurnFacts' shape must stay exactly as it is today for existing consumers."""
        path = self.tmp_path / "t.jsonl"
        _write_transcript(path, [_assistant_record(model="m", input_tokens=1)])
        last_facts = transcript.read_last_assistant_facts(str(path))
        self.assertFalse(hasattr(last_facts, "record_id"))
        self.assertFalse(hasattr(last_facts, "service_tier"))


if __name__ == "__main__":
    unittest.main()
