"""Subagent-role Runner-mode sessions for the ghcp-cli adapter.

A simulated GHCP CLI session (userPromptSubmitted BEFORE sessionStart, tool
calls, agentStop with a transcript, sessionEnd) must produce one invocation
folder under OrchestrationLogs/{run_id}/{MOSAIC_AGENT_INSTANCE_ID}/ holding the
event types a native invocation folder holds, with exactly one
invocation_start and one invocation_end, and nothing in the orchestrator stream.
"""

import importlib
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from tests.runner_mode_support import (
    FINAL_MODEL, FINAL_RESPONSE, FINAL_TOKEN_USAGE, INSTANCE_ID, OTHER_RUN_ID,
    PROMPT, RUN_ID, SESSION_ID, SUBAGENT_TRANSCRIPT, LazyModule,
    RunnerModeTestCase, event_types,
)

handlers_runner = LazyModule("mosaic_logger_handlers_runner")

_NATIVE_INVOCATION_TYPES = {
    "invocation_start", "invocation_end", "turn", "tool_call_start", "tool_call_end",
}


class _SubagentSessionCase(RunnerModeTestCase):

    def setUp(self):
        super().setUp()
        self.subagent_env()
        self.transcript = self.copy_transcript()

    def run_full_session(self):
        """Prompt arrives before sessionStart, as GHCP CLI fires them."""
        self.dispatch("userPromptSubmitted", {"prompt": PROMPT})
        self.dispatch("sessionStart", {"source": "new"})
        self.dispatch("preToolUse", {"toolName": "bash", "toolArgs": {"command": "ls"},
                                     "toolUseId": "tu-1"})
        self.dispatch("postToolUse", {"toolName": "bash", "toolUseId": "tu-1",
                                      "toolResult": {"textResultForLlm": "ok"}})
        self.dispatch("agentStop", {"transcriptPath": str(self.transcript),
                                    "stopReason": "end_turn"})
        self.dispatch("sessionEnd", {"reason": "complete"})

    def of_type(self, name):
        return [e for e in self.invocation_events() if e.get("event") == name]


class TestSubagentInvocationFolder(_SubagentSessionCase):

    def test_only_native_invocation_event_types_are_written(self):
        self.run_full_session()
        self.assertEqual(_NATIVE_INVOCATION_TYPES, set(event_types(self.invocation_events())))

    def test_exactly_one_invocation_start_and_end(self):
        self.run_full_session()
        types = event_types(self.invocation_events())
        self.assertEqual(1, types.count("invocation_start"))
        self.assertEqual(1, types.count("invocation_end"))

    def test_invocation_start_is_first_and_carries_prompt_and_identity(self):
        self.run_full_session()
        events = self.invocation_events()
        self.assertEqual("invocation_start", events[0]["event"])
        self.assertEqual(PROMPT, events[0]["prompt"])
        self.assertEqual(INSTANCE_ID, events[0]["agent_instance_id"])
        self.assertEqual("Research", events[0]["agent_type"])

    def test_no_event_types_other_than_turn_follow_invocation_end(self):
        self.run_full_session()
        events = self.invocation_events()
        trailing = [e["event"] for e in events[events.index(self.of_type("invocation_end")[0]) + 1:]]
        self.assertEqual([], [t for t in trailing if t != "turn"])

    def test_invocation_end_fields_come_from_the_transcript(self):
        self.run_full_session()
        end = self.of_type("invocation_end")[0]
        self.assertEqual(FINAL_RESPONSE, end["response"])
        self.assertEqual("SUCCESS", end["status_code"])
        self.assertEqual(FINAL_MODEL, end["model"])
        self.assertEqual(FINAL_TOKEN_USAGE, end["token_usage"])
        self.assertEqual(INSTANCE_ID, end["agent_instance_id"])

    def test_initial_user_turn_carries_prompt_exactly_once(self):
        self.run_full_session()
        user_turns = [t for t in self.of_type("turn") if t.get("role") == "user"]
        self.assertEqual(1, len(user_turns))
        self.assertEqual(PROMPT, user_turns[0]["content"])

    def test_final_assistant_turn_is_written_after_the_user_turn(self):
        self.run_full_session()
        roles = [t.get("role") for t in self.of_type("turn")]
        self.assertEqual(["user", "assistant"], roles)

    def test_tool_calls_are_logged_in_the_invocation_stream(self):
        self.run_full_session()
        self.assertEqual(1, len(self.of_type("tool_call_start")))
        self.assertEqual("success", self.of_type("tool_call_end")[0]["status"])

    def test_no_usage_record_is_written(self):
        self.run_full_session()
        self.assertIn("invocation_end", event_types(self.invocation_events()))
        self.assertEqual([], self.of_type("usage_record"))

    def test_no_run_or_session_events_are_written_anywhere(self):
        self.run_full_session()
        types = set(event_types(self.invocation_events()))
        self.assertIn("invocation_start", types)
        self.assertFalse(types & {"run_start", "run_end", "session_start", "session_end"})

    def test_every_envelope_carries_session_id_and_ghcp_harness(self):
        self.run_full_session()
        events = self.invocation_events()
        self.assertIn("invocation_start", event_types(events))
        for event in events:
            with self.subTest(event=event["event"]):
                self.assertEqual(SESSION_ID, event["session_id"])
                self.assertEqual("ghcp-cli", event["harness"])


class TestSubagentIsolation(_SubagentSessionCase):

    def test_nothing_is_written_to_the_orchestrator_stream(self):
        self.run_full_session()
        self.assertTrue(self.paths.invocation_events(RUN_ID, INSTANCE_ID).exists())
        self.assertFalse(self.paths.orchestrator_events(RUN_ID).exists())
        self.assertEqual([], list((self.workspace / "OrchestrationLogs").glob(
            "*/00_orchestrator_events.jsonl")))

    def test_nothing_is_routed_to_unknown_run(self):
        self.run_full_session()
        self.assertNotIn("unknown-run", self.run_dir_names())

    def test_prompt_event_before_session_start_is_attributed_to_declared_run(self):
        self.dispatch("userPromptSubmitted", {"prompt": f"{PROMPT}\nrun_id: {OTHER_RUN_ID}"})
        self.assertTrue(self.paths.invocation_events(RUN_ID, INSTANCE_ID).exists())
        self.assertNotIn(OTHER_RUN_ID, self.run_dir_names())
        self.assertNotIn("unknown-run", self.run_dir_names())

    def test_run_folder_holds_only_the_invocation_folder_and_dot_prefixed_state(self):
        self.run_full_session()
        run_dir = self.paths.run_root(RUN_ID)
        names = {p.name for p in run_dir.iterdir()}
        self.assertIn(INSTANCE_ID, names)
        self.assertEqual(set(), {n for n in names if n != INSTANCE_ID and not n.startswith(".")})

    def test_primary_session_only_events_are_not_written_in_subagent_role(self):
        self.dispatch("userPromptSubmitted", {"prompt": PROMPT})
        self.dispatch("sessionStart")
        self.dispatch("notification", {"notificationType": "info", "message": "hello"})
        self.dispatch("preCompact", {"trigger": "auto"})
        self.dispatch("errorOccurred", {"error": {"message": "x", "name": "E"}})
        self.dispatch("permissionRequest", {"toolName": "bash"})
        self.dispatch("userPromptTransformed", {"transformedPrompt": "p2"})
        types = set(event_types(self.invocation_events()))
        self.assertIn("invocation_start", types)
        self.assertEqual(set(), types - _NATIVE_INVOCATION_TYPES)
        self.assertEqual([], list((self.workspace / "OrchestrationLogs").glob(
            "*/00_orchestrator_events.jsonl")))


class TestSubagentArtifacts(_SubagentSessionCase):

    def test_input_and_output_documents_are_written(self):
        self.run_full_session()
        self.assertIn(PROMPT, self.paths.invocation_input(RUN_ID, INSTANCE_ID).read_text("utf-8"))
        self.assertTrue(self.paths.invocation_output(RUN_ID, INSTANCE_ID).exists())

    def test_output_document_contains_the_response(self):
        self.run_full_session()
        text = self.paths.invocation_output(RUN_ID, INSTANCE_ID).read_text("utf-8")
        self.assertIn("Work complete.", text)

    def test_transcript_is_exported_byte_for_byte_with_sidecar(self):
        self.run_full_session()
        raw = self.paths.invocation_raw(RUN_ID, INSTANCE_ID)
        self.assertEqual(SUBAGENT_TRANSCRIPT.read_bytes(), raw.read_bytes())
        self.assertTrue(raw.with_suffix(".meta.json").exists())

    def test_no_orchestrator_transcript_is_exported(self):
        self.run_full_session()
        self.assertTrue(self.paths.invocation_raw(RUN_ID, INSTANCE_ID).exists())
        leftovers = list(self.paths.run_root(RUN_ID).glob("00_orchestrator_session*"))
        self.assertEqual([], leftovers)


class TestSubagentOrderingAndExactlyOnce(_SubagentSessionCase):

    def test_session_start_first_then_prompt_still_yields_one_start_and_one_user_turn(self):
        self.dispatch("sessionStart")
        self.dispatch("userPromptSubmitted", {"prompt": PROMPT})
        self.dispatch("agentStop", {"transcriptPath": str(self.transcript)})
        events = self.invocation_events()
        self.assertEqual(1, event_types(events).count("invocation_start"))
        user_turns = [e for e in events if e["event"] == "turn" and e.get("role") == "user"]
        self.assertEqual(1, len(user_turns))
        self.assertIn(PROMPT, self.paths.invocation_input(RUN_ID, INSTANCE_ID).read_text("utf-8"))

    def test_repeated_prompt_events_write_one_invocation_start(self):
        self.dispatch("userPromptSubmitted", {"prompt": PROMPT})
        self.dispatch("sessionStart")
        self.dispatch("userPromptSubmitted", {"prompt": "A second prompt"})
        self.assertEqual(1, event_types(self.invocation_events()).count("invocation_start"))

    def test_repeated_agent_stop_and_session_end_write_one_invocation_end(self):
        self.run_full_session()
        self.dispatch("agentStop", {"transcriptPath": str(self.transcript)})
        self.dispatch("sessionEnd", {"reason": "complete"})
        self.assertEqual(1, event_types(self.invocation_events()).count("invocation_end"))

    def test_session_end_alone_completes_the_invocation_once(self):
        self.dispatch("userPromptSubmitted", {"prompt": PROMPT})
        self.dispatch("sessionStart")
        self.dispatch("sessionEnd", {"reason": "complete"})
        self.dispatch("sessionEnd", {"reason": "complete"})
        events = self.invocation_events()
        self.assertEqual(1, event_types(events).count("invocation_start"))
        self.assertEqual(1, event_types(events).count("invocation_end"))
        end = [e for e in events if e["event"] == "invocation_end"][0]
        self.assertNotIn("response", end)
        self.assertNotIn("status_code", end)

    def test_session_end_without_any_prior_event_still_writes_start_then_end(self):
        self.dispatch("sessionEnd", {"reason": "complete"})
        types = event_types(self.invocation_events())
        self.assertEqual(["invocation_start", "invocation_end"], types)

    def test_a_new_harness_session_for_the_same_instance_is_a_new_invocation(self):
        self.run_full_session()
        self.dispatch("userPromptSubmitted", {"prompt": "resumed"}, session_id="other-session")
        self.dispatch("sessionEnd", {"reason": "complete"}, session_id="other-session")
        types = event_types(self.invocation_events())
        self.assertEqual(2, types.count("invocation_start"))
        self.assertEqual(2, types.count("invocation_end"))

    def test_unknown_event_in_subagent_role_does_not_raise_or_write(self):
        self.dispatch("userPromptSubmitted", {"prompt": PROMPT})
        self.dispatch("sessionStart")
        before = self.invocation_events()
        self.assertIn("invocation_start", event_types(before))
        self.dispatch("someFutureEvent", {"x": 1})
        self.assertEqual(before, self.invocation_events())


class TestSubagentAdditionalEvents(_SubagentSessionCase):

    def test_tool_failure_is_logged_in_invocation_stream_with_failure_status(self):
        self.dispatch("userPromptSubmitted", {"prompt": PROMPT})
        self.dispatch("sessionStart")
        self.dispatch("preToolUse", {"toolName": "bash", "toolArgs": {"command": "x"},
                                     "toolUseId": "tu-9"})
        self.dispatch("postToolUseFailure", {"toolName": "bash", "toolUseId": "tu-9",
                                             "error": "boom"})
        ends = self.of_type("tool_call_end")
        self.assertEqual(1, len(ends))
        self.assertNotEqual("success", ends[0]["status"])
        self.assertFalse(self.paths.orchestrator_events(RUN_ID).exists())

    def test_native_subagent_events_do_not_create_a_second_invocation(self):
        self.dispatch("userPromptSubmitted", {"prompt": PROMPT})
        self.dispatch("sessionStart")
        self.dispatch("subagentStart", {"agentName": "child", "agentId": "c-1"})
        self.dispatch("subagentStop", {"agentName": "child", "agentId": "c-1"})
        events = self.invocation_events()
        self.assertEqual(1, event_types(events).count("invocation_start"))
        self.assertFalse(self.paths.orchestrator_events(RUN_ID).exists())
        folders = sorted(p.name for p in self.paths.run_root(RUN_ID).iterdir()
                         if not p.name.startswith("."))
        self.assertEqual([INSTANCE_ID], folders)

    def test_session_start_without_any_prompt_writes_start_without_prompt(self):
        self.dispatch("sessionStart", {"source": "new"})
        self.dispatch("sessionEnd", {"reason": "complete"})
        events = self.invocation_events()
        starts = self.of_type("invocation_start")
        self.assertEqual(1, len(starts))
        self.assertNotIn("prompt", starts[0])
        self.assertEqual([], [e for e in events
                              if e["event"] == "turn" and e.get("role") == "user"])
        self.assertFalse(self.paths.invocation_input(RUN_ID, INSTANCE_ID).exists())

    def test_absent_run_id_attributes_invocation_to_unknown_run_without_raising(self):
        self.set_runner_env("subagent", None, INSTANCE_ID)
        self.dispatch("userPromptSubmitted", {"prompt": PROMPT})
        self.dispatch("sessionStart")
        self.dispatch("sessionEnd", {"reason": "complete"})
        types = event_types(self.invocation_events("unknown-run"))
        self.assertEqual(1, types.count("invocation_start"))
        self.assertNotIn(RUN_ID, self.run_dir_names())


class TestKnownGapsDocumentation(RunnerModeTestCase):

    def test_handler_module_carries_known_gaps_block(self):
        real_module = importlib.import_module("mosaic_logger_handlers_runner")
        self.assertIn("Known Runner-mode field gaps", real_module.__doc__ or "")

    def test_handler_table_covers_the_subagent_events(self):
        self.assertEqual(
            {"sessionStart", "userPromptSubmitted", "agentStop", "sessionEnd"},
            set(handlers_runner.SUBAGENT_HANDLERS))


if __name__ == "__main__":
    import unittest
    unittest.main()
