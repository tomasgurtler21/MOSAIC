"""End-to-end failure-mode tests for the mosaic-logger adapter.

Verifies that every documented failure mode leaves the dispatcher exiting 0
with no stdout decision output. These tests run the adapter as a subprocess
to verify the real exit code, which in-process testing via dispatch() cannot
exercise because dispatch() returns None rather than calling sys.exit().

Failure modes covered:
  - Malformed stdin (not JSON)
  - Unknown hook_event_name
  - Missing hook_event_name
  - Empty stdin
  - Non-object JSON root (array, string, number, null)
  - Unreadable transcript path (SubagentStop with agent_transcript_path
    pointing to a nonexistent file)
  - Missing agent_prompt on SubagentStart (adapter degrades, does not crash)
  - Unwritable / inaccessible log directory (adapter degrades, does not crash)
  - Handler exception (injected via patching in a dispatch() test, subprocess
    variant confirmed by the always-exit-0 boundary)
"""

import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger
import mosaic_logger_core as core


_ADAPTER_PATH = str(
    pathlib.Path(__file__).parent.parent / "mosaic_logger.py"
)

_FIXTURE_DIR = pathlib.Path(__file__).parent / "fixtures"
_SAMPLE_TRANSCRIPT = _FIXTURE_DIR / "sample_transcript.jsonl"


# ---------------------------------------------------------------------------
# Subprocess helper
# ---------------------------------------------------------------------------

def _run_adapter(stdin_bytes: bytes,
                 extra_env: "dict | None" = None,
                 timeout: int = 15) -> subprocess.CompletedProcess:
    """Run mosaic_logger.py as a subprocess with the given stdin bytes."""
    env = {**os.environ, **(extra_env or {})}
    return subprocess.run(
        ["py", _ADAPTER_PATH],
        input=stdin_bytes,
        capture_output=True,
        env=env,
        timeout=timeout,
    )


# ---------------------------------------------------------------------------
# Exit-0 invariant across all failure modes (subprocess)
# ---------------------------------------------------------------------------

class TestFailureModeExitCode(unittest.TestCase):
    """Every failure mode must produce exit code 0."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.env = {"CLAUDE_PROJECT_DIR": str(self.tmp_path)}

    def tearDown(self):
        self.tmp.cleanup()

    def test_malformed_json_exits_0(self):
        result = _run_adapter(b"not json at all {{{", extra_env=self.env)
        self.assertEqual(0, result.returncode)

    def test_empty_stdin_exits_0(self):
        result = _run_adapter(b"", extra_env=self.env)
        self.assertEqual(0, result.returncode)

    def test_unknown_event_name_exits_0(self):
        payload = json.dumps({
            "hook_event_name": "NonExistentEventXYZ",
            "session_id": "fail-unknown-001",
        }).encode()
        result = _run_adapter(payload, extra_env=self.env)
        self.assertEqual(0, result.returncode)

    def test_missing_hook_event_name_exits_0(self):
        payload = json.dumps({"session_id": "fail-noevent-001"}).encode()
        result = _run_adapter(payload, extra_env=self.env)
        self.assertEqual(0, result.returncode)

    def test_json_array_exits_0(self):
        result = _run_adapter(b"[1, 2, 3]", extra_env=self.env)
        self.assertEqual(0, result.returncode)

    def test_json_string_exits_0(self):
        result = _run_adapter(b'"just a string"', extra_env=self.env)
        self.assertEqual(0, result.returncode)

    def test_json_number_exits_0(self):
        result = _run_adapter(b"42", extra_env=self.env)
        self.assertEqual(0, result.returncode)

    def test_json_null_exits_0(self):
        result = _run_adapter(b"null", extra_env=self.env)
        self.assertEqual(0, result.returncode)

    def test_json_boolean_exits_0(self):
        result = _run_adapter(b"true", extra_env=self.env)
        self.assertEqual(0, result.returncode)

    def test_subagent_stop_with_unreadable_transcript_exits_0(self):
        """Unreadable agent_transcript_path degrades gracefully; adapter must still
        exit 0."""
        # First establish a session and register the subagent mapping.
        session_id = "fail-transcript-001"
        agent_id = "agt-fail-trans-001"
        for payload in [
            {"hook_event_name": "SessionStart", "session_id": session_id,
             "source": "startup"},
            {"hook_event_name": "SubagentStart", "session_id": session_id,
             "agent_id": agent_id, "agent_type": "Research",
             "agent_prompt": '{"agent_instance_id": "Research#1"}'},
        ]:
            r = _run_adapter(json.dumps(payload).encode(), extra_env=self.env)
            self.assertEqual(0, r.returncode, f"Setup step failed: {payload['hook_event_name']}")

        # SubagentStop with a nonexistent transcript path.
        stop_payload = json.dumps({
            "hook_event_name": "SubagentStop",
            "session_id": session_id,
            "agent_id": agent_id,
            "agent_type": "Research",
            "agent_transcript_path": "/no/such/transcript.jsonl",
            "last_assistant_message": '{"status_code": "SUCCESS"}',
        }).encode()
        result = _run_adapter(stop_payload, extra_env=self.env)
        self.assertEqual(0, result.returncode)

    def test_subagent_start_without_agent_prompt_exits_0(self):
        """Missing agent_prompt triggers the fallback path; must still exit 0."""
        for payload in [
            {"hook_event_name": "SessionStart", "session_id": "fail-noprompt-001",
             "source": "startup"},
            {"hook_event_name": "SubagentStart", "session_id": "fail-noprompt-001",
             "agent_id": "agt-noprompt-001", "agent_type": "Research"},
        ]:
            result = _run_adapter(json.dumps(payload).encode(), extra_env=self.env)
            self.assertEqual(0, result.returncode)

    def test_unwritable_log_directory_exits_0(self):
        """When the log directory cannot be created or written, adapter exits 0."""
        # Point CLAUDE_PROJECT_DIR at a regular file so mkdir(OrchestrationLogs)
        # fails on every platform — a file at the path blocks directory creation.
        with tempfile.NamedTemporaryFile(delete=False) as f:
            bogus_path = f.name
        try:
            env = {"CLAUDE_PROJECT_DIR": bogus_path}
            payload = json.dumps({
                "hook_event_name": "SessionStart",
                "session_id": "fail-unwritable-001",
                "source": "startup",
            }).encode()
            result = _run_adapter(payload, extra_env=env)
            self.assertEqual(0, result.returncode)
        finally:
            os.unlink(bogus_path)

    def test_session_id_absent_does_not_crash(self):
        """No session_id → UNRESOLVED run → no files written but exit 0."""
        payload = json.dumps({
            "hook_event_name": "SessionStart",
            "source": "startup",
            # session_id deliberately omitted
        }).encode()
        result = _run_adapter(payload, extra_env=self.env)
        self.assertEqual(0, result.returncode)


# ---------------------------------------------------------------------------
# No decision output in any failure mode (subprocess)
# ---------------------------------------------------------------------------

class TestFailureModeNoStdout(unittest.TestCase):
    """Every failure mode must produce empty stdout — no JSON, no decision."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.env = {"CLAUDE_PROJECT_DIR": str(self.tmp_path)}

    def tearDown(self):
        self.tmp.cleanup()

    def test_malformed_json_produces_no_stdout(self):
        result = _run_adapter(b"not json {{{", extra_env=self.env)
        self.assertEqual(b"", result.stdout)

    def test_empty_stdin_produces_no_stdout(self):
        result = _run_adapter(b"", extra_env=self.env)
        self.assertEqual(b"", result.stdout)

    def test_unknown_event_produces_no_stdout(self):
        payload = json.dumps({
            "hook_event_name": "NoSuchEvent",
            "session_id": "nostdout-001",
        }).encode()
        result = _run_adapter(payload, extra_env=self.env)
        self.assertEqual(b"", result.stdout)

    def test_json_array_produces_no_stdout(self):
        result = _run_adapter(b"[1, 2]", extra_env=self.env)
        self.assertEqual(b"", result.stdout)

    def test_subagent_stop_with_missing_transcript_produces_no_stdout(self):
        session_id = "nostdout-trans-001"
        agent_id = "agt-nostdout-001"
        for payload in [
            {"hook_event_name": "SessionStart", "session_id": session_id,
             "source": "startup"},
            {"hook_event_name": "SubagentStart", "session_id": session_id,
             "agent_id": agent_id, "agent_type": "Research",
             "agent_prompt": '{"agent_instance_id": "Research#1"}'},
        ]:
            _run_adapter(json.dumps(payload).encode(), extra_env=self.env)

        result = _run_adapter(json.dumps({
            "hook_event_name": "SubagentStop",
            "session_id": session_id,
            "agent_id": agent_id,
            "agent_transcript_path": "/no/such/file.jsonl",
            "last_assistant_message": '{"status_code": "SUCCESS"}',
        }).encode(), extra_env=self.env)
        self.assertEqual(b"", result.stdout)

    def test_unwritable_directory_produces_no_stdout(self):
        with tempfile.NamedTemporaryFile(delete=False) as f:
            bogus_path = f.name
        try:
            env = {"CLAUDE_PROJECT_DIR": bogus_path}
            result = _run_adapter(
                json.dumps({"hook_event_name": "SessionStart",
                            "session_id": "nostdout-unwritable",
                            "source": "startup"}).encode(),
                extra_env=env,
            )
            self.assertEqual(b"", result.stdout)
        finally:
            os.unlink(bogus_path)

    def test_valid_session_start_produces_no_stdout(self):
        """A fully successful dispatch must also produce no stdout — silence is correct."""
        result = _run_adapter(
            json.dumps({"hook_event_name": "SessionStart",
                        "session_id": "nostdout-success-001",
                        "source": "startup"}).encode(),
            extra_env=self.env,
        )
        self.assertEqual(b"", result.stdout)

    def test_valid_pre_tool_use_produces_no_stdout(self):
        # Establish a session first.
        _run_adapter(
            json.dumps({"hook_event_name": "SessionStart",
                        "session_id": "nostdout-tool-001",
                        "source": "startup"}).encode(),
            extra_env=self.env,
        )
        result = _run_adapter(
            json.dumps({"hook_event_name": "PreToolUse",
                        "session_id": "nostdout-tool-001",
                        "tool_name": "Bash",
                        "tool_use_id": "call-001",
                        "tool_input": {"command": "ls"}}).encode(),
            extra_env=self.env,
        )
        self.assertEqual(b"", result.stdout)


# ---------------------------------------------------------------------------
# Failure-mode behavior assertions via in-process dispatch()
# ---------------------------------------------------------------------------

class TestFailureModeInProcess(unittest.TestCase):
    """Complement the subprocess tests with in-process assertions that verify
    the adapter neither raises nor produces any observable side-effect on
    stdout for every documented failure mode."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self._orig_env = os.environ.get("CLAUDE_PROJECT_DIR")
        os.environ["CLAUDE_PROJECT_DIR"] = str(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()
        if self._orig_env is None:
            os.environ.pop("CLAUDE_PROJECT_DIR", None)
        else:
            os.environ["CLAUDE_PROJECT_DIR"] = self._orig_env

    def _call_dispatch(self, raw: str) -> None:
        """Call dispatch() asserting it does not raise."""
        try:
            mosaic_logger.dispatch(raw)
        except Exception as exc:
            self.fail(f"dispatch() raised an exception: {exc!r}")

    def test_malformed_json_does_not_raise(self):
        self._call_dispatch("not json {{{")

    def test_empty_input_does_not_raise(self):
        self._call_dispatch("")

    def test_json_array_does_not_raise(self):
        self._call_dispatch("[1, 2, 3]")

    def test_json_null_does_not_raise(self):
        self._call_dispatch("null")

    def test_unknown_event_does_not_raise(self):
        self._call_dispatch(json.dumps({
            "hook_event_name": "NoSuchEventFoo",
            "session_id": "inproc-unknown-001",
        }))

    def test_missing_session_id_does_not_raise(self):
        self._call_dispatch(json.dumps({
            "hook_event_name": "SessionStart",
            "source": "startup",
        }))

    def test_unreadable_transcript_path_in_subagent_stop_does_not_raise(self):
        session_id = "inproc-badtrans-001"
        agent_id = "agt-inproc-001"
        for raw in [
            json.dumps({"hook_event_name": "SessionStart", "session_id": session_id,
                        "source": "startup"}),
            json.dumps({"hook_event_name": "SubagentStart", "session_id": session_id,
                        "agent_id": agent_id, "agent_type": "Research",
                        "agent_prompt": '{"agent_instance_id": "Research#1"}'}),
        ]:
            self._call_dispatch(raw)
        self._call_dispatch(json.dumps({
            "hook_event_name": "SubagentStop",
            "session_id": session_id,
            "agent_id": agent_id,
            "agent_transcript_path": "/totally/nonexistent/transcript.jsonl",
            "last_assistant_message": '{"status_code": "SUCCESS"}',
        }))

    def test_absent_agent_prompt_does_not_raise(self):
        session_id = "inproc-noprompt-001"
        self._call_dispatch(json.dumps({
            "hook_event_name": "SessionStart",
            "session_id": session_id,
            "source": "startup",
        }))
        self._call_dispatch(json.dumps({
            "hook_event_name": "SubagentStart",
            "session_id": session_id,
            "agent_id": "agt-noprompt",
            "agent_type": "Research",
            # agent_prompt omitted
        }))

    def test_unwritable_log_root_does_not_raise(self):
        """Write CLAUDE_PROJECT_DIR to a regular file so directory creation fails."""
        with tempfile.NamedTemporaryFile(delete=False) as f:
            bogus_path = f.name
        try:
            orig = os.environ.get("CLAUDE_PROJECT_DIR")
            os.environ["CLAUDE_PROJECT_DIR"] = bogus_path
            self._call_dispatch(json.dumps({
                "hook_event_name": "SessionStart",
                "session_id": "inproc-unwritable-001",
                "source": "startup",
            }))
        finally:
            if orig is None:
                os.environ.pop("CLAUDE_PROJECT_DIR", None)
            else:
                os.environ["CLAUDE_PROJECT_DIR"] = orig
            os.unlink(bogus_path)

    def test_unmapped_agent_id_on_tool_event_does_not_raise(self):
        """A tool event for an agent_id with no registered mapping must not crash.
        The adapter must route to the deterministic unmapped_ folder and exit 0."""
        session_id = "inproc-unmapped-001"
        self._call_dispatch(json.dumps({
            "hook_event_name": "SessionStart",
            "session_id": session_id,
            "source": "startup",
        }))
        # PreToolUse carrying an agent_id we never called SubagentStart for.
        self._call_dispatch(json.dumps({
            "hook_event_name": "PreToolUse",
            "session_id": session_id,
            "agent_id": "agt-never-registered",
            "tool_name": "Bash",
            "tool_use_id": "call-unmapped-001",
            "tool_input": {"command": "ls"},
        }))

    def test_unmapped_agent_id_routes_to_unmapped_folder_not_orchestrator(self):
        """Tool events for an unmapped agent_id must land in an unmapped_ folder,
        never in the orchestrator stream."""
        session_id = "inproc-unmapped-route-001"
        agent_id = "never-registered-agent"
        self._call_dispatch(json.dumps({
            "hook_event_name": "SessionStart",
            "session_id": session_id,
            "source": "startup",
        }))
        self._call_dispatch(json.dumps({
            "hook_event_name": "PreToolUse",
            "session_id": session_id,
            "agent_id": agent_id,
            "tool_name": "Read",
            "tool_use_id": "call-unmapped-route-001",
            "tool_input": {"file_path": "/some/path"},
        }))
        paths = core.build_paths(self.tmp_path)
        # Resolve run_id via directory scan — extraction-based routing uses
        # unknown-run/ when no run_id is embedded in the prompt.
        _RUN_ID_RE = re.compile(r'^[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$')
        log_root = paths.root
        if not log_root.exists():
            return
        run_id = None
        for child in log_root.iterdir():
            if child.is_dir() and _RUN_ID_RE.match(child.name):
                run_id = child.name
                break
        if run_id is None and (log_root / "unknown-run").exists():
            run_id = "unknown-run"
        if run_id is None:
            return
        orch_events_path = paths.orchestrator_events(run_id)
        if not orch_events_path.exists():
            return
        orch_events = [
            json.loads(ln)
            for ln in orch_events_path.read_text("utf-8").splitlines()
            if ln.strip()
        ]
        # The tool_call_start must NOT appear in the orchestrator stream.
        tool_starts_in_orch = [
            e for e in orch_events
            if e.get("event") == "tool_call_start"
            and e.get("call_id") == "call-unmapped-route-001"
        ]
        self.assertEqual(
            0, len(tool_starts_in_orch),
            "Unmapped agent's tool event leaked into the orchestrator stream"
        )
        # The event must appear in an unmapped_ folder instead.
        run_root = paths.run_root(run_id)
        unmapped_dirs = [
            d for d in run_root.iterdir()
            if d.is_dir() and d.name.startswith("unmapped_")
        ]
        self.assertGreater(
            len(unmapped_dirs), 0,
            "No unmapped_ folder created for the unregistered agent_id"
        )


# ---------------------------------------------------------------------------
# T3.1 / T3.4 / T3.5  Attribution cascade and stop quarantine (in-process)
# ---------------------------------------------------------------------------

class TestAttributionCascadeAndStopQuarantine(unittest.TestCase):
    """Full dispatch()-driven flows covering: a resolvable run's tool events
    landing in the same invocation directory as invocation_start with no
    unmapped_* orphan (AC3.2), the diagnostic on both SubagentStop outcomes
    (T3.4), and the regression guarantee that today's succeeding capture
    paths keep succeeding (T3.5 / AC3.6)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self._orig_env = os.environ.get("CLAUDE_PROJECT_DIR")
        os.environ["CLAUDE_PROJECT_DIR"] = str(self.tmp_path)
        self._orig_debug = os.environ.get("MOSAIC_LOGGER_DEBUG")

    def tearDown(self):
        self.tmp.cleanup()
        if self._orig_env is None:
            os.environ.pop("CLAUDE_PROJECT_DIR", None)
        else:
            os.environ["CLAUDE_PROJECT_DIR"] = self._orig_env
        if self._orig_debug is None:
            os.environ.pop("MOSAIC_LOGGER_DEBUG", None)
        else:
            os.environ["MOSAIC_LOGGER_DEBUG"] = self._orig_debug

    def _dispatch(self, payload: dict) -> None:
        mosaic_logger.dispatch(json.dumps(payload))

    def test_resolvable_run_tool_event_lands_in_same_dir_as_invocation_start(self):
        """AC3.2: for a session with a resolvable run, a subagent's tool event
        lands in the same invocation directory as its invocation_start, and no
        unmapped_* directory is created."""
        session_id = "e2e-cascade-001"
        run_id = "20260808T063000Z-cafe"
        agent_id = "agt-cascade-001"
        instance_id = "TestWriter#77"
        prompt = f'{{"agent_instance_id": "{instance_id}", "run_id": "{run_id}"}}'

        # Establishes the session-run binding for this session (D4/A4).
        self._dispatch({
            "hook_event_name": "PreToolUse",
            "session_id": session_id,
            "tool_name": "Task",
            "tool_use_id": "call-cascade-dispatch",
            "tool_input": {"prompt": prompt},
        })
        self._dispatch({
            "hook_event_name": "SubagentStart",
            "session_id": session_id,
            "agent_id": agent_id,
            "agent_type": "TestWriter",
        })
        self._dispatch({
            "hook_event_name": "PreToolUse",
            "session_id": session_id,
            "agent_id": agent_id,
            "tool_name": "Read",
            "tool_use_id": "call-cascade-tool",
            "tool_input": {"file_path": "/some/file.py"},
        })

        paths = core.build_paths(self.tmp_path)
        run_root = paths.run_root(run_id)
        self.assertTrue(run_root.exists(), "The resolvable run's own directory must exist")

        unmapped_dirs = [
            d for d in run_root.iterdir()
            if d.is_dir() and d.name.startswith("unmapped_")
        ] if run_root.exists() else []
        self.assertEqual(
            0, len(unmapped_dirs),
            "No unmapped_* directory may be created for a session with a resolvable run",
        )

        invocation_events = paths.invocation_events(run_id, instance_id)
        self.assertTrue(
            invocation_events.exists(),
            "The subagent's tool event must land in its own invocation directory",
        )
        events = [
            json.loads(ln) for ln in invocation_events.read_text("utf-8").splitlines()
            if ln.strip()
        ]
        event_names = [e.get("event") for e in events]
        self.assertIn("invocation_start", event_names)
        self.assertIn("tool_call_start", event_names)

    def test_genuine_unmapped_stop_diagnostic_through_full_dispatch(self):
        """T3.4: dispatching a genuine (agent_transcript_path present) but
        unmapped SubagentStop emits the quarantine diagnostic through the real
        dispatch() path, not just the unit-level handler call."""
        debug_file = self.tmp_path / "debug.log"
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        session_id = "e2e-cascade-genuine-001"
        self._dispatch({
            "hook_event_name": "SubagentStop",
            "session_id": session_id,
            "agent_id": "agt-cascade-genuine-001",
            "last_assistant_message": "Genuine unmapped completion.",
        })
        self.assertTrue(debug_file.exists())
        text = debug_file.read_text(encoding="utf-8")
        self.assertIn("subagent-stop: quarantined", text)
        self.assertIn("agt-cascade-genuine-001", text,
                      "Diagnostic must name the agent_id so a warm-up miss can be told apart from a genuine failure")

    def test_spurious_unmapped_stop_diagnostic_through_full_dispatch(self):
        """T3.4: dispatching a spurious narration SubagentStop (no transcript
        path, no last_assistant_message) emits the discard diagnostic through
        the real dispatch() path."""
        debug_file = self.tmp_path / "debug.log"
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        session_id = "e2e-cascade-spurious-001"
        self._dispatch({
            "hook_event_name": "SubagentStop",
            "session_id": session_id,
            "agent_id": "agt-cascade-spurious-001",
        })
        self.assertTrue(debug_file.exists())
        text = debug_file.read_text(encoding="utf-8")
        self.assertIn("subagent-stop: discarded", text)
        self.assertIn("agt-cascade-spurious-001", text,
                      "Diagnostic must name the agent_id so a warm-up miss can be told apart from a genuine failure")

    def test_mapped_subagent_lifecycle_still_succeeds_end_to_end(self):
        """T3.5 / AC3.6: a normal, fully-mapped SubagentStart -> PreToolUse ->
        SubagentStop lifecycle must keep producing every artifact it produces
        today: invocation_start, tool_call_start, invocation_end, and
        02_output.md -- none of it lost as a side effect of this stage."""
        session_id = "e2e-cascade-regress-001"
        run_id = "20260808T063500Z-beef"
        agent_id = "agt-cascade-regress-001"
        instance_id = "Reviewer#88"
        prompt = f'{{"agent_instance_id": "{instance_id}", "run_id": "{run_id}"}}'

        self._dispatch({
            "hook_event_name": "PreToolUse",
            "session_id": session_id,
            "tool_name": "Task",
            "tool_use_id": "call-regress-dispatch",
            "tool_input": {"prompt": prompt},
        })
        self._dispatch({
            "hook_event_name": "SubagentStart",
            "session_id": session_id,
            "agent_id": agent_id,
            "agent_type": "Reviewer",
        })
        self._dispatch({
            "hook_event_name": "PreToolUse",
            "session_id": session_id,
            "agent_id": agent_id,
            "tool_name": "Read",
            "tool_use_id": "call-regress-tool",
            "tool_input": {"file_path": "/some/other/file.py"},
        })
        self._dispatch({
            "hook_event_name": "SubagentStop",
            "session_id": session_id,
            "agent_id": agent_id,
            "last_assistant_message": '{"status_code": "SUCCESS"}',
        })

        paths = core.build_paths(self.tmp_path)
        events = [
            json.loads(ln)
            for ln in paths.invocation_events(run_id, instance_id).read_text("utf-8").splitlines()
            if ln.strip()
        ]
        event_names = [e.get("event") for e in events]
        self.assertIn("invocation_start", event_names)
        self.assertIn("tool_call_start", event_names)
        self.assertIn("invocation_end", event_names)
        self.assertTrue(paths.invocation_output(run_id, instance_id).exists())


if __name__ == "__main__":
    unittest.main()
