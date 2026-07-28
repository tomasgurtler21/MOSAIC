"""Tests for session-scoped event handlers in mosaic_logger_handlers_session.

Covers:
  T3.1 - SessionStart → session_start event with resumed derivation and
          SessionEnd → session_end with undocumented reason pass-through
  T3.3 - UserPromptSubmit → user turn (full untruncated content);
          Stop → assistant turn (full content, transcript-derived model/token_usage)
  T3.5 - Notification → notification event; PreCompact/PostCompact →
          two independent partial compaction events (design decision: two partials)
  T3.6 - Stop carrying agent_id → no entry written to orchestrator event stream
  T3.7 - Stop → exports 00_orchestrator_session.raw with .meta.json sidecar

RED-phase notes:
  - T3.3 model/token_usage assertions will FAIL: handle_stop does not yet call
    transcript.read_last_assistant_facts.
  - T3.7 assertions will FAIL: handle_stop does not yet call export.export_transcript.
  Tests for T3.1, T3.5, T3.6 exercise implemented behaviour and define the specification.
"""

import json
import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_handlers_session as session_handlers


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

TEST_RUN_ID = "20260101T000000Z-ab12"
TEST_SESSION_ID = "test-session-abc123"
TEST_TIMESTAMP = "2026-01-01T00:00:00.000Z"


def _make_ctx(payload: dict,
              tmp_path: pathlib.Path,
              run_id: str = TEST_RUN_ID) -> core.HookContext:
    """Build a HookContext with a fixed timestamp and the given temp workspace."""
    paths = core.build_paths(tmp_path)
    ctx = core.HookContext(payload, tmp_path, paths, TEST_TIMESTAMP)
    ctx.run_id = run_id
    return ctx


def _read_events(ctx: core.HookContext) -> list:
    """Read all JSONL lines from the orchestrator events file for ctx.run_id."""
    path = ctx.paths.orchestrator_events(ctx.run_id)
    if not path.exists():
        return []
    lines = path.read_text(encoding="utf-8").splitlines()
    return [json.loads(line) for line in lines if line.strip()]


def _write_transcript(path: pathlib.Path, records: list) -> None:
    """Write a list of dicts as JSONL lines to path, creating parents as needed."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        "\n".join(json.dumps(r, ensure_ascii=False) for r in records) + "\n",
        encoding="utf-8",
    )


# ---------------------------------------------------------------------------
# T3.1 – SessionStart → session_start event
# ---------------------------------------------------------------------------

class TestHandleSessionStart(unittest.TestCase):
    """handle_session_start emits a session_start event to the orchestrator stream."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))

    def test_emitted_event_type_is_session_start(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("session_start", event["event"])

    def test_session_id_is_required_and_present(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual(TEST_SESSION_ID, event["session_id"])

    def test_writes_to_orchestrator_events_file(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        expected_path = ctx.paths.orchestrator_events(TEST_RUN_ID)
        self.assertTrue(expected_path.exists())

    def test_resumed_true_when_source_is_resume(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "resume",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertIn("resumed", event)
        self.assertIs(True, event["resumed"])

    def test_resumed_false_when_source_is_startup(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertIn("resumed", event)
        self.assertIs(False, event["resumed"])

    def test_resumed_false_when_source_is_clear(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "clear",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertIs(False, event["resumed"])

    def test_resumed_false_when_source_is_compact(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "compact",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertIs(False, event["resumed"])

    def test_resumed_false_when_source_is_fork(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "fork",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertIs(False, event["resumed"])

    def test_resumed_absent_when_source_absent(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("resumed", event)

    def test_false_resumed_is_kept_not_omitted(self):
        """False is a genuine resolved value; the serializer must not drop it."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        # The key must be present and its value must be the boolean False
        self.assertIn("resumed", event)
        self.assertIsInstance(event["resumed"], bool)
        self.assertIs(False, event["resumed"])

    def test_cwd_included_when_present(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
            "cwd": "/workspace/project",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("/workspace/project", event["cwd"])

    def test_cwd_absent_when_not_in_payload(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("cwd", event)

    def test_model_included_when_present(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
            "model": "claude-opus-4-5",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("claude-opus-4-5", event["model"])

    def test_model_absent_when_not_in_payload(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("model", event)

    def test_no_null_values_in_emitted_event(self):
        """The degrade-never-fabricate rule: no key may be present with a null value."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        for key, value in event.items():
            with self.subTest(key=key):
                self.assertIsNotNone(value)

    def test_no_event_written_when_run_id_is_none(self):
        """When run_id is None, no event file may be written."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        ctx.run_id = None
        session_handlers.handle_session_start(ctx)
        # No events file should be created (run_id is unresolvable)
        log_root = self.tmp_path / "OrchestrationLogs"
        jsonl_files = list(log_root.rglob("*.jsonl")) if log_root.exists() else []
        self.assertEqual(0, len(jsonl_files))


# ---------------------------------------------------------------------------
# T3.1 – SessionEnd → session_end event
# ---------------------------------------------------------------------------

class TestHandleSessionEnd(unittest.TestCase):
    """handle_session_end emits a session_end event with reason passed through."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        payload = {
            "hook_event_name": "SessionEnd",
            "session_id": TEST_SESSION_ID,
            "reason": "clear",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_end(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))

    def test_emitted_event_type_is_session_end(self):
        payload = {
            "hook_event_name": "SessionEnd",
            "session_id": TEST_SESSION_ID,
            "reason": "clear",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_end(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("session_end", event["event"])

    def test_session_id_is_required_and_present(self):
        payload = {
            "hook_event_name": "SessionEnd",
            "session_id": TEST_SESSION_ID,
            "reason": "clear",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_end(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual(TEST_SESSION_ID, event["session_id"])

    def test_documented_reason_clear_passed_through(self):
        payload = {
            "hook_event_name": "SessionEnd",
            "session_id": TEST_SESSION_ID,
            "reason": "clear",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_end(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("clear", event["reason"])

    def test_documented_reason_resume_passed_through(self):
        payload = {
            "hook_event_name": "SessionEnd",
            "session_id": TEST_SESSION_ID,
            "reason": "resume",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_end(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("resume", event["reason"])

    def test_documented_reason_logout_passed_through(self):
        payload = {
            "hook_event_name": "SessionEnd",
            "session_id": TEST_SESSION_ID,
            "reason": "logout",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_end(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("logout", event["reason"])

    def test_undocumented_reason_passed_through_unvalidated(self):
        """Undocumented end reasons must be passed through as observed, never rejected."""
        for undocumented in ("timeout", "crash", "network_lost", "force_quit", "sigkill"):
            with self.subTest(reason=undocumented):
                payload = {
                    "hook_event_name": "SessionEnd",
                    "session_id": TEST_SESSION_ID,
                    "reason": undocumented,
                }
                tmp_sub = pathlib.Path(self.tmp_path) / f"sub_{undocumented}"
                tmp_sub.mkdir()
                ctx = _make_ctx(payload, tmp_sub)
                session_handlers.handle_session_end(ctx)
                event = _read_events(ctx)[0]
                self.assertEqual(undocumented, event["reason"])

    def test_reason_absent_when_not_in_payload(self):
        payload = {
            "hook_event_name": "SessionEnd",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_end(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("reason", event)

    def test_no_null_values_in_emitted_event(self):
        payload = {
            "hook_event_name": "SessionEnd",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_end(ctx)
        event = _read_events(ctx)[0]
        for key, value in event.items():
            with self.subTest(key=key):
                self.assertIsNotNone(value)

    def test_writes_to_orchestrator_events_file(self):
        payload = {
            "hook_event_name": "SessionEnd",
            "session_id": TEST_SESSION_ID,
            "reason": "clear",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_end(ctx)
        expected_path = ctx.paths.orchestrator_events(TEST_RUN_ID)
        self.assertTrue(expected_path.exists())


# ---------------------------------------------------------------------------
# T3.3 – UserPromptSubmit → user turn
# ---------------------------------------------------------------------------

class TestHandleUserPromptSubmit(unittest.TestCase):
    """handle_user_prompt_submit emits a turn with role 'user' and full content."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "user_prompt": "Hello, Claude!",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))

    def test_emitted_event_type_is_turn(self):
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "user_prompt": "Hello!",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("turn", event["event"])

    def test_role_is_user(self):
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "user_prompt": "Hello!",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("user", event["role"])

    def test_content_matches_user_prompt_exactly(self):
        prompt = "What is the meaning of life?"
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "user_prompt": prompt,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual(prompt, event["content"])

    def test_content_is_full_and_untruncated(self):
        """Multi-kilobyte prompts must not be truncated."""
        long_prompt = "A" * 50_000
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "user_prompt": long_prompt,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual(len(long_prompt), len(event["content"]))
        self.assertEqual(long_prompt, event["content"])

    def test_no_event_when_user_prompt_absent(self):
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        events = _read_events(ctx)
        self.assertEqual(0, len(events))

    def test_no_event_when_user_prompt_is_empty_string(self):
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "user_prompt": "",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        events = _read_events(ctx)
        self.assertEqual(0, len(events))

    def test_user_turn_has_no_token_usage(self):
        """User turns have no assistant usage record; token_usage must not be fabricated."""
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "user_prompt": "Hello!",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("token_usage", event)

    def test_user_turn_has_no_model(self):
        """User turns should not carry model; model is an assistant-turn field."""
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "user_prompt": "Hello!",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("model", event)


# ---------------------------------------------------------------------------
# T3.3 – Stop → assistant turn (including transcript-derived fields)
# ---------------------------------------------------------------------------

class TestHandleStop(unittest.TestCase):
    """handle_stop emits an assistant turn with full content, model, and token_usage."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event_when_no_agent_id(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Here is my response.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))

    def test_emitted_event_type_is_turn(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("turn", event["event"])

    def test_role_is_assistant(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("assistant", event["role"])

    def test_content_matches_last_assistant_message_exactly(self):
        message = "Here is a detailed response with many paragraphs."
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": message,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual(message, event["content"])

    def test_content_is_full_and_untruncated(self):
        """Multi-kilobyte messages must not be truncated."""
        long_message = "B" * 100_000
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": long_message,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual(len(long_message), len(event["content"]))
        self.assertEqual(long_message, event["content"])

    def test_no_event_when_last_assistant_message_absent(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        self.assertEqual(0, len(events))

    def test_model_from_transcript_included_in_assistant_turn(self):
        """handle_stop must read model from transcript and include it in the turn event."""
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "user", "message": {"role": "user"}},
            {"type": "assistant", "message": {
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 100, "output_tokens": 50},
            }},
        ])
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(transcript_path),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        event = _read_events(ctx)[0]
        # Expected to FAIL until handle_stop integrates transcript reading
        self.assertIn("model", event)
        self.assertEqual("claude-opus-4-5", event["model"])

    def test_token_usage_from_transcript_included_in_assistant_turn(self):
        """handle_stop must read token_usage from transcript and include it in the turn."""
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "model": "claude-opus-4-5",
                "usage": {
                    "input_tokens": 200,
                    "output_tokens": 75,
                    "cache_read_input_tokens": 500,
                },
            }},
        ])
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(transcript_path),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        event = _read_events(ctx)[0]
        # Expected to FAIL until handle_stop integrates transcript reading
        self.assertIn("token_usage", event)
        usage = event["token_usage"]
        self.assertEqual(200, usage["input_tokens"])
        self.assertEqual(75, usage["output_tokens"])
        self.assertEqual(500, usage["cache_read_tokens"])

    def test_token_usage_absent_when_transcript_missing(self):
        """A missing transcript must degrade to omitted fields, not raise or fabricate."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(self.tmp_path / "nonexistent.jsonl"),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        # Must not raise
        try:
            session_handlers.handle_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_stop raised when transcript missing: {exc}")
        events = _read_events(ctx)
        if events:
            event = events[0]
            self.assertNotIn("token_usage", event)

    def test_model_absent_when_transcript_missing(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(self.tmp_path / "nonexistent.jsonl"),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        if events:
            event = events[0]
            self.assertNotIn("model", event)


# ---------------------------------------------------------------------------
# T3.5 – Notification → notification event
# ---------------------------------------------------------------------------

class TestHandleNotification(unittest.TestCase):
    """handle_notification emits a notification event with notification_type passed through."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        payload = {
            "hook_event_name": "Notification",
            "session_id": TEST_SESSION_ID,
            "notification_type": "info",
            "message": "Something happened",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_notification(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))

    def test_emitted_event_type_is_notification(self):
        payload = {
            "hook_event_name": "Notification",
            "session_id": TEST_SESSION_ID,
            "notification_type": "warning",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_notification(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("notification", event["event"])

    def test_notification_type_passed_through_as_observed(self):
        for notification_type in ("info", "warning", "error", "unknown_future_type", "42"):
            with self.subTest(notification_type=notification_type):
                payload = {
                    "hook_event_name": "Notification",
                    "session_id": TEST_SESSION_ID,
                    "notification_type": notification_type,
                }
                tmp_sub = self.tmp_path / f"sub_{notification_type}"
                tmp_sub.mkdir(exist_ok=True)
                ctx = _make_ctx(payload, tmp_sub)
                session_handlers.handle_notification(ctx)
                event = _read_events(ctx)[0]
                self.assertEqual(notification_type, event["notification_type"])

    def test_message_included_when_present(self):
        payload = {
            "hook_event_name": "Notification",
            "session_id": TEST_SESSION_ID,
            "notification_type": "info",
            "message": "Context window is nearly full.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_notification(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("Context window is nearly full.", event["message"])

    def test_message_absent_when_not_in_payload(self):
        payload = {
            "hook_event_name": "Notification",
            "session_id": TEST_SESSION_ID,
            "notification_type": "info",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_notification(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("message", event)

    def test_no_event_when_notification_type_absent(self):
        """notification_type is required; an absent type means nothing to emit."""
        payload = {
            "hook_event_name": "Notification",
            "session_id": TEST_SESSION_ID,
            "message": "Something happened",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_notification(ctx)
        events = _read_events(ctx)
        self.assertEqual(0, len(events))

    def test_no_fabricated_fields_like_requires_response_or_resolution(self):
        """Fields the harness does not supply must never be fabricated."""
        payload = {
            "hook_event_name": "Notification",
            "session_id": TEST_SESSION_ID,
            "notification_type": "info",
            "message": "Done.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_notification(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("requires_response", event)
        self.assertNotIn("resolution", event)
        self.assertNotIn("notification_id", event)


# ---------------------------------------------------------------------------
# T3.5 – PreCompact / PostCompact → two partial compaction events
# ---------------------------------------------------------------------------

class TestHandleCompaction(unittest.TestCase):
    """PreCompact and PostCompact each emit one partial compaction event.

    Design decision: two partial events, not one merged event. All compaction
    fields are optional, so each partial event is schema-valid on its own.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_pre_compact_emits_exactly_one_event(self):
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
            "tokens_before": 180000,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))

    def test_pre_compact_event_type_is_compaction(self):
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
            "tokens_before": 180000,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("compaction", event["event"])

    def test_pre_compact_includes_trigger(self):
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "manual",
            "tokens_before": 50000,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("manual", event["trigger"])

    def test_pre_compact_includes_tokens_before(self):
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
            "tokens_before": 180000,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual(180000, event["tokens_before"])

    def test_pre_compact_does_not_include_tokens_after(self):
        """PreCompact does not supply tokens_after; it must not be fabricated."""
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
            "tokens_before": 180000,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("tokens_after", event)

    def test_post_compact_emits_exactly_one_event(self):
        payload = {
            "hook_event_name": "PostCompact",
            "session_id": TEST_SESSION_ID,
            "tokens_after": 5000,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_post_compact(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))

    def test_post_compact_event_type_is_compaction(self):
        payload = {
            "hook_event_name": "PostCompact",
            "session_id": TEST_SESSION_ID,
            "tokens_after": 5000,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_post_compact(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("compaction", event["event"])

    def test_post_compact_includes_tokens_after(self):
        payload = {
            "hook_event_name": "PostCompact",
            "session_id": TEST_SESSION_ID,
            "tokens_after": 5000,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_post_compact(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual(5000, event["tokens_after"])

    def test_post_compact_does_not_include_trigger_or_tokens_before(self):
        """PostCompact does not supply trigger or tokens_before; they must not be fabricated."""
        payload = {
            "hook_event_name": "PostCompact",
            "session_id": TEST_SESSION_ID,
            "tokens_after": 5000,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_post_compact(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("trigger", event)
        self.assertNotIn("tokens_before", event)

    def test_two_separate_events_emitted_not_one_merged(self):
        """Design: two partial events, not one merged. Each hook fires independently."""
        pre_payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
            "tokens_before": 180000,
        }
        post_payload = {
            "hook_event_name": "PostCompact",
            "session_id": TEST_SESSION_ID,
            "tokens_after": 5000,
        }
        ctx = _make_ctx(pre_payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)

        # Reuse the same context/run_id/events file for the second handler
        ctx2 = core.HookContext(
            post_payload,
            self.tmp_path,
            ctx.paths,
            TEST_TIMESTAMP,
        )
        ctx2.run_id = TEST_RUN_ID
        session_handlers.handle_post_compact(ctx2)

        events = _read_events(ctx)
        self.assertEqual(2, len(events))
        self.assertEqual("compaction", events[0]["event"])
        self.assertEqual("compaction", events[1]["event"])

    def test_tokens_after_zero_is_kept(self):
        """0 is a genuine resolved value and must not be dropped by the serializer."""
        payload = {
            "hook_event_name": "PostCompact",
            "session_id": TEST_SESSION_ID,
            "tokens_after": 0,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_post_compact(ctx)
        event = _read_events(ctx)[0]
        self.assertIn("tokens_after", event)
        self.assertEqual(0, event["tokens_after"])


# ---------------------------------------------------------------------------
# T3.6 – Stop with agent_id → no orchestrator stream entry
# ---------------------------------------------------------------------------

class TestHandleStopAgentIdGuard(unittest.TestCase):
    """A Stop input carrying agent_id must not produce any orchestrator event."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_stop_with_agent_id_writes_no_event(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "agent_id": "subagent-abc",
            "last_assistant_message": "Subagent result.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        self.assertEqual(0, len(events))

    def test_stop_with_agent_id_does_not_create_events_file(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "agent_id": "subagent-xyz",
            "last_assistant_message": "Done.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events_file = ctx.paths.orchestrator_events(TEST_RUN_ID)
        self.assertFalse(events_file.exists())

    def test_stop_without_agent_id_does_write_event(self):
        """Contrast: orchestrator Stop (no agent_id) DOES emit a turn."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Orchestrator done.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))

    def test_stop_with_agent_id_does_not_raise(self):
        """The guard must silently return, never raise."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "agent_id": "agent-001",
            "last_assistant_message": "Done.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        try:
            session_handlers.handle_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_stop raised when agent_id present: {exc}")

    def test_stop_with_empty_string_agent_id_not_treated_as_subagent(self):
        """An empty-string agent_id normalizes to None — not a subagent invocation."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "agent_id": "",
            "last_assistant_message": "Orchestrator done.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        # ctx.agent_id normalizes "" to None in HookContext, so this is an orchestrator Stop
        events = _read_events(ctx)
        self.assertEqual(1, len(events))


# ---------------------------------------------------------------------------
# T3.7 – Orchestrator transcript export (handle_stop)
# ---------------------------------------------------------------------------

class TestOrchestratorTranscriptExport(unittest.TestCase):
    """handle_stop exports the orchestrator transcript to 00_orchestrator_session.raw.

    These tests are expected to FAIL until handle_stop integrates export_transcript.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _make_transcript(self, content: bytes = b"transcript content\n") -> pathlib.Path:
        src = self.tmp_path / "source_transcript.jsonl"
        src.write_bytes(content)
        return src

    def test_stop_exports_raw_transcript_to_orchestrator_raw_path(self):
        """handle_stop must write 00_orchestrator_session.raw from transcript_path."""
        content = b'{"type": "user"}\n{"type": "assistant"}\n'
        transcript = self._make_transcript(content)
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(transcript),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        # Expected to FAIL until handle_stop calls export_transcript
        raw_path = ctx.paths.orchestrator_raw(TEST_RUN_ID)
        self.assertTrue(raw_path.exists(), "00_orchestrator_session.raw was not created")

    def test_exported_raw_content_is_byte_identical_to_source(self):
        content = b'{"type": "user"}\n{"type": "assistant"}\n'
        transcript = self._make_transcript(content)
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(transcript),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        raw_path = ctx.paths.orchestrator_raw(TEST_RUN_ID)
        self.assertTrue(raw_path.exists())
        self.assertEqual(content, raw_path.read_bytes())

    def test_sidecar_meta_json_is_written_alongside_raw(self):
        """A .meta.json sidecar must be written beside 00_orchestrator_session.raw."""
        transcript = self._make_transcript()
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(transcript),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        meta_path = ctx.paths.orchestrator_raw(TEST_RUN_ID).with_suffix(".meta.json")
        self.assertTrue(meta_path.exists(), "00_orchestrator_session.meta.json was not created")

    def test_sidecar_source_mechanism_is_transcript_path(self):
        transcript = self._make_transcript()
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(transcript),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        meta_path = ctx.paths.orchestrator_raw(TEST_RUN_ID).with_suffix(".meta.json")
        self.assertTrue(meta_path.exists())
        sidecar = json.loads(meta_path.read_text(encoding="utf-8"))
        self.assertEqual("transcript_path", sidecar["source_mechanism"])

    def test_no_raw_written_when_transcript_path_absent(self):
        """Missing transcript_path must degrade silently with no raw file written."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        try:
            session_handlers.handle_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_stop raised when transcript_path absent: {exc}")
        raw_path = ctx.paths.orchestrator_raw(TEST_RUN_ID)
        self.assertFalse(raw_path.exists())

    def test_no_raw_written_when_transcript_path_points_to_nonexistent_file(self):
        """Unreadable transcript_path must degrade silently with no raw file written."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(self.tmp_path / "does_not_exist.jsonl"),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        try:
            session_handlers.handle_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_stop raised with unreadable transcript: {exc}")
        raw_path = ctx.paths.orchestrator_raw(TEST_RUN_ID)
        self.assertFalse(raw_path.exists())

    def test_turn_event_still_written_when_export_fails(self):
        """A transcript export failure must not prevent the turn event from being written."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(self.tmp_path / "does_not_exist.jsonl"),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        # The turn event itself must still be present regardless of export outcome
        self.assertEqual(1, len(events))
        self.assertEqual("turn", events[0]["event"])


if __name__ == "__main__":
    unittest.main()
