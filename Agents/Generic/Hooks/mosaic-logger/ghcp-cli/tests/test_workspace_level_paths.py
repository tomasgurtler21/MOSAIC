"""Tests for the workspace-level LogPaths accessors added for the session run-id
cache (ghcp-cli variant).

The key requirement is that the new directory family lives directly under the
logs root, NOT inside any per-run folder, because its readers do not yet know
the run_id when they need to read.

Covers:
- MODULE CONSTANT: SESSION_RUN_ID_DIRNAME
- session_run_id_dir(): directly under logs root, correct name, creates no dirs
- session_run_id_entry(session_id): inside session_run_id_dir(), .json suffix,
  session_id is sanitized into a single safe path component
- Accessor is distinct from run-scoped stores (agent_map_dir, pending_dispatch_dir)
"""

import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core


_SESSION_ID = "ghcp-sess-path-test"
_SESSION_ID_RESERVED = 'sess/with:reserved"chars'
_RUN_ID = "20260804T150537Z-796a"


class TestModuleLevelDirNameConstants(unittest.TestCase):
    """SESSION_RUN_ID_DIRNAME must be defined on the module."""

    def test_session_run_id_dirname_constant_exists(self):
        self.assertTrue(hasattr(core, "SESSION_RUN_ID_DIRNAME"))

    def test_session_run_id_dirname_value(self):
        self.assertEqual(".session-run-id", core.SESSION_RUN_ID_DIRNAME)


class TestSessionRunIdDirAccessor(unittest.TestCase):
    """session_run_id_dir() resolves directly under the logs root."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.workspace = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.workspace)

    def tearDown(self):
        self.tmp.cleanup()

    def test_method_exists_and_is_callable(self):
        self.assertTrue(hasattr(self.paths, "session_run_id_dir"))
        self.assertTrue(callable(self.paths.session_run_id_dir))

    def test_returns_pathlib_path(self):
        result = self.paths.session_run_id_dir()
        self.assertIsInstance(result, pathlib.Path)

    def test_parent_is_logs_root(self):
        """session_run_id_dir must be a direct child of logs root."""
        result = self.paths.session_run_id_dir()
        self.assertEqual(self.paths.root, result.parent)

    def test_directory_name_is_correct(self):
        result = self.paths.session_run_id_dir()
        self.assertEqual(".session-run-id", result.name)

    def test_is_not_inside_any_run_root(self):
        """The directory must be a sibling of per-run folders, not nested inside one."""
        result = self.paths.session_run_id_dir()
        run_root = self.paths.run_root(_RUN_ID)
        # result must not start with run_root's path
        self.assertFalse(
            str(result).startswith(str(run_root)),
            f"session_run_id_dir {result} must not be under run_root {run_root}",
        )

    def test_creates_no_directories(self):
        """Consistent with every other LogPaths accessor, no directories are created."""
        self.paths.session_run_id_dir()
        session_dir = self.paths.root / ".session-run-id"
        self.assertFalse(session_dir.exists())

    def test_result_is_deterministic(self):
        first = self.paths.session_run_id_dir()
        second = self.paths.session_run_id_dir()
        self.assertEqual(first, second)


class TestSessionRunIdEntryAccessor(unittest.TestCase):
    """session_run_id_entry(session_id) builds the correct per-session path."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.workspace = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.workspace)

    def tearDown(self):
        self.tmp.cleanup()

    def test_method_exists_and_is_callable(self):
        self.assertTrue(hasattr(self.paths, "session_run_id_entry"))
        self.assertTrue(callable(self.paths.session_run_id_entry))

    def test_returns_pathlib_path(self):
        result = self.paths.session_run_id_entry(_SESSION_ID)
        self.assertIsInstance(result, pathlib.Path)

    def test_has_json_suffix(self):
        result = self.paths.session_run_id_entry(_SESSION_ID)
        self.assertEqual(".json", result.suffix)

    def test_parent_is_session_run_id_dir(self):
        result = self.paths.session_run_id_entry(_SESSION_ID)
        self.assertEqual(self.paths.session_run_id_dir(), result.parent)

    def test_stem_equals_sanitized_session_id(self):
        result = self.paths.session_run_id_entry(_SESSION_ID)
        expected_stem = core.sanitize_component(_SESSION_ID)
        self.assertEqual(expected_stem, result.stem)

    def test_session_id_with_reserved_chars_is_sanitized(self):
        result = self.paths.session_run_id_entry(_SESSION_ID_RESERVED)
        for ch in '<>:"/\\|?*':
            self.assertNotIn(ch, result.name,
                             f"Reserved char {ch!r} found in entry name {result.name!r}")

    def test_different_session_ids_produce_different_paths(self):
        entry_a = self.paths.session_run_id_entry("session-alpha")
        entry_b = self.paths.session_run_id_entry("session-beta")
        self.assertNotEqual(entry_a, entry_b)

    def test_same_session_id_is_deterministic(self):
        first = self.paths.session_run_id_entry(_SESSION_ID)
        second = self.paths.session_run_id_entry(_SESSION_ID)
        self.assertEqual(first, second)

    def test_creates_no_directories(self):
        self.paths.session_run_id_entry(_SESSION_ID)
        session_dir = self.paths.root / ".session-run-id"
        self.assertFalse(session_dir.exists())

    def test_is_not_inside_any_run_root(self):
        result = self.paths.session_run_id_entry(_SESSION_ID)
        run_root = self.paths.run_root(_RUN_ID)
        self.assertFalse(
            str(result).startswith(str(run_root)),
            f"session_run_id_entry {result} must not be under run_root {run_root}",
        )


class TestWorkspaceLevelPathsAreDistinctFromRunScopedPaths(unittest.TestCase):
    """session_run_id_dir is distinct from run-scoped stores."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.workspace = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.workspace)

    def tearDown(self):
        self.tmp.cleanup()

    def test_session_run_id_dir_differs_from_agent_map_dir(self):
        session_dir = self.paths.session_run_id_dir()
        agent_map_dir = self.paths.agent_map_dir(_RUN_ID)
        self.assertNotEqual(session_dir, agent_map_dir)

    def test_session_run_id_dir_differs_from_pending_dispatch_dir(self):
        session_dir = self.paths.session_run_id_dir()
        pending_dir = self.paths.pending_dispatch_dir(_RUN_ID)
        self.assertNotEqual(session_dir, pending_dir)


if __name__ == "__main__":
    unittest.main()
