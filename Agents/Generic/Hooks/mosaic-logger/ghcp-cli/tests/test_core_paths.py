"""Tests for path layout, workspace root resolution, HookContext construction,
directory sanitization, event sink routing, atomic replace, and JSONL append
in mosaic_logger_core (ghcp-cli variant).

Key differences from the claude-code variant:
- resolve_workspace_root precedence: payload cwd > process cwd() -- no env var
  as primary signal (GHCP CLI provides cwd on every payload)
- HookContext accepts event as first positional argument (separate from payload)
- HookContext reads camelCase payload fields (sessionId, agentId, agentType,
  transcriptPath, cwd)
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
    """payload cwd > process cwd() — GHCP CLI provides cwd as a first-class field.
    No harness-specific environment variable is used as primary signal."""

    def test_payload_cwd_is_used_when_present(self):
        """payload['cwd'] is the primary workspace signal."""
        with tempfile.TemporaryDirectory() as tmp:
            result = core.resolve_workspace_root({"cwd": tmp})
            self.assertEqual(pathlib.Path(tmp), result)

    def test_process_cwd_used_when_payload_cwd_absent(self):
        """Falls back to process cwd() when payload lacks 'cwd' key."""
        result = core.resolve_workspace_root({})
        self.assertEqual(pathlib.Path.cwd(), result)

    def test_process_cwd_used_when_payload_cwd_is_empty_string(self):
        """Empty string for payload cwd is treated as absent."""
        result = core.resolve_workspace_root({"cwd": ""})
        self.assertEqual(pathlib.Path.cwd(), result)

    def test_process_cwd_used_when_payload_cwd_is_none(self):
        """None for payload cwd is treated as absent."""
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

    def test_no_claude_project_dir_env_var_as_primary_signal(self):
        """GHCP CLI must NOT treat CLAUDE_PROJECT_DIR as primary signal.
        Even when the env var is set to a different path, payload cwd wins."""
        with tempfile.TemporaryDirectory() as tmp_payload:
            with tempfile.TemporaryDirectory() as tmp_env:
                with unittest.mock.patch.dict("os.environ",
                                              {"CLAUDE_PROJECT_DIR": tmp_env}):
                    result = core.resolve_workspace_root({"cwd": tmp_payload})
                    # payload cwd must win over any env var
                    self.assertEqual(pathlib.Path(tmp_payload), result)

    def test_payload_cwd_wins_over_claude_project_dir(self):
        """payload cwd must override CLAUDE_PROJECT_DIR (if present)."""
        with tempfile.TemporaryDirectory() as tmp_payload:
            with tempfile.TemporaryDirectory() as tmp_env:
                with unittest.mock.patch.dict("os.environ",
                                              {"CLAUDE_PROJECT_DIR": tmp_env}):
                    result = core.resolve_workspace_root({"cwd": tmp_payload})
                    self.assertNotEqual(pathlib.Path(tmp_env), result)


class TestHookContextConstruction(unittest.TestCase):
    """HookContext reads camelCase payload fields and accepts event as first param."""

    def _make_ctx(self, event="sessionStart", payload=None):
        if payload is None:
            payload = {}
        with tempfile.TemporaryDirectory() as tmp:
            workspace_root = pathlib.Path(tmp)
            paths = core.build_paths(workspace_root)
            timestamp = "2026-01-01T00:00:00.000Z"
            ctx = core.HookContext(event, payload, workspace_root, paths, timestamp)
            return ctx

    def test_event_stored_from_constructor_parameter(self):
        """event comes from the first constructor parameter, not from payload."""
        ctx = self._make_ctx(event="preToolUse")
        self.assertEqual("preToolUse", ctx.event)

    def test_session_id_mapped_from_camelcase_sessionId(self):
        """sessionId (camelCase) maps to session_id attribute."""
        ctx = self._make_ctx(payload={"sessionId": "sess-abc-123"})
        self.assertEqual("sess-abc-123", ctx.session_id)

    def test_session_id_absent_when_sessionId_not_in_payload(self):
        ctx = self._make_ctx(payload={})
        self.assertIsNone(ctx.session_id)

    def test_session_id_absent_when_sessionId_is_null(self):
        ctx = self._make_ctx(payload={"sessionId": None})
        self.assertIsNone(ctx.session_id)

    def test_session_id_absent_when_sessionId_is_empty_string(self):
        ctx = self._make_ctx(payload={"sessionId": ""})
        self.assertIsNone(ctx.session_id)

    def test_cwd_mapped_from_payload_cwd(self):
        """cwd is read from the payload's 'cwd' field."""
        ctx = self._make_ctx(payload={"cwd": "/home/user/project"})
        self.assertEqual("/home/user/project", ctx.cwd)

    def test_cwd_absent_when_not_in_payload(self):
        ctx = self._make_ctx(payload={})
        self.assertIsNone(ctx.cwd)

    def test_agent_id_mapped_from_camelcase_agentId(self):
        """agentId (camelCase) maps to agent_id attribute."""
        ctx = self._make_ctx(payload={"agentId": "harness-agent-001"})
        self.assertEqual("harness-agent-001", ctx.agent_id)

    def test_agent_id_absent_when_agentId_not_in_payload(self):
        ctx = self._make_ctx(payload={})
        self.assertIsNone(ctx.agent_id)

    def test_agent_id_absent_when_agentId_is_null(self):
        ctx = self._make_ctx(payload={"agentId": None})
        self.assertIsNone(ctx.agent_id)

    def test_agent_type_mapped_from_camelcase_agentType(self):
        """agentType (camelCase) maps to agent_type attribute."""
        ctx = self._make_ctx(payload={"agentType": "TestWriter"})
        self.assertEqual("TestWriter", ctx.agent_type)

    def test_agent_type_absent_when_agentType_not_in_payload(self):
        ctx = self._make_ctx(payload={})
        self.assertIsNone(ctx.agent_type)

    def test_transcript_path_mapped_from_camelcase_transcriptPath(self):
        """transcriptPath (camelCase) maps to transcript_path attribute."""
        ctx = self._make_ctx(payload={"transcriptPath": "/path/to/transcript.jsonl"})
        self.assertEqual("/path/to/transcript.jsonl", ctx.transcript_path)

    def test_transcript_path_absent_when_transcriptPath_not_in_payload(self):
        ctx = self._make_ctx(payload={})
        self.assertIsNone(ctx.transcript_path)

    def test_payload_stored_verbatim(self):
        """The raw payload dict is accessible via ctx.payload."""
        payload = {"sessionId": "s1", "toolName": "bash", "toolUseId": "tu-1"}
        ctx = self._make_ctx(payload=payload)
        self.assertEqual(payload, ctx.payload)

    def test_run_id_initially_none(self):
        """run_id starts as None; it is set later by the dispatcher."""
        ctx = self._make_ctx()
        self.assertIsNone(ctx.run_id)

    def test_timestamp_stored_from_constructor(self):
        with tempfile.TemporaryDirectory() as tmp:
            workspace_root = pathlib.Path(tmp)
            paths = core.build_paths(workspace_root)
            ts = "2026-07-31T13:26:37.123Z"
            ctx = core.HookContext("sessionStart", {}, workspace_root, paths, ts)
            self.assertEqual(ts, ctx.timestamp)

    def test_event_name_not_read_from_payload(self):
        """Unlike claude-code, GHCP CLI event name is NOT in the payload.
        Verify the event attribute is set from the constructor param only."""
        payload = {"sessionId": "s1"}  # no hook_event_name field
        ctx = self._make_ctx(event="notification", payload=payload)
        self.assertEqual("notification", ctx.event)
        self.assertNotIn("hook_event_name", ctx.payload)


class TestHookContextField(unittest.TestCase):
    """HookContext.field() performs single top-level lookup with normalization."""

    def _make_ctx(self, payload):
        with tempfile.TemporaryDirectory() as tmp:
            workspace_root = pathlib.Path(tmp)
            paths = core.build_paths(workspace_root)
            return core.HookContext("sessionStart", payload, workspace_root, paths,
                                   "2026-01-01T00:00:00.000Z")

    def test_field_returns_value_for_present_key(self):
        ctx = self._make_ctx({"toolName": "bash"})
        self.assertEqual("bash", ctx.field("toolName"))

    def test_field_returns_none_for_absent_key(self):
        ctx = self._make_ctx({})
        self.assertIsNone(ctx.field("toolName"))

    def test_field_returns_none_for_null_value(self):
        ctx = self._make_ctx({"toolName": None})
        self.assertIsNone(ctx.field("toolName"))

    def test_field_returns_none_for_empty_string_value(self):
        ctx = self._make_ctx({"toolName": ""})
        self.assertIsNone(ctx.field("toolName"))

    def test_field_returns_false_as_resolved_value(self):
        ctx = self._make_ctx({"resumed": False})
        self.assertIs(False, ctx.field("resumed"))

    def test_field_returns_zero_as_resolved_value(self):
        ctx = self._make_ctx({"count": 0})
        self.assertEqual(0, ctx.field("count"))

    def test_field_returns_dict_value_without_normalization(self):
        """field() does not normalize dict values — only top-level absent/null/empty."""
        ctx = self._make_ctx({"toolArgs": {"path": "/file.py"}})
        self.assertEqual({"path": "/file.py"}, ctx.field("toolArgs"))

    def test_field_does_not_support_dotted_paths(self):
        """field() only supports top-level keys; dotted paths are not interpreted."""
        ctx = self._make_ctx({"toolResult": {"textResultForLlm": "the result"}})
        # Accessing a dotted path returns None (no special nested access behavior)
        result = ctx.field("toolResult.textResultForLlm")
        self.assertIsNone(result)

    def test_field_reads_camelcase_key_directly(self):
        """field() reads whatever key name is given — camelCase works directly."""
        ctx = self._make_ctx({"notificationType": "info"})
        self.assertEqual("info", ctx.field("notificationType"))


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

    def test_agent_map_entry_has_json_suffix(self):
        entry = self.paths.agent_map_entry(self.run_id, "harness-agent-001")
        self.assertEqual(".json", entry.suffix)

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
        self.assertEqual("RequirementsRefinement",
                         core.sanitize_component("RequirementsRefinement"))

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
        result = core.event_sink_for(self.ctx, "RequirementsRefinement#2")
        self.assertEqual(
            self.paths.invocation_events(self.run_id, "RequirementsRefinement#2"),
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
