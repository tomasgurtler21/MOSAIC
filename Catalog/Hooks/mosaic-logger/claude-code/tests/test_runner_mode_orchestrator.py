"""Tests for a Runner orchestrator-role session (MOSAIC_ROLE=orchestrator).

The session is a run-level script-orchestrator call: events go to the run's
00_orchestrator_events.jsonl as for a native primary session, but the Runner
owns run_start/run_end so the adapter never writes them.  The transcript is
exported under a session-scoped name so consultations never overwrite each other.
"""

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import mosaic_logger_core as core
from runner_mode_support import (
    RUN_ID, SESSION_ID, WorkspaceTestCase, assistant_record, fire, runner_env,
    read_jsonl, user_record, write_transcript,
)


def _scoped_paths(workspace):
    return core.build_paths(workspace, scope_orchestrator_transcript=True)


def _scoped_name(session_id):
    return ("00_orchestrator_session__"
            f"{core.transcript_scope_segment(core.HARNESS, session_id)}.raw")


class OrchestratorCase(WorkspaceTestCase):

    def consult(self, session_id, text, transcript_name=None):
        """Play one orchestrator session whose transcript holds `text`."""
        transcript = write_transcript(
            self.workspace,
            [user_record("go"), assistant_record(text, message_id=session_id)],
            name=transcript_name or f"{session_id}.jsonl")
        with runner_env(self.workspace, role="orchestrator", run_id=RUN_ID):
            fire(self.workspace, "SessionStart", source="startup",
                 session_id=session_id, transcript_path=transcript)
            fire(self.workspace, "UserPromptSubmit", user_prompt="go",
                 session_id=session_id, transcript_path=transcript)
            fire(self.workspace, "PreToolUse", tool_name="Bash",
                 tool_use_id="c1", tool_input={"command": "ls"},
                 session_id=session_id, transcript_path=transcript)
            fire(self.workspace, "PostToolUse", tool_name="Bash",
                 tool_use_id="c1", tool_response="ok",
                 session_id=session_id, transcript_path=transcript)
            fire(self.workspace, "Notification", message="hello",
                 notification_type="permission_request",
                 session_id=session_id, transcript_path=transcript)
            fire(self.workspace, "Stop", last_assistant_message=text,
                 session_id=session_id, transcript_path=transcript)
            fire(self.workspace, "SessionEnd", reason="other",
                 session_id=session_id, transcript_path=transcript)
        return transcript


class TestOrchestratorEvents(OrchestratorCase):

    def setUp(self):
        super().setUp()
        self.consult(SESSION_ID, "all done")
        self.names = [e["event"] for e in self.orchestrator_events()]

    def test_run_level_events_are_written(self):
        for name in ("session_start", "turn", "tool_call_start",
                     "tool_call_end", "notification", "session_end"):
            self.assertIn(name, self.names)

    def test_run_start_and_run_end_are_never_written(self):
        self.assertNotIn("run_start", self.names)
        self.assertNotIn("run_end", self.names)

    def test_one_session_start_and_one_session_end(self):
        self.assertEqual(1, self.names.count("session_start"))
        self.assertEqual(1, self.names.count("session_end"))

    def test_usage_records_are_emitted_for_the_transcript(self):
        self.assertIn("usage_record", self.names)

    def test_no_invocation_events_are_written(self):
        self.assertNotIn("invocation_start", self.names)
        self.assertNotIn("invocation_end", self.names)

    def test_only_the_run_folder_exists(self):
        self.assertEqual([RUN_ID], self.run_folders())


class TestOrchestratorTranscriptScoping(OrchestratorCase):

    def test_transcript_is_exported_under_the_session_scoped_name(self):
        self.consult(SESSION_ID, "all done")
        raw = _scoped_paths(self.workspace).orchestrator_raw(RUN_ID, SESSION_ID)
        self.assertEqual(_scoped_name(SESSION_ID), raw.name)
        self.assertTrue(raw.exists())

    def test_sidecar_sits_next_to_the_scoped_transcript(self):
        self.consult(SESSION_ID, "all done")
        raw = _scoped_paths(self.workspace).orchestrator_raw(RUN_ID, SESSION_ID)
        self.assertTrue(raw.with_suffix("").with_suffix(".meta.json").exists())

    def test_fixed_native_name_is_not_used_in_runner_mode(self):
        self.consult(SESSION_ID, "all done")
        fixed = self.paths.orchestrator_raw(RUN_ID, None)
        self.assertFalse(fixed.exists())

    def test_two_consecutive_sessions_keep_two_transcripts(self):
        self.consult("session-one", "first consultation")
        self.consult("session-two", "second consultation")
        scoped = _scoped_paths(self.workspace)
        first = scoped.orchestrator_raw(RUN_ID, "session-one")
        second = scoped.orchestrator_raw(RUN_ID, "session-two")
        self.assertNotEqual(first, second)
        self.assertIn(b"first consultation", first.read_bytes())
        self.assertIn(b"second consultation", second.read_bytes())

    def test_exported_transcript_is_final_at_session_end(self):
        transcript = self.consult(SESSION_ID, "early text")
        write_transcript(self.workspace, [
            assistant_record("early text", message_id="m1"),
            assistant_record("late text", message_id="m2")],
            name=os.path.basename(transcript))
        with runner_env(self.workspace, role="orchestrator", run_id=RUN_ID):
            fire(self.workspace, "SessionEnd", reason="other",
                 transcript_path=transcript)
        raw = _scoped_paths(self.workspace).orchestrator_raw(RUN_ID, SESSION_ID)
        self.assertIn(b"late text", raw.read_bytes())

    def test_repeated_session_end_does_not_duplicate_usage_records(self):
        transcript = self.consult(SESSION_ID, "all done")
        before = [e for e in self.orchestrator_events()
                  if e["event"] == "usage_record"]
        with runner_env(self.workspace, role="orchestrator", run_id=RUN_ID):
            fire(self.workspace, "SessionEnd", reason="other",
                 transcript_path=transcript)
        after = [e for e in self.orchestrator_events()
                 if e["event"] == "usage_record"]
        self.assertEqual(len(before), len(after))


class TestOrchestratorCompaction(OrchestratorCase):

    def test_compaction_events_are_written_to_the_orchestrator_stream(self):
        with runner_env(self.workspace, role="orchestrator", run_id=RUN_ID):
            fire(self.workspace, "PreCompact", trigger="auto", tokens_before=9)
            fire(self.workspace, "PostCompact", tokens_after=3)
        names = [e["event"] for e in self.orchestrator_events()]
        self.assertTrue(any("compact" in n for n in names))
        self.assertNotIn("run_start", names)


class TestOrchestratorRunIdHandling(OrchestratorCase):

    def test_invalid_run_id_still_honours_the_role(self):
        with runner_env(self.workspace, role="orchestrator",
                        run_id="not a valid run id"):
            fire(self.workspace, "SessionStart", source="startup")
            fire(self.workspace, "SessionEnd", reason="other")
        events = []
        root = self.paths.root
        for folder in self.run_folders():
            events += read_jsonl(root / folder / "00_orchestrator_events.jsonl")
        names = [e["event"] for e in events]
        self.assertIn("session_start", names)
        self.assertNotIn("run_start", names)
        self.assertNotIn("run_end", names)


class TestOrchestratorSessionsKeepSeparateUsage(OrchestratorCase):

    def test_each_session_contributes_its_own_usage_records(self):
        self.consult("session-one", "first consultation")
        after_first = len([e for e in self.orchestrator_events()
                           if e["event"] == "usage_record"])
        self.consult("session-two", "second consultation")
        records = [e for e in self.orchestrator_events()
                   if e["event"] == "usage_record"]
        self.assertGreater(after_first, 0)
        self.assertGreater(len(records), after_first)
        second = [r for r in records if r.get("session_id") == "session-two"]
        self.assertGreater(len(second), 0)


class TestNativeTranscriptNameUnchanged(OrchestratorCase):

    def test_native_session_keeps_the_fixed_transcript_name(self):
        transcript = write_transcript(self.workspace, [assistant_record("hi")])
        with runner_env(self.workspace, run_id=RUN_ID):
            fire(self.workspace, "SessionStart", source="startup",
                 transcript_path=transcript)
            fire(self.workspace, "Stop", last_assistant_message="hi",
                 transcript_path=transcript)
            fire(self.workspace, "SessionEnd", reason="other",
                 transcript_path=transcript)
        fixed = self.paths.orchestrator_raw(RUN_ID, SESSION_ID)
        self.assertEqual("00_orchestrator_session.raw", fixed.name)
        self.assertTrue(fixed.exists())


if __name__ == "__main__":
    unittest.main()
