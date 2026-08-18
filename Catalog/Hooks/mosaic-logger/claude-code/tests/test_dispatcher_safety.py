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
import mosaic_logger_core as core
import mosaic_logger_runstate as runstate

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


# ---------------------------------------------------------------------------
# resolve_run_identity — never-raises contract with the binding store
# (Stage 2, Design A2/A4, AC2.7)
# ---------------------------------------------------------------------------

class TestResolveRunIdentityNeverRaisesWithBindingStore(unittest.TestCase):
    """resolve_run_identity and its binding-lookup path must never raise,
    even when the session-run binding store is corrupt, unreadable, or the
    session_id is malformed/absent."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def _ctx(self, event_name, session_id, **fields):
        payload = {"hook_event_name": event_name, "session_id": session_id}
        payload.update(fields)
        return core.HookContext(payload, self.tmp_path, self.paths,
                                 "2026-07-27T17:00:00.000Z")

    def test_never_raises_with_corrupt_binding_file(self):
        binding_path = self.paths.session_binding_entry("safety-sess-corrupt")
        binding_path.parent.mkdir(parents=True, exist_ok=True)
        binding_path.write_text("{not json\nnot json either", encoding="utf-8")
        ctx = self._ctx("PreToolUse", "safety-sess-corrupt",
                         tool_name="Bash", tool_input={})
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as exc:
            self.fail(f"resolve_run_identity raised with corrupt binding file: {exc}")

    def test_never_raises_with_binding_dir_blocked_by_a_file(self):
        """The .session-runs/ directory itself is a plain file, not a directory --
        an extreme corruption scenario that must still degrade cleanly."""
        binding_dir = self.paths.session_binding_dir()
        binding_dir.parent.mkdir(parents=True, exist_ok=True)
        binding_dir.write_bytes(b"not a directory")
        ctx = self._ctx("SessionStart", "safety-sess-blocked-dir")
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as exc:
            self.fail(f"resolve_run_identity raised with a blocked binding dir: {exc}")

    def test_never_raises_with_missing_session_id(self):
        ctx = self._ctx("PreToolUse", None, tool_name="Bash", tool_input={})
        ctx.session_id = None
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as exc:
            self.fail(f"resolve_run_identity raised with session_id=None: {exc}")

    def test_never_raises_for_malformed_unexpected_payload_shapes(self):
        """Malformed/unexpected payload field shapes (e.g. a dict where a
        string is expected) must not make resolve_run_identity raise."""
        payload = {
            "hook_event_name": "SubagentStart",
            "session_id": "safety-sess-malformed",
            "agent_prompt": {"this": "is a dict, not a string"},
        }
        ctx = core.HookContext(payload, self.tmp_path, self.paths,
                                "2026-07-27T17:00:00.000Z")
        try:
            mosaic_logger.resolve_run_identity(ctx)
        except Exception as exc:
            self.fail(f"resolve_run_identity raised for a malformed payload shape: {exc}")


# ---------------------------------------------------------------------------
# Dispatch ordering unaffected by binding-first resolution (Stage 2, AC2.7)
# ---------------------------------------------------------------------------

class TestDispatchOrderingUnaffectedByBinding(unittest.TestCase):
    """run_start still fires on SessionStart and run_end still fires on
    SessionEnd even when a session-run binding already exists for that
    session (Design A4: dispatch() step ordering is unchanged)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _dispatch(self, payload):
        orig = os.environ.get("CLAUDE_PROJECT_DIR")
        os.environ["CLAUDE_PROJECT_DIR"] = str(self.tmp_path)
        try:
            mosaic_logger.dispatch(json.dumps(payload))
        finally:
            if orig is None:
                os.environ.pop("CLAUDE_PROJECT_DIR", None)
            else:
                os.environ["CLAUDE_PROJECT_DIR"] = orig

    def _events(self, path):
        if not path.exists():
            return []
        return [json.loads(l) for l in path.read_text("utf-8").splitlines() if l.strip()]

    def test_run_start_still_emitted_when_binding_pre_exists(self):
        session_id = "ordering-sess-run-start"
        paths = core.build_paths(self.tmp_path)
        run_id = "20260727T160000Z-cafe"
        runstate.put_session_run_binding(paths, session_id, run_id, "2026-07-27T16:00:00.000Z")

        self._dispatch({
            "hook_event_name": "SessionStart",
            "session_id": session_id,
            "cwd": str(self.tmp_path),
        })

        events = self._events(paths.orchestrator_events(run_id))
        self.assertIn("run_start", [e["event"] for e in events],
                      "run_start must still be emitted on SessionStart even when "
                      "a binding already resolves ctx.run_id")

    def test_run_end_still_emitted_when_binding_pre_exists(self):
        session_id = "ordering-sess-run-end"
        paths = core.build_paths(self.tmp_path)
        run_id = "20260727T160000Z-babe"
        runstate.put_session_run_binding(paths, session_id, run_id, "2026-07-27T16:00:00.000Z")

        self._dispatch({
            "hook_event_name": "SessionStart",
            "session_id": session_id,
            "cwd": str(self.tmp_path),
        })
        self._dispatch({
            "hook_event_name": "SessionEnd",
            "session_id": session_id,
            "reason": "clear",
        })

        events = self._events(paths.orchestrator_events(run_id))
        self.assertIn("run_end", [e["event"] for e in events],
                      "run_end must still be emitted on SessionEnd even when "
                      "a binding already resolves ctx.run_id")
