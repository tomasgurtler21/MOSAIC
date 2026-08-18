"""Tests for session-scoped event handlers in mosaic_logger_handlers_session
(vscode-ghcp variant).

Covers:
  SessionStart -> session_start event with resumed derivation (source=="resume"
    -> True, other documented values -> False, absent -> omitted)
  UserPromptSubmit -> user turn from the 'prompt' field (not 'user_prompt')
  PreCompact -> compaction event with only schema-defined fields (trigger,
    tokens_before); custom_instructions is NOT emitted
  Stop -> assistant turn from last_assistant_message (omitted when absent)
    AND session_end emitted unconditionally without a reason field
  emit_run_start -> run_start event written by the dispatcher before the
    handler registry, with run_id, cwd, adapter_version (model absent for VS Code)
  emit_run_end -> run_end event always omits outcome regardless of parameter

Key vscode-ghcp divergences tested here:
  D5: session lifecycle anchors on SessionStart/Stop (no SessionEnd)
  D5: session_end.reason never populated from stop_reason
  D5: run_end.outcome never populated
  D5: session_end emitted unconditionally on Stop (not conditional on turn)
  PreCompact: custom_instructions absent from emitted event
  UserPromptSubmit: field name is 'prompt', not 'user_prompt'
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
TEST_SESSION_ID = "test-session-vscode-abc"
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
# SessionStart -> session_start event
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

    def test_session_id_is_present(self):
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

    def test_harness_is_vscode_ghcp(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_session_start(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("vscode-ghcp", event["harness"])

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

    def test_resumed_false_when_source_is_new(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "new",
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

    def test_model_absent_by_default_for_vscode(self):
        """VS Code SessionStart does not carry a model field; model must be absent."""
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
        unknown_run_events = ctx.paths.orchestrator_events("unknown-run")
        self.assertTrue(unknown_run_events.exists(),
                        "Event must be routed to unknown-run/ when run_id is None")

    def test_handler_does_not_raise(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "source": "startup",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        try:
            session_handlers.handle_session_start(ctx)
        except Exception as exc:
            self.fail(f"handle_session_start raised unexpectedly: {exc}")


# ---------------------------------------------------------------------------
# UserPromptSubmit -> user turn (reads 'prompt', not 'user_prompt')
# ---------------------------------------------------------------------------

class TestHandleUserPromptSubmit(unittest.TestCase):
    """handle_user_prompt_submit emits a turn with role 'user'.

    VS Code names the field 'prompt'; 'user_prompt' is the claude-code field
    name and must NOT be used here.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "prompt": "Hello, assistant!",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))

    def test_emitted_event_type_is_turn(self):
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "prompt": "Hello!",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("turn", event["event"])

    def test_role_is_user(self):
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "prompt": "Hello!",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("user", event["role"])

    def test_content_comes_from_prompt_field_not_user_prompt(self):
        """VS Code uses 'prompt'; content must match that field, not 'user_prompt'."""
        text = "What does this function do?"
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "prompt": text,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual(text, event["content"])

    def test_no_event_when_prompt_absent(self):
        """With no prompt field, no turn event is emitted."""
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        events = _read_events(ctx)
        self.assertEqual(0, len(events))

    def test_no_event_when_prompt_is_empty_string(self):
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "prompt": "",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        events = _read_events(ctx)
        self.assertEqual(0, len(events))

    def test_user_prompt_field_does_not_trigger_event(self):
        """'user_prompt' is the claude-code field name; using it must produce no event here."""
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "user_prompt": "This is the wrong field name for VS Code.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        events = _read_events(ctx)
        self.assertEqual(0, len(events),
                         "'user_prompt' is the claude-code field; VS Code uses 'prompt'")

    def test_content_is_full_and_untruncated(self):
        long_prompt = "A" * 50_000
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "prompt": long_prompt,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual(len(long_prompt), len(event["content"]))
        self.assertEqual(long_prompt, event["content"])

    def test_user_turn_has_no_token_usage(self):
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "prompt": "Hello!",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("token_usage", event)

    def test_user_turn_has_no_model(self):
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "prompt": "Hello!",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_user_prompt_submit(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("model", event)

    def test_handler_does_not_raise(self):
        payload = {
            "hook_event_name": "UserPromptSubmit",
            "session_id": TEST_SESSION_ID,
            "prompt": "Hello!",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        try:
            session_handlers.handle_user_prompt_submit(ctx)
        except Exception as exc:
            self.fail(f"handle_user_prompt_submit raised unexpectedly: {exc}")


# ---------------------------------------------------------------------------
# PreCompact -> compaction event (schema-defined fields only, no custom_instructions)
# ---------------------------------------------------------------------------

class TestHandlePreCompact(unittest.TestCase):
    """handle_pre_compact emits a compaction event with only schema-defined fields.

    VS Code's payload includes custom_instructions, but the schema defines only
    trigger, tokens_before, and tokens_after for the compaction event.
    custom_instructions has no schema field and must NOT be emitted.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_exactly_one_event(self):
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
            "custom_instructions": "some instructions",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))

    def test_emitted_event_type_is_compaction(self):
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual("compaction", event["event"])

    def test_trigger_included_when_present(self):
        for trigger_value in ("auto", "manual"):
            with self.subTest(trigger=trigger_value):
                tmp_sub = self.tmp_path / f"sub_{trigger_value}"
                tmp_sub.mkdir()
                payload = {
                    "hook_event_name": "PreCompact",
                    "session_id": TEST_SESSION_ID,
                    "trigger": trigger_value,
                }
                ctx = _make_ctx(payload, tmp_sub)
                session_handlers.handle_pre_compact(ctx)
                event = _read_events(ctx)[0]
                self.assertEqual(trigger_value, event["trigger"])

    def test_tokens_before_included_when_present(self):
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
            "tokens_before": 180_000,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        event = _read_events(ctx)[0]
        self.assertEqual(180_000, event.get("tokens_before"))

    def test_custom_instructions_not_emitted(self):
        """custom_instructions has no schema field and must NEVER appear in the emitted event."""
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
            "custom_instructions": "Do not reveal confidential data.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn(
            "custom_instructions", event,
            "custom_instructions has no schema field and must not be emitted "
            "(degrade-never-fabricate rule)"
        )

    def test_tokens_after_not_emitted_by_pre_compact(self):
        """PreCompact does not supply tokens_after; it must not be fabricated."""
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        event = _read_events(ctx)[0]
        self.assertNotIn("tokens_after", event)

    def test_no_null_values_in_emitted_event(self):
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_pre_compact(ctx)
        event = _read_events(ctx)[0]
        for key, value in event.items():
            with self.subTest(key=key):
                self.assertIsNotNone(value)

    def test_handler_does_not_raise(self):
        payload = {
            "hook_event_name": "PreCompact",
            "session_id": TEST_SESSION_ID,
            "trigger": "auto",
            "custom_instructions": "whatever",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        try:
            session_handlers.handle_pre_compact(ctx)
        except Exception as exc:
            self.fail(f"handle_pre_compact raised unexpectedly: {exc}")


# ---------------------------------------------------------------------------
# Stop -> assistant turn + session_end (unconditionally, without reason)
# ---------------------------------------------------------------------------

class TestHandleStop(unittest.TestCase):
    """handle_stop emits an assistant turn (when last_assistant_message present)
    and a session_end event unconditionally.

    Key vscode-ghcp divergences (D5):
    - session_end is ALWAYS emitted, not just when a turn was emitted
    - session_end.reason is NEVER populated (stop_reason is not a valid mapping)
    - session_end is a separate event from the turn, so Stop emits 2 events when
      last_assistant_message is present and 1 event when it is absent
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_two_events_when_last_assistant_message_present(self):
        """When last_assistant_message present: turn + session_end = 2 events."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Here is my response.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        self.assertEqual(2, len(events),
                         "Stop with last_assistant_message must emit turn + session_end")

    def test_emits_session_end_unconditionally_when_no_last_assistant_message(self):
        """session_end is emitted even when last_assistant_message is absent."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        event_types = [e["event"] for e in events]
        self.assertIn("session_end", event_types,
                      "session_end must be emitted even when last_assistant_message is absent")

    def test_emits_exactly_one_event_when_last_assistant_message_absent(self):
        """When last_assistant_message is absent, only session_end is emitted."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events),
                         "Only session_end should be emitted when last_assistant_message is absent")

    def test_session_end_event_type(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        session_end = next(e for e in events if e["event"] == "session_end")
        self.assertEqual("session_end", session_end["event"])

    def test_session_end_carries_session_id(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        session_end = next(e for e in events if e["event"] == "session_end")
        self.assertEqual(TEST_SESSION_ID, session_end["session_id"])

    def test_session_end_reason_absent_always(self):
        """reason must never be populated: stop_reason is not a valid mapping."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "stop_reason": "end_turn",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        session_end = next(e for e in events if e["event"] == "session_end")
        self.assertNotIn(
            "reason", session_end,
            "session_end.reason must NEVER be populated — stop_reason is not "
            "a member of the complete|error|abort|timeout|user_exit enum"
        )

    def test_session_end_reason_absent_even_when_stop_reason_present(self):
        """Explicitly verify stop_reason does not leak into session_end.reason."""
        for stop_reason in ("end_turn", "max_tokens", "stop_sequence", "tool_use"):
            with self.subTest(stop_reason=stop_reason):
                tmp_sub = self.tmp_path / f"sub_{stop_reason}"
                tmp_sub.mkdir()
                payload = {
                    "hook_event_name": "Stop",
                    "session_id": TEST_SESSION_ID,
                    "stop_reason": stop_reason,
                }
                ctx = _make_ctx(payload, tmp_sub)
                session_handlers.handle_stop(ctx)
                events_sub = [
                    json.loads(line)
                    for line in ctx.paths.orchestrator_events(TEST_RUN_ID)
                        .read_text(encoding="utf-8").splitlines()
                    if line.strip()
                ] if ctx.paths.orchestrator_events(TEST_RUN_ID).exists() else []
                session_end = next(
                    (e for e in events_sub if e.get("event") == "session_end"), None
                )
                self.assertIsNotNone(session_end, f"session_end must be emitted for stop_reason={stop_reason}")
                self.assertNotIn("reason", session_end,
                                 f"reason must not appear in session_end for stop_reason={stop_reason}")

    def test_assistant_turn_event_type(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        turn = next((e for e in events if e["event"] == "turn"), None)
        self.assertIsNotNone(turn)
        self.assertEqual("turn", turn["event"])

    def test_assistant_turn_role(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        turn = next(e for e in events if e["event"] == "turn")
        self.assertEqual("assistant", turn["role"])

    def test_assistant_turn_content_matches_last_assistant_message(self):
        message = "Here is a detailed response."
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": message,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        turn = next(e for e in events if e["event"] == "turn")
        self.assertEqual(message, turn["content"])

    def test_assistant_turn_content_is_full_and_untruncated(self):
        long_message = "B" * 100_000
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": long_message,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        turn = next(e for e in events if e["event"] == "turn")
        self.assertEqual(len(long_message), len(turn["content"]))
        self.assertEqual(long_message, turn["content"])

    def test_no_turn_when_last_assistant_message_absent(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        turns = [e for e in events if e.get("event") == "turn"]
        self.assertEqual(0, len(turns))

    def test_model_from_transcript_included_in_assistant_turn(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
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
        events = _read_events(ctx)
        turn = next(e for e in events if e["event"] == "turn")
        self.assertIn("model", turn)
        self.assertEqual("claude-opus-4-5", turn["model"])

    def test_token_usage_from_transcript_included_in_assistant_turn(self):
        transcript_path = self.tmp_path / "transcript.jsonl"
        _write_transcript(transcript_path, [
            {"type": "assistant", "message": {
                "model": "claude-opus-4-5",
                "usage": {"input_tokens": 200, "output_tokens": 75},
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
        events = _read_events(ctx)
        turn = next(e for e in events if e["event"] == "turn")
        self.assertIn("token_usage", turn)
        self.assertEqual(200, turn["token_usage"]["input_tokens"])
        self.assertEqual(75, turn["token_usage"]["output_tokens"])

    def test_session_end_still_emitted_when_turn_has_no_transcript(self):
        """session_end must be written even when transcript is absent/unreadable."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(self.tmp_path / "nonexistent.jsonl"),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        try:
            session_handlers.handle_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_stop raised when transcript missing: {exc}")
        events = _read_events(ctx)
        event_types = [e["event"] for e in events]
        self.assertIn("session_end", event_types,
                      "session_end must still be emitted even when transcript is unreadable")

    def test_no_null_values_in_session_end(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        events = _read_events(ctx)
        session_end = next(e for e in events if e["event"] == "session_end")
        for key, value in session_end.items():
            with self.subTest(key=key):
                self.assertIsNotNone(value)

    def test_handler_does_not_raise(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        try:
            session_handlers.handle_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_stop raised unexpectedly: {exc}")

    def test_stop_exports_raw_transcript_when_transcript_path_present(self):
        """handle_stop exports transcript to 00_orchestrator_session.raw."""
        content = b'{"type": "user"}\n{"type": "assistant"}\n'
        src = self.tmp_path / "source_transcript.jsonl"
        src.write_bytes(content)
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
            "last_assistant_message": "Done.",
            "transcript_path": str(src),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.handle_stop(ctx)
        raw_path = ctx.paths.orchestrator_raw(TEST_RUN_ID)
        self.assertTrue(raw_path.exists(), "00_orchestrator_session.raw was not created")

    def test_no_raw_written_when_transcript_path_absent(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        try:
            session_handlers.handle_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_stop raised with absent transcript_path: {exc}")
        raw_path = ctx.paths.orchestrator_raw(TEST_RUN_ID)
        self.assertFalse(raw_path.exists())


# ---------------------------------------------------------------------------
# emit_run_start and emit_run_end
# ---------------------------------------------------------------------------

class TestEmitRunStart(unittest.TestCase):
    """emit_run_start writes a run_start event to the orchestrator stream.

    Called by the dispatcher on SessionStart (not registered in HANDLERS).
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_run_start_event(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "cwd": str(self.tmp_path),
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.emit_run_start(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))
        self.assertEqual("run_start", events[0]["event"])

    def test_run_start_has_run_id(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.emit_run_start(ctx)
        events = _read_events(ctx)
        self.assertIn("run_id", events[0])

    def test_run_start_cwd_included_when_present(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
            "cwd": "/some/workspace",
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.emit_run_start(ctx)
        events = _read_events(ctx)
        self.assertEqual("/some/workspace", events[0].get("cwd"))

    def test_run_start_model_absent_for_vscode(self):
        """VS Code SessionStart does not carry a model field; run_start.model must be absent."""
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.emit_run_start(ctx)
        events = _read_events(ctx)
        self.assertNotIn("model", events[0])

    def test_run_start_harness_is_vscode_ghcp(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.emit_run_start(ctx)
        events = _read_events(ctx)
        self.assertEqual("vscode-ghcp", events[0]["harness"])

    def test_emit_run_start_does_not_raise(self):
        payload = {
            "hook_event_name": "SessionStart",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        try:
            session_handlers.emit_run_start(ctx)
        except Exception as exc:
            self.fail(f"emit_run_start raised: {exc}")


class TestEmitRunEnd(unittest.TestCase):
    """emit_run_end writes a run_end event.

    D5: outcome is always omitted regardless of the outcome parameter value,
    because VS Code's stop_reason is not a valid mapping to the outcome enum.
    The parameter is retained so a future mapping requires only a call-site change.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_emits_run_end_event(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.emit_run_end(ctx)
        events = _read_events(ctx)
        self.assertEqual(1, len(events))
        self.assertEqual("run_end", events[0]["event"])

    def test_outcome_absent_when_called_with_none(self):
        """The dispatcher always passes outcome=None; the field must be absent."""
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.emit_run_end(ctx, outcome=None)
        events = _read_events(ctx)
        self.assertNotIn(
            "outcome", events[0],
            "outcome must be absent when called with outcome=None (D5)"
        )

    def test_run_end_harness_is_vscode_ghcp(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        session_handlers.emit_run_end(ctx)
        events = _read_events(ctx)
        self.assertEqual("vscode-ghcp", events[0]["harness"])

    def test_emit_run_end_does_not_raise(self):
        payload = {
            "hook_event_name": "Stop",
            "session_id": TEST_SESSION_ID,
        }
        ctx = _make_ctx(payload, self.tmp_path)
        try:
            session_handlers.emit_run_end(ctx)
        except Exception as exc:
            self.fail(f"emit_run_end raised: {exc}")


# ---------------------------------------------------------------------------
# HANDLERS registry — session module
# ---------------------------------------------------------------------------

class TestSessionHandlersRegistry(unittest.TestCase):
    """HANDLERS in the session module contains exactly the 4 session events."""

    def test_handlers_is_a_dict(self):
        self.assertIsInstance(session_handlers.HANDLERS, dict)

    def test_session_start_registered(self):
        self.assertIn("SessionStart", session_handlers.HANDLERS)

    def test_user_prompt_submit_registered(self):
        self.assertIn("UserPromptSubmit", session_handlers.HANDLERS)

    def test_pre_compact_registered(self):
        self.assertIn("PreCompact", session_handlers.HANDLERS)

    def test_stop_registered(self):
        self.assertIn("Stop", session_handlers.HANDLERS)

    def test_session_end_absent(self):
        """SessionEnd is not confirmed for VS Code and must be absent from HANDLERS."""
        self.assertNotIn("SessionEnd", session_handlers.HANDLERS)

    def test_notification_absent(self):
        """Notification is not confirmed for VS Code and must be absent from HANDLERS."""
        self.assertNotIn("Notification", session_handlers.HANDLERS)

    def test_post_compact_absent(self):
        """PostCompact is not confirmed for VS Code and must be absent from HANDLERS."""
        self.assertNotIn("PostCompact", session_handlers.HANDLERS)

    def test_all_handlers_callable(self):
        for event, handler in session_handlers.HANDLERS.items():
            with self.subTest(event=event):
                self.assertTrue(callable(handler))

    def test_handlers_has_exactly_four_entries(self):
        self.assertEqual(4, len(session_handlers.HANDLERS))


if __name__ == "__main__":
    unittest.main()
