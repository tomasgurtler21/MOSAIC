"""Tests for a Runner subagent-role session (MOSAIC_ROLE=subagent).

The whole primary session is one invocation.  A simulated session (SessionStart,
UserPromptSubmit, tool calls, Stop, SessionEnd) must produce the same invocation
folder a native subagent invocation produces, and nothing in the run-level
orchestrator stream.
"""

import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from runner_mode_support import (
    INSTANCE_ID, NATIVE_INVOCATION_EVENT_TYPES, RUN_ID, SESSION_ID,
    WorkspaceTestCase, assistant_record, fire, runner_env, user_record,
    write_transcript,
)

PROMPT = "Research the retry policy and report back."
RESPONSE = '{"status_code": "BLOCKED", "status_message": "needs upstream input"}'


class SubagentSessionCase(WorkspaceTestCase):
    """Base: plays one complete subagent-role session in setUp."""

    def setUp(self):
        super().setUp()
        self.transcript = write_transcript(self.workspace, [
            user_record(PROMPT),
            assistant_record("Looking into it.", message_id="msg_1",
                             input_tokens=120, output_tokens=40),
            assistant_record(RESPONSE, message_id="msg_2",
                             input_tokens=300, output_tokens=60),
        ])
        self.play_session()

    def env(self):
        return runner_env(self.workspace, role="subagent", run_id=RUN_ID,
                          instance_id=INSTANCE_ID)

    def play_session(self):
        with self.env():
            fire(self.workspace, "SessionStart", source="startup",
                 model="claude-opus-4-5", transcript_path=self.transcript)
            fire(self.workspace, "UserPromptSubmit", user_prompt=PROMPT,
                 transcript_path=self.transcript)
            fire(self.workspace, "PreToolUse", tool_name="Read",
                 tool_use_id="call-1", tool_input={"file_path": "a.txt"},
                 transcript_path=self.transcript)
            fire(self.workspace, "PostToolUse", tool_name="Read",
                 tool_use_id="call-1", tool_response="contents",
                 transcript_path=self.transcript)
            fire(self.workspace, "Stop", last_assistant_message=RESPONSE,
                 transcript_path=self.transcript)
            fire(self.workspace, "SessionEnd", reason="other",
                 transcript_path=self.transcript)

    def of_type(self, name):
        return [e for e in self.invocation_events() if e["event"] == name]


class TestSubagentInvocationFolder(SubagentSessionCase):
    """The invocation folder holds the native file set."""

    def test_invocation_folder_has_the_four_artifacts(self):
        folder = self.paths.invocation_dir(RUN_ID, INSTANCE_ID)
        for name in ("01_input.md", "02_output.md", "03_events.jsonl",
                     "04_session.raw", "04_session.meta.json"):
            self.assertTrue((folder / name).exists(), name)

    def test_input_file_contains_the_prompt(self):
        text = self.paths.invocation_input(RUN_ID, INSTANCE_ID).read_text("utf-8")
        self.assertIn(PROMPT, text)

    def test_output_file_contains_the_response(self):
        text = self.paths.invocation_output(RUN_ID, INSTANCE_ID).read_text("utf-8")
        self.assertIn("needs upstream input", text)

    def test_raw_transcript_is_the_final_transcript(self):
        raw = self.paths.invocation_raw(RUN_ID, INSTANCE_ID).read_bytes()
        self.assertIn(b"msg_2", raw)

    def test_nothing_is_written_to_the_orchestrator_stream(self):
        self.assertFalse(self.paths.orchestrator_events(RUN_ID).exists())

    def test_run_state_lives_only_in_dot_prefixed_directories(self):
        entries = [p.name for p in self.paths.run_root(RUN_ID).iterdir()]
        extras = [n for n in entries
                  if not n.startswith(".") and n != self.paths.invocation_dir(
                      RUN_ID, INSTANCE_ID).name]
        self.assertEqual([], extras)
        state = self.paths.run_root(RUN_ID) / ".runner-session"
        self.assertTrue(state.is_dir())
        self.assertTrue(any(state.iterdir()))


class TestSubagentEventStream(SubagentSessionCase):
    """03_events.jsonl carries exactly the native invocation event types."""

    def test_event_types_are_exactly_the_native_invocation_set(self):
        types = {e["event"] for e in self.invocation_events()}
        self.assertEqual(NATIVE_INVOCATION_EVENT_TYPES, types)

    def test_no_run_or_session_lifecycle_events(self):
        self.assertTrue(self.invocation_events())
        for event in self.invocation_events():
            self.assertNotIn(event["event"], (
                "run_start", "run_end", "session_start", "session_end"))

    def test_exactly_one_invocation_start_and_one_invocation_end(self):
        self.assertEqual(1, len(self.of_type("invocation_start")))
        self.assertEqual(1, len(self.of_type("invocation_end")))

    def test_invocation_start_is_first_and_carries_prompt_and_identity(self):
        first = self.invocation_events()[0]
        self.assertEqual("invocation_start", first["event"])
        self.assertEqual(PROMPT, first["prompt"])
        self.assertEqual(INSTANCE_ID, first["agent_instance_id"])
        self.assertEqual("Research", first["agent_type"])

    def test_initial_user_turn_follows_invocation_start(self):
        second = self.invocation_events()[1]
        self.assertEqual("turn", second["event"])
        self.assertEqual("user", second["role"])
        self.assertEqual(PROMPT, second["content"])

    def test_final_assistant_turn_carries_the_response(self):
        turns = self.of_type("turn")
        self.assertEqual(2, len(turns))
        self.assertEqual("assistant", turns[-1]["role"])
        self.assertEqual(RESPONSE, turns[-1]["content"])

    def test_tool_call_pair_is_in_the_invocation_stream(self):
        self.assertEqual(1, len(self.of_type("tool_call_start")))
        self.assertEqual(1, len(self.of_type("tool_call_end")))

    def test_session_id_is_in_every_envelope(self):
        self.assertTrue(self.invocation_events())
        for event in self.invocation_events():
            self.assertEqual(SESSION_ID, event.get("session_id"), event["event"])


class TestSubagentInvocationEnd(SubagentSessionCase):
    """invocation_end is populated as a native invocation_end is."""

    def test_status_code_is_extracted_from_the_response(self):
        self.assertEqual("BLOCKED", self.of_type("invocation_end")[0]["status_code"])

    def test_response_is_the_stop_message(self):
        self.assertEqual(RESPONSE, self.of_type("invocation_end")[0]["response"])

    def test_model_comes_from_the_final_transcript(self):
        self.assertEqual("claude-opus-4-5", self.of_type("invocation_end")[0]["model"])

    def test_token_usage_comes_from_the_final_transcript(self):
        usage = self.of_type("invocation_end")[0]["token_usage"]
        self.assertEqual(420, usage["input_tokens"])
        self.assertEqual(100, usage["output_tokens"])

    def test_completion_source_is_session_end(self):
        self.assertEqual("session_end",
                         self.of_type("invocation_end")[0]["completion_source"])

    def test_invocation_end_is_the_last_lifecycle_event(self):
        names = [e["event"] for e in self.invocation_events()
                 if e["event"] not in ("turn", "usage_record")]
        self.assertEqual("invocation_start", names[0])
        self.assertEqual("invocation_end", names[-1])

    def test_response_format_is_absent_for_a_well_formed_json_response(self):
        self.assertIsNone(
            self.of_type("invocation_end")[0].get("response_format"))

    def test_one_usage_record_per_transcript_assistant_record(self):
        records = self.of_type("usage_record")
        self.assertEqual(2, len(records))
        for record in records:
            self.assertEqual("agent_transcript", record["source"])


class TestSubagentExitSafety(WorkspaceTestCase):
    """Transcript-dependent work happens at SessionEnd, not at Stop."""

    def env(self):
        return runner_env(self.workspace, role="subagent", run_id=RUN_ID,
                          instance_id=INSTANCE_ID)

    def start_and_stop(self, transcript):
        fire(self.workspace, "UserPromptSubmit", user_prompt=PROMPT,
             transcript_path=transcript)
        fire(self.workspace, "Stop", last_assistant_message=RESPONSE,
             transcript_path=transcript)

    def test_stop_alone_does_not_complete_the_invocation(self):
        transcript = write_transcript(self.workspace, [assistant_record("x")])
        with self.env():
            self.start_and_stop(transcript)
        names = [e["event"] for e in self.invocation_events()]
        self.assertIn("invocation_start", names)
        self.assertNotIn("invocation_end", names)
        self.assertNotIn("usage_record", names)

    def test_tool_firings_do_not_emit_usage_records(self):
        transcript = write_transcript(self.workspace, [assistant_record("x")])
        with self.env():
            fire(self.workspace, "UserPromptSubmit", user_prompt=PROMPT,
                 transcript_path=transcript)
            fire(self.workspace, "PreToolUse", tool_name="Read",
                 tool_use_id="c1", tool_input={},
                 transcript_path=transcript)
            fire(self.workspace, "PostToolUse", tool_name="Read",
                 tool_use_id="c1", tool_response="ok",
                 transcript_path=transcript)
        names = [e["event"] for e in self.invocation_events()]
        self.assertIn("invocation_start", names)
        self.assertIn("tool_call_start", names)
        self.assertNotIn("usage_record", names)

    def test_transcript_growth_after_stop_is_captured_at_session_end(self):
        transcript = write_transcript(self.workspace, [assistant_record("one")])
        with self.env():
            self.start_and_stop(transcript)
            write_transcript(self.workspace, [
                assistant_record("one", message_id="msg_1"),
                assistant_record("two", message_id="msg_2")])
            fire(self.workspace, "SessionEnd", reason="other",
                 transcript_path=transcript)
        self.assertEqual(2, len([e for e in self.invocation_events()
                                 if e["event"] == "usage_record"]))

    def test_unreadable_transcript_still_completes_without_model_or_usage(self):
        with self.env():
            self.start_and_stop(str(self.workspace / "missing.jsonl"))
            fire(self.workspace, "SessionEnd", reason="other",
                 transcript_path=str(self.workspace / "missing.jsonl"))
        end = [e for e in self.invocation_events()
               if e["event"] == "invocation_end"]
        self.assertEqual(1, len(end))
        self.assertNotIn("model", end[0])
        self.assertNotIn("token_usage", end[0])

    def test_session_end_without_response_omits_final_assistant_turn(self):
        transcript = write_transcript(self.workspace, [assistant_record("x")])
        with self.env():
            fire(self.workspace, "UserPromptSubmit", user_prompt=PROMPT,
                 transcript_path=transcript)
            fire(self.workspace, "SessionEnd", reason="other",
                 transcript_path=transcript)
        turns = [e for e in self.invocation_events() if e["event"] == "turn"]
        self.assertEqual(["user"], [t["role"] for t in turns])
        self.assertEqual(1, len([e for e in self.invocation_events()
                                 if e["event"] == "invocation_end"]))


class TestSubagentNativeRouting(WorkspaceTestCase):
    """Tool and native-subagent events keep their native behaviour."""

    def env(self):
        return runner_env(self.workspace, role="subagent", run_id=RUN_ID,
                          instance_id=INSTANCE_ID)

    def test_failed_tool_call_is_routed_to_the_invocation_stream(self):
        with self.env():
            fire(self.workspace, "PreToolUse", tool_name="Bash",
                 tool_use_id="f1", tool_input={"command": "x"})
            fire(self.workspace, "PostToolUseFailure", tool_name="Bash",
                 tool_use_id="f1", error="boom")
        ends = [e for e in self.invocation_events()
                if e["event"] == "tool_call_end"]
        self.assertEqual(1, len(ends))
        self.assertEqual("error", ends[0]["status"])
        self.assertFalse(self.paths.orchestrator_events(RUN_ID).exists())

    def test_tool_firing_with_payload_agent_id_is_not_routed_by_env_instance(self):
        with self.env():
            fire(self.workspace, "PreToolUse", tool_name="Read",
                 tool_use_id="n1", tool_input={}, agent_id="native-agent-1")
        self.assertEqual([], self.invocation_events())

    def test_native_subagent_start_and_stop_do_not_use_the_runner_stream(self):
        with self.env():
            fire(self.workspace, "SubagentStart", agent_id="native-agent-1",
                 agent_type="Research", agent_prompt="inner task")
            fire(self.workspace, "SubagentStop", agent_id="native-agent-1",
                 last_assistant_message=RESPONSE)
        names = [e["event"] for e in self.invocation_events()]
        self.assertNotIn("invocation_end", names)


class TestSubagentResponseFormat(WorkspaceTestCase):

    def test_non_json_response_is_classified_in_invocation_end(self):
        transcript = write_transcript(self.workspace, [assistant_record("x")])
        with runner_env(self.workspace, role="subagent", run_id=RUN_ID,
                        instance_id=INSTANCE_ID):
            fire(self.workspace, "UserPromptSubmit", user_prompt=PROMPT,
                 transcript_path=transcript)
            fire(self.workspace, "Stop", last_assistant_message="plain text",
                 transcript_path=transcript)
            fire(self.workspace, "SessionEnd", reason="other",
                 transcript_path=transcript)
        end = [e for e in self.invocation_events()
               if e["event"] == "invocation_end"][0]
        self.assertEqual("non_json", end["response_format"])


class TestSubagentIgnoredEvents(WorkspaceTestCase):
    """Primary-session events with no native invocation counterpart write nothing."""

    def test_session_start_notification_and_compaction_write_nothing(self):
        with runner_env(self.workspace, role="subagent", run_id=RUN_ID,
                        instance_id=INSTANCE_ID):
            fire(self.workspace, "SessionStart", source="startup")
            fire(self.workspace, "Notification",
                 notification_type="permission_request", message="ok?")
            fire(self.workspace, "PreCompact", trigger="auto", tokens_before=9)
            fire(self.workspace, "PostCompact", tokens_after=3)
        self.assertEqual([], self.invocation_events())
        self.assertFalse(self.paths.orchestrator_events(RUN_ID).exists())


if __name__ == "__main__":
    unittest.main()
