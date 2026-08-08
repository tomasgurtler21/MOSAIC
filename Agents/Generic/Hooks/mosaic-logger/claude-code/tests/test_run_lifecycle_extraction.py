"""Tests for the extraction-based run lifecycle without marker files.

Covers:
- resolve_run_identity: per-event extraction from event-specific prompt fields
  (SubagentStart from agent_prompt, UserPromptSubmit from user_prompt, Stop and
  SubagentStop from last_assistant_message, Notification from message)
- Events with no prompt-bearing field (SessionStart, SessionEnd, PreToolUse,
  PostToolUse, PostToolUseFailure, PreCompact, PostCompact) yield ctx.run_id = None
- End-to-end: SubagentStart with run_id in agent_prompt writes logs to
  OrchestrationLogs/{run_id}/{agent_instance_id}/ (not to a marker-minted run_id)
- run_start emitted on SessionStart (event-based, not mint-outcome-based)
- run_end emitted on SessionEnd (event-based, not close-outcome-based)
- Negative: .run-markers/ directory is never created or consulted after the
  marker mechanism is removed

These tests target mosaic_logger.resolve_run_identity (new function) and exercise
the end-to-end dispatch path. They fail in TDD RED phase.
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
    """Return parsed JSONL lines from a .jsonl file, or [] if absent."""
    if not path.exists():
        return []
    lines = path.read_text(encoding="utf-8").splitlines()
    return [json.loads(line) for line in lines if line.strip()]


# ---------------------------------------------------------------------------
# resolve_run_identity — per-event extraction unit tests
# ---------------------------------------------------------------------------

class TestResolveRunIdentityPerEvent(unittest.TestCase):
    """resolve_run_identity sets ctx.run_id by extracting from event-specific
    prompt fields, or leaves it None for events with no prompt-bearing field.

    Targets mosaic_logger.resolve_run_identity — this function does not yet exist.
    All tests in this class fail with AttributeError in TDD RED phase.

    Each test gets its own isolated workspace root and a session_id unique to
    the test method: text extraction now durably binds the session id to a
    run id (Design A4's adopt_run_id), so sharing either a tempdir or a
    session_id literal across methods would let one test's binding leak into
    another's resolution.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def _make_ctx(self, event_name: str, session_id: str, **fields) -> "core.HookContext":
        """Build a HookContext isolated to this test's own tempdir and session_id."""
        payload = {"hook_event_name": event_name, "session_id": session_id}
        payload.update(fields)
        timestamp = "2026-07-27T17:00:00.000Z"
        return core.HookContext(payload, self.tmp_path, self.paths, timestamp)

    def test_subagent_start_extracts_from_agent_prompt(self):
        """SubagentStart: run_id is extracted from the agent_prompt field."""
        ctx = self._make_ctx(
            "SubagentStart", "per-event-subagent-start",
            agent_prompt=(
                f'{{"agent_instance_id": "TestAgent#1", '
                f'"run_id": "{VALID_RUN_ID}", '
                '"task_description": "some task"}}'
            ),
        )
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_subagent_stop_extracts_from_last_assistant_message(self):
        """SubagentStop: run_id is extracted from the last_assistant_message field."""
        ctx = self._make_ctx(
            "SubagentStop", "per-event-subagent-stop",
            last_assistant_message=(
                f'{{"agent_instance_id": "TestAgent#1", '
                f'"run_id": "{VALID_RUN_ID}", '
                '"status_code": "SUCCESS", "status_message": "Done"}}'
            ),
        )
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_user_prompt_submit_extracts_from_user_prompt(self):
        """UserPromptSubmit: run_id is extracted from the user_prompt field."""
        ctx = self._make_ctx(
            "UserPromptSubmit", "per-event-user-prompt-submit",
            user_prompt=f'"run_id": "{VALID_RUN_ID}" — process this please',
        )
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_stop_extracts_from_last_assistant_message(self):
        """Stop: run_id is extracted from the last_assistant_message field."""
        ctx = self._make_ctx(
            "Stop", "per-event-stop",
            last_assistant_message=(
                f'{{"run_id": "{VALID_RUN_ID}", '
                '"status_code": "SUCCESS", "status_message": "Done"}}'
            ),
        )
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_notification_extracts_from_message_field(self):
        """Notification: run_id is extracted from the message field."""
        ctx = self._make_ctx(
            "Notification", "per-event-notification",
            notification_type="info",
            message=f"Dispatch complete for run_id: {VALID_RUN_ID}",
        )
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_session_start_yields_none_no_prompt_bearing_field(self):
        """SessionStart has no prompt-bearing field; ctx.run_id stays None after
        resolve_run_identity. This is expected and accepted behavior."""
        ctx = self._make_ctx("SessionStart", "per-event-session-start")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_session_end_yields_none(self):
        """SessionEnd has no prompt-bearing field; ctx.run_id stays None."""
        ctx = self._make_ctx("SessionEnd", "per-event-session-end")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_pre_tool_use_yields_none(self):
        """PreToolUse has no prompt-bearing field; ctx.run_id stays None."""
        ctx = self._make_ctx("PreToolUse", "per-event-pre-tool-use",
                              tool_name="Bash", tool_input={})
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_post_tool_use_yields_none(self):
        """PostToolUse has no prompt-bearing field; ctx.run_id stays None."""
        ctx = self._make_ctx("PostToolUse", "per-event-post-tool-use",
                              tool_name="Bash", tool_output="output")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_post_tool_use_failure_yields_none(self):
        """PostToolUseFailure has no prompt-bearing field; ctx.run_id stays None."""
        ctx = self._make_ctx("PostToolUseFailure", "per-event-post-tool-use-failure",
                              tool_name="Bash", tool_error="err")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_pre_compact_yields_none(self):
        """PreCompact has no prompt-bearing field; ctx.run_id stays None."""
        ctx = self._make_ctx("PreCompact", "per-event-pre-compact")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_post_compact_yields_none(self):
        """PostCompact has no prompt-bearing field; ctx.run_id stays None."""
        ctx = self._make_ctx("PostCompact", "per-event-post-compact")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_subagent_start_with_no_run_id_in_prompt_yields_none(self):
        """SubagentStart where agent_prompt has no valid run_id yields None."""
        ctx = self._make_ctx(
            "SubagentStart", "per-event-subagent-start-no-run-id",
            agent_prompt='{"agent_instance_id": "TestAgent#1", "task_description": "no run id"}',
        )
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_subagent_start_with_none_agent_prompt_yields_none(self):
        """SubagentStart where agent_prompt is absent yields None without raising."""
        ctx = self._make_ctx("SubagentStart", "per-event-subagent-start-none-prompt")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_resolve_run_identity_never_raises(self):
        """resolve_run_identity must never raise regardless of payload content."""
        ctx = self._make_ctx("SubagentStart", "per-event-never-raises", agent_prompt=None)
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as e:
            self.fail(f"resolve_run_identity raised: {e}")

    def test_resolve_run_identity_never_raises_for_malformed_content(self):
        """resolve_run_identity is robust against malformed prompt content."""
        ctx = self._make_ctx(
            "UserPromptSubmit", "per-event-never-raises-malformed",
            user_prompt="\x00\x01 {broken",
        )
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as e:
            self.fail(f"resolve_run_identity raised on malformed content: {e}")


# ---------------------------------------------------------------------------
# End-to-end: extraction routes to correct {run_id}/ folder
# ---------------------------------------------------------------------------

class TestExtractionBasedDispatch(unittest.TestCase):
    """End-to-end tests: when run_id is extracted from dispatch content, logs are
    written to OrchestrationLogs/{run_id}/ (the extracted folder, not a minted one)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_subagent_start_with_run_id_writes_to_extracted_run_folder(self):
        """SubagentStart where agent_prompt contains a valid run_id must write logs
        to OrchestrationLogs/{extracted_run_id}/{agent_instance_id}/."""
        agent_instance_id = "TestWriter#5"
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "extraction-sess-001",
            "agent_id": "agt-extract-001",
            "agent_type": "TestWriter",
            "agent_prompt": (
                f'{{"agent_instance_id": "{agent_instance_id}", '
                f'"run_id": "{VALID_RUN_ID}", '
                '"task_description": "Write tests"}}'
            ),
        }
        _dispatch_with_env(payload, self.tmp_path)

        events_file = (
            self.tmp_path / "OrchestrationLogs" / VALID_RUN_ID /
            agent_instance_id / "03_events.jsonl"
        )
        self.assertTrue(
            events_file.exists(),
            f"03_events.jsonl not found at OrchestrationLogs/{VALID_RUN_ID}/{agent_instance_id}/"
        )

    def test_invocation_start_event_carries_extracted_run_id(self):
        """The run_id field in the emitted invocation_start event matches the extracted
        run_id, not a marker-minted one."""
        agent_instance_id = "TestWriter#5"
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "extraction-sess-002",
            "agent_id": "agt-extract-002",
            "agent_type": "TestWriter",
            "agent_prompt": (
                f'{{"agent_instance_id": "{agent_instance_id}", '
                f'"run_id": "{VALID_RUN_ID}", '
                '"task_description": "Write tests"}}'
            ),
        }
        _dispatch_with_env(payload, self.tmp_path)

        events_file = (
            self.tmp_path / "OrchestrationLogs" / VALID_RUN_ID /
            agent_instance_id / "03_events.jsonl"
        )
        events = _read_events(events_file)
        invocation_start = next(
            (e for e in events if e["event"] == "invocation_start"), None
        )
        self.assertIsNotNone(invocation_start, "invocation_start event not found")
        self.assertEqual(
            VALID_RUN_ID, invocation_start.get("run_id"),
            "run_id in invocation_start event does not match the extracted run_id"
        )

    def test_user_prompt_submit_with_run_id_writes_to_extracted_folder(self):
        """UserPromptSubmit where user_prompt contains a valid run_id writes to the
        extracted run_id's orchestrator events file."""
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": "extraction-sess-003",
            "user_prompt": f'"run_id": "{VALID_RUN_ID}" -- please process this task',
        }
        _dispatch_with_env(payload, self.tmp_path)

        events_file = (
            self.tmp_path / "OrchestrationLogs" / VALID_RUN_ID /
            "00_orchestrator_events.jsonl"
        )
        self.assertTrue(
            events_file.exists(),
            f"00_orchestrator_events.jsonl not found in OrchestrationLogs/{VALID_RUN_ID}/"
        )

    def test_logs_go_only_to_extracted_run_id_folder_not_to_other_folders(self):
        """When run_id is extracted from a dispatch, no other run_id folder is created.
        Specifically, no randomly-minted run_id folder should appear."""
        another_run_id = "20260101T120000Z-bcde"
        agent_instance_id = "TestWriter#5"
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "extraction-sess-004",
            "agent_id": "agt-extract-004",
            "agent_type": "TestWriter",
            "agent_prompt": (
                f'{{"agent_instance_id": "{agent_instance_id}", '
                f'"run_id": "{VALID_RUN_ID}", '
                '"task_description": "Write tests"}}'
            ),
        }
        _dispatch_with_env(payload, self.tmp_path)

        wrong_folder = self.tmp_path / "OrchestrationLogs" / another_run_id
        self.assertFalse(
            wrong_folder.exists(),
            f"An unexpected run_id folder was created: {another_run_id}/"
        )

    def test_input_artifact_written_to_extracted_run_id_folder(self):
        """The 01_input.md artifact is written into the extracted run_id folder."""
        agent_instance_id = "TestWriter#5"
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "extraction-sess-005",
            "agent_id": "agt-extract-005",
            "agent_type": "TestWriter",
            "agent_prompt": (
                f'{{"agent_instance_id": "{agent_instance_id}", '
                f'"run_id": "{VALID_RUN_ID}", '
                '"task_description": "Write tests"}}'
            ),
        }
        _dispatch_with_env(payload, self.tmp_path)

        input_file = (
            self.tmp_path / "OrchestrationLogs" / VALID_RUN_ID /
            agent_instance_id / "01_input.md"
        )
        self.assertTrue(
            input_file.exists(),
            f"01_input.md not found in OrchestrationLogs/{VALID_RUN_ID}/{agent_instance_id}/"
        )


# ---------------------------------------------------------------------------
# run_start / run_end: event-based emission
# ---------------------------------------------------------------------------

class TestRunStartRunEndEventBased(unittest.TestCase):
    """run_start is emitted on SessionStart; run_end is emitted on SessionEnd.
    Emission is event-based, not tied to marker mint/close outcomes.

    SessionStart and SessionEnd carry no prompt-bearing field, so they cannot
    extract a run_id. They land in unknown-run/. The lifecycle events (run_start,
    run_end) must still be emitted there.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_session_start_triggers_run_start_emission(self):
        """run_start must be emitted when SessionStart fires, regardless of run_id
        extraction success. It lands in the effective_run_id bucket."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "lifecycle-sess-001",
            "cwd": str(self.tmp_path),
        }
        _dispatch_with_env(payload, self.tmp_path)

        # run_start lands wherever effective_run_id routes it; search the whole tree
        logs_dir = self.tmp_path / "OrchestrationLogs"
        run_start_found = False
        if logs_dir.exists():
            for jsonl_file in logs_dir.rglob("*.jsonl"):
                events = _read_events(jsonl_file)
                if any(e.get("event") == "run_start" for e in events):
                    run_start_found = True
                    break
        self.assertTrue(
            run_start_found,
            "run_start event not found anywhere in log tree after SessionStart"
        )

    def test_session_end_triggers_run_end_emission(self):
        """run_end must be emitted when SessionEnd fires, regardless of run_id
        extraction success."""
        session_id = "lifecycle-sess-002"
        for payload in [
            {"hook_event_name": "SessionStart", "session_id": session_id},
            {"hook_event_name": "SessionEnd", "session_id": session_id, "reason": "clear"},
        ]:
            _dispatch_with_env(payload, self.tmp_path)

        logs_dir = self.tmp_path / "OrchestrationLogs"
        run_end_found = False
        if logs_dir.exists():
            for jsonl_file in logs_dir.rglob("*.jsonl"):
                events = _read_events(jsonl_file)
                if any(e.get("event") == "run_end" for e in events):
                    run_end_found = True
                    break
        self.assertTrue(
            run_end_found,
            "run_end event not found anywhere in log tree after SessionEnd"
        )

    def test_run_start_precedes_session_start_in_event_stream(self):
        """run_start must appear before session_start in the same event stream,
        preserving the outer-scope ordering."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "lifecycle-sess-003",
            "cwd": str(self.tmp_path),
        }
        _dispatch_with_env(payload, self.tmp_path)

        # Find the file that contains both events
        logs_dir = self.tmp_path / "OrchestrationLogs"
        found_file = None
        if logs_dir.exists():
            for jsonl_file in logs_dir.rglob("*.jsonl"):
                events = _read_events(jsonl_file)
                types = [e.get("event") for e in events]
                if "run_start" in types and "session_start" in types:
                    found_file = (jsonl_file, types)
                    break

        self.assertIsNotNone(
            found_file,
            "No file contains both run_start and session_start events"
        )
        jsonl_file, types = found_file
        self.assertLess(
            types.index("run_start"), types.index("session_start"),
            "run_start must precede session_start in the event stream"
        )

    def test_run_end_follows_session_end_in_event_stream(self):
        """run_end must appear after session_end in the same event stream."""
        session_id = "lifecycle-sess-004"
        for payload in [
            {"hook_event_name": "SessionStart", "session_id": session_id},
            {"hook_event_name": "SessionEnd", "session_id": session_id, "reason": "clear"},
        ]:
            _dispatch_with_env(payload, self.tmp_path)

        logs_dir = self.tmp_path / "OrchestrationLogs"
        found_file = None
        if logs_dir.exists():
            for jsonl_file in logs_dir.rglob("*.jsonl"):
                events = _read_events(jsonl_file)
                types = [e.get("event") for e in events]
                if "session_end" in types and "run_end" in types:
                    found_file = (jsonl_file, types)
                    break

        self.assertIsNotNone(
            found_file,
            "No file contains both session_end and run_end events"
        )
        jsonl_file, types = found_file
        self.assertLess(
            types.index("session_end"), types.index("run_end"),
            "session_end must precede run_end in the event stream"
        )

    def test_run_end_carries_outcome_from_session_end_reason(self):
        """run_end.outcome reflects the reason field from the SessionEnd payload."""
        session_id = "lifecycle-sess-005"
        for payload in [
            {"hook_event_name": "SessionStart", "session_id": session_id},
            {"hook_event_name": "SessionEnd", "session_id": session_id, "reason": "clear"},
        ]:
            _dispatch_with_env(payload, self.tmp_path)

        logs_dir = self.tmp_path / "OrchestrationLogs"
        run_end_event = None
        if logs_dir.exists():
            for jsonl_file in logs_dir.rglob("*.jsonl"):
                events = _read_events(jsonl_file)
                candidate = next(
                    (e for e in events if e.get("event") == "run_end"), None
                )
                if candidate:
                    run_end_event = candidate
                    break

        self.assertIsNotNone(run_end_event, "run_end event not found")
        self.assertEqual("clear", run_end_event.get("outcome"))


# ---------------------------------------------------------------------------
# Negative: .run-markers/ directory must never be created
# ---------------------------------------------------------------------------

class TestMarkerDirectoryNeverCreated(unittest.TestCase):
    """The .run-markers/ directory must not be created or consulted after the
    marker mechanism is removed. These are negative tests: they fail as long as
    the old marker-based code is still in place."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_session_start_does_not_create_run_markers_directory(self):
        """Dispatching SessionStart must not create OrchestrationLogs/.run-markers/."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": "marker-neg-sess-001",
            "cwd": str(self.tmp_path),
        }
        _dispatch_with_env(payload, self.tmp_path)

        marker_dir = self.tmp_path / "OrchestrationLogs" / ".run-markers"
        self.assertFalse(
            marker_dir.exists(),
            ".run-markers/ was created — the marker mechanism must be fully removed"
        )

    def test_subagent_start_does_not_create_run_markers_directory(self):
        """Dispatching SubagentStart with a run_id must not create .run-markers/."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "marker-neg-sess-002",
            "agent_id": "agt-marker-001",
            "agent_type": "TestWriter",
            "agent_prompt": (
                f'{{"agent_instance_id": "TestWriter#1", '
                f'"run_id": "{VALID_RUN_ID}", '
                '"task_description": "test"}}'
            ),
        }
        _dispatch_with_env(payload, self.tmp_path)

        marker_dir = self.tmp_path / "OrchestrationLogs" / ".run-markers"
        self.assertFalse(
            marker_dir.exists(),
            ".run-markers/ was created by SubagentStart — marker mechanism must be removed"
        )

    def test_full_session_sequence_does_not_create_run_markers_directory(self):
        """A sequence of SessionStart + SubagentStart + SessionEnd must not create
        the .run-markers/ directory at any point."""
        session_id = "marker-neg-sess-003"
        payloads = [
            {
                "hook_event_name": "SessionStart",
                "session_id": session_id,
                "cwd": str(self.tmp_path),
            },
            {
                "hook_event_name": "SubagentStart",
                "session_id": session_id,
                "agent_id": "agt-marker-002",
                "agent_type": "TestWriter",
                "agent_prompt": (
                    f'{{"agent_instance_id": "TestWriter#2", '
                    f'"run_id": "{VALID_RUN_ID}", '
                    '"task_description": "test"}}'
                ),
            },
            {
                "hook_event_name": "SessionEnd",
                "session_id": session_id,
                "reason": "clear",
            },
        ]
        for payload in payloads:
            _dispatch_with_env(payload, self.tmp_path)

        marker_dir = self.tmp_path / "OrchestrationLogs" / ".run-markers"
        self.assertFalse(
            marker_dir.exists(),
            ".run-markers/ was created during a full session sequence"
        )

    def test_no_run_markers_directory_anywhere_in_log_tree(self):
        """No .run-markers/ directory exists anywhere under OrchestrationLogs/
        after dispatching events."""
        payloads = [
            {"hook_event_name": "SessionStart", "session_id": "marker-neg-sess-004"},
            {"hook_event_name": "Notification", "session_id": "marker-neg-sess-004",
             "notification_type": "info", "message": "test"},
            {"hook_event_name": "SessionEnd", "session_id": "marker-neg-sess-004"},
        ]
        for payload in payloads:
            _dispatch_with_env(payload, self.tmp_path)

        logs_dir = self.tmp_path / "OrchestrationLogs"
        marker_dirs = []
        if logs_dir.exists():
            marker_dirs = [
                item for item in logs_dir.rglob(".run-markers")
                if item.is_dir()
            ]
        self.assertEqual(
            0, len(marker_dirs),
            f"Found .run-markers directories in log tree: {marker_dirs}"
        )


# ---------------------------------------------------------------------------
# resolve_run_identity — binding-first resolution (Stage 2, Design A4)
# ---------------------------------------------------------------------------

class TestResolveRunIdentityBindingFirst(unittest.TestCase):
    """Events with no prompt-bearing field resolve ctx.run_id from an
    existing session-run binding rather than falling back to unknown-run.

    Targets the rewritten mosaic_logger.resolve_run_identity (Design A4)
    together with mosaic_logger_runstate.put_session_run_binding /
    get_session_run_binding (Design A2, Stage 1).
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def _ctx(self, event_name, session_id, timestamp, **fields):
        payload = {"hook_event_name": event_name, "session_id": session_id}
        payload.update(fields)
        return core.HookContext(payload, self.tmp_path, self.paths, timestamp)

    def _bind(self, session_id, run_id, start_time):
        runstate.put_session_run_binding(self.paths, session_id, run_id, start_time)

    def test_session_start_resolves_from_existing_binding(self):
        session_id = "binding-sess-session-start"
        self._bind(session_id, VALID_RUN_ID, "2026-07-27T16:00:00.000Z")
        ctx = self._ctx("SessionStart", session_id, "2026-07-27T17:00:00.000Z")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_session_end_resolves_from_existing_binding(self):
        session_id = "binding-sess-session-end"
        self._bind(session_id, VALID_RUN_ID, "2026-07-27T16:00:00.000Z")
        ctx = self._ctx("SessionEnd", session_id, "2026-07-27T17:00:00.000Z")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_pre_tool_use_resolves_from_existing_binding(self):
        session_id = "binding-sess-pre-tool"
        self._bind(session_id, VALID_RUN_ID, "2026-07-27T16:00:00.000Z")
        ctx = self._ctx("PreToolUse", session_id, "2026-07-27T17:00:00.000Z",
                         tool_name="Bash", tool_input={})
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_post_tool_use_resolves_from_existing_binding(self):
        session_id = "binding-sess-post-tool"
        self._bind(session_id, VALID_RUN_ID, "2026-07-27T16:00:00.000Z")
        ctx = self._ctx("PostToolUse", session_id, "2026-07-27T17:00:00.000Z",
                         tool_name="Bash", tool_output="ok")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_post_tool_use_failure_resolves_from_existing_binding(self):
        session_id = "binding-sess-post-tool-fail"
        self._bind(session_id, VALID_RUN_ID, "2026-07-27T16:00:00.000Z")
        ctx = self._ctx("PostToolUseFailure", session_id, "2026-07-27T17:00:00.000Z",
                         tool_name="Bash", tool_error="boom")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_pre_compact_resolves_from_existing_binding(self):
        session_id = "binding-sess-pre-compact"
        self._bind(session_id, VALID_RUN_ID, "2026-07-27T16:00:00.000Z")
        ctx = self._ctx("PreCompact", session_id, "2026-07-27T17:00:00.000Z")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_post_compact_resolves_from_existing_binding(self):
        session_id = "binding-sess-post-compact"
        self._bind(session_id, VALID_RUN_ID, "2026-07-27T16:00:00.000Z")
        ctx = self._ctx("PostCompact", session_id, "2026-07-27T17:00:00.000Z")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_binding_lookup_respects_firing_timestamp_not_now(self):
        """The binding lookup must use ctx.timestamp (the firing's own
        instant), not wall-clock 'now'. A firing timestamped BEFORE the
        binding's start_time must not resolve to that binding."""
        session_id = "binding-sess-before-start"
        self._bind(session_id, VALID_RUN_ID, "2026-07-27T18:00:00.000Z")
        ctx = self._ctx("PreToolUse", session_id, "2026-07-27T17:00:00.000Z",
                         tool_name="Bash", tool_input={})
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(
            ctx.run_id,
            "A firing before the binding's start_time must not resolve to it",
        )

    def test_no_binding_and_no_prompt_field_still_yields_none(self):
        """Genuine fallback: with no binding recorded for the session at all,
        ctx.run_id stays None for an event with no prompt-bearing field."""
        session_id = "binding-sess-none-at-all"
        ctx = self._ctx("PreToolUse", session_id, "2026-07-27T17:00:00.000Z",
                         tool_name="Bash", tool_input={})
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)


class TestResolveRunIdentityEstablishesBinding(unittest.TestCase):
    """The first firing that carries a run_id in its text establishes the
    session-run binding (via adopt_run_id), so a later firing with no
    prompt-bearing field can resolve it from the binding (Design A2/A4).
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def _ctx(self, event_name, session_id, timestamp, **fields):
        payload = {"hook_event_name": event_name, "session_id": session_id}
        payload.update(fields)
        return core.HookContext(payload, self.tmp_path, self.paths, timestamp)

    def test_subagent_start_extraction_establishes_binding(self):
        session_id = "establish-sess-subagent-start"
        ctx = self._ctx(
            "SubagentStart", session_id, "2026-07-27T17:00:00.000Z",
            agent_prompt=(
                f'{{"agent_instance_id": "TestAgent#1", "run_id": "{VALID_RUN_ID}"}}'
            ),
        )
        mosaic_logger.resolve_run_identity(ctx)
        bound = runstate.get_session_run_binding(
            self.paths, session_id, at="2026-07-27T17:00:01.000Z"
        )
        self.assertEqual(VALID_RUN_ID, bound)

    def test_user_prompt_submit_extraction_establishes_binding(self):
        session_id = "establish-sess-user-prompt"
        ctx = self._ctx(
            "UserPromptSubmit", session_id, "2026-07-27T17:00:00.000Z",
            user_prompt=f'"run_id": "{VALID_RUN_ID}" — please proceed',
        )
        mosaic_logger.resolve_run_identity(ctx)
        bound = runstate.get_session_run_binding(
            self.paths, session_id, at="2026-07-27T17:00:01.000Z"
        )
        self.assertEqual(VALID_RUN_ID, bound)

    def test_later_event_with_no_prompt_field_resolves_from_established_binding(self):
        """End-to-end within resolve_run_identity: a SubagentStart establishes
        the binding, then a later PreToolUse (no prompt-bearing field) for
        the same session resolves it from that binding."""
        session_id = "establish-sess-later-event"
        ctx1 = self._ctx(
            "SubagentStart", session_id, "2026-07-27T17:00:00.000Z",
            agent_prompt=(
                f'{{"agent_instance_id": "TestAgent#1", "run_id": "{VALID_RUN_ID}"}}'
            ),
        )
        mosaic_logger.resolve_run_identity(ctx1)

        ctx2 = self._ctx(
            "PreToolUse", session_id, "2026-07-27T17:05:00.000Z",
            tool_name="Bash", tool_input={},
        )
        mosaic_logger.resolve_run_identity(ctx2)
        self.assertEqual(VALID_RUN_ID, ctx2.run_id)


class TestResolveRunIdentityRefinementOverride(unittest.TestCase):
    """A run_id extracted from event text that differs from the currently
    bound run_id wins for that firing and extends the session timeline
    (Design A4 resolution order, AC2.4).
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def _ctx(self, event_name, session_id, timestamp, **fields):
        payload = {"hook_event_name": event_name, "session_id": session_id}
        payload.update(fields)
        return core.HookContext(payload, self.tmp_path, self.paths, timestamp)

    def test_differing_extracted_run_id_overrides_binding_for_this_firing(self):
        session_id = "override-sess-this-firing"
        runstate.put_session_run_binding(
            self.paths, session_id, VALID_RUN_ID, "2026-07-27T16:00:00.000Z"
        )
        new_run_id = "20260727T180000Z-bbbb"
        ctx = self._ctx(
            "UserPromptSubmit", session_id, "2026-07-27T18:00:00.000Z",
            user_prompt=f'"run_id": "{new_run_id}"',
        )
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(
            new_run_id, ctx.run_id,
            "A differing extracted run_id must win over the existing binding "
            "for this firing",
        )

    def test_differing_extracted_run_id_extends_timeline_for_later_firings(self):
        session_id = "override-sess-later-firings"
        runstate.put_session_run_binding(
            self.paths, session_id, VALID_RUN_ID, "2026-07-27T16:00:00.000Z"
        )
        new_run_id = "20260727T180000Z-bbbb"
        ctx1 = self._ctx(
            "UserPromptSubmit", session_id, "2026-07-27T18:00:00.000Z",
            user_prompt=f'"run_id": "{new_run_id}"',
        )
        mosaic_logger.resolve_run_identity(ctx1)

        ctx2 = self._ctx(
            "PreToolUse", session_id, "2026-07-27T18:05:00.000Z",
            tool_name="Bash", tool_input={},
        )
        mosaic_logger.resolve_run_identity(ctx2)
        self.assertEqual(
            new_run_id, ctx2.run_id,
            "The overriding run_id must extend the session timeline so later "
            "firings with no prompt-bearing field resolve to it too",
        )


class TestResolveRunIdentityGenuineFallbackDiagnostic(unittest.TestCase):
    """With no binding and no extractable run_id, resolution still yields
    the unknown-run bucket, and this genuine-fallback outcome emits a
    'run-identity: unresolved' diagnostic (Design A8, AC2.5).
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def test_unresolved_run_id_emits_run_identity_diagnostic(self):
        debug_file = self.tmp_path / "debug.log"
        orig = os.environ.get("MOSAIC_LOGGER_DEBUG")
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        try:
            payload = {
                "hook_event_name": "PreToolUse",
                "session_id": "diag-sess-unresolved",
                "tool_name": "Bash",
                "tool_input": {},
            }
            ctx = core.HookContext(
                payload, self.tmp_path, self.paths, "2026-07-27T17:00:00.000Z"
            )
            mosaic_logger.resolve_run_identity(ctx)
        finally:
            if orig is None:
                os.environ.pop("MOSAIC_LOGGER_DEBUG", None)
            else:
                os.environ["MOSAIC_LOGGER_DEBUG"] = orig

        self.assertIsNone(ctx.run_id)
        self.assertTrue(debug_file.exists(), "Diagnostic file must be written")
        text = debug_file.read_text(encoding="utf-8")
        self.assertIn(
            "run-identity: unresolved", text,
            "Unresolved run_id must emit the documented 'run-identity: unresolved' "
            "diagnostic prefix",
        )


if __name__ == "__main__":
    unittest.main()
