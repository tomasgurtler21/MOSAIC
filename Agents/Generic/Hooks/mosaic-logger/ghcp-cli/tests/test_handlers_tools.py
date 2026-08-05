"""Tests for tool-use event handlers in mosaic_logger_handlers_tools (ghcp-cli variant).

Key differences from the claude-code variant:
- HookContext.__init__ takes event name as first argument (from sys.argv[1])
- Payload fields are camelCase: toolName, toolArgs, toolUseId, toolResult, agentId, error
- Tool names are GHCP CLI native (lowercase): 'agent' and 'task' (not PascalCase 'Agent'/'Task')
- postToolUse reads nested path: toolResult.textResultForLlm (not top-level via ctx.field)
- resolve_destination routes to orchestrator when no agentId, else to invocation stream
- preToolUse MUST NEVER write stdout (fail-closed for non-zero exit from GHCP CLI)
- toolUseId (camelCase) is the harness-native call ID field

Covers:
  resolve_destination: orchestrator vs. invocation stream routing based on agentId
  resolve_call_id: uses toolUseId when present, fallback otherwise
  handle_pre_tool_use: emits tool_call_start, captures pending dispatch for agent/task
  handle_post_tool_use: reads toolResult.textResultForLlm, emits tool_call_end status=success
  handle_post_tool_use_failure: emits tool_call_end status=error
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
import mosaic_logger_runstate as runstate
import mosaic_logger_handlers_tools as tools


# ---------------------------------------------------------------------------
# Shared helpers
# ---------------------------------------------------------------------------

_RUN_ID = "20260101T120000Z-cd34"
_SESSION_ID = "test-ghcp-tools-session"
_AGENT_ID = "ghcp-agt-tools-001"
_INSTANCE_ID = "Research#2"
_TS = "2026-01-01T12:00:00.000Z"


def _make_ctx(event: str, tmp_path: pathlib.Path,
              run_id: str = _RUN_ID,
              session_id: str = _SESSION_ID,
              **payload_extra) -> core.HookContext:
    """Build a HookContext with run_id pre-set."""
    payload = {"sessionId": session_id, **payload_extra}
    paths = core.build_paths(tmp_path)
    ctx = core.HookContext(event, payload, tmp_path, paths, _TS)
    ctx.run_id = run_id
    return ctx


def _read_jsonl(path: pathlib.Path) -> list:
    return [json.loads(ln) for ln in path.read_text("utf-8").splitlines() if ln.strip()]


def _register_mapping(tmp_path, run_id, agent_id, agent_instance_id):
    paths = core.build_paths(pathlib.Path(tmp_path))
    runstate.put_agent_mapping(paths, run_id, agent_id, agent_instance_id, None)


# ---------------------------------------------------------------------------
# HANDLERS registry
# ---------------------------------------------------------------------------

class TestToolHandlersRegistry(unittest.TestCase):

    def test_handlers_is_a_dict(self):
        self.assertIsInstance(tools.HANDLERS, dict)

    def test_all_three_tool_events_registered(self):
        self.assertIn("preToolUse", tools.HANDLERS)
        self.assertIn("postToolUse", tools.HANDLERS)
        self.assertIn("postToolUseFailure", tools.HANDLERS)

    def test_all_handlers_callable(self):
        for event, handler in tools.HANDLERS.items():
            with self.subTest(event=event):
                self.assertTrue(callable(handler))


# ---------------------------------------------------------------------------
# resolve_destination
# ---------------------------------------------------------------------------

class TestResolveDestination(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_routes_to_orchestrator_when_no_agent_id(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash")
        dest = tools.resolve_destination(ctx)
        expected = ctx.paths.orchestrator_events(_RUN_ID)
        self.assertEqual(expected, dest)

    def test_routes_to_invocation_when_agent_id_present(self):
        _register_mapping(self.tmp_path, _RUN_ID, _AGENT_ID, _INSTANCE_ID)
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId=_AGENT_ID, toolName="bash")
        dest = tools.resolve_destination(ctx)
        expected = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertEqual(expected, dest)

    def test_routes_to_unmapped_bucket_when_agent_not_registered(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId="unregistered-agent-xyz",
                        toolName="bash")
        dest = tools.resolve_destination(ctx)
        # Unmapped agent routes to invocation_events with unmapped_ prefix
        self.assertIn("unmapped_", str(dest))

    def test_orchestrator_routing_with_empty_agent_id(self):
        """Empty string agentId normalizes to None -> orchestrator routing."""
        ctx = _make_ctx("preToolUse", self.tmp_path, agentId="", toolName="bash")
        dest = tools.resolve_destination(ctx)
        expected = ctx.paths.orchestrator_events(_RUN_ID)
        self.assertEqual(expected, dest)


# ---------------------------------------------------------------------------
# resolve_call_id
# ---------------------------------------------------------------------------

class TestResolveCallId(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_tool_use_id_when_present(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash",
                        toolUseId="tu-12345")
        self.assertEqual("tu-12345", tools.resolve_call_id(ctx))

    def test_returns_fallback_when_tool_use_id_absent(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="view")
        call_id = tools.resolve_call_id(ctx)
        self.assertIsNotNone(call_id)
        self.assertIsInstance(call_id, str)
        self.assertGreater(len(call_id), 0)

    def test_fallback_call_id_contains_tool_name(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="create")
        call_id = tools.resolve_call_id(ctx)
        self.assertIn("create", call_id)

    def test_returns_fallback_when_tool_use_id_is_empty_string(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash", toolUseId="")
        call_id = tools.resolve_call_id(ctx)
        # Empty string normalizes to None -> fallback
        self.assertIsNotNone(call_id)


# ---------------------------------------------------------------------------
# handle_pre_tool_use
# ---------------------------------------------------------------------------

class TestHandlePreToolUse(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _read_orch_events(self, ctx):
        path = ctx.paths.orchestrator_events(_RUN_ID)
        if not path.exists():
            return []
        return _read_jsonl(path)

    def test_emits_tool_call_start_event(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash",
                        toolArgs={"command": "ls -la"}, toolUseId="tu-001")
        tools.handle_pre_tool_use(ctx)
        events = self._read_orch_events(ctx)
        self.assertEqual(1, len(events))
        self.assertEqual("tool_call_start", events[0]["event"])

    def test_emitted_event_has_tool_name(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="view",
                        toolUseId="tu-002")
        tools.handle_pre_tool_use(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertEqual("view", event["tool_name"])

    def test_emitted_event_has_call_id(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash",
                        toolUseId="tu-abc-123")
        tools.handle_pre_tool_use(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertEqual("tu-abc-123", event["call_id"])

    def test_tool_input_included_when_present(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash",
                        toolArgs={"command": "echo hello"}, toolUseId="tu-003")
        tools.handle_pre_tool_use(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertIn("tool_input", event)
        self.assertEqual("echo hello", event["tool_input"]["command"])

    def test_writes_nothing_to_stdout(self):
        """CRITICAL SAFETY: preToolUse fail-closed -- stdout could deny tool call."""
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash",
                        toolArgs={}, toolUseId="tu-004")
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            tools.handle_pre_tool_use(ctx)
        self.assertEqual("", buf.getvalue())

    def test_no_permission_decision_in_stdout_for_agent_tool(self):
        """Even for 'agent' tool invocations, stdout must be empty."""
        prompt_with_id = f'agent_instance_id: "TestWriter#5"\nrun_id: "20260101T000000Z-ab12"'
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="agent",
                        toolArgs={"prompt": prompt_with_id}, toolUseId="tu-005")
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            tools.handle_pre_tool_use(ctx)
        self.assertEqual("", buf.getvalue())

    def test_captures_pending_dispatch_for_agent_tool(self):
        """When toolName is 'agent', must persist pending dispatch for subagentStart handoff."""
        prompt_with_id = f'agent_instance_id: "Planner#3"\nrun_id: "20260101T000000Z-bb22"'
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="agent",
                        toolArgs={"prompt": prompt_with_id}, toolUseId="tu-006")
        tools.handle_pre_tool_use(ctx)
        paths = ctx.paths
        dispatch = runstate.pop_pending_dispatch(paths, _RUN_ID, _SESSION_ID)
        self.assertIsNotNone(dispatch)
        self.assertEqual("Planner#3", dispatch["agent_instance_id"])

    def test_captures_pending_dispatch_for_task_tool(self):
        """When toolName is 'task' (lowercase GHCP CLI native), also captures dispatch."""
        prompt_with_id = f'agent_instance_id: "Executor#1"\n'
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="task",
                        toolArgs={"prompt": prompt_with_id}, toolUseId="tu-007")
        tools.handle_pre_tool_use(ctx)
        dispatch = runstate.pop_pending_dispatch(ctx.paths, _RUN_ID, _SESSION_ID)
        self.assertIsNotNone(dispatch)
        self.assertEqual("Executor#1", dispatch["agent_instance_id"])

    def test_does_not_capture_dispatch_for_non_agent_tool(self):
        """bash, view, create etc. must not create a pending dispatch."""
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash",
                        toolArgs={"command": "ls"}, toolUseId="tu-008")
        tools.handle_pre_tool_use(ctx)
        dispatch = runstate.pop_pending_dispatch(ctx.paths, _RUN_ID, _SESSION_ID)
        self.assertIsNone(dispatch)

    def test_routes_to_orchestrator_when_no_agent_id(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash", toolUseId="tu-009")
        tools.handle_pre_tool_use(ctx)
        self.assertTrue(ctx.paths.orchestrator_events(_RUN_ID).exists())

    def test_routes_to_invocation_stream_when_agent_id_present(self):
        _register_mapping(self.tmp_path, _RUN_ID, _AGENT_ID, _INSTANCE_ID)
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash",
                        agentId=_AGENT_ID, toolUseId="tu-010")
        tools.handle_pre_tool_use(ctx)
        inv_file = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(inv_file.exists())

    def test_does_not_raise(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="bash")
        try:
            tools.handle_pre_tool_use(ctx)
        except Exception as exc:
            self.fail(f"handle_pre_tool_use raised: {exc}")

    def test_does_not_raise_with_malformed_tool_args(self):
        ctx = _make_ctx("preToolUse", self.tmp_path, toolName="agent",
                        toolArgs="not a dict", toolUseId="tu-011")
        try:
            tools.handle_pre_tool_use(ctx)
        except Exception as exc:
            self.fail(f"handle_pre_tool_use raised with malformed toolArgs: {exc}")


# ---------------------------------------------------------------------------
# handle_pre_tool_use — run_id capture end-to-end wiring
# ---------------------------------------------------------------------------

_CAPTURE_RUN_ID = "20260804T150537Z-796a"
_CAPTURE_SESSION_ID = "ghcp-sess-capture-e2e"
_CAPTURE_PATH = f"C:/AI/MOSAIC/MOSAIC/Orchestration-{_CAPTURE_RUN_ID}/Stage-1/Plan.md"


class TestHandlePreToolUseRunIdCapture(unittest.TestCase):
    """handle_pre_tool_use must invoke capture_run_id_from_tool_args before
    resolve_destination, so the triggering preToolUse event is routed correctly
    and the session cache is populated as a side effect.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _make_capture_ctx(self, tool_args=None, run_id_preset=None):
        """Build a preToolUse HookContext for the capture tests, without a pre-set run_id."""
        payload = {"sessionId": _CAPTURE_SESSION_ID}
        if tool_args is not None:
            payload["toolArgs"] = tool_args
        paths = core.build_paths(self.tmp_path)
        ctx = core.HookContext("preToolUse", payload, self.tmp_path, paths, _TS)
        ctx.run_id = run_id_preset
        return ctx

    def test_session_cache_is_populated_when_tool_args_contain_orchestration_path(self):
        """Driving a preToolUse payload with an Orchestration-{run_id} path through
        handle_pre_tool_use must populate the session run-id cache as a side effect.
        """
        ctx = self._make_capture_ctx(
            tool_args={"filePath": _CAPTURE_PATH},
        )
        tools.handle_pre_tool_use(ctx)
        cached = runstate.get_session_run_id(ctx.paths, _CAPTURE_SESSION_ID)
        self.assertEqual(_CAPTURE_RUN_ID, cached)

    def test_ctx_run_id_is_set_before_destination_resolution(self):
        """When handle_pre_tool_use processes a path-bearing preToolUse event and
        ctx.run_id is unset, ctx.run_id must be assigned the observed run_id so
        the emitted tool_call_start event lands in the correct run folder.
        """
        ctx = self._make_capture_ctx(
            tool_args={"filePath": _CAPTURE_PATH},
            run_id_preset=None,
        )
        tools.handle_pre_tool_use(ctx)
        # The event must have been written to the correct run folder, not unknown-run.
        expected_orch = ctx.paths.orchestrator_events(_CAPTURE_RUN_ID)
        self.assertTrue(
            expected_orch.exists(),
            f"Expected tool_call_start in run {_CAPTURE_RUN_ID!r} folder, "
            f"but {expected_orch} was not created.",
        )

    def test_session_cache_not_written_when_tool_args_have_no_run_id(self):
        """When toolArgs carry no Orchestration-{run_id} path, the cache must not
        be written for that session.
        """
        ctx = self._make_capture_ctx(
            tool_args={"filePath": "/workspace/src/main.py"},
        )
        tools.handle_pre_tool_use(ctx)
        cached = runstate.get_session_run_id(ctx.paths, _CAPTURE_SESSION_ID)
        self.assertIsNone(cached)

    def test_capture_does_not_suppress_tool_call_start_event(self):
        """A successful run_id capture must not prevent the tool_call_start event
        from being written — the capture runs before destination resolution, inside
        its own guard, so even if it were to fail the event write must still happen.
        """
        ctx = self._make_capture_ctx(
            tool_args={"filePath": _CAPTURE_PATH},
            run_id_preset=None,
        )
        tools.handle_pre_tool_use(ctx)
        # After capture sets ctx.run_id, the event is written to the correct run folder.
        orch_path = ctx.paths.orchestrator_events(_CAPTURE_RUN_ID)
        events = [json.loads(ln) for ln in orch_path.read_text("utf-8").splitlines() if ln.strip()]
        self.assertEqual(1, len(events))
        self.assertEqual("tool_call_start", events[0]["event"])


# ---------------------------------------------------------------------------
# handle_post_tool_use
# ---------------------------------------------------------------------------

class TestHandlePostToolUse(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _read_orch_events(self, ctx):
        path = ctx.paths.orchestrator_events(_RUN_ID)
        if not path.exists():
            return []
        return _read_jsonl(path)

    def test_emits_tool_call_end_event(self):
        ctx = _make_ctx("postToolUse", self.tmp_path, toolName="bash",
                        toolUseId="tu-001",
                        toolResult={"resultType": "text", "textResultForLlm": "output"})
        tools.handle_post_tool_use(ctx)
        events = self._read_orch_events(ctx)
        self.assertEqual(1, len(events))
        self.assertEqual("tool_call_end", events[0]["event"])

    def test_status_is_success(self):
        ctx = _make_ctx("postToolUse", self.tmp_path, toolName="bash",
                        toolUseId="tu-002",
                        toolResult={"textResultForLlm": "output text"})
        tools.handle_post_tool_use(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertEqual("success", event["status"])

    def test_reads_tool_output_from_nested_tool_result(self):
        """Reads toolResult.textResultForLlm (nested access, not via ctx.field)."""
        ctx = _make_ctx("postToolUse", self.tmp_path, toolName="view",
                        toolUseId="tu-003",
                        toolResult={"resultType": "text",
                                    "textResultForLlm": "File contents here"})
        tools.handle_post_tool_use(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertIn("tool_output", event)
        self.assertEqual("File contents here", event["tool_output"])

    def test_tool_output_absent_when_tool_result_missing(self):
        ctx = _make_ctx("postToolUse", self.tmp_path, toolName="bash",
                        toolUseId="tu-004")
        tools.handle_post_tool_use(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertNotIn("tool_output", event)

    def test_tool_output_absent_when_tool_result_not_a_dict(self):
        ctx = _make_ctx("postToolUse", self.tmp_path, toolName="bash",
                        toolUseId="tu-005", toolResult="string-not-dict")
        tools.handle_post_tool_use(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertNotIn("tool_output", event)

    def test_tool_output_absent_when_text_result_for_llm_is_empty_string(self):
        ctx = _make_ctx("postToolUse", self.tmp_path, toolName="bash",
                        toolUseId="tu-006",
                        toolResult={"textResultForLlm": ""})
        tools.handle_post_tool_use(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertNotIn("tool_output", event)

    def test_tool_output_absent_when_text_result_for_llm_is_none(self):
        ctx = _make_ctx("postToolUse", self.tmp_path, toolName="bash",
                        toolUseId="tu-007",
                        toolResult={"textResultForLlm": None})
        tools.handle_post_tool_use(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertNotIn("tool_output", event)

    def test_routes_to_invocation_stream_when_agent_id_present(self):
        _register_mapping(self.tmp_path, _RUN_ID, _AGENT_ID, _INSTANCE_ID)
        ctx = _make_ctx("postToolUse", self.tmp_path, toolName="bash",
                        agentId=_AGENT_ID, toolUseId="tu-008",
                        toolResult={"textResultForLlm": "result"})
        tools.handle_post_tool_use(ctx)
        inv_file = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(inv_file.exists())

    def test_writes_nothing_to_stdout(self):
        ctx = _make_ctx("postToolUse", self.tmp_path, toolName="bash",
                        toolUseId="tu-009",
                        toolResult={"textResultForLlm": "output"})
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            tools.handle_post_tool_use(ctx)
        self.assertEqual("", buf.getvalue())

    def test_does_not_raise(self):
        ctx = _make_ctx("postToolUse", self.tmp_path, toolName="bash")
        try:
            tools.handle_post_tool_use(ctx)
        except Exception as exc:
            self.fail(f"handle_post_tool_use raised: {exc}")


# ---------------------------------------------------------------------------
# handle_post_tool_use_failure
# ---------------------------------------------------------------------------

class TestHandlePostToolUseFailure(unittest.TestCase):

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _read_orch_events(self, ctx):
        path = ctx.paths.orchestrator_events(_RUN_ID)
        if not path.exists():
            return []
        return _read_jsonl(path)

    def test_emits_tool_call_end_event(self):
        ctx = _make_ctx("postToolUseFailure", self.tmp_path, toolName="bash",
                        toolUseId="tu-001", error="Command not found")
        tools.handle_post_tool_use_failure(ctx)
        events = self._read_orch_events(ctx)
        self.assertEqual(1, len(events))
        self.assertEqual("tool_call_end", events[0]["event"])

    def test_status_is_error(self):
        ctx = _make_ctx("postToolUseFailure", self.tmp_path, toolName="bash",
                        toolUseId="tu-002", error="Permission denied")
        tools.handle_post_tool_use_failure(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertEqual("error", event["status"])

    def test_error_field_included_when_present(self):
        ctx = _make_ctx("postToolUseFailure", self.tmp_path, toolName="view",
                        toolUseId="tu-003", error="File not found: foo.txt")
        tools.handle_post_tool_use_failure(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertIn("error", event)
        self.assertEqual("File not found: foo.txt", event["error"])

    def test_error_field_absent_when_not_in_payload(self):
        ctx = _make_ctx("postToolUseFailure", self.tmp_path, toolName="bash",
                        toolUseId="tu-004")
        tools.handle_post_tool_use_failure(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertNotIn("error", event)

    def test_no_tool_output_field_in_failure_event(self):
        ctx = _make_ctx("postToolUseFailure", self.tmp_path, toolName="bash",
                        toolUseId="tu-005", error="Crash")
        tools.handle_post_tool_use_failure(ctx)
        event = self._read_orch_events(ctx)[0]
        self.assertNotIn("tool_output", event)

    def test_routes_to_invocation_stream_when_agent_id_present(self):
        _register_mapping(self.tmp_path, _RUN_ID, _AGENT_ID, _INSTANCE_ID)
        ctx = _make_ctx("postToolUseFailure", self.tmp_path, toolName="bash",
                        agentId=_AGENT_ID, toolUseId="tu-006", error="Oops")
        tools.handle_post_tool_use_failure(ctx)
        inv_file = ctx.paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(inv_file.exists())

    def test_writes_nothing_to_stdout(self):
        ctx = _make_ctx("postToolUseFailure", self.tmp_path, toolName="bash",
                        toolUseId="tu-007", error="Crash")
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf):
            tools.handle_post_tool_use_failure(ctx)
        self.assertEqual("", buf.getvalue())

    def test_does_not_raise(self):
        ctx = _make_ctx("postToolUseFailure", self.tmp_path, toolName="bash")
        try:
            tools.handle_post_tool_use_failure(ctx)
        except Exception as exc:
            self.fail(f"handle_post_tool_use_failure raised: {exc}")


if __name__ == "__main__":
    unittest.main()
