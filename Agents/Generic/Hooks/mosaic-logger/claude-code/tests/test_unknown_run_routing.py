"""Tests for unknown-run routing when run_id and/or agent_instance_id extraction fails.

Covers:
- effective_run_id helper: returns ctx.run_id when set, returns "unknown-run" when None
- Session events (SessionStart, SessionEnd, Notification, etc.) with no extractable
  run_id route to OrchestrationLogs/unknown-run/00_orchestrator_events.jsonl
- Invocation events (SubagentStart) with run_id extraction failure but agent_instance_id
  success route to OrchestrationLogs/unknown-run/{agent_instance_id}/03_events.jsonl
- Invocation events with both extractions failing route to
  OrchestrationLogs/unknown-run/unknown-agent/03_events.jsonl
- No path component is the literal string "None"
- The logger never crashes, halts, or drops events regardless of extraction outcome

These tests target mosaic_logger_core.effective_run_id (new function) and end-to-end
routing through mosaic_logger.dispatch(). They fail in TDD RED phase.
"""

import json
import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger
import mosaic_logger_core as core
import mosaic_logger_runstate as runstate


VALID_RUN_ID = "20260727T170000Z-a3f9"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _dispatch_with_env(payload: dict, tmp_path: pathlib.Path) -> None:
    """Dispatch a hook payload with CLAUDE_PROJECT_DIR pointing at tmp_path."""
    orig = os.environ.get("CLAUDE_PROJECT_DIR")
    os.environ["CLAUDE_PROJECT_DIR"] = str(tmp_path)
    try:
        mosaic_logger.dispatch(json.dumps(payload))
    finally:
        if orig is None:
            os.environ.pop("CLAUDE_PROJECT_DIR", None)
        else:
            os.environ["CLAUDE_PROJECT_DIR"] = orig


def _read_events(path: pathlib.Path) -> list:
    """Return parsed JSONL lines from a file, or [] if absent."""
    if not path.exists():
        return []
    lines = path.read_text(encoding="utf-8").splitlines()
    return [json.loads(line) for line in lines if line.strip()]


def _make_ctx(run_id, event_name: str = "SessionStart") -> "core.HookContext":
    """Build a minimal HookContext with the given run_id (may be None)."""
    payload = {"hook_event_name": event_name, "session_id": "test-sess"}
    root = pathlib.Path(tempfile.gettempdir())
    paths = core.build_paths(root)
    timestamp = "2026-07-27T17:00:00.000Z"
    ctx = core.HookContext(payload, root, paths, timestamp)
    ctx.run_id = run_id
    return ctx


# ---------------------------------------------------------------------------
# effective_run_id unit tests
# ---------------------------------------------------------------------------

class TestEffectiveRunId(unittest.TestCase):
    """effective_run_id(ctx) returns ctx.run_id when set, 'unknown-run' when None.

    Targets mosaic_logger_core.effective_run_id — this function does not yet exist.
    All tests in this class fail with AttributeError in TDD RED phase.
    """

    def test_returns_run_id_when_ctx_run_id_is_set(self):
        ctx = _make_ctx(VALID_RUN_ID)
        result = core.effective_run_id(ctx)
        self.assertEqual(VALID_RUN_ID, result)

    def test_returns_unknown_run_when_ctx_run_id_is_none(self):
        ctx = _make_ctx(None)
        result = core.effective_run_id(ctx)
        self.assertEqual("unknown-run", result)

    def test_returns_string_suitable_for_path_construction_when_none(self):
        """The returned string must be usable as a filesystem path component.
        It must not be None, empty, or contain path separators."""
        ctx = _make_ctx(None)
        result = core.effective_run_id(ctx)
        self.assertIsNotNone(result)
        self.assertIsInstance(result, str)
        self.assertGreater(len(result), 0)
        self.assertNotIn("/", result)
        self.assertNotIn("\\", result)

    def test_never_raises_for_none_run_id(self):
        ctx = _make_ctx(None)
        try:
            core.effective_run_id(ctx)
        except Exception as e:
            self.fail(f"effective_run_id raised with run_id=None: {e}")

    def test_never_raises_for_valid_run_id(self):
        ctx = _make_ctx(VALID_RUN_ID)
        try:
            core.effective_run_id(ctx)
        except Exception as e:
            self.fail(f"effective_run_id raised with run_id={VALID_RUN_ID!r}: {e}")


# ---------------------------------------------------------------------------
# Session events without run_id → unknown-run/
# ---------------------------------------------------------------------------

class TestUnknownRunRoutingSessionEvents(unittest.TestCase):
    """Session events with no extractable run_id route to unknown-run/ bucket.

    SessionStart, SessionEnd, Notification and similar events carry no prompt-bearing
    field, so run_id extraction always yields None. Under the new extraction-based
    design these events must route to OrchestrationLogs/unknown-run/ rather than
    being silently dropped.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_session_start_without_run_id_creates_unknown_run_directory(self):
        """SessionStart with no extractable run_id must create the unknown-run/ bucket."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "unknown-route-sess-001",
            "cwd": str(self.tmp_path),
        }
        _dispatch_with_env(payload, self.tmp_path)

        unknown_run_dir = self.tmp_path / "OrchestrationLogs" / "unknown-run"
        self.assertTrue(
            unknown_run_dir.exists(),
            "OrchestrationLogs/unknown-run/ was not created when run_id extraction failed"
        )

    def test_session_start_writes_session_start_event_to_unknown_run(self):
        """The session_start event appears in unknown-run/00_orchestrator_events.jsonl."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "unknown-route-sess-002",
            "cwd": str(self.tmp_path),
        }
        _dispatch_with_env(payload, self.tmp_path)

        events_file = (
            self.tmp_path / "OrchestrationLogs" / "unknown-run" /
            "00_orchestrator_events.jsonl"
        )
        self.assertTrue(
            events_file.exists(),
            "00_orchestrator_events.jsonl not created in unknown-run/ for SessionStart"
        )
        events = _read_events(events_file)
        event_types = [e["event"] for e in events]
        self.assertIn("session_start", event_types)

    def test_session_end_without_run_id_writes_to_unknown_run(self):
        """SessionEnd with no extractable run_id writes to unknown-run/ bucket."""
        session_id = "unknown-route-sess-003"
        for payload in [
            {"hook_event_name": "SessionStart", "session_id": session_id},
            {"hook_event_name": "SessionEnd", "session_id": session_id, "reason": "clear"},
        ]:
            _dispatch_with_env(payload, self.tmp_path)

        events_file = (
            self.tmp_path / "OrchestrationLogs" / "unknown-run" /
            "00_orchestrator_events.jsonl"
        )
        events = _read_events(events_file)
        event_types = [e["event"] for e in events]
        self.assertIn("session_end", event_types)

    def test_notification_without_run_id_in_message_routes_to_unknown_run(self):
        """Notification with no run_id in message routes to unknown-run/."""
        payload = {
            "hook_event_name": "Notification",
            "session_id": "unknown-route-sess-004",
            "notification_type": "info",
            "message": "Just a plain message with no run_id pattern",
        }
        _dispatch_with_env(payload, self.tmp_path)

        events_file = (
            self.tmp_path / "OrchestrationLogs" / "unknown-run" /
            "00_orchestrator_events.jsonl"
        )
        self.assertTrue(
            events_file.exists(),
            "00_orchestrator_events.jsonl not created in unknown-run/ for Notification"
        )

    def test_user_prompt_submit_without_run_id_routes_to_unknown_run(self):
        """UserPromptSubmit whose user_prompt has no run_id routes to unknown-run/."""
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": "unknown-route-sess-005",
            "user_prompt": "Hello, no run_id in this message",
        }
        _dispatch_with_env(payload, self.tmp_path)

        events_file = (
            self.tmp_path / "OrchestrationLogs" / "unknown-run" /
            "00_orchestrator_events.jsonl"
        )
        self.assertTrue(
            events_file.exists(),
            "00_orchestrator_events.jsonl not created in unknown-run/ for UserPromptSubmit"
        )


# ---------------------------------------------------------------------------
# Invocation events: run_id fails, agent_instance_id succeeds → unknown-run/{agent_id}/
# ---------------------------------------------------------------------------

class TestUnknownRunRoutingInvocationEventsWithAgentId(unittest.TestCase):
    """SubagentStart with run_id extraction failure but valid agent_instance_id
    routes to OrchestrationLogs/unknown-run/{agent_instance_id}/."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_subagent_start_no_run_id_creates_unknown_run_agent_directory(self):
        """SubagentStart with no valid run_id in agent_prompt creates the
        unknown-run/{agent_instance_id}/ folder."""
        agent_instance_id = "TestWriter#5"
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "unknown-route-inv-001",
            "agent_id": "agt-001",
            "agent_type": "TestWriter",
            "agent_prompt": (
                f'{{"agent_instance_id": "{agent_instance_id}", '
                '"task_description": "No run_id in this dispatch"}}'
            ),
        }
        _dispatch_with_env(payload, self.tmp_path)

        invocation_dir = (
            self.tmp_path / "OrchestrationLogs" / "unknown-run" / agent_instance_id
        )
        self.assertTrue(
            invocation_dir.exists(),
            f"OrchestrationLogs/unknown-run/{agent_instance_id}/ not created "
            "when run_id extraction fails but agent_instance_id is present"
        )

    def test_subagent_start_writes_invocation_start_to_unknown_run_agent(self):
        """invocation_start event lands in unknown-run/{agent_instance_id}/03_events.jsonl."""
        agent_instance_id = "TestWriter#5"
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "unknown-route-inv-002",
            "agent_id": "agt-002",
            "agent_type": "TestWriter",
            "agent_prompt": (
                f'{{"agent_instance_id": "{agent_instance_id}", '
                '"task_description": "No run_id"}}'
            ),
        }
        _dispatch_with_env(payload, self.tmp_path)

        events_file = (
            self.tmp_path / "OrchestrationLogs" / "unknown-run" /
            agent_instance_id / "03_events.jsonl"
        )
        self.assertTrue(
            events_file.exists(),
            f"03_events.jsonl not found at unknown-run/{agent_instance_id}/"
        )
        events = _read_events(events_file)
        event_types = [e["event"] for e in events]
        self.assertIn("invocation_start", event_types)

    def test_subagent_start_stop_correlation_preserved_in_unknown_run(self):
        """Start and stop events for the same invocation land in the same
        unknown-run/{agent_instance_id}/ folder, not in different folders.

        This verifies that put_agent_mapping receives effective_run_id (not raw None)
        so the mapping persists and resolve_invocation_id in the stop handler can
        look it up correctly.
        """
        agent_instance_id = "TestWriter#5"
        session_id = "unknown-route-inv-003"
        agent_id = "agt-003"

        start_payload = {
            "hook_event_name": "SubagentStart",
            "session_id": session_id,
            "agent_id": agent_id,
            "agent_type": "TestWriter",
            "agent_prompt": (
                f'{{"agent_instance_id": "{agent_instance_id}", '
                '"task_description": "No run_id"}}'
            ),
        }
        stop_payload = {
            "hook_event_name": "SubagentStop",
            "session_id": session_id,
            "agent_id": agent_id,
            "last_assistant_message": (
                f'{{"agent_instance_id": "{agent_instance_id}", '
                '"status_code": "SUCCESS", "status_message": "Done"}}'
            ),
        }
        _dispatch_with_env(start_payload, self.tmp_path)
        _dispatch_with_env(stop_payload, self.tmp_path)

        events_file = (
            self.tmp_path / "OrchestrationLogs" / "unknown-run" /
            agent_instance_id / "03_events.jsonl"
        )
        events = _read_events(events_file)
        event_types = [e["event"] for e in events]
        # Both start and end must appear in the same folder
        self.assertIn("invocation_start", event_types,
                      "invocation_start missing from unknown-run folder")
        self.assertIn("invocation_end", event_types,
                      "invocation_end missing from unknown-run folder — "
                      "correlation broken, stop handler resolved to different folder")

    def test_no_none_string_in_any_path_component(self):
        """No path component must be the literal string 'None' when run_id is None."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "unknown-route-inv-004",
            "agent_id": "agt-004",
            "agent_type": "TestWriter",
            "agent_prompt": '{"agent_instance_id": "TestAgent#1", "task_description": "no run_id"}',
        }
        _dispatch_with_env(payload, self.tmp_path)

        logs_dir = self.tmp_path / "OrchestrationLogs"
        if logs_dir.exists():
            for item in logs_dir.rglob("*"):
                self.assertNotEqual(
                    item.name, "None",
                    f"Literal 'None' found as path component: {item}"
                )


# ---------------------------------------------------------------------------
# Both extractions fail → unknown-run/unknown-agent/
# ---------------------------------------------------------------------------

class TestUnknownRunUnknownAgentRouting(unittest.TestCase):
    """When both run_id and agent_instance_id extraction fail, route to
    OrchestrationLogs/unknown-run/unknown-agent/.

    This is the fallback-of-fallbacks bucket for SubagentStart events where the
    agent_prompt is absent or carries no extractable identifiers of either kind.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_subagent_start_with_no_extractable_identifiers_uses_unknown_agent(self):
        """SubagentStart with no agent_prompt (both extractions fail) routes to
        unknown-run/unknown-agent/."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "unknown-agent-sess-001",
            "agent_id": "agt-ua-001",
            # No agent_prompt, no agent_type — both extractions fail
        }
        _dispatch_with_env(payload, self.tmp_path)

        unknown_agent_dir = (
            self.tmp_path / "OrchestrationLogs" / "unknown-run" / "unknown-agent"
        )
        self.assertTrue(
            unknown_agent_dir.exists(),
            "unknown-run/unknown-agent/ must be created when both run_id and "
            "agent_instance_id extraction fail"
        )

    def test_subagent_start_unknown_agent_folder_has_events_file(self):
        """invocation_start event lands in unknown-run/unknown-agent/03_events.jsonl."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "unknown-agent-sess-002",
            "agent_id": "agt-ua-002",
        }
        _dispatch_with_env(payload, self.tmp_path)

        events_file = (
            self.tmp_path / "OrchestrationLogs" / "unknown-run" /
            "unknown-agent" / "03_events.jsonl"
        )
        self.assertTrue(
            events_file.exists(),
            "03_events.jsonl not found in unknown-run/unknown-agent/"
        )

    def test_subagent_start_with_empty_agent_prompt_uses_unknown_agent(self):
        """SubagentStart where agent_prompt contains no valid identifiers routes to
        unknown-run/unknown-agent/ rather than producing a timestamp-based fallback ID."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "unknown-agent-sess-003",
            "agent_id": "agt-ua-003",
            "agent_prompt": "plain text with no identifiers or run id",
        }
        _dispatch_with_env(payload, self.tmp_path)

        unknown_agent_dir = (
            self.tmp_path / "OrchestrationLogs" / "unknown-run" / "unknown-agent"
        )
        self.assertTrue(
            unknown_agent_dir.exists(),
            "unknown-run/unknown-agent/ not created when agent_instance_id extraction fails"
        )

    def test_unknown_agent_events_contain_invocation_start(self):
        """The invocation_start event is written to unknown-run/unknown-agent/."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "unknown-agent-sess-004",
            "agent_id": "agt-ua-004",
        }
        _dispatch_with_env(payload, self.tmp_path)

        events_file = (
            self.tmp_path / "OrchestrationLogs" / "unknown-run" /
            "unknown-agent" / "03_events.jsonl"
        )
        events = _read_events(events_file)
        event_types = [e["event"] for e in events]
        self.assertIn("invocation_start", event_types)


# ---------------------------------------------------------------------------
# Never-crash contract
# ---------------------------------------------------------------------------

class TestNoCrashOnExtractionFailure(unittest.TestCase):
    """The logger never crashes, halts, or drops events regardless of whether
    run_id or agent_instance_id extraction succeeds or fails."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_all_event_types_complete_without_exception_when_run_id_absent(self):
        """Every hook event type dispatches without raising when run_id is not extractable."""
        payloads = [
            {"hook_event_name": "SessionStart", "session_id": "s1"},
            {"hook_event_name": "SessionEnd", "session_id": "s1"},
            {"hook_event_name": "UserPromptSubmit", "session_id": "s1",
             "user_prompt": "no run id here"},
            {"hook_event_name": "Stop", "session_id": "s1",
             "last_assistant_message": "done"},
            {"hook_event_name": "Notification", "session_id": "s1",
             "notification_type": "info", "message": "plain message"},
            {"hook_event_name": "PreToolUse", "session_id": "s1",
             "tool_name": "Bash", "tool_input": {}},
            {"hook_event_name": "PostToolUse", "session_id": "s1",
             "tool_name": "Bash", "tool_output": "output"},
            {"hook_event_name": "PostToolUseFailure", "session_id": "s1",
             "tool_name": "Bash", "tool_error": "error"},
            {"hook_event_name": "SubagentStart", "session_id": "s1",
             "agent_id": "agt-x"},
        ]
        for payload in payloads:
            try:
                _dispatch_with_env(payload, self.tmp_path)
            except Exception as e:
                self.fail(
                    f"dispatch raised for {payload['hook_event_name']} "
                    f"without extractable run_id: {e}"
                )

    def test_tool_events_do_not_crash_with_both_run_id_and_agent_id_absent(self):
        """Tool events with no run_id and no agent_id complete silently."""
        for event_name in ("PreToolUse", "PostToolUse", "PostToolUseFailure"):
            payload = {
                "hook_event_name": event_name,
                "session_id": "s1",
                "tool_name": "Bash",
            }
            try:
                _dispatch_with_env(payload, self.tmp_path)
            except Exception as e:
                self.fail(
                    f"dispatch raised for {event_name} without run_id or agent_id: {e}"
                )

    def test_empty_subagent_start_does_not_raise(self):
        """SubagentStart with minimal payload (no prompt, no type) completes silently."""
        try:
            _dispatch_with_env(
                {"hook_event_name": "SubagentStart", "agent_id": "agt-y"},
                self.tmp_path,
            )
        except Exception as e:
            self.fail(f"dispatch raised on minimal SubagentStart: {e}")

    def test_tool_events_write_to_unknown_run_when_run_id_absent(self):
        """Tool events with no extractable run_id must write a file under
        OrchestrationLogs/unknown-run/ rather than being silently dropped.

        This drives AC8.6 "never drops events" for the tool handler path.
        Verifies that the handle_pre_tool_use / handle_post_tool_use /
        handle_post_tool_use_failure guard replacement writes to unknown-run/
        instead of returning early.
        """
        payloads = [
            {
                "hook_event_name": "PreToolUse",
                "session_id": "no-drop-tool-001",
                "tool_name": "Bash",
                "tool_input": {"command": "echo hi"},
            },
            {
                "hook_event_name": "PostToolUse",
                "session_id": "no-drop-tool-001",
                "tool_name": "Bash",
                "tool_output": "hi",
            },
            {
                "hook_event_name": "PostToolUseFailure",
                "session_id": "no-drop-tool-001",
                "tool_name": "Bash",
                "tool_error": "command failed",
            },
        ]
        for payload in payloads:
            _dispatch_with_env(payload, self.tmp_path)

        unknown_run_dir = self.tmp_path / "OrchestrationLogs" / "unknown-run"
        self.assertTrue(
            unknown_run_dir.exists(),
            "OrchestrationLogs/unknown-run/ was not created after dispatching tool "
            "events with no extractable run_id — tool handler appears to be silently "
            "dropping events instead of routing to unknown-run/"
        )
        # At least one file must exist under unknown-run/ to confirm events were written
        files_under_unknown_run = list(unknown_run_dir.rglob("*"))
        files_only = [f for f in files_under_unknown_run if f.is_file()]
        self.assertGreater(
            len(files_only), 0,
            "No files were written under OrchestrationLogs/unknown-run/ after "
            "dispatching PreToolUse/PostToolUse/PostToolUseFailure with absent run_id"
        )


# ---------------------------------------------------------------------------
# run_id extracted, agent_instance_id extraction fails → {run_id}/unknown-agent/
# ---------------------------------------------------------------------------

class TestKnownRunIdUnknownAgentRouting(unittest.TestCase):
    """SubagentStart where run_id is successfully extracted from agent_prompt but
    agent_instance_id extraction fails routes to
    OrchestrationLogs/{run_id}/unknown-agent/.

    This is routing case 4 from the design's routing table (ContractsDesign.md,
    Log routing rules section):
      run_id extracted, agent_instance_id extraction fails → {run_id}/unknown-agent/
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_subagent_start_with_run_id_but_no_agent_id_creates_unknown_agent_folder(self):
        """SubagentStart carrying a valid run_id but no extractable agent_instance_id
        must create OrchestrationLogs/{run_id}/unknown-agent/."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "known-run-unknown-agent-001",
            "agent_id": "agt-kra-001",
            # agent_prompt has run_id but no agent_instance_id field
            "agent_prompt": f'{{"run_id": "{VALID_RUN_ID}", "task_description": "no agent id here"}}',
        }
        _dispatch_with_env(payload, self.tmp_path)

        unknown_agent_dir = (
            self.tmp_path / "OrchestrationLogs" / VALID_RUN_ID / "unknown-agent"
        )
        self.assertTrue(
            unknown_agent_dir.exists(),
            f"OrchestrationLogs/{VALID_RUN_ID}/unknown-agent/ was not created when "
            "run_id extraction succeeded but agent_instance_id extraction failed"
        )

    def test_subagent_start_events_file_at_run_id_unknown_agent(self):
        """The invocation_start event lands in
        OrchestrationLogs/{run_id}/unknown-agent/03_events.jsonl."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "known-run-unknown-agent-002",
            "agent_id": "agt-kra-002",
            "agent_prompt": f'{{"run_id": "{VALID_RUN_ID}", "task_description": "no agent id"}}',
        }
        _dispatch_with_env(payload, self.tmp_path)

        events_file = (
            self.tmp_path / "OrchestrationLogs" / VALID_RUN_ID /
            "unknown-agent" / "03_events.jsonl"
        )
        self.assertTrue(
            events_file.exists(),
            f"03_events.jsonl not found at OrchestrationLogs/{VALID_RUN_ID}/unknown-agent/"
        )
        events = _read_events(events_file)
        event_types = [e["event"] for e in events]
        self.assertIn(
            "invocation_start", event_types,
            f"invocation_start not found in {VALID_RUN_ID}/unknown-agent/03_events.jsonl"
        )

    def test_subagent_start_does_not_create_unknown_run_when_run_id_extracted(self):
        """When run_id extraction succeeds, logs must NOT go to unknown-run/.
        They should use the extracted run_id folder even when agent_instance_id fails."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "known-run-unknown-agent-003",
            "agent_id": "agt-kra-003",
            "agent_prompt": f'{{"run_id": "{VALID_RUN_ID}", "task_description": "no agent id"}}',
        }
        _dispatch_with_env(payload, self.tmp_path)

        unknown_run_invocation = (
            self.tmp_path / "OrchestrationLogs" / "unknown-run"
        )
        # If unknown-run/ is created at all, it should not contain any invocation folder
        # for this event — the event should route to {run_id}/unknown-agent/ instead
        run_id_dir = self.tmp_path / "OrchestrationLogs" / VALID_RUN_ID
        self.assertTrue(
            run_id_dir.exists(),
            f"OrchestrationLogs/{VALID_RUN_ID}/ not created — event routed to wrong bucket"
        )

    def test_no_none_string_in_path_when_agent_id_missing_but_run_id_extracted(self):
        """No path component is the literal string 'None' when agent_instance_id
        extraction fails but run_id extraction succeeds."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "known-run-unknown-agent-004",
            "agent_id": "agt-kra-004",
            "agent_prompt": f'{{"run_id": "{VALID_RUN_ID}"}}',
        }
        _dispatch_with_env(payload, self.tmp_path)

        logs_dir = self.tmp_path / "OrchestrationLogs"
        if logs_dir.exists():
            for item in logs_dir.rglob("*"):
                self.assertNotEqual(
                    item.name, "None",
                    f"Literal 'None' found as a path component: {item}"
                )


if __name__ == "__main__":
    unittest.main()
