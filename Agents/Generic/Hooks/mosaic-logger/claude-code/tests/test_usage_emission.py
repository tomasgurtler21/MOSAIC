"""Tests for mosaic_logger_usage: raw usage_record emission and capture gating.

Covers:
  T5.1 - a transcript with N assistant records produces N raw usage events,
         each carrying record identifier, model, token usage, service tier
  T5.2 - each raw usage event lands on exactly one stream (orchestrator vs
         invocation), driven solely by the agent_instance_id argument
  T5.3 - tool_capture_enabled() gates the high-frequency tool path per the
         MOSAIC_LOGGER_USAGE_CAPTURE environment variable
  T5.4 - re-emission of an already-emitted record on a later firing is
         permitted and does not corrupt or truncate the sink
  T5.5 - the bumped schema version is stamped on emitted events; genuinely-
         zero usage fields survive pruning, genuinely-absent ones are dropped
  T5.7 - emission failures degrade silently with a diagnostic and never raise

ALL tests in this file are expected to FAIL (RED phase) until
mosaic_logger_usage.py is implemented.
"""

import json
import os
import pathlib
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_usage as usage


# ---------------------------------------------------------------------------
# Shared constants and helpers
# ---------------------------------------------------------------------------

_RUN_ID = "20260101T170000Z-c9a1"
_SESSION_ID = "test-session-usage"
_TS = "2026-01-01T17:00:00.000Z"


def _make_ctx(tmp_path, run_id=_RUN_ID, session_id=_SESSION_ID):
    """Build a HookContext with run_id pre-set, rooted at tmp_path."""
    payload = {"hook_event_name": "Stop", "session_id": session_id}
    workspace_root = pathlib.Path(tmp_path)
    paths = core.build_paths(workspace_root)
    ctx = core.HookContext(payload, workspace_root, paths, _TS)
    ctx.run_id = run_id
    return ctx


def _assistant_record(model="claude-opus-4-5", input_tokens=None, output_tokens=None,
                       cache_read_input_tokens=None, cache_creation_input_tokens=None,
                       message_id=None, record_uuid=None, service_tier=None):
    """Build a minimal transcript assistant record.

    Numeric usage sub-fields default to omitted; pass an explicit value
    (including 0) to include it, matching the transcript reader's contract
    that only genuine numbers are mapped.
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
    if service_tier is not None:
        usage["service_tier"] = service_tier

    msg = {}
    if model is not None:
        msg["model"] = model
    if message_id is not None:
        msg["id"] = message_id
    if usage:
        msg["usage"] = usage

    record = {"type": "assistant", "message": msg}
    if record_uuid is not None:
        record["uuid"] = record_uuid
    return record


def _write_transcript(path, records):
    """Write a list of dicts as JSONL lines to path, creating parents as needed."""
    path = pathlib.Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        "\n".join(json.dumps(r, ensure_ascii=False) for r in records) + "\n",
        encoding="utf-8",
    )


def _read_jsonl(path):
    """Read all non-empty lines of a JSONL file as parsed dicts. [] if absent."""
    path = pathlib.Path(path)
    if not path.exists():
        return []
    return [json.loads(ln) for ln in path.read_text(encoding="utf-8").splitlines()
            if ln.strip()]


# ---------------------------------------------------------------------------
# T5.3 - tool_capture_enabled() gating
# ---------------------------------------------------------------------------

class TestToolCaptureEnabled(unittest.TestCase):
    """tool_capture_enabled() reflects MOSAIC_LOGGER_USAGE_CAPTURE, defaulting
    to True (capture from every firing) per the design's documented values."""

    def setUp(self):
        self._orig = os.environ.get(usage.CAPTURE_MODE_ENV)

    def tearDown(self):
        if self._orig is None:
            os.environ.pop(usage.CAPTURE_MODE_ENV, None)
        else:
            os.environ[usage.CAPTURE_MODE_ENV] = self._orig

    def test_default_when_unset_is_true(self):
        os.environ.pop(usage.CAPTURE_MODE_ENV, None)
        self.assertTrue(usage.tool_capture_enabled())

    def test_explicit_all_is_true(self):
        os.environ[usage.CAPTURE_MODE_ENV] = "all"
        self.assertTrue(usage.tool_capture_enabled())

    def test_boundaries_is_false(self):
        os.environ[usage.CAPTURE_MODE_ENV] = "boundaries"
        self.assertFalse(usage.tool_capture_enabled())

    def test_unrecognised_value_is_true(self):
        """An unrecognised value is treated as 'all' per the documented default."""
        os.environ[usage.CAPTURE_MODE_ENV] = "bogus-value"
        self.assertTrue(usage.tool_capture_enabled())

    def test_never_raises_with_empty_string(self):
        os.environ[usage.CAPTURE_MODE_ENV] = ""
        try:
            usage.tool_capture_enabled()
        except Exception as exc:
            self.fail(f"tool_capture_enabled raised: {exc}")


# ---------------------------------------------------------------------------
# T5.1 / AC5.2 - N records produce N events with the documented fields
# ---------------------------------------------------------------------------

class TestEmitUsageRecordsCount(unittest.TestCase):
    """A transcript with N assistant records produces N usage_record events."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.ctx = _make_ctx(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def test_three_records_produce_three_events(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(input_tokens=10, output_tokens=5, message_id="msg_1"),
            _assistant_record(input_tokens=20, output_tokens=8, message_id="msg_2"),
            _assistant_record(input_tokens=30, output_tokens=12, message_id="msg_3"),
        ])
        count = usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        self.assertEqual(3, count)
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == usage.USAGE_EVENT]
        self.assertEqual(3, len(usage_events))

    def test_returned_count_matches_appended_line_count(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(message_id="msg_a"),
            _assistant_record(message_id="msg_b"),
        ])
        count = usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        self.assertEqual(count, len(events))

    def test_no_assistant_records_returns_zero(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "user", "message": {"role": "user", "content": "hi"}},
        ])
        count = usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        self.assertEqual(0, count)

    def test_falsy_transcript_path_returns_zero(self):
        count = usage.emit_usage_records(
            self.ctx, None, None, "orchestrator_transcript"
        )
        self.assertEqual(0, count)

    def test_nonexistent_transcript_path_returns_zero(self):
        count = usage.emit_usage_records(
            self.ctx, str(self.tmp_path / "does-not-exist.jsonl"), None,
            "orchestrator_transcript",
        )
        self.assertEqual(0, count)

    def test_each_event_carries_a_non_empty_record_id(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(message_id="msg_x"),
        ])
        usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_event = next(e for e in events if e["event"] == usage.USAGE_EVENT)
        self.assertEqual("msg_x", usage_event["record_id"])

    def test_each_event_carries_its_model(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(model="claude-haiku-3", message_id="msg_y"),
        ])
        usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_event = next(e for e in events if e["event"] == usage.USAGE_EVENT)
        self.assertEqual("claude-haiku-3", usage_event["model"])

    def test_each_event_carries_its_token_usage(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(input_tokens=42, output_tokens=17, message_id="msg_z"),
        ])
        usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_event = next(e for e in events if e["event"] == usage.USAGE_EVENT)
        self.assertEqual(42, usage_event["token_usage"]["input_tokens"])
        self.assertEqual(17, usage_event["token_usage"]["output_tokens"])

    def test_each_event_carries_its_service_tier(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(service_tier="standard", message_id="msg_tier"),
        ])
        usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_event = next(e for e in events if e["event"] == usage.USAGE_EVENT)
        self.assertEqual("standard", usage_event["service_tier"])

    def test_record_index_zero_survives_for_first_record(self):
        """record_index is 0 for the first record and must survive pruning (B1)."""
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(message_id="msg_first"),
        ])
        usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_event = next(e for e in events if e["event"] == usage.USAGE_EVENT)
        self.assertIn("record_index", usage_event)
        self.assertEqual(0, usage_event["record_index"])

    def test_distinct_records_get_distinct_record_indexes_in_file_order(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(message_id="msg_1"),
            _assistant_record(message_id="msg_2"),
        ])
        usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == usage.USAGE_EVENT]
        indexes = sorted(e["record_index"] for e in usage_events)
        self.assertEqual([0, 1], indexes)


# ---------------------------------------------------------------------------
# T5.2 / AC5.3 - single-stream routing
# ---------------------------------------------------------------------------

class TestEmitUsageRecordsRouting(unittest.TestCase):
    """agent_instance_id alone determines the sink; a record never reaches
    both the orchestrator and an invocation stream."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.ctx = _make_ctx(self.tmp_path)
        self.transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(self.transcript_path, [
            _assistant_record(message_id="msg_route"),
        ])

    def tearDown(self):
        self.tmp.cleanup()

    def test_none_agent_instance_id_routes_to_orchestrator_stream(self):
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        self.assertEqual(1, len([e for e in events if e["event"] == usage.USAGE_EVENT]))

    def test_named_agent_instance_id_routes_to_invocation_stream(self):
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), "Research#1", "agent_transcript"
        )
        events = _read_jsonl(self.ctx.paths.invocation_events(_RUN_ID, "Research#1"))
        self.assertEqual(1, len([e for e in events if e["event"] == usage.USAGE_EVENT]))

    def test_invocation_routed_record_does_not_reach_orchestrator_stream(self):
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), "Research#1", "agent_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        self.assertEqual(0, len([e for e in events if e["event"] == usage.USAGE_EVENT]))

    def test_orchestrator_routed_record_does_not_reach_invocation_stream(self):
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.invocation_events(_RUN_ID, "Research#1"))
        self.assertEqual(0, len([e for e in events if e["event"] == usage.USAGE_EVENT]))

    def test_orchestrator_stream_event_omits_agent_instance_id(self):
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_event = next(e for e in events if e["event"] == usage.USAGE_EVENT)
        self.assertNotIn("agent_instance_id", usage_event)

    def test_invocation_stream_event_carries_agent_instance_id(self):
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), "Research#1", "agent_transcript"
        )
        events = _read_jsonl(self.ctx.paths.invocation_events(_RUN_ID, "Research#1"))
        usage_event = next(e for e in events if e["event"] == usage.USAGE_EVENT)
        self.assertEqual("Research#1", usage_event["agent_instance_id"])

    def test_source_field_reflects_caller_supplied_value(self):
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), "Research#1", "agent_transcript"
        )
        events = _read_jsonl(self.ctx.paths.invocation_events(_RUN_ID, "Research#1"))
        usage_event = next(e for e in events if e["event"] == usage.USAGE_EVENT)
        self.assertEqual("agent_transcript", usage_event["source"])


# ---------------------------------------------------------------------------
# T5.4 / AC5.5 - repeated emission across firings is tolerated
# ---------------------------------------------------------------------------

class TestEmitUsageRecordsReemission(unittest.TestCase):
    """Calling emit_usage_records again on a transcript that already produced
    events is permitted: no exception, no truncation, no corruption."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.ctx = _make_ctx(self.tmp_path)
        self.transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(self.transcript_path, [
            _assistant_record(message_id="msg_dup"),
        ])

    def tearDown(self):
        self.tmp.cleanup()

    def test_two_firings_append_rather_than_replace(self):
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == usage.USAGE_EVENT]
        self.assertEqual(2, len(usage_events))

    def test_repeated_record_id_is_identical_across_firings(self):
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == usage.USAGE_EVENT]
        self.assertEqual(usage_events[0]["record_id"], usage_events[1]["record_id"])

    def test_file_remains_valid_jsonl_after_repeated_firings(self):
        for _ in range(3):
            usage.emit_usage_records(
                self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
            )
        path = self.ctx.paths.orchestrator_events(_RUN_ID)
        lines = path.read_text(encoding="utf-8").splitlines()
        for line in lines:
            if line.strip():
                json.loads(line)  # raises ValueError if corrupted

    def test_earlier_lines_are_not_overwritten_by_a_later_firing(self):
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        events_after_first = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        events_after_second = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        self.assertEqual(events_after_first[0], events_after_second[0])


# ---------------------------------------------------------------------------
# T5.5 / AC5.6 - bumped schema version and zero-vs-absent pruning
# ---------------------------------------------------------------------------

class TestEmitUsageRecordsSchemaAndPruning(unittest.TestCase):
    """Emitted usage_record events stamp the bumped schema version and keep
    the degrade-never-fabricate pruning rule for the token_usage block."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.ctx = _make_ctx(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emitted_event_schema_version_is_bumped(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [_assistant_record(message_id="msg_v")])
        usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_event = next(e for e in events if e["event"] == usage.USAGE_EVENT)
        self.assertEqual("1.1.0", usage_event["schema_version"])

    def test_genuinely_zero_usage_field_survives(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(input_tokens=0, output_tokens=5, message_id="msg_zero"),
        ])
        usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_event = next(e for e in events if e["event"] == usage.USAGE_EVENT)
        self.assertIn("input_tokens", usage_event["token_usage"])
        self.assertEqual(0, usage_event["token_usage"]["input_tokens"])

    def test_genuinely_absent_usage_field_is_dropped(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(output_tokens=5, message_id="msg_absent"),
        ])
        usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_event = next(e for e in events if e["event"] == usage.USAGE_EVENT)
        self.assertNotIn("input_tokens", usage_event["token_usage"])

    def test_no_resolved_usage_sub_field_drops_the_entire_token_usage_key(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(message_id="msg_no_usage"),
        ])
        usage.emit_usage_records(
            self.ctx, str(transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_event = next(e for e in events if e["event"] == usage.USAGE_EVENT)
        self.assertNotIn("token_usage", usage_event)


# ---------------------------------------------------------------------------
# T5.7 - failure degradation
# ---------------------------------------------------------------------------

class TestEmitUsageRecordsFailureDegradation(unittest.TestCase):
    """emit_usage_records never raises; unexpected failures degrade silently
    with a diagnostic and never suppress the count-so-far or the caller."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.ctx = _make_ctx(self.tmp_path)
        self._orig_debug = os.environ.get("MOSAIC_LOGGER_DEBUG")

    def tearDown(self):
        self.tmp.cleanup()
        if self._orig_debug is None:
            os.environ.pop("MOSAIC_LOGGER_DEBUG", None)
        else:
            os.environ["MOSAIC_LOGGER_DEBUG"] = self._orig_debug

    def test_never_raises_on_reader_failure(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [_assistant_record(message_id="msg_fail")])
        with mock.patch(
            "mosaic_logger_transcript.read_assistant_records",
            side_effect=RuntimeError("boom"),
        ):
            try:
                count = usage.emit_usage_records(
                    self.ctx, str(transcript_path), None, "orchestrator_transcript"
                )
            except Exception as exc:
                self.fail(f"emit_usage_records raised: {exc}")
        self.assertEqual(0, count)

    def test_reader_failure_emits_usage_capture_failed_diagnostic(self):
        debug_file = self.tmp_path / "debug.log"
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [_assistant_record(message_id="msg_fail2")])
        with mock.patch(
            "mosaic_logger_transcript.read_assistant_records",
            side_effect=RuntimeError("boom"),
        ):
            usage.emit_usage_records(
                self.ctx, str(transcript_path), None, "orchestrator_transcript"
            )
        self.assertTrue(debug_file.exists())
        text = debug_file.read_text(encoding="utf-8")
        self.assertIn("usage-capture: failed", text)

    def test_never_raises_with_non_string_transcript_path(self):
        try:
            count = usage.emit_usage_records(
                self.ctx, 12345, None, "orchestrator_transcript"
            )
        except Exception as exc:
            self.fail(f"emit_usage_records raised: {exc}")
        self.assertEqual(0, count)


if __name__ == "__main__":
    unittest.main()
