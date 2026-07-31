"""Tests for session-scoped event handlers in mosaic_logger_handlers_session (ghcp-cli variant).

Key differences from the claude-code variant:
- HookContext.__init__ takes event name as first argument (from sys.argv[1])
- Payload fields are camelCase: sessionId, cwd, transcriptPath, etc.
- handle_session_start reads 'resumed' as a direct boolean field (not derived from 'source')
- handle_user_prompt_submitted reads from 'prompt' (not 'user_prompt')
- handle_agent_stop always emits a turn WITHOUT content (no last_assistant_message in agentStop)
- handle_notification reads 'notificationType' (camelCase)
- handle_pre_compact omits tokens_before (GHCP CLI preCompact payload has no token count fields)
- Three GHCP-CLI-specific observational events: userPromptTransformed, permissionRequest, errorOccurred

Covers:
  sessionStart -> session_start event (resumed as direct boolean)
  sessionEnd -> session_end with reason
  userPromptSubmitted -> turn(user) reading from camelCase 'prompt' field
  agentStop -> turn(assistant) without content, transcript-derived model/token_usage
  notification -> notification event reading 'notificationType'
  preCompact -> compaction event (trigger only, no tokens_before)
  userPromptTransformed -> observational prompt_transformed event
  permissionRequest -> observational permission_request, never writes stdout
  errorOccurred -> observational error_occurred, reads nested error.message
  emit_run_start and emit_run_end called by dispatcher
"""

import contextlib
import io
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
TEST_SESSION_ID = "test-ghcp-session-abc"
TEST_TIMESTAMP = "2026-01-01T00:00:00.000Z"


def _make_ctx(event: str, payload: dict,
              tmp_path: pathlib.Path,
              run_id: "str | None" = TEST_RUN_ID) -> core.HookContext:
    """Build a HookContext rooted at tmp_path with a fixed timestamp."""
    paths = core.build_paths(tmp_path)
    ctx = core.HookContext(event, payload, tmp_path, paths, TEST_TIMESTAMP)
    ctx.run_id = run_id
    return ctx


def _read_orchestrator_events(ctx: core.HookContext) -> list:
    """Read all JSONL lines from the orchestrator events file."""
    run_id = ctx.run_id or "unknown-run"
    path = ctx.paths.orchestrator_events(run_id)
    if not path.exists():
        return []
    return [json.loads(ln) for ln in path.read_text("utf-8").splitlines() if ln.strip()]


def _write_transcript(path: pathlib.Path, records: list) -> None:
    """Write a list of dicts as JSONL to path, creating parents as needed."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        "\n".join(json.dumps(r, ensure_ascii=False) for r in records) + "\n",
        encoding="utf-8",
    )


# ---------------------------------------------------------------------------
# HANDLERS registry
# ---------------------------------------------------------------------------

class TestHandlersRegistry(unittest.TestCase):
    """HANDLERS dict maps all 9 session-scoped camelCase event names to callables."""

    def test_handlers_is_a_dict(self):
        self.assertIsInstance(session_handlers.HANDLERS, dict)

    def test_all_nine_session_events_registered(self):
        expected = {
            "sessionStart", "sessionEnd", "userPromptSubmitted", "agentStop",
            "notification", "preCompact", "userPromptTransformed",
            "permissionRequest", "errorOccurred",
        }
        self.assertEqual(expected, set(session_handlers.HANDLERS.keys()))

    def test_all_handlers_are_callable(self):
        for event, handler in session_handlers.HANDLERS.items():
            with self.subTest(event=event):
                self.assertTrue(callable(handler))


# ---------------------------------------------------------------------------
# sessionStart -> session_start event
# ---------------------------------------------------------------------------

class TestHandleSessionStart(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        self.assertEqual(1, len(_read_orchestrator_events(ctx)))

    def test_emitted_event_type_is_session_start(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        self.assertEqual("session_start", _read_orchestrator_events(ctx)[0]["event"])

    def test_harness_is_ghcp_cli(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        self.assertEqual("ghcp-cli", _read_orchestrator_events(ctx)[0]["harness"])

    def test_session_id_present_in_event(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        self.assertEqual(TEST_SESSION_ID, _read_orchestrator_events(ctx)[0]["session_id"])

    def test_resumed_true_when_payload_has_true(self):
        """GHCP CLI sends resumed as a direct boolean in the payload."""
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID, "resumed": True},
                        self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertIn("resumed", event)
        self.assertIs(True, event["resumed"])

    def test_resumed_false_when_payload_has_false(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID, "resumed": False},
                        self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertIn("resumed", event)
        self.assertIs(False, event["resumed"])

    def test_false_resumed_kept_not_pruned(self):
        """False is a genuine resolved value; the serializer must not prune it."""
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID, "resumed": False},
                        self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertIsInstance(event["resumed"], bool)

    def test_resumed_absent_when_not_in_payload(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        self.assertNotIn("resumed", _read_orchestrator_events(ctx)[0])

    def test_cwd_included_when_present(self):
        ctx = _make_ctx("sessionStart",
                        {"sessionId": TEST_SESSION_ID, "cwd": "/workspace/proj"},
                        self.tmp_path)
        session_handlers.handle_session_start(ctx)
        self.assertEqual("/workspace/proj", _read_orchestrator_events(ctx)[0]["cwd"])

    def test_cwd_absent_when_not_in_payload(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        self.assertNotIn("cwd", _read_orchestrator_events(ctx)[0])

    def test_no_null_values_in_emitted_event(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        for key, value in _read_orchestrator_events(ctx)[0].items():
            with self.subTest(key=key):
                self.assertIsNotNone(value)

    def test_writes_to_orchestrator_events_file(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        self.assertTrue(ctx.paths.orchestrator_events(TEST_RUN_ID).exists())

    def test_routes_to_unknown_run_when_run_id_is_none(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID}, self.tmp_path,
                        run_id=None)
        session_handlers.handle_session_start(ctx)
        self.assertTrue(ctx.paths.orchestrator_events("unknown-run").exists())


# ---------------------------------------------------------------------------
# sessionEnd -> session_end event
# ---------------------------------------------------------------------------

class TestHandleSessionEnd(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        ctx = _make_ctx("sessionEnd", {"sessionId": TEST_SESSION_ID, "reason": "clear"},
                        self.tmp_path)
        session_handlers.handle_session_end(ctx)
        self.assertEqual(1, len(_read_orchestrator_events(ctx)))

    def test_emitted_event_type_is_session_end(self):
        ctx = _make_ctx("sessionEnd", {"sessionId": TEST_SESSION_ID, "reason": "clear"},
                        self.tmp_path)
        session_handlers.handle_session_end(ctx)
        self.assertEqual("session_end", _read_orchestrator_events(ctx)[0]["event"])

    def test_reason_passed_through(self):
        for reason in ("clear", "resume", "logout", "timeout", "crash"):
            with self.subTest(reason=reason):
                sub = self.tmp_path / f"sub_{reason}"
                sub.mkdir()
                ctx = _make_ctx("sessionEnd", {"sessionId": TEST_SESSION_ID, "reason": reason},
                                sub)
                session_handlers.handle_session_end(ctx)
                self.assertEqual(reason, _read_orchestrator_events(ctx)[0]["reason"])

    def test_reason_absent_when_not_in_payload(self):
        ctx = _make_ctx("sessionEnd", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_session_end(ctx)
        self.assertNotIn("reason", _read_orchestrator_events(ctx)[0])

    def test_no_null_values_in_emitted_event(self):
        ctx = _make_ctx("sessionEnd", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_session_end(ctx)
        for key, value in _read_orchestrator_events(ctx)[0].items():
            with self.subTest(key=key):
                self.assertIsNotNone(value)


# ---------------------------------------------------------------------------
# userPromptSubmitted -> turn(user)
# ---------------------------------------------------------------------------

class TestHandleUserPromptSubmitted(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        ctx = _make_ctx("userPromptSubmitted",
                        {"sessionId": TEST_SESSION_ID, "prompt": "Hello GHCP!"},
                        self.tmp_path)
        session_handlers.handle_user_prompt_submitted(ctx)
        self.assertEqual(1, len(_read_orchestrator_events(ctx)))

    def test_emitted_event_type_is_turn(self):
        ctx = _make_ctx("userPromptSubmitted",
                        {"sessionId": TEST_SESSION_ID, "prompt": "Hello!"},
                        self.tmp_path)
        session_handlers.handle_user_prompt_submitted(ctx)
        self.assertEqual("turn", _read_orchestrator_events(ctx)[0]["event"])

    def test_role_is_user(self):
        ctx = _make_ctx("userPromptSubmitted",
                        {"sessionId": TEST_SESSION_ID, "prompt": "Hello!"},
                        self.tmp_path)
        session_handlers.handle_user_prompt_submitted(ctx)
        self.assertEqual("user", _read_orchestrator_events(ctx)[0]["role"])

    def test_content_matches_prompt_exactly(self):
        """Reads from camelCase 'prompt' field (not 'user_prompt')."""
        prompt_text = "What is the meaning of life?"
        ctx = _make_ctx("userPromptSubmitted",
                        {"sessionId": TEST_SESSION_ID, "prompt": prompt_text},
                        self.tmp_path)
        session_handlers.handle_user_prompt_submitted(ctx)
        self.assertEqual(prompt_text, _read_orchestrator_events(ctx)[0]["content"])

    def test_content_is_full_and_untruncated(self):
        long_prompt = "X" * 50_000
        ctx = _make_ctx("userPromptSubmitted",
                        {"sessionId": TEST_SESSION_ID, "prompt": long_prompt},
                        self.tmp_path)
        session_handlers.handle_user_prompt_submitted(ctx)
        self.assertEqual(long_prompt, _read_orchestrator_events(ctx)[0]["content"])

    def test_no_event_when_prompt_absent(self):
        ctx = _make_ctx("userPromptSubmitted", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_user_prompt_submitted(ctx)
        self.assertEqual(0, len(_read_orchestrator_events(ctx)))

    def test_no_event_when_prompt_is_empty_string(self):
        ctx = _make_ctx("userPromptSubmitted",
                        {"sessionId": TEST_SESSION_ID, "prompt": ""},
                        self.tmp_path)
        session_handlers.handle_user_prompt_submitted(ctx)
        self.assertEqual(0, len(_read_orchestrator_events(ctx)))

    def test_no_token_usage_in_user_turn(self):
        ctx = _make_ctx("userPromptSubmitted",
                        {"sessionId": TEST_SESSION_ID, "prompt": "Hello!"},
                        self.tmp_path)
        session_handlers.handle_user_prompt_submitted(ctx)
        self.assertNotIn("token_usage", _read_orchestrator_events(ctx)[0])

    def test_no_model_in_user_turn(self):
        ctx = _make_ctx("userPromptSubmitted",
                        {"sessionId": TEST_SESSION_ID, "prompt": "Hello!"},
                        self.tmp_path)
        session_handlers.handle_user_prompt_submitted(ctx)
        self.assertNotIn("model", _read_orchestrator_events(ctx)[0])


# ---------------------------------------------------------------------------
# agentStop -> turn(assistant) WITHOUT content
# ---------------------------------------------------------------------------

class TestHandleAgentStop(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_always_emits_exactly_one_event(self):
        """agentStop always emits -- no content guard (unlike claude-code's handle_stop)."""
        ctx = _make_ctx("agentStop", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_agent_stop(ctx)
        self.assertEqual(1, len(_read_orchestrator_events(ctx)))

    def test_emits_turn_event_type(self):
        ctx = _make_ctx("agentStop", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_agent_stop(ctx)
        self.assertEqual("turn", _read_orchestrator_events(ctx)[0]["event"])

    def test_role_is_assistant(self):
        ctx = _make_ctx("agentStop", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_agent_stop(ctx)
        self.assertEqual("assistant", _read_orchestrator_events(ctx)[0]["role"])

    def test_content_field_absent(self):
        """GHCP CLI agentStop has no last_assistant_message -- content must be omitted."""
        ctx = _make_ctx("agentStop", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_agent_stop(ctx)
        self.assertNotIn("content", _read_orchestrator_events(ctx)[0])

    def test_emits_without_transcript_path(self):
        """Missing transcriptPath must degrade gracefully -- event still emitted."""
        ctx = _make_ctx("agentStop", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        try:
            session_handlers.handle_agent_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_agent_stop raised when transcriptPath absent: {exc}")
        self.assertEqual(1, len(_read_orchestrator_events(ctx)))

    def test_model_from_transcript_included_when_available(self):
        transcript_path = self.tmp_path / "agent_transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 100, "output_tokens": 50},
            }},
        ])
        ctx = _make_ctx("agentStop",
                        {"sessionId": TEST_SESSION_ID, "transcriptPath": str(transcript_path)},
                        self.tmp_path)
        session_handlers.handle_agent_stop(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertIn("model", event)
        self.assertEqual("claude-opus-4-5", event["model"])

    def test_token_usage_from_transcript_included_when_available(self):
        transcript_path = self.tmp_path / "agent_transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 200, "output_tokens": 80},
            }},
        ])
        ctx = _make_ctx("agentStop",
                        {"sessionId": TEST_SESSION_ID, "transcriptPath": str(transcript_path)},
                        self.tmp_path)
        session_handlers.handle_agent_stop(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertIn("token_usage", event)
        self.assertEqual(200, event["token_usage"]["input_tokens"])

    def test_model_absent_when_transcript_missing(self):
        ctx = _make_ctx("agentStop",
                        {"sessionId": TEST_SESSION_ID,
                         "transcriptPath": str(self.tmp_path / "nonexistent.jsonl")},
                        self.tmp_path)
        session_handlers.handle_agent_stop(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertNotIn("model", event)

    def test_no_raw_written_when_transcript_path_absent(self):
        ctx = _make_ctx("agentStop", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_agent_stop(ctx)
        self.assertFalse(ctx.paths.orchestrator_raw(TEST_RUN_ID).exists())

    def test_exports_raw_transcript_when_path_is_valid(self):
        transcript_path = self.tmp_path / "agent_transcript.jsonl"
        content = b'{"type": "assistant"}\n'
        transcript_path.write_bytes(content)
        ctx = _make_ctx("agentStop",
                        {"sessionId": TEST_SESSION_ID, "transcriptPath": str(transcript_path)},
                        self.tmp_path)
        session_handlers.handle_agent_stop(ctx)
        raw = ctx.paths.orchestrator_raw(TEST_RUN_ID)
        self.assertTrue(raw.exists())
        self.assertEqual(content, raw.read_bytes())

    def test_handles_role_assistant_pattern_in_transcript(self):
        """Transcript reader accepts 'role: assistant' in addition to 'type: assistant',
        and the model field at the top level of the role-pattern record is extracted."""
        transcript_path = self.tmp_path / "agent_transcript.jsonl"
        _write_transcript(transcript_path, [
            {"role": "assistant", "model": "claude-sonnet-4-6"},
        ])
        ctx = _make_ctx("agentStop",
                        {"sessionId": TEST_SESSION_ID, "transcriptPath": str(transcript_path)},
                        self.tmp_path)
        session_handlers.handle_agent_stop(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertEqual("assistant", event["role"])
        self.assertEqual("claude-sonnet-4-6", event.get("model"))

    def test_does_not_raise(self):
        ctx = _make_ctx("agentStop", {"sessionId": TEST_SESSION_ID,
                                       "stopReason": "end_turn"}, self.tmp_path)
        try:
            session_handlers.handle_agent_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_agent_stop raised: {exc}")


# ---------------------------------------------------------------------------
# notification -> notification event
# ---------------------------------------------------------------------------

class TestHandleNotification(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        ctx = _make_ctx("notification",
                        {"sessionId": TEST_SESSION_ID, "notificationType": "info",
                         "message": "Something happened"},
                        self.tmp_path)
        session_handlers.handle_notification(ctx)
        self.assertEqual(1, len(_read_orchestrator_events(ctx)))

    def test_emitted_event_type_is_notification(self):
        ctx = _make_ctx("notification",
                        {"sessionId": TEST_SESSION_ID, "notificationType": "info"},
                        self.tmp_path)
        session_handlers.handle_notification(ctx)
        self.assertEqual("notification", _read_orchestrator_events(ctx)[0]["event"])

    def test_reads_notification_type_from_camelcase_field(self):
        """GHCP CLI uses 'notificationType' (camelCase), not 'notification_type'."""
        ctx = _make_ctx("notification",
                        {"sessionId": TEST_SESSION_ID, "notificationType": "warning"},
                        self.tmp_path)
        session_handlers.handle_notification(ctx)
        self.assertEqual("warning", _read_orchestrator_events(ctx)[0]["notification_type"])

    def test_notification_type_values_passed_through(self):
        for ntype in ("info", "warning", "error", "debug", "custom_future_type"):
            with self.subTest(ntype=ntype):
                sub = self.tmp_path / f"sub_{ntype}"
                sub.mkdir()
                ctx = _make_ctx("notification",
                                {"sessionId": TEST_SESSION_ID, "notificationType": ntype},
                                sub)
                session_handlers.handle_notification(ctx)
                event = _read_orchestrator_events(ctx)[0]
                self.assertEqual(ntype, event["notification_type"])

    def test_message_included_when_present(self):
        ctx = _make_ctx("notification",
                        {"sessionId": TEST_SESSION_ID, "notificationType": "info",
                         "message": "Context window is nearly full."},
                        self.tmp_path)
        session_handlers.handle_notification(ctx)
        self.assertEqual("Context window is nearly full.",
                         _read_orchestrator_events(ctx)[0]["message"])

    def test_message_absent_when_not_in_payload(self):
        ctx = _make_ctx("notification",
                        {"sessionId": TEST_SESSION_ID, "notificationType": "info"},
                        self.tmp_path)
        session_handlers.handle_notification(ctx)
        self.assertNotIn("message", _read_orchestrator_events(ctx)[0])

    def test_no_event_when_notification_type_absent(self):
        """notificationType is required; absent type means nothing to emit."""
        ctx = _make_ctx("notification",
                        {"sessionId": TEST_SESSION_ID, "message": "Something happened"},
                        self.tmp_path)
        session_handlers.handle_notification(ctx)
        self.assertEqual(0, len(_read_orchestrator_events(ctx)))

    def test_no_event_when_notification_type_is_empty_string(self):
        ctx = _make_ctx("notification",
                        {"sessionId": TEST_SESSION_ID, "notificationType": "",
                         "message": "Something happened"},
                        self.tmp_path)
        session_handlers.handle_notification(ctx)
        self.assertEqual(0, len(_read_orchestrator_events(ctx)))


# ---------------------------------------------------------------------------
# preCompact -> compaction event
# ---------------------------------------------------------------------------

class TestHandlePreCompact(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        ctx = _make_ctx("preCompact",
                        {"sessionId": TEST_SESSION_ID, "trigger": "auto"},
                        self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        self.assertEqual(1, len(_read_orchestrator_events(ctx)))

    def test_emitted_event_type_is_compaction(self):
        ctx = _make_ctx("preCompact",
                        {"sessionId": TEST_SESSION_ID, "trigger": "auto"},
                        self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        self.assertEqual("compaction", _read_orchestrator_events(ctx)[0]["event"])

    def test_trigger_included_when_present(self):
        ctx = _make_ctx("preCompact",
                        {"sessionId": TEST_SESSION_ID, "trigger": "manual"},
                        self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        self.assertEqual("manual", _read_orchestrator_events(ctx)[0]["trigger"])

    def test_auto_trigger_passed_through(self):
        ctx = _make_ctx("preCompact",
                        {"sessionId": TEST_SESSION_ID, "trigger": "auto"},
                        self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        self.assertEqual("auto", _read_orchestrator_events(ctx)[0]["trigger"])

    def test_tokens_before_absent(self):
        """GHCP CLI preCompact payload has no token count fields; must not be fabricated."""
        ctx = _make_ctx("preCompact",
                        {"sessionId": TEST_SESSION_ID, "trigger": "auto"},
                        self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        self.assertNotIn("tokens_before", _read_orchestrator_events(ctx)[0])

    def test_emits_even_without_trigger(self):
        ctx = _make_ctx("preCompact", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        self.assertEqual(1, len(_read_orchestrator_events(ctx)))

    def test_does_not_raise(self):
        ctx = _make_ctx("preCompact", {"sessionId": TEST_SESSION_ID, "trigger": "auto"},
                        self.tmp_path)
        try:
            session_handlers.handle_pre_compact(ctx)
        except Exception as exc:
            self.fail(f"handle_pre_compact raised: {exc}")


# ---------------------------------------------------------------------------
# userPromptTransformed -> observational prompt_transformed event
# ---------------------------------------------------------------------------

class TestHandleUserPromptTransformed(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        ctx = _make_ctx("userPromptTransformed",
                        {"sessionId": TEST_SESSION_ID,
                         "transformedPrompt": "Enhanced prompt text"},
                        self.tmp_path)
        session_handlers.handle_user_prompt_transformed(ctx)
        self.assertEqual(1, len(_read_orchestrator_events(ctx)))

    def test_emitted_event_type_is_prompt_transformed(self):
        ctx = _make_ctx("userPromptTransformed",
                        {"sessionId": TEST_SESSION_ID,
                         "transformedPrompt": "Enhanced prompt"},
                        self.tmp_path)
        session_handlers.handle_user_prompt_transformed(ctx)
        self.assertEqual("prompt_transformed", _read_orchestrator_events(ctx)[0]["event"])

    def test_transformed_prompt_included_when_present(self):
        ctx = _make_ctx("userPromptTransformed",
                        {"sessionId": TEST_SESSION_ID,
                         "transformedPrompt": "System-injected context + user prompt"},
                        self.tmp_path)
        session_handlers.handle_user_prompt_transformed(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertEqual("System-injected context + user prompt",
                         event["transformed_prompt"])

    def test_transformed_prompt_absent_when_not_in_payload(self):
        ctx = _make_ctx("userPromptTransformed",
                        {"sessionId": TEST_SESSION_ID},
                        self.tmp_path)
        session_handlers.handle_user_prompt_transformed(ctx)
        self.assertNotIn("transformed_prompt", _read_orchestrator_events(ctx)[0])

    def test_emits_to_orchestrator_stream(self):
        ctx = _make_ctx("userPromptTransformed",
                        {"sessionId": TEST_SESSION_ID,
                         "transformedPrompt": "Transformed"},
                        self.tmp_path)
        session_handlers.handle_user_prompt_transformed(ctx)
        self.assertTrue(ctx.paths.orchestrator_events(TEST_RUN_ID).exists())

    def test_writes_nothing_to_stdout(self):
        ctx = _make_ctx("userPromptTransformed",
                        {"sessionId": TEST_SESSION_ID,
                         "transformedPrompt": "Transformed prompt"},
                        self.tmp_path)
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            session_handlers.handle_user_prompt_transformed(ctx)
        self.assertEqual("", buf.getvalue())

    def test_does_not_raise(self):
        ctx = _make_ctx("userPromptTransformed",
                        {"sessionId": TEST_SESSION_ID,
                         "transformedPrompt": "Hello"},
                        self.tmp_path)
        try:
            session_handlers.handle_user_prompt_transformed(ctx)
        except Exception as exc:
            self.fail(f"handle_user_prompt_transformed raised: {exc}")


# ---------------------------------------------------------------------------
# permissionRequest -> observational permission_request event (CRITICAL SAFETY)
# ---------------------------------------------------------------------------

class TestHandlePermissionRequest(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        ctx = _make_ctx("permissionRequest",
                        {"sessionId": TEST_SESSION_ID, "toolName": "bash"},
                        self.tmp_path)
        session_handlers.handle_permission_request(ctx)
        self.assertEqual(1, len(_read_orchestrator_events(ctx)))

    def test_emitted_event_type_is_permission_request(self):
        ctx = _make_ctx("permissionRequest",
                        {"sessionId": TEST_SESSION_ID, "toolName": "bash"},
                        self.tmp_path)
        session_handlers.handle_permission_request(ctx)
        self.assertEqual("permission_request", _read_orchestrator_events(ctx)[0]["event"])

    def test_tool_name_included_when_present(self):
        ctx = _make_ctx("permissionRequest",
                        {"sessionId": TEST_SESSION_ID, "toolName": "create"},
                        self.tmp_path)
        session_handlers.handle_permission_request(ctx)
        self.assertEqual("create", _read_orchestrator_events(ctx)[0]["tool_name"])

    def test_writes_nothing_to_stdout(self):
        """CRITICAL SAFETY: GHCP CLI parses stdout from permissionRequest as {behavior: allow|deny}.
        Any stdout output could inadvertently deny tool calls."""
        ctx = _make_ctx("permissionRequest",
                        {"sessionId": TEST_SESSION_ID, "toolName": "bash"},
                        self.tmp_path)
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            session_handlers.handle_permission_request(ctx)
        self.assertEqual("", buf.getvalue())

    def test_no_permission_decision_field_in_event(self):
        """Must never emit a permissionDecision field."""
        ctx = _make_ctx("permissionRequest",
                        {"sessionId": TEST_SESSION_ID, "toolName": "bash"},
                        self.tmp_path)
        session_handlers.handle_permission_request(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertNotIn("permissionDecision", event)
        self.assertNotIn("behavior", event)
        self.assertNotIn("decision", event)

    def test_no_stdout_for_any_tool_name(self):
        for tool in ("bash", "view", "create", "edit", "agent", "task"):
            with self.subTest(tool=tool):
                sub = self.tmp_path / f"sub_{tool}"
                sub.mkdir()
                ctx = _make_ctx("permissionRequest",
                                {"sessionId": TEST_SESSION_ID, "toolName": tool},
                                sub)
                buf = io.StringIO()
                with contextlib.redirect_stdout(buf):
                    session_handlers.handle_permission_request(ctx)
                self.assertEqual("", buf.getvalue())

    def test_does_not_raise(self):
        ctx = _make_ctx("permissionRequest",
                        {"sessionId": TEST_SESSION_ID, "toolName": "bash"},
                        self.tmp_path)
        try:
            session_handlers.handle_permission_request(ctx)
        except Exception as exc:
            self.fail(f"handle_permission_request raised: {exc}")

    def test_emits_even_when_tool_name_absent(self):
        ctx = _make_ctx("permissionRequest", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_permission_request(ctx)
        self.assertEqual(1, len(_read_orchestrator_events(ctx)))


# ---------------------------------------------------------------------------
# errorOccurred -> observational error_occurred event
# ---------------------------------------------------------------------------

class TestHandleErrorOccurred(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        ctx = _make_ctx("errorOccurred",
                        {"sessionId": TEST_SESSION_ID,
                         "error": {"message": "Something failed", "name": "Error"},
                         "recoverable": True},
                        self.tmp_path)
        session_handlers.handle_error_occurred(ctx)
        self.assertEqual(1, len(_read_orchestrator_events(ctx)))

    def test_emitted_event_type_is_error_occurred(self):
        ctx = _make_ctx("errorOccurred",
                        {"sessionId": TEST_SESSION_ID,
                         "error": {"message": "Oops", "name": "Error"}},
                        self.tmp_path)
        session_handlers.handle_error_occurred(ctx)
        self.assertEqual("error_occurred", _read_orchestrator_events(ctx)[0]["event"])

    def test_error_message_extracted_from_nested_error_dict(self):
        """error is a nested dict {message, name, stack?}; reads error.message."""
        ctx = _make_ctx("errorOccurred",
                        {"sessionId": TEST_SESSION_ID,
                         "error": {"message": "Connection refused", "name": "NetworkError"}},
                        self.tmp_path)
        session_handlers.handle_error_occurred(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertIn("error_message", event)
        self.assertEqual("Connection refused", event["error_message"])

    def test_error_message_absent_when_error_is_not_a_dict(self):
        ctx = _make_ctx("errorOccurred",
                        {"sessionId": TEST_SESSION_ID, "error": "flat string error"},
                        self.tmp_path)
        session_handlers.handle_error_occurred(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertNotIn("error_message", event)

    def test_error_message_absent_when_error_field_absent(self):
        ctx = _make_ctx("errorOccurred", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.handle_error_occurred(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertNotIn("error_message", event)

    def test_error_context_included_when_present(self):
        ctx = _make_ctx("errorOccurred",
                        {"sessionId": TEST_SESSION_ID,
                         "error": {"message": "Oops", "name": "Error"},
                         "errorContext": "During tool execution"},
                        self.tmp_path)
        session_handlers.handle_error_occurred(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertEqual("During tool execution", event["error_context"])

    def test_recoverable_included_when_true(self):
        ctx = _make_ctx("errorOccurred",
                        {"sessionId": TEST_SESSION_ID,
                         "error": {"message": "Oops", "name": "Error"},
                         "recoverable": True},
                        self.tmp_path)
        session_handlers.handle_error_occurred(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertIn("recoverable", event)
        self.assertIs(True, event["recoverable"])

    def test_recoverable_false_kept_not_pruned(self):
        """False is a genuine boolean value; the serializer must not prune it as falsy.
        Mirrors test_false_resumed_kept_not_pruned for the resumed field on sessionStart."""
        ctx = _make_ctx("errorOccurred",
                        {"sessionId": TEST_SESSION_ID,
                         "error": {"message": "Transient failure", "name": "Error"},
                         "recoverable": False},
                        self.tmp_path)
        session_handlers.handle_error_occurred(ctx)
        event = _read_orchestrator_events(ctx)[0]
        self.assertIn("recoverable", event)
        self.assertIs(False, event["recoverable"])
        self.assertIsInstance(event["recoverable"], bool)

    def test_writes_nothing_to_stdout(self):
        ctx = _make_ctx("errorOccurred",
                        {"sessionId": TEST_SESSION_ID,
                         "error": {"message": "Crash", "name": "FatalError"}},
                        self.tmp_path)
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            session_handlers.handle_error_occurred(ctx)
        self.assertEqual("", buf.getvalue())

    def test_does_not_raise(self):
        ctx = _make_ctx("errorOccurred", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        try:
            session_handlers.handle_error_occurred(ctx)
        except Exception as exc:
            self.fail(f"handle_error_occurred raised: {exc}")


# ---------------------------------------------------------------------------
# emit_run_start and emit_run_end (called by dispatcher)
# ---------------------------------------------------------------------------

class TestEmitRunLifecycle(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emit_run_start_writes_run_start_event(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.emit_run_start(ctx)
        events = _read_orchestrator_events(ctx)
        self.assertEqual(1, len(events))
        self.assertEqual("run_start", events[0]["event"])

    def test_emit_run_start_harness_is_ghcp_cli(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.emit_run_start(ctx)
        self.assertEqual("ghcp-cli", _read_orchestrator_events(ctx)[0]["harness"])

    def test_emit_run_end_writes_run_end_event(self):
        ctx = _make_ctx("sessionEnd", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.emit_run_end(ctx, "clear")
        events = _read_orchestrator_events(ctx)
        self.assertEqual(1, len(events))
        self.assertEqual("run_end", events[0]["event"])

    def test_emit_run_end_includes_outcome(self):
        ctx = _make_ctx("sessionEnd", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.emit_run_end(ctx, "timeout")
        event = _read_orchestrator_events(ctx)[0]
        self.assertEqual("timeout", event["outcome"])

    def test_emit_run_end_outcome_absent_when_none(self):
        ctx = _make_ctx("sessionEnd", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        session_handlers.emit_run_end(ctx, None)
        event = _read_orchestrator_events(ctx)[0]
        self.assertNotIn("outcome", event)

    def test_emit_run_start_does_not_raise(self):
        ctx = _make_ctx("sessionStart", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        try:
            session_handlers.emit_run_start(ctx)
        except Exception as exc:
            self.fail(f"emit_run_start raised: {exc}")

    def test_emit_run_end_does_not_raise(self):
        ctx = _make_ctx("sessionEnd", {"sessionId": TEST_SESSION_ID}, self.tmp_path)
        try:
            session_handlers.emit_run_end(ctx, "clear")
        except Exception as exc:
            self.fail(f"emit_run_end raised: {exc}")


if __name__ == "__main__":
    unittest.main()
