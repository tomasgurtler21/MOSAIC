"""Tests for the PreToolUse and PostToolUse handlers (vscode-ghcp variant).

Key vscode-ghcp divergences from reference adapters:
  D4: tool output is nested snake_case: tool_result.text_result_for_llm
      (claude-code uses a flat field; ghcp-cli uses toolResult.textResultForLlm)
  D4: read_tool_output bypasses ctx.field() (which is flat-only)
  AC2.11: VS Code matchers parse but do not filter — ALL filtering happens inside
      the handler body via SUBAGENT_DISPATCH_TOOL_NAMES; the registration fragment
      declares no matcher
  AC2.2: No PostToolUseFailure handler — the event is unconfirmed for VS Code;
      every completed tool call is recorded as status="success"

Covers:
  SUBAGENT_DISPATCH_TOOL_NAMES: module-top constant listing Agent, Task, runSubagent
  read_tool_output: reads nested tool_result.text_result_for_llm; None for absent,
      non-dict, or empty-string; does NOT read camelCase or top-level field
  resolve_destination: orchestrator when agent_id absent, invocation when present
  resolve_call_id: uses tool_use_id when present, fallback otherwise
  handle_pre_tool_use: emits tool_call_start; captures pending dispatch only for
      SUBAGENT_DISPATCH_TOOL_NAMES; non-matching tool_name skips capture
  handle_post_tool_use: emits tool_call_end with status="success"; tool_output
      from nested path; no status="error" path (PostToolUseFailure absent)
  HANDLERS: exactly PreToolUse and PostToolUse, no PostToolUseFailure
"""

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

_RUN_ID = "20260101T170000Z-b5c8"
_SESSION_ID = "test-vscode-tools-session"
_TS = "2026-01-01T17:00:00.000Z"


def _make_ctx(tmp_path, hook_event_name, run_id=_RUN_ID,
              session_id=_SESSION_ID, **payload_extra):
    """Build a HookContext with run_id pre-set, rooted at tmp_path."""
    payload = {
        "hook_event_name": hook_event_name,
        "session_id": session_id,
        **payload_extra,
    }
    workspace_root = pathlib.Path(tmp_path)
    paths = core.build_paths(workspace_root)
    ctx = core.HookContext(payload, workspace_root, paths, _TS)
    ctx.run_id = run_id
    return ctx


def _pre_tool_ctx(tmp_path, **kwargs):
    return _make_ctx(tmp_path, "PreToolUse", **kwargs)


def _post_tool_ctx(tmp_path, **kwargs):
    return _make_ctx(tmp_path, "PostToolUse", **kwargs)


def _read_jsonl(path):
    """Read all non-empty lines of a JSONL file as parsed dicts."""
    return [json.loads(ln) for ln in pathlib.Path(path).read_text("utf-8").splitlines()
            if ln.strip()]


def _register_mapping(tmp_path, run_id, agent_key, agent_instance_id, agent_type=None):
    """Pre-register an agent_key -> agent_instance_id mapping on disk."""
    paths = core.build_paths(pathlib.Path(tmp_path))
    runstate.put_agent_mapping(paths, run_id, agent_key, agent_instance_id, agent_type)


# ---------------------------------------------------------------------------
# SUBAGENT_DISPATCH_TOOL_NAMES constant
# ---------------------------------------------------------------------------

class TestSubagentDispatchToolNames(unittest.TestCase):
    """SUBAGENT_DISPATCH_TOOL_NAMES is the single module-top seam for dispatch capture.

    VS Code matchers do not filter — all tool events fire for all invocations.
    The handler body must test tool_name against this constant and skip capture
    for non-matching names.
    """

    def test_constant_exists_at_module_level(self):
        self.assertTrue(hasattr(tools, "SUBAGENT_DISPATCH_TOOL_NAMES"),
                        "SUBAGENT_DISPATCH_TOOL_NAMES must be a module-top constant")

    def test_constant_contains_agent(self):
        self.assertIn("Agent", tools.SUBAGENT_DISPATCH_TOOL_NAMES)

    def test_constant_contains_task(self):
        self.assertIn("Task", tools.SUBAGENT_DISPATCH_TOOL_NAMES)

    def test_constant_contains_run_subagent(self):
        self.assertIn("runSubagent", tools.SUBAGENT_DISPATCH_TOOL_NAMES)

    def test_constant_does_not_contain_bash(self):
        """Bash is a regular tool, not a subagent-dispatch tool."""
        self.assertNotIn("Bash", tools.SUBAGENT_DISPATCH_TOOL_NAMES)

    def test_constant_is_not_empty(self):
        self.assertGreater(len(tools.SUBAGENT_DISPATCH_TOOL_NAMES), 0)


# ---------------------------------------------------------------------------
# read_tool_output (D4 seam — nested snake_case)
# ---------------------------------------------------------------------------

class TestReadToolOutput(unittest.TestCase):
    """read_tool_output reads tool_result.text_result_for_llm from the context payload.

    This is the D4 seam: VS Code uses nested snake_case, claude-code uses a flat
    field, and ghcp-cli uses nested camelCase. read_tool_output is the single
    function that isolates this difference.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_reads_nested_text_result_for_llm(self):
        """Reads from tool_result.text_result_for_llm (nested snake_case path)."""
        ctx = _post_tool_ctx(self.tmp_path,
                             tool_name="Read",
                             tool_result={"text_result_for_llm": "file content here"})
        result = tools.read_tool_output(ctx)
        self.assertEqual("file content here", result)

    def test_returns_none_when_tool_result_absent(self):
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read")
        result = tools.read_tool_output(ctx)
        self.assertIsNone(result)

    def test_returns_none_when_tool_result_is_not_a_dict(self):
        """tool_result must be a dict; non-dict values must return None."""
        for non_dict in ("string result", 42, None, [1, 2, 3]):
            with self.subTest(tool_result=non_dict):
                ctx = _post_tool_ctx(self.tmp_path, tool_name="Read",
                                     tool_result=non_dict)
                result = tools.read_tool_output(ctx)
                self.assertIsNone(result,
                                  f"Non-dict tool_result {non_dict!r} must return None")

    def test_returns_none_when_text_result_for_llm_absent(self):
        """Absent nested key must return None, not raise."""
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read",
                             tool_result={"result_type": "text"})
        result = tools.read_tool_output(ctx)
        self.assertIsNone(result)

    def test_returns_none_when_text_result_for_llm_is_empty_string(self):
        """Empty string normalizes to None (degrade-never-fabricate rule)."""
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read",
                             tool_result={"text_result_for_llm": ""})
        result = tools.read_tool_output(ctx)
        self.assertIsNone(result)

    def test_does_not_read_camelcase_field(self):
        """textResultForLlm is the ghcp-cli field name; must NOT be read here."""
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read",
                             tool_result={"textResultForLlm": "wrong field name"})
        result = tools.read_tool_output(ctx)
        self.assertIsNone(result,
                          "textResultForLlm (camelCase) must not be read — "
                          "vscode-ghcp uses snake_case text_result_for_llm")

    def test_does_not_read_top_level_tool_output(self):
        """The claude-code adapter reads tool_output at the top level; this adapter must not."""
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read",
                             tool_output="top-level field should be ignored")
        result = tools.read_tool_output(ctx)
        self.assertIsNone(result,
                          "tool_output at top level must not be read — "
                          "vscode-ghcp reads nested tool_result.text_result_for_llm")

    def test_does_not_raise(self):
        """read_tool_output is a total function; it never raises."""
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read")
        try:
            tools.read_tool_output(ctx)
        except Exception as exc:
            self.fail(f"read_tool_output raised: {exc}")


# ---------------------------------------------------------------------------
# resolve_destination
# ---------------------------------------------------------------------------

class TestResolveDestination(unittest.TestCase):
    """resolve_destination routes to orchestrator when agent_id absent, else invocation.

    VS Code's documented tool payloads carry no agent_id, so all tool calls land
    in the orchestrator stream today. The agent_id branch is retained so it
    activates automatically if VS Code adds the field.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def test_no_agent_id_routes_to_orchestrator_events(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-001")
        dest = tools.resolve_destination(ctx)
        expected = self.paths.orchestrator_events(_RUN_ID)
        self.assertEqual(expected, dest)

    def test_orchestrator_destination_filename_is_00_orchestrator_events(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Read")
        dest = tools.resolve_destination(ctx)
        self.assertEqual("00_orchestrator_events.jsonl", dest.name)

    def test_agent_id_present_routes_to_invocation_stream(self):
        _register_mapping(self.tmp_path, _RUN_ID, "aid-001", "ToolAgent#1")
        ctx = _pre_tool_ctx(self.tmp_path, agent_id="aid-001", tool_name="Read")
        dest = tools.resolve_destination(ctx)
        expected = self.paths.invocation_events(_RUN_ID, "ToolAgent#1")
        self.assertEqual(expected, dest)

    def test_agent_id_present_never_routes_to_orchestrator(self):
        _register_mapping(self.tmp_path, _RUN_ID, "aid-002", "ToolAgent#2")
        ctx = _pre_tool_ctx(self.tmp_path, agent_id="aid-002", tool_name="Bash")
        dest = tools.resolve_destination(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertNotEqual(orc_path, dest)

    def test_destination_contains_run_id_in_path(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Read")
        dest = tools.resolve_destination(ctx)
        self.assertIn(_RUN_ID, str(dest))

    def test_does_not_raise(self):
        ctx = _pre_tool_ctx(self.tmp_path)
        try:
            tools.resolve_destination(ctx)
        except Exception as exc:
            self.fail(f"resolve_destination raised: {exc}")


# ---------------------------------------------------------------------------
# resolve_call_id
# ---------------------------------------------------------------------------

class TestResolveCallId(unittest.TestCase):
    """resolve_call_id uses tool_use_id when present, else a fallback."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_uses_tool_use_id_when_present(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-xyz-001")
        call_id = tools.resolve_call_id(ctx)
        self.assertEqual("tu-xyz-001", call_id)

    def test_returns_non_empty_fallback_when_tool_use_id_absent(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Write")
        call_id = tools.resolve_call_id(ctx)
        self.assertTrue(call_id, "resolve_call_id must return a non-empty fallback")

    def test_does_not_raise(self):
        ctx = _pre_tool_ctx(self.tmp_path)
        try:
            tools.resolve_call_id(ctx)
        except Exception as exc:
            self.fail(f"resolve_call_id raised: {exc}")


# ---------------------------------------------------------------------------
# handle_pre_tool_use
# ---------------------------------------------------------------------------

class TestHandlePreToolUse(unittest.TestCase):
    """handle_pre_tool_use emits tool_call_start.

    For tool names in SUBAGENT_DISPATCH_TOOL_NAMES, it also captures a pending
    dispatch entry when an agent_instance_id can be extracted from tool_input.prompt.
    Non-matching tool names must NOT trigger the capture (filter is in the handler
    body, not via a matcher pattern).
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def _orc_events(self):
        return _read_jsonl(self.paths.orchestrator_events(_RUN_ID))

    def test_emits_tool_call_start_event(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-100")
        tools.handle_pre_tool_use(ctx)
        events = self._orc_events()
        self.assertEqual(1, len(events))
        self.assertEqual("tool_call_start", events[0]["event"])

    def test_tool_call_start_has_call_id(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-101")
        tools.handle_pre_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertIn("call_id", event)
        self.assertTrue(event["call_id"])

    def test_tool_call_start_uses_tool_use_id_as_call_id(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="my-id-xyz")
        tools.handle_pre_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertEqual("my-id-xyz", event["call_id"])

    def test_tool_call_start_has_tool_name(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Bash", tool_use_id="tu-102")
        tools.handle_pre_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertEqual("Bash", event["tool_name"])

    def test_tool_input_included_when_present(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-103",
                            tool_input={"file_path": "/some/file.py"})
        tools.handle_pre_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertIn("tool_input", event)
        self.assertEqual({"file_path": "/some/file.py"}, event["tool_input"])

    def test_tool_input_absent_when_not_in_payload(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-104")
        tools.handle_pre_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertNotIn("tool_input", event)

    def test_event_written_to_orchestrator_when_no_agent_id(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Write", tool_use_id="tu-105")
        tools.handle_pre_tool_use(ctx)
        self.assertTrue(self.paths.orchestrator_events(_RUN_ID).exists())

    def test_harness_is_vscode_ghcp_in_emitted_event(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-106")
        tools.handle_pre_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertEqual("vscode-ghcp", event["harness"])

    def test_agent_tool_name_captures_pending_dispatch_when_instance_id_extractable(self):
        """'Agent' tool with extractable agent_instance_id must put a pending-dispatch entry."""
        prompt_text = '{"agent_instance_id": "TestWriter#1"}'
        ctx = _pre_tool_ctx(
            self.tmp_path, tool_name="Agent", tool_use_id="tu-agent-001",
            tool_input={"prompt": prompt_text}
        )
        tools.handle_pre_tool_use(ctx)
        # Pop the entry to verify it was written
        entry = runstate.pop_pending_dispatch(self.paths, _RUN_ID, _SESSION_ID)
        self.assertIsNotNone(entry,
                             "A pending dispatch entry must be written for Agent tool with extractable ID")
        self.assertEqual("TestWriter#1", entry.get("agent_instance_id"))

    def test_non_dispatch_tool_does_not_capture_pending_dispatch(self):
        """A Bash invocation must not create a pending-dispatch entry."""
        ctx = _pre_tool_ctx(
            self.tmp_path, tool_name="Bash", tool_use_id="tu-bash-001",
            tool_input={"command": 'echo hello'}
        )
        tools.handle_pre_tool_use(ctx)
        entry = runstate.pop_pending_dispatch(self.paths, _RUN_ID, _SESSION_ID)
        self.assertIsNone(entry,
                          "Non-dispatch tool (Bash) must not write a pending dispatch entry "
                          "— filtering happens inside the handler body")

    def test_read_tool_does_not_capture_pending_dispatch(self):
        """Read is not a dispatch tool; no pending-dispatch entry must be written."""
        ctx = _pre_tool_ctx(
            self.tmp_path, tool_name="Read", tool_use_id="tu-read-001",
            tool_input={"file_path": "/tmp/test.py"}
        )
        tools.handle_pre_tool_use(ctx)
        entry = runstate.pop_pending_dispatch(self.paths, _RUN_ID, _SESSION_ID)
        self.assertIsNone(entry)

    def test_task_tool_name_captures_pending_dispatch(self):
        """'Task' is also a dispatch tool name and must capture the dispatch."""
        prompt_text = '{"agent_instance_id": "Researcher#2"}'
        ctx = _pre_tool_ctx(
            self.tmp_path, tool_name="Task", tool_use_id="tu-task-001",
            tool_input={"prompt": prompt_text}
        )
        tools.handle_pre_tool_use(ctx)
        entry = runstate.pop_pending_dispatch(self.paths, _RUN_ID, _SESSION_ID)
        self.assertIsNotNone(entry,
                             "'Task' tool must also capture a pending dispatch entry")

    def test_run_subagent_tool_name_captures_pending_dispatch(self):
        """'runSubagent' is a dispatch tool name and must capture the dispatch."""
        prompt_text = '{"agent_instance_id": "Planner#1"}'
        ctx = _pre_tool_ctx(
            self.tmp_path, tool_name="runSubagent", tool_use_id="tu-rs-001",
            tool_input={"prompt": prompt_text}
        )
        tools.handle_pre_tool_use(ctx)
        entry = runstate.pop_pending_dispatch(self.paths, _RUN_ID, _SESSION_ID)
        self.assertIsNotNone(entry,
                             "'runSubagent' tool must also capture a pending dispatch entry")

    def test_tool_call_start_still_emitted_even_when_capture_fails(self):
        """A failed pending-dispatch capture must not suppress the tool_call_start event."""
        ctx = _pre_tool_ctx(
            self.tmp_path, tool_name="Agent", tool_use_id="tu-agent-002",
            tool_input={"prompt": "no extractable id here"}  # no agent_instance_id
        )
        tools.handle_pre_tool_use(ctx)
        events = self._orc_events()
        self.assertEqual(1, len(events))
        self.assertEqual("tool_call_start", events[0]["event"])

    def test_handler_does_not_raise(self):
        ctx = _pre_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-200")
        try:
            tools.handle_pre_tool_use(ctx)
        except Exception as exc:
            self.fail(f"handle_pre_tool_use raised unexpectedly: {exc}")


# ---------------------------------------------------------------------------
# handle_post_tool_use
# ---------------------------------------------------------------------------

class TestHandlePostToolUse(unittest.TestCase):
    """handle_post_tool_use emits tool_call_end with status="success".

    Every completed tool call records status="success" because PostToolUseFailure
    is unconfirmed for VS Code.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def _orc_events(self):
        return _read_jsonl(self.paths.orchestrator_events(_RUN_ID))

    def test_emits_tool_call_end_event(self):
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-200",
                             tool_result={"text_result_for_llm": "file content"})
        tools.handle_post_tool_use(ctx)
        events = self._orc_events()
        self.assertEqual(1, len(events))
        self.assertEqual("tool_call_end", events[0]["event"])

    def test_status_is_always_success(self):
        """Every completed PostToolUse records status='success' (no failure event)."""
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-201",
                             tool_result={"text_result_for_llm": "content"})
        tools.handle_post_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertEqual("success", event["status"])

    def test_tool_output_from_nested_tool_result(self):
        """tool_output must come from tool_result.text_result_for_llm."""
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-202",
                             tool_result={"text_result_for_llm": "the file content"})
        tools.handle_post_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertEqual("the file content", event.get("tool_output"))

    def test_tool_output_absent_when_tool_result_absent(self):
        """When tool_result is absent, tool_output must not be fabricated."""
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-203")
        tools.handle_post_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertNotIn("tool_output", event)

    def test_tool_output_absent_when_text_result_for_llm_empty(self):
        """Empty string normalizes to None; tool_output must be absent."""
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-204",
                             tool_result={"text_result_for_llm": ""})
        tools.handle_post_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertNotIn("tool_output", event)

    def test_tool_output_does_not_read_camelcase_field(self):
        """textResultForLlm (ghcp-cli field) must not be read here."""
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-205",
                             tool_result={"textResultForLlm": "wrong field"})
        tools.handle_post_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertNotIn("tool_output", event,
                         "camelCase textResultForLlm must not be read by vscode-ghcp adapter")

    def test_call_id_matches_tool_use_id(self):
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-my-id",
                             tool_result={"text_result_for_llm": "content"})
        tools.handle_post_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertEqual("tu-my-id", event["call_id"])

    def test_no_null_values_in_emitted_event(self):
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-206",
                             tool_result={"text_result_for_llm": "content"})
        tools.handle_post_tool_use(ctx)
        event = self._orc_events()[0]
        for key, value in event.items():
            with self.subTest(key=key):
                self.assertIsNotNone(value)

    def test_handler_does_not_raise(self):
        ctx = _post_tool_ctx(self.tmp_path, tool_name="Read", tool_use_id="tu-207")
        try:
            tools.handle_post_tool_use(ctx)
        except Exception as exc:
            self.fail(f"handle_post_tool_use raised unexpectedly: {exc}")


# ---------------------------------------------------------------------------
# HANDLERS registry — tools module
# ---------------------------------------------------------------------------

class TestToolHandlersRegistry(unittest.TestCase):
    """HANDLERS in the tools module contains exactly PreToolUse and PostToolUse.

    PostToolUseFailure is absent — the event is unconfirmed for VS Code.
    """

    def test_handlers_is_a_dict(self):
        self.assertIsInstance(tools.HANDLERS, dict)

    def test_pre_tool_use_registered(self):
        self.assertIn("PreToolUse", tools.HANDLERS)

    def test_post_tool_use_registered(self):
        self.assertIn("PostToolUse", tools.HANDLERS)

    def test_post_tool_use_failure_absent(self):
        """PostToolUseFailure is not confirmed for VS Code and must not be registered."""
        self.assertNotIn("PostToolUseFailure", tools.HANDLERS)

    def test_handlers_has_exactly_two_entries(self):
        self.assertEqual(2, len(tools.HANDLERS))

    def test_all_handlers_callable(self):
        for event, handler in tools.HANDLERS.items():
            with self.subTest(event=event):
                self.assertTrue(callable(handler))


if __name__ == "__main__":
    unittest.main()
