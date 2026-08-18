"""Tests for the workspace-level LogPaths accessors for the agent-to-run
association store (ghcp-cli variant).

The agent-to-run association must live directly under the logs root, NOT inside
any per-run folder, because its readers (tool-event handlers) do not know the
run_id when they need to look up the agent.

Covers:
- MODULE CONSTANT: AGENT_RUN_DIRNAME
- agent_run_dir(): directly under logs root, correct name, creates no dirs
- agent_run_entry(agent_id): inside agent_run_dir(), .json suffix,
  agent_id is sanitized into a single safe path component
- Distinct from run-scoped stores (agent_map_dir, pending_dispatch_dir)
  and from the session run-id cache (session_run_id_dir)
"""

import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core


_AGENT_ID = "harness-agent-pathtest-001"
_AGENT_ID_RESERVED = 'agent/with:reserved"chars'
_RUN_ID = "20260804T150537Z-796a"


class TestAgentRunDirNameConstant(unittest.TestCase):
    """AGENT_RUN_DIRNAME must be defined on the module."""

    def test_agent_run_dirname_constant_exists(self):
        self.assertTrue(hasattr(core, "AGENT_RUN_DIRNAME"))

    def test_agent_run_dirname_value(self):
        self.assertEqual(".agent-run", core.AGENT_RUN_DIRNAME)


class TestAgentRunDirAccessor(unittest.TestCase):
    """agent_run_dir() resolves directly under the logs root."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.workspace = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.workspace)

    def tearDown(self):
        self.tmp.cleanup()

    def test_method_exists_and_is_callable(self):
        self.assertTrue(hasattr(self.paths, "agent_run_dir"))
        self.assertTrue(callable(self.paths.agent_run_dir))

    def test_returns_pathlib_path(self):
        result = self.paths.agent_run_dir()
        self.assertIsInstance(result, pathlib.Path)

    def test_parent_is_logs_root(self):
        """agent_run_dir must be a direct child of logs root."""
        result = self.paths.agent_run_dir()
        self.assertEqual(self.paths.root, result.parent)

    def test_directory_name_is_correct(self):
        result = self.paths.agent_run_dir()
        self.assertEqual(".agent-run", result.name)

    def test_is_not_inside_any_run_root(self):
        """The directory must be a sibling of per-run folders, not nested inside one."""
        result = self.paths.agent_run_dir()
        run_root = self.paths.run_root(_RUN_ID)
        self.assertFalse(
            str(result).startswith(str(run_root)),
            f"agent_run_dir {result} must not be under run_root {run_root}",
        )

    def test_creates_no_directories(self):
        """Consistent with every other LogPaths accessor, no directories are created."""
        self.paths.agent_run_dir()
        agent_run_dir = self.paths.root / ".agent-run"
        self.assertFalse(agent_run_dir.exists())

    def test_result_is_deterministic(self):
        first = self.paths.agent_run_dir()
        second = self.paths.agent_run_dir()
        self.assertEqual(first, second)


class TestAgentRunEntryAccessor(unittest.TestCase):
    """agent_run_entry(agent_id) builds the correct per-agent path."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.workspace = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.workspace)

    def tearDown(self):
        self.tmp.cleanup()

    def test_method_exists_and_is_callable(self):
        self.assertTrue(hasattr(self.paths, "agent_run_entry"))
        self.assertTrue(callable(self.paths.agent_run_entry))

    def test_returns_pathlib_path(self):
        result = self.paths.agent_run_entry(_AGENT_ID)
        self.assertIsInstance(result, pathlib.Path)

    def test_has_json_suffix(self):
        result = self.paths.agent_run_entry(_AGENT_ID)
        self.assertEqual(".json", result.suffix)

    def test_parent_is_agent_run_dir(self):
        result = self.paths.agent_run_entry(_AGENT_ID)
        self.assertEqual(self.paths.agent_run_dir(), result.parent)

    def test_stem_equals_sanitized_agent_id(self):
        result = self.paths.agent_run_entry(_AGENT_ID)
        expected_stem = core.sanitize_component(_AGENT_ID)
        self.assertEqual(expected_stem, result.stem)

    def test_agent_id_with_reserved_chars_is_sanitized(self):
        result = self.paths.agent_run_entry(_AGENT_ID_RESERVED)
        for ch in '<>:"/\\|?*':
            self.assertNotIn(ch, result.name,
                             f"Reserved char {ch!r} found in entry name {result.name!r}")

    def test_different_agent_ids_produce_different_paths(self):
        entry_a = self.paths.agent_run_entry("harness-agent-alpha")
        entry_b = self.paths.agent_run_entry("harness-agent-beta")
        self.assertNotEqual(entry_a, entry_b)

    def test_same_agent_id_is_deterministic(self):
        first = self.paths.agent_run_entry(_AGENT_ID)
        second = self.paths.agent_run_entry(_AGENT_ID)
        self.assertEqual(first, second)

    def test_creates_no_directories(self):
        self.paths.agent_run_entry(_AGENT_ID)
        agent_run_dir = self.paths.root / ".agent-run"
        self.assertFalse(agent_run_dir.exists())

    def test_is_not_inside_any_run_root(self):
        result = self.paths.agent_run_entry(_AGENT_ID)
        run_root = self.paths.run_root(_RUN_ID)
        self.assertFalse(
            str(result).startswith(str(run_root)),
            f"agent_run_entry {result} must not be under run_root {run_root}",
        )

    def test_path_does_not_take_run_id_parameter(self):
        """Neither accessor takes a run_id parameter — enforced by calling without one."""
        # Verify that agent_run_entry accepts exactly one argument (agent_id).
        import inspect
        sig = inspect.signature(self.paths.agent_run_entry)
        params = list(sig.parameters.keys())
        self.assertEqual(["agent_id"], params,
                         "agent_run_entry must accept only agent_id (no run_id parameter)")


class TestAgentRunPathsDistinctFromOtherStores(unittest.TestCase):
    """agent_run_dir is distinct from all run-scoped and other workspace-level stores."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.workspace = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.workspace)

    def tearDown(self):
        self.tmp.cleanup()

    def test_agent_run_dir_differs_from_agent_map_dir(self):
        agent_run_dir = self.paths.agent_run_dir()
        agent_map_dir = self.paths.agent_map_dir(_RUN_ID)
        self.assertNotEqual(agent_run_dir, agent_map_dir)

    def test_agent_run_dir_differs_from_pending_dispatch_dir(self):
        agent_run_dir = self.paths.agent_run_dir()
        pending_dir = self.paths.pending_dispatch_dir(_RUN_ID)
        self.assertNotEqual(agent_run_dir, pending_dir)

    def test_agent_run_dir_differs_from_session_run_id_dir(self):
        agent_run_dir = self.paths.agent_run_dir()
        session_dir = self.paths.session_run_id_dir()
        self.assertNotEqual(agent_run_dir, session_dir)

    def test_agent_run_entry_differs_from_session_run_id_entry(self):
        """Even if agent_id and session_id happen to be the same string, paths differ."""
        shared_key = "key-abc-123"
        agent_entry = self.paths.agent_run_entry(shared_key)
        session_entry = self.paths.session_run_id_entry(shared_key)
        self.assertNotEqual(agent_entry, session_entry)


if __name__ == "__main__":
    unittest.main()
