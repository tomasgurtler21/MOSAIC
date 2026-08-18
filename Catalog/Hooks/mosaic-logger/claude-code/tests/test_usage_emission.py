"""Tests for mosaic_logger_usage: raw usage_record emission and capture gating.

Covers:
  - A transcript with N assistant records produces N raw usage events, each
    carrying record identifier, model, token usage, service tier
  - Each raw usage event lands on exactly one stream (orchestrator vs
    invocation), driven solely by the agent_instance_id argument
  - tool_capture_enabled() gates the high-frequency tool path per the
    MOSAIC_LOGGER_USAGE_CAPTURE environment variable
  - emit-on-change behaviour: a first firing emits all records; a subsequent
    firing over an unchanged transcript emits nothing; a record whose values
    changed is re-emitted; a genuinely new record is emitted while unchanged
    earlier records are not
  - The bumped schema version is stamped on emitted events; genuinely-zero
    usage fields survive pruning, genuinely-absent ones are dropped
  - Lines written to a stream grow linearly with distinct records, not
    quadratically with the number of firings
  - Per-stream state isolation: orchestrator-stream state never suppresses
    emission to an invocation stream or a quarantine destination
  - record_fingerprint covers only model and token_usage; record_id,
    record_index and service_tier do not affect the fingerprint value
  - load_usage_state / save_usage_state: round-trip, degrade-to-empty for
    absent / corrupt / unknown-version files, best-effort save semantics
  - Every entry point — emit_usage_records, record_fingerprint,
    load_usage_state, save_usage_state — never raises
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
import mosaic_logger_transcript as transcript
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
# Emit-on-change: unchanged transcripts suppress re-emission
# ---------------------------------------------------------------------------

class TestEmitUsageRecordsReemission(unittest.TestCase):
    """emit_usage_records emits a record only when it is new or its
    analyzer-significant values changed since the last emission to that stream.
    An immediately repeated firing over an unchanged transcript writes nothing."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.ctx = _make_ctx(self.tmp_path)
        self.transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=10, output_tokens=5, message_id="msg_dup"),
        ])

    def tearDown(self):
        self.tmp.cleanup()

    def test_second_identical_firing_writes_no_additional_events(self):
        """A second firing over an unchanged transcript appends nothing to the
        stream — the first firing's state prevents redundant writes."""
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == usage.USAGE_EVENT]
        self.assertEqual(1, len(usage_events))

    def test_second_firing_with_changed_token_usage_reemits_same_record_id(self):
        """When a record's token_usage changes between firings the second
        firing re-emits the same record_id with the updated values."""
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        # Update transcript so msg_dup has different token_usage
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=10, output_tokens=99, message_id="msg_dup"),
        ])
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == usage.USAGE_EVENT]
        self.assertEqual(2, len(usage_events))
        self.assertEqual(usage_events[0]["record_id"], usage_events[1]["record_id"])

    def test_file_remains_valid_jsonl_after_repeated_identical_firings(self):
        """Three identical firings still leave a well-formed JSONL file with
        exactly one usage record (only the first firing wrote anything)."""
        for _ in range(3):
            usage.emit_usage_records(
                self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
            )
        path = self.ctx.paths.orchestrator_events(_RUN_ID)
        lines = [ln for ln in path.read_text(encoding="utf-8").splitlines() if ln.strip()]
        for line in lines:
            json.loads(line)  # raises ValueError if corrupted
        usage_events = [json.loads(ln) for ln in lines if json.loads(ln)["event"] == usage.USAGE_EVENT]
        self.assertEqual(1, len(usage_events))

    def test_event_written_on_first_firing_is_not_overwritten_by_later_firings(self):
        """The event written during the first firing is unchanged after
        a subsequent identical firing."""
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


# ---------------------------------------------------------------------------
# record_fingerprint: content fingerprinting for change detection
# ---------------------------------------------------------------------------

class TestRecordFingerprint(unittest.TestCase):
    """record_fingerprint computes a stable hash over model and token_usage only.
    Changes to record_id, record_index and service_tier do not affect it.
    Returns the empty string for any unfingerprintable input."""

    def _rec(self, record_id="msg_x", record_index=0,
             model="claude-opus-4-5", token_usage=None, service_tier=None):
        if token_usage is None:
            token_usage = {"input_tokens": 10, "output_tokens": 5}
        return transcript.RecordFacts(record_id, record_index, model, token_usage, service_tier)

    def test_equal_model_and_token_usage_produce_equal_fingerprint(self):
        rec1 = self._rec()
        rec2 = self._rec()
        self.assertEqual(usage.record_fingerprint(rec1), usage.record_fingerprint(rec2))

    def test_changed_output_tokens_produces_different_fingerprint(self):
        rec1 = self._rec(token_usage={"input_tokens": 10, "output_tokens": 5})
        rec2 = self._rec(token_usage={"input_tokens": 10, "output_tokens": 99})
        self.assertNotEqual(usage.record_fingerprint(rec1), usage.record_fingerprint(rec2))

    def test_changed_input_tokens_produces_different_fingerprint(self):
        rec1 = self._rec(token_usage={"input_tokens": 10, "output_tokens": 5})
        rec2 = self._rec(token_usage={"input_tokens": 999, "output_tokens": 5})
        self.assertNotEqual(usage.record_fingerprint(rec1), usage.record_fingerprint(rec2))

    def test_changed_model_produces_different_fingerprint(self):
        rec1 = self._rec(model="claude-opus-4-5")
        rec2 = self._rec(model="claude-haiku-3")
        self.assertNotEqual(usage.record_fingerprint(rec1), usage.record_fingerprint(rec2))

    def test_changed_record_id_does_not_change_fingerprint(self):
        """record_id is the state key itself; fingerprinting it would be circular."""
        rec1 = self._rec(record_id="msg_aaa")
        rec2 = self._rec(record_id="msg_bbb")
        self.assertEqual(usage.record_fingerprint(rec1), usage.record_fingerprint(rec2))

    def test_changed_record_index_does_not_change_fingerprint(self):
        """record_index is diagnostic only and shifts wholesale on compaction."""
        rec1 = self._rec(record_index=0)
        rec2 = self._rec(record_index=7)
        self.assertEqual(usage.record_fingerprint(rec1), usage.record_fingerprint(rec2))

    def test_changed_service_tier_does_not_change_fingerprint(self):
        """service_tier is descriptive only and drives no pricing decision."""
        rec1 = self._rec(service_tier="standard")
        rec2 = self._rec(service_tier="batch")
        self.assertEqual(usage.record_fingerprint(rec1), usage.record_fingerprint(rec2))

    def test_returns_non_empty_string_for_typical_record(self):
        rec = self._rec()
        result = usage.record_fingerprint(rec)
        self.assertIsInstance(result, str)
        self.assertGreater(len(result), 0)

    def test_result_is_lowercase_hex(self):
        """Fixed-length lowercase hex is required by the wire contract."""
        rec = self._rec()
        result = usage.record_fingerprint(rec)
        self.assertRegex(result, r'^[0-9a-f]+$')

    def test_is_deterministic_across_calls(self):
        rec = self._rec()
        self.assertEqual(usage.record_fingerprint(rec), usage.record_fingerprint(rec))

    def test_token_usage_dict_key_order_does_not_affect_fingerprint(self):
        """Fingerprint must be order-independent so two observations of the
        same usage with different insertion order compare equal."""
        rec1 = self._rec(token_usage={"input_tokens": 10, "output_tokens": 5})
        rec2 = self._rec(token_usage={"output_tokens": 5, "input_tokens": 10})
        self.assertEqual(usage.record_fingerprint(rec1), usage.record_fingerprint(rec2))

    def test_returns_empty_string_for_object_with_no_model_attribute(self):
        """A missing attribute is the unfingerprintable sentinel; the caller
        must emit and must not store such a record."""
        rec = object()  # has neither model nor token_usage
        result = usage.record_fingerprint(rec)
        self.assertEqual("", result)

    def test_returns_empty_string_for_none_input(self):
        result = usage.record_fingerprint(None)
        self.assertEqual("", result)

    def test_never_raises(self):
        for bad_input in [None, 42, [], {}, object()]:
            try:
                usage.record_fingerprint(bad_input)
            except Exception as exc:
                self.fail(f"record_fingerprint raised on {bad_input!r}: {exc}")


# ---------------------------------------------------------------------------
# load_usage_state / save_usage_state: state store contract
# ---------------------------------------------------------------------------

class TestLoadUsageState(unittest.TestCase):
    """load_usage_state returns the persisted record_id → fingerprint mapping,
    degrading to {} for any read, parse or schema problem."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)
        self.run_id = "20260101T170000Z-st01"

    def tearDown(self):
        self.tmp.cleanup()

    def test_absent_file_returns_empty_dict(self):
        result = usage.load_usage_state(self.paths, self.run_id, "no-such-stream-key")
        self.assertEqual({}, result)

    def test_corrupt_json_returns_empty_dict(self):
        entry = self.paths.usage_state_entry(self.run_id, "corrupt-key")
        entry.parent.mkdir(parents=True, exist_ok=True)
        entry.write_text("not valid json {{{{", encoding="utf-8")
        result = usage.load_usage_state(self.paths, self.run_id, "corrupt-key")
        self.assertEqual({}, result)

    def test_truncated_file_returns_empty_dict(self):
        entry = self.paths.usage_state_entry(self.run_id, "trunc-key")
        entry.parent.mkdir(parents=True, exist_ok=True)
        entry.write_text('{"version": 1, "records": {"msg_', encoding="utf-8")
        result = usage.load_usage_state(self.paths, self.run_id, "trunc-key")
        self.assertEqual({}, result)

    def test_unknown_version_returns_empty_dict(self):
        """A reader that does not recognise the version must not guess."""
        entry = self.paths.usage_state_entry(self.run_id, "ver-key")
        entry.parent.mkdir(parents=True, exist_ok=True)
        state = {"version": 999, "stream": "x", "records": {"msg_abc": "fp123"}}
        entry.write_text(json.dumps(state), encoding="utf-8")
        result = usage.load_usage_state(self.paths, self.run_id, "ver-key")
        self.assertEqual({}, result)

    def test_valid_state_file_returns_records_mapping(self):
        entry = self.paths.usage_state_entry(self.run_id, "valid-key")
        entry.parent.mkdir(parents=True, exist_ok=True)
        state = {
            "version": 1,
            "stream": "some-path",
            "records": {"msg_abc": "fp-aaa", "msg_def": "fp-bbb"},
        }
        entry.write_text(json.dumps(state), encoding="utf-8")
        result = usage.load_usage_state(self.paths, self.run_id, "valid-key")
        self.assertEqual({"msg_abc": "fp-aaa", "msg_def": "fp-bbb"}, result)

    def test_entry_with_empty_string_key_is_skipped(self):
        """Non-empty string constraint: empty keys are skipped."""
        entry = self.paths.usage_state_entry(self.run_id, "empty-k-key")
        entry.parent.mkdir(parents=True, exist_ok=True)
        state = {
            "version": 1,
            "stream": "x",
            "records": {"": "fp-bad", "msg_good": "fp-good"},
        }
        entry.write_text(json.dumps(state), encoding="utf-8")
        result = usage.load_usage_state(self.paths, self.run_id, "empty-k-key")
        self.assertNotIn("", result)
        self.assertIn("msg_good", result)

    def test_entry_with_empty_string_value_is_skipped(self):
        """Non-empty string constraint: empty fingerprint values are skipped."""
        entry = self.paths.usage_state_entry(self.run_id, "empty-v-key")
        entry.parent.mkdir(parents=True, exist_ok=True)
        state = {
            "version": 1,
            "stream": "x",
            "records": {"msg_bad": "", "msg_good": "fp-good"},
        }
        entry.write_text(json.dumps(state), encoding="utf-8")
        result = usage.load_usage_state(self.paths, self.run_id, "empty-v-key")
        self.assertNotIn("msg_bad", result)
        self.assertIn("msg_good", result)

    def test_entry_with_non_string_value_is_skipped(self):
        entry = self.paths.usage_state_entry(self.run_id, "nonstr-v-key")
        entry.parent.mkdir(parents=True, exist_ok=True)
        state = {
            "version": 1,
            "stream": "x",
            "records": {"msg_bad": 42, "msg_good": "fp-good"},
        }
        entry.write_text(json.dumps(state), encoding="utf-8")
        result = usage.load_usage_state(self.paths, self.run_id, "nonstr-v-key")
        self.assertNotIn("msg_bad", result)
        self.assertIn("msg_good", result)

    def test_partially_usable_file_returns_usable_entries(self):
        """Malformed individual entries are skipped; valid ones are returned."""
        entry = self.paths.usage_state_entry(self.run_id, "partial-key")
        entry.parent.mkdir(parents=True, exist_ok=True)
        state = {
            "version": 1,
            "stream": "x",
            "records": {
                "msg_good1": "fp-aaa",
                "": "fp-bad-empty-key",
                "msg_good2": "fp-bbb",
                "msg_bad_val": 0,
            },
        }
        entry.write_text(json.dumps(state), encoding="utf-8")
        result = usage.load_usage_state(self.paths, self.run_id, "partial-key")
        self.assertEqual({"msg_good1": "fp-aaa", "msg_good2": "fp-bbb"}, result)

    def test_never_raises(self):
        try:
            usage.load_usage_state(self.paths, self.run_id, "nr-key")
        except Exception as exc:
            self.fail(f"load_usage_state raised: {exc}")


class TestSaveUsageState(unittest.TestCase):
    """save_usage_state persists a record_id → fingerprint mapping atomically.
    Returns True on success, False on any I/O failure. Never raises."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)
        self.run_id = "20260101T170000Z-sv01"

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_true_on_success(self):
        result = usage.save_usage_state(
            self.paths, self.run_id, "save-key", {"msg_x": "fp-123"}
        )
        self.assertTrue(result)

    def test_round_trip_preserves_entries(self):
        state = {"msg_abc": "fp-1234abcd", "msg_def": "fp-5678efgh"}
        usage.save_usage_state(self.paths, self.run_id, "rt-key", state)
        result = usage.load_usage_state(self.paths, self.run_id, "rt-key")
        self.assertEqual(state, result)

    def test_creates_parent_directory_when_absent(self):
        """save_usage_state must create the state dir if it does not exist."""
        # State dir should not exist before calling save
        state_dir = self.paths.usage_state_dir(self.run_id)
        self.assertFalse(state_dir.exists())
        result = usage.save_usage_state(
            self.paths, self.run_id, "mkdir-key", {"msg_y": "fp-abc"}
        )
        self.assertTrue(result)
        self.assertTrue(state_dir.exists())

    def test_overwrites_previous_save_for_same_stream_key(self):
        usage.save_usage_state(self.paths, self.run_id, "ow-key", {"msg_a": "fp-old"})
        usage.save_usage_state(self.paths, self.run_id, "ow-key", {"msg_a": "fp-new"})
        result = usage.load_usage_state(self.paths, self.run_id, "ow-key")
        self.assertEqual({"msg_a": "fp-new"}, result)

    def test_different_stream_keys_are_independent(self):
        usage.save_usage_state(self.paths, self.run_id, "key-a", {"msg_1": "fp-1"})
        usage.save_usage_state(self.paths, self.run_id, "key-b", {"msg_2": "fp-2"})
        result_a = usage.load_usage_state(self.paths, self.run_id, "key-a")
        result_b = usage.load_usage_state(self.paths, self.run_id, "key-b")
        self.assertEqual({"msg_1": "fp-1"}, result_a)
        self.assertEqual({"msg_2": "fp-2"}, result_b)

    def test_returns_false_on_io_failure_without_raising(self):
        # Place a regular file where the state directory would be, causing
        # any mkdir attempt to fail with NotADirectoryError.
        run_root = self.paths.run_root(self.run_id)
        run_root.mkdir(parents=True, exist_ok=True)
        blocker = run_root / core.USAGE_STATE_DIRNAME
        blocker.write_bytes(b"")  # file at what should be a directory
        result = usage.save_usage_state(
            self.paths, self.run_id, "fail-key", {"msg_z": "fp-z"}
        )
        self.assertFalse(result)

    def test_never_raises(self):
        try:
            usage.save_usage_state(
                self.paths, self.run_id, "nr-save-key", {"msg_a": "fp-a"}
            )
        except Exception as exc:
            self.fail(f"save_usage_state raised: {exc}")

    def test_empty_state_saves_and_loads_back_as_empty_dict(self):
        usage.save_usage_state(self.paths, self.run_id, "empty-state-key", {})
        result = usage.load_usage_state(self.paths, self.run_id, "empty-state-key")
        self.assertEqual({}, result)


# ---------------------------------------------------------------------------
# Emit-on-change: full behavioral contract
# ---------------------------------------------------------------------------

class TestEmitOnChange(unittest.TestCase):
    """emit_usage_records loads per-stream state before the loop and persists
    it after: new records are emitted, changed records are re-emitted, and
    unchanged records are skipped."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.ctx = _make_ctx(self.tmp_path)
        self.transcript_path = self.tmp_path / "transcript.jsonl"

    def tearDown(self):
        self.tmp.cleanup()

    def test_first_firing_emits_all_records_in_transcript(self):
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=10, output_tokens=5, message_id="msg_a"),
            _assistant_record(input_tokens=20, output_tokens=8, message_id="msg_b"),
        ])
        count = usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        self.assertEqual(2, count)

    def test_immediate_repeat_over_unchanged_transcript_emits_nothing(self):
        """The entire second firing is suppressed: count = 0, no new lines."""
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=10, output_tokens=5, message_id="msg_r"),
        ])
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        count = usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        self.assertEqual(0, count)
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == usage.USAGE_EVENT]
        self.assertEqual(1, len(usage_events))

    def test_changed_token_usage_triggers_reemission(self):
        """A record whose token_usage settled to new values is re-emitted."""
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=10, output_tokens=5, message_id="msg_s"),
        ])
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=10, output_tokens=50, message_id="msg_s"),
        ])
        count = usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        self.assertEqual(1, count)
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == usage.USAGE_EVENT]
        self.assertEqual(2, len(usage_events))
        # Latest observation has the settled output_tokens value
        self.assertEqual(50, usage_events[1]["token_usage"]["output_tokens"])

    def test_changed_model_triggers_reemission(self):
        """A record whose model changed is re-emitted."""
        _write_transcript(self.transcript_path, [
            _assistant_record(model="claude-opus-4-5", input_tokens=10, message_id="msg_m"),
        ])
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        _write_transcript(self.transcript_path, [
            _assistant_record(model="claude-haiku-3", input_tokens=10, message_id="msg_m"),
        ])
        count = usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        self.assertEqual(1, count)

    def test_new_record_emitted_while_unchanged_earlier_records_are_not(self):
        """Growing transcript: only the new record is emitted on the second firing."""
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=10, output_tokens=5, message_id="msg_early"),
        ])
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=10, output_tokens=5, message_id="msg_early"),
            _assistant_record(input_tokens=20, output_tokens=8, message_id="msg_new"),
        ])
        count = usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        self.assertEqual(1, count)
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == usage.USAGE_EVENT]
        self.assertEqual(2, len(usage_events))
        # Second event is the genuinely new record
        self.assertEqual("msg_new", usage_events[1]["record_id"])

    def test_return_count_equals_records_actually_appended(self):
        """The return value always equals the number of lines actually written,
        matching both the count from a growing transcript and a zero-write firing."""
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=5, output_tokens=2, message_id="msg_c1"),
            _assistant_record(input_tokens=7, output_tokens=3, message_id="msg_c2"),
        ])
        count1 = usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        events_after1 = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        self.assertEqual(count1, len([e for e in events_after1 if e["event"] == usage.USAGE_EVENT]))

        count2 = usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        self.assertEqual(0, count2)

    def test_lines_written_grow_linearly_not_quadratically(self):
        """Across N firings where each firing sees one additional new record,
        the total lines written equals N (linear), not N*(N+1)/2 (quadratic)."""
        message_ids = ["msg_g1", "msg_g2", "msg_g3", "msg_g4"]
        all_records = []
        for i, mid in enumerate(message_ids):
            all_records.append(
                _assistant_record(input_tokens=10 * (i + 1), output_tokens=5, message_id=mid)
            )
            _write_transcript(self.transcript_path, all_records)
            usage.emit_usage_records(
                self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
            )

        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == usage.USAGE_EVENT]
        # Linear: 1+1+1+1 = 4. Quadratic would be 1+2+3+4 = 10.
        self.assertEqual(len(message_ids), len(usage_events))

    def test_orchestrator_stream_state_does_not_suppress_invocation_stream(self):
        """A record emitted to the orchestrator stream must still be emitted
        to an invocation stream; per-stream state keys are distinct."""
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=10, output_tokens=5, message_id="msg_iso"),
        ])
        # Emit to orchestrator stream
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        # Emit the same record to an invocation stream
        count = usage.emit_usage_records(
            self.ctx, str(self.transcript_path), "ResearchAgent#1", "agent_transcript"
        )
        self.assertEqual(1, count)
        inv_events = _read_jsonl(self.ctx.paths.invocation_events(_RUN_ID, "ResearchAgent#1"))
        self.assertEqual(1, len([e for e in inv_events if e["event"] == usage.USAGE_EVENT]))

    def test_invocation_stream_state_does_not_suppress_quarantine_destination(self):
        """The quarantine sink_override branch acquires its own isolated state
        automatically via sink-derived stream keying."""
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=10, output_tokens=5, message_id="msg_qua"),
        ])
        quarantine_path = self.ctx.paths.quarantine_events(_RUN_ID, "agent-qua-id")
        # Emit to invocation stream
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), "SubAgent#1", "agent_transcript"
        )
        # Same record to quarantine destination (different sink → different state key)
        count = usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "agent_transcript",
            sink_override=quarantine_path,
        )
        self.assertEqual(1, count)
        qua_events = _read_jsonl(quarantine_path)
        self.assertEqual(1, len([e for e in qua_events if e["event"] == usage.USAGE_EVENT]))

    def test_state_entries_for_absent_records_are_retained(self):
        """Records that disappear from a transcript read (e.g. partial read)
        keep their state entries — they are not pruned, to prevent mass
        re-emission on the next firing when the transcript is fully available."""
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=5, output_tokens=2, message_id="msg_retain"),
        ])
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        # Replace transcript with an empty file (simulating a partial read)
        _write_transcript(self.transcript_path, [])
        usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        # Restore transcript — msg_retain's state must still exist
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=5, output_tokens=2, message_id="msg_retain"),
        ])
        count_third = usage.emit_usage_records(
            self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
        )
        # msg_retain is unchanged so its retained state suppresses re-emission
        self.assertEqual(0, count_third)

    def test_unkeyed_record_is_emitted_on_every_firing_not_suppressed(self):
        """A record with a falsy record_id cannot be keyed into state and must
        be emitted on every firing, never suppressed by the state store.

        The transcript reader guarantees record_id is always non-empty — it
        synthesises a deterministic position-hash fallback when no message id
        is present in the transcript line.  That means omitting message_id from
        a transcript fixture cannot produce a falsy record_id at the boundary
        where emit_usage_records inspects it.

        The only way to exercise the falsy-record_id always-emit branch is to
        patch read_assistant_records to return a RecordFacts whose record_id is
        genuinely empty, bypassing the reader's synthesis step entirely.

        Two firings with such a record must produce two usage events, not one."""
        fake_record = transcript.RecordFacts(
            record_id="",
            record_index=0,
            model="claude-opus-4-5",
            token_usage={"input_tokens": 7, "output_tokens": 3},
        )
        with mock.patch(
            "mosaic_logger_usage.transcript.read_assistant_records",
            return_value=[fake_record],
        ):
            usage.emit_usage_records(
                self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
            )
            count2 = usage.emit_usage_records(
                self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
            )
        # Second firing must still emit; records with falsy record_id are never suppressed.
        self.assertEqual(1, count2)
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == usage.USAGE_EVENT]
        self.assertEqual(2, len(usage_events))

    def test_unfingerprintable_record_is_emitted_on_every_firing_and_not_persisted(self):
        """A keyed record whose fingerprint resolves to "" (unfingerprintable
        degradation) is emitted on every firing and never stored in the
        persisted state mapping.

        Two firings must each emit the record (a) and the saved state must
        contain no entry for it after either firing (b)."""
        _write_transcript(self.transcript_path, [
            _assistant_record(input_tokens=9, output_tokens=4, message_id="msg_nofp"),
        ])
        with mock.patch(
            "mosaic_logger_usage.record_fingerprint",
            return_value="",
        ):
            usage.emit_usage_records(
                self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
            )
            count2 = usage.emit_usage_records(
                self.ctx, str(self.transcript_path), None, "orchestrator_transcript"
            )

        # (a) second firing must re-emit because "" fingerprint means always-emit
        self.assertEqual(1, count2)
        events = _read_jsonl(self.ctx.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == usage.USAGE_EVENT]
        self.assertEqual(2, len(usage_events))

        # (b) "msg_nofp" must be absent from the persisted state mapping.
        # Derive the stream key using the normative formula from the design
        # contract: SHA-256 of str(sink), first 16 hex characters.
        import hashlib as _hashlib
        sink = self.ctx.paths.orchestrator_events(_RUN_ID)
        stream_key = _hashlib.sha256(str(sink).encode("utf-8")).hexdigest()[:16]
        state = usage.load_usage_state(self.ctx.paths, _RUN_ID, stream_key)
        self.assertNotIn("msg_nofp", state)


# ---------------------------------------------------------------------------
# Never-raises: state store and fingerprint discipline
# ---------------------------------------------------------------------------

class TestStateStoreNeverRaises(unittest.TestCase):
    """Every new entry point in the state and fingerprint path must be
    unconditionally non-raising, regardless of I/O or serialization failure."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)
        self.run_id = "20260101T170000Z-nr01"
        self.ctx = _make_ctx(self.tmp_path)
        self._orig_debug = os.environ.get("MOSAIC_LOGGER_DEBUG")

    def tearDown(self):
        self.tmp.cleanup()
        if self._orig_debug is None:
            os.environ.pop("MOSAIC_LOGGER_DEBUG", None)
        else:
            os.environ["MOSAIC_LOGGER_DEBUG"] = self._orig_debug

    def test_load_usage_state_never_raises_on_open_failure(self):
        with mock.patch("builtins.open", side_effect=OSError("no permission")):
            try:
                result = usage.load_usage_state(self.paths, self.run_id, "nr-load-key")
            except Exception as exc:
                self.fail(f"load_usage_state raised: {exc}")
        self.assertEqual({}, result)

    def test_save_usage_state_never_raises_on_mkdir_failure(self):
        run_root = self.paths.run_root(self.run_id)
        run_root.mkdir(parents=True, exist_ok=True)
        blocker = run_root / core.USAGE_STATE_DIRNAME
        blocker.write_bytes(b"")  # file where directory is expected
        try:
            usage.save_usage_state(
                self.paths, self.run_id, "nr-save-key", {"msg_a": "fp-a"}
            )
        except Exception as exc:
            self.fail(f"save_usage_state raised: {exc}")

    def test_save_failure_logs_diagnostic_when_debug_enabled(self):
        debug_file = self.tmp_path / "debug.log"
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        run_root = self.paths.run_root(self.run_id)
        run_root.mkdir(parents=True, exist_ok=True)
        blocker = run_root / core.USAGE_STATE_DIRNAME
        blocker.write_bytes(b"")
        usage.save_usage_state(
            self.paths, self.run_id, "diag-key", {"msg_b": "fp-b"}
        )
        self.assertTrue(debug_file.exists())

    def test_emit_usage_records_never_raises_when_state_load_fails(self):
        transcript_path = self.tmp_path / "t.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(input_tokens=5, message_id="msg_sl"),
        ])
        with mock.patch(
            "mosaic_logger_usage.load_usage_state",
            side_effect=RuntimeError("state boom"),
        ):
            try:
                count = usage.emit_usage_records(
                    self.ctx, str(transcript_path), None, "orchestrator_transcript"
                )
            except Exception as exc:
                self.fail(f"emit_usage_records raised on state-load failure: {exc}")
        # Degraded to emitting all records (missing state = emit everything):
        # exactly the one record in the transcript, not zero (dropped) or more.
        self.assertEqual(1, count)

    def test_emit_usage_records_never_raises_when_state_save_fails(self):
        transcript_path = self.tmp_path / "t2.jsonl"
        _write_transcript(transcript_path, [
            _assistant_record(input_tokens=5, message_id="msg_ss"),
        ])
        with mock.patch(
            "mosaic_logger_usage.save_usage_state",
            side_effect=RuntimeError("save boom"),
        ):
            try:
                count = usage.emit_usage_records(
                    self.ctx, str(transcript_path), None, "orchestrator_transcript"
                )
            except Exception as exc:
                self.fail(f"emit_usage_records raised on state-save failure: {exc}")
        # Events were already appended before the save attempt; the save failure
        # must not suppress what was already written.  Exactly one record in the
        # transcript, so exactly one event must have been emitted.
        self.assertEqual(1, count)

    def test_record_fingerprint_never_raises(self):
        bad_inputs = [None, 42, [], {}, "string", b"bytes", object()]
        for val in bad_inputs:
            try:
                usage.record_fingerprint(val)
            except Exception as exc:
                self.fail(f"record_fingerprint raised on {val!r}: {exc}")


if __name__ == "__main__":
    unittest.main()
