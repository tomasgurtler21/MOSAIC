"""Tests for the always-exit-0 safety boundary in mosaic_logger.

Covers: dispatch() swallows all exceptions and emits nothing to stdout for unknown
event names, malformed stdin, non-object JSON, missing hook_event_name, and handler
exceptions. main() exits 0 and produces empty stdout for all failure modes.
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
    """Call dispatch() capturing any stdout it produces. Returns captured text."""
    buf = io.StringIO()
    with contextlib.redirect_stdout(buf):
        mosaic_logger.dispatch(raw_input)
    return buf.getvalue()


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

    def test_missing_hook_event_name_does_not_raise(self):
        payload = json.dumps({"session_id": "s1"})
        try:
            _run_dispatch(payload)
        except Exception as e:
            self.fail(f"dispatch raised for missing event name: {e}")

    def test_missing_hook_event_name_emits_nothing(self):
        payload = json.dumps({"session_id": "s1"})
        self.assertEqual("", _run_dispatch(payload))


class TestDispatchHandlerException(unittest.TestCase):
    """A handler that raises must not propagate the exception; stdout must stay empty."""

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


class TestDispatchNeverWritesDecisionOutput(unittest.TestCase):
    """dispatch() must never write permissionDecision, continue, or any JSON to stdout."""

    def test_no_permission_decision_on_any_event(self):
        payload = json.dumps({"hook_event_name": "SessionStart", "session_id": "s1"})
        stdout = _run_dispatch(payload)
        # Nothing at all is the correct and complete output for a logging hook
        self.assertEqual("", stdout)

    def test_no_continue_field_on_any_event(self):
        payload = json.dumps({"hook_event_name": "PreToolUse", "session_id": "s1",
                              "tool_name": "Bash", "tool_input": {}})
        stdout = _run_dispatch(payload)
        self.assertEqual("", stdout)


class TestMosaicLoggerHandlersRegistry(unittest.TestCase):
    """HANDLERS is a dict mapping event names to callables; unknown names not present."""

    def test_handlers_is_a_dict(self):
        self.assertIsInstance(mosaic_logger.HANDLERS, dict)

    def test_unknown_event_not_in_registry(self):
        self.assertNotIn("NonExistentEventXYZ", mosaic_logger.HANDLERS)



class TestMainExitCode(unittest.TestCase):
    """main() always exits 0, always produces empty stdout, and processes events."""

    def setUp(self):
        # Guard: these subprocess tests are only meaningful once the implementation
        # ships a populated HANDLERS registry. Without it, main() is an empty stub
        # that exits 0 and emits nothing for any input — exactly what the exit-0
        # and no-stdout assertions check — so they pass trivially against the stub.
        # Asserting HANDLERS exists and is non-empty causes every test in this class
        # to ERROR in TDD RED phase, providing the required RED signal.
        self.assertIsInstance(mosaic_logger.HANDLERS, dict)
        self.assertGreater(
            len(mosaic_logger.HANDLERS), 0,
            "HANDLERS is empty — dispatcher implementation is missing",
        )

    def _run_main(
        self,
        stdin_bytes: bytes,
        extra_env: "dict | None" = None,
    ) -> subprocess.CompletedProcess:
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

    def test_main_processes_session_start_and_creates_log_directory(self):
        """Verifies the dispatcher actually runs (not just exits): a SessionStart
        with a known workspace dir must produce an unknown-run/ log directory on disk."""
        with tempfile.TemporaryDirectory() as tmp:
            payload = json.dumps({
                "hook_event_name": "SessionStart",
                "session_id": "test-main-sess-001",
                "cwd": tmp,
            }).encode()
            result = self._run_main(payload, extra_env={"CLAUDE_PROJECT_DIR": tmp})
            # Safety: exit 0 always
            self.assertEqual(0, result.returncode)
            self.assertEqual(b"", result.stdout)
            # Behavioral: unknown-run/ directory must have been created since no
            # run_id can be extracted from a SessionStart event's (absent) prompt field
            unknown_run_dir = pathlib.Path(tmp) / "OrchestrationLogs" / "unknown-run"
            self.assertTrue(unknown_run_dir.exists(),
                            "unknown-run/ directory not created — dispatcher did not process the event")
