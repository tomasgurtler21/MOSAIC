"""Tests for run_id succession within a single session and independence across
sessions in the session run-id cache (ghcp-cli variant).

A workspace can host several orchestration runs. Sessions are keyed by session id.
Within one session a user may start a second run (reusing the same chat session).
The cache must be latest-wins so events after the switchover go to the new run,
never to the previous one.

Covers:
- Latest-wins write: second put for same session replaces first run_id
- Only the most-recently-written run_id is returned after multiple writes
- After succession, resolve_run_identity for that session yields the new run_id
- effective_run_id is the new run_id after succession
- Two different sessions cache their run_ids independently
- Overwriting one session does not affect the other session's cached value
- A put/get for session A never touches session B's cache file
- resolve_run_identity uses the session-specific cache key (no cross-session bleed)
- Each session gets its own distinct cache file (no shared index)
"""

import os
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import mosaic_logger_core as core
import mosaic_logger_runstate as runstate
import mosaic_logger


_RUN_ID_FIRST = "20260804T140000Z-aaaa"
_RUN_ID_SECOND = "20260804T160000Z-bbbb"
_RUN_ID_SESSION_B = "20260804T170000Z-cccc"
_SESSION_ID_A = "ghcp-sess-succession-a"
_SESSION_ID_B = "ghcp-sess-succession-b"
_TS = "2026-08-04T16:00:00.000Z"


def _make_ctx(event, tmp_path, session_id=_SESSION_ID_A, **payload_extra):
    payload = {"sessionId": session_id, **payload_extra}
    paths = core.build_paths(tmp_path)
    return core.HookContext(event, payload, tmp_path, paths, _TS)


class TestLatestWinsWithinOneSession(unittest.TestCase):
    """A second run_id for the same session replaces the first (latest-wins)."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def test_second_put_replaces_first_run_id(self):
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_FIRST)
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_SECOND)
        result = runstate.get_session_run_id(self.paths, _SESSION_ID_A)
        self.assertEqual(_RUN_ID_SECOND, result)

    def test_first_run_id_is_not_returned_after_second_put(self):
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_FIRST)
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_SECOND)
        result = runstate.get_session_run_id(self.paths, _SESSION_ID_A)
        self.assertNotEqual(_RUN_ID_FIRST, result)

    def test_many_successive_puts_always_returns_last(self):
        run_ids = [
            "20260804T100000Z-1111",
            "20260804T110000Z-2222",
            "20260804T120000Z-3333",
            _RUN_ID_SECOND,
        ]
        for rid in run_ids:
            runstate.put_session_run_id(self.paths, _SESSION_ID_A, rid)
        result = runstate.get_session_run_id(self.paths, _SESSION_ID_A)
        self.assertEqual(_RUN_ID_SECOND, result)

    def test_second_put_still_returns_true(self):
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_FIRST)
        result = runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_SECOND)
        self.assertTrue(result)

    def test_one_file_per_session_after_multiple_puts(self):
        """Multiple puts for one session produce exactly one cache file (not appended)."""
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_FIRST)
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_SECOND)
        session_dir = self.paths.session_run_id_dir()
        if session_dir.exists():
            json_files = list(session_dir.glob("*.json"))
            # Only one file for session A
            stems = {f.stem for f in json_files}
            expected_stem = core.sanitize_component(_SESSION_ID_A)
            self.assertIn(expected_stem, stems)
            # Count how many files correspond to session A
            session_a_files = [
                f for f in json_files
                if f.stem == core.sanitize_component(_SESSION_ID_A)
            ]
            self.assertEqual(1, len(session_a_files))


class TestSubsequentEventsResolveToNewerRun(unittest.TestCase):
    """After run_id succession, events resolve to the newer run via the cache."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def test_pre_tool_use_resolves_to_second_run_after_succession(self):
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_FIRST)
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_SECOND)
        ctx = _make_ctx("preToolUse", self.tmp_path, session_id=_SESSION_ID_A,
                        toolArgs={"command": "echo hi"})
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(_RUN_ID_SECOND, ctx.run_id)

    def test_effective_run_id_is_second_run_after_succession(self):
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_FIRST)
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_SECOND)
        ctx = _make_ctx("sessionEnd", self.tmp_path, session_id=_SESSION_ID_A)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertEqual(_RUN_ID_SECOND, core.effective_run_id(ctx))

    def test_first_run_id_is_not_effective_run_id_after_succession(self):
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_FIRST)
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_SECOND)
        ctx = _make_ctx("preToolUse", self.tmp_path, session_id=_SESSION_ID_A)
        mosaic_logger.resolve_run_identity(ctx)
        self.assertNotEqual(_RUN_ID_FIRST, core.effective_run_id(ctx))


class TestCrossSessionIsolation(unittest.TestCase):
    """Two different sessions cache their run_ids independently."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.tmp_path = pathlib.Path(self.tmp.name)
        self.paths = core.build_paths(self.tmp_path)

    def tearDown(self):
        self.tmp.cleanup()

    def test_sessions_cache_independently(self):
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_FIRST)
        runstate.put_session_run_id(self.paths, _SESSION_ID_B, _RUN_ID_SESSION_B)
        result_a = runstate.get_session_run_id(self.paths, _SESSION_ID_A)
        result_b = runstate.get_session_run_id(self.paths, _SESSION_ID_B)
        self.assertEqual(_RUN_ID_FIRST, result_a)
        self.assertEqual(_RUN_ID_SESSION_B, result_b)

    def test_overwriting_session_a_does_not_change_session_b(self):
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_FIRST)
        runstate.put_session_run_id(self.paths, _SESSION_ID_B, _RUN_ID_SESSION_B)
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_SECOND)
        result_b = runstate.get_session_run_id(self.paths, _SESSION_ID_B)
        self.assertEqual(_RUN_ID_SESSION_B, result_b)

    def test_session_b_not_affected_when_session_a_has_no_entry(self):
        runstate.put_session_run_id(self.paths, _SESSION_ID_B, _RUN_ID_SESSION_B)
        result_a = runstate.get_session_run_id(self.paths, _SESSION_ID_A)
        result_b = runstate.get_session_run_id(self.paths, _SESSION_ID_B)
        self.assertIsNone(result_a)
        self.assertEqual(_RUN_ID_SESSION_B, result_b)

    def test_resolve_run_identity_session_b_not_contaminated_by_session_a(self):
        """resolve_run_identity must use the session-specific cache key."""
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_FIRST)
        runstate.put_session_run_id(self.paths, _SESSION_ID_B, _RUN_ID_SESSION_B)
        ctx_b = _make_ctx("preToolUse", self.tmp_path, session_id=_SESSION_ID_B)
        mosaic_logger.resolve_run_identity(ctx_b)
        self.assertEqual(_RUN_ID_SESSION_B, ctx_b.run_id)

    def test_resolve_run_identity_session_a_not_contaminated_by_session_b(self):
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_FIRST)
        runstate.put_session_run_id(self.paths, _SESSION_ID_B, _RUN_ID_SESSION_B)
        ctx_a = _make_ctx("preToolUse", self.tmp_path, session_id=_SESSION_ID_A)
        mosaic_logger.resolve_run_identity(ctx_a)
        self.assertEqual(_RUN_ID_FIRST, ctx_a.run_id)

    def test_two_sessions_occupy_distinct_cache_files(self):
        runstate.put_session_run_id(self.paths, _SESSION_ID_A, _RUN_ID_FIRST)
        runstate.put_session_run_id(self.paths, _SESSION_ID_B, _RUN_ID_SESSION_B)
        entry_a = self.paths.session_run_id_entry(_SESSION_ID_A)
        entry_b = self.paths.session_run_id_entry(_SESSION_ID_B)
        self.assertNotEqual(entry_a, entry_b)
        self.assertTrue(entry_a.exists())
        self.assertTrue(entry_b.exists())


if __name__ == "__main__":
    unittest.main()
