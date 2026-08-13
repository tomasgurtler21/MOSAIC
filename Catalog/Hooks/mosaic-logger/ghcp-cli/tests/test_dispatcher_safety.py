"""Tests for the always-exit-0 safety boundary in mosaic_logger (ghcp-cli variant).

Key differences from the claude-code dispatcher:
- dispatch(event, raw_input) takes the event name as a separate first argument
  (from sys.argv[1]), not from the JSON payload
- GHCP CLI payloads use camelCase fields; no 'hook_event_name' key in payload
- main() reads event from sys.argv[1] and JSON payload from stdin
- HANDLERS dict is keyed by GHCP CLI camelCase event names

Covers: dispatch() swallows all exceptions and emits nothing to stdout for
unknown event names, malformed stdin, non-object JSON, and handler exceptions.
main() exits 0 and produces empty stdout for all failure modes. HANDLERS
registry is a dict mapping camelCase event names to callables.
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


def _run_dispatch(event: str, raw_input: str) -> str:
    """Call dispatch(event, raw_input) capturing any stdout it produces."""
    buf = io.StringIO()
    with contextlib.redirect_stdout(buf):
        mosaic_logger.dispatch(event, raw_input)
    return buf.getvalue()


class TestDispatchUnknownEvent(unittest.TestCase):
    """An unregistered camelCase event name is a silent no-op."""

    def test_unknown_event_does_not_raise(self):
        payload = json.dumps({"sessionId": "s1"})
        try:
            _run_dispatch("NonExistentEvent", payload)
        except Exception as e:
            self.fail(f"dispatch raised for unknown event: {e}")

    def test_unknown_event_emits_nothing_to_stdout(self):
        payload = json.dumps({"sessionId": "s1"})
        self.assertEqual("", _run_dispatch("NonExistentEvent", payload))


class TestDispatchMalformedInput(unittest.TestCase):
    """Non-JSON, empty string, and non-object JSON are all silent no-ops."""

    def test_malformed_json_does_not_raise(self):
        try:
            _run_dispatch("sessionStart", "not json at all {{{")
        except Exception as e:
            self.fail(f"dispatch raised for malformed JSON: {e}")

    def test_malformed_json_emits_nothing_to_stdout(self):
        self.assertEqual("", _run_dispatch("sessionStart", "not json at all {{{"))

    def test_empty_string_does_not_raise(self):
        try:
            _run_dispatch("sessionStart", "")
        except Exception as e:
            self.fail(f"dispatch raised for empty input: {e}")

    def test_empty_string_emits_nothing_to_stdout(self):
        self.assertEqual("", _run_dispatch("sessionStart", ""))

    def test_json_array_does_not_raise(self):
        try:
            _run_dispatch("sessionStart", "[1, 2, 3]")
        except Exception as e:
            self.fail(f"dispatch raised for JSON array: {e}")

    def test_json_array_emits_nothing_to_stdout(self):
        self.assertEqual("", _run_dispatch("sessionStart", "[1, 2, 3]"))

    def test_json_string_does_not_raise(self):
        try:
            _run_dispatch("sessionStart", '"just a string"')
        except Exception as e:
            self.fail(f"dispatch raised for JSON string: {e}")

    def test_json_string_emits_nothing_to_stdout(self):
        self.assertEqual("", _run_dispatch("sessionStart", '"just a string"'))

    def test_json_null_does_not_raise(self):
        try:
            _run_dispatch("sessionStart", "null")
        except Exception as e:
            self.fail(f"dispatch raised for JSON null: {e}")

    def test_empty_event_name_does_not_raise(self):
        """An empty event name string is a valid (if unregistered) event."""
        payload = json.dumps({"sessionId": "s1"})
        try:
            _run_dispatch("", payload)
        except Exception as e:
            self.fail(f"dispatch raised for empty event name: {e}")

    def test_empty_event_name_emits_nothing(self):
        payload = json.dumps({"sessionId": "s1"})
        self.assertEqual("", _run_dispatch("", payload))


class TestDispatchHandlerException(unittest.TestCase):
    """A handler that raises must not propagate the exception; stdout must stay empty."""

    def _patched_dispatch(self, event_name: str, exc: Exception) -> str:
        def failing_handler(ctx):
            raise exc

        payload = json.dumps({"sessionId": "s1"})
        with unittest.mock.patch.dict(mosaic_logger.HANDLERS, {event_name: failing_handler}):
            return _run_dispatch(event_name, payload)

    def test_handler_runtime_error_does_not_propagate(self):
        try:
            self._patched_dispatch("testCrashEvent", RuntimeError("boom"))
        except Exception as e:
            self.fail(f"dispatch propagated handler RuntimeError: {e}")

    def test_handler_runtime_error_emits_nothing_to_stdout(self):
        stdout = self._patched_dispatch("testCrashEvent", RuntimeError("boom"))
        self.assertEqual("", stdout)

    def test_handler_value_error_does_not_propagate(self):
        try:
            self._patched_dispatch("testCrashEvent2", ValueError("bad value"))
        except Exception as e:
            self.fail(f"dispatch propagated handler ValueError: {e}")

    def test_handler_os_error_does_not_propagate(self):
        try:
            self._patched_dispatch("testCrashEvent3", OSError("disk full"))
        except Exception as e:
            self.fail(f"dispatch propagated handler OSError: {e}")


class TestDispatchNeverWritesDecisionOutput(unittest.TestCase):
    """dispatch() must NEVER write permissionDecision, behavior, or any JSON to stdout.

    This is a critical safety contract for GHCP CLI:
    - preToolUse handlers: stdout JSON could inadvertently allow/deny a tool call
    - permissionRequest handlers: stdout JSON is parsed as {behavior: allow|deny}

    Any stdout output from this adapter is harmful and must be prevented.
    """

    def test_no_permission_decision_on_session_start(self):
        payload = json.dumps({"sessionId": "s1"})
        stdout = _run_dispatch("sessionStart", payload)
        self.assertEqual("", stdout)

    def test_no_decision_output_on_pre_tool_use(self):
        """preToolUse: non-zero exit denies the tool call; stdout is also parsed.
        The adapter must produce absolutely no stdout on this event."""
        payload = json.dumps({"sessionId": "s1", "toolName": "bash",
                               "toolArgs": {}, "toolUseId": "tu-001"})
        stdout = _run_dispatch("preToolUse", payload)
        self.assertEqual("", stdout)

    def test_no_decision_output_on_permission_request(self):
        """permissionRequest: GHCP CLI parses stdout as {behavior: allow|deny}.
        The adapter MUST write nothing to stdout."""
        payload = json.dumps({"sessionId": "s1", "toolName": "bash"})
        stdout = _run_dispatch("permissionRequest", payload)
        self.assertEqual("", stdout)

    def test_no_stdout_output_on_any_known_event(self):
        """No GHCP CLI event name should produce any stdout from this adapter."""
        known_events = [
            "sessionStart", "sessionEnd", "userPromptSubmitted",
            "userPromptTransformed", "preToolUse", "postToolUse",
            "postToolUseFailure", "preCompact", "agentStop",
            "subagentStart", "subagentStop", "permissionRequest",
            "errorOccurred", "notification",
        ]
        payload = json.dumps({"sessionId": "s1"})
        for event in known_events:
            with self.subTest(event=event):
                stdout = _run_dispatch(event, payload)
                self.assertEqual("", stdout,
                                 f"event '{event}' produced unexpected stdout output")


class TestHandlersRegistry(unittest.TestCase):
    """HANDLERS is a dict mapping camelCase event names to callables."""

    def test_handlers_is_a_dict(self):
        self.assertIsInstance(mosaic_logger.HANDLERS, dict)

    def test_unknown_event_not_in_registry(self):
        self.assertNotIn("NonExistentEventXYZ", mosaic_logger.HANDLERS)

    def test_handler_keys_are_camelcase_not_pascalcase(self):
        """GHCP CLI uses camelCase event names (sessionStart, not SessionStart)."""
        for key in mosaic_logger.HANDLERS:
            # PascalCase keys (e.g., SessionStart) are claude-code style -- not allowed
            self.assertFalse(
                key and key[0].isupper() and len(key) > 1,
                f"Handler key '{key}' looks like PascalCase (claude-code style); "
                "GHCP CLI uses camelCase (e.g., 'sessionStart')"
            )


class TestRunIdPromptFieldsMapping(unittest.TestCase):
    """_RUN_ID_PROMPT_FIELDS maps GHCP CLI event names to prompt-bearing payload fields."""

    def test_run_id_prompt_fields_is_a_dict(self):
        self.assertIsInstance(mosaic_logger._RUN_ID_PROMPT_FIELDS, dict)

    def test_subagent_start_maps_to_agent_prompt(self):
        """subagentStart carries run_id in the 'agentPrompt' field."""
        self.assertIn("subagentStart", mosaic_logger._RUN_ID_PROMPT_FIELDS)
        self.assertEqual("agentPrompt", mosaic_logger._RUN_ID_PROMPT_FIELDS["subagentStart"])

    def test_subagent_stop_maps_to_response(self):
        """subagentStop carries run_id in the 'response' field."""
        self.assertIn("subagentStop", mosaic_logger._RUN_ID_PROMPT_FIELDS)
        self.assertEqual("response", mosaic_logger._RUN_ID_PROMPT_FIELDS["subagentStop"])

    def test_user_prompt_submitted_maps_to_prompt(self):
        """userPromptSubmitted carries run_id in the 'prompt' field."""
        self.assertIn("userPromptSubmitted", mosaic_logger._RUN_ID_PROMPT_FIELDS)
        self.assertEqual("prompt", mosaic_logger._RUN_ID_PROMPT_FIELDS["userPromptSubmitted"])

    def test_notification_maps_to_message(self):
        """notification carries run_id in the 'message' field."""
        self.assertIn("notification", mosaic_logger._RUN_ID_PROMPT_FIELDS)
        self.assertEqual("message", mosaic_logger._RUN_ID_PROMPT_FIELDS["notification"])

    def test_agent_stop_is_not_in_mapping(self):
        """agentStop is intentionally absent: GHCP CLI's agentStop payload does not
        carry a last_assistant_message or equivalent text field for run_id extraction."""
        self.assertNotIn("agentStop", mosaic_logger._RUN_ID_PROMPT_FIELDS)

    def test_session_start_is_not_in_mapping(self):
        """sessionStart has no prompt-bearing field."""
        self.assertNotIn("sessionStart", mosaic_logger._RUN_ID_PROMPT_FIELDS)

    def test_session_end_is_not_in_mapping(self):
        """sessionEnd has no prompt-bearing field."""
        self.assertNotIn("sessionEnd", mosaic_logger._RUN_ID_PROMPT_FIELDS)

    def test_pre_tool_use_is_not_in_mapping(self):
        """preToolUse has no prompt-bearing field for run_id extraction."""
        self.assertNotIn("preToolUse", mosaic_logger._RUN_ID_PROMPT_FIELDS)


class TestBuildContext(unittest.TestCase):
    """build_context constructs a HookContext from event name and camelCase payload."""

    def test_build_context_returns_hook_context(self):
        """build_context must return a HookContext instance."""
        import mosaic_logger_core as core
        payload = {"sessionId": "s1", "cwd": "/tmp"}
        ctx = mosaic_logger.build_context("sessionStart", payload)
        self.assertIsInstance(ctx, core.HookContext)

    def test_build_context_sets_event_from_first_argument(self):
        """The event attribute must come from the first argument, not from payload."""
        payload = {"sessionId": "s1"}
        ctx = mosaic_logger.build_context("preToolUse", payload)
        self.assertEqual("preToolUse", ctx.event)

    def test_build_context_maps_session_id_from_camelcase(self):
        """sessionId (camelCase) in payload maps to ctx.session_id."""
        payload = {"sessionId": "ghcp-sess-abc"}
        ctx = mosaic_logger.build_context("sessionStart", payload)
        self.assertEqual("ghcp-sess-abc", ctx.session_id)

    def test_build_context_maps_agent_id_from_camelcase(self):
        """agentId (camelCase) in payload maps to ctx.agent_id."""
        payload = {"sessionId": "s1", "agentId": "harness-001"}
        ctx = mosaic_logger.build_context("subagentStart", payload)
        self.assertEqual("harness-001", ctx.agent_id)

    def test_build_context_run_id_initially_none(self):
        """run_id starts as None; it is set by resolve_run_identity later."""
        payload = {"sessionId": "s1"}
        ctx = mosaic_logger.build_context("sessionStart", payload)
        self.assertIsNone(ctx.run_id)


class TestMainExitCode(unittest.TestCase):
    """main() always exits 0, always produces empty stdout."""

    def setUp(self):
        # Guard: these subprocess tests are only meaningful once the implementation
        # ships a populated HANDLERS registry. Without it, main() is an empty stub
        # that exits 0 and emits nothing for any input — so the exit-0 and no-stdout
        # assertions would pass trivially. Asserting HANDLERS is non-empty ensures
        # these tests ERROR in TDD RED phase, providing the required RED signal.
        self.assertIsInstance(mosaic_logger.HANDLERS, dict)
        self.assertGreater(
            len(mosaic_logger.HANDLERS), 0,
            "HANDLERS is empty — dispatcher implementation is missing",
        )

    def _run_main(self, event: str, stdin_bytes: bytes,
                  extra_env: "dict | None" = None) -> subprocess.CompletedProcess:
        """Run the adapter as a subprocess with event as sys.argv[1]."""
        env = {**os.environ, **(extra_env or {})}
        return subprocess.run(
            ["py", _ADAPTER_PATH, event],
            input=stdin_bytes,
            capture_output=True,
            env=env,
            timeout=15,
        )

    def test_main_is_callable(self):
        self.assertTrue(callable(mosaic_logger.main))

    def test_main_exits_0_for_unknown_event(self):
        result = self._run_main(
            "UnknownEventXYZ",
            json.dumps({"sessionId": "s1"}).encode(),
        )
        self.assertEqual(0, result.returncode)

    def test_main_exits_0_for_malformed_stdin(self):
        result = self._run_main("sessionStart", b"not json {{{")
        self.assertEqual(0, result.returncode)

    def test_main_exits_0_for_empty_stdin(self):
        result = self._run_main("sessionStart", b"")
        self.assertEqual(0, result.returncode)

    def test_main_produces_no_stdout_for_unknown_event(self):
        result = self._run_main(
            "UnknownEventXYZ",
            json.dumps({"sessionId": "s1"}).encode(),
        )
        self.assertEqual(b"", result.stdout)

    def test_main_produces_no_stdout_for_malformed_stdin(self):
        result = self._run_main("sessionStart", b"not json")
        self.assertEqual(b"", result.stdout)

    def test_main_exits_0_for_pre_tool_use_with_malformed_payload(self):
        """preToolUse with invalid payload must exit 0 (not 2 or non-zero)."""
        result = self._run_main("preToolUse", b"garbage input")
        self.assertEqual(0, result.returncode)

    def test_main_exits_0_for_permission_request(self):
        """permissionRequest must always exit 0; never exit 2 (deny behavior)."""
        payload = json.dumps({"sessionId": "s1", "toolName": "bash"}).encode()
        result = self._run_main("permissionRequest", payload)
        self.assertEqual(0, result.returncode)
        self.assertEqual(b"", result.stdout)

    def test_main_processes_session_start_and_creates_log_directory(self):
        """Verifies the dispatcher actually runs: a sessionStart with a known
        workspace dir must produce a log directory on disk."""
        with tempfile.TemporaryDirectory() as tmp:
            payload = json.dumps({
                "sessionId": "test-main-sess-001",
                "cwd": tmp,
            }).encode()
            result = self._run_main(
                "sessionStart", payload,
                extra_env={"MOSAIC_LOGGER_DEBUG": ""},
            )
            self.assertEqual(0, result.returncode)
            self.assertEqual(b"", result.stdout)
            # sessionStart has no prompt-bearing field so run_id stays None ->
            # effective_run_id returns 'unknown-run'
            unknown_run_dir = pathlib.Path(tmp) / "OrchestrationLogs" / "unknown-run"
            self.assertTrue(
                unknown_run_dir.exists(),
                "unknown-run/ directory not created — dispatcher did not process the event"
            )


if __name__ == "__main__":
    unittest.main()
