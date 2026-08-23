"""Tests for session-scoped event handlers in mosaic_logger_handlers_session.

Covers:
  SessionStart → session_start event with resumed derivation and
  SessionEnd → session_end with undocumented reason pass-through
  UserPromptSubmit → user turn (full untruncated content);
  Stop → assistant turn (full content, transcript-derived model/token_usage)
  Notification → notification event; PreCompact/PostCompact →
  two independent partial compaction events (design decision: two partials)
  Stop carrying agent_id → still emits orchestrator turn event (guard removed)
  Stop → exports 00_orchestrator_session.raw with .meta.json sidecar
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

    def test_event_written_to_unknown_run_when_run_id_is_none(self):
        """When run_id is None, event is routed to the unknown-run/ bucket."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        ctx.run_id = None
        session_handlers.handle_session_start(ctx)
        # effective_run_id returns "unknown-run" — event must land there
        unknown_run_events = ctx.paths.orchestrator_events("unknown-run")
        self.assertTrue(unknown_run_events.exists(),
                        "Event must be routed to unknown-run/ when run_id is None")


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
# T5.2 / T5.3 / T5.6 – handle_stop emits usage_record events to the
# orchestrator stream, one per assistant record, alongside the unchanged
# turn event.
# ---------------------------------------------------------------------------

class TestHandleStopUsageEmission(unittest.TestCase):
    """handle_stop emits one usage_record per assistant record in
    ctx.transcript_path to the orchestrator stream, without disturbing the
    pre-existing turn event."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _usage_events(self, ctx):
        return [e for e in _read_events(ctx) if e["event"] == "usage_record"]

    def test_two_assistant_records_produce_two_usage_records(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "id": "msg_1", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10, "output_tokens": 5},
            }},
            {"type": "assistant", "message": {
                "id": "msg_2", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 20, "output_tokens": 8},
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
        usage_events = self._usage_events(ctx)
        self.assertEqual(2, len(usage_events))

    def test_usage_records_omit_agent_instance_id_on_orchestrator_stream(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "id": "msg_orch", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
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
        usage_event = self._usage_events(ctx)[0]
        self.assertNotIn("agent_instance_id", usage_event)

    def test_usage_record_source_is_orchestrator_transcript(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "id": "msg_src", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
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
        usage_event = self._usage_events(ctx)[0]
        self.assertEqual("orchestrator_transcript", usage_event["source"])

    def test_no_usage_records_when_transcript_missing(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(self.tmp_path / "nonexistent.jsonl"),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        self.assertEqual(0, len(self._usage_events(ctx)))

    def test_turn_event_still_emitted_alongside_usage_records(self):
        """T5.6 regression: the pre-existing turn event is unaffected by
        the new usage_record emission."""
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "id": "msg_turn", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10, "output_tokens": 5},
            }},
        ])
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Here is my response.",
            "transcript_path": str(transcript_path),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        turn_events = [e for e in events if e["event"] == "turn"]
        self.assertEqual(1, len(turn_events))
        self.assertEqual("Here is my response.", turn_events[0]["content"])
        self.assertEqual("claude-opus-4-5", turn_events[0]["model"])

    def test_repeated_stop_firings_do_not_error_or_duplicate(self):
        """Two Stop firings against the same transcript are tolerated: no
        exception is raised and the usage record is emitted exactly once, not
        duplicated on the second identical firing."""
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "id": "msg_repeat", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
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
        session_handlers.handle_stop(ctx)
        self.assertEqual(1, len(self._usage_events(ctx)))


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

    def test_pre_compact_emits_usage_records_from_transcript(self):
        """T5.3: PreCompact has transcript access and must emit usage_record
        per assistant record, alongside its own compaction event."""
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "id": "msg_pre", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
            }},
        ])
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
            "tokens_before": 180000,
            "transcript_path": str(transcript_path),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        events = _read_events(ctx)
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events))
        compaction_events = [e for e in events if e["event"] == "compaction"]
        self.assertEqual(1, len(compaction_events))

    def test_post_compact_emits_usage_records_from_transcript(self):
        """T5.3: PostCompact has transcript access and must emit usage_record
        per assistant record, alongside its own compaction event."""
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "id": "msg_post", "model": "claude-opus-4-5",
                "usage": {"output_tokens": 8},
            }},
        ])
        payload = {
            "hook_event_name": "PostCompact",
            "session_id": TEST_SESSION_ID,
            "tokens_after": 5000,
            "transcript_path": str(transcript_path),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_post_compact(ctx)
        events = _read_events(ctx)
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events))
        compaction_events = [e for e in events if e["event"] == "compaction"]
        self.assertEqual(1, len(compaction_events))


# ---------------------------------------------------------------------------
# T3.6 – Stop with agent_id → still emits orchestrator event (guard removed)
# ---------------------------------------------------------------------------

class TestHandleStopAgentIdGuard(unittest.TestCase):
    """handle_stop must process Stop events regardless of agent_id presence.

    The previous `if ctx.agent_id: return` guard is removed. Event-name-based
    dispatch (Stop vs SubagentStop) already distinguishes orchestrator from
    subagent firings, so the guard is redundant and unsafe once orchestrators
    adopt `--agent` launches (which populates agent_id on all hook events
    including the top-level Stop).
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_stop_with_agent_id_still_emits_turn_event(self):
        """handle_stop must emit a turn event even when agent_id is populated."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "agent_id": "subagent-abc",
            "last_assistant_message": "Subagent result.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))

    def test_stop_with_agent_id_still_creates_events_file(self):
        """handle_stop must create the orchestrator events file even when agent_id is set."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "agent_id": "subagent-xyz",
            "last_assistant_message": "Done.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events_file = ctx.paths.orchestrator_events(TEST_RUN_ID)
        self.assertTrue(events_file.exists())

    def test_stop_without_agent_id_does_write_event(self):
        """Orchestrator Stop (no agent_id) also emits a turn."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Orchestrator done.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))

    def test_stop_with_agent_id_turn_event_has_assistant_role(self):
        """handle_stop must emit a turn event with role 'assistant' when agent_id is populated."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "agent_id": "orchestrator-agent-001",
            "last_assistant_message": "Orchestrator response.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("assistant", event["role"])

    def test_stop_with_agent_id_does_not_raise(self):
        """handle_stop must never raise when agent_id is present."""
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
    """handle_stop exports the orchestrator transcript to 00_orchestrator_session.raw."""

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


# ---------------------------------------------------------------------------
# AC4.1(b) – payload-driven usage_record events carry model and token fields
# ---------------------------------------------------------------------------

class TestHandleStopUsageRecordModelAndTokenFields(unittest.TestCase):
    """handle_stop driven by a payload whose transcript_path names a readable
    transcript emits usage_record events that carry both the model and the
    token-count fields.

    This closes the payload → HookContext.transcript_path → emit_usage_records
    → usage_record seam.  Two gaps in the existing suites make this class
    necessary:

    (1) TestHandleStopUsageEmission (in this file) asserts usage_record count,
        source, and the absence of agent_instance_id — but never model or
        token_usage.
    (2) test_usage_emission.py asserts model and token fields on usage_records,
        but calls emit_usage_records directly with a path argument, bypassing
        the payload → ctx.transcript_path intake that this stage changed.

    The tests here are payload-driven end-to-end and assert the full content
    of the emitted usage_record, spanning both halves at once.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _usage_events(self, ctx):
        return [e for e in _read_events(ctx) if e["event"] == "usage_record"]

    def test_usage_record_carries_model_from_payload_driven_transcript(self):
        """A payload-driven Stop with a readable transcript emits a usage_record
        event that contains the model name from the transcript's assistant record."""
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "id": "msg_model_seam",
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 100, "output_tokens": 40},
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
        usage_events = self._usage_events(ctx)
        self.assertEqual(1, len(usage_events))
        self.assertIn("model", usage_events[0])
        self.assertEqual("claude-opus-4-5", usage_events[0]["model"])

    def test_usage_record_carries_input_and_output_tokens_from_payload_driven_transcript(self):
        """A payload-driven Stop with a readable transcript emits a usage_record
        event whose token_usage block contains input_tokens and output_tokens
        matching the assistant record in the transcript."""
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "id": "msg_tokens_seam",
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 250, "output_tokens": 85},
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
        usage_events = self._usage_events(ctx)
        self.assertEqual(1, len(usage_events))
        self.assertIn("token_usage", usage_events[0])
        token_usage = usage_events[0]["token_usage"]
        self.assertEqual(250, token_usage["input_tokens"])
        self.assertEqual(85, token_usage["output_tokens"])

    def test_usage_record_carries_cache_read_tokens_from_payload_driven_transcript(self):
        """Cache-read token counts in the transcript are included in the
        usage_record's token_usage block when the payload drives the transcript path."""
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "id": "msg_cache_seam",
                "model": "claude-opus-4-5",
                "usage": {
                    "input_tokens": 50,
                    "output_tokens": 20,
                    "cache_read_input_tokens": 1000,
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
        usage_events = self._usage_events(ctx)
        self.assertEqual(1, len(usage_events))
        token_usage = usage_events[0]["token_usage"]
        self.assertIn("cache_read_tokens", token_usage)
        self.assertEqual(1000, token_usage["cache_read_tokens"])

    def test_each_of_multiple_usage_records_carries_model_and_tokens(self):
        """When the transcript has multiple assistant records, each emitted
        usage_record carries the model and token counts from its own record —
        verified end-to-end through the payload-driven seam."""
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "id": "msg_multi_first",
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 100, "output_tokens": 40},
            }},
            {"type": "assistant", "message": {
                "id": "msg_multi_second",
                "model": "claude-haiku-3",
                "usage": {"input_tokens": 200, "output_tokens": 60},
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
        usage_events = self._usage_events(ctx)
        self.assertEqual(2, len(usage_events))
        # Every emitted record must carry both model and token_usage.
        for event in usage_events:
            with self.subTest(record_id=event.get("record_id")):
                self.assertIn("model", event)
                self.assertIn("token_usage", event)
        # Per-record values must match the transcript.
        by_id = {e["record_id"]: e for e in usage_events}
        self.assertEqual("claude-opus-4-5", by_id["msg_multi_first"]["model"])
        self.assertEqual(100, by_id["msg_multi_first"]["token_usage"]["input_tokens"])
        self.assertEqual(40, by_id["msg_multi_first"]["token_usage"]["output_tokens"])
        self.assertEqual("claude-haiku-3", by_id["msg_multi_second"]["model"])
        self.assertEqual(200, by_id["msg_multi_second"]["token_usage"]["input_tokens"])
        self.assertEqual(60, by_id["msg_multi_second"]["token_usage"]["output_tokens"])

    def test_model_and_tokens_absent_from_usage_record_when_transcript_missing(self):
        """When transcript_path in the payload names a file that does not exist,
        no usage_record events are emitted (the transcript cannot be read, so
        there is nothing to emit).  This is the degrade-silently path."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(self.tmp_path / "nonexistent.jsonl"),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        usage_events = self._usage_events(ctx)
        self.assertEqual(0, len(usage_events))


if __name__ == "__main__":
    unittest.main()
