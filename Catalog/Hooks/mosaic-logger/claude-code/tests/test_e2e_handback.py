"""End-to-end tests for hand-back completion through the dispatcher.

Drives mosaic_logger.dispatch() (in-process) with harness-shaped payloads:
SubagentStart, the SubagentHandback PreToolUse/PostToolUse pair,
post-hand-back text, and SubagentStop. The hand-back/stop race is exercised
with two in-process threads and a serialized, patched append. Asserts the
final 03_events.jsonl and 02_output.md.
"""

import json
import os
import pathlib
import sys
import tempfile
import threading
import time
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger
import mosaic_logger_core as core
import mosaic_logger_runstate as runstate

_RUN_ID = "20260909T090000Z-4321"
_SESSION_ID = "e2e-handback-session"
_AGENT_ID = "a24d6c04de2a3dec4"
_INSTANCE = "TestWriter#5"
_OTHER_AGENT_ID = "b31f5e9a77c0d112"
_OTHER_INSTANCE = "Research#6"

_REPLY = json.dumps({
    "agent_instance_id": _INSTANCE, "run_id": _RUN_ID,
    "status_code": "SUCCESS", "status_message": "Stub delivered.",
})
_ACK = {"success": True, "message": "Report delivered to your caller."}


def _start_payload(agent_id=_AGENT_ID, instance=_INSTANCE, agent_type="TestWriter"):
    return {
        "hook_event_name": "SubagentStart", "session_id": _SESSION_ID,
        "agent_id": agent_id, "agent_type": agent_type,
        "agent_prompt": json.dumps({"agent_instance_id": instance, "run_id": _RUN_ID}),
    }


def _handback_payloads(session_path, agent_id=_AGENT_ID, message=_REPLY, use_id="tu-hb-1"):
    common = {"session_id": _SESSION_ID, "agent_id": agent_id,
              "agent_type": "TestWriter", "permission_mode": "auto",
              "tool_name": "SubagentHandback", "tool_use_id": use_id,
              "tool_input": {"message": message},
              "transcript_path": str(session_path)}
    pre = {"hook_event_name": "PreToolUse", **common}
    post = {"hook_event_name": "PostToolUse", **common,
            "tool_response": _ACK, "duration_ms": 60}
    return pre, post


def _stop_payload(session_path, agent_transcript_path=None, agent_id=_AGENT_ID,
                  message="Post hand-back chatter."):
    p = {"hook_event_name": "SubagentStop", "session_id": _SESSION_ID,
         "agent_id": agent_id, "agent_type": "TestWriter",
         "last_assistant_message": message,
         "transcript_path": str(session_path)}
    if agent_transcript_path:
        p["agent_transcript_path"] = str(agent_transcript_path)
    return p


def _assistant(msg_id, model="claude-opus-4-5"):
    return {"type": "assistant", "message": {
        "id": msg_id, "model": model,
        "usage": {"input_tokens": 120, "output_tokens": 40}}}


def _write_agent_transcript(session_path, agent_id, records):
    base = str(session_path)[:-len(".jsonl")]
    target = pathlib.Path(base) / "subagents" / f"agent-{agent_id}.jsonl"
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text("".join(json.dumps(r) + "\n" for r in records), encoding="utf-8")
    return target


def _read_jsonl(path):
    path = pathlib.Path(path)
    if not path.exists():
        return []
    return [json.loads(ln) for ln in path.read_text("utf-8").splitlines() if ln.strip()]


class _WorkspaceCase(unittest.TestCase):

    def setUp(self):
        self._td = tempfile.TemporaryDirectory()
        self.workspace = pathlib.Path(self._td.name) / "ws"
        self.workspace.mkdir()
        self.paths = core.build_paths(self.workspace)
        self.session_path = pathlib.Path(self._td.name) / "claude" / "sess-e2e.jsonl"
        self.session_path.parent.mkdir(parents=True)
        self.session_path.write_text("", encoding="utf-8")
        self._orig_dir = os.environ.get("CLAUDE_PROJECT_DIR")
        os.environ["CLAUDE_PROJECT_DIR"] = str(self.workspace)

    def tearDown(self):
        if self._orig_dir is None:
            os.environ.pop("CLAUDE_PROJECT_DIR", None)
        else:
            os.environ["CLAUDE_PROJECT_DIR"] = self._orig_dir
        self._td.cleanup()

    def replay(self, *payloads):
        for p in payloads:
            mosaic_logger.dispatch(json.dumps(p))

    def events(self, instance=_INSTANCE):
        return _read_jsonl(self.paths.invocation_events(_RUN_ID, instance))

    def names(self, instance=_INSTANCE):
        return [e["event"] for e in self.events(instance)]

    def output_text(self, instance=_INSTANCE):
        p = self.paths.invocation_output(_RUN_ID, instance)
        return p.read_text("utf-8") if p.exists() else None


class TestHandbackSessionFlow(_WorkspaceCase):

    def _full_flow(self, gap_before_stop=0.0):
        agent_tp = _write_agent_transcript(self.session_path, _AGENT_ID, [_assistant("m1")])
        pre, post = _handback_payloads(self.session_path)
        self.replay(_start_payload(), pre, post)
        if gap_before_stop:
            time.sleep(gap_before_stop)
        self.replay(_stop_payload(self.session_path, agent_tp))
        return agent_tp

    def test_exactly_one_invocation_end_with_handback_reply(self):
        self._full_flow()
        ends = [e for e in self.events() if e["event"] == "invocation_end"]
        self.assertEqual(1, len(ends))
        self.assertEqual(_REPLY, ends[0]["response"])
        self.assertEqual("SUCCESS", ends[0]["status_code"])
        self.assertEqual("claude-opus-4-5", ends[0]["model"])
        self.assertEqual("handback", ends[0]["completion_source"])

    def test_invocation_end_is_at_handback_post_tool_use_time(self):
        self._full_flow(gap_before_stop=0.05)
        events = self.events()
        end = next(e for e in events if e["event"] == "invocation_end")
        post_end = next(e for e in events if e["event"] == "tool_call_end")
        pre_start = next(e for e in events if e["event"] == "tool_call_start")
        self.assertEqual(post_end["timestamp"], end["timestamp"])
        self.assertLessEqual(pre_start["timestamp"], end["timestamp"])

    def test_event_sequence_has_one_completion_turn_and_no_post_handback_turn(self):
        self._full_flow()
        events = self.events()
        assistant_turns = [e for e in events
                           if e["event"] == "turn" and e.get("role") == "assistant"]
        self.assertEqual(1, len(assistant_turns))
        self.assertEqual(_REPLY, assistant_turns[0]["content"])
        self.assertNotIn("Post hand-back chatter.", json.dumps(events))

    def test_output_artifact_carries_handback_reply_and_status(self):
        self._full_flow()
        text = self.output_text()
        self.assertIn("Stub delivered.", text)
        self.assertIn("SUCCESS", text)
        self.assertNotIn("Post hand-back chatter.", text)

    def test_usage_recorded_once_and_export_still_written(self):
        agent_tp = self._full_flow()
        usage = [e for e in self.events() if e["event"] == "usage_record"]
        self.assertEqual(["m1"], [u["record_id"] for u in usage])
        self.assertEqual(agent_tp.read_bytes(),
                         self.paths.invocation_raw(_RUN_ID, _INSTANCE).read_bytes())

    def test_no_quarantine_and_no_unmapped_folder(self):
        self._full_flow()
        self.assertFalse(self.paths.quarantine_dir(_RUN_ID).exists())
        run_root = self.paths.run_root(_RUN_ID)
        self.assertEqual([], [d for d in run_root.iterdir()
                              if d.name.startswith("unmapped_")])

    def test_handback_without_stop_is_complete(self):
        pre, post = _handback_payloads(self.session_path)
        self.replay(_start_payload(), pre, post)
        self.assertEqual(1, self.names().count("invocation_end"))
        self.assertIn("Stub delivered.", self.output_text())

    def test_two_subagents_each_complete_with_their_own_reply(self):
        reply_two = json.dumps({"status_code": "BLOCKED", "status_message": "two"})
        pre1, post1 = _handback_payloads(self.session_path)
        pre2, post2 = _handback_payloads(
            self.session_path, agent_id=_OTHER_AGENT_ID, message=reply_two, use_id="tu-hb-2")
        self.replay(
            _start_payload(),
            _start_payload(_OTHER_AGENT_ID, _OTHER_INSTANCE, "Research"),
            pre1, pre2, post2, post1,
            _stop_payload(self.session_path, agent_id=_OTHER_AGENT_ID),
            _stop_payload(self.session_path))
        first = [e for e in self.events() if e["event"] == "invocation_end"]
        second = [e for e in self.events(_OTHER_INSTANCE) if e["event"] == "invocation_end"]
        self.assertEqual(1, len(first))
        self.assertEqual(1, len(second))
        self.assertEqual("SUCCESS", first[0]["status_code"])
        self.assertEqual("BLOCKED", second[0]["status_code"])
        self.assertEqual(reply_two, second[0]["response"])

    def test_stop_without_handback_is_unchanged(self):
        self.replay(_start_payload(),
                    _stop_payload(self.session_path, message='{"status_code": "BLOCKED"}'))
        ends = [e for e in self.events() if e["event"] == "invocation_end"]
        self.assertEqual(1, len(ends))
        self.assertEqual("BLOCKED", ends[0]["status_code"])
        self.assertNotIn("completion_source", ends[0])

    def test_lagging_agent_transcript_leaves_model_and_tokens_out(self):
        pre, post = _handback_payloads(self.session_path)
        self.replay(_start_payload(), pre, post)  # agent transcript not written yet
        agent_tp = _write_agent_transcript(self.session_path, _AGENT_ID, [_assistant("m9")])
        self.replay(_stop_payload(self.session_path, agent_tp))
        ends = [e for e in self.events() if e["event"] == "invocation_end"]
        self.assertEqual(1, len(ends))
        self.assertNotIn("model", ends[0])
        self.assertNotIn("token_usage", ends[0])

    def test_resumed_cycle_after_handback_is_completed_by_its_stop(self):
        pre, post = _handback_payloads(self.session_path)
        resumed_start = {
            "hook_event_name": "SubagentStart", "session_id": _SESSION_ID,
            "agent_id": _AGENT_ID, "agent_type": "TestWriter"}
        self.replay(_start_payload(), pre, post, resumed_start,
                    _stop_payload(self.session_path, message="resumed reply"))
        resumed = [e for e in self.events("unknown-agent")
                   if e["event"] == "invocation_end"]
        self.assertEqual(1, len(resumed))
        self.assertEqual("resumed reply", resumed[0]["response"])
        self.assertEqual(1, self.names().count("invocation_end"))


class TestHandbackEdgeCasesEndToEnd(_WorkspaceCase):

    def test_empty_message_end_to_end(self):
        pre, post = _handback_payloads(self.session_path, message="")
        self.replay(_start_payload(), pre, post, _stop_payload(self.session_path))
        ends = [e for e in self.events() if e["event"] == "invocation_end"]
        self.assertEqual(1, len(ends))
        self.assertEqual("empty", ends[0]["response_format"])
        self.assertNotIn("status_code", ends[0])
        self.assertIn("empty", self.output_text().lower())

    def test_non_json_message_end_to_end(self):
        pre, post = _handback_payloads(self.session_path, message="plain words")
        self.replay(_start_payload(), pre, post, _stop_payload(self.session_path))
        ends = [e for e in self.events() if e["event"] == "invocation_end"]
        self.assertEqual(1, len(ends))
        self.assertEqual("non_json", ends[0]["response_format"])
        self.assertEqual("plain words", ends[0]["response"])

    def test_unmapped_handback_then_stop_ends_in_quarantine(self):
        pre, post = _handback_payloads(self.session_path, agent_id="ghost-agent-1")
        stop = _stop_payload(self.session_path, agent_id="ghost-agent-1",
                             message="genuine unmapped")
        self.replay(_start_payload(), pre, post, stop)
        q_events = _read_jsonl(self.paths.quarantine_events(_RUN_ID, "ghost-agent-1"))
        self.assertEqual(1, len([e for e in q_events if e["event"] == "invocation_end"]))
        self.assertEqual(0, self.names().count("invocation_end"))


class TestHandbackRacesSubagentStop(_WorkspaceCase):
    """The hand-back PostToolUse and SubagentStop hooks may run at the same
    time; whichever wins, exactly one completion results.

    The two hooks are dispatched from two threads released together by a
    barrier. Event-file appends are serialized by a test lock so that the
    shared unlocked append cannot corrupt the file; the completion claim
    is what is under test.
    """

    _ROUNDS = 8

    def setUp(self):
        super().setUp()
        self._append_lock = threading.Lock()
        self._real_append = core.append_event

        def locked_append(path, event):
            with self._append_lock:
                self._real_append(path, event)

        core.append_event = locked_append

    def tearDown(self):
        core.append_event = self._real_append
        super().tearDown()

    def _dispatch_together(self, payloads):
        barrier = threading.Barrier(len(payloads))
        errors = []

        def run(payload):
            try:
                barrier.wait(timeout=10)
                mosaic_logger.dispatch(json.dumps(payload))
            except Exception as exc:  # dispatcher must never raise
                errors.append(exc)

        threads = [threading.Thread(target=run, args=(p,)) for p in payloads]
        for t in threads:
            t.start()
        for t in threads:
            t.join(timeout=60)
        return errors

    def test_exactly_one_completion_when_hooks_interleave(self):
        for round_no in range(self._ROUNDS):
            with self.subTest(round=round_no):
                run_id_dir = self.paths.run_root(_RUN_ID)
                if run_id_dir.exists():
                    import shutil
                    shutil.rmtree(run_id_dir)
                self.replay(_start_payload())
                _pre, post = _handback_payloads(self.session_path)
                stop = _stop_payload(self.session_path, message="stop-side reply")
                order = (post, stop) if round_no % 2 == 0 else (stop, post)

                errors = self._dispatch_together(order)

                self.assertEqual([], errors)
                events = self.events()
                ends = [e for e in events if e["event"] == "invocation_end"]
                self.assertEqual(1, len(ends), "exactly one invocation_end")
                turns = [e for e in events
                         if e["event"] == "turn" and e.get("role") == "assistant"]
                self.assertEqual(1, len(turns), "exactly one final assistant turn")
                output = self.output_text()
                claim = runstate.get_completion_claim(self.paths, _RUN_ID, _AGENT_ID)
                self.assertIsNotNone(claim, "the winning completion must leave a claim")
                if claim["source"] == runstate.CLAIM_SOURCE_HANDBACK:
                    self.assertEqual("handback", ends[0].get("completion_source"))
                    self.assertEqual(_REPLY, ends[0]["response"])
                    self.assertEqual("SUCCESS", ends[0]["status_code"])
                    self.assertNotIn("stop-side reply", output)
                else:
                    self.assertEqual(runstate.CLAIM_SOURCE_SUBAGENT_STOP, claim["source"])
                    self.assertEqual("stop-side reply", ends[0]["response"])
                    self.assertEqual("stop-side reply", turns[0]["content"])
                self.assertFalse(self.paths.quarantine_dir(_RUN_ID).exists())


if __name__ == "__main__":
    unittest.main()
