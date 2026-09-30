"""Tests for hand-back completion at the handler level.

A subagent's SubagentHandback call is the delivery of its reply: the
PostToolUse firing completes the invocation (invocation_end, final assistant
turn, 02_output.md). A later SubagentStop must not complete it a second time,
but keeps its other duties. Without a hand-back, SubagentStop stays the
delivery point. Edge cases end in an explicit, reported outcome.
"""

import json
import os
import pathlib
import re
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger
import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger_handlers_invocation as invocation
import mosaic_logger_handlers_tools as tools
import mosaic_logger_transcript as transcript


_RUN_ID = "20260101T170000Z-d4e2"
_SESSION_ID = "handback-session"
_AGENT_ID = "a24d6c04de2a3dec4"
_INSTANCE = "Reviewer#1"
_TS_START = "2026-01-01T17:00:00.000Z"
_TS_PRE = "2026-01-01T17:00:05.000Z"
_TS_POST = "2026-01-01T17:00:05.060Z"
_TS_STOP = "2026-01-01T17:00:12.000Z"

_REPLY = json.dumps({
    "agent_instance_id": _INSTANCE, "run_id": _RUN_ID,
    "status_code": "SUCCESS", "status_message": "All checks done.",
})
_ACK = {"success": True, "message": "Report delivered to your caller."}


def _ctx(tmp, event, ts, **payload):
    root = pathlib.Path(tmp)
    body = {"hook_event_name": event, "session_id": _SESSION_ID, **payload}
    ctx = core.HookContext(body, root, core.build_paths(root), ts)
    ctx.run_id = _RUN_ID
    return ctx


def _read_jsonl(path):
    path = pathlib.Path(path)
    if not path.exists():
        return []
    return [json.loads(ln) for ln in path.read_text("utf-8").splitlines() if ln.strip()]


def _write_agent_transcript(session_path, agent_id, records):
    """Write <session minus .jsonl>/subagents/agent-<id>.jsonl and return it."""
    base = str(session_path)[:-len(".jsonl")]
    target = pathlib.Path(base) / "subagents" / f"agent-{agent_id}.jsonl"
    target.parent.mkdir(parents=True, exist_ok=True)
    with open(target, "w", encoding="utf-8") as fh:
        for r in records:
            fh.write(json.dumps(r) + "\n")
    return target


def _assistant(msg_id, model="claude-opus-4-5", input_tokens=100, output_tokens=50):
    return {"type": "assistant", "message": {
        "id": msg_id, "model": model,
        "usage": {"input_tokens": input_tokens, "output_tokens": output_tokens}}}


class _Base(unittest.TestCase):

    def setUp(self):
        self._td = tempfile.TemporaryDirectory()
        self.tmp = self._td.name
        self.paths = core.build_paths(pathlib.Path(self.tmp))
        self.session_path = pathlib.Path(self.tmp) / "claude-sessions" / "sess-1.jsonl"
        self.session_path.parent.mkdir(parents=True, exist_ok=True)
        self.session_path.write_text("", encoding="utf-8")
        self.debug_file = pathlib.Path(self.tmp) / "debug.log"
        self._orig_debug = os.environ.get("MOSAIC_LOGGER_DEBUG")
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(self.debug_file)
        runstate.put_agent_mapping(self.paths, _RUN_ID, _AGENT_ID, _INSTANCE, "Reviewer")

    def tearDown(self):
        if self._orig_debug is None:
            os.environ.pop("MOSAIC_LOGGER_DEBUG", None)
        else:
            os.environ["MOSAIC_LOGGER_DEBUG"] = self._orig_debug
        self._td.cleanup()

    # -- actions -----------------------------------------------------------
    def handback_pre(self, message=_REPLY, agent_id=_AGENT_ID, use_id="tu-hb-1"):
        ctx = _ctx(self.tmp, "PreToolUse", _TS_PRE, agent_id=agent_id,
                   tool_name="SubagentHandback", tool_use_id=use_id,
                   tool_input={"message": message},
                   transcript_path=str(self.session_path))
        tools.handle_pre_tool_use(ctx)

    def handback_post(self, message=_REPLY, agent_id=_AGENT_ID, use_id="tu-hb-1",
                      ts=_TS_POST, **overrides):
        payload = {"agent_id": agent_id, "tool_name": "SubagentHandback",
                   "tool_use_id": use_id, "tool_input": {"message": message},
                   "tool_response": _ACK, "duration_ms": 60,
                   "transcript_path": str(self.session_path)}
        payload.update(overrides)
        payload = {k: v for k, v in payload.items() if v is not _MISSING}
        ctx = _ctx(self.tmp, "PostToolUse", ts, **payload)
        tools.handle_post_tool_use(ctx)

    def stop(self, message="Post hand-back chatter.", agent_id=_AGENT_ID, **extra):
        payload = {"agent_id": agent_id, "agent_type": "Reviewer",
                   "last_assistant_message": message,
                   "transcript_path": str(self.session_path)}
        payload.update(extra)
        invocation.handle_subagent_stop(_ctx(self.tmp, "SubagentStop", _TS_STOP, **payload))

    # -- observations ------------------------------------------------------
    def events(self, instance=_INSTANCE):
        return _read_jsonl(self.paths.invocation_events(_RUN_ID, instance))

    def of(self, name, instance=_INSTANCE):
        return [e for e in self.events(instance) if e["event"] == name]

    def output_text(self, instance=_INSTANCE):
        p = self.paths.invocation_output(_RUN_ID, instance)
        return p.read_text("utf-8") if p.exists() else None

    def debug_text(self):
        return self.debug_file.read_text("utf-8") if self.debug_file.exists() else ""


class _Missing:
    pass


_MISSING = _Missing()


# ---------------------------------------------------------------------------
# Completion at hand-back time
# ---------------------------------------------------------------------------

class TestHandbackCompletion(_Base):

    def test_post_tool_use_emits_single_invocation_end_with_reply(self):
        self.handback_post()
        ends = self.of("invocation_end")
        self.assertEqual(1, len(ends))
        self.assertEqual(_REPLY, ends[0]["response"])
        self.assertEqual(_INSTANCE, ends[0]["agent_instance_id"])

    def test_status_code_is_extracted_from_handback_message(self):
        self.handback_post(json.dumps({"status_code": "BLOCKED", "error_code": "E101"}))
        self.assertEqual("BLOCKED", self.of("invocation_end")[0]["status_code"])

    def test_invocation_end_is_stamped_with_post_tool_use_time(self):
        self.handback_pre()
        self.handback_post()
        end = self.of("invocation_end")[0]
        self.assertEqual(_TS_POST, end["timestamp"])
        self.assertNotEqual(_TS_PRE, end["timestamp"])

    def test_invocation_end_records_handback_as_completion_source(self):
        self.handback_post()
        self.assertEqual("handback", self.of("invocation_end")[0]["completion_source"])

    def test_json_reply_has_no_response_format_marker(self):
        self.handback_post()
        self.assertNotIn("response_format", self.of("invocation_end")[0])

    def test_final_assistant_turn_carries_handback_message(self):
        self.handback_post()
        turns = [t for t in self.of("turn") if t.get("role") == "assistant"]
        self.assertEqual(1, len(turns))
        self.assertEqual(_REPLY, turns[0]["content"])

    def test_output_artifact_shows_reply_and_status_code(self):
        self.handback_post()
        text = self.output_text()
        self.assertIsNotNone(text, "02_output.md must exist after the hand-back")
        self.assertIn("All checks done.", text)
        self.assertIn("SUCCESS", text)

    def test_ordinary_tool_call_end_is_still_emitted(self):
        self.handback_post()
        ends = self.of("tool_call_end")
        self.assertEqual(1, len(ends))
        self.assertIn("Report delivered", json.dumps(ends[0].get("tool_output")))

    def test_pre_tool_use_alone_does_not_complete(self):
        self.handback_pre()
        self.assertEqual([], self.of("invocation_end"))
        self.assertEqual([], self.of("turn"))
        self.assertIsNone(self.output_text())
        self.assertEqual(1, len(self.of("tool_call_start")))

    def test_completion_does_not_need_a_subagent_stop(self):
        self.handback_post()
        self.assertEqual(1, len(self.of("invocation_end")))
        self.assertEqual(1, len([t for t in self.of("turn")
                                 if t.get("role") == "assistant"]))
        self.assertIsNotNone(self.output_text())

    def test_handback_does_not_write_to_orchestrator_stream(self):
        self.handback_post()
        self.assertFalse(self.paths.orchestrator_events(_RUN_ID).exists())

    def test_never_raises(self):
        try:
            self.handback_post()
        except Exception as exc:
            self.fail(f"hand-back handling raised: {exc}")

    def test_completion_claim_records_handback_source_time_and_transcript(self):
        target = _write_agent_transcript(self.session_path, _AGENT_ID, [_assistant("m1")])
        self.handback_post()
        doc = runstate.get_completion_claim(self.paths, _RUN_ID, _AGENT_ID)
        self.assertIsNotNone(doc, "a completed hand-back must leave a claim")
        self.assertEqual(runstate.CLAIM_SOURCE_HANDBACK, doc["source"])
        self.assertEqual(_TS_POST, doc["claimed_at"])
        self.assertEqual(target, pathlib.Path(doc["agent_transcript_path"]))
        self.assertEqual(
            pathlib.Path(transcript.derive_agent_transcript_path(
                str(self.session_path), _AGENT_ID)),
            pathlib.Path(doc["agent_transcript_path"]))

    def test_claim_error_degrades_toward_emitting_and_reports(self):
        claim_dir = self.paths.completion_claim_dir(_RUN_ID)
        claim_dir.parent.mkdir(parents=True, exist_ok=True)
        claim_dir.write_bytes(b"")  # a file where the directory must go
        before = self.debug_text()
        self.handback_post()
        self.assertEqual(1, len(self.of("invocation_end")))
        self.assertEqual(_REPLY, self.of("invocation_end")[0]["response"])
        self.assertIn(_AGENT_ID, self.debug_text()[len(before):])


class TestHandbackModelAndUsage(_Base):
    """model / token_usage come from the agent transcript at the derived path."""

    def test_model_and_tokens_read_from_derived_transcript(self):
        _write_agent_transcript(self.session_path, _AGENT_ID, [
            _assistant("msg_a", "claude-opus-4-5", 200, 75)])
        self.handback_post()
        end = self.of("invocation_end")[0]
        self.assertEqual("claude-opus-4-5", end["model"])
        self.assertEqual(200, end["token_usage"]["input_tokens"])
        self.assertEqual(75, end["token_usage"]["output_tokens"])

    def test_final_turn_carries_model_from_derived_transcript(self):
        _write_agent_transcript(self.session_path, _AGENT_ID, [_assistant("msg_a", "m-x")])
        self.handback_post()
        turn = [t for t in self.of("turn") if t.get("role") == "assistant"][0]
        self.assertEqual("m-x", turn["model"])

    def test_usage_records_emitted_at_handback_time(self):
        _write_agent_transcript(self.session_path, _AGENT_ID, [_assistant("msg_hb")])
        self.handback_post()
        usage = self.of("usage_record")
        self.assertEqual(["msg_hb"], [u["record_id"] for u in usage])
        self.assertEqual("agent_transcript", usage[0]["source"])

    def test_transcript_lag_omits_model_and_tokens_and_still_completes(self):
        self.handback_post()  # agent transcript file does not exist yet
        end = self.of("invocation_end")[0]
        self.assertNotIn("model", end)
        self.assertNotIn("token_usage", end)
        self.assertEqual(_REPLY, end["response"])

    def test_transcript_lag_writes_diagnostic_naming_agent(self):
        debug_before = self.debug_text()
        self.handback_post()
        self.assertIn(_AGENT_ID, self.debug_text()[len(debug_before):])

    def test_transcript_file_without_assistant_record_omits_fields_and_reports(self):
        debug_before = self.debug_text()
        _write_agent_transcript(self.session_path, _AGENT_ID, [
            {"type": "user", "message": {"role": "user", "content": "task"}}])
        self.handback_post()
        end = self.of("invocation_end")[0]
        self.assertNotIn("model", end)
        self.assertNotIn("token_usage", end)
        self.assertEqual(_REPLY, end["response"])
        self.assertIn(_AGENT_ID, self.debug_text()[len(debug_before):])

    def test_empty_transcript_file_omits_fields_and_still_completes(self):
        debug_before = self.debug_text()
        _write_agent_transcript(self.session_path, _AGENT_ID, [])
        self.handback_post()
        end = self.of("invocation_end")[0]
        self.assertNotIn("model", end)
        self.assertNotIn("token_usage", end)
        self.assertIn(_AGENT_ID, self.debug_text()[len(debug_before):])

    def test_lag_is_never_back_filled_by_a_later_stop(self):
        self.handback_post()
        late = _write_agent_transcript(self.session_path, _AGENT_ID, [_assistant("msg_late")])
        self.stop(agent_transcript_path=str(late))
        ends = self.of("invocation_end")
        self.assertEqual(1, len(ends))
        self.assertNotIn("model", ends[0])
        self.assertNotIn("token_usage", ends[0])

    def test_transcript_only_looked_up_at_derived_path(self):
        """A transcript at a plausible but different location is not used."""
        wrong = self.session_path.parent / "sess-1" / f"agent-{_AGENT_ID}.jsonl"
        wrong.parent.mkdir(parents=True, exist_ok=True)
        wrong.write_text(json.dumps(_assistant("msg_wrong")) + "\n", encoding="utf-8")
        self.handback_post()
        end = self.of("invocation_end")[0]
        self.assertNotIn("model", end)
        self.assertEqual([], self.of("usage_record"))

    def test_underivable_path_omits_fields_and_reports(self):
        debug_before = self.debug_text()
        self.handback_post(transcript_path=_MISSING)
        end = self.of("invocation_end")[0]
        self.assertNotIn("model", end)
        self.assertNotIn("token_usage", end)
        self.assertIn(_AGENT_ID, self.debug_text()[len(debug_before):])

    def test_session_path_without_jsonl_extension_is_not_guessed(self):
        _write_agent_transcript(self.session_path, _AGENT_ID, [_assistant("msg_a")])
        self.handback_post(transcript_path=str(self.session_path)[:-len(".jsonl")])
        self.assertNotIn("model", self.of("invocation_end")[0])


# ---------------------------------------------------------------------------
# Exactly one completion, and SubagentStop's remaining duties
# ---------------------------------------------------------------------------

class TestSubagentStopAfterHandback(_Base):

    def _handback_then_stop(self, **stop_extra):
        agent_tp = _write_agent_transcript(
            self.session_path, _AGENT_ID, [_assistant("msg_hb")])
        self.handback_post()
        self.stop(agent_transcript_path=str(agent_tp), **stop_extra)
        return agent_tp

    def test_no_second_invocation_end(self):
        self._handback_then_stop()
        ends = self.of("invocation_end")
        self.assertEqual(1, len(ends))
        self.assertEqual("handback", ends[0].get("completion_source"))

    def test_no_second_final_turn(self):
        self._handback_then_stop()
        turns = [t for t in self.of("turn") if t.get("role") == "assistant"]
        self.assertEqual(1, len(turns))
        self.assertEqual(_REPLY, turns[0]["content"])

    def test_output_artifact_not_overwritten(self):
        self.handback_post()
        before = self.output_text()
        self.stop(message='{"status_code": "BLOCKED", "status_message": "late"}')
        self.assertEqual(before, self.output_text())

    def test_post_handback_text_never_becomes_reply_or_status(self):
        self._handback_then_stop(
            last_assistant_message='{"status_code": "BLOCKED"} late words')
        end = self.of("invocation_end")[0]
        self.assertEqual(_REPLY, end["response"])
        self.assertEqual("SUCCESS", end["status_code"])
        self.assertNotIn("late words", self.output_text())

    def test_no_quarantine_output(self):
        self._handback_then_stop()
        self.assertFalse(self.paths.quarantine_dir(_RUN_ID).exists())
        run_root = self.paths.run_root(_RUN_ID)
        legacy = [d for d in run_root.iterdir() if d.name.startswith("unmapped_")]
        self.assertEqual([], legacy)

    def test_transcript_export_still_produced(self):
        agent_tp = self._handback_then_stop()
        raw = self.paths.invocation_raw(_RUN_ID, _INSTANCE)
        self.assertTrue(raw.exists())
        self.assertEqual(agent_tp.read_bytes(), raw.read_bytes())

    def test_post_handback_tokens_are_still_counted(self):
        agent_tp = _write_agent_transcript(
            self.session_path, _AGENT_ID, [_assistant("msg_hb")])
        self.handback_post()
        _write_agent_transcript(
            self.session_path, _AGENT_ID, [_assistant("msg_hb"), _assistant("msg_late")])
        self.stop(agent_transcript_path=str(agent_tp))
        ids = sorted(u["record_id"] for u in self.of("usage_record"))
        self.assertEqual(["msg_hb", "msg_late"], ids)

    def test_usage_records_are_not_duplicated_by_stop(self):
        self._handback_then_stop()
        ids = [u["record_id"] for u in self.of("usage_record")]
        self.assertEqual(["msg_hb"], ids)

    def test_orchestrator_usage_still_emitted_at_stop(self):
        self.session_path.write_text(
            json.dumps(_assistant("msg_orch")) + "\n", encoding="utf-8")
        self._handback_then_stop()
        orch = [e for e in _read_jsonl(self.paths.orchestrator_events(_RUN_ID))
                if e["event"] == "usage_record"]
        self.assertEqual(["msg_orch"], [u["record_id"] for u in orch])

    def test_stop_after_handback_does_not_raise(self):
        try:
            self._handback_then_stop()
        except Exception as exc:
            self.fail(f"SubagentStop raised: {exc}")

    def test_stop_held_by_handback_is_reported(self):
        self.handback_post()
        before = self.debug_text()
        self.stop()
        self.assertIn(_AGENT_ID, self.debug_text()[len(before):])

    def test_differing_transcript_path_changes_no_events(self):
        self.handback_post()
        events_before = self.events()
        self.stop(agent_transcript_path=str(pathlib.Path(self.tmp) / "elsewhere.jsonl"))
        self.assertEqual(events_before, self.events())


class TestHandbackAfterCompletedStop(_Base):
    """SubagentStop won the completion first; the hand-back arrives second."""

    def _stop_then_handback(self):
        self.stop(message="stop-side reply")
        after_stop = self.output_text()
        self.handback_post()
        return after_stop

    def test_exactly_one_invocation_end_with_stop_side_response(self):
        self._stop_then_handback()
        ends = self.of("invocation_end")
        self.assertEqual(1, len(ends))
        self.assertEqual("stop-side reply", ends[0]["response"])
        self.assertNotIn("completion_source", ends[0])

    def test_no_second_final_turn(self):
        self._stop_then_handback()
        turns = [t for t in self.of("turn") if t.get("role") == "assistant"]
        self.assertEqual(1, len(turns))
        self.assertEqual("stop-side reply", turns[0]["content"])

    def test_output_artifact_not_overwritten(self):
        after_stop = self._stop_then_handback()
        self.assertIsNotNone(after_stop)
        self.assertEqual(after_stop, self.output_text())

    def test_ordinary_tool_call_end_is_still_written(self):
        self._stop_then_handback()
        self.assertEqual(1, len(self.of("tool_call_end")))

    def test_handback_after_stop_does_not_raise(self):
        try:
            self._stop_then_handback()
        except Exception as exc:
            self.fail(f"hand-back after stop raised: {exc}")


class TestSecondHandback(_Base):

    def test_second_handback_adds_no_second_completion(self):
        self.handback_post()
        self.handback_pre(message='{"status_code": "BLOCKED"}', use_id="tu-hb-2")
        self.handback_post(message='{"status_code": "BLOCKED"}', use_id="tu-hb-2",
                           ts="2026-01-01T17:00:07.000Z")
        self.assertEqual(1, len(self.of("invocation_end")))
        turns = [t for t in self.of("turn") if t.get("role") == "assistant"]
        self.assertEqual(1, len(turns))

    def test_first_handback_wins(self):
        self.handback_post()
        self.handback_post(message='{"status_code": "BLOCKED"}', use_id="tu-hb-2",
                           ts="2026-01-01T17:00:07.000Z")
        end = self.of("invocation_end")[0]
        self.assertEqual("SUCCESS", end["status_code"])
        self.assertEqual(_TS_POST, end["timestamp"])
        self.assertIn("All checks done.", self.output_text())

    def test_second_handback_is_kept_as_ordinary_tool_events(self):
        self.handback_post()
        self.handback_pre(use_id="tu-hb-2")
        self.handback_post(use_id="tu-hb-2", ts="2026-01-01T17:00:07.000Z")
        self.assertEqual(2, len(self.of("tool_call_end")))
        self.assertEqual(1, len(self.of("tool_call_start")))


class TestNoHandbackFallback(_Base):

    def test_stop_without_handback_completes_as_before(self):
        self.stop(message='{"status_code": "PARTIALLY_DONE"}')
        end = self.of("invocation_end")[0]
        self.assertEqual('{"status_code": "PARTIALLY_DONE"}', end["response"])
        self.assertEqual("PARTIALLY_DONE", end["status_code"])
        self.assertNotIn("completion_source", end)
        self.assertNotIn("response_format", end)
        self.assertEqual(1, len([t for t in self.of("turn")
                                 if t.get("role") == "assistant"]))
        self.assertIsNotNone(self.output_text())


class TestConcurrentSubagents(_Base):

    def test_two_agents_are_attributed_independently(self):
        other_agent, other_instance = "b77e1c0f00000001", "TestWriter#2"
        runstate.put_agent_mapping(self.paths, _RUN_ID, other_agent, other_instance,
                                   "TestWriter")
        reply_two = json.dumps({"status_code": "BLOCKED", "status_message": "two"})
        self.handback_post()
        self.handback_post(message=reply_two, agent_id=other_agent, use_id="tu-hb-9")
        first = self.of("invocation_end", _INSTANCE)
        second = self.of("invocation_end", other_instance)
        self.assertEqual(1, len(first))
        self.assertEqual(1, len(second))
        self.assertEqual("SUCCESS", first[0]["status_code"])
        self.assertEqual("BLOCKED", second[0]["status_code"])
        self.assertEqual(reply_two, second[0]["response"])

    def test_one_agents_stop_is_not_suppressed_by_anothers_handback(self):
        other_agent, other_instance = "b77e1c0f00000002", "TestWriter#2"
        runstate.put_agent_mapping(self.paths, _RUN_ID, other_agent, other_instance,
                                   "TestWriter")
        self.handback_post()
        self.stop(message="plain reply", agent_id=other_agent)
        ends = self.of("invocation_end", other_instance)
        self.assertEqual(1, len(ends))
        self.assertEqual("plain reply", ends[0]["response"])


class TestResumedSubagent(_Base):

    def test_new_cycle_for_same_agent_id_is_not_suppressed(self):
        self.handback_post()
        invocation.handle_subagent_start(_ctx(
            self.tmp, "SubagentStart", _TS_STOP,
            agent_id=_AGENT_ID, agent_type="Reviewer"))
        self.stop(message="resumed cycle reply")
        resumed = self.of("invocation_end", "unknown-agent")
        self.assertEqual(1, len(resumed))
        self.assertEqual("resumed cycle reply", resumed[0]["response"])
        self.assertNotIn("completion_source", resumed[0])
        self.assertEqual(1, len(self.of("invocation_end", _INSTANCE)))


# ---------------------------------------------------------------------------
# Edge cases: explicit, reported outcomes
# ---------------------------------------------------------------------------

class TestEmptyAndNonJsonMessage(_Base):

    def test_empty_message_is_still_the_delivery(self):
        self.handback_post(message="")
        ends = self.of("invocation_end")
        self.assertEqual(1, len(ends))
        self.assertEqual(_TS_POST, ends[0]["timestamp"])
        self.assertNotIn("status_code", ends[0])
        self.assertNotIn("response", ends[0])
        self.assertIsNotNone(self.output_text())

    def test_empty_message_is_marked_and_reported(self):
        debug_before = self.debug_text()
        self.handback_post(message="")
        self.assertEqual("empty", self.of("invocation_end")[0]["response_format"])
        self.assertIn(_AGENT_ID, self.debug_text()[len(debug_before):])
        self.assertIn("empty", self.output_text().lower())

    def test_empty_message_produces_no_assistant_turn(self):
        self.handback_post(message="")
        self.assertEqual("handback", self.of("invocation_end")[0].get("completion_source"))
        self.assertEqual([], [t for t in self.of("turn") if t.get("role") == "assistant"])

    def test_whitespace_message_is_classified_empty(self):
        self.handback_post(message="  \n ")
        self.assertEqual("empty", self.of("invocation_end")[0]["response_format"])

    def test_non_json_message_is_the_delivery_without_status(self):
        self.handback_post(message="All done, nothing to report.")
        end = self.of("invocation_end")[0]
        self.assertEqual("All done, nothing to report.", end["response"])
        self.assertEqual("non_json", end["response_format"])
        self.assertNotIn("status_code", end)
        self.assertEqual(_TS_POST, end["timestamp"])

    def test_non_json_message_still_recorded_as_final_turn_and_reported(self):
        debug_before = self.debug_text()
        self.handback_post(message="All done, nothing to report.")
        turns = [t for t in self.of("turn") if t.get("role") == "assistant"]
        self.assertEqual("All done, nothing to report.", turns[0]["content"])
        self.assertIn(_AGENT_ID, self.debug_text()[len(debug_before):])
        self.assertIn("non_json", self.output_text())

    def test_later_stop_adds_nothing_after_empty_handback(self):
        self.handback_post(message="")
        self.stop()
        ends = self.of("invocation_end")
        self.assertEqual(1, len(ends))
        self.assertEqual("handback", ends[0].get("completion_source"))
        self.assertEqual("empty", ends[0].get("response_format"))


class TestMalformedHandbackPayload(_Base):

    def _assert_reported_and_not_completed(self, **overrides):
        debug_before = self.debug_text()
        try:
            self.handback_post(**overrides)
        except Exception as exc:
            self.fail(f"handler raised: {exc}")
        self.assertEqual([], self.of("invocation_end"))
        self.assertIsNone(self.output_text())
        self.assertEqual(1, len(self.of("tool_call_end")),
                         "the ordinary tool_call_end must still be written")
        self.assertIn(_AGENT_ID, self.debug_text()[len(debug_before):],
                      "the outcome must be reported, not silent")

    def test_missing_tool_input(self):
        self._assert_reported_and_not_completed(tool_input=_MISSING)

    def test_tool_input_not_an_object(self):
        self._assert_reported_and_not_completed(tool_input="just text")

    def test_missing_message_key(self):
        self._assert_reported_and_not_completed(tool_input={"other": 1})

    def test_non_string_message(self):
        self._assert_reported_and_not_completed(tool_input={"message": {"a": 1}})

    def test_stop_after_malformed_handback_completes_as_fallback(self):
        self.handback_post(tool_input=_MISSING)
        self.stop(message='{"status_code": "SUCCESS"}')
        ends = self.of("invocation_end")
        self.assertEqual(1, len(ends))
        self.assertEqual('{"status_code": "SUCCESS"}', ends[0]["response"])
        self.assertNotIn("completion_source", ends[0])

    def test_missing_agent_id_is_reported_and_not_completed(self):
        before = self.debug_text()
        try:
            self.handback_post(agent_id=_MISSING)
        except Exception as exc:
            self.fail(f"handler raised: {exc}")
        run_root = self.paths.run_root(_RUN_ID)
        every_event = []
        for f in run_root.rglob("*.jsonl"):
            every_event.extend(_read_jsonl(f))
        self.assertEqual([], [e for e in every_event if e["event"] == "invocation_end"])
        self.assertEqual([], list(run_root.rglob("02_output.md")))
        added = self.debug_text()[len(before):]
        self.assertRegex(added.lower(), r"hand[-_ ]?back")


class TestUnmappedHandback(unittest.TestCase):

    _UNMAPPED = "ffff0000unmapped1"

    def setUp(self):
        self._td = tempfile.TemporaryDirectory()
        self.tmp = self._td.name
        self.paths = core.build_paths(pathlib.Path(self.tmp))
        self.debug_file = pathlib.Path(self.tmp) / "debug.log"
        self._orig_debug = os.environ.get("MOSAIC_LOGGER_DEBUG")
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(self.debug_file)

    def tearDown(self):
        if self._orig_debug is None:
            os.environ.pop("MOSAIC_LOGGER_DEBUG", None)
        else:
            os.environ["MOSAIC_LOGGER_DEBUG"] = self._orig_debug
        self._td.cleanup()

    def _handback(self):
        tools.handle_pre_tool_use(_ctx(
            self.tmp, "PreToolUse", _TS_PRE, agent_id=self._UNMAPPED,
            tool_name="SubagentHandback", tool_use_id="tu-u-1",
            tool_input={"message": _REPLY}))
        tools.handle_post_tool_use(_ctx(
            self.tmp, "PostToolUse", _TS_POST, agent_id=self._UNMAPPED,
            tool_name="SubagentHandback", tool_use_id="tu-u-1",
            tool_input={"message": _REPLY}, tool_response=_ACK))

    def _unmapped_events(self):
        return _read_jsonl(self.paths.run_root(_RUN_ID)
                           / f"unmapped_{self._UNMAPPED}" / "03_events.jsonl")

    def test_ordinary_tool_events_use_unmapped_routing(self):
        self._handback()
        names = [e["event"] for e in self._unmapped_events()]
        self.assertEqual(["tool_call_start", "tool_call_end"], names)

    def test_no_completion_and_no_output_artifact(self):
        self._handback()
        self.assertEqual([], [e for e in self._unmapped_events()
                              if e["event"] in ("invocation_end", "turn")])
        run_root = self.paths.run_root(_RUN_ID)
        self.assertEqual([], list(run_root.rglob("02_output.md")))

    def test_no_completion_state_is_written(self):
        self._handback()
        self.assertIsNone(runstate.get_completion_claim(
            self.paths, _RUN_ID, self._UNMAPPED))

    def test_reported_with_agent_id(self):
        self._handback()
        text = self.debug_file.read_text("utf-8")
        self.assertIn(self._UNMAPPED, text)
        self.assertRegex(text.lower(), r"hand[-_ ]?back")

    def test_later_stop_still_quarantines_when_genuine(self):
        self._handback()
        invocation.handle_subagent_stop(_ctx(
            self.tmp, "SubagentStop", _TS_STOP, agent_id=self._UNMAPPED,
            last_assistant_message="genuine but unmapped"))
        events = _read_jsonl(self.paths.quarantine_events(_RUN_ID, self._UNMAPPED))
        self.assertEqual(1, len([e for e in events if e["event"] == "invocation_end"]))
        self.assertTrue(self.paths.quarantine_output(_RUN_ID, self._UNMAPPED).exists())

    def test_later_spurious_stop_is_still_discarded(self):
        self._handback()
        invocation.handle_subagent_stop(_ctx(
            self.tmp, "SubagentStop", _TS_STOP, agent_id=self._UNMAPPED))
        self.assertFalse(
            self.paths.quarantine_events(_RUN_ID, self._UNMAPPED).exists())


class TestDispatcherReportsUnusablePayloads(unittest.TestCase):
    """Unparseable hook stdin is reported explicitly (never a silent drop)."""

    def setUp(self):
        self._td = tempfile.TemporaryDirectory()
        self.debug_file = pathlib.Path(self._td.name) / "debug.log"
        self._orig_debug = os.environ.get("MOSAIC_LOGGER_DEBUG")
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(self.debug_file)
        self._orig_dir = os.environ.get("CLAUDE_PROJECT_DIR")
        os.environ["CLAUDE_PROJECT_DIR"] = self._td.name

    def tearDown(self):
        for name, orig in (("MOSAIC_LOGGER_DEBUG", self._orig_debug),
                           ("CLAUDE_PROJECT_DIR", self._orig_dir)):
            if orig is None:
                os.environ.pop(name, None)
            else:
                os.environ[name] = orig
        self._td.cleanup()

    def _text(self):
        return self.debug_file.read_text("utf-8") if self.debug_file.exists() else ""

    def test_invalid_json_is_reported(self):
        mosaic_logger.dispatch("{not json at all")
        self.assertNotEqual("", self._text().strip())

    def test_empty_input_is_reported(self):
        mosaic_logger.dispatch("")
        self.assertNotEqual("", self._text().strip())

    def test_non_object_json_is_reported(self):
        mosaic_logger.dispatch("[1, 2, 3]")
        self.assertNotEqual("", self._text().strip())

    def test_invalid_json_and_non_object_are_distinguishable(self):
        mosaic_logger.dispatch("{not json at all")
        first = self._text()
        mosaic_logger.dispatch("[1, 2, 3]")
        second = self._text()[len(first):]
        stamp = r"^\S+\s+"
        self.assertNotEqual(re.sub(stamp, "", first.strip()),
                            re.sub(stamp, "", second.strip()))


    def test_reporting_does_not_raise(self):
        for raw in ("{bad", "", "null", "[1]", '"s"'):
            try:
                mosaic_logger.dispatch(raw)
            except Exception as exc:
                self.fail(f"dispatch raised for {raw!r}: {exc}")


if __name__ == "__main__":
    unittest.main()
