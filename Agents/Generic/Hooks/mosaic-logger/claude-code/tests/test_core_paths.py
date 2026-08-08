"""Tests for path layout, workspace root resolution, directory sanitization,
event sink routing, atomic replace, and JSONL append in mosaic_logger_core.
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
    """CLAUDE_PROJECT_DIR > payload cwd > process cwd, strict precedence."""

    def test_env_var_takes_precedence_over_payload_cwd(self):
        with tempfile.TemporaryDirectory() as tmp:
            with unittest.mock.patch.dict("os.environ", {"CLAUDE_PROJECT_DIR": tmp}):
                result = core.resolve_workspace_root({"cwd": "/some/other/path"})
                self.assertEqual(pathlib.Path(tmp), result)

    def test_payload_cwd_used_when_env_var_absent(self):
        with tempfile.TemporaryDirectory() as tmp:
            clean_env = {k: v for k, v in os.environ.items() if k != "CLAUDE_PROJECT_DIR"}
            with unittest.mock.patch.dict("os.environ", clean_env, clear=True):
                result = core.resolve_workspace_root({"cwd": tmp})
                self.assertEqual(pathlib.Path(tmp), result)

    def test_process_cwd_used_when_both_absent(self):
        clean_env = {k: v for k, v in os.environ.items() if k != "CLAUDE_PROJECT_DIR"}
        with unittest.mock.patch.dict("os.environ", clean_env, clear=True):
            result = core.resolve_workspace_root({})
            self.assertEqual(pathlib.Path.cwd(), result)

    def test_empty_env_var_falls_through_to_payload_cwd(self):
        with tempfile.TemporaryDirectory() as tmp:
            with unittest.mock.patch.dict("os.environ", {"CLAUDE_PROJECT_DIR": ""}):
                result = core.resolve_workspace_root({"cwd": tmp})
                self.assertEqual(pathlib.Path(tmp), result)

    def test_env_var_absent_and_empty_payload_cwd_falls_through(self):
        clean_env = {k: v for k, v in os.environ.items() if k != "CLAUDE_PROJECT_DIR"}
        with unittest.mock.patch.dict("os.environ", clean_env, clear=True):
            result = core.resolve_workspace_root({"cwd": ""})
            self.assertEqual(pathlib.Path.cwd(), result)

    def test_result_is_always_absolute_path(self):
        with tempfile.TemporaryDirectory() as tmp:
            with unittest.mock.patch.dict("os.environ", {"CLAUDE_PROJECT_DIR": tmp}):
                result = core.resolve_workspace_root({})
                self.assertTrue(result.is_absolute())


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

    def test_build_paths_creates_no_directories(self):
        # build_paths must be pure path construction; no I/O side effects
        new_tmp = tempfile.TemporaryDirectory()
        new_root = pathlib.Path(new_tmp.name) / "workspace"
        core.build_paths(new_root)
        self.assertFalse(new_root.exists())
        new_tmp.cleanup()


class TestLogPathsQuarantine(unittest.TestCase):
    """build_paths returns a LogPaths whose quarantine path-builders match the
    documented layout: a dot-prefixed directory inside the run folder, laid
    out like an ordinary invocation directory (ContractsDesign.md A1/D5)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.root)
        self.run_id = "20260101T170000Z-ab12"

    def tearDown(self):
        self.tmp.cleanup()

    def test_quarantine_dir_is_dot_prefixed_child_of_run_root(self):
        result = self.paths.quarantine_dir(self.run_id)
        expected = self.paths.run_root(self.run_id) / ".quarantine"
        self.assertEqual(expected, result)

    def test_quarantine_invocation_dir_is_under_quarantine_dir(self):
        result = self.paths.quarantine_invocation_dir(self.run_id, "aid-1")
        self.assertEqual(self.paths.quarantine_dir(self.run_id), result.parent)

    def test_quarantine_invocation_dir_sanitizes_agent_id_with_reserved_chars(self):
        result = self.paths.quarantine_invocation_dir(self.run_id, 'agent"id')
        self.assertNotIn('"', str(result))

    def test_quarantine_events_filename(self):
        result = self.paths.quarantine_events(self.run_id, "aid-1")
        self.assertEqual("03_events.jsonl", result.name)

    def test_quarantine_output_filename(self):
        result = self.paths.quarantine_output(self.run_id, "aid-1")
        self.assertEqual("02_output.md", result.name)

    def test_quarantine_raw_filename(self):
        result = self.paths.quarantine_raw(self.run_id, "aid-1")
        self.assertEqual("04_session.raw", result.name)

    def test_quarantine_events_and_output_are_siblings(self):
        events = self.paths.quarantine_events(self.run_id, "aid-1")
        output = self.paths.quarantine_output(self.run_id, "aid-1")
        self.assertEqual(events.parent, output.parent)

    def test_quarantine_events_is_under_run_root(self):
        result = self.paths.quarantine_events(self.run_id, "aid-1")
        self.assertTrue(str(result).startswith(str(self.paths.run_root(self.run_id))))

    def test_quarantine_path_is_distinct_from_ordinary_invocation_path(self):
        """The same agent_id must resolve to different paths depending on
        whether it is routed through the ordinary invocation tree or the
        quarantine tree -- quarantine never aliases a mapped invocation."""
        quarantine_events = self.paths.quarantine_events(self.run_id, "aid-1")
        ordinary_events = self.paths.invocation_events(self.run_id, "aid-1")
        self.assertNotEqual(ordinary_events, quarantine_events)


class TestSanitizeComponent(unittest.TestCase):
    """sanitize_component replaces reserved/control chars, strips trailing dots and spaces."""

    def test_reserved_chars_are_replaced_with_underscore(self):
        for char in '<>:"/\\|?*':
            result = core.sanitize_component(f"name{char}value")
            self.assertEqual("name_value", result, f"Expected char {char!r} to be replaced with _")

    def test_plain_identifier_is_unchanged(self):
        self.assertEqual("RequirementsRefinement", core.sanitize_component("RequirementsRefinement"))

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
        # Dots only: all stripped, result would be empty -> "_"
        result = core.sanitize_component("...")
        self.assertEqual("_", result)

    def test_all_reserved_chars_become_underscores(self):
        result = core.sanitize_component("<>")
        self.assertEqual("__", result)

    def test_mapping_is_deterministic(self):
        name = 'Agent<Name>:value"/path\\file|here?now*done'
        first = core.sanitize_component(name)
        second = core.sanitize_component(name)
        self.assertEqual(first, second)


class TestEventSinkRouting(unittest.TestCase):
    """event_sink_for is the single routing decision: None agent_id → orchestrator,
    non-None → invocation stream. No other path construction is permitted."""

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

    def test_large_event_is_written_as_single_line(self):
        path = self.tmp_path / "events.jsonl"
        large_content = "x" * 1_000_000
        event = {"event": "big", "content": large_content}
        core.append_event(path, event)
        lines = [ln for ln in path.read_text(encoding="utf-8").splitlines() if ln.strip()]
        self.assertEqual(1, len(lines))
        self.assertEqual(large_content, json.loads(lines[0])["content"])

    def test_serialized_newlines_in_content_do_not_split_the_line(self):
        path = self.tmp_path / "events.jsonl"
        event = {"event": "turn", "content": "line one\nline two\nline three"}
        core.append_event(path, event)
        lines = [ln for ln in path.read_text(encoding="utf-8").splitlines() if ln.strip()]
        self.assertEqual(1, len(lines))

    def test_does_not_raise_on_unwritable_path(self):
        # Place a regular file at the would-be parent path position.
        # Trying to mkdir() inside a file raises NotADirectoryError on all
        # platforms, so append_event must swallow it.  This avoids relying on
        # root-owned paths (Linux-only) or driving a path outside the temp tree.
        blocker = self.tmp_path / "blocker_parent"
        blocker.write_bytes(b"")
        unwritable = blocker / "events.jsonl"
        try:
            core.append_event(unwritable, {"event": "test"})
        except Exception as e:
            self.fail(f"append_event raised unexpectedly: {e}")


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
        # A regular file placed at the parent path position causes mkstemp to
        # fail with NotADirectoryError on every platform, without depending on
        # root-owned paths or leaving directories outside the temp tree.
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
        # Same file-as-parent technique as test_returns_false_on_failure_without_raising.
        blocker = self.tmp_path / "blocker_parent_text"
        blocker.write_bytes(b"")
        result = core.atomic_replace_text(blocker / "file.json", "data")
        self.assertFalse(result)


class TestLogPathsSessionBinding(unittest.TestCase):
    """LogPaths must expose session_binding_dir and session_binding_entry,
    both hung directly off self.root -- NOT off run_root(run_id) -- so the
    binding is reachable by a firing that has not yet resolved a run id."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_session_binding_dir_is_a_direct_child_of_root(self):
        d = self.paths.session_binding_dir()
        self.assertEqual(self.paths.root, d.parent)

    def test_session_binding_dir_uses_dot_prefixed_dirname(self):
        d = self.paths.session_binding_dir()
        self.assertEqual(".session-runs", d.name)

    def test_session_binding_entry_is_inside_session_binding_dir(self):
        entry = self.paths.session_binding_entry("some-session")
        self.assertEqual(self.paths.session_binding_dir(), entry.parent)

    def test_session_binding_entry_has_jsonl_suffix(self):
        entry = self.paths.session_binding_entry("some-session")
        self.assertEqual(".jsonl", entry.suffix)

    def test_session_binding_entry_path_has_no_run_id_component(self):
        """Below the root, the path must be exactly '.session-runs/<file>' --
        no run-folder layer, since no run id is known yet at this call site."""
        entry = self.paths.session_binding_entry("some-session")
        relative = entry.relative_to(self.paths.root)
        self.assertEqual(2, len(relative.parts),
                         f"Expected '.session-runs/<file>.jsonl', got {relative}")

    def test_session_binding_entry_is_a_sibling_of_run_root_not_nested_in_it(self):
        """The binding directory must sit alongside run folders, not inside one."""
        run_root = self.paths.run_root("20260101T170000Z-ab12")
        entry = self.paths.session_binding_entry("some-session")
        self.assertFalse(str(entry).startswith(str(run_root)))

    def test_different_sessions_produce_different_entry_paths(self):
        e1 = self.paths.session_binding_entry("session-a")
        e2 = self.paths.session_binding_entry("session-b")
        self.assertNotEqual(e1, e2)

    def test_entry_filename_uses_sanitized_session_id(self):
        """A session id containing reserved filesystem characters must not
        leak those characters into the resulting path."""
        entry = self.paths.session_binding_entry('sess"ion<id>')
        self.assertNotIn('"', str(entry))
        self.assertNotIn('<', str(entry))
        self.assertNotIn('>', str(entry))
