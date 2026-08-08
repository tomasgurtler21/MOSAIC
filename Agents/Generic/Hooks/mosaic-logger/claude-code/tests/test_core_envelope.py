"""Tests for build_event in mosaic_logger_core.

Covers: required envelope fields, optional context fields, the degrade-never-fabricate
rule (None/empty-string/empty-collection produce absent keys; False and 0 are kept),
and recursive pruning of nested dicts whose resolved sub-fields all vanish.
"""

import json
import os
import sys
import types
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core


def _ctx(session_id="sess1", run_id="20260101T000000Z-ab12"):
    """Minimal context object for envelope tests."""
    return types.SimpleNamespace(
        session_id=session_id,
        run_id=run_id,
        timestamp="2026-01-01T00:00:00.000Z",
    )


class TestEnvelopeRequiredFields(unittest.TestCase):
    """The four required envelope fields must be present on every emitted event."""

    def setUp(self):
        self.ctx = _ctx()

    def test_schema_version_is_1_1_0(self):
        # Bumped from "1.0.0" to "1.1.0" (D-B3): the new usage_record wire
        # shape is a schema-level change, so every emitted event carries the
        # bumped version, not only usage_record itself.
        result = core.build_event("session_start", self.ctx)
        self.assertEqual("1.1.0", result["schema_version"])

    def test_event_field_matches_argument(self):
        result = core.build_event("run_start", self.ctx)
        self.assertEqual("run_start", result["event"])

    def test_timestamp_comes_from_context(self):
        result = core.build_event("session_start", self.ctx)
        self.assertEqual("2026-01-01T00:00:00.000Z", result["timestamp"])

    def test_harness_is_claude_code(self):
        result = core.build_event("session_start", self.ctx)
        self.assertEqual("claude-code", result["harness"])

    def test_all_four_required_fields_present(self):
        result = core.build_event("session_start", self.ctx)
        for field in ("schema_version", "event", "timestamp", "harness"):
            with self.subTest(field=field):
                self.assertIn(field, result)

    def test_result_is_a_plain_dict(self):
        result = core.build_event("session_start", self.ctx)
        self.assertIsInstance(result, dict)


class TestEnvelopeOptionalContextFields(unittest.TestCase):
    """session_id and run_id are optional: present when resolvable, absent otherwise."""

    def test_session_id_present_when_resolvable(self):
        ctx = _ctx(session_id="abc123")
        result = core.build_event("session_start", ctx)
        self.assertEqual("abc123", result["session_id"])

    def test_run_id_present_when_resolvable(self):
        ctx = _ctx(run_id="20260101T170000Z-ff00")
        result = core.build_event("session_start", ctx)
        self.assertEqual("20260101T170000Z-ff00", result["run_id"])

    def test_session_id_absent_when_context_has_none(self):
        ctx = _ctx(session_id=None)
        result = core.build_event("session_start", ctx)
        self.assertNotIn("session_id", result)

    def test_run_id_absent_when_context_has_none(self):
        ctx = _ctx(run_id=None)
        result = core.build_event("session_start", ctx)
        self.assertNotIn("run_id", result)

    def test_session_id_absent_means_key_does_not_exist(self):
        # Verify truly absent, not present with a None value
        ctx = _ctx(session_id=None)
        result = core.build_event("session_start", ctx)
        self.assertFalse("session_id" in result)


class TestFieldOmissionRule(unittest.TestCase):
    """Degrade-never-fabricate: None/empty-string/empty-collection → key absent.
    False and 0 are genuine resolved values and must be kept."""

    def setUp(self):
        self.ctx = _ctx()

    def test_none_kwarg_produces_absent_key(self):
        result = core.build_event("session_start", self.ctx, cwd=None)
        self.assertNotIn("cwd", result)

    def test_none_kwarg_key_is_truly_absent_not_null(self):
        result = core.build_event("run_start", self.ctx, model=None)
        self.assertFalse("model" in result)

    def test_empty_string_kwarg_produces_absent_key(self):
        result = core.build_event("session_start", self.ctx, cwd="")
        self.assertNotIn("cwd", result)

    def test_empty_dict_kwarg_produces_absent_key(self):
        result = core.build_event("turn", self.ctx, token_usage={})
        self.assertNotIn("token_usage", result)

    def test_empty_list_kwarg_produces_absent_key(self):
        result = core.build_event("session_start", self.ctx, items=[])
        self.assertNotIn("items", result)

    def test_false_kwarg_key_is_present(self):
        result = core.build_event("session_start", self.ctx, resumed=False)
        self.assertIn("resumed", result)
        self.assertIs(False, result["resumed"])

    def test_zero_kwarg_key_is_present(self):
        result = core.build_event("compaction", self.ctx, tokens_after=0)
        self.assertIn("tokens_after", result)
        self.assertEqual(0, result["tokens_after"])

    def test_truthy_string_kwarg_is_kept(self):
        result = core.build_event("session_start", self.ctx, model="claude-opus-4-5")
        self.assertEqual("claude-opus-4-5", result["model"])

    def test_positive_integer_kwarg_is_kept(self):
        result = core.build_event("compaction", self.ctx, tokens_before=5000)
        self.assertEqual(5000, result["tokens_before"])

    def test_true_kwarg_is_kept(self):
        result = core.build_event("session_start", self.ctx, resumed=True)
        self.assertIn("resumed", result)
        self.assertIs(True, result["resumed"])


class TestNestedDictPruning(unittest.TestCase):
    """Nested dicts are pruned recursively; a dict empty after pruning is itself dropped."""

    def setUp(self):
        self.ctx = _ctx()

    def test_all_none_sub_fields_drops_entire_key(self):
        result = core.build_event("turn", self.ctx,
                                  token_usage={"input_tokens": None, "output_tokens": None})
        self.assertNotIn("token_usage", result)

    def test_mixed_sub_fields_keeps_key_and_only_resolved_sub_fields(self):
        result = core.build_event("turn", self.ctx,
                                  token_usage={"input_tokens": 100, "output_tokens": None})
        self.assertIn("token_usage", result)
        self.assertIn("input_tokens", result["token_usage"])
        self.assertNotIn("output_tokens", result["token_usage"])

    def test_only_resolved_sub_fields_survive_pruning(self):
        result = core.build_event("turn", self.ctx,
                                  token_usage={
                                      "input_tokens": 100,
                                      "output_tokens": None,
                                      "cache_read_tokens": 50,
                                      "cache_creation_tokens": None,
                                  })
        usage = result["token_usage"]
        self.assertIn("input_tokens", usage)
        self.assertIn("cache_read_tokens", usage)
        self.assertNotIn("output_tokens", usage)
        self.assertNotIn("cache_creation_tokens", usage)

    def test_empty_string_sub_field_drops_entire_key(self):
        result = core.build_event("run_start", self.ctx,
                                  token_usage={"input_tokens": ""})
        self.assertNotIn("token_usage", result)

    def test_zero_sub_field_is_kept(self):
        result = core.build_event("compaction", self.ctx,
                                  token_usage={"input_tokens": 0, "output_tokens": 200})
        self.assertIn("token_usage", result)
        self.assertEqual(0, result["token_usage"]["input_tokens"])


class TestBuildEventJsonSerializable(unittest.TestCase):
    """build_event must produce output that is directly serializable to JSONL."""

    def test_output_is_json_serializable(self):
        ctx = _ctx()
        result = core.build_event("session_start", ctx, model="claude-opus-4-5")
        serialized = json.dumps(result, ensure_ascii=False)
        self.assertIsInstance(serialized, str)

    def test_serialized_output_round_trips(self):
        ctx = _ctx()
        result = core.build_event("session_start", ctx,
                                  model="claude-opus-4-5", resumed=False)
        parsed = json.loads(json.dumps(result, ensure_ascii=False))
        self.assertEqual(result, parsed)

    def test_serialized_form_has_no_null_values(self):
        # The serializer must never let a None reach the wire
        ctx = _ctx(session_id=None, run_id=None)
        result = core.build_event("session_start", ctx, cwd=None, model=None)
        serialized = json.dumps(result)
        self.assertNotIn("null", serialized)
