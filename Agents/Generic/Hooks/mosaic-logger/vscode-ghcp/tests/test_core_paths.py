"""Tests for path layout, workspace root resolution, HookContext construction,
directory sanitization, event sink routing, atomic replace, JSONL append, and
adapter version reading in mosaic_logger_core (vscode-ghcp variant).

Key differences from the claude-code and ghcp-cli variants:
- HookContext.__init__ takes (payload, workspace_root, paths, timestamp) —
  event comes from payload["hook_event_name"], NOT a separate constructor arg.
- Payload fields are snake_case (session_id, agent_id, cwd, transcript_path,
  agent_type) matching VS Code's payload format.
- HookContext adds a first-class 'agent_name' attribute for VS Code's
  correlation key (absent in both reference adapters).
- resolve_workspace_root: payload["cwd"] first, then Path.cwd() — NO workspace-
  root environment variable is consulted.  The WORKSPACE_ENV_VAR constant must
  be absent entirely so a copy-paste regression is detectable by grep.
"""

import json
import os
import pathlib
import sys
import tempfile
import types
import unittest
import unittest.mock

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core


class TestWorkspaceRootResolution(unittest.TestCase):
    """payload['cwd'] first, Path.cwd() fallback — no environment variable."""

    def test_payload_cwd_is_used_when_present(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = core.resolve_workspace_root({"cwd": tmp})
            self.assertEqual(pathlib.Path(tmp), result)

    def test_process_cwd_used_when_payload_cwd_absent(self):
        result = core.resolve_workspace_root({})
        self.assertEqual(pathlib.Path.cwd(), result)

    def test_process_cwd_used_when_payload_cwd_is_empty_string(self):
        result = core.resolve_workspace_root({"cwd": ""})
        self.assertEqual(pathlib.Path.cwd(), result)

    def test_process_cwd_used_when_payload_cwd_is_none(self):
        result = core.resolve_workspace_root({"cwd": None})
        self.assertEqual(pathlib.Path.cwd(), result)

    def test_result_is_always_a_path_object(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = core.resolve_workspace_root({"cwd": tmp})
            self.assertIsInstance(result, pathlib.Path)

    def test_result_is_always_absolute(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = core.resolve_workspace_root({"cwd": tmp})
            self.assertTrue(result.is_absolute())

    def test_no_workspace_env_var_constant_defined(self):
        """VS Code sets no CLAUDE_PROJECT_DIR equivalent; the constant must be absent."""
        self.assertFalse(hasattr(core, "WORKSPACE_ENV_VAR"),
                         "WORKSPACE_ENV_VAR must not exist on vscode-ghcp core module "
                         "to prevent copy-paste regressions")

    def test_claude_project_dir_env_var_is_never_consulted(self):
        """Even when CLAUDE_PROJECT_DIR is set, payload cwd wins and env var is ignored."""
        with tempfile.TemporaryDirectory() as tmp_payload:
            with tempfile.TemporaryDirectory() as tmp_env:
                with unittest.mock.patch.dict("os.environ",
                                              {"CLAUDE_PROJECT_DIR": tmp_env}):
                    result = core.resolve_workspace_root({"cwd": tmp_payload})
                    self.assertEqual(pathlib.Path(tmp_payload), result)
                    self.assertNotEqual(pathlib.Path(tmp_env), result)

    def test_claude_project_dir_not_used_as_fallback_when_cwd_absent(self):
        """CLAUDE_PROJECT_DIR must not be used even as a fallback for absent cwd."""
        with tempfile.TemporaryDirectory() as tmp_env:
            with unittest.mock.patch.dict("os.environ",
                                          {"CLAUDE_PROJECT_DIR": tmp_env},
                                          clear=False):
                # Fallback must be process cwd, NOT CLAUDE_PROJECT_DIR
                result = core.resolve_workspace_root({})
                self.assertEqual(pathlib.Path.cwd(), result)


class TestHookContextConstruction(unittest.TestCase):
    """HookContext reads hook_event_name from payload; payload fields are snake_case.
    agent_name is a new first-class attribute specific to the vscode-ghcp adapter."""

    def _make_ctx(self, payload=None):
        if payload is None:
            payload = {}
        with tempfile.TemporaryDirectory() as tmp:
            workspace_root = pathlib.Path(tmp)
            paths = core.build_paths(workspace_root)
            timestamp = "2026-01-01T00:00:00.000Z"
            return core.HookContext(payload, workspace_root, paths, timestamp)

    def test_event_read_from_payload_hook_event_name(self):
        """event comes from payload['hook_event_name'], not a separate constructor param."""
        ctx = self._make_ctx(payload={"hook_event_name": "SessionStart"})
        self.assertEqual("SessionStart", ctx.event)

    def test_event_is_empty_string_when_hook_event_name_absent(self):
        """Absent hook_event_name produces an empty-string event (routes to no-op)."""
        ctx = self._make_ctx(payload={})
        self.assertEqual("", ctx.event)

    def test_event_is_empty_string_when_hook_event_name_is_none(self):
        ctx = self._make_ctx(payload={"hook_event_name": None})
        self.assertEqual("", ctx.event)

    def test_session_id_mapped_from_snake_case_session_id(self):
        """session_id (snake_case) maps to ctx.session_id."""
        ctx = self._make_ctx(payload={"session_id": "sess-abc-123"})
        self.assertEqual("sess-abc-123", ctx.session_id)

    def test_session_id_absent_when_not_in_payload(self):
        ctx = self._make_ctx(payload={})
        self.assertIsNone(ctx.session_id)

    def test_session_id_absent_when_session_id_is_none(self):
        ctx = self._make_ctx(payload={"session_id": None})
        self.assertIsNone(ctx.session_id)

    def test_session_id_absent_when_session_id_is_empty_string(self):
        ctx = self._make_ctx(payload={"session_id": ""})
        self.assertIsNone(ctx.session_id)

    def test_cwd_mapped_from_payload_cwd(self):
        ctx = self._make_ctx(payload={"cwd": "/workspace/myproject"})
        self.assertEqual("/workspace/myproject", ctx.cwd)

    def test_cwd_absent_when_not_in_payload(self):
        ctx = self._make_ctx(payload={})
        self.assertIsNone(ctx.cwd)

    def test_agent_id_mapped_from_snake_case_agent_id(self):
        """agent_id (snake_case) maps to ctx.agent_id."""
        ctx = self._make_ctx(payload={"agent_id": "vscode-agent-001"})
        self.assertEqual("vscode-agent-001", ctx.agent_id)

    def test_agent_id_absent_when_not_in_payload(self):
        """VS Code's SubagentStart has no agent_id — must be None without payload key."""
        ctx = self._make_ctx(payload={})
        self.assertIsNone(ctx.agent_id)

    def test_agent_id_absent_when_agent_id_is_none(self):
        ctx = self._make_ctx(payload={"agent_id": None})
        self.assertIsNone(ctx.agent_id)

    def test_agent_name_mapped_from_snake_case_agent_name(self):
        """agent_name is VS Code's primary correlation key — must be a first-class attr."""
        ctx = self._make_ctx(payload={"agent_name": "TestWriter"})
        self.assertEqual("TestWriter", ctx.agent_name)

    def test_agent_name_absent_when_not_in_payload(self):
        ctx = self._make_ctx(payload={})
        self.assertIsNone(ctx.agent_name)

    def test_agent_name_absent_when_agent_name_is_none(self):
        ctx = self._make_ctx(payload={"agent_name": None})
        self.assertIsNone(ctx.agent_name)

    def test_agent_name_absent_when_agent_name_is_empty_string(self):
        ctx = self._make_ctx(payload={"agent_name": ""})
        self.assertIsNone(ctx.agent_name)

    def test_agent_name_attribute_exists_even_when_not_in_payload(self):
        """agent_name must be a defined attribute (None), not a missing attribute."""
        ctx = self._make_ctx(payload={})
        self.assertTrue(hasattr(ctx, "agent_name"),
                        "ctx.agent_name must exist as a first-class attribute")

    def test_agent_type_mapped_from_snake_case_agent_type(self):
        ctx = self._make_ctx(payload={"agent_type": "ContractsDesigner"})
        self.assertEqual("ContractsDesigner", ctx.agent_type)

    def test_agent_type_absent_when_not_in_payload(self):
        ctx = self._make_ctx(payload={})
        self.assertIsNone(ctx.agent_type)

    def test_transcript_path_mapped_from_snake_case_transcript_path(self):
        ctx = self._make_ctx(payload={"transcript_path": "/path/to/transcript"})
        self.assertEqual("/path/to/transcript", ctx.transcript_path)

    def test_transcript_path_absent_when_not_in_payload(self):
        ctx = self._make_ctx(payload={})
        self.assertIsNone(ctx.transcript_path)

    def test_payload_stored_verbatim(self):
        payload = {"hook_event_name": "SessionStart", "session_id": "s1",
                   "agent_name": "MyAgent"}
        ctx = self._make_ctx(payload=payload)
        self.assertEqual(payload, ctx.payload)

    def test_run_id_initially_none(self):
        """run_id starts as None; set later by dispatcher or SubagentStart handler."""
        ctx = self._make_ctx()
        self.assertIsNone(ctx.run_id)

    def test_timestamp_stored_from_constructor(self):
        with tempfile.TemporaryDirectory() as tmp:
            workspace_root = pathlib.Path(tmp)
            paths = core.build_paths(workspace_root)
            ts = "2026-07-31T13:26:37.123Z"
            ctx = core.HookContext({}, workspace_root, paths, ts)
            self.assertEqual(ts, ctx.timestamp)

    def test_both_agent_id_and_agent_name_can_be_set(self):
        """Rare case where both fields appear in the payload."""
        ctx = self._make_ctx(payload={
            "agent_id": "some-opaque-id",
            "agent_name": "TestWriter",
        })
        self.assertEqual("some-opaque-id", ctx.agent_id)
        self.assertEqual("TestWriter", ctx.agent_name)


class TestHookContextField(unittest.TestCase):
    """HookContext.field() performs single top-level lookup with normalization."""

    def _make_ctx(self, payload):
        with tempfile.TemporaryDirectory() as tmp:
            workspace_root = pathlib.Path(tmp)
            paths = core.build_paths(workspace_root)
            return core.HookContext(payload, workspace_root, paths,
                                   "2026-01-01T00:00:00.000Z")

    def test_field_returns_value_for_present_key(self):
        ctx = self._make_ctx({"tool_name": "bash"})
        self.assertEqual("bash", ctx.field("tool_name"))

    def test_field_returns_none_for_absent_key(self):
        ctx = self._make_ctx({})
        self.assertIsNone(ctx.field("tool_name"))

    def test_field_returns_none_for_null_value(self):
        ctx = self._make_ctx({"tool_name": None})
        self.assertIsNone(ctx.field("tool_name"))

    def test_field_returns_none_for_empty_string_value(self):
        ctx = self._make_ctx({"tool_name": ""})
        self.assertIsNone(ctx.field("tool_name"))

    def test_field_returns_false_as_resolved_value(self):
        ctx = self._make_ctx({"resumed": False})
        self.assertIs(False, ctx.field("resumed"))

    def test_field_returns_zero_as_resolved_value(self):
        ctx = self._make_ctx({"count": 0})
        self.assertEqual(0, ctx.field("count"))

    def test_field_returns_dict_value_without_normalization(self):
        """field() does not normalize dict values — only top-level absent/null/empty."""
        ctx = self._make_ctx({"tool_input": {"path": "/file.py"}})
        self.assertEqual({"path": "/file.py"}, ctx.field("tool_input"))

    def test_field_does_not_support_dotted_paths(self):
        """field() is flat-only by design; nested access goes through named seams."""
        ctx = self._make_ctx({"tool_result": {"text_result_for_llm": "the output"}})
        result = ctx.field("tool_result.text_result_for_llm")
        self.assertIsNone(result)

    def test_field_reads_snake_case_key_directly(self):
        ctx = self._make_ctx({"agent_name": "PlannerTDD"})
        self.assertEqual("PlannerTDD", ctx.field("agent_name"))


class TestLogPathsLayout(unittest.TestCase):
    """build_paths returns a LogPaths whose computed paths match the documented layout."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.root)
        self.run_id = "20260101T170000Z-ab12"

    def tearDown(self):
        self.tmp.cleanup()

    def test_root_is_workspace_orchestration_logs(self):
        self.assertEqual(self.root / "OrchestrationLogs", self.paths.root)

    def test_run_root_contains_run_id_component(self):
        result = self.paths.run_root(self.run_id)
        self.assertEqual(self.root / "OrchestrationLogs" / self.run_id, result)

    def test_orchestrator_events_path(self):
        result = self.paths.orchestrator_events(self.run_id)
        expected = (
            self.root / "OrchestrationLogs" / self.run_id / "00_orchestrator_events.jsonl"
        )
        self.assertEqual(expected, result)

    def test_orchestrator_raw_path(self):
        result = self.paths.orchestrator_raw(self.run_id)
        expected = (
            self.root / "OrchestrationLogs" / self.run_id / "00_orchestrator_session.raw"
        )
        self.assertEqual(expected, result)

    def test_invocation_dir_path(self):
        """invocation_dir returns the direct parent directory for the invocation files."""
        result = self.paths.invocation_dir(self.run_id, "TestAgent#1")
        sanitized = core.sanitize_component("TestAgent#1")
        expected = self.root / "OrchestrationLogs" / self.run_id / sanitized
        self.assertEqual(expected, result)

    def test_invocation_events_filename(self):
        result = self.paths.invocation_events(self.run_id, "TestAgent#1")
        self.assertEqual("03_events.jsonl", result.name)

    def test_invocation_events_is_under_run_root(self):
        result = self.paths.invocation_events(self.run_id, "TestAgent#1")
        self.assertTrue(str(result).startswith(str(self.paths.run_root(self.run_id))))

    def test_invocation_events_sanitizes_agent_instance_id_with_reserved_chars(self):
        result = self.paths.invocation_events(self.run_id, 'Agent"Name#1')
        self.assertNotIn('"', str(result))

    def test_invocation_input_filename(self):
        result = self.paths.invocation_input(self.run_id, "TestAgent#1")
        self.assertEqual("01_input.md", result.name)

    def test_invocation_output_filename(self):
        result = self.paths.invocation_output(self.run_id, "TestAgent#1")
        self.assertEqual("02_output.md", result.name)

    def test_invocation_raw_filename(self):
        result = self.paths.invocation_raw(self.run_id, "TestAgent#1")
        self.assertEqual("04_session.raw", result.name)

    def test_invocation_input_and_events_are_siblings(self):
        events = self.paths.invocation_events(self.run_id, "TestAgent#1")
        inp = self.paths.invocation_input(self.run_id, "TestAgent#1")
        self.assertEqual(events.parent, inp.parent)

    def test_pending_dispatch_dir_is_under_run_root(self):
        d = self.paths.pending_dispatch_dir(self.run_id)
        self.assertTrue(str(d).startswith(str(self.paths.run_root(self.run_id))))

    def test_pending_dispatch_dir_uses_dot_prefix_dirname(self):
        d = self.paths.pending_dispatch_dir(self.run_id)
        self.assertEqual(".pending-dispatch", d.name)

    def test_pending_dispatch_entry_has_jsonl_suffix(self):
        entry = self.paths.pending_dispatch_entry(self.run_id, "sess-1")
        self.assertEqual(".jsonl", entry.suffix)

    def test_pending_dispatch_entry_is_inside_pending_dispatch_dir(self):
        entry = self.paths.pending_dispatch_entry(self.run_id, "sess-1")
        expected_dir = self.paths.pending_dispatch_dir(self.run_id)
        self.assertEqual(expected_dir, entry.parent)

    def test_build_paths_creates_no_directories(self):
        new_tmp = tempfile.TemporaryDirectory()
        new_root = pathlib.Path(new_tmp.name) / "workspace"
        core.build_paths(new_root)
        self.assertFalse(new_root.exists())
        new_tmp.cleanup()

    def test_agent_map_dir_is_under_run_root(self):
        d = self.paths.agent_map_dir(self.run_id)
        self.assertTrue(str(d).startswith(str(self.paths.run_root(self.run_id))))

    def test_agent_map_dir_uses_dot_agent_map_dirname(self):
        d = self.paths.agent_map_dir(self.run_id)
        self.assertEqual(".agent-map", d.name)

    def test_agent_map_entry_has_json_suffix(self):
        entry = self.paths.agent_map_entry(self.run_id, "TestWriter")
        self.assertEqual(".json", entry.suffix)

    def test_agent_map_entry_is_inside_agent_map_dir(self):
        entry = self.paths.agent_map_entry(self.run_id, "TestWriter")
        expected_dir = self.paths.agent_map_dir(self.run_id)
        self.assertEqual(expected_dir, entry.parent)

    def test_unknown_run_is_a_valid_run_root(self):
        """The 'unknown-run' fallback bucket must resolve to a valid path."""
        result = self.paths.run_root("unknown-run")
        self.assertEqual(self.root / "OrchestrationLogs" / "unknown-run", result)


class TestSanitizeComponent(unittest.TestCase):
    """sanitize_component replaces reserved/control chars, strips trailing dots and spaces."""

    def test_reserved_chars_are_replaced_with_underscore(self):
        for char in '<>:"/\\|?*':
            result = core.sanitize_component(f"name{char}value")
            self.assertEqual("name_value", result,
                             f"Expected char {char!r} to be replaced with _")

    def test_plain_identifier_is_unchanged(self):
        self.assertEqual("TestWriter", core.sanitize_component("TestWriter"))

    def test_alphanumeric_with_hyphens_and_dots_is_unchanged(self):
        self.assertEqual("run-2026.01.01", core.sanitize_component("run-2026.01.01"))

    def test_control_character_replaced_with_underscore(self):
        result = core.sanitize_component("name\x01value")
        self.assertEqual("name_value", result)

    def test_null_byte_replaced_with_underscore(self):
        result = core.sanitize_component("name\x00value")
        self.assertEqual("name_value", result)

    def test_trailing_dots_stripped(self):
        result = core.sanitize_component("name...")
        self.assertEqual("name", result)

    def test_trailing_spaces_stripped(self):
        result = core.sanitize_component("name   ")
        self.assertEqual("name", result)

    def test_empty_string_returns_underscore(self):
        result = core.sanitize_component("")
        self.assertEqual("_", result)

    def test_fully_stripped_result_returns_underscore(self):
        result = core.sanitize_component("...")
        self.assertEqual("_", result)

    def test_mapping_is_deterministic(self):
        name = 'Agent<Name>:value"/path\\file|here?now*done'
        first = core.sanitize_component(name)
        second = core.sanitize_component(name)
        self.assertEqual(first, second)


class TestEventSinkRouting(unittest.TestCase):
    """event_sink_for routes to orchestrator (None agent) or invocation stream."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(tmp_path)
        self.run_id = "20260101T170000Z-ab12"
        self.ctx = types.SimpleNamespace(run_id=self.run_id, paths=self.paths)

    def tearDown(self):
        self.tmp.cleanup()

    def test_none_agent_routes_to_orchestrator_events_file(self):
        result = core.event_sink_for(self.ctx, None)
        self.assertEqual(self.paths.orchestrator_events(self.run_id), result)

    def test_agent_instance_id_routes_to_invocation_events_file(self):
        result = core.event_sink_for(self.ctx, "TestWriter#2")
        self.assertEqual(
            self.paths.invocation_events(self.run_id, "TestWriter#2"),
            result,
        )

    def test_agent_instance_id_does_not_route_to_orchestrator_file(self):
        orchestrator = self.paths.orchestrator_events(self.run_id)
        result = core.event_sink_for(self.ctx, "SomeAgent#1")
        self.assertNotEqual(orchestrator, result)

    def test_different_agent_ids_route_to_different_files(self):
        sink_a = core.event_sink_for(self.ctx, "AgentA#1")
        sink_b = core.event_sink_for(self.ctx, "AgentB#1")
        self.assertNotEqual(sink_a, sink_b)

    def test_same_agent_id_always_routes_to_same_file(self):
        first = core.event_sink_for(self.ctx, "TestAgent#3")
        second = core.event_sink_for(self.ctx, "TestAgent#3")
        self.assertEqual(first, second)


class TestAppendEvent(unittest.TestCase):
    """append_event writes exactly one JSON object per call to a JSONL file."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_appended_content_is_valid_json(self):
        path = self.tmp_path / "events.jsonl"
        event = {"event": "test", "value": "hello"}
        core.append_event(path, event)
        parsed = json.loads(path.read_text(encoding="utf-8").strip())
        self.assertEqual(event, parsed)

    def test_each_call_appends_exactly_one_line(self):
        path = self.tmp_path / "events.jsonl"
        core.append_event(path, {"event": "first"})
        core.append_event(path, {"event": "second"})
        lines = [ln for ln in path.read_text(encoding="utf-8").splitlines() if ln.strip()]
        self.assertEqual(2, len(lines))

    def test_lines_are_written_in_order(self):
        path = self.tmp_path / "events.jsonl"
        core.append_event(path, {"event": "first"})
        core.append_event(path, {"event": "second"})
        lines = [ln for ln in path.read_text(encoding="utf-8").splitlines() if ln.strip()]
        self.assertEqual("first", json.loads(lines[0])["event"])
        self.assertEqual("second", json.loads(lines[1])["event"])

    def test_creates_parent_directories_when_missing(self):
        path = self.tmp_path / "nested" / "dir" / "events.jsonl"
        core.append_event(path, {"event": "test"})
        self.assertTrue(path.exists())

    def test_does_not_raise_on_unwritable_path(self):
        blocker = self.tmp_path / "blocker_parent"
        blocker.write_bytes(b"")
        unwritable = blocker / "events.jsonl"
        try:
            core.append_event(unwritable, {"event": "test"})
        except Exception as e:
            self.fail(f"append_event raised unexpectedly: {e}")

    def test_serialized_newlines_in_content_do_not_split_the_line(self):
        path = self.tmp_path / "events.jsonl"
        event = {"event": "turn", "content": "line one\nline two\nline three"}
        core.append_event(path, event)
        lines = [ln for ln in path.read_text(encoding="utf-8").splitlines() if ln.strip()]
        self.assertEqual(1, len(lines))


class TestAtomicReplace(unittest.TestCase):
    """atomic_replace writes data via temp-file-then-rename; returns True/False, never raises."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def test_writes_correct_content(self):
        path = self.tmp_path / "marker.json"
        data = b'{"status": "open"}'
        core.atomic_replace(path, data)
        self.assertEqual(data, path.read_bytes())

    def test_replaces_existing_content(self):
        path = self.tmp_path / "marker.json"
        core.atomic_replace(path, b"old content")
        core.atomic_replace(path, b"new content")
        self.assertEqual(b"new content", path.read_bytes())

    def test_returns_true_on_success(self):
        path = self.tmp_path / "marker.json"
        result = core.atomic_replace(path, b"data")
        self.assertTrue(result)

    def test_returns_false_on_failure_without_raising(self):
        blocker = self.tmp_path / "blocker_parent"
        blocker.write_bytes(b"")
        result = core.atomic_replace(blocker / "file.json", b"data")
        self.assertFalse(result)

    def test_text_wrapper_writes_utf8_encoded_bytes(self):
        path = self.tmp_path / "text.json"
        text = '{"key": "café"}'
        core.atomic_replace_text(path, text)
        self.assertEqual(text.encode("utf-8"), path.read_bytes())

    def test_text_wrapper_returns_true_on_success(self):
        path = self.tmp_path / "text.json"
        result = core.atomic_replace_text(path, '{"ok": true}')
        self.assertTrue(result)

    def test_text_wrapper_returns_false_on_failure(self):
        blocker = self.tmp_path / "blocker_parent_text"
        blocker.write_bytes(b"")
        result = core.atomic_replace_text(blocker / "file.json", "data")
        self.assertFalse(result)


class TestCurrentTimestamp(unittest.TestCase):
    """current_timestamp returns ISO 8601 UTC with millisecond precision and Z suffix."""

    def test_returns_string(self):
        ts = core.current_timestamp()
        self.assertIsInstance(ts, str)

    def test_ends_with_z_suffix(self):
        ts = core.current_timestamp()
        self.assertTrue(ts.endswith("Z"))

    def test_matches_iso8601_ms_z_format(self):
        import re
        ts = core.current_timestamp()
        pattern = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$")
        self.assertRegex(ts, pattern)


if __name__ == "__main__":
    unittest.main()
