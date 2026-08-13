"""End-to-end synthetic validation tests for the mosaic-logger GHCP CLI adapter.

Drives the full dispatcher pipeline by feeding synthetic hook payloads through
mosaic_logger.dispatch() against a temporary workspace and asserting the resulting
on-disk tree, event streams, and field-level behavior match the adapter design.

Key differences from the claude-code e2e tests:
- dispatch(event, raw_input) takes event as separate first argument (from sys.argv[1])
- Every payload uses camelCase fields (sessionId, agentId, agentPrompt, etc.)
- Workspace root is set via payload 'cwd' field (not CLAUDE_PROJECT_DIR env var)
- sessionStart has no prompt-bearing field -- run_id stays None -> unknown-run routing
- agentStop does NOT carry last_assistant_message; turn emitted without content
- Tool names are lowercase 'agent'/'task' (not PascalCase)

Covers:
  Full session lifecycle: sessionStart -> tool events -> subagent lifecycle -> agentStop -> sessionEnd
  Directory structure: OrchestrationLogs/{run_id}/ with correct event files
  JSONL event content: schema_version, harness, required envelope fields
  Failure modes: malformed payloads, unknown events, missing fields -> exit 0, no stdout
  Safety: no stdout output on any event, exit 0 invariant
"""

import contextlib
import io
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


# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

_SESSION_ID = "e2e-ghcp-session-001"
_RUN_ID = "20260101T120000Z-e2e1"
_AGENT_ID_ALPHA = "ghcp-agt-alpha-001"
_AGENT_ID_BETA = "ghcp-agt-beta-002"
_INSTANCE_ID_ALPHA = "Research#1"
_INSTANCE_ID_BETA = "TestWriter#2"

_REQUIRED_ENVELOPE = ("schema_version", "event", "timestamp", "harness")
_ISO8601_MS_Z = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")


# ---------------------------------------------------------------------------
# Replay harness for ghcp-cli
# ---------------------------------------------------------------------------

class _GhcpReplayHarness:
    """Drives mosaic_logger.dispatch() against an isolated temporary workspace.

    For the ghcp-cli adapter, workspace root is resolved from payload 'cwd'.
    Each payload dispatched includes 'cwd' set to the workspace path so that
    all writes land under the temp tree (not the real filesystem).

    Usage:
        with _GhcpReplayHarness(tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": "s1"})
            events = h.orchestrator_events("unknown-run")
    """

    def __init__(self, workspace: pathlib.Path):
        self.workspace = workspace

    def __enter__(self) -> "_GhcpReplayHarness":
        return self

    def __exit__(self, *_) -> None:
        pass

    def dispatch(self, event: str, payload: dict) -> str:
        """Dispatch one event, capturing and returning any stdout produced."""
        # Inject workspace cwd into every payload
        payload_with_cwd = {"cwd": str(self.workspace), **payload}
        raw = json.dumps(payload_with_cwd, ensure_ascii=False)
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            mosaic_logger.dispatch(event, raw)
        return buf.getvalue()

    def dispatch_no_cwd(self, event: str, raw_input: str) -> str:
        """Dispatch raw JSON without injecting cwd."""
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            mosaic_logger.dispatch(event, raw_input)
        return buf.getvalue()

    def orchestrator_events(self, run_id: str) -> list:
        paths = core.build_paths(self.workspace)
        path = paths.orchestrator_events(run_id)
        if not path.exists():
            return []
        return [json.loads(ln) for ln in path.read_text("utf-8").splitlines() if ln.strip()]

    def invocation_events(self, run_id: str, instance_id: str) -> list:
        paths = core.build_paths(self.workspace)
        path = paths.invocation_events(run_id, instance_id)
        if not path.exists():
            return []
        return [json.loads(ln) for ln in path.read_text("utf-8").splitlines() if ln.strip()]


# ---------------------------------------------------------------------------
# Lifecycle test helpers
# ---------------------------------------------------------------------------

def _prompt_with_ids(instance_id: str, run_id: str) -> str:
    return (
        f'agent_instance_id: "{instance_id}"\n'
        f'run_id: "{run_id}"\n'
        f'Task description here.'
    )


# ---------------------------------------------------------------------------
# Session lifecycle: sessionStart and sessionEnd
# ---------------------------------------------------------------------------

class TestSessionLifecycle(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_session_start_creates_orchestrator_events_file(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            # sessionStart has no prompt-bearing field -> unknown-run
            orch_path = core.build_paths(self.tmp_path).orchestrator_events("unknown-run")
            self.assertTrue(orch_path.exists())

    def test_session_start_emits_run_start_and_session_start(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            events = h.orchestrator_events("unknown-run")
            event_types = {e["event"] for e in events}
            self.assertIn("run_start", event_types)
            self.assertIn("session_start", event_types)

    def test_session_end_emits_session_end_and_run_end(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("sessionEnd", {"sessionId": _SESSION_ID, "reason": "clear"})
            events = h.orchestrator_events("unknown-run")
            event_types = {e["event"] for e in events}
            self.assertIn("session_end", event_types)
            self.assertIn("run_end", event_types)

    def test_all_emitted_events_have_required_envelope_fields(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("sessionEnd", {"sessionId": _SESSION_ID, "reason": "clear"})
            events = h.orchestrator_events("unknown-run")
            for event in events:
                for field in _REQUIRED_ENVELOPE:
                    with self.subTest(event=event.get("event"), field=field):
                        self.assertIn(field, event)

    def test_harness_is_ghcp_cli_in_all_events(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("userPromptSubmitted", {"sessionId": _SESSION_ID, "prompt": "Hello!"})
            h.dispatch("sessionEnd", {"sessionId": _SESSION_ID, "reason": "clear"})
            events = h.orchestrator_events("unknown-run")
            for event in events:
                with self.subTest(event_type=event.get("event")):
                    self.assertEqual("ghcp-cli", event["harness"])

    def test_resumed_true_emitted_when_payload_says_true(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID, "resumed": True})
            events = h.orchestrator_events("unknown-run")
            session_start = next(e for e in events if e["event"] == "session_start")
            self.assertIs(True, session_start["resumed"])

    def test_no_null_values_in_any_emitted_event(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("sessionEnd", {"sessionId": _SESSION_ID})
            events = h.orchestrator_events("unknown-run")
            for event in events:
                for key, value in event.items():
                    with self.subTest(event_type=event.get("event"), key=key):
                        self.assertIsNotNone(value)


# ---------------------------------------------------------------------------
# User prompt and notification events
# ---------------------------------------------------------------------------

class TestSessionScopedEvents(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_user_prompt_submitted_emits_user_turn(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("userPromptSubmitted", {
                "sessionId": _SESSION_ID,
                "prompt": "Hello GHCP CLI!"
            })
            events = h.orchestrator_events("unknown-run")
            turns = [e for e in events if e.get("event") == "turn"]
            self.assertTrue(any(t.get("role") == "user" for t in turns))

    def test_user_turn_content_matches_prompt_field(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("userPromptSubmitted", {
                "sessionId": _SESSION_ID,
                "prompt": "Specific prompt text here"
            })
            events = h.orchestrator_events("unknown-run")
            user_turn = next((e for e in events
                              if e.get("event") == "turn" and e.get("role") == "user"), None)
            self.assertIsNotNone(user_turn)
            self.assertEqual("Specific prompt text here", user_turn["content"])

    def test_notification_emits_notification_event(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("notification", {
                "sessionId": _SESSION_ID,
                "notificationType": "info",
                "message": "Context nearing limit"
            })
            events = h.orchestrator_events("unknown-run")
            notif = next((e for e in events if e.get("event") == "notification"), None)
            self.assertIsNotNone(notif)

    def test_agent_stop_emits_assistant_turn_without_content(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("agentStop", {
                "sessionId": _SESSION_ID,
                "stopReason": "end_turn",
            })
            events = h.orchestrator_events("unknown-run")
            assistant_turns = [e for e in events
                               if e.get("event") == "turn" and e.get("role") == "assistant"]
            self.assertTrue(len(assistant_turns) >= 1)
            for turn in assistant_turns:
                self.assertNotIn("content", turn)

    def test_pre_compact_emits_compaction_event(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("preCompact", {
                "sessionId": _SESSION_ID,
                "trigger": "auto",
            })
            events = h.orchestrator_events("unknown-run")
            compaction = next((e for e in events if e.get("event") == "compaction"), None)
            self.assertIsNotNone(compaction)


# ---------------------------------------------------------------------------
# GHCP-CLI-specific observational events
# ---------------------------------------------------------------------------

class TestObservationalEvents(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_user_prompt_transformed_emits_prompt_transformed_event(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            stdout = h.dispatch("userPromptTransformed", {
                "sessionId": _SESSION_ID,
                "transformedPrompt": "Enhanced prompt with context"
            })
            self.assertEqual("", stdout)
            events = h.orchestrator_events("unknown-run")
            pt = next((e for e in events if e.get("event") == "prompt_transformed"), None)
            self.assertIsNotNone(pt)

    def test_permission_request_emits_permission_request_event(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            stdout = h.dispatch("permissionRequest", {
                "sessionId": _SESSION_ID,
                "toolName": "bash"
            })
            self.assertEqual("", stdout)
            events = h.orchestrator_events("unknown-run")
            pr = next((e for e in events if e.get("event") == "permission_request"), None)
            self.assertIsNotNone(pr)

    def test_error_occurred_emits_error_occurred_event(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("errorOccurred", {
                "sessionId": _SESSION_ID,
                "error": {"message": "Network failure", "name": "NetworkError"},
                "recoverable": False,
            })
            events = h.orchestrator_events("unknown-run")
            err = next((e for e in events if e.get("event") == "error_occurred"), None)
            self.assertIsNotNone(err)

    def test_observational_events_produce_no_stdout(self):
        observational = [
            ("userPromptTransformed",
             {"sessionId": _SESSION_ID, "transformedPrompt": "Enhanced"}),
            ("permissionRequest",
             {"sessionId": _SESSION_ID, "toolName": "bash"}),
            ("errorOccurred",
             {"sessionId": _SESSION_ID, "error": {"message": "Oops", "name": "Error"}}),
        ]
        with _GhcpReplayHarness(self.tmp_path) as h:
            for event, payload in observational:
                with self.subTest(event=event):
                    stdout = h.dispatch(event, payload)
                    self.assertEqual("", stdout)


# ---------------------------------------------------------------------------
# Tool events
# ---------------------------------------------------------------------------

class TestToolEvents(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_pre_tool_use_emits_tool_call_start(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("preToolUse", {
                "sessionId": _SESSION_ID,
                "toolName": "bash",
                "toolArgs": {"command": "ls"},
                "toolUseId": "tu-001",
            })
            events = h.orchestrator_events("unknown-run")
            starts = [e for e in events if e.get("event") == "tool_call_start"]
            self.assertTrue(len(starts) >= 1)

    def test_post_tool_use_emits_tool_call_end_success(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("postToolUse", {
                "sessionId": _SESSION_ID,
                "toolName": "bash",
                "toolUseId": "tu-002",
                "toolResult": {"textResultForLlm": "output here"},
            })
            events = h.orchestrator_events("unknown-run")
            ends = [e for e in events if e.get("event") == "tool_call_end"]
            self.assertTrue(len(ends) >= 1)
            self.assertEqual("success", ends[0]["status"])

    def test_post_tool_use_failure_emits_tool_call_end_error(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("postToolUseFailure", {
                "sessionId": _SESSION_ID,
                "toolName": "bash",
                "toolUseId": "tu-003",
                "error": "Command not found",
            })
            events = h.orchestrator_events("unknown-run")
            ends = [e for e in events if e.get("event") == "tool_call_end"]
            self.assertTrue(len(ends) >= 1)
            self.assertEqual("error", ends[0]["status"])

    def test_tool_events_produce_no_stdout(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            tool_payloads = [
                ("preToolUse",
                 {"sessionId": _SESSION_ID, "toolName": "bash",
                  "toolArgs": {}, "toolUseId": "tu-s1"}),
                ("postToolUse",
                 {"sessionId": _SESSION_ID, "toolName": "bash",
                  "toolUseId": "tu-s2",
                  "toolResult": {"textResultForLlm": "result"}}),
                ("postToolUseFailure",
                 {"sessionId": _SESSION_ID, "toolName": "bash",
                  "toolUseId": "tu-s3", "error": "Crash"}),
            ]
            for event, payload in tool_payloads:
                with self.subTest(event=event):
                    stdout = h.dispatch(event, payload)
                    self.assertEqual("", stdout)


# ---------------------------------------------------------------------------
# Subagent lifecycle
# ---------------------------------------------------------------------------

class TestSubagentLifecycle(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_subagent_start_creates_invocation_directory(self):
        """After pre-populating a pending dispatch, subagentStart creates invocation dir."""
        paths = core.build_paths(self.tmp_path)
        # Put a pending dispatch (simulating preToolUse having fired)
        runstate.put_pending_dispatch(
            paths, "unknown-run", _SESSION_ID, _INSTANCE_ID_ALPHA, None,
            prompt=_prompt_with_ids(_INSTANCE_ID_ALPHA, _RUN_ID),
        )
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("subagentStart", {
                "sessionId": _SESSION_ID,
                "agentId": _AGENT_ID_ALPHA,
                "agentName": "Research",
                "agentPrompt": "Fallback prompt",
            })
            inv_dir = core.build_paths(self.tmp_path).invocation_dir(
                "unknown-run", _INSTANCE_ID_ALPHA
            )
            self.assertTrue(inv_dir.exists())

    def test_subagent_start_emits_invocation_start(self):
        paths = core.build_paths(self.tmp_path)
        runstate.put_pending_dispatch(
            paths, "unknown-run", _SESSION_ID, _INSTANCE_ID_ALPHA, None,
            prompt="Dispatch prompt text."
        )
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("subagentStart", {
                "sessionId": _SESSION_ID,
                "agentId": _AGENT_ID_ALPHA,
                "agentName": "Research",
            })
            inv_events = h.invocation_events("unknown-run", _INSTANCE_ID_ALPHA)
            event_types = {e["event"] for e in inv_events}
            self.assertIn("invocation_start", event_types)

    def test_subagent_stop_emits_invocation_end(self):
        paths = core.build_paths(self.tmp_path)
        # Register mapping directly (simulates subagentStart having fired)
        runstate.put_agent_mapping(
            paths, "unknown-run", _AGENT_ID_ALPHA, _INSTANCE_ID_ALPHA, "worker"
        )
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("subagentStop", {
                "sessionId": _SESSION_ID,
                "agentId": _AGENT_ID_ALPHA,
                "response": '{"status_code": "SUCCESS"}\nWork complete.',
            })
            inv_events = h.invocation_events("unknown-run", _INSTANCE_ID_ALPHA)
            event_types = {e["event"] for e in inv_events}
            self.assertIn("invocation_end", event_types)

    def test_subagent_stop_extracts_status_code(self):
        paths = core.build_paths(self.tmp_path)
        runstate.put_agent_mapping(
            paths, "unknown-run", _AGENT_ID_ALPHA, _INSTANCE_ID_ALPHA, None
        )
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("subagentStop", {
                "sessionId": _SESSION_ID,
                "agentId": _AGENT_ID_ALPHA,
                "response": 'status_code: "COMPLETED_NEEDS_ACTION"',
            })
            inv_events = h.invocation_events("unknown-run", _INSTANCE_ID_ALPHA)
            end_event = next(e for e in inv_events if e["event"] == "invocation_end")
            self.assertEqual("COMPLETED_NEEDS_ACTION", end_event["status_code"])

    def test_subagent_start_via_pretooluse_agent_dispatch_handoff(self):
        """Full pipeline: preToolUse(agent) writes the pending dispatch, then
        subagentStart pops it — no manual runstate pre-population.

        This exercises the integration seam between the two handlers: preToolUse
        writes a pending dispatch keyed by session_id under the session's effective
        run_id (unknown-run, since preToolUse is not a prompt-bearing event).
        subagentStart pops the dispatch and inherits the run_id embedded in the
        pending dispatch record, so the invocation directory is created under
        that resolved run_id (_RUN_ID), not "unknown-run". The test verifies the
        full handoff without any manual put_pending_dispatch call."""
        prompt = _prompt_with_ids(_INSTANCE_ID_ALPHA, _RUN_ID)
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            # preToolUse with toolName 'agent' stores the pending dispatch
            stdout = h.dispatch("preToolUse", {
                "sessionId": _SESSION_ID,
                "toolName": "agent",
                "toolArgs": {"prompt": prompt},
                "toolUseId": "tu-e2e-handoff-001",
            })
            self.assertEqual("", stdout, "preToolUse must not write stdout")
            # subagentStart pops the pending dispatch; ctx.run_id is inherited
            # from dispatch["run_id"] (_RUN_ID), so the invocation dir is under _RUN_ID
            h.dispatch("subagentStart", {
                "sessionId": _SESSION_ID,
                "agentId": _AGENT_ID_ALPHA,
                "agentName": "Research",
            })
            # Verify invocation directory exists under the resolved run_id
            inv_dir = core.build_paths(self.tmp_path).invocation_dir(
                _RUN_ID, _INSTANCE_ID_ALPHA
            )
            self.assertTrue(inv_dir.exists(),
                            "invocation directory must exist after full preToolUse→subagentStart handoff")
            inv_events = h.invocation_events(_RUN_ID, _INSTANCE_ID_ALPHA)
            event_types = {e["event"] for e in inv_events}
            self.assertIn("invocation_start", event_types)

    def test_subagent_events_do_not_leak_to_orchestrator_stream(self):
        """invocation_start and invocation_end must not appear in orchestrator stream."""
        paths = core.build_paths(self.tmp_path)
        runstate.put_pending_dispatch(
            paths, "unknown-run", _SESSION_ID, _INSTANCE_ID_ALPHA, None,
            prompt="Task prompt."
        )
        runstate.put_agent_mapping(
            paths, "unknown-run", _AGENT_ID_ALPHA, _INSTANCE_ID_ALPHA, None
        )
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("subagentStart", {
                "sessionId": _SESSION_ID,
                "agentId": _AGENT_ID_ALPHA,
                "agentName": "Research",
            })
            h.dispatch("subagentStop", {
                "sessionId": _SESSION_ID,
                "agentId": _AGENT_ID_ALPHA,
                "response": "Done.",
            })
            orch_events = h.orchestrator_events("unknown-run")
            orch_types = {e.get("event") for e in orch_events}
            self.assertNotIn("invocation_start", orch_types)
            self.assertNotIn("invocation_end", orch_types)


# ---------------------------------------------------------------------------
# Failure modes: malformed payloads, unknown events
# ---------------------------------------------------------------------------

class TestFailureModes(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_unknown_event_does_not_raise(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            try:
                h.dispatch("UnknownGhcpEvent", {"sessionId": _SESSION_ID})
            except Exception as exc:
                self.fail(f"dispatch raised for unknown event: {exc}")

    def test_unknown_event_produces_no_stdout(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            stdout = h.dispatch("UnknownGhcpEvent", {"sessionId": _SESSION_ID})
            self.assertEqual("", stdout)

    def test_malformed_json_does_not_raise(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            try:
                h.dispatch_no_cwd("sessionStart", "{invalid json")
            except Exception as exc:
                self.fail(f"dispatch raised for malformed JSON: {exc}")

    def test_malformed_json_produces_no_stdout(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            stdout = h.dispatch_no_cwd("sessionStart", "{invalid json")
            self.assertEqual("", stdout)

    def test_empty_payload_does_not_raise(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            try:
                h.dispatch("sessionStart", {})
            except Exception as exc:
                self.fail(f"dispatch raised for empty payload: {exc}")

    def test_empty_event_name_does_not_raise(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            try:
                h.dispatch("", {"sessionId": _SESSION_ID})
            except Exception as exc:
                self.fail(f"dispatch raised for empty event name: {exc}")

    def test_all_known_events_produce_no_stdout(self):
        """No GHCP CLI event should produce any stdout from this adapter."""
        known_events = [
            ("sessionStart", {"sessionId": _SESSION_ID}),
            ("sessionEnd", {"sessionId": _SESSION_ID, "reason": "clear"}),
            ("userPromptSubmitted", {"sessionId": _SESSION_ID, "prompt": "Hello"}),
            ("userPromptTransformed", {"sessionId": _SESSION_ID, "transformedPrompt": "Enhanced"}),
            ("preToolUse", {"sessionId": _SESSION_ID, "toolName": "bash",
                            "toolArgs": {}, "toolUseId": "tu-f1"}),
            ("postToolUse", {"sessionId": _SESSION_ID, "toolName": "bash",
                             "toolUseId": "tu-f2"}),
            ("postToolUseFailure", {"sessionId": _SESSION_ID, "toolName": "bash",
                                    "toolUseId": "tu-f3", "error": "Crash"}),
            ("preCompact", {"sessionId": _SESSION_ID, "trigger": "auto"}),
            ("agentStop", {"sessionId": _SESSION_ID, "stopReason": "end_turn"}),
            ("subagentStart", {"sessionId": _SESSION_ID, "agentId": "a1"}),
            ("subagentStop", {"sessionId": _SESSION_ID, "agentId": "a1"}),
            ("permissionRequest", {"sessionId": _SESSION_ID, "toolName": "bash"}),
            ("errorOccurred", {"sessionId": _SESSION_ID,
                               "error": {"message": "Oops", "name": "Error"}}),
            ("notification", {"sessionId": _SESSION_ID, "notificationType": "info",
                              "message": "Test"}),
        ]
        with _GhcpReplayHarness(self.tmp_path) as h:
            for event, payload in known_events:
                with self.subTest(event=event):
                    stdout = h.dispatch(event, payload)
                    self.assertEqual("", stdout,
                                     f"event '{event}' produced unexpected stdout: {stdout!r}")

    def test_missing_session_id_does_not_raise(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            try:
                h.dispatch("sessionStart", {})
            except Exception as exc:
                self.fail(f"dispatch raised when sessionId absent: {exc}")

    def test_json_array_input_does_not_raise(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            try:
                h.dispatch_no_cwd("sessionStart", "[1, 2, 3]")
            except Exception as exc:
                self.fail(f"dispatch raised for JSON array: {exc}")

    def test_json_array_produces_no_stdout(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            stdout = h.dispatch_no_cwd("sessionStart", "[1, 2, 3]")
            self.assertEqual("", stdout)


# ---------------------------------------------------------------------------
# Directory structure
# ---------------------------------------------------------------------------

class TestDirectoryStructure(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_orchestration_logs_directory_created(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            logs_dir = self.tmp_path / "OrchestrationLogs"
            self.assertTrue(logs_dir.exists())

    def test_unknown_run_directory_created_when_run_id_unresolvable(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            unknown_dir = self.tmp_path / "OrchestrationLogs" / "unknown-run"
            self.assertTrue(unknown_dir.exists())

    def test_orchestrator_events_file_is_valid_jsonl(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("userPromptSubmitted", {"sessionId": _SESSION_ID, "prompt": "Hi"})
            h.dispatch("sessionEnd", {"sessionId": _SESSION_ID, "reason": "clear"})
            orch_path = core.build_paths(self.tmp_path).orchestrator_events("unknown-run")
            self.assertTrue(orch_path.exists())
            text = orch_path.read_text("utf-8")
            for line in text.splitlines():
                if line.strip():
                    data = json.loads(line)  # Should not raise
                    self.assertIsInstance(data, dict)

    def test_schema_version_is_1_0_0_in_all_events(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            h.dispatch("sessionEnd", {"sessionId": _SESSION_ID, "reason": "clear"})
            events = h.orchestrator_events("unknown-run")
            for event in events:
                with self.subTest(event_type=event.get("event")):
                    self.assertEqual("1.0.0", event["schema_version"])

    def test_timestamp_is_iso8601_ms_z_in_all_events(self):
        with _GhcpReplayHarness(self.tmp_path) as h:
            h.dispatch("sessionStart", {"sessionId": _SESSION_ID})
            events = h.orchestrator_events("unknown-run")
            for event in events:
                with self.subTest(event_type=event.get("event")):
                    self.assertRegex(event["timestamp"], _ISO8601_MS_Z)


if __name__ == "__main__":
    unittest.main()
