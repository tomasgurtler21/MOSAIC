"""Tests for the extraction-based run lifecycle in mosaic_logger (ghcp-cli variant).

Key differences from the claude-code variant:
- resolve_run_identity uses GHCP CLI camelCase event names and payload fields
- _RUN_ID_PROMPT_FIELDS mapping: subagentStart->agentPrompt, subagentStop->response,
  userPromptSubmitted->prompt, notification->message
- agentStop is intentionally absent from _RUN_ID_PROMPT_FIELDS: GHCP CLI's
  agentStop payload does not carry a last_assistant_message equivalent
- Workspace root is resolved from payload 'cwd', not CLAUDE_PROJECT_DIR

Covers:
- resolve_run_identity: per-event extraction from event-specific prompt fields
- Events with no prompt-bearing field (sessionStart, sessionEnd, preToolUse,
  postToolUse, postToolUseFailure, preCompact, agentStop) yield ctx.run_id = None
- agentStop specifically must NOT attempt run_id extraction
- End-to-end: subagentStart with run_id in agentPrompt writes logs to
  OrchestrationLogs/{run_id}/{agent_instance_id}/
- run_start emitted on sessionStart; run_end emitted on sessionEnd
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


VALID_RUN_ID = "20260727T170000Z-a3f9"


def _dispatch_with_cwd(payload: dict, tmp_path: pathlib.Path) -> None:
    """Dispatch a hook payload using the payload's own cwd field for workspace root.

    In GHCP CLI, the workspace root comes from the payload's 'cwd' field
    (not from CLAUDE_PROJECT_DIR). We inject it directly into the payload.
    """
    if "cwd" not in payload:
        payload = dict(payload, cwd=str(tmp_path))
    event = payload.pop("_event", "sessionStart")
    mosaic_logger.dispatch(event, json.dumps(payload))


def _read_events(path: pathlib.Path) -> list:
    """Return parsed JSONL lines from a .jsonl file, or [] if absent."""
    if not path.exists():
        return []
    lines = path.read_text(encoding="utf-8").splitlines()
    return [json.loads(line) for line in lines if line.strip()]


def _make_ctx(event: str, **fields) -> "core.HookContext":
    """Build a HookContext for the given GHCP CLI event with the supplied payload fields.
    Uses a temporary directory as the workspace root.
    """
    payload = {"sessionId": "test-sess"}
    payload.update(fields)
    root = pathlib.Path(tempfile.gettempdir())
    paths = core.build_paths(root)
    timestamp = "2026-07-27T17:00:00.000Z"
    return core.HookContext(event, payload, root, paths, timestamp)


class TestResolveRunIdentityPerEvent(unittest.TestCase):
    """resolve_run_identity sets ctx.run_id by extracting from event-specific
    prompt fields, or leaves it None for events with no prompt-bearing field."""

    def test_subagent_start_extracts_from_agent_prompt(self):
        """subagentStart: run_id is extracted from the 'agentPrompt' field."""
        ctx = _make_ctx(
            "subagentStart",
            agentPrompt=(
                f'{{"agent_instance_id": "TestAgent#1", '
                f'"run_id": "{VALID_RUN_ID}", '
                '"task_description": "some task"}}'
            ),
        )
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_subagent_stop_extracts_from_response(self):
        """subagentStop: run_id is extracted from the 'response' field."""
        ctx = _make_ctx(
            "subagentStop",
            response=(
                f'{{"agent_instance_id": "TestAgent#1", '
                f'"run_id": "{VALID_RUN_ID}", '
                '"status_code": "SUCCESS", "status_message": "Done"}}'
            ),
        )
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_user_prompt_submitted_extracts_from_prompt(self):
        """userPromptSubmitted: run_id is extracted from the 'prompt' field."""
        ctx = _make_ctx(
            "userPromptSubmitted",
            prompt=f'"run_id": "{VALID_RUN_ID}" — process this please',
        )
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_notification_extracts_from_message_field(self):
        """notification: run_id is extracted from the 'message' field."""
        ctx = _make_ctx(
            "notification",
            notificationType="info",
            message=f"Dispatch complete for run_id: {VALID_RUN_ID}",
        )
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(VALID_RUN_ID, ctx.run_id)

    def test_session_start_yields_none_no_prompt_bearing_field(self):
        """sessionStart has no prompt-bearing field; ctx.run_id stays None."""
        ctx = _make_ctx("sessionStart")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_session_end_yields_none(self):
        """sessionEnd has no prompt-bearing field; ctx.run_id stays None."""
        ctx = _make_ctx("sessionEnd")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_pre_tool_use_yields_none(self):
        """preToolUse has no prompt-bearing field; ctx.run_id stays None."""
        ctx = _make_ctx("preToolUse", toolName="bash", toolArgs={})
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_post_tool_use_yields_none(self):
        """postToolUse has no prompt-bearing field; ctx.run_id stays None."""
        ctx = _make_ctx("postToolUse", toolName="bash",
                        toolResult={"textResultForLlm": "output"})
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_post_tool_use_failure_yields_none(self):
        """postToolUseFailure has no prompt-bearing field; ctx.run_id stays None."""
        ctx = _make_ctx("postToolUseFailure", toolName="bash", error="cmd failed")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_pre_compact_yields_none(self):
        """preCompact has no prompt-bearing field; ctx.run_id stays None."""
        ctx = _make_ctx("preCompact")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_agent_stop_yields_none(self):
        """agentStop is intentionally absent from _RUN_ID_PROMPT_FIELDS.

        GHCP CLI's agentStop payload does not carry a 'last_assistant_message'
        or equivalent text field. Unlike claude-code's Stop event, agentStop
        cannot provide a run_id via prompt-field extraction. ctx.run_id must
        remain None and writes must go to the 'unknown-run' fallback bucket.
        """
        ctx = _make_ctx("agentStop",
                        transcriptPath="/path/to/transcript.jsonl",
                        stopReason="end_turn")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_agent_stop_not_in_run_id_prompt_fields(self):
        """Explicit check: agentStop must not be in _RUN_ID_PROMPT_FIELDS."""
        self.assertNotIn("agentStop", mosaic_logger._RUN_ID_PROMPT_FIELDS)

    def test_subagent_start_with_no_run_id_in_prompt_yields_none(self):
        """subagentStart where agentPrompt has no valid run_id yields None."""
        ctx = _make_ctx(
            "subagentStart",
            agentPrompt='{"agent_instance_id": "TestAgent#1", "task_description": "no run id"}',
        )
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_subagent_start_with_none_agent_prompt_yields_none(self):
        """subagentStart where agentPrompt is absent yields None without raising."""
        ctx = _make_ctx("subagentStart")
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id)

    def test_resolve_run_identity_never_raises(self):
        """resolve_run_identity must never raise regardless of payload content."""
        ctx = _make_ctx("subagentStart", agentPrompt=None)
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as e:
            self.fail(f"resolve_run_identity raised: {e}")

    def test_resolve_run_identity_never_raises_for_malformed_content(self):
        """resolve_run_identity is robust against malformed prompt content."""
        ctx = _make_ctx("userPromptSubmitted", prompt="\x00\x01 {broken")
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as e:
            self.fail(f"resolve_run_identity raised on malformed content: {e}")

    def test_ghcp_cli_prompt_field_names_are_camelcase(self):
        """The field names in _RUN_ID_PROMPT_FIELDS are camelCase GHCP CLI names."""
        for event, field in mosaic_logger._RUN_ID_PROMPT_FIELDS.items():
            # camelCase: first char lowercase, no underscores in field names
            # (agent_prompt would be claude-code style; agentPrompt is correct)
            self.assertFalse(
                "_" in field,
                f"Field name '{field}' for event '{event}' contains underscore "
                "— should be camelCase (e.g., 'agentPrompt', not 'agent_prompt')"
            )


class TestExtractionBasedDispatch(unittest.TestCase):
    """End-to-end tests: when run_id is extracted from dispatch content, logs are
    written to OrchestrationLogs/{run_id}/ (not to the 'unknown-run' bucket)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_subagent_start_with_run_id_writes_to_extracted_run_folder(self):
        """subagentStart where agentPrompt contains a valid run_id must write logs
        to OrchestrationLogs/{extracted_run_id}/{agent_instance_id}/."""
        agent_instance_id = "TestWriter#5"
        payload = {
            "sessionId": "extraction-sess-001",
            "agentId": "agt-extract-001",
            "agentType": "TestWriter",
            "agentPrompt": (
                f'{{"agent_instance_id": "{agent_instance_id}", '
                f'"run_id": "{VALID_RUN_ID}", '
                '"task_description": "Write tests"}}'
            ),
            "cwd": str(self.tmp_path),
        }
        mosaic_logger.dispatch("subagentStart", json.dumps(payload))

        events_file = (
            self.tmp_path / "OrchestrationLogs" / VALID_RUN_ID /
            agent_instance_id / "03_events.jsonl"
        )
        self.assertTrue(
            events_file.exists(),
            f"03_events.jsonl not found at OrchestrationLogs/{VALID_RUN_ID}/{agent_instance_id}/"
        )

    def test_invocation_start_event_carries_extracted_run_id(self):
        """The run_id field in the emitted invocation_start event matches the
        extracted run_id, not a bucket-minted one."""
        agent_instance_id = "TestWriter#5"
        payload = {
            "sessionId": "extraction-sess-002",
            "agentId": "agt-extract-002",
            "agentType": "TestWriter",
            "agentPrompt": (
                f'{{"agent_instance_id": "{agent_instance_id}", '
                f'"run_id": "{VALID_RUN_ID}", '
                '"task_description": "Write tests"}}'
            ),
            "cwd": str(self.tmp_path),
        }
        mosaic_logger.dispatch("subagentStart", json.dumps(payload))

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

    def test_user_prompt_submitted_with_run_id_writes_to_extracted_folder(self):
        """userPromptSubmitted where prompt contains a valid run_id writes to the
        extracted run_id's orchestrator events file."""
        payload = {
            "sessionId": "extraction-sess-003",
            "prompt": f'"run_id": "{VALID_RUN_ID}" -- please process this task',
            "cwd": str(self.tmp_path),
        }
        mosaic_logger.dispatch("userPromptSubmitted", json.dumps(payload))

        events_file = (
            self.tmp_path / "OrchestrationLogs" / VALID_RUN_ID /
            "00_orchestrator_events.jsonl"
        )
        self.assertTrue(
            events_file.exists(),
            f"00_orchestrator_events.jsonl not found in OrchestrationLogs/{VALID_RUN_ID}/"
        )

    def test_agent_stop_writes_to_unknown_run_fallback(self):
        """agentStop cannot extract run_id (absent from _RUN_ID_PROMPT_FIELDS),
        so all writes go to the 'unknown-run' fallback bucket."""
        payload = {
            "sessionId": "extraction-sess-004",
            "transcriptPath": "/nonexistent/transcript.jsonl",
            "stopReason": "end_turn",
            "cwd": str(self.tmp_path),
        }
        mosaic_logger.dispatch("agentStop", json.dumps(payload))

        # The log must land in unknown-run, not in any run_id bucket
        unknown_run_dir = self.tmp_path / "OrchestrationLogs" / "unknown-run"
        self.assertTrue(
            unknown_run_dir.exists(),
            "agentStop must write to unknown-run/ fallback (no run_id extraction)"
        )


class TestRunStartRunEndEventBased(unittest.TestCase):
    """run_start is emitted on sessionStart; run_end is emitted on sessionEnd.
    Emission is event-based. sessionStart/sessionEnd carry no prompt-bearing
    fields, so they land in 'unknown-run'."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _dispatch(self, event: str, payload: dict) -> None:
        payload = dict(payload, cwd=str(self.tmp_path))
        mosaic_logger.dispatch(event, json.dumps(payload))

    def test_session_start_triggers_run_start_emission(self):
        """run_start must be emitted when sessionStart fires."""
        self._dispatch("sessionStart", {"sessionId": "lifecycle-sess-001"})

        logs_dir = self.tmp_path / "OrchestrationLogs"
        run_start_found = False
        if logs_dir.exists():
            for jsonl_file in logs_dir.rglob("*.jsonl"):
                events = _read_events(jsonl_file)
                if any(e.get("event") == "run_start" for e in events):
                    run_start_found = True
                    break
        self.assertTrue(run_start_found,
                        "run_start event not found anywhere in log tree after sessionStart")

    def test_session_end_triggers_run_end_emission(self):
        """run_end must be emitted when sessionEnd fires."""
        session_id = "lifecycle-sess-002"
        self._dispatch("sessionStart", {"sessionId": session_id})
        self._dispatch("sessionEnd", {"sessionId": session_id, "reason": "clear"})

        logs_dir = self.tmp_path / "OrchestrationLogs"
        run_end_found = False
        if logs_dir.exists():
            for jsonl_file in logs_dir.rglob("*.jsonl"):
                events = _read_events(jsonl_file)
                if any(e.get("event") == "run_end" for e in events):
                    run_end_found = True
                    break
        self.assertTrue(run_end_found,
                        "run_end event not found anywhere in log tree after sessionEnd")

    def test_run_start_precedes_session_start_in_event_stream(self):
        """run_start must appear before session_start in the same event stream."""
        self._dispatch("sessionStart", {"sessionId": "lifecycle-sess-003"})

        logs_dir = self.tmp_path / "OrchestrationLogs"
        found_file = None
        if logs_dir.exists():
            for jsonl_file in logs_dir.rglob("*.jsonl"):
                events = _read_events(jsonl_file)
                types = [e.get("event") for e in events]
                if "run_start" in types and "session_start" in types:
                    found_file = (jsonl_file, types)
                    break

        self.assertIsNotNone(found_file,
                             "No file contains both run_start and session_start")
        jsonl_file, types = found_file
        self.assertLess(
            types.index("run_start"), types.index("session_start"),
            "run_start must precede session_start in the event stream"
        )

    def test_run_end_follows_session_end_in_event_stream(self):
        """run_end must appear after session_end in the same event stream."""
        session_id = "lifecycle-sess-004"
        self._dispatch("sessionStart", {"sessionId": session_id})
        self._dispatch("sessionEnd", {"sessionId": session_id, "reason": "clear"})

        logs_dir = self.tmp_path / "OrchestrationLogs"
        found_file = None
        if logs_dir.exists():
            for jsonl_file in logs_dir.rglob("*.jsonl"):
                events = _read_events(jsonl_file)
                types = [e.get("event") for e in events]
                if "session_end" in types and "run_end" in types:
                    found_file = (jsonl_file, types)
                    break

        self.assertIsNotNone(found_file,
                             "No file contains both session_end and run_end")
        jsonl_file, types = found_file
        self.assertLess(
            types.index("session_end"), types.index("run_end"),
            "session_end must precede run_end in the event stream"
        )

    def test_run_end_carries_outcome_from_session_end_reason(self):
        """run_end.outcome reflects the reason field from the sessionEnd payload."""
        session_id = "lifecycle-sess-005"
        self._dispatch("sessionStart", {"sessionId": session_id})
        self._dispatch("sessionEnd", {"sessionId": session_id, "reason": "clear"})

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


class TestAgentStopBehavior(unittest.TestCase):
    """Specific behavior tests for agentStop vs. claude-code Stop.

    agentStop MUST NOT attempt run_id extraction (it is absent from
    _RUN_ID_PROMPT_FIELDS). Unlike claude-code's Stop event which carries
    last_assistant_message, GHCP CLI's agentStop only has transcriptPath
    and stopReason.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_agent_stop_does_not_require_last_assistant_message(self):
        """agentStop handler must work without last_assistant_message field."""
        payload = {
            "sessionId": "agent-stop-test-001",
            "transcriptPath": "/nonexistent/transcript.jsonl",
            "stopReason": "end_turn",
            "cwd": str(self.tmp_path),
        }
        try:
            mosaic_logger.dispatch("agentStop", json.dumps(payload))
        except Exception as e:
            self.fail(f"dispatch agentStop raised unexpectedly: {e}")

    def test_agent_stop_emits_turn_event_without_content(self):
        """agentStop emits a turn with role='assistant' but without content field.
        (No last_assistant_message equivalent in GHCP CLI payload.)"""
        payload = {
            "sessionId": "agent-stop-test-002",
            "stopReason": "end_turn",
            "cwd": str(self.tmp_path),
        }
        mosaic_logger.dispatch("agentStop", json.dumps(payload))

        logs_dir = self.tmp_path / "OrchestrationLogs"
        turn_events = []
        if logs_dir.exists():
            for jsonl_file in logs_dir.rglob("*.jsonl"):
                events = _read_events(jsonl_file)
                turn_events.extend(
                    e for e in events if e.get("event") == "turn"
                )

        self.assertTrue(len(turn_events) > 0, "agentStop must emit a turn event")
        assistant_turns = [e for e in turn_events if e.get("role") == "assistant"]
        self.assertTrue(len(assistant_turns) > 0,
                        "agentStop turn must have role='assistant'")
        # Content field must be absent (no last_assistant_message)
        for turn in assistant_turns:
            self.assertNotIn("content", turn,
                             "agentStop turn must not have 'content' field "
                             "(no last_assistant_message in GHCP CLI payload)")

    def test_agent_stop_run_id_remains_none_after_resolve_run_identity(self):
        """resolve_run_identity must not modify ctx.run_id for agentStop."""
        ctx = _make_ctx(
            "agentStop",
            transcriptPath="/path/to/transcript.jsonl",
            stopReason="end_turn",
        )
        self.assertIsNone(ctx.run_id)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertIsNone(ctx.run_id,
                          "ctx.run_id must remain None for agentStop after "
                          "resolve_run_identity (agentStop absent from _RUN_ID_PROMPT_FIELDS)")


if __name__ == "__main__":
    unittest.main()
