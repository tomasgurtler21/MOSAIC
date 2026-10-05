"""Orchestrator-role Runner-mode sessions for the ghcp-cli adapter.

The session is a run-level script-orchestrator call: events go to
00_orchestrator_events.jsonl of the declared run, no run_start/run_end is ever
written, and the transcript is exported under the session-scoped name.

Covers:
  Run-level events and the absence of run_start/run_end
  userPromptSubmitted arriving before sessionStart
  Session-scoped transcript naming in a real run folder (LogPaths flag)
  Fallbacks that put an unrecognised role or an instance-id-less subagent in
  orchestrator behaviour
  No invocation_start/invocation_end in this role
"""

import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core

from tests.runner_mode_support import (
    INSTANCE_ID, OTHER_RUN_ID, PROMPT, RUN_ID, SESSION_ID, RunnerModeTestCase,
    event_types,
)

_SCOPED_NAME = (
    "00_orchestrator_session__" + core.transcript_scope_segment("ghcp-cli", SESSION_ID) + ".raw"
)


class TestOrchestratorRunLevelEvents(RunnerModeTestCase):

    def setUp(self):
        super().setUp()
        self.orchestrator_env()

    def run_session(self, prompt_first=True):
        if prompt_first:
            self.dispatch("userPromptSubmitted", {"prompt": PROMPT})
            self.dispatch("sessionStart", {"source": "new"})
        else:
            self.dispatch("sessionStart", {"source": "new"})
            self.dispatch("userPromptSubmitted", {"prompt": PROMPT})
        self.dispatch("preToolUse", {"toolName": "bash", "toolArgs": {"command": "ls"},
                                     "toolUseId": "tu-1"})
        self.dispatch("postToolUse", {"toolName": "bash", "toolUseId": "tu-1",
                                      "toolResult": {"textResultForLlm": "ok"}})
        self.dispatch("agentStop", {"stopReason": "end_turn"})
        self.dispatch("sessionEnd", {"reason": "complete"})

    def test_session_events_go_to_the_declared_run_orchestrator_stream(self):
        self.run_session()
        types = event_types(self.orchestrator_events())
        self.assertEqual(1, types.count("session_start"))
        self.assertEqual(1, types.count("session_end"))
        self.assertIn("tool_call_start", types)
        self.assertIn("tool_call_end", types)

    def test_no_run_start_or_run_end_is_written(self):
        self.run_session()
        types = set(event_types(self.orchestrator_events()))
        self.assertIn("session_start", types)
        self.assertFalse(types & {"run_start", "run_end"})

    def test_prompt_before_session_start_is_logged_completely_in_declared_run(self):
        self.run_session(prompt_first=True)
        events = self.orchestrator_events()
        user_turns = [e for e in events if e["event"] == "turn" and e.get("role") == "user"]
        self.assertEqual([PROMPT], [t["content"] for t in user_turns])
        self.assertEqual(1, event_types(events).count("session_start"))
        self.assertNotIn("unknown-run", self.run_dir_names())

    def test_session_start_first_ordering_is_also_logged_in_declared_run(self):
        self.run_session(prompt_first=False)
        types = event_types(self.orchestrator_events())
        self.assertEqual(1, types.count("session_start"))
        self.assertNotIn("unknown-run", self.run_dir_names())

    def test_no_invocation_events_in_orchestrator_role(self):
        self.run_session()
        types = set(event_types(self.orchestrator_events()))
        self.assertIn("session_start", types)
        self.assertFalse(types & {"invocation_start", "invocation_end"})
        self.assertFalse(self.paths.invocation_dir(RUN_ID, INSTANCE_ID).exists())

    def test_env_run_id_wins_over_run_id_in_prompt(self):
        self.dispatch("userPromptSubmitted", {"prompt": f"run_id: {OTHER_RUN_ID}"})
        self.dispatch("sessionStart")
        self.assertNotIn(OTHER_RUN_ID, self.run_dir_names())
        self.assertIn("session_start", event_types(self.orchestrator_events()))

    def test_every_event_carries_session_id(self):
        self.run_session()
        events = self.orchestrator_events()
        self.assertIn("session_start", event_types(events))
        for event in events:
            with self.subTest(event=event["event"]):
                self.assertEqual(SESSION_ID, event["session_id"])


class TestOrchestratorTranscriptNaming(RunnerModeTestCase):

    def setUp(self):
        super().setUp()
        self.orchestrator_env()
        self.transcript = self.copy_transcript()

    def test_agent_stop_exports_session_scoped_transcript_in_real_run_folder(self):
        self.dispatch("sessionStart")
        self.dispatch("agentStop", {"transcriptPath": str(self.transcript)})
        run_dir = self.paths.run_root(RUN_ID)
        self.assertTrue((run_dir / _SCOPED_NAME).exists())
        self.assertEqual(self.transcript.read_bytes(), (run_dir / _SCOPED_NAME).read_bytes())

    def test_scoped_transcript_has_sidecar(self):
        self.dispatch("sessionStart")
        self.dispatch("agentStop", {"transcriptPath": str(self.transcript)})
        sidecar = (self.paths.run_root(RUN_ID) / _SCOPED_NAME).with_suffix(".meta.json")
        self.assertTrue(sidecar.exists())

    def test_fixed_transcript_name_is_not_used(self):
        self.dispatch("sessionStart")
        self.dispatch("agentStop", {"transcriptPath": str(self.transcript)})
        self.assertTrue((self.paths.run_root(RUN_ID) / _SCOPED_NAME).exists())
        self.assertFalse((self.paths.run_root(RUN_ID) / "00_orchestrator_session.raw").exists())

    def test_two_sessions_in_one_run_keep_separate_transcripts(self):
        second = self.copy_transcript(name="second.jsonl")
        second.write_text('{"role":"assistant","model":"m2"}\n', encoding="utf-8")
        self.dispatch("agentStop", {"transcriptPath": str(self.transcript)}, session_id="sess-A")
        self.dispatch("agentStop", {"transcriptPath": str(second)}, session_id="sess-B")
        raws = sorted(p.name for p in self.paths.run_root(RUN_ID).glob("00_orchestrator_session__*.raw"))
        self.assertEqual(2, len(raws))

    def test_no_orchestrator_transcript_when_path_absent(self):
        self.dispatch("sessionStart")
        self.dispatch("agentStop", {"stopReason": "end_turn"})
        self.assertIn("session_start", event_types(self.orchestrator_events()))
        self.assertEqual([], list(self.paths.run_root(RUN_ID).glob("00_orchestrator_session*")))


class TestScopedOrchestratorRawPaths(unittest.TestCase):

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.workspace = pathlib.Path(self._tmp.name)

    def test_native_paths_keep_fixed_name_in_real_run_folder(self):
        paths = core.build_paths(self.workspace)
        self.assertEqual("00_orchestrator_session.raw", paths.orchestrator_raw(RUN_ID, SESSION_ID).name)

    def test_scope_flag_gives_scoped_name_in_real_run_folder(self):
        paths = core.build_paths(self.workspace, scope_orchestrator_transcript=True)
        self.assertEqual(_SCOPED_NAME, paths.orchestrator_raw(RUN_ID, SESSION_ID).name)

    def test_scope_flag_without_session_id_falls_back_to_fixed_name(self):
        paths = core.build_paths(self.workspace, scope_orchestrator_transcript=True)
        self.assertEqual("00_orchestrator_session.raw", paths.orchestrator_raw(RUN_ID).name)
        self.assertEqual("00_orchestrator_session.raw", paths.orchestrator_raw(RUN_ID, "").name)

    def test_unknown_run_bucket_is_scoped_with_or_without_flag(self):
        for flag in (False, True):
            with self.subTest(flag=flag):
                paths = core.build_paths(self.workspace, scope_orchestrator_transcript=flag)
                self.assertEqual(_SCOPED_NAME, paths.orchestrator_raw("unknown-run", SESSION_ID).name)

    def test_log_paths_constructor_accepts_the_flag(self):
        paths = core.LogPaths(self.workspace, scope_orchestrator_transcript=True)
        self.assertEqual(_SCOPED_NAME, paths.orchestrator_raw(RUN_ID, SESSION_ID).name)


class TestRoleFallbackToOrchestratorBehaviour(RunnerModeTestCase):

    def test_unrecognised_role_writes_run_level_without_run_events(self):
        self.set_runner_env("wizard", RUN_ID, None)
        self.dispatch("sessionStart")
        self.dispatch("sessionEnd", {"reason": "complete"})
        types = set(event_types(self.orchestrator_events()))
        self.assertIn("session_start", types)
        self.assertFalse(types & {"run_start", "run_end"})

    def test_subagent_without_instance_id_writes_run_level_without_run_events(self):
        self.set_runner_env("subagent", RUN_ID, None)
        self.dispatch("sessionStart")
        self.dispatch("sessionEnd", {"reason": "complete"})
        types = set(event_types(self.orchestrator_events()))
        self.assertIn("session_start", types)
        self.assertFalse(types & {"run_start", "run_end", "invocation_start"})

    def test_invalid_run_id_still_honours_role_and_writes_no_run_events(self):
        self.set_runner_env("orchestrator", "garbage", None)
        self.dispatch("sessionStart")
        self.dispatch("sessionEnd", {"reason": "complete"})
        types = set(event_types(self.orchestrator_events("unknown-run")))
        self.assertIn("session_start", types)
        self.assertFalse(types & {"run_start", "run_end"})


if __name__ == "__main__":
    unittest.main()
