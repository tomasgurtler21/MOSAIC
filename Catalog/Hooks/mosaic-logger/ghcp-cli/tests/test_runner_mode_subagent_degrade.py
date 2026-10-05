"""Subagent-role Runner mode: degrade-never-fabricate cases.

When the transcript path is missing, the file is absent, or the transcript is
unreadable or malformed, agentStop must still complete the invocation exactly
once with the fields that can honestly be resolved. Unresolvable fields are
omitted, no exception escapes, and no 04_session.raw is fabricated.

The no-raw rule is asserted only where no real transcript file exists (path
missing, empty, absent, directory). For malformed, undecodable and
no-assistant transcripts the file is real, so exporting it as 04_session.raw
is allowed (it is not a fabrication) and is deliberately not asserted either way.
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.runner_mode_support import (
    FIXTURES, INSTANCE_ID, PROMPT, RUN_ID, RunnerModeTestCase, event_types,
)

GHCP_EVENTS_TRANSCRIPT = FIXTURES / "runner_ghcp_events_transcript.jsonl"
_RESOLVABLE_ONLY_FROM_TRANSCRIPT = ("response", "status_code", "model", "token_usage")


class TestSubagentTranscriptDegradation(RunnerModeTestCase):

    def setUp(self):
        super().setUp()
        self.subagent_env()

    def finish_session(self, agent_stop_payload: dict):
        self.dispatch("userPromptSubmitted", {"prompt": PROMPT})
        self.dispatch("sessionStart")
        self.dispatch("agentStop", agent_stop_payload)
        self.dispatch("sessionEnd", {"reason": "complete"})

    def assert_degraded_completion(self, expect_no_raw=True):
        events = self.invocation_events()
        types = event_types(events)
        self.assertEqual(1, types.count("invocation_start"))
        self.assertEqual(1, types.count("invocation_end"))
        end = [e for e in events if e["event"] == "invocation_end"][0]
        self.assertEqual(INSTANCE_ID, end["agent_instance_id"])
        for field in _RESOLVABLE_ONLY_FROM_TRANSCRIPT:
            self.assertNotIn(field, end)
        self.assertNotIn("usage_record", types)
        self.assertFalse(self.paths.orchestrator_events(RUN_ID).exists())
        if expect_no_raw:
            self.assertFalse(self.paths.invocation_raw(RUN_ID, INSTANCE_ID).exists())

    def test_transcript_path_missing_from_payload(self):
        self.finish_session({"stopReason": "end_turn"})
        self.assert_degraded_completion()

    def test_transcript_path_empty_string(self):
        self.finish_session({"transcriptPath": ""})
        self.assert_degraded_completion()

    def test_transcript_file_absent(self):
        self.finish_session({"transcriptPath": str(self.workspace / "no_such_transcript.jsonl")})
        self.assert_degraded_completion()

    def test_transcript_path_is_a_directory_so_unreadable(self):
        self.finish_session({"transcriptPath": str(self.workspace)})
        self.assert_degraded_completion()

    def test_transcript_with_only_malformed_lines(self):
        bad = self.workspace / "bad.jsonl"
        bad.write_text("{not json\n\x00\x01garbage\n[1, 2\n", encoding="utf-8")
        self.finish_session({"transcriptPath": str(bad)})
        self.assert_degraded_completion(expect_no_raw=False)

    def test_transcript_with_undecodable_bytes(self):
        bad = self.workspace / "binary.jsonl"
        bad.write_bytes(b"\xff\xfe\x00\x80\x81\x82\n\xc3\x28\n")
        self.finish_session({"transcriptPath": str(bad)})
        self.assert_degraded_completion(expect_no_raw=False)

    def test_transcript_without_any_assistant_record(self):
        user_only = self.workspace / "user_only.jsonl"
        user_only.write_text('{"type":"user","message":{"content":"hi"}}\n', encoding="utf-8")
        self.finish_session({"transcriptPath": str(user_only)})
        self.assert_degraded_completion(expect_no_raw=False)

    def test_response_without_parseable_status_keeps_response_and_omits_status(self):
        plain = self.workspace / "plain.jsonl"
        plain.write_text(
            '{"type":"assistant","message":{"model":"m","content":"Just plain text, no JSON."}}\n',
            encoding="utf-8")
        self.finish_session({"transcriptPath": str(plain)})
        end = [e for e in self.invocation_events() if e["event"] == "invocation_end"][0]
        self.assertIn("Just plain text", end["response"])
        self.assertNotIn("status_code", end)

    def test_assistant_text_without_model_or_usage_keeps_response_only(self):
        events_shape = self.copy_transcript(GHCP_EVENTS_TRANSCRIPT, name="ghcp_events.jsonl")
        self.finish_session({"transcriptPath": str(events_shape)})
        ends = [e for e in self.invocation_events() if e["event"] == "invocation_end"]
        self.assertEqual(1, len(ends))
        self.assertIn("Final answer text", ends[0]["response"])
        self.assertNotIn("model", ends[0])
        self.assertNotIn("token_usage", ends[0])

    def test_no_assistant_turn_is_fabricated_without_a_response(self):
        self.finish_session({})
        roles = [e.get("role") for e in self.invocation_events() if e["event"] == "turn"]
        self.assertEqual(["user"], roles)

    def test_degraded_session_does_not_raise_out_of_dispatch(self):
        try:
            self.finish_session({"transcriptPath": 12345})
        except Exception as exc:  # noqa: BLE001
            self.fail(f"dispatch raised for a non-string transcript path: {exc}")
        self.assert_degraded_completion()


if __name__ == "__main__":
    import unittest
    unittest.main()
