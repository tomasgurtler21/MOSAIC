"""Tests for the always-exit-0 safety boundary in mosaic_logger (vscode-ghcp variant).

Key vscode-ghcp dispatcher properties:
  - dispatch(raw_input) takes a single argument; event name is read from
    payload['hook_event_name'], NOT from sys.argv[1]
  - HANDLERS contains exactly 8 PascalCase keys (VS Code event names)
  - SessionEnd, Notification, PostToolUseFailure, PostCompact are absent
  - emit_run_start is called for SessionStart (outside the handler registry)
  - emit_run_end is called for Stop (outside the registry), always, even when
    the Stop handler raised
  - RUN_ID_PROMPT_FIELDS maps UserPromptSubmit->prompt, SubagentStop->
    last_assistant_message, Stop->last_assistant_message; SubagentStart absent
  - The process exits 0 and writes zero bytes to stdout for every input

Covers:
  dispatch(): unknown event name, malformed JSON, non-object JSON, empty input,
    missing hook_event_name, handler exception — all silent no-ops
  HANDLERS: exactly 8 keys, 4 absent keys confirmed, all callable
  RUN_ID_PROMPT_FIELDS: correct field mappings, SubagentStart absent,
    'prompt' (not 'user_prompt') for UserPromptSubmit
  run lifecycle: emit_run_start on SessionStart, emit_run_end on Stop even
    when Stop handler raises
  main(): exits 0 and produces zero bytes stdout for every input type
"""

import contextlib
import io
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest
import unittest.mock

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger

_ADAPTER_PATH = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    "mosaic_logger.py",
)


def _run_dispatch(raw_input: str) -> str:
    """Call dispatch(raw_input) capturing any stdout it produces. Returns captured text."""
    buf = io.StringIO()
    with contextlib.redirect_stdout(buf):
        mosaic_logger.dispatch(raw_input)
    return buf.getvalue()


# ---------------------------------------------------------------------------
# dispatch() — unknown event
# ---------------------------------------------------------------------------

class TestDispatchUnknownEvent(unittest.TestCase):
    """An unregistered hook_event_name is a silent no-op."""

    def test_unknown_event_does_not_raise(self):
        payload = json.dumps({"hook_event_name": "NonExistentEvent", "session_id": "s1"})
        try:
            _run_dispatch(payload)
        except Exception as e:
            self.fail(f"dispatch raised for unknown event: {e}")

    def test_unknown_event_emits_nothing_to_stdout(self):
        payload = json.dumps({"hook_event_name": "NonExistentEvent", "session_id": "s1"})
        self.assertEqual("", _run_dispatch(payload))

    def test_vscode_excluded_session_end_is_silent_noop(self):
        """SessionEnd is excluded; an unexpected firing must be silently dropped."""
        payload = json.dumps({"hook_event_name": "SessionEnd", "session_id": "s1"})
        try:
            stdout = _run_dispatch(payload)
        except Exception as e:
            self.fail(f"dispatch raised for excluded SessionEnd event: {e}")
        self.assertEqual("", stdout)

    def test_vscode_excluded_notification_is_silent_noop(self):
        """Notification is excluded; must be silently dropped."""
        payload = json.dumps({"hook_event_name": "Notification", "session_id": "s1",
                               "notification_type": "info"})
        stdout = _run_dispatch(payload)
        self.assertEqual("", stdout)

    def test_vscode_excluded_post_tool_use_failure_is_silent_noop(self):
        """PostToolUseFailure is excluded; must be silently dropped."""
        payload = json.dumps({"hook_event_name": "PostToolUseFailure", "session_id": "s1"})
        stdout = _run_dispatch(payload)
        self.assertEqual("", stdout)

    def test_vscode_excluded_post_compact_is_silent_noop(self):
        """PostCompact is excluded; must be silently dropped."""
        payload = json.dumps({"hook_event_name": "PostCompact", "session_id": "s1"})
        stdout = _run_dispatch(payload)
        self.assertEqual("", stdout)


# ---------------------------------------------------------------------------
# dispatch() — malformed / non-object input
# ---------------------------------------------------------------------------

class TestDispatchMalformedInput(unittest.TestCase):
    """Non-JSON, empty string, and non-object JSON are all silent no-ops."""

    def test_malformed_json_does_not_raise(self):
        try:
            _run_dispatch("not json at all {{{")
        except Exception as e:
            self.fail(f"dispatch raised for malformed JSON: {e}")

    def test_malformed_json_emits_nothing_to_stdout(self):
        self.assertEqual("", _run_dispatch("not json at all {{{"))

    def test_empty_string_does_not_raise(self):
        try:
            _run_dispatch("")
        except Exception as e:
            self.fail(f"dispatch raised for empty input: {e}")

    def test_empty_string_emits_nothing_to_stdout(self):
        self.assertEqual("", _run_dispatch(""))

    def test_json_array_does_not_raise(self):
        try:
            _run_dispatch("[1, 2, 3]")
        except Exception as e:
            self.fail(f"dispatch raised for JSON array: {e}")

    def test_json_array_emits_nothing_to_stdout(self):
        self.assertEqual("", _run_dispatch("[1, 2, 3]"))

    def test_json_string_does_not_raise(self):
        try:
            _run_dispatch('"just a string"')
        except Exception as e:
            self.fail(f"dispatch raised for JSON string: {e}")

    def test_json_string_emits_nothing_to_stdout(self):
        self.assertEqual("", _run_dispatch('"just a string"'))

    def test_json_null_does_not_raise(self):
        try:
            _run_dispatch("null")
        except Exception as e:
            self.fail(f"dispatch raised for JSON null: {e}")

    def test_json_null_emits_nothing_to_stdout(self):
        self.assertEqual("", _run_dispatch("null"))

    def test_missing_hook_event_name_does_not_raise(self):
        """A payload dict with no hook_event_name is a silent no-op."""
        payload = json.dumps({"session_id": "s1"})
        try:
            _run_dispatch(payload)
        except Exception as e:
            self.fail(f"dispatch raised for missing event name: {e}")

    def test_missing_hook_event_name_emits_nothing(self):
        payload = json.dumps({"session_id": "s1"})
        self.assertEqual("", _run_dispatch(payload))


# ---------------------------------------------------------------------------
# dispatch() — handler exception swallowing
# ---------------------------------------------------------------------------

class TestDispatchHandlerException(unittest.TestCase):
    """A handler that raises must not propagate the exception; stdout stays empty."""

    def _patched_dispatch(self, event_name: str, exc: Exception) -> str:
        def failing_handler(ctx):
            raise exc

        payload = json.dumps({"hook_event_name": event_name, "session_id": "s1"})
        with unittest.mock.patch.dict(mosaic_logger.HANDLERS, {event_name: failing_handler}):
            return _run_dispatch(payload)

    def test_handler_runtime_error_does_not_propagate(self):
        try:
            self._patched_dispatch("TestCrashEvent", RuntimeError("boom"))
        except Exception as e:
            self.fail(f"dispatch propagated handler RuntimeError: {e}")

    def test_handler_runtime_error_emits_nothing_to_stdout(self):
        stdout = self._patched_dispatch("TestCrashEvent", RuntimeError("boom"))
        self.assertEqual("", stdout)

    def test_handler_value_error_does_not_propagate(self):
        try:
            self._patched_dispatch("TestCrashEvent2", ValueError("bad value"))
        except Exception as e:
            self.fail(f"dispatch propagated handler ValueError: {e}")

    def test_handler_os_error_does_not_propagate(self):
        try:
            self._patched_dispatch("TestCrashEvent3", OSError("disk full"))
        except Exception as e:
            self.fail(f"dispatch propagated handler OSError: {e}")


# ---------------------------------------------------------------------------
# dispatch() — never writes decision output to stdout
# ---------------------------------------------------------------------------

class TestDispatchNeverWritesDecisionOutput(unittest.TestCase):
    """dispatch() must never write any bytes to stdout.

    VS Code parses stdout as a hook-output control object on exit 0.
    Any stray output could alter the user's session.
    """

    def test_no_stdout_on_session_start(self):
        payload = json.dumps({"hook_event_name": "SessionStart", "session_id": "s1"})
        self.assertEqual("", _run_dispatch(payload))

    def test_no_stdout_on_pre_tool_use(self):
        payload = json.dumps({"hook_event_name": "PreToolUse", "session_id": "s1",
                               "tool_name": "Bash", "tool_input": {}})
        self.assertEqual("", _run_dispatch(payload))

    def test_no_stdout_on_stop(self):
        payload = json.dumps({"hook_event_name": "Stop", "session_id": "s1"})
        self.assertEqual("", _run_dispatch(payload))

    def test_no_stdout_on_all_eight_registered_events(self):
        """None of the 8 registered events may produce stdout."""
        registered_events = [
            "SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
            "PreCompact", "SubagentStart", "SubagentStop", "Stop",
        ]
        base_payload = {"session_id": "s1"}
        for event in registered_events:
            with self.subTest(event=event):
                payload = json.dumps({**base_payload, "hook_event_name": event})
                stdout = _run_dispatch(payload)
                self.assertEqual("", stdout,
                                 f"event '{event}' produced unexpected stdout output")


# ---------------------------------------------------------------------------
# HANDLERS registry — dispatcher (exactly 8 keys)
# ---------------------------------------------------------------------------

class TestMosaicLoggerHandlersRegistry(unittest.TestCase):
    """HANDLERS contains exactly the 8 documented VS Code events."""

    def test_handlers_is_a_dict(self):
        self.assertIsInstance(mosaic_logger.HANDLERS, dict)

    def test_handlers_has_exactly_eight_entries(self):
        self.assertEqual(
            8, len(mosaic_logger.HANDLERS),
            f"Expected 8 handler entries, got {len(mosaic_logger.HANDLERS)}: "
            f"{set(mosaic_logger.HANDLERS)}"
        )

    def test_session_start_registered(self):
        self.assertIn("SessionStart", mosaic_logger.HANDLERS)

    def test_user_prompt_submit_registered(self):
        self.assertIn("UserPromptSubmit", mosaic_logger.HANDLERS)

    def test_pre_tool_use_registered(self):
        self.assertIn("PreToolUse", mosaic_logger.HANDLERS)

    def test_post_tool_use_registered(self):
        self.assertIn("PostToolUse", mosaic_logger.HANDLERS)

    def test_pre_compact_registered(self):
        self.assertIn("PreCompact", mosaic_logger.HANDLERS)

    def test_subagent_start_registered(self):
        self.assertIn("SubagentStart", mosaic_logger.HANDLERS)

    def test_subagent_stop_registered(self):
        self.assertIn("SubagentStop", mosaic_logger.HANDLERS)

    def test_stop_registered(self):
        self.assertIn("Stop", mosaic_logger.HANDLERS)

    def test_session_end_absent(self):
        """SessionEnd is not confirmed for VS Code and must be absent."""
        self.assertNotIn("SessionEnd", mosaic_logger.HANDLERS)

    def test_notification_absent(self):
        """Notification is not confirmed for VS Code and must be absent."""
        self.assertNotIn("Notification", mosaic_logger.HANDLERS)

    def test_post_tool_use_failure_absent(self):
        """PostToolUseFailure is not confirmed for VS Code and must be absent."""
        self.assertNotIn("PostToolUseFailure", mosaic_logger.HANDLERS)

    def test_post_compact_absent(self):
        """PostCompact is not confirmed for VS Code and must be absent."""
        self.assertNotIn("PostCompact", mosaic_logger.HANDLERS)

    def test_all_handlers_callable(self):
        for event, handler in mosaic_logger.HANDLERS.items():
            with self.subTest(event=event):
                self.assertTrue(callable(handler))

    def test_unknown_event_not_in_registry(self):
        self.assertNotIn("NonExistentEventXYZ", mosaic_logger.HANDLERS)


# ---------------------------------------------------------------------------
# RUN_ID_PROMPT_FIELDS mapping
# ---------------------------------------------------------------------------

class TestRunIdPromptFields(unittest.TestCase):
    """RUN_ID_PROMPT_FIELDS is public and directly assertable.

    VS Code field name divergence: UserPromptSubmit uses 'prompt', not 'user_prompt'.
    SubagentStart is deliberately absent because it carries no prompt text;
    its run_id comes from the popped pending dispatch.
    """

    def test_run_id_prompt_fields_is_a_dict(self):
        self.assertIsInstance(mosaic_logger.RUN_ID_PROMPT_FIELDS, dict)

    def test_user_prompt_submit_maps_to_prompt(self):
        """VS Code uses 'prompt'; 'user_prompt' is the claude-code field name."""
        self.assertIn("UserPromptSubmit", mosaic_logger.RUN_ID_PROMPT_FIELDS)
        self.assertEqual(
            "prompt",
            mosaic_logger.RUN_ID_PROMPT_FIELDS["UserPromptSubmit"],
            "UserPromptSubmit must map to 'prompt', not 'user_prompt' (VS Code field name)"
        )

    def test_user_prompt_submit_does_not_map_to_user_prompt(self):
        """Explicitly assert the claude-code field name is NOT used here."""
        field = mosaic_logger.RUN_ID_PROMPT_FIELDS.get("UserPromptSubmit")
        self.assertNotEqual(
            "user_prompt", field,
            "'user_prompt' is the claude-code field; VS Code uses 'prompt'"
        )

    def test_subagent_stop_maps_to_last_assistant_message(self):
        self.assertIn("SubagentStop", mosaic_logger.RUN_ID_PROMPT_FIELDS)
        self.assertEqual(
            "last_assistant_message",
            mosaic_logger.RUN_ID_PROMPT_FIELDS["SubagentStop"]
        )

    def test_stop_maps_to_last_assistant_message(self):
        self.assertIn("Stop", mosaic_logger.RUN_ID_PROMPT_FIELDS)
        self.assertEqual(
            "last_assistant_message",
            mosaic_logger.RUN_ID_PROMPT_FIELDS["Stop"]
        )

    def test_subagent_start_is_absent(self):
        """SubagentStart carries no prompt text; run_id comes from the pending dispatch."""
        self.assertNotIn(
            "SubagentStart", mosaic_logger.RUN_ID_PROMPT_FIELDS,
            "SubagentStart must not be in RUN_ID_PROMPT_FIELDS — "
            "its run_id comes from the popped pending dispatch, not from the payload"
        )

    def test_session_start_is_absent(self):
        """SessionStart has no prompt-bearing field."""
        self.assertNotIn("SessionStart", mosaic_logger.RUN_ID_PROMPT_FIELDS)

    def test_pre_tool_use_is_absent(self):
        """PreToolUse has no prompt-bearing field for run_id extraction."""
        self.assertNotIn("PreToolUse", mosaic_logger.RUN_ID_PROMPT_FIELDS)


# ---------------------------------------------------------------------------
# build_context
# ---------------------------------------------------------------------------

class TestBuildContext(unittest.TestCase):
    """build_context constructs a HookContext from the payload dict.

    VS Code dispatcher: event name comes from payload['hook_event_name'],
    not from sys.argv[1] (that is the ghcp-cli pattern).
    """

    def test_build_context_returns_hook_context(self):
        import mosaic_logger_core as core
        payload = {"hook_event_name": "SessionStart", "session_id": "s1",
                   "cwd": str(pathlib.Path.cwd())}
        ctx = mosaic_logger.build_context(payload)
        self.assertIsInstance(ctx, core.HookContext)

    def test_build_context_sets_event_from_hook_event_name(self):
        """ctx.event must come from payload['hook_event_name']."""
        payload = {"hook_event_name": "PreToolUse", "session_id": "s1"}
        ctx = mosaic_logger.build_context(payload)
        self.assertEqual("PreToolUse", ctx.event)

    def test_build_context_run_id_initially_none(self):
        """run_id starts as None; it is set by resolve_run_identity later."""
        payload = {"hook_event_name": "SessionStart", "session_id": "s1"}
        ctx = mosaic_logger.build_context(payload)
        self.assertIsNone(ctx.run_id)

    def test_build_context_takes_no_separate_event_argument(self):
        """build_context accepts only the payload dict; no separate event string argument."""
        payload = {"hook_event_name": "Stop", "session_id": "s1"}
        try:
            mosaic_logger.build_context(payload)
        except TypeError as exc:
            self.fail(
                f"build_context raised TypeError — it should accept only the payload dict: {exc}"
            )


# ---------------------------------------------------------------------------
# Run lifecycle: emit_run_start / emit_run_end
# ---------------------------------------------------------------------------

class TestRunLifecycle(unittest.TestCase):
    """emit_run_start is called on SessionStart; emit_run_end is called on Stop.

    run_end must be emitted even when the Stop handler raises (D5, AC2.9).
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _dispatch_with_cwd(self, payload_dict: dict) -> str:
        """Dispatch with cwd set to tmp so events land on disk."""
        payload_dict.setdefault("cwd", str(self.tmp_path))
        stdout = _run_dispatch(json.dumps(payload_dict))
        return stdout

    def _read_orc_events(self, run_id: str) -> list:
        import mosaic_logger_core as core
        paths = core.build_paths(self.tmp_path)
        p = paths.orchestrator_events(run_id)
        if not p.exists():
            return []
        return [json.loads(ln) for ln in p.read_text("utf-8").splitlines() if ln.strip()]

    def test_run_start_emitted_on_session_start(self):
        """dispatch must call emit_run_start when event is SessionStart."""
        self._dispatch_with_cwd({
            "hook_event_name": "SessionStart",
            "session_id": "lifecycle-sess-001",
        })
        events = self._read_orc_events("unknown-run")
        run_starts = [e for e in events if e.get("event") == "run_start"]
        self.assertGreater(len(run_starts), 0,
                           "run_start event must be written when SessionStart fires")

    def test_run_end_emitted_on_stop(self):
        """dispatch must call emit_run_end when event is Stop."""
        self._dispatch_with_cwd({
            "hook_event_name": "Stop",
            "session_id": "lifecycle-sess-002",
        })
        events = self._read_orc_events("unknown-run")
        run_ends = [e for e in events if e.get("event") == "run_end"]
        self.assertGreater(len(run_ends), 0,
                           "run_end event must be written when Stop fires")

    def test_run_end_emitted_even_when_stop_handler_raises(self):
        """run_end must still be emitted when the Stop handler itself raises (AC2.9)."""
        def failing_stop_handler(ctx):
            raise RuntimeError("Stop handler failure")

        payload = json.dumps({
            "hook_event_name": "Stop",
            "session_id": "lifecycle-sess-003",
            "cwd": str(self.tmp_path),
        })
        with unittest.mock.patch.dict(mosaic_logger.HANDLERS, {"Stop": failing_stop_handler}):
            stdout = _run_dispatch(payload)

        self.assertEqual("", stdout, "No stdout even when Stop handler raises")
        events = self._read_orc_events("unknown-run")
        run_ends = [e for e in events if e.get("event") == "run_end"]
        self.assertGreater(
            len(run_ends), 0,
            "run_end must still be emitted even when the Stop handler raised (AC2.9)"
        )

    def test_run_start_not_emitted_for_non_session_start_events(self):
        """run_start must only be triggered on SessionStart, not on other events."""
        self._dispatch_with_cwd({
            "hook_event_name": "UserPromptSubmit",
            "session_id": "lifecycle-sess-004",
            "prompt": "hello",
        })
        events = self._read_orc_events("unknown-run")
        run_starts = [e for e in events if e.get("event") == "run_start"]
        self.assertEqual(0, len(run_starts),
                         "run_start must not be emitted for UserPromptSubmit")


# ---------------------------------------------------------------------------
# main() — exit code and stdout
# ---------------------------------------------------------------------------

class TestMainExitCode(unittest.TestCase):
    """main() always exits 0, always produces empty stdout."""

    def setUp(self):
        # Guard: these subprocess tests require the implementation to be present.
        # An empty HANDLERS dict means the dispatcher is not yet implemented.
        # This assertion causes the entire class to ERROR in TDD RED phase,
        # providing the required RED signal for subprocess-level tests.
        self.assertIsInstance(mosaic_logger.HANDLERS, dict)
        self.assertGreater(
            len(mosaic_logger.HANDLERS), 0,
            "HANDLERS is empty — dispatcher implementation is missing",
        )

    def _run_main(self, stdin_bytes: bytes,
                  extra_env: "dict | None" = None) -> subprocess.CompletedProcess:
        """Run the adapter as a subprocess, reading event from stdin payload."""
        env = {**os.environ, **(extra_env or {})}
        return subprocess.run(
            ["py", _ADAPTER_PATH],
            input=stdin_bytes,
            capture_output=True,
            env=env,
            timeout=15,
        )

    def test_main_is_callable(self):
        self.assertTrue(callable(mosaic_logger.main))

    def test_main_exits_0_for_unknown_event(self):
        result = self._run_main(
            json.dumps({"hook_event_name": "UnknownEventXYZ"}).encode()
        )
        self.assertEqual(0, result.returncode)

    def test_main_exits_0_for_malformed_stdin(self):
        result = self._run_main(b"not json {{{")
        self.assertEqual(0, result.returncode)

    def test_main_exits_0_for_empty_stdin(self):
        result = self._run_main(b"")
        self.assertEqual(0, result.returncode)

    def test_main_produces_no_stdout_for_unknown_event(self):
        result = self._run_main(
            json.dumps({"hook_event_name": "UnknownEventXYZ"}).encode()
        )
        self.assertEqual(b"", result.stdout)

    def test_main_produces_no_stdout_for_malformed_stdin(self):
        result = self._run_main(b"not json")
        self.assertEqual(b"", result.stdout)

    def test_main_produces_no_stdout_for_empty_stdin(self):
        result = self._run_main(b"")
        self.assertEqual(b"", result.stdout)

    def test_main_reads_event_from_payload_not_argv(self):
        """VS Code dispatcher reads hook_event_name from the JSON payload, not argv."""
        # Sending a SessionStart with a real cwd; no argv argument is passed.
        with tempfile.TemporaryDirectory() as tmp:
            payload = json.dumps({
                "hook_event_name": "SessionStart",
                "session_id": "test-main-sess-001",
                "cwd": tmp,
            }).encode()
            result = self._run_main(payload)
            self.assertEqual(0, result.returncode)
            self.assertEqual(b"", result.stdout)
            unknown_run_dir = pathlib.Path(tmp) / "OrchestrationLogs" / "unknown-run"
            self.assertTrue(
                unknown_run_dir.exists(),
                "unknown-run/ directory must be created when SessionStart fires via payload dispatch"
            )

    def test_main_exits_0_for_pre_tool_use_with_malformed_payload(self):
        """PreToolUse with invalid payload must exit 0."""
        result = self._run_main(b"garbage input")
        self.assertEqual(0, result.returncode)

    def test_main_produces_no_stdout_for_pre_tool_use(self):
        """VS Code parses stdout on exit 0; PreToolUse must write nothing."""
        result = self._run_main(
            json.dumps({
                "hook_event_name": "PreToolUse",
                "session_id": "s1",
                "tool_name": "Bash",
                "tool_input": {},
            }).encode()
        )
        self.assertEqual(b"", result.stdout)


if __name__ == "__main__":
    unittest.main()
