"""Regression tests confirming that currently-correct routing remains unaffected
after the session run_id cache feature is added (ghcp-cli variant).

These tests document the existing expected behaviour for:
  - subagentStart agent-mapping registration (put_agent_mapping, resolve_invocation_id)
  - invocation_start event placement in the correct run-scoped invocation folder
  - invocation_end event placement in the same run-scoped folder
  - 01_input.md and 02_output.md written to the run-scoped invocation folder
  - unmapped guard: subagentStop is a no-op when no mapping was registered

They must remain green both before and after the cache implementation is added.
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
import mosaic_logger_handlers_invocation as invocation


_RUN_ID = "20260804T150537Z-796a"
_SESSION_ID = "ghcp-sess-regression-test"
_AGENT_ID = "ghcp-agt-regression-001"
_INSTANCE_ID = "planner-tdd-soft#7"
_TS = "2026-08-04T15:05:37.000Z"
_AGENT_PROMPT = (
    f'{{"agent_instance_id": "{_INSTANCE_ID}", "run_id": "{_RUN_ID}", '
    '"task_description": "Write the plan."}}'
)


def _make_ctx(event, tmp_path, run_id=_RUN_ID, session_id=_SESSION_ID,
              **payload_extra):
    """Build a HookContext with run_id pre-set (as the dispatcher would)."""
    payload = {"sessionId": session_id, **payload_extra}
    paths = core.build_paths(tmp_path)
    ctx = core.HookContext(event, payload, tmp_path, paths, _TS)
    ctx.run_id = run_id
    return ctx


def _read_jsonl(path):
    return [json.loads(ln) for ln in path.read_text("utf-8").splitlines() if ln.strip()]


def _register_agent(tmp_path, run_id, agent_id, agent_instance_id):
    paths = core.build_paths(pathlib.Path(tmp_path))
    runstate.put_agent_mapping(paths, run_id, agent_id, agent_instance_id, None)


class TestSubagentMappingRegistrationUnchanged(unittest.TestCase):
    """handle_subagent_start still registers the agent_id -> agent_instance_id mapping."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _make_start_ctx(self, **extra):
        return _make_ctx("subagentStart", self.tmp_path,
                         agentId=_AGENT_ID, agentType="planner-tdd-soft",
                         agentPrompt=_AGENT_PROMPT, **extra)

    def test_handle_subagent_start_registers_agent_mapping(self):
        ctx = self._make_start_ctx()
        invocation.handle_subagent_start(ctx)
        paths = core.build_paths(self.tmp_path)
        mapping = runstate.get_agent_mapping(paths, _RUN_ID, _AGENT_ID)
        self.assertIsNotNone(mapping)

    def test_agent_mapping_contains_correct_agent_instance_id(self):
        ctx = self._make_start_ctx()
        invocation.handle_subagent_start(ctx)
        paths = core.build_paths(self.tmp_path)
        mapping = runstate.get_agent_mapping(paths, _RUN_ID, _AGENT_ID)
        self.assertEqual(_INSTANCE_ID, mapping["agent_instance_id"])

    def test_agent_mapping_entry_is_under_run_root(self):
        """The run-scoped agent map entry must live under the run_root."""
        ctx = self._make_start_ctx()
        invocation.handle_subagent_start(ctx)
        paths = core.build_paths(self.tmp_path)
        entry = paths.agent_map_entry(_RUN_ID, _AGENT_ID)
        self.assertTrue(entry.exists())
        run_root = paths.run_root(_RUN_ID)
        self.assertTrue(
            str(entry).startswith(str(run_root)),
            f"agent_map_entry {entry} is not under run_root {run_root}",
        )

    def test_resolve_invocation_id_returns_instance_id_after_subagent_start(self):
        ctx = self._make_start_ctx()
        invocation.handle_subagent_start(ctx)
        paths = core.build_paths(self.tmp_path)
        resolved = runstate.resolve_invocation_id(paths, _RUN_ID, _AGENT_ID)
        self.assertEqual(_INSTANCE_ID, resolved)

    def test_resolve_invocation_id_returns_unmapped_prefix_for_unknown_agent(self):
        paths = core.build_paths(self.tmp_path)
        resolved = runstate.resolve_invocation_id(paths, _RUN_ID, "no-such-agent")
        self.assertTrue(resolved.startswith("unmapped_"),
                        f"Expected 'unmapped_' prefix, got: {resolved!r}")


class TestInvocationStartPlacement(unittest.TestCase):
    """invocation_start event is written to the correct run-scoped invocation folder."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _start_ctx(self):
        return _make_ctx("subagentStart", self.tmp_path,
                         agentId=_AGENT_ID, agentType="planner-tdd-soft",
                         agentPrompt=_AGENT_PROMPT)

    def test_invocation_start_written_to_run_scoped_events_file(self):
        ctx = self._start_ctx()
        invocation.handle_subagent_start(ctx)
        paths = core.build_paths(self.tmp_path)
        sink = paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(sink.exists(),
                        f"invocation events file not found: {sink}")
        events = _read_jsonl(sink)
        event_types = [e["event"] for e in events]
        self.assertIn("invocation_start", event_types)

    def test_invocation_start_not_written_to_unknown_run_folder(self):
        ctx = self._start_ctx()
        invocation.handle_subagent_start(ctx)
        paths = core.build_paths(self.tmp_path)
        unknown_sink = paths.orchestrator_events("unknown-run")
        if unknown_sink.exists():
            events = _read_jsonl(unknown_sink)
            event_types = [e["event"] for e in events]
            self.assertNotIn("invocation_start", event_types)

    def test_invocation_start_event_has_correct_agent_instance_id(self):
        ctx = self._start_ctx()
        invocation.handle_subagent_start(ctx)
        paths = core.build_paths(self.tmp_path)
        sink = paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        events = _read_jsonl(sink)
        start_events = [e for e in events if e["event"] == "invocation_start"]
        self.assertEqual(1, len(start_events))
        self.assertEqual(_INSTANCE_ID, start_events[0]["agent_instance_id"])

    def test_invocation_start_path_is_inside_run_root(self):
        ctx = self._start_ctx()
        invocation.handle_subagent_start(ctx)
        paths = core.build_paths(self.tmp_path)
        sink = paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        run_root = paths.run_root(_RUN_ID)
        self.assertTrue(
            str(sink).startswith(str(run_root)),
            f"invocation_events {sink} is not under run_root {run_root}",
        )

    def test_input_artifact_written_to_run_scoped_folder(self):
        ctx = self._start_ctx()
        invocation.handle_subagent_start(ctx)
        paths = core.build_paths(self.tmp_path)
        input_file = paths.invocation_input(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(input_file.exists(),
                        f"01_input.md not found: {input_file}")

    def test_input_artifact_is_not_in_unknown_run_folder(self):
        ctx = self._start_ctx()
        invocation.handle_subagent_start(ctx)
        paths = core.build_paths(self.tmp_path)
        # unknown-run input for this agent must not exist
        unknown_input = paths.invocation_input("unknown-run", _INSTANCE_ID)
        self.assertFalse(unknown_input.exists())


class TestInvocationEndPlacement(unittest.TestCase):
    """invocation_end event is written to the same run-scoped invocation folder."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)

    def tearDown(self):
        self.tmp.cleanup()

    def _register(self):
        _register_agent(self.tmp_path, _RUN_ID, _AGENT_ID, _INSTANCE_ID)

    def _stop_ctx(self, **extra):
        return _make_ctx("subagentStop", self.tmp_path,
                         agentId=_AGENT_ID,
                         response='{"status_code": "SUCCESS"}',
                         **extra)

    def test_invocation_end_written_to_run_scoped_folder(self):
        self._register()
        ctx = self._stop_ctx()
        invocation.handle_subagent_stop(ctx)
        paths = core.build_paths(self.tmp_path)
        sink = paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(sink.exists())
        events = _read_jsonl(sink)
        event_types = [e["event"] for e in events]
        self.assertIn("invocation_end", event_types)

    def test_invocation_end_has_correct_agent_instance_id(self):
        self._register()
        ctx = self._stop_ctx()
        invocation.handle_subagent_stop(ctx)
        paths = core.build_paths(self.tmp_path)
        sink = paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        events = _read_jsonl(sink)
        end_events = [e for e in events if e["event"] == "invocation_end"]
        self.assertEqual(1, len(end_events))
        self.assertEqual(_INSTANCE_ID, end_events[0]["agent_instance_id"])

    def test_invocation_start_and_end_share_same_folder(self):
        """start followed by stop both write to the same invocation folder."""
        # Write start
        start_ctx = _make_ctx("subagentStart", self.tmp_path,
                               agentId=_AGENT_ID, agentType="planner-tdd-soft",
                               agentPrompt=_AGENT_PROMPT)
        invocation.handle_subagent_start(start_ctx)
        # Write stop (mapping is now registered)
        stop_ctx = self._stop_ctx()
        invocation.handle_subagent_stop(stop_ctx)
        paths = core.build_paths(self.tmp_path)
        sink = paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        events = _read_jsonl(sink)
        event_types = [e["event"] for e in events]
        self.assertIn("invocation_start", event_types)
        self.assertIn("invocation_end", event_types)

    def test_output_artifact_written_to_run_scoped_folder(self):
        self._register()
        ctx = self._stop_ctx()
        invocation.handle_subagent_stop(ctx)
        paths = core.build_paths(self.tmp_path)
        output_file = paths.invocation_output(_RUN_ID, _INSTANCE_ID)
        self.assertTrue(output_file.exists(),
                        f"02_output.md not found: {output_file}")

    def test_unmapped_subagent_stop_is_a_no_op(self):
        """subagentStop without a prior mapping must write nothing and not raise."""
        ctx = _make_ctx("subagentStop", self.tmp_path,
                        agentId="unmapped-agent-999",
                        response='{"status_code": "SUCCESS"}')
        try:
            invocation.handle_subagent_stop(ctx)
        except Exception as exc:
            self.fail(f"handle_subagent_stop raised for unmapped agent: {exc}")
        paths = core.build_paths(self.tmp_path)
        invocation_dir = paths.invocation_dir(_RUN_ID, f"unmapped_unmapped-agent-999")
        self.assertFalse(invocation_dir.exists())

    def test_invocation_end_path_is_inside_run_root(self):
        self._register()
        ctx = self._stop_ctx()
        invocation.handle_subagent_stop(ctx)
        paths = core.build_paths(self.tmp_path)
        sink = paths.invocation_events(_RUN_ID, _INSTANCE_ID)
        run_root = paths.run_root(_RUN_ID)
        self.assertTrue(
            str(sink).startswith(str(run_root)),
            f"invocation_events {sink} is not under run_root {run_root}",
        )


if __name__ == "__main__":
    unittest.main()
