"""Tests for agent mapping functions in mosaic_logger_runstate (vscode-ghcp variant).

Key differences from claude-code and ghcp-cli variants:
- The correlation key parameter is named 'agent_key' (not 'agent_id').  In this
  adapter, handlers pass ctx.agent_name (VS Code's primary correlation key) as
  the agent_key argument, but the functions accept any arbitrary string key.
- The on-disk JSON file stores the field as "agent_key" (not "agent_id"), making
  the divergence self-documenting on disk.
- resolve_invocation_id falls back to f"unmapped_{agent_key}", which will contain
  the agent_name string rather than an opaque harness ID.

Covers: put_agent_mapping persistence and return value, get_agent_mapping
reading and absent-file resilience, resolve_invocation_id mapping return and
fallback to 'unmapped_{agent_key}', arbitrary string keys, and never-raises.
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
_AGENT_KEY = "TestWriter"                        # agent_name string in vscode-ghcp
_AGENT_INSTANCE_ID = "contracts-designer#12"
_AGENT_TYPE = "ContractsDesigner"


class TestPutAgentMapping(unittest.TestCase):
    """put_agent_mapping persists agent_key -> agent_instance_id mapping.
    The agent_key parameter accepts any arbitrary string (in this adapter, the
    agent_name string from VS Code's SubagentStart payload)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_true_on_success(self):
        result = runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_KEY, _AGENT_INSTANCE_ID, _AGENT_TYPE
        )
        self.assertTrue(result)

    def test_creates_mapping_file(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_KEY, _AGENT_INSTANCE_ID, _AGENT_TYPE
        )
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_KEY)
        self.assertTrue(entry_path.exists())

    def test_stored_mapping_has_agent_instance_id(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_KEY, _AGENT_INSTANCE_ID, _AGENT_TYPE
        )
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_KEY)
        data = json.loads(entry_path.read_text(encoding="utf-8"))
        self.assertEqual(_AGENT_INSTANCE_ID, data["agent_instance_id"])

    def test_stored_mapping_uses_agent_key_field_name_not_agent_id(self):
        """On-disk entry must use 'agent_key' as the field name, not 'agent_id'.
        This makes the vscode-ghcp divergence self-documenting on disk."""
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_KEY, _AGENT_INSTANCE_ID, _AGENT_TYPE
        )
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_KEY)
        data = json.loads(entry_path.read_text(encoding="utf-8"))
        self.assertIn("agent_key", data,
                      "On-disk entry must use 'agent_key', not 'agent_id'")
        self.assertNotIn("agent_id", data,
                         "On-disk entry must not use 'agent_id' in vscode-ghcp")

    def test_stored_agent_key_value_matches_input(self):
        """The stored agent_key field must contain the exact key passed in."""
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "MySpecificAgentName", _AGENT_INSTANCE_ID, None
        )
        entry_path = self.paths.agent_map_entry(_RUN_ID, "MySpecificAgentName")
        data = json.loads(entry_path.read_text(encoding="utf-8"))
        self.assertEqual("MySpecificAgentName", data["agent_key"])

    def test_stored_mapping_has_agent_type_when_provided(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_KEY, _AGENT_INSTANCE_ID, _AGENT_TYPE
        )
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_KEY)
        data = json.loads(entry_path.read_text(encoding="utf-8"))
        self.assertEqual(_AGENT_TYPE, data.get("agent_type"))

    def test_put_creates_parent_agent_map_directory(self):
        map_dir = self.paths.agent_map_dir(_RUN_ID)
        self.assertFalse(map_dir.exists())
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_KEY, _AGENT_INSTANCE_ID, None
        )
        self.assertTrue(map_dir.exists())

    def test_different_agent_keys_produce_separate_files(self):
        """Different agent_name strings produce different map files."""
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "AgentNameAlpha", "AgentA#1", None
        )
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "AgentNameBeta", "AgentB#2", None
        )
        path_a = self.paths.agent_map_entry(_RUN_ID, "AgentNameAlpha")
        path_b = self.paths.agent_map_entry(_RUN_ID, "AgentNameBeta")
        self.assertNotEqual(path_a, path_b)
        self.assertTrue(path_a.exists())
        self.assertTrue(path_b.exists())

    def test_overwrite_updates_existing_mapping(self):
        """A second SubagentStart with the same agent_name overwrites the first."""
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_KEY, "first-agent#1", None
        )
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_KEY, "second-agent#2", None
        )
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_KEY)
        data = json.loads(entry_path.read_text(encoding="utf-8"))
        self.assertEqual("second-agent#2", data["agent_instance_id"])

    def test_arbitrary_string_key_with_spaces_in_name(self):
        """An agent_name that contains spaces is accepted as a valid key."""
        key_with_spaces = "My Agent Name"
        result = runstate.put_agent_mapping(
            self.paths, _RUN_ID, key_with_spaces, _AGENT_INSTANCE_ID, None
        )
        self.assertTrue(result)
        entry_path = self.paths.agent_map_entry(_RUN_ID, key_with_spaces)
        self.assertTrue(entry_path.exists())

    def test_returns_false_on_io_failure(self):
        """Returns False when the mapping file cannot be written."""
        map_dir = self.paths.agent_map_dir(_RUN_ID)
        map_dir.parent.mkdir(parents=True, exist_ok=True)
        map_dir.write_bytes(b"blocker")
        result = runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_KEY, _AGENT_INSTANCE_ID, None
        )
        self.assertFalse(result)

    def test_never_raises(self):
        """put_agent_mapping must never raise, even on I/O failure."""
        map_dir = self.paths.agent_map_dir(_RUN_ID)
        map_dir.parent.mkdir(parents=True, exist_ok=True)
        map_dir.write_bytes(b"blocker")
        try:
            runstate.put_agent_mapping(
                self.paths, _RUN_ID, _AGENT_KEY, _AGENT_INSTANCE_ID, None
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

    def _put(self, agent_key=_AGENT_KEY, agent_instance_id=_AGENT_INSTANCE_ID,
             agent_type=_AGENT_TYPE):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, agent_key, agent_instance_id, agent_type
        )

    def test_returns_dict_after_successful_put(self):
        self._put()
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, _AGENT_KEY)
        self.assertIsNotNone(result)
        self.assertIsInstance(result, dict)

    def test_returns_correct_agent_instance_id(self):
        self._put(agent_instance_id="planner-tdd-soft#6")
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, _AGENT_KEY)
        self.assertEqual("planner-tdd-soft#6", result["agent_instance_id"])

    def test_returns_correct_agent_type(self):
        self._put(agent_type="PlannerTddSoft")
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, _AGENT_KEY)
        self.assertEqual("PlannerTddSoft", result.get("agent_type"))

    def test_returns_none_for_absent_agent_key(self):
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, "NonexistentAgentName")
        self.assertIsNone(result)

    def test_returns_none_when_no_entries_exist(self):
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, _AGENT_KEY)
        self.assertIsNone(result)

    def test_returns_none_for_corrupt_file(self):
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_KEY)
        entry_path.parent.mkdir(parents=True, exist_ok=True)
        entry_path.write_text("{broken json{{", encoding="utf-8")
        result = runstate.get_agent_mapping(self.paths, _RUN_ID, _AGENT_KEY)
        self.assertIsNone(result)

    def test_never_raises_on_absent_file(self):
        try:
            runstate.get_agent_mapping(self.paths, _RUN_ID, "nonexistent")
        except Exception as exc:
            self.fail(f"get_agent_mapping raised on absent file: {exc}")

    def test_never_raises_on_corrupt_file(self):
        entry_path = self.paths.agent_map_entry(_RUN_ID, _AGENT_KEY)
        entry_path.parent.mkdir(parents=True, exist_ok=True)
        entry_path.write_text("}{totally broken{{", encoding="utf-8")
        try:
            runstate.get_agent_mapping(self.paths, _RUN_ID, _AGENT_KEY)
        except Exception as exc:
            self.fail(f"get_agent_mapping raised on corrupt file: {exc}")

    def test_isolation_between_different_run_ids(self):
        """A mapping under one run_id must not be visible under a different run_id."""
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_KEY, _AGENT_INSTANCE_ID, None
        )
        result = runstate.get_agent_mapping(self.paths, "other-run-id", _AGENT_KEY)
        self.assertIsNone(result)


class TestResolveInvocationId(unittest.TestCase):
    """resolve_invocation_id returns the mapped agent_instance_id or
    'unmapped_{agent_key}' — the fallback includes the agent_name string, not
    an opaque harness ID."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.paths = core.build_paths(pathlib.Path(self.tmp.name))

    def tearDown(self):
        self.tmp.cleanup()

    def test_returns_mapped_agent_instance_id_when_mapping_exists(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, _AGENT_KEY, _AGENT_INSTANCE_ID, None
        )
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, _AGENT_KEY)
        self.assertEqual(_AGENT_INSTANCE_ID, result)

    def test_returns_unmapped_prefix_when_no_mapping_exists(self):
        """Without a mapping, returns 'unmapped_{agent_key}'."""
        result = runstate.resolve_invocation_id(
            self.paths, _RUN_ID, "UnmappedAgentName"
        )
        self.assertEqual("unmapped_UnmappedAgentName", result)

    def test_fallback_starts_with_unmapped_prefix(self):
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, "SomeAgentName")
        self.assertTrue(result.startswith("unmapped_"))

    def test_fallback_includes_agent_key(self):
        """The fallback quarantine string includes the agent_name for traceability."""
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, "MySpecificAgentName")
        self.assertIn("MySpecificAgentName", result)

    def test_fallback_contains_agent_name_not_agent_id_literal(self):
        """The fallback must not end in 'unmapped_agent_id' — it uses the actual key."""
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, "SomeName")
        self.assertNotEqual("unmapped_agent_id", result)

    def test_never_returns_none(self):
        """resolve_invocation_id always returns a string, never None."""
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, _AGENT_KEY)
        self.assertIsNotNone(result)
        self.assertIsInstance(result, str)

    def test_different_agent_keys_produce_different_results(self):
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "NameAlpha", "AgentA#1", None
        )
        runstate.put_agent_mapping(
            self.paths, _RUN_ID, "NameBeta", "AgentB#2", None
        )
        result_a = runstate.resolve_invocation_id(self.paths, _RUN_ID, "NameAlpha")
        result_b = runstate.resolve_invocation_id(self.paths, _RUN_ID, "NameBeta")
        self.assertNotEqual(result_a, result_b)

    def test_never_raises(self):
        try:
            runstate.resolve_invocation_id(self.paths, _RUN_ID, "any-agent-name")
        except Exception as exc:
            self.fail(f"resolve_invocation_id raised unexpectedly: {exc}")

    def test_unmapped_result_is_never_empty(self):
        """Even for an empty string key the result must be non-empty."""
        result = runstate.resolve_invocation_id(self.paths, _RUN_ID, "")
        self.assertIsNotNone(result)
        self.assertGreater(len(result), 0)


if __name__ == "__main__":
    unittest.main()
