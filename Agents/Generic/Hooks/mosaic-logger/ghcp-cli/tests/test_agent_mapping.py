"""Tests for agent mapping functions in mosaic_logger_runstate (ghcp-cli variant).

Covers: put_agent_mapping persistence and return value, get_agent_mapping reading
and absent-file resilience, resolve_invocation_id mapping return and fallback to
'unmapped_{agent_id}', and never-raises contracts.

In GHCP CLI, the agent_id comes from the 'agentId' camelCase payload field,
which HookContext maps to ctx.agent_id. These functions operate on agent_id
values (the harness-native opaque identifier) and map them to the structured
MOSAIC agent_instance_id values extracted from dispatch prompts.
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


_RUN_ID = "20260101T120000Z-d7e3"
_AGENT_ID = "harness-agent-001"
_AGENT_INSTANCE_ID = "contracts-designer#12"
_AGENT_TYPE = "ContractsDesigner"


class TestPutAgentMapping(unittest.TestCase):
    """put_agent_mapping persists agent_id -> agent_instance_id mapping."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_true_on_success(self):
        result = runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_ID, _AGENT_INSTANCE_ID, _AGENT_TYPE
        )
        self.assertTrue(result)

    def test_creates_mapping_file(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_ID, _AGENT_INSTANCE_ID, _AGENT_TYPE
        )
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_ID)
        self.assertTrue(entry_path.exists())

    def test_stored_mapping_has_agent_instance_id(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_ID, _AGENT_INSTANCE_ID, _AGENT_TYPE
        )
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_ID)
        data = json.loads(entry_path.read_text(encoding="utf-8"))
        self.assertEqual(_AGENT_INSTANCE_ID, data["agent_instance_id"])

    def test_stored_mapping_has_agent_type_when_provided(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_ID, _AGENT_INSTANCE_ID, _AGENT_TYPE
        )
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_ID)
        data = json.loads(entry_path.read_text(encoding="utf-8"))
        self.assertEqual(_AGENT_TYPE, data.get("agent_type"))

    def test_put_creates_parent_agent_map_directory(self):
        map_dir = self.paths.agent_map_dir(_RUN_ID)
        self.assertFalse(map_dir.exists())
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_ID, _AGENT_INSTANCE_ID, None
        )
        self.assertTrue(map_dir.exists())

    def test_different_agent_ids_produce_separate_files(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "harness-a", "AgentA#1", None
        )
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "harness-b", "AgentB#2", None
        )
        path_a = self.paths.agent_map_entry(_RUN_ID, "harness-a")
        path_b = self.paths.agent_map_entry(_RUN_ID, "harness-b")
        self.assertNotEqual(path_a, path_b)
        self.assertTrue(path_a.exists())
        self.assertTrue(path_b.exists())

    def test_overwrite_updates_existing_mapping(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_ID, "first-agent#1", None
        )
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_ID, "second-agent#2", None
        )
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_ID)
        data = json.loads(entry_path.read_text(encoding="utf-8"))
        self.assertEqual("second-agent#2", data["agent_instance_id"])

    def test_returns_false_on_io_failure(self):
        """Returns False when the mapping file cannot be written."""
        map_dir = self.paths.agent_map_dir(_RUN_ID)
        map_dir.parent.mkdir(parents=True, exist_ok=True)
        map_dir.write_bytes(b"blocker")  # place a file where dir would be
        result = runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_ID, _AGENT_INSTANCE_ID, None
        )
        self.assertFalse(result)

    def test_never_raises(self):
        """put_agent_mapping must never raise, even on I/O failure."""
        map_dir = self.paths.agent_map_dir(_RUN_ID)
        map_dir.parent.mkdir(parents=True, exist_ok=True)
        map_dir.write_bytes(b"blocker")
        try:
            runstate.put_agent_mapping(
                self.paths, _RUN_ID, _AGENT_ID, _AGENT_INSTANCE_ID, None
            )
        except Exception as exc:
            self.fail(f"put_agent_mapping raised unexpectedly: {exc}")


class TestGetAgentMapping(unittest.TestCase):
    """get_agent_mapping reads the mapping file; returns None on any failure."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def _put(self, agent_id=_AGENT_ID, agent_instance_id=_AGENT_INSTANCE_ID,
             agent_type=_AGENT_TYPE):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, agent_id, agent_instance_id, agent_type
        )

    def test_returns_dict_after_successful_put(self):
        self._put()
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, _AGENT_ID)
        self.assertIsNotNone(result)
        self.assertIsInstance(result, dict)

    def test_returns_correct_agent_instance_id(self):
        self._put(agent_instance_id="planner-tdd-soft#6")
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, _AGENT_ID)
        self.assertEqual("planner-tdd-soft#6", result["agent_instance_id"])

    def test_returns_correct_agent_type(self):
        self._put(agent_type="PlannerTddSoft")
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, _AGENT_ID)
        self.assertEqual("PlannerTddSoft", result.get("agent_type"))

    def test_returns_none_for_absent_agent_id(self):
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, "nonexistent-agent")
        self.assertIsNone(result)

    def test_returns_none_when_no_entries_exist(self):
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, _AGENT_ID)
        self.assertIsNone(result)

    def test_returns_none_for_corrupt_file(self):
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_ID)
        entry_path.parent.mkdir(parents=True, exist_ok=True)
        entry_path.write_text("{broken json{{", encoding="utf-8")
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, _AGENT_ID)
        self.assertIsNone(result)

    def test_never_raises_on_absent_file(self):
        try:
            runstate.get_agent_mapping(self.paths, _RUN_ID, "nonexistent")
        except Exception as exc:
            self.fail(f"get_agent_mapping raised on absent file: {exc}")

    def test_never_raises_on_corrupt_file(self):
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_ID)
        entry_path.parent.mkdir(parents=True, exist_ok=True)
        entry_path.write_text("}{totally broken{{", encoding="utf-8")
        try:
            runstate.get_agent_mapping(self.paths, _RUN_ID, _AGENT_ID)
        except Exception as exc:
            self.fail(f"get_agent_mapping raised on corrupt file: {exc}")

    def test_isolation_between_different_run_ids(self):
        """A mapping under one run_id must not be visible under a different run_id."""
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_ID, _AGENT_INSTANCE_ID, None
        )
        result = runstate.get_agent_mapping(self.paths, "other-run-id", _AGENT_ID)
        self.assertIsNone(result)


class TestResolveInvocationId(unittest.TestCase):
    """resolve_invocation_id returns the mapped agent_instance_id or 'unmapped_{agent_id}'."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_mapped_agent_instance_id_when_mapping_exists(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_ID, _AGENT_INSTANCE_ID, None
        )
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, _AGENT_ID)
        self.assertEqual(_AGENT_INSTANCE_ID, result)

    def test_returns_unmapped_prefix_when_no_mapping_exists(self):
        """Without a mapping, returns 'unmapped_{agent_id}'."""
        result = runstate.resolve_invocation_id(
            self.paths, _RUN_ID, "harness-unknown-xyz"
        )
        self.assertEqual("unmapped_harness-unknown-xyz", result)

    def test_fallback_starts_with_unmapped_prefix(self):
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, "some-agent-id")
        self.assertTrue(result.startswith("unmapped_"))

    def test_fallback_includes_agent_id(self):
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, "harness-agent-999")
        self.assertIn("harness-agent-999", result)

    def test_never_returns_none(self):
        """resolve_invocation_id always returns a string, never None."""
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, _AGENT_ID)
        self.assertIsNotNone(result)
        self.assertIsInstance(result, str)

    def test_different_agent_ids_produce_different_results(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "harness-a", "AgentA#1", None
        )
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "harness-b", "AgentB#2", None
        )
        result_a = runstate.resolve_invocation_id(self.paths, _RUN_ID, "harness-a")
        result_b = runstate.resolve_invocation_id(self.paths, _RUN_ID, "harness-b")
        self.assertNotEqual(result_a, result_b)

    def test_never_raises(self):
        try:
            runstate.resolve_invocation_id(self.paths, _RUN_ID, "any-agent-id")
        except Exception as exc:
            self.fail(f"resolve_invocation_id raised unexpectedly: {exc}")


if __name__ == "__main__":
    unittest.main()
