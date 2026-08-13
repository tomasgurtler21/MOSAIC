"""Tests for the PreToolUse, PostToolUse, and PostToolUseFailure handlers.

Covers orchestrator-scoped tool call capture, subagent-scoped routing to the
owning invocation's stream, call_id resolution (native reuse and suffixed
fallback), full payload capture with no truncation, and the missing-mapping
degradation path.

All tests feed plain dicts built from the documented hook input contract;
no harness simulation is used anywhere.
"""

import datetime
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
# Shared constants and helpers
# ---------------------------------------------------------------------------

_RUN_ID = "20260101T170000Z-b5c8"
_SESSION_ID = "test-session-tools"
_TS = "2026-01-01T17:00:00.000Z"
_BASE_TIME = datetime.datetime(2026, 1, 1, 17, 0, 0, tzinfo=datetime.timezone.utc)


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


def _failure_ctx(tmp_path, **kwargs):
    return _make_ctx(tmp_path, "PostToolUseFailure", **kwargs)


def _read_jsonl(path):
    """Read all non-empty lines of a JSONL file as parsed dicts."""
    return [json.loads(ln) for ln in pathlib.Path(path).read_text("utf-8").splitlines()
            if ln.strip()]


def _register_mapping(tmp_path, run_id, agent_id, agent_instance_id, agent_type=None):
    """Pre-register an agent_id -> agent_instance_id mapping on disk."""
    paths = core.build_paths(pathlib.Path(tmp_path))
    runstate.put_agent_mapping(paths, run_id, agent_id, agent_instance_id, agent_type)


# ---------------------------------------------------------------------------
# T5.1  resolve_destination — pure routing function
# ---------------------------------------------------------------------------

class TestResolveDestination(unittest.TestCase):
    """resolve_destination returns the correct path based solely on agent_id presence.

    This is the highest-risk correctness decision in the adapter; it is
    testable as a pure path assertion with no I/O."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _ctx(self, hook_event_name="PreToolUse", **kwargs):
        return _make_ctx(self.tmp.name, hook_event_name, **kwargs)

    def test_no_agent_id_routes_to_orchestrator_events(self):
        """When agent_id is absent the destination is 00_orchestrator_events.jsonl."""
        ctx = self._ctx(tool_name="Read", tool_use_id="tu-001")
        dest = tools.resolve_destination(ctx)
        expected = self.paths.orchestrator_events(_RUN_ID)
        self.assertEqual(expected, dest)

    def test_agent_id_present_routes_to_invocation_events(self):
        """When agent_id is present the destination is the invocation's 03_events.jsonl."""
        _register_mapping(self.tmp.name, _RUN_ID, "aid-x1", "ToolAgent#1")
        ctx = self._ctx(agent_id="aid-x1", tool_name="Write", tool_use_id="tu-002")
        dest = tools.resolve_destination(ctx)
        expected = self.paths.invocation_events(_RUN_ID, "ToolAgent#1")
        self.assertEqual(expected, dest)

    def test_agent_id_present_never_routes_to_orchestrator(self):
        """A subagent-fired tool event must never land in the orchestrator stream."""
        _register_mapping(self.tmp.name, _RUN_ID, "aid-x2", "ToolAgent#2")
        ctx = self._ctx(agent_id="aid-x2", tool_name="Bash", tool_use_id="tu-003")
        dest = tools.resolve_destination(ctx)
        orchestrator_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertNotEqual(
            orchestrator_path, dest,
            "Subagent tool event must not route to the orchestrator stream",
        )

    def test_destination_contains_run_id_in_path(self):
        """The resolved destination is under the run_id directory."""
        ctx = self._ctx(tool_name="Read")
        dest = tools.resolve_destination(ctx)
        self.assertIn(_RUN_ID, str(dest))

    def test_orchestrator_destination_filename_is_00_orchestrator_events(self):
        """Orchestrator-routed events land in the canonical 00_orchestrator_events.jsonl."""
        ctx = self._ctx(tool_name="Read")
        dest = tools.resolve_destination(ctx)
        self.assertEqual("00_orchestrator_events.jsonl", dest.name)

    def test_subagent_destination_filename_is_03_events(self):
        """Subagent-routed events land in the canonical 03_events.jsonl."""
        _register_mapping(self.tmp.name, _RUN_ID, "aid-x3", "ToolAgent#3")
        ctx = self._ctx(agent_id="aid-x3", tool_name="Read")
        dest = tools.resolve_destination(ctx)
        self.assertEqual("03_events.jsonl", dest.name)

    def test_never_raises(self):
        """resolve_destination never raises regardless of input state."""
        ctx = self._ctx()
        try:
            tools.resolve_destination(ctx)
        except Exception as exc:
            self.fail(f"resolve_destination raised: {exc}")


# ---------------------------------------------------------------------------
# T5.1  handle_pre_tool_use — orchestrator-scoped tool_call_start
# ---------------------------------------------------------------------------

class TestPreToolUseOrchestratorScoped(unittest.TestCase):
    """handle_pre_tool_use emits tool_call_start to the orchestrator stream when
    no agent_id is present. Required and optional fields are correct."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _run(self, **kwargs):
        ctx = _pre_tool_ctx(self.tmp.name, **kwargs)
        tools.handle_pre_tool_use(ctx)
        return ctx

    def _orc_events(self):
        return _read_jsonl(self.paths.orchestrator_events(_RUN_ID))

    def test_emits_tool_call_start_event(self):
        """PreToolUse without agent_id emits a tool_call_start event."""
        self._run(tool_name="Read", tool_use_id="tu-100")
        events = self._orc_events()
        self.assertEqual(1, len(events))
        self.assertEqual("tool_call_start", events[0]["event"])

    def test_event_has_required_envelope_fields(self):
        """tool_call_start carries the common envelope fields."""
        self._run(tool_name="Read", tool_use_id="tu-101")
        event = self._orc_events()[0]
        for field in ("schema_version", "event", "timestamp", "harness"):
            self.assertIn(field, event, f"Required envelope field '{field}' missing")

    def test_event_has_required_call_id(self):
        """tool_call_start carries a non-empty call_id."""
        self._run(tool_name="Write", tool_use_id="tu-102")
        event = self._orc_events()[0]
        self.assertIn("call_id", event)
        self.assertTrue(event["call_id"], "call_id must be non-empty")

    def test_event_has_required_tool_name(self):
        """tool_call_start carries the tool_name."""
        self._run(tool_name="Bash", tool_use_id="tu-103")
        event = self._orc_events()[0]
        self.assertIn("tool_name", event)
        self.assertEqual("Bash", event["tool_name"])

    def test_event_has_tool_input_when_present(self):
        """tool_input is included when the payload supplies it."""
        self._run(tool_name="Read", tool_use_id="tu-104",
                  tool_input={"file_path": "/some/file.py"})
        event = self._orc_events()[0]
        self.assertIn("tool_input", event)
        self.assertEqual({"file_path": "/some/file.py"}, event["tool_input"])

    def test_tool_input_absent_when_not_in_payload(self):
        """tool_input key is absent (not null) when the payload omits it."""
        self._run(tool_name="Read", tool_use_id="tu-105")
        event = self._orc_events()[0]
        self.assertNotIn("tool_input", event,
                         "tool_input must be absent, not null, when not in payload")

    def test_event_written_to_orchestrator_not_invocation(self):
        """Orchestrator-fired PreToolUse must not write to any invocation directory."""
        self._run(tool_name="Write", tool_use_id="tu-106")
        run_root = self.paths.run_root(_RUN_ID)
        invocation_dirs = [
            d for d in run_root.iterdir()
            if d.is_dir() and not d.name.startswith(".")
        ]
        self.assertEqual(0, len(invocation_dirs),
                         "No invocation directory should be created by an orchestrator-fired hook")

    def test_no_call_index_field_ever_written(self):
        """call_index must never appear in a tool_call_start event."""
        self._run(tool_name="Bash", tool_use_id="tu-107")
        event = self._orc_events()[0]
        self.assertNotIn("call_index", event,
                         "call_index is derived downstream and must never be written here")

    def test_no_duration_ms_field_ever_written(self):
        """duration_ms must never appear in a tool_call_start event."""
        self._run(tool_name="Bash", tool_use_id="tu-108")
        event = self._orc_events()[0]
        self.assertNotIn("duration_ms", event,
                         "duration_ms is derived downstream and must never be written here")

    def test_tool_name_absent_degrades_gracefully_and_drops_key(self):
        """When tool_name is absent from the payload, the handler completes without
        raising and emits an event with no tool_name key (degrade-never-fabricate).
        The emitted event is schema-non-compliant for tool_call_start (which requires
        tool_name), but the adapter never fabricates a value for a missing field."""
        ctx = _pre_tool_ctx(self.tmp.name, tool_use_id="tu-absent-name")  # no tool_name
        try:
            tools.handle_pre_tool_use(ctx)
        except Exception as exc:
            self.fail(f"handle_pre_tool_use raised when tool_name absent: {exc}")
        events = self._orc_events()
        self.assertEqual(1, len(events),
                         "Event must still be written even when tool_name is absent")
        event = events[0]
        self.assertEqual("tool_call_start", event.get("event"),
                         "Event type must still be tool_call_start even when tool_name degrades")
        self.assertNotIn("tool_name", event,
                         "tool_name must be absent (not null) when missing from payload "
                         "— degrade-never-fabricate rule")

    def test_does_nothing_when_run_id_is_none(self):
        """No file is written when ctx.run_id is None (unresolved run)."""
        ctx = _pre_tool_ctx(self.tmp.name, tool_name="Read", tool_use_id="tu-109")
        ctx.run_id = None
        tools.handle_pre_tool_use(ctx)
        self.assertFalse(
            self.paths.orchestrator_events(_RUN_ID).exists(),
            "No file must be written when run_id is None",
        )


# ---------------------------------------------------------------------------
# T5.1  handle_post_tool_use — orchestrator-scoped tool_call_end (success)
# ---------------------------------------------------------------------------

class TestPostToolUseOrchestratorScoped(unittest.TestCase):
    """handle_post_tool_use emits tool_call_end with status 'success'."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _run(self, **kwargs):
        ctx = _post_tool_ctx(self.tmp.name, **kwargs)
        tools.handle_post_tool_use(ctx)
        return ctx

    def _orc_events(self):
        return _read_jsonl(self.paths.orchestrator_events(_RUN_ID))

    def test_emits_tool_call_end_event(self):
        """PostToolUse without agent_id emits a tool_call_end event."""
        self._run(tool_name="Read", tool_use_id="tu-200",
                  tool_output="file contents here")
        events = self._orc_events()
        self.assertEqual(1, len(events))
        self.assertEqual("tool_call_end", events[0]["event"])

    def test_status_is_success(self):
        """The status field is exactly 'success' on a PostToolUse event."""
        self._run(tool_name="Read", tool_use_id="tu-201", tool_output="output")
        event = self._orc_events()[0]
        self.assertEqual("success", event.get("status"))

    def test_event_has_required_call_id(self):
        self._run(tool_name="Write", tool_use_id="tu-202", tool_output="ok")
        event = self._orc_events()[0]
        self.assertIn("call_id", event)
        self.assertTrue(event["call_id"])

    def test_event_has_required_envelope_fields(self):
        self._run(tool_name="Read", tool_use_id="tu-203", tool_output="x")
        event = self._orc_events()[0]
        for field in ("schema_version", "event", "timestamp", "harness"):
            self.assertIn(field, event)

    def test_tool_output_present_when_in_payload(self):
        """tool_output is captured when the payload supplies it."""
        self._run(tool_name="Read", tool_use_id="tu-204", tool_output="the output")
        event = self._orc_events()[0]
        self.assertIn("tool_output", event)
        self.assertEqual("the output", event["tool_output"])

    def test_tool_output_absent_when_not_in_payload(self):
        """tool_output key is absent (not null) when the payload omits it."""
        self._run(tool_name="Read", tool_use_id="tu-205")
        event = self._orc_events()[0]
        self.assertNotIn("tool_output", event,
                         "tool_output must be absent, not null, when not in payload")

    def test_no_error_field_on_success(self):
        """A successful tool_call_end must not carry an error field."""
        self._run(tool_name="Bash", tool_use_id="tu-206", tool_output="done")
        event = self._orc_events()[0]
        self.assertNotIn("error", event,
                         "error field must not appear on a success tool_call_end")

    def test_no_call_index_ever_written(self):
        self._run(tool_name="Bash", tool_use_id="tu-207", tool_output="x")
        event = self._orc_events()[0]
        self.assertNotIn("call_index", event)

    def test_no_duration_ms_ever_written(self):
        self._run(tool_name="Bash", tool_use_id="tu-208", tool_output="x")
        event = self._orc_events()[0]
        self.assertNotIn("duration_ms", event)

    def test_does_nothing_when_run_id_is_none(self):
        ctx = _post_tool_ctx(self.tmp.name, tool_name="Read",
                             tool_use_id="tu-209", tool_output="x")
        ctx.run_id = None
        tools.handle_post_tool_use(ctx)
        self.assertFalse(self.paths.orchestrator_events(_RUN_ID).exists())


# ---------------------------------------------------------------------------
# T5.1  handle_post_tool_use_failure — orchestrator-scoped tool_call_end (error)
# ---------------------------------------------------------------------------

class TestPostToolUseFailureOrchestratorScoped(unittest.TestCase):
    """handle_post_tool_use_failure emits tool_call_end with status 'error'.
    Failure outcomes are never a distinct event type."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _run(self, **kwargs):
        ctx = _failure_ctx(self.tmp.name, **kwargs)
        tools.handle_post_tool_use_failure(ctx)
        return ctx

    def _orc_events(self):
        return _read_jsonl(self.paths.orchestrator_events(_RUN_ID))

    def test_emits_tool_call_end_not_a_distinct_error_event(self):
        """Failure results in tool_call_end, never a custom error event type."""
        self._run(tool_name="Read", tool_use_id="tu-300",
                  tool_output="Error: file not found")
        events = self._orc_events()
        self.assertEqual(1, len(events))
        self.assertEqual("tool_call_end", events[0]["event"],
                         "Failure must produce tool_call_end, not a distinct error event type")

    def test_status_is_error(self):
        """The status field is exactly 'error' on a PostToolUseFailure event."""
        self._run(tool_name="Read", tool_use_id="tu-301",
                  tool_output="Error: permission denied")
        event = self._orc_events()[0]
        self.assertEqual("error", event.get("status"))

    def test_event_has_required_call_id(self):
        self._run(tool_name="Write", tool_use_id="tu-302",
                  tool_output="Error: disk full")
        event = self._orc_events()[0]
        self.assertIn("call_id", event)
        self.assertTrue(event["call_id"])

    def test_event_has_required_envelope_fields(self):
        self._run(tool_name="Read", tool_use_id="tu-303",
                  tool_output="Error: not found")
        event = self._orc_events()[0]
        for field in ("schema_version", "event", "timestamp", "harness"):
            self.assertIn(field, event)

    def test_error_field_populated_from_dedicated_error_field_when_present(self):
        """error is populated from a dedicated 'error' payload field when present."""
        self._run(tool_name="Bash", tool_use_id="tu-304",
                  error="Command not found: foobar",
                  tool_output="Command not found: foobar")
        event = self._orc_events()[0]
        self.assertEqual("Command not found: foobar", event.get("error"))

    def test_error_field_populated_from_tool_output_when_no_dedicated_error_field(self):
        """When no dedicated error field, error falls back to the string form of tool_output."""
        self._run(tool_name="Read", tool_use_id="tu-305",
                  tool_output="Error: file not found at /nonexistent")
        event = self._orc_events()[0]
        self.assertIn("error", event,
                      "error must be populated from tool_output when no dedicated error field")

    def test_error_field_absent_when_no_detail_available(self):
        """When neither error field nor tool_output is present, error is omitted."""
        self._run(tool_name="Read", tool_use_id="tu-306")
        event = self._orc_events()[0]
        self.assertNotIn("error", event,
                         "error must be absent (not null) when no detail is available")

    def test_tool_output_present_when_in_payload(self):
        self._run(tool_name="Bash", tool_use_id="tu-307",
                  tool_output="stderr: something went wrong")
        event = self._orc_events()[0]
        self.assertIn("tool_output", event)

    def test_no_call_index_ever_written(self):
        self._run(tool_name="Bash", tool_use_id="tu-308",
                  tool_output="Error: timeout")
        event = self._orc_events()[0]
        self.assertNotIn("call_index", event)

    def test_no_duration_ms_ever_written(self):
        self._run(tool_name="Bash", tool_use_id="tu-309",
                  tool_output="Error: timeout")
        event = self._orc_events()[0]
        self.assertNotIn("duration_ms", event)

    def test_does_nothing_when_run_id_is_none(self):
        ctx = _failure_ctx(self.tmp.name, tool_name="Read",
                           tool_use_id="tu-310", tool_output="err")
        ctx.run_id = None
        tools.handle_post_tool_use_failure(ctx)
        self.assertFalse(self.paths.orchestrator_events(_RUN_ID).exists())


# ---------------------------------------------------------------------------
# T5.2  Subagent-scoped routing
# ---------------------------------------------------------------------------

class TestSubagentScopedRouting(unittest.TestCase):
    """When agent_id is present tool events land in the owning invocation's
    03_events.jsonl and NEVER in the orchestrator stream."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _register(self, agent_id, agent_instance_id, agent_type=None):
        _register_mapping(self.tmp.name, _RUN_ID, agent_id, agent_instance_id, agent_type)

    def _invocation_events(self, agent_instance_id):
        return _read_jsonl(self.paths.invocation_events(_RUN_ID, agent_instance_id))

    def test_pre_tool_use_routes_to_invocation_stream(self):
        """PreToolUse with agent_id routes to the invocation's 03_events.jsonl."""
        self._register("aid-s1", "SubAgent#1", "SubAgent")
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-s1",
                            tool_name="Read", tool_use_id="tu-400")
        tools.handle_pre_tool_use(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "SubAgent#1")
        self.assertTrue(event_path.exists())
        events = _read_jsonl(event_path)
        self.assertEqual(1, len(events))
        self.assertEqual("tool_call_start", events[0]["event"])

    def test_post_tool_use_routes_to_invocation_stream(self):
        """PostToolUse with agent_id routes to the invocation's 03_events.jsonl."""
        self._register("aid-s2", "SubAgent#2", "SubAgent")
        ctx = _post_tool_ctx(self.tmp.name, agent_id="aid-s2",
                             tool_name="Write", tool_use_id="tu-401",
                             tool_output="written")
        tools.handle_post_tool_use(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "SubAgent#2")
        self.assertTrue(event_path.exists())
        events = _read_jsonl(event_path)
        self.assertEqual("tool_call_end", events[0]["event"])

    def test_post_tool_use_failure_routes_to_invocation_stream(self):
        """PostToolUseFailure with agent_id routes to the invocation's 03_events.jsonl."""
        self._register("aid-s3", "SubAgent#3", "SubAgent")
        ctx = _failure_ctx(self.tmp.name, agent_id="aid-s3",
                           tool_name="Bash", tool_use_id="tu-402",
                           tool_output="Error: exit 1")
        tools.handle_post_tool_use_failure(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "SubAgent#3")
        self.assertTrue(event_path.exists())

    def test_subagent_pre_tool_use_does_not_write_to_orchestrator_stream(self):
        """Subagent-fired PreToolUse must never write to 00_orchestrator_events.jsonl."""
        self._register("aid-s4", "SubAgent#4", "SubAgent")
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-s4",
                            tool_name="Read", tool_use_id="tu-403")
        tools.handle_pre_tool_use(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertFalse(
            orc_path.exists(),
            "Subagent-fired PreToolUse must not touch the orchestrator event stream",
        )

    def test_subagent_post_tool_use_does_not_write_to_orchestrator_stream(self):
        """Subagent-fired PostToolUse must never write to 00_orchestrator_events.jsonl."""
        self._register("aid-s5", "SubAgent#5", "SubAgent")
        ctx = _post_tool_ctx(self.tmp.name, agent_id="aid-s5",
                             tool_name="Write", tool_use_id="tu-404",
                             tool_output="ok")
        tools.handle_post_tool_use(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertFalse(orc_path.exists(),
                         "Subagent-fired PostToolUse must not touch the orchestrator stream")

    def test_subagent_failure_does_not_write_to_orchestrator_stream(self):
        """Subagent-fired PostToolUseFailure must never write to the orchestrator stream."""
        self._register("aid-s6", "SubAgent#6", "SubAgent")
        ctx = _failure_ctx(self.tmp.name, agent_id="aid-s6",
                           tool_name="Bash", tool_use_id="tu-405",
                           tool_output="Error: failure")
        tools.handle_post_tool_use_failure(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertFalse(orc_path.exists(),
                         "Subagent-fired PostToolUseFailure must not touch the orchestrator stream")

    def test_invocation_event_has_correct_schema_version_and_harness(self):
        """Events written to the invocation stream carry the standard envelope."""
        self._register("aid-s7", "SubAgent#7", "SubAgent")
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-s7",
                            tool_name="Read", tool_use_id="tu-406")
        tools.handle_pre_tool_use(ctx)
        events = self._invocation_events("SubAgent#7")
        event = events[0]
        self.assertEqual("1.1.0", event.get("schema_version"))
        self.assertEqual("claude-code", event.get("harness"))

    def test_orchestrator_events_and_subagent_events_are_independent(self):
        """Orchestrator and subagent can both fire tool calls; each goes to its own stream."""
        self._register("aid-s8", "SubAgent#8", "SubAgent")

        # Orchestrator fires PreToolUse (no agent_id)
        orc_ctx = _pre_tool_ctx(self.tmp.name, tool_name="Read", tool_use_id="orc-tu-1")
        tools.handle_pre_tool_use(orc_ctx)

        # Subagent fires PreToolUse (with agent_id)
        sub_ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-s8",
                                tool_name="Write", tool_use_id="sub-tu-1")
        tools.handle_pre_tool_use(sub_ctx)

        orc_events = _read_jsonl(self.paths.orchestrator_events(_RUN_ID))
        sub_events = self._invocation_events("SubAgent#8")

        self.assertEqual(1, len(orc_events), "Orchestrator stream must have exactly 1 event")
        self.assertEqual(1, len(sub_events), "Subagent stream must have exactly 1 event")
        self.assertEqual("orc-tu-1", orc_events[0]["call_id"])
        self.assertEqual("sub-tu-1", sub_events[0]["call_id"])

    def test_subagent_tool_events_captured_without_reading_any_transcript(self):
        """The handler must write the event without any agent_transcript_path in the payload.
        This confirms live capture with no transcript reconstruction."""
        self._register("aid-s9", "SubAgent#9", "SubAgent")
        # Intentionally no transcript_path or agent_transcript_path in payload
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-s9",
                            tool_name="Bash", tool_use_id="tu-live-capture")
        tools.handle_pre_tool_use(ctx)
        event_path = self.paths.invocation_events(_RUN_ID, "SubAgent#9")
        self.assertTrue(event_path.exists(),
                        "Subagent tool event must be captured live, no transcript needed")


# ---------------------------------------------------------------------------
# T5.3  call_id resolution — native reuse and suffixed fallback
# ---------------------------------------------------------------------------

class TestCallIdResolution(unittest.TestCase):
    """resolve_call_id returns the harness-native tool_use_id when present.
    Only when genuinely absent does it produce a suffixed fallback."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()

    def tearDown(self):
        self.tmp.cleanup()

    def _ctx(self, **kwargs):
        return _pre_tool_ctx(self.tmp.name, **kwargs)

    def test_uses_native_tool_use_id_when_present(self):
        """Native tool_use_id is returned unchanged as call_id."""
        ctx = self._ctx(tool_name="Read", tool_use_id="toolu_native_abc123")
        call_id = tools.resolve_call_id(ctx)
        self.assertEqual("toolu_native_abc123", call_id)

    def test_native_id_preserved_exactly_including_special_chars(self):
        """Native ID is passed through literally, including any harness-specific format."""
        ctx = self._ctx(tool_name="Write", tool_use_id="toolu_01AbCdEf_2024")
        call_id = tools.resolve_call_id(ctx)
        self.assertEqual("toolu_01AbCdEf_2024", call_id)

    def test_fallback_used_when_tool_use_id_absent(self):
        """When tool_use_id is absent, a fallback call_id is generated."""
        ctx = self._ctx(tool_name="Read")
        call_id = tools.resolve_call_id(ctx)
        self.assertIsNotNone(call_id)
        self.assertTrue(call_id, "Fallback call_id must be non-empty")

    def test_empty_string_tool_use_id_triggers_fallback(self):
        """tool_use_id='' is normalized to None by HookContext.field(), triggering the fallback.
        The harness is unlikely to send an empty string, but the normalization contract
        guarantees that empty string behaves identically to absent — the fallback activates."""
        ctx = self._ctx(tool_name="Read", tool_use_id="")
        call_id = tools.resolve_call_id(ctx)
        self.assertNotEqual("", call_id,
                            "Empty string must not be passed through as call_id")
        self.assertTrue(call_id.startswith("Read_"),
                        f"Expected fallback with 'Read_' prefix on empty tool_use_id, "
                        f"got {call_id!r}")

    def test_fallback_includes_tool_name_as_prefix(self):
        """Fallback call_id starts with the tool_name."""
        ctx = self._ctx(tool_name="Bash")
        call_id = tools.resolve_call_id(ctx)
        self.assertTrue(call_id.startswith("Bash_"),
                        f"Expected 'Bash_' prefix, got {call_id!r}")

    def test_fallback_matches_documented_format(self):
        """Fallback call_id follows the {tool_name}_{YYYYMMDD}T{HHMMSS}Z-{4hex} format."""
        import re
        ctx = self._ctx(tool_name="Read")
        call_id = tools.resolve_call_id(ctx)
        self.assertRegex(call_id,
                         r"^Read_[0-9]{8}T[0-9]{6}Z-[0-9a-f]{4}$")

    def test_fallback_has_four_char_hex_suffix(self):
        """The random suffix in the fallback is exactly 4 lowercase hex characters."""
        import re
        ctx = self._ctx(tool_name="Write")
        call_id = tools.resolve_call_id(ctx)
        suffix = call_id.split("-")[-1]
        self.assertEqual(4, len(suffix))
        self.assertRegex(suffix, r"^[0-9a-f]{4}$")

    def test_same_tool_parallel_batch_produces_distinct_call_ids(self):
        """All 20 fallback call_ids for the same tool in a parallel batch must be unique.
        This guards against the realistic collision case where several calls to the
        same tool are dispatched in a parallel batch at millisecond-close timing.
        Asserting the full count (not just > 1) catches a partially-broken suffix
        implementation that might produce only 2 distinct values across 20 calls."""
        call_ids = set()
        for _ in range(20):
            ctx = self._ctx(tool_name="Bash")
            call_ids.add(tools.resolve_call_id(ctx))
        self.assertEqual(
            20, len(call_ids),
            "All 20 fallback call_ids for the same tool must be distinct (random suffix required)",
        )

    def test_native_id_used_in_emitted_event(self):
        """The emitted tool_call_start event carries the native tool_use_id as call_id."""
        paths = core.build_paths(pathlib.Path(self.tmp.name))
        ctx = _pre_tool_ctx(self.tmp.name, tool_name="Read",
                            tool_use_id="toolu_exact_native_id")
        tools.handle_pre_tool_use(ctx)
        events = _read_jsonl(paths.orchestrator_events(_RUN_ID))
        self.assertEqual("toolu_exact_native_id", events[0]["call_id"])

    def test_pre_tool_and_post_tool_share_call_id_via_native_id(self):
        """PreToolUse and PostToolUse for the same native tool_use_id produce the same call_id,
        enabling downstream pairing of start and end events."""
        paths = core.build_paths(pathlib.Path(self.tmp.name))
        shared_id = "toolu_shared_correlation_xyz"

        ctx_start = _pre_tool_ctx(self.tmp.name, tool_name="Read",
                                   tool_use_id=shared_id)
        tools.handle_pre_tool_use(ctx_start)

        ctx_end = _post_tool_ctx(self.tmp.name, tool_name="Read",
                                  tool_use_id=shared_id, tool_output="result")
        tools.handle_post_tool_use(ctx_end)

        events = _read_jsonl(paths.orchestrator_events(_RUN_ID))
        self.assertEqual(2, len(events))
        self.assertEqual(events[0]["call_id"], events[1]["call_id"],
                         "start and end events must share the same call_id for downstream pairing")

    def test_fallback_uses_tool_literal_when_tool_name_absent(self):
        """When both tool_use_id and tool_name are absent, fallback uses 'tool' as prefix."""
        ctx = self._ctx()  # no tool_name, no tool_use_id
        call_id = tools.resolve_call_id(ctx)
        self.assertTrue(call_id.startswith("tool_"),
                        f"Expected 'tool_' prefix when tool_name absent, got {call_id!r}")


# ---------------------------------------------------------------------------
# T5.4  Full payload capture with no truncation
# ---------------------------------------------------------------------------

class TestFullPayloadCapture(unittest.TestCase):
    """tool_input and tool_output are captured in full with no size cap and no
    truncation marker. Per-invocation JSONL files may reach tens of megabytes;
    that is accepted and expected."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _orc_events(self):
        return _read_jsonl(self.paths.orchestrator_events(_RUN_ID))

    def test_large_tool_input_captured_in_full(self):
        """tool_input with large content is written entirely, without truncation."""
        large_input = {"content": "x" * 100_000}
        ctx = _pre_tool_ctx(self.tmp.name, tool_name="Write",
                            tool_use_id="tu-500", tool_input=large_input)
        tools.handle_pre_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertIn("tool_input", event)
        self.assertEqual(100_000, len(event["tool_input"]["content"]),
                         "tool_input must be captured in full with no size cap")

    def test_large_tool_output_captured_in_full(self):
        """tool_output with large content is written entirely, without truncation."""
        large_output = "y" * 100_000
        ctx = _post_tool_ctx(self.tmp.name, tool_name="Read",
                             tool_use_id="tu-501", tool_output=large_output)
        tools.handle_post_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertIn("tool_output", event)
        self.assertEqual(100_000, len(event["tool_output"]),
                         "tool_output must be captured in full with no size cap")

    def test_large_failure_output_captured_in_full(self):
        """tool_output on a failure event is captured entirely, without truncation."""
        large_error_output = "Error details: " + "z" * 50_000
        ctx = _failure_ctx(self.tmp.name, tool_name="Bash",
                           tool_use_id="tu-502", tool_output=large_error_output)
        tools.handle_post_tool_use_failure(ctx)
        event = self._orc_events()[0]
        self.assertIn("tool_output", event)
        self.assertEqual(len(large_error_output), len(event["tool_output"]),
                         "Failure tool_output must be captured in full")

    def test_no_truncation_marker_in_tool_input(self):
        """No truncation marker (such as '...' or '[truncated]') appears in tool_input."""
        large_input = {"data": "a" * 100_000}
        ctx = _pre_tool_ctx(self.tmp.name, tool_name="Write",
                            tool_use_id="tu-503", tool_input=large_input)
        tools.handle_pre_tool_use(ctx)
        event = self._orc_events()[0]
        raw_json = json.dumps(event["tool_input"])
        self.assertNotIn("[truncated]", raw_json)
        self.assertNotIn("...", raw_json)

    def test_no_truncation_marker_in_tool_output(self):
        """No truncation marker appears in tool_output."""
        large_output = "b" * 100_000
        ctx = _post_tool_ctx(self.tmp.name, tool_name="Read",
                             tool_use_id="tu-504", tool_output=large_output)
        tools.handle_post_tool_use(ctx)
        event = self._orc_events()[0]
        self.assertNotIn("[truncated]", event["tool_output"])

    def test_single_jsonl_line_per_event_even_at_large_size(self):
        """Even a large payload produces exactly one JSONL line (newlines are JSON-escaped)."""
        large_input = {"content": "content\nwith\nnewlines\n" * 5_000}
        ctx = _pre_tool_ctx(self.tmp.name, tool_name="Write",
                            tool_use_id="tu-505", tool_input=large_input)
        tools.handle_pre_tool_use(ctx)
        raw_text = self.paths.orchestrator_events(_RUN_ID).read_text("utf-8")
        non_empty_lines = [ln for ln in raw_text.splitlines() if ln.strip()]
        self.assertEqual(1, len(non_empty_lines),
                         "A single event must produce exactly one JSONL line")


# ---------------------------------------------------------------------------
# T5.5  Missing-mapping degradation
# ---------------------------------------------------------------------------

class TestMissingMappingDegradation(unittest.TestCase):
    """When agent_id is present but no mapping exists in the store, the handler
    must route to a deterministic fallback folder derived from agent_id.
    It must NEVER route to the orchestrator stream and must NEVER raise."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_pre_tool_use_does_not_raise_on_missing_mapping(self):
        """handle_pre_tool_use never raises when the agent mapping is absent."""
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="unmapped-agent-001",
                            tool_name="Read", tool_use_id="tu-600")
        try:
            tools.handle_pre_tool_use(ctx)
        except Exception as exc:
            self.fail(f"handle_pre_tool_use raised on missing mapping: {exc}")

    def test_post_tool_use_does_not_raise_on_missing_mapping(self):
        """handle_post_tool_use never raises when the agent mapping is absent."""
        ctx = _post_tool_ctx(self.tmp.name, agent_id="unmapped-agent-002",
                             tool_name="Write", tool_use_id="tu-601", tool_output="ok")
        try:
            tools.handle_post_tool_use(ctx)
        except Exception as exc:
            self.fail(f"handle_post_tool_use raised on missing mapping: {exc}")

    def test_failure_handler_does_not_raise_on_missing_mapping(self):
        """handle_post_tool_use_failure never raises when the agent mapping is absent."""
        ctx = _failure_ctx(self.tmp.name, agent_id="unmapped-agent-003",
                           tool_name="Bash", tool_use_id="tu-602",
                           tool_output="Error: failure")
        try:
            tools.handle_post_tool_use_failure(ctx)
        except Exception as exc:
            self.fail(f"handle_post_tool_use_failure raised on missing mapping: {exc}")

    def test_missing_mapping_does_not_write_to_orchestrator_stream(self):
        """When mapping is missing, the event must not land in 00_orchestrator_events.jsonl.
        Misattributing a subagent's events to the orchestrator corrupts actor attribution."""
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="unmapped-agent-004",
                            tool_name="Read", tool_use_id="tu-603")
        tools.handle_pre_tool_use(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        self.assertFalse(
            orc_path.exists(),
            "A missing-mapping subagent event must never be written to the orchestrator stream",
        )

    def test_missing_mapping_routes_to_unmapped_prefix_folder(self):
        """Events from an unmapped agent_id land in 'unmapped_{agent_id}/03_events.jsonl'."""
        agent_id = "unmapped-agent-005"
        ctx = _pre_tool_ctx(self.tmp.name, agent_id=agent_id,
                            tool_name="Read", tool_use_id="tu-604")
        tools.handle_pre_tool_use(ctx)
        expected_folder = f"unmapped_{agent_id}"
        expected_path = self.paths.run_root(_RUN_ID) / expected_folder / "03_events.jsonl"
        self.assertTrue(
            expected_path.exists(),
            f"Missing-mapping event must be routed to {expected_folder}/03_events.jsonl",
        )

    def test_missing_mapping_destination_is_deterministic(self):
        """Multiple events from the same unmapped agent_id land in the same folder.
        Determinism ensures all events from one unmapped invocation stay together."""
        agent_id = "unmapped-agent-006"
        for i in range(3):
            ctx = _pre_tool_ctx(self.tmp.name, agent_id=agent_id,
                                tool_name="Read", tool_use_id=f"tu-605-{i}")
            tools.handle_pre_tool_use(ctx)

        expected_folder = f"unmapped_{agent_id}"
        expected_path = self.paths.run_root(_RUN_ID) / expected_folder / "03_events.jsonl"
        events = _read_jsonl(expected_path)
        self.assertEqual(3, len(events),
                         "All events from one unmapped agent_id must land in the same folder")

    def test_two_different_unmapped_agents_land_in_different_folders(self):
        """Two distinct unmapped agent_ids route to their own separate folders."""
        for i in range(1, 3):
            ctx = _pre_tool_ctx(self.tmp.name, agent_id=f"unmapped-distinct-{i}",
                                tool_name="Read", tool_use_id=f"tu-606-{i}")
            tools.handle_pre_tool_use(ctx)

        folder1 = self.paths.run_root(_RUN_ID) / "unmapped_unmapped-distinct-1"
        folder2 = self.paths.run_root(_RUN_ID) / "unmapped_unmapped-distinct-2"
        self.assertTrue(folder1.exists(), "First unmapped agent must have its own folder")
        self.assertTrue(folder2.exists(), "Second unmapped agent must have its own folder")
        self.assertNotEqual(folder1, folder2)

    def test_event_is_still_valid_jsonl_with_missing_mapping(self):
        """The event written to the fallback folder is a valid JSON object."""
        agent_id = "unmapped-agent-007"
        ctx = _pre_tool_ctx(self.tmp.name, agent_id=agent_id,
                            tool_name="Bash", tool_use_id="tu-607")
        tools.handle_pre_tool_use(ctx)
        expected_path = self.paths.run_root(_RUN_ID) / f"unmapped_{agent_id}" / "03_events.jsonl"
        events = _read_jsonl(expected_path)
        self.assertEqual(1, len(events))
        event = events[0]
        self.assertEqual("tool_call_start", event["event"])
        self.assertIn("call_id", event)

    def test_resolve_invocation_id_never_returns_none_for_unmapped(self):
        """resolve_invocation_id always returns a non-None string, even for unmapped agents."""
        paths = core.build_paths(pathlib.Path(self.tmp.name))
        result = runstate.resolve_invocation_id(paths, _RUN_ID, "some-never-seen-agent-id")
        self.assertIsNotNone(result, "resolve_invocation_id must never return None")
        self.assertTrue(result, "resolve_invocation_id must return a non-empty string")

    def test_resolve_invocation_id_fallback_contains_agent_id(self):
        """The deterministic fallback ID is derived from agent_id so it is traceable."""
        paths = core.build_paths(pathlib.Path(self.tmp.name))
        agent_id = "traceable-unmapped-id"
        result = runstate.resolve_invocation_id(paths, _RUN_ID, agent_id)
        self.assertIn(agent_id, result,
                      "Fallback ID must contain agent_id for traceability")


# ---------------------------------------------------------------------------
# Tool-name gate: both "Agent" and "Task" must trigger dispatch capture
# ---------------------------------------------------------------------------

class TestPreToolUseToolNameGate(unittest.TestCase):
    """handle_pre_tool_use must capture a pending dispatch for tool_name='Task'
    as well as tool_name='Agent'. All other tool names must leave the
    pending-dispatch queue untouched.

    Tests that verify 'Task' capture will FAIL until the gate is widened from
    `if tool_name == 'Agent':` to `if tool_name in ('Agent', 'Task'):`.
    """

    # A realistic dispatch JSON containing an extractable agent_instance_id and run_id.
    _DISPATCH_PROMPT = (
        '{"agent_instance_id": "test-writer-tdd#6", '
        '"run_id": "20260731T141500Z-e5a2", '
        '"task_description": "Write tests."}'
    )

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _dispatch_tool_ctx(self, tool_name, tool_use_id="tu-gate-001"):
        """Build a PreToolUse context carrying the dispatch prompt in tool_input."""
        return _make_ctx(
            self.tmp.name, "PreToolUse",
            run_id=None,
            tool_name=tool_name,
            tool_use_id=tool_use_id,
            tool_input={"prompt": self._DISPATCH_PROMPT},
        )

    def _read_queue(self):
        """Read all pending-dispatch queue entries for the default session."""
        entry_path = self.paths.pending_dispatch_entry(_SESSION_ID)
        if not entry_path.exists():
            return []
        return [
            json.loads(ln)
            for ln in entry_path.read_text("utf-8").splitlines()
            if ln.strip()
        ]

    # --- Task capture (RED until gate is widened) ---

    def test_task_tool_name_creates_pending_dispatch_entry(self):
        """Dispatching with tool_name='Task' must create a pending dispatch entry.

        Currently FAILS because the gate only matches tool_name='Agent'.
        After the fix the gate must accept both 'Agent' and 'Task'.
        """
        ctx = self._dispatch_tool_ctx("Task")
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(
            1, len(entries),
            "tool_name='Task' must create exactly one pending dispatch entry",
        )

    def test_task_tool_name_extracts_agent_instance_id_from_prompt(self):
        """The pending dispatch created for tool_name='Task' must carry the
        agent_instance_id extracted from tool_input.prompt.

        Currently FAILS because the 'Task' branch is not entered.
        """
        ctx = self._dispatch_tool_ctx("Task")
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(1, len(entries))
        self.assertEqual(
            "test-writer-tdd#6", entries[0]["agent_instance_id"],
            "agent_instance_id must be extracted from the dispatch prompt",
        )

    def test_task_tool_name_extracts_run_id_from_prompt(self):
        """The pending dispatch created for tool_name='Task' must carry the
        run_id extracted from tool_input.prompt.

        Currently FAILS because the 'Task' branch is not entered.
        """
        ctx = self._dispatch_tool_ctx("Task")
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(1, len(entries))
        self.assertEqual(
            "20260731T141500Z-e5a2", entries[0]["run_id"],
            "run_id must be extracted from the dispatch prompt",
        )

    # --- Regression guard: 'Agent' must still work ---

    def test_agent_tool_name_still_creates_pending_dispatch(self):
        """tool_name='Agent' must still create a pending dispatch (regression guard)."""
        ctx = self._dispatch_tool_ctx("Agent")
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(
            1, len(entries),
            "tool_name='Agent' must still create a pending dispatch entry",
        )

    # --- Gate must not fire for non-dispatch tool names ---

    def test_read_tool_name_does_not_create_pending_dispatch(self):
        """tool_name='Read' must not create any pending dispatch entry."""
        ctx = _make_ctx(
            self.tmp.name, "PreToolUse",
            run_id=None,
            tool_name="Read",
            tool_input={"file_path": "/some/file.py"},
        )
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(
            0, len(entries),
            "tool_name='Read' must never create a pending dispatch",
        )

    def test_bash_tool_name_does_not_create_pending_dispatch(self):
        """tool_name='Bash' must not create any pending dispatch entry."""
        ctx = _make_ctx(
            self.tmp.name, "PreToolUse",
            run_id=None,
            tool_name="Bash",
            tool_input={"command": "echo hello"},
        )
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(
            0, len(entries),
            "tool_name='Bash' must never create a pending dispatch",
        )

    def test_write_tool_name_does_not_create_pending_dispatch(self):
        """tool_name='Write' must not create any pending dispatch entry."""
        ctx = _make_ctx(
            self.tmp.name, "PreToolUse",
            run_id=None,
            tool_name="Write",
            tool_input={"file_path": "/f", "content": "x"},
        )
        tools.handle_pre_tool_use(ctx)
        entries = self._read_queue()
        self.assertEqual(
            0, len(entries),
            "tool_name='Write' must never create a pending dispatch",
        )

    # --- Verify tool_call_start is always emitted regardless of gate ---

    def test_task_tool_name_still_emits_tool_call_start(self):
        """tool_call_start must always be emitted, even when tool_name='Task'.

        The dispatch capture branch must never suppress the primary event.
        """
        ctx = _make_ctx(
            self.tmp.name, "PreToolUse",
            run_id=None,
            tool_name="Task",
            tool_use_id="tu-task-event-check",
            tool_input={"prompt": self._DISPATCH_PROMPT},
        )
        tools.handle_pre_tool_use(ctx)
        orc_path = self.paths.orchestrator_events("unknown-run")
        self.assertTrue(
            orc_path.exists(),
            "tool_call_start must be written for tool_name='Task'",
        )
        events = [
            json.loads(ln)
            for ln in orc_path.read_text("utf-8").splitlines()
            if ln.strip()
        ]
        event_names = [e["event"] for e in events]
        self.assertIn("tool_call_start", event_names,
                      "tool_call_start must appear in orchestrator events for 'Task' tool")

    def test_never_raises_for_task_tool(self):
        """handle_pre_tool_use must never raise when tool_name='Task'."""
        ctx = self._dispatch_tool_ctx("Task")
        try:
            tools.handle_pre_tool_use(ctx)
        except Exception as exc:
            self.fail(f"handle_pre_tool_use raised for tool_name='Task': {exc}")


# ---------------------------------------------------------------------------
# T3.1 / T3.2  resolve_destination — agent-map miss diagnostic
# ---------------------------------------------------------------------------

class TestResolveDestinationAgentMapDiagnostic(unittest.TestCase):
    """resolve_destination routes through runstate.resolve_invocation, so an
    agent-map miss on a tool event emits the 'agent-map: miss' diagnostic --
    the same miss path exercised by SubagentStop, now covered for the
    high-frequency tool route."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))
        self._orig_debug = os.environ.get("MOSAIC_LOGGER_DEBUG")

    def tearDown(self):
        self.tmp.cleanup()
        if self._orig_debug is None:
            os.environ.pop("MOSAIC_LOGGER_DEBUG", None)
        else:
            os.environ["MOSAIC_LOGGER_DEBUG"] = self._orig_debug

    def test_missing_mapping_emits_agent_map_miss_diagnostic(self):
        debug_file = pathlib.Path(self.tmp.name) / "debug.log"
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-tool-miss-1",
                            tool_name="Read", tool_use_id="tu-miss-1")
        tools.resolve_destination(ctx)
        self.assertTrue(debug_file.exists(), "Diagnostic file must be written on a miss")
        text = debug_file.read_text(encoding="utf-8")
        self.assertIn("agent-map: miss", text)
        self.assertIn("aid-tool-miss-1", text)

    def test_present_mapping_emits_no_miss_diagnostic(self):
        debug_file = pathlib.Path(self.tmp.name) / "debug.log"
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        _register_mapping(self.tmp.name, _RUN_ID, "aid-tool-hit-1", "ToolAgent#9")
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-tool-hit-1",
                            tool_name="Read", tool_use_id="tu-hit-1")
        tools.resolve_destination(ctx)
        if debug_file.exists():
            text = debug_file.read_text(encoding="utf-8")
            self.assertNotIn("agent-map: miss", text)

    def test_no_agent_id_emits_no_agent_map_diagnostic(self):
        """Orchestrator-scoped events never touch the agent map at all."""
        debug_file = pathlib.Path(self.tmp.name) / "debug.log"
        os.environ["MOSAIC_LOGGER_DEBUG"] = str(debug_file)
        ctx = _pre_tool_ctx(self.tmp.name, tool_name="Read", tool_use_id="tu-noagent-1")
        tools.resolve_destination(ctx)
        if debug_file.exists():
            text = debug_file.read_text(encoding="utf-8")
            self.assertNotIn("agent-map: miss", text)

    def test_still_routes_to_unmapped_folder_despite_diagnostic(self):
        """Emitting the diagnostic must not change the routing decision --
        the destination is still the deterministic unmapped_ folder."""
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-tool-miss-2",
                            tool_name="Read", tool_use_id="tu-miss-2")
        dest = tools.resolve_destination(ctx)
        self.assertEqual(
            self.paths.invocation_events(_RUN_ID, "unmapped_aid-tool-miss-2"), dest
        )


# ---------------------------------------------------------------------------
# T5.3  Tool-firing usage_record emission, gated by tool_capture_enabled()
# ---------------------------------------------------------------------------

def _write_tool_transcript(dir_path, records):
    """Write a minimal transcript JSONL for the tool-firing tests."""
    dir_path = pathlib.Path(dir_path)
    dir_path.mkdir(parents=True, exist_ok=True)
    path = dir_path / "tool_transcript.jsonl"
    with open(path, "w", encoding="utf-8") as fh:
        for record in records:
            fh.write(json.dumps(record) + "\n")
    return str(path)


class TestToolFiringUsageEmission(unittest.TestCase):
    """PreToolUse/PostToolUse/PostToolUseFailure emit usage_record from
    ctx.transcript_path, routed to the same destination as the tool event
    itself, but only when tool_capture_enabled() allows it -- the deliberate
    performance-sensitive reversal of the 'no transcript reads' property on
    this high-frequency path."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))
        self._orig_capture = os.environ.get("MOSAIC_LOGGER_USAGE_CAPTURE")

    def tearDown(self):
        self.tmp.cleanup()
        if self._orig_capture is None:
            os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        else:
            os.environ["MOSAIC_LOGGER_USAGE_CAPTURE"] = self._orig_capture

    def _transcript(self):
        return _write_tool_transcript(self.tmp.name, [
            {"type": "assistant", "message": {
                "id": "msg_tool", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
            }},
        ])

    def test_pre_tool_use_emits_usage_record_by_default(self):
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        ctx = _pre_tool_ctx(self.tmp.name, tool_name="Read", tool_use_id="tu-usage-1",
                            transcript_path=self._transcript())
        tools.handle_pre_tool_use(ctx)
        events = _read_jsonl(self.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events))

    def test_post_tool_use_emits_usage_record_by_default(self):
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        ctx = _post_tool_ctx(self.tmp.name, tool_name="Read", tool_use_id="tu-usage-2",
                             transcript_path=self._transcript())
        tools.handle_post_tool_use(ctx)
        events = _read_jsonl(self.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events))

    def test_post_tool_use_failure_emits_usage_record_by_default(self):
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        ctx = _failure_ctx(self.tmp.name, tool_name="Bash", tool_use_id="tu-usage-3",
                           transcript_path=self._transcript())
        tools.handle_post_tool_use_failure(ctx)
        events = _read_jsonl(self.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events))

    def test_no_usage_record_when_capture_mode_is_boundaries(self):
        os.environ["MOSAIC_LOGGER_USAGE_CAPTURE"] = "boundaries"
        ctx = _pre_tool_ctx(self.tmp.name, tool_name="Read", tool_use_id="tu-usage-4",
                            transcript_path=self._transcript())
        tools.handle_pre_tool_use(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        events = _read_jsonl(orc_path) if orc_path.exists() else []
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events))

    def test_tool_call_start_still_emitted_when_capture_disabled(self):
        """Gating the usage_record must never suppress the tool event itself."""
        os.environ["MOSAIC_LOGGER_USAGE_CAPTURE"] = "boundaries"
        ctx = _pre_tool_ctx(self.tmp.name, tool_name="Read", tool_use_id="tu-usage-5",
                            transcript_path=self._transcript())
        tools.handle_pre_tool_use(ctx)
        events = _read_jsonl(self.paths.orchestrator_events(_RUN_ID))
        self.assertIn("tool_call_start", [e["event"] for e in events])

    def test_usage_record_routes_with_the_tool_event_for_subagent_scope(self):
        """A subagent-scoped tool firing must NOT emit a usage_record to any stream.
        Tool events are still emitted and still routed to the invocation stream,
        but usage emission is suppressed entirely when ctx.agent_id is set.
        The subagent-stop path is the sole source of a subagent's own usage records."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        _register_mapping(self.tmp.name, _RUN_ID, "aid-usage-6", "ToolAgent#6")
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-usage-6",
                            tool_name="Read", tool_use_id="tu-usage-6",
                            transcript_path=self._transcript())
        tools.handle_pre_tool_use(ctx)
        # No usage_record must appear on the invocation stream
        invocation_events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "ToolAgent#6"))
        usage_events = [e for e in invocation_events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events),
                         "Subagent-scoped tool firing must not write usage_record to the invocation stream")
        # No usage_record must appear on the orchestrator stream either
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        orc_events = _read_jsonl(orc_path) if orc_path.exists() else []
        orc_usage = [e for e in orc_events if e["event"] == "usage_record"]
        self.assertEqual(0, len(orc_usage),
                         "Subagent-scoped tool firing must not write usage_record to the orchestrator stream")
        # The tool event itself must still be written to the invocation stream
        tool_events = [e for e in invocation_events if e["event"] == "tool_call_start"]
        self.assertEqual(1, len(tool_events),
                         "Tool event must still be emitted to the invocation stream in subagent scope")

    def test_no_usage_record_when_transcript_path_absent(self):
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        ctx = _pre_tool_ctx(self.tmp.name, tool_name="Read", tool_use_id="tu-usage-7")
        tools.handle_pre_tool_use(ctx)
        events = _read_jsonl(self.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events))


# ---------------------------------------------------------------------------
# Tool-firing usage suppression in subagent scope
# ---------------------------------------------------------------------------

class TestSubagentToolFiringUsageSuppression(unittest.TestCase):
    """When ctx.agent_id is set (subagent scope), no usage_record is written
    to any stream by any of the three tool handlers, regardless of capture mode.

    Tool events themselves are unaffected: they are still emitted and still
    route to the invocation stream exactly as before. Orchestrator-scoped
    firings (no ctx.agent_id) continue to emit usage records, unchanged."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))
        self._orig_capture = os.environ.get("MOSAIC_LOGGER_USAGE_CAPTURE")

    def tearDown(self):
        self.tmp.cleanup()
        if self._orig_capture is None:
            os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        else:
            os.environ["MOSAIC_LOGGER_USAGE_CAPTURE"] = self._orig_capture

    def _transcript(self, subdir=None):
        """Write a single-record transcript and return its path."""
        dir_path = pathlib.Path(self.tmp.name)
        if subdir:
            dir_path = dir_path / subdir
        return _write_tool_transcript(str(dir_path), [
            {"type": "assistant", "message": {
                "id": "msg_supp", "model": "claude-opus-4-5",
                "usage": {"input_tokens": 10},
            }},
        ])

    def _register(self, agent_id, agent_instance_id):
        _register_mapping(self.tmp.name, _RUN_ID, agent_id, agent_instance_id)

    # --- Subagent scope: no usage_record to invocation stream ---

    def test_pre_tool_use_subagent_scope_emits_no_usage_record_to_invocation_stream(self):
        """PreToolUse with agent_id must write no usage_record to the invocation stream."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        self._register("aid-sup-1", "SubAgent#101")
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-sup-1",
                            tool_name="Read", tool_use_id="tu-sup-1",
                            transcript_path=self._transcript("s1"))
        tools.handle_pre_tool_use(ctx)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "SubAgent#101"))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events),
                         "PreToolUse in subagent scope must not write usage_record to the invocation stream")

    def test_post_tool_use_subagent_scope_emits_no_usage_record_to_invocation_stream(self):
        """PostToolUse with agent_id must write no usage_record to the invocation stream."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        self._register("aid-sup-2", "SubAgent#102")
        ctx = _post_tool_ctx(self.tmp.name, agent_id="aid-sup-2",
                             tool_name="Write", tool_use_id="tu-sup-2",
                             tool_output="ok", transcript_path=self._transcript("s2"))
        tools.handle_post_tool_use(ctx)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "SubAgent#102"))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events),
                         "PostToolUse in subagent scope must not write usage_record to the invocation stream")

    def test_post_tool_use_failure_subagent_scope_emits_no_usage_record_to_invocation_stream(self):
        """PostToolUseFailure with agent_id must write no usage_record to the invocation stream."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        self._register("aid-sup-3", "SubAgent#103")
        ctx = _failure_ctx(self.tmp.name, agent_id="aid-sup-3",
                           tool_name="Bash", tool_use_id="tu-sup-3",
                           tool_output="Error: exit 1", transcript_path=self._transcript("s3"))
        tools.handle_post_tool_use_failure(ctx)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "SubAgent#103"))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events),
                         "PostToolUseFailure in subagent scope must not write usage_record to the invocation stream")

    # --- Subagent scope: no usage_record to orchestrator stream either ---

    def test_pre_tool_use_subagent_scope_emits_no_usage_record_to_orchestrator_stream(self):
        """PreToolUse with agent_id must not leak a usage_record to the orchestrator stream."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        self._register("aid-sup-4", "SubAgent#104")
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-sup-4",
                            tool_name="Read", tool_use_id="tu-sup-4",
                            transcript_path=self._transcript("s4"))
        tools.handle_pre_tool_use(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        orc_events = _read_jsonl(orc_path) if orc_path.exists() else []
        usage_events = [e for e in orc_events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events),
                         "PreToolUse in subagent scope must not leak usage_record to the orchestrator stream")

    def test_post_tool_use_subagent_scope_emits_no_usage_record_to_orchestrator_stream(self):
        """PostToolUse with agent_id must not leak a usage_record to the orchestrator stream."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        self._register("aid-sup-5", "SubAgent#105")
        ctx = _post_tool_ctx(self.tmp.name, agent_id="aid-sup-5",
                             tool_name="Write", tool_use_id="tu-sup-5",
                             tool_output="ok", transcript_path=self._transcript("s5"))
        tools.handle_post_tool_use(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        orc_events = _read_jsonl(orc_path) if orc_path.exists() else []
        usage_events = [e for e in orc_events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events),
                         "PostToolUse in subagent scope must not leak usage_record to the orchestrator stream")

    def test_post_tool_use_failure_subagent_scope_emits_no_usage_record_to_orchestrator_stream(self):
        """PostToolUseFailure with agent_id must not leak a usage_record to the orchestrator stream."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        self._register("aid-sup-6", "SubAgent#106")
        ctx = _failure_ctx(self.tmp.name, agent_id="aid-sup-6",
                           tool_name="Bash", tool_use_id="tu-sup-6",
                           tool_output="Error: exit 1", transcript_path=self._transcript("s6"))
        tools.handle_post_tool_use_failure(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        orc_events = _read_jsonl(orc_path) if orc_path.exists() else []
        usage_events = [e for e in orc_events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events),
                         "PostToolUseFailure in subagent scope must not leak usage_record to the orchestrator stream")

    # --- Subagent scope: tool event itself still emitted ---

    def test_pre_tool_use_subagent_scope_tool_event_still_emitted(self):
        """Suppressing usage_record must not suppress the tool_call_start event.
        The tool event must still be written to the invocation stream."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        self._register("aid-sup-7", "SubAgent#107")
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-sup-7",
                            tool_name="Read", tool_use_id="tu-sup-7",
                            transcript_path=self._transcript("s7"))
        tools.handle_pre_tool_use(ctx)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "SubAgent#107"))
        tool_events = [e for e in events if e["event"] == "tool_call_start"]
        self.assertEqual(1, len(tool_events),
                         "tool_call_start must still be emitted to the invocation stream when usage is suppressed")

    def test_post_tool_use_subagent_scope_tool_event_still_emitted(self):
        """Suppressing usage_record must not suppress the tool_call_end event.
        The tool event must still be written to the invocation stream."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        self._register("aid-sup-8", "SubAgent#108")
        ctx = _post_tool_ctx(self.tmp.name, agent_id="aid-sup-8",
                             tool_name="Write", tool_use_id="tu-sup-8",
                             tool_output="ok", transcript_path=self._transcript("s8"))
        tools.handle_post_tool_use(ctx)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "SubAgent#108"))
        tool_events = [e for e in events if e["event"] == "tool_call_end"]
        self.assertEqual(1, len(tool_events),
                         "tool_call_end must still be emitted to the invocation stream when usage is suppressed")

    def test_post_tool_use_failure_subagent_scope_tool_event_still_emitted(self):
        """Suppressing usage_record must not suppress the tool_call_end (error) event.
        The tool event must still be written to the invocation stream."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        self._register("aid-sup-9", "SubAgent#109")
        ctx = _failure_ctx(self.tmp.name, agent_id="aid-sup-9",
                           tool_name="Bash", tool_use_id="tu-sup-9",
                           tool_output="Error: exit 1", transcript_path=self._transcript("s9"))
        tools.handle_post_tool_use_failure(ctx)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "SubAgent#109"))
        tool_events = [e for e in events if e["event"] == "tool_call_end"]
        self.assertEqual(1, len(tool_events),
                         "tool_call_end (error) must still be emitted to the invocation stream when usage is suppressed")

    # --- Orchestrator scope: usage_record emission unchanged (regression guards) ---

    def test_orchestrator_pre_tool_use_still_emits_usage_record(self):
        """Suppression applies only in subagent scope. Orchestrator-scoped
        PreToolUse must still emit a usage_record to the orchestrator stream."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        ctx = _pre_tool_ctx(self.tmp.name, tool_name="Read", tool_use_id="tu-sup-orc-1",
                            transcript_path=self._transcript("o1"))
        tools.handle_pre_tool_use(ctx)
        events = _read_jsonl(self.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events),
                         "Orchestrator-scoped PreToolUse must still emit usage_record")

    def test_orchestrator_post_tool_use_still_emits_usage_record(self):
        """Orchestrator-scoped PostToolUse must still emit a usage_record."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        ctx = _post_tool_ctx(self.tmp.name, tool_name="Read", tool_use_id="tu-sup-orc-2",
                             tool_output="x", transcript_path=self._transcript("o2"))
        tools.handle_post_tool_use(ctx)
        events = _read_jsonl(self.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events),
                         "Orchestrator-scoped PostToolUse must still emit usage_record")

    def test_orchestrator_post_tool_use_failure_still_emits_usage_record(self):
        """Orchestrator-scoped PostToolUseFailure must still emit a usage_record."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        ctx = _failure_ctx(self.tmp.name, tool_name="Bash", tool_use_id="tu-sup-orc-3",
                           tool_output="Error: exit 1", transcript_path=self._transcript("o3"))
        tools.handle_post_tool_use_failure(ctx)
        events = _read_jsonl(self.paths.orchestrator_events(_RUN_ID))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(1, len(usage_events),
                         "Orchestrator-scoped PostToolUseFailure must still emit usage_record")

    # --- boundaries gate: unchanged for orchestrator scope (regression guards) ---

    def test_boundaries_gate_suppresses_orchestrator_scope_emission(self):
        """MOSAIC_LOGGER_USAGE_CAPTURE=boundaries continues to suppress usage
        emission for orchestrator-scoped firings, unaffected by the subagent fix."""
        os.environ["MOSAIC_LOGGER_USAGE_CAPTURE"] = "boundaries"
        ctx = _pre_tool_ctx(self.tmp.name, tool_name="Read", tool_use_id="tu-sup-gate-1",
                            transcript_path=self._transcript("g1"))
        tools.handle_pre_tool_use(ctx)
        orc_path = self.paths.orchestrator_events(_RUN_ID)
        events = _read_jsonl(orc_path) if orc_path.exists() else []
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events),
                         "boundaries gate must still suppress usage_record for orchestrator-scoped firings")

    def test_subagent_scope_suppression_independent_of_boundaries_gate(self):
        """Subagent suppression applies regardless of MOSAIC_LOGGER_USAGE_CAPTURE.
        Setting boundaries while in subagent scope must also produce no usage_record."""
        os.environ["MOSAIC_LOGGER_USAGE_CAPTURE"] = "boundaries"
        self._register("aid-sup-gate-2", "SubAgent#110")
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-sup-gate-2",
                            tool_name="Read", tool_use_id="tu-sup-gate-2",
                            transcript_path=self._transcript("g2"))
        tools.handle_pre_tool_use(ctx)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "SubAgent#110"))
        usage_events = [e for e in events if e["event"] == "usage_record"]
        self.assertEqual(0, len(usage_events),
                         "Subagent-scope suppression must apply even when boundaries gate is active")

    def test_subagent_tool_event_not_suppressed_when_boundaries_gate_active(self):
        """With MOSAIC_LOGGER_USAGE_CAPTURE=boundaries, the tool event is still
        emitted in subagent scope. Only usage emission is affected by the gate."""
        os.environ["MOSAIC_LOGGER_USAGE_CAPTURE"] = "boundaries"
        self._register("aid-sup-gate-3", "SubAgent#111")
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-sup-gate-3",
                            tool_name="Read", tool_use_id="tu-sup-gate-3",
                            transcript_path=self._transcript("g3"))
        tools.handle_pre_tool_use(ctx)
        events = _read_jsonl(self.paths.invocation_events(_RUN_ID, "SubAgent#111"))
        tool_events = [e for e in events if e["event"] == "tool_call_start"]
        self.assertEqual(1, len(tool_events),
                         "tool_call_start must still be emitted in subagent scope with boundaries gate active")

    def test_subagent_scope_never_raises_regardless_of_transcript_content(self):
        """handle_pre_tool_use must not raise even in subagent scope with a
        transcript that contains unusual data. Suppression is silent."""
        os.environ.pop("MOSAIC_LOGGER_USAGE_CAPTURE", None)
        self._register("aid-sup-nr-1", "SubAgent#112")
        ctx = _pre_tool_ctx(self.tmp.name, agent_id="aid-sup-nr-1",
                            tool_name="Read", tool_use_id="tu-sup-nr-1",
                            transcript_path=self._transcript("nr1"))
        try:
            tools.handle_pre_tool_use(ctx)
        except Exception as exc:
            self.fail(f"handle_pre_tool_use raised in subagent scope: {exc}")


if __name__ == "__main__":
    unittest.main()
