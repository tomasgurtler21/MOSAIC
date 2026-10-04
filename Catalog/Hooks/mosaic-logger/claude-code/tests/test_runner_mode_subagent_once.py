"""Exactly-once guarantees for the Runner subagent role.

Hook firings can repeat (Stop and SessionEnd both carrying end data), arrive in
an unexpected order, or belong to a resumed session that reuses the instance id.
Each (instance id, session id) pair must yield exactly one invocation_start and
one invocation_end.
"""

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from runner_mode_support import (
    INSTANCE_ID, RUN_ID, SESSION_ID, WorkspaceTestCase, assistant_record, fire,
    runner_env, write_transcript,
)

PROMPT = "Do the thing."
RESPONSE = '{"status_code": "SUCCESS"}'


class OnceCase(WorkspaceTestCase):

    def setUp(self):
        super().setUp()
        self.transcript = write_transcript(
            self.workspace, [assistant_record("done")])
        self._env = runner_env(self.workspace, role="subagent", run_id=RUN_ID,
                               instance_id=INSTANCE_ID)
        self._env.__enter__()
        self.addCleanup(self._env.__exit__, None, None, None)

    def prompt(self, session_id=SESSION_ID):
        fire(self.workspace, "UserPromptSubmit", user_prompt=PROMPT,
             session_id=session_id, transcript_path=self.transcript)

    def stop(self, message=RESPONSE, session_id=SESSION_ID):
        fire(self.workspace, "Stop", last_assistant_message=message,
             session_id=session_id, transcript_path=self.transcript)

    def end(self, session_id=SESSION_ID, **extra):
        fire(self.workspace, "SessionEnd", reason="other",
             session_id=session_id, transcript_path=self.transcript, **extra)

    def count(self, name):
        return len([e for e in self.invocation_events() if e["event"] == name])


class TestRepeatedFirings(OnceCase):

    def test_repeated_stop_and_session_end_emit_one_invocation_end(self):
        self.prompt()
        self.stop()
        self.stop()
        self.end()
        self.end()
        self.assertEqual(1, self.count("invocation_end"))

    def test_session_end_carrying_response_after_stop_does_not_duplicate(self):
        self.prompt()
        self.stop()
        self.end(last_assistant_message=RESPONSE)
        self.assertEqual(1, self.count("invocation_end"))
        self.assertEqual(1, len([e for e in self.invocation_events()
                                 if e["event"] == "turn"
                                 and e["role"] == "assistant"]))

    def test_repeated_prompt_submission_emits_one_start_and_one_user_turn(self):
        self.prompt()
        self.prompt()
        self.assertEqual(1, self.count("invocation_start"))
        self.assertEqual(1, self.count("turn"))

    def test_repeated_session_end_does_not_repeat_usage_records(self):
        self.prompt()
        self.stop()
        self.end()
        self.end()
        self.assertEqual(1, self.count("usage_record"))

    def test_last_stop_message_wins(self):
        self.prompt()
        self.stop('{"status_code": "BLOCKED"}')
        self.stop('{"status_code": "SUCCESS"}')
        self.end()
        end = [e for e in self.invocation_events()
               if e["event"] == "invocation_end"][0]
        self.assertEqual("SUCCESS", end["status_code"])


class TestUnexpectedOrder(OnceCase):

    def test_session_end_before_stop_completes_once_and_late_stop_adds_nothing(self):
        self.prompt()
        self.end()
        self.stop()
        self.assertEqual(1, self.count("invocation_end"))
        self.assertEqual(1, self.count("invocation_start"))

    def test_session_end_without_prompt_emits_start_first_without_prompt(self):
        self.stop()
        self.end()
        events = self.invocation_events()
        self.assertEqual("invocation_start", events[0]["event"])
        self.assertNotIn("prompt", events[0])
        self.assertEqual(INSTANCE_ID, events[0]["agent_instance_id"])
        self.assertEqual(1, self.count("invocation_end"))

    def test_late_prompt_after_completion_adds_nothing(self):
        self.prompt()
        self.stop()
        self.end()
        before = len(self.invocation_events())
        self.assertEqual(1, self.count("invocation_end"))
        self.prompt()
        self.assertEqual(before, len(self.invocation_events()))


class TestResumedInvocation(OnceCase):
    """Same instance id, new harness session id: not blocked by stale claims."""

    def test_resumed_session_gets_its_own_start_and_end(self):
        self.prompt("session-A")
        self.stop(session_id="session-A")
        self.end("session-A")
        self.prompt("session-B")
        self.stop(session_id="session-B")
        self.end("session-B")
        self.assertEqual(2, self.count("invocation_start"))
        self.assertEqual(2, self.count("invocation_end"))

    def test_repeated_firings_of_resumed_session_still_dedupe(self):
        self.prompt("session-A")
        self.end("session-A")
        self.prompt("session-B")
        self.end("session-B")
        self.end("session-B")
        self.assertEqual(2, self.count("invocation_end"))

    def test_each_resumed_session_exports_its_own_usage_records(self):
        self.prompt("session-A")
        self.end("session-A")
        self.prompt("session-B")
        self.end("session-B")
        sessions = [e.get("session_id") for e in self.invocation_events()
                    if e["event"] == "usage_record"]
        self.assertEqual(["session-A", "session-B"], sessions)


if __name__ == "__main__":
    unittest.main()
